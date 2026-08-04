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

Run `argocdapp2helmfile --help` to list all command-line options.
Run `argocdapp2helmfile --version` to print the version.
Release builds can embed it with `go build -ldflags '-X main.version=v1.2.3' .`;
otherwise, the version reported by Go build information is used.

Separate multiple resources with YAML document markers.
Direct Applications and ApplicationSets may be mixed, and release order follows input order.
A Kubernetes `List` wrapper is not accepted directly; expand it first:

```sh
yq '.items[]' applications.yaml | argocdapp2helmfile
```

Diagnostics go to standard error.
Lossy synchronization settings warn by default; `--strict` rejects them.
Conversion errors never write a partial helmfile,
unless `--skip-unconvertible` asks for one.
See the [diagnostic reference](docs/diagnostics.md), also available with
`--help-diagnostics`, for all rules and examples.

## Reviewing what an ApplicationSet expands to

The generated helmfile provides an offline, normalized list of releases for review.
Use `--skip-unconvertible` to convert supported input while reporting omissions on standard error
and in comments at the top of the helmfile:

```sh
argocdapp2helmfile --skip-unconvertible <applications.yaml >helmfile.yaml
```

```
# skipped document 2: spec.generators[0].scmProvider generator is not supported: ...
repositories:
  - name: charts
    url: https://example.com/charts
...
```

A document that cannot be decoded or expanded is skipped whole.
Otherwise, each generated Application is skipped independently without affecting later releases.

| Exit code | Meaning |
| --- | --- |
| 0 | every input converted |
| 1 | nothing was written |
| 2 | a helmfile was written, and some input was skipped |

Exit 2 is only possible with `--skip-unconvertible`.
Skipping every input writes nothing and exits 1.
`--strict` and `--skip-unconvertible` are mutually exclusive.

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

See the generated [Application mapping reference](docs/application-mapping.md),
also available with `--help-application-mapping`.

### ApplicationSet

An ApplicationSet must contain one or more List, Git, Cluster, Matrix, or Merge generators.
The converter expands the set locally,
then applies the normal Application conversion and validation rules to every result.
Generators and elements retain their input order.

ApplicationSets may use either Go templates or the legacy fasttemplate syntax;
set `spec.goTemplate: true` to use Go templates:

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

The accepted generators and their fields, the unsupported generators, the supported
`goTemplateOptions`, and the nesting, `templatePatch`, and
`RollingSync` requirements are listed in the generated
[ApplicationSet reference](docs/applicationset.md),
also available with `--help-applicationset`.
Unsupported generators are rejected with the reason they cannot convert.
See the [Argo CD generator documentation][argocd-applicationset-generators] for the upstream
parameter names, selector operators, pattern matching, and merge precedence.

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

The supported and unsupported `kustomize` options, their Helmfile output, and the
transformer semantics are listed in the
[Application mapping reference](docs/application-mapping.md),
also available with `--help-application-mapping`.
Unsupported options are rejected with the reason they cannot convert.
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

Statically known Argo CD build-environment variables are expanded in value-file
paths, and the remaining ones are rejected.
The variables, their expanded values, and the rejection reasons are listed in the
[Application mapping reference](docs/application-mapping.md),
also available with `--help-application-mapping`.

File parameters are passed as files without converter-side glob expansion.
Build-environment variables follow the same expansion rules as value-file paths,
and `ignoreMissingValueFiles` does not apply to file parameters.

### Release labels

Each `releaseLabels` item has a unique non-empty `name` and a non-empty
[jq](https://jqlang.org/manual/) expression in `query`.
The query runs against each final Application, including those generated from
an ApplicationSet.

A single string, boolean, or number becomes a string label value.
`null` or no result omits the label.
Multiple results, arrays, objects, jq errors, and duplicate names fail conversion.
Before queries run, the complete Application is normalized to jq's data model.
YAML timestamps become RFC 3339 strings;
a date-only timestamp such as `!!timestamp 2020-01-01` becomes `2020-01-01T00:00:00Z`.
Due to a YAML decoder limitation, integers outside the range
`-9223372036854775808` through `18446744073709551615` are exposed to jq as strings.
Unsupported YAML values such as binary data fail conversion even when no query selects them.
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

Unsupported inputs fail instead of producing an incomplete helmfile,
unless `--skip-unconvertible` is given.
Supported boolean Helm options accept booleans only, with an explicit `null` treated as omission;
empty strings, sequences, and mappings are rejected.
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
[argocd-applicationset-generators]:
  https://argo-cd.readthedocs.io/en/stable/operator-manual/applicationset/Generators/
[argocd-go-template]:
  https://argo-cd.readthedocs.io/en/latest/operator-manual/applicationset/GoTemplate/
[argocd-create-namespace]:
  https://argo-cd.readthedocs.io/en/stable/user-guide/sync-options/#create-namespace
[argocd-kustomize-namespace]:
  https://argo-cd.readthedocs.io/en/stable/user-guide/kustomize/#setting-the-manifests-namespace
[argocd-kustomize-options]:
  https://argo-cd.readthedocs.io/en/stable/user-guide/kustomize/
[helmfile-configuration]: https://helmfile.readthedocs.io/en/latest/configuration/
[helmfile-kustomizations]:
  https://helmfile.readthedocs.io/en/latest/advanced-features/#deploy-kustomizations-with-helmfile

## License

MIT
