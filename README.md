# argocdapp2helmfile

`argocdapp2helmfile` converts Argo CD `Application` resources and
`ApplicationSet` resources using the List generator into one helmfile.
It is an offline Unix filter: it reads a YAML stream from standard input,
writes YAML to standard output, and never fetches charts or repositories.

An optional config maps Argo CD destinations to helmfile kube contexts, maps Git sources to paths available when helmfile runs, and projects Application fields into release labels.

## Quick start

```sh
go install github.com/johejo/argocdapp2helmfile@latest
argocdapp2helmfile <application.yaml >helmfile.yaml
```

Use `--config` for destination kube contexts, Git-hosted charts, external values repositories, or release labels:

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
| `spec.source.repoURL` | Repository `url`; scheme-less OCI also sets `oci: true` |
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
| `spec.destination.name` or `spec.destination.server` | Release `kubeContext` through Config `destinations` |
| Config `releaseLabels` query result | Release `labels` entry |

The required fields are `metadata.name`, the chart source's `repoURL` and
`targetRevision`, and either `chart` for a Helm repository or `path` for Git.

Argo CD documents Helm value precedence, from lowest to highest, as
`valueFiles`, `values`, `valuesObject`, then `parameters`.
The converter preserves that ordering in the generated `values`, `set`, and `setString`
entries.
Multiple value files and parameters retain input order.

`fileParameters` use Helm's `--set-file` behavior and are emitted after ordinary
parameters in `set`, so a same-name file parameter wins within that list.
A file parameter conflicting with a same-name `forceString` parameter is rejected:
helmfile cannot preserve ordering across `set` and `setString`.

### ApplicationSet List generator

ApplicationSets must enable Go templates and contain one or more List generators:

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

Supported List features are:

- nested YAML values in `elements`;
- literal `elementsYaml`;
- generator-level `template` overrides;
- selectors using `matchLabels` and the `In`, `NotIn`, `Exists`, and
  `DoesNotExist` operators;
- templating of every string field and string mapping key; and
- a YAML or JSON `templatePatch`.

Templates provide the Sprig functions used by ApplicationSet except `env`,
`expandenv`, and `getHostByName`.
They also provide `normalize`, `slugify`, `toYaml`, `fromYaml`, and `fromYamlArray`.
Supported Go template options are `missingkey=default`, `missingkey=invalid`,
`missingkey=zero`, and `missingkey=error`.

`templatePatch` is rendered once per selected List element with the same parameters, functions, and Go template options as `template`.
An empty or whitespace-only rendered patch has no effect.
Mappings merge recursively, while scalars and sequences replace the template value; `null` deletes a field.
New mapping keys are appended in patch order.
The patch is applied after generator-level and spec-level templates are merged and rendered, so patched metadata is also available to `releaseLabels` queries.
As in Argo CD, the pre-patch `spec.project` is always retained.

The rendered patch must be exactly one YAML or JSON document with a mapping at its root.
Strategic Merge Patch directives are not implemented: `$patch`, `$retainKeys`, `$setElementOrder/...`, and `$deleteFromPrimitiveList/...` are rejected wherever they occur instead of being interpreted with different semantics.

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

Each `destinations` item must contain exactly one non-empty `name` or `server` and a non-empty `kubeContext`.
Entries match the corresponding literal `spec.destination.name` or `spec.destination.server` by exact string equality.
The configured `kubeContext` is copied unchanged to the generated helmfile release; the converter does not read a kubeconfig or verify that the context exists.

For example, this Application destination:

```yaml
destination:
  name: production
  namespace: web
```

uses the `production` entry above and produces:

```yaml
namespace: web
kubeContext: prod-admin
```

Destination resolution is fail closed.
If an Application sets `name` or `server`, `--config` is required and a matching entry must exist.
Setting both `name` and `server` is rejected in both Config entries and Applications.
Duplicate Config entries with the same selector type and value are also rejected.
If both Application selector fields are empty, `kubeContext` is omitted for compatibility with inputs that do not identify a cluster.
For an ApplicationSet, resolution happens after template rendering and `templatePatch` application.

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

Path resolution follows these rules:

- a relative path starts at the Git chart directory;
- a leading `/` starts at that Git repository's root, not the OS root;
- the part after `$ref/` starts at the referenced repository's root;
- normalized paths may not escape their configured source; and
- input order is preserved.

The converter does not clone, fetch, inspect, or change repositories.
It does not evaluate roots, expand `~`, `$PWD`, or `${HOME}`, verify revisions,
or check whether a path exists.
Prepare every configured source in the environment where helmfile runs.

For templated roots, use a `.gotmpl` output and provide the value at runtime:

```sh
argocdapp2helmfile --config config.yaml \
  <application.yaml >helmfile.yaml.gotmpl
VALUES_ROOT="$PWD/checkouts/values" \
  helmfile -f helmfile.yaml.gotmpl apply
```

Value-file paths and globs are passed to helmfile for native resolution.
This differs from Argo CD's doublestar behavior: recursive `**` matching and
deduplication across explicit paths and globs are not guaranteed.
File parameters are passed as files without converter-side glob expansion.
`ignoreMissingValueFiles: true` becomes `missingFileHandler: Warn` and does not
apply to file parameters.

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

For the `argocd.skipTests` rule above, select releases according to the matching
helmfile runtime behavior:

```sh
helmfile --selector argocd.skipTests=false template
helmfile --selector argocd.skipTests=true template --skip-tests
```

This projection is opt-in.
`spec.source.helm.skipTests` is otherwise accepted and intentionally ignored
because it controls Argo CD's Helm invocation, not a helmfile release.

## Conversion behavior and constraints

- Repository aliases use the final non-empty URL path element, normalized to
  lowercase with unsafe character runs replaced by `-`.
  Query strings, fragments, and trailing slashes are ignored.
  An empty result falls back to `source`.
  Exact matching `repoURL` strings share an alias.
  When aliases for different URLs collide, later aliases receive the first available numeric suffix
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
- Argo CD operational fields such as `project` and `syncPolicy` are not converted.

The converter rejects:

- ApplicationSet generators other than List and legacy fasttemplate syntax;
- Strategic Merge Patch directives in ApplicationSet `templatePatch`;
- multi-source Applications outside the values-only `ref` form above;
- values-only sources with `path` or another manifest-generating configuration;
- non-`$ref` value files and file parameters for HTTP or OCI charts;
- remote URLs and Argo CD build-environment substitutions in `valueFiles` or
  `fileParameters`;
- unsafe Git paths, refs, or paths that escape a configured source;
- configured Application destinations without a matching Config entry;
- Applications that set both destination `name` and `server`;
- same-name file parameters and `forceString` parameters; and
- non-empty Helm options not listed in the mapping, except ignored `skipTests`.

Unsupported inputs fail instead of producing an incomplete helmfile.
Empty unsupported Helm options are ignored.

For upstream behavior, see also

- [Argo CD Helm documentation](https://argo-cd.readthedocs.io/en/latest/user-guide/helm/)
- [Argo CD Go Template documentation](https://argo-cd.readthedocs.io/en/latest/operator-manual/applicationset/GoTemplate/)
- [helmfile configuration reference](https://helmfile.readthedocs.io/en/latest/configuration/)

## License

MIT
