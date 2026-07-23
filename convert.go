package main

import (
	"bytes"
	"errors"
	"fmt"
	"strings"

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

type templatePath string

func (value templatePath) MarshalYAML() ([]byte, error) {
	return []byte("'" + strings.ReplaceAll(string(value), "'", "''") + "'"), nil
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
