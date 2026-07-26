package main

import (
	"fmt"

	"github.com/goccy/go-yaml"
	"github.com/johejo/argocdapp2helmfile/internal/applicationset"
)

func generateMatrixParams(
	items yaml.MapSlice,
	field string,
	config *conversionConfig,
	renderer applicationSetRenderer,
	parentParams map[string]any,
	parent combinationContext,
) (generatorResult, error) {
	var result generatorResult
	var children []any
	for _, item := range items {
		key, ok := item.Key.(string)
		if !ok {
			return result, fmt.Errorf("%s contains a non-string field name", field)
		}
		switch key {
		case "generators":
			var sequenceOK bool
			children, sequenceOK = item.Value.([]any)
			if !sequenceOK {
				return result, fmt.Errorf("%s.generators must be a sequence", field)
			}
		case "template":
			value, ok := item.Value.(yaml.MapSlice)
			if !ok {
				return result, fmt.Errorf("%s.template must be a mapping", field)
			}
			result.template = value
		default:
			return result, fmt.Errorf("%s.%s is not supported", field, key)
		}
	}
	if len(children) != 2 {
		return result, fmt.Errorf("%s.generators must contain exactly two generators", field)
	}
	firstRaw, ok := children[0].(yaml.MapSlice)
	if !ok {
		return result, fmt.Errorf("%s.generators[0] must be a mapping", field)
	}
	secondRaw, ok := children[1].(yaml.MapSlice)
	if !ok {
		return result, fmt.Errorf("%s.generators[1] must be a mapping", field)
	}

	firstField := field + ".generators[0]"
	first, err := parseApplicationSetGenerator(
		firstRaw,
		firstField,
		config,
		renderer,
		parentParams,
		parent.child("matrix"),
	)
	if err != nil {
		return result, err
	}
	if len(first.params) == 0 {
		return result, fmt.Errorf("%s generated no parameters", firstField)
	}

	secondField := field + ".generators[1]"
	for _, firstParams := range first.params {
		context := mergeMatrixParams(parentParams, firstParams.params)
		second, err := parseApplicationSetGenerator(
			secondRaw,
			secondField,
			config,
			renderer,
			context,
			parent.child("matrix"),
		)
		if err != nil {
			return result, fmt.Errorf("%s -> %w", firstParams.path, err)
		}
		if len(second.params) == 0 {
			return result, fmt.Errorf(
				"%s -> %s generated no parameters",
				firstParams.path,
				secondField,
			)
		}
		for _, secondParams := range second.params {
			result.params = append(result.params, generatedGeneratorParams{
				params: mergeMatrixParams(firstParams.params, secondParams.params),
				path:   firstParams.path + " × " + secondParams.path,
			})
		}
	}
	return result, nil
}

func mergeMatrixParams(first, second map[string]any) map[string]any {
	result := cloneMatrixMap(second)
	for key, firstValue := range first {
		secondValue, exists := result[key]
		firstMap, firstIsMap := firstValue.(map[string]any)
		secondMap, secondIsMap := secondValue.(map[string]any)
		if exists && firstIsMap && secondIsMap {
			result[key] = mergeMatrixParams(firstMap, secondMap)
			continue
		}
		result[key] = cloneValue(firstValue)
	}
	return result
}

func cloneMatrixMap(value map[string]any) map[string]any {
	return cloneValue(value).(map[string]any)
}

func renderGeneratorValue(
	value any,
	params map[string]any,
	renderer applicationSetRenderer,
	path []string,
) (any, error) {
	return renderValueTree(value, params, renderer, path, isGeneratorValuesPath)
}

func isGeneratorValuesPath(path []string) bool {
	if len(path) < 2 || path[len(path)-1] != "values" {
		return false
	}
	generator, known := applicationset.LookupGenerator(path[len(path)-2])
	return known && generator.ValuesMap
}
