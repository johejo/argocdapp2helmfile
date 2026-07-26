package main

import (
	"bytes"
	"os"
	"testing"

	"github.com/johejo/argocdapp2helmfile/internal/applicationset"
)

func TestApplicationSetReferenceIsCurrent(t *testing.T) {
	document, err := os.ReadFile("docs/applicationset.md")
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(document, applicationset.Markdown()) {
		t.Fatal("docs/applicationset.md is stale; run go generate ./...")
	}
}
