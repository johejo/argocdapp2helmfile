package main

import (
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/johejo/argocdapp2helmfile/internal/diagnostic"
)

//go:generate go run ./internal/cmd/gendiagnostics

func main() {
	os.Exit(run(os.Args[1:], os.Stdin, os.Stdout, os.Stderr))
}

type commandOptions struct {
	strict          bool
	configPath      string
	helpDiagnostics bool
}

func run(args []string, stdin io.Reader, stdout, stderr io.Writer) int {
	options, ok := parseArgs(args)
	if !ok {
		fmt.Fprintln(
			stderr,
			"argocdapp2helmfile: usage: "+
				"argocdapp2helmfile [--strict] [--config PATH] | --help-diagnostics",
		)
		return 1
	}
	if options.helpDiagnostics {
		if _, err := stdout.Write(diagnostic.Markdown()); err != nil {
			writeDiagnostic(stderr, fmt.Errorf("write diagnostics reference: %w", err))
			return 1
		}
		return 0
	}

	var config *conversionConfig
	if options.configPath != "" {
		input, err := os.ReadFile(options.configPath)
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
	if options.strict {
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
	if options.strict && len(result.diagnostics) != 0 {
		return 1
	}
	if _, err := stdout.Write(result.output); err != nil {
		writeDiagnostic(stderr, fmt.Errorf("write output: %w", err))
		return 1
	}
	return 0
}

func parseArgs(args []string) (commandOptions, bool) {
	var options commandOptions
	for index := 0; index < len(args); index++ {
		switch args[index] {
		case "--strict":
			if options.strict {
				return commandOptions{}, false
			}
			options.strict = true
		case "--config":
			if options.configPath != "" || index+1 == len(args) {
				return commandOptions{}, false
			}
			index++
			options.configPath = args[index]
			if options.configPath == "" || strings.HasPrefix(options.configPath, "--") {
				return commandOptions{}, false
			}
		case "--help-diagnostics":
			if options.helpDiagnostics {
				return commandOptions{}, false
			}
			options.helpDiagnostics = true
		default:
			return commandOptions{}, false
		}
	}
	if options.helpDiagnostics && (options.strict || options.configPath != "") {
		return commandOptions{}, false
	}
	return options, true
}

func writeDiagnostic(stderr io.Writer, err error) {
	// Collapse annotated parser errors to one diagnostic line.
	message := strings.Join(strings.Fields(err.Error()), " ")
	fmt.Fprintf(stderr, "argocdapp2helmfile: %s\n", message)
}
