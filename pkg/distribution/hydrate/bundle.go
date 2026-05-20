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
	"slices"
	"strings"

	"sigs.k8s.io/yaml"

	"github.com/labring/sealos/pkg/distribution"
	"github.com/labring/sealos/pkg/distribution/bom"
	"github.com/labring/sealos/pkg/distribution/ownership"
	"github.com/labring/sealos/pkg/distribution/packageformat"
)

const BundleFileName = "bundle.yaml"

type Metadata struct {
	Name   string            `json:"name" yaml:"name"`
	Labels map[string]string `json:"labels,omitempty" yaml:"labels,omitempty"`
}

type Bundle struct {
	APIVersion string     `json:"apiVersion" yaml:"apiVersion"`
	Kind       string     `json:"kind" yaml:"kind"`
	Metadata   Metadata   `json:"metadata" yaml:"metadata"`
	Spec       BundleSpec `json:"spec" yaml:"spec"`
}

type BundleSpec struct {
	BOMName                string                           `json:"bomName" yaml:"bomName"`
	Revision               string                           `json:"revision" yaml:"revision"`
	Channel                bom.ReleaseChannel               `json:"channel" yaml:"channel"`
	RenderProvenance       RenderProvenance                 `json:"renderProvenance,omitempty" yaml:"renderProvenance,omitempty"`
	SourcePreflight        *SourcePreflight                 `json:"sourcePreflight,omitempty" yaml:"sourcePreflight,omitempty"`
	ExecutionTopology      ExecutionTopology                `json:"executionTopology,omitempty" yaml:"executionTopology,omitempty"`
	LocalPatchPolicySource ownership.LocalPatchPolicySource `json:"localPatchPolicySource,omitempty" yaml:"localPatchPolicySource,omitempty"`
	LocalPatchPolicyScope  ownership.LocalPatchPolicyScope  `json:"localPatchPolicyScope,omitempty" yaml:"localPatchPolicyScope,omitempty"`
	LocalPatchPolicyName   string                           `json:"localPatchPolicyName,omitempty" yaml:"localPatchPolicyName,omitempty"`
	LocalPatchPolicyPath   string                           `json:"localPatchPolicyPath,omitempty" yaml:"localPatchPolicyPath,omitempty"`
	LocalPatchPolicyDigest string                           `json:"localPatchPolicyDigest,omitempty" yaml:"localPatchPolicyDigest,omitempty"`
	LocalResources         []string                         `json:"localResources,omitempty" yaml:"localResources,omitempty"`
	TrackedK8sObjects      []TrackedK8sObject               `json:"trackedK8sObjects,omitempty" yaml:"trackedK8sObjects,omitempty"`
	TrackedHostPaths       []TrackedHostPath                `json:"trackedHostPaths,omitempty" yaml:"trackedHostPaths,omitempty"`
	Components             []RenderedComponent              `json:"components" yaml:"components"`
}

type RenderProvenance struct {
	DistributionChannelPath   string                          `json:"distributionChannelPath,omitempty" yaml:"distributionChannelPath,omitempty"`
	DistributionChannelDigest string                          `json:"distributionChannelDigest,omitempty" yaml:"distributionChannelDigest,omitempty"`
	DistributionLine          string                          `json:"distributionLine,omitempty" yaml:"distributionLine,omitempty"`
	BOMPath                   string                          `json:"bomPath,omitempty" yaml:"bomPath,omitempty"`
	BOMDigest                 string                          `json:"bomDigest,omitempty" yaml:"bomDigest,omitempty"`
	LocalRepoPath             string                          `json:"localRepoPath,omitempty" yaml:"localRepoPath,omitempty"`
	LocalRepoRevision         string                          `json:"localRepoRevision,omitempty" yaml:"localRepoRevision,omitempty"`
	LocalPatchRevision        string                          `json:"localPatchRevision,omitempty" yaml:"localPatchRevision,omitempty"`
	PackageSources            []RenderProvenancePackageSource `json:"packageSources,omitempty" yaml:"packageSources,omitempty"`
}

type RenderProvenancePackageSource struct {
	Component string `json:"component" yaml:"component"`
	Path      string `json:"path" yaml:"path"`
	Digest    string `json:"digest,omitempty" yaml:"digest,omitempty"`
}

type SourcePreflight struct {
	State             string                `json:"state" yaml:"state"`
	Summary           string                `json:"summary" yaml:"summary"`
	RecommendedAction string                `json:"recommendedAction" yaml:"recommendedAction"`
	Blocked           bool                  `json:"blocked" yaml:"blocked"`
	BlockedReasons    []string              `json:"blockedReasons,omitempty" yaml:"blockedReasons,omitempty"`
	Counts            SourcePreflightCounts `json:"counts" yaml:"counts"`
	LocalRepoDoctor   *SourcePreflightCheck `json:"localRepoDoctor,omitempty" yaml:"localRepoDoctor,omitempty"`
	Validate          SourcePreflightCheck  `json:"validate" yaml:"validate"`
}

type SourcePreflightCounts struct {
	Components       int `json:"components" yaml:"components"`
	RequiredInputs   int `json:"requiredInputs" yaml:"requiredInputs"`
	BoundInputs      int `json:"boundInputs" yaml:"boundInputs"`
	LocalResources   int `json:"localResources" yaml:"localResources"`
	LocalPatches     int `json:"localPatches" yaml:"localPatches"`
	DoctorErrors     int `json:"doctorErrors" yaml:"doctorErrors"`
	DoctorWarnings   int `json:"doctorWarnings" yaml:"doctorWarnings"`
	ValidateErrors   int `json:"validateErrors" yaml:"validateErrors"`
	ValidateWarnings int `json:"validateWarnings" yaml:"validateWarnings"`
}

type SourcePreflightCheck struct {
	Passed   bool `json:"passed" yaml:"passed"`
	Errors   int  `json:"errors" yaml:"errors"`
	Warnings int  `json:"warnings" yaml:"warnings"`
}

func (p RenderProvenance) Empty() bool {
	return strings.TrimSpace(p.DistributionChannelPath) == "" &&
		strings.TrimSpace(p.DistributionChannelDigest) == "" &&
		strings.TrimSpace(p.DistributionLine) == "" &&
		strings.TrimSpace(p.BOMPath) == "" &&
		strings.TrimSpace(p.BOMDigest) == "" &&
		strings.TrimSpace(p.LocalRepoPath) == "" &&
		strings.TrimSpace(p.LocalRepoRevision) == "" &&
		strings.TrimSpace(p.LocalPatchRevision) == "" &&
		len(p.PackageSources) == 0
}

func (p RenderProvenance) Normalize() RenderProvenance {
	normalized := RenderProvenance{
		DistributionChannelPath:   strings.TrimSpace(p.DistributionChannelPath),
		DistributionChannelDigest: strings.TrimSpace(p.DistributionChannelDigest),
		DistributionLine:          strings.TrimSpace(p.DistributionLine),
		BOMPath:                   strings.TrimSpace(p.BOMPath),
		BOMDigest:                 strings.TrimSpace(p.BOMDigest),
		LocalRepoPath:             strings.TrimSpace(p.LocalRepoPath),
		LocalRepoRevision:         strings.TrimSpace(p.LocalRepoRevision),
		LocalPatchRevision:        strings.TrimSpace(p.LocalPatchRevision),
	}
	if len(p.PackageSources) > 0 {
		normalized.PackageSources = make([]RenderProvenancePackageSource, 0, len(p.PackageSources))
		for _, source := range p.PackageSources {
			component := strings.TrimSpace(source.Component)
			path := strings.TrimSpace(source.Path)
			if component == "" && path == "" {
				continue
			}
			normalized.PackageSources = append(normalized.PackageSources, RenderProvenancePackageSource{
				Component: component,
				Path:      path,
				Digest:    strings.TrimSpace(source.Digest),
			})
		}
		slices.SortFunc(normalized.PackageSources, func(a, b RenderProvenancePackageSource) int {
			if a.Component < b.Component {
				return -1
			}
			if a.Component > b.Component {
				return 1
			}
			if a.Path < b.Path {
				return -1
			}
			if a.Path > b.Path {
				return 1
			}
			return 0
		})
	}
	return normalized
}

type ExecutionTopology struct {
	Source      string                  `json:"source,omitempty" yaml:"source,omitempty"`
	AllNodes    []string                `json:"allNodes,omitempty" yaml:"allNodes,omitempty"`
	FirstMaster string                  `json:"firstMaster,omitempty" yaml:"firstMaster,omitempty"`
	HostRoles   []ExecutionHostRoleList `json:"hostRoles,omitempty" yaml:"hostRoles,omitempty"`
}

type ExecutionHostRoleList struct {
	Host  string   `json:"host" yaml:"host"`
	Roles []string `json:"roles,omitempty" yaml:"roles,omitempty"`
}

type RenderedComponent struct {
	Name              string                       `json:"name" yaml:"name"`
	PackageName       string                       `json:"packageName" yaml:"packageName"`
	Version           string                       `json:"version" yaml:"version"`
	Class             packageformat.PackageClass   `json:"class" yaml:"class"`
	Artifact          string                       `json:"artifact" yaml:"artifact"`
	Dependencies      []string                     `json:"dependencies,omitempty" yaml:"dependencies,omitempty"`
	Inputs            []packageformat.Input        `json:"inputs,omitempty" yaml:"inputs,omitempty"`
	InputBindings     map[string]string            `json:"inputBindings,omitempty" yaml:"inputBindings,omitempty"`
	HostInputBindings map[string]map[string]string `json:"hostInputBindings,omitempty" yaml:"hostInputBindings,omitempty"`
	LocalPatches      []string                     `json:"localPatches,omitempty" yaml:"localPatches,omitempty"`
	ManifestPath      string                       `json:"manifestPath" yaml:"manifestPath"`
	RootPath          string                       `json:"rootPath" yaml:"rootPath"`
	Steps             []RenderedStep               `json:"steps" yaml:"steps"`
}

type RenderedStep struct {
	Name           string                        `json:"name" yaml:"name"`
	Kind           StepKind                      `json:"kind" yaml:"kind"`
	BundlePath     string                        `json:"bundlePath" yaml:"bundlePath"`
	SourcePath     string                        `json:"sourcePath" yaml:"sourcePath"`
	ContentType    packageformat.ContentType     `json:"contentType,omitempty" yaml:"contentType,omitempty"`
	HookPhase      packageformat.HookPhase       `json:"hookPhase,omitempty" yaml:"hookPhase,omitempty"`
	Target         packageformat.ExecutionTarget `json:"target,omitempty" yaml:"target,omitempty"`
	MediaType      string                        `json:"mediaType,omitempty" yaml:"mediaType,omitempty"`
	Required       bool                          `json:"required,omitempty" yaml:"required,omitempty"`
	Args           []string                      `json:"args,omitempty" yaml:"args,omitempty"`
	TimeoutSeconds int32                         `json:"timeoutSeconds,omitempty" yaml:"timeoutSeconds,omitempty"`
}

func NewBundle(plan *Plan) *Bundle {
	bundle := &Bundle{
		APIVersion: distribution.APIVersion,
		Kind:       distribution.KindHydratedBundle,
	}
	if plan == nil {
		return bundle
	}

	bundle.Metadata = Metadata{
		Name: fmt.Sprintf("%s-%s", plan.BOMName, plan.Revision),
		Labels: map[string]string{
			"distribution.sealos.io/bom-name": plan.BOMName,
			"distribution.sealos.io/revision": plan.Revision,
			"distribution.sealos.io/channel":  string(plan.Channel),
		},
	}
	bundle.Spec = BundleSpec{
		BOMName:    plan.BOMName,
		Revision:   plan.Revision,
		Channel:    plan.Channel,
		Components: make([]RenderedComponent, 0, len(plan.Components)),
	}
	return bundle
}

func NewSingleNodeExecutionTopology() ExecutionTopology {
	return ExecutionTopology{
		Source:      "singleNodeDefault",
		AllNodes:    []string{"localhost"},
		FirstMaster: "localhost",
		HostRoles: []ExecutionHostRoleList{
			{
				Host:  "localhost",
				Roles: []string{"master"},
			},
		},
	}
}

func (t ExecutionTopology) Empty() bool {
	return len(t.AllNodes) == 0 && strings.TrimSpace(t.FirstMaster) == "" && len(t.HostRoles) == 0
}

func (t ExecutionTopology) Normalize() ExecutionTopology {
	normalized := ExecutionTopology{
		Source:      strings.TrimSpace(t.Source),
		AllNodes:    normalizeTopologyHosts(t.AllNodes),
		FirstMaster: strings.TrimSpace(t.FirstMaster),
	}
	if normalized.FirstMaster == "" && len(normalized.AllNodes) > 0 {
		normalized.FirstMaster = normalized.AllNodes[0]
	}

	rolesByHost := make(map[string][]string, len(t.HostRoles))
	for _, item := range t.HostRoles {
		host := strings.TrimSpace(item.Host)
		if host == "" {
			continue
		}
		rolesByHost[host] = normalizeTopologyRoles(item.Roles)
	}
	normalized.HostRoles = make([]ExecutionHostRoleList, 0, len(normalized.AllNodes)+len(rolesByHost))
	seen := make(map[string]struct{}, len(normalized.AllNodes)+len(rolesByHost))
	for _, host := range normalized.AllNodes {
		normalized.HostRoles = append(normalized.HostRoles, ExecutionHostRoleList{
			Host:  host,
			Roles: slices.Clone(rolesByHost[host]),
		})
		seen[host] = struct{}{}
	}
	extraHosts := make([]string, 0, len(rolesByHost))
	for host := range rolesByHost {
		if _, ok := seen[host]; ok {
			continue
		}
		extraHosts = append(extraHosts, host)
	}
	slices.Sort(extraHosts)
	for _, host := range extraHosts {
		normalized.HostRoles = append(normalized.HostRoles, ExecutionHostRoleList{
			Host:  host,
			Roles: slices.Clone(rolesByHost[host]),
		})
	}
	return normalized
}

func (t ExecutionTopology) RolesForHost(host string) []string {
	host = strings.TrimSpace(host)
	if host == "" {
		return nil
	}
	normalized := t.Normalize()
	for _, item := range normalized.HostRoles {
		if item.Host == host {
			return slices.Clone(item.Roles)
		}
	}
	return nil
}

func normalizeTopologyHosts(hosts []string) []string {
	seen := make(map[string]struct{}, len(hosts))
	normalized := make([]string, 0, len(hosts))
	for _, host := range hosts {
		host = strings.TrimSpace(host)
		if host == "" {
			continue
		}
		if _, ok := seen[host]; ok {
			continue
		}
		seen[host] = struct{}{}
		normalized = append(normalized, host)
	}
	return normalized
}

func normalizeTopologyRoles(roles []string) []string {
	seen := make(map[string]struct{}, len(roles))
	normalized := make([]string, 0, len(roles))
	for _, role := range roles {
		role = strings.TrimSpace(role)
		if role == "" {
			continue
		}
		if _, ok := seen[role]; ok {
			continue
		}
		seen[role] = struct{}{}
		normalized = append(normalized, role)
	}
	slices.Sort(normalized)
	return normalized
}

func (b Bundle) String() string {
	data, _ := yaml.Marshal(b)
	return string(data)
}
