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
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/opencontainers/go-digest"
	"sigs.k8s.io/yaml"

	"github.com/labring/sealos/pkg/constants"
	"github.com/labring/sealos/pkg/distribution/bom"
	"github.com/labring/sealos/pkg/distribution/hydrate"
	"github.com/labring/sealos/pkg/distribution/localrepo"
	"github.com/labring/sealos/pkg/distribution/ownership"
	"github.com/labring/sealos/pkg/distribution/packageformat"
	"github.com/labring/sealos/pkg/distribution/state"
	yamlutil "github.com/labring/sealos/pkg/utils/yaml"
)

const (
	BundlesDirName       = "bundles"
	CurrentBundleDirName = "current"
	RevisionsDirName     = "revisions"
)

type Options struct {
	ClusterName        string
	Channel            bom.ReleaseChannel
	BOMRoot            string
	RenderProvenance   hydrate.RenderProvenance
	SourcePreflight    *hydrate.SourcePreflight
	ExecutionTopology  hydrate.ExecutionTopology
	LocalRepo          *localrepo.Repo
	LocalPatchRevision string
	PackageLoader      packageformat.Loader
	Sources            hydrate.SourceProvider
}

type Result struct {
	Plan               *hydrate.Plan
	Bundle             *hydrate.Bundle
	BundlePath         string
	DesiredStateDigest string
	AppliedRevision    *state.AppliedRevision
}

func DistributionRootPath(clusterName string) string {
	return filepath.Join(constants.NewPathResolver(clusterName).RunRoot(), state.StoreDirName)
}

func BundleStorePath(clusterName string) string {
	return filepath.Join(DistributionRootPath(clusterName), BundlesDirName)
}

func CurrentBundlePath(clusterName string) string {
	return filepath.Join(BundleStorePath(clusterName), CurrentBundleDirName)
}

func RevisionBundlePath(clusterName, desiredStateDigest string) (string, error) {
	if strings.TrimSpace(clusterName) == "" {
		return "", fmt.Errorf("cluster name cannot be empty")
	}
	d, err := digest.Parse(desiredStateDigest)
	if err != nil {
		return "", fmt.Errorf("parse desired state digest %q: %w", desiredStateDigest, err)
	}
	return filepath.Join(BundleStorePath(clusterName), RevisionsDirName, digestPathName(d)), nil
}

func MaterializeFile(path string, opts Options) (*Result, error) {
	doc, err := bom.LoadFile(path)
	if err != nil {
		return nil, err
	}
	provenance := opts.RenderProvenance
	if provenance.BOMPath == "" {
		absPath, err := filepath.Abs(path)
		if err != nil {
			return nil, fmt.Errorf("resolve BOM path %q: %w", path, err)
		}
		provenance.BOMPath = absPath
	}
	if provenance.BOMDigest == "" {
		data, err := os.ReadFile(provenance.BOMPath)
		if err != nil {
			return nil, fmt.Errorf("read BOM path %q: %w", provenance.BOMPath, err)
		}
		provenance.BOMDigest = digest.Canonical.FromBytes(data).String()
	}
	opts.RenderProvenance = provenance
	if opts.BOMRoot == "" {
		opts.BOMRoot = filepath.Dir(provenance.BOMPath)
	}
	return Materialize(doc, opts)
}

func Materialize(doc *bom.BOM, opts Options) (result *Result, err error) {
	if doc == nil {
		return nil, fmt.Errorf("bom cannot be nil")
	}
	if opts.ClusterName == "" {
		return nil, fmt.Errorf("cluster name cannot be empty")
	}
	if opts.PackageLoader == nil {
		return nil, fmt.Errorf("package loader cannot be nil")
	}
	if opts.Sources == nil {
		return nil, fmt.Errorf("source provider cannot be nil")
	}

	if closer, ok := opts.Sources.(interface{ Close() error }); ok {
		defer func() {
			if closeErr := closer.Close(); closeErr != nil {
				err = errors.Join(err, closeErr)
			}
		}()
	}

	doc.SetRuntimeChannel(opts.Channel)
	resolved, err := doc.ResolveComponentPackages(opts.PackageLoader)
	if err != nil {
		return nil, err
	}
	plan, err := hydrate.BuildPlanFromResolved(doc, resolved)
	if err != nil {
		return nil, err
	}
	if err := attachLocalBindings(plan, opts.BOMRoot, opts.Sources, opts.LocalRepo); err != nil {
		return nil, err
	}

	topology, err := materializeExecutionTopology(opts.ClusterName, opts.ExecutionTopology)
	if err != nil {
		return nil, err
	}
	provenance := opts.RenderProvenance
	provenance.LocalRepoRevision = localRepoRevision(opts.LocalRepo)
	provenance.LocalPatchRevision = opts.LocalPatchRevision
	provenance = provenance.Normalize()
	renderedBundle, stagePath, err := materializeBundle(plan, opts.ClusterName, opts.BOMRoot, opts.Sources, topology, provenance, opts.SourcePreflight)
	if err != nil {
		return nil, err
	}
	stagePromoted := false
	defer func() {
		if !stagePromoted {
			_ = os.RemoveAll(stagePath)
		}
	}()

	desiredStateDigest, err := hydrate.DigestBundle(stagePath)
	if err != nil {
		return nil, fmt.Errorf("digest bundle %q: %w", stagePath, err)
	}

	bundlePath, err := promoteRenderedBundle(stagePath, opts.ClusterName, desiredStateDigest)
	if err != nil {
		return nil, err
	}
	stagePromoted = true

	ref, err := newBOMReference(doc, opts.Channel)
	if err != nil {
		return nil, err
	}
	targetState := appliedRevisionTargetState(provenance, ref)
	appliedRevision, err := state.PersistRenderedStateWithOptions(
		opts.ClusterName,
		ref,
		desiredStateDigest.String(),
		localRepoRevision(opts.LocalRepo),
		opts.LocalPatchRevision,
		state.PersistRevisionOptions{
			RequestedTarget: targetState.Requested,
			ResolvedTarget:  targetState.Resolved,
		},
	)
	if err != nil {
		return nil, err
	}

	return &Result{
		Plan:               plan,
		Bundle:             renderedBundle,
		BundlePath:         bundlePath,
		DesiredStateDigest: desiredStateDigest.String(),
		AppliedRevision:    appliedRevision,
	}, nil
}

func attachLocalBindings(plan *hydrate.Plan, bomRoot string, sources hydrate.SourceProvider, repo *localrepo.Repo) error {
	if plan == nil {
		return nil
	}
	plan.LocalPatchPolicy = ownership.DefaultLocalPatchPolicyDocument().Clone()
	plan.LocalPatchPolicySource = ownership.LocalPatchPolicySourceBuiltInDefault

	repoPolicySelected := false
	if repo != nil {
		if localPatchPolicy := repo.LocalPatchPolicy(); localPatchPolicy != nil {
			plan.LocalPatchPolicy = localPatchPolicy
			plan.LocalPatchPolicySource = ownership.LocalPatchPolicySourceLocalRepo
			repoPolicySelected = true
		}
	}
	if !repoPolicySelected {
		bomPolicy, err := hydrate.LoadBOMLocalPatchPolicy(plan, bomRoot)
		if err != nil {
			return err
		}
		if bomPolicy != nil {
			plan.LocalPatchPolicy = bomPolicy
			plan.LocalPatchPolicySource = ownership.LocalPatchPolicySourceBOM
		} else {
			packagePolicy, err := hydrate.LoadPackageLocalPatchPolicy(plan, sources)
			if err != nil {
				return err
			}
			if packagePolicy != nil {
				plan.LocalPatchPolicy = packagePolicy
				plan.LocalPatchPolicySource = ownership.LocalPatchPolicySourcePackage
			}
		}
	}
	if repo == nil {
		return nil
	}

	resources := repo.Resources()
	if len(resources) > 0 {
		plan.LocalResources = make([]hydrate.LocalResource, 0, len(resources))
		for _, resource := range resources {
			plan.LocalResources = append(plan.LocalResources, hydrate.LocalResource{
				Path:         resource.Path,
				RelativePath: resource.RelativePath,
			})
		}
	}

	for i := range plan.Components {
		component := &plan.Components[i]
		if len(component.Inputs) > 0 {
			bindings := make(map[string]string)
			hostBindings := make(map[string]map[string]string)
			for _, input := range component.Inputs {
				if path, ok := repo.BindingFor(component.Name, input); ok {
					bindings[input.Name] = path
				}
				if perHost := repo.HostBindingsFor(component.Name, input); len(perHost) > 0 {
					hostBindings[input.Name] = perHost
				}
			}
			if len(bindings) > 0 {
				component.InputBindings = bindings
			}
			if len(hostBindings) > 0 {
				component.HostInputBindings = hostBindings
			}
		}

		patches := repo.PatchesFor(component.Name)
		if len(patches) == 0 {
			continue
		}
		component.LocalPatches = make([]hydrate.LocalPatch, 0, len(patches))
		for _, patch := range patches {
			component.LocalPatches = append(component.LocalPatches, hydrate.LocalPatch{
				Path:         patch.Path,
				RelativePath: patch.RelativePath,
			})
		}
	}
	return nil
}

func localRepoRevision(repo *localrepo.Repo) string {
	if repo == nil {
		return ""
	}
	return repo.Revision
}

func materializeExecutionTopology(clusterName string, requested hydrate.ExecutionTopology) (hydrate.ExecutionTopology, error) {
	normalized := requested.Normalize()
	if !normalized.Empty() {
		return normalized, nil
	}
	topology, err := loadApplyExecutionTopology(clusterName)
	if err != nil {
		return hydrate.ExecutionTopology{}, err
	}
	if topology == nil {
		return hydrate.NewSingleNodeExecutionTopology(), nil
	}
	return topology.snapshot(), nil
}

func materializeBundle(plan *hydrate.Plan, clusterName, bomRoot string, sources hydrate.SourceProvider, topology hydrate.ExecutionTopology, provenance hydrate.RenderProvenance, sourcePreflight *hydrate.SourcePreflight) (*hydrate.Bundle, string, error) {
	storePath := BundleStorePath(clusterName)
	if err := os.MkdirAll(storePath, 0o755); err != nil {
		return nil, "", fmt.Errorf("create bundle store %q: %w", storePath, err)
	}

	stagePath, err := os.MkdirTemp(storePath, "render-")
	if err != nil {
		return nil, "", fmt.Errorf("create staged bundle path in %q: %w", storePath, err)
	}
	keepStage := false
	defer func() {
		if !keepStage {
			_ = os.RemoveAll(stagePath)
		}
	}()

	renderedBundle, err := hydrate.RenderPlanWithOptions(plan, sources, stagePath, hydrate.RenderOptions{
		BOMRoot: bomRoot,
	})
	if err != nil {
		return nil, "", err
	}
	renderedBundle.Spec.RenderProvenance = provenance
	renderedBundle.Spec.SourcePreflight = sourcePreflight
	renderedBundle.Spec.ExecutionTopology = topology.Normalize()
	if err := yamlutil.MarshalFile(filepath.Join(stagePath, hydrate.BundleFileName), renderedBundle); err != nil {
		return nil, "", fmt.Errorf("write bundle manifest %q: %w", filepath.Join(stagePath, hydrate.BundleFileName), err)
	}

	keepStage = true
	return renderedBundle, stagePath, nil
}

func promoteRenderedBundle(stagePath, clusterName string, desiredStateDigest digest.Digest) (string, error) {
	revisionPath, err := RevisionBundlePath(clusterName, desiredStateDigest.String())
	if err != nil {
		return "", err
	}
	if _, err := os.Stat(revisionPath); err == nil {
		if err := os.RemoveAll(stagePath); err != nil {
			return "", fmt.Errorf("cleanup duplicate staged bundle %q: %w", stagePath, err)
		}
	} else if errors.Is(err, os.ErrNotExist) {
		if err := promoteBundle(stagePath, revisionPath); err != nil {
			return "", err
		}
	} else {
		return "", err
	}

	currentPath := CurrentBundlePath(clusterName)
	if err := mirrorBundle(revisionPath, currentPath); err != nil {
		return "", err
	}
	return currentPath, nil
}

func promoteBundle(stagePath, currentPath string) error {
	backupPath := currentPath + ".bak"
	if err := os.RemoveAll(backupPath); err != nil {
		return fmt.Errorf("cleanup backup bundle %q: %w", backupPath, err)
	}
	if err := os.MkdirAll(filepath.Dir(currentPath), 0o755); err != nil {
		return fmt.Errorf("create bundle parent %q: %w", filepath.Dir(currentPath), err)
	}

	currentExists := true
	if err := os.Rename(currentPath, backupPath); err != nil {
		if !os.IsNotExist(err) {
			return fmt.Errorf("backup current bundle %q: %w", currentPath, err)
		}
		currentExists = false
	}

	if err := os.Rename(stagePath, currentPath); err != nil {
		if !currentExists {
			return fmt.Errorf("promote staged bundle %q to %q: %w", stagePath, currentPath, err)
		}
		restoreErr := os.Rename(backupPath, currentPath)
		if restoreErr != nil {
			return errors.Join(
				fmt.Errorf("promote staged bundle %q to %q: %w", stagePath, currentPath, err),
				fmt.Errorf("restore previous bundle %q to %q: %w", backupPath, currentPath, restoreErr),
			)
		}
		return fmt.Errorf("promote staged bundle %q to %q: %w", stagePath, currentPath, err)
	}

	if currentExists {
		if err := os.RemoveAll(backupPath); err != nil {
			return fmt.Errorf("cleanup previous bundle backup %q: %w", backupPath, err)
		}
	}
	return nil
}

func mirrorBundle(src, dst string) error {
	stagePath := dst + ".stage"
	if err := os.RemoveAll(stagePath); err != nil {
		return fmt.Errorf("cleanup staged bundle mirror %q: %w", stagePath, err)
	}
	if err := copyDir(src, stagePath); err != nil {
		return fmt.Errorf("copy bundle %q to %q: %w", src, stagePath, err)
	}
	if err := promoteBundle(stagePath, dst); err != nil {
		return err
	}
	return nil
}

func digestPathName(d digest.Digest) string {
	return strings.ReplaceAll(d.String(), ":", "-")
}

func newBOMReference(doc *bom.BOM, channel bom.ReleaseChannel) (state.BOMReference, error) {
	if doc == nil {
		return state.BOMReference{}, fmt.Errorf("bom cannot be nil")
	}
	if err := doc.Validate(); err != nil {
		return state.BOMReference{}, err
	}

	data, err := yaml.Marshal(doc)
	if err != nil {
		return state.BOMReference{}, fmt.Errorf("marshal bom: %w", err)
	}
	return state.BOMReference{
		Name:     doc.Metadata.Name,
		Revision: doc.Spec.Revision,
		Channel:  channel,
		Digest:   digest.Canonical.FromBytes(data).String(),
	}, nil
}

func appliedRevisionTargetState(provenance hydrate.RenderProvenance, ref state.BOMReference) state.TargetState {
	provenance = provenance.Normalize()
	requested := state.RequestedTarget{
		Kind:                 state.TargetKindBOM,
		BOMPath:              provenance.BOMPath,
		BOMDigest:            provenance.BOMDigest,
		ReleaseChannelPath:   provenance.ReleaseChannelPath,
		ReleaseChannelDigest: provenance.ReleaseChannelDigest,
		ReleaseSource:        provenance.ReleaseSource,
		DistributionLine:     provenance.DistributionLine,
		Channel:              bom.ReleaseChannel(provenance.ReleaseChannel),
	}
	if requested.BOMDigest == "" {
		requested.BOMDigest = ref.Digest
	}
	if requested.ReleaseSource != "" {
		requested.Kind = state.TargetKindReleaseChannelLookup
	} else if requested.ReleaseChannelPath != "" || requested.ReleaseChannelDigest != "" {
		requested.Kind = state.TargetKindReleaseChannelFile
	} else if requested.BOMPath == "" && requested.BOMDigest == "" {
		requested.BOMDigest = ref.Digest
	}
	if requested.DistributionLine == "" && requested.Kind != state.TargetKindBOM {
		requested.DistributionLine = ref.Name
	}
	if requested.Channel == "" && requested.Kind != state.TargetKindBOM {
		requested.Channel = ref.Channel
	}

	resolved := state.ResolvedTarget{
		BOM: ref,
	}
	if requested.Kind != state.TargetKindBOM {
		resolved.ReleaseChannel = &state.ReleaseChannelReference{
			DistributionLine: requested.DistributionLine,
			Channel:          requested.Channel,
			TargetRevision:   ref.Revision,
			Source:           requested.ReleaseChannelPath,
			Digest:           requested.ReleaseChannelDigest,
		}
	}
	return state.TargetState{
		Requested: &requested,
		Resolved:  &resolved,
	}
}
