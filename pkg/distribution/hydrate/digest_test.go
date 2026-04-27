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

	"github.com/labring/sealos/pkg/distribution/bom"
	"github.com/labring/sealos/pkg/distribution/packageformat"
)

func TestDigestBundleStableAcrossRenderRoots(t *testing.T) {
	t.Parallel()

	renderA := renderBundleFixture(t)
	renderB := renderBundleFixture(t)

	digestA, err := DigestBundle(renderA)
	if err != nil {
		t.Fatalf("DigestBundle(renderA) error = %v", err)
	}
	digestB, err := DigestBundle(renderB)
	if err != nil {
		t.Fatalf("DigestBundle(renderB) error = %v", err)
	}

	if digestA != digestB {
		t.Fatalf("DigestBundle() mismatch: %q != %q", digestA, digestB)
	}
}

func TestDigestBundleDetectsContentChange(t *testing.T) {
	t.Parallel()

	root := renderBundleFixture(t)
	before, err := DigestBundle(root)
	if err != nil {
		t.Fatalf("DigestBundle(before) error = %v", err)
	}

	target := filepath.Join(root, "components", "kubernetes", "files", "hooks", "bootstrap.sh")
	if err := os.WriteFile(target, []byte("#!/bin/sh\necho changed\n"), 0o755); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	after, err := DigestBundle(root)
	if err != nil {
		t.Fatalf("DigestBundle(after) error = %v", err)
	}
	if before == after {
		t.Fatalf("DigestBundle() = %q after content change, want different digest", after)
	}
}

func TestDigestBundleRejectsInvalidRoot(t *testing.T) {
	t.Parallel()

	if _, err := DigestBundle(""); err == nil {
		t.Fatal("DigestBundle(\"\") error = nil, want error")
	}
	if _, err := DigestBundle(filepath.Join(t.TempDir(), "missing")); err == nil {
		t.Fatal("DigestBundle(missing) error = nil, want error")
	}
}

func renderBundleFixture(t *testing.T) string {
	t.Helper()

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
	if _, err := RenderPlan(plan, SourceMap{"kubernetes": root}, out); err != nil {
		t.Fatalf("RenderPlan() error = %v", err)
	}
	return out
}
