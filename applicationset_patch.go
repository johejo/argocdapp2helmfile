package main

import (
	"errors"
	"fmt"
	"reflect"
	"slices"
	"strings"

	"github.com/goccy/go-yaml"
)

// mergeMapSlice merges from into a clone of into. deleteOnNil makes a nil
// incoming value remove the key; overwrite lets an incoming scalar replace a
// value that is neither nil nor an empty collection.
func mergeMapSlice(into, from yaml.MapSlice, deleteOnNil, overwrite bool) yaml.MapSlice {
	result := cloneMapSlice(into)
	for _, item := range from {
		index := mapSliceIndex(result, item.Key)
		if deleteOnNil && item.Value == nil {
			if index >= 0 {
				result = slices.Delete(result, index, index+1)
			}
			continue
		}
		if index < 0 {
			result = append(result, cloneMapItem(item))
			continue
		}
		intoMap, intoOK := result[index].Value.(yaml.MapSlice)
		fromMap, fromOK := item.Value.(yaml.MapSlice)
		switch {
		case intoOK && fromOK:
			result[index].Value = mergeMapSlice(intoMap, fromMap, deleteOnNil, overwrite)
		case overwrite || isNilOrEmptyCollection(result[index].Value):
			result[index].Value = cloneValue(item.Value)
		}
	}
	return result
}

func mergeTemplate(override, base yaml.MapSlice) yaml.MapSlice {
	if override == nil {
		return cloneMapSlice(base)
	}
	return mergeMapSlice(override, base, false, false)
}

func cloneMapSlice(value yaml.MapSlice) yaml.MapSlice {
	result := make(yaml.MapSlice, len(value))
	for i, item := range value {
		result[i] = cloneMapItem(item)
	}
	return result
}

func cloneMapItem(item yaml.MapItem) yaml.MapItem {
	return yaml.MapItem{Key: cloneValue(item.Key), Value: cloneValue(item.Value)}
}

func cloneValue(value any) any {
	switch typed := value.(type) {
	case yaml.MapSlice:
		return cloneMapSlice(typed)
	case map[string]any:
		result := make(map[string]any, len(typed))
		for key, item := range typed {
			result[key] = cloneValue(item)
		}
		return result
	case []any:
		result := make([]any, len(typed))
		for i, item := range typed {
			result[i] = cloneValue(item)
		}
		return result
	default:
		return value
	}
}

func mapSliceIndex(items yaml.MapSlice, key any) int {
	stringKey, isString := key.(string)
	for i, item := range items {
		if isString {
			if itemKey, ok := item.Key.(string); ok {
				if itemKey == stringKey {
					return i
				}
				continue
			}
		}
		if reflect.DeepEqual(item.Key, key) {
			return i
		}
	}
	return -1
}

func decodeApplicationTemplatePatch(input string) (yaml.MapSlice, error) {
	const field = "rendered spec.templatePatch"
	value, err := decodeSingleDocument([]byte(input), field)
	if err != nil {
		return nil, err
	}
	patch, ok := value.(yaml.MapSlice)
	if !ok {
		return nil, fmt.Errorf("%s must contain a mapping", field)
	}
	if err := validateApplicationTemplatePatch(patch); err != nil {
		return nil, err
	}
	return patch, nil
}

func validateApplicationTemplatePatch(value any) error {
	switch typed := value.(type) {
	case yaml.MapSlice:
		for _, item := range typed {
			key, ok := item.Key.(string)
			if !ok {
				return errors.New("rendered spec.templatePatch mapping keys must be strings")
			}
			if isStrategicMergePatchDirective(key) {
				return fmt.Errorf(
					"rendered spec.templatePatch contains unsupported Strategic Merge Patch directive %q",
					key,
				)
			}
			if err := validateApplicationTemplatePatch(item.Value); err != nil {
				return err
			}
		}
	case []any:
		for _, item := range typed {
			if err := validateApplicationTemplatePatch(item); err != nil {
				return err
			}
		}
	}
	return nil
}

func isStrategicMergePatchDirective(key string) bool {
	return key == "$patch" ||
		key == "$retainKeys" ||
		strings.HasPrefix(key, "$setElementOrder/") ||
		strings.HasPrefix(key, "$deleteFromPrimitiveList/")
}

func mergeApplicationTemplatePatch(target, patch yaml.MapSlice) yaml.MapSlice {
	return mergeMapSlice(target, patch, true, true)
}

func applicationProject(application yaml.MapSlice) (any, bool) {
	specIndex := mapSliceIndex(application, "spec")
	if specIndex < 0 {
		return nil, false
	}
	spec, ok := application[specIndex].Value.(yaml.MapSlice)
	if !ok {
		return nil, false
	}
	projectIndex := mapSliceIndex(spec, "project")
	if projectIndex < 0 {
		return nil, false
	}
	return cloneValue(spec[projectIndex].Value), true
}

func restoreApplicationProject(application yaml.MapSlice, project any, hasProject bool) yaml.MapSlice {
	specIndex := mapSliceIndex(application, "spec")
	if specIndex < 0 {
		if hasProject {
			application = append(application, yaml.MapItem{
				Key: "spec",
				Value: yaml.MapSlice{
					{Key: "project", Value: cloneValue(project)},
				},
			})
		}
		return application
	}
	spec, ok := application[specIndex].Value.(yaml.MapSlice)
	if !ok {
		return application
	}
	projectIndex := mapSliceIndex(spec, "project")
	if !hasProject {
		if projectIndex >= 0 {
			spec = slices.Delete(spec, projectIndex, projectIndex+1)
			application[specIndex].Value = spec
		}
		return application
	}
	if projectIndex >= 0 {
		spec[projectIndex].Value = cloneValue(project)
	} else {
		spec = append(spec, yaml.MapItem{Key: "project", Value: cloneValue(project)})
	}
	application[specIndex].Value = spec
	return application
}

func setMapSliceField(items yaml.MapSlice, key string, value any) yaml.MapSlice {
	index := mapSliceIndex(items, key)
	if index >= 0 {
		items[index].Value = value
		return items
	}
	return append(yaml.MapSlice{{Key: key, Value: value}}, items...)
}
