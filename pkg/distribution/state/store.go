// Copyright 2024 sealos.
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
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/labring/sealos/pkg/constants"
	yamlutil "github.com/labring/sealos/pkg/utils/yaml"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

const (
	StoreDirName            = "distribution"
	AppliedRevisionFileName = "applied-revision.yaml"
	ConditionTypeApplied    = "Applied"
	ConditionTypeObserved   = "Observed"

	maxSuccessfulRevisionHistory = 10
)

func CurrentAppliedRevisionName(clusterName string) string {
	if clusterName == "" {
		return ""
	}
	return clusterName + "-current"
}

func AppliedRevisionPath(clusterName string) string {
	return filepath.Join(
		constants.NewPathResolver(clusterName).RunRoot(),
		StoreDirName,
		AppliedRevisionFileName,
	)
}

type PersistRevisionOptions struct {
	RequestedTarget *RequestedTarget
	ResolvedTarget  *ResolvedTarget
}

func ApplyRevisionSnapshotToSpec(doc *AppliedRevision, snapshot RevisionSnapshot) {
	if doc == nil {
		return
	}
	doc.Spec.BOM = snapshot.BOM
	doc.Spec.RequestedTarget = cloneRequestedTarget(snapshot.RequestedTarget)
	doc.Spec.ResolvedTarget = cloneResolvedTarget(snapshot.ResolvedTarget)
	doc.Spec.LocalRepoRevision = snapshot.LocalRepoRevision
	doc.Spec.LocalPatchRevision = snapshot.LocalPatchRevision
	doc.Spec.DesiredStateDigest = snapshot.DesiredStateDigest
}

func LoadAppliedRevision(clusterName string) (*AppliedRevision, error) {
	if clusterName == "" {
		return nil, fmt.Errorf("cluster name cannot be empty")
	}

	path := AppliedRevisionPath(clusterName)
	var doc AppliedRevision
	if err := yamlutil.UnmarshalFile(path, &doc); err != nil {
		return nil, fmt.Errorf("load applied revision %q: %w", path, err)
	}
	if err := doc.Validate(); err != nil {
		return nil, fmt.Errorf("validate applied revision %q: %w", path, err)
	}
	return &doc, nil
}

func SaveAppliedRevision(doc *AppliedRevision) error {
	if doc == nil {
		return fmt.Errorf("applied revision cannot be nil")
	}
	if err := doc.Validate(); err != nil {
		return fmt.Errorf("validate applied revision: %w", err)
	}

	path := AppliedRevisionPath(doc.Spec.ClusterName)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("create applied revision directory %q: %w", filepath.Dir(path), err)
	}
	if err := yamlutil.MarshalFile(path, doc); err != nil {
		return fmt.Errorf("write applied revision %q: %w", path, err)
	}
	return nil
}

func PersistRenderedState(
	clusterName string,
	ref BOMReference,
	desiredStateDigest, localRepoRevision, localPatchRevision string,
) (*AppliedRevision, error) {
	return PersistRenderedStateWithOptions(
		clusterName,
		ref,
		desiredStateDigest,
		localRepoRevision,
		localPatchRevision,
		PersistRevisionOptions{},
	)
}

func PersistRenderedStateWithOptions(
	clusterName string,
	ref BOMReference,
	desiredStateDigest, localRepoRevision, localPatchRevision string,
	opts PersistRevisionOptions,
) (*AppliedRevision, error) {
	doc := NewAppliedRevision(
		CurrentAppliedRevisionName(clusterName),
		clusterName,
		ref,
		desiredStateDigest,
	)
	doc.Spec.LocalRepoRevision = localRepoRevision
	doc.Spec.LocalPatchRevision = localPatchRevision
	doc.Spec.RequestedTarget = cloneRequestedTarget(opts.RequestedTarget)
	doc.Spec.ResolvedTarget = cloneResolvedTarget(opts.ResolvedTarget)

	existing, err := LoadAppliedRevision(clusterName)
	switch {
	case err == nil:
		doc.Status.LastAppliedTime = existing.Status.LastAppliedTime
		doc.Status.LastSuccessfulRevision = cloneRevisionSnapshot(
			existing.Status.LastSuccessfulRevision,
		)
		doc.Status.SuccessfulRevisions = cloneRevisionSnapshots(existing.Status.SuccessfulRevisions)
	case errors.Is(err, os.ErrNotExist):
		existing = nil
	default:
		return nil, err
	}

	if existing != nil &&
		existing.Status.State == StateClean &&
		existing.Status.LastSuccessfulRevision != nil &&
		existing.Status.LastSuccessfulRevision.DesiredStateDigest == desiredStateDigest {
		doc.Status = existing.Status
	} else {
		doc.Status.State = StateDirty
		doc.Status.ObservedSummary = nil
		doc.Status.Conditions = []Condition{
			NewCondition(ConditionTypeApplied, corev1.ConditionFalse, "DesiredStateRendered", "desired revision rendered but not yet applied"),
		}
	}

	if err := SaveAppliedRevision(doc); err != nil {
		return nil, err
	}
	return doc, nil
}

func PersistSuccessfulApply(
	clusterName string,
	ref BOMReference,
	desiredStateDigest, localRepoRevision, localPatchRevision string,
) (*AppliedRevision, error) {
	return PersistSuccessfulApplyWithOptions(
		clusterName,
		ref,
		desiredStateDigest,
		localRepoRevision,
		localPatchRevision,
		PersistRevisionOptions{},
	)
}

func PersistSuccessfulApplyWithOptions(
	clusterName string,
	ref BOMReference,
	desiredStateDigest, localRepoRevision, localPatchRevision string,
	opts PersistRevisionOptions,
) (*AppliedRevision, error) {
	existing, err := LoadAppliedRevision(clusterName)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return nil, err
	}

	doc := NewAppliedRevision(
		CurrentAppliedRevisionName(clusterName),
		clusterName,
		ref,
		desiredStateDigest,
	)
	doc.Spec.LocalRepoRevision = localRepoRevision
	doc.Spec.LocalPatchRevision = localPatchRevision
	doc.Spec.RequestedTarget = cloneRequestedTarget(opts.RequestedTarget)
	doc.Spec.ResolvedTarget = cloneResolvedTarget(opts.ResolvedTarget)

	now := metav1.Now()
	snapshot := RevisionSnapshot{
		BOM:                ref,
		RequestedTarget:    cloneRequestedTarget(doc.Spec.RequestedTarget),
		ResolvedTarget:     cloneResolvedTarget(doc.Spec.ResolvedTarget),
		LocalRepoRevision:  localRepoRevision,
		LocalPatchRevision: localPatchRevision,
		DesiredStateDigest: desiredStateDigest,
	}
	doc.Status.State = StateClean
	doc.Status.LastAppliedTime = &now
	doc.Status.ObservedSummary = nil
	doc.Status.LastSuccessfulRevision = &snapshot
	doc.Status.SuccessfulRevisions = prependSuccessfulRevisionHistory(snapshot, existing)
	doc.Status.Conditions = []Condition{
		NewCondition(
			ConditionTypeApplied,
			corev1.ConditionTrue,
			"ReconcileSucceeded",
			"desired revision applied",
		),
	}

	if err := SaveAppliedRevision(doc); err != nil {
		return nil, err
	}
	return doc, nil
}

func MarkSuccessfulApply(clusterName string) (*AppliedRevision, error) {
	doc, err := LoadAppliedRevision(clusterName)
	if err != nil {
		return nil, err
	}

	now := metav1.Now()
	snapshot := RevisionSnapshot{
		BOM:                doc.Spec.BOM,
		RequestedTarget:    cloneRequestedTarget(doc.Spec.RequestedTarget),
		ResolvedTarget:     cloneResolvedTarget(doc.Spec.ResolvedTarget),
		LocalRepoRevision:  doc.Spec.LocalRepoRevision,
		LocalPatchRevision: doc.Spec.LocalPatchRevision,
		DesiredStateDigest: doc.Spec.DesiredStateDigest,
	}
	doc.Status.State = StateClean
	doc.Status.LastAppliedTime = &now
	doc.Status.ObservedSummary = nil
	doc.Status.LastSuccessfulRevision = &snapshot
	doc.Status.SuccessfulRevisions = prependSuccessfulRevisionHistory(snapshot, doc)
	doc.Status.Conditions = []Condition{
		NewCondition(
			ConditionTypeApplied,
			corev1.ConditionTrue,
			"ReconcileSucceeded",
			"desired revision applied",
		),
	}

	if err := SaveAppliedRevision(doc); err != nil {
		return nil, err
	}
	return doc, nil
}

func MarkDegraded(clusterName, reason, message string) (*AppliedRevision, bool, error) {
	doc, err := LoadAppliedRevision(clusterName)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, false, nil
		}
		return nil, false, err
	}

	doc.Status.State = StateDegraded
	doc.Status.Conditions = upsertCondition(
		doc.Status.Conditions,
		NewCondition(ConditionTypeApplied, corev1.ConditionFalse, reason, message),
	)
	if err := SaveAppliedRevision(doc); err != nil {
		return nil, false, err
	}
	return doc, true, nil
}

func PersistObservedState(
	clusterName, desiredStateDigest string,
	observedState ClusterState,
	observedSummary *ObservedSummary,
	message string,
) (*AppliedRevision, bool, error) {
	if err := observedState.Validate(); err != nil {
		return nil, false, err
	}
	if observedSummary != nil {
		if err := observedSummary.Validate(); err != nil {
			return nil, false, err
		}
	}

	doc, err := LoadAppliedRevision(clusterName)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, false, nil
		}
		return nil, false, err
	}
	if desiredStateDigest != "" && doc.Spec.DesiredStateDigest != desiredStateDigest {
		return doc, false, nil
	}

	doc.Status.State = observedState
	doc.Status.ObservedSummary = cloneObservedSummary(observedSummary)
	if doc.Status.ObservedSummary != nil && doc.Status.ObservedSummary.LastObservedTime == nil {
		now := metav1.Now()
		doc.Status.ObservedSummary.LastObservedTime = &now
	}
	doc.Status.Conditions = upsertCondition(
		doc.Status.Conditions,
		observedStateCondition(observedState, message),
	)
	if err := SaveAppliedRevision(doc); err != nil {
		return nil, false, err
	}
	return doc, true, nil
}

func observedStateCondition(observedState ClusterState, message string) Condition {
	switch observedState {
	case StateClean:
		return NewCondition(
			ConditionTypeObserved,
			corev1.ConditionFalse,
			"LiveStateMatchesDesired",
			observedMessage(message, "live tracked state matches desired ownership state"),
		)
	case StateDirty:
		return NewCondition(
			ConditionTypeObserved,
			corev1.ConditionTrue,
			"LocalOwnershipDriftDetected",
			observedMessage(message, "local-owned drift detected"),
		)
	case StateOrphan:
		return NewCondition(
			ConditionTypeObserved,
			corev1.ConditionTrue,
			"GlobalOwnershipDriftDetected",
			observedMessage(message, "global-owned drift detected"),
		)
	default:
		return NewCondition(
			ConditionTypeObserved,
			corev1.ConditionUnknown,
			"ObservationIncomplete",
			observedMessage(message, "live ownership state could not be fully determined"),
		)
	}
}

func observedMessage(message, fallback string) string {
	if message == "" {
		return fallback
	}
	return message
}

func upsertCondition(conditions []Condition, condition Condition) []Condition {
	for i, existing := range conditions {
		if existing.Type != condition.Type {
			continue
		}
		conditions[i] = condition
		return conditions
	}
	return append(conditions, condition)
}

func cloneObservedSummary(summary *ObservedSummary) *ObservedSummary {
	if summary == nil {
		return nil
	}
	cloned := *summary
	if summary.LastObservedTime != nil {
		timestamp := *summary.LastObservedTime
		cloned.LastObservedTime = &timestamp
	}
	return &cloned
}

func prependSuccessfulRevisionHistory(
	snapshot RevisionSnapshot,
	existing *AppliedRevision,
) []RevisionSnapshot {
	history := []RevisionSnapshot{cloneRevisionSnapshotValue(snapshot)}
	if existing != nil {
		history = append(history, existing.Status.SuccessfulRevisions...)
		if len(existing.Status.SuccessfulRevisions) == 0 &&
			existing.Status.LastSuccessfulRevision != nil {
			history = append(history, *existing.Status.LastSuccessfulRevision)
		}
	}
	out := make([]RevisionSnapshot, 0, min(len(history), maxSuccessfulRevisionHistory))
	seen := make(map[string]struct{}, len(history))
	for _, revision := range history {
		key := revisionHistoryKey(revision)
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		out = append(out, cloneRevisionSnapshotValue(revision))
		if len(out) == maxSuccessfulRevisionHistory {
			break
		}
	}
	return out
}

func revisionHistoryKey(revision RevisionSnapshot) string {
	return revision.BOM.Name + "\x00" +
		revision.BOM.Revision + "\x00" +
		string(revision.BOM.Channel) + "\x00" +
		revision.BOM.Digest + "\x00" +
		revision.DesiredStateDigest
}

func cloneRevisionSnapshot(snapshot *RevisionSnapshot) *RevisionSnapshot {
	if snapshot == nil {
		return nil
	}
	cloned := cloneRevisionSnapshotValue(*snapshot)
	return &cloned
}

func cloneRevisionSnapshotValue(snapshot RevisionSnapshot) RevisionSnapshot {
	cloned := snapshot
	cloned.RequestedTarget = cloneRequestedTarget(snapshot.RequestedTarget)
	cloned.ResolvedTarget = cloneResolvedTarget(snapshot.ResolvedTarget)
	return cloned
}

func cloneRevisionSnapshots(snapshots []RevisionSnapshot) []RevisionSnapshot {
	if len(snapshots) == 0 {
		return nil
	}
	cloned := make([]RevisionSnapshot, 0, len(snapshots))
	for _, snapshot := range snapshots {
		cloned = append(cloned, cloneRevisionSnapshotValue(snapshot))
	}
	return cloned
}

func cloneRequestedTarget(target *RequestedTarget) *RequestedTarget {
	if target == nil {
		return nil
	}
	cloned := *target
	return &cloned
}

func cloneResolvedTarget(target *ResolvedTarget) *ResolvedTarget {
	if target == nil {
		return nil
	}
	cloned := *target
	if target.ReleaseChannel != nil {
		releaseChannel := *target.ReleaseChannel
		cloned.ReleaseChannel = &releaseChannel
	}
	return &cloned
}
