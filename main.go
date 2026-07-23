package main

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"net/url"
	"os"
	"reflect"
	"strings"
	"unicode"

	"github.com/goccy/go-yaml"
	"github.com/goccy/go-yaml/parser"
)

type application struct {
	APIVersion string `yaml:"apiVersion"`
	Kind       string `yaml:"kind"`
	Metadata   struct {
		Name string `yaml:"name"`
	} `yaml:"metadata"`
	Spec struct {
		Destination struct {
			Namespace string `yaml:"namespace"`
		} `yaml:"destination"`
		Source struct {
			RepoURL        string        `yaml:"repoURL"`
			Chart          string        `yaml:"chart"`
			TargetRevision string        `yaml:"targetRevision"`
			Path           string        `yaml:"path"`
			Helm           yaml.MapSlice `yaml:"helm"`
		} `yaml:"source"`
		Sources []any `yaml:"sources"`
	} `yaml:"spec"`
}

type helmfile struct {
	Repositories []repository `yaml:"repositories"`
	Releases     []release    `yaml:"releases"`
}

type repository struct {
	Name string `yaml:"name"`
	URL  string `yaml:"url"`
	OCI  bool   `yaml:"oci,omitempty"`
}

type release struct {
	Name      string         `yaml:"name"`
	Namespace string         `yaml:"namespace,omitempty"`
	Chart     string         `yaml:"chart"`
	Version   string         `yaml:"version"`
	Values    []any          `yaml:"values,omitempty"`
	Set       []setParameter `yaml:"set,omitempty"`
	SetString []setParameter `yaml:"setString,omitempty"`
}

type setParameter struct {
	Name  string `yaml:"name"`
	Value string `yaml:"value"`
}

type helmParameter struct {
	Name        string
	Value       string
	ForceString bool
}

type helmOptions struct {
	releaseName  string
	values       any
	valuesObject any
	parameters   []helmParameter
}

func main() {
	os.Exit(run(os.Args[1:], os.Stdin, os.Stdout, os.Stderr))
}

func run(args []string, stdin io.Reader, stdout, stderr io.Writer) int {
	if len(args) != 0 {
		fmt.Fprintln(stderr, "argocdapp2helmfile: command-line arguments are not supported")
		return 1
	}

	input, err := io.ReadAll(stdin)
	if err != nil {
		writeDiagnostic(stderr, fmt.Errorf("read input: %w", err))
		return 1
	}
	output, err := convert(input)
	if err != nil {
		writeDiagnostic(stderr, err)
		return 1
	}
	if _, err := stdout.Write(output); err != nil {
		writeDiagnostic(stderr, fmt.Errorf("write output: %w", err))
		return 1
	}
	return 0
}

func writeDiagnostic(stderr io.Writer, err error) {
	// Parser errors can contain annotated source excerpts. Keep the CLI contract
	// of one diagnostic line while retaining the meaningful tokens and location.
	message := strings.Join(strings.Fields(err.Error()), " ")
	fmt.Fprintf(stderr, "argocdapp2helmfile: %s\n", message)
}

func convert(input []byte) ([]byte, error) {
	if err := requireSingleDocument(input, "input"); err != nil {
		return nil, err
	}
	var app application
	decoder := yaml.NewDecoder(bytes.NewReader(input), yaml.UseOrderedMap())
	if err := decoder.Decode(&app); err != nil {
		if errors.Is(err, io.EOF) {
			return nil, errors.New("input must contain exactly one YAML document")
		}
		return nil, fmt.Errorf("decode Application: %w", err)
	}
	var extra any
	if err := decoder.Decode(&extra); !errors.Is(err, io.EOF) {
		if err != nil {
			return nil, fmt.Errorf("decode additional YAML document: %w", err)
		}
		return nil, errors.New("input must contain exactly one YAML document")
	}

	if app.APIVersion != "argoproj.io/v1alpha1" {
		return nil, fmt.Errorf("apiVersion must be %q", "argoproj.io/v1alpha1")
	}
	if app.Kind != "Application" {
		return nil, errors.New("kind must be \"Application\"")
	}
	if strings.TrimSpace(app.Metadata.Name) == "" {
		return nil, errors.New("metadata.name is required")
	}
	if app.Spec.Sources != nil {
		return nil, errors.New("spec.sources is not supported")
	}
	if strings.TrimSpace(app.Spec.Source.Path) != "" {
		return nil, errors.New("spec.source.path is not supported")
	}
	oci, err := classifyRepositoryURL(app.Spec.Source.RepoURL)
	if err != nil {
		return nil, err
	}
	if strings.TrimSpace(app.Spec.Source.Chart) == "" {
		return nil, errors.New("spec.source.chart is required")
	}
	if strings.TrimSpace(app.Spec.Source.TargetRevision) == "" {
		return nil, errors.New("spec.source.targetRevision is required")
	}

	helm, err := parseHelmOptions(app.Spec.Source.Helm)
	if err != nil {
		return nil, err
	}
	releaseName := helm.releaseName
	if releaseName == "" {
		releaseName = app.Metadata.Name
	}
	values := make([]any, 0, 2)
	if !isEmpty(helm.values) {
		values = append(values, helm.values)
	}
	if !isEmpty(helm.valuesObject) {
		values = append(values, helm.valuesObject)
	}
	set := make([]setParameter, 0, len(helm.parameters))
	setString := make([]setParameter, 0, len(helm.parameters))
	for _, parameter := range helm.parameters {
		outputParameter := setParameter{Name: parameter.Name, Value: parameter.Value}
		if parameter.ForceString {
			setString = append(setString, outputParameter)
		} else {
			set = append(set, outputParameter)
		}
	}

	result := helmfile{
		Repositories: []repository{{Name: "source", URL: app.Spec.Source.RepoURL, OCI: oci}},
		Releases: []release{{
			Name:      releaseName,
			Namespace: app.Spec.Destination.Namespace,
			Chart:     "source/" + app.Spec.Source.Chart,
			Version:   app.Spec.Source.TargetRevision,
			Values:    values,
			Set:       set,
			SetString: setString,
		}},
	}

	var output bytes.Buffer
	if err := yaml.NewEncoder(&output, yaml.Indent(2), yaml.IndentSequence(true)).Encode(result); err != nil {
		return nil, fmt.Errorf("encode helmfile: %w", err)
	}
	return output.Bytes(), nil
}

func classifyRepositoryURL(raw string) (bool, error) {
	parsed, err := url.Parse(raw)
	if err == nil && (parsed.Scheme == "http" || parsed.Scheme == "https") && parsed.Host != "" {
		return false, nil
	}

	if raw == "" || strings.IndexFunc(raw, unicode.IsSpace) >= 0 || strings.Contains(raw, "://") {
		return false, errors.New("spec.source.repoURL must be a valid HTTP, HTTPS, or scheme-less OCI repository URL")
	}
	parsed, err = url.Parse("//" + raw)
	if err != nil || parsed.Hostname() == "" || parsed.User != nil || parsed.RawQuery != "" || parsed.Fragment != "" {
		return false, errors.New("spec.source.repoURL must be a valid HTTP, HTTPS, or scheme-less OCI repository URL")
	}
	return true, nil
}

func parseHelmOptions(items yaml.MapSlice) (helmOptions, error) {
	var result helmOptions
	for _, item := range items {
		key, ok := item.Key.(string)
		if !ok {
			return result, errors.New("spec.source.helm contains a non-string option name")
		}
		switch key {
		case "releaseName":
			if isEmpty(item.Value) {
				continue
			}
			value, ok := item.Value.(string)
			if !ok {
				return result, errors.New("spec.source.helm.releaseName must be a string")
			}
			result.releaseName = value
		case "values":
			if isEmpty(item.Value) {
				continue
			}
			inline, ok := item.Value.(string)
			if !ok {
				return result, errors.New("spec.source.helm.values must be a string")
			}
			value, err := decodeInlineValues(inline)
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
			parameters, err := parseParameters(item.Value)
			if err != nil {
				return result, err
			}
			result.parameters = parameters
		default:
			if !isEmpty(item.Value) {
				return result, fmt.Errorf("spec.source.helm.%s is not supported", key)
			}
		}
	}
	return result, nil
}

func decodeInlineValues(inline string) (any, error) {
	if strings.TrimSpace(inline) == "" {
		return nil, nil
	}
	if err := requireSingleDocument([]byte(inline), "spec.source.helm.values"); err != nil {
		return nil, err
	}
	decoder := yaml.NewDecoder(strings.NewReader(inline), yaml.UseOrderedMap())
	var value any
	if err := decoder.Decode(&value); err != nil {
		if errors.Is(err, io.EOF) {
			return nil, nil
		}
		return nil, fmt.Errorf("decode spec.source.helm.values: %w", err)
	}
	var extra any
	if err := decoder.Decode(&extra); !errors.Is(err, io.EOF) {
		if err != nil {
			return nil, fmt.Errorf("decode additional spec.source.helm.values document: %w", err)
		}
		return nil, errors.New("spec.source.helm.values must contain exactly one YAML document")
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

func parseParameters(value any) ([]helmParameter, error) {
	sequence, ok := value.([]any)
	if !ok {
		return nil, errors.New("spec.source.helm.parameters must be a sequence")
	}
	parameters := make([]helmParameter, 0, len(sequence))
	for i, raw := range sequence {
		items, ok := raw.(yaml.MapSlice)
		if !ok {
			return nil, fmt.Errorf("spec.source.helm.parameters[%d] must be a mapping", i)
		}
		var parameter helmParameter
		var hasValue bool
		for _, item := range items {
			key, ok := item.Key.(string)
			if !ok {
				return nil, fmt.Errorf("spec.source.helm.parameters[%d] contains a non-string field name", i)
			}
			switch key {
			case "name":
				name, ok := item.Value.(string)
				if !ok {
					return nil, fmt.Errorf("spec.source.helm.parameters[%d].name must be a string", i)
				}
				parameter.Name = name
			case "value":
				parameterValue, ok := item.Value.(string)
				if !ok {
					return nil, fmt.Errorf("spec.source.helm.parameters[%d].value must be a string", i)
				}
				parameter.Value = parameterValue
				hasValue = true
			case "forceString":
				forceString, ok := item.Value.(bool)
				if !ok {
					return nil, fmt.Errorf("spec.source.helm.parameters[%d].forceString must be a boolean", i)
				}
				parameter.ForceString = forceString
			default:
				if !isEmpty(item.Value) {
					return nil, fmt.Errorf("spec.source.helm.parameters[%d].%s is not supported", i, key)
				}
			}
		}
		if strings.TrimSpace(parameter.Name) == "" {
			return nil, fmt.Errorf("spec.source.helm.parameters[%d].name is required", i)
		}
		if !hasValue {
			return nil, fmt.Errorf("spec.source.helm.parameters[%d].value is required", i)
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
