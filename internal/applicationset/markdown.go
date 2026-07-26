package applicationset

import (
	_ "embed"
	"text/template"

	"github.com/johejo/argocdapp2helmfile/internal/catalog"
)

//go:embed markdown.gotmpl
var markdownTemplateSource string

var markdownTemplate = template.Must(
	template.New("markdown.gotmpl").Parse(markdownTemplateSource),
)

type markdownData struct {
	Generators            []Generator
	UnsupportedGenerators []Generator
	GoTemplateOptions     []GoTemplateOption
}

func Markdown() []byte {
	data := markdownData{GoTemplateOptions: goTemplateOptions}
	data.UnsupportedGenerators, data.Generators = catalog.Partition(
		generators,
		func(generator Generator) bool { return generator.Reason != "" },
	)
	return catalog.Render(markdownTemplate, data)
}
