package main

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"net/url"
	"os"
	"path"
	"reflect"
	"regexp"
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
		Source  *applicationSource  `yaml:"source"`
		Sources []applicationSource `yaml:"sources"`
	} `yaml:"spec"`
}

type applicationSource struct {
	RepoURL        string        `yaml:"repoURL"`
	Chart          string        `yaml:"chart"`
	TargetRevision string        `yaml:"targetRevision"`
	Path           string        `yaml:"path"`
	Ref            string        `yaml:"ref"`
	Helm           yaml.MapSlice `yaml:"helm"`
	Directory      yaml.MapSlice `yaml:"directory"`
	Kustomize      yaml.MapSlice `yaml:"kustomize"`
	Plugin         yaml.MapSlice `yaml:"plugin"`
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
	MissingFileHandler   string         `yaml:"missingFileHandler,omitempty"`
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
	valueFiles           []string
	values               any
	valuesObject         any
	parameters           []helmParameter
	ignoreMissingValues  bool
	skipSchemaValidation bool
	skipCRDs             bool
}

type templatePath string

func (value templatePath) MarshalYAML() ([]byte, error) {
	return []byte("'" + strings.ReplaceAll(string(value), "'", "''") + "'"), nil
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
	var provenanceComments []string
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
		converted, err := convertApplication(app, documentNumber)
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
		provenanceComments = append(provenanceComments, converted.provenanceComments...)
	}
	if len(file.Docs) == 0 {
		return nil, errors.New("document 1: document must contain an Application")
	}
	if sharedSkipCRDs {
		result.HelmDefaults = &helmDefaults{SkipCRDs: true}
	}

	var output bytes.Buffer
	for _, comment := range provenanceComments {
		fmt.Fprintf(&output, "# %s\n", comment)
	}
	if err := yaml.NewEncoder(&output, yaml.Indent(2), yaml.IndentSequence(true)).Encode(result); err != nil {
		return nil, fmt.Errorf("encode helmfile: %w", err)
	}
	return output.Bytes(), nil
}

type convertedApplication struct {
	repository         repository
	release            release
	chart              string
	skipCRDs           bool
	provenanceComments []string
}

func convertApplication(app application, documentNumber int) (convertedApplication, error) {
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
	chartSource, chartSourceField, refs, provenance, err := resolveSources(app, documentNumber)
	if err != nil {
		return converted, err
	}
	oci, err := classifyRepositoryURL(chartSource.RepoURL)
	if err != nil {
		return converted, err
	}
	if strings.TrimSpace(chartSource.Chart) == "" {
		return converted, fmt.Errorf("%s.chart is required", chartSourceField)
	}
	if strings.TrimSpace(chartSource.TargetRevision) == "" {
		return converted, fmt.Errorf("%s.targetRevision is required", chartSourceField)
	}

	helm, err := parseHelmOptions(chartSource.Helm, chartSourceField+".helm")
	if err != nil {
		return converted, err
	}
	releaseName := helm.releaseName
	if releaseName == "" {
		releaseName = app.Metadata.Name
	}
	values := make([]any, 0, len(helm.valueFiles)+2)
	for i, valueFile := range helm.valueFiles {
		resolved, err := resolveValueFile(valueFile, documentNumber, refs)
		if err != nil {
			return converted, fmt.Errorf("%s.helm.valueFiles[%d]: %w", chartSourceField, i, err)
		}
		values = append(values, templatePath(resolved))
	}
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
		repository:         repository{URL: chartSource.RepoURL, OCI: oci},
		chart:              chartSource.Chart,
		skipCRDs:           helm.skipCRDs,
		provenanceComments: provenance,
		release: release{
			Name:                 releaseName,
			Namespace:            app.Spec.Destination.Namespace,
			Version:              chartSource.TargetRevision,
			Values:               values,
			Set:                  set,
			SetString:            setString,
			MissingFileHandler:   missingFileHandler(helm.ignoreMissingValues),
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

var safeReferenceName = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]*$`)

func resolveSources(app application, documentNumber int) (applicationSource, string, map[string]struct{}, []string, error) {
	if app.Spec.Source != nil && app.Spec.Sources != nil {
		return applicationSource{}, "", nil, nil, errors.New("spec.source and spec.sources cannot both be set")
	}
	if app.Spec.Source != nil {
		if strings.TrimSpace(app.Spec.Source.Path) != "" {
			return applicationSource{}, "", nil, nil, errors.New("spec.source.path is not supported")
		}
		if strings.TrimSpace(app.Spec.Source.Ref) != "" {
			return applicationSource{}, "", nil, nil, errors.New("spec.source.ref is only supported in spec.sources")
		}
		return *app.Spec.Source, "spec.source", nil, nil, nil
	}
	if app.Spec.Sources == nil {
		return applicationSource{}, "", nil, nil, errors.New("spec.source or spec.sources is required")
	}
	if len(app.Spec.Sources) == 0 {
		return applicationSource{}, "", nil, nil, errors.New("spec.sources must contain one Helm chart source")
	}

	refs := make(map[string]struct{})
	var chartSource applicationSource
	chartSourceField := ""
	var comments []string
	for i, source := range app.Spec.Sources {
		field := fmt.Sprintf("spec.sources[%d]", i)
		if strings.TrimSpace(source.Chart) != "" {
			if chartSourceField != "" {
				return applicationSource{}, "", nil, nil, errors.New("spec.sources must contain exactly one Helm chart source")
			}
			if strings.TrimSpace(source.Path) != "" {
				return applicationSource{}, "", nil, nil, fmt.Errorf("%s.path is not supported", field)
			}
			if strings.TrimSpace(source.Ref) != "" {
				return applicationSource{}, "", nil, nil, fmt.Errorf("%s.ref is not supported on the Helm chart source", field)
			}
			if source.Directory != nil || source.Kustomize != nil || source.Plugin != nil {
				return applicationSource{}, "", nil, nil, fmt.Errorf("%s contains a non-Helm source configuration", field)
			}
			chartSource = source
			chartSourceField = field
			continue
		}

		if strings.TrimSpace(source.Path) != "" {
			return applicationSource{}, "", nil, nil, fmt.Errorf("%s.path would generate manifests and is not supported", field)
		}
		if !isEmpty(source.Helm) {
			return applicationSource{}, "", nil, nil, fmt.Errorf("%s.helm is only supported on the Helm chart source", field)
		}
		if source.Directory != nil || source.Kustomize != nil || source.Plugin != nil {
			return applicationSource{}, "", nil, nil, fmt.Errorf("%s is not a values-only ref source", field)
		}
		if err := validateReferenceName(source.Ref); err != nil {
			return applicationSource{}, "", nil, nil, fmt.Errorf("%s.ref: %w", field, err)
		}
		if _, exists := refs[source.Ref]; exists {
			return applicationSource{}, "", nil, nil, fmt.Errorf("%s.ref %q is duplicated", field, source.Ref)
		}
		if strings.TrimSpace(source.RepoURL) == "" {
			return applicationSource{}, "", nil, nil, fmt.Errorf("%s.repoURL is required", field)
		}
		if strings.TrimSpace(source.TargetRevision) == "" {
			return applicationSource{}, "", nil, nil, fmt.Errorf("%s.targetRevision is required", field)
		}
		refs[source.Ref] = struct{}{}
		comments = append(comments, fmt.Sprintf(
			"document %d values source ref %q: repoURL %q, targetRevision %q",
			documentNumber, source.Ref, source.RepoURL, source.TargetRevision,
		))
	}
	if chartSourceField == "" {
		return applicationSource{}, "", nil, nil, errors.New("spec.sources must contain exactly one Helm chart source")
	}
	return chartSource, chartSourceField, refs, comments, nil
}

func validateReferenceName(ref string) error {
	if !safeReferenceName.MatchString(ref) || ref == "." || ref == ".." {
		return errors.New("must be a safe single path segment containing only letters, digits, '.', '_', or '-'")
	}
	return nil
}

func resolveValueFile(valueFile string, documentNumber int, refs map[string]struct{}) (string, error) {
	base := fmt.Sprintf(`{{ requiredEnv "ARGOCDAPP2HELMFILE_VALUES_ROOT" }}/document-%d`, documentNumber)
	if strings.HasPrefix(valueFile, "$") {
		separator := strings.IndexByte(valueFile, '/')
		if separator < 0 {
			return "", errors.New("a $ref value file must include a path after the reference")
		}
		ref := strings.TrimPrefix(valueFile[:separator], "$")
		if err := validateReferenceName(ref); err != nil {
			return "", fmt.Errorf("reference %q is unsafe: %w", ref, err)
		}
		if _, exists := refs[ref]; !exists {
			return "", fmt.Errorf("reference %q is not defined by spec.sources", ref)
		}
		relative := valueFile[separator+1:]
		if err := validateRelativeValuePath(relative); err != nil {
			return "", err
		}
		return base + "/refs/" + ref + "/" + relative, nil
	}
	if err := validateRelativeValuePath(valueFile); err != nil {
		return "", err
	}
	return base + "/chart/" + valueFile, nil
}

func validateRelativeValuePath(valuePath string) error {
	if valuePath == "" {
		return errors.New("path must not be empty")
	}
	if strings.Contains(valuePath, `\`) {
		return errors.New("backslashes are not supported")
	}
	if strings.IndexFunc(valuePath, unicode.IsControl) >= 0 {
		return errors.New("control characters are not supported")
	}
	if path.IsAbs(valuePath) || isWindowsAbsolutePath(valuePath) {
		return errors.New("absolute paths are not supported")
	}
	if strings.ContainsAny(valuePath, "*?[]{}") {
		return errors.New("glob paths are not supported")
	}
	for _, segment := range strings.Split(valuePath, "/") {
		if segment == "" || segment == "." || segment == ".." {
			return errors.New("path must contain only non-empty segments other than '.' or '..'")
		}
	}
	return nil
}

func isWindowsAbsolutePath(valuePath string) bool {
	if len(valuePath) < 3 || valuePath[1] != ':' || valuePath[2] != '/' {
		return false
	}
	drive := valuePath[0]
	return drive >= 'A' && drive <= 'Z' || drive >= 'a' && drive <= 'z'
}

func missingFileHandler(ignoreMissing bool) string {
	if ignoreMissing {
		return "Warn"
	}
	return ""
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
