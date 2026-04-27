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

type RevisionSnapshot struct {
	BOM                BOMReference `json:"bom" yaml:"bom"`
	LocalPatchRevision string       `json:"localPatchRevision,omitempty" yaml:"localPatchRevision,omitempty"`
	DesiredStateDigest string       `json:"desiredStateDigest" yaml:"desiredStateDigest"`
}

type Condition struct {
	Type               string                 `json:"type" yaml:"type"`
	Status             corev1.ConditionStatus `json:"status" yaml:"status"`
	LastTransitionTime metav1.Time            `json:"lastTransitionTime,omitempty" yaml:"lastTransitionTime,omitempty"`
	Reason             string                 `json:"reason,omitempty" yaml:"reason,omitempty"`
	Message            string                 `json:"message,omitempty" yaml:"message,omitempty"`
}

type AppliedRevisionSpec struct {
	ClusterName        string       `json:"clusterName" yaml:"clusterName"`
	BOM                BOMReference `json:"bom" yaml:"bom"`
	LocalPatchRevision string       `json:"localPatchRevision,omitempty" yaml:"localPatchRevision,omitempty"`
	DesiredStateDigest string       `json:"desiredStateDigest" yaml:"desiredStateDigest"`
}

type AppliedRevisionStatus struct {
	State                  ClusterState      `json:"state" yaml:"state"`
	LastAppliedTime        *metav1.Time      `json:"lastAppliedTime,omitempty" yaml:"lastAppliedTime,omitempty"`
	LastSuccessfulRevision *RevisionSnapshot `json:"lastSuccessfulRevision,omitempty" yaml:"lastSuccessfulRevision,omitempty"`
	Conditions             []Condition       `json:"conditions,omitempty" yaml:"conditions,omitempty"`
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

func (r RevisionSnapshot) Validate() error {
	if err := r.BOM.Validate(); err != nil {
		return fmt.Errorf("bom: %w", err)
	}
	if err := validateDigest("desiredStateDigest", r.DesiredStateDigest); err != nil {
		return err
	}
	return nil
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

func validateDigest(field, value string) error {
	if value == "" {
		return fmt.Errorf("%s cannot be empty", field)
	}
	if _, err := digest.Parse(value); err != nil {
		return fmt.Errorf("invalid %s %q: %w", field, value, err)
	}
	return nil
}
