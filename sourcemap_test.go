package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestParseSourceMapRejectsInvalidConfiguration(t *testing.T) {
	valid := `apiVersion: argocdapp2helmfile/v1alpha1
kind: SourceMap
sources:
  - repoURL: https://github.com/example/repo.git
    targetRevision: main
    root: '{{ requiredEnv "REPO_ROOT" }}'
`
	tests := map[string]string{
		"apiVersion":    strings.Replace(valid, sourceMapAPIVersion, "other/v1", 1),
		"kind":          strings.Replace(valid, "SourceMap", "Other", 1),
		"unknown field": valid + "unknown: true\n",
		"empty root":    strings.Replace(valid, `root: '{{ requiredEnv "REPO_ROOT" }}'`, `root: ""`, 1),
		"legacy env":    strings.Replace(valid, `root: '{{ requiredEnv "REPO_ROOT" }}'`, "env: REPO_ROOT", 1),
		"allowDirty": strings.Replace(
			valid,
			`root: '{{ requiredEnv "REPO_ROOT" }}'`,
			"root: /repo\n    allowDirty: true",
			1,
		),
		"duplicate key": valid + `  - repoURL: https://github.com/example/repo.git
    targetRevision: main
    root: /other
`,
	}
	for name, input := range tests {
		t.Run(name, func(t *testing.T) {
			if _, err := parseSourceMap([]byte(input)); err == nil {
				t.Fatal("parseSourceMap succeeded")
			}
		})
	}
}

func TestSourceMapAllowsLiteralAndDuplicateRoots(t *testing.T) {
	const config = `apiVersion: argocdapp2helmfile/v1alpha1
kind: SourceMap
sources:
  - repoURL: https://github.com/example/charts.git
    targetRevision: main
    root: '{{ requiredEnv "SOURCES_ROOT" }}'
  - repoURL: https://github.com/example/values.git
    targetRevision: main
    root: '{{ requiredEnv "SOURCES_ROOT" }}'
`
	resolver, err := parseSourceMap([]byte(config))
	if err != nil {
		t.Fatal(err)
	}
	resolved, err := resolver.resolve(applicationSource{
		RepoURL: "https://github.com/example/charts.git", TargetRevision: "main",
	}, "source")
	if err != nil {
		t.Fatal(err)
	}
	if resolved.root != `{{ requiredEnv "SOURCES_ROOT" }}` {
		t.Fatalf("unexpected root: %q", resolved.root)
	}
}

func TestJoinSourcePath(t *testing.T) {
	for name, test := range map[string]struct {
		root, relative, want string
	}{
		"template":        {`{{ requiredEnv "ROOT" }}`, "charts/app", `{{ requiredEnv "ROOT" }}/charts/app`},
		"trailing slash":  {"/workspace/", "charts/app", "/workspace/charts/app"},
		"repository root": {"/workspace", ".", "/workspace"},
	} {
		t.Run(name, func(t *testing.T) {
			if got := joinSourcePath(test.root, test.relative); got != test.want {
				t.Fatalf("joinSourcePath() = %q, want %q", got, test.want)
			}
		})
	}
}

func TestConvertRequiresMappedGitSource(t *testing.T) {
	input := gitApplication("https://github.com/example/charts.git", "chart", "main", "")
	if output, err := convert([]byte(input)); err == nil || !strings.Contains(err.Error(), "requires --source-map") {
		t.Fatalf("unexpected result: %s, %v", output, err)
	}
}

func TestRunWithSourceMapDoesNotResolveRoot(t *testing.T) {
	sourceMapPath := filepath.Join(t.TempDir(), "sources.yaml")
	config := `apiVersion: argocdapp2helmfile/v1alpha1
kind: SourceMap
sources:
  - repoURL: https://github.com/example/charts.git
    targetRevision: main
    root: '{{ requiredEnv "REPO_ROOT" }}'
`
	if err := os.WriteFile(sourceMapPath, []byte(config), 0o644); err != nil {
		t.Fatal(err)
	}
	input := gitApplication("https://github.com/example/charts.git", "chart", "main", "")
	var stdout, stderr bytes.Buffer
	if code := run([]string{"--source-map", sourceMapPath}, strings.NewReader(input), &stdout, &stderr); code != 0 {
		t.Fatalf("run returned %d: %s", code, stderr.String())
	}
	if !strings.Contains(stdout.String(), `chart: '{{ requiredEnv "REPO_ROOT" }}/chart'`) {
		t.Fatalf("unexpected output:\n%s", stdout.String())
	}
}
