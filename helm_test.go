package main

import (
	"bytes"
	"strings"
	"testing"

	"github.com/goccy/go-yaml"
)

func TestConvertDefaultsAndOmitsEmptyFields(t *testing.T) {
	input := minimalApplication(`    helm:
      releaseName: ""
      values: ""
      valuesObject: {}
      parameters: []
      valueFiles: []
      skipCrds: false
      skipSchemaValidation: false
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
	for _, omitted := range []string{"namespace:", "values:", "set:", "setString:", "forceString:", "missingFileHandler:", "helmDefaults:", "skipCRDs:", "skipSchemaValidation:"} {
		if strings.Contains(text, omitted) {
			t.Errorf("output unexpectedly contains %q:\n%s", omitted, text)
		}
	}
}

func TestConvertHelmBooleanOptionsIndependently(t *testing.T) {
	tests := []struct {
		name    string
		helm    string
		present string
		absent  string
	}{
		{
			name:    "skip schema validation",
			helm:    "    helm:\n      skipSchemaValidation: true\n",
			present: "    skipSchemaValidation: true\n",
			absent:  "helmDefaults:\n",
		},
		{
			name:    "skip CRDs",
			helm:    "    helm:\n      skipCrds: true\n",
			present: "helmDefaults:\n  skipCRDs: true\n",
			absent:  "    skipSchemaValidation: true\n",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			output, err := convert([]byte(minimalApplication(test.helm)))
			if err != nil {
				t.Fatal(err)
			}
			if !strings.Contains(string(output), test.present) {
				t.Fatalf("output does not contain %q:\n%s", test.present, output)
			}
			if strings.Contains(string(output), test.absent) {
				t.Fatalf("output unexpectedly contains %q:\n%s", test.absent, output)
			}
		})
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
        - name: service.port
          value: "8080"
        - name: image.digest
          value: sha256:abc
          forceString: true
`)
	output, err := convert([]byte(input))
	if err != nil {
		t.Fatal(err)
	}
	want := `    set:
      - name: replicaCount
        value: "3"
      - name: service.port
        value: "8080"
    setString:
      - name: image.tag
        value: "001"
      - name: image.digest
        value: sha256:abc
`
	if !strings.Contains(string(output), want) {
		t.Fatalf("parameters not partitioned into set and setString:\n%s", output)
	}
	if strings.Contains(string(output), "forceString:") {
		t.Fatalf("forceString was emitted:\n%s", output)
	}
}

func TestConvertOmitsEmptyParameterGroups(t *testing.T) {
	tests := []struct {
		name    string
		helm    string
		present string
		absent  string
	}{
		{
			name: "set only",
			helm: `    helm:
      parameters:
        - name: replicaCount
          value: "3"
`,
			present: "    set:\n",
			absent:  "    setString:\n",
		},
		{
			name: "setString only",
			helm: `    helm:
      parameters:
        - name: image.tag
          value: "001"
          forceString: true
`,
			present: "    setString:\n",
			absent:  "    set:\n",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			input := minimalApplication(test.helm)
			output, err := convert([]byte(input))
			if err != nil {
				t.Fatal(err)
			}
			if !strings.Contains(string(output), test.present) {
				t.Fatalf("output does not contain %q:\n%s", test.present, output)
			}
			if strings.Contains(string(output), test.absent) {
				t.Fatalf("output unexpectedly contains %q:\n%s", test.absent, output)
			}
		})
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
		"empty":                       "",
		"invalid YAML":                "apiVersion: [",
		"duplicate key":               strings.Replace(exampleApplication, "kind: Application", "kind: Application\nkind: Application", 1),
		"multiple documents":          exampleApplication + "---\nfoo: bar\n",
		"trailing empty document":     exampleApplication + "---\n",
		"wrong apiVersion":            strings.Replace(exampleApplication, "argoproj.io/v1alpha1", "v1", 1),
		"wrong kind":                  strings.Replace(exampleApplication, "kind: Application", "kind: List", 1),
		"missing name":                strings.Replace(exampleApplication, "name: nginx", "name: ''", 1),
		"missing repoURL":             strings.Replace(exampleApplication, "https://charts.bitnami.com/bitnami", "", 1),
		"invalid repoURL":             strings.Replace(exampleApplication, "https://charts.bitnami.com/bitnami", "https:///missing-host", 1),
		"OCI repoURL with scheme":     strings.Replace(exampleApplication, "https://charts.bitnami.com/bitnami", "oci://registry.example.com/charts", 1),
		"unknown repoURL scheme":      strings.Replace(exampleApplication, "https://charts.bitnami.com/bitnami", "ftp://example.com/charts", 1),
		"OCI repoURL with query":      strings.Replace(exampleApplication, "https://charts.bitnami.com/bitnami", "registry.example.com/charts?channel=stable", 1),
		"OCI repoURL with fragment":   strings.Replace(exampleApplication, "https://charts.bitnami.com/bitnami", `"registry.example.com/charts#stable"`, 1),
		"OCI repoURL with userinfo":   strings.Replace(exampleApplication, "https://charts.bitnami.com/bitnami", "user@registry.example.com/charts", 1),
		"OCI repoURL with whitespace": strings.Replace(exampleApplication, "https://charts.bitnami.com/bitnami", `"registry.example.com/bad charts"`, 1),
		"missing chart":               strings.Replace(exampleApplication, "chart: nginx", "chart: ''", 1),
		"missing revision":            strings.Replace(exampleApplication, "targetRevision: 18.2.4", "targetRevision: ''", 1),
		"path":                        strings.Replace(exampleApplication, "chart: nginx", "chart: nginx\n    path: charts/nginx", 1),
		"sources":                     strings.Replace(exampleApplication, "  source:\n", "  sources: []\n  source:\n", 1),
		"fileParameters":              strings.Replace(exampleApplication, "      releaseName: edge", "      releaseName: edge\n      fileParameters: [{name: x, path: x}]", 1),
		"unknown Helm option":         strings.Replace(exampleApplication, "      releaseName: edge", "      releaseName: edge\n      unsupportedOption: true", 1),
		"non-boolean skipCrds":        strings.Replace(exampleApplication, "      skipCrds: true", "      skipCrds: enabled", 1),
		"non-boolean schema skip":     strings.Replace(exampleApplication, "      skipSchemaValidation: true", "      skipSchemaValidation: 1", 1),
		"invalid inline values":       strings.Replace(exampleApplication, "        service:\n          type: ClusterIP", "        invalid: [", 1),
		"multi-doc values":            strings.Replace(exampleApplication, "        service:\n          type: ClusterIP", "        one: 1\n        ---\n        two: 2", 1),
		"trailing values document":    strings.Replace(exampleApplication, "        service:\n          type: ClusterIP", "        one: 1\n        ---", 1),
		"non-string parameter":        strings.Replace(exampleApplication, "value: \"2\"", "value: 2", 1),
		"non-boolean forceString":     strings.Replace(exampleApplication, "value: \"2\"", "value: \"2\"\n          forceString: yes", 1),
	}
	for name, input := range tests {
		t.Run(name, func(t *testing.T) {
			if output, err := convert([]byte(input)); err == nil {
				t.Fatalf("convert succeeded with output:\n%s", output)
			}
		})
	}
}
