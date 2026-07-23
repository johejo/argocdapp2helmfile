package main

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func readTestdata(t *testing.T, name string) string {
	t.Helper()

	path := filepath.Join("testdata", filepath.FromSlash(name))
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read testdata %q: %v", path, err)
	}
	return string(data)
}

type testSource struct {
	repoURL        string
	targetRevision string
	env            string
	root           string
	allowDirty     bool
}

func newTestGitRepository(t *testing.T, revision string, files map[string]string) string {
	t.Helper()
	root := t.TempDir()
	for name, content := range files {
		fullPath := filepath.Join(root, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(fullPath), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(fullPath, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	runTestGit(t, root, "init", "-q")
	runTestGit(t, root, "config", "user.name", "Test")
	runTestGit(t, root, "config", "user.email", "test@example.com")
	runTestGit(t, root, "add", ".")
	runTestGit(t, root, "commit", "-q", "-m", "fixture")
	switch revision {
	case "HEAD":
	case "main":
		runTestGit(t, root, "branch", "-M", "main")
	default:
		runTestGit(t, root, "tag", revision)
	}
	return root
}

func testSourceResolver(t *testing.T, sources ...testSource) *sourceResolver {
	t.Helper()
	var config strings.Builder
	config.WriteString("apiVersion: argocdapp2helmfile/v1alpha1\nkind: SourceMap\nsources:\n")
	for _, source := range sources {
		fmt.Fprintf(&config, "  - repoURL: %q\n    targetRevision: %q\n    env: %s\n", source.repoURL, source.targetRevision, source.env)
		if source.allowDirty {
			config.WriteString("    allowDirty: true\n")
		}
		t.Setenv(source.env, source.root)
	}
	resolver, err := parseSourceMap([]byte(config.String()))
	if err != nil {
		t.Fatal(err)
	}
	return resolver
}

func runTestGit(t *testing.T, root string, args ...string) string {
	t.Helper()
	command := exec.Command("git", append([]string{"-C", root}, args...)...)
	output, err := command.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v: %v: %s", args, err, output)
	}
	return strings.TrimSpace(string(output))
}
