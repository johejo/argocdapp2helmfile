package main

import (
	"errors"
	"fmt"
	"regexp"
	"strings"
	"text/template"

	"github.com/Masterminds/sprig/v3"
	"github.com/goccy/go-yaml"
	"github.com/gosimple/slug"
)

func newGoTemplateRenderer(options []string) (applicationSetRenderer, error) {
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
	return goTemplateRenderer{
		template: result,
		parsed:   make(map[string]*template.Template),
	}, nil
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
