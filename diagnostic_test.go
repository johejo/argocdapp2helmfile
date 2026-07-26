package main

import (
	"regexp"
	"strconv"
	"strings"
	"testing"

	diagnosticrule "github.com/johejo/argocdapp2helmfile/internal/diagnostic"
)

// syncOptionSetting is the placeholder the catalog stores for rules that describe
// a malformed or unrecognized sync option rather than a single setting.
const syncOptionSetting = "spec.syncPolicy.syncOptions[index]"

var syncOptionPathPattern = regexp.MustCompile(`^spec\.syncPolicy\.syncOptions\[\d+\]$`)

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
	if got, want := strings.Join(diagnostics, "\n")+"\n",
		readTestdata(t, "diagnostics/expected.txt"); got != want {
		t.Fatalf("diagnostics changed:\n%s\nwant:\n%s", got, want)
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

func TestSyncOptionEvaluation(t *testing.T) {
	tests := []struct {
		name         string
		values       []any
		wantCategory string
		wantPath     string
		wantMessage  string
		wantCount    int
	}{
		{
			name:      "supported",
			values:    []any{"Validate=true"},
			wantCount: 0,
		},
		{
			name:         "approximate",
			values:       []any{"CreateNamespace=true"},
			wantCategory: "approximate",
			wantPath:     "spec.syncPolicy.syncOptions[0]",
			wantMessage:  "Helmfile createNamespace creates a namespace",
			wantCount:    1,
		},
		{
			name:         "intentionally ignored",
			values:       []any{"ApplyOutOfSyncOnly=true"},
			wantCategory: "intentionally-ignored",
			wantPath:     "spec.syncPolicy.syncOptions[0]",
			wantMessage:  "selective sync",
			wantCount:    1,
		},
		{
			name:         "unconvertible",
			values:       []any{"Validate=false"},
			wantCategory: "unconvertible",
			wantPath:     "spec.syncPolicy.syncOptions[0]",
			wantMessage:  `"Validate=false" has no Helmfile equivalent`,
			wantCount:    1,
		},
		{
			name:         "unknown option",
			values:       []any{"validate=true"},
			wantCategory: "unconvertible",
			wantPath:     "spec.syncPolicy.syncOptions[0]",
			wantMessage:  `"validate=true" is unknown or cannot be interpreted`,
			wantCount:    1,
		},
		{
			name:         "unknown value",
			values:       []any{"Validate=TRUE"},
			wantCategory: "unconvertible",
			wantPath:     "spec.syncPolicy.syncOptions[0]",
			wantMessage:  `"Validate=TRUE" is unknown or cannot be interpreted`,
			wantCount:    1,
		},
		{
			name:         "malformed",
			values:       []any{"Validate"},
			wantCategory: "unconvertible",
			wantPath:     "spec.syncPolicy.syncOptions[0]",
			wantMessage:  `"Validate" is unknown or cannot be interpreted`,
			wantCount:    1,
		},
		{
			name:         "non-string",
			values:       []any{true},
			wantCategory: "unconvertible",
			wantPath:     "spec.syncPolicy.syncOptions[0]",
			wantMessage:  "sync option must be a string",
			wantCount:    1,
		},
		{
			name:         "identical duplicate",
			values:       []any{"Validate=false", "Validate=false"},
			wantCategory: "unconvertible",
			wantPath:     "spec.syncPolicy.syncOptions[0]",
			wantMessage:  `"Validate=false" has no Helmfile equivalent`,
			wantCount:    1,
		},
		{
			name:         "conflicting duplicate",
			values:       []any{"Validate=false", "Validate=true"},
			wantCategory: "unconvertible",
			wantPath:     "spec.syncPolicy.syncOptions[0]",
			wantMessage:  `"Validate" has conflicting duplicate values`,
			wantCount:    1,
		},
		{
			name: "migration dependency",
			values: []any{
				"ServerSideApply=true",
				"ClientSideApplyMigration=false",
			},
			wantCategory: "unconvertible",
			wantPath:     "spec.syncPolicy.syncOptions[0]",
			wantMessage:  `"ServerSideApply=true" has no Helmfile equivalent`,
			wantCount:    2,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			audit := applicationAudit{name: "app"}
			audit.syncOptions(map[string]any{"syncOptions": test.values})
			if len(audit.diagnostics) != test.wantCount {
				t.Fatalf(
					"diagnostic count = %d, want %d: %#v",
					len(audit.diagnostics),
					test.wantCount,
					audit.diagnostics,
				)
			}
			if test.wantCount == 0 {
				return
			}
			got := audit.diagnostics[0]
			if string(got.category) != test.wantCategory ||
				got.path != test.wantPath ||
				!strings.Contains(got.message, test.wantMessage) {
				t.Errorf(
					"diagnostic = category %q, path %q, message %q",
					got.category,
					got.path,
					got.message,
				)
			}
		})
	}
}

// Enumerating the catalog rather than a fixed list makes a newly added sync option
// rule fail here unless applicationAudit.syncOption passes its format arguments.
func TestSyncOptionMessagesRenderEveryFormatArgument(t *testing.T) {
	syncOptionKeys := make(map[string]struct{})
	seen := make(map[diagnosticrule.RuleID]struct{})
	for _, option := range diagnosticrule.SyncOptions() {
		syncOptionKeys[option.Key] = struct{}{}
		for _, value := range option.Values {
			text := option.Key + "=" + value.Value
			// ClientSideApplyMigration=false only reports while server-side apply
			// is enabled, so each option is also paired with it.
			for _, values := range [][]any{{text}, {text, "ServerSideApply=true"}} {
				audit := applicationAudit{name: "app"}
				audit.syncOptions(map[string]any{"syncOptions": values})
				for _, diagnostic := range audit.diagnostics {
					assertRenderedMessage(t, diagnostic)
					seen[diagnostic.rule] = struct{}{}
				}
			}
		}
	}

	// Guards against the loop above passing vacuously.
	for _, rule := range diagnosticrule.Rules() {
		if rule.Disposition == diagnosticrule.Supported {
			continue
		}
		if _, isSyncOption := syncOptionKeys[rule.Setting]; !isSyncOption {
			continue
		}
		if _, exists := seen[rule.ID]; !exists {
			t.Errorf("sync option rule %q was never rendered", rule.ID)
		}
	}
}

// Keeps the paths reported by diagnostic.go from drifting away from the Setting
// the catalog documents for the same rule.
func TestDiagnosticPathsMatchTheRuleCatalog(t *testing.T) {
	syncOptionKeys := make(map[string]struct{})
	for _, option := range diagnosticrule.SyncOptions() {
		syncOptionKeys[option.Key] = struct{}{}
	}

	for _, name := range []string{"diagnostics/cases.yaml", "diagnostics/applicationset.yaml"} {
		result, err := convertWithDiagnostics([]byte(readTestdata(t, name)), nil)
		if err != nil {
			t.Fatal(err)
		}
		if len(result.diagnostics) == 0 {
			t.Fatalf("%s produced no diagnostics", name)
		}
		for _, diagnostic := range result.diagnostics {
			assertRenderedMessage(t, diagnostic)
			rule := diagnosticrule.MustLookup(diagnostic.rule)
			_, isSyncOption := syncOptionKeys[rule.Setting]
			if isSyncOption || rule.Setting == syncOptionSetting {
				if !syncOptionPathPattern.MatchString(diagnostic.path) {
					t.Errorf(
						"rule %q reported path %q, want an indexed sync option path",
						diagnostic.rule,
						diagnostic.path,
					)
				}
				continue
			}
			if diagnostic.path != rule.Setting {
				t.Errorf(
					"rule %q reported path %q, want %q from the catalog",
					diagnostic.rule,
					diagnostic.path,
					rule.Setting,
				)
			}
		}
	}
}

// No catalog message contains a literal percent sign, so any percent that
// survives rendering is one of fmt's argument mismatch markers.
func assertRenderedMessage(t *testing.T, diagnostic conversionDiagnostic) {
	t.Helper()
	if strings.Contains(diagnostic.message, "%") {
		t.Errorf(
			"rule %q rendered an unsubstituted format verb: %q",
			diagnostic.rule,
			diagnostic.message,
		)
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
