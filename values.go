package main

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"strings"

	"github.com/bmatcuk/doublestar/v4"
	"github.com/johejo/argocdapp2helmfile/internal/applicationmapping"
)

type valueFileContext struct {
	chartMapping *mappedSource
	chartRoot    string
	environment  map[string]string
	refs         map[string]applicationSource
	resolver     *sourceResolver
}

type resolvedValueFile struct {
	mapping            mappedSource
	repositoryRelative string
	output             string
	glob               bool
}

func resolveValueFiles(
	valueFiles []string,
	context valueFileContext,
	ignoreMissing bool,
) ([]any, error) {
	resolved := make([]resolvedValueFile, len(valueFiles))
	explicit := make(map[string]struct{}, len(valueFiles))
	for i, valueFile := range valueFiles {
		expanded, leadingDollarLiteral, err := expandBuildEnvironment(
			valueFile,
			context.environment,
		)
		if err != nil {
			return nil, fmt.Errorf("valueFiles[%d]: %w", i, err)
		}
		entry, err := resolveValuePath(expanded, context, !leadingDollarLiteral)
		if err != nil {
			return nil, fmt.Errorf("valueFiles[%d]: %w", i, err)
		}
		resolved[i] = entry
		if !entry.glob {
			explicit[entry.output] = struct{}{}
		}
	}

	result := make([]any, 0, len(valueFiles))
	emitted := make(map[string]struct{}, len(valueFiles))
	for i, entry := range resolved {
		if !entry.glob {
			if _, exists := emitted[entry.output]; exists {
				continue
			}
			emitted[entry.output] = struct{}{}
			result = append(result, templatePath(entry.output))
			continue
		}

		matches, err := expandValueFileGlob(entry)
		if err != nil {
			return nil, fmt.Errorf("valueFiles[%d]: %w", i, err)
		}
		if len(matches) == 0 {
			if ignoreMissing {
				continue
			}
			return nil, fmt.Errorf(
				"valueFiles[%d]: glob %q matched no files",
				i,
				entry.repositoryRelative,
			)
		}
		for _, match := range matches {
			output := entry.mapping.join(match)
			if _, exists := explicit[output]; exists {
				continue
			}
			if _, exists := emitted[output]; exists {
				continue
			}
			emitted[output] = struct{}{}
			result = append(result, templatePath(output))
		}
	}
	return result, nil
}

func resolveValuePath(
	raw string,
	context valueFileContext,
	parseReference bool,
) (resolvedValueFile, error) {
	mapping, repositoryRelative, err := resolveSourceLocation(
		raw,
		context,
		"valueFiles",
		parseReference,
	)
	if err != nil {
		return resolvedValueFile{}, err
	}
	isGlob := containsValueFileGlob(repositoryRelative)
	if isGlob && !doublestar.ValidatePattern(repositoryRelative) {
		return resolvedValueFile{}, fmt.Errorf("invalid glob %q", repositoryRelative)
	}
	return resolvedValueFile{
		mapping:            mapping,
		repositoryRelative: repositoryRelative,
		output:             mapping.join(repositoryRelative),
		glob:               isGlob,
	}, nil
}

func resolveSourcePath(raw string, context valueFileContext, option string) (string, error) {
	expanded, leadingDollarLiteral, err := expandBuildEnvironment(raw, context.environment)
	if err != nil {
		return "", err
	}
	mapping, repositoryRelative, err := resolveSourceLocation(
		expanded,
		context,
		option,
		!leadingDollarLiteral,
	)
	if err != nil {
		return "", err
	}
	return mapping.join(repositoryRelative), nil
}

func resolveSourceLocation(
	raw string,
	context valueFileContext,
	option string,
	parseReference bool,
) (mappedSource, string, error) {
	if err := validatePathCharacters(raw); err != nil {
		return mappedSource{}, "", err
	}
	if strings.Contains(raw, "://") {
		return mappedSource{}, "", fmt.Errorf("remote URLs in %s are not supported", option)
	}

	var mapping mappedSource
	var base, relative string
	if parseReference && strings.HasPrefix(raw, "$") {
		before, after, ok := strings.Cut(raw, "/")
		if !ok {
			return mappedSource{}, "", errors.New(
				"a $ref value file must include a path after the reference",
			)
		}
		ref := strings.TrimPrefix(before, "$")
		source, exists := context.refs[ref]
		if !exists {
			return mappedSource{}, "", fmt.Errorf(
				"reference %q is not defined by spec.sources",
				ref,
			)
		}
		resolved, err := context.resolver.resolve(source, "values source ref "+fmt.Sprintf("%q", ref))
		if err != nil {
			return mappedSource{}, "", err
		}
		mapping = resolved
		relative = after
	} else {
		if context.chartMapping == nil {
			return mappedSource{}, "", fmt.Errorf(
				"non-$ref %s are not supported for HTTP or OCI Helm charts",
				option,
			)
		}
		mapping = *context.chartMapping
		base = context.chartRoot
		relative = raw
	}
	if strings.HasPrefix(relative, "/") {
		base = ""
		relative = strings.TrimLeft(relative, "/")
	}
	if relative == "" {
		return mappedSource{}, "", errors.New("path must not be empty")
	}

	repositoryRelative := path.Clean(path.Join(base, relative))
	if repositoryRelative == ".." || strings.HasPrefix(repositoryRelative, "../") {
		return mappedSource{}, "", errors.New("path resolves outside repository root")
	}
	return mapping, repositoryRelative, nil
}

func containsValueFileGlob(value string) bool {
	return strings.ContainsAny(value, "*?[")
}

func expandValueFileGlob(entry resolvedValueFile) ([]string, error) {
	root := entry.mapping.localRoot
	canonicalRoot, err := canonicalLocalRoot(root)
	if err != nil {
		return nil, err
	}

	var matches []string
	err = doublestar.GlobWalk(
		os.DirFS(root),
		entry.repositoryRelative,
		func(logical string, _ fs.DirEntry) error {
			candidate := filepath.Join(canonicalRoot, filepath.FromSlash(logical))
			_, inside, err := pathWithinRoot(canonicalRoot, candidate)
			if err != nil {
				return fmt.Errorf("check matched path %q: %w", logical, err)
			}
			if !inside {
				return fmt.Errorf("matched path %q resolves outside config localRoot", logical)
			}
			matches = append(matches, logical)
			return nil
		},
		doublestar.WithFilesOnly(),
		doublestar.WithFailOnIOErrors(),
	)
	if err != nil {
		if errors.Is(err, doublestar.ErrBadPattern) {
			return nil, fmt.Errorf("invalid glob %q", entry.repositoryRelative)
		}
		return nil, fmt.Errorf("expand glob %q: %w", entry.repositoryRelative, err)
	}
	return matches, nil
}

func applicationBuildEnvironment(app application, source applicationSource) map[string]string {
	project := app.Spec.Project
	if project == "" {
		project = "default"
	}
	return map[string]string{
		"ARGOCD_APP_NAME":                   app.Metadata.Name,
		"ARGOCD_APP_NAMESPACE":              app.Metadata.Namespace,
		"ARGOCD_APP_PROJECT_NAME":           project,
		"ARGOCD_APP_SOURCE_PATH":            source.Path,
		"ARGOCD_APP_SOURCE_REPO_URL":        source.RepoURL,
		"ARGOCD_APP_SOURCE_TARGET_REVISION": source.TargetRevision,
	}
}

func expandBuildEnvironment(
	value string,
	environment map[string]string,
) (expanded string, leadingDollarLiteral bool, err error) {
	return expandEnvironmentVariables(value, environment, func(name string, braced bool) (string, error) {
		if isUnexpandableBuildEnvironmentVariable(name) {
			return "", fmt.Errorf(
				"build environment variable %s cannot be determined statically",
				name,
			)
		}
		if braced {
			return "${" + name + "}", nil
		}
		return "$" + name, nil
	})
}

// argoExpansion renders value the way Argo CD's Envsubst does, which stays exact for input
// the converter's own tokenizer reads differently, such as the shell variables $1 and $@.
func argoExpansion(value string, environment map[string]string) string {
	return os.Expand(value, func(name string) string {
		if name == "$" {
			return "$"
		}
		return environment[name]
	})
}

// divergentExpansion reports a value Argo CD and the converter render differently. A value
// expansion rejects is skipped because it fails the conversion instead.
func divergentExpansion(
	value string,
	environment map[string]string,
) (argo string, converted string, differs bool) {
	converted, _, err := expandBuildEnvironment(value, environment)
	if err != nil {
		return "", "", false
	}
	argo = argoExpansion(value, environment)
	return argo, converted, argo != converted
}

// A statically expandable variable is always present in the environment, so a variable
// that reaches this check is either dynamic or an unknown Argo CD variable.
func isUnexpandableBuildEnvironmentVariable(name string) bool {
	if strings.HasPrefix(name, "ARGOCD_") {
		return true
	}
	_, known := applicationmapping.LookupBuildEnvironmentVariable(name)
	return known
}

func expandEnvironmentVariables(
	value string,
	environment map[string]string,
	onUnresolved func(name string, braced bool) (string, error),
) (expanded string, leadingDollarLiteral bool, err error) {
	var result strings.Builder
	for i := 0; i < len(value); {
		if value[i] != '$' {
			result.WriteByte(value[i])
			i++
			continue
		}
		if i+1 < len(value) && value[i+1] == '$' {
			result.WriteByte('$')
			if i == 0 {
				leadingDollarLiteral = true
			}
			i += 2
			continue
		}

		name, end, braced := buildEnvironmentVariable(value, i)
		if name == "" {
			result.WriteByte('$')
			i++
			continue
		}
		if replacement, exists := environment[name]; exists {
			result.WriteString(replacement)
			i = end
			continue
		}
		replacement, err := onUnresolved(name, braced)
		if err != nil {
			return "", false, err
		}
		result.WriteString(replacement)
		i = end
	}
	return result.String(), leadingDollarLiteral, nil
}

func buildEnvironmentVariable(value string, start int) (name string, end int, braced bool) {
	if start+1 >= len(value) {
		return "", start + 1, false
	}
	if value[start+1] == '{' {
		close := strings.IndexByte(value[start+2:], '}')
		if close < 0 {
			return "", start + 1, false
		}
		close += start + 2
		name = value[start+2 : close]
		if !isEnvironmentVariableName(name) {
			return "", start + 1, false
		}
		return name, close + 1, true
	}
	end = start + 1
	for end < len(value) && isEnvironmentVariableCharacter(value[end], end == start+1) {
		end++
	}
	if end == start+1 {
		return "", start + 1, false
	}
	return value[start+1 : end], end, false
}

func isEnvironmentVariableName(name string) bool {
	if name == "" {
		return false
	}
	for i := range len(name) {
		if !isEnvironmentVariableCharacter(name[i], i == 0) {
			return false
		}
	}
	return true
}

func isEnvironmentVariableCharacter(char byte, first bool) bool {
	if char >= 'A' && char <= 'Z' || char >= 'a' && char <= 'z' || char == '_' {
		return true
	}
	return !first && char >= '0' && char <= '9'
}
