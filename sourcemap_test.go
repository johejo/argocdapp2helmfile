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
    env: REPO_ROOT
`
	tests := map[string]string{
		"apiVersion":    strings.Replace(valid, sourceMapAPIVersion, "other/v1", 1),
		"kind":          strings.Replace(valid, "SourceMap", "Other", 1),
		"unknown field": valid + "unknown: true\n",
		"invalid env":   strings.Replace(valid, "REPO_ROOT", "1INVALID", 1),
		"duplicate key": valid + `  - repoURL: https://github.com/example/repo.git
    targetRevision: main
    env: OTHER_ROOT
`,
		"duplicate env": valid + `  - repoURL: https://github.com/example/other.git
    targetRevision: main
    env: REPO_ROOT
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

func TestSourceMapGitValidation(t *testing.T) {
	const repoURL = "https://github.com/example/charts.git"
	root := newTestGitRepository(t, "main", map[string]string{
		"Chart.yaml": "apiVersion: v2\nname: app\nversion: 1.0.0\n",
	})

	t.Run("clean checkout", func(t *testing.T) {
		resolver := testSourceResolver(t, testSource{
			repoURL: repoURL, targetRevision: "main", env: "TEST_REPO_ROOT", root: root,
		})
		if _, err := resolver.resolve(applicationSource{RepoURL: repoURL, TargetRevision: "main"}, "source"); err != nil {
			t.Fatal(err)
		}
	})

	t.Run("untracked files allowed", func(t *testing.T) {
		untracked := filepath.Join(root, "untracked")
		if err := os.WriteFile(untracked, []byte("local\n"), 0o644); err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() {
			if err := os.Remove(untracked); err != nil && !os.IsNotExist(err) {
				t.Error(err)
			}
		})
		resolver := testSourceResolver(t, testSource{
			repoURL: repoURL, targetRevision: "main", env: "TEST_REPO_ROOT", root: root,
		})
		if _, err := resolver.resolve(applicationSource{RepoURL: repoURL, TargetRevision: "main"}, "source"); err != nil {
			t.Fatal(err)
		}
	})

	t.Run("tracked dirty rejected by default", func(t *testing.T) {
		if err := os.WriteFile(filepath.Join(root, "Chart.yaml"), []byte("dirty\n"), 0o644); err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() {
			runTestGit(t, root, "restore", "Chart.yaml")
		})
		resolver := testSourceResolver(t, testSource{
			repoURL: repoURL, targetRevision: "main", env: "TEST_REPO_ROOT", root: root,
		})
		if _, err := resolver.resolve(applicationSource{RepoURL: repoURL, TargetRevision: "main"}, "source"); err == nil {
			t.Fatal("dirty checkout was accepted")
		}
	})

	t.Run("allowDirty", func(t *testing.T) {
		if err := os.WriteFile(filepath.Join(root, "Chart.yaml"), []byte("dirty\n"), 0o644); err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() {
			runTestGit(t, root, "restore", "Chart.yaml")
		})
		resolver := testSourceResolver(t, testSource{
			repoURL: repoURL, targetRevision: "main", env: "TEST_REPO_ROOT", root: root, allowDirty: true,
		})
		if _, err := resolver.resolve(applicationSource{RepoURL: repoURL, TargetRevision: "main"}, "source"); err != nil {
			t.Fatal(err)
		}
	})

	t.Run("revision mismatch", func(t *testing.T) {
		runTestGit(t, root, "tag", "other")
		if err := os.WriteFile(filepath.Join(root, "next"), []byte("next\n"), 0o644); err != nil {
			t.Fatal(err)
		}
		runTestGit(t, root, "add", "next")
		runTestGit(t, root, "commit", "-q", "-m", "next")
		resolver := testSourceResolver(t, testSource{
			repoURL: repoURL, targetRevision: "other", env: "TEST_REPO_ROOT", root: root,
		})
		if _, err := resolver.resolve(applicationSource{RepoURL: repoURL, TargetRevision: "other"}, "source"); err == nil {
			t.Fatal("revision mismatch was accepted")
		}
	})
}

func TestSourceMapRequiresAbsoluteEnvironmentPath(t *testing.T) {
	const config = `apiVersion: argocdapp2helmfile/v1alpha1
kind: SourceMap
sources:
  - repoURL: https://github.com/example/repo.git
    targetRevision: main
    env: TEST_REPO_ROOT
`
	source := applicationSource{
		RepoURL: "https://github.com/example/repo.git", TargetRevision: "main",
	}
	for name, value := range map[string]string{"missing": "", "relative": "checkouts/repo"} {
		t.Run(name, func(t *testing.T) {
			t.Setenv("TEST_REPO_ROOT", value)
			resolver, err := parseSourceMap([]byte(config))
			if err != nil {
				t.Fatal(err)
			}
			if _, err := resolver.resolve(source, "source"); err == nil {
				t.Fatal("source resolution succeeded")
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

func TestRunWithSourceMap(t *testing.T) {
	const repoURL = "https://github.com/example/charts.git"
	root := newTestGitRepository(t, "main", map[string]string{
		"chart/Chart.yaml": "apiVersion: v2\nname: app\nversion: 1.0.0\n",
	})
	t.Setenv("TEST_REPO_ROOT", root)
	sourceMapPath := filepath.Join(t.TempDir(), "sources.yaml")
	config := `apiVersion: argocdapp2helmfile/v1alpha1
kind: SourceMap
sources:
  - repoURL: https://github.com/example/charts.git
    targetRevision: main
    env: TEST_REPO_ROOT
`
	if err := os.WriteFile(sourceMapPath, []byte(config), 0o644); err != nil {
		t.Fatal(err)
	}
	input := gitApplication(repoURL, "chart", "main", "")
	var stdout, stderr bytes.Buffer
	if code := run([]string{"--source-map", sourceMapPath}, strings.NewReader(input), &stdout, &stderr); code != 0 {
		t.Fatalf("run returned %d: %s", code, stderr.String())
	}
	if !strings.Contains(stdout.String(), `chart: '{{ requiredEnv "TEST_REPO_ROOT" }}/chart'`) {
		t.Fatalf("unexpected output:\n%s", stdout.String())
	}
}
