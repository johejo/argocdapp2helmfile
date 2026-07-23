# argocdapp2helmfile

`argocdapp2helmfile` converts one or more Argo CD `Application` resources that
deploy Helm charts into one helmfile. It is intended to be a small, offline
Unix filter: it reads a YAML stream from standard input, writes YAML to standard
output, and does not fetch charts or repositories.

## Usage

```sh
go install github.com/johejo/argocdapp2helmfile@latest
argocdapp2helmfile <application.yaml >helmfile.yaml.gotmpl
```

Use the `.gotmpl` suffix whenever the input contains `helm.valueFiles`. Those
entries use helmfile's `requiredEnv` template function, so helmfile must render
the generated file as a template.

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
Helm repository, or a chart directory in an SSH Git repository.

| Argo CD Application | helmfile |
| --- | --- |
| `metadata.name` | Release `name` when `helm.releaseName` is absent |
| `spec.source.helm.releaseName` | Release `name` |
| `spec.destination.namespace` | Release `namespace` |
| `spec.source.repoURL` | Repository `url`; scheme-less OCI repositories also set `oci: true` |
| `spec.source.chart` | Release chart as `source/<chart>` |
| `spec.source.targetRevision` | Release `version` |
| SSH Git `spec.source.repoURL` | Checkout provenance; accepts `git@host:path` and `ssh://user@host/path` |
| Git `spec.source.path` | Chart root placed at `document-N/chart/` |
| Git `spec.source.targetRevision` | Checkout provenance; not emitted as a Helm chart version |
| `spec.source.helm.valueFiles` | Files below `document-N/chart/` under the configured values root |
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

## Git-hosted charts and external values files

The converter remains an offline Unix filter. It does not fetch, check out, or
copy a chart or values repository. Before running helmfile, arrange the files
under a values root using this public layout:

```text
$ARGOCDAPP2HELMFILE_VALUES_ROOT/
├── document-1/
│   ├── chart/
│   └── refs/
│       └── <ref>/
├── document-2/
│   ├── chart/
│   └── refs/
│       └── <ref>/
└── ...
```

`document-N` uses the one-based position of the Application in the input YAML
stream. Place the chart root beneath `chart/`, and place each referenced
repository root beneath `refs/<ref>/`. A regular value file such as
`environments/prod.yaml` is emitted as:

```yaml
values:
  - '{{ requiredEnv "ARGOCDAPP2HELMFILE_VALUES_ROOT" }}/document-1/chart/environments/prod.yaml'
```

Set the root when invoking helmfile:

```sh
ARGOCDAPP2HELMFILE_VALUES_ROOT="$PWD/values" helmfile -f helmfile.yaml.gotmpl apply
```

The environment variable is required instead of defaulting to an empty value.
With the default Argo CD behavior, a missing values file remains an error. When
`ignoreMissingValueFiles: true` is set, the release instead gets
`missingFileHandler: Warn`; the root environment variable is still required.

Only safe relative value file paths are accepted. Empty and absolute paths, `.`
or `..` path segments, empty segments, backslashes, and glob syntax are
rejected.

### Git-hosted charts

For a Git-hosted chart, use the Argo CD form with `path` instead of `chart`:

```yaml
source:
  repoURL: git@github.com:example/platform-charts.git
  path: charts/my-app
  targetRevision: release-1
```

SCP-like `user@host:path` and standard `ssh://user@host/path` URLs are accepted;
use the latter for a non-default SSH port. Check out `targetRevision` and copy
the chart root selected by `path` into `document-N/chart/`. A `path` of `.` is
accepted for a chart at the repository root.

The generated release refers directly to that directory:

```yaml
# document 1 chart source: repoURL "git@github.com:example/platform-charts.git", path "charts/my-app", targetRevision "release-1"
releases:
  - name: my-app
    chart: '{{ requiredEnv "ARGOCDAPP2HELMFILE_VALUES_ROOT" }}/document-1/chart'
```

No Helm repository or release `version` is emitted: `targetRevision` selects a
Git revision, not the version in `Chart.yaml`. SSH keys, agents, and known-hosts
configuration remain the responsibility of the checkout environment.

### Multi-source values repositories

A multi-source Application is supported when it has exactly one Helm chart
source and all other sources are values-only sources with a unique `ref`. A
value file beginning with `$ref/` is resolved from the corresponding
`document-N/refs/<ref>/` directory. The `$ref` token is accepted only at the
start of the value file path, matching Argo CD's convention.

For example, `$values/prod/values.yaml` becomes:

```yaml
values:
  - '{{ requiredEnv "ARGOCDAPP2HELMFILE_VALUES_ROOT" }}/document-1/refs/values/prod/values.yaml'
```

The generated file includes comments with each values source's `repoURL` and
`targetRevision`. Use that provenance to check out and place the correct
revision yourself. Undefined or duplicate refs and unsafe ref names are
rejected. A second chart source, a source without a ref, or a values source
with `path` is also rejected; the latter could generate additional manifests
that cannot be represented by this conversion.

Argo CD operational settings that do not describe a Helm release, such as
`project` and `syncPolicy`, are not included in the generated helmfile. The
destination cluster in `spec.destination.server` or `spec.destination.name` is
also not converted: select the intended kube context when running helmfile.

## Current limitations

The converter rejects inputs that require any of the following:

- `List` or `ApplicationSet` resources;
- multi-source Applications outside the values-only `ref` form described
  above;
- `fileParameters`; or
- non-empty Helm options not listed in the supported mapping above.

The required fields are `metadata.name`, the chart source's `repoURL` and
`targetRevision`, plus `chart` for a Helm repository or `path` for an SSH Git
repository. Helm repository URLs must be HTTP(S) or scheme-less OCI references;
in particular, `oci://` must not be included. Git chart URLs must use SCP-like
or `ssh://` syntax. Empty unsupported Helm options are ignored.

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
