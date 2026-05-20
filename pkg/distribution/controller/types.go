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
	"fmt"
	"strings"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"

	"github.com/labring/sealos/pkg/distribution"
	"github.com/labring/sealos/pkg/distribution/agent"
	"github.com/labring/sealos/pkg/distribution/reconcile"
)

const (
	KindDistributionTarget = "DistributionTarget"

	DistributionTargetConditionReady    = "Ready"
	DistributionTargetConditionDegraded = "Degraded"

	DistributionTargetReasonReconcileSucceeded = "ReconcileSucceeded"
	DistributionTargetReasonReconcileFailed    = "ReconcileFailed"
)

var GroupVersion = schema.GroupVersion{Group: distribution.GroupName, Version: distribution.Version}

type DistributionTargetSpec struct {
	ClusterName             string                      `json:"clusterName,omitempty"`
	BOMPath                 string                      `json:"bomPath,omitempty"`
	DistributionChannelPath string                      `json:"distributionChannelPath,omitempty"`
	LocalRepoPath           string                      `json:"localRepoPath,omitempty"`
	LocalPatchRevision      string                      `json:"localPatchRevision,omitempty"`
	PackageSources          []DistributionPackageSource `json:"packageSources,omitempty"`
	CacheRoot               string                      `json:"cacheRoot,omitempty"`
	KubeconfigPath          string                      `json:"kubeconfigPath,omitempty"`
	HostRoot                string                      `json:"hostRoot,omitempty"`
	RolloutBatchSize        int                         `json:"rolloutBatchSize,omitempty"`
	RequeueAfter            *metav1.Duration            `json:"requeueAfter,omitempty"`
}

type DistributionPackageSource struct {
	Component string `json:"component"`
	Root      string `json:"root"`
}

type DistributionTargetStatus struct {
	ObservedGeneration int64                        `json:"observedGeneration,omitempty"`
	LastReconcileTime  *metav1.Time                 `json:"lastReconcileTime,omitempty"`
	LastResult         *DistributionReconcileResult `json:"lastResult,omitempty"`
	Conditions         []metav1.Condition           `json:"conditions,omitempty"`
}

type DistributionReconcileResult struct {
	ClusterName        string `json:"clusterName,omitempty"`
	BOMName            string `json:"bomName,omitempty"`
	Revision           string `json:"revision,omitempty"`
	Channel            string `json:"channel,omitempty"`
	BundlePath         string `json:"bundlePath,omitempty"`
	DesiredStateDigest string `json:"desiredStateDigest,omitempty"`
	AppliedRevision    string `json:"appliedRevisionPath,omitempty"`
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status

type DistributionTarget struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   DistributionTargetSpec   `json:"spec,omitempty"`
	Status DistributionTargetStatus `json:"status,omitempty"`
}

// +kubebuilder:object:root=true

type DistributionTargetList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []DistributionTarget `json:"items"`
}

func AddToScheme(scheme *runtime.Scheme) error {
	scheme.AddKnownTypes(GroupVersion, &DistributionTarget{}, &DistributionTargetList{})
	metav1.AddToGroupVersion(scheme, GroupVersion)
	return nil
}

func (d *DistributionTarget) DeepCopyObject() runtime.Object {
	if d == nil {
		return nil
	}
	out := new(DistributionTarget)
	d.DeepCopyInto(out)
	return out
}

func (d *DistributionTarget) DeepCopyInto(out *DistributionTarget) {
	*out = *d
	out.TypeMeta = d.TypeMeta
	d.ObjectMeta.DeepCopyInto(&out.ObjectMeta)
	d.Spec.DeepCopyInto(&out.Spec)
	d.Status.DeepCopyInto(&out.Status)
}

func (d *DistributionTarget) DeepCopy() *DistributionTarget {
	if d == nil {
		return nil
	}
	out := new(DistributionTarget)
	d.DeepCopyInto(out)
	return out
}

func (l *DistributionTargetList) DeepCopyObject() runtime.Object {
	if l == nil {
		return nil
	}
	out := new(DistributionTargetList)
	l.DeepCopyInto(out)
	return out
}

func (l *DistributionTargetList) DeepCopyInto(out *DistributionTargetList) {
	*out = *l
	out.TypeMeta = l.TypeMeta
	l.ListMeta.DeepCopyInto(&out.ListMeta)
	if l.Items != nil {
		out.Items = make([]DistributionTarget, len(l.Items))
		for i := range l.Items {
			l.Items[i].DeepCopyInto(&out.Items[i])
		}
	}
}

func (l *DistributionTargetList) DeepCopy() *DistributionTargetList {
	if l == nil {
		return nil
	}
	out := new(DistributionTargetList)
	l.DeepCopyInto(out)
	return out
}

func (s *DistributionTargetSpec) DeepCopyInto(out *DistributionTargetSpec) {
	*out = *s
	if s.PackageSources != nil {
		out.PackageSources = make([]DistributionPackageSource, len(s.PackageSources))
		copy(out.PackageSources, s.PackageSources)
	}
	if s.RequeueAfter != nil {
		requeueAfter := *s.RequeueAfter
		out.RequeueAfter = &requeueAfter
	}
}

func (s *DistributionTargetStatus) DeepCopyInto(out *DistributionTargetStatus) {
	*out = *s
	if s.LastReconcileTime != nil {
		lastReconcileTime := *s.LastReconcileTime
		out.LastReconcileTime = &lastReconcileTime
	}
	if s.LastResult != nil {
		lastResult := *s.LastResult
		out.LastResult = &lastResult
	}
	if s.Conditions != nil {
		out.Conditions = make([]metav1.Condition, len(s.Conditions))
		copy(out.Conditions, s.Conditions)
	}
}

func (s DistributionTargetSpec) Validate() error {
	if strings.TrimSpace(s.BOMPath) == "" && strings.TrimSpace(s.DistributionChannelPath) == "" {
		return fmt.Errorf("one of spec.bomPath or spec.distributionChannelPath is required")
	}
	if strings.TrimSpace(s.BOMPath) != "" && strings.TrimSpace(s.DistributionChannelPath) != "" {
		return fmt.Errorf("use either spec.bomPath or spec.distributionChannelPath, not both")
	}
	if s.RolloutBatchSize < 0 {
		return fmt.Errorf("spec.rolloutBatchSize cannot be negative")
	}
	if s.RequeueAfter != nil && s.RequeueAfter.Duration < 0 {
		return fmt.Errorf("spec.requeueAfter cannot be negative")
	}
	seen := make(map[string]struct{}, len(s.PackageSources))
	for i, source := range s.PackageSources {
		component := strings.TrimSpace(source.Component)
		root := strings.TrimSpace(source.Root)
		if component == "" {
			return fmt.Errorf("spec.packageSources[%d].component cannot be empty", i)
		}
		if root == "" {
			return fmt.Errorf("spec.packageSources[%d].root cannot be empty", i)
		}
		if _, ok := seen[component]; ok {
			return fmt.Errorf("spec.packageSources[%d].component duplicates %q", i, component)
		}
		seen[component] = struct{}{}
	}
	return nil
}

func (s DistributionTargetSpec) AgentOptions(defaults Defaults) (agent.Options, error) {
	if err := s.Validate(); err != nil {
		return agent.Options{}, err
	}
	clusterName := strings.TrimSpace(s.ClusterName)
	if clusterName == "" {
		clusterName = strings.TrimSpace(defaults.ClusterName)
	}
	if clusterName == "" {
		clusterName = "default"
	}

	packageSources := make([]agent.PackageSource, 0, len(s.PackageSources))
	for _, source := range s.PackageSources {
		packageSources = append(packageSources, agent.PackageSource{
			Component: strings.TrimSpace(source.Component),
			Root:      strings.TrimSpace(source.Root),
		})
	}

	kubeconfigPath := strings.TrimSpace(s.KubeconfigPath)
	if kubeconfigPath == "" {
		kubeconfigPath = strings.TrimSpace(defaults.KubeconfigPath)
	}
	if kubeconfigPath == "" {
		kubeconfigPath = "/etc/kubernetes/admin.conf"
	}
	hostRoot := strings.TrimSpace(s.HostRoot)
	if hostRoot == "" {
		hostRoot = strings.TrimSpace(defaults.HostRoot)
	}
	if hostRoot == "" {
		hostRoot = "/"
	}

	return agent.Options{
		ClusterName:        clusterName,
		Target:             agent.TargetOptions{BOMPath: strings.TrimSpace(s.BOMPath), DistributionChannelPath: strings.TrimSpace(s.DistributionChannelPath)},
		LocalRepoPath:      strings.TrimSpace(s.LocalRepoPath),
		LocalPatchRevision: strings.TrimSpace(s.LocalPatchRevision),
		PackageSources:     packageSources,
		CacheRoot:          strings.TrimSpace(s.CacheRoot),
		Mounter:            defaults.Mounter,
		ApplyOptions: reconcile.ApplyOptions{
			KubeconfigPath: kubeconfigPath,
			HostRoot:       hostRoot,
			Stderr:         defaults.Stderr,
			Rollout: reconcile.RolloutStrategy{
				BatchSize: s.RolloutBatchSize,
			},
		},
		Once: true,
		Out:  defaults.Stderr,
	}, nil
}

func resultFromAgent(result *agent.Result) *DistributionReconcileResult {
	if result == nil {
		return nil
	}
	return &DistributionReconcileResult{
		ClusterName:        result.ClusterName,
		BOMName:            result.BOMName,
		Revision:           result.Revision,
		Channel:            result.Channel,
		BundlePath:         result.BundlePath,
		DesiredStateDigest: result.DesiredStateDigest,
		AppliedRevision:    result.AppliedRevision,
	}
}

func requeueDuration(target *DistributionTarget) time.Duration {
	if target == nil || target.Spec.RequeueAfter == nil {
		return 0
	}
	return target.Spec.RequeueAfter.Duration
}
