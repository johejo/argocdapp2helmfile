package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func readTestdata(t *testing.T, name string) string {
	t.Helper()

	path := filepath.Join("testdata", filepath.FromSlash(name))
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read testdata %q: %v", path, err)
	}
	return string(data)
}

type testSource struct {
	repoURL        string
	targetRevision string
	root           string
}

func testSourceResolver(t *testing.T, sources ...testSource) *sourceResolver {
	t.Helper()
	var config strings.Builder
	config.WriteString("apiVersion: argocdapp2helmfile/v1alpha1\nkind: Config\nsources:\n")
	for _, source := range sources {
		fmt.Fprintf(
			&config,
			"  - repoURL: %q\n    targetRevision: %q\n    localRoot: %q\n",
			source.repoURL,
			source.targetRevision,
			source.root,
		)
	}
	parsed, err := parseConfig([]byte(config.String()))
	if err != nil {
		t.Fatal(err)
	}
	return parsed.sourceResolver
}

func convertWithResolver(input []byte, resolver *sourceResolver) ([]byte, error) {
	return convertWithConfig(input, &conversionConfig{sourceResolver: resolver})
}

func testConfig(t *testing.T, body string) *conversionConfig {
	t.Helper()
	config, err := parseConfig([]byte(
		"apiVersion: argocdapp2helmfile/v1alpha1\nkind: Config\n" + body,
	))
	if err != nil {
		t.Fatal(err)
	}
	return config
}

func testConvertApplicationSetFixture(t *testing.T, name string, withConfig bool) {
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

func writeTestConfig(t *testing.T, body string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "config.yaml")
	data := "apiVersion: argocdapp2helmfile/v1alpha1\nkind: Config\n" + body
	if err := os.WriteFile(path, []byte(data), 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}
