package main

import (
	"bytes"
	"os"
	"testing"

	"github.com/johejo/argocdapp2helmfile/internal/diagnostic"
)

func TestDiagnosticReferenceIsGenerated(t *testing.T) {
	document, err := os.ReadFile("docs/diagnostics.md")
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(document, diagnostic.Markdown()) {
		t.Fatal("docs/diagnostics.md is stale; run go generate ./...")
	}
}
