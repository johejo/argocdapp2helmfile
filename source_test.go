package main

import (
	"strings"
	"testing"
)

func TestConvertValueFilesPrecedeInlineValues(t *testing.T) {
	repoURL := "https://github.com/example/charts.git"
	resolver := testSourceResolver(t, testSource{
		repoURL: repoURL, targetRevision: "main", root: `{{ requiredEnv "TEST_CHART_ROOT" }}`,
	})
	input := gitApplication(repoURL, "charts/app", "main", `    helm:
      valueFiles:
        - values.yaml
        - environments/prod.yaml
      values: |
        image:
          tag: inline
      valuesObject:
        image:
          pullPolicy: Always
      parameters:
        - name: replicaCount
          value: "3"
`)
	output, err := convertWithSourceMap([]byte(input), resolver)
	if err != nil {
		t.Fatal(err)
	}
	ordered := []string{
		`'{{ requiredEnv "TEST_CHART_ROOT" }}/charts/app/values.yaml'`,
		`'{{ requiredEnv "TEST_CHART_ROOT" }}/charts/app/environments/prod.yaml'`,
		"      - image:\n          tag: inline",
		"      - image:\n          pullPolicy: Always",
		"    set:\n      - name: replicaCount",
	}
	position := -1
	for _, fragment := range ordered {
		next := strings.Index(string(output)[position+1:], fragment)
		if next < 0 {
			t.Fatalf("output does not contain %q in precedence order:\n%s", fragment, output)
		}
		position += next + 1
	}
}

func TestConvertValueFilesShareMappedSourceAcrossDocuments(t *testing.T) {
	repoURL := "https://github.com/example/charts.git"
	resolver := testSourceResolver(t, testSource{
		repoURL: repoURL, targetRevision: "main", root: `{{ requiredEnv "TEST_CHART_ROOT" }}`,
	})
	first := gitApplication(repoURL, "chart", "main", "    helm:\n      valueFiles: [values.yaml]\n")
	second := strings.Replace(first, "name: app", "name: second", 1)
	output, err := convertWithSourceMap([]byte(first+"---\n"+second), resolver)
	if err != nil {
		t.Fatal(err)
	}
	want := `{{ requiredEnv "TEST_CHART_ROOT" }}/chart/values.yaml`
	if strings.Count(string(output), want) != 2 {
		t.Errorf("output does not contain two mapped values paths:\n%s", output)
	}
}

func TestConvertIgnoreMissingValueFiles(t *testing.T) {
	repoURL := "git@github.com:example/charts.git"
	resolver := testSourceResolver(t, testSource{
		repoURL: repoURL, targetRevision: "main", root: `{{ requiredEnv "TEST_CHART_ROOT" }}`,
	})
	input := gitApplication(repoURL, "chart", "main", `    helm:
      valueFiles: [optional.yaml]
      ignoreMissingValueFiles: true
`)
	output, err := convertWithSourceMap([]byte(input), resolver)
	if err != nil {
		t.Fatal(err)
	}
	text := string(output)
	if !strings.Contains(text, "    missingFileHandler: Warn\n") {
		t.Fatalf("missingFileHandler was not emitted:\n%s", output)
	}
	if !strings.Contains(text, `{{ requiredEnv "TEST_CHART_ROOT" }}/chart/optional.yaml`) {
		t.Fatalf("missing value file was not delegated to helmfile:\n%s", output)
	}
}

func TestConvertRejectsNonRefValueFilesForRemoteCharts(t *testing.T) {
	for _, valueFile := range []string{"values.yaml", "/values.yaml", "*.yaml"} {
		t.Run(valueFile, func(t *testing.T) {
			input := minimalApplication("    helm:\n      valueFiles:\n        - " + yamlScalar(valueFile) + "\n")
			if output, err := convert([]byte(input)); err == nil ||
				!strings.Contains(err.Error(), "non-$ref valueFiles are not supported") {
				t.Fatalf("convert succeeded with output:\n%s", output)
			}
		})
	}
}

func TestConvertMultiSourceValueReferences(t *testing.T) {
	input := readTestdata(t, "multi-source/application.yaml")
	resolver := testSourceResolver(t,
		testSource{
			repoURL: "https://github.com/example/values.git", targetRevision: "0123456789abcdef",
			root: `{{ requiredEnv "TEST_VALUES_ROOT" }}`,
		},
		testSource{
			repoURL: "ssh://git@example.com/secrets.git", targetRevision: "release-1",
			root: `{{ requiredEnv "TEST_SECRETS_ROOT" }}`,
		},
	)
	output, err := convertWithSourceMap([]byte(input), resolver)
	if err != nil {
		t.Fatal(err)
	}
	text := string(output)
	for _, want := range []string{
		`{{ requiredEnv "TEST_VALUES_ROOT" }}/environments/prod.yaml`,
		`{{ requiredEnv "TEST_SECRETS_ROOT" }}/team/values.yaml`,
		`# document 1 values source ref "values": repoURL "https://github.com/example/values.git", targetRevision "0123456789abcdef"`,
		`# document 1 values source ref "secrets": repoURL "ssh://git@example.com/secrets.git", targetRevision "release-1"`,
	} {
		if !strings.Contains(text, want) {
			t.Errorf("output does not contain %q:\n%s", want, output)
		}
	}
}

func TestConvertRejectsInvalidMultiSources(t *testing.T) {
	chart := `    - repoURL: https://example.com/charts
      chart: chart
      targetRevision: 1.2.3
`
	values := `    - repoURL: https://github.com/example/values.git
      targetRevision: main
      ref: values
`
	tests := map[string]string{
		"undefined ref":   chartWithValueFile("$missing/values.yaml") + values,
		"duplicate ref":   chart + values + values,
		"unsafe ref":      chart + strings.Replace(values, "ref: values", "ref: ../values", 1),
		"source path":     chart + strings.Replace(values, "ref: values", "ref: values\n      path: manifests", 1),
		"source plugin":   chart + strings.Replace(values, "ref: values", "ref: values\n      plugin: {}", 1),
		"multiple charts": chart + chart,
		"no chart":        values,
	}
	for name, sources := range tests {
		t.Run(name, func(t *testing.T) {
			input := "apiVersion: argoproj.io/v1alpha1\nkind: Application\nmetadata:\n  name: app\nspec:\n  sources:\n" + sources
			if output, err := convert([]byte(input)); err == nil {
				t.Fatalf("convert succeeded with output:\n%s", output)
			}
		})
	}
}

func TestConvertGitChartWithExternalValuesSource(t *testing.T) {
	input := readTestdata(t, "git-chart-external-values/application.yaml")
	resolver := testSourceResolver(t,
		testSource{
			repoURL: "git@github.com:example/charts.git", targetRevision: "release-1",
			root: `{{ requiredEnv "TEST_CHART_ROOT" }}`,
		},
		testSource{
			repoURL: "git@git.example.com:platform/values.git", targetRevision: "main",
			root: `{{ requiredEnv "TEST_VALUES_ROOT" }}`,
		},
	)
	output, err := convertWithSourceMap([]byte(input), resolver)
	if err != nil {
		t.Fatal(err)
	}
	text := string(output)
	for _, want := range []string{
		`# document 1 chart source: repoURL "git@github.com:example/charts.git", path "charts/app", targetRevision "release-1"`,
		`# document 1 values source ref "values": repoURL "git@git.example.com:platform/values.git", targetRevision "main"`,
		`{{ requiredEnv "TEST_VALUES_ROOT" }}/prod/values.yaml`,
	} {
		if !strings.Contains(text, want) {
			t.Errorf("output does not contain %q:\n%s", want, output)
		}
	}
}

func TestConvertRejectsInvalidGitCharts(t *testing.T) {
	tests := map[string]string{
		"chart instead of path": strings.Replace(
			gitApplication("git@github.com:example/charts.git", "charts/app", "main", ""),
			"path: charts/app", "chart: app", 1,
		),
		"chart and path": strings.Replace(
			gitApplication("git@github.com:example/charts.git", "charts/app", "main", ""),
			"path: charts/app", "chart: app\n    path: charts/app", 1,
		),
		"HTTP repository with path": strings.Replace(
			gitApplication("git@github.com:example/charts.git", "charts/app", "main", ""),
			"git@github.com:example/charts.git", "https://github.com/example/charts.git", 1,
		),
		"unsafe path":           gitApplication("git@github.com:example/charts.git", "../charts/app", "main", ""),
		"empty revision":        gitApplication("git@github.com:example/charts.git", "charts/app", "''", ""),
		"missing SCP separator": gitApplication("git@github.com/example/charts.git", "charts/app", "main", ""),
		"empty SCP path":        gitApplication("git@github.com:/", "charts/app", "main", ""),
		"SSH password":          gitApplication("ssh://git:secret@git.example.com/example/charts.git", "charts/app", "main", ""),
		"SSH query":             gitApplication("ssh://git@git.example.com/example/charts.git?ref=main", "charts/app", "main", ""),
		"SSH fragment":          gitApplication("ssh://git@git.example.com/example/charts.git#main", "charts/app", "main", ""),
		"SSH empty query":       gitApplication("ssh://git@git.example.com/example/charts.git?", "charts/app", "main", ""),
	}
	for name, input := range tests {
		t.Run(name, func(t *testing.T) {
			if output, err := convert([]byte(input)); err == nil {
				t.Fatalf("convert succeeded with output:\n%s", output)
			}
		})
	}
}

func chartWithValueFile(valueFile string) string {
	return `    - repoURL: https://example.com/charts
      chart: chart
      targetRevision: 1.2.3
      helm:
        valueFiles:
          - ` + valueFile + "\n"
}

func yamlScalar(value string) string {
	return `"` + strings.ReplaceAll(value, `\`, `\\`) + `"`
}
