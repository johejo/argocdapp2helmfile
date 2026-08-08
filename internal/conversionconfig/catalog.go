// Package conversionconfig defines the conversion configuration resource and
// the catalog used to validate and document it.
package conversionconfig

import (
	"errors"
	"fmt"
	"strings"

	"github.com/goccy/go-yaml/ast"
)

const (
	APIVersion = "argocdapp2helmfile/v1alpha1"
	Kind       = "Config"
)

type Resource struct {
	APIVersion    string             `yaml:"apiVersion"`
	Kind          string             `yaml:"kind"`
	Destinations  []Destination      `yaml:"destinations,omitempty"`
	Clusters      []Cluster          `yaml:"clusters,omitempty"`
	Sources       []Source           `yaml:"sources,omitempty"`
	ReleaseLabels []ReleaseLabelRule `yaml:"releaseLabels,omitempty"`
}

type Source struct {
	RepoURL        string `yaml:"repoURL"`
	TargetRevision string `yaml:"targetRevision"`
	LocalRoot      string `yaml:"localRoot"`
}

type Destination struct {
	Name        string `yaml:"name,omitempty"`
	Server      string `yaml:"server,omitempty"`
	KubeContext string `yaml:"kubeContext"`
}

type Cluster struct {
	Name        string          `yaml:"name"`
	Server      string          `yaml:"server"`
	KubeContext string          `yaml:"kubeContext"`
	Project     string          `yaml:"project,omitempty"`
	Labels      StrictStringMap `yaml:"labels,omitempty"`
	Annotations StrictStringMap `yaml:"annotations,omitempty"`
}

type StrictStringMap map[string]string

func (mapping *StrictStringMap) UnmarshalYAML(node ast.Node) error {
	if node.Type() == ast.NullType {
		*mapping = nil
		return nil
	}
	root, ok := node.(*ast.MappingNode)
	if !ok {
		return errors.New("must be a mapping")
	}
	result := make(StrictStringMap, len(root.Values))
	for _, item := range root.Values {
		key, keyOK := item.Key.(*ast.StringNode)
		value, valueOK := item.Value.(*ast.StringNode)
		if !keyOK || !valueOK {
			return errors.New("keys and values must be strings")
		}
		result[key.Value] = value.Value
	}
	*mapping = result
	return nil
}

type ReleaseLabelRule struct {
	Name  string `yaml:"name"`
	Query string `yaml:"query"`
}

type Field struct {
	Path        string
	Required    string
	Description string
}

var fields = []Field{
	{"apiVersion", "yes", "Must be `" + APIVersion + "`."},
	{"kind", "yes", "Must be `" + Kind + "`."},
	{"destinations", "no", "Literal Argo CD destination-to-kube-context mappings."},
	{"destinations[].name", "one selector", "Exact `spec.destination.name` to match."},
	{"destinations[].server", "one selector", "Exact `spec.destination.server` to match."},
	{"destinations[].kubeContext", "yes", "Helmfile kube context to emit unchanged."},
	{"clusters", "no", "Offline cluster inventory used by destinations and ApplicationSets."},
	{"clusters[].name", "yes", "Unique cluster name and destination selector."},
	{"clusters[].server", "yes", "Unique cluster server and destination selector."},
	{"clusters[].kubeContext", "yes", "Helmfile kube context to emit unchanged."},
	{"clusters[].project", "no", "Argo CD project exposed to a clusters generator."},
	{"clusters[].labels", "no", "String labels exposed to selectors and templates."},
	{"clusters[].annotations", "no", "String annotations exposed to templates."},
	{"sources", "no", "Git source-to-local-root mappings."},
	{"sources[].repoURL", "yes", "Exact Application or generator repository URL."},
	{"sources[].targetRevision", "yes", "Normalized revision paired with `repoURL`."},
	{"sources[].localRoot", "yes", "Local path or Helmfile template expression."},
	{"releaseLabels", "no", "Ordered jq projections into Helmfile release labels."},
	{"releaseLabels[].name", "yes", "Unique Helmfile label name."},
	{"releaseLabels[].query", "yes", "jq expression evaluated against the final Application."},
}

func Fields() []Field {
	return fields
}

func ValidateSources(entries []Source) error {
	seen := make(map[[2]string]struct{}, len(entries))
	for i, entry := range entries {
		field := fmt.Sprintf("config sources[%d]", i)
		if strings.TrimSpace(entry.RepoURL) == "" {
			return fmt.Errorf("%s.repoURL is required", field)
		}
		if strings.TrimSpace(entry.TargetRevision) == "" {
			return fmt.Errorf("%s.targetRevision is required", field)
		}
		if strings.TrimSpace(entry.LocalRoot) == "" {
			return fmt.Errorf("%s.localRoot is required", field)
		}
		key := [2]string{entry.RepoURL, entry.TargetRevision}
		if _, exists := seen[key]; exists {
			return fmt.Errorf(
				"%s duplicates repoURL %q at targetRevision %q",
				field,
				entry.RepoURL,
				entry.TargetRevision,
			)
		}
		seen[key] = struct{}{}
	}
	return nil
}

func ValidateDestinations(entries []Destination, clusters []Cluster) error {
	seen := make(map[[2]string]struct{}, len(entries)+len(clusters)*2)
	add := func(field, kind, value string) error {
		key := [2]string{kind, value}
		if _, exists := seen[key]; exists {
			return fmt.Errorf("%s duplicates %s %q", field, kind, value)
		}
		seen[key] = struct{}{}
		return nil
	}
	for i, entry := range entries {
		field := fmt.Sprintf("config destinations[%d]", i)
		hasName := strings.TrimSpace(entry.Name) != ""
		hasServer := strings.TrimSpace(entry.Server) != ""
		if hasName == hasServer {
			return fmt.Errorf("%s must set exactly one of name or server", field)
		}
		if strings.TrimSpace(entry.KubeContext) == "" {
			return fmt.Errorf("%s.kubeContext is required", field)
		}
		if hasName {
			if err := add(field, "name", entry.Name); err != nil {
				return err
			}
		} else if err := add(field, "server", entry.Server); err != nil {
			return err
		}
	}
	for i, cluster := range clusters {
		field := fmt.Sprintf("config clusters[%d]", i)
		if strings.TrimSpace(cluster.Name) == "" {
			return fmt.Errorf("%s.name is required", field)
		}
		if strings.TrimSpace(cluster.Server) == "" {
			return fmt.Errorf("%s.server is required", field)
		}
		if strings.TrimSpace(cluster.KubeContext) == "" {
			return fmt.Errorf("%s.kubeContext is required", field)
		}
		if err := add(field, "name", cluster.Name); err != nil {
			return err
		}
		if err := add(field, "server", cluster.Server); err != nil {
			return err
		}
	}
	return nil
}

func ValidateReleaseLabels(entries []ReleaseLabelRule) error {
	seen := make(map[string]struct{}, len(entries))
	for i, entry := range entries {
		field := fmt.Sprintf("config releaseLabels[%d]", i)
		if strings.TrimSpace(entry.Name) == "" {
			return fmt.Errorf("%s.name is required", field)
		}
		if strings.TrimSpace(entry.Query) == "" {
			return fmt.Errorf("%s.query is required", field)
		}
		if _, exists := seen[entry.Name]; exists {
			return fmt.Errorf("%s duplicates name %q", field, entry.Name)
		}
		seen[entry.Name] = struct{}{}
	}
	return nil
}
