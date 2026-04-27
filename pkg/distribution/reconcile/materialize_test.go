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
	"github.com/labring/sealos/pkg/distribution/packageformat"
	"github.com/labring/sealos/pkg/distribution/state"
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
		PackageLoader:      loaderForDir(doc.Spec.Components[0].Artifact.Reference(), sourceRoot),
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
	if got, want := loaded.Spec.DesiredStateDigest, result.DesiredStateDigest; got != want {
		t.Fatalf("loaded.spec.desiredStateDigest = %q, want %q", got, want)
	}
	if !strings.HasPrefix(loaded.Spec.BOM.Digest, "sha256:") {
		t.Fatalf("loaded.spec.bom.digest = %q, want sha256 digest", loaded.Spec.BOM.Digest)
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
		PackageLoader: loaderForDir(doc.Spec.Components[0].Artifact.Reference(), sourceA),
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
		PackageLoader: loaderForDir(doc.Spec.Components[0].Artifact.Reference(), sourceB),
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
	if got, want := loaded.Spec.DesiredStateDigest, second.DesiredStateDigest; got != want {
		t.Fatalf("loaded.spec.desiredStateDigest = %q, want %q", got, want)
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
