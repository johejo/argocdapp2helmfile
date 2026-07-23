package main

import (
	"fmt"
	"os"
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
	root           string
}

func testSourceResolver(t *testing.T, sources ...testSource) *sourceResolver {
	t.Helper()
	var config strings.Builder
	config.WriteString("apiVersion: argocdapp2helmfile/v1alpha1\nkind: SourceMap\nsources:\n")
	for _, source := range sources {
		fmt.Fprintf(&config, "  - repoURL: %q\n    targetRevision: %q\n    root: %q\n", source.repoURL, source.targetRevision, source.root)
	}
	resolver, err := parseSourceMap([]byte(config.String()))
	if err != nil {
		t.Fatal(err)
	}
	return resolver
}
