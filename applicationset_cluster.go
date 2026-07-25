package main

import (
	"fmt"

	"github.com/goccy/go-yaml"
)

type clusterGeneratorOptions struct {
	selector *labelSelector
	values   []generatorValue
	template yaml.MapSlice
}

func generateClusterParams(
	items yaml.MapSlice,
	field string,
	config *conversionConfig,
	renderer applicationSetRenderer,
	parentParams map[string]any,
) (generatorResult, error) {
	var result generatorResult
	if config == nil {
		return result, fmt.Errorf("%s requires --config", field)
	}
	options, err := parseClusterGeneratorOptions(items, field)
	if err != nil {
		return result, err
	}
	result.template = options.template
	for _, cluster := range config.clusters {
		if options.selector != nil {
			matches, err := options.selector.matches(stringMapToAny(cluster.Labels))
			if err != nil {
				return result, fmt.Errorf("%s.selector: %w", field, err)
			}
			if !matches {
				continue
			}
		}

		params := clusterGeneratorParams(cluster, renderer.GoTemplate())
		origin := fmt.Sprintf("%s[%q]", field, cluster.Name)
		if err := renderGeneratorValues(
			params,
			parentParams,
			options.values,
			renderer,
		); err != nil {
			return result, fmt.Errorf("%s: values: %w", origin, err)
		}
		result.params = append(result.params, generatedGeneratorParams{
			params: params,
			path:   origin,
		})
	}
	return result, nil
}

func parseClusterGeneratorOptions(
	items yaml.MapSlice,
	field string,
) (clusterGeneratorOptions, error) {
	var result clusterGeneratorOptions
	for _, item := range items {
		key, ok := item.Key.(string)
		if !ok {
			return result, fmt.Errorf("%s contains a non-string field name", field)
		}
		switch key {
		case "selector":
			value, ok := item.Value.(yaml.MapSlice)
			if !ok {
				return result, fmt.Errorf("%s.selector must be a mapping", field)
			}
			selector, err := parseLabelSelector(value, field+".selector")
			if err != nil {
				return result, err
			}
			result.selector = &selector
		case "values":
			values, err := parseGeneratorValues(item.Value, field+".values")
			if err != nil {
				return result, err
			}
			result.values = values
		case "template":
			value, ok := item.Value.(yaml.MapSlice)
			if !ok {
				return result, fmt.Errorf("%s.template must be a mapping", field)
			}
			result.template = value
		case "flatList":
			value, ok := item.Value.(bool)
			if !ok {
				return result, fmt.Errorf("%s.flatList must be a boolean", field)
			}
			if value {
				return result, fmt.Errorf("%s.flatList: true is not supported", field)
			}
		default:
			return result, fmt.Errorf("%s.%s is not supported", field, key)
		}
	}
	return result, nil
}

func clusterGeneratorParams(cluster clusterConfigEntry, goTemplate bool) map[string]any {
	params := map[string]any{
		"name":           cluster.Name,
		"nameNormalized": normalizeName(cluster.Name),
		"server":         cluster.Server,
		"project":        cluster.Project,
	}
	if goTemplate {
		metadata := make(map[string]any, 2)
		if len(cluster.Labels) != 0 {
			metadata["labels"] = stringMapToAny(cluster.Labels)
		}
		if len(cluster.Annotations) != 0 {
			metadata["annotations"] = stringMapToAny(cluster.Annotations)
		}
		if len(metadata) != 0 {
			params["metadata"] = metadata
		}
		return params
	}
	for key, value := range cluster.Labels {
		params["metadata.labels."+key] = value
	}
	for key, value := range cluster.Annotations {
		params["metadata.annotations."+key] = value
	}
	return params
}

func stringMapToAny[T ~map[string]string](values T) map[string]any {
	result := make(map[string]any, len(values))
	for key, value := range values {
		result[key] = value
	}
	return result
}
