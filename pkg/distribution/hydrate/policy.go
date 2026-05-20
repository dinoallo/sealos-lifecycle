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
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/opencontainers/go-digest"

	"github.com/labring/sealos/pkg/distribution/ownership"
	yamlutil "github.com/labring/sealos/pkg/utils/yaml"
)

const LocalPatchPolicyBundlePath = "policy/" + ownership.LocalPatchPolicyFileName

func resolvePlanLocalPatchPolicy(plan *Plan) ownership.LocalPatchPolicyDocument {
	if plan != nil && plan.LocalPatchPolicy != nil {
		return *plan.LocalPatchPolicy.Clone()
	}
	return ownership.DefaultLocalPatchPolicyDocument()
}

func LoadBOMLocalPatchPolicy(plan *Plan, bomRoot string) (*ownership.LocalPatchPolicyDocument, error) {
	if plan == nil {
		return nil, nil
	}
	policyPath := strings.TrimSpace(plan.BOMLocalPatchPolicy)
	if policyPath == "" {
		return nil, nil
	}
	if strings.TrimSpace(bomRoot) == "" {
		return nil, fmt.Errorf("BOM root cannot be empty when BOM declares local patch policy %q", policyPath)
	}
	cleanPath, err := cleanRelative(policyPath)
	if err != nil {
		return nil, fmt.Errorf("BOM local patch policy: %w", err)
	}
	resolved := filepath.Join(bomRoot, filepath.FromSlash(cleanPath))
	document, err := ownership.LoadLocalPatchPolicyFile(resolved)
	if err != nil {
		return nil, fmt.Errorf("load BOM local patch policy %q: %w", cleanPath, err)
	}
	return document.Clone(), nil
}

func LoadPackageLocalPatchPolicy(plan *Plan, sources SourceProvider) (*ownership.LocalPatchPolicyDocument, error) {
	if plan == nil || len(plan.Components) == 0 {
		return nil, nil
	}

	type candidate struct {
		component string
		path      string
	}
	candidates := make([]candidate, 0)
	for _, component := range plan.Components {
		policyPath := strings.TrimSpace(component.LocalPatchPolicy)
		if policyPath == "" {
			continue
		}
		cleanPath, err := cleanRelative(policyPath)
		if err != nil {
			return nil, fmt.Errorf("component %q local patch policy: %w", component.Name, err)
		}
		candidates = append(candidates, candidate{
			component: component.Name,
			path:      cleanPath,
		})
	}

	if len(candidates) == 0 {
		return nil, nil
	}
	if len(candidates) > 1 {
		descriptions := make([]string, 0, len(candidates))
		for _, candidate := range candidates {
			descriptions = append(descriptions, fmt.Sprintf("%s:%s", candidate.component, candidate.path))
		}
		slices.Sort(descriptions)
		return nil, fmt.Errorf("multiple component packages declare local patch policies: %s; exactly one package policy is supported", strings.Join(descriptions, ", "))
	}
	if sources == nil {
		return nil, fmt.Errorf("source provider cannot be nil")
	}
	selected := candidates[0]
	component, ok := planComponentByName(plan, selected.component)
	if !ok {
		return nil, fmt.Errorf("component %q not found in plan", selected.component)
	}
	source, err := sources.Source(component)
	if err != nil {
		return nil, err
	}
	if strings.TrimSpace(source.Root) == "" {
		return nil, fmt.Errorf("source root for component %q cannot be empty", component.Name)
	}
	resolved := filepath.Join(source.Root, filepath.FromSlash(selected.path))
	document, err := ownership.LoadLocalPatchPolicyFile(resolved)
	if err != nil {
		return nil, fmt.Errorf("load component %q local patch policy %q: %w", component.Name, selected.path, err)
	}
	return document.Clone(), nil
}

func planComponentByName(plan *Plan, name string) (ComponentPlan, bool) {
	if plan == nil {
		return ComponentPlan{}, false
	}
	for _, component := range plan.Components {
		if component.Name == name {
			return component, true
		}
	}
	return ComponentPlan{}, false
}

func renderLocalPatchPolicy(plan *Plan, bundle *Bundle, outputDir string) error {
	if bundle == nil {
		return fmt.Errorf("bundle cannot be nil")
	}

	document := resolvePlanLocalPatchPolicy(plan)
	if err := document.Validate(); err != nil {
		return fmt.Errorf("local patch policy document: %w", err)
	}
	source := ownership.LocalPatchPolicySourceBuiltInDefault
	if plan != nil && plan.LocalPatchPolicySource != "" {
		source = plan.LocalPatchPolicySource
	}
	if err := source.Validate(); err != nil {
		return fmt.Errorf("local patch policy source: %w", err)
	}

	dst := filepath.Join(outputDir, filepath.FromSlash(LocalPatchPolicyBundlePath))
	if err := yamlutil.MarshalFile(dst, document); err != nil {
		return fmt.Errorf("write local patch policy %q: %w", dst, err)
	}
	data, err := os.ReadFile(dst)
	if err != nil {
		return fmt.Errorf("read rendered local patch policy %q: %w", dst, err)
	}

	bundle.Spec.LocalPatchPolicySource = source
	bundle.Spec.LocalPatchPolicyScope = document.Spec.EffectiveScope()
	bundle.Spec.LocalPatchPolicyName = document.EffectiveName()
	bundle.Spec.LocalPatchPolicyPath = mustBundlePath(outputDir, dst)
	bundle.Spec.LocalPatchPolicyDigest = digest.Canonical.FromBytes(data).String()
	return nil
}

func LoadBundleLocalPatchPolicy(bundle *Bundle, bundleRoot string) (*ownership.LocalPatchPolicyDocument, error) {
	if bundle == nil {
		return nil, fmt.Errorf("bundle cannot be nil")
	}

	source, err := resolveBundleLocalPatchPolicySource(bundle.Spec)
	if err != nil {
		return nil, err
	}
	path := strings.TrimSpace(bundle.Spec.LocalPatchPolicyPath)
	if path == "" {
		document := ownership.DefaultLocalPatchPolicyDocument()
		return document.Clone(), nil
	}

	resolved, err := resolveInventoryBundlePath(bundleRoot, path)
	if err != nil {
		return nil, fmt.Errorf("resolve local patch policy %q: %w", path, err)
	}
	document, err := ownership.LoadLocalPatchPolicyFile(resolved)
	if err != nil {
		return nil, err
	}
	if expectedName := strings.TrimSpace(bundle.Spec.LocalPatchPolicyName); expectedName != "" && document.EffectiveName() != expectedName {
		return nil, fmt.Errorf("local patch policy name mismatch for source %q: bundle records %q but rendered policy is %q", source, expectedName, document.EffectiveName())
	}
	if expectedScope := bundle.Spec.LocalPatchPolicyScope; expectedScope != "" && document.Spec.EffectiveScope() != expectedScope {
		return nil, fmt.Errorf("local patch policy scope mismatch for source %q: bundle records %q but rendered policy is %q", source, expectedScope, document.Spec.EffectiveScope())
	}
	if expectedDigest := strings.TrimSpace(bundle.Spec.LocalPatchPolicyDigest); expectedDigest != "" {
		data, err := os.ReadFile(resolved)
		if err != nil {
			return nil, fmt.Errorf("read rendered local patch policy %q: %w", resolved, err)
		}
		actualDigest := digest.Canonical.FromBytes(data).String()
		if actualDigest != expectedDigest {
			return nil, fmt.Errorf("local patch policy digest mismatch for source %q: bundle records %q but rendered policy is %q", source, expectedDigest, actualDigest)
		}
	}
	return document, nil
}

func resolveBundleLocalPatchPolicySource(spec BundleSpec) (ownership.LocalPatchPolicySource, error) {
	source := spec.LocalPatchPolicySource
	path := strings.TrimSpace(spec.LocalPatchPolicyPath)

	if source == "" && path == "" {
		return ownership.LocalPatchPolicySourceBuiltInDefault, nil
	}
	if source == "" {
		return "", fmt.Errorf("local patch policy source must be set when localPatchPolicyPath is present")
	}
	if err := source.Validate(); err != nil {
		return "", fmt.Errorf("local patch policy source: %w", err)
	}
	if path == "" {
		return "", fmt.Errorf("local patch policy path must be set for source %q", source)
	}
	return source, nil
}
