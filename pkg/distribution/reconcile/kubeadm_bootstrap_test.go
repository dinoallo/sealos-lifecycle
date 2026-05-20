package reconcile

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/labring/sealos/pkg/distribution/bom"
	"github.com/labring/sealos/pkg/distribution/hydrate"
	"github.com/labring/sealos/pkg/distribution/packageformat"
	v1beta1 "github.com/labring/sealos/pkg/types/v1beta1"
	yamlutil "github.com/labring/sealos/pkg/utils/yaml"
	kubeletconfigv1beta1 "k8s.io/kubelet/config/v1beta1"
	kubeadmapi "k8s.io/kubernetes/cmd/kubeadm/app/apis/kubeadm"
)

func TestRenderKubeadmClusterConfigSetsControlPlaneEndpoint(t *testing.T) {
	bundleDir := t.TempDir()
	configPath := filepath.Join(bundleDir, "components", "kubernetes", "files", "files", "etc", "kubernetes", "kubeadm.yaml")
	if err := os.MkdirAll(filepath.Dir(configPath), 0o755); err != nil {
		t.Fatalf("MkdirAll() error = %v", err)
	}
	config := []byte(`apiVersion: kubeadm.k8s.io/v1beta3
kind: ClusterConfiguration
clusterName: poc-minimal
kubernetesVersion: v1.30.3
networking:
  podSubnet: 10.244.0.0/16
  serviceSubnet: 10.96.0.0/12
`)
	if err := os.WriteFile(configPath, config, 0o644); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	executor := &bundleExecutor{bundlePath: bundleDir}
	step := hydrate.RenderedStep{
		BundlePath: "components/kubernetes/files/files/etc/kubernetes/kubeadm.yaml",
		SourcePath: "files/etc/kubernetes/kubeadm.yaml",
	}
	rendered, err := executor.renderKubeadmClusterConfig(step, "192.168.13.18:6443")
	if err != nil {
		t.Fatalf("renderKubeadmClusterConfig() error = %v", err)
	}

	renderedPath := filepath.Join(bundleDir, "rendered-cluster-config.yaml")
	if err := os.WriteFile(renderedPath, rendered, 0o644); err != nil {
		t.Fatalf("WriteFile(renderedPath) error = %v", err)
	}

	clusterConfig := kubeadmapi.ClusterConfiguration{}
	if err := yamlutil.UnmarshalFile(renderedPath, &clusterConfig); err != nil {
		t.Fatalf("UnmarshalFile(renderedPath) error = %v", err)
	}
	if clusterConfig.ControlPlaneEndpoint != "192.168.13.18:6443" {
		t.Fatalf("ControlPlaneEndpoint = %q, want %q", clusterConfig.ControlPlaneEndpoint, "192.168.13.18:6443")
	}
	if strings.Contains(string(rendered), "TTLAfterFinished") || strings.Contains(string(rendered), "EphemeralContainers") {
		t.Fatalf("rendered cluster config should not contain removed feature gates, got:\n%s", string(rendered))
	}
}

func TestRenderKubeadmJoinConfigDoesNotInjectLegacyFeatureGates(t *testing.T) {
	bundleDir := t.TempDir()
	configPath := filepath.Join(bundleDir, "components", "kubernetes", "files", "files", "etc", "kubernetes", "kubeadm.yaml")
	if err := os.MkdirAll(filepath.Dir(configPath), 0o755); err != nil {
		t.Fatalf("MkdirAll() error = %v", err)
	}
	config := []byte(`apiVersion: kubeadm.k8s.io/v1beta3
kind: ClusterConfiguration
clusterName: poc-minimal
kubernetesVersion: v1.30.3
networking:
  podSubnet: 10.244.0.0/16
  serviceSubnet: 10.96.0.0/12
`)
	if err := os.WriteFile(configPath, config, 0o644); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	executor := &bundleExecutor{bundlePath: bundleDir}
	step := hydrate.RenderedStep{
		BundlePath: "components/kubernetes/files/files/etc/kubernetes/kubeadm.yaml",
		SourcePath: "files/etc/kubernetes/kubeadm.yaml",
	}
	joinMetadata := &kubeadmJoinMetadata{
		apiServerEndpoint: "192.168.13.18:6443",
		token:             "abcdef.0123456789abcdef",
		caCertHashes:      []string{"sha256:0123456789abcdef"},
	}
	rendered, err := executor.renderKubeadmJoinConfig(step, "192.168.0.240", joinMetadata)
	if err != nil {
		t.Fatalf("renderKubeadmJoinConfig() error = %v", err)
	}

	joinPath := filepath.Join(bundleDir, "rendered-join-config.yaml")
	if err := os.WriteFile(joinPath, rendered, 0o644); err != nil {
		t.Fatalf("WriteFile(joinPath) error = %v", err)
	}

	if strings.Contains(string(rendered), "TTLAfterFinished") || strings.Contains(string(rendered), "EphemeralContainers") {
		t.Fatalf("rendered join config should not contain removed feature gates, got:\n%s", string(rendered))
	}

	kubeletConfig := kubeletconfigv1beta1.KubeletConfiguration{}
	if err := yamlutil.Unmarshal(strings.NewReader(string(rendered)), &kubeletConfig); err != nil {
		t.Fatalf("Unmarshal(rendered kubelet config) error = %v", err)
	}
	if len(kubeletConfig.FeatureGates) != 0 {
		t.Fatalf("KubeletConfiguration.FeatureGates = %#v, want empty", kubeletConfig.FeatureGates)
	}
}

func TestRunKubeadmBootstrapHookRepairsJoinedSecondaryControlPlaneWithoutFreshHosts(t *testing.T) {
	tmpDir := t.TempDir()
	writeExecutable(t, filepath.Join(tmpDir, "bin", "kubectl"), `#!/bin/sh
if [ "$1" = "--kubeconfig" ]; then
  shift 2
fi
if [ "$1" = "-n" ]; then
  shift 2
fi
if [ "$1" = "get" ] && [ "$2" = "cm" ] && [ "$3" = "kubeadm-config" ]; then
  printf '%s' 'apiVersion: kubeadm.k8s.io/v1beta3
kind: ClusterConfiguration
clusterName: demo
controlPlaneEndpoint: 10.0.0.10:6443
kubernetesVersion: v1.30.3
'
  exit 0
fi
exit 0
`)
	t.Setenv("PATH", filepath.Join(tmpDir, "bin")+":"+os.Getenv("PATH"))

	bundleDir := filepath.Join(t.TempDir(), "bundle")
	writeExecutable(t, filepath.Join(bundleDir, "components", "kubernetes", "files", "hooks", "bootstrap.sh"), "#!/bin/sh\nexit 0\n")
	writeFile(t, filepath.Join(bundleDir, "components", "kubernetes", "files", "files", "etc", "kubernetes", "kubeadm.yaml"), `apiVersion: kubeadm.k8s.io/v1beta3
kind: ClusterConfiguration
clusterName: demo
kubernetesVersion: v1.30.3
`, 0o644)

	fakeRemote := &fakeApplyRemoteExecutor{
		outputs: []fakeRemoteCommandOutput{
			{host: "10.0.0.10:22", contains: "/etc/kubernetes/kubelet.conf", output: "joined\n"},
			{host: "10.0.0.11:22", contains: "/etc/kubernetes/kubelet.conf", output: "joined\n"},
			{host: "10.0.0.12:22", contains: "/etc/kubernetes/kubelet.conf", output: "joined\n"},
			{
				host:     "10.0.0.10:22",
				contains: "kubeadm token create --print-join-command",
				output:   "kubeadm join 10.0.0.10:6443 --token abcdef.0123456789abcdef --discovery-token-ca-cert-hash sha256:cafebabe --certificate-key 11223344556677889900aabbccddeeff\n",
			},
		},
	}

	executor := &bundleExecutor{
		clusterName:    "cluster-a",
		bundlePath:     bundleDir,
		kubeconfigPath: filepath.Join(tmpDir, "admin.conf"),
		topology: &clusterExecutionTopology{
			clusterName: "cluster-a",
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
		},
		remoteExec: fakeRemote,
	}
	executor.applyDefaults()

	component := hydrate.RenderedComponent{
		Name:        "kubernetes",
		PackageName: "kubernetes-rootfs",
	}
	hook := hydrate.RenderedStep{
		Name:       "bootstrap",
		Kind:       hydrate.StepHook,
		BundlePath: "components/kubernetes/files/hooks/bootstrap.sh",
		SourcePath: "hooks/bootstrap.sh",
		HookPhase:  packageformat.PhaseBootstrap,
		Target:     packageformat.TargetAllNodes,
	}
	kubeadmConfigStep := hydrate.RenderedStep{
		Name:        "kubeadm-defaults",
		Kind:        hydrate.StepContent,
		BundlePath:  "components/kubernetes/files/files/etc/kubernetes/kubeadm.yaml",
		SourcePath:  "files/etc/kubernetes/kubeadm.yaml",
		ContentType: packageformat.ContentFile,
	}

	if err := executor.runKubeadmBootstrapHook(component, hook, kubeadmConfigStep, []string{}); err != nil {
		t.Fatalf("runKubeadmBootstrapHook() error = %v", err)
	}

	firstMasterCommands := strings.Join(fakeRemote.commandsForHost("10.0.0.10:22"), "\n")
	if strings.Contains(firstMasterCommands, "bootstrap.sh") {
		t.Fatalf("first master bootstrap hook should not run when there are no fresh hosts\ncommands:\n%s", firstMasterCommands)
	}

	secondMasterCommands := strings.Join(fakeRemote.commandsForHost("10.0.0.11:22"), "\n")
	for _, marker := range []string{
		"kubeadm' 'join' 'phase' 'control-plane-prepare' 'kubeconfig' '--config' '/etc/kubernetes/kubeadm.yaml'",
		"kubeadm' 'join' 'phase' 'control-plane-prepare' 'control-plane' '--config' '/etc/kubernetes/kubeadm.yaml'",
		"systemctl' 'restart' 'kubelet'",
	} {
		if !strings.Contains(secondMasterCommands, marker) {
			t.Fatalf("second master command stream missing %q\ncommands:\n%s", marker, secondMasterCommands)
		}
	}
	if got := strings.Join(fakeRemote.commandsForHost("10.0.0.12:22"), "\n"); strings.Contains(got, "control-plane-prepare") {
		t.Fatalf("worker should not receive control-plane repair commands\ncommands:\n%s", got)
	}

	var repairedConfig string
	for _, op := range fakeRemote.copyOps {
		if op.host == "10.0.0.11:22" && op.dst == "/etc/kubernetes/kubeadm.yaml" {
			repairedConfig = string(op.data)
			break
		}
	}
	if !strings.Contains(repairedConfig, "certificateKey: 11223344556677889900aabbccddeeff") {
		t.Fatalf("repaired config missing control-plane certificate key:\n%s", repairedConfig)
	}
}

func TestRunKubeadmBootstrapHookRepairsJoinedFirstControlPlaneWithoutFreshHosts(t *testing.T) {
	tmpDir := t.TempDir()
	writeExecutable(t, filepath.Join(tmpDir, "bin", "kubectl"), `#!/bin/sh
if [ "$1" = "--kubeconfig" ]; then
  shift 2
fi
if [ "$1" = "-n" ]; then
  shift 2
fi
if [ "$1" = "get" ] && [ "$2" = "cm" ] && [ "$3" = "kubeadm-config" ]; then
  printf '%s' 'apiVersion: kubeadm.k8s.io/v1beta3
kind: ClusterConfiguration
clusterName: demo
controlPlaneEndpoint: 10.0.0.10:6443
kubernetesVersion: v1.30.3
'
  exit 0
fi
exit 0
`)
	t.Setenv("PATH", filepath.Join(tmpDir, "bin")+":"+os.Getenv("PATH"))

	bundleDir := filepath.Join(t.TempDir(), "bundle")
	writeExecutable(t, filepath.Join(bundleDir, "components", "kubernetes", "files", "hooks", "bootstrap.sh"), "#!/bin/sh\nexit 0\n")
	writeFile(t, filepath.Join(bundleDir, "components", "kubernetes", "files", "files", "etc", "kubernetes", "kubeadm.yaml"), `apiVersion: kubeadm.k8s.io/v1beta3
kind: ClusterConfiguration
clusterName: demo
kubernetesVersion: v1.30.3
`, 0o644)

	fakeRemote := &fakeApplyRemoteExecutor{
		outputs: []fakeRemoteCommandOutput{
			{host: "203.0.113.10:22", contains: "/etc/kubernetes/kubelet.conf", output: "joined\n"},
			{host: "203.0.113.12:22", contains: "/etc/kubernetes/kubelet.conf", output: "joined\n"},
		},
	}

	executor := &bundleExecutor{
		clusterName:    "cluster-a",
		bundlePath:     bundleDir,
		kubeconfigPath: filepath.Join(tmpDir, "admin.conf"),
		topology: &clusterExecutionTopology{
			clusterName: "cluster-a",
			allNodes:    []string{"203.0.113.10:22", "203.0.113.12:22"},
			firstMaster: "203.0.113.10:22",
			cluster: &v1beta1.Cluster{
				Spec: v1beta1.ClusterSpec{
					Hosts: []v1beta1.Host{
						{IPS: []string{"203.0.113.10:22"}, Roles: []string{v1beta1.MASTER}},
						{IPS: []string{"203.0.113.12:22"}, Roles: []string{v1beta1.NODE}},
					},
				},
			},
		},
		remoteExec: fakeRemote,
	}
	executor.applyDefaults()

	component := hydrate.RenderedComponent{
		Name:        "kubernetes",
		PackageName: "kubernetes-rootfs",
	}
	hook := hydrate.RenderedStep{
		Name:       "bootstrap",
		Kind:       hydrate.StepHook,
		BundlePath: "components/kubernetes/files/hooks/bootstrap.sh",
		SourcePath: "hooks/bootstrap.sh",
		HookPhase:  packageformat.PhaseBootstrap,
		Target:     packageformat.TargetAllNodes,
	}
	kubeadmConfigStep := hydrate.RenderedStep{
		Name:        "kubeadm-defaults",
		Kind:        hydrate.StepContent,
		BundlePath:  "components/kubernetes/files/files/etc/kubernetes/kubeadm.yaml",
		SourcePath:  "files/etc/kubernetes/kubeadm.yaml",
		ContentType: packageformat.ContentFile,
	}

	if err := executor.runKubeadmBootstrapHook(component, hook, kubeadmConfigStep, []string{}); err != nil {
		t.Fatalf("runKubeadmBootstrapHook() error = %v", err)
	}

	firstMasterCommands := strings.Join(fakeRemote.commandsForHost("203.0.113.10:22"), "\n")
	if strings.Contains(firstMasterCommands, "bootstrap.sh") {
		t.Fatalf("first master bootstrap hook should not run during repair-only reconcile\ncommands:\n%s", firstMasterCommands)
	}
	for _, marker := range []string{
		"kubeadm' 'init' 'phase' 'upload-config' 'kubeadm'",
		"kubeadm' 'init' 'phase' 'kubeconfig' 'all' '--config' '/etc/kubernetes/kubeadm.yaml'",
		"kubeadm' 'init' 'phase' 'control-plane' 'all' '--config' '/etc/kubernetes/kubeadm.yaml'",
		"systemctl' 'restart' 'kubelet'",
	} {
		if !strings.Contains(firstMasterCommands, marker) {
			t.Fatalf("first master command stream missing %q\ncommands:\n%s", marker, firstMasterCommands)
		}
	}
	if strings.Contains(firstMasterCommands, "kubeadm token create --print-join-command") {
		t.Fatalf("repair-only reconcile should not generate join metadata\ncommands:\n%s", firstMasterCommands)
	}

	if got := strings.Join(fakeRemote.commandsForHost("203.0.113.12:22"), "\n"); strings.Contains(got, "control-plane") || strings.Contains(got, "bootstrap.sh") {
		t.Fatalf("worker should not receive control-plane repair commands\ncommands:\n%s", got)
	}

	foundClusterConfigUpload := false
	for _, op := range fakeRemote.copyOps {
		if op.host == "203.0.113.10:22" && strings.HasPrefix(op.dst, "/tmp/sealos-kubeadm-cluster-config-") {
			foundClusterConfigUpload = true
			break
		}
	}
	if !foundClusterConfigUpload {
		t.Fatal("expected a kubeadm cluster config upload for joined first master repair")
	}
}

func TestRepairGeneratedControlPlaneHostRepairsJoinedSecondaryControlPlane(t *testing.T) {
	tmpDir := t.TempDir()
	writeExecutable(t, filepath.Join(tmpDir, "bin", "kubectl"), `#!/bin/sh
if [ "$1" = "--kubeconfig" ]; then
  shift 2
fi
if [ "$1" = "-n" ]; then
  shift 2
fi
if [ "$1" = "get" ] && [ "$2" = "cm" ] && [ "$3" = "kubeadm-config" ]; then
  printf '%s' 'apiVersion: kubeadm.k8s.io/v1beta3
kind: ClusterConfiguration
clusterName: demo
controlPlaneEndpoint: 10.0.0.10:6443
kubernetesVersion: v1.30.3
'
  exit 0
fi
exit 0
`)
	t.Setenv("PATH", filepath.Join(tmpDir, "bin")+":"+os.Getenv("PATH"))

	bundleDir := filepath.Join(t.TempDir(), "bundle")
	writeFile(t, filepath.Join(bundleDir, "components", "kubernetes", "files", "files", "etc", "kubernetes", "kubeadm.yaml"), `apiVersion: kubeadm.k8s.io/v1beta3
kind: ClusterConfiguration
clusterName: demo
kubernetesVersion: v1.30.3
`, 0o644)
	if err := yamlutil.MarshalFile(filepath.Join(bundleDir, hydrate.BundleFileName), &hydrate.Bundle{
		APIVersion: "distribution.sealos.io/v1alpha1",
		Kind:       "HydratedBundle",
		Spec: hydrate.BundleSpec{
			BOMName:  "minimal-single-node",
			Revision: "rev-poc-001",
			Channel:  bom.ChannelAlpha,
			Components: []hydrate.RenderedComponent{{
				Name:        "kubernetes",
				PackageName: "kubernetes-rootfs",
				Steps: []hydrate.RenderedStep{{
					Name:        "kubeadm-defaults",
					Kind:        hydrate.StepContent,
					BundlePath:  "components/kubernetes/files/files/etc/kubernetes/kubeadm.yaml",
					SourcePath:  "files/etc/kubernetes/kubeadm.yaml",
					ContentType: packageformat.ContentFile,
				}},
			}},
		},
	}); err != nil {
		t.Fatalf("MarshalFile(bundle) error = %v", err)
	}

	fakeRemote := &fakeApplyRemoteExecutor{
		outputs: []fakeRemoteCommandOutput{
			{host: "10.0.0.11:22", contains: "/etc/kubernetes/kubelet.conf", output: "joined\n"},
			{
				host:     "10.0.0.10:22",
				contains: "kubeadm token create --print-join-command",
				output:   "kubeadm join 10.0.0.10:6443 --token abcdef.0123456789abcdef --discovery-token-ca-cert-hash sha256:cafebabe --certificate-key 11223344556677889900aabbccddeeff\n",
			},
		},
	}

	previousTopologyLoader := loadApplyExecutionTopology
	previousRemoteFactory := newApplyRemoteExecutor
	loadApplyExecutionTopology = func(clusterName string) (*clusterExecutionTopology, error) {
		return &clusterExecutionTopology{
			clusterName: clusterName,
			allNodes:    []string{"10.0.0.10:22", "10.0.0.11:22"},
			firstMaster: "10.0.0.10:22",
			cluster: &v1beta1.Cluster{
				Spec: v1beta1.ClusterSpec{
					Hosts: []v1beta1.Host{
						{IPS: []string{"10.0.0.10:22"}, Roles: []string{v1beta1.MASTER}},
						{IPS: []string{"10.0.0.11:22"}, Roles: []string{v1beta1.MASTER}},
					},
				},
			},
		}, nil
	}
	newApplyRemoteExecutor = func(*clusterExecutionTopology) (applyRemoteExecutor, error) {
		return fakeRemote, nil
	}
	t.Cleanup(func() {
		loadApplyExecutionTopology = previousTopologyLoader
		newApplyRemoteExecutor = previousRemoteFactory
	})

	if err := RepairGeneratedControlPlaneHost(RepairGeneratedControlPlaneHostOptions{
		ClusterName:    "cluster-a",
		BundlePath:     bundleDir,
		KubeconfigPath: filepath.Join(tmpDir, "admin.conf"),
		HostRoot:       string(os.PathSeparator),
		Host:           "10.0.0.11:22",
	}); err != nil {
		t.Fatalf("RepairGeneratedControlPlaneHost() error = %v", err)
	}

	secondMasterCommands := strings.Join(fakeRemote.commandsForHost("10.0.0.11:22"), "\n")
	for _, marker := range []string{
		"kubeadm' 'join' 'phase' 'control-plane-prepare' 'kubeconfig' '--config' '/etc/kubernetes/kubeadm.yaml'",
		"kubeadm' 'join' 'phase' 'control-plane-prepare' 'control-plane' '--config' '/etc/kubernetes/kubeadm.yaml'",
		"systemctl' 'restart' 'kubelet'",
	} {
		if !strings.Contains(secondMasterCommands, marker) {
			t.Fatalf("second master command stream missing %q\ncommands:\n%s", marker, secondMasterCommands)
		}
	}
}

func TestRepairGeneratedControlPlaneHostRepairsJoinedFirstControlPlane(t *testing.T) {
	tmpDir := t.TempDir()
	writeExecutable(t, filepath.Join(tmpDir, "bin", "kubectl"), `#!/bin/sh
if [ "$1" = "--kubeconfig" ]; then
  shift 2
fi
if [ "$1" = "-n" ]; then
  shift 2
fi
if [ "$1" = "get" ] && [ "$2" = "cm" ] && [ "$3" = "kubeadm-config" ]; then
  printf '%s' 'apiVersion: kubeadm.k8s.io/v1beta3
kind: ClusterConfiguration
clusterName: demo
controlPlaneEndpoint: 10.0.0.10:6443
kubernetesVersion: v1.30.3
'
  exit 0
fi
exit 0
`)
	t.Setenv("PATH", filepath.Join(tmpDir, "bin")+":"+os.Getenv("PATH"))

	bundleDir := filepath.Join(t.TempDir(), "bundle")
	writeFile(t, filepath.Join(bundleDir, "components", "kubernetes", "files", "files", "etc", "kubernetes", "kubeadm.yaml"), `apiVersion: kubeadm.k8s.io/v1beta3
kind: ClusterConfiguration
clusterName: demo
kubernetesVersion: v1.30.3
`, 0o644)
	if err := yamlutil.MarshalFile(filepath.Join(bundleDir, hydrate.BundleFileName), &hydrate.Bundle{
		APIVersion: "distribution.sealos.io/v1alpha1",
		Kind:       "HydratedBundle",
		Spec: hydrate.BundleSpec{
			BOMName:  "minimal-single-node",
			Revision: "rev-poc-001",
			Channel:  bom.ChannelAlpha,
			Components: []hydrate.RenderedComponent{{
				Name:        "kubernetes",
				PackageName: "kubernetes-rootfs",
				Steps: []hydrate.RenderedStep{{
					Name:        "kubeadm-defaults",
					Kind:        hydrate.StepContent,
					BundlePath:  "components/kubernetes/files/files/etc/kubernetes/kubeadm.yaml",
					SourcePath:  "files/etc/kubernetes/kubeadm.yaml",
					ContentType: packageformat.ContentFile,
				}},
			}},
		},
	}); err != nil {
		t.Fatalf("MarshalFile(bundle) error = %v", err)
	}

	fakeRemote := &fakeApplyRemoteExecutor{
		outputs: []fakeRemoteCommandOutput{
			{host: "203.0.113.10:22", contains: "/etc/kubernetes/kubelet.conf", output: "joined\n"},
		},
	}

	previousTopologyLoader := loadApplyExecutionTopology
	previousRemoteFactory := newApplyRemoteExecutor
	loadApplyExecutionTopology = func(clusterName string) (*clusterExecutionTopology, error) {
		return &clusterExecutionTopology{
			clusterName: clusterName,
			allNodes:    []string{"203.0.113.10:22"},
			firstMaster: "203.0.113.10:22",
			cluster: &v1beta1.Cluster{
				Spec: v1beta1.ClusterSpec{
					Hosts: []v1beta1.Host{
						{IPS: []string{"203.0.113.10:22"}, Roles: []string{v1beta1.MASTER}},
					},
				},
			},
		}, nil
	}
	newApplyRemoteExecutor = func(*clusterExecutionTopology) (applyRemoteExecutor, error) {
		return fakeRemote, nil
	}
	t.Cleanup(func() {
		loadApplyExecutionTopology = previousTopologyLoader
		newApplyRemoteExecutor = previousRemoteFactory
	})

	if err := RepairGeneratedControlPlaneHost(RepairGeneratedControlPlaneHostOptions{
		ClusterName:    "cluster-a",
		BundlePath:     bundleDir,
		KubeconfigPath: filepath.Join(tmpDir, "admin.conf"),
		HostRoot:       string(os.PathSeparator),
		Host:           "203.0.113.10:22",
	}); err != nil {
		t.Fatalf("RepairGeneratedControlPlaneHost() error = %v", err)
	}

	firstMasterCommands := strings.Join(fakeRemote.commandsForHost("203.0.113.10:22"), "\n")
	for _, marker := range []string{
		"kubeadm' 'init' 'phase' 'upload-config' 'kubeadm'",
		"kubeadm' 'init' 'phase' 'kubeconfig' 'all' '--config' '/etc/kubernetes/kubeadm.yaml'",
		"kubeadm' 'init' 'phase' 'control-plane' 'all' '--config' '/etc/kubernetes/kubeadm.yaml'",
		"systemctl' 'restart' 'kubelet'",
	} {
		if !strings.Contains(firstMasterCommands, marker) {
			t.Fatalf("first master command stream missing %q\ncommands:\n%s", marker, firstMasterCommands)
		}
	}
}
