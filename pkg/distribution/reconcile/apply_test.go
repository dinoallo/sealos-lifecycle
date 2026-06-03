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

package reconcile

import (
	"bytes"
	"context"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/labring/sealos/pkg/constants"
	"github.com/labring/sealos/pkg/distribution"
	"github.com/labring/sealos/pkg/distribution/bom"
	"github.com/labring/sealos/pkg/distribution/hydrate"
	"github.com/labring/sealos/pkg/distribution/packageformat"
	"github.com/labring/sealos/pkg/distribution/state"
	v1beta1 "github.com/labring/sealos/pkg/types/v1beta1"
	yamlutil "github.com/labring/sealos/pkg/utils/yaml"
)

func TestApplyPreparedHostBundle(t *testing.T) {
	previousRuntimeRoot := constants.DefaultRuntimeRootDir
	constants.DefaultRuntimeRootDir = t.TempDir()
	t.Cleanup(func() {
		constants.DefaultRuntimeRootDir = previousRuntimeRoot
	})

	tmpDir := t.TempDir()
	bundleDir := filepath.Join(tmpDir, "bundle")
	hostRoot := filepath.Join(tmpDir, "host")
	logPath := filepath.Join(tmpDir, "apply.log")
	kubeconfigPath := filepath.Join(tmpDir, "admin.conf")
	if err := os.WriteFile(kubeconfigPath, []byte("apiVersion: v1\nkind: Config\n"), 0o644); err != nil {
		t.Fatalf("WriteFile(kubeconfig) error = %v", err)
	}

	writeExecutable(t, filepath.Join(tmpDir, "bin", "kubectl"), `#!/bin/sh
echo "kubectl $*" >>"$TEST_LOG"
if [ "$1" = "--kubeconfig" ]; then
  shift 2
fi
case "$1" in
  get)
    if [ "$2" = "--raw=/readyz" ]; then
      exit 0
    fi
    if [ "$2" = "nodes" ]; then
      printf 'node-a'
      exit 0
    fi
    ;;
  taint)
    exit 0
    ;;
  apply)
    exit 0
    ;;
esac
exit 0
`)
	writeExecutable(t, filepath.Join(tmpDir, "bin", "systemctl"), `#!/bin/sh
echo "systemctl $*" >>"$TEST_LOG"
exit 0
`)
	writeExecutable(t, filepath.Join(tmpDir, "bin", "sysctl"), `#!/bin/sh
echo "sysctl $*" >>"$TEST_LOG"
exit 0
`)

	t.Setenv("PATH", filepath.Join(tmpDir, "bin")+":"+os.Getenv("PATH"))
	t.Setenv("TEST_LOG", logPath)

	writeExecutable(t, filepath.Join(bundleDir, "components", "containerd", "files", "hooks", "preflight.sh"), `#!/bin/sh
[ ! -f "$HOST_ROOT/usr/bin/containerd" ] || exit 1
echo "containerd-preflight" >>"$TEST_LOG"
`)
	writeExecutable(t, filepath.Join(bundleDir, "components", "containerd", "files", "hooks", "bootstrap.sh"), `#!/bin/sh
echo "containerd-bootstrap" >>"$TEST_LOG"
`)
	writeExecutable(t, filepath.Join(bundleDir, "components", "containerd", "files", "hooks", "healthcheck.sh"), `#!/bin/sh
echo "containerd-health" >>"$TEST_LOG"
`)
	writeExecutable(t, filepath.Join(bundleDir, "components", "kubernetes", "files", "hooks", "preflight.sh"), `#!/bin/sh
[ -f "$HOST_ROOT/usr/bin/kubelet" ] || exit 1
[ -f "$HOST_ROOT/etc/kubernetes/kubeadm.yaml" ] || exit 1
echo "kubernetes-preflight" >>"$TEST_LOG"
`)
	writeExecutable(t, filepath.Join(bundleDir, "components", "kubernetes", "files", "hooks", "bootstrap.sh"), `#!/bin/sh
echo "kubernetes-bootstrap" >>"$TEST_LOG"
`)
	writeExecutable(t, filepath.Join(bundleDir, "components", "kubernetes", "files", "hooks", "healthcheck.sh"), `#!/bin/sh
echo "kubernetes-healthcheck" >>"$TEST_LOG"
`)
	writeExecutable(t, filepath.Join(bundleDir, "components", "cilium", "files", "hooks", "healthcheck.sh"), `#!/bin/sh
echo "cilium-health" >>"$TEST_LOG"
`)

	for _, rel := range []string{
		filepath.Join("usr", "bin", "containerd"),
		filepath.Join("usr", "bin", "ctr"),
		filepath.Join("usr", "bin", "containerd-shim-runc-v2"),
		filepath.Join("usr", "bin", "runc"),
	} {
		writeFile(t, filepath.Join(bundleDir, "components", "containerd", "files", "rootfs", rel), "#!/bin/sh\nexit 0\n", 0o755)
	}
	writeFile(t, filepath.Join(bundleDir, "components", "containerd", "files", "files", "etc", "containerd", "config.toml"), "version = 2\n", 0o644)
	for _, rel := range []string{
		filepath.Join("usr", "bin", "kubeadm"),
		filepath.Join("usr", "bin", "kubelet"),
		filepath.Join("usr", "bin", "kubectl"),
	} {
		writeFile(t, filepath.Join(bundleDir, "components", "kubernetes", "files", "rootfs", rel), "#!/bin/sh\nexit 0\n", 0o755)
	}
	writeFile(t, filepath.Join(bundleDir, "components", "kubernetes", "files", "files", "etc", "kubernetes", "kubeadm.yaml"), "apiVersion: kubeadm.k8s.io/v1beta3\n", 0o644)
	writeFile(t, filepath.Join(bundleDir, "components", "kubernetes", "files", "files", "etc", "sysctl.d", "99-kubernetes.conf"), "net.ipv4.ip_forward = 1\n", 0o644)
	writeFile(t, filepath.Join(bundleDir, "components", "kubernetes", "files", "manifests", "bootstrap", "rbac.yaml"), "apiVersion: v1\nkind: Namespace\nmetadata:\n  name: kube-system\n", 0o644)
	writeFile(t, filepath.Join(bundleDir, "components", "cilium", "files", "manifests", "cilium.yaml"), "apiVersion: apps/v1\nkind: DaemonSet\nmetadata:\n  name: cilium\n---\napiVersion: apps/v1\nkind: Deployment\nmetadata:\n  name: cilium-operator\n", 0o644)
	writeFile(t, filepath.Join(bundleDir, "local-resources", "grafana-admin-secret.yaml"), "apiVersion: v1\nkind: Secret\nmetadata:\n  name: grafana-admin-credentials\n  namespace: default\nstringData:\n  username: admin\n  password: passw0rd\n", 0o644)

	bundle := &hydrate.Bundle{
		APIVersion: distribution.APIVersion,
		Kind:       distribution.KindHydratedBundle,
		Spec: hydrate.BundleSpec{
			BOMName:        "minimal-single-node",
			Revision:       "rev-1",
			Channel:        bom.ChannelAlpha,
			LocalResources: []string{"local-resources/grafana-admin-secret.yaml"},
			Components: []hydrate.RenderedComponent{
				{
					Name:        "containerd",
					PackageName: "containerd-runtime",
					Version:     "v1.7.18",
					Class:       packageformat.ClassRootfs,
					RootPath:    "components/containerd/files",
					Steps: []hydrate.RenderedStep{
						{
							Name:        "runtime-rootfs",
							Kind:        hydrate.StepContent,
							BundlePath:  "components/containerd/files/rootfs",
							SourcePath:  "rootfs/",
							ContentType: packageformat.ContentRootfs,
						},
						{
							Name:        "runtime-config",
							Kind:        hydrate.StepContent,
							BundlePath:  "components/containerd/files/files/etc/containerd/config.toml",
							SourcePath:  "files/etc/containerd/config.toml",
							ContentType: packageformat.ContentFile,
						},
						{
							Name:           "preflight",
							Kind:           hydrate.StepHook,
							BundlePath:     "components/containerd/files/hooks/preflight.sh",
							SourcePath:     "hooks/preflight.sh",
							HookPhase:      packageformat.PhaseBootstrap,
							Target:         packageformat.TargetAllNodes,
							TimeoutSeconds: 5,
						},
						{
							Name:           "bootstrap",
							Kind:           hydrate.StepHook,
							BundlePath:     "components/containerd/files/hooks/bootstrap.sh",
							SourcePath:     "hooks/bootstrap.sh",
							HookPhase:      packageformat.PhaseBootstrap,
							Target:         packageformat.TargetAllNodes,
							TimeoutSeconds: 5,
						},
						{
							Name:           "healthcheck",
							Kind:           hydrate.StepHook,
							BundlePath:     "components/containerd/files/hooks/healthcheck.sh",
							SourcePath:     "hooks/healthcheck.sh",
							HookPhase:      packageformat.PhaseHealth,
							Target:         packageformat.TargetAllNodes,
							TimeoutSeconds: 5,
						},
					},
				},
				{
					Name:         "kubernetes",
					PackageName:  "kubernetes-rootfs",
					Version:      "v1.30.3",
					Class:        packageformat.ClassRootfs,
					Dependencies: []string{"containerd"},
					RootPath:     "components/kubernetes/files",
					Steps: []hydrate.RenderedStep{
						{
							Name:        "kubernetes-rootfs",
							Kind:        hydrate.StepContent,
							BundlePath:  "components/kubernetes/files/rootfs",
							SourcePath:  "rootfs/",
							ContentType: packageformat.ContentRootfs,
						},
						{
							Name:        "kubeadm-defaults",
							Kind:        hydrate.StepContent,
							BundlePath:  "components/kubernetes/files/files/etc/kubernetes/kubeadm.yaml",
							SourcePath:  "files/etc/kubernetes/kubeadm.yaml",
							ContentType: packageformat.ContentFile,
						},
						{
							Name:        "sysctl-profile",
							Kind:        hydrate.StepContent,
							BundlePath:  "components/kubernetes/files/files/etc/sysctl.d/99-kubernetes.conf",
							SourcePath:  "files/etc/sysctl.d/99-kubernetes.conf",
							ContentType: packageformat.ContentFile,
						},
						{
							Name:        "bootstrap-manifests",
							Kind:        hydrate.StepContent,
							BundlePath:  "components/kubernetes/files/manifests/bootstrap",
							SourcePath:  "manifests/bootstrap/",
							ContentType: packageformat.ContentManifest,
						},
						{
							Name:           "preflight",
							Kind:           hydrate.StepHook,
							BundlePath:     "components/kubernetes/files/hooks/preflight.sh",
							SourcePath:     "hooks/preflight.sh",
							HookPhase:      packageformat.PhaseBootstrap,
							Target:         packageformat.TargetFirstMaster,
							TimeoutSeconds: 5,
						},
						{
							Name:           "bootstrap",
							Kind:           hydrate.StepHook,
							BundlePath:     "components/kubernetes/files/hooks/bootstrap.sh",
							SourcePath:     "hooks/bootstrap.sh",
							HookPhase:      packageformat.PhaseBootstrap,
							Target:         packageformat.TargetAllNodes,
							TimeoutSeconds: 5,
						},
						{
							Name:           "healthcheck",
							Kind:           hydrate.StepHook,
							BundlePath:     "components/kubernetes/files/hooks/healthcheck.sh",
							SourcePath:     "hooks/healthcheck.sh",
							HookPhase:      packageformat.PhaseHealth,
							Target:         packageformat.TargetCluster,
							TimeoutSeconds: 5,
						},
					},
				},
				{
					Name:        "cilium",
					PackageName: "cilium-cni",
					Version:     "v1.15.0",
					Class:       packageformat.ClassApplication,
					RootPath:    "components/cilium/files",
					Steps: []hydrate.RenderedStep{
						{
							Name:        "cilium-manifest",
							Kind:        hydrate.StepContent,
							BundlePath:  "components/cilium/files/manifests/cilium.yaml",
							SourcePath:  "manifests/cilium.yaml",
							ContentType: packageformat.ContentManifest,
						},
						{
							Name:           "healthcheck",
							Kind:           hydrate.StepHook,
							BundlePath:     "components/cilium/files/hooks/healthcheck.sh",
							SourcePath:     "hooks/healthcheck.sh",
							HookPhase:      packageformat.PhaseHealth,
							Target:         packageformat.TargetCluster,
							TimeoutSeconds: 5,
						},
					},
				},
			},
		},
	}
	if err := yamlutil.MarshalFile(filepath.Join(bundleDir, hydrate.BundleFileName), bundle); err != nil {
		t.Fatalf("MarshalFile(bundle) error = %v", err)
	}

	bundleDigest, err := hydrate.DigestBundle(bundleDir)
	if err != nil {
		t.Fatalf("DigestBundle() error = %v", err)
	}
	if _, err := state.PersistRenderedState(
		"cluster-a",
		state.BOMReference{
			Name:     bundle.Spec.BOMName,
			Revision: bundle.Spec.Revision,
			Channel:  bundle.Spec.Channel,
			Digest:   "sha256:1111111111111111111111111111111111111111111111111111111111111111",
		},
		bundleDigest.String(),
		"",
		"local-rev-1",
	); err != nil {
		t.Fatalf("PersistRenderedState() error = %v", err)
	}

	stderr := bytes.NewBuffer(nil)
	result, err := Apply(ApplyOptions{
		ClusterName:    "cluster-a",
		BundlePath:     bundleDir,
		KubeconfigPath: kubeconfigPath,
		HostRoot:       hostRoot,
		Stderr:         stderr,
	})
	if err != nil {
		t.Fatalf("Apply() error = %v\nstderr=%s", err, stderr.String())
	}
	if got, want := result.DesiredStateDigest, bundleDigest.String(); got != want {
		t.Fatalf("result.DesiredStateDigest = %q, want %q", got, want)
	}
	if result.AppliedRevision == nil {
		t.Fatal("result.AppliedRevision = nil, want value")
	}

	for _, path := range []string{
		filepath.Join(hostRoot, "usr", "bin", "containerd"),
		filepath.Join(hostRoot, "etc", "containerd", "config.toml"),
		filepath.Join(hostRoot, "usr", "bin", "kubelet"),
		filepath.Join(hostRoot, "etc", "kubernetes", "kubeadm.yaml"),
		filepath.Join(hostRoot, "etc", "sysctl.d", "99-kubernetes.conf"),
	} {
		if _, err := os.Stat(path); err != nil {
			t.Fatalf("Stat(%q) error = %v", path, err)
		}
	}

	doc, err := state.LoadAppliedRevision("cluster-a")
	if err != nil {
		t.Fatalf("LoadAppliedRevision() error = %v", err)
	}
	if got, want := doc.Status.State, state.StateClean; got != want {
		t.Fatalf("status.state = %q, want %q", got, want)
	}
	if doc.Status.LastSuccessfulRevision == nil {
		t.Fatal("status.lastSuccessfulRevision = nil, want value")
	}

	data, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatalf("ReadFile(log) error = %v", err)
	}
	logText := string(data)
	for _, marker := range []string{
		"containerd-preflight",
		"systemctl is-active --quiet containerd",
		"systemctl stop containerd",
		"containerd-bootstrap",
		"containerd-health",
		"systemctl is-active --quiet kubelet",
		"systemctl stop kubelet",
		"sysctl --system",
		"systemctl daemon-reload",
		"kubernetes-preflight",
		"kubernetes-bootstrap",
		"kubectl --kubeconfig " + kubeconfigPath + " get --raw=/readyz",
		"kubectl --kubeconfig " + kubeconfigPath + " taint nodes node-a node-role.kubernetes.io/control-plane-",
		"kubectl --kubeconfig " + kubeconfigPath + " apply -f " + filepath.Join(bundleDir, "local-resources", "grafana-admin-secret.yaml"),
		"kubectl --kubeconfig " + kubeconfigPath + " apply -f " + filepath.Join(bundleDir, "components", "kubernetes", "files", "manifests", "bootstrap"),
		"kubernetes-healthcheck",
		"kubectl --kubeconfig " + kubeconfigPath + " apply -f " + filepath.Join(bundleDir, "components", "cilium", "files", "manifests", "cilium.yaml"),
		"cilium-health",
	} {
		if !strings.Contains(logText, marker) {
			t.Fatalf("log missing %q\nfull log:\n%s", marker, logText)
		}
	}
	if indexOf(logText, "containerd-preflight") > indexOf(logText, "systemctl stop containerd") {
		t.Fatalf("containerd preflight ran after host mutation started\nfull log:\n%s", logText)
	}
	if indexOf(logText, "kubernetes-preflight") > indexOf(logText, "kubernetes-bootstrap") {
		t.Fatalf("preflight ran after bootstrap\nfull log:\n%s", logText)
	}
	if indexOf(logText, "kubectl --kubeconfig "+kubeconfigPath+" apply -f "+filepath.Join(bundleDir, "local-resources", "grafana-admin-secret.yaml")) > indexOf(logText, "kubectl --kubeconfig "+kubeconfigPath+" apply -f "+filepath.Join(bundleDir, "components", "kubernetes", "files", "manifests", "bootstrap")) {
		t.Fatalf("local resource apply happened after bootstrap manifest apply\nfull log:\n%s", logText)
	}
	if indexOf(logText, "kubernetes-bootstrap") > indexOf(logText, "kubectl --kubeconfig "+kubeconfigPath+" apply -f "+filepath.Join(bundleDir, "components", "kubernetes", "files", "manifests", "bootstrap")) {
		t.Fatalf("bootstrap manifest apply happened before bootstrap hook\nfull log:\n%s", logText)
	}
	if indexOf(logText, "kubectl --kubeconfig "+kubeconfigPath+" apply -f "+filepath.Join(bundleDir, "components", "kubernetes", "files", "manifests", "bootstrap")) > indexOf(logText, "kubernetes-healthcheck") {
		t.Fatalf("kubernetes healthcheck ran before manifest apply\nfull log:\n%s", logText)
	}
}

func TestApplyRequiresRenderedState(t *testing.T) {
	previousRuntimeRoot := constants.DefaultRuntimeRootDir
	constants.DefaultRuntimeRootDir = t.TempDir()
	t.Cleanup(func() {
		constants.DefaultRuntimeRootDir = previousRuntimeRoot
	})

	bundleDir := t.TempDir()
	bundle := &hydrate.Bundle{
		APIVersion: distribution.APIVersion,
		Kind:       distribution.KindHydratedBundle,
		Spec: hydrate.BundleSpec{
			BOMName:  "minimal-single-node",
			Revision: "rev-1",
			Channel:  bom.ChannelAlpha,
			Components: []hydrate.RenderedComponent{
				{
					Name:        "cilium",
					PackageName: "cilium-cni",
					Version:     "v1.15.0",
					Class:       packageformat.ClassApplication,
					RootPath:    "components/cilium/files",
					Steps: []hydrate.RenderedStep{
						{
							Name:        "cilium-manifest",
							Kind:        hydrate.StepContent,
							BundlePath:  "components/cilium/files/manifests/cilium.yaml",
							SourcePath:  "manifests/cilium.yaml",
							ContentType: packageformat.ContentManifest,
						},
					},
				},
			},
		},
	}
	writeFile(t, filepath.Join(bundleDir, "components", "cilium", "files", "manifests", "cilium.yaml"), "apiVersion: apps/v1\nkind: DaemonSet\nmetadata:\n  name: cilium\n---\napiVersion: apps/v1\nkind: Deployment\nmetadata:\n  name: cilium-operator\n", 0o644)
	if err := yamlutil.MarshalFile(filepath.Join(bundleDir, hydrate.BundleFileName), bundle); err != nil {
		t.Fatalf("MarshalFile(bundle) error = %v", err)
	}

	_, err := Apply(ApplyOptions{
		ClusterName: "cluster-a",
		BundlePath:  bundleDir,
		HostRoot:    t.TempDir(),
	})
	if err == nil || !strings.Contains(err.Error(), "run sync render first") {
		t.Fatalf("Apply() error = %v, want missing rendered state error", err)
	}
}

func TestApplySupportsClusterApplicationOnMultiNodeCluster(t *testing.T) {
	previousRuntimeRoot := constants.DefaultRuntimeRootDir
	constants.DefaultRuntimeRootDir = t.TempDir()
	t.Cleanup(func() {
		constants.DefaultRuntimeRootDir = previousRuntimeRoot
	})

	tmpDir := t.TempDir()
	bundleDir := filepath.Join(tmpDir, "bundle")
	logPath := filepath.Join(tmpDir, "apply.log")
	kubeconfigPath := filepath.Join(tmpDir, "admin.conf")
	if err := os.WriteFile(kubeconfigPath, []byte("apiVersion: v1\nkind: Config\n"), 0o644); err != nil {
		t.Fatalf("WriteFile(kubeconfig) error = %v", err)
	}

	writeExecutable(t, filepath.Join(tmpDir, "bin", "kubectl"), `#!/bin/sh
echo "kubectl $*" >>"$TEST_LOG"
if [ "$1" = "--kubeconfig" ]; then
  shift 2
fi
case "$1" in
  get)
    if [ "$2" = "--raw=/readyz" ]; then
      exit 0
    fi
    if [ "$2" = "nodes" ]; then
      printf 'node-a node-b'
      exit 0
    fi
    ;;
esac
exit 0
`)
	t.Setenv("PATH", filepath.Join(tmpDir, "bin")+":"+os.Getenv("PATH"))
	t.Setenv("TEST_LOG", logPath)

	writeClusterInventory(t, "cluster-a", []v1beta1.Host{
		{IPS: []string{"10.0.0.10:22"}, Roles: []string{v1beta1.MASTER}},
		{IPS: []string{"10.0.0.11:22"}, Roles: []string{v1beta1.NODE}},
	})

	writeExecutable(t, filepath.Join(bundleDir, "components", "cilium", "files", "hooks", "healthcheck.sh"), `#!/bin/sh
exit 0
`)
	writeFile(t, filepath.Join(bundleDir, "components", "cilium", "files", "manifests", "cilium.yaml"), "apiVersion: apps/v1\nkind: DaemonSet\nmetadata:\n  name: cilium\n---\napiVersion: apps/v1\nkind: Deployment\nmetadata:\n  name: cilium-operator\n", 0o644)

	bundle := &hydrate.Bundle{
		APIVersion: distribution.APIVersion,
		Kind:       distribution.KindHydratedBundle,
		Spec: hydrate.BundleSpec{
			BOMName:  "minimal-single-node",
			Revision: "rev-1",
			Channel:  bom.ChannelAlpha,
			Components: []hydrate.RenderedComponent{
				{
					Name:        "cilium",
					PackageName: "cilium-cni",
					Version:     "v1.15.0",
					Class:       packageformat.ClassApplication,
					RootPath:    "components/cilium/files",
					Steps: []hydrate.RenderedStep{
						{
							Name:        "cilium-manifest",
							Kind:        hydrate.StepContent,
							BundlePath:  "components/cilium/files/manifests/cilium.yaml",
							SourcePath:  "manifests/cilium.yaml",
							ContentType: packageformat.ContentManifest,
						},
						{
							Name:           "healthcheck",
							Kind:           hydrate.StepHook,
							BundlePath:     "components/cilium/files/hooks/healthcheck.sh",
							SourcePath:     "hooks/healthcheck.sh",
							HookPhase:      packageformat.PhaseHealth,
							Target:         packageformat.TargetCluster,
							TimeoutSeconds: 5,
						},
					},
				},
			},
		},
	}
	if err := yamlutil.MarshalFile(filepath.Join(bundleDir, hydrate.BundleFileName), bundle); err != nil {
		t.Fatalf("MarshalFile(bundle) error = %v", err)
	}

	bundleDigest, err := hydrate.DigestBundle(bundleDir)
	if err != nil {
		t.Fatalf("DigestBundle() error = %v", err)
	}
	if _, err := state.PersistRenderedState(
		"cluster-a",
		state.BOMReference{
			Name:     bundle.Spec.BOMName,
			Revision: bundle.Spec.Revision,
			Channel:  bundle.Spec.Channel,
			Digest:   "sha256:1111111111111111111111111111111111111111111111111111111111111111",
		},
		bundleDigest.String(),
		"",
		"local-rev-1",
	); err != nil {
		t.Fatalf("PersistRenderedState() error = %v", err)
	}

	result, err := Apply(ApplyOptions{
		ClusterName:    "cluster-a",
		BundlePath:     bundleDir,
		KubeconfigPath: kubeconfigPath,
	})
	if err != nil {
		t.Fatalf("Apply() error = %v", err)
	}
	if result == nil || result.AppliedRevision == nil {
		t.Fatal("Apply() returned nil result or applied revision")
	}
	data, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatalf("ReadFile(log) error = %v", err)
	}
	logText := string(data)
	if strings.Contains(logText, "taint nodes") {
		t.Fatalf("multi-node cluster should not untaint nodes during prepare\nfull log:\n%s", logText)
	}
	if !strings.Contains(logText, "apply -f "+filepath.Join(bundleDir, "components", "cilium", "files", "manifests", "cilium.yaml")) {
		t.Fatalf("log missing manifest apply\nfull log:\n%s", logText)
	}
}

func TestApplyRejectsIncompletePreparedHostBundle(t *testing.T) {
	previousRuntimeRoot := constants.DefaultRuntimeRootDir
	constants.DefaultRuntimeRootDir = t.TempDir()
	t.Cleanup(func() {
		constants.DefaultRuntimeRootDir = previousRuntimeRoot
	})

	testCases := []struct {
		name    string
		setup   func(t *testing.T, bundleDir string)
		bundle  *hydrate.Bundle
		wantErr string
	}{
		{
			name: "missing runtime binary",
			setup: func(t *testing.T, bundleDir string) {
				writeExecutable(t, filepath.Join(bundleDir, "components", "containerd", "files", "hooks", "preflight.sh"), "#!/bin/sh\nexit 0\n")
				writeFile(t, filepath.Join(bundleDir, "components", "containerd", "files", "rootfs", "README"), "placeholder\n", 0o644)
			},
			bundle: &hydrate.Bundle{
				APIVersion: distribution.APIVersion,
				Kind:       distribution.KindHydratedBundle,
				Spec: hydrate.BundleSpec{
					BOMName:  "minimal-single-node",
					Revision: "rev-1",
					Channel:  bom.ChannelAlpha,
					Components: []hydrate.RenderedComponent{{
						Name:        "containerd",
						PackageName: "containerd-runtime",
						Version:     "v1.7.18",
						Class:       packageformat.ClassRootfs,
						RootPath:    "components/containerd/files",
						Steps: []hydrate.RenderedStep{
							{
								Name:        "runtime-rootfs",
								Kind:        hydrate.StepContent,
								BundlePath:  "components/containerd/files/rootfs",
								SourcePath:  "rootfs/",
								ContentType: packageformat.ContentRootfs,
							},
							{
								Name:           "preflight",
								Kind:           hydrate.StepHook,
								BundlePath:     "components/containerd/files/hooks/preflight.sh",
								SourcePath:     "hooks/preflight.sh",
								HookPhase:      packageformat.PhaseBootstrap,
								Target:         packageformat.TargetAllNodes,
								TimeoutSeconds: 5,
							},
						},
					}},
				},
			},
			wantErr: "missing required staged payloads",
		},
		{
			name: "placeholder hook content",
			setup: func(t *testing.T, bundleDir string) {
				for _, rel := range []string{
					filepath.Join("usr", "bin", "containerd"),
					filepath.Join("usr", "bin", "ctr"),
					filepath.Join("usr", "bin", "containerd-shim-runc-v2"),
					filepath.Join("usr", "bin", "runc"),
				} {
					writeFile(t, filepath.Join(bundleDir, "components", "containerd", "files", "rootfs", rel), "#!/bin/sh\nexit 0\n", 0o755)
				}
				writeExecutable(t, filepath.Join(bundleDir, "components", "containerd", "files", "hooks", "preflight.sh"), "#!/bin/sh\n# placeholder\nexit 0\n")
			},
			bundle: &hydrate.Bundle{
				APIVersion: distribution.APIVersion,
				Kind:       distribution.KindHydratedBundle,
				Spec: hydrate.BundleSpec{
					BOMName:  "minimal-single-node",
					Revision: "rev-1",
					Channel:  bom.ChannelAlpha,
					Components: []hydrate.RenderedComponent{{
						Name:        "containerd",
						PackageName: "containerd-runtime",
						Version:     "v1.7.18",
						Class:       packageformat.ClassRootfs,
						RootPath:    "components/containerd/files",
						Steps: []hydrate.RenderedStep{
							{
								Name:        "runtime-rootfs",
								Kind:        hydrate.StepContent,
								BundlePath:  "components/containerd/files/rootfs",
								SourcePath:  "rootfs/",
								ContentType: packageformat.ContentRootfs,
							},
							{
								Name:           "preflight",
								Kind:           hydrate.StepHook,
								BundlePath:     "components/containerd/files/hooks/preflight.sh",
								SourcePath:     "hooks/preflight.sh",
								HookPhase:      packageformat.PhaseBootstrap,
								Target:         packageformat.TargetAllNodes,
								TimeoutSeconds: 5,
							},
						},
					}},
				},
			},
			wantErr: "still contains placeholder content",
		},
		{
			name: "invalid cilium manifest payload",
			setup: func(t *testing.T, bundleDir string) {
				writeExecutable(t, filepath.Join(bundleDir, "components", "cilium", "files", "hooks", "healthcheck.sh"), "#!/bin/sh\nexit 0\n")
				writeFile(t, filepath.Join(bundleDir, "components", "cilium", "files", "manifests", "cilium.yaml"), "apiVersion: v1\nkind: ConfigMap\nmetadata:\n  name: cilium-config\n", 0o644)
			},
			bundle: &hydrate.Bundle{
				APIVersion: distribution.APIVersion,
				Kind:       distribution.KindHydratedBundle,
				Spec: hydrate.BundleSpec{
					BOMName:  "minimal-single-node",
					Revision: "rev-1",
					Channel:  bom.ChannelAlpha,
					Components: []hydrate.RenderedComponent{{
						Name:        "cilium",
						PackageName: "cilium-cni",
						Version:     "v1.15.0",
						Class:       packageformat.ClassApplication,
						RootPath:    "components/cilium/files",
						Steps: []hydrate.RenderedStep{
							{
								Name:        "cilium-manifest",
								Kind:        hydrate.StepContent,
								BundlePath:  "components/cilium/files/manifests/cilium.yaml",
								SourcePath:  "manifests/cilium.yaml",
								ContentType: packageformat.ContentManifest,
							},
							{
								Name:           "healthcheck",
								Kind:           hydrate.StepHook,
								BundlePath:     "components/cilium/files/hooks/healthcheck.sh",
								SourcePath:     "hooks/healthcheck.sh",
								HookPhase:      packageformat.PhaseHealth,
								Target:         packageformat.TargetCluster,
								TimeoutSeconds: 5,
							},
						},
					}},
				},
			},
			wantErr: "missing required manifest kinds",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			bundleDir := filepath.Join(t.TempDir(), "bundle")
			tc.setup(t, bundleDir)
			if err := yamlutil.MarshalFile(filepath.Join(bundleDir, hydrate.BundleFileName), tc.bundle); err != nil {
				t.Fatalf("MarshalFile(bundle) error = %v", err)
			}

			clusterName := strings.ReplaceAll(tc.name, " ", "-")
			persistRenderedStateForBundle(t, clusterName, bundleDir, tc.bundle)

			_, err := Apply(ApplyOptions{
				ClusterName: clusterName,
				BundlePath:  bundleDir,
				HostRoot:    t.TempDir(),
				Stderr:      bytes.NewBuffer(nil),
			})
			if err == nil || !strings.Contains(err.Error(), tc.wantErr) {
				t.Fatalf("Apply() error = %v, want substring %q", err, tc.wantErr)
			}
		})
	}
}

func TestApplyExecutesNodeTargetStepsAcrossResolvedHosts(t *testing.T) {
	previousRuntimeRoot := constants.DefaultRuntimeRootDir
	constants.DefaultRuntimeRootDir = t.TempDir()
	t.Cleanup(func() {
		constants.DefaultRuntimeRootDir = previousRuntimeRoot
	})

	previousLoadTopology := loadApplyExecutionTopology
	previousNewRemoteExecutor := newApplyRemoteExecutor
	t.Cleanup(func() {
		loadApplyExecutionTopology = previousLoadTopology
		newApplyRemoteExecutor = previousNewRemoteExecutor
	})

	fakeRemote := &fakeApplyRemoteExecutor{}
	loadApplyExecutionTopology = func(clusterName string) (*clusterExecutionTopology, error) {
		return &clusterExecutionTopology{
			clusterName: clusterName,
			allNodes:    []string{"10.0.0.10:22", "10.0.0.11:22"},
			firstMaster: "10.0.0.10:22",
		}, nil
	}
	newApplyRemoteExecutor = func(topology *clusterExecutionTopology) (applyRemoteExecutor, error) {
		return fakeRemote, nil
	}

	tmpDir := t.TempDir()
	writeExecutable(t, filepath.Join(tmpDir, "bin", "kubectl"), `#!/bin/sh
if [ "$1" = "--kubeconfig" ]; then
  shift 2
fi
if [ "$1" = "-n" ]; then
  shift 2
fi
case "$1" in
  get)
    if [ "$2" = "--raw=/readyz" ]; then
      exit 0
    fi
    if [ "$2" = "cm" ] && [ "$3" = "kubeadm-config" ]; then
      printf '%s' 'apiVersion: kubeadm.k8s.io/v1beta3
kind: ClusterConfiguration
clusterName: demo
'
      exit 0
    fi
    ;;
esac
exit 0
`)
	t.Setenv("PATH", filepath.Join(tmpDir, "bin")+":"+os.Getenv("PATH"))

	bundleDir := filepath.Join(t.TempDir(), "bundle")
	writeExecutable(t, filepath.Join(bundleDir, "components", "runtime", "files", "hooks", "preflight.sh"), "#!/bin/sh\nexit 0\n")
	writeExecutable(t, filepath.Join(bundleDir, "components", "runtime", "files", "hooks", "configure.sh"), "#!/bin/sh\nexit 0\n")
	writeExecutable(t, filepath.Join(bundleDir, "components", "runtime", "files", "hooks", "healthcheck.sh"), "#!/bin/sh\nexit 0\n")
	writeFile(t, filepath.Join(bundleDir, "components", "runtime", "files", "rootfs", "usr", "bin", "demo"), "#!/bin/sh\nexit 0\n", 0o755)
	writeFile(t, filepath.Join(bundleDir, "components", "runtime", "files", "files", "etc", "demo", "config.yaml"), "enabled: true\n", 0o644)

	bundle := &hydrate.Bundle{
		APIVersion: distribution.APIVersion,
		Kind:       distribution.KindHydratedBundle,
		Spec: hydrate.BundleSpec{
			BOMName:  "multi-node-runtime",
			Revision: "rev-1",
			Channel:  bom.ChannelAlpha,
			Components: []hydrate.RenderedComponent{{
				Name:        "runtime",
				PackageName: "runtime-rootfs",
				Version:     "v0.1.0",
				Class:       packageformat.ClassRootfs,
				RootPath:    "components/runtime/files",
				Steps: []hydrate.RenderedStep{
					{
						Name:        "runtime-rootfs",
						Kind:        hydrate.StepContent,
						BundlePath:  "components/runtime/files/rootfs",
						SourcePath:  "rootfs/",
						ContentType: packageformat.ContentRootfs,
					},
					{
						Name:        "runtime-config",
						Kind:        hydrate.StepContent,
						BundlePath:  "components/runtime/files/files/etc/demo/config.yaml",
						SourcePath:  "files/etc/demo/config.yaml",
						ContentType: packageformat.ContentFile,
					},
					{
						Name:           "preflight",
						Kind:           hydrate.StepHook,
						BundlePath:     "components/runtime/files/hooks/preflight.sh",
						SourcePath:     "hooks/preflight.sh",
						HookPhase:      packageformat.PhaseBootstrap,
						Target:         packageformat.TargetAllNodes,
						TimeoutSeconds: 5,
					},
					{
						Name:           "configure",
						Kind:           hydrate.StepHook,
						BundlePath:     "components/runtime/files/hooks/configure.sh",
						SourcePath:     "hooks/configure.sh",
						HookPhase:      packageformat.PhaseConfigure,
						Target:         packageformat.TargetFirstMaster,
						TimeoutSeconds: 5,
					},
					{
						Name:           "healthcheck",
						Kind:           hydrate.StepHook,
						BundlePath:     "components/runtime/files/hooks/healthcheck.sh",
						SourcePath:     "hooks/healthcheck.sh",
						HookPhase:      packageformat.PhaseHealth,
						Target:         packageformat.TargetAllNodes,
						TimeoutSeconds: 5,
					},
				},
			}},
		},
	}
	if err := yamlutil.MarshalFile(filepath.Join(bundleDir, hydrate.BundleFileName), bundle); err != nil {
		t.Fatalf("MarshalFile(bundle) error = %v", err)
	}
	persistRenderedStateForBundle(t, "cluster-a", bundleDir, bundle)

	result, err := Apply(ApplyOptions{
		ClusterName: "cluster-a",
		BundlePath:  bundleDir,
	})
	if err != nil {
		t.Fatalf("Apply() error = %v", err)
	}
	if result == nil || result.AppliedRevision == nil {
		t.Fatal("Apply() returned nil result or applied revision")
	}
	if got, want := len(fakeRemote.copyOps), 2; got != want {
		t.Fatalf("len(copyOps) = %d, want %d", got, want)
	}
	if got := strings.Join(fakeRemote.commandsForHost("10.0.0.10:22"), "\n"); !strings.Contains(got, "TARGET_IS_FIRST_MASTER='true'") {
		t.Fatalf("first master command stream missing TARGET_IS_FIRST_MASTER\ncommands:\n%s", got)
	}
	if got := strings.Join(fakeRemote.commandsForHost("10.0.0.11:22"), "\n"); strings.Contains(got, "configure.sh") {
		t.Fatalf("configure hook should not run on non-first-master\ncommands:\n%s", got)
	}
	for _, host := range []string{"10.0.0.10:22", "10.0.0.11:22"} {
		got := strings.Join(fakeRemote.commandsForHost(host), "\n")
		for _, marker := range []string{
			"cp -a \"$src\"/. \"$dst\"/",
			"cp -a \"$src\" \"$dst\"",
			"preflight.sh",
			"healthcheck.sh",
			"daemon-reload",
		} {
			if !strings.Contains(got, marker) {
				t.Fatalf("host %q command stream missing %q\ncommands:\n%s", host, marker, got)
			}
		}
	}
}

func TestApplyUsesBundleExecutionTopologySnapshotWithClusterfileRemoteExecutor(t *testing.T) {
	previousRuntimeRoot := constants.DefaultRuntimeRootDir
	constants.DefaultRuntimeRootDir = t.TempDir()
	t.Cleanup(func() {
		constants.DefaultRuntimeRootDir = previousRuntimeRoot
	})

	previousLoadTopology := loadApplyExecutionTopology
	previousNewRemoteExecutor := newApplyRemoteExecutor
	t.Cleanup(func() {
		loadApplyExecutionTopology = previousLoadTopology
		newApplyRemoteExecutor = previousNewRemoteExecutor
	})

	loadTopologyCalled := false
	loadApplyExecutionTopology = func(clusterName string) (*clusterExecutionTopology, error) {
		loadTopologyCalled = true
		return &clusterExecutionTopology{
			clusterName: clusterName,
			allNodes:    []string{"10.0.0.99:22"},
			firstMaster: "10.0.0.99:22",
			cluster: &v1beta1.Cluster{
				Spec: v1beta1.ClusterSpec{
					Hosts: []v1beta1.Host{
						{IPS: []string{"10.0.0.99:22"}, Roles: []string{v1beta1.NODE}},
					},
				},
			},
		}, nil
	}
	fakeRemote := &fakeApplyRemoteExecutor{}
	newApplyRemoteExecutor = func(topology *clusterExecutionTopology) (applyRemoteExecutor, error) {
		if topology.cluster == nil {
			t.Fatal("remote executor topology has nil Clusterfile inventory")
		}
		if got, want := strings.Join(topology.nodeExecutionHosts(), ","), "10.0.0.99:22"; got != want {
			t.Fatalf("topology nodes = %q, want %q", got, want)
		}
		if got, want := strings.Join(topology.rolesForHost("10.0.0.99:22"), ","), "node"; got != want {
			t.Fatalf("topology roles = %q, want %q", got, want)
		}
		return fakeRemote, nil
	}

	tmpDir := t.TempDir()
	bundleDir := filepath.Join(tmpDir, "bundle")
	writeFile(t, filepath.Join(bundleDir, "components", "runtime", "files", "rootfs", "usr", "bin", "runtime"), "#!/bin/sh\nexit 0\n", 0o755)
	writeExecutable(t, filepath.Join(bundleDir, "components", "runtime", "files", "hooks", "healthcheck.sh"), "#!/bin/sh\necho health\n")

	bundle := &hydrate.Bundle{
		APIVersion: distribution.APIVersion,
		Kind:       distribution.KindHydratedBundle,
		Spec: hydrate.BundleSpec{
			BOMName:  "snapshot-topology",
			Revision: "rev-1",
			Channel:  bom.ChannelAlpha,
			ExecutionTopology: hydrate.ExecutionTopology{
				Source:      "testSnapshot",
				AllNodes:    []string{"10.0.0.11:22"},
				FirstMaster: "10.0.0.11:22",
				HostRoles: []hydrate.ExecutionHostRoleList{
					{Host: "10.0.0.11:22", Roles: []string{v1beta1.MASTER}},
				},
			},
			Components: []hydrate.RenderedComponent{
				{
					Name:        "runtime",
					PackageName: "generic-runtime",
					Version:     "v1",
					Class:       packageformat.ClassRootfs,
					RootPath:    "components/runtime/files",
					Steps: []hydrate.RenderedStep{
						{
							Name:        "runtime-rootfs",
							Kind:        hydrate.StepContent,
							BundlePath:  "components/runtime/files/rootfs",
							SourcePath:  "rootfs/",
							ContentType: packageformat.ContentRootfs,
						},
						{
							Name:       "healthcheck",
							Kind:       hydrate.StepHook,
							BundlePath: "components/runtime/files/hooks/healthcheck.sh",
							SourcePath: "hooks/healthcheck.sh",
							HookPhase:  packageformat.PhaseHealth,
							Target:     packageformat.TargetAllNodes,
						},
					},
				},
			},
		},
	}
	if err := yamlutil.MarshalFile(filepath.Join(bundleDir, hydrate.BundleFileName), bundle); err != nil {
		t.Fatalf("MarshalFile(bundle) error = %v", err)
	}
	persistRenderedStateForBundle(t, "snapshot-topology", bundleDir, bundle)

	if _, err := Apply(ApplyOptions{
		ClusterName: "snapshot-topology",
		BundlePath:  bundleDir,
		Stderr:      bytes.NewBuffer(nil),
	}); err != nil {
		t.Fatalf("Apply() error = %v", err)
	}
	if !loadTopologyCalled {
		t.Fatal("loadApplyExecutionTopology was not called for remote executor credentials")
	}
	if got := strings.Join(fakeRemote.commandsForHost("10.0.0.11:22"), "\n"); !strings.Contains(got, "healthcheck.sh") {
		t.Fatalf("remote command stream missing healthcheck hook\ncommands:\n%s", got)
	}
	if got := strings.Join(fakeRemote.commandsForHost("10.0.0.99:22"), "\n"); got != "" {
		t.Fatalf("remote commands used Clusterfile host instead of bundle snapshot host:\n%s", got)
	}
}

func TestApplyRespectsExecutionTargetsOnMultiNodeCluster(t *testing.T) {
	previousRuntimeRoot := constants.DefaultRuntimeRootDir
	constants.DefaultRuntimeRootDir = t.TempDir()
	t.Cleanup(func() {
		constants.DefaultRuntimeRootDir = previousRuntimeRoot
	})

	previousLoadTopology := loadApplyExecutionTopology
	previousNewRemoteExecutor := newApplyRemoteExecutor
	t.Cleanup(func() {
		loadApplyExecutionTopology = previousLoadTopology
		newApplyRemoteExecutor = previousNewRemoteExecutor
	})

	fakeRemote := &fakeApplyRemoteExecutor{}
	loadApplyExecutionTopology = func(clusterName string) (*clusterExecutionTopology, error) {
		return &clusterExecutionTopology{
			clusterName: clusterName,
			allNodes:    []string{"10.0.0.10:22", "10.0.0.11:22", "10.0.0.12:22"},
			firstMaster: "10.0.0.10:22",
		}, nil
	}
	newApplyRemoteExecutor = func(topology *clusterExecutionTopology) (applyRemoteExecutor, error) {
		return fakeRemote, nil
	}

	tmpDir := t.TempDir()
	hookLog := filepath.Join(tmpDir, "cluster-hook.log")
	writeExecutable(t, filepath.Join(tmpDir, "bin", "kubectl"), `#!/bin/sh
if [ "$1" = "--kubeconfig" ]; then
  shift 2
fi
if [ "$1" = "get" ] && [ "$2" = "--raw=/readyz" ]; then
  exit 0
fi
if [ "$1" = "get" ] && [ "$2" = "nodes" ]; then
  printf 'node-a'
  exit 0
fi
if [ "$1" = "taint" ]; then
  exit 0
fi
exit 1
`)
	t.Setenv("PATH", filepath.Join(tmpDir, "bin")+":"+os.Getenv("PATH"))
	t.Setenv("CLUSTER_HOOK_LOG", hookLog)

	bundleDir := filepath.Join(t.TempDir(), "bundle")
	writeFile(t, filepath.Join(bundleDir, "components", "runtime", "files", "rootfs", "usr", "bin", "demo"), "#!/bin/sh\nexit 0\n", 0o755)
	writeFile(t, filepath.Join(bundleDir, "components", "runtime", "files", "files", "etc", "demo", "config.yaml"), "enabled: true\n", 0o644)
	writeExecutable(t, filepath.Join(bundleDir, "components", "runtime", "files", "hooks", "bootstrap-all.sh"), "#!/bin/sh\nexit 0\n")
	writeExecutable(t, filepath.Join(bundleDir, "components", "runtime", "files", "hooks", "configure-first.sh"), "#!/bin/sh\nexit 0\n")
	writeExecutable(t, filepath.Join(bundleDir, "components", "runtime", "files", "hooks", "install-cluster.sh"), "#!/bin/sh\nprintf 'cluster:%s:%s\\n' \"$TARGET_HOST\" \"$HOST_ROOT\" >>\"$CLUSTER_HOOK_LOG\"\n")
	writeExecutable(t, filepath.Join(bundleDir, "components", "runtime", "files", "hooks", "health-all.sh"), "#!/bin/sh\nexit 0\n")

	bundle := &hydrate.Bundle{
		APIVersion: distribution.APIVersion,
		Kind:       distribution.KindHydratedBundle,
		Spec: hydrate.BundleSpec{
			BOMName:  "multi-node-targets",
			Revision: "rev-1",
			Channel:  bom.ChannelAlpha,
			Components: []hydrate.RenderedComponent{{
				Name:        "runtime",
				PackageName: "runtime-rootfs",
				Version:     "v0.1.0",
				Class:       packageformat.ClassRootfs,
				RootPath:    "components/runtime/files",
				Steps: []hydrate.RenderedStep{
					{
						Name:        "runtime-rootfs",
						Kind:        hydrate.StepContent,
						BundlePath:  "components/runtime/files/rootfs",
						SourcePath:  "rootfs/",
						ContentType: packageformat.ContentRootfs,
					},
					{
						Name:        "runtime-config",
						Kind:        hydrate.StepContent,
						BundlePath:  "components/runtime/files/files/etc/demo/config.yaml",
						SourcePath:  "files/etc/demo/config.yaml",
						ContentType: packageformat.ContentFile,
					},
					{
						Name:           "bootstrap-all",
						Kind:           hydrate.StepHook,
						BundlePath:     "components/runtime/files/hooks/bootstrap-all.sh",
						SourcePath:     "hooks/bootstrap-all.sh",
						HookPhase:      packageformat.PhaseBootstrap,
						Target:         packageformat.TargetAllNodes,
						TimeoutSeconds: 5,
					},
					{
						Name:           "configure-first",
						Kind:           hydrate.StepHook,
						BundlePath:     "components/runtime/files/hooks/configure-first.sh",
						SourcePath:     "hooks/configure-first.sh",
						HookPhase:      packageformat.PhaseConfigure,
						Target:         packageformat.TargetFirstMaster,
						TimeoutSeconds: 5,
					},
					{
						Name:           "install-cluster",
						Kind:           hydrate.StepHook,
						BundlePath:     "components/runtime/files/hooks/install-cluster.sh",
						SourcePath:     "hooks/install-cluster.sh",
						HookPhase:      packageformat.PhaseInstall,
						Target:         packageformat.TargetCluster,
						TimeoutSeconds: 5,
					},
					{
						Name:           "health-all",
						Kind:           hydrate.StepHook,
						BundlePath:     "components/runtime/files/hooks/health-all.sh",
						SourcePath:     "hooks/health-all.sh",
						HookPhase:      packageformat.PhaseHealth,
						Target:         packageformat.TargetAllNodes,
						TimeoutSeconds: 5,
					},
				},
			}},
		},
	}
	if err := yamlutil.MarshalFile(filepath.Join(bundleDir, hydrate.BundleFileName), bundle); err != nil {
		t.Fatalf("MarshalFile(bundle) error = %v", err)
	}
	persistRenderedStateForBundle(t, "cluster-targets", bundleDir, bundle)

	result, err := Apply(ApplyOptions{
		ClusterName: "cluster-targets",
		BundlePath:  bundleDir,
	})
	if err != nil {
		t.Fatalf("Apply() error = %v", err)
	}
	if result == nil || result.AppliedRevision == nil {
		t.Fatal("Apply() returned nil result or applied revision")
	}

	allCommands := strings.Join(fakeRemote.allCommands(), "\n")
	if strings.Contains(allCommands, "install-cluster.sh") {
		t.Fatalf("cluster target hook ran on a remote host\ncommands:\n%s", allCommands)
	}
	for _, host := range []string{"10.0.0.10:22", "10.0.0.11:22", "10.0.0.12:22"} {
		commands := strings.Join(fakeRemote.commandsForHost(host), "\n")
		for _, marker := range []string{"bootstrap-all.sh", "health-all.sh"} {
			if !strings.Contains(commands, marker) {
				t.Fatalf("host %q command stream missing allNodes hook %q\ncommands:\n%s", host, marker, commands)
			}
		}
		if host == "10.0.0.10:22" {
			if !strings.Contains(commands, "configure-first.sh") {
				t.Fatalf("first master command stream missing firstMaster hook\ncommands:\n%s", commands)
			}
			continue
		}
		if strings.Contains(commands, "configure-first.sh") {
			t.Fatalf("non-first-master %q command stream included firstMaster hook\ncommands:\n%s", host, commands)
		}
	}
	data, err := os.ReadFile(hookLog)
	if err != nil {
		t.Fatalf("ReadFile(cluster hook log) error = %v", err)
	}
	if got, want := strings.TrimSpace(string(data)), "cluster:localhost:/"; got != want {
		t.Fatalf("cluster hook log = %q, want %q", got, want)
	}
}

func TestApplyUsesHostScopedInputBindingsForMultiNodeFileContent(t *testing.T) {
	previousRuntimeRoot := constants.DefaultRuntimeRootDir
	constants.DefaultRuntimeRootDir = t.TempDir()
	t.Cleanup(func() {
		constants.DefaultRuntimeRootDir = previousRuntimeRoot
	})

	previousLoadTopology := loadApplyExecutionTopology
	previousNewRemoteExecutor := newApplyRemoteExecutor
	t.Cleanup(func() {
		loadApplyExecutionTopology = previousLoadTopology
		newApplyRemoteExecutor = previousNewRemoteExecutor
	})

	fakeRemote := &fakeApplyRemoteExecutor{}
	loadApplyExecutionTopology = func(clusterName string) (*clusterExecutionTopology, error) {
		return &clusterExecutionTopology{
			clusterName: clusterName,
			allNodes:    []string{"10.0.0.10:22", "10.0.0.11:22"},
			firstMaster: "10.0.0.10:22",
		}, nil
	}
	newApplyRemoteExecutor = func(topology *clusterExecutionTopology) (applyRemoteExecutor, error) {
		return fakeRemote, nil
	}

	tmpDir := t.TempDir()
	writeExecutable(t, filepath.Join(tmpDir, "bin", "kubectl"), `#!/bin/sh
if [ "$1" = "--kubeconfig" ]; then
  shift 2
fi
if [ "$1" = "get" ] && [ "$2" = "--raw=/readyz" ]; then
  exit 0
fi
if [ "$1" = "get" ] && [ "$2" = "nodes" ]; then
  printf 'node-a'
  exit 0
fi
exit 0
`)
	t.Setenv("PATH", filepath.Join(tmpDir, "bin")+":"+os.Getenv("PATH"))

	bundleDir := filepath.Join(t.TempDir(), "bundle")
	writeFile(t, filepath.Join(bundleDir, "components", "runtime", "files", "rootfs", "usr", "bin", "demo"), "#!/bin/sh\nexit 0\n", 0o755)
	writeExecutable(t, filepath.Join(bundleDir, "components", "runtime", "files", "hooks", "bootstrap.sh"), "#!/bin/sh\nexit 0\n")
	writeExecutable(t, filepath.Join(bundleDir, "components", "runtime", "files", "hooks", "healthcheck.sh"), "#!/bin/sh\nexit 0\n")
	writeFile(t, filepath.Join(bundleDir, "components", "runtime", "files", "files", "etc", "demo", "config.yaml"), "clusterName: default\n", 0o644)
	writeFile(t, filepath.Join(bundleDir, "components", "runtime", "host-inputs", "10.0.0.11", "files", "etc", "demo", "config.yaml"), "clusterName: host-11\n", 0o644)

	bundle := &hydrate.Bundle{
		APIVersion: distribution.APIVersion,
		Kind:       distribution.KindHydratedBundle,
		Spec: hydrate.BundleSpec{
			BOMName:  "multi-node-runtime-inputs",
			Revision: "rev-1",
			Channel:  bom.ChannelAlpha,
			Components: []hydrate.RenderedComponent{{
				Name:        "runtime",
				PackageName: "runtime-rootfs",
				Version:     "v0.1.0",
				Class:       packageformat.ClassRootfs,
				RootPath:    "components/runtime/files",
				Inputs: []packageformat.Input{
					{
						Name: "runtime-config",
						Type: packageformat.InputConfigFile,
						Path: "files/etc/demo/config.yaml",
					},
				},
				InputBindings: map[string]string{
					"runtime-config": "/tmp/local-repo/inputs/runtime/config.yaml",
				},
				HostInputBindings: map[string]map[string]string{
					"runtime-config": {
						"10.0.0.11": "components/runtime/host-inputs/10.0.0.11/files/etc/demo/config.yaml",
					},
				},
				Steps: []hydrate.RenderedStep{
					{
						Name:        "runtime-rootfs",
						Kind:        hydrate.StepContent,
						BundlePath:  "components/runtime/files/rootfs",
						SourcePath:  "rootfs/",
						ContentType: packageformat.ContentRootfs,
					},
					{
						Name:        "runtime-config",
						Kind:        hydrate.StepContent,
						BundlePath:  "components/runtime/files/files/etc/demo/config.yaml",
						SourcePath:  "files/etc/demo/config.yaml",
						ContentType: packageformat.ContentFile,
					},
					{
						Name:           "bootstrap",
						Kind:           hydrate.StepHook,
						BundlePath:     "components/runtime/files/hooks/bootstrap.sh",
						SourcePath:     "hooks/bootstrap.sh",
						HookPhase:      packageformat.PhaseBootstrap,
						Target:         packageformat.TargetAllNodes,
						TimeoutSeconds: 5,
					},
					{
						Name:           "healthcheck",
						Kind:           hydrate.StepHook,
						BundlePath:     "components/runtime/files/hooks/healthcheck.sh",
						SourcePath:     "hooks/healthcheck.sh",
						HookPhase:      packageformat.PhaseHealth,
						Target:         packageformat.TargetAllNodes,
						TimeoutSeconds: 5,
					},
				},
			}},
		},
	}
	if err := yamlutil.MarshalFile(filepath.Join(bundleDir, hydrate.BundleFileName), bundle); err != nil {
		t.Fatalf("MarshalFile(bundle) error = %v", err)
	}
	persistRenderedStateForBundle(t, "cluster-a", bundleDir, bundle)

	result, err := Apply(ApplyOptions{
		ClusterName: "cluster-a",
		BundlePath:  bundleDir,
	})
	if err != nil {
		t.Fatalf("Apply() error = %v", err)
	}
	if result == nil || result.AppliedRevision == nil {
		t.Fatal("Apply() returned nil result or applied revision")
	}

	host10Commands := strings.Join(fakeRemote.commandsForHost("10.0.0.10:22"), "\n")
	host11Commands := strings.Join(fakeRemote.commandsForHost("10.0.0.11:22"), "\n")
	if !strings.Contains(host10Commands, "files/files/etc/demo/config.yaml") {
		t.Fatalf("default host command stream missing default input path\ncommands:\n%s", host10Commands)
	}
	if strings.Contains(host10Commands, "host-inputs/10.0.0.11/files/etc/demo/config.yaml") {
		t.Fatalf("default host command stream unexpectedly used host-scoped input path\ncommands:\n%s", host10Commands)
	}
	if !strings.Contains(host11Commands, "host-inputs/10.0.0.11/files/etc/demo/config.yaml") {
		t.Fatalf("host-scoped command stream missing host-specific input path\ncommands:\n%s", host11Commands)
	}
	if !strings.Contains(host10Commands, "/etc/demo/config.yaml") || !strings.Contains(host11Commands, "/etc/demo/config.yaml") {
		t.Fatalf("expected both hosts to target /etc/demo/config.yaml\nhost10:\n%s\nhost11:\n%s", host10Commands, host11Commands)
	}
}

func TestApplyGeneratesPerHostKubeadmJoinConfigsForMultiNodeBootstrap(t *testing.T) {
	previousRuntimeRoot := constants.DefaultRuntimeRootDir
	constants.DefaultRuntimeRootDir = t.TempDir()
	t.Cleanup(func() {
		constants.DefaultRuntimeRootDir = previousRuntimeRoot
	})

	previousLoadTopology := loadApplyExecutionTopology
	previousNewRemoteExecutor := newApplyRemoteExecutor
	t.Cleanup(func() {
		loadApplyExecutionTopology = previousLoadTopology
		newApplyRemoteExecutor = previousNewRemoteExecutor
	})

	fakeRemote := &fakeApplyRemoteExecutor{
		outputs: []fakeRemoteCommandOutput{{
			host:     "10.0.0.10:22",
			contains: "kubeadm token create --print-join-command",
			output:   "kubeadm join 10.0.0.10:6443 --token abcdef.0123456789abcdef --discovery-token-ca-cert-hash sha256:cafebabe --certificate-key 11223344556677889900aabbccddeeff\n",
		}},
	}
	loadApplyExecutionTopology = func(clusterName string) (*clusterExecutionTopology, error) {
		return &clusterExecutionTopology{
			clusterName: clusterName,
			allNodes:    []string{"10.0.0.10:22", "10.0.0.11:22", "10.0.0.12:22"},
			firstMaster: "10.0.0.10:22",
			cluster: &v1beta1.Cluster{
				Spec: v1beta1.ClusterSpec{
					Hosts: []v1beta1.Host{
						{IPS: []string{"10.0.0.10:22"}, Roles: []string{v1beta1.MASTER}},
						{IPS: []string{"10.0.0.11:22"}, Roles: []string{v1beta1.MASTER}},
						{IPS: []string{"10.0.0.12:22"}, Roles: []string{v1beta1.NODE}},
					},
				},
			},
		}, nil
	}
	newApplyRemoteExecutor = func(topology *clusterExecutionTopology) (applyRemoteExecutor, error) {
		return fakeRemote, nil
	}

	bundleDir := filepath.Join(t.TempDir(), "bundle")
	writeExecutable(t, filepath.Join(bundleDir, "components", "kubernetes", "files", "hooks", "bootstrap.sh"), "#!/bin/sh\nexit 0\n")
	writeExecutable(t, filepath.Join(bundleDir, "components", "kubernetes", "files", "hooks", "healthcheck.sh"), "#!/bin/sh\nexit 0\n")
	writeFile(t, filepath.Join(bundleDir, "components", "kubernetes", "files", "rootfs", "usr", "bin", "kubeadm"), "#!/bin/sh\nexit 0\n", 0o755)
	writeFile(t, filepath.Join(bundleDir, "components", "kubernetes", "files", "rootfs", "usr", "bin", "kubelet"), "#!/bin/sh\nexit 0\n", 0o755)
	writeFile(t, filepath.Join(bundleDir, "components", "kubernetes", "files", "rootfs", "usr", "bin", "kubectl"), "#!/bin/sh\nexit 0\n", 0o755)
	writeFile(t, filepath.Join(bundleDir, "components", "kubernetes", "files", "files", "etc", "kubernetes", "kubeadm.yaml"), "apiVersion: kubeadm.k8s.io/v1beta3\nkind: ClusterConfiguration\nclusterName: demo\n", 0o644)

	bundle := &hydrate.Bundle{
		APIVersion: distribution.APIVersion,
		Kind:       distribution.KindHydratedBundle,
		Spec: hydrate.BundleSpec{
			BOMName:  "multi-node-kubernetes",
			Revision: "rev-1",
			Channel:  bom.ChannelAlpha,
			Components: []hydrate.RenderedComponent{{
				Name:        "kubernetes",
				PackageName: "kubernetes-rootfs",
				Version:     "v1.30.3",
				Class:       packageformat.ClassRootfs,
				RootPath:    "components/kubernetes/files",
				Steps: []hydrate.RenderedStep{
					{
						Name:        "kubernetes-rootfs",
						Kind:        hydrate.StepContent,
						BundlePath:  "components/kubernetes/files/rootfs",
						SourcePath:  "rootfs/",
						ContentType: packageformat.ContentRootfs,
					},
					{
						Name:        "kubeadm-defaults",
						Kind:        hydrate.StepContent,
						BundlePath:  "components/kubernetes/files/files/etc/kubernetes/kubeadm.yaml",
						SourcePath:  "files/etc/kubernetes/kubeadm.yaml",
						ContentType: packageformat.ContentFile,
					},
					{
						Name:           "bootstrap",
						Kind:           hydrate.StepHook,
						BundlePath:     "components/kubernetes/files/hooks/bootstrap.sh",
						SourcePath:     "hooks/bootstrap.sh",
						HookPhase:      packageformat.PhaseBootstrap,
						Target:         packageformat.TargetAllNodes,
						TimeoutSeconds: 5,
					},
					{
						Name:           "healthcheck",
						Kind:           hydrate.StepHook,
						BundlePath:     "components/kubernetes/files/hooks/healthcheck.sh",
						SourcePath:     "hooks/healthcheck.sh",
						HookPhase:      packageformat.PhaseHealth,
						Target:         packageformat.TargetCluster,
						TimeoutSeconds: 5,
					},
				},
			}},
		},
	}
	if err := yamlutil.MarshalFile(filepath.Join(bundleDir, hydrate.BundleFileName), bundle); err != nil {
		t.Fatalf("MarshalFile(bundle) error = %v", err)
	}
	persistRenderedStateForBundle(t, "cluster-a", bundleDir, bundle)

	result, err := Apply(ApplyOptions{
		ClusterName:    "cluster-a",
		BundlePath:     bundleDir,
		KubeconfigPath: filepath.Join(t.TempDir(), "admin.conf"),
	})
	if err != nil {
		t.Fatalf("Apply() error = %v", err)
	}
	if result == nil || result.AppliedRevision == nil {
		t.Fatal("Apply() returned nil result or applied revision")
	}

	firstMasterCommands := strings.Join(fakeRemote.commandsForHost("10.0.0.10:22"), "\n")
	if count := strings.Count(firstMasterCommands, "bootstrap.sh"); count != 1 {
		t.Fatalf("first master bootstrap count = %d, want 1\ncommands:\n%s", count, firstMasterCommands)
	}
	if !strings.Contains(firstMasterCommands, "kubeadm token create --print-join-command") {
		t.Fatalf("first master command stream missing join metadata generation\ncommands:\n%s", firstMasterCommands)
	}

	if got, want := len(fakeRemote.copyOps), 6; got != want {
		t.Fatalf("len(copyOps) = %d, want %d", got, want)
	}
	var joinMasterConfig, joinWorkerConfig string
	foundClusterConfigUpload := false
	for _, op := range fakeRemote.copyOps {
		if op.host == "10.0.0.10:22" && strings.HasPrefix(op.dst, "/tmp/sealos-kubeadm-cluster-config-") {
			foundClusterConfigUpload = true
		}
		if op.dst != "/etc/kubernetes/kubeadm.yaml" {
			continue
		}
		switch op.host {
		case "10.0.0.11:22":
			joinMasterConfig = string(op.data)
		case "10.0.0.12:22":
			joinWorkerConfig = string(op.data)
		}
	}
	if !strings.Contains(joinMasterConfig, "kind: JoinConfiguration") || !strings.Contains(joinMasterConfig, "certificateKey: 11223344556677889900aabbccddeeff") || !strings.Contains(joinMasterConfig, "advertiseAddress: 10.0.0.11") {
		t.Fatalf("join master config did not include expected control-plane fields:\n%s", joinMasterConfig)
	}
	if !strings.Contains(joinWorkerConfig, "kind: JoinConfiguration") || strings.Contains(joinWorkerConfig, "certificateKey:") || !strings.Contains(joinWorkerConfig, "token: abcdef.0123456789abcdef") {
		t.Fatalf("join worker config did not include expected worker fields:\n%s", joinWorkerConfig)
	}
	if !foundClusterConfigUpload {
		t.Fatal("expected a kubeadm cluster config upload for the first master")
	}

	masterJoinCommands := strings.Join(fakeRemote.commandsForHost("10.0.0.11:22"), "\n")
	if strings.Count(masterJoinCommands, "bootstrap.sh") != 1 {
		t.Fatalf("expected one bootstrap invocation on second master\ncommands:\n%s", masterJoinCommands)
	}
	workerJoinCommands := strings.Join(fakeRemote.commandsForHost("10.0.0.12:22"), "\n")
	if strings.Count(workerJoinCommands, "bootstrap.sh") != 1 {
		t.Fatalf("expected one bootstrap invocation on worker\ncommands:\n%s", workerJoinCommands)
	}
}

func TestApplyFetchesKubeconfigFromRemoteFirstMasterForClusterSteps(t *testing.T) {
	previousRuntimeRoot := constants.DefaultRuntimeRootDir
	constants.DefaultRuntimeRootDir = t.TempDir()
	t.Cleanup(func() {
		constants.DefaultRuntimeRootDir = previousRuntimeRoot
	})

	previousLoadTopology := loadApplyExecutionTopology
	previousNewRemoteExecutor := newApplyRemoteExecutor
	t.Cleanup(func() {
		loadApplyExecutionTopology = previousLoadTopology
		newApplyRemoteExecutor = previousNewRemoteExecutor
	})

	fakeRemote := &fakeApplyRemoteExecutor{
		fetchContents: map[string][]byte{
			"10.0.0.10:22:/etc/kubernetes/admin.conf": []byte("apiVersion: v1\nkind: Config\n"),
		},
		outputs: []fakeRemoteCommandOutput{{
			host:     "10.0.0.10:22",
			contains: "kubeadm token create --print-join-command",
			output:   "kubeadm join 10.0.0.10:6443 --token abcdef.0123456789abcdef --discovery-token-ca-cert-hash sha256:cafebabe --certificate-key 11223344556677889900aabbccddeeff\n",
		}},
	}
	loadApplyExecutionTopology = func(clusterName string) (*clusterExecutionTopology, error) {
		return &clusterExecutionTopology{
			clusterName: clusterName,
			allNodes:    []string{"10.0.0.10:22", "10.0.0.11:22"},
			firstMaster: "10.0.0.10:22",
			cluster: &v1beta1.Cluster{
				Spec: v1beta1.ClusterSpec{
					Hosts: []v1beta1.Host{
						{IPS: []string{"10.0.0.10:22"}, Roles: []string{v1beta1.MASTER}},
						{IPS: []string{"10.0.0.11:22"}, Roles: []string{v1beta1.NODE}},
					},
				},
			},
		}, nil
	}
	newApplyRemoteExecutor = func(topology *clusterExecutionTopology) (applyRemoteExecutor, error) {
		return fakeRemote, nil
	}

	tmpDir := t.TempDir()
	kubectlLog := filepath.Join(tmpDir, "kubectl.log")
	writeExecutable(t, filepath.Join(tmpDir, "bin", "kubectl"), `#!/bin/sh
printf '%s\n' "$*" >>"`+kubectlLog+`"
if [ "$1" = "--kubeconfig" ]; then
  shift 2
fi
if [ "$1" = "-n" ]; then
  shift 2
fi
case "$1" in
  get)
    if [ "$2" = "--raw=/readyz" ]; then
      exit 0
    fi
    if [ "$2" = "cm" ] && [ "$3" = "kubeadm-config" ]; then
      printf '%s' 'apiVersion: kubeadm.k8s.io/v1beta3
kind: ClusterConfiguration
clusterName: demo
'
      exit 0
    fi
    if [ "$2" = "nodes" ]; then
      printf 'node-a node-b'
      exit 0
    fi
    ;;
  apply)
    exit 0
    ;;
esac
exit 0
`)
	t.Setenv("PATH", filepath.Join(tmpDir, "bin")+":"+os.Getenv("PATH"))

	bundleDir := filepath.Join(t.TempDir(), "bundle")
	writeExecutable(t, filepath.Join(bundleDir, "components", "kubernetes", "files", "hooks", "bootstrap.sh"), "#!/bin/sh\nexit 0\n")
	writeExecutable(t, filepath.Join(bundleDir, "components", "kubernetes", "files", "hooks", "healthcheck.sh"), "#!/bin/sh\nexit 0\n")
	writeFile(t, filepath.Join(bundleDir, "components", "kubernetes", "files", "rootfs", "usr", "bin", "kubeadm"), "#!/bin/sh\nexit 0\n", 0o755)
	writeFile(t, filepath.Join(bundleDir, "components", "kubernetes", "files", "rootfs", "usr", "bin", "kubelet"), "#!/bin/sh\nexit 0\n", 0o755)
	writeFile(t, filepath.Join(bundleDir, "components", "kubernetes", "files", "rootfs", "usr", "bin", "kubectl"), "#!/bin/sh\nexit 0\n", 0o755)
	writeFile(t, filepath.Join(bundleDir, "components", "kubernetes", "files", "files", "etc", "kubernetes", "kubeadm.yaml"), "apiVersion: kubeadm.k8s.io/v1beta3\nkind: ClusterConfiguration\nclusterName: demo\n", 0o644)
	writeFile(t, filepath.Join(bundleDir, "components", "kubernetes", "files", "manifests", "bootstrap", "namespace.yaml"), "apiVersion: v1\nkind: Namespace\nmetadata:\n  name: demo\n", 0o644)

	bundle := &hydrate.Bundle{
		APIVersion: distribution.APIVersion,
		Kind:       distribution.KindHydratedBundle,
		Spec: hydrate.BundleSpec{
			BOMName:  "remote-first-master",
			Revision: "rev-1",
			Channel:  bom.ChannelAlpha,
			Components: []hydrate.RenderedComponent{{
				Name:        "kubernetes",
				PackageName: "kubernetes-rootfs",
				Version:     "v1.30.3",
				Class:       packageformat.ClassRootfs,
				RootPath:    "components/kubernetes/files",
				Steps: []hydrate.RenderedStep{
					{
						Name:        "kubernetes-rootfs",
						Kind:        hydrate.StepContent,
						BundlePath:  "components/kubernetes/files/rootfs",
						SourcePath:  "rootfs/",
						ContentType: packageformat.ContentRootfs,
					},
					{
						Name:        "kubeadm-defaults",
						Kind:        hydrate.StepContent,
						BundlePath:  "components/kubernetes/files/files/etc/kubernetes/kubeadm.yaml",
						SourcePath:  "files/etc/kubernetes/kubeadm.yaml",
						ContentType: packageformat.ContentFile,
					},
					{
						Name:        "bootstrap-manifests",
						Kind:        hydrate.StepContent,
						BundlePath:  "components/kubernetes/files/manifests/bootstrap",
						SourcePath:  "manifests/bootstrap",
						ContentType: packageformat.ContentManifest,
					},
					{
						Name:           "bootstrap",
						Kind:           hydrate.StepHook,
						BundlePath:     "components/kubernetes/files/hooks/bootstrap.sh",
						SourcePath:     "hooks/bootstrap.sh",
						HookPhase:      packageformat.PhaseBootstrap,
						Target:         packageformat.TargetAllNodes,
						TimeoutSeconds: 5,
					},
					{
						Name:           "healthcheck",
						Kind:           hydrate.StepHook,
						BundlePath:     "components/kubernetes/files/hooks/healthcheck.sh",
						SourcePath:     "hooks/healthcheck.sh",
						HookPhase:      packageformat.PhaseHealth,
						Target:         packageformat.TargetCluster,
						TimeoutSeconds: 5,
					},
				},
			}},
		},
	}
	if err := yamlutil.MarshalFile(filepath.Join(bundleDir, hydrate.BundleFileName), bundle); err != nil {
		t.Fatalf("MarshalFile(bundle) error = %v", err)
	}
	persistRenderedStateForBundle(t, "cluster-a", bundleDir, bundle)

	kubeconfigPath := filepath.Join(t.TempDir(), "admin.conf")
	result, err := Apply(ApplyOptions{
		ClusterName:    "cluster-a",
		BundlePath:     bundleDir,
		KubeconfigPath: kubeconfigPath,
	})
	if err != nil {
		t.Fatalf("Apply() error = %v", err)
	}
	if result == nil || result.AppliedRevision == nil {
		t.Fatal("Apply() returned nil result or applied revision")
	}

	if got, want := len(fakeRemote.fetchOps), 1; got != want {
		t.Fatalf("len(fetchOps) = %d, want %d", got, want)
	}
	if got, want := fakeRemote.fetchOps[0].host, "10.0.0.10:22"; got != want {
		t.Fatalf("fetch host = %q, want %q", got, want)
	}
	if got, want := fakeRemote.fetchOps[0].src, "/etc/kubernetes/admin.conf"; got != want {
		t.Fatalf("fetch src = %q, want %q", got, want)
	}
	if data, err := os.ReadFile(kubeconfigPath); err != nil || !strings.Contains(string(data), "kind: Config") {
		t.Fatalf("fetched kubeconfig = %q, err = %v", string(data), err)
	}
	kubectlCalls, err := os.ReadFile(kubectlLog)
	if err != nil {
		t.Fatalf("ReadFile(kubectl log) error = %v", err)
	}
	if !strings.Contains(string(kubectlCalls), "--kubeconfig "+kubeconfigPath+" apply -f") {
		t.Fatalf("kubectl log missing apply with fetched kubeconfig:\n%s", string(kubectlCalls))
	}
}

func TestKubeadmBootstrapImagesIncludePauseImage(t *testing.T) {
	fakeRemote := &fakeApplyRemoteExecutor{
		outputs: []fakeRemoteCommandOutput{
			{
				host:     "10.0.0.10:22",
				contains: "kubeadm config images list",
				output: strings.Join([]string{
					"registry.k8s.io/kube-apiserver:v1.30.3",
					"registry.k8s.io/kube-proxy:v1.30.3",
					"",
				}, "\n"),
			},
			{
				host:     "10.0.0.10:22",
				contains: "sandbox_image",
				output:   "registry.k8s.io/pause:3.8\n",
			},
		},
	}

	executor := &bundleExecutor{
		clusterName: "cluster-a",
		topology: &clusterExecutionTopology{
			clusterName: "cluster-a",
			allNodes:    []string{"10.0.0.10:22", "10.0.0.11:22"},
			firstMaster: "10.0.0.10:22",
		},
		remoteExec: fakeRemote,
	}
	executor.applyDefaults()

	images, err := executor.kubeadmBootstrapImages("10.0.0.10:22")
	if err != nil {
		t.Fatalf("kubeadmBootstrapImages() error = %v", err)
	}
	got := strings.Join(images, "\n")
	for _, image := range []string{
		"registry.k8s.io/kube-apiserver:v1.30.3",
		"registry.k8s.io/kube-proxy:v1.30.3",
		"registry.k8s.io/pause:3.8",
	} {
		if !strings.Contains(got, image) {
			t.Fatalf("kubeadmBootstrapImages() missing %q\nimages:\n%s", image, got)
		}
	}
}

func TestImportKubeadmBootstrapImagesCopiesArchiveToRemoteHost(t *testing.T) {
	archivePath := filepath.Join(t.TempDir(), "images.tar")
	if err := os.WriteFile(archivePath, []byte("archive"), 0o644); err != nil {
		t.Fatalf("WriteFile(archive) error = %v", err)
	}

	fakeRemote := &fakeApplyRemoteExecutor{}
	executor := &bundleExecutor{
		clusterName: "cluster-a",
		topology: &clusterExecutionTopology{
			clusterName: "cluster-a",
			allNodes:    []string{"10.0.0.10:22", "10.0.0.11:22"},
			firstMaster: "10.0.0.10:22",
		},
		remoteExec: fakeRemote,
	}
	executor.applyDefaults()

	if err := executor.importKubeadmBootstrapImages("10.0.0.11:22", archivePath); err != nil {
		t.Fatalf("importKubeadmBootstrapImages() error = %v", err)
	}
	if len(fakeRemote.copyOps) != 1 {
		t.Fatalf("len(copyOps) = %d, want 1", len(fakeRemote.copyOps))
	}
	if got, want := fakeRemote.copyOps[0].dst, "/tmp/images.tar"; got != want {
		t.Fatalf("copy dst = %q, want %q", got, want)
	}
	commands := strings.Join(fakeRemote.commandsForHost("10.0.0.11:22"), "\n")
	if !strings.Contains(commands, "ctr' '-n' 'k8s.io' 'images' 'import' '/tmp/images.tar'") {
		t.Fatalf("remote command stream missing ctr import\ncommands:\n%s", commands)
	}
}

func TestProvisioningHostsForComponentSkipsJoinedNodes(t *testing.T) {
	fakeRemote := &fakeApplyRemoteExecutor{
		outputs: []fakeRemoteCommandOutput{
			{host: "10.0.0.10:22", contains: "/etc/kubernetes/kubelet.conf", output: "joined\n"},
			{host: "10.0.0.11:22", contains: "/etc/kubernetes/kubelet.conf", output: ""},
			{host: "10.0.0.12:22", contains: "/etc/kubernetes/kubelet.conf", output: "joined\n"},
		},
	}

	executor := &bundleExecutor{
		clusterName: "cluster-a",
		topology: &clusterExecutionTopology{
			clusterName: "cluster-a",
			allNodes:    []string{"10.0.0.10:22", "10.0.0.11:22", "10.0.0.12:22"},
			firstMaster: "10.0.0.10:22",
		},
		remoteExec: fakeRemote,
	}
	executor.applyDefaults()

	hosts, err := executor.provisioningHostsForComponent(hydrate.RenderedComponent{
		Name:        "kubernetes",
		PackageName: "kubernetes-rootfs",
	}, []string{"10.0.0.10:22", "10.0.0.11:22", "10.0.0.12:22"})
	if err != nil {
		t.Fatalf("provisioningHostsForComponent() error = %v", err)
	}
	if got, want := strings.Join(hosts, ","), "10.0.0.11:22"; got != want {
		t.Fatalf("provisioning hosts = %q, want %q", got, want)
	}
}

func TestRolloutHostBatches(t *testing.T) {
	t.Parallel()

	executor := &bundleExecutor{
		rollout: RolloutStrategy{BatchSize: 2},
	}
	batches := executor.rolloutHostBatches([]string{"host-a", "host-b", "host-c"})
	if got, want := len(batches), 2; got != want {
		t.Fatalf("len(batches) = %d, want %d", got, want)
	}
	if got, want := strings.Join(batches[0], ","), "host-a,host-b"; got != want {
		t.Fatalf("batch 0 = %q, want %q", got, want)
	}
	if got, want := strings.Join(batches[1], ","), "host-c"; got != want {
		t.Fatalf("batch 1 = %q, want %q", got, want)
	}

	executor.rollout.BatchSize = 0
	batches = executor.rolloutHostBatches([]string{"host-a", "host-b"})
	if got, want := len(batches), 1; got != want {
		t.Fatalf("default len(batches) = %d, want %d", got, want)
	}
	if got, want := strings.Join(batches[0], ","), "host-a,host-b"; got != want {
		t.Fatalf("default batch = %q, want %q", got, want)
	}

	executor.rollout = RolloutStrategy{BatchSize: 2, Canary: RolloutCanary{BatchSize: 3}}
	batches = executor.rolloutHostBatches([]string{"host-a", "host-b", "host-c", "host-d", "host-e"})
	if got, want := len(batches), 2; got != want {
		t.Fatalf("canary len(batches) = %d, want %d", got, want)
	}
	if got, want := strings.Join(batches[0], ","), "host-a,host-b,host-c"; got != want {
		t.Fatalf("canary batch 0 = %q, want %q", got, want)
	}
	if got, want := strings.Join(batches[1], ","), "host-d,host-e"; got != want {
		t.Fatalf("canary batch 1 = %q, want %q", got, want)
	}
}

func TestRolloutPauseAfterCanary(t *testing.T) {
	t.Parallel()

	executor := &bundleExecutor{
		rollout: RolloutStrategy{
			BatchSize: 2,
			Canary:    RolloutCanary{BatchSize: 1},
			Pause:     RolloutPause{AfterCanary: true},
		},
		stderr: io.Discard,
	}
	var visited []string
	err := executor.forEachHostBatch([]string{"host-a", "host-b", "host-c"}, func(batch []string) error {
		visited = append(visited, batch...)
		return nil
	})
	if !IsRolloutPaused(err) {
		t.Fatalf("forEachHostBatch() error = %v, want rollout paused", err)
	}
	if got, want := strings.Join(visited, ","), "host-a"; got != want {
		t.Fatalf("visited hosts = %q, want %q", got, want)
	}
}

func TestApplyRolloutBatchSizeLogsHostWaves(t *testing.T) {
	previousRuntimeRoot := constants.DefaultRuntimeRootDir
	constants.DefaultRuntimeRootDir = t.TempDir()
	t.Cleanup(func() {
		constants.DefaultRuntimeRootDir = previousRuntimeRoot
	})

	previousLoadTopology := loadApplyExecutionTopology
	previousNewRemoteExecutor := newApplyRemoteExecutor
	t.Cleanup(func() {
		loadApplyExecutionTopology = previousLoadTopology
		newApplyRemoteExecutor = previousNewRemoteExecutor
	})

	fakeRemote := &fakeApplyRemoteExecutor{}
	loadApplyExecutionTopology = func(clusterName string) (*clusterExecutionTopology, error) {
		return &clusterExecutionTopology{
			clusterName: clusterName,
			allNodes:    []string{"10.0.0.10:22", "10.0.0.11:22", "10.0.0.12:22"},
			firstMaster: "10.0.0.10:22",
		}, nil
	}
	newApplyRemoteExecutor = func(topology *clusterExecutionTopology) (applyRemoteExecutor, error) {
		return fakeRemote, nil
	}

	bundleDir := filepath.Join(t.TempDir(), "bundle")
	writeExecutable(t, filepath.Join(bundleDir, "components", "runtime", "files", "hooks", "preflight.sh"), "#!/bin/sh\nexit 0\n")
	writeFile(t, filepath.Join(bundleDir, "components", "runtime", "files", "rootfs", "usr", "bin", "demo"), "#!/bin/sh\nexit 0\n", 0o755)
	bundle := &hydrate.Bundle{
		APIVersion: distribution.APIVersion,
		Kind:       distribution.KindHydratedBundle,
		Spec: hydrate.BundleSpec{
			BOMName:  "rollout-runtime",
			Revision: "rev-1",
			Channel:  bom.ChannelAlpha,
			ExecutionTopology: hydrate.ExecutionTopology{
				AllNodes:    []string{"10.0.0.10:22", "10.0.0.11:22", "10.0.0.12:22"},
				FirstMaster: "10.0.0.10:22",
			},
			Components: []hydrate.RenderedComponent{{
				Name:        "runtime",
				PackageName: "runtime-rootfs",
				Version:     "v0.1.0",
				Class:       packageformat.ClassRootfs,
				RootPath:    "components/runtime/files",
				Steps: []hydrate.RenderedStep{
					{
						Name:        "runtime-rootfs",
						Kind:        hydrate.StepContent,
						BundlePath:  "components/runtime/files/rootfs",
						SourcePath:  "rootfs/",
						ContentType: packageformat.ContentRootfs,
					},
					{
						Name:           "preflight",
						Kind:           hydrate.StepHook,
						BundlePath:     "components/runtime/files/hooks/preflight.sh",
						SourcePath:     "hooks/preflight.sh",
						HookPhase:      packageformat.PhaseBootstrap,
						Target:         packageformat.TargetAllNodes,
						TimeoutSeconds: 5,
					},
				},
			}},
		},
	}
	if err := yamlutil.MarshalFile(filepath.Join(bundleDir, hydrate.BundleFileName), bundle); err != nil {
		t.Fatalf("MarshalFile(bundle) error = %v", err)
	}
	persistRenderedStateForBundle(t, "rollout-cluster", bundleDir, bundle)

	stderr := bytes.NewBuffer(nil)
	if _, err := Apply(ApplyOptions{
		ClusterName: "rollout-cluster",
		BundlePath:  bundleDir,
		Stderr:      stderr,
		Rollout: RolloutStrategy{
			BatchSize: 1,
		},
	}); err != nil {
		t.Fatalf("Apply() error = %v", err)
	}
	logText := stderr.String()
	first := strings.Index(logText, "running rollout batch 1/3")
	second := strings.Index(logText, "running rollout batch 2/3")
	third := strings.Index(logText, "running rollout batch 3/3")
	if first < 0 || second < 0 || third < 0 {
		t.Fatalf("rollout batch logs missing\nlog:\n%s", logText)
	}
	if !(first < second && second < third) {
		t.Fatalf("rollout batch logs out of order\nlog:\n%s", logText)
	}
}

func TestApplyRolloutBatchSizeCompletesHostWaveBeforeNext(t *testing.T) {
	previousRuntimeRoot := constants.DefaultRuntimeRootDir
	constants.DefaultRuntimeRootDir = t.TempDir()
	t.Cleanup(func() {
		constants.DefaultRuntimeRootDir = previousRuntimeRoot
	})

	previousLoadTopology := loadApplyExecutionTopology
	previousNewRemoteExecutor := newApplyRemoteExecutor
	t.Cleanup(func() {
		loadApplyExecutionTopology = previousLoadTopology
		newApplyRemoteExecutor = previousNewRemoteExecutor
	})

	fakeRemote := &fakeApplyRemoteExecutor{}
	loadApplyExecutionTopology = func(clusterName string) (*clusterExecutionTopology, error) {
		return &clusterExecutionTopology{
			clusterName: clusterName,
			allNodes:    []string{"10.0.0.10:22", "10.0.0.11:22"},
			firstMaster: "10.0.0.10:22",
		}, nil
	}
	newApplyRemoteExecutor = func(topology *clusterExecutionTopology) (applyRemoteExecutor, error) {
		return fakeRemote, nil
	}

	bundleDir := filepath.Join(t.TempDir(), "bundle")
	writeExecutable(t, filepath.Join(bundleDir, "components", "runtime", "files", "hooks", "preflight.sh"), "#!/bin/sh\nexit 0\n")
	writeExecutable(t, filepath.Join(bundleDir, "components", "runtime", "files", "hooks", "configure.sh"), "#!/bin/sh\nexit 0\n")
	writeFile(t, filepath.Join(bundleDir, "components", "runtime", "files", "rootfs", "usr", "bin", "demo"), "#!/bin/sh\nexit 0\n", 0o755)
	bundle := &hydrate.Bundle{
		APIVersion: distribution.APIVersion,
		Kind:       distribution.KindHydratedBundle,
		Spec: hydrate.BundleSpec{
			BOMName:  "rollout-runtime",
			Revision: "rev-1",
			Channel:  bom.ChannelAlpha,
			ExecutionTopology: hydrate.ExecutionTopology{
				AllNodes:    []string{"10.0.0.10:22", "10.0.0.11:22"},
				FirstMaster: "10.0.0.10:22",
			},
			Components: []hydrate.RenderedComponent{{
				Name:        "runtime",
				PackageName: "runtime-rootfs",
				Version:     "v0.1.0",
				Class:       packageformat.ClassRootfs,
				RootPath:    "components/runtime/files",
				Steps: []hydrate.RenderedStep{
					{
						Name:        "runtime-rootfs",
						Kind:        hydrate.StepContent,
						BundlePath:  "components/runtime/files/rootfs",
						SourcePath:  "rootfs/",
						ContentType: packageformat.ContentRootfs,
					},
					{
						Name:           "preflight",
						Kind:           hydrate.StepHook,
						BundlePath:     "components/runtime/files/hooks/preflight.sh",
						SourcePath:     "hooks/preflight.sh",
						HookPhase:      packageformat.PhaseBootstrap,
						Target:         packageformat.TargetAllNodes,
						TimeoutSeconds: 5,
					},
					{
						Name:           "configure",
						Kind:           hydrate.StepHook,
						BundlePath:     "components/runtime/files/hooks/configure.sh",
						SourcePath:     "hooks/configure.sh",
						HookPhase:      packageformat.PhaseConfigure,
						Target:         packageformat.TargetAllNodes,
						TimeoutSeconds: 5,
					},
				},
			}},
		},
	}
	if err := yamlutil.MarshalFile(filepath.Join(bundleDir, hydrate.BundleFileName), bundle); err != nil {
		t.Fatalf("MarshalFile(bundle) error = %v", err)
	}
	persistRenderedStateForBundle(t, "rollout-wave-cluster", bundleDir, bundle)

	if _, err := Apply(ApplyOptions{
		ClusterName: "rollout-wave-cluster",
		BundlePath:  bundleDir,
		Rollout: RolloutStrategy{
			BatchSize: 1,
		},
	}); err != nil {
		t.Fatalf("Apply() error = %v", err)
	}
	firstConfigure := -1
	secondPreflight := -1
	for i, op := range fakeRemote.cmdOps {
		switch {
		case op.host == "10.0.0.10:22" && strings.Contains(op.cmd, "configure.sh") && firstConfigure < 0:
			firstConfigure = i
		case op.host == "10.0.0.11:22" && strings.Contains(op.cmd, "preflight.sh") && secondPreflight < 0:
			secondPreflight = i
		}
	}
	if firstConfigure < 0 || secondPreflight < 0 {
		t.Fatalf("missing expected rollout hook commands\ncommands:\n%s", strings.Join(fakeRemote.allCommands(), "\n"))
	}
	if !(firstConfigure < secondPreflight) {
		t.Fatalf("second rollout wave started before first wave completed configure\ncommands:\n%s", strings.Join(fakeRemote.allCommands(), "\n"))
	}
}

func TestApplyRolloutHealthGateRunsHealthAfterEachHostWave(t *testing.T) {
	previousRuntimeRoot := constants.DefaultRuntimeRootDir
	constants.DefaultRuntimeRootDir = t.TempDir()
	t.Cleanup(func() {
		constants.DefaultRuntimeRootDir = previousRuntimeRoot
	})

	previousLoadTopology := loadApplyExecutionTopology
	previousNewRemoteExecutor := newApplyRemoteExecutor
	t.Cleanup(func() {
		loadApplyExecutionTopology = previousLoadTopology
		newApplyRemoteExecutor = previousNewRemoteExecutor
	})

	fakeRemote := &fakeApplyRemoteExecutor{}
	loadApplyExecutionTopology = func(clusterName string) (*clusterExecutionTopology, error) {
		return &clusterExecutionTopology{
			clusterName: clusterName,
			allNodes:    []string{"10.0.0.10:22", "10.0.0.11:22"},
			firstMaster: "10.0.0.10:22",
		}, nil
	}
	newApplyRemoteExecutor = func(topology *clusterExecutionTopology) (applyRemoteExecutor, error) {
		return fakeRemote, nil
	}

	bundleDir := filepath.Join(t.TempDir(), "bundle")
	writeExecutable(t, filepath.Join(bundleDir, "components", "runtime", "files", "hooks", "configure.sh"), "#!/bin/sh\nexit 0\n")
	writeExecutable(t, filepath.Join(bundleDir, "components", "runtime", "files", "hooks", "healthcheck.sh"), "#!/bin/sh\nexit 0\n")
	writeFile(t, filepath.Join(bundleDir, "components", "runtime", "files", "rootfs", "usr", "bin", "demo"), "#!/bin/sh\nexit 0\n", 0o755)
	bundle := &hydrate.Bundle{
		APIVersion: distribution.APIVersion,
		Kind:       distribution.KindHydratedBundle,
		Spec: hydrate.BundleSpec{
			BOMName:  "rollout-health-gate-runtime",
			Revision: "rev-1",
			Channel:  bom.ChannelAlpha,
			ExecutionTopology: hydrate.ExecutionTopology{
				AllNodes:    []string{"10.0.0.10:22", "10.0.0.11:22"},
				FirstMaster: "10.0.0.10:22",
			},
			Components: []hydrate.RenderedComponent{{
				Name:        "runtime",
				PackageName: "runtime-rootfs",
				Version:     "v0.1.0",
				Class:       packageformat.ClassRootfs,
				RootPath:    "components/runtime/files",
				Steps: []hydrate.RenderedStep{
					{
						Name:        "runtime-rootfs",
						Kind:        hydrate.StepContent,
						BundlePath:  "components/runtime/files/rootfs",
						SourcePath:  "rootfs/",
						ContentType: packageformat.ContentRootfs,
					},
					{
						Name:           "configure",
						Kind:           hydrate.StepHook,
						BundlePath:     "components/runtime/files/hooks/configure.sh",
						SourcePath:     "hooks/configure.sh",
						HookPhase:      packageformat.PhaseConfigure,
						Target:         packageformat.TargetAllNodes,
						TimeoutSeconds: 5,
					},
					{
						Name:           "healthcheck",
						Kind:           hydrate.StepHook,
						BundlePath:     "components/runtime/files/hooks/healthcheck.sh",
						SourcePath:     "hooks/healthcheck.sh",
						HookPhase:      packageformat.PhaseHealth,
						Target:         packageformat.TargetAllNodes,
						TimeoutSeconds: 5,
					},
				},
			}},
		},
	}
	if err := yamlutil.MarshalFile(filepath.Join(bundleDir, hydrate.BundleFileName), bundle); err != nil {
		t.Fatalf("MarshalFile(bundle) error = %v", err)
	}
	persistRenderedStateForBundle(t, "rollout-health-gate-cluster", bundleDir, bundle)

	if _, err := Apply(ApplyOptions{
		ClusterName: "rollout-health-gate-cluster",
		BundlePath:  bundleDir,
		Rollout: RolloutStrategy{
			BatchSize:  1,
			HealthGate: true,
		},
	}); err != nil {
		t.Fatalf("Apply() error = %v", err)
	}

	firstConfigure := -1
	firstHealth := -1
	secondConfigure := -1
	secondHealth := -1
	for i, op := range fakeRemote.cmdOps {
		switch {
		case op.host == "10.0.0.10:22" && strings.Contains(op.cmd, "configure.sh") && firstConfigure < 0:
			firstConfigure = i
		case op.host == "10.0.0.10:22" && strings.Contains(op.cmd, "healthcheck.sh") && firstHealth < 0:
			firstHealth = i
		case op.host == "10.0.0.11:22" && strings.Contains(op.cmd, "configure.sh") && secondConfigure < 0:
			secondConfigure = i
		case op.host == "10.0.0.11:22" && strings.Contains(op.cmd, "healthcheck.sh") && secondHealth < 0:
			secondHealth = i
		}
	}
	if firstConfigure < 0 || firstHealth < 0 || secondConfigure < 0 || secondHealth < 0 {
		t.Fatalf("missing expected health-gated rollout hooks\ncommands:\n%s", strings.Join(fakeRemote.allCommands(), "\n"))
	}
	if !(firstConfigure < firstHealth && firstHealth < secondConfigure && secondConfigure < secondHealth) {
		t.Fatalf("health-gated rollout order is wrong\ncommands:\n%s", strings.Join(fakeRemote.allCommands(), "\n"))
	}
}

func TestApplyRolloutCanaryPauseStopsBeforeLaterHosts(t *testing.T) {
	previousRuntimeRoot := constants.DefaultRuntimeRootDir
	constants.DefaultRuntimeRootDir = t.TempDir()
	t.Cleanup(func() {
		constants.DefaultRuntimeRootDir = previousRuntimeRoot
	})

	previousLoadTopology := loadApplyExecutionTopology
	previousNewRemoteExecutor := newApplyRemoteExecutor
	t.Cleanup(func() {
		loadApplyExecutionTopology = previousLoadTopology
		newApplyRemoteExecutor = previousNewRemoteExecutor
	})

	fakeRemote := &fakeApplyRemoteExecutor{}
	loadApplyExecutionTopology = func(clusterName string) (*clusterExecutionTopology, error) {
		return &clusterExecutionTopology{
			clusterName: clusterName,
			allNodes:    []string{"10.0.0.10:22", "10.0.0.11:22", "10.0.0.12:22"},
			firstMaster: "10.0.0.10:22",
		}, nil
	}
	newApplyRemoteExecutor = func(topology *clusterExecutionTopology) (applyRemoteExecutor, error) {
		return fakeRemote, nil
	}

	bundleDir := filepath.Join(t.TempDir(), "bundle")
	writeExecutable(t, filepath.Join(bundleDir, "components", "runtime", "files", "hooks", "configure.sh"), "#!/bin/sh\nexit 0\n")
	writeFile(t, filepath.Join(bundleDir, "components", "runtime", "files", "rootfs", "usr", "bin", "demo"), "#!/bin/sh\nexit 0\n", 0o755)
	bundle := rolloutRuntimeBundle("rollout-canary-pause-runtime", "rev-1", []string{"10.0.0.10:22", "10.0.0.11:22", "10.0.0.12:22"}, []hydrate.RenderedStep{
		{
			Name:        "runtime-rootfs",
			Kind:        hydrate.StepContent,
			BundlePath:  "components/runtime/files/rootfs",
			SourcePath:  "rootfs/",
			ContentType: packageformat.ContentRootfs,
		},
		{
			Name:           "configure",
			Kind:           hydrate.StepHook,
			BundlePath:     "components/runtime/files/hooks/configure.sh",
			SourcePath:     "hooks/configure.sh",
			HookPhase:      packageformat.PhaseConfigure,
			Target:         packageformat.TargetAllNodes,
			TimeoutSeconds: 5,
		},
	})
	if err := yamlutil.MarshalFile(filepath.Join(bundleDir, hydrate.BundleFileName), bundle); err != nil {
		t.Fatalf("MarshalFile(bundle) error = %v", err)
	}
	persistRenderedStateForBundle(t, "rollout-canary-pause-cluster", bundleDir, bundle)

	stderr := bytes.NewBuffer(nil)
	_, err := Apply(ApplyOptions{
		ClusterName: "rollout-canary-pause-cluster",
		BundlePath:  bundleDir,
		Stderr:      stderr,
		Rollout: RolloutStrategy{
			BatchSize: 2,
			Canary:    RolloutCanary{BatchSize: 1},
			Pause:     RolloutPause{AfterCanary: true},
		},
	})
	if !IsRolloutPaused(err) {
		t.Fatalf("Apply() error = %v, want rollout paused", err)
	}
	if got := strings.Join(fakeRemote.commandsForHost("10.0.0.11:22"), "\n"); strings.Contains(got, "configure.sh") {
		t.Fatalf("non-canary host was configured before pause\ncommands:\n%s", strings.Join(fakeRemote.allCommands(), "\n"))
	}
	if !strings.Contains(stderr.String(), "running rollout batch 1/2 (canary)") {
		t.Fatalf("canary batch log missing\nlog:\n%s", stderr.String())
	}
}

func TestApplyRolloutBatchSizeDoesNotRepeatScopedHooks(t *testing.T) {
	previousRuntimeRoot := constants.DefaultRuntimeRootDir
	constants.DefaultRuntimeRootDir = t.TempDir()
	t.Cleanup(func() {
		constants.DefaultRuntimeRootDir = previousRuntimeRoot
	})

	previousLoadTopology := loadApplyExecutionTopology
	previousNewRemoteExecutor := newApplyRemoteExecutor
	t.Cleanup(func() {
		loadApplyExecutionTopology = previousLoadTopology
		newApplyRemoteExecutor = previousNewRemoteExecutor
	})

	fakeRemote := &fakeApplyRemoteExecutor{}
	loadApplyExecutionTopology = func(clusterName string) (*clusterExecutionTopology, error) {
		return &clusterExecutionTopology{
			clusterName: clusterName,
			allNodes:    []string{"10.0.0.10:22", "10.0.0.11:22", "10.0.0.12:22"},
			firstMaster: "10.0.0.10:22",
		}, nil
	}
	newApplyRemoteExecutor = func(topology *clusterExecutionTopology) (applyRemoteExecutor, error) {
		return fakeRemote, nil
	}

	bundleDir := filepath.Join(t.TempDir(), "bundle")
	writeExecutable(t, filepath.Join(bundleDir, "components", "runtime", "files", "hooks", "configure-first.sh"), "#!/bin/sh\nexit 0\n")
	writeFile(t, filepath.Join(bundleDir, "components", "runtime", "files", "rootfs", "usr", "bin", "demo"), "#!/bin/sh\nexit 0\n", 0o755)
	bundle := &hydrate.Bundle{
		APIVersion: distribution.APIVersion,
		Kind:       distribution.KindHydratedBundle,
		Spec: hydrate.BundleSpec{
			BOMName:  "rollout-scoped-runtime",
			Revision: "rev-1",
			Channel:  bom.ChannelAlpha,
			ExecutionTopology: hydrate.ExecutionTopology{
				AllNodes:    []string{"10.0.0.10:22", "10.0.0.11:22", "10.0.0.12:22"},
				FirstMaster: "10.0.0.10:22",
			},
			Components: []hydrate.RenderedComponent{{
				Name:        "runtime",
				PackageName: "runtime-rootfs",
				Version:     "v0.1.0",
				Class:       packageformat.ClassRootfs,
				RootPath:    "components/runtime/files",
				Steps: []hydrate.RenderedStep{
					{
						Name:        "runtime-rootfs",
						Kind:        hydrate.StepContent,
						BundlePath:  "components/runtime/files/rootfs",
						SourcePath:  "rootfs/",
						ContentType: packageformat.ContentRootfs,
					},
					{
						Name:           "configure-first",
						Kind:           hydrate.StepHook,
						BundlePath:     "components/runtime/files/hooks/configure-first.sh",
						SourcePath:     "hooks/configure-first.sh",
						HookPhase:      packageformat.PhaseConfigure,
						Target:         packageformat.TargetFirstMaster,
						TimeoutSeconds: 5,
					},
				},
			}},
		},
	}
	if err := yamlutil.MarshalFile(filepath.Join(bundleDir, hydrate.BundleFileName), bundle); err != nil {
		t.Fatalf("MarshalFile(bundle) error = %v", err)
	}
	persistRenderedStateForBundle(t, "rollout-scoped-cluster", bundleDir, bundle)

	if _, err := Apply(ApplyOptions{
		ClusterName: "rollout-scoped-cluster",
		BundlePath:  bundleDir,
		Rollout: RolloutStrategy{
			BatchSize: 1,
		},
	}); err != nil {
		t.Fatalf("Apply() error = %v", err)
	}

	for _, host := range []string{"10.0.0.10:22", "10.0.0.11:22", "10.0.0.12:22"} {
		count := strings.Count(strings.Join(fakeRemote.commandsForHost(host), "\n"), "configure-first.sh")
		if host == "10.0.0.10:22" {
			if count != 1 {
				t.Fatalf("first master configure-first hook count = %d, want 1\ncommands:\n%s", count, strings.Join(fakeRemote.allCommands(), "\n"))
			}
			continue
		}
		if count != 0 {
			t.Fatalf("non-first-master %q configure-first hook count = %d, want 0\ncommands:\n%s", host, count, strings.Join(fakeRemote.allCommands(), "\n"))
		}
	}
}

func TestApplyRolloutFailureActionRollbackRestoresLastSuccessfulBundle(t *testing.T) {
	previousRuntimeRoot := constants.DefaultRuntimeRootDir
	constants.DefaultRuntimeRootDir = t.TempDir()
	t.Cleanup(func() {
		constants.DefaultRuntimeRootDir = previousRuntimeRoot
	})

	clusterName := "rollout-rollback-cluster"
	tmpDir := t.TempDir()
	hostRoot := filepath.Join(tmpDir, "host")
	kubeconfigPath := filepath.Join(tmpDir, "admin.conf")
	if err := os.WriteFile(kubeconfigPath, []byte("apiVersion: v1\nkind: Config\n"), 0o644); err != nil {
		t.Fatalf("WriteFile(kubeconfig) error = %v", err)
	}
	writeExecutable(t, filepath.Join(tmpDir, "bin", "kubectl"), `#!/bin/sh
if [ "$1" = "--kubeconfig" ]; then
  shift 2
fi
case "$1" in
  get)
    if [ "$2" = "--raw=/readyz" ]; then
      exit 0
    fi
    if [ "$2" = "nodes" ]; then
      printf 'node-a'
      exit 0
    fi
    ;;
  taint|apply)
    exit 0
    ;;
esac
exit 0
`)
	writeExecutable(t, filepath.Join(tmpDir, "bin", "systemctl"), "#!/bin/sh\nexit 0\n")
	t.Setenv("PATH", filepath.Join(tmpDir, "bin")+":"+os.Getenv("PATH"))

	previousBundleDir := filepath.Join(tmpDir, "previous-bundle")
	previousBundle := localRuntimeBundle("rollback-runtime", "rev-1")
	writeLocalRuntimeBundle(t, previousBundleDir, previousBundle, "previous\n", "#!/bin/sh\nexit 0\n")
	previousDigest, err := hydrate.DigestBundle(previousBundleDir)
	if err != nil {
		t.Fatalf("DigestBundle(previous) error = %v", err)
	}
	previousRevisionPath, err := RevisionBundlePath(clusterName, previousDigest.String())
	if err != nil {
		t.Fatalf("RevisionBundlePath(previous) error = %v", err)
	}
	if err := copyDir(previousBundleDir, previousRevisionPath); err != nil {
		t.Fatalf("copy previous revision bundle error = %v", err)
	}
	if err := mirrorBundle(previousRevisionPath, CurrentBundlePath(clusterName)); err != nil {
		t.Fatalf("mirror previous bundle error = %v", err)
	}
	if _, err := state.PersistSuccessfulApply(
		clusterName,
		state.BOMReference{
			Name:     previousBundle.Spec.BOMName,
			Revision: previousBundle.Spec.Revision,
			Channel:  previousBundle.Spec.Channel,
			Digest:   "sha256:1111111111111111111111111111111111111111111111111111111111111111",
		},
		previousDigest.String(),
		"",
		"local-rev-1",
	); err != nil {
		t.Fatalf("PersistSuccessfulApply(previous) error = %v", err)
	}

	nextBundleDir := filepath.Join(tmpDir, "next-bundle")
	nextBundle := localRuntimeBundle("rollback-runtime", "rev-2")
	writeLocalRuntimeBundle(t, nextBundleDir, nextBundle, "next\n", "#!/bin/sh\nexit 1\n")
	if err := mirrorBundle(nextBundleDir, CurrentBundlePath(clusterName)); err != nil {
		t.Fatalf("mirror next bundle error = %v", err)
	}
	persistRenderedStateForBundle(t, clusterName, nextBundleDir, nextBundle)

	result, err := Apply(ApplyOptions{
		ClusterName:    clusterName,
		BundlePath:     nextBundleDir,
		KubeconfigPath: kubeconfigPath,
		HostRoot:       hostRoot,
		Rollout: RolloutStrategy{
			FailureAction: RolloutFailureActionRollback,
		},
	})
	if !IsRolloutRolledBack(err) {
		t.Fatalf("Apply() error = %v, want rollback error", err)
	}
	if result == nil || result.AppliedRevision == nil {
		t.Fatal("Apply() result/appliedRevision = nil, want rollback result")
	}
	if got, want := result.AppliedRevision.Spec.BOM.Revision, "rev-1"; got != want {
		t.Fatalf("rolled back revision = %q, want %q", got, want)
	}
	content, err := os.ReadFile(filepath.Join(hostRoot, "etc", "demo", "version"))
	if err != nil {
		t.Fatalf("ReadFile(host version) error = %v", err)
	}
	if got, want := string(content), "previous\n"; got != want {
		t.Fatalf("host version = %q, want %q", got, want)
	}
	loaded, err := state.LoadAppliedRevision(clusterName)
	if err != nil {
		t.Fatalf("LoadAppliedRevision() error = %v", err)
	}
	if got, want := loaded.Spec.DesiredStateDigest, previousDigest.String(); got != want {
		t.Fatalf("loaded.spec.desiredStateDigest = %q, want %q", got, want)
	}
}

func TestApplyRolloutRollbackUsesLastSuccessfulBundleTopology(t *testing.T) {
	previousRuntimeRoot := constants.DefaultRuntimeRootDir
	constants.DefaultRuntimeRootDir = t.TempDir()
	t.Cleanup(func() {
		constants.DefaultRuntimeRootDir = previousRuntimeRoot
	})

	previousLoadTopology := loadApplyExecutionTopology
	previousNewRemoteExecutor := newApplyRemoteExecutor
	t.Cleanup(func() {
		loadApplyExecutionTopology = previousLoadTopology
		newApplyRemoteExecutor = previousNewRemoteExecutor
	})

	clusterName := "rollout-rollback-topology-cluster"
	currentHost := "10.0.0.20:22"
	previousHost := "10.0.0.10:22"
	loadApplyExecutionTopology = func(clusterName string) (*clusterExecutionTopology, error) {
		return &clusterExecutionTopology{
			clusterName: clusterName,
			allNodes:    []string{currentHost},
			firstMaster: currentHost,
			cluster: &v1beta1.Cluster{
				Spec: v1beta1.ClusterSpec{
					Hosts: []v1beta1.Host{
						{IPS: []string{currentHost}, Roles: []string{v1beta1.MASTER}},
					},
				},
			},
		}, nil
	}
	fakeRemote := &fakeApplyRemoteExecutor{
		errors: []fakeRemoteCommandError{
			{host: currentHost, contains: "fail-new.sh", err: os.ErrPermission},
		},
	}
	newApplyRemoteExecutor = func(topology *clusterExecutionTopology) (applyRemoteExecutor, error) {
		return fakeRemote, nil
	}

	tmpDir := t.TempDir()
	previousBundleDir := filepath.Join(tmpDir, "previous-bundle")
	previousBundle := rollbackTopologyRuntimeBundle("rollback-topology-runtime", "rev-1", previousHost, "restore-old.sh")
	writeRollbackTopologyRuntimeBundle(t, previousBundleDir, previousBundle, "restore-old.sh")
	previousDigest, err := hydrate.DigestBundle(previousBundleDir)
	if err != nil {
		t.Fatalf("DigestBundle(previous) error = %v", err)
	}
	previousRevisionPath, err := RevisionBundlePath(clusterName, previousDigest.String())
	if err != nil {
		t.Fatalf("RevisionBundlePath(previous) error = %v", err)
	}
	if err := copyDir(previousBundleDir, previousRevisionPath); err != nil {
		t.Fatalf("copy previous revision bundle error = %v", err)
	}
	if err := mirrorBundle(previousRevisionPath, CurrentBundlePath(clusterName)); err != nil {
		t.Fatalf("mirror previous bundle error = %v", err)
	}
	if _, err := state.PersistSuccessfulApply(
		clusterName,
		state.BOMReference{
			Name:     previousBundle.Spec.BOMName,
			Revision: previousBundle.Spec.Revision,
			Channel:  previousBundle.Spec.Channel,
			Digest:   "sha256:1111111111111111111111111111111111111111111111111111111111111111",
		},
		previousDigest.String(),
		"",
		"local-rev-1",
	); err != nil {
		t.Fatalf("PersistSuccessfulApply(previous) error = %v", err)
	}

	nextBundleDir := filepath.Join(tmpDir, "next-bundle")
	nextBundle := rollbackTopologyRuntimeBundle("rollback-topology-runtime", "rev-2", currentHost, "fail-new.sh")
	writeRollbackTopologyRuntimeBundle(t, nextBundleDir, nextBundle, "fail-new.sh")
	if err := mirrorBundle(nextBundleDir, CurrentBundlePath(clusterName)); err != nil {
		t.Fatalf("mirror next bundle error = %v", err)
	}
	persistRenderedStateForBundle(t, clusterName, nextBundleDir, nextBundle)

	_, err = Apply(ApplyOptions{
		ClusterName: clusterName,
		BundlePath:  nextBundleDir,
		Rollout: RolloutStrategy{
			FailureAction: RolloutFailureActionRollback,
		},
	})
	if !IsRolloutRolledBack(err) {
		t.Fatalf("Apply() error = %v, want rollback error", err)
	}
	if got := strings.Join(fakeRemote.commandsForHost(previousHost), "\n"); !strings.Contains(got, "restore-old.sh") {
		t.Fatalf("rollback did not use previous bundle host %q\ncommands:\n%s", previousHost, strings.Join(fakeRemote.allCommands(), "\n"))
	}
	if got := strings.Join(fakeRemote.commandsForHost(currentHost), "\n"); strings.Contains(got, "restore-old.sh") {
		t.Fatalf("rollback reused failed revision host %q\ncommands:\n%s", currentHost, strings.Join(fakeRemote.allCommands(), "\n"))
	}
}

func persistRenderedStateForBundle(t *testing.T, clusterName, bundleDir string, bundle *hydrate.Bundle) {
	t.Helper()

	bundleDigest, err := hydrate.DigestBundle(bundleDir)
	if err != nil {
		t.Fatalf("DigestBundle() error = %v", err)
	}
	if _, err := state.PersistRenderedState(
		clusterName,
		state.BOMReference{
			Name:     bundle.Spec.BOMName,
			Revision: bundle.Spec.Revision,
			Channel:  bundle.Spec.Channel,
			Digest:   "sha256:1111111111111111111111111111111111111111111111111111111111111111",
		},
		bundleDigest.String(),
		"",
		"local-rev-1",
	); err != nil {
		t.Fatalf("PersistRenderedState() error = %v", err)
	}
}

func rolloutRuntimeBundle(name, revision string, hosts []string, steps []hydrate.RenderedStep) *hydrate.Bundle {
	return &hydrate.Bundle{
		APIVersion: distribution.APIVersion,
		Kind:       distribution.KindHydratedBundle,
		Spec: hydrate.BundleSpec{
			BOMName:  name,
			Revision: revision,
			Channel:  bom.ChannelAlpha,
			ExecutionTopology: hydrate.ExecutionTopology{
				AllNodes:    hosts,
				FirstMaster: hosts[0],
			},
			Components: []hydrate.RenderedComponent{{
				Name:        "runtime",
				PackageName: "runtime-rootfs",
				Version:     "v0.1.0",
				Class:       packageformat.ClassRootfs,
				RootPath:    "components/runtime/files",
				Steps:       steps,
			}},
		},
	}
}

func localRuntimeBundle(name, revision string) *hydrate.Bundle {
	return rolloutRuntimeBundle(name, revision, []string{"localhost"}, []hydrate.RenderedStep{
		{
			Name:        "runtime-rootfs",
			Kind:        hydrate.StepContent,
			BundlePath:  "components/runtime/files/rootfs",
			SourcePath:  "rootfs/",
			ContentType: packageformat.ContentRootfs,
		},
		{
			Name:           "configure",
			Kind:           hydrate.StepHook,
			BundlePath:     "components/runtime/files/hooks/configure.sh",
			SourcePath:     "hooks/configure.sh",
			HookPhase:      packageformat.PhaseConfigure,
			Target:         packageformat.TargetAllNodes,
			TimeoutSeconds: 5,
		},
	})
}

func writeLocalRuntimeBundle(t *testing.T, bundleDir string, bundle *hydrate.Bundle, versionContent, configureHook string) {
	t.Helper()
	writeFile(t, filepath.Join(bundleDir, "components", "runtime", "files", "rootfs", "etc", "demo", "version"), versionContent, 0o644)
	writeExecutable(t, filepath.Join(bundleDir, "components", "runtime", "files", "hooks", "configure.sh"), configureHook)
	if err := yamlutil.MarshalFile(filepath.Join(bundleDir, hydrate.BundleFileName), bundle); err != nil {
		t.Fatalf("MarshalFile(bundle) error = %v", err)
	}
}

func rollbackTopologyRuntimeBundle(name, revision, host, hookName string) *hydrate.Bundle {
	return rolloutRuntimeBundle(name, revision, []string{host}, []hydrate.RenderedStep{
		{
			Name:        "runtime-rootfs",
			Kind:        hydrate.StepContent,
			BundlePath:  "components/runtime/files/rootfs",
			SourcePath:  "rootfs/",
			ContentType: packageformat.ContentRootfs,
		},
		{
			Name:           strings.TrimSuffix(hookName, ".sh"),
			Kind:           hydrate.StepHook,
			BundlePath:     "components/runtime/files/hooks/" + hookName,
			SourcePath:     "hooks/" + hookName,
			HookPhase:      packageformat.PhaseConfigure,
			Target:         packageformat.TargetAllNodes,
			TimeoutSeconds: 5,
		},
	})
}

func writeRollbackTopologyRuntimeBundle(t *testing.T, bundleDir string, bundle *hydrate.Bundle, hookName string) {
	t.Helper()
	writeFile(t, filepath.Join(bundleDir, "components", "runtime", "files", "rootfs", "usr", "bin", "demo"), "#!/bin/sh\nexit 0\n", 0o755)
	writeExecutable(t, filepath.Join(bundleDir, "components", "runtime", "files", "hooks", hookName), "#!/bin/sh\nexit 0\n")
	if err := yamlutil.MarshalFile(filepath.Join(bundleDir, hydrate.BundleFileName), bundle); err != nil {
		t.Fatalf("MarshalFile(bundle) error = %v", err)
	}
}

func writeClusterInventory(t *testing.T, clusterName string, hosts []v1beta1.Host) {
	t.Helper()
	cluster := &v1beta1.Cluster{
		Spec: v1beta1.ClusterSpec{
			Hosts: hosts,
		},
	}
	if err := os.MkdirAll(filepath.Dir(constants.Clusterfile(clusterName)), 0o755); err != nil {
		t.Fatalf("MkdirAll(clusterfile dir) error = %v", err)
	}
	if err := yamlutil.MarshalFile(constants.Clusterfile(clusterName), cluster); err != nil {
		t.Fatalf("MarshalFile(clusterfile) error = %v", err)
	}
}

type fakeApplyRemoteExecutor struct {
	copyOps       []fakeRemoteCopy
	fetchOps      []fakeRemoteFetch
	cmdOps        []fakeRemoteCommand
	outputs       []fakeRemoteCommandOutput
	errors        []fakeRemoteCommandError
	fetchContents map[string][]byte
}

type fakeRemoteCopy struct {
	host string
	src  string
	dst  string
	data []byte
}

type fakeRemoteFetch struct {
	host string
	src  string
	dst  string
}

type fakeRemoteCommand struct {
	host string
	cmd  string
}

type fakeRemoteCommandOutput struct {
	host     string
	contains string
	output   string
}

type fakeRemoteCommandError struct {
	host     string
	contains string
	err      error
}

func (f *fakeApplyRemoteExecutor) Copy(host, src, dst string) error {
	var data []byte
	if src != "" {
		content, err := os.ReadFile(src)
		if err == nil {
			data = content
		}
	}
	f.copyOps = append(f.copyOps, fakeRemoteCopy{host: host, src: src, dst: dst, data: data})
	return nil
}

func (f *fakeApplyRemoteExecutor) Fetch(host, src, dst string) error {
	f.fetchOps = append(f.fetchOps, fakeRemoteFetch{host: host, src: src, dst: dst})
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return err
	}
	content := f.fetchContents[host+":"+src]
	if content == nil {
		content = []byte("fetched")
	}
	return os.WriteFile(dst, content, 0o644)
}

func (f *fakeApplyRemoteExecutor) CmdAsyncWithContext(_ context.Context, host string, cmds ...string) error {
	for _, cmd := range cmds {
		f.cmdOps = append(f.cmdOps, fakeRemoteCommand{host: host, cmd: cmd})
		if err := f.errForCommand(host, cmd); err != nil {
			return err
		}
	}
	return nil
}

func (f *fakeApplyRemoteExecutor) CmdToString(host, cmd, _ string) (string, error) {
	f.cmdOps = append(f.cmdOps, fakeRemoteCommand{host: host, cmd: cmd})
	if err := f.errForCommand(host, cmd); err != nil {
		return "", err
	}
	for _, candidate := range f.outputs {
		if candidate.host == host && strings.Contains(cmd, candidate.contains) {
			return candidate.output, nil
		}
	}
	return "", nil
}

func (f *fakeApplyRemoteExecutor) errForCommand(host, cmd string) error {
	for _, candidate := range f.errors {
		if candidate.host == host && strings.Contains(cmd, candidate.contains) {
			return candidate.err
		}
	}
	return nil
}

func (f *fakeApplyRemoteExecutor) commandsForHost(host string) []string {
	var commands []string
	for _, op := range f.cmdOps {
		if op.host == host {
			commands = append(commands, op.cmd)
		}
	}
	return commands
}

func (f *fakeApplyRemoteExecutor) allCommands() []string {
	commands := make([]string, 0, len(f.cmdOps))
	for _, op := range f.cmdOps {
		commands = append(commands, op.host+":"+op.cmd)
	}
	return commands
}

func writeExecutable(t *testing.T, path, content string) {
	t.Helper()
	writeFile(t, path, content, 0o755)
}

func writeFile(t *testing.T, path, content string, mode os.FileMode) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("MkdirAll(%q) error = %v", path, err)
	}
	if err := os.WriteFile(path, []byte(content), mode); err != nil {
		t.Fatalf("WriteFile(%q) error = %v", path, err)
	}
}

func indexOf(text, needle string) int {
	return strings.Index(text, needle)
}
