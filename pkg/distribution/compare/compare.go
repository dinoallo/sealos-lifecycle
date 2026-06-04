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
	"bytes"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"

	"github.com/opencontainers/go-digest"
	"k8s.io/apimachinery/pkg/runtime"
	utilyaml "k8s.io/apimachinery/pkg/util/yaml"
	"sigs.k8s.io/yaml"

	"github.com/labring/sealos/pkg/distribution/hydrate"
	"github.com/labring/sealos/pkg/distribution/ownership"
	"github.com/labring/sealos/pkg/distribution/state"
)

type ObjectPresence string

const (
	ObjectPresencePresent ObjectPresence = "present"
	ObjectPresenceMissing ObjectPresence = "missing"
)

type ObjectComparison string

const (
	ObjectComparisonMatched ObjectComparison = "matched"
	ObjectComparisonDrifted ObjectComparison = "drifted"
)

type ObjectStatus struct {
	Tracked       hydrate.TrackedK8sObject   `json:"tracked" yaml:"tracked"`
	Fragments     []hydrate.TrackedK8sObject `json:"fragments,omitempty" yaml:"fragments,omitempty"`
	Presence      ObjectPresence             `json:"presence" yaml:"presence"`
	Comparison    ObjectComparison           `json:"comparison,omitempty" yaml:"comparison,omitempty"`
	State         state.ClusterState         `json:"state" yaml:"state"`
	DesiredDigest string                     `json:"desiredDigest,omitempty" yaml:"desiredDigest,omitempty"`
	LiveDigest    string                     `json:"liveDigest,omitempty" yaml:"liveDigest,omitempty"`
	Mismatches    []FieldMismatch            `json:"mismatches,omitempty" yaml:"mismatches,omitempty"`
	Remediation   *HostPathRemediation       `json:"remediation,omitempty" yaml:"remediation,omitempty"`
}

type HostPathStatus struct {
	Host          string                  `json:"host,omitempty" yaml:"host,omitempty"`
	Tracked       hydrate.TrackedHostPath `json:"tracked" yaml:"tracked"`
	Presence      ObjectPresence          `json:"presence" yaml:"presence"`
	Comparison    ObjectComparison        `json:"comparison,omitempty" yaml:"comparison,omitempty"`
	State         state.ClusterState      `json:"state" yaml:"state"`
	DesiredDigest string                  `json:"desiredDigest,omitempty" yaml:"desiredDigest,omitempty"`
	LiveDigest    string                  `json:"liveDigest,omitempty" yaml:"liveDigest,omitempty"`
	Mismatches    []HostPathMismatch      `json:"mismatches,omitempty" yaml:"mismatches,omitempty"`
	Remediation   *HostPathRemediation    `json:"remediation,omitempty" yaml:"remediation,omitempty"`
}

type HostPathMismatch struct {
	Reason  string             `json:"reason" yaml:"reason"`
	Desired string             `json:"desired,omitempty" yaml:"desired,omitempty"`
	Live    string             `json:"live,omitempty" yaml:"live,omitempty"`
	State   state.ClusterState `json:"state,omitempty" yaml:"state,omitempty"`
}

type HostPathRemediation struct {
	Action              string                    `json:"action" yaml:"action"`
	ChangeOwner         string                    `json:"changeOwner,omitempty" yaml:"changeOwner,omitempty"`
	Source              string                    `json:"source,omitempty" yaml:"source,omitempty"`
	ProjectionClass     string                    `json:"projectionClass,omitempty" yaml:"projectionClass,omitempty"`
	Generator           string                    `json:"generator,omitempty" yaml:"generator,omitempty"`
	GeneratedKind       string                    `json:"generatedKind,omitempty" yaml:"generatedKind,omitempty"`
	GeneratedName       string                    `json:"generatedName,omitempty" yaml:"generatedName,omitempty"`
	Repairable          *bool                     `json:"repairable,omitempty" yaml:"repairable,omitempty"`
	PolicyName          string                    `json:"policyName,omitempty" yaml:"policyName,omitempty"`
	PolicyEligiblePaths []string                  `json:"policyEligiblePaths,omitempty" yaml:"policyEligiblePaths,omitempty"`
	SafeDirectRevert    bool                      `json:"safeDirectRevert" yaml:"safeDirectRevert"`
	SafeCommit          bool                      `json:"safeCommit" yaml:"safeCommit"`
	Message             string                    `json:"message,omitempty" yaml:"message,omitempty"`
	NextSteps           []string                  `json:"nextSteps,omitempty" yaml:"nextSteps,omitempty"`
	AllowedCommands     []string                  `json:"allowedCommands,omitempty" yaml:"allowedCommands,omitempty"`
	CommandGuidance     []HostPathCommandGuidance `json:"commandGuidance,omitempty" yaml:"commandGuidance,omitempty"`
}

type HostPathCommandGuidance struct {
	Command       string   `json:"command" yaml:"command"`
	Preconditions []string `json:"preconditions,omitempty" yaml:"preconditions,omitempty"`
	Availability  string   `json:"availability,omitempty" yaml:"availability,omitempty"`
	Reason        string   `json:"reason,omitempty" yaml:"reason,omitempty"`
}

type FieldMismatch struct {
	Path           string                     `json:"path" yaml:"path"`
	Reason         string                     `json:"reason" yaml:"reason"`
	Ownership      hydrate.InventoryOwnership `json:"ownership,omitempty" yaml:"ownership,omitempty"`
	PolicyName     string                     `json:"policyName,omitempty" yaml:"policyName,omitempty"`
	PolicyEligible bool                       `json:"policyEligible,omitempty" yaml:"policyEligible,omitempty"`
	State          state.ClusterState         `json:"state,omitempty" yaml:"state,omitempty"`
	Desired        string                     `json:"desired,omitempty" yaml:"desired,omitempty"`
	Live           string                     `json:"live,omitempty" yaml:"live,omitempty"`
}

type Summary struct {
	Total   int `json:"total" yaml:"total"`
	Present int `json:"present" yaml:"present"`
	Missing int `json:"missing" yaml:"missing"`
	Matched int `json:"matched" yaml:"matched"`
	Drifted int `json:"drifted" yaml:"drifted"`
	Clean   int `json:"clean" yaml:"clean"`
	Dirty   int `json:"dirty" yaml:"dirty"`
	Orphan  int `json:"orphan" yaml:"orphan"`
}

type Result struct {
	Summary   Summary          `json:"summary" yaml:"summary"`
	Objects   []ObjectStatus   `json:"objects" yaml:"objects"`
	HostPaths []HostPathStatus `json:"hostPaths,omitempty" yaml:"hostPaths,omitempty"`
}

type CompareOptions struct {
	HostRoot      string
	HostIdentity  string
	SkipObjects   bool
	SkipHostPaths bool
}

type ObjectResolver interface {
	Get(apiVersion, kind, namespace, name string) ([]byte, error)
}

type trackedObjectGroup struct {
	primary   hydrate.TrackedK8sObject
	fragments []hydrate.TrackedK8sObject
}

type desiredProjection struct {
	merged     interface{}
	kind       string
	ownership  pathOwnershipIndex
	policy     ownership.LocalPatchPolicy
	policyName string
}

type pathOwnershipIndex map[string]hydrate.InventoryOwnership

func CompareBundle(bundle *hydrate.Bundle, bundleRoot string, resolver ObjectResolver) (*Result, error) {
	return CompareBundleWithOptions(bundle, bundleRoot, resolver, CompareOptions{})
}

func CompareBundleWithOptions(bundle *hydrate.Bundle, bundleRoot string, resolver ObjectResolver, opts CompareOptions) (*Result, error) {
	if bundle == nil {
		return nil, fmt.Errorf("bundle cannot be nil")
	}
	if strings.TrimSpace(bundleRoot) == "" {
		return nil, fmt.Errorf("bundle root cannot be empty")
	}
	if resolver == nil {
		return nil, fmt.Errorf("object resolver cannot be nil")
	}

	result := &Result{
		Objects:   make([]ObjectStatus, 0, len(bundle.Spec.TrackedK8sObjects)),
		HostPaths: make([]HostPathStatus, 0, len(bundle.Spec.TrackedHostPaths)),
	}
	policyDoc, err := hydrate.LoadBundleLocalPatchPolicy(bundle, bundleRoot)
	if err != nil {
		return nil, err
	}
	if !opts.SkipObjects {
		for _, group := range groupTrackedObjects(bundle.Spec.TrackedK8sObjects) {
			projection, err := compileDesiredProjection(bundleRoot, group.fragments, policyDoc.Spec, policyDoc.EffectiveName())
			if err != nil {
				return nil, err
			}
			desiredDigest, err := digestNormalizedValue(projection.merged)
			if err != nil {
				return nil, fmt.Errorf("normalize desired object %s %s/%s: %w", group.primary.Kind, group.primary.Namespace, group.primary.Name, err)
			}

			status := ObjectStatus{
				Tracked:       group.primary,
				Fragments:     append([]hydrate.TrackedK8sObject(nil), group.fragments...),
				Presence:      ObjectPresencePresent,
				State:         state.StateClean,
				DesiredDigest: desiredDigest,
			}

			liveRaw, err := resolver.Get(group.primary.APIVersion, group.primary.Kind, group.primary.Namespace, group.primary.Name)
			if err != nil {
				if isNotFound(err) {
					status.Presence = ObjectPresenceMissing
					status.State = projection.ownership.maxState()
					status.Remediation = objectRemediation(status)
					result.Objects = append(result.Objects, status)
					continue
				}
				return nil, err
			}

			liveDigest, err := normalizedObjectDigest(liveRaw)
			if err != nil {
				return nil, fmt.Errorf("normalize live object %s %s/%s: %w", group.primary.Kind, group.primary.Namespace, group.primary.Name, err)
			}
			status.LiveDigest = liveDigest
			mismatches, err := projection.mismatches(liveRaw)
			if err != nil {
				return nil, fmt.Errorf("compare owned fields for %s %s/%s: %w", group.primary.Kind, group.primary.Namespace, group.primary.Name, err)
			}
			if len(mismatches) == 0 {
				status.Comparison = ObjectComparisonMatched
			} else {
				status.Comparison = ObjectComparisonDrifted
				status.State = highestMismatchState(mismatches)
				status.Mismatches = mismatches
				status.Remediation = objectRemediation(status)
			}
			result.Objects = append(result.Objects, status)
		}
	}

	hostRoot := opts.HostRoot
	if strings.TrimSpace(hostRoot) == "" {
		hostRoot = string(os.PathSeparator)
	}
	if !opts.SkipHostPaths {
		for _, tracked := range bundle.Spec.TrackedHostPaths {
			hostIdentity := strings.TrimSpace(opts.HostIdentity)
			status, err := compareTrackedHostPath(bundleRoot, hostRoot, hostIdentity, tracked)
			if err != nil {
				return nil, err
			}
			status.Host = hostIdentity
			result.HostPaths = append(result.HostPaths, status)
		}
	}

	result.Summary = SummarizeResult(result)
	return result, nil
}

func SummarizeResult(result *Result) Summary {
	if result == nil {
		return Summary{}
	}
	summary := Summary{Total: len(result.Objects) + len(result.HostPaths)}
	for _, object := range result.Objects {
		switch object.Presence {
		case ObjectPresencePresent:
			summary.Present++
		case ObjectPresenceMissing:
			summary.Missing++
		}
		switch object.Comparison {
		case ObjectComparisonMatched:
			summary.Matched++
		case ObjectComparisonDrifted:
			summary.Drifted++
		}
		switch object.State {
		case state.StateClean:
			summary.Clean++
		case state.StateDirty:
			summary.Dirty++
		case state.StateOrphan:
			summary.Orphan++
		}
	}
	for _, hostPath := range result.HostPaths {
		switch hostPath.Presence {
		case ObjectPresencePresent:
			summary.Present++
		case ObjectPresenceMissing:
			summary.Missing++
		}
		switch hostPath.Comparison {
		case ObjectComparisonMatched:
			summary.Matched++
		case ObjectComparisonDrifted:
			summary.Drifted++
		}
		switch hostPath.State {
		case state.StateClean:
			summary.Clean++
		case state.StateDirty:
			summary.Dirty++
		case state.StateOrphan:
			summary.Orphan++
		}
	}
	return summary
}

func DesiredObjectYAML(bundleRoot string, object ObjectStatus) ([]byte, error) {
	fragments := object.Fragments
	if len(fragments) == 0 {
		if object.Tracked.Kind == "" || object.Tracked.Name == "" {
			return nil, fmt.Errorf("object fragments cannot be empty")
		}
		fragments = []hydrate.TrackedK8sObject{object.Tracked}
	}

	projection, err := compileDesiredProjection(bundleRoot, fragments, ownership.DefaultLocalPatchPolicy(), ownership.DefaultLocalPatchPolicyName)
	if err != nil {
		return nil, err
	}
	data, err := yaml.Marshal(projection.merged)
	if err != nil {
		return nil, fmt.Errorf("marshal desired object %s %s/%s: %w", object.Tracked.Kind, object.Tracked.Namespace, object.Tracked.Name, err)
	}
	return data, nil
}

func groupTrackedObjects(tracked []hydrate.TrackedK8sObject) []trackedObjectGroup {
	groups := make([]trackedObjectGroup, 0, len(tracked))
	index := make(map[string]int, len(tracked))
	for _, object := range tracked {
		key := trackedObjectKey(object)
		if position, ok := index[key]; ok {
			groups[position].fragments = append(groups[position].fragments, object)
			continue
		}
		index[key] = len(groups)
		groups = append(groups, trackedObjectGroup{
			primary:   object,
			fragments: []hydrate.TrackedK8sObject{object},
		})
	}
	return groups
}

func trackedObjectKey(object hydrate.TrackedK8sObject) string {
	return strings.Join([]string{object.APIVersion, object.Kind, object.Namespace, object.Name}, "\x00")
}

func compileDesiredProjection(bundleRoot string, fragments []hydrate.TrackedK8sObject, policy ownership.LocalPatchPolicy, policyName string) (*desiredProjection, error) {
	if len(fragments) == 0 {
		return nil, fmt.Errorf("desired projection fragments cannot be empty")
	}

	sortedFragments := append([]hydrate.TrackedK8sObject(nil), fragments...)
	sort.SliceStable(sortedFragments, func(i, j int) bool {
		left := desiredFragmentPriority(sortedFragments[i])
		right := desiredFragmentPriority(sortedFragments[j])
		if left != right {
			return left < right
		}
		return sortedFragments[i].Path < sortedFragments[j].Path
	})

	projection := &desiredProjection{
		kind:       sortedFragments[0].Kind,
		ownership:  make(pathOwnershipIndex),
		policy:     policy,
		policyName: policyName,
	}
	for _, fragment := range sortedFragments {
		raw, err := desiredTrackedObject(bundleRoot, fragment)
		if err != nil {
			return nil, err
		}
		value, err := normalizedObjectValue(raw)
		if err != nil {
			return nil, fmt.Errorf("normalize desired fragment %s %s/%s from %q: %w", fragment.Kind, fragment.Namespace, fragment.Name, fragment.Path, err)
		}
		projection.merged = mergeDesiredValue(nil, projection.merged, value, fragment.Ownership, projection.ownership)
	}
	return projection, nil
}

func desiredFragmentPriority(fragment hydrate.TrackedK8sObject) int {
	switch fragment.Ownership {
	case hydrate.InventoryOwnershipGlobal:
		return 0
	case hydrate.InventoryOwnershipLocal:
		return 1
	default:
		return 2
	}
}

func (p *desiredProjection) mismatches(liveRaw []byte) ([]FieldMismatch, error) {
	if p == nil {
		return nil, fmt.Errorf("desired projection cannot be nil")
	}
	live, err := normalizedObjectValue(liveRaw)
	if err != nil {
		return nil, err
	}
	return p.ownership.annotateMismatches(p.kind, p.policy, p.policyName, diffOwnedFieldsAtPath(nil, p.merged, live)), nil
}

func digestNormalizedValue(value interface{}) (string, error) {
	if value == nil {
		return "", fmt.Errorf("normalized value cannot be nil")
	}
	data, err := json.Marshal(value)
	if err != nil {
		return "", fmt.Errorf("marshal normalized object: %w", err)
	}
	return digest.Canonical.FromBytes(data).String(), nil
}

func highestMismatchState(mismatches []FieldMismatch) state.ClusterState {
	current := state.StateClean
	for _, mismatch := range mismatches {
		switch mismatch.State {
		case state.StateOrphan:
			return state.StateOrphan
		case state.StateDirty:
			current = state.StateDirty
		case state.StateDegraded:
			if current == state.StateClean {
				current = state.StateDegraded
			}
		}
	}
	return current
}

func (p pathOwnershipIndex) maxState() state.ClusterState {
	current := state.StateDegraded
	for _, ownership := range p {
		switch ownership {
		case hydrate.InventoryOwnershipGlobal:
			return state.StateOrphan
		case hydrate.InventoryOwnershipLocal:
			current = state.StateDirty
		}
	}
	return current
}

func (p pathOwnershipIndex) annotateMismatches(kind string, policy ownership.LocalPatchPolicy, policyName string, mismatches []FieldMismatch) []FieldMismatch {
	if len(mismatches) == 0 {
		return nil
	}
	annotated := make([]FieldMismatch, 0, len(mismatches))
	for _, mismatch := range mismatches {
		mismatchOwnership := p.resolve(mismatch.Path)
		mismatch.Ownership = mismatchOwnership
		mismatch.State = driftStateForOwnership(mismatchOwnership)
		if len(normalizeLocalPatchPolicyPath(mismatch.Path)) > 0 && policy.IsAllowed(kind, normalizeLocalPatchPolicyPath(mismatch.Path)) {
			mismatch.PolicyName = policyName
			mismatch.PolicyEligible = true
		}
		annotated = append(annotated, mismatch)
	}
	return annotated
}

func (p pathOwnershipIndex) resolve(path string) hydrate.InventoryOwnership {
	if ownership, ok := p[path]; ok {
		return ownership
	}
	best := ""
	bestOwnership := hydrate.InventoryOwnership("")
	for candidate, ownership := range p {
		if candidate == "" {
			continue
		}
		if path == candidate || strings.HasPrefix(path, candidate+".") || strings.HasPrefix(path, candidate+"[") {
			if len(candidate) > len(best) {
				best = candidate
				bestOwnership = ownership
			}
		}
	}
	return bestOwnership
}

func driftStateForOwnership(ownership hydrate.InventoryOwnership) state.ClusterState {
	switch ownership {
	case hydrate.InventoryOwnershipGlobal:
		return state.StateOrphan
	case hydrate.InventoryOwnershipLocal:
		return state.StateDirty
	default:
		return state.StateDegraded
	}
}

func desiredTrackedObject(bundleRoot string, tracked hydrate.TrackedK8sObject) ([]byte, error) {
	targetPath, err := resolveBundlePath(bundleRoot, tracked.Path)
	if err != nil {
		return nil, err
	}

	info, err := os.Stat(targetPath)
	if err != nil {
		return nil, err
	}

	if info.IsDir() {
		files := make([]string, 0)
		if err := filepath.WalkDir(targetPath, func(path string, entry os.DirEntry, walkErr error) error {
			if walkErr != nil {
				return walkErr
			}
			if entry.IsDir() {
				return nil
			}
			files = append(files, path)
			return nil
		}); err != nil {
			return nil, err
		}
		sort.Strings(files)
		for _, path := range files {
			doc, ok, err := findTrackedObjectInFile(path, tracked)
			if err != nil {
				return nil, err
			}
			if ok {
				return doc, nil
			}
		}
		return nil, fmt.Errorf("tracked object %s %s/%s not found under %q", tracked.Kind, tracked.Namespace, tracked.Name, tracked.Path)
	}

	doc, ok, err := findTrackedObjectInFile(targetPath, tracked)
	if err != nil {
		return nil, err
	}
	if !ok {
		return nil, fmt.Errorf("tracked object %s %s/%s not found in %q", tracked.Kind, tracked.Namespace, tracked.Name, tracked.Path)
	}
	return doc, nil
}

func compareTrackedHostPath(bundleRoot, hostRoot, host string, tracked hydrate.TrackedHostPath) (HostPathStatus, error) {
	if isGeneratedTrackedHostPath(tracked) {
		return compareGeneratedTrackedHostPath(hostRoot, tracked)
	}

	desiredBundlePath := hostDesiredBundlePath(tracked, host)
	desiredPath, err := resolveBundlePath(bundleRoot, desiredBundlePath)
	if err != nil {
		return HostPathStatus{}, err
	}
	desiredDigest, expected, err := digestTrackedHostPath(desiredPath, tracked.Type)
	if err != nil {
		return HostPathStatus{}, fmt.Errorf("normalize desired host path %q from %q: %w", tracked.HostPath, desiredBundlePath, err)
	}

	status := HostPathStatus{
		Tracked:       tracked,
		Presence:      ObjectPresencePresent,
		State:         driftStateForOwnership(tracked.Ownership),
		DesiredDigest: desiredDigest,
	}

	livePath, err := resolveLiveHostPath(hostRoot, tracked.HostPath)
	if err != nil {
		return HostPathStatus{}, err
	}
	liveDigest, actual, err := digestTrackedHostPath(livePath, tracked.Type)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			status.Presence = ObjectPresenceMissing
			status.State = driftStateForOwnership(tracked.Ownership)
			status.Remediation = directHostPathRemediation(status)
			return status, nil
		}
		return HostPathStatus{}, fmt.Errorf("normalize live host path %q: %w", tracked.HostPath, err)
	}
	status.LiveDigest = liveDigest

	if mismatches := diffTrackedHostPath(expected, actual, driftStateForOwnership(tracked.Ownership)); len(mismatches) > 0 {
		status.Comparison = ObjectComparisonDrifted
		status.State = driftStateForOwnership(tracked.Ownership)
		status.Mismatches = mismatches
		status.Remediation = directHostPathRemediation(status)
		return status, nil
	}

	status.Comparison = ObjectComparisonMatched
	status.State = state.StateClean
	return status, nil
}

func hostDesiredBundlePath(tracked hydrate.TrackedHostPath, host string) string {
	if path := hostInputBindingPathForHost(tracked.HostInputBindings, host); path != "" {
		return path
	}
	return tracked.BundlePath
}

func isGeneratedTrackedHostPath(tracked hydrate.TrackedHostPath) bool {
	return tracked.ProjectionClass == hydrate.HostPathProjectionClassGenerated ||
		tracked.CompareStrategy == hydrate.HostPathCompareStrategySemanticGenerated ||
		tracked.Generated != nil
}

func compareGeneratedTrackedHostPath(hostRoot string, tracked hydrate.TrackedHostPath) (HostPathStatus, error) {
	desiredDigest, err := digestGeneratedHostPathExpectation(tracked)
	if err != nil {
		return HostPathStatus{}, err
	}

	status := HostPathStatus{
		Tracked:       tracked,
		Presence:      ObjectPresencePresent,
		State:         driftStateForOwnership(tracked.Ownership),
		DesiredDigest: desiredDigest,
	}

	livePath, err := resolveLiveHostPath(hostRoot, tracked.HostPath)
	if err != nil {
		return HostPathStatus{}, err
	}
	info, err := os.Lstat(livePath)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			status.Presence = ObjectPresenceMissing
			status.Remediation = generatedHostPathRemediation(tracked, status)
			return status, nil
		}
		return HostPathStatus{}, fmt.Errorf("stat generated host path %q: %w", tracked.HostPath, err)
	}
	actualType, ok := modeToHostPathType(info.Mode())
	if !ok {
		status.Comparison = ObjectComparisonDrifted
		status.Mismatches = []HostPathMismatch{{
			Reason:  "unsupportedType",
			Desired: string(tracked.Type),
			Live:    info.Mode().String(),
			State:   driftStateForOwnership(tracked.Ownership),
		}}
		status.Remediation = generatedHostPathRemediation(tracked, status)
		return status, nil
	}
	if tracked.Type != "" && actualType != tracked.Type {
		status.Comparison = ObjectComparisonDrifted
		status.Mismatches = []HostPathMismatch{{
			Reason:  "typeMismatch",
			Desired: string(tracked.Type),
			Live:    string(actualType),
			State:   driftStateForOwnership(tracked.Ownership),
		}}
		status.Remediation = generatedHostPathRemediation(tracked, status)
		return status, nil
	}
	if actualType != hydrate.HostPathRegularFile {
		status.Comparison = ObjectComparisonDrifted
		status.Mismatches = []HostPathMismatch{{
			Reason:  "unsupportedGeneratedType",
			Desired: string(hydrate.HostPathRegularFile),
			Live:    string(actualType),
			State:   driftStateForOwnership(tracked.Ownership),
		}}
		status.Remediation = generatedHostPathRemediation(tracked, status)
		return status, nil
	}

	raw, err := os.ReadFile(livePath)
	if err != nil {
		return HostPathStatus{}, fmt.Errorf("read generated host path %q: %w", tracked.HostPath, err)
	}
	liveDigest, err := normalizedObjectDigest(raw)
	if err != nil {
		status.Comparison = ObjectComparisonDrifted
		status.Mismatches = []HostPathMismatch{{
			Reason:  "semanticParseError",
			Desired: generatedHostPathIdentityString(tracked.Generated),
			Live:    err.Error(),
			State:   driftStateForOwnership(tracked.Ownership),
		}}
		status.Remediation = generatedHostPathRemediation(tracked, status)
		return status, nil
	}
	status.LiveDigest = liveDigest

	liveValue, err := normalizedObjectValue(raw)
	if err != nil {
		status.Comparison = ObjectComparisonDrifted
		status.Mismatches = []HostPathMismatch{{
			Reason:  "semanticParseError",
			Desired: generatedHostPathIdentityString(tracked.Generated),
			Live:    err.Error(),
			State:   driftStateForOwnership(tracked.Ownership),
		}}
		status.Remediation = generatedHostPathRemediation(tracked, status)
		return status, nil
	}
	mismatches := diffGeneratedHostPathSemantics(liveValue, tracked.Generated, driftStateForOwnership(tracked.Ownership))
	if len(mismatches) == 0 {
		status.Comparison = ObjectComparisonMatched
		status.State = state.StateClean
		return status, nil
	}

	status.Comparison = ObjectComparisonDrifted
	status.Mismatches = mismatches
	status.Remediation = generatedHostPathRemediation(tracked, status)
	return status, nil
}

func digestGeneratedHostPathExpectation(tracked hydrate.TrackedHostPath) (string, error) {
	if tracked.Generated == nil {
		return "", fmt.Errorf("generated host path %q is missing semantic metadata", tracked.HostPath)
	}
	value := map[string]interface{}{
		"tool":            tracked.Generated.Tool,
		"hook":            tracked.Generated.Hook,
		"apiVersion":      tracked.Generated.APIVersion,
		"kind":            tracked.Generated.Kind,
		"namespace":       tracked.Generated.Namespace,
		"name":            tracked.Generated.Name,
		"containerName":   tracked.Generated.ContainerName,
		"expectedImage":   tracked.Generated.ExpectedImage,
		"expectedCommand": tracked.Generated.ExpectedCommand,
		"expectedArgs":    tracked.Generated.ExpectedArgs,
		"expectedMounts":  tracked.Generated.ExpectedVolumeMounts,
		"projectionClass": tracked.ProjectionClass,
	}
	return digestNormalizedValue(value)
}

func diffGeneratedHostPathSemantics(live interface{}, semantics *hydrate.GeneratedHostPathSemantics, driftState state.ClusterState) []HostPathMismatch {
	if semantics == nil {
		return []HostPathMismatch{{
			Reason: "missingSemanticExpectation",
			State:  driftState,
		}}
	}
	liveObject, ok := live.(map[string]interface{})
	if !ok {
		return []HostPathMismatch{{
			Reason:  "typeMismatch",
			Desired: "KubernetesObject",
			Live:    fmt.Sprintf("%T", live),
			State:   driftState,
		}}
	}

	mismatches := make([]HostPathMismatch, 0, 2)
	if apiVersion, _ := liveObject["apiVersion"].(string); apiVersion != semantics.APIVersion {
		mismatches = append(mismatches, HostPathMismatch{
			Reason:  "identityMismatch",
			Desired: generatedHostPathIdentityString(semantics),
			Live:    generatedHostPathIdentityFromObject(liveObject),
			State:   driftState,
		})
		return mismatches
	}
	if kind, _ := liveObject["kind"].(string); kind != semantics.Kind {
		mismatches = append(mismatches, HostPathMismatch{
			Reason:  "identityMismatch",
			Desired: generatedHostPathIdentityString(semantics),
			Live:    generatedHostPathIdentityFromObject(liveObject),
			State:   driftState,
		})
		return mismatches
	}

	metadata, _ := liveObject["metadata"].(map[string]interface{})
	if name, _ := metadata["name"].(string); name != semantics.Name {
		mismatches = append(mismatches, HostPathMismatch{
			Reason:  "identityMismatch",
			Desired: generatedHostPathIdentityString(semantics),
			Live:    generatedHostPathIdentityFromObject(liveObject),
			State:   driftState,
		})
		return mismatches
	}
	if semantics.Namespace != "" {
		if namespace, _ := metadata["namespace"].(string); namespace != semantics.Namespace {
			mismatches = append(mismatches, HostPathMismatch{
				Reason:  "identityMismatch",
				Desired: generatedHostPathIdentityString(semantics),
				Live:    generatedHostPathIdentityFromObject(liveObject),
				State:   driftState,
			})
			return mismatches
		}
	}
	if semantics.ContainerName == "" {
		return nil
	}

	spec, _ := liveObject["spec"].(map[string]interface{})
	containers, _ := spec["containers"].([]interface{})
	for _, item := range containers {
		container, ok := item.(map[string]interface{})
		if !ok {
			continue
		}
		if name, _ := container["name"].(string); name == semantics.ContainerName {
			mismatches := make([]HostPathMismatch, 0, 4)
			if semantics.ExpectedImage != "" {
				image, _ := container["image"].(string)
				if image != semantics.ExpectedImage {
					mismatches = append(mismatches, HostPathMismatch{
						Reason:  "imageMismatch",
						Desired: semantics.ExpectedImage,
						Live:    image,
						State:   driftState,
					})
				}
			}
			command, _ := stringSliceField(container["command"])
			if semantics.ExpectedCommand != "" {
				if len(command) == 0 {
					mismatches = append(mismatches, HostPathMismatch{
						Reason:  "missingExpectedCommand",
						Desired: semantics.ExpectedCommand,
						State:   driftState,
					})
				} else if command[0] != semantics.ExpectedCommand {
					mismatches = append(mismatches, HostPathMismatch{
						Reason:  "commandMismatch",
						Desired: semantics.ExpectedCommand,
						Live:    command[0],
						State:   driftState,
					})
				}
			}
			if len(semantics.ExpectedArgs) > 0 && len(command) > 0 {
				actualArgs := parseCommandArgs(command[1:])
				for key, expected := range semantics.ExpectedArgs {
					actual, ok := actualArgs[key]
					if !ok {
						mismatches = append(mismatches, HostPathMismatch{
							Reason:  "missingExpectedFlag",
							Desired: fmt.Sprintf("--%s=%s", key, expected),
							State:   driftState,
						})
						continue
					}
					if expected == "" || actual == expected {
						continue
					}
					mismatches = append(mismatches, HostPathMismatch{
						Reason:  "flagMismatch",
						Desired: fmt.Sprintf("--%s=%s", key, expected),
						Live:    fmt.Sprintf("--%s=%s", key, actual),
						State:   driftState,
					})
				}
			}
			if len(semantics.ExpectedVolumeMounts) > 0 {
				mounts := volumeMountPaths(container)
				for _, expected := range semantics.ExpectedVolumeMounts {
					if stringSliceContains(mounts, expected) {
						continue
					}
					mismatches = append(mismatches, HostPathMismatch{
						Reason:  "missingExpectedVolumeMount",
						Desired: expected,
						State:   driftState,
					})
				}
			}
			return mismatches
		}
	}
	return []HostPathMismatch{{
		Reason:  "missingExpectedContainer",
		Desired: semantics.ContainerName,
		State:   driftState,
	}}
}

func stringSliceField(value interface{}) ([]string, bool) {
	items, ok := value.([]interface{})
	if !ok {
		return nil, false
	}
	out := make([]string, 0, len(items))
	for _, item := range items {
		text, ok := item.(string)
		if !ok {
			return nil, false
		}
		out = append(out, text)
	}
	return out, true
}

func parseCommandArgs(command []string) map[string]string {
	args := make(map[string]string)
	for i := 0; i < len(command); i++ {
		token := command[i]
		if !strings.HasPrefix(token, "--") {
			continue
		}
		trimmed := strings.TrimPrefix(token, "--")
		if index := strings.Index(trimmed, "="); index >= 0 {
			args[trimmed[:index]] = trimmed[index+1:]
			continue
		}
		value := ""
		if i+1 < len(command) && !strings.HasPrefix(command[i+1], "--") {
			value = command[i+1]
			i++
		}
		args[trimmed] = value
	}
	return args
}

func volumeMountPaths(container map[string]interface{}) []string {
	mounts, _ := container["volumeMounts"].([]interface{})
	paths := make([]string, 0, len(mounts))
	for _, item := range mounts {
		mount, ok := item.(map[string]interface{})
		if !ok {
			continue
		}
		path, _ := mount["mountPath"].(string)
		if path == "" {
			continue
		}
		paths = append(paths, path)
	}
	return paths
}

func stringSliceContains(values []string, target string) bool {
	for _, value := range values {
		if value == target {
			return true
		}
	}
	return false
}

func objectRemediation(status ObjectStatus) *HostPathRemediation {
	if status.State == state.StateClean {
		return nil
	}
	policyEligiblePaths := localPatchPolicyEligiblePaths(status)
	policyName := localPatchPolicyName(status)
	if status.State == state.StateOrphan {
		remediation := &HostPathRemediation{
			Action:           "reviewDistributionBaselineForAppliedObject",
			ChangeOwner:      "globalBaseline",
			Source:           objectRemediationSource(status),
			SafeDirectRevert: true,
			SafeCommit:       false,
			Message:          "review the selected BOM/package baseline for this applied object; sync revert can restore the recorded desired object, while baseline changes should go through package/BOM updates",
			NextSteps: []string{
				"Inspect the selected BOM revision and package manifest that define this object.",
				"If the live drift should be discarded, run sync revert against the recorded desired state.",
				"If the baseline is wrong, update the package or BOM selection, rerender, and apply the refreshed bundle.",
			},
			AllowedCommands: []string{
				"sync diff",
				"sync status",
				"sync revert",
				"sync render",
				"sync apply",
				"sync package build",
				"sync package push",
			},
			CommandGuidance: []HostPathCommandGuidance{
				{Command: "sync diff", Availability: "unknown"},
				{Command: "sync status", Availability: "unknown"},
				{Command: "sync revert", Preconditions: []string{"bundleMatchesRecordedDesiredStateDigest"}, Availability: "unknown"},
				{Command: "sync render", Availability: "unknown"},
				{Command: "sync apply", Preconditions: []string{"bundleMatchesRecordedDesiredStateDigest"}, Availability: "unknown"},
				{Command: "sync package build", Availability: "unknown"},
				{Command: "sync package push", Availability: "unknown"},
			},
		}
		if len(policyEligiblePaths) > 0 {
			remediation.PolicyName = policyName
			remediation.PolicyEligiblePaths = policyEligiblePaths
			remediation.Message = "review whether this applied object drift should become a cluster-local patch or be reverted; the changed fields fall within the selected local patch policy, but live state is still outside recorded desired state"
			remediation.NextSteps = []string{
				"Inspect the drifted fields and confirm they only touch allowed local patch surfaces.",
				"If the drift should be kept for this cluster, add or update a local repo patch, rerender, and apply the refreshed desired state.",
				"If the drift should be discarded, run sync revert against the recorded desired state.",
			}
		}
		return remediation
	}
	if status.State != state.StateDirty {
		return nil
	}

	remediation := &HostPathRemediation{
		Action:           "reviewLocalObjectOverlayAndCommitOrReapply",
		ChangeOwner:      "localOverlay",
		Source:           localObjectRemediationSource(status),
		SafeDirectRevert: true,
		SafeCommit:       status.Presence == ObjectPresencePresent,
		Message:          "review the cluster-local desired object overlay; sync commit can persist an approved live drift, while sync revert restores the recorded desired object",
		NextSteps: []string{
			"Inspect the local repo overlay that owns this object drift and compare it with the live object.",
			"If the live drift should be kept, run sync commit; if it should be discarded, run sync revert.",
			"If you edit the local repo manually, rerender and apply the refreshed desired state afterward.",
		},
		AllowedCommands: []string{
			"sync diff",
			"sync status",
			"sync revert",
			"sync render",
			"sync apply",
		},
		CommandGuidance: []HostPathCommandGuidance{
			{Command: "sync diff", Availability: "unknown"},
			{Command: "sync status", Availability: "unknown"},
			{Command: "sync revert", Preconditions: []string{"bundleMatchesRecordedDesiredStateDigest"}, Availability: "unknown"},
			{Command: "sync render", Availability: "unknown"},
			{Command: "sync apply", Preconditions: []string{"bundleMatchesRecordedDesiredStateDigest"}, Availability: "unknown"},
		},
	}
	if hasLocalPatchFragment(status) && len(policyEligiblePaths) > 0 {
		remediation.PolicyName = policyName
		remediation.PolicyEligiblePaths = policyEligiblePaths
	}
	if status.Presence == ObjectPresencePresent {
		remediation.AllowedCommands = insertAllowedCommand(remediation.AllowedCommands, 2, "sync commit")
		remediation.CommandGuidance = insertCommandGuidance(remediation.CommandGuidance, 2, HostPathCommandGuidance{
			Command:       "sync commit",
			Preconditions: []string{"bundleMatchesRecordedDesiredStateDigest"},
			Availability:  "unknown",
		})
		remediation.NextSteps[1] = "If the live drift should be kept, run sync commit; if it should be discarded, run sync revert."
	} else {
		remediation.Message = "review the cluster-local desired object overlay; sync revert can restore a missing local-owned object, while manual local repo edits should be rerendered and applied"
		remediation.NextSteps[1] = "If the object should exist, run sync revert to restore it; otherwise edit the local repo and rerender the desired state."
	}
	return remediation
}

func directHostPathRemediation(status HostPathStatus) *HostPathRemediation {
	if status.State == state.StateClean || isGeneratedTrackedHostPath(status.Tracked) {
		return nil
	}
	if status.State == state.StateOrphan {
		return &HostPathRemediation{
			Action:           "reviewDistributionBaselineForHostPath",
			ChangeOwner:      "globalBaseline",
			Source:           directHostPathRemediationSource(status.Tracked),
			SafeDirectRevert: true,
			SafeCommit:       false,
			Message:          "review the selected BOM/package baseline for this host path; sync revert can restore the recorded desired projection, while baseline changes should go through package/BOM updates",
			NextSteps: []string{
				"Inspect the selected package content that projects to this host path.",
				"If the live drift should be discarded, run sync revert against the recorded desired state.",
				"If the baseline is wrong, update the package or BOM selection, rerender, and apply the refreshed bundle.",
			},
			AllowedCommands: []string{
				"sync diff",
				"sync status",
				"sync revert",
				"sync render",
				"sync apply",
				"sync package build",
				"sync package push",
			},
			CommandGuidance: []HostPathCommandGuidance{
				{Command: "sync diff", Availability: "unknown"},
				{Command: "sync status", Availability: "unknown"},
				{Command: "sync revert", Preconditions: []string{"bundleMatchesRecordedDesiredStateDigest"}, Availability: "unknown"},
				{Command: "sync render", Availability: "unknown"},
				{Command: "sync apply", Preconditions: []string{"bundleMatchesRecordedDesiredStateDigest"}, Availability: "unknown"},
				{Command: "sync package build", Availability: "unknown"},
				{Command: "sync package push", Availability: "unknown"},
			},
		}
	}
	if status.State != state.StateDirty {
		return nil
	}

	remediation := &HostPathRemediation{
		Action:           "reviewLocalHostInputAndCommitOrReapply",
		ChangeOwner:      "localInput",
		Source:           localHostPathRemediationSource(status.Tracked),
		SafeDirectRevert: true,
		SafeCommit:       status.Presence == ObjectPresencePresent,
		Message:          "review the cluster-local input that owns this host projection; sync commit can persist an approved live drift, while sync revert restores the recorded desired file",
		NextSteps: []string{
			"Inspect the cluster-local input that feeds this host path projection and compare it with the live file.",
			"If the live drift should be kept, run sync commit; if it should be discarded, run sync revert.",
			"If you edit the local repo manually, rerender and apply the refreshed desired state afterward.",
		},
		AllowedCommands: []string{
			"sync diff",
			"sync status",
			"sync revert",
			"sync render",
			"sync apply",
		},
		CommandGuidance: []HostPathCommandGuidance{
			{Command: "sync diff", Availability: "unknown"},
			{Command: "sync status", Availability: "unknown"},
			{Command: "sync revert", Preconditions: []string{"bundleMatchesRecordedDesiredStateDigest"}, Availability: "unknown"},
			{Command: "sync render", Availability: "unknown"},
			{Command: "sync apply", Preconditions: []string{"bundleMatchesRecordedDesiredStateDigest"}, Availability: "unknown"},
		},
	}
	if status.Presence == ObjectPresencePresent {
		remediation.AllowedCommands = insertAllowedCommand(remediation.AllowedCommands, 2, "sync commit")
		remediation.CommandGuidance = insertCommandGuidance(remediation.CommandGuidance, 2, HostPathCommandGuidance{
			Command:       "sync commit",
			Preconditions: []string{"bundleMatchesRecordedDesiredStateDigest"},
			Availability:  "unknown",
		})
	} else {
		remediation.SafeCommit = false
		remediation.Message = "review the cluster-local input that owns this host projection; sync revert can restore a missing local-owned file, while manual local repo edits should be rerendered and applied"
		remediation.NextSteps[1] = "If the file should exist, run sync revert to restore it; otherwise edit the local repo input and rerender the desired state."
	}
	return remediation
}

func objectRemediationSource(status ObjectStatus) string {
	switch status.Tracked.Source {
	case hydrate.InventorySourcePackageManifest:
		return "selected BOM/package manifest"
	case hydrate.InventorySourceGeneratedHook:
		return "generated hook projection tracked by the selected baseline"
	default:
		return "selected distribution baseline"
	}
}

func localObjectRemediationSource(status ObjectStatus) string {
	hasPatch := false
	hasResource := false
	for _, fragment := range status.Fragments {
		switch fragment.Source {
		case hydrate.InventorySourceLocalPatch:
			hasPatch = true
		case hydrate.InventorySourceLocalResource:
			hasResource = true
		}
	}
	switch {
	case hasPatch && hasResource:
		return "local repo patch plus local repo resource"
	case hasPatch:
		return "local repo patch"
	case hasResource:
		return "local repo resource"
	default:
		return "cluster-local object overlay"
	}
}

func hasLocalPatchFragment(status ObjectStatus) bool {
	for _, fragment := range status.Fragments {
		if fragment.Source == hydrate.InventorySourceLocalPatch {
			return true
		}
	}
	return false
}

func localPatchPolicyEligiblePaths(status ObjectStatus) []string {
	if len(status.Mismatches) == 0 {
		return nil
	}
	eligible := make([]string, 0, len(status.Mismatches))
	seen := make(map[string]struct{}, len(status.Mismatches))
	for _, mismatch := range status.Mismatches {
		if mismatch.Path == "" || !mismatch.PolicyEligible {
			continue
		}
		if _, exists := seen[mismatch.Path]; exists {
			continue
		}
		seen[mismatch.Path] = struct{}{}
		eligible = append(eligible, mismatch.Path)
	}
	sort.Strings(eligible)
	return eligible
}

func localPatchPolicyName(status ObjectStatus) string {
	for _, mismatch := range status.Mismatches {
		if mismatch.Path == "" || !mismatch.PolicyEligible {
			continue
		}
		if strings.TrimSpace(mismatch.PolicyName) == "" {
			continue
		}
		return mismatch.PolicyName
	}
	return ownership.DefaultLocalPatchPolicyName
}

func normalizeLocalPatchPolicyPath(path string) []string {
	if strings.TrimSpace(path) == "" {
		return nil
	}
	parts := strings.Split(path, ".")
	segments := make([]string, 0, len(parts))
	for _, part := range parts {
		segment := part
		if index := strings.Index(segment, "["); index >= 0 {
			segment = segment[:index]
		}
		if strings.TrimSpace(segment) == "" {
			continue
		}
		segments = append(segments, segment)
	}
	return segments
}

func directHostPathRemediationSource(tracked hydrate.TrackedHostPath) string {
	switch tracked.Source {
	case hydrate.InventorySourcePackageManifest:
		return "package file or rootfs content from the selected BOM/package"
	default:
		return "selected distribution baseline host projection"
	}
}

func localHostPathRemediationSource(tracked hydrate.TrackedHostPath) string {
	if tracked.InputName != "" {
		return fmt.Sprintf("local repo input %q", tracked.InputName)
	}
	return "cluster-local host input"
}

func insertAllowedCommand(values []string, index int, value string) []string {
	if index < 0 || index >= len(values) {
		return append(values, value)
	}
	values = append(values[:index], append([]string{value}, values[index:]...)...)
	return values
}

func insertCommandGuidance(values []HostPathCommandGuidance, index int, value HostPathCommandGuidance) []HostPathCommandGuidance {
	if index < 0 || index >= len(values) {
		return append(values, value)
	}
	values = append(values[:index], append([]HostPathCommandGuidance{value}, values[index:]...)...)
	return values
}

func generatedHostPathRemediation(tracked hydrate.TrackedHostPath, status HostPathStatus) *HostPathRemediation {
	if tracked.ProjectionClass != hydrate.HostPathProjectionClassGenerated || tracked.Generated == nil {
		return nil
	}
	if status.State == state.StateClean {
		return nil
	}

	source := "generated static Pod projection"
	if tracked.Generated.Tool == "kubeadm" {
		source = "rendered kubeadm config (files/etc/kubernetes/kubeadm.yaml)"
	}
	base := generatedHostPathRemediationBase(tracked, source)

	manualReview := false
	for _, mismatch := range status.Mismatches {
		switch mismatch.Reason {
		case "semanticParseError", "typeMismatch", "missingSemanticExpectation":
			manualReview = true
		}
	}

	if manualReview {
		remediation := base
		remediation.Action = "manualReviewGeneratedProjection"
		remediation.ChangeOwner = "manualReview"
		remediation.SafeDirectRevert = false
		remediation.SafeCommit = false
		remediation.Message = "inspect the live generated projection and its bootstrap source before rerendering; direct sync commit/revert is not supported for generated projections"
		remediation.NextSteps = []string{
			"Inspect the live generated static Pod manifest and identify why Sealos could not classify it semantically.",
			"Compare the live projection with the rendered kubeadm input and the selected BOM/package baseline.",
			"After manual review, fix the bootstrap input or baseline source, then rerender and apply again.",
		}
		remediation.AllowedCommands = []string{
			"sync diff",
			"sync status",
			"sync render",
			"sync apply",
		}
		remediation.CommandGuidance = []HostPathCommandGuidance{
			{Command: "sync diff", Availability: "unknown"},
			{Command: "sync status", Availability: "unknown"},
			{Command: "sync render", Availability: "unknown"},
			{Command: "sync apply", Preconditions: []string{"bundleMatchesRecordedDesiredStateDigest"}, Availability: "unknown"},
		}
		return &remediation
	}

	if generatedProjectionNeedsLocalInputChange(status.Mismatches) {
		remediation := base
		remediation.Action = "updateLocalBootstrapInputAndRerender"
		remediation.ChangeOwner = "localInput"
		remediation.SafeDirectRevert = false
		remediation.SafeCommit = false
		remediation.Message = "update the cluster-local bootstrap input that produced this generated projection, then rerender and sync apply; direct sync commit/revert is not supported for generated projections"
		remediation.NextSteps = []string{
			"Update the cluster-local bootstrap input that feeds the rendered kubeadm config.",
			"Rerender the bundle so the generated projection expectation is recalculated from the new local input.",
			"Re-run diff or status, then apply the refreshed desired state.",
		}
		remediation.AllowedCommands = []string{
			"sync render",
			"sync diff",
			"sync status",
			"sync apply",
		}
		remediation.CommandGuidance = []HostPathCommandGuidance{
			{Command: "sync render", Availability: "unknown"},
			{Command: "sync diff", Availability: "unknown"},
			{Command: "sync status", Availability: "unknown"},
			{Command: "sync apply", Preconditions: []string{"bundleMatchesRecordedDesiredStateDigest"}, Availability: "unknown"},
		}
		return &remediation
	}

	remediation := base
	remediation.Action = "reviewDistributionBaselineForGeneratedProjection"
	remediation.ChangeOwner = "globalBaseline"
	remediation.SafeDirectRevert = false
	remediation.SafeCommit = false
	remediation.Message = "review the selected BOM/package baseline and bootstrap flow for this generated projection; direct sync commit/revert is not supported for generated projections"
	remediation.NextSteps = []string{
		"Review the selected BOM revision and package baseline that define this generated projection.",
		"If the baseline is wrong, update the package content or BOM selection and rerender the desired state.",
		"Re-run diff or status, then apply the refreshed bundle.",
	}
	remediation.AllowedCommands = []string{
		"sync render",
		"sync diff",
		"sync status",
		"sync apply",
		"sync package build",
		"sync package push",
	}
	remediation.CommandGuidance = []HostPathCommandGuidance{
		{Command: "sync render", Availability: "unknown"},
		{Command: "sync diff", Availability: "unknown"},
		{Command: "sync status", Availability: "unknown"},
		{Command: "sync apply", Preconditions: []string{"bundleMatchesRecordedDesiredStateDigest"}, Availability: "unknown"},
		{Command: "sync package build", Availability: "unknown"},
		{Command: "sync package push", Availability: "unknown"},
	}
	return &remediation
}

func generatedHostPathRemediationBase(tracked hydrate.TrackedHostPath, source string) HostPathRemediation {
	repairable := isRepairableGeneratedHostPath(tracked)
	remediation := HostPathRemediation{
		Source:          source,
		ProjectionClass: string(tracked.ProjectionClass),
		Repairable:      &repairable,
	}
	if tracked.Generated != nil {
		remediation.Generator = tracked.Generated.Tool
		remediation.GeneratedKind = tracked.Generated.Kind
		remediation.GeneratedName = tracked.Generated.Name
	}
	return remediation
}

func isRepairableGeneratedHostPath(tracked hydrate.TrackedHostPath) bool {
	if tracked.ProjectionClass != hydrate.HostPathProjectionClassGenerated || tracked.Generated == nil {
		return false
	}
	if tracked.Generated.Tool != "kubeadm" {
		return false
	}
	switch strings.TrimSpace(tracked.HostPath) {
	case "/etc/kubernetes/manifests/kube-apiserver.yaml",
		"/etc/kubernetes/manifests/kube-controller-manager.yaml",
		"/etc/kubernetes/manifests/kube-scheduler.yaml":
		return true
	default:
		return false
	}
}

func generatedProjectionNeedsLocalInputChange(mismatches []HostPathMismatch) bool {
	if len(mismatches) == 0 {
		return false
	}
	for _, mismatch := range mismatches {
		switch mismatch.Reason {
		case "missingExpectedFlag", "flagMismatch", "missingExpectedVolumeMount":
			continue
		default:
			return false
		}
	}
	return true
}

func generatedHostPathIdentityString(semantics *hydrate.GeneratedHostPathSemantics) string {
	if semantics == nil {
		return "<unknown generated host path>"
	}
	parts := []string{semantics.APIVersion, semantics.Kind, semantics.Name}
	if semantics.Namespace != "" {
		parts = append(parts, "namespace="+semantics.Namespace)
	}
	return strings.Join(parts, " ")
}

func generatedHostPathIdentityFromObject(object map[string]interface{}) string {
	if object == nil {
		return "<invalid object>"
	}
	apiVersion, _ := object["apiVersion"].(string)
	kind, _ := object["kind"].(string)
	metadata, _ := object["metadata"].(map[string]interface{})
	name, _ := metadata["name"].(string)
	namespace, _ := metadata["namespace"].(string)
	parts := []string{apiVersion, kind, name}
	if namespace != "" {
		parts = append(parts, "namespace="+namespace)
	}
	return strings.Join(parts, " ")
}

type trackedHostPathProjection struct {
	Type   hydrate.HostPathType
	Mode   os.FileMode
	Digest string
	Target string
}

func digestTrackedHostPath(path string, expectedType hydrate.HostPathType) (string, trackedHostPathProjection, error) {
	info, err := os.Lstat(path)
	if err != nil {
		return "", trackedHostPathProjection{}, err
	}
	actualType, ok := modeToHostPathType(info.Mode())
	if !ok {
		return "", trackedHostPathProjection{}, fmt.Errorf("unsupported file type at %q", path)
	}
	if expectedType != "" && actualType != expectedType {
		projection := trackedHostPathProjection{
			Type: actualType,
			Mode: info.Mode() & os.ModePerm,
		}
		return digestHostPathProjection(projection), projection, nil
	}

	projection := trackedHostPathProjection{
		Type: actualType,
		Mode: info.Mode() & os.ModePerm,
	}
	switch actualType {
	case hydrate.HostPathRegularFile:
		data, err := os.ReadFile(path)
		if err != nil {
			return "", trackedHostPathProjection{}, err
		}
		projection.Digest = digest.Canonical.FromBytes(data).String()
	case hydrate.HostPathSymlink:
		target, err := os.Readlink(path)
		if err != nil {
			return "", trackedHostPathProjection{}, err
		}
		projection.Target = target
	default:
		return "", trackedHostPathProjection{}, fmt.Errorf("unsupported host path type %q", actualType)
	}
	return digestHostPathProjection(projection), projection, nil
}

func modeToHostPathType(mode os.FileMode) (hydrate.HostPathType, bool) {
	switch {
	case mode&os.ModeSymlink != 0:
		return hydrate.HostPathSymlink, true
	case mode.IsRegular():
		return hydrate.HostPathRegularFile, true
	default:
		return "", false
	}
}

func digestHostPathProjection(projection trackedHostPathProjection) string {
	payload := map[string]interface{}{
		"type": projection.Type,
		"mode": projection.Mode.Perm().String(),
	}
	switch projection.Type {
	case hydrate.HostPathRegularFile:
		payload["contentDigest"] = projection.Digest
	case hydrate.HostPathSymlink:
		payload["target"] = projection.Target
	}
	data, _ := json.Marshal(payload)
	return digest.Canonical.FromBytes(data).String()
}

func diffTrackedHostPath(desired, live trackedHostPathProjection, driftState state.ClusterState) []HostPathMismatch {
	mismatches := make([]HostPathMismatch, 0)
	if desired.Type != live.Type {
		return []HostPathMismatch{{
			Reason:  "typeMismatch",
			Desired: string(desired.Type),
			Live:    string(live.Type),
			State:   driftState,
		}}
	}
	if desired.Mode.Perm() != live.Mode.Perm() {
		mismatches = append(mismatches, HostPathMismatch{
			Reason:  "modeMismatch",
			Desired: desired.Mode.Perm().String(),
			Live:    live.Mode.Perm().String(),
			State:   driftState,
		})
	}
	switch desired.Type {
	case hydrate.HostPathRegularFile:
		if desired.Digest != live.Digest {
			mismatches = append(mismatches, HostPathMismatch{
				Reason:  "contentMismatch",
				Desired: desired.Digest,
				Live:    live.Digest,
				State:   driftState,
			})
		}
	case hydrate.HostPathSymlink:
		if desired.Target != live.Target {
			mismatches = append(mismatches, HostPathMismatch{
				Reason:  "targetMismatch",
				Desired: desired.Target,
				Live:    live.Target,
				State:   driftState,
			})
		}
	}
	return mismatches
}

func resolveLiveHostPath(hostRoot, trackedHostPath string) (string, error) {
	if strings.TrimSpace(trackedHostPath) == "" {
		return "", fmt.Errorf("tracked host path cannot be empty")
	}
	if !filepath.IsAbs(trackedHostPath) {
		return "", fmt.Errorf("tracked host path %q must be absolute", trackedHostPath)
	}
	if strings.TrimSpace(hostRoot) == "" {
		hostRoot = string(os.PathSeparator)
	}
	cleanRoot := filepath.Clean(hostRoot)
	relative := strings.TrimPrefix(filepath.Clean(trackedHostPath), string(os.PathSeparator))
	resolved := filepath.Join(cleanRoot, relative)
	rel, err := filepath.Rel(cleanRoot, resolved)
	if err != nil {
		return "", err
	}
	if rel == ".." || strings.HasPrefix(rel, ".."+string(os.PathSeparator)) {
		return "", fmt.Errorf("tracked host path %q escapes host root %q", trackedHostPath, hostRoot)
	}
	return resolved, nil
}

func findTrackedObjectInFile(path string, tracked hydrate.TrackedK8sObject) ([]byte, bool, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, false, fmt.Errorf("read tracked object file %q: %w", path, err)
	}

	decoder := utilyaml.NewYAMLOrJSONDecoder(bytes.NewReader(data), 4096)
	for {
		var raw runtime.RawExtension
		if err := decoder.Decode(&raw); err != nil {
			if err == io.EOF {
				break
			}
			return nil, false, fmt.Errorf("decode tracked object file %q: %w", path, err)
		}
		if len(bytes.TrimSpace(raw.Raw)) == 0 {
			continue
		}

		meta, err := objectIdentity(raw.Raw)
		if err != nil {
			return nil, false, err
		}
		if meta.APIVersion == tracked.APIVersion &&
			meta.Kind == tracked.Kind &&
			meta.Metadata.Name == tracked.Name &&
			meta.Metadata.Namespace == tracked.Namespace {
			return raw.Raw, true, nil
		}
	}
	return nil, false, nil
}

func objectIdentity(raw []byte) (struct {
	APIVersion string `json:"apiVersion" yaml:"apiVersion"`
	Kind       string `json:"kind" yaml:"kind"`
	Metadata   struct {
		Name      string `json:"name" yaml:"name"`
		Namespace string `json:"namespace,omitempty" yaml:"namespace,omitempty"`
	} `json:"metadata" yaml:"metadata"`
}, error) {
	var meta struct {
		APIVersion string `json:"apiVersion" yaml:"apiVersion"`
		Kind       string `json:"kind" yaml:"kind"`
		Metadata   struct {
			Name      string `json:"name" yaml:"name"`
			Namespace string `json:"namespace,omitempty" yaml:"namespace,omitempty"`
		} `json:"metadata" yaml:"metadata"`
	}
	if err := yaml.Unmarshal(raw, &meta); err != nil {
		return meta, fmt.Errorf("unmarshal tracked object identity: %w", err)
	}
	return meta, nil
}

func normalizedObjectDigest(raw []byte) (string, error) {
	var object map[string]interface{}
	if err := yaml.Unmarshal(raw, &object); err != nil {
		return "", fmt.Errorf("unmarshal object: %w", err)
	}
	if len(object) == 0 {
		return "", fmt.Errorf("object cannot be empty")
	}

	normalizeObject(object)
	data, err := json.Marshal(object)
	if err != nil {
		return "", fmt.Errorf("marshal normalized object: %w", err)
	}
	return digest.Canonical.FromBytes(data).String(), nil
}

func matchesOwnedFields(desiredRaw, liveRaw []byte) (bool, error) {
	mismatches, err := ownedFieldMismatches(desiredRaw, liveRaw)
	if err != nil {
		return false, err
	}
	return len(mismatches) == 0, nil
}

func ownedFieldMismatches(desiredRaw, liveRaw []byte) ([]FieldMismatch, error) {
	desired, err := normalizedObjectValue(desiredRaw)
	if err != nil {
		return nil, err
	}
	live, err := normalizedObjectValue(liveRaw)
	if err != nil {
		return nil, err
	}
	return diffOwnedFieldsAtPath(nil, desired, live), nil
}

func normalizedObjectValue(raw []byte) (interface{}, error) {
	var object map[string]interface{}
	if err := yaml.Unmarshal(raw, &object); err != nil {
		return nil, fmt.Errorf("unmarshal object: %w", err)
	}
	if len(object) == 0 {
		return nil, fmt.Errorf("object cannot be empty")
	}
	normalizeObject(object)
	return object, nil
}

func diffOwnedFieldsAtPath(path []string, desired, live interface{}) []FieldMismatch {
	switch desiredTyped := desired.(type) {
	case map[string]interface{}:
		liveTyped, ok := live.(map[string]interface{})
		if !ok {
			return []FieldMismatch{newMismatch(path, "typeMismatch", desired, live)}
		}
		mismatches := make([]FieldMismatch, 0)
		for key, desiredValue := range desiredTyped {
			liveValue, ok := liveTyped[key]
			if !ok {
				mismatches = append(mismatches, newMismatch(appendPath(path, key), "missingField", desiredValue, nil))
				continue
			}
			mismatches = append(mismatches, diffOwnedFieldsAtPath(appendPath(path, key), desiredValue, liveValue)...)
		}
		return mismatches
	case []interface{}:
		liveTyped, ok := live.([]interface{})
		if !ok {
			return []FieldMismatch{newMismatch(path, "typeMismatch", desired, live)}
		}
		if mismatches, handled := compareMergeKeyList(path, desiredTyped, liveTyped); handled {
			return mismatches
		}
		mismatches := make([]FieldMismatch, 0)
		if len(desiredTyped) != len(liveTyped) {
			mismatches = append(mismatches, FieldMismatch{
				Path:    pathString(path),
				Reason:  "listLengthMismatch",
				Desired: formatMismatchValue(len(desiredTyped)),
				Live:    formatMismatchValue(len(liveTyped)),
			})
		}
		limit := len(desiredTyped)
		if len(liveTyped) < limit {
			limit = len(liveTyped)
		}
		for i := 0; i < limit; i++ {
			mismatches = append(mismatches, diffOwnedFieldsAtPath(indexedPath(path, i), desiredTyped[i], liveTyped[i])...)
		}
		return mismatches
	default:
		if reflect.DeepEqual(desired, live) {
			return nil
		}
		return []FieldMismatch{newMismatch(path, "valueMismatch", desired, live)}
	}
}

type mergeKey struct {
	key      string
	selector string
}

type mergeKeyMatcher func(item map[string]interface{}) (mergeKey, bool)

func compareMergeKeyList(path []string, desired, live []interface{}) ([]FieldMismatch, bool) {
	matcher := mergeKeyMatcherForPath(path)
	if matcher == nil {
		return nil, false
	}

	liveIndex := make(map[string]interface{}, len(live))
	for _, item := range live {
		liveMap, ok := item.(map[string]interface{})
		if !ok {
			return nil, false
		}
		key, ok := matcher(liveMap)
		if !ok {
			return nil, false
		}
		if _, exists := liveIndex[key.key]; exists {
			return []FieldMismatch{newMismatch(path, "duplicateMergeKey", key.selector, key.selector)}, true
		}
		liveIndex[key.key] = liveMap
	}

	mismatches := make([]FieldMismatch, 0)
	for _, item := range desired {
		desiredMap, ok := item.(map[string]interface{})
		if !ok {
			return nil, false
		}
		key, ok := matcher(desiredMap)
		if !ok {
			return nil, false
		}
		liveItem, ok := liveIndex[key.key]
		if !ok {
			mismatches = append(mismatches, FieldMismatch{
				Path:   pathString(selectorPath(path, key.selector)),
				Reason: "missingListItem",
			})
			continue
		}
		mismatches = append(mismatches, diffOwnedFieldsAtPath(selectorPath(path, key.selector), desiredMap, liveItem)...)
	}
	return mismatches, true
}

func mergeKeyMatcherForPath(path []string) mergeKeyMatcher {
	if len(path) == 0 {
		return nil
	}

	switch path[len(path)-1] {
	case "containers", "initContainers", "ephemeralContainers", "env", "volumes":
		return fieldKeyMatcher("name")
	case "volumeMounts":
		return fieldKeyMatcher("mountPath")
	case "ports":
		return portKeyMatcher
	default:
		return nil
	}
}

func fieldKeyMatcher(field string) mergeKeyMatcher {
	return func(item map[string]interface{}) (mergeKey, bool) {
		value, ok := item[field]
		if !ok {
			return mergeKey{}, false
		}
		text := stringifyMergeKey(value)
		return mergeKey{
			key:      field + ":" + text,
			selector: field + "=" + text,
		}, true
	}
}

func portKeyMatcher(item map[string]interface{}) (mergeKey, bool) {
	if value, ok := item["containerPort"]; ok {
		key := stringifyMergeKey(value)
		if protocol, ok := item["protocol"]; ok {
			key += "/" + stringifyMergeKey(protocol)
		}
		return mergeKey{
			key:      "containerPort:" + key,
			selector: "containerPort=" + key,
		}, true
	}
	if value, ok := item["port"]; ok {
		key := stringifyMergeKey(value)
		if protocol, ok := item["protocol"]; ok {
			key += "/" + stringifyMergeKey(protocol)
		}
		return mergeKey{
			key:      "port:" + key,
			selector: "port=" + key,
		}, true
	}
	if value, ok := item["name"]; ok {
		text := stringifyMergeKey(value)
		return mergeKey{
			key:      "name:" + text,
			selector: "name=" + text,
		}, true
	}
	return mergeKey{}, false
}

func stringifyMergeKey(value interface{}) string {
	switch typed := value.(type) {
	case string:
		return typed
	case int:
		return fmt.Sprintf("%d", typed)
	case int32:
		return fmt.Sprintf("%d", typed)
	case int64:
		return fmt.Sprintf("%d", typed)
	case float64:
		return fmt.Sprintf("%.0f", typed)
	default:
		return fmt.Sprintf("%v", typed)
	}
}

func appendPath(path []string, segment string) []string {
	next := append([]string(nil), path...)
	return append(next, segment)
}

func indexedPath(path []string, index int) []string {
	if len(path) == 0 {
		return []string{fmt.Sprintf("[%d]", index)}
	}
	next := append([]string(nil), path...)
	next[len(next)-1] = next[len(next)-1] + fmt.Sprintf("[%d]", index)
	return next
}

func selectorPath(path []string, selector string) []string {
	if len(path) == 0 {
		return []string{"[" + selector + "]"}
	}
	next := append([]string(nil), path...)
	next[len(next)-1] = next[len(next)-1] + "[" + selector + "]"
	return next
}

func pathString(path []string) string {
	return strings.Join(path, ".")
}

func newMismatch(path []string, reason string, desired, live interface{}) FieldMismatch {
	return FieldMismatch{
		Path:    pathString(path),
		Reason:  reason,
		Desired: formatMismatchValue(desired),
		Live:    formatMismatchValue(live),
	}
}

func formatMismatchValue(value interface{}) string {
	if value == nil {
		return ""
	}
	data, err := json.Marshal(value)
	if err != nil {
		return fmt.Sprintf("%v", value)
	}
	return string(data)
}

func mergeDesiredValue(path []string, base, overlay interface{}, ownership hydrate.InventoryOwnership, index pathOwnershipIndex) interface{} {
	index[pathString(path)] = ownership

	switch overlayTyped := overlay.(type) {
	case map[string]interface{}:
		result := make(map[string]interface{}, len(overlayTyped))
		if baseTyped, ok := base.(map[string]interface{}); ok {
			for key, value := range baseTyped {
				result[key] = cloneValue(value)
			}
		}
		for key, value := range overlayTyped {
			result[key] = mergeDesiredValue(appendPath(path, key), result[key], value, ownership, index)
		}
		return result
	case []interface{}:
		if baseTyped, ok := base.([]interface{}); ok {
			if merged, handled := mergeDesiredList(path, baseTyped, overlayTyped, ownership, index); handled {
				return merged
			}
		}
		result := make([]interface{}, len(overlayTyped))
		for i, value := range overlayTyped {
			result[i] = mergeDesiredValue(indexedPath(path, i), nil, value, ownership, index)
		}
		return result
	default:
		return overlayTyped
	}
}

func mergeDesiredList(path []string, base, overlay []interface{}, ownership hydrate.InventoryOwnership, index pathOwnershipIndex) ([]interface{}, bool) {
	matcher := mergeKeyMatcherForPath(path)
	if matcher == nil {
		return nil, false
	}

	result := make([]interface{}, len(base))
	basePositions := make(map[string]int, len(base))
	for i, item := range base {
		itemMap, ok := item.(map[string]interface{})
		if !ok {
			return nil, false
		}
		key, ok := matcher(itemMap)
		if !ok {
			return nil, false
		}
		result[i] = cloneValue(item)
		basePositions[key.key] = i
	}

	for _, item := range overlay {
		itemMap, ok := item.(map[string]interface{})
		if !ok {
			return nil, false
		}
		key, ok := matcher(itemMap)
		if !ok {
			return nil, false
		}
		itemPath := selectorPath(path, key.selector)
		if position, exists := basePositions[key.key]; exists {
			result[position] = mergeDesiredValue(itemPath, result[position], itemMap, ownership, index)
			continue
		}
		result = append(result, mergeDesiredValue(itemPath, nil, itemMap, ownership, index))
		basePositions[key.key] = len(result) - 1
	}
	return result, true
}

func cloneValue(value interface{}) interface{} {
	switch typed := value.(type) {
	case map[string]interface{}:
		cloned := make(map[string]interface{}, len(typed))
		for key, item := range typed {
			cloned[key] = cloneValue(item)
		}
		return cloned
	case []interface{}:
		cloned := make([]interface{}, len(typed))
		for i, item := range typed {
			cloned[i] = cloneValue(item)
		}
		return cloned
	default:
		return typed
	}
}

func normalizeObject(object map[string]interface{}) {
	delete(object, "status")

	if kind, ok := object["kind"].(string); ok && kind == "Secret" {
		normalizeSecret(object)
	}

	metadata, ok := object["metadata"].(map[string]interface{})
	if !ok {
		return
	}

	for _, key := range []string{
		"managedFields",
		"resourceVersion",
		"uid",
		"creationTimestamp",
		"generation",
		"selfLink",
	} {
		delete(metadata, key)
	}

	annotations, ok := metadata["annotations"].(map[string]interface{})
	if ok {
		for _, key := range []string{
			"kubectl.kubernetes.io/last-applied-configuration",
			"deployment.kubernetes.io/revision",
			"deprecated.daemonset.template.generation",
		} {
			delete(annotations, key)
		}
		if len(annotations) == 0 {
			delete(metadata, "annotations")
		}
	}
}

func normalizeSecret(object map[string]interface{}) {
	if _, ok := object["type"]; !ok {
		object["type"] = "Opaque"
	}

	stringData, ok := object["stringData"].(map[string]interface{})
	if !ok || len(stringData) == 0 {
		delete(object, "stringData")
		return
	}

	data, ok := object["data"].(map[string]interface{})
	if !ok {
		data = make(map[string]interface{}, len(stringData))
		object["data"] = data
	}
	for key, value := range stringData {
		text, ok := value.(string)
		if !ok {
			continue
		}
		data[key] = base64.StdEncoding.EncodeToString([]byte(text))
	}
	delete(object, "stringData")
}

func resolveBundlePath(bundleRoot, bundleRel string) (string, error) {
	if strings.TrimSpace(bundleRel) == "" {
		return "", fmt.Errorf("bundle path cannot be empty")
	}
	if filepath.IsAbs(bundleRel) {
		return "", fmt.Errorf("bundle path %q must be relative", bundleRel)
	}

	resolved := filepath.Join(bundleRoot, filepath.FromSlash(bundleRel))
	relative, err := filepath.Rel(bundleRoot, resolved)
	if err != nil {
		return "", err
	}
	if relative == ".." || strings.HasPrefix(relative, ".."+string(os.PathSeparator)) {
		return "", fmt.Errorf("bundle path %q escapes bundle root", bundleRel)
	}
	return resolved, nil
}

type notFoundError struct {
	message string
}

func (e notFoundError) Error() string {
	return e.message
}

func NewNotFoundError(apiVersion, kind, namespace, name string) error {
	key := strings.TrimSpace(namespace)
	if key == "" {
		key = "<cluster>"
	}
	return notFoundError{
		message: fmt.Sprintf("%s %s/%s in namespace %s not found", apiVersion, kind, name, key),
	}
}

func isNotFound(err error) bool {
	if err == nil {
		return false
	}
	_, ok := err.(notFoundError)
	return ok
}

type kubectlResolver struct {
	lookup func(args ...string) ([]byte, error)
}

func NewKubectlResolver(lookup func(args ...string) ([]byte, error)) ObjectResolver {
	return kubectlResolver{lookup: lookup}
}

func (r kubectlResolver) Get(apiVersion, kind, namespace, name string) ([]byte, error) {
	if r.lookup == nil {
		return nil, fmt.Errorf("kubectl lookup cannot be nil")
	}

	args := []string{"get", kind, name}
	if strings.TrimSpace(namespace) != "" {
		args = append(args, "-n", namespace)
	}
	args = append(args, "-o", "json")

	data, err := r.lookup(args...)
	if err != nil {
		message := strings.ToLower(err.Error())
		if strings.Contains(message, "notfound") || strings.Contains(message, "not found") {
			return nil, NewNotFoundError(apiVersion, kind, namespace, name)
		}
		return nil, err
	}

	if !json.Valid(data) {
		return nil, fmt.Errorf("kubectl returned invalid JSON for %s %s/%s", kind, namespace, name)
	}
	return data, nil
}
