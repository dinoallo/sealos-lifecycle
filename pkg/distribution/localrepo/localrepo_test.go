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

package localrepo

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/labring/sealos/pkg/distribution/ownership"
	"github.com/labring/sealos/pkg/distribution/packageformat"
)

func TestLoadAndBindingFor(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	writeLocalInput(t, root, "cilium", "cilium-values.yaml", "hubble:\n  enabled: true\n")
	writeLocalHostInput(t, root, "cilium", "10.0.0.11", "cilium-values.yaml", "hubble:\n  enabled: false\n")
	writeLocalInput(t, root, "kubernetes", "kubeadm-config.yaml", "apiVersion: kubeadm.k8s.io/v1beta4\n")
	writeLocalResource(t, root, filepath.Join("secrets", "grafana-admin-credentials.yaml"), "apiVersion: v1\nkind: Secret\nmetadata:\n  name: grafana-admin-credentials\n")
	writeLocalPatch(t, root, "cilium", "config/cilium-config.patch.yaml", "apiVersion: v1\nkind: ConfigMap\nmetadata:\n  name: cilium-config\n  namespace: kube-system\ndata:\n  enable-hubble: \"true\"\n")
	writeLocalPatchPolicy(t, root, "apiVersion: distribution.sealos.io/v1alpha1\nkind: LocalPatchPolicy\nmetadata:\n  name: custom-local-policy\nspec:\n  scope: clusterLocal\n  forbiddenExactPaths:\n    - status\n    - spec.selector\n  forbiddenMetadataKeys:\n    - uid\n    - resourceVersion\n    - generation\n    - creationTimestamp\n    - managedFields\n    - ownerReferences\n    - finalizers\n    - generateName\n    - selfLink\n    - deletionTimestamp\n    - deletionGracePeriodSeconds\n  forbiddenContainerFields:\n    - image\n  kindRules:\n    - kind: ConfigMap\n      allowedPrefixes:\n        - data\n        - metadata.annotations\n")

	repo, err := Load(root)
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if got, want := repo.Root, root; got != want {
		t.Fatalf("repo.Root = %q, want %q", got, want)
	}
	if !strings.HasPrefix(repo.Revision, "sha256:") {
		t.Fatalf("repo.Revision = %q, want sha256 digest", repo.Revision)
	}

	path, ok := repo.BindingFor("cilium", packageformat.Input{Name: "cilium-values"})
	if !ok {
		t.Fatal("BindingFor(cilium-values) = missing, want value")
	}
	if got, want := filepath.Base(path), "cilium-values.yaml"; got != want {
		t.Fatalf("binding basename = %q, want %q", got, want)
	}

	if _, ok := repo.BindingFor("cilium", packageformat.Input{Name: "missing"}); ok {
		t.Fatal("BindingFor(missing) = present, want absent")
	}
	hostBindings := repo.HostBindingsFor("cilium", packageformat.Input{Name: "cilium-values"})
	if got, want := len(hostBindings), 1; got != want {
		t.Fatalf("len(repo.HostBindingsFor(cilium-values)) = %d, want %d", got, want)
	}
	if got, want := filepath.Base(hostBindings["10.0.0.11"]), "cilium-values.yaml"; got != want {
		t.Fatalf("host binding basename = %q, want %q", got, want)
	}
	hostBindingPath, ok := repo.HostBindingPath("cilium", "cilium-values", "10.0.0.11:22")
	if !ok {
		t.Fatal("HostBindingPath(cilium-values, 10.0.0.11:22) = missing, want value")
	}
	if got, want := filepath.Base(hostBindingPath), "cilium-values.yaml"; got != want {
		t.Fatalf("host binding path basename = %q, want %q", got, want)
	}
	resources := repo.Resources()
	if got, want := len(resources), 1; got != want {
		t.Fatalf("len(repo.Resources()) = %d, want %d", got, want)
	}
	if got, want := resources[0].RelativePath, "secrets/grafana-admin-credentials.yaml"; got != want {
		t.Fatalf("resource relative path = %q, want %q", got, want)
	}
	patches := repo.PatchesFor("cilium")
	if got, want := len(patches), 1; got != want {
		t.Fatalf("len(repo.PatchesFor(cilium)) = %d, want %d", got, want)
	}
	if got, want := patches[0].RelativePath, "config/cilium-config.patch.yaml"; got != want {
		t.Fatalf("patch relative path = %q, want %q", got, want)
	}
	policy := repo.LocalPatchPolicy()
	if policy == nil {
		t.Fatal("repo.LocalPatchPolicy() = nil, want policy")
	}
	if got, want := policy.Metadata.Name, "custom-local-policy"; got != want {
		t.Fatalf("policy.Metadata.Name = %q, want %q", got, want)
	}
	if got, want := policy.Kind, ownership.LocalPatchPolicyKind; got != want {
		t.Fatalf("policy.Kind = %q, want %q", got, want)
	}
	if got, want := policy.Spec.EffectiveScope(), ownership.LocalPatchPolicyScopeClusterLocal; got != want {
		t.Fatalf("policy.Spec.EffectiveScope() = %q, want %q", got, want)
	}
}

func TestLoadRejectsDuplicateBasenames(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	writeLocalInput(t, root, "cilium", "cilium-values.yaml", "a: 1\n")
	writeLocalInput(t, root, "cilium", "cilium-values.json", "{\"a\":1}\n")

	if _, err := Load(root); err == nil {
		t.Fatal("Load() error = nil, want error")
	}
}

func writeLocalInput(t *testing.T, root, component, filename, content string) {
	t.Helper()

	path := filepath.Join(root, InputsDirName, component, filename)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("MkdirAll(%q) error = %v", filepath.Dir(path), err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("WriteFile(%q) error = %v", path, err)
	}
}

func writeLocalHostInput(t *testing.T, root, component, host, filename, content string) {
	t.Helper()

	path := filepath.Join(root, InputsDirName, component, "hosts", host, filename)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("MkdirAll(%q) error = %v", filepath.Dir(path), err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("WriteFile(%q) error = %v", path, err)
	}
}

func writeLocalResource(t *testing.T, root, relativePath, content string) {
	t.Helper()

	path := filepath.Join(root, ResourcesDirName, relativePath)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("MkdirAll(%q) error = %v", filepath.Dir(path), err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("WriteFile(%q) error = %v", path, err)
	}
}

func writeLocalPatch(t *testing.T, root, component, relativePath, content string) {
	t.Helper()

	path := filepath.Join(root, PatchesDirName, component, filepath.FromSlash(relativePath))
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("MkdirAll(%q) error = %v", filepath.Dir(path), err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("WriteFile(%q) error = %v", path, err)
	}
}

func writeLocalPatchPolicy(t *testing.T, root, content string) {
	t.Helper()

	path := filepath.Join(root, PolicyDirName, ownership.LocalPatchPolicyFileName)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("MkdirAll(%q) error = %v", filepath.Dir(path), err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("WriteFile(%q) error = %v", path, err)
	}
}
