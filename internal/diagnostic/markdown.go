package diagnostic

import (
	_ "embed"
	"slices"
	"text/template"

	"github.com/johejo/argocdapp2helmfile/internal/catalog"
)

//go:embed markdown.gotmpl
var markdownTemplateSource string

var markdownTemplate = template.Must(
	template.New("markdown.gotmpl").Parse(markdownTemplateSource),
)

type markdownData struct {
	SettingRules    []Rule
	SyncOptionRules []Rule
}

func Markdown() []byte {
	data := markdownData{}
	for _, rule := range rules {
		switch {
		case isSyncOptionRule(rule):
			data.SyncOptionRules = append(data.SyncOptionRules, rule)
		case !isInternalSyncRule(rule.ID):
			data.SettingRules = append(data.SettingRules, rule)
		}
	}
	return catalog.Render(markdownTemplate, data)
}

func isSyncOptionRule(rule Rule) bool {
	if slices.ContainsFunc(syncOptions, func(option SyncOption) bool {
		return option.Key == rule.Setting
	}) {
		return true
	}
	return rule.ID == ClientSideApplyMigrationDisabled
}

func isInternalSyncRule(id RuleID) bool {
	switch id {
	case SyncOptionNotString, SyncOptionMalformed, SyncOptionUnknown, SyncOptionValueUnknown,
		SyncOptionConflict:
		return true
	default:
		return false
	}
}
