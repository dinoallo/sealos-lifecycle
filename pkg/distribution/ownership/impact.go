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

import "sort"

type KindPath struct {
	Kind string `json:"kind" yaml:"kind"`
	Path string `json:"path" yaml:"path"`
}

type PolicyImpact struct {
	AddedAllowedPrefixes            []KindPath `json:"addedAllowedPrefixes,omitempty" yaml:"addedAllowedPrefixes,omitempty"`
	RemovedAllowedPrefixes          []KindPath `json:"removedAllowedPrefixes,omitempty" yaml:"removedAllowedPrefixes,omitempty"`
	AddedForbiddenExactPaths        []string   `json:"addedForbiddenExactPaths,omitempty" yaml:"addedForbiddenExactPaths,omitempty"`
	RemovedForbiddenExactPaths      []string   `json:"removedForbiddenExactPaths,omitempty" yaml:"removedForbiddenExactPaths,omitempty"`
	AddedForbiddenMetadataKeys      []string   `json:"addedForbiddenMetadataKeys,omitempty" yaml:"addedForbiddenMetadataKeys,omitempty"`
	RemovedForbiddenMetadataKeys    []string   `json:"removedForbiddenMetadataKeys,omitempty" yaml:"removedForbiddenMetadataKeys,omitempty"`
	AddedForbiddenContainerFields   []string   `json:"addedForbiddenContainerFields,omitempty" yaml:"addedForbiddenContainerFields,omitempty"`
	RemovedForbiddenContainerFields []string   `json:"removedForbiddenContainerFields,omitempty" yaml:"removedForbiddenContainerFields,omitempty"`
}

func DiffLocalPatchPolicy(oldPolicy, newPolicy LocalPatchPolicy) PolicyImpact {
	oldAllowed := flattenAllowedPrefixes(oldPolicy)
	newAllowed := flattenAllowedPrefixes(newPolicy)

	return PolicyImpact{
		AddedAllowedPrefixes:            diffKindPaths(newAllowed, oldAllowed),
		RemovedAllowedPrefixes:          diffKindPaths(oldAllowed, newAllowed),
		AddedForbiddenExactPaths:        diffStrings(newPolicy.ForbiddenExactPaths, oldPolicy.ForbiddenExactPaths),
		RemovedForbiddenExactPaths:      diffStrings(oldPolicy.ForbiddenExactPaths, newPolicy.ForbiddenExactPaths),
		AddedForbiddenMetadataKeys:      diffStrings(newPolicy.ForbiddenMetadataKeys, oldPolicy.ForbiddenMetadataKeys),
		RemovedForbiddenMetadataKeys:    diffStrings(oldPolicy.ForbiddenMetadataKeys, newPolicy.ForbiddenMetadataKeys),
		AddedForbiddenContainerFields:   diffStrings(newPolicy.ForbiddenContainerFields, oldPolicy.ForbiddenContainerFields),
		RemovedForbiddenContainerFields: diffStrings(oldPolicy.ForbiddenContainerFields, newPolicy.ForbiddenContainerFields),
	}
}

func (r PolicyImpact) HasWideningChanges() bool {
	return len(r.AddedAllowedPrefixes) > 0 ||
		len(r.RemovedForbiddenExactPaths) > 0 ||
		len(r.RemovedForbiddenMetadataKeys) > 0 ||
		len(r.RemovedForbiddenContainerFields) > 0
}

func (r PolicyImpact) HasNarrowingChanges() bool {
	return len(r.RemovedAllowedPrefixes) > 0 ||
		len(r.AddedForbiddenExactPaths) > 0 ||
		len(r.AddedForbiddenMetadataKeys) > 0 ||
		len(r.AddedForbiddenContainerFields) > 0
}

func flattenAllowedPrefixes(policy LocalPatchPolicy) []KindPath {
	items := make([]KindPath, 0)
	for _, rule := range policy.KindRules {
		for _, prefix := range rule.AllowedPrefixes {
			items = append(items, KindPath{
				Kind: rule.Kind,
				Path: prefix,
			})
		}
	}
	sort.Slice(items, func(i, j int) bool {
		if items[i].Kind != items[j].Kind {
			return items[i].Kind < items[j].Kind
		}
		return items[i].Path < items[j].Path
	})
	return items
}

func diffKindPaths(left, right []KindPath) []KindPath {
	rightSet := make(map[string]struct{}, len(right))
	for _, item := range right {
		rightSet[item.Kind+"\x00"+item.Path] = struct{}{}
	}

	diff := make([]KindPath, 0)
	for _, item := range left {
		key := item.Kind + "\x00" + item.Path
		if _, ok := rightSet[key]; ok {
			continue
		}
		diff = append(diff, item)
	}
	return diff
}

func diffStrings(left, right []string) []string {
	rightSet := make(map[string]struct{}, len(right))
	for _, item := range right {
		rightSet[item] = struct{}{}
	}

	diff := make([]string, 0)
	for _, item := range left {
		if _, ok := rightSet[item]; ok {
			continue
		}
		diff = append(diff, item)
	}
	sort.Strings(diff)
	return diff
}
