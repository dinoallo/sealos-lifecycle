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

package ownership

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestDefaultLocalPatchPolicyDocumentValidate(t *testing.T) {
	t.Parallel()

	doc := DefaultLocalPatchPolicyDocument()
	if err := doc.Validate(); err != nil {
		t.Fatalf("Validate() error = %v", err)
	}
	if got, want := doc.EffectiveName(), DefaultLocalPatchPolicyName; got != want {
		t.Fatalf("EffectiveName() = %q, want %q", got, want)
	}
	if got, want := doc.Spec.EffectiveScope(), LocalPatchPolicyScopeClusterLocal; got != want {
		t.Fatalf("doc.Spec.EffectiveScope() = %q, want %q", got, want)
	}
}

func TestLoadLocalPatchPolicyFile(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	path := filepath.Join(root, LocalPatchPolicyFileName)
	if err := os.WriteFile(path, []byte("apiVersion: distribution.sealos.io/v1alpha1\nkind: LocalPatchPolicy\nmetadata:\n  name: custom\nspec:\n  forbiddenExactPaths:\n    - status\n    - spec.selector\n  forbiddenMetadataKeys:\n    - uid\n    - resourceVersion\n    - generation\n    - creationTimestamp\n    - managedFields\n    - ownerReferences\n    - finalizers\n    - generateName\n    - selfLink\n    - deletionTimestamp\n    - deletionGracePeriodSeconds\n  forbiddenContainerFields:\n    - image\n  kindRules:\n    - kind: ConfigMap\n      allowedPrefixes:\n        - metadata.annotations\n"), 0o644); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	doc, err := LoadLocalPatchPolicyFile(path)
	if err != nil {
		t.Fatalf("LoadLocalPatchPolicyFile() error = %v", err)
	}
	if got, want := doc.EffectiveName(), "custom"; got != want {
		t.Fatalf("doc.EffectiveName() = %q, want %q", got, want)
	}
	if got, want := doc.Spec.KindRules[0].AllowedPrefixes[0], "metadata.annotations"; got != want {
		t.Fatalf("doc.Spec.KindRules[0].AllowedPrefixes[0] = %q, want %q", got, want)
	}
	if got, want := doc.Spec.EffectiveScope(), LocalPatchPolicyScopeClusterLocal; got != want {
		t.Fatalf("doc.Spec.EffectiveScope() = %q, want %q", got, want)
	}
}

func TestLoadLocalPatchPolicyFileRejectsUnsupportedScope(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	path := filepath.Join(root, LocalPatchPolicyFileName)
	if err := os.WriteFile(path, []byte("apiVersion: distribution.sealos.io/v1alpha1\nkind: LocalPatchPolicy\nmetadata:\n  name: custom\nspec:\n  scope: packageBaseline\n  forbiddenExactPaths:\n    - status\n    - spec.selector\n  forbiddenMetadataKeys:\n    - uid\n    - resourceVersion\n    - generation\n    - creationTimestamp\n    - managedFields\n    - ownerReferences\n    - finalizers\n    - generateName\n    - selfLink\n    - deletionTimestamp\n    - deletionGracePeriodSeconds\n  forbiddenContainerFields:\n    - image\n  kindRules:\n    - kind: ConfigMap\n      allowedPrefixes:\n        - metadata.annotations\n"), 0o644); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	if _, err := LoadLocalPatchPolicyFile(path); err == nil {
		t.Fatal("LoadLocalPatchPolicyFile() error = nil, want error")
	}
}

func TestLoadLocalPatchPolicyFileFixtures(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		file    string
		wantErr string
	}{
		{
			name: "valid cluster local fixture",
			file: "valid-cluster-local.yaml",
		},
		{
			name:    "unsupported scope fixture",
			file:    "invalid-unsupported-scope.yaml",
			wantErr: `unsupported local patch policy scope "packageBaseline"`,
		},
		{
			name:    "missing required exact path fixture",
			file:    "invalid-missing-required-exact-path.yaml",
			wantErr: `forbiddenExactPaths must include required path "spec.selector"`,
		},
		{
			name:    "missing required metadata key fixture",
			file:    "invalid-missing-required-metadata-key.yaml",
			wantErr: `forbiddenMetadataKeys must include required metadata key "resourceVersion"`,
		},
		{
			name:    "missing required container field fixture",
			file:    "invalid-missing-required-container-field.yaml",
			wantErr: `forbiddenContainerFields must include required container field "image"`,
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			path := filepath.Join("testdata", tt.file)
			doc, err := LoadLocalPatchPolicyFile(path)
			if tt.wantErr == "" {
				if err != nil {
					t.Fatalf("LoadLocalPatchPolicyFile(%q) error = %v", path, err)
				}
				if doc == nil {
					t.Fatalf("LoadLocalPatchPolicyFile(%q) = nil, want document", path)
				}
				return
			}
			if err == nil {
				t.Fatalf("LoadLocalPatchPolicyFile(%q) error = nil, want %q", path, tt.wantErr)
			}
			if got := err.Error(); got == "" || !strings.Contains(got, tt.wantErr) {
				t.Fatalf("LoadLocalPatchPolicyFile(%q) error = %q, want substring %q", path, got, tt.wantErr)
			}
		})
	}
}
