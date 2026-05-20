package cmd

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"k8s.io/apimachinery/pkg/runtime"
	k8syaml "k8s.io/apimachinery/pkg/util/yaml"
	"sigs.k8s.io/yaml"

	"github.com/labring/sealos/pkg/buildah"
	"github.com/labring/sealos/pkg/constants"
	"github.com/labring/sealos/pkg/distribution/bom"
	"github.com/labring/sealos/pkg/distribution/compare"
	"github.com/labring/sealos/pkg/distribution/hydrate"
	"github.com/labring/sealos/pkg/distribution/localrepo"
	"github.com/labring/sealos/pkg/distribution/packageformat"
	"github.com/labring/sealos/pkg/distribution/reconcile"
	"github.com/labring/sealos/pkg/distribution/state"
	yamlutil "github.com/labring/sealos/pkg/utils/yaml"
)

func TestSyncWorkflowCmdCommitLocalDrift(t *testing.T) {
	previousRuntimeRoot := constants.DefaultRuntimeRootDir
	constants.DefaultRuntimeRootDir = t.TempDir()
	t.Cleanup(func() {
		constants.DefaultRuntimeRootDir = previousRuntimeRoot
	})

	previousFactory := newSyncBuildah
	newSyncBuildah = func(string) (buildah.Interface, error) {
		return nil, fmt.Errorf("unexpected buildah usage")
	}
	t.Cleanup(func() {
		newSyncBuildah = previousFactory
	})

	live := newSyncWorkflowLiveCluster(t)
	previousKubectl := outputSyncKubectl
	outputSyncKubectl = live.kubectl
	t.Cleanup(func() {
		outputSyncKubectl = previousKubectl
	})

	previousApply := runSyncApply
	runSyncApply = syncWorkflowApplyStub(t, live)
	t.Cleanup(func() {
		runSyncApply = previousApply
	})

	fixture := createSyncWorkflowFixture(t, uniqueSyncClusterName(t, "workflow-local"))

	var renderOut syncRenderOutput
	renderArgs := append([]string{
		"render",
		"--file", fixture.BOMPath,
		"--cluster", fixture.ClusterName,
		"--local-repo", fixture.LocalRepoRoot,
	}, fixture.packageSourceArgs()...)
	runSyncCommand(t, &renderOut, renderArgs...)

	var applyOut syncApplyOutput
	runSyncCommand(t, &applyOut,
		"apply",
		"--cluster", fixture.ClusterName,
		"--bundle-dir", renderOut.BundlePath,
		"--kubeconfig", fixture.KubeconfigPath,
		"--host-root", fixture.HostRoot,
	)
	if got, want := applyOut.DesiredStateDigest, renderOut.DesiredStateDigest; got != want {
		t.Fatalf("apply desiredStateDigest = %q, want %q", got, want)
	}

	live.mutateObject(t, "ConfigMap", "default", "grafana-settings", func(object map[string]interface{}) {
		data := ensureNestedMap(object, "data")
		data["adminUser"] = "ops"
	})
	driftedKubeadm := "apiVersion: kubeadm.k8s.io/v1beta4\nkind: ClusterConfiguration\nclusterName: ops-demo\nnetworking:\n  podSubnet: 10.244.0.0/16\n  serviceSubnet: 10.120.0.0/12\n"
	live.writeHostPath(t, fixture.HostRoot, "/etc/kubernetes/kubeadm.yaml", []byte(driftedKubeadm), 0o644)

	var diffOut syncDiffOutput
	runSyncCommand(t, &diffOut,
		"diff",
		"--cluster", fixture.ClusterName,
		"--bundle-dir", renderOut.BundlePath,
		"--kubeconfig", fixture.KubeconfigPath,
		"--host-root", fixture.HostRoot,
	)
	if got, want := diffOut.CurrentState, state.StateDirty; got != want {
		t.Fatalf("diff currentState = %q, want %q", got, want)
	}
	if got, want := diffOut.CurrentCompare.Summary.Dirty, 2; got != want {
		t.Fatalf("diff summary.dirty = %d, want %d", got, want)
	}
	if diffOut.SourcePreflight == nil {
		t.Fatal("diff sourcePreflight = nil, want rendered bundle source preflight summary")
	}
	if diffOut.SourcePreflight.Blocked {
		t.Fatalf("diff sourcePreflight.blocked = true, reasons=%#v", diffOut.SourcePreflight.BlockedReasons)
	}
	if diffOut.SourcePreflight.State == "" {
		t.Fatal("diff sourcePreflight.state is empty")
	}
	if len(diffOut.Warnings) != 0 {
		t.Fatalf("diff warnings = %#v, want empty for current rendered bundle", diffOut.Warnings)
	}
	grafanaObject := mustFindWorkflowObjectStatus(t, diffOut.CurrentCompare, "ConfigMap", "default", "grafana-settings")
	if got, want := grafanaObject.State, state.StateDirty; got != want {
		t.Fatalf("diff grafana state = %q, want %q", got, want)
	}
	if got, want := grafanaObject.Mismatches[0].Path, "data.adminUser"; got != want {
		t.Fatalf("diff grafana mismatch path = %q, want %q", got, want)
	}
	kubeadmHostPath := mustFindWorkflowHostPathStatus(t, diffOut.CurrentCompare, "/etc/kubernetes/kubeadm.yaml")
	if got, want := kubeadmHostPath.Tracked.HostPath, "/etc/kubernetes/kubeadm.yaml"; got != want {
		t.Fatalf("diff host path = %q, want %q", got, want)
	}
	if got, want := kubeadmHostPath.State, state.StateDirty; got != want {
		t.Fatalf("diff kubeadm host path state = %q, want %q", got, want)
	}

	var statusOut syncStatusOutput
	runSyncCommand(t, &statusOut,
		"status",
		"--cluster", fixture.ClusterName,
		"--bundle-dir", renderOut.BundlePath,
		"--kubeconfig", fixture.KubeconfigPath,
		"--host-root", fixture.HostRoot,
	)
	if got, want := statusOut.CurrentState, state.StateDirty; got != want {
		t.Fatalf("status currentState = %q, want %q", got, want)
	}
	if statusOut.SourcePreflight == nil {
		t.Fatal("status sourcePreflight = nil, want rendered bundle source preflight summary")
	}
	if statusOut.SourcePreflight.Blocked {
		t.Fatalf("status sourcePreflight.blocked = true, reasons=%#v", statusOut.SourcePreflight.BlockedReasons)
	}
	if statusOut.SourcePreflight.State == "" {
		t.Fatal("status sourcePreflight.state is empty")
	}
	if len(statusOut.Warnings) != 0 {
		t.Fatalf("status warnings = %#v, want empty for current rendered bundle", statusOut.Warnings)
	}
	if got, want := len(statusOut.DirtyObjects), 1; got != want {
		t.Fatalf("len(status dirtyObjects) = %d, want %d", got, want)
	}
	if got, want := statusOut.DirtyObjects[0].Name, "grafana-settings"; got != want {
		t.Fatalf("status dirty object = %q, want %q", got, want)
	}
	if got, want := len(statusOut.DirtyHostPaths), 1; got != want {
		t.Fatalf("len(status dirtyHostPaths) = %d, want %d", got, want)
	}
	if got, want := statusOut.DirtyHostPaths[0].Path, "/etc/kubernetes/kubeadm.yaml"; got != want {
		t.Fatalf("status dirty host path = %q, want %q", got, want)
	}

	var commitOut syncCommitOutput
	runSyncCommand(t, &commitOut,
		"commit",
		"--cluster", fixture.ClusterName,
		"--bundle-dir", renderOut.BundlePath,
		"--local-repo", fixture.LocalRepoRoot,
		"--kubeconfig", fixture.KubeconfigPath,
		"--host-root", fixture.HostRoot,
	)
	if got, want := commitOut.CurrentState, state.StateClean; got != want {
		t.Fatalf("commit currentState = %q, want %q", got, want)
	}
	if got, want := len(commitOut.CommittedObjects), 1; got != want {
		t.Fatalf("len(committedObjects) = %d, want %d", got, want)
	}
	if got, want := commitOut.CommittedObjects[0].Name, "grafana-settings"; got != want {
		t.Fatalf("committedObjects[0].name = %q, want %q", got, want)
	}
	if got, want := len(commitOut.CommittedHostPaths), 1; got != want {
		t.Fatalf("len(committedHostPaths) = %d, want %d", got, want)
	}
	if got, want := commitOut.CommittedHostPaths[0].Path, "/etc/kubernetes/kubeadm.yaml"; got != want {
		t.Fatalf("committedHostPaths[0].path = %q, want %q", got, want)
	}
	if commitOut.DesiredStateDigest == renderOut.DesiredStateDigest {
		t.Fatalf("commit desiredStateDigest = %q, want new digest", commitOut.DesiredStateDigest)
	}
	if commitOut.LocalRepoRevision == renderOut.LocalRepoRevision {
		t.Fatalf("commit localRepoRevision = %q, want new revision", commitOut.LocalRepoRevision)
	}

	patchData, err := os.ReadFile(filepath.Join(fixture.LocalRepoRoot, localrepo.PatchesDirName, "grafana", "grafana-settings.patch.yaml"))
	if err != nil {
		t.Fatalf("ReadFile(patch) error = %v", err)
	}
	if !strings.Contains(string(patchData), "adminUser: ops") {
		t.Fatalf("local patch missing committed drift:\n%s", string(patchData))
	}
	inputData, err := os.ReadFile(filepath.Join(fixture.LocalRepoRoot, localrepo.InputsDirName, "kubernetes", "kubeadm-cluster-config.yaml"))
	if err != nil {
		t.Fatalf("ReadFile(input) error = %v", err)
	}
	if got := string(inputData); got != driftedKubeadm {
		t.Fatalf("local input content = %q, want %q", got, driftedKubeadm)
	}

	var cleanDiff syncDiffOutput
	runSyncCommand(t, &cleanDiff,
		"diff",
		"--cluster", fixture.ClusterName,
		"--bundle-dir", renderOut.BundlePath,
		"--kubeconfig", fixture.KubeconfigPath,
		"--host-root", fixture.HostRoot,
	)
	if got, want := cleanDiff.CurrentState, state.StateClean; got != want {
		t.Fatalf("post-commit diff currentState = %q, want %q", got, want)
	}
	if got, want := cleanDiff.CurrentCompare.Summary.Dirty, 0; got != want {
		t.Fatalf("post-commit diff summary.dirty = %d, want %d", got, want)
	}
	if got, want := cleanDiff.CurrentCompare.Summary.Orphan, 0; got != want {
		t.Fatalf("post-commit diff summary.orphan = %d, want %d", got, want)
	}

	var cleanStatus syncStatusOutput
	runSyncCommand(t, &cleanStatus,
		"status",
		"--cluster", fixture.ClusterName,
		"--bundle-dir", renderOut.BundlePath,
		"--kubeconfig", fixture.KubeconfigPath,
		"--host-root", fixture.HostRoot,
	)
	if got, want := cleanStatus.CurrentState, state.StateClean; got != want {
		t.Fatalf("post-commit status currentState = %q, want %q", got, want)
	}
	if got, want := len(cleanStatus.DirtyObjects), 0; got != want {
		t.Fatalf("len(post-commit dirtyObjects) = %d, want %d", got, want)
	}
	if got, want := len(cleanStatus.DirtyHostPaths), 0; got != want {
		t.Fatalf("len(post-commit dirtyHostPaths) = %d, want %d", got, want)
	}
}

func TestSyncWorkflowCmdRevertGlobalDrift(t *testing.T) {
	previousRuntimeRoot := constants.DefaultRuntimeRootDir
	constants.DefaultRuntimeRootDir = t.TempDir()
	t.Cleanup(func() {
		constants.DefaultRuntimeRootDir = previousRuntimeRoot
	})

	previousFactory := newSyncBuildah
	newSyncBuildah = func(string) (buildah.Interface, error) {
		return nil, fmt.Errorf("unexpected buildah usage")
	}
	t.Cleanup(func() {
		newSyncBuildah = previousFactory
	})

	live := newSyncWorkflowLiveCluster(t)
	previousKubectl := outputSyncKubectl
	outputSyncKubectl = live.kubectl
	t.Cleanup(func() {
		outputSyncKubectl = previousKubectl
	})

	previousApply := runSyncApply
	runSyncApply = syncWorkflowApplyStub(t, live)
	t.Cleanup(func() {
		runSyncApply = previousApply
	})

	fixture := createSyncWorkflowFixture(t, uniqueSyncClusterName(t, "workflow-global"))

	var renderOut syncRenderOutput
	renderArgs := append([]string{
		"render",
		"--file", fixture.BOMPath,
		"--cluster", fixture.ClusterName,
		"--local-repo", fixture.LocalRepoRoot,
	}, fixture.packageSourceArgs()...)
	runSyncCommand(t, &renderOut, renderArgs...)

	runSyncCommand(t, &syncApplyOutput{},
		"apply",
		"--cluster", fixture.ClusterName,
		"--bundle-dir", renderOut.BundlePath,
		"--kubeconfig", fixture.KubeconfigPath,
		"--host-root", fixture.HostRoot,
	)

	live.mutateObject(t, "DaemonSet", "kube-system", "cilium", func(object map[string]interface{}) {
		spec := ensureNestedMap(object, "spec")
		template := ensureNestedMap(spec, "template")
		podSpec := ensureNestedMap(template, "spec")
		containers, _ := podSpec["containers"].([]interface{})
		container := containers[0].(map[string]interface{})
		container["image"] = "quay.io/cilium/cilium:v1.15.1"
	})
	live.writeHostPath(t, fixture.HostRoot, "/usr/bin/kubelet", []byte("drifted-kubelet"), 0o755)

	var diffOut syncDiffOutput
	runSyncCommand(t, &diffOut,
		"diff",
		"--cluster", fixture.ClusterName,
		"--bundle-dir", renderOut.BundlePath,
		"--kubeconfig", fixture.KubeconfigPath,
		"--host-root", fixture.HostRoot,
	)
	if got, want := diffOut.CurrentState, state.StateOrphan; got != want {
		t.Fatalf("diff currentState = %q, want %q", got, want)
	}
	if got, want := diffOut.CurrentCompare.Summary.Orphan, 2; got != want {
		t.Fatalf("diff summary.orphan = %d, want %d", got, want)
	}

	var statusOut syncStatusOutput
	runSyncCommand(t, &statusOut,
		"status",
		"--cluster", fixture.ClusterName,
		"--bundle-dir", renderOut.BundlePath,
		"--kubeconfig", fixture.KubeconfigPath,
		"--host-root", fixture.HostRoot,
	)
	if got, want := statusOut.CurrentState, state.StateOrphan; got != want {
		t.Fatalf("status currentState = %q, want %q", got, want)
	}
	if got, want := len(statusOut.OrphanObjects), 1; got != want {
		t.Fatalf("len(status orphanObjects) = %d, want %d", got, want)
	}
	if got, want := statusOut.OrphanObjects[0].Name, "cilium"; got != want {
		t.Fatalf("status orphan object = %q, want %q", got, want)
	}
	if got, want := len(statusOut.OrphanHostPaths), 1; got != want {
		t.Fatalf("len(status orphanHostPaths) = %d, want %d", got, want)
	}
	if got, want := statusOut.OrphanHostPaths[0].Path, "/usr/bin/kubelet"; got != want {
		t.Fatalf("status orphan host path = %q, want %q", got, want)
	}

	var revertObject syncRevertOutput
	runSyncCommand(t, &revertObject,
		"revert",
		"--cluster", fixture.ClusterName,
		"--bundle-dir", renderOut.BundlePath,
		"--kubeconfig", fixture.KubeconfigPath,
		"--host-root", fixture.HostRoot,
		"--kind", "DaemonSet",
		"--namespace", "kube-system",
		"--name", "cilium",
	)
	if got, want := revertObject.BeforeState, state.StateOrphan; got != want {
		t.Fatalf("revert object beforeState = %q, want %q", got, want)
	}
	if got, want := revertObject.CurrentState, state.StateOrphan; got != want {
		t.Fatalf("revert object currentState = %q, want %q", got, want)
	}
	if !revertObject.Reverted {
		t.Fatal("revert object reverted = false, want true")
	}
	if got, want := len(revertObject.RevertedObjects), 1; got != want {
		t.Fatalf("len(revertedObjects) = %d, want %d", got, want)
	}
	if got, want := revertObject.RevertedObjects[0].Name, "cilium"; got != want {
		t.Fatalf("revertedObjects[0].name = %q, want %q", got, want)
	}

	liveObject := live.object("DaemonSet", "kube-system", "cilium")
	if !bytes.Contains(liveObject, []byte(`"image":"quay.io/cilium/cilium:v1.15.0"`)) {
		t.Fatalf("live cilium object after revert = %s, want image v1.15.0", string(liveObject))
	}

	var revertHost syncRevertOutput
	runSyncCommand(t, &revertHost,
		"revert",
		"--cluster", fixture.ClusterName,
		"--bundle-dir", renderOut.BundlePath,
		"--kubeconfig", fixture.KubeconfigPath,
		"--host-root", fixture.HostRoot,
		"--host-path", "/usr/bin/kubelet",
	)
	if got, want := revertHost.CurrentState, state.StateClean; got != want {
		t.Fatalf("revert host currentState = %q, want %q", got, want)
	}
	if !revertHost.Reverted {
		t.Fatal("revert host reverted = false, want true")
	}
	if got, want := len(revertHost.RevertedHostPaths), 1; got != want {
		t.Fatalf("len(revertedHostPaths) = %d, want %d", got, want)
	}
	if got, want := revertHost.RevertedHostPaths[0].Path, "/usr/bin/kubelet"; got != want {
		t.Fatalf("revertedHostPaths[0].path = %q, want %q", got, want)
	}
	data, err := os.ReadFile(filepath.Join(fixture.HostRoot, "usr", "bin", "kubelet"))
	if err != nil {
		t.Fatalf("ReadFile(kubelet) error = %v", err)
	}
	if got, want := string(data), "desired-kubelet"; got != want {
		t.Fatalf("live kubelet content = %q, want %q", got, want)
	}

	var cleanDiff syncDiffOutput
	runSyncCommand(t, &cleanDiff,
		"diff",
		"--cluster", fixture.ClusterName,
		"--bundle-dir", renderOut.BundlePath,
		"--kubeconfig", fixture.KubeconfigPath,
		"--host-root", fixture.HostRoot,
	)
	if got, want := cleanDiff.CurrentState, state.StateClean; got != want {
		t.Fatalf("post-revert diff currentState = %q, want %q", got, want)
	}
	if got, want := cleanDiff.CurrentCompare.Summary.Orphan, 0; got != want {
		t.Fatalf("post-revert diff summary.orphan = %d, want %d", got, want)
	}

	var cleanStatus syncStatusOutput
	runSyncCommand(t, &cleanStatus,
		"status",
		"--cluster", fixture.ClusterName,
		"--bundle-dir", renderOut.BundlePath,
		"--kubeconfig", fixture.KubeconfigPath,
		"--host-root", fixture.HostRoot,
	)
	if got, want := cleanStatus.CurrentState, state.StateClean; got != want {
		t.Fatalf("post-revert status currentState = %q, want %q", got, want)
	}
	if got, want := len(cleanStatus.OrphanObjects), 0; got != want {
		t.Fatalf("len(post-revert orphanObjects) = %d, want %d", got, want)
	}
	if got, want := len(cleanStatus.OrphanHostPaths), 0; got != want {
		t.Fatalf("len(post-revert orphanHostPaths) = %d, want %d", got, want)
	}
}

type syncWorkflowFixture struct {
	ClusterName    string
	BOMPath        string
	LocalRepoRoot  string
	HostRoot       string
	KubeconfigPath string
	PackageRoots   map[string]string
}

func (f syncWorkflowFixture) packageSourceArgs() []string {
	keys := make([]string, 0, len(f.PackageRoots))
	for key := range f.PackageRoots {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	args := make([]string, 0, len(keys)*2)
	for _, key := range keys {
		args = append(args, "--package-source", key+"="+f.PackageRoots[key])
	}
	return args
}

func createSyncWorkflowFixture(t *testing.T, clusterName string) syncWorkflowFixture {
	t.Helper()

	root := t.TempDir()
	packagesRoot := filepath.Join(root, "packages")
	localRepoRoot := filepath.Join(root, "local-repo")
	hostRoot := filepath.Join(root, "host-root")
	bomPath := filepath.Join(root, "bom.yaml")
	kubeconfigPath := filepath.Join(root, "admin.conf")

	packageRoots := map[string]string{
		"cilium":     filepath.Join(packagesRoot, "cilium"),
		"grafana":    filepath.Join(packagesRoot, "grafana"),
		"kubernetes": filepath.Join(packagesRoot, "kubernetes"),
	}
	writeSyncWorkflowKubernetesPackage(t, packageRoots["kubernetes"])
	writeSyncWorkflowGrafanaPackage(t, packageRoots["grafana"])
	writeSyncWorkflowCiliumPackage(t, packageRoots["cilium"])
	writeSyncWorkflowLocalRepo(t, localRepoRoot)

	if err := os.MkdirAll(filepath.Dir(kubeconfigPath), 0o755); err != nil {
		t.Fatalf("MkdirAll(kubeconfig) error = %v", err)
	}
	if err := os.WriteFile(kubeconfigPath, []byte("apiVersion: v1\nkind: Config\n"), 0o644); err != nil {
		t.Fatalf("WriteFile(kubeconfig) error = %v", err)
	}

	doc := bom.New("workflow-platform", "rev-workflow-001", bom.ChannelAlpha)
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
			Name:    "grafana",
			Kind:    "addon",
			Version: "v10.4.0",
			Artifact: bom.ArtifactReference{
				Name:   "grafana",
				Image:  "registry.example.io/sealos/grafana:v10.4.0",
				Digest: "sha256:2222222222222222222222222222222222222222222222222222222222222222",
			},
		},
		{
			Name:    "cilium",
			Kind:    "addon",
			Version: "v1.15.0",
			Artifact: bom.ArtifactReference{
				Name:   "cilium-cni",
				Image:  "registry.example.io/sealos/cilium-cni:v1.15.0",
				Digest: "sha256:3333333333333333333333333333333333333333333333333333333333333333",
			},
		},
	}
	if err := yamlutil.MarshalFile(bomPath, doc); err != nil {
		t.Fatalf("MarshalFile(bom) error = %v", err)
	}

	return syncWorkflowFixture{
		ClusterName:    clusterName,
		BOMPath:        bomPath,
		LocalRepoRoot:  localRepoRoot,
		HostRoot:       hostRoot,
		KubeconfigPath: kubeconfigPath,
		PackageRoots:   packageRoots,
	}
}

func writeSyncWorkflowKubernetesPackage(t *testing.T, root string) {
	t.Helper()

	pkg := packageformat.New("kubernetes-rootfs", "kubernetes", "v1.30.3", packageformat.ClassRootfs)
	pkg.Spec.Inputs = []packageformat.Input{
		{
			Name:     "kubeadm-cluster-config",
			Type:     packageformat.InputConfigFile,
			Path:     "files/etc/kubernetes/kubeadm.yaml",
			Format:   "yaml",
			Required: true,
		},
	}
	pkg.Spec.Contents = []packageformat.Content{
		{Name: "kubernetes-rootfs", Type: packageformat.ContentRootfs, Path: "rootfs/"},
		{Name: "kubeadm-defaults", Type: packageformat.ContentFile, Path: "files/etc/kubernetes/kubeadm.yaml"},
	}
	writeSyncWorkflowPackage(t, root, pkg, map[string]string{
		"rootfs/usr/bin/kubelet":            "desired-kubelet",
		"files/etc/kubernetes/kubeadm.yaml": "apiVersion: kubeadm.k8s.io/v1beta4\nkind: ClusterConfiguration\nclusterName: default-demo\nnetworking:\n  podSubnet: 10.244.0.0/16\n  serviceSubnet: 10.96.0.0/12\n",
	})
}

func writeSyncWorkflowGrafanaPackage(t *testing.T, root string) {
	t.Helper()

	pkg := packageformat.New("grafana", "grafana", "v10.4.0", packageformat.ClassApplication)
	pkg.Spec.Contents = []packageformat.Content{
		{Name: "grafana-settings", Type: packageformat.ContentManifest, Path: "manifests/grafana-settings.yaml"},
	}
	writeSyncWorkflowPackage(t, root, pkg, map[string]string{
		"manifests/grafana-settings.yaml": "apiVersion: v1\nkind: ConfigMap\nmetadata:\n  name: grafana-settings\n  namespace: default\ndata:\n  adminUser: admin\n  imageTag: \"10.4.0\"\n",
	})
}

func writeSyncWorkflowCiliumPackage(t *testing.T, root string) {
	t.Helper()

	pkg := packageformat.New("cilium-cni", "cilium", "v1.15.0", packageformat.ClassApplication)
	pkg.Spec.Contents = []packageformat.Content{
		{Name: "cilium-manifest", Type: packageformat.ContentManifest, Path: "manifests/cilium.yaml"},
	}
	writeSyncWorkflowPackage(t, root, pkg, map[string]string{
		"manifests/cilium.yaml": "apiVersion: apps/v1\nkind: DaemonSet\nmetadata:\n  name: cilium\n  namespace: kube-system\nspec:\n  template:\n    spec:\n      containers:\n        - name: cilium-agent\n          image: quay.io/cilium/cilium:v1.15.0\n",
	})
}

func writeSyncWorkflowPackage(t *testing.T, root string, pkg *packageformat.ComponentPackage, files map[string]string) {
	t.Helper()

	if err := os.MkdirAll(root, 0o755); err != nil {
		t.Fatalf("MkdirAll(%q) error = %v", root, err)
	}
	if err := yamlutil.MarshalFile(filepath.Join(root, "package.yaml"), pkg); err != nil {
		t.Fatalf("MarshalFile(package.yaml) error = %v", err)
	}
	for rel, content := range files {
		path := filepath.Join(root, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatalf("MkdirAll(%q) error = %v", filepath.Dir(path), err)
		}
		mode := os.FileMode(0o644)
		if strings.HasPrefix(rel, "rootfs/") {
			mode = 0o755
		}
		if err := os.WriteFile(path, []byte(content), mode); err != nil {
			t.Fatalf("WriteFile(%q) error = %v", path, err)
		}
	}
}

func writeSyncWorkflowLocalRepo(t *testing.T, root string) {
	t.Helper()

	files := map[string]string{
		filepath.Join(localrepo.InputsDirName, "kubernetes", "kubeadm-cluster-config.yaml"): "apiVersion: kubeadm.k8s.io/v1beta4\nkind: ClusterConfiguration\nclusterName: demo\nnetworking:\n  podSubnet: 10.244.0.0/16\n  serviceSubnet: 10.96.0.0/12\n",
		filepath.Join(localrepo.PatchesDirName, "grafana", "grafana-settings.patch.yaml"):   "apiVersion: v1\nkind: ConfigMap\nmetadata:\n  name: grafana-settings\n  namespace: default\ndata:\n  adminUser: root\n",
	}
	for rel, content := range files {
		path := filepath.Join(root, rel)
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatalf("MkdirAll(%q) error = %v", filepath.Dir(path), err)
		}
		if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
			t.Fatalf("WriteFile(%q) error = %v", path, err)
		}
	}
}

func runSyncCommand(t *testing.T, out interface{}, args ...string) []byte {
	t.Helper()

	buf := bytes.NewBuffer(nil)
	cmd := newSyncCmd()
	cmd.SetOut(buf)
	cmd.SetErr(buf)
	cmd.SetArgs(args)
	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute(%v) error = %v\noutput=%s", args, err, buf.String())
	}
	if out != nil {
		if err := yaml.Unmarshal(buf.Bytes(), out); err != nil {
			t.Fatalf("Unmarshal(%v) error = %v\noutput=%s", args, err, buf.String())
		}
	}
	return buf.Bytes()
}

func syncWorkflowApplyStub(t *testing.T, live *syncWorkflowLiveCluster) func(reconcile.ApplyOptions) (*reconcile.ApplyResult, error) {
	t.Helper()

	return func(opts reconcile.ApplyOptions) (*reconcile.ApplyResult, error) {
		bundle, err := reconcile.LoadBundle(opts.BundlePath)
		if err != nil {
			return nil, err
		}
		if err := seedSyncWorkflowLiveState(bundle, opts.BundlePath, opts.HostRoot, live); err != nil {
			return nil, err
		}
		desiredStateDigest, err := hydrate.DigestBundle(opts.BundlePath)
		if err != nil {
			return nil, err
		}
		applied, err := state.MarkSuccessfulApply(opts.ClusterName)
		if err != nil {
			return nil, err
		}
		return &reconcile.ApplyResult{
			Bundle:             bundle,
			BundlePath:         opts.BundlePath,
			DesiredStateDigest: desiredStateDigest.String(),
			AppliedRevision:    applied,
		}, nil
	}
}

func seedSyncWorkflowLiveState(bundle *hydrate.Bundle, bundleRoot, hostRoot string, live *syncWorkflowLiveCluster) error {
	if bundle == nil {
		return fmt.Errorf("bundle cannot be nil")
	}
	for _, group := range groupWorkflowTrackedObjects(bundle.Spec.TrackedK8sObjects) {
		desired, err := compare.DesiredObjectYAML(bundleRoot, compare.ObjectStatus{
			Tracked:   group[0],
			Fragments: group,
		})
		if err != nil {
			return err
		}
		desiredJSON, err := yaml.YAMLToJSON(desired)
		if err != nil {
			return err
		}
		live.setObject(group[0].Kind, group[0].Namespace, group[0].Name, desiredJSON)
	}

	for _, tracked := range bundle.Spec.TrackedHostPaths {
		if tracked.ProjectionClass == hydrate.HostPathProjectionClassGenerated || tracked.BundlePath == "" {
			continue
		}
		sourcePath := filepath.Join(bundleRoot, filepath.FromSlash(tracked.BundlePath))
		data, err := os.ReadFile(sourcePath)
		if err != nil {
			return err
		}
		mode := os.FileMode(0o644)
		if info, err := os.Stat(sourcePath); err == nil {
			mode = info.Mode()
		}
		if err := os.MkdirAll(filepath.Dir(filepath.Join(hostRoot, strings.TrimPrefix(filepath.Clean(tracked.HostPath), string(os.PathSeparator)))), 0o755); err != nil {
			return err
		}
		resolved := filepath.Join(hostRoot, strings.TrimPrefix(filepath.Clean(tracked.HostPath), string(os.PathSeparator)))
		if err := os.WriteFile(resolved, data, mode); err != nil {
			return err
		}
	}
	return nil
}

func groupWorkflowTrackedObjects(tracked []hydrate.TrackedK8sObject) [][]hydrate.TrackedK8sObject {
	groups := make([][]hydrate.TrackedK8sObject, 0)
	index := make(map[string]int, len(tracked))
	for _, object := range tracked {
		key := strings.Join([]string{object.APIVersion, object.Kind, object.Namespace, object.Name}, "\x00")
		if position, ok := index[key]; ok {
			groups[position] = append(groups[position], object)
			continue
		}
		index[key] = len(groups)
		groups = append(groups, []hydrate.TrackedK8sObject{object})
	}
	return groups
}

type syncWorkflowLiveCluster struct {
	t       *testing.T
	objects map[string][]byte
}

func newSyncWorkflowLiveCluster(t *testing.T) *syncWorkflowLiveCluster {
	t.Helper()
	return &syncWorkflowLiveCluster{
		t:       t,
		objects: make(map[string][]byte),
	}
}

func (c *syncWorkflowLiveCluster) object(kind, namespace, name string) []byte {
	c.t.Helper()
	data, ok := c.objects[c.objectKey(kind, namespace, name)]
	if !ok {
		c.t.Fatalf("live object %s %s/%s missing", kind, namespace, name)
	}
	return append([]byte(nil), data...)
}

func (c *syncWorkflowLiveCluster) setObject(kind, namespace, name string, data []byte) {
	c.objects[c.objectKey(kind, namespace, name)] = append([]byte(nil), data...)
}

func (c *syncWorkflowLiveCluster) mutateObject(t *testing.T, kind, namespace, name string, mutate func(map[string]interface{})) {
	t.Helper()

	key := c.objectKey(kind, namespace, name)
	data, ok := c.objects[key]
	if !ok {
		t.Fatalf("live object %s %s/%s missing", kind, namespace, name)
	}
	var object map[string]interface{}
	if err := json.Unmarshal(data, &object); err != nil {
		t.Fatalf("Unmarshal(live object %s %s/%s) error = %v", kind, namespace, name, err)
	}
	mutate(object)
	updated, err := json.Marshal(object)
	if err != nil {
		t.Fatalf("Marshal(live object %s %s/%s) error = %v", kind, namespace, name, err)
	}
	c.objects[key] = updated
}

func (c *syncWorkflowLiveCluster) kubectl(args ...string) ([]byte, error) {
	trimmed := stripSyncWorkflowKubeconfigArgs(args)
	if len(trimmed) == 0 {
		return nil, fmt.Errorf("empty kubectl arguments")
	}

	switch trimmed[0] {
	case "get":
		if len(trimmed) < 3 {
			return nil, fmt.Errorf("invalid kubectl get arguments: %v", trimmed)
		}
		kind := trimmed[1]
		name := trimmed[2]
		namespace := syncWorkflowNamespaceArg(trimmed[3:])
		data, ok := c.objects[c.objectKey(kind, namespace, name)]
		if !ok {
			return nil, fmt.Errorf("Error from server (NotFound): %s %q not found", strings.ToLower(kind), name)
		}
		return append([]byte(nil), data...), nil
	case "apply":
		manifestPath, err := syncWorkflowApplyFileArg(trimmed[1:])
		if err != nil {
			return nil, err
		}
		data, err := os.ReadFile(manifestPath)
		if err != nil {
			return nil, err
		}
		decoder := k8syaml.NewYAMLOrJSONDecoder(bytes.NewReader(data), 4096)
		applied := make([]string, 0)
		for {
			var raw runtime.RawExtension
			if err := decoder.Decode(&raw); err != nil {
				if err == io.EOF {
					break
				}
				return nil, err
			}
			if len(bytes.TrimSpace(raw.Raw)) == 0 {
				continue
			}
			var meta struct {
				Kind     string `json:"kind" yaml:"kind"`
				Metadata struct {
					Name      string `json:"name" yaml:"name"`
					Namespace string `json:"namespace,omitempty" yaml:"namespace,omitempty"`
				} `json:"metadata" yaml:"metadata"`
			}
			if err := yaml.Unmarshal(raw.Raw, &meta); err != nil {
				return nil, err
			}
			objectJSON, err := yaml.YAMLToJSON(raw.Raw)
			if err != nil {
				return nil, err
			}
			c.setObject(meta.Kind, meta.Metadata.Namespace, meta.Metadata.Name, objectJSON)
			applied = append(applied, fmt.Sprintf("%s/%s configured", strings.ToLower(meta.Kind), meta.Metadata.Name))
		}
		return []byte(strings.Join(applied, "\n")), nil
	default:
		return nil, fmt.Errorf("unexpected kubectl invocation: %s", strings.Join(trimmed, " "))
	}
}

func (c *syncWorkflowLiveCluster) writeHostPath(t *testing.T, hostRoot, trackedHostPath string, data []byte, mode os.FileMode) {
	t.Helper()
	resolved := filepath.Join(hostRoot, strings.TrimPrefix(filepath.Clean(trackedHostPath), string(os.PathSeparator)))
	if err := os.MkdirAll(filepath.Dir(resolved), 0o755); err != nil {
		t.Fatalf("MkdirAll(%q) error = %v", filepath.Dir(resolved), err)
	}
	if err := os.WriteFile(resolved, data, mode); err != nil {
		t.Fatalf("WriteFile(%q) error = %v", resolved, err)
	}
}

func (c *syncWorkflowLiveCluster) objectKey(kind, namespace, name string) string {
	return strings.Join([]string{kind, namespace, name}, "\x00")
}

func stripSyncWorkflowKubeconfigArgs(args []string) []string {
	trimmed := make([]string, 0, len(args))
	skipNext := false
	for _, arg := range args {
		if skipNext {
			skipNext = false
			continue
		}
		if arg == "--kubeconfig" {
			skipNext = true
			continue
		}
		trimmed = append(trimmed, arg)
	}
	return trimmed
}

func syncWorkflowNamespaceArg(args []string) string {
	for i := 0; i < len(args); i++ {
		if args[i] == "-n" || args[i] == "--namespace" {
			if i+1 < len(args) {
				return args[i+1]
			}
			return ""
		}
	}
	return ""
}

func syncWorkflowApplyFileArg(args []string) (string, error) {
	for i := 0; i < len(args); i++ {
		if args[i] == "-f" || args[i] == "--filename" {
			if i+1 < len(args) {
				return args[i+1], nil
			}
			return "", fmt.Errorf("kubectl apply missing filename")
		}
	}
	return "", fmt.Errorf("kubectl apply missing -f")
}

func ensureNestedMap(object map[string]interface{}, key string) map[string]interface{} {
	if value, ok := object[key]; ok {
		if typed, ok := value.(map[string]interface{}); ok {
			return typed
		}
	}
	created := make(map[string]interface{})
	object[key] = created
	return created
}

func mustFindWorkflowObjectStatus(t *testing.T, result *compare.Result, kind, namespace, name string) compare.ObjectStatus {
	t.Helper()
	for _, object := range result.Objects {
		if object.Tracked.Kind == kind && object.Tracked.Namespace == namespace && object.Tracked.Name == name {
			return object
		}
	}
	t.Fatalf("tracked object %s %s/%s not found in compare result", kind, namespace, name)
	return compare.ObjectStatus{}
}

func mustFindWorkflowHostPathStatus(t *testing.T, result *compare.Result, hostPath string) compare.HostPathStatus {
	t.Helper()
	for _, tracked := range result.HostPaths {
		if tracked.Tracked.HostPath == hostPath {
			return tracked
		}
	}
	t.Fatalf("tracked host path %q not found in compare result", hostPath)
	return compare.HostPathStatus{}
}
