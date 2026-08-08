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
	"time"

	"github.com/goccy/go-yaml"
	"github.com/itchyny/gojq"
	"github.com/johejo/argocdapp2helmfile/internal/conversionconfig"
)

const (
	configAPIVersion = conversionconfig.APIVersion
	configKind       = conversionconfig.Kind
)

type configResource = conversionconfig.Resource
type sourceConfigEntry = conversionconfig.Source
type destinationConfigEntry = conversionconfig.Destination
type clusterConfigEntry = conversionconfig.Cluster
type releaseLabelConfig = conversionconfig.ReleaseLabelRule

type sourceKey struct {
	repoURL        string
	targetRevision string
}

// mappedSource is a configured source's local root, which is either a real
// directory or a helmfile template expression. Use join for output and
// directory for anything that touches the filesystem.
type mappedSource struct {
	localRoot string
}

func (source mappedSource) join(relative string) string {
	return joinSourcePath(source.localRoot, relative)
}

func (source mappedSource) directory() (string, error) {
	if err := validateLocalRootDirectory(source.localRoot); err != nil {
		return "", err
	}
	return source.localRoot, nil
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

	resolver, err := parseConfigSources(resource.Sources)
	if err != nil {
		return nil, err
	}
	destinations, err := parseConfigDestinations(resource.Destinations, resource.Clusters)
	if err != nil {
		return nil, err
	}
	projector, err := parseConfigReleaseLabels(resource.ReleaseLabels)
	if err != nil {
		return nil, err
	}

	return &conversionConfig{
		sourceResolver:      resolver,
		destinationResolver: destinations,
		clusters:            resource.Clusters,
		labelProjector:      projector,
	}, nil
}

func parseConfigSources(entries []sourceConfigEntry) (*sourceResolver, error) {
	if err := conversionconfig.ValidateSources(entries); err != nil {
		return nil, err
	}
	resolver := &sourceResolver{
		entries: make(map[sourceKey]sourceConfigEntry, len(entries)),
	}
	for _, entry := range entries {
		key := sourceKey{repoURL: entry.RepoURL, targetRevision: entry.TargetRevision}
		resolver.entries[key] = entry
	}
	return resolver, nil
}

// parseConfigDestinations indexes destinations and clusters together, because a
// cluster registers both of the keys a destination may select it by and the two
// lists must not collide.
func parseConfigDestinations(
	entries []destinationConfigEntry,
	clusters []clusterConfigEntry,
) (*destinationResolver, error) {
	if err := conversionconfig.ValidateDestinations(entries, clusters); err != nil {
		return nil, err
	}
	resolver := &destinationResolver{
		entries: make(
			map[destinationKey]destinationConfigEntry,
			len(entries)+len(clusters)*2,
		),
	}
	for _, entry := range entries {
		hasName := strings.TrimSpace(entry.Name) != ""
		key := destinationKey{kind: "name", value: entry.Name}
		if !hasName {
			key = destinationKey{kind: "server", value: entry.Server}
		}
		resolver.entries[key] = entry
	}
	for _, cluster := range clusters {
		entry := destinationConfigEntry{
			Name:        cluster.Name,
			Server:      cluster.Server,
			KubeContext: cluster.KubeContext,
		}
		for _, key := range []destinationKey{
			{kind: "name", value: cluster.Name},
			{kind: "server", value: cluster.Server},
		} {
			resolver.entries[key] = entry
		}
	}
	return resolver, nil
}

func parseConfigReleaseLabels(entries []releaseLabelConfig) (*releaseLabelProjector, error) {
	if err := conversionconfig.ValidateReleaseLabels(entries); err != nil {
		return nil, err
	}
	projector := &releaseLabelProjector{
		rules: make([]releaseLabelRule, 0, len(entries)),
	}
	for i, entry := range entries {
		field := fmt.Sprintf("config releaseLabels[%d]", i)
		query, err := gojq.Parse(entry.Query)
		if err != nil {
			return nil, fmt.Errorf("%s.query: %w", field, err)
		}
		code, err := gojq.Compile(query)
		if err != nil {
			return nil, fmt.Errorf("%s.query: %w", field, err)
		}
		projector.rules = append(projector.rules, releaseLabelRule{name: entry.Name, code: code})
	}
	return projector, nil
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
	// Absolute first: EvalSymlinks only resolves the components it is given.
	canonical, err := filepath.Abs(root)
	if err != nil {
		return "", fmt.Errorf("make config localRoot %q absolute: %w", root, err)
	}
	canonical, err = filepath.EvalSymlinks(canonical)
	if err != nil {
		return "", fmt.Errorf("evaluate config localRoot %q: %w", root, err)
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
	if err := validateDestinationSelector(destination, field); err != nil {
		return "", err
	}
	hasName := strings.TrimSpace(destination.Name) != ""
	hasServer := strings.TrimSpace(destination.Server) != ""
	if !hasName && !hasServer {
		return "", nil
	}
	if resolver == nil {
		return "", fmt.Errorf(
			"%s requires --config; use --kube-context-mode omit if the kube context is not needed",
			field,
		)
	}
	key := destinationKey{kind: "name", value: destination.Name}
	if hasServer {
		key = destinationKey{kind: "server", value: destination.Server}
	}
	entry, exists := resolver.entries[key]
	if !exists {
		return "", fmt.Errorf(
			"%s has no config destination entry for %s %q; use --kube-context-mode omit if the kube context is not needed",
			field,
			key.kind,
			key.value,
		)
	}
	return entry.KubeContext, nil
}

func validateDestinationSelector(destination applicationDestination, field string) error {
	hasName := strings.TrimSpace(destination.Name) != ""
	hasServer := strings.TrimSpace(destination.Server) != ""
	if hasName && hasServer {
		return fmt.Errorf("%s.name and %s.server cannot both be set", field, field)
	}
	return nil
}

func normalizeJQValue(value any) (any, error) {
	switch typed := value.(type) {
	case nil, bool, string, int, float64, *big.Int:
		return value, nil
	case time.Time:
		return typed.Format(time.RFC3339Nano), nil
	case map[string]any:
		result := make(map[string]any, len(typed))
		for key, item := range typed {
			normalized, err := normalizeJQValue(item)
			if err != nil {
				return nil, err
			}
			result[key] = normalized
		}
		return result, nil
	case []any:
		result := make([]any, len(typed))
		for i, item := range typed {
			normalized, err := normalizeJQValue(item)
			if err != nil {
				return nil, err
			}
			result[i] = normalized
		}
		return result, nil
	case int64:
		return normalizeJQSignedInteger(typed), nil
	case uint64:
		return normalizeJQUnsignedInteger(typed), nil
	default:
		return nil, fmt.Errorf("unsupported value type %T", value)
	}
}

func normalizeJQSignedInteger(value int64) any {
	maxInt := uint64(^uint(0) >> 1)
	if value >= -int64(maxInt)-1 && value <= int64(maxInt) {
		return int(value)
	}
	return big.NewInt(value)
}

func normalizeJQUnsignedInteger(value uint64) any {
	if value <= uint64(^uint(0)>>1) {
		return int(value)
	}
	return new(big.Int).SetUint64(value)
}

func (projector *releaseLabelProjector) project(input any) (yaml.MapSlice, error) {
	if projector == nil || len(projector.rules) == 0 {
		return nil, nil
	}
	input, err := normalizeJQValue(input)
	if err != nil {
		return nil, fmt.Errorf("release label query input: %w", err)
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
