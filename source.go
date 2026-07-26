package main

import (
	"errors"
	"fmt"
	"net/url"
	"path"
	"regexp"
	"strings"
	"unicode"

	"github.com/goccy/go-yaml"
)

type applicationSource struct {
	RepoURL        string        `yaml:"repoURL"`
	Chart          string        `yaml:"chart"`
	TargetRevision string        `yaml:"targetRevision"`
	Path           string        `yaml:"path"`
	Ref            string        `yaml:"ref"`
	Helm           yaml.MapSlice `yaml:"helm"`
	Directory      yaml.MapSlice `yaml:"directory"`
	Kustomize      kustomizeMap  `yaml:"kustomize"`
	Plugin         yaml.MapSlice `yaml:"plugin"`
}

const defaultGitTargetRevision = "HEAD"

var safeReferenceName = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]*$`)

type repositoryKind int

const (
	httpRepository repositoryKind = iota
	ociRepository
	gitRepository
)

type resolvedSources struct {
	chartSource applicationSource
	field       string
	refs        map[string]applicationSource
	comments    []string
}

func resolveSources(app application, documentNumber int) (resolvedSources, error) {
	if app.Spec.Source != nil && app.Spec.Sources != nil {
		return resolvedSources{}, errors.New("spec.source and spec.sources cannot both be set")
	}
	if app.Spec.Source != nil {
		if strings.TrimSpace(app.Spec.Source.Ref) != "" {
			return resolvedSources{}, errors.New("spec.source.ref is only supported in spec.sources")
		}
		return resolvedSources{chartSource: *app.Spec.Source, field: "spec.source"}, nil
	}
	if app.Spec.Sources == nil {
		return resolvedSources{}, errors.New("spec.source or spec.sources is required")
	}
	if len(app.Spec.Sources) == 0 {
		return resolvedSources{}, errors.New("spec.sources must contain one Helm chart source")
	}

	result := resolvedSources{refs: make(map[string]applicationSource)}
	for i, source := range app.Spec.Sources {
		field := fmt.Sprintf("spec.sources[%d]", i)
		isManifestSource := strings.TrimSpace(source.Chart) != "" ||
			strings.TrimSpace(source.Path) != "" && strings.TrimSpace(source.Ref) == "" ||
			source.Kustomize != nil
		if isManifestSource {
			if result.field != "" {
				return resolvedSources{}, errors.New(
					"spec.sources must contain exactly one manifest source",
				)
			}
			if strings.TrimSpace(source.Ref) != "" {
				return resolvedSources{}, fmt.Errorf(
					"%s.ref is not supported on the manifest source",
					field,
				)
			}
			if source.Kustomize == nil &&
				(source.Directory != nil || source.Plugin != nil) {
				return resolvedSources{}, fmt.Errorf("%s contains a non-Helm source configuration", field)
			}
			result.chartSource = source
			result.field = field
			continue
		}

		if strings.TrimSpace(source.Path) != "" {
			return resolvedSources{}, fmt.Errorf("%s.path would generate manifests and is not supported", field)
		}
		if !isNilOrEmptyCollection(source.Helm) {
			return resolvedSources{}, fmt.Errorf("%s.helm is only supported on the Helm chart source", field)
		}
		if source.Directory != nil || source.Kustomize != nil || source.Plugin != nil {
			return resolvedSources{}, fmt.Errorf("%s is not a values-only ref source", field)
		}
		if err := validateReferenceName(source.Ref); err != nil {
			return resolvedSources{}, fmt.Errorf("%s.ref: %w", field, err)
		}
		if _, exists := result.refs[source.Ref]; exists {
			return resolvedSources{}, fmt.Errorf("%s.ref %q is duplicated", field, source.Ref)
		}
		if strings.TrimSpace(source.RepoURL) == "" {
			return resolvedSources{}, fmt.Errorf("%s.repoURL is required", field)
		}
		source.TargetRevision = normalizeGitTargetRevision(source.TargetRevision)
		if strings.TrimSpace(source.TargetRevision) == "" {
			return resolvedSources{}, fmt.Errorf("%s.targetRevision is required", field)
		}
		result.refs[source.Ref] = source
		result.comments = append(result.comments, fmt.Sprintf(
			"document %d values source ref %q: repoURL %q, targetRevision %q",
			documentNumber, source.Ref, source.RepoURL, source.TargetRevision,
		))
	}
	if result.field == "" {
		return resolvedSources{}, errors.New(
			"spec.sources must contain exactly one manifest source",
		)
	}
	return result, nil
}

func normalizeGitTargetRevision(targetRevision string) string {
	if targetRevision == "" {
		return defaultGitTargetRevision
	}
	return targetRevision
}

func validateReferenceName(ref string) error {
	if !safeReferenceName.MatchString(ref) || ref == "." || ref == ".." {
		return errors.New("must be a safe single path segment containing only letters, digits, '.', '_', or '-'")
	}
	return nil
}

func validatePathCharacters(value string) error {
	if value == "" {
		return errors.New("path must not be empty")
	}
	if strings.Contains(value, `\`) {
		return errors.New("backslashes are not supported")
	}
	if strings.ContainsFunc(value, unicode.IsControl) {
		return errors.New("control characters are not supported")
	}
	return nil
}

func validateRelativeValuePath(valuePath string) error {
	if err := validatePathCharacters(valuePath); err != nil {
		return err
	}
	if path.IsAbs(valuePath) || isWindowsAbsolutePath(valuePath) {
		return errors.New("absolute paths are not supported")
	}
	if strings.ContainsAny(valuePath, "*?[]{}") {
		return errors.New("glob paths are not supported")
	}
	for segment := range strings.SplitSeq(valuePath, "/") {
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

func classifyRepositoryURL(raw string) (repositoryKind, error) {
	parsed, err := url.Parse(raw)
	if err == nil && (parsed.Scheme == "http" || parsed.Scheme == "https") && parsed.Host != "" {
		return httpRepository, nil
	}
	if err == nil && parsed.Scheme == "ssh" {
		if parsed.User == nil || parsed.User.Username() == "" || parsed.Hostname() == "" ||
			parsed.Path == "" || parsed.Path == "/" || strings.ContainsAny(raw, "?#") {
			return 0, invalidRepositoryURLError()
		}
		if _, hasPassword := parsed.User.Password(); hasPassword {
			return 0, invalidRepositoryURLError()
		}
		return gitRepository, nil
	}
	if isSCPStyleGitURL(raw) {
		return gitRepository, nil
	}

	if raw == "" || strings.ContainsFunc(raw, unicode.IsSpace) || strings.Contains(raw, "://") {
		return 0, invalidRepositoryURLError()
	}
	parsed, err = url.Parse("//" + raw)
	if err != nil || parsed.Hostname() == "" || parsed.User != nil || parsed.RawQuery != "" || parsed.Fragment != "" {
		return 0, invalidRepositoryURLError()
	}
	return ociRepository, nil
}

func isSCPStyleGitURL(raw string) bool {
	if strings.Contains(raw, "://") || strings.ContainsFunc(raw, unicode.IsSpace) ||
		strings.ContainsFunc(raw, unicode.IsControl) ||
		strings.ContainsAny(raw, "?#") {
		return false
	}
	at := strings.IndexByte(raw, '@')
	if at <= 0 || strings.LastIndexByte(raw, '@') != at {
		return false
	}
	separator := strings.IndexByte(raw[at+1:], ':')
	if separator <= 0 {
		return false
	}
	separator += at + 1
	user, host, repositoryPath := raw[:at], raw[at+1:separator], raw[separator+1:]
	return strings.Trim(repositoryPath, "/") != "" &&
		!strings.ContainsAny(user, "/:") &&
		!strings.ContainsAny(host, "/:")
}

func validateGitChartPath(chartPath string) error {
	if chartPath == "." {
		return nil
	}
	if err := validateRelativeValuePath(chartPath); err != nil {
		return fmt.Errorf("must be a safe repository-relative directory: %w", err)
	}
	return nil
}

func invalidRepositoryURLError() error {
	return errors.New("spec.source.repoURL must be a valid HTTP, HTTPS, SSH Git, SCP-like Git, or scheme-less OCI repository URL")
}
