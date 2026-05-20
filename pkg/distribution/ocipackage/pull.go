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
	"path/filepath"
	"strings"

	"github.com/opencontainers/go-digest"

	"github.com/labring/sealos/pkg/distribution/packageformat"
	fileutil "github.com/labring/sealos/pkg/utils/file"
)

type PullOptions struct {
	Image     string
	OutputDir string
	Mounter   packageformat.ImageMounter
	Overwrite bool
}

type PullResult struct {
	Image     string
	OutputDir string
	Package   *packageformat.ComponentPackage
}

type Cache struct {
	Root    string
	Mounter packageformat.ImageMounter
}

func Pull(opts PullOptions) (*PullResult, error) {
	image := strings.TrimSpace(opts.Image)
	if image == "" {
		return nil, fmt.Errorf("image cannot be empty")
	}
	outputDir := strings.TrimSpace(opts.OutputDir)
	if outputDir == "" {
		return nil, fmt.Errorf("output directory cannot be empty")
	}
	if opts.Mounter == nil {
		return nil, fmt.Errorf("image mounter cannot be nil")
	}

	if _, err := os.Stat(outputDir); err == nil && !opts.Overwrite {
		return nil, fmt.Errorf("output directory %q already exists", outputDir)
	} else if err != nil && !os.IsNotExist(err) {
		return nil, fmt.Errorf("stat output directory %q: %w", outputDir, err)
	}

	parent := filepath.Dir(outputDir)
	if err := os.MkdirAll(parent, 0o755); err != nil {
		return nil, fmt.Errorf("create output parent %q: %w", parent, err)
	}
	tmpDir, err := os.MkdirTemp(parent, "."+filepath.Base(outputDir)+".tmp-")
	if err != nil {
		return nil, fmt.Errorf("create temporary output directory under %q: %w", parent, err)
	}
	cleanupTmp := true
	defer func() {
		if cleanupTmp {
			_ = os.RemoveAll(tmpDir)
		}
	}()

	info, err := opts.Mounter.Mount(image)
	if err != nil {
		return nil, fmt.Errorf("mount component package image %q: %w", image, err)
	}
	cleanupName := info.Name
	if cleanupName == "" {
		cleanupName = image
	}

	copyErr := copyMountedPackage(info.MountPoint, tmpDir)
	unmountErr := opts.Mounter.Unmount(cleanupName)
	if copyErr != nil {
		if unmountErr != nil {
			return nil, fmt.Errorf("%v (cleanup failed: %v)", copyErr, unmountErr)
		}
		return nil, copyErr
	}
	if unmountErr != nil {
		return nil, fmt.Errorf("cleanup component package mount %q: %w", cleanupName, unmountErr)
	}

	pkg, err := packageformat.LoadDir(tmpDir)
	if err != nil {
		return nil, err
	}
	if opts.Overwrite {
		if err := os.RemoveAll(outputDir); err != nil {
			return nil, fmt.Errorf("remove existing output directory %q: %w", outputDir, err)
		}
	}
	if err := os.Rename(tmpDir, outputDir); err != nil {
		return nil, fmt.Errorf("move package image %q into %q: %w", image, outputDir, err)
	}
	cleanupTmp = false

	return &PullResult{
		Image:     image,
		OutputDir: outputDir,
		Package:   pkg,
	}, nil
}

func (c *Cache) Ensure(image string) (string, *packageformat.ComponentPackage, error) {
	if c == nil {
		return "", nil, fmt.Errorf("package cache cannot be nil")
	}
	if c.Mounter == nil {
		return "", nil, fmt.Errorf("image mounter cannot be nil")
	}
	outputDir, err := CacheDirForReference(c.Root, image)
	if err != nil {
		return "", nil, err
	}
	if pkg, err := packageformat.LoadDir(outputDir); err == nil {
		return outputDir, pkg, nil
	}
	if err := os.RemoveAll(outputDir); err != nil {
		return "", nil, fmt.Errorf("remove invalid package cache entry %q: %w", outputDir, err)
	}
	result, err := Pull(PullOptions{
		Image:     image,
		OutputDir: outputDir,
		Mounter:   c.Mounter,
		Overwrite: true,
	})
	if err != nil {
		return "", nil, err
	}
	return result.OutputDir, result.Package, nil
}

func (c *Cache) Load(image string) (*packageformat.ComponentPackage, error) {
	_, pkg, err := c.Ensure(image)
	return pkg, err
}

func CacheDirForReference(root, image string) (string, error) {
	root = strings.TrimSpace(root)
	if root == "" {
		return "", fmt.Errorf("cache root cannot be empty")
	}
	image = strings.TrimSpace(image)
	if image == "" {
		return "", fmt.Errorf("image cannot be empty")
	}
	if digestValue := digestFromReference(image); digestValue != "" {
		dgst, err := digest.Parse(digestValue)
		if err != nil {
			return "", fmt.Errorf("parse image digest from %q: %w", image, err)
		}
		return filepath.Join(root, dgst.Algorithm().String(), dgst.Encoded()), nil
	}
	return filepath.Join(root, "ref", sanitizeCacheKey(image)), nil
}

func copyMountedPackage(src, dst string) error {
	src = strings.TrimSpace(src)
	if src == "" {
		return fmt.Errorf("mounted package root cannot be empty")
	}
	if err := fileutil.CopyDirV3(src, dst); err != nil {
		return fmt.Errorf("copy mounted package from %q to %q: %w", src, dst, err)
	}
	return nil
}

func digestFromReference(image string) string {
	_, digestValue, ok := strings.Cut(image, "@")
	if !ok {
		return ""
	}
	return strings.TrimSpace(digestValue)
}

func sanitizeCacheKey(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return "empty"
	}
	var b strings.Builder
	for _, r := range value {
		switch {
		case r >= 'a' && r <= 'z',
			r >= 'A' && r <= 'Z',
			r >= '0' && r <= '9',
			r == '.', r == '-', r == '_':
			b.WriteRune(r)
		default:
			b.WriteByte('_')
		}
	}
	return b.String()
}
