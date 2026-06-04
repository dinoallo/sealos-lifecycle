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

package bom

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/opencontainers/go-digest"

	yamlutil "github.com/labring/sealos/pkg/utils/yaml"
)

func TestReleaseChannelValidate(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		mutate  func(*ReleaseChannelDocument)
		wantErr string
	}{
		{
			name: "valid",
		},
		{
			name: "metadata name channel",
			mutate: func(c *ReleaseChannelDocument) {
				c.Metadata.Name = string(ChannelBeta)
				c.Spec.Channel = ""
			},
		},
		{
			name: "missing distribution",
			mutate: func(c *ReleaseChannelDocument) {
				c.Spec.Distribution = ""
			},
			wantErr: "spec.distribution",
		},
		{
			name: "unsupported kind rejected",
			mutate: func(c *ReleaseChannelDocument) {
				c.Kind = "UnexpectedKind"
			},
			wantErr: "unsupported kind",
		},
		{
			name: "invalid channel",
			mutate: func(c *ReleaseChannelDocument) {
				c.Spec.Channel = ReleaseChannel("ga")
			},
			wantErr: "spec.channel",
		},
		{
			name: "missing target revision",
			mutate: func(c *ReleaseChannelDocument) {
				c.Spec.TargetRevision = ""
			},
			wantErr: "spec.targetRevision",
		},
		{
			name: "missing bom path",
			mutate: func(c *ReleaseChannelDocument) {
				c.Spec.BOMPath = ""
			},
			wantErr: "spec.bomPath",
		},
		{
			name: "invalid promotion history",
			mutate: func(c *ReleaseChannelDocument) {
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

			doc := NewReleaseChannel("default-platform-beta", "default-platform", ChannelBeta, "rev-20240423", "bom.yaml")
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

func TestDistributionHealthProofValidate(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		mutate  func(*DistributionHealthProof)
		wantErr string
	}{
		{
			name: "valid",
		},
		{
			name: "missing line",
			mutate: func(p *DistributionHealthProof) {
				p.Spec.Line = ""
			},
			wantErr: "spec.line",
		},
		{
			name: "missing target revision",
			mutate: func(p *DistributionHealthProof) {
				p.Spec.TargetRevision = ""
			},
			wantErr: "spec.targetRevision",
		},
		{
			name: "invalid collectedAt",
			mutate: func(p *DistributionHealthProof) {
				p.Spec.CollectedAt = "not-rfc3339"
			},
			wantErr: "spec.collectedAt",
		},
		{
			name: "empty signal name",
			mutate: func(p *DistributionHealthProof) {
				p.Spec.Signals = []DistributionHealthSignal{{Passed: true}}
			},
			wantErr: "spec.signals[0].name",
		},
		{
			name: "empty required signal",
			mutate: func(p *DistributionHealthProof) {
				p.Spec.Thresholds.RequiredSignals = []string{""}
			},
			wantErr: "spec.thresholds.requiredSignals[0]",
		},
		{
			name: "duplicate required signal",
			mutate: func(p *DistributionHealthProof) {
				p.Spec.Thresholds.RequiredSignals = []string{"node-readiness", "node-readiness"}
			},
			wantErr: "spec.thresholds.requiredSignals[1]",
		},
		{
			name: "negative min passed signals",
			mutate: func(p *DistributionHealthProof) {
				p.Spec.Thresholds.MinPassedSignals = -1
			},
			wantErr: "spec.thresholds.minPassedSignals",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			proof := NewDistributionHealthProof("default-platform-rev-20240424", "default-platform", "rev-20240424", true)
			if tt.mutate != nil {
				tt.mutate(proof)
			}

			err := proof.Validate()
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

func TestDistributionHealthProofEvaluate(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name                  string
		proof                 *DistributionHealthProof
		wantPassed            bool
		wantFailedRequired    []string
		wantMissingRequired   []string
		wantPassedSignals     int
		wantMinPassedSignals  int
		wantFailedSignalCount int
	}{
		{
			name: "legacy proof requires every signal to pass",
			proof: func() *DistributionHealthProof {
				proof := NewDistributionHealthProof("default-platform-rev-20240424", "default-platform", "rev-20240424", true)
				proof.Spec.Signals = []DistributionHealthSignal{
					{Name: "node-readiness", Passed: true},
					{Name: "optional-warning", Passed: false},
				}
				return proof
			}(),
			wantPassed:            false,
			wantPassedSignals:     1,
			wantMinPassedSignals:  2,
			wantFailedSignalCount: 1,
		},
		{
			name: "threshold proof allows failed optional signal",
			proof: func() *DistributionHealthProof {
				proof := NewDistributionHealthProof("default-platform-rev-20240424", "default-platform", "rev-20240424", true)
				proof.Spec.Thresholds = DistributionHealthThresholds{
					RequiredSignals:  []string{"node-readiness"},
					MinPassedSignals: 1,
				}
				proof.Spec.Signals = []DistributionHealthSignal{
					{Name: "node-readiness", Passed: true},
					{Name: "optional-warning", Passed: false},
				}
				return proof
			}(),
			wantPassed:            true,
			wantPassedSignals:     1,
			wantMinPassedSignals:  1,
			wantFailedSignalCount: 1,
		},
		{
			name: "threshold proof rejects missing required signal",
			proof: func() *DistributionHealthProof {
				proof := NewDistributionHealthProof("default-platform-rev-20240424", "default-platform", "rev-20240424", true)
				proof.Spec.Thresholds = DistributionHealthThresholds{
					RequiredSignals:  []string{"node-readiness", "runtime-preflight"},
					MinPassedSignals: 1,
				}
				proof.Spec.Signals = []DistributionHealthSignal{
					{Name: "node-readiness", Passed: true},
				}
				return proof
			}(),
			wantPassed:           false,
			wantMissingRequired:  []string{"runtime-preflight"},
			wantPassedSignals:    1,
			wantMinPassedSignals: 1,
		},
		{
			name: "threshold proof rejects minimum passed count",
			proof: func() *DistributionHealthProof {
				proof := NewDistributionHealthProof("default-platform-rev-20240424", "default-platform", "rev-20240424", true)
				proof.Spec.Thresholds = DistributionHealthThresholds{
					RequiredSignals:  []string{"node-readiness"},
					MinPassedSignals: 2,
				}
				proof.Spec.Signals = []DistributionHealthSignal{
					{Name: "node-readiness", Passed: true},
					{Name: "runtime-preflight", Passed: false},
				}
				return proof
			}(),
			wantPassed:            false,
			wantPassedSignals:     1,
			wantMinPassedSignals:  2,
			wantFailedSignalCount: 1,
		},
		{
			name: "signal required flag contributes to required set",
			proof: func() *DistributionHealthProof {
				proof := NewDistributionHealthProof("default-platform-rev-20240424", "default-platform", "rev-20240424", true)
				proof.Spec.Thresholds.MinPassedSignals = 1
				proof.Spec.Signals = []DistributionHealthSignal{
					{Name: "node-readiness", Passed: false, Required: true},
				}
				return proof
			}(),
			wantPassed:            false,
			wantFailedRequired:    []string{"node-readiness"},
			wantMinPassedSignals:  1,
			wantFailedSignalCount: 1,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			evaluation := EvaluateDistributionHealthProof(tt.proof)
			if got, want := evaluation.Passed, tt.wantPassed; got != want {
				t.Fatalf("Passed = %v, want %v; evaluation=%#v", got, want, evaluation)
			}
			if got, want := evaluation.FailedRequiredSignals, tt.wantFailedRequired; strings.Join(got, ",") != strings.Join(want, ",") {
				t.Fatalf("FailedRequiredSignals = %#v, want %#v", got, want)
			}
			if got, want := evaluation.MissingRequiredSignals, tt.wantMissingRequired; strings.Join(got, ",") != strings.Join(want, ",") {
				t.Fatalf("MissingRequiredSignals = %#v, want %#v", got, want)
			}
			if got, want := evaluation.SignalSummary.PassedSignals, tt.wantPassedSignals; got != want {
				t.Fatalf("PassedSignals = %d, want %d", got, want)
			}
			if got, want := evaluation.SignalSummary.MinPassedSignals, tt.wantMinPassedSignals; got != want {
				t.Fatalf("MinPassedSignals = %d, want %d", got, want)
			}
			if got, want := len(evaluation.FailedSignals), tt.wantFailedSignalCount; got != want {
				t.Fatalf("len(FailedSignals) = %d, want %d: %#v", got, want, evaluation.FailedSignals)
			}
		})
	}
}

func TestResolveReleaseChannelFile(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	bomPath := filepath.Join(root, "bom.yaml")
	if err := yamlutil.MarshalFile(bomPath, validBOM()); err != nil {
		t.Fatalf("MarshalFile(bom) error = %v", err)
	}
	channelPath := filepath.Join(root, "channel.yaml")
	channel := NewReleaseChannel("default-platform-beta", "default-platform", ChannelBeta, "rev-20240423", "bom.yaml")
	if err := yamlutil.MarshalFile(channelPath, channel); err != nil {
		t.Fatalf("MarshalFile(channel) error = %v", err)
	}

	resolved, err := ResolveReleaseChannelFile(channelPath)
	if err != nil {
		t.Fatalf("ResolveReleaseChannelFile() error = %v", err)
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

func TestResolveReleaseChannelFileRejectsMismatchedBOMDigest(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	bomPath := filepath.Join(root, "bom.yaml")
	if err := yamlutil.MarshalFile(bomPath, validBOM()); err != nil {
		t.Fatalf("MarshalFile(bom) error = %v", err)
	}
	channelPath := filepath.Join(root, "channel.yaml")
	channel := NewReleaseChannel("default-platform-beta", "default-platform", ChannelBeta, "rev-20240423", "bom.yaml")
	channel.Spec.BOMDigest = "sha256:dddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddd"
	if err := yamlutil.MarshalFile(channelPath, channel); err != nil {
		t.Fatalf("MarshalFile(channel) error = %v", err)
	}

	_, err := ResolveReleaseChannelFile(channelPath)
	if err == nil {
		t.Fatal("ResolveReleaseChannelFile() error = nil, want digest mismatch")
	}
	if !strings.Contains(err.Error(), "BOM digest mismatch") {
		t.Fatalf("ResolveReleaseChannelFile() error = %v, want BOM digest mismatch", err)
	}
}

func TestResolveReleaseChannelLookupFromDirectory(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	bomPath := filepath.Join(root, "boms", "rev-20240423.yaml")
	if err := os.MkdirAll(filepath.Dir(bomPath), 0o755); err != nil {
		t.Fatalf("MkdirAll(boms) error = %v", err)
	}
	if err := yamlutil.MarshalFile(bomPath, validBOM()); err != nil {
		t.Fatalf("MarshalFile(bom) error = %v", err)
	}
	bomData, err := os.ReadFile(bomPath)
	if err != nil {
		t.Fatalf("ReadFile(bom) error = %v", err)
	}
	channelPath := filepath.Join(root, "default-platform", "stable.yaml")
	if err := os.MkdirAll(filepath.Dir(channelPath), 0o755); err != nil {
		t.Fatalf("MkdirAll(channel) error = %v", err)
	}
	channel := NewReleaseChannel("default-platform-stable", "default-platform", ChannelStable, "rev-20240423", "../boms/rev-20240423.yaml")
	channel.Spec.BOMDigest = digest.Canonical.FromBytes(bomData).String()
	if err := yamlutil.MarshalFile(channelPath, channel); err != nil {
		t.Fatalf("MarshalFile(channel) error = %v", err)
	}

	resolved, err := ResolveReleaseChannelLookup(ReleaseChannelLookupOptions{
		DistributionLine: "default-platform",
		Channel:          ChannelStable,
		Source:           root,
	})
	if err != nil {
		t.Fatalf("ResolveReleaseChannelLookup() error = %v", err)
	}
	if got, want := resolved.BOM.Spec.Revision, "rev-20240423"; got != want {
		t.Fatalf("resolved BOM revision = %q, want %q", got, want)
	}
	if got, want := resolved.BOMPath, bomPath; got != want {
		t.Fatalf("resolved BOMPath = %q, want %q", got, want)
	}
	if got, want := resolved.ChannelSource, channelPath; got != want {
		t.Fatalf("resolved ChannelSource = %q, want %q", got, want)
	}
	if got, want := resolved.BOMDigest, channel.Spec.BOMDigest; got != want {
		t.Fatalf("resolved BOMDigest = %q, want %q", got, want)
	}
}

func TestResolveReleaseChannelLookupFromHTTP(t *testing.T) {
	t.Parallel()

	bomDoc := validBOM()
	bomData := []byte(bomDoc.String())
	bomDigest := digest.Canonical.FromBytes(bomData).String()
	channel := NewReleaseChannel("default-platform-stable", "default-platform", ChannelStable, "rev-20240423", "/boms/rev-20240423.yaml")
	channel.Spec.BOMDigest = bomDigest

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v1/distributions/default-platform/channels/stable":
			_, _ = w.Write([]byte(channel.String()))
		case "/boms/rev-20240423.yaml":
			_, _ = w.Write(bomData)
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(server.Close)

	resolved, err := ResolveReleaseChannelLookup(ReleaseChannelLookupOptions{
		DistributionLine: "default-platform",
		Channel:          ChannelStable,
		Source:           server.URL,
	})
	if err != nil {
		t.Fatalf("ResolveReleaseChannelLookup() error = %v", err)
	}
	if got, want := resolved.BOM.Spec.Revision, "rev-20240423"; got != want {
		t.Fatalf("resolved BOM revision = %q, want %q", got, want)
	}
	if got, want := resolved.BOMPath, server.URL+"/boms/rev-20240423.yaml"; got != want {
		t.Fatalf("resolved BOMPath = %q, want %q", got, want)
	}
	if got, want := resolved.ChannelSource, server.URL+"/v1/distributions/default-platform/channels/stable"; got != want {
		t.Fatalf("resolved ChannelSource = %q, want %q", got, want)
	}
}

func TestReleaseMetadataServiceServesChannelLookup(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	bomPath := filepath.Join(root, "releases", "default-platform", "rev-20240423", "bom.yaml")
	if err := os.MkdirAll(filepath.Dir(bomPath), 0o755); err != nil {
		t.Fatalf("MkdirAll(bom) error = %v", err)
	}
	if err := yamlutil.MarshalFile(bomPath, validBOM()); err != nil {
		t.Fatalf("MarshalFile(bom) error = %v", err)
	}
	bomData, err := os.ReadFile(bomPath)
	if err != nil {
		t.Fatalf("ReadFile(bom) error = %v", err)
	}
	channelPath := filepath.Join(root, "channels", "default-platform", "stable.yaml")
	if err := os.MkdirAll(filepath.Dir(channelPath), 0o755); err != nil {
		t.Fatalf("MkdirAll(channel) error = %v", err)
	}
	channel := NewReleaseChannel("default-platform-stable", "default-platform", ChannelStable, "rev-20240423", "../../releases/default-platform/rev-20240423/bom.yaml")
	channel.Spec.BOMDigest = digest.Canonical.FromBytes(bomData).String()
	if err := yamlutil.MarshalFile(channelPath, channel); err != nil {
		t.Fatalf("MarshalFile(channel) error = %v", err)
	}

	handler, err := NewReleaseMetadataHandler(root)
	if err != nil {
		t.Fatalf("NewReleaseMetadataHandler() error = %v", err)
	}
	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)

	resolved, err := ResolveReleaseChannelLookup(ReleaseChannelLookupOptions{
		DistributionLine: "default-platform",
		Channel:          ChannelStable,
		Source:           server.URL,
	})
	if err != nil {
		t.Fatalf("ResolveReleaseChannelLookup() error = %v", err)
	}
	if got, want := resolved.ChannelSource, server.URL+"/v1/distributions/default-platform/channels/stable"; got != want {
		t.Fatalf("resolved ChannelSource = %q, want %q", got, want)
	}
	if got, want := resolved.BOMPath, server.URL+"/v1/distributions/default-platform/revisions/rev-20240423/bom"; got != want {
		t.Fatalf("resolved BOMPath = %q, want %q", got, want)
	}
	if got, want := resolved.BOMDigest, channel.Spec.BOMDigest; got != want {
		t.Fatalf("resolved BOMDigest = %q, want %q", got, want)
	}
	if got, want := resolved.BOM.Spec.Revision, "rev-20240423"; got != want {
		t.Fatalf("resolved BOM revision = %q, want %q", got, want)
	}
}

func TestReleaseMetadataServicePromotesWithHealthProof(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	oldBOM := validBOM()
	newBOM := validBOM()
	newBOM.Spec.Revision = "rev-20240424"
	oldBOMPath := filepath.Join(root, "releases", "default-platform", oldBOM.Spec.Revision, "bom.yaml")
	newBOMPath := filepath.Join(root, "releases", "default-platform", newBOM.Spec.Revision, "bom.yaml")
	if err := os.MkdirAll(filepath.Dir(oldBOMPath), 0o755); err != nil {
		t.Fatalf("MkdirAll(oldBOM) error = %v", err)
	}
	if err := os.MkdirAll(filepath.Dir(newBOMPath), 0o755); err != nil {
		t.Fatalf("MkdirAll(newBOM) error = %v", err)
	}
	if err := yamlutil.MarshalFile(oldBOMPath, oldBOM); err != nil {
		t.Fatalf("MarshalFile(oldBOM) error = %v", err)
	}
	if err := yamlutil.MarshalFile(newBOMPath, newBOM); err != nil {
		t.Fatalf("MarshalFile(newBOM) error = %v", err)
	}
	oldBOMData, err := os.ReadFile(oldBOMPath)
	if err != nil {
		t.Fatalf("ReadFile(oldBOM) error = %v", err)
	}
	channelPath := filepath.Join(root, "channels", "default-platform", "stable.yaml")
	if err := os.MkdirAll(filepath.Dir(channelPath), 0o755); err != nil {
		t.Fatalf("MkdirAll(channel) error = %v", err)
	}
	channel := NewReleaseChannel("default-platform-stable", "default-platform", ChannelStable, oldBOM.Spec.Revision, "../../releases/default-platform/"+oldBOM.Spec.Revision+"/bom.yaml")
	channel.Spec.BOMDigest = digest.Canonical.FromBytes(oldBOMData).String()
	if err := yamlutil.MarshalFile(channelPath, channel); err != nil {
		t.Fatalf("MarshalFile(channel) error = %v", err)
	}
	handler, err := NewReleaseMetadataHandler(root)
	if err != nil {
		t.Fatalf("NewReleaseMetadataHandler() error = %v", err)
	}
	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)

	body := []byte(`targetRevision: rev-20240424
sourceChannel: beta
reason: beta cohort passed
approvedBy: release-service
approvedAt: "2024-04-24T10:30:00Z"
healthProof:
  apiVersion: distribution.sealos.io/v1alpha1
  kind: DistributionHealthProof
  metadata:
    name: default-platform-rev-20240424
  spec:
    line: default-platform
    targetRevision: rev-20240424
    passed: true
    summary: beta cohort passed
    signals:
      - name: node-readiness
        passed: true
`)
	resp, err := http.Post(server.URL+"/v1/distributions/default-platform/channels/stable/promotions", "application/yaml", strings.NewReader(string(body)))
	if err != nil {
		t.Fatalf("Post(promotion) error = %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("promotion status = %s, want 200", resp.Status)
	}

	loaded, err := LoadReleaseChannelFile(channelPath)
	if err != nil {
		t.Fatalf("LoadReleaseChannelFile(promoted) error = %v", err)
	}
	if got, want := loaded.Spec.TargetRevision, "rev-20240424"; got != want {
		t.Fatalf("promoted targetRevision = %q, want %q", got, want)
	}
	if got := loaded.Spec.BOMDigest; !strings.HasPrefix(got, "sha256:") || got == channel.Spec.BOMDigest {
		t.Fatalf("promoted bomDigest = %q, want new sha256 digest", got)
	}
	if got, want := len(loaded.Spec.PromotionHistory), 1; got != want {
		t.Fatalf("len(promotionHistory) = %d, want %d", got, want)
	}
	entry := loaded.Spec.PromotionHistory[0]
	if got, want := entry.HealthProofSummary, "beta cohort passed"; got != want {
		t.Fatalf("promotion healthProofSummary = %q, want %q", got, want)
	}
	if !strings.HasPrefix(entry.HealthProofDigest, "sha256:") {
		t.Fatalf("promotion healthProofDigest = %q, want sha256 digest", entry.HealthProofDigest)
	}
	if _, err := os.Stat(filepath.Join(root, "proofs", "default-platform", "rev-20240424", "default-platform-rev-20240424.yaml")); err != nil {
		t.Fatalf("health proof file missing: %v", err)
	}

	resolved, err := ResolveReleaseChannelLookup(ReleaseChannelLookupOptions{
		DistributionLine: "default-platform",
		Channel:          ChannelStable,
		Source:           server.URL,
	})
	if err != nil {
		t.Fatalf("ResolveReleaseChannelLookup(promoted) error = %v", err)
	}
	if got, want := resolved.BOM.Spec.Revision, "rev-20240424"; got != want {
		t.Fatalf("resolved promoted BOM revision = %q, want %q", got, want)
	}
}

func TestResolveReleaseChannelLookupRequiresDigestPinnedBOM(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	bomPath := filepath.Join(root, "boms", "rev-20240423.yaml")
	if err := os.MkdirAll(filepath.Dir(bomPath), 0o755); err != nil {
		t.Fatalf("MkdirAll(boms) error = %v", err)
	}
	if err := yamlutil.MarshalFile(bomPath, validBOM()); err != nil {
		t.Fatalf("MarshalFile(bom) error = %v", err)
	}
	channelPath := filepath.Join(root, "default-platform", "stable.yaml")
	if err := os.MkdirAll(filepath.Dir(channelPath), 0o755); err != nil {
		t.Fatalf("MkdirAll(channel) error = %v", err)
	}
	channel := NewReleaseChannel("default-platform-stable", "default-platform", ChannelStable, "rev-20240423", "../boms/rev-20240423.yaml")
	if err := yamlutil.MarshalFile(channelPath, channel); err != nil {
		t.Fatalf("MarshalFile(channel) error = %v", err)
	}

	_, err := ResolveReleaseChannelLookup(ReleaseChannelLookupOptions{
		DistributionLine: "default-platform",
		Channel:          ChannelStable,
		Source:           root,
	})
	if err == nil {
		t.Fatal("ResolveReleaseChannelLookup() error = nil, want digest-pinned error")
	}
	if !strings.Contains(err.Error(), "digest-pinned BOM") {
		t.Fatalf("ResolveReleaseChannelLookup() error = %v, want digest-pinned BOM", err)
	}
}

func TestResolveReleaseChannelFileRejectsMismatchedRevision(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	if err := yamlutil.MarshalFile(filepath.Join(root, "bom.yaml"), validBOM()); err != nil {
		t.Fatalf("MarshalFile(bom) error = %v", err)
	}
	channelPath := filepath.Join(root, "channel.yaml")
	channel := NewReleaseChannel("default-platform-beta", "default-platform", ChannelBeta, "rev-missing", "bom.yaml")
	if err := yamlutil.MarshalFile(channelPath, channel); err != nil {
		t.Fatalf("MarshalFile(channel) error = %v", err)
	}

	_, err := ResolveReleaseChannelFile(channelPath)
	if err == nil {
		t.Fatal("ResolveReleaseChannelFile() error = nil, want error")
	}
	if !strings.Contains(err.Error(), "targetRevision") {
		t.Fatalf("ResolveReleaseChannelFile() error = %v, want targetRevision mismatch", err)
	}
}

func TestLoadReleaseChannelFileRejectsMissingFile(t *testing.T) {
	t.Parallel()

	_, err := LoadReleaseChannelFile(filepath.Join(t.TempDir(), "missing.yaml"))
	if err == nil {
		t.Fatal("LoadReleaseChannelFile() error = nil, want error")
	}
}

func TestResolveReleaseChannelFileUsesAbsoluteBOMPath(t *testing.T) {
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
	channel := NewReleaseChannel("default-platform-beta", "default-platform", ChannelBeta, "rev-20240423", bomPath)
	if err := yamlutil.MarshalFile(channelPath, channel); err != nil {
		t.Fatalf("MarshalFile(channel) error = %v", err)
	}

	resolved, err := ResolveReleaseChannelFile(channelPath)
	if err != nil {
		t.Fatalf("ResolveReleaseChannelFile() error = %v", err)
	}
	if got, want := resolved.BOMPath, bomPath; got != want {
		t.Fatalf("BOMPath = %q, want %q", got, want)
	}
}

func TestPromoteReleaseChannelFile(t *testing.T) {
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
	newBOMData, err := os.ReadFile(newBOMPath)
	if err != nil {
		t.Fatalf("ReadFile(newBOM) error = %v", err)
	}
	newBOMDigest := digest.Canonical.FromBytes(newBOMData).String()
	healthProofPath := filepath.Join(root, "proofs", "rev-20240424-health.yaml")
	healthProof := NewDistributionHealthProof("default-platform-rev-20240424", newBOM.Metadata.Name, newBOM.Spec.Revision, true)
	healthProof.Spec.Summary = "beta cohort passed"
	healthProof.Spec.CollectedAt = "2024-04-24T09:30:00Z"
	healthProof.Spec.Signals = []DistributionHealthSignal{
		{Name: "reconcile", Passed: true, Message: "all canaries ready"},
		{Name: "node-readiness", Passed: true},
	}
	if err := yamlutil.MarshalFile(healthProofPath, healthProof); err != nil {
		t.Fatalf("MarshalFile(healthProof) error = %v", err)
	}
	channel := NewReleaseChannel("default-platform-stable", "default-platform", ChannelStable, oldBOM.Spec.Revision, "../boms/rev-20240423.yaml")
	if err := yamlutil.MarshalFile(channelPath, channel); err != nil {
		t.Fatalf("MarshalFile(channel) error = %v", err)
	}

	approvedAt := time.Date(2024, 4, 24, 10, 30, 0, 0, time.UTC)
	result, err := PromoteReleaseChannelFile(PromoteReleaseChannelOptions{
		ChannelPath:     channelPath,
		TargetBOMPath:   newBOMPath,
		HealthProofPath: healthProofPath,
		Reason:          "beta cohort passed",
		ApprovedBy:      "release-team",
		ApprovedAt:      approvedAt,
	})
	if err != nil {
		t.Fatalf("PromoteReleaseChannelFile() error = %v", err)
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
	if got, want := result.Channel.Spec.BOMDigest, newBOMDigest; got != want {
		t.Fatalf("channel spec.bomDigest = %q, want %q", got, want)
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
	if got, want := entry.HealthProofPath, "../proofs/rev-20240424-health.yaml"; got != want {
		t.Fatalf("promotionHistory[0].healthProofPath = %q, want %q", got, want)
	}
	if got, want := entry.HealthProofSummary, "beta cohort passed"; got != want {
		t.Fatalf("promotionHistory[0].healthProofSummary = %q, want %q", got, want)
	}
	if !strings.HasPrefix(entry.HealthProofDigest, "sha256:") {
		t.Fatalf("promotionHistory[0].healthProofDigest = %q, want sha256 digest", entry.HealthProofDigest)
	}
	if result.HealthProof == nil {
		t.Fatal("HealthProof = nil, want loaded proof")
	}
	if got, want := result.HealthProof.Spec.TargetRevision, "rev-20240424"; got != want {
		t.Fatalf("HealthProof targetRevision = %q, want %q", got, want)
	}

	resolved, err := ResolveReleaseChannelFile(channelPath)
	if err != nil {
		t.Fatalf("ResolveReleaseChannelFile(promoted) error = %v", err)
	}
	if got, want := resolved.BOM.Spec.Revision, "rev-20240424"; got != want {
		t.Fatalf("resolved BOM revision = %q, want %q", got, want)
	}
	if got, want := resolved.BOMDigest, newBOMDigest; got != want {
		t.Fatalf("resolved BOM digest = %q, want %q", got, want)
	}
	if got, want := resolved.BOM.Channel(), ChannelStable; got != want {
		t.Fatalf("resolved BOM channel = %q, want %q", got, want)
	}
}

func TestPromoteReleaseChannelFileRejectsFailedHealthProof(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	channelPath := filepath.Join(root, "channels", "stable.yaml")
	targetBOMPath := filepath.Join(root, "boms", "rev-20240424.yaml")
	healthProofPath := filepath.Join(root, "proofs", "rev-20240424-health.yaml")
	targetBOM := validBOM()
	targetBOM.Spec.Revision = "rev-20240424"
	if err := yamlutil.MarshalFile(targetBOMPath, targetBOM); err != nil {
		t.Fatalf("MarshalFile(targetBOM) error = %v", err)
	}
	channel := NewReleaseChannel("default-platform-stable", "default-platform", ChannelStable, "rev-20240423", "../boms/rev-20240423.yaml")
	if err := yamlutil.MarshalFile(channelPath, channel); err != nil {
		t.Fatalf("MarshalFile(channel) error = %v", err)
	}
	healthProof := NewDistributionHealthProof("default-platform-rev-20240424", targetBOM.Metadata.Name, targetBOM.Spec.Revision, true)
	healthProof.Spec.Signals = []DistributionHealthSignal{
		{Name: "node-readiness", Passed: false, Message: "one canary node not ready"},
	}
	if err := yamlutil.MarshalFile(healthProofPath, healthProof); err != nil {
		t.Fatalf("MarshalFile(healthProof) error = %v", err)
	}

	_, err := PromoteReleaseChannelFile(PromoteReleaseChannelOptions{
		ChannelPath:     channelPath,
		TargetBOMPath:   targetBOMPath,
		HealthProofPath: healthProofPath,
		Reason:          "passed canary",
		ApprovedBy:      "release-team",
	})
	if err == nil {
		t.Fatal("PromoteReleaseChannelFile() error = nil, want failed health proof error")
	}
	if !strings.Contains(err.Error(), "failed signal(s): node-readiness") {
		t.Fatalf("PromoteReleaseChannelFile() error = %v, want failed signal error", err)
	}
}

func TestPromoteReleaseChannelFileRejectsEmptyHealthProofSignals(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	channelPath := filepath.Join(root, "channels", "stable.yaml")
	targetBOMPath := filepath.Join(root, "boms", "rev-20240424.yaml")
	healthProofPath := filepath.Join(root, "proofs", "rev-20240424-health.yaml")
	targetBOM := validBOM()
	targetBOM.Spec.Revision = "rev-20240424"
	if err := yamlutil.MarshalFile(targetBOMPath, targetBOM); err != nil {
		t.Fatalf("MarshalFile(targetBOM) error = %v", err)
	}
	channel := NewReleaseChannel("default-platform-stable", "default-platform", ChannelStable, "rev-20240423", "../boms/rev-20240423.yaml")
	if err := yamlutil.MarshalFile(channelPath, channel); err != nil {
		t.Fatalf("MarshalFile(channel) error = %v", err)
	}
	healthProof := NewDistributionHealthProof("default-platform-rev-20240424", targetBOM.Metadata.Name, targetBOM.Spec.Revision, true)
	if err := yamlutil.MarshalFile(healthProofPath, healthProof); err != nil {
		t.Fatalf("MarshalFile(healthProof) error = %v", err)
	}

	_, err := PromoteReleaseChannelFile(PromoteReleaseChannelOptions{
		ChannelPath:     channelPath,
		TargetBOMPath:   targetBOMPath,
		HealthProofPath: healthProofPath,
		Reason:          "passed canary",
		ApprovedBy:      "release-team",
	})
	if err == nil {
		t.Fatal("PromoteReleaseChannelFile() error = nil, want empty health signal error")
	}
	if !strings.Contains(err.Error(), "has no health signals") {
		t.Fatalf("PromoteReleaseChannelFile() error = %v, want empty health signal error", err)
	}
}

func TestPromoteReleaseChannelFileRejectsMissingRequiredHealthProofSignal(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	channelPath := filepath.Join(root, "channels", "stable.yaml")
	targetBOMPath := filepath.Join(root, "boms", "rev-20240424.yaml")
	healthProofPath := filepath.Join(root, "proofs", "rev-20240424-health.yaml")
	targetBOM := validBOM()
	targetBOM.Spec.Revision = "rev-20240424"
	if err := yamlutil.MarshalFile(targetBOMPath, targetBOM); err != nil {
		t.Fatalf("MarshalFile(targetBOM) error = %v", err)
	}
	channel := NewReleaseChannel("default-platform-stable", "default-platform", ChannelStable, "rev-20240423", "../boms/rev-20240423.yaml")
	if err := yamlutil.MarshalFile(channelPath, channel); err != nil {
		t.Fatalf("MarshalFile(channel) error = %v", err)
	}
	healthProof := NewDistributionHealthProof("default-platform-rev-20240424", targetBOM.Metadata.Name, targetBOM.Spec.Revision, true)
	healthProof.Spec.Thresholds = DistributionHealthThresholds{
		RequiredSignals:  []string{"node-readiness", "runtime-preflight"},
		MinPassedSignals: 1,
	}
	healthProof.Spec.Signals = []DistributionHealthSignal{
		{Name: "node-readiness", Passed: true},
	}
	if err := yamlutil.MarshalFile(healthProofPath, healthProof); err != nil {
		t.Fatalf("MarshalFile(healthProof) error = %v", err)
	}

	_, err := PromoteReleaseChannelFile(PromoteReleaseChannelOptions{
		ChannelPath:     channelPath,
		TargetBOMPath:   targetBOMPath,
		HealthProofPath: healthProofPath,
		Reason:          "passed canary",
		ApprovedBy:      "release-team",
	})
	if err == nil {
		t.Fatal("PromoteReleaseChannelFile() error = nil, want missing required signal error")
	}
	if !strings.Contains(err.Error(), "missing required signal(s): runtime-preflight") {
		t.Fatalf("PromoteReleaseChannelFile() error = %v, want missing required signal error", err)
	}
}

func TestPromoteReleaseChannelFileRejectsMissingHealthProofForStable(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	channelPath := filepath.Join(root, "channels", "stable.yaml")
	targetBOMPath := filepath.Join(root, "boms", "rev-20240424.yaml")
	targetBOM := validBOM()
	targetBOM.Spec.Revision = "rev-20240424"
	if err := yamlutil.MarshalFile(targetBOMPath, targetBOM); err != nil {
		t.Fatalf("MarshalFile(targetBOM) error = %v", err)
	}
	channel := NewReleaseChannel("default-platform-stable", "default-platform", ChannelStable, "rev-20240423", "../boms/rev-20240423.yaml")
	if err := yamlutil.MarshalFile(channelPath, channel); err != nil {
		t.Fatalf("MarshalFile(channel) error = %v", err)
	}

	_, err := PromoteReleaseChannelFile(PromoteReleaseChannelOptions{
		ChannelPath:   channelPath,
		TargetBOMPath: targetBOMPath,
		Reason:        "passed canary",
		ApprovedBy:    "release-team",
	})
	if err == nil {
		t.Fatal("PromoteReleaseChannelFile() error = nil, want missing health proof policy error")
	}
	if !strings.Contains(err.Error(), "healthProofRequired") {
		t.Fatalf("PromoteReleaseChannelFile() error = %v, want missing proof policy violation", err)
	}

	loaded, loadErr := LoadReleaseChannelFile(channelPath)
	if loadErr != nil {
		t.Fatalf("LoadReleaseChannelFile() error = %v", loadErr)
	}
	if got, want := loaded.Spec.TargetRevision, "rev-20240423"; got != want {
		t.Fatalf("targetRevision after blocked promotion = %q, want %q", got, want)
	}
	if got, want := len(loaded.Spec.PromotionHistory), 0; got != want {
		t.Fatalf("len(promotionHistory) after blocked promotion = %d, want %d", got, want)
	}
}

func TestPromoteReleaseChannelFileRejectsAlphaCandidateForStable(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	channelPath := filepath.Join(root, "channels", "stable.yaml")
	targetBOMPath := filepath.Join(root, "boms", "rev-20240424.yaml")
	healthProofPath := filepath.Join(root, "proofs", "rev-20240424-health.yaml")
	targetBOM := validBOM()
	targetBOM.Spec.Revision = "rev-20240424"
	if err := yamlutil.MarshalFile(targetBOMPath, targetBOM); err != nil {
		t.Fatalf("MarshalFile(targetBOM) error = %v", err)
	}
	channel := NewReleaseChannel("default-platform-stable", "default-platform", ChannelStable, "rev-20240423", "../boms/rev-20240423.yaml")
	if err := yamlutil.MarshalFile(channelPath, channel); err != nil {
		t.Fatalf("MarshalFile(channel) error = %v", err)
	}
	healthProof := NewDistributionHealthProof("default-platform-rev-20240424", targetBOM.Metadata.Name, targetBOM.Spec.Revision, true)
	healthProof.Spec.Signals = []DistributionHealthSignal{{Name: "node-readiness", Passed: true}}
	if err := yamlutil.MarshalFile(healthProofPath, healthProof); err != nil {
		t.Fatalf("MarshalFile(healthProof) error = %v", err)
	}

	_, err := PromoteReleaseChannelFile(PromoteReleaseChannelOptions{
		ChannelPath:     channelPath,
		TargetBOMPath:   targetBOMPath,
		SourceChannel:   ChannelAlpha,
		HealthProofPath: healthProofPath,
		Reason:          "passed canary",
		ApprovedBy:      "release-team",
	})
	if err == nil {
		t.Fatal("PromoteReleaseChannelFile() error = nil, want source channel policy error")
	}
	if !strings.Contains(err.Error(), "sourceChannelBlocked") {
		t.Fatalf("PromoteReleaseChannelFile() error = %v, want source channel policy violation", err)
	}
}

func TestPromoteReleaseChannelFileRejectsMismatchedLine(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	channelPath := filepath.Join(root, "channel.yaml")
	channel := NewReleaseChannel("default-platform-stable", "default-platform", ChannelStable, "rev-20240423", "bom.yaml")
	if err := yamlutil.MarshalFile(channelPath, channel); err != nil {
		t.Fatalf("MarshalFile(channel) error = %v", err)
	}
	targetBOM := validBOM()
	targetBOM.Metadata.Name = "other-platform"
	targetBOMPath := filepath.Join(root, "other-bom.yaml")
	if err := yamlutil.MarshalFile(targetBOMPath, targetBOM); err != nil {
		t.Fatalf("MarshalFile(targetBOM) error = %v", err)
	}

	_, err := PromoteReleaseChannelFile(PromoteReleaseChannelOptions{
		ChannelPath:   channelPath,
		TargetBOMPath: targetBOMPath,
		Reason:        "passed canary",
		ApprovedBy:    "release-team",
	})
	if err == nil {
		t.Fatal("PromoteReleaseChannelFile() error = nil, want mismatch")
	}
	if !strings.Contains(err.Error(), "does not match target BOM metadata.name") {
		t.Fatalf("PromoteReleaseChannelFile() error = %v, want line mismatch", err)
	}
}
