package main

import (
	"bytes"
	"os"
	"testing"

	"github.com/johejo/argocdapp2helmfile/internal/reference"
)

func TestReferencesAreCurrent(t *testing.T) {
	for _, document := range reference.Documents {
		t.Run(document.Path, func(t *testing.T) {
			content, err := os.ReadFile(document.Path)
			if err != nil {
				t.Fatal(err)
			}
			if !bytes.Equal(content, document.Render()) {
				t.Fatalf("%s is stale; run go generate ./...", document.Path)
			}
		})
	}
}
