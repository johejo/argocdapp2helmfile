package main

import (
	"strings"
	"testing"
)

func TestConvertApplicationSetMatrixListList(t *testing.T) {
	testConvertApplicationSetFixture(t, "matrix-list-list", false)
}

func TestConvertApplicationSetMatrixGitList(t *testing.T) {
	testConvertApplicationSetFixture(t, "matrix-git-list", true)
}

func TestConvertApplicationSetMatrixListGit(t *testing.T) {
	testConvertApplicationSetFixture(t, "matrix-list-git", true)
}

func TestConvertApplicationSetMatrixTemplate(t *testing.T) {
	testConvertApplicationSetFixture(t, "matrix-template", false)
}

func TestConvertApplicationSetMatrixGitGit(t *testing.T) {
	testConvertApplicationSetFixture(t, "matrix-git-git", true)
}

func TestConvertApplicationSetNestedMatrix(t *testing.T) {
	testConvertApplicationSetFixture(t, "matrix-nested", false)
}

func TestConvertApplicationSetMatrixMerge(t *testing.T) {
	testConvertApplicationSetFixture(t, "matrix-merge", false)
}

func TestConvertApplicationSetLegacyMatrix(t *testing.T) {
	testConvertApplicationSetFixture(t, "legacy-matrix", true)
}

func TestConvertApplicationSetNestedSelectorsAlwaysApply(t *testing.T) {
	const fixture = "applicationset/matrix-nested-selectors/"
	want := readTestdata(t, fixture+"helmfile.yaml")
	for _, mode := range []string{"omitted", "false", "true"} {
		t.Run(mode, func(t *testing.T) {
			output, err := convert(
				[]byte(readTestdata(t, fixture+"application-"+mode+".yaml")),
			)
			if err != nil {
				t.Fatal(err)
			}
			if string(output) != want {
				t.Fatalf("unexpected output:\n%s\nwant:\n%s", output, want)
			}
		})
	}
}

func TestConvertApplicationSetMatrixListGitMissingKeyModes(t *testing.T) {
	const fixture = "applicationset/matrix-list-git-missingkey/"
	config, err := parseConfig([]byte(readTestdata(t, fixture+"config.yaml")))
	if err != nil {
		t.Fatal(err)
	}
	want := readTestdata(t, fixture+"helmfile.yaml")
	for _, mode := range []string{"default", "invalid", "zero"} {
		t.Run(mode, func(t *testing.T) {
			output, err := convertWithConfig(
				[]byte(readTestdata(t, fixture+"application-"+mode+".yaml")),
				config,
			)
			if err != nil {
				t.Fatal(err)
			}
			if string(output) != want {
				t.Fatalf("unexpected output:\n%s\nwant:\n%s", output, want)
			}
		})
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
		"child template": {
			fixture: "child-template.yaml",
			want: "matrix.generators[0].list.template is not supported " +
				"in a matrix generator",
		},
		"Git child template": {
			fixture: "git-child-template.yaml",
			want: "matrix.generators[1].git.template is not supported " +
				"in a matrix generator",
		},
		"nested matrix template": {
			fixture: "nested-template.yaml",
			want:    "matrix.generators[0].matrix.template is not supported",
		},
		"deeply nested matrix": {
			fixture: "deeply-nested-matrix.yaml",
			want:    "matrix.generators[0].matrix exceeds the supported nesting depth",
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

func TestConvertApplicationSetNestedMatrixErrorReportsEveryOrigin(t *testing.T) {
	input := readTestdata(t, "applicationset/matrix-errors/nested-origin.yaml")
	_, err := convert([]byte(input))
	want := "spec.generators[0].matrix.generators[0].list.elements[0] × " +
		"spec.generators[0].matrix.generators[1].matrix.generators[0].list.elements[0] × " +
		"spec.generators[0].matrix.generators[1].matrix.generators[1].list.elements[0]"
	if err == nil || !strings.Contains(err.Error(), want) {
		t.Fatalf("unexpected error: %v", err)
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

func TestConvertApplicationSetMatrixGitValuesRejectsRenderedDuplicateKey(t *testing.T) {
	const fixture = "applicationset/matrix-errors/git-values-duplicate/"
	config, err := parseConfig([]byte(readTestdata(t, fixture+"config.yaml")))
	if err != nil {
		t.Fatal(err)
	}
	input := readTestdata(t, fixture+"application.yaml")
	_, err = convertWithConfig([]byte(input), config)
	want := "spec.generators[0].matrix.generators[0].list.elements[0] -> " +
		`spec.generators[0].matrix.generators[1].git.files["clusters/dev.yaml"]` +
		`: values: templating produced duplicate mapping key "collision"`
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
