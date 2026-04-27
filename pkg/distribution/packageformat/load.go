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

package packageformat

import (
	"fmt"
	"os"
	"path/filepath"

	fileutil "github.com/labring/sealos/pkg/utils/file"
	yamlutil "github.com/labring/sealos/pkg/utils/yaml"
)

const ManifestFileName = "package.yaml"

type MountedImage struct {
	Name       string
	MountPoint string
}

type ImageMounter interface {
	Mount(image string) (MountedImage, error)
	Unmount(name string) error
}

type Loader interface {
	Load(image string) (*ComponentPackage, error)
}

type LoaderFunc func(image string) (*ComponentPackage, error)

func (f LoaderFunc) Load(image string) (*ComponentPackage, error) {
	return f(image)
}

type MountedImageLoader struct {
	Mounter ImageMounter
}

func ManifestPath(root string) string {
	return filepath.Join(root, ManifestFileName)
}

func (l MountedImageLoader) Load(image string) (*ComponentPackage, error) {
	return LoadFromImage(l.Mounter, image)
}

func LoadFile(manifestPath string) (*ComponentPackage, error) {
	if !fileutil.IsFile(manifestPath) {
		return nil, fmt.Errorf("component package manifest %q not found", manifestPath)
	}

	var pkg ComponentPackage
	if err := yamlutil.UnmarshalFile(manifestPath, &pkg); err != nil {
		return nil, fmt.Errorf("unmarshal component package %q: %w", manifestPath, err)
	}
	if err := pkg.Validate(); err != nil {
		return nil, fmt.Errorf("validate component package %q: %w", manifestPath, err)
	}
	return &pkg, nil
}

func LoadDir(root string) (*ComponentPackage, error) {
	if root == "" {
		return nil, fmt.Errorf("component package root cannot be empty")
	}
	pkg, err := LoadFile(ManifestPath(root))
	if err != nil {
		return nil, err
	}
	if err := validatePackageContents(root, pkg); err != nil {
		return nil, err
	}
	return pkg, nil
}

func LoadFromImage(mounter ImageMounter, image string) (*ComponentPackage, error) {
	if mounter == nil {
		return nil, fmt.Errorf("image mounter cannot be nil")
	}
	if image == "" {
		return nil, fmt.Errorf("image cannot be empty")
	}

	info, err := mounter.Mount(image)
	if err != nil {
		return nil, fmt.Errorf("mount component package image %q: %w", image, err)
	}

	cleanupName := info.Name
	if cleanupName == "" {
		cleanupName = image
	}

	pkg, loadErr := LoadDir(info.MountPoint)
	deleteErr := mounter.Unmount(cleanupName)

	if loadErr != nil {
		if deleteErr != nil {
			return nil, fmt.Errorf("%v (cleanup failed: %v)", loadErr, deleteErr)
		}
		return nil, loadErr
	}
	if deleteErr != nil {
		return nil, fmt.Errorf("cleanup component package mount %q: %w", cleanupName, deleteErr)
	}
	return pkg, nil
}

func validatePackageContents(root string, pkg *ComponentPackage) error {
	for _, content := range pkg.Spec.Contents {
		contentPath := filepath.Join(root, filepath.FromSlash(content.Path))
		if _, err := os.Stat(contentPath); err != nil {
			if os.IsNotExist(err) {
				return fmt.Errorf("component package content %q not found at %q", content.Name, content.Path)
			}
			return fmt.Errorf("stat component package content %q: %w", content.Name, err)
		}
	}
	for _, hook := range pkg.Spec.Hooks {
		hookPath := filepath.Join(root, filepath.FromSlash(hook.Path))
		info, err := os.Stat(hookPath)
		if err != nil {
			if os.IsNotExist(err) {
				return fmt.Errorf("component package hook %q not found at %q", hook.Name, hook.Path)
			}
			return fmt.Errorf("stat component package hook %q: %w", hook.Name, err)
		}
		if info.IsDir() {
			return fmt.Errorf("component package hook %q must reference a file, got directory %q", hook.Name, hook.Path)
		}
	}
	return nil
}
