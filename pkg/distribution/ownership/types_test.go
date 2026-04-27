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
