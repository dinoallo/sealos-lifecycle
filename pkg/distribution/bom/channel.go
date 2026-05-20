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

package bom

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/opencontainers/go-digest"
	"sigs.k8s.io/yaml"

	"github.com/labring/sealos/pkg/distribution"
	promotionpolicy "github.com/labring/sealos/pkg/distribution/promotion"
	fileutil "github.com/labring/sealos/pkg/utils/file"
	yamlutil "github.com/labring/sealos/pkg/utils/yaml"
)

type DistributionChannelSpec struct {
	Line             string                     `json:"line" yaml:"line"`
	Channel          ReleaseChannel             `json:"channel" yaml:"channel"`
	TargetRevision   string                     `json:"targetRevision" yaml:"targetRevision"`
	BOMPath          string                     `json:"bomPath" yaml:"bomPath"`
	PromotionHistory []DistributionPromotionRef `json:"promotionHistory,omitempty" yaml:"promotionHistory,omitempty"`
}

type DistributionPromotionRef struct {
	FromRevision       string `json:"fromRevision,omitempty" yaml:"fromRevision,omitempty"`
	ToRevision         string `json:"toRevision" yaml:"toRevision"`
	BOMPath            string `json:"bomPath" yaml:"bomPath"`
	Reason             string `json:"reason" yaml:"reason"`
	ApprovedBy         string `json:"approvedBy" yaml:"approvedBy"`
	ApprovedAt         string `json:"approvedAt" yaml:"approvedAt"`
	HealthProofPath    string `json:"healthProofPath,omitempty" yaml:"healthProofPath,omitempty"`
	HealthProofDigest  string `json:"healthProofDigest,omitempty" yaml:"healthProofDigest,omitempty"`
	HealthProofSummary string `json:"healthProofSummary,omitempty" yaml:"healthProofSummary,omitempty"`
}

type DistributionChannel struct {
	APIVersion string                  `json:"apiVersion" yaml:"apiVersion"`
	Kind       string                  `json:"kind" yaml:"kind"`
	Metadata   Metadata                `json:"metadata" yaml:"metadata"`
	Spec       DistributionChannelSpec `json:"spec" yaml:"spec"`
}

type ResolvedDistributionChannel struct {
	Channel *DistributionChannel
	BOM     *BOM
	BOMPath string
}

type PromoteDistributionChannelOptions struct {
	ChannelPath     string
	TargetBOMPath   string
	HealthProofPath string
	Reason          string
	ApprovedBy      string
	ApprovedAt      time.Time
}

type PromoteDistributionChannelResult struct {
	Channel      *DistributionChannel
	BOM          *BOM
	ChannelPath  string
	BOMPath      string
	FromRevision string
	ToRevision   string
	Changed      bool
	Promotion    DistributionPromotionRef
	HealthProof  *DistributionHealthProof
	Decision     *promotionpolicy.Decision
}

type DistributionHealthProofSpec struct {
	Line           string                     `json:"line" yaml:"line"`
	TargetRevision string                     `json:"targetRevision" yaml:"targetRevision"`
	Passed         bool                       `json:"passed" yaml:"passed"`
	Summary        string                     `json:"summary,omitempty" yaml:"summary,omitempty"`
	CollectedAt    string                     `json:"collectedAt,omitempty" yaml:"collectedAt,omitempty"`
	Signals        []DistributionHealthSignal `json:"signals,omitempty" yaml:"signals,omitempty"`
}

type DistributionHealthSignal struct {
	Name    string `json:"name" yaml:"name"`
	Passed  bool   `json:"passed" yaml:"passed"`
	Message string `json:"message,omitempty" yaml:"message,omitempty"`
}

type DistributionHealthProof struct {
	APIVersion string                      `json:"apiVersion" yaml:"apiVersion"`
	Kind       string                      `json:"kind" yaml:"kind"`
	Metadata   Metadata                    `json:"metadata" yaml:"metadata"`
	Spec       DistributionHealthProofSpec `json:"spec" yaml:"spec"`
}

func NewDistributionHealthProof(name, line, targetRevision string, passed bool) *DistributionHealthProof {
	return &DistributionHealthProof{
		APIVersion: distribution.APIVersion,
		Kind:       distribution.KindDistributionHealthProof,
		Metadata: Metadata{
			Name: name,
		},
		Spec: DistributionHealthProofSpec{
			Line:           line,
			TargetRevision: targetRevision,
			Passed:         passed,
		},
	}
}

func NewDistributionChannel(name, line string, channel ReleaseChannel, targetRevision, bomPath string) *DistributionChannel {
	return &DistributionChannel{
		APIVersion: distribution.APIVersion,
		Kind:       distribution.KindDistributionChannel,
		Metadata: Metadata{
			Name: name,
		},
		Spec: DistributionChannelSpec{
			Line:           line,
			Channel:        channel,
			TargetRevision: targetRevision,
			BOMPath:        bomPath,
		},
	}
}

func (c DistributionChannel) String() string {
	data, _ := yaml.Marshal(c)
	return string(data)
}

func (c DistributionChannel) Validate() error {
	if c.APIVersion != distribution.APIVersion {
		return fmt.Errorf("unsupported apiVersion %q", c.APIVersion)
	}
	if c.Kind != distribution.KindDistributionChannel {
		return fmt.Errorf("unsupported kind %q", c.Kind)
	}
	if c.Metadata.Name == "" {
		return fmt.Errorf("metadata.name cannot be empty")
	}
	if strings.TrimSpace(c.Spec.Line) == "" {
		return fmt.Errorf("spec.line cannot be empty")
	}
	if err := c.Spec.Channel.Validate(); err != nil {
		return fmt.Errorf("spec.channel: %w", err)
	}
	if strings.TrimSpace(c.Spec.TargetRevision) == "" {
		return fmt.Errorf("spec.targetRevision cannot be empty")
	}
	if strings.TrimSpace(c.Spec.BOMPath) == "" {
		return fmt.Errorf("spec.bomPath cannot be empty")
	}
	for i, promotion := range c.Spec.PromotionHistory {
		if err := promotion.Validate(); err != nil {
			return fmt.Errorf("spec.promotionHistory[%d]: %w", i, err)
		}
	}
	return nil
}

func (p DistributionPromotionRef) Validate() error {
	if strings.TrimSpace(p.ToRevision) == "" {
		return fmt.Errorf("toRevision cannot be empty")
	}
	if strings.TrimSpace(p.BOMPath) == "" {
		return fmt.Errorf("bomPath cannot be empty")
	}
	if strings.TrimSpace(p.Reason) == "" {
		return fmt.Errorf("reason cannot be empty")
	}
	if strings.TrimSpace(p.ApprovedBy) == "" {
		return fmt.Errorf("approvedBy cannot be empty")
	}
	if strings.TrimSpace(p.ApprovedAt) == "" {
		return fmt.Errorf("approvedAt cannot be empty")
	}
	if _, err := time.Parse(time.RFC3339, p.ApprovedAt); err != nil {
		return fmt.Errorf("approvedAt must be RFC3339: %w", err)
	}
	if strings.TrimSpace(p.HealthProofDigest) != "" && strings.TrimSpace(p.HealthProofPath) == "" {
		return fmt.Errorf("healthProofPath cannot be empty when healthProofDigest is set")
	}
	return nil
}

func (p DistributionHealthProof) Validate() error {
	if p.APIVersion != distribution.APIVersion {
		return fmt.Errorf("unsupported apiVersion %q", p.APIVersion)
	}
	if p.Kind != distribution.KindDistributionHealthProof {
		return fmt.Errorf("unsupported kind %q", p.Kind)
	}
	if p.Metadata.Name == "" {
		return fmt.Errorf("metadata.name cannot be empty")
	}
	if strings.TrimSpace(p.Spec.Line) == "" {
		return fmt.Errorf("spec.line cannot be empty")
	}
	if strings.TrimSpace(p.Spec.TargetRevision) == "" {
		return fmt.Errorf("spec.targetRevision cannot be empty")
	}
	if strings.TrimSpace(p.Spec.CollectedAt) != "" {
		if _, err := time.Parse(time.RFC3339, p.Spec.CollectedAt); err != nil {
			return fmt.Errorf("spec.collectedAt must be RFC3339: %w", err)
		}
	}
	for i, signal := range p.Spec.Signals {
		if strings.TrimSpace(signal.Name) == "" {
			return fmt.Errorf("spec.signals[%d].name cannot be empty", i)
		}
	}
	return nil
}

func LoadDistributionChannelFile(path string) (*DistributionChannel, error) {
	if !fileutil.IsFile(path) {
		return nil, fmt.Errorf("distribution channel file %q not found", path)
	}

	var doc DistributionChannel
	if err := yamlutil.UnmarshalFile(path, &doc); err != nil {
		return nil, fmt.Errorf("unmarshal distribution channel %q: %w", path, err)
	}
	if err := doc.Validate(); err != nil {
		return nil, fmt.Errorf("validate distribution channel %q: %w", path, err)
	}
	return &doc, nil
}

func LoadDistributionHealthProofFile(path string) (*DistributionHealthProof, string, error) {
	if !fileutil.IsFile(path) {
		return nil, "", fmt.Errorf("distribution health proof file %q not found", path)
	}

	data, err := os.ReadFile(path)
	if err != nil {
		return nil, "", fmt.Errorf("read distribution health proof %q: %w", path, err)
	}
	proofDigest := digest.Canonical.FromBytes(data).String()
	var doc DistributionHealthProof
	if err := yaml.Unmarshal(data, &doc); err != nil {
		return nil, "", fmt.Errorf("unmarshal distribution health proof %q: %w", path, err)
	}
	if err := doc.Validate(); err != nil {
		return nil, "", fmt.Errorf("validate distribution health proof %q: %w", path, err)
	}
	return &doc, proofDigest, nil
}

func ResolveDistributionChannelFile(path string) (*ResolvedDistributionChannel, error) {
	channel, err := LoadDistributionChannelFile(path)
	if err != nil {
		return nil, err
	}
	bomPath := strings.TrimSpace(channel.Spec.BOMPath)
	if !filepath.IsAbs(bomPath) {
		bomPath = filepath.Join(filepath.Dir(path), bomPath)
	}
	doc, err := LoadFile(bomPath)
	if err != nil {
		return nil, err
	}
	if doc.Metadata.Name != channel.Spec.Line {
		return nil, fmt.Errorf("distribution channel %q line %q does not match BOM metadata.name %q", channel.Metadata.Name, channel.Spec.Line, doc.Metadata.Name)
	}
	if doc.Spec.Revision != channel.Spec.TargetRevision {
		return nil, fmt.Errorf("distribution channel %q targetRevision %q does not match BOM spec.revision %q", channel.Metadata.Name, channel.Spec.TargetRevision, doc.Spec.Revision)
	}
	doc.Spec.Channel = channel.Spec.Channel
	return &ResolvedDistributionChannel{
		Channel: channel,
		BOM:     doc,
		BOMPath: bomPath,
	}, nil
}

func PromoteDistributionChannelFile(opts PromoteDistributionChannelOptions) (*PromoteDistributionChannelResult, error) {
	channelPath := strings.TrimSpace(opts.ChannelPath)
	if channelPath == "" {
		return nil, fmt.Errorf("channel path cannot be empty")
	}
	targetBOMPath := strings.TrimSpace(opts.TargetBOMPath)
	if targetBOMPath == "" {
		return nil, fmt.Errorf("target BOM path cannot be empty")
	}
	reason := strings.TrimSpace(opts.Reason)
	if reason == "" {
		return nil, fmt.Errorf("promotion reason cannot be empty")
	}
	approvedBy := strings.TrimSpace(opts.ApprovedBy)
	if approvedBy == "" {
		return nil, fmt.Errorf("promotion approvedBy cannot be empty")
	}

	channel, err := LoadDistributionChannelFile(channelPath)
	if err != nil {
		return nil, err
	}
	targetBOM, err := LoadFile(targetBOMPath)
	if err != nil {
		return nil, err
	}
	if targetBOM.Metadata.Name != channel.Spec.Line {
		return nil, fmt.Errorf("distribution channel %q line %q does not match target BOM metadata.name %q", channel.Metadata.Name, channel.Spec.Line, targetBOM.Metadata.Name)
	}
	healthProof, healthProofDigest, err := distributionHealthProofForPromotion(opts.HealthProofPath, targetBOM)
	if err != nil {
		return nil, err
	}

	fromRevision := channel.Spec.TargetRevision
	decision, err := evaluateDistributionPromotion(channel, targetBOM, healthProof)
	if err != nil {
		return nil, err
	}

	targetBOMPathForChannel := distributionChannelRelativePath(channelPath, targetBOMPath)
	changed := fromRevision != targetBOM.Spec.Revision || strings.TrimSpace(channel.Spec.BOMPath) != targetBOMPathForChannel
	channel.Spec.TargetRevision = targetBOM.Spec.Revision
	channel.Spec.BOMPath = targetBOMPathForChannel

	approvedAt := opts.ApprovedAt
	if approvedAt.IsZero() {
		approvedAt = time.Now().UTC()
	}
	promotion := DistributionPromotionRef{
		FromRevision: strings.TrimSpace(fromRevision),
		ToRevision:   targetBOM.Spec.Revision,
		BOMPath:      targetBOMPathForChannel,
		Reason:       reason,
		ApprovedBy:   approvedBy,
		ApprovedAt:   approvedAt.UTC().Format(time.RFC3339),
	}
	if healthProof != nil {
		healthProofPath := distributionChannelRelativePath(channelPath, opts.HealthProofPath)
		promotion.HealthProofPath = healthProofPath
		promotion.HealthProofDigest = healthProofDigest
		promotion.HealthProofSummary = strings.TrimSpace(healthProof.Spec.Summary)
	}
	channel.Spec.PromotionHistory = append(channel.Spec.PromotionHistory, promotion)

	if err := channel.Validate(); err != nil {
		return nil, fmt.Errorf("validate promoted distribution channel %q: %w", channelPath, err)
	}
	if err := yamlutil.MarshalFile(channelPath, channel); err != nil {
		return nil, fmt.Errorf("write promoted distribution channel %q: %w", channelPath, err)
	}

	return &PromoteDistributionChannelResult{
		Channel:      channel,
		BOM:          targetBOM,
		ChannelPath:  channelPath,
		BOMPath:      targetBOMPath,
		FromRevision: fromRevision,
		ToRevision:   targetBOM.Spec.Revision,
		Changed:      changed,
		Promotion:    promotion,
		HealthProof:  healthProof,
		Decision:     decision,
	}, nil
}

func evaluateDistributionPromotion(channel *DistributionChannel, targetBOM *BOM, healthProof *DistributionHealthProof) (*promotionpolicy.Decision, error) {
	if channel == nil {
		return nil, fmt.Errorf("distribution channel cannot be nil")
	}
	if targetBOM == nil {
		return nil, fmt.Errorf("target BOM cannot be nil")
	}
	decision, err := promotionpolicy.EvaluateDefault(promotionpolicy.Request{
		TargetChannel: promotionpolicy.Channel(channel.Spec.Channel),
		Candidate: promotionpolicy.CandidateRevision{
			Line:          targetBOM.Metadata.Name,
			Revision:      targetBOM.Spec.Revision,
			SourceChannel: promotionpolicy.Channel(targetBOM.Spec.Channel),
			Replacing:     channel.Spec.TargetRevision,
		},
		HealthProof: distributionPromotionHealthProofSummary(healthProof),
	})
	if err != nil {
		return nil, fmt.Errorf("evaluate promotion policy: %w", err)
	}
	if !decision.Allowed {
		return nil, fmt.Errorf("promotion policy blocked channel %q to revision %q: %s", channel.Metadata.Name, targetBOM.Spec.Revision, distributionPromotionViolationSummary(decision.Violations))
	}
	return decision, nil
}

func distributionPromotionHealthProofSummary(proof *DistributionHealthProof) promotionpolicy.HealthProofSummary {
	if proof == nil {
		return promotionpolicy.HealthProofSummary{}
	}
	failedSignals := make([]string, 0)
	for _, signal := range proof.Spec.Signals {
		if !signal.Passed {
			failedSignals = append(failedSignals, signal.Name)
		}
	}
	return promotionpolicy.HealthProofSummary{
		Provided:      true,
		Passed:        proof.Spec.Passed,
		FailedSignals: failedSignals,
	}
}

func distributionPromotionViolationSummary(violations []promotionpolicy.Violation) string {
	if len(violations) == 0 {
		return "no policy violation details"
	}
	parts := make([]string, 0, len(violations))
	for _, violation := range violations {
		parts = append(parts, fmt.Sprintf("%s: %s", violation.Code, violation.Message))
	}
	return strings.Join(parts, "; ")
}

func distributionHealthProofForPromotion(path string, targetBOM *BOM) (*DistributionHealthProof, string, error) {
	path = strings.TrimSpace(path)
	if path == "" {
		return nil, "", nil
	}
	if targetBOM == nil {
		return nil, "", fmt.Errorf("target BOM cannot be nil")
	}
	proof, digest, err := LoadDistributionHealthProofFile(path)
	if err != nil {
		return nil, "", err
	}
	if proof.Spec.Line != targetBOM.Metadata.Name {
		return nil, "", fmt.Errorf("distribution health proof %q line %q does not match target BOM metadata.name %q", proof.Metadata.Name, proof.Spec.Line, targetBOM.Metadata.Name)
	}
	if proof.Spec.TargetRevision != targetBOM.Spec.Revision {
		return nil, "", fmt.Errorf("distribution health proof %q targetRevision %q does not match target BOM spec.revision %q", proof.Metadata.Name, proof.Spec.TargetRevision, targetBOM.Spec.Revision)
	}
	if !proof.Spec.Passed {
		return nil, "", fmt.Errorf("distribution health proof %q did not pass", proof.Metadata.Name)
	}
	if len(proof.Spec.Signals) == 0 {
		return nil, "", fmt.Errorf("distribution health proof %q has no health signals", proof.Metadata.Name)
	}
	for _, signal := range proof.Spec.Signals {
		if !signal.Passed {
			return nil, "", fmt.Errorf("distribution health proof %q signal %q did not pass", proof.Metadata.Name, signal.Name)
		}
	}
	return proof, digest, nil
}

func distributionChannelRelativePath(channelPath, path string) string {
	channelAbs, err := filepath.Abs(channelPath)
	if err != nil {
		return filepath.ToSlash(filepath.Clean(path))
	}
	pathAbs, err := filepath.Abs(path)
	if err != nil {
		return filepath.ToSlash(filepath.Clean(path))
	}
	rel, err := filepath.Rel(filepath.Dir(channelAbs), pathAbs)
	if err != nil {
		return pathAbs
	}
	return filepath.ToSlash(rel)
}
