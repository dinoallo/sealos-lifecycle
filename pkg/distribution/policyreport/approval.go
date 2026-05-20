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

package policyreport

import (
	"fmt"
	"os"
	"strings"
	"time"

	"sigs.k8s.io/yaml"

	"github.com/labring/sealos/pkg/distribution/ownership"
)

const (
	GateApprovalAPIVersion = "distribution.sealos.io/v1alpha1"
	GateApprovalKind       = "LocalPatchPolicyGateApproval"
)

type GateApprovalDocument struct {
	APIVersion string           `json:"apiVersion" yaml:"apiVersion"`
	Kind       string           `json:"kind" yaml:"kind"`
	Metadata   GateApprovalMeta `json:"metadata,omitempty" yaml:"metadata,omitempty"`
	Spec       GateApprovalSpec `json:"spec" yaml:"spec"`
}

type GateApprovalMeta struct {
	Name string `json:"name,omitempty" yaml:"name,omitempty"`
}

type GateApprovalSpec struct {
	Owner      string              `json:"owner" yaml:"owner"`
	ApprovedBy string              `json:"approvedBy" yaml:"approvedBy"`
	ChangeRef  string              `json:"changeRef" yaml:"changeRef"`
	ExpiresAt  string              `json:"expiresAt" yaml:"expiresAt"`
	OldPolicy  PolicyRef           `json:"oldPolicy" yaml:"oldPolicy"`
	NewPolicy  PolicyRef           `json:"newPolicy" yaml:"newPolicy"`
	Approvals  []GateApprovalEntry `json:"approvals,omitempty" yaml:"approvals,omitempty"`
}

type GateApprovalEntry struct {
	Code           GateViolationCode  `json:"code" yaml:"code"`
	ExpectedCount  int                `json:"expectedCount" yaml:"expectedCount"`
	ExpectedImpact GateApprovalImpact `json:"expectedImpact" yaml:"expectedImpact"`
	Reason         string             `json:"reason" yaml:"reason"`
}

type GateApprovalImpact struct {
	AddedAllowedPrefixes            []ownership.KindPath   `json:"addedAllowedPrefixes,omitempty" yaml:"addedAllowedPrefixes,omitempty"`
	RemovedForbiddenExactPaths      []string               `json:"removedForbiddenExactPaths,omitempty" yaml:"removedForbiddenExactPaths,omitempty"`
	RemovedForbiddenMetadataKeys    []string               `json:"removedForbiddenMetadataKeys,omitempty" yaml:"removedForbiddenMetadataKeys,omitempty"`
	RemovedForbiddenContainerFields []string               `json:"removedForbiddenContainerFields,omitempty" yaml:"removedForbiddenContainerFields,omitempty"`
	IncompatiblePatches             []GateApprovalPatchRef `json:"incompatiblePatches,omitempty" yaml:"incompatiblePatches,omitempty"`
}

type GateApprovalPatchRef struct {
	Component    string `json:"component" yaml:"component"`
	RelativePath string `json:"relativePath" yaml:"relativePath"`
}

func LoadGateApprovalFile(path string) (*GateApprovalDocument, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read local patch policy gate approval file %q: %w", path, err)
	}

	var doc GateApprovalDocument
	if err := yaml.Unmarshal(data, &doc); err != nil {
		return nil, fmt.Errorf("decode local patch policy gate approval file %q: %w", path, err)
	}
	if err := doc.Validate(); err != nil {
		return nil, fmt.Errorf("validate local patch policy gate approval file %q: %w", path, err)
	}
	return &doc, nil
}

func (d *GateApprovalDocument) Validate() error {
	if d == nil {
		return fmt.Errorf("local patch policy gate approval document cannot be nil")
	}
	if strings.TrimSpace(d.APIVersion) != GateApprovalAPIVersion {
		return fmt.Errorf("apiVersion must be %q", GateApprovalAPIVersion)
	}
	if strings.TrimSpace(d.Kind) != GateApprovalKind {
		return fmt.Errorf("kind must be %q", GateApprovalKind)
	}
	if strings.TrimSpace(d.Spec.Owner) == "" {
		return fmt.Errorf("spec.owner must be set")
	}
	if strings.TrimSpace(d.Spec.ApprovedBy) == "" {
		return fmt.Errorf("spec.approvedBy must be set")
	}
	if strings.TrimSpace(d.Spec.ChangeRef) == "" {
		return fmt.Errorf("spec.changeRef must be set")
	}
	if strings.TrimSpace(d.Spec.ExpiresAt) == "" {
		return fmt.Errorf("spec.expiresAt must be set")
	}
	if _, err := d.ExpirationTime(); err != nil {
		return err
	}
	if err := validateApprovalPolicyRef("spec.oldPolicy", d.Spec.OldPolicy); err != nil {
		return err
	}
	if err := validateApprovalPolicyRef("spec.newPolicy", d.Spec.NewPolicy); err != nil {
		return err
	}

	seen := make(map[GateViolationCode]struct{}, len(d.Spec.Approvals))
	for i, approval := range d.Spec.Approvals {
		switch approval.Code {
		case GateViolationWidening, GateViolationIncompatiblePatches:
		default:
			return fmt.Errorf("spec.approvals[%d].code must be one of %q or %q", i, GateViolationWidening, GateViolationIncompatiblePatches)
		}
		if strings.TrimSpace(approval.Reason) == "" {
			return fmt.Errorf("spec.approvals[%d].reason must be set", i)
		}
		if approval.ExpectedCount <= 0 {
			return fmt.Errorf("spec.approvals[%d].expectedCount must be greater than zero", i)
		}
		switch approval.Code {
		case GateViolationWidening:
			if got := approval.ExpectedImpact.wideningCount(); got == 0 {
				return fmt.Errorf("spec.approvals[%d].expectedImpact must describe at least one widening item", i)
			} else if got != approval.ExpectedCount {
				return fmt.Errorf("spec.approvals[%d].expectedCount must match widening impact count %d", i, got)
			}
			if got := approval.ExpectedImpact.incompatiblePatchCount(); got != 0 {
				return fmt.Errorf("spec.approvals[%d].expectedImpact.incompatiblePatches must be empty for %q", i, approval.Code)
			}
		case GateViolationIncompatiblePatches:
			if got := approval.ExpectedImpact.incompatiblePatchCount(); got == 0 {
				return fmt.Errorf("spec.approvals[%d].expectedImpact must describe at least one incompatible patch", i)
			} else if got != approval.ExpectedCount {
				return fmt.Errorf("spec.approvals[%d].expectedCount must match incompatible patch count %d", i, got)
			}
			if got := approval.ExpectedImpact.wideningCount(); got != 0 {
				return fmt.Errorf("spec.approvals[%d].expectedImpact widening fields must be empty for %q", i, approval.Code)
			}
		}
		if _, ok := seen[approval.Code]; ok {
			return fmt.Errorf("spec.approvals contains duplicate code %q", approval.Code)
		}
		seen[approval.Code] = struct{}{}
	}

	return nil
}

func (d *GateApprovalDocument) ExpirationTime() (time.Time, error) {
	if d == nil {
		return time.Time{}, fmt.Errorf("local patch policy gate approval document cannot be nil")
	}
	value := strings.TrimSpace(d.Spec.ExpiresAt)
	expiresAt, err := time.Parse(time.RFC3339, value)
	if err != nil {
		return time.Time{}, fmt.Errorf("spec.expiresAt must be a valid RFC3339 timestamp: %w", err)
	}
	return expiresAt, nil
}

func validateApprovalPolicyRef(field string, ref PolicyRef) error {
	if strings.TrimSpace(ref.Name) == "" {
		return fmt.Errorf("%s.name must be set", field)
	}
	if strings.TrimSpace(string(ref.Scope)) == "" {
		return fmt.Errorf("%s.scope must be set", field)
	}
	if strings.TrimSpace(ref.Digest) == "" {
		return fmt.Errorf("%s.digest must be set", field)
	}
	return nil
}

func (i GateApprovalImpact) wideningCount() int {
	return len(i.AddedAllowedPrefixes) +
		len(i.RemovedForbiddenExactPaths) +
		len(i.RemovedForbiddenMetadataKeys) +
		len(i.RemovedForbiddenContainerFields)
}

func (i GateApprovalImpact) incompatiblePatchCount() int {
	return len(i.IncompatiblePatches)
}
