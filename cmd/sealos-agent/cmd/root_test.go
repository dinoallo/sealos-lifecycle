package cmd

import (
	"path/filepath"
	"testing"

	"github.com/labring/sealos/pkg/distribution/agent"
)

func TestTargetOptions(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		bom     string
		channel string
		want    agent.TargetOptions
		wantErr bool
	}{
		{
			name: "bom",
			bom:  "bom.yaml",
			want: agent.TargetOptions{BOMPath: "bom.yaml"},
		},
		{
			name:    "channel",
			channel: "channel.yaml",
			want:    agent.TargetOptions{DistributionChannelPath: "channel.yaml"},
		},
		{
			name:    "missing",
			wantErr: true,
		},
		{
			name:    "ambiguous",
			bom:     "bom.yaml",
			channel: "channel.yaml",
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got, err := targetOptions(tt.bom, tt.channel)
			if tt.wantErr {
				if err == nil {
					t.Fatal("targetOptions() error = nil, want error")
				}
				return
			}
			if err != nil {
				t.Fatalf("targetOptions() error = %v", err)
			}
			if got != tt.want {
				t.Fatalf("targetOptions() = %#v, want %#v", got, tt.want)
			}
		})
	}
}

func TestParsePackageSources(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	source := filepath.Join(root, "runtime")
	got, err := parsePackageSources([]string{"runtime=" + source})
	if err != nil {
		t.Fatalf("parsePackageSources() error = %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("len(sources) = %d, want 1", len(got))
	}
	if got[0].Component != "runtime" {
		t.Fatalf("component = %q, want runtime", got[0].Component)
	}
	if want, err := filepath.Abs(source); err != nil {
		t.Fatalf("Abs(source) error = %v", err)
	} else if got[0].Root != want {
		t.Fatalf("root = %q, want %q", got[0].Root, want)
	}

	if _, err := parsePackageSources([]string{"runtime=" + source, "runtime=" + source}); err == nil {
		t.Fatal("duplicate package source error = nil, want error")
	}
	if _, err := parsePackageSources([]string{"invalid"}); err == nil {
		t.Fatal("invalid package source error = nil, want error")
	}
}
