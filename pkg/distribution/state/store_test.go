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

func TestPersistRenderedState(t *testing.T) {
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

	doc, err := PersistRenderedState(
		"cluster-a",
		ref,
		"sha256:eeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeee",
		"local-rev-1",
	)
	if err != nil {
		t.Fatalf("PersistRenderedState() error = %v", err)
	}
	if got, want := doc.Status.State, StateDirty; got != want {
		t.Fatalf("status.state = %q, want %q", got, want)
	}
	if doc.Status.LastAppliedTime != nil {
		t.Fatalf("status.lastAppliedTime = %v, want nil", doc.Status.LastAppliedTime)
	}
	if doc.Status.LastSuccessfulRevision != nil {
		t.Fatalf("status.lastSuccessfulRevision = %v, want nil", doc.Status.LastSuccessfulRevision)
	}
	if len(doc.Status.Conditions) != 1 {
		t.Fatalf("len(status.conditions) = %d, want 1", len(doc.Status.Conditions))
	}
	if got, want := doc.Status.Conditions[0].Reason, "DesiredStateRendered"; got != want {
		t.Fatalf("status.conditions[0].reason = %q, want %q", got, want)
	}
}

func TestPersistRenderedStateKeepsCleanStatusForNoopRender(t *testing.T) {
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
	desiredStateDigest := "sha256:eeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeee"

	if _, err := PersistSuccessfulApply("cluster-a", ref, desiredStateDigest, "local-rev-1"); err != nil {
		t.Fatalf("PersistSuccessfulApply() error = %v", err)
	}

	doc, err := PersistRenderedState("cluster-a", ref, desiredStateDigest, "local-rev-2")
	if err != nil {
		t.Fatalf("PersistRenderedState() error = %v", err)
	}
	if got, want := doc.Status.State, StateClean; got != want {
		t.Fatalf("status.state = %q, want %q", got, want)
	}
	if doc.Status.LastAppliedTime == nil {
		t.Fatal("status.lastAppliedTime = nil, want value")
	}
	if doc.Status.LastSuccessfulRevision == nil {
		t.Fatal("status.lastSuccessfulRevision = nil, want value")
	}
	if got, want := doc.Status.LastSuccessfulRevision.DesiredStateDigest, desiredStateDigest; got != want {
		t.Fatalf("lastSuccessfulRevision.desiredStateDigest = %q, want %q", got, want)
	}
}

func TestPersistRenderedStatePreservesLastSuccessfulRevision(t *testing.T) {
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
	lastSuccessfulDigest := "sha256:eeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeee"
	nextDesiredDigest := "sha256:ffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffff"

	if _, err := PersistSuccessfulApply("cluster-a", ref, lastSuccessfulDigest, "local-rev-1"); err != nil {
		t.Fatalf("PersistSuccessfulApply() error = %v", err)
	}

	doc, err := PersistRenderedState("cluster-a", ref, nextDesiredDigest, "local-rev-2")
	if err != nil {
		t.Fatalf("PersistRenderedState() error = %v", err)
	}
	if got, want := doc.Status.State, StateDirty; got != want {
		t.Fatalf("status.state = %q, want %q", got, want)
	}
	if doc.Status.LastAppliedTime == nil {
		t.Fatal("status.lastAppliedTime = nil, want value")
	}
	if doc.Status.LastSuccessfulRevision == nil {
		t.Fatal("status.lastSuccessfulRevision = nil, want value")
	}
	if got, want := doc.Status.LastSuccessfulRevision.DesiredStateDigest, lastSuccessfulDigest; got != want {
		t.Fatalf("lastSuccessfulRevision.desiredStateDigest = %q, want %q", got, want)
	}
	if got, want := doc.Spec.DesiredStateDigest, nextDesiredDigest; got != want {
		t.Fatalf("spec.desiredStateDigest = %q, want %q", got, want)
	}
}

func TestMarkSuccessfulApply(t *testing.T) {
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

	if _, err := PersistRenderedState(
		"cluster-a",
		ref,
		"sha256:eeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeee",
		"local-rev-1",
	); err != nil {
		t.Fatalf("PersistRenderedState() error = %v", err)
	}

	doc, err := MarkSuccessfulApply("cluster-a")
	if err != nil {
		t.Fatalf("MarkSuccessfulApply() error = %v", err)
	}
	if got, want := doc.Status.State, StateClean; got != want {
		t.Fatalf("status.state = %q, want %q", got, want)
	}
	if doc.Status.LastAppliedTime == nil {
		t.Fatal("status.lastAppliedTime = nil, want value")
	}
	if doc.Status.LastSuccessfulRevision == nil {
		t.Fatal("status.lastSuccessfulRevision = nil, want value")
	}
	if got, want := doc.Status.LastSuccessfulRevision.DesiredStateDigest, doc.Spec.DesiredStateDigest; got != want {
		t.Fatalf("lastSuccessfulRevision.desiredStateDigest = %q, want %q", got, want)
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
