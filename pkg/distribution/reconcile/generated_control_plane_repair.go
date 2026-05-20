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
	"fmt"
	"strings"

	"github.com/labring/sealos/pkg/distribution/hydrate"
	"github.com/labring/sealos/pkg/distribution/packageformat"
)

var repairableGeneratedControlPlaneHostPaths = map[string]struct{}{
	"/etc/kubernetes/manifests/kube-apiserver.yaml":          {},
	"/etc/kubernetes/manifests/kube-controller-manager.yaml": {},
	"/etc/kubernetes/manifests/kube-scheduler.yaml":          {},
}

type RepairGeneratedControlPlaneHostOptions struct {
	ClusterName    string
	BundlePath     string
	KubeconfigPath string
	HostRoot       string
	Host           string
}

func IsRepairableGeneratedControlPlaneHostPath(hostPath string) bool {
	_, ok := repairableGeneratedControlPlaneHostPaths[strings.TrimSpace(hostPath)]
	return ok
}

func RepairGeneratedControlPlaneHost(opts RepairGeneratedControlPlaneHostOptions) error {
	if strings.TrimSpace(opts.ClusterName) == "" {
		return fmt.Errorf("cluster name cannot be empty")
	}
	if strings.TrimSpace(opts.BundlePath) == "" {
		return fmt.Errorf("bundle path cannot be empty")
	}

	host := strings.TrimSpace(opts.Host)
	if host == "" {
		host = localExecutionHost
	}

	bundle, err := LoadBundle(opts.BundlePath)
	if err != nil {
		return err
	}
	_, kubeadmConfigStep, err := findKubernetesKubeadmConfigStep(bundle)
	if err != nil {
		return err
	}

	topology, err := loadApplyExecutionTopology(opts.ClusterName)
	if err != nil {
		return err
	}
	executor := &bundleExecutor{
		bundlePath:     opts.BundlePath,
		clusterName:    opts.ClusterName,
		kubeconfigPath: opts.KubeconfigPath,
		hostRoot:       opts.HostRoot,
		topology:       topology,
	}
	executor.applyDefaults()
	if executor.topology != nil && executor.topology.hasRemoteHosts() {
		executor.remoteExec, err = newApplyRemoteExecutor(executor.topology)
		if err != nil {
			return fmt.Errorf("create remote execution client: %w", err)
		}
	}

	if !executor.hostIsControlPlane(host) {
		return fmt.Errorf("host %q is not a control-plane host", host)
	}
	joined, err := executor.hostHasKubeletIdentity(host)
	if err != nil {
		return err
	}
	if !joined {
		return fmt.Errorf("host %q does not appear to be a joined kubelet node", host)
	}

	firstMasterHosts, err := executor.resolveTargetHosts(packageformat.TargetFirstMaster)
	if err != nil {
		return err
	}
	if len(firstMasterHosts) != 1 {
		return fmt.Errorf("generated control-plane repair requires exactly one first master, got %d", len(firstMasterHosts))
	}
	firstMasterHost := firstMasterHosts[0]

	if err := executor.ensureStableControlPlaneEndpoint(firstMasterHost, kubeadmConfigStep, []string{host}); err != nil {
		return err
	}

	if executor.topology != nil && executor.topology.isFirstMaster(host) {
		return executor.repairJoinedFirstControlPlane(host)
	}

	joinMetadata, err := executor.generateKubeadmJoinMetadata(firstMasterHost)
	if err != nil {
		return err
	}
	return executor.repairJoinedControlPlanes(kubeadmConfigStep, []string{host}, joinMetadata)
}

func findKubernetesKubeadmConfigStep(bundle *hydrate.Bundle) (hydrate.RenderedComponent, hydrate.RenderedStep, error) {
	if bundle == nil {
		return hydrate.RenderedComponent{}, hydrate.RenderedStep{}, fmt.Errorf("bundle cannot be nil")
	}
	for _, component := range bundle.Spec.Components {
		if component.PackageName != "kubernetes-rootfs" && component.Name != "kubernetes" {
			continue
		}
		files, _, err := classifyComponent(component)
		if err != nil {
			return hydrate.RenderedComponent{}, hydrate.RenderedStep{}, err
		}
		if step, ok := kubeadmBootstrapConfigStep(files); ok {
			return component, step, nil
		}
	}
	return hydrate.RenderedComponent{}, hydrate.RenderedStep{}, fmt.Errorf("bundle does not contain a kubernetes kubeadm bootstrap config step")
}
