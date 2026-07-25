package main

import (
	"strings"
	"testing"
)

func TestConvertApplicationSetMergeListList(t *testing.T) {
	testConvertApplicationSetMergeFixture(t, "merge-list-list", false)
}

func TestConvertApplicationSetMergeListGit(t *testing.T) {
	testConvertApplicationSetMergeFixture(t, "merge-list-git", true)
}

func TestConvertApplicationSetLegacyMergeDottedKey(t *testing.T) {
	testConvertApplicationSetMergeFixture(t, "legacy-merge", false)
}

func TestConvertApplicationSetMergeNestedCombinations(t *testing.T) {
	testConvertApplicationSetMergeFixture(t, "merge-nested-combinations", false)
}

func testConvertApplicationSetMergeFixture(t *testing.T, name string, withConfig bool) {
	t.Helper()
	var config *conversionConfig
	if withConfig {
		var err error
		config, err = parseConfig([]byte(
			readTestdata(t, "applicationset/"+name+"/config.yaml"),
		))
		if err != nil {
			t.Fatal(err)
		}
	}
	output, err := convertWithConfig(
		[]byte(readTestdata(t, "applicationset/"+name+"/application.yaml")),
		config,
	)
	if err != nil {
		t.Fatal(err)
	}
	want := readTestdata(t, "applicationset/"+name+"/helmfile.yaml")
	if string(output) != want {
		t.Fatalf("unexpected output:\n%s\nwant:\n%s", output, want)
	}
}

func TestApplicationSetMergeErrors(t *testing.T) {
	tests := map[string]struct {
		fixture string
		want    string
	}{
		"missing merge keys": {
			fixture: "missing-merge-keys.yaml",
			want:    "merge.mergeKeys must contain at least one key",
		},
		"invalid merge key type": {
			fixture: "invalid-merge-key-type.yaml",
			want:    "merge.mergeKeys[0] must be a string",
		},
		"one child": {
			fixture: "one-child.yaml",
			want:    "merge.generators must contain at least two generators",
		},
		"List child template": {
			fixture: "list-child-template.yaml",
			want:    "merge.generators[0].list.template is not supported in a merge generator",
		},
		"Git child template": {
			fixture: "git-child-template.yaml",
			want:    "merge.generators[1].git.template is not supported in a merge generator",
		},
		"empty child": {
			fixture: "empty-child.yaml",
			want:    "merge.generators[1] generated no parameters",
		},
		"Go template nested key": {
			fixture: "go-template-nested-key.yaml",
			want:    "merge.mergeKeys[0] must not be a nested key when goTemplate is enabled",
		},
		"unknown field": {
			fixture: "unknown-field.yaml",
			want:    "merge.extra is not supported",
		},
		"deeply nested combination": {
			fixture: "deeply-nested-combination.yaml",
			want: "spec.generators[0].merge.generators[0].matrix.generators[0].merge " +
				"exceeds the supported nesting depth",
		},
		"nested merge template": {
			fixture: "nested-merge-template.yaml",
			want: "spec.generators[0].matrix.generators[0].merge.template is not supported " +
				"in a nested merge generator",
		},
		"nested terminal template": {
			fixture: "nested-terminal-template.yaml",
			want: "spec.generators[0].merge.generators[0].matrix.generators[0].list.template " +
				"is not supported in a matrix generator",
		},
	}
	for name, test := range tests {
		t.Run(name, func(t *testing.T) {
			input := readTestdata(t, "applicationset/merge-errors/"+test.fixture)
			_, err := convert([]byte(input))
			if err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("unexpected error: %v", err)
			}
		})
	}
}

func TestApplicationSetNestedCombinationRenderErrorReportsEveryOrigin(t *testing.T) {
	input := readTestdata(t, "applicationset/merge-errors/nested-origin.yaml")
	_, err := convert([]byte(input))
	want := "spec.generators[0].merge.generators[0].matrix.generators[0].list.elements[0] × " +
		"spec.generators[0].merge.generators[0].matrix.generators[1].list.elements[0] ← " +
		"spec.generators[0].merge.generators[1].merge.generators[0].list.elements[0] ← " +
		"spec.generators[0].merge.generators[1].merge.generators[1].list.elements[0]"
	if err == nil || !strings.Contains(err.Error(), want) {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestApplicationSetMergeDuplicateReportsBothOrigins(t *testing.T) {
	input := readTestdata(t, "applicationset/merge-errors/duplicate-key.yaml")
	_, err := convert([]byte(input))
	for _, want := range []string{
		"spec.generators[0].merge.generators[1].list.elements[0]",
		"spec.generators[0].merge.generators[1].list.elements[1]",
	} {
		if err == nil || !strings.Contains(err.Error(), want) {
			t.Fatalf("unexpected error: %v", err)
		}
	}
}

func TestApplicationSetMergeRenderErrorReportsEveryAppliedOrigin(t *testing.T) {
	input := readTestdata(t, "applicationset/merge-errors/render-origin.yaml")
	_, err := convert([]byte(input))
	want := "spec.generators[0].merge.generators[0].list.elements[0] ← " +
		"spec.generators[0].merge.generators[1].list.elements[0] ← " +
		"spec.generators[0].merge.generators[2].list.elements[0]"
	if err == nil || !strings.Contains(err.Error(), want) {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestMergeParamsLaterGeneratorWinsRecursively(t *testing.T) {
	base := map[string]any{
		"scalar":   "base",
		"sequence": []any{"base"},
		"nested": map[string]any{
			"base":   "kept",
			"shared": "base",
		},
	}
	override := map[string]any{
		"scalar":   "override",
		"sequence": []any{"override"},
		"nested": map[string]any{
			"override": "kept",
			"shared":   "override",
		},
	}
	got := mergeMatrixParams(override, base)
	nested := got["nested"].(map[string]any)
	if got["scalar"] != "override" ||
		got["sequence"].([]any)[0] != "override" ||
		nested["base"] != "kept" ||
		nested["override"] != "kept" ||
		nested["shared"] != "override" {
		t.Fatalf("unexpected merged parameters: %#v", got)
	}
}
