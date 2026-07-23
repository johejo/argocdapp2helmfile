package main

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/goccy/go-yaml"
)

const (
	sourceMapAPIVersion = "argocdapp2helmfile/v1alpha1"
	sourceMapKind       = "SourceMap"
)

var environmentName = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`)

type sourceMap struct {
	APIVersion string           `yaml:"apiVersion"`
	Kind       string           `yaml:"kind"`
	Sources    []sourceMapEntry `yaml:"sources"`
}

type sourceMapEntry struct {
	RepoURL        string `yaml:"repoURL"`
	TargetRevision string `yaml:"targetRevision"`
	Env            string `yaml:"env"`
	AllowDirty     bool   `yaml:"allowDirty,omitempty"`
}

type sourceKey struct {
	repoURL        string
	targetRevision string
}

type localSource struct {
	env  string
	root string
}

type sourceResolver struct {
	entries  map[sourceKey]sourceMapEntry
	resolved map[sourceKey]localSource
}

func parseSourceMap(input []byte) (*sourceResolver, error) {
	decoder := yaml.NewDecoder(bytes.NewReader(input), yaml.DisallowUnknownField())
	var config sourceMap
	if err := decoder.Decode(&config); err != nil {
		return nil, fmt.Errorf("decode source map: %w", err)
	}
	var extra any
	if err := decoder.Decode(&extra); !errors.Is(err, io.EOF) {
		if err == nil {
			return nil, errors.New("source map must contain exactly one YAML document")
		}
		return nil, fmt.Errorf("decode additional source map document: %w", err)
	}
	if config.APIVersion != sourceMapAPIVersion {
		return nil, fmt.Errorf("source map apiVersion must be %q", sourceMapAPIVersion)
	}
	if config.Kind != sourceMapKind {
		return nil, fmt.Errorf("source map kind must be %q", sourceMapKind)
	}

	resolver := &sourceResolver{
		entries:  make(map[sourceKey]sourceMapEntry, len(config.Sources)),
		resolved: make(map[sourceKey]localSource, len(config.Sources)),
	}
	usedEnvironments := make(map[string]int)
	for i, entry := range config.Sources {
		field := fmt.Sprintf("source map sources[%d]", i)
		if strings.TrimSpace(entry.RepoURL) == "" {
			return nil, fmt.Errorf("%s.repoURL is required", field)
		}
		if strings.TrimSpace(entry.TargetRevision) == "" {
			return nil, fmt.Errorf("%s.targetRevision is required", field)
		}
		if !environmentName.MatchString(entry.Env) {
			return nil, fmt.Errorf("%s.env must be a valid environment variable name", field)
		}
		key := sourceKey{repoURL: entry.RepoURL, targetRevision: entry.TargetRevision}
		if _, exists := resolver.entries[key]; exists {
			return nil, fmt.Errorf("%s duplicates repoURL %q at targetRevision %q", field, entry.RepoURL, entry.TargetRevision)
		}
		if previous, exists := usedEnvironments[entry.Env]; exists {
			return nil, fmt.Errorf("%s.env %q duplicates sources[%d]", field, entry.Env, previous)
		}
		resolver.entries[key] = entry
		usedEnvironments[entry.Env] = i
	}
	return resolver, nil
}

func (resolver *sourceResolver) resolve(source applicationSource, field string) (localSource, error) {
	if resolver == nil {
		return localSource{}, fmt.Errorf("%s requires --source-map", field)
	}
	key := sourceKey{repoURL: source.RepoURL, targetRevision: source.TargetRevision}
	if resolved, exists := resolver.resolved[key]; exists {
		return resolved, nil
	}
	entry, exists := resolver.entries[key]
	if !exists {
		return localSource{}, fmt.Errorf(
			"%s has no source map entry for repoURL %q at targetRevision %q",
			field, source.RepoURL, source.TargetRevision,
		)
	}
	root := os.Getenv(entry.Env)
	if root == "" {
		return localSource{}, fmt.Errorf("source map environment variable %s is required for %s", entry.Env, field)
	}
	if !filepath.IsAbs(root) {
		return localSource{}, fmt.Errorf("source map environment variable %s must contain an absolute path", entry.Env)
	}
	canonicalRoot, err := filepath.EvalSymlinks(root)
	if err != nil {
		return localSource{}, fmt.Errorf("resolve source map environment variable %s: %w", entry.Env, err)
	}
	info, err := os.Stat(canonicalRoot)
	if err != nil {
		return localSource{}, fmt.Errorf("inspect source map environment variable %s: %w", entry.Env, err)
	}
	if !info.IsDir() {
		return localSource{}, fmt.Errorf("source map environment variable %s must point to a directory", entry.Env)
	}
	if err := validateGitCheckout(canonicalRoot, entry.TargetRevision, entry.AllowDirty); err != nil {
		return localSource{}, fmt.Errorf("source map environment variable %s: %w", entry.Env, err)
	}
	resolved := localSource{env: entry.Env, root: canonicalRoot}
	resolver.resolved[key] = resolved
	return resolved, nil
}

func validateGitCheckout(root, targetRevision string, allowDirty bool) error {
	topLevel, err := runGit(root, "rev-parse", "--show-toplevel")
	if err != nil {
		return fmt.Errorf("not a Git worktree: %w", err)
	}
	canonicalTopLevel, err := filepath.EvalSymlinks(topLevel)
	if err != nil {
		return fmt.Errorf("resolve Git worktree root: %w", err)
	}
	if canonicalTopLevel != root {
		return fmt.Errorf("path must be the Git worktree root %q", canonicalTopLevel)
	}
	head, err := runGit(root, "rev-parse", "--verify", "HEAD^{commit}")
	if err != nil {
		return fmt.Errorf("resolve HEAD: %w", err)
	}
	target, err := runGit(root, "rev-parse", "--verify", "--end-of-options", targetRevision+"^{commit}")
	if err != nil && isRemoteBranchCandidate(targetRevision) {
		target, err = runGit(root, "rev-parse", "--verify", "--end-of-options", "refs/remotes/origin/"+targetRevision+"^{commit}")
	}
	if err != nil {
		return fmt.Errorf("resolve targetRevision %q: %w", targetRevision, err)
	}
	if head != target {
		return fmt.Errorf("HEAD %s does not match targetRevision %q at %s", head, targetRevision, target)
	}
	if !allowDirty {
		status, err := runGit(root, "status", "--porcelain=v1", "--untracked-files=no")
		if err != nil {
			return fmt.Errorf("inspect tracked changes: %w", err)
		}
		if status != "" {
			return errors.New("tracked files contain uncommitted changes; set allowDirty: true to permit them")
		}
	}
	return nil
}

func runGit(root string, args ...string) (string, error) {
	command := exec.Command("git", append([]string{"-C", root}, args...)...)
	command.Env = append(os.Environ(), "GIT_OPTIONAL_LOCKS=0")
	output, err := command.CombinedOutput()
	text := strings.TrimSpace(string(output))
	if err != nil {
		if text == "" {
			text = err.Error()
		}
		return "", errors.New(text)
	}
	return text, nil
}

func isRemoteBranchCandidate(revision string) bool {
	return revision != "" &&
		!strings.HasPrefix(revision, "-") &&
		!strings.Contains(revision, "..") &&
		!strings.ContainsAny(revision, "~^:{}[]*?\\")
}
