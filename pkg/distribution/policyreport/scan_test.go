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
	"os"
	"path/filepath"
	"testing"
	"time"

	"sigs.k8s.io/yaml"

	"github.com/labring/sealos/pkg/distribution/ownership"
)

func TestScanApprovals(t *testing.T) {
	previousNow := approvalScanNow
	approvalScanNow = func() time.Time {
		return time.Date(2026, 5, 12, 0, 0, 0, 0, time.UTC)
	}
	t.Cleanup(func() {
		approvalScanNow = previousNow
	})

	t.Run("reports valid near-expiry expired and invalid approvals", func(t *testing.T) {
		root := t.TempDir()
		mustWriteApprovalDocument(t, filepath.Join(root, "a", "local-patch-policy-approval-valid.yaml"), testApprovalDocument("valid", "2030-01-01T00:00:00Z"))
		mustWriteApprovalDocument(t, filepath.Join(root, "b", "local-patch-policy-approval-soon.yaml"), testApprovalDocument("soon", "2026-05-20T00:00:00Z"))
		mustWriteApprovalDocument(t, filepath.Join(root, "c", "local-patch-policy-approval-expired.yaml"), testApprovalDocument("expired", "2026-05-01T00:00:00Z"))
		mustWriteFile(t, filepath.Join(root, "d", "local-patch-policy-approval-invalid.yaml"), "apiVersion: distribution.sealos.io/v1alpha1\nkind: LocalPatchPolicyGateApproval\nmetadata:\n  name: broken\nspec:\n  owner: \"\"\n")
		mustWriteFile(t, filepath.Join(root, "ignored.txt"), "ignored")

		result, err := ScanApprovals(root, ApprovalScanConfig{
			ApprovalExpiryWarningDays: 30,
		})
		if err != nil {
			t.Fatalf("ScanApprovals() error = %v", err)
		}
		if result.Passed {
			t.Fatal("result.Passed = true, want false")
		}
		if got, want := result.Summary.Scanned, 4; got != want {
			t.Fatalf("summary.scanned = %d, want %d", got, want)
		}
		if got, want := result.Summary.Valid, 1; got != want {
			t.Fatalf("summary.valid = %d, want %d", got, want)
		}
		if got, want := result.Summary.ExpiresSoon, 1; got != want {
			t.Fatalf("summary.expiresSoon = %d, want %d", got, want)
		}
		if got, want := result.Summary.Expired, 1; got != want {
			t.Fatalf("summary.expired = %d, want %d", got, want)
		}
		if got, want := result.Summary.Invalid, 1; got != want {
			t.Fatalf("summary.invalid = %d, want %d", got, want)
		}
		if got, want := result.Summary.Blocking, 2; got != want {
			t.Fatalf("summary.blocking = %d, want %d", got, want)
		}
		if got, want := len(result.Approvals), 4; got != want {
			t.Fatalf("len(approvals) = %d, want %d", got, want)
		}
		if got, want := result.Approvals[0].Path, "a/local-patch-policy-approval-valid.yaml"; got != want {
			t.Fatalf("approvals[0].path = %q, want %q", got, want)
		}
		if got, want := result.Approvals[0].Status, ApprovalScanStatusValid; got != want {
			t.Fatalf("approvals[0].status = %q, want %q", got, want)
		}
		if got, want := result.Approvals[1].Status, ApprovalScanStatusExpiresSoon; got != want {
			t.Fatalf("approvals[1].status = %q, want %q", got, want)
		}
		if got, want := result.Approvals[1].DaysUntilExpiry, 8; got != want {
			t.Fatalf("approvals[1].daysUntilExpiry = %d, want %d", got, want)
		}
		if got, want := result.Approvals[1].FollowUpAction, GateApprovalFollowUpRenewOrRemoveBeforeExpiry; got != want {
			t.Fatalf("approvals[1].followUpAction = %q, want %q", got, want)
		}
		if result.Approvals[1].Blocking {
			t.Fatal("approvals[1].blocking = true, want false")
		}
		if got, want := result.Approvals[2].Status, ApprovalScanStatusExpired; got != want {
			t.Fatalf("approvals[2].status = %q, want %q", got, want)
		}
		if !result.Approvals[2].Blocking {
			t.Fatal("approvals[2].blocking = false, want true")
		}
		if got, want := result.Approvals[2].FollowUpAction, ApprovalScanFollowUpRemoveExpiredApproval; got != want {
			t.Fatalf("approvals[2].followUpAction = %q, want %q", got, want)
		}
		if got, want := result.Approvals[3].Status, ApprovalScanStatusInvalid; got != want {
			t.Fatalf("approvals[3].status = %q, want %q", got, want)
		}
		if got, want := result.Approvals[3].FollowUpAction, ApprovalScanFollowUpRepairInvalidDocument; got != want {
			t.Fatalf("approvals[3].followUpAction = %q, want %q", got, want)
		}
		if got, want := len(result.Warnings), 3; got != want {
			t.Fatalf("len(warnings) = %d, want %d", got, want)
		}
	})

	t.Run("can treat near-expiry approvals as blocking", func(t *testing.T) {
		root := t.TempDir()
		mustWriteApprovalDocument(t, filepath.Join(root, "local-patch-policy-approval-soon.yaml"), testApprovalDocument("soon", "2026-05-20T00:00:00Z"))

		result, err := ScanApprovals(root, ApprovalScanConfig{
			ApprovalExpiryWarningDays: 30,
			FailOnApprovalExpiresSoon: true,
		})
		if err != nil {
			t.Fatalf("ScanApprovals() error = %v", err)
		}
		if result.Passed {
			t.Fatal("result.Passed = true, want false")
		}
		if got, want := result.Summary.Blocking, 1; got != want {
			t.Fatalf("summary.blocking = %d, want %d", got, want)
		}
		if !result.Approvals[0].Blocking {
			t.Fatal("approvals[0].blocking = false, want true")
		}
		if got, want := result.Approvals[0].Status, ApprovalScanStatusExpiresSoon; got != want {
			t.Fatalf("approvals[0].status = %q, want %q", got, want)
		}
	})
}

func mustWriteApprovalDocument(t *testing.T, path string, doc GateApprovalDocument) {
	t.Helper()
	data, err := yaml.Marshal(doc)
	if err != nil {
		t.Fatalf("yaml.Marshal() error = %v", err)
	}
	mustWriteFile(t, path, string(data))
}

func mustWriteFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("MkdirAll() error = %v", err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}
}

func testApprovalDocument(name, expiresAt string) GateApprovalDocument {
	return GateApprovalDocument{
		APIVersion: GateApprovalAPIVersion,
		Kind:       GateApprovalKind,
		Metadata: GateApprovalMeta{
			Name: name,
		},
		Spec: GateApprovalSpec{
			Owner:      "platform-sre",
			ApprovedBy: "reviewer@example.com",
			ChangeRef:  "OPS-1234",
			ExpiresAt:  expiresAt,
			OldPolicy: PolicyRef{
				Name:   "custom-local-patch-policy",
				Scope:  ownership.LocalPatchPolicyScopeClusterLocal,
				Digest: "sha256:1111111111111111111111111111111111111111111111111111111111111111",
			},
			NewPolicy: PolicyRef{
				Name:   "custom-local-patch-policy",
				Scope:  ownership.LocalPatchPolicyScopeClusterLocal,
				Digest: "sha256:2222222222222222222222222222222222222222222222222222222222222222",
			},
		},
	}
}
