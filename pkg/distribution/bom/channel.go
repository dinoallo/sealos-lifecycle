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
	"path/filepath"
	"strings"
	"time"

	"sigs.k8s.io/yaml"

	"github.com/labring/sealos/pkg/distribution"
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
	FromRevision string `json:"fromRevision,omitempty" yaml:"fromRevision,omitempty"`
	ToRevision   string `json:"toRevision" yaml:"toRevision"`
	BOMPath      string `json:"bomPath" yaml:"bomPath"`
	Reason       string `json:"reason" yaml:"reason"`
	ApprovedBy   string `json:"approvedBy" yaml:"approvedBy"`
	ApprovedAt   string `json:"approvedAt" yaml:"approvedAt"`
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
	ChannelPath   string
	TargetBOMPath string
	Reason        string
	ApprovedBy    string
	ApprovedAt    time.Time
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

	fromRevision := channel.Spec.TargetRevision
	targetBOMPathForChannel := distributionChannelRelativeBOMPath(channelPath, targetBOMPath)
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
	}, nil
}

func distributionChannelRelativeBOMPath(channelPath, bomPath string) string {
	channelAbs, err := filepath.Abs(channelPath)
	if err != nil {
		return filepath.ToSlash(filepath.Clean(bomPath))
	}
	bomAbs, err := filepath.Abs(bomPath)
	if err != nil {
		return filepath.ToSlash(filepath.Clean(bomPath))
	}
	rel, err := filepath.Rel(filepath.Dir(channelAbs), bomAbs)
	if err != nil {
		return bomAbs
	}
	return filepath.ToSlash(rel)
}
