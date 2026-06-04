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
	"os"

	"sigs.k8s.io/yaml"

	fileutil "github.com/labring/sealos/pkg/utils/file"
)

func LoadFile(path string) (*BOM, error) {
	if !fileutil.IsFile(path) {
		return nil, fmt.Errorf("bom file %q not found", path)
	}

	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read bom %q: %w", path, err)
	}
	return LoadBytes(data, path)
}

func LoadBytes(data []byte, subject string) (*BOM, error) {
	var doc BOM
	if err := yaml.Unmarshal(data, &doc); err != nil {
		return nil, fmt.Errorf("unmarshal bom %q: %w", subject, err)
	}
	if err := doc.Validate(); err != nil {
		return nil, fmt.Errorf("validate bom %q: %w", subject, err)
	}
	return &doc, nil
}
