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

package compare

import (
	"strings"
	"testing"

	"github.com/labring/sealos/pkg/distribution/hydrate"
	"github.com/labring/sealos/pkg/distribution/state"
)

func TestSummarizeStatus(t *testing.T) {
	t.Parallel()

	result := &Result{
		Objects: []ObjectStatus{
			{
				Tracked: hydrate.TrackedK8sObject{
					APIVersion: "v1",
					Kind:       "Secret",
					Namespace:  "default",
					Name:       "grafana-admin-credentials",
					Ownership:  hydrate.InventoryOwnershipLocal,
				},
				Fragments: []hydrate.TrackedK8sObject{
					{Ownership: hydrate.InventoryOwnershipLocal},
				},
				Presence:   ObjectPresencePresent,
				Comparison: ObjectComparisonMatched,
				State:      state.StateClean,
			},
			{
				Tracked: hydrate.TrackedK8sObject{
					APIVersion: "apps/v1",
					Kind:       "DaemonSet",
					Namespace:  "kube-system",
					Name:       "cilium",
					Ownership:  hydrate.InventoryOwnershipGlobal,
				},
				Fragments: []hydrate.TrackedK8sObject{
					{Ownership: hydrate.InventoryOwnershipGlobal},
				},
				Presence:   ObjectPresencePresent,
				Comparison: ObjectComparisonDrifted,
				State:      state.StateOrphan,
				Mismatches: []FieldMismatch{
					{
						Path:           "spec.template.spec.containers[name=cilium-agent].image",
						Ownership:      hydrate.InventoryOwnershipGlobal,
						PolicyEligible: false,
						State:          state.StateOrphan,
					},
				},
				Remediation: &HostPathRemediation{
					Action:      "reviewDistributionBaselineForAppliedObject",
					ChangeOwner: "globalBaseline",
				},
			},
			{
				Tracked: hydrate.TrackedK8sObject{
					APIVersion: "v1",
					Kind:       "ConfigMap",
					Namespace:  "kube-system",
					Name:       "cilium-config",
					Ownership:  hydrate.InventoryOwnershipGlobal,
				},
				Fragments: []hydrate.TrackedK8sObject{
					{Ownership: hydrate.InventoryOwnershipGlobal},
				},
				Presence:   ObjectPresencePresent,
				Comparison: ObjectComparisonDrifted,
				State:      state.StateOrphan,
				Mismatches: []FieldMismatch{
					{
						Path:           "data.enable-hubble",
						Ownership:      hydrate.InventoryOwnershipGlobal,
						PolicyName:     "defaultLocalPatchPolicy",
						PolicyEligible: true,
						State:          state.StateOrphan,
					},
				},
				Remediation: &HostPathRemediation{
					Action:              "reviewDistributionBaselineForAppliedObject",
					ChangeOwner:         "globalBaseline",
					PolicyName:          "defaultLocalPatchPolicy",
					PolicyEligiblePaths: []string{"data.enable-hubble"},
				},
			},
			{
				Tracked: hydrate.TrackedK8sObject{
					APIVersion: "v1",
					Kind:       "ConfigMap",
					Namespace:  "default",
					Name:       "grafana-settings",
					Ownership:  hydrate.InventoryOwnershipGlobal,
				},
				Fragments: []hydrate.TrackedK8sObject{
					{Ownership: hydrate.InventoryOwnershipGlobal},
					{Ownership: hydrate.InventoryOwnershipLocal},
				},
				Presence:   ObjectPresencePresent,
				Comparison: ObjectComparisonDrifted,
				State:      state.StateDirty,
				Mismatches: []FieldMismatch{
					{
						Path:      "data.adminUser",
						Ownership: hydrate.InventoryOwnershipLocal,
						State:     state.StateDirty,
					},
				},
				Remediation: &HostPathRemediation{
					Action:      "reviewLocalObjectOverlayAndCommitOrReapply",
					ChangeOwner: "localOverlay",
				},
			},
		},
		HostPaths: []HostPathStatus{
			{
				Tracked: hydrate.TrackedHostPath{
					HostPath:  "/etc/kubernetes/kubeadm.yaml",
					Component: "kubernetes",
					Ownership: hydrate.InventoryOwnershipGlobal,
					Type:      hydrate.HostPathRegularFile,
				},
				Presence:   ObjectPresencePresent,
				Comparison: ObjectComparisonDrifted,
				State:      state.StateOrphan,
				Mismatches: []HostPathMismatch{
					{
						Reason: "contentMismatch",
						State:  state.StateOrphan,
					},
				},
				Remediation: &HostPathRemediation{
					Action:      "reviewDistributionBaselineForHostPath",
					ChangeOwner: "globalBaseline",
				},
			},
			{
				Tracked: hydrate.TrackedHostPath{
					HostPath:  "/etc/default/kubelet",
					Component: "kubernetes",
					Ownership: hydrate.InventoryOwnershipLocal,
					Type:      hydrate.HostPathRegularFile,
				},
				Presence:   ObjectPresencePresent,
				Comparison: ObjectComparisonDrifted,
				State:      state.StateDirty,
				Mismatches: []HostPathMismatch{
					{
						Reason: "modeMismatch",
						State:  state.StateDirty,
					},
				},
				Remediation: &HostPathRemediation{
					Action:      "reviewLocalHostInputAndCommitOrReapply",
					ChangeOwner: "localInput",
				},
			},
		},
	}

	summary := SummarizeStatus(result)
	if got, want := summary.State, state.StateOrphan; got != want {
		t.Fatalf("summary.state = %q, want %q", got, want)
	}
	if got, want := summary.OperatorActionSummary.DirectCommitEligible, 2; got != want {
		t.Fatalf("summary.operatorActionSummary.directCommitEligible = %d, want %d", got, want)
	}
	if got, want := summary.OperatorActionSummary.DirectRevertEligible, 4; got != want {
		t.Fatalf("summary.operatorActionSummary.directRevertEligible = %d, want %d", got, want)
	}
	if got, want := summary.OperatorActionSummary.BundleMatchRequired, 4; got != want {
		t.Fatalf("summary.operatorActionSummary.bundleMatchRequired = %d, want %d", got, want)
	}
	if got, want := len(summary.MixedOwnershipObjects), 1; got != want {
		t.Fatalf("len(summary.mixedOwnershipObjects) = %d, want %d", got, want)
	}
	if got, want := summary.MixedOwnershipObjects[0].Name, "grafana-settings"; got != want {
		t.Fatalf("summary.mixedOwnershipObjects[0].name = %q, want %q", got, want)
	}
	if got, want := len(summary.MixedOwnershipObjects[0].Ownerships), 2; got != want {
		t.Fatalf("len(summary.mixedOwnershipObjects[0].ownerships) = %d, want %d", got, want)
	}
	if got, want := len(summary.DirtyObjects), 1; got != want {
		t.Fatalf("len(summary.dirtyObjects) = %d, want %d", got, want)
	}
	if got, want := summary.DirtyObjects[0].Name, "grafana-settings"; got != want {
		t.Fatalf("summary.dirtyObjects[0].name = %q, want %q", got, want)
	}
	if got, want := len(summary.DirtyObjects[0].Paths), 1; got != want {
		t.Fatalf("len(summary.dirtyObjects[0].paths) = %d, want %d", got, want)
	}
	if got, want := summary.DirtyObjects[0].Paths[0], "data.adminUser"; got != want {
		t.Fatalf("summary.dirtyObjects[0].paths[0] = %q, want %q", got, want)
	}
	if summary.DirtyObjects[0].Remediation == nil {
		t.Fatal("summary.dirtyObjects[0].remediation = nil, want remediation")
	}
	if got, want := summary.DirtyObjects[0].Remediation.Action, "reviewLocalObjectOverlayAndCommitOrReapply"; got != want {
		t.Fatalf("summary.dirtyObjects[0].remediation.action = %q, want %q", got, want)
	}
	if got, want := summary.DirtyObjects[0].OperatorAction, OperatorActionCommitOrReapplyLocalOverlay; got != want {
		t.Fatalf("summary.dirtyObjects[0].operatorAction = %q, want %q", got, want)
	}
	if summary.DirtyObjects[0].OperatorActionMetadata == nil {
		t.Fatal("summary.dirtyObjects[0].operatorActionMetadata = nil, want metadata")
	}
	if got, want := summary.DirtyObjects[0].OperatorActionMetadata.AllowsDirectCommit, true; got != want {
		t.Fatalf("summary.dirtyObjects[0].operatorActionMetadata.allowsDirectCommit = %t, want %t", got, want)
	}
	if got, want := summary.DirtyObjects[0].OperatorActionMetadata.AllowsDirectRevert, true; got != want {
		t.Fatalf("summary.dirtyObjects[0].operatorActionMetadata.allowsDirectRevert = %t, want %t", got, want)
	}
	if got, want := summary.DirtyObjects[0].OperatorActionMetadata.RequiresBundleMatch, true; got != want {
		t.Fatalf("summary.dirtyObjects[0].operatorActionMetadata.requiresBundleMatch = %t, want %t", got, want)
	}
	if got, want := len(summary.OrphanObjects), 2; got != want {
		t.Fatalf("len(summary.orphanObjects) = %d, want %d", got, want)
	}
	if got, want := summary.OrphanObjects[0].Name, "cilium-config"; got != want {
		t.Fatalf("summary.orphanObjects[0].name = %q, want %q", got, want)
	}
	if got, want := len(summary.OrphanObjects[0].Paths), 1; got != want {
		t.Fatalf("len(summary.orphanObjects[0].paths) = %d, want %d", got, want)
	}
	if got, want := summary.OrphanObjects[0].Paths[0], "data.enable-hubble"; got != want {
		t.Fatalf("summary.orphanObjects[0].paths[0] = %q, want %q", got, want)
	}
	if summary.OrphanObjects[0].Remediation == nil {
		t.Fatal("summary.orphanObjects[0].remediation = nil, want remediation")
	}
	if got, want := summary.OrphanObjects[0].Remediation.Action, "reviewDistributionBaselineForAppliedObject"; got != want {
		t.Fatalf("summary.orphanObjects[0].remediation.action = %q, want %q", got, want)
	}
	if got, want := summary.OrphanObjects[0].OperatorAction, OperatorActionPromoteToLocalPatch; got != want {
		t.Fatalf("summary.orphanObjects[0].operatorAction = %q, want %q", got, want)
	}
	if summary.OrphanObjects[0].OperatorActionMetadata == nil {
		t.Fatal("summary.orphanObjects[0].operatorActionMetadata = nil, want metadata")
	}
	if got, want := summary.OrphanObjects[0].OperatorActionMetadata.AllowsDirectCommit, false; got != want {
		t.Fatalf("summary.orphanObjects[0].operatorActionMetadata.allowsDirectCommit = %t, want %t", got, want)
	}
	if got, want := summary.OrphanObjects[0].OperatorActionMetadata.AllowsDirectRevert, false; got != want {
		t.Fatalf("summary.orphanObjects[0].operatorActionMetadata.allowsDirectRevert = %t, want %t", got, want)
	}
	if got, want := summary.OrphanObjects[0].OperatorActionMetadata.RequiresBundleMatch, false; got != want {
		t.Fatalf("summary.orphanObjects[0].operatorActionMetadata.requiresBundleMatch = %t, want %t", got, want)
	}
	if got, want := summary.OrphanObjects[1].Name, "cilium"; got != want {
		t.Fatalf("summary.orphanObjects[1].name = %q, want %q", got, want)
	}
	if got, want := summary.OrphanObjects[1].Paths[0], "spec.template.spec.containers[name=cilium-agent].image"; got != want {
		t.Fatalf("summary.orphanObjects[1].paths[0] = %q, want %q", got, want)
	}
	if got, want := summary.OrphanObjects[1].OperatorAction, OperatorActionRevertOrUpdateGlobalBaseline; got != want {
		t.Fatalf("summary.orphanObjects[1].operatorAction = %q, want %q", got, want)
	}
	if got, want := len(summary.PolicyEligibleOrphanObjects), 1; got != want {
		t.Fatalf("len(summary.policyEligibleOrphanObjects) = %d, want %d", got, want)
	}
	if got, want := summary.PolicyEligibleOrphanObjects[0].Name, "cilium-config"; got != want {
		t.Fatalf("summary.policyEligibleOrphanObjects[0].name = %q, want %q", got, want)
	}
	if got, want := summary.PolicyEligibleOrphanObjects[0].Paths[0], "data.enable-hubble"; got != want {
		t.Fatalf("summary.policyEligibleOrphanObjects[0].paths[0] = %q, want %q", got, want)
	}
	if summary.PolicyEligibleOrphanObjects[0].Remediation == nil {
		t.Fatal("summary.policyEligibleOrphanObjects[0].remediation = nil, want remediation")
	}
	if got, want := summary.PolicyEligibleOrphanObjects[0].OperatorAction, OperatorActionPromoteToLocalPatch; got != want {
		t.Fatalf("summary.policyEligibleOrphanObjects[0].operatorAction = %q, want %q", got, want)
	}
	if got, want := summary.PolicyEligibleOrphanObjects[0].Remediation.PolicyName, "defaultLocalPatchPolicy"; got != want {
		t.Fatalf("summary.policyEligibleOrphanObjects[0].remediation.policyName = %q, want %q", got, want)
	}
	if got, want := len(summary.DirtyHostPaths), 1; got != want {
		t.Fatalf("len(summary.dirtyHostPaths) = %d, want %d", got, want)
	}
	if got, want := summary.DirtyHostPaths[0].Path, "/etc/default/kubelet"; got != want {
		t.Fatalf("summary.dirtyHostPaths[0].path = %q, want %q", got, want)
	}
	if got, want := summary.DirtyHostPaths[0].Reasons[0], "modeMismatch"; got != want {
		t.Fatalf("summary.dirtyHostPaths[0].reasons[0] = %q, want %q", got, want)
	}
	if summary.DirtyHostPaths[0].Remediation == nil {
		t.Fatal("summary.dirtyHostPaths[0].remediation = nil, want remediation")
	}
	if got, want := summary.DirtyHostPaths[0].Remediation.Action, "reviewLocalHostInputAndCommitOrReapply"; got != want {
		t.Fatalf("summary.dirtyHostPaths[0].remediation.action = %q, want %q", got, want)
	}
	if got, want := summary.DirtyHostPaths[0].OperatorAction, OperatorActionCommitOrReapplyLocalInput; got != want {
		t.Fatalf("summary.dirtyHostPaths[0].operatorAction = %q, want %q", got, want)
	}
	if got, want := len(summary.OrphanHostPaths), 1; got != want {
		t.Fatalf("len(summary.orphanHostPaths) = %d, want %d", got, want)
	}
	if got, want := summary.OrphanHostPaths[0].Path, "/etc/kubernetes/kubeadm.yaml"; got != want {
		t.Fatalf("summary.orphanHostPaths[0].path = %q, want %q", got, want)
	}
	if got, want := summary.OrphanHostPaths[0].Reasons[0], "contentMismatch"; got != want {
		t.Fatalf("summary.orphanHostPaths[0].reasons[0] = %q, want %q", got, want)
	}
	if summary.OrphanHostPaths[0].Remediation == nil {
		t.Fatal("summary.orphanHostPaths[0].remediation = nil, want remediation")
	}
	if got, want := summary.OrphanHostPaths[0].Remediation.Action, "reviewDistributionBaselineForHostPath"; got != want {
		t.Fatalf("summary.orphanHostPaths[0].remediation.action = %q, want %q", got, want)
	}
	if got, want := summary.OrphanHostPaths[0].OperatorAction, OperatorActionRevertOrUpdateGlobalBaseline; got != want {
		t.Fatalf("summary.orphanHostPaths[0].operatorAction = %q, want %q", got, want)
	}
	if summary.OrphanHostPaths[0].OperatorActionMetadata == nil {
		t.Fatal("summary.orphanHostPaths[0].operatorActionMetadata = nil, want metadata")
	}
	if got, want := summary.OrphanHostPaths[0].OperatorActionMetadata.AllowsDirectCommit, false; got != want {
		t.Fatalf("summary.orphanHostPaths[0].operatorActionMetadata.allowsDirectCommit = %t, want %t", got, want)
	}
	if got, want := summary.OrphanHostPaths[0].OperatorActionMetadata.AllowsDirectRevert, true; got != want {
		t.Fatalf("summary.orphanHostPaths[0].operatorActionMetadata.allowsDirectRevert = %t, want %t", got, want)
	}
	if got, want := summary.OrphanHostPaths[0].OperatorActionMetadata.RequiresBundleMatch, true; got != want {
		t.Fatalf("summary.orphanHostPaths[0].operatorActionMetadata.requiresBundleMatch = %t, want %t", got, want)
	}
	if got, want := len(summary.HostPathConflicts), 0; got != want {
		t.Fatalf("len(summary.hostPathConflicts) = %d, want %d", got, want)
	}
}

func TestSummarizeStatusDetectsHostPathConflicts(t *testing.T) {
	t.Parallel()

	summary := SummarizeStatus(&Result{
		HostPaths: []HostPathStatus{
			{
				Host: "10.0.0.11:22",
				Tracked: hydrate.TrackedHostPath{
					HostPath:  "/README",
					Component: "containerd",
				},
				Presence:   ObjectPresencePresent,
				Comparison: ObjectComparisonDrifted,
				State:      state.StateOrphan,
			},
			{
				Host: "10.0.0.11:22",
				Tracked: hydrate.TrackedHostPath{
					HostPath:  "/README",
					Component: "kubernetes",
				},
				Presence:   ObjectPresencePresent,
				Comparison: ObjectComparisonMatched,
				State:      state.StateClean,
			},
			{
				Host: "10.0.0.12:22",
				Tracked: hydrate.TrackedHostPath{
					HostPath:  "/README",
					Component: "containerd",
				},
				Presence:   ObjectPresencePresent,
				Comparison: ObjectComparisonMatched,
				State:      state.StateClean,
			},
		},
	})

	if got, want := len(summary.HostPathConflicts), 1; got != want {
		t.Fatalf("len(summary.hostPathConflicts) = %d, want %d", got, want)
	}
	if got, want := summary.HostPathConflicts[0].Host, "10.0.0.11:22"; got != want {
		t.Fatalf("summary.hostPathConflicts[0].host = %q, want %q", got, want)
	}
	if got, want := summary.HostPathConflicts[0].Path, "/README"; got != want {
		t.Fatalf("summary.hostPathConflicts[0].path = %q, want %q", got, want)
	}
	if got, want := len(summary.HostPathConflicts[0].Components), 2; got != want {
		t.Fatalf("len(summary.hostPathConflicts[0].components) = %d, want %d", got, want)
	}
	if got, want := summary.HostPathConflicts[0].Components[0], "containerd"; got != want {
		t.Fatalf("summary.hostPathConflicts[0].components[0] = %q, want %q", got, want)
	}
	if got, want := summary.HostPathConflicts[0].Components[1], "kubernetes"; got != want {
		t.Fatalf("summary.hostPathConflicts[0].components[1] = %q, want %q", got, want)
	}
	if got, want := summary.HostPathConflicts[0].Hint, "select a single component when reverting this host path"; got != want {
		t.Fatalf("summary.hostPathConflicts[0].hint = %q, want %q", got, want)
	}
}

func TestSummarizeStatusDetectsLocalInputHostSplits(t *testing.T) {
	t.Parallel()

	result := &Result{
		HostPaths: []HostPathStatus{
			{
				Host:          "10.0.0.10:22",
				Presence:      ObjectPresencePresent,
				Comparison:    ObjectComparisonDrifted,
				State:         state.StateDirty,
				LiveDigest:    "sha256:aaa",
				DesiredDigest: "sha256:desired",
				Tracked: hydrate.TrackedHostPath{
					HostPath:  "/etc/kubernetes/kubeadm.yaml",
					Component: "kubernetes",
					Source:    hydrate.InventorySourceLocalInput,
					Ownership: hydrate.InventoryOwnershipLocal,
					Type:      hydrate.HostPathRegularFile,
					InputName: "kubeadm-cluster-config",
				},
				Mismatches: []HostPathMismatch{{
					Reason: "contentMismatch",
					State:  state.StateDirty,
				}},
				Remediation: &HostPathRemediation{
					Action:      "reviewLocalHostInputAndCommitOrReapply",
					ChangeOwner: "localInput",
				},
			},
			{
				Host:          "10.0.0.11:22",
				Presence:      ObjectPresencePresent,
				Comparison:    ObjectComparisonDrifted,
				State:         state.StateDirty,
				LiveDigest:    "sha256:bbb",
				DesiredDigest: "sha256:desired",
				Tracked: hydrate.TrackedHostPath{
					HostPath:  "/etc/kubernetes/kubeadm.yaml",
					Component: "kubernetes",
					Source:    hydrate.InventorySourceLocalInput,
					Ownership: hydrate.InventoryOwnershipLocal,
					Type:      hydrate.HostPathRegularFile,
					InputName: "kubeadm-cluster-config",
					HostInputBindings: map[string]string{
						"10.0.0.11": "components/kubernetes/host-inputs/10.0.0.11/files/etc/kubernetes/kubeadm.yaml",
					},
				},
				Mismatches: []HostPathMismatch{{
					Reason: "contentMismatch",
					State:  state.StateDirty,
				}},
				Remediation: &HostPathRemediation{
					Action:      "reviewLocalHostInputAndCommitOrReapply",
					ChangeOwner: "localInput",
				},
			},
		},
	}

	summary := SummarizeStatus(result)
	if got, want := len(summary.LocalInputHostSplits), 1; got != want {
		t.Fatalf("len(summary.localInputHostSplits) = %d, want %d", got, want)
	}
	split := summary.LocalInputHostSplits[0]
	if got, want := split.Path, "/etc/kubernetes/kubeadm.yaml"; got != want {
		t.Fatalf("split.path = %q, want %q", got, want)
	}
	if got, want := split.Component, "kubernetes"; got != want {
		t.Fatalf("split.component = %q, want %q", got, want)
	}
	if got, want := split.InputName, "kubeadm-cluster-config"; got != want {
		t.Fatalf("split.inputName = %q, want %q", got, want)
	}
	if got, want := len(split.Hosts), 2; got != want {
		t.Fatalf("len(split.hosts) = %d, want %d", got, want)
	}
	if got, want := strings.Join(split.HostsWithScopedInput, ","), "10.0.0.11:22"; got != want {
		t.Fatalf("split.hostsWithScopedInput = %q, want %q", got, want)
	}
	if got, want := strings.Join(split.HostsWithoutScopedInput, ","), "10.0.0.10:22"; got != want {
		t.Fatalf("split.hostsWithoutScopedInput = %q, want %q", got, want)
	}
	if got, want := split.Hint, "some hosts already have host-scoped input payloads; add host-scoped inputs for the remaining divergent hosts or commit an explicit host"; got != want {
		t.Fatalf("split.hint = %q, want %q", got, want)
	}
}

func TestSummarizeStatusUsesMissingReasonForMissingHostPath(t *testing.T) {
	t.Parallel()

	summary := SummarizeStatus(&Result{
		HostPaths: []HostPathStatus{
			{
				Tracked: hydrate.TrackedHostPath{
					HostPath:        "/etc/kubernetes/manifests/kube-apiserver.yaml",
					Component:       "kubernetes",
					Ownership:       hydrate.InventoryOwnershipGlobal,
					ProjectionClass: hydrate.HostPathProjectionClassGenerated,
					Type:            hydrate.HostPathRegularFile,
				},
				Presence: ObjectPresenceMissing,
				State:    state.StateOrphan,
			},
		},
	})
	if got, want := len(summary.OrphanHostPaths), 1; got != want {
		t.Fatalf("len(summary.orphanHostPaths) = %d, want %d", got, want)
	}
	if got, want := len(summary.OrphanHostPaths[0].Reasons), 1; got != want {
		t.Fatalf("len(summary.orphanHostPaths[0].reasons) = %d, want %d", got, want)
	}
	if got, want := summary.OrphanHostPaths[0].Reasons[0], "missing"; got != want {
		t.Fatalf("summary.orphanHostPaths[0].reasons[0] = %q, want %q", got, want)
	}
	if summary.OrphanHostPaths[0].Remediation != nil {
		t.Fatalf("summary.orphanHostPaths[0].remediation = %#v, want nil", summary.OrphanHostPaths[0].Remediation)
	}
	if got, want := summary.OrphanHostPaths[0].OperatorAction, OperatorAction(""); got != want {
		t.Fatalf("summary.orphanHostPaths[0].operatorAction = %q, want %q", got, want)
	}
	if summary.OrphanHostPaths[0].OperatorActionMetadata != nil {
		t.Fatalf("summary.orphanHostPaths[0].operatorActionMetadata = %#v, want nil", summary.OrphanHostPaths[0].OperatorActionMetadata)
	}
	if got, want := summary.OperatorActionSummary.DirectCommitEligible, 0; got != want {
		t.Fatalf("summary.operatorActionSummary.directCommitEligible = %d, want %d", got, want)
	}
	if got, want := summary.OperatorActionSummary.DirectRevertEligible, 0; got != want {
		t.Fatalf("summary.operatorActionSummary.directRevertEligible = %d, want %d", got, want)
	}
	if got, want := summary.OperatorActionSummary.BundleMatchRequired, 0; got != want {
		t.Fatalf("summary.operatorActionSummary.bundleMatchRequired = %d, want %d", got, want)
	}
}

func TestSummarizeStatusCopiesGeneratedHostPathRemediation(t *testing.T) {
	t.Parallel()

	summary := SummarizeStatus(&Result{
		HostPaths: []HostPathStatus{
			{
				Tracked: hydrate.TrackedHostPath{
					HostPath:        "/etc/kubernetes/manifests/kube-apiserver.yaml",
					Component:       "kubernetes",
					Ownership:       hydrate.InventoryOwnershipGlobal,
					ProjectionClass: hydrate.HostPathProjectionClassGenerated,
					Type:            hydrate.HostPathRegularFile,
				},
				Presence:   ObjectPresencePresent,
				Comparison: ObjectComparisonDrifted,
				State:      state.StateOrphan,
				Mismatches: []HostPathMismatch{{
					Reason: "imageMismatch",
					State:  state.StateOrphan,
				}},
				Remediation: &HostPathRemediation{
					Action:           "updateLocalBootstrapInputAndRerender",
					ChangeOwner:      "localInput",
					Source:           "rendered kubeadm config (files/etc/kubernetes/kubeadm.yaml)",
					SafeDirectRevert: false,
					SafeCommit:       false,
					NextSteps: []string{
						"Update the cluster-local bootstrap input that feeds the rendered kubeadm config.",
					},
					AllowedCommands: []string{
						"sync render",
					},
				},
			},
		},
	})
	if got, want := len(summary.OrphanHostPaths), 1; got != want {
		t.Fatalf("len(summary.orphanHostPaths) = %d, want %d", got, want)
	}
	if summary.OrphanHostPaths[0].Remediation == nil {
		t.Fatal("summary.orphanHostPaths[0].remediation = nil, want remediation")
	}
	if got, want := summary.OrphanHostPaths[0].Remediation.Action, "updateLocalBootstrapInputAndRerender"; got != want {
		t.Fatalf("summary.orphanHostPaths[0].remediation.action = %q, want %q", got, want)
	}
	if got, want := summary.OrphanHostPaths[0].Remediation.ChangeOwner, "localInput"; got != want {
		t.Fatalf("summary.orphanHostPaths[0].remediation.changeOwner = %q, want %q", got, want)
	}
	if got, want := summary.OrphanHostPaths[0].OperatorAction, OperatorActionUpdateLocalInputAndRerender; got != want {
		t.Fatalf("summary.orphanHostPaths[0].operatorAction = %q, want %q", got, want)
	}
	if summary.OrphanHostPaths[0].OperatorActionMetadata == nil {
		t.Fatal("summary.orphanHostPaths[0].operatorActionMetadata = nil, want metadata")
	}
	if got, want := summary.OrphanHostPaths[0].OperatorActionMetadata.AllowsDirectCommit, false; got != want {
		t.Fatalf("summary.orphanHostPaths[0].operatorActionMetadata.allowsDirectCommit = %t, want %t", got, want)
	}
	if got, want := summary.OrphanHostPaths[0].OperatorActionMetadata.AllowsDirectRevert, false; got != want {
		t.Fatalf("summary.orphanHostPaths[0].operatorActionMetadata.allowsDirectRevert = %t, want %t", got, want)
	}
	if got, want := summary.OrphanHostPaths[0].OperatorActionMetadata.RequiresBundleMatch, false; got != want {
		t.Fatalf("summary.orphanHostPaths[0].operatorActionMetadata.requiresBundleMatch = %t, want %t", got, want)
	}
	if got, want := summary.OrphanHostPaths[0].Remediation.NextSteps[0], "Update the cluster-local bootstrap input that feeds the rendered kubeadm config."; got != want {
		t.Fatalf("summary.orphanHostPaths[0].remediation.nextSteps[0] = %q, want %q", got, want)
	}
	if got, want := summary.OrphanHostPaths[0].Remediation.AllowedCommands[0], "sync render"; got != want {
		t.Fatalf("summary.orphanHostPaths[0].remediation.allowedCommands[0] = %q, want %q", got, want)
	}
	if got, want := summary.OperatorActionSummary.DirectCommitEligible, 0; got != want {
		t.Fatalf("summary.operatorActionSummary.directCommitEligible = %d, want %d", got, want)
	}
	if got, want := summary.OperatorActionSummary.DirectRevertEligible, 0; got != want {
		t.Fatalf("summary.operatorActionSummary.directRevertEligible = %d, want %d", got, want)
	}
	if got, want := summary.OperatorActionSummary.BundleMatchRequired, 0; got != want {
		t.Fatalf("summary.operatorActionSummary.bundleMatchRequired = %d, want %d", got, want)
	}
}
