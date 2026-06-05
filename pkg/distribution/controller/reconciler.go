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
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/labring/sealos/pkg/distribution/agent"
	"github.com/labring/sealos/pkg/distribution/packageformat"
	"github.com/labring/sealos/pkg/distribution/reconcile"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/builder"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/manager"
	ctrl "sigs.k8s.io/controller-runtime/pkg/reconcile"
)

type Defaults struct {
	ClusterName    string
	KubeconfigPath string
	HostRoot       string
	Mounter        packageformat.ImageMounter
	Stderr         io.Writer
}

type Runner interface {
	Run(context.Context, agent.Options) (*agent.Result, error)
}

type EventRecorder interface {
	Event(object runtime.Object, eventtype, reason, message string)
}

type Reconciler struct {
	Client        client.Client
	Runner        Runner
	EventRecorder EventRecorder
	Defaults      Defaults
}

func (r *Reconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	if r.Client == nil {
		return ctrl.Result{}, nil
	}

	var target DistributionTarget
	if err := r.Client.Get(ctx, req.NamespacedName, &target); err != nil {
		if apierrors.IsNotFound(err) {
			return ctrl.Result{}, nil
		}
		return ctrl.Result{}, err
	}
	if !target.ObjectMeta.DeletionTimestamp.IsZero() {
		return ctrl.Result{}, nil
	}

	target.Status.ObservedGeneration = target.Generation
	now := metav1.Now()
	target.Status.LastReconcileTime = &now
	result, err := r.reconcileTarget(ctx, &target)
	target.Status.LastResult = resultFromAgent(result)
	if err != nil {
		if reconcile.IsRolloutPaused(err) {
			r.markPaused(&target, now, err)
			updateErr := r.Client.Status().Update(ctx, &target)
			if updateErr != nil {
				return ctrl.Result{}, updateErr
			}
			r.recordEvent(&target, "Normal", DistributionTargetReasonRolloutPaused, err.Error())
			return ctrl.Result{}, nil
		}
		if reconcile.IsRolloutRolledBack(err) {
			r.markRollbackHold(&target, now, err)
			updateErr := r.Client.Status().Update(ctx, &target)
			if updateErr != nil {
				return ctrl.Result{}, updateErr
			}
			r.recordEvent(
				&target,
				"Warning",
				DistributionTargetReasonRolloutRolledBack,
				err.Error(),
			)
			return ctrl.Result{}, nil
		}
		requeue := retryBackoff(&target)
		partial := result != nil
		r.markFailed(&target, now, err, partial, requeue)
		updateErr := r.Client.Status().Update(ctx, &target)
		if updateErr != nil {
			return ctrl.Result{}, updateErr
		}
		if partial {
			r.recordEvent(&target, "Warning", DistributionTargetReasonReconcilePartial, err.Error())
		} else {
			r.recordEvent(&target, "Warning", DistributionTargetReasonReconcileFailed, err.Error())
		}
		if requeue > 0 {
			return ctrl.Result{RequeueAfter: requeue}, nil
		}
		return ctrl.Result{}, err
	}

	r.markSucceeded(&target, now)
	if err := r.Client.Status().Update(ctx, &target); err != nil {
		return ctrl.Result{}, err
	}
	r.recordEvent(
		&target,
		"Normal",
		DistributionTargetReasonReconcileSucceeded,
		"distribution target reconciled",
	)
	return ctrl.Result{RequeueAfter: requeueDuration(&target)}, nil
}

func (r *Reconciler) reconcileTarget(
	ctx context.Context,
	target *DistributionTarget,
) (*agent.Result, error) {
	if err := target.Spec.Validate(); err != nil {
		return nil, err
	}
	rollout, err := r.rolloutStrategyForTarget(ctx, target)
	if err != nil {
		return nil, err
	}
	opts, err := target.Spec.AgentOptionsWithRollout(r.Defaults, rollout)
	if err != nil {
		return nil, err
	}
	runner := r.Runner
	if runner == nil {
		runner = agent.Runner{}
	}
	return runner.Run(ctx, opts)
}

func (r *Reconciler) rolloutStrategyForTarget(
	ctx context.Context,
	target *DistributionTarget,
) (reconcile.RolloutStrategy, error) {
	if target == nil {
		return reconcile.RolloutStrategy{}, fmt.Errorf("distribution target cannot be nil")
	}
	if target.Spec.RolloutPolicyRef == nil {
		return reconcile.RolloutStrategy{BatchSize: target.Spec.RolloutBatchSize}, nil
	}
	policyName := strings.TrimSpace(target.Spec.RolloutPolicyRef.Name)
	var policy DistributionRolloutPolicy
	if err := r.Client.Get(ctx, client.ObjectKey{Namespace: target.Namespace, Name: policyName}, &policy); err != nil {
		return reconcile.RolloutStrategy{}, fmt.Errorf(
			"load rollout policy %q: %w",
			policyName,
			err,
		)
	}
	if err := policy.Spec.Validate(); err != nil {
		return reconcile.RolloutStrategy{}, fmt.Errorf(
			"validate rollout policy %q: %w",
			policyName,
			err,
		)
	}
	return policy.Spec.Strategy, nil
}

func (r *Reconciler) SetupWithManager(mgr manager.Manager) error {
	r.Client = mgr.GetClient()
	if r.EventRecorder == nil {
		r.EventRecorder = mgr.GetEventRecorderFor("sealos-distribution-controller")
	}
	return builder.ControllerManagedBy(mgr).
		For(&DistributionTarget{}).
		Watches(&DistributionRolloutPolicy{}, handler.EnqueueRequestsFromMapFunc(r.targetsForRolloutPolicy)).
		Complete(r)
}

func (r *Reconciler) targetsForRolloutPolicy(
	ctx context.Context,
	object client.Object,
) []ctrl.Request {
	if r.Client == nil || object == nil {
		return nil
	}

	var targets DistributionTargetList
	if err := r.Client.List(ctx, &targets, client.InNamespace(object.GetNamespace())); err != nil {
		return nil
	}
	requests := make([]ctrl.Request, 0, len(targets.Items))
	for _, target := range targets.Items {
		if target.Spec.RolloutPolicyRef == nil ||
			strings.TrimSpace(target.Spec.RolloutPolicyRef.Name) != object.GetName() {
			continue
		}
		requests = append(
			requests,
			ctrl.Request{NamespacedName: client.ObjectKeyFromObject(&target)},
		)
	}
	return requests
}

func setCondition(conditions []metav1.Condition, condition metav1.Condition) []metav1.Condition {
	existing := -1
	for i := range conditions {
		if conditions[i].Type == condition.Type {
			existing = i
			break
		}
	}
	if existing == -1 {
		return append(conditions, condition)
	}
	if conditions[existing].Status == condition.Status &&
		conditions[existing].Reason == condition.Reason &&
		conditions[existing].Message == condition.Message {
		condition.LastTransitionTime = conditions[existing].LastTransitionTime
	}
	conditions[existing] = condition
	return conditions
}

func (r *Reconciler) markSucceeded(target *DistributionTarget, now metav1.Time) {
	target.Status.Phase = DistributionTargetPhaseSucceeded
	target.Status.RetryCount = 0
	target.Status.NextRetryTime = nil
	target.Status.HoldReason = ""
	target.Status.LastDiagnostic = &DistributionTargetDiagnostic{
		Type:    "Normal",
		Reason:  DistributionTargetReasonReconcileSucceeded,
		Message: "distribution target reconciled",
		Time:    &now,
	}
	target.Status.Conditions = setCondition(target.Status.Conditions, metav1.Condition{
		Type:               DistributionTargetConditionReady,
		Status:             metav1.ConditionTrue,
		ObservedGeneration: target.Generation,
		LastTransitionTime: now,
		Reason:             DistributionTargetReasonReconcileSucceeded,
		Message:            "distribution target reconciled",
	})
	target.Status.Conditions = setCondition(target.Status.Conditions, metav1.Condition{
		Type:               DistributionTargetConditionDegraded,
		Status:             metav1.ConditionFalse,
		ObservedGeneration: target.Generation,
		LastTransitionTime: now,
		Reason:             DistributionTargetReasonReconcileSucceeded,
		Message:            "distribution target reconciled",
	})
}

func (r *Reconciler) markFailed(
	target *DistributionTarget,
	now metav1.Time,
	err error,
	partial bool,
	requeue time.Duration,
) {
	reason := DistributionTargetReasonReconcileFailed
	target.Status.Phase = DistributionTargetPhaseRetrying
	if partial {
		reason = DistributionTargetReasonReconcilePartial
		target.Status.Phase = DistributionTargetPhasePartiallyFailed
	}
	target.Status.RetryCount++
	if requeue > 0 {
		next := metav1.NewTime(now.Add(requeue))
		target.Status.NextRetryTime = &next
	} else {
		target.Status.NextRetryTime = nil
	}
	target.Status.HoldReason = ""
	target.Status.LastDiagnostic = &DistributionTargetDiagnostic{
		Type:    "Warning",
		Reason:  reason,
		Message: err.Error(),
		Time:    &now,
	}
	target.Status.Conditions = setCondition(target.Status.Conditions, metav1.Condition{
		Type:               DistributionTargetConditionReady,
		Status:             metav1.ConditionFalse,
		ObservedGeneration: target.Generation,
		LastTransitionTime: now,
		Reason:             reason,
		Message:            err.Error(),
	})
	target.Status.Conditions = setCondition(target.Status.Conditions, metav1.Condition{
		Type:               DistributionTargetConditionDegraded,
		Status:             metav1.ConditionTrue,
		ObservedGeneration: target.Generation,
		LastTransitionTime: now,
		Reason:             reason,
		Message:            err.Error(),
	})
}

func (r *Reconciler) markPaused(target *DistributionTarget, now metav1.Time, err error) {
	target.Status.Phase = DistributionTargetPhasePaused
	target.Status.NextRetryTime = nil
	target.Status.HoldReason = DistributionTargetReasonRolloutPaused
	target.Status.LastDiagnostic = &DistributionTargetDiagnostic{
		Type:    "Normal",
		Reason:  DistributionTargetReasonRolloutPaused,
		Message: err.Error(),
		Time:    &now,
	}
	target.Status.Conditions = setCondition(target.Status.Conditions, metav1.Condition{
		Type:               DistributionTargetConditionReady,
		Status:             metav1.ConditionFalse,
		ObservedGeneration: target.Generation,
		LastTransitionTime: now,
		Reason:             DistributionTargetReasonRolloutPaused,
		Message:            err.Error(),
	})
	target.Status.Conditions = setCondition(target.Status.Conditions, metav1.Condition{
		Type:               DistributionTargetConditionDegraded,
		Status:             metav1.ConditionFalse,
		ObservedGeneration: target.Generation,
		LastTransitionTime: now,
		Reason:             DistributionTargetReasonRolloutPaused,
		Message:            err.Error(),
	})
}

func (r *Reconciler) markRollbackHold(target *DistributionTarget, now metav1.Time, err error) {
	target.Status.Phase = DistributionTargetPhaseRollbackHold
	target.Status.NextRetryTime = nil
	target.Status.HoldReason = DistributionTargetReasonRolloutRolledBack
	target.Status.LastDiagnostic = &DistributionTargetDiagnostic{
		Type:    "Warning",
		Reason:  DistributionTargetReasonRolloutRolledBack,
		Message: err.Error(),
		Time:    &now,
	}
	target.Status.Conditions = setCondition(target.Status.Conditions, metav1.Condition{
		Type:               DistributionTargetConditionReady,
		Status:             metav1.ConditionFalse,
		ObservedGeneration: target.Generation,
		LastTransitionTime: now,
		Reason:             DistributionTargetReasonRolloutRolledBack,
		Message:            err.Error(),
	})
	target.Status.Conditions = setCondition(target.Status.Conditions, metav1.Condition{
		Type:               DistributionTargetConditionDegraded,
		Status:             metav1.ConditionFalse,
		ObservedGeneration: target.Generation,
		LastTransitionTime: now,
		Reason:             DistributionTargetReasonRolloutRolledBack,
		Message:            err.Error(),
	})
}

func (r *Reconciler) recordEvent(target *DistributionTarget, eventType, reason, message string) {
	if r.EventRecorder == nil {
		return
	}
	r.EventRecorder.Event(target, eventType, reason, message)
}

func retryBackoff(target *DistributionTarget) time.Duration {
	if target == nil || target.Spec.RetryBackoff == nil {
		return 0
	}
	return target.Spec.RetryBackoff.Duration
}

var _ ctrl.Reconciler = (*Reconciler)(nil)
