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
	"path"

	"github.com/labring/sealos/pkg/apply/processor"
	"github.com/labring/sealos/pkg/clusterfile"
	"github.com/labring/sealos/pkg/constants"
	"github.com/labring/sealos/pkg/runtime"
	"github.com/labring/sealos/pkg/runtime/factory"
	fileutils "github.com/labring/sealos/pkg/utils/file"
	"github.com/labring/sealos/pkg/utils/logger"
)

func loadClusterRuntime(clusterName string) (clusterfile.Interface, runtime.Interface, error) {
	processor.SyncNewVersionConfig(clusterName)

	clusterPath := constants.Clusterfile(clusterName)
	pathResolver := constants.NewPathResolver(clusterName)

	var opts []clusterfile.OptionFunc
	if runtimeConfigPath := findRuntimeConfigPath(pathResolver); runtimeConfigPath != "" {
		opts = append(opts, clusterfile.WithCustomRuntimeConfigFiles([]string{runtimeConfigPath}))
	} else {
		logger.Warn("cannot locate the default runtime config file")
	}

	cf := clusterfile.NewClusterFile(clusterPath, opts...)
	if err := cf.Process(); err != nil {
		return nil, nil, err
	}

	rt, err := factory.New(cf.GetCluster(), cf.GetRuntimeConfig())
	if err != nil {
		return nil, nil, err
	}
	return cf, rt, nil
}

func findRuntimeConfigPath(pathResolver constants.PathResolver) string {
	for _, file := range []string{
		path.Join(pathResolver.ConfigsPath(), "kubeadm-init.yaml"),
		path.Join(pathResolver.EtcPath(), "kubeadm-init.yaml"),
		path.Join(pathResolver.ConfigsPath(), "k3s-init.yaml"),
		path.Join(pathResolver.EtcPath(), "k3s-init.yaml"),
	} {
		if fileutils.IsExist(file) {
			return file
		}
	}
	return ""
}
