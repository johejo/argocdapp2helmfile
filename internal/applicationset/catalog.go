// Package applicationset catalogs the ApplicationSet features the converter
// accepts. The catalog drives both the parser in package main and the generated
// reference, so the two cannot disagree.
package applicationset

import "github.com/johejo/argocdapp2helmfile/internal/catalog"

// Generator describes one Argo CD generator, so that one lookup decides whether
// a generator is known, whether it converts, and how the parser treats its
// children.
type Generator struct {
	Name string
	// Combination generators nest child generators of their own.
	Combination bool
	// ValuesMap generators render their own values: mapping.
	ValuesMap bool
	// DeferredRender generators interpolate parent parameters per child instead
	// of once before parsing.
	DeferredRender bool
	// Fields and Expansion are Markdown for the reference, Reason is plain text
	// for rejection errors, and Reason excludes the other two.
	Fields    string
	Expansion string
	Reason    string
}

// GoTemplateOption is one accepted spec.goTemplateOptions value. text/template
// panics on an option it does not define, so the catalog, not template.Option,
// decides which values are accepted.
type GoTemplateOption struct {
	Name   string
	Output string
}

var generators = []Generator{
	{
		Name:      "list",
		Fields:    "`elements`, `elementsYaml`, `template`",
		Expansion: "One parameter set per element, `elements` before `elementsYaml`",
	},
	{
		Name: "git", ValuesMap: true,
		Fields: "`repoURL`, `revision`, exactly one non-empty `directories` or `files`, " +
			"`pathParamPrefix`, `values`, `template`, `requeueAfterSeconds`",
		Expansion: "One parameter set per matching directory, or per mapping in each matching " +
			"parameter file",
	},
	{
		Name: "clusters", ValuesMap: true,
		Fields:    "`selector`, `values`, `template`, `flatList`",
		Expansion: "One parameter set per Config `clusters` entry, in declaration order",
	},
	{
		Name: "matrix", Combination: true, DeferredRender: true,
		Fields: "`generators` with exactly two children, `template`",
		Expansion: "Every combination of the children's parameter sets, the second child " +
			"re-expanded for each parameter set of the first",
	},
	{
		Name: "merge", Combination: true,
		Fields: "`mergeKeys`, `generators` with two or more children, `template`",
		Expansion: "The first child's parameter sets, each overridden by later children whose " +
			"`mergeKeys` values match",
	},
	{
		Name:   "scmProvider",
		Reason: "listing repositories requires querying a live SCM provider",
	},
	{
		Name:   "pullRequest",
		Reason: "listing pull requests requires querying a live SCM provider",
	},
	{
		Name:   "clusterDecisionResource",
		Reason: "the generator reads a duck-typed resource from a live cluster",
	},
	{
		Name:   "plugin",
		Reason: "the generator calls a plugin service over HTTP",
	},
}

var goTemplateOptions = []GoTemplateOption{
	{
		Name:   "missingkey=default",
		Output: "A missing map key renders `<no value>`",
	},
	{
		Name:   "missingkey=invalid",
		Output: "Same as `missingkey=default`",
	},
	{
		Name:   "missingkey=zero",
		Output: "A missing map key renders the zero value of the map's element type",
	},
	{
		Name:   "missingkey=error",
		Output: "A missing map key fails the conversion",
	},
}

func Generators() []Generator {
	return generators
}

func GoTemplateOptions() []GoTemplateOption {
	return goTemplateOptions
}

var generatorsByName = catalog.IndexBy(generators, func(generator Generator) string {
	return generator.Name
})

func LookupGenerator(name string) (Generator, bool) {
	generator, ok := generatorsByName[name]
	return generator, ok
}

var goTemplateOptionsByName = catalog.IndexBy(
	goTemplateOptions,
	func(option GoTemplateOption) string { return option.Name },
)

func LookupGoTemplateOption(name string) (GoTemplateOption, bool) {
	option, ok := goTemplateOptionsByName[name]
	return option, ok
}
