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

package packageformat

import "testing"

func TestComponentPackageValidate(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		mutate  func(*ComponentPackage)
		wantErr bool
	}{
		{
			name: "valid",
		},
		{
			name: "invalid class",
			mutate: func(p *ComponentPackage) {
				p.Spec.Class = PackageClass("bundle")
			},
			wantErr: true,
		},
		{
			name: "path traversal",
			mutate: func(p *ComponentPackage) {
				p.Spec.Contents[0].Path = "../rootfs"
			},
			wantErr: true,
		},
		{
			name: "rootfs package missing rootfs content",
			mutate: func(p *ComponentPackage) {
				p.Spec.Contents[0].Type = ContentManifest
			},
			wantErr: true,
		},
		{
			name: "application package with rootfs content",
			mutate: func(p *ComponentPackage) {
				p.Spec.Class = ClassApplication
			},
			wantErr: true,
		},
		{
			name: "duplicate dependency",
			mutate: func(p *ComponentPackage) {
				p.Spec.Dependencies = []Dependency{
					{Name: "calico"},
					{Name: "calico"},
				}
			},
			wantErr: true,
		},
		{
			name: "valid local patch policy path",
			mutate: func(p *ComponentPackage) {
				p.Spec.LocalPatchPolicy = "policy/local-patch-policy.yaml"
			},
		},
		{
			name: "local patch policy path traversal",
			mutate: func(p *ComponentPackage) {
				p.Spec.LocalPatchPolicy = "../policy/local-patch-policy.yaml"
			},
			wantErr: true,
		},
		{
			name: "invalid hook target",
			mutate: func(p *ComponentPackage) {
				p.Spec.Hooks[0].Target = ExecutionTarget("node0")
			},
			wantErr: true,
		},
		{
			name: "valid generated host path output",
			mutate: func(p *ComponentPackage) {
				p.Spec.GeneratedOutputs.HostPaths = []GeneratedHostPathOutput{{
					Name:            "cilium-status",
					HostPath:        "/var/lib/sealos/generated/cilium/status.yaml",
					Tool:            "cilium-cli",
					Hook:            "healthcheck",
					APIVersion:      "v1",
					Kind:            "ConfigMap",
					Namespace:       "kube-system",
					ObjectName:      "cilium-status",
					ExpectedCommand: "cilium-status-renderer",
				}}
			},
		},
		{
			name: "generated host path must be absolute",
			mutate: func(p *ComponentPackage) {
				p.Spec.GeneratedOutputs.HostPaths = []GeneratedHostPathOutput{{
					HostPath:   "var/lib/sealos/generated/cilium/status.yaml",
					Tool:       "cilium-cli",
					APIVersion: "v1",
					Kind:       "ConfigMap",
					ObjectName: "cilium-status",
				}}
			},
			wantErr: true,
		},
		{
			name: "generated host path requires object name",
			mutate: func(p *ComponentPackage) {
				p.Spec.GeneratedOutputs.HostPaths = []GeneratedHostPathOutput{{
					HostPath:   "/var/lib/sealos/generated/cilium/status.yaml",
					Tool:       "cilium-cli",
					APIVersion: "v1",
					Kind:       "ConfigMap",
				}}
			},
			wantErr: true,
		},
		{
			name: "duplicate generated host path",
			mutate: func(p *ComponentPackage) {
				p.Spec.GeneratedOutputs.HostPaths = []GeneratedHostPathOutput{
					{
						HostPath:   "/var/lib/sealos/generated/cilium/status.yaml",
						Tool:       "cilium-cli",
						APIVersion: "v1",
						Kind:       "ConfigMap",
						ObjectName: "cilium-status",
					},
					{
						HostPath:   "/var/lib/sealos/generated/cilium/status.yaml",
						Tool:       "cilium-cli",
						APIVersion: "v1",
						Kind:       "ConfigMap",
						ObjectName: "cilium-status",
					},
				}
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			doc := validComponentPackage()
			if tt.mutate != nil {
				tt.mutate(doc)
			}
			err := doc.Validate()
			if tt.wantErr && err == nil {
				t.Fatal("expected validation error, got nil")
			}
			if !tt.wantErr && err != nil {
				t.Fatalf("expected no validation error, got %v", err)
			}
		})
	}
}

func validComponentPackage() *ComponentPackage {
	doc := New("kubernetes-rootfs", "kubernetes", "v1.30.3", ClassRootfs)
	doc.Spec.Dependencies = []Dependency{
		{Name: "registry-mirror", Optional: true},
	}
	doc.Spec.Compatibility = Compatibility{
		Kubernetes: ">=1.30.0 <1.31.0",
		Sealos:     ">=4.0.0",
		Platforms: []Platform{
			{OS: "linux", Arch: "amd64"},
			{OS: "linux", Arch: "arm64"},
		},
	}
	doc.Spec.Inputs = []Input{
		{
			Name:     "kubeadm-config",
			Type:     InputConfigFile,
			Path:     "files/etc/kubeadm-config.yaml",
			Format:   "yaml",
			Required: false,
		},
	}
	doc.Spec.Contents = []Content{
		{
			Name:      "rootfs",
			Type:      ContentRootfs,
			Path:      "rootfs/",
			MediaType: "application/vnd.sealos.rootfs.layer.v1+tar",
			Required:  true,
		},
		{
			Name:      "bootstrap-manifests",
			Type:      ContentManifest,
			Path:      "manifests/bootstrap.yaml",
			MediaType: "application/yaml",
		},
		{
			Name:      "bootstrap-hook",
			Type:      ContentHook,
			Path:      "hooks/bootstrap.sh",
			MediaType: "text/x-shellscript",
		},
	}
	doc.Spec.Hooks = []Hook{
		{
			Name:           "bootstrap-nodes",
			Phase:          PhaseBootstrap,
			Target:         TargetAllNodes,
			Path:           "hooks/bootstrap.sh",
			TimeoutSeconds: 300,
		},
	}
	return doc
}
