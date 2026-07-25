package main

import (
	"strings"
	"testing"
)

func TestConvertApplicationSetCluster(t *testing.T) {
	testConvertApplicationSetFixture(t, "cluster", true)
}

func TestConvertApplicationSetLegacyCluster(t *testing.T) {
	testConvertApplicationSetFixture(t, "legacy-cluster", true)
}

func TestConvertApplicationSetClusterMatrix(t *testing.T) {
	testConvertApplicationSetFixture(t, "cluster-matrix", true)
}

func TestConvertApplicationSetClusterMerge(t *testing.T) {
	testConvertApplicationSetFixture(t, "cluster-merge", true)
}

func TestConvertApplicationSetClusterSelectors(t *testing.T) {
	testConvertApplicationSetFixture(t, "cluster-selectors", true)
}

func TestClusterGeneratorOmitsEmptyMetadata(t *testing.T) {
	params := clusterGeneratorParams(clusterConfigEntry{
		Name:    "local",
		Server:  "https://kubernetes.default.svc",
		Project: "",
	}, true)
	if _, exists := params["metadata"]; exists {
		t.Fatalf("empty metadata was included: %#v", params)
	}
	if params["project"] != "" {
		t.Fatalf("project = %#v, want empty string", params["project"])
	}
}

func TestClusterGeneratorRequiresConfigAndRejectsFlatList(t *testing.T) {
	input := readTestdata(t, "applicationset/cluster-errors/application.yaml")
	if _, err := convert([]byte(input)); err == nil ||
		!strings.Contains(err.Error(), "spec.generators[0].clusters requires --config") {
		t.Fatalf("unexpected missing-config error: %v", err)
	}

	config := testConfig(t, "clusters: []\n")
	flatList := readTestdata(t, "applicationset/cluster-errors/flat-list.yaml")
	if _, err := convertWithConfig([]byte(flatList), config); err == nil ||
		!strings.Contains(err.Error(), "flatList: true is not supported") {
		t.Fatalf("unexpected flatList error: %v", err)
	}
}

func TestClusterGeneratorAllowsEmptySnapshot(t *testing.T) {
	input := readTestdata(t, "applicationset/cluster-errors/application.yaml")
	output, err := convertWithConfig([]byte(input), testConfig(t, "clusters: []\n"))
	if err != nil {
		t.Fatal(err)
	}
	if string(output) != "helmDefaults:\n  createNamespace: false\nreleases: []\n" {
		t.Fatalf("unexpected output:\n%s", output)
	}
}

func TestClusterGeneratorReportsEntryOriginAndUnknownFields(t *testing.T) {
	config, err := parseConfig([]byte(readTestdata(t, "applicationset/cluster/config.yaml")))
	if err != nil {
		t.Fatal(err)
	}
	render := readTestdata(t, "applicationset/cluster-errors/render.yaml")
	if _, err := convertWithConfig([]byte(render), config); err == nil ||
		!strings.Contains(err.Error(), `spec.generators[0].clusters["Prod_US"]`) {
		t.Fatalf("unexpected render error: %v", err)
	}

	unknown := readTestdata(t, "applicationset/cluster-errors/unknown-field.yaml")
	if _, err := convertWithConfig([]byte(unknown), config); err == nil ||
		!strings.Contains(err.Error(), "clusters.secretType is not supported") {
		t.Fatalf("unexpected unknown-field error: %v", err)
	}
}

func TestReadmeClusterSnapshotExampleDoesNotProjectCredentials(t *testing.T) {
	readme := readTestdata(t, "../README.md")
	for _, required := range []string{
		"@base64d",
		`with_entries(select(.key == "example.com/owner"))`,
		"kubectl.kubernetes.io/last-applied-configuration",
		"default local cluster",
	} {
		if !strings.Contains(readme, required) {
			t.Errorf("README snapshot guidance is missing %q", required)
		}
	}
	if strings.Contains(readme, ".data.config") {
		t.Error("README snapshot example projects the Cluster Secret config field")
	}
}
