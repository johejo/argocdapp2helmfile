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
