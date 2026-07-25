package applicationmapping

import (
	"bytes"
	"slices"
	"strings"
	"testing"
)

func TestCatalogIsValid(t *testing.T) {
	ids := make(map[ID]bool)
	inputs := make(map[string]bool)
	helmOptions := make(map[string]bool)
	validKinds := []HelmValueKind{
		String,
		Boolean,
		StringSequence,
		InlineValues,
		RawValues,
		Parameters,
		FileParameters,
		Ignored,
	}

	for index, entry := range Entries() {
		if entry.ID == "" || ids[entry.ID] {
			t.Errorf("entry %d has an empty or duplicate ID %q", index, entry.ID)
		}
		ids[entry.ID] = true
		if strings.TrimSpace(entry.Input) == "" || inputs[entry.Input] {
			t.Errorf("entry %q has an empty or duplicate input %q", entry.ID, entry.Input)
		}
		inputs[entry.Input] = true
		if strings.TrimSpace(entry.Output) == "" {
			t.Errorf("entry %q has no output description", entry.ID)
		}
		if entry.HelmOption == "" {
			if entry.HelmValueKind != "" || entry.AllowEmpty {
				t.Errorf("non-Helm entry %q declares Helm value behavior", entry.ID)
			}
			continue
		}
		if helmOptions[entry.HelmOption] {
			t.Errorf("duplicate Helm option %q", entry.HelmOption)
		}
		helmOptions[entry.HelmOption] = true
		if !slices.Contains(validKinds, entry.HelmValueKind) {
			t.Errorf(
				"Helm option %q has invalid value kind %q",
				entry.HelmOption,
				entry.HelmValueKind,
			)
		}
		if entry.AllowEmpty && entry.HelmValueKind == Ignored {
			t.Errorf("ignored Helm option %q redundantly allows empty values", entry.HelmOption)
		}
	}

	for _, entry := range Entries() {
		for _, reference := range entry.References {
			if !ids[reference] {
				t.Errorf("entry %q references unknown ID %q", entry.ID, reference)
			}
		}
	}
}

func TestHelmOptionLookupCoversCatalog(t *testing.T) {
	for _, entry := range Entries() {
		if entry.HelmOption == "" {
			continue
		}
		got, ok := LookupHelmOption(entry.HelmOption)
		if !ok || got.ID != entry.ID {
			t.Errorf("LookupHelmOption(%q) = %#v, %v", entry.HelmOption, got, ok)
		}
	}
	if _, ok := LookupHelmOption("unknown"); ok {
		t.Fatal("unknown Helm option was found")
	}
}

func TestMarkdownIsDeterministicAndComplete(t *testing.T) {
	first := Markdown()
	second := Markdown()
	if !bytes.Equal(first, second) {
		t.Fatal("Markdown output is not deterministic")
	}
	for _, entry := range Entries() {
		if !bytes.Contains(first, []byte(entry.Input)) ||
			!bytes.Contains(first, []byte(entry.Output)) {
			t.Errorf("Markdown output does not contain entry %q", entry.ID)
		}
	}
}
