package main

import (
	"strings"
	"testing"
)

func TestConvertExample(t *testing.T) {
	input := readTestdata(t, "example/application.yaml")
	output, err := convert([]byte(input))
	if err != nil {
		t.Fatal(err)
	}
	want := readTestdata(t, "example/helmfile.yaml")
	if string(output) != want {
		t.Fatalf("unexpected output:\n%s\nwant:\n%s", output, want)
	}
}

func TestConvertOCIRepository(t *testing.T) {
	input := readTestdata(t, "oci/application.yaml")
	output, err := convert([]byte(input))
	if err != nil {
		t.Fatal(err)
	}
	want := readTestdata(t, "oci/helmfile.yaml")
	if string(output) != want {
		t.Fatalf("unexpected output:\n%s\nwant:\n%s", output, want)
	}
}

func TestConvertOCIRepositoryWithPort(t *testing.T) {
	input := strings.Replace(minimalApplication(""), "https://example.com/charts", "registry.example.com:5000/charts", 1)
	output, err := convert([]byte(input))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(output), "    url: registry.example.com:5000/charts\n    oci: true\n") {
		t.Fatalf("OCI repository was not emitted correctly:\n%s", output)
	}
}

func TestConvertGitChart(t *testing.T) {
	tests := []struct {
		name      string
		repoURL   string
		chartPath string
		revision  string
	}{
		{
			name:      "SCP-like",
			repoURL:   "git@github.com:example/charts.git",
			chartPath: "deploy/charts/app",
			revision:  "release-1",
		},
		{
			name:      "SSH URL with port and root chart",
			repoURL:   "ssh://git@git.example.com:2222/example/chart.git",
			chartPath: ".",
			revision:  "0123456789abcdef",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			chartPrefix := ""
			if test.chartPath != "." {
				chartPrefix = test.chartPath + "/"
			}
			resolver := testSourceResolver(t, testSource{
				repoURL: test.repoURL, targetRevision: test.revision, root: `{{ requiredEnv "TEST_CHART_ROOT" }}`,
			})
			input := gitApplication(test.repoURL, test.chartPath, test.revision, `    helm:
      valueFiles: [environments/prod.yaml]
`)
			output, err := convertWithResolver([]byte(input), resolver)
			if err != nil {
				t.Fatal(err)
			}
			text := string(output)
			expectedChart := test.chartPath
			if expectedChart == "." {
				expectedChart = ""
			} else {
				expectedChart = "/" + expectedChart
			}
			for _, want := range []string{
				`# document 1 chart source: repoURL "` + test.repoURL + `", path "` + test.chartPath + `", targetRevision "` + test.revision + `"`,
				`chart: '{{ requiredEnv "TEST_CHART_ROOT" }}` + expectedChart + `'`,
				`'{{ requiredEnv "TEST_CHART_ROOT" }}/` + chartPrefix + `environments/prod.yaml'`,
			} {
				if !strings.Contains(text, want) {
					t.Errorf("output does not contain %q:\n%s", want, output)
				}
			}
			for _, unwanted := range []string{"repositories:", "version:"} {
				if strings.Contains(text, unwanted) {
					t.Errorf("output unexpectedly contains %q:\n%s", unwanted, output)
				}
			}
		})
	}
}

func TestConvertGitChartDoesNotConsumeRepositoryAlias(t *testing.T) {
	repoURL := "git@github.com:example/charts.git"
	resolver := testSourceResolver(t, testSource{
		repoURL: repoURL, targetRevision: "main", root: `{{ requiredEnv "TEST_CHART_ROOT" }}`,
	})
	gitInput := gitApplication(repoURL, "charts/app", "main", "")
	httpInput := strings.Replace(minimalApplication(""), "name: app", "name: packaged", 1)
	output, err := convertWithResolver([]byte(gitInput+"---\n"+httpInput), resolver)
	if err != nil {
		t.Fatal(err)
	}
	text := string(output)
	if !strings.Contains(text, "  - name: source\n    url: https://example.com/charts\n") ||
		!strings.Contains(text, "  - name: packaged\n    chart: source/chart\n") {
		t.Fatalf("HTTP repository did not retain the first alias:\n%s", output)
	}
}

func TestConvertMultipleApplications(t *testing.T) {
	second := strings.NewReplacer(
		"name: app", "name: worker",
		"https://example.com/charts", "registry.example.com/charts",
		"chart: chart", "chart: worker",
		"targetRevision: 1.2.3", "targetRevision: 4.5.6",
	).Replace(minimalApplication(""))
	input := minimalApplication("") + "---\n" + second

	output, err := convert([]byte(input))
	if err != nil {
		t.Fatal(err)
	}
	want := readTestdata(t, "multiple-applications/helmfile.yaml")
	if string(output) != want {
		t.Fatalf("unexpected output:\n%s\nwant:\n%s", output, want)
	}
}

func gitApplication(repoURL, chartPath, revision, helm string) string {
	return `apiVersion: argoproj.io/v1alpha1
kind: Application
metadata:
  name: app
spec:
  source:
    repoURL: ` + repoURL + `
    path: ` + chartPath + `
    targetRevision: ` + revision + `
` + helm
}

func TestConvertSharesOnlyExactlyMatchingRepositories(t *testing.T) {
	shared := strings.Replace(minimalApplication(""), "name: app", "name: shared", 1)
	distinct := strings.NewReplacer(
		"name: app", "name: distinct",
		"https://example.com/charts", "https://example.com/charts/",
	).Replace(minimalApplication(""))
	input := minimalApplication("") + "---\n" + shared + "---\n" + distinct

	output, err := convert([]byte(input))
	if err != nil {
		t.Fatal(err)
	}
	text := string(output)
	if strings.Count(text, "  - name: source\n") != 1 || strings.Count(text, "  - name: source-2\n") != 1 {
		t.Fatalf("repositories were not deduplicated by exact URL:\n%s", output)
	}
	for _, want := range []string{
		"    chart: source/chart\n",
		"  - name: shared\n    chart: source/chart\n",
		"  - name: distinct\n    chart: source-2/chart\n",
	} {
		if !strings.Contains(text, want) {
			t.Errorf("output does not contain %q:\n%s", want, output)
		}
	}
}

func TestConvertRejectsDuplicateReleaseNamesAcrossNamespaces(t *testing.T) {
	first := strings.Replace(minimalApplication(""), "spec:\n", "spec:\n  destination:\n    namespace: one\n", 1)
	second := strings.Replace(minimalApplication(""), "spec:\n", "spec:\n  destination:\n    namespace: two\n", 1)

	_, err := convert([]byte(first + "---\n" + second))
	if err == nil || !strings.Contains(err.Error(), `document 2: release name "app" duplicates document 1`) {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestConvertAggregatesSkipCRDs(t *testing.T) {
	t.Run("all true", func(t *testing.T) {
		first := minimalApplication("    helm:\n      skipCrds: true\n")
		second := strings.Replace(first, "name: app", "name: second", 1)
		output, err := convert([]byte(first + "---\n" + second))
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(string(output), "helmDefaults:\n  skipCRDs: true\n") {
			t.Fatalf("shared skipCRDs was not emitted:\n%s", output)
		}
	})

	t.Run("false and absent", func(t *testing.T) {
		first := minimalApplication("    helm:\n      skipCrds: false\n")
		second := strings.Replace(minimalApplication(""), "name: app", "name: second", 1)
		output, err := convert([]byte(first + "---\n" + second))
		if err != nil {
			t.Fatal(err)
		}
		if strings.Contains(string(output), "helmDefaults:") {
			t.Fatalf("false shared skipCRDs was emitted:\n%s", output)
		}
	})

	t.Run("conflict", func(t *testing.T) {
		first := minimalApplication("    helm:\n      skipCrds: true\n")
		second := strings.Replace(minimalApplication(""), "name: app", "name: second", 1)
		_, err := convert([]byte(first + "---\n" + second))
		if err == nil || !strings.Contains(err.Error(), "document 2: spec.source.helm.skipCrds conflicts with document 1") {
			t.Fatalf("unexpected error: %v", err)
		}
	})
}

func TestConvertAcceptsDifferentSkipTestsValues(t *testing.T) {
	first := minimalApplication("    helm:\n      skipTests: true\n")
	second := strings.Replace(
		minimalApplication("    helm:\n      skipTests: false\n"),
		"name: app",
		"name: second",
		1,
	)

	output, err := convert([]byte(first + "---\n" + second))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(output), "skipTests") {
		t.Fatalf("skipTests was emitted:\n%s", output)
	}
	for _, name := range []string{"app", "second"} {
		if !strings.Contains(string(output), "  - name: "+name+"\n") {
			t.Errorf("output does not contain release %q:\n%s", name, output)
		}
	}
}

func TestConvertRejectsEmptyDocuments(t *testing.T) {
	tests := map[string]string{
		"only":     "",
		"leading":  "---\n---\n" + minimalApplication(""),
		"middle":   minimalApplication("") + "---\n---\n" + strings.Replace(minimalApplication(""), "name: app", "name: second", 1),
		"trailing": minimalApplication("") + "---\n",
	}
	for name, input := range tests {
		t.Run(name, func(t *testing.T) {
			if output, err := convert([]byte(input)); err == nil {
				t.Fatalf("convert succeeded with output:\n%s", output)
			}
		})
	}
}

func TestConvertReportsFailingDocument(t *testing.T) {
	tests := map[string]string{
		"wrong kind":   minimalApplication("") + "---\nkind: ConfigMap\n",
		"invalid YAML": minimalApplication("") + "---\ninvalid: [\n",
		"duplicate key": minimalApplication("") + `---
apiVersion: argoproj.io/v1alpha1
apiVersion: argoproj.io/v1alpha1
kind: Application
`,
	}
	for name, input := range tests {
		t.Run(name, func(t *testing.T) {
			_, err := convert([]byte(input))
			if err == nil || !strings.HasPrefix(err.Error(), "document 2: ") {
				t.Fatalf("unexpected error: %v", err)
			}
		})
	}
}

func TestConvertDoesNotSplitDocumentMarkerInBlockScalar(t *testing.T) {
	input := minimalApplication(`    helm:
      values: |
        message: |
          ---
`)
	output, err := convert([]byte(input))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(output), "      - message: |\n          ---\n") {
		t.Fatalf("block scalar was not preserved:\n%s", output)
	}
}
