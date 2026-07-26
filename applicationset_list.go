package main

import (
	"errors"
	"fmt"
	"strings"

	"github.com/goccy/go-yaml"
)

type listGenerator struct {
	elements     []any
	elementsYAML string
	template     yaml.MapSlice
}

func generateListParams(request generatorRequest) (generatorResult, error) {
	var result generatorResult
	list, err := parseListOptions(request.items, request.field)
	if err != nil {
		return result, err
	}
	result.template = list.template
	yamlElements, err := parseElementsYAML(list.elementsYAML, request.field+".elementsYaml")
	if err != nil {
		return result, err
	}
	// elementsYaml always parses as goTemplate: its values are already typed.
	groups := []struct {
		option     string
		elements   []any
		goTemplate bool
	}{
		{"elements", list.elements, request.renderer.GoTemplate()},
		{"elementsYaml", yamlElements, true},
	}
	for _, group := range groups {
		for i, rawElement := range group.elements {
			elementField := fmt.Sprintf("%s.%s[%d]", request.field, group.option, i)
			params, err := normalizeListElement(rawElement, group.goTemplate)
			if err != nil {
				return result, fmt.Errorf("%s: must be a mapping: %w", elementField, err)
			}
			result.params = append(result.params, generatedGeneratorParams{
				params: params,
				path:   elementField,
			})
		}
	}
	return result, nil
}

func parseListOptions(items yaml.MapSlice, field string) (listGenerator, error) {
	var result listGenerator
	for _, item := range items {
		key, ok := item.Key.(string)
		if !ok {
			return result, fmt.Errorf("%s contains a non-string field name", field)
		}
		switch key {
		case "elements":
			if isIgnorableEmptyYAMLOption(item.Value) {
				continue
			}
			elements, ok := item.Value.([]any)
			if !ok {
				return result, fmt.Errorf("%s.elements must be a sequence", field)
			}
			result.elements = elements
		case "elementsYaml":
			if isIgnorableEmptyYAMLOption(item.Value) {
				continue
			}
			value, ok := item.Value.(string)
			if !ok {
				return result, fmt.Errorf("%s.elementsYaml must be a string", field)
			}
			result.elementsYAML = value
		case "template":
			if isIgnorableEmptyYAMLOption(item.Value) {
				continue
			}
			value, err := readMappingYAMLOption(item.Value, field+".template")
			if err != nil {
				return result, err
			}
			result.template = value
		default:
			return result, fmt.Errorf("%s.%s is not supported", field, key)
		}
	}
	return result, nil
}

func parseElementsYAML(input, field string) ([]any, error) {
	if strings.TrimSpace(input) == "" {
		return nil, nil
	}
	value, err := decodeSingleDocument([]byte(input), field)
	if err != nil {
		return nil, err
	}
	elements, ok := value.([]any)
	if !ok {
		return nil, fmt.Errorf("%s must contain a sequence", field)
	}
	return elements, nil
}

func normalizeListElement(value any, goTemplate bool) (map[string]any, error) {
	params, err := normalizeStringMap(value)
	if err != nil || goTemplate {
		return params, err
	}
	result := make(map[string]any, len(params))
	for key, value := range params {
		if key != "values" {
			stringValue, ok := value.(string)
			if !ok {
				return nil, fmt.Errorf("field %q must be a string", key)
			}
			result[key] = stringValue
			continue
		}
		values, ok := value.(map[string]any)
		if !ok {
			return nil, errors.New(`field "values" must be a mapping`)
		}
		for valueKey, value := range values {
			stringValue, ok := value.(string)
			if !ok {
				return nil, fmt.Errorf("field %q must be a string", "values."+valueKey)
			}
			result["values."+valueKey] = stringValue
		}
	}
	return result, nil
}
