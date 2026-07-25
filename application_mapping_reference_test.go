package main

import (
	"bytes"
	"os"
	"testing"

	"github.com/johejo/argocdapp2helmfile/internal/applicationmapping"
)

func TestApplicationMappingReferenceIsCurrent(t *testing.T) {
	document, err := os.ReadFile("docs/application-mapping.md")
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(document, applicationmapping.Markdown()) {
		t.Fatal("docs/application-mapping.md is stale; run go generate ./...")
	}
}
