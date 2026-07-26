# ApplicationSet reference

This reference describes the `ApplicationSet` features the converter accepts.
An ApplicationSet must contain one or more supported generators and a `spec.template`.
The converter expands the set locally, then applies the ordinary Application conversion and
validation rules to every generated Application;
the [Application mapping reference](application-mapping.md) describes those rules.
Generators and their elements retain input order.

Parameter names, selector operators, pattern matching, and merge precedence follow the
[Argo CD generator documentation][argocd-applicationset-generators].
The sections below list what this converter adds or restricts.

## Generators

Each entry in `spec.generators` contains exactly one generator and an optional `selector`.
Fields outside the accepted list are rejected.

| Generator | Accepted fields | Expansion |
| --- | --- | --- |
| `list` | `elements`, `elementsYaml`, `template` | One parameter set per element, `elements` before `elementsYaml` |
| `git` | `repoURL`, `revision`, exactly one non-empty `directories` or `files`, `pathParamPrefix`, `values`, `template`, `requeueAfterSeconds` | One parameter set per matching directory, or per mapping in each matching parameter file |
| `clusters` | `selector`, `values`, `template`, `flatList` | One parameter set per Config `clusters` entry, in declaration order |
| `matrix` | `generators` with exactly two children, `template` | Every combination of the children's parameter sets, the second child re-expanded for each parameter set of the first |
| `merge` | `mergeKeys`, `generators` with two or more children, `template` | The first child's parameter sets, each overridden by later children whose `mergeKeys` values match |

`requeueAfterSeconds` is accepted and ignored because a one-shot filter has nothing to
requeue.
`spec.applyNestedSelectors` is also accepted and ignored because selectors always apply at
every depth.

## Unsupported generators

Naming any of these generators is a conversion error.
The rejection reason is reported in the error message.

| Generator | Rejection reason |
| --- | --- |
| `scmProvider` | listing repositories requires querying a live SCM provider |
| `pullRequest` | listing pull requests requires querying a live SCM provider |
| `clusterDecisionResource` | the generator reads a duck-typed resource from a live cluster |
| `plugin` | the generator calls a plugin service over HTTP |

Each of them resolves its parameters over the network, which an offline filter cannot do.

## Go template options

`spec.goTemplate: true` selects Go templates.
Otherwise fields are rendered with the legacy fasttemplate syntax, which ignores
`spec.goTemplateOptions`.

| Go template option | Rendering behavior |
| --- | --- |
| `missingkey=default` | A missing map key renders `<no value>` |
| `missingkey=invalid` | Same as `missingkey=default` |
| `missingkey=zero` | A missing map key renders the zero value of the map's element type |
| `missingkey=error` | A missing map key fails the conversion |

An empty option, and any option outside this table, is rejected.

## Nesting, selectors, and templates

`matrix` requires exactly two children and `merge` accepts two or more.
Either may contain one `matrix` or `merge` child one level deep,
and those nested combination generators may contain only `list`, `git`, or `clusters`
children.
A child of a combination generator that produces no parameters is an error.

Generator-level `template` overrides are supported only for top-level generators,
and their fields take precedence over `spec.template`.
A `selector` applies at every level and filters the parameter sets its own generator
produced.

## Git and Cluster generator inputs

The `git` generator never fetches:
its `repoURL` and `revision` must exactly match a Config `sources` entry,
whose `localRoot` must resolve from the converter's current working directory to an existing
non-symlink directory without helmfile template expressions.

The `clusters` generator expands the Config `clusters` snapshot in declaration order and
therefore requires `--config`;
an omitted or empty inventory generates no Applications.
`flatList: true` is not supported.

## Template patches

A rendered `spec.templatePatch` must be one YAML or JSON mapping,
and Strategic Merge Patch directives are rejected.
The patch is applied after template rendering,
so patched metadata is available to Config `releaseLabels` queries,
while the pre-patch `spec.project` is always retained.

## RollingSync strategy

`spec.strategy.type: RollingSync` becomes [helmfile `needs`][helmfile-releases-dag] step by
step;
configurations that cannot be expressed that way are rejected with an explanatory error.
Unlike [Argo CD Progressive Syncs][argocd-progressive-syncs],
helmfile does not wait for Application health or reproduce manual gates and partial
concurrency,
and label selectors ignore `needs` unless `--include-transitive-needs` is set.

[argocd-applicationset-generators]:
  https://argo-cd.readthedocs.io/en/stable/operator-manual/applicationset/Generators/
[argocd-progressive-syncs]:
  https://argo-cd.readthedocs.io/en/stable/operator-manual/applicationset/Progressive-Syncs/
[helmfile-releases-dag]: https://helmfile.readthedocs.io/en/latest/releases/
