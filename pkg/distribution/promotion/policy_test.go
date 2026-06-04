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

package promotion

import (
	"strings"
	"testing"
)

func TestEvaluateDefault(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		request    Request
		wantAllow  bool
		wantCodes  []ViolationCode
		wantHealth bool
	}{
		{
			name: "alpha accepts alpha candidate without health proof",
			request: Request{
				TargetChannel: ChannelAlpha,
				Candidate: CandidateRevision{
					Line:          "default-platform",
					Revision:      "rev-20240424",
					SourceChannel: ChannelAlpha,
				},
			},
			wantAllow: true,
		},
		{
			name: "beta accepts alpha candidate with passed health proof",
			request: Request{
				TargetChannel: ChannelBeta,
				Candidate: CandidateRevision{
					Line:          "default-platform",
					Revision:      "rev-20240424",
					SourceChannel: ChannelAlpha,
					Replacing:     "rev-20240423",
				},
				HealthProof: HealthProofSummary{
					Provided: true,
					Passed:   true,
				},
			},
			wantAllow:  true,
			wantHealth: true,
		},
		{
			name: "stable accepts beta candidate with passed health proof",
			request: Request{
				TargetChannel: ChannelStable,
				Candidate: CandidateRevision{
					Line:          "default-platform",
					Revision:      "rev-20240424",
					SourceChannel: ChannelBeta,
					Replacing:     "rev-20240423",
				},
				HealthProof: HealthProofSummary{
					Provided: true,
					Passed:   true,
				},
			},
			wantAllow:  true,
			wantHealth: true,
		},
		{
			name: "stable rejects alpha candidate",
			request: Request{
				TargetChannel: ChannelStable,
				Candidate: CandidateRevision{
					Line:          "default-platform",
					Revision:      "rev-20240424",
					SourceChannel: ChannelAlpha,
				},
				HealthProof: HealthProofSummary{
					Provided: true,
					Passed:   true,
				},
			},
			wantCodes:  []ViolationCode{ViolationSourceChannelBlocked},
			wantHealth: true,
		},
		{
			name: "beta rejects missing health proof",
			request: Request{
				TargetChannel: ChannelBeta,
				Candidate: CandidateRevision{
					Line:          "default-platform",
					Revision:      "rev-20240424",
					SourceChannel: ChannelAlpha,
				},
			},
			wantCodes:  []ViolationCode{ViolationHealthProofRequired},
			wantHealth: true,
		},
		{
			name: "stable rejects failed proof signal",
			request: Request{
				TargetChannel: ChannelStable,
				Candidate: CandidateRevision{
					Line:          "default-platform",
					Revision:      "rev-20240424",
					SourceChannel: ChannelBeta,
				},
				HealthProof: HealthProofSummary{
					Provided:      true,
					Passed:        true,
					FailedSignals: []string{"node-readiness"},
				},
			},
			wantCodes:  []ViolationCode{ViolationHealthProofFailed},
			wantHealth: true,
		},
		{
			name: "stable rejects missing required health signal",
			request: Request{
				TargetChannel: ChannelStable,
				Candidate: CandidateRevision{
					Line:          "default-platform",
					Revision:      "rev-20240424",
					SourceChannel: ChannelBeta,
				},
				HealthProof: HealthProofSummary{
					Provided:               true,
					Passed:                 true,
					RequiredSignals:        []string{"node-readiness", "runtime-preflight"},
					MissingRequiredSignals: []string{"runtime-preflight"},
					MinPassedSignals:       1,
					PassedSignals:          1,
				},
			},
			wantCodes:  []ViolationCode{ViolationHealthProofFailed},
			wantHealth: true,
		},
		{
			name: "beta accepts optional failed signal when thresholds pass",
			request: Request{
				TargetChannel: ChannelBeta,
				Candidate: CandidateRevision{
					Line:          "default-platform",
					Revision:      "rev-20240424",
					SourceChannel: ChannelAlpha,
				},
				HealthProof: HealthProofSummary{
					Provided:              true,
					Passed:                true,
					RequiredSignals:       []string{"node-readiness"},
					OptionalFailedSignals: []string{"observability-smoke"},
					MinPassedSignals:      1,
					PassedSignals:         1,
				},
			},
			wantAllow:  true,
			wantHealth: true,
		},
		{
			name: "alpha rejects provided failed health proof",
			request: Request{
				TargetChannel: ChannelAlpha,
				Candidate: CandidateRevision{
					Line:          "default-platform",
					Revision:      "rev-20240424",
					SourceChannel: ChannelAlpha,
				},
				HealthProof: HealthProofSummary{
					Provided: true,
					Passed:   false,
				},
			},
			wantCodes: []ViolationCode{ViolationHealthProofFailed},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			decision, err := EvaluateDefault(tt.request)
			if err != nil {
				t.Fatalf("EvaluateDefault() error = %v", err)
			}
			if got, want := decision.Allowed, tt.wantAllow; got != want {
				t.Fatalf("Allowed = %v, want %v; violations=%#v", got, want, decision.Violations)
			}
			if got, want := decision.Transition.HealthProofRequired, tt.wantHealth; got != want {
				t.Fatalf("HealthProofRequired = %v, want %v", got, want)
			}
			assertViolationCodes(t, decision.Violations, tt.wantCodes)
			if tt.name == "beta accepts optional failed signal when thresholds pass" && len(decision.Warnings) == 0 {
				t.Fatal("Warnings empty, want optional failed signal warning")
			}
		})
	}
}

func TestPolicyValidate(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		policy  Policy
		wantErr string
	}{
		{
			name:    "default",
			policy:  DefaultPolicy(),
			wantErr: "",
		},
		{
			name:    "empty",
			policy:  Policy{},
			wantErr: "channelRules",
		},
		{
			name: "duplicate channel",
			policy: Policy{
				ChannelRules: []ChannelRule{
					{
						Channel:               ChannelAlpha,
						Intent:                "one",
						Rank:                  10,
						AllowedSourceChannels: []Channel{ChannelAlpha},
					},
					{
						Channel:               ChannelAlpha,
						Intent:                "two",
						Rank:                  20,
						AllowedSourceChannels: []Channel{ChannelAlpha},
					},
				},
			},
			wantErr: "duplicate channel",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			err := tt.policy.Validate()
			if tt.wantErr == "" {
				if err != nil {
					t.Fatalf("Validate() error = %v", err)
				}
				return
			}
			if err == nil {
				t.Fatal("Validate() error = nil, want error")
			}
			if !strings.Contains(err.Error(), tt.wantErr) {
				t.Fatalf("Validate() error = %v, want substring %q", err, tt.wantErr)
			}
		})
	}
}

func TestEvaluateDefaultRejectsInvalidRequest(t *testing.T) {
	t.Parallel()

	_, err := EvaluateDefault(Request{
		TargetChannel: ChannelStable,
		Candidate: CandidateRevision{
			Line:          "default-platform",
			Revision:      "rev-20240424",
			SourceChannel: Channel("ga"),
		},
	})
	if err == nil {
		t.Fatal("EvaluateDefault() error = nil, want invalid request")
	}
	if !strings.Contains(err.Error(), "sourceChannel") {
		t.Fatalf("EvaluateDefault() error = %v, want sourceChannel", err)
	}
}

func assertViolationCodes(t *testing.T, violations []Violation, want []ViolationCode) {
	t.Helper()

	if len(violations) != len(want) {
		t.Fatalf("len(Violations) = %d, want %d; violations=%#v", len(violations), len(want), violations)
	}
	for i, violation := range violations {
		if violation.Code != want[i] {
			t.Fatalf("Violations[%d].Code = %q, want %q; violations=%#v", i, violation.Code, want[i], violations)
		}
	}
}
