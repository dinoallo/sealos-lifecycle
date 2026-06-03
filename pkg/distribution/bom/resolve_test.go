// Copyright © 2026 sealos.
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

package bom

import (
	"fmt"
	"testing"

	"github.com/labring/sealos/pkg/distribution/packageformat"
)

func TestArtifactReferenceReference(t *testing.T) {
	t.Parallel()

	ref := ArtifactReference{
		Image:  "registry.example.io/sealos/calico:v3.28.0",
		Digest: "sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
	}
	got := ref.Reference()
	want := "registry.example.io/sealos/calico:v3.28.0@sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	if got != want {
		t.Fatalf("Reference() = %q, want %q", got, want)
	}
}

func TestBOMResolveComponentPackages(t *testing.T) {
	t.Parallel()

	doc := validBOM()
	resolved, err := doc.ResolveComponentPackages(packageformat.LoaderFunc(func(image string) (*packageformat.ComponentPackage, error) {
		switch image {
		case "registry.example.io/sealos/calico:3.28.0@sha256:bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb":
			return packageformat.New("calico-package", "calico", "3.28.0", packageformat.ClassApplication), nil
		case "registry.example.io/sealos/ingress-nginx:1.10.1@sha256:cccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccc":
			return packageformat.New("ingress-package", "ingress-nginx", "1.10.1", packageformat.ClassApplication), nil
		default:
			return nil, fmt.Errorf("unexpected image %q", image)
		}
	}))
	if err != nil {
		t.Fatalf("ResolveComponentPackages() error = %v", err)
	}
	if len(resolved) != 2 {
		t.Fatalf("len(resolved) = %d, want 2", len(resolved))
	}
	if resolved["calico"].Spec.Component != "calico" {
		t.Fatalf("resolved calico component = %q, want calico", resolved["calico"].Spec.Component)
	}
}

func TestBOMResolveComponentPackagesRejectsMismatch(t *testing.T) {
	t.Parallel()

	doc := validBOM()
	_, err := doc.ResolveComponentPackages(packageformat.LoaderFunc(func(string) (*packageformat.ComponentPackage, error) {
		return packageformat.New("wrong", "different-component", "3.28.0", packageformat.ClassApplication), nil
	}))
	if err == nil {
		t.Fatal("ResolveComponentPackages() error = nil, want error")
	}
}
