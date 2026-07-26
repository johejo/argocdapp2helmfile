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
	"github.com/johejo/argocdapp2helmfile/internal/applicationset"
	"github.com/johejo/argocdapp2helmfile/internal/diagnostic"
)

//go:generate go run ./internal/cmd/gendiagnostics
//go:generate go run ./internal/cmd/genapplicationmapping
//go:generate go run ./internal/cmd/genapplicationset

func main() {
	os.Exit(run(os.Args[1:], os.Stdin, os.Stdout, os.Stderr))
}

type commandOptions struct {
	strict     bool
	configPath string
	reference  *reference
}

// reference is one generated document a --help flag prints instead of
// converting. usage reads in help output, name inside an error message.
type reference struct {
	flag   string
	usage  string
	name   string
	render func() []byte
}

var references = []reference{
	{
		flag:   "help-diagnostics",
		usage:  "print the diagnostics reference",
		name:   "diagnostics",
		render: diagnostic.Markdown,
	},
	{
		flag:   "help-application-mapping",
		usage:  "print the Application mapping reference",
		name:   "application mapping",
		render: applicationmapping.Markdown,
	},
	{
		flag:   "help-applicationset",
		usage:  "print the ApplicationSet reference",
		name:   "ApplicationSet",
		render: applicationset.Markdown,
	},
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
	if options.reference != nil {
		if _, err := stdout.Write(options.reference.render()); err != nil {
			writeDiagnostic(
				stderr,
				fmt.Errorf("write %s reference: %w", options.reference.name, err),
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
	selected := make([]bool, len(references))
	for i, item := range references {
		flags.BoolVar(&selected[i], item.flag, false, item.usage)
	}
	flags.Usage = func() {
		fmt.Fprintln(&output, "Usage: argocdapp2helmfile [--strict] [--config PATH]")
		for _, item := range references {
			fmt.Fprintf(&output, "       argocdapp2helmfile --%s\n", item.flag)
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
		options.reference = &references[i]
	}
	if options.reference != nil && (options.strict || options.configPath != "") {
		return commandOptions{}, "", fmt.Errorf(
			"--%s cannot be combined with --strict or --config",
			options.reference.flag,
		)
	}
	return options, "", nil
}

func writeDiagnostic(stderr io.Writer, err error) {
	// Collapse annotated parser errors to one diagnostic line.
	message := strings.Join(strings.Fields(err.Error()), " ")
	fmt.Fprintf(stderr, "argocdapp2helmfile: %s\n", message)
}
