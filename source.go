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
	Kustomize      yaml.MapSlice `yaml:"kustomize"`
	Plugin         yaml.MapSlice `yaml:"plugin"`
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
