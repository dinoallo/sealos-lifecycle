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

package hydrate

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/labring/sealos/pkg/distribution/bom"
	"github.com/labring/sealos/pkg/distribution/packageformat"
)

func TestRenderPlanCollectsTrackedK8sObjects(t *testing.T) {
	t.Parallel()

	root := filepath.Join("..", "..", "..", "scripts", "poc", "minimal-single-node", "packages", "cilium")
	pkg, err := packageformat.LoadDir(root)
	if err != nil {
		t.Fatalf("LoadDir() error = %v", err)
	}
	kubernetesRoot := filepath.Join("..", "packageformat", "testdata", "kubernetes-rootfs")
	kubernetesPkg, err := packageformat.LoadDir(kubernetesRoot)
	if err != nil {
		t.Fatalf("LoadDir(kubernetes) error = %v", err)
	}

	localResourceRoot := t.TempDir()
	localResourcePath := filepath.Join(localResourceRoot, "grafana-admin-secret.yaml")
	if err := os.WriteFile(localResourcePath, []byte("apiVersion: v1\nkind: Secret\nmetadata:\n  name: grafana-admin-credentials\n  namespace: default\n"), 0o644); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}
	localPatchPath := filepath.Join(localResourceRoot, "cilium-config.patch.yaml")
	if err := os.WriteFile(localPatchPath, []byte("apiVersion: v1\nkind: ConfigMap\nmetadata:\n  name: cilium-config\n  namespace: kube-system\ndata:\n  enable-hubble: \"true\"\n"), 0o644); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	doc := bom.New("minimal-single-node", "rev-poc-001", bom.ChannelAlpha)
	doc.Spec.Components = []bom.Component{
		{
			Name:    "kubernetes",
			Kind:    "infra",
			Version: "v1.30.3",
			Artifact: bom.ArtifactReference{
				Name:   "kubernetes-rootfs",
				Image:  "registry.example.io/sealos/kubernetes-rootfs:v1.30.3",
				Digest: "sha256:1111111111111111111111111111111111111111111111111111111111111111",
			},
		},
		{
			Name:    "cilium",
			Kind:    "addon",
			Version: "v1.15.0",
			Dependencies: []string{
				"kubernetes",
			},
			Artifact: bom.ArtifactReference{
				Name:   "cilium-cni",
				Image:  "registry.example.io/sealos/cilium-cni:v1.15.0",
				Digest: "sha256:2222222222222222222222222222222222222222222222222222222222222222",
			},
		},
	}

	plan, err := BuildPlanFromResolved(doc, map[string]*packageformat.ComponentPackage{
		"kubernetes": kubernetesPkg,
		"cilium":     pkg,
	})
	if err != nil {
		t.Fatalf("BuildPlanFromResolved() error = %v", err)
	}
	plan.LocalResources = []LocalResource{{Path: localResourcePath, RelativePath: "grafana-admin-secret.yaml"}}
	plan.Components[1].LocalPatches = []LocalPatch{{Path: localPatchPath, RelativePath: "cilium-config.patch.yaml"}}

	out := t.TempDir()
	bundle, err := RenderPlan(plan, SourceMap{
		"kubernetes": kubernetesRoot,
		"cilium":     root,
	}, out)
	if err != nil {
		t.Fatalf("RenderPlan() error = %v", err)
	}

	if got := len(bundle.Spec.TrackedK8sObjects); got < 3 {
		t.Fatalf("len(bundle.Spec.TrackedK8sObjects) = %d, want at least 3", got)
	}
	if got := len(bundle.Spec.TrackedHostPaths); got == 0 {
		t.Fatalf("len(bundle.Spec.TrackedHostPaths) = %d, want > 0", got)
	}

	var sawSecret, sawDaemonSet, sawDeployment, sawLocalPatch bool
	for _, object := range bundle.Spec.TrackedK8sObjects {
		switch {
		case object.Kind == "Secret" && object.Source == InventorySourceLocalResource && object.Ownership == InventoryOwnershipLocal:
			sawSecret = true
		case object.Kind == "DaemonSet" && object.Component == "cilium" && object.Source == InventorySourcePackageManifest:
			sawDaemonSet = true
		case object.Kind == "Deployment" && object.Component == "cilium" && object.Source == InventorySourcePackageManifest:
			sawDeployment = true
		case object.Kind == "ConfigMap" && object.Component == "cilium" && object.Source == InventorySourceLocalPatch && object.Ownership == InventoryOwnershipLocal:
			sawLocalPatch = true
		}
	}
	if !sawSecret || !sawDaemonSet || !sawDeployment || !sawLocalPatch {
		t.Fatalf("tracked objects missing expected entries: %+v", bundle.Spec.TrackedK8sObjects)
	}

	var sawRootfsReadme bool
	for _, path := range bundle.Spec.TrackedHostPaths {
		switch path.HostPath {
		case "/README":
			sawRootfsReadme = true
		}
	}
	if !sawRootfsReadme {
		t.Fatalf("tracked host paths missing expected entries: %+v", bundle.Spec.TrackedHostPaths)
	}

	renderedManifestPath := filepath.Join(out, "components", "cilium", "files", "manifests", "cilium.yaml")
	data, err := os.ReadFile(renderedManifestPath)
	if err != nil {
		t.Fatalf("ReadFile(%q) error = %v", renderedManifestPath, err)
	}
	if got := string(data); !strings.Contains(got, "enable-hubble: \"true\"") {
		t.Fatalf("rendered manifest missing patched value: %s", got)
	}
}

func TestRenderPlanCollectsLocalInputTrackedHostPath(t *testing.T) {
	t.Parallel()

	root := filepath.Join("..", "..", "..", "scripts", "poc", "minimal-single-node", "packages", "kubernetes")
	pkg, err := packageformat.LoadDir(root)
	if err != nil {
		t.Fatalf("LoadDir() error = %v", err)
	}

	localInputRoot := t.TempDir()
	localInputPath := filepath.Join(localInputRoot, "kubeadm.yaml")
	if err := os.WriteFile(localInputPath, []byte("apiVersion: kubeadm.k8s.io/v1beta4\nkind: ClusterConfiguration\nclusterName: local-input\n"), 0o644); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}
	hostInputPath := filepath.Join(localInputRoot, "hosts", "10.0.0.11", "kubeadm.yaml")
	if err := os.MkdirAll(filepath.Dir(hostInputPath), 0o755); err != nil {
		t.Fatalf("MkdirAll(host input) error = %v", err)
	}
	if err := os.WriteFile(hostInputPath, []byte("apiVersion: kubeadm.k8s.io/v1beta4\nkind: ClusterConfiguration\nclusterName: host-input\n"), 0o644); err != nil {
		t.Fatalf("WriteFile(host input) error = %v", err)
	}

	doc := bom.New("minimal-single-node", "rev-poc-001", bom.ChannelAlpha)
	doc.Spec.Components = []bom.Component{
		{
			Name:    "containerd",
			Kind:    "runtime",
			Version: "v1.7.18",
			Artifact: bom.ArtifactReference{
				Name:   "containerd-runtime",
				Image:  "registry.example.io/sealos/containerd-runtime:v1.7.18",
				Digest: "sha256:3333333333333333333333333333333333333333333333333333333333333333",
			},
		},
		{
			Name:    "kubernetes",
			Kind:    "infra",
			Version: "v1.30.3",
			Dependencies: []string{
				"containerd",
			},
			Artifact: bom.ArtifactReference{
				Name:   "kubernetes-rootfs",
				Image:  "registry.example.io/sealos/kubernetes-rootfs:v1.30.3",
				Digest: "sha256:1111111111111111111111111111111111111111111111111111111111111111",
			},
		},
	}

	plan, err := BuildPlanFromResolved(doc, map[string]*packageformat.ComponentPackage{
		"containerd": packageformat.New("containerd-runtime", "containerd", "v1.7.18", packageformat.ClassRootfs),
		"kubernetes": pkg,
	})
	if err != nil {
		t.Fatalf("BuildPlanFromResolved() error = %v", err)
	}
	plan.Components[1].InputBindings = map[string]string{
		"kubeadm-cluster-config": localInputPath,
	}
	plan.Components[1].HostInputBindings = map[string]map[string]string{
		"kubeadm-cluster-config": {
			"10.0.0.11": hostInputPath,
		},
	}

	out := t.TempDir()
	bundle, err := RenderPlan(plan, SourceMap{
		"containerd": root,
		"kubernetes": root,
	}, out)
	if err != nil {
		t.Fatalf("RenderPlan() error = %v", err)
	}

	var tracked *TrackedHostPath
	for i := range bundle.Spec.TrackedHostPaths {
		path := &bundle.Spec.TrackedHostPaths[i]
		if path.HostPath == "/etc/kubernetes/kubeadm.yaml" {
			tracked = path
			break
		}
	}
	if tracked == nil {
		t.Fatalf("tracked host paths missing /etc/kubernetes/kubeadm.yaml: %+v", bundle.Spec.TrackedHostPaths)
	}
	if got, want := tracked.Ownership, InventoryOwnershipLocal; got != want {
		t.Fatalf("tracked ownership = %q, want %q", got, want)
	}
	if got, want := tracked.Source, InventorySourceLocalInput; got != want {
		t.Fatalf("tracked source = %q, want %q", got, want)
	}
	if got, want := tracked.InputName, "kubeadm-cluster-config"; got != want {
		t.Fatalf("tracked inputName = %q, want %q", got, want)
	}
	if got, want := tracked.HostInputBindings["10.0.0.11"], "components/kubernetes/host-inputs/10.0.0.11/files/etc/kubernetes/kubeadm.yaml"; got != want {
		t.Fatalf("tracked hostInputBindings[10.0.0.11] = %q, want %q", got, want)
	}
}

func TestRenderPlanCollectsSingleFileRootfsTrackedHostPath(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "rootfs", "usr", "bin"), 0o755); err != nil {
		t.Fatalf("MkdirAll() error = %v", err)
	}
	if err := os.WriteFile(filepath.Join(root, "rootfs", "usr", "bin", "kubelet"), []byte("desired-kubelet"), 0o644); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}
	if err := os.WriteFile(filepath.Join(root, "package.yaml"), []byte("apiVersion: distribution.sealos.io/v1alpha1\nkind: ComponentPackage\nmetadata:\n  name: kubernetes-rootfs\nspec:\n  component: kubernetes\n  version: v1.30.3\n  class: rootfs\n"), 0o644); err != nil {
		t.Fatalf("WriteFile(package.yaml) error = %v", err)
	}

	pkg := packageformat.New("kubernetes-rootfs", "kubernetes", "v1.30.3", packageformat.ClassRootfs)
	pkg.Spec.Contents = []packageformat.Content{
		{
			Name: "kubelet",
			Type: packageformat.ContentRootfs,
			Path: "rootfs/usr/bin/kubelet",
		},
	}

	doc := bom.New("minimal-single-node", "rev-poc-001", bom.ChannelAlpha)
	doc.Spec.Components = []bom.Component{
		{
			Name:    "kubernetes",
			Kind:    "infra",
			Version: "v1.30.3",
			Artifact: bom.ArtifactReference{
				Name:   "kubernetes-rootfs",
				Image:  "registry.example.io/sealos/kubernetes-rootfs:v1.30.3",
				Digest: "sha256:1111111111111111111111111111111111111111111111111111111111111111",
			},
		},
	}

	plan, err := BuildPlanFromResolved(doc, map[string]*packageformat.ComponentPackage{
		"kubernetes": pkg,
	})
	if err != nil {
		t.Fatalf("BuildPlanFromResolved() error = %v", err)
	}

	out := t.TempDir()
	bundle, err := RenderPlan(plan, SourceMap{"kubernetes": root}, out)
	if err != nil {
		t.Fatalf("RenderPlan() error = %v", err)
	}

	if len(bundle.Spec.TrackedHostPaths) != 1 {
		t.Fatalf("len(bundle.Spec.TrackedHostPaths) = %d, want 1", len(bundle.Spec.TrackedHostPaths))
	}
	if got, want := bundle.Spec.TrackedHostPaths[0].HostPath, "/usr/bin/kubelet"; got != want {
		t.Fatalf("tracked host path = %q, want %q", got, want)
	}
}

func TestRenderPlanCollectsGeneratedHostPaths(t *testing.T) {
	t.Parallel()

	root := filepath.Join("..", "..", "..", "scripts", "poc", "minimal-single-node", "packages", "kubernetes")
	pkg, err := packageformat.LoadDir(root)
	if err != nil {
		t.Fatalf("LoadDir() error = %v", err)
	}

	doc := bom.New("minimal-single-node", "rev-poc-001", bom.ChannelAlpha)
	doc.Spec.Components = []bom.Component{
		{
			Name:    "containerd",
			Kind:    "runtime",
			Version: "v1.7.18",
			Artifact: bom.ArtifactReference{
				Name:   "containerd-runtime",
				Image:  "registry.example.io/sealos/containerd-runtime:v1.7.18",
				Digest: "sha256:3333333333333333333333333333333333333333333333333333333333333333",
			},
		},
		{
			Name:    "kubernetes",
			Kind:    "infra",
			Version: "v1.30.3",
			Dependencies: []string{
				"containerd",
			},
			Artifact: bom.ArtifactReference{
				Name:   "kubernetes-rootfs",
				Image:  "registry.example.io/sealos/kubernetes-rootfs:v1.30.3",
				Digest: "sha256:1111111111111111111111111111111111111111111111111111111111111111",
			},
		},
	}

	plan, err := BuildPlanFromResolved(doc, map[string]*packageformat.ComponentPackage{
		"containerd": packageformat.New("containerd-runtime", "containerd", "v1.7.18", packageformat.ClassRootfs),
		"kubernetes": pkg,
	})
	if err != nil {
		t.Fatalf("BuildPlanFromResolved() error = %v", err)
	}
	localInputRoot := t.TempDir()
	localInputPath := filepath.Join(localInputRoot, "kubeadm.yaml")
	if err := os.WriteFile(localInputPath, []byte(`apiVersion: kubeadm.k8s.io/v1beta3
kind: ClusterConfiguration
clusterName: generated-tracking
kubernetesVersion: v1.30.3
networking:
  podSubnet: 10.244.0.0/16
  serviceSubnet: 10.96.0.0/12
apiServer:
  extraArgs:
    - name: audit-policy-file
      value: /etc/kubernetes/audit-policy.yaml
  extraVolumes:
    - name: localtime
      hostPath: /etc/localtime
      mountPath: /etc/localtime
controllerManager:
  extraArgs:
    - name: bind-address
      value: 0.0.0.0
  extraVolumes:
    - name: localtime
      hostPath: /etc/localtime
      mountPath: /etc/localtime
scheduler:
  extraArgs:
    - name: bind-address
      value: 0.0.0.0
  extraVolumes:
    - name: localtime
      hostPath: /etc/localtime
      mountPath: /etc/localtime
`), 0o644); err != nil {
		t.Fatalf("WriteFile(%q) error = %v", localInputPath, err)
	}
	plan.Components[1].InputBindings = map[string]string{
		"kubeadm-cluster-config": localInputPath,
	}

	out := t.TempDir()
	bundle, err := RenderPlan(plan, SourceMap{
		"containerd": root,
		"kubernetes": root,
	}, out)
	if err != nil {
		t.Fatalf("RenderPlan() error = %v", err)
	}

	tracked := make(map[string]*TrackedHostPath)
	for i := range bundle.Spec.TrackedHostPaths {
		path := &bundle.Spec.TrackedHostPaths[i]
		switch path.HostPath {
		case "/etc/kubernetes/manifests/kube-apiserver.yaml",
			"/etc/kubernetes/manifests/kube-controller-manager.yaml",
			"/etc/kubernetes/manifests/kube-scheduler.yaml":
			tracked[path.HostPath] = path
		}
	}
	if got, want := len(tracked), 3; got != want {
		t.Fatalf("generated tracked host paths = %d, want %d: %+v", got, want, bundle.Spec.TrackedHostPaths)
	}
	for hostPath, entry := range tracked {
		if got, want := entry.Source, InventorySourceGeneratedHook; got != want {
			t.Fatalf("%s source = %q, want %q", hostPath, got, want)
		}
		if got, want := entry.ProjectionClass, HostPathProjectionClassGenerated; got != want {
			t.Fatalf("%s projectionClass = %q, want %q", hostPath, got, want)
		}
		if got, want := entry.CompareStrategy, HostPathCompareStrategySemanticGenerated; got != want {
			t.Fatalf("%s compareStrategy = %q, want %q", hostPath, got, want)
		}
		if entry.Generated == nil {
			t.Fatalf("%s generated metadata = nil, want metadata", hostPath)
		}
		if got, want := entry.Generated.Tool, "kubeadm"; got != want {
			t.Fatalf("%s generated.tool = %q, want %q", hostPath, got, want)
		}
		if got, want := entry.Generated.Hook, "bootstrap"; got != want {
			t.Fatalf("%s generated.hook = %q, want %q", hostPath, got, want)
		}
	}

	apiserver := tracked["/etc/kubernetes/manifests/kube-apiserver.yaml"]
	if got, want := apiserver.Generated.Name, "kube-apiserver"; got != want {
		t.Fatalf("apiserver generated.name = %q, want %q", got, want)
	}
	if got, want := apiserver.Generated.ExpectedImage, "registry.k8s.io/kube-apiserver:v1.30.3"; got != want {
		t.Fatalf("apiserver generated.expectedImage = %q, want %q", got, want)
	}
	if got, want := apiserver.Generated.ExpectedCommand, "kube-apiserver"; got != want {
		t.Fatalf("apiserver generated.expectedCommand = %q, want %q", got, want)
	}
	if got, want := apiserver.Generated.ExpectedArgs["service-cluster-ip-range"], "10.96.0.0/12"; got != want {
		t.Fatalf("apiserver generated.expectedArgs[service-cluster-ip-range] = %q, want %q", got, want)
	}
	if got, want := apiserver.Generated.ExpectedArgs["audit-policy-file"], "/etc/kubernetes/audit-policy.yaml"; got != want {
		t.Fatalf("apiserver generated.expectedArgs[audit-policy-file] = %q, want %q", got, want)
	}
	for _, mountPath := range []string{"/etc/kubernetes", "/etc/kubernetes/pki", "/etc/localtime"} {
		if !containsString(apiserver.Generated.ExpectedVolumeMounts, mountPath) {
			t.Fatalf("apiserver generated.expectedVolumeMounts missing %q: %+v", mountPath, apiserver.Generated.ExpectedVolumeMounts)
		}
	}

	controllerManager := tracked["/etc/kubernetes/manifests/kube-controller-manager.yaml"]
	if got, want := controllerManager.Generated.Name, "kube-controller-manager"; got != want {
		t.Fatalf("controller-manager generated.name = %q, want %q", got, want)
	}
	if got, want := controllerManager.Generated.ExpectedImage, "registry.k8s.io/kube-controller-manager:v1.30.3"; got != want {
		t.Fatalf("controller-manager generated.expectedImage = %q, want %q", got, want)
	}
	if got, want := controllerManager.Generated.ExpectedCommand, "kube-controller-manager"; got != want {
		t.Fatalf("controller-manager generated.expectedCommand = %q, want %q", got, want)
	}
	if got, want := controllerManager.Generated.ExpectedArgs["cluster-cidr"], "10.244.0.0/16"; got != want {
		t.Fatalf("controller-manager generated.expectedArgs[cluster-cidr] = %q, want %q", got, want)
	}
	if got, want := controllerManager.Generated.ExpectedArgs["bind-address"], "0.0.0.0"; got != want {
		t.Fatalf("controller-manager generated.expectedArgs[bind-address] = %q, want %q", got, want)
	}
	for _, mountPath := range []string{"/etc/kubernetes/pki", "/etc/localtime"} {
		if !containsString(controllerManager.Generated.ExpectedVolumeMounts, mountPath) {
			t.Fatalf("controller-manager generated.expectedVolumeMounts missing %q: %+v", mountPath, controllerManager.Generated.ExpectedVolumeMounts)
		}
	}

	scheduler := tracked["/etc/kubernetes/manifests/kube-scheduler.yaml"]
	if got, want := scheduler.Generated.Name, "kube-scheduler"; got != want {
		t.Fatalf("scheduler generated.name = %q, want %q", got, want)
	}
	if got, want := scheduler.Generated.ExpectedImage, "registry.k8s.io/kube-scheduler:v1.30.3"; got != want {
		t.Fatalf("scheduler generated.expectedImage = %q, want %q", got, want)
	}
	if got, want := scheduler.Generated.ExpectedCommand, "kube-scheduler"; got != want {
		t.Fatalf("scheduler generated.expectedCommand = %q, want %q", got, want)
	}
	if got, want := scheduler.Generated.ExpectedArgs["bind-address"], "0.0.0.0"; got != want {
		t.Fatalf("scheduler generated.expectedArgs[bind-address] = %q, want %q", got, want)
	}
	if !containsString(scheduler.Generated.ExpectedVolumeMounts, "/etc/localtime") {
		t.Fatalf("scheduler generated.expectedVolumeMounts missing %q: %+v", "/etc/localtime", scheduler.Generated.ExpectedVolumeMounts)
	}
}

func containsString(values []string, target string) bool {
	for _, value := range values {
		if value == target {
			return true
		}
	}
	return false
}
