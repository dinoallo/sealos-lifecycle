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

func TestValidateLocalPatchOverlay(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		patch   map[string]interface{}
		wantErr bool
	}{
		{
			name: "configmap data overlay",
			patch: map[string]interface{}{
				"apiVersion": "v1",
				"kind":       "ConfigMap",
				"metadata": map[string]interface{}{
					"name":      "cilium-config",
					"namespace": "kube-system",
				},
				"data": map[string]interface{}{
					"enable-hubble": "true",
				},
			},
		},
		{
			name: "daemonset placement overlay",
			patch: map[string]interface{}{
				"apiVersion": "apps/v1",
				"kind":       "DaemonSet",
				"metadata": map[string]interface{}{
					"name":      "cilium",
					"namespace": "kube-system",
				},
				"spec": map[string]interface{}{
					"template": map[string]interface{}{
						"spec": map[string]interface{}{
							"nodeSelector": map[string]interface{}{
								"node-role.kubernetes.io/control-plane": "",
							},
							"tolerations": []interface{}{
								map[string]interface{}{
									"key":      "node-role.kubernetes.io/control-plane",
									"operator": "Exists",
									"effect":   "NoSchedule",
								},
							},
						},
					},
				},
			},
		},
		{
			name: "ingress rules overlay",
			patch: map[string]interface{}{
				"apiVersion": "networking.k8s.io/v1",
				"kind":       "Ingress",
				"metadata": map[string]interface{}{
					"name":      "grafana",
					"namespace": "monitoring",
					"annotations": map[string]interface{}{
						"nginx.ingress.kubernetes.io/proxy-body-size": "16m",
					},
				},
				"spec": map[string]interface{}{
					"tls": []interface{}{
						map[string]interface{}{
							"hosts":      []interface{}{"grafana.example.com"},
							"secretName": "grafana-tls",
						},
					},
					"rules": []interface{}{
						map[string]interface{}{
							"host": "grafana.example.com",
						},
					},
				},
			},
		},
		{
			name: "identity only rejected",
			patch: map[string]interface{}{
				"apiVersion": "v1",
				"kind":       "ConfigMap",
				"metadata": map[string]interface{}{
					"name":      "cilium-config",
					"namespace": "kube-system",
				},
			},
			wantErr: true,
		},
		{
			name: "status rejected",
			patch: map[string]interface{}{
				"apiVersion": "v1",
				"kind":       "ConfigMap",
				"metadata": map[string]interface{}{
					"name":      "cilium-config",
					"namespace": "kube-system",
				},
				"status": map[string]interface{}{
					"phase": "Ready",
				},
			},
			wantErr: true,
		},
		{
			name: "server metadata rejected",
			patch: map[string]interface{}{
				"apiVersion": "v1",
				"kind":       "ConfigMap",
				"metadata": map[string]interface{}{
					"name":            "cilium-config",
					"namespace":       "kube-system",
					"resourceVersion": "1",
				},
				"data": map[string]interface{}{
					"enable-hubble": "true",
				},
			},
			wantErr: true,
		},
		{
			name: "configmap annotations rejected by allowlist",
			patch: map[string]interface{}{
				"apiVersion": "v1",
				"kind":       "ConfigMap",
				"metadata": map[string]interface{}{
					"name":      "cilium-config",
					"namespace": "kube-system",
					"annotations": map[string]interface{}{
						"example.com/local": "true",
					},
				},
				"data": map[string]interface{}{
					"enable-hubble": "true",
				},
			},
			wantErr: true,
		},
		{
			name: "selector rejected",
			patch: map[string]interface{}{
				"apiVersion": "v1",
				"kind":       "Service",
				"metadata": map[string]interface{}{
					"name":      "cilium",
					"namespace": "kube-system",
				},
				"spec": map[string]interface{}{
					"selector": map[string]interface{}{
						"app": "other",
					},
				},
			},
			wantErr: true,
		},
		{
			name: "container image rejected",
			patch: map[string]interface{}{
				"apiVersion": "apps/v1",
				"kind":       "Deployment",
				"metadata": map[string]interface{}{
					"name":      "cilium-operator",
					"namespace": "kube-system",
				},
				"spec": map[string]interface{}{
					"template": map[string]interface{}{
						"spec": map[string]interface{}{
							"containers": []interface{}{
								map[string]interface{}{
									"name":  "cilium-operator",
									"image": "example.com/forbidden:latest",
								},
							},
						},
					},
				},
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			err := ValidateLocalPatchOverlay(tt.patch)
			if tt.wantErr && err == nil {
				t.Fatal("ValidateLocalPatchOverlay() error = nil, want error")
			}
			if !tt.wantErr && err != nil {
				t.Fatalf("ValidateLocalPatchOverlay() error = %v", err)
			}
		})
	}
}

func TestExtractAllowedLocalPatchOverlay(t *testing.T) {
	t.Parallel()

	live := map[string]interface{}{
		"apiVersion": "v1",
		"kind":       "ConfigMap",
		"metadata": map[string]interface{}{
			"name":            "grafana-settings",
			"namespace":       "default",
			"resourceVersion": "42",
			"annotations": map[string]interface{}{
				"ignored": "true",
			},
		},
		"data": map[string]interface{}{
			"adminUser": "ops",
		},
	}

	overlay, err := ExtractAllowedLocalPatchOverlay(live)
	if err != nil {
		t.Fatalf("ExtractAllowedLocalPatchOverlay() error = %v", err)
	}
	if _, ok := overlay["data"]; !ok {
		t.Fatal("overlay.data missing")
	}
	metadata, _ := overlay["metadata"].(map[string]interface{})
	if _, ok := metadata["resourceVersion"]; ok {
		t.Fatal("overlay.metadata.resourceVersion present, want filtered")
	}
	if _, ok := metadata["annotations"]; ok {
		t.Fatal("overlay.metadata.annotations present, want filtered for ConfigMap")
	}
	if got := metadata["name"]; got != "grafana-settings" {
		t.Fatalf("overlay.metadata.name = %#v, want grafana-settings", got)
	}
}

func TestValidateLocalPatchOverlayWithPolicy(t *testing.T) {
	t.Parallel()

	policy := LocalPatchPolicy{
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
		ForbiddenContainerFields: []string{"image"},
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

	patch := map[string]interface{}{
		"apiVersion": "v1",
		"kind":       "ConfigMap",
		"metadata": map[string]interface{}{
			"name":      "grafana-settings",
			"namespace": "default",
			"annotations": map[string]interface{}{
				"example.com/local": "true",
			},
		},
		"data": map[string]interface{}{
			"adminUser": "ops",
		},
	}

	if err := ValidateLocalPatchOverlayWithPolicy(policy, patch); err != nil {
		t.Fatalf("ValidateLocalPatchOverlayWithPolicy() error = %v", err)
	}
}

func TestExtractAllowedLocalPatchOverlayWithPolicy(t *testing.T) {
	t.Parallel()

	policy := LocalPatchPolicy{
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
		ForbiddenContainerFields: []string{"image"},
		KindRules: []LocalPatchKindRule{
			{
				Kind: "ConfigMap",
				AllowedPrefixes: []string{
					"metadata.annotations",
				},
			},
		},
	}

	live := map[string]interface{}{
		"apiVersion": "v1",
		"kind":       "ConfigMap",
		"metadata": map[string]interface{}{
			"name":      "grafana-settings",
			"namespace": "default",
			"annotations": map[string]interface{}{
				"example.com/local": "true",
			},
			"resourceVersion": "42",
		},
		"data": map[string]interface{}{
			"adminUser": "ops",
		},
	}

	overlay, err := ExtractAllowedLocalPatchOverlayWithPolicy(policy, live)
	if err != nil {
		t.Fatalf("ExtractAllowedLocalPatchOverlayWithPolicy() error = %v", err)
	}
	metadata, _ := overlay["metadata"].(map[string]interface{})
	annotations, _ := metadata["annotations"].(map[string]interface{})
	if got := annotations["example.com/local"]; got != "true" {
		t.Fatalf("overlay.metadata.annotations[example.com/local] = %#v, want true", got)
	}
	if _, ok := overlay["data"]; ok {
		t.Fatal("overlay.data present, want filtered by custom policy")
	}
	if _, ok := metadata["resourceVersion"]; ok {
		t.Fatal("overlay.metadata.resourceVersion present, want filtered")
	}
}
