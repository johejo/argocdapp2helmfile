package main

import (
	"strings"
	"testing"

	"github.com/goccy/go-yaml"
)

func TestLegacyTemplateRenderer(t *testing.T) {
	renderer := legacyTemplateRenderer{}
	params := map[string]any{
		"key":        "value",
		"Key":        "upper",
		"expression": "{{key}}",
		"number":     3,
	}
	tests := map[string]struct {
		input string
		want  string
	}{
		"plain tag": {
			input: "{{key}}",
			want:  "value",
		},
		"whitespace and multiple tags": {
			input: "{{ key }}/{{Key}}",
			want:  "value/upper",
		},
		"case sensitive": {
			input: "{{KEY}}",
			want:  "{{KEY}}",
		},
		"undefined": {
			input: "{{ missing }}",
			want:  "{{ missing }}",
		},
		"non-string": {
			input: "{{number}}",
			want:  "{{number}}",
		},
		"empty": {
			input: "{{ }}",
			want:  "{{ }}",
		},
		"replacement is not evaluated": {
			input: "{{expression}}",
			want:  "{{key}}",
		},
	}
	for name, test := range tests {
		t.Run(name, func(t *testing.T) {
			got, err := renderer.Render(test.input, params)
			if err != nil {
				t.Fatal(err)
			}
			if got != test.want {
				t.Fatalf("Render() = %q, want %q", got, test.want)
			}
		})
	}
}

func TestLegacyTemplateRendererRejectsInvalidDelimiters(t *testing.T) {
	renderer := legacyTemplateRenderer{}
	for _, input := range []string{"{{key", "key}}", "{{outer {{inner}}"} {
		t.Run(input, func(t *testing.T) {
			_, err := renderer.Render(input, map[string]any{"key": "value"})
			if err == nil || !strings.Contains(err.Error(), "parse template") {
				t.Fatalf("unexpected error: %v", err)
			}
		})
	}
}

func TestRenderLegacyTemplateMappingKeys(t *testing.T) {
	renderer := legacyTemplateRenderer{}
	rendered, err := renderTemplateValue(yaml.MapSlice{
		{Key: "{{ key }}", Value: "{{value}}"},
	}, map[string]any{"key": "rendered", "value": "content"}, renderer)
	if err != nil {
		t.Fatal(err)
	}
	items := rendered.(yaml.MapSlice)
	if items[0].Key != "rendered" || items[0].Value != "content" {
		t.Fatalf("unexpected rendered map: %#v", items)
	}

	_, err = renderTemplateValue(yaml.MapSlice{
		{Key: "{{ first }}", Value: "first"},
		{Key: "{{ second }}", Value: "second"},
	}, map[string]any{"first": "duplicate", "second": "duplicate"}, renderer)
	if err == nil || !strings.Contains(err.Error(), `duplicate mapping key "duplicate"`) {
		t.Fatalf("unexpected error: %v", err)
	}
}
