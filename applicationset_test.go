package main

import (
	"bytes"
	"reflect"
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
		"    chart: charts/chart\n",
		"    version: 2.0.0\n",
	} {
		if !strings.Contains(string(output), want) {
			t.Errorf("output does not contain %q:\n%s", want, output)
		}
	}
}

func TestConvertApplicationSetIgnoresDifferentSkipTestsValues(t *testing.T) {
	input := readTestdata(t, "applicationset/skip-tests/application.yaml")
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
	input := readTestdata(t, "applicationset/template-functions/application.yaml")
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
		"    chart: charts/parsed\n",
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
	appSet := readTestdata(t, "applicationset/mixed/application-set.yaml")
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

func TestConvertApplicationSetGitChartWithConfig(t *testing.T) {
	const repoURL = "git@github.com:example/charts.git"
	resolver := testSourceResolver(t, testSource{
		repoURL: repoURL, targetRevision: "main", root: `{{ requiredEnv "CHARTS_ROOT" }}`,
	})
	input := readTestdata(t, "applicationset/git-chart-with-config/application.yaml")
	output, err := convertWithResolver([]byte(input), resolver)
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
	appSet := readTestdata(t, "applicationset/minimal/application.yaml")
	_, err := convert([]byte(direct + "---\n" + appSet))
	want := `document 2: spec.generators[0].list.elements[0]: release name "app" duplicates document 1`
	if err == nil || !strings.Contains(err.Error(), want) {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestConvertEmptyApplicationSet(t *testing.T) {
	input := readTestdata(t, "applicationset/empty/application.yaml")
	output, err := convert([]byte(input))
	if err != nil {
		t.Fatal(err)
	}
	if string(output) != "releases: []\n" {
		t.Fatalf("unexpected output: %s", output)
	}
}

func TestConvertApplicationSetErrors(t *testing.T) {
	valid := readTestdata(t, "applicationset/minimal/application.yaml")
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

func TestConvertApplicationSetTemplatePatchYAMLAndJSON(t *testing.T) {
	const yamlPatch = `metadata:
  labels:
    environment: '{{ .environment }}'
  annotations:
    owner: '{{ .owner }}'
spec:
  destination:
    namespace: '{{ .namespace }}'
  source:
    chart: '{{ .chart }}'
    targetRevision: '{{ .version }}'
    helm:
      parameters:
        - name: image.tag
          value: '{{ .tag }}'
      valuesObject:
        replicaCount: '{{ .replicas }}'
`
	const jsonPatch = `{"metadata":{"labels":{"environment":"{{ .environment }}"},"annotations":{"owner":"{{ .owner }}"}},"spec":{"destination":{"namespace":"{{ .namespace }}"},"source":{"chart":"{{ .chart }}","targetRevision":"{{ .version }}","helm":{"parameters":[{"name":"image.tag","value":"{{ .tag }}"}],"valuesObject":{"replicaCount":"{{ .replicas }}"}}}}}`
	config := testConfig(t, `releaseLabels:
  - name: environment
    query: .metadata.labels.environment
  - name: owner
    query: .metadata.annotations.owner
`)

	var outputs [][]byte
	for _, patch := range []string{yamlPatch, jsonPatch} {
		input := applicationSetWithTemplatePatch(patch, `          - name: app
            environment: production
            owner: platform
            namespace: workloads
            chart: patched-chart
            version: 2.0.0
            tag: "001"
            replicas: "3"
`)
		output, err := convertWithConfig([]byte(input), config)
		if err != nil {
			t.Fatal(err)
		}
		outputs = append(outputs, output)
	}
	if !bytes.Equal(outputs[0], outputs[1]) {
		t.Fatalf("YAML and JSON patches differ:\nYAML:\n%s\nJSON:\n%s", outputs[0], outputs[1])
	}
	for _, want := range []string{
		"    labels:\n      environment: production\n      owner: platform\n",
		"    namespace: workloads\n",
		"    chart: charts/patched-chart\n",
		"    version: 2.0.0\n",
		"      - replicaCount: \"3\"\n",
		"        value: \"001\"\n",
	} {
		if !strings.Contains(string(outputs[0]), want) {
			t.Errorf("output does not contain %q:\n%s", want, outputs[0])
		}
	}
}

func TestConvertApplicationSetTemplatePatchConditionalAndGeneratorTemplateOrder(t *testing.T) {
	const patch = `{{- if .patch }}
spec:
  destination:
    namespace: patched
  source:
    chart: patched-chart
{{- end }}
`
	input := applicationSetWithTemplatePatch(patch, `          - name: enabled
            patch: true
          - name: disabled
            patch: false
        template:
          spec:
            destination:
              namespace: generator
            source:
              chart: generator-chart
`)
	output, err := convert([]byte(input))
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		"  - name: enabled\n    namespace: patched\n    chart: charts/patched-chart\n",
		"  - name: disabled\n    namespace: generator\n    chart: charts/generator-chart\n",
	} {
		if !strings.Contains(string(output), want) {
			t.Errorf("output does not contain %q:\n%s", want, output)
		}
	}
}

func TestConvertApplicationSetTemplatePatchValueFiles(t *testing.T) {
	const repoURL = "git@github.com:example/charts.git"
	resolver := testSourceResolver(t, testSource{
		repoURL: repoURL, targetRevision: "main", root: "/workspace/charts",
	})
	input := readTestdata(t, "applicationset/git-chart-with-config/application.yaml")
	input = strings.Replace(input, "  generators:\n", `  templatePatch: |
    spec:
      source:
        helm:
          valueFiles:
            - environments/{{ .directory }}.yaml
  generators:
`, 1)
	output, err := convertWithResolver([]byte(input), resolver)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(output), "      - '/workspace/charts/charts/frontend/environments/frontend.yaml'\n") {
		t.Fatalf("patched valueFiles were not converted:\n%s", output)
	}
	if strings.Contains(string(output), "environments/prod.yaml") {
		t.Fatalf("template valueFiles sequence was not replaced:\n%s", output)
	}
}

func TestApplicationSetTemplatePatchMergeSemantics(t *testing.T) {
	target := yaml.MapSlice{
		{Key: "mapping", Value: yaml.MapSlice{
			{Key: "kept", Value: "old"},
			{Key: "changed", Value: "old"},
			{Key: "removed", Value: "old"},
		}},
		{Key: "sequence", Value: []any{"old", "values"}},
		{Key: "scalar", Value: "old"},
	}
	patch := yaml.MapSlice{
		{Key: "mapping", Value: yaml.MapSlice{
			{Key: "changed", Value: "new"},
			{Key: "removed", Value: nil},
			{Key: "addedFirst", Value: 1},
			{Key: "addedSecond", Value: 2},
		}},
		{Key: "sequence", Value: []any{"replacement"}},
		{Key: "scalar", Value: true},
		{Key: "newMapping", Value: yaml.MapSlice{{Key: "nested", Value: "value"}}},
	}
	got := mergeApplicationTemplatePatch(target, patch)
	want := yaml.MapSlice{
		{Key: "mapping", Value: yaml.MapSlice{
			{Key: "kept", Value: "old"},
			{Key: "changed", Value: "new"},
			{Key: "addedFirst", Value: 1},
			{Key: "addedSecond", Value: 2},
		}},
		{Key: "sequence", Value: []any{"replacement"}},
		{Key: "scalar", Value: true},
		{Key: "newMapping", Value: yaml.MapSlice{{Key: "nested", Value: "value"}}},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("unexpected merge:\n got: %#v\nwant: %#v", got, want)
	}
}

func TestConvertApplicationSetTemplatePatchPreservesProject(t *testing.T) {
	config := testConfig(t, `releaseLabels:
  - name: project
    query: .spec.project
`)
	for name, patch := range map[string]string{
		"replace": "spec:\n  project: changed\n",
		"delete":  "spec:\n  project: null\n",
	} {
		t.Run(name, func(t *testing.T) {
			input := strings.Replace(
				applicationSetWithTemplatePatch(patch, "          - name: app\n"),
				"    spec:\n      destination:\n",
				"    spec:\n      project: original\n      destination:\n",
				1,
			)
			output, err := convertWithConfig([]byte(input), config)
			if err != nil {
				t.Fatal(err)
			}
			if !strings.Contains(string(output), "    labels:\n      project: original\n") {
				t.Fatalf("project was not preserved:\n%s", output)
			}
		})
	}

	input := applicationSetWithTemplatePatch("spec:\n  project: added\n", "          - name: app\n")
	output, err := convertWithConfig([]byte(input), config)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(output), "project:") {
		t.Fatalf("patch added a project absent from the template:\n%s", output)
	}
}

func TestConvertApplicationSetEmptyTemplatePatchIsNoOp(t *testing.T) {
	unpatched := applicationSetWithTemplatePatch("", "          - name: app\n")
	patched := applicationSetWithTemplatePatch("  \n\t\n", "          - name: app\n")
	first, err := convert([]byte(unpatched))
	if err != nil {
		t.Fatal(err)
	}
	second, err := convert([]byte(patched))
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(first, second) {
		t.Fatalf("whitespace patch changed output:\n%s\nwant:\n%s", second, first)
	}
}

func TestConvertApplicationSetTemplatePatchErrors(t *testing.T) {
	tests := map[string]struct {
		patch string
		want  string
	}{
		"invalid Go template": {
			patch: "{{",
			want:  "render spec.templatePatch",
		},
		"invalid YAML": {
			patch: "metadata: [",
			want:  "decode rendered spec.templatePatch",
		},
		"multiple documents": {
			patch: "{}\n---\n{}",
			want:  "must contain exactly one YAML document",
		},
		"non-mapping root": {
			patch: "[]",
			want:  "must contain a mapping",
		},
		"patch directive": {
			patch: "spec:\n  source:\n    helm:\n      parameters:\n        - $patch: replace\n",
			want:  `unsupported Strategic Merge Patch directive "$patch"`,
		},
		"retain keys directive": {
			patch: "spec:\n  $retainKeys: [source]\n",
			want:  `unsupported Strategic Merge Patch directive "$retainKeys"`,
		},
		"set element order directive": {
			patch: "spec:\n  source:\n    helm:\n      $setElementOrder/parameters: []\n",
			want:  `unsupported Strategic Merge Patch directive "$setElementOrder/parameters"`,
		},
		"delete primitive list directive": {
			patch: "spec:\n  source:\n    helm:\n      $deleteFromPrimitiveList/valueFiles: []\n",
			want:  `unsupported Strategic Merge Patch directive "$deleteFromPrimitiveList/valueFiles"`,
		},
		"invalid resulting Application": {
			patch: "spec:\n  source:\n    chart: null\n",
			want:  "spec.source.chart is required",
		},
	}
	for name, test := range tests {
		t.Run(name, func(t *testing.T) {
			input := applicationSetWithTemplatePatch(test.patch, "          - name: app\n")
			_, err := convert([]byte(input))
			if err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("unexpected error: %v", err)
			}
			if !strings.Contains(err.Error(), "document 1: spec.generators[0].list.elements[0]") {
				t.Fatalf("error does not identify the element: %v", err)
			}
		})
	}
}

func TestConvertApplicationSetTemplatePatchMustBeString(t *testing.T) {
	input := strings.Replace(
		applicationSetWithTemplatePatch("", "          - name: app\n"),
		"  templatePatch: |\n",
		"  templatePatch: {}\n",
		1,
	)
	_, err := convert([]byte(input))
	if err == nil || !strings.Contains(err.Error(), "templatePatch") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func applicationSetWithTemplatePatch(patch, elements string) string {
	indentedPatch := strings.ReplaceAll(patch, "\n", "\n    ")
	return `apiVersion: argoproj.io/v1alpha1
kind: ApplicationSet
metadata:
  name: apps
spec:
  goTemplate: true
  goTemplateOptions: [missingkey=error]
  generators:
    - list:
        elements:
` + elements + `  templatePatch: |
    ` + indentedPatch + `
  template:
    metadata:
      name: '{{ .name }}'
    spec:
      destination:
        namespace: default
      source:
        repoURL: https://example.com/charts
        chart: base-chart
        targetRevision: 1.0.0
`
}

func TestRunApplicationSetErrorIsAtomic(t *testing.T) {
	input := readTestdata(t, "applicationset/atomic-error/application.yaml")
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
