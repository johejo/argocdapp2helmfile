package applicationmapping

import (
	_ "embed"
	"text/template"

	"github.com/johejo/argocdapp2helmfile/internal/catalog"
)

//go:embed markdown.gotmpl
var markdownTemplateSource string

var markdownTemplate = template.Must(
	template.New("markdown.gotmpl").
		Funcs(template.FuncMap{"cell": markdownCell}).
		Parse(markdownTemplateSource),
)

type markdownData struct {
	Entries                     []Entry
	KustomizeOptions            []KustomizeOption
	UnsupportedKustomizeOptions []KustomizeOption
	StaticBuildEnvironment      []BuildEnvironmentVariable
	DynamicBuildEnvironment     []BuildEnvironmentVariable
}

func Markdown() []byte {
	data := markdownData{Entries: entries}
	data.UnsupportedKustomizeOptions, data.KustomizeOptions = catalog.Partition(
		kustomizeOptions,
		func(option KustomizeOption) bool { return option.ValueKind == KustomizeUnsupported },
	)
	data.DynamicBuildEnvironment, data.StaticBuildEnvironment = catalog.Partition(
		buildEnvironmentVariables,
		func(variable BuildEnvironmentVariable) bool {
			return variable.Kind == BuildEnvironmentDynamic
		},
	)
	return catalog.Render(markdownTemplate, data)
}

func markdownCell(value string) string {
	if value == "" {
		return "—"
	}
	return value
}
