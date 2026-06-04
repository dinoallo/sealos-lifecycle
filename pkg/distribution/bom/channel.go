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
	"sort"
	"strings"
	"time"

	"github.com/opencontainers/go-digest"
	"sigs.k8s.io/yaml"

	"github.com/labring/sealos/pkg/distribution"
	promotionpolicy "github.com/labring/sealos/pkg/distribution/promotion"
	fileutil "github.com/labring/sealos/pkg/utils/file"
	yamlutil "github.com/labring/sealos/pkg/utils/yaml"
)

type ReleaseChannelDocumentSpec struct {
	Distribution     string                     `json:"distribution" yaml:"distribution"`
	Channel          ReleaseChannel             `json:"channel" yaml:"channel"`
	TargetRevision   string                     `json:"targetRevision" yaml:"targetRevision"`
	BOMPath          string                     `json:"bomPath" yaml:"bomPath"`
	BOMDigest        string                     `json:"bomDigest,omitempty" yaml:"bomDigest,omitempty"`
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

type ReleaseChannelDocument struct {
	APIVersion string                     `json:"apiVersion" yaml:"apiVersion"`
	Kind       string                     `json:"kind" yaml:"kind"`
	Metadata   Metadata                   `json:"metadata" yaml:"metadata"`
	Spec       ReleaseChannelDocumentSpec `json:"spec" yaml:"spec"`
}

type ResolvedReleaseChannel struct {
	Channel       *ReleaseChannelDocument
	BOM           *BOM
	BOMPath       string
	BOMDigest     string
	ChannelSource string
}

type PromoteReleaseChannelOptions struct {
	ChannelPath     string
	TargetBOMPath   string
	SourceChannel   ReleaseChannel
	HealthProofPath string
	Reason          string
	ApprovedBy      string
	ApprovedAt      time.Time
}

type PromoteReleaseChannelResult struct {
	Channel       *ReleaseChannelDocument
	BOM           *BOM
	ChannelPath   string
	BOMPath       string
	FromRevision  string
	ToRevision    string
	Changed       bool
	Promotion     DistributionPromotionRef
	HealthProof   *DistributionHealthProof
	Decision      *promotionpolicy.Decision
	SourceChannel ReleaseChannel
}

type DistributionHealthProofSpec struct {
	Line           string                           `json:"line" yaml:"line"`
	TargetRevision string                           `json:"targetRevision" yaml:"targetRevision"`
	Passed         bool                             `json:"passed" yaml:"passed"`
	Summary        string                           `json:"summary,omitempty" yaml:"summary,omitempty"`
	CollectedAt    string                           `json:"collectedAt,omitempty" yaml:"collectedAt,omitempty"`
	Thresholds     DistributionHealthThresholds     `json:"thresholds,omitempty" yaml:"thresholds,omitempty"`
	SignalSummary  *DistributionHealthSignalSummary `json:"signalSummary,omitempty" yaml:"signalSummary,omitempty"`
	Signals        []DistributionHealthSignal       `json:"signals,omitempty" yaml:"signals,omitempty"`
}

type DistributionHealthThresholds struct {
	RequiredSignals  []string `json:"requiredSignals,omitempty" yaml:"requiredSignals,omitempty"`
	MinPassedSignals int      `json:"minPassedSignals,omitempty" yaml:"minPassedSignals,omitempty"`
}

type DistributionHealthSignalSummary struct {
	TotalSignals           int `json:"totalSignals" yaml:"totalSignals"`
	PassedSignals          int `json:"passedSignals" yaml:"passedSignals"`
	FailedSignals          int `json:"failedSignals" yaml:"failedSignals"`
	RequiredSignals        int `json:"requiredSignals,omitempty" yaml:"requiredSignals,omitempty"`
	PassedRequiredSignals  int `json:"passedRequiredSignals,omitempty" yaml:"passedRequiredSignals,omitempty"`
	FailedRequiredSignals  int `json:"failedRequiredSignals,omitempty" yaml:"failedRequiredSignals,omitempty"`
	MissingRequiredSignals int `json:"missingRequiredSignals,omitempty" yaml:"missingRequiredSignals,omitempty"`
	MinPassedSignals       int `json:"minPassedSignals,omitempty" yaml:"minPassedSignals,omitempty"`
}

type DistributionHealthSignal struct {
	Name        string `json:"name" yaml:"name"`
	Passed      bool   `json:"passed" yaml:"passed"`
	Required    bool   `json:"required,omitempty" yaml:"required,omitempty"`
	Source      string `json:"source,omitempty" yaml:"source,omitempty"`
	EvidenceRef string `json:"evidenceRef,omitempty" yaml:"evidenceRef,omitempty"`
	Message     string `json:"message,omitempty" yaml:"message,omitempty"`
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

func NewReleaseChannel(name, distributionLine string, channel ReleaseChannel, targetRevision, bomPath string) *ReleaseChannelDocument {
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
	if p.Spec.Thresholds.MinPassedSignals < 0 {
		return fmt.Errorf("spec.thresholds.minPassedSignals must be non-negative")
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

func EvaluateDistributionHealthProof(proof *DistributionHealthProof) DistributionHealthProofEvaluation {
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
		return fmt.Errorf("release channel cannot be nil")
	}
	if doc == nil {
		return fmt.Errorf("BOM cannot be nil")
	}
	if doc.Metadata.Name != channel.Distribution() {
		return fmt.Errorf("release channel %q distribution %q does not match BOM metadata.name %q", channel.Metadata.Name, channel.Distribution(), doc.Metadata.Name)
	}
	if doc.Spec.Revision != channel.Spec.TargetRevision {
		return fmt.Errorf("release channel %q targetRevision %q does not match BOM spec.revision %q", channel.Metadata.Name, channel.Spec.TargetRevision, doc.Spec.Revision)
	}
	return nil
}

func verifyReleaseChannelBOMDigest(channel *ReleaseChannelDocument, actual string) error {
	if channel == nil {
		return fmt.Errorf("release channel cannot be nil")
	}
	expected := strings.TrimSpace(channel.Spec.BOMDigest)
	if expected == "" {
		return nil
	}
	if actual != expected {
		return fmt.Errorf("release channel %q BOM digest mismatch: expected %s, got %s", channel.Metadata.Name, expected, actual)
	}
	return nil
}

func PromoteReleaseChannelFile(opts PromoteReleaseChannelOptions) (*PromoteReleaseChannelResult, error) {
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
		return nil, fmt.Errorf("release channel %q distribution %q does not match target BOM metadata.name %q", channel.Metadata.Name, channel.Distribution(), targetBOM.Metadata.Name)
	}
	healthProof, healthProofDigest, err := distributionHealthProofForPromotion(opts.HealthProofPath, targetBOM)
	if err != nil {
		return nil, err
	}

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
	changed := fromRevision != targetBOM.Spec.Revision || strings.TrimSpace(channel.Spec.BOMPath) != targetBOMPathForChannel
	channel.Spec.TargetRevision = targetBOM.Spec.Revision
	channel.Spec.BOMPath = targetBOMPathForChannel
	channel.Spec.BOMDigest = targetBOMDigest
	channel.Normalize()

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
		healthProofPath := releaseChannelRelativePath(channelPath, opts.HealthProofPath)
		promotion.HealthProofPath = healthProofPath
		promotion.HealthProofDigest = healthProofDigest
		promotion.HealthProofSummary = strings.TrimSpace(healthProof.Spec.Summary)
	}
	channel.Spec.PromotionHistory = append(channel.Spec.PromotionHistory, promotion)

	if err := channel.Validate(); err != nil {
		return nil, fmt.Errorf("validate promoted release channel %q: %w", channelPath, err)
	}
	if err := yamlutil.MarshalFile(channelPath, channel); err != nil {
		return nil, fmt.Errorf("write promoted release channel %q: %w", channelPath, err)
	}

	return &PromoteReleaseChannelResult{
		Channel:       channel,
		BOM:           targetBOM,
		ChannelPath:   channelPath,
		BOMPath:       targetBOMPath,
		FromRevision:  fromRevision,
		ToRevision:    targetBOM.Spec.Revision,
		Changed:       changed,
		Promotion:     promotion,
		HealthProof:   healthProof,
		Decision:      decision,
		SourceChannel: sourceChannel,
	}, nil
}

func evaluateDistributionPromotion(channel *ReleaseChannelDocument, targetBOM *BOM, healthProof *DistributionHealthProof, sourceChannel ReleaseChannel) (*promotionpolicy.Decision, error) {
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
		return nil, fmt.Errorf("promotion policy blocked channel %q to revision %q: %s", channel.Metadata.Name, targetBOM.Spec.Revision, distributionPromotionViolationSummary(decision.Violations))
	}
	return decision, nil
}

func distributionPromotionHealthProofSummary(proof *DistributionHealthProof) promotionpolicy.HealthProofSummary {
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
	evaluation := EvaluateDistributionHealthProof(proof)
	if !evaluation.Passed {
		return nil, "", fmt.Errorf("distribution health proof %q did not satisfy evidence thresholds: %s", proof.Metadata.Name, distributionHealthProofFailureSummary(evaluation))
	}
	return proof, digest, nil
}

func distributionHealthProofFailureSummary(evaluation DistributionHealthProofEvaluation) string {
	parts := make([]string, 0, 4)
	if len(evaluation.FailedRequiredSignals) > 0 {
		parts = append(parts, "failed required signal(s): "+strings.Join(evaluation.FailedRequiredSignals, ", "))
	}
	if len(evaluation.MissingRequiredSignals) > 0 {
		parts = append(parts, "missing required signal(s): "+strings.Join(evaluation.MissingRequiredSignals, ", "))
	}
	if len(evaluation.FailedSignals) > 0 && !evaluation.HasThresholds {
		parts = append(parts, "failed signal(s): "+strings.Join(evaluation.FailedSignals, ", "))
	}
	if evaluation.SignalSummary.PassedSignals < evaluation.SignalSummary.MinPassedSignals {
		parts = append(parts, fmt.Sprintf("passed %d/%d signal(s)", evaluation.SignalSummary.PassedSignals, evaluation.SignalSummary.MinPassedSignals))
	}
	if len(parts) == 0 {
		return "proof did not pass"
	}
	return strings.Join(parts, "; ")
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
