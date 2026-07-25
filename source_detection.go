package main

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

var kustomizationFileNames = []string{
	"kustomization.yaml",
	"kustomization.yml",
	"Kustomization",
}

func validateGitChartSource(
	mapping mappedSource,
	source applicationSource,
	field string,
) error {
	root, ok := inspectableSourceRoot(mapping.localRoot)
	if !ok {
		return nil
	}
	sourcePath, ok := inspectablePathWithinRoot(
		root,
		filepath.Join(root, filepath.FromSlash(source.Path)),
	)
	if !ok {
		return nil
	}
	info, err := os.Stat(sourcePath)
	if err != nil || !info.IsDir() {
		return nil
	}

	hasChart, ok := inspectableRegularFile(root, filepath.Join(sourcePath, "Chart.yaml"))
	if !ok || hasChart {
		return nil
	}
	for _, name := range kustomizationFileNames {
		found, _ := inspectableRegularFile(root, filepath.Join(sourcePath, name))
		if found {
			return fmt.Errorf(
				"%s.path %q appears to be a Kustomization because %q exists but Chart.yaml does not; add %s.kustomize: {}",
				field,
				source.Path,
				name,
				field,
			)
		}
	}
	return nil
}

func inspectableSourceRoot(root string) (string, bool) {
	if validateLocalRootDirectory(root) != nil {
		return "", false
	}
	canonical, err := filepath.EvalSymlinks(root)
	if err != nil {
		return "", false
	}
	canonical, err = filepath.Abs(canonical)
	if err != nil {
		return "", false
	}
	return canonical, true
}

func inspectableRegularFile(root, candidate string) (bool, bool) {
	info, err := os.Lstat(candidate)
	if errors.Is(err, os.ErrNotExist) {
		return false, true
	}
	if err != nil {
		return false, false
	}
	canonical, ok := inspectablePathWithinRoot(root, candidate)
	if !ok {
		return false, false
	}
	if info.Mode()&os.ModeSymlink != 0 {
		info, err = os.Stat(canonical)
		if err != nil {
			return false, false
		}
	}
	return info.Mode().IsRegular(), true
}

func inspectablePathWithinRoot(root, candidate string) (string, bool) {
	canonical, err := filepath.EvalSymlinks(candidate)
	if err != nil {
		return "", false
	}
	canonical, err = filepath.Abs(canonical)
	if err != nil {
		return "", false
	}
	relative, err := filepath.Rel(root, canonical)
	if err != nil ||
		relative == ".." ||
		strings.HasPrefix(relative, ".."+string(filepath.Separator)) {
		return "", false
	}
	return canonical, true
}
