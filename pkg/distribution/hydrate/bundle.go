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

package hydrate

import (
	"fmt"

	"sigs.k8s.io/yaml"

	"github.com/labring/sealos/pkg/distribution"
	"github.com/labring/sealos/pkg/distribution/bom"
	"github.com/labring/sealos/pkg/distribution/packageformat"
)

const BundleFileName = "bundle.yaml"

type Metadata struct {
	Name   string            `json:"name" yaml:"name"`
	Labels map[string]string `json:"labels,omitempty" yaml:"labels,omitempty"`
}

type Bundle struct {
	APIVersion string     `json:"apiVersion" yaml:"apiVersion"`
	Kind       string     `json:"kind" yaml:"kind"`
	Metadata   Metadata   `json:"metadata" yaml:"metadata"`
	Spec       BundleSpec `json:"spec" yaml:"spec"`
}

type BundleSpec struct {
	BOMName    string              `json:"bomName" yaml:"bomName"`
	Revision   string              `json:"revision" yaml:"revision"`
	Channel    bom.ReleaseChannel  `json:"channel" yaml:"channel"`
	Components []RenderedComponent `json:"components" yaml:"components"`
}

type RenderedComponent struct {
	Name         string                     `json:"name" yaml:"name"`
	PackageName  string                     `json:"packageName" yaml:"packageName"`
	Version      string                     `json:"version" yaml:"version"`
	Class        packageformat.PackageClass `json:"class" yaml:"class"`
	Artifact     string                     `json:"artifact" yaml:"artifact"`
	Dependencies []string                   `json:"dependencies,omitempty" yaml:"dependencies,omitempty"`
	Inputs       []packageformat.Input      `json:"inputs,omitempty" yaml:"inputs,omitempty"`
	ManifestPath string                     `json:"manifestPath" yaml:"manifestPath"`
	RootPath     string                     `json:"rootPath" yaml:"rootPath"`
	Steps        []RenderedStep             `json:"steps" yaml:"steps"`
}

type RenderedStep struct {
	Name           string                        `json:"name" yaml:"name"`
	Kind           StepKind                      `json:"kind" yaml:"kind"`
	BundlePath     string                        `json:"bundlePath" yaml:"bundlePath"`
	SourcePath     string                        `json:"sourcePath" yaml:"sourcePath"`
	ContentType    packageformat.ContentType     `json:"contentType,omitempty" yaml:"contentType,omitempty"`
	HookPhase      packageformat.HookPhase       `json:"hookPhase,omitempty" yaml:"hookPhase,omitempty"`
	Target         packageformat.ExecutionTarget `json:"target,omitempty" yaml:"target,omitempty"`
	MediaType      string                        `json:"mediaType,omitempty" yaml:"mediaType,omitempty"`
	Required       bool                          `json:"required,omitempty" yaml:"required,omitempty"`
	Args           []string                      `json:"args,omitempty" yaml:"args,omitempty"`
	TimeoutSeconds int32                         `json:"timeoutSeconds,omitempty" yaml:"timeoutSeconds,omitempty"`
}

func NewBundle(plan *Plan) *Bundle {
	bundle := &Bundle{
		APIVersion: distribution.APIVersion,
		Kind:       distribution.KindHydratedBundle,
	}
	if plan == nil {
		return bundle
	}

	bundle.Metadata = Metadata{
		Name: fmt.Sprintf("%s-%s", plan.BOMName, plan.Revision),
		Labels: map[string]string{
			"distribution.sealos.io/bom-name": plan.BOMName,
			"distribution.sealos.io/revision": plan.Revision,
			"distribution.sealos.io/channel":  string(plan.Channel),
		},
	}
	bundle.Spec = BundleSpec{
		BOMName:    plan.BOMName,
		Revision:   plan.Revision,
		Channel:    plan.Channel,
		Components: make([]RenderedComponent, 0, len(plan.Components)),
	}
	return bundle
}

func (b Bundle) String() string {
	data, _ := yaml.Marshal(b)
	return string(data)
}
