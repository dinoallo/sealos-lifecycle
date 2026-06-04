// Copyright © 2026 sealos.
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

package cmd

import (
	"fmt"
	"sort"
	"strings"

	"github.com/spf13/cobra"

	"github.com/labring/sealos/pkg/distribution/hydrate"
	"github.com/labring/sealos/pkg/distribution/packageformat"
	"github.com/labring/sealos/pkg/distribution/reconcile"
)

type syncPlanSummary struct {
	Components        int  `json:"components" yaml:"components"`
	Steps             int  `json:"steps" yaml:"steps"`
	ContentSteps      int  `json:"contentSteps" yaml:"contentSteps"`
	HookSteps         int  `json:"hookSteps" yaml:"hookSteps"`
	LocalResources    int  `json:"localResources" yaml:"localResources"`
	KubernetesObjects int  `json:"kubernetesObjects" yaml:"kubernetesObjects"`
	SecretObjects     int  `json:"secretObjects" yaml:"secretObjects"`
	TrackedHostPaths  int  `json:"trackedHostPaths" yaml:"trackedHostPaths"`
	Blocked           bool `json:"blocked" yaml:"blocked"`
}

type syncPlanOutput struct {
	ClusterName        string                    `json:"clusterName" yaml:"clusterName"`
	BOMName            string                    `json:"bomName" yaml:"bomName"`
	Revision           string                    `json:"revision" yaml:"revision"`
	Channel            string                    `json:"channel" yaml:"channel"`
	BundlePath         string                    `json:"bundlePath" yaml:"bundlePath"`
	ExecutionTopology  hydrate.ExecutionTopology `json:"executionTopology" yaml:"executionTopology"`
	SourcePreflight    *hydrate.SourcePreflight  `json:"sourcePreflight,omitempty" yaml:"sourcePreflight,omitempty"`
	Summary            syncPlanSummary           `json:"summary" yaml:"summary"`
	Preflight          syncPreflightOutput       `json:"preflight" yaml:"preflight"`
	Components         []syncPlanComponent       `json:"components,omitempty" yaml:"components,omitempty"`
	LocalResources     []syncPlanLocalResource   `json:"localResources,omitempty" yaml:"localResources,omitempty"`
	KubernetesObjects  []syncPlanK8sObject       `json:"kubernetesObjects,omitempty" yaml:"kubernetesObjects,omitempty"`
	TrackedHostPathSet []syncPlanHostPathSet     `json:"trackedHostPathSets,omitempty" yaml:"trackedHostPathSets,omitempty"`
	Warnings           []string                  `json:"warnings,omitempty" yaml:"warnings,omitempty"`
}

type syncPlanComponent struct {
	Order       int                   `json:"order" yaml:"order"`
	Name        string                `json:"name" yaml:"name"`
	PackageName string                `json:"packageName" yaml:"packageName"`
	Version     string                `json:"version" yaml:"version"`
	Class       string                `json:"class" yaml:"class"`
	Safety      syncPlanPackageSafety `json:"safety" yaml:"safety"`
	Steps       []syncPlanStep        `json:"steps,omitempty" yaml:"steps,omitempty"`
}

type syncPlanStep struct {
	Order          int                    `json:"order" yaml:"order"`
	Name           string                 `json:"name" yaml:"name"`
	Kind           string                 `json:"kind" yaml:"kind"`
	ContentType    string                 `json:"contentType,omitempty" yaml:"contentType,omitempty"`
	HookPhase      string                 `json:"hookPhase,omitempty" yaml:"hookPhase,omitempty"`
	BundlePath     string                 `json:"bundlePath" yaml:"bundlePath"`
	SourcePath     string                 `json:"sourcePath" yaml:"sourcePath"`
	Required       bool                   `json:"required,omitempty" yaml:"required,omitempty"`
	TimeoutSeconds int32                  `json:"timeoutSeconds,omitempty" yaml:"timeoutSeconds,omitempty"`
	Target         syncPlanResolvedTarget `json:"target" yaml:"target"`
	Safety         syncPlanStepSafety     `json:"safety" yaml:"safety"`
}

type syncPlanResolvedTarget struct {
	Declared  string   `json:"declared,omitempty" yaml:"declared,omitempty"`
	Effective string   `json:"effective" yaml:"effective"`
	Scope     string   `json:"scope" yaml:"scope"`
	Hosts     []string `json:"hosts,omitempty" yaml:"hosts,omitempty"`
	Error     string   `json:"error,omitempty" yaml:"error,omitempty"`
}

type syncPlanPackageSafety struct {
	Profile          string   `json:"profile" yaml:"profile"`
	RolloutScope     string   `json:"rolloutScope" yaml:"rolloutScope"`
	Gates            []string `json:"gates,omitempty" yaml:"gates,omitempty"`
	Reasons          []string `json:"reasons,omitempty" yaml:"reasons,omitempty"`
	BarrierSteps     []string `json:"barrierSteps,omitempty" yaml:"barrierSteps,omitempty"`
	HostWaveSteps    []string `json:"hostWaveSteps,omitempty" yaml:"hostWaveSteps,omitempty"`
	HookPhases       []string `json:"hookPhases,omitempty" yaml:"hookPhases,omitempty"`
	PhaseProfiles    []string `json:"phaseProfiles,omitempty" yaml:"phaseProfiles,omitempty"`
	RequiresApproval bool     `json:"requiresApproval,omitempty" yaml:"requiresApproval,omitempty"`
	ManifestOnly     bool     `json:"manifestOnly,omitempty" yaml:"manifestOnly,omitempty"`
	HasRootfs        bool     `json:"hasRootfs,omitempty" yaml:"hasRootfs,omitempty"`
	HasHostFile      bool     `json:"hasHostFile,omitempty" yaml:"hasHostFile,omitempty"`
	HasLocalPatch    bool     `json:"hasLocalPatch,omitempty" yaml:"hasLocalPatch,omitempty"`
	HasPackageHook   bool     `json:"hasPackageHook,omitempty" yaml:"hasPackageHook,omitempty"`
}

type syncPlanStepSafety struct {
	Profile          string   `json:"profile" yaml:"profile"`
	Phase            string   `json:"phase,omitempty" yaml:"phase,omitempty"`
	RolloutScope     string   `json:"rolloutScope" yaml:"rolloutScope"`
	Gates            []string `json:"gates,omitempty" yaml:"gates,omitempty"`
	Reasons          []string `json:"reasons,omitempty" yaml:"reasons,omitempty"`
	RequiresApproval bool     `json:"requiresApproval,omitempty" yaml:"requiresApproval,omitempty"`
}

type syncPlanLocalResource struct {
	Path    string                 `json:"path" yaml:"path"`
	Target  syncPlanResolvedTarget `json:"target" yaml:"target"`
	Objects []syncPlanK8sObject    `json:"objects,omitempty" yaml:"objects,omitempty"`
}

type syncPlanK8sObject struct {
	APIVersion string `json:"apiVersion,omitempty" yaml:"apiVersion,omitempty"`
	Kind       string `json:"kind" yaml:"kind"`
	Namespace  string `json:"namespace,omitempty" yaml:"namespace,omitempty"`
	Name       string `json:"name" yaml:"name"`
	Path       string `json:"path" yaml:"path"`
	Component  string `json:"component,omitempty" yaml:"component,omitempty"`
	Source     string `json:"source" yaml:"source"`
	Ownership  string `json:"ownership" yaml:"ownership"`
	Sensitive  bool   `json:"sensitive,omitempty" yaml:"sensitive,omitempty"`
}

type syncPlanHostPathSet struct {
	Component       string   `json:"component,omitempty" yaml:"component,omitempty"`
	Source          string   `json:"source" yaml:"source"`
	Ownership       string   `json:"ownership" yaml:"ownership"`
	ProjectionClass string   `json:"projectionClass,omitempty" yaml:"projectionClass,omitempty"`
	CompareStrategy string   `json:"compareStrategy,omitempty" yaml:"compareStrategy,omitempty"`
	InputName       string   `json:"inputName,omitempty" yaml:"inputName,omitempty"`
	SafetyProfile   string   `json:"safetyProfile,omitempty" yaml:"safetyProfile,omitempty"`
	RolloutScope    string   `json:"rolloutScope,omitempty" yaml:"rolloutScope,omitempty"`
	Gates           []string `json:"gates,omitempty" yaml:"gates,omitempty"`
	Count           int      `json:"count" yaml:"count"`
	Hosts           []string `json:"hosts,omitempty" yaml:"hosts,omitempty"`
	Examples        []string `json:"examples,omitempty" yaml:"examples,omitempty"`
}

func newSyncPlanCmd() *cobra.Command {
	var flags struct {
		clusterName            string
		bundleDir              string
		allowStaleTopology     bool
		allowStaleRenderInputs bool
		output                 string
	}

	cmd := &cobra.Command{
		Use:          "plan",
		Short:        "Preview the static apply plan for a rendered desired-state bundle",
		Args:         cobra.NoArgs,
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			bundlePath := flags.bundleDir
			if bundlePath == "" {
				bundlePath = reconcile.CurrentBundlePath(flags.clusterName)
			}
			preflight, err := syncApplyPreflight(flags.clusterName, bundlePath, flags.allowStaleTopology, flags.allowStaleRenderInputs)
			if err != nil {
				return err
			}
			topology, err := syncExecutionTopologyForBundle(flags.clusterName, preflight.Bundle)
			if err != nil {
				return err
			}
			out := newSyncPlanOutput(flags.clusterName, bundlePath, preflight, topology)
			return writeSyncOutput(cmd, out, flags.output, "plan result")
		},
	}
	cmd.Flags().StringVarP(&flags.clusterName, "cluster", "c", "default", "name of cluster to plan desired state for")
	cmd.Flags().StringVar(&flags.bundleDir, "bundle-dir", "", "path to a rendered bundle directory; defaults to the cluster current bundle")
	cmd.Flags().BoolVar(&flags.allowStaleTopology, "allow-stale-topology", false, "show the plan with stale executionTopology treated as an allowed preflight condition")
	cmd.Flags().BoolVar(&flags.allowStaleRenderInputs, "allow-stale-render-inputs", false, "show the plan with stale render inputs treated as an allowed preflight condition")
	addSyncOutputFlag(cmd, &flags.output)
	return cmd
}

func newSyncPlanOutput(clusterName, bundlePath string, preflight syncApplyPreflightResult, topology *syncExecutionTopology) syncPlanOutput {
	bundle := preflight.Bundle
	executionTopology := hydrate.NewSingleNodeExecutionTopology()
	if topology != nil {
		executionTopology = topology.snapshot()
	}
	out := syncPlanOutput{
		ClusterName:       clusterName,
		BundlePath:        bundlePath,
		ExecutionTopology: executionTopology,
		Preflight:         newSyncPreflightOutput(clusterName, bundlePath, preflight),
		Warnings:          syncSourcePreflightBundleWarnings(bundle),
	}
	out.Summary.Blocked = preflight.Blocked
	if bundle == nil {
		return out
	}
	out.BOMName = bundle.Spec.BOMName
	out.Revision = bundle.Spec.Revision
	out.Channel = string(bundle.Spec.Channel)
	out.SourcePreflight = bundle.Spec.SourcePreflight
	out.Components = buildSyncPlanComponents(bundle, topology, &out.Summary)
	out.KubernetesObjects = buildSyncPlanK8sObjects(bundle.Spec.TrackedK8sObjects, &out.Summary)
	out.LocalResources = buildSyncPlanLocalResources(bundle.Spec.LocalResources, out.KubernetesObjects)
	out.TrackedHostPathSet = buildSyncPlanHostPathSets(bundle.Spec.TrackedHostPaths, topology)
	out.Summary.Components = len(out.Components)
	out.Summary.LocalResources = len(out.LocalResources)
	out.Summary.TrackedHostPaths = len(bundle.Spec.TrackedHostPaths)
	return out
}

func buildSyncPlanComponents(bundle *hydrate.Bundle, topology *syncExecutionTopology, summary *syncPlanSummary) []syncPlanComponent {
	components := make([]syncPlanComponent, 0, len(bundle.Spec.Components))
	for componentIndex, component := range bundle.Spec.Components {
		item := syncPlanComponent{
			Order:       componentIndex + 1,
			Name:        component.Name,
			PackageName: component.PackageName,
			Version:     component.Version,
			Class:       string(component.Class),
			Safety:      syncPlanPackageSafetyForComponent(component),
			Steps:       make([]syncPlanStep, 0, len(component.Steps)),
		}
		for stepIndex, step := range component.Steps {
			target := syncPlanTargetForStep(topology, step)
			planned := syncPlanStep{
				Order:          stepIndex + 1,
				Name:           step.Name,
				Kind:           string(step.Kind),
				ContentType:    string(step.ContentType),
				HookPhase:      string(step.HookPhase),
				BundlePath:     step.BundlePath,
				SourcePath:     step.SourcePath,
				Required:       step.Required,
				TimeoutSeconds: step.TimeoutSeconds,
				Target:         target,
				Safety:         syncPlanStepSafetyForStep(step, target),
			}
			item.Steps = append(item.Steps, planned)
			summary.Steps++
			switch step.Kind {
			case hydrate.StepContent:
				summary.ContentSteps++
			case hydrate.StepHook:
				summary.HookSteps++
			}
		}
		components = append(components, item)
	}
	return components
}

func syncPlanTargetForStep(topology *syncExecutionTopology, step hydrate.RenderedStep) syncPlanResolvedTarget {
	switch step.Kind {
	case hydrate.StepHook:
		return resolveSyncPlanTarget(topology, step.Target, step.Target)
	case hydrate.StepContent:
		switch step.ContentType {
		case packageformat.ContentRootfs, packageformat.ContentFile:
			return resolveSyncPlanTarget(topology, "", packageformat.TargetAllNodes)
		case packageformat.ContentManifest:
			return resolveSyncPlanTarget(topology, "", packageformat.TargetCluster)
		case packageformat.ContentValues:
			return syncPlanResolvedTarget{Effective: "renderOnly", Scope: "renderOnly"}
		default:
			return syncPlanResolvedTarget{Effective: string(step.ContentType), Scope: "unsupported", Error: fmt.Sprintf("unsupported content type %q", step.ContentType)}
		}
	default:
		return syncPlanResolvedTarget{Effective: string(step.Kind), Scope: "unsupported", Error: fmt.Sprintf("unsupported step kind %q", step.Kind)}
	}
}

func resolveSyncPlanTarget(topology *syncExecutionTopology, declared, effective packageformat.ExecutionTarget) syncPlanResolvedTarget {
	target := syncPlanResolvedTarget{
		Declared:  string(declared),
		Effective: string(effective),
	}
	switch effective {
	case packageformat.TargetCluster:
		target.Scope = "cluster"
	case packageformat.TargetAllNodes:
		target.Scope = "hosts"
		if topology == nil {
			target.Hosts = []string{syncLocalExecutionHost}
			return target
		}
		target.Hosts = topology.nodeExecutionHosts()
	case packageformat.TargetFirstMaster:
		target.Scope = "hosts"
		if topology == nil || strings.TrimSpace(topology.firstMaster) == "" {
			target.Error = "firstMaster is empty"
			return target
		}
		target.Hosts = []string{topology.firstMaster}
	default:
		target.Scope = "unknown"
		target.Error = fmt.Sprintf("unsupported target %q", effective)
	}
	return target
}

func syncPlanPackageSafetyForComponent(component hydrate.RenderedComponent) syncPlanPackageSafety {
	safety := syncPlanPackageSafety{
		Profile:      "package",
		RolloutScope: "mixed",
	}
	seenGates := map[string]struct{}{}
	seenReasons := map[string]struct{}{}
	seenPhases := map[string]struct{}{}
	seenProfiles := map[string]struct{}{}

	if len(component.LocalPatches) > 0 {
		safety.HasLocalPatch = true
		safety.RequiresApproval = true
		addUniqueString(&safety.Gates, seenGates, "localPatchPolicy", "approval")
		addUniqueString(&safety.Reasons, seenReasons, "local patches mutate package output and require policy approval")
	}

	for _, step := range component.Steps {
		target := syncPlanTargetForStep(nil, step)
		stepSafety := syncPlanStepSafetyForStep(step, target)
		addUniqueString(&safety.Gates, seenGates, stepSafety.Gates...)
		addUniqueString(&safety.Reasons, seenReasons, stepSafety.Reasons...)
		addUniqueString(&safety.PhaseProfiles, seenProfiles, stepSafety.Profile)
		if stepSafety.Phase != "" {
			addUniqueString(&safety.HookPhases, seenPhases, stepSafety.Phase)
		}
		if stepSafety.RequiresApproval {
			safety.RequiresApproval = true
		}

		switch stepSafety.RolloutScope {
		case "hostWave":
			safety.HostWaveSteps = append(safety.HostWaveSteps, step.Name)
		case "clusterBarrier", "renderOnly", "unsupported":
			safety.BarrierSteps = append(safety.BarrierSteps, step.Name)
		}

		switch {
		case step.Kind == hydrate.StepContent && step.ContentType == packageformat.ContentRootfs:
			safety.HasRootfs = true
		case step.Kind == hydrate.StepContent && step.ContentType == packageformat.ContentFile:
			safety.HasHostFile = true
		case step.Kind == hydrate.StepContent && step.ContentType == packageformat.ContentManifest:
			safety.ManifestOnly = true
		case step.Kind == hydrate.StepHook:
			safety.HasPackageHook = true
		}
	}

	if safety.ManifestOnly && !safety.HasRootfs && !safety.HasHostFile && len(safety.HostWaveSteps) == 0 {
		safety.Profile = "manifestOnly"
		safety.RolloutScope = "clusterBarrier"
		addUniqueString(&safety.Gates, seenGates, "kubeconfig", "apiReady", "applyManifest", "healthcheck")
		addUniqueString(&safety.Reasons, seenReasons, "package only changes Kubernetes API objects and must pass cluster-level health gates")
		return safety
	}
	if safety.HasRootfs {
		safety.Profile = "rootfsBootstrap"
		if len(safety.BarrierSteps) > 0 {
			safety.RolloutScope = "mixed"
		} else {
			safety.RolloutScope = "hostWave"
		}
		addUniqueString(&safety.Gates, seenGates, "preflight", "hostBatch", "serviceStop", "contentApply", "daemonReload", "healthcheck")
		addUniqueString(&safety.Reasons, seenReasons, "rootfs content mutates host filesystem and is protected by host batches")
		return safety
	}
	if safety.HasHostFile {
		safety.Profile = "hostFile"
		if len(safety.BarrierSteps) > 0 {
			safety.RolloutScope = "mixed"
		} else {
			safety.RolloutScope = "hostWave"
		}
		addUniqueString(&safety.Gates, seenGates, "preflight", "hostBatch", "contentApply", "daemonReload", "healthcheck")
		addUniqueString(&safety.Reasons, seenReasons, "host file content mutates host filesystem and is protected by host batches")
		return safety
	}
	if safety.HasPackageHook {
		safety.Profile = "packageHook"
		switch {
		case len(safety.HostWaveSteps) > 0 && len(safety.BarrierSteps) == 0:
			safety.RolloutScope = "hostWave"
		case len(safety.HostWaveSteps) == 0 && len(safety.BarrierSteps) > 0:
			safety.RolloutScope = "clusterBarrier"
		default:
			safety.RolloutScope = "mixed"
		}
		addUniqueString(&safety.Gates, seenGates, "hookTimeout", "targetResolution", "healthcheck")
		addUniqueString(&safety.Reasons, seenReasons, "package hooks execute package-specific logic and must declare explicit targets")
		return safety
	}
	return safety
}

func syncPlanStepSafetyForStep(step hydrate.RenderedStep, target syncPlanResolvedTarget) syncPlanStepSafety {
	safety := syncPlanStepSafety{
		Profile:      "generic",
		RolloutScope: target.Scope,
	}
	switch {
	case step.Kind == hydrate.StepContent && step.ContentType == packageformat.ContentRootfs:
		safety.Profile = "rootfsContent"
		safety.RolloutScope = "hostWave"
		safety.Gates = []string{"hostBatch", "serviceStop", "contentApply", "daemonReload"}
		safety.Reasons = []string{"rootfs content mutates host filesystem"}
	case step.Kind == hydrate.StepContent && step.ContentType == packageformat.ContentFile:
		safety.Profile = "hostFileContent"
		safety.RolloutScope = "hostWave"
		safety.Gates = []string{"hostBatch", "contentApply", "daemonReload"}
		safety.Reasons = []string{"file content mutates host filesystem"}
	case step.Kind == hydrate.StepContent && step.ContentType == packageformat.ContentManifest:
		safety.Profile = "manifestContent"
		safety.RolloutScope = "clusterBarrier"
		safety.Gates = []string{"kubeconfig", "apiReady", "applyManifest"}
		safety.Reasons = []string{"manifest content mutates Kubernetes API objects"}
	case step.Kind == hydrate.StepContent && step.ContentType == packageformat.ContentValues:
		safety.Profile = "renderOnly"
		safety.RolloutScope = "renderOnly"
		safety.Reasons = []string{"values content is consumed during render and is not applied directly"}
	case step.Kind == hydrate.StepContent && step.ContentType == packageformat.ContentPatch:
		safety.Profile = "localPatchContent"
		safety.RolloutScope = "clusterBarrier"
		safety.Gates = []string{"localPatchPolicy", "approval"}
		safety.Reasons = []string{"patch content changes rendered package output and requires policy approval before apply"}
		safety.RequiresApproval = true
	case step.Kind == hydrate.StepContent && step.ContentType == packageformat.ContentChart:
		safety.Profile = "chartContent"
		safety.RolloutScope = "clusterBarrier"
		safety.Gates = []string{"render", "kubeconfig", "apiReady", "healthcheck"}
		safety.Reasons = []string{"chart content renders Kubernetes API objects and must pass cluster-level health gates"}
	case step.Kind == hydrate.StepHook:
		safety = syncPlanHookSafety(step, target)
	default:
		safety.Profile = "unsupported"
		safety.RolloutScope = "unsupported"
		safety.Reasons = []string{"step kind or content type is not supported by sync apply"}
	}
	return safety
}

func syncPlanHookSafety(step hydrate.RenderedStep, target syncPlanResolvedTarget) syncPlanStepSafety {
	safety := syncPlanStepSafety{
		Profile:      "packageHook",
		Phase:        string(step.HookPhase),
		RolloutScope: "clusterBarrier",
		Gates:        []string{"hookTimeout", "targetResolution"},
		Reasons:      []string{"package hook executes package-specific logic"},
	}
	switch target.Scope {
	case "hosts":
		safety.RolloutScope = "hostWave"
		safety.Gates = append(safety.Gates, "hostBatch")
	case "cluster":
		safety.RolloutScope = "clusterBarrier"
		safety.Gates = append(safety.Gates, "kubeconfig", "apiReady")
	}
	switch step.HookPhase {
	case packageformat.PhaseBootstrap:
		safety.Profile = "bootstrapHook"
		safety.Gates = append(safety.Gates, "bootstrap", "preflight")
		safety.Reasons = append(safety.Reasons, "bootstrap hooks run before package install completion")
	case packageformat.PhaseConfigure:
		safety.Profile = "configureHook"
		safety.Gates = append(safety.Gates, "configurationDrift", "healthcheck")
		safety.Reasons = append(safety.Reasons, "configure hooks can mutate runtime configuration and must be followed by health gates")
	case packageformat.PhaseInstall:
		safety.Profile = "installHook"
		safety.Gates = append(safety.Gates, "install", "healthcheck")
		safety.Reasons = append(safety.Reasons, "install hooks can create runtime resources and must be followed by health gates")
	case packageformat.PhaseUpgrade:
		safety.Profile = "upgradeHook"
		safety.Gates = append(safety.Gates, "upgrade", "healthcheck", "rollbackPlan")
		safety.Reasons = append(safety.Reasons, "upgrade hooks can migrate live state and require rollback planning")
		safety.RequiresApproval = true
	case packageformat.PhaseRemove:
		safety.Profile = "removeHook"
		safety.RolloutScope = "clusterBarrier"
		safety.Gates = append(safety.Gates, "destructiveAction", "approval", "rollbackPlan")
		safety.Reasons = append(safety.Reasons, "remove hooks can delete runtime resources and require explicit approval")
		safety.RequiresApproval = true
	case packageformat.PhaseHealth:
		safety.Profile = "healthHook"
		safety.Gates = append(safety.Gates, "healthcheck")
		safety.Reasons = append(safety.Reasons, "health hooks gate package readiness")
	}
	return safety
}

func addUniqueString(out *[]string, seen map[string]struct{}, values ...string) {
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		if _, ok := seen[value]; ok {
			continue
		}
		seen[value] = struct{}{}
		*out = append(*out, value)
	}
}

func buildSyncPlanK8sObjects(objects []hydrate.TrackedK8sObject, summary *syncPlanSummary) []syncPlanK8sObject {
	out := make([]syncPlanK8sObject, 0, len(objects))
	for _, object := range objects {
		item := syncPlanK8sObject{
			APIVersion: object.APIVersion,
			Kind:       object.Kind,
			Namespace:  object.Namespace,
			Name:       object.Name,
			Path:       object.Path,
			Component:  object.Component,
			Source:     string(object.Source),
			Ownership:  string(object.Ownership),
			Sensitive:  strings.EqualFold(object.Kind, "Secret"),
		}
		if item.Sensitive {
			summary.SecretObjects++
		}
		out = append(out, item)
	}
	sort.Slice(out, func(i, j int) bool {
		return syncPlanK8sObjectSortKey(out[i]) < syncPlanK8sObjectSortKey(out[j])
	})
	summary.KubernetesObjects = len(out)
	return out
}

func buildSyncPlanLocalResources(paths []string, objects []syncPlanK8sObject) []syncPlanLocalResource {
	if len(paths) == 0 {
		return nil
	}
	objectsByPath := make(map[string][]syncPlanK8sObject, len(objects))
	for _, object := range objects {
		if object.Source != string(hydrate.InventorySourceLocalResource) {
			continue
		}
		objectsByPath[object.Path] = append(objectsByPath[object.Path], object)
	}
	out := make([]syncPlanLocalResource, 0, len(paths))
	for _, path := range paths {
		out = append(out, syncPlanLocalResource{
			Path:    path,
			Target:  resolveSyncPlanTarget(nil, "", packageformat.TargetCluster),
			Objects: objectsByPath[path],
		})
	}
	sort.Slice(out, func(i, j int) bool {
		return out[i].Path < out[j].Path
	})
	return out
}

func buildSyncPlanHostPathSets(paths []hydrate.TrackedHostPath, topology *syncExecutionTopology) []syncPlanHostPathSet {
	if len(paths) == 0 {
		return nil
	}
	groups := make(map[string]*syncPlanHostPathSet)
	for _, path := range paths {
		hosts := syncPlanHostsForTrackedHostPath(topology, path)
		key := strings.Join([]string{
			path.Component,
			string(path.Source),
			string(path.Ownership),
			string(path.ProjectionClass),
			string(path.CompareStrategy),
			path.InputName,
			strings.Join(hosts, ","),
		}, "\x00")
		group, ok := groups[key]
		if !ok {
			group = &syncPlanHostPathSet{
				Component:       path.Component,
				Source:          string(path.Source),
				Ownership:       string(path.Ownership),
				ProjectionClass: string(path.ProjectionClass),
				CompareStrategy: string(path.CompareStrategy),
				InputName:       path.InputName,
				SafetyProfile:   syncPlanHostPathSafetyProfile(path),
				RolloutScope:    syncPlanHostPathRolloutScope(path),
				Gates:           syncPlanHostPathGates(path),
				Hosts:           hosts,
			}
			groups[key] = group
		}
		group.Count++
		if len(group.Examples) < 5 {
			group.Examples = append(group.Examples, path.HostPath)
		}
	}
	out := make([]syncPlanHostPathSet, 0, len(groups))
	for _, group := range groups {
		out = append(out, *group)
	}
	sort.Slice(out, func(i, j int) bool {
		return syncPlanHostPathSetSortKey(out[i]) < syncPlanHostPathSetSortKey(out[j])
	})
	return out
}

func syncPlanHostsForTrackedHostPath(topology *syncExecutionTopology, path hydrate.TrackedHostPath) []string {
	if topology == nil {
		return []string{syncLocalExecutionHost}
	}
	var hosts []string
	for _, host := range topology.nodeExecutionHosts() {
		if syncTrackedHostPathAppliesToHost(topology, host, path) {
			hosts = append(hosts, host)
		}
	}
	return hosts
}

func syncPlanHostPathSafetyProfile(path hydrate.TrackedHostPath) string {
	switch path.ProjectionClass {
	case hydrate.HostPathProjectionClassGenerated:
		return "generatedHostProjection"
	case hydrate.HostPathProjectionClassDirect:
		return "directHostPath"
	default:
		return "hostPath"
	}
}

func syncPlanHostPathRolloutScope(path hydrate.TrackedHostPath) string {
	switch path.ProjectionClass {
	case hydrate.HostPathProjectionClassGenerated:
		return "controlPlaneBarrier"
	default:
		return "hostWave"
	}
}

func syncPlanHostPathGates(path hydrate.TrackedHostPath) []string {
	switch path.ProjectionClass {
	case hydrate.HostPathProjectionClassGenerated:
		return []string{"semanticCompare", "bootstrapInputReview", "controlPlaneHealth"}
	case hydrate.HostPathProjectionClassDirect:
		return []string{"hostBatch", "contentApply", "daemonReload"}
	default:
		return []string{"hostBatch", "fileCompare"}
	}
}

func syncPlanK8sObjectSortKey(object syncPlanK8sObject) string {
	return strings.Join([]string{object.Path, object.Namespace, object.Kind, object.Name}, "\x00")
}

func syncPlanHostPathSetSortKey(set syncPlanHostPathSet) string {
	return strings.Join([]string{set.Component, set.Source, set.Ownership, set.ProjectionClass, set.CompareStrategy, set.InputName, strings.Join(set.Hosts, ",")}, "\x00")
}
