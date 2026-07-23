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
      skipSchemaValidation: true
      skipCrds: true
`

func TestConvertExample(t *testing.T) {
	output, err := convert([]byte(exampleApplication))
	if err != nil {
		t.Fatal(err)
	}
	want := `repositories:
  - name: source
    url: https://charts.bitnami.com/bitnami
helmDefaults:
  skipCRDs: true
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
    skipSchemaValidation: true
`
	if string(output) != want {
		t.Fatalf("unexpected output:\n%s\nwant:\n%s", output, want)
	}
}

func TestConvertOCIRepository(t *testing.T) {
	input := `apiVersion: argoproj.io/v1alpha1
kind: Application
metadata:
  name: nginx
spec:
  destination:
    namespace: nginx
  source:
    repoURL: registry-1.docker.io/bitnamicharts
    chart: nginx
    targetRevision: 15.9.0
`
	output, err := convert([]byte(input))
	if err != nil {
		t.Fatal(err)
	}
	want := `repositories:
  - name: source
    url: registry-1.docker.io/bitnamicharts
    oci: true
releases:
  - name: nginx
    namespace: nginx
    chart: source/nginx
    version: 15.9.0
`
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
	want := `repositories:
  - name: source
    url: https://example.com/charts
  - name: source-2
    url: registry.example.com/charts
    oci: true
releases:
  - name: app
    chart: source/chart
    version: 1.2.3
  - name: worker
    chart: source-2/worker
    version: 4.5.6
`
	if string(output) != want {
		t.Fatalf("unexpected output:\n%s\nwant:\n%s", output, want)
	}
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

func TestConvertValueFilesPrecedeInlineValues(t *testing.T) {
	input := minimalApplication(`    helm:
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
	output, err := convert([]byte(input))
	if err != nil {
		t.Fatal(err)
	}
	ordered := []string{
		`'{{ requiredEnv "ARGOCDAPP2HELMFILE_VALUES_ROOT" }}/document-1/chart/values.yaml'`,
		`'{{ requiredEnv "ARGOCDAPP2HELMFILE_VALUES_ROOT" }}/document-1/chart/environments/prod.yaml'`,
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

func TestConvertValueFilesUseDocumentScopedRoots(t *testing.T) {
	first := minimalApplication("    helm:\n      valueFiles: [values.yaml]\n")
	second := strings.Replace(first, "name: app", "name: second", 1)
	output, err := convert([]byte(first + "---\n" + second))
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		`document-1/chart/values.yaml`,
		`document-2/chart/values.yaml`,
	} {
		if !strings.Contains(string(output), want) {
			t.Errorf("output does not contain %q:\n%s", want, output)
		}
	}
}

func TestConvertIgnoreMissingValueFiles(t *testing.T) {
	input := minimalApplication(`    helm:
      valueFiles: [optional.yaml]
      ignoreMissingValueFiles: true
`)
	output, err := convert([]byte(input))
	if err != nil {
		t.Fatal(err)
	}
	text := string(output)
	if !strings.Contains(text, "    missingFileHandler: Warn\n") {
		t.Fatalf("missingFileHandler was not emitted:\n%s", output)
	}
	if !strings.Contains(text, `requiredEnv "ARGOCDAPP2HELMFILE_VALUES_ROOT"`) {
		t.Fatalf("values root is not still required:\n%s", output)
	}
}

func TestConvertRejectsUnsafeValueFilePaths(t *testing.T) {
	tests := []string{"", "/values.yaml", "C:/values.yaml", "./values.yaml", "../values.yaml", "a/../values.yaml", "a//values.yaml", `a\values.yaml`, "*.yaml", "values?.yaml", "values[0].yaml", "{a,b}.yaml"}
	for _, valueFile := range tests {
		t.Run(valueFile, func(t *testing.T) {
			input := minimalApplication("    helm:\n      valueFiles:\n        - " + yamlScalar(valueFile) + "\n")
			if output, err := convert([]byte(input)); err == nil {
				t.Fatalf("convert succeeded with output:\n%s", output)
			}
		})
	}
}

func TestConvertMultiSourceValueReferences(t *testing.T) {
	input := `apiVersion: argoproj.io/v1alpha1
kind: Application
metadata:
  name: app
spec:
  sources:
    - repoURL: https://example.com/charts
      chart: chart
      targetRevision: 1.2.3
      helm:
        valueFiles:
          - defaults.yaml
          - $values/environments/prod.yaml
          - $secrets/team/values.yaml
    - repoURL: https://github.com/example/values.git
      targetRevision: 0123456789abcdef
      ref: values
    - repoURL: ssh://git@example.com/secrets.git
      targetRevision: release-1
      ref: secrets
`
	output, err := convert([]byte(input))
	if err != nil {
		t.Fatal(err)
	}
	text := string(output)
	for _, want := range []string{
		`document-1/chart/defaults.yaml`,
		`document-1/refs/values/environments/prod.yaml`,
		`document-1/refs/secrets/team/values.yaml`,
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

func TestRunErrorsAreAtomic(t *testing.T) {
	tests := []struct {
		name  string
		args  []string
		input string
	}{
		{name: "conversion", input: "invalid: ["},
		{name: "later document", input: minimalApplication("") + "---\ninvalid: ["},
		{name: "duplicate release", input: minimalApplication("") + "---\n" + minimalApplication("")},
		{name: "shared option conflict", input: minimalApplication("    helm:\n      skipCrds: true\n") + "---\n" + strings.Replace(minimalApplication(""), "name: app", "name: second", 1)},
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
