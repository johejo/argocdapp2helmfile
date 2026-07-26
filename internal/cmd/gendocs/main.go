package main

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/johejo/argocdapp2helmfile/internal/reference"
)

func main() {
	for _, document := range reference.Documents {
		if err := write(document); err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
	}
}

func write(document reference.Document) error {
	if err := os.MkdirAll(filepath.Dir(document.Path), 0o755); err != nil {
		return err
	}
	return os.WriteFile(document.Path, document.Render(), 0o644)
}
