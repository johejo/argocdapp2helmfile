# argocdapp2helmfile

`argocdapp2helmfile` converts one or more Argo CD `Application` resources that
deploy Helm charts into one helmfile. It is intended to be a small, offline
Unix filter: it reads a YAML stream from standard input, writes YAML to standard
output, and does not fetch charts or repositories. Git charts and external
values repositories can be supplied as pre-existing local checkouts.

## Usage

```sh
go install github.com/johejo/argocdapp2helmfile@latest
argocdapp2helmfile <application.yaml >helmfile.yaml.gotmpl
```

When the input uses a Git-hosted chart or external values repository, pass a
source map and set its environment variables:

```sh
CHARTS_ROOT="$PWD/checkouts/charts" \
VALUES_ROOT="$PWD/checkouts/values" \
argocdapp2helmfile --source-map sources.yaml \
  <application.yaml >helmfile.yaml.gotmpl
```

Use the `.gotmpl` suffix whenever a source map is used. Local paths use
helmfile's `requiredEnv` template function, so the same environment variables
must also be set when helmfile runs.

Diagnostics are written to standard error. If the input cannot be converted
without losing relevant Helm configuration, the command exits with a non-zero
status and does not write a partial helmfile to standard output.

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

Every YAML document must contain exactly one supported `Application`; empty
documents are rejected. A failure in any document produces no helmfile output.
Diagnostics for document-specific failures include a one-based document number.

Argo CD exposes `skipCrds` per Application, while helmfile exposes the matching
`skipCRDs` setting under the shared top-level `helmDefaults`. Consequently, all
Applications must have the same effective `skipCrds` value, with an absent value
treated as `false`. Conflicting values are rejected. Generated files containing
`helmDefaults.skipCRDs` require helmfile v1.3.0 or newer.

## Supported mapping

Each input document must contain one `argoproj.io/v1alpha1` `Application`. Its
source must identify either a packaged chart in an HTTP(S) or scheme-less OCI
Helm repository, or a chart directory in a Git repository.

| Argo CD Application | helmfile |
| --- | --- |
| `metadata.name` | Release `name` when `helm.releaseName` is absent |
| `spec.source.helm.releaseName` | Release `name` |
| `spec.destination.namespace` | Release `namespace` |
| `spec.source.repoURL` | Repository `url`; scheme-less OCI repositories also set `oci: true` |
| `spec.source.chart` | Release chart as `source/<chart>` |
| `spec.source.targetRevision` | Release `version` |
| Git `spec.source.repoURL` | Checkout identity; accepts HTTP(S), `git@host:path`, and `ssh://user@host/path` |
| Git `spec.source.path` | Chart root below the mapped local Git checkout |
| Git `spec.source.targetRevision` | Checkout provenance; not emitted as a Helm chart version |
| `spec.source.helm.valueFiles` | Resolved local files using Argo CD 3.4 path and glob semantics |
| `spec.source.helm.values` | Parsed inline `values` entry |
| `spec.source.helm.valuesObject` | Inline `values` entry |
| `spec.source.helm.parameters` | Release `set` or `setString` entries |
| `spec.source.helm.ignoreMissingValueFiles` | Release `missingFileHandler: Warn` when true |
| `spec.source.helm.skipSchemaValidation` | Release `skipSchemaValidation` |
| `spec.source.helm.skipCrds` | `helmDefaults.skipCRDs` |

For each parameter, `name` and the string `value` are preserved. Parameters
with `forceString: true` are emitted under `setString`; all other parameters
are emitted under `set`.

Values retain Argo CD's precedence: chart defaults, `valueFiles`, `values`,
`valuesObject`, then `parameters`, from lowest to highest precedence. Multiple
`valueFiles` retain their input order. The generated entries are ordered so
that helmfile applies the same precedence.

## Local Git source map

The converter never clones, fetches, or changes a repository. Describe existing
Git worktrees in a versioned YAML source map:

```yaml
apiVersion: argocdapp2helmfile/v1alpha1
kind: SourceMap
sources:
  - repoURL: git@github.com:example/platform-charts.git
    targetRevision: release-1
    env: PLATFORM_CHARTS_ROOT
  - repoURL: https://github.com/example/values.git
    targetRevision: main
    env: PLATFORM_VALUES_ROOT
    allowDirty: true
```

Entries match the Application's literal `repoURL` and `targetRevision` pair.
Environment variable names must be unique and their values must be absolute
paths to Git worktree roots. A source map may be omitted only when no local Git
source is needed. A required mapping or environment variable that is absent is
an error; there is no implicit `document-N` directory fallback.

For every used checkout, the converter verifies that `HEAD` resolves to
`targetRevision`. By default it also rejects staged or unstaged tracked changes,
including dirty submodules. Untracked files are allowed. Set `allowDirty: true`
on an entry to permit tracked changes without disabling the revision check.

### Git-hosted charts

For a Git-hosted chart, use the Argo CD form with `path` instead of `chart`:

```yaml
source:
  repoURL: git@github.com:example/platform-charts.git
  path: charts/my-app
  targetRevision: release-1
```

SCP-like `user@host:path`, `ssh://user@host/path`, and HTTP(S) Git URLs are
accepted. Set the mapped environment variable to a checkout of
`targetRevision`. The directory selected by `path` must contain `Chart.yaml`; a
`path` of `.` is accepted for a chart at the repository root.

The generated release refers directly to that directory:

```yaml
# document 1 chart source: repoURL "git@github.com:example/platform-charts.git", path "charts/my-app", targetRevision "release-1"
releases:
  - name: my-app
    chart: '{{ requiredEnv "PLATFORM_CHARTS_ROOT" }}/charts/my-app'
```

No Helm repository or release `version` is emitted: `targetRevision` selects a
Git revision, not the version in `Chart.yaml`. SSH keys, agents, and known-hosts
configuration remain the responsibility of the checkout environment.

### Multi-source values repositories

A multi-source Application is supported when it has exactly one Helm chart
source and all other sources are values-only sources with a unique `ref`. A
value file beginning with `$ref/` is resolved from the root of the corresponding
mapped checkout. The `$ref` token is accepted only at the start of the value
file path.

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

### Value file path behavior

Value paths follow Argo CD 3.4 filesystem behavior:

- a relative path is based at the Git chart directory;
- a leading `/` is based at that Git repository's root, not the OS root;
- the portion after `$ref/` is based at the referenced repository's root;
- `*`, `?`, `[]`, and `**` use doublestar expansion and lexical ordering;
- explicit paths take priority over glob matches, and duplicate resolved paths
  are emitted once; and
- normalized paths and symlink targets must remain inside their source
  repository.

Glob expansion happens during conversion so helmfile receives explicit files in
Argo CD precedence order. A missing explicit file or unmatched glob fails the
conversion unless `ignoreMissingValueFiles: true`, in which case it is omitted
and `missingFileHandler: Warn` is emitted.

Non-`$ref` value files are not supported for HTTP/OCI charts because those
charts remain remote. Argo CD build-environment substitutions in value paths and
remote values-file URLs are also rejected explicitly.

Argo CD operational settings that do not describe a Helm release, such as
`project` and `syncPolicy`, are not included in the generated helmfile. The
destination cluster in `spec.destination.server` or `spec.destination.name` is
also not converted: select the intended kube context when running helmfile.

## Current limitations

The converter rejects inputs that require any of the following:

- `List` or `ApplicationSet` resources;
- multi-source Applications outside the values-only `ref` form described
  above;
- non-`$ref` value files for HTTP/OCI charts;
- Argo CD build-environment substitutions and remote URLs in `valueFiles`;
- `fileParameters`; or
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

- commonly used Helm options such as `fileParameters` and `skipTests`;
- accepting `List` resources as an alternative batch input format.

Generating Applications from an `ApplicationSet` and translating Argo CD
cluster identities into local kubeconfig contexts are intentional non-goals.
The tool should consume an already-rendered `Application`, and cluster selection
should remain under the helmfile user's control.

## License

MIT
