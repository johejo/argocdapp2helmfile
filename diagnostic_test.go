package main

import (
	"strconv"
	"strings"
	"testing"
)

func TestConvertWithDiagnosticsClassifiesSyncSettings(t *testing.T) {
	input := readTestdata(t, "diagnostics/cases.yaml")
	result, err := convertWithDiagnostics([]byte(input), nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(result.output) == 0 {
		t.Fatal("conversion output was empty")
	}

	var diagnostics []string
	for _, diagnostic := range result.diagnostics {
		diagnostics = append(diagnostics, diagnostic.String())
		if strings.Contains(diagnostic.String(), "\n") {
			t.Fatalf("diagnostic contains a newline: %q", diagnostic)
		}
	}
	if len(diagnostics) != 27 {
		t.Fatalf("diagnostic count = %d, want 27:\n%s", len(diagnostics), strings.Join(diagnostics, "\n"))
	}

	for index := range 12 {
		assertDiagnostic(
			t,
			diagnostics,
			`document 1: Application "critical-options": `+
				`spec.syncPolicy.syncOptions[`+strconv.Itoa(index)+`]: unconvertible`,
		)
	}

	for _, want := range []string{
		`spec.revisionHistoryLimit: approximate`,
		`spec.syncPolicy.automated.enabled: intentionally-ignored`,
		`spec.syncPolicy.automated.prune: unconvertible`,
		`spec.syncPolicy.automated.selfHeal: unconvertible`,
		`spec.syncPolicy.automated.allowEmpty: unconvertible`,
		`spec.syncPolicy.retry: unconvertible`,
		`spec.syncPolicy.syncOptions[0]: approximate`,
		`spec.syncPolicy.syncOptions[1]: intentionally-ignored`,
		`spec.syncPolicy.managedNamespaceMetadata: unconvertible`,
		`spec.ignoreDifferences: unconvertible`,
	} {
		assertDiagnostic(
			t,
			diagnostics,
			`document 2: Application "controller-settings": `+want,
		)
	}

	assertDiagnostic(t, diagnostics, `document 3: Application "duplicate-options": `+
		`spec.syncPolicy.syncOptions[0]: unconvertible`)
	assertDiagnostic(t, diagnostics, `document 3: Application "duplicate-options": `+
		`spec.syncPolicy.syncOptions[2]: unconvertible`)
	assertDiagnostic(t, diagnostics, `document 3: Application "duplicate-options": `+
		`spec.syncPolicy.syncOptions[4]: unconvertible`)
	assertDiagnostic(t, diagnostics, `document 3: Application "duplicate-options": `+
		`spec.syncPolicy.syncOptions[5]: unconvertible`)
	assertDiagnostic(t, diagnostics, `document 3: Application "duplicate-options": `+
		`spec.syncPolicy.syncOptions[6]: unconvertible: sync option "Validate" `+
		`has conflicting duplicate values`)
	for _, diagnostic := range diagnostics {
		if strings.Contains(diagnostic, `Application "defaults"`) ||
			strings.Contains(diagnostic, `Application "ineffective-migration"`) {
			t.Fatalf("default or ineffective setting produced a diagnostic: %s", diagnostic)
		}
	}
}

func TestApplicationSetDiagnosticsUseRenderedValuesAndGeneratorOrigins(t *testing.T) {
	input := readTestdata(t, "diagnostics/applicationset.yaml")
	result, err := convertWithDiagnostics([]byte(input), nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(result.diagnostics) != 2 {
		t.Fatalf("diagnostic count = %d, want 2", len(result.diagnostics))
	}
	wants := []string{
		`document 1: spec.generators[0].list.elements[0]: Application "api": ` +
			`spec.syncPolicy.syncOptions[0]: unconvertible`,
		`document 1: spec.generators[0].list.elements[1]: Application "worker": ` +
			`spec.syncPolicy.syncOptions[0]: approximate`,
	}
	for index, want := range wants {
		if !strings.Contains(result.diagnostics[index].String(), want) {
			t.Fatalf(
				"diagnostic %d = %q, want it to contain %q",
				index,
				result.diagnostics[index],
				want,
			)
		}
	}
	if !strings.Contains(string(result.output), "    createNamespace: true\n") {
		t.Fatalf("CreateNamespace conversion was not preserved:\n%s", result.output)
	}
}

func assertDiagnostic(t *testing.T, diagnostics []string, want string) {
	t.Helper()
	for _, diagnostic := range diagnostics {
		if strings.Contains(diagnostic, want) {
			return
		}
	}
	t.Errorf("no diagnostic contains %q:\n%s", want, strings.Join(diagnostics, "\n"))
}
