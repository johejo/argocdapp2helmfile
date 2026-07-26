package applicationset

import (
	"bytes"
	_ "embed"
	"text/template"
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
	for _, generator := range generators {
		if generator.Reason != "" {
			data.UnsupportedGenerators = append(data.UnsupportedGenerators, generator)
			continue
		}
		data.Generators = append(data.Generators, generator)
	}

	var output bytes.Buffer
	if err := markdownTemplate.Execute(&output, data); err != nil {
		panic(err)
	}
	return output.Bytes()
}
