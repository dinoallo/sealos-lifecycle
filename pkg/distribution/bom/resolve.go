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

package bom

import (
	"fmt"
	"strings"

	"github.com/labring/sealos/pkg/distribution/packageformat"
)

func (a ArtifactReference) Reference() string {
	image := a.Image
	if idx := strings.Index(image, "@"); idx >= 0 {
		image = image[:idx]
	}
	return image + "@" + a.Digest
}

func (b BOM) ResolveComponentPackages(loader packageformat.Loader) (map[string]*packageformat.ComponentPackage, error) {
	if loader == nil {
		return nil, fmt.Errorf("package loader cannot be nil")
	}
	if err := b.Validate(); err != nil {
		return nil, err
	}

	resolved := make(map[string]*packageformat.ComponentPackage, len(b.Spec.Components))
	for i, component := range b.Spec.Components {
		pkg, err := loader.Load(component.Artifact.Reference())
		if err != nil {
			return nil, fmt.Errorf("load component %q: %w", component.Name, err)
		}
		if pkg.Spec.Component != component.Name {
			return nil, fmt.Errorf(
				"spec.components[%d]: package component mismatch, got %q want %q",
				i, pkg.Spec.Component, component.Name,
			)
		}
		if pkg.Spec.Version != component.Version {
			return nil, fmt.Errorf(
				"spec.components[%d]: package version mismatch, got %q want %q",
				i, pkg.Spec.Version, component.Version,
			)
		}
		resolved[component.Name] = pkg
	}
	return resolved, nil
}
