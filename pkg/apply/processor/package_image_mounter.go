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

package processor

import (
	"fmt"

	"github.com/labring/sealos/pkg/buildah"
	"github.com/labring/sealos/pkg/distribution/packageformat"
	"github.com/labring/sealos/pkg/utils/rand"
)

type packageImageMounter struct {
	builder buildah.Interface
}

func NewPackageImageMounter(builder buildah.Interface) packageformat.ImageMounter {
	return &packageImageMounter{
		builder: builder,
	}
}

func (m *packageImageMounter) Mount(image string) (packageformat.MountedImage, error) {
	if m.builder == nil {
		return packageformat.MountedImage{}, fmt.Errorf("buildah interface cannot be nil")
	}
	if image == "" {
		return packageformat.MountedImage{}, fmt.Errorf("image cannot be empty")
	}

	if err := m.builder.Pull([]string{image}, buildah.WithPullPolicyOption(buildah.PullIfMissing.String())); err != nil {
		return packageformat.MountedImage{}, err
	}

	containerName := "pkgfmt-" + rand.Generator(8)
	info, err := m.builder.Create(containerName, image)
	if err != nil {
		return packageformat.MountedImage{}, err
	}

	name := info.Container
	if name == "" {
		name = containerName
	}
	if info.MountPoint == "" {
		_ = m.builder.Delete(name)
		return packageformat.MountedImage{}, fmt.Errorf("image %q mounted with empty mount point", image)
	}

	return packageformat.MountedImage{
		Name:       name,
		MountPoint: info.MountPoint,
	}, nil
}

func (m *packageImageMounter) Unmount(name string) error {
	if m.builder == nil {
		return fmt.Errorf("buildah interface cannot be nil")
	}
	return m.builder.Delete(name)
}
