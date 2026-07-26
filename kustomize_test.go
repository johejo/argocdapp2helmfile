package main

import (
	"reflect"
	"slices"
	"strings"
	"testing"

	"github.com/goccy/go-yaml"
	"github.com/johejo/argocdapp2helmfile/internal/applicationmapping"
)

func TestConvertKustomizationGolden(t *testing.T) {
	for _, directory := range []string{
		"kustomize/empty",
		"kustomize/options",
		"kustomize/replicas",
		"kustomize/metadata",
		"kustomize/multi-source",
		"applicationset/kustomize",
	} {
		t.Run(directory, func(t *testing.T) {
			assertConvertFixture(t, directory, true)
		})
	}
}

func TestConvertRejectsInvalidKustomizeMetadataOptions(t *testing.T) {
	tests := map[string]string{
		"common-labels-key.yaml":           "spec.source.kustomize.commonLabels contains a non-string key",
		"common-labels-value.yaml":         "spec.source.kustomize.commonLabels.app.kubernetes.io/name must be a string",
		"common-annotations-sequence.yaml": "spec.source.kustomize.commonAnnotations must be a mapping",
		"boolean.yaml":                     "spec.source.kustomize.forceCommonLabels must be a boolean",
		"dynamic-environment.yaml":         "ARGOCD_APP_REVISION cannot be determined statically",
		"label-flags.yaml":                 "labelIncludeTemplates cannot be true when labelWithoutSelector is false",
	}
	config, err := parseConfig([]byte(readTestdata(t, "kustomize/metadata/config.yaml")))
	if err != nil {
		t.Fatal(err)
	}
	for fixture, want := range tests {
		t.Run(fixture, func(t *testing.T) {
			input := readTestdata(t, "kustomize/errors/"+fixture)
			_, err := convertWithConfig([]byte(input), config)
			assertErrorContains(t, err, want)
		})
	}
}

func TestKustomizeLabelFieldSpecs(t *testing.T) {
	tests := []struct {
		name             string
		withoutSelector  bool
		includeTemplates bool
		want             []kustomizeFieldSpec
	}{
		{name: "default", want: commonLabelFieldSpecs},
		{
			name: "resource metadata only", withoutSelector: true,
			want: resourceMetadataLabelFieldSpecs,
		},
		{
			name: "resource and templates", withoutSelector: true,
			includeTemplates: true, want: metadataLabelFieldSpecs,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			options := kustomizeOptions{
				commonLabels:          yamlMap("app", "api"),
				labelWithoutSelector:  test.withoutSelector,
				labelIncludeTemplates: test.includeTemplates,
			}
			transformers, err := options.transformers(nil, "spec.source.kustomize")
			if err != nil {
				t.Fatal(err)
			}
			got := transformers[0].(kustomizeTransformer).FieldSpecs
			if !reflect.DeepEqual(got, test.want) {
				t.Fatalf("field specs differ:\n%#v\nwant:\n%#v", got, test.want)
			}
		})
	}
}

func TestExpandKustomizeBuildEnvironment(t *testing.T) {
	environment := map[string]string{"ARGOCD_APP_NAME": "api"}
	got, err := expandKustomizeBuildEnvironment(
		"$ARGOCD_APP_NAME/${UNKNOWN}/$$LITERAL",
		environment,
	)
	if err != nil {
		t.Fatal(err)
	}
	if got != "api//$LITERAL" {
		t.Fatalf("unexpected expansion: %q", got)
	}

	for _, variable := range dynamicBuildEnvironmentVariables() {
		t.Run(variable, func(t *testing.T) {
			_, err := expandKustomizeBuildEnvironment("$"+variable, environment)
			assertErrorContains(t, err, variable)
		})
	}
}

func TestKustomizeAnnotationsDoNotExpandByDefault(t *testing.T) {
	options := kustomizeOptions{
		commonAnnotations: yamlMap("revision", "$ARGOCD_APP_REVISION"),
	}
	transformers, err := options.transformers(nil, "spec.source.kustomize")
	if err != nil {
		t.Fatal(err)
	}
	got := transformers[0].(kustomizeTransformer).Annotations[0].Value
	if got != "$ARGOCD_APP_REVISION" {
		t.Fatalf("annotation was unexpectedly expanded: %q", got)
	}
}

func yamlMap(key, value string) yaml.MapSlice {
	return yaml.MapSlice{{Key: key, Value: value}}
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
		"unknown option": {
			input: strings.Replace(
				source,
				"kustomize: {}",
				"kustomize:\n      nameprefix: edge-",
				1,
			),
			want: "spec.source.kustomize.nameprefix is not supported",
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
			assertErrorContains(t, err, test.want)
		})
	}
}

func TestParseKustomizeReplicas(t *testing.T) {
	tests := map[string]struct {
		input any
		want  []kustomizeReplica
	}{
		"integer count": {
			input: []any{yaml.MapSlice{{Key: "name", Value: "api"}, {Key: "count", Value: 3}}},
			want:  []kustomizeReplica{{Name: "api", Count: 3}},
		},
		"numeric string count": {
			input: []any{yaml.MapSlice{{Key: "name", Value: "api"}, {Key: "count", Value: "3"}}},
			want:  []kustomizeReplica{{Name: "api", Count: 3}},
		},
		"zero count": {
			input: []any{yaml.MapSlice{{Key: "name", Value: "api"}, {Key: "count", Value: 0}}},
			want:  []kustomizeReplica{{Name: "api", Count: 0}},
		},
		"input order retained": {
			input: []any{
				yaml.MapSlice{{Key: "name", Value: "web"}, {Key: "count", Value: 2}},
				yaml.MapSlice{{Key: "count", Value: 5}, {Key: "name", Value: "api"}},
			},
			want: []kustomizeReplica{{Name: "web", Count: 2}, {Name: "api", Count: 5}},
		},
		"empty sequence": {input: []any{}, want: nil},
	}
	for name, test := range tests {
		t.Run(name, func(t *testing.T) {
			got, err := parseKustomizeReplicas(test.input, "spec.source.kustomize.replicas")
			if err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(got, test.want) {
				t.Fatalf("parseKustomizeReplicas() = %#v, want %#v", got, test.want)
			}
		})
	}
}

func TestParseKustomizeReplicasRejectsInvalidEntries(t *testing.T) {
	tests := map[string]struct {
		input any
		want  string
	}{
		"not a sequence": {
			input: yaml.MapSlice{{Key: "name", Value: "api"}},
			want:  "spec.source.kustomize.replicas must be a sequence",
		},
		"element not a mapping": {
			input: []any{"api=3"},
			want:  "spec.source.kustomize.replicas[0] must be a mapping",
		},
		"unknown field": {
			input: []any{yaml.MapSlice{
				{Key: "name", Value: "api"},
				{Key: "count", Value: 3},
				{Key: "namespace", Value: "web"},
			}},
			want: "spec.source.kustomize.replicas[0].namespace is not supported",
		},
		"missing name": {
			input: []any{yaml.MapSlice{{Key: "count", Value: 3}}},
			want:  "spec.source.kustomize.replicas[0].name is required",
		},
		"missing count": {
			input: []any{yaml.MapSlice{{Key: "name", Value: "api"}}},
			want:  "spec.source.kustomize.replicas[0].count is required",
		},
		"name not a string": {
			input: []any{yaml.MapSlice{{Key: "name", Value: 1}, {Key: "count", Value: 3}}},
			want:  "spec.source.kustomize.replicas[0].name must be a string",
		},
		"count not an integer": {
			input: []any{yaml.MapSlice{{Key: "name", Value: "api"}, {Key: "count", Value: "two"}}},
			want:  `spec.source.kustomize.replicas[0].count must be an integer: "two"`,
		},
		"count is a mapping": {
			input: []any{yaml.MapSlice{
				{Key: "name", Value: "api"},
				{Key: "count", Value: yaml.MapSlice{}},
			}},
			want: "spec.source.kustomize.replicas[0].count must be an integer",
		},
	}
	for name, test := range tests {
		t.Run(name, func(t *testing.T) {
			_, err := parseKustomizeReplicas(test.input, "spec.source.kustomize.replicas")
			assertErrorContains(t, err, test.want)
		})
	}
}

func TestKustomizeReplicaTransformerTargetsRenamedResource(t *testing.T) {
	options := kustomizeOptions{
		namePrefix: "edge-",
		nameSuffix: "-prod",
		replicas:   []kustomizeReplica{{Name: "api", Count: 3}, {Name: "web", Count: 1}},
	}
	transformers, err := options.transformers(nil, "spec.source.kustomize")
	if err != nil {
		t.Fatal(err)
	}
	want := []any{
		kustomizeTransformer{
			APIVersion: "builtin",
			Kind:       "ReplicaCountTransformer",
			Metadata:   kustomizeObjectMeta{Name: "argocd-replicas-0"},
			Replica:    &kustomizeReplica{Name: "edge-api-prod", Count: 3},
			FieldSpecs: replicaCountFieldSpecs,
		},
		kustomizeTransformer{
			APIVersion: "builtin",
			Kind:       "ReplicaCountTransformer",
			Metadata:   kustomizeObjectMeta{Name: "argocd-replicas-1"},
			Replica:    &kustomizeReplica{Name: "edge-web-prod", Count: 1},
			FieldSpecs: replicaCountFieldSpecs,
		},
	}
	if !reflect.DeepEqual(transformers, want) {
		t.Fatalf("transformers() = %#v, want %#v", transformers, want)
	}
}

func TestConvertRejectsUnsupportedKustomizeOptions(t *testing.T) {
	source := readTestdata(t, "kustomize/empty/application.yaml")
	config, err := parseConfig([]byte(readTestdata(t, "kustomize/empty/config.yaml")))
	if err != nil {
		t.Fatal(err)
	}
	for _, option := range applicationmapping.KustomizeOptions() {
		if option.ValueKind != applicationmapping.KustomizeUnsupported {
			continue
		}
		t.Run(option.Name, func(t *testing.T) {
			// Rejection happens at catalog lookup, before any value is read.
			input := strings.Replace(
				source,
				"kustomize: {}",
				"kustomize:\n      "+option.Name+": []",
				1,
			)
			want := "spec.source.kustomize." + option.Name +
				" is not supported: " + option.Reason
			_, err := convertWithConfig([]byte(input), config)
			assertErrorContains(t, err, want)
		})
	}
}

func TestParseKustomizeOptionsAssignsEveryCatalogOption(t *testing.T) {
	items := kustomizeMap{
		{Key: "namePrefix", Value: "edge-"},
		{Key: "nameSuffix", Value: "-prod"},
		{Key: "namespace", Value: "manifests"},
		{Key: "images", Value: []any{"example/app:v2"}},
		{Key: "replicas", Value: []any{
			yaml.MapSlice{{Key: "name", Value: "api"}, {Key: "count", Value: 3}},
		}},
		{Key: "commonLabels", Value: yaml.MapSlice{{Key: "app", Value: "storefront"}}},
		{Key: "labelWithoutSelector", Value: true},
		{Key: "labelIncludeTemplates", Value: true},
		{Key: "commonAnnotations", Value: yaml.MapSlice{{Key: "team", Value: "platform"}}},
		{Key: "commonAnnotationsEnvsubst", Value: true},
		{Key: "forceCommonLabels", Value: true},
		{Key: "forceCommonAnnotations", Value: true},
	}
	want := kustomizeOptions{
		namePrefix:                "edge-",
		nameSuffix:                "-prod",
		namespace:                 "manifests",
		images:                    []kustomizeImage{{Name: "example/app", NewTag: "v2"}},
		replicas:                  []kustomizeReplica{{Name: "api", Count: 3}},
		commonLabels:              yaml.MapSlice{{Key: "app", Value: "storefront"}},
		labelWithoutSelector:      true,
		labelIncludeTemplates:     true,
		commonAnnotations:         yaml.MapSlice{{Key: "team", Value: "platform"}},
		commonAnnotationsEnvsubst: true,
	}

	got, err := parseKustomizeOptions(items, "spec.source.kustomize")
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("parseKustomizeOptions() = %#v, want %#v", got, want)
	}
	for _, option := range applicationmapping.KustomizeOptions() {
		if option.ValueKind == applicationmapping.KustomizeUnsupported {
			continue
		}
		if !slices.ContainsFunc(items, func(item yaml.MapItem) bool {
			return item.Key == option.Name
		}) {
			t.Errorf("this test does not cover Kustomize option %q", option.Name)
		}
	}
}

func TestConvertKustomizationRequiresConfig(t *testing.T) {
	input := readTestdata(t, "kustomize/empty/application.yaml")
	_, err := convert([]byte(input))
	assertErrorContains(t, err, "spec.source requires --config")

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
