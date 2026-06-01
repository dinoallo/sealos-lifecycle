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
	"testing"

	"github.com/labring/sealos/pkg/distribution"
	"github.com/labring/sealos/pkg/distribution/bom"
	"github.com/labring/sealos/pkg/distribution/ownership"
	"github.com/labring/sealos/pkg/distribution/packageformat"
	yamlutil "github.com/labring/sealos/pkg/utils/yaml"
)

func TestRenderPlan(t *testing.T) {
	t.Parallel()

	root := filepath.Join("..", "packageformat", "testdata", "kubernetes-rootfs")
	pkg, err := packageformat.LoadDir(root)
	if err != nil {
		t.Fatalf("LoadDir() error = %v", err)
	}

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

	plan, err := BuildPlanFromResolved(doc, map[string]*packageformat.ComponentPackage{
		"kubernetes": pkg,
	})
	if err != nil {
		t.Fatalf("BuildPlanFromResolved() error = %v", err)
	}

	out := t.TempDir()
	rendered, err := RenderPlan(plan, SourceMap{"kubernetes": root}, out)
	if err != nil {
		t.Fatalf("RenderPlan() error = %v", err)
	}

	if got, want := rendered.Kind, distribution.KindHydratedBundle; got != want {
		t.Fatalf("bundle kind = %q, want %q", got, want)
	}
	if got, want := len(rendered.Spec.Components), 1; got != want {
		t.Fatalf("len(rendered.Spec.Components) = %d, want %d", got, want)
	}

	component := rendered.Spec.Components[0]
	if got, want := component.ManifestPath, "components/kubernetes/package.yaml"; got != want {
		t.Fatalf("component manifestPath = %q, want %q", got, want)
	}
	if got, want := component.RootPath, "components/kubernetes/files"; got != want {
		t.Fatalf("component rootPath = %q, want %q", got, want)
	}
	if got, want := len(component.Steps), 4; got != want {
		t.Fatalf("len(component.Steps) = %d, want %d", got, want)
	}
	if got, want := component.Steps[2].BundlePath, "components/kubernetes/files/hooks/bootstrap.sh"; got != want {
		t.Fatalf("component.Steps[2].BundlePath = %q, want %q", got, want)
	}
	if got, want := component.Steps[3].BundlePath, "components/kubernetes/files/hooks/bootstrap.sh"; got != want {
		t.Fatalf("component.Steps[3].BundlePath = %q, want %q", got, want)
	}

	for _, rel := range []string{
		"bundle.yaml",
		"policy/local-patch-policy.yaml",
		"components/kubernetes/package.yaml",
		"components/kubernetes/files/rootfs/README",
		"components/kubernetes/files/manifests/bootstrap.yaml",
		"components/kubernetes/files/hooks/bootstrap.sh",
	} {
		if !exists(filepath.Join(out, rel)) {
			t.Fatalf("expected rendered file %q to exist", rel)
		}
	}

	var onDisk Bundle
	if err := yamlutil.UnmarshalFile(filepath.Join(out, BundleFileName), &onDisk); err != nil {
		t.Fatalf("UnmarshalFile() error = %v", err)
	}
	if got, want := onDisk.Spec.Revision, plan.Revision; got != want {
		t.Fatalf("bundle revision = %q, want %q", got, want)
	}
	if got, want := onDisk.Spec.LocalPatchPolicyName, "defaultLocalPatchPolicy"; got != want {
		t.Fatalf("bundle localPatchPolicyName = %q, want %q", got, want)
	}
	if got, want := string(onDisk.Spec.LocalPatchPolicySource), "builtInDefault"; got != want {
		t.Fatalf("bundle localPatchPolicySource = %q, want %q", got, want)
	}
	if got, want := string(onDisk.Spec.LocalPatchPolicyScope), "clusterLocal"; got != want {
		t.Fatalf("bundle localPatchPolicyScope = %q, want %q", got, want)
	}
	if got, want := onDisk.Spec.LocalPatchPolicyPath, "policy/local-patch-policy.yaml"; got != want {
		t.Fatalf("bundle localPatchPolicyPath = %q, want %q", got, want)
	}
	if got := onDisk.Spec.LocalPatchPolicyDigest; got == "" {
		t.Fatal("bundle localPatchPolicyDigest = empty, want sha256 digest")
	}
}

func TestRenderPlanWithMountedArtifactSourceProvider(t *testing.T) {
	t.Parallel()

	root := filepath.Join("..", "packageformat", "testdata", "kubernetes-rootfs")
	pkg, err := packageformat.LoadDir(root)
	if err != nil {
		t.Fatalf("LoadDir() error = %v", err)
	}

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

	plan, err := BuildPlanFromResolved(doc, map[string]*packageformat.ComponentPackage{
		"kubernetes": pkg,
	})
	if err != nil {
		t.Fatalf("BuildPlanFromResolved() error = %v", err)
	}

	artifactRef := doc.Spec.Packages[0].Artifact.Reference()
	fake := &fakeRenderMounter{
		mounts: map[string]packageformat.MountedImage{
			artifactRef: {
				Name:       "mounted-kubernetes",
				MountPoint: root,
			},
		},
	}
	provider := NewMountedArtifactSourceProvider(fake)
	t.Cleanup(func() {
		if err := provider.Close(); err != nil {
			t.Fatalf("Close() error = %v", err)
		}
	})

	out := t.TempDir()
	rendered, err := RenderPlan(plan, provider, out)
	if err != nil {
		t.Fatalf("RenderPlan() error = %v", err)
	}

	if got, want := len(fake.mounted), 1; got != want {
		t.Fatalf("len(fake.mounted) = %d, want %d", got, want)
	}
	if got, want := fake.mounted[0], artifactRef; got != want {
		t.Fatalf("fake.mounted[0] = %q, want %q", got, want)
	}
	if got, want := rendered.Spec.Components[0].Artifact, artifactRef; got != want {
		t.Fatalf("component artifact = %q, want %q", got, want)
	}

	for _, rel := range []string{
		"bundle.yaml",
		"policy/local-patch-policy.yaml",
		"components/kubernetes/package.yaml",
		"components/kubernetes/files/rootfs/README",
		"components/kubernetes/files/manifests/bootstrap.yaml",
		"components/kubernetes/files/hooks/bootstrap.sh",
	} {
		if !exists(filepath.Join(out, rel)) {
			t.Fatalf("expected rendered file %q to exist", rel)
		}
	}
}

func TestRenderPlanUsesPackageLocalPatchPolicy(t *testing.T) {
	t.Parallel()

	root := copyRenderFixture(t)
	policyPath := filepath.Join(root, "policy", ownership.LocalPatchPolicyFileName)
	if err := os.MkdirAll(filepath.Dir(policyPath), 0o755); err != nil {
		t.Fatalf("MkdirAll(policy dir) error = %v", err)
	}
	if err := os.WriteFile(policyPath, []byte("apiVersion: distribution.sealos.io/v1alpha1\nkind: LocalPatchPolicy\nmetadata:\n  name: package-local-patch-policy\nspec:\n  scope: clusterLocal\n  forbiddenExactPaths:\n    - status\n    - spec.selector\n  forbiddenMetadataKeys:\n    - uid\n    - resourceVersion\n    - generation\n    - creationTimestamp\n    - managedFields\n    - ownerReferences\n    - finalizers\n    - generateName\n    - selfLink\n    - deletionTimestamp\n    - deletionGracePeriodSeconds\n  forbiddenContainerFields:\n    - image\n  kindRules:\n    - kind: ConfigMap\n      allowedPrefixes:\n        - metadata.annotations\n"), 0o644); err != nil {
		t.Fatalf("WriteFile(policy) error = %v", err)
	}

	pkg, err := packageformat.LoadDir(root)
	if err != nil {
		t.Fatalf("LoadDir() error = %v", err)
	}
	pkg.Spec.LocalPatchPolicy = "policy/local-patch-policy.yaml"

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
	plan, err := BuildPlanFromResolved(doc, map[string]*packageformat.ComponentPackage{"kubernetes": pkg})
	if err != nil {
		t.Fatalf("BuildPlanFromResolved() error = %v", err)
	}

	out := t.TempDir()
	rendered, err := RenderPlan(plan, SourceMap{"kubernetes": root}, out)
	if err != nil {
		t.Fatalf("RenderPlan() error = %v", err)
	}

	if got, want := rendered.Spec.LocalPatchPolicySource, ownership.LocalPatchPolicySourcePackage; got != want {
		t.Fatalf("localPatchPolicySource = %q, want %q", got, want)
	}
	if got, want := rendered.Spec.LocalPatchPolicyName, "package-local-patch-policy"; got != want {
		t.Fatalf("localPatchPolicyName = %q, want %q", got, want)
	}
	loaded, err := LoadBundleLocalPatchPolicy(rendered, out)
	if err != nil {
		t.Fatalf("LoadBundleLocalPatchPolicy() error = %v", err)
	}
	if got, want := loaded.EffectiveName(), "package-local-patch-policy"; got != want {
		t.Fatalf("loaded policy name = %q, want %q", got, want)
	}
}

func TestRenderPlanMissingSource(t *testing.T) {
	t.Parallel()

	plan := &Plan{
		BOMName:  "default-platform",
		Revision: "rev-20240423",
		Channel:  bom.ChannelBeta,
		Components: []ComponentPlan{
			{
				Name: "kubernetes",
			},
		},
	}

	if _, err := RenderPlan(plan, SourceMap{}, t.TempDir()); err == nil {
		t.Fatal("RenderPlan() error = nil, want error")
	}
}

func copyRenderFixture(t *testing.T) string {
	t.Helper()

	root := filepath.Join("..", "packageformat", "testdata", "kubernetes-rootfs")
	dst := filepath.Join(t.TempDir(), "kubernetes-rootfs")
	if err := copyEntry(root, dst); err != nil {
		t.Fatalf("copyEntry(%q, %q) error = %v", root, dst, err)
	}
	return dst
}

func TestRenderPlanPreservesHostInputBindings(t *testing.T) {
	t.Parallel()

	localRepoRoot := t.TempDir()
	defaultInputPath := filepath.Join(localRepoRoot, "inputs", "kubernetes", "kubeadm-config.yaml")
	hostInputPath := filepath.Join(localRepoRoot, "inputs", "kubernetes", "hosts", "10.0.0.11", "kubeadm-config.yaml")
	for _, item := range []struct {
		path    string
		content string
	}{
		{path: defaultInputPath, content: "apiVersion: kubeadm.k8s.io/v1beta4\nkind: InitConfiguration\n"},
		{path: hostInputPath, content: "apiVersion: kubeadm.k8s.io/v1beta4\nkind: JoinConfiguration\n"},
	} {
		if err := os.MkdirAll(filepath.Dir(item.path), 0o755); err != nil {
			t.Fatalf("MkdirAll(%q) error = %v", filepath.Dir(item.path), err)
		}
		if err := os.WriteFile(item.path, []byte(item.content), 0o644); err != nil {
			t.Fatalf("WriteFile(%q) error = %v", item.path, err)
		}
	}

	plan := &Plan{
		BOMName:  "default-platform",
		Revision: "rev-20240423",
		Channel:  bom.ChannelBeta,
		Components: []ComponentPlan{
			{
				Name:        "kubernetes",
				PackageName: "kubernetes-rootfs",
				Version:     "v1.30.3",
				Class:       packageformat.ClassRootfs,
				Artifact:    "registry.example.io/sealos/kubernetes-rootfs:v1.30.3@sha256:1111111111111111111111111111111111111111111111111111111111111111",
				Inputs: []packageformat.Input{
					{Name: "kubeadm-config", Path: "files/etc/kubeadm-config.yaml"},
				},
				InputBindings: map[string]string{
					"kubeadm-config": defaultInputPath,
				},
				HostInputBindings: map[string]map[string]string{
					"kubeadm-config": {
						"10.0.0.11": hostInputPath,
					},
				},
				Steps: []Step{
					{
						Name:        "kubeadm-config",
						Kind:        StepContent,
						Path:        "files/etc/kubeadm-config.yaml",
						ContentType: packageformat.ContentFile,
					},
				},
			},
		},
	}

	root := filepath.Join("..", "packageformat", "testdata", "kubernetes-rootfs")
	out := t.TempDir()
	rendered, err := RenderPlan(plan, SourceMap{"kubernetes": root}, out)
	if err != nil {
		t.Fatalf("RenderPlan() error = %v", err)
	}

	component := rendered.Spec.Components[0]
	if got, want := component.InputBindings["kubeadm-config"], defaultInputPath; got != want {
		t.Fatalf("component.InputBindings[kubeadm-config] = %q, want %q", got, want)
	}
	if got, want := component.HostInputBindings["kubeadm-config"]["10.0.0.11"], "components/kubernetes/host-inputs/10.0.0.11/files/etc/kubeadm-config.yaml"; got != want {
		t.Fatalf("component.HostInputBindings[kubeadm-config][10.0.0.11] = %q, want %q", got, want)
	}
	if !exists(filepath.Join(out, component.HostInputBindings["kubeadm-config"]["10.0.0.11"])) {
		t.Fatalf("expected rendered host-scoped input %q to exist", component.HostInputBindings["kubeadm-config"]["10.0.0.11"])
	}
}

func TestMountedArtifactSourceProvider(t *testing.T) {
	t.Parallel()

	fake := &fakeRenderMounter{
		mounts: map[string]packageformat.MountedImage{
			"registry.example.io/sealos/kubernetes-rootfs:v1.30.3@sha256:1111111111111111111111111111111111111111111111111111111111111111": {
				Name:       "mounted-kubernetes",
				MountPoint: "/tmp/kubernetes",
			},
		},
	}
	provider := NewMountedArtifactSourceProvider(fake)

	component := ComponentPlan{
		Name:     "kubernetes",
		Artifact: "registry.example.io/sealos/kubernetes-rootfs:v1.30.3@sha256:1111111111111111111111111111111111111111111111111111111111111111",
	}

	source, err := provider.Source(component)
	if err != nil {
		t.Fatalf("Source() error = %v", err)
	}
	if got, want := source.Root, "/tmp/kubernetes"; got != want {
		t.Fatalf("source.Root = %q, want %q", got, want)
	}

	reused, err := provider.Source(component)
	if err != nil {
		t.Fatalf("second Source() error = %v", err)
	}
	if got, want := reused.Root, "/tmp/kubernetes"; got != want {
		t.Fatalf("reused.Root = %q, want %q", got, want)
	}
	if got, want := len(fake.mounted), 1; got != want {
		t.Fatalf("len(fake.mounted) = %d, want %d", got, want)
	}

	if err := provider.Close(); err != nil {
		t.Fatalf("Close() error = %v", err)
	}
	if got, want := len(fake.unmounted), 1; got != want {
		t.Fatalf("len(fake.unmounted) = %d, want %d", got, want)
	}
	if got, want := fake.unmounted[0], "mounted-kubernetes"; got != want {
		t.Fatalf("fake.unmounted[0] = %q, want %q", got, want)
	}
}

type fakeRenderMounter struct {
	mounts    map[string]packageformat.MountedImage
	mounted   []string
	unmounted []string
}

func (f *fakeRenderMounter) Mount(image string) (packageformat.MountedImage, error) {
	f.mounted = append(f.mounted, image)
	info, ok := f.mounts[image]
	if !ok {
		return packageformat.MountedImage{}, os.ErrNotExist
	}
	return info, nil
}

func (f *fakeRenderMounter) Unmount(name string) error {
	f.unmounted = append(f.unmounted, name)
	return nil
}

func exists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

var _ packageformat.ImageMounter = (*fakeRenderMounter)(nil)
