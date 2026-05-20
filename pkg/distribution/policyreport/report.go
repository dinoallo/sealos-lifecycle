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

package policyreport

import (
	"fmt"

	"github.com/opencontainers/go-digest"
	"sigs.k8s.io/yaml"

	"github.com/labring/sealos/pkg/distribution/localrepo"
	"github.com/labring/sealos/pkg/distribution/ownership"
)

type PolicyRef struct {
	Name   string                          `json:"name" yaml:"name"`
	Scope  ownership.LocalPatchPolicyScope `json:"scope" yaml:"scope"`
	Digest string                          `json:"digest" yaml:"digest"`
}

type Report struct {
	Old                 PolicyRef                           `json:"old" yaml:"old"`
	New                 PolicyRef                           `json:"new" yaml:"new"`
	HasWideningChanges  bool                                `json:"hasWideningChanges" yaml:"hasWideningChanges"`
	HasNarrowingChanges bool                                `json:"hasNarrowingChanges" yaml:"hasNarrowingChanges"`
	Impact              ownership.PolicyImpact              `json:"impact" yaml:"impact"`
	Compatibility       localrepo.PolicyCompatibilityReport `json:"compatibility" yaml:"compatibility"`
}

func Build(oldDoc, newDoc *ownership.LocalPatchPolicyDocument, repo *localrepo.Repo) (*Report, error) {
	if oldDoc == nil {
		return nil, fmt.Errorf("old local patch policy document cannot be nil")
	}
	if newDoc == nil {
		return nil, fmt.Errorf("new local patch policy document cannot be nil")
	}
	if err := oldDoc.Validate(); err != nil {
		return nil, fmt.Errorf("validate old local patch policy: %w", err)
	}
	if err := newDoc.Validate(); err != nil {
		return nil, fmt.Errorf("validate new local patch policy: %w", err)
	}

	impact := ownership.DiffLocalPatchPolicy(oldDoc.Spec, newDoc.Spec)
	compatibility, err := localrepo.EvaluatePatchCompatibility(repo, newDoc.Spec)
	if err != nil {
		return nil, fmt.Errorf("evaluate local patch compatibility: %w", err)
	}
	oldDigest, err := localPatchPolicyDigest(oldDoc)
	if err != nil {
		return nil, fmt.Errorf("digest old local patch policy: %w", err)
	}
	newDigest, err := localPatchPolicyDigest(newDoc)
	if err != nil {
		return nil, fmt.Errorf("digest new local patch policy: %w", err)
	}

	return &Report{
		Old: PolicyRef{
			Name:   oldDoc.EffectiveName(),
			Scope:  oldDoc.Spec.EffectiveScope(),
			Digest: oldDigest,
		},
		New: PolicyRef{
			Name:   newDoc.EffectiveName(),
			Scope:  newDoc.Spec.EffectiveScope(),
			Digest: newDigest,
		},
		HasWideningChanges:  impact.HasWideningChanges(),
		HasNarrowingChanges: impact.HasNarrowingChanges(),
		Impact:              impact,
		Compatibility:       compatibility,
	}, nil
}

func localPatchPolicyDigest(doc *ownership.LocalPatchPolicyDocument) (string, error) {
	data, err := yaml.Marshal(doc)
	if err != nil {
		return "", err
	}
	return digest.Canonical.FromBytes(data).String(), nil
}
