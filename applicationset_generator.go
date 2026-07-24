package main

import (
	"errors"
	"fmt"
	"slices"
	"strconv"
	"strings"

	"github.com/goccy/go-yaml"
)

type listGenerator struct {
	elements     []any
	elementsYAML string
	template     yaml.MapSlice
}

type generatedGeneratorParams struct {
	params map[string]any
	path   string
}

type applicationSetGenerator struct {
	params   []generatedGeneratorParams
	template yaml.MapSlice
	selector *labelSelector
}

func parseApplicationSetGenerator(
	raw yaml.MapSlice,
	field string,
	resolver *sourceResolver,
	renderer applicationSetRenderer,
	parentParams map[string]any,
	matrixDepth int,
) (applicationSetGenerator, error) {
	var result applicationSetGenerator
	var generatorName string
	var generatorValue any
	for _, item := range raw {
		key, ok := item.Key.(string)
		if !ok {
			return result, fmt.Errorf("%s contains a non-string field name", field)
		}
		if key == "selector" {
			selectorValue := item.Value
			if len(parentParams) != 0 {
				rendered, err := renderGeneratorValue(
					selectorValue,
					parentParams,
					renderer,
					[]string{"selector"},
				)
				if err != nil {
					return result, fmt.Errorf("%s.selector: render: %w", field, err)
				}
				selectorValue = rendered
			}
			items, ok := selectorValue.(yaml.MapSlice)
			if !ok {
				return result, fmt.Errorf("%s.selector must be a mapping", field)
			}
			selector, err := parseLabelSelector(items, field+".selector")
			if err != nil {
				return result, err
			}
			result.selector = &selector
			continue
		}
		if key != "list" && key != "git" && key != "matrix" {
			return result, fmt.Errorf("%s.%s generator is not supported", field, key)
		}
		if generatorName != "" {
			return result, fmt.Errorf("%s must contain exactly one generator", field)
		}
		generatorName, generatorValue = key, item.Value
	}
	if matrixDepth > 0 && generatorName != "matrix" {
		if err := rejectMatrixChildTemplate(raw, field); err != nil {
			return result, err
		}
	}
	if generatorName != "matrix" && len(parentParams) != 0 {
		rendered, err := renderGeneratorValue(raw, parentParams, renderer, nil)
		if err != nil {
			return result, fmt.Errorf("%s: render generator: %w", field, err)
		}
		var ok bool
		raw, ok = rendered.(yaml.MapSlice)
		if !ok {
			return result, fmt.Errorf("%s must be a mapping", field)
		}
		generatorValue = nil
		for _, item := range raw {
			if item.Key == generatorName {
				generatorValue = item.Value
				break
			}
		}
	}
	switch generatorName {
	case "list":
		items, ok := generatorValue.(yaml.MapSlice)
		if !ok {
			return result, fmt.Errorf("%s.list must be a mapping", field)
		}
		list, err := parseListOptions(items, field+".list")
		if err != nil {
			return result, err
		}
		result.template = list.template
		elements := append([]any(nil), list.elements...)
		yamlElements, err := parseElementsYAML(list.elementsYAML, field+".list.elementsYaml")
		if err != nil {
			return result, err
		}
		elements = append(elements, yamlElements...)
		for i, rawElement := range elements {
			elementField := fmt.Sprintf("%s.list.elements[%d]", field, i)
			if i >= len(list.elements) {
				elementField = fmt.Sprintf("%s.list.elementsYaml[%d]", field, i-len(list.elements))
			}
			params, err := normalizeStringMap(rawElement)
			if err == nil && i < len(list.elements) {
				params, err = normalizeListElement(rawElement, renderer.GoTemplate())
			}
			if err != nil {
				return result, fmt.Errorf("%s: must be a mapping: %w", elementField, err)
			}
			result.params = append(result.params, generatedGeneratorParams{
				params: params,
				path:   elementField,
			})
		}
	case "git":
		items, ok := generatorValue.(yaml.MapSlice)
		if !ok {
			return result, fmt.Errorf("%s.git must be a mapping", field)
		}
		git, err := generateGitParams(
			items,
			field+".git",
			resolver,
			renderer,
			parentParams,
		)
		if err != nil {
			return result, err
		}
		result.params = git.params
		result.template = git.template
	case "matrix":
		if matrixDepth >= 2 {
			return result, fmt.Errorf("%s.matrix exceeds the supported nesting depth", field)
		}
		items, ok := generatorValue.(yaml.MapSlice)
		if !ok {
			return result, fmt.Errorf("%s.matrix must be a mapping", field)
		}
		matrix, err := generateMatrixParams(
			items,
			field+".matrix",
			resolver,
			renderer,
			parentParams,
			matrixDepth,
		)
		if err != nil {
			return result, err
		}
		result.params = matrix.params
		result.template = matrix.template
	case "":
		return result, fmt.Errorf("%s must contain exactly one generator", field)
	default:
		return result, fmt.Errorf("%s.%s generator is not supported", field, generatorName)
	}
	if result.selector != nil {
		filtered := make([]generatedGeneratorParams, 0, len(result.params))
		for _, generatedParams := range result.params {
			matches, err := result.selector.matches(generatedParams.params)
			if err != nil {
				return result, fmt.Errorf("%s.selector: %w", field, err)
			}
			if matches {
				filtered = append(filtered, generatedParams)
			}
		}
		result.params = filtered
	}
	return result, nil
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

func rejectMatrixChildTemplate(raw yaml.MapSlice, field string) error {
	for _, item := range raw {
		key, ok := item.Key.(string)
		if !ok || key == "selector" {
			continue
		}
		options, ok := item.Value.(yaml.MapSlice)
		if !ok {
			continue
		}
		for _, option := range options {
			if option.Key == "template" {
				return fmt.Errorf("%s.%s.template is not supported in a matrix generator", field, key)
			}
		}
	}
	return nil
}

type labelSelector struct {
	matchLabels      map[string]string
	matchExpressions []labelExpression
}

type labelExpression struct {
	key      string
	operator string
	values   []string
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
			value, ok := item.Value.(yaml.MapSlice)
			if !ok {
				return result, fmt.Errorf("%s.template must be a mapping", field)
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
	if err := requireSingleDocument([]byte(input), field); err != nil {
		return nil, err
	}
	var value any
	decoder := yaml.NewDecoder(strings.NewReader(input), yaml.UseOrderedMap())
	if err := decoder.Decode(&value); err != nil {
		return nil, fmt.Errorf("decode %s: %w", field, err)
	}
	elements, ok := value.([]any)
	if !ok {
		return nil, fmt.Errorf("%s must contain a sequence", field)
	}
	return elements, nil
}

func normalizeStringMap(value any) (map[string]any, error) {
	normalized, err := normalizeTemplateValue(value)
	if err != nil {
		return nil, err
	}
	result, ok := normalized.(map[string]any)
	if !ok {
		return nil, errors.New("value is not a mapping")
	}
	return result, nil
}

func parseLabelSelector(items yaml.MapSlice, field string) (labelSelector, error) {
	result := labelSelector{matchLabels: make(map[string]string)}
	for _, item := range items {
		key, ok := item.Key.(string)
		if !ok {
			return result, fmt.Errorf("%s contains a non-string field name", field)
		}
		switch key {
		case "matchLabels":
			labels, ok := item.Value.(yaml.MapSlice)
			if !ok {
				return result, fmt.Errorf("%s.matchLabels must be a mapping", field)
			}
			for _, label := range labels {
				labelKey, ok := label.Key.(string)
				if !ok || strings.TrimSpace(labelKey) == "" {
					return result, fmt.Errorf("%s.matchLabels keys must be non-empty strings", field)
				}
				labelValue, ok := label.Value.(string)
				if !ok {
					return result, fmt.Errorf("%s.matchLabels.%s must be a string", field, labelKey)
				}
				result.matchLabels[labelKey] = labelValue
			}
		case "matchExpressions":
			expressions, ok := item.Value.([]any)
			if !ok {
				return result, fmt.Errorf("%s.matchExpressions must be a sequence", field)
			}
			for i, rawExpression := range expressions {
				expressionItems, ok := rawExpression.(yaml.MapSlice)
				if !ok {
					return result, fmt.Errorf("%s.matchExpressions[%d] must be a mapping", field, i)
				}
				expression, err := parseLabelExpression(
					expressionItems,
					fmt.Sprintf("%s.matchExpressions[%d]", field, i),
				)
				if err != nil {
					return result, err
				}
				result.matchExpressions = append(result.matchExpressions, expression)
			}
		default:
			return result, fmt.Errorf("%s.%s is not supported", field, key)
		}
	}
	return result, nil
}

func parseLabelExpression(items yaml.MapSlice, field string) (labelExpression, error) {
	var result labelExpression
	for _, item := range items {
		key, ok := item.Key.(string)
		if !ok {
			return result, fmt.Errorf("%s contains a non-string field name", field)
		}
		switch key {
		case "key":
			value, ok := item.Value.(string)
			if !ok {
				return result, fmt.Errorf("%s.key must be a string", field)
			}
			result.key = value
		case "operator":
			value, ok := item.Value.(string)
			if !ok {
				return result, fmt.Errorf("%s.operator must be a string", field)
			}
			result.operator = value
		case "values":
			values, ok := item.Value.([]any)
			if !ok {
				return result, fmt.Errorf("%s.values must be a sequence", field)
			}
			for i, raw := range values {
				value, ok := raw.(string)
				if !ok {
					return result, fmt.Errorf("%s.values[%d] must be a string", field, i)
				}
				result.values = append(result.values, value)
			}
		default:
			return result, fmt.Errorf("%s.%s is not supported", field, key)
		}
	}
	if strings.TrimSpace(result.key) == "" {
		return result, fmt.Errorf("%s.key is required", field)
	}
	switch result.operator {
	case "In", "NotIn":
		if len(result.values) == 0 {
			return result, fmt.Errorf("%s.values must not be empty for operator %s", field, result.operator)
		}
	case "Exists", "DoesNotExist":
		if len(result.values) != 0 {
			return result, fmt.Errorf("%s.values must be empty for operator %s", field, result.operator)
		}
	default:
		return result, fmt.Errorf("%s.operator must be In, NotIn, Exists, or DoesNotExist", field)
	}
	return result, nil
}

func (selector labelSelector) matches(params map[string]any) (bool, error) {
	flat := make(map[string]string)
	if err := flattenParameters(flat, "", params); err != nil {
		return false, err
	}
	for key, want := range selector.matchLabels {
		if got, exists := flat[key]; !exists || got != want {
			return false, nil
		}
	}
	for _, expression := range selector.matchExpressions {
		value, exists := flat[expression.key]
		contains := slices.Contains(expression.values, value)
		switch expression.operator {
		case "In":
			if !exists || !contains {
				return false, nil
			}
		case "NotIn":
			if exists && contains {
				return false, nil
			}
		case "Exists":
			if !exists {
				return false, nil
			}
		case "DoesNotExist":
			if exists {
				return false, nil
			}
		}
	}
	return true, nil
}

func flattenParameters(output map[string]string, prefix string, value any) error {
	switch typed := value.(type) {
	case map[string]any:
		for key, item := range typed {
			next := key
			if prefix != "" {
				next = prefix + "." + key
			}
			if err := flattenParameters(output, next, item); err != nil {
				return err
			}
		}
	case []any:
		for i, item := range typed {
			next := strconv.Itoa(i)
			if prefix != "" {
				next = prefix + "." + next
			}
			if err := flattenParameters(output, next, item); err != nil {
				return err
			}
		}
	default:
		if prefix == "" {
			return errors.New("selector parameters must be a mapping")
		}
		output[prefix] = fmt.Sprintf("%v", value)
	}
	return nil
}
