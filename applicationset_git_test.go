package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestConvertApplicationSetGitDirectory(t *testing.T) {
	testConvertApplicationSetFixture(t, "git-directory", true)
}

func TestConvertApplicationSetGitDirectoryExcludesRepositoryRoot(t *testing.T) {
	testConvertApplicationSetFixture(t, "git-directory-root", true)
}

func TestConvertApplicationSetGitFile(t *testing.T) {
	testConvertApplicationSetFixture(t, "git-file", true)
}

func TestConvertApplicationSetLegacyGitDirectory(t *testing.T) {
	testConvertApplicationSetFixture(t, "legacy-git-directory", true)
}

func TestConvertApplicationSetLegacyGitFile(t *testing.T) {
	testConvertApplicationSetFixture(t, "legacy-git-file", true)
}

func TestApplicationSetGitGeneratorErrors(t *testing.T) {
	root := filepath.Join("testdata", "applicationset", "git-errors", "repository")
	resolver := testSourceResolver(t, testSource{
		repoURL:        "https://github.com/example/config.git",
		targetRevision: "main",
		root:           root,
	})
	missingResolver := testSourceResolver(t, testSource{
		repoURL:        "https://github.com/example/config.git",
		targetRevision: "main",
		root:           filepath.Join(root, "missing"),
	})
	templateResolver := testSourceResolver(t, testSource{
		repoURL:        "https://github.com/example/config.git",
		targetRevision: "main",
		root:           `{{ requiredEnv "CONFIG_ROOT" }}`,
	})
	symlinkRoot := filepath.Join(t.TempDir(), "root-link")
	if err := os.Symlink(root, symlinkRoot); err != nil {
		t.Fatal(err)
	}
	symlinkResolver := testSourceResolver(t, testSource{
		repoURL:        "https://github.com/example/config.git",
		targetRevision: "main",
		root:           symlinkRoot,
	})
	base := readTestdata(t, "applicationset/git-errors/application.yaml")
	tests := map[string]struct {
		input    string
		resolver *sourceResolver
		want     string
	}{
		"config required": {
			input: base,
			want:  "spec.generators[0].git requires --config",
		},
		"both subtypes": {
			input: strings.Replace(
				base,
				"        files:\n",
				"        directories:\n          - path: .\n        files:\n",
				1,
			),
			resolver: resolver,
			want:     "must set exactly one non-empty directories or files",
		},
		"invalid glob": {
			input:    strings.Replace(base, "scalar.yaml", "'['", 1),
			resolver: resolver,
			want:     `spec.generators[0].git.files[0].path: invalid glob "["`,
		},
		"non-mapping root": {
			input:    base,
			resolver: resolver,
			want:     `spec.generators[0].git.files["scalar.yaml"]: root must be a mapping`,
		},
		"source mismatch": {
			input:    strings.Replace(base, "revision: main", "revision: other", 1),
			resolver: resolver,
			want:     `has no config source entry`,
		},
		"missing localRoot": {
			input:    base,
			resolver: missingResolver,
			want:     `config localRoot`,
		},
		"templated localRoot": {
			input:    base,
			resolver: templateResolver,
			want:     `config localRoot must not contain a template expression`,
		},
		"symlink localRoot": {
			input:    base,
			resolver: symlinkResolver,
			want:     `must not be a symlink`,
		},
		"invalid exclude type": {
			input: strings.Replace(
				base,
				"- path: scalar.yaml",
				"- path: scalar.yaml\n            exclude: yes",
				1,
			),
			resolver: resolver,
			want:     `files[0].exclude must be a boolean`,
		},
		"invalid values type": {
			input: strings.Replace(
				base,
				"  template:\n",
				"        values:\n          enabled: true\n  template:\n",
				1,
			),
			resolver: resolver,
			want:     `git.values.enabled must be a string`,
		},
	}
	for name, test := range tests {
		t.Run(name, func(t *testing.T) {
			_, err := convertWithResolver([]byte(test.input), test.resolver)
			assertErrorContains(t, err, test.want)
		})
	}
}

func TestApplicationSetGitFileSkipsSymlinks(t *testing.T) {
	root := t.TempDir()
	real := readTestdata(t, "applicationset/git-symlink/repository/real.yaml")
	if err := os.WriteFile(filepath.Join(root, "real.yaml"), []byte(real), 0o644); err != nil {
		t.Fatal(err)
	}
	outside := t.TempDir()
	outsideFile := filepath.Join(outside, "outside.yaml")
	outsideData := readTestdata(t, "applicationset/git-symlink/repository/outside.yaml")
	if err := os.WriteFile(outsideFile, []byte(outsideData), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(
		outsideFile,
		filepath.Join(root, "linked.yaml"),
	); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(root, "linked-directory")); err != nil {
		t.Fatal(err)
	}
	resolver := testSourceResolver(t, testSource{
		repoURL:        "https://github.com/example/config.git",
		targetRevision: "main",
		root:           root,
	})
	input := readTestdata(t, "applicationset/git-symlink/application.yaml")
	output, err := convertWithResolver([]byte(input), resolver)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Count(string(output), "\n  - name: ") != 2 ||
		!strings.Contains(string(output), "\n  - name: real\n") ||
		strings.Contains(string(output), "outside") {
		t.Fatalf("symlink candidates were not skipped:\n%s", output)
	}
}
