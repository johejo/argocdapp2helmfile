package applicationset

import (
	"bytes"
	"strings"
	"testing"
	"unicode"
)

func TestGeneratorCatalogIsValid(t *testing.T) {
	names := make(map[string]bool)

	for index, generator := range Generators() {
		if generator.Name == "" || names[generator.Name] ||
			strings.ContainsFunc(generator.Name, unicode.IsSpace) {
			t.Errorf("generator %d has an empty, duplicate, or spaced name %q",
				index, generator.Name)
		}
		names[generator.Name] = true
		if generator.Reason != "" {
			if generator.Fields != "" || generator.Expansion != "" {
				t.Errorf("unsupported generator %q must describe only a reason", generator.Name)
			}
			if generator.Combination || generator.ValuesMap || generator.DeferredRender {
				t.Errorf("unsupported generator %q declares parsing behavior", generator.Name)
			}
			// Reasons reach plain-text errors, where an identifier cannot be marked up.
			if strings.Contains(generator.Reason, "`") ||
				strings.HasSuffix(generator.Reason, ".") {
				t.Errorf("generator %q has a reason that is not plain text: %q",
					generator.Name, generator.Reason)
			}
			// Reasons also stand alone as table cells, where a pronoun has no referent.
			for _, pronoun := range []string{"it ", "they ", "this ", "these "} {
				if strings.HasPrefix(generator.Reason, pronoun) {
					t.Errorf("generator %q has a reason starting with a pronoun: %q",
						generator.Name, generator.Reason)
				}
			}
			continue
		}
		if generator.Fields == "" || generator.Expansion == "" {
			t.Errorf("supported generator %q must describe its fields and expansion",
				generator.Name)
		}
		if generator.DeferredRender && !generator.Combination {
			t.Errorf("generator %q defers rendering without nesting children", generator.Name)
		}
	}
}

func TestGeneratorLookupCoversCatalog(t *testing.T) {
	for _, generator := range Generators() {
		got, ok := LookupGenerator(generator.Name)
		if !ok || got != generator {
			t.Errorf("LookupGenerator(%q) = %#v, %v", generator.Name, got, ok)
		}
	}
	if _, ok := LookupGenerator("unknown"); ok {
		t.Fatal("unknown generator was found")
	}
}

func TestGoTemplateOptionCatalogIsValid(t *testing.T) {
	names := make(map[string]bool)

	for index, option := range GoTemplateOptions() {
		if option.Name == "" || names[option.Name] ||
			strings.ContainsFunc(option.Name, unicode.IsSpace) {
			t.Errorf("option %d has an empty, duplicate, or spaced name %q", index, option.Name)
		}
		names[option.Name] = true
		if option.Output == "" {
			t.Errorf("option %q has no rendering behavior", option.Name)
		}
	}
}

func TestGoTemplateOptionLookupCoversCatalog(t *testing.T) {
	for _, option := range GoTemplateOptions() {
		got, ok := LookupGoTemplateOption(option.Name)
		if !ok || got != option {
			t.Errorf("LookupGoTemplateOption(%q) = %#v, %v", option.Name, got, ok)
		}
	}
	for _, name := range []string{"unknown", "missingkey", "missingkey=wat", ""} {
		if _, ok := LookupGoTemplateOption(name); ok {
			t.Errorf("unknown option %q was found", name)
		}
	}
}

func TestMarkdownIsDeterministicAndComplete(t *testing.T) {
	first := Markdown()
	second := Markdown()
	if !bytes.Equal(first, second) {
		t.Fatal("Markdown output is not deterministic")
	}
	for _, generator := range Generators() {
		description := generator.Expansion
		if generator.Reason != "" {
			description = generator.Reason
		}
		if !bytes.Contains(first, []byte(generator.Name)) ||
			!bytes.Contains(first, []byte(description)) {
			t.Errorf("Markdown output does not contain generator %q", generator.Name)
		}
	}
	for _, option := range GoTemplateOptions() {
		if !bytes.Contains(first, []byte(option.Name)) ||
			!bytes.Contains(first, []byte(option.Output)) {
			t.Errorf("Markdown output does not contain option %q", option.Name)
		}
	}
}
