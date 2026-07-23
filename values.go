package main

import (
	"errors"
	"fmt"
	"path"
	"strings"
	"unicode"
)

type valueFileContext struct {
	chartMapping *mappedSource
	chartRoot    string
	refs         map[string]applicationSource
	resolver     *sourceResolver
}

func resolveValueFiles(valueFiles []string, context valueFileContext) ([]any, error) {
	result := make([]any, 0, len(valueFiles))
	for i, valueFile := range valueFiles {
		resolved, err := resolveValuePath(valueFile, context)
		if err != nil {
			return nil, fmt.Errorf("valueFiles[%d]: %w", i, err)
		}
		result = append(result, templatePath(resolved))
	}
	return result, nil
}

func resolveValuePath(raw string, context valueFileContext) (string, error) {
	if raw == "" {
		return "", errors.New("path must not be empty")
	}
	if strings.Contains(raw, `\`) {
		return "", errors.New("backslashes are not supported")
	}
	if strings.IndexFunc(raw, unicode.IsControl) >= 0 {
		return "", errors.New("control characters are not supported")
	}
	if strings.Contains(raw, "://") {
		return "", errors.New("remote values file URLs are not supported")
	}
	if strings.Contains(raw, "$ARGOCD_") {
		return "", errors.New("Argo CD build environment variables in valueFiles are not supported")
	}

	var mapping mappedSource
	var base, relative string
	if strings.HasPrefix(raw, "$") {
		separator := strings.IndexByte(raw, '/')
		if separator < 0 {
			return "", errors.New("a $ref value file must include a path after the reference")
		}
		ref := strings.TrimPrefix(raw[:separator], "$")
		source, exists := context.refs[ref]
		if !exists {
			return "", fmt.Errorf("reference %q is not defined by spec.sources", ref)
		}
		resolved, err := context.resolver.resolve(source, "values source ref "+fmt.Sprintf("%q", ref))
		if err != nil {
			return "", err
		}
		mapping = resolved
		relative = raw[separator+1:]
	} else {
		if context.chartMapping == nil {
			return "", errors.New("non-$ref valueFiles are not supported for HTTP or OCI Helm charts")
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
		return "", errors.New("path must not be empty")
	}

	repositoryRelative := path.Clean(path.Join(base, relative))
	if repositoryRelative == ".." || strings.HasPrefix(repositoryRelative, "../") {
		return "", errors.New("path resolves outside repository root")
	}
	return joinSourcePath(mapping.root, repositoryRelative), nil
}
