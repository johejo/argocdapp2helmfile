package main

import (
	"errors"
	"fmt"
	"strings"

	"github.com/goccy/go-yaml"
	"github.com/goccy/go-yaml/ast"
)

type inputOrigin struct {
	document int
	path     string
}

func (origin inputOrigin) String() string {
	if origin.path == "" {
		return fmt.Sprintf("document %d", origin.document)
	}
	return fmt.Sprintf("document %d: %s", origin.document, origin.path)
}

func (origin inputOrigin) wrap(err error) error {
	return fmt.Errorf("%s: %w", origin, err)
}

type generatedApplication struct {
	application application
	data        any
	path        string
	rollingStep *int
}

type applicationSetResource struct {
	APIVersion string `yaml:"apiVersion"`
	Kind       string `yaml:"kind"`
	Metadata   struct {
		Name string `yaml:"name"`
	} `yaml:"metadata"`
	Spec struct {
		GoTemplate        bool     `yaml:"goTemplate"`
		GoTemplateOptions []string `yaml:"goTemplateOptions"`
		// spec.applyNestedSelectors is accepted but ignored: selectors always
		// apply at every depth.
		Generators    []yaml.MapSlice `yaml:"generators"`
		Template      yaml.MapSlice   `yaml:"template"`
		TemplatePatch string          `yaml:"templatePatch"`
		Strategy      yaml.MapSlice   `yaml:"strategy"`
	} `yaml:"spec"`
}

func expandApplicationSet(node ast.Node, config *conversionConfig) ([]generatedApplication, error) {
	var appSet applicationSetResource
	if err := yaml.NodeToValue(node, &appSet, yaml.UseOrderedMap()); err != nil {
		return nil, fmt.Errorf("decode ApplicationSet: %w", err)
	}
	if appSet.APIVersion != "argoproj.io/v1alpha1" {
		return nil, fmt.Errorf("apiVersion must be %q", "argoproj.io/v1alpha1")
	}
	if strings.TrimSpace(appSet.Metadata.Name) == "" {
		return nil, errors.New("metadata.name is required")
	}
	if len(appSet.Spec.Generators) == 0 {
		return nil, errors.New("spec.generators must contain at least one generator")
	}
	if appSet.Spec.Template == nil {
		return nil, errors.New("spec.template is required")
	}
	strategy, err := parseApplicationSetStrategy(appSet.Spec.Strategy)
	if err != nil {
		return nil, err
	}
	renderer, err := newApplicationSetRenderer(
		appSet.Spec.GoTemplate,
		appSet.Spec.GoTemplateOptions,
	)
	if err != nil {
		return nil, err
	}

	var generated []generatedApplication
	for generatorIndex, rawGenerator := range appSet.Spec.Generators {
		field := fmt.Sprintf("spec.generators[%d]", generatorIndex)
		generator, err := parseApplicationSetGenerator(
			rawGenerator,
			field,
			config,
			renderer,
			nil,
			0,
			"",
		)
		if err != nil {
			return nil, err
		}
		mergedTemplate := mergeTemplate(generator.template, appSet.Spec.Template)
		for _, generatedParams := range generator.params {
			rendered, data, err := renderApplicationTemplate(
				mergedTemplate,
				appSet.Spec.TemplatePatch,
				generatedParams.params,
				renderer,
			)
			if err != nil {
				return nil, fmt.Errorf("%s: render template: %w", generatedParams.path, err)
			}
			generated = append(generated, generatedApplication{
				application: rendered,
				data:        data,
				path:        generatedParams.path,
			})
		}
	}
	if strategy != nil {
		if err := assignRollingSyncSteps(generated, strategy); err != nil {
			return nil, err
		}
	}
	return generated, nil
}
