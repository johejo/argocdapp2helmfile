package conversionconfig

import (
	_ "embed"
	"strings"
	"text/template"

	"github.com/goccy/go-yaml"
	"github.com/johejo/argocdapp2helmfile/internal/catalog"
)

//go:embed markdown.gotmpl
var markdownTemplateSource string

var markdownTemplate = template.Must(
	template.New("markdown.gotmpl").Parse(markdownTemplateSource),
)

type markdownData struct {
	Fields  []Field
	Example string
}

func Markdown() []byte {
	example, err := yaml.MarshalWithOptions(exampleResource(), yaml.IndentSequence(true))
	if err != nil {
		panic(err)
	}
	return catalog.Render(markdownTemplate, markdownData{
		Fields:  Fields(),
		Example: strings.TrimSpace(string(example)),
	})
}

func exampleResource() Resource {
	return Resource{
		APIVersion: APIVersion,
		Kind:       Kind,
		Destinations: []Destination{
			{Name: "production", KubeContext: "prod-admin"},
			{Server: "https://kubernetes.default.svc", KubeContext: "in-cluster"},
		},
		Clusters: []Cluster{{
			Name: "production-cluster", Server: "https://production.example.com",
			KubeContext: "production-admin", Project: "platform",
			Labels:      StrictStringMap{"environment": "production"},
			Annotations: StrictStringMap{"example.com/owner": "platform"},
		}},
		Sources: []Source{
			{
				RepoURL:        "git@github.com:example/platform-charts.git",
				TargetRevision: "release-1", LocalRoot: "checkouts/platform-charts",
			},
			{
				RepoURL:        "https://github.com/example/values.git",
				TargetRevision: "main", LocalRoot: `{{ requiredEnv "VALUES_ROOT" }}`,
			},
		},
		ReleaseLabels: []ReleaseLabelRule{
			{Name: "argocd.skipTests", Query: ".spec.source.helm.skipTests // false"},
			{Name: "argocd.project", Query: ".spec.project"},
		},
	}
}
