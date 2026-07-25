package main

import (
	"bytes"
	"errors"
	"fmt"
	"math/big"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/goccy/go-yaml"
	"github.com/goccy/go-yaml/ast"
	"github.com/itchyny/gojq"
)

const (
	configAPIVersion = "argocdapp2helmfile/v1alpha1"
	configKind       = "Config"
)

type configResource struct {
	APIVersion    string                   `yaml:"apiVersion"`
	Kind          string                   `yaml:"kind"`
	Sources       []sourceConfigEntry      `yaml:"sources"`
	Destinations  []destinationConfigEntry `yaml:"destinations"`
	Clusters      []clusterConfigEntry     `yaml:"clusters"`
	ReleaseLabels []releaseLabelConfig     `yaml:"releaseLabels"`
}

type sourceConfigEntry struct {
	RepoURL        string `yaml:"repoURL"`
	TargetRevision string `yaml:"targetRevision"`
	LocalRoot      string `yaml:"localRoot"`
}

type destinationConfigEntry struct {
	Name        string `yaml:"name"`
	Server      string `yaml:"server"`
	KubeContext string `yaml:"kubeContext"`
}

type clusterConfigEntry struct {
	Name        string          `yaml:"name"`
	Server      string          `yaml:"server"`
	KubeContext string          `yaml:"kubeContext"`
	Project     string          `yaml:"project"`
	Labels      strictStringMap `yaml:"labels"`
	Annotations strictStringMap `yaml:"annotations"`
}

type strictStringMap map[string]string

func (mapping *strictStringMap) UnmarshalYAML(node ast.Node) error {
	if node.Type() == ast.NullType {
		*mapping = nil
		return nil
	}
	root, ok := node.(*ast.MappingNode)
	if !ok {
		return errors.New("must be a mapping")
	}
	result := make(strictStringMap, len(root.Values))
	for _, item := range root.Values {
		key, keyOK := item.Key.(*ast.StringNode)
		value, valueOK := item.Value.(*ast.StringNode)
		if !keyOK || !valueOK {
			return errors.New("keys and values must be strings")
		}
		result[key.Value] = value.Value
	}
	*mapping = result
	return nil
}

type releaseLabelConfig struct {
	Name  string `yaml:"name"`
	Query string `yaml:"query"`
}

type sourceKey struct {
	repoURL        string
	targetRevision string
}

type mappedSource struct {
	localRoot string
}

type sourceResolver struct {
	entries map[sourceKey]sourceConfigEntry
}

type destinationKey struct {
	kind  string
	value string
}

type destinationResolver struct {
	entries map[destinationKey]destinationConfigEntry
}

type releaseLabelRule struct {
	name string
	code *gojq.Code
}

type releaseLabelProjector struct {
	rules []releaseLabelRule
}

type conversionConfig struct {
	sourceResolver      *sourceResolver
	destinationResolver *destinationResolver
	clusters            []clusterConfigEntry
	labelProjector      *releaseLabelProjector
	gitWalks            map[gitWalkKey][]string
}

type gitWalkKey struct {
	root        string
	directories bool
}

func (config *conversionConfig) sources() *sourceResolver {
	if config == nil {
		return nil
	}
	return config.sourceResolver
}

func (config *conversionConfig) walkGitCandidates(
	root string,
	directories bool,
	walk func() ([]string, error),
) ([]string, error) {
	if config == nil {
		return walk()
	}
	key := gitWalkKey{root: root, directories: directories}
	if cached, exists := config.gitWalks[key]; exists {
		return cached, nil
	}
	candidates, err := walk()
	if err != nil {
		return nil, err
	}
	if config.gitWalks == nil {
		config.gitWalks = make(map[gitWalkKey][]string)
	}
	config.gitWalks[key] = candidates
	return candidates, nil
}

func parseConfig(input []byte) (*conversionConfig, error) {
	if _, err := singleDocumentBody(input, "config"); err != nil {
		return nil, err
	}
	decoder := yaml.NewDecoder(bytes.NewReader(input), yaml.DisallowUnknownField())
	var resource configResource
	if err := decoder.Decode(&resource); err != nil {
		return nil, fmt.Errorf("decode config: %w", err)
	}
	if resource.APIVersion != configAPIVersion {
		return nil, fmt.Errorf("config apiVersion must be %q", configAPIVersion)
	}
	if resource.Kind != configKind {
		return nil, fmt.Errorf("config kind must be %q", configKind)
	}

	resolver := &sourceResolver{
		entries: make(map[sourceKey]sourceConfigEntry, len(resource.Sources)),
	}
	for i, entry := range resource.Sources {
		field := fmt.Sprintf("config sources[%d]", i)
		if strings.TrimSpace(entry.RepoURL) == "" {
			return nil, fmt.Errorf("%s.repoURL is required", field)
		}
		if strings.TrimSpace(entry.TargetRevision) == "" {
			return nil, fmt.Errorf("%s.targetRevision is required", field)
		}
		if strings.TrimSpace(entry.LocalRoot) == "" {
			return nil, fmt.Errorf("%s.localRoot is required", field)
		}
		key := sourceKey{repoURL: entry.RepoURL, targetRevision: entry.TargetRevision}
		if _, exists := resolver.entries[key]; exists {
			return nil, fmt.Errorf("%s duplicates repoURL %q at targetRevision %q", field, entry.RepoURL, entry.TargetRevision)
		}
		resolver.entries[key] = entry
	}

	destinationResolver := &destinationResolver{
		entries: make(
			map[destinationKey]destinationConfigEntry,
			len(resource.Destinations)+len(resource.Clusters)*2,
		),
	}
	for i, entry := range resource.Destinations {
		field := fmt.Sprintf("config destinations[%d]", i)
		hasName := strings.TrimSpace(entry.Name) != ""
		hasServer := strings.TrimSpace(entry.Server) != ""
		if hasName == hasServer {
			return nil, fmt.Errorf("%s must set exactly one of name or server", field)
		}
		if strings.TrimSpace(entry.KubeContext) == "" {
			return nil, fmt.Errorf("%s.kubeContext is required", field)
		}
		key := destinationKey{kind: "name", value: entry.Name}
		if hasServer {
			key = destinationKey{kind: "server", value: entry.Server}
		}
		if _, exists := destinationResolver.entries[key]; exists {
			return nil, fmt.Errorf("%s duplicates %s %q", field, key.kind, key.value)
		}
		destinationResolver.entries[key] = entry
	}
	for i, cluster := range resource.Clusters {
		field := fmt.Sprintf("config clusters[%d]", i)
		if strings.TrimSpace(cluster.Name) == "" {
			return nil, fmt.Errorf("%s.name is required", field)
		}
		if strings.TrimSpace(cluster.Server) == "" {
			return nil, fmt.Errorf("%s.server is required", field)
		}
		if strings.TrimSpace(cluster.KubeContext) == "" {
			return nil, fmt.Errorf("%s.kubeContext is required", field)
		}
		entry := destinationConfigEntry{
			Name:        cluster.Name,
			Server:      cluster.Server,
			KubeContext: cluster.KubeContext,
		}
		for _, key := range []destinationKey{
			{kind: "name", value: cluster.Name},
			{kind: "server", value: cluster.Server},
		} {
			if _, exists := destinationResolver.entries[key]; exists {
				return nil, fmt.Errorf("%s duplicates %s %q", field, key.kind, key.value)
			}
			destinationResolver.entries[key] = entry
		}
	}

	projector := &releaseLabelProjector{
		rules: make([]releaseLabelRule, 0, len(resource.ReleaseLabels)),
	}
	names := make(map[string]struct{}, len(resource.ReleaseLabels))
	for i, entry := range resource.ReleaseLabels {
		field := fmt.Sprintf("config releaseLabels[%d]", i)
		if strings.TrimSpace(entry.Name) == "" {
			return nil, fmt.Errorf("%s.name is required", field)
		}
		if strings.TrimSpace(entry.Query) == "" {
			return nil, fmt.Errorf("%s.query is required", field)
		}
		if _, exists := names[entry.Name]; exists {
			return nil, fmt.Errorf("%s duplicates name %q", field, entry.Name)
		}
		query, err := gojq.Parse(entry.Query)
		if err != nil {
			return nil, fmt.Errorf("%s.query: %w", field, err)
		}
		code, err := gojq.Compile(query)
		if err != nil {
			return nil, fmt.Errorf("%s.query: %w", field, err)
		}
		names[entry.Name] = struct{}{}
		projector.rules = append(projector.rules, releaseLabelRule{name: entry.Name, code: code})
	}

	return &conversionConfig{
		sourceResolver:      resolver,
		destinationResolver: destinationResolver,
		clusters:            resource.Clusters,
		labelProjector:      projector,
	}, nil
}

func validateLocalRootDirectory(root string) error {
	if strings.Contains(root, "{{") || strings.Contains(root, "}}") {
		return errors.New("config localRoot must not contain a template expression")
	}
	info, err := os.Lstat(root)
	if err != nil {
		return fmt.Errorf("config localRoot %q: %w", root, err)
	}
	if info.Mode()&os.ModeSymlink != 0 {
		return fmt.Errorf("config localRoot %q must not be a symlink", root)
	}
	if !info.IsDir() {
		return fmt.Errorf("config localRoot %q must be a directory", root)
	}
	return nil
}

func canonicalLocalRoot(root string) (string, error) {
	if err := validateLocalRootDirectory(root); err != nil {
		return "", err
	}
	canonical, err := filepath.EvalSymlinks(root)
	if err != nil {
		return "", fmt.Errorf("evaluate config localRoot %q: %w", root, err)
	}
	canonical, err = filepath.Abs(canonical)
	if err != nil {
		return "", fmt.Errorf("make config localRoot %q absolute: %w", root, err)
	}
	return canonical, nil
}

// pathWithinRoot keeps symlinks from reaching outside root, which must already
// be canonical.
func pathWithinRoot(root, candidate string) (string, bool, error) {
	canonical, err := filepath.EvalSymlinks(candidate)
	if err != nil {
		return "", false, err
	}
	canonical, err = filepath.Abs(canonical)
	if err != nil {
		return "", false, err
	}
	relative, err := filepath.Rel(root, canonical)
	if err != nil {
		return "", false, err
	}
	if relative == ".." || strings.HasPrefix(relative, ".."+string(filepath.Separator)) {
		return canonical, false, nil
	}
	return canonical, true, nil
}

func (resolver *sourceResolver) resolve(source applicationSource, field string) (mappedSource, error) {
	if resolver == nil {
		return mappedSource{}, fmt.Errorf("%s requires --config", field)
	}
	key := sourceKey{repoURL: source.RepoURL, targetRevision: source.TargetRevision}
	entry, exists := resolver.entries[key]
	if !exists {
		return mappedSource{}, fmt.Errorf(
			"%s has no config source entry for repoURL %q at targetRevision %q",
			field, source.RepoURL, source.TargetRevision,
		)
	}
	return mappedSource{localRoot: entry.LocalRoot}, nil
}

func (resolver *destinationResolver) resolve(destination applicationDestination, field string) (string, error) {
	hasName := strings.TrimSpace(destination.Name) != ""
	hasServer := strings.TrimSpace(destination.Server) != ""
	if hasName && hasServer {
		return "", fmt.Errorf("%s.name and %s.server cannot both be set", field, field)
	}
	if !hasName && !hasServer {
		return "", nil
	}
	if resolver == nil {
		return "", fmt.Errorf("%s requires --config", field)
	}
	key := destinationKey{kind: "name", value: destination.Name}
	if hasServer {
		key = destinationKey{kind: "server", value: destination.Server}
	}
	entry, exists := resolver.entries[key]
	if !exists {
		return "", fmt.Errorf("%s has no config destination entry for %s %q", field, key.kind, key.value)
	}
	return entry.KubeContext, nil
}

func (projector *releaseLabelProjector) project(input any) (yaml.MapSlice, error) {
	if projector == nil || len(projector.rules) == 0 {
		return nil, nil
	}
	labels := make(yaml.MapSlice, 0, len(projector.rules))
	for _, rule := range projector.rules {
		iterator := rule.code.Run(input)
		value, ok := iterator.Next()
		if !ok {
			continue
		}
		if err, ok := value.(error); ok {
			return nil, fmt.Errorf("release label %q query execution: %w", rule.name, err)
		}
		next, ok := iterator.Next()
		if err, isError := next.(error); ok && isError {
			return nil, fmt.Errorf("release label %q query execution: %w", rule.name, err)
		}
		if ok {
			return nil, fmt.Errorf("release label %q query produced multiple results", rule.name)
		}
		if value == nil {
			continue
		}
		stringValue, err := releaseLabelValue(value)
		if err != nil {
			return nil, fmt.Errorf("release label %q query result: %w", rule.name, err)
		}
		labels = append(labels, yaml.MapItem{Key: rule.name, Value: stringValue})
	}
	return labels, nil
}

func releaseLabelValue(value any) (string, error) {
	switch value := value.(type) {
	case string:
		return value, nil
	case bool:
		return strconv.FormatBool(value), nil
	case int:
		return strconv.Itoa(value), nil
	case *big.Int:
		return value.String(), nil
	case float64:
		return strconv.FormatFloat(value, 'g', -1, 64), nil
	case int8, int16, int32, int64, uint, uint8, uint16, uint32, uint64:
		return fmt.Sprint(value), nil
	case float32:
		return strconv.FormatFloat(float64(value), 'g', -1, 32), nil
	default:
		return "", fmt.Errorf("must be a string, boolean, or number, got %T", value)
	}
}

func joinSourcePath(root, relative string) string {
	if relative == "." || relative == "" {
		return root
	}
	if strings.HasSuffix(root, "/") {
		return root + relative
	}
	return root + "/" + relative
}
