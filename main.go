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
	Repositories []repository  `yaml:"repositories"`
	HelmDefaults *helmDefaults `yaml:"helmDefaults,omitempty"`
	Releases     []release     `yaml:"releases"`
}

type helmDefaults struct {
	SkipCRDs bool `yaml:"skipCRDs,omitempty"`
}

type repository struct {
	Name string `yaml:"name"`
	URL  string `yaml:"url"`
	OCI  bool   `yaml:"oci,omitempty"`
}

type release struct {
	Name                 string         `yaml:"name"`
	Namespace            string         `yaml:"namespace,omitempty"`
	Chart                string         `yaml:"chart"`
	Version              string         `yaml:"version"`
	Values               []any          `yaml:"values,omitempty"`
	Set                  []setParameter `yaml:"set,omitempty"`
	SetString            []setParameter `yaml:"setString,omitempty"`
	SkipSchemaValidation bool           `yaml:"skipSchemaValidation,omitempty"`
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
	releaseName          string
	values               any
	valuesObject         any
	parameters           []helmParameter
	skipSchemaValidation bool
	skipCRDs             bool
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
	file, err := parser.ParseBytes(input, 0)
	if err != nil {
		return nil, fmt.Errorf("document %d: decode Application: %w", documentNumberForError(input, err), err)
	}

	result := helmfile{}
	repositoryAliases := make(map[string]string)
	releaseDocuments := make(map[string]int)
	var sharedSkipCRDs bool
	for i, document := range file.Docs {
		documentNumber := i + 1
		if document.Body == nil {
			return nil, fmt.Errorf("document %d: document must contain an Application", documentNumber)
		}

		var app application
		if err := yaml.NodeToValue(document.Body, &app, yaml.UseOrderedMap()); err != nil {
			return nil, fmt.Errorf("document %d: decode Application: %w", documentNumber, err)
		}
		converted, err := convertApplication(app)
		if err != nil {
			return nil, fmt.Errorf("document %d: %w", documentNumber, err)
		}

		if previousDocument, exists := releaseDocuments[converted.release.Name]; exists {
			return nil, fmt.Errorf(
				"document %d: release name %q duplicates document %d",
				documentNumber,
				converted.release.Name,
				previousDocument,
			)
		}
		releaseDocuments[converted.release.Name] = documentNumber

		if documentNumber == 1 {
			sharedSkipCRDs = converted.skipCRDs
		} else if converted.skipCRDs != sharedSkipCRDs {
			return nil, fmt.Errorf("document %d: spec.source.helm.skipCrds conflicts with document 1", documentNumber)
		}

		alias, exists := repositoryAliases[converted.repository.URL]
		if !exists {
			alias = repositoryAlias(len(result.Repositories))
			repositoryAliases[converted.repository.URL] = alias
			converted.repository.Name = alias
			result.Repositories = append(result.Repositories, converted.repository)
		}
		converted.release.Chart = alias + "/" + converted.chart
		result.Releases = append(result.Releases, converted.release)
	}
	if len(file.Docs) == 0 {
		return nil, errors.New("document 1: document must contain an Application")
	}
	if sharedSkipCRDs {
		result.HelmDefaults = &helmDefaults{SkipCRDs: true}
	}

	var output bytes.Buffer
	if err := yaml.NewEncoder(&output, yaml.Indent(2), yaml.IndentSequence(true)).Encode(result); err != nil {
		return nil, fmt.Errorf("encode helmfile: %w", err)
	}
	return output.Bytes(), nil
}

type convertedApplication struct {
	repository repository
	release    release
	chart      string
	skipCRDs   bool
}

func convertApplication(app application) (convertedApplication, error) {
	var converted convertedApplication

	if app.APIVersion != "argoproj.io/v1alpha1" {
		return converted, fmt.Errorf("apiVersion must be %q", "argoproj.io/v1alpha1")
	}
	if app.Kind != "Application" {
		return converted, errors.New("kind must be \"Application\"")
	}
	if strings.TrimSpace(app.Metadata.Name) == "" {
		return converted, errors.New("metadata.name is required")
	}
	if app.Spec.Sources != nil {
		return converted, errors.New("spec.sources is not supported")
	}
	if strings.TrimSpace(app.Spec.Source.Path) != "" {
		return converted, errors.New("spec.source.path is not supported")
	}
	oci, err := classifyRepositoryURL(app.Spec.Source.RepoURL)
	if err != nil {
		return converted, err
	}
	if strings.TrimSpace(app.Spec.Source.Chart) == "" {
		return converted, errors.New("spec.source.chart is required")
	}
	if strings.TrimSpace(app.Spec.Source.TargetRevision) == "" {
		return converted, errors.New("spec.source.targetRevision is required")
	}

	helm, err := parseHelmOptions(app.Spec.Source.Helm)
	if err != nil {
		return converted, err
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

	converted = convertedApplication{
		repository: repository{URL: app.Spec.Source.RepoURL, OCI: oci},
		chart:      app.Spec.Source.Chart,
		skipCRDs:   helm.skipCRDs,
		release: release{
			Name:                 releaseName,
			Namespace:            app.Spec.Destination.Namespace,
			Version:              app.Spec.Source.TargetRevision,
			Values:               values,
			Set:                  set,
			SetString:            setString,
			SkipSchemaValidation: helm.skipSchemaValidation,
		},
	}
	return converted, nil
}

func repositoryAlias(index int) string {
	if index == 0 {
		return "source"
	}
	return fmt.Sprintf("source-%d", index+1)
}

func documentNumberForError(input []byte, err error) int {
	var yamlError yaml.Error
	if !errors.As(err, &yamlError) || yamlError.GetToken() == nil {
		return 1
	}
	targetLine := yamlError.GetToken().Position.Line
	lines := strings.Split(string(input), "\n")
	documentHeaders := 0
	contentBeforeFirstHeader := false
	for lineNumber, line := range lines {
		if lineNumber+1 > targetLine {
			break
		}
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(line, "---") {
			remainder := strings.TrimSpace(strings.TrimPrefix(line, "---"))
			if remainder == "" || strings.HasPrefix(remainder, "#") {
				documentHeaders++
				continue
			}
		}
		if documentHeaders == 0 && trimmed != "" && !strings.HasPrefix(trimmed, "#") && !strings.HasPrefix(trimmed, "%") {
			contentBeforeFirstHeader = true
		}
	}
	if documentHeaders == 0 {
		return 1
	}
	if contentBeforeFirstHeader {
		return documentHeaders + 1
	}
	return documentHeaders
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
		case "skipSchemaValidation":
			if isEmpty(item.Value) {
				continue
			}
			value, ok := item.Value.(bool)
			if !ok {
				return result, errors.New("spec.source.helm.skipSchemaValidation must be a boolean")
			}
			result.skipSchemaValidation = value
		case "skipCrds":
			if isEmpty(item.Value) {
				continue
			}
			value, ok := item.Value.(bool)
			if !ok {
				return result, errors.New("spec.source.helm.skipCrds must be a boolean")
			}
			result.skipCRDs = value
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
