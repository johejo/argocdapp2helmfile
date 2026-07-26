package main

import (
	"strings"
	"testing"
)

func TestConvertGitFileParameterPathsAndOrdering(t *testing.T) {
	const repoURL = "git@github.com:example/charts.git"
	resolver := testSourceResolver(t, testSource{
		repoURL: repoURL, targetRevision: "main", root: `{{ requiredEnv "CHARTS_ROOT" }}`,
	})
	input := readTestdata(t, "file-parameters/git-paths/application.yaml")
	output, err := convertWithResolver([]byte(input), resolver)
	if err != nil {
		t.Fatal(err)
	}
	ordered := []string{
		"name: ordinary\n        value: value",
		"name: overridden\n        value: old",
		`name: relative
        file: '{{ requiredEnv "CHARTS_ROOT" }}/charts/app/files/config.json'`,
		`name: root
        file: '{{ requiredEnv "CHARTS_ROOT" }}/shared/config.json'`,
		`name: parent
        file: '{{ requiredEnv "CHARTS_ROOT" }}/charts/common/config.json'`,
		`name: overridden
        file: '{{ requiredEnv "CHARTS_ROOT" }}/charts/app/files/new.json'`,
		"files/first.json",
		"files/last.json",
	}
	position := -1
	for _, fragment := range ordered {
		next := strings.Index(string(output)[position+1:], fragment)
		if next < 0 {
			t.Fatalf("output does not contain %q in order:\n%s", fragment, output)
		}
		position += next + 1
	}
}

func TestConvertRemoteChartFileParameters(t *testing.T) {
	t.Run("ordinary path rejected", func(t *testing.T) {
		for _, repository := range []struct {
			name    string
			repoURL string
		}{
			{name: "HTTP", repoURL: "https://example.com/charts"},
			{name: "OCI", repoURL: "registry.example.com/charts"},
		} {
			t.Run(repository.name, func(t *testing.T) {
				input := strings.Replace(minimalApplication(`    helm:
      fileParameters:
        - name: config
          path: files/config.json
`), "https://example.com/charts", repository.repoURL, 1)
				if output, err := convert([]byte(input)); err == nil ||
					!strings.Contains(err.Error(), "non-$ref fileParameters are not supported") {
					t.Fatalf("unexpected output/error:\n%s\n%v", output, err)
				}
			})
		}
	})

	t.Run("$ref resolved", func(t *testing.T) {
		input := readTestdata(t, "file-parameters/ref/application.yaml")
		resolver := testSourceResolver(t, testSource{
			repoURL:        "https://github.com/example/files.git",
			targetRevision: "main",
			root:           `{{ requiredEnv "FILES_ROOT" }}`,
		})
		output, err := convertWithResolver([]byte(input), resolver)
		if err != nil {
			t.Fatal(err)
		}
		want := `file: '{{ requiredEnv "FILES_ROOT" }}/config/app.json'`
		if !strings.Contains(string(output), want) {
			t.Fatalf("output does not contain %q:\n%s", want, output)
		}
	})
}

func TestConvertRejectsInvalidFileParameters(t *testing.T) {
	tests := map[string]string{
		"not a sequence":   `fileParameters: wrong`,
		"item not mapping": `fileParameters: [wrong]`,
		"missing name":     `fileParameters: [{path: file}]`,
		"non-string name":  `fileParameters: [{name: 1, path: file}]`,
		"missing path":     `fileParameters: [{name: config}]`,
		"empty path":       `fileParameters: [{name: config, path: ""}]`,
		"blank path":       `fileParameters: [{name: config, path: "  "}]`,
		"non-string path":  `fileParameters: [{name: config, path: 1}]`,
		"unknown field":    `fileParameters: [{name: config, path: file, extra: true}]`,
	}
	for name, option := range tests {
		t.Run(name, func(t *testing.T) {
			input := minimalApplication("    helm:\n      " + option + "\n")
			if output, err := convert([]byte(input)); err == nil {
				t.Fatalf("conversion succeeded:\n%s", output)
			}
		})
	}
}

func TestConvertRejectsUnsafeFileParameterPaths(t *testing.T) {
	const repoURL = "git@github.com:example/charts.git"
	resolver := testSourceResolver(t, testSource{
		repoURL: repoURL, targetRevision: "main", root: "/workspace/charts",
	})
	tests := map[string]string{
		"outside repository":        "../../outside",
		"undefined ref":             "$missing/file",
		"URL":                       "https://example.com/file",
		"backslash":                 `files\config`,
		"control":                   "files/\tconfig",
		"dynamic build environment": "env/$ARGOCD_APP_REVISION/config",
		"unknown build environment": "env/$ARGOCD_UNKNOWN/config",
	}
	for name, parameterPath := range tests {
		t.Run(name, func(t *testing.T) {
			input := gitApplication(repoURL, "chart", "main", "    helm:\n      fileParameters:\n        - name: config\n          path: "+yamlScalar(parameterPath)+"\n")
			if output, err := convertWithResolver([]byte(input), resolver); err == nil {
				t.Fatalf("conversion succeeded:\n%s", output)
			}
		})
	}
}

func TestConvertExpandsFileParameterBuildEnvironment(t *testing.T) {
	resolver := testSourceResolver(t, testSource{
		repoURL:        "git@github.com:example/charts.git",
		targetRevision: "main",
		root:           "/workspace/charts",
	})
	input := readTestdata(t, "file-parameters/environment/application.yaml")
	output, err := convertWithResolver([]byte(input), resolver)
	if err != nil {
		t.Fatal(err)
	}
	assertOrdered(t, string(output), []string{
		"charts/app/env/static-app.json",
		"charts/app/env/argocd.json",
		"charts/app/env/production.json",
		"charts/app/env/main-static-app.json",
		"charts/app/env/$ARGOCD_APP_REVISION.json",
		"charts/app/$literal.json",
	})
}

func TestConvertRejectsForceStringFileParameterConflict(t *testing.T) {
	input := minimalApplication(`    helm:
      parameters:
        - name: config
          value: old
          forceString: true
      fileParameters:
        - name: config
          path: $files/config
`)
	if output, err := convert([]byte(input)); err == nil ||
		!strings.Contains(err.Error(), `name "config" conflicts with a forceString parameter`) {
		t.Fatalf("unexpected output/error:\n%s\n%v", output, err)
	}
}

func TestConvertApplicationSetFileParameters(t *testing.T) {
	const repoURL = "git@github.com:example/charts.git"
	resolver := testSourceResolver(t, testSource{
		repoURL: repoURL, targetRevision: "main", root: `{{ requiredEnv "CHARTS_ROOT" }}`,
	})
	input := readTestdata(t, "applicationset/file-parameters/application.yaml")
	output, err := convertWithResolver([]byte(input), resolver)
	if err != nil {
		t.Fatal(err)
	}
	want := `file: '{{ requiredEnv "CHARTS_ROOT" }}/charts/app/files/frontend.json'`
	if !strings.Contains(string(output), want) {
		t.Fatalf("output does not contain %q:\n%s", want, output)
	}
}
