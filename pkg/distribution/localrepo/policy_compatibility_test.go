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
	"strings"
	"testing"

	"github.com/labring/sealos/pkg/distribution/ownership"
)

func TestEvaluatePatchCompatibility(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	writeLocalPatch(t, root, "grafana", "grafana-settings.patch.yaml", "apiVersion: v1\nkind: ConfigMap\nmetadata:\n  name: grafana-settings\n  namespace: default\n  annotations:\n    localFeature: \"true\"\ndata:\n  adminUser: ops\n")
	writeLocalPatch(t, root, "grafana", "grafana-image.patch.yaml", "apiVersion: apps/v1\nkind: Deployment\nmetadata:\n  name: grafana\n  namespace: default\nspec:\n  template:\n    spec:\n      containers:\n        - name: grafana\n          image: example.com/forbidden:latest\n")

	repo, err := Load(root)
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}

	policy := ownership.LocalPatchPolicy{
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
			{
				Kind: "Deployment",
				AllowedPrefixes: []string{
					"spec.template.spec.nodeSelector",
				},
			},
		},
	}

	report, err := EvaluatePatchCompatibility(repo, policy)
	if err != nil {
		t.Fatalf("EvaluatePatchCompatibility() error = %v", err)
	}
	if got, want := report.ValidPatchCount, 1; got != want {
		t.Fatalf("report.ValidPatchCount = %d, want %d", got, want)
	}
	if got, want := len(report.InvalidPatches), 1; got != want {
		t.Fatalf("len(report.InvalidPatches) = %d, want %d", got, want)
	}
	if got, want := report.InvalidPatches[0].Component, "grafana"; got != want {
		t.Fatalf("report.InvalidPatches[0].Component = %q, want %q", got, want)
	}
	if got, want := report.InvalidPatches[0].RelativePath, "grafana-image.patch.yaml"; got != want {
		t.Fatalf("report.InvalidPatches[0].RelativePath = %q, want %q", got, want)
	}
	if got := report.InvalidPatches[0].Reason; !strings.Contains(got, `local patch path "spec.template.spec.containers" is not allowed for kind "Deployment"`) {
		t.Fatalf("report.InvalidPatches[0].Reason = %q, want disallowed deployment container path", got)
	}
	if !report.HasIncompatiblePatches() {
		t.Fatal("report.HasIncompatiblePatches() = false, want true")
	}
}
