package main

import (
	"errors"
	"fmt"
	"slices"
	"strconv"
	"strings"

	"github.com/goccy/go-yaml"
	"github.com/johejo/argocdapp2helmfile/internal/applicationset"
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

type generatorResult struct {
	params   []generatedGeneratorParams
	template yaml.MapSlice
}

type applicationSetGenerator struct {
	params   []generatedGeneratorParams
	template yaml.MapSlice
	selector *labelSelector
}

// combinationContext names the matrix or merge generator a generator is nested
// inside. The zero value is the top level.
type combinationContext struct {
	name  string
	depth int
}

func (parent combinationContext) child(name string) combinationContext {
	return combinationContext{name: name, depth: parent.depth + 1}
}

func parseApplicationSetGenerator(
	raw yaml.MapSlice,
	field string,
	config *conversionConfig,
	renderer applicationSetRenderer,
	parentParams map[string]any,
	parent combinationContext,
) (applicationSetGenerator, error) {
	var result applicationSetGenerator
	var generatorName string
	var generatorValue any
	var kind applicationset.Generator
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
		candidate, known := applicationset.LookupGenerator(key)
		if !known {
			return result, fmt.Errorf("%s.%s generator is not supported", field, key)
		}
		if candidate.Reason != "" {
			return result, fmt.Errorf(
				"%s.%s generator is not supported: %s",
				field,
				key,
				candidate.Reason,
			)
		}
		if generatorName != "" {
			return result, fmt.Errorf("%s must contain exactly one generator", field)
		}
		generatorName, generatorValue, kind = key, item.Value, candidate
	}
	if parent.depth > 0 && !kind.Combination {
		if err := rejectCombinationChildTemplate(raw, field, parent.name); err != nil {
			return result, err
		}
	}
	if parent.depth > 0 && kind.Combination {
		if generatorHasTemplate(generatorValue) {
			return result, fmt.Errorf(
				"%s.%s.template is not supported in a nested %s generator",
				field,
				generatorName,
				generatorName,
			)
		}
	}
	if !kind.DeferredRender && len(parentParams) != 0 {
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
	if kind.Combination && parent.depth >= 2 {
		return result, fmt.Errorf(
			"%s.%s exceeds the supported nesting depth",
			field,
			generatorName,
		)
	}
	if generatorName == "" {
		return result, fmt.Errorf("%s must contain exactly one generator", field)
	}
	generatorField := field + "." + generatorName
	items, ok := generatorValue.(yaml.MapSlice)
	if !ok {
		return result, fmt.Errorf("%s must be a mapping", generatorField)
	}
	switch generatorName {
	case "list":
		list, err := parseListOptions(items, generatorField)
		if err != nil {
			return result, err
		}
		result.template = list.template
		yamlElements, err := parseElementsYAML(
			list.elementsYAML,
			generatorField+".elementsYaml",
		)
		if err != nil {
			return result, err
		}
		groups := []struct {
			option    string
			elements  []any
			normalize func(any) (map[string]any, error)
		}{
			{"elements", list.elements, func(element any) (map[string]any, error) {
				return normalizeListElement(element, renderer.GoTemplate())
			}},
			{"elementsYaml", yamlElements, normalizeStringMap},
		}
		for _, group := range groups {
			for i, rawElement := range group.elements {
				elementField := fmt.Sprintf("%s.%s[%d]", generatorField, group.option, i)
				params, err := group.normalize(rawElement)
				if err != nil {
					return result, fmt.Errorf("%s: must be a mapping: %w", elementField, err)
				}
				result.params = append(result.params, generatedGeneratorParams{
					params: params,
					path:   elementField,
				})
			}
		}
	case "git":
		generated, err := generateGitParams(
			items,
			generatorField,
			config,
			renderer,
			parentParams,
		)
		if err != nil {
			return result, err
		}
		result.params, result.template = generated.params, generated.template
	case "clusters":
		generated, err := generateClusterParams(
			items,
			generatorField,
			config,
			renderer,
			parentParams,
		)
		if err != nil {
			return result, err
		}
		result.params, result.template = generated.params, generated.template
	case "matrix":
		generated, err := generateMatrixParams(
			items,
			generatorField,
			config,
			renderer,
			parentParams,
			parent,
		)
		if err != nil {
			return result, err
		}
		result.params, result.template = generated.params, generated.template
	case "merge":
		generated, err := generateMergeParams(
			items,
			generatorField,
			config,
			renderer,
			parent,
		)
		if err != nil {
			return result, err
		}
		result.params, result.template = generated.params, generated.template
	default:
		panic("unhandled generator: " + generatorName)
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

func rejectCombinationChildTemplate(raw yaml.MapSlice, field, parent string) error {
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
				return fmt.Errorf(
					"%s.%s.template is not supported in a %s generator",
					field,
					key,
					parent,
				)
			}
		}
	}
	return nil
}

func generatorHasTemplate(value any) bool {
	options, ok := value.(yaml.MapSlice)
	if !ok {
		return false
	}
	for _, option := range options {
		if option.Key == "template" {
			return true
		}
	}
	return false
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
			labels, err := parseGeneratorValues(item.Value, field+".matchLabels")
			if err != nil {
				return result, err
			}
			for _, label := range labels {
				result.matchLabels[label.key] = label.value
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
			values, err := readStringSequenceYAMLOption(item.Value, field+".values")
			if err != nil {
				return result, err
			}
			result.values = values
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
	return selector.matchesFlat(flat), nil
}

func (selector labelSelector) matchesFlat(flat map[string]string) bool {
	for key, want := range selector.matchLabels {
		if got, exists := flat[key]; !exists || got != want {
			return false
		}
	}
	for _, expression := range selector.matchExpressions {
		value, exists := flat[expression.key]
		contains := slices.Contains(expression.values, value)
		switch expression.operator {
		case "In":
			if !exists || !contains {
				return false
			}
		case "NotIn":
			if exists && contains {
				return false
			}
		case "Exists":
			if !exists {
				return false
			}
		case "DoesNotExist":
			if exists {
				return false
			}
		}
	}
	return true
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
