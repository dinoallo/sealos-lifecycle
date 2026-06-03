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

import "testing"

func TestLocalPatchPolicyValidate(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		policy  LocalPatchPolicy
		wantErr bool
	}{
		{
			name:   "default policy is valid",
			policy: DefaultLocalPatchPolicy(),
		},
		{
			name: "kind rule requires kind",
			policy: LocalPatchPolicy{
				KindRules: []LocalPatchKindRule{
					{AllowedPrefixes: []string{"data"}},
				},
			},
			wantErr: true,
		},
		{
			name: "kind rule requires prefixes",
			policy: LocalPatchPolicy{
				KindRules: []LocalPatchKindRule{
					{Kind: "ConfigMap"},
				},
			},
			wantErr: true,
		},
		{
			name: "policy requires kind rules",
			policy: LocalPatchPolicy{
				ForbiddenExactPaths: []string{"status"},
			},
			wantErr: true,
		},
		{
			name: "policy requires spec.selector forbidden path",
			policy: LocalPatchPolicy{
				ForbiddenExactPaths: []string{"status"},
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
				ForbiddenContainerFields: []string{"image"},
				KindRules: []LocalPatchKindRule{
					{Kind: "ConfigMap", AllowedPrefixes: []string{"data"}},
				},
			},
			wantErr: true,
		},
		{
			name: "policy requires resourceVersion metadata key",
			policy: LocalPatchPolicy{
				ForbiddenExactPaths: []string{"status", "spec.selector"},
				ForbiddenMetadataKeys: []string{
					"uid",
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
				ForbiddenContainerFields: []string{"image"},
				KindRules: []LocalPatchKindRule{
					{Kind: "ConfigMap", AllowedPrefixes: []string{"data"}},
				},
			},
			wantErr: true,
		},
		{
			name: "policy requires image container field",
			policy: LocalPatchPolicy{
				ForbiddenExactPaths: []string{"status", "spec.selector"},
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
				ForbiddenContainerFields: []string{},
				KindRules: []LocalPatchKindRule{
					{Kind: "ConfigMap", AllowedPrefixes: []string{"data"}},
				},
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			err := tt.policy.Validate()
			if tt.wantErr && err == nil {
				t.Fatal("Validate() error = nil, want error")
			}
			if !tt.wantErr && err != nil {
				t.Fatalf("Validate() error = %v", err)
			}
		})
	}
}

func TestLocalPatchPolicyPathMatching(t *testing.T) {
	t.Parallel()

	policy := DefaultLocalPatchPolicy()

	if !policy.IsAllowed("DaemonSet", []string{"spec", "template", "spec", "nodeSelector"}) {
		t.Fatal("expected DaemonSet nodeSelector path to be allowed")
	}
	if !policy.IsAllowed("DaemonSet", []string{"spec", "template", "spec", "containers", "env", "name"}) {
		t.Fatal("expected DaemonSet container env.name path to be allowed by prefix matching")
	}
	if policy.IsAllowed("ConfigMap", []string{"metadata", "annotations"}) {
		t.Fatal("expected ConfigMap metadata.annotations to be denied by default policy")
	}
	if !policy.IsForbidden([]string{"status"}) {
		t.Fatal("expected status to be forbidden")
	}
	if !policy.IsForbidden([]string{"metadata", "resourceVersion"}) {
		t.Fatal("expected metadata.resourceVersion to be forbidden")
	}
	if !policy.IsForbidden([]string{"spec", "template", "spec", "containers", "image"}) {
		t.Fatal("expected container image to be forbidden")
	}
}
