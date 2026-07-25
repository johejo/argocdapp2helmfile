package main

import (
	"fmt"
	"strconv"
	"strings"

	diagnosticrule "github.com/johejo/argocdapp2helmfile/internal/diagnostic"
)

type conversionDiagnostic struct {
	origin      inputOrigin
	application string
	path        string
	category    diagnosticrule.Disposition
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
	ruleID diagnosticrule.RuleID,
	args ...any,
) {
	rule := diagnosticrule.MustLookup(ruleID)
	audit.diagnostics = append(audit.diagnostics, conversionDiagnostic{
		origin:      audit.input.origin,
		application: audit.name,
		path:        path,
		category:    rule.Disposition,
		message:     diagnosticrule.Message(ruleID, args...),
	})
}

func (audit *applicationAudit) revisionHistoryLimit() {
	limit := audit.input.application.Spec.RevisionHistoryLimit
	if limit != nil && *limit > 0 {
		audit.add(
			"spec.revisionHistoryLimit",
			diagnosticrule.RevisionHistoryPositive,
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
			diagnosticrule.AutomatedNotMapping,
		)
		return
	}

	enabled := true
	if rawEnabled, exists := automated["enabled"]; exists && rawEnabled != nil {
		value, ok := rawEnabled.(bool)
		if !ok {
			audit.add(
				"spec.syncPolicy.automated.enabled",
				diagnosticrule.AutomatedEnabledNotBool,
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
		diagnosticrule.AutomatedEnabled,
	)

	prune := audit.enabledBoolean(
		automated,
		"prune",
		"spec.syncPolicy.automated.prune",
		diagnosticrule.AutomatedPruneNotBool,
		diagnosticrule.AutomatedPrune,
	)
	audit.enabledBoolean(
		automated,
		"selfHeal",
		"spec.syncPolicy.automated.selfHeal",
		diagnosticrule.AutomatedSelfHealNotBool,
		diagnosticrule.AutomatedSelfHeal,
	)
	if prune {
		audit.enabledBoolean(
			automated,
			"allowEmpty",
			"spec.syncPolicy.automated.allowEmpty",
			diagnosticrule.AutomatedAllowEmptyNotBool,
			diagnosticrule.AutomatedAllowEmpty,
		)
	}
}

func (audit *applicationAudit) enabledBoolean(
	values map[string]any,
	field string,
	path string,
	invalidRule diagnosticrule.RuleID,
	enabledRule diagnosticrule.RuleID,
) bool {
	raw, exists := values[field]
	if !exists || raw == nil {
		return false
	}
	value, ok := raw.(bool)
	if !ok {
		audit.add(path, invalidRule)
		return false
	}
	if value {
		audit.add(path, enabledRule)
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
		audit.add("spec.syncPolicy.retry", diagnosticrule.RetryNotMapping)
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
			diagnosticrule.RetryLimitNotInt,
		)
		return
	}
	if limit != 0 {
		audit.add(
			"spec.syncPolicy.retry",
			diagnosticrule.RetryEnabled,
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
				diagnosticrule.SyncOptionNotString,
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
					diagnosticrule.SyncOptionConflict,
					option.key,
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
	path := syncOptionPath(option.index)
	if option.key == "" {
		audit.add(path, diagnosticrule.SyncOptionMalformed, option.text)
		return
	}

	ruleID, knownKey, knownValue := diagnosticrule.LookupSyncOption(option.key, option.value)
	if !knownKey {
		audit.add(path, diagnosticrule.SyncOptionUnknown, option.text)
		return
	}
	if !knownValue {
		audit.add(path, diagnosticrule.SyncOptionValueUnknown, option.text)
		return
	}
	if ruleID == diagnosticrule.ClientSideApplyMigrationFalse && serverSideApply {
		ruleID = diagnosticrule.ClientSideApplyMigrationDisabled
	}
	rule := diagnosticrule.MustLookup(ruleID)
	if rule.Disposition == diagnosticrule.Supported {
		return
	}
	switch ruleID {
	case diagnosticrule.ValidateFalse, diagnosticrule.SkipDryRunTrue,
		diagnosticrule.PruneLastTrue, diagnosticrule.ReplaceTrue,
		diagnosticrule.ForceTrue, diagnosticrule.ServerSideApplyTrue,
		diagnosticrule.FailOnSharedResourceTrue,
		diagnosticrule.RespectIgnoreDifferencesTrue:
		audit.add(path, ruleID, option.text)
	case diagnosticrule.PruneFalse, diagnosticrule.PruneConfirm,
		diagnosticrule.DeleteFalse, diagnosticrule.DeleteConfirm:
		audit.add(path, ruleID, option.key)
	default:
		audit.add(path, ruleID)
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
			diagnosticrule.ManagedNamespaceNotMapping,
		)
		return
	}
	if mappingHasValues(metadata["labels"]) || mappingHasValues(metadata["annotations"]) {
		audit.add(
			"spec.syncPolicy.managedNamespaceMetadata",
			diagnosticrule.ManagedNamespaceValues,
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
			diagnosticrule.IgnoreDifferencesNotSequence,
		)
		return
	}
	if len(values) != 0 {
		audit.add(
			"spec.ignoreDifferences",
			diagnosticrule.IgnoreDifferencesValues,
		)
	}
}
