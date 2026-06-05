// Copyright © 2026 sealos.
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
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/labring/sealos/pkg/distribution/packageformat"
)

func TestLoadMetadata(t *testing.T) {
	t.Parallel()

	meta, err := LoadMetadata(filepath.Join("..", "packageformat", "testdata", "kubernetes-rootfs"))
	if err != nil {
		t.Fatalf("LoadMetadata() error = %v", err)
	}

	if got, want := meta.Name, "kubernetes-rootfs"; got != want {
		t.Fatalf("meta.Name = %q, want %q", got, want)
	}
	if got, want := meta.Component, "kubernetes"; got != want {
		t.Fatalf("meta.Component = %q, want %q", got, want)
	}
	if got, want := meta.Version, "v1.30.3"; got != want {
		t.Fatalf("meta.Version = %q, want %q", got, want)
	}
	if len(meta.Paths) == 0 {
		t.Fatal("meta.Paths is empty, want manifest-referenced entries")
	}
}

func TestStageContext(t *testing.T) {
	t.Parallel()

	out := t.TempDir()
	ts := time.Unix(1234, 0)

	meta, err := StageContext(filepath.Join("..", "packageformat", "testdata", "kubernetes-rootfs"), out, ts)
	if err != nil {
		t.Fatalf("StageContext() error = %v", err)
	}

	if got, want := meta.Component, "kubernetes"; got != want {
		t.Fatalf("meta.Component = %q, want %q", got, want)
	}

	for _, rel := range []string{
		"package.yaml",
		"rootfs/README",
		"manifests/bootstrap.yaml",
		"hooks/bootstrap.sh",
	} {
		info, err := os.Lstat(filepath.Join(out, filepath.FromSlash(rel)))
		if err != nil {
			t.Fatalf("Lstat(%q) error = %v", rel, err)
		}
		if got, want := info.ModTime().Unix(), ts.Unix(); got != want {
			t.Fatalf("modtime(%q) = %d, want %d", rel, got, want)
		}
	}
}

func TestPull(t *testing.T) {
	t.Parallel()

	fixtureRoot := filepath.Join("..", "packageformat", "testdata", "kubernetes-rootfs")
	mounter := &fakeImageMounter{
		mounts: map[string]packageformat.MountedImage{
			"registry.example.io/sealos/kubernetes-rootfs:v1.30.3": {
				Name:       "mounted-package",
				MountPoint: fixtureRoot,
			},
		},
	}
	outputDir := filepath.Join(t.TempDir(), "pulled")

	result, err := Pull(PullOptions{
		Image:     "registry.example.io/sealos/kubernetes-rootfs:v1.30.3",
		OutputDir: outputDir,
		Mounter:   mounter,
	})
	if err != nil {
		t.Fatalf("Pull() error = %v", err)
	}

	if got, want := result.OutputDir, outputDir; got != want {
		t.Fatalf("result.OutputDir = %q, want %q", got, want)
	}
	if got, want := result.Package.Metadata.Name, "kubernetes-rootfs"; got != want {
		t.Fatalf("result.Package.Metadata.Name = %q, want %q", got, want)
	}
	if _, err := os.Stat(filepath.Join(outputDir, "rootfs", "README")); err != nil {
		t.Fatalf("pulled package missing rootfs/README: %v", err)
	}
	if got, want := mounter.unmounted[0], "mounted-package"; got != want {
		t.Fatalf("unmounted = %q, want %q", got, want)
	}
}

func TestCacheEnsureUsesDigestPathAndReusesEntry(t *testing.T) {
	t.Parallel()

	fixtureRoot := filepath.Join("..", "packageformat", "testdata", "kubernetes-rootfs")
	ref := "registry.example.io/sealos/kubernetes-rootfs:v1.30.3@sha256:1111111111111111111111111111111111111111111111111111111111111111"
	mounter := &fakeImageMounter{
		mounts: map[string]packageformat.MountedImage{
			ref: {
				Name:       "mounted-package",
				MountPoint: fixtureRoot,
			},
		},
	}
	cache := &Cache{
		Root:    t.TempDir(),
		Mounter: mounter,
	}

	root, pkg, err := cache.Ensure(ref)
	if err != nil {
		t.Fatalf("Ensure() error = %v", err)
	}
	wantRoot := filepath.Join(cache.Root, "sha256", "1111111111111111111111111111111111111111111111111111111111111111")
	if got := root; got != wantRoot {
		t.Fatalf("cache root = %q, want %q", got, wantRoot)
	}
	if got, want := pkg.Spec.Component, "kubernetes"; got != want {
		t.Fatalf("pkg.Spec.Component = %q, want %q", got, want)
	}

	root, pkg, err = cache.Ensure(ref)
	if err != nil {
		t.Fatalf("second Ensure() error = %v", err)
	}
	if root != wantRoot || pkg.Spec.Component != "kubernetes" {
		t.Fatalf("second Ensure() root=%q component=%q, want cached kubernetes package", root, pkg.Spec.Component)
	}
	if got, want := len(mounter.mounted), 1; got != want {
		t.Fatalf("mount count = %d, want %d", got, want)
	}
}

func TestCacheListAndGC(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	validPath := filepath.Join(root, "sha256", "1111111111111111111111111111111111111111111111111111111111111111")
	if err := os.MkdirAll(filepath.Dir(validPath), 0o755); err != nil {
		t.Fatalf("MkdirAll(valid parent) error = %v", err)
	}
	if err := copyTestPackage(filepath.Join("..", "packageformat", "testdata", "kubernetes-rootfs"), validPath); err != nil {
		t.Fatalf("copyTestPackage(valid) error = %v", err)
	}
	invalidPath := filepath.Join(root, "ref", "stale")
	if err := os.MkdirAll(invalidPath, 0o755); err != nil {
		t.Fatalf("MkdirAll(invalid) error = %v", err)
	}
	if err := os.WriteFile(filepath.Join(invalidPath, "README"), []byte("invalid"), 0o644); err != nil {
		t.Fatalf("WriteFile(invalid) error = %v", err)
	}
	oldTime := time.Now().Add(-48 * time.Hour)
	for _, path := range []string{validPath, invalidPath} {
		if err := os.Chtimes(path, oldTime, oldTime); err != nil {
			t.Fatalf("Chtimes(%q) error = %v", path, err)
		}
	}

	cache := &Cache{Root: root}
	entries, err := cache.List()
	if err != nil {
		t.Fatalf("List() error = %v", err)
	}
	if got, want := len(entries), 2; got != want {
		t.Fatalf("len(entries) = %d, want %d", got, want)
	}
	validEntry := findCacheEntry(entries, "sha256", "1111111111111111111111111111111111111111111111111111111111111111")
	if validEntry == nil {
		t.Fatalf("valid cache entry missing: %#v", entries)
	}
	if !validEntry.Valid {
		t.Fatalf("validEntry.Valid = false, error=%q", validEntry.Error)
	}
	if got, want := validEntry.Component, "kubernetes"; got != want {
		t.Fatalf("validEntry.Component = %q, want %q", got, want)
	}
	invalidEntry := findCacheEntry(entries, "ref", "stale")
	if invalidEntry == nil {
		t.Fatalf("invalid cache entry missing: %#v", entries)
	}
	if invalidEntry.Valid || !strings.Contains(invalidEntry.Error, "package.yaml") {
		t.Fatalf("invalidEntry = %#v, want invalid package.yaml entry", invalidEntry)
	}

	result, err := cache.GC(CacheGCOptions{
		MaxAge: 24 * time.Hour,
		DryRun: true,
		Now:    time.Now(),
	})
	if err != nil {
		t.Fatalf("GC(dry-run) error = %v", err)
	}
	if got, want := len(result.Removed), 1; got != want {
		t.Fatalf("len(dry-run removed) = %d, want %d", got, want)
	}
	if _, err := os.Stat(invalidPath); err != nil {
		t.Fatalf("dry-run removed invalid path: %v", err)
	}

	result, err = cache.GC(CacheGCOptions{
		MaxAge: 24 * time.Hour,
		Now:    time.Now(),
	})
	if err != nil {
		t.Fatalf("GC(invalid) error = %v", err)
	}
	if got, want := len(result.Removed), 1; got != want {
		t.Fatalf("len(removed invalid) = %d, want %d", got, want)
	}
	if _, err := os.Stat(invalidPath); !os.IsNotExist(err) {
		t.Fatalf("invalid path still exists or stat failed: %v", err)
	}
	if _, err := os.Stat(validPath); err != nil {
		t.Fatalf("valid path removed without include-valid: %v", err)
	}

	result, err = cache.GC(CacheGCOptions{
		MaxAge:       24 * time.Hour,
		IncludeValid: true,
		Now:          time.Now(),
	})
	if err != nil {
		t.Fatalf("GC(include-valid) error = %v", err)
	}
	if got, want := len(result.Removed), 1; got != want {
		t.Fatalf("len(removed valid) = %d, want %d", got, want)
	}
	if _, err := os.Stat(validPath); !os.IsNotExist(err) {
		t.Fatalf("valid path still exists or stat failed: %v", err)
	}
}

func copyTestPackage(src, dst string) error {
	return filepath.WalkDir(src, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(src, path)
		if err != nil {
			return err
		}
		target := filepath.Join(dst, rel)
		if d.IsDir() {
			return os.MkdirAll(target, 0o755)
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		return os.WriteFile(target, data, 0o644)
	})
}

func findCacheEntry(entries []CacheEntry, kind, key string) *CacheEntry {
	for i := range entries {
		if entries[i].Kind == kind && entries[i].Key == key {
			return &entries[i]
		}
	}
	return nil
}

type fakeImageMounter struct {
	mounts    map[string]packageformat.MountedImage
	mounted   []string
	unmounted []string
}

func (m *fakeImageMounter) Mount(image string) (packageformat.MountedImage, error) {
	info, ok := m.mounts[image]
	if !ok {
		return packageformat.MountedImage{}, os.ErrNotExist
	}
	m.mounted = append(m.mounted, image)
	return info, nil
}

func (m *fakeImageMounter) Unmount(name string) error {
	m.unmounted = append(m.unmounted, name)
	return nil
}
