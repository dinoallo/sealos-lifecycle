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
	"path/filepath"
	"sort"
	"strings"

	"github.com/opencontainers/go-digest"

	"github.com/labring/sealos/pkg/distribution/ownership"
	"github.com/labring/sealos/pkg/distribution/packageformat"
)

const (
	InputsDirName    = "inputs"
	ResourcesDirName = "resources"
	PatchesDirName   = "patches"
	PolicyDirName    = "policy"
)

type Patch struct {
	Component    string
	Path         string
	RelativePath string
}

type Resource struct {
	Path         string
	RelativePath string
}

type Repo struct {
	Root     string
	Revision string

	inputsByComponent     map[string]map[string]string
	hostInputsByComponent map[string]map[string]map[string]string
	resources             []Resource
	patchesByComponent    map[string][]Patch
	localPatchPolicy      *ownership.LocalPatchPolicyDocument
	policyRelativePath    string
}

func Load(root string) (*Repo, error) {
	if strings.TrimSpace(root) == "" {
		return nil, fmt.Errorf("local repo root cannot be empty")
	}

	resolvedRoot, err := filepath.Abs(root)
	if err != nil {
		return nil, fmt.Errorf("resolve local repo root %q: %w", root, err)
	}

	info, err := os.Stat(resolvedRoot)
	if err != nil {
		return nil, fmt.Errorf("stat local repo root %q: %w", resolvedRoot, err)
	}
	if !info.IsDir() {
		return nil, fmt.Errorf("local repo root %q must be a directory", resolvedRoot)
	}

	inputsByComponent, hostInputsByComponent, inputHashes, err := scanInputs(resolvedRoot)
	if err != nil {
		return nil, err
	}
	resources, resourceHashes, err := scanResources(resolvedRoot)
	if err != nil {
		return nil, err
	}
	patchesByComponent, patchHashes, err := scanPatches(resolvedRoot)
	if err != nil {
		return nil, err
	}
	localPatchPolicy, policyHash, err := scanLocalPatchPolicy(resolvedRoot)
	if err != nil {
		return nil, err
	}

	hashParts := append(append(inputHashes, resourceHashes...), patchHashes...)
	if policyHash != "" {
		hashParts = append(hashParts, policyHash)
	}

	return &Repo{
		Root:                  resolvedRoot,
		Revision:              digestHashes(hashParts),
		inputsByComponent:     inputsByComponent,
		hostInputsByComponent: hostInputsByComponent,
		resources:             resources,
		patchesByComponent:    patchesByComponent,
		localPatchPolicy:      localPatchPolicy,
		policyRelativePath:    policyRelativePath(localPatchPolicy),
	}, nil
}

func (r *Repo) BindingFor(componentName string, input packageformat.Input) (string, bool) {
	if r == nil {
		return "", false
	}
	if componentName == "" || input.Name == "" {
		return "", false
	}

	componentInputs, ok := r.inputsByComponent[componentName]
	if !ok {
		return "", false
	}
	path, ok := componentInputs[input.Name]
	return path, ok
}

func (r *Repo) HostBindingsFor(componentName string, input packageformat.Input) map[string]string {
	if r == nil || componentName == "" || input.Name == "" {
		return nil
	}

	componentInputs, ok := r.hostInputsByComponent[componentName]
	if !ok {
		return nil
	}
	hostBindings, ok := componentInputs[input.Name]
	if !ok || len(hostBindings) == 0 {
		return nil
	}
	cloned := make(map[string]string, len(hostBindings))
	for host, path := range hostBindings {
		cloned[host] = path
	}
	return cloned
}

func (r *Repo) HostBindingPath(componentName, inputName, host string) (string, bool) {
	if r == nil || componentName == "" || inputName == "" {
		return "", false
	}

	componentInputs, ok := r.hostInputsByComponent[componentName]
	if !ok {
		return "", false
	}
	hostBindings, ok := componentInputs[inputName]
	if !ok || len(hostBindings) == 0 {
		return "", false
	}
	return hostBindingPathForHost(hostBindings, host)
}

func hostBindingPathForHost(bindings map[string]string, host string) (string, bool) {
	trimmed := strings.TrimSpace(host)
	if trimmed == "" || len(bindings) == 0 {
		return "", false
	}
	if path := strings.TrimSpace(bindings[trimmed]); path != "" {
		return path, true
	}

	hostWithoutPort := hostBindingHostWithoutPort(trimmed)
	if hostWithoutPort == "" || hostWithoutPort == trimmed {
		return "", false
	}
	path := strings.TrimSpace(bindings[hostWithoutPort])
	return path, path != ""
}

func hostBindingHostWithoutPort(host string) string {
	trimmed := strings.TrimSpace(host)
	if trimmed == "" {
		return ""
	}
	if base, _, found := strings.Cut(trimmed, ":"); found {
		return strings.TrimSpace(base)
	}
	return trimmed
}

func (r *Repo) ResourcePaths() []string {
	if r == nil || len(r.resources) == 0 {
		return nil
	}
	paths := make([]string, 0, len(r.resources))
	for _, resource := range r.resources {
		paths = append(paths, resource.Path)
	}
	return paths
}

func (r *Repo) Resources() []Resource {
	if r == nil || len(r.resources) == 0 {
		return nil
	}
	return append([]Resource(nil), r.resources...)
}

func (r *Repo) PatchesFor(componentName string) []Patch {
	if r == nil || componentName == "" {
		return nil
	}
	patches := r.patchesByComponent[componentName]
	if len(patches) == 0 {
		return nil
	}
	return append([]Patch(nil), patches...)
}

func (r *Repo) LocalPatchPolicy() *ownership.LocalPatchPolicyDocument {
	if r == nil || r.localPatchPolicy == nil {
		return nil
	}
	return r.localPatchPolicy.Clone()
}

func (r *Repo) LocalPatchPolicyRelativePath() string {
	if r == nil || r.localPatchPolicy == nil {
		return ""
	}
	return r.policyRelativePath
}

func scanInputs(root string) (map[string]map[string]string, map[string]map[string]map[string]string, []string, error) {
	inputsRoot := filepath.Join(root, InputsDirName)
	if _, err := os.Stat(inputsRoot); err != nil {
		if os.IsNotExist(err) {
			return map[string]map[string]string{}, map[string]map[string]map[string]string{}, nil, nil
		}
		return nil, nil, nil, fmt.Errorf("stat local repo inputs root %q: %w", inputsRoot, err)
	}

	componentEntries, err := os.ReadDir(inputsRoot)
	if err != nil {
		return nil, nil, nil, fmt.Errorf("read local repo inputs root %q: %w", inputsRoot, err)
	}

	sort.Slice(componentEntries, func(i, j int) bool {
		return componentEntries[i].Name() < componentEntries[j].Name()
	})

	inputsByComponent := make(map[string]map[string]string, len(componentEntries))
	hostInputsByComponent := make(map[string]map[string]map[string]string, len(componentEntries))
	hashParts := make([]string, 0)

	for _, componentEntry := range componentEntries {
		if !componentEntry.IsDir() {
			continue
		}

		componentName := componentEntry.Name()
		componentDir := filepath.Join(inputsRoot, componentName)
		fileEntries, err := os.ReadDir(componentDir)
		if err != nil {
			return nil, nil, nil, fmt.Errorf("read local repo component input dir %q: %w", componentDir, err)
		}

		sort.Slice(fileEntries, func(i, j int) bool {
			return fileEntries[i].Name() < fileEntries[j].Name()
		})

		componentInputs := make(map[string]string)
		componentHostInputs := make(map[string]map[string]string)
		for _, fileEntry := range fileEntries {
			if !fileEntry.IsDir() {
				filename := fileEntry.Name()
				inputName := strings.TrimSuffix(filename, filepath.Ext(filename))
				if inputName == "" {
					return nil, nil, nil, fmt.Errorf("local repo input file %q must have a basename", filepath.Join(componentDir, filename))
				}
				if _, ok := componentInputs[inputName]; ok {
					return nil, nil, nil, fmt.Errorf("local repo component %q has multiple files for input %q", componentName, inputName)
				}

				absPath := filepath.Join(componentDir, filename)
				data, err := os.ReadFile(absPath)
				if err != nil {
					return nil, nil, nil, fmt.Errorf("read local repo input file %q: %w", absPath, err)
				}

				componentInputs[inputName] = absPath
				hashParts = append(hashParts, fmt.Sprintf("%s/%s=%s", componentName, filename, digest.Canonical.FromBytes(data)))
				continue
			}
			if fileEntry.Name() != "hosts" {
				continue
			}
			hostInputsRoot := filepath.Join(componentDir, fileEntry.Name())
			hostEntries, err := os.ReadDir(hostInputsRoot)
			if err != nil {
				return nil, nil, nil, fmt.Errorf("read local repo host input dir %q: %w", hostInputsRoot, err)
			}
			sort.Slice(hostEntries, func(i, j int) bool {
				return hostEntries[i].Name() < hostEntries[j].Name()
			})
			for _, hostEntry := range hostEntries {
				if !hostEntry.IsDir() {
					continue
				}
				hostName := hostEntry.Name()
				hostDir := filepath.Join(hostInputsRoot, hostName)
				hostFiles, err := os.ReadDir(hostDir)
				if err != nil {
					return nil, nil, nil, fmt.Errorf("read local repo host input dir %q: %w", hostDir, err)
				}
				sort.Slice(hostFiles, func(i, j int) bool {
					return hostFiles[i].Name() < hostFiles[j].Name()
				})
				for _, hostFile := range hostFiles {
					if hostFile.IsDir() {
						continue
					}
					filename := hostFile.Name()
					inputName := strings.TrimSuffix(filename, filepath.Ext(filename))
					if inputName == "" {
						return nil, nil, nil, fmt.Errorf("local repo host input file %q must have a basename", filepath.Join(hostDir, filename))
					}
					hostBindings, ok := componentHostInputs[inputName]
					if !ok {
						hostBindings = make(map[string]string)
						componentHostInputs[inputName] = hostBindings
					}
					if _, ok := hostBindings[hostName]; ok {
						return nil, nil, nil, fmt.Errorf("local repo component %q host %q has multiple files for input %q", componentName, hostName, inputName)
					}

					absPath := filepath.Join(hostDir, filename)
					data, err := os.ReadFile(absPath)
					if err != nil {
						return nil, nil, nil, fmt.Errorf("read local repo host input file %q: %w", absPath, err)
					}

					hostBindings[hostName] = absPath
					hashParts = append(hashParts, fmt.Sprintf("%s/hosts/%s/%s=%s", componentName, hostName, filename, digest.Canonical.FromBytes(data)))
				}
			}
		}

		if len(componentInputs) > 0 {
			inputsByComponent[componentName] = componentInputs
		}
		if len(componentHostInputs) > 0 {
			hostInputsByComponent[componentName] = componentHostInputs
		}
	}

	return inputsByComponent, hostInputsByComponent, hashParts, nil
}

func scanResources(root string) ([]Resource, []string, error) {
	resourcesRoot := filepath.Join(root, ResourcesDirName)
	if _, err := os.Stat(resourcesRoot); err != nil {
		if os.IsNotExist(err) {
			return nil, nil, nil
		}
		return nil, nil, fmt.Errorf("stat local repo resources root %q: %w", resourcesRoot, err)
	}

	resources := make([]Resource, 0)
	hashParts := make([]string, 0)
	if err := filepath.WalkDir(resourcesRoot, func(path string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.IsDir() {
			return nil
		}

		relPath, err := filepath.Rel(resourcesRoot, path)
		if err != nil {
			return err
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}

		resources = append(resources, Resource{
			Path:         path,
			RelativePath: filepath.ToSlash(relPath),
		})
		hashParts = append(hashParts, fmt.Sprintf("%s=%s", filepath.ToSlash(relPath), digest.Canonical.FromBytes(data)))
		return nil
	}); err != nil {
		return nil, nil, fmt.Errorf("scan local repo resources in %q: %w", resourcesRoot, err)
	}

	sort.Slice(resources, func(i, j int) bool {
		return resources[i].RelativePath < resources[j].RelativePath
	})
	sort.Strings(hashParts)
	return resources, hashParts, nil
}

func scanPatches(root string) (map[string][]Patch, []string, error) {
	patchesRoot := filepath.Join(root, PatchesDirName)
	if _, err := os.Stat(patchesRoot); err != nil {
		if os.IsNotExist(err) {
			return map[string][]Patch{}, nil, nil
		}
		return nil, nil, fmt.Errorf("stat local repo patches root %q: %w", patchesRoot, err)
	}

	componentEntries, err := os.ReadDir(patchesRoot)
	if err != nil {
		return nil, nil, fmt.Errorf("read local repo patches root %q: %w", patchesRoot, err)
	}
	sort.Slice(componentEntries, func(i, j int) bool {
		return componentEntries[i].Name() < componentEntries[j].Name()
	})

	patchesByComponent := make(map[string][]Patch, len(componentEntries))
	hashParts := make([]string, 0)

	for _, componentEntry := range componentEntries {
		if !componentEntry.IsDir() {
			continue
		}

		componentName := componentEntry.Name()
		componentDir := filepath.Join(patchesRoot, componentName)
		componentPatches := make([]Patch, 0)
		if err := filepath.WalkDir(componentDir, func(path string, entry os.DirEntry, walkErr error) error {
			if walkErr != nil {
				return walkErr
			}
			if entry.IsDir() {
				return nil
			}

			relPath, err := filepath.Rel(componentDir, path)
			if err != nil {
				return err
			}
			data, err := os.ReadFile(path)
			if err != nil {
				return err
			}

			componentPatches = append(componentPatches, Patch{
				Component:    componentName,
				Path:         path,
				RelativePath: filepath.ToSlash(relPath),
			})
			hashParts = append(hashParts, fmt.Sprintf("%s/%s=%s", componentName, filepath.ToSlash(relPath), digest.Canonical.FromBytes(data)))
			return nil
		}); err != nil {
			return nil, nil, fmt.Errorf("scan local repo patches in %q: %w", componentDir, err)
		}

		sort.Slice(componentPatches, func(i, j int) bool {
			return componentPatches[i].RelativePath < componentPatches[j].RelativePath
		})
		if len(componentPatches) > 0 {
			patchesByComponent[componentName] = componentPatches
		}
	}

	sort.Strings(hashParts)
	return patchesByComponent, hashParts, nil
}

func scanLocalPatchPolicy(root string) (*ownership.LocalPatchPolicyDocument, string, error) {
	policyPath := filepath.Join(root, PolicyDirName, ownership.LocalPatchPolicyFileName)
	if _, err := os.Stat(policyPath); err != nil {
		if os.IsNotExist(err) {
			return nil, "", nil
		}
		return nil, "", fmt.Errorf("stat local repo policy file %q: %w", policyPath, err)
	}

	doc, err := ownership.LoadLocalPatchPolicyFile(policyPath)
	if err != nil {
		return nil, "", err
	}
	data, err := os.ReadFile(policyPath)
	if err != nil {
		return nil, "", fmt.Errorf("read local repo policy file %q: %w", policyPath, err)
	}
	return doc, fmt.Sprintf("%s=%s", filepath.ToSlash(filepath.Join(PolicyDirName, ownership.LocalPatchPolicyFileName)), digest.Canonical.FromBytes(data)), nil
}

func policyRelativePath(doc *ownership.LocalPatchPolicyDocument) string {
	if doc == nil {
		return ""
	}
	return filepath.ToSlash(filepath.Join(PolicyDirName, ownership.LocalPatchPolicyFileName))
}

func digestHashes(parts []string) string {
	if len(parts) == 0 {
		return digest.Canonical.FromBytes(nil).String()
	}
	sort.Strings(parts)
	return digest.Canonical.FromString(strings.Join(parts, "\n")).String()
}
