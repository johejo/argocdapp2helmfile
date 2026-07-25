package main

import (
	"fmt"
	"strings"
	"unicode"

	"github.com/goccy/go-yaml"
)

type kustomizeOptions struct {
	namePrefix string
	nameSuffix string
	namespace  string
	images     []kustomizeImage
}

type kustomizeImage struct {
	Name    string `yaml:"name"`
	NewName string `yaml:"newName,omitempty"`
	NewTag  string `yaml:"newTag,omitempty"`
	Digest  string `yaml:"digest,omitempty"`
}

func parseKustomizeOptions(items yaml.MapSlice, field string) (kustomizeOptions, error) {
	var result kustomizeOptions
	for _, item := range items {
		key, ok := item.Key.(string)
		if !ok {
			return result, fmt.Errorf("%s contains a non-string option name", field)
		}
		switch key {
		case "namePrefix":
			value, err := readOptionalStringYAMLOption(item.Value, field+".namePrefix")
			if err != nil {
				return result, err
			}
			result.namePrefix = value
		case "nameSuffix":
			value, err := readOptionalStringYAMLOption(item.Value, field+".nameSuffix")
			if err != nil {
				return result, err
			}
			result.nameSuffix = value
		case "namespace":
			value, err := readOptionalStringYAMLOption(item.Value, field+".namespace")
			if err != nil {
				return result, err
			}
			result.namespace = value
		case "images":
			images, err := parseKustomizeImages(item.Value, field+".images")
			if err != nil {
				return result, err
			}
			result.images = images
		default:
			return result, fmt.Errorf("%s.%s is not supported", field, key)
		}
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
		return kustomizeImage{}, fmt.Errorf("must not be empty")
	}
	if strings.IndexFunc(value, unicode.IsSpace) >= 0 ||
		strings.IndexFunc(value, unicode.IsControl) >= 0 {
		return kustomizeImage{}, fmt.Errorf("whitespace and control characters are not supported")
	}
	if strings.Count(value, "=") > 1 {
		return kustomizeImage{}, fmt.Errorf("must contain at most one '='")
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
			return kustomizeImage{}, fmt.Errorf("replacement image must not be empty")
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
		return "", "", "", fmt.Errorf("must contain at most one '@'")
	}
	if name, digest, found := strings.Cut(value, "@"); found {
		if err := validateKustomizeImageName(name); err != nil {
			return "", "", "", fmt.Errorf("image name: %w", err)
		}
		if digest == "" || !strings.Contains(digest, ":") ||
			strings.HasPrefix(digest, ":") || strings.HasSuffix(digest, ":") {
			return "", "", "", fmt.Errorf("digest must use algorithm:value form")
		}
		if strings.ContainsAny(digest, "=/@") {
			return "", "", "", fmt.Errorf("digest contains an unsupported character")
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
			return "", "", "", fmt.Errorf("tag must not be empty")
		}
		if strings.ContainsAny(tag, "=/:@") {
			return "", "", "", fmt.Errorf("tag contains an unsupported character")
		}
	}
	if err := validateKustomizeImageName(name); err != nil {
		return "", "", "", fmt.Errorf("image name: %w", err)
	}
	return name, tag, "", nil
}

func validateKustomizeImageName(value string) error {
	if value == "" {
		return fmt.Errorf("must not be empty")
	}
	if strings.ContainsAny(value, "=@") {
		return fmt.Errorf("contains an unsupported character")
	}
	lastSlash := strings.LastIndexByte(value, '/')
	if strings.LastIndexByte(value, ':') > lastSlash {
		return fmt.Errorf("contains a tag where only an image name is allowed")
	}
	for segment := range strings.SplitSeq(value, "/") {
		if segment == "" || segment == "." || segment == ".." {
			return fmt.Errorf("must contain non-empty image name segments")
		}
	}
	return nil
}
