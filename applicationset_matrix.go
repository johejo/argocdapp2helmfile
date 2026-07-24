package main

import (
	"fmt"

	"github.com/goccy/go-yaml"
)

type matrixGeneratorResult struct {
	params   []generatedGeneratorParams
	template yaml.MapSlice
}

func generateMatrixParams(
	items yaml.MapSlice,
	field string,
	resolver *sourceResolver,
	renderer applicationSetRenderer,
	parentParams map[string]any,
	matrixDepth int,
) (matrixGeneratorResult, error) {
	var result matrixGeneratorResult
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
			if matrixDepth > 0 {
				return result, fmt.Errorf("%s.template is not supported in a nested matrix generator", field)
			}
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
		resolver,
		renderer,
		parentParams,
		matrixDepth+1,
		false,
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
			resolver,
			renderer,
			context,
			matrixDepth+1,
			false,
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
		result[key] = cloneMatrixValue(firstValue)
	}
	return result
}

func cloneMatrixMap(value map[string]any) map[string]any {
	result := make(map[string]any, len(value))
	for key, item := range value {
		result[key] = cloneMatrixValue(item)
	}
	return result
}

func cloneMatrixValue(value any) any {
	switch typed := value.(type) {
	case map[string]any:
		return cloneMatrixMap(typed)
	case []any:
		result := make([]any, len(typed))
		for i, item := range typed {
			result[i] = cloneMatrixValue(item)
		}
		return result
	default:
		return value
	}
}

func renderGeneratorValue(
	value any,
	params map[string]any,
	renderer applicationSetRenderer,
	path []string,
) (any, error) {
	if isGitValuesPath(path) {
		return value, nil
	}
	switch typed := value.(type) {
	case string:
		return renderer.Render(typed, params)
	case yaml.MapSlice:
		result := make(yaml.MapSlice, 0, len(typed))
		keys := make(map[string]struct{}, len(typed))
		for _, item := range typed {
			key := item.Key
			nextPath := path
			if stringKey, ok := key.(string); ok {
				renderedKey, err := renderer.Render(stringKey, params)
				if err != nil {
					return nil, err
				}
				if _, exists := keys[renderedKey]; exists {
					return nil, fmt.Errorf(
						"templating produced duplicate mapping key %q",
						renderedKey,
					)
				}
				keys[renderedKey] = struct{}{}
				key = renderedKey
				nextPath = appendPath(path, stringKey)
			}
			renderedValue, err := renderGeneratorValue(item.Value, params, renderer, nextPath)
			if err != nil {
				return nil, err
			}
			result = append(result, yaml.MapItem{Key: key, Value: renderedValue})
		}
		return result, nil
	case []any:
		result := make([]any, len(typed))
		for i, item := range typed {
			rendered, err := renderGeneratorValue(item, params, renderer, path)
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

func appendPath(path []string, element string) []string {
	result := make([]string, len(path), len(path)+1)
	copy(result, path)
	return append(result, element)
}

func isGitValuesPath(path []string) bool {
	return len(path) >= 2 && path[len(path)-2] == "git" && path[len(path)-1] == "values"
}
