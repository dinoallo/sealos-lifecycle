package cmd

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"sigs.k8s.io/yaml"

	"github.com/labring/sealos/pkg/buildah"
	"github.com/labring/sealos/pkg/constants"
	"github.com/labring/sealos/pkg/distribution/bom"
	"github.com/labring/sealos/pkg/distribution/state"
	yamlutil "github.com/labring/sealos/pkg/utils/yaml"
)

func TestSyncRenderCmdWithLocalPackageSource(t *testing.T) {
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

	bomPath := filepath.Join(t.TempDir(), "bom.yaml")
	if err := yamlutil.MarshalFile(bomPath, testSyncBOM()); err != nil {
		t.Fatalf("MarshalFile() error = %v", err)
	}

	buf := bytes.NewBuffer(nil)
	cmd := newSyncCmd()
	cmd.SetOut(buf)
	cmd.SetErr(buf)
	cmd.SetArgs([]string{
		"render",
		"--file", bomPath,
		"--cluster", "cluster-a",
		"--local-patch-revision", "local-rev-1",
		"--package-source", "kubernetes=" + syncFixtureRoot(),
	})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute() error = %v", err)
	}

	var out syncRenderOutput
	if err := yaml.Unmarshal(buf.Bytes(), &out); err != nil {
		t.Fatalf("Unmarshal() error = %v\noutput=%s", err, buf.String())
	}

	if got, want := out.ClusterName, "cluster-a"; got != want {
		t.Fatalf("out.ClusterName = %q, want %q", got, want)
	}
	if !strings.HasPrefix(out.DesiredStateDigest, "sha256:") {
		t.Fatalf("out.DesiredStateDigest = %q, want sha256 digest", out.DesiredStateDigest)
	}

	doc, err := state.LoadAppliedRevision("cluster-a")
	if err != nil {
		t.Fatalf("LoadAppliedRevision() error = %v", err)
	}
	if got, want := doc.Status.State, state.StateDirty; got != want {
		t.Fatalf("status.state = %q, want %q", got, want)
	}
	if got, want := doc.Spec.LocalPatchRevision, "local-rev-1"; got != want {
		t.Fatalf("spec.localPatchRevision = %q, want %q", got, want)
	}
	if got, want := doc.Spec.DesiredStateDigest, out.DesiredStateDigest; got != want {
		t.Fatalf("spec.desiredStateDigest = %q, want %q", got, want)
	}
}

func TestSyncRenderCmdRejectsInvalidPackageSource(t *testing.T) {
	bomPath := filepath.Join(t.TempDir(), "bom.yaml")
	if err := yamlutil.MarshalFile(bomPath, testSyncBOM()); err != nil {
		t.Fatalf("MarshalFile() error = %v", err)
	}

	cmd := newSyncCmd()
	cmd.SetArgs([]string{
		"render",
		"--file", bomPath,
		"--package-source", "invalid",
	})

	if err := cmd.Execute(); err == nil {
		t.Fatal("Execute() error = nil, want error")
	}
}

func TestSyncRenderCmdWithMinimalSingleNodePOC(t *testing.T) {
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

	buf := bytes.NewBuffer(nil)
	cmd := newSyncCmd()
	cmd.SetOut(buf)
	cmd.SetErr(buf)
	cmd.SetArgs([]string{
		"render",
		"--file", syncPOCBOMPath(),
		"--cluster", "poc-minimal",
		"--package-source", "containerd=" + syncPOCPackageRoot("containerd"),
		"--package-source", "kubernetes=" + syncPOCPackageRoot("kubernetes"),
		"--package-source", "cilium=" + syncPOCPackageRoot("cilium"),
	})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute() error = %v", err)
	}

	var out syncRenderOutput
	if err := yaml.Unmarshal(buf.Bytes(), &out); err != nil {
		t.Fatalf("Unmarshal() error = %v\noutput=%s", err, buf.String())
	}

	if got, want := out.ClusterName, "poc-minimal"; got != want {
		t.Fatalf("out.ClusterName = %q, want %q", got, want)
	}
	if got, want := out.Revision, "rev-poc-001"; got != want {
		t.Fatalf("out.Revision = %q, want %q", got, want)
	}
	if !strings.HasPrefix(out.DesiredStateDigest, "sha256:") {
		t.Fatalf("out.DesiredStateDigest = %q, want sha256 digest", out.DesiredStateDigest)
	}

	for _, rel := range []string{
		"bundle.yaml",
		"components/containerd/package.yaml",
		"components/containerd/files/rootfs/README",
		"components/kubernetes/files/manifests/bootstrap/kubelet-bootstrap-rbac.yaml",
		"components/cilium/files/manifests/cilium.yaml",
	} {
		if _, err := os.Stat(filepath.Join(out.BundlePath, rel)); err != nil {
			t.Fatalf("rendered bundle missing %q: %v", rel, err)
		}
	}

	doc, err := state.LoadAppliedRevision("poc-minimal")
	if err != nil {
		t.Fatalf("LoadAppliedRevision() error = %v", err)
	}
	if got, want := doc.Status.State, state.StateDirty; got != want {
		t.Fatalf("status.state = %q, want %q", got, want)
	}
	if got, want := doc.Spec.BOM.Name, "minimal-single-node"; got != want {
		t.Fatalf("spec.bom.name = %q, want %q", got, want)
	}
	if got, want := doc.Spec.DesiredStateDigest, out.DesiredStateDigest; got != want {
		t.Fatalf("spec.desiredStateDigest = %q, want %q", got, want)
	}
}

func testSyncBOM() *bom.BOM {
	doc := bom.New("default-platform", "rev-20240423", bom.ChannelBeta)
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
	return doc
}

func syncFixtureRoot() string {
	return filepath.Join("..", "..", "..", "pkg", "distribution", "packageformat", "testdata", "kubernetes-rootfs")
}

func syncPOCBOMPath() string {
	return filepath.Join(syncPOCRoot(), "bom.yaml")
}

func syncPOCPackageRoot(name string) string {
	return filepath.Join(syncPOCRoot(), "packages", name)
}

func syncPOCRoot() string {
	return filepath.Join("..", "..", "..", "scripts", "poc", "minimal-single-node")
}
