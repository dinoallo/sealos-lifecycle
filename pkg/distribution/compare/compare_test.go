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
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/labring/sealos/pkg/distribution/hydrate"
	"github.com/labring/sealos/pkg/distribution/ownership"
	"github.com/labring/sealos/pkg/distribution/state"
	yamlutil "github.com/labring/sealos/pkg/utils/yaml"
)

func TestCompareBundle(t *testing.T) {
	t.Parallel()

	bundleRoot := t.TempDir()
	secretPath := filepath.Join(bundleRoot, "local-resources", "grafana-admin-secret.yaml")
	if err := os.MkdirAll(filepath.Dir(secretPath), 0o755); err != nil {
		t.Fatalf("MkdirAll() error = %v", err)
	}
	if err := os.WriteFile(secretPath, []byte("apiVersion: v1\nkind: Secret\nmetadata:\n  name: grafana-admin-credentials\n  namespace: default\nstringData:\n  username: admin\n  password: passw0rd\n"), 0o644); err != nil {
		t.Fatalf("WriteFile(secret) error = %v", err)
	}
	missingSecretPath := filepath.Join(bundleRoot, "local-resources", "missing-secret.yaml")
	if err := os.WriteFile(missingSecretPath, []byte("apiVersion: v1\nkind: Secret\nmetadata:\n  name: missing-secret\n  namespace: default\nstringData:\n  token: missing\n"), 0o644); err != nil {
		t.Fatalf("WriteFile(missing secret) error = %v", err)
	}

	manifestPath := filepath.Join(
		bundleRoot,
		"components",
		"cilium",
		"files",
		"manifests",
		"cilium.yaml",
	)
	if err := os.MkdirAll(filepath.Dir(manifestPath), 0o755); err != nil {
		t.Fatalf("MkdirAll() error = %v", err)
	}
	if err := os.WriteFile(manifestPath, []byte("apiVersion: apps/v1\nkind: DaemonSet\nmetadata:\n  name: cilium\n  namespace: kube-system\nspec:\n  template:\n    spec:\n      containers:\n        - name: cilium-agent\n          image: quay.io/cilium/cilium:v1.15.0\n"), 0o644); err != nil {
		t.Fatalf("WriteFile(manifest) error = %v", err)
	}

	bundle := &hydrate.Bundle{
		Spec: hydrate.BundleSpec{
			TrackedK8sObjects: []hydrate.TrackedK8sObject{
				{
					APIVersion: "v1",
					Kind:       "Secret",
					Namespace:  "default",
					Name:       "grafana-admin-credentials",
					Path:       "local-resources/grafana-admin-secret.yaml",
					Source:     hydrate.InventorySourceLocalResource,
					Ownership:  hydrate.InventoryOwnershipLocal,
				},
				{
					APIVersion: "apps/v1",
					Kind:       "DaemonSet",
					Namespace:  "kube-system",
					Name:       "cilium",
					Path:       "components/cilium/files/manifests/cilium.yaml",
					Component:  "cilium",
					Source:     hydrate.InventorySourcePackageManifest,
					Ownership:  hydrate.InventoryOwnershipGlobal,
				},
				{
					APIVersion: "v1",
					Kind:       "Secret",
					Namespace:  "default",
					Name:       "missing-secret",
					Path:       "local-resources/missing-secret.yaml",
					Source:     hydrate.InventorySourceLocalResource,
					Ownership:  hydrate.InventoryOwnershipLocal,
				},
			},
		},
	}

	result, err := CompareBundle(
		bundle,
		bundleRoot,
		resolverFunc(func(apiVersion, kind, namespace, name string) ([]byte, error) {
			switch name {
			case "grafana-admin-credentials":
				return []byte(
					`{"apiVersion":"v1","kind":"Secret","metadata":{"name":"grafana-admin-credentials","namespace":"default","resourceVersion":"1","uid":"abc","managedFields":[{}],"annotations":{"kubectl.kubernetes.io/last-applied-configuration":"{}"}},"data":{"username":"YWRtaW4=","password":"cGFzc3cwcmQ="},"type":"Opaque"}`,
				), nil
			case "cilium":
				return []byte(
					`{"apiVersion":"apps/v1","kind":"DaemonSet","metadata":{"name":"cilium","namespace":"kube-system","resourceVersion":"2"},"spec":{"template":{"spec":{"containers":[{"name":"cilium-agent","image":"quay.io/cilium/cilium:v1.15.1"}]}}}}`,
				), nil
			default:
				return nil, NewNotFoundError(apiVersion, kind, namespace, name)
			}
		}),
	)
	if err != nil {
		t.Fatalf("CompareBundle() error = %v", err)
	}
	if got, want := result.Summary.Total, 3; got != want {
		t.Fatalf("summary.total = %d, want %d", got, want)
	}
	if got, want := result.Summary.Present, 2; got != want {
		t.Fatalf("summary.present = %d, want %d", got, want)
	}
	if got, want := result.Summary.Missing, 1; got != want {
		t.Fatalf("summary.missing = %d, want %d", got, want)
	}
	if got, want := result.Summary.Matched, 1; got != want {
		t.Fatalf("summary.matched = %d, want %d", got, want)
	}
	if got, want := result.Summary.Drifted, 1; got != want {
		t.Fatalf("summary.drifted = %d, want %d", got, want)
	}
	if got, want := result.Summary.Clean, 1; got != want {
		t.Fatalf("summary.clean = %d, want %d", got, want)
	}
	if got, want := result.Summary.Dirty, 1; got != want {
		t.Fatalf("summary.dirty = %d, want %d", got, want)
	}
	if got, want := result.Summary.Orphan, 1; got != want {
		t.Fatalf("summary.orphan = %d, want %d", got, want)
	}
	if got, want := len(result.Objects), 3; got != want {
		t.Fatalf("len(objects) = %d, want %d", got, want)
	}
	matched := result.Objects[0]
	if got, want := matched.State, state.StateClean; got != want {
		t.Fatalf("matched.state = %q, want %q", got, want)
	}
	drifted := result.Objects[1]
	if got, want := drifted.Comparison, ObjectComparisonDrifted; got != want {
		t.Fatalf("drifted.comparison = %q, want %q", got, want)
	}
	if got, want := drifted.State, state.StateOrphan; got != want {
		t.Fatalf("drifted.state = %q, want %q", got, want)
	}
	if got, want := len(drifted.Mismatches), 1; got != want {
		t.Fatalf("len(drifted.mismatches) = %d, want %d", got, want)
	}
	if got, want := drifted.Mismatches[0].Path, "spec.template.spec.containers[name=cilium-agent].image"; got != want {
		t.Fatalf("drifted.mismatches[0].path = %q, want %q", got, want)
	}
	if got, want := drifted.Mismatches[0].Reason, "valueMismatch"; got != want {
		t.Fatalf("drifted.mismatches[0].reason = %q, want %q", got, want)
	}
	if got, want := drifted.Mismatches[0].Ownership, hydrate.InventoryOwnershipGlobal; got != want {
		t.Fatalf("drifted.mismatches[0].ownership = %q, want %q", got, want)
	}
	if got := drifted.Mismatches[0].PolicyName; got != "" {
		t.Fatalf("drifted.mismatches[0].policyName = %q, want empty", got)
	}
	if got := drifted.Mismatches[0].PolicyEligible; got {
		t.Fatalf("drifted.mismatches[0].policyEligible = %t, want false", got)
	}
	if got, want := drifted.Mismatches[0].State, state.StateOrphan; got != want {
		t.Fatalf("drifted.mismatches[0].state = %q, want %q", got, want)
	}
	if drifted.Remediation == nil {
		t.Fatal("drifted.remediation = nil, want remediation")
	}
	if got, want := drifted.Remediation.Action, "reviewDistributionBaselineForAppliedObject"; got != want {
		t.Fatalf("drifted.remediation.action = %q, want %q", got, want)
	}
	if got, want := drifted.Remediation.ChangeOwner, "globalBaseline"; got != want {
		t.Fatalf("drifted.remediation.changeOwner = %q, want %q", got, want)
	}
	if got, want := drifted.Remediation.AllowedCommands[2], "sync revert"; got != want {
		t.Fatalf("drifted.remediation.allowedCommands[2] = %q, want %q", got, want)
	}
	missing := result.Objects[2]
	if got, want := missing.State, state.StateDirty; got != want {
		t.Fatalf("missing.state = %q, want %q", got, want)
	}
	if missing.Remediation == nil {
		t.Fatal("missing.remediation = nil, want remediation")
	}
	if got, want := missing.Remediation.Action, "reviewLocalObjectOverlayAndCommitOrReapply"; got != want {
		t.Fatalf("missing.remediation.action = %q, want %q", got, want)
	}
	if got, want := missing.Remediation.ChangeOwner, "localOverlay"; got != want {
		t.Fatalf("missing.remediation.changeOwner = %q, want %q", got, want)
	}
	if got, want := missing.Remediation.SafeCommit, false; got != want {
		t.Fatalf("missing.remediation.safeCommit = %t, want %t", got, want)
	}
}

func TestCompareBundleMarksPolicyEligibleOrphanObjectRemediation(t *testing.T) {
	t.Parallel()

	bundleRoot := t.TempDir()
	manifestPath := filepath.Join(
		bundleRoot,
		"components",
		"cilium",
		"files",
		"manifests",
		"cilium-config.yaml",
	)
	if err := os.MkdirAll(filepath.Dir(manifestPath), 0o755); err != nil {
		t.Fatalf("MkdirAll() error = %v", err)
	}
	if err := os.WriteFile(manifestPath, []byte("apiVersion: v1\nkind: ConfigMap\nmetadata:\n  name: cilium-config\n  namespace: kube-system\ndata:\n  enable-hubble: \"false\"\n"), 0o644); err != nil {
		t.Fatalf("WriteFile(manifest) error = %v", err)
	}

	bundle := &hydrate.Bundle{
		Spec: hydrate.BundleSpec{
			TrackedK8sObjects: []hydrate.TrackedK8sObject{
				{
					APIVersion: "v1",
					Kind:       "ConfigMap",
					Namespace:  "kube-system",
					Name:       "cilium-config",
					Path:       "components/cilium/files/manifests/cilium-config.yaml",
					Component:  "cilium",
					Source:     hydrate.InventorySourcePackageManifest,
					Ownership:  hydrate.InventoryOwnershipGlobal,
				},
			},
		},
	}

	result, err := CompareBundle(
		bundle,
		bundleRoot,
		resolverFunc(func(apiVersion, kind, namespace, name string) ([]byte, error) {
			return []byte(
				`{"apiVersion":"v1","kind":"ConfigMap","metadata":{"name":"cilium-config","namespace":"kube-system","resourceVersion":"7"},"data":{"enable-hubble":"true"}}`,
			), nil
		}),
	)
	if err != nil {
		t.Fatalf("CompareBundle() error = %v", err)
	}
	if got, want := len(result.Objects), 1; got != want {
		t.Fatalf("len(objects) = %d, want %d", got, want)
	}
	drifted := result.Objects[0]
	if got, want := drifted.State, state.StateOrphan; got != want {
		t.Fatalf("drifted.state = %q, want %q", got, want)
	}
	if got, want := len(drifted.Mismatches), 1; got != want {
		t.Fatalf("len(drifted.mismatches) = %d, want %d", got, want)
	}
	if got, want := drifted.Mismatches[0].PolicyName, "defaultLocalPatchPolicy"; got != want {
		t.Fatalf("drifted.mismatches[0].policyName = %q, want %q", got, want)
	}
	if got, want := drifted.Mismatches[0].PolicyEligible, true; got != want {
		t.Fatalf("drifted.mismatches[0].policyEligible = %t, want %t", got, want)
	}
	if drifted.Remediation == nil {
		t.Fatal("drifted.remediation = nil, want remediation")
	}
	if got, want := drifted.Remediation.PolicyName, "defaultLocalPatchPolicy"; got != want {
		t.Fatalf("drifted.remediation.policyName = %q, want %q", got, want)
	}
	if got, want := len(drifted.Remediation.PolicyEligiblePaths), 1; got != want {
		t.Fatalf("len(drifted.remediation.policyEligiblePaths) = %d, want %d", got, want)
	}
	if got, want := drifted.Remediation.PolicyEligiblePaths[0], "data.enable-hubble"; got != want {
		t.Fatalf("drifted.remediation.policyEligiblePaths[0] = %q, want %q", got, want)
	}
	if got, want := drifted.Remediation.NextSteps[1], "If the drift should be kept for this cluster, add or update a local repo patch, rerender, and apply the refreshed desired state."; got != want {
		t.Fatalf("drifted.remediation.nextSteps[1] = %q, want %q", got, want)
	}
}

func TestCompareBundleUsesRenderedLocalPatchPolicy(t *testing.T) {
	t.Parallel()

	bundleRoot := t.TempDir()
	manifestPath := filepath.Join(
		bundleRoot,
		"components",
		"grafana",
		"files",
		"manifests",
		"grafana-settings.yaml",
	)
	if err := os.MkdirAll(filepath.Dir(manifestPath), 0o755); err != nil {
		t.Fatalf("MkdirAll(%q) error = %v", filepath.Dir(manifestPath), err)
	}
	if err := os.WriteFile(manifestPath, []byte("apiVersion: v1\nkind: ConfigMap\nmetadata:\n  name: grafana-settings\n  namespace: default\n  annotations:\n    localFeature: disabled\ndata:\n  adminUser: admin\n"), 0o644); err != nil {
		t.Fatalf("WriteFile(%q) error = %v", manifestPath, err)
	}
	policyPath := filepath.Join(bundleRoot, "policy", "local-patch-policy.yaml")
	if err := os.MkdirAll(filepath.Dir(policyPath), 0o755); err != nil {
		t.Fatalf("MkdirAll(%q) error = %v", filepath.Dir(policyPath), err)
	}
	if err := os.WriteFile(policyPath, []byte("apiVersion: distribution.sealos.io/v1alpha1\nkind: LocalPatchPolicy\nmetadata:\n  name: custom-configmap-policy\nspec:\n  scope: clusterLocal\n  forbiddenExactPaths:\n    - status\n    - spec.selector\n  forbiddenMetadataKeys:\n    - uid\n    - resourceVersion\n    - generation\n    - creationTimestamp\n    - managedFields\n    - ownerReferences\n    - finalizers\n    - generateName\n    - selfLink\n    - deletionTimestamp\n    - deletionGracePeriodSeconds\n  forbiddenContainerFields:\n    - image\n  kindRules:\n    - kind: ConfigMap\n      allowedPrefixes:\n        - metadata.annotations\n"), 0o644); err != nil {
		t.Fatalf("WriteFile(%q) error = %v", policyPath, err)
	}

	bundle := &hydrate.Bundle{
		Spec: hydrate.BundleSpec{
			LocalPatchPolicySource: ownership.LocalPatchPolicySourceLocalRepo,
			LocalPatchPolicyName:   "custom-configmap-policy",
			LocalPatchPolicyPath:   "policy/local-patch-policy.yaml",
			TrackedK8sObjects: []hydrate.TrackedK8sObject{
				{
					APIVersion: "v1",
					Kind:       "ConfigMap",
					Namespace:  "default",
					Name:       "grafana-settings",
					Path:       "components/grafana/files/manifests/grafana-settings.yaml",
					Component:  "grafana",
					Source:     hydrate.InventorySourcePackageManifest,
					Ownership:  hydrate.InventoryOwnershipGlobal,
				},
			},
		},
	}

	result, err := CompareBundle(
		bundle,
		bundleRoot,
		resolverFunc(func(apiVersion, kind, namespace, name string) ([]byte, error) {
			return []byte(
				`{"apiVersion":"v1","kind":"ConfigMap","metadata":{"name":"grafana-settings","namespace":"default","annotations":{"localFeature":"enabled"}},"data":{"adminUser":"admin"}}`,
			), nil
		}),
	)
	if err != nil {
		t.Fatalf("CompareBundle() error = %v", err)
	}
	if got, want := len(result.Objects), 1; got != want {
		t.Fatalf("len(objects) = %d, want %d", got, want)
	}
	drifted := result.Objects[0]
	if got, want := len(drifted.Mismatches), 1; got != want {
		t.Fatalf("len(drifted.mismatches) = %d, want %d", got, want)
	}
	if got, want := drifted.Mismatches[0].Path, "metadata.annotations.localFeature"; got != want {
		t.Fatalf("drifted.mismatches[0].path = %q, want %q", got, want)
	}
	if got, want := drifted.Mismatches[0].PolicyName, "custom-configmap-policy"; got != want {
		t.Fatalf("drifted.mismatches[0].policyName = %q, want %q", got, want)
	}
	if got, want := drifted.Mismatches[0].PolicyEligible, true; got != want {
		t.Fatalf("drifted.mismatches[0].policyEligible = %t, want %t", got, want)
	}
	if drifted.Remediation == nil {
		t.Fatal("drifted.remediation = nil, want remediation")
	}
	if got, want := drifted.Remediation.PolicyName, "custom-configmap-policy"; got != want {
		t.Fatalf("drifted.remediation.policyName = %q, want %q", got, want)
	}
	if got, want := drifted.Remediation.PolicyEligiblePaths[0], "metadata.annotations.localFeature"; got != want {
		t.Fatalf("drifted.remediation.policyEligiblePaths[0] = %q, want %q", got, want)
	}
}

func TestCompareBundleWithHostPaths(t *testing.T) {
	t.Parallel()

	bundleRoot := t.TempDir()
	hostRoot := t.TempDir()

	desiredKubeletPath := filepath.Join(
		bundleRoot,
		"components",
		"kubernetes",
		"files",
		"rootfs",
		"usr",
		"bin",
		"kubelet",
	)
	desiredKubeadmPath := filepath.Join(
		bundleRoot,
		"components",
		"kubernetes",
		"files",
		"files",
		"etc",
		"kubernetes",
		"kubeadm.yaml",
	)
	desiredCtrPath := filepath.Join(
		bundleRoot,
		"components",
		"containerd",
		"files",
		"rootfs",
		"usr",
		"bin",
		"ctr",
	)
	for _, path := range []string{desiredKubeletPath, desiredKubeadmPath, desiredCtrPath} {
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatalf("MkdirAll(%q) error = %v", filepath.Dir(path), err)
		}
	}
	if err := os.WriteFile(desiredKubeletPath, []byte("desired-kubelet"), 0o755); err != nil {
		t.Fatalf("WriteFile(%q) error = %v", desiredKubeletPath, err)
	}
	if err := os.WriteFile(desiredKubeadmPath, []byte("apiVersion: kubeadm.k8s.io/v1beta4\nkind: ClusterConfiguration\n"), 0o644); err != nil {
		t.Fatalf("WriteFile(%q) error = %v", desiredKubeadmPath, err)
	}
	if err := os.WriteFile(desiredCtrPath, []byte("desired-ctr"), 0o755); err != nil {
		t.Fatalf("WriteFile(%q) error = %v", desiredCtrPath, err)
	}

	liveKubeletPath := filepath.Join(hostRoot, "usr", "bin", "kubelet")
	liveKubeadmPath := filepath.Join(hostRoot, "etc", "kubernetes", "kubeadm.yaml")
	for _, path := range []string{liveKubeletPath, liveKubeadmPath} {
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatalf("MkdirAll(%q) error = %v", filepath.Dir(path), err)
		}
	}
	if err := os.WriteFile(liveKubeletPath, []byte("drifted-kubelet"), 0o755); err != nil {
		t.Fatalf("WriteFile(%q) error = %v", liveKubeletPath, err)
	}
	if err := os.WriteFile(liveKubeadmPath, []byte("apiVersion: kubeadm.k8s.io/v1beta4\nkind: ClusterConfiguration\n"), 0o644); err != nil {
		t.Fatalf("WriteFile(%q) error = %v", liveKubeadmPath, err)
	}

	bundle := &hydrate.Bundle{
		Spec: hydrate.BundleSpec{
			TrackedHostPaths: []hydrate.TrackedHostPath{
				{
					HostPath:   "/usr/bin/kubelet",
					BundlePath: "components/kubernetes/files/rootfs/usr/bin/kubelet",
					Component:  "kubernetes",
					Source:     hydrate.InventorySourcePackageManifest,
					Ownership:  hydrate.InventoryOwnershipGlobal,
					Type:       hydrate.HostPathRegularFile,
				},
				{
					HostPath:   "/etc/kubernetes/kubeadm.yaml",
					BundlePath: "components/kubernetes/files/files/etc/kubernetes/kubeadm.yaml",
					Component:  "kubernetes",
					Source:     hydrate.InventorySourcePackageManifest,
					Ownership:  hydrate.InventoryOwnershipGlobal,
					Type:       hydrate.HostPathRegularFile,
				},
				{
					HostPath:   "/usr/bin/ctr",
					BundlePath: "components/containerd/files/rootfs/usr/bin/ctr",
					Component:  "containerd",
					Source:     hydrate.InventorySourcePackageManifest,
					Ownership:  hydrate.InventoryOwnershipGlobal,
					Type:       hydrate.HostPathRegularFile,
				},
			},
		},
	}

	result, err := CompareBundleWithOptions(
		bundle,
		bundleRoot,
		resolverFunc(func(apiVersion, kind, namespace, name string) ([]byte, error) {
			return nil, NewNotFoundError(apiVersion, kind, namespace, name)
		}),
		CompareOptions{HostRoot: hostRoot},
	)
	if err != nil {
		t.Fatalf("CompareBundleWithOptions() error = %v", err)
	}
	if got, want := len(result.HostPaths), 3; got != want {
		t.Fatalf("len(hostPaths) = %d, want %d", got, want)
	}
	if got, want := result.Summary.Total, 3; got != want {
		t.Fatalf("summary.total = %d, want %d", got, want)
	}
	if got, want := result.Summary.Present, 2; got != want {
		t.Fatalf("summary.present = %d, want %d", got, want)
	}
	if got, want := result.Summary.Missing, 1; got != want {
		t.Fatalf("summary.missing = %d, want %d", got, want)
	}
	if got, want := result.Summary.Matched, 1; got != want {
		t.Fatalf("summary.matched = %d, want %d", got, want)
	}
	if got, want := result.Summary.Drifted, 1; got != want {
		t.Fatalf("summary.drifted = %d, want %d", got, want)
	}
	if got, want := result.Summary.Clean, 1; got != want {
		t.Fatalf("summary.clean = %d, want %d", got, want)
	}
	if got, want := result.Summary.Orphan, 2; got != want {
		t.Fatalf("summary.orphan = %d, want %d", got, want)
	}

	hostPaths := make(map[string]HostPathStatus, len(result.HostPaths))
	for _, hostPath := range result.HostPaths {
		hostPaths[hostPath.Tracked.HostPath] = hostPath
	}

	drifted := hostPaths["/usr/bin/kubelet"]
	if got, want := drifted.Tracked.HostPath, "/usr/bin/kubelet"; got != want {
		t.Fatalf("drifted.hostPath = %q, want %q", got, want)
	}
	if got, want := drifted.State, state.StateOrphan; got != want {
		t.Fatalf("drifted.state = %q, want %q", got, want)
	}
	if got, want := len(drifted.Mismatches), 1; got != want {
		t.Fatalf("len(drifted.mismatches) = %d, want %d", got, want)
	}
	if got, want := drifted.Mismatches[0].Reason, "contentMismatch"; got != want {
		t.Fatalf("drifted.mismatches[0].reason = %q, want %q", got, want)
	}
	if drifted.Remediation == nil {
		t.Fatal("drifted.remediation = nil, want remediation")
	}
	if got, want := drifted.Remediation.Action, "reviewDistributionBaselineForHostPath"; got != want {
		t.Fatalf("drifted.remediation.action = %q, want %q", got, want)
	}
	if got, want := drifted.Remediation.ChangeOwner, "globalBaseline"; got != want {
		t.Fatalf("drifted.remediation.changeOwner = %q, want %q", got, want)
	}

	matched := hostPaths["/etc/kubernetes/kubeadm.yaml"]
	if got, want := matched.Tracked.HostPath, "/etc/kubernetes/kubeadm.yaml"; got != want {
		t.Fatalf("matched.hostPath = %q, want %q", got, want)
	}
	if got, want := matched.State, state.StateClean; got != want {
		t.Fatalf("matched.state = %q, want %q", got, want)
	}

	missing := hostPaths["/usr/bin/ctr"]
	if got, want := missing.Tracked.HostPath, "/usr/bin/ctr"; got != want {
		t.Fatalf("missing.hostPath = %q, want %q", got, want)
	}
	if got, want := missing.Presence, ObjectPresenceMissing; got != want {
		t.Fatalf("missing.presence = %q, want %q", got, want)
	}
	if missing.Remediation == nil {
		t.Fatal("missing.remediation = nil, want remediation")
	}
	if got, want := missing.Remediation.Action, "reviewDistributionBaselineForHostPath"; got != want {
		t.Fatalf("missing.remediation.action = %q, want %q", got, want)
	}
}

func TestCompareBundleUsesHostScopedInputForHostPathDesiredState(t *testing.T) {
	t.Parallel()

	bundleRoot := t.TempDir()
	hostRoot := t.TempDir()

	defaultDesiredPath := filepath.Join(
		bundleRoot,
		"components",
		"runtime",
		"files",
		"files",
		"etc",
		"demo",
		"config.yaml",
	)
	hostDesiredPath := filepath.Join(
		bundleRoot,
		"components",
		"runtime",
		"host-inputs",
		"10.0.0.11",
		"files",
		"etc",
		"demo",
		"config.yaml",
	)
	livePath := filepath.Join(hostRoot, "etc", "demo", "config.yaml")
	for _, path := range []string{defaultDesiredPath, hostDesiredPath, livePath} {
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatalf("MkdirAll(%q) error = %v", filepath.Dir(path), err)
		}
	}
	if err := os.WriteFile(defaultDesiredPath, []byte("clusterName: default\n"), 0o644); err != nil {
		t.Fatalf("WriteFile(%q) error = %v", defaultDesiredPath, err)
	}
	if err := os.WriteFile(hostDesiredPath, []byte("clusterName: host-11\n"), 0o644); err != nil {
		t.Fatalf("WriteFile(%q) error = %v", hostDesiredPath, err)
	}
	if err := os.WriteFile(livePath, []byte("clusterName: host-11\n"), 0o644); err != nil {
		t.Fatalf("WriteFile(%q) error = %v", livePath, err)
	}

	bundle := &hydrate.Bundle{
		Spec: hydrate.BundleSpec{
			TrackedHostPaths: []hydrate.TrackedHostPath{{
				HostPath:   "/etc/demo/config.yaml",
				BundlePath: "components/runtime/files/files/etc/demo/config.yaml",
				Component:  "runtime",
				Source:     hydrate.InventorySourceLocalInput,
				Ownership:  hydrate.InventoryOwnershipLocal,
				Type:       hydrate.HostPathRegularFile,
				InputName:  "runtime-config",
				HostInputBindings: map[string]string{
					"10.0.0.11": "components/runtime/host-inputs/10.0.0.11/files/etc/demo/config.yaml",
				},
			}},
		},
	}

	result, err := CompareBundleWithOptions(
		bundle,
		bundleRoot,
		resolverFunc(func(apiVersion, kind, namespace, name string) ([]byte, error) {
			return nil, NewNotFoundError(apiVersion, kind, namespace, name)
		}),
		CompareOptions{
			HostRoot:     hostRoot,
			HostIdentity: "10.0.0.11:22",
		},
	)
	if err != nil {
		t.Fatalf("CompareBundleWithOptions() error = %v", err)
	}
	if got, want := result.Summary.Clean, 1; got != want {
		t.Fatalf("summary.clean = %d, want %d", got, want)
	}
	if got, want := result.HostPaths[0].Comparison, ObjectComparisonMatched; got != want {
		t.Fatalf("hostPath.comparison = %q, want %q", got, want)
	}
}

func TestCompareBundleWithGeneratedHostPaths(t *testing.T) {
	t.Parallel()

	bundleRoot := t.TempDir()
	hostRoot := t.TempDir()

	liveAPIServerPath := filepath.Join(
		hostRoot,
		"etc",
		"kubernetes",
		"manifests",
		"kube-apiserver.yaml",
	)
	liveControllerManagerPath := filepath.Join(
		hostRoot,
		"etc",
		"kubernetes",
		"manifests",
		"kube-controller-manager.yaml",
	)
	liveSchedulerPath := filepath.Join(
		hostRoot,
		"etc",
		"kubernetes",
		"manifests",
		"kube-scheduler.yaml",
	)
	for _, path := range []string{liveAPIServerPath, liveControllerManagerPath, liveSchedulerPath} {
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatalf("MkdirAll(%q) error = %v", filepath.Dir(path), err)
		}
	}
	if err := os.WriteFile(liveAPIServerPath, []byte("apiVersion: v1\nkind: Pod\nmetadata:\n  name: kube-apiserver\n  namespace: kube-system\nspec:\n  containers:\n    - name: kube-apiserver\n      image: registry.k8s.io/kube-apiserver:v1.30.3\n      command:\n        - kube-apiserver\n        - --service-cluster-ip-range=10.96.0.0/12\n        - --audit-policy-file=/etc/kubernetes/audit-policy.yaml\n      volumeMounts:\n        - mountPath: /etc/kubernetes/pki\n        - mountPath: /etc/kubernetes\n        - mountPath: /etc/localtime\n"), 0o644); err != nil {
		t.Fatalf("WriteFile(%q) error = %v", liveAPIServerPath, err)
	}
	if err := os.WriteFile(liveControllerManagerPath, []byte("apiVersion: v1\nkind: Pod\nmetadata:\n  name: kube-controller-manager\n  namespace: kube-system\nspec:\n  containers:\n    - name: kube-controller-manager\n      image: registry.k8s.io/kube-controller-manager:v1.30.4\n      command:\n        - kube-controller-manager\n        - --cluster-cidr=10.244.0.0/16\n        - --bind-address=0.0.0.0\n      volumeMounts:\n        - mountPath: /etc/kubernetes/pki\n        - mountPath: /etc/localtime\n"), 0o644); err != nil {
		t.Fatalf("WriteFile(%q) error = %v", liveControllerManagerPath, err)
	}
	if err := os.WriteFile(liveSchedulerPath, []byte("apiVersion: v1\nkind: Pod\nmetadata:\n  name: kube-scheduler\n  namespace: kube-system\nspec:\n  containers:\n    - name: kube-scheduler\n      image: registry.k8s.io/kube-scheduler:v1.30.3\n      command:\n        - kube-scheduler\n"), 0o644); err != nil {
		t.Fatalf("WriteFile(%q) error = %v", liveSchedulerPath, err)
	}

	bundle := &hydrate.Bundle{
		Spec: hydrate.BundleSpec{
			TrackedHostPaths: []hydrate.TrackedHostPath{
				{
					HostPath:        "/etc/kubernetes/manifests/kube-apiserver.yaml",
					Component:       "kubernetes",
					Source:          hydrate.InventorySourceGeneratedHook,
					Ownership:       hydrate.InventoryOwnershipGlobal,
					Type:            hydrate.HostPathRegularFile,
					ProjectionClass: hydrate.HostPathProjectionClassGenerated,
					CompareStrategy: hydrate.HostPathCompareStrategySemanticGenerated,
					Generated: &hydrate.GeneratedHostPathSemantics{
						Tool:            "kubeadm",
						Hook:            "bootstrap",
						APIVersion:      "v1",
						Kind:            "Pod",
						Namespace:       "kube-system",
						Name:            "kube-apiserver",
						ContainerName:   "kube-apiserver",
						ExpectedImage:   "registry.k8s.io/kube-apiserver:v1.30.3",
						ExpectedCommand: "kube-apiserver",
						ExpectedArgs: map[string]string{
							"service-cluster-ip-range": "10.96.0.0/12",
							"audit-policy-file":        "/etc/kubernetes/audit-policy.yaml",
						},
						ExpectedVolumeMounts: []string{
							"/etc/kubernetes",
							"/etc/kubernetes/pki",
							"/etc/localtime",
						},
					},
				},
				{
					HostPath:        "/etc/kubernetes/manifests/kube-controller-manager.yaml",
					Component:       "kubernetes",
					Source:          hydrate.InventorySourceGeneratedHook,
					Ownership:       hydrate.InventoryOwnershipGlobal,
					Type:            hydrate.HostPathRegularFile,
					ProjectionClass: hydrate.HostPathProjectionClassGenerated,
					CompareStrategy: hydrate.HostPathCompareStrategySemanticGenerated,
					Generated: &hydrate.GeneratedHostPathSemantics{
						Tool:            "kubeadm",
						Hook:            "bootstrap",
						APIVersion:      "v1",
						Kind:            "Pod",
						Namespace:       "kube-system",
						Name:            "kube-controller-manager",
						ContainerName:   "kube-controller-manager",
						ExpectedImage:   "registry.k8s.io/kube-controller-manager:v1.30.3",
						ExpectedCommand: "kube-controller-manager",
						ExpectedArgs: map[string]string{
							"bind-address": "0.0.0.0",
							"cluster-cidr": "10.244.0.0/16",
						},
						ExpectedVolumeMounts: []string{
							"/etc/kubernetes/pki",
							"/etc/localtime",
						},
					},
				},
				{
					HostPath:        "/etc/kubernetes/manifests/kube-scheduler.yaml",
					Component:       "kubernetes",
					Source:          hydrate.InventorySourceGeneratedHook,
					Ownership:       hydrate.InventoryOwnershipGlobal,
					Type:            hydrate.HostPathRegularFile,
					ProjectionClass: hydrate.HostPathProjectionClassGenerated,
					CompareStrategy: hydrate.HostPathCompareStrategySemanticGenerated,
					Generated: &hydrate.GeneratedHostPathSemantics{
						Tool:            "kubeadm",
						Hook:            "bootstrap",
						APIVersion:      "v1",
						Kind:            "Pod",
						Namespace:       "kube-system",
						Name:            "kube-scheduler",
						ContainerName:   "kube-scheduler",
						ExpectedImage:   "registry.k8s.io/kube-scheduler:v1.30.3",
						ExpectedCommand: "kube-scheduler",
						ExpectedArgs: map[string]string{
							"bind-address": "0.0.0.0",
						},
						ExpectedVolumeMounts: []string{
							"/etc/localtime",
						},
					},
				},
				{
					HostPath:        "/etc/kubernetes/manifests/etcd.yaml",
					Component:       "kubernetes",
					Source:          hydrate.InventorySourceGeneratedHook,
					Ownership:       hydrate.InventoryOwnershipGlobal,
					Type:            hydrate.HostPathRegularFile,
					ProjectionClass: hydrate.HostPathProjectionClassGenerated,
					CompareStrategy: hydrate.HostPathCompareStrategySemanticGenerated,
					Generated: &hydrate.GeneratedHostPathSemantics{
						Tool:          "kubeadm",
						Hook:          "bootstrap",
						APIVersion:    "v1",
						Kind:          "Pod",
						Namespace:     "kube-system",
						Name:          "etcd",
						ContainerName: "etcd",
						ExpectedImage: "registry.k8s.io/etcd:3.5.12-0",
					},
				},
			},
		},
	}

	result, err := CompareBundleWithOptions(
		bundle,
		bundleRoot,
		resolverFunc(func(apiVersion, kind, namespace, name string) ([]byte, error) {
			return nil, NewNotFoundError(apiVersion, kind, namespace, name)
		}),
		CompareOptions{HostRoot: hostRoot},
	)
	if err != nil {
		t.Fatalf("CompareBundleWithOptions() error = %v", err)
	}
	if got, want := len(result.HostPaths), 4; got != want {
		t.Fatalf("len(hostPaths) = %d, want %d", got, want)
	}
	if got, want := result.Summary.Clean, 1; got != want {
		t.Fatalf("summary.clean = %d, want %d", got, want)
	}
	if got, want := result.Summary.Orphan, 3; got != want {
		t.Fatalf("summary.orphan = %d, want %d", got, want)
	}
	if got, want := result.Summary.Missing, 1; got != want {
		t.Fatalf("summary.missing = %d, want %d", got, want)
	}
	if got, want := result.Summary.Drifted, 2; got != want {
		t.Fatalf("summary.drifted = %d, want %d", got, want)
	}

	hostPaths := make(map[string]HostPathStatus, len(result.HostPaths))
	for _, hostPath := range result.HostPaths {
		hostPaths[hostPath.Tracked.HostPath] = hostPath
	}

	matched := hostPaths["/etc/kubernetes/manifests/kube-apiserver.yaml"]
	if got, want := matched.State, state.StateClean; got != want {
		t.Fatalf("matched.state = %q, want %q", got, want)
	}
	if got, want := matched.Comparison, ObjectComparisonMatched; got != want {
		t.Fatalf("matched.comparison = %q, want %q", got, want)
	}

	drifted := hostPaths["/etc/kubernetes/manifests/kube-controller-manager.yaml"]
	if got, want := drifted.State, state.StateOrphan; got != want {
		t.Fatalf("drifted.state = %q, want %q", got, want)
	}
	if drifted.Remediation == nil {
		t.Fatal("drifted.remediation = nil, want remediation")
	}
	if got, want := drifted.Remediation.Action, "reviewDistributionBaselineForGeneratedProjection"; got != want {
		t.Fatalf("drifted.remediation.action = %q, want %q", got, want)
	}
	if got, want := drifted.Remediation.ChangeOwner, "globalBaseline"; got != want {
		t.Fatalf("drifted.remediation.changeOwner = %q, want %q", got, want)
	}
	assertGeneratedHostPathRemediation(t, drifted.Remediation, generatedRemediationMetadata{
		ProjectionClass: "generatedHostPath",
		Generator:       "kubeadm",
		GeneratedKind:   "Pod",
		GeneratedName:   "kube-controller-manager",
		Repairable:      true,
	})
	if got, want := drifted.Remediation.NextSteps[0], "Review the selected BOM revision and package baseline that define this generated projection."; got != want {
		t.Fatalf("drifted.remediation.nextSteps[0] = %q, want %q", got, want)
	}
	if got, want := drifted.Remediation.AllowedCommands[0], "sync render"; got != want {
		t.Fatalf("drifted.remediation.allowedCommands[0] = %q, want %q", got, want)
	}
	if got, want := drifted.Remediation.CommandGuidance[3].Command, "sync apply"; got != want {
		t.Fatalf("drifted.remediation.commandGuidance[3].command = %q, want %q", got, want)
	}
	if got, want := drifted.Remediation.CommandGuidance[3].Preconditions[0], "bundleMatchesRecordedDesiredStateDigest"; got != want {
		t.Fatalf("drifted.remediation.commandGuidance[3].preconditions[0] = %q, want %q", got, want)
	}
	if got, want := drifted.Remediation.CommandGuidance[3].Availability, "unknown"; got != want {
		t.Fatalf("drifted.remediation.commandGuidance[3].availability = %q, want %q", got, want)
	}
	if got, want := drifted.Remediation.SafeDirectRevert, false; got != want {
		t.Fatalf("drifted.remediation.safeDirectRevert = %t, want %t", got, want)
	}
	if got, want := drifted.Remediation.SafeCommit, false; got != want {
		t.Fatalf("drifted.remediation.safeCommit = %t, want %t", got, want)
	}
	if got, want := drifted.Mismatches[0].Reason, "imageMismatch"; got != want {
		t.Fatalf("drifted.mismatches[0].reason = %q, want %q", got, want)
	}
	if got, want := drifted.Mismatches[0].Desired, "registry.k8s.io/kube-controller-manager:v1.30.3"; got != want {
		t.Fatalf("drifted.mismatches[0].desired = %q, want %q", got, want)
	}
	if got, want := drifted.Mismatches[0].Live, "registry.k8s.io/kube-controller-manager:v1.30.4"; got != want {
		t.Fatalf("drifted.mismatches[0].live = %q, want %q", got, want)
	}

	schedulerDrifted := hostPaths["/etc/kubernetes/manifests/kube-scheduler.yaml"]
	if got, want := schedulerDrifted.Presence, ObjectPresencePresent; got != want {
		t.Fatalf("schedulerDrifted.presence = %q, want %q", got, want)
	}
	if got, want := schedulerDrifted.State, state.StateOrphan; got != want {
		t.Fatalf("schedulerDrifted.state = %q, want %q", got, want)
	}
	if got, want := schedulerDrifted.Mismatches[0].Reason, "missingExpectedFlag"; got != want {
		t.Fatalf("schedulerDrifted.mismatches[0].reason = %q, want %q", got, want)
	}
	if got, want := schedulerDrifted.Mismatches[0].Desired, "--bind-address=0.0.0.0"; got != want {
		t.Fatalf("schedulerDrifted.mismatches[0].desired = %q, want %q", got, want)
	}
	if got, want := schedulerDrifted.Mismatches[1].Reason, "missingExpectedVolumeMount"; got != want {
		t.Fatalf("schedulerDrifted.mismatches[1].reason = %q, want %q", got, want)
	}
	if got, want := schedulerDrifted.Mismatches[1].Desired, "/etc/localtime"; got != want {
		t.Fatalf("schedulerDrifted.mismatches[1].desired = %q, want %q", got, want)
	}
	if schedulerDrifted.Remediation == nil {
		t.Fatal("schedulerDrifted.remediation = nil, want remediation")
	}
	if got, want := schedulerDrifted.Remediation.Action, "updateLocalBootstrapInputAndRerender"; got != want {
		t.Fatalf("schedulerDrifted.remediation.action = %q, want %q", got, want)
	}
	if got, want := schedulerDrifted.Remediation.ChangeOwner, "localInput"; got != want {
		t.Fatalf("schedulerDrifted.remediation.changeOwner = %q, want %q", got, want)
	}
	assertGeneratedHostPathRemediation(
		t,
		schedulerDrifted.Remediation,
		generatedRemediationMetadata{
			ProjectionClass: "generatedHostPath",
			Generator:       "kubeadm",
			GeneratedKind:   "Pod",
			GeneratedName:   "kube-scheduler",
			Repairable:      true,
		},
	)
	if got, want := schedulerDrifted.Remediation.NextSteps[0], "Update the cluster-local bootstrap input that feeds the rendered kubeadm config."; got != want {
		t.Fatalf("schedulerDrifted.remediation.nextSteps[0] = %q, want %q", got, want)
	}
	if got, want := schedulerDrifted.Remediation.AllowedCommands[0], "sync render"; got != want {
		t.Fatalf("schedulerDrifted.remediation.allowedCommands[0] = %q, want %q", got, want)
	}
	if got, want := schedulerDrifted.Remediation.CommandGuidance[3].Availability, "unknown"; got != want {
		t.Fatalf(
			"schedulerDrifted.remediation.commandGuidance[3].availability = %q, want %q",
			got,
			want,
		)
	}

	missing := hostPaths["/etc/kubernetes/manifests/etcd.yaml"]
	if got, want := missing.Presence, ObjectPresenceMissing; got != want {
		t.Fatalf("missing.presence = %q, want %q", got, want)
	}
	if got, want := missing.State, state.StateOrphan; got != want {
		t.Fatalf("missing.state = %q, want %q", got, want)
	}
	if missing.Remediation == nil {
		t.Fatal("missing.remediation = nil, want remediation")
	}
	if got, want := missing.Remediation.Action, "reviewDistributionBaselineForGeneratedProjection"; got != want {
		t.Fatalf("missing.remediation.action = %q, want %q", got, want)
	}
	if got, want := missing.Remediation.ChangeOwner, "globalBaseline"; got != want {
		t.Fatalf("missing.remediation.changeOwner = %q, want %q", got, want)
	}
	assertGeneratedHostPathRemediation(t, missing.Remediation, generatedRemediationMetadata{
		ProjectionClass: "generatedHostPath",
		Generator:       "kubeadm",
		GeneratedKind:   "Pod",
		GeneratedName:   "etcd",
		Repairable:      false,
	})
	if got, want := missing.Remediation.AllowedCommands[4], "sync package build"; got != want {
		t.Fatalf("missing.remediation.allowedCommands[4] = %q, want %q", got, want)
	}
}

func TestCompareBundleWithGeneratedHostPathSemanticParseErrorUsesManualReviewRemediation(
	t *testing.T,
) {
	t.Parallel()

	bundleRoot := t.TempDir()
	hostRoot := t.TempDir()
	livePath := filepath.Join(hostRoot, "etc", "kubernetes", "manifests", "kube-apiserver.yaml")
	if err := os.MkdirAll(filepath.Dir(livePath), 0o755); err != nil {
		t.Fatalf("MkdirAll(%q) error = %v", filepath.Dir(livePath), err)
	}
	if err := os.WriteFile(livePath, []byte("not: [valid"), 0o644); err != nil {
		t.Fatalf("WriteFile(%q) error = %v", livePath, err)
	}

	bundle := &hydrate.Bundle{
		Spec: hydrate.BundleSpec{
			TrackedHostPaths: []hydrate.TrackedHostPath{{
				HostPath:        "/etc/kubernetes/manifests/kube-apiserver.yaml",
				Component:       "kubernetes",
				Source:          hydrate.InventorySourceGeneratedHook,
				Ownership:       hydrate.InventoryOwnershipGlobal,
				Type:            hydrate.HostPathRegularFile,
				ProjectionClass: hydrate.HostPathProjectionClassGenerated,
				CompareStrategy: hydrate.HostPathCompareStrategySemanticGenerated,
				Generated: &hydrate.GeneratedHostPathSemantics{
					Tool:          "kubeadm",
					Hook:          "bootstrap",
					APIVersion:    "v1",
					Kind:          "Pod",
					Namespace:     "kube-system",
					Name:          "kube-apiserver",
					ContainerName: "kube-apiserver",
					ExpectedImage: "registry.k8s.io/kube-apiserver:v1.30.3",
				},
			}},
		},
	}

	result, err := CompareBundleWithOptions(
		bundle,
		bundleRoot,
		resolverFunc(func(apiVersion, kind, namespace, name string) ([]byte, error) {
			return nil, NewNotFoundError(apiVersion, kind, namespace, name)
		}),
		CompareOptions{HostRoot: hostRoot},
	)
	if err != nil {
		t.Fatalf("CompareBundleWithOptions() error = %v", err)
	}
	if got, want := len(result.HostPaths), 1; got != want {
		t.Fatalf("len(hostPaths) = %d, want %d", got, want)
	}
	status := result.HostPaths[0]
	if got, want := status.Mismatches[0].Reason, "semanticParseError"; got != want {
		t.Fatalf("status.mismatches[0].reason = %q, want %q", got, want)
	}
	if status.Remediation == nil {
		t.Fatal("status.remediation = nil, want remediation")
	}
	if got, want := status.Remediation.Action, "manualReviewGeneratedProjection"; got != want {
		t.Fatalf("status.remediation.action = %q, want %q", got, want)
	}
	if got, want := status.Remediation.ChangeOwner, "manualReview"; got != want {
		t.Fatalf("status.remediation.changeOwner = %q, want %q", got, want)
	}
	assertGeneratedHostPathRemediation(t, status.Remediation, generatedRemediationMetadata{
		ProjectionClass: "generatedHostPath",
		Generator:       "kubeadm",
		GeneratedKind:   "Pod",
		GeneratedName:   "kube-apiserver",
		Repairable:      true,
	})
	if got, want := status.Remediation.NextSteps[0], "Inspect the live generated static Pod manifest and identify why Sealos could not classify it semantically."; got != want {
		t.Fatalf("status.remediation.nextSteps[0] = %q, want %q", got, want)
	}
	if got, want := status.Remediation.AllowedCommands[0], "sync diff"; got != want {
		t.Fatalf("status.remediation.allowedCommands[0] = %q, want %q", got, want)
	}
	if got, want := status.Remediation.CommandGuidance[3].Availability, "unknown"; got != want {
		t.Fatalf("status.remediation.commandGuidance[3].availability = %q, want %q", got, want)
	}
}

func TestKubectlResolverTranslatesNotFound(t *testing.T) {
	t.Parallel()

	resolver := NewKubectlResolver(func(args ...string) ([]byte, error) {
		return nil, fmt.Errorf(
			"Error from server (NotFound): secrets %q not found",
			"grafana-admin-credentials",
		)
	})

	if _, err := resolver.Get("v1", "Secret", "default", "grafana-admin-credentials"); err == nil ||
		!isNotFound(err) {
		t.Fatalf("resolver.Get() error = %v, want notFound", err)
	}
}

func TestNormalizedObjectDigestTreatsStringDataAndServerMetadataAsEqual(t *testing.T) {
	t.Parallel()

	desired := []byte(
		"apiVersion: v1\nkind: Secret\nmetadata:\n  name: grafana-admin-credentials\n  namespace: default\nstringData:\n  username: admin\n  password: passw0rd\n",
	)
	live := []byte(
		`{"apiVersion":"v1","kind":"Secret","metadata":{"name":"grafana-admin-credentials","namespace":"default","resourceVersion":"1","uid":"abc","managedFields":[{}],"annotations":{"kubectl.kubernetes.io/last-applied-configuration":"{}"}},"data":{"username":"YWRtaW4=","password":"cGFzc3cwcmQ="}}`,
	)

	desiredDigest, err := normalizedObjectDigest(desired)
	if err != nil {
		t.Fatalf("normalizedObjectDigest(desired) error = %v", err)
	}
	liveDigest, err := normalizedObjectDigest(live)
	if err != nil {
		t.Fatalf("normalizedObjectDigest(live) error = %v", err)
	}
	if got, want := liveDigest, desiredDigest; got != want {
		t.Fatalf("live digest = %q, want %q", got, want)
	}
}

func TestMatchesOwnedFieldsAllowsExtraLiveMapFields(t *testing.T) {
	t.Parallel()

	desired := []byte(`
apiVersion: apps/v1
kind: Deployment
metadata:
  name: grafana
  namespace: default
spec:
  template:
    spec:
      containers:
        - name: grafana
          image: grafana/grafana:10.4.0
`)
	live := []byte(`{
  "apiVersion":"apps/v1",
  "kind":"Deployment",
  "metadata":{
    "name":"grafana",
    "namespace":"default",
    "resourceVersion":"3",
    "labels":{"app":"grafana","pod-template-hash":"abc"}
  },
  "spec":{
    "strategy":{"type":"RollingUpdate"},
    "template":{
      "metadata":{"labels":{"app":"grafana","pod-template-hash":"abc"}},
      "spec":{
        "serviceAccountName":"grafana",
        "containers":[
          {"name":"grafana","image":"grafana/grafana:10.4.0","ports":[{"containerPort":3000}]}
        ]
      }
    }
  },
  "status":{"readyReplicas":1}
}`)

	matched, err := matchesOwnedFields(desired, live)
	if err != nil {
		t.Fatalf("matchesOwnedFields() error = %v", err)
	}
	if !matched {
		t.Fatal("matchesOwnedFields() = false, want true")
	}
}

func TestMatchesOwnedFieldsDetectsChangedOwnedField(t *testing.T) {
	t.Parallel()

	desired := []byte(`
apiVersion: apps/v1
kind: DaemonSet
metadata:
  name: cilium
  namespace: kube-system
spec:
  template:
    spec:
      containers:
        - name: cilium-agent
          image: quay.io/cilium/cilium:v1.15.0
`)
	live := []byte(`{
  "apiVersion":"apps/v1",
  "kind":"DaemonSet",
  "metadata":{"name":"cilium","namespace":"kube-system"},
  "spec":{
    "template":{
      "spec":{
        "containers":[
          {"name":"cilium-agent","image":"quay.io/cilium/cilium:v1.15.1"}
        ]
      }
    }
  }
}`)

	matched, err := matchesOwnedFields(desired, live)
	if err != nil {
		t.Fatalf("matchesOwnedFields() error = %v", err)
	}
	if matched {
		t.Fatal("matchesOwnedFields() = true, want false")
	}
}

func TestMatchesOwnedFieldsAllowsMergeKeyListsOutOfOrder(t *testing.T) {
	t.Parallel()

	desired := []byte(`
apiVersion: apps/v1
kind: Deployment
metadata:
  name: grafana
  namespace: default
spec:
  template:
    spec:
      volumes:
        - name: config
      containers:
        - name: grafana
          image: grafana/grafana:10.4.0
          env:
            - name: GF_SECURITY_ADMIN_USER
              value: admin
          ports:
            - containerPort: 3000
          volumeMounts:
            - name: config
              mountPath: /etc/grafana
`)
	live := []byte(`{
  "apiVersion":"apps/v1",
  "kind":"Deployment",
  "metadata":{"name":"grafana","namespace":"default"},
  "spec":{
    "template":{
      "spec":{
        "volumes":[
          {"name":"scratch"},
          {"name":"config"}
        ],
        "containers":[
          {
            "name":"sidecar",
            "image":"busybox:1.36"
          },
          {
            "name":"grafana",
            "image":"grafana/grafana:10.4.0",
            "env":[
              {"name":"GF_SECURITY_ADMIN_PASSWORD","value":"secret"},
              {"name":"GF_SECURITY_ADMIN_USER","value":"admin"}
            ],
            "ports":[
              {"containerPort":9090},
              {"containerPort":3000}
            ],
            "volumeMounts":[
              {"name":"scratch","mountPath":"/tmp"},
              {"name":"config","mountPath":"/etc/grafana"}
            ]
          }
        ]
      }
    }
  }
}`)

	matched, err := matchesOwnedFields(desired, live)
	if err != nil {
		t.Fatalf("matchesOwnedFields() error = %v", err)
	}
	if !matched {
		t.Fatal("matchesOwnedFields() = false, want true")
	}
}

func TestMatchesOwnedFieldsDetectsChangedMergeKeyListItem(t *testing.T) {
	t.Parallel()

	desired := []byte(`
apiVersion: apps/v1
kind: Deployment
metadata:
  name: grafana
  namespace: default
spec:
  template:
    spec:
      containers:
        - name: grafana
          env:
            - name: GF_SECURITY_ADMIN_USER
              value: admin
`)
	live := []byte(`{
  "apiVersion":"apps/v1",
  "kind":"Deployment",
  "metadata":{"name":"grafana","namespace":"default"},
  "spec":{
    "template":{
      "spec":{
        "containers":[
          {
            "name":"grafana",
            "env":[
              {"name":"GF_SECURITY_ADMIN_USER","value":"root"}
            ]
          }
        ]
      }
    }
  }
}`)

	matched, err := matchesOwnedFields(desired, live)
	if err != nil {
		t.Fatalf("matchesOwnedFields() error = %v", err)
	}
	if matched {
		t.Fatal("matchesOwnedFields() = true, want false")
	}
}

func TestOwnedFieldMismatchesReportsMergeKeyPath(t *testing.T) {
	t.Parallel()

	desired := []byte(`
apiVersion: apps/v1
kind: Deployment
metadata:
  name: grafana
  namespace: default
spec:
  template:
    spec:
      containers:
        - name: grafana
          env:
            - name: GF_SECURITY_ADMIN_USER
              value: admin
`)
	live := []byte(`{
  "apiVersion":"apps/v1",
  "kind":"Deployment",
  "metadata":{"name":"grafana","namespace":"default"},
  "spec":{
    "template":{
      "spec":{
        "containers":[
          {
            "name":"grafana",
            "env":[
              {"name":"GF_SECURITY_ADMIN_USER","value":"root"}
            ]
          }
        ]
      }
    }
  }
}`)

	mismatches, err := ownedFieldMismatches(desired, live)
	if err != nil {
		t.Fatalf("ownedFieldMismatches() error = %v", err)
	}
	if got, want := len(mismatches), 1; got != want {
		t.Fatalf("len(mismatches) = %d, want %d", got, want)
	}
	if got, want := mismatches[0].Path, "spec.template.spec.containers[name=grafana].env[name=GF_SECURITY_ADMIN_USER].value"; got != want {
		t.Fatalf("mismatches[0].path = %q, want %q", got, want)
	}
	if got, want := mismatches[0].Reason, "valueMismatch"; got != want {
		t.Fatalf("mismatches[0].reason = %q, want %q", got, want)
	}
	if got, want := mismatches[0].Ownership, hydrate.InventoryOwnership(""); got != want {
		t.Fatalf("mismatches[0].ownership = %q, want empty before annotation", got)
	}
}

func TestCompareBundleRejectsMissingDesiredObject(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	bundle := &hydrate.Bundle{
		Spec: hydrate.BundleSpec{
			TrackedK8sObjects: []hydrate.TrackedK8sObject{
				{
					APIVersion: "v1",
					Kind:       "Secret",
					Namespace:  "default",
					Name:       "grafana-admin-credentials",
					Path:       "local-resources/missing.yaml",
				},
			},
		},
	}
	if _, err := CompareBundle(bundle, root, resolverFunc(func(apiVersion, kind, namespace, name string) ([]byte, error) {
		return nil, NewNotFoundError(apiVersion, kind, namespace, name)
	})); err == nil {
		t.Fatal("CompareBundle() error = nil, want error")
	}
}

func TestCompareBundleLoadsBundleManifestPaths(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	bundle := &hydrate.Bundle{
		APIVersion: "distribution.sealos.io/v1alpha1",
		Kind:       "HydratedBundle",
		Spec: hydrate.BundleSpec{
			BOMName:  "minimal-single-node",
			Revision: "rev-poc-001",
			TrackedK8sObjects: []hydrate.TrackedK8sObject{
				{
					APIVersion: "apps/v1",
					Kind:       "DaemonSet",
					Namespace:  "kube-system",
					Name:       "cilium",
					Path:       "components/cilium/files/manifests/cilium.yaml",
				},
			},
			Components: []hydrate.RenderedComponent{{Name: "cilium"}},
		},
	}
	manifest := filepath.Join(root, "components", "cilium", "files", "manifests", "cilium.yaml")
	if err := os.MkdirAll(filepath.Dir(manifest), 0o755); err != nil {
		t.Fatalf("MkdirAll() error = %v", err)
	}
	if err := os.WriteFile(manifest, []byte("apiVersion: apps/v1\nkind: DaemonSet\nmetadata:\n  name: cilium\n  namespace: kube-system\n"), 0o644); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}
	if err := yamlutil.MarshalFile(filepath.Join(root, hydrate.BundleFileName), bundle); err != nil {
		t.Fatalf("MarshalFile(bundle) error = %v", err)
	}

	loaded, err := CompareBundle(
		bundle,
		root,
		resolverFunc(func(apiVersion, kind, namespace, name string) ([]byte, error) {
			return []byte(
				`{"apiVersion":"apps/v1","kind":"DaemonSet","metadata":{"name":"cilium","namespace":"kube-system"}}`,
			), nil
		}),
	)
	if err != nil {
		t.Fatalf("CompareBundle() error = %v", err)
	}
	if got, want := loaded.Summary.Matched, 1; got != want {
		t.Fatalf("summary.matched = %d, want %d", got, want)
	}
}

func TestCompareBundleUsesFieldLevelOwnershipAcrossFragments(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	globalPath := filepath.Join(
		root,
		"components",
		"grafana",
		"files",
		"manifests",
		"grafana-settings.yaml",
	)
	if err := os.MkdirAll(filepath.Dir(globalPath), 0o755); err != nil {
		t.Fatalf("MkdirAll(globalPath) error = %v", err)
	}
	if err := os.WriteFile(globalPath, []byte(`
apiVersion: v1
kind: ConfigMap
metadata:
  name: grafana-settings
  namespace: default
data:
  adminUser: admin
  imageTag: "10.4.0"
`), 0o644); err != nil {
		t.Fatalf("WriteFile(globalPath) error = %v", err)
	}

	localPath := filepath.Join(root, "local-resources", "grafana-settings-local.yaml")
	if err := os.MkdirAll(filepath.Dir(localPath), 0o755); err != nil {
		t.Fatalf("MkdirAll(localPath) error = %v", err)
	}
	if err := os.WriteFile(localPath, []byte(`
apiVersion: v1
kind: ConfigMap
metadata:
  name: grafana-settings
  namespace: default
data:
  adminUser: root
`), 0o644); err != nil {
		t.Fatalf("WriteFile(localPath) error = %v", err)
	}

	bundle := &hydrate.Bundle{
		Spec: hydrate.BundleSpec{
			TrackedK8sObjects: []hydrate.TrackedK8sObject{
				{
					APIVersion: "v1",
					Kind:       "ConfigMap",
					Namespace:  "default",
					Name:       "grafana-settings",
					Path:       "components/grafana/files/manifests/grafana-settings.yaml",
					Component:  "grafana",
					Source:     hydrate.InventorySourcePackageManifest,
					Ownership:  hydrate.InventoryOwnershipGlobal,
				},
				{
					APIVersion: "v1",
					Kind:       "ConfigMap",
					Namespace:  "default",
					Name:       "grafana-settings",
					Path:       "local-resources/grafana-settings-local.yaml",
					Source:     hydrate.InventorySourceLocalResource,
					Ownership:  hydrate.InventoryOwnershipLocal,
				},
			},
		},
	}

	result, err := CompareBundle(
		bundle,
		root,
		resolverFunc(func(apiVersion, kind, namespace, name string) ([]byte, error) {
			return []byte(`{
  "apiVersion":"v1",
  "kind":"ConfigMap",
  "metadata":{"name":"grafana-settings","namespace":"default"},
  "data":{"adminUser":"admin","imageTag":"10.4.0"}
}`), nil
		}),
	)
	if err != nil {
		t.Fatalf("CompareBundle() error = %v", err)
	}
	if got, want := result.Summary.Total, 1; got != want {
		t.Fatalf("summary.total = %d, want %d", got, want)
	}
	if got, want := result.Summary.Dirty, 1; got != want {
		t.Fatalf("summary.dirty = %d, want %d", got, want)
	}
	if got, want := result.Summary.Orphan, 0; got != want {
		t.Fatalf("summary.orphan = %d, want %d", got, want)
	}
	if got, want := len(result.Objects), 1; got != want {
		t.Fatalf("len(objects) = %d, want %d", got, want)
	}
	object := result.Objects[0]
	if got, want := len(object.Fragments), 2; got != want {
		t.Fatalf("len(object.fragments) = %d, want %d", got, want)
	}
	if got, want := object.State, state.StateDirty; got != want {
		t.Fatalf("object.state = %q, want %q", got, want)
	}
	if got, want := len(object.Mismatches), 1; got != want {
		t.Fatalf("len(object.mismatches) = %d, want %d", got, want)
	}
	if got, want := object.Mismatches[0].Path, "data.adminUser"; got != want {
		t.Fatalf("object.mismatches[0].path = %q, want %q", got, want)
	}
	if got, want := object.Mismatches[0].Ownership, hydrate.InventoryOwnershipLocal; got != want {
		t.Fatalf("object.mismatches[0].ownership = %q, want %q", got, want)
	}
	if got, want := object.Mismatches[0].State, state.StateDirty; got != want {
		t.Fatalf("object.mismatches[0].state = %q, want %q", got, want)
	}
}

type resolverFunc func(apiVersion, kind, namespace, name string) ([]byte, error)

func (f resolverFunc) Get(apiVersion, kind, namespace, name string) ([]byte, error) {
	return f(apiVersion, kind, namespace, name)
}

type generatedRemediationMetadata struct {
	ProjectionClass string
	Generator       string
	GeneratedKind   string
	GeneratedName   string
	Repairable      bool
}

func assertGeneratedHostPathRemediation(
	t *testing.T,
	remediation *HostPathRemediation,
	want generatedRemediationMetadata,
) {
	t.Helper()

	if remediation == nil {
		t.Fatal("remediation = nil, want generated host path remediation")
	}
	if got := remediation.ProjectionClass; got != want.ProjectionClass {
		t.Fatalf("remediation.projectionClass = %q, want %q", got, want.ProjectionClass)
	}
	if got := remediation.Generator; got != want.Generator {
		t.Fatalf("remediation.generator = %q, want %q", got, want.Generator)
	}
	if got := remediation.GeneratedKind; got != want.GeneratedKind {
		t.Fatalf("remediation.generatedKind = %q, want %q", got, want.GeneratedKind)
	}
	if got := remediation.GeneratedName; got != want.GeneratedName {
		t.Fatalf("remediation.generatedName = %q, want %q", got, want.GeneratedName)
	}
	if remediation.Repairable == nil {
		t.Fatal("remediation.repairable = nil, want explicit generated repairability")
	}
	if got := *remediation.Repairable; got != want.Repairable {
		t.Fatalf("remediation.repairable = %t, want %t", got, want.Repairable)
	}
}
