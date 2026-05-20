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
	"testing"
	"time"

	"github.com/labring/sealos/pkg/distribution/localrepo"
	"github.com/labring/sealos/pkg/distribution/ownership"
)

func TestEvaluateGate(t *testing.T) {
	previousNow := gateApprovalNow
	gateApprovalNow = func() time.Time {
		return time.Date(2026, 5, 11, 0, 0, 0, 0, time.UTC)
	}
	t.Cleanup(func() {
		gateApprovalNow = previousNow
	})

	report := &Report{
		Old: PolicyRef{
			Name:   "old-policy",
			Scope:  ownership.LocalPatchPolicyScopeClusterLocal,
			Digest: "sha256:1111111111111111111111111111111111111111111111111111111111111111",
		},
		New: PolicyRef{
			Name:   "new-policy",
			Scope:  ownership.LocalPatchPolicyScopeClusterLocal,
			Digest: "sha256:2222222222222222222222222222222222222222222222222222222222222222",
		},
		HasWideningChanges:  true,
		HasNarrowingChanges: true,
		Impact: ownership.PolicyImpact{
			AddedAllowedPrefixes: []ownership.KindPath{
				{Kind: "ConfigMap", Path: "metadata.annotations"},
			},
		},
		Compatibility: localrepo.PolicyCompatibilityReport{
			ValidPatchCount: 1,
			InvalidPatches: []localrepo.PolicyCompatibilityIssue{
				{Component: "grafana", RelativePath: "patches/grafana/grafana-image.patch.yaml"},
			},
		},
	}

	t.Run("strict default config blocks widening and incompatible patches", func(t *testing.T) {
		result, err := EvaluateGate(report, DefaultGateConfig())
		if err != nil {
			t.Fatalf("EvaluateGate() error = %v", err)
		}
		if result.Passed {
			t.Fatal("result.Passed = true, want false")
		}
		if result.ApprovalSummary.ApprovalProvided {
			t.Fatal("result.ApprovalSummary.ApprovalProvided = true, want false")
		}
		if result.ApprovalSummary.ApprovalApplied {
			t.Fatal("result.ApprovalSummary.ApprovalApplied = true, want false")
		}
		if got, want := len(result.Violations), 2; got != want {
			t.Fatalf("len(result.Violations) = %d, want %d", got, want)
		}
		if got, want := result.Violations[0].Code, GateViolationWidening; got != want {
			t.Fatalf("result.Violations[0].Code = %q, want %q", got, want)
		}
		if got, want := result.Violations[1].Code, GateViolationIncompatiblePatches; got != want {
			t.Fatalf("result.Violations[1].Code = %q, want %q", got, want)
		}
		if got, want := len(result.Warnings), 1; got != want {
			t.Fatalf("len(result.Warnings) = %d, want %d", got, want)
		}
	})

	t.Run("explicit allowances keep narrowing as warning only", func(t *testing.T) {
		result, err := EvaluateGate(report, GateConfig{
			FailOnWidening:            false,
			FailOnIncompatiblePatches: false,
		})
		if err != nil {
			t.Fatalf("EvaluateGate() error = %v", err)
		}
		if !result.Passed {
			t.Fatal("result.Passed = false, want true")
		}
		if got, want := len(result.Violations), 0; got != want {
			t.Fatalf("len(result.Violations) = %d, want %d", got, want)
		}
		if got, want := len(result.Warnings), 1; got != want {
			t.Fatalf("len(result.Warnings) = %d, want %d", got, want)
		}
	})

	t.Run("approval document allows auditable exception", func(t *testing.T) {
		approval := &GateApprovalDocument{
			APIVersion: GateApprovalAPIVersion,
			Kind:       GateApprovalKind,
			Metadata: GateApprovalMeta{
				Name: "example-approval",
			},
			Spec: GateApprovalSpec{
				Owner:      "platform-sre",
				ApprovedBy: "reviewer@example.com",
				ChangeRef:  "OPS-1234",
				ExpiresAt:  "2026-12-31T00:00:00Z",
				OldPolicy:  report.Old,
				NewPolicy:  report.New,
				Approvals: []GateApprovalEntry{
					{
						Code:          GateViolationWidening,
						ExpectedCount: 1,
						ExpectedImpact: GateApprovalImpact{
							AddedAllowedPrefixes: []ownership.KindPath{
								{Kind: "ConfigMap", Path: "metadata.annotations"},
							},
						},
						Reason: "cluster-local annotations are intentionally being opened up",
					},
					{
						Code:          GateViolationIncompatiblePatches,
						ExpectedCount: 1,
						ExpectedImpact: GateApprovalImpact{
							IncompatiblePatches: []GateApprovalPatchRef{
								{Component: "grafana", RelativePath: "patches/grafana/grafana-image.patch.yaml"},
							},
						},
						Reason: "existing patches will be migrated in the same change window",
					},
				},
			},
		}

		result, err := EvaluateGateWithApproval(report, DefaultGateConfig(), approval)
		if err != nil {
			t.Fatalf("EvaluateGateWithApproval() error = %v", err)
		}
		if !result.Passed {
			t.Fatal("result.Passed = false, want true")
		}
		if !result.ApprovalSummary.ApprovalProvided {
			t.Fatal("result.ApprovalSummary.ApprovalProvided = false, want true")
		}
		if !result.ApprovalSummary.ApprovalApplied {
			t.Fatal("result.ApprovalSummary.ApprovalApplied = false, want true")
		}
		if got, want := result.ApprovalSummary.Owner, "platform-sre"; got != want {
			t.Fatalf("result.ApprovalSummary.Owner = %q, want %q", got, want)
		}
		if got, want := result.ApprovalSummary.ApprovedBy, "reviewer@example.com"; got != want {
			t.Fatalf("result.ApprovalSummary.ApprovedBy = %q, want %q", got, want)
		}
		if got, want := result.ApprovalSummary.ChangeRef, "OPS-1234"; got != want {
			t.Fatalf("result.ApprovalSummary.ChangeRef = %q, want %q", got, want)
		}
		if got, want := result.ApprovalSummary.ExpiresAt, "2026-12-31T00:00:00Z"; got != want {
			t.Fatalf("result.ApprovalSummary.ExpiresAt = %q, want %q", got, want)
		}
		if result.ApprovalSummary.ExpiresSoon {
			t.Fatal("result.ApprovalSummary.ExpiresSoon = true, want false")
		}
		if got, want := result.ApprovalSummary.FollowUpAction, GateApprovalFollowUpRemoveAfterChangeRefResolved; got != want {
			t.Fatalf("result.ApprovalSummary.FollowUpAction = %q, want %q", got, want)
		}
		if got, want := len(result.ApprovalSummary.ApprovedViolationCodes), 2; got != want {
			t.Fatalf("len(result.ApprovalSummary.ApprovedViolationCodes) = %d, want %d", got, want)
		}
		if got, want := result.ApprovalSummary.ApprovedViolationCodes[0], GateViolationIncompatiblePatches; got != want {
			t.Fatalf("result.ApprovalSummary.ApprovedViolationCodes[0] = %q, want %q", got, want)
		}
		if got, want := result.ApprovalSummary.ApprovedViolationCodes[1], GateViolationWidening; got != want {
			t.Fatalf("result.ApprovalSummary.ApprovedViolationCodes[1] = %q, want %q", got, want)
		}
		if got, want := len(result.Violations), 0; got != want {
			t.Fatalf("len(result.Violations) = %d, want %d", got, want)
		}
		if got, want := len(result.ApprovedViolations), 2; got != want {
			t.Fatalf("len(result.ApprovedViolations) = %d, want %d", got, want)
		}
		if got, want := result.ApprovedViolations[0].Code, GateViolationWidening; got != want {
			t.Fatalf("result.ApprovedViolations[0].Code = %q, want %q", got, want)
		}
		if got, want := result.ApprovedViolations[0].Impact.AddedAllowedPrefixes[0].Path, "metadata.annotations"; got != want {
			t.Fatalf("result.ApprovedViolations[0].Impact.AddedAllowedPrefixes[0].Path = %q, want %q", got, want)
		}
		if got, want := result.ApprovedViolations[1].Code, GateViolationIncompatiblePatches; got != want {
			t.Fatalf("result.ApprovedViolations[1].Code = %q, want %q", got, want)
		}
		if got, want := result.ApprovedViolations[1].Impact.IncompatiblePatches[0].RelativePath, "patches/grafana/grafana-image.patch.yaml"; got != want {
			t.Fatalf("result.ApprovedViolations[1].Impact.IncompatiblePatches[0].RelativePath = %q, want %q", got, want)
		}
		if got, want := len(result.Warnings), 4; got != want {
			t.Fatalf("len(result.Warnings) = %d, want %d", got, want)
		}
		if got, want := result.Warnings[2], "candidate LocalPatchPolicy narrows previously allowed surface; review operator continuity impact"; got != want {
			t.Fatalf("result.Warnings[2] = %q, want %q", got, want)
		}
		if got, want := result.Warnings[3], `approval file was used to pass the gate; remove it after changeRef "OPS-1234" is resolved`; got != want {
			t.Fatalf("result.Warnings[3] = %q, want %q", got, want)
		}
	})

	t.Run("approval document must match compared policy identity", func(t *testing.T) {
		approval := &GateApprovalDocument{
			APIVersion: GateApprovalAPIVersion,
			Kind:       GateApprovalKind,
			Metadata: GateApprovalMeta{
				Name: "example-approval",
			},
			Spec: GateApprovalSpec{
				Owner:      "platform-sre",
				ApprovedBy: "reviewer@example.com",
				ChangeRef:  "OPS-1234",
				ExpiresAt:  "2026-12-31T00:00:00Z",
				OldPolicy: PolicyRef{
					Name:   report.Old.Name,
					Scope:  report.Old.Scope,
					Digest: "sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
				},
				NewPolicy: report.New,
				Approvals: []GateApprovalEntry{
					{
						Code:          GateViolationWidening,
						ExpectedCount: 2,
						ExpectedImpact: GateApprovalImpact{
							AddedAllowedPrefixes: []ownership.KindPath{
								{Kind: "ConfigMap", Path: "metadata.annotations"},
							},
						},
						Reason: "intentionally approved",
					},
				},
			},
		}

		_, err := EvaluateGateWithApproval(report, DefaultGateConfig(), approval)
		if err == nil {
			t.Fatal("EvaluateGateWithApproval() error = nil, want identity mismatch")
		}
	})

	t.Run("approval document must match exact violation counts", func(t *testing.T) {
		approval := &GateApprovalDocument{
			APIVersion: GateApprovalAPIVersion,
			Kind:       GateApprovalKind,
			Metadata: GateApprovalMeta{
				Name: "example-approval",
			},
			Spec: GateApprovalSpec{
				Owner:      "platform-sre",
				ApprovedBy: "reviewer@example.com",
				ChangeRef:  "OPS-1234",
				ExpiresAt:  "2026-12-31T00:00:00Z",
				OldPolicy:  report.Old,
				NewPolicy:  report.New,
				Approvals: []GateApprovalEntry{
					{
						Code:          GateViolationWidening,
						ExpectedCount: 2,
						Reason:        "intentionally approved",
					},
				},
			},
		}

		_, err := EvaluateGateWithApproval(report, DefaultGateConfig(), approval)
		if err == nil {
			t.Fatal("EvaluateGateWithApproval() error = nil, want count mismatch")
		}
	})

	t.Run("expired approval document is rejected", func(t *testing.T) {
		approval := &GateApprovalDocument{
			APIVersion: GateApprovalAPIVersion,
			Kind:       GateApprovalKind,
			Metadata: GateApprovalMeta{
				Name: "example-approval",
			},
			Spec: GateApprovalSpec{
				Owner:      "platform-sre",
				ApprovedBy: "reviewer@example.com",
				ChangeRef:  "OPS-1234",
				ExpiresAt:  "2026-05-10T00:00:00Z",
				OldPolicy:  report.Old,
				NewPolicy:  report.New,
				Approvals: []GateApprovalEntry{
					{
						Code:          GateViolationWidening,
						ExpectedCount: 1,
						ExpectedImpact: GateApprovalImpact{
							AddedAllowedPrefixes: []ownership.KindPath{
								{Kind: "ConfigMap", Path: "metadata.annotations"},
							},
						},
						Reason: "intentionally approved",
					},
				},
			},
		}

		_, err := EvaluateGateWithApproval(report, DefaultGateConfig(), approval)
		if err == nil {
			t.Fatal("EvaluateGateWithApproval() error = nil, want expired approval error")
		}
		if got, want := err.Error(), "approval file expired at 2026-05-10T00:00:00Z"; got != want {
			t.Fatalf("EvaluateGateWithApproval() error = %q, want %q", got, want)
		}
	})

	t.Run("near-expiry approval document emits renewal warning", func(t *testing.T) {
		approval := &GateApprovalDocument{
			APIVersion: GateApprovalAPIVersion,
			Kind:       GateApprovalKind,
			Metadata: GateApprovalMeta{
				Name: "example-approval",
			},
			Spec: GateApprovalSpec{
				Owner:      "platform-sre",
				ApprovedBy: "reviewer@example.com",
				ChangeRef:  "OPS-1234",
				ExpiresAt:  "2026-05-20T00:00:00Z",
				OldPolicy:  report.Old,
				NewPolicy:  report.New,
				Approvals: []GateApprovalEntry{
					{
						Code:          GateViolationWidening,
						ExpectedCount: 1,
						ExpectedImpact: GateApprovalImpact{
							AddedAllowedPrefixes: []ownership.KindPath{
								{Kind: "ConfigMap", Path: "metadata.annotations"},
							},
						},
						Reason: "cluster-local annotations are intentionally being opened up",
					},
					{
						Code:          GateViolationIncompatiblePatches,
						ExpectedCount: 1,
						ExpectedImpact: GateApprovalImpact{
							IncompatiblePatches: []GateApprovalPatchRef{
								{Component: "grafana", RelativePath: "patches/grafana/grafana-image.patch.yaml"},
							},
						},
						Reason: "existing patches will be migrated in the same change window",
					},
				},
			},
		}

		result, err := EvaluateGateWithApproval(report, DefaultGateConfig(), approval)
		if err != nil {
			t.Fatalf("EvaluateGateWithApproval() error = %v", err)
		}
		if !result.ApprovalSummary.ExpiresSoon {
			t.Fatal("result.ApprovalSummary.ExpiresSoon = false, want true")
		}
		if got, want := result.ApprovalSummary.DaysUntilExpiry, 9; got != want {
			t.Fatalf("result.ApprovalSummary.DaysUntilExpiry = %d, want %d", got, want)
		}
		if got, want := result.ApprovalSummary.FollowUpAction, GateApprovalFollowUpRenewOrRemoveBeforeExpiry; got != want {
			t.Fatalf("result.ApprovalSummary.FollowUpAction = %q, want %q", got, want)
		}
		if got, want := len(result.Warnings), 5; got != want {
			t.Fatalf("len(result.Warnings) = %d, want %d", got, want)
		}
		if got, want := result.Warnings[0], "approval file expires in 9 day(s); renew or remove it before 2026-05-20T00:00:00Z"; got != want {
			t.Fatalf("result.Warnings[0] = %q, want %q", got, want)
		}
		if got, want := result.Warnings[4], `approval file was used to pass the gate; remove it after changeRef "OPS-1234" is resolved`; got != want {
			t.Fatalf("result.Warnings[4] = %q, want %q", got, want)
		}
	})

	t.Run("near-expiry approval document can be promoted to a blocking violation", func(t *testing.T) {
		approval := &GateApprovalDocument{
			APIVersion: GateApprovalAPIVersion,
			Kind:       GateApprovalKind,
			Metadata: GateApprovalMeta{
				Name: "example-approval",
			},
			Spec: GateApprovalSpec{
				Owner:      "platform-sre",
				ApprovedBy: "reviewer@example.com",
				ChangeRef:  "OPS-1234",
				ExpiresAt:  "2026-05-20T00:00:00Z",
				OldPolicy:  report.Old,
				NewPolicy:  report.New,
				Approvals: []GateApprovalEntry{
					{
						Code:          GateViolationWidening,
						ExpectedCount: 1,
						ExpectedImpact: GateApprovalImpact{
							AddedAllowedPrefixes: []ownership.KindPath{
								{Kind: "ConfigMap", Path: "metadata.annotations"},
							},
						},
						Reason: "cluster-local annotations are intentionally being opened up",
					},
					{
						Code:          GateViolationIncompatiblePatches,
						ExpectedCount: 1,
						ExpectedImpact: GateApprovalImpact{
							IncompatiblePatches: []GateApprovalPatchRef{
								{Component: "grafana", RelativePath: "patches/grafana/grafana-image.patch.yaml"},
							},
						},
						Reason: "existing patches will be migrated in the same change window",
					},
				},
			},
		}

		result, err := EvaluateGateWithApproval(report, GateConfig{
			FailOnWidening:            true,
			FailOnIncompatiblePatches: true,
			ApprovalExpiryWarningDays: 30,
			FailOnApprovalExpiresSoon: true,
		}, approval)
		if err != nil {
			t.Fatalf("EvaluateGateWithApproval() error = %v", err)
		}
		if result.Passed {
			t.Fatal("result.Passed = true, want false")
		}
		if !result.ApprovalSummary.ExpiresSoon {
			t.Fatal("result.ApprovalSummary.ExpiresSoon = false, want true")
		}
		if got, want := result.ApprovalSummary.FollowUpAction, GateApprovalFollowUpRenewOrRemoveBeforeExpiry; got != want {
			t.Fatalf("result.ApprovalSummary.FollowUpAction = %q, want %q", got, want)
		}
		if got, want := len(result.Violations), 1; got != want {
			t.Fatalf("len(result.Violations) = %d, want %d", got, want)
		}
		if got, want := result.Violations[0].Code, GateViolationApprovalExpiresSoon; got != want {
			t.Fatalf("result.Violations[0].Code = %q, want %q", got, want)
		}
		if got, want := result.Violations[0].Count, 9; got != want {
			t.Fatalf("result.Violations[0].Count = %d, want %d", got, want)
		}
	})
}
