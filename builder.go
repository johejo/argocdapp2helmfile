package main

import (
	"bytes"
	"fmt"

	"github.com/goccy/go-yaml"
)

type helmfileBuilder struct {
	result                    helmfile
	provenanceComments        []string
	repositoryAliases         map[string]string
	repositoryPassCredentials map[string]bool
	repositoryOrigins         map[string]inputOrigin
	usedRepositoryAliases     map[string]struct{}
	releaseOrigins            map[string]inputOrigin
	sharedSkipCRDs            bool
	sharedSkipCRDsOrigin      inputOrigin
	hasApplications           bool
	resolver                  *sourceResolver
	destinationResolver       *destinationResolver
	projector                 *releaseLabelProjector
}

func newHelmfileBuilder(config *conversionConfig) *helmfileBuilder {
	builder := &helmfileBuilder{
		repositoryAliases:         make(map[string]string),
		repositoryPassCredentials: make(map[string]bool),
		repositoryOrigins:         make(map[string]inputOrigin),
		usedRepositoryAliases:     make(map[string]struct{}),
		releaseOrigins:            make(map[string]inputOrigin),
	}
	if config != nil {
		builder.resolver = config.sourceResolver
		builder.destinationResolver = config.destinationResolver
		builder.projector = config.labelProjector
	}
	return builder
}

func (builder *helmfileBuilder) add(item applicationInput) error {
	converted, err := convertApplication(
		item.application,
		item.origin.document,
		builder.resolver,
		builder.destinationResolver,
	)
	if err != nil {
		return item.origin.wrap(err)
	}
	converted.release.Labels, err = builder.projector.project(item.data)
	if err != nil {
		return item.origin.wrap(err)
	}

	if previousOrigin, exists := builder.releaseOrigins[converted.release.Name]; exists {
		if item.origin.path == "" && previousOrigin.path == "" {
			return fmt.Errorf(
				"document %d: release name %q duplicates document %d",
				item.origin.document,
				converted.release.Name,
				previousOrigin.document,
			)
		}
		return item.origin.wrap(fmt.Errorf(
			"release name %q duplicates %s",
			converted.release.Name,
			previousOrigin,
		))
	}
	builder.releaseOrigins[converted.release.Name] = item.origin

	if !builder.hasApplications {
		builder.sharedSkipCRDs = converted.skipCRDs
		builder.sharedSkipCRDsOrigin = item.origin
		builder.hasApplications = true
	} else if converted.skipCRDs != builder.sharedSkipCRDs {
		if item.origin.path == "" &&
			builder.sharedSkipCRDsOrigin.path == "" &&
			builder.sharedSkipCRDsOrigin.document == 1 {
			return fmt.Errorf(
				"document %d: spec.source.helm.skipCrds conflicts with document 1",
				item.origin.document,
			)
		}
		return item.origin.wrap(fmt.Errorf(
			"spec.source.helm.skipCrds conflicts with %s",
			builder.sharedSkipCRDsOrigin,
		))
	}

	if converted.repository != nil {
		alias, exists := builder.repositoryAliases[converted.repository.URL]
		if !exists {
			alias = uniqueRepositoryAlias(
				repositoryAlias(converted.repository.URL),
				builder.usedRepositoryAliases,
			)
			builder.repositoryAliases[converted.repository.URL] = alias
			builder.repositoryPassCredentials[converted.repository.URL] =
				converted.repository.PassCredentials
			builder.repositoryOrigins[converted.repository.URL] = item.origin
			builder.usedRepositoryAliases[alias] = struct{}{}
			converted.repository.Name = alias
			builder.result.Repositories = append(builder.result.Repositories, *converted.repository)
		} else if converted.repository.PassCredentials !=
			builder.repositoryPassCredentials[converted.repository.URL] {
			return item.origin.wrap(fmt.Errorf(
				"spec.source.helm.passCredentials conflicts with %s",
				builder.repositoryOrigins[converted.repository.URL],
			))
		}
		converted.release.Chart = alias + "/" + converted.chart
	}
	builder.result.Releases = append(builder.result.Releases, converted.release)
	builder.provenanceComments = append(
		builder.provenanceComments,
		converted.provenanceComments...,
	)
	return nil
}

func (builder *helmfileBuilder) finalize() ([]byte, error) {
	if builder.sharedSkipCRDs {
		builder.result.HelmDefaults = &helmDefaults{SkipCRDs: true}
	}

	var output bytes.Buffer
	for _, comment := range builder.provenanceComments {
		fmt.Fprintf(&output, "# %s\n", comment)
	}
	if err := yaml.NewEncoder(
		&output,
		yaml.Indent(2),
		yaml.IndentSequence(true),
	).Encode(builder.result); err != nil {
		return nil, fmt.Errorf("encode helmfile: %w", err)
	}
	return output.Bytes(), nil
}
