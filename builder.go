package main

import (
	"bytes"
	"fmt"

	"github.com/goccy/go-yaml"
)

type repositoryRecord struct {
	alias           string
	passCredentials bool
	origin          inputOrigin
}

type helmfileBuilder struct {
	result                helmfile
	provenanceComments    []string
	repositories          map[string]repositoryRecord
	usedRepositoryAliases map[string]struct{}
	releaseOrigins        map[string]inputOrigin
	sharedSkipCRDs        bool
	sharedSkipCRDsOrigin  inputOrigin
	hasApplications       bool
	resolver              *sourceResolver
	destinationResolver   *destinationResolver
	projector             *releaseLabelProjector
	rollingSyncReleases   map[int]map[int][]int
}

func newHelmfileBuilder(config *conversionConfig) *helmfileBuilder {
	builder := &helmfileBuilder{
		repositories:          make(map[string]repositoryRecord),
		usedRepositoryAliases: make(map[string]struct{}),
		releaseOrigins:        make(map[string]inputOrigin),
		rollingSyncReleases:   make(map[int]map[int][]int),
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
		return item.origin.wrap(fmt.Errorf(
			"release name %q duplicates %s",
			converted.release.Name,
			previousOrigin,
		))
	}
	builder.releaseOrigins[converted.release.Name] = item.origin

	if converted.skipCRDsApplicable && !builder.hasApplications {
		builder.sharedSkipCRDs = converted.skipCRDs
		builder.sharedSkipCRDsOrigin = item.origin
		builder.hasApplications = true
	} else if converted.skipCRDsApplicable && converted.skipCRDs != builder.sharedSkipCRDs {
		return item.origin.wrap(fmt.Errorf(
			"spec.source.helm.skipCrds conflicts with %s",
			builder.sharedSkipCRDsOrigin,
		))
	}

	if converted.repository != nil {
		record, exists := builder.repositories[converted.repository.URL]
		if !exists {
			record = repositoryRecord{
				alias: uniqueRepositoryAlias(
					repositoryAlias(converted.repository.URL),
					builder.usedRepositoryAliases,
				),
				passCredentials: converted.repository.PassCredentials,
				origin:          item.origin,
			}
			builder.repositories[converted.repository.URL] = record
			builder.usedRepositoryAliases[record.alias] = struct{}{}
			converted.repository.Name = record.alias
			builder.result.Repositories = append(builder.result.Repositories, *converted.repository)
		} else if converted.repository.PassCredentials != record.passCredentials {
			return item.origin.wrap(fmt.Errorf(
				"spec.source.helm.passCredentials conflicts with %s",
				record.origin,
			))
		}
		converted.release.Chart = record.alias + "/" + converted.chart
	}
	builder.result.Releases = append(builder.result.Releases, converted.release)
	if item.rollingStep != nil {
		steps := builder.rollingSyncReleases[item.origin.document]
		if steps == nil {
			steps = make(map[int][]int)
			builder.rollingSyncReleases[item.origin.document] = steps
		}
		step := *item.rollingStep
		steps[step] = append(steps[step], len(builder.result.Releases)-1)
	}
	builder.provenanceComments = append(
		builder.provenanceComments,
		converted.provenanceComments...,
	)
	return nil
}

func (builder *helmfileBuilder) finalize() ([]byte, error) {
	builder.addRollingSyncNeeds()
	builder.result.HelmDefaults = &helmDefaults{
		SkipCRDs:        builder.sharedSkipCRDs,
		CreateNamespace: false,
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

func (builder *helmfileBuilder) addRollingSyncNeeds() {
	for _, steps := range builder.rollingSyncReleases {
		var previous []int
		highest := highestRollingSyncStep(steps)
		for step := 0; step <= highest; step++ {
			current, exists := steps[step]
			if !exists {
				continue
			}
			if len(current) == 0 {
				continue
			}
			if len(previous) != 0 {
				needs := make([]string, 0, len(previous))
				for _, index := range previous {
					needs = append(needs, releaseNeedsID(builder.result.Releases[index]))
				}
				for _, index := range current {
					builder.result.Releases[index].Needs = append([]string(nil), needs...)
				}
			}
			previous = current
		}
	}
}

func highestRollingSyncStep(steps map[int][]int) int {
	highest := -1
	for step := range steps {
		if step > highest {
			highest = step
		}
	}
	return highest
}

func releaseNeedsID(item release) string {
	if item.KubeContext != "" {
		return item.KubeContext + "/" + item.Namespace + "/" + item.Name
	}
	if item.Namespace != "" {
		return item.Namespace + "/" + item.Name
	}
	return item.Name
}
