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

package ownership

import "testing"

func TestDiffLocalPatchPolicy(t *testing.T) {
	t.Parallel()

	oldPolicy := LocalPatchPolicy{
		Scope: LocalPatchPolicyScopeClusterLocal,
		ForbiddenExactPaths: []string{
			"status",
			"spec.selector",
			"spec.template.spec.hostNetwork",
		},
		ForbiddenMetadataKeys: []string{
			"uid",
			"resourceVersion",
		},
		ForbiddenContainerFields: []string{
			"image",
			"command",
		},
		KindRules: []LocalPatchKindRule{
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
	}
	newPolicy := LocalPatchPolicy{
		Scope: LocalPatchPolicyScopeClusterLocal,
		ForbiddenExactPaths: []string{
			"status",
			"spec.selector",
			"metadata.annotations.owner",
		},
		ForbiddenMetadataKeys: []string{
			"uid",
			"resourceVersion",
			"managedFields",
		},
		ForbiddenContainerFields: []string{
			"image",
		},
		KindRules: []LocalPatchKindRule{
			{
				Kind: "ConfigMap",
				AllowedPrefixes: []string{
					"data",
					"metadata.annotations",
				},
			},
		},
	}

	impact := DiffLocalPatchPolicy(oldPolicy, newPolicy)

	if got, want := len(impact.AddedAllowedPrefixes), 1; got != want {
		t.Fatalf("len(impact.AddedAllowedPrefixes) = %d, want %d", got, want)
	}
	if got, want := impact.AddedAllowedPrefixes[0], (KindPath{Kind: "ConfigMap", Path: "metadata.annotations"}); got != want {
		t.Fatalf("impact.AddedAllowedPrefixes[0] = %#v, want %#v", got, want)
	}
	if got, want := len(impact.RemovedAllowedPrefixes), 1; got != want {
		t.Fatalf("len(impact.RemovedAllowedPrefixes) = %d, want %d", got, want)
	}
	if got, want := impact.RemovedAllowedPrefixes[0], (KindPath{Kind: "Deployment", Path: "spec.template.spec.nodeSelector"}); got != want {
		t.Fatalf("impact.RemovedAllowedPrefixes[0] = %#v, want %#v", got, want)
	}
	if got, want := impact.AddedForbiddenExactPaths[0], "metadata.annotations.owner"; got != want {
		t.Fatalf("impact.AddedForbiddenExactPaths[0] = %q, want %q", got, want)
	}
	if got, want := impact.RemovedForbiddenExactPaths[0], "spec.template.spec.hostNetwork"; got != want {
		t.Fatalf("impact.RemovedForbiddenExactPaths[0] = %q, want %q", got, want)
	}
	if got, want := impact.AddedForbiddenMetadataKeys[0], "managedFields"; got != want {
		t.Fatalf("impact.AddedForbiddenMetadataKeys[0] = %q, want %q", got, want)
	}
	if got, want := impact.RemovedForbiddenContainerFields[0], "command"; got != want {
		t.Fatalf("impact.RemovedForbiddenContainerFields[0] = %q, want %q", got, want)
	}
	if !impact.HasWideningChanges() {
		t.Fatal("impact.HasWideningChanges() = false, want true")
	}
	if !impact.HasNarrowingChanges() {
		t.Fatal("impact.HasNarrowingChanges() = false, want true")
	}
}
