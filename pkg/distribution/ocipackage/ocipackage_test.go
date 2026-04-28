package ocipackage

import (
	"os"
	"path/filepath"
	"testing"
	"time"
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
