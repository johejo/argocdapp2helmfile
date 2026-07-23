package main

import (
	"fmt"
	"io"
	"os"
	"strings"
)

func main() {
	os.Exit(run(os.Args[1:], os.Stdin, os.Stdout, os.Stderr))
}

func run(args []string, stdin io.Reader, stdout, stderr io.Writer) int {
	var resolver *sourceResolver
	switch {
	case len(args) == 0:
	case len(args) == 2 && args[0] == "--source-map":
		input, err := os.ReadFile(args[1])
		if err != nil {
			writeDiagnostic(stderr, fmt.Errorf("read source map: %w", err))
			return 1
		}
		resolver, err = parseSourceMap(input)
		if err != nil {
			writeDiagnostic(stderr, err)
			return 1
		}
	default:
		fmt.Fprintln(stderr, "argocdapp2helmfile: usage: argocdapp2helmfile [--source-map PATH]")
		return 1
	}

	input, err := io.ReadAll(stdin)
	if err != nil {
		writeDiagnostic(stderr, fmt.Errorf("read input: %w", err))
		return 1
	}
	output, err := convertWithSourceMap(input, resolver)
	if err != nil {
		writeDiagnostic(stderr, err)
		return 1
	}
	if _, err := stdout.Write(output); err != nil {
		writeDiagnostic(stderr, fmt.Errorf("write output: %w", err))
		return 1
	}
	return 0
}

func writeDiagnostic(stderr io.Writer, err error) {
	// Parser errors can contain annotated source excerpts. Keep the CLI contract
	// of one diagnostic line while retaining the meaningful tokens and location.
	message := strings.Join(strings.Fields(err.Error()), " ")
	fmt.Fprintf(stderr, "argocdapp2helmfile: %s\n", message)
}
