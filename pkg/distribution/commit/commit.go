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

package commit

import (
	"bytes"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"k8s.io/apimachinery/pkg/runtime"
	utilyaml "k8s.io/apimachinery/pkg/util/yaml"
	"sigs.k8s.io/yaml"

	"github.com/labring/sealos/pkg/distribution/compare"
	"github.com/labring/sealos/pkg/distribution/hydrate"
	"github.com/labring/sealos/pkg/distribution/localrepo"
	"github.com/labring/sealos/pkg/distribution/ownership"
	"github.com/labring/sealos/pkg/distribution/state"
)

type Options struct {
	Bundle        *hydrate.Bundle
	BundleRoot    string
	LocalRepo     *localrepo.Repo
	CompareResult *compare.Result
	Resolver      compare.ObjectResolver
	HostRoot      string
	SelectedHost  string
}

type Result struct {
	CommittedObjects   []CommittedObject   `json:"committedObjects" yaml:"committedObjects"`
	CommittedHostPaths []CommittedHostPath `json:"committedHostPaths,omitempty" yaml:"committedHostPaths,omitempty"`
	DesiredStateDigest string              `json:"desiredStateDigest" yaml:"desiredStateDigest"`
	LocalRepoRevision  string              `json:"localRepoRevision" yaml:"localRepoRevision"`
}

type CommittedObject struct {
	APIVersion string `json:"apiVersion" yaml:"apiVersion"`
	Kind       string `json:"kind" yaml:"kind"`
	Namespace  string `json:"namespace,omitempty" yaml:"namespace,omitempty"`
	Name       string `json:"name" yaml:"name"`
	RepoPath   string `json:"repoPath" yaml:"repoPath"`
	BundlePath string `json:"bundlePath" yaml:"bundlePath"`
}

type CommittedHostPath struct {
	Path       string `json:"path" yaml:"path"`
	Host       string `json:"host,omitempty" yaml:"host,omitempty"`
	Component  string `json:"component,omitempty" yaml:"component,omitempty"`
	InputName  string `json:"inputName,omitempty" yaml:"inputName,omitempty"`
	RepoPath   string `json:"repoPath" yaml:"repoPath"`
	BundlePath string `json:"bundlePath" yaml:"bundlePath"`
}

type manifestIdentity struct {
	APIVersion string
	Kind       string
	Namespace  string
	Name       string
}

type patchUpdate struct {
	repoPath   string
	bundlePath string
	identity   manifestIdentity
	overlay    map[string]interface{}
	object     CommittedObject
}

func LocalPatches(opts Options) (*Result, error) {
	if opts.Bundle == nil {
		return nil, fmt.Errorf("bundle cannot be nil")
	}
	if strings.TrimSpace(opts.BundleRoot) == "" {
		return nil, fmt.Errorf("bundle root cannot be empty")
	}
	if opts.LocalRepo == nil {
		return nil, fmt.Errorf("local repo cannot be nil")
	}
	if opts.CompareResult == nil {
		return nil, fmt.Errorf("compare result cannot be nil")
	}
	if opts.Resolver == nil {
		return nil, fmt.Errorf("object resolver cannot be nil")
	}
	policyDoc, err := hydrate.LoadBundleLocalPatchPolicy(opts.Bundle, opts.BundleRoot)
	if err != nil {
		return nil, err
	}

	objectUpdates, hostPathUpdates, err := planCommitUpdates(opts.Bundle, opts.CompareResult, opts.BundleRoot, opts.LocalRepo, opts.Resolver, opts.HostRoot, opts.SelectedHost, policyDoc.Spec)
	if err != nil {
		return nil, err
	}
	for _, update := range objectUpdates {
		if err := upsertPatchDocument(update.repoPath, update.identity, update.overlay); err != nil {
			return nil, err
		}
		if err := upsertPatchDocument(update.bundlePath, update.identity, update.overlay); err != nil {
			return nil, err
		}
	}
	for _, update := range hostPathUpdates {
		if err := writeCommittedHostPath(update.repoPath, update.content); err != nil {
			return nil, err
		}
		if err := writeCommittedHostPath(update.bundlePath, update.content); err != nil {
			return nil, err
		}
	}

	if len(objectUpdates) > 0 {
		if err := hydrate.ReapplyRenderedLocalPatches(opts.Bundle, opts.BundleRoot); err != nil {
			return nil, err
		}
	}

	reloadedRepo, err := localrepo.Load(opts.LocalRepo.Root)
	if err != nil {
		return nil, err
	}
	digest, err := hydrate.DigestBundle(opts.BundleRoot)
	if err != nil {
		return nil, fmt.Errorf("digest updated bundle %q: %w", opts.BundleRoot, err)
	}

	committed := make([]CommittedObject, 0, len(objectUpdates))
	for _, update := range objectUpdates {
		committed = append(committed, update.object)
	}
	sort.Slice(committed, func(i, j int) bool {
		if committed[i].Kind != committed[j].Kind {
			return committed[i].Kind < committed[j].Kind
		}
		if committed[i].Namespace != committed[j].Namespace {
			return committed[i].Namespace < committed[j].Namespace
		}
		return committed[i].Name < committed[j].Name
	})
	committedHostPaths := make([]CommittedHostPath, 0, len(hostPathUpdates))
	for _, update := range hostPathUpdates {
		committedHostPaths = append(committedHostPaths, update.object)
	}
	sort.Slice(committedHostPaths, func(i, j int) bool {
		return committedHostPaths[i].Path < committedHostPaths[j].Path
	})

	return &Result{
		CommittedObjects:   committed,
		CommittedHostPaths: committedHostPaths,
		DesiredStateDigest: digest.String(),
		LocalRepoRevision:  reloadedRepo.Revision,
	}, nil
}

type hostPathUpdate struct {
	repoPath   string
	bundlePath string
	content    []byte
	object     CommittedHostPath
}

type hostPathCommitKey struct {
	path      string
	component string
	inputName string
}

func planCommitUpdates(bundle *hydrate.Bundle, result *compare.Result, bundleRoot string, repo *localrepo.Repo, resolver compare.ObjectResolver, hostRoot, selectedHost string, policy ownership.LocalPatchPolicy) ([]patchUpdate, []hostPathUpdate, error) {
	if bundle == nil {
		return nil, nil, fmt.Errorf("bundle cannot be nil")
	}
	if repo == nil {
		return nil, nil, fmt.Errorf("local repo cannot be nil")
	}
	repoRoot := repo.Root
	orphanObjects := make([]string, 0)
	objectUpdates := make([]patchUpdate, 0)
	hostPathUpdates := make([]hostPathUpdate, 0)
	groupedHostPaths := groupDirtyLocalInputHostPaths(result.HostPaths)
	processedHostPathKeys := make(map[hostPathCommitKey]struct{}, len(groupedHostPaths))
	for _, object := range result.Objects {
		if object.State == state.StateOrphan {
			orphanObjects = append(orphanObjects, objectIdentityString(object.Tracked.APIVersion, object.Tracked.Kind, object.Tracked.Namespace, object.Tracked.Name))
			continue
		}
		if object.State != state.StateDirty {
			continue
		}

		update, err := planObjectCommitUpdate(object, bundleRoot, repoRoot, resolver, policy)
		if err != nil {
			return nil, nil, err
		}
		objectUpdates = append(objectUpdates, update)
	}
	for _, hostPath := range result.HostPaths {
		if hostPath.State == state.StateOrphan {
			orphanObjects = append(orphanObjects, "hostPath "+hostPath.Tracked.HostPath)
			continue
		}
		if hostPath.State != state.StateDirty {
			continue
		}
		key := hostPathCommitKey{
			path:      hostPath.Tracked.HostPath,
			component: hostPath.Tracked.Component,
			inputName: hostPath.Tracked.InputName,
		}
		if _, ok := processedHostPathKeys[key]; ok {
			continue
		}

		update, err := planHostPathCommitUpdate(bundle, hostPath, groupedHostPaths, bundleRoot, repo, hostRoot, selectedHost)
		if err != nil {
			return nil, nil, err
		}
		if update.repoPath == "" && update.bundlePath == "" && update.object.Path == "" {
			continue
		}
		processedHostPathKeys[key] = struct{}{}
		hostPathUpdates = append(hostPathUpdates, update)
	}
	if len(orphanObjects) > 0 {
		sort.Strings(orphanObjects)
		return nil, nil, fmt.Errorf("cannot commit global-owned drift for %s", strings.Join(orphanObjects, ", "))
	}
	return objectUpdates, hostPathUpdates, nil
}

func groupDirtyLocalInputHostPaths(statuses []compare.HostPathStatus) map[hostPathCommitKey][]compare.HostPathStatus {
	grouped := make(map[hostPathCommitKey][]compare.HostPathStatus)
	for _, status := range statuses {
		if status.State != state.StateDirty {
			continue
		}
		if status.Tracked.Source != hydrate.InventorySourceLocalInput || status.Tracked.Ownership != hydrate.InventoryOwnershipLocal {
			continue
		}
		key := hostPathCommitKey{
			path:      status.Tracked.HostPath,
			component: status.Tracked.Component,
			inputName: status.Tracked.InputName,
		}
		grouped[key] = append(grouped[key], status)
	}
	return grouped
}

func planObjectCommitUpdate(object compare.ObjectStatus, bundleRoot, repoRoot string, resolver compare.ObjectResolver, policy ownership.LocalPatchPolicy) (patchUpdate, error) {
	if object.Presence != compare.ObjectPresencePresent {
		return patchUpdate{}, fmt.Errorf("cannot commit non-present object %s", objectIdentityString(object.Tracked.APIVersion, object.Tracked.Kind, object.Tracked.Namespace, object.Tracked.Name))
	}

	liveRaw, err := resolver.Get(object.Tracked.APIVersion, object.Tracked.Kind, object.Tracked.Namespace, object.Tracked.Name)
	if err != nil {
		return patchUpdate{}, err
	}
	var liveObject map[string]interface{}
	if err := yaml.Unmarshal(liveRaw, &liveObject); err != nil {
		return patchUpdate{}, fmt.Errorf("unmarshal live object %s: %w", objectIdentityString(object.Tracked.APIVersion, object.Tracked.Kind, object.Tracked.Namespace, object.Tracked.Name), err)
	}

	identity := manifestIdentity{
		APIVersion: object.Tracked.APIVersion,
		Kind:       object.Tracked.Kind,
		Namespace:  object.Tracked.Namespace,
		Name:       object.Tracked.Name,
	}

	if hasLocalPatchFragment(object) {
		repoPatchPath, bundlePatchPath, err := resolveLocalPatchPaths(object, bundleRoot, repoRoot)
		if err != nil {
			return patchUpdate{}, err
		}
		overlay, patchErr := ownership.ExtractAllowedLocalPatchOverlayWithPolicy(policy, liveObject)
		if patchErr != nil {
			return patchUpdate{}, fmt.Errorf("extract local patch overlay for %s: %w", objectIdentityString(object.Tracked.APIVersion, object.Tracked.Kind, object.Tracked.Namespace, object.Tracked.Name), patchErr)
		}
		ensurePatchIdentity(overlay, identity)

		return patchUpdate{
			repoPath:   repoPatchPath,
			bundlePath: bundlePatchPath,
			identity:   identity,
			overlay:    overlay,
			object: CommittedObject{
				APIVersion: object.Tracked.APIVersion,
				Kind:       object.Tracked.Kind,
				Namespace:  object.Tracked.Namespace,
				Name:       object.Tracked.Name,
				RepoPath:   repoPatchPath,
				BundlePath: bundlePatchPath,
			},
		}, nil
	}

	repoResourcePath, bundleResourcePath, err := resolveLocalResourcePaths(object, bundleRoot, repoRoot)
	if err != nil {
		return patchUpdate{}, err
	}
	resourceDoc := sanitizeLocalResourceDocument(liveObject)
	ensurePatchIdentity(resourceDoc, identity)

	return patchUpdate{
		repoPath:   repoResourcePath,
		bundlePath: bundleResourcePath,
		identity:   identity,
		overlay:    resourceDoc,
		object: CommittedObject{
			APIVersion: object.Tracked.APIVersion,
			Kind:       object.Tracked.Kind,
			Namespace:  object.Tracked.Namespace,
			Name:       object.Tracked.Name,
			RepoPath:   repoResourcePath,
			BundlePath: bundleResourcePath,
		},
	}, nil
}

func hasLocalPatchFragment(object compare.ObjectStatus) bool {
	for _, fragment := range object.Fragments {
		if fragment.Source == hydrate.InventorySourceLocalPatch {
			return true
		}
	}
	return false
}

func resolveLocalPatchPaths(object compare.ObjectStatus, bundleRoot, repoRoot string) (string, string, error) {
	paths := make(map[string]hydrate.TrackedK8sObject)
	for _, fragment := range object.Fragments {
		if fragment.Source != hydrate.InventorySourceLocalPatch {
			continue
		}
		paths[fragment.Path] = fragment
	}
	if len(paths) == 0 {
		return "", "", fmt.Errorf("dirty object %s is not backed by a local patch fragment", objectIdentityString(object.Tracked.APIVersion, object.Tracked.Kind, object.Tracked.Namespace, object.Tracked.Name))
	}
	if len(paths) > 1 {
		keys := make([]string, 0, len(paths))
		for key := range paths {
			keys = append(keys, key)
		}
		sort.Strings(keys)
		return "", "", fmt.Errorf("dirty object %s has multiple local patch fragments: %s", objectIdentityString(object.Tracked.APIVersion, object.Tracked.Kind, object.Tracked.Namespace, object.Tracked.Name), strings.Join(keys, ", "))
	}

	var fragment hydrate.TrackedK8sObject
	for _, value := range paths {
		fragment = value
	}
	if strings.TrimSpace(fragment.Component) == "" {
		return "", "", fmt.Errorf("dirty object %s local patch fragment is missing component name", objectIdentityString(object.Tracked.APIVersion, object.Tracked.Kind, object.Tracked.Namespace, object.Tracked.Name))
	}

	prefix := filepath.ToSlash(filepath.Join("components", fragment.Component, "local-patches")) + "/"
	if !strings.HasPrefix(fragment.Path, prefix) {
		return "", "", fmt.Errorf("dirty object %s local patch fragment %q does not live under %q", objectIdentityString(object.Tracked.APIVersion, object.Tracked.Kind, object.Tracked.Namespace, object.Tracked.Name), fragment.Path, prefix)
	}
	relativePath := strings.TrimPrefix(fragment.Path, prefix)
	if strings.TrimSpace(relativePath) == "" {
		return "", "", fmt.Errorf("dirty object %s local patch fragment %q has an empty relative path", objectIdentityString(object.Tracked.APIVersion, object.Tracked.Kind, object.Tracked.Namespace, object.Tracked.Name), fragment.Path)
	}

	bundlePatchPath, err := resolvePathUnderRoot(bundleRoot, fragment.Path)
	if err != nil {
		return "", "", err
	}
	repoPatchPath, err := resolvePathUnderRoot(repoRoot, filepath.ToSlash(filepath.Join(localrepo.PatchesDirName, fragment.Component, relativePath)))
	if err != nil {
		return "", "", err
	}
	return repoPatchPath, bundlePatchPath, nil
}

func resolveLocalResourcePaths(object compare.ObjectStatus, bundleRoot, repoRoot string) (string, string, error) {
	localFragments := make([]hydrate.TrackedK8sObject, 0, len(object.Fragments))
	for _, fragment := range object.Fragments {
		if fragment.Source == hydrate.InventorySourceLocalResource {
			localFragments = append(localFragments, fragment)
		}
	}
	if len(localFragments) == 0 {
		return "", "", fmt.Errorf("dirty object %s is not backed by a local resource fragment", objectIdentityString(object.Tracked.APIVersion, object.Tracked.Kind, object.Tracked.Namespace, object.Tracked.Name))
	}
	if len(localFragments) != len(object.Fragments) || len(localFragments) != 1 {
		return "", "", fmt.Errorf("dirty object %s is not a standalone local resource object", objectIdentityString(object.Tracked.APIVersion, object.Tracked.Kind, object.Tracked.Namespace, object.Tracked.Name))
	}

	fragment := localFragments[0]
	prefix := "local-resources/"
	if !strings.HasPrefix(fragment.Path, prefix) {
		return "", "", fmt.Errorf("dirty object %s local resource fragment %q does not live under %q", objectIdentityString(object.Tracked.APIVersion, object.Tracked.Kind, object.Tracked.Namespace, object.Tracked.Name), fragment.Path, prefix)
	}
	relativePath := strings.TrimPrefix(fragment.Path, prefix)
	if strings.TrimSpace(relativePath) == "" {
		return "", "", fmt.Errorf("dirty object %s local resource fragment %q has an empty relative path", objectIdentityString(object.Tracked.APIVersion, object.Tracked.Kind, object.Tracked.Namespace, object.Tracked.Name), fragment.Path)
	}

	bundleResourcePath, err := resolvePathUnderRoot(bundleRoot, fragment.Path)
	if err != nil {
		return "", "", err
	}
	repoResourcePath, err := resolvePathUnderRoot(repoRoot, filepath.ToSlash(filepath.Join(localrepo.ResourcesDirName, relativePath)))
	if err != nil {
		return "", "", err
	}
	return repoResourcePath, bundleResourcePath, nil
}

func resolvePathUnderRoot(root, relative string) (string, error) {
	if strings.TrimSpace(root) == "" {
		return "", fmt.Errorf("root cannot be empty")
	}
	if strings.TrimSpace(relative) == "" {
		return "", fmt.Errorf("relative path cannot be empty")
	}
	if filepath.IsAbs(relative) {
		return "", fmt.Errorf("path %q must be relative", relative)
	}
	resolved := filepath.Join(root, filepath.FromSlash(relative))
	rel, err := filepath.Rel(root, resolved)
	if err != nil {
		return "", err
	}
	if rel == ".." || strings.HasPrefix(rel, ".."+string(os.PathSeparator)) {
		return "", fmt.Errorf("path %q escapes root %q", relative, root)
	}
	return resolved, nil
}

func resolveExistingPathUnderRoot(root, absolute string) (string, error) {
	if strings.TrimSpace(root) == "" {
		return "", fmt.Errorf("root cannot be empty")
	}
	if strings.TrimSpace(absolute) == "" {
		return "", fmt.Errorf("path cannot be empty")
	}
	resolved, err := filepath.Abs(absolute)
	if err != nil {
		return "", err
	}
	rel, err := filepath.Rel(root, resolved)
	if err != nil {
		return "", err
	}
	if rel == ".." || strings.HasPrefix(rel, ".."+string(os.PathSeparator)) {
		return "", fmt.Errorf("path %q escapes root %q", absolute, root)
	}
	return resolved, nil
}

func planHostPathCommitUpdate(bundle *hydrate.Bundle, hostPath compare.HostPathStatus, grouped map[hostPathCommitKey][]compare.HostPathStatus, bundleRoot string, repo *localrepo.Repo, hostRoot, selectedHost string) (hostPathUpdate, error) {
	if hostPath.Presence != compare.ObjectPresencePresent {
		return hostPathUpdate{}, fmt.Errorf("cannot commit non-present host path %s", hostPath.Tracked.HostPath)
	}
	if hostPath.Tracked.Source != hydrate.InventorySourceLocalInput || hostPath.Tracked.Ownership != hydrate.InventoryOwnershipLocal {
		return hostPathUpdate{}, fmt.Errorf("dirty host path %s is not backed by a local input binding", hostPath.Tracked.HostPath)
	}
	if hostPath.Tracked.Type != hydrate.HostPathRegularFile {
		return hostPathUpdate{}, fmt.Errorf("dirty host path %s uses unsupported type %q for commit", hostPath.Tracked.HostPath, hostPath.Tracked.Type)
	}

	component := findRenderedComponent(bundle, hostPath.Tracked.Component)
	if component == nil {
		return hostPathUpdate{}, fmt.Errorf("dirty host path %s references missing component %q", hostPath.Tracked.HostPath, hostPath.Tracked.Component)
	}
	if strings.TrimSpace(hostPath.Tracked.InputName) == "" {
		return hostPathUpdate{}, fmt.Errorf("dirty host path %s is missing input name metadata", hostPath.Tracked.HostPath)
	}
	key := hostPathCommitKey{
		path:      hostPath.Tracked.HostPath,
		component: hostPath.Tracked.Component,
		inputName: hostPath.Tracked.InputName,
	}
	selected, skip, err := selectHostPathCommitCandidate(hostPath.Tracked.HostPath, grouped[key], selectedHost)
	if err != nil {
		return hostPathUpdate{}, err
	}
	if skip {
		return hostPathUpdate{}, nil
	}

	repoInputPath, bundleInputPath, err := resolveHostPathCommitTargets(component, selected, grouped[key], bundleRoot, repo, selectedHost)
	if err != nil {
		return hostPathUpdate{}, err
	}
	liveHostPath, err := resolveCommittedHostPath(hostRoot, selected.Tracked.HostPath)
	if err != nil {
		return hostPathUpdate{}, err
	}
	content, err := os.ReadFile(liveHostPath)
	if err != nil {
		return hostPathUpdate{}, fmt.Errorf("read live host path %s: %w", selected.Tracked.HostPath, err)
	}

	return hostPathUpdate{
		repoPath:   repoInputPath,
		bundlePath: bundleInputPath,
		content:    content,
		object: CommittedHostPath{
			Path:       selected.Tracked.HostPath,
			Host:       selected.Host,
			Component:  selected.Tracked.Component,
			InputName:  selected.Tracked.InputName,
			RepoPath:   repoInputPath,
			BundlePath: bundleInputPath,
		},
	}, nil
}

func resolveHostPathCommitTargets(component *hydrate.RenderedComponent, selected compare.HostPathStatus, candidates []compare.HostPathStatus, bundleRoot string, repo *localrepo.Repo, selectedHost string) (string, string, error) {
	if component == nil {
		return "", "", fmt.Errorf("component cannot be nil")
	}
	if repo == nil {
		return "", "", fmt.Errorf("local repo cannot be nil")
	}
	hostPath := selected.Tracked.HostPath
	inputName := selected.Tracked.InputName
	hasSelectedHost := strings.TrimSpace(selectedHost) != ""
	if hasSelectedHost {
		bundlePath, ok := hostInputBundlePathForHost(selected.Tracked.HostInputBindings, selected.Host)
		if ok {
			repoPath, ok := repo.HostBindingPath(component.Name, inputName, selected.Host)
			if !ok {
				return "", "", fmt.Errorf("dirty host path %s has host-scoped bundle input for host %q but local repo is missing inputs/%s/hosts/<host>/%s", hostPath, selected.Host, component.Name, inputName)
			}
			resolvedRepoPath, err := resolveExistingPathUnderRoot(repo.Root, repoPath)
			if err != nil {
				return "", "", err
			}
			resolvedBundlePath, err := resolvePathUnderRoot(bundleRoot, bundlePath)
			if err != nil {
				return "", "", err
			}
			return resolvedRepoPath, resolvedBundlePath, nil
		}
	}

	if hasSelectedHost && hasDivergentHostPathLiveContents(candidates) {
		return "", "", fmt.Errorf("dirty host path %s on selected host %q has no host-scoped input binding; create inputs/%s/hosts/<host>/%s before committing divergent host-specific content", hostPath, selected.Host, component.Name, inputName)
	}

	repoInputPath, ok := component.InputBindings[inputName]
	if !ok {
		return "", "", fmt.Errorf("dirty host path %s missing input binding for %q", hostPath, inputName)
	}
	repoInputPath, err := resolveExistingPathUnderRoot(repo.Root, repoInputPath)
	if err != nil {
		return "", "", err
	}
	bundleInputPath, err := resolvePathUnderRoot(bundleRoot, selected.Tracked.BundlePath)
	if err != nil {
		return "", "", err
	}
	return repoInputPath, bundleInputPath, nil
}

func hostInputBundlePathForHost(bindings map[string]string, host string) (string, bool) {
	trimmed := strings.TrimSpace(host)
	if trimmed == "" || len(bindings) == 0 {
		return "", false
	}
	if path := strings.TrimSpace(bindings[trimmed]); path != "" {
		return path, true
	}
	hostWithoutPort := hostWithoutPort(trimmed)
	if hostWithoutPort == "" || hostWithoutPort == trimmed {
		return "", false
	}
	path := strings.TrimSpace(bindings[hostWithoutPort])
	return path, path != ""
}

func hostWithoutPort(host string) string {
	trimmed := strings.TrimSpace(host)
	if trimmed == "" {
		return ""
	}
	if base, _, found := strings.Cut(trimmed, ":"); found {
		return strings.TrimSpace(base)
	}
	return trimmed
}

func hasDivergentHostPathLiveContents(candidates []compare.HostPathStatus) bool {
	if len(candidates) < 2 {
		return false
	}
	firstDigest := strings.TrimSpace(candidates[0].LiveDigest)
	for _, candidate := range candidates[1:] {
		if strings.TrimSpace(candidate.LiveDigest) != firstDigest {
			return true
		}
	}
	return false
}

func selectHostPathCommitCandidate(path string, candidates []compare.HostPathStatus, selectedHost string) (compare.HostPathStatus, bool, error) {
	if len(candidates) == 0 {
		return compare.HostPathStatus{}, false, fmt.Errorf("dirty host path %s has no commit candidates", path)
	}
	if strings.TrimSpace(selectedHost) != "" {
		for _, candidate := range candidates {
			if candidate.Host == selectedHost {
				return candidate, false, nil
			}
		}
		hosts := availableCommitHosts(candidates)
		return compare.HostPathStatus{}, false, fmt.Errorf("dirty host path %s is not tracked on selected host %q; available hosts: %s", path, selectedHost, strings.Join(hosts, ", "))
	}
	if len(candidates) == 1 {
		return candidates[0], false, nil
	}
	firstDigest := strings.TrimSpace(candidates[0].LiveDigest)
	if firstDigest != "" {
		sameDigest := true
		for _, candidate := range candidates[1:] {
			if strings.TrimSpace(candidate.LiveDigest) != firstDigest {
				sameDigest = false
				break
			}
		}
		if sameDigest {
			return candidates[0], false, nil
		}
	}
	hosts := availableCommitHosts(candidates)
	return compare.HostPathStatus{}, false, fmt.Errorf("dirty host path %s has multiple host-specific live contents; specify --host from [%s]", path, strings.Join(hosts, ", "))
}

func availableCommitHosts(candidates []compare.HostPathStatus) []string {
	set := make(map[string]struct{}, len(candidates))
	hosts := make([]string, 0, len(candidates))
	for _, candidate := range candidates {
		host := strings.TrimSpace(candidate.Host)
		if host == "" {
			host = "localhost"
		}
		if _, ok := set[host]; ok {
			continue
		}
		set[host] = struct{}{}
		hosts = append(hosts, host)
	}
	sort.Strings(hosts)
	return hosts
}

func findRenderedComponent(bundle *hydrate.Bundle, name string) *hydrate.RenderedComponent {
	if bundle == nil || strings.TrimSpace(name) == "" {
		return nil
	}
	for i := range bundle.Spec.Components {
		if bundle.Spec.Components[i].Name == name {
			return &bundle.Spec.Components[i]
		}
	}
	return nil
}

func resolveCommittedHostPath(hostRoot, trackedHostPath string) (string, error) {
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

func writeCommittedHostPath(path string, content []byte) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	mode := os.FileMode(0o644)
	if info, err := os.Stat(path); err == nil {
		mode = info.Mode().Perm()
	}
	return os.WriteFile(path, content, mode)
}

func upsertPatchDocument(path string, identity manifestIdentity, overlay map[string]interface{}) error {
	documents, err := loadObjectDocuments(path)
	if err != nil {
		if !os.IsNotExist(err) {
			return err
		}
		documents = nil
	}

	updated := false
	matches := 0
	for i, document := range documents {
		if !sameManifestIdentity(identity, extractManifestIdentity(document)) {
			continue
		}
		documents[i] = cloneObject(overlay)
		updated = true
		matches++
	}
	if matches > 1 {
		return fmt.Errorf("patch file %q contains duplicate documents for %s", path, manifestIdentityString(identity))
	}
	if !updated {
		documents = append(documents, cloneObject(overlay))
	}
	return writeObjectDocuments(path, documents)
}

func loadObjectDocuments(path string) ([]map[string]interface{}, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}

	decoder := utilyaml.NewYAMLOrJSONDecoder(bytes.NewReader(data), 4096)
	documents := make([]map[string]interface{}, 0)
	for {
		var raw runtime.RawExtension
		if err := decoder.Decode(&raw); err != nil {
			if err == io.EOF {
				break
			}
			return nil, fmt.Errorf("decode patch file %q: %w", path, err)
		}
		if len(bytes.TrimSpace(raw.Raw)) == 0 {
			continue
		}

		var document map[string]interface{}
		if err := yaml.Unmarshal(raw.Raw, &document); err != nil {
			return nil, fmt.Errorf("unmarshal patch file %q: %w", path, err)
		}
		if len(document) == 0 {
			continue
		}
		documents = append(documents, document)
	}
	return documents, nil
}

func writeObjectDocuments(path string, documents []map[string]interface{}) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}

	rendered := make([][]byte, 0, len(documents))
	for _, document := range documents {
		data, err := yaml.Marshal(document)
		if err != nil {
			return fmt.Errorf("marshal patch file %q: %w", path, err)
		}
		rendered = append(rendered, bytes.TrimRight(data, "\n"))
	}
	content := strings.Join(byteSlicesToStrings(rendered), "\n---\n")
	if len(content) > 0 {
		content += "\n"
	}
	return os.WriteFile(path, []byte(content), 0o644)
}

func byteSlicesToStrings(values [][]byte) []string {
	if len(values) == 0 {
		return nil
	}
	rendered := make([]string, 0, len(values))
	for _, value := range values {
		rendered = append(rendered, string(value))
	}
	return rendered
}

func extractManifestIdentity(document map[string]interface{}) manifestIdentity {
	identity := manifestIdentity{}
	if document == nil {
		return identity
	}
	if value, ok := document["apiVersion"].(string); ok {
		identity.APIVersion = value
	}
	if value, ok := document["kind"].(string); ok {
		identity.Kind = value
	}
	metadata, _ := document["metadata"].(map[string]interface{})
	if value, ok := metadata["name"].(string); ok {
		identity.Name = value
	}
	if value, ok := metadata["namespace"].(string); ok {
		identity.Namespace = value
	}
	return identity
}

func sameManifestIdentity(left, right manifestIdentity) bool {
	return left.APIVersion == right.APIVersion &&
		left.Kind == right.Kind &&
		left.Namespace == right.Namespace &&
		left.Name == right.Name
}

func ensurePatchIdentity(document map[string]interface{}, identity manifestIdentity) {
	document["apiVersion"] = identity.APIVersion
	document["kind"] = identity.Kind

	metadata, _ := document["metadata"].(map[string]interface{})
	if metadata == nil {
		metadata = make(map[string]interface{})
		document["metadata"] = metadata
	}
	metadata["name"] = identity.Name
	if identity.Namespace == "" {
		delete(metadata, "namespace")
	} else {
		metadata["namespace"] = identity.Namespace
	}
}

func cloneObject(document map[string]interface{}) map[string]interface{} {
	if document == nil {
		return nil
	}
	data, _ := yaml.Marshal(document)
	var cloned map[string]interface{}
	_ = yaml.Unmarshal(data, &cloned)
	return cloned
}

func sanitizeLocalResourceDocument(document map[string]interface{}) map[string]interface{} {
	cloned := cloneObject(document)
	if cloned == nil {
		return nil
	}
	delete(cloned, "status")

	metadata, ok := cloned["metadata"].(map[string]interface{})
	if ok {
		for _, key := range []string{
			"uid",
			"resourceVersion",
			"generation",
			"creationTimestamp",
			"managedFields",
			"ownerReferences",
			"finalizers",
			"selfLink",
			"deletionTimestamp",
			"deletionGracePeriodSeconds",
		} {
			delete(metadata, key)
		}
	}
	return cloned
}

func manifestIdentityString(identity manifestIdentity) string {
	return objectIdentityString(identity.APIVersion, identity.Kind, identity.Namespace, identity.Name)
}

func objectIdentityString(apiVersion, kind, namespace, name string) string {
	if strings.TrimSpace(namespace) == "" {
		return fmt.Sprintf("%s %s/%s", apiVersion, kind, name)
	}
	return fmt.Sprintf("%s %s/%s in namespace %s", apiVersion, kind, name, namespace)
}
