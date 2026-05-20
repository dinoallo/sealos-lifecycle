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

package compare

import (
	"sort"
	"strings"

	"github.com/labring/sealos/pkg/distribution/hydrate"
	"github.com/labring/sealos/pkg/distribution/state"
)

type StatusSummary struct {
	State                       state.ClusterState     `json:"state" yaml:"state"`
	OperatorActionSummary       OperatorActionSummary  `json:"operatorActionSummary" yaml:"operatorActionSummary"`
	MixedOwnershipObjects       []MixedOwnershipObject `json:"mixedOwnershipObjects,omitempty" yaml:"mixedOwnershipObjects,omitempty"`
	DirtyObjects                []ObjectIssue          `json:"dirtyObjects,omitempty" yaml:"dirtyObjects,omitempty"`
	OrphanObjects               []ObjectIssue          `json:"orphanObjects,omitempty" yaml:"orphanObjects,omitempty"`
	PolicyEligibleOrphanObjects []ObjectIssue          `json:"policyEligibleOrphanObjects,omitempty" yaml:"policyEligibleOrphanObjects,omitempty"`
	HostPathConflicts           []HostPathConflict     `json:"hostPathConflicts,omitempty" yaml:"hostPathConflicts,omitempty"`
	LocalInputHostSplits        []LocalInputHostSplit  `json:"localInputHostSplits,omitempty" yaml:"localInputHostSplits,omitempty"`
	DirtyHostPaths              []HostPathIssue        `json:"dirtyHostPaths,omitempty" yaml:"dirtyHostPaths,omitempty"`
	OrphanHostPaths             []HostPathIssue        `json:"orphanHostPaths,omitempty" yaml:"orphanHostPaths,omitempty"`
}

type OperatorActionSummary struct {
	DirectCommitEligible int `json:"directCommitEligible" yaml:"directCommitEligible"`
	DirectRevertEligible int `json:"directRevertEligible" yaml:"directRevertEligible"`
	BundleMatchRequired  int `json:"bundleMatchRequired" yaml:"bundleMatchRequired"`
}

type MixedOwnershipObject struct {
	APIVersion string                       `json:"apiVersion" yaml:"apiVersion"`
	Kind       string                       `json:"kind" yaml:"kind"`
	Namespace  string                       `json:"namespace,omitempty" yaml:"namespace,omitempty"`
	Name       string                       `json:"name" yaml:"name"`
	Ownerships []hydrate.InventoryOwnership `json:"ownerships" yaml:"ownerships"`
}

type ObjectIssue struct {
	APIVersion             string                  `json:"apiVersion" yaml:"apiVersion"`
	Kind                   string                  `json:"kind" yaml:"kind"`
	Namespace              string                  `json:"namespace,omitempty" yaml:"namespace,omitempty"`
	Name                   string                  `json:"name" yaml:"name"`
	Presence               ObjectPresence          `json:"presence" yaml:"presence"`
	Comparison             ObjectComparison        `json:"comparison,omitempty" yaml:"comparison,omitempty"`
	State                  state.ClusterState      `json:"state" yaml:"state"`
	OperatorAction         OperatorAction          `json:"operatorAction,omitempty" yaml:"operatorAction,omitempty"`
	OperatorActionMetadata *OperatorActionMetadata `json:"operatorActionMetadata,omitempty" yaml:"operatorActionMetadata,omitempty"`
	Paths                  []string                `json:"paths,omitempty" yaml:"paths,omitempty"`
	Remediation            *HostPathRemediation    `json:"remediation,omitempty" yaml:"remediation,omitempty"`
}

type HostPathIssue struct {
	Host                   string                  `json:"host,omitempty" yaml:"host,omitempty"`
	Path                   string                  `json:"path" yaml:"path"`
	Component              string                  `json:"component,omitempty" yaml:"component,omitempty"`
	InputName              string                  `json:"inputName,omitempty" yaml:"inputName,omitempty"`
	UsesHostScopedInput    bool                    `json:"usesHostScopedInput,omitempty" yaml:"usesHostScopedInput,omitempty"`
	HostInputBindingPath   string                  `json:"hostInputBindingPath,omitempty" yaml:"hostInputBindingPath,omitempty"`
	Presence               ObjectPresence          `json:"presence" yaml:"presence"`
	Comparison             ObjectComparison        `json:"comparison,omitempty" yaml:"comparison,omitempty"`
	State                  state.ClusterState      `json:"state" yaml:"state"`
	OperatorAction         OperatorAction          `json:"operatorAction,omitempty" yaml:"operatorAction,omitempty"`
	OperatorActionMetadata *OperatorActionMetadata `json:"operatorActionMetadata,omitempty" yaml:"operatorActionMetadata,omitempty"`
	Reasons                []string                `json:"reasons,omitempty" yaml:"reasons,omitempty"`
	Remediation            *HostPathRemediation    `json:"remediation,omitempty" yaml:"remediation,omitempty"`
}

type HostPathConflict struct {
	Host       string   `json:"host,omitempty" yaml:"host,omitempty"`
	Path       string   `json:"path" yaml:"path"`
	Components []string `json:"components" yaml:"components"`
	Hint       string   `json:"hint,omitempty" yaml:"hint,omitempty"`
}

type LocalInputHostSplit struct {
	Path                    string   `json:"path" yaml:"path"`
	Component               string   `json:"component,omitempty" yaml:"component,omitempty"`
	InputName               string   `json:"inputName,omitempty" yaml:"inputName,omitempty"`
	Hosts                   []string `json:"hosts" yaml:"hosts"`
	HostsWithScopedInput    []string `json:"hostsWithScopedInput,omitempty" yaml:"hostsWithScopedInput,omitempty"`
	HostsWithoutScopedInput []string `json:"hostsWithoutScopedInput,omitempty" yaml:"hostsWithoutScopedInput,omitempty"`
	Hint                    string   `json:"hint,omitempty" yaml:"hint,omitempty"`
}

func SummarizeStatus(result *Result) StatusSummary {
	summary := StatusSummary{State: state.StateClean}
	if result == nil {
		summary.State = state.StateDegraded
		return summary
	}

	for _, object := range result.Objects {
		summary.State = promoteStatusState(summary.State, object.State)

		if mixed, ok := summarizeMixedOwnershipObject(object); ok {
			summary.MixedOwnershipObjects = append(summary.MixedOwnershipObjects, mixed)
		}

		switch object.State {
		case state.StateDirty:
			summary.DirtyObjects = append(summary.DirtyObjects, summarizeObjectIssue(object, state.StateDirty))
		case state.StateOrphan:
			summary.OrphanObjects = append(summary.OrphanObjects, summarizeObjectIssue(object, state.StateOrphan))
			if policyEligible, ok := summarizePolicyEligibleOrphanObjectIssue(object); ok {
				summary.PolicyEligibleOrphanObjects = append(summary.PolicyEligibleOrphanObjects, policyEligible)
			}
		}
	}
	for _, hostPath := range result.HostPaths {
		summary.State = promoteStatusState(summary.State, hostPath.State)
		switch hostPath.State {
		case state.StateDirty:
			summary.DirtyHostPaths = append(summary.DirtyHostPaths, summarizeHostPathIssue(hostPath, state.StateDirty))
		case state.StateOrphan:
			summary.OrphanHostPaths = append(summary.OrphanHostPaths, summarizeHostPathIssue(hostPath, state.StateOrphan))
		}
	}
	summary.HostPathConflicts = summarizeHostPathConflicts(result.HostPaths)
	summary.LocalInputHostSplits = summarizeLocalInputHostSplits(result.HostPaths)

	sort.Slice(summary.MixedOwnershipObjects, func(i, j int) bool {
		return objectIdentityLess(
			summary.MixedOwnershipObjects[i].Kind,
			summary.MixedOwnershipObjects[i].Namespace,
			summary.MixedOwnershipObjects[i].Name,
			summary.MixedOwnershipObjects[j].Kind,
			summary.MixedOwnershipObjects[j].Namespace,
			summary.MixedOwnershipObjects[j].Name,
		)
	})
	sort.Slice(summary.DirtyObjects, func(i, j int) bool {
		return objectIdentityLess(
			summary.DirtyObjects[i].Kind,
			summary.DirtyObjects[i].Namespace,
			summary.DirtyObjects[i].Name,
			summary.DirtyObjects[j].Kind,
			summary.DirtyObjects[j].Namespace,
			summary.DirtyObjects[j].Name,
		)
	})
	sort.Slice(summary.OrphanObjects, func(i, j int) bool {
		return objectIdentityLess(
			summary.OrphanObjects[i].Kind,
			summary.OrphanObjects[i].Namespace,
			summary.OrphanObjects[i].Name,
			summary.OrphanObjects[j].Kind,
			summary.OrphanObjects[j].Namespace,
			summary.OrphanObjects[j].Name,
		)
	})
	sort.Slice(summary.PolicyEligibleOrphanObjects, func(i, j int) bool {
		return objectIdentityLess(
			summary.PolicyEligibleOrphanObjects[i].Kind,
			summary.PolicyEligibleOrphanObjects[i].Namespace,
			summary.PolicyEligibleOrphanObjects[i].Name,
			summary.PolicyEligibleOrphanObjects[j].Kind,
			summary.PolicyEligibleOrphanObjects[j].Namespace,
			summary.PolicyEligibleOrphanObjects[j].Name,
		)
	})
	sort.Slice(summary.DirtyHostPaths, func(i, j int) bool {
		return hostPathIssueLess(summary.DirtyHostPaths[i], summary.DirtyHostPaths[j])
	})
	sort.Slice(summary.OrphanHostPaths, func(i, j int) bool {
		return hostPathIssueLess(summary.OrphanHostPaths[i], summary.OrphanHostPaths[j])
	})
	sort.Slice(summary.HostPathConflicts, func(i, j int) bool {
		if summary.HostPathConflicts[i].Host != summary.HostPathConflicts[j].Host {
			return summary.HostPathConflicts[i].Host < summary.HostPathConflicts[j].Host
		}
		return summary.HostPathConflicts[i].Path < summary.HostPathConflicts[j].Path
	})
	sort.Slice(summary.LocalInputHostSplits, func(i, j int) bool {
		if summary.LocalInputHostSplits[i].Path != summary.LocalInputHostSplits[j].Path {
			return summary.LocalInputHostSplits[i].Path < summary.LocalInputHostSplits[j].Path
		}
		if summary.LocalInputHostSplits[i].Component != summary.LocalInputHostSplits[j].Component {
			return summary.LocalInputHostSplits[i].Component < summary.LocalInputHostSplits[j].Component
		}
		return summary.LocalInputHostSplits[i].InputName < summary.LocalInputHostSplits[j].InputName
	})
	for _, issue := range summary.DirtyObjects {
		accumulateOperatorActionSummary(&summary.OperatorActionSummary, issue.OperatorActionMetadata)
	}
	for _, issue := range summary.OrphanObjects {
		accumulateOperatorActionSummary(&summary.OperatorActionSummary, issue.OperatorActionMetadata)
	}
	for _, issue := range summary.DirtyHostPaths {
		accumulateOperatorActionSummary(&summary.OperatorActionSummary, issue.OperatorActionMetadata)
	}
	for _, issue := range summary.OrphanHostPaths {
		accumulateOperatorActionSummary(&summary.OperatorActionSummary, issue.OperatorActionMetadata)
	}
	return summary
}

func summarizeMixedOwnershipObject(object ObjectStatus) (MixedOwnershipObject, bool) {
	ownerships := uniqueFragmentOwnerships(object)
	if len(ownerships) < 2 {
		return MixedOwnershipObject{}, false
	}
	return MixedOwnershipObject{
		APIVersion: object.Tracked.APIVersion,
		Kind:       object.Tracked.Kind,
		Namespace:  object.Tracked.Namespace,
		Name:       object.Tracked.Name,
		Ownerships: ownerships,
	}, true
}

func summarizeObjectIssue(object ObjectStatus, targetState state.ClusterState) ObjectIssue {
	return summarizeObjectIssueWithFilter(object, targetState, func(mismatch FieldMismatch) bool {
		return true
	})
}

func summarizePolicyEligibleOrphanObjectIssue(object ObjectStatus) (ObjectIssue, bool) {
	issue := summarizeObjectIssueWithFilter(object, state.StateOrphan, func(mismatch FieldMismatch) bool {
		return mismatch.PolicyEligible
	})
	if len(issue.Paths) == 0 {
		return ObjectIssue{}, false
	}
	return issue, true
}

func summarizeObjectIssueWithFilter(object ObjectStatus, targetState state.ClusterState, include func(FieldMismatch) bool) ObjectIssue {
	issue := ObjectIssue{
		APIVersion: object.Tracked.APIVersion,
		Kind:       object.Tracked.Kind,
		Namespace:  object.Tracked.Namespace,
		Name:       object.Tracked.Name,
		Presence:   object.Presence,
		Comparison: object.Comparison,
		State:      targetState,
	}

	paths := make([]string, 0, len(object.Mismatches))
	seen := make(map[string]struct{}, len(object.Mismatches))
	for _, mismatch := range object.Mismatches {
		if mismatch.State != targetState || mismatch.Path == "" {
			continue
		}
		if include != nil && !include(mismatch) {
			continue
		}
		if _, ok := seen[mismatch.Path]; ok {
			continue
		}
		seen[mismatch.Path] = struct{}{}
		paths = append(paths, mismatch.Path)
	}
	sort.Strings(paths)
	issue.Paths = paths
	issue.Remediation = object.Remediation
	issue.OperatorAction = operatorActionForObjectIssue(issue)
	issue.OperatorActionMetadata = metadataForOperatorAction(issue.OperatorAction)
	return issue
}

func operatorActionForObjectIssue(issue ObjectIssue) OperatorAction {
	if issue.Remediation == nil {
		return ""
	}
	if issue.State == state.StateOrphan &&
		issue.Remediation.ChangeOwner == "globalBaseline" &&
		issue.Remediation.PolicyName != "" &&
		len(issue.Remediation.PolicyEligiblePaths) > 0 {
		return OperatorActionPromoteToLocalPatch
	}

	switch issue.Remediation.ChangeOwner {
	case "localOverlay":
		return OperatorActionCommitOrReapplyLocalOverlay
	case "localInput":
		return OperatorActionUpdateLocalInputAndRerender
	case "globalBaseline":
		if issue.State == state.StateOrphan {
			return OperatorActionRevertOrUpdateGlobalBaseline
		}
	case "manualReview":
		return OperatorActionManualReview
	}
	return ""
}

func summarizeHostPathIssue(hostPath HostPathStatus, targetState state.ClusterState) HostPathIssue {
	issue := HostPathIssue{
		Host:                 hostPath.Host,
		Path:                 hostPath.Tracked.HostPath,
		Component:            hostPath.Tracked.Component,
		InputName:            hostPath.Tracked.InputName,
		UsesHostScopedInput:  hostPathUsesHostScopedInput(hostPath),
		HostInputBindingPath: hostPathHostInputBindingPath(hostPath),
		Presence:             hostPath.Presence,
		Comparison:           hostPath.Comparison,
		State:                targetState,
	}
	reasons := make([]string, 0, len(hostPath.Mismatches))
	seen := make(map[string]struct{}, len(hostPath.Mismatches))
	for _, mismatch := range hostPath.Mismatches {
		if mismatch.State != targetState || mismatch.Reason == "" {
			continue
		}
		if _, ok := seen[mismatch.Reason]; ok {
			continue
		}
		seen[mismatch.Reason] = struct{}{}
		reasons = append(reasons, mismatch.Reason)
	}
	sort.Strings(reasons)
	if len(reasons) == 0 && hostPath.Presence == ObjectPresenceMissing {
		reasons = []string{"missing"}
	}
	issue.Reasons = reasons
	issue.Remediation = hostPath.Remediation
	issue.OperatorAction = operatorActionForHostPathIssue(issue)
	issue.OperatorActionMetadata = metadataForOperatorAction(issue.OperatorAction)
	return issue
}

func hostPathUsesHostScopedInput(hostPath HostPathStatus) bool {
	return hostPathHostInputBindingPath(hostPath) != ""
}

func hostPathHostInputBindingPath(hostPath HostPathStatus) string {
	return hostInputBindingPathForHost(hostPath.Tracked.HostInputBindings, hostPath.Host)
}

func hostInputBindingPathForHost(bindings map[string]string, host string) string {
	trimmed := strings.TrimSpace(host)
	if trimmed == "" || len(bindings) == 0 {
		return ""
	}
	if path := strings.TrimSpace(bindings[trimmed]); path != "" {
		return path
	}
	hostWithoutPort := strings.TrimSpace(strings.Split(trimmed, ":")[0])
	if hostWithoutPort == "" || hostWithoutPort == trimmed {
		return ""
	}
	return strings.TrimSpace(bindings[hostWithoutPort])
}

func operatorActionForHostPathIssue(issue HostPathIssue) OperatorAction {
	if issue.Remediation == nil {
		return ""
	}

	switch issue.Remediation.Action {
	case "reviewLocalHostInputAndCommitOrReapply":
		return OperatorActionCommitOrReapplyLocalInput
	case "updateLocalBootstrapInputAndRerender":
		return OperatorActionUpdateLocalInputAndRerender
	case "reviewDistributionBaselineForHostPath":
		return OperatorActionRevertOrUpdateGlobalBaseline
	case "reviewDistributionBaselineForGeneratedProjection":
		return OperatorActionRerenderOrUpdateGlobalBaseline
	case "manualReviewGeneratedProjection":
		return OperatorActionManualReview
	}

	switch issue.Remediation.ChangeOwner {
	case "localInput":
		return OperatorActionUpdateLocalInputAndRerender
	case "globalBaseline":
		return OperatorActionRevertOrUpdateGlobalBaseline
	case "manualReview":
		return OperatorActionManualReview
	}
	return ""
}

func hostPathIssueLess(left, right HostPathIssue) bool {
	if left.Host != right.Host {
		return left.Host < right.Host
	}
	return left.Path < right.Path
}

func summarizeHostPathConflicts(hostPaths []HostPathStatus) []HostPathConflict {
	type key struct {
		host string
		path string
	}

	grouped := make(map[key]map[string]struct{})
	for _, hostPath := range hostPaths {
		component := hostPath.Tracked.Component
		if component == "" {
			continue
		}
		k := key{host: hostPath.Host, path: hostPath.Tracked.HostPath}
		if grouped[k] == nil {
			grouped[k] = make(map[string]struct{})
		}
		grouped[k][component] = struct{}{}
	}

	conflicts := make([]HostPathConflict, 0)
	for k, components := range grouped {
		if len(components) < 2 {
			continue
		}
		names := make([]string, 0, len(components))
		for component := range components {
			names = append(names, component)
		}
		sort.Strings(names)
		conflicts = append(conflicts, HostPathConflict{
			Host:       k.host,
			Path:       k.path,
			Components: names,
			Hint:       "select a single component when reverting this host path",
		})
	}
	return conflicts
}

func summarizeLocalInputHostSplits(hostPaths []HostPathStatus) []LocalInputHostSplit {
	type key struct {
		path      string
		component string
		inputName string
	}

	grouped := make(map[key]map[string]map[string]struct{})
	for _, hostPath := range hostPaths {
		if hostPath.State != state.StateDirty {
			continue
		}
		if hostPath.Tracked.Source != hydrate.InventorySourceLocalInput || hostPath.Tracked.Ownership != hydrate.InventoryOwnershipLocal {
			continue
		}
		if strings.TrimSpace(hostPath.Tracked.InputName) == "" || strings.TrimSpace(hostPath.Host) == "" {
			continue
		}
		k := key{
			path:      hostPath.Tracked.HostPath,
			component: hostPath.Tracked.Component,
			inputName: hostPath.Tracked.InputName,
		}
		if grouped[k] == nil {
			grouped[k] = make(map[string]map[string]struct{})
		}
		digest := hostPath.LiveDigest
		if digest == "" {
			digest = "<unknown>"
		}
		if grouped[k][digest] == nil {
			grouped[k][digest] = make(map[string]struct{})
		}
		grouped[k][digest][hostPath.Host] = struct{}{}
	}

	splits := make([]LocalInputHostSplit, 0)
	for k, digestGroups := range grouped {
		if len(digestGroups) < 2 {
			continue
		}
		hostSet := make(map[string]struct{})
		hostsWithScopedInputSet := make(map[string]struct{})
		for _, hosts := range digestGroups {
			for host := range hosts {
				hostSet[host] = struct{}{}
			}
		}
		for _, hostPath := range hostPaths {
			if hostPath.State != state.StateDirty {
				continue
			}
			if hostPath.Tracked.HostPath != k.path || hostPath.Tracked.Component != k.component || hostPath.Tracked.InputName != k.inputName {
				continue
			}
			if hostPathUsesHostScopedInput(hostPath) {
				hostsWithScopedInputSet[hostPath.Host] = struct{}{}
			}
		}
		hosts := make([]string, 0, len(hostSet))
		for host := range hostSet {
			hosts = append(hosts, host)
		}
		sort.Strings(hosts)
		hostsWithScopedInput := make([]string, 0, len(hostsWithScopedInputSet))
		for host := range hostsWithScopedInputSet {
			hostsWithScopedInput = append(hostsWithScopedInput, host)
		}
		sort.Strings(hostsWithScopedInput)
		hostsWithoutScopedInput := make([]string, 0)
		for _, host := range hosts {
			if _, ok := hostsWithScopedInputSet[host]; !ok {
				hostsWithoutScopedInput = append(hostsWithoutScopedInput, host)
			}
		}
		hint := "select a single host when committing this local input, or split the input per host"
		if len(hostsWithScopedInput) > 0 && len(hostsWithoutScopedInput) > 0 {
			hint = "some hosts already have host-scoped input payloads; add host-scoped inputs for the remaining divergent hosts or commit an explicit host"
		} else if len(hostsWithScopedInput) > 0 {
			hint = "all divergent hosts have host-scoped input payloads; inspect those payloads before committing a host"
		}
		splits = append(splits, LocalInputHostSplit{
			Path:                    k.path,
			Component:               k.component,
			InputName:               k.inputName,
			Hosts:                   hosts,
			HostsWithScopedInput:    hostsWithScopedInput,
			HostsWithoutScopedInput: hostsWithoutScopedInput,
			Hint:                    hint,
		})
	}
	return splits
}

func accumulateOperatorActionSummary(summary *OperatorActionSummary, metadata *OperatorActionMetadata) {
	if summary == nil || metadata == nil {
		return
	}
	if metadata.AllowsDirectCommit {
		summary.DirectCommitEligible++
	}
	if metadata.AllowsDirectRevert {
		summary.DirectRevertEligible++
	}
	if metadata.RequiresBundleMatch {
		summary.BundleMatchRequired++
	}
}

func uniqueFragmentOwnerships(object ObjectStatus) []hydrate.InventoryOwnership {
	seen := make(map[hydrate.InventoryOwnership]struct{}, len(object.Fragments)+1)
	for _, fragment := range object.Fragments {
		if fragment.Ownership == "" {
			continue
		}
		seen[fragment.Ownership] = struct{}{}
	}
	if len(seen) == 0 && object.Tracked.Ownership != "" {
		seen[object.Tracked.Ownership] = struct{}{}
	}

	ownerships := make([]hydrate.InventoryOwnership, 0, len(seen))
	for ownership := range seen {
		ownerships = append(ownerships, ownership)
	}
	sort.Slice(ownerships, func(i, j int) bool {
		return ownerships[i] < ownerships[j]
	})
	return ownerships
}

func promoteStatusState(current, next state.ClusterState) state.ClusterState {
	if current == state.StateOrphan || next == state.StateOrphan {
		return state.StateOrphan
	}
	if current == state.StateDirty || next == state.StateDirty {
		return state.StateDirty
	}
	if current == state.StateDegraded || next == state.StateDegraded {
		return state.StateDegraded
	}
	return state.StateClean
}

func objectIdentityLess(leftKind, leftNamespace, leftName, rightKind, rightNamespace, rightName string) bool {
	if leftKind != rightKind {
		return leftKind < rightKind
	}
	if leftNamespace != rightNamespace {
		return leftNamespace < rightNamespace
	}
	return leftName < rightName
}
