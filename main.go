package main

import (
	"bytes"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/johejo/argocdapp2helmfile/internal/applicationmapping"
	"github.com/johejo/argocdapp2helmfile/internal/diagnostic"
)

//go:generate go run ./internal/cmd/gendiagnostics
//go:generate go run ./internal/cmd/genapplicationmapping

func main() {
	os.Exit(run(os.Args[1:], os.Stdin, os.Stdout, os.Stderr))
}

type commandOptions struct {
	strict          bool
	configPath      string
	helpDiagnostics bool
	helpMapping     bool
}

func run(args []string, stdin io.Reader, stdout, stderr io.Writer) int {
	options, help, err := parseArgs(args)
	if errors.Is(err, flag.ErrHelp) {
		if _, err := io.WriteString(stdout, help); err != nil {
			writeDiagnostic(stderr, fmt.Errorf("write help: %w", err))
			return 1
		}
		return 0
	}
	if err != nil {
		writeDiagnostic(stderr, err)
		return 1
	}
	if options.helpDiagnostics {
		if _, err := stdout.Write(diagnostic.Markdown()); err != nil {
			writeDiagnostic(stderr, fmt.Errorf("write diagnostics reference: %w", err))
			return 1
		}
		return 0
	}
	if options.helpMapping {
		if _, err := stdout.Write(applicationmapping.Markdown()); err != nil {
			writeDiagnostic(stderr, fmt.Errorf("write application mapping reference: %w", err))
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

func parseArgs(args []string) (commandOptions, string, error) {
	var options commandOptions
	var output bytes.Buffer
	flags := flag.NewFlagSet("argocdapp2helmfile", flag.ContinueOnError)
	flags.SetOutput(&output)
	flags.BoolVar(&options.strict, "strict", false, "reject lossy conversions")
	flags.StringVar(&options.configPath, "config", "", "read conversion configuration from `path`")
	flags.BoolVar(
		&options.helpDiagnostics,
		"help-diagnostics",
		false,
		"print the diagnostics reference",
	)
	flags.BoolVar(
		&options.helpMapping,
		"help-application-mapping",
		false,
		"print the Application mapping reference",
	)
	flags.Usage = func() {
		fmt.Fprintln(&output, "Usage: argocdapp2helmfile [--strict] [--config PATH]")
		fmt.Fprintln(&output, "       argocdapp2helmfile --help-diagnostics")
		fmt.Fprintln(&output, "       argocdapp2helmfile --help-application-mapping")
		fmt.Fprintln(&output)
		fmt.Fprintln(&output, "Options:")
		flags.PrintDefaults()
	}
	if err := flags.Parse(args); err != nil {
		return commandOptions{}, output.String(), err
	}
	if flags.NArg() != 0 {
		return commandOptions{}, "", fmt.Errorf("unexpected argument %q", flags.Arg(0))
	}
	if options.helpDiagnostics && (options.strict || options.configPath != "") {
		return commandOptions{}, "", errors.New(
			"--help-diagnostics cannot be combined with --strict or --config",
		)
	}
	if options.helpMapping &&
		(options.strict || options.configPath != "" || options.helpDiagnostics) {
		return commandOptions{}, "", errors.New(
			"--help-application-mapping cannot be combined with " +
				"--strict, --config, or --help-diagnostics",
		)
	}
	return options, "", nil
}

func writeDiagnostic(stderr io.Writer, err error) {
	// Collapse annotated parser errors to one diagnostic line.
	message := strings.Join(strings.Fields(err.Error()), " ")
	fmt.Fprintf(stderr, "argocdapp2helmfile: %s\n", message)
}
