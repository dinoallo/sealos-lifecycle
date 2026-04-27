package state

import (
	"os"
	"testing"

	"github.com/labring/sealos/pkg/constants"
)

func TestPersistSuccessfulApply(t *testing.T) {
	previousRoot := constants.DefaultRuntimeRootDir
	constants.DefaultRuntimeRootDir = t.TempDir()
	t.Cleanup(func() {
		constants.DefaultRuntimeRootDir = previousRoot
	})

	ref := BOMReference{
		Name:     "default-platform",
		Revision: "rev-20240423",
		Channel:  "beta",
		Digest:   "sha256:dddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddd",
	}

	doc, err := PersistSuccessfulApply(
		"cluster-a",
		ref,
		"sha256:eeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeee",
		"local-rev-1",
	)
	if err != nil {
		t.Fatalf("PersistSuccessfulApply() error = %v", err)
	}
	if got, want := doc.Metadata.Name, "cluster-a-current"; got != want {
		t.Fatalf("metadata.name = %q, want %q", got, want)
	}
	if got, want := doc.Status.State, StateClean; got != want {
		t.Fatalf("status.state = %q, want %q", got, want)
	}
	if doc.Status.LastSuccessfulRevision == nil {
		t.Fatal("status.lastSuccessfulRevision = nil, want value")
	}
	if got, want := doc.Status.LastSuccessfulRevision.DesiredStateDigest, doc.Spec.DesiredStateDigest; got != want {
		t.Fatalf("lastSuccessfulRevision.desiredStateDigest = %q, want %q", got, want)
	}

	path := AppliedRevisionPath("cluster-a")
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("Stat(%q) error = %v", path, err)
	}

	loaded, err := LoadAppliedRevision("cluster-a")
	if err != nil {
		t.Fatalf("LoadAppliedRevision() error = %v", err)
	}
	if got, want := loaded.Spec.DesiredStateDigest, doc.Spec.DesiredStateDigest; got != want {
		t.Fatalf("spec.desiredStateDigest = %q, want %q", got, want)
	}
	if got, want := loaded.Spec.LocalPatchRevision, "local-rev-1"; got != want {
		t.Fatalf("spec.localPatchRevision = %q, want %q", got, want)
	}
}

func TestSaveAppliedRevisionRejectsInvalidDocument(t *testing.T) {
	if err := SaveAppliedRevision(&AppliedRevision{}); err == nil {
		t.Fatal("SaveAppliedRevision() error = nil, want error")
	}
}

func TestLoadAppliedRevisionMissingFile(t *testing.T) {
	previousRoot := constants.DefaultRuntimeRootDir
	constants.DefaultRuntimeRootDir = t.TempDir()
	t.Cleanup(func() {
		constants.DefaultRuntimeRootDir = previousRoot
	})

	if _, err := LoadAppliedRevision("cluster-a"); err == nil {
		t.Fatal("LoadAppliedRevision() error = nil, want error")
	}
}
