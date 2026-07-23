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
      fileParameters: []
      valueFiles: []
      kubeVersion: ""
      apiVersions: []
      skipCrds: false
      skipSchemaValidation: false
`)
	output, err := convert([]byte(input))
	if err != nil {
		t.Fatal(err)
	}
	text := string(output)
	for _, want := range []string{"- name: app\n", "chart: charts/chart\n", "version: 1.2.3\n"} {
		if !strings.Contains(text, want) {
			t.Errorf("output does not contain %q:\n%s", want, text)
		}
	}
	for _, omitted := range []string{"namespace:", "values:", "set:", "setString:", "forceString:", "missingFileHandler:", "helmDefaults:", "skipCRDs:", "skipSchemaValidation:", "kubeVersion:", "apiVersions:"} {
		if strings.Contains(text, omitted) {
			t.Errorf("output unexpectedly contains %q:\n%s", omitted, text)
		}
	}
}

func TestConvertHelmCapabilities(t *testing.T) {
	input := minimalApplication(`    helm:
      kubeVersion: 1.30.4
      apiVersions:
        - batch/v1
        - example.com/v1/Widget
        - v1
`)
	output, err := convert([]byte(input))
	if err != nil {
		t.Fatal(err)
	}
	want := `    kubeVersion: 1.30.4
    apiVersions:
      - batch/v1
      - example.com/v1/Widget
      - v1
`
	if !strings.Contains(string(output), want) {
		t.Fatalf("Helm capabilities were not preserved in order:\n%s", output)
	}
}

func TestConvertHelmCapabilitiesAreReleaseSpecific(t *testing.T) {
	first := minimalApplication(`    helm:
      kubeVersion: 1.29.0
      apiVersions: [first.example/v1]
`)
	second := strings.Replace(
		minimalApplication(`    helm:
      kubeVersion: 1.31.0
      apiVersions: [second.example/v1, second.example/v2]
`),
		"name: app",
		"name: second",
		1,
	)

	output, err := convert([]byte(first + "---\n" + second))
	if err != nil {
		t.Fatal(err)
	}
	want := `  - name: app
    chart: charts/chart
    version: 1.2.3
    kubeVersion: 1.29.0
    apiVersions:
      - first.example/v1
  - name: second
    chart: charts/chart
    version: 1.2.3
    kubeVersion: 1.31.0
    apiVersions:
      - second.example/v1
      - second.example/v2
`
	if !strings.Contains(string(output), want) {
		t.Fatalf("Helm capabilities were not kept per release:\n%s", output)
	}
}

func TestConvertRejectsInvalidHelmCapabilities(t *testing.T) {
	tests := map[string]string{
		"non-string kubeVersion": `    helm:
      kubeVersion: 1
`,
		"non-sequence apiVersions": `    helm:
      apiVersions: v1
`,
		"non-string apiVersions element": `    helm:
      apiVersions: [v1, 2]
`,
	}
	for name, helm := range tests {
		t.Run(name, func(t *testing.T) {
			if output, err := convert([]byte(minimalApplication(helm))); err == nil {
				t.Fatalf("convert succeeded with output:\n%s", output)
			}
		})
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

func TestConvertPassCredentials(t *testing.T) {
	tests := []struct {
		name string
		helm string
		want bool
	}{
		{
			name: "true",
			helm: "    helm:\n      passCredentials: true\n",
			want: true,
		},
		{
			name: "false",
			helm: "    helm:\n      passCredentials: false\n",
		},
		{
			name: "absent",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			output, err := convert([]byte(minimalApplication(test.helm)))
			if err != nil {
				t.Fatal(err)
			}
			contains := strings.Contains(string(output), "    passCredentials: true\n")
			if contains != test.want {
				t.Fatalf("passCredentials presence = %v, want %v:\n%s", contains, test.want, output)
			}
		})
	}
}

func TestConvertRejectsNonBooleanPassCredentials(t *testing.T) {
	for _, value := range []string{"enabled", "1", "null", `""`, "[]", "{}"} {
		t.Run(value, func(t *testing.T) {
			_, err := convert([]byte(minimalApplication(
				"    helm:\n      passCredentials: " + value + "\n",
			)))
			if err == nil || err.Error() != "document 1: spec.source.helm.passCredentials must be a boolean" {
				t.Fatalf("unexpected error: %v", err)
			}
		})
	}
}

func TestConvertRejectsZeroSkipCRDs(t *testing.T) {
	input := readTestdata(t, "helm-options/non-boolean-zero/application.yaml")
	_, err := convert([]byte(input))
	if err == nil ||
		err.Error() != "document 1: spec.source.helm.skipCrds must be a boolean" {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestConvertPreservesFalseAndZeroInlineValues(t *testing.T) {
	input := readTestdata(t, "helm-options/scalar-values/application.yaml")
	output, err := convert([]byte(input))
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		"  - name: false-value\n    chart: charts/chart\n    version: 1.2.3\n" +
			"    values:\n      - false\n",
		"  - name: zero-value\n    chart: charts/chart\n    version: 1.2.3\n" +
			"    values:\n      - 0\n",
	} {
		if !strings.Contains(string(output), want) {
			t.Errorf("output does not contain %q:\n%s", want, output)
		}
	}
}

func TestHelmBooleanOptionsAcceptFalseAndRejectZero(t *testing.T) {
	for _, option := range []string{
		"ignoreMissingValueFiles",
		"skipSchemaValidation",
		"skipCrds",
	} {
		t.Run(option, func(t *testing.T) {
			if _, err := parseHelmOptions(
				yaml.MapSlice{{Key: option, Value: false}},
				"helm",
			); err != nil {
				t.Fatalf("false was rejected: %v", err)
			}
			_, err := parseHelmOptions(yaml.MapSlice{{Key: option, Value: 0}}, "helm")
			want := "helm." + option + " must be a boolean"
			if err == nil || err.Error() != want {
				t.Fatalf("unexpected error: %v", err)
			}
		})
	}
}

func TestUnsupportedHelmOptionsIgnoreOnlyEmptyValues(t *testing.T) {
	for _, value := range []any{nil, "", []any{}, yaml.MapSlice{}} {
		if _, err := parseHelmOptions(
			yaml.MapSlice{{Key: "unsupported", Value: value}},
			"helm",
		); err != nil {
			t.Errorf("empty value %#v was rejected: %v", value, err)
		}
	}
	for _, value := range []any{false, 0} {
		_, err := parseHelmOptions(
			yaml.MapSlice{{Key: "unsupported", Value: value}},
			"helm",
		)
		if err == nil || err.Error() != "helm.unsupported is not supported" {
			t.Errorf("unexpected error for %#v: %v", value, err)
		}
	}
}

func TestYAMLEmptyValueClassifications(t *testing.T) {
	tests := []struct {
		name                 string
		value                any
		nilOrEmptyCollection bool
		ignorableOption      bool
	}{
		{name: "nil", nilOrEmptyCollection: true, ignorableOption: true},
		{name: "empty string", value: "", ignorableOption: true},
		{name: "empty sequence", value: []any{}, nilOrEmptyCollection: true, ignorableOption: true},
		{name: "empty mapping", value: yaml.MapSlice{}, nilOrEmptyCollection: true, ignorableOption: true},
		{name: "false", value: false},
		{name: "integer zero", value: 0},
		{name: "float zero", value: 0.0},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := isNilOrEmptyCollection(test.value); got != test.nilOrEmptyCollection {
				t.Errorf("isNilOrEmptyCollection() = %v, want %v", got, test.nilOrEmptyCollection)
			}
			if got := isIgnorableEmptyYAMLOption(test.value); got != test.ignorableOption {
				t.Errorf("isIgnorableEmptyYAMLOption() = %v, want %v", got, test.ignorableOption)
			}
		})
	}
}

func TestConvertIgnoresSkipTestsRegardlessOfValue(t *testing.T) {
	baseline, err := convert([]byte(minimalApplication("")))
	if err != nil {
		t.Fatal(err)
	}

	values := map[string]string{
		"true":     "true",
		"false":    "false",
		"null":     "null",
		"string":   "enabled",
		"number":   "1",
		"sequence": "[true, false]",
		"mapping":  "{enabled: true}",
	}
	for name, value := range values {
		t.Run(name, func(t *testing.T) {
			input := minimalApplication("    helm:\n      skipTests: " + value + "\n")
			output, err := convert([]byte(input))
			if err != nil {
				t.Fatal(err)
			}
			if string(output) != string(baseline) {
				t.Fatalf("skipTests changed output:\n%s\nwant:\n%s", output, baseline)
			}
		})
	}
}

func TestConvertAcceptsHelmVersionAndMatchingNamespace(t *testing.T) {
	tests := []struct {
		name           string
		input          string
		expected       string
		namespaceCount int
	}{
		{
			name:     "version",
			input:    "helm-options/version/application.yaml",
			expected: "helm-options/version/helmfile.yaml",
		},
		{
			name:     "non-string version",
			input:    "helm-options/version-non-string/application.yaml",
			expected: "helm-options/version/helmfile.yaml",
		},
		{
			name:           "namespace",
			input:          "helm-options/namespace/application.yaml",
			expected:       "helm-options/namespace/helmfile.yaml",
			namespaceCount: 1,
		},
		{
			name:           "combined",
			input:          "helm-options/combined/application.yaml",
			expected:       "helm-options/namespace/helmfile.yaml",
			namespaceCount: 1,
		},
		{
			name:     "empty options",
			input:    "helm-options/empty/application.yaml",
			expected: "helm-options/version/helmfile.yaml",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			output, err := convert([]byte(readTestdata(t, test.input)))
			if err != nil {
				t.Fatal(err)
			}
			want := readTestdata(t, test.expected)
			if string(output) != want {
				t.Fatalf("unexpected output:\n%s\nwant:\n%s", output, want)
			}
			namespaceCount := strings.Count(string(output), "    namespace:")
			if namespaceCount != test.namespaceCount {
				t.Fatalf(
					"release namespace count = %d, want %d:\n%s",
					namespaceCount, test.namespaceCount, output,
				)
			}
		})
	}
}

func TestConvertRejectsUnsupportedHelmNamespace(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  string
	}{
		{
			name:  "mismatch",
			input: "helm-options/mismatched-namespace/application.yaml",
			want:  `document 1: spec.source.helm.namespace "staging" must match spec.destination.namespace "production"`,
		},
		{
			name:  "empty destination",
			input: "helm-options/empty-destination-namespace/application.yaml",
			want:  `document 1: spec.source.helm.namespace "staging" must match spec.destination.namespace ""`,
		},
		{
			name:  "non-string",
			input: "helm-options/non-string-namespace/application.yaml",
			want:  "document 1: spec.source.helm.namespace must be a string",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			output, err := convert([]byte(readTestdata(t, test.input)))
			if err == nil {
				t.Fatalf("convert succeeded with output:\n%s", output)
			}
			if err.Error() != test.want {
				t.Fatalf("unexpected error: %v\nwant: %s", err, test.want)
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

func TestConvertPreservesEmptyParameterValue(t *testing.T) {
	input := minimalApplication(`    helm:
      parameters:
        - name: empty
          value: ""
`)
	output, err := convert([]byte(input))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(output), "      - name: empty\n        value: \"\"\n") {
		t.Fatalf("empty parameter value was not emitted:\n%s", output)
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
	exampleApplication := readTestdata(t, "example/application.yaml")
	config := testConfig(t, `destinations:
  - server: https://kubernetes.default.svc
    kubeContext: in-cluster
`)
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
			if output, err := convertWithConfig([]byte(input), config); err == nil {
				t.Fatalf("convert succeeded with output:\n%s", output)
			}
		})
	}
}
