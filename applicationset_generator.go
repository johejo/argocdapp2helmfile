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

// generatorRequest is everything a generator parser may need, so that one
// dispatch table can key on the catalog name alone. Parsers that do not nest
// children ignore parent, and merge re-derives its children's parameters
// instead of reading parentParams.
type generatorRequest struct {
	items        yaml.MapSlice
	field        string
	config       *conversionConfig
	renderer     applicationSetRenderer
	parentParams map[string]any
	parent       combinationContext
}

// generatorParsers holds one entry per convertible catalog generator. The
// catalog decides whether a generator is accepted, so a name the catalog
// accepts without an entry here is a programming error, not user input.
// The combination parsers recurse back into parseApplicationSetGenerator, so
// the table is filled in init to keep Go from seeing an initialization cycle.
var generatorParsers map[string]func(generatorRequest) (generatorResult, error)

func init() {
	generatorParsers = map[string]func(generatorRequest) (generatorResult, error){
		"list":     generateListParams,
		"git":      generateGitParams,
		"clusters": generateClusterParams,
		"matrix":   generateMatrixParams,
		"merge":    generateMergeParams,
	}
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
			items, err := readMappingYAMLOption(selectorValue, field+".selector")
			if err != nil {
				return result, err
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
	if parent.depth > 0 {
		if kind.Combination {
			if generatorHasTemplate(generatorValue) {
				return result, fmt.Errorf(
					"%s.%s.template is not supported in a nested %s generator",
					field,
					generatorName,
					generatorName,
				)
			}
		} else if err := rejectCombinationChildTemplate(raw, field, parent.name); err != nil {
			return result, err
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
	parse, handled := generatorParsers[generatorName]
	if !handled {
		panic("unhandled generator: " + generatorName)
	}
	generated, err := parse(generatorRequest{
		items:        items,
		field:        generatorField,
		config:       config,
		renderer:     renderer,
		parentParams: parentParams,
		parent:       parent,
	})
	if err != nil {
		return result, err
	}
	result.params, result.template = generated.params, generated.template
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

func rejectCombinationChildTemplate(raw yaml.MapSlice, field, parent string) error {
	for _, item := range raw {
		key, ok := item.Key.(string)
		if !ok || key == "selector" {
			continue
		}
		if generatorHasTemplate(item.Value) {
			return fmt.Errorf(
				"%s.%s.template is not supported in a %s generator",
				field,
				key,
				parent,
			)
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
			expressions, err := parseLabelExpressions(item.Value, field)
			if err != nil {
				return result, err
			}
			result.matchExpressions = append(result.matchExpressions, expressions...)
		default:
			return result, fmt.Errorf("%s.%s is not supported", field, key)
		}
	}
	return result, nil
}

func parseLabelExpressions(value any, field string) ([]labelExpression, error) {
	rawExpressions, ok := value.([]any)
	if !ok {
		return nil, fmt.Errorf("%s.matchExpressions must be a sequence", field)
	}
	result := make([]labelExpression, 0, len(rawExpressions))
	for i, rawExpression := range rawExpressions {
		expressionField := fmt.Sprintf("%s.matchExpressions[%d]", field, i)
		items, ok := rawExpression.(yaml.MapSlice)
		if !ok {
			return nil, fmt.Errorf("%s must be a mapping", expressionField)
		}
		expression, err := parseLabelExpression(items, expressionField)
		if err != nil {
			return nil, err
		}
		result = append(result, expression)
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
