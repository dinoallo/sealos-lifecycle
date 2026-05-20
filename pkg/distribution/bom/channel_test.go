package bom

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	yamlutil "github.com/labring/sealos/pkg/utils/yaml"
)

func TestDistributionChannelValidate(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		mutate  func(*DistributionChannel)
		wantErr string
	}{
		{
			name: "valid",
		},
		{
			name: "missing line",
			mutate: func(c *DistributionChannel) {
				c.Spec.Line = ""
			},
			wantErr: "spec.line",
		},
		{
			name: "invalid channel",
			mutate: func(c *DistributionChannel) {
				c.Spec.Channel = ReleaseChannel("ga")
			},
			wantErr: "spec.channel",
		},
		{
			name: "missing target revision",
			mutate: func(c *DistributionChannel) {
				c.Spec.TargetRevision = ""
			},
			wantErr: "spec.targetRevision",
		},
		{
			name: "missing bom path",
			mutate: func(c *DistributionChannel) {
				c.Spec.BOMPath = ""
			},
			wantErr: "spec.bomPath",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			doc := NewDistributionChannel("default-platform-beta", "default-platform", ChannelBeta, "rev-20240423", "bom.yaml")
			if tt.mutate != nil {
				tt.mutate(doc)
			}

			err := doc.Validate()
			if tt.wantErr == "" {
				if err != nil {
					t.Fatalf("Validate() error = %v", err)
				}
				return
			}
			if err == nil {
				t.Fatal("Validate() error = nil, want error")
			}
			if !strings.Contains(err.Error(), tt.wantErr) {
				t.Fatalf("Validate() error = %v, want substring %q", err, tt.wantErr)
			}
		})
	}
}

func TestResolveDistributionChannelFile(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	bomPath := filepath.Join(root, "bom.yaml")
	if err := yamlutil.MarshalFile(bomPath, validBOM()); err != nil {
		t.Fatalf("MarshalFile(bom) error = %v", err)
	}
	channelPath := filepath.Join(root, "channel.yaml")
	channel := NewDistributionChannel("default-platform-beta", "default-platform", ChannelBeta, "rev-20240423", "bom.yaml")
	if err := yamlutil.MarshalFile(channelPath, channel); err != nil {
		t.Fatalf("MarshalFile(channel) error = %v", err)
	}

	resolved, err := ResolveDistributionChannelFile(channelPath)
	if err != nil {
		t.Fatalf("ResolveDistributionChannelFile() error = %v", err)
	}
	if got, want := resolved.Channel.Metadata.Name, "default-platform-beta"; got != want {
		t.Fatalf("channel metadata.name = %q, want %q", got, want)
	}
	if got, want := resolved.BOM.Spec.Revision, "rev-20240423"; got != want {
		t.Fatalf("bom spec.revision = %q, want %q", got, want)
	}
	if got, want := resolved.BOMPath, bomPath; got != want {
		t.Fatalf("BOMPath = %q, want %q", got, want)
	}
}

func TestResolveDistributionChannelFileRejectsMismatchedRevision(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	if err := yamlutil.MarshalFile(filepath.Join(root, "bom.yaml"), validBOM()); err != nil {
		t.Fatalf("MarshalFile(bom) error = %v", err)
	}
	channelPath := filepath.Join(root, "channel.yaml")
	channel := NewDistributionChannel("default-platform-beta", "default-platform", ChannelBeta, "rev-missing", "bom.yaml")
	if err := yamlutil.MarshalFile(channelPath, channel); err != nil {
		t.Fatalf("MarshalFile(channel) error = %v", err)
	}

	_, err := ResolveDistributionChannelFile(channelPath)
	if err == nil {
		t.Fatal("ResolveDistributionChannelFile() error = nil, want error")
	}
	if !strings.Contains(err.Error(), "targetRevision") {
		t.Fatalf("ResolveDistributionChannelFile() error = %v, want targetRevision mismatch", err)
	}
}

func TestLoadDistributionChannelFileRejectsMissingFile(t *testing.T) {
	t.Parallel()

	_, err := LoadDistributionChannelFile(filepath.Join(t.TempDir(), "missing.yaml"))
	if err == nil {
		t.Fatal("LoadDistributionChannelFile() error = nil, want error")
	}
}

func TestResolveDistributionChannelFileUsesAbsoluteBOMPath(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	bomPath := filepath.Join(root, "bom.yaml")
	if err := yamlutil.MarshalFile(bomPath, validBOM()); err != nil {
		t.Fatalf("MarshalFile(bom) error = %v", err)
	}
	channelPath := filepath.Join(root, "nested", "channel.yaml")
	if err := os.MkdirAll(filepath.Dir(channelPath), 0o755); err != nil {
		t.Fatalf("MkdirAll() error = %v", err)
	}
	channel := NewDistributionChannel("default-platform-beta", "default-platform", ChannelBeta, "rev-20240423", bomPath)
	if err := yamlutil.MarshalFile(channelPath, channel); err != nil {
		t.Fatalf("MarshalFile(channel) error = %v", err)
	}

	resolved, err := ResolveDistributionChannelFile(channelPath)
	if err != nil {
		t.Fatalf("ResolveDistributionChannelFile() error = %v", err)
	}
	if got, want := resolved.BOMPath, bomPath; got != want {
		t.Fatalf("BOMPath = %q, want %q", got, want)
	}
}
