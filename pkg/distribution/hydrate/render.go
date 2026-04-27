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
	"errors"
	"fmt"
	"os"
	"path"
	"path/filepath"
	"slices"
	"strings"

	"github.com/labring/sealos/pkg/distribution/packageformat"
	fileutil "github.com/labring/sealos/pkg/utils/file"
	yamlutil "github.com/labring/sealos/pkg/utils/yaml"
)

type Source struct {
	Root string
}

type SourceProvider interface {
	Source(component ComponentPlan) (Source, error)
}

type SourceProviderFunc func(component ComponentPlan) (Source, error)

func (f SourceProviderFunc) Source(component ComponentPlan) (Source, error) {
	return f(component)
}

type SourceMap map[string]string

func (m SourceMap) Source(component ComponentPlan) (Source, error) {
	root, ok := m[component.Name]
	if !ok {
		return Source{}, fmt.Errorf("source for component %q not found", component.Name)
	}
	if root == "" {
		return Source{}, fmt.Errorf("source root for component %q cannot be empty", component.Name)
	}
	return Source{Root: root}, nil
}

type MountedArtifactSourceProvider struct {
	Mounter packageformat.ImageMounter
	mounts  map[string]packageformat.MountedImage
}

func NewMountedArtifactSourceProvider(mounter packageformat.ImageMounter) *MountedArtifactSourceProvider {
	return &MountedArtifactSourceProvider{
		Mounter: mounter,
		mounts:  make(map[string]packageformat.MountedImage),
	}
}

func (p *MountedArtifactSourceProvider) Source(component ComponentPlan) (Source, error) {
	if p == nil || p.Mounter == nil {
		return Source{}, fmt.Errorf("image mounter cannot be nil")
	}
	if component.Artifact == "" {
		return Source{}, fmt.Errorf("artifact reference for component %q cannot be empty", component.Name)
	}

	if info, ok := p.mounts[component.Artifact]; ok {
		return Source{Root: info.MountPoint}, nil
	}

	info, err := p.Mounter.Mount(component.Artifact)
	if err != nil {
		return Source{}, fmt.Errorf("mount component %q artifact %q: %w", component.Name, component.Artifact, err)
	}
	if info.MountPoint == "" {
		cleanupName := info.Name
		if cleanupName == "" {
			cleanupName = component.Artifact
		}
		_ = p.Mounter.Unmount(cleanupName)
		return Source{}, fmt.Errorf("component %q artifact %q mounted with empty root", component.Name, component.Artifact)
	}
	p.mounts[component.Artifact] = info
	return Source{Root: info.MountPoint}, nil
}

func (p *MountedArtifactSourceProvider) Close() error {
	if p == nil || p.Mounter == nil {
		return nil
	}

	keys := make([]string, 0, len(p.mounts))
	for artifact := range p.mounts {
		keys = append(keys, artifact)
	}
	slices.Sort(keys)

	var errs []error
	for _, artifact := range keys {
		info := p.mounts[artifact]
		name := info.Name
		if name == "" {
			name = artifact
		}
		if err := p.Mounter.Unmount(name); err != nil {
			errs = append(errs, fmt.Errorf("unmount component artifact %q: %w", artifact, err))
			continue
		}
		delete(p.mounts, artifact)
	}
	return errors.Join(errs...)
}

func RenderPlan(plan *Plan, sources SourceProvider, outputDir string) (*Bundle, error) {
	if plan == nil {
		return nil, fmt.Errorf("plan cannot be nil")
	}
	if sources == nil {
		return nil, fmt.Errorf("source provider cannot be nil")
	}
	if outputDir == "" {
		return nil, fmt.Errorf("output directory cannot be empty")
	}

	if err := os.MkdirAll(outputDir, 0o755); err != nil {
		return nil, fmt.Errorf("create output directory %q: %w", outputDir, err)
	}

	bundle := NewBundle(plan)
	for _, component := range plan.Components {
		rendered, err := renderComponent(component, sources, outputDir)
		if err != nil {
			return nil, err
		}
		bundle.Spec.Components = append(bundle.Spec.Components, rendered)
	}

	bundlePath := filepath.Join(outputDir, BundleFileName)
	if err := yamlutil.MarshalFile(bundlePath, bundle); err != nil {
		return nil, fmt.Errorf("write bundle manifest %q: %w", bundlePath, err)
	}
	return bundle, nil
}

func renderComponent(component ComponentPlan, sources SourceProvider, outputDir string) (RenderedComponent, error) {
	source, err := sources.Source(component)
	if err != nil {
		return RenderedComponent{}, err
	}
	if source.Root == "" {
		return RenderedComponent{}, fmt.Errorf("source root for component %q cannot be empty", component.Name)
	}

	componentDir := filepath.Join(outputDir, "components", component.Name)
	componentFilesDir := filepath.Join(componentDir, "files")
	if err := os.MkdirAll(componentFilesDir, 0o755); err != nil {
		return RenderedComponent{}, fmt.Errorf("create component bundle directory %q: %w", componentDir, err)
	}

	manifestDst := filepath.Join(componentDir, packageformat.ManifestFileName)
	if err := copyEntry(packageformat.ManifestPath(source.Root), manifestDst); err != nil {
		return RenderedComponent{}, fmt.Errorf("copy component package manifest for %q: %w", component.Name, err)
	}

	rendered := RenderedComponent{
		Name:         component.Name,
		PackageName:  component.PackageName,
		Version:      component.Version,
		Class:        component.Class,
		Artifact:     component.Artifact,
		Dependencies: append([]string(nil), component.Dependencies...),
		Inputs:       append([]packageformat.Input(nil), component.Inputs...),
		ManifestPath: mustBundlePath(outputDir, manifestDst),
		RootPath:     mustBundlePath(outputDir, componentFilesDir),
		Steps:        make([]RenderedStep, 0, len(component.Steps)),
	}

	copied := make(map[string]string, len(component.Steps))
	for _, step := range component.Steps {
		sourcePath, err := cleanRelative(step.Path)
		if err != nil {
			return RenderedComponent{}, fmt.Errorf("component %q step %q: %w", component.Name, step.Name, err)
		}

		bundlePath, ok := copied[sourcePath]
		if !ok {
			src := filepath.Join(source.Root, filepath.FromSlash(sourcePath))
			dst := filepath.Join(componentFilesDir, filepath.FromSlash(sourcePath))
			if err := copyEntry(src, dst); err != nil {
				return RenderedComponent{}, fmt.Errorf("copy component %q payload %q: %w", component.Name, sourcePath, err)
			}
			bundlePath = mustBundlePath(outputDir, dst)
			copied[sourcePath] = bundlePath
		}

		rendered.Steps = append(rendered.Steps, RenderedStep{
			Name:           step.Name,
			Kind:           step.Kind,
			BundlePath:     bundlePath,
			SourcePath:     sourcePath,
			ContentType:    step.ContentType,
			HookPhase:      step.HookPhase,
			Target:         step.Target,
			MediaType:      step.MediaType,
			Required:       step.Required,
			Args:           append([]string(nil), step.Args...),
			TimeoutSeconds: step.TimeoutSeconds,
		})
	}

	return rendered, nil
}

func copyEntry(src, dst string) error {
	info, err := os.Stat(src)
	if err != nil {
		return err
	}

	if info.IsDir() {
		return fileutil.CopyDirV3(src, dst)
	}

	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return err
	}
	return fileutil.Copy(src, dst)
}

func cleanRelative(value string) (string, error) {
	if strings.TrimSpace(value) == "" {
		return "", fmt.Errorf("path cannot be empty")
	}
	if path.IsAbs(value) {
		return "", fmt.Errorf("path %q must be relative", value)
	}

	cleaned := path.Clean(value)
	switch {
	case cleaned == ".":
		return "", fmt.Errorf("path %q must reference a file or directory", value)
	case cleaned == "..":
		return "", fmt.Errorf("path %q escapes the package root", value)
	case strings.HasPrefix(cleaned, "../"):
		return "", fmt.Errorf("path %q escapes the package root", value)
	default:
		return cleaned, nil
	}
}

func mustBundlePath(root, target string) string {
	relative, err := filepath.Rel(root, target)
	if err != nil {
		panic(err)
	}
	return filepath.ToSlash(relative)
}
