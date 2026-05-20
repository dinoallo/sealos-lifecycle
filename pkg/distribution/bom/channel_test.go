package bom

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

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
		{
			name: "invalid promotion history",
			mutate: func(c *DistributionChannel) {
				c.Spec.PromotionHistory = []DistributionPromotionRef{
					{
						ToRevision: "rev-20240424",
						BOMPath:    "bom.yaml",
						Reason:     "passed beta",
						ApprovedBy: "release-team",
						ApprovedAt: "not-rfc3339",
					},
				}
			},
			wantErr: "spec.promotionHistory[0]",
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

func TestPromoteDistributionChannelFile(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	channelPath := filepath.Join(root, "channels", "stable.yaml")
	if err := os.MkdirAll(filepath.Dir(channelPath), 0o755); err != nil {
		t.Fatalf("MkdirAll(channel dir) error = %v", err)
	}
	oldBOMPath := filepath.Join(root, "boms", "rev-20240423.yaml")
	newBOMPath := filepath.Join(root, "boms", "rev-20240424.yaml")
	if err := os.MkdirAll(filepath.Dir(newBOMPath), 0o755); err != nil {
		t.Fatalf("MkdirAll(bom dir) error = %v", err)
	}
	oldBOM := validBOM()
	if err := yamlutil.MarshalFile(oldBOMPath, oldBOM); err != nil {
		t.Fatalf("MarshalFile(oldBOM) error = %v", err)
	}
	newBOM := validBOM()
	newBOM.Spec.Revision = "rev-20240424"
	if err := yamlutil.MarshalFile(newBOMPath, newBOM); err != nil {
		t.Fatalf("MarshalFile(newBOM) error = %v", err)
	}
	channel := NewDistributionChannel("default-platform-stable", "default-platform", ChannelStable, oldBOM.Spec.Revision, "../boms/rev-20240423.yaml")
	if err := yamlutil.MarshalFile(channelPath, channel); err != nil {
		t.Fatalf("MarshalFile(channel) error = %v", err)
	}

	approvedAt := time.Date(2024, 4, 24, 10, 30, 0, 0, time.UTC)
	result, err := PromoteDistributionChannelFile(PromoteDistributionChannelOptions{
		ChannelPath:   channelPath,
		TargetBOMPath: newBOMPath,
		Reason:        "beta cohort passed",
		ApprovedBy:    "release-team",
		ApprovedAt:    approvedAt,
	})
	if err != nil {
		t.Fatalf("PromoteDistributionChannelFile() error = %v", err)
	}

	if !result.Changed {
		t.Fatal("Changed = false, want true")
	}
	if got, want := result.FromRevision, "rev-20240423"; got != want {
		t.Fatalf("FromRevision = %q, want %q", got, want)
	}
	if got, want := result.ToRevision, "rev-20240424"; got != want {
		t.Fatalf("ToRevision = %q, want %q", got, want)
	}
	if got, want := result.Channel.Spec.BOMPath, "../boms/rev-20240424.yaml"; got != want {
		t.Fatalf("channel spec.bomPath = %q, want %q", got, want)
	}
	if got, want := result.Channel.Spec.TargetRevision, "rev-20240424"; got != want {
		t.Fatalf("channel spec.targetRevision = %q, want %q", got, want)
	}
	if got, want := len(result.Channel.Spec.PromotionHistory), 1; got != want {
		t.Fatalf("len(promotionHistory) = %d, want %d", got, want)
	}
	entry := result.Channel.Spec.PromotionHistory[0]
	if got, want := result.Promotion, entry; got != want {
		t.Fatalf("Promotion = %#v, want %#v", got, want)
	}
	if got, want := entry.FromRevision, "rev-20240423"; got != want {
		t.Fatalf("promotionHistory[0].fromRevision = %q, want %q", got, want)
	}
	if got, want := entry.ToRevision, "rev-20240424"; got != want {
		t.Fatalf("promotionHistory[0].toRevision = %q, want %q", got, want)
	}
	if got, want := entry.BOMPath, "../boms/rev-20240424.yaml"; got != want {
		t.Fatalf("promotionHistory[0].bomPath = %q, want %q", got, want)
	}
	if got, want := entry.Reason, "beta cohort passed"; got != want {
		t.Fatalf("promotionHistory[0].reason = %q, want %q", got, want)
	}
	if got, want := entry.ApprovedBy, "release-team"; got != want {
		t.Fatalf("promotionHistory[0].approvedBy = %q, want %q", got, want)
	}
	if got, want := entry.ApprovedAt, "2024-04-24T10:30:00Z"; got != want {
		t.Fatalf("promotionHistory[0].approvedAt = %q, want %q", got, want)
	}

	resolved, err := ResolveDistributionChannelFile(channelPath)
	if err != nil {
		t.Fatalf("ResolveDistributionChannelFile(promoted) error = %v", err)
	}
	if got, want := resolved.BOM.Spec.Revision, "rev-20240424"; got != want {
		t.Fatalf("resolved BOM revision = %q, want %q", got, want)
	}
	if got, want := resolved.BOM.Spec.Channel, ChannelStable; got != want {
		t.Fatalf("resolved BOM channel = %q, want %q", got, want)
	}
}

func TestPromoteDistributionChannelFileRejectsMismatchedLine(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	channelPath := filepath.Join(root, "channel.yaml")
	channel := NewDistributionChannel("default-platform-stable", "default-platform", ChannelStable, "rev-20240423", "bom.yaml")
	if err := yamlutil.MarshalFile(channelPath, channel); err != nil {
		t.Fatalf("MarshalFile(channel) error = %v", err)
	}
	targetBOM := validBOM()
	targetBOM.Metadata.Name = "other-platform"
	targetBOMPath := filepath.Join(root, "other-bom.yaml")
	if err := yamlutil.MarshalFile(targetBOMPath, targetBOM); err != nil {
		t.Fatalf("MarshalFile(targetBOM) error = %v", err)
	}

	_, err := PromoteDistributionChannelFile(PromoteDistributionChannelOptions{
		ChannelPath:   channelPath,
		TargetBOMPath: targetBOMPath,
		Reason:        "passed canary",
		ApprovedBy:    "release-team",
	})
	if err == nil {
		t.Fatal("PromoteDistributionChannelFile() error = nil, want mismatch")
	}
	if !strings.Contains(err.Error(), "does not match target BOM metadata.name") {
		t.Fatalf("PromoteDistributionChannelFile() error = %v, want line mismatch", err)
	}
}
