package bom

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/labring/sealos/pkg/distribution/packageformat"
)

func TestLoadFile(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	path := filepath.Join(root, "bom.yaml")
	data := []byte(`apiVersion: distribution.sealos.io/v1alpha1
kind: BOM
metadata:
  name: default-platform
spec:
  revision: rev-20240423
  channel: beta
  components:
    - name: calico
      kind: infra
      version: 3.28.0
      artifact:
        name: calico-artifact
        image: registry.example.io/sealos/calico:3.28.0
        digest: sha256:bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb
`)
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	doc, err := LoadFile(path)
	if err != nil {
		t.Fatalf("LoadFile() error = %v", err)
	}
	if got, want := doc.Metadata.Name, "default-platform"; got != want {
		t.Fatalf("metadata.name = %q, want %q", got, want)
	}
}

func TestLoadFileProductionExample(t *testing.T) {
	t.Parallel()

	path := filepath.Join("testdata", "default-platform-production-bom.yaml")
	doc, err := LoadFile(path)
	if err != nil {
		t.Fatalf("LoadFile() error = %v", err)
	}

	if got, want := doc.Metadata.Name, "default-platform-production"; got != want {
		t.Fatalf("metadata.name = %q, want %q", got, want)
	}
	if got, want := doc.Spec.Revision, "rev-20240424-prod"; got != want {
		t.Fatalf("spec.revision = %q, want %q", got, want)
	}
	if got, want := len(doc.Spec.Components), 1; got != want {
		t.Fatalf("len(spec.components) = %d, want %d", got, want)
	}

	root := filepath.Join("..", "packageformat", "testdata", "kubernetes-production-rootfs")
	resolved, err := doc.ResolveComponentPackages(packageformat.LoaderFunc(func(image string) (*packageformat.ComponentPackage, error) {
		if got, want := image, "registry.example.io/sealos/kubernetes-production-rootfs:v1.30.3@sha256:1111111111111111111111111111111111111111111111111111111111111111"; got != want {
			t.Fatalf("loader image = %q, want %q", got, want)
		}
		return packageformat.LoadDir(root)
	}))
	if err != nil {
		t.Fatalf("ResolveComponentPackages() error = %v", err)
	}
	if got, want := resolved["kubernetes"].Metadata.Name, "kubernetes-production-rootfs"; got != want {
		t.Fatalf("resolved package metadata.name = %q, want %q", got, want)
	}
}
