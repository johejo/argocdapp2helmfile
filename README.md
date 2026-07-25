# argocdapp2helmfile

`argocdapp2helmfile` converts Argo CD `Application` resources and
`ApplicationSet` resources using supported List, Git, Cluster, Matrix, and Merge generators
into one helmfile.
It is an offline Unix filter: it reads a YAML stream from standard input,
writes YAML to standard output, and never fetches charts or repositories.

An optional config maps Argo CD destinations to helmfile kube contexts,
maps Git sources to paths available when helmfile runs,
and projects Application fields into release labels.

## Quick start

```sh
go install github.com/johejo/argocdapp2helmfile@latest
argocdapp2helmfile <application.yaml >helmfile.yaml
```

Use `--config` for destination kube contexts, Git-hosted charts or Kustomizations,
external values repositories, or release labels:

```sh
argocdapp2helmfile --config config.yaml \
  <application.yaml >helmfile.yaml
```

Separate multiple resources with YAML document markers.
Direct Applications and ApplicationSets may be mixed, and release order follows input order.
A Kubernetes `List` wrapper is not accepted directly; expand it first:

```sh
yq '.items[]' applications.yaml | argocdapp2helmfile
```

Diagnostics go to standard error.
Any invalid document makes the command fail without writing a partial helmfile.

## End-to-end test

Install `helmfile`, `helm`, and `kustomize` on `PATH`, then run the offline E2E test:

```sh
ARGOCDAPP2HELMFILE_E2E=1 \
  go test -count=1 -run '^TestE2E' ./...
```

## Conversion example

Input:

```yaml
apiVersion: argoproj.io/v1alpha1
kind: Application
metadata:
  name: nginx
spec:
  destination:
    namespace: web
  source:
    repoURL: https://charts.bitnami.com/bitnami
    chart: nginx
    targetRevision: 18.2.4
    helm:
      releaseName: edge
      passCredentials: true
      values: |
        service:
          type: ClusterIP
      parameters:
        - name: replicaCount
          value: "2"
        - name: image.tag
          value: "001"
          forceString: true
      skipSchemaValidation: true
```

Output:

```yaml
repositories:
  - name: bitnami
    url: https://charts.bitnami.com/bitnami
    passCredentials: true
helmDefaults:
  createNamespace: false
releases:
  - name: edge
    namespace: web
    chart: bitnami/nginx
    version: 18.2.4
    values:
      - service:
          type: ClusterIP
    set:
      - name: replicaCount
        value: "2"
    setString:
      - name: image.tag
        value: "001"
    skipSchemaValidation: true
```

Scheme-less OCI sources, such as `registry-1.docker.io/bitnamicharts`, produce a
repository with `oci: true`.
As required by Argo CD, do not include an `oci://` prefix.

## Supported input and mapping

Each YAML document must contain one `argoproj.io/v1alpha1` `Application` or
supported `ApplicationSet`.
An Application must identify either:

- a packaged chart in an HTTP(S) or scheme-less OCI Helm repository; or
- a Git directory, treated as a chart unless explicitly selected as a
  Kustomization with `kustomize: {}`.

### Application mapping

The `spec.source.*` paths below also apply to the manifest source selected from
`spec.sources`.
`spec.source` and `spec.sources` are mutually exclusive.

| Argo CD Application | helmfile |
| --- | --- |
| `metadata.name` | Release `name` when `helm.releaseName` is absent |
| `spec.source.helm.releaseName` | Release `name` |
| `spec.destination.namespace` | Release `namespace` |
| Positive `spec.revisionHistoryLimit` | Release `historyMax` |
| `spec.source.helm.namespace` | Accepted only when it exactly matches `spec.destination.namespace`; no additional output |
| `spec.source.helm.version` | Accepted and intentionally ignored |
| `spec.source.repoURL` | Repository `url`; scheme-less OCI also sets `oci: true` |
| Packaged chart `spec.source.helm.passCredentials` | Repository `passCredentials: true` when true |
| `spec.source.chart` | Release chart as `<alias>/<chart>` |
| `spec.source.targetRevision` for a packaged chart | Release `version` |
| Git `spec.source.repoURL` | Config source identity; HTTP(S), `git@host:path`, or `ssh://user@host/path` |
| Git `spec.source.path` | Helm chart or explicit Kustomization path below the configured `localRoot` |
| Git `spec.source.targetRevision` | Config source identity and provenance; defaults to `HEAD` |
| `spec.source.helm.valueFiles` | Release `values` paths |
| `spec.source.helm.values` | Parsed inline `values` entry |
| `spec.source.helm.valuesObject` | Inline `values` entry |
| `spec.source.helm.parameters` | Release `set` or `setString` entries |
| `spec.source.helm.fileParameters` | Release `set` entries using `file` |
| `spec.source.helm.ignoreMissingValueFiles` | Release `missingFileHandler: Warn` when true |
| `spec.source.helm.skipSchemaValidation` | Release `skipSchemaValidation` |
| `spec.source.helm.kubeVersion` | Release `kubeVersion` |
| `spec.source.helm.apiVersions` | Release `apiVersions` |
| `spec.source.helm.skipCrds` | Shared `helmDefaults.skipCRDs` |
| `spec.syncPolicy.syncOptions` | Shared `helmDefaults.createNamespace: false`; exact `CreateNamespace=true` also sets release `createNamespace: true` |
| `spec.destination.name` or `spec.destination.server` | Release `kubeContext` through Config `destinations` |
| Config `releaseLabels` query result | Release `labels` entry |

The required fields are `metadata.name`, the manifest source's `repoURL`, and either
`chart` plus `targetRevision` for a Helm/OCI repository or `path` for Git.

Argo CD documents Helm value precedence, from lowest to highest, as
`valueFiles`, `values`, `valuesObject`, then `parameters`.
The converter preserves that ordering in the generated `values`, `set`, and `setString`
entries.
Value-file entries retain their positions subject to glob expansion and deduplication,
and parameters retain input order.

`helm.namespace`, when set, must match `spec.destination.namespace`.
`fileParameters` follow ordinary parameters in `set`;
a same-name `forceString` parameter is rejected because it belongs to `setString`.

### ApplicationSet generators

ApplicationSets may use either Go templates or the legacy fasttemplate syntax
and must contain one or more supported generators.
Set `spec.goTemplate: true` to use Go templates:

```yaml
apiVersion: argoproj.io/v1alpha1
kind: ApplicationSet
metadata:
  name: services
spec:
  goTemplate: true
  goTemplateOptions: [missingkey=error]
  generators:
    - list:
        elements:
          - name: frontend
            chart: nginx
            version: 18.2.4
            namespace: web
          - name: metrics
            chart: prometheus
            version: 27.3.1
            namespace: monitoring
  template:
    metadata:
      name: '{{ .name }}'
    spec:
      destination:
        namespace: '{{ .namespace }}'
      source:
        repoURL: https://charts.example.com
        chart: '{{ .chart }}'
        targetRevision: '{{ .version }}'
```

The converter expands the set locally, then applies the normal Application
conversion and validation rules to every result.
Multiple generators and elements retain their input order.

#### RollingSync

`strategy.type: RollingSync` is converted to
[helmfile `needs`][helmfile-releases-dag] when:

- `strategy.deletionOrder` is `Reverse`;
- `rollingSync.steps` is non-empty and uses only `In` and `NotIn`
  `matchExpressions`;
- `maxUpdate` is omitted or is the string `"100%"`; and
- every generated Application matches exactly one step by its final labels.

Empty steps are skipped.
Each subsequent non-empty step depends on the preceding non-empty step,
within the same ApplicationSet.

Unlike [Argo CD Progressive Syncs][argocd-progressive-syncs],
helmfile does not wait for Application health or reproduce manual gates and
partial concurrency.
Run without label selectors to preserve ordering,
or use `--include-transitive-needs` because selectors ignore `needs` by default.

Go template expressions use a leading dot, such as `{{ .name }}`.
They provide Sprig functions except `env`, `expandenv`, and `getHostByName`,
plus `normalize`, `slugify`, `toYaml`, `fromYaml`, and `fromYamlArray`.
Supported `goTemplateOptions` are `missingkey=default`, `missingkey=invalid`,
`missingkey=zero`, and `missingkey=error`.

When `goTemplate` is `false` or omitted,
legacy expressions use flat keys without a leading dot, such as `{{name}}`.
Whitespace inside delimiters is ignored,
and undefined or non-string parameters are left unchanged.
`goTemplateOptions` are ignored in this mode.

Supported List features are:

- nested YAML values in `elements` when Go templates are enabled;
- literal `elementsYaml`;
- generator-level `template` overrides for top-level List generators;
- selectors using `matchLabels` and the `In`, `NotIn`, `Exists`, and
  `DoesNotExist` operators;
- templating of every string field and string mapping key; and
- a YAML or JSON `templatePatch`.

Explicit legacy List `elements` accept string fields.
Their reserved `values` mapping is exposed as flat `values.<key>` parameters.
Legacy `elementsYaml` values retain their decoded YAML shape.

#### Git generator

The Git generator supports either `directories` or `files`.
Its `repoURL` and `revision` must exactly match a Config `sources` entry,
whose `localRoot` must resolve from the converter's current working directory to an
existing non-symlink directory without helmfile template expressions.

Directory patterns use Go's `path.Match` rules.
File patterns use doublestar rules,
where `*` matches one component and `**` matches recursively.
Excludes take precedence,
duplicates are removed,
and results are generated in lexical order.

Templates receive Argo CD-compatible path parameters.
File generators read YAML or JSON mappings and mapping sequences.
`pathParamPrefix`, generator `values`, selectors,
top-level generator templates, and `templatePatch` are supported.

Go templates use path parameters such as `.path.path`, `.path.basename`,
and `.path.segments`.
Legacy templates use flat parameters such as `path`, `path.basename`, and `path[0]`.
`pathParamPrefix: repo` changes these to `.repo.path.path` and `repo.path`,
respectively.
Legacy file content and generator `values` are flattened into dot-separated keys,
with scalar values converted to strings.

Hidden directories are skipped by directory generators;
`.git` and symlinks are skipped by file generators.
See the
[Argo CD Git generator documentation](https://argo-cd.readthedocs.io/en/stable/operator-manual/applicationset/Generators-Git/)
for the upstream generator model.

#### Cluster generator

The Cluster generator expands the Config `clusters` snapshot in declaration order.
It requires `--config`; an omitted or empty inventory generates no Applications.

Each entry exposes `name`, `nameNormalized`, `server`, and `project`.
An omitted `project` is an empty string.
Go templates receive nested `metadata` and `values` maps;
legacy templates receive flat `metadata.labels.<key>`,
`metadata.annotations.<key>`, and `values.<key>` parameters.

The `clusters.selector` matches inventory labels,
while a sibling `selector` filters all generated parameters.
Cluster `values` may reference cluster and Matrix parent parameters.
Selectors use the same operators as List and Git;
top-level template overrides are supported.

`flatList: true` is not supported.
The parameter shape follows the
[Argo CD Cluster generator documentation](https://argo-cd.readthedocs.io/en/stable/operator-manual/applicationset/Generators-Cluster/).

Matrix and Merge may contain a Matrix or Merge child one level deep.
Nested combination generators may contain only List, Git, or Cluster children.
Only top-level generator templates are supported,
while selectors apply at every level.

#### Matrix generator

Matrix requires exactly two List, Git, Cluster, Matrix, or Merge children.

The first child's parameters may be used to render the second child,
including a nested Merge definition,
dynamic List `elementsYaml`,
and Git fields.
Git `values` may reference parent, Git path, and Git parameter-file fields together.
Results retain child order,
and the first child's values take precedence recursively when parameter maps overlap.

For Git × Git,
`pathParamPrefix` is recommended when both children need to retain their path parameters.
Without prefixes,
the normal first-child-wins merge behavior applies to the shared `path` key.
See the
[Argo CD Matrix generator documentation](https://argo-cd.readthedocs.io/en/stable/operator-manual/applicationset/Generators-Matrix/)
for the upstream generator constraints and parameter model.

#### Merge generator

Merge accepts two or more List, Git, Cluster, Matrix, or Merge children.
It preserves the first child's results and order,
then applies matching children in declaration order using `mergeKeys`.
Maps merge recursively;
scalars and sequences use the later value.
Unmatched overrides are ignored,
and results missing any merge key do not match.

Children expand and apply selectors independently.
Complete merge-key tuples must be unique within each child.
Go templates do not support dotted merge keys;
legacy mode matches their flattened form.
See the
[Argo CD Merge generator documentation](https://argo-cd.readthedocs.io/en/stable/operator-manual/applicationset/Generators-Merge/)
for the upstream generator model.

`templatePatch` is rendered once per selected generator parameter set
with the configured template mode.
Mappings merge recursively,
while scalars and sequences replace the template value;
`null` deletes a field.
The patch is applied after template rendering,
and patched metadata is available to `releaseLabels` queries.
As in Argo CD, the pre-patch `spec.project` is always retained.

The rendered patch must be one YAML or JSON mapping.
Strategic Merge Patch directives are rejected.

## Conversion config

The optional config is exactly one YAML document.
It uses a fixed API version and kind, and rejects unknown fields.
This complete example shows all available features:

```yaml
apiVersion: argocdapp2helmfile/v1alpha1
kind: Config
destinations:
  - name: production
    kubeContext: prod-admin
  - server: https://kubernetes.default.svc
    kubeContext: in-cluster
clusters:
  - name: production-cluster
    server: https://production.example.com
    kubeContext: production-admin
    project: platform
    labels:
      environment: production
    annotations:
      example.com/owner: platform
sources:
  - repoURL: git@github.com:example/platform-charts.git
    targetRevision: release-1
    localRoot: checkouts/platform-charts
  - repoURL: https://github.com/example/values.git
    targetRevision: main
    localRoot: '{{ requiredEnv "VALUES_ROOT" }}'
releaseLabels:
  - name: argocd.skipTests
    query: .spec.source.helm.skipTests // false
  - name: argocd.project
    query: .spec.project
```

Any of `destinations`, `clusters`, `sources`, or `releaseLabels` may be omitted.
The config may be omitted when none of these features is needed.

### Destination kube contexts

Each `destinations` item must contain exactly one non-empty `name` or `server`
and a non-empty `kubeContext`.
Entries match the corresponding literal `spec.destination.name`
or `spec.destination.server` by exact string equality.
The configured `kubeContext` is copied unchanged to the generated helmfile release;
the converter does not read a kubeconfig or verify that the context exists.
If an Application sets either field, `--config` and a matching entry are required.
If neither is set, `kubeContext` is omitted.

Every `clusters` entry is also registered as a destination under both its exact
`name` and `server`.
Cluster names and servers must therefore be unique across `clusters` and
`destinations`.

### Creating a cluster snapshot

The converter does not access Kubernetes or refresh `clusters`.
It does not add a local cluster or secret-type label.

This example extracts the inventory without selecting the authentication `config`.
Replace the annotation allowlist and review the output:

```sh
kubectl get secrets -n argocd \
  -l argocd.argoproj.io/secret-type=cluster \
  -o json |
  jq '{
    clusters: [
      .items[] |
      {
        name: (.data.name | @base64d),
        server: (.data.server | @base64d),
        kubeContext: (.data.name | @base64d),
        project: (
          (.data.project // "") |
          if . == "" then "" else @base64d end
        ),
        labels: (
          (.metadata.labels // {}) |
          del(."argocd.argoproj.io/secret-type")
        ),
        annotations: (
          (.metadata.annotations // {}) |
          with_entries(select(.key == "example.com/owner"))
        )
      }
    ]
  }' |
  yq -P
```

Cluster Secrets do not contain kubeconfig context names.
The example uses the cluster name as `kubeContext`;
otherwise, match `server` against kubeconfig.
Multiple contexts for one server require an explicit choice.

Allowlist annotations because fields such as
`kubectl.kubernetes.io/last-applied-configuration` may contain Secret material.
The default local cluster may have no Cluster Secret and is therefore absent from
the dump.

### Git charts, Kustomizations, external values, and paths

A Config `sources` entry matches the Application's `repoURL` and normalized
`targetRevision` pair.
Its required `localRoot` is copied unchanged as the prefix for generated paths.
It may be absolute, relative, or contain a helmfile template expression,
and the converter does not expand shell paths.
`localRoot` values need not be unique.
A Git source without a matching entry is an error.
Config `sources[].targetRevision` remains required; use `HEAD` explicitly when its
`localRoot` is checked out at the corresponding revision.
Because `HEAD` can move, use a branch, tag, or commit SHA when that distinction matters.

For a Git-hosted chart, use `path` rather than `chart`:

```yaml
source:
  repoURL: git@github.com:example/platform-charts.git
  path: charts/my-app
  targetRevision: release-1
```

With the config above, the release chart becomes
`checkouts/platform-charts/charts/my-app`.
A path of `.` refers to the configured `localRoot`.
No repository or release `version` is emitted because `targetRevision` selects
the Git source, not the version in `Chart.yaml`.

A Git `path` remains a Helm chart unless the source contains an explicit
non-null `kustomize` mapping.
When `localRoot` is inspectable, a standard Kustomization file without
`Chart.yaml` is an error that suggests adding the mapping.
Use `kustomize: {}` when no options are needed:

```yaml
source:
  repoURL: https://github.com/example/platform.git
  targetRevision: release-1
  path: deploy/my-app
  kustomize:
    namePrefix: edge-
    nameSuffix: -prod
    namespace: manifests
    images:
      - example/app:v2
      - old=registry.example.com:5000/team/app@sha256:abcdef
    commonLabels:
      app.kubernetes.io/name: ${ARGOCD_APP_NAME}
    commonAnnotations:
      app.example.com/source-path: ${ARGOCD_APP_SOURCE_PATH}
    commonAnnotationsEnvsubst: true
```

The supported options and their Helmfile representations are:

| Argo CD Kustomize option | Helmfile representation |
| --- | --- |
| `namePrefix`, `nameSuffix`, `namespace`, `images` | Kustomization release `values` |
| `commonLabels` | Inline built-in `LabelTransformer` |
| `labelWithoutSelector`, `labelIncludeTemplates` | `LabelTransformer.fieldSpecs` selection |
| `commonAnnotations` | Inline built-in `AnnotationsTransformer` |
| `commonAnnotationsEnvsubst` | Conversion-time annotation value expansion |
| `forceCommonLabels`, `forceCommonAnnotations` | Accepted; no generated output |

Images use Kustomize's `[old=]image[:tag|@digest]` syntax and retain input order.
Transformers use the Kustomize v5.8.1 built-in field specs.
By default, labels apply to resource metadata, workload templates, and selectors.
With `labelWithoutSelector: true`, labels apply only to resource metadata unless
`labelIncludeTemplates: true` also includes templates.
`labelIncludeTemplates: true` requires `labelWithoutSelector: true`.

`commonLabels` values always expand the Argo CD build environment.
`commonAnnotations` values expand it only when `commonAnnotationsEnvsubst: true`.
Supported variables are `ARGOCD_APP_NAME`, `ARGOCD_APP_NAMESPACE`,
`ARGOCD_APP_PROJECT_NAME`, `ARGOCD_APP_SOURCE_PATH`, `ARGOCD_APP_SOURCE_REPO_URL`, and
`ARGOCD_APP_SOURCE_TARGET_REVISION`.
Revision and Kubernetes variables are rejected;
unknown variables expand to an empty string.

`forceCommonLabels` and `forceCommonAnnotations` affect Argo CD's source edits.
Helmfile applies later transformers instead, so these options are no-ops.

`replicas`, `patches`, `components`, `ignoreMissingComponents`, `version`, `kubeVersion`,
and `apiVersions` are unsupported.
They require source edits or build controls that Helmfile's Kustomization integration
does not expose.
See [Argo CD's Kustomize options][argocd-kustomize-options] for the upstream semantics.

`spec.destination.namespace` remains the Helm release namespace.
`kustomize.namespace` controls the generated manifest namespace, matching
[Argo CD's Kustomize namespace semantics][argocd-kustomize-namespace].

`kustomize` cannot be combined with `chart`, `helm`, `directory`, or `plugin`.

A multi-source Application is supported when exactly one source is a Helm chart
or explicit Kustomization and every other source is a values-only source with a
unique `ref`.
For example, `$values/prod/values.yaml` resolves to
`{{ requiredEnv "VALUES_ROOT" }}/prod/values.yaml`.
The `$ref` token is valid only at the start of a value-file or file-parameter path.
Generated output includes provenance comments for Git chart and values sources.
Packaged charts require `$ref` paths;
remote value-file and file-parameter URLs are not supported.

Helmfile installs a Kustomization as a temporary Helm chart through its
[Kustomization support][helmfile-kustomizations].
The standalone Helmfile binary requires an external `kustomize` binary,
while the official Helmfile container includes it.
Deletion, history, and hook semantics therefore follow Helm rather than direct
Argo CD Kustomize management.

Path resolution follows these rules:

- a relative path starts at the Git chart directory;
- a leading `/` starts at that Git repository's root, not the OS root;
- the part after `$ref/` starts at the referenced repository's root;
- normalized paths may not escape their configured source; and
- explicit paths retain input order.

For templated `localRoot` values, use a `.gotmpl` output and provide the value at
runtime:

```sh
argocdapp2helmfile --config config.yaml \
  <application.yaml >helmfile.yaml.gotmpl
VALUES_ROOT="$PWD/checkouts/values" \
  helmfile -f helmfile.yaml.gotmpl apply
```

The converter expands `*`, `?`, character classes, and recursive `**` patterns
in value-file paths relative to the chart or `$ref` repository root.
A source used by a pattern must have an existing, non-symlink directory as its
Config `localRoot`; that directory cannot contain a helmfile template expression.
Explicit value files are not checked against the local filesystem.

Matches retain doublestar traversal order rather than being globally sorted.
Duplicate normalized paths are removed.
Explicit paths take precedence over glob matches regardless of entry order.
Symlinks may point within the canonical source root but not outside it;
output retains the matched logical path.
A pattern matching no files is an error unless `ignoreMissingValueFiles: true`,
which omits the entry and sets `missingFileHandler: Warn`.

The following statically known Argo CD build-environment variables are expanded
in value-file paths in both `$VAR` and `${VAR}` forms:
`ARGOCD_APP_NAME`, `ARGOCD_APP_NAMESPACE`, `ARGOCD_APP_PROJECT_NAME`,
`ARGOCD_APP_SOURCE_PATH`, `ARGOCD_APP_SOURCE_REPO_URL`, and
`ARGOCD_APP_SOURCE_TARGET_REVISION`.
An omitted project expands as `default`, and `$$` emits a literal `$`.
Source variables describe the Helm chart source, including for `$ref` value files.
`ARGOCD_APP_REVISION*`, `KUBE_VERSION`, `KUBE_API_VERSIONS`,
and unknown `ARGOCD_` variables are rejected.

File parameters are passed as files without converter-side glob expansion.
Build-environment variables are rejected,
and `ignoreMissingValueFiles` does not apply to them.

### Release labels

Each `releaseLabels` item has a unique non-empty `name` and a non-empty
[jq](https://jqlang.org/manual/) expression in `query`.
The query runs against each final Application, including those generated from
an ApplicationSet.

A single string, boolean, or number becomes a string label value.
`null` or no result omits the label.
Multiple results, arrays, objects, jq errors, and duplicate names fail conversion.
Labels retain config order.
No `labels` mapping is emitted when there are no rules or every result is omitted.

This projection is opt-in.
`spec.source.helm.skipTests` is otherwise accepted and intentionally ignored
because it controls Argo CD's Helm invocation, not a helmfile release.

## Conversion behavior and constraints

- Repository aliases use the final non-empty URL path element, normalized to
  lowercase with unsafe character runs replaced by `-`.
  Query strings, fragments, and trailing slashes are ignored.
  An empty result falls back to `source`.
  Exact matching `repoURL` strings share an alias.
  Applications sharing a `repoURL` must also have
  the same effective `helm.passCredentials` value.
  Omission is treated as `false`.
  When aliases for different URLs collide,
  later aliases receive the first available numeric suffix
  (`charts-2`, `charts-3`, and so on) within the generated Helmfile.
- A release name defaults to `metadata.name` unless `helm.releaseName` is set.
  Resolved names must be unique across all namespaces and generated Applications.
- [`revisionHistoryLimit`][argocd-application-spec] to
  [`historyMax`][helmfile-configuration] is an approximate mapping because
  Argo CD and Helm track different histories.
  Omission preserves their defaults;
  zero and negative values are rejected because Argo CD zero disables history,
  while Helm zero means unlimited history.
- `skipCrds` is per Helm Application in Argo CD but `helmDefaults.skipCRDs` is
  shared.
  Every converted Helm chart must therefore have the same effective value, with
  omission treated as `false`.
  Kustomization releases do not participate in this comparison.
  Generated `skipCRDs` output requires helmfile v1.3.0 or newer.
- Empty YAML documents are rejected.
  Errors identify the one-based document number and, for ApplicationSets, the
  generator and element.
- Apart from `CreateNamespace=true`, operational fields such as `project`
  and other sync options are not converted.

Unsupported inputs fail instead of producing an incomplete helmfile.
Unsupported Helm options are ignored only when their value is null, an empty string,
an empty sequence, or an empty mapping.
Boolean `false` and numeric zero are values rather than empty input, so they are rejected.

For upstream behavior, see also

- [Argo CD Helm documentation][argocd-helm]
- [Argo CD Go Template documentation][argocd-go-template]
- [Argo CD sync option documentation][argocd-create-namespace]
- [helmfile configuration reference][helmfile-configuration]

[argocd-helm]: https://argo-cd.readthedocs.io/en/latest/user-guide/helm/
[argocd-application-spec]:
  https://argo-cd.readthedocs.io/en/stable/user-guide/application-specification/
[argocd-go-template]:
  https://argo-cd.readthedocs.io/en/latest/operator-manual/applicationset/GoTemplate/
[argocd-create-namespace]:
  https://argo-cd.readthedocs.io/en/stable/user-guide/sync-options/#create-namespace
[argocd-kustomize-namespace]:
  https://argo-cd.readthedocs.io/en/stable/user-guide/kustomize/#setting-the-manifests-namespace
[argocd-kustomize-options]:
  https://argo-cd.readthedocs.io/en/stable/user-guide/kustomize/
[argocd-progressive-syncs]:
  https://argo-cd.readthedocs.io/en/stable/operator-manual/applicationset/Progressive-Syncs/
[helmfile-configuration]: https://helmfile.readthedocs.io/en/latest/configuration/
[helmfile-kustomizations]:
  https://helmfile.readthedocs.io/en/latest/advanced-features/#deploy-kustomizations-with-helmfile
[helmfile-releases-dag]: https://helmfile.readthedocs.io/en/latest/releases/

## License

MIT
