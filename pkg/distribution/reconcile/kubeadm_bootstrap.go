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
	"net"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/labring/sealos/pkg/distribution/hydrate"
	"github.com/labring/sealos/pkg/distribution/packageformat"
	"github.com/labring/sealos/pkg/runtime/decode"
	kubeadmtypes "github.com/labring/sealos/pkg/runtime/kubernetes/types"
	v1beta1 "github.com/labring/sealos/pkg/types/v1beta1"
	"github.com/labring/sealos/pkg/utils/iputils"
	yamlutil "github.com/labring/sealos/pkg/utils/yaml"
	versionutil "k8s.io/apimachinery/pkg/util/version"
	kubeadmapi "k8s.io/kubernetes/cmd/kubeadm/app/apis/kubeadm"
)

const defaultContainerdPauseImage = "registry.k8s.io/pause:3.8"

type kubeadmJoinMetadata struct {
	apiServerEndpoint string
	token             string
	caCertHashes      []string
	certificateKey    string
}

func (e *bundleExecutor) runBootstrapHooks(component hydrate.RenderedComponent, files contentSet, hooks []hydrate.RenderedStep) error {
	return e.runBootstrapHooksForHosts(component, files, hooks, nil)
}

func (e *bundleExecutor) runBootstrapHooksForHosts(component hydrate.RenderedComponent, files contentSet, hooks []hydrate.RenderedStep, allNodeHosts []string) error {
	for _, hook := range hooks {
		kubeadmConfig, ok := kubeadmBootstrapConfigStep(files)
		if ok && hook.Target == packageformat.TargetAllNodes && e.topology != nil && len(e.nodeExecutionHosts()) > 1 {
			if err := e.runKubeadmBootstrapHook(component, hook, kubeadmConfig, allNodeHosts); err != nil {
				return err
			}
			continue
		}

		if err := e.runHooksForHosts(component, []hydrate.RenderedStep{hook}, allNodeHosts); err != nil {
			return err
		}
	}
	return nil
}

func kubeadmBootstrapConfigStep(files contentSet) (hydrate.RenderedStep, bool) {
	for _, step := range files.files {
		if strings.TrimSpace(step.SourcePath) == "files/etc/kubernetes/kubeadm.yaml" {
			return step, true
		}
	}
	return hydrate.RenderedStep{}, false
}

func (e *bundleExecutor) runKubeadmBootstrapHook(component hydrate.RenderedComponent, hook, kubeadmConfig hydrate.RenderedStep, targetHosts []string) error {
	if targetHosts == nil {
		var err error
		targetHosts, err = e.resolveTargetHosts(hook.Target)
		if err != nil {
			return err
		}
	}
	repairHosts, err := e.joinedControlPlaneRepairHosts(component, e.nodeExecutionHosts())
	if err != nil {
		return err
	}

	firstMasterHosts, err := e.resolveTargetHosts(packageformat.TargetFirstMaster)
	if err != nil {
		return err
	}
	if len(firstMasterHosts) != 1 {
		return fmt.Errorf("kubeadm bootstrap requires exactly one first master, got %d", len(firstMasterHosts))
	}
	firstMasterHost := firstMasterHosts[0]
	firstMasterRepairHost, err := e.joinedFirstMasterRepairHost(component, firstMasterHost, targetHosts)
	if err != nil {
		return err
	}
	if len(targetHosts) == 0 && len(repairHosts) == 0 && firstMasterRepairHost == "" {
		return nil
	}

	if len(targetHosts) > 0 {
		if err := e.runHookOnHost(firstMasterHost, component, hook); err != nil {
			return err
		}
	}

	remainingCapacity := len(targetHosts)
	if remainingCapacity > 0 {
		remainingCapacity--
	}
	remainingHosts := make([]string, 0, remainingCapacity)
	for _, host := range targetHosts {
		if iputils.GetHostIP(host) == iputils.GetHostIP(firstMasterHost) {
			continue
		}
		remainingHosts = append(remainingHosts, host)
	}
	if len(remainingHosts) == 0 {
		if len(repairHosts) == 0 && firstMasterRepairHost == "" {
			return nil
		}
	}

	controlPlaneHosts := append([]string{}, remainingHosts...)
	controlPlaneHosts = append(controlPlaneHosts, repairHosts...)
	if firstMasterRepairHost != "" {
		controlPlaneHosts = append(controlPlaneHosts, firstMasterRepairHost)
	}
	if err := e.ensureStableControlPlaneEndpoint(firstMasterHost, kubeadmConfig, controlPlaneHosts); err != nil {
		return err
	}

	if firstMasterRepairHost != "" {
		if err := e.repairJoinedFirstControlPlane(firstMasterRepairHost); err != nil {
			return err
		}
	}

	if len(remainingHosts) == 0 && len(repairHosts) == 0 {
		return nil
	}

	joinMetadata, err := e.generateKubeadmJoinMetadata(firstMasterHost)
	if err != nil {
		return err
	}
	if err := e.preloadKubeadmBootstrapImages(firstMasterHost, remainingHosts); err != nil {
		return err
	}
	for _, host := range remainingHosts {
		configBytes, err := e.renderKubeadmJoinConfig(kubeadmConfig, host, joinMetadata)
		if err != nil {
			return err
		}
		if err := e.writeKubeadmConfigForHost(host, kubeadmConfig, configBytes); err != nil {
			return err
		}
		if err := e.runHookOnHost(host, component, hook); err != nil {
			return err
		}
	}

	if err := e.repairJoinedControlPlanes(kubeadmConfig, repairHosts, joinMetadata); err != nil {
		return err
	}
	return nil
}

func (e *bundleExecutor) joinedFirstMasterRepairHost(component hydrate.RenderedComponent, firstMasterHost string, freshHosts []string) (string, error) {
	if len(freshHosts) != 0 || e.topology == nil || e.topology.isSingleNode() {
		return "", nil
	}
	if component.PackageName != "kubernetes-rootfs" && component.Name != "kubernetes" {
		return "", nil
	}
	joined, err := e.hostHasKubeletIdentity(firstMasterHost)
	if err != nil {
		return "", err
	}
	if !joined {
		return "", nil
	}
	return firstMasterHost, nil
}

func (e *bundleExecutor) joinedControlPlaneRepairHosts(component hydrate.RenderedComponent, hosts []string) ([]string, error) {
	if len(hosts) == 0 || e.topology == nil || e.topology.isSingleNode() {
		return nil, nil
	}
	if component.PackageName != "kubernetes-rootfs" && component.Name != "kubernetes" {
		return nil, nil
	}

	repairHosts := make([]string, 0, len(hosts))
	for _, host := range hosts {
		if e.topology.isFirstMaster(host) || !e.hostIsControlPlane(host) {
			continue
		}
		joined, err := e.hostHasKubeletIdentity(host)
		if err != nil {
			return nil, err
		}
		if joined {
			repairHosts = append(repairHosts, host)
		}
	}
	return repairHosts, nil
}

func (e *bundleExecutor) repairJoinedControlPlanes(step hydrate.RenderedStep, hosts []string, metadata *kubeadmJoinMetadata) error {
	if len(hosts) == 0 {
		return nil
	}
	for _, host := range hosts {
		configBytes, err := e.renderKubeadmJoinConfig(step, host, metadata)
		if err != nil {
			return err
		}
		if err := e.writeKubeadmConfigForHost(host, step, configBytes); err != nil {
			return err
		}
		e.logf("repairing joined control-plane host %q via kubeadm control-plane-prepare", host)
		if err := e.runCommandOnHost(host, 0, nil, "kubeadm", "join", "phase", "control-plane-prepare", "kubeconfig", "--config", "/etc/kubernetes/kubeadm.yaml"); err != nil {
			return err
		}
		if err := e.runCommandOnHost(host, 0, nil, "kubeadm", "join", "phase", "control-plane-prepare", "control-plane", "--config", "/etc/kubernetes/kubeadm.yaml"); err != nil {
			return err
		}
		if err := e.runCommandOnHost(host, 0, nil, "systemctl", "restart", "kubelet"); err != nil {
			return err
		}
	}
	return nil
}

func (e *bundleExecutor) repairJoinedFirstControlPlane(host string) error {
	e.logf("repairing joined first control-plane host %q via kubeadm init phase", host)
	if err := e.runCommandOnHost(host, 0, nil, "kubeadm", "init", "phase", "kubeconfig", "all", "--config", "/etc/kubernetes/kubeadm.yaml"); err != nil {
		return err
	}
	if err := e.runCommandOnHost(host, 0, nil, "kubeadm", "init", "phase", "control-plane", "all", "--config", "/etc/kubernetes/kubeadm.yaml"); err != nil {
		return err
	}
	if err := e.runShellOnHost(host, 0, nil, strings.Join([]string{
		"if [ -f /etc/kubernetes/admin.conf ]; then",
		"  install -d -m 0700 /root/.kube",
		"  cp -f /etc/kubernetes/admin.conf /root/.kube/config",
		"  chmod 0600 /root/.kube/config",
		"fi",
	}, "\n")); err != nil {
		return err
	}
	if err := e.runCommandOnHost(host, 0, nil, "systemctl", "restart", "kubelet"); err != nil {
		return err
	}
	return nil
}

func (e *bundleExecutor) ensureStableControlPlaneEndpoint(firstMasterHost string, kubeadmConfig hydrate.RenderedStep, remainingHosts []string) error {
	if !containsControlPlaneHost(e, remainingHosts) {
		return nil
	}

	firstMasterIP, err := kubeadmAdvertiseAddressForHost(firstMasterHost)
	if err != nil {
		return err
	}
	controlPlaneEndpoint := fmt.Sprintf("%s:6443", firstMasterIP)
	currentConfig, err := e.outputCommand(0, nil, "kubectl", "--kubeconfig", e.kubeconfigPath, "-n", "kube-system", "get", "cm", "kubeadm-config", "-o", "jsonpath={.data.ClusterConfiguration}")
	if err == nil {
		currentKubeadmConfig, parseErr := loadKubeadmClusterConfigFromString(currentConfig)
		if parseErr == nil {
			if strings.TrimSpace(currentKubeadmConfig.ClusterConfiguration.ControlPlaneEndpoint) != "" {
				controlPlaneEndpoint = strings.TrimSpace(currentKubeadmConfig.ClusterConfiguration.ControlPlaneEndpoint)
			}
		} else {
			e.logf("unable to parse kubeadm cluster configuration, falling back to rendered config upload: %v", parseErr)
		}
	} else {
		e.logf("unable to read kubeadm cluster configuration, falling back to rendered config upload: %v", err)
	}

	configBytes, err := e.renderKubeadmClusterConfig(kubeadmConfig, controlPlaneEndpoint)
	if err != nil {
		return err
	}
	return e.uploadKubeadmClusterConfig(firstMasterHost, configBytes)
}

func containsControlPlaneHost(e *bundleExecutor, hosts []string) bool {
	for _, host := range hosts {
		if e.hostIsControlPlane(host) {
			return true
		}
	}
	return false
}

func (e *bundleExecutor) preloadKubeadmBootstrapImages(firstMasterHost string, remainingHosts []string) error {
	if len(remainingHosts) == 0 {
		return nil
	}
	if !isLocalExecutionHost(firstMasterHost) {
		e.logf("skipping kubeadm image preloading because first master %q is remote", firstMasterHost)
		return nil
	}

	images, err := e.kubeadmBootstrapImages(firstMasterHost)
	if err != nil {
		return err
	}
	if len(images) == 0 {
		return nil
	}

	archivePath, cleanup, err := e.exportKubeadmBootstrapImages(images)
	if err != nil {
		return err
	}
	defer cleanup()

	for _, host := range remainingHosts {
		if err := e.importKubeadmBootstrapImages(host, archivePath); err != nil {
			return err
		}
	}
	return nil
}

func (e *bundleExecutor) kubeadmBootstrapImages(firstMasterHost string) ([]string, error) {
	output, err := e.outputShellOnHost(firstMasterHost, "kubeadm config images list --config /etc/kubernetes/kubeadm.yaml")
	if err != nil {
		return nil, fmt.Errorf("list kubeadm bootstrap images from %q: %w", firstMasterHost, err)
	}

	images := make([]string, 0)
	seen := make(map[string]struct{})
	for _, line := range strings.Split(output, "\n") {
		image := strings.TrimSpace(line)
		if image == "" {
			continue
		}
		if _, ok := seen[image]; ok {
			continue
		}
		seen[image] = struct{}{}
		images = append(images, image)
	}

	pauseImage, err := e.detectContainerdPauseImage(firstMasterHost)
	if err != nil {
		return nil, err
	}
	if _, ok := seen[pauseImage]; !ok {
		images = append(images, pauseImage)
	}
	return images, nil
}

func (e *bundleExecutor) detectContainerdPauseImage(firstMasterHost string) (string, error) {
	output, err := e.outputShellOnHost(firstMasterHost, `if [ -f /etc/containerd/config.toml ]; then value="$(awk -F'"' '/sandbox_image/ {print $2; exit}' /etc/containerd/config.toml)"; if [ -n "$value" ]; then echo "$value"; else echo "`+defaultContainerdPauseImage+`"; fi; else echo "`+defaultContainerdPauseImage+`"; fi`)
	if err != nil {
		return "", fmt.Errorf("detect containerd pause image on %q: %w", firstMasterHost, err)
	}
	pauseImage := strings.TrimSpace(output)
	if pauseImage == "" {
		pauseImage = defaultContainerdPauseImage
	}
	return pauseImage, nil
}

func (e *bundleExecutor) exportKubeadmBootstrapImages(images []string) (string, func(), error) {
	tempDir, err := os.MkdirTemp("", "sealos-kubeadm-images-*")
	if err != nil {
		return "", nil, err
	}
	archivePath := filepath.Join(tempDir, "images.tar")
	args := append([]string{"-n", "k8s.io", "images", "export", archivePath}, images...)
	if err := e.runCommand(0, nil, "ctr", args...); err != nil {
		_ = os.RemoveAll(tempDir)
		return "", nil, fmt.Errorf("export kubeadm bootstrap images: %w", err)
	}
	cleanup := func() {
		_ = os.RemoveAll(tempDir)
	}
	return archivePath, cleanup, nil
}

func (e *bundleExecutor) importKubeadmBootstrapImages(host, archivePath string) error {
	if isLocalExecutionHost(host) {
		return e.runCommand(0, nil, "ctr", "-n", "k8s.io", "images", "import", archivePath)
	}
	if e.remoteExec == nil {
		return fmt.Errorf("remote executor is not configured for host %q", host)
	}

	remotePath := filepath.ToSlash(filepath.Join("/tmp", filepath.Base(archivePath)))
	if err := e.remoteExec.Copy(host, archivePath, remotePath); err != nil {
		return fmt.Errorf("copy kubeadm bootstrap image archive to %q: %w", host, err)
	}
	command := strings.Join([]string{
		"trap 'rm -f " + shellQuote(remotePath) + "' EXIT",
		shellCommand("ctr", "-n", "k8s.io", "images", "import", remotePath),
	}, "\n")
	return e.runShellOnHost(host, 0, nil, command)
}

func (e *bundleExecutor) generateKubeadmJoinMetadata(firstMasterHost string) (*kubeadmJoinMetadata, error) {
	script := strings.Join([]string{
		`KEY="$(kubeadm certs certificate-key)"`,
		`kubeadm init phase upload-certs --upload-certs --certificate-key "$KEY" >/dev/null`,
		`kubeadm token create --print-join-command --certificate-key "$KEY" --ttl 2h`,
	}, "\n")

	deadline := time.Now().Add(e.waitTimeout)
	var lastErr error
	for {
		output, err := e.outputShellOnHost(firstMasterHost, script)
		if err == nil {
			return parseKubeadmJoinMetadata(output)
		}
		lastErr = err
		if time.Now().After(deadline) {
			break
		}
		time.Sleep(2 * time.Second)
	}
	return nil, fmt.Errorf("generate kubeadm join metadata from first master %q: %w", firstMasterHost, lastErr)
}

func (e *bundleExecutor) outputShellOnHost(host, script string) (string, error) {
	if isLocalExecutionHost(host) {
		return e.outputCommand(0, nil, "bash", "-lc", script)
	}
	if e.remoteExec == nil {
		return "", fmt.Errorf("remote executor is not configured for host %q", host)
	}
	output, err := e.remoteExec.CmdToString(host, wrapShellCommand(script, nil), "\n")
	if err != nil {
		return "", fmt.Errorf("run on host %q: %w", host, err)
	}
	return output, nil
}

func parseKubeadmJoinMetadata(output string) (*kubeadmJoinMetadata, error) {
	var joinLine string
	for _, line := range strings.Split(strings.TrimSpace(output), "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "kubeadm join ") {
			joinLine = line
		}
	}
	if joinLine == "" {
		return nil, fmt.Errorf("did not find kubeadm join command in output %q", strings.TrimSpace(output))
	}

	fields := strings.Fields(joinLine)
	if len(fields) < 3 {
		return nil, fmt.Errorf("unexpected kubeadm join command %q", joinLine)
	}

	metadata := &kubeadmJoinMetadata{
		apiServerEndpoint: fields[2],
	}
	for i := 3; i < len(fields); i++ {
		switch fields[i] {
		case "--token":
			if i+1 >= len(fields) {
				return nil, fmt.Errorf("join command %q is missing token value", joinLine)
			}
			metadata.token = fields[i+1]
			i++
		case "--discovery-token-ca-cert-hash":
			if i+1 >= len(fields) {
				return nil, fmt.Errorf("join command %q is missing discovery token CA cert hash value", joinLine)
			}
			metadata.caCertHashes = append(metadata.caCertHashes, fields[i+1])
			i++
		case "--certificate-key":
			if i+1 >= len(fields) {
				return nil, fmt.Errorf("join command %q is missing certificate key value", joinLine)
			}
			metadata.certificateKey = fields[i+1]
			i++
		}
	}
	if metadata.apiServerEndpoint == "" || metadata.token == "" || len(metadata.caCertHashes) == 0 {
		return nil, fmt.Errorf("incomplete kubeadm join metadata parsed from %q", joinLine)
	}
	return metadata, nil
}

func (e *bundleExecutor) renderKubeadmJoinConfig(step hydrate.RenderedStep, host string, metadata *kubeadmJoinMetadata) ([]byte, error) {
	kubeadmConfig, err := e.loadKubeadmConfigFromStep(step)
	if err != nil {
		return nil, err
	}

	hostIP, err := kubeadmAdvertiseAddressForHost(host)
	if err != nil {
		return nil, err
	}
	setOrReplaceKubeadmArg(&kubeadmConfig.JoinConfiguration.NodeRegistration.KubeletExtraArgs, "node-ip", hostIP)
	if kubeadmConfig.JoinConfiguration.Discovery.BootstrapToken == nil {
		kubeadmConfig.JoinConfiguration.Discovery.BootstrapToken = &kubeadmapi.BootstrapTokenDiscovery{}
	}
	kubeadmConfig.JoinConfiguration.Discovery.BootstrapToken.Token = metadata.token
	kubeadmConfig.JoinConfiguration.Discovery.BootstrapToken.CACertHashes = append([]string(nil), metadata.caCertHashes...)
	kubeadmConfig.JoinConfiguration.Discovery.BootstrapToken.APIServerEndpoint = metadata.apiServerEndpoint

	if e.hostIsControlPlane(host) {
		if kubeadmConfig.JoinConfiguration.ControlPlane == nil {
			kubeadmConfig.JoinConfiguration.ControlPlane = &kubeadmapi.JoinControlPlane{}
		}
		kubeadmConfig.JoinConfiguration.ControlPlane.CertificateKey = metadata.certificateKey
		kubeadmConfig.JoinConfiguration.ControlPlane.LocalAPIEndpoint.AdvertiseAddress = hostIP
	} else {
		kubeadmConfig.JoinConfiguration.ControlPlane = nil
	}

	conversion, err := kubeadmConfig.ToConvertedKubeadmConfig()
	if err != nil {
		return nil, err
	}
	return yamlutil.MarshalConfigs(&conversion.JoinConfiguration, &conversion.KubeletConfiguration)
}

func kubeadmAdvertiseAddressForHost(host string) (string, error) {
	hostIP := strings.TrimSpace(iputils.GetHostIP(host))
	if hostIP == "" {
		return "", fmt.Errorf("host %q does not resolve to an advertise address", host)
	}
	if net.ParseIP(hostIP) != nil {
		return hostIP, nil
	}
	addrs, err := net.LookupHost(hostIP)
	if err != nil {
		return "", fmt.Errorf("resolve host %q advertise address: %w", host, err)
	}
	for _, addr := range addrs {
		if ip := net.ParseIP(addr); ip != nil {
			return ip.String(), nil
		}
	}
	return "", fmt.Errorf("resolve host %q advertise address: no IP addresses found", host)
}

func (e *bundleExecutor) renderKubeadmClusterConfig(step hydrate.RenderedStep, controlPlaneEndpoint string) ([]byte, error) {
	kubeadmConfig, err := e.loadKubeadmConfigFromStep(step)
	if err != nil {
		return nil, err
	}
	kubeadmConfig.ClusterConfiguration.ControlPlaneEndpoint = controlPlaneEndpoint

	return marshalKubeadmClusterConfig(kubeadmConfig)
}

func (e *bundleExecutor) loadKubeadmConfigFromStep(step hydrate.RenderedStep) (*kubeadmtypes.KubeadmConfig, error) {
	baseConfigPath, err := e.resolveBundleStepPathForHost(localExecutionHost, step)
	if err != nil {
		return nil, err
	}

	kubeadmConfig, err := kubeadmtypes.LoadKubeadmConfigs(baseConfigPath, false, decode.CRDFromFile)
	if err != nil {
		return nil, err
	}
	if kubeadmConfig == nil {
		return nil, fmt.Errorf("load kubeadm config from %q: no documents decoded", baseConfigPath)
	}
	if apiVersion := strings.TrimSpace(kubeadmConfig.ClusterConfiguration.APIVersion); apiVersion != "" {
		kubeadmConfig.SetAPIVersion(apiVersion)
	}
	kubeadmConfig.SetDefaults()
	sanitizeKubeadmFeatureGates(kubeadmConfig)
	return kubeadmConfig, nil
}

func loadKubeadmClusterConfigFromString(data string) (*kubeadmtypes.KubeadmConfig, error) {
	kubeadmConfig, err := kubeadmtypes.LoadKubeadmConfigs(data, false, decode.CRDFromString)
	if err != nil {
		return nil, err
	}
	if kubeadmConfig == nil {
		return nil, fmt.Errorf("no kubeadm config documents decoded")
	}
	if apiVersion := strings.TrimSpace(kubeadmConfig.ClusterConfiguration.APIVersion); apiVersion != "" {
		kubeadmConfig.SetAPIVersion(apiVersion)
	}
	kubeadmConfig.SetDefaults()
	return kubeadmConfig, nil
}

func marshalKubeadmClusterConfig(kubeadmConfig *kubeadmtypes.KubeadmConfig) ([]byte, error) {
	conversion, err := kubeadmConfig.ToConvertedKubeadmConfig()
	if err != nil {
		return nil, err
	}
	return yamlutil.MarshalConfigs(&conversion.ClusterConfiguration)
}

type removedFeatureGateRule struct {
	name     string
	operator string
	version  string
}

var removedFeatureGateRules = []removedFeatureGateRule{
	{name: "CSIStorageCapacity", operator: "LessThan", version: "v1.21.0"},
	{name: "TTLAfterFinished", operator: "AtLeast", version: "v1.24.0"},
	{name: "EphemeralContainers", operator: "AtLeast", version: "v1.26.0"},
}

func sanitizeKubeadmFeatureGates(kubeadmConfig *kubeadmtypes.KubeadmConfig) bool {
	if kubeadmConfig == nil {
		return false
	}
	version := strings.TrimSpace(kubeadmConfig.ClusterConfiguration.KubernetesVersion)
	if version == "" {
		return false
	}
	changed := false
	changed = sanitizeFeatureGateArgs(&kubeadmConfig.ClusterConfiguration.APIServer.ExtraArgs, version) || changed
	changed = sanitizeFeatureGateArgs(&kubeadmConfig.ClusterConfiguration.ControllerManager.ExtraArgs, version) || changed
	changed = sanitizeFeatureGateArgs(&kubeadmConfig.ClusterConfiguration.Scheduler.ExtraArgs, version) || changed
	changed = sanitizeFeatureGateMap(kubeadmConfig.KubeletConfiguration.FeatureGates, version) || changed
	return changed
}

func sanitizeFeatureGateArgs(args *[]kubeadmapi.Arg, version string) bool {
	if args == nil || len(*args) == 0 {
		return false
	}
	changed := false
	filtered := make([]kubeadmapi.Arg, 0, len(*args))
	for _, arg := range *args {
		if arg.Name != "feature-gates" {
			filtered = append(filtered, arg)
			continue
		}
		sanitized := sanitizeFeatureGateString(arg.Value, version)
		if sanitized != arg.Value {
			changed = true
		}
		if strings.TrimSpace(sanitized) == "" {
			changed = true
			continue
		}
		arg.Value = sanitized
		filtered = append(filtered, arg)
	}
	if len(filtered) != len(*args) {
		changed = true
	}
	*args = filtered
	return changed
}

func sanitizeFeatureGateMap(featureGates map[string]bool, version string) bool {
	if len(featureGates) == 0 {
		return false
	}
	changed := false
	for gate := range removedFeatureGates(version) {
		if _, ok := featureGates[gate]; ok {
			delete(featureGates, gate)
			changed = true
		}
	}
	return changed
}

func sanitizeFeatureGateString(value, version string) string {
	if strings.TrimSpace(value) == "" {
		return ""
	}
	removed := removedFeatureGates(version)
	parts := strings.Split(value, ",")
	filtered := make([]string, 0, len(parts))
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		name := part
		if idx := strings.Index(part, "="); idx >= 0 {
			name = strings.TrimSpace(part[:idx])
		}
		if _, ok := removed[name]; ok {
			continue
		}
		filtered = append(filtered, part)
	}
	return strings.Join(filtered, ",")
}

func removedFeatureGates(version string) map[string]struct{} {
	removed := make(map[string]struct{})
	parsedVersion, err := versionutil.ParseSemantic(version)
	if err != nil {
		return removed
	}
	for _, rule := range removedFeatureGateRules {
		ruleVersion, parseErr := versionutil.ParseSemantic(rule.version)
		if parseErr != nil {
			continue
		}
		switch rule.operator {
		case "LessThan":
			if parsedVersion.LessThan(ruleVersion) {
				removed[rule.name] = struct{}{}
			}
		case "AtLeast":
			if parsedVersion.AtLeast(ruleVersion) {
				removed[rule.name] = struct{}{}
			}
		}
	}
	return removed
}

func setOrReplaceKubeadmArg(args *[]kubeadmapi.Arg, name, value string) {
	if args == nil {
		return
	}
	for i := range *args {
		if (*args)[i].Name == name {
			(*args)[i].Value = value
			return
		}
	}
	*args = append(*args, kubeadmapi.Arg{Name: name, Value: value})
}

func (e *bundleExecutor) hostIsControlPlane(host string) bool {
	if e.topology == nil {
		return false
	}
	for _, role := range e.topology.rolesForHost(host) {
		if role == v1beta1.MASTER {
			return true
		}
	}
	return false
}

func (e *bundleExecutor) writeKubeadmConfigForHost(host string, step hydrate.RenderedStep, data []byte) error {
	hostRel, ok := trimStepPrefix(step.SourcePath, "files/")
	if !ok {
		return fmt.Errorf("file step %q path %q must stay under files/", step.Name, step.SourcePath)
	}
	dst := filepath.Join(string(os.PathSeparator), filepath.FromSlash(hostRel))
	if isLocalExecutionHost(host) {
		dst = filepath.Join(e.hostRoot, filepath.FromSlash(hostRel))
		if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
			return err
		}
		return os.WriteFile(dst, data, 0o644)
	}

	tempFile, err := os.CreateTemp("", "sealos-kubeadm-join-*.yaml")
	if err != nil {
		return err
	}
	defer os.Remove(tempFile.Name())
	if _, err := tempFile.Write(data); err != nil {
		tempFile.Close()
		return err
	}
	if err := tempFile.Close(); err != nil {
		return err
	}
	return e.remoteExec.Copy(host, tempFile.Name(), dst)
}

func (e *bundleExecutor) uploadKubeadmClusterConfig(host string, data []byte) error {
	tempFile, err := os.CreateTemp("", "sealos-kubeadm-cluster-config-*.yaml")
	if err != nil {
		return err
	}
	defer os.Remove(tempFile.Name())
	if _, err := tempFile.Write(data); err != nil {
		tempFile.Close()
		return err
	}
	if err := tempFile.Close(); err != nil {
		return err
	}

	configPath := tempFile.Name()
	cleanup := func() {}
	if !isLocalExecutionHost(host) {
		if e.remoteExec == nil {
			return fmt.Errorf("remote executor is not configured for host %q", host)
		}
		configPath = filepath.ToSlash(filepath.Join("/tmp", filepath.Base(tempFile.Name())))
		if err := e.remoteExec.Copy(host, tempFile.Name(), configPath); err != nil {
			return fmt.Errorf("copy kubeadm cluster config to %q: %w", host, err)
		}
		cleanup = func() {
			_ = e.runShellOnHost(host, 0, nil, "rm -f "+shellQuote(configPath))
		}
	}
	defer cleanup()

	command := strings.Join([]string{
		shellCommand("install", "-m", "0644", configPath, "/etc/kubernetes/kubeadm.yaml"),
		shellCommand("kubeadm", "init", "phase", "upload-config", "kubeadm", "--config", configPath),
	}, "\n")
	return e.runShellOnHost(host, 0, nil, command)
}
