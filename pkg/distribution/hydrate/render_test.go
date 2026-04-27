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
