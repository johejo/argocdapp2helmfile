package main

import (
	"fmt"
	"strings"

	"github.com/goccy/go-yaml"
)

type kustomizeTransformer struct {
	APIVersion  string               `yaml:"apiVersion"`
	Kind        string               `yaml:"kind"`
	Metadata    kustomizeObjectMeta  `yaml:"metadata"`
	Labels      yaml.MapSlice        `yaml:"labels,omitempty"`
	Annotations yaml.MapSlice        `yaml:"annotations,omitempty"`
	FieldSpecs  []kustomizeFieldSpec `yaml:"fieldSpecs"`
}

type kustomizeObjectMeta struct {
	Name string `yaml:"name"`
}

type kustomizeFieldSpec struct {
	Path    string `yaml:"path"`
	Create  bool   `yaml:"create,omitempty"`
	Group   string `yaml:"group,omitempty"`
	Version string `yaml:"version,omitempty"`
	Kind    string `yaml:"kind,omitempty"`
}

func (options kustomizeOptions) transformers(
	environment map[string]string,
	field string,
) ([]any, error) {
	var result []any
	if len(options.commonLabels) != 0 {
		labels, err := expandKustomizeStringMap(
			options.commonLabels,
			environment,
			field+".commonLabels",
		)
		if err != nil {
			return nil, err
		}
		fieldSpecs := commonLabelFieldSpecs
		if options.labelWithoutSelector {
			fieldSpecs = resourceMetadataLabelFieldSpecs
			if options.labelIncludeTemplates {
				fieldSpecs = metadataLabelFieldSpecs
			}
		}
		result = append(result, kustomizeTransformer{
			APIVersion: "builtin",
			Kind:       "LabelTransformer",
			Metadata:   kustomizeObjectMeta{Name: "argocd-common-labels"},
			Labels:     labels,
			FieldSpecs: fieldSpecs,
		})
	}
	if len(options.commonAnnotations) != 0 {
		annotations := options.commonAnnotations
		if options.commonAnnotationsEnvsubst {
			var err error
			annotations, err = expandKustomizeStringMap(
				options.commonAnnotations,
				environment,
				field+".commonAnnotations",
			)
			if err != nil {
				return nil, err
			}
		}
		result = append(result, kustomizeTransformer{
			APIVersion:  "builtin",
			Kind:        "AnnotationsTransformer",
			Metadata:    kustomizeObjectMeta{Name: "argocd-common-annotations"},
			Annotations: annotations,
			FieldSpecs:  commonAnnotationFieldSpecs,
		})
	}
	return result, nil
}

func expandKustomizeStringMap(
	values yaml.MapSlice,
	environment map[string]string,
	field string,
) (yaml.MapSlice, error) {
	result := make(yaml.MapSlice, 0, len(values))
	for _, item := range values {
		key := item.Key.(string)
		expanded, err := expandKustomizeBuildEnvironment(item.Value.(string), environment)
		if err != nil {
			return nil, fmt.Errorf("%s.%s: %w", field, key, err)
		}
		result = append(result, yaml.MapItem{Key: key, Value: expanded})
	}
	return result, nil
}

func expandKustomizeBuildEnvironment(
	value string,
	environment map[string]string,
) (string, error) {
	var result strings.Builder
	for i := 0; i < len(value); {
		if value[i] != '$' {
			result.WriteByte(value[i])
			i++
			continue
		}
		if i+1 < len(value) && value[i+1] == '$' {
			result.WriteByte('$')
			i += 2
			continue
		}
		name, end, _ := buildEnvironmentVariable(value, i)
		if name == "" {
			result.WriteByte('$')
			i++
			continue
		}
		if replacement, exists := environment[name]; exists {
			result.WriteString(replacement)
			i = end
			continue
		}
		if isDynamicArgoCDBuildEnvironmentVariable(name) {
			return "", fmt.Errorf(
				"build environment variable %s cannot be determined statically",
				name,
			)
		}
		i = end
	}
	return result.String(), nil
}

func isDynamicArgoCDBuildEnvironmentVariable(name string) bool {
	switch name {
	case "ARGOCD_APP_REVISION",
		"ARGOCD_APP_REVISION_SHORT",
		"ARGOCD_APP_REVISION_SHORT_8",
		"KUBE_VERSION",
		"KUBE_API_VERSIONS":
		return true
	default:
		return false
	}
}

var resourceMetadataLabelFieldSpecs = []kustomizeFieldSpec{
	{Path: "metadata/labels", Create: true},
}

var metadataLabelFieldSpecs = []kustomizeFieldSpec{
	{Path: "metadata/labels", Create: true},
	{
		Path: "spec/template/metadata/labels", Create: true,
		Version: "v1", Kind: "ReplicationController",
	},
	{Path: "spec/template/metadata/labels", Create: true, Kind: "Deployment"},
	{Path: "spec/template/metadata/labels", Create: true, Kind: "ReplicaSet"},
	{Path: "spec/template/metadata/labels", Create: true, Kind: "DaemonSet"},
	{
		Path: "spec/template/metadata/labels", Create: true,
		Group: "apps", Kind: "StatefulSet",
	},
	{
		Path: "spec/volumeClaimTemplates[]/metadata/labels", Create: true,
		Group: "apps", Kind: "StatefulSet",
	},
	{
		Path: "spec/template/metadata/labels", Create: true,
		Group: "batch", Kind: "Job",
	},
	{
		Path: "spec/jobTemplate/metadata/labels", Create: true,
		Group: "batch", Kind: "CronJob",
	},
	{
		Path: "spec/jobTemplate/spec/template/metadata/labels", Create: true,
		Group: "batch", Kind: "CronJob",
	},
}

var commonLabelFieldSpecs = append([]kustomizeFieldSpec{
	{Path: "spec/selector", Create: true, Version: "v1", Kind: "Service"},
	{
		Path: "spec/selector", Create: true,
		Version: "v1", Kind: "ReplicationController",
	},
	{Path: "spec/selector/matchLabels", Create: true, Kind: "Deployment"},
	{
		Path: "spec/template/spec/affinity/podAffinity/" +
			"preferredDuringSchedulingIgnoredDuringExecution/podAffinityTerm/" +
			"labelSelector/matchLabels",
		Group: "apps", Kind: "Deployment",
	},
	{
		Path: "spec/template/spec/affinity/podAffinity/" +
			"requiredDuringSchedulingIgnoredDuringExecution/labelSelector/matchLabels",
		Group: "apps", Kind: "Deployment",
	},
	{
		Path: "spec/template/spec/affinity/podAntiAffinity/" +
			"preferredDuringSchedulingIgnoredDuringExecution/podAffinityTerm/" +
			"labelSelector/matchLabels",
		Group: "apps", Kind: "Deployment",
	},
	{
		Path: "spec/template/spec/affinity/podAntiAffinity/" +
			"requiredDuringSchedulingIgnoredDuringExecution/labelSelector/matchLabels",
		Group: "apps", Kind: "Deployment",
	},
	{
		Path:  "spec/template/spec/topologySpreadConstraints/labelSelector/matchLabels",
		Group: "apps", Kind: "Deployment",
	},
	{Path: "spec/selector/matchLabels", Create: true, Kind: "ReplicaSet"},
	{Path: "spec/selector/matchLabels", Create: true, Kind: "DaemonSet"},
	{
		Path: "spec/selector/matchLabels", Create: true,
		Group: "apps", Kind: "StatefulSet",
	},
	{
		Path: "spec/template/spec/affinity/podAffinity/" +
			"preferredDuringSchedulingIgnoredDuringExecution/podAffinityTerm/" +
			"labelSelector/matchLabels",
		Group: "apps", Kind: "StatefulSet",
	},
	{
		Path: "spec/template/spec/affinity/podAffinity/" +
			"requiredDuringSchedulingIgnoredDuringExecution/labelSelector/matchLabels",
		Group: "apps", Kind: "StatefulSet",
	},
	{
		Path: "spec/template/spec/affinity/podAntiAffinity/" +
			"preferredDuringSchedulingIgnoredDuringExecution/podAffinityTerm/" +
			"labelSelector/matchLabels",
		Group: "apps", Kind: "StatefulSet",
	},
	{
		Path: "spec/template/spec/affinity/podAntiAffinity/" +
			"requiredDuringSchedulingIgnoredDuringExecution/labelSelector/matchLabels",
		Group: "apps", Kind: "StatefulSet",
	},
	{
		Path:  "spec/template/spec/topologySpreadConstraints/labelSelector/matchLabels",
		Group: "apps", Kind: "StatefulSet",
	},
	{Path: "spec/selector/matchLabels", Group: "batch", Kind: "Job"},
	{
		Path:  "spec/jobTemplate/spec/selector/matchLabels",
		Group: "batch", Kind: "CronJob",
	},
	{
		Path:  "spec/selector/matchLabels",
		Group: "policy", Kind: "PodDisruptionBudget",
	},
	{
		Path:  "spec/podSelector/matchLabels",
		Group: "networking.k8s.io", Kind: "NetworkPolicy",
	},
	{
		Path:  "spec/ingress/from/podSelector/matchLabels",
		Group: "networking.k8s.io", Kind: "NetworkPolicy",
	},
	{
		Path:  "spec/egress/to/podSelector/matchLabels",
		Group: "networking.k8s.io", Kind: "NetworkPolicy",
	},
}, metadataLabelFieldSpecs...)

var commonAnnotationFieldSpecs = []kustomizeFieldSpec{
	{Path: "metadata/annotations", Create: true},
	{
		Path: "spec/template/metadata/annotations", Create: true,
		Version: "v1", Kind: "ReplicationController",
	},
	{Path: "spec/template/metadata/annotations", Create: true, Kind: "Deployment"},
	{Path: "spec/template/metadata/annotations", Create: true, Kind: "ReplicaSet"},
	{Path: "spec/template/metadata/annotations", Create: true, Kind: "DaemonSet"},
	{
		Path: "spec/template/metadata/annotations", Create: true,
		Group: "apps", Kind: "StatefulSet",
	},
	{
		Path: "spec/template/metadata/annotations", Create: true,
		Group: "batch", Kind: "Job",
	},
	{
		Path: "spec/jobTemplate/metadata/annotations", Create: true,
		Group: "batch", Kind: "CronJob",
	},
	{
		Path: "spec/jobTemplate/spec/template/metadata/annotations", Create: true,
		Group: "batch", Kind: "CronJob",
	},
}
