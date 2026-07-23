package main

import (
	"strings"
	"testing"
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
