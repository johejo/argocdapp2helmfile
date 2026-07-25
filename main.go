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
	strict, configPath, ok := parseArgs(args)
	if !ok {
		fmt.Fprintln(
			stderr,
			"argocdapp2helmfile: usage: argocdapp2helmfile [--strict] [--config PATH]",
		)
		return 1
	}

	var config *conversionConfig
	if configPath != "" {
		input, err := os.ReadFile(configPath)
		if err != nil {
			writeDiagnostic(stderr, fmt.Errorf("read config: %w", err))
			return 1
		}
		config, err = parseConfig(input)
		if err != nil {
			writeDiagnostic(stderr, err)
			return 1
		}
	}

	input, err := io.ReadAll(stdin)
	if err != nil {
		writeDiagnostic(stderr, fmt.Errorf("read input: %w", err))
		return 1
	}
	result, err := convertWithDiagnostics(input, config)
	if err != nil {
		writeDiagnostic(stderr, err)
		return 1
	}
	level := "warning"
	if strict {
		level = "error"
	}
	for _, diagnostic := range result.diagnostics {
		fmt.Fprintf(
			stderr,
			"argocdapp2helmfile: %s: %s\n",
			level,
			diagnostic,
		)
	}
	if strict && len(result.diagnostics) != 0 {
		return 1
	}
	if _, err := stdout.Write(result.output); err != nil {
		writeDiagnostic(stderr, fmt.Errorf("write output: %w", err))
		return 1
	}
	return 0
}

func parseArgs(args []string) (strict bool, configPath string, ok bool) {
	for index := 0; index < len(args); index++ {
		switch args[index] {
		case "--strict":
			if strict {
				return false, "", false
			}
			strict = true
		case "--config":
			if configPath != "" || index+1 == len(args) {
				return false, "", false
			}
			index++
			configPath = args[index]
			if configPath == "" || strings.HasPrefix(configPath, "--") {
				return false, "", false
			}
		default:
			return false, "", false
		}
	}
	return strict, configPath, true
}

func writeDiagnostic(stderr io.Writer, err error) {
	// Collapse annotated parser errors to one diagnostic line.
	message := strings.Join(strings.Fields(err.Error()), " ")
	fmt.Fprintf(stderr, "argocdapp2helmfile: %s\n", message)
}
