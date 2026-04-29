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

func newKubeadmCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "kubeadm",
		Short: "Manage kubeadm-specific cluster state",
	}
	cmd.AddCommand(newKubeadmCacheCmd())
	return cmd
}

func newKubeadmCacheCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "cache",
		Short: "Manage kubeadm cache files",
	}
	cmd.AddCommand(newKubeadmCacheRefreshCmd())
	return cmd
}

func newKubeadmCacheRefreshCmd() *cobra.Command {
	var (
		refreshAll            bool
		refreshToken          bool
		refreshCertificateKey bool
	)

	cmd := &cobra.Command{
		Use:   "refresh",
		Short: "Refresh kubeadm token and certificate-key caches",
		Example: `sealos kubeadm cache refresh --cluster default
sealos kubeadm cache refresh --cluster default --token
sealos kubeadm cache refresh --cluster default --certificate-key`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			cf, rt, err := loadClusterRuntime(clusterName)
			if err != nil {
				return err
			}

			km, ok := rt.(runtime.KubeadmCacheManager)
			if !ok {
				return fmt.Errorf("distribution %q does not support kubeadm cache refresh", cf.GetCluster().GetDistribution())
			}

			refreshToken, refreshCertificateKey = resolveKubeadmCacheRefreshSelection(refreshAll, refreshToken, refreshCertificateKey)
			if err := km.RefreshKubeadmCache(refreshToken, refreshCertificateKey); err != nil {
				return err
			}

			logger.Info("refreshed kubeadm cache files for cluster %s: %s", cf.GetCluster().GetName(), constants.NewPathResolver(cf.GetCluster().GetName()).EtcPath())
			return nil
		},
	}
	cmd.Flags().StringVarP(&clusterName, "cluster", "c", "default", "name of cluster to refresh kubeadm caches for")
	cmd.Flags().BoolVar(&refreshAll, "all", false, "refresh both kubeadm-token.json and kubeadm-certificate-key.txt")
	cmd.Flags().BoolVar(&refreshToken, "token", false, "refresh kubeadm-token.json")
	cmd.Flags().BoolVar(&refreshCertificateKey, "certificate-key", false, "rotate kubeadm-certificate-key.txt and refresh dependent token cache")
	return cmd
}

func resolveKubeadmCacheRefreshSelection(refreshAll, refreshToken, refreshCertificateKey bool) (bool, bool) {
	if refreshAll || (!refreshToken && !refreshCertificateKey) {
		return true, true
	}
	if refreshCertificateKey {
		refreshToken = true
	}
	return refreshToken, refreshCertificateKey
}
