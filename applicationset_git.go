package main

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"maps"
	"os"
	"path"
	"path/filepath"
	"slices"
	"strconv"
	"strings"

	"github.com/bmatcuk/doublestar/v4"
	"github.com/goccy/go-yaml"
)

type gitPathPattern struct {
	pattern string
	exclude bool
	field   string
}

type gitGeneratorOptions struct {
	repoURL         string
	revision        string
	directories     []gitPathPattern
	files           []gitPathPattern
	hasDirectories  bool
	hasFiles        bool
	pathParamPrefix string
	values          []generatorValue
	template        yaml.MapSlice
}

type generatorValue struct {
	key   string
	value string
}

func generateGitParams(request generatorRequest) (generatorResult, error) {
	var result generatorResult
	options, err := parseGitGeneratorOptions(request.items, request.field)
	if err != nil {
		return result, err
	}
	root, err := resolveGitGeneratorRoot(
		request.config.sources(),
		options.repoURL,
		options.revision,
		request.field,
	)
	if err != nil {
		return result, err
	}
	result.template = options.template
	if len(options.directories) != 0 {
		result.params, err = generateGitDirectoryParams(
			root,
			options,
			request.config,
			request.renderer,
			request.parentParams,
			request.field,
		)
	} else {
		result.params, err = generateGitFileParams(
			root,
			options,
			request.config,
			request.renderer,
			request.parentParams,
			request.field,
		)
	}
	return result, err
}

func parseGitGeneratorOptions(items yaml.MapSlice, field string) (gitGeneratorOptions, error) {
	var result gitGeneratorOptions
	for _, item := range items {
		key, ok := item.Key.(string)
		if !ok {
			return result, fmt.Errorf("%s contains a non-string field name", field)
		}
		switch key {
		case "repoURL":
			value, ok := item.Value.(string)
			if !ok {
				return result, fmt.Errorf("%s.repoURL must be a string", field)
			}
			result.repoURL = value
		case "revision":
			value, ok := item.Value.(string)
			if !ok {
				return result, fmt.Errorf("%s.revision must be a string", field)
			}
			result.revision = value
		case "directories":
			result.hasDirectories = true
			patterns, err := parseGitPathPatterns(item.Value, field+".directories")
			if err != nil {
				return result, err
			}
			result.directories = patterns
		case "files":
			result.hasFiles = true
			patterns, err := parseGitPathPatterns(item.Value, field+".files")
			if err != nil {
				return result, err
			}
			result.files = patterns
		case "pathParamPrefix":
			value, ok := item.Value.(string)
			if !ok {
				return result, fmt.Errorf("%s.pathParamPrefix must be a string", field)
			}
			result.pathParamPrefix = value
		case "values":
			values, err := parseGeneratorValues(item.Value, field+".values")
			if err != nil {
				return result, err
			}
			result.values = values
		case "template":
			value, err := readMappingYAMLOption(item.Value, field+".template")
			if err != nil {
				return result, err
			}
			result.template = value
		case "requeueAfterSeconds":
			// A one-shot CLI has nothing to requeue.
		default:
			return result, fmt.Errorf("%s.%s is not supported", field, key)
		}
	}
	if strings.TrimSpace(result.repoURL) == "" {
		return result, fmt.Errorf("%s.repoURL is required", field)
	}
	if strings.TrimSpace(result.revision) == "" {
		return result, fmt.Errorf("%s.revision is required", field)
	}
	if result.hasDirectories == result.hasFiles ||
		result.hasDirectories && len(result.directories) == 0 ||
		result.hasFiles && len(result.files) == 0 {
		return result, fmt.Errorf("%s must set exactly one non-empty directories or files", field)
	}
	return result, nil
}

func parseGitPathPatterns(value any, field string) ([]gitPathPattern, error) {
	items, ok := value.([]any)
	if !ok {
		return nil, fmt.Errorf("%s must be a sequence", field)
	}
	result := make([]gitPathPattern, 0, len(items))
	for i, raw := range items {
		itemField := fmt.Sprintf("%s[%d]", field, i)
		options, ok := raw.(yaml.MapSlice)
		if !ok {
			return nil, fmt.Errorf("%s must be a mapping", itemField)
		}
		var pattern gitPathPattern
		pattern.field = itemField + ".path"
		for _, option := range options {
			key, ok := option.Key.(string)
			if !ok {
				return nil, fmt.Errorf("%s contains a non-string field name", itemField)
			}
			switch key {
			case "path":
				value, ok := option.Value.(string)
				if !ok {
					return nil, fmt.Errorf("%s.path must be a string", itemField)
				}
				pattern.pattern = filepath.ToSlash(value)
			case "exclude":
				value, ok := option.Value.(bool)
				if !ok {
					return nil, fmt.Errorf("%s.exclude must be a boolean", itemField)
				}
				pattern.exclude = value
			default:
				return nil, fmt.Errorf("%s.%s is not supported", itemField, key)
			}
		}
		if strings.TrimSpace(pattern.pattern) == "" {
			return nil, fmt.Errorf("%s.path is required", itemField)
		}
		result = append(result, pattern)
	}
	return result, nil
}

func parseGeneratorValues(value any, field string) ([]generatorValue, error) {
	items, ok := value.(yaml.MapSlice)
	if !ok {
		return nil, fmt.Errorf("%s must be a mapping", field)
	}
	result := make([]generatorValue, 0, len(items))
	for _, item := range items {
		key, ok := item.Key.(string)
		if !ok || strings.TrimSpace(key) == "" {
			return nil, fmt.Errorf("%s keys must be non-empty strings", field)
		}
		value, ok := item.Value.(string)
		if !ok {
			return nil, fmt.Errorf("%s.%s must be a string", field, key)
		}
		result = append(result, generatorValue{key: key, value: value})
	}
	return result, nil
}

func resolveGitGeneratorRoot(
	resolver *sourceResolver,
	repoURL string,
	revision string,
	field string,
) (string, error) {
	mapping, err := resolver.resolve(
		applicationSource{RepoURL: repoURL, TargetRevision: revision},
		field,
	)
	if err != nil {
		return "", err
	}
	root, err := mapping.directory()
	if err != nil {
		return "", fmt.Errorf("%s %w", field, err)
	}
	return root, nil
}

func generateGitDirectoryParams(
	root string,
	options gitGeneratorOptions,
	config *conversionConfig,
	renderer applicationSetRenderer,
	parentParams map[string]any,
	field string,
) ([]generatedGeneratorParams, error) {
	for _, pattern := range options.directories {
		if _, err := path.Match(pattern.pattern, "."); err != nil {
			return nil, fmt.Errorf("%s: invalid glob %q: %w", pattern.field, pattern.pattern, err)
		}
	}
	candidates, err := config.walkGitCandidates(root, true, func() ([]string, error) {
		return walkGitEntries(
			root,
			func(relative string, entry fs.DirEntry) bool {
				return relative != "." && entry.IsDir() &&
					strings.HasPrefix(entry.Name(), ".")
			},
			func(relative string, entry fs.DirEntry) bool {
				return relative != "." && entry.IsDir()
			},
		)
	})
	if err != nil {
		return nil, fmt.Errorf("%s: walk config localRoot: %w", field, err)
	}
	matches := filterGitCandidates(candidates, options.directories, path.Match)
	result := make([]generatedGeneratorParams, 0, len(matches))
	for _, relative := range matches {
		params := make(map[string]any)
		pathObject := gitDirectoryPathObject(relative)
		setGitPathParams(params, options.pathParamPrefix, pathObject, renderer.GoTemplate())
		if err := renderGeneratorValues(params, parentParams, options.values, renderer); err != nil {
			return nil, fmt.Errorf("%s.directories[%q]: values: %w", field, relative, err)
		}
		result = append(result, generatedGeneratorParams{
			params: params,
			path:   fmt.Sprintf("%s.directories[%q]", field, relative),
		})
	}
	return result, nil
}

// walkGitEntries lists root-relative slash paths. Skipped entries and symlinks
// prune the subtree below them.
func walkGitEntries(
	root string,
	skip func(relative string, entry fs.DirEntry) bool,
	include func(relative string, entry fs.DirEntry) bool,
) ([]string, error) {
	var candidates []string
	err := filepath.WalkDir(root, func(current string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		relative, err := filepath.Rel(root, current)
		if err != nil {
			return err
		}
		relative = filepath.ToSlash(relative)
		if skip(relative, entry) || entry.Type()&os.ModeSymlink != 0 {
			if entry.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		if include(relative, entry) {
			candidates = append(candidates, relative)
		}
		return nil
	})
	return candidates, err
}

type gitMatcher func(pattern, name string) (bool, error)

func filterGitCandidates(
	candidates []string,
	patterns []gitPathPattern,
	matcher gitMatcher,
) []string {
	selected := make(map[string]struct{})
	excluded := make(map[string]struct{})
	for _, candidate := range candidates {
		for _, pattern := range patterns {
			matched, _ := matcher(pattern.pattern, candidate)
			if !matched {
				continue
			}
			if pattern.exclude {
				excluded[candidate] = struct{}{}
			} else {
				selected[candidate] = struct{}{}
			}
		}
	}
	result := make([]string, 0, len(selected))
	for _, candidate := range slices.Sorted(maps.Keys(selected)) {
		if _, skip := excluded[candidate]; !skip {
			result = append(result, candidate)
		}
	}
	return result
}

func generateGitFileParams(
	root string,
	options gitGeneratorOptions,
	config *conversionConfig,
	renderer applicationSetRenderer,
	parentParams map[string]any,
	field string,
) ([]generatedGeneratorParams, error) {
	for _, pattern := range options.files {
		if !doublestar.ValidatePattern(pattern.pattern) {
			return nil, fmt.Errorf("%s: invalid glob %q", pattern.field, pattern.pattern)
		}
	}
	candidates, err := config.walkGitCandidates(root, false, func() ([]string, error) {
		return walkGitEntries(
			root,
			func(relative string, entry fs.DirEntry) bool {
				return relative != "." && entry.Name() == ".git"
			},
			func(relative string, entry fs.DirEntry) bool {
				return relative != "." && entry.Type().IsRegular()
			},
		)
	})
	if err != nil {
		return nil, fmt.Errorf("%s: walk config localRoot: %w", field, err)
	}
	matches := filterGitCandidates(candidates, options.files, doublestar.Match)
	var result []generatedGeneratorParams
	for _, relative := range matches {
		fileParams, err := decodeGitParameterFile(
			filepath.Join(root, filepath.FromSlash(relative)),
			renderer.GoTemplate(),
		)
		if err != nil {
			var indexed *gitFileEntryError
			if errors.As(err, &indexed) {
				return nil, fmt.Errorf(
					"%s.files[%q][%d]: %w",
					field,
					relative,
					indexed.index,
					indexed.err,
				)
			}
			return nil, fmt.Errorf("%s.files[%q]: %w", field, relative, err)
		}
		for _, fileParam := range fileParams {
			params := fileParam.params
			pathObject := gitFilePathObject(relative)
			setGitPathParams(params, options.pathParamPrefix, pathObject, renderer.GoTemplate())
			origin := fmt.Sprintf("%s.files[%q]", field, relative)
			if fileParam.index != nil {
				origin += fmt.Sprintf("[%d]", *fileParam.index)
			}
			if err := renderGeneratorValues(params, parentParams, options.values, renderer); err != nil {
				return nil, fmt.Errorf("%s: values: %w", origin, err)
			}
			result = append(result, generatedGeneratorParams{
				params: params,
				path:   origin,
			})
		}
	}
	return result, nil
}

type decodedGitFileParams struct {
	params map[string]any
	index  *int
}

type gitFileEntryError struct {
	index int
	err   error
}

func (err *gitFileEntryError) Error() string {
	return err.err.Error()
}

func (err *gitFileEntryError) Unwrap() error {
	return err.err
}

func decodeGitParameterFile(
	filename string,
	goTemplate bool,
) ([]decodedGitFileParams, error) {
	data, err := os.ReadFile(filename)
	if err != nil {
		return nil, fmt.Errorf("read file: %w", err)
	}
	if len(bytes.TrimSpace(data)) == 0 {
		return []decodedGitFileParams{{params: map[string]any{}}}, nil
	}
	decoder := yaml.NewDecoder(bytes.NewReader(data), yaml.UseOrderedMap())
	var raw any
	if err := decoder.Decode(&raw); err != nil {
		return nil, fmt.Errorf("decode file: %w", err)
	}
	var extra any
	if err := decoder.Decode(&extra); !errors.Is(err, io.EOF) {
		if err == nil {
			return nil, errors.New("file must contain exactly one YAML or JSON document")
		}
		return nil, fmt.Errorf("decode additional document: %w", err)
	}
	switch value := raw.(type) {
	case yaml.MapSlice:
		params, err := normalizeStringMap(value)
		if err != nil {
			return nil, err
		}
		if !goTemplate {
			params = flattenLegacyGitFileParams(params)
		}
		return []decodedGitFileParams{{params: params}}, nil
	case []any:
		result := make([]decodedGitFileParams, 0, len(value))
		for i, item := range value {
			params, err := normalizeStringMap(item)
			if err != nil {
				return nil, &gitFileEntryError{
					index: i,
					err:   fmt.Errorf("must be a mapping: %w", err),
				}
			}
			if !goTemplate {
				params = flattenLegacyGitFileParams(params)
			}
			index := i
			result = append(result, decodedGitFileParams{params: params, index: &index})
		}
		return result, nil
	default:
		return nil, errors.New("root must be a mapping or a sequence of mappings")
	}
}

func gitDirectoryPathObject(relative string) map[string]any {
	return map[string]any{
		"path":               relative,
		"basename":           path.Base(relative),
		"basenameNormalized": normalizeName(path.Base(relative)),
		"segments":           strings.Split(relative, "/"),
	}
}

func gitFilePathObject(relative string) map[string]any {
	directory := path.Dir(relative)
	filename := path.Base(relative)
	result := gitDirectoryPathObject(directory)
	result["filename"] = filename
	result["filenameNormalized"] = normalizeName(filename)
	return result
}

func setGitPathParams(
	params map[string]any,
	prefix string,
	pathObject map[string]any,
	goTemplate bool,
) {
	if !goTemplate {
		setLegacyGitPathParams(params, prefix, pathObject)
		return
	}
	if prefix == "" {
		params["path"] = pathObject
		return
	}
	params[prefix] = map[string]any{"path": pathObject}
}

// setLegacyGitPathParams matches Argo CD, which indexes segments as path[0] and
// drops the segments key. flattenLegacyGitFileParams uses the generic
// flattenParameters instead, producing path.segments.0; the two are not
// interchangeable.
func setLegacyGitPathParams(
	params map[string]any,
	prefix string,
	pathObject map[string]any,
) {
	base := ""
	if prefix != "" {
		base = prefix + "."
	}
	params[base+"path"] = pathObject["path"]
	params[base+"path.basename"] = pathObject["basename"]
	params[base+"path.basenameNormalized"] = pathObject["basenameNormalized"]
	if filename, exists := pathObject["filename"]; exists {
		params[base+"path.filename"] = filename
		params[base+"path.filenameNormalized"] = pathObject["filenameNormalized"]
	}
	segments, _ := pathObject["segments"].([]string)
	for i, segment := range segments {
		params[base+"path["+strconv.Itoa(i)+"]"] = segment
	}
}

func flattenLegacyGitFileParams(params map[string]any) map[string]any {
	flat := make(map[string]string, len(params))
	// Maps cannot trigger flattenParameters errors.
	_ = flattenParameters(flat, "", params)
	return stringMapToAny(flat)
}

func renderGeneratorValues(
	params map[string]any,
	parentParams map[string]any,
	values []generatorValue,
	renderer applicationSetRenderer,
) error {
	if len(values) == 0 {
		return nil
	}
	rendered := make(map[string]any, len(values))
	context := mergeMatrixParams(parentParams, params)
	for _, item := range values {
		key, err := renderer.Render(item.key, context)
		if err != nil {
			return fmt.Errorf("%s: render key: %w", item.key, err)
		}
		if _, exists := rendered[key]; exists {
			return fmt.Errorf("templating produced duplicate mapping key %q", key)
		}
		result, err := renderer.Render(item.value, context)
		if err != nil {
			return fmt.Errorf("%s: %w", item.key, err)
		}
		rendered[key] = result
	}
	if len(rendered) != 0 {
		if renderer.GoTemplate() {
			params["values"] = rendered
		} else {
			for key, value := range rendered {
				params["values."+key] = value
			}
		}
	}
	return nil
}
