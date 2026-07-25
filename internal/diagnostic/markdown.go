package diagnostic

import (
	"bytes"
	"fmt"
)

func Markdown() []byte {
	var output bytes.Buffer
	output.WriteString(`# Diagnostic reference

The converter audits synchronization settings that can change Argo CD behavior:
` + "`spec.revisionHistoryLimit`" + `, ` + "`spec.syncPolicy`" + `, and ` +
		"`spec.ignoreDifferences`" + `.
It does not audit source conversion, destination resolution, or general Application and
ApplicationSet validation; failures in those areas are ordinary conversion errors.
Metadata, project, ` + "`sourceHydrator`" + `, and
` + "`argocd.argoproj.io/sync-options`" + ` resource annotations are outside this audit.

## Dispositions

- ` + "`supported`" + ` means the value is converted or is equivalent to Helmfile's effective
  behavior, so no diagnostic is emitted.
- ` + "`approximate`" + ` means the output has related but non-equivalent behavior.
- ` + "`intentionally-ignored`" + ` means the setting is understood but emits no Helmfile setting.
- ` + "`unconvertible`" + ` means the behavior has no Helmfile equivalent.
- ` + "`conversion-error`" + ` means conversion stops because emitting a Helmfile would be
  misleading.

## Setting rules

| Setting | Condition | Result |
| --- | --- | --- |
`)
	for _, rule := range rules {
		if isSyncOptionRule(rule) || isInternalSyncRule(rule.ID) {
			continue
		}
		fmt.Fprintf(
			&output,
			"| `%s` | %s | `%s` — %s |\n",
			rule.Setting,
			rule.Condition,
			rule.Disposition,
			rule.Description,
		)
	}

	output.WriteString(`
## Sync option rules

Sync option keys and values are case-sensitive and whitespace is not normalized.

| Sync option | Value or condition | Result |
| --- | --- | --- |
`)
	for _, rule := range rules {
		if !isSyncOptionRule(rule) {
			continue
		}
		fmt.Fprintf(
			&output,
			"| `%s` | %s | `%s` — %s |\n",
			rule.Setting,
			rule.Condition,
			rule.Disposition,
			rule.Description,
		)
	}

	output.WriteString(`
Unknown keys, unknown values, non-string items, and strings without a nonempty key followed by
` + "`=`" + ` are ` + "`unconvertible`" + `.
Identical duplicate strings are audited once at their first occurrence.
If one key has conflicting values, one ` + "`unconvertible`" + ` diagnostic is emitted at that
key's first occurrence and none of its values are evaluated.

Three conditions depend on other settings:

- ` + "`automated.allowEmpty`" + ` is audited only when ` + "`automated.prune`" + ` is true.
- ` + "`ClientSideApplyMigration=false`" + ` is unconvertible only when the non-conflicting option
  ` + "`ServerSideApply=true`" + ` is present.
- ` + "`managedNamespaceMetadata`" + ` is audited only when ` + "`CreateNamespace=true`" + ` is
  present.

## Reporting

Normal execution reports diagnostics as warnings, writes the generated Helmfile, and exits zero.
With ` + "`--strict`" + `, diagnostics are reported as errors, no Helmfile is written, and the
command exits with status 1.
Conversion errors report one error, write no Helmfile, and exit with status 1.

Diagnostics are one line each and preserve document and generated Application order.
ApplicationSet diagnostics include the generator origin and rendered Application name:

` + "```text\n" +
		`argocdapp2helmfile: warning: document 1: spec.generators[0].list.elements[0]: Application "api": spec.syncPolicy.syncOptions[0]: unconvertible: sync option "ServerSideApply=true" has no Helmfile equivalent
` + "```\n")
	return output.Bytes()
}

func isSyncOptionRule(rule Rule) bool {
	for _, option := range syncOptions {
		if option.Key == rule.Setting {
			return true
		}
	}
	return rule.ID == ClientSideApplyMigrationDisabled
}

func isInternalSyncRule(id RuleID) bool {
	switch id {
	case SyncOptionNotString, SyncOptionMalformed, SyncOptionUnknown, SyncOptionValueUnknown,
		SyncOptionConflict:
		return true
	default:
		return false
	}
}
