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
	"strings"
	"time"

	"github.com/opencontainers/go-digest"

	"github.com/labring/sealos/pkg/distribution"
	"github.com/labring/sealos/pkg/distribution/hydrate"
	"github.com/labring/sealos/pkg/distribution/packageformat"
	"github.com/labring/sealos/pkg/distribution/state"
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
}

type ApplyResult struct {
	Bundle             *hydrate.Bundle
	BundlePath         string
	DesiredStateDigest string
	AppliedRevision    *state.AppliedRevision
}

type bundleExecutor struct {
	bundlePath       string
	clusterName      string
	kubeconfigPath   string
	hostRoot         string
	stderr           io.Writer
	waitTimeout      time.Duration
	untaintAttempted bool
}

func Apply(opts ApplyOptions) (*ApplyResult, error) {
	if strings.TrimSpace(opts.ClusterName) == "" {
		return nil, fmt.Errorf("cluster name cannot be empty")
	}
	if strings.TrimSpace(opts.BundlePath) == "" {
		return nil, fmt.Errorf("bundle path cannot be empty")
	}

	bundlePath, err := filepath.Abs(opts.BundlePath)
	if err != nil {
		return nil, fmt.Errorf("resolve bundle path %q: %w", opts.BundlePath, err)
	}

	bundle, err := loadBundle(bundlePath)
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
	executor := bundleExecutor{
		bundlePath:     bundlePath,
		clusterName:    opts.ClusterName,
		kubeconfigPath: opts.KubeconfigPath,
		hostRoot:       opts.HostRoot,
		stderr:         opts.Stderr,
		waitTimeout:    opts.WaitTimeout,
	}
	executor.applyDefaults()

	if err := executor.applyBundle(bundle); err != nil {
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

func loadBundle(bundlePath string) (*hydrate.Bundle, error) {
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
		if err := e.applyComponent(component); err != nil {
			return fmt.Errorf("apply component %q: %w", component.Name, err)
		}
	}
	return nil
}

func (e *bundleExecutor) ensurePrivileges(bundle *hydrate.Bundle) error {
	if bundle == nil {
		return fmt.Errorf("bundle cannot be nil")
	}
	if e.hostRoot != string(os.PathSeparator) {
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

func (e *bundleExecutor) applyComponent(component hydrate.RenderedComponent) error {
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
		return e.applyBootstrapRootfsComponent(component, files, hooks)
	case componentModeClusterApplication:
		return e.applyClusterApplicationComponent(component, files, hooks)
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
	if err := e.runHooks(component, hooks.preflight); err != nil {
		return err
	}
	if err := e.stopServices(files); err != nil {
		return err
	}
	if err := e.applyMutableContent(files); err != nil {
		return err
	}
	if err := e.runReloadIfNeeded(files, false); err != nil {
		return err
	}
	if err := e.runHooks(component, hooks.bootstrap); err != nil {
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

func (e *bundleExecutor) applyBootstrapRootfsComponent(component hydrate.RenderedComponent, files contentSet, hooks hookSet) error {
	if err := e.stopServices(files); err != nil {
		return err
	}
	if err := e.applyMutableContent(files); err != nil {
		return err
	}
	if err := e.runHooks(component, hooks.preflight); err != nil {
		return err
	}
	if err := e.runReloadIfNeeded(files, files.hasSysctl); err != nil {
		return err
	}
	if err := e.runHooks(component, hooks.bootstrap); err != nil {
		return err
	}
	if err := e.runHooks(component, hooks.configure); err != nil {
		return err
	}
	if err := e.runHooks(component, hooks.install); err != nil {
		return err
	}
	if err := e.applyManifestSteps(files.manifests); err != nil {
		return err
	}
	return e.runHooks(component, hooks.healthcheck)
}

func (e *bundleExecutor) applyClusterApplicationComponent(component hydrate.RenderedComponent, files contentSet, hooks hookSet) error {
	if err := e.runHooks(component, hooks.preflight); err != nil {
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

func (e *bundleExecutor) stopServices(files contentSet) error {
	for _, service := range servicesForComponent(files, e.bundlePath) {
		if err := e.stopServiceIfActive(service); err != nil {
			return err
		}
	}
	return nil
}

func (e *bundleExecutor) applyMutableContent(files contentSet) error {
	for _, step := range files.rootfs {
		if err := e.applyRootfs(step); err != nil {
			return err
		}
	}
	for _, step := range files.files {
		if err := e.applyFile(step); err != nil {
			return err
		}
	}
	return nil
}

func (e *bundleExecutor) runReloadIfNeeded(files contentSet, applySysctl bool) error {
	if applySysctl {
		if err := e.runIfPresent("sysctl", "--system"); err != nil {
			return err
		}
	}
	if len(files.rootfs) > 0 || len(files.files) > 0 {
		if err := e.runIfPresent("systemctl", "daemon-reload"); err != nil {
			return err
		}
	}
	return nil
}

func (e *bundleExecutor) runHooks(component hydrate.RenderedComponent, hooks []hydrate.RenderedStep) error {
	for _, hook := range hooks {
		if err := e.runHook(component, hook); err != nil {
			return err
		}
	}
	return nil
}

func (e *bundleExecutor) applyManifestSteps(steps []hydrate.RenderedStep) error {
	if len(steps) == 0 {
		return nil
	}
	if err := e.waitForFile(e.kubeconfigPath, e.waitTimeout); err != nil {
		return err
	}
	if err := e.waitForAPI(e.waitTimeout); err != nil {
		return err
	}
	if err := e.ensureSingleNodeClusterPrepared(); err != nil {
		return err
	}
	for _, step := range steps {
		if err := e.applyManifest(step); err != nil {
			return err
		}
	}
	return nil
}

func (e *bundleExecutor) applyRootfs(step hydrate.RenderedStep) error {
	if _, ok := trimStepPrefix(step.SourcePath, "rootfs"); !ok {
		return fmt.Errorf("rootfs step %q path %q must stay under rootfs/", step.Name, step.SourcePath)
	}

	src, err := e.resolveBundleStepPath(step)
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

func (e *bundleExecutor) applyFile(step hydrate.RenderedStep) error {
	hostRel, ok := trimStepPrefix(step.SourcePath, "files/")
	if !ok {
		return fmt.Errorf("file step %q path %q must stay under files/", step.Name, step.SourcePath)
	}

	src, err := e.resolveBundleStepPath(step)
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
	stepPath, err := e.resolveBundleStepPath(step)
	if err != nil {
		return err
	}
	e.logf("applying manifest payload %q", step.SourcePath)
	return e.runCommand(step.TimeoutSeconds, nil, "kubectl", "--kubeconfig", e.kubeconfigPath, "apply", "-f", stepPath)
}

func (e *bundleExecutor) runHook(component hydrate.RenderedComponent, step hydrate.RenderedStep) error {
	hookPath, err := e.resolveBundleStepPath(step)
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

	componentRoot, err := e.resolveBundlePath(component.RootPath)
	if err != nil {
		return err
	}

	env := []string{
		"COMPONENT_ROOT=" + componentRoot,
		"BUNDLE_DIR=" + e.bundlePath,
		"CLUSTER_NAME=" + e.clusterName,
		"KUBECONFIG=" + e.kubeconfigPath,
		"HOST_ROOT=" + e.hostRoot,
	}

	e.logf("running hook %q for component %q", step.Name, component.Name)
	args := append([]string{hookPath}, step.Args...)
	return e.runCommand(step.TimeoutSeconds, env, args[0], args[1:]...)
}

func (e *bundleExecutor) stopServiceIfActive(service string) error {
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

func (e *bundleExecutor) runIfPresent(name string, args ...string) error {
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

func (e *bundleExecutor) ensureSingleNodeClusterPrepared() error {
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
		return fmt.Errorf("sync apply only supports single-node clusters, found %d nodes", len(nodes))
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

func (e *bundleExecutor) resolveBundleStepPath(step hydrate.RenderedStep) (string, error) {
	return e.resolveBundlePath(step.BundlePath)
}

func (e *bundleExecutor) resolveBundlePath(bundleRel string) (string, error) {
	if strings.TrimSpace(bundleRel) == "" {
		return "", fmt.Errorf("bundle path cannot be empty")
	}
	if filepath.IsAbs(bundleRel) {
		return "", fmt.Errorf("bundle path %q must be relative", bundleRel)
	}

	resolved := filepath.Join(e.bundlePath, filepath.FromSlash(bundleRel))
	relative, err := filepath.Rel(e.bundlePath, resolved)
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
