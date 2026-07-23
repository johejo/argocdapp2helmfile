package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestConvertValueFileArgoCD34PathSemantics(t *testing.T) {
	const repoURL = "https://github.com/example/charts.git"
	root := newTestGitRepository(t, "main", map[string]string{
		"charts/app/Chart.yaml":       "apiVersion: v2\nname: app\nversion: 1.0.0\n",
		"charts/app/values/a.yaml":    "value: a\n",
		"charts/app/values/b.yaml":    "value: b\n",
		"charts/shared.yaml":          "shared: true\n",
		"repository-root-values.yaml": "root: true\n",
	})
	resolver := testSourceResolver(t, testSource{
		repoURL: repoURL, targetRevision: "main", env: "TEST_CHART_ROOT", root: root,
	})
	input := gitApplication(repoURL, "charts/app", "main", `    helm:
      valueFiles:
        - values/*.yaml
        - values/b.yaml
        - ../shared.yaml
        - /repository-root-values.yaml
`)
	output, err := convertWithSourceMap([]byte(input), resolver)
	if err != nil {
		t.Fatal(err)
	}
	text := string(output)
	ordered := []string{
		"/charts/app/values/a.yaml",
		"/charts/app/values/b.yaml",
		"/charts/shared.yaml",
		"/repository-root-values.yaml",
	}
	position := -1
	for _, fragment := range ordered {
		next := strings.Index(text[position+1:], fragment)
		if next < 0 {
			t.Fatalf("output does not contain %q in order:\n%s", fragment, output)
		}
		position += next + 1
	}
	if strings.Count(text, "/charts/app/values/b.yaml") != 1 {
		t.Fatalf("explicit file was not deduplicated from glob:\n%s", output)
	}
}

func TestConvertValueFileGlobNoMatch(t *testing.T) {
	const repoURL = "git@github.com:example/charts.git"
	root := newTestGitRepository(t, "main", map[string]string{
		"chart/Chart.yaml": "apiVersion: v2\nname: app\nversion: 1.0.0\n",
	})
	for _, ignore := range []bool{false, true} {
		t.Run(map[bool]string{false: "error", true: "ignored"}[ignore], func(t *testing.T) {
			resolver := testSourceResolver(t, testSource{
				repoURL: repoURL, targetRevision: "main", env: "TEST_CHART_ROOT", root: root,
			})
			ignoreLine := ""
			if ignore {
				ignoreLine = "      ignoreMissingValueFiles: true\n"
			}
			input := gitApplication(repoURL, "chart", "main", "    helm:\n      valueFiles: [missing/**/*.yaml]\n"+ignoreLine)
			output, err := convertWithSourceMap([]byte(input), resolver)
			if ignore {
				if err != nil {
					t.Fatal(err)
				}
				if strings.Contains(string(output), "missing/") {
					t.Fatalf("unmatched glob was emitted:\n%s", output)
				}
			} else if err == nil || !strings.Contains(err.Error(), "matched no files") {
				t.Fatalf("unexpected error: %v", err)
			}
		})
	}
}

func TestConvertRejectsValueSymlinkOutsideRepository(t *testing.T) {
	const repoURL = "git@github.com:example/charts.git"
	root := newTestGitRepository(t, "main", map[string]string{
		"chart/Chart.yaml": "apiVersion: v2\nname: app\nversion: 1.0.0\n",
	})
	outside := filepath.Join(t.TempDir(), "outside.yaml")
	if err := os.WriteFile(outside, []byte("outside: true\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(root, "chart", "outside.yaml")); err != nil {
		t.Fatal(err)
	}
	resolver := testSourceResolver(t, testSource{
		repoURL: repoURL, targetRevision: "main", env: "TEST_CHART_ROOT", root: root,
	})
	input := gitApplication(repoURL, "chart", "main", "    helm:\n      valueFiles: ['*.yaml']\n")
	if output, err := convertWithSourceMap([]byte(input), resolver); err == nil {
		t.Fatalf("conversion succeeded:\n%s", output)
	}
}

func TestConvertRejectsValuePathOutsideRepository(t *testing.T) {
	const repoURL = "git@github.com:example/charts.git"
	root := newTestGitRepository(t, "main", map[string]string{
		"chart/Chart.yaml": "apiVersion: v2\nname: app\nversion: 1.0.0\n",
	})
	resolver := testSourceResolver(t, testSource{
		repoURL: repoURL, targetRevision: "main", env: "TEST_CHART_ROOT", root: root,
	})
	input := gitApplication(repoURL, "chart", "main", "    helm:\n      valueFiles: [../../outside.yaml]\n")
	if output, err := convertWithSourceMap([]byte(input), resolver); err == nil {
		t.Fatalf("conversion succeeded:\n%s", output)
	}
}
