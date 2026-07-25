package main

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/goccy/go-yaml"
)

func TestE2ERender(t *testing.T) {
	if os.Getenv("ARGOCDAPP2HELMFILE_E2E") != "1" {
		t.Skip("set ARGOCDAPP2HELMFILE_E2E=1 to run the Helmfile rendering E2E test")
	}

	cliPaths := make(map[string]string, 3)
	for _, name := range []string{"helmfile", "helm", "kustomize"} {
		path, err := exec.LookPath(name)
		if err != nil {
			t.Fatalf("required CLI %q was not found on PATH: %v", name, err)
		}
		absolutePath, err := filepath.Abs(path)
		if err != nil {
			t.Fatalf("resolve absolute path for %q at %q: %v", name, path, err)
		}
		cliPaths[name] = absolutePath
	}

	application := readE2EFixture(t, "application.yaml")
	configPath := filepath.Join("testdata", "e2e", "config.yaml")
	var helmfileState bytes.Buffer
	var conversionStderr bytes.Buffer
	if code := run(
		[]string{"--config", configPath},
		bytes.NewReader(application),
		&helmfileState,
		&conversionStderr,
	); code != 0 {
		t.Fatalf(
			"convert fixtures (exit %d):\nstdout:\n%s\nstderr:\n%s",
			code,
			helmfileState.String(),
			conversionStderr.String(),
		)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	command := exec.CommandContext(
		ctx,
		cliPaths["helmfile"],
		"-f", "-",
		"--helm-binary", cliPaths["helm"],
		"--kustomize-binary", cliPaths["kustomize"],
		"--skip-deps",
		"--skip-refresh",
		"--no-color",
		"template",
	)
	command.Stdin = bytes.NewReader(helmfileState.Bytes())
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	command.Stdout = &stdout
	command.Stderr = &stderr

	helmHome := t.TempDir()
	command.Env = append(
		os.Environ(),
		"HELM_CACHE_HOME="+filepath.Join(helmHome, "cache"),
		"HELM_CONFIG_HOME="+filepath.Join(helmHome, "config"),
		"HELM_DATA_HOME="+filepath.Join(helmHome, "data"),
	)

	err := command.Run()
	if ctx.Err() != nil {
		err = fmt.Errorf("timed out after 30s: %w", ctx.Err())
	}
	if err != nil {
		t.Fatalf(
			"helmfile template: %v\nCLIs:\n%s\nstdout:\n%s\nstderr:\n%s",
			err,
			formatCLIPaths(cliPaths),
			stdout.String(),
			stderr.String(),
		)
	}

	actual := decodeYAMLDocuments(t, "helmfile output", stdout.Bytes())
	expected := decodeYAMLDocuments(t, "expected fixture", readE2EFixture(t, "expected.yaml"))
	if !reflect.DeepEqual(actual, expected) {
		t.Fatalf(
			"rendered manifests differ\nCLIs:\n%s\nactual:\n%s\nexpected:\n%s",
			formatCLIPaths(cliPaths),
			marshalYAMLDocuments(t, actual),
			marshalYAMLDocuments(t, expected),
		)
	}
}

func readE2EFixture(t *testing.T, name string) []byte {
	t.Helper()
	return []byte(readTestdata(t, "e2e/"+name))
}

func decodeYAMLDocuments(t *testing.T, description string, input []byte) []any {
	t.Helper()
	decoder := yaml.NewDecoder(bytes.NewReader(input))
	var documents []any
	for {
		var document any
		err := decoder.Decode(&document)
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			t.Fatalf("decode %s as YAML document %d: %v", description, len(documents)+1, err)
		}
		if document != nil {
			documents = append(documents, document)
		}
	}
	return documents
}

func marshalYAMLDocuments(t *testing.T, documents []any) string {
	t.Helper()
	var result strings.Builder
	encoder := yaml.NewEncoder(&result)
	for _, document := range documents {
		if err := encoder.Encode(document); err != nil {
			t.Fatalf("encode YAML document for diagnostic: %v", err)
		}
	}
	return result.String()
}

func formatCLIPaths(paths map[string]string) string {
	return fmt.Sprintf(
		"  helmfile: %s\n  helm: %s\n  kustomize: %s",
		paths["helmfile"],
		paths["helm"],
		paths["kustomize"],
	)
}
