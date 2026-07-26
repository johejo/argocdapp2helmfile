package main

import (
	"errors"
	"fmt"
	"strconv"
	"strings"
	"unicode"

	"github.com/goccy/go-yaml"
	"github.com/goccy/go-yaml/ast"
	"github.com/johejo/argocdapp2helmfile/internal/applicationmapping"
)

type kustomizeOptions struct {
	namePrefix                string
	nameSuffix                string
	namespace                 string
	images                    []kustomizeImage
	replicas                  []kustomizeReplica
	commonLabels              yaml.MapSlice
	labelWithoutSelector      bool
	labelIncludeTemplates     bool
	commonAnnotations         yaml.MapSlice
	commonAnnotationsEnvsubst bool
}

type kustomizeMap yaml.MapSlice

type nonStringYAMLKey struct{}

func (mapping *kustomizeMap) UnmarshalYAML(node ast.Node) error {
	var ordered yaml.MapSlice
	if err := yaml.NodeToValue(node, &ordered, yaml.UseOrderedMap()); err != nil {
		return err
	}
	root, ok := node.(*ast.MappingNode)
	if !ok {
		*mapping = kustomizeMap(ordered)
		return nil
	}
	for _, option := range root.Values {
		key, ok := option.Key.(*ast.StringNode)
		if !ok {
			continue
		}
		mapped, known := applicationmapping.LookupKustomizeOption(key.Value)
		if !known || mapped.ValueKind != applicationmapping.KustomizeStringMap {
			continue
		}
		nested, ok := option.Value.(*ast.MappingNode)
		if !ok {
			continue
		}
		for _, item := range nested.Values {
			if item.Key.Type() == ast.StringType {
				continue
			}
			for i := range ordered {
				if ordered[i].Key != key.Value {
					continue
				}
				orderedNested, ok := ordered[i].Value.(yaml.MapSlice)
				if ok && len(orderedNested) != 0 {
					orderedNested[0].Key = nonStringYAMLKey{}
					ordered[i].Value = orderedNested
				}
			}
			break
		}
	}
	*mapping = kustomizeMap(ordered)
	return nil
}

type kustomizeImage struct {
	Name    string `yaml:"name"`
	NewName string `yaml:"newName,omitempty"`
	NewTag  string `yaml:"newTag,omitempty"`
	Digest  string `yaml:"digest,omitempty"`
}

// kustomizeReplica mirrors Kustomize's builtin ReplicaCountTransformer replica field.
type kustomizeReplica struct {
	Name  string `yaml:"name"`
	Count int64  `yaml:"count"`
}

func parseKustomizeOptions(items kustomizeMap, field string) (kustomizeOptions, error) {
	var result kustomizeOptions
	for _, item := range items {
		key, ok := item.Key.(string)
		if !ok {
			return result, fmt.Errorf("%s contains a non-string option name", field)
		}
		option, known := applicationmapping.LookupKustomizeOption(key)
		if !known {
			return result, fmt.Errorf("%s.%s is not supported", field, key)
		}
		if option.ValueKind == applicationmapping.KustomizeUnsupported {
			return result, fmt.Errorf("%s.%s is not supported: %s", field, key, option.Reason)
		}
		value, err := parseKustomizeOptionValue(option, item.Value, field+"."+key)
		if err != nil {
			return result, err
		}
		switch option.Name {
		case "namePrefix":
			result.namePrefix = value.(string)
		case "nameSuffix":
			result.nameSuffix = value.(string)
		case "namespace":
			result.namespace = value.(string)
		case "images":
			result.images = value.([]kustomizeImage)
		case "replicas":
			result.replicas = value.([]kustomizeReplica)
		case "commonLabels":
			result.commonLabels = value.(yaml.MapSlice)
		case "labelWithoutSelector":
			result.labelWithoutSelector = value.(bool)
		case "labelIncludeTemplates":
			result.labelIncludeTemplates = value.(bool)
		case "commonAnnotations":
			result.commonAnnotations = value.(yaml.MapSlice)
		case "commonAnnotationsEnvsubst":
			result.commonAnnotationsEnvsubst = value.(bool)
		case "forceCommonLabels", "forceCommonAnnotations":
			// Validated only: helmfile has no equivalent setting.
		default:
			panic("unhandled Kustomize option: " + option.Name)
		}
	}
	if result.labelIncludeTemplates && !result.labelWithoutSelector {
		return result, fmt.Errorf(
			"%s.labelIncludeTemplates cannot be true when labelWithoutSelector is false",
			field,
		)
	}
	return result, nil
}

func parseKustomizeOptionValue(
	option applicationmapping.KustomizeOption,
	value any,
	field string,
) (any, error) {
	switch option.ValueKind {
	case applicationmapping.KustomizeString:
		return readOptionalStringYAMLOption(value, field)
	case applicationmapping.KustomizeBoolean:
		return readOptionalBooleanYAMLOption(value, field)
	case applicationmapping.KustomizeStringMap:
		return readOptionalStringMapYAMLOption(value, field)
	case applicationmapping.KustomizeImages:
		return parseKustomizeImages(value, field)
	case applicationmapping.KustomizeReplicas:
		return parseKustomizeReplicas(value, field)
	default:
		panic("unknown Kustomize option value kind: " + option.ValueKind)
	}
}

func readOptionalStringMapYAMLOption(value any, field string) (yaml.MapSlice, error) {
	if isNilOrEmptyCollection(value) {
		return nil, nil
	}
	mapping, ok := value.(yaml.MapSlice)
	if !ok {
		return nil, fmt.Errorf("%s must be a mapping", field)
	}
	result := make(yaml.MapSlice, 0, len(mapping))
	for _, item := range mapping {
		key, ok := item.Key.(string)
		if !ok {
			return nil, fmt.Errorf("%s contains a non-string key", field)
		}
		text, ok := item.Value.(string)
		if !ok {
			return nil, fmt.Errorf("%s.%s must be a string", field, key)
		}
		result = append(result, yaml.MapItem{Key: key, Value: text})
	}
	return result, nil
}

func (options kustomizeOptions) values() yaml.MapSlice {
	var values yaml.MapSlice
	if options.namePrefix != "" {
		values = append(values, yaml.MapItem{Key: "namePrefix", Value: options.namePrefix})
	}
	if options.nameSuffix != "" {
		values = append(values, yaml.MapItem{Key: "nameSuffix", Value: options.nameSuffix})
	}
	if options.namespace != "" {
		values = append(values, yaml.MapItem{Key: "namespace", Value: options.namespace})
	}
	if len(options.images) != 0 {
		values = append(values, yaml.MapItem{Key: "images", Value: options.images})
	}
	return values
}

func parseKustomizeImages(value any, field string) ([]kustomizeImage, error) {
	if isNilOrEmptyCollection(value) {
		return nil, nil
	}
	sequence, ok := value.([]any)
	if !ok {
		return nil, fmt.Errorf("%s must be a sequence", field)
	}
	images := make([]kustomizeImage, 0, len(sequence))
	for i, raw := range sequence {
		text, ok := raw.(string)
		if !ok {
			return nil, fmt.Errorf("%s[%d] must be a string", field, i)
		}
		image, err := parseKustomizeImage(text)
		if err != nil {
			return nil, fmt.Errorf("%s[%d] %q: %w", field, i, text, err)
		}
		images = append(images, image)
	}
	return images, nil
}

func parseKustomizeImage(value string) (kustomizeImage, error) {
	if value == "" {
		return kustomizeImage{}, errors.New("must not be empty")
	}
	if strings.IndexFunc(value, unicode.IsSpace) >= 0 ||
		strings.IndexFunc(value, unicode.IsControl) >= 0 {
		return kustomizeImage{}, errors.New("whitespace and control characters are not supported")
	}
	if strings.Count(value, "=") > 1 {
		return kustomizeImage{}, errors.New("must contain at most one '='")
	}

	name := ""
	replacement := value
	if before, after, found := strings.Cut(value, "="); found {
		name = before
		replacement = after
		if err := validateKustomizeImageName(name); err != nil {
			return kustomizeImage{}, fmt.Errorf("old image name: %w", err)
		}
		if replacement == "" {
			return kustomizeImage{}, errors.New("replacement image must not be empty")
		}
	}

	newName, newTag, digest, err := splitKustomizeImageReplacement(replacement)
	if err != nil {
		return kustomizeImage{}, err
	}
	if name == "" {
		name = newName
		newName = ""
	}
	return kustomizeImage{
		Name:    name,
		NewName: newName,
		NewTag:  newTag,
		Digest:  digest,
	}, nil
}

func splitKustomizeImageReplacement(value string) (string, string, string, error) {
	if strings.Count(value, "@") > 1 {
		return "", "", "", errors.New("must contain at most one '@'")
	}
	if name, digest, found := strings.Cut(value, "@"); found {
		if err := validateKustomizeImageName(name); err != nil {
			return "", "", "", fmt.Errorf("image name: %w", err)
		}
		if digest == "" || !strings.Contains(digest, ":") ||
			strings.HasPrefix(digest, ":") || strings.HasSuffix(digest, ":") {
			return "", "", "", errors.New("digest must use algorithm:value form")
		}
		if strings.ContainsAny(digest, "=/@") {
			return "", "", "", errors.New("digest contains an unsupported character")
		}
		return name, "", digest, nil
	}

	lastSlash := strings.LastIndexByte(value, '/')
	lastColon := strings.LastIndexByte(value, ':')
	name := value
	tag := ""
	if lastColon > lastSlash {
		name = value[:lastColon]
		tag = value[lastColon+1:]
		if tag == "" {
			return "", "", "", errors.New("tag must not be empty")
		}
		if strings.ContainsAny(tag, "=/:@") {
			return "", "", "", errors.New("tag contains an unsupported character")
		}
	}
	if err := validateKustomizeImageName(name); err != nil {
		return "", "", "", fmt.Errorf("image name: %w", err)
	}
	return name, tag, "", nil
}

func parseKustomizeReplicas(value any, field string) ([]kustomizeReplica, error) {
	if isNilOrEmptyCollection(value) {
		return nil, nil
	}
	sequence, ok := value.([]any)
	if !ok {
		return nil, fmt.Errorf("%s must be a sequence", field)
	}
	replicas := make([]kustomizeReplica, 0, len(sequence))
	for i, raw := range sequence {
		replica, err := parseKustomizeReplica(raw, fmt.Sprintf("%s[%d]", field, i))
		if err != nil {
			return nil, err
		}
		replicas = append(replicas, replica)
	}
	return replicas, nil
}

func parseKustomizeReplica(value any, field string) (kustomizeReplica, error) {
	mapping, ok := value.(yaml.MapSlice)
	if !ok {
		return kustomizeReplica{}, fmt.Errorf("%s must be a mapping", field)
	}
	var replica kustomizeReplica
	var hasCount bool
	for _, item := range mapping {
		key, ok := item.Key.(string)
		if !ok {
			return kustomizeReplica{}, fmt.Errorf("%s contains a non-string field name", field)
		}
		switch key {
		case "name":
			name, ok := item.Value.(string)
			if !ok {
				return kustomizeReplica{}, fmt.Errorf("%s.name must be a string", field)
			}
			replica.Name = name
		case "count":
			count, err := readKustomizeReplicaCount(item.Value, field+".count")
			if err != nil {
				return kustomizeReplica{}, err
			}
			replica.Count = count
			hasCount = true
		default:
			return kustomizeReplica{}, fmt.Errorf("%s.%s is not supported", field, key)
		}
	}
	if replica.Name == "" {
		return kustomizeReplica{}, fmt.Errorf("%s.name is required", field)
	}
	if !hasCount {
		return kustomizeReplica{}, fmt.Errorf("%s.count is required", field)
	}
	return replica, nil
}

// Argo CD's count is an IntOrString, so a numeric string is valid.
func readKustomizeReplicaCount(value any, field string) (int64, error) {
	if text, ok := value.(string); ok {
		count, err := strconv.ParseInt(text, 10, 64)
		if err != nil {
			return 0, fmt.Errorf("%s must be an integer: %q", field, text)
		}
		return count, nil
	}
	count, ok := integerValue(value)
	if !ok {
		return 0, fmt.Errorf("%s must be an integer", field)
	}
	return count, nil
}

func validateKustomizeImageName(value string) error {
	if value == "" {
		return errors.New("must not be empty")
	}
	if strings.ContainsAny(value, "=@") {
		return errors.New("contains an unsupported character")
	}
	lastSlash := strings.LastIndexByte(value, '/')
	if strings.LastIndexByte(value, ':') > lastSlash {
		return errors.New("contains a tag where only an image name is allowed")
	}
	for segment := range strings.SplitSeq(value, "/") {
		if segment == "" || segment == "." || segment == ".." {
			return errors.New("must contain non-empty image name segments")
		}
	}
	return nil
}
