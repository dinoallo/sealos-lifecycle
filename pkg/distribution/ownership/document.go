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

package ownership

import (
	"fmt"
	"os"
	"strings"

	"sigs.k8s.io/yaml"
)

const (
	LocalPatchPolicyAPIVersion = "distribution.sealos.io/v1alpha1"
	LocalPatchPolicyKind       = "LocalPatchPolicy"
	LocalPatchPolicyFileName   = "local-patch-policy.yaml"
)

type LocalPatchPolicySource string

const (
	LocalPatchPolicySourceBuiltInDefault LocalPatchPolicySource = "builtInDefault"
	LocalPatchPolicySourceLocalRepo      LocalPatchPolicySource = "localRepo"
	LocalPatchPolicySourceBOM            LocalPatchPolicySource = "bom"
	LocalPatchPolicySourcePackage        LocalPatchPolicySource = "package"
)

type PolicyMetadata struct {
	Name string `json:"name" yaml:"name"`
}

type LocalPatchPolicyDocument struct {
	APIVersion string           `json:"apiVersion" yaml:"apiVersion"`
	Kind       string           `json:"kind" yaml:"kind"`
	Metadata   PolicyMetadata   `json:"metadata" yaml:"metadata"`
	Spec       LocalPatchPolicy `json:"spec" yaml:"spec"`
}

func DefaultLocalPatchPolicyDocument() LocalPatchPolicyDocument {
	return LocalPatchPolicyDocument{
		APIVersion: LocalPatchPolicyAPIVersion,
		Kind:       LocalPatchPolicyKind,
		Metadata: PolicyMetadata{
			Name: DefaultLocalPatchPolicyName,
		},
		Spec: DefaultLocalPatchPolicy(),
	}
}

func (d LocalPatchPolicyDocument) EffectiveName() string {
	if strings.TrimSpace(d.Metadata.Name) == "" {
		return DefaultLocalPatchPolicyName
	}
	return d.Metadata.Name
}

func (d LocalPatchPolicyDocument) Clone() *LocalPatchPolicyDocument {
	clone := LocalPatchPolicyDocument{
		APIVersion: d.APIVersion,
		Kind:       d.Kind,
		Metadata: PolicyMetadata{
			Name: d.Metadata.Name,
		},
		Spec: LocalPatchPolicy{
			Scope:                    d.Spec.Scope,
			ForbiddenExactPaths:      append([]string(nil), d.Spec.ForbiddenExactPaths...),
			ForbiddenMetadataKeys:    append([]string(nil), d.Spec.ForbiddenMetadataKeys...),
			ForbiddenContainerFields: append([]string(nil), d.Spec.ForbiddenContainerFields...),
			KindRules:                make([]LocalPatchKindRule, 0, len(d.Spec.KindRules)),
		},
	}
	for _, rule := range d.Spec.KindRules {
		clone.Spec.KindRules = append(clone.Spec.KindRules, LocalPatchKindRule{
			Kind:            rule.Kind,
			AllowedPrefixes: append([]string(nil), rule.AllowedPrefixes...),
		})
	}
	return &clone
}

func (d LocalPatchPolicyDocument) Validate() error {
	if strings.TrimSpace(d.APIVersion) == "" {
		return fmt.Errorf("apiVersion cannot be empty")
	}
	if d.APIVersion != LocalPatchPolicyAPIVersion {
		return fmt.Errorf("apiVersion must be %q", LocalPatchPolicyAPIVersion)
	}
	if strings.TrimSpace(d.Kind) == "" {
		return fmt.Errorf("kind cannot be empty")
	}
	if d.Kind != LocalPatchPolicyKind {
		return fmt.Errorf("kind must be %q", LocalPatchPolicyKind)
	}
	if strings.TrimSpace(d.Metadata.Name) == "" {
		return fmt.Errorf("metadata.name cannot be empty")
	}
	if err := d.Spec.Validate(); err != nil {
		return fmt.Errorf("spec: %w", err)
	}
	return nil
}

func (s LocalPatchPolicySource) Validate() error {
	switch s {
	case LocalPatchPolicySourceBuiltInDefault, LocalPatchPolicySourceLocalRepo, LocalPatchPolicySourceBOM, LocalPatchPolicySourcePackage:
		return nil
	case "":
		return fmt.Errorf("local patch policy source cannot be empty")
	default:
		return fmt.Errorf("unsupported local patch policy source %q", s)
	}
}

func LoadLocalPatchPolicyFile(path string) (*LocalPatchPolicyDocument, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read local patch policy file %q: %w", path, err)
	}

	var doc LocalPatchPolicyDocument
	if err := yaml.Unmarshal(data, &doc); err != nil {
		return nil, fmt.Errorf("decode local patch policy file %q: %w", path, err)
	}
	if err := doc.Validate(); err != nil {
		return nil, fmt.Errorf("validate local patch policy file %q: %w", path, err)
	}
	return doc.Clone(), nil
}
