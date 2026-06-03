// Copyright © 2026 sealos.
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

import "testing"

func TestRuleValidate(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		rule    Rule
		wantErr bool
	}{
		{
			name: "valid",
			rule: Rule{
				Group:      "apps",
				Kind:       "Deployment",
				Plane:      PlaneInfra,
				Scope:      ScopeGlobal,
				Promotable: true,
			},
		},
		{
			name: "missing kind",
			rule: Rule{
				Plane: PlaneInfra,
				Scope: ScopeGlobal,
			},
			wantErr: true,
		},
		{
			name: "invalid plane",
			rule: Rule{
				Kind:  "Deployment",
				Plane: Plane("tenant"),
				Scope: ScopeGlobal,
			},
			wantErr: true,
		},
		{
			name: "invalid scope",
			rule: Rule{
				Kind:  "Deployment",
				Plane: PlaneInfra,
				Scope: Scope("shared"),
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			err := tt.rule.Validate()
			if tt.wantErr && err == nil {
				t.Fatal("expected validation error, got nil")
			}
			if !tt.wantErr && err != nil {
				t.Fatalf("expected no validation error, got %v", err)
			}
		})
	}
}
