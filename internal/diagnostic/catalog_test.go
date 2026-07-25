package diagnostic

import (
	"bytes"
	"slices"
	"testing"
)

func TestCatalogIsValidAndDeterministic(t *testing.T) {
	validDispositions := []Disposition{
		Supported,
		Approximate,
		IntentionallyIgnored,
		Unconvertible,
		ConversionError,
	}
	seen := make(map[RuleID]struct{})
	for _, rule := range Rules() {
		if rule.ID == "" {
			t.Error("rule has an empty ID")
		}
		if _, exists := seen[rule.ID]; exists {
			t.Errorf("duplicate rule ID %q", rule.ID)
		}
		seen[rule.ID] = struct{}{}
		if !slices.Contains(validDispositions, rule.Disposition) {
			t.Errorf("rule %q has invalid disposition %q", rule.ID, rule.Disposition)
		}
		if rule.Setting == "" || rule.Condition == "" || rule.Description == "" {
			t.Errorf("rule %q is missing reference content", rule.ID)
		}
		if rule.Disposition != Supported && rule.Message == "" {
			t.Errorf("rule %q is missing its diagnostic or error message", rule.ID)
		}
	}

	first := Markdown()
	second := Markdown()
	if !bytes.Equal(first, second) {
		t.Error("Markdown rendering order is not deterministic")
	}

	seenKeys := make(map[string]struct{})
	for _, option := range SyncOptions() {
		if _, exists := seenKeys[option.Key]; exists {
			t.Errorf("duplicate sync option key %q", option.Key)
		}
		seenKeys[option.Key] = struct{}{}
		seenValues := make(map[string]struct{})
		for _, value := range option.Values {
			if _, exists := seenValues[value.Value]; exists {
				t.Errorf("sync option %q has duplicate value %q", option.Key, value.Value)
			}
			seenValues[value.Value] = struct{}{}
			rule, exists := Lookup(value.Rule)
			if !exists {
				t.Errorf("sync option %q value %q refers to unknown rule %q", option.Key, value.Value, value.Rule)
			} else if rule.Setting != option.Key {
				t.Errorf(
					"sync option %q value %q refers to rule for %q",
					option.Key,
					value.Value,
					rule.Setting,
				)
			}
		}
	}
}

func TestSyncOptionCatalog(t *testing.T) {
	tests := []struct {
		name        string
		key         string
		value       string
		wantRule    RuleID
		wantKey     bool
		wantValue   bool
		disposition Disposition
	}{
		{"supported", "Validate", "true", ValidateTrue, true, true, Supported},
		{"approximate", "CreateNamespace", "true", CreateNamespaceTrue, true, true, Approximate},
		{"ignored", "ApplyOutOfSyncOnly", "true", ApplyOutOfSyncOnlyTrue, true, true, IntentionallyIgnored},
		{"unconvertible", "Validate", "false", ValidateFalse, true, true, Unconvertible},
		{"unknown value", "Validate", "TRUE", "", true, false, ""},
		{"unknown option", "validate", "true", "", false, false, ""},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			ruleID, knownKey, knownValue := LookupSyncOption(test.key, test.value)
			if ruleID != test.wantRule || knownKey != test.wantKey || knownValue != test.wantValue {
				t.Fatalf(
					"LookupSyncOption(%q, %q) = %q, %t, %t; want %q, %t, %t",
					test.key,
					test.value,
					ruleID,
					knownKey,
					knownValue,
					test.wantRule,
					test.wantKey,
					test.wantValue,
				)
			}
			if test.wantRule != "" &&
				MustLookup(test.wantRule).Disposition != test.disposition {
				t.Errorf("rule %q has the wrong disposition", test.wantRule)
			}
		})
	}
}
