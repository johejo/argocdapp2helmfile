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

type labelSelector struct {
	matchLabels      map[string]string
	matchExpressions []labelExpression
}

type labelExpression struct {
	key      string
	operator string
	values   []string
}

func parseListGenerator(raw yaml.MapSlice, field string) (listGenerator, *labelSelector, error) {
	var result listGenerator
	var selector *labelSelector
	var hasList bool
	for _, item := range raw {
		key, ok := item.Key.(string)
		if !ok {
			return result, nil, fmt.Errorf("%s contains a non-string field name", field)
		}
		switch key {
		case "list":
			if hasList {
				return result, nil, fmt.Errorf("%s.list is duplicated", field)
			}
			hasList = true
			items, ok := item.Value.(yaml.MapSlice)
			if !ok {
				return result, nil, fmt.Errorf("%s.list must be a mapping", field)
			}
			parsed, err := parseListOptions(items, field+".list")
			if err != nil {
				return result, nil, err
			}
			result = parsed
		case "selector":
			items, ok := item.Value.(yaml.MapSlice)
			if !ok {
				return result, nil, fmt.Errorf("%s.selector must be a mapping", field)
			}
			parsed, err := parseLabelSelector(items, field+".selector")
			if err != nil {
				return result, nil, err
			}
			selector = &parsed
		default:
			return result, nil, fmt.Errorf("%s.%s generator is not supported", field, key)
		}
	}
	if !hasList {
		return result, nil, fmt.Errorf("%s must contain exactly one List generator", field)
	}
	return result, selector, nil
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
