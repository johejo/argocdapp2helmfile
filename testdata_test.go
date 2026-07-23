package main

import (
	"os"
	"path/filepath"
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
