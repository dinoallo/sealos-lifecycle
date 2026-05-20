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

package localrepo

import (
	"fmt"
	"os"
	"sort"

	"sigs.k8s.io/yaml"

	"github.com/labring/sealos/pkg/distribution/ownership"
)

type PolicyCompatibilityIssue struct {
	Component    string `json:"component" yaml:"component"`
	RelativePath string `json:"relativePath" yaml:"relativePath"`
	Reason       string `json:"reason" yaml:"reason"`
}

type PolicyCompatibilityReport struct {
	ValidPatchCount int                        `json:"validPatchCount" yaml:"validPatchCount"`
	InvalidPatches  []PolicyCompatibilityIssue `json:"invalidPatches,omitempty" yaml:"invalidPatches,omitempty"`
}

func (r PolicyCompatibilityReport) HasIncompatiblePatches() bool {
	return len(r.InvalidPatches) > 0
}

func EvaluatePatchCompatibility(repo *Repo, policy ownership.LocalPatchPolicy) (PolicyCompatibilityReport, error) {
	if err := policy.Validate(); err != nil {
		return PolicyCompatibilityReport{}, fmt.Errorf("validate local patch policy: %w", err)
	}
	if repo == nil {
		return PolicyCompatibilityReport{}, nil
	}

	report := PolicyCompatibilityReport{}
	componentNames := make([]string, 0, len(repo.patchesByComponent))
	for componentName := range repo.patchesByComponent {
		componentNames = append(componentNames, componentName)
	}
	sort.Strings(componentNames)

	for _, componentName := range componentNames {
		patches := repo.patchesByComponent[componentName]
		for _, patch := range patches {
			data, err := os.ReadFile(patch.Path)
			if err != nil {
				return PolicyCompatibilityReport{}, fmt.Errorf("read local patch %q: %w", patch.Path, err)
			}
			var document map[string]interface{}
			if err := yaml.Unmarshal(data, &document); err != nil {
				return PolicyCompatibilityReport{}, fmt.Errorf("decode local patch %q: %w", patch.Path, err)
			}
			if err := ownership.ValidateLocalPatchOverlayWithPolicy(policy, document); err != nil {
				report.InvalidPatches = append(report.InvalidPatches, PolicyCompatibilityIssue{
					Component:    componentName,
					RelativePath: patch.RelativePath,
					Reason:       err.Error(),
				})
				continue
			}
			report.ValidPatchCount++
		}
	}
	return report, nil
}
