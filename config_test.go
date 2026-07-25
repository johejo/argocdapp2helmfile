package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestParseConfigRejectsInvalidConfiguration(t *testing.T) {
	valid := `apiVersion: argocdapp2helmfile/v1alpha1
kind: Config
sources:
  - repoURL: https://github.com/example/repo.git
    targetRevision: main
    localRoot: '{{ requiredEnv "REPO_ROOT" }}'
`
	tests := map[string]string{
		"apiVersion":    strings.Replace(valid, configAPIVersion, "other/v1", 1),
		"kind":          strings.Replace(valid, "Config", "Other", 1),
		"unknown field": valid + "unknown: true\n",
		"missing revision": strings.Replace(
			valid,
			"    targetRevision: main\n",
			"",
			1,
		),
		"empty localRoot": strings.Replace(valid, `localRoot: '{{ requiredEnv "REPO_ROOT" }}'`, `localRoot: ""`, 1),
		"legacy env": strings.Replace(
			valid,
			`localRoot: '{{ requiredEnv "REPO_ROOT" }}'`,
			"env: REPO_ROOT",
			1,
		),
		"allowDirty": strings.Replace(
			valid,
			`localRoot: '{{ requiredEnv "REPO_ROOT" }}'`,
			"localRoot: /repo\n    allowDirty: true",
			1,
		),
		"duplicate key": valid + `  - repoURL: https://github.com/example/repo.git
    targetRevision: main
    localRoot: /other
`,
		"old kind": strings.Replace(valid, "kind: Config", "kind: SourceMap", 1),
		"multiple documents": valid + `---
apiVersion: argocdapp2helmfile/v1alpha1
kind: Config
`,
	}
	for name, input := range tests {
		t.Run(name, func(t *testing.T) {
			if _, err := parseConfig([]byte(input)); err == nil {
				t.Fatal("parseConfig succeeded")
			}
		})
	}
}

func TestConfigAllowsLiteralAndDuplicateLocalRoots(t *testing.T) {
	const config = `apiVersion: argocdapp2helmfile/v1alpha1
kind: Config
sources:
  - repoURL: https://github.com/example/charts.git
    targetRevision: main
    localRoot: '{{ requiredEnv "SOURCES_ROOT" }}'
  - repoURL: https://github.com/example/values.git
    targetRevision: main
    localRoot: '{{ requiredEnv "SOURCES_ROOT" }}'
`
	parsed, err := parseConfig([]byte(config))
	if err != nil {
		t.Fatal(err)
	}
	resolved, err := parsed.sourceResolver.resolve(applicationSource{
		RepoURL: "https://github.com/example/charts.git", TargetRevision: "main",
	}, "source")
	if err != nil {
		t.Fatal(err)
	}
	if resolved.localRoot != `{{ requiredEnv "SOURCES_ROOT" }}` {
		t.Fatalf("unexpected localRoot: %q", resolved.localRoot)
	}
}

func TestConfigDestinationsResolveNameAndServer(t *testing.T) {
	config := testConfig(t, `destinations:
  - name: production
    kubeContext: prod-admin
  - server: https://example
    kubeContext: example-admin
`)
	tests := []struct {
		name        string
		destination applicationDestination
		want        string
	}{
		{
			name:        "name",
			destination: applicationDestination{Name: "production"},
			want:        "prod-admin",
		},
		{
			name:        "server",
			destination: applicationDestination{Server: "https://example"},
			want:        "example-admin",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got, err := config.destinationResolver.resolve(test.destination, "spec.destination")
			if err != nil {
				t.Fatal(err)
			}
			if got != test.want {
				t.Fatalf("resolve() = %q, want %q", got, test.want)
			}
		})
	}
}

func TestParseConfigRejectsInvalidDestinations(t *testing.T) {
	tests := map[string]string{
		"missing selector": `destinations:
  - kubeContext: admin
`,
		"name and server": `destinations:
  - name: production
    server: https://example
    kubeContext: admin
`,
		"missing kube context": `destinations:
  - name: production
`,
		"empty kube context": `destinations:
  - name: production
    kubeContext: " "
`,
		"duplicate name": `destinations:
  - name: production
    kubeContext: first
  - name: production
    kubeContext: second
`,
		"duplicate server": `destinations:
  - server: https://example
    kubeContext: first
  - server: https://example
    kubeContext: second
`,
		"unknown field": `destinations:
  - name: production
    kubeContext: admin
    namespace: default
`,
	}
	for name, body := range tests {
		t.Run(name, func(t *testing.T) {
			input := "apiVersion: argocdapp2helmfile/v1alpha1\nkind: Config\n" + body
			if _, err := parseConfig([]byte(input)); err == nil {
				t.Fatal("parseConfig succeeded")
			}
		})
	}
}

func TestConfigClustersResolveNameAndServer(t *testing.T) {
	config, err := parseConfig([]byte(readTestdata(t, "config/clusters/valid.yaml")))
	if err != nil {
		t.Fatal(err)
	}
	for _, destination := range []applicationDestination{
		{Name: "production"},
		{Server: "https://production.example.com"},
	} {
		got, err := config.destinationResolver.resolve(destination, "spec.destination")
		if err != nil {
			t.Fatal(err)
		}
		if got != "prod-admin" {
			t.Fatalf("resolve(%#v) = %q, want prod-admin", destination, got)
		}
	}
}

func TestParseConfigRejectsInvalidClusters(t *testing.T) {
	for _, name := range []string{
		"missing-name",
		"missing-server",
		"missing-kube-context",
		"duplicate-name",
		"duplicate-server",
		"destination-conflict",
		"non-string-label",
		"unknown-field",
	} {
		t.Run(name, func(t *testing.T) {
			input := readTestdata(t, "config/clusters/"+name+".yaml")
			if _, err := parseConfig([]byte(input)); err == nil {
				t.Fatal("parseConfig succeeded")
			}
		})
	}
}

func TestJoinSourcePath(t *testing.T) {
	for name, test := range map[string]struct {
		root, relative, want string
	}{
		"template":        {`{{ requiredEnv "ROOT" }}`, "charts/app", `{{ requiredEnv "ROOT" }}/charts/app`},
		"trailing slash":  {"/workspace/", "charts/app", "/workspace/charts/app"},
		"repository root": {"/workspace", ".", "/workspace"},
	} {
		t.Run(name, func(t *testing.T) {
			if got := joinSourcePath(test.root, test.relative); got != test.want {
				t.Fatalf("joinSourcePath() = %q, want %q", got, test.want)
			}
		})
	}
}

func TestConvertRequiresMappedGitSource(t *testing.T) {
	input := gitApplication("https://github.com/example/charts.git", "chart", "main", "")
	if output, err := convert([]byte(input)); err == nil || !strings.Contains(err.Error(), "requires --config") {
		t.Fatalf("unexpected result: %s, %v", output, err)
	}
}

func TestRunWithConfigDoesNotResolveLocalRoot(t *testing.T) {
	configPath := filepath.Join(t.TempDir(), "config.yaml")
	config := `apiVersion: argocdapp2helmfile/v1alpha1
kind: Config
sources:
  - repoURL: https://github.com/example/charts.git
    targetRevision: main
    localRoot: '{{ requiredEnv "REPO_ROOT" }}'
`
	if err := os.WriteFile(configPath, []byte(config), 0o644); err != nil {
		t.Fatal(err)
	}
	input := gitApplication("https://github.com/example/charts.git", "chart", "main", "")
	var stdout, stderr bytes.Buffer
	if code := run([]string{"--config", configPath}, strings.NewReader(input), &stdout, &stderr); code != 0 {
		t.Fatalf("run returned %d: %s", code, stderr.String())
	}
	if !strings.Contains(stdout.String(), `chart: '{{ requiredEnv "REPO_ROOT" }}/chart'`) {
		t.Fatalf("unexpected output:\n%s", stdout.String())
	}
}
