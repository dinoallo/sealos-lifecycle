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
	"testing"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/labring/sealos/pkg/distribution/reconcile"
)

func TestDistributionTargetSpecAgentOptions(t *testing.T) {
	t.Parallel()

	spec := DistributionTargetSpec{
		BOMPath:            "bom.yaml",
		LocalRepoPath:      "local",
		LocalPatchRevision: "patch-1",
		PackageSources: []DistributionPackageSource{
			{Component: "runtime", Root: "/packages/runtime"},
		},
		KubeconfigPath:   "/custom/admin.conf",
		HostRoot:         "/host",
		RolloutBatchSize: 2,
		RequeueAfter:     &metav1.Duration{Duration: 0},
	}

	opts, err := spec.AgentOptions(Defaults{ClusterName: "fallback"})
	if err != nil {
		t.Fatalf("AgentOptions() error = %v", err)
	}
	if got, want := opts.ClusterName, "fallback"; got != want {
		t.Fatalf("ClusterName = %q, want %q", got, want)
	}
	if got, want := opts.Target.BOMPath, "bom.yaml"; got != want {
		t.Fatalf("BOMPath = %q, want %q", got, want)
	}
	if got, want := opts.LocalRepoPath, "local"; got != want {
		t.Fatalf("LocalRepoPath = %q, want %q", got, want)
	}
	if got, want := opts.LocalPatchRevision, "patch-1"; got != want {
		t.Fatalf("LocalPatchRevision = %q, want %q", got, want)
	}
	if got, want := len(opts.PackageSources), 1; got != want {
		t.Fatalf("len(PackageSources) = %d, want %d", got, want)
	}
	if got, want := opts.PackageSources[0].Component, "runtime"; got != want {
		t.Fatalf("PackageSources[0].Component = %q, want %q", got, want)
	}
	if got, want := opts.ApplyOptions.KubeconfigPath, "/custom/admin.conf"; got != want {
		t.Fatalf("KubeconfigPath = %q, want %q", got, want)
	}
	if got, want := opts.ApplyOptions.HostRoot, "/host"; got != want {
		t.Fatalf("HostRoot = %q, want %q", got, want)
	}
	if got, want := opts.ApplyOptions.Rollout.BatchSize, 2; got != want {
		t.Fatalf("Rollout.BatchSize = %d, want %d", got, want)
	}
	if !opts.Once {
		t.Fatal("Once = false, want true")
	}
}

func TestDistributionTargetSpecValidateRejectsAmbiguousTarget(t *testing.T) {
	t.Parallel()

	err := (DistributionTargetSpec{
		BOMPath:                 "bom.yaml",
		DistributionChannelPath: "channel.yaml",
	}).Validate()
	if err == nil {
		t.Fatal("Validate() error = nil, want ambiguous target error")
	}
}

func TestDistributionTargetSpecValidateRejectsDuplicatePackageSources(t *testing.T) {
	t.Parallel()

	err := (DistributionTargetSpec{
		BOMPath: "bom.yaml",
		PackageSources: []DistributionPackageSource{
			{Component: "runtime", Root: "/one"},
			{Component: "runtime", Root: "/two"},
		},
	}).Validate()
	if err == nil {
		t.Fatal("Validate() error = nil, want duplicate package source error")
	}
}

func TestDistributionTargetSpecValidateRejectsEmptyRolloutPolicyRef(t *testing.T) {
	t.Parallel()

	err := (DistributionTargetSpec{
		BOMPath:          "bom.yaml",
		RolloutPolicyRef: &DistributionPolicyRef{Name: " "},
	}).Validate()
	if err == nil {
		t.Fatal("Validate() error = nil, want empty rollout policy ref error")
	}
}

func TestDistributionTargetSpecAgentOptionsRejectsUnresolvedRolloutPolicyRef(t *testing.T) {
	t.Parallel()

	_, err := (DistributionTargetSpec{
		BOMPath:          "bom.yaml",
		RolloutPolicyRef: &DistributionPolicyRef{Name: "steady"},
	}).AgentOptions(Defaults{})
	if err == nil {
		t.Fatal("AgentOptions() error = nil, want unresolved rollout policy ref error")
	}
}

func TestDistributionRolloutPolicySpecValidateRejectsNegativeBatch(t *testing.T) {
	t.Parallel()

	err := (DistributionRolloutPolicySpec{
		Strategy: reconcile.RolloutStrategy{BatchSize: -1},
	}).Validate()
	if err == nil {
		t.Fatal("Validate() error = nil, want negative batch size error")
	}
}
