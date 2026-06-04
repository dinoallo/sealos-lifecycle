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

package reconcile

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/labring/sealos/pkg/constants"
	"github.com/labring/sealos/pkg/distribution/bom"
	"github.com/labring/sealos/pkg/distribution/hydrate"
	"github.com/labring/sealos/pkg/distribution/localrepo"
	"github.com/labring/sealos/pkg/distribution/packageformat"
	"github.com/labring/sealos/pkg/distribution/state"
	v1beta1 "github.com/labring/sealos/pkg/types/v1beta1"
	fileutil "github.com/labring/sealos/pkg/utils/file"
	yamlutil "github.com/labring/sealos/pkg/utils/yaml"
)

func TestMaterializeFile(t *testing.T) {
	previousRuntimeRoot := constants.DefaultRuntimeRootDir
	constants.DefaultRuntimeRootDir = t.TempDir()
	t.Cleanup(func() {
		constants.DefaultRuntimeRootDir = previousRuntimeRoot
	})

	sourceRoot := fixtureRoot()
	doc := testBOM()
	bomPath := filepath.Join(t.TempDir(), "bom.yaml")
	if err := yamlutil.MarshalFile(bomPath, doc); err != nil {
		t.Fatalf("MarshalFile() error = %v", err)
	}

	source := &trackedSourceProvider{root: sourceRoot}
	result, err := MaterializeFile(bomPath, Options{
		ClusterName:        "cluster-a",
		LocalPatchRevision: "local-rev-1",
		PackageLoader:      loaderForDir(doc.Spec.Packages[0].Artifact.Reference(), sourceRoot),
		Sources:            source,
	})
	if err != nil {
		t.Fatalf("MaterializeFile() error = %v", err)
	}

	if got, want := source.closed, 1; got != want {
		t.Fatalf("source.closed = %d, want %d", got, want)
	}
	if got, want := result.BundlePath, CurrentBundlePath("cluster-a"); got != want {
		t.Fatalf("result.BundlePath = %q, want %q", got, want)
	}
	if !strings.HasPrefix(result.DesiredStateDigest, "sha256:") {
		t.Fatalf("result.DesiredStateDigest = %q, want sha256 digest", result.DesiredStateDigest)
	}
	if got, want := result.AppliedRevision.Spec.LocalPatchRevision, "local-rev-1"; got != want {
		t.Fatalf("appliedRevision.spec.localPatchRevision = %q, want %q", got, want)
	}

	for _, rel := range []string{
		hydrate.BundleFileName,
		filepath.Join("components", "kubernetes", "package.yaml"),
		filepath.Join("components", "kubernetes", "files", "hooks", "bootstrap.sh"),
	} {
		if _, err := os.Stat(filepath.Join(result.BundlePath, rel)); err != nil {
			t.Fatalf("Stat(%q) error = %v", rel, err)
		}
	}

	loaded, err := state.LoadAppliedRevision("cluster-a")
	if err != nil {
		t.Fatalf("LoadAppliedRevision() error = %v", err)
	}
	if got, want := loaded.Status.State, state.StateDirty; got != want {
		t.Fatalf("loaded.status.state = %q, want %q", got, want)
	}
	if got, want := loaded.Spec.DesiredStateDigest, result.DesiredStateDigest; got != want {
		t.Fatalf("loaded.spec.desiredStateDigest = %q, want %q", got, want)
	}
	if !strings.HasPrefix(loaded.Spec.BOM.Digest, "sha256:") {
		t.Fatalf("loaded.spec.bom.digest = %q, want sha256 digest", loaded.Spec.BOM.Digest)
	}
	if loaded.Spec.RequestedTarget == nil {
		t.Fatal("loaded.spec.requestedTarget = nil, want value")
	}
	if got, want := loaded.Spec.RequestedTarget.Kind, state.TargetKindBOM; got != want {
		t.Fatalf("loaded.spec.requestedTarget.kind = %q, want %q", got, want)
	}
	if got, want := loaded.Spec.RequestedTarget.BOMPath, bomPath; got != want {
		t.Fatalf("loaded.spec.requestedTarget.bomPath = %q, want %q", got, want)
	}
	if loaded.Spec.ResolvedTarget == nil {
		t.Fatal("loaded.spec.resolvedTarget = nil, want value")
	}
	if got, want := loaded.Spec.ResolvedTarget.BOM, loaded.Spec.BOM; got != want {
		t.Fatalf("loaded.spec.resolvedTarget.bom = %#v, want %#v", got, want)
	}
	if got, want := result.Bundle.Spec.ExecutionTopology.FirstMaster, "localhost"; got != want {
		t.Fatalf("bundle.executionTopology.firstMaster = %q, want %q", got, want)
	}
	if got, want := strings.Join(result.Bundle.Spec.ExecutionTopology.AllNodes, ","), "localhost"; got != want {
		t.Fatalf("bundle.executionTopology.allNodes = %q, want %q", got, want)
	}
	if got, want := result.Bundle.Spec.ExecutionTopology.RolesForHost("localhost")[0], "master"; got != want {
		t.Fatalf("bundle.executionTopology localhost role = %q, want %q", got, want)
	}
}

func TestMaterializeReplacesCurrentBundle(t *testing.T) {
	previousRuntimeRoot := constants.DefaultRuntimeRootDir
	constants.DefaultRuntimeRootDir = t.TempDir()
	t.Cleanup(func() {
		constants.DefaultRuntimeRootDir = previousRuntimeRoot
	})

	doc := testBOM()
	bomPath := filepath.Join(t.TempDir(), "bom.yaml")
	if err := yamlutil.MarshalFile(bomPath, doc); err != nil {
		t.Fatalf("MarshalFile() error = %v", err)
	}

	sourceA := fixtureRoot()
	first, err := MaterializeFile(bomPath, Options{
		ClusterName:   "cluster-a",
		PackageLoader: loaderForDir(doc.Spec.Packages[0].Artifact.Reference(), sourceA),
		Sources:       hydrate.SourceMap{"kubernetes": sourceA},
	})
	if err != nil {
		t.Fatalf("first MaterializeFile() error = %v", err)
	}

	sourceB := copiedFixture(t)
	hookPath := filepath.Join(sourceB, "hooks", "bootstrap.sh")
	if err := os.WriteFile(hookPath, []byte("#!/bin/sh\necho changed\n"), 0o755); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	second, err := MaterializeFile(bomPath, Options{
		ClusterName:   "cluster-a",
		PackageLoader: loaderForDir(doc.Spec.Packages[0].Artifact.Reference(), sourceB),
		Sources:       hydrate.SourceMap{"kubernetes": sourceB},
	})
	if err != nil {
		t.Fatalf("second MaterializeFile() error = %v", err)
	}

	if first.DesiredStateDigest == second.DesiredStateDigest {
		t.Fatalf("desiredStateDigest = %q for both renders, want different digests", second.DesiredStateDigest)
	}

	renderedHook, err := os.ReadFile(filepath.Join(CurrentBundlePath("cluster-a"), "components", "kubernetes", "files", "hooks", "bootstrap.sh"))
	if err != nil {
		t.Fatalf("ReadFile() error = %v", err)
	}
	if !strings.Contains(string(renderedHook), "changed") {
		t.Fatalf("rendered hook = %q, want changed content", string(renderedHook))
	}

	loaded, err := state.LoadAppliedRevision("cluster-a")
	if err != nil {
		t.Fatalf("LoadAppliedRevision() error = %v", err)
	}
	if got, want := loaded.Status.State, state.StateDirty; got != want {
		t.Fatalf("loaded.status.state = %q, want %q", got, want)
	}
	if got, want := loaded.Spec.DesiredStateDigest, second.DesiredStateDigest; got != want {
		t.Fatalf("loaded.spec.desiredStateDigest = %q, want %q", got, want)
	}

	firstRevisionPath, err := RevisionBundlePath("cluster-a", first.DesiredStateDigest)
	if err != nil {
		t.Fatalf("RevisionBundlePath(first) error = %v", err)
	}
	if _, err := os.Stat(firstRevisionPath); err != nil {
		t.Fatalf("Stat(first revision bundle) error = %v", err)
	}
	secondRevisionPath, err := RevisionBundlePath("cluster-a", second.DesiredStateDigest)
	if err != nil {
		t.Fatalf("RevisionBundlePath(second) error = %v", err)
	}
	if _, err := os.Stat(secondRevisionPath); err != nil {
		t.Fatalf("Stat(second revision bundle) error = %v", err)
	}
}

func TestMaterializeSnapshotsClusterInventoryTopology(t *testing.T) {
	previousRuntimeRoot := constants.DefaultRuntimeRootDir
	constants.DefaultRuntimeRootDir = t.TempDir()
	t.Cleanup(func() {
		constants.DefaultRuntimeRootDir = previousRuntimeRoot
	})

	clusterName := "cluster-topology"
	writeClusterInventory(t, clusterName, []v1beta1.Host{
		{
			IPS:   []string{"10.0.0.10:22", "10.0.0.11:22"},
			Roles: []string{v1beta1.MASTER},
		},
		{
			IPS:   []string{"10.0.0.12:22"},
			Roles: []string{v1beta1.NODE},
		},
	})

	doc := testBOM()
	sourceRoot := fixtureRoot()
	result, err := Materialize(doc, Options{
		ClusterName:   clusterName,
		PackageLoader: loaderForDir(doc.Spec.Packages[0].Artifact.Reference(), sourceRoot),
		Sources:       hydrate.SourceMap{"kubernetes": sourceRoot},
	})
	if err != nil {
		t.Fatalf("Materialize() error = %v", err)
	}

	topology := result.Bundle.Spec.ExecutionTopology
	if got, want := topology.Source, "clusterInventory"; got != want {
		t.Fatalf("executionTopology.source = %q, want %q", got, want)
	}
	if got, want := strings.Join(topology.AllNodes, ","), "10.0.0.10:22,10.0.0.11:22,10.0.0.12:22"; got != want {
		t.Fatalf("executionTopology.allNodes = %q, want %q", got, want)
	}
	if got, want := topology.FirstMaster, "10.0.0.10:22"; got != want {
		t.Fatalf("executionTopology.firstMaster = %q, want %q", got, want)
	}
	if got, want := strings.Join(topology.RolesForHost("10.0.0.12:22"), ","), v1beta1.NODE; got != want {
		t.Fatalf("executionTopology worker roles = %q, want %q", got, want)
	}
}

func TestMaterializeOverlaysLocalRepoInput(t *testing.T) {
	previousRuntimeRoot := constants.DefaultRuntimeRootDir
	constants.DefaultRuntimeRootDir = t.TempDir()
	t.Cleanup(func() {
		constants.DefaultRuntimeRootDir = previousRuntimeRoot
	})

	doc := bom.New("minimal-single-node", "rev-poc-001", bom.ChannelAlpha)
	doc.Spec.Packages = []bom.Package{
		{
			Name:     "kubernetes",
			Category: "infra",
			Version:  "v1.30.3",
			Artifact: bom.ArtifactReference{
				Name:   "kubernetes-rootfs",
				Image:  "registry.example.io/sealos/kubernetes-rootfs:v1.30.3",
				Digest: "sha256:1111111111111111111111111111111111111111111111111111111111111111",
			},
		},
		{
			Name:     "cilium",
			Category: "addon",
			Version:  "v1.15.0",
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

	localRoot := t.TempDir()
	writeLocalRepoInput(t, localRoot, "cilium", "cilium-values.yaml", "hubble:\n  enabled: true\n")
	writeLocalRepoHostInput(t, localRoot, "cilium", "10.0.0.11", "cilium-values.yaml", "hubble:\n  enabled: false\n")
	writeLocalRepoResource(t, localRoot, filepath.Join("secrets", "grafana-admin-credentials.yaml"), "apiVersion: v1\nkind: Secret\nmetadata:\n  name: grafana-admin-credentials\n")
	writeLocalRepoPatchPolicy(t, localRoot, "apiVersion: distribution.sealos.io/v1alpha1\nkind: LocalPatchPolicy\nmetadata:\n  name: custom-local-patch-policy\nspec:\n  scope: clusterLocal\n  forbiddenExactPaths:\n    - status\n    - spec.selector\n  forbiddenMetadataKeys:\n    - uid\n    - resourceVersion\n    - generation\n    - creationTimestamp\n    - managedFields\n    - ownerReferences\n    - finalizers\n    - generateName\n    - selfLink\n    - deletionTimestamp\n    - deletionGracePeriodSeconds\n  forbiddenContainerFields:\n    - image\n  kindRules:\n    - kind: ConfigMap\n      allowedPrefixes:\n        - data\n        - metadata.annotations\n")
	writeLocalRepoPatch(t, localRoot, "cilium", "cilium-config.patch.yaml", "apiVersion: v1\nkind: ConfigMap\nmetadata:\n  name: cilium-config\n  namespace: kube-system\n  annotations:\n    local.sealos.io/managed: \"true\"\ndata:\n  enable-hubble: \"true\"\n")

	repo, err := localrepo.Load(localRoot)
	if err != nil {
		t.Fatalf("localrepo.Load() error = %v", err)
	}

	sourceRoot := filepath.Join("..", "..", "..", "scripts", "poc", "minimal-single-node", "packages", "cilium")
	result, err := Materialize(doc, Options{
		ClusterName: "cluster-a",
		LocalRepo:   repo,
		PackageLoader: packageformat.LoaderFunc(func(image string) (*packageformat.ComponentPackage, error) {
			switch image {
			case doc.Spec.Packages[0].Artifact.Reference():
				return packageformat.LoadDir(fixtureRoot())
			case doc.Spec.Packages[1].Artifact.Reference():
				return packageformat.LoadDir(sourceRoot)
			default:
				return nil, fmt.Errorf("unexpected image %q", image)
			}
		}),
		Sources: hydrate.SourceMap{
			"kubernetes": fixtureRoot(),
			"cilium":     sourceRoot,
		},
	})
	if err != nil {
		t.Fatalf("Materialize() error = %v", err)
	}

	renderedPath := filepath.Join(result.BundlePath, "components", "cilium", "files", "files", "values", "basic.yaml")
	data, err := os.ReadFile(renderedPath)
	if err != nil {
		t.Fatalf("ReadFile(%q) error = %v", renderedPath, err)
	}
	if !strings.Contains(string(data), "enabled: true") {
		t.Fatalf("rendered input file = %q, want local repo content", string(data))
	}

	if got, want := result.AppliedRevision.Spec.LocalRepoRevision, repo.Revision; got != want {
		t.Fatalf("spec.localRepoRevision = %q, want %q", got, want)
	}
	if got, want := result.Bundle.Spec.LocalPatchPolicyName, "custom-local-patch-policy"; got != want {
		t.Fatalf("bundle.spec.localPatchPolicyName = %q, want %q", got, want)
	}
	if got, want := string(result.Bundle.Spec.LocalPatchPolicySource), "localRepo"; got != want {
		t.Fatalf("bundle.spec.localPatchPolicySource = %q, want %q", got, want)
	}
	if got, want := string(result.Bundle.Spec.LocalPatchPolicyScope), "clusterLocal"; got != want {
		t.Fatalf("bundle.spec.localPatchPolicyScope = %q, want %q", got, want)
	}
	if got, want := result.Bundle.Spec.LocalPatchPolicyPath, "policy/local-patch-policy.yaml"; got != want {
		t.Fatalf("bundle.spec.localPatchPolicyPath = %q, want %q", got, want)
	}
	if got := result.Bundle.Spec.LocalPatchPolicyDigest; got == "" {
		t.Fatal("bundle.spec.localPatchPolicyDigest = empty, want sha256 digest")
	}
	if got, want := len(result.Bundle.Spec.LocalResources), 1; got != want {
		t.Fatalf("len(bundle.spec.localResources) = %d, want %d", got, want)
	}
	if _, err := os.Stat(filepath.Join(result.BundlePath, result.Bundle.Spec.LocalResources[0])); err != nil {
		t.Fatalf("Stat(local resource) error = %v", err)
	}
	ciliumComponent := findRenderedComponent(result.Bundle, "cilium")
	if ciliumComponent == nil {
		t.Fatal("bundle missing cilium component")
	}
	if got := ciliumComponent.InputBindings["cilium-values"]; got == "" {
		t.Fatal("bundle input binding missing for cilium-values")
	}
	if got, want := ciliumComponent.HostInputBindings["cilium-values"]["10.0.0.11"], "components/cilium/host-inputs/10.0.0.11/files/values/basic.yaml"; got != want {
		t.Fatalf("bundle host input binding = %q, want %q", got, want)
	}
	if _, err := os.Stat(filepath.Join(result.BundlePath, ciliumComponent.HostInputBindings["cilium-values"]["10.0.0.11"])); err != nil {
		t.Fatalf("Stat(rendered host input binding) error = %v", err)
	}
	if got, want := len(ciliumComponent.LocalPatches), 1; got != want {
		t.Fatalf("len(ciliumComponent.LocalPatches) = %d, want %d", got, want)
	}
	renderedManifestPath := filepath.Join(result.BundlePath, "components", "cilium", "files", "manifests", "cilium.yaml")
	renderedManifest, err := os.ReadFile(renderedManifestPath)
	if err != nil {
		t.Fatalf("ReadFile(%q) error = %v", renderedManifestPath, err)
	}
	if !strings.Contains(string(renderedManifest), "enable-hubble: \"true\"") {
		t.Fatalf("rendered manifest missing local patch value: %s", string(renderedManifest))
	}
	if !strings.Contains(string(renderedManifest), "local.sealos.io/managed: \"true\"") {
		t.Fatalf("rendered manifest missing local patch annotation: %s", string(renderedManifest))
	}

	var sawLocalPatch bool
	for _, object := range result.Bundle.Spec.TrackedK8sObjects {
		if object.Kind == "ConfigMap" && object.Component == "cilium" && object.Source == hydrate.InventorySourceLocalPatch {
			sawLocalPatch = true
			break
		}
	}
	if !sawLocalPatch {
		t.Fatalf("tracked objects missing local patch entry: %+v", result.Bundle.Spec.TrackedK8sObjects)
	}
}

func TestMaterializeUsesPackageLocalPatchPolicyWhenLocalRepoPolicyMissing(t *testing.T) {
	previousRuntimeRoot := constants.DefaultRuntimeRootDir
	constants.DefaultRuntimeRootDir = t.TempDir()
	t.Cleanup(func() {
		constants.DefaultRuntimeRootDir = previousRuntimeRoot
	})

	doc := bom.New("minimal-single-node", "rev-poc-001", bom.ChannelAlpha)
	doc.Spec.Packages = []bom.Package{
		{
			Name:     "kubernetes",
			Category: "infra",
			Version:  "v1.30.3",
			Artifact: bom.ArtifactReference{
				Name:   "kubernetes-rootfs",
				Image:  "registry.example.io/sealos/kubernetes-rootfs:v1.30.3",
				Digest: "sha256:1111111111111111111111111111111111111111111111111111111111111111",
			},
		},
		{
			Name:     "cilium",
			Category: "addon",
			Version:  "v1.15.0",
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

	localRoot := t.TempDir()
	writeLocalRepoPatch(t, localRoot, "cilium", "cilium-config.patch.yaml", "apiVersion: v1\nkind: ConfigMap\nmetadata:\n  name: cilium-config\n  namespace: kube-system\n  annotations:\n    package-policy.sealos.io/managed: \"true\"\n")
	repo, err := localrepo.Load(localRoot)
	if err != nil {
		t.Fatalf("localrepo.Load() error = %v", err)
	}

	sourceRoot := filepath.Join("..", "..", "..", "scripts", "poc", "minimal-single-node", "packages", "cilium")
	ciliumSource := copiedPackageSource(t, sourceRoot)
	writePackagePatchPolicy(t, ciliumSource, "apiVersion: distribution.sealos.io/v1alpha1\nkind: LocalPatchPolicy\nmetadata:\n  name: package-local-patch-policy\nspec:\n  scope: clusterLocal\n  forbiddenExactPaths:\n    - status\n    - spec.selector\n  forbiddenMetadataKeys:\n    - uid\n    - resourceVersion\n    - generation\n    - creationTimestamp\n    - managedFields\n    - ownerReferences\n    - finalizers\n    - generateName\n    - selfLink\n    - deletionTimestamp\n    - deletionGracePeriodSeconds\n  forbiddenContainerFields:\n    - image\n  kindRules:\n    - kind: ConfigMap\n      allowedPrefixes:\n        - metadata.annotations\n")

	result, err := Materialize(doc, Options{
		ClusterName: "cluster-a",
		LocalRepo:   repo,
		PackageLoader: packageformat.LoaderFunc(func(image string) (*packageformat.ComponentPackage, error) {
			switch image {
			case doc.Spec.Packages[0].Artifact.Reference():
				return packageformat.LoadDir(fixtureRoot())
			case doc.Spec.Packages[1].Artifact.Reference():
				pkg, err := packageformat.LoadDir(ciliumSource)
				if err != nil {
					return nil, err
				}
				pkg.Spec.LocalPatchPolicy = "policy/local-patch-policy.yaml"
				return pkg, nil
			default:
				return nil, fmt.Errorf("unexpected image %q", image)
			}
		}),
		Sources: hydrate.SourceMap{
			"kubernetes": fixtureRoot(),
			"cilium":     ciliumSource,
		},
	})
	if err != nil {
		t.Fatalf("Materialize() error = %v", err)
	}

	if got, want := string(result.Bundle.Spec.LocalPatchPolicySource), "package"; got != want {
		t.Fatalf("bundle.spec.localPatchPolicySource = %q, want %q", got, want)
	}
	if got, want := result.Bundle.Spec.LocalPatchPolicyName, "package-local-patch-policy"; got != want {
		t.Fatalf("bundle.spec.localPatchPolicyName = %q, want %q", got, want)
	}
	renderedManifestPath := filepath.Join(result.BundlePath, "components", "cilium", "files", "manifests", "cilium.yaml")
	renderedManifest, err := os.ReadFile(renderedManifestPath)
	if err != nil {
		t.Fatalf("ReadFile(%q) error = %v", renderedManifestPath, err)
	}
	if !strings.Contains(string(renderedManifest), "package-policy.sealos.io/managed: \"true\"") {
		t.Fatalf("rendered manifest missing package-policy local patch annotation: %s", string(renderedManifest))
	}
}

func TestMaterializeLocalRepoPolicyOverridesPackagePolicy(t *testing.T) {
	previousRuntimeRoot := constants.DefaultRuntimeRootDir
	constants.DefaultRuntimeRootDir = t.TempDir()
	t.Cleanup(func() {
		constants.DefaultRuntimeRootDir = previousRuntimeRoot
	})

	doc := testBOM()
	sourceRoot := copiedFixture(t)
	writePackagePatchPolicy(t, sourceRoot, "apiVersion: distribution.sealos.io/v1alpha1\nkind: LocalPatchPolicy\nmetadata:\n  name: package-local-patch-policy\nspec:\n  scope: clusterLocal\n  forbiddenExactPaths:\n    - status\n    - spec.selector\n  forbiddenMetadataKeys:\n    - uid\n    - resourceVersion\n    - generation\n    - creationTimestamp\n    - managedFields\n    - ownerReferences\n    - finalizers\n    - generateName\n    - selfLink\n    - deletionTimestamp\n    - deletionGracePeriodSeconds\n  forbiddenContainerFields:\n    - image\n  kindRules:\n    - kind: ConfigMap\n      allowedPrefixes:\n        - metadata.annotations\n")

	localRoot := t.TempDir()
	writeLocalRepoPatchPolicy(t, localRoot, "apiVersion: distribution.sealos.io/v1alpha1\nkind: LocalPatchPolicy\nmetadata:\n  name: repo-local-patch-policy\nspec:\n  scope: clusterLocal\n  forbiddenExactPaths:\n    - status\n    - spec.selector\n  forbiddenMetadataKeys:\n    - uid\n    - resourceVersion\n    - generation\n    - creationTimestamp\n    - managedFields\n    - ownerReferences\n    - finalizers\n    - generateName\n    - selfLink\n    - deletionTimestamp\n    - deletionGracePeriodSeconds\n  forbiddenContainerFields:\n    - image\n  kindRules:\n    - kind: ConfigMap\n      allowedPrefixes:\n        - data\n")
	repo, err := localrepo.Load(localRoot)
	if err != nil {
		t.Fatalf("localrepo.Load() error = %v", err)
	}

	result, err := Materialize(doc, Options{
		ClusterName: "cluster-a",
		LocalRepo:   repo,
		PackageLoader: packageformat.LoaderFunc(func(image string) (*packageformat.ComponentPackage, error) {
			pkg, err := loaderForDir(doc.Spec.Packages[0].Artifact.Reference(), sourceRoot).Load(image)
			if err != nil {
				return nil, err
			}
			pkg.Spec.LocalPatchPolicy = "policy/local-patch-policy.yaml"
			return pkg, nil
		}),
		Sources: hydrate.SourceMap{"kubernetes": sourceRoot},
	})
	if err != nil {
		t.Fatalf("Materialize() error = %v", err)
	}
	if got, want := string(result.Bundle.Spec.LocalPatchPolicySource), "localRepo"; got != want {
		t.Fatalf("bundle.spec.localPatchPolicySource = %q, want %q", got, want)
	}
	if got, want := result.Bundle.Spec.LocalPatchPolicyName, "repo-local-patch-policy"; got != want {
		t.Fatalf("bundle.spec.localPatchPolicyName = %q, want %q", got, want)
	}
}

func TestMaterializeUsesBOMLocalPatchPolicy(t *testing.T) {
	previousRuntimeRoot := constants.DefaultRuntimeRootDir
	constants.DefaultRuntimeRootDir = t.TempDir()
	t.Cleanup(func() {
		constants.DefaultRuntimeRootDir = previousRuntimeRoot
	})

	doc := testBOM()
	doc.Spec.LocalPatchPolicy = "policy/local-patch-policy.yaml"
	bomRoot := t.TempDir()
	writePackagePatchPolicy(t, bomRoot, packagePolicyYAML("bom-local-patch-policy"))
	sourceRoot := copiedFixture(t)
	writePackagePatchPolicy(t, sourceRoot, packagePolicyYAML("package-local-patch-policy"))

	result, err := Materialize(doc, Options{
		ClusterName: "cluster-a",
		BOMRoot:     bomRoot,
		PackageLoader: packageformat.LoaderFunc(func(image string) (*packageformat.ComponentPackage, error) {
			pkg, err := loaderForDir(doc.Spec.Packages[0].Artifact.Reference(), sourceRoot).Load(image)
			if err != nil {
				return nil, err
			}
			pkg.Spec.LocalPatchPolicy = "policy/local-patch-policy.yaml"
			return pkg, nil
		}),
		Sources: hydrate.SourceMap{"kubernetes": sourceRoot},
	})
	if err != nil {
		t.Fatalf("Materialize() error = %v", err)
	}
	if got, want := string(result.Bundle.Spec.LocalPatchPolicySource), "bom"; got != want {
		t.Fatalf("bundle.spec.localPatchPolicySource = %q, want %q", got, want)
	}
	if got, want := result.Bundle.Spec.LocalPatchPolicyName, "bom-local-patch-policy"; got != want {
		t.Fatalf("bundle.spec.localPatchPolicyName = %q, want %q", got, want)
	}
}

func TestMaterializeRejectsMultiplePackageLocalPatchPolicies(t *testing.T) {
	previousRuntimeRoot := constants.DefaultRuntimeRootDir
	constants.DefaultRuntimeRootDir = t.TempDir()
	t.Cleanup(func() {
		constants.DefaultRuntimeRootDir = previousRuntimeRoot
	})

	doc := bom.New("minimal-single-node", "rev-poc-001", bom.ChannelAlpha)
	doc.Spec.Packages = []bom.Package{
		{
			Name:     "kubernetes",
			Category: "infra",
			Version:  "v1.30.3",
			Artifact: bom.ArtifactReference{
				Name:   "kubernetes-rootfs",
				Image:  "registry.example.io/sealos/kubernetes-rootfs:v1.30.3",
				Digest: "sha256:1111111111111111111111111111111111111111111111111111111111111111",
			},
		},
		{
			Name:     "other",
			Category: "infra",
			Version:  "v1.30.3",
			Artifact: bom.ArtifactReference{
				Name:   "other-rootfs",
				Image:  "registry.example.io/sealos/other-rootfs:v1.30.3",
				Digest: "sha256:2222222222222222222222222222222222222222222222222222222222222222",
			},
		},
	}
	sourceA := copiedFixture(t)
	sourceB := copiedFixture(t)
	writePackagePatchPolicy(t, sourceA, packagePolicyYAML("package-policy-a"))
	writePackagePatchPolicy(t, sourceB, packagePolicyYAML("package-policy-b"))

	_, err := Materialize(doc, Options{
		ClusterName: "cluster-a",
		PackageLoader: packageformat.LoaderFunc(func(image string) (*packageformat.ComponentPackage, error) {
			var root string
			componentName := ""
			switch image {
			case doc.Spec.Packages[0].Artifact.Reference():
				root = sourceA
				componentName = "kubernetes"
			case doc.Spec.Packages[1].Artifact.Reference():
				root = sourceB
				componentName = "other"
			default:
				return nil, fmt.Errorf("unexpected image %q", image)
			}
			pkg, err := packageformat.LoadDir(root)
			if err != nil {
				return nil, err
			}
			pkg.Spec.Component = componentName
			pkg.Spec.LocalPatchPolicy = "policy/local-patch-policy.yaml"
			return pkg, nil
		}),
		Sources: hydrate.SourceMap{
			"kubernetes": sourceA,
			"other":      sourceB,
		},
	})
	if err == nil {
		t.Fatal("Materialize() error = nil, want multiple package policy error")
	}
	if !strings.Contains(err.Error(), "multiple component packages declare local patch policies") {
		t.Fatalf("Materialize() error = %v, want multiple package policy error", err)
	}
}

func TestMaterializeBOMPolicyOverridesMultiplePackagePolicies(t *testing.T) {
	previousRuntimeRoot := constants.DefaultRuntimeRootDir
	constants.DefaultRuntimeRootDir = t.TempDir()
	t.Cleanup(func() {
		constants.DefaultRuntimeRootDir = previousRuntimeRoot
	})

	doc := bom.New("minimal-single-node", "rev-poc-001", bom.ChannelAlpha)
	doc.Spec.LocalPatchPolicy = "policy/local-patch-policy.yaml"
	doc.Spec.Packages = []bom.Package{
		{
			Name:     "kubernetes",
			Category: "infra",
			Version:  "v1.30.3",
			Artifact: bom.ArtifactReference{
				Name:   "kubernetes-rootfs",
				Image:  "registry.example.io/sealos/kubernetes-rootfs:v1.30.3",
				Digest: "sha256:1111111111111111111111111111111111111111111111111111111111111111",
			},
		},
		{
			Name:     "other",
			Category: "infra",
			Version:  "v1.30.3",
			Artifact: bom.ArtifactReference{
				Name:   "other-rootfs",
				Image:  "registry.example.io/sealos/other-rootfs:v1.30.3",
				Digest: "sha256:2222222222222222222222222222222222222222222222222222222222222222",
			},
		},
	}
	bomRoot := t.TempDir()
	writePackagePatchPolicy(t, bomRoot, packagePolicyYAML("bom-local-patch-policy"))
	sourceA := copiedFixture(t)
	sourceB := copiedFixture(t)
	writePackagePatchPolicy(t, sourceA, packagePolicyYAML("package-policy-a"))
	writePackagePatchPolicy(t, sourceB, packagePolicyYAML("package-policy-b"))

	result, err := Materialize(doc, Options{
		ClusterName: "cluster-a",
		BOMRoot:     bomRoot,
		PackageLoader: packageformat.LoaderFunc(func(image string) (*packageformat.ComponentPackage, error) {
			var root string
			componentName := ""
			switch image {
			case doc.Spec.Packages[0].Artifact.Reference():
				root = sourceA
				componentName = "kubernetes"
			case doc.Spec.Packages[1].Artifact.Reference():
				root = sourceB
				componentName = "other"
			default:
				return nil, fmt.Errorf("unexpected image %q", image)
			}
			pkg, err := packageformat.LoadDir(root)
			if err != nil {
				return nil, err
			}
			pkg.Spec.Component = componentName
			pkg.Spec.LocalPatchPolicy = "policy/local-patch-policy.yaml"
			return pkg, nil
		}),
		Sources: hydrate.SourceMap{
			"kubernetes": sourceA,
			"other":      sourceB,
		},
	})
	if err != nil {
		t.Fatalf("Materialize() error = %v", err)
	}
	if got, want := string(result.Bundle.Spec.LocalPatchPolicySource), "bom"; got != want {
		t.Fatalf("bundle.spec.localPatchPolicySource = %q, want %q", got, want)
	}
	if got, want := result.Bundle.Spec.LocalPatchPolicyName, "bom-local-patch-policy"; got != want {
		t.Fatalf("bundle.spec.localPatchPolicyName = %q, want %q", got, want)
	}
}

func TestMaterializeLocalRepoPolicyOverridesMultiplePackagePolicies(t *testing.T) {
	previousRuntimeRoot := constants.DefaultRuntimeRootDir
	constants.DefaultRuntimeRootDir = t.TempDir()
	t.Cleanup(func() {
		constants.DefaultRuntimeRootDir = previousRuntimeRoot
	})

	doc := bom.New("minimal-single-node", "rev-poc-001", bom.ChannelAlpha)
	doc.Spec.Packages = []bom.Package{
		{
			Name:     "kubernetes",
			Category: "infra",
			Version:  "v1.30.3",
			Artifact: bom.ArtifactReference{
				Name:   "kubernetes-rootfs",
				Image:  "registry.example.io/sealos/kubernetes-rootfs:v1.30.3",
				Digest: "sha256:1111111111111111111111111111111111111111111111111111111111111111",
			},
		},
		{
			Name:     "other",
			Category: "infra",
			Version:  "v1.30.3",
			Artifact: bom.ArtifactReference{
				Name:   "other-rootfs",
				Image:  "registry.example.io/sealos/other-rootfs:v1.30.3",
				Digest: "sha256:2222222222222222222222222222222222222222222222222222222222222222",
			},
		},
	}
	localRoot := t.TempDir()
	writeLocalRepoPatchPolicy(t, localRoot, packagePolicyYAML("repo-local-patch-policy"))
	repo, err := localrepo.Load(localRoot)
	if err != nil {
		t.Fatalf("localrepo.Load() error = %v", err)
	}
	sourceA := copiedFixture(t)
	sourceB := copiedFixture(t)
	writePackagePatchPolicy(t, sourceA, packagePolicyYAML("package-policy-a"))
	writePackagePatchPolicy(t, sourceB, packagePolicyYAML("package-policy-b"))

	result, err := Materialize(doc, Options{
		ClusterName: "cluster-a",
		LocalRepo:   repo,
		PackageLoader: packageformat.LoaderFunc(func(image string) (*packageformat.ComponentPackage, error) {
			var root string
			componentName := ""
			switch image {
			case doc.Spec.Packages[0].Artifact.Reference():
				root = sourceA
				componentName = "kubernetes"
			case doc.Spec.Packages[1].Artifact.Reference():
				root = sourceB
				componentName = "other"
			default:
				return nil, fmt.Errorf("unexpected image %q", image)
			}
			pkg, err := packageformat.LoadDir(root)
			if err != nil {
				return nil, err
			}
			pkg.Spec.Component = componentName
			pkg.Spec.LocalPatchPolicy = "policy/local-patch-policy.yaml"
			return pkg, nil
		}),
		Sources: hydrate.SourceMap{
			"kubernetes": sourceA,
			"other":      sourceB,
		},
	})
	if err != nil {
		t.Fatalf("Materialize() error = %v", err)
	}
	if got, want := string(result.Bundle.Spec.LocalPatchPolicySource), "localRepo"; got != want {
		t.Fatalf("bundle.spec.localPatchPolicySource = %q, want %q", got, want)
	}
	if got, want := result.Bundle.Spec.LocalPatchPolicyName, "repo-local-patch-policy"; got != want {
		t.Fatalf("bundle.spec.localPatchPolicyName = %q, want %q", got, want)
	}
}

func TestMaterializeRejectsInvalidLocalRepoPatch(t *testing.T) {
	previousRuntimeRoot := constants.DefaultRuntimeRootDir
	constants.DefaultRuntimeRootDir = t.TempDir()
	t.Cleanup(func() {
		constants.DefaultRuntimeRootDir = previousRuntimeRoot
	})

	doc := bom.New("minimal-single-node", "rev-poc-001", bom.ChannelAlpha)
	doc.Spec.Packages = []bom.Package{
		{
			Name:     "kubernetes",
			Category: "infra",
			Version:  "v1.30.3",
			Artifact: bom.ArtifactReference{
				Name:   "kubernetes-rootfs",
				Image:  "registry.example.io/sealos/kubernetes-rootfs:v1.30.3",
				Digest: "sha256:1111111111111111111111111111111111111111111111111111111111111111",
			},
		},
		{
			Name:     "cilium",
			Category: "addon",
			Version:  "v1.15.0",
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

	localRoot := t.TempDir()
	writeLocalRepoPatch(t, localRoot, "cilium", "cilium-config.patch.yaml", "apiVersion: v1\nkind: ConfigMap\nmetadata:\n  name: cilium-config\n  namespace: kube-system\n  resourceVersion: \"1\"\ndata:\n  enable-hubble: \"true\"\n")
	repo, err := localrepo.Load(localRoot)
	if err != nil {
		t.Fatalf("localrepo.Load() error = %v", err)
	}

	sourceRoot := filepath.Join("..", "..", "..", "scripts", "poc", "minimal-single-node", "packages", "cilium")
	_, err = Materialize(doc, Options{
		ClusterName: "cluster-a",
		LocalRepo:   repo,
		PackageLoader: packageformat.LoaderFunc(func(image string) (*packageformat.ComponentPackage, error) {
			switch image {
			case doc.Spec.Packages[0].Artifact.Reference():
				return packageformat.LoadDir(fixtureRoot())
			case doc.Spec.Packages[1].Artifact.Reference():
				return packageformat.LoadDir(sourceRoot)
			default:
				return nil, fmt.Errorf("unexpected image %q", image)
			}
		}),
		Sources: hydrate.SourceMap{
			"kubernetes": fixtureRoot(),
			"cilium":     sourceRoot,
		},
	})
	if err == nil {
		t.Fatal("Materialize() error = nil, want error")
	}
	if !strings.Contains(err.Error(), "local patch metadata key") && !strings.Contains(err.Error(), "local patch cannot modify") {
		t.Fatalf("Materialize() error = %v, want ownership validation failure", err)
	}
}

type trackedSourceProvider struct {
	root   string
	closed int
}

func (p *trackedSourceProvider) Source(component hydrate.ComponentPlan) (hydrate.Source, error) {
	if component.Name != "kubernetes" {
		return hydrate.Source{}, fmt.Errorf("unexpected component %q", component.Name)
	}
	return hydrate.Source{Root: p.root}, nil
}

func (p *trackedSourceProvider) Close() error {
	p.closed++
	return nil
}

func testBOM() *bom.BOM {
	doc := bom.New("default-platform", "rev-20240423", bom.ChannelBeta)
	doc.Spec.Packages = []bom.Package{
		{
			Name:     "kubernetes",
			Category: "infra",
			Version:  "v1.30.3",
			Artifact: bom.ArtifactReference{
				Name:   "kubernetes-rootfs",
				Image:  "registry.example.io/sealos/kubernetes-rootfs:v1.30.3",
				Digest: "sha256:1111111111111111111111111111111111111111111111111111111111111111",
			},
		},
	}
	return doc
}

func loaderForDir(expectedRef, root string) packageformat.Loader {
	return packageformat.LoaderFunc(func(image string) (*packageformat.ComponentPackage, error) {
		if image != expectedRef {
			return nil, fmt.Errorf("unexpected image %q", image)
		}
		return packageformat.LoadDir(root)
	})
}

func fixtureRoot() string {
	return filepath.Join("..", "packageformat", "testdata", "kubernetes-rootfs")
}

func copiedFixture(t *testing.T) string {
	t.Helper()

	dst := filepath.Join(t.TempDir(), "kubernetes-rootfs")
	if err := fileutil.CopyDirV3(fixtureRoot(), dst); err != nil {
		t.Fatalf("CopyDirV3() error = %v", err)
	}
	return dst
}

func copiedPackageSource(t *testing.T, root string) string {
	t.Helper()

	dst := filepath.Join(t.TempDir(), filepath.Base(root))
	if err := fileutil.CopyDirV3(root, dst); err != nil {
		t.Fatalf("CopyDirV3(%q, %q) error = %v", root, dst, err)
	}
	return dst
}

func writePackagePatchPolicy(t *testing.T, root, content string) {
	t.Helper()

	path := filepath.Join(root, "policy", "local-patch-policy.yaml")
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("MkdirAll(%q) error = %v", filepath.Dir(path), err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("WriteFile(%q) error = %v", path, err)
	}
}

func packagePolicyYAML(name string) string {
	return "apiVersion: distribution.sealos.io/v1alpha1\nkind: LocalPatchPolicy\nmetadata:\n  name: " + name + "\nspec:\n  scope: clusterLocal\n  forbiddenExactPaths:\n    - status\n    - spec.selector\n  forbiddenMetadataKeys:\n    - uid\n    - resourceVersion\n    - generation\n    - creationTimestamp\n    - managedFields\n    - ownerReferences\n    - finalizers\n    - generateName\n    - selfLink\n    - deletionTimestamp\n    - deletionGracePeriodSeconds\n  forbiddenContainerFields:\n    - image\n  kindRules:\n    - kind: ConfigMap\n      allowedPrefixes:\n        - data\n"
}

func writeLocalRepoInput(t *testing.T, root, component, filename, content string) {
	t.Helper()

	path := filepath.Join(root, "inputs", component, filename)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("MkdirAll(%q) error = %v", filepath.Dir(path), err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("WriteFile(%q) error = %v", path, err)
	}
}

func writeLocalRepoHostInput(t *testing.T, root, component, host, filename, content string) {
	t.Helper()

	path := filepath.Join(root, "inputs", component, "hosts", host, filename)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("MkdirAll(%q) error = %v", filepath.Dir(path), err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("WriteFile(%q) error = %v", path, err)
	}
}

func writeLocalRepoResource(t *testing.T, root, relativePath, content string) {
	t.Helper()

	path := filepath.Join(root, "resources", relativePath)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("MkdirAll(%q) error = %v", filepath.Dir(path), err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("WriteFile(%q) error = %v", path, err)
	}
}

func writeLocalRepoPatch(t *testing.T, root, component, relativePath, content string) {
	t.Helper()

	path := filepath.Join(root, localrepo.PatchesDirName, component, filepath.FromSlash(relativePath))
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("MkdirAll(%q) error = %v", filepath.Dir(path), err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("WriteFile(%q) error = %v", path, err)
	}
}

func writeLocalRepoPatchPolicy(t *testing.T, root, content string) {
	t.Helper()

	path := filepath.Join(root, localrepo.PolicyDirName, "local-patch-policy.yaml")
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("MkdirAll(%q) error = %v", filepath.Dir(path), err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("WriteFile(%q) error = %v", path, err)
	}
}

func findRenderedComponent(bundle *hydrate.Bundle, name string) *hydrate.RenderedComponent {
	if bundle == nil {
		return nil
	}
	for i := range bundle.Spec.Components {
		if bundle.Spec.Components[i].Name == name {
			return &bundle.Spec.Components[i]
		}
	}
	return nil
}
