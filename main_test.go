package main

import (
	"bytes"
	"errors"
	"io"
	"runtime/debug"
	"strings"
	"testing"

	"github.com/johejo/argocdapp2helmfile/internal/applicationmapping"
	"github.com/johejo/argocdapp2helmfile/internal/applicationset"
	"github.com/johejo/argocdapp2helmfile/internal/diagnostic"
	"github.com/johejo/argocdapp2helmfile/internal/reference"
)

func TestRunErrorsAreAtomic(t *testing.T) {
	exampleApplication := readTestdata(t, "example/application.yaml")
	tests := []struct {
		name  string
		args  []string
		input string
	}{
		{name: "conversion", input: "invalid: ["},
		{name: "later document", input: minimalApplication("") + "---\ninvalid: ["},
		{name: "duplicate release", input: minimalApplication("") + "---\n" + minimalApplication("")},
		{name: "shared option conflict", input: minimalApplication("    helm:\n      skipCrds: true\n") + "---\n" + strings.Replace(minimalApplication(""), "name: app", "name: second", 1)},
		{name: "argument", args: []string{"unexpected"}, input: exampleApplication},
		{name: "removed source map flag", args: []string{"--source-map", "sources.yaml"}, input: exampleApplication},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			var stdout, stderr bytes.Buffer
			if code := run(test.args, strings.NewReader(test.input), &stdout, &stderr); code != 1 {
				t.Fatalf("exit code = %d, want 1", code)
			}
			if stdout.Len() != 0 {
				t.Fatalf("stdout was not empty: %q", stdout.String())
			}
			if !strings.HasPrefix(stderr.String(), "argocdapp2helmfile: ") || strings.Count(stderr.String(), "\n") != 1 {
				t.Fatalf("stderr is not one diagnostic line: %q", stderr.String())
			}
		})
	}
}

func TestRunWritesWarningsAndHelmfile(t *testing.T) {
	input := readTestdata(t, "revision-history/application.yaml")
	var stdout, stderr bytes.Buffer
	if code := run(nil, strings.NewReader(input), &stdout, &stderr); code != 0 {
		t.Fatalf("exit code = %d, want 0: %s", code, stderr.String())
	}
	if want := readTestdata(t, "revision-history/helmfile.yaml"); stdout.String() != want {
		t.Fatalf("unexpected stdout:\n%s\nwant:\n%s", stdout.String(), want)
	}
	if strings.Count(stderr.String(), "\n") != 2 {
		t.Fatalf("stderr does not contain two one-line warnings:\n%s", stderr.String())
	}
	for _, name := range []string{"frontend", "worker"} {
		want := `argocdapp2helmfile: warning: document `
		if !strings.Contains(stderr.String(), want) ||
			!strings.Contains(stderr.String(), `Application "`+name+`"`) ||
			!strings.Contains(stderr.String(), "spec.revisionHistoryLimit: approximate:") {
			t.Fatalf("stderr is missing the warning for %q:\n%s", name, stderr.String())
		}
	}
}

func TestRunStrictWritesAllDiagnosticsWithoutHelmfile(t *testing.T) {
	input := readTestdata(t, "revision-history/application.yaml")
	var stdout, stderr bytes.Buffer
	if code := run([]string{"--strict"}, strings.NewReader(input), &stdout, &stderr); code != 1 {
		t.Fatalf("exit code = %d, want 1", code)
	}
	if stdout.Len() != 0 {
		t.Fatalf("stdout was not empty: %q", stdout.String())
	}
	if strings.Count(stderr.String(), "\n") != 2 ||
		strings.Count(stderr.String(), "argocdapp2helmfile: error:") != 2 {
		t.Fatalf("stderr does not contain two strict diagnostics:\n%s", stderr.String())
	}
}

func TestRunConversionErrorSuppressesDiagnostics(t *testing.T) {
	input := readTestdata(t, "diagnostics/atomic-error.yaml")
	var stdout, stderr bytes.Buffer
	if code := run(nil, strings.NewReader(input), &stdout, &stderr); code != 1 {
		t.Fatalf("exit code = %d, want 1", code)
	}
	if stdout.Len() != 0 {
		t.Fatalf("stdout was not empty: %q", stdout.String())
	}
	if strings.Contains(stderr.String(), "warning:") {
		t.Fatalf("conversion diagnostics were written before an error: %s", stderr.String())
	}
	if strings.Count(stderr.String(), "\n") != 1 {
		t.Fatalf("stderr is not one error line: %q", stderr.String())
	}
}

func TestRunSkipUnconvertibleWritesPartialHelmfile(t *testing.T) {
	input := readTestdata(t, "skip-unconvertible/application.yaml")
	var stdout, stderr bytes.Buffer
	if code := run([]string{"--skip-unconvertible"}, strings.NewReader(input), &stdout, &stderr); code != 2 {
		t.Fatalf("exit code = %d, want 2: %s", code, stderr.String())
	}
	if want := readTestdata(t, "skip-unconvertible/helmfile.yaml"); stdout.String() != want {
		t.Fatalf("unexpected stdout:\n%s\nwant:\n%s", stdout.String(), want)
	}
	if strings.Count(stderr.String(), "\n") != 2 ||
		strings.Count(stderr.String(), "argocdapp2helmfile: skipped:") != 2 {
		t.Fatalf("stderr does not contain two skip reports:\n%s", stderr.String())
	}
	for _, want := range []string{
		"document 2: spec.generators[0].scmProvider",
		"document 4: spec.revisionHistoryLimit",
	} {
		if !strings.Contains(stderr.String(), want) {
			t.Fatalf("stderr is missing %q:\n%s", want, stderr.String())
		}
	}
}

func TestRunSkipUnconvertibleDiscardsSkippedApplicationState(t *testing.T) {
	input := readTestdata(t, "skip-unconvertible/rollback.yaml")
	var stdout, stderr bytes.Buffer
	if code := run([]string{"--skip-unconvertible"}, strings.NewReader(input), &stdout, &stderr); code != 2 {
		t.Fatalf("exit code = %d, want 2: %s", code, stderr.String())
	}
	if want := readTestdata(t, "skip-unconvertible/rollback-helmfile.yaml"); stdout.String() != want {
		t.Fatalf("unexpected stdout:\n%s\nwant:\n%s", stdout.String(), want)
	}
	if strings.Count(stderr.String(), "\n") != 1 {
		t.Fatalf("a skipped Application rejected a later one:\n%s", stderr.String())
	}
}

func TestRunSkipUnconvertibleFailsWhenNothingConverts(t *testing.T) {
	input := readTestdata(t, "skip-unconvertible/all-skipped.yaml")
	var stdout, stderr bytes.Buffer
	if code := run([]string{"--skip-unconvertible"}, strings.NewReader(input), &stdout, &stderr); code != 1 {
		t.Fatalf("exit code = %d, want 1", code)
	}
	if stdout.Len() != 0 {
		t.Fatalf("a helmfile with no releases was written: %q", stdout.String())
	}
	if !strings.Contains(stderr.String(), "every input was skipped: 2 of 2") {
		t.Fatalf("stderr does not report the skipped total:\n%s", stderr.String())
	}
}

func TestRunWithoutSkipUnconvertibleStaysAtomic(t *testing.T) {
	for _, name := range []string{
		"skip-unconvertible/application.yaml",
		"skip-unconvertible/rollback.yaml",
		"skip-unconvertible/all-skipped.yaml",
	} {
		t.Run(name, func(t *testing.T) {
			var stdout, stderr bytes.Buffer
			code := run(nil, strings.NewReader(readTestdata(t, name)), &stdout, &stderr)
			if code != 1 {
				t.Fatalf("exit code = %d, want 1", code)
			}
			if stdout.Len() != 0 {
				t.Fatalf("stdout was not empty: %q", stdout.String())
			}
			if strings.Contains(stderr.String(), "skipped") {
				t.Fatalf("input was skipped without the flag:\n%s", stderr.String())
			}
		})
	}
}

func TestParseArgs(t *testing.T) {
	tests := []struct {
		args              []string
		strict            bool
		skipUnconvertible bool
		configPath        string
	}{
		{args: []string{"--strict", "--config", "config.yaml"}, strict: true, configPath: "config.yaml"},
		{args: []string{"-config", "config.yaml", "-strict"}, strict: true, configPath: "config.yaml"},
		{args: []string{"--config=first.yaml", "--config=last.yaml"}, configPath: "last.yaml"},
		{args: []string{"--strict=true", "--strict=false"}},
		{args: []string{"--skip-unconvertible"}, skipUnconvertible: true},
		{
			args:              []string{"-skip-unconvertible", "--config", "config.yaml"},
			skipUnconvertible: true,
			configPath:        "config.yaml",
		},
		{args: []string{"--skip-unconvertible=false", "--strict"}, strict: true},
	}
	for _, test := range tests {
		options, _, err := parseArgs(test.args)
		if err != nil || options.strict != test.strict ||
			options.skipUnconvertible != test.skipUnconvertible ||
			options.configPath != test.configPath {
			t.Errorf("parseArgs(%q) = %#v, %v", test.args, options, err)
		}
	}
	for _, args := range [][]string{
		{"--config"},
		{"--strict", "--skip-unconvertible"},
		{"--help-diagnostics", "--skip-unconvertible"},
		{"--help-diagnostics", "--strict"},
		{"--config", "a", "--help-diagnostics"},
		{"--help-application-mapping", "--strict"},
		{"--config", "a", "--help-application-mapping"},
		{"--help-applicationset", "--strict"},
		{"--config", "a", "--help-applicationset"},
		{"--help-diagnostics", "--help-application-mapping"},
		{"--help-application-mapping", "--help-applicationset"},
		{"unknown"},
		{"--unknown"},
	} {
		if _, _, err := parseArgs(args); err == nil {
			t.Errorf("parseArgs(%q) unexpectedly succeeded", args)
		}
	}
}

func TestRunHelp(t *testing.T) {
	for _, arg := range []string{"-h", "--help"} {
		t.Run(arg, func(t *testing.T) {
			var stdout, stderr bytes.Buffer
			code := run([]string{arg}, errorReader{}, &stdout, &stderr)
			if code != 0 {
				t.Fatalf("exit code = %d, want 0: %s", code, stderr.String())
			}
			for _, want := range []string{
				"Usage: argocdapp2helmfile",
				"-config path",
				"-help-application-mapping",
				"-help-applicationset",
				"-help-diagnostics",
				"-strict",
				"-version",
			} {
				if !strings.Contains(stdout.String(), want) {
					t.Errorf("help output does not contain %q:\n%s", want, stdout.String())
				}
			}
			if stderr.Len() != 0 {
				t.Errorf("stderr was not empty: %q", stderr.String())
			}
		})
	}
}

func TestRunVersion(t *testing.T) {
	oldVersion := version
	version = "v1.2.3"
	t.Cleanup(func() { version = oldVersion })

	for _, arg := range []string{"-version", "--version"} {
		t.Run(arg, func(t *testing.T) {
			var stdout, stderr bytes.Buffer
			code := run([]string{arg}, errorReader{}, &stdout, &stderr)
			if code != 0 {
				t.Fatalf("exit code = %d, want 0: %s", code, stderr.String())
			}
			if got, want := stdout.String(), "argocdapp2helmfile v1.2.3\n"; got != want {
				t.Errorf("stdout = %q, want %q", got, want)
			}
			if stderr.Len() != 0 {
				t.Errorf("stderr was not empty: %q", stderr.String())
			}
		})
	}
}

func TestResolvedVersionFallsBackToBuildInfo(t *testing.T) {
	oldVersion := version
	version = ""
	t.Cleanup(func() { version = oldVersion })

	want := "(devel)"
	if info, ok := debug.ReadBuildInfo(); ok && info.Main.Version != "" {
		want = info.Main.Version
	}
	if got := resolvedVersion(); got != want {
		t.Errorf("resolvedVersion() = %q, want %q", got, want)
	}
}

func TestRunVersionReportsWriteFailure(t *testing.T) {
	var stderr bytes.Buffer
	if code := run([]string{"--version"}, errorReader{}, errorWriter{}, &stderr); code != 1 {
		t.Fatalf("exit code = %d, want 1", code)
	}
	if got := stderr.String(); !strings.Contains(got, "write version: write failed") ||
		strings.Count(got, "\n") != 1 {
		t.Fatalf("unexpected stderr: %q", got)
	}
}

func TestRunHelpReportsWriteFailure(t *testing.T) {
	var stderr bytes.Buffer
	if code := run([]string{"--help"}, errorReader{}, errorWriter{}, &stderr); code != 1 {
		t.Fatalf("exit code = %d, want 1", code)
	}
	if got := stderr.String(); !strings.Contains(got, "write help: write failed") ||
		strings.Count(got, "\n") != 1 {
		t.Fatalf("unexpected stderr: %q", got)
	}
}

func TestRunRejectsPositionalArguments(t *testing.T) {
	for _, args := range [][]string{
		{"application.yaml"},
		{"--", "application.yaml"},
	} {
		var stdout, stderr bytes.Buffer
		if code := run(args, strings.NewReader("invalid: ["), &stdout, &stderr); code != 1 {
			t.Errorf("run(%q) exit code = %d, want 1", args, code)
		}
		if stdout.Len() != 0 {
			t.Errorf("run(%q) wrote stdout: %q", args, stdout.String())
		}
	}
}

// referenceFlags pins every --help flag to the document it prints.
var referenceFlags = []struct {
	flag     string
	name     string
	document func() []byte
}{
	{"help-diagnostics", "diagnostics", diagnostic.Markdown},
	{"help-application-mapping", "application mapping", applicationmapping.Markdown},
	{"help-applicationset", "ApplicationSet", applicationset.Markdown},
}

func TestReferenceFlagsCoverEveryReference(t *testing.T) {
	if len(referenceFlags) != len(reference.Documents) {
		t.Fatalf(
			"%d reference flags cover %d references",
			len(referenceFlags),
			len(reference.Documents),
		)
	}
	for i, document := range reference.Documents {
		if referenceFlags[i].flag != document.Flag || referenceFlags[i].name != document.Name {
			t.Errorf("reference %q is not covered by %#v", document.Flag, referenceFlags[i])
		}
	}
}

func TestRunReferenceDoesNotReadInput(t *testing.T) {
	for _, test := range referenceFlags {
		t.Run(test.flag, func(t *testing.T) {
			var stdout, stderr bytes.Buffer
			code := run([]string{"--" + test.flag}, errorReader{}, &stdout, &stderr)
			if code != 0 {
				t.Fatalf("exit code = %d, want 0: %s", code, stderr.String())
			}
			if !bytes.Equal(stdout.Bytes(), test.document()) {
				t.Fatalf("--%s output differs from the renderer", test.flag)
			}
			if stderr.Len() != 0 {
				t.Fatalf("stderr was not empty: %q", stderr.String())
			}
		})
	}
}

func TestRunReferenceRejectsOtherArguments(t *testing.T) {
	for i, test := range referenceFlags {
		t.Run(test.flag, func(t *testing.T) {
			flag := "--" + test.flag
			other := "--" + referenceFlags[(i+1)%len(referenceFlags)].flag
			for _, args := range [][]string{
				{flag, "--strict"},
				{"--strict", flag},
				{flag, "--config", "config.yaml"},
				{flag, other},
				{flag, "application.yaml"},
			} {
				var stdout, stderr bytes.Buffer
				if code := run(args, strings.NewReader("invalid: ["), &stdout, &stderr); code != 1 {
					t.Errorf("run(%q) exit code = %d, want 1", args, code)
				}
				if stdout.Len() != 0 {
					t.Errorf("run(%q) wrote stdout: %q", args, stdout.String())
				}
			}
		})
	}
}

func TestRunReferenceReportsWriteFailure(t *testing.T) {
	for _, test := range referenceFlags {
		t.Run(test.flag, func(t *testing.T) {
			var stderr bytes.Buffer
			code := run(
				[]string{"--" + test.flag},
				strings.NewReader("invalid: ["),
				errorWriter{},
				&stderr,
			)
			if code != 1 {
				t.Fatalf("exit code = %d, want 1", code)
			}
			want := "write " + test.name + " reference: write failed"
			if got := stderr.String(); !strings.Contains(got, want) ||
				strings.Count(got, "\n") != 1 {
				t.Fatalf("unexpected stderr: %q", got)
			}
		})
	}
}

type errorReader struct{}

func (errorReader) Read([]byte) (int, error) {
	return 0, errors.New("read failed")
}

type errorWriter struct{}

func (errorWriter) Write([]byte) (int, error) {
	return 0, errors.New("write failed")
}

var _ io.Reader = errorReader{}
var _ io.Writer = errorWriter{}

func minimalApplication(helm string) string {
	return `apiVersion: argoproj.io/v1alpha1
kind: Application
metadata:
  name: app
spec:
  source:
    repoURL: https://example.com/charts
    chart: chart
    targetRevision: 1.2.3
` + helm
}
