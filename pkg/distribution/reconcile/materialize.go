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

	"github.com/opencontainers/go-digest"
	"sigs.k8s.io/yaml"

	"github.com/labring/sealos/pkg/constants"
	"github.com/labring/sealos/pkg/distribution/bom"
	"github.com/labring/sealos/pkg/distribution/hydrate"
	"github.com/labring/sealos/pkg/distribution/packageformat"
	"github.com/labring/sealos/pkg/distribution/state"
)

const (
	BundlesDirName       = "bundles"
	CurrentBundleDirName = "current"
)

type Options struct {
	ClusterName        string
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

func MaterializeFile(path string, opts Options) (*Result, error) {
	doc, err := bom.LoadFile(path)
	if err != nil {
		return nil, err
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

	resolved, err := doc.ResolveComponentPackages(opts.PackageLoader)
	if err != nil {
		return nil, err
	}
	plan, err := hydrate.BuildPlanFromResolved(doc, resolved)
	if err != nil {
		return nil, err
	}

	renderedBundle, bundlePath, err := materializeBundle(plan, opts.ClusterName, opts.Sources)
	if err != nil {
		return nil, err
	}

	desiredStateDigest, err := hydrate.DigestBundle(bundlePath)
	if err != nil {
		return nil, fmt.Errorf("digest bundle %q: %w", bundlePath, err)
	}

	ref, err := newBOMReference(doc)
	if err != nil {
		return nil, err
	}
	appliedRevision, err := state.PersistSuccessfulApply(
		opts.ClusterName,
		ref,
		desiredStateDigest.String(),
		opts.LocalPatchRevision,
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

func materializeBundle(plan *hydrate.Plan, clusterName string, sources hydrate.SourceProvider) (*hydrate.Bundle, string, error) {
	storePath := BundleStorePath(clusterName)
	if err := os.MkdirAll(storePath, 0o755); err != nil {
		return nil, "", fmt.Errorf("create bundle store %q: %w", storePath, err)
	}

	stagePath, err := os.MkdirTemp(storePath, "render-")
	if err != nil {
		return nil, "", fmt.Errorf("create staged bundle path in %q: %w", storePath, err)
	}
	defer func() {
		_ = os.RemoveAll(stagePath)
	}()

	renderedBundle, err := hydrate.RenderPlan(plan, sources, stagePath)
	if err != nil {
		return nil, "", err
	}

	currentPath := CurrentBundlePath(clusterName)
	if err := promoteBundle(stagePath, currentPath); err != nil {
		return nil, "", err
	}
	return renderedBundle, currentPath, nil
}

func promoteBundle(stagePath, currentPath string) error {
	backupPath := currentPath + ".bak"
	if err := os.RemoveAll(backupPath); err != nil {
		return fmt.Errorf("cleanup backup bundle %q: %w", backupPath, err)
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

func newBOMReference(doc *bom.BOM) (state.BOMReference, error) {
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
		Channel:  doc.Spec.Channel,
		Digest:   digest.Canonical.FromBytes(data).String(),
	}, nil
}
