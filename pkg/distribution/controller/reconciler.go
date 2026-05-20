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
	"io"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/builder"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/manager"
	ctrl "sigs.k8s.io/controller-runtime/pkg/reconcile"

	"github.com/labring/sealos/pkg/distribution/agent"
	"github.com/labring/sealos/pkg/distribution/packageformat"
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

type Reconciler struct {
	Client   client.Client
	Runner   Runner
	Defaults Defaults
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

	result, err := r.reconcileTarget(ctx, &target)
	target.Status.ObservedGeneration = target.Generation
	now := metav1.Now()
	target.Status.LastReconcileTime = &now
	target.Status.LastResult = resultFromAgent(result)
	if err != nil {
		target.Status.Conditions = setCondition(target.Status.Conditions, metav1.Condition{
			Type:               DistributionTargetConditionReady,
			Status:             metav1.ConditionFalse,
			ObservedGeneration: target.Generation,
			LastTransitionTime: now,
			Reason:             DistributionTargetReasonReconcileFailed,
			Message:            err.Error(),
		})
		target.Status.Conditions = setCondition(target.Status.Conditions, metav1.Condition{
			Type:               DistributionTargetConditionDegraded,
			Status:             metav1.ConditionTrue,
			ObservedGeneration: target.Generation,
			LastTransitionTime: now,
			Reason:             DistributionTargetReasonReconcileFailed,
			Message:            err.Error(),
		})
		updateErr := r.Client.Status().Update(ctx, &target)
		if updateErr != nil {
			return ctrl.Result{}, updateErr
		}
		return ctrl.Result{}, err
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
	if err := r.Client.Status().Update(ctx, &target); err != nil {
		return ctrl.Result{}, err
	}
	return ctrl.Result{RequeueAfter: requeueDuration(&target)}, nil
}

func (r *Reconciler) reconcileTarget(ctx context.Context, target *DistributionTarget) (*agent.Result, error) {
	opts, err := target.Spec.AgentOptions(r.Defaults)
	if err != nil {
		return nil, err
	}
	runner := r.Runner
	if runner == nil {
		runner = agent.Runner{}
	}
	return runner.Run(ctx, opts)
}

func (r *Reconciler) SetupWithManager(mgr manager.Manager) error {
	r.Client = mgr.GetClient()
	return builder.ControllerManagedBy(mgr).For(&DistributionTarget{}).Complete(r)
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

var _ ctrl.Reconciler = (*Reconciler)(nil)
