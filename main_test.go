package main

import (
	"bytes"
	"strings"
	"testing"
)

func TestRunErrorsAreAtomic(t *testing.T) {
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
