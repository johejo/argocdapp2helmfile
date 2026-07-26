package applicationmapping

import (
	"bytes"
	"slices"
	"strings"
	"testing"
	"unicode"
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

func TestKustomizeCatalogIsValid(t *testing.T) {
	validKinds := []KustomizeValueKind{
		KustomizeString,
		KustomizeBoolean,
		KustomizeStringMap,
		KustomizeImages,
		KustomizeReplicas,
		KustomizeUnsupported,
	}
	names := make(map[string]bool)

	for index, option := range KustomizeOptions() {
		if option.Name == "" || names[option.Name] ||
			strings.ContainsFunc(option.Name, unicode.IsSpace) {
			t.Errorf("option %d has an empty, duplicate, or spaced name %q", index, option.Name)
		}
		names[option.Name] = true
		if !slices.Contains(validKinds, option.ValueKind) {
			t.Errorf("option %q has invalid value kind %q", option.Name, option.ValueKind)
		}
		if option.ValueKind == KustomizeUnsupported {
			if option.Reason == "" || option.Output != "" {
				t.Errorf("unsupported option %q must describe only a reason", option.Name)
			}
			// Reasons reach plain-text errors, where an identifier cannot be marked
			// up and a lowercase kustomize reads as a typo.
			if strings.Contains(option.Reason, "`") ||
				strings.HasSuffix(option.Reason, ".") ||
				strings.Contains(option.Reason, "kustomize") {
				t.Errorf("option %q has a reason that is not plain text: %q",
					option.Name, option.Reason)
			}
			// Reasons also stand alone as table cells, where a pronoun has no referent.
			for _, pronoun := range []string{"it ", "they ", "this ", "these "} {
				if strings.HasPrefix(option.Reason, pronoun) {
					t.Errorf("option %q has a reason starting with a pronoun: %q",
						option.Name, option.Reason)
				}
			}
			continue
		}
		if option.Output == "" || option.Reason != "" {
			t.Errorf("supported option %q must describe only an output", option.Name)
		}
	}
}

func TestKustomizeOptionLookupCoversCatalog(t *testing.T) {
	for _, option := range KustomizeOptions() {
		got, ok := LookupKustomizeOption(option.Name)
		if !ok || got != option {
			t.Errorf("LookupKustomizeOption(%q) = %#v, %v", option.Name, got, ok)
		}
	}
	if _, ok := LookupKustomizeOption("unknown"); ok {
		t.Fatal("unknown Kustomize option was found")
	}
}

func TestBuildEnvironmentCatalogIsValid(t *testing.T) {
	validKinds := []BuildEnvironmentKind{BuildEnvironmentStatic, BuildEnvironmentDynamic}
	names := make(map[string]bool)

	for index, variable := range BuildEnvironmentVariables() {
		if variable.Name == "" || names[variable.Name] ||
			strings.ContainsFunc(variable.Name, unicode.IsSpace) {
			t.Errorf("variable %d has an empty, duplicate, or spaced name %q",
				index, variable.Name)
		}
		names[variable.Name] = true
		if !slices.Contains(validKinds, variable.Kind) {
			t.Errorf("variable %q has invalid kind %q", variable.Name, variable.Kind)
		}
		if variable.Kind == BuildEnvironmentDynamic {
			if variable.Reason == "" || variable.Source != "" {
				t.Errorf("dynamic variable %q must describe only a reason", variable.Name)
			}
			continue
		}
		if variable.Source == "" || variable.Reason != "" {
			t.Errorf("static variable %q must describe only a source", variable.Name)
		}
	}
}

func TestBuildEnvironmentVariableLookupCoversCatalog(t *testing.T) {
	for _, variable := range BuildEnvironmentVariables() {
		got, ok := LookupBuildEnvironmentVariable(variable.Name)
		if !ok || got != variable {
			t.Errorf("LookupBuildEnvironmentVariable(%q) = %#v, %v", variable.Name, got, ok)
		}
	}
	if _, ok := LookupBuildEnvironmentVariable("ARGOCD_UNKNOWN"); ok {
		t.Fatal("unknown build environment variable was found")
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
	for _, option := range KustomizeOptions() {
		description := option.Output
		if option.ValueKind == KustomizeUnsupported {
			description = option.Reason
		}
		if !bytes.Contains(first, []byte(option.Name)) ||
			!bytes.Contains(first, []byte(description)) {
			t.Errorf("Markdown output does not contain Kustomize option %q", option.Name)
		}
	}
	for _, variable := range BuildEnvironmentVariables() {
		description := variable.Source
		if variable.Kind == BuildEnvironmentDynamic {
			description = variable.Reason
		}
		if !bytes.Contains(first, []byte(variable.Name)) ||
			!bytes.Contains(first, []byte(description)) {
			t.Errorf("Markdown output does not contain build environment variable %q",
				variable.Name)
		}
	}
}
