package main

import (
	"bytes"
	"strings"
	"testing"

	"github.com/goccy/go-yaml"
)

const exampleApplication = `apiVersion: argoproj.io/v1alpha1
kind: Application
metadata:
  name: nginx
spec:
  destination:
    namespace: web
    server: https://kubernetes.default.svc
  source:
    repoURL: https://charts.bitnami.com/bitnami
    chart: nginx
    targetRevision: 18.2.4
    helm:
      releaseName: edge
      values: |
        service:
          type: ClusterIP
      valuesObject:
        service:
          annotations:
            example.com/owner: platform
      parameters:
        - name: replicaCount
          value: "2"
`

func TestConvertExample(t *testing.T) {
	output, err := convert([]byte(exampleApplication))
	if err != nil {
		t.Fatal(err)
	}
	want := `repositories:
  - name: source
    url: https://charts.bitnami.com/bitnami
releases:
  - name: edge
    namespace: web
    chart: source/nginx
    version: 18.2.4
    values:
      - service:
          type: ClusterIP
      - service:
          annotations:
            example.com/owner: platform
    set:
      - name: replicaCount
        value: "2"
`
	if string(output) != want {
		t.Fatalf("unexpected output:\n%s\nwant:\n%s", output, want)
	}
}

func TestConvertDefaultsAndOmitsEmptyFields(t *testing.T) {
	input := minimalApplication(`    helm:
      releaseName: ""
      values: ""
      valuesObject: {}
      parameters: []
      valueFiles: []
      skipCrds: false
`)
	output, err := convert([]byte(input))
	if err != nil {
		t.Fatal(err)
	}
	text := string(output)
	for _, want := range []string{"- name: app\n", "chart: source/chart\n", "version: 1.2.3\n"} {
		if !strings.Contains(text, want) {
			t.Errorf("output does not contain %q:\n%s", want, text)
		}
	}
	for _, omitted := range []string{"namespace:", "values:", "set:", "forceString:"} {
		if strings.Contains(text, omitted) {
			t.Errorf("output unexpectedly contains %q:\n%s", omitted, text)
		}
	}
}

func TestConvertPreservesValuesTypesOrderAndPrecedence(t *testing.T) {
	input := minimalApplication(`    helm:
      values: |
        z: 1
        enabled: true
        nothing: null
        items: [one, 2]
        nested:
          b: 2
          a: 1
      valuesObject:
        z: 3
        ratio: 1.5
`)
	output, err := convert([]byte(input))
	if err != nil {
		t.Fatal(err)
	}
	text := string(output)
	ordered := []string{"      - z: 1", "        enabled: true", "        nothing: null", "        items:", "        nested:", "          b: 2", "          a: 1", "      - z: 3", "        ratio: 1.5"}
	position := -1
	for _, fragment := range ordered {
		next := strings.Index(text[position+1:], fragment)
		if next < 0 {
			t.Fatalf("output does not contain %q after prior fields:\n%s", fragment, text)
		}
		position += next + 1
	}

	var decoded any
	if err := yaml.NewDecoder(bytes.NewReader(output), yaml.UseOrderedMap()).Decode(&decoded); err != nil {
		t.Fatalf("generated output is not YAML: %v", err)
	}
}

func TestConvertParameters(t *testing.T) {
	input := minimalApplication(`    helm:
      parameters:
        - name: image.tag
          value: "001"
          forceString: true
        - name: replicaCount
          value: "3"
          forceString: false
`)
	output, err := convert([]byte(input))
	if err != nil {
		t.Fatal(err)
	}
	want := `    set:
      - name: image.tag
        value: "001"
        forceString: true
      - name: replicaCount
        value: "3"
`
	if !strings.Contains(string(output), want) {
		t.Fatalf("parameters not preserved:\n%s", output)
	}
	if strings.Count(string(output), "forceString:") != 1 {
		t.Fatalf("false forceString was not omitted:\n%s", output)
	}
}

func TestConvertInlineScalar(t *testing.T) {
	input := minimalApplication("    helm:\n      values: '42'\n")
	output, err := convert([]byte(input))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(output), "    values:\n      - 42\n") {
		t.Fatalf("scalar inline value was not retained:\n%s", output)
	}
}

func TestConvertRejectsInvalidInput(t *testing.T) {
	tests := map[string]string{
		"empty":                    "",
		"invalid YAML":             "apiVersion: [",
		"duplicate key":            strings.Replace(exampleApplication, "kind: Application", "kind: Application\nkind: Application", 1),
		"multiple documents":       exampleApplication + "---\nfoo: bar\n",
		"trailing empty document":  exampleApplication + "---\n",
		"wrong apiVersion":         strings.Replace(exampleApplication, "argoproj.io/v1alpha1", "v1", 1),
		"wrong kind":               strings.Replace(exampleApplication, "kind: Application", "kind: List", 1),
		"missing name":             strings.Replace(exampleApplication, "name: nginx", "name: ''", 1),
		"missing repoURL":          strings.Replace(exampleApplication, "https://charts.bitnami.com/bitnami", "", 1),
		"invalid repoURL":          strings.Replace(exampleApplication, "https://charts.bitnami.com/bitnami", "https:///missing-host", 1),
		"OCI repoURL":              strings.Replace(exampleApplication, "https://charts.bitnami.com/bitnami", "oci://registry.example.com/charts", 1),
		"missing chart":            strings.Replace(exampleApplication, "chart: nginx", "chart: ''", 1),
		"missing revision":         strings.Replace(exampleApplication, "targetRevision: 18.2.4", "targetRevision: ''", 1),
		"path":                     strings.Replace(exampleApplication, "chart: nginx", "chart: nginx\n    path: charts/nginx", 1),
		"sources":                  strings.Replace(exampleApplication, "  source:\n", "  sources: []\n  source:\n", 1),
		"valueFiles":               strings.Replace(exampleApplication, "      releaseName: edge", "      releaseName: edge\n      valueFiles: [values.yaml]", 1),
		"fileParameters":           strings.Replace(exampleApplication, "      releaseName: edge", "      releaseName: edge\n      fileParameters: [{name: x, path: x}]", 1),
		"unknown Helm option":      strings.Replace(exampleApplication, "      releaseName: edge", "      releaseName: edge\n      skipCrds: true", 1),
		"invalid inline values":    strings.Replace(exampleApplication, "        service:\n          type: ClusterIP", "        invalid: [", 1),
		"multi-doc values":         strings.Replace(exampleApplication, "        service:\n          type: ClusterIP", "        one: 1\n        ---\n        two: 2", 1),
		"trailing values document": strings.Replace(exampleApplication, "        service:\n          type: ClusterIP", "        one: 1\n        ---", 1),
		"non-string parameter":     strings.Replace(exampleApplication, "value: \"2\"", "value: 2", 1),
	}
	for name, input := range tests {
		t.Run(name, func(t *testing.T) {
			if output, err := convert([]byte(input)); err == nil {
				t.Fatalf("convert succeeded with output:\n%s", output)
			}
		})
	}
}

func TestRunErrorsAreAtomic(t *testing.T) {
	tests := []struct {
		name  string
		args  []string
		input string
	}{
		{name: "conversion", input: "invalid: ["},
		{name: "argument", args: []string{"unexpected"}, input: exampleApplication},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			var stdout, stderr bytes.Buffer
			if code := run(test.args, strings.NewReader(test.input), &stdout, &stderr); code != 1 {
				t.Fatalf("exit code = %d, want 1", code)
			}
			if stdout.Len() != 0 {
				t.Fatalf("stdout was not empty: %q", stdout.String())
			}
			if !strings.HasPrefix(stderr.String(), "argocdapp2helmfile: ") || strings.Count(stderr.String(), "\n") != 1 {
				t.Fatalf("stderr is not one diagnostic line: %q", stderr.String())
			}
		})
	}
}

func minimalApplication(helm string) string {
	return `apiVersion: argoproj.io/v1alpha1
kind: Application
metadata:
  name: app
spec:
  source:
    repoURL: https://example.com/charts
    chart: chart
    targetRevision: 1.2.3
` + helm
}
