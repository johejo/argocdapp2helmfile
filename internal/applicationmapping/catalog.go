package applicationmapping

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

func Entries() []Entry {
	return entries
}

func LookupHelmOption(name string) (Entry, bool) {
	for _, entry := range entries {
		if entry.HelmOption == name {
			return entry, true
		}
	}
	return Entry{}, false
}
