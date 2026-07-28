package main

import (
	"strings"
	"testing"
)

func TestNormalizeName(t *testing.T) {
	tests := []struct {
		name  string
		value string
		want  string
	}{
		{
			name:  "replaces each invalid character",
			value: "Über  App",
			want:  "ber--app",
		},
		{
			name:  "truncates before trimming",
			value: "." + strings.Repeat("a", 253),
			want:  strings.Repeat("a", 252),
		},
		{
			name:  "trims leading and trailing separators",
			value: ".-app-.",
			want:  "app",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := normalizeName(test.value); got != test.want {
				t.Fatalf("normalizeName(%q) = %q, want %q", test.value, got, test.want)
			}
		})
	}
}
