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

package ownership

import "fmt"

type Plane string

const (
	PlaneInfra Plane = "infra"
	PlaneUser  Plane = "user"
)

type Scope string

const (
	ScopeGlobal Scope = "global"
	ScopeLocal  Scope = "local"
)

type Rule struct {
	Group      string `json:"group,omitempty" yaml:"group,omitempty"`
	Kind       string `json:"kind" yaml:"kind"`
	Name       string `json:"name,omitempty" yaml:"name,omitempty"`
	Plane      Plane  `json:"plane" yaml:"plane"`
	Scope      Scope  `json:"scope" yaml:"scope"`
	Promotable bool   `json:"promotable,omitempty" yaml:"promotable,omitempty"`
}

func (p Plane) Validate() error {
	switch p {
	case PlaneInfra, PlaneUser:
		return nil
	default:
		return fmt.Errorf("invalid plane %q", p)
	}
}

func (s Scope) Validate() error {
	switch s {
	case ScopeGlobal, ScopeLocal:
		return nil
	default:
		return fmt.Errorf("invalid scope %q", s)
	}
}

func (r Rule) Validate() error {
	if r.Kind == "" {
		return fmt.Errorf("kind cannot be empty")
	}
	if err := r.Plane.Validate(); err != nil {
		return fmt.Errorf("plane: %w", err)
	}
	if err := r.Scope.Validate(); err != nil {
		return fmt.Errorf("scope: %w", err)
	}
	return nil
}
