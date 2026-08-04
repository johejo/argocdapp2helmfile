package main

import (
	"bytes"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/johejo/argocdapp2helmfile/internal/reference"
)

//go:generate go run ./internal/cmd/gendocs

func main() {
	os.Exit(run(os.Args[1:], os.Stdin, os.Stdout, os.Stderr))
}

type commandOptions struct {
	strict            bool
	skipUnconvertible bool
	configPath        string
	reference         *reference.Document
}

const (
	exitOK      = 0
	exitFailure = 1
	exitPartial = 2
)

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
	if options.reference != nil {
		if _, err := stdout.Write(options.reference.Render()); err != nil {
			writeDiagnostic(
				stderr,
				fmt.Errorf("write %s reference: %w", options.reference.Name, err),
			)
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
	result, err := convertWithDiagnostics(input, config, conversionOptions{
		skipUnconvertible: options.skipUnconvertible,
	})
	if err != nil {
		writeDiagnostic(stderr, err)
		return exitFailure
	}
	for _, skipped := range result.skipped {
		fmt.Fprintf(stderr, "argocdapp2helmfile: skipped: %s\n", collapseMessage(skipped))
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
		return exitFailure
	}
	if _, err := stdout.Write(result.output); err != nil {
		writeDiagnostic(stderr, fmt.Errorf("write output: %w", err))
		return exitFailure
	}
	if len(result.skipped) != 0 {
		return exitPartial
	}
	return exitOK
}

func parseArgs(args []string) (commandOptions, string, error) {
	var options commandOptions
	var output bytes.Buffer
	flags := flag.NewFlagSet("argocdapp2helmfile", flag.ContinueOnError)
	flags.SetOutput(&output)
	flags.BoolVar(&options.strict, "strict", false, "reject lossy conversions")
	flags.BoolVar(
		&options.skipUnconvertible,
		"skip-unconvertible",
		false,
		"convert what can be converted, reporting and omitting the rest, and exit 2",
	)
	flags.StringVar(&options.configPath, "config", "", "read conversion configuration from `path`")
	selected := make([]bool, len(reference.Documents))
	for i, document := range reference.Documents {
		flags.BoolVar(&selected[i], document.Flag, false, document.Usage)
	}
	flags.Usage = func() {
		fmt.Fprintln(
			&output,
			"Usage: argocdapp2helmfile [--strict | --skip-unconvertible] [--config PATH]",
		)
		for _, document := range reference.Documents {
			fmt.Fprintf(&output, "       argocdapp2helmfile --%s\n", document.Flag)
		}
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
	for i, chosen := range selected {
		if !chosen {
			continue
		}
		if options.reference != nil {
			return commandOptions{}, "", errors.New("only one reference can be printed at a time")
		}
		options.reference = &reference.Documents[i]
	}
	if options.reference != nil &&
		(options.strict || options.skipUnconvertible || options.configPath != "") {
		return commandOptions{}, "", fmt.Errorf(
			"--%s cannot be combined with --strict, --skip-unconvertible, or --config",
			options.reference.Flag,
		)
	}
	if options.strict && options.skipUnconvertible {
		return commandOptions{}, "", errors.New(
			"--strict cannot be combined with --skip-unconvertible",
		)
	}
	return options, "", nil
}

func writeDiagnostic(stderr io.Writer, err error) {
	fmt.Fprintf(stderr, "argocdapp2helmfile: %s\n", collapseMessage(err))
}

func collapseMessage(err error) string {
	return strings.Join(strings.Fields(err.Error()), " ")
}
