package applicationmapping

import (
	"bytes"
	_ "embed"
	"text/template"
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
	for _, option := range kustomizeOptions {
		if option.ValueKind == KustomizeUnsupported {
			data.UnsupportedKustomizeOptions = append(data.UnsupportedKustomizeOptions, option)
			continue
		}
		data.KustomizeOptions = append(data.KustomizeOptions, option)
	}
	for _, variable := range buildEnvironmentVariables {
		if variable.Kind == BuildEnvironmentDynamic {
			data.DynamicBuildEnvironment = append(data.DynamicBuildEnvironment, variable)
			continue
		}
		data.StaticBuildEnvironment = append(data.StaticBuildEnvironment, variable)
	}

	var output bytes.Buffer
	if err := markdownTemplate.Execute(&output, data); err != nil {
		panic(err)
	}
	return output.Bytes()
}

func markdownCell(value string) string {
	if value == "" {
		return "—"
	}
	return value
}
