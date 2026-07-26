package main

import (
	"bytes"
	"strconv"
	"strings"
	"testing"
)

func TestParseConfigRejectsInvalidReleaseLabels(t *testing.T) {
	const valid = `apiVersion: argocdapp2helmfile/v1alpha1
kind: Config
releaseLabels:
  - name: project
    query: .spec.project
`
	tests := map[string]string{
		"empty name":      strings.Replace(valid, "name: project", `name: ""`, 1),
		"empty query":     strings.Replace(valid, "query: .spec.project", `query: ""`, 1),
		"duplicate name":  valid + "  - name: project\n    query: .metadata.name\n",
		"invalid jq":      strings.Replace(valid, ".spec.project", ".[", 1),
		"compile failure": strings.Replace(valid, ".spec.project", "undefined_function", 1),
		"unknown field":   strings.Replace(valid, "query: .spec.project", "query: .spec.project\n    default: unknown", 1),
	}
	for name, input := range tests {
		t.Run(name, func(t *testing.T) {
			if _, err := parseConfig([]byte(input)); err == nil {
				t.Fatal("parseConfig succeeded")
			}
		})
	}
}

func TestConvertProjectsReleaseLabelsFromCompleteApplication(t *testing.T) {
	config := testConfig(t, `releaseLabels:
  - name: z-project
    query: .spec.project
  - name: a-team
    query: .metadata.labels.team
  - name: enabled
    query: .spec.source.helm.skipTests // false
  - name: replicas
    query: .spec.custom.replicas
`)
	input := strings.Replace(minimalApplication(`    helm:
      skipTests: true
`), "metadata:\n  name: app\n", `metadata:
  name: app
  labels:
    team: platform
`, 1)
	input = strings.Replace(input, "spec:\n", `spec:
  project: production
  custom:
    replicas: 3
`, 1)

	output, err := convertWithConfig([]byte(input), config)
	if err != nil {
		t.Fatal(err)
	}
	want := `    labels:
      z-project: production
      a-team: platform
      enabled: "true"
      replicas: "3"
`
	if !strings.Contains(string(output), want) {
		t.Fatalf("labels were not emitted in config order:\n%s", output)
	}
}

func TestConvertOmitsEmptyReleaseLabelResults(t *testing.T) {
	config := testConfig(t, `releaseLabels:
  - name: missing
    query: .spec.missing
  - name: absent
    query: empty
`)
	output, err := convertWithConfig([]byte(minimalApplication("")), config)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(output), "labels:") {
		t.Fatalf("empty labels mapping was emitted:\n%s", output)
	}
}

func TestConvertRejectsInvalidReleaseLabelResults(t *testing.T) {
	tests := map[string]string{
		"multiple":      ".metadata.name, .kind",
		"array":         "[.metadata.name]",
		"object":        ".metadata",
		"runtime error": ".metadata.name | tonumber",
	}
	for name, query := range tests {
		t.Run(name, func(t *testing.T) {
			config := testConfig(t, "releaseLabels:\n  - name: invalid\n    query: "+strconv.Quote(query)+"\n")
			if output, err := convertWithConfig([]byte(minimalApplication("")), config); err == nil {
				t.Fatalf("convert succeeded:\n%s", output)
			}
		})
	}
}

func TestRunReleaseLabelFailureIsAtomic(t *testing.T) {
	configPath := writeTestConfig(t, `releaseLabels:
  - name: invalid
    query: .metadata
`)
	var stdout, stderr bytes.Buffer
	code := run(
		[]string{"--config", configPath},
		strings.NewReader(minimalApplication("")),
		&stdout,
		&stderr,
	)
	if code != 1 || stdout.Len() != 0 {
		t.Fatalf("code = %d, stdout = %q, stderr = %q", code, stdout.String(), stderr.String())
	}
}

func TestConvertUsesConfigSourcesAndReleaseLabelsTogether(t *testing.T) {
	config := testConfig(t, `sources:
  - repoURL: https://github.com/example/charts.git
    targetRevision: main
    localRoot: ./sources/charts
releaseLabels:
  - name: argocd.project
    query: .spec.project
`)
	input := strings.Replace(
		gitApplication("https://github.com/example/charts.git", "charts/app", "main", ""),
		"spec:\n",
		"spec:\n  project: production\n",
		1,
	)
	output, err := convertWithConfig([]byte(input), config)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		"    labels:\n      argocd.project: production\n",
		"    chart: './sources/charts/charts/app'\n",
	} {
		if !strings.Contains(string(output), want) {
			t.Errorf("output does not contain %q:\n%s", want, output)
		}
	}
}

func TestConvertApplicationSetProjectsPerApplicationLabels(t *testing.T) {
	config := testConfig(t, `releaseLabels:
  - name: argocd.skipTests
    query: .spec.source.helm.skipTests
  - name: argocd.project
    query: .spec.project
  - name: resource
    query: .apiVersion + "/" + .kind
`)
	input := readTestdata(t, "applicationset/release-labels/application.yaml")
	output, err := convertWithConfig([]byte(input), config)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		"  - name: enabled\n    labels:\n      argocd.skipTests: \"true\"\n      argocd.project: one\n      resource: argoproj.io/v1alpha1/Application\n",
		"  - name: disabled\n    labels:\n      argocd.skipTests: \"false\"\n      argocd.project: two\n      resource: argoproj.io/v1alpha1/Application\n",
	} {
		if !strings.Contains(string(output), want) {
			t.Errorf("output does not contain %q:\n%s", want, output)
		}
	}
}

func TestConvertApplicationSetLabelFailureReportsElementOrigin(t *testing.T) {
	config := testConfig(t, `releaseLabels:
  - name: invalid
    query: .metadata
`)
	input := readTestdata(t, "applicationset/minimal/application.yaml")
	_, err := convertWithConfig([]byte(input), config)
	want := "document 1: spec.generators[0].list.elements[0]: release label"
	assertErrorContains(t, err, want)
}
