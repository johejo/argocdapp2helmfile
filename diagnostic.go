package main

import (
	"fmt"
	"strconv"
	"strings"
)

type diagnosticCategory string

const (
	diagnosticApproximate          diagnosticCategory = "approximate"
	diagnosticIntentionallyIgnored diagnosticCategory = "intentionally-ignored"
	diagnosticUnconvertible        diagnosticCategory = "unconvertible"
)

type conversionDiagnostic struct {
	origin      inputOrigin
	application string
	path        string
	category    diagnosticCategory
	message     string
}

func (diagnostic conversionDiagnostic) String() string {
	return fmt.Sprintf(
		`%s: Application %q: %s: %s: %s`,
		diagnostic.origin,
		diagnostic.application,
		diagnostic.path,
		diagnostic.category,
		diagnostic.message,
	)
}

func auditApplication(input applicationInput) []conversionDiagnostic {
	audit := applicationAudit{
		input: input,
		name:  input.application.Metadata.Name,
	}
	audit.revisionHistoryLimit()

	root, _ := input.data.(map[string]any)
	spec, _ := root["spec"].(map[string]any)
	syncPolicy, hasSyncPolicy := spec["syncPolicy"].(map[string]any)
	if hasSyncPolicy {
		audit.automated(syncPolicy)
		audit.retry(syncPolicy)
		audit.syncOptions(syncPolicy)
		audit.managedNamespaceMetadata(syncPolicy)
	}
	audit.ignoreDifferences(spec)
	return audit.diagnostics
}

type applicationAudit struct {
	input       applicationInput
	name        string
	diagnostics []conversionDiagnostic
}

func (audit *applicationAudit) add(
	path string,
	category diagnosticCategory,
	message string,
) {
	audit.diagnostics = append(audit.diagnostics, conversionDiagnostic{
		origin:      audit.input.origin,
		application: audit.name,
		path:        path,
		category:    category,
		message:     message,
	})
}

func (audit *applicationAudit) revisionHistoryLimit() {
	limit := audit.input.application.Spec.RevisionHistoryLimit
	if limit != nil && *limit > 0 {
		audit.add(
			"spec.revisionHistoryLimit",
			diagnosticApproximate,
			"Helmfile historyMax limits Helm release history but does not reproduce Argo CD revision history",
		)
	}
}

func (audit *applicationAudit) automated(syncPolicy map[string]any) {
	raw, exists := syncPolicy["automated"]
	if !exists || raw == nil {
		return
	}
	automated, ok := raw.(map[string]any)
	if !ok {
		audit.add(
			"spec.syncPolicy.automated",
			diagnosticUnconvertible,
			"expected a mapping",
		)
		return
	}

	enabled := true
	if rawEnabled, exists := automated["enabled"]; exists && rawEnabled != nil {
		value, ok := rawEnabled.(bool)
		if !ok {
			audit.add(
				"spec.syncPolicy.automated.enabled",
				diagnosticUnconvertible,
				"expected a boolean or null",
			)
			enabled = false
		} else {
			enabled = value
		}
	}
	if !enabled {
		return
	}
	audit.add(
		"spec.syncPolicy.automated.enabled",
		diagnosticIntentionallyIgnored,
		"Helmfile runs only when invoked and does not reproduce Argo CD automated sync",
	)

	prune := audit.enabledBoolean(
		automated,
		"prune",
		"spec.syncPolicy.automated.prune",
	)
	audit.enabledBoolean(
		automated,
		"selfHeal",
		"spec.syncPolicy.automated.selfHeal",
	)
	if prune {
		audit.enabledBoolean(
			automated,
			"allowEmpty",
			"spec.syncPolicy.automated.allowEmpty",
		)
	}
}

func (audit *applicationAudit) enabledBoolean(
	values map[string]any,
	field string,
	path string,
) bool {
	raw, exists := values[field]
	if !exists || raw == nil {
		return false
	}
	value, ok := raw.(bool)
	if !ok {
		audit.add(path, diagnosticUnconvertible, "expected a boolean")
		return false
	}
	if value {
		audit.add(
			path,
			diagnosticUnconvertible,
			"the enabled Argo CD controller behavior has no Helmfile equivalent",
		)
	}
	return value
}

func (audit *applicationAudit) retry(syncPolicy map[string]any) {
	raw, exists := syncPolicy["retry"]
	if !exists || raw == nil {
		return
	}
	retry, ok := raw.(map[string]any)
	if !ok {
		audit.add("spec.syncPolicy.retry", diagnosticUnconvertible, "expected a mapping")
		return
	}
	rawLimit, exists := retry["limit"]
	if !exists || rawLimit == nil {
		return
	}
	limit, ok := integerValue(rawLimit)
	if !ok {
		audit.add(
			"spec.syncPolicy.retry.limit",
			diagnosticUnconvertible,
			"expected an integer",
		)
		return
	}
	if limit != 0 {
		audit.add(
			"spec.syncPolicy.retry",
			diagnosticUnconvertible,
			"Argo CD retry behavior has no Helmfile equivalent",
		)
	}
}

func integerValue(value any) (int64, bool) {
	switch typed := value.(type) {
	case int:
		return int64(typed), true
	case int8:
		return int64(typed), true
	case int16:
		return int64(typed), true
	case int32:
		return int64(typed), true
	case int64:
		return typed, true
	case uint:
		return int64(typed), uint64(typed) <= uint64(^uint64(0)>>1)
	case uint8:
		return int64(typed), true
	case uint16:
		return int64(typed), true
	case uint32:
		return int64(typed), true
	case uint64:
		return int64(typed), typed <= uint64(^uint64(0)>>1)
	default:
		return 0, false
	}
}

type syncOptionOccurrence struct {
	index int
	text  string
	key   string
	value string
}

func (audit *applicationAudit) syncOptions(syncPolicy map[string]any) {
	raw, exists := syncPolicy["syncOptions"]
	if !exists || raw == nil {
		return
	}
	values, ok := raw.([]any)
	if !ok {
		return
	}

	occurrences := make([]syncOptionOccurrence, 0, len(values))
	for index, rawValue := range values {
		value, ok := rawValue.(string)
		if !ok {
			audit.add(
				syncOptionPath(index),
				diagnosticUnconvertible,
				"sync option must be a string",
			)
			continue
		}
		key, optionValue, found := strings.Cut(value, "=")
		if !found || key == "" {
			occurrences = append(occurrences, syncOptionOccurrence{
				index: index,
				text:  value,
			})
			continue
		}
		occurrences = append(occurrences, syncOptionOccurrence{
			index: index,
			text:  value,
			key:   key,
			value: optionValue,
		})
	}

	conflicting := make(map[string]bool)
	firstValue := make(map[string]string)
	for _, option := range occurrences {
		if option.key == "" {
			continue
		}
		if value, exists := firstValue[option.key]; exists && value != option.value {
			conflicting[option.key] = true
		} else if !exists {
			firstValue[option.key] = option.value
		}
	}

	seenConflict := make(map[string]bool)
	seenText := make(map[string]bool)
	serverSideApply := firstValue["ServerSideApply"] == "true" &&
		!conflicting["ServerSideApply"]
	for _, option := range occurrences {
		if option.key != "" && conflicting[option.key] {
			if !seenConflict[option.key] {
				audit.add(
					syncOptionPath(option.index),
					diagnosticUnconvertible,
					fmt.Sprintf(
						"sync option %q has conflicting duplicate values",
						option.key,
					),
				)
				seenConflict[option.key] = true
			}
			continue
		}
		if seenText[option.text] {
			continue
		}
		seenText[option.text] = true
		audit.syncOption(option, serverSideApply)
	}
}

func syncOptionPath(index int) string {
	return "spec.syncPolicy.syncOptions[" + strconv.Itoa(index) + "]"
}

func (audit *applicationAudit) syncOption(
	option syncOptionOccurrence,
	serverSideApply bool,
) {
	unsupported := func(message string) {
		audit.add(syncOptionPath(option.index), diagnosticUnconvertible, message)
	}
	ignored := func(message string) {
		audit.add(syncOptionPath(option.index), diagnosticIntentionallyIgnored, message)
	}
	approximate := func(message string) {
		audit.add(syncOptionPath(option.index), diagnosticApproximate, message)
	}
	unknown := func() {
		unsupported(fmt.Sprintf("sync option %q is unknown or cannot be interpreted", option.text))
	}

	switch option.key {
	case "CreateNamespace":
		switch option.value {
		case "true":
			approximate(
				"Helmfile createNamespace creates a namespace but does not reproduce Argo CD namespace management",
			)
		case "false":
		default:
			unknown()
		}
	case "ApplyOutOfSyncOnly":
		switch option.value {
		case "true":
			ignored("Helmfile does not reproduce Argo CD selective sync")
		case "false":
		default:
			unknown()
		}
	case "Validate":
		audit.booleanOption(option, "false", unsupported, unknown)
	case "SkipDryRunOnMissingResource":
		audit.booleanOption(option, "true", unsupported, unknown)
	case "PrunePropagationPolicy":
		switch option.value {
		case "foreground":
		case "background", "orphan":
			unsupported(
				"non-default Argo CD prune propagation has no Helmfile equivalent",
			)
		default:
			unknown()
		}
	case "PruneLast", "Replace", "Force", "ServerSideApply",
		"FailOnSharedResource", "RespectIgnoreDifferences":
		audit.booleanOption(option, "true", unsupported, unknown)
	case "ClientSideApplyMigration":
		switch option.value {
		case "true":
		case "false":
			if serverSideApply {
				unsupported(
					"disabling client-side apply migration has no Helmfile equivalent",
				)
			}
		default:
			unknown()
		}
	case "Prune", "Delete":
		switch option.value {
		case "true":
		case "false", "confirm":
			unsupported(
				fmt.Sprintf("Argo CD %s behavior has no Helmfile equivalent", option.key),
			)
		default:
			unknown()
		}
	default:
		unknown()
	}
}

func (audit *applicationAudit) booleanOption(
	option syncOptionOccurrence,
	unsupportedValue string,
	unsupported func(string),
	unknown func(),
) {
	switch option.value {
	case unsupportedValue:
		unsupported(fmt.Sprintf("sync option %q has no Helmfile equivalent", option.text))
	case "true", "false":
	default:
		unknown()
	}
}

func (audit *applicationAudit) managedNamespaceMetadata(syncPolicy map[string]any) {
	raw, exists := syncPolicy["managedNamespaceMetadata"]
	if !exists || raw == nil || !hasCreateNamespace(syncPolicy) {
		return
	}
	metadata, ok := raw.(map[string]any)
	if !ok {
		audit.add(
			"spec.syncPolicy.managedNamespaceMetadata",
			diagnosticUnconvertible,
			"expected a mapping",
		)
		return
	}
	if mappingHasValues(metadata["labels"]) || mappingHasValues(metadata["annotations"]) {
		audit.add(
			"spec.syncPolicy.managedNamespaceMetadata",
			diagnosticUnconvertible,
			"Argo CD managed namespace labels and annotations have no Helmfile equivalent",
		)
	}
}

func mappingHasValues(value any) bool {
	mapping, ok := value.(map[string]any)
	return ok && len(mapping) != 0
}

func hasCreateNamespace(syncPolicy map[string]any) bool {
	raw, ok := syncPolicy["syncOptions"].([]any)
	if !ok {
		return false
	}
	for _, option := range raw {
		if option == "CreateNamespace=true" {
			return true
		}
	}
	return false
}

func (audit *applicationAudit) ignoreDifferences(spec map[string]any) {
	raw, exists := spec["ignoreDifferences"]
	if !exists || raw == nil {
		return
	}
	values, ok := raw.([]any)
	if !ok {
		audit.add(
			"spec.ignoreDifferences",
			diagnosticUnconvertible,
			"expected a sequence",
		)
		return
	}
	if len(values) != 0 {
		audit.add(
			"spec.ignoreDifferences",
			diagnosticUnconvertible,
			"Helmfile does not reproduce Argo CD diff customization",
		)
	}
}
