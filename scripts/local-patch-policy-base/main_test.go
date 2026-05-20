package main

import "testing"

func TestResolveBaseSHA(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name      string
		eventName string
		prBaseSHA string
		beforeSHA string
		want      string
	}{
		{
			name:      "pull request uses PR base SHA",
			eventName: "pull_request",
			prBaseSHA: "abc123",
			beforeSHA: "def456",
			want:      "abc123",
		},
		{
			name:      "push uses before SHA",
			eventName: "push",
			beforeSHA: "def456",
			want:      "def456",
		},
		{
			name:      "zero before SHA is ignored",
			eventName: "push",
			beforeSHA: "0000000000000000000000000000000000000000",
			want:      "",
		},
		{
			name:      "empty PR base SHA returns empty",
			eventName: "pull_request",
			prBaseSHA: "",
			want:      "",
		},
	}

	for _, tc := range testCases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			if got := resolveBaseSHA(tc.eventName, tc.prBaseSHA, tc.beforeSHA); got != tc.want {
				t.Fatalf("resolveBaseSHA(%q, %q, %q) = %q, want %q", tc.eventName, tc.prBaseSHA, tc.beforeSHA, got, tc.want)
			}
		})
	}
}

func TestFilepathToSlash(t *testing.T) {
	t.Parallel()

	if got, want := filepathToSlash("docs/examples/policy.yaml"), "docs/examples/policy.yaml"; got != want {
		t.Fatalf("filepathToSlash() = %q, want %q", got, want)
	}
}
