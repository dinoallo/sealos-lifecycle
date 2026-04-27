package bom

import "testing"

func TestBOMValidate(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		mutate  func(*BOM)
		wantErr bool
	}{
		{
			name: "valid",
		},
		{
			name: "invalid channel",
			mutate: func(b *BOM) {
				b.Spec.Channel = ReleaseChannel("ga")
			},
			wantErr: true,
		},
		{
			name: "missing component digest",
			mutate: func(b *BOM) {
				b.Spec.Components[0].Artifact.Digest = ""
			},
			wantErr: true,
		},
		{
			name: "unknown dependency",
			mutate: func(b *BOM) {
				b.Spec.Components[0].Dependencies = []string{"missing"}
			},
			wantErr: true,
		},
		{
			name: "duplicate component",
			mutate: func(b *BOM) {
				b.Spec.Components = append(b.Spec.Components, b.Spec.Components[0])
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			doc := validBOM()
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

func validBOM() *BOM {
	doc := New("default-platform", "rev-20240423", ChannelBeta)
	doc.Spec.BaseArtifacts = []ArtifactReference{
		{
			Name:   "platform-base",
			Image:  "registry.example.io/sealos/platform-base:1.0.0",
			Digest: "sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
		},
	}
	doc.Spec.Components = []Component{
		{
			Name:    "calico",
			Kind:    "infra",
			Version: "3.28.0",
			Artifact: ArtifactReference{
				Name:   "calico-artifact",
				Image:  "registry.example.io/sealos/calico:3.28.0",
				Digest: "sha256:bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb",
			},
			Required: true,
		},
		{
			Name:         "ingress-nginx",
			Kind:         "infra",
			Version:      "1.10.1",
			Dependencies: []string{"calico"},
			Artifact: ArtifactReference{
				Name:   "ingress-artifact",
				Image:  "registry.example.io/sealos/ingress-nginx:1.10.1",
				Digest: "sha256:cccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccc",
			},
		},
	}
	return doc
}
