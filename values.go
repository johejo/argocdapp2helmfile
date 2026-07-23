package main

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"unicode"

	"github.com/bmatcuk/doublestar/v4"
)

type valueFileContext struct {
	chartLocal *localSource
	chartRoot  string
	refs       map[string]applicationSource
	resolver   *sourceResolver
}

type resolvedValuePattern struct {
	raw     string
	env     string
	root    string
	pattern string
	glob    bool
}

func resolveValueFiles(valueFiles []string, ignoreMissing bool, context valueFileContext) ([]any, error) {
	patterns := make([]resolvedValuePattern, 0, len(valueFiles))
	explicit := make(map[string]struct{})
	for i, valueFile := range valueFiles {
		pattern, err := resolveValuePattern(valueFile, context)
		if err != nil {
			return nil, fmt.Errorf("valueFiles[%d]: %w", i, err)
		}
		patterns = append(patterns, pattern)
		if !pattern.glob {
			explicit[pattern.pattern] = struct{}{}
		}
	}

	result := make([]any, 0, len(valueFiles))
	seen := make(map[string]struct{})
	appendPath := func(pattern resolvedValuePattern, absolute string) error {
		if _, exists := seen[absolute]; exists {
			return nil
		}
		if err := verifyPathWithinRoot(absolute, pattern.root); err != nil {
			return err
		}
		relative, err := filepath.Rel(pattern.root, absolute)
		if err != nil {
			return fmt.Errorf("make value file relative to repository root: %w", err)
		}
		result = append(result, templatePath(
			fmt.Sprintf(`{{ requiredEnv %q }}/%s`, pattern.env, filepath.ToSlash(relative)),
		))
		seen[absolute] = struct{}{}
		return nil
	}

	for _, pattern := range patterns {
		if pattern.glob {
			matches, err := doublestar.FilepathGlob(pattern.pattern)
			if err != nil {
				return nil, fmt.Errorf("expand value file glob %q: %w", pattern.raw, err)
			}
			if len(matches) == 0 {
				if ignoreMissing {
					continue
				}
				return nil, fmt.Errorf("values file glob %q matched no files", pattern.raw)
			}
			for _, match := range matches {
				if _, isExplicit := explicit[match]; isExplicit {
					continue
				}
				if err := appendPath(pattern, match); err != nil {
					return nil, fmt.Errorf("value file glob %q: %w", pattern.raw, err)
				}
			}
			continue
		}
		if _, err := os.Stat(pattern.pattern); err != nil {
			if errors.Is(err, os.ErrNotExist) && ignoreMissing {
				continue
			}
			return nil, fmt.Errorf("inspect value file %q: %w", pattern.raw, err)
		}
		if err := appendPath(pattern, pattern.pattern); err != nil {
			return nil, fmt.Errorf("value file %q: %w", pattern.raw, err)
		}
	}
	return result, nil
}

func resolveValuePattern(raw string, context valueFileContext) (resolvedValuePattern, error) {
	if raw == "" {
		return resolvedValuePattern{}, errors.New("path must not be empty")
	}
	if strings.Contains(raw, `\`) {
		return resolvedValuePattern{}, errors.New("backslashes are not supported")
	}
	if strings.IndexFunc(raw, unicode.IsControl) >= 0 {
		return resolvedValuePattern{}, errors.New("control characters are not supported")
	}
	if strings.Contains(raw, "://") {
		return resolvedValuePattern{}, errors.New("remote values file URLs are not supported")
	}
	if strings.Contains(raw, "$ARGOCD_") {
		return resolvedValuePattern{}, errors.New("Argo CD build environment variables in valueFiles are not supported")
	}

	var local localSource
	var root, base, relative string
	if strings.HasPrefix(raw, "$") {
		separator := strings.IndexByte(raw, '/')
		if separator < 0 {
			return resolvedValuePattern{}, errors.New("a $ref value file must include a path after the reference")
		}
		ref := strings.TrimPrefix(raw[:separator], "$")
		source, exists := context.refs[ref]
		if !exists {
			return resolvedValuePattern{}, fmt.Errorf("reference %q is not defined by spec.sources", ref)
		}
		resolved, err := context.resolver.resolve(source, "values source ref "+fmt.Sprintf("%q", ref))
		if err != nil {
			return resolvedValuePattern{}, err
		}
		local = resolved
		root = local.root
		base = root
		relative = raw[separator+1:]
	} else {
		if context.chartLocal == nil {
			return resolvedValuePattern{}, errors.New("non-$ref valueFiles are not supported for HTTP or OCI Helm charts")
		}
		local = *context.chartLocal
		root = local.root
		base = context.chartRoot
		relative = raw
	}
	if strings.HasPrefix(relative, "/") {
		base = root
		relative = strings.TrimLeft(relative, "/")
	}
	if relative == "" {
		return resolvedValuePattern{}, errors.New("path must not be empty")
	}

	absolute := filepath.Clean(filepath.Join(base, filepath.FromSlash(relative)))
	if err := verifyLexicalPathWithinRoot(absolute, root); err != nil {
		return resolvedValuePattern{}, err
	}
	return resolvedValuePattern{
		raw:     raw,
		env:     local.env,
		root:    root,
		pattern: absolute,
		glob:    strings.ContainsAny(absolute, "*?["),
	}, nil
}

func verifyLexicalPathWithinRoot(candidate, root string) error {
	relative, err := filepath.Rel(root, candidate)
	if err != nil {
		return fmt.Errorf("resolve path relative to repository root: %w", err)
	}
	if relative == ".." || strings.HasPrefix(relative, ".."+string(os.PathSeparator)) {
		return errors.New("path resolves outside repository root")
	}
	return nil
}

func verifyPathWithinRoot(candidate, root string) error {
	canonicalRoot, err := filepath.EvalSymlinks(root)
	if err != nil {
		return fmt.Errorf("resolve repository root: %w", err)
	}
	canonicalCandidate, err := filepath.EvalSymlinks(candidate)
	if err != nil {
		return fmt.Errorf("resolve symlinks for %q: %w", candidate, err)
	}
	return verifyLexicalPathWithinRoot(canonicalCandidate, canonicalRoot)
}
