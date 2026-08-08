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

Destination names and servers are mapped to Helmfile `kubeContext` values by default.
When generating a Helmfile that should use Helmfile's execution context, such as for
`helmfile template`, omit `kubeContext` explicitly:

```sh
argocdapp2helmfile --kube-context-mode omit \
  <application.yaml >helmfile.yaml
```

This mode still converts `spec.destination.namespace` to the release namespace.

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

The optional conversion config maps Git sources and destination kube contexts,
provides an offline cluster inventory, and projects Application fields into release labels.
See the generated [Conversion config reference](docs/conversion-config.md),
also available with `--help-conversion-config`, for the complete schema, validation rules,
examples, and path-resolution behavior.

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
[helmfile-configuration]: https://helmfile.readthedocs.io/en/latest/configuration/

## License

MIT
