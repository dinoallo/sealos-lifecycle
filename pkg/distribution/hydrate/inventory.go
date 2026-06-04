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

package hydrate

import (
	"bytes"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"

	"k8s.io/apimachinery/pkg/runtime"
	utilyaml "k8s.io/apimachinery/pkg/util/yaml"
	"sigs.k8s.io/yaml"

	"github.com/labring/sealos/pkg/distribution/packageformat"
)

type InventoryOwnership string

const (
	InventoryOwnershipGlobal InventoryOwnership = "global"
	InventoryOwnershipLocal  InventoryOwnership = "local"
)

type InventorySource string

const (
	InventorySourcePackageManifest InventorySource = "packageManifest"
	InventorySourceLocalPatch      InventorySource = "localPatch"
	InventorySourceLocalResource   InventorySource = "localResource"
	InventorySourceLocalInput      InventorySource = "localInput"
	InventorySourceGeneratedHook   InventorySource = "generatedHook"
)

type TrackedK8sObject struct {
	APIVersion string             `json:"apiVersion" yaml:"apiVersion"`
	Kind       string             `json:"kind" yaml:"kind"`
	Namespace  string             `json:"namespace,omitempty" yaml:"namespace,omitempty"`
	Name       string             `json:"name" yaml:"name"`
	Path       string             `json:"path" yaml:"path"`
	Component  string             `json:"component,omitempty" yaml:"component,omitempty"`
	Source     InventorySource    `json:"source" yaml:"source"`
	Ownership  InventoryOwnership `json:"ownership" yaml:"ownership"`
}

type HostPathType string

const (
	HostPathRegularFile HostPathType = "regularFile"
	HostPathSymlink     HostPathType = "symlink"
)

type HostPathProjectionClass string

const (
	HostPathProjectionClassDirect    HostPathProjectionClass = "hostPath"
	HostPathProjectionClassGenerated HostPathProjectionClass = "generatedHostPath"
)

type HostPathCompareStrategy string

const (
	HostPathCompareStrategyBytewiseFile      HostPathCompareStrategy = "bytewiseFile"
	HostPathCompareStrategySemanticGenerated HostPathCompareStrategy = "semanticGeneratedFile"
)

type GeneratedHostPathSemantics struct {
	Tool                 string            `json:"tool,omitempty" yaml:"tool,omitempty"`
	Hook                 string            `json:"hook,omitempty" yaml:"hook,omitempty"`
	APIVersion           string            `json:"apiVersion,omitempty" yaml:"apiVersion,omitempty"`
	Kind                 string            `json:"kind,omitempty" yaml:"kind,omitempty"`
	Namespace            string            `json:"namespace,omitempty" yaml:"namespace,omitempty"`
	Name                 string            `json:"name,omitempty" yaml:"name,omitempty"`
	ContainerName        string            `json:"containerName,omitempty" yaml:"containerName,omitempty"`
	ExpectedImage        string            `json:"expectedImage,omitempty" yaml:"expectedImage,omitempty"`
	ExpectedCommand      string            `json:"expectedCommand,omitempty" yaml:"expectedCommand,omitempty"`
	ExpectedArgs         map[string]string `json:"expectedArgs,omitempty" yaml:"expectedArgs,omitempty"`
	ExpectedVolumeMounts []string          `json:"expectedVolumeMounts,omitempty" yaml:"expectedVolumeMounts,omitempty"`
}

type TrackedHostPath struct {
	HostPath          string                      `json:"hostPath" yaml:"hostPath"`
	BundlePath        string                      `json:"bundlePath" yaml:"bundlePath"`
	Component         string                      `json:"component,omitempty" yaml:"component,omitempty"`
	Source            InventorySource             `json:"source" yaml:"source"`
	Ownership         InventoryOwnership          `json:"ownership" yaml:"ownership"`
	Type              HostPathType                `json:"type" yaml:"type"`
	ProjectionClass   HostPathProjectionClass     `json:"projectionClass,omitempty" yaml:"projectionClass,omitempty"`
	CompareStrategy   HostPathCompareStrategy     `json:"compareStrategy,omitempty" yaml:"compareStrategy,omitempty"`
	InputName         string                      `json:"inputName,omitempty" yaml:"inputName,omitempty"`
	HostInputBindings map[string]string           `json:"hostInputBindings,omitempty" yaml:"hostInputBindings,omitempty"`
	Generated         *GeneratedHostPathSemantics `json:"generated,omitempty" yaml:"generated,omitempty"`
}

func collectTrackedK8sObjects(bundle *Bundle, outputDir string) ([]TrackedK8sObject, error) {
	if bundle == nil {
		return nil, nil
	}

	objects := make([]TrackedK8sObject, 0)
	for _, component := range bundle.Spec.Components {
		for _, step := range component.Steps {
			if step.Kind != StepContent || step.ContentType != "manifest" {
				continue
			}
			resolved, err := resolveInventoryBundlePath(outputDir, step.BundlePath)
			if err != nil {
				return nil, err
			}
			discovered, err := discoverTrackedObjects(resolved, step.BundlePath, component.Name, InventorySourcePackageManifest, InventoryOwnershipGlobal)
			if err != nil {
				return nil, err
			}
			objects = append(objects, discovered...)
		}
		for _, patchPath := range component.LocalPatches {
			resolved, err := resolveInventoryBundlePath(outputDir, patchPath)
			if err != nil {
				return nil, err
			}
			discovered, err := discoverTrackedObjects(resolved, patchPath, component.Name, InventorySourceLocalPatch, InventoryOwnershipLocal)
			if err != nil {
				return nil, err
			}
			objects = append(objects, discovered...)
		}
	}

	for _, resourcePath := range bundle.Spec.LocalResources {
		resolved, err := resolveInventoryBundlePath(outputDir, resourcePath)
		if err != nil {
			return nil, err
		}
		discovered, err := discoverTrackedObjects(resolved, resourcePath, "", InventorySourceLocalResource, InventoryOwnershipLocal)
		if err != nil {
			return nil, err
		}
		objects = append(objects, discovered...)
	}

	sort.Slice(objects, func(i, j int) bool {
		if objects[i].Path != objects[j].Path {
			return objects[i].Path < objects[j].Path
		}
		if objects[i].Namespace != objects[j].Namespace {
			return objects[i].Namespace < objects[j].Namespace
		}
		if objects[i].Kind != objects[j].Kind {
			return objects[i].Kind < objects[j].Kind
		}
		return objects[i].Name < objects[j].Name
	})
	return objects, nil
}

func collectTrackedHostPaths(bundle *Bundle, outputDir string) ([]TrackedHostPath, error) {
	if bundle == nil {
		return nil, nil
	}

	paths := make([]TrackedHostPath, 0)
	for _, component := range bundle.Spec.Components {
		inputOwners := localInputOwners(component)
		for _, step := range component.Steps {
			if step.Kind != StepContent {
				continue
			}
			switch step.ContentType {
			case "rootfs":
				hostRel, ok := trimStepPrefix(step.SourcePath, "rootfs/")
				if !ok {
					return nil, fmt.Errorf("rootfs step %q path %q must stay under rootfs/", step.Name, step.SourcePath)
				}
				resolved, err := resolveInventoryBundlePath(outputDir, step.BundlePath)
				if err != nil {
					return nil, err
				}
				discovered, err := discoverTrackedRootfsHostPaths(resolved, step.BundlePath, hostRel, component.Name, InventorySourcePackageManifest, InventoryOwnershipGlobal)
				if err != nil {
					return nil, err
				}
				paths = append(paths, discovered...)
			case "file":
				hostRel, ok := trimStepPrefix(step.SourcePath, "files/")
				if !ok {
					return nil, fmt.Errorf("file step %q path %q must stay under files/", step.Name, step.SourcePath)
				}
				resolved, err := resolveInventoryBundlePath(outputDir, step.BundlePath)
				if err != nil {
					return nil, err
				}
				source := InventorySourcePackageManifest
				ownership := InventoryOwnershipGlobal
				inputName := ""
				if localInput, ok := inputOwners[step.SourcePath]; ok {
					source = InventorySourceLocalInput
					ownership = InventoryOwnershipLocal
					inputName = localInput
				}
				discovered, err := discoverTrackedFileHostPaths(resolved, step.BundlePath, hostRel, component.Name, InventorySourcePackageManifest, InventoryOwnershipGlobal)
				if err != nil {
					return nil, err
				}
				for i := range discovered {
					discovered[i].Source = source
					discovered[i].Ownership = ownership
					discovered[i].InputName = inputName
					if inputName != "" && len(component.HostInputBindings[inputName]) > 0 {
						discovered[i].HostInputBindings = cloneStringMap(component.HostInputBindings[inputName])
					}
				}
				paths = append(paths, discovered...)
			}
		}
	}
	paths = append(paths, collectKnownGeneratedHostPaths(bundle, outputDir)...)

	sort.Slice(paths, func(i, j int) bool {
		if paths[i].HostPath != paths[j].HostPath {
			return paths[i].HostPath < paths[j].HostPath
		}
		return paths[i].BundlePath < paths[j].BundlePath
	})
	return paths, nil
}

func discoverTrackedRootfsHostPaths(resolvedPath, bundlePath, hostRel, component string, source InventorySource, ownership InventoryOwnership) ([]TrackedHostPath, error) {
	cleanHostRel := filepath.ToSlash(strings.TrimPrefix(filepath.Clean(filepath.FromSlash(hostRel)), string(os.PathSeparator)))
	return discoverTrackedHostPathEntries(resolvedPath, bundlePath, component, source, ownership, func(rel string) string {
		if cleanHostRel == "" {
			return filepath.Join(string(os.PathSeparator), filepath.FromSlash(rel))
		}
		if rel == "" {
			return filepath.Join(string(os.PathSeparator), filepath.FromSlash(cleanHostRel))
		}
		return filepath.Join(string(os.PathSeparator), filepath.FromSlash(cleanHostRel), filepath.FromSlash(rel))
	})
}

func discoverTrackedFileHostPaths(resolvedPath, bundlePath, hostRel, component string, source InventorySource, ownership InventoryOwnership) ([]TrackedHostPath, error) {
	cleanHostRel := filepath.ToSlash(strings.TrimPrefix(filepath.Clean(filepath.FromSlash(hostRel)), string(os.PathSeparator)))
	return discoverTrackedHostPathEntries(resolvedPath, bundlePath, component, source, ownership, func(rel string) string {
		if rel == "" {
			return filepath.Join(string(os.PathSeparator), filepath.FromSlash(cleanHostRel))
		}
		return filepath.Join(string(os.PathSeparator), filepath.FromSlash(cleanHostRel), filepath.FromSlash(rel))
	})
}

func discoverTrackedHostPathEntries(resolvedPath, bundlePath, component string, source InventorySource, ownership InventoryOwnership, hostPath func(rel string) string) ([]TrackedHostPath, error) {
	info, err := os.Lstat(resolvedPath)
	if err != nil {
		return nil, err
	}

	if !info.IsDir() {
		entryType, ok := trackedHostPathType(info.Mode())
		if !ok {
			return nil, nil
		}
		return []TrackedHostPath{{
			HostPath:        filepath.ToSlash(hostPath("")),
			BundlePath:      bundlePath,
			Component:       component,
			Source:          source,
			Ownership:       ownership,
			Type:            entryType,
			ProjectionClass: HostPathProjectionClassDirect,
			CompareStrategy: HostPathCompareStrategyBytewiseFile,
		}}, nil
	}

	paths := make([]TrackedHostPath, 0)
	if err := filepath.WalkDir(resolvedPath, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.IsDir() {
			return nil
		}
		rel, err := filepath.Rel(resolvedPath, path)
		if err != nil {
			return err
		}
		info, err := entry.Info()
		if err != nil {
			return err
		}
		entryType, ok := trackedHostPathType(info.Mode())
		if !ok {
			return nil
		}
		paths = append(paths, TrackedHostPath{
			HostPath:        filepath.ToSlash(hostPath(filepath.ToSlash(rel))),
			BundlePath:      filepath.ToSlash(filepath.Join(bundlePath, rel)),
			Component:       component,
			Source:          source,
			Ownership:       ownership,
			Type:            entryType,
			ProjectionClass: HostPathProjectionClassDirect,
			CompareStrategy: HostPathCompareStrategyBytewiseFile,
		})
		return nil
	}); err != nil {
		return nil, err
	}
	return paths, nil
}

func collectKnownGeneratedHostPaths(bundle *Bundle, outputDir string) []TrackedHostPath {
	if bundle == nil {
		return nil
	}

	paths := make([]TrackedHostPath, 0, len(bundle.Spec.Components))
	for _, component := range bundle.Spec.Components {
		declared := declaredGeneratedHostPaths(component)
		paths = append(paths, declared...)
		seen := make(map[string]struct{}, len(declared))
		for _, path := range declared {
			seen[path.HostPath] = struct{}{}
		}
		discovered := generatedKubeadmStaticPodManifests(component, outputDir)
		for _, path := range discovered {
			if _, ok := seen[path.HostPath]; ok {
				continue
			}
			paths = append(paths, path)
		}
	}
	return paths
}

func declaredGeneratedHostPaths(component RenderedComponent) []TrackedHostPath {
	if len(component.GeneratedOutputs.HostPaths) == 0 {
		return nil
	}
	paths := make([]TrackedHostPath, 0, len(component.GeneratedOutputs.HostPaths))
	for _, output := range component.GeneratedOutputs.HostPaths {
		semantics := generatedHostPathSemanticsFromPackageOutput(output)
		paths = append(paths, TrackedHostPath{
			HostPath:        filepath.ToSlash(output.HostPath),
			Component:       component.Name,
			Source:          InventorySourceGeneratedHook,
			Ownership:       InventoryOwnershipGlobal,
			Type:            HostPathRegularFile,
			ProjectionClass: HostPathProjectionClassGenerated,
			CompareStrategy: HostPathCompareStrategySemanticGenerated,
			Generated:       &semantics,
		})
	}
	return paths
}

func generatedHostPathSemanticsFromPackageOutput(output packageformat.GeneratedHostPathOutput) GeneratedHostPathSemantics {
	return GeneratedHostPathSemantics{
		Tool:                 strings.TrimSpace(output.Tool),
		Hook:                 strings.TrimSpace(output.Hook),
		APIVersion:           strings.TrimSpace(output.APIVersion),
		Kind:                 strings.TrimSpace(output.Kind),
		Namespace:            strings.TrimSpace(output.Namespace),
		Name:                 strings.TrimSpace(output.ObjectName),
		ContainerName:        strings.TrimSpace(output.ContainerName),
		ExpectedImage:        strings.TrimSpace(output.ExpectedImage),
		ExpectedCommand:      strings.TrimSpace(output.ExpectedCommand),
		ExpectedArgs:         cloneStringMap(output.ExpectedArgs),
		ExpectedVolumeMounts: append([]string(nil), output.ExpectedVolumeMounts...),
	}
}

func generatedKubeadmStaticPodManifests(component RenderedComponent, outputDir string) []TrackedHostPath {
	hasBootstrapHook := false
	kubeadmConfigPath := ""
	for _, step := range component.Steps {
		switch {
		case step.Kind == StepHook &&
			step.HookPhase == packageformat.PhaseBootstrap &&
			step.SourcePath == "hooks/bootstrap.sh":
			hasBootstrapHook = true
		case step.Kind == StepContent &&
			step.ContentType == packageformat.ContentFile &&
			step.SourcePath == "files/etc/kubernetes/kubeadm.yaml":
			kubeadmConfigPath = step.BundlePath
		}
	}
	if kubeadmConfigPath == "" {
		for _, input := range component.Inputs {
			if input.Path == "files/etc/kubernetes/kubeadm.yaml" {
				kubeadmConfigPath = renderedBundlePathForSource(component, input.Path)
				break
			}
		}
	}
	if !hasBootstrapHook || kubeadmConfigPath == "" {
		return nil
	}

	expectations, ok := expectedKubeadmStaticPodSemantics(outputDir, kubeadmConfigPath, component.Version)
	if !ok || len(expectations) == 0 {
		return nil
	}

	paths := make([]TrackedHostPath, 0, len(expectations))
	for _, expectation := range expectations {
		paths = append(paths, TrackedHostPath{
			HostPath:        expectation.HostPath,
			Component:       component.Name,
			Source:          InventorySourceGeneratedHook,
			Ownership:       InventoryOwnershipGlobal,
			Type:            HostPathRegularFile,
			ProjectionClass: HostPathProjectionClassGenerated,
			CompareStrategy: HostPathCompareStrategySemanticGenerated,
			Generated:       expectation.Semantics,
		})
	}
	return paths
}

func renderedBundlePathForSource(component RenderedComponent, sourcePath string) string {
	for _, step := range component.Steps {
		if step.Kind != StepContent || step.SourcePath != sourcePath {
			continue
		}
		return step.BundlePath
	}
	return ""
}

type kubeadmClusterConfiguration struct {
	KubernetesVersion string `yaml:"kubernetesVersion"`
	ImageRepository   string `yaml:"imageRepository"`
	Networking        struct {
		PodSubnet     string `yaml:"podSubnet"`
		ServiceSubnet string `yaml:"serviceSubnet"`
	} `yaml:"networking"`
	APIServer         kubeadmControlPlaneComponent `yaml:"apiServer"`
	ControllerManager kubeadmControlPlaneComponent `yaml:"controllerManager"`
	Scheduler         kubeadmControlPlaneComponent `yaml:"scheduler"`
}

type generatedStaticPodExpectation struct {
	HostPath  string
	Semantics *GeneratedHostPathSemantics
}

type kubeadmControlPlaneComponent struct {
	ExtraArgs    interface{} `yaml:"extraArgs"`
	ExtraVolumes interface{} `yaml:"extraVolumes"`
}

func expectedKubeadmStaticPodSemantics(outputDir, bundlePath, componentVersion string) ([]generatedStaticPodExpectation, bool) {
	resolved, err := resolveInventoryBundlePath(outputDir, bundlePath)
	if err != nil {
		return nil, false
	}
	data, err := os.ReadFile(resolved)
	if err != nil {
		return nil, false
	}
	var config kubeadmClusterConfiguration
	if err := yaml.Unmarshal(data, &config); err != nil {
		return nil, false
	}
	version := strings.TrimSpace(config.KubernetesVersion)
	if version == "" {
		version = strings.TrimSpace(componentVersion)
	}
	if version == "" {
		return nil, false
	}
	repository := strings.TrimSpace(config.ImageRepository)
	if repository == "" {
		repository = "registry.k8s.io"
	}
	repository = strings.TrimSuffix(repository, "/")

	expectations := make([]generatedStaticPodExpectation, 0, 3)

	apiserverArgs := cleanExpectedArgs(config.APIServer.ExtraArgs)
	if serviceSubnet := strings.TrimSpace(config.Networking.ServiceSubnet); serviceSubnet != "" {
		if apiserverArgs == nil {
			apiserverArgs = make(map[string]string, 1)
		}
		apiserverArgs["service-cluster-ip-range"] = serviceSubnet
	}

	expectations = append(expectations, generatedStaticPodExpectation{
		HostPath: "/etc/kubernetes/manifests/kube-apiserver.yaml",
		Semantics: &GeneratedHostPathSemantics{
			Tool:                 "kubeadm",
			Hook:                 "bootstrap",
			APIVersion:           "v1",
			Kind:                 "Pod",
			Namespace:            "kube-system",
			Name:                 "kube-apiserver",
			ContainerName:        "kube-apiserver",
			ExpectedImage:        fmt.Sprintf("%s/kube-apiserver:%s", repository, version),
			ExpectedCommand:      "kube-apiserver",
			ExpectedArgs:         apiserverArgs,
			ExpectedVolumeMounts: expectedMounts([]string{"/etc/kubernetes/pki"}, apiserverArgs, config.APIServer.ExtraVolumes),
		},
	})

	controllerManagerArgs := cleanExpectedArgs(config.ControllerManager.ExtraArgs)
	if podSubnet := strings.TrimSpace(config.Networking.PodSubnet); podSubnet != "" {
		if controllerManagerArgs == nil {
			controllerManagerArgs = make(map[string]string, 1)
		}
		controllerManagerArgs["cluster-cidr"] = podSubnet
	}
	expectations = append(expectations, generatedStaticPodExpectation{
		HostPath: "/etc/kubernetes/manifests/kube-controller-manager.yaml",
		Semantics: &GeneratedHostPathSemantics{
			Tool:                 "kubeadm",
			Hook:                 "bootstrap",
			APIVersion:           "v1",
			Kind:                 "Pod",
			Namespace:            "kube-system",
			Name:                 "kube-controller-manager",
			ContainerName:        "kube-controller-manager",
			ExpectedImage:        fmt.Sprintf("%s/kube-controller-manager:%s", repository, version),
			ExpectedCommand:      "kube-controller-manager",
			ExpectedArgs:         controllerManagerArgs,
			ExpectedVolumeMounts: expectedMounts([]string{"/etc/kubernetes/pki"}, controllerManagerArgs, config.ControllerManager.ExtraVolumes),
		},
	})

	schedulerArgs := cleanExpectedArgs(config.Scheduler.ExtraArgs)
	expectations = append(expectations, generatedStaticPodExpectation{
		HostPath: "/etc/kubernetes/manifests/kube-scheduler.yaml",
		Semantics: &GeneratedHostPathSemantics{
			Tool:                 "kubeadm",
			Hook:                 "bootstrap",
			APIVersion:           "v1",
			Kind:                 "Pod",
			Namespace:            "kube-system",
			Name:                 "kube-scheduler",
			ContainerName:        "kube-scheduler",
			ExpectedImage:        fmt.Sprintf("%s/kube-scheduler:%s", repository, version),
			ExpectedCommand:      "kube-scheduler",
			ExpectedArgs:         schedulerArgs,
			ExpectedVolumeMounts: expectedMounts(nil, schedulerArgs, config.Scheduler.ExtraVolumes),
		},
	})

	return expectations, true
}

func cleanExpectedArgs(raw interface{}) map[string]string {
	if raw == nil {
		return nil
	}
	clean := make(map[string]string)
	switch typed := raw.(type) {
	case map[string]interface{}:
		for key, value := range typed {
			cleanKey := strings.TrimSpace(key)
			if cleanKey == "" {
				continue
			}
			clean[cleanKey] = strings.TrimSpace(fmt.Sprint(value))
		}
	case []interface{}:
		for _, item := range typed {
			arg, ok := item.(map[string]interface{})
			if !ok {
				continue
			}
			name, _ := arg["name"].(string)
			cleanKey := strings.TrimSpace(name)
			if cleanKey == "" {
				continue
			}
			clean[cleanKey] = strings.TrimSpace(fmt.Sprint(arg["value"]))
		}
	default:
		return nil
	}
	if len(clean) == 0 {
		return nil
	}
	return clean
}

func extraVolumeMounts(raw interface{}) []string {
	items, ok := raw.([]interface{})
	if !ok {
		return nil
	}
	mounts := make([]string, 0, len(items))
	for _, item := range items {
		volume, ok := item.(map[string]interface{})
		if !ok {
			continue
		}
		mountPath, _ := volume["mountPath"].(string)
		clean := strings.TrimSpace(mountPath)
		if clean == "" {
			continue
		}
		mounts = append(mounts, clean)
	}
	if len(mounts) == 0 {
		return nil
	}
	return mounts
}

func expectedMounts(defaults []string, expectedArgs map[string]string, rawExtraVolumes interface{}) []string {
	mounts := make(map[string]struct{}, len(defaults))
	for _, mountPath := range defaults {
		clean := strings.TrimSpace(mountPath)
		if clean == "" {
			continue
		}
		mounts[clean] = struct{}{}
	}
	for _, value := range expectedArgs {
		if !strings.HasPrefix(value, "/") {
			continue
		}
		mountPath := value
		base := path.Base(value)
		if strings.Contains(base, ".") {
			mountPath = path.Dir(value)
		}
		mounts[mountPath] = struct{}{}
	}
	for _, mountPath := range extraVolumeMounts(rawExtraVolumes) {
		mounts[mountPath] = struct{}{}
	}
	if len(mounts) == 0 {
		return nil
	}
	expected := make([]string, 0, len(mounts))
	for mountPath := range mounts {
		expected = append(expected, mountPath)
	}
	sort.Strings(expected)
	return expected
}

func trackedHostPathType(mode fs.FileMode) (HostPathType, bool) {
	switch {
	case mode&os.ModeSymlink != 0:
		return HostPathSymlink, true
	case mode.IsRegular():
		return HostPathRegularFile, true
	default:
		return "", false
	}
}

func trimStepPrefix(path, prefix string) (string, bool) {
	cleanPrefix := strings.TrimSuffix(filepath.ToSlash(prefix), "/")
	cleanPath := filepath.ToSlash(path)
	if cleanPath == cleanPrefix {
		return "", true
	}
	needle := cleanPrefix + "/"
	if !strings.HasPrefix(cleanPath, needle) {
		return "", false
	}
	return cleanPath[len(needle):], true
}

func localInputOwners(component RenderedComponent) map[string]string {
	if len(component.InputBindings) == 0 || len(component.Inputs) == 0 {
		return nil
	}
	owners := make(map[string]string, len(component.InputBindings))
	for _, input := range component.Inputs {
		if _, ok := component.InputBindings[input.Name]; !ok {
			continue
		}
		cleanPath, err := cleanRelative(input.Path)
		if err != nil {
			continue
		}
		owners[cleanPath] = input.Name
	}
	if len(owners) == 0 {
		return nil
	}
	return owners
}

func discoverTrackedObjects(resolvedPath, bundlePath, component string, source InventorySource, ownership InventoryOwnership) ([]TrackedK8sObject, error) {
	info, err := os.Stat(resolvedPath)
	if err != nil {
		return nil, err
	}

	objects := make([]TrackedK8sObject, 0)
	if info.IsDir() {
		if err := filepath.WalkDir(resolvedPath, func(path string, entry os.DirEntry, walkErr error) error {
			if walkErr != nil {
				return walkErr
			}
			if entry.IsDir() {
				return nil
			}
			rel, err := filepath.Rel(resolvedPath, path)
			if err != nil {
				return err
			}
			fileObjects, err := parseTrackedObjectsFile(path, filepath.ToSlash(filepath.Join(bundlePath, rel)), component, source, ownership)
			if err != nil {
				return err
			}
			objects = append(objects, fileObjects...)
			return nil
		}); err != nil {
			return nil, err
		}
		return objects, nil
	}

	return parseTrackedObjectsFile(resolvedPath, bundlePath, component, source, ownership)
}

func resolveInventoryBundlePath(bundleRoot, bundleRel string) (string, error) {
	if strings.TrimSpace(bundleRel) == "" {
		return "", fmt.Errorf("bundle path cannot be empty")
	}
	if filepath.IsAbs(bundleRel) {
		return "", fmt.Errorf("bundle path %q must be relative", bundleRel)
	}

	resolved := filepath.Join(bundleRoot, filepath.FromSlash(bundleRel))
	relative, err := filepath.Rel(bundleRoot, resolved)
	if err != nil {
		return "", err
	}
	if relative == ".." || strings.HasPrefix(relative, ".."+string(os.PathSeparator)) {
		return "", fmt.Errorf("bundle path %q escapes bundle root", bundleRel)
	}
	return resolved, nil
}

func cloneStringMap(in map[string]string) map[string]string {
	if len(in) == 0 {
		return nil
	}
	out := make(map[string]string, len(in))
	for key, value := range in {
		out[key] = value
	}
	return out
}

func parseTrackedObjectsFile(resolvedPath, bundlePath, component string, source InventorySource, ownership InventoryOwnership) ([]TrackedK8sObject, error) {
	data, err := os.ReadFile(resolvedPath)
	if err != nil {
		return nil, fmt.Errorf("read Kubernetes object manifest %q: %w", resolvedPath, err)
	}

	decoder := utilyaml.NewYAMLOrJSONDecoder(bytes.NewReader(data), 4096)
	objects := make([]TrackedK8sObject, 0)
	for {
		var raw runtime.RawExtension
		if err := decoder.Decode(&raw); err != nil {
			if err == io.EOF {
				break
			}
			return nil, fmt.Errorf("decode Kubernetes object manifest %q: %w", resolvedPath, err)
		}
		if len(bytes.TrimSpace(raw.Raw)) == 0 {
			continue
		}

		var meta struct {
			APIVersion string `json:"apiVersion" yaml:"apiVersion"`
			Kind       string `json:"kind" yaml:"kind"`
			Metadata   struct {
				Name      string `json:"name" yaml:"name"`
				Namespace string `json:"namespace,omitempty" yaml:"namespace,omitempty"`
			} `json:"metadata" yaml:"metadata"`
		}
		if err := utilyaml.Unmarshal(raw.Raw, &meta); err != nil {
			return nil, fmt.Errorf("unmarshal Kubernetes object metadata from %q: %w", resolvedPath, err)
		}
		if meta.Kind == "" || meta.Metadata.Name == "" {
			continue
		}

		objects = append(objects, TrackedK8sObject{
			APIVersion: meta.APIVersion,
			Kind:       meta.Kind,
			Namespace:  meta.Metadata.Namespace,
			Name:       meta.Metadata.Name,
			Path:       bundlePath,
			Component:  component,
			Source:     source,
			Ownership:  ownership,
		})
	}
	return objects, nil
}
