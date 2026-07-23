package main

import (
	"bytes"
	"errors"
	"fmt"
	"strings"
	"text/template"

	"github.com/goccy/go-yaml"
)

func normalizeTemplateValue(value any) (any, error) {
	switch typed := value.(type) {
	case yaml.MapSlice:
		result := make(map[string]any, len(typed))
		for _, item := range typed {
			key, ok := item.Key.(string)
			if !ok {
				return nil, errors.New("mapping keys must be strings")
			}
			normalized, err := normalizeTemplateValue(item.Value)
			if err != nil {
				return nil, err
			}
			result[key] = normalized
		}
		return result, nil
	case map[string]any:
		result := make(map[string]any, len(typed))
		for key, item := range typed {
			normalized, err := normalizeTemplateValue(item)
			if err != nil {
				return nil, err
			}
			result[key] = normalized
		}
		return result, nil
	case []any:
		result := make([]any, len(typed))
		for i, item := range typed {
			normalized, err := normalizeTemplateValue(item)
			if err != nil {
				return nil, err
			}
			result[i] = normalized
		}
		return result, nil
	default:
		return value, nil
	}
}

func renderApplicationTemplate(
	input yaml.MapSlice,
	templatePatch string,
	params map[string]any,
	renderer *template.Template,
) (application, any, error) {
	rendered, err := renderTemplateValue(input, params, renderer)
	if err != nil {
		return application{}, nil, err
	}
	renderedMap, ok := rendered.(yaml.MapSlice)
	if !ok {
		return application{}, nil, errors.New("rendered Application must be a mapping")
	}
	renderedMap = setMapSliceField(renderedMap, "apiVersion", "argoproj.io/v1alpha1")
	renderedMap = setMapSliceField(renderedMap, "kind", "Application")
	project, hasProject := applicationProject(renderedMap)
	renderedPatch, err := executeTemplate(templatePatch, params, renderer)
	if err != nil {
		return application{}, nil, fmt.Errorf("render spec.templatePatch: %w", err)
	}
	if strings.TrimSpace(renderedPatch) != "" {
		patch, err := decodeApplicationTemplatePatch(renderedPatch)
		if err != nil {
			return application{}, nil, err
		}
		renderedMap = mergeApplicationTemplatePatch(renderedMap, patch)
		renderedMap = restoreApplicationProject(renderedMap, project, hasProject)
	}
	data, err := yaml.Marshal(renderedMap)
	if err != nil {
		return application{}, nil, fmt.Errorf("encode rendered Application: %w", err)
	}
	var app application
	if err := yaml.UnmarshalWithOptions(data, &app, yaml.UseOrderedMap()); err != nil {
		return application{}, nil, fmt.Errorf("decode rendered Application: %w", err)
	}
	normalized, err := normalizeTemplateValue(renderedMap)
	if err != nil {
		return application{}, nil, fmt.Errorf("normalize rendered Application: %w", err)
	}
	return app, normalized, nil
}

func renderTemplateValue(value any, params map[string]any, base *template.Template) (any, error) {
	switch typed := value.(type) {
	case string:
		return executeTemplate(typed, params, base)
	case yaml.MapSlice:
		result := make(yaml.MapSlice, 0, len(typed))
		keys := make(map[string]struct{}, len(typed))
		for _, item := range typed {
			key := item.Key
			if stringKey, ok := key.(string); ok {
				renderedKey, err := executeTemplate(stringKey, params, base)
				if err != nil {
					return nil, err
				}
				if _, exists := keys[renderedKey]; exists {
					return nil, fmt.Errorf("templating produced duplicate mapping key %q", renderedKey)
				}
				keys[renderedKey] = struct{}{}
				key = renderedKey
			}
			renderedValue, err := renderTemplateValue(item.Value, params, base)
			if err != nil {
				return nil, err
			}
			result = append(result, yaml.MapItem{Key: key, Value: renderedValue})
		}
		return result, nil
	case []any:
		result := make([]any, len(typed))
		for i, item := range typed {
			rendered, err := renderTemplateValue(item, params, base)
			if err != nil {
				return nil, err
			}
			result[i] = rendered
		}
		return result, nil
	default:
		return value, nil
	}
}

func executeTemplate(input string, params map[string]any, base *template.Template) (string, error) {
	field, err := base.Clone()
	if err != nil {
		return "", fmt.Errorf("clone template: %w", err)
	}
	field, err = field.Parse(input)
	if err != nil {
		return "", fmt.Errorf("parse template %q: %w", input, err)
	}
	var output bytes.Buffer
	if err := field.Execute(&output, params); err != nil {
		return "", fmt.Errorf("execute template %q: %w", input, err)
	}
	return output.String(), nil
}
