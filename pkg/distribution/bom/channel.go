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

	"sigs.k8s.io/yaml"

	"github.com/labring/sealos/pkg/distribution"
	fileutil "github.com/labring/sealos/pkg/utils/file"
	yamlutil "github.com/labring/sealos/pkg/utils/yaml"
)

type DistributionChannelSpec struct {
	Line           string         `json:"line" yaml:"line"`
	Channel        ReleaseChannel `json:"channel" yaml:"channel"`
	TargetRevision string         `json:"targetRevision" yaml:"targetRevision"`
	BOMPath        string         `json:"bomPath" yaml:"bomPath"`
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
