package reconcile

import (
	"bytes"
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

	bundle := &hydrate.Bundle{
		APIVersion: distribution.APIVersion,
		Kind:       distribution.KindHydratedBundle,
		Spec: hydrate.BundleSpec{
			BOMName:  "minimal-single-node",
			Revision: "rev-1",
			Channel:  bom.ChannelAlpha,
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

func TestApplyRejectsMultiNodeCluster(t *testing.T) {
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
		"local-rev-1",
	); err != nil {
		t.Fatalf("PersistRenderedState() error = %v", err)
	}

	_, err = Apply(ApplyOptions{
		ClusterName:    "cluster-a",
		BundlePath:     bundleDir,
		KubeconfigPath: kubeconfigPath,
		HostRoot:       t.TempDir(),
	})
	if err == nil || !strings.Contains(err.Error(), "single-node clusters") {
		t.Fatalf("Apply() error = %v, want single-node rejection", err)
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
		"local-rev-1",
	); err != nil {
		t.Fatalf("PersistRenderedState() error = %v", err)
	}
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
