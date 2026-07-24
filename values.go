package main

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"strings"
	"unicode"

	"github.com/bmatcuk/doublestar/v4"
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
			output := joinSourcePath(entry.mapping.root, match)
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
		output:             joinSourcePath(mapping.root, repositoryRelative),
		glob:               isGlob,
	}, nil
}

func resolveSourcePath(raw string, context valueFileContext, option string) (string, error) {
	mapping, repositoryRelative, err := resolveSourceLocation(raw, context, option, true)
	if err != nil {
		return "", err
	}
	return joinSourcePath(mapping.root, repositoryRelative), nil
}

func resolveSourceLocation(
	raw string,
	context valueFileContext,
	option string,
	parseReference bool,
) (mappedSource, string, error) {
	if raw == "" {
		return mappedSource{}, "", errors.New("path must not be empty")
	}
	if strings.Contains(raw, `\`) {
		return mappedSource{}, "", errors.New("backslashes are not supported")
	}
	if strings.IndexFunc(raw, unicode.IsControl) >= 0 {
		return mappedSource{}, "", errors.New("control characters are not supported")
	}
	if strings.Contains(raw, "://") {
		return mappedSource{}, "", fmt.Errorf("remote URLs in %s are not supported", option)
	}
	if option != "valueFiles" && strings.Contains(raw, "$ARGOCD_") {
		return mappedSource{}, "", fmt.Errorf(
			"Argo CD build environment variables in %s are not supported",
			option,
		)
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
	root := entry.mapping.root
	if strings.Contains(root, "{{") || strings.Contains(root, "}}") {
		return nil, errors.New("config root must not contain a template expression for glob expansion")
	}
	info, err := os.Lstat(root)
	if err != nil {
		return nil, fmt.Errorf("config root %q: %w", root, err)
	}
	if info.Mode()&os.ModeSymlink != 0 {
		return nil, fmt.Errorf("config root %q must not be a symlink", root)
	}
	if !info.IsDir() {
		return nil, fmt.Errorf("config root %q must be a directory", root)
	}
	canonicalRoot, err := filepath.EvalSymlinks(root)
	if err != nil {
		return nil, fmt.Errorf("evaluate config root %q: %w", root, err)
	}
	canonicalRoot, err = filepath.Abs(canonicalRoot)
	if err != nil {
		return nil, fmt.Errorf("make config root %q absolute: %w", root, err)
	}

	var matches []string
	err = doublestar.GlobWalk(
		os.DirFS(root),
		entry.repositoryRelative,
		func(logical string, _ fs.DirEntry) error {
			candidate := filepath.Join(root, filepath.FromSlash(logical))
			canonical, err := filepath.EvalSymlinks(candidate)
			if err != nil {
				return fmt.Errorf("evaluate matched path %q: %w", logical, err)
			}
			canonical, err = filepath.Abs(canonical)
			if err != nil {
				return fmt.Errorf("make matched path %q absolute: %w", logical, err)
			}
			relative, err := filepath.Rel(canonicalRoot, canonical)
			if err != nil {
				return fmt.Errorf("check matched path %q: %w", logical, err)
			}
			if relative == ".." || strings.HasPrefix(relative, ".."+string(filepath.Separator)) {
				return fmt.Errorf("matched path %q resolves outside config root", logical)
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
		if strings.HasPrefix(name, "ARGOCD_") ||
			name == "KUBE_VERSION" ||
			name == "KUBE_API_VERSIONS" {
			return "", false, fmt.Errorf(
				"build environment variable %s cannot be determined statically",
				name,
			)
		}
		if braced {
			result.WriteString("${" + name + "}")
		} else {
			result.WriteByte('$')
			result.WriteString(name)
		}
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
