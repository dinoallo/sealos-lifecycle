package packageformat

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadDir(t *testing.T) {
	t.Parallel()

	root := filepath.Join("testdata", "kubernetes-rootfs")
	pkg, err := LoadDir(root)
	if err != nil {
		t.Fatalf("LoadDir() error = %v", err)
	}

	if got, want := pkg.Metadata.Name, "kubernetes-rootfs"; got != want {
		t.Fatalf("metadata.name = %q, want %q", got, want)
	}
	if got, want := pkg.Spec.Component, "kubernetes"; got != want {
		t.Fatalf("spec.component = %q, want %q", got, want)
	}
	if got, want := pkg.Spec.Class, ClassRootfs; got != want {
		t.Fatalf("spec.class = %q, want %q", got, want)
	}
}

func TestLoadDirProductionKubernetesExample(t *testing.T) {
	t.Parallel()

	root := filepath.Join("testdata", "kubernetes-production-rootfs")
	pkg, err := LoadDir(root)
	if err != nil {
		t.Fatalf("LoadDir() error = %v", err)
	}

	if got, want := pkg.Metadata.Name, "kubernetes-production-rootfs"; got != want {
		t.Fatalf("metadata.name = %q, want %q", got, want)
	}
	if got, want := len(pkg.Spec.Inputs), 4; got != want {
		t.Fatalf("len(spec.inputs) = %d, want %d", got, want)
	}
	if got, want := len(pkg.Spec.Contents), 7; got != want {
		t.Fatalf("len(spec.contents) = %d, want %d", got, want)
	}
	if got, want := len(pkg.Spec.Hooks), 3; got != want {
		t.Fatalf("len(spec.hooks) = %d, want %d", got, want)
	}
}

func TestLoadDirMissingContent(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	manifest := filepath.Join(root, ManifestFileName)
	data := []byte(`apiVersion: distribution.sealos.io/v1alpha1
kind: ComponentPackage
metadata:
  name: broken-package
spec:
  component: broken
  version: v0.1.0
  class: application
  contents:
    - name: app-manifest
      type: manifest
      path: manifests/app.yaml
`)
	if err := os.WriteFile(manifest, data, 0o644); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	if _, err := LoadDir(root); err == nil {
		t.Fatal("LoadDir() error = nil, want error")
	}
}

func TestLoadFromImage(t *testing.T) {
	t.Parallel()

	root := filepath.Join("testdata", "kubernetes-rootfs")
	fake := &fakeMounter{
		mountInfo: MountedImage{
			Name:       "pkgfmt-test",
			MountPoint: root,
		},
	}

	pkg, err := LoadFromImage(fake, "registry.example.io/sealos/kubernetes-rootfs:v1.30.3")
	if err != nil {
		t.Fatalf("LoadFromImage() error = %v", err)
	}
	if pkg.Metadata.Name != "kubernetes-rootfs" {
		t.Fatalf("metadata.name = %q, want %q", pkg.Metadata.Name, "kubernetes-rootfs")
	}
	if len(fake.deleted) != 1 || fake.deleted[0] != "pkgfmt-test" {
		t.Fatalf("deleted = %v, want [pkgfmt-test]", fake.deleted)
	}
}

func TestLoadFromImageCleanupOnFailure(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	fake := &fakeMounter{
		mountInfo: MountedImage{
			Name:       "pkgfmt-broken",
			MountPoint: root,
		},
	}

	if _, err := LoadFromImage(fake, "registry.example.io/sealos/broken:v0.1.0"); err == nil {
		t.Fatal("LoadFromImage() error = nil, want error")
	}
	if len(fake.deleted) != 1 || fake.deleted[0] != "pkgfmt-broken" {
		t.Fatalf("deleted = %v, want [pkgfmt-broken]", fake.deleted)
	}
}

type fakeMounter struct {
	mountInfo  MountedImage
	mountErr   error
	unmountErr error
	deleted    []string
}

func (f *fakeMounter) Mount(string) (MountedImage, error) {
	return f.mountInfo, f.mountErr
}

func (f *fakeMounter) Unmount(name string) error {
	f.deleted = append(f.deleted, name)
	return f.unmountErr
}

var _ ImageMounter = (*fakeMounter)(nil)
