package main

import (
	"strings"
	"testing"
)

func TestConvertValueFilePathSemantics(t *testing.T) {
	const repoURL = "https://github.com/example/charts.git"
	resolver := testSourceResolver(t, testSource{
		repoURL: repoURL, targetRevision: "main", root: `{{ requiredEnv "CHARTS_ROOT" }}`,
	})
	input := gitApplication(repoURL, "charts/app", "main", `    helm:
      valueFiles:
        - values/*.yaml
        - values/b.yaml
        - ../shared.yaml
        - /repository-root-values.yaml
`)
	output, err := convertWithResolver([]byte(input), resolver)
	if err != nil {
		t.Fatal(err)
	}
	ordered := []string{
		`{{ requiredEnv "CHARTS_ROOT" }}/charts/app/values/*.yaml`,
		`{{ requiredEnv "CHARTS_ROOT" }}/charts/app/values/b.yaml`,
		`{{ requiredEnv "CHARTS_ROOT" }}/charts/shared.yaml`,
		`{{ requiredEnv "CHARTS_ROOT" }}/repository-root-values.yaml`,
	}
	position := -1
	for _, fragment := range ordered {
		next := strings.Index(string(output)[position+1:], fragment)
		if next < 0 {
			t.Fatalf("output does not contain %q in order:\n%s", fragment, output)
		}
		position += next + 1
	}
}

func TestConvertDelegatesValueFileGlobAndMissingHandling(t *testing.T) {
	const repoURL = "git@github.com:example/charts.git"
	resolver := testSourceResolver(t, testSource{
		repoURL: repoURL, targetRevision: "main", root: "/workspace/charts",
	})
	for _, ignore := range []bool{false, true} {
		t.Run(map[bool]string{false: "error by default", true: "warn"}[ignore], func(t *testing.T) {
			ignoreLine := ""
			if ignore {
				ignoreLine = "      ignoreMissingValueFiles: true\n"
			}
			input := gitApplication(repoURL, "chart", "main", "    helm:\n      valueFiles: [missing/**/*.yaml]\n"+ignoreLine)
			output, err := convertWithResolver([]byte(input), resolver)
			if err != nil {
				t.Fatal(err)
			}
			text := string(output)
			if !strings.Contains(text, "/workspace/charts/chart/missing/**/*.yaml") {
				t.Fatalf("glob was not preserved:\n%s", output)
			}
			hasWarn := strings.Contains(text, "    missingFileHandler: Warn\n")
			if hasWarn != ignore {
				t.Fatalf("unexpected missingFileHandler:\n%s", output)
			}
		})
	}
}

func TestConvertDoesNotInspectValueFile(t *testing.T) {
	const repoURL = "git@github.com:example/charts.git"
	resolver := testSourceResolver(t, testSource{
		repoURL: repoURL, targetRevision: "main", root: "/path/that/does/not/exist",
	})
	input := gitApplication(repoURL, "chart", "main", "    helm:\n      valueFiles: [values.yaml]\n")
	output, err := convertWithResolver([]byte(input), resolver)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(output), "/path/that/does/not/exist/chart/values.yaml") {
		t.Fatalf("unexpected output:\n%s", output)
	}
}

func TestConvertRejectsValuePathOutsideRepository(t *testing.T) {
	const repoURL = "git@github.com:example/charts.git"
	resolver := testSourceResolver(t, testSource{
		repoURL: repoURL, targetRevision: "main", root: "/workspace/charts",
	})
	input := gitApplication(repoURL, "chart", "main", "    helm:\n      valueFiles: [../../outside.yaml]\n")
	if output, err := convertWithResolver([]byte(input), resolver); err == nil {
		t.Fatalf("conversion succeeded:\n%s", output)
	}
}
