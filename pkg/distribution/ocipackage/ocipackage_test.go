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
