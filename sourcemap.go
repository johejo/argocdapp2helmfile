package main

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"strings"

	"github.com/goccy/go-yaml"
)

const (
	sourceMapAPIVersion = "argocdapp2helmfile/v1alpha1"
	sourceMapKind       = "SourceMap"
)

type sourceMap struct {
	APIVersion string           `yaml:"apiVersion"`
	Kind       string           `yaml:"kind"`
	Sources    []sourceMapEntry `yaml:"sources"`
}

type sourceMapEntry struct {
	RepoURL        string `yaml:"repoURL"`
	TargetRevision string `yaml:"targetRevision"`
	Root           string `yaml:"root"`
}

type sourceKey struct {
	repoURL        string
	targetRevision string
}

type mappedSource struct {
	root string
}

type sourceResolver struct {
	entries map[sourceKey]sourceMapEntry
}

func parseSourceMap(input []byte) (*sourceResolver, error) {
	decoder := yaml.NewDecoder(bytes.NewReader(input), yaml.DisallowUnknownField())
	var config sourceMap
	if err := decoder.Decode(&config); err != nil {
		return nil, fmt.Errorf("decode source map: %w", err)
	}
	var extra any
	if err := decoder.Decode(&extra); !errors.Is(err, io.EOF) {
		if err == nil {
			return nil, errors.New("source map must contain exactly one YAML document")
		}
		return nil, fmt.Errorf("decode additional source map document: %w", err)
	}
	if config.APIVersion != sourceMapAPIVersion {
		return nil, fmt.Errorf("source map apiVersion must be %q", sourceMapAPIVersion)
	}
	if config.Kind != sourceMapKind {
		return nil, fmt.Errorf("source map kind must be %q", sourceMapKind)
	}

	resolver := &sourceResolver{
		entries: make(map[sourceKey]sourceMapEntry, len(config.Sources)),
	}
	for i, entry := range config.Sources {
		field := fmt.Sprintf("source map sources[%d]", i)
		if strings.TrimSpace(entry.RepoURL) == "" {
			return nil, fmt.Errorf("%s.repoURL is required", field)
		}
		if strings.TrimSpace(entry.TargetRevision) == "" {
			return nil, fmt.Errorf("%s.targetRevision is required", field)
		}
		if strings.TrimSpace(entry.Root) == "" {
			return nil, fmt.Errorf("%s.root is required", field)
		}
		key := sourceKey{repoURL: entry.RepoURL, targetRevision: entry.TargetRevision}
		if _, exists := resolver.entries[key]; exists {
			return nil, fmt.Errorf("%s duplicates repoURL %q at targetRevision %q", field, entry.RepoURL, entry.TargetRevision)
		}
		resolver.entries[key] = entry
	}
	return resolver, nil
}

func (resolver *sourceResolver) resolve(source applicationSource, field string) (mappedSource, error) {
	if resolver == nil {
		return mappedSource{}, fmt.Errorf("%s requires --source-map", field)
	}
	key := sourceKey{repoURL: source.RepoURL, targetRevision: source.TargetRevision}
	entry, exists := resolver.entries[key]
	if !exists {
		return mappedSource{}, fmt.Errorf(
			"%s has no source map entry for repoURL %q at targetRevision %q",
			field, source.RepoURL, source.TargetRevision,
		)
	}
	return mappedSource{root: entry.Root}, nil
}

func joinSourcePath(root, relative string) string {
	if relative == "." || relative == "" {
		return root
	}
	if strings.HasSuffix(root, "/") {
		return root + relative
	}
	return root + "/" + relative
}
