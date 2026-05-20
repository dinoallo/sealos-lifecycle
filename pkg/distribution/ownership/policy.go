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
	"strings"
)

type LocalPatchPolicyScope string

const (
	LocalPatchPolicyScopeClusterLocal LocalPatchPolicyScope = "clusterLocal"
)

type LocalPatchPolicy struct {
	Scope                    LocalPatchPolicyScope `json:"scope,omitempty" yaml:"scope,omitempty"`
	ForbiddenExactPaths      []string              `json:"forbiddenExactPaths,omitempty" yaml:"forbiddenExactPaths,omitempty"`
	ForbiddenMetadataKeys    []string              `json:"forbiddenMetadataKeys,omitempty" yaml:"forbiddenMetadataKeys,omitempty"`
	ForbiddenContainerFields []string              `json:"forbiddenContainerFields,omitempty" yaml:"forbiddenContainerFields,omitempty"`
	KindRules                []LocalPatchKindRule  `json:"kindRules" yaml:"kindRules"`
}

type LocalPatchKindRule struct {
	Kind            string   `json:"kind" yaml:"kind"`
	AllowedPrefixes []string `json:"allowedPrefixes,omitempty" yaml:"allowedPrefixes,omitempty"`
}

const DefaultLocalPatchPolicyName = "defaultLocalPatchPolicy"

var defaultLocalPatchPolicy = LocalPatchPolicy{
	Scope: LocalPatchPolicyScopeClusterLocal,
	ForbiddenExactPaths: []string{
		"status",
		"spec.selector",
	},
	ForbiddenMetadataKeys: []string{
		"uid",
		"resourceVersion",
		"generation",
		"creationTimestamp",
		"managedFields",
		"ownerReferences",
		"finalizers",
		"generateName",
		"selfLink",
		"deletionTimestamp",
		"deletionGracePeriodSeconds",
	},
	ForbiddenContainerFields: []string{
		"image",
	},
	KindRules: []LocalPatchKindRule{
		{
			Kind: "ConfigMap",
			AllowedPrefixes: []string{
				"data",
				"binaryData",
			},
		},
		{
			Kind:            "Deployment",
			AllowedPrefixes: templatePlacementPatchPrefixes("spec.template"),
		},
		{
			Kind:            "DaemonSet",
			AllowedPrefixes: templatePlacementPatchPrefixes("spec.template"),
		},
		{
			Kind: "StatefulSet",
			AllowedPrefixes: append(
				templatePlacementPatchPrefixes("spec.template"),
				"spec.volumeClaimTemplates",
			),
		},
		{
			Kind:            "Job",
			AllowedPrefixes: templatePlacementPatchPrefixes("spec.template"),
		},
		{
			Kind:            "CronJob",
			AllowedPrefixes: templatePlacementPatchPrefixes("spec.jobTemplate.spec.template"),
		},
		{
			Kind: "Ingress",
			AllowedPrefixes: []string{
				"metadata.annotations",
				"metadata.labels",
				"spec.ingressClassName",
				"spec.rules",
				"spec.tls",
			},
		},
		{
			Kind: "Service",
			AllowedPrefixes: []string{
				"metadata.annotations",
				"metadata.labels",
				"spec.type",
				"spec.ports",
				"spec.loadBalancerClass",
				"spec.loadBalancerSourceRanges",
				"spec.externalTrafficPolicy",
				"spec.internalTrafficPolicy",
				"spec.sessionAffinity",
			},
		},
	},
}

func DefaultLocalPatchPolicy() LocalPatchPolicy {
	return defaultLocalPatchPolicy
}

func (p LocalPatchPolicy) EffectiveScope() LocalPatchPolicyScope {
	if strings.TrimSpace(string(p.Scope)) == "" {
		return LocalPatchPolicyScopeClusterLocal
	}
	return LocalPatchPolicyScope(strings.TrimSpace(string(p.Scope)))
}

func (s LocalPatchPolicyScope) Validate() error {
	switch s {
	case LocalPatchPolicyScopeClusterLocal:
		return nil
	case "":
		return fmt.Errorf("local patch policy scope cannot be empty")
	default:
		return fmt.Errorf("unsupported local patch policy scope %q: only %q is supported; package/BOM-scoped local patch policy is not supported", s, LocalPatchPolicyScopeClusterLocal)
	}
}

func (p LocalPatchPolicy) Validate() error {
	if err := p.EffectiveScope().Validate(); err != nil {
		return err
	}
	for i, path := range p.ForbiddenExactPaths {
		if strings.TrimSpace(path) == "" {
			return fmt.Errorf("forbiddenExactPaths[%d] cannot be empty", i)
		}
	}
	for i, key := range p.ForbiddenMetadataKeys {
		if strings.TrimSpace(key) == "" {
			return fmt.Errorf("forbiddenMetadataKeys[%d] cannot be empty", i)
		}
	}
	for i, field := range p.ForbiddenContainerFields {
		if strings.TrimSpace(field) == "" {
			return fmt.Errorf("forbiddenContainerFields[%d] cannot be empty", i)
		}
	}
	if err := requireLocalPatchPolicyForbiddenEntries("forbiddenExactPaths", "path", p.ForbiddenExactPaths, defaultLocalPatchPolicy.ForbiddenExactPaths); err != nil {
		return err
	}
	if err := requireLocalPatchPolicyForbiddenEntries("forbiddenMetadataKeys", "metadata key", p.ForbiddenMetadataKeys, defaultLocalPatchPolicy.ForbiddenMetadataKeys); err != nil {
		return err
	}
	if err := requireLocalPatchPolicyForbiddenEntries("forbiddenContainerFields", "container field", p.ForbiddenContainerFields, defaultLocalPatchPolicy.ForbiddenContainerFields); err != nil {
		return err
	}
	if len(p.KindRules) == 0 {
		return fmt.Errorf("kindRules cannot be empty")
	}
	for i, rule := range p.KindRules {
		if err := rule.Validate(); err != nil {
			return fmt.Errorf("kindRules[%d]: %w", i, err)
		}
	}
	return nil
}

func requireLocalPatchPolicyForbiddenEntries(fieldName, valueLabel string, got, required []string) error {
	seen := make(map[string]struct{}, len(got))
	for _, entry := range got {
		seen[strings.TrimSpace(entry)] = struct{}{}
	}
	for _, requiredEntry := range required {
		requiredEntry = strings.TrimSpace(requiredEntry)
		if _, ok := seen[requiredEntry]; !ok {
			return fmt.Errorf("%s must include required %s %q", fieldName, valueLabel, requiredEntry)
		}
	}
	return nil
}

func (r LocalPatchKindRule) Validate() error {
	if strings.TrimSpace(r.Kind) == "" {
		return fmt.Errorf("kind cannot be empty")
	}
	if len(r.AllowedPrefixes) == 0 {
		return fmt.Errorf("allowedPrefixes cannot be empty for kind %q", r.Kind)
	}
	for i, prefix := range r.AllowedPrefixes {
		if strings.TrimSpace(prefix) == "" {
			return fmt.Errorf("allowedPrefixes[%d] cannot be empty for kind %q", i, r.Kind)
		}
	}
	return nil
}

func (p LocalPatchPolicy) IsForbidden(path []string) bool {
	current := pathString(path)
	for _, exact := range p.ForbiddenExactPaths {
		if current == exact {
			return true
		}
	}
	if len(path) >= 2 && path[0] == "metadata" {
		for _, key := range p.ForbiddenMetadataKeys {
			if path[1] == key {
				return true
			}
		}
	}
	if containsContainerPath(path) {
		field := path[len(path)-1]
		for _, forbiddenField := range p.ForbiddenContainerFields {
			if field == forbiddenField {
				return true
			}
		}
	}
	return false
}

func (p LocalPatchPolicy) IsAllowed(kind string, path []string) bool {
	rule, ok := p.ruleForKind(kind)
	if !ok {
		return false
	}
	current := pathString(path)
	for _, prefix := range rule.AllowedPrefixes {
		if current == prefix || strings.HasPrefix(current, prefix+".") {
			return true
		}
		if strings.HasPrefix(prefix, current+".") {
			return true
		}
	}
	return false
}

func (p LocalPatchPolicy) ruleForKind(kind string) (LocalPatchKindRule, bool) {
	for _, rule := range p.KindRules {
		if rule.Kind == kind {
			return rule, true
		}
	}
	return LocalPatchKindRule{}, false
}
