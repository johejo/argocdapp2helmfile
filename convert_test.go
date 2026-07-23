package main

import (
	"strings"
	"testing"
)

func TestConvertExample(t *testing.T) {
	input := readTestdata(t, "example/application.yaml")
	config := testConfig(t, `destinations:
  - server: https://kubernetes.default.svc
    kubeContext: in-cluster
`)
	output, err := convertWithConfig([]byte(input), config)
	if err != nil {
		t.Fatal(err)
	}
	want := readTestdata(t, "example/helmfile.yaml")
	if string(output) != want {
		t.Fatalf("unexpected output:\n%s\nwant:\n%s", output, want)
	}
}

func TestConvertDestinationToKubeContext(t *testing.T) {
	config := testConfig(t, `destinations:
  - name: production
    kubeContext: prod-admin
  - server: https://example
    kubeContext: example-admin
`)
	tests := []struct {
		name        string
		destination string
		want        string
	}{
		{name: "name", destination: "    name: production\n", want: "prod-admin"},
		{name: "server", destination: "    server: https://example\n", want: "example-admin"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			input := strings.Replace(
				minimalApplication(""),
				"spec:\n",
				"spec:\n  destination:\n"+test.destination,
				1,
			)
			output, err := convertWithConfig([]byte(input), config)
			if err != nil {
				t.Fatal(err)
			}
			if !strings.Contains(string(output), "    kubeContext: "+test.want+"\n") {
				t.Fatalf("kubeContext was not emitted:\n%s", output)
			}
		})
	}
}

func TestConvertDestinationFailsClosed(t *testing.T) {
	withDestination := func(destination string) string {
		return strings.Replace(
			minimalApplication(""),
			"spec:\n",
			"spec:\n  destination:\n"+destination,
			1,
		)
	}
	tests := []struct {
		name   string
		input  string
		config *conversionConfig
		want   string
	}{
		{
			name:  "config required",
			input: withDestination("    name: production\n"),
			want:  "document 1: spec.destination requires --config",
		},
		{
			name:   "name not mapped",
			input:  withDestination("    name: staging\n"),
			config: testConfig(t, "destinations: []\n"),
			want:   `document 1: spec.destination has no config destination entry for name "staging"`,
		},
		{
			name:   "server not mapped",
			input:  withDestination("    server: https://unknown\n"),
			config: testConfig(t, "destinations: []\n"),
			want:   `document 1: spec.destination has no config destination entry for server "https://unknown"`,
		},
		{
			name:  "name and server",
			input: withDestination("    name: production\n    server: https://example\n"),
			want:  "document 1: spec.destination.name and spec.destination.server cannot both be set",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			_, err := convertWithConfig([]byte(test.input), test.config)
			if err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("unexpected error: %v", err)
			}
		})
	}
}

func TestConvertOmitsKubeContextWithoutDestinationSelector(t *testing.T) {
	input := strings.Replace(
		minimalApplication(""),
		"spec:\n",
		"spec:\n  destination:\n    namespace: default\n",
		1,
	)
	output, err := convert([]byte(input))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(output), "kubeContext:") {
		t.Fatalf("kubeContext was emitted:\n%s", output)
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
	repoURL := "git@github.com:example/charts"
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
	if !strings.Contains(text, "  - name: charts\n    url: https://example.com/charts\n") ||
		!strings.Contains(text, "  - name: packaged\n    chart: charts/chart\n") {
		t.Fatalf("Git repository consumed the HTTP repository alias:\n%s", output)
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
	if strings.Count(text, "  - name: charts\n") != 1 || strings.Count(text, "  - name: charts-2\n") != 1 {
		t.Fatalf("repositories were not deduplicated by exact URL:\n%s", output)
	}
	for _, want := range []string{
		"    chart: charts/chart\n",
		"  - name: shared\n    chart: charts/chart\n",
		"  - name: distinct\n    chart: charts-2/chart\n",
	} {
		if !strings.Contains(text, want) {
			t.Errorf("output does not contain %q:\n%s", want, output)
		}
	}
}

func TestRepositoryAlias(t *testing.T) {
	tests := []struct {
		name          string
		repositoryURL string
		want          string
	}{
		{name: "HTTP", repositoryURL: "https://charts.bitnami.com/bitnami", want: "bitnami"},
		{name: "OCI", repositoryURL: "registry-1.docker.io/bitnamicharts", want: "bitnamicharts"},
		{name: "trailing slash query and fragment", repositoryURL: "https://example.com/Stable_Charts/?channel=prod#readme", want: "stable-charts"},
		{name: "unsafe and percent encoded characters", repositoryURL: "https://example.com/%E3%83%81%E3%83%A3%E3%83%BC%E3%83%88%20Repo!", want: "repo"},
		{name: "encoded slash stays in final segment", repositoryURL: "https://example.com/team%2Fcharts", want: "team-charts"},
		{name: "root URL", repositoryURL: "https://example.com/", want: "source"},
		{name: "empty normalized segment", repositoryURL: "https://example.com/%E3%83%81%E3%83%A3%E3%83%BC%E3%83%88", want: "source"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := repositoryAlias(test.repositoryURL); got != test.want {
				t.Fatalf("repositoryAlias(%q) = %q, want %q", test.repositoryURL, got, test.want)
			}
		})
	}
}

func TestConvertRepositoryAliasCollisions(t *testing.T) {
	app := func(name, repositoryURL string) string {
		return strings.NewReplacer(
			"name: app", "name: "+name,
			"https://example.com/charts", repositoryURL,
		).Replace(minimalApplication(""))
	}
	input := strings.Join([]string{
		app("first", "https://one.example/charts"),
		app("reserved", "https://two.example/charts-2"),
		app("second", "https://three.example/charts"),
	}, "---\n")

	output, err := convert([]byte(input))
	if err != nil {
		t.Fatal(err)
	}
	text := string(output)
	for _, want := range []string{
		"  - name: charts\n    url: https://one.example/charts\n",
		"  - name: charts-2\n    url: https://two.example/charts-2\n",
		"  - name: charts-3\n    url: https://three.example/charts\n",
		"  - name: first\n    chart: charts/chart\n",
		"  - name: reserved\n    chart: charts-2/chart\n",
		"  - name: second\n    chart: charts-3/chart\n",
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

func TestConvertAggregatesRepositoryPassCredentials(t *testing.T) {
	app := func(name, repositoryURL, helm string) string {
		return strings.NewReplacer(
			"name: app", "name: "+name,
			"https://example.com/charts", repositoryURL,
		).Replace(minimalApplication(helm))
	}

	t.Run("true and true", func(t *testing.T) {
		input := app("first", "https://example.com/charts", "    helm:\n      passCredentials: true\n") +
			"---\n" +
			app("second", "https://example.com/charts", "    helm:\n      passCredentials: true\n")
		output, err := convert([]byte(input))
		if err != nil {
			t.Fatal(err)
		}
		if strings.Count(string(output), "  - name: charts\n") != 1 ||
			strings.Count(string(output), "    passCredentials: true\n") != 1 {
			t.Fatalf("repository was not shared with passCredentials:\n%s", output)
		}
	})

	t.Run("false and absent", func(t *testing.T) {
		input := app("first", "https://example.com/charts", "    helm:\n      passCredentials: false\n") +
			"---\n" +
			app("second", "https://example.com/charts", "")
		output, err := convert([]byte(input))
		if err != nil {
			t.Fatal(err)
		}
		if strings.Count(string(output), "  - name: charts\n") != 1 {
			t.Fatalf("repository was not shared:\n%s", output)
		}
		if strings.Contains(string(output), "passCredentials:") {
			t.Fatalf("false passCredentials was emitted:\n%s", output)
		}
	})

	for _, secondHelm := range []string{"", "    helm:\n      passCredentials: false\n"} {
		t.Run("conflict", func(t *testing.T) {
			input := app("first", "https://example.com/charts", "    helm:\n      passCredentials: true\n") +
				"---\n" +
				app("second", "https://example.com/charts", secondHelm)
			_, err := convert([]byte(input))
			if err == nil || !strings.Contains(
				err.Error(),
				"document 2: spec.source.helm.passCredentials conflicts with document 1",
			) {
				t.Fatalf("unexpected error: %v", err)
			}
		})
	}

	t.Run("different URLs", func(t *testing.T) {
		input := app("first", "https://one.example/charts", "    helm:\n      passCredentials: true\n") +
			"---\n" +
			app("second", "https://two.example/charts", "")
		output, err := convert([]byte(input))
		if err != nil {
			t.Fatal(err)
		}
		if strings.Count(string(output), "passCredentials: true") != 1 ||
			strings.Count(string(output), "url: https://") != 2 {
			t.Fatalf("different repository settings were not retained:\n%s", output)
		}
	})
}

func TestConvertOCIPassCredentials(t *testing.T) {
	input := strings.Replace(
		minimalApplication("    helm:\n      passCredentials: true\n"),
		"https://example.com/charts",
		"registry.example.com/charts",
		1,
	)
	output, err := convert([]byte(input))
	if err != nil {
		t.Fatal(err)
	}
	want := "    url: registry.example.com/charts\n    oci: true\n    passCredentials: true\n"
	if !strings.Contains(string(output), want) {
		t.Fatalf("OCI passCredentials was not emitted:\n%s", output)
	}

	second := strings.Replace(input, "name: app", "name: second", 1)
	second = strings.Replace(second, "      passCredentials: true\n", "", 1)
	_, err = convert([]byte(input + "---\n" + second))
	if err == nil || !strings.Contains(err.Error(), "spec.source.helm.passCredentials conflicts with document 1") {
		t.Fatalf("unexpected OCI conflict error: %v", err)
	}
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
