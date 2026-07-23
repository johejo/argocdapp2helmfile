package main

import (
	"bytes"
	"errors"
	"fmt"
	"reflect"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"text/template"

	"github.com/Masterminds/sprig/v3"
	"github.com/goccy/go-yaml"
	"github.com/goccy/go-yaml/ast"
	"github.com/gosimple/slug"
)

type inputOrigin struct {
	document int
	path     string
}

func (origin inputOrigin) String() string {
	if origin.path == "" {
		return fmt.Sprintf("document %d", origin.document)
	}
	return fmt.Sprintf("document %d: %s", origin.document, origin.path)
}

func (origin inputOrigin) wrap(err error) error {
	if origin.path == "" {
		return fmt.Errorf("document %d: %w", origin.document, err)
	}
	return fmt.Errorf("document %d: %s: %w", origin.document, origin.path, err)
}

type generatedApplication struct {
	application application
	path        string
}

type applicationSetResource struct {
	APIVersion string `yaml:"apiVersion"`
	Kind       string `yaml:"kind"`
	Metadata   struct {
		Name string `yaml:"name"`
	} `yaml:"metadata"`
	Spec struct {
		GoTemplate        bool            `yaml:"goTemplate"`
		GoTemplateOptions []string        `yaml:"goTemplateOptions"`
		Generators        []yaml.MapSlice `yaml:"generators"`
		Template          yaml.MapSlice   `yaml:"template"`
		TemplatePatch     any             `yaml:"templatePatch"`
	} `yaml:"spec"`
}

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

func expandApplicationSet(node ast.Node) ([]generatedApplication, error) {
	var appSet applicationSetResource
	if err := yaml.NodeToValue(node, &appSet, yaml.UseOrderedMap()); err != nil {
		return nil, fmt.Errorf("decode ApplicationSet: %w", err)
	}
	if appSet.APIVersion != "argoproj.io/v1alpha1" {
		return nil, fmt.Errorf("apiVersion must be %q", "argoproj.io/v1alpha1")
	}
	if strings.TrimSpace(appSet.Metadata.Name) == "" {
		return nil, errors.New("metadata.name is required")
	}
	if !appSet.Spec.GoTemplate {
		return nil, errors.New("spec.goTemplate must be true; fasttemplate is not supported")
	}
	if len(appSet.Spec.Generators) == 0 {
		return nil, errors.New("spec.generators must contain at least one List generator")
	}
	if appSet.Spec.Template == nil {
		return nil, errors.New("spec.template is required")
	}
	if !isEmpty(appSet.Spec.TemplatePatch) {
		return nil, errors.New("spec.templatePatch is not supported")
	}
	if _, err := newApplicationSetTemplate(appSet.Spec.GoTemplateOptions); err != nil {
		return nil, err
	}

	var generated []generatedApplication
	for generatorIndex, rawGenerator := range appSet.Spec.Generators {
		field := fmt.Sprintf("spec.generators[%d]", generatorIndex)
		list, selector, err := parseListGenerator(rawGenerator, field)
		if err != nil {
			return nil, err
		}
		mergedTemplate := mergeTemplate(list.template, appSet.Spec.Template)
		elements := append([]any(nil), list.elements...)
		yamlElements, err := parseElementsYAML(list.elementsYAML, field+".list.elementsYaml")
		if err != nil {
			return nil, err
		}
		elements = append(elements, yamlElements...)

		for elementIndex, rawElement := range elements {
			elementField := fmt.Sprintf("%s.list.elements[%d]", field, elementIndex)
			if elementIndex >= len(list.elements) {
				elementField = fmt.Sprintf(
					"%s.list.elementsYaml[%d]",
					field,
					elementIndex-len(list.elements),
				)
			}
			params, err := normalizeStringMap(rawElement)
			if err != nil {
				return nil, fmt.Errorf("%s: must be a mapping: %w", elementField, err)
			}
			if selector != nil {
				matches, err := selector.matches(params)
				if err != nil {
					return nil, fmt.Errorf("%s.selector: %w", field, err)
				}
				if !matches {
					continue
				}
			}
			rendered, err := renderApplicationTemplate(
				mergedTemplate,
				params,
				appSet.Spec.GoTemplateOptions,
			)
			if err != nil {
				return nil, fmt.Errorf("%s: render template: %w", elementField, err)
			}
			generated = append(generated, generatedApplication{
				application: rendered,
				path:        elementField,
			})
		}
	}
	return generated, nil
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
			if isEmpty(item.Value) {
				continue
			}
			elements, ok := item.Value.([]any)
			if !ok {
				return result, fmt.Errorf("%s.elements must be a sequence", field)
			}
			result.elements = elements
		case "elementsYaml":
			if isEmpty(item.Value) {
				continue
			}
			value, ok := item.Value.(string)
			if !ok {
				return result, fmt.Errorf("%s.elementsYaml must be a string", field)
			}
			result.elementsYAML = value
		case "template":
			if isEmpty(item.Value) {
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

func mergeTemplate(override, base yaml.MapSlice) yaml.MapSlice {
	if override == nil {
		return cloneMapSlice(base)
	}
	result := cloneMapSlice(override)
	for _, baseItem := range base {
		index := mapSliceIndex(result, baseItem.Key)
		if index < 0 {
			result = append(result, cloneMapItem(baseItem))
			continue
		}
		overrideMap, overrideOK := result[index].Value.(yaml.MapSlice)
		baseMap, baseOK := baseItem.Value.(yaml.MapSlice)
		if overrideOK && baseOK {
			result[index].Value = mergeTemplate(overrideMap, baseMap)
		} else if isEmpty(result[index].Value) {
			result[index].Value = cloneValue(baseItem.Value)
		}
	}
	return result
}

func cloneMapSlice(value yaml.MapSlice) yaml.MapSlice {
	result := make(yaml.MapSlice, len(value))
	for i, item := range value {
		result[i] = cloneMapItem(item)
	}
	return result
}

func cloneMapItem(item yaml.MapItem) yaml.MapItem {
	return yaml.MapItem{Key: cloneValue(item.Key), Value: cloneValue(item.Value)}
}

func cloneValue(value any) any {
	switch typed := value.(type) {
	case yaml.MapSlice:
		return cloneMapSlice(typed)
	case []any:
		result := make([]any, len(typed))
		for i, item := range typed {
			result[i] = cloneValue(item)
		}
		return result
	default:
		return value
	}
}

func mapSliceIndex(items yaml.MapSlice, key any) int {
	for i, item := range items {
		if reflect.DeepEqual(item.Key, key) {
			return i
		}
	}
	return -1
}

func renderApplicationTemplate(
	input yaml.MapSlice,
	params map[string]any,
	options []string,
) (application, error) {
	renderer, err := newApplicationSetTemplate(options)
	if err != nil {
		return application{}, err
	}
	rendered, err := renderTemplateValue(input, params, renderer)
	if err != nil {
		return application{}, err
	}
	data, err := yaml.Marshal(rendered)
	if err != nil {
		return application{}, fmt.Errorf("encode rendered Application: %w", err)
	}
	var app application
	if err := yaml.UnmarshalWithOptions(data, &app, yaml.UseOrderedMap()); err != nil {
		return application{}, fmt.Errorf("decode rendered Application: %w", err)
	}
	app.APIVersion = "argoproj.io/v1alpha1"
	app.Kind = "Application"
	return app, nil
}

func newApplicationSetTemplate(options []string) (*template.Template, error) {
	functions := sprig.GenericFuncMap()
	delete(functions, "env")
	delete(functions, "expandenv")
	delete(functions, "getHostByName")
	functions["normalize"] = normalizeName
	functions["slugify"] = slugifyName
	functions["toYaml"] = templateToYAML
	functions["fromYaml"] = templateFromYAML
	functions["fromYamlArray"] = templateFromYAMLArray

	result := template.New("field").Funcs(functions)
	for _, option := range options {
		if strings.TrimSpace(option) == "" {
			return nil, errors.New("spec.goTemplateOptions must not contain an empty option")
		}
		switch option {
		case "missingkey=default", "missingkey=invalid", "missingkey=zero", "missingkey=error":
		default:
			return nil, fmt.Errorf("spec.goTemplateOptions contains unsupported option %q", option)
		}
		result = result.Option(option)
	}
	return result, nil
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

var invalidDNSNameCharacters = regexp.MustCompile(`[^a-z0-9.-]+`)

func normalizeName(value string) string {
	value = strings.ToLower(value)
	value = invalidDNSNameCharacters.ReplaceAllString(value, "-")
	value = strings.Trim(value, "-.")
	if len(value) > 253 {
		value = value[:253]
		value = strings.TrimRight(value, "-.")
	}
	return value
}

func slugifyName(args ...any) (string, error) {
	maxLength := 50
	smartTruncate := true
	name := ""
	switch len(args) {
	case 1:
		value, ok := args[0].(string)
		if !ok {
			return "", errors.New("slugify expects a string")
		}
		name = value
	case 2:
		length, ok := args[0].(int)
		if !ok {
			return "", errors.New("slugify maximum length must be an integer")
		}
		value, ok := args[1].(string)
		if !ok {
			return "", errors.New("slugify expects a string")
		}
		maxLength, name = length, value
	case 3:
		length, ok := args[0].(int)
		if !ok {
			return "", errors.New("slugify maximum length must be an integer")
		}
		smart, ok := args[1].(bool)
		if !ok {
			return "", errors.New("slugify smart truncation flag must be a boolean")
		}
		value, ok := args[2].(string)
		if !ok {
			return "", errors.New("slugify expects a string")
		}
		maxLength, smartTruncate, name = length, smart, value
	default:
		return "", errors.New("slugify expects a name with optional maximum length and smart truncation flag")
	}
	if maxLength < 0 {
		return "", errors.New("slugify maximum length must not be negative")
	}
	slug.EnableSmartTruncate = smartTruncate
	slug.MaxLength = maxLength
	return slug.Make(normalizeName(name)), nil
}

func templateToYAML(value any) (string, error) {
	data, err := yaml.Marshal(value)
	if err != nil {
		return "", err
	}
	return strings.TrimSuffix(string(data), "\n"), nil
}

func templateFromYAML(input string) (any, error) {
	var value any
	if err := yaml.UnmarshalWithOptions([]byte(input), &value, yaml.UseOrderedMap()); err != nil {
		return nil, err
	}
	return normalizeTemplateValue(value)
}

func templateFromYAMLArray(input string) ([]any, error) {
	value, err := templateFromYAML(input)
	if err != nil {
		return nil, err
	}
	result, ok := value.([]any)
	if !ok {
		return nil, errors.New("YAML value is not an array")
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
		contains := stringSliceContains(expression.values, value)
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

func stringSliceContains(values []string, target string) bool {
	return slices.Contains(values, target)
}
