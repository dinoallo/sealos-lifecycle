// Copyright © 2023 sealos.
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

	"github.com/spf13/cobra"

	"github.com/labring/sealos/pkg/constants"
	"github.com/labring/sealos/pkg/runtime"
	"github.com/labring/sealos/pkg/utils/logger"
)

var exampleKubeconfig = `
regenerate the cluster-local admin kubeconfig:
	sealos kubeconfig --cluster default

regenerate all local kubeconfig files under the cluster etc directory:
	sealos kubeconfig --cluster default --all
`

func newKubeconfigCmd() *cobra.Command {
	var regenerateAll bool
	cmd := &cobra.Command{
		Use:     "kubeconfig",
		Short:   "Regenerate local kubeconfig files for a cluster",
		Example: exampleKubeconfig,
		Args:    cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			cf, rt, err := loadClusterRuntime(clusterName)
			if err != nil {
				return err
			}

			km, ok := rt.(runtime.KubeConfigManager)
			if !ok {
				return fmt.Errorf("distribution %q does not support kubeconfig regeneration", cf.GetCluster().GetDistribution())
			}
			if err := km.RefreshKubeConfigFiles(regenerateAll); err != nil {
				return err
			}

			if regenerateAll {
				logger.Info("regenerated kubeconfig files for cluster %s: %s", cf.GetCluster().GetName(), constants.NewPathResolver(cf.GetCluster().GetName()).EtcPath())
			} else {
				logger.Info("regenerated admin kubeconfig for cluster %s: %s", cf.GetCluster().GetName(), constants.NewPathResolver(cf.GetCluster().GetName()).AdminFile())
			}
			return nil
		},
	}
	cmd.Flags().StringVarP(&clusterName, "cluster", "c", "default", "name of cluster to regenerate kubeconfig for")
	cmd.Flags().BoolVar(&regenerateAll, "all", false, "regenerate all kubeconfig files under the cluster etc directory")
	return cmd
}
