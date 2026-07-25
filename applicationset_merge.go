package main

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/goccy/go-yaml"
)

func generateMergeParams(
	items yaml.MapSlice,
	field string,
	config *conversionConfig,
	renderer applicationSetRenderer,
	combinationDepth int,
) (generatorResult, error) {
	var result generatorResult
	var mergeKeys []string
	var children []any
	for _, item := range items {
		key, ok := item.Key.(string)
		if !ok {
			return result, fmt.Errorf("%s contains a non-string field name", field)
		}
		switch key {
		case "mergeKeys":
			values, ok := item.Value.([]any)
			if !ok {
				return result, fmt.Errorf("%s.mergeKeys must be a sequence", field)
			}
			for i, raw := range values {
				value, ok := raw.(string)
				if !ok {
					return result, fmt.Errorf("%s.mergeKeys[%d] must be a string", field, i)
				}
				if strings.TrimSpace(value) == "" {
					return result, fmt.Errorf(
						"%s.mergeKeys[%d] must be a non-empty string",
						field,
						i,
					)
				}
				if renderer.GoTemplate() && strings.Contains(value, ".") {
					return result, fmt.Errorf(
						"%s.mergeKeys[%d] must not be a nested key when goTemplate is enabled",
						field,
						i,
					)
				}
				mergeKeys = append(mergeKeys, value)
			}
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
	if len(mergeKeys) == 0 {
		return result, fmt.Errorf("%s.mergeKeys must contain at least one key", field)
	}
	if len(children) < 2 {
		return result, fmt.Errorf("%s.generators must contain at least two generators", field)
	}

	generatedChildren := make([][]generatedGeneratorParams, len(children))
	for i, rawChild := range children {
		childField := fmt.Sprintf("%s.generators[%d]", field, i)
		child, ok := rawChild.(yaml.MapSlice)
		if !ok {
			return result, fmt.Errorf("%s must be a mapping", childField)
		}
		generated, err := parseApplicationSetGenerator(
			child,
			childField,
			config,
			renderer,
			nil,
			combinationDepth+1,
			"merge",
		)
		if err != nil {
			return result, err
		}
		if len(generated.params) == 0 {
			return result, fmt.Errorf("%s generated no parameters", childField)
		}
		if err := validateUniqueMergeKeys(generated.params, mergeKeys, childField); err != nil {
			return result, err
		}
		generatedChildren[i] = generated.params
	}

	result.params = append(result.params, generatedChildren[0]...)
	baseIndexes := make(map[string][]int)
	for i, params := range result.params {
		key, matchable, err := mergeParamsKey(params.params, mergeKeys)
		if err != nil {
			return result, fmt.Errorf("%s: merge keys: %w", params.path, err)
		}
		if matchable {
			baseIndexes[key] = append(baseIndexes[key], i)
		}
	}
	for _, child := range generatedChildren[1:] {
		for _, override := range child {
			key, matchable, err := mergeParamsKey(override.params, mergeKeys)
			if err != nil {
				return result, fmt.Errorf("%s: merge keys: %w", override.path, err)
			}
			if !matchable {
				continue
			}
			for _, index := range baseIndexes[key] {
				base := result.params[index]
				result.params[index] = generatedGeneratorParams{
					params: mergeMatrixParams(override.params, base.params),
					path:   base.path + " ← " + override.path,
				}
			}
		}
	}
	return result, nil
}

func validateUniqueMergeKeys(
	params []generatedGeneratorParams,
	mergeKeys []string,
	field string,
) error {
	origins := make(map[string]string)
	for _, generated := range params {
		key, matchable, err := mergeParamsKey(generated.params, mergeKeys)
		if err != nil {
			return fmt.Errorf("%s: merge keys: %w", generated.path, err)
		}
		if !matchable {
			continue
		}
		if first, exists := origins[key]; exists {
			return fmt.Errorf(
				"%s has duplicate mergeKeys in %s and %s",
				field,
				first,
				generated.path,
			)
		}
		origins[key] = generated.path
	}
	return nil
}

func mergeParamsKey(params map[string]any, mergeKeys []string) (string, bool, error) {
	values := make([]any, len(mergeKeys))
	for i, key := range mergeKeys {
		value, exists := params[key]
		if !exists {
			return "", false, nil
		}
		values[i] = value
	}
	encoded, err := json.Marshal(values)
	if err != nil {
		return "", false, err
	}
	return string(encoded), true, nil
}
