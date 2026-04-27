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

	"github.com/opencontainers/go-digest"
	"sigs.k8s.io/yaml"

	"github.com/labring/sealos/pkg/distribution"
)

type ReleaseChannel string

const (
	ChannelAlpha  ReleaseChannel = "alpha"
	ChannelBeta   ReleaseChannel = "beta"
	ChannelStable ReleaseChannel = "stable"
)

type Metadata struct {
	Name   string            `json:"name" yaml:"name"`
	Labels map[string]string `json:"labels,omitempty" yaml:"labels,omitempty"`
}

type ArtifactReference struct {
	Name     string `json:"name" yaml:"name"`
	Image    string `json:"image" yaml:"image"`
	Digest   string `json:"digest" yaml:"digest"`
	Platform string `json:"platform,omitempty" yaml:"platform,omitempty"`
	Source   string `json:"source,omitempty" yaml:"source,omitempty"`
}

type Component struct {
	Name         string            `json:"name" yaml:"name"`
	Kind         string            `json:"kind" yaml:"kind"`
	Version      string            `json:"version" yaml:"version"`
	Artifact     ArtifactReference `json:"artifact" yaml:"artifact"`
	Dependencies []string          `json:"dependencies,omitempty" yaml:"dependencies,omitempty"`
	Required     bool              `json:"required,omitempty" yaml:"required,omitempty"`
}

type Spec struct {
	Revision      string              `json:"revision" yaml:"revision"`
	Channel       ReleaseChannel      `json:"channel" yaml:"channel"`
	BaseArtifacts []ArtifactReference `json:"baseArtifacts,omitempty" yaml:"baseArtifacts,omitempty"`
	Components    []Component         `json:"components" yaml:"components"`
}

type BOM struct {
	APIVersion string   `json:"apiVersion" yaml:"apiVersion"`
	Kind       string   `json:"kind" yaml:"kind"`
	Metadata   Metadata `json:"metadata" yaml:"metadata"`
	Spec       Spec     `json:"spec" yaml:"spec"`
}

func New(name, revision string, channel ReleaseChannel) *BOM {
	return &BOM{
		APIVersion: distribution.APIVersion,
		Kind:       distribution.KindBOM,
		Metadata: Metadata{
			Name: name,
		},
		Spec: Spec{
			Revision: revision,
			Channel:  channel,
		},
	}
}

func (b BOM) String() string {
	data, _ := yaml.Marshal(b)
	return string(data)
}

func (b BOM) Validate() error {
	if b.APIVersion != distribution.APIVersion {
		return fmt.Errorf("unsupported apiVersion %q", b.APIVersion)
	}
	if b.Kind != distribution.KindBOM {
		return fmt.Errorf("unsupported kind %q", b.Kind)
	}
	if b.Metadata.Name == "" {
		return fmt.Errorf("metadata.name cannot be empty")
	}
	if b.Spec.Revision == "" {
		return fmt.Errorf("spec.revision cannot be empty")
	}
	if err := b.Spec.Channel.Validate(); err != nil {
		return fmt.Errorf("spec.channel: %w", err)
	}
	if len(b.Spec.Components) == 0 {
		return fmt.Errorf("spec.components cannot be empty")
	}

	artifactNames := make(map[string]struct{}, len(b.Spec.BaseArtifacts))
	for i, artifact := range b.Spec.BaseArtifacts {
		if err := artifact.Validate(); err != nil {
			return fmt.Errorf("spec.baseArtifacts[%d]: %w", i, err)
		}
		if _, ok := artifactNames[artifact.Name]; ok {
			return fmt.Errorf("spec.baseArtifacts[%d]: duplicate artifact name %q", i, artifact.Name)
		}
		artifactNames[artifact.Name] = struct{}{}
	}

	componentNames := make(map[string]struct{}, len(b.Spec.Components))
	for i, component := range b.Spec.Components {
		if err := component.Validate(); err != nil {
			return fmt.Errorf("spec.components[%d]: %w", i, err)
		}
		if _, ok := componentNames[component.Name]; ok {
			return fmt.Errorf("spec.components[%d]: duplicate component name %q", i, component.Name)
		}
		componentNames[component.Name] = struct{}{}
	}

	for i, component := range b.Spec.Components {
		for _, dependency := range component.Dependencies {
			if _, ok := componentNames[dependency]; !ok {
				return fmt.Errorf("spec.components[%d]: unknown dependency %q", i, dependency)
			}
		}
	}

	return nil
}

func (c ReleaseChannel) Validate() error {
	switch c {
	case ChannelAlpha, ChannelBeta, ChannelStable:
		return nil
	default:
		return fmt.Errorf("invalid release channel %q", c)
	}
}

func (a ArtifactReference) Validate() error {
	if a.Name == "" {
		return fmt.Errorf("name cannot be empty")
	}
	if a.Image == "" {
		return fmt.Errorf("image cannot be empty")
	}
	if a.Digest == "" {
		return fmt.Errorf("digest cannot be empty")
	}
	if _, err := digest.Parse(a.Digest); err != nil {
		return fmt.Errorf("invalid digest %q: %w", a.Digest, err)
	}
	return nil
}

func (c Component) Validate() error {
	if c.Name == "" {
		return fmt.Errorf("name cannot be empty")
	}
	if c.Kind == "" {
		return fmt.Errorf("kind cannot be empty")
	}
	if c.Version == "" {
		return fmt.Errorf("version cannot be empty")
	}
	if err := c.Artifact.Validate(); err != nil {
		return fmt.Errorf("artifact: %w", err)
	}

	dependencies := make(map[string]struct{}, len(c.Dependencies))
	for _, dependency := range c.Dependencies {
		if dependency == "" {
			return fmt.Errorf("dependencies cannot contain empty values")
		}
		if dependency == c.Name {
			return fmt.Errorf("dependency %q cannot refer to itself", dependency)
		}
		if _, ok := dependencies[dependency]; ok {
			return fmt.Errorf("duplicate dependency %q", dependency)
		}
		dependencies[dependency] = struct{}{}
	}
	return nil
}
