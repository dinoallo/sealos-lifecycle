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
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/labring/sealos/pkg/distribution"
	promotionpolicy "github.com/labring/sealos/pkg/distribution/promotion"
	fileutil "github.com/labring/sealos/pkg/utils/file"
	yamlutil "github.com/labring/sealos/pkg/utils/yaml"
	"github.com/opencontainers/go-digest"
	"sigs.k8s.io/yaml"
)

type ReleaseChannelDocumentSpec struct {
	Distribution     string                     `json:"distribution"               yaml:"distribution"`
	Channel          ReleaseChannel             `json:"channel"                    yaml:"channel"`
	TargetRevision   string                     `json:"targetRevision"             yaml:"targetRevision"`
	BOMPath          string                     `json:"bomPath"                    yaml:"bomPath"`
	BOMDigest        string                     `json:"bomDigest,omitempty"        yaml:"bomDigest,omitempty"`
	PromotionHistory []DistributionPromotionRef `json:"promotionHistory,omitempty" yaml:"promotionHistory,omitempty"`
}

type DistributionPromotionRef struct {
	FromRevision       string                          `json:"fromRevision,omitempty"       yaml:"fromRevision,omitempty"`
	ToRevision         string                          `json:"toRevision"                   yaml:"toRevision"`
	SourceChannel      ReleaseChannel                  `json:"sourceChannel,omitempty"      yaml:"sourceChannel,omitempty"`
	TargetChannel      ReleaseChannel                  `json:"targetChannel,omitempty"      yaml:"targetChannel,omitempty"`
	BOMPath            string                          `json:"bomPath"                      yaml:"bomPath"`
	BOMDigest          string                          `json:"bomDigest,omitempty"          yaml:"bomDigest,omitempty"`
	ComponentDigests   []ReleaseComponentDigestRef     `json:"componentDigests,omitempty"   yaml:"componentDigests,omitempty"`
	ValidationCohort   string                          `json:"validationCohort,omitempty"   yaml:"validationCohort,omitempty"`
	Reason             string                          `json:"reason"                       yaml:"reason"`
	ApprovedBy         string                          `json:"approvedBy"                   yaml:"approvedBy"`
	ApprovedAt         string                          `json:"approvedAt"                   yaml:"approvedAt"`
	Evidence           []ReleasePromotionEvidenceRef   `json:"evidence,omitempty"           yaml:"evidence,omitempty"`
	Timeline           []ReleasePromotionTimelineEvent `json:"timeline,omitempty"           yaml:"timeline,omitempty"`
	HealthProofPath    string                          `json:"healthProofPath,omitempty"    yaml:"healthProofPath,omitempty"`
	HealthProofDigest  string                          `json:"healthProofDigest,omitempty"  yaml:"healthProofDigest,omitempty"`
	HealthProofSummary string                          `json:"healthProofSummary,omitempty" yaml:"healthProofSummary,omitempty"`
}

type ReleaseComponentDigestRef struct {
	PackageName  string `json:"packageName"            yaml:"packageName"`
	Category     string `json:"category,omitempty"     yaml:"category,omitempty"`
	Version      string `json:"version,omitempty"      yaml:"version,omitempty"`
	ArtifactName string `json:"artifactName,omitempty" yaml:"artifactName,omitempty"`
	Image        string `json:"image,omitempty"        yaml:"image,omitempty"`
	Digest       string `json:"digest"                 yaml:"digest"`
}

type ReleasePromotionEvidenceRef struct {
	Type    string `json:"type"              yaml:"type"`
	Path    string `json:"path,omitempty"    yaml:"path,omitempty"`
	Digest  string `json:"digest,omitempty"  yaml:"digest,omitempty"`
	Summary string `json:"summary,omitempty" yaml:"summary,omitempty"`
}

type ReleasePromotionTimelineEvent struct {
	Type     string         `json:"type"               yaml:"type"`
	At       string         `json:"at"                 yaml:"at"`
	Actor    string         `json:"actor,omitempty"    yaml:"actor,omitempty"`
	Channel  ReleaseChannel `json:"channel,omitempty"  yaml:"channel,omitempty"`
	Revision string         `json:"revision,omitempty" yaml:"revision,omitempty"`
	Reason   string         `json:"reason,omitempty"   yaml:"reason,omitempty"`
	Message  string         `json:"message,omitempty"  yaml:"message,omitempty"`
}

type ReleaseChannelDocument struct {
	APIVersion string                     `json:"apiVersion" yaml:"apiVersion"`
	Kind       string                     `json:"kind"       yaml:"kind"`
	Metadata   Metadata                   `json:"metadata"   yaml:"metadata"`
	Spec       ReleaseChannelDocumentSpec `json:"spec"       yaml:"spec"`
}

type ResolvedReleaseChannel struct {
	Channel       *ReleaseChannelDocument
	BOM           *BOM
	BOMPath       string
	BOMDigest     string
	ChannelSource string
}

type PromoteReleaseChannelOptions struct {
	ChannelPath      string
	TargetBOMPath    string
	ReleaseStoreRoot string
	SourceChannel    ReleaseChannel
	ValidationCohort string
	HealthProofPath  string
	Reason           string
	ApprovedBy       string
	ApprovedAt       time.Time
}

type PromoteReleaseChannelResult struct {
	Channel              *ReleaseChannelDocument
	BOM                  *BOM
	ChannelPath          string
	BOMPath              string
	FromRevision         string
	ToRevision           string
	Changed              bool
	Promotion            DistributionPromotionRef
	HealthProof          *DistributionHealthProof
	Decision             *promotionpolicy.Decision
	SourceChannel        ReleaseChannel
	Candidate            *ReleaseCandidateRevisionDocument
	CandidatePath        string
	PromotionHistory     *ReleasePromotionHistoryDocument
	PromotionHistoryPath string
}

type ReleaseCandidateRevisionSpec struct {
	Line             string                          `json:"line"                       yaml:"line"`
	Revision         string                          `json:"revision"                   yaml:"revision"`
	SourceChannel    ReleaseChannel                  `json:"sourceChannel"              yaml:"sourceChannel"`
	TargetChannel    ReleaseChannel                  `json:"targetChannel"              yaml:"targetChannel"`
	ReplacesRevision string                          `json:"replacesRevision,omitempty" yaml:"replacesRevision,omitempty"`
	BOMPath          string                          `json:"bomPath"                    yaml:"bomPath"`
	BOMDigest        string                          `json:"bomDigest"                  yaml:"bomDigest"`
	ComponentDigests []ReleaseComponentDigestRef     `json:"componentDigests"           yaml:"componentDigests"`
	ValidationCohort string                          `json:"validationCohort,omitempty" yaml:"validationCohort,omitempty"`
	Evidence         []ReleasePromotionEvidenceRef   `json:"evidence,omitempty"         yaml:"evidence,omitempty"`
	Timeline         []ReleasePromotionTimelineEvent `json:"timeline,omitempty"         yaml:"timeline,omitempty"`
	CreatedAt        string                          `json:"createdAt"                  yaml:"createdAt"`
}

type ReleaseCandidateRevisionDocument struct {
	APIVersion string                       `json:"apiVersion" yaml:"apiVersion"`
	Kind       string                       `json:"kind"       yaml:"kind"`
	Metadata   Metadata                     `json:"metadata"   yaml:"metadata"`
	Spec       ReleaseCandidateRevisionSpec `json:"spec"       yaml:"spec"`
}

type ReleasePromotionHistorySpec struct {
	Line           string                    `json:"line"                     yaml:"line"`
	Channel        ReleaseChannel            `json:"channel"                  yaml:"channel"`
	Promotion      DistributionPromotionRef  `json:"promotion"                yaml:"promotion"`
	PolicyDecision *promotionpolicy.Decision `json:"policyDecision,omitempty" yaml:"policyDecision,omitempty"`
	CandidateRef   string                    `json:"candidateRef,omitempty"   yaml:"candidateRef,omitempty"`
	RecordedAt     string                    `json:"recordedAt"               yaml:"recordedAt"`
}

type ReleasePromotionHistoryDocument struct {
	APIVersion string                      `json:"apiVersion" yaml:"apiVersion"`
	Kind       string                      `json:"kind"       yaml:"kind"`
	Metadata   Metadata                    `json:"metadata"   yaml:"metadata"`
	Spec       ReleasePromotionHistorySpec `json:"spec"       yaml:"spec"`
}

type DistributionHealthProofSpec struct {
	Line           string                           `json:"line"                    yaml:"line"`
	TargetRevision string                           `json:"targetRevision"          yaml:"targetRevision"`
	Passed         bool                             `json:"passed"                  yaml:"passed"`
	Summary        string                           `json:"summary,omitempty"       yaml:"summary,omitempty"`
	CollectedAt    string                           `json:"collectedAt,omitempty"   yaml:"collectedAt,omitempty"`
	Thresholds     DistributionHealthThresholds     `json:"thresholds,omitempty"    yaml:"thresholds,omitempty"`
	SignalSummary  *DistributionHealthSignalSummary `json:"signalSummary,omitempty" yaml:"signalSummary,omitempty"`
	Signals        []DistributionHealthSignal       `json:"signals,omitempty"       yaml:"signals,omitempty"`
}

type DistributionHealthThresholds struct {
	RequiredSignals  []string `json:"requiredSignals,omitempty"  yaml:"requiredSignals,omitempty"`
	MinPassedSignals int      `json:"minPassedSignals,omitempty" yaml:"minPassedSignals,omitempty"`
}

type DistributionHealthSignalSummary struct {
	TotalSignals           int `json:"totalSignals"                     yaml:"totalSignals"`
	PassedSignals          int `json:"passedSignals"                    yaml:"passedSignals"`
	FailedSignals          int `json:"failedSignals"                    yaml:"failedSignals"`
	RequiredSignals        int `json:"requiredSignals,omitempty"        yaml:"requiredSignals,omitempty"`
	PassedRequiredSignals  int `json:"passedRequiredSignals,omitempty"  yaml:"passedRequiredSignals,omitempty"`
	FailedRequiredSignals  int `json:"failedRequiredSignals,omitempty"  yaml:"failedRequiredSignals,omitempty"`
	MissingRequiredSignals int `json:"missingRequiredSignals,omitempty" yaml:"missingRequiredSignals,omitempty"`
	MinPassedSignals       int `json:"minPassedSignals,omitempty"       yaml:"minPassedSignals,omitempty"`
}

type DistributionHealthSignal struct {
	Name        string `json:"name"                  yaml:"name"`
	Passed      bool   `json:"passed"                yaml:"passed"`
	Required    bool   `json:"required,omitempty"    yaml:"required,omitempty"`
	Source      string `json:"source,omitempty"      yaml:"source,omitempty"`
	EvidenceRef string `json:"evidenceRef,omitempty" yaml:"evidenceRef,omitempty"`
	Message     string `json:"message,omitempty"     yaml:"message,omitempty"`
}

type DistributionHealthProofEvaluation struct {
	Passed                 bool
	HasThresholds          bool
	MinPassedSignals       int
	SignalSummary          DistributionHealthSignalSummary
	RequiredSignals        []string
	FailedSignals          []string
	FailedRequiredSignals  []string
	MissingRequiredSignals []string
}

type DistributionHealthProof struct {
	APIVersion string                      `json:"apiVersion" yaml:"apiVersion"`
	Kind       string                      `json:"kind"       yaml:"kind"`
	Metadata   Metadata                    `json:"metadata"   yaml:"metadata"`
	Spec       DistributionHealthProofSpec `json:"spec"       yaml:"spec"`
}

func NewDistributionHealthProof(
	name, line, targetRevision string,
	passed bool,
) *DistributionHealthProof {
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

func NewReleaseChannel(
	name, distributionLine string,
	channel ReleaseChannel,
	targetRevision, bomPath string,
) *ReleaseChannelDocument {
	return &ReleaseChannelDocument{
		APIVersion: distribution.APIVersion,
		Kind:       distribution.KindReleaseChannel,
		Metadata: Metadata{
			Name: name,
		},
		Spec: ReleaseChannelDocumentSpec{
			Distribution:   distributionLine,
			Channel:        channel,
			TargetRevision: targetRevision,
			BOMPath:        bomPath,
		},
	}
}

func (c ReleaseChannelDocument) String() string {
	data, _ := yaml.Marshal(c)
	return string(data)
}

func (c ReleaseChannelDocument) Validate() error {
	if c.APIVersion != distribution.APIVersion {
		return fmt.Errorf("unsupported apiVersion %q", c.APIVersion)
	}
	if c.Kind != distribution.KindReleaseChannel {
		return fmt.Errorf("unsupported kind %q", c.Kind)
	}
	if c.Metadata.Name == "" {
		return fmt.Errorf("metadata.name cannot be empty")
	}
	if strings.TrimSpace(c.Distribution()) == "" {
		return fmt.Errorf("spec.distribution cannot be empty")
	}
	if err := c.releaseChannel().ValidateRequired(); err != nil {
		return fmt.Errorf("spec.channel: %w", err)
	}
	if strings.TrimSpace(c.Spec.TargetRevision) == "" {
		return fmt.Errorf("spec.targetRevision cannot be empty")
	}
	if strings.TrimSpace(c.Spec.BOMPath) == "" {
		return fmt.Errorf("spec.bomPath cannot be empty")
	}
	if digestValue := strings.TrimSpace(c.Spec.BOMDigest); digestValue != "" {
		if _, err := digest.Parse(digestValue); err != nil {
			return fmt.Errorf("spec.bomDigest: invalid digest %q: %w", digestValue, err)
		}
	}
	for i, promotion := range c.Spec.PromotionHistory {
		if err := promotion.Validate(); err != nil {
			return fmt.Errorf("spec.promotionHistory[%d]: %w", i, err)
		}
	}
	return nil
}

func (c ReleaseChannelDocument) Distribution() string {
	return strings.TrimSpace(c.Spec.Distribution)
}

func (c ReleaseChannelDocument) releaseChannel() ReleaseChannel {
	if c.Spec.Channel != "" {
		return c.Spec.Channel
	}
	return ReleaseChannel(strings.TrimSpace(c.Metadata.Name))
}

func (c *ReleaseChannelDocument) Normalize() {
	if c == nil {
		return
	}
	if c.Spec.Channel == "" {
		c.Spec.Channel = c.releaseChannel()
	}
	if c.Kind == "" {
		c.Kind = distribution.KindReleaseChannel
	}
}

func (p DistributionPromotionRef) Validate() error {
	if strings.TrimSpace(p.ToRevision) == "" {
		return fmt.Errorf("toRevision cannot be empty")
	}
	if err := p.SourceChannel.Validate(); err != nil {
		return fmt.Errorf("sourceChannel: %w", err)
	}
	if err := p.TargetChannel.Validate(); err != nil {
		return fmt.Errorf("targetChannel: %w", err)
	}
	if strings.TrimSpace(p.BOMPath) == "" {
		return fmt.Errorf("bomPath cannot be empty")
	}
	if digestValue := strings.TrimSpace(p.BOMDigest); digestValue != "" {
		if _, err := digest.Parse(digestValue); err != nil {
			return fmt.Errorf("bomDigest: invalid digest %q: %w", digestValue, err)
		}
	}
	for i, component := range p.ComponentDigests {
		if err := component.Validate(); err != nil {
			return fmt.Errorf("componentDigests[%d]: %w", i, err)
		}
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
	if digestValue := strings.TrimSpace(p.HealthProofDigest); digestValue != "" {
		if _, err := digest.Parse(digestValue); err != nil {
			return fmt.Errorf("healthProofDigest: invalid digest %q: %w", digestValue, err)
		}
	}
	for i, evidence := range p.Evidence {
		if err := evidence.Validate(); err != nil {
			return fmt.Errorf("evidence[%d]: %w", i, err)
		}
	}
	for i, event := range p.Timeline {
		if err := event.Validate(); err != nil {
			return fmt.Errorf("timeline[%d]: %w", i, err)
		}
	}
	return nil
}

func (r ReleaseComponentDigestRef) Validate() error {
	if strings.TrimSpace(r.PackageName) == "" {
		return errors.New("packageName cannot be empty")
	}
	if strings.TrimSpace(r.Digest) == "" {
		return errors.New("digest cannot be empty")
	}
	if _, err := digest.Parse(strings.TrimSpace(r.Digest)); err != nil {
		return fmt.Errorf("invalid digest %q: %w", r.Digest, err)
	}
	return nil
}

func (e ReleasePromotionEvidenceRef) Validate() error {
	if strings.TrimSpace(e.Type) == "" {
		return errors.New("type cannot be empty")
	}
	if digestValue := strings.TrimSpace(e.Digest); digestValue != "" {
		if _, err := digest.Parse(digestValue); err != nil {
			return fmt.Errorf("digest: invalid digest %q: %w", digestValue, err)
		}
	}
	return nil
}

func (e ReleasePromotionTimelineEvent) Validate() error {
	if strings.TrimSpace(e.Type) == "" {
		return errors.New("type cannot be empty")
	}
	if strings.TrimSpace(e.At) == "" {
		return errors.New("at cannot be empty")
	}
	if _, err := time.Parse(time.RFC3339, e.At); err != nil {
		return fmt.Errorf("at must be RFC3339: %w", err)
	}
	if err := e.Channel.Validate(); err != nil {
		return fmt.Errorf("channel: %w", err)
	}
	return nil
}

func (c ReleaseCandidateRevisionDocument) Validate() error {
	if c.APIVersion != distribution.APIVersion {
		return fmt.Errorf("unsupported apiVersion %q", c.APIVersion)
	}
	if c.Kind != distribution.KindReleaseCandidateRevision {
		return fmt.Errorf("unsupported kind %q", c.Kind)
	}
	if strings.TrimSpace(c.Metadata.Name) == "" {
		return errors.New("metadata.name cannot be empty")
	}
	if strings.TrimSpace(c.Spec.Line) == "" {
		return errors.New("spec.line cannot be empty")
	}
	if strings.TrimSpace(c.Spec.Revision) == "" {
		return errors.New("spec.revision cannot be empty")
	}
	if err := c.Spec.SourceChannel.ValidateRequired(); err != nil {
		return fmt.Errorf("spec.sourceChannel: %w", err)
	}
	if err := c.Spec.TargetChannel.ValidateRequired(); err != nil {
		return fmt.Errorf("spec.targetChannel: %w", err)
	}
	if strings.TrimSpace(c.Spec.BOMPath) == "" {
		return errors.New("spec.bomPath cannot be empty")
	}
	if strings.TrimSpace(c.Spec.BOMDigest) == "" {
		return errors.New("spec.bomDigest cannot be empty")
	}
	if _, err := digest.Parse(strings.TrimSpace(c.Spec.BOMDigest)); err != nil {
		return fmt.Errorf("spec.bomDigest: invalid digest %q: %w", c.Spec.BOMDigest, err)
	}
	if len(c.Spec.ComponentDigests) == 0 {
		return errors.New("spec.componentDigests cannot be empty")
	}
	for i, component := range c.Spec.ComponentDigests {
		if err := component.Validate(); err != nil {
			return fmt.Errorf("spec.componentDigests[%d]: %w", i, err)
		}
	}
	for i, evidence := range c.Spec.Evidence {
		if err := evidence.Validate(); err != nil {
			return fmt.Errorf("spec.evidence[%d]: %w", i, err)
		}
	}
	for i, event := range c.Spec.Timeline {
		if err := event.Validate(); err != nil {
			return fmt.Errorf("spec.timeline[%d]: %w", i, err)
		}
	}
	if strings.TrimSpace(c.Spec.CreatedAt) == "" {
		return errors.New("spec.createdAt cannot be empty")
	}
	if _, err := time.Parse(time.RFC3339, c.Spec.CreatedAt); err != nil {
		return fmt.Errorf("spec.createdAt must be RFC3339: %w", err)
	}
	return nil
}

func (h ReleasePromotionHistoryDocument) Validate() error {
	if h.APIVersion != distribution.APIVersion {
		return fmt.Errorf("unsupported apiVersion %q", h.APIVersion)
	}
	if h.Kind != distribution.KindReleasePromotionHistory {
		return fmt.Errorf("unsupported kind %q", h.Kind)
	}
	if strings.TrimSpace(h.Metadata.Name) == "" {
		return errors.New("metadata.name cannot be empty")
	}
	if strings.TrimSpace(h.Spec.Line) == "" {
		return errors.New("spec.line cannot be empty")
	}
	if err := h.Spec.Channel.ValidateRequired(); err != nil {
		return fmt.Errorf("spec.channel: %w", err)
	}
	if err := h.Spec.Promotion.Validate(); err != nil {
		return fmt.Errorf("spec.promotion: %w", err)
	}
	if strings.TrimSpace(h.Spec.RecordedAt) == "" {
		return errors.New("spec.recordedAt cannot be empty")
	}
	if _, err := time.Parse(time.RFC3339, h.Spec.RecordedAt); err != nil {
		return fmt.Errorf("spec.recordedAt must be RFC3339: %w", err)
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
	if p.Spec.Thresholds.MinPassedSignals < 0 {
		return errors.New("spec.thresholds.minPassedSignals must be non-negative")
	}
	seenRequired := make(map[string]struct{}, len(p.Spec.Thresholds.RequiredSignals))
	for i, name := range p.Spec.Thresholds.RequiredSignals {
		name = strings.TrimSpace(name)
		if name == "" {
			return fmt.Errorf("spec.thresholds.requiredSignals[%d] cannot be empty", i)
		}
		if _, ok := seenRequired[name]; ok {
			return fmt.Errorf("spec.thresholds.requiredSignals[%d]: duplicate signal %q", i, name)
		}
		seenRequired[name] = struct{}{}
	}
	for i, signal := range p.Spec.Signals {
		if strings.TrimSpace(signal.Name) == "" {
			return fmt.Errorf("spec.signals[%d].name cannot be empty", i)
		}
	}
	return nil
}

func (p *DistributionHealthProof) Normalize() {
	if p == nil {
		return
	}
	evaluation := EvaluateDistributionHealthProof(p)
	p.Spec.Passed = evaluation.Passed
	summary := evaluation.SignalSummary
	p.Spec.SignalSummary = &summary
}

func EvaluateDistributionHealthProof(
	proof *DistributionHealthProof,
) DistributionHealthProofEvaluation {
	if proof == nil {
		return DistributionHealthProofEvaluation{}
	}
	requiredSet := make(map[string]struct{}, len(proof.Spec.Thresholds.RequiredSignals))
	for _, name := range proof.Spec.Thresholds.RequiredSignals {
		name = strings.TrimSpace(name)
		if name == "" {
			continue
		}
		requiredSet[name] = struct{}{}
	}

	signalByName := make(map[string]DistributionHealthSignal, len(proof.Spec.Signals))
	evaluation := DistributionHealthProofEvaluation{
		HasThresholds:    len(requiredSet) > 0 || proof.Spec.Thresholds.MinPassedSignals > 0,
		MinPassedSignals: proof.Spec.Thresholds.MinPassedSignals,
	}
	for _, signal := range proof.Spec.Signals {
		name := strings.TrimSpace(signal.Name)
		if name == "" {
			continue
		}
		signal.Name = name
		if signal.Required {
			requiredSet[name] = struct{}{}
		}
		signalByName[name] = signal
		evaluation.SignalSummary.TotalSignals++
		if signal.Passed {
			evaluation.SignalSummary.PassedSignals++
		} else {
			evaluation.SignalSummary.FailedSignals++
			evaluation.FailedSignals = append(evaluation.FailedSignals, name)
		}
	}

	evaluation.RequiredSignals = sortedHealthSignalNames(requiredSet)
	for _, name := range evaluation.RequiredSignals {
		signal, ok := signalByName[name]
		evaluation.SignalSummary.RequiredSignals++
		switch {
		case !ok:
			evaluation.SignalSummary.MissingRequiredSignals++
			evaluation.MissingRequiredSignals = append(evaluation.MissingRequiredSignals, name)
		case signal.Passed:
			evaluation.SignalSummary.PassedRequiredSignals++
		default:
			evaluation.SignalSummary.FailedRequiredSignals++
			evaluation.FailedRequiredSignals = append(evaluation.FailedRequiredSignals, name)
		}
	}

	minPassed := evaluation.MinPassedSignals
	if minPassed == 0 && !evaluation.HasThresholds {
		minPassed = len(proof.Spec.Signals)
	}
	evaluation.SignalSummary.MinPassedSignals = minPassed
	evaluation.Passed = len(proof.Spec.Signals) > 0 &&
		evaluation.SignalSummary.PassedSignals >= minPassed &&
		len(evaluation.FailedRequiredSignals) == 0 &&
		len(evaluation.MissingRequiredSignals) == 0
	if !evaluation.HasThresholds && len(evaluation.FailedSignals) > 0 {
		evaluation.Passed = false
	}
	return evaluation
}

func sortedHealthSignalNames(values map[string]struct{}) []string {
	if len(values) == 0 {
		return nil
	}
	names := make([]string, 0, len(values))
	for name := range values {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

func LoadReleaseChannelFile(path string) (*ReleaseChannelDocument, error) {
	if !fileutil.IsFile(path) {
		return nil, fmt.Errorf("release channel file %q not found", path)
	}

	var doc ReleaseChannelDocument
	if err := yamlutil.UnmarshalFile(path, &doc); err != nil {
		return nil, fmt.Errorf("unmarshal release channel %q: %w", path, err)
	}
	doc.Normalize()
	if err := doc.Validate(); err != nil {
		return nil, fmt.Errorf("validate release channel %q: %w", path, err)
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

func LoadReleaseCandidateRevisionFile(path string) (*ReleaseCandidateRevisionDocument, error) {
	if !fileutil.IsFile(path) {
		return nil, fmt.Errorf("release candidate revision file %q not found", path)
	}

	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read release candidate revision %q: %w", path, err)
	}
	var doc ReleaseCandidateRevisionDocument
	if err := yaml.Unmarshal(data, &doc); err != nil {
		return nil, fmt.Errorf("unmarshal release candidate revision %q: %w", path, err)
	}
	if err := doc.Validate(); err != nil {
		return nil, fmt.Errorf("validate release candidate revision %q: %w", path, err)
	}
	return &doc, nil
}

func LoadReleasePromotionHistoryFile(path string) (*ReleasePromotionHistoryDocument, error) {
	if !fileutil.IsFile(path) {
		return nil, fmt.Errorf("release promotion history file %q not found", path)
	}

	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read release promotion history %q: %w", path, err)
	}
	var doc ReleasePromotionHistoryDocument
	if err := yaml.Unmarshal(data, &doc); err != nil {
		return nil, fmt.Errorf("unmarshal release promotion history %q: %w", path, err)
	}
	if err := doc.Validate(); err != nil {
		return nil, fmt.Errorf("validate release promotion history %q: %w", path, err)
	}
	return &doc, nil
}

func ResolveReleaseChannelFile(path string) (*ResolvedReleaseChannel, error) {
	channel, err := LoadReleaseChannelFile(path)
	if err != nil {
		return nil, err
	}
	bomPath := strings.TrimSpace(channel.Spec.BOMPath)
	if !filepath.IsAbs(bomPath) {
		bomPath = filepath.Join(filepath.Dir(path), bomPath)
	}
	data, err := os.ReadFile(bomPath)
	if err != nil {
		return nil, fmt.Errorf("read bom %q: %w", bomPath, err)
	}
	bomDigest := digest.Canonical.FromBytes(data).String()
	if err := verifyReleaseChannelBOMDigest(channel, bomDigest); err != nil {
		return nil, err
	}
	doc, err := LoadBytes(data, bomPath)
	if err != nil {
		return nil, err
	}
	if err := validateReleaseChannelBOM(channel, doc); err != nil {
		return nil, err
	}
	doc.SetRuntimeChannel(channel.Spec.Channel)
	return &ResolvedReleaseChannel{
		Channel:       channel,
		BOM:           doc,
		BOMPath:       bomPath,
		BOMDigest:     bomDigest,
		ChannelSource: path,
	}, nil
}

func validateReleaseChannelBOM(channel *ReleaseChannelDocument, doc *BOM) error {
	if channel == nil {
		return errors.New("release channel cannot be nil")
	}
	if doc == nil {
		return errors.New("BOM cannot be nil")
	}
	if doc.Metadata.Name != channel.Distribution() {
		return fmt.Errorf(
			"release channel %q distribution %q does not match BOM metadata.name %q",
			channel.Metadata.Name,
			channel.Distribution(),
			doc.Metadata.Name,
		)
	}
	if doc.Spec.Revision != channel.Spec.TargetRevision {
		return fmt.Errorf(
			"release channel %q targetRevision %q does not match BOM spec.revision %q",
			channel.Metadata.Name,
			channel.Spec.TargetRevision,
			doc.Spec.Revision,
		)
	}
	return nil
}

func verifyReleaseChannelBOMDigest(channel *ReleaseChannelDocument, actual string) error {
	if channel == nil {
		return errors.New("release channel cannot be nil")
	}
	expected := strings.TrimSpace(channel.Spec.BOMDigest)
	if expected == "" {
		return nil
	}
	if actual != expected {
		return fmt.Errorf(
			"release channel %q BOM digest mismatch: expected %s, got %s",
			channel.Metadata.Name,
			expected,
			actual,
		)
	}
	return nil
}

func PromoteReleaseChannelFile(
	opts PromoteReleaseChannelOptions,
) (*PromoteReleaseChannelResult, error) {
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

	channel, err := LoadReleaseChannelFile(channelPath)
	if err != nil {
		return nil, err
	}
	targetBOMData, err := os.ReadFile(targetBOMPath)
	if err != nil {
		return nil, fmt.Errorf("read target BOM %q: %w", targetBOMPath, err)
	}
	targetBOMDigest := digest.Canonical.FromBytes(targetBOMData).String()
	targetBOM, err := LoadBytes(targetBOMData, targetBOMPath)
	if err != nil {
		return nil, err
	}
	if targetBOM.Metadata.Name != channel.Distribution() {
		return nil, fmt.Errorf(
			"release channel %q distribution %q does not match target BOM metadata.name %q",
			channel.Metadata.Name,
			channel.Distribution(),
			targetBOM.Metadata.Name,
		)
	}
	healthProof, healthProofDigest, err := distributionHealthProofForPromotion(
		opts.HealthProofPath,
		targetBOM,
	)
	if err != nil {
		return nil, err
	}
	validationCohort := strings.TrimSpace(opts.ValidationCohort)

	fromRevision := channel.Spec.TargetRevision
	sourceChannel := opts.SourceChannel
	if sourceChannel == "" {
		sourceChannel = channel.Spec.Channel
	}
	decision, err := evaluateDistributionPromotion(channel, targetBOM, healthProof, sourceChannel)
	if err != nil {
		return nil, err
	}

	targetBOMPathForChannel := releaseChannelRelativePath(channelPath, targetBOMPath)
	changed := fromRevision != targetBOM.Spec.Revision ||
		strings.TrimSpace(channel.Spec.BOMPath) != targetBOMPathForChannel
	channel.Spec.TargetRevision = targetBOM.Spec.Revision
	channel.Spec.BOMPath = targetBOMPathForChannel
	channel.Spec.BOMDigest = targetBOMDigest
	channel.Normalize()

	approvedAt := opts.ApprovedAt
	if approvedAt.IsZero() {
		approvedAt = time.Now().UTC()
	}
	approvedAtValue := approvedAt.UTC().Format(time.RFC3339)
	componentDigests := releaseComponentDigests(targetBOM)
	storeRoot := releaseStoreRoot(opts.ReleaseStoreRoot, channelPath)
	evidence := releasePromotionEvidence(
		storeRoot,
		opts.HealthProofPath,
		healthProofDigest,
		healthProof,
	)
	timeline := releasePromotionTimeline(
		fromRevision,
		targetBOM.Spec.Revision,
		sourceChannel,
		channel.Spec.Channel,
		reason,
		approvedBy,
		approvedAtValue,
	)
	promotion := DistributionPromotionRef{
		FromRevision:     strings.TrimSpace(fromRevision),
		ToRevision:       targetBOM.Spec.Revision,
		SourceChannel:    sourceChannel,
		TargetChannel:    channel.Spec.Channel,
		BOMPath:          targetBOMPathForChannel,
		BOMDigest:        targetBOMDigest,
		ComponentDigests: componentDigests,
		ValidationCohort: validationCohort,
		Reason:           reason,
		ApprovedBy:       approvedBy,
		ApprovedAt:       approvedAtValue,
		Evidence:         evidence,
		Timeline:         timeline,
	}
	if healthProof != nil {
		healthProofPath := releaseChannelRelativePath(channelPath, opts.HealthProofPath)
		promotion.HealthProofPath = healthProofPath
		promotion.HealthProofDigest = healthProofDigest
		promotion.HealthProofSummary = strings.TrimSpace(healthProof.Spec.Summary)
	}
	channel.Spec.PromotionHistory = append(channel.Spec.PromotionHistory, promotion)
	candidate, candidatePath := newReleaseCandidateRevisionDocument(releaseCandidateRevisionOptions{
		Root:             storeRoot,
		Line:             targetBOM.Metadata.Name,
		Revision:         targetBOM.Spec.Revision,
		SourceChannel:    sourceChannel,
		TargetChannel:    channel.Spec.Channel,
		ReplacesRevision: strings.TrimSpace(fromRevision),
		BOMPath:          targetBOMPath,
		BOMDigest:        targetBOMDigest,
		ComponentDigests: componentDigests,
		ValidationCohort: validationCohort,
		Evidence:         evidence,
		Timeline:         timeline,
		CreatedAt:        approvedAtValue,
	})
	promotionHistory, promotionHistoryPath := newReleasePromotionHistoryDocument(
		releasePromotionHistoryOptions{
			Root:           storeRoot,
			Line:           targetBOM.Metadata.Name,
			Channel:        channel.Spec.Channel,
			Promotion:      promotion,
			PolicyDecision: decision,
			CandidatePath:  candidatePath,
			RecordedAt:     approvedAtValue,
		},
	)

	if err := channel.Validate(); err != nil {
		return nil, fmt.Errorf("validate promoted release channel %q: %w", channelPath, err)
	}
	if err := candidate.Validate(); err != nil {
		return nil, fmt.Errorf("validate release candidate revision %q: %w", candidatePath, err)
	}
	if err := promotionHistory.Validate(); err != nil {
		return nil, fmt.Errorf(
			"validate release promotion history %q: %w",
			promotionHistoryPath,
			err,
		)
	}
	if err := yamlutil.MarshalFile(candidatePath, candidate); err != nil {
		return nil, fmt.Errorf("write release candidate revision %q: %w", candidatePath, err)
	}
	if err := yamlutil.MarshalFile(channelPath, channel); err != nil {
		return nil, fmt.Errorf("write promoted release channel %q: %w", channelPath, err)
	}
	if err := yamlutil.MarshalFile(promotionHistoryPath, promotionHistory); err != nil {
		return nil, fmt.Errorf("write release promotion history %q: %w", promotionHistoryPath, err)
	}

	return &PromoteReleaseChannelResult{
		Channel:              channel,
		BOM:                  targetBOM,
		ChannelPath:          channelPath,
		BOMPath:              targetBOMPath,
		FromRevision:         fromRevision,
		ToRevision:           targetBOM.Spec.Revision,
		Changed:              changed,
		Promotion:            promotion,
		HealthProof:          healthProof,
		Decision:             decision,
		SourceChannel:        sourceChannel,
		Candidate:            candidate,
		CandidatePath:        candidatePath,
		PromotionHistory:     promotionHistory,
		PromotionHistoryPath: promotionHistoryPath,
	}, nil
}

func evaluateDistributionPromotion(
	channel *ReleaseChannelDocument,
	targetBOM *BOM,
	healthProof *DistributionHealthProof,
	sourceChannel ReleaseChannel,
) (*promotionpolicy.Decision, error) {
	if channel == nil {
		return nil, fmt.Errorf("release channel cannot be nil")
	}
	if targetBOM == nil {
		return nil, fmt.Errorf("target BOM cannot be nil")
	}
	if err := sourceChannel.ValidateRequired(); err != nil {
		return nil, fmt.Errorf("source channel: %w", err)
	}
	decision, err := promotionpolicy.EvaluateDefault(promotionpolicy.Request{
		TargetChannel: promotionpolicy.Channel(channel.Spec.Channel),
		Candidate: promotionpolicy.CandidateRevision{
			Line:          targetBOM.Metadata.Name,
			Revision:      targetBOM.Spec.Revision,
			SourceChannel: promotionpolicy.Channel(sourceChannel),
			Replacing:     channel.Spec.TargetRevision,
		},
		HealthProof: distributionPromotionHealthProofSummary(healthProof),
	})
	if err != nil {
		return nil, fmt.Errorf("evaluate promotion policy: %w", err)
	}
	if !decision.Allowed {
		return nil, fmt.Errorf(
			"promotion policy blocked channel %q to revision %q: %s",
			channel.Metadata.Name,
			targetBOM.Spec.Revision,
			distributionPromotionViolationSummary(decision.Violations),
		)
	}
	return decision, nil
}

func distributionPromotionHealthProofSummary(
	proof *DistributionHealthProof,
) promotionpolicy.HealthProofSummary {
	if proof == nil {
		return promotionpolicy.HealthProofSummary{}
	}
	evaluation := EvaluateDistributionHealthProof(proof)
	failedSignals := evaluation.FailedSignals
	optionalFailedSignals := []string(nil)
	if evaluation.HasThresholds {
		failedSignals = evaluation.FailedRequiredSignals
		requiredSet := make(map[string]struct{}, len(evaluation.RequiredSignals))
		for _, name := range evaluation.RequiredSignals {
			requiredSet[name] = struct{}{}
		}
		for _, name := range evaluation.FailedSignals {
			if _, required := requiredSet[name]; !required {
				optionalFailedSignals = append(optionalFailedSignals, name)
			}
		}
	}
	return promotionpolicy.HealthProofSummary{
		Provided:               true,
		Passed:                 proof.Spec.Passed && evaluation.Passed,
		TotalSignals:           evaluation.SignalSummary.TotalSignals,
		PassedSignals:          evaluation.SignalSummary.PassedSignals,
		FailedSignals:          failedSignals,
		OptionalFailedSignals:  optionalFailedSignals,
		RequiredSignals:        evaluation.RequiredSignals,
		FailedRequiredSignals:  evaluation.FailedRequiredSignals,
		MissingRequiredSignals: evaluation.MissingRequiredSignals,
		MinPassedSignals:       evaluation.SignalSummary.MinPassedSignals,
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

func distributionHealthProofForPromotion(
	path string,
	targetBOM *BOM,
) (*DistributionHealthProof, string, error) {
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
		return nil, "", fmt.Errorf(
			"distribution health proof %q line %q does not match target BOM metadata.name %q",
			proof.Metadata.Name,
			proof.Spec.Line,
			targetBOM.Metadata.Name,
		)
	}
	if proof.Spec.TargetRevision != targetBOM.Spec.Revision {
		return nil, "", fmt.Errorf(
			"distribution health proof %q targetRevision %q does not match target BOM spec.revision %q",
			proof.Metadata.Name,
			proof.Spec.TargetRevision,
			targetBOM.Spec.Revision,
		)
	}
	if !proof.Spec.Passed {
		return nil, "", fmt.Errorf("distribution health proof %q did not pass", proof.Metadata.Name)
	}
	if len(proof.Spec.Signals) == 0 {
		return nil, "", fmt.Errorf(
			"distribution health proof %q has no health signals",
			proof.Metadata.Name,
		)
	}
	evaluation := EvaluateDistributionHealthProof(proof)
	if !evaluation.Passed {
		return nil, "", fmt.Errorf(
			"distribution health proof %q did not satisfy evidence thresholds: %s",
			proof.Metadata.Name,
			distributionHealthProofFailureSummary(evaluation),
		)
	}
	return proof, digest, nil
}

func distributionHealthProofFailureSummary(evaluation DistributionHealthProofEvaluation) string {
	parts := make([]string, 0, 4)
	if len(evaluation.FailedRequiredSignals) > 0 {
		parts = append(
			parts,
			"failed required signal(s): "+strings.Join(evaluation.FailedRequiredSignals, ", "),
		)
	}
	if len(evaluation.MissingRequiredSignals) > 0 {
		parts = append(
			parts,
			"missing required signal(s): "+strings.Join(evaluation.MissingRequiredSignals, ", "),
		)
	}
	if len(evaluation.FailedSignals) > 0 && !evaluation.HasThresholds {
		parts = append(parts, "failed signal(s): "+strings.Join(evaluation.FailedSignals, ", "))
	}
	if evaluation.SignalSummary.PassedSignals < evaluation.SignalSummary.MinPassedSignals {
		parts = append(
			parts,
			fmt.Sprintf(
				"passed %d/%d signal(s)",
				evaluation.SignalSummary.PassedSignals,
				evaluation.SignalSummary.MinPassedSignals,
			),
		)
	}
	if len(parts) == 0 {
		return "proof did not pass"
	}
	return strings.Join(parts, "; ")
}

type releaseCandidateRevisionOptions struct {
	Root             string
	Line             string
	Revision         string
	SourceChannel    ReleaseChannel
	TargetChannel    ReleaseChannel
	ReplacesRevision string
	BOMPath          string
	BOMDigest        string
	ComponentDigests []ReleaseComponentDigestRef
	ValidationCohort string
	Evidence         []ReleasePromotionEvidenceRef
	Timeline         []ReleasePromotionTimelineEvent
	CreatedAt        string
}

type releasePromotionHistoryOptions struct {
	Root           string
	Line           string
	Channel        ReleaseChannel
	Promotion      DistributionPromotionRef
	PolicyDecision *promotionpolicy.Decision
	CandidatePath  string
	RecordedAt     string
}

func releaseComponentDigests(doc *BOM) []ReleaseComponentDigestRef {
	if doc == nil {
		return nil
	}
	out := make([]ReleaseComponentDigestRef, 0, len(doc.Spec.Packages))
	for _, pkg := range doc.Spec.Packages {
		out = append(out, ReleaseComponentDigestRef{
			PackageName:  strings.TrimSpace(pkg.Name),
			Category:     strings.TrimSpace(pkg.Category),
			Version:      strings.TrimSpace(pkg.Version),
			ArtifactName: strings.TrimSpace(pkg.Artifact.Name),
			Image:        strings.TrimSpace(pkg.Artifact.Image),
			Digest:       strings.TrimSpace(pkg.Artifact.Digest),
		})
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].PackageName != out[j].PackageName {
			return out[i].PackageName < out[j].PackageName
		}
		if out[i].Version != out[j].Version {
			return out[i].Version < out[j].Version
		}
		return out[i].Digest < out[j].Digest
	})
	return out
}

func releasePromotionEvidence(
	storeRoot, healthProofPath, healthProofDigest string,
	proof *DistributionHealthProof,
) []ReleasePromotionEvidenceRef {
	if proof == nil {
		return nil
	}
	return []ReleasePromotionEvidenceRef{
		{
			Type:    "healthProof",
			Path:    releaseStoreRelativePath(storeRoot, healthProofPath),
			Digest:  strings.TrimSpace(healthProofDigest),
			Summary: strings.TrimSpace(proof.Spec.Summary),
		},
	}
}

func releasePromotionTimeline(
	fromRevision, toRevision string,
	sourceChannel, targetChannel ReleaseChannel,
	reason, actor, at string,
) []ReleasePromotionTimelineEvent {
	return []ReleasePromotionTimelineEvent{
		{
			Type:     "candidateRecorded",
			At:       at,
			Actor:    actor,
			Channel:  sourceChannel,
			Revision: toRevision,
			Reason:   reason,
		},
		{
			Type:     "promotionApproved",
			At:       at,
			Actor:    actor,
			Channel:  targetChannel,
			Revision: toRevision,
			Reason:   reason,
		},
		{
			Type:     "channelPromoted",
			At:       at,
			Actor:    actor,
			Channel:  targetChannel,
			Revision: toRevision,
			Reason:   reason,
			Message:  promotionTimelineMessage(fromRevision, toRevision),
		},
	}
}

func promotionTimelineMessage(fromRevision, toRevision string) string {
	fromRevision = strings.TrimSpace(fromRevision)
	toRevision = strings.TrimSpace(toRevision)
	if fromRevision == "" {
		return "channel target initialized to " + toRevision
	}
	if fromRevision == toRevision {
		return "channel target confirmed at " + toRevision
	}
	return "channel target advanced from " + fromRevision + " to " + toRevision
}

func newReleaseCandidateRevisionDocument(
	opts releaseCandidateRevisionOptions,
) (*ReleaseCandidateRevisionDocument, string) {
	name := releaseMetadataSafeName(opts.Line + "-" + opts.Revision + "-candidate")
	if name == "" {
		name = "candidate"
	}
	path := filepath.Join(
		opts.Root,
		"candidates",
		strings.TrimSpace(opts.Line),
		strings.TrimSpace(opts.Revision),
		"candidate.yaml",
	)
	return &ReleaseCandidateRevisionDocument{
		APIVersion: distribution.APIVersion,
		Kind:       distribution.KindReleaseCandidateRevision,
		Metadata: Metadata{
			Name: name,
		},
		Spec: ReleaseCandidateRevisionSpec{
			Line:             strings.TrimSpace(opts.Line),
			Revision:         strings.TrimSpace(opts.Revision),
			SourceChannel:    opts.SourceChannel,
			TargetChannel:    opts.TargetChannel,
			ReplacesRevision: strings.TrimSpace(opts.ReplacesRevision),
			BOMPath:          releaseStoreRelativePath(opts.Root, opts.BOMPath),
			BOMDigest:        strings.TrimSpace(opts.BOMDigest),
			ComponentDigests: append([]ReleaseComponentDigestRef(nil), opts.ComponentDigests...),
			ValidationCohort: strings.TrimSpace(opts.ValidationCohort),
			Evidence:         append([]ReleasePromotionEvidenceRef(nil), opts.Evidence...),
			Timeline:         append([]ReleasePromotionTimelineEvent(nil), opts.Timeline...),
			CreatedAt:        strings.TrimSpace(opts.CreatedAt),
		},
	}, path
}

func newReleasePromotionHistoryDocument(
	opts releasePromotionHistoryOptions,
) (*ReleasePromotionHistoryDocument, string) {
	recordedAt := strings.TrimSpace(opts.RecordedAt)
	name := releaseMetadataSafeName(
		recordedAt + "-" + opts.Line + "-" + string(opts.Channel) + "-" + opts.Promotion.ToRevision,
	)
	if name == "" {
		name = "promotion"
	}
	path := filepath.Join(
		opts.Root,
		"promotions",
		strings.TrimSpace(opts.Line),
		string(opts.Channel),
		name+".yaml",
	)
	candidateRef := ""
	if strings.TrimSpace(opts.CandidatePath) != "" {
		candidateRef = releaseStoreRelativePath(opts.Root, opts.CandidatePath)
	}
	return &ReleasePromotionHistoryDocument{
		APIVersion: distribution.APIVersion,
		Kind:       distribution.KindReleasePromotionHistory,
		Metadata: Metadata{
			Name: name,
		},
		Spec: ReleasePromotionHistorySpec{
			Line:           strings.TrimSpace(opts.Line),
			Channel:        opts.Channel,
			Promotion:      opts.Promotion,
			PolicyDecision: opts.PolicyDecision,
			CandidateRef:   candidateRef,
			RecordedAt:     recordedAt,
		},
	}, path
}

func releaseStoreRoot(explicitRoot, channelPath string) string {
	if root := strings.TrimSpace(explicitRoot); root != "" {
		return root
	}
	channelDir, err := filepath.Abs(filepath.Dir(channelPath))
	if err != nil {
		return filepath.Dir(channelPath)
	}
	for dir := channelDir; ; dir = filepath.Dir(dir) {
		if filepath.Base(dir) == "channels" {
			return filepath.Dir(dir)
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			break
		}
	}
	return filepath.Dir(channelPath)
}

func releaseStoreRelativePath(root, path string) string {
	rootAbs, err := filepath.Abs(root)
	if err != nil {
		return filepath.ToSlash(filepath.Clean(path))
	}
	pathAbs, err := filepath.Abs(path)
	if err != nil {
		return filepath.ToSlash(filepath.Clean(path))
	}
	rel, err := filepath.Rel(rootAbs, pathAbs)
	if err != nil {
		return pathAbs
	}
	return filepath.ToSlash(rel)
}

func releaseChannelRelativePath(channelPath, path string) string {
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
