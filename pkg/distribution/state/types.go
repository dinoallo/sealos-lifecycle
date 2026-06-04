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
	"fmt"

	"github.com/opencontainers/go-digest"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/yaml"

	"github.com/labring/sealos/pkg/distribution"
	"github.com/labring/sealos/pkg/distribution/bom"
)

type ClusterState string

const (
	StateClean    ClusterState = "Clean"
	StateDirty    ClusterState = "Dirty"
	StateOrphan   ClusterState = "Orphan"
	StateDegraded ClusterState = "Degraded"
)

type Metadata struct {
	Name   string            `json:"name" yaml:"name"`
	Labels map[string]string `json:"labels,omitempty" yaml:"labels,omitempty"`
}

type BOMReference struct {
	Name     string             `json:"name" yaml:"name"`
	Revision string             `json:"revision" yaml:"revision"`
	Channel  bom.ReleaseChannel `json:"channel" yaml:"channel"`
	Digest   string             `json:"digest,omitempty" yaml:"digest,omitempty"`
}

type TargetKind string

const (
	TargetKindBOM                  TargetKind = "bom"
	TargetKindReleaseChannelFile   TargetKind = "releaseChannelFile"
	TargetKindReleaseChannelLookup TargetKind = "releaseChannelLookup"
)

type RequestedTarget struct {
	Kind                 TargetKind         `json:"kind" yaml:"kind"`
	BOMPath              string             `json:"bomPath,omitempty" yaml:"bomPath,omitempty"`
	BOMDigest            string             `json:"bomDigest,omitempty" yaml:"bomDigest,omitempty"`
	ReleaseChannelPath   string             `json:"releaseChannelPath,omitempty" yaml:"releaseChannelPath,omitempty"`
	ReleaseChannelDigest string             `json:"releaseChannelDigest,omitempty" yaml:"releaseChannelDigest,omitempty"`
	ReleaseSource        string             `json:"releaseSource,omitempty" yaml:"releaseSource,omitempty"`
	DistributionLine     string             `json:"distributionLine,omitempty" yaml:"distributionLine,omitempty"`
	Channel              bom.ReleaseChannel `json:"channel,omitempty" yaml:"channel,omitempty"`
}

type ReleaseChannelReference struct {
	Name             string             `json:"name,omitempty" yaml:"name,omitempty"`
	DistributionLine string             `json:"distributionLine" yaml:"distributionLine"`
	Channel          bom.ReleaseChannel `json:"channel" yaml:"channel"`
	TargetRevision   string             `json:"targetRevision" yaml:"targetRevision"`
	Source           string             `json:"source,omitempty" yaml:"source,omitempty"`
	Digest           string             `json:"digest,omitempty" yaml:"digest,omitempty"`
}

type ResolvedTarget struct {
	BOM            BOMReference             `json:"bom" yaml:"bom"`
	ReleaseChannel *ReleaseChannelReference `json:"releaseChannel,omitempty" yaml:"releaseChannel,omitempty"`
}

type TargetState struct {
	Requested *RequestedTarget `json:"requested,omitempty" yaml:"requested,omitempty"`
	Resolved  *ResolvedTarget  `json:"resolved,omitempty" yaml:"resolved,omitempty"`
}

type RevisionSnapshot struct {
	BOM                BOMReference     `json:"bom" yaml:"bom"`
	RequestedTarget    *RequestedTarget `json:"requestedTarget,omitempty" yaml:"requestedTarget,omitempty"`
	ResolvedTarget     *ResolvedTarget  `json:"resolvedTarget,omitempty" yaml:"resolvedTarget,omitempty"`
	LocalRepoRevision  string           `json:"localRepoRevision,omitempty" yaml:"localRepoRevision,omitempty"`
	LocalPatchRevision string           `json:"localPatchRevision,omitempty" yaml:"localPatchRevision,omitempty"`
	DesiredStateDigest string           `json:"desiredStateDigest" yaml:"desiredStateDigest"`
}

type Condition struct {
	Type               string                 `json:"type" yaml:"type"`
	Status             corev1.ConditionStatus `json:"status" yaml:"status"`
	LastTransitionTime metav1.Time            `json:"lastTransitionTime,omitempty" yaml:"lastTransitionTime,omitempty"`
	Reason             string                 `json:"reason,omitempty" yaml:"reason,omitempty"`
	Message            string                 `json:"message,omitempty" yaml:"message,omitempty"`
}

type AppliedRevisionSpec struct {
	ClusterName        string           `json:"clusterName" yaml:"clusterName"`
	BOM                BOMReference     `json:"bom" yaml:"bom"`
	RequestedTarget    *RequestedTarget `json:"requestedTarget,omitempty" yaml:"requestedTarget,omitempty"`
	ResolvedTarget     *ResolvedTarget  `json:"resolvedTarget,omitempty" yaml:"resolvedTarget,omitempty"`
	LocalRepoRevision  string           `json:"localRepoRevision,omitempty" yaml:"localRepoRevision,omitempty"`
	LocalPatchRevision string           `json:"localPatchRevision,omitempty" yaml:"localPatchRevision,omitempty"`
	DesiredStateDigest string           `json:"desiredStateDigest" yaml:"desiredStateDigest"`
}

type AppliedRevisionStatus struct {
	State                  ClusterState       `json:"state" yaml:"state"`
	LastAppliedTime        *metav1.Time       `json:"lastAppliedTime,omitempty" yaml:"lastAppliedTime,omitempty"`
	LastSuccessfulRevision *RevisionSnapshot  `json:"lastSuccessfulRevision,omitempty" yaml:"lastSuccessfulRevision,omitempty"`
	SuccessfulRevisions    []RevisionSnapshot `json:"successfulRevisions,omitempty" yaml:"successfulRevisions,omitempty"`
	ObservedSummary        *ObservedSummary   `json:"observedSummary,omitempty" yaml:"observedSummary,omitempty"`
	Conditions             []Condition        `json:"conditions,omitempty" yaml:"conditions,omitempty"`
}

type ObservedSummary struct {
	LastObservedTime     *metav1.Time `json:"lastObservedTime,omitempty" yaml:"lastObservedTime,omitempty"`
	Total                int          `json:"total" yaml:"total"`
	Present              int          `json:"present" yaml:"present"`
	Missing              int          `json:"missing" yaml:"missing"`
	Matched              int          `json:"matched" yaml:"matched"`
	Drifted              int          `json:"drifted" yaml:"drifted"`
	Clean                int          `json:"clean" yaml:"clean"`
	Dirty                int          `json:"dirty" yaml:"dirty"`
	Orphan               int          `json:"orphan" yaml:"orphan"`
	MixedOwnershipObject int          `json:"mixedOwnershipObject" yaml:"mixedOwnershipObject"`
	DirectCommitEligible int          `json:"directCommitEligible" yaml:"directCommitEligible"`
	DirectRevertEligible int          `json:"directRevertEligible" yaml:"directRevertEligible"`
	BundleMatchRequired  int          `json:"bundleMatchRequired" yaml:"bundleMatchRequired"`
}

type AppliedRevision struct {
	APIVersion string                `json:"apiVersion" yaml:"apiVersion"`
	Kind       string                `json:"kind" yaml:"kind"`
	Metadata   Metadata              `json:"metadata" yaml:"metadata"`
	Spec       AppliedRevisionSpec   `json:"spec" yaml:"spec"`
	Status     AppliedRevisionStatus `json:"status" yaml:"status"`
}

func NewAppliedRevision(name, clusterName string, ref BOMReference, desiredStateDigest string) *AppliedRevision {
	return &AppliedRevision{
		APIVersion: distribution.APIVersion,
		Kind:       distribution.KindAppliedRevision,
		Metadata: Metadata{
			Name: name,
		},
		Spec: AppliedRevisionSpec{
			ClusterName:        clusterName,
			BOM:                ref,
			DesiredStateDigest: desiredStateDigest,
		},
		Status: AppliedRevisionStatus{
			State: StateDegraded,
		},
	}
}

func NewCondition(conditionType string, status corev1.ConditionStatus, reason, message string) Condition {
	return Condition{
		Type:               conditionType,
		Status:             status,
		LastTransitionTime: metav1.Now(),
		Reason:             reason,
		Message:            message,
	}
}

func (a AppliedRevision) String() string {
	data, _ := yaml.Marshal(a)
	return string(data)
}

func (a AppliedRevision) Validate() error {
	if a.APIVersion != distribution.APIVersion {
		return fmt.Errorf("unsupported apiVersion %q", a.APIVersion)
	}
	if a.Kind != distribution.KindAppliedRevision {
		return fmt.Errorf("unsupported kind %q", a.Kind)
	}
	if a.Metadata.Name == "" {
		return fmt.Errorf("metadata.name cannot be empty")
	}
	if err := a.Spec.Validate(); err != nil {
		return fmt.Errorf("spec: %w", err)
	}
	if err := a.Status.Validate(); err != nil {
		return fmt.Errorf("status: %w", err)
	}
	return nil
}

func (s AppliedRevisionSpec) Validate() error {
	if s.ClusterName == "" {
		return fmt.Errorf("clusterName cannot be empty")
	}
	if err := s.BOM.Validate(); err != nil {
		return fmt.Errorf("bom: %w", err)
	}
	if err := validateTargetPair(s.RequestedTarget, s.ResolvedTarget); err != nil {
		return err
	}
	if s.ResolvedTarget != nil && !sameBOMReference(s.BOM, s.ResolvedTarget.BOM) {
		return fmt.Errorf("resolvedTarget.bom must match bom")
	}
	if err := validateDigest("desiredStateDigest", s.DesiredStateDigest); err != nil {
		return err
	}
	return nil
}

func (s AppliedRevisionStatus) Validate() error {
	if err := s.State.Validate(); err != nil {
		return fmt.Errorf("state: %w", err)
	}
	if s.LastSuccessfulRevision != nil {
		if err := s.LastSuccessfulRevision.Validate(); err != nil {
			return fmt.Errorf("lastSuccessfulRevision: %w", err)
		}
	}
	for i, revision := range s.SuccessfulRevisions {
		if err := revision.Validate(); err != nil {
			return fmt.Errorf("successfulRevisions[%d]: %w", i, err)
		}
	}
	if s.ObservedSummary != nil {
		if err := s.ObservedSummary.Validate(); err != nil {
			return fmt.Errorf("observedSummary: %w", err)
		}
	}

	conditionTypes := make(map[string]struct{}, len(s.Conditions))
	for i, condition := range s.Conditions {
		if err := condition.Validate(); err != nil {
			return fmt.Errorf("conditions[%d]: %w", i, err)
		}
		if _, ok := conditionTypes[condition.Type]; ok {
			return fmt.Errorf("conditions[%d]: duplicate condition type %q", i, condition.Type)
		}
		conditionTypes[condition.Type] = struct{}{}
	}
	return nil
}

func (s ClusterState) Validate() error {
	switch s {
	case StateClean, StateDirty, StateOrphan, StateDegraded:
		return nil
	default:
		return fmt.Errorf("invalid cluster state %q", s)
	}
}

func (r BOMReference) Validate() error {
	if r.Name == "" {
		return fmt.Errorf("name cannot be empty")
	}
	if r.Revision == "" {
		return fmt.Errorf("revision cannot be empty")
	}
	if err := r.Channel.Validate(); err != nil {
		return fmt.Errorf("channel: %w", err)
	}
	if r.Digest != "" {
		if err := validateDigest("digest", r.Digest); err != nil {
			return err
		}
	}
	return nil
}

func (k TargetKind) Validate() error {
	switch k {
	case TargetKindBOM, TargetKindReleaseChannelFile, TargetKindReleaseChannelLookup:
		return nil
	default:
		return fmt.Errorf("invalid target kind %q", k)
	}
}

func (t RequestedTarget) Validate() error {
	if err := t.Kind.Validate(); err != nil {
		return err
	}
	switch t.Kind {
	case TargetKindBOM:
		if t.BOMPath == "" && t.BOMDigest == "" {
			return fmt.Errorf("bomPath or bomDigest is required for bom target")
		}
	case TargetKindReleaseChannelFile:
		if t.ReleaseChannelPath == "" {
			return fmt.Errorf("releaseChannelPath is required for releaseChannelFile target")
		}
		if t.DistributionLine == "" {
			return fmt.Errorf("distributionLine is required for releaseChannelFile target")
		}
		if err := t.Channel.ValidateRequired(); err != nil {
			return fmt.Errorf("channel: %w", err)
		}
	case TargetKindReleaseChannelLookup:
		if t.ReleaseSource == "" {
			return fmt.Errorf("releaseSource is required for releaseChannelLookup target")
		}
		if t.DistributionLine == "" {
			return fmt.Errorf("distributionLine is required for releaseChannelLookup target")
		}
		if err := t.Channel.ValidateRequired(); err != nil {
			return fmt.Errorf("channel: %w", err)
		}
	}
	if t.BOMDigest != "" {
		if err := validateDigest("bomDigest", t.BOMDigest); err != nil {
			return err
		}
	}
	if t.ReleaseChannelDigest != "" {
		if err := validateDigest("releaseChannelDigest", t.ReleaseChannelDigest); err != nil {
			return err
		}
	}
	return nil
}

func (r ReleaseChannelReference) Validate() error {
	if r.DistributionLine == "" {
		return fmt.Errorf("distributionLine cannot be empty")
	}
	if err := r.Channel.ValidateRequired(); err != nil {
		return fmt.Errorf("channel: %w", err)
	}
	if r.TargetRevision == "" {
		return fmt.Errorf("targetRevision cannot be empty")
	}
	if r.Digest != "" {
		if err := validateDigest("digest", r.Digest); err != nil {
			return err
		}
	}
	return nil
}

func (r ResolvedTarget) Validate() error {
	if err := r.BOM.Validate(); err != nil {
		return fmt.Errorf("bom: %w", err)
	}
	if r.ReleaseChannel != nil {
		if err := r.ReleaseChannel.Validate(); err != nil {
			return fmt.Errorf("releaseChannel: %w", err)
		}
		if r.ReleaseChannel.DistributionLine != r.BOM.Name {
			return fmt.Errorf("releaseChannel.distributionLine must match bom.name")
		}
		if r.ReleaseChannel.TargetRevision != r.BOM.Revision {
			return fmt.Errorf("releaseChannel.targetRevision must match bom.revision")
		}
		if r.ReleaseChannel.Channel != r.BOM.Channel {
			return fmt.Errorf("releaseChannel.channel must match bom.channel")
		}
	}
	return nil
}

func (r RevisionSnapshot) Validate() error {
	if err := r.BOM.Validate(); err != nil {
		return fmt.Errorf("bom: %w", err)
	}
	if err := validateTargetPair(r.RequestedTarget, r.ResolvedTarget); err != nil {
		return err
	}
	if r.ResolvedTarget != nil && !sameBOMReference(r.BOM, r.ResolvedTarget.BOM) {
		return fmt.Errorf("resolvedTarget.bom must match bom")
	}
	if err := validateDigest("desiredStateDigest", r.DesiredStateDigest); err != nil {
		return err
	}
	return nil
}

func validateTargetPair(requested *RequestedTarget, resolved *ResolvedTarget) error {
	if requested == nil && resolved == nil {
		return nil
	}
	if requested == nil || resolved == nil {
		return fmt.Errorf("requestedTarget and resolvedTarget must be recorded together")
	}
	if err := requested.Validate(); err != nil {
		return fmt.Errorf("requestedTarget: %w", err)
	}
	if err := resolved.Validate(); err != nil {
		return fmt.Errorf("resolvedTarget: %w", err)
	}
	return nil
}

func sameBOMReference(left, right BOMReference) bool {
	return left.Name == right.Name &&
		left.Revision == right.Revision &&
		left.Channel == right.Channel &&
		left.Digest == right.Digest
}

func (c Condition) Validate() error {
	if c.Type == "" {
		return fmt.Errorf("type cannot be empty")
	}
	switch c.Status {
	case corev1.ConditionTrue, corev1.ConditionFalse, corev1.ConditionUnknown:
		return nil
	default:
		return fmt.Errorf("invalid condition status %q", c.Status)
	}
}

func (s ObservedSummary) Validate() error {
	for _, field := range []struct {
		name  string
		value int
	}{
		{name: "total", value: s.Total},
		{name: "present", value: s.Present},
		{name: "missing", value: s.Missing},
		{name: "matched", value: s.Matched},
		{name: "drifted", value: s.Drifted},
		{name: "clean", value: s.Clean},
		{name: "dirty", value: s.Dirty},
		{name: "orphan", value: s.Orphan},
		{name: "mixedOwnershipObject", value: s.MixedOwnershipObject},
		{name: "directCommitEligible", value: s.DirectCommitEligible},
		{name: "directRevertEligible", value: s.DirectRevertEligible},
		{name: "bundleMatchRequired", value: s.BundleMatchRequired},
	} {
		if field.value < 0 {
			return fmt.Errorf("%s cannot be negative", field.name)
		}
	}
	return nil
}

func validateDigest(field, value string) error {
	if value == "" {
		return fmt.Errorf("%s cannot be empty", field)
	}
	if _, err := digest.Parse(value); err != nil {
		return fmt.Errorf("invalid %s %q: %w", field, value, err)
	}
	return nil
}
