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

package cmd

import (
	"fmt"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"
	"sigs.k8s.io/yaml"

	"github.com/labring/sealos/pkg/apply/processor"
	"github.com/labring/sealos/pkg/buildah"
	"github.com/labring/sealos/pkg/distribution/bom"
	"github.com/labring/sealos/pkg/distribution/hydrate"
	"github.com/labring/sealos/pkg/distribution/packageformat"
	"github.com/labring/sealos/pkg/distribution/reconcile"
	"github.com/labring/sealos/pkg/distribution/state"
)

type syncBuildahFactory func(id string) (buildah.Interface, error)

var newSyncBuildah syncBuildahFactory = buildah.New
var newSyncMountedArtifactResolver = defaultSyncMountedArtifactResolver

func newSyncCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "sync",
		Short: "Experimental distribution workflows for package-based cluster state",
		Args:  cobra.NoArgs,
	}
	cmd.AddCommand(newSyncPackageCmd())
	cmd.AddCommand(newSyncRenderCmd())
	cmd.AddCommand(newSyncApplyCmd())
	return cmd
}

func newSyncRenderCmd() *cobra.Command {
	var flags struct {
		bomFile            string
		clusterName        string
		localPatchRevision string
		packageSources     []string
	}

	cmd := &cobra.Command{
		Use:   "render",
		Short: "Render a BOM into a cluster-local desired-state bundle from package artifacts",
		Long: strings.TrimSpace(`
Render resolves component packages from the BOM's OCI image and digest
references by default.

Use --package-source only as a local development override for individual
components when iterating on package directories in-tree.
`),
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			doc, err := bom.LoadFile(flags.bomFile)
			if err != nil {
				return err
			}

			opts, err := newSyncMaterializeOptions(doc, flags.clusterName, flags.localPatchRevision, flags.packageSources)
			if err != nil {
				return err
			}

			result, err := reconcile.Materialize(doc, opts)
			if err != nil {
				return err
			}

			out := syncRenderOutput{
				ClusterName:        flags.clusterName,
				BOMName:            result.AppliedRevision.Spec.BOM.Name,
				Revision:           result.AppliedRevision.Spec.BOM.Revision,
				Channel:            string(result.AppliedRevision.Spec.BOM.Channel),
				BundlePath:         result.BundlePath,
				DesiredStateDigest: result.DesiredStateDigest,
				AppliedRevision:    state.AppliedRevisionPath(flags.clusterName),
			}
			data, err := yaml.Marshal(out)
			if err != nil {
				return fmt.Errorf("marshal render result: %w", err)
			}
			if _, err := cmd.OutOrStdout().Write(data); err != nil {
				return err
			}
			return nil
		},
	}
	cmd.Flags().StringVarP(&flags.bomFile, "file", "f", "", "path to the BOM file to render")
	cmd.Flags().StringVarP(&flags.clusterName, "cluster", "c", "default", "name of cluster to materialize desired state for")
	cmd.Flags().StringVar(&flags.localPatchRevision, "local-patch-revision", "", "optional local patch revision recorded in applied state")
	cmd.Flags().StringSliceVar(&flags.packageSources, "package-source", nil, "override a BOM component package source as component=dir for local development")
	if err := cmd.MarkFlagRequired("file"); err != nil {
		panic(err)
	}
	return cmd
}

type syncRenderOutput struct {
	ClusterName        string `json:"clusterName" yaml:"clusterName"`
	BOMName            string `json:"bomName" yaml:"bomName"`
	Revision           string `json:"revision" yaml:"revision"`
	Channel            string `json:"channel" yaml:"channel"`
	BundlePath         string `json:"bundlePath" yaml:"bundlePath"`
	DesiredStateDigest string `json:"desiredStateDigest" yaml:"desiredStateDigest"`
	AppliedRevision    string `json:"appliedRevisionPath" yaml:"appliedRevisionPath"`
}

func newSyncApplyCmd() *cobra.Command {
	var flags struct {
		clusterName    string
		bundleDir      string
		kubeconfigPath string
	}

	cmd := &cobra.Command{
		Use:   "apply",
		Short: "Apply a rendered desired-state bundle to a prepared single-node host",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			bundlePath := flags.bundleDir
			if bundlePath == "" {
				bundlePath = reconcile.CurrentBundlePath(flags.clusterName)
			}

			result, err := reconcile.Apply(reconcile.ApplyOptions{
				ClusterName:    flags.clusterName,
				BundlePath:     bundlePath,
				KubeconfigPath: flags.kubeconfigPath,
				Stderr:         cmd.ErrOrStderr(),
			})
			if err != nil {
				return err
			}

			out := syncApplyOutput{
				ClusterName:        flags.clusterName,
				BOMName:            result.Bundle.Spec.BOMName,
				Revision:           result.Bundle.Spec.Revision,
				Channel:            string(result.Bundle.Spec.Channel),
				BundlePath:         result.BundlePath,
				DesiredStateDigest: result.DesiredStateDigest,
				AppliedRevision:    state.AppliedRevisionPath(flags.clusterName),
			}
			data, err := yaml.Marshal(out)
			if err != nil {
				return fmt.Errorf("marshal apply result: %w", err)
			}
			if _, err := cmd.OutOrStdout().Write(data); err != nil {
				return err
			}
			return nil
		},
	}
	cmd.Flags().StringVarP(&flags.clusterName, "cluster", "c", "default", "name of single-node cluster to apply desired state for")
	cmd.Flags().StringVar(&flags.bundleDir, "bundle-dir", "", "path to a rendered bundle directory that matches the cluster rendered state; defaults to the cluster current bundle")
	cmd.Flags().StringVar(&flags.kubeconfigPath, "kubeconfig", "/etc/kubernetes/admin.conf", "path to the admin kubeconfig used for manifest and healthcheck steps")
	return cmd
}

type syncApplyOutput struct {
	ClusterName        string `json:"clusterName" yaml:"clusterName"`
	BOMName            string `json:"bomName" yaml:"bomName"`
	Revision           string `json:"revision" yaml:"revision"`
	Channel            string `json:"channel" yaml:"channel"`
	BundlePath         string `json:"bundlePath" yaml:"bundlePath"`
	DesiredStateDigest string `json:"desiredStateDigest" yaml:"desiredStateDigest"`
	AppliedRevision    string `json:"appliedRevisionPath" yaml:"appliedRevisionPath"`
}

func newSyncMaterializeOptions(doc *bom.BOM, clusterName, localPatchRevision string, packageSources []string) (reconcile.Options, error) {
	localRootsByComponent, localPackagesByArtifact, err := resolveSyncPackageSources(doc, packageSources)
	if err != nil {
		return reconcile.Options{}, err
	}

	var fallbackLoader packageformat.Loader
	var fallbackSources hydrate.SourceProvider
	if len(localRootsByComponent) < len(doc.Spec.Components) {
		fallbackLoader, fallbackSources, err = newSyncMountedArtifactResolver()
		if err != nil {
			return reconcile.Options{}, err
		}
	}

	return reconcile.Options{
		ClusterName:        clusterName,
		LocalPatchRevision: localPatchRevision,
		PackageLoader: syncPackageLoader{
			local:    localPackagesByArtifact,
			fallback: fallbackLoader,
		},
		Sources: &syncSourceProvider{
			local:    localRootsByComponent,
			fallback: fallbackSources,
		},
	}, nil
}

func defaultSyncMountedArtifactResolver() (packageformat.Loader, hydrate.SourceProvider, error) {
	if err := buildah.TrySetupWithDefaults(); err != nil {
		return nil, nil, err
	}
	builder, err := newSyncBuildah("sync")
	if err != nil {
		return nil, nil, err
	}
	mounter := processor.NewPackageImageMounter(builder)
	return packageformat.MountedImageLoader{Mounter: mounter}, hydrate.NewMountedArtifactSourceProvider(mounter), nil
}

func resolveSyncPackageSources(doc *bom.BOM, values []string) (map[string]string, map[string]*packageformat.ComponentPackage, error) {
	localRootsByComponent := make(map[string]string, len(values))
	localPackagesByArtifact := make(map[string]*packageformat.ComponentPackage, len(values))
	if len(values) == 0 {
		return localRootsByComponent, localPackagesByArtifact, nil
	}

	if doc == nil {
		return nil, nil, fmt.Errorf("bom cannot be nil")
	}

	componentIndex := make(map[string]bom.Component, len(doc.Spec.Components))
	for _, component := range doc.Spec.Components {
		componentIndex[component.Name] = component
	}

	for _, value := range values {
		componentName, root, ok := strings.Cut(value, "=")
		if !ok {
			return nil, nil, fmt.Errorf("invalid package source %q, want component=dir", value)
		}
		componentName = strings.TrimSpace(componentName)
		root = strings.TrimSpace(root)
		if componentName == "" || root == "" {
			return nil, nil, fmt.Errorf("invalid package source %q, want component=dir", value)
		}
		component, ok := componentIndex[componentName]
		if !ok {
			return nil, nil, fmt.Errorf("component %q not found in BOM", componentName)
		}
		if _, ok := localRootsByComponent[componentName]; ok {
			return nil, nil, fmt.Errorf("duplicate package source for component %q", componentName)
		}

		absRoot, err := filepath.Abs(root)
		if err != nil {
			return nil, nil, fmt.Errorf("resolve package source %q: %w", value, err)
		}
		pkg, err := packageformat.LoadDir(absRoot)
		if err != nil {
			return nil, nil, err
		}

		localRootsByComponent[componentName] = absRoot
		localPackagesByArtifact[component.Artifact.Reference()] = pkg
	}

	return localRootsByComponent, localPackagesByArtifact, nil
}

type syncPackageLoader struct {
	local    map[string]*packageformat.ComponentPackage
	fallback packageformat.Loader
}

func (l syncPackageLoader) Load(image string) (*packageformat.ComponentPackage, error) {
	if pkg, ok := l.local[image]; ok {
		return pkg, nil
	}
	if l.fallback == nil {
		return nil, fmt.Errorf("package source for artifact %q not found", image)
	}
	return l.fallback.Load(image)
}

type syncSourceProvider struct {
	local    map[string]string
	fallback hydrate.SourceProvider
}

func (p *syncSourceProvider) Source(component hydrate.ComponentPlan) (hydrate.Source, error) {
	if root, ok := p.local[component.Name]; ok {
		return hydrate.Source{Root: root}, nil
	}
	if p.fallback == nil {
		return hydrate.Source{}, fmt.Errorf("source for component %q not found", component.Name)
	}
	return p.fallback.Source(component)
}

func (p *syncSourceProvider) Close() error {
	if p == nil || p.fallback == nil {
		return nil
	}
	closer, ok := p.fallback.(interface{ Close() error })
	if !ok {
		return nil
	}
	return closer.Close()
}
