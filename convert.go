package main

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"path/filepath"
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
	Repositories []repository  `yaml:"repositories,omitempty"`
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
	Chart                any            `yaml:"chart"`
	Version              string         `yaml:"version,omitempty"`
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
	return convertWithSourceMap(input, nil)
}

func convertWithSourceMap(input []byte, resolver *sourceResolver) ([]byte, error) {
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
		converted, err := convertApplication(app, documentNumber, resolver)
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

		if converted.repository != nil {
			alias, exists := repositoryAliases[converted.repository.URL]
			if !exists {
				alias = repositoryAlias(len(result.Repositories))
				repositoryAliases[converted.repository.URL] = alias
				converted.repository.Name = alias
				result.Repositories = append(result.Repositories, *converted.repository)
			}
			converted.release.Chart = alias + "/" + converted.chart
		}
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
	repository         *repository
	release            release
	chart              string
	skipCRDs           bool
	provenanceComments []string
}

func convertApplication(app application, documentNumber int, resolver *sourceResolver) (convertedApplication, error) {
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
	repositoryType, err := classifyRepositoryURL(chartSource.RepoURL)
	if err != nil {
		return converted, err
	}
	hasChart := strings.TrimSpace(chartSource.Chart) != ""
	hasPath := strings.TrimSpace(chartSource.Path) != ""
	if hasChart && hasPath {
		return converted, fmt.Errorf("%s.chart and %s.path cannot both be set", chartSourceField, chartSourceField)
	}
	if hasPath && repositoryType == httpRepository {
		repositoryType = gitRepository
	}
	if repositoryType == gitRepository {
		if hasChart {
			return converted, fmt.Errorf("%s.chart is not supported for a Git repository; use path", chartSourceField)
		}
		if !hasPath {
			return converted, fmt.Errorf("%s.path is required for a Git repository", chartSourceField)
		}
		if err := validateGitChartPath(chartSource.Path); err != nil {
			return converted, fmt.Errorf("%s.path %q: %w", chartSourceField, chartSource.Path, err)
		}
	} else {
		if hasPath {
			return converted, fmt.Errorf("%s.path is only supported for a Git repository", chartSourceField)
		}
		if !hasChart {
			return converted, fmt.Errorf("%s.chart is required", chartSourceField)
		}
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
	var chartLocal *localSource
	var chartRoot string
	if repositoryType == gitRepository {
		local, err := resolver.resolve(chartSource, chartSourceField)
		if err != nil {
			return converted, err
		}
		chartLocal = &local
		chartRoot = filepath.Clean(filepath.Join(local.root, filepath.FromSlash(chartSource.Path)))
		if err := verifyLexicalPathWithinRoot(chartRoot, local.root); err != nil {
			return converted, fmt.Errorf("%s.path %q: %w", chartSourceField, chartSource.Path, err)
		}
		if err := verifyPathWithinRoot(chartRoot, local.root); err != nil {
			return converted, fmt.Errorf("%s.path %q: %w", chartSourceField, chartSource.Path, err)
		}
		info, err := os.Stat(chartRoot)
		if err != nil {
			return converted, fmt.Errorf("%s.path %q: inspect chart directory: %w", chartSourceField, chartSource.Path, err)
		}
		if !info.IsDir() {
			return converted, fmt.Errorf("%s.path %q must point to a chart directory", chartSourceField, chartSource.Path)
		}
		if info, err := os.Stat(filepath.Join(chartRoot, "Chart.yaml")); err != nil || info.IsDir() {
			if err == nil {
				err = errors.New("is a directory")
			}
			return converted, fmt.Errorf("%s.path %q: Chart.yaml: %w", chartSourceField, chartSource.Path, err)
		}
	}
	values, err := resolveValueFiles(helm.valueFiles, helm.ignoreMissingValues, valueFileContext{
		chartLocal: chartLocal,
		chartRoot:  chartRoot,
		refs:       refs,
		resolver:   resolver,
	})
	if err != nil {
		return converted, fmt.Errorf("%s.helm.%w", chartSourceField, err)
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
		chart:              chartSource.Chart,
		skipCRDs:           helm.skipCRDs,
		provenanceComments: provenance,
		release: release{
			Name:                 releaseName,
			Namespace:            app.Spec.Destination.Namespace,
			Values:               values,
			Set:                  set,
			SetString:            setString,
			MissingFileHandler:   missingFileHandler(helm.ignoreMissingValues),
			SkipSchemaValidation: helm.skipSchemaValidation,
		},
	}
	if repositoryType == gitRepository {
		chartRelative, err := filepath.Rel(chartLocal.root, chartRoot)
		if err != nil {
			return converted, fmt.Errorf("%s.path %q: %w", chartSourceField, chartSource.Path, err)
		}
		chartTemplate := fmt.Sprintf(`{{ requiredEnv %q }}`, chartLocal.env)
		if chartRelative != "." {
			chartTemplate += "/" + filepath.ToSlash(chartRelative)
		}
		converted.release.Chart = templatePath(chartTemplate)
		converted.provenanceComments = append([]string{fmt.Sprintf(
			"document %d chart source: repoURL %q, path %q, targetRevision %q",
			documentNumber, chartSource.RepoURL, chartSource.Path, chartSource.TargetRevision,
		)}, converted.provenanceComments...)
	} else {
		repository := repository{URL: chartSource.RepoURL, OCI: repositoryType == ociRepository}
		converted.repository = &repository
		converted.release.Version = chartSource.TargetRevision
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
