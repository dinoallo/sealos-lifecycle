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
	"sort"
	"strings"
	"time"

	"github.com/labring/sealos/pkg/distribution/localrepo"
	"github.com/labring/sealos/pkg/distribution/ownership"
)

type GateConfig struct {
	FailOnWidening            bool `json:"failOnWidening" yaml:"failOnWidening"`
	FailOnIncompatiblePatches bool `json:"failOnIncompatiblePatches" yaml:"failOnIncompatiblePatches"`
	ApprovalExpiryWarningDays int  `json:"approvalExpiryWarningDays" yaml:"approvalExpiryWarningDays"`
	FailOnApprovalExpiresSoon bool `json:"failOnApprovalExpiresSoon" yaml:"failOnApprovalExpiresSoon"`
}

func DefaultGateConfig() GateConfig {
	return GateConfig{
		FailOnWidening:            true,
		FailOnIncompatiblePatches: true,
		ApprovalExpiryWarningDays: 30,
		FailOnApprovalExpiresSoon: false,
	}
}

type GateViolationCode string

const (
	GateViolationWidening            GateViolationCode = "wideningChange"
	GateViolationIncompatiblePatches GateViolationCode = "incompatiblePatches"
	GateViolationApprovalExpiresSoon GateViolationCode = "approvalExpiresSoon"
)

type GateViolation struct {
	Code    GateViolationCode `json:"code" yaml:"code"`
	Message string            `json:"message" yaml:"message"`
	Count   int               `json:"count,omitempty" yaml:"count,omitempty"`
}

type GateResult struct {
	Passed             bool                    `json:"passed" yaml:"passed"`
	Config             GateConfig              `json:"config" yaml:"config"`
	ApprovalSummary    GateApprovalSummary     `json:"approvalSummary" yaml:"approvalSummary"`
	Violations         []GateViolation         `json:"violations,omitempty" yaml:"violations,omitempty"`
	ApprovedViolations []ApprovedGateViolation `json:"approvedViolations,omitempty" yaml:"approvedViolations,omitempty"`
	Warnings           []string                `json:"warnings,omitempty" yaml:"warnings,omitempty"`
}

type ApprovedGateViolation struct {
	Code    GateViolationCode  `json:"code" yaml:"code"`
	Message string             `json:"message" yaml:"message"`
	Reason  string             `json:"reason" yaml:"reason"`
	Count   int                `json:"count,omitempty" yaml:"count,omitempty"`
	Impact  GateApprovalImpact `json:"impact,omitempty" yaml:"impact,omitempty"`
}

type GateApprovalSummary struct {
	ApprovalProvided       bool                `json:"approvalProvided" yaml:"approvalProvided"`
	ApprovalApplied        bool                `json:"approvalApplied" yaml:"approvalApplied"`
	ApprovedViolationCodes []GateViolationCode `json:"approvedViolationCodes,omitempty" yaml:"approvedViolationCodes,omitempty"`
	Owner                  string              `json:"owner,omitempty" yaml:"owner,omitempty"`
	ApprovedBy             string              `json:"approvedBy,omitempty" yaml:"approvedBy,omitempty"`
	ChangeRef              string              `json:"changeRef,omitempty" yaml:"changeRef,omitempty"`
	ExpiresAt              string              `json:"expiresAt,omitempty" yaml:"expiresAt,omitempty"`
	ExpiresSoon            bool                `json:"expiresSoon,omitempty" yaml:"expiresSoon,omitempty"`
	DaysUntilExpiry        int                 `json:"daysUntilExpiry,omitempty" yaml:"daysUntilExpiry,omitempty"`
	FollowUpAction         string              `json:"followUpAction,omitempty" yaml:"followUpAction,omitempty"`
}

type gateApprovalDecision struct {
	Entry GateApprovalEntry
}

var gateApprovalNow = time.Now

const (
	GateApprovalFollowUpRemoveAfterChangeRefResolved = "removeApprovalAfterChangeRefResolved"
	GateApprovalFollowUpRenewOrRemoveBeforeExpiry    = "renewOrRemoveApprovalBeforeExpiry"
)

func EvaluateGate(report *Report, config GateConfig) (*GateResult, error) {
	return EvaluateGateWithApproval(report, config, nil)
}

func EvaluateGateWithApproval(report *Report, config GateConfig, approval *GateApprovalDocument) (*GateResult, error) {
	if report == nil {
		return nil, fmt.Errorf("policy report cannot be nil")
	}

	result := &GateResult{
		Passed: true,
		Config: config,
		ApprovalSummary: GateApprovalSummary{
			ApprovalProvided: approval != nil,
		},
	}

	approved := make(map[GateViolationCode]gateApprovalDecision)
	consumed := make(map[GateViolationCode]struct{})
	if approval != nil {
		if err := approval.Validate(); err != nil {
			return nil, err
		}
		expiresAt, err := approval.ExpirationTime()
		if err != nil {
			return nil, err
		}
		result.ApprovalSummary.Owner = approval.Spec.Owner
		result.ApprovalSummary.ApprovedBy = approval.Spec.ApprovedBy
		result.ApprovalSummary.ChangeRef = approval.Spec.ChangeRef
		result.ApprovalSummary.ExpiresAt = expiresAt.UTC().Format(time.RFC3339)
		now := gateApprovalNow().UTC()
		if !expiresAt.After(now) {
			return nil, fmt.Errorf("approval file expired at %s", expiresAt.UTC().Format(time.RFC3339))
		}
		if window := config.approvalExpiryWarningWindow(); window > 0 && expiresAt.Sub(now) <= window {
			result.ApprovalSummary.ExpiresSoon = true
			result.ApprovalSummary.DaysUntilExpiry = gateApprovalDaysUntilExpiry(now, expiresAt)
			result.ApprovalSummary.FollowUpAction = GateApprovalFollowUpRenewOrRemoveBeforeExpiry
			message := fmt.Sprintf("approval file expires in %d day(s); renew or remove it before %s",
				result.ApprovalSummary.DaysUntilExpiry, expiresAt.UTC().Format(time.RFC3339))
			if config.FailOnApprovalExpiresSoon {
				result.Passed = false
				result.Violations = append(result.Violations, GateViolation{
					Code:    GateViolationApprovalExpiresSoon,
					Message: message,
					Count:   result.ApprovalSummary.DaysUntilExpiry,
				})
			} else {
				result.Warnings = append(result.Warnings, message)
			}
		}
		if !policyRefsEqual(approval.Spec.OldPolicy, report.Old) {
			return nil, fmt.Errorf("approval old policy identity does not match compared old policy")
		}
		if !policyRefsEqual(approval.Spec.NewPolicy, report.New) {
			return nil, fmt.Errorf("approval new policy identity does not match compared new policy")
		}
		for _, item := range approval.Spec.Approvals {
			approved[item.Code] = gateApprovalDecision{Entry: item}
		}
	}

	if config.FailOnWidening && report.HasWideningChanges {
		violation := GateViolation{
			Code:    GateViolationWidening,
			Message: "candidate LocalPatchPolicy widens cluster-local override surface",
			Count: len(report.Impact.AddedAllowedPrefixes) +
				len(report.Impact.RemovedForbiddenExactPaths) +
				len(report.Impact.RemovedForbiddenMetadataKeys) +
				len(report.Impact.RemovedForbiddenContainerFields),
		}
		if decision, ok := approved[GateViolationWidening]; ok {
			if decision.Entry.ExpectedCount != violation.Count {
				return nil, fmt.Errorf("approval for %q expected count %d, got %d", GateViolationWidening, decision.Entry.ExpectedCount, violation.Count)
			}
			actualImpact := wideningImpactFromReport(report)
			if !gateApprovalImpactEqual(decision.Entry.ExpectedImpact, actualImpact) {
				return nil, fmt.Errorf("approval for %q impact set does not match actual gate impact", GateViolationWidening)
			}
			consumed[GateViolationWidening] = struct{}{}
			result.ApprovedViolations = append(result.ApprovedViolations, ApprovedGateViolation{
				Code:    violation.Code,
				Message: violation.Message,
				Reason:  decision.Entry.Reason,
				Count:   violation.Count,
				Impact:  actualImpact,
			})
			result.Warnings = append(result.Warnings,
				fmt.Sprintf("widening change was explicitly approved: %s", decision.Entry.Reason))
		} else {
			result.Passed = false
			result.Violations = append(result.Violations, violation)
		}
	}

	if config.FailOnIncompatiblePatches && report.Compatibility.HasIncompatiblePatches() {
		violation := GateViolation{
			Code:    GateViolationIncompatiblePatches,
			Message: "candidate LocalPatchPolicy would reject existing local patches",
			Count:   len(report.Compatibility.InvalidPatches),
		}
		if decision, ok := approved[GateViolationIncompatiblePatches]; ok {
			if decision.Entry.ExpectedCount != violation.Count {
				return nil, fmt.Errorf("approval for %q expected count %d, got %d", GateViolationIncompatiblePatches, decision.Entry.ExpectedCount, violation.Count)
			}
			actualImpact := incompatiblePatchImpactFromReport(report.Compatibility.InvalidPatches)
			if !gateApprovalImpactEqual(decision.Entry.ExpectedImpact, actualImpact) {
				return nil, fmt.Errorf("approval for %q impact set does not match actual gate impact", GateViolationIncompatiblePatches)
			}
			consumed[GateViolationIncompatiblePatches] = struct{}{}
			result.ApprovedViolations = append(result.ApprovedViolations, ApprovedGateViolation{
				Code:    violation.Code,
				Message: violation.Message,
				Reason:  decision.Entry.Reason,
				Count:   violation.Count,
				Impact:  actualImpact,
			})
			result.Warnings = append(result.Warnings,
				fmt.Sprintf("incompatible existing patches were explicitly approved: %s", decision.Entry.Reason))
		} else {
			result.Passed = false
			result.Violations = append(result.Violations, violation)
		}
	}

	for code := range approved {
		if _, ok := consumed[code]; ok {
			continue
		}
		return nil, fmt.Errorf("approval for %q did not match any actual gate violation", code)
	}

	if report.HasNarrowingChanges {
		result.Warnings = append(result.Warnings,
			"candidate LocalPatchPolicy narrows previously allowed surface; review operator continuity impact")
	}

	if len(result.ApprovedViolations) > 0 {
		result.ApprovalSummary.ApprovalApplied = true
		result.ApprovalSummary.ApprovedViolationCodes = make([]GateViolationCode, 0, len(result.ApprovedViolations))
		for _, item := range result.ApprovedViolations {
			result.ApprovalSummary.ApprovedViolationCodes = append(result.ApprovalSummary.ApprovedViolationCodes, item.Code)
		}
		sort.Slice(result.ApprovalSummary.ApprovedViolationCodes, func(i, j int) bool {
			return result.ApprovalSummary.ApprovedViolationCodes[i] < result.ApprovalSummary.ApprovedViolationCodes[j]
		})
		if result.ApprovalSummary.FollowUpAction == "" {
			result.ApprovalSummary.FollowUpAction = GateApprovalFollowUpRemoveAfterChangeRefResolved
		}
		result.Warnings = append(result.Warnings,
			fmt.Sprintf("approval file was used to pass the gate; remove it after changeRef %q is resolved",
				result.ApprovalSummary.ChangeRef))
	}

	return result, nil
}

func gateApprovalDaysUntilExpiry(now, expiresAt time.Time) int {
	if !expiresAt.After(now) {
		return 0
	}
	duration := expiresAt.Sub(now)
	days := int(duration / (24 * time.Hour))
	if duration%(24*time.Hour) != 0 {
		days++
	}
	return days
}

func (c GateConfig) approvalExpiryWarningWindow() time.Duration {
	if c.ApprovalExpiryWarningDays <= 0 {
		return 0
	}
	return time.Duration(c.ApprovalExpiryWarningDays) * 24 * time.Hour
}

func policyRefsEqual(left, right PolicyRef) bool {
	return strings.TrimSpace(left.Name) == strings.TrimSpace(right.Name) &&
		strings.TrimSpace(string(left.Scope)) == strings.TrimSpace(string(right.Scope)) &&
		strings.TrimSpace(left.Digest) == strings.TrimSpace(right.Digest)
}

func wideningImpactFromReport(report *Report) GateApprovalImpact {
	return GateApprovalImpact{
		AddedAllowedPrefixes:            cloneKindPaths(report.Impact.AddedAllowedPrefixes),
		RemovedForbiddenExactPaths:      cloneStrings(report.Impact.RemovedForbiddenExactPaths),
		RemovedForbiddenMetadataKeys:    cloneStrings(report.Impact.RemovedForbiddenMetadataKeys),
		RemovedForbiddenContainerFields: cloneStrings(report.Impact.RemovedForbiddenContainerFields),
	}
}

func incompatiblePatchImpactFromReport(issues []localrepo.PolicyCompatibilityIssue) GateApprovalImpact {
	items := make([]GateApprovalPatchRef, 0, len(issues))
	for _, issue := range issues {
		items = append(items, GateApprovalPatchRef{
			Component:    issue.Component,
			RelativePath: issue.RelativePath,
		})
	}
	return GateApprovalImpact{IncompatiblePatches: items}
}

func gateApprovalImpactEqual(left, right GateApprovalImpact) bool {
	left = normalizedGateApprovalImpact(left)
	right = normalizedGateApprovalImpact(right)

	if len(left.AddedAllowedPrefixes) != len(right.AddedAllowedPrefixes) ||
		len(left.RemovedForbiddenExactPaths) != len(right.RemovedForbiddenExactPaths) ||
		len(left.RemovedForbiddenMetadataKeys) != len(right.RemovedForbiddenMetadataKeys) ||
		len(left.RemovedForbiddenContainerFields) != len(right.RemovedForbiddenContainerFields) ||
		len(left.IncompatiblePatches) != len(right.IncompatiblePatches) {
		return false
	}

	for i := range left.AddedAllowedPrefixes {
		if left.AddedAllowedPrefixes[i] != right.AddedAllowedPrefixes[i] {
			return false
		}
	}
	for i := range left.RemovedForbiddenExactPaths {
		if left.RemovedForbiddenExactPaths[i] != right.RemovedForbiddenExactPaths[i] {
			return false
		}
	}
	for i := range left.RemovedForbiddenMetadataKeys {
		if left.RemovedForbiddenMetadataKeys[i] != right.RemovedForbiddenMetadataKeys[i] {
			return false
		}
	}
	for i := range left.RemovedForbiddenContainerFields {
		if left.RemovedForbiddenContainerFields[i] != right.RemovedForbiddenContainerFields[i] {
			return false
		}
	}
	for i := range left.IncompatiblePatches {
		if left.IncompatiblePatches[i] != right.IncompatiblePatches[i] {
			return false
		}
	}

	return true
}

func normalizedGateApprovalImpact(in GateApprovalImpact) GateApprovalImpact {
	in.AddedAllowedPrefixes = cloneKindPaths(in.AddedAllowedPrefixes)
	sort.Slice(in.AddedAllowedPrefixes, func(i, j int) bool {
		if in.AddedAllowedPrefixes[i].Kind != in.AddedAllowedPrefixes[j].Kind {
			return in.AddedAllowedPrefixes[i].Kind < in.AddedAllowedPrefixes[j].Kind
		}
		return in.AddedAllowedPrefixes[i].Path < in.AddedAllowedPrefixes[j].Path
	})
	in.RemovedForbiddenExactPaths = cloneStrings(in.RemovedForbiddenExactPaths)
	sort.Strings(in.RemovedForbiddenExactPaths)
	in.RemovedForbiddenMetadataKeys = cloneStrings(in.RemovedForbiddenMetadataKeys)
	sort.Strings(in.RemovedForbiddenMetadataKeys)
	in.RemovedForbiddenContainerFields = cloneStrings(in.RemovedForbiddenContainerFields)
	sort.Strings(in.RemovedForbiddenContainerFields)
	in.IncompatiblePatches = clonePatchRefs(in.IncompatiblePatches)
	sort.Slice(in.IncompatiblePatches, func(i, j int) bool {
		if in.IncompatiblePatches[i].Component != in.IncompatiblePatches[j].Component {
			return in.IncompatiblePatches[i].Component < in.IncompatiblePatches[j].Component
		}
		return in.IncompatiblePatches[i].RelativePath < in.IncompatiblePatches[j].RelativePath
	})
	return in
}

func cloneStrings(in []string) []string {
	if len(in) == 0 {
		return nil
	}
	out := make([]string, len(in))
	copy(out, in)
	return out
}

func cloneKindPaths(in []ownership.KindPath) []ownership.KindPath {
	if len(in) == 0 {
		return nil
	}
	out := make([]ownership.KindPath, len(in))
	copy(out, in)
	return out
}

func clonePatchRefs(in []GateApprovalPatchRef) []GateApprovalPatchRef {
	if len(in) == 0 {
		return nil
	}
	out := make([]GateApprovalPatchRef, len(in))
	copy(out, in)
	return out
}
