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

package cmd

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/spf13/cobra"
	"sigs.k8s.io/yaml"

	"github.com/labring/sealos/pkg/apply/processor"
	"github.com/labring/sealos/pkg/buildah"
	"github.com/labring/sealos/pkg/constants"
	"github.com/labring/sealos/pkg/distribution/agent"
	"github.com/labring/sealos/pkg/distribution/packageformat"
	"github.com/labring/sealos/pkg/distribution/reconcile"
	"github.com/labring/sealos/pkg/system"
	fileutil "github.com/labring/sealos/pkg/utils/file"
	"github.com/labring/sealos/pkg/utils/logger"
)

type buildahFactory func(id string) (buildah.Interface, error)
type imageMounterFactory func(id string) (packageformat.ImageMounter, error)

var (
	debug           bool
	newBuildah      buildahFactory      = buildah.New
	newImageMounter imageMounterFactory = defaultImageMounter
	rootCmd                             = newRootCmd()
)

type lazyImageMounter struct {
	id      string
	factory imageMounterFactory
	mounter packageformat.ImageMounter
}

func Execute() {
	if err := rootCmd.Execute(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func newRootCmd() *cobra.Command {
	var flags struct {
		clusterName             string
		bomFile                 string
		distributionChannelFile string
		localRepo               string
		localPatchRevision      string
		packageSources          []string
		cacheRoot               string
		kubeconfigPath          string
		hostRoot                string
		interval                time.Duration
		once                    bool
		output                  string
		runtimeRoot             string
	}

	cmd := &cobra.Command{
		Use:          "sealos-agent",
		Short:        "Run the Sealos distribution reconcile agent",
		Args:         cobra.NoArgs,
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			if strings.TrimSpace(flags.runtimeRoot) != "" {
				constants.DefaultRuntimeRootDir = strings.TrimSpace(flags.runtimeRoot)
			}
			target, err := targetOptions(flags.bomFile, flags.distributionChannelFile)
			if err != nil {
				return err
			}
			packageSources, err := parsePackageSources(flags.packageSources)
			if err != nil {
				return err
			}
			ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
			defer stop()
			result, err := (agent.Runner{}).Run(ctx, agent.Options{
				ClusterName:        flags.clusterName,
				Target:             target,
				LocalRepoPath:      flags.localRepo,
				LocalPatchRevision: flags.localPatchRevision,
				PackageSources:     packageSources,
				CacheRoot:          flags.cacheRoot,
				Mounter:            &lazyImageMounter{id: "sealos-agent", factory: newImageMounter},
				ApplyOptions: reconcile.ApplyOptions{
					KubeconfigPath: flags.kubeconfigPath,
					HostRoot:       flags.hostRoot,
					Stderr:         cmd.ErrOrStderr(),
				},
				Interval: flags.interval,
				Once:     flags.once,
				Out:      cmd.ErrOrStderr(),
			})
			if result != nil {
				if writeErr := writeOutput(cmd, result, flags.output); writeErr != nil {
					return writeErr
				}
			}
			return err
		},
	}

	cmd.PersistentFlags().BoolVar(&debug, "debug", false, "enable debug logger")
	cmd.Flags().StringVarP(&flags.clusterName, "cluster", "c", "default", "cluster name to reconcile")
	cmd.Flags().StringVarP(&flags.bomFile, "file", "f", "", "path to the BOM file to reconcile")
	cmd.Flags().StringVar(&flags.distributionChannelFile, "distribution-channel", "", "path to a DistributionChannel file to resolve before loading the target BOM")
	cmd.Flags().StringVar(&flags.localRepo, "local-repo", "", "path to a cluster-local repo that provides input bindings during render")
	cmd.Flags().StringVar(&flags.localPatchRevision, "local-patch-revision", "", "optional local patch revision recorded in applied state")
	cmd.Flags().StringSliceVar(&flags.packageSources, "package-source", nil, "override a BOM component package source as component=dir for local development")
	cmd.Flags().StringVar(&flags.cacheRoot, "cache-root", "", "package cache root; defaults under the cluster distribution state")
	cmd.Flags().StringVar(&flags.kubeconfigPath, "kubeconfig", "/etc/kubernetes/admin.conf", "path to the admin kubeconfig used for Kubernetes apply steps")
	cmd.Flags().StringVar(&flags.hostRoot, "host-root", string(os.PathSeparator), "host filesystem root used for host apply steps")
	cmd.Flags().DurationVar(&flags.interval, "interval", time.Minute, "reconcile interval when running continuously")
	cmd.Flags().BoolVar(&flags.once, "once", false, "run one reconcile pass and exit")
	cmd.Flags().StringVar(&flags.output, "output", "yaml", "output format: yaml or json")
	cmd.Flags().StringVar(&flags.runtimeRoot, "runtime-root", "", "override the cluster runtime root used to resolve sync state, bundles, and inventory")
	buildah.RegisterRootCommand(cmd)
	cobra.OnInitialize(onInitialize)
	return cmd
}

func targetOptions(bomPath, distributionChannelPath string) (agent.TargetOptions, error) {
	bomPath = strings.TrimSpace(bomPath)
	distributionChannelPath = strings.TrimSpace(distributionChannelPath)
	switch {
	case bomPath == "" && distributionChannelPath == "":
		return agent.TargetOptions{}, fmt.Errorf("one of --file or --distribution-channel is required")
	case bomPath != "" && distributionChannelPath != "":
		return agent.TargetOptions{}, fmt.Errorf("use either --file or --distribution-channel, not both")
	default:
		return agent.TargetOptions{
			BOMPath:                 bomPath,
			DistributionChannelPath: distributionChannelPath,
		}, nil
	}
}

func parsePackageSources(values []string) ([]agent.PackageSource, error) {
	sources := make([]agent.PackageSource, 0, len(values))
	seen := make(map[string]struct{}, len(values))
	for _, value := range values {
		component, root, ok := strings.Cut(value, "=")
		if !ok {
			return nil, fmt.Errorf("invalid package source %q, want component=dir", value)
		}
		component = strings.TrimSpace(component)
		root = strings.TrimSpace(root)
		if component == "" || root == "" {
			return nil, fmt.Errorf("invalid package source %q, want component=dir", value)
		}
		if _, ok := seen[component]; ok {
			return nil, fmt.Errorf("duplicate package source for component %q", component)
		}
		seen[component] = struct{}{}
		absRoot, err := filepath.Abs(root)
		if err != nil {
			return nil, fmt.Errorf("resolve package source %q: %w", root, err)
		}
		sources = append(sources, agent.PackageSource{
			Component: component,
			Root:      absRoot,
		})
	}
	return sources, nil
}

func writeOutput(cmd *cobra.Command, value any, format string) error {
	switch strings.ToLower(strings.TrimSpace(format)) {
	case "", "yaml":
		data, err := yaml.Marshal(value)
		if err != nil {
			return fmt.Errorf("marshal output as yaml: %w", err)
		}
		_, err = cmd.OutOrStdout().Write(data)
		return err
	case "json":
		data, err := json.MarshalIndent(value, "", "  ")
		if err != nil {
			return fmt.Errorf("marshal output as json: %w", err)
		}
		data = append(data, '\n')
		_, err = cmd.OutOrStdout().Write(data)
		return err
	default:
		return fmt.Errorf("unsupported output format %q, want yaml or json", format)
	}
}

func defaultImageMounter(id string) (packageformat.ImageMounter, error) {
	if err := buildah.TrySetupWithDefaults(); err != nil {
		return nil, err
	}
	builder, err := newBuildah(id)
	if err != nil {
		return nil, err
	}
	return processor.NewPackageImageMounter(builder), nil
}

func (m *lazyImageMounter) Mount(image string) (packageformat.MountedImage, error) {
	realMounter, err := m.real()
	if err != nil {
		return packageformat.MountedImage{}, err
	}
	return realMounter.Mount(image)
}

func (m *lazyImageMounter) Unmount(name string) error {
	realMounter, err := m.real()
	if err != nil {
		return err
	}
	return realMounter.Unmount(name)
}

func (m *lazyImageMounter) real() (packageformat.ImageMounter, error) {
	if m.mounter != nil {
		return m.mounter, nil
	}
	if m.factory == nil {
		return nil, fmt.Errorf("image mounter factory cannot be nil")
	}
	mounter, err := m.factory(m.id)
	if err != nil {
		return nil, err
	}
	m.mounter = mounter
	return m.mounter, nil
}

func onInitialize() {
	val, err := system.Get(system.DataRootConfigKey)
	errExit(err)
	constants.DefaultClusterRootFsDir = val
	val, err = system.Get(system.RuntimeRootConfigKey)
	errExit(err)
	constants.DefaultRuntimeRootDir = val
	errExit(fileutil.MkDirs(constants.LogPath(), constants.WorkDir()))
	logger.CfgConsoleAndFileLogger(debug, constants.LogPath(), "sealos-agent", false)
}

func errExit(err error) {
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
