// Copyright 2026 sealos.
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package ocipackage

import (
	"fmt"
	"os"
	"path"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"golang.org/x/sys/unix"

	"github.com/labring/sealos/pkg/distribution/packageformat"
	fileutil "github.com/labring/sealos/pkg/utils/file"
)

type Metadata struct {
	PackageDir string
	Name       string
	Component  string
	Version    string
	Class      packageformat.PackageClass
	Paths      []string
}

func LoadMetadata(packageDir string) (*Metadata, error) {
	if packageDir == "" {
		return nil, fmt.Errorf("package directory cannot be empty")
	}

	absDir, err := filepath.Abs(packageDir)
	if err != nil {
		return nil, fmt.Errorf("resolve package directory %q: %w", packageDir, err)
	}

	pkg, err := packageformat.LoadDir(absDir)
	if err != nil {
		return nil, err
	}

	paths := []string{packageformat.ManifestFileName}
	for _, content := range pkg.Spec.Contents {
		cleaned, err := cleanRelative(content.Path)
		if err != nil {
			return nil, err
		}
		paths = append(paths, cleaned)
	}
	for _, hook := range pkg.Spec.Hooks {
		cleaned, err := cleanRelative(hook.Path)
		if err != nil {
			return nil, err
		}
		paths = append(paths, cleaned)
	}

	return &Metadata{
		PackageDir: absDir,
		Name:       pkg.Metadata.Name,
		Component:  pkg.Spec.Component,
		Version:    pkg.Spec.Version,
		Class:      pkg.Spec.Class,
		Paths:      compactPaths(paths),
	}, nil
}

func StageContext(packageDir, contextDir string, timestamp time.Time) (*Metadata, error) {
	meta, err := LoadMetadata(packageDir)
	if err != nil {
		return nil, err
	}

	if contextDir == "" {
		return nil, fmt.Errorf("context directory cannot be empty")
	}
	if err := os.MkdirAll(contextDir, 0o755); err != nil {
		return nil, fmt.Errorf("create context directory %q: %w", contextDir, err)
	}

	for _, relPath := range meta.Paths {
		srcPath := filepath.Join(meta.PackageDir, filepath.FromSlash(relPath))
		dstPath := filepath.Join(contextDir, filepath.FromSlash(relPath))

		if err := os.MkdirAll(filepath.Dir(dstPath), 0o755); err != nil {
			return nil, fmt.Errorf("create parent directory for %q: %w", dstPath, err)
		}

		info, err := os.Lstat(srcPath)
		if err != nil {
			return nil, fmt.Errorf("stat %q: %w", srcPath, err)
		}
		if info.IsDir() {
			if err := fileutil.CopyDirV3(srcPath, dstPath); err != nil {
				return nil, fmt.Errorf("copy directory %q: %w", srcPath, err)
			}
			continue
		}
		if err := fileutil.Copy(srcPath, dstPath); err != nil {
			return nil, fmt.Errorf("copy file %q: %w", srcPath, err)
		}
	}

	if err := normalizeTreeTimes(contextDir, timestamp); err != nil {
		return nil, err
	}

	return meta, nil
}

func normalizeTreeTimes(root string, timestamp time.Time) error {
	if root == "" {
		return fmt.Errorf("root cannot be empty")
	}

	ts := []unix.Timespec{
		unix.NsecToTimespec(timestamp.UnixNano()),
		unix.NsecToTimespec(timestamp.UnixNano()),
	}

	return filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if err := unix.UtimesNanoAt(unix.AT_FDCWD, path, ts, unix.AT_SYMLINK_NOFOLLOW); err != nil {
			return fmt.Errorf("set mtime on %q: %w", path, err)
		}
		return nil
	})
}

func cleanRelative(relPath string) (string, error) {
	cleaned := path.Clean(relPath)
	switch {
	case cleaned == ".":
		return "", fmt.Errorf("invalid relative path %q", relPath)
	case strings.HasPrefix(cleaned, "/"):
		return "", fmt.Errorf("path %q must be relative", relPath)
	case strings.HasPrefix(cleaned, "../"):
		return "", fmt.Errorf("path %q escapes package root", relPath)
	default:
		return cleaned, nil
	}
}

func compactPaths(paths []string) []string {
	cleaned := make([]string, 0, len(paths))
	seen := make(map[string]struct{}, len(paths))
	for _, relPath := range paths {
		if _, ok := seen[relPath]; ok {
			continue
		}
		seen[relPath] = struct{}{}
		cleaned = append(cleaned, relPath)
	}

	slices.SortFunc(cleaned, func(a, b string) int {
		aDepth := strings.Count(a, "/")
		bDepth := strings.Count(b, "/")
		switch {
		case aDepth < bDepth:
			return -1
		case aDepth > bDepth:
			return 1
		case a < b:
			return -1
		case a > b:
			return 1
		default:
			return 0
		}
	})

	kept := make([]string, 0, len(cleaned))
	for _, relPath := range cleaned {
		skip := false
		for _, parent := range kept {
			if relPath == parent || strings.HasPrefix(relPath, parent+"/") {
				skip = true
				break
			}
		}
		if !skip {
			kept = append(kept, relPath)
		}
	}
	return kept
}
