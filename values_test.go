package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const globRepositoryURL = "git@example.com:charts.git"

func TestConvertExpandsValueFileGlobsInArgoOrder(t *testing.T) {
	config, err := parseConfig([]byte(readTestdata(t, "value-files/glob/config.yaml")))
	if err != nil {
		t.Fatal(err)
	}
	output, err := convertWithConfig(
		[]byte(readTestdata(t, "value-files/glob/application.yaml")),
		config,
	)
	if err != nil {
		t.Fatal(err)
	}
	assertOrdered(t, string(output), []string{
		"repository/chart/values/z.yaml",
		"repository/chart/values/nested/a.yaml",
		"repository/chart/values/nested/deep/c.yaml",
		"repository/chart/values/b.yaml",
		"repository/chart/values/00.yaml",
	})
	if strings.Count(string(output), "repository/chart/values/00.yaml") != 1 {
		t.Fatalf("explicit duplicate was not removed:\n%s", output)
	}
}

func TestConvertSupportsValueFileGlobSyntax(t *testing.T) {
	tests := map[string]struct {
		pattern string
		want    []string
	}{
		"star": {
			pattern: "values/*.yaml",
			want:    []string{"values/00.yaml", "values/b.yaml", "values/z.yaml"},
		},
		"question": {
			pattern: "values/?.yaml",
			want:    []string{"values/b.yaml", "values/z.yaml"},
		},
		"character class": {
			pattern: "values/[bz].yaml",
			want:    []string{"values/b.yaml", "values/z.yaml"},
		},
	}
	for name, test := range tests {
		t.Run(name, func(t *testing.T) {
			input := applicationWithValueFiles(t, "        - "+test.pattern+"\n")
			output, err := convertWithResolver(
				[]byte(input),
				globFixtureResolver(t),
			)
			if err != nil {
				t.Fatal(err)
			}
			assertOrdered(t, string(output), test.want)
		})
	}
}

func TestConvertValueFileGlobLocations(t *testing.T) {
	input := applicationWithValueFiles(t, `        - ../shared/*.yaml
        - /repository*.yaml
`)
	output, err := convertWithResolver([]byte(input), globFixtureResolver(t))
	if err != nil {
		t.Fatal(err)
	}
	assertOrdered(t, string(output), []string{
		"repository/shared/root.yaml",
		"repository/repository.yaml",
	})
}

func TestConvertValueFileRefGlob(t *testing.T) {
	input := readTestdata(t, "value-files/glob/ref-application.yaml")
	resolver := testSourceResolver(
		t,
		testSource{
			repoURL: globRepositoryURL, targetRevision: "main",
			root: "testdata/value-files/glob/repository",
		},
		testSource{
			repoURL: "git@example.com:values.git", targetRevision: "stable",
			root: "testdata/value-files/glob/referenced",
		},
	)
	output, err := convertWithResolver([]byte(input), resolver)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(output), "referenced/prod/glob-app.yaml") {
		t.Fatalf("chart-source build environment was not expanded in $ref glob:\n%s", output)
	}
}

func TestConvertValueFileGlobMissingHandling(t *testing.T) {
	for _, ignore := range []bool{false, true} {
		t.Run(map[bool]string{false: "error", true: "ignored"}[ignore], func(t *testing.T) {
			valueFiles := "        - missing/**/*.yaml\n"
			if ignore {
				valueFiles += "      ignoreMissingValueFiles: true\n"
			}
			input := applicationWithValueFiles(t, valueFiles)
			output, err := convertWithResolver([]byte(input), globFixtureResolver(t))
			if !ignore {
				if err == nil || !strings.Contains(err.Error(), "valueFiles[0]") ||
					!strings.Contains(err.Error(), "matched no files") {
					t.Fatalf("unexpected error: %v\n%s", err, output)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if !strings.Contains(string(output), "missingFileHandler: Warn") ||
				strings.Contains(string(output), "missing/**/*.yaml") {
				t.Fatalf("unexpected output:\n%s", output)
			}
		})
	}
}

func TestConvertValueFileGlobLocalRootRequirements(t *testing.T) {
	fileRoot := filepath.Join(t.TempDir(), "file")
	if err := os.WriteFile(fileRoot, []byte("not a directory"), 0o644); err != nil {
		t.Fatal(err)
	}
	symlinkRoot := filepath.Join(t.TempDir(), "root-link")
	if err := os.Symlink("testdata/value-files/glob/repository", symlinkRoot); err != nil {
		t.Fatal(err)
	}
	tests := map[string]struct {
		root string
		want string
	}{
		"template": {
			root: `{{ requiredEnv "CHARTS_ROOT" }}`,
			want: "must not contain a template expression",
		},
		"missing": {
			root: filepath.Join(t.TempDir(), "missing"),
			want: "no such file or directory",
		},
		"file": {
			root: fileRoot,
			want: "must be a directory",
		},
		"symlink": {
			root: symlinkRoot,
			want: "must not be a symlink",
		},
	}
	for name, test := range tests {
		t.Run(name, func(t *testing.T) {
			input := applicationWithValueFiles(t, "        - values/*.yaml\n")
			resolver := testSourceResolver(t, testSource{
				repoURL: globRepositoryURL, targetRevision: "main", root: test.root,
			})
			if output, err := convertWithResolver([]byte(input), resolver); err == nil ||
				!strings.Contains(err.Error(), test.want) {
				t.Fatalf("unexpected result: %v\n%s", err, output)
			}
		})
	}
}

func TestConvertValueFileGlobSymlinks(t *testing.T) {
	root := t.TempDir()
	chartValues := filepath.Join(root, "chart", "values")
	if err := os.MkdirAll(filepath.Join(chartValues, "real"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(chartValues, "real", "inside.yaml"), []byte("x: y\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("real/inside.yaml", filepath.Join(chartValues, "inside.yaml")); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("inside.yaml", filepath.Join(chartValues, "inside-chain.yaml")); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("loop-b.yaml", filepath.Join(chartValues, "loop-a.yaml")); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("loop-a.yaml", filepath.Join(chartValues, "loop-b.yaml")); err != nil {
		t.Fatal(err)
	}
	outside := filepath.Join(t.TempDir(), "outside.yaml")
	if err := os.WriteFile(outside, []byte("outside: true\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(chartValues, "outside.yaml")); err != nil {
		t.Fatal(err)
	}

	resolver := testSourceResolver(t, testSource{
		repoURL: globRepositoryURL, targetRevision: "main", root: root,
	})
	input := applicationWithValueFiles(t, "        - values/inside*.yaml\n")
	output, err := convertWithResolver([]byte(input), resolver)
	if err != nil {
		t.Fatal(err)
	}
	assertOrdered(t, string(output), []string{"values/inside-chain.yaml", "values/inside.yaml"})

	input = applicationWithValueFiles(t, "        - values/outside*.yaml\n")
	if output, err := convertWithResolver([]byte(input), resolver); err == nil ||
		!strings.Contains(err.Error(), "resolves outside config localRoot") {
		t.Fatalf("unexpected result: %v\n%s", err, output)
	}

	input = applicationWithValueFiles(t, "        - values/loop-*.yaml\n")
	if output, err := convertWithResolver([]byte(input), resolver); err == nil ||
		!strings.Contains(err.Error(), "expand glob") {
		t.Fatalf("unexpected result: %v\n%s", err, output)
	}
}

func TestConvertExpandsStaticBuildEnvironment(t *testing.T) {
	input := readTestdata(t, "value-files/environment/application.yaml")
	resolver := testSourceResolver(t, testSource{
		repoURL: globRepositoryURL, targetRevision: "main", root: "/workspace/charts",
	})
	output, err := convertWithResolver([]byte(input), resolver)
	if err != nil {
		t.Fatal(err)
	}
	assertOrdered(t, string(output), []string{
		"env/static-app.yaml",
		"env/argocd.yaml",
		"env/production.yaml",
		"env/chart.yaml",
		"env/git@example.com:charts.git.yaml",
		"env/main-static-app.yaml",
		"env/$ARGOCD_APP_REVISION.yaml",
		"chart/$literal.yaml",
	})
}

func TestConvertBuildEnvironmentDefaultProject(t *testing.T) {
	input := strings.Replace(
		readTestdata(t, "value-files/environment/application.yaml"),
		"  project: production\n",
		"",
		1,
	)
	input = replaceValueFiles(input, "        - env/$ARGOCD_APP_PROJECT_NAME.yaml\n")
	output, err := convertWithResolver([]byte(input), testSourceResolver(t, testSource{
		repoURL: globRepositoryURL, targetRevision: "main", root: "/workspace/charts",
	}))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(output), "env/default.yaml") {
		t.Fatalf("default project was not expanded:\n%s", output)
	}
}

func TestConvertBuildEnvironmentAfterApplicationSetGeneration(t *testing.T) {
	output, err := convertWithResolver(
		[]byte(readTestdata(t, "value-files/environment/applicationset.yaml")),
		testSourceResolver(t, testSource{
			repoURL: globRepositoryURL, targetRevision: "main", root: "/workspace/charts",
		}),
	)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(output), "env/generated.yaml") {
		t.Fatalf("generated Application name was not expanded:\n%s", output)
	}
}

func TestConvertRejectsDynamicBuildEnvironment(t *testing.T) {
	for _, variable := range []string{
		"ARGOCD_APP_REVISION",
		"ARGOCD_APP_REVISION_SHORT",
		"ARGOCD_APP_REVISION_SHORT_8",
		"KUBE_VERSION",
		"KUBE_API_VERSIONS",
		"ARGOCD_UNKNOWN",
	} {
		t.Run(variable, func(t *testing.T) {
			input := applicationWithValueFiles(t, "        - env/$"+variable+".yaml\n")
			output, err := convertWithResolver([]byte(input), globFixtureResolver(t))
			if err == nil || !strings.Contains(err.Error(), variable) {
				t.Fatalf("unexpected result: %v\n%s", err, output)
			}
		})
	}
}

func TestConvertValueFileGlobErrors(t *testing.T) {
	tests := map[string]struct {
		valueFile string
		want      string
	}{
		"invalid": {
			valueFile: "values/[.yaml",
			want:      "invalid glob",
		},
		"outside repository": {
			valueFile: "../../outside/*.yaml",
			want:      "outside repository root",
		},
	}
	for name, test := range tests {
		t.Run(name, func(t *testing.T) {
			input := applicationWithValueFiles(t, "        - "+test.valueFile+"\n")
			output, err := convertWithResolver([]byte(input), globFixtureResolver(t))
			if err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("unexpected result: %v\n%s", err, output)
			}
		})
	}
}

func TestConvertDoesNotInspectExplicitValueFileOrTemplateLocalRoot(t *testing.T) {
	resolver := testSourceResolver(t, testSource{
		repoURL:        globRepositoryURL,
		targetRevision: "main",
		root:           `{{ requiredEnv "CHARTS_ROOT" }}`,
	})
	input := applicationWithValueFiles(t, "        - values/missing.yaml\n")
	output, err := convertWithResolver([]byte(input), resolver)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(
		string(output),
		`{{ requiredEnv "CHARTS_ROOT" }}/chart/values/missing.yaml`,
	) {
		t.Fatalf("unexpected output:\n%s", output)
	}
}

func globFixtureResolver(t *testing.T) *sourceResolver {
	t.Helper()
	return testSourceResolver(t, testSource{
		repoURL:        globRepositoryURL,
		targetRevision: "main",
		root:           "testdata/value-files/glob/repository",
	})
}

func applicationWithValueFiles(t *testing.T, valueFiles string) string {
	t.Helper()
	return replaceValueFiles(
		readTestdata(t, "value-files/glob/application.yaml"),
		valueFiles,
	)
}

func replaceValueFiles(input, replacement string) string {
	start := strings.Index(input, "      valueFiles:\n")
	if start < 0 {
		panic("fixture does not contain valueFiles")
	}
	end := len(input)
	for _, marker := range []string{"      ignoreMissingValueFiles:", "    other:"} {
		if index := strings.Index(input[start+len("      valueFiles:\n"):], marker); index >= 0 {
			index += start + len("      valueFiles:\n")
			if index < end {
				end = index
			}
		}
	}
	if end == len(input) {
		lines := strings.SplitAfter(input[start+len("      valueFiles:\n"):], "\n")
		offset := start + len("      valueFiles:\n")
		end = offset
		for _, line := range lines {
			if strings.HasPrefix(line, "        - ") || strings.TrimSpace(line) == "" {
				end += len(line)
				continue
			}
			break
		}
	}
	return input[:start] + "      valueFiles:\n" + replacement + input[end:]
}

func assertOrdered(t *testing.T, output string, ordered []string) {
	t.Helper()
	position := 0
	for _, fragment := range ordered {
		next := strings.Index(output[position:], fragment)
		if next < 0 {
			t.Fatalf("output does not contain %q in order:\n%s", fragment, output)
		}
		position += next + len(fragment)
	}
}
