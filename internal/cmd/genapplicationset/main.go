package main

import (
	"fmt"
	"os"

	"github.com/johejo/argocdapp2helmfile/internal/applicationset"
)

func main() {
	if err := os.MkdirAll("docs", 0o755); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	if err := os.WriteFile(
		"docs/applicationset.md",
		applicationset.Markdown(),
		0o644,
	); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
