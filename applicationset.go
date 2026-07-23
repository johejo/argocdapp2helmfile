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
	if origin.path == "" {
		return fmt.Errorf("document %d: %w", origin.document, err)
	}
	return fmt.Errorf("document %d: %s: %w", origin.document, origin.path, err)
}

type generatedApplication struct {
	application application
	data        any
	path        string
}

type applicationSetResource struct {
	APIVersion string `yaml:"apiVersion"`
	Kind       string `yaml:"kind"`
	Metadata   struct {
		Name string `yaml:"name"`
	} `yaml:"metadata"`
	Spec struct {
		GoTemplate        bool            `yaml:"goTemplate"`
		GoTemplateOptions []string        `yaml:"goTemplateOptions"`
		Generators        []yaml.MapSlice `yaml:"generators"`
		Template          yaml.MapSlice   `yaml:"template"`
		TemplatePatch     string          `yaml:"templatePatch"`
	} `yaml:"spec"`
}

func expandApplicationSet(node ast.Node) ([]generatedApplication, error) {
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
	if !appSet.Spec.GoTemplate {
		return nil, errors.New("spec.goTemplate must be true; fasttemplate is not supported")
	}
	if len(appSet.Spec.Generators) == 0 {
		return nil, errors.New("spec.generators must contain at least one List generator")
	}
	if appSet.Spec.Template == nil {
		return nil, errors.New("spec.template is required")
	}
	renderer, err := newApplicationSetTemplate(appSet.Spec.GoTemplateOptions)
	if err != nil {
		return nil, err
	}

	var generated []generatedApplication
	for generatorIndex, rawGenerator := range appSet.Spec.Generators {
		field := fmt.Sprintf("spec.generators[%d]", generatorIndex)
		list, selector, err := parseListGenerator(rawGenerator, field)
		if err != nil {
			return nil, err
		}
		mergedTemplate := mergeTemplate(list.template, appSet.Spec.Template)
		elements := append([]any(nil), list.elements...)
		yamlElements, err := parseElementsYAML(list.elementsYAML, field+".list.elementsYaml")
		if err != nil {
			return nil, err
		}
		elements = append(elements, yamlElements...)

		for elementIndex, rawElement := range elements {
			elementField := fmt.Sprintf("%s.list.elements[%d]", field, elementIndex)
			if elementIndex >= len(list.elements) {
				elementField = fmt.Sprintf(
					"%s.list.elementsYaml[%d]",
					field,
					elementIndex-len(list.elements),
				)
			}
			params, err := normalizeStringMap(rawElement)
			if err != nil {
				return nil, fmt.Errorf("%s: must be a mapping: %w", elementField, err)
			}
			if selector != nil {
				matches, err := selector.matches(params)
				if err != nil {
					return nil, fmt.Errorf("%s.selector: %w", field, err)
				}
				if !matches {
					continue
				}
			}
			rendered, data, err := renderApplicationTemplate(
				mergedTemplate,
				appSet.Spec.TemplatePatch,
				params,
				renderer,
			)
			if err != nil {
				return nil, fmt.Errorf("%s: render template: %w", elementField, err)
			}
			generated = append(generated, generatedApplication{
				application: rendered,
				data:        data,
				path:        elementField,
			})
		}
	}
	return generated, nil
}
