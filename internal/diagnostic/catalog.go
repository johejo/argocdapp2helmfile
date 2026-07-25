package diagnostic

import (
	"fmt"
)

type RuleID string

type Disposition string

const (
	Supported            Disposition = "supported"
	Approximate          Disposition = "approximate"
	IntentionallyIgnored Disposition = "intentionally-ignored"
	Unconvertible        Disposition = "unconvertible"
	ConversionError      Disposition = "conversion-error"
)

type Rule struct {
	ID          RuleID
	Setting     string
	Condition   string
	Disposition Disposition
	Message     string
	Description string
}

type SyncOptionValue struct {
	Value string
	Rule  RuleID
}

type SyncOption struct {
	Key    string
	Values []SyncOptionValue
}

const (
	RevisionHistoryPositive RuleID = "revision-history-positive"
	RevisionHistoryZero     RuleID = "revision-history-zero"
	RevisionHistoryNegative RuleID = "revision-history-negative"

	AutomatedNotMapping         RuleID = "automated-not-mapping"
	AutomatedEnabledNotBool     RuleID = "automated-enabled-not-boolean"
	AutomatedEnabled            RuleID = "automated-enabled"
	AutomatedDisabled           RuleID = "automated-disabled"
	AutomatedPruneNotBool       RuleID = "automated-prune-not-boolean"
	AutomatedPrune              RuleID = "automated-prune"
	AutomatedPruneDisabled      RuleID = "automated-prune-disabled"
	AutomatedSelfHealNotBool    RuleID = "automated-self-heal-not-boolean"
	AutomatedSelfHeal           RuleID = "automated-self-heal"
	AutomatedSelfHealDisabled   RuleID = "automated-self-heal-disabled"
	AutomatedAllowEmptyNotBool  RuleID = "automated-allow-empty-not-boolean"
	AutomatedAllowEmpty         RuleID = "automated-allow-empty"
	AutomatedAllowEmptyInactive RuleID = "automated-allow-empty-inactive"

	RetryNotMapping  RuleID = "retry-not-mapping"
	RetryLimitNotInt RuleID = "retry-limit-not-integer"
	RetryEnabled     RuleID = "retry-enabled"
	RetryDisabled    RuleID = "retry-disabled"

	SyncOptionNotString    RuleID = "sync-option-not-string"
	SyncOptionMalformed    RuleID = "sync-option-malformed"
	SyncOptionUnknown      RuleID = "sync-option-unknown"
	SyncOptionValueUnknown RuleID = "sync-option-value-unknown"
	SyncOptionConflict     RuleID = "sync-option-conflict"

	CreateNamespaceTrue              RuleID = "sync-create-namespace-true"
	CreateNamespaceFalse             RuleID = "sync-create-namespace-false"
	ApplyOutOfSyncOnlyTrue           RuleID = "sync-apply-out-of-sync-only-true"
	ApplyOutOfSyncOnlyFalse          RuleID = "sync-apply-out-of-sync-only-false"
	ValidateTrue                     RuleID = "sync-validate-true"
	ValidateFalse                    RuleID = "sync-validate-false"
	SkipDryRunTrue                   RuleID = "sync-skip-dry-run-true"
	SkipDryRunFalse                  RuleID = "sync-skip-dry-run-false"
	PrunePropagationForeground       RuleID = "sync-prune-propagation-foreground"
	PrunePropagationBackground       RuleID = "sync-prune-propagation-background"
	PrunePropagationOrphan           RuleID = "sync-prune-propagation-orphan"
	PruneLastTrue                    RuleID = "sync-prune-last-true"
	PruneLastFalse                   RuleID = "sync-prune-last-false"
	ReplaceTrue                      RuleID = "sync-replace-true"
	ReplaceFalse                     RuleID = "sync-replace-false"
	ForceTrue                        RuleID = "sync-force-true"
	ForceFalse                       RuleID = "sync-force-false"
	ServerSideApplyTrue              RuleID = "sync-server-side-apply-true"
	ServerSideApplyFalse             RuleID = "sync-server-side-apply-false"
	ClientSideApplyMigrationTrue     RuleID = "sync-client-side-apply-migration-true"
	ClientSideApplyMigrationFalse    RuleID = "sync-client-side-apply-migration-false"
	ClientSideApplyMigrationDisabled RuleID = "sync-client-side-apply-migration-disabled"
	FailOnSharedResourceTrue         RuleID = "sync-fail-on-shared-resource-true"
	FailOnSharedResourceFalse        RuleID = "sync-fail-on-shared-resource-false"
	RespectIgnoreDifferencesTrue     RuleID = "sync-respect-ignore-differences-true"
	RespectIgnoreDifferencesFalse    RuleID = "sync-respect-ignore-differences-false"
	PruneTrue                        RuleID = "sync-prune-true"
	PruneFalse                       RuleID = "sync-prune-false"
	PruneConfirm                     RuleID = "sync-prune-confirm"
	DeleteTrue                       RuleID = "sync-delete-true"
	DeleteFalse                      RuleID = "sync-delete-false"
	DeleteConfirm                    RuleID = "sync-delete-confirm"

	ManagedNamespaceNotMapping   RuleID = "managed-namespace-metadata-not-mapping"
	ManagedNamespaceValues       RuleID = "managed-namespace-metadata-values"
	ManagedNamespaceInactive     RuleID = "managed-namespace-metadata-inactive"
	IgnoreDifferencesNotSequence RuleID = "ignore-differences-not-sequence"
	IgnoreDifferencesValues      RuleID = "ignore-differences-values"
	IgnoreDifferencesEmpty       RuleID = "ignore-differences-empty"
)

var rules = []Rule{
	{RevisionHistoryPositive, "spec.revisionHistoryLimit", "positive", Approximate,
		"Helmfile historyMax limits Helm release history but does not reproduce Argo CD revision history",
		"sets `historyMax`, which limits Helm release history rather than Argo CD revision history"},
	{RevisionHistoryZero, "spec.revisionHistoryLimit", "zero", ConversionError,
		"spec.revisionHistoryLimit cannot be 0: Argo CD disables revision history, but Helmfile historyMax 0 means unlimited history",
		"cannot be represented because Helmfile interprets zero as unlimited history"},
	{RevisionHistoryNegative, "spec.revisionHistoryLimit", "negative", ConversionError,
		"spec.revisionHistoryLimit cannot convert %d: history limit must be greater than 0",
		"cannot be represented because a Helm history limit must be positive"},
	{AutomatedNotMapping, "spec.syncPolicy.automated", "not a mapping", Unconvertible,
		"expected a mapping", "must be a mapping"},
	{AutomatedEnabledNotBool, "spec.syncPolicy.automated.enabled", "not a boolean or null", Unconvertible,
		"expected a boolean or null", "must be a boolean or null"},
	{AutomatedEnabled, "spec.syncPolicy.automated.enabled", "true or omitted", IntentionallyIgnored,
		"Helmfile runs only when invoked and does not reproduce Argo CD automated sync",
		"Helmfile runs only when invoked and does not reproduce automated sync"},
	{AutomatedDisabled, "spec.syncPolicy.automated.enabled", "false", Supported, "",
		"disables the controller behavior and requires no output setting"},
	{AutomatedPruneNotBool, "spec.syncPolicy.automated.prune", "not a boolean", Unconvertible,
		"expected a boolean", "must be a boolean"},
	{AutomatedPrune, "spec.syncPolicy.automated.prune", "true", Unconvertible,
		"the enabled Argo CD controller behavior has no Helmfile equivalent",
		"enabled controller pruning has no Helmfile equivalent"},
	{AutomatedPruneDisabled, "spec.syncPolicy.automated.prune", "false or null", Supported, "",
		"requires no output setting"},
	{AutomatedSelfHealNotBool, "spec.syncPolicy.automated.selfHeal", "not a boolean", Unconvertible,
		"expected a boolean", "must be a boolean"},
	{AutomatedSelfHeal, "spec.syncPolicy.automated.selfHeal", "true", Unconvertible,
		"the enabled Argo CD controller behavior has no Helmfile equivalent",
		"enabled self-healing has no Helmfile equivalent"},
	{AutomatedSelfHealDisabled, "spec.syncPolicy.automated.selfHeal", "false or null", Supported, "",
		"requires no output setting"},
	{AutomatedAllowEmptyNotBool, "spec.syncPolicy.automated.allowEmpty", "not a boolean and prune is true",
		Unconvertible, "expected a boolean", "must be a boolean"},
	{AutomatedAllowEmpty, "spec.syncPolicy.automated.allowEmpty", "true and prune is true", Unconvertible,
		"the enabled Argo CD controller behavior has no Helmfile equivalent",
		"allowing an empty automated prune has no Helmfile equivalent"},
	{AutomatedAllowEmptyInactive, "spec.syncPolicy.automated.allowEmpty", "false, null, or prune is not true",
		Supported, "", "is disabled or ineffective and requires no output setting"},
	{RetryNotMapping, "spec.syncPolicy.retry", "not a mapping", Unconvertible,
		"expected a mapping", "must be a mapping"},
	{RetryLimitNotInt, "spec.syncPolicy.retry.limit", "not an integer", Unconvertible,
		"expected an integer", "must be an integer"},
	{RetryEnabled, "spec.syncPolicy.retry", "limit is nonzero", Unconvertible,
		"Argo CD retry behavior has no Helmfile equivalent", "retry behavior has no Helmfile equivalent"},
	{RetryDisabled, "spec.syncPolicy.retry", "limit is zero or omitted", Supported, "",
		"requires no output setting"},
	{SyncOptionNotString, "spec.syncPolicy.syncOptions[index]", "item is not a string", Unconvertible,
		"sync option must be a string", "each item must be a string"},
	{SyncOptionMalformed, "spec.syncPolicy.syncOptions[index]", "missing a key or `=`", Unconvertible,
		"sync option %q is unknown or cannot be interpreted", "cannot be interpreted"},
	{SyncOptionUnknown, "spec.syncPolicy.syncOptions[index]", "unknown key", Unconvertible,
		"sync option %q is unknown or cannot be interpreted", "has an unknown, case-sensitive key"},
	{SyncOptionValueUnknown, "spec.syncPolicy.syncOptions[index]", "unknown value", Unconvertible,
		"sync option %q is unknown or cannot be interpreted", "has a value that cannot be interpreted"},
	{SyncOptionConflict, "spec.syncPolicy.syncOptions[index]", "same key has different values", Unconvertible,
		"sync option %q has conflicting duplicate values", "conflicting duplicates cannot be interpreted"},

	{CreateNamespaceTrue, "CreateNamespace", "`true`", Approximate,
		"Helmfile createNamespace creates a namespace but does not reproduce Argo CD namespace management",
		"sets Helmfile `createNamespace`, without reproducing Argo CD namespace management"},
	{CreateNamespaceFalse, "CreateNamespace", "`false`", Supported, "", "requires no output setting"},
	{ApplyOutOfSyncOnlyTrue, "ApplyOutOfSyncOnly", "`true`", IntentionallyIgnored,
		"Helmfile does not reproduce Argo CD selective sync", "selective sync is not reproduced"},
	{ApplyOutOfSyncOnlyFalse, "ApplyOutOfSyncOnly", "`false`", Supported, "", "requires no output setting"},
	{ValidateTrue, "Validate", "`true`", Supported, "", "uses Helm's default validation"},
	{ValidateFalse, "Validate", "`false`", Unconvertible,
		"sync option %q has no Helmfile equivalent", "has no Helmfile equivalent"},
	{SkipDryRunTrue, "SkipDryRunOnMissingResource", "`true`", Unconvertible,
		"sync option %q has no Helmfile equivalent", "has no Helmfile equivalent"},
	{SkipDryRunFalse, "SkipDryRunOnMissingResource", "`false`", Supported, "", "uses the default behavior"},
	{PrunePropagationForeground, "PrunePropagationPolicy", "`foreground`", Supported, "",
		"matches the effective default"},
	{PrunePropagationBackground, "PrunePropagationPolicy", "`background`", Unconvertible,
		"non-default Argo CD prune propagation has no Helmfile equivalent",
		"non-default prune propagation has no Helmfile equivalent"},
	{PrunePropagationOrphan, "PrunePropagationPolicy", "`orphan`", Unconvertible,
		"non-default Argo CD prune propagation has no Helmfile equivalent",
		"non-default prune propagation has no Helmfile equivalent"},
	{PruneLastTrue, "PruneLast", "`true`", Unconvertible,
		"sync option %q has no Helmfile equivalent", "has no Helmfile equivalent"},
	{PruneLastFalse, "PruneLast", "`false`", Supported, "", "uses the default behavior"},
	{ReplaceTrue, "Replace", "`true`", Unconvertible,
		"sync option %q has no Helmfile equivalent", "has no Helmfile equivalent"},
	{ReplaceFalse, "Replace", "`false`", Supported, "", "uses the default behavior"},
	{ForceTrue, "Force", "`true`", Unconvertible,
		"sync option %q has no Helmfile equivalent", "has no Helmfile equivalent"},
	{ForceFalse, "Force", "`false`", Supported, "", "uses the default behavior"},
	{ServerSideApplyTrue, "ServerSideApply", "`true`", Unconvertible,
		"sync option %q has no Helmfile equivalent", "has no Helmfile equivalent"},
	{ServerSideApplyFalse, "ServerSideApply", "`false`", Supported, "", "uses client-side apply"},
	{ClientSideApplyMigrationTrue, "ClientSideApplyMigration", "`true`", Supported, "",
		"uses the effective default"},
	{ClientSideApplyMigrationFalse, "ClientSideApplyMigration", "`false` and server-side apply is disabled",
		Supported, "", "is ineffective without server-side apply"},
	{ClientSideApplyMigrationDisabled, "ClientSideApplyMigration",
		"`false` and `ServerSideApply=true`", Unconvertible,
		"disabling client-side apply migration has no Helmfile equivalent",
		"has no Helmfile equivalent when server-side apply is enabled"},
	{FailOnSharedResourceTrue, "FailOnSharedResource", "`true`", Unconvertible,
		"sync option %q has no Helmfile equivalent", "has no Helmfile equivalent"},
	{FailOnSharedResourceFalse, "FailOnSharedResource", "`false`", Supported, "", "uses the default behavior"},
	{RespectIgnoreDifferencesTrue, "RespectIgnoreDifferences", "`true`", Unconvertible,
		"sync option %q has no Helmfile equivalent", "has no Helmfile equivalent"},
	{RespectIgnoreDifferencesFalse, "RespectIgnoreDifferences", "`false`", Supported, "",
		"uses the default behavior"},
	{PruneTrue, "Prune", "`true`", Supported, "", "uses the default deletion behavior"},
	{PruneFalse, "Prune", "`false`", Unconvertible,
		"Argo CD %s behavior has no Helmfile equivalent", "has no Helmfile equivalent"},
	{PruneConfirm, "Prune", "`confirm`", Unconvertible,
		"Argo CD %s behavior has no Helmfile equivalent", "has no Helmfile equivalent"},
	{DeleteTrue, "Delete", "`true`", Supported, "", "uses the default deletion behavior"},
	{DeleteFalse, "Delete", "`false`", Unconvertible,
		"Argo CD %s behavior has no Helmfile equivalent", "has no Helmfile equivalent"},
	{DeleteConfirm, "Delete", "`confirm`", Unconvertible,
		"Argo CD %s behavior has no Helmfile equivalent", "has no Helmfile equivalent"},

	{ManagedNamespaceNotMapping, "spec.syncPolicy.managedNamespaceMetadata",
		"not a mapping and `CreateNamespace=true`", Unconvertible,
		"expected a mapping", "must be a mapping"},
	{ManagedNamespaceValues, "spec.syncPolicy.managedNamespaceMetadata",
		"nonempty labels or annotations and `CreateNamespace=true`", Unconvertible,
		"Argo CD managed namespace labels and annotations have no Helmfile equivalent",
		"managed labels and annotations have no Helmfile equivalent"},
	{ManagedNamespaceInactive, "spec.syncPolicy.managedNamespaceMetadata",
		"empty or `CreateNamespace=true` is absent", Supported, "",
		"is empty or ineffective and requires no output setting"},
	{IgnoreDifferencesNotSequence, "spec.ignoreDifferences", "not a sequence", Unconvertible,
		"expected a sequence", "must be a sequence"},
	{IgnoreDifferencesValues, "spec.ignoreDifferences", "nonempty", Unconvertible,
		"Helmfile does not reproduce Argo CD diff customization",
		"diff customization has no Helmfile equivalent"},
	{IgnoreDifferencesEmpty, "spec.ignoreDifferences", "empty or omitted", Supported, "",
		"requires no output setting"},
}

var syncOptions = []SyncOption{
	{"CreateNamespace", []SyncOptionValue{{"true", CreateNamespaceTrue}, {"false", CreateNamespaceFalse}}},
	{"ApplyOutOfSyncOnly", []SyncOptionValue{{"true", ApplyOutOfSyncOnlyTrue}, {"false", ApplyOutOfSyncOnlyFalse}}},
	{"Validate", []SyncOptionValue{{"true", ValidateTrue}, {"false", ValidateFalse}}},
	{"SkipDryRunOnMissingResource", []SyncOptionValue{{"true", SkipDryRunTrue}, {"false", SkipDryRunFalse}}},
	{"PrunePropagationPolicy", []SyncOptionValue{
		{"foreground", PrunePropagationForeground},
		{"background", PrunePropagationBackground},
		{"orphan", PrunePropagationOrphan},
	}},
	{"PruneLast", []SyncOptionValue{{"true", PruneLastTrue}, {"false", PruneLastFalse}}},
	{"Replace", []SyncOptionValue{{"true", ReplaceTrue}, {"false", ReplaceFalse}}},
	{"Force", []SyncOptionValue{{"true", ForceTrue}, {"false", ForceFalse}}},
	{"ServerSideApply", []SyncOptionValue{{"true", ServerSideApplyTrue}, {"false", ServerSideApplyFalse}}},
	{"ClientSideApplyMigration", []SyncOptionValue{
		{"true", ClientSideApplyMigrationTrue},
		{"false", ClientSideApplyMigrationFalse},
	}},
	{"FailOnSharedResource", []SyncOptionValue{
		{"true", FailOnSharedResourceTrue},
		{"false", FailOnSharedResourceFalse},
	}},
	{"RespectIgnoreDifferences", []SyncOptionValue{
		{"true", RespectIgnoreDifferencesTrue},
		{"false", RespectIgnoreDifferencesFalse},
	}},
	{"Prune", []SyncOptionValue{{"true", PruneTrue}, {"false", PruneFalse}, {"confirm", PruneConfirm}}},
	{"Delete", []SyncOptionValue{{"true", DeleteTrue}, {"false", DeleteFalse}, {"confirm", DeleteConfirm}}},
}

var rulesByID = func() map[RuleID]Rule {
	result := make(map[RuleID]Rule, len(rules))
	for _, rule := range rules {
		result[rule.ID] = rule
	}
	return result
}()

func Rules() []Rule {
	return append([]Rule(nil), rules...)
}

func SyncOptions() []SyncOption {
	result := make([]SyncOption, len(syncOptions))
	for index, option := range syncOptions {
		result[index] = option
		result[index].Values = append([]SyncOptionValue(nil), option.Values...)
	}
	return result
}

func Lookup(id RuleID) (Rule, bool) {
	rule, ok := rulesByID[id]
	return rule, ok
}

func MustLookup(id RuleID) Rule {
	rule, ok := Lookup(id)
	if !ok {
		panic("unknown diagnostic rule: " + id)
	}
	return rule
}

func Message(id RuleID, args ...any) string {
	rule := MustLookup(id)
	if len(args) == 0 {
		return rule.Message
	}
	return fmt.Sprintf(rule.Message, args...)
}

func Error(id RuleID, args ...any) error {
	rule := MustLookup(id)
	if rule.Disposition != ConversionError {
		panic("diagnostic rule is not a conversion error: " + id)
	}
	return fmt.Errorf(rule.Message, args...)
}

func LookupSyncOption(key, value string) (RuleID, bool, bool) {
	for _, option := range syncOptions {
		if option.Key != key {
			continue
		}
		for _, candidate := range option.Values {
			if candidate.Value == value {
				return candidate.Rule, true, true
			}
		}
		return "", true, false
	}
	return "", false, false
}
