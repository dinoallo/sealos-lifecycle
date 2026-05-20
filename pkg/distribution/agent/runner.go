// Copyright 2026 sealos.
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

package agent

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/opencontainers/go-digest"

	"github.com/labring/sealos/pkg/distribution/bom"
	"github.com/labring/sealos/pkg/distribution/hydrate"
	"github.com/labring/sealos/pkg/distribution/localrepo"
	"github.com/labring/sealos/pkg/distribution/ocipackage"
	"github.com/labring/sealos/pkg/distribution/packageformat"
	"github.com/labring/sealos/pkg/distribution/reconcile"
	"github.com/labring/sealos/pkg/distribution/state"
)

type TargetOptions struct {
	BOMPath                 string
	DistributionChannelPath string
}

type PackageSource struct {
	Component string
	Root      string
}

type Options struct {
	ClusterName        string
	Target             TargetOptions
	LocalRepoPath      string
	LocalPatchRevision string
	PackageSources     []PackageSource
	CacheRoot          string
	Mounter            packageformat.ImageMounter
	ApplyOptions       reconcile.ApplyOptions
	Interval           time.Duration
	Once               bool
	Out                io.Writer
}

type Result struct {
	ClusterName        string `json:"clusterName" yaml:"clusterName"`
	BOMName            string `json:"bomName" yaml:"bomName"`
	Revision           string `json:"revision" yaml:"revision"`
	Channel            string `json:"channel" yaml:"channel"`
	BundlePath         string `json:"bundlePath" yaml:"bundlePath"`
	DesiredStateDigest string `json:"desiredStateDigest" yaml:"desiredStateDigest"`
	AppliedRevision    string `json:"appliedRevisionPath" yaml:"appliedRevisionPath"`
}

type Runner struct {
	Materialize func(*bom.BOM, reconcile.Options) (*reconcile.Result, error)
	Apply       func(reconcile.ApplyOptions) (*reconcile.ApplyResult, error)
	Sleep       func(context.Context, time.Duration) error
}

type resolvedTarget struct {
	bom                     *bom.BOM
	bomPath                 string
	distributionChannel     *bom.DistributionChannel
	distributionChannelPath string
}

type packageLoader struct {
	local    map[string]*packageformat.ComponentPackage
	fallback packageformat.Loader
}

type sourceProvider struct {
	local    map[string]string
	fallback hydrate.SourceProvider
}

type cachedArtifactResolver struct {
	cache *ocipackage.Cache
}

func (r Runner) Run(ctx context.Context, opts Options) (*Result, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	opts = normalizeOptions(opts)
	if opts.Once {
		return r.runOnce(ctx, opts)
	}
	if opts.Interval <= 0 {
		return nil, fmt.Errorf("interval must be positive unless once is true")
	}

	var last *Result
	for {
		result, err := r.runOnce(ctx, opts)
		if err != nil {
			if ctxErr := ctx.Err(); ctxErr != nil {
				return result, ctxErr
			}
			if isContextError(err) {
				return result, err
			}
			if result != nil {
				last = result
			}
			logReconcileError(opts.Out, err)
		} else {
			last = result
		}
		if err := sleepContext(r.Sleep, ctx, opts.Interval); err != nil {
			if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
				return last, err
			}
			return last, err
		}
	}
}

func (r Runner) runOnce(ctx context.Context, opts Options) (*Result, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	target, err := resolveTarget(opts.Target)
	if err != nil {
		return nil, markDegraded(opts.ClusterName, "ResolveTargetFailed", err)
	}
	materializeOpts, err := materializeOptions(target, opts)
	if err != nil {
		return nil, markDegraded(opts.ClusterName, "PrepareRenderFailed", err)
	}

	materialize := r.Materialize
	if materialize == nil {
		materialize = reconcile.Materialize
	}
	rendered, err := materialize(target.bom, materializeOpts)
	if err != nil {
		return nil, markDegraded(opts.ClusterName, "RenderFailed", err)
	}

	apply := r.Apply
	if apply == nil {
		apply = reconcile.Apply
	}
	applyOpts := opts.ApplyOptions
	applyOpts.ClusterName = opts.ClusterName
	applyOpts.BundlePath = rendered.BundlePath
	if applyOpts.Stderr == nil {
		applyOpts.Stderr = opts.Out
	}
	applied, err := apply(applyOpts)
	if err != nil {
		return nil, markDegraded(opts.ClusterName, "ApplyFailed", err)
	}
	return resultFromApply(opts.ClusterName, applied), nil
}

func normalizeOptions(opts Options) Options {
	opts.ClusterName = strings.TrimSpace(opts.ClusterName)
	if opts.ClusterName == "" {
		opts.ClusterName = "default"
	}
	if opts.Out == nil {
		opts.Out = io.Discard
	}
	return opts
}

func resolveTarget(opts TargetOptions) (*resolvedTarget, error) {
	bomPath := strings.TrimSpace(opts.BOMPath)
	channelPath := strings.TrimSpace(opts.DistributionChannelPath)
	switch {
	case bomPath == "" && channelPath == "":
		return nil, fmt.Errorf("one of BOMPath or DistributionChannelPath is required")
	case bomPath != "" && channelPath != "":
		return nil, fmt.Errorf("use either BOMPath or DistributionChannelPath, not both")
	case channelPath != "":
		resolved, err := bom.ResolveDistributionChannelFile(channelPath)
		if err != nil {
			return nil, err
		}
		return &resolvedTarget{
			bom:                     resolved.BOM,
			bomPath:                 resolved.BOMPath,
			distributionChannel:     resolved.Channel,
			distributionChannelPath: channelPath,
		}, nil
	default:
		doc, err := bom.LoadFile(bomPath)
		if err != nil {
			return nil, err
		}
		return &resolvedTarget{bom: doc, bomPath: bomPath}, nil
	}
}

func materializeOptions(target *resolvedTarget, opts Options) (reconcile.Options, error) {
	localRoots, localPackages, err := resolvePackageSources(target.bom, opts.PackageSources)
	if err != nil {
		return reconcile.Options{}, err
	}

	var repo *localrepo.Repo
	localRepoPath := ""
	if strings.TrimSpace(opts.LocalRepoPath) != "" {
		localRepoPath, err = filepath.Abs(opts.LocalRepoPath)
		if err != nil {
			return reconcile.Options{}, fmt.Errorf("resolve local repo path %q: %w", opts.LocalRepoPath, err)
		}
		repo, err = localrepo.Load(localRepoPath)
		if err != nil {
			return reconcile.Options{}, err
		}
	}

	var fallbackLoader packageformat.Loader
	var fallbackSources hydrate.SourceProvider
	if len(localRoots) < len(target.bom.Spec.Components) {
		if opts.Mounter == nil {
			return reconcile.Options{}, fmt.Errorf("image mounter cannot be nil when BOM components are not fully backed by package sources")
		}
		resolver := &cachedArtifactResolver{
			cache: &ocipackage.Cache{
				Root:    cacheRoot(opts),
				Mounter: opts.Mounter,
			},
		}
		fallbackLoader = resolver
		fallbackSources = resolver
	}

	provenance, err := renderProvenance(target, localRepoPath, repo, opts.LocalPatchRevision, localRoots)
	if err != nil {
		return reconcile.Options{}, err
	}
	return reconcile.Options{
		ClusterName:        opts.ClusterName,
		RenderProvenance:   provenance,
		LocalRepo:          repo,
		LocalPatchRevision: strings.TrimSpace(opts.LocalPatchRevision),
		PackageLoader: packageLoader{
			local:    localPackages,
			fallback: fallbackLoader,
		},
		Sources: &sourceProvider{
			local:    localRoots,
			fallback: fallbackSources,
		},
	}, nil
}

func cacheRoot(opts Options) string {
	if strings.TrimSpace(opts.CacheRoot) != "" {
		return strings.TrimSpace(opts.CacheRoot)
	}
	return filepath.Join(reconcile.DistributionRootPath(opts.ClusterName), "package-cache")
}

func resolvePackageSources(doc *bom.BOM, sources []PackageSource) (map[string]string, map[string]*packageformat.ComponentPackage, error) {
	localRoots := make(map[string]string, len(sources))
	localPackages := make(map[string]*packageformat.ComponentPackage, len(sources))
	if len(sources) == 0 {
		return localRoots, localPackages, nil
	}
	if doc == nil {
		return nil, nil, fmt.Errorf("bom cannot be nil")
	}

	componentIndex := make(map[string]bom.Component, len(doc.Spec.Components))
	for _, component := range doc.Spec.Components {
		componentIndex[component.Name] = component
	}

	for _, source := range sources {
		componentName := strings.TrimSpace(source.Component)
		root := strings.TrimSpace(source.Root)
		if componentName == "" || root == "" {
			return nil, nil, fmt.Errorf("package source must include component and root")
		}
		component, ok := componentIndex[componentName]
		if !ok {
			return nil, nil, fmt.Errorf("component %q not found in BOM", componentName)
		}
		if _, ok := localRoots[componentName]; ok {
			return nil, nil, fmt.Errorf("duplicate package source for component %q", componentName)
		}
		absRoot, err := filepath.Abs(root)
		if err != nil {
			return nil, nil, fmt.Errorf("resolve package source %q: %w", root, err)
		}
		pkg, err := packageformat.LoadDir(absRoot)
		if err != nil {
			return nil, nil, err
		}
		localRoots[componentName] = absRoot
		localPackages[component.Artifact.Reference()] = pkg
	}
	return localRoots, localPackages, nil
}

func renderProvenance(target *resolvedTarget, localRepoPath string, repo *localrepo.Repo, localPatchRevision string, packageSources map[string]string) (hydrate.RenderProvenance, error) {
	provenance := hydrate.RenderProvenance{
		LocalRepoPath:      strings.TrimSpace(localRepoPath),
		LocalPatchRevision: strings.TrimSpace(localPatchRevision),
	}
	if target != nil && target.distributionChannel != nil {
		provenance.DistributionLine = strings.TrimSpace(target.distributionChannel.Spec.Line)
	}
	if target != nil && strings.TrimSpace(target.distributionChannelPath) != "" {
		absChannelPath, err := filepath.Abs(target.distributionChannelPath)
		if err != nil {
			return hydrate.RenderProvenance{}, fmt.Errorf("resolve DistributionChannel path %q: %w", target.distributionChannelPath, err)
		}
		provenance.DistributionChannelPath = absChannelPath
		data, err := os.ReadFile(absChannelPath)
		if err != nil {
			return hydrate.RenderProvenance{}, fmt.Errorf("read DistributionChannel path %q: %w", absChannelPath, err)
		}
		provenance.DistributionChannelDigest = digest.Canonical.FromBytes(data).String()
	}
	if target != nil && strings.TrimSpace(target.bomPath) != "" {
		absBOMPath, err := filepath.Abs(target.bomPath)
		if err != nil {
			return hydrate.RenderProvenance{}, fmt.Errorf("resolve BOM path %q: %w", target.bomPath, err)
		}
		provenance.BOMPath = absBOMPath
		data, err := os.ReadFile(absBOMPath)
		if err != nil {
			return hydrate.RenderProvenance{}, fmt.Errorf("read BOM path %q: %w", absBOMPath, err)
		}
		provenance.BOMDigest = digest.Canonical.FromBytes(data).String()
	}
	if repo != nil {
		provenance.LocalRepoRevision = repo.Revision
	}
	if len(packageSources) > 0 {
		names := make([]string, 0, len(packageSources))
		for name := range packageSources {
			names = append(names, name)
		}
		sort.Strings(names)
		provenance.PackageSources = make([]hydrate.RenderProvenancePackageSource, 0, len(names))
		for _, name := range names {
			sourceDigest, err := hydrate.DigestDirectory(packageSources[name])
			if err != nil {
				return hydrate.RenderProvenance{}, fmt.Errorf("digest package source %q: %w", packageSources[name], err)
			}
			provenance.PackageSources = append(provenance.PackageSources, hydrate.RenderProvenancePackageSource{
				Component: name,
				Path:      packageSources[name],
				Digest:    sourceDigest.String(),
			})
		}
	}
	return provenance, nil
}

func (l packageLoader) Load(image string) (*packageformat.ComponentPackage, error) {
	if pkg, ok := l.local[image]; ok {
		return pkg, nil
	}
	if l.fallback == nil {
		return nil, fmt.Errorf("package source for artifact %q not found", image)
	}
	return l.fallback.Load(image)
}

func (p *sourceProvider) Source(component hydrate.ComponentPlan) (hydrate.Source, error) {
	if root, ok := p.local[component.Name]; ok {
		return hydrate.Source{Root: root}, nil
	}
	if p.fallback == nil {
		return hydrate.Source{}, fmt.Errorf("source for component %q not found", component.Name)
	}
	return p.fallback.Source(component)
}

func (p *sourceProvider) Close() error {
	if p == nil || p.fallback == nil {
		return nil
	}
	closer, ok := p.fallback.(interface{ Close() error })
	if !ok {
		return nil
	}
	return closer.Close()
}

func (r *cachedArtifactResolver) Load(image string) (*packageformat.ComponentPackage, error) {
	if r == nil || r.cache == nil {
		return nil, fmt.Errorf("cached artifact resolver cannot be nil")
	}
	return r.cache.Load(image)
}

func (r *cachedArtifactResolver) Source(component hydrate.ComponentPlan) (hydrate.Source, error) {
	if r == nil || r.cache == nil {
		return hydrate.Source{}, fmt.Errorf("cached artifact resolver cannot be nil")
	}
	if strings.TrimSpace(component.Artifact) == "" {
		return hydrate.Source{}, fmt.Errorf("artifact reference for component %q cannot be empty", component.Name)
	}
	root, _, err := r.cache.Ensure(component.Artifact)
	if err != nil {
		return hydrate.Source{}, fmt.Errorf("cache component %q artifact %q: %w", component.Name, component.Artifact, err)
	}
	return hydrate.Source{Root: root}, nil
}

func resultFromApply(clusterName string, result *reconcile.ApplyResult) *Result {
	if result == nil || result.AppliedRevision == nil {
		return nil
	}
	return &Result{
		ClusterName:        clusterName,
		BOMName:            result.AppliedRevision.Spec.BOM.Name,
		Revision:           result.AppliedRevision.Spec.BOM.Revision,
		Channel:            string(result.AppliedRevision.Spec.BOM.Channel),
		BundlePath:         result.BundlePath,
		DesiredStateDigest: result.DesiredStateDigest,
		AppliedRevision:    state.AppliedRevisionPath(clusterName),
	}
}

func markDegraded(clusterName, reason string, err error) error {
	if isContextError(err) {
		return err
	}
	message := err.Error()
	if _, _, stateErr := state.MarkDegraded(clusterName, reason, message); stateErr != nil {
		return errors.Join(err, stateErr)
	}
	return err
}

func sleepContext(sleep func(context.Context, time.Duration) error, ctx context.Context, d time.Duration) error {
	if sleep != nil {
		return sleep(ctx, d)
	}
	timer := time.NewTimer(d)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}

func isContextError(err error) bool {
	return errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded)
}

func logReconcileError(out io.Writer, err error) {
	if out == nil || err == nil {
		return
	}
	_, _ = fmt.Fprintf(out, "reconcile failed: %v\n", err)
}
