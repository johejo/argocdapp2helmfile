package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestConvertDiagnosesKustomizationWithoutExplicitConfiguration(t *testing.T) {
	input := readTestdata(t, "kustomize/detection/application.yaml")
	config, err := parseConfig([]byte(readTestdata(t, "kustomize/detection/config.yaml")))
	if err != nil {
		t.Fatal(err)
	}
	tests := []struct {
		path     string
		detected string
	}{
		{path: "apps/yaml", detected: "kustomization.yaml"},
		{path: "apps/yml", detected: "kustomization.yml"},
		{path: "apps/capital", detected: "Kustomization"},
	}
	for _, test := range tests {
		t.Run(test.path, func(t *testing.T) {
			application := strings.Replace(input, "apps/yaml", test.path, 1)
			_, err := convertWithConfig([]byte(application), config)
			if err == nil {
				t.Fatal("convert succeeded")
			}
			want := `document 1: spec.source.path "` + test.path +
				`" appears to be a Kustomization because "` + test.detected +
				`" exists but Chart.yaml does not; add spec.source.kustomize: {}`
			if err.Error() != want {
				t.Fatalf("unexpected error:\n got: %s\nwant: %s", err, want)
			}
		})
	}
}

func TestConvertDoesNotDiagnoseValidOrUninspectableGitChart(t *testing.T) {
	input := readTestdata(t, "kustomize/detection/application.yaml")
	tests := []struct {
		name   string
		path   string
		config string
	}{
		{
			name:   "chart",
			path:   "apps/chart",
			config: "kustomize/detection/config.yaml",
		},
		{
			name:   "chart and Kustomization",
			path:   "apps/both",
			config: "kustomize/detection/config.yaml",
		},
		{
			name:   "neither",
			path:   "apps/empty",
			config: "kustomize/detection/config.yaml",
		},
		{
			name:   "templated localRoot",
			path:   "apps/yaml",
			config: "kustomize/detection/config-template.yaml",
		},
		{
			name:   "missing localRoot",
			path:   "apps/yaml",
			config: "kustomize/detection/config-missing.yaml",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			config, err := parseConfig([]byte(readTestdata(t, test.config)))
			if err != nil {
				t.Fatal(err)
			}
			application := strings.Replace(input, "apps/yaml", test.path, 1)
			output, err := convertWithConfig([]byte(application), config)
			if err != nil {
				t.Fatal(err)
			}
			if !strings.Contains(string(output), "chart source") {
				t.Fatalf("Git source was not converted as a chart:\n%s", output)
			}
		})
	}
}

func TestValidateGitChartSourceSkipsOutOfRootSymlink(t *testing.T) {
	root := filepath.Join(t.TempDir(), "repository")
	outside := filepath.Join(t.TempDir(), "outside")
	if err := os.MkdirAll(filepath.Join(root, "apps"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(outside, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(outside, "kustomization.yaml"), nil, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(root, "apps", "linked")); err != nil {
		t.Fatal(err)
	}

	err := validateGitChartSource(
		mappedSource{localRoot: root},
		applicationSource{Path: "apps/linked"},
		"spec.source",
	)
	if err != nil {
		t.Fatalf("out-of-root symlink produced a diagnostic: %v", err)
	}
}
