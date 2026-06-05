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
	"testing"

	corev1 "k8s.io/api/core/v1"
)

func TestAppliedRevisionValidate(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		mutate  func(*AppliedRevision)
		wantErr bool
	}{
		{
			name: "valid",
		},
		{
			name: "invalid state",
			mutate: func(a *AppliedRevision) {
				a.Status.State = ClusterState("UnknownState")
			},
			wantErr: true,
		},
		{
			name: "invalid desired digest",
			mutate: func(a *AppliedRevision) {
				a.Spec.DesiredStateDigest = "not-a-digest"
			},
			wantErr: true,
		},
		{
			name: "duplicate condition type",
			mutate: func(a *AppliedRevision) {
				a.Status.Conditions = append(a.Status.Conditions, a.Status.Conditions[0])
			},
			wantErr: true,
		},
		{
			name: "invalid bom channel",
			mutate: func(a *AppliedRevision) {
				a.Spec.BOM.Channel = "ga"
			},
			wantErr: true,
		},
		{
			name: "requested target without resolved target",
			mutate: func(a *AppliedRevision) {
				a.Spec.RequestedTarget = &RequestedTarget{
					Kind:      TargetKindBOM,
					BOMDigest: a.Spec.BOM.Digest,
				}
			},
			wantErr: true,
		},
		{
			name: "resolved target does not match bom revision",
			mutate: func(a *AppliedRevision) {
				a.Spec.RequestedTarget = &RequestedTarget{
					Kind:      TargetKindBOM,
					BOMDigest: a.Spec.BOM.Digest,
				}
				resolved := a.Spec.BOM
				resolved.Revision = "rev-other"
				a.Spec.ResolvedTarget = &ResolvedTarget{BOM: resolved}
			},
			wantErr: true,
		},
		{
			name: "invalid release channel lookup target",
			mutate: func(a *AppliedRevision) {
				a.Spec.RequestedTarget = &RequestedTarget{
					Kind:             TargetKindReleaseChannelLookup,
					DistributionLine: a.Spec.BOM.Name,
					Channel:          a.Spec.BOM.Channel,
				}
				a.Spec.ResolvedTarget = &ResolvedTarget{
					BOM: a.Spec.BOM,
					ReleaseChannel: &ReleaseChannelReference{
						DistributionLine: a.Spec.BOM.Name,
						Channel:          a.Spec.BOM.Channel,
						TargetRevision:   a.Spec.BOM.Revision,
					},
				}
			},
			wantErr: true,
		},
		{
			name: "negative observed summary",
			mutate: func(a *AppliedRevision) {
				a.Status.ObservedSummary = &ObservedSummary{Dirty: -1}
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			doc := validAppliedRevision()
			if tt.mutate != nil {
				tt.mutate(doc)
			}
			err := doc.Validate()
			if tt.wantErr && err == nil {
				t.Fatal("expected validation error, got nil")
			}
			if !tt.wantErr && err != nil {
				t.Fatalf("expected no validation error, got %v", err)
			}
		})
	}
}

func validAppliedRevision() *AppliedRevision {
	ref := BOMReference{
		Name:     "default-platform",
		Revision: "rev-20240423",
		Channel:  "beta",
		Digest:   "sha256:dddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddd",
	}

	doc := NewAppliedRevision(
		"cluster-a-current",
		"cluster-a",
		ref,
		"sha256:eeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeee",
	)
	doc.Status.State = StateClean
	doc.Status.Conditions = []Condition{
		NewCondition(ConditionTypeApplied, corev1.ConditionTrue, "ReconcileSucceeded", "desired revision applied"),
	}
	doc.Status.LastSuccessfulRevision = &RevisionSnapshot{
		BOM:                ref,
		LocalRepoRevision:  "sha256:abababababababababababababababababababababababababababababababab",
		LocalPatchRevision: "local-rev-1",
		DesiredStateDigest: "sha256:ffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffff",
	}
	return doc
}
