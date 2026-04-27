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

package packageformat

import (
	"fmt"
	"path"
	"strings"

	"sigs.k8s.io/yaml"

	"github.com/labring/sealos/pkg/distribution"
)

type PackageClass string

const (
	ClassRootfs      PackageClass = "rootfs"
	ClassPatch       PackageClass = "patch"
	ClassApplication PackageClass = "application"
)

type ContentType string

const (
	ContentRootfs   ContentType = "rootfs"
	ContentManifest ContentType = "manifest"
	ContentChart    ContentType = "chart"
	ContentPatch    ContentType = "patch"
	ContentFile     ContentType = "file"
	ContentValues   ContentType = "values"
	ContentHook     ContentType = "hook"
)

type HookPhase string

const (
	PhaseBootstrap HookPhase = "bootstrap"
	PhaseConfigure HookPhase = "configure"
	PhaseInstall   HookPhase = "install"
	PhaseUpgrade   HookPhase = "upgrade"
	PhaseRemove    HookPhase = "remove"
	PhaseHealth    HookPhase = "healthcheck"
)

type ExecutionTarget string

const (
	TargetAllNodes    ExecutionTarget = "allNodes"
	TargetFirstMaster ExecutionTarget = "firstMaster"
	TargetCluster     ExecutionTarget = "cluster"
)

type InputType string

const (
	InputConfigFile InputType = "configFile"
	InputValuesFile InputType = "valuesFile"
	InputEnv        InputType = "env"
)

type Metadata struct {
	Name   string            `json:"name" yaml:"name"`
	Labels map[string]string `json:"labels,omitempty" yaml:"labels,omitempty"`
}

type Platform struct {
	OS   string `json:"os" yaml:"os"`
	Arch string `json:"arch" yaml:"arch"`
}

type Dependency struct {
	Name     string `json:"name" yaml:"name"`
	Version  string `json:"version,omitempty" yaml:"version,omitempty"`
	Optional bool   `json:"optional,omitempty" yaml:"optional,omitempty"`
}

type Compatibility struct {
	Kubernetes string     `json:"kubernetes,omitempty" yaml:"kubernetes,omitempty"`
	Sealos     string     `json:"sealos,omitempty" yaml:"sealos,omitempty"`
	Platforms  []Platform `json:"platforms,omitempty" yaml:"platforms,omitempty"`
}

type Input struct {
	Name     string    `json:"name" yaml:"name"`
	Type     InputType `json:"type" yaml:"type"`
	Path     string    `json:"path" yaml:"path"`
	Format   string    `json:"format,omitempty" yaml:"format,omitempty"`
	Required bool      `json:"required,omitempty" yaml:"required,omitempty"`
}

type Content struct {
	Name      string      `json:"name" yaml:"name"`
	Type      ContentType `json:"type" yaml:"type"`
	Path      string      `json:"path" yaml:"path"`
	MediaType string      `json:"mediaType,omitempty" yaml:"mediaType,omitempty"`
	Required  bool        `json:"required,omitempty" yaml:"required,omitempty"`
}

type Hook struct {
	Name           string          `json:"name" yaml:"name"`
	Phase          HookPhase       `json:"phase" yaml:"phase"`
	Target         ExecutionTarget `json:"target" yaml:"target"`
	Path           string          `json:"path" yaml:"path"`
	Args           []string        `json:"args,omitempty" yaml:"args,omitempty"`
	TimeoutSeconds int32           `json:"timeoutSeconds,omitempty" yaml:"timeoutSeconds,omitempty"`
}

type Spec struct {
	Component     string        `json:"component" yaml:"component"`
	Version       string        `json:"version" yaml:"version"`
	Class         PackageClass  `json:"class" yaml:"class"`
	Dependencies  []Dependency  `json:"dependencies,omitempty" yaml:"dependencies,omitempty"`
	Compatibility Compatibility `json:"compatibility,omitempty" yaml:"compatibility,omitempty"`
	Inputs        []Input       `json:"inputs,omitempty" yaml:"inputs,omitempty"`
	Contents      []Content     `json:"contents" yaml:"contents"`
	Hooks         []Hook        `json:"hooks,omitempty" yaml:"hooks,omitempty"`
}

type ComponentPackage struct {
	APIVersion string   `json:"apiVersion" yaml:"apiVersion"`
	Kind       string   `json:"kind" yaml:"kind"`
	Metadata   Metadata `json:"metadata" yaml:"metadata"`
	Spec       Spec     `json:"spec" yaml:"spec"`
}

func New(name, component, version string, class PackageClass) *ComponentPackage {
	return &ComponentPackage{
		APIVersion: distribution.APIVersion,
		Kind:       distribution.KindComponentPackage,
		Metadata: Metadata{
			Name: name,
		},
		Spec: Spec{
			Component: component,
			Version:   version,
			Class:     class,
		},
	}
}

func (p ComponentPackage) String() string {
	data, _ := yaml.Marshal(p)
	return string(data)
}

func (p ComponentPackage) Validate() error {
	if p.APIVersion != distribution.APIVersion {
		return fmt.Errorf("unsupported apiVersion %q", p.APIVersion)
	}
	if p.Kind != distribution.KindComponentPackage {
		return fmt.Errorf("unsupported kind %q", p.Kind)
	}
	if p.Metadata.Name == "" {
		return fmt.Errorf("metadata.name cannot be empty")
	}
	if err := p.Spec.Validate(); err != nil {
		return fmt.Errorf("spec: %w", err)
	}
	return nil
}

func (s Spec) Validate() error {
	if s.Component == "" {
		return fmt.Errorf("component cannot be empty")
	}
	if s.Version == "" {
		return fmt.Errorf("version cannot be empty")
	}
	if err := s.Class.Validate(); err != nil {
		return fmt.Errorf("class: %w", err)
	}
	if len(s.Contents) == 0 {
		return fmt.Errorf("contents cannot be empty")
	}

	dependencies := make(map[string]struct{}, len(s.Dependencies))
	for i, dependency := range s.Dependencies {
		if err := dependency.Validate(); err != nil {
			return fmt.Errorf("dependencies[%d]: %w", i, err)
		}
		if _, ok := dependencies[dependency.Name]; ok {
			return fmt.Errorf("dependencies[%d]: duplicate dependency %q", i, dependency.Name)
		}
		dependencies[dependency.Name] = struct{}{}
	}

	inputNames := make(map[string]struct{}, len(s.Inputs))
	inputPaths := make(map[string]struct{}, len(s.Inputs))
	for i, input := range s.Inputs {
		if err := input.Validate(); err != nil {
			return fmt.Errorf("inputs[%d]: %w", i, err)
		}
		if _, ok := inputNames[input.Name]; ok {
			return fmt.Errorf("inputs[%d]: duplicate input name %q", i, input.Name)
		}
		if _, ok := inputPaths[input.Path]; ok {
			return fmt.Errorf("inputs[%d]: duplicate input path %q", i, input.Path)
		}
		inputNames[input.Name] = struct{}{}
		inputPaths[input.Path] = struct{}{}
	}

	contentNames := make(map[string]struct{}, len(s.Contents))
	contentPaths := make(map[string]struct{}, len(s.Contents))
	var hasRootfsContent bool
	for i, content := range s.Contents {
		if err := content.Validate(); err != nil {
			return fmt.Errorf("contents[%d]: %w", i, err)
		}
		if _, ok := contentNames[content.Name]; ok {
			return fmt.Errorf("contents[%d]: duplicate content name %q", i, content.Name)
		}
		if _, ok := contentPaths[content.Path]; ok {
			return fmt.Errorf("contents[%d]: duplicate content path %q", i, content.Path)
		}
		contentNames[content.Name] = struct{}{}
		contentPaths[content.Path] = struct{}{}
		if content.Type == ContentRootfs {
			hasRootfsContent = true
		}
	}

	hookNames := make(map[string]struct{}, len(s.Hooks))
	for i, hook := range s.Hooks {
		if err := hook.Validate(); err != nil {
			return fmt.Errorf("hooks[%d]: %w", i, err)
		}
		if _, ok := hookNames[hook.Name]; ok {
			return fmt.Errorf("hooks[%d]: duplicate hook name %q", i, hook.Name)
		}
		hookNames[hook.Name] = struct{}{}
	}

	switch s.Class {
	case ClassRootfs:
		if !hasRootfsContent {
			return fmt.Errorf("rootfs packages must include at least one rootfs content entry")
		}
	case ClassPatch, ClassApplication:
		if hasRootfsContent {
			return fmt.Errorf("%s packages cannot include rootfs content entries", s.Class)
		}
	}

	return nil
}

func (c PackageClass) Validate() error {
	switch c {
	case ClassRootfs, ClassPatch, ClassApplication:
		return nil
	default:
		return fmt.Errorf("invalid package class %q", c)
	}
}

func (t ContentType) Validate() error {
	switch t {
	case ContentRootfs, ContentManifest, ContentChart, ContentPatch, ContentFile, ContentValues, ContentHook:
		return nil
	default:
		return fmt.Errorf("invalid content type %q", t)
	}
}

func (t InputType) Validate() error {
	switch t {
	case InputConfigFile, InputValuesFile, InputEnv:
		return nil
	default:
		return fmt.Errorf("invalid input type %q", t)
	}
}

func (p HookPhase) Validate() error {
	switch p {
	case PhaseBootstrap, PhaseConfigure, PhaseInstall, PhaseUpgrade, PhaseRemove, PhaseHealth:
		return nil
	default:
		return fmt.Errorf("invalid hook phase %q", p)
	}
}

func (t ExecutionTarget) Validate() error {
	switch t {
	case TargetAllNodes, TargetFirstMaster, TargetCluster:
		return nil
	default:
		return fmt.Errorf("invalid execution target %q", t)
	}
}

func (d Dependency) Validate() error {
	if d.Name == "" {
		return fmt.Errorf("name cannot be empty")
	}
	return nil
}

func (p Platform) Validate() error {
	if p.OS == "" {
		return fmt.Errorf("os cannot be empty")
	}
	if p.Arch == "" {
		return fmt.Errorf("arch cannot be empty")
	}
	return nil
}

func (c Compatibility) Validate() error {
	for i, platform := range c.Platforms {
		if err := platform.Validate(); err != nil {
			return fmt.Errorf("platforms[%d]: %w", i, err)
		}
	}
	return nil
}

func (i Input) Validate() error {
	if i.Name == "" {
		return fmt.Errorf("name cannot be empty")
	}
	if err := i.Type.Validate(); err != nil {
		return fmt.Errorf("type: %w", err)
	}
	if err := validateRelativePath("path", i.Path); err != nil {
		return err
	}
	return nil
}

func (c Content) Validate() error {
	if c.Name == "" {
		return fmt.Errorf("name cannot be empty")
	}
	if err := c.Type.Validate(); err != nil {
		return fmt.Errorf("type: %w", err)
	}
	if err := validateRelativePath("path", c.Path); err != nil {
		return err
	}
	return nil
}

func (h Hook) Validate() error {
	if h.Name == "" {
		return fmt.Errorf("name cannot be empty")
	}
	if err := h.Phase.Validate(); err != nil {
		return fmt.Errorf("phase: %w", err)
	}
	if err := h.Target.Validate(); err != nil {
		return fmt.Errorf("target: %w", err)
	}
	if err := validateRelativePath("path", h.Path); err != nil {
		return err
	}
	if h.TimeoutSeconds < 0 {
		return fmt.Errorf("timeoutSeconds cannot be negative")
	}
	return nil
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
		return fmt.Errorf("%s cannot escape the package root, got %q", field, value)
	}
	return nil
}
