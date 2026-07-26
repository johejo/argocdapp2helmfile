package applicationmapping

import "github.com/johejo/argocdapp2helmfile/internal/catalog"

type ID string

type HelmValueKind string

const (
	String         HelmValueKind = "string"
	Boolean        HelmValueKind = "boolean"
	StringSequence HelmValueKind = "string-sequence"
	InlineValues   HelmValueKind = "inline-values"
	RawValues      HelmValueKind = "raw-values"
	Parameters     HelmValueKind = "parameters"
	FileParameters HelmValueKind = "file-parameters"
	Ignored        HelmValueKind = "ignored"
)

// KustomizeUnsupported is a value kind so that one lookup decides both whether an
// option is known and whether it converts.
type KustomizeValueKind string

const (
	KustomizeString      KustomizeValueKind = "string"
	KustomizeBoolean     KustomizeValueKind = "boolean"
	KustomizeStringMap   KustomizeValueKind = "string-map"
	KustomizeImages      KustomizeValueKind = "images"
	KustomizeReplicas    KustomizeValueKind = "replicas"
	KustomizeUnsupported KustomizeValueKind = "unsupported"
)

// BuildEnvironmentKind is a value kind so that one lookup decides both whether a
// build environment variable is known and whether it can be expanded.
type BuildEnvironmentKind string

const (
	BuildEnvironmentStatic  BuildEnvironmentKind = "static"
	BuildEnvironmentDynamic BuildEnvironmentKind = "dynamic"
)

type Entry struct {
	ID            ID
	Input         string
	Output        string
	Notes         string
	HelmOption    string
	HelmValueKind HelmValueKind
	AllowEmpty    bool
	References    []ID
}

// Output is Markdown for the reference, Reason is plain text for rejection errors,
// and exactly one of them is set.
type KustomizeOption struct {
	Name      string
	ValueKind KustomizeValueKind
	Output    string
	Reason    string
}

// Source describes where a static value comes from, Reason describes why a dynamic
// variable cannot be expanded, and exactly one of them is set.
type BuildEnvironmentVariable struct {
	Name   string
	Kind   BuildEnvironmentKind
	Source string
	Reason string
}

const (
	MetadataName            ID = "metadata-name"
	HelmReleaseName         ID = "helm-release-name"
	DestinationNamespace    ID = "destination-namespace"
	RevisionHistoryLimit    ID = "revision-history-limit"
	HelmNamespace           ID = "helm-namespace"
	HelmVersion             ID = "helm-version"
	HelmSkipTests           ID = "helm-skip-tests"
	RepositoryURL           ID = "repository-url"
	HelmPassCredentials     ID = "helm-pass-credentials"
	Chart                   ID = "chart"
	PackagedTargetRevision  ID = "packaged-target-revision"
	GitRepositoryURL        ID = "git-repository-url"
	GitPath                 ID = "git-path"
	GitTargetRevision       ID = "git-target-revision"
	KustomizeOptionsEntry   ID = "kustomize-options"
	HelmValueFiles          ID = "helm-value-files"
	HelmValues              ID = "helm-values"
	HelmValuesObject        ID = "helm-values-object"
	HelmParameters          ID = "helm-parameters"
	HelmFileParameters      ID = "helm-file-parameters"
	HelmIgnoreMissingValues ID = "helm-ignore-missing-value-files"
	HelmSkipSchema          ID = "helm-skip-schema-validation"
	HelmKubeVersion         ID = "helm-kube-version"
	HelmAPIVersions         ID = "helm-api-versions"
	HelmSkipCRDs            ID = "helm-skip-crds"
	CreateNamespace         ID = "create-namespace"
	DestinationContext      ID = "destination-context"
	ReleaseLabels           ID = "release-labels"
	SourceSelection         ID = "source-selection"
	MultipleSources         ID = "multiple-sources"
	ValuePrecedence         ID = "value-precedence"
	RepositoryAlias         ID = "repository-alias"
)

var entries = []Entry{
	{
		ID: MetadataName, Input: "metadata.name",
		Output:     "Release `name` when `helm.releaseName` is absent",
		References: []ID{HelmReleaseName},
	},
	{
		ID: HelmReleaseName, Input: "spec.source.helm.releaseName",
		Output: "Release `name`", HelmOption: "releaseName",
		HelmValueKind: String, AllowEmpty: true,
	},
	{
		ID: DestinationNamespace, Input: "spec.destination.namespace",
		Output: "Release `namespace`", References: []ID{HelmNamespace},
	},
	{
		ID: RevisionHistoryLimit, Input: "Positive `spec.revisionHistoryLimit`",
		Output: "Release `historyMax`", Notes: "Approximate mapping.",
	},
	{
		ID: HelmNamespace, Input: "spec.source.helm.namespace",
		Output:     "No additional output",
		Notes:      "Accepted only when it exactly matches `spec.destination.namespace`.",
		HelmOption: "namespace", HelmValueKind: String, AllowEmpty: true,
		References: []ID{DestinationNamespace},
	},
	{
		ID: HelmVersion, Input: "spec.source.helm.version",
		Output: "None", Notes: "Accepted and intentionally ignored.",
		HelmOption: "version", HelmValueKind: Ignored,
	},
	{
		ID: HelmSkipTests, Input: "spec.source.helm.skipTests",
		Output: "None", Notes: "Accepted and intentionally ignored.",
		HelmOption: "skipTests", HelmValueKind: Ignored,
	},
	{
		ID: RepositoryURL, Input: "spec.source.repoURL",
		Output:     "Repository `url`",
		Notes:      "Scheme-less OCI repositories also set `oci: true`.",
		References: []ID{RepositoryAlias, GitRepositoryURL},
	},
	{
		ID: HelmPassCredentials, Input: "Packaged chart `spec.source.helm.passCredentials`",
		Output:     "Repository `passCredentials: true` when true",
		HelmOption: "passCredentials", HelmValueKind: Boolean,
	},
	{
		ID: Chart, Input: "spec.source.chart",
		Output: "Release chart as `<alias>/<chart>`", References: []ID{RepositoryAlias},
	},
	{
		ID:    PackagedTargetRevision,
		Input: "Packaged chart `spec.source.targetRevision`", Output: "Release `version`",
	},
	{
		ID: GitRepositoryURL, Input: "Git `spec.source.repoURL`",
		Output:     "Config source identity",
		Notes:      "Accepts HTTP(S), `git@host:path`, or `ssh://user@host/path`.",
		References: []ID{GitTargetRevision},
	},
	{
		ID: GitPath, Input: "Git `spec.source.path`",
		Output:     "Chart or explicit Kustomization path below configured `localRoot`",
		References: []ID{GitRepositoryURL},
	},
	{
		ID: GitTargetRevision, Input: "Git `spec.source.targetRevision`",
		Output: "Config source identity and provenance", Notes: "Defaults to `HEAD`.",
		References: []ID{GitRepositoryURL},
	},
	{
		ID: KustomizeOptionsEntry, Input: "spec.source.kustomize",
		Output:     "Kustomization release `values` and `transformers`",
		Notes:      "Options are listed in the Kustomize option catalogs.",
		References: []ID{SourceSelection, GitPath},
	},
	{
		ID: HelmValueFiles, Input: "spec.source.helm.valueFiles",
		Output: "Release `values` paths", HelmOption: "valueFiles",
		HelmValueKind: StringSequence, AllowEmpty: true, References: []ID{ValuePrecedence},
	},
	{
		ID: HelmValues, Input: "spec.source.helm.values",
		Output: "Parsed inline release `values` entry", HelmOption: "values",
		HelmValueKind: InlineValues, AllowEmpty: true, References: []ID{ValuePrecedence},
	},
	{
		ID: HelmValuesObject, Input: "spec.source.helm.valuesObject",
		Output: "Inline release `values` entry", HelmOption: "valuesObject",
		HelmValueKind: RawValues, References: []ID{ValuePrecedence},
	},
	{
		ID: HelmParameters, Input: "spec.source.helm.parameters",
		Output: "Release `set` or `setString` entries", HelmOption: "parameters",
		HelmValueKind: Parameters, AllowEmpty: true,
		References: []ID{ValuePrecedence, HelmFileParameters},
	},
	{
		ID: HelmFileParameters, Input: "spec.source.helm.fileParameters",
		Output: "Release `set` entries using `file`", HelmOption: "fileParameters",
		HelmValueKind: FileParameters, AllowEmpty: true, References: []ID{HelmParameters},
	},
	{
		ID: HelmIgnoreMissingValues, Input: "spec.source.helm.ignoreMissingValueFiles",
		Output:     "Release `missingFileHandler: Warn` when true",
		HelmOption: "ignoreMissingValueFiles", HelmValueKind: Boolean, AllowEmpty: true,
	},
	{
		ID: HelmSkipSchema, Input: "spec.source.helm.skipSchemaValidation",
		Output: "Release `skipSchemaValidation`", HelmOption: "skipSchemaValidation",
		HelmValueKind: Boolean, AllowEmpty: true,
	},
	{
		ID: HelmKubeVersion, Input: "spec.source.helm.kubeVersion",
		Output: "Release `kubeVersion`", HelmOption: "kubeVersion",
		HelmValueKind: String, AllowEmpty: true,
	},
	{
		ID: HelmAPIVersions, Input: "spec.source.helm.apiVersions",
		Output: "Release `apiVersions`", HelmOption: "apiVersions",
		HelmValueKind: StringSequence, AllowEmpty: true,
	},
	{
		ID: HelmSkipCRDs, Input: "spec.source.helm.skipCrds",
		Output: "Shared `helmDefaults.skipCRDs`", HelmOption: "skipCrds",
		HelmValueKind: Boolean, AllowEmpty: true,
		Notes: "All Helm releases must resolve to the same value.",
	},
	{
		ID: CreateNamespace, Input: "Exact `CreateNamespace=true` sync option",
		Output: "Release `createNamespace: true`", Notes: "Approximate mapping.",
	},
	{
		ID:     DestinationContext,
		Input:  "spec.destination.name or spec.destination.server",
		Output: "Release `kubeContext` through Config `destinations`",
	},
	{
		ID: ReleaseLabels, Input: "Config `releaseLabels` query result",
		Output: "Release `labels` entry",
	},
	{
		ID: SourceSelection, Input: "Source type selection",
		Output: "Packaged Helm chart, Git Helm chart, or explicit Git Kustomization",
		Notes:  "`chart`, `path`, repository URL type, and explicit `kustomize: {}` select the type.",
	},
	{
		ID: MultipleSources, Input: "spec.source or spec.sources",
		Output:     "Exactly one manifest source plus optional values-only ref sources",
		Notes:      "The fields are mutually exclusive. Mapping paths apply to the selected manifest source.",
		References: []ID{SourceSelection},
	},
	{
		ID: ValuePrecedence, Input: "Helm values inputs",
		Output:     "Release `values`, `set`, and `setString` in Argo CD precedence order",
		Notes:      "From lowest to highest: `valueFiles`, `values`, `valuesObject`, then `parameters`.",
		References: []ID{HelmValueFiles, HelmValues, HelmValuesObject, HelmParameters},
	},
	{
		ID: RepositoryAlias, Input: "Repository URL",
		Output:     "Stable repository alias used by release chart `<alias>/<chart>`",
		Notes:      "Aliases are derived from the repository URL and disambiguated when necessary.",
		References: []ID{RepositoryURL, Chart},
	},
}

var kustomizeOptions = []KustomizeOption{
	{
		Name: "namePrefix", ValueKind: KustomizeString,
		Output: "Kustomization release `values.namePrefix`",
	},
	{
		Name: "nameSuffix", ValueKind: KustomizeString,
		Output: "Kustomization release `values.nameSuffix`",
	},
	{
		Name: "namespace", ValueKind: KustomizeString,
		Output: "Kustomization release `values.namespace`",
	},
	{
		Name: "images", ValueKind: KustomizeImages,
		Output: "Kustomization release `values.images`",
	},
	{
		Name: "replicas", ValueKind: KustomizeReplicas,
		Output: "Inline built-in `ReplicaCountTransformer`",
	},
	{
		Name: "commonLabels", ValueKind: KustomizeStringMap,
		Output: "Inline built-in `LabelTransformer`",
	},
	{
		Name: "labelWithoutSelector", ValueKind: KustomizeBoolean,
		Output: "`LabelTransformer.fieldSpecs` selection",
	},
	{
		Name: "labelIncludeTemplates", ValueKind: KustomizeBoolean,
		Output: "`LabelTransformer.fieldSpecs` selection",
	},
	{
		Name: "commonAnnotations", ValueKind: KustomizeStringMap,
		Output: "Inline built-in `AnnotationsTransformer`",
	},
	{
		Name: "commonAnnotationsEnvsubst", ValueKind: KustomizeBoolean,
		Output: "Conversion-time `commonAnnotations` value expansion",
	},
	{
		Name: "forceCommonLabels", ValueKind: KustomizeBoolean,
		Output: "None; accepted for validation only",
	},
	{
		Name: "forceCommonAnnotations", ValueKind: KustomizeBoolean,
		Output: "None; accepted for validation only",
	},
	{
		Name: "patches", ValueKind: KustomizeUnsupported,
		Reason: "Helmfile applies patches after namePrefix, nameSuffix, and images, " +
			"reversing Argo CD's order",
	},
	{
		Name: "components", ValueKind: KustomizeUnsupported,
		Reason: "Kustomize components have no Helmfile equivalent",
	},
	{
		Name: "ignoreMissingComponents", ValueKind: KustomizeUnsupported,
		Reason: "the option only affects components, which have no Helmfile equivalent",
	},
	{
		Name: "version", ValueKind: KustomizeUnsupported,
		Reason: "Helmfile selects the Kustomize binary globally, not per Application",
	},
	{
		Name: "kubeVersion", ValueKind: KustomizeUnsupported,
		Reason: "Helmfile does not pass a Kubernetes version to Kustomize builds",
	},
	{
		Name: "apiVersions", ValueKind: KustomizeUnsupported,
		Reason: "Helmfile does not pass API versions to Kustomize builds",
	},
}

var buildEnvironmentVariables = []BuildEnvironmentVariable{
	{
		Name: "ARGOCD_APP_NAME", Kind: BuildEnvironmentStatic,
		Source: "`metadata.name`",
	},
	{
		Name: "ARGOCD_APP_NAMESPACE", Kind: BuildEnvironmentStatic,
		Source: "`metadata.namespace`",
	},
	{
		Name: "ARGOCD_APP_PROJECT_NAME", Kind: BuildEnvironmentStatic,
		Source: "`spec.project`, or `default` when it is omitted",
	},
	{
		Name: "ARGOCD_APP_SOURCE_PATH", Kind: BuildEnvironmentStatic,
		Source: "Manifest source `path`",
	},
	{
		Name: "ARGOCD_APP_SOURCE_REPO_URL", Kind: BuildEnvironmentStatic,
		Source: "Manifest source `repoURL`",
	},
	{
		Name: "ARGOCD_APP_SOURCE_TARGET_REVISION", Kind: BuildEnvironmentStatic,
		Source: "Manifest source `targetRevision`",
	},
	{
		Name: "ARGOCD_APP_REVISION", Kind: BuildEnvironmentDynamic,
		Reason: "the revision is resolved when Argo CD syncs, not when the Application is written",
	},
	{
		Name: "ARGOCD_APP_REVISION_SHORT", Kind: BuildEnvironmentDynamic,
		Reason: "the revision is resolved when Argo CD syncs, not when the Application is written",
	},
	{
		Name: "ARGOCD_APP_REVISION_SHORT_8", Kind: BuildEnvironmentDynamic,
		Reason: "the revision is resolved when Argo CD syncs, not when the Application is written",
	},
	{
		Name: "KUBE_VERSION", Kind: BuildEnvironmentDynamic,
		Reason: "the value comes from the destination cluster, which the converter does not query",
	},
	{
		Name: "KUBE_API_VERSIONS", Kind: BuildEnvironmentDynamic,
		Reason: "the value comes from the destination cluster, which the converter does not query",
	},
}

func Entries() []Entry {
	return entries
}

func BuildEnvironmentVariables() []BuildEnvironmentVariable {
	return buildEnvironmentVariables
}

func KustomizeOptions() []KustomizeOption {
	return kustomizeOptions
}

var entriesByHelmOption = func() map[string]Entry {
	result := make(map[string]Entry)
	for _, entry := range entries {
		if entry.HelmOption != "" {
			result[entry.HelmOption] = entry
		}
	}
	return result
}()

func LookupHelmOption(name string) (Entry, bool) {
	entry, ok := entriesByHelmOption[name]
	return entry, ok
}

var kustomizeOptionsByName = catalog.IndexBy(
	kustomizeOptions,
	func(option KustomizeOption) string { return option.Name },
)

func LookupKustomizeOption(name string) (KustomizeOption, bool) {
	option, ok := kustomizeOptionsByName[name]
	return option, ok
}

var buildEnvironmentVariablesByName = catalog.IndexBy(
	buildEnvironmentVariables,
	func(variable BuildEnvironmentVariable) string { return variable.Name },
)

func LookupBuildEnvironmentVariable(name string) (BuildEnvironmentVariable, bool) {
	variable, ok := buildEnvironmentVariablesByName[name]
	return variable, ok
}
