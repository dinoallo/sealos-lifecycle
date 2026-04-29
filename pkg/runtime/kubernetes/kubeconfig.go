/*
Copyright 2022 cuisongliu@qq.com.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package kubernetes

import (
	"context"
	"fmt"
	"os"
	"path/filepath"

	"golang.org/x/sync/errgroup"

	"github.com/labring/sealos/pkg/cert"
)

const copyKubeAdminConfigCommand = `rm -rf $HOME/.kube/config && mkdir -p $HOME/.kube && cp /etc/kubernetes/admin.conf $HOME/.kube/config`

var resolveLocalKubeConfigNodeName = func(k *KubeadmRuntime) (string, error) {
	return k.execHostname(k.getMaster0IPAndPort())
}

func (k *KubeadmRuntime) copyKubeConfigFileToNodes(hosts ...string) error {
	src := k.pathResolver.AdminFile()
	eg, _ := errgroup.WithContext(context.Background())
	for _, node := range hosts {
		node := node
		eg.Go(func() error {
			home, err := k.execer.CmdToString(node, "echo $HOME", "")
			if err != nil {
				return err
			}
			dst := filepath.Join(home, ".kube", "config")
			return k.execer.Copy(node, src, dst)
		})
	}
	return eg.Wait()
}

func (k *KubeadmRuntime) copyMasterKubeConfig(host string) error {
	return k.sshCmdAsync(host, copyKubeAdminConfigCommand)
}

func (k *KubeadmRuntime) RefreshKubeConfigFiles(regenerateAll bool) error {
	nodeName := k.getMaster0IP()
	files := []string{AdminConf}
	if regenerateAll {
		var err error
		nodeName, err = resolveLocalKubeConfigNodeName(k)
		if err != nil {
			return fmt.Errorf("failed to resolve master0 hostname: %w", err)
		}
		files = []string{AdminConf, ControllerConf, SchedulerConf, KubeletConf}
	}
	return k.refreshLocalKubeConfigFiles(nodeName, files...)
}

func (k *KubeadmRuntime) refreshLocalKubeConfigFiles(nodeName string, files ...string) error {
	if err := k.CompleteKubeadmConfig(); err != nil {
		return err
	}
	if err := os.MkdirAll(k.pathResolver.EtcPath(), 0o755); err != nil {
		return err
	}

	// The admin kubeconfig embeds a client cert signed by the cluster CA, so the
	// kubeconfig can be rebuilt locally from the persisted PKI and API endpoint.
	cfg := cert.Config{
		Path:     k.pathResolver.PkiPath(),
		BaseName: "ca",
	}
	for _, name := range files {
		target := filepath.Join(k.pathResolver.EtcPath(), name)
		if err := os.Remove(target); err != nil && !os.IsNotExist(err) {
			return err
		}
		if err := cert.CreateKubeConfigFile(name, k.pathResolver.EtcPath(), cfg, nodeName, k.getClusterAPIServer(), "kubernetes"); err != nil {
			return fmt.Errorf("failed to regenerate %s: %w", name, err)
		}
	}
	return nil
}
