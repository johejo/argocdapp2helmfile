# argocdapp2helmfile

`argocdapp2helmfile` converts Argo CD `Application` resources and
`ApplicationSet` resources using List, Git, or supported Matrix generators
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

Use `--config` for destination kube contexts, Git-hosted charts,
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
- a chart directory in a Git repository.

### Application mapping

| Argo CD Application | helmfile |
| --- | --- |
| `metadata.name` | Release `name` when `helm.releaseName` is absent |
| `spec.source.helm.releaseName` | Release `name` |
| `spec.destination.namespace` | Release `namespace` |
| `spec.source.helm.namespace` | Accepted only when it exactly matches `spec.destination.namespace`; no additional output |
| `spec.source.helm.version` | Accepted and intentionally ignored |
| `spec.source.repoURL` | Repository `url`; scheme-less OCI also sets `oci: true` |
| Packaged chart `spec.source.helm.passCredentials` | Repository `passCredentials: true` when true |
| `spec.source.chart` | Release chart as `<alias>/<chart>` |
| `spec.source.targetRevision` for a packaged chart | Release `version` |
| Git `spec.source.repoURL` | Config source identity; HTTP(S), `git@host:path`, or `ssh://user@host/path` |
| Git `spec.source.path` | Chart path below the configured source root |
| Git `spec.source.targetRevision` | Config source identity and provenance, not a chart version |
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

The required fields are `metadata.name`, the chart source's `repoURL` and
`targetRevision`, and either `chart` for a Helm repository or `path` for Git.

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
- generator-level `template` overrides outside Matrix;
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
whose `root` is explored from the converter's current working directory.

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

The root must be an existing non-symlink directory without helmfile template expressions.
Hidden directories are skipped by directory generators;
`.git` and symlinks are skipped by file generators.
See the
[Argo CD Git generator documentation](https://argo-cd.readthedocs.io/en/stable/operator-manual/applicationset/Generators-Git/)
for the upstream generator model.

#### Matrix generator

Matrix supports every two-child combination of List, Git, and a one-level nested Matrix.
Each nested Matrix must itself have exactly two List or Git children;
a third Matrix level is rejected.

The first child's parameters may be used to render the second child,
including dynamic List `elementsYaml` and Git fields.
Git `values` may reference parent, Git path, and Git parameter-file fields together.
Results retain child order,
and the first child's values take precedence recursively when parameter maps overlap.

For Git × Git,
`pathParamPrefix` is recommended when both children need to retain their path parameters.
Without prefixes,
the normal first-child-wins merge behavior applies to the shared `path` key.
A top-level Matrix `template` is supported;
child-generator and nested Matrix templates are rejected.
Selectors apply at every generator level.
See the
[Argo CD Matrix generator documentation](https://argo-cd.readthedocs.io/en/stable/operator-manual/applicationset/Generators-Matrix/)
for the upstream generator constraints and parameter model.

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
sources:
  - repoURL: git@github.com:example/platform-charts.git
    targetRevision: release-1
    root: checkouts/platform-charts
  - repoURL: https://github.com/example/values.git
    targetRevision: main
    root: '{{ requiredEnv "VALUES_ROOT" }}'
releaseLabels:
  - name: argocd.skipTests
    query: .spec.source.helm.skipTests // false
  - name: argocd.project
    query: .spec.project
```

Any of `destinations`, `sources`, or `releaseLabels` may be omitted.
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

### Git charts, external values, and paths

A `sources` entry matches the Application's literal `repoURL` and
`targetRevision` pair.
Its required `root` is copied unchanged as the prefix for generated paths.
It may be absolute, relative, or contain a helmfile template expression.
Roots need not be unique.
A Git source without a matching entry is an error.

For a Git-hosted chart, use `path` rather than `chart`:

```yaml
source:
  repoURL: git@github.com:example/platform-charts.git
  path: charts/my-app
  targetRevision: release-1
```

With the config above, the release chart becomes
`checkouts/platform-charts/charts/my-app`.
A path of `.` refers to the configured root.
No repository or release `version` is emitted because `targetRevision` selects
the Git source, not the version in `Chart.yaml`.

A multi-source Application is supported when exactly one source is a Helm chart
and every other source is a values-only source with a unique `ref`.
For example, `$values/prod/values.yaml` resolves to
`{{ requiredEnv "VALUES_ROOT" }}/prod/values.yaml`.
The `$ref` token is valid only at the start of a value-file or file-parameter path.
Generated output includes provenance comments for Git chart and values sources.
Packaged charts require `$ref` paths;
remote value-file and file-parameter URLs are not supported.

Path resolution follows these rules:

- a relative path starts at the Git chart directory;
- a leading `/` starts at that Git repository's root, not the OS root;
- the part after `$ref/` starts at the referenced repository's root;
- normalized paths may not escape their configured source; and
- explicit paths retain input order.

Roots are used as written;
the converter does not expand shell paths or check explicit value-file paths.

For templated roots, use a `.gotmpl` output and provide the value at runtime:

```sh
argocdapp2helmfile --config config.yaml \
  <application.yaml >helmfile.yaml.gotmpl
VALUES_ROOT="$PWD/checkouts/values" \
  helmfile -f helmfile.yaml.gotmpl apply
```

The converter expands `*`, `?`, character classes, and recursive `**` patterns
in value-file paths relative to the chart or `$ref` repository root.
A source used by a pattern must have an existing, non-symlink directory as its
Config `root`; that root cannot contain a helmfile template expression.
Explicit value files do not impose this local-root requirement.

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
`spec.source.helm.version` is also accepted regardless of its value or type
and intentionally ignored as an Argo CD backward-compatibility field.

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
- `skipCrds` is per Application in Argo CD but `helmDefaults.skipCRDs` is shared.
  Every Application must therefore have the same effective value, with omission
  treated as `false`.
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
[argocd-go-template]:
  https://argo-cd.readthedocs.io/en/latest/operator-manual/applicationset/GoTemplate/
[argocd-create-namespace]:
  https://argo-cd.readthedocs.io/en/stable/user-guide/sync-options/#create-namespace
[helmfile-configuration]: https://helmfile.readthedocs.io/en/latest/configuration/

## License

MIT
