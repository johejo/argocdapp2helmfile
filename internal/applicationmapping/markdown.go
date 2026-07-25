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

func Markdown() []byte {
	var output bytes.Buffer
	if err := markdownTemplate.Execute(&output, entries); err != nil {
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
