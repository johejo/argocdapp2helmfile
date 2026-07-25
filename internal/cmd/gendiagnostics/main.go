package main

import (
	"fmt"
	"os"

	"github.com/johejo/argocdapp2helmfile/internal/diagnostic"
)

func main() {
	if err := os.MkdirAll("docs", 0o755); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	if err := os.WriteFile("docs/diagnostics.md", diagnostic.Markdown(), 0o644); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
