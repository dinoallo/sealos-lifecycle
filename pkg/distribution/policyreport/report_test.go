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

package policyreport

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/labring/sealos/pkg/distribution/localrepo"
	"github.com/labring/sealos/pkg/distribution/ownership"
)

func TestBuild(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	writePatch(t, root, "grafana", "grafana-settings.patch.yaml", "apiVersion: v1\nkind: ConfigMap\nmetadata:\n  name: grafana-settings\n  namespace: default\n  annotations:\n    localFeature: \"true\"\ndata:\n  adminUser: ops\n")
	writePatch(t, root, "grafana", "grafana-image.patch.yaml", "apiVersion: apps/v1\nkind: Deployment\nmetadata:\n  name: grafana\n  namespace: default\nspec:\n  template:\n    spec:\n      containers:\n        - name: grafana\n          image: example.com/forbidden:latest\n")

	repo, err := localrepo.Load(root)
	if err != nil {
		t.Fatalf("localrepo.Load() error = %v", err)
	}

	oldDoc := &ownership.LocalPatchPolicyDocument{
		APIVersion: ownership.LocalPatchPolicyAPIVersion,
		Kind:       ownership.LocalPatchPolicyKind,
		Metadata: ownership.PolicyMetadata{
			Name: "old-policy",
		},
		Spec: ownership.LocalPatchPolicy{
			Scope: ownership.LocalPatchPolicyScopeClusterLocal,
			ForbiddenExactPaths: []string{
				"status",
				"spec.selector",
			},
			ForbiddenMetadataKeys: []string{
				"uid",
				"resourceVersion",
				"generation",
				"creationTimestamp",
				"managedFields",
				"ownerReferences",
				"finalizers",
				"generateName",
				"selfLink",
				"deletionTimestamp",
				"deletionGracePeriodSeconds",
			},
			ForbiddenContainerFields: []string{
				"image",
				"command",
			},
			KindRules: []ownership.LocalPatchKindRule{
				{
					Kind: "ConfigMap",
					AllowedPrefixes: []string{
						"data",
					},
				},
				{
					Kind: "Deployment",
					AllowedPrefixes: []string{
						"spec.template.spec.nodeSelector",
					},
				},
			},
		},
	}
	newDoc := &ownership.LocalPatchPolicyDocument{
		APIVersion: ownership.LocalPatchPolicyAPIVersion,
		Kind:       ownership.LocalPatchPolicyKind,
		Metadata: ownership.PolicyMetadata{
			Name: "new-policy",
		},
		Spec: ownership.LocalPatchPolicy{
			Scope: ownership.LocalPatchPolicyScopeClusterLocal,
			ForbiddenExactPaths: []string{
				"status",
				"spec.selector",
			},
			ForbiddenMetadataKeys: []string{
				"uid",
				"resourceVersion",
				"generation",
				"creationTimestamp",
				"managedFields",
				"ownerReferences",
				"finalizers",
				"generateName",
				"selfLink",
				"deletionTimestamp",
				"deletionGracePeriodSeconds",
			},
			ForbiddenContainerFields: []string{
				"image",
			},
			KindRules: []ownership.LocalPatchKindRule{
				{
					Kind: "ConfigMap",
					AllowedPrefixes: []string{
						"data",
						"metadata.annotations",
					},
				},
			},
		},
	}

	report, err := Build(oldDoc, newDoc, repo)
	if err != nil {
		t.Fatalf("Build() error = %v", err)
	}
	if got, want := report.Old.Name, "old-policy"; got != want {
		t.Fatalf("report.Old.Name = %q, want %q", got, want)
	}
	if got, want := report.New.Name, "new-policy"; got != want {
		t.Fatalf("report.New.Name = %q, want %q", got, want)
	}
	if !report.HasWideningChanges {
		t.Fatal("report.HasWideningChanges = false, want true")
	}
	if !report.HasNarrowingChanges {
		t.Fatal("report.HasNarrowingChanges = false, want true")
	}
	if got, want := report.Compatibility.ValidPatchCount, 1; got != want {
		t.Fatalf("report.Compatibility.ValidPatchCount = %d, want %d", got, want)
	}
	if got, want := len(report.Compatibility.InvalidPatches), 1; got != want {
		t.Fatalf("len(report.Compatibility.InvalidPatches) = %d, want %d", got, want)
	}
}

func writePatch(t *testing.T, root, component, relativePath, content string) {
	t.Helper()

	path := filepath.Join(root, localrepo.PatchesDirName, component, relativePath)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("MkdirAll(%q) error = %v", filepath.Dir(path), err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("WriteFile(%q) error = %v", path, err)
	}
}
