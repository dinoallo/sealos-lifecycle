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

	"github.com/labring/sealos/pkg/distribution/bom"
	"github.com/labring/sealos/pkg/distribution/packageformat"
)

type StepKind string

const (
	StepContent StepKind = "content"
	StepHook    StepKind = "hook"
)

type Plan struct {
	BOMName    string             `json:"bomName" yaml:"bomName"`
	Revision   string             `json:"revision" yaml:"revision"`
	Channel    bom.ReleaseChannel `json:"channel" yaml:"channel"`
	Components []ComponentPlan    `json:"components" yaml:"components"`
}

type ComponentPlan struct {
	Name         string                     `json:"name" yaml:"name"`
	PackageName  string                     `json:"packageName" yaml:"packageName"`
	Version      string                     `json:"version" yaml:"version"`
	Class        packageformat.PackageClass `json:"class" yaml:"class"`
	Artifact     string                     `json:"artifact" yaml:"artifact"`
	Dependencies []string                   `json:"dependencies,omitempty" yaml:"dependencies,omitempty"`
	Inputs       []packageformat.Input      `json:"inputs,omitempty" yaml:"inputs,omitempty"`
	Steps        []Step                     `json:"steps" yaml:"steps"`
}

type Step struct {
	Name           string                        `json:"name" yaml:"name"`
	Kind           StepKind                      `json:"kind" yaml:"kind"`
	Path           string                        `json:"path" yaml:"path"`
	ContentType    packageformat.ContentType     `json:"contentType,omitempty" yaml:"contentType,omitempty"`
	HookPhase      packageformat.HookPhase       `json:"hookPhase,omitempty" yaml:"hookPhase,omitempty"`
	Target         packageformat.ExecutionTarget `json:"target,omitempty" yaml:"target,omitempty"`
	MediaType      string                        `json:"mediaType,omitempty" yaml:"mediaType,omitempty"`
	Required       bool                          `json:"required,omitempty" yaml:"required,omitempty"`
	Args           []string                      `json:"args,omitempty" yaml:"args,omitempty"`
	TimeoutSeconds int32                         `json:"timeoutSeconds,omitempty" yaml:"timeoutSeconds,omitempty"`
}

func BuildPlan(doc *bom.BOM, loader packageformat.Loader) (*Plan, error) {
	if doc == nil {
		return nil, fmt.Errorf("bom cannot be nil")
	}
	resolved, err := doc.ResolveComponentPackages(loader)
	if err != nil {
		return nil, err
	}
	return BuildPlanFromResolved(doc, resolved)
}

func BuildPlanFromResolved(doc *bom.BOM, resolved map[string]*packageformat.ComponentPackage) (*Plan, error) {
	if doc == nil {
		return nil, fmt.Errorf("bom cannot be nil")
	}
	if err := doc.Validate(); err != nil {
		return nil, err
	}

	componentIndex := make(map[string]bom.Component, len(doc.Spec.Components))
	for _, component := range doc.Spec.Components {
		componentIndex[component.Name] = component
	}

	dependenciesByComponent, err := buildDependencySet(componentIndex, resolved)
	if err != nil {
		return nil, err
	}
	order, err := topoSortDependencies(dependenciesByComponent)
	if err != nil {
		return nil, err
	}

	plan := &Plan{
		BOMName:  doc.Metadata.Name,
		Revision: doc.Spec.Revision,
		Channel:  doc.Spec.Channel,
	}

	for _, name := range order {
		component := componentIndex[name]
		pkg, ok := resolved[name]
		if !ok {
			return nil, fmt.Errorf("resolved package missing for component %q", name)
		}
		inputs := append([]packageformat.Input(nil), pkg.Spec.Inputs...)
		slices.SortFunc(inputs, compareInputs)
		plan.Components = append(plan.Components, ComponentPlan{
			Name:         component.Name,
			PackageName:  pkg.Metadata.Name,
			Version:      component.Version,
			Class:        pkg.Spec.Class,
			Artifact:     component.Artifact.Reference(),
			Dependencies: append([]string(nil), dependenciesByComponent[name]...),
			Inputs:       inputs,
			Steps:        buildSteps(pkg),
		})
	}

	return plan, nil
}

func buildDependencySet(componentIndex map[string]bom.Component, resolved map[string]*packageformat.ComponentPackage) (map[string][]string, error) {
	dependenciesByComponent := make(map[string][]string, len(componentIndex))
	for name, component := range componentIndex {
		pkg, ok := resolved[name]
		if !ok {
			return nil, fmt.Errorf("resolved package missing for component %q", name)
		}
		dependencies := make(map[string]struct{}, len(component.Dependencies)+len(pkg.Spec.Dependencies))
		for _, dependency := range component.Dependencies {
			dependencies[dependency] = struct{}{}
		}
		for _, dependency := range pkg.Spec.Dependencies {
			target, ok := componentIndex[dependency.Name]
			if !ok {
				if dependency.Optional {
					continue
				}
				return nil, fmt.Errorf("component %q requires missing dependency %q", name, dependency.Name)
			}
			if dependency.Version != "" && target.Version != dependency.Version {
				return nil, fmt.Errorf(
					"component %q dependency %q version mismatch, got %q want %q",
					name, dependency.Name, target.Version, dependency.Version,
				)
			}
			dependencies[dependency.Name] = struct{}{}
		}
		delete(dependencies, name)

		list := make([]string, 0, len(dependencies))
		for dependency := range dependencies {
			list = append(list, dependency)
		}
		slices.Sort(list)
		dependenciesByComponent[name] = list
	}
	return dependenciesByComponent, nil
}

func topoSortDependencies(dependenciesByComponent map[string][]string) ([]string, error) {
	indegree := make(map[string]int, len(dependenciesByComponent))
	reverse := make(map[string][]string, len(dependenciesByComponent))
	for name := range dependenciesByComponent {
		indegree[name] = 0
	}

	for name, dependencies := range dependenciesByComponent {
		for _, dependency := range dependencies {
			if _, ok := indegree[dependency]; !ok {
				return nil, fmt.Errorf("component %q depends on unknown component %q", name, dependency)
			}
			indegree[name]++
			reverse[dependency] = append(reverse[dependency], name)
		}
	}

	ready := make([]string, 0, len(indegree))
	for name, degree := range indegree {
		if degree == 0 {
			ready = append(ready, name)
		}
	}
	slices.Sort(ready)

	order := make([]string, 0, len(indegree))
	for len(ready) > 0 {
		name := ready[0]
		ready = ready[1:]
		order = append(order, name)

		next := append([]string(nil), reverse[name]...)
		slices.Sort(next)
		for _, dependent := range next {
			indegree[dependent]--
			if indegree[dependent] == 0 {
				ready = append(ready, dependent)
				slices.Sort(ready)
			}
		}
	}

	if len(order) != len(indegree) {
		return nil, fmt.Errorf("component dependency cycle detected")
	}
	return order, nil
}

func buildSteps(pkg *packageformat.ComponentPackage) []Step {
	contents := append([]packageformat.Content(nil), pkg.Spec.Contents...)
	slices.SortFunc(contents, compareContents)

	steps := make([]Step, 0, len(contents)+len(pkg.Spec.Hooks))
	for _, content := range contents {
		steps = append(steps, Step{
			Name:        content.Name,
			Kind:        StepContent,
			Path:        content.Path,
			ContentType: content.Type,
			MediaType:   content.MediaType,
			Required:    content.Required,
		})
	}

	hooks := append([]packageformat.Hook(nil), pkg.Spec.Hooks...)
	slices.SortFunc(hooks, compareHooks)
	for _, hook := range hooks {
		steps = append(steps, Step{
			Name:           hook.Name,
			Kind:           StepHook,
			Path:           hook.Path,
			HookPhase:      hook.Phase,
			Target:         hook.Target,
			Args:           append([]string(nil), hook.Args...),
			TimeoutSeconds: hook.TimeoutSeconds,
		})
	}
	return steps
}

func compareInputs(a, b packageformat.Input) int {
	if a.Type != b.Type {
		return compareString(string(a.Type), string(b.Type))
	}
	if a.Path != b.Path {
		return compareString(a.Path, b.Path)
	}
	return compareString(a.Name, b.Name)
}

func compareContents(a, b packageformat.Content) int {
	if rank := compareInt(contentRank(a.Type), contentRank(b.Type)); rank != 0 {
		return rank
	}
	if a.Path != b.Path {
		return compareString(a.Path, b.Path)
	}
	return compareString(a.Name, b.Name)
}

func compareHooks(a, b packageformat.Hook) int {
	if rank := compareInt(hookPhaseRank(a.Phase), hookPhaseRank(b.Phase)); rank != 0 {
		return rank
	}
	if a.Target != b.Target {
		return compareString(string(a.Target), string(b.Target))
	}
	if a.Path != b.Path {
		return compareString(a.Path, b.Path)
	}
	return compareString(a.Name, b.Name)
}

func contentRank(t packageformat.ContentType) int {
	switch t {
	case packageformat.ContentRootfs:
		return 0
	case packageformat.ContentFile:
		return 1
	case packageformat.ContentValues:
		return 2
	case packageformat.ContentManifest:
		return 3
	case packageformat.ContentChart:
		return 4
	case packageformat.ContentPatch:
		return 5
	case packageformat.ContentHook:
		return 6
	default:
		return 100
	}
}

func hookPhaseRank(p packageformat.HookPhase) int {
	switch p {
	case packageformat.PhaseBootstrap:
		return 0
	case packageformat.PhaseConfigure:
		return 1
	case packageformat.PhaseInstall:
		return 2
	case packageformat.PhaseUpgrade:
		return 3
	case packageformat.PhaseHealth:
		return 4
	case packageformat.PhaseRemove:
		return 5
	default:
		return 100
	}
}

func compareString(a, b string) int {
	switch {
	case a < b:
		return -1
	case a > b:
		return 1
	default:
		return 0
	}
}

func compareInt(a, b int) int {
	switch {
	case a < b:
		return -1
	case a > b:
		return 1
	default:
		return 0
	}
}
