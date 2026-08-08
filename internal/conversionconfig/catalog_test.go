package conversionconfig

import (
	"reflect"
	"slices"
	"strings"
	"testing"
)

func TestFieldsCoverResource(t *testing.T) {
	want := resourceFieldPaths()
	got := make([]string, 0, len(Fields()))
	seen := make(map[string]bool, len(Fields()))
	for i, field := range Fields() {
		if field.Path == "" || seen[field.Path] {
			t.Errorf("field %d has an empty or duplicate path %q", i, field.Path)
		}
		if field.Required == "" || field.Description == "" {
			t.Errorf("field %q lacks required metadata", field.Path)
		}
		seen[field.Path] = true
		got = append(got, field.Path)
	}
	slices.Sort(got)
	if !slices.Equal(got, want) {
		t.Errorf("documented fields = %q, want resource fields %q", got, want)
	}
}

func resourceFieldPaths() []string {
	result := yamlFieldPaths("", reflect.TypeFor[Resource]())
	for path, typ := range map[string]reflect.Type{
		"sources[]":       reflect.TypeFor[Source](),
		"destinations[]":  reflect.TypeFor[Destination](),
		"clusters[]":      reflect.TypeFor[Cluster](),
		"releaseLabels[]": reflect.TypeFor[ReleaseLabelRule](),
	} {
		result = append(result, yamlFieldPaths(path, typ)...)
	}
	slices.Sort(result)
	return result
}

func yamlFieldPaths(prefix string, typ reflect.Type) []string {
	result := make([]string, 0, typ.NumField())
	for i := range typ.NumField() {
		name := strings.Split(typ.Field(i).Tag.Get("yaml"), ",")[0]
		if prefix != "" {
			name = prefix + "." + name
		}
		result = append(result, name)
	}
	return result
}
