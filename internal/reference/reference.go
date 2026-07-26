// Package reference lists the generated reference documents. One table drives
// the --help flags, the generator, and the staleness test.
package reference

import (
	"github.com/johejo/argocdapp2helmfile/internal/applicationmapping"
	"github.com/johejo/argocdapp2helmfile/internal/applicationset"
	"github.com/johejo/argocdapp2helmfile/internal/diagnostic"
)

// Document is one generated document a --help flag prints instead of
// converting. Usage reads in help output, Name inside an error message.
type Document struct {
	Flag   string
	Usage  string
	Name   string
	Path   string
	Render func() []byte
}

var Documents = []Document{
	{
		Flag:   "help-diagnostics",
		Usage:  "print the diagnostics reference",
		Name:   "diagnostics",
		Path:   "docs/diagnostics.md",
		Render: diagnostic.Markdown,
	},
	{
		Flag:   "help-application-mapping",
		Usage:  "print the Application mapping reference",
		Name:   "application mapping",
		Path:   "docs/application-mapping.md",
		Render: applicationmapping.Markdown,
	},
	{
		Flag:   "help-applicationset",
		Usage:  "print the ApplicationSet reference",
		Name:   "ApplicationSet",
		Path:   "docs/applicationset.md",
		Render: applicationset.Markdown,
	},
}
