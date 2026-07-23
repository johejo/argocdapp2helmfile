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
	valueFiles           []string
	values               any
	valuesObject         any
	parameters           []helmParameter
	fileParameters       []helmFileParameter
	ignoreMissingValues  bool
	skipSchemaValidation bool
	skipCRDs             bool
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
			if isEmpty(item.Value) {
				continue
			}
			value, ok := item.Value.(string)
			if !ok {
				return result, fmt.Errorf("%s.releaseName must be a string", field)
			}
			result.releaseName = value
		case "valueFiles":
			if isEmpty(item.Value) {
				continue
			}
			sequence, ok := item.Value.([]any)
			if !ok {
				return result, fmt.Errorf("%s.valueFiles must be a sequence", field)
			}
			result.valueFiles = make([]string, 0, len(sequence))
			for i, raw := range sequence {
				valueFile, ok := raw.(string)
				if !ok {
					return result, fmt.Errorf("%s.valueFiles[%d] must be a string", field, i)
				}
				result.valueFiles = append(result.valueFiles, valueFile)
			}
		case "values":
			if isEmpty(item.Value) {
				continue
			}
			inline, ok := item.Value.(string)
			if !ok {
				return result, fmt.Errorf("%s.values must be a string", field)
			}
			value, err := decodeInlineValues(inline, field+".values")
			if err != nil {
				return result, err
			}
			result.values = value
		case "valuesObject":
			result.valuesObject = item.Value
		case "parameters":
			if isEmpty(item.Value) {
				continue
			}
			parameters, err := parseParameters(item.Value, field+".parameters")
			if err != nil {
				return result, err
			}
			result.parameters = parameters
		case "fileParameters":
			if isEmpty(item.Value) {
				continue
			}
			fileParameters, err := parseFileParameters(item.Value, field+".fileParameters")
			if err != nil {
				return result, err
			}
			result.fileParameters = fileParameters
		case "ignoreMissingValueFiles":
			if isEmpty(item.Value) {
				continue
			}
			value, ok := item.Value.(bool)
			if !ok {
				return result, fmt.Errorf("%s.ignoreMissingValueFiles must be a boolean", field)
			}
			result.ignoreMissingValues = value
		case "skipSchemaValidation":
			if isEmpty(item.Value) {
				continue
			}
			value, ok := item.Value.(bool)
			if !ok {
				return result, fmt.Errorf("%s.skipSchemaValidation must be a boolean", field)
			}
			result.skipSchemaValidation = value
		case "skipCrds":
			if isEmpty(item.Value) {
				continue
			}
			value, ok := item.Value.(bool)
			if !ok {
				return result, fmt.Errorf("%s.skipCrds must be a boolean", field)
			}
			result.skipCRDs = value
		default:
			if !isEmpty(item.Value) {
				return result, fmt.Errorf("%s.%s is not supported", field, key)
			}
		}
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
				if !isEmpty(item.Value) {
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
				if !isEmpty(item.Value) {
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

func isEmpty(value any) bool {
	if value == nil {
		return true
	}
	if ordered, ok := value.(yaml.MapSlice); ok {
		return len(ordered) == 0
	}
	v := reflect.ValueOf(value)
	switch v.Kind() {
	case reflect.Array, reflect.Map, reflect.Slice, reflect.String:
		return v.Len() == 0
	case reflect.Bool:
		return !v.Bool()
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		return v.Int() == 0
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		return v.Uint() == 0
	case reflect.Float32, reflect.Float64:
		return v.Float() == 0
	}
	return false
}
