package main

import (
	"fmt"
	"reflect"
	"strings"

	"github.com/goccy/go-yaml"
	"github.com/goccy/go-yaml/ast"
	"github.com/goccy/go-yaml/parser"
	"github.com/johejo/argocdapp2helmfile/internal/applicationmapping"
)

type helmParameter struct {
	Name        string
	Value       string
	ForceString bool
}

type helmFileParameter struct {
	Name string
	Path string
}

type helmOptions struct {
	releaseName          string
	namespace            string
	kubeVersion          string
	apiVersions          []string
	valueFiles           []string
	values               any
	valuesObject         any
	parameters           []helmParameter
	fileParameters       []helmFileParameter
	ignoreMissingValues  bool
	skipSchemaValidation bool
	skipCRDs             bool
	passCredentials      bool
}

func parseHelmOptions(items yaml.MapSlice, field string) (helmOptions, error) {
	var result helmOptions
	for _, item := range items {
		key, ok := item.Key.(string)
		if !ok {
			return result, fmt.Errorf("%s contains a non-string option name", field)
		}
		entry, known := applicationmapping.LookupHelmOption(key)
		if !known {
			if !isIgnorableEmptyYAMLOption(item.Value) {
				return result, fmt.Errorf("%s.%s is not supported", field, key)
			}
			continue
		}
		value, err := parseHelmOptionValue(entry, item.Value, field+"."+key)
		if err != nil {
			return result, err
		}
		if _, omitted := value.(omittedHelmOption); omitted {
			continue
		}
		switch entry.HelmOption {
		case "releaseName":
			result.releaseName = value.(string)
		case "namespace":
			result.namespace = value.(string)
		case "kubeVersion":
			result.kubeVersion = value.(string)
		case "apiVersions":
			result.apiVersions = value.([]string)
		case "valueFiles":
			result.valueFiles = value.([]string)
		case "values":
			result.values = value
		case "valuesObject":
			result.valuesObject = value
		case "parameters":
			result.parameters = value.([]helmParameter)
		case "fileParameters":
			result.fileParameters = value.([]helmFileParameter)
		case "ignoreMissingValueFiles":
			result.ignoreMissingValues = value.(bool)
		case "skipSchemaValidation":
			result.skipSchemaValidation = value.(bool)
		case "skipCrds":
			result.skipCRDs = value.(bool)
		case "passCredentials":
			result.passCredentials = value.(bool)
		default:
			panic("unhandled Helm option: " + entry.HelmOption)
		}
	}
	return result, nil
}

type omittedHelmOption struct{}

func parseHelmOptionValue(
	entry applicationmapping.Entry,
	value any,
	field string,
) (any, error) {
	if entry.HelmValueKind == applicationmapping.Ignored {
		return omittedHelmOption{}, nil
	}
	if entry.AllowEmpty && isIgnorableEmptyYAMLOption(value) {
		switch entry.HelmValueKind {
		case applicationmapping.String:
			return "", nil
		case applicationmapping.Boolean:
			return false, nil
		case applicationmapping.StringSequence:
			return []string(nil), nil
		case applicationmapping.InlineValues, applicationmapping.RawValues:
			return nil, nil
		case applicationmapping.Parameters:
			return omittedHelmOption{}, nil
		case applicationmapping.FileParameters:
			return omittedHelmOption{}, nil
		}
	}
	switch entry.HelmValueKind {
	case applicationmapping.String:
		text, ok := value.(string)
		if !ok {
			return nil, fmt.Errorf("%s must be a string", field)
		}
		return text, nil
	case applicationmapping.Boolean:
		return readBooleanYAMLOption(value, field)
	case applicationmapping.StringSequence:
		return readOptionalStringSequenceYAMLOption(value, field)
	case applicationmapping.InlineValues:
		inline, ok := value.(string)
		if !ok {
			return nil, fmt.Errorf("%s must be a string", field)
		}
		return decodeInlineValues(inline, field)
	case applicationmapping.RawValues:
		return value, nil
	case applicationmapping.Parameters:
		return parseParameters(value, field)
	case applicationmapping.FileParameters:
		return parseFileParameters(value, field)
	default:
		panic("unknown Helm option value kind: " + entry.HelmValueKind)
	}
}

func readOptionalStringYAMLOption(value any, field string) (string, error) {
	if isIgnorableEmptyYAMLOption(value) {
		return "", nil
	}
	text, ok := value.(string)
	if !ok {
		return "", fmt.Errorf("%s must be a string", field)
	}
	return text, nil
}

func readOptionalBooleanYAMLOption(value any, field string) (bool, error) {
	if isIgnorableEmptyYAMLOption(value) {
		return false, nil
	}
	return readBooleanYAMLOption(value, field)
}

func readBooleanYAMLOption(value any, field string) (bool, error) {
	enabled, ok := value.(bool)
	if !ok {
		return false, fmt.Errorf("%s must be a boolean", field)
	}
	return enabled, nil
}

func readOptionalStringSequenceYAMLOption(value any, field string) ([]string, error) {
	if isIgnorableEmptyYAMLOption(value) {
		return nil, nil
	}
	return readStringSequenceYAMLOption(value, field)
}

func readStringSequenceYAMLOption(value any, field string) ([]string, error) {
	sequence, ok := value.([]any)
	if !ok {
		return nil, fmt.Errorf("%s must be a sequence", field)
	}
	result := make([]string, 0, len(sequence))
	for i, raw := range sequence {
		element, ok := raw.(string)
		if !ok {
			return nil, fmt.Errorf("%s[%d] must be a string", field, i)
		}
		result = append(result, element)
	}
	return result, nil
}

func decodeInlineValues(inline, field string) (any, error) {
	if strings.TrimSpace(inline) == "" {
		return nil, nil
	}
	return decodeSingleDocument([]byte(inline), field)
}

// singleDocumentBody returns nil when the document holds nothing but comments.
func singleDocumentBody(input []byte, field string) (ast.Node, error) {
	file, err := parser.ParseBytes(input, 0)
	if err != nil {
		return nil, fmt.Errorf("decode %s: %w", field, err)
	}
	if len(file.Docs) != 1 {
		return nil, fmt.Errorf("%s must contain exactly one YAML document", field)
	}
	return file.Docs[0].Body, nil
}

func decodeSingleDocument(input []byte, field string) (any, error) {
	body, err := singleDocumentBody(input, field)
	if err != nil || body == nil {
		return nil, err
	}
	var value any
	if err := yaml.NodeToValue(body, &value, yaml.UseOrderedMap()); err != nil {
		return nil, fmt.Errorf("decode %s: %w", field, err)
	}
	return value, nil
}

type namedParameter struct {
	name        string
	value       string
	hasValue    bool
	forceString bool
}

func parseNamedParameters(
	value any,
	field string,
	valueKey string,
	allowForceString bool,
) ([]namedParameter, error) {
	sequence, ok := value.([]any)
	if !ok {
		return nil, fmt.Errorf("%s must be a sequence", field)
	}
	parameters := make([]namedParameter, 0, len(sequence))
	for i, raw := range sequence {
		items, ok := raw.(yaml.MapSlice)
		if !ok {
			return nil, fmt.Errorf("%s[%d] must be a mapping", field, i)
		}
		var parameter namedParameter
		for _, item := range items {
			key, ok := item.Key.(string)
			if !ok {
				return nil, fmt.Errorf("%s[%d] contains a non-string field name", field, i)
			}
			switch {
			case key == "name":
				name, ok := item.Value.(string)
				if !ok {
					return nil, fmt.Errorf("%s[%d].name must be a string", field, i)
				}
				parameter.name = name
			case key == valueKey:
				parameterValue, ok := item.Value.(string)
				if !ok {
					return nil, fmt.Errorf("%s[%d].%s must be a string", field, i, valueKey)
				}
				parameter.value = parameterValue
				parameter.hasValue = true
			case key == "forceString" && allowForceString:
				forceString, ok := item.Value.(bool)
				if !ok {
					return nil, fmt.Errorf("%s[%d].forceString must be a boolean", field, i)
				}
				parameter.forceString = forceString
			default:
				if !isIgnorableEmptyYAMLOption(item.Value) {
					return nil, fmt.Errorf("%s[%d].%s is not supported", field, i, key)
				}
			}
		}
		if strings.TrimSpace(parameter.name) == "" {
			return nil, fmt.Errorf("%s[%d].name is required", field, i)
		}
		parameters = append(parameters, parameter)
	}
	return parameters, nil
}

func parseParameters(value any, field string) ([]helmParameter, error) {
	parsed, err := parseNamedParameters(value, field, "value", true)
	if err != nil {
		return nil, err
	}
	parameters := make([]helmParameter, 0, len(parsed))
	for i, parameter := range parsed {
		if !parameter.hasValue {
			return nil, fmt.Errorf("%s[%d].value is required", field, i)
		}
		parameters = append(parameters, helmParameter{
			Name:        parameter.name,
			Value:       parameter.value,
			ForceString: parameter.forceString,
		})
	}
	return parameters, nil
}

func parseFileParameters(value any, field string) ([]helmFileParameter, error) {
	parsed, err := parseNamedParameters(value, field, "path", false)
	if err != nil {
		return nil, err
	}
	parameters := make([]helmFileParameter, 0, len(parsed))
	for i, parameter := range parsed {
		if !parameter.hasValue || strings.TrimSpace(parameter.value) == "" {
			return nil, fmt.Errorf("%s[%d].path is required", field, i)
		}
		parameters = append(parameters, helmFileParameter{
			Name: parameter.name,
			Path: parameter.value,
		})
	}
	return parameters, nil
}

func isNilOrEmptyCollection(value any) bool {
	if value == nil {
		return true
	}
	v := reflect.ValueOf(value)
	switch v.Kind() {
	case reflect.Array, reflect.Map, reflect.Slice:
		return v.Len() == 0
	}
	return false
}

func isIgnorableEmptyYAMLOption(value any) bool {
	if text, ok := value.(string); ok {
		return text == ""
	}
	return isNilOrEmptyCollection(value)
}
