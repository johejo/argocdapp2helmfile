# Conversion config reference

The optional config is exactly one YAML document.
It uses a fixed API version and kind, and rejects unknown fields.
Any of `destinations`, `clusters`, `sources`, or `releaseLabels` may be omitted.
The config may be omitted when none of these features is needed.

## Fields

| Field | Required | Description |
| --- | --- | --- |
| `apiVersion` | yes | Must be `argocdapp2helmfile/v1alpha1`. |
| `kind` | yes | Must be `Config`. |
| `destinations` | no | Literal Argo CD destination-to-kube-context mappings. |
| `destinations[].name` | one selector | Exact `spec.destination.name` to match. |
| `destinations[].server` | one selector | Exact `spec.destination.server` to match. |
| `destinations[].kubeContext` | yes | Helmfile kube context to emit unchanged. |
| `clusters` | no | Offline cluster inventory used by destinations and ApplicationSets. |
| `clusters[].name` | yes | Unique cluster name and destination selector. |
| `clusters[].server` | yes | Unique cluster server and destination selector. |
| `clusters[].kubeContext` | yes | Helmfile kube context to emit unchanged. |
| `clusters[].project` | no | Argo CD project exposed to a clusters generator. |
| `clusters[].labels` | no | String labels exposed to selectors and templates. |
| `clusters[].annotations` | no | String annotations exposed to templates. |
| `sources` | no | Git source-to-local-root mappings. |
| `sources[].repoURL` | yes | Exact Application or generator repository URL. |
| `sources[].targetRevision` | yes | Normalized revision paired with `repoURL`. |
| `sources[].localRoot` | yes | Local path or Helmfile template expression. |
| `releaseLabels` | no | Ordered jq projections into Helmfile release labels. |
| `releaseLabels[].name` | yes | Unique Helmfile label name. |
| `releaseLabels[].query` | yes | jq expression evaluated against the final Application. |

`destinations[].name` and `destinations[].server` are mutually exclusive;
exactly one must be non-empty.
Cluster names and servers must be unique across `clusters` and `destinations`.
The `sources` pair of `repoURL` and `targetRevision` must be unique,
as must each `releaseLabels[].name`.

## Complete example

This example shows every available field:

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
    localRoot: "{{ requiredEnv \"VALUES_ROOT\" }}"
releaseLabels:
  - name: argocd.skipTests
    query: .spec.source.helm.skipTests // false
  - name: argocd.project
    query: .spec.project
```

## Destination kube contexts

Destination entries match the corresponding literal `spec.destination.name`
or `spec.destination.server` by exact string equality.
The configured `kubeContext` is copied unchanged to the generated Helmfile release;
the converter does not read a kubeconfig or verify that the context exists.
If an Application sets either field, `--config` and a matching entry are required.
If neither is set, `kubeContext` is omitted.
With `--kube-context-mode omit`, destination name and server resolution is disabled and
`kubeContext` is always omitted; `spec.destination.namespace` remains the release namespace.

Every `clusters` entry is also registered as a destination under both its exact
`name` and `server`.

## Creating a cluster snapshot

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
The default local cluster may have no Cluster Secret and is therefore absent from the dump.

## Git charts, Kustomizations, external values, and paths

A Config `sources` entry matches the Application's `repoURL` and normalized
`targetRevision` pair.
Its required `localRoot` is copied unchanged as the prefix for generated paths.
It may be absolute, relative, or contain a Helmfile template expression,
and the converter does not expand shell paths.
`localRoot` values need not be unique.
A Git source without a matching entry is an error.
Use `HEAD` explicitly when its `localRoot` is checked out at that revision.
Because `HEAD` can move, use a branch, tag, or commit SHA when that distinction matters.

For a Git-hosted chart, use `path` rather than `chart`:

```yaml
source:
  repoURL: git@github.com:example/platform-charts.git
  path: charts/my-app
  targetRevision: release-1
```

With the complete example, the release chart becomes
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

The supported and unsupported `kustomize` options, their Helmfile output, and the
transformer semantics are listed in the
[Application mapping reference](application-mapping.md),
also available with `--help-application-mapping`.
Unsupported options are rejected with the reason they cannot convert.
See [Argo CD's Kustomize options][argocd-kustomize-options] for upstream semantics.

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

For templated `localRoot` values, use a `.gotmpl` output and provide the value at runtime:

```sh
argocdapp2helmfile --config config.yaml \
  <application.yaml >helmfile.yaml.gotmpl
VALUES_ROOT="$PWD/checkouts/values" \
  helmfile -f helmfile.yaml.gotmpl apply
```

The converter expands `*`, `?`, character classes, and recursive `**` patterns
in value-file paths relative to the chart or `$ref` repository root.
A source used by a pattern must have an existing, non-symlink directory as its
Config `localRoot`; that directory cannot contain a Helmfile template expression.
Explicit value files are not checked against the local filesystem.

Matches retain doublestar traversal order rather than being globally sorted.
Duplicate normalized paths are removed.
Explicit paths take precedence over glob matches regardless of entry order.
Symlinks may point within the canonical source root but not outside it;
output retains the matched logical path.
A pattern matching no files is an error unless `ignoreMissingValueFiles: true`,
which omits the entry and sets `missingFileHandler: Warn`.

Statically known Argo CD build-environment variables are expanded in value-file paths,
and the remaining ones are rejected.
The variables, their expanded values, and the rejection reasons are listed in the
[Application mapping reference](application-mapping.md),
also available with `--help-application-mapping`.

File parameters are passed as files without converter-side glob expansion.
Build-environment variables follow the same expansion rules as value-file paths,
and `ignoreMissingValueFiles` does not apply to file parameters.

## Release labels

Each `releaseLabels` item contains a non-empty [jq](https://jqlang.org/manual/)
expression in `query`.
The query runs against each final Application, including those generated from
an ApplicationSet.

A single string, boolean, or number becomes a string label value.
`null` or no result omits the label.
Multiple results, arrays, objects, jq errors, and duplicate names fail conversion.
Before queries run, the complete Application is normalized to jq's data model.
YAML timestamps become RFC 3339 strings;
a date-only timestamp such as `!!timestamp 2020-01-01` becomes
`2020-01-01T00:00:00Z`.
Due to a YAML decoder limitation, integers outside the range
`-9223372036854775808` through `18446744073709551615` are exposed to jq as strings.
Unsupported YAML values such as binary data fail conversion even when no query selects them.
Labels retain config order.
No `labels` mapping is emitted when there are no rules or every result is omitted.

This projection is opt-in.
`spec.source.helm.skipTests` is otherwise accepted and intentionally ignored
because it controls Argo CD's Helm invocation, not a Helmfile release.

[argocd-kustomize-namespace]:
  https://argo-cd.readthedocs.io/en/stable/user-guide/kustomize/#setting-the-manifests-namespace
[argocd-kustomize-options]:
  https://argo-cd.readthedocs.io/en/stable/user-guide/kustomize/
[helmfile-kustomizations]:
  https://helmfile.readthedocs.io/en/latest/advanced-features/#deploy-kustomizations-with-helmfile
