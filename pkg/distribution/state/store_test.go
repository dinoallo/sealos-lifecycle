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
		"sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
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
	if got, want := loaded.Spec.LocalRepoRevision, "sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"; got != want {
		t.Fatalf("spec.localRepoRevision = %q, want %q", got, want)
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
		"sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
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

	if _, err := PersistSuccessfulApply("cluster-a", ref, desiredStateDigest, "sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", "local-rev-1"); err != nil {
		t.Fatalf("PersistSuccessfulApply() error = %v", err)
	}

	doc, err := PersistRenderedState("cluster-a", ref, desiredStateDigest, "sha256:bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb", "local-rev-2")
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

	if _, err := PersistSuccessfulApply("cluster-a", ref, lastSuccessfulDigest, "sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", "local-rev-1"); err != nil {
		t.Fatalf("PersistSuccessfulApply() error = %v", err)
	}

	doc, err := PersistRenderedState("cluster-a", ref, nextDesiredDigest, "sha256:bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb", "local-rev-2")
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
		"sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
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

func TestPersistObservedState(t *testing.T) {
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

	if _, err := PersistSuccessfulApply("cluster-a", ref, desiredStateDigest, "sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", "local-rev-1"); err != nil {
		t.Fatalf("PersistSuccessfulApply() error = %v", err)
	}

	doc, updated, err := PersistObservedState("cluster-a", desiredStateDigest, StateOrphan, &ObservedSummary{
		Total:                3,
		Present:              2,
		Missing:              1,
		Matched:              1,
		Drifted:              1,
		Clean:                1,
		Dirty:                1,
		Orphan:               1,
		MixedOwnershipObject: 1,
		DirectCommitEligible: 1,
		DirectRevertEligible: 2,
		BundleMatchRequired:  2,
	}, "global drift detected while diffing tracked objects")
	if err != nil {
		t.Fatalf("PersistObservedState() error = %v", err)
	}
	if !updated {
		t.Fatal("PersistObservedState() updated = false, want true")
	}
	if got, want := doc.Status.State, StateOrphan; got != want {
		t.Fatalf("status.state = %q, want %q", got, want)
	}
	if len(doc.Status.Conditions) != 2 {
		t.Fatalf("len(status.conditions) = %d, want 2", len(doc.Status.Conditions))
	}
	var observed *Condition
	for i := range doc.Status.Conditions {
		if doc.Status.Conditions[i].Type == ConditionTypeObserved {
			observed = &doc.Status.Conditions[i]
			break
		}
	}
	if observed == nil {
		t.Fatal("observed condition missing")
	}
	if got, want := observed.Reason, "GlobalOwnershipDriftDetected"; got != want {
		t.Fatalf("observed.reason = %q, want %q", got, want)
	}
	if got, want := observed.Message, "global drift detected while diffing tracked objects"; got != want {
		t.Fatalf("observed.message = %q, want %q", got, want)
	}
	if doc.Status.ObservedSummary == nil {
		t.Fatal("status.observedSummary = nil, want value")
	}
	if got, want := doc.Status.ObservedSummary.Orphan, 1; got != want {
		t.Fatalf("status.observedSummary.orphan = %d, want %d", got, want)
	}
	if got, want := doc.Status.ObservedSummary.MixedOwnershipObject, 1; got != want {
		t.Fatalf("status.observedSummary.mixedOwnershipObject = %d, want %d", got, want)
	}
	if got, want := doc.Status.ObservedSummary.DirectCommitEligible, 1; got != want {
		t.Fatalf("status.observedSummary.directCommitEligible = %d, want %d", got, want)
	}
	if got, want := doc.Status.ObservedSummary.DirectRevertEligible, 2; got != want {
		t.Fatalf("status.observedSummary.directRevertEligible = %d, want %d", got, want)
	}
	if got, want := doc.Status.ObservedSummary.BundleMatchRequired, 2; got != want {
		t.Fatalf("status.observedSummary.bundleMatchRequired = %d, want %d", got, want)
	}
}

func TestPersistObservedStateSkipsMismatchedDigest(t *testing.T) {
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

	if _, err := PersistSuccessfulApply("cluster-a", ref, desiredStateDigest, "sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", "local-rev-1"); err != nil {
		t.Fatalf("PersistSuccessfulApply() error = %v", err)
	}

	doc, updated, err := PersistObservedState("cluster-a", "sha256:ffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffff", StateDirty, &ObservedSummary{
		Dirty: 1,
	}, "local drift")
	if err != nil {
		t.Fatalf("PersistObservedState() error = %v", err)
	}
	if updated {
		t.Fatal("PersistObservedState() updated = true, want false")
	}
	if got, want := doc.Status.State, StateClean; got != want {
		t.Fatalf("status.state = %q, want %q", got, want)
	}
	if doc.Status.ObservedSummary != nil {
		t.Fatalf("status.observedSummary = %#v, want nil", doc.Status.ObservedSummary)
	}
}
