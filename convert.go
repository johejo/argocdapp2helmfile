package main

import (
	"errors"
	"fmt"
	"net/url"
	"slices"
	"strings"

	"github.com/goccy/go-yaml"
)

type applicationDestination struct {
	Name      string `yaml:"name"`
	Server    string `yaml:"server"`
	Namespace string `yaml:"namespace"`
}

type application struct {
	APIVersion string `yaml:"apiVersion"`
	Kind       string `yaml:"kind"`
	Metadata   struct {
		Name      string            `yaml:"name"`
		Namespace string            `yaml:"namespace"`
		Labels    map[string]string `yaml:"labels"`
	} `yaml:"metadata"`
	Spec struct {
		Destination          applicationDestination `yaml:"destination"`
		Project              string                 `yaml:"project"`
		RevisionHistoryLimit *int                   `yaml:"revisionHistoryLimit"`
		Source               *applicationSource     `yaml:"source"`
		Sources              []applicationSource    `yaml:"sources"`
		SyncPolicy           struct {
			SyncOptions []string `yaml:"syncOptions"`
		} `yaml:"syncPolicy"`
	} `yaml:"spec"`
}

type helmfile struct {
	Repositories []repository  `yaml:"repositories,omitempty"`
	HelmDefaults *helmDefaults `yaml:"helmDefaults,omitempty"`
	Releases     []release     `yaml:"releases"`
}

type helmDefaults struct {
	SkipCRDs        bool `yaml:"skipCRDs,omitempty"`
	CreateNamespace bool `yaml:"createNamespace"`
}

type repository struct {
	Name            string `yaml:"name"`
	URL             string `yaml:"url"`
	OCI             bool   `yaml:"oci,omitempty"`
	PassCredentials bool   `yaml:"passCredentials,omitempty"`
}

type release struct {
	Name                 string         `yaml:"name"`
	Namespace            string         `yaml:"namespace,omitempty"`
	KubeContext          string         `yaml:"kubeContext,omitempty"`
	HistoryMax           int            `yaml:"historyMax,omitempty"`
	Labels               yaml.MapSlice  `yaml:"labels,omitempty"`
	Chart                any            `yaml:"chart"`
	Version              string         `yaml:"version,omitempty"`
	KubeVersion          string         `yaml:"kubeVersion,omitempty"`
	APIVersions          []string       `yaml:"apiVersions,omitempty"`
	Values               []any          `yaml:"values,omitempty"`
	Set                  []setParameter `yaml:"set,omitempty"`
	SetString            []setParameter `yaml:"setString,omitempty"`
	MissingFileHandler   string         `yaml:"missingFileHandler,omitempty"`
	SkipSchemaValidation bool           `yaml:"skipSchemaValidation,omitempty"`
	CreateNamespace      bool           `yaml:"createNamespace,omitempty"`
	Transformers         []any          `yaml:"transformers,omitempty"`
	Needs                []string       `yaml:"needs,omitempty"`
}

type setParameter struct {
	Name  string       `yaml:"name"`
	Value *string      `yaml:"value,omitempty"`
	File  templatePath `yaml:"file,omitempty"`
}

type templatePath string

func (value templatePath) MarshalYAML() ([]byte, error) {
	return []byte("'" + strings.ReplaceAll(string(value), "'", "''") + "'"), nil
}

func convert(input []byte) ([]byte, error) {
	return convertWithConfig(input, nil)
}

func convertWithConfig(input []byte, config *conversionConfig) ([]byte, error) {
	applications, err := decodeApplicationInputs(input, config)
	if err != nil {
		return nil, err
	}

	builder := newHelmfileBuilder(config)
	for _, item := range applications {
		if err := builder.add(item); err != nil {
			return nil, err
		}
	}
	return builder.finalize()
}

type convertedApplication struct {
	repository         *repository
	release            release
	chart              string
	skipCRDs           bool
	skipCRDsApplicable bool
	provenanceComments []string
}

func convertApplication(
	app application,
	documentNumber int,
	resolver *sourceResolver,
	destinationResolver *destinationResolver,
) (convertedApplication, error) {
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
	if app.Spec.RevisionHistoryLimit != nil {
		switch {
		case *app.Spec.RevisionHistoryLimit == 0:
			return converted, errors.New(
				"spec.revisionHistoryLimit cannot be 0: Argo CD disables revision history, " +
					"but Helmfile historyMax 0 means unlimited history",
			)
		case *app.Spec.RevisionHistoryLimit < 0:
			return converted, fmt.Errorf(
				"spec.revisionHistoryLimit cannot convert %d: history limit must be greater than 0",
				*app.Spec.RevisionHistoryLimit,
			)
		}
	}
	kubeContext, err := destinationResolver.resolve(app.Spec.Destination, "spec.destination")
	if err != nil {
		return converted, err
	}
	sources, err := resolveSources(app, documentNumber)
	if err != nil {
		return converted, err
	}
	chartSource := sources.chartSource
	chartSourceField := sources.field
	refs := sources.refs
	provenance := sources.comments
	isKustomization := chartSource.Kustomize != nil
	if isKustomization {
		for _, conflict := range []struct {
			name string
			set  bool
		}{
			{"chart", strings.TrimSpace(chartSource.Chart) != ""},
			{"helm", chartSource.Helm != nil},
			{"directory", chartSource.Directory != nil},
			{"plugin", chartSource.Plugin != nil},
		} {
			if conflict.set {
				return converted, fmt.Errorf(
					"%s.%s and %s.kustomize cannot both be set",
					chartSourceField,
					conflict.name,
					chartSourceField,
				)
			}
		}
	} else if chartSource.Directory != nil || chartSource.Plugin != nil {
		return converted, fmt.Errorf("%s contains a non-Helm source configuration", chartSourceField)
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
	if (hasPath || isKustomization) && repositoryType == httpRepository {
		repositoryType = gitRepository
	}
	if repositoryType == gitRepository {
		chartSource.TargetRevision = normalizeGitTargetRevision(chartSource.TargetRevision)
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

	baseRelease := release{
		Name:            app.Metadata.Name,
		Namespace:       app.Spec.Destination.Namespace,
		KubeContext:     kubeContext,
		HistoryMax:      valueOrZero(app.Spec.RevisionHistoryLimit),
		CreateNamespace: slices.Contains(app.Spec.SyncPolicy.SyncOptions, "CreateNamespace=true"),
	}

	if isKustomization {
		if repositoryType != gitRepository {
			return converted, fmt.Errorf("%s.kustomize requires a Git repository", chartSourceField)
		}
		options, err := parseKustomizeOptions(
			chartSource.Kustomize,
			chartSourceField+".kustomize",
		)
		if err != nil {
			return converted, err
		}
		transformers, err := options.transformers(
			applicationBuildEnvironment(app, chartSource),
			chartSourceField+".kustomize",
		)
		if err != nil {
			return converted, err
		}
		mapping, err := resolver.resolve(chartSource, chartSourceField)
		if err != nil {
			return converted, err
		}
		values := options.values()
		var releaseValues []any
		if len(values) != 0 {
			releaseValues = []any{values}
		}
		kustomizeRelease := baseRelease
		kustomizeRelease.Chart = templatePath(joinSourcePath(mapping.localRoot, chartSource.Path))
		kustomizeRelease.Values = releaseValues
		kustomizeRelease.Transformers = transformers
		return convertedApplication{
			provenanceComments: append(
				[]string{gitSourceProvenance("kustomization", documentNumber, chartSource)},
				provenance...,
			),
			release: kustomizeRelease,
		}, nil
	}

	helm, err := parseHelmOptions(chartSource.Helm, chartSourceField+".helm")
	if err != nil {
		return converted, err
	}
	if helm.namespace != "" && helm.namespace != app.Spec.Destination.Namespace {
		return converted, fmt.Errorf(
			"%s.helm.namespace %q must match spec.destination.namespace %q",
			chartSourceField, helm.namespace, app.Spec.Destination.Namespace,
		)
	}
	for _, parameter := range helm.parameters {
		if !parameter.ForceString {
			continue
		}
		for _, fileParameter := range helm.fileParameters {
			if parameter.Name == fileParameter.Name {
				return converted, fmt.Errorf(
					"%s.helm.fileParameters name %q conflicts with a forceString parameter",
					chartSourceField, parameter.Name,
				)
			}
		}
	}
	releaseName := helm.releaseName
	if releaseName == "" {
		releaseName = app.Metadata.Name
	}
	var chartMapping *mappedSource
	var chartRoot string
	if repositoryType == gitRepository {
		mapping, err := resolver.resolve(chartSource, chartSourceField)
		if err != nil {
			return converted, err
		}
		if err := validateGitChartSource(mapping, chartSource, chartSourceField); err != nil {
			return converted, err
		}
		chartMapping = &mapping
		chartRoot = chartSource.Path
	}
	values, err := resolveValueFiles(helm.valueFiles, valueFileContext{
		chartMapping: chartMapping,
		chartRoot:    chartRoot,
		environment:  applicationBuildEnvironment(app, chartSource),
		refs:         refs,
		resolver:     resolver,
	}, helm.ignoreMissingValues)
	if err != nil {
		return converted, fmt.Errorf("%s.helm.%w", chartSourceField, err)
	}
	if !isNilOrEmptyCollection(helm.values) {
		values = append(values, helm.values)
	}
	if !isNilOrEmptyCollection(helm.valuesObject) {
		values = append(values, helm.valuesObject)
	}
	set := make([]setParameter, 0, len(helm.parameters)+len(helm.fileParameters))
	setString := make([]setParameter, 0, len(helm.parameters))
	for _, parameter := range helm.parameters {
		value := parameter.Value
		outputParameter := setParameter{Name: parameter.Name, Value: &value}
		if parameter.ForceString {
			setString = append(setString, outputParameter)
		} else {
			set = append(set, outputParameter)
		}
	}
	for i, parameter := range helm.fileParameters {
		resolved, err := resolveSourcePath(parameter.Path, valueFileContext{
			chartMapping: chartMapping,
			chartRoot:    chartRoot,
			refs:         refs,
			resolver:     resolver,
		}, "fileParameters")
		if err != nil {
			return converted, fmt.Errorf("%s.helm.fileParameters[%d].path: %w", chartSourceField, i, err)
		}
		set = append(set, setParameter{Name: parameter.Name, File: templatePath(resolved)})
	}

	helmRelease := baseRelease
	helmRelease.Name = releaseName
	helmRelease.Values = values
	helmRelease.Set = set
	helmRelease.SetString = setString
	helmRelease.MissingFileHandler = missingFileHandler(helm.ignoreMissingValues)
	helmRelease.SkipSchemaValidation = helm.skipSchemaValidation
	helmRelease.KubeVersion = helm.kubeVersion
	helmRelease.APIVersions = helm.apiVersions
	converted = convertedApplication{
		chart:              chartSource.Chart,
		skipCRDs:           helm.skipCRDs,
		skipCRDsApplicable: true,
		provenanceComments: provenance,
		release:            helmRelease,
	}
	if repositoryType == gitRepository {
		converted.release.Chart = templatePath(joinSourcePath(chartMapping.localRoot, chartRoot))
		converted.provenanceComments = append(
			[]string{gitSourceProvenance("chart", documentNumber, chartSource)},
			converted.provenanceComments...,
		)
	} else {
		repository := repository{
			URL:             chartSource.RepoURL,
			OCI:             repositoryType == ociRepository,
			PassCredentials: helm.passCredentials,
		}
		converted.repository = &repository
		converted.release.Version = chartSource.TargetRevision
	}
	return converted, nil
}

func gitSourceProvenance(kind string, documentNumber int, source applicationSource) string {
	return fmt.Sprintf(
		"document %d %s source: repoURL %q, path %q, targetRevision %q",
		documentNumber, kind, source.RepoURL, source.Path, source.TargetRevision,
	)
}

func valueOrZero(value *int) int {
	if value == nil {
		return 0
	}
	return *value
}

func repositoryAlias(repositoryURL string) string {
	raw := repositoryURL
	if !strings.Contains(raw, "://") {
		raw = "//" + raw
	}
	parsed, err := url.Parse(raw)
	if err != nil {
		return "source"
	}

	var segment string
	for escapedSegment := range strings.SplitSeq(parsed.EscapedPath(), "/") {
		if escapedSegment == "" {
			continue
		}
		decoded, err := url.PathUnescape(escapedSegment)
		if err != nil {
			continue
		}
		segment = decoded
	}
	alias := normalizeRepositoryAlias(segment)
	if alias == "" {
		return "source"
	}
	return alias
}

func normalizeRepositoryAlias(candidate string) string {
	var normalized strings.Builder
	previousInvalid := false
	for _, char := range strings.ToLower(candidate) {
		valid := char >= 'a' && char <= 'z' ||
			char >= '0' && char <= '9' ||
			char == '-' || char == '.'
		if valid {
			normalized.WriteRune(char)
			previousInvalid = false
		} else if !previousInvalid {
			normalized.WriteByte('-')
			previousInvalid = true
		}
	}
	return strings.Trim(normalized.String(), "-.")
}

func uniqueRepositoryAlias(candidate string, used map[string]struct{}) string {
	if _, exists := used[candidate]; !exists {
		return candidate
	}
	for suffix := 2; ; suffix++ {
		alias := fmt.Sprintf("%s-%d", candidate, suffix)
		if _, exists := used[alias]; !exists {
			return alias
		}
	}
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
		if after, ok := strings.CutPrefix(line, "---"); ok {
			remainder := strings.TrimSpace(after)
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
