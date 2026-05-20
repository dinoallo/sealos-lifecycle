// Copyright 2026 sealos.
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

package controller

import (
	"context"
	"errors"
	"testing"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	ctrl "sigs.k8s.io/controller-runtime/pkg/reconcile"

	"github.com/labring/sealos/pkg/distribution/agent"
	"github.com/labring/sealos/pkg/distribution/reconcile"
)

func TestReconcilerUpdatesReadyStatus(t *testing.T) {
	t.Parallel()

	scheme := runtime.NewScheme()
	if err := AddToScheme(scheme); err != nil {
		t.Fatalf("AddToScheme() error = %v", err)
	}
	target := &DistributionTarget{
		ObjectMeta: metav1.ObjectMeta{
			Name:       "platform",
			Namespace:  "sealos-system",
			Generation: 3,
		},
		Spec: DistributionTargetSpec{
			ClusterName:      "cluster-a",
			BOMPath:          "bom.yaml",
			RequeueAfter:     &metav1.Duration{Duration: 5 * time.Second},
			RolloutBatchSize: 2,
		},
	}
	cl := fake.NewClientBuilder().
		WithScheme(scheme).
		WithStatusSubresource(&DistributionTarget{}).
		WithObjects(target).
		Build()
	runner := &recordingRunner{
		result: &agent.Result{
			ClusterName:        "cluster-a",
			BOMName:            "default-platform",
			Revision:           "rev-1",
			Channel:            "beta",
			BundlePath:         "/var/lib/sealos/data/default/run/default/distribution/current",
			DesiredStateDigest: "sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
			AppliedRevision:    "/var/lib/sealos/data/default/run/default/distribution/applied-revision.yaml",
		},
	}

	result, err := (&Reconciler{
		Client: cl,
		Runner: runner,
	}).Reconcile(context.Background(), ctrl.Request{NamespacedName: client.ObjectKeyFromObject(target)})
	if err != nil {
		t.Fatalf("Reconcile() error = %v", err)
	}
	if got, want := result.RequeueAfter, 5*time.Second; got != want {
		t.Fatalf("RequeueAfter = %s, want %s", got, want)
	}
	if len(runner.calls) != 1 {
		t.Fatalf("runner calls = %d, want 1", len(runner.calls))
	}
	if got, want := runner.calls[0].ApplyOptions.Rollout.BatchSize, 2; got != want {
		t.Fatalf("rollout batch size = %d, want %d", got, want)
	}

	var updated DistributionTarget
	if err := cl.Get(context.Background(), types.NamespacedName{Name: target.Name, Namespace: target.Namespace}, &updated); err != nil {
		t.Fatalf("Get(updated) error = %v", err)
	}
	if got, want := updated.Status.ObservedGeneration, int64(3); got != want {
		t.Fatalf("ObservedGeneration = %d, want %d", got, want)
	}
	if updated.Status.LastResult == nil {
		t.Fatal("LastResult = nil, want value")
	}
	if got, want := updated.Status.LastResult.Revision, "rev-1"; got != want {
		t.Fatalf("LastResult.Revision = %q, want %q", got, want)
	}
	assertCondition(t, updated.Status.Conditions, DistributionTargetConditionReady, metav1.ConditionTrue, DistributionTargetReasonReconcileSucceeded)
	assertCondition(t, updated.Status.Conditions, DistributionTargetConditionDegraded, metav1.ConditionFalse, DistributionTargetReasonReconcileSucceeded)
}

func TestReconcilerUsesReferencedRolloutPolicy(t *testing.T) {
	t.Parallel()

	scheme := runtime.NewScheme()
	if err := AddToScheme(scheme); err != nil {
		t.Fatalf("AddToScheme() error = %v", err)
	}
	target := &DistributionTarget{
		ObjectMeta: metav1.ObjectMeta{
			Name:       "platform",
			Namespace:  "sealos-system",
			Generation: 2,
		},
		Spec: DistributionTargetSpec{
			ClusterName:      "cluster-a",
			BOMPath:          "bom.yaml",
			RolloutBatchSize: 5,
			RolloutPolicyRef: &DistributionPolicyRef{Name: "steady"},
		},
	}
	policy := &DistributionRolloutPolicy{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "steady",
			Namespace: "sealos-system",
		},
		Spec: DistributionRolloutPolicySpec{
			Strategy: reconcile.RolloutStrategy{
				BatchSize:     2,
				Canary:        reconcile.RolloutCanary{BatchSize: 1},
				Pause:         reconcile.RolloutPause{AfterCanary: true},
				HealthGate:    true,
				FailureAction: reconcile.RolloutFailureActionRollback,
			},
		},
	}
	cl := fake.NewClientBuilder().
		WithScheme(scheme).
		WithStatusSubresource(&DistributionTarget{}).
		WithObjects(target, policy).
		Build()
	runner := &recordingRunner{}

	if _, err := (&Reconciler{
		Client: cl,
		Runner: runner,
	}).Reconcile(context.Background(), ctrl.Request{NamespacedName: client.ObjectKeyFromObject(target)}); err != nil {
		t.Fatalf("Reconcile() error = %v", err)
	}
	if len(runner.calls) != 1 {
		t.Fatalf("runner calls = %d, want 1", len(runner.calls))
	}
	if got, want := runner.calls[0].ApplyOptions.Rollout.BatchSize, 2; got != want {
		t.Fatalf("rollout batch size = %d, want policy value %d", got, want)
	}
	if !runner.calls[0].ApplyOptions.Rollout.HealthGate {
		t.Fatal("rollout health gate = false, want policy value true")
	}
	if got, want := runner.calls[0].ApplyOptions.Rollout.Canary.BatchSize, 1; got != want {
		t.Fatalf("rollout canary batch size = %d, want policy value %d", got, want)
	}
	if !runner.calls[0].ApplyOptions.Rollout.Pause.AfterCanary {
		t.Fatal("rollout pause after canary = false, want policy value true")
	}
	if got, want := runner.calls[0].ApplyOptions.Rollout.FailureAction, reconcile.RolloutFailureActionRollback; got != want {
		t.Fatalf("rollout failure action = %q, want %q", got, want)
	}
}

func TestReconcilerMarksDegradedWhenRolloutPolicyMissing(t *testing.T) {
	t.Parallel()

	scheme := runtime.NewScheme()
	if err := AddToScheme(scheme); err != nil {
		t.Fatalf("AddToScheme() error = %v", err)
	}
	target := &DistributionTarget{
		ObjectMeta: metav1.ObjectMeta{
			Name:       "platform",
			Namespace:  "sealos-system",
			Generation: 1,
		},
		Spec: DistributionTargetSpec{
			ClusterName:      "cluster-a",
			BOMPath:          "bom.yaml",
			RolloutPolicyRef: &DistributionPolicyRef{Name: "missing"},
		},
	}
	cl := fake.NewClientBuilder().
		WithScheme(scheme).
		WithStatusSubresource(&DistributionTarget{}).
		WithObjects(target).
		Build()
	runner := &recordingRunner{}

	_, err := (&Reconciler{
		Client: cl,
		Runner: runner,
	}).Reconcile(context.Background(), ctrl.Request{NamespacedName: client.ObjectKeyFromObject(target)})
	if err == nil {
		t.Fatal("Reconcile() error = nil, want missing policy error")
	}
	if len(runner.calls) != 0 {
		t.Fatalf("runner calls = %d, want 0", len(runner.calls))
	}

	var updated DistributionTarget
	if err := cl.Get(context.Background(), types.NamespacedName{Name: target.Name, Namespace: target.Namespace}, &updated); err != nil {
		t.Fatalf("Get(updated) error = %v", err)
	}
	assertCondition(t, updated.Status.Conditions, DistributionTargetConditionReady, metav1.ConditionFalse, DistributionTargetReasonReconcileFailed)
	assertCondition(t, updated.Status.Conditions, DistributionTargetConditionDegraded, metav1.ConditionTrue, DistributionTargetReasonReconcileFailed)
}

func TestReconcilerUpdatesDegradedStatusOnRunnerError(t *testing.T) {
	t.Parallel()

	scheme := runtime.NewScheme()
	if err := AddToScheme(scheme); err != nil {
		t.Fatalf("AddToScheme() error = %v", err)
	}
	target := &DistributionTarget{
		ObjectMeta: metav1.ObjectMeta{
			Name:       "platform",
			Namespace:  "sealos-system",
			Generation: 1,
		},
		Spec: DistributionTargetSpec{
			ClusterName: "cluster-a",
			BOMPath:     "bom.yaml",
		},
	}
	cl := fake.NewClientBuilder().
		WithScheme(scheme).
		WithStatusSubresource(&DistributionTarget{}).
		WithObjects(target).
		Build()
	wantErr := errors.New("apply failed")

	_, err := (&Reconciler{
		Client: cl,
		Runner: &recordingRunner{err: wantErr},
	}).Reconcile(context.Background(), ctrl.Request{NamespacedName: client.ObjectKeyFromObject(target)})
	if !errors.Is(err, wantErr) {
		t.Fatalf("Reconcile() error = %v, want %v", err, wantErr)
	}

	var updated DistributionTarget
	if err := cl.Get(context.Background(), types.NamespacedName{Name: target.Name, Namespace: target.Namespace}, &updated); err != nil {
		t.Fatalf("Get(updated) error = %v", err)
	}
	assertCondition(t, updated.Status.Conditions, DistributionTargetConditionReady, metav1.ConditionFalse, DistributionTargetReasonReconcileFailed)
	assertCondition(t, updated.Status.Conditions, DistributionTargetConditionDegraded, metav1.ConditionTrue, DistributionTargetReasonReconcileFailed)
}

func TestReconcilerMarksPausedRolloutWithoutDegraded(t *testing.T) {
	t.Parallel()

	scheme := runtime.NewScheme()
	if err := AddToScheme(scheme); err != nil {
		t.Fatalf("AddToScheme() error = %v", err)
	}
	target := &DistributionTarget{
		ObjectMeta: metav1.ObjectMeta{
			Name:       "platform",
			Namespace:  "sealos-system",
			Generation: 1,
		},
		Spec: DistributionTargetSpec{
			ClusterName:  "cluster-a",
			BOMPath:      "bom.yaml",
			RequeueAfter: &metav1.Duration{Duration: time.Minute},
		},
	}
	cl := fake.NewClientBuilder().
		WithScheme(scheme).
		WithStatusSubresource(&DistributionTarget{}).
		WithObjects(target).
		Build()

	result, err := (&Reconciler{
		Client: cl,
		Runner: &recordingRunner{err: reconcile.NewRolloutPausedError("rollout paused after canary batch")},
	}).Reconcile(context.Background(), ctrl.Request{NamespacedName: client.ObjectKeyFromObject(target)})
	if err != nil {
		t.Fatalf("Reconcile() error = %v, want nil paused result", err)
	}
	if !result.IsZero() {
		t.Fatalf("result = %#v, want zero", result)
	}

	var updated DistributionTarget
	if err := cl.Get(context.Background(), types.NamespacedName{Name: target.Name, Namespace: target.Namespace}, &updated); err != nil {
		t.Fatalf("Get(updated) error = %v", err)
	}
	assertCondition(t, updated.Status.Conditions, DistributionTargetConditionReady, metav1.ConditionFalse, DistributionTargetReasonRolloutPaused)
	assertCondition(t, updated.Status.Conditions, DistributionTargetConditionDegraded, metav1.ConditionFalse, DistributionTargetReasonRolloutPaused)
}

func TestReconcilerMarksRolledBackRolloutWithoutDegraded(t *testing.T) {
	t.Parallel()

	scheme := runtime.NewScheme()
	if err := AddToScheme(scheme); err != nil {
		t.Fatalf("AddToScheme() error = %v", err)
	}
	target := &DistributionTarget{
		ObjectMeta: metav1.ObjectMeta{
			Name:       "platform",
			Namespace:  "sealos-system",
			Generation: 1,
		},
		Spec: DistributionTargetSpec{
			ClusterName:  "cluster-a",
			BOMPath:      "bom.yaml",
			RequeueAfter: &metav1.Duration{Duration: time.Minute},
		},
	}
	cl := fake.NewClientBuilder().
		WithScheme(scheme).
		WithStatusSubresource(&DistributionTarget{}).
		WithObjects(target).
		Build()

	result, err := (&Reconciler{
		Client: cl,
		Runner: &recordingRunner{err: reconcile.NewRolloutRolledBackError(errors.New("apply failed"))},
	}).Reconcile(context.Background(), ctrl.Request{NamespacedName: client.ObjectKeyFromObject(target)})
	if err != nil {
		t.Fatalf("Reconcile() error = %v, want nil rolled-back result", err)
	}
	if !result.IsZero() {
		t.Fatalf("result = %#v, want zero", result)
	}

	var updated DistributionTarget
	if err := cl.Get(context.Background(), types.NamespacedName{Name: target.Name, Namespace: target.Namespace}, &updated); err != nil {
		t.Fatalf("Get(updated) error = %v", err)
	}
	assertCondition(t, updated.Status.Conditions, DistributionTargetConditionReady, metav1.ConditionFalse, DistributionTargetReasonRolloutRolledBack)
	assertCondition(t, updated.Status.Conditions, DistributionTargetConditionDegraded, metav1.ConditionFalse, DistributionTargetReasonRolloutRolledBack)
}

func TestReconcilerIgnoresMissingTarget(t *testing.T) {
	t.Parallel()

	scheme := runtime.NewScheme()
	if err := AddToScheme(scheme); err != nil {
		t.Fatalf("AddToScheme() error = %v", err)
	}
	cl := fake.NewClientBuilder().WithScheme(scheme).Build()
	result, err := (&Reconciler{Client: cl}).Reconcile(context.Background(), ctrl.Request{
		NamespacedName: types.NamespacedName{Name: "missing", Namespace: "sealos-system"},
	})
	if err != nil {
		t.Fatalf("Reconcile() error = %v", err)
	}
	if !result.IsZero() {
		t.Fatalf("result = %#v, want zero", result)
	}
}

func TestTargetsForRolloutPolicy(t *testing.T) {
	t.Parallel()

	scheme := runtime.NewScheme()
	if err := AddToScheme(scheme); err != nil {
		t.Fatalf("AddToScheme() error = %v", err)
	}
	targets := []client.Object{
		&DistributionTarget{
			ObjectMeta: metav1.ObjectMeta{Name: "uses-policy", Namespace: "sealos-system"},
			Spec: DistributionTargetSpec{
				BOMPath:          "bom.yaml",
				RolloutPolicyRef: &DistributionPolicyRef{Name: "steady"},
			},
		},
		&DistributionTarget{
			ObjectMeta: metav1.ObjectMeta{Name: "other-policy", Namespace: "sealos-system"},
			Spec: DistributionTargetSpec{
				BOMPath:          "bom.yaml",
				RolloutPolicyRef: &DistributionPolicyRef{Name: "fast"},
			},
		},
		&DistributionTarget{
			ObjectMeta: metav1.ObjectMeta{Name: "other-namespace", Namespace: "other"},
			Spec: DistributionTargetSpec{
				BOMPath:          "bom.yaml",
				RolloutPolicyRef: &DistributionPolicyRef{Name: "steady"},
			},
		},
	}
	cl := fake.NewClientBuilder().
		WithScheme(scheme).
		WithObjects(targets...).
		Build()
	requests := (&Reconciler{Client: cl}).targetsForRolloutPolicy(context.Background(), &DistributionRolloutPolicy{
		ObjectMeta: metav1.ObjectMeta{Name: "steady", Namespace: "sealos-system"},
	})

	if got, want := len(requests), 1; got != want {
		t.Fatalf("len(requests) = %d, want %d: %#v", got, want, requests)
	}
	if got, want := requests[0].NamespacedName, (types.NamespacedName{Name: "uses-policy", Namespace: "sealos-system"}); got != want {
		t.Fatalf("request = %s, want %s", got, want)
	}
}

type recordingRunner struct {
	calls  []agent.Options
	result *agent.Result
	err    error
}

func (r *recordingRunner) Run(_ context.Context, opts agent.Options) (*agent.Result, error) {
	r.calls = append(r.calls, opts)
	return r.result, r.err
}

func assertCondition(t *testing.T, conditions []metav1.Condition, conditionType string, status metav1.ConditionStatus, reason string) {
	t.Helper()
	for _, condition := range conditions {
		if condition.Type != conditionType {
			continue
		}
		if condition.Status != status || condition.Reason != reason {
			t.Fatalf("condition %q = (%s, %s), want (%s, %s)", conditionType, condition.Status, condition.Reason, status, reason)
		}
		return
	}
	t.Fatalf("condition %q not found in %#v", conditionType, conditions)
}
