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
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/labring/sealos/pkg/distribution/hydrate"
	"github.com/labring/sealos/pkg/distribution/packageformat"
)

var runSyncRuntimePreflight = defaultSyncRuntimePreflight

type syncRuntimePreflightOptions struct {
	ClusterName    string
	Bundle         *hydrate.Bundle
	HostRoot       string
	KubeconfigPath string
}

type syncRuntimePreflightOutput struct {
	HostRoot       string                      `json:"hostRoot" yaml:"hostRoot"`
	KubeconfigPath string                      `json:"kubeconfigPath,omitempty" yaml:"kubeconfigPath,omitempty"`
	State          syncPreflightState          `json:"state" yaml:"state"`
	Summary        string                      `json:"summary" yaml:"summary"`
	Blocked        bool                        `json:"blocked" yaml:"blocked"`
	BlockedReasons []string                    `json:"blockedReasons,omitempty" yaml:"blockedReasons,omitempty"`
	Counts         syncRuntimePreflightCounts  `json:"counts" yaml:"counts"`
	Checks         []syncRuntimePreflightCheck `json:"checks,omitempty" yaml:"checks,omitempty"`
}

type syncRuntimePreflightCounts struct {
	Passed   int `json:"passed" yaml:"passed"`
	Warnings int `json:"warnings" yaml:"warnings"`
	Blocked  int `json:"blocked" yaml:"blocked"`
	Skipped  int `json:"skipped" yaml:"skipped"`
}

type syncRuntimePreflightCheck struct {
	Name    string                         `json:"name" yaml:"name"`
	State   syncRuntimePreflightCheckState `json:"state" yaml:"state"`
	Message string                         `json:"message" yaml:"message"`
	Details []string                       `json:"details,omitempty" yaml:"details,omitempty"`
}

type syncRuntimePreflightCheckState string

const (
	syncRuntimePreflightCheckPassed  syncRuntimePreflightCheckState = "Passed"
	syncRuntimePreflightCheckWarning syncRuntimePreflightCheckState = "Warning"
	syncRuntimePreflightCheckBlocked syncRuntimePreflightCheckState = "Blocked"
	syncRuntimePreflightCheckSkipped syncRuntimePreflightCheckState = "Skipped"
)

type syncRuntimeBundleRequirements struct {
	TargetsLocalHost            bool
	RequiresHostMutation        bool
	RequiresKubernetesAPI       bool
	IncludesContainerdRuntime   bool
	IncludesKubernetesBootstrap bool
	IncludesKubeletRootfs       bool
}

func defaultSyncRuntimePreflight(opts syncRuntimePreflightOptions) syncRuntimePreflightOutput {
	hostRoot := syncRuntimeHostRoot(opts.HostRoot)
	kubeconfigPath := strings.TrimSpace(opts.KubeconfigPath)
	if kubeconfigPath == "" {
		kubeconfigPath = "/etc/kubernetes/admin.conf"
	}
	out := syncRuntimePreflightOutput{
		HostRoot:       hostRoot,
		KubeconfigPath: kubeconfigPath,
	}
	if opts.Bundle == nil {
		out.addCheck("bundle", syncRuntimePreflightCheckBlocked, "rendered bundle is not loaded")
		out.finalize()
		return out
	}

	requirements := syncRuntimeRequirementsForBundle(opts.Bundle)
	if !requirements.TargetsLocalHost {
		out.addCheck("local-host", syncRuntimePreflightCheckSkipped, "bundle does not target the local host; remote host runtime checks are not implemented yet")
		out.finalize()
		return out
	}

	out.checkHostRoot(hostRoot)
	out.checkPrivileges(hostRoot, requirements)
	out.checkSystemd(hostRoot, requirements)
	out.checkSwap(hostRoot, requirements)
	existingKubernetesState := out.checkExistingKubernetesState(hostRoot, requirements)
	out.checkPortAvailability(hostRoot, requirements, existingKubernetesState)
	out.checkExistingBinaries(hostRoot, requirements)
	out.checkRequiredClientTools(hostRoot, kubeconfigPath, requirements)
	out.checkServiceState(hostRoot, requirements)
	out.finalize()
	return out
}

func syncRuntimeHostRoot(hostRoot string) string {
	hostRoot = strings.TrimSpace(hostRoot)
	if hostRoot == "" {
		return string(os.PathSeparator)
	}
	return filepath.Clean(hostRoot)
}

func syncRuntimeRequirementsForBundle(bundle *hydrate.Bundle) syncRuntimeBundleRequirements {
	requirements := syncRuntimeBundleRequirements{
		TargetsLocalHost: true,
	}
	if bundle == nil {
		return requirements
	}
	if topology := bundle.Spec.ExecutionTopology.Normalize(); !topology.Empty() && len(topology.AllNodes) > 0 {
		requirements.TargetsLocalHost = false
		for _, host := range topology.AllNodes {
			if syncIsLocalExecutionHost(host) {
				requirements.TargetsLocalHost = true
				break
			}
		}
	}
	if len(bundle.Spec.LocalResources) > 0 {
		requirements.RequiresKubernetesAPI = true
	}
	for _, component := range bundle.Spec.Components {
		componentID := strings.ToLower(strings.TrimSpace(component.Name + " " + component.PackageName))
		if strings.Contains(componentID, "containerd") {
			requirements.IncludesContainerdRuntime = true
		}
		if strings.Contains(componentID, "kubernetes") || strings.Contains(componentID, "kubeadm") {
			requirements.IncludesKubernetesBootstrap = true
		}
		for _, step := range component.Steps {
			if step.Kind != hydrate.StepContent {
				continue
			}
			switch step.ContentType {
			case packageformat.ContentRootfs:
				requirements.RequiresHostMutation = true
				if strings.Contains(componentID, "kubernetes") || strings.Contains(componentID, "kubelet") {
					requirements.IncludesKubeletRootfs = true
				}
			case packageformat.ContentFile:
				requirements.RequiresHostMutation = true
			case packageformat.ContentManifest:
				requirements.RequiresKubernetesAPI = true
			}
		}
	}
	return requirements
}

func (out *syncRuntimePreflightOutput) checkHostRoot(hostRoot string) {
	if hostRoot == string(os.PathSeparator) {
		out.addCheck("host-root", syncRuntimePreflightCheckPassed, "using real host root")
		return
	}
	info, err := os.Stat(hostRoot)
	if err != nil {
		if os.IsNotExist(err) && hostRoot != string(os.PathSeparator) {
			out.addCheck("host-root", syncRuntimePreflightCheckWarning, fmt.Sprintf("custom host root %q does not exist yet; apply may create it while projecting content", hostRoot))
			return
		}
		out.addCheck("host-root", syncRuntimePreflightCheckBlocked, fmt.Sprintf("custom host root %q is not accessible: %v", hostRoot, err))
		return
	}
	if !info.IsDir() {
		out.addCheck("host-root", syncRuntimePreflightCheckBlocked, fmt.Sprintf("custom host root %q is not a directory", hostRoot))
		return
	}
	out.addCheck("host-root", syncRuntimePreflightCheckPassed, fmt.Sprintf("custom host root %q exists", hostRoot))
}

func (out *syncRuntimePreflightOutput) checkPrivileges(hostRoot string, requirements syncRuntimeBundleRequirements) {
	if !requirements.RequiresHostMutation {
		out.addCheck("privileges", syncRuntimePreflightCheckSkipped, "bundle has no local host-mutating content")
		return
	}
	if hostRoot != string(os.PathSeparator) {
		out.addCheck("privileges", syncRuntimePreflightCheckSkipped, "custom host root is used; real host root privilege check is not required")
		return
	}
	if os.Geteuid() != 0 {
		out.addCheck("privileges", syncRuntimePreflightCheckBlocked, "sync apply must run as root when mutating the real host root")
		return
	}
	out.addCheck("privileges", syncRuntimePreflightCheckPassed, "current process has root privileges for host mutation")
}

func (out *syncRuntimePreflightOutput) checkSystemd(hostRoot string, requirements syncRuntimeBundleRequirements) {
	if !requirements.RequiresHostMutation {
		out.addCheck("systemd", syncRuntimePreflightCheckSkipped, "bundle has no host-mutating content that requires service management")
		return
	}
	if hostRoot != string(os.PathSeparator) {
		out.addCheck("systemd", syncRuntimePreflightCheckSkipped, "custom host root is used; live systemd checks are skipped")
		return
	}
	if _, err := exec.LookPath("systemctl"); err != nil {
		out.addCheck("systemd", syncRuntimePreflightCheckBlocked, "systemctl is not available on PATH")
		return
	}
	if _, err := os.Stat("/run/systemd/system"); err != nil {
		out.addCheck("systemd", syncRuntimePreflightCheckBlocked, "systemd does not appear to be PID 1 or is not booted")
		return
	}
	out.addCheck("systemd", syncRuntimePreflightCheckPassed, "systemd is available for service management")
}

func (out *syncRuntimePreflightOutput) checkSwap(hostRoot string, requirements syncRuntimeBundleRequirements) {
	if !requirements.IncludesKubernetesBootstrap {
		out.addCheck("swap", syncRuntimePreflightCheckSkipped, "bundle does not bootstrap Kubernetes")
		return
	}
	if hostRoot != string(os.PathSeparator) {
		out.addCheck("swap", syncRuntimePreflightCheckSkipped, "custom host root is used; live swap checks are skipped")
		return
	}
	data, err := os.ReadFile("/proc/swaps")
	if err != nil {
		out.addCheck("swap", syncRuntimePreflightCheckWarning, fmt.Sprintf("unable to inspect /proc/swaps: %v", err))
		return
	}
	active := activeSwapLines(string(data))
	if len(active) > 0 {
		out.addCheck("swap", syncRuntimePreflightCheckBlocked, "swap is active and kubeadm bootstrap requires swap to be disabled", active...)
		return
	}
	out.addCheck("swap", syncRuntimePreflightCheckPassed, "swap is disabled")
}

func activeSwapLines(data string) []string {
	var active []string
	for index, line := range strings.Split(data, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || index == 0 {
			continue
		}
		active = append(active, line)
	}
	return active
}

func (out *syncRuntimePreflightOutput) checkExistingKubernetesState(hostRoot string, requirements syncRuntimeBundleRequirements) []string {
	if !requirements.IncludesKubernetesBootstrap && !requirements.RequiresKubernetesAPI {
		out.addCheck("kubernetes-state", syncRuntimePreflightCheckSkipped, "bundle does not interact with Kubernetes state")
		return nil
	}
	candidates := []string{
		"/etc/kubernetes/admin.conf",
		"/etc/kubernetes/kubelet.conf",
		"/etc/kubernetes/manifests/kube-apiserver.yaml",
		"/etc/kubernetes/manifests/kube-controller-manager.yaml",
		"/etc/kubernetes/manifests/kube-scheduler.yaml",
		"/var/lib/kubelet/config.yaml",
	}
	var existing []string
	for _, candidate := range candidates {
		if syncRuntimePathExists(hostRoot, candidate) {
			existing = append(existing, candidate)
		}
	}
	if len(existing) > 0 {
		out.addCheck("kubernetes-state", syncRuntimePreflightCheckWarning, "existing Kubernetes node state was detected; apply will reconcile an existing node rather than a fresh host", existing...)
		return existing
	}
	out.addCheck("kubernetes-state", syncRuntimePreflightCheckPassed, "no existing Kubernetes node state detected")
	return nil
}

func (out *syncRuntimePreflightOutput) checkPortAvailability(hostRoot string, requirements syncRuntimeBundleRequirements, existingKubernetesState []string) {
	if !requirements.IncludesKubernetesBootstrap {
		out.addCheck("ports", syncRuntimePreflightCheckSkipped, "bundle does not bootstrap Kubernetes control-plane ports")
		return
	}
	if hostRoot != string(os.PathSeparator) {
		out.addCheck("ports", syncRuntimePreflightCheckSkipped, "custom host root is used; live port checks are skipped")
		return
	}
	ports := []int{6443, 2379, 2380, 10250, 10257, 10259}
	var occupied []string
	for _, port := range ports {
		if !syncRuntimePortAvailable(port) {
			occupied = append(occupied, strconv.Itoa(port))
		}
	}
	if len(occupied) == 0 {
		out.addCheck("ports", syncRuntimePreflightCheckPassed, "Kubernetes bootstrap ports are available")
		return
	}
	if len(existingKubernetesState) > 0 {
		out.addCheck("ports", syncRuntimePreflightCheckWarning, "Kubernetes ports are already in use on an existing node", occupied...)
		return
	}
	out.addCheck("ports", syncRuntimePreflightCheckBlocked, "Kubernetes bootstrap ports are already in use on a host without detected Kubernetes state", occupied...)
}

func syncRuntimePortAvailable(port int) bool {
	listener, err := net.Listen("tcp", net.JoinHostPort("127.0.0.1", strconv.Itoa(port)))
	if err != nil {
		return false
	}
	_ = listener.Close()
	return true
}

func (out *syncRuntimePreflightOutput) checkExistingBinaries(hostRoot string, requirements syncRuntimeBundleRequirements) {
	if !requirements.RequiresHostMutation {
		out.addCheck("host-binaries", syncRuntimePreflightCheckSkipped, "bundle has no host-mutating rootfs or file content")
		return
	}
	var candidates []string
	if requirements.IncludesContainerdRuntime {
		candidates = append(candidates, "/usr/bin/containerd", "/usr/bin/ctr", "/usr/bin/runc")
	}
	if requirements.IncludesKubernetesBootstrap || requirements.IncludesKubeletRootfs {
		candidates = append(candidates, "/usr/bin/kubeadm", "/usr/bin/kubelet", "/usr/bin/kubectl")
	}
	var existing []string
	for _, candidate := range candidates {
		if syncRuntimePathExists(hostRoot, candidate) {
			existing = append(existing, candidate)
		}
	}
	if len(existing) > 0 {
		out.addCheck("host-binaries", syncRuntimePreflightCheckWarning, "existing runtime binaries will be overwritten or reconciled by apply", existing...)
		return
	}
	out.addCheck("host-binaries", syncRuntimePreflightCheckPassed, "no conflicting runtime binaries detected under the selected host root")
}

func (out *syncRuntimePreflightOutput) checkRequiredClientTools(hostRoot, kubeconfigPath string, requirements syncRuntimeBundleRequirements) {
	if !requirements.RequiresKubernetesAPI {
		out.addCheck("kubernetes-client", syncRuntimePreflightCheckSkipped, "bundle does not apply Kubernetes API resources")
		return
	}
	if requirements.IncludesKubernetesBootstrap {
		out.addCheck("kubernetes-client", syncRuntimePreflightCheckSkipped, "bundle bootstraps Kubernetes and is expected to stage kubectl and kubeconfig before applying API resources")
		return
	}
	var blockers []string
	if _, err := exec.LookPath("kubectl"); err != nil {
		blockers = append(blockers, "kubectl is not available on PATH")
	}
	if _, err := os.Stat(kubeconfigPath); err != nil {
		blockers = append(blockers, fmt.Sprintf("kubeconfig %q does not exist", kubeconfigPath))
	}
	if len(blockers) > 0 {
		out.addCheck("kubernetes-client", syncRuntimePreflightCheckBlocked, "Kubernetes API resources require a working kubectl client and kubeconfig", blockers...)
		return
	}
	out.addCheck("kubernetes-client", syncRuntimePreflightCheckPassed, "kubectl and kubeconfig are available for Kubernetes API resources")
}

func (out *syncRuntimePreflightOutput) checkServiceState(hostRoot string, requirements syncRuntimeBundleRequirements) {
	if hostRoot != string(os.PathSeparator) {
		out.addCheck("services", syncRuntimePreflightCheckSkipped, "custom host root is used; live service checks are skipped")
		return
	}
	var services []string
	if requirements.IncludesContainerdRuntime {
		services = append(services, "containerd")
	}
	if requirements.IncludesKubernetesBootstrap || requirements.IncludesKubeletRootfs {
		services = append(services, "kubelet")
	}
	if len(services) == 0 {
		out.addCheck("services", syncRuntimePreflightCheckSkipped, "bundle does not manage known host services")
		return
	}
	if _, err := exec.LookPath("systemctl"); err != nil {
		out.addCheck("services", syncRuntimePreflightCheckSkipped, "systemctl is not available; service state was not inspected")
		return
	}
	var active []string
	for _, service := range services {
		output, err := exec.Command("systemctl", "is-active", service).CombinedOutput() // #nosec G204
		if err == nil && strings.TrimSpace(string(output)) == "active" {
			active = append(active, service)
		}
	}
	if len(active) > 0 {
		out.addCheck("services", syncRuntimePreflightCheckWarning, "managed services are currently active and may be restarted by apply", active...)
		return
	}
	out.addCheck("services", syncRuntimePreflightCheckPassed, "managed services are not active")
}

func syncRuntimePathExists(hostRoot, path string) bool {
	resolved := syncRuntimePath(hostRoot, path)
	_, err := os.Stat(resolved)
	return err == nil
}

func syncRuntimePath(hostRoot, path string) string {
	if hostRoot == "" || hostRoot == string(os.PathSeparator) {
		return filepath.Clean(path)
	}
	cleanedPath := filepath.Clean(path)
	return filepath.Join(hostRoot, strings.TrimPrefix(cleanedPath, string(os.PathSeparator)))
}

func (out *syncRuntimePreflightOutput) addCheck(name string, state syncRuntimePreflightCheckState, message string, details ...string) {
	check := syncRuntimePreflightCheck{
		Name:    name,
		State:   state,
		Message: message,
		Details: append([]string(nil), details...),
	}
	out.Checks = append(out.Checks, check)
	switch state {
	case syncRuntimePreflightCheckPassed:
		out.Counts.Passed++
	case syncRuntimePreflightCheckWarning:
		out.Counts.Warnings++
	case syncRuntimePreflightCheckBlocked:
		out.Counts.Blocked++
		out.BlockedReasons = append(out.BlockedReasons, message)
	case syncRuntimePreflightCheckSkipped:
		out.Counts.Skipped++
	}
}

func (out *syncRuntimePreflightOutput) finalize() {
	out.Blocked = out.Counts.Blocked > 0
	switch {
	case out.Blocked:
		out.State = syncPreflightStateBlocked
		out.Summary = "runtime environment is not ready for apply"
	case out.Counts.Warnings > 0:
		out.State = syncPreflightStateWarning
		out.Summary = "runtime environment passed blocking checks with warnings"
	default:
		out.State = syncPreflightStateReady
		out.Summary = "runtime environment is ready for apply"
	}
}
