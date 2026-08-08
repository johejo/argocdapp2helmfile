package main

import (
	"bytes"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"runtime/debug"
	"strings"

	"github.com/johejo/argocdapp2helmfile/internal/reference"
)

//go:generate go run ./internal/cmd/gendocs

var version string

func main() {
	os.Exit(run(os.Args[1:], os.Stdin, os.Stdout, os.Stderr))
}

type commandOptions struct {
	strict            bool
	skipUnconvertible bool
	configPath        string
	kubeContextMode   kubeContextMode
	reference         *reference.Document
	showVersion       bool
}

type kubeContextMode uint8

const (
	// Keep mapped as the zero value so conversionOptions{} uses the safe default.
	kubeContextModeMapped kubeContextMode = iota
	kubeContextModeOmit
)

func (mode *kubeContextMode) Set(value string) error {
	switch value {
	case "mapped":
		*mode = kubeContextModeMapped
		return nil
	case "omit":
		*mode = kubeContextModeOmit
		return nil
	default:
		return errors.New(`must be "mapped" or "omit"`)
	}
}

func (mode kubeContextMode) String() string {
	switch mode {
	case kubeContextModeMapped:
		return "mapped"
	case kubeContextModeOmit:
		return "omit"
	default:
		return fmt.Sprintf("unknown(%d)", mode)
	}
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
	if options.showVersion {
		if _, err := fmt.Fprintf(stdout, "argocdapp2helmfile %s\n", resolvedVersion()); err != nil {
			writeDiagnostic(stderr, fmt.Errorf("write version: %w", err))
			return 1
		}
		return 0
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
		kubeContextMode:   options.kubeContextMode,
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
	flags.BoolVar(&options.showVersion, "version", false, "print version and exit")
	flags.BoolVar(&options.strict, "strict", false, "reject lossy conversions")
	flags.BoolVar(
		&options.skipUnconvertible,
		"skip-unconvertible",
		false,
		"convert what can be converted, reporting and omitting the rest, and exit 2",
	)
	flags.StringVar(&options.configPath, "config", "", "read conversion configuration from `path`")
	flags.Var(
		&options.kubeContextMode,
		"kube-context-mode",
		"set kubeContext handling to `mode` (mapped or omit; default mapped)",
	)
	selected := make([]bool, len(reference.Documents))
	for i, document := range reference.Documents {
		flags.BoolVar(&selected[i], document.Flag, false, document.Usage)
	}
	flags.Usage = func() {
		fmt.Fprintln(
			&output,
			"Usage: argocdapp2helmfile [--strict | --skip-unconvertible] [--config PATH] [--kube-context-mode MODE]",
		)
		fmt.Fprintln(&output, "       argocdapp2helmfile --version")
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
	var conversionFlags []string
	flags.Visit(func(item *flag.Flag) {
		switch item.Name {
		case "config", "kube-context-mode", "skip-unconvertible", "strict":
			conversionFlags = append(conversionFlags, "--"+item.Name)
		}
	})
	for i, chosen := range selected {
		if !chosen {
			continue
		}
		if options.reference != nil {
			return commandOptions{}, "", errors.New("only one reference can be printed at a time")
		}
		options.reference = &reference.Documents[i]
	}
	if options.reference != nil && len(conversionFlags) != 0 {
		return commandOptions{}, "", fmt.Errorf(
			"--%s cannot be combined with %s",
			options.reference.Flag,
			strings.Join(conversionFlags, ", "),
		)
	}
	if options.strict && options.skipUnconvertible {
		return commandOptions{}, "", errors.New(
			"--strict cannot be combined with --skip-unconvertible",
		)
	}
	return options, "", nil
}

func resolvedVersion() string {
	if version != "" {
		return version
	}
	if info, ok := debug.ReadBuildInfo(); ok && info.Main.Version != "" {
		return info.Main.Version
	}
	return "(devel)"
}

func writeDiagnostic(stderr io.Writer, err error) {
	fmt.Fprintf(stderr, "argocdapp2helmfile: %s\n", collapseMessage(err))
}

func collapseMessage(err error) string {
	return strings.Join(strings.Fields(err.Error()), " ")
}
