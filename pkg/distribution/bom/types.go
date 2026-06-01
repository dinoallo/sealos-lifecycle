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
	"path"
	"strings"

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

type SourceReference struct {
	Path   string `json:"path,omitempty" yaml:"path,omitempty"`
	Digest string `json:"digest,omitempty" yaml:"digest,omitempty"`
}

type BuildReference struct {
	Class   string `json:"class,omitempty" yaml:"class,omitempty"`
	Profile string `json:"profile,omitempty" yaml:"profile,omitempty"`
}

type Package struct {
	Category     string            `json:"category" yaml:"category"`
	Name         string            `json:"name" yaml:"name"`
	Version      string            `json:"version" yaml:"version"`
	Source       SourceReference   `json:"source,omitempty" yaml:"source,omitempty"`
	Build        BuildReference    `json:"build,omitempty" yaml:"build,omitempty"`
	Artifact     ArtifactReference `json:"artifact" yaml:"artifact"`
	Dependencies []string          `json:"dependencies,omitempty" yaml:"dependencies,omitempty"`
	Required     bool              `json:"required,omitempty" yaml:"required,omitempty"`
}

type Spec struct {
	Revision         string              `json:"revision" yaml:"revision"`
	LocalPatchPolicy string              `json:"localPatchPolicy,omitempty" yaml:"localPatchPolicy,omitempty"`
	BaseArtifacts    []ArtifactReference `json:"baseArtifacts,omitempty" yaml:"baseArtifacts,omitempty"`
	Packages         []Package           `json:"packages" yaml:"packages"`
}

type BOM struct {
	APIVersion     string         `json:"apiVersion" yaml:"apiVersion"`
	Kind           string         `json:"kind" yaml:"kind"`
	Metadata       Metadata       `json:"metadata" yaml:"metadata"`
	Spec           Spec           `json:"spec" yaml:"spec"`
	RuntimeChannel ReleaseChannel `json:"-" yaml:"-"`
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
		},
		RuntimeChannel: channel,
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
	if b.Spec.LocalPatchPolicy != "" {
		if err := validateRelativePath("spec.localPatchPolicy", b.Spec.LocalPatchPolicy); err != nil {
			return err
		}
	}
	if len(b.Spec.Packages) == 0 {
		return fmt.Errorf("spec.packages cannot be empty")
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

	packageNames := make(map[string]struct{}, len(b.Spec.Packages))
	packageIdentities := make(map[string]struct{}, len(b.Spec.Packages))
	for i, pkg := range b.Spec.Packages {
		if err := pkg.Validate(); err != nil {
			return fmt.Errorf("spec.packages[%d]: %w", i, err)
		}
		if _, ok := packageNames[pkg.Name]; ok {
			return fmt.Errorf("spec.packages[%d]: duplicate package name %q", i, pkg.Name)
		}
		packageNames[pkg.Name] = struct{}{}
		identity := pkg.Identity()
		if _, ok := packageIdentities[identity]; ok {
			return fmt.Errorf("spec.packages[%d]: duplicate package identity %q", i, identity)
		}
		packageIdentities[identity] = struct{}{}
	}

	for i, pkg := range b.Spec.Packages {
		for _, dependency := range pkg.Dependencies {
			if _, ok := packageNames[dependency]; !ok {
				return fmt.Errorf("spec.packages[%d]: unknown dependency %q", i, dependency)
			}
		}
	}

	return nil
}

func (c ReleaseChannel) Validate() error {
	if c == "" {
		return nil
	}
	switch c {
	case ChannelAlpha, ChannelBeta, ChannelStable:
		return nil
	default:
		return fmt.Errorf("invalid release channel %q", c)
	}
}

func (c ReleaseChannel) ValidateRequired() error {
	if c == "" {
		return fmt.Errorf("release channel cannot be empty")
	}
	return c.Validate()
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

func (p Package) Identity() string {
	return p.Category + "/" + p.Name + "@" + p.Version
}

func (p Package) Validate() error {
	if p.Category == "" {
		return fmt.Errorf("category cannot be empty")
	}
	if p.Name == "" {
		return fmt.Errorf("name cannot be empty")
	}
	if p.Version == "" {
		return fmt.Errorf("version cannot be empty")
	}
	if p.Source.Path != "" {
		if err := validateRelativePath("source.path", p.Source.Path); err != nil {
			return err
		}
	}
	if p.Source.Digest != "" {
		if _, err := digest.Parse(p.Source.Digest); err != nil {
			return fmt.Errorf("source.digest: invalid digest %q: %w", p.Source.Digest, err)
		}
	}
	if err := p.Artifact.Validate(); err != nil {
		return fmt.Errorf("artifact: %w", err)
	}

	dependencies := make(map[string]struct{}, len(p.Dependencies))
	for _, dependency := range p.Dependencies {
		if dependency == "" {
			return fmt.Errorf("dependencies cannot contain empty values")
		}
		if dependency == p.Name {
			return fmt.Errorf("dependency %q cannot refer to itself", dependency)
		}
		if _, ok := dependencies[dependency]; ok {
			return fmt.Errorf("duplicate dependency %q", dependency)
		}
		dependencies[dependency] = struct{}{}
	}
	return nil
}

func (b BOM) Channel() ReleaseChannel {
	return b.RuntimeChannel
}

func (b *BOM) SetRuntimeChannel(channel ReleaseChannel) {
	if b == nil {
		return
	}
	b.RuntimeChannel = channel
}

func (b BOM) Packages() []Package {
	return append([]Package(nil), b.Spec.Packages...)
}

func (b BOM) PackageIndex() map[string]Package {
	packages := b.Packages()
	index := make(map[string]Package, len(packages))
	for _, pkg := range packages {
		index[pkg.Name] = pkg
	}
	return index
}

func (b BOM) PackageCount() int {
	return len(b.Spec.Packages)
}

func validateRelativePath(field, value string) error {
	if value == "" {
		return fmt.Errorf("%s cannot be empty", field)
	}
	if strings.HasPrefix(value, "/") {
		return fmt.Errorf("%s must be relative, got %q", field, value)
	}
	cleaned := path.Clean(value)
	if cleaned == "." || cleaned == "" {
		return fmt.Errorf("%s cannot be empty", field)
	}
	if strings.HasPrefix(cleaned, "../") || cleaned == ".." {
		return fmt.Errorf("%s cannot escape the BOM root, got %q", field, value)
	}
	return nil
}
