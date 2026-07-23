# argocdapp2helmfile

`argocdapp2helmfile` converts one or more Argo CD `Application` resources, or
`ApplicationSet` resources using the List generator, into one helmfile. It is
intended to be a small, offline Unix filter: it reads a YAML stream from
standard input, writes YAML to standard output, and does not fetch charts or
repositories. A source map describes how Git charts and external values
repositories should be referenced by helmfile.

## Usage

```sh
go install github.com/johejo/argocdapp2helmfile@latest
argocdapp2helmfile <application.yaml >helmfile.yaml.gotmpl
```

When the input uses a Git-hosted chart or external values repository, pass a
source map:

```sh
argocdapp2helmfile --source-map sources.yaml \
  <application.yaml >helmfile.yaml.gotmpl
```

Source-map roots are copied into the generated paths without being evaluated.
If they contain helmfile template expressions, use the `.gotmpl` suffix and
provide their inputs when helmfile runs:

```sh
CHARTS_ROOT="$PWD/checkouts/charts" \
VALUES_ROOT="$PWD/checkouts/values" \
helmfile -f helmfile.yaml.gotmpl apply
```

Diagnostics are written to standard error. If the input cannot be converted
without losing relevant Helm configuration, the command exits with a non-zero
status and does not write a partial helmfile to standard output.

Kubernetes `List` wrappers are not accepted directly. Expand `items` into a
YAML stream before conversion, for example:

```sh
yq '.items[]' applications.yaml | argocdapp2helmfile
```

## Example

Given this Argo CD `Application`:

```yaml
apiVersion: argoproj.io/v1alpha1
kind: Application
metadata:
  name: nginx
spec:
  destination:
    namespace: web
    server: https://kubernetes.default.svc
  source:
    repoURL: https://charts.bitnami.com/bitnami
    chart: nginx
    targetRevision: 18.2.4
    helm:
      releaseName: edge
      values: |
        service:
          type: ClusterIP
      valuesObject:
        service:
          annotations:
            example.com/owner: platform
      parameters:
        - name: replicaCount
          value: "2"
        - name: image.tag
          value: "001"
          forceString: true
      skipSchemaValidation: true
      skipCrds: true
```

`argocdapp2helmfile` produces:

```yaml
repositories:
  - name: source
    url: https://charts.bitnami.com/bitnami
helmDefaults:
  skipCRDs: true
releases:
  - name: edge
    namespace: web
    chart: source/nginx
    version: 18.2.4
    values:
      - service:
          type: ClusterIP
      - service:
          annotations:
            example.com/owner: platform
    set:
      - name: replicaCount
        value: "2"
    setString:
      - name: image.tag
        value: "001"
    skipSchemaValidation: true
```

Repository aliases are assigned in first-use order as `source`, `source-2`,
`source-3`, and so on. Applications whose `repoURL` strings match exactly share
one repository entry and alias. If `helm.releaseName` is absent, the release
name defaults to `metadata.name`. Resolved release names must be unique across
the entire generated helmfile, including releases in different namespaces.

OCI Helm repositories use the scheme-less form accepted by Argo CD. For
example, this source:

```yaml
source:
  repoURL: registry-1.docker.io/bitnamicharts
  chart: nginx
  targetRevision: 15.9.0
```

produces an OCI-enabled helmfile repository while retaining the same release
chart alias:

```yaml
repositories:
  - name: source
    url: registry-1.docker.io/bitnamicharts
    oci: true
releases:
  - name: nginx
    chart: source/nginx
    version: 15.9.0
```

## Multiple Applications

Separate Applications with YAML document markers to aggregate them. Releases
retain document order in the generated helmfile:

```yaml
apiVersion: argoproj.io/v1alpha1
kind: Application
metadata:
  name: frontend
spec:
  source:
    repoURL: https://example.com/charts
    chart: frontend
    targetRevision: 1.2.3
---
apiVersion: argoproj.io/v1alpha1
kind: Application
metadata:
  name: backend
spec:
  source:
    repoURL: https://example.com/charts
    chart: backend
    targetRevision: 4.5.6
```

Every YAML document must contain exactly one supported `Application` or
`ApplicationSet`; the two kinds may be mixed in one stream. Empty documents are
rejected. A failure in any document produces no helmfile output. Diagnostics
for document-specific failures include a one-based document number.

Argo CD exposes `skipCrds` per Application, while helmfile exposes the matching
`skipCRDs` setting under the shared top-level `helmDefaults`. Consequently, all
Applications must have the same effective `skipCrds` value, with an absent value
treated as `false`. Conflicting values are rejected. Generated files containing
`helmDefaults.skipCRDs` require helmfile v1.3.0 or newer.

## ApplicationSet List generator

An `ApplicationSet` is expanded locally before its generated Applications are
converted. ApplicationSets must enable Go templating and use one or more List
generators:

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

List `elements` may contain nested YAML values. Literal `elementsYaml`,
generator-level `template` overrides, and post-generator `selector` rules using
`matchLabels` or the `In`, `NotIn`, `Exists`, and `DoesNotExist` operators are
also supported. Multiple generators and elements retain their input order.

Templates are evaluated independently for every string field and string mapping
key. They provide the Sprig function set used by ApplicationSet, except for
`env`, `expandenv`, and `getHostByName`, plus `normalize`, `slugify`, `toYaml`,
`fromYaml`, and `fromYamlArray`. Only Go templates are accepted; the legacy
fasttemplate syntax is rejected. `templatePatch` is not supported.

Generated Applications use the same conversion and validation rules as direct
Application input. Source maps therefore also apply to rendered Git chart and
external values sources. If conversion fails, diagnostics identify the
document, generator, and `elements` or `elementsYaml` index.

List elements commonly contain destination cluster information, but
`spec.destination.server` and `spec.destination.name` remain operational
settings that are not converted. Select the kube context when running helmfile.
Resolved release names must still be unique across direct and generated
Applications.

## Supported mapping

Each direct or generated `argoproj.io/v1alpha1` `Application` source must
identify either a packaged chart in an HTTP(S) or scheme-less OCI Helm
repository, or a chart directory in a Git repository.

| Argo CD Application | helmfile |
| --- | --- |
| `metadata.name` | Release `name` when `helm.releaseName` is absent |
| `spec.source.helm.releaseName` | Release `name` |
| `spec.destination.namespace` | Release `namespace` |
| `spec.source.repoURL` | Repository `url`; scheme-less OCI repositories also set `oci: true` |
| `spec.source.chart` | Release chart as `source/<chart>` |
| `spec.source.targetRevision` | Release `version` |
| Git `spec.source.repoURL` | Source identity; accepts HTTP(S), `git@host:path`, and `ssh://user@host/path` |
| Git `spec.source.path` | Chart path below the mapped source root |
| Git `spec.source.targetRevision` | Source-map identity and provenance; not emitted as a Helm chart version |
| `spec.source.helm.valueFiles` | Source-relative paths and globs resolved by helmfile |
| `spec.source.helm.values` | Parsed inline `values` entry |
| `spec.source.helm.valuesObject` | Inline `values` entry |
| `spec.source.helm.parameters` | Release `set` or `setString` entries |
| `spec.source.helm.fileParameters` | Release `set` entries using `file` |
| `spec.source.helm.ignoreMissingValueFiles` | Release `missingFileHandler: Warn` when true |
| `spec.source.helm.skipSchemaValidation` | Release `skipSchemaValidation` |
| `spec.source.helm.skipCrds` | `helmDefaults.skipCRDs` |

For each parameter, `name` and the string `value` are preserved. Parameters
with `forceString: true` are emitted under `setString`; all other parameters
are emitted under `set`. File parameters preserve `name` and resolve `path`
into a `set` entry using helmfile's `file` form:

```yaml
set:
  - name: config.raw
    file: '{{ requiredEnv "PLATFORM_CHARTS_ROOT" }}/charts/my-app/files/config.json'
```

Values retain Argo CD's precedence: chart defaults, `valueFiles`, `values`,
`valuesObject`, `parameters`, then `fileParameters`, from lowest to highest
precedence. Multiple `valueFiles` and file parameters retain their input order.
Normal parameters are emitted before file parameters in `set`, so a file
parameter with the same name wins. A file parameter that has the same name as
a parameter with `forceString: true` is rejected because helmfile cannot
reproduce Argo CD's precedence across `set` and `setString`.

## Git source map

The converter never clones, fetches, changes, or inspects a repository. Map each
Git source to the string that should prefix its paths in the generated helmfile:

```yaml
apiVersion: argocdapp2helmfile/v1alpha1
kind: SourceMap
sources:
  - repoURL: git@github.com:example/platform-charts.git
    targetRevision: release-1
    root: '{{ requiredEnv "PLATFORM_CHARTS_ROOT" }}'
  - repoURL: https://github.com/example/values.git
    targetRevision: main
    root: '{{ requiredEnv "PLATFORM_VALUES_ROOT" }}'
```

Entries match the Application's literal `repoURL` and `targetRevision` pair.
`root` is a required, non-empty string; it may be a fixed path, a helmfile
template expression, or a combination of both. Roots need not be unique. A
source map may be omitted only when no Git source is needed. A required mapping
that is absent is an error.

The converter does not evaluate templates or verify that a root exists,
contains a Git worktree at `targetRevision`, or has a clean status. Preparing
the correct sources is the responsibility of the helmfile execution
environment.

### Git-hosted charts

For a Git-hosted chart, use the Argo CD form with `path` instead of `chart`:

```yaml
source:
  repoURL: git@github.com:example/platform-charts.git
  path: charts/my-app
  targetRevision: release-1
```

SCP-like `user@host:path`, `ssh://user@host/path`, and HTTP(S) Git URLs are
accepted. The mapped root should resolve to the corresponding source when
helmfile runs. A `path` of `.` refers directly to the mapped root.

The generated release refers directly to that directory:

```yaml
# document 1 chart source: repoURL "git@github.com:example/platform-charts.git", path "charts/my-app", targetRevision "release-1"
releases:
  - name: my-app
    chart: '{{ requiredEnv "PLATFORM_CHARTS_ROOT" }}/charts/my-app'
```

No Helm repository or release `version` is emitted: `targetRevision` selects a
Git revision, not the version in `Chart.yaml`. SSH keys, agents, and known-hosts
configuration remain the responsibility of the helmfile execution environment.

### Multi-source values repositories

A multi-source Application is supported when it has exactly one Helm chart
source and all other sources are values-only sources with a unique `ref`. A
value file or file-parameter path beginning with `$ref/` is resolved from the
root of the corresponding mapped source. The `$ref` token is accepted only at
the start of the path.

For example, `$values/prod/values.yaml` becomes:

```yaml
values:
  - '{{ requiredEnv "PLATFORM_VALUES_ROOT" }}/prod/values.yaml'
```

The generated file includes comments with each values source's `repoURL` and
`targetRevision`. Undefined or duplicate refs and unsafe ref names are rejected.
A second chart source, a source without a ref, or a values source with `path` is
also rejected; the latter could generate additional manifests that cannot be
represented by this conversion.

### Value and file-parameter path behavior

Value-file and file-parameter paths retain Argo CD's source-relative
interpretation:

- a relative path is based at the Git chart directory;
- a leading `/` is based at that Git repository's root, not the OS root;
- the portion after `$ref/` is based at the referenced repository's root;
- input order is preserved; and
- normalized paths must not escape their mapped source.

The converter does not inspect files or expand paths. It emits value paths and
glob patterns for helmfile to resolve at execution time using its native glob
behavior. This is not identical to Argo CD's doublestar behavior: recursive
`**` matching and deduplication across explicit paths and globs are not
guaranteed. File-parameter paths are passed to helmfile as files without glob
expansion by the converter. When `ignoreMissingValueFiles: true` is set, the
converter emits `missingFileHandler: Warn`; otherwise helmfile's default
missing-file behavior applies. This setting does not apply to file parameters.

Non-`$ref` value files and file parameters are not supported for HTTP/OCI
charts because those charts remain remote and the converter does not unpack
them. Argo CD build-environment substitutions and remote URLs in either kind of
path are also rejected explicitly.

Argo CD operational settings that do not describe a Helm release, such as
`project` and `syncPolicy`, are not included in the generated helmfile. The
destination cluster in `spec.destination.server` or `spec.destination.name` is
also not converted: select the intended kube context when running helmfile.

## Current limitations

The converter rejects inputs that require any of the following:

- ApplicationSet generators other than List, legacy fasttemplate rendering, or
  `templatePatch`;
- multi-source Applications outside the values-only `ref` form described
  above;
- non-`$ref` value files or file parameters for HTTP/OCI charts;
- Argo CD build-environment substitutions and remote URLs in `valueFiles` or
  `fileParameters`;
- same-name `fileParameters` and `parameters` using `forceString: true`; or
- non-empty Helm options not listed in the supported mapping above.

The required fields are `metadata.name`, the chart source's `repoURL` and
`targetRevision`, plus `chart` for a Helm repository or `path` for a Git
repository. Helm repository URLs must be HTTP(S) or scheme-less OCI references;
in particular, `oci://` must not be included. Git chart URLs may use HTTP(S),
SCP-like, or `ssh://` syntax. Empty unsupported Helm options are ignored.

These inputs fail explicitly instead of producing an incomplete helmfile.
Fields unrelated to describing or rendering the Helm release are ignored as
described above.

## Future considerations

The following capabilities may be useful additions after the initial format
and error behavior are established. This is not a committed roadmap:

- commonly used Helm options such as `skipTests`.

Additional ApplicationSet generators and translating Argo CD cluster identities
into local kubeconfig contexts are not currently planned. Cluster selection
should remain under the helmfile user's control.

## License

MIT
