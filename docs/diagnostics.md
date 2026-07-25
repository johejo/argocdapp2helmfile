# Diagnostic reference

The converter audits synchronization settings that can change Argo CD behavior:
`spec.revisionHistoryLimit`, `spec.syncPolicy`, and `spec.ignoreDifferences`.
It does not audit source conversion, destination resolution, or general Application and
ApplicationSet validation; failures in those areas are ordinary conversion errors.
Metadata, project, `sourceHydrator`, and
`argocd.argoproj.io/sync-options` resource annotations are outside this audit.

## Dispositions

- `supported` means the value is converted or is equivalent to Helmfile's effective
  behavior, so no diagnostic is emitted.
- `approximate` means the output has related but non-equivalent behavior.
- `intentionally-ignored` means the setting is understood but emits no Helmfile setting.
- `unconvertible` means the behavior has no Helmfile equivalent.
- `conversion-error` means conversion stops because emitting a Helmfile would be
  misleading.

## Setting rules

| Setting | Condition | Result |
| --- | --- | --- |
| `spec.revisionHistoryLimit` | positive | `approximate` — sets `historyMax`, which limits Helm release history rather than Argo CD revision history |
| `spec.revisionHistoryLimit` | zero | `conversion-error` — cannot be represented because Helmfile interprets zero as unlimited history |
| `spec.revisionHistoryLimit` | negative | `conversion-error` — cannot be represented because a Helm history limit must be positive |
| `spec.syncPolicy.automated` | not a mapping | `unconvertible` — must be a mapping |
| `spec.syncPolicy.automated.enabled` | not a boolean or null | `unconvertible` — must be a boolean or null |
| `spec.syncPolicy.automated.enabled` | true or omitted | `intentionally-ignored` — Helmfile runs only when invoked and does not reproduce automated sync |
| `spec.syncPolicy.automated.enabled` | false | `supported` — disables the controller behavior and requires no output setting |
| `spec.syncPolicy.automated.prune` | not a boolean | `unconvertible` — must be a boolean |
| `spec.syncPolicy.automated.prune` | true | `unconvertible` — enabled controller pruning has no Helmfile equivalent |
| `spec.syncPolicy.automated.prune` | false or null | `supported` — requires no output setting |
| `spec.syncPolicy.automated.selfHeal` | not a boolean | `unconvertible` — must be a boolean |
| `spec.syncPolicy.automated.selfHeal` | true | `unconvertible` — enabled self-healing has no Helmfile equivalent |
| `spec.syncPolicy.automated.selfHeal` | false or null | `supported` — requires no output setting |
| `spec.syncPolicy.automated.allowEmpty` | not a boolean and prune is true | `unconvertible` — must be a boolean |
| `spec.syncPolicy.automated.allowEmpty` | true and prune is true | `unconvertible` — allowing an empty automated prune has no Helmfile equivalent |
| `spec.syncPolicy.automated.allowEmpty` | false, null, or prune is not true | `supported` — is disabled or ineffective and requires no output setting |
| `spec.syncPolicy.retry` | not a mapping | `unconvertible` — must be a mapping |
| `spec.syncPolicy.retry.limit` | not an integer | `unconvertible` — must be an integer |
| `spec.syncPolicy.retry` | limit is nonzero | `unconvertible` — retry behavior has no Helmfile equivalent |
| `spec.syncPolicy.retry` | limit is zero or omitted | `supported` — requires no output setting |
| `spec.syncPolicy.managedNamespaceMetadata` | not a mapping and `CreateNamespace=true` | `unconvertible` — must be a mapping |
| `spec.syncPolicy.managedNamespaceMetadata` | nonempty labels or annotations and `CreateNamespace=true` | `unconvertible` — managed labels and annotations have no Helmfile equivalent |
| `spec.syncPolicy.managedNamespaceMetadata` | empty or `CreateNamespace=true` is absent | `supported` — is empty or ineffective and requires no output setting |
| `spec.ignoreDifferences` | not a sequence | `unconvertible` — must be a sequence |
| `spec.ignoreDifferences` | nonempty | `unconvertible` — diff customization has no Helmfile equivalent |
| `spec.ignoreDifferences` | empty or omitted | `supported` — requires no output setting |

## Sync option rules

Sync option keys and values are case-sensitive and whitespace is not normalized.

| Sync option | Value or condition | Result |
| --- | --- | --- |
| `CreateNamespace` | `true` | `approximate` — sets Helmfile `createNamespace`, without reproducing Argo CD namespace management |
| `CreateNamespace` | `false` | `supported` — requires no output setting |
| `ApplyOutOfSyncOnly` | `true` | `intentionally-ignored` — selective sync is not reproduced |
| `ApplyOutOfSyncOnly` | `false` | `supported` — requires no output setting |
| `Validate` | `true` | `supported` — uses Helm's default validation |
| `Validate` | `false` | `unconvertible` — has no Helmfile equivalent |
| `SkipDryRunOnMissingResource` | `true` | `unconvertible` — has no Helmfile equivalent |
| `SkipDryRunOnMissingResource` | `false` | `supported` — uses the default behavior |
| `PrunePropagationPolicy` | `foreground` | `supported` — matches the effective default |
| `PrunePropagationPolicy` | `background` | `unconvertible` — non-default prune propagation has no Helmfile equivalent |
| `PrunePropagationPolicy` | `orphan` | `unconvertible` — non-default prune propagation has no Helmfile equivalent |
| `PruneLast` | `true` | `unconvertible` — has no Helmfile equivalent |
| `PruneLast` | `false` | `supported` — uses the default behavior |
| `Replace` | `true` | `unconvertible` — has no Helmfile equivalent |
| `Replace` | `false` | `supported` — uses the default behavior |
| `Force` | `true` | `unconvertible` — has no Helmfile equivalent |
| `Force` | `false` | `supported` — uses the default behavior |
| `ServerSideApply` | `true` | `unconvertible` — has no Helmfile equivalent |
| `ServerSideApply` | `false` | `supported` — uses client-side apply |
| `ClientSideApplyMigration` | `true` | `supported` — uses the effective default |
| `ClientSideApplyMigration` | `false` and server-side apply is disabled | `supported` — is ineffective without server-side apply |
| `ClientSideApplyMigration` | `false` and `ServerSideApply=true` | `unconvertible` — has no Helmfile equivalent when server-side apply is enabled |
| `FailOnSharedResource` | `true` | `unconvertible` — has no Helmfile equivalent |
| `FailOnSharedResource` | `false` | `supported` — uses the default behavior |
| `RespectIgnoreDifferences` | `true` | `unconvertible` — has no Helmfile equivalent |
| `RespectIgnoreDifferences` | `false` | `supported` — uses the default behavior |
| `Prune` | `true` | `supported` — uses the default deletion behavior |
| `Prune` | `false` | `unconvertible` — has no Helmfile equivalent |
| `Prune` | `confirm` | `unconvertible` — has no Helmfile equivalent |
| `Delete` | `true` | `supported` — uses the default deletion behavior |
| `Delete` | `false` | `unconvertible` — has no Helmfile equivalent |
| `Delete` | `confirm` | `unconvertible` — has no Helmfile equivalent |

Unknown keys, unknown values, non-string items, and strings without a nonempty key followed by
`=` are `unconvertible`.
Identical duplicate strings are audited once at their first occurrence.
If one key has conflicting values, one `unconvertible` diagnostic is emitted at that
key's first occurrence and none of its values are evaluated.

Three conditions depend on other settings:

- `automated.allowEmpty` is audited only when `automated.prune` is true.
- `ClientSideApplyMigration=false` is unconvertible only when the non-conflicting option
  `ServerSideApply=true` is present.
- `managedNamespaceMetadata` is audited only when `CreateNamespace=true` is
  present.

## Reporting

Normal execution reports diagnostics as warnings, writes the generated Helmfile, and exits zero.
With `--strict`, diagnostics are reported as errors, no Helmfile is written, and the
command exits with status 1.
Conversion errors report one error, write no Helmfile, and exit with status 1.

Diagnostics are one line each and preserve document and generated Application order.
ApplicationSet diagnostics include the generator origin and rendered Application name:

```text
argocdapp2helmfile: warning: document 1: spec.generators[0].list.elements[0]: Application "api": spec.syncPolicy.syncOptions[0]: unconvertible: sync option "ServerSideApply=true" has no Helmfile equivalent
```
