package main

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"strings"
	"text/template"

	"github.com/valyala/fasttemplate"
)

type applicationSetRenderer interface {
	Render(string, map[string]any) (string, error)
	GoTemplate() bool
}

type goTemplateRenderer struct {
	template *template.Template
	parsed   map[string]*template.Template
}

func (renderer goTemplateRenderer) Render(input string, params map[string]any) (string, error) {
	field, cached := renderer.parsed[input]
	if !cached {
		clone, err := renderer.template.Clone()
		if err != nil {
			return "", fmt.Errorf("clone template: %w", err)
		}
		field, err = clone.Parse(input)
		if err != nil {
			return "", fmt.Errorf("parse template %q: %w", input, err)
		}
		renderer.parsed[input] = field
	}
	var output bytes.Buffer
	if err := field.Execute(&output, params); err != nil {
		return "", fmt.Errorf("execute template %q: %w", input, err)
	}
	return output.String(), nil
}

func (goTemplateRenderer) GoTemplate() bool {
	return true
}

type legacyTemplateRenderer struct{}

func (legacyTemplateRenderer) Render(input string, params map[string]any) (string, error) {
	if err := validateLegacyDelimiters(input); err != nil {
		return "", fmt.Errorf("parse template %q: %w", input, err)
	}
	field, err := fasttemplate.NewTemplate(input, "{{", "}}")
	if err != nil {
		return "", fmt.Errorf("parse template %q: %w", input, err)
	}
	output, err := field.ExecuteFuncStringWithErr(func(writer io.Writer, tag string) (int, error) {
		key := strings.TrimSpace(tag)
		value, exists := params[key]
		stringValue, isString := value.(string)
		if key == "" || !exists || !isString {
			return io.WriteString(writer, "{{"+tag+"}}")
		}
		return io.WriteString(writer, stringValue)
	})
	if err != nil {
		return "", fmt.Errorf("execute template %q: %w", input, err)
	}
	return output, nil
}

func validateLegacyDelimiters(input string) error {
	for offset := 0; offset < len(input); {
		start := strings.Index(input[offset:], "{{")
		end := strings.Index(input[offset:], "}}")
		if end >= 0 && (start < 0 || end < start) {
			return errors.New("unexpected closing delimiter")
		}
		if start < 0 {
			return nil
		}
		offset += start + len("{{")
		end = strings.Index(input[offset:], "}}")
		if end < 0 {
			return errors.New("unclosed opening delimiter")
		}
		nextStart := strings.Index(input[offset:], "{{")
		if nextStart >= 0 && nextStart < end {
			return errors.New("unexpected opening delimiter")
		}
		offset += end + len("}}")
	}
	return nil
}

func (legacyTemplateRenderer) GoTemplate() bool {
	return false
}

func newApplicationSetRenderer(
	goTemplate bool,
	options []string,
) (applicationSetRenderer, error) {
	if goTemplate {
		return newGoTemplateRenderer(options)
	}
	return legacyTemplateRenderer{}, nil
}
