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

package reconcile

import (
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/opencontainers/go-digest"

	"github.com/labring/sealos/pkg/distribution"
	"github.com/labring/sealos/pkg/distribution/hydrate"
	"github.com/labring/sealos/pkg/distribution/packageformat"
	"github.com/labring/sealos/pkg/distribution/state"
	"github.com/labring/sealos/pkg/utils/iputils"
	yamlutil "github.com/labring/sealos/pkg/utils/yaml"
)

const defaultApplyWaitTimeout = 180 * time.Second

type ApplyOptions struct {
	ClusterName    string
	BundlePath     string
	KubeconfigPath string
	HostRoot       string
	Stderr         io.Writer
	WaitTimeout    time.Duration
	Rollout        RolloutStrategy
}

type ApplyResult struct {
	Bundle             *hydrate.Bundle
	BundlePath         string
	DesiredStateDigest string
	AppliedRevision    *state.AppliedRevision
}

type RolloutStrategy struct {
	BatchSize     int                  `json:"batchSize,omitempty" yaml:"batchSize,omitempty"`
	Canary        RolloutCanary        `json:"canary,omitempty" yaml:"canary,omitempty"`
	Pause         RolloutPause         `json:"pause,omitempty" yaml:"pause,omitempty"`
	HealthGate    bool                 `json:"healthGate,omitempty" yaml:"healthGate,omitempty"`
	FailureAction RolloutFailureAction `json:"failureAction,omitempty" yaml:"failureAction,omitempty"`
}

type RolloutCanary struct {
	BatchSize int `json:"batchSize,omitempty" yaml:"batchSize,omitempty"`
}

type RolloutPause struct {
	AfterCanary bool `json:"afterCanary,omitempty" yaml:"afterCanary,omitempty"`
}

type RolloutFailureAction string

const (
	RolloutFailureActionStop     RolloutFailureAction = "Stop"
	RolloutFailureActionRollback RolloutFailureAction = "Rollback"
)

type rolloutPausedError struct {
	message string
}

type rolloutRollbackError struct {
	cause error
}

func (e rolloutPausedError) Error() string {
	return e.message
}

func (e rolloutPausedError) Is(target error) bool {
	_, ok := target.(rolloutPausedError)
	return ok
}

func (e rolloutRollbackError) Error() string {
	return fmt.Sprintf("rollout failed and rolled back to last successful revision: %v", e.cause)
}

func (e rolloutRollbackError) Unwrap() error {
	return e.cause
}

func (e rolloutRollbackError) Is(target error) bool {
	_, ok := target.(rolloutRollbackError)
	return ok
}

func IsRolloutPaused(err error) bool {
	return errors.Is(err, rolloutPausedError{})
}

func NewRolloutPausedError(message string) error {
	return rolloutPausedError{message: message}
}

func IsRolloutRolledBack(err error) bool {
	return errors.Is(err, rolloutRollbackError{})
}

func NewRolloutRolledBackError(cause error) error {
	return rolloutRollbackError{cause: cause}
}

func (s RolloutStrategy) Validate() error {
	if s.BatchSize < 0 {
		return fmt.Errorf("rollout.batchSize cannot be negative")
	}
	if s.Canary.BatchSize < 0 {
		return fmt.Errorf("rollout.canary.batchSize cannot be negative")
	}
	switch s.FailureAction {
	case "", RolloutFailureActionStop, RolloutFailureActionRollback:
	default:
		return fmt.Errorf("rollout.failureAction must be %q, %q, or empty", RolloutFailureActionStop, RolloutFailureActionRollback)
	}
	return nil
}

type bundleExecutor struct {
	bundle                *hydrate.Bundle
	bundlePath            string
	clusterName           string
	desiredStateDigest    string
	kubeconfigPath        string
	hostRoot              string
	stderr                io.Writer
	waitTimeout           time.Duration
	rollout               RolloutStrategy
	topology              *clusterExecutionTopology
	remoteExec            applyRemoteExecutor
	stagedBundleRoots     map[string]string
	localResourcesApplied bool
	untaintAttempted      bool
}

func Apply(opts ApplyOptions) (*ApplyResult, error) {
	if strings.TrimSpace(opts.ClusterName) == "" {
		return nil, fmt.Errorf("cluster name cannot be empty")
	}
	if strings.TrimSpace(opts.BundlePath) == "" {
		return nil, fmt.Errorf("bundle path cannot be empty")
	}
	if err := opts.Rollout.Validate(); err != nil {
		return nil, err
	}

	bundlePath, err := filepath.Abs(opts.BundlePath)
	if err != nil {
		return nil, fmt.Errorf("resolve bundle path %q: %w", opts.BundlePath, err)
	}

	bundle, err := LoadBundle(bundlePath)
	if err != nil {
		return nil, err
	}

	desiredStateDigest, err := hydrate.DigestBundle(bundlePath)
	if err != nil {
		return nil, fmt.Errorf("digest bundle %q: %w", bundlePath, err)
	}

	if _, err := loadRenderedRevision(opts.ClusterName, desiredStateDigest); err != nil {
		return nil, err
	}
	topology, err := applyTopologyForBundle(opts.ClusterName, bundle)
	if err != nil {
		return nil, err
	}
	if err := validatePreparedHostBundle(bundlePath, bundle); err != nil {
		return nil, err
	}
	executor := bundleExecutor{
		bundle:             bundle,
		bundlePath:         bundlePath,
		clusterName:        opts.ClusterName,
		desiredStateDigest: desiredStateDigest.String(),
		kubeconfigPath:     opts.KubeconfigPath,
		hostRoot:           opts.HostRoot,
		stderr:             opts.Stderr,
		waitTimeout:        opts.WaitTimeout,
		rollout:            opts.Rollout,
		topology:           topology,
	}
	executor.applyDefaults()
	if err := executor.configureRemoteExecutor(); err != nil {
		return nil, err
	}

	if err := executor.applyBundle(bundle); err != nil {
		if opts.Rollout.FailureAction == RolloutFailureActionRollback && !errors.Is(err, rolloutPausedError{}) {
			if rollbackErr := executor.rollbackToLastSuccessfulRevision(); rollbackErr != nil {
				return nil, errors.Join(err, rollbackErr)
			}
			appliedRevision, stateErr := state.LoadAppliedRevision(opts.ClusterName)
			if stateErr != nil {
				return nil, errors.Join(rolloutRollbackError{cause: err}, stateErr)
			}
			rollbackBundle, bundleErr := LoadBundle(CurrentBundlePath(opts.ClusterName))
			if bundleErr != nil {
				return nil, errors.Join(rolloutRollbackError{cause: err}, bundleErr)
			}
			return &ApplyResult{
				Bundle:             rollbackBundle,
				BundlePath:         CurrentBundlePath(opts.ClusterName),
				DesiredStateDigest: appliedRevision.Spec.DesiredStateDigest,
				AppliedRevision:    appliedRevision,
			}, rolloutRollbackError{cause: err}
		}
		return nil, err
	}

	appliedRevision, err := state.MarkSuccessfulApply(opts.ClusterName)
	if err != nil {
		return nil, err
	}

	return &ApplyResult{
		Bundle:             bundle,
		BundlePath:         bundlePath,
		DesiredStateDigest: desiredStateDigest.String(),
		AppliedRevision:    appliedRevision,
	}, nil
}

func LoadBundle(bundlePath string) (*hydrate.Bundle, error) {
	manifestPath := filepath.Join(bundlePath, hydrate.BundleFileName)

	var bundle hydrate.Bundle
	if err := yamlutil.UnmarshalFile(manifestPath, &bundle); err != nil {
		return nil, fmt.Errorf("load bundle manifest %q: %w", manifestPath, err)
	}
	if bundle.APIVersion != distribution.APIVersion {
		return nil, fmt.Errorf("unsupported bundle apiVersion %q", bundle.APIVersion)
	}
	if bundle.Kind != distribution.KindHydratedBundle {
		return nil, fmt.Errorf("unsupported bundle kind %q", bundle.Kind)
	}
	if bundle.Spec.BOMName == "" {
		return nil, fmt.Errorf("bundle spec.bomName cannot be empty")
	}
	if bundle.Spec.Revision == "" {
		return nil, fmt.Errorf("bundle spec.revision cannot be empty")
	}
	if len(bundle.Spec.Components) == 0 {
		return nil, fmt.Errorf("bundle spec.components cannot be empty")
	}
	return &bundle, nil
}

func applyTopologyForBundle(clusterName string, bundle *hydrate.Bundle) (*clusterExecutionTopology, error) {
	if bundle == nil {
		return nil, fmt.Errorf("bundle cannot be nil")
	}
	topology, hasSnapshot, err := applyExecutionTopologyFromSnapshot(clusterName, bundle.Spec.ExecutionTopology)
	if err != nil {
		return nil, err
	}
	if hasSnapshot {
		return topology, nil
	}
	return loadApplyExecutionTopology(clusterName)
}

func loadRenderedRevision(clusterName string, desiredStateDigest digest.Digest) (*state.AppliedRevision, error) {
	doc, err := state.LoadAppliedRevision(clusterName)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, fmt.Errorf("rendered desired state for cluster %q not found: run sync render first", clusterName)
		}
		return nil, err
	}
	if doc.Spec.DesiredStateDigest != desiredStateDigest.String() {
		return nil, fmt.Errorf(
			"rendered desired state digest mismatch for cluster %q: state has %q, bundle has %q",
			clusterName,
			doc.Spec.DesiredStateDigest,
			desiredStateDigest.String(),
		)
	}
	return doc, nil
}

func (e *bundleExecutor) applyDefaults() {
	if e.kubeconfigPath == "" {
		e.kubeconfigPath = "/etc/kubernetes/admin.conf"
	}
	if e.hostRoot == "" {
		e.hostRoot = string(os.PathSeparator)
	}
	if e.stderr == nil {
		e.stderr = io.Discard
	}
	if e.waitTimeout <= 0 {
		e.waitTimeout = defaultApplyWaitTimeout
	}
	if e.topology == nil {
		e.topology = fallbackLocalExecutionTopology(e.clusterName)
	}
	if e.stagedBundleRoots == nil {
		e.stagedBundleRoots = make(map[string]string)
	}
}

func (e *bundleExecutor) configureRemoteExecutor() error {
	e.remoteExec = nil
	if e.topology == nil || !e.topology.hasRemoteHosts() {
		return nil
	}
	remoteTopology, err := remoteExecutionTopologyForApply(e.clusterName, e.topology)
	if err != nil {
		return err
	}
	remoteExec, err := newApplyRemoteExecutor(remoteTopology)
	if err != nil {
		return fmt.Errorf("create remote execution client: %w", err)
	}
	e.remoteExec = remoteExec
	return nil
}

func (e *bundleExecutor) applyBundle(bundle *hydrate.Bundle) error {
	if bundle == nil {
		return fmt.Errorf("bundle cannot be nil")
	}
	if err := validateSingleNodeBundle(bundle); err != nil {
		return err
	}
	if err := e.ensurePrivileges(bundle); err != nil {
		return err
	}

	for _, component := range bundle.Spec.Components {
		if err := e.applyComponent(bundle, component); err != nil {
			return fmt.Errorf("apply component %q: %w", component.Name, err)
		}
	}
	return nil
}

func (e *bundleExecutor) rollbackToLastSuccessfulRevision() error {
	applied, err := state.LoadAppliedRevision(e.clusterName)
	if err != nil {
		return fmt.Errorf("load applied revision for rollback: %w", err)
	}
	if applied.Status.LastSuccessfulRevision == nil {
		return fmt.Errorf("rollback requested but no last successful revision is recorded")
	}

	lastSuccessful := applied.Status.LastSuccessfulRevision
	rollbackBundlePath, err := RevisionBundlePath(e.clusterName, lastSuccessful.DesiredStateDigest)
	if err != nil {
		return err
	}
	rollbackBundle, err := LoadBundle(rollbackBundlePath)
	if err != nil {
		return fmt.Errorf("load rollback bundle %q: %w", rollbackBundlePath, err)
	}
	rollbackTopology, err := applyTopologyForBundle(e.clusterName, rollbackBundle)
	if err != nil {
		return fmt.Errorf("resolve rollback bundle topology: %w", err)
	}

	e.logf("rolling back to last successful revision %q (%s)", lastSuccessful.BOM.Revision, lastSuccessful.DesiredStateDigest)
	rollbackExecutor := *e
	rollbackExecutor.bundle = rollbackBundle
	rollbackExecutor.bundlePath = rollbackBundlePath
	rollbackExecutor.desiredStateDigest = lastSuccessful.DesiredStateDigest
	rollbackExecutor.topology = rollbackTopology
	rollbackExecutor.stagedBundleRoots = make(map[string]string)
	rollbackExecutor.localResourcesApplied = false
	rollbackExecutor.untaintAttempted = false
	rollbackExecutor.rollout = RolloutStrategy{
		BatchSize:  e.rollout.BatchSize,
		HealthGate: e.rollout.HealthGate,
	}
	rollbackExecutor.applyDefaults()
	if err := rollbackExecutor.configureRemoteExecutor(); err != nil {
		return err
	}
	if err := rollbackExecutor.applyBundle(rollbackBundle); err != nil {
		return fmt.Errorf("rollback to last successful revision failed: %w", err)
	}

	restored := *applied
	restored.Spec.BOM = lastSuccessful.BOM
	restored.Spec.LocalRepoRevision = lastSuccessful.LocalRepoRevision
	restored.Spec.LocalPatchRevision = lastSuccessful.LocalPatchRevision
	restored.Spec.DesiredStateDigest = lastSuccessful.DesiredStateDigest
	restored.Status.State = state.StateDirty
	if err := mirrorBundle(rollbackBundlePath, CurrentBundlePath(e.clusterName)); err != nil {
		return fmt.Errorf("restore current bundle after rollback: %w", err)
	}
	if err := state.SaveAppliedRevision(&restored); err != nil {
		return fmt.Errorf("record rollback target: %w", err)
	}
	if _, err := state.MarkSuccessfulApply(e.clusterName); err != nil {
		return fmt.Errorf("mark rollback apply successful: %w", err)
	}
	return nil
}

func (e *bundleExecutor) ensurePrivileges(bundle *hydrate.Bundle) error {
	if bundle == nil {
		return fmt.Errorf("bundle cannot be nil")
	}
	hasHostMutatingContent := false
	for _, component := range bundle.Spec.Components {
		for _, step := range component.Steps {
			if step.Kind != hydrate.StepContent {
				continue
			}
			switch step.ContentType {
			case packageformat.ContentRootfs, packageformat.ContentFile:
				hasHostMutatingContent = true
			}
		}
	}

	if e.hostRoot != string(os.PathSeparator) {
		if hasHostMutatingContent && e.topology != nil && !e.topology.isSingleNode() {
			return fmt.Errorf("custom host root %q is only supported for single-node sync apply", e.hostRoot)
		}
		return nil
	}
	if e.topology != nil && !e.topology.hasLocalExecutionHost() {
		return nil
	}

	for _, component := range bundle.Spec.Components {
		for _, step := range component.Steps {
			if step.Kind != hydrate.StepContent {
				continue
			}
			switch step.ContentType {
			case packageformat.ContentRootfs, packageformat.ContentFile:
				if os.Geteuid() != 0 {
					return fmt.Errorf("sync apply must run as root when mutating the host")
				}
				return nil
			}
		}
	}
	return nil
}

func (e *bundleExecutor) applyComponent(bundle *hydrate.Bundle, component hydrate.RenderedComponent) error {
	files, hooks, err := classifyComponent(component)
	if err != nil {
		return err
	}

	mode, err := determineSingleNodeComponentMode(component, files, hooks)
	if err != nil {
		return err
	}

	switch mode {
	case componentModeRuntimeRootfs:
		return e.applyRuntimeRootfsComponent(component, files, hooks)
	case componentModeBootstrapRootfs:
		return e.applyBootstrapRootfsComponent(bundle, component, files, hooks)
	case componentModeClusterApplication:
		return e.applyClusterApplicationComponent(bundle, component, files, hooks)
	default:
		return fmt.Errorf("component %q uses unsupported single-node apply mode", component.Name)
	}
}

type contentSet struct {
	rootfs    []hydrate.RenderedStep
	files     []hydrate.RenderedStep
	manifests []hydrate.RenderedStep
	hasSysctl bool
}

type hookSet struct {
	preflight   []hydrate.RenderedStep
	bootstrap   []hydrate.RenderedStep
	configure   []hydrate.RenderedStep
	install     []hydrate.RenderedStep
	healthcheck []hydrate.RenderedStep
}

type componentMode string

const (
	componentModeRuntimeRootfs      componentMode = "runtime-rootfs"
	componentModeBootstrapRootfs    componentMode = "bootstrap-rootfs"
	componentModeClusterApplication componentMode = "cluster-application"
)

func classifyComponent(component hydrate.RenderedComponent) (contentSet, hookSet, error) {
	var contents contentSet
	var hooks hookSet

	for _, step := range component.Steps {
		switch step.Kind {
		case hydrate.StepContent:
			switch step.ContentType {
			case packageformat.ContentRootfs:
				contents.rootfs = append(contents.rootfs, step)
			case packageformat.ContentFile:
				contents.files = append(contents.files, step)
				if hostRel, ok := trimStepPrefix(step.SourcePath, "files/"); ok &&
					strings.HasPrefix(hostRel, "etc/sysctl.d/") {
					contents.hasSysctl = true
				}
			case packageformat.ContentManifest:
				contents.manifests = append(contents.manifests, step)
			case packageformat.ContentValues:
				continue
			default:
				return contentSet{}, hookSet{}, fmt.Errorf(
					"component %q step %q uses unsupported content type %q",
					component.Name,
					step.Name,
					step.ContentType,
				)
			}
		case hydrate.StepHook:
			if strings.EqualFold(step.Name, "preflight") {
				if err := validateSingleNodeTarget(component.Name, step); err != nil {
					return contentSet{}, hookSet{}, err
				}
				hooks.preflight = append(hooks.preflight, step)
				continue
			}
			if err := validateSingleNodeTarget(component.Name, step); err != nil {
				return contentSet{}, hookSet{}, err
			}
			switch step.HookPhase {
			case packageformat.PhaseBootstrap:
				hooks.bootstrap = append(hooks.bootstrap, step)
			case packageformat.PhaseConfigure:
				hooks.configure = append(hooks.configure, step)
			case packageformat.PhaseInstall:
				hooks.install = append(hooks.install, step)
			case packageformat.PhaseHealth:
				hooks.healthcheck = append(hooks.healthcheck, step)
			default:
				return contentSet{}, hookSet{}, fmt.Errorf(
					"component %q hook %q uses unsupported phase %q",
					component.Name,
					step.Name,
					step.HookPhase,
				)
			}
		default:
			return contentSet{}, hookSet{}, fmt.Errorf(
				"component %q step %q uses unsupported kind %q",
				component.Name,
				step.Name,
				step.Kind,
			)
		}
	}
	return contents, hooks, nil
}

func validateSingleNodeBundle(bundle *hydrate.Bundle) error {
	if bundle == nil {
		return fmt.Errorf("bundle cannot be nil")
	}

	for _, component := range bundle.Spec.Components {
		files, hooks, err := classifyComponent(component)
		if err != nil {
			return err
		}
		if _, err := determineSingleNodeComponentMode(component, files, hooks); err != nil {
			return err
		}
	}
	return nil
}

func validatePreparedHostBundle(bundlePath string, bundle *hydrate.Bundle) error {
	if err := validateSingleNodeBundle(bundle); err != nil {
		return err
	}

	for _, component := range bundle.Spec.Components {
		files, hooks, err := classifyComponent(component)
		if err != nil {
			return err
		}
		mode, err := determineSingleNodeComponentMode(component, files, hooks)
		if err != nil {
			return err
		}

		if err := validateHookPayloads(bundlePath, component, hooks); err != nil {
			return err
		}

		switch {
		case component.PackageName == "containerd-runtime" || component.Name == "containerd":
			if err := validateRequiredRootfsFiles(bundlePath, component, files.rootfs, []string{
				filepath.Join("usr", "bin", "containerd"),
				filepath.Join("usr", "bin", "ctr"),
				filepath.Join("usr", "bin", "containerd-shim-runc-v2"),
				filepath.Join("usr", "bin", "runc"),
			}); err != nil {
				return err
			}
		case component.PackageName == "kubernetes-rootfs" || component.Name == "kubernetes":
			if err := validateRequiredRootfsFiles(bundlePath, component, files.rootfs, []string{
				filepath.Join("usr", "bin", "kubeadm"),
				filepath.Join("usr", "bin", "kubelet"),
				filepath.Join("usr", "bin", "kubectl"),
			}); err != nil {
				return err
			}
		case mode == componentModeClusterApplication && (component.PackageName == "cilium-cni" || component.Name == "cilium"):
			if err := validateManifestKinds(bundlePath, component, files.manifests, "DaemonSet", "Deployment"); err != nil {
				return err
			}
		}
	}
	return nil
}

func validateHookPayloads(bundlePath string, component hydrate.RenderedComponent, hooks hookSet) error {
	for _, hook := range append(append(append(append(
		slices.Clone(hooks.preflight),
		hooks.bootstrap...),
		hooks.configure...),
		hooks.install...),
		hooks.healthcheck...) {
		hookPath, err := resolveBundleRelativePath(bundlePath, hook.BundlePath)
		if err != nil {
			return fmt.Errorf("component %q hook %q: %w", component.Name, hook.Name, err)
		}
		data, err := os.ReadFile(hookPath)
		if err != nil {
			return fmt.Errorf("component %q hook %q: read %q: %w", component.Name, hook.Name, hookPath, err)
		}
		if strings.Contains(strings.ToLower(string(data)), "placeholder") {
			return fmt.Errorf("component %q hook %q still contains placeholder content", component.Name, hook.Name)
		}
	}
	return nil
}

func validateRequiredRootfsFiles(bundlePath string, component hydrate.RenderedComponent, steps []hydrate.RenderedStep, required []string) error {
	var missing []string
	for _, relPath := range required {
		found := false
		for _, step := range steps {
			stepPath, err := resolveBundleRelativePath(bundlePath, step.BundlePath)
			if err != nil {
				return fmt.Errorf("component %q rootfs step %q: %w", component.Name, step.Name, err)
			}
			if _, err := os.Stat(filepath.Join(stepPath, relPath)); err == nil {
				found = true
				break
			}
		}
		if !found {
			missing = append(missing, filepath.ToSlash(relPath))
		}
	}
	if len(missing) > 0 {
		return fmt.Errorf(
			"component %q bundle is missing required staged payloads: %s",
			component.Name,
			strings.Join(missing, ", "),
		)
	}
	return nil
}

func validateManifestKinds(bundlePath string, component hydrate.RenderedComponent, steps []hydrate.RenderedStep, kinds ...string) error {
	found := make(map[string]bool, len(kinds))
	for _, kind := range kinds {
		found[kind] = false
	}

	for _, step := range steps {
		stepPath, err := resolveBundleRelativePath(bundlePath, step.BundlePath)
		if err != nil {
			return fmt.Errorf("component %q manifest step %q: %w", component.Name, step.Name, err)
		}

		info, err := os.Stat(stepPath)
		if err != nil {
			return fmt.Errorf("component %q manifest step %q: stat %q: %w", component.Name, step.Name, stepPath, err)
		}

		if info.IsDir() {
			if err := filepath.WalkDir(stepPath, func(path string, d fs.DirEntry, walkErr error) error {
				if walkErr != nil {
					return walkErr
				}
				if d.IsDir() {
					return nil
				}
				return markManifestKinds(path, found)
			}); err != nil {
				return fmt.Errorf("component %q manifest step %q: %w", component.Name, step.Name, err)
			}
			continue
		}

		if err := markManifestKinds(stepPath, found); err != nil {
			return fmt.Errorf("component %q manifest step %q: %w", component.Name, step.Name, err)
		}
	}

	var missing []string
	for _, kind := range kinds {
		if !found[kind] {
			missing = append(missing, kind)
		}
	}
	if len(missing) > 0 {
		return fmt.Errorf(
			"component %q bundle is missing required manifest kinds: %s",
			component.Name,
			strings.Join(missing, ", "),
		)
	}
	return nil
}

func markManifestKinds(path string, found map[string]bool) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	text := string(data)
	for kind := range found {
		if strings.Contains(text, "\nkind: "+kind+"\n") ||
			strings.HasPrefix(text, "kind: "+kind+"\n") ||
			strings.Contains(text, "\nkind: "+kind+"\r\n") ||
			strings.HasPrefix(text, "kind: "+kind+"\r\n") {
			found[kind] = true
		}
	}
	return nil
}

func determineSingleNodeComponentMode(component hydrate.RenderedComponent, files contentSet, hooks hookSet) (componentMode, error) {
	switch {
	case len(files.rootfs) > 0 && (files.hasSysctl || len(files.manifests) > 0):
		return componentModeBootstrapRootfs, nil
	case len(files.rootfs) > 0:
		return componentModeRuntimeRootfs, nil
	case len(files.rootfs) == 0 && len(files.files) == 0 && len(files.manifests) > 0:
		if len(hooks.bootstrap) > 0 {
			return "", fmt.Errorf("component %q uses bootstrap hooks without rootfs content, unsupported in single-node apply", component.Name)
		}
		return componentModeClusterApplication, nil
	default:
		return "", fmt.Errorf("component %q uses unsupported content layout for single-node apply", component.Name)
	}
}

func validateSingleNodeTarget(componentName string, step hydrate.RenderedStep) error {
	switch step.Target {
	case packageformat.TargetAllNodes, packageformat.TargetFirstMaster, packageformat.TargetCluster:
		return nil
	default:
		return fmt.Errorf(
			"component %q hook %q uses unsupported target %q for single-node apply",
			componentName,
			step.Name,
			step.Target,
		)
	}
}

func servicesForComponent(contents contentSet, bundlePath string) []string {
	serviceSet := map[string]struct{}{}
	for _, step := range contents.rootfs {
		resolvedPath := filepath.Join(bundlePath, filepath.FromSlash(step.BundlePath))
		for _, candidate := range []struct {
			relative string
			service  string
		}{
			{relative: filepath.Join("usr", "bin", "containerd"), service: "containerd"},
			{relative: filepath.Join("usr", "bin", "kubelet"), service: "kubelet"},
		} {
			if _, err := os.Stat(filepath.Join(resolvedPath, candidate.relative)); err == nil {
				serviceSet[candidate.service] = struct{}{}
			}
		}
	}

	services := make([]string, 0, len(serviceSet))
	for service := range serviceSet {
		services = append(services, service)
	}
	slices.Sort(services)
	return services
}

func (e *bundleExecutor) applyRuntimeRootfsComponent(component hydrate.RenderedComponent, files contentSet, hooks hookSet) error {
	nodeHosts := e.nodeExecutionHosts()
	provisioningHosts, err := e.provisioningHostsForComponent(component, nodeHosts)
	if err != nil {
		return err
	}
	if !supportsRuntimeRootfsRolloutBatches(files, hooks) {
		if err := e.applyRuntimeRootfsHostBatch(component, files, hooks, provisioningHosts); err != nil {
			return err
		}
		return e.runHooks(component, hooks.healthcheck)
	}
	if e.rollout.HealthGate {
		if len(provisioningHosts) == 0 {
			return e.runHooks(component, hooks.healthcheck)
		}
		return e.forEachHostBatch(provisioningHosts, func(batch []string) error {
			if err := e.applyRuntimeRootfsHostBatch(component, files, hooks, batch); err != nil {
				return err
			}
			return e.runHooksForHosts(component, hooks.healthcheck, batch)
		})
	}
	if err := e.forEachHostBatch(provisioningHosts, func(batch []string) error {
		return e.applyRuntimeRootfsHostBatch(component, files, hooks, batch)
	}); err != nil {
		return err
	}
	return e.runHooks(component, hooks.healthcheck)
}

func supportsRuntimeRootfsRolloutBatches(files contentSet, hooks hookSet) bool {
	if _, ok := kubeadmBootstrapConfigStep(files); ok {
		return false
	}
	for _, hook := range append(append(append(
		slices.Clone(hooks.preflight),
		hooks.bootstrap...),
		hooks.configure...),
		hooks.install...) {
		if hook.Target != packageformat.TargetAllNodes {
			return false
		}
	}
	return true
}

func (e *bundleExecutor) applyRuntimeRootfsHostBatch(component hydrate.RenderedComponent, files contentSet, hooks hookSet, hosts []string) error {
	if err := e.runHooksForHosts(component, hooks.preflight, hosts); err != nil {
		return err
	}
	if err := e.stopServices(hosts, files); err != nil {
		return err
	}
	if err := e.applyMutableContent(hosts, files); err != nil {
		return err
	}
	if err := e.runReloadIfNeeded(hosts, files, false); err != nil {
		return err
	}
	if err := e.runBootstrapHooksForHosts(component, files, hooks.bootstrap, hosts); err != nil {
		return err
	}
	if err := e.runHooksForHosts(component, hooks.configure, hosts); err != nil {
		return err
	}
	return e.runHooksForHosts(component, hooks.install, hosts)
}

func (e *bundleExecutor) applyBootstrapRootfsComponent(bundle *hydrate.Bundle, component hydrate.RenderedComponent, files contentSet, hooks hookSet) error {
	nodeHosts := e.nodeExecutionHosts()
	provisioningHosts, err := e.provisioningHostsForComponent(component, nodeHosts)
	if err != nil {
		return err
	}
	if err := e.stopServices(provisioningHosts, files); err != nil {
		return err
	}
	if err := e.applyMutableContent(provisioningHosts, files); err != nil {
		return err
	}
	if err := e.runHooksForHosts(component, hooks.preflight, provisioningHosts); err != nil {
		return err
	}
	if err := e.runReloadIfNeeded(provisioningHosts, files, files.hasSysctl); err != nil {
		return err
	}
	if err := e.runBootstrapHooksForHosts(component, files, hooks.bootstrap, provisioningHosts); err != nil {
		return err
	}
	if err := e.runHooksForHosts(component, hooks.configure, provisioningHosts); err != nil {
		return err
	}
	if err := e.runHooksForHosts(component, hooks.install, provisioningHosts); err != nil {
		return err
	}
	if err := e.applyLocalResources(bundle); err != nil {
		return err
	}
	if err := e.applyManifestSteps(files.manifests); err != nil {
		return err
	}
	return e.runHooks(component, hooks.healthcheck)
}

func (e *bundleExecutor) applyClusterApplicationComponent(bundle *hydrate.Bundle, component hydrate.RenderedComponent, files contentSet, hooks hookSet) error {
	if err := e.runHooks(component, hooks.preflight); err != nil {
		return err
	}
	if err := e.applyLocalResources(bundle); err != nil {
		return err
	}
	if err := e.applyManifestSteps(files.manifests); err != nil {
		return err
	}
	if err := e.runHooks(component, hooks.configure); err != nil {
		return err
	}
	if err := e.runHooks(component, hooks.install); err != nil {
		return err
	}
	return e.runHooks(component, hooks.healthcheck)
}

func (e *bundleExecutor) applyLocalResources(bundle *hydrate.Bundle) error {
	if e.localResourcesApplied || bundle == nil || len(bundle.Spec.LocalResources) == 0 {
		return nil
	}
	if err := e.ensureLocalKubeconfig(); err != nil {
		return err
	}
	if err := e.waitForFile(e.kubeconfigPath, e.waitTimeout); err != nil {
		return err
	}
	if err := e.waitForAPI(e.waitTimeout); err != nil {
		return err
	}
	if err := e.ensureClusterPrepared(); err != nil {
		return err
	}

	for _, bundlePath := range bundle.Spec.LocalResources {
		resourcePath, err := e.resolveBundlePath(bundlePath)
		if err != nil {
			return err
		}
		e.logf("applying local resource payload %q", bundlePath)
		if err := e.runCommand(0, nil, "kubectl", "--kubeconfig", e.kubeconfigPath, "apply", "-f", resourcePath); err != nil {
			return err
		}
	}
	e.localResourcesApplied = true
	return nil
}

func (e *bundleExecutor) provisioningHostsForComponent(component hydrate.RenderedComponent, hosts []string) ([]string, error) {
	if len(hosts) == 0 || e.topology == nil || e.topology.isSingleNode() {
		return slices.Clone(hosts), nil
	}
	if component.PackageName != "containerd-runtime" && component.Name != "containerd" &&
		component.PackageName != "kubernetes-rootfs" && component.Name != "kubernetes" {
		return slices.Clone(hosts), nil
	}

	provisioningHosts := make([]string, 0, len(hosts))
	for _, host := range hosts {
		joined, err := e.hostHasKubeletIdentity(host)
		if err != nil {
			return nil, err
		}
		if joined {
			e.logf("skipping host-side reprovisioning for already joined host %q", host)
			continue
		}
		provisioningHosts = append(provisioningHosts, host)
	}
	return provisioningHosts, nil
}

func (e *bundleExecutor) hostHasKubeletIdentity(host string) (bool, error) {
	output, err := e.outputShellOnHost(host, `if [ -f /etc/kubernetes/kubelet.conf ] || [ -f /etc/kubernetes/admin.conf ]; then echo joined; else echo fresh; fi`)
	if err != nil {
		return false, fmt.Errorf("detect kubelet identity on %q: %w", host, err)
	}
	return strings.TrimSpace(output) == "joined", nil
}

func (e *bundleExecutor) stopServices(hosts []string, files contentSet) error {
	services := servicesForComponent(files, e.bundlePath)
	if len(services) == 0 {
		return nil
	}
	for _, host := range hosts {
		for _, service := range services {
			if err := e.stopServiceIfActiveOnHost(host, service); err != nil {
				return err
			}
		}
	}
	return nil
}

func (e *bundleExecutor) applyMutableContent(hosts []string, files contentSet) error {
	for _, host := range hosts {
		for _, step := range files.rootfs {
			if err := e.applyRootfsToHost(host, step); err != nil {
				return err
			}
		}
		for _, step := range files.files {
			if err := e.applyFileToHost(host, step); err != nil {
				return err
			}
		}
	}
	return nil
}

func (e *bundleExecutor) runReloadIfNeeded(hosts []string, files contentSet, applySysctl bool) error {
	for _, host := range hosts {
		if applySysctl {
			if err := e.runIfPresentOnHost(host, "sysctl", "--system"); err != nil {
				return err
			}
		}
		if len(files.rootfs) > 0 || len(files.files) > 0 {
			if err := e.runIfPresentOnHost(host, "systemctl", "daemon-reload"); err != nil {
				return err
			}
		}
	}
	return nil
}

func (e *bundleExecutor) runHooksForHosts(component hydrate.RenderedComponent, hooks []hydrate.RenderedStep, allNodeHosts []string) error {
	for _, hook := range hooks {
		switch hook.Target {
		case packageformat.TargetCluster:
			if err := e.runLocalHook(component, hook); err != nil {
				return err
			}
		case packageformat.TargetAllNodes:
			targetHosts := allNodeHosts
			if targetHosts == nil {
				var err error
				targetHosts, err = e.resolveTargetHosts(hook.Target)
				if err != nil {
					return err
				}
			}
			for _, host := range targetHosts {
				if err := e.runHookOnHost(host, component, hook); err != nil {
					return err
				}
			}
		default:
			targetHosts, err := e.resolveTargetHosts(hook.Target)
			if err != nil {
				return err
			}
			for _, host := range targetHosts {
				if err := e.runHookOnHost(host, component, hook); err != nil {
					return err
				}
			}
		}
	}
	return nil
}

func (e *bundleExecutor) runHooks(component hydrate.RenderedComponent, hooks []hydrate.RenderedStep) error {
	return e.runHooksForHosts(component, hooks, nil)
}

func (e *bundleExecutor) applyManifestSteps(steps []hydrate.RenderedStep) error {
	if len(steps) == 0 {
		return nil
	}
	if err := e.ensureLocalKubeconfig(); err != nil {
		return err
	}
	if err := e.waitForFile(e.kubeconfigPath, e.waitTimeout); err != nil {
		return err
	}
	if err := e.waitForAPI(e.waitTimeout); err != nil {
		return err
	}
	if err := e.ensureClusterPrepared(); err != nil {
		return err
	}
	for _, step := range steps {
		if err := e.applyManifest(step); err != nil {
			return err
		}
	}
	return nil
}

func (e *bundleExecutor) ensureLocalKubeconfig() error {
	if strings.TrimSpace(e.kubeconfigPath) == "" {
		return fmt.Errorf("kubeconfig path cannot be empty")
	}
	if _, err := os.Stat(e.kubeconfigPath); err == nil {
		return nil
	} else if err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	if e.topology == nil || e.topology.hasLocalExecutionHost() {
		return nil
	}
	firstMasterHosts, err := e.resolveTargetHosts(packageformat.TargetFirstMaster)
	if err != nil {
		return err
	}
	if len(firstMasterHosts) != 1 || isLocalExecutionHost(firstMasterHosts[0]) {
		return nil
	}
	if e.remoteExec == nil {
		return fmt.Errorf("remote execution client is not configured for host %q", firstMasterHosts[0])
	}
	if err := os.MkdirAll(filepath.Dir(e.kubeconfigPath), 0o700); err != nil {
		return err
	}
	e.logf("fetching kubeconfig from first master %q to %q", firstMasterHosts[0], e.kubeconfigPath)
	if err := e.remoteExec.Fetch(firstMasterHosts[0], "/etc/kubernetes/admin.conf", e.kubeconfigPath); err != nil {
		return fmt.Errorf("fetch kubeconfig from first master %q: %w", firstMasterHosts[0], err)
	}
	return nil
}

func (e *bundleExecutor) applyRootfsToHost(host string, step hydrate.RenderedStep) error {
	if !isLocalExecutionHost(host) {
		return e.applyRemoteRootfs(host, step)
	}
	if _, ok := trimStepPrefix(step.SourcePath, "rootfs"); !ok {
		return fmt.Errorf("rootfs step %q path %q must stay under rootfs/", step.Name, step.SourcePath)
	}

	src, err := e.resolveBundleStepPathForHost(host, step)
	if err != nil {
		return err
	}
	info, err := os.Stat(src)
	if err != nil {
		return err
	}
	if !info.IsDir() {
		return fmt.Errorf("rootfs step %q path %q must be a directory", step.Name, src)
	}

	return filepath.WalkDir(src, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		rel, err := filepath.Rel(src, path)
		if err != nil {
			return err
		}
		if rel == "." {
			return nil
		}

		dst := filepath.Join(e.hostRoot, rel)
		info, err := entry.Info()
		if err != nil {
			return err
		}

		switch {
		case entry.Type()&os.ModeSymlink != 0:
			return copySymlink(path, dst)
		case info.IsDir():
			return os.MkdirAll(dst, info.Mode())
		case info.Mode().IsRegular():
			return copyRegularFile(path, dst, info.Mode(), info.ModTime())
		default:
			return fmt.Errorf("unsupported rootfs entry type for %q", path)
		}
	})
}

func (e *bundleExecutor) applyFileToHost(host string, step hydrate.RenderedStep) error {
	if !isLocalExecutionHost(host) {
		return e.applyRemoteFile(host, step)
	}
	hostRel, ok := trimStepPrefix(step.SourcePath, "files/")
	if !ok {
		return fmt.Errorf("file step %q path %q must stay under files/", step.Name, step.SourcePath)
	}

	src, err := e.resolveBundleStepPathForHost(host, step)
	if err != nil {
		return err
	}
	info, err := os.Lstat(src)
	if err != nil {
		return err
	}

	dst := filepath.Join(e.hostRoot, filepath.FromSlash(hostRel))
	if info.Mode()&os.ModeSymlink != 0 {
		return copySymlink(src, dst)
	}
	if info.IsDir() {
		return copyDir(src, dst)
	}
	if !info.Mode().IsRegular() {
		return fmt.Errorf("unsupported file payload type for %q", src)
	}
	return copyRegularFile(src, dst, info.Mode(), info.ModTime())
}

func (e *bundleExecutor) applyManifest(step hydrate.RenderedStep) error {
	stepPath, err := e.resolveBundleStepPathForHost(localExecutionHost, step)
	if err != nil {
		return err
	}
	e.logf("applying manifest payload %q", step.SourcePath)
	return e.runCommand(step.TimeoutSeconds, nil, "kubectl", "--kubeconfig", e.kubeconfigPath, "apply", "-f", stepPath)
}

func (e *bundleExecutor) runLocalHook(component hydrate.RenderedComponent, step hydrate.RenderedStep) error {
	hookPath, err := e.resolveBundleStepPathForHost(localExecutionHost, step)
	if err != nil {
		return err
	}
	info, err := os.Stat(hookPath)
	if err != nil {
		return err
	}
	if info.IsDir() {
		return fmt.Errorf("hook %q cannot be a directory", hookPath)
	}
	if info.Mode()&0o111 == 0 {
		if err := os.Chmod(hookPath, info.Mode()|0o755); err != nil {
			return fmt.Errorf("chmod hook %q: %w", hookPath, err)
		}
	}

	componentRoot, err := e.resolveComponentRootForHost(localExecutionHost, component)
	if err != nil {
		return err
	}

	env := e.hookEnvironment(localExecutionHost, componentRoot, e.bundlePath)
	e.logf("running hook %q for component %q", step.Name, component.Name)
	args := append([]string{hookPath}, step.Args...)
	return e.runCommand(step.TimeoutSeconds, env, args[0], args[1:]...)
}

func (e *bundleExecutor) runHookOnHost(host string, component hydrate.RenderedComponent, step hydrate.RenderedStep) error {
	if isLocalExecutionHost(host) {
		return e.runLocalHook(component, step)
	}

	hookPath, err := e.resolveBundleStepPathForHost(host, step)
	if err != nil {
		return err
	}
	componentRoot, err := e.resolveComponentRootForHost(host, component)
	if err != nil {
		return err
	}

	env := e.hookEnvironment(host, componentRoot, e.remoteBundleRoot(host))
	e.logf("running hook %q for component %q on host %q", step.Name, component.Name, host)
	args := append([]string{hookPath}, step.Args...)
	return e.runCommandOnHost(host, step.TimeoutSeconds, env, args[0], args[1:]...)
}

func (e *bundleExecutor) stopServiceIfActiveOnHost(host, service string) error {
	if !isLocalExecutionHost(host) {
		if err := e.runShellOnHost(host, 0, nil, "systemctl is-active --quiet "+shellQuote(service)); err != nil {
			return nil
		}
		e.logf("stopping service %q on host %q before updating host binaries", service, host)
		return e.runCommandOnHost(host, 0, nil, "systemctl", "stop", service)
	}

	systemctlPath, err := exec.LookPath("systemctl")
	if err != nil {
		return nil
	}

	check := exec.Command(systemctlPath, "is-active", "--quiet", service) // #nosec G204
	check.Stdout = io.Discard
	check.Stderr = io.Discard
	if err := check.Run(); err != nil {
		return nil
	}

	e.logf("stopping service %q before updating host binaries", service)
	return e.runCommand(0, nil, systemctlPath, "stop", service)
}

func (e *bundleExecutor) runIfPresentOnHost(host, name string, args ...string) error {
	if !isLocalExecutionHost(host) {
		e.logf("running %s %s on host %q", name, strings.Join(args, " "), host)
		return e.runShellOnHost(host, 0, nil, "command -v "+shellQuote(name)+" >/dev/null 2>&1 || exit 0\n"+shellCommand(name, args...))
	}

	path, err := exec.LookPath(name)
	if err != nil {
		return nil
	}
	e.logf("running %s %s", name, strings.Join(args, " "))
	return e.runCommand(0, nil, path, args...)
}

func (e *bundleExecutor) waitForFile(path string, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	for {
		if _, err := os.Stat(path); err == nil {
			return nil
		} else if !errors.Is(err, os.ErrNotExist) {
			return err
		}
		if time.Now().After(deadline) {
			return fmt.Errorf("timed out waiting for file %q", path)
		}
		time.Sleep(time.Second)
	}
}

func (e *bundleExecutor) waitForAPI(timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	for {
		err := e.runCommand(0, nil, "kubectl", "--kubeconfig", e.kubeconfigPath, "get", "--raw=/readyz")
		if err == nil {
			return nil
		}
		if time.Now().After(deadline) {
			return fmt.Errorf("timed out waiting for Kubernetes API readiness: %w", err)
		}
		time.Sleep(2 * time.Second)
	}
}

func (e *bundleExecutor) ensureClusterPrepared() error {
	if e.untaintAttempted {
		return nil
	}
	e.untaintAttempted = true

	output, err := e.outputCommand(0, nil, "kubectl", "--kubeconfig", e.kubeconfigPath, "get", "nodes", "-o", "jsonpath={.items[*].metadata.name}")
	if err != nil {
		return err
	}
	nodes := strings.Fields(strings.TrimSpace(output))
	if len(nodes) != 1 {
		return nil
	}

	node := nodes[0]
	e.logf("removing single-node control-plane taints from %q", node)
	for _, taint := range []string{
		"node-role.kubernetes.io/control-plane-",
		"node-role.kubernetes.io/master-",
	} {
		_ = e.runCommand(0, nil, "kubectl", "--kubeconfig", e.kubeconfigPath, "taint", "nodes", node, taint)
	}
	return nil
}

func (e *bundleExecutor) resolveBundleStepPathForHost(host string, step hydrate.RenderedStep) (string, error) {
	if hostScoped, ok, err := e.resolveHostScopedInputStepPath(host, step); err != nil {
		return "", err
	} else if ok {
		return hostScoped, nil
	}
	bundleRoot, err := e.bundleRootForHost(host)
	if err != nil {
		return "", err
	}
	return resolveBundleRelativePath(bundleRoot, step.BundlePath)
}

func (e *bundleExecutor) resolveComponentRootForHost(host string, component hydrate.RenderedComponent) (string, error) {
	bundleRoot, err := e.bundleRootForHost(host)
	if err != nil {
		return "", err
	}
	return resolveBundleRelativePath(bundleRoot, component.RootPath)
}

func (e *bundleExecutor) resolveBundlePath(bundleRel string) (string, error) {
	return resolveBundleRelativePath(e.bundlePath, bundleRel)
}

func (e *bundleExecutor) resolveHostScopedInputStepPath(host string, step hydrate.RenderedStep) (string, bool, error) {
	if e.bundle == nil || step.Kind != hydrate.StepContent || step.ContentType != packageformat.ContentFile {
		return "", false, nil
	}
	if _, ok := trimStepPrefix(step.SourcePath, "files/"); !ok {
		return "", false, nil
	}

	for _, component := range e.bundle.Spec.Components {
		if !componentHasRenderedStep(component, step) {
			continue
		}
		inputName, ok := localInputNameForSourcePath(component, step.SourcePath)
		if !ok {
			return "", false, nil
		}
		hostBindings, ok := component.HostInputBindings[inputName]
		if !ok || len(hostBindings) == 0 {
			return "", false, nil
		}
		normalizedHost := normalizeExecutionHost(host)
		bundleRel, ok := hostBindings[normalizedHost]
		if !ok {
			return "", false, nil
		}
		bundleRoot, err := e.bundleRootForHost(host)
		if err != nil {
			return "", false, err
		}
		resolved, err := resolveBundleRelativePath(bundleRoot, bundleRel)
		if err != nil {
			return "", false, err
		}
		return resolved, true, nil
	}
	return "", false, nil
}

func componentHasRenderedStep(component hydrate.RenderedComponent, step hydrate.RenderedStep) bool {
	for _, renderedStep := range component.Steps {
		if renderedStep.Name == step.Name &&
			renderedStep.Kind == step.Kind &&
			renderedStep.BundlePath == step.BundlePath &&
			renderedStep.SourcePath == step.SourcePath {
			return true
		}
	}
	return false
}

func localInputNameForSourcePath(component hydrate.RenderedComponent, sourcePath string) (string, bool) {
	for _, input := range component.Inputs {
		if input.Path == sourcePath {
			return input.Name, true
		}
	}
	return "", false
}

func normalizeExecutionHost(host string) string {
	trimmed := strings.TrimSpace(host)
	if trimmed == "" {
		return ""
	}
	if normalized := iputils.GetHostIP(trimmed); normalized != "" {
		return normalized
	}
	if base, _, found := strings.Cut(trimmed, ":"); found {
		trimmed = strings.TrimSpace(base)
	}
	return trimmed
}

func resolveBundleRelativePath(bundleRoot, bundleRel string) (string, error) {
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

func (e *bundleExecutor) runCommand(timeoutSeconds int32, extraEnv []string, name string, args ...string) error {
	ctx := context.Background()
	if timeoutSeconds > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, time.Duration(timeoutSeconds)*time.Second)
		defer cancel()
	}

	cmd := exec.CommandContext(ctx, name, args...) // #nosec G204
	cmd.Stdout = e.stderr
	cmd.Stderr = e.stderr
	cmd.Env = append(os.Environ(), extraEnv...)
	if err := cmd.Run(); err != nil {
		if ctx.Err() == context.DeadlineExceeded {
			return fmt.Errorf("command %q timed out: %w", name, ctx.Err())
		}
		return fmt.Errorf("run %q: %w", strings.Join(append([]string{name}, args...), " "), err)
	}
	return nil
}

func (e *bundleExecutor) runCommandOnHost(host string, timeoutSeconds int32, extraEnv []string, name string, args ...string) error {
	if isLocalExecutionHost(host) {
		return e.runCommand(timeoutSeconds, extraEnv, name, args...)
	}
	return e.runShellOnHost(host, timeoutSeconds, extraEnv, shellCommand(name, args...))
}

func (e *bundleExecutor) runShellOnHost(host string, timeoutSeconds int32, extraEnv []string, command string) error {
	if isLocalExecutionHost(host) {
		return e.runCommand(timeoutSeconds, extraEnv, "/bin/bash", "-lc", command)
	}
	if e.remoteExec == nil {
		return fmt.Errorf("remote execution client is not configured for host %q", host)
	}

	ctx := context.Background()
	if timeoutSeconds > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, time.Duration(timeoutSeconds)*time.Second)
		defer cancel()
	}

	if err := e.remoteExec.CmdAsyncWithContext(ctx, host, wrapShellCommand(command, extraEnv)); err != nil {
		if ctx.Err() == context.DeadlineExceeded {
			return fmt.Errorf("command on host %q timed out: %w", host, ctx.Err())
		}
		return fmt.Errorf("run on host %q: %w", host, err)
	}
	return nil
}

func (e *bundleExecutor) outputCommand(timeoutSeconds int32, extraEnv []string, name string, args ...string) (string, error) {
	ctx := context.Background()
	if timeoutSeconds > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, time.Duration(timeoutSeconds)*time.Second)
		defer cancel()
	}

	cmd := exec.CommandContext(ctx, name, args...) // #nosec G204
	cmd.Stderr = e.stderr
	cmd.Env = append(os.Environ(), extraEnv...)
	output, err := cmd.Output()
	if err != nil {
		if ctx.Err() == context.DeadlineExceeded {
			return "", fmt.Errorf("command %q timed out: %w", name, ctx.Err())
		}
		return "", fmt.Errorf("run %q: %w", strings.Join(append([]string{name}, args...), " "), err)
	}
	return string(output), nil
}

func (e *bundleExecutor) resolveTargetHosts(target packageformat.ExecutionTarget) ([]string, error) {
	if e.topology == nil {
		return fallbackLocalExecutionTopology(e.clusterName).resolveHostTarget(target)
	}
	return e.topology.resolveHostTarget(target)
}

func (e *bundleExecutor) nodeExecutionHosts() []string {
	if e.topology == nil {
		return []string{localExecutionHost}
	}
	return e.topology.nodeExecutionHosts()
}

func (e *bundleExecutor) forEachHostBatch(hosts []string, fn func([]string) error) error {
	if len(hosts) == 0 {
		return nil
	}
	if fn == nil {
		return fmt.Errorf("host batch function cannot be nil")
	}
	batches := e.rolloutHostBatches(hosts)
	canaryBatchCount := e.rolloutCanaryBatchCount(len(hosts))
	for i, batch := range batches {
		if len(batches) > 1 {
			e.logf("running rollout batch %d/%d%s on hosts: %s", i+1, len(batches), rolloutBatchLabel(i, canaryBatchCount), strings.Join(batch, ","))
		}
		if err := fn(batch); err != nil {
			return err
		}
		if e.rollout.Pause.AfterCanary && canaryBatchCount > 0 && i+1 == canaryBatchCount && i+1 < len(batches) {
			return rolloutPausedError{message: "rollout paused after canary batch"}
		}
	}
	return nil
}

func (e *bundleExecutor) rolloutHostBatches(hosts []string) [][]string {
	batchSize := e.rollout.BatchSize
	canarySize := e.rollout.Canary.BatchSize
	if canarySize > 0 && canarySize < len(hosts) {
		batches := [][]string{slices.Clone(hosts[:canarySize])}
		for _, batch := range splitRolloutHosts(hosts[canarySize:], batchSize) {
			batches = append(batches, batch)
		}
		return batches
	}
	return splitRolloutHosts(hosts, batchSize)
}

func splitRolloutHosts(hosts []string, batchSize int) [][]string {
	if batchSize <= 0 || batchSize >= len(hosts) {
		if len(hosts) == 0 {
			return nil
		}
		return [][]string{slices.Clone(hosts)}
	}
	var batches [][]string
	for start := 0; start < len(hosts); start += batchSize {
		end := start + batchSize
		if end > len(hosts) {
			end = len(hosts)
		}
		batches = append(batches, slices.Clone(hosts[start:end]))
	}
	return batches
}

func (e *bundleExecutor) rolloutCanaryBatchCount(hostCount int) int {
	if e.rollout.Canary.BatchSize <= 0 || e.rollout.Canary.BatchSize >= hostCount {
		return 0
	}
	return 1
}

func rolloutBatchLabel(index, canaryBatchCount int) string {
	if index < 0 {
		return ""
	}
	if index < canaryBatchCount {
		return " (canary)"
	}
	return ""
}

func (e *bundleExecutor) hookEnvironment(host, componentRoot, bundleRoot string) []string {
	env := []string{
		"COMPONENT_ROOT=" + componentRoot,
		"BUNDLE_DIR=" + bundleRoot,
		"CLUSTER_NAME=" + e.clusterName,
		"KUBECONFIG=" + e.kubeconfigPath,
		"HOST_ROOT=" + e.hostRoot,
	}
	if strings.TrimSpace(host) != "" {
		env = append(env,
			"TARGET_HOST="+host,
			"TARGET_HOST_IP="+iputils.GetHostIP(host),
		)
		if e.topology != nil {
			env = append(env,
				"TARGET_IS_FIRST_MASTER="+strconv.FormatBool(e.topology.isFirstMaster(host)),
				"TARGET_NODE_ROLES="+strings.Join(e.topology.rolesForHost(host), ","),
			)
		}
	}
	return env
}

func (e *bundleExecutor) bundleRootForHost(host string) (string, error) {
	if isLocalExecutionHost(host) {
		return e.bundlePath, nil
	}
	if root, ok := e.stagedBundleRoots[host]; ok {
		return root, nil
	}
	if e.remoteExec == nil {
		return "", fmt.Errorf("remote execution client is not configured for host %q", host)
	}

	remoteRoot := e.remoteBundleRoot(host)
	e.logf("staging bundle for host %q at %q", host, remoteRoot)
	if err := e.remoteExec.Copy(host, e.bundlePath, remoteRoot); err != nil {
		return "", fmt.Errorf("stage bundle on host %q: %w", host, err)
	}
	e.stagedBundleRoots[host] = remoteRoot
	return remoteRoot, nil
}

func (e *bundleExecutor) remoteBundleRoot(host string) string {
	if root, ok := e.stagedBundleRoots[host]; ok && root != "" {
		return root
	}
	stageID := strings.ReplaceAll(e.desiredStateDigest, ":", "-")
	if strings.TrimSpace(stageID) == "" {
		stageID = "current"
	}
	return filepath.Join(BundleStorePath(e.clusterName), "staged", stageID)
}

func (e *bundleExecutor) applyRemoteRootfs(host string, step hydrate.RenderedStep) error {
	if _, ok := trimStepPrefix(step.SourcePath, "rootfs"); !ok {
		return fmt.Errorf("rootfs step %q path %q must stay under rootfs/", step.Name, step.SourcePath)
	}
	src, err := e.resolveBundleStepPathForHost(host, step)
	if err != nil {
		return err
	}
	script := fmt.Sprintf(
		"src=%s\ndst=%s\nmkdir -p \"$dst\"\ncd \"$src\"\nfind . -mindepth 1 | while IFS= read -r rel; do\n  rel=${rel#./}\n  from=\"$src/$rel\"\n  to=\"$dst/$rel\"\n  if [ -d \"$from\" ] && [ ! -L \"$from\" ]; then\n    mkdir -p \"$to\"\n    continue\n  fi\n  mkdir -p \"$(dirname \"$to\")\"\n  if [ -L \"$from\" ]; then\n    link_target=\"$(readlink \"$from\")\"\n    if [ -L \"$to\" ] && [ \"$(readlink \"$to\")\" = \"$link_target\" ]; then\n      continue\n    fi\n    rm -rf \"$to\"\n    ln -s \"$link_target\" \"$to\"\n    continue\n  fi\n  if [ -f \"$from\" ]; then\n    if [ -f \"$to\" ] && cmp -s \"$from\" \"$to\"; then\n      continue\n    fi\n    cp -a \"$from\" \"$to\"\n    continue\n  fi\n  echo \"unsupported rootfs entry type: $from\" >&2\n  exit 1\ndone\n",
		shellQuote(src),
		shellQuote(e.hostRoot),
	)
	return e.runShellOnHost(host, step.TimeoutSeconds, nil, script)
}

func (e *bundleExecutor) applyRemoteFile(host string, step hydrate.RenderedStep) error {
	hostRel, ok := trimStepPrefix(step.SourcePath, "files/")
	if !ok {
		return fmt.Errorf("file step %q path %q must stay under files/", step.Name, step.SourcePath)
	}
	src, err := e.resolveBundleStepPathForHost(host, step)
	if err != nil {
		return err
	}
	dst := filepath.Join(e.hostRoot, filepath.FromSlash(hostRel))
	script := fmt.Sprintf(
		"src=%s\ndst=%s\nif [ -L \"$src\" ] || [ -f \"$src\" ]; then\n  mkdir -p \"$(dirname \"$dst\")\"\n  cp -a \"$src\" \"$dst\"\nelif [ -d \"$src\" ]; then\n  mkdir -p \"$dst\"\n  cp -a \"$src\"/. \"$dst\"/\nelse\n  echo \"unsupported file payload type: $src\" >&2\n  exit 1\nfi\n",
		shellQuote(src),
		shellQuote(dst),
	)
	return e.runShellOnHost(host, step.TimeoutSeconds, nil, script)
}

func (e *bundleExecutor) logf(format string, args ...interface{}) {
	fmt.Fprintf(e.stderr, "==> "+format+"\n", args...)
}

func trimStepPrefix(path, prefix string) (string, bool) {
	cleaned := filepath.ToSlash(filepath.Clean(path))
	normalizedPrefix := filepath.ToSlash(filepath.Clean(prefix))

	switch {
	case cleaned == normalizedPrefix:
		return "", true
	case strings.HasPrefix(cleaned, normalizedPrefix+"/"):
		return strings.TrimPrefix(cleaned, normalizedPrefix+"/"), true
	default:
		return "", false
	}
}

func shellCommand(name string, args ...string) string {
	quoted := make([]string, 0, 1+len(args))
	quoted = append(quoted, shellQuote(name))
	for _, arg := range args {
		quoted = append(quoted, shellQuote(arg))
	}
	return strings.Join(quoted, " ")
}

func wrapShellCommand(command string, extraEnv []string) string {
	if len(extraEnv) == 0 {
		return command
	}
	var builder strings.Builder
	for _, env := range extraEnv {
		parts := strings.SplitN(env, "=", 2)
		if len(parts) != 2 {
			continue
		}
		builder.WriteString("export ")
		builder.WriteString(parts[0])
		builder.WriteString("=")
		builder.WriteString(shellQuote(parts[1]))
		builder.WriteString("\n")
	}
	builder.WriteString(command)
	return builder.String()
}

func shellQuote(value string) string {
	return "'" + strings.ReplaceAll(value, "'", `'"'"'`) + "'"
}

func copyDir(src, dst string) error {
	info, err := os.Stat(src)
	if err != nil {
		return err
	}
	if !info.IsDir() {
		return fmt.Errorf("copyDir source %q is not a directory", src)
	}

	return filepath.WalkDir(src, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		rel, err := filepath.Rel(src, path)
		if err != nil {
			return err
		}
		target := dst
		if rel != "." {
			target = filepath.Join(dst, rel)
		}

		info, err := entry.Info()
		if err != nil {
			return err
		}
		switch {
		case entry.Type()&os.ModeSymlink != 0:
			return copySymlink(path, target)
		case info.IsDir():
			return os.MkdirAll(target, info.Mode())
		case info.Mode().IsRegular():
			return copyRegularFile(path, target, info.Mode(), info.ModTime())
		default:
			return fmt.Errorf("unsupported directory entry type for %q", path)
		}
	})
}

func copyRegularFile(src, dst string, mode fs.FileMode, modTime time.Time) error {
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return err
	}

	srcFile, err := os.Open(src)
	if err != nil {
		return err
	}
	defer srcFile.Close()

	tempFile, err := os.CreateTemp(filepath.Dir(dst), filepath.Base(dst)+".sealos-*")
	if err != nil {
		return err
	}

	tempPath := tempFile.Name()
	cleanup := true
	defer func() {
		if cleanup {
			_ = os.Remove(tempPath)
		}
	}()

	if _, err := io.Copy(tempFile, srcFile); err != nil {
		_ = tempFile.Close()
		return err
	}
	if err := tempFile.Close(); err != nil {
		return err
	}
	if err := os.Chmod(tempPath, mode); err != nil {
		return err
	}
	if err := os.Chtimes(tempPath, modTime, modTime); err != nil {
		return err
	}
	if err := os.Rename(tempPath, dst); err != nil {
		return err
	}
	cleanup = false
	return nil
}

func copySymlink(src, dst string) error {
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return err
	}
	target, err := os.Readlink(src)
	if err != nil {
		return err
	}
	if err := os.RemoveAll(dst); err != nil {
		return err
	}
	return os.Symlink(target, dst)
}
