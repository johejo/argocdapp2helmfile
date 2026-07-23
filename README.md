# argocdapp2helmfile

`argocdapp2helmfile` converts a single Argo CD `Application` that deploys a
Helm chart into a single-release helmfile. It is intended to be a small,
offline Unix filter: it reads YAML from standard input, writes YAML to standard
output, and does not fetch charts or repositories.

## Usage

```sh
go install github.com/johejo/argocdapp2helmfile@latest
argocdapp2helmfile <application.yaml >helmfile.yaml
```

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
```

`argocdapp2helmfile` produces:

```yaml
repositories:
  - name: source
    url: https://charts.bitnami.com/bitnami
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
```

The repository alias is always `source`. If `helm.releaseName` is absent, the
release name defaults to `metadata.name`.

## Supported mapping

The initial version accepts exactly one YAML document containing one
`argoproj.io/v1alpha1` `Application`. Its source must use `spec.source.chart`
and an HTTP or HTTPS Helm repository.

| Argo CD Application | helmfile |
| --- | --- |
| `metadata.name` | Release `name` when `helm.releaseName` is absent |
| `spec.source.helm.releaseName` | Release `name` |
| `spec.destination.namespace` | Release `namespace` |
| `spec.source.repoURL` | Repository `url` |
| `spec.source.chart` | Release chart as `source/<chart>` |
| `spec.source.targetRevision` | Release `version` |
| `spec.source.helm.values` | Parsed inline `values` entry |
| `spec.source.helm.valuesObject` | Inline `values` entry |
| `spec.source.helm.parameters` | Release `set` or `setString` entries |

For each parameter, `name` and the string `value` are preserved. Parameters
with `forceString: true` are emitted under `setString`; all other parameters
are emitted under `set`.

Values retain Argo CD's precedence: chart defaults, `values`, `valuesObject`,
then `parameters`, from lowest to highest precedence. The generated entries are
ordered so that helmfile applies the same precedence.

Argo CD operational settings that do not describe a Helm release, such as
`project` and `syncPolicy`, are not included in the generated helmfile. The
destination cluster in `spec.destination.server` or `spec.destination.name` is
also not converted: select the intended kube context when running helmfile.

## Current limitations

The initial version rejects inputs that require any of the following:

- multiple YAML documents, multiple Applications, `List`, or `ApplicationSet`;
- Git-hosted charts selected with `spec.source.path`;
- OCI Helm repositories;
- multi-source Applications using `spec.sources`;
- `valueFiles` or `fileParameters`; or
- non-empty Helm options not listed in the supported mapping above.

The required fields are `metadata.name`, `spec.source.repoURL`,
`spec.source.chart`, and `spec.source.targetRevision`. The repository URL must
be an HTTP or HTTPS URL. Empty unsupported Helm options are ignored.

These inputs fail explicitly instead of producing an incomplete helmfile.
Fields unrelated to describing or rendering the Helm release are ignored as
described above.

## Future considerations

The following capabilities may be useful additions after the initial format
and error behavior are established. This is not a committed roadmap:

- OCI Helm repositories;
- charts stored in Git repositories;
- `valueFiles` for a local or already-fetched chart;
- commonly used Helm options such as `fileParameters`,
  `ignoreMissingValueFiles`, and `skipCrds`;
- multi-source Applications and cross-repository `$values` references; and
- aggregating multiple Applications into one helmfile.

Supporting `valueFiles` is not just a matter of copying the path. Argo CD can
resolve a relative values path inside a fetched chart, whereas helmfile usually
resolves a values path from the local filesystem. Git-hosted charts and
multi-source `$values` references would therefore require explicit rules for
fetching repositories, pinning revisions, and resolving paths.

Generating Applications from an `ApplicationSet` and translating Argo CD
cluster identities into local kubeconfig contexts are intentional non-goals.
The tool should consume an already-rendered `Application`, and cluster selection
should remain under the helmfile user's control.

## License

MIT
