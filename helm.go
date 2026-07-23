package main

import (
	"errors"
	"fmt"
	"io"
	"reflect"
	"strings"

	"github.com/goccy/go-yaml"
	"github.com/goccy/go-yaml/parser"
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
		switch key {
		case "releaseName":
			value, err := readOptionalStringYAMLOption(item.Value, field+".releaseName")
			if err != nil {
				return result, err
			}
			result.releaseName = value
		case "namespace":
			value, err := readOptionalStringYAMLOption(item.Value, field+".namespace")
			if err != nil {
				return result, err
			}
			result.namespace = value
		case "kubeVersion":
			value, err := readOptionalStringYAMLOption(item.Value, field+".kubeVersion")
			if err != nil {
				return result, err
			}
			result.kubeVersion = value
		case "apiVersions":
			value, err := readOptionalStringSequenceYAMLOption(
				item.Value,
				field+".apiVersions",
			)
			if err != nil {
				return result, err
			}
			result.apiVersions = value
		case "valueFiles":
			value, err := readOptionalStringSequenceYAMLOption(
				item.Value,
				field+".valueFiles",
			)
			if err != nil {
				return result, err
			}
			result.valueFiles = value
		case "values":
			inline, err := readOptionalStringYAMLOption(item.Value, field+".values")
			if err != nil {
				return result, err
			}
			value, err := decodeInlineValues(inline, field+".values")
			if err != nil {
				return result, err
			}
			result.values = value
		case "valuesObject":
			result.valuesObject = item.Value
		case "parameters":
			if isIgnorableEmptyYAMLOption(item.Value) {
				continue
			}
			parameters, err := parseParameters(item.Value, field+".parameters")
			if err != nil {
				return result, err
			}
			result.parameters = parameters
		case "fileParameters":
			if isIgnorableEmptyYAMLOption(item.Value) {
				continue
			}
			fileParameters, err := parseFileParameters(item.Value, field+".fileParameters")
			if err != nil {
				return result, err
			}
			result.fileParameters = fileParameters
		case "ignoreMissingValueFiles":
			value, err := readOptionalBooleanYAMLOption(
				item.Value,
				field+".ignoreMissingValueFiles",
			)
			if err != nil {
				return result, err
			}
			result.ignoreMissingValues = value
		case "skipSchemaValidation":
			value, err := readOptionalBooleanYAMLOption(
				item.Value,
				field+".skipSchemaValidation",
			)
			if err != nil {
				return result, err
			}
			result.skipSchemaValidation = value
		case "skipCrds":
			value, err := readOptionalBooleanYAMLOption(item.Value, field+".skipCrds")
			if err != nil {
				return result, err
			}
			result.skipCRDs = value
		case "passCredentials":
			value, err := readBooleanYAMLOption(item.Value, field+".passCredentials")
			if err != nil {
				return result, err
			}
			result.passCredentials = value
		case "skipTests", "version":
			// These compatibility and invocation options do not change the
			// release definition represented by the generated helmfile.
			continue
		default:
			if !isIgnorableEmptyYAMLOption(item.Value) {
				return result, fmt.Errorf("%s.%s is not supported", field, key)
			}
		}
	}
	return result, nil
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
	if err := requireSingleDocument([]byte(inline), field); err != nil {
		return nil, err
	}
	decoder := yaml.NewDecoder(strings.NewReader(inline), yaml.UseOrderedMap())
	var value any
	if err := decoder.Decode(&value); err != nil {
		if errors.Is(err, io.EOF) {
			return nil, nil
		}
		return nil, fmt.Errorf("decode %s: %w", field, err)
	}
	var extra any
	if err := decoder.Decode(&extra); !errors.Is(err, io.EOF) {
		if err != nil {
			return nil, fmt.Errorf("decode additional %s document: %w", field, err)
		}
		return nil, fmt.Errorf("%s must contain exactly one YAML document", field)
	}
	return value, nil
}

func requireSingleDocument(input []byte, field string) error {
	file, err := parser.ParseBytes(input, 0)
	if err != nil {
		return fmt.Errorf("decode %s: %w", field, err)
	}
	if len(file.Docs) != 1 {
		return fmt.Errorf("%s must contain exactly one YAML document", field)
	}
	return nil
}

func parseParameters(value any, field string) ([]helmParameter, error) {
	sequence, ok := value.([]any)
	if !ok {
		return nil, fmt.Errorf("%s must be a sequence", field)
	}
	parameters := make([]helmParameter, 0, len(sequence))
	for i, raw := range sequence {
		items, ok := raw.(yaml.MapSlice)
		if !ok {
			return nil, fmt.Errorf("%s[%d] must be a mapping", field, i)
		}
		var parameter helmParameter
		var hasValue bool
		for _, item := range items {
			key, ok := item.Key.(string)
			if !ok {
				return nil, fmt.Errorf("%s[%d] contains a non-string field name", field, i)
			}
			switch key {
			case "name":
				name, ok := item.Value.(string)
				if !ok {
					return nil, fmt.Errorf("%s[%d].name must be a string", field, i)
				}
				parameter.Name = name
			case "value":
				parameterValue, ok := item.Value.(string)
				if !ok {
					return nil, fmt.Errorf("%s[%d].value must be a string", field, i)
				}
				parameter.Value = parameterValue
				hasValue = true
			case "forceString":
				forceString, ok := item.Value.(bool)
				if !ok {
					return nil, fmt.Errorf("%s[%d].forceString must be a boolean", field, i)
				}
				parameter.ForceString = forceString
			default:
				if !isIgnorableEmptyYAMLOption(item.Value) {
					return nil, fmt.Errorf("%s[%d].%s is not supported", field, i, key)
				}
			}
		}
		if strings.TrimSpace(parameter.Name) == "" {
			return nil, fmt.Errorf("%s[%d].name is required", field, i)
		}
		if !hasValue {
			return nil, fmt.Errorf("%s[%d].value is required", field, i)
		}
		parameters = append(parameters, parameter)
	}
	return parameters, nil
}

func parseFileParameters(value any, field string) ([]helmFileParameter, error) {
	sequence, ok := value.([]any)
	if !ok {
		return nil, fmt.Errorf("%s must be a sequence", field)
	}
	parameters := make([]helmFileParameter, 0, len(sequence))
	for i, raw := range sequence {
		items, ok := raw.(yaml.MapSlice)
		if !ok {
			return nil, fmt.Errorf("%s[%d] must be a mapping", field, i)
		}
		var parameter helmFileParameter
		var hasPath bool
		for _, item := range items {
			key, ok := item.Key.(string)
			if !ok {
				return nil, fmt.Errorf("%s[%d] contains a non-string field name", field, i)
			}
			switch key {
			case "name":
				name, ok := item.Value.(string)
				if !ok {
					return nil, fmt.Errorf("%s[%d].name must be a string", field, i)
				}
				parameter.Name = name
			case "path":
				parameterPath, ok := item.Value.(string)
				if !ok {
					return nil, fmt.Errorf("%s[%d].path must be a string", field, i)
				}
				parameter.Path = parameterPath
				hasPath = true
			default:
				if !isIgnorableEmptyYAMLOption(item.Value) {
					return nil, fmt.Errorf("%s[%d].%s is not supported", field, i, key)
				}
			}
		}
		if strings.TrimSpace(parameter.Name) == "" {
			return nil, fmt.Errorf("%s[%d].name is required", field, i)
		}
		if !hasPath || strings.TrimSpace(parameter.Path) == "" {
			return nil, fmt.Errorf("%s[%d].path is required", field, i)
		}
		parameters = append(parameters, parameter)
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
