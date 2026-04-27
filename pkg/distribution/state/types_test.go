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
		NewCondition("Applied", corev1.ConditionTrue, "ReconcileSucceeded", "desired revision applied"),
	}
	doc.Status.LastSuccessfulRevision = &RevisionSnapshot{
		BOM:                ref,
		LocalPatchRevision: "local-rev-1",
		DesiredStateDigest: "sha256:ffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffff",
	}
	return doc
}
