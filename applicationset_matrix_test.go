package main

import (
	"strings"
	"testing"
)

func TestConvertApplicationSetMatrixListList(t *testing.T) {
	testConvertApplicationSetMatrixFixture(t, "matrix-list-list", false)
}

func TestConvertApplicationSetMatrixGitList(t *testing.T) {
	testConvertApplicationSetMatrixFixture(t, "matrix-git-list", true)
}

func TestConvertApplicationSetMatrixListGit(t *testing.T) {
	testConvertApplicationSetMatrixFixture(t, "matrix-list-git", true)
}

func testConvertApplicationSetMatrixFixture(t *testing.T, name string, withConfig bool) {
	t.Helper()
	var config *conversionConfig
	if withConfig {
		var err error
		config, err = parseConfig([]byte(readTestdata(t, "applicationset/"+name+"/config.yaml")))
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

func TestApplicationSetMatrixErrors(t *testing.T) {
	tests := map[string]struct {
		fixture string
		want    string
	}{
		"one child": {
			fixture: "one-child.yaml",
			want:    "must contain exactly two generators",
		},
		"empty first child": {
			fixture: "empty-first-child.yaml",
			want:    "generators[0] generated no parameters",
		},
		"empty second child": {
			fixture: "empty-second-child.yaml",
			want: "list.elements[0] -> " +
				"spec.generators[0].matrix.generators[1] generated no parameters",
		},
		"matrix template": {
			fixture: "matrix-template.yaml",
			want:    "spec.generators[0].matrix.template is not supported",
		},
		"child template": {
			fixture: "child-template.yaml",
			want: "matrix.generators[0].list.template is not supported " +
				"in a matrix generator",
		},
		"nested matrix": {
			fixture: "nested-matrix.yaml",
			want:    "matrix.generators[0].matrix nesting is not supported",
		},
		"git git": {
			fixture: "git-git.yaml",
			want:    "spec.generators[0].matrix Git × Git is not supported",
		},
	}
	for name, test := range tests {
		t.Run(name, func(t *testing.T) {
			input := readTestdata(t, "applicationset/matrix-errors/"+test.fixture)
			_, err := convert([]byte(input))
			if err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("unexpected error: %v", err)
			}
		})
	}
}

func TestConvertApplicationSetMatrixErrorReportsBothOrigins(t *testing.T) {
	input := readTestdata(t, "applicationset/matrix-errors/duplicate-origin.yaml")
	_, err := convert([]byte(input))
	want := "spec.generators[0].matrix.generators[0].list.elements[0] × " +
		"spec.generators[0].matrix.generators[1].list.elements[1]"
	if err == nil || !strings.Contains(err.Error(), want) {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestMergeMatrixParamsFirstGeneratorWinsRecursively(t *testing.T) {
	first := map[string]any{
		"scalar": "first",
		"nested": map[string]any{
			"first":  "kept",
			"shared": "first",
		},
	}
	second := map[string]any{
		"scalar": "second",
		"nested": map[string]any{
			"second": "kept",
			"shared": "second",
		},
	}
	got := mergeMatrixParams(first, second)
	nested := got["nested"].(map[string]any)
	if got["scalar"] != "first" ||
		nested["first"] != "kept" ||
		nested["second"] != "kept" ||
		nested["shared"] != "first" {
		t.Fatalf("unexpected merged parameters: %#v", got)
	}
}
