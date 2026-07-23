package main

import (
	"errors"
	"fmt"

	"github.com/goccy/go-yaml"
	"github.com/goccy/go-yaml/parser"
)

type applicationInput struct {
	application application
	data        any
	origin      inputOrigin
}

func decodeApplicationInputs(input []byte) ([]applicationInput, error) {
	file, err := parser.ParseBytes(input, 0)
	if err != nil {
		return nil, fmt.Errorf(
			"document %d: decode Application: %w",
			documentNumberForError(input, err),
			err,
		)
	}

	var applications []applicationInput
	for i, document := range file.Docs {
		documentNumber := i + 1
		if document.Body == nil {
			return nil, fmt.Errorf(
				"document %d: document must contain an Application or ApplicationSet",
				documentNumber,
			)
		}

		var header struct {
			Kind string `yaml:"kind"`
		}
		if err := yaml.NodeToValue(document.Body, &header); err != nil {
			return nil, fmt.Errorf("document %d: decode resource: %w", documentNumber, err)
		}
		switch header.Kind {
		case "Application":
			var app application
			if err := yaml.NodeToValue(document.Body, &app, yaml.UseOrderedMap()); err != nil {
				return nil, fmt.Errorf("document %d: decode Application: %w", documentNumber, err)
			}
			var data any
			if err := yaml.NodeToValue(document.Body, &data, yaml.UseOrderedMap()); err != nil {
				return nil, fmt.Errorf("document %d: decode Application data: %w", documentNumber, err)
			}
			data, err = normalizeTemplateValue(data)
			if err != nil {
				return nil, fmt.Errorf("document %d: normalize Application data: %w", documentNumber, err)
			}
			applications = append(applications, applicationInput{
				application: app,
				data:        data,
				origin:      inputOrigin{document: documentNumber},
			})
		case "ApplicationSet":
			generated, err := expandApplicationSet(document.Body)
			if err != nil {
				return nil, fmt.Errorf("document %d: %w", documentNumber, err)
			}
			for _, item := range generated {
				applications = append(applications, applicationInput{
					application: item.application,
					data:        item.data,
					origin: inputOrigin{
						document: documentNumber,
						path:     item.path,
					},
				})
			}
		default:
			return nil, fmt.Errorf(
				"document %d: kind must be \"Application\" or \"ApplicationSet\"",
				documentNumber,
			)
		}
	}
	if len(file.Docs) == 0 {
		return nil, errors.New(
			"document 1: document must contain an Application or ApplicationSet",
		)
	}
	return applications, nil
}
