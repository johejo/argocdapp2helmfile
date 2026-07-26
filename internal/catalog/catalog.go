// Package catalog holds the helpers shared by the reference catalogs.
package catalog

import (
	"bytes"
	"text/template"
)

// IndexBy returns items keyed by key. A later item wins a duplicate key.
func IndexBy[T any, K comparable](items []T, key func(T) K) map[K]T {
	result := make(map[K]T, len(items))
	for _, item := range items {
		result[key(item)] = item
	}
	return result
}

// Partition preserves the original order within each group.
func Partition[T any](items []T, pred func(T) bool) (matched, rest []T) {
	for _, item := range items {
		if pred(item) {
			matched = append(matched, item)
			continue
		}
		rest = append(rest, item)
	}
	return matched, rest
}

// Render panics because a template that disagrees with its data is a
// programming error rather than bad input.
func Render(markdown *template.Template, data any) []byte {
	var output bytes.Buffer
	if err := markdown.Execute(&output, data); err != nil {
		panic(err)
	}
	return output.Bytes()
}
