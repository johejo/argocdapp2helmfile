package main

import (
	"strings"
	"testing"
)

func TestConvertKustomizationGolden(t *testing.T) {
	for _, directory := range []string{
		"kustomize/empty",
		"kustomize/options",
		"kustomize/multi-source",
		"applicationset/kustomize",
	} {
		t.Run(directory, func(t *testing.T) {
			config, err := parseConfig([]byte(readTestdata(t, directory+"/config.yaml")))
			if err != nil {
				t.Fatal(err)
			}
			output, err := convertWithConfig(
				[]byte(readTestdata(t, directory+"/application.yaml")),
				config,
			)
			if err != nil {
				t.Fatal(err)
			}
			want := readTestdata(t, directory+"/helmfile.yaml")
			if string(output) != want {
				t.Fatalf("unexpected output:\n%s\nwant:\n%s", output, want)
			}
		})
	}
}

func TestParseKustomizeImage(t *testing.T) {
	tests := []struct {
		input string
		want  kustomizeImage
	}{
		{
			input: "example/app:v2",
			want:  kustomizeImage{Name: "example/app", NewTag: "v2"},
		},
		{
			input: "example/app@sha256:abcdef",
			want:  kustomizeImage{Name: "example/app", Digest: "sha256:abcdef"},
		},
		{
			input: "old=registry.example.com:5000/team/app@sha256:abcdef",
			want: kustomizeImage{
				Name:    "old",
				NewName: "registry.example.com:5000/team/app",
				Digest:  "sha256:abcdef",
			},
		},
		{
			input: "old=registry.example.com/team/app:v2",
			want: kustomizeImage{
				Name:    "old",
				NewName: "registry.example.com/team/app",
				NewTag:  "v2",
			},
		},
		{
			input: "registry.example.com:5000/team/app",
			want:  kustomizeImage{Name: "registry.example.com:5000/team/app"},
		},
	}
	for _, test := range tests {
		t.Run(test.input, func(t *testing.T) {
			got, err := parseKustomizeImage(test.input)
			if err != nil {
				t.Fatal(err)
			}
			if got != test.want {
				t.Fatalf("parseKustomizeImage(%q) = %#v, want %#v", test.input, got, test.want)
			}
		})
	}
}

func TestParseKustomizeImageRejectsInvalidFormats(t *testing.T) {
	for _, input := range []string{
		"",
		"app:",
		"app@",
		"app@sha256",
		"app:tag@sha256:abcdef",
		"old=",
		"=replacement",
		"old=new=extra",
		"app@@sha256:abcdef",
		"registry//app:v2",
		"app name:v2",
	} {
		t.Run(input, func(t *testing.T) {
			if _, err := parseKustomizeImage(input); err == nil {
				t.Fatalf("parseKustomizeImage(%q) succeeded", input)
			}
		})
	}
}

func TestConvertRejectsInvalidKustomizations(t *testing.T) {
	source := readTestdata(t, "kustomize/empty/application.yaml")
	tests := map[string]struct {
		input string
		want  string
	}{
		"unsupported option": {
			input: strings.Replace(source, "kustomize: {}", "kustomize:\n      patches: []", 1),
			want:  "spec.source.kustomize.patches is not supported",
		},
		"chart mixed": {
			input: strings.Replace(source, "    path:", "    chart: app\n    path:", 1),
			want:  "spec.source.chart and spec.source.kustomize cannot both be set",
		},
		"helm mixed": {
			input: strings.Replace(source, "    kustomize:", "    helm: {}\n    kustomize:", 1),
			want:  "spec.source.helm and spec.source.kustomize cannot both be set",
		},
		"directory mixed": {
			input: strings.Replace(source, "    kustomize:", "    directory: {}\n    kustomize:", 1),
			want:  "spec.source.directory and spec.source.kustomize cannot both be set",
		},
		"plugin mixed": {
			input: strings.Replace(source, "    kustomize:", "    plugin: {}\n    kustomize:", 1),
			want:  "spec.source.plugin and spec.source.kustomize cannot both be set",
		},
		"unsafe path": {
			input: strings.Replace(
				source,
				"path: deploy/storefront",
				"path: ../deploy/storefront",
				1,
			),
			want: "must be a safe repository-relative directory",
		},
		"missing path": {
			input: strings.Replace(source, "    path: deploy/storefront\n", "", 1),
			want:  "spec.source.path is required for a Git repository",
		},
	}
	config, err := parseConfig([]byte(readTestdata(t, "kustomize/empty/config.yaml")))
	if err != nil {
		t.Fatal(err)
	}
	for name, test := range tests {
		t.Run(name, func(t *testing.T) {
			_, err := convertWithConfig([]byte(test.input), config)
			if err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("unexpected error: %v", err)
			}
		})
	}
}

func TestConvertKustomizationRequiresConfig(t *testing.T) {
	input := readTestdata(t, "kustomize/empty/application.yaml")
	_, err := convert([]byte(input))
	if err == nil || !strings.Contains(err.Error(), "spec.source requires --config") {
		t.Fatalf("unexpected error: %v", err)
	}

	_, err = convertWithConfig([]byte(input), testConfig(t, "sources: []\n"))
	if err == nil || !strings.Contains(
		err.Error(),
		`spec.source has no config source entry for repoURL "https://github.com/example/platform.git"`,
	) {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestConvertKustomizeMustBeMapping(t *testing.T) {
	input := readTestdata(t, "kustomize/empty/application.yaml")
	for _, replacement := range []string{"[]", "automatic", `"enabled"`} {
		t.Run(replacement, func(t *testing.T) {
			invalid := strings.Replace(input, "kustomize: {}", "kustomize: "+replacement, 1)
			if output, err := convert([]byte(invalid)); err == nil {
				t.Fatalf("convert succeeded with output:\n%s", output)
			}
		})
	}
}

func TestConvertNullKustomizeDoesNotSelectKustomization(t *testing.T) {
	input := strings.Replace(
		readTestdata(t, "kustomize/empty/application.yaml"),
		"kustomize: {}",
		"kustomize: null",
		1,
	)
	config, err := parseConfig([]byte(readTestdata(t, "kustomize/empty/config.yaml")))
	if err != nil {
		t.Fatal(err)
	}
	output, err := convertWithConfig([]byte(input), config)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(output), "kustomization source") ||
		!strings.Contains(string(output), "chart source") {
		t.Fatalf("null kustomize selected Kustomization conversion:\n%s", output)
	}
}

func TestConvertKustomizationDoesNotParticipateInSkipCRDs(t *testing.T) {
	helm := minimalApplication("    helm:\n      skipCrds: true\n")
	kustomization := strings.NewReplacer(
		"name: storefront", "name: manifests",
		"namespace: storefront-system", "namespace: workloads",
	).Replace(readTestdata(t, "kustomize/empty/application.yaml"))
	config, err := parseConfig([]byte(readTestdata(t, "kustomize/empty/config.yaml")))
	if err != nil {
		t.Fatal(err)
	}
	output, err := convertWithConfig([]byte(helm+"---\n"+kustomization), config)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(output), "helmDefaults:\n  skipCRDs: true\n") {
		t.Fatalf("Helm skipCRDs was not retained:\n%s", output)
	}
}

func TestConvertGitChartWithoutKustomizeRemainsHelm(t *testing.T) {
	repoURL := "git@github.com:example/platform.git"
	resolver := testSourceResolver(t, testSource{
		repoURL: repoURL, targetRevision: "main", root: "checkouts/platform",
	})
	output, err := convertWithResolver(
		[]byte(gitApplication(repoURL, "deploy/app", "main", "")),
		resolver,
	)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(output), "kustomization source") ||
		!strings.Contains(string(output), "chart source") {
		t.Fatalf("Git chart behavior changed:\n%s", output)
	}
}
