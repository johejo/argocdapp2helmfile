package main

import (
	"errors"
	"fmt"
	"reflect"
	"slices"
	"strings"

	"github.com/goccy/go-yaml"
)

func mergeTemplate(override, base yaml.MapSlice) yaml.MapSlice {
	if override == nil {
		return cloneMapSlice(base)
	}
	result := cloneMapSlice(override)
	for _, baseItem := range base {
		index := mapSliceIndex(result, baseItem.Key)
		if index < 0 {
			result = append(result, cloneMapItem(baseItem))
			continue
		}
		overrideMap, overrideOK := result[index].Value.(yaml.MapSlice)
		baseMap, baseOK := baseItem.Value.(yaml.MapSlice)
		if overrideOK && baseOK {
			result[index].Value = mergeTemplate(overrideMap, baseMap)
		} else if isNilOrEmptyCollection(result[index].Value) {
			result[index].Value = cloneValue(baseItem.Value)
		}
	}
	return result
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
	for i, item := range items {
		if reflect.DeepEqual(item.Key, key) {
			return i
		}
	}
	return -1
}

func decodeApplicationTemplatePatch(input string) (yaml.MapSlice, error) {
	const field = "rendered spec.templatePatch"
	if err := requireSingleDocument([]byte(input), field); err != nil {
		return nil, err
	}
	var value any
	decoder := yaml.NewDecoder(strings.NewReader(input), yaml.UseOrderedMap())
	if err := decoder.Decode(&value); err != nil {
		return nil, fmt.Errorf("decode %s: %w", field, err)
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
	result := cloneMapSlice(target)
	for _, patchItem := range patch {
		index := mapSliceIndex(result, patchItem.Key)
		if patchItem.Value == nil {
			if index >= 0 {
				result = slices.Delete(result, index, index+1)
			}
			continue
		}
		if index < 0 {
			result = append(result, cloneMapItem(patchItem))
			continue
		}
		targetMap, targetOK := result[index].Value.(yaml.MapSlice)
		patchMap, patchOK := patchItem.Value.(yaml.MapSlice)
		if targetOK && patchOK {
			result[index].Value = mergeApplicationTemplatePatch(targetMap, patchMap)
			continue
		}
		result[index].Value = cloneValue(patchItem.Value)
	}
	return result
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
