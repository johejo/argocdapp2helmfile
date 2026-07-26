# Application mapping reference

This reference describes how an Argo CD `Application` becomes Helmfile output.
Paths beginning with `spec.source` also apply to the manifest source selected from
`spec.sources`.

## Mapping catalog

| Argo CD Application or Config | Helmfile output | Conditions and notes |
| --- | --- | --- |
| metadata.name | Release `name` when `helm.releaseName` is absent | — |
| spec.source.helm.releaseName | Release `name` | — |
| spec.destination.namespace | Release `namespace` | — |
| Positive `spec.revisionHistoryLimit` | Release `historyMax` | Approximate mapping. |
| spec.source.helm.namespace | No additional output | Accepted only when it exactly matches `spec.destination.namespace`. |
| spec.source.helm.version | None | Accepted and intentionally ignored. |
| spec.source.helm.skipTests | None | Accepted and intentionally ignored. |
| spec.source.repoURL | Repository `url` | Scheme-less OCI repositories also set `oci: true`. |
| Packaged chart `spec.source.helm.passCredentials` | Repository `passCredentials: true` when true | — |
| spec.source.chart | Release chart as `<alias>/<chart>` | — |
| Packaged chart `spec.source.targetRevision` | Release `version` | — |
| Git `spec.source.repoURL` | Config source identity | Accepts HTTP(S), `git@host:path`, or `ssh://user@host/path`. |
| Git `spec.source.path` | Chart or explicit Kustomization path below configured `localRoot` | — |
| Git `spec.source.targetRevision` | Config source identity and provenance | Defaults to `HEAD`. |
| spec.source.kustomize | Kustomization release `values` and `transformers` | Options are listed in the Kustomize option catalogs. |
| spec.source.helm.valueFiles | Release `values` paths | — |
| spec.source.helm.values | Parsed inline release `values` entry | — |
| spec.source.helm.valuesObject | Inline release `values` entry | — |
| spec.source.helm.parameters | Release `set` or `setString` entries | — |
| spec.source.helm.fileParameters | Release `set` entries using `file` | — |
| spec.source.helm.ignoreMissingValueFiles | Release `missingFileHandler: Warn` when true | — |
| spec.source.helm.skipSchemaValidation | Release `skipSchemaValidation` | — |
| spec.source.helm.kubeVersion | Release `kubeVersion` | — |
| spec.source.helm.apiVersions | Release `apiVersions` | — |
| spec.source.helm.skipCrds | Shared `helmDefaults.skipCRDs` | All Helm releases must resolve to the same value. |
| Exact `CreateNamespace=true` sync option | Release `createNamespace: true` | Approximate mapping. |
| spec.destination.name or spec.destination.server | Release `kubeContext` through Config `destinations` | — |
| Config `releaseLabels` query result | Release `labels` entry | — |
| Source type selection | Packaged Helm chart, Git Helm chart, or explicit Git Kustomization | `chart`, `path`, repository URL type, and explicit `kustomize: {}` select the type. |
| spec.source or spec.sources | Exactly one manifest source plus optional values-only ref sources | The fields are mutually exclusive. Mapping paths apply to the selected manifest source. |
| Helm values inputs | Release `values`, `set`, and `setString` in Argo CD precedence order | From lowest to highest: `valueFiles`, `values`, `valuesObject`, then `parameters`. |
| Repository URL | Stable repository alias used by release chart `<alias>/<chart>` | Aliases are derived from the repository URL and disambiguated when necessary. |

## Source selection and required fields

`spec.source` and `spec.sources` are mutually exclusive.
Multiple sources must contain exactly one manifest source and may also contain values-only
sources identified by `ref`.
The mapping paths in the catalog apply to that selected manifest source.

A packaged chart uses `chart` and a valid HTTP(S) or scheme-less OCI repository.
A Git source uses `path` and is treated as a Helm chart unless
`kustomize: {}` explicitly selects a Kustomization.
The required fields are `metadata.name`, the manifest source's `repoURL`, and either `chart` plus
`targetRevision`
for a packaged chart or `path` for Git.
Git `targetRevision` defaults to `HEAD`.

## Helm values and correlated settings

Argo CD value precedence, from lowest to highest, is `valueFiles`,
`values`, `valuesObject`, then `parameters`.
The converter preserves that order in generated `values`, `set`, and
`setString` entries.
Value-file entries retain their positions subject to glob expansion and deduplication, and
parameters retain input order.

`helm.namespace`, when set, must exactly match
`spec.destination.namespace`.
`fileParameters` follow ordinary parameters in `set`;
a same-name `forceString` parameter is rejected because it belongs to
`setString`.
Because Helmfile's `helmDefaults.skipCRDs` is shared, all converted Helm releases
must resolve to the same `skipCrds` value.

## Build environment variables

Argo CD build environment variables are expanded in both `$VAR` and `${VAR}` forms, and
`$$` emits a literal `$`.
Source variables describe the selected manifest source, including where they appear in the
path of a `$ref` value file.

| Build environment variable | Expanded value |
| --- | --- |
| `ARGOCD_APP_NAME` | `metadata.name` |
| `ARGOCD_APP_NAMESPACE` | `metadata.namespace` |
| `ARGOCD_APP_PROJECT_NAME` | `spec.project`, or `default` when it is omitted |
| `ARGOCD_APP_SOURCE_PATH` | Manifest source `path` |
| `ARGOCD_APP_SOURCE_REPO_URL` | Manifest source `repoURL` |
| `ARGOCD_APP_SOURCE_TARGET_REVISION` | Manifest source `targetRevision` |

The remaining variables are rejected wherever expansion applies.

| Build environment variable | Rejection reason |
| --- | --- |
| `ARGOCD_APP_REVISION` | the revision is resolved when Argo CD syncs, not when the Application is written |
| `ARGOCD_APP_REVISION_SHORT` | the revision is resolved when Argo CD syncs, not when the Application is written |
| `ARGOCD_APP_REVISION_SHORT_8` | the revision is resolved when Argo CD syncs, not when the Application is written |
| `KUBE_VERSION` | the value comes from the destination cluster, which the converter does not query |
| `KUBE_API_VERSIONS` | the value comes from the destination cluster, which the converter does not query |

Expansion applies to `valueFiles` and `fileParameters` paths and to `parameters` values,
matching where Argo CD calls `Envsubst`.
Those inputs reject an unknown `ARGOCD_` variable and leave any other variable as written;
Argo CD instead replaces it with an empty string, so each value that renders differently is
reported as an `approximate` diagnostic.
Kustomize `commonLabels` and `commonAnnotations` values do replace every unresolved
variable with an empty string, as Argo CD does.

## Kustomize options

A non-null `spec.source.kustomize` mapping selects an explicit Kustomization and accepts
only these options:

| Argo CD Kustomize option | Helmfile output |
| --- | --- |
| `namePrefix` | Kustomization release `values.namePrefix` |
| `nameSuffix` | Kustomization release `values.nameSuffix` |
| `namespace` | Kustomization release `values.namespace` |
| `images` | Kustomization release `values.images` |
| `replicas` | Inline built-in `ReplicaCountTransformer` |
| `commonLabels` | Inline built-in `LabelTransformer` |
| `labelWithoutSelector` | `LabelTransformer.fieldSpecs` selection |
| `labelIncludeTemplates` | `LabelTransformer.fieldSpecs` selection |
| `commonAnnotations` | Inline built-in `AnnotationsTransformer` |
| `commonAnnotationsEnvsubst` | Conversion-time `commonAnnotations` value expansion |
| `forceCommonLabels` | None; accepted for validation only |
| `forceCommonAnnotations` | None; accepted for validation only |

Images use Kustomize's `[old=]image[:tag|@digest]` syntax and retain input order.
Transformers use the Kustomize v5.8.1 built-in field specs.
By default, labels apply to resource metadata, workload templates, and selectors.
With `labelWithoutSelector: true`, labels apply only to resource metadata unless
`labelIncludeTemplates: true` also includes templates.
`labelIncludeTemplates: true` requires `labelWithoutSelector: true`.

`replicas` becomes one `ReplicaCountTransformer` per entry, in input order, and a `count`
may be an integer or a numeric string.
Helmfile renames resources in an earlier build than the one that applies transformers, so
each replica target name is emitted with `namePrefix` and `nameSuffix` already applied.
Kustomize fails the build when a replica target matches no resource.

`commonLabels` values always expand the Argo CD build environment.
`commonAnnotations` values expand it only when `commonAnnotationsEnvsubst: true`.

`forceCommonLabels` and `forceCommonAnnotations` affect Argo CD's source edits.
Helmfile applies later transformers instead, so these options are accepted, validated as
booleans, and produce no output.

## Unsupported Kustomize options

Setting any of these options is a conversion error.
The rejection reason is reported in the error message.

| Argo CD Kustomize option | Rejection reason |
| --- | --- |
| `patches` | Helmfile applies patches after namePrefix, nameSuffix, and images, reversing Argo CD's order |
| `components` | Kustomize components have no Helmfile equivalent |
| `ignoreMissingComponents` | the option only affects components, which have no Helmfile equivalent |
| `version` | Helmfile selects the Kustomize binary globally, not per Application |
| `kubeVersion` | Helmfile does not pass a Kubernetes version to Kustomize builds |
| `apiVersions` | Helmfile does not pass API versions to Kustomize builds |

## Repository and Config resolution

Packaged-chart repository aliases are derived from repository URLs and disambiguated when
necessary.
Scheme-less OCI repository URLs must not include an `oci://` prefix.
Git sources are resolved through Config `sources` by repository URL and revision.
Destination name or server is resolved through Config `destinations` (including
cluster-derived entries), and Config `releaseLabels` project query results into release
labels.
