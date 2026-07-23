package main

import (
	"bytes"
	"slices"
	"strings"
	"testing"

	"github.com/goccy/go-yaml"
)

func TestConvertApplicationSetList(t *testing.T) {
	input := readTestdata(t, "applicationset/list/application.yaml")
	output, err := convert([]byte(input))
	if err != nil {
		t.Fatal(err)
	}
	want := readTestdata(t, "applicationset/list/helmfile.yaml")
	if string(output) != want {
		t.Fatalf("unexpected output:\n%s\nwant:\n%s", output, want)
	}
}

func TestConvertApplicationSetGeneratorTemplateOverridesAndInherits(t *testing.T) {
	input := readTestdata(t, "applicationset/generator-template/application.yaml")
	output, err := convert([]byte(input))
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		"    namespace: generated\n",
		"    chart: source/chart\n",
		"    version: 2.0.0\n",
	} {
		if !strings.Contains(string(output), want) {
			t.Errorf("output does not contain %q:\n%s", want, output)
		}
	}
}

func TestConvertApplicationSetIgnoresDifferentSkipTestsValues(t *testing.T) {
	input := `apiVersion: argoproj.io/v1alpha1
kind: ApplicationSet
metadata:
  name: apps
spec:
  goTemplate: true
  generators:
    - list:
        elements:
          - name: tests-enabled
            skipTests: true
          - name: tests-disabled
            skipTests: false
  template:
    metadata:
      name: '{{ .name }}'
    spec:
      source:
        repoURL: https://example.com/charts
        chart: chart
        targetRevision: 1.0.0
        helm:
          skipTests: '{{ .skipTests }}'
`
	output, err := convert([]byte(input))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(output), "skipTests") {
		t.Fatalf("skipTests was emitted:\n%s", output)
	}
	for _, name := range []string{"tests-enabled", "tests-disabled"} {
		if !strings.Contains(string(output), "  - name: "+name+"\n") {
			t.Errorf("output does not contain release %q:\n%s", name, output)
		}
	}
}

func TestConvertApplicationSetTemplateFunctions(t *testing.T) {
	input := `apiVersion: argoproj.io/v1alpha1
kind: ApplicationSet
metadata:
  name: apps
spec:
  goTemplate: true
  goTemplateOptions: [missingkey=error]
  generators:
    - list:
        elements:
          - raw: Hello Wide World
  template:
    metadata:
      name: '{{ .raw | slugify 12 }}'
    spec:
      source:
        repoURL: https://example.com/charts
        chart: '{{ (fromYaml "chart: parsed").chart }}'
        targetRevision: '{{ default "1.0.0" .version }}'
`
	output, err := convert([]byte(input))
	if err == nil || !strings.Contains(err.Error(), `map has no entry for key "version"`) {
		t.Fatalf("default unexpectedly bypassed missingkey=error: output=%s error=%v", output, err)
	}

	input = strings.Replace(input, `{{ default "1.0.0" .version }}`, "1.0.0", 1)
	output, err = convert([]byte(input))
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		"  - name: hello-wide\n",
		"    chart: source/parsed\n",
	} {
		if !strings.Contains(string(output), want) {
			t.Errorf("output does not contain %q:\n%s", want, output)
		}
	}
}

func TestApplicationSetSelectorOperators(t *testing.T) {
	tests := []struct {
		name       string
		expression string
		want       []string
	}{
		{
			name: "In",
			expression: `          - key: metadata.environment
            operator: In
            values: [prod]
`,
			want: []string{"prod"},
		},
		{
			name: "NotIn includes missing",
			expression: `          - key: metadata.environment
            operator: NotIn
            values: [dev]
`,
			want: []string{"prod", "missing"},
		},
		{
			name: "Exists",
			expression: `          - key: metadata.environment
            operator: Exists
`,
			want: []string{"dev", "prod"},
		},
		{
			name: "DoesNotExist",
			expression: `          - key: metadata.environment
            operator: DoesNotExist
`,
			want: []string{"missing"},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			input := `apiVersion: argoproj.io/v1alpha1
kind: ApplicationSet
metadata:
  name: apps
spec:
  goTemplate: true
  generators:
    - list:
        elements:
          - name: dev
            metadata: {environment: dev}
          - name: prod
            metadata: {environment: prod}
          - name: missing
      selector:
        matchExpressions:
` + test.expression + `  template:
    metadata:
      name: '{{ .name }}'
    spec:
      source:
        repoURL: https://example.com/charts
        chart: chart
        targetRevision: 1.0.0
`
			output, err := convert([]byte(input))
			if err != nil {
				t.Fatal(err)
			}
			for _, name := range []string{"dev", "prod", "missing"} {
				contains := strings.Contains(string(output), "  - name: "+name+"\n")
				if contains != slices.Contains(test.want, name) {
					t.Errorf("release %q presence = %v, want %v:\n%s", name, contains, !contains, output)
				}
			}
		})
	}
}

func TestConvertMixedApplicationsAndApplicationSets(t *testing.T) {
	first := strings.Replace(minimalApplication(""), "name: app", "name: first", 1)
	appSet := `apiVersion: argoproj.io/v1alpha1
kind: ApplicationSet
metadata:
  name: generated
spec:
  goTemplate: true
  generators:
    - list:
        elements:
          - name: second
          - name: third
  template:
    metadata:
      name: '{{ .name }}'
    spec:
      source:
        repoURL: https://example.com/charts
        chart: chart
        targetRevision: 1.2.3
`
	input := first + "---\n" + appSet
	output, err := convert([]byte(input))
	if err != nil {
		t.Fatal(err)
	}
	firstIndex := strings.Index(string(output), "  - name: first\n")
	secondIndex := strings.Index(string(output), "  - name: second\n")
	thirdIndex := strings.Index(string(output), "  - name: third\n")
	if firstIndex < 0 || secondIndex <= firstIndex || thirdIndex <= secondIndex {
		t.Fatalf("releases do not preserve expanded input order:\n%s", output)
	}
}

func TestConvertApplicationSetGitChartWithSourceMap(t *testing.T) {
	const repoURL = "git@github.com:example/charts.git"
	resolver := testSourceResolver(t, testSource{
		repoURL: repoURL, targetRevision: "main", root: `{{ requiredEnv "CHARTS_ROOT" }}`,
	})
	input := `apiVersion: argoproj.io/v1alpha1
kind: ApplicationSet
metadata:
  name: apps
spec:
  goTemplate: true
  generators:
    - list:
        elements:
          - name: frontend
            directory: frontend
  template:
    metadata:
      name: '{{ .name }}'
    spec:
      source:
        repoURL: ` + repoURL + `
        path: 'charts/{{ .directory }}'
        targetRevision: main
        helm:
          valueFiles:
            - environments/prod.yaml
`
	output, err := convertWithSourceMap([]byte(input), resolver)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		`# document 1 chart source: repoURL "` + repoURL + `", path "charts/frontend", targetRevision "main"`,
		`chart: '{{ requiredEnv "CHARTS_ROOT" }}/charts/frontend'`,
		`'{{ requiredEnv "CHARTS_ROOT" }}/charts/frontend/environments/prod.yaml'`,
	} {
		if !strings.Contains(string(output), want) {
			t.Errorf("output does not contain %q:\n%s", want, output)
		}
	}
}

func TestConvertApplicationSetDuplicateReleaseReportsOrigins(t *testing.T) {
	direct := minimalApplication("")
	appSet := `apiVersion: argoproj.io/v1alpha1
kind: ApplicationSet
metadata:
  name: apps
spec:
  goTemplate: true
  generators:
    - list:
        elements:
          - name: app
  template:
    metadata:
      name: '{{ .name }}'
    spec:
      source:
        repoURL: https://example.com/charts
        chart: chart
        targetRevision: 1.0.0
`
	_, err := convert([]byte(direct + "---\n" + appSet))
	want := `document 2: spec.generators[0].list.elements[0]: release name "app" duplicates document 1`
	if err == nil || !strings.Contains(err.Error(), want) {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestConvertEmptyApplicationSet(t *testing.T) {
	input := `apiVersion: argoproj.io/v1alpha1
kind: ApplicationSet
metadata:
  name: empty
spec:
  goTemplate: true
  generators:
    - list: {}
  template:
    metadata:
      name: unused
`
	output, err := convert([]byte(input))
	if err != nil {
		t.Fatal(err)
	}
	if string(output) != "releases: []\n" {
		t.Fatalf("unexpected output: %s", output)
	}
}

func TestConvertApplicationSetErrors(t *testing.T) {
	valid := readTestdata(t, "applicationset/errors-base/application.yaml")
	tests := map[string]struct {
		input string
		want  string
	}{
		"fasttemplate": {
			input: strings.Replace(valid, "  goTemplate: true\n", "", 1),
			want:  "spec.goTemplate must be true",
		},
		"unsupported generator": {
			input: strings.Replace(valid, "    - list:\n", "    - clusters: {}\n      list:\n", 1),
			want:  "spec.generators[0].clusters generator is not supported",
		},
		"template patch": {
			input: strings.Replace(valid, "  template:\n", "  templatePatch: '{}'\n  template:\n", 1),
			want:  "spec.templatePatch is not supported",
		},
		"invalid option": {
			input: strings.Replace(valid, "  generators:\n", "  goTemplateOptions: [missingkey=wat]\n  generators:\n", 1),
			want:  `unsupported option "missingkey=wat"`,
		},
		"non-mapping element": {
			input: strings.Replace(valid, "          - name: app\n", "          - app\n", 1),
			want:  "spec.generators[0].list.elements[0]: must be a mapping",
		},
		"missing template value": {
			input: strings.Replace(
				strings.Replace(valid, "  generators:\n", "  goTemplateOptions: [missingkey=error]\n  generators:\n", 1),
				"{{ .name }}",
				"{{ .missing }}",
				1,
			),
			want: "spec.generators[0].list.elements[0]: render template",
		},
		"generated application conversion": {
			input: strings.Replace(valid, "        chart: chart\n", "", 1),
			want:  "spec.generators[0].list.elements[0]: spec.source.chart is required",
		},
	}
	for name, test := range tests {
		t.Run(name, func(t *testing.T) {
			_, err := convert([]byte(test.input))
			if err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("unexpected error: %v", err)
			}
		})
	}
}

func TestRunApplicationSetErrorIsAtomic(t *testing.T) {
	input := `apiVersion: argoproj.io/v1alpha1
kind: ApplicationSet
metadata:
  name: apps
spec:
  goTemplate: true
  goTemplateOptions: [missingkey=error]
  generators:
    - list:
        elements:
          - name: first
          - name: second
  template:
    metadata:
      name: '{{ .missing }}'
`
	var stdout, stderr bytes.Buffer
	if code := run(nil, strings.NewReader(input), &stdout, &stderr); code != 1 {
		t.Fatalf("exit code = %d, want 1", code)
	}
	if stdout.Len() != 0 {
		t.Fatalf("stdout was not empty: %q", stdout.String())
	}
	if !strings.Contains(stderr.String(), "spec.generators[0].list.elements[0]") {
		t.Fatalf("stderr does not identify generated element: %q", stderr.String())
	}
}

func TestRenderApplicationSetTemplatesMappingKeys(t *testing.T) {
	renderer, err := newApplicationSetTemplate([]string{"missingkey=error"})
	if err != nil {
		t.Fatal(err)
	}
	rendered, err := renderTemplateValue(yaml.MapSlice{
		{Key: "{{ .key }}", Value: "{{ .value }}"},
	}, map[string]any{"key": "rendered", "value": "content"}, renderer)
	if err != nil {
		t.Fatal(err)
	}
	items := rendered.(yaml.MapSlice)
	if items[0].Key != "rendered" || items[0].Value != "content" {
		t.Fatalf("unexpected rendered map: %#v", items)
	}
}
