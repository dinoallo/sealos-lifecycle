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

package promotion

import (
	"fmt"
	"sort"
	"strings"
)

type Channel string

const (
	ChannelAlpha  Channel = "alpha"
	ChannelBeta   Channel = "beta"
	ChannelStable Channel = "stable"
)

type CandidateRevision struct {
	Line            string  `json:"line" yaml:"line"`
	Revision        string  `json:"revision" yaml:"revision"`
	SourceChannel   Channel `json:"sourceChannel" yaml:"sourceChannel"`
	Replacing       string  `json:"replacing,omitempty" yaml:"replacing,omitempty"`
	ComponentDigest string  `json:"componentDigest,omitempty" yaml:"componentDigest,omitempty"`
}

type HealthProofSummary struct {
	Provided      bool     `json:"provided" yaml:"provided"`
	Passed        bool     `json:"passed" yaml:"passed"`
	FailedSignals []string `json:"failedSignals,omitempty" yaml:"failedSignals,omitempty"`
}

type ChannelRule struct {
	Channel               Channel   `json:"channel" yaml:"channel"`
	Intent                string    `json:"intent" yaml:"intent"`
	Rank                  int       `json:"rank" yaml:"rank"`
	AllowedSourceChannels []Channel `json:"allowedSourceChannels" yaml:"allowedSourceChannels"`
	RequiresHealthProof   bool      `json:"requiresHealthProof" yaml:"requiresHealthProof"`
}

type Policy struct {
	ChannelRules []ChannelRule `json:"channelRules" yaml:"channelRules"`
}

type Request struct {
	TargetChannel Channel            `json:"targetChannel" yaml:"targetChannel"`
	Candidate     CandidateRevision  `json:"candidate" yaml:"candidate"`
	HealthProof   HealthProofSummary `json:"healthProof" yaml:"healthProof"`
}

type Decision struct {
	Allowed     bool               `json:"allowed" yaml:"allowed"`
	Transition  ChannelTransition  `json:"transition" yaml:"transition"`
	Violations  []Violation        `json:"violations,omitempty" yaml:"violations,omitempty"`
	Warnings    []string           `json:"warnings,omitempty" yaml:"warnings,omitempty"`
	ChannelRule ChannelRule        `json:"channelRule" yaml:"channelRule"`
	HealthProof HealthProofSummary `json:"healthProof" yaml:"healthProof"`
}

type ChannelTransition struct {
	Line                string  `json:"line" yaml:"line"`
	FromRevision        string  `json:"fromRevision,omitempty" yaml:"fromRevision,omitempty"`
	ToRevision          string  `json:"toRevision" yaml:"toRevision"`
	SourceChannel       Channel `json:"sourceChannel" yaml:"sourceChannel"`
	TargetChannel       Channel `json:"targetChannel" yaml:"targetChannel"`
	HealthProofRequired bool    `json:"healthProofRequired" yaml:"healthProofRequired"`
}

type ViolationCode string

const (
	ViolationSourceChannelBlocked ViolationCode = "sourceChannelBlocked"
	ViolationHealthProofRequired  ViolationCode = "healthProofRequired"
	ViolationHealthProofFailed    ViolationCode = "healthProofFailed"
)

type Violation struct {
	Code    ViolationCode `json:"code" yaml:"code"`
	Message string        `json:"message" yaml:"message"`
}

func DefaultPolicy() Policy {
	return Policy{
		ChannelRules: []ChannelRule{
			{
				Channel:               ChannelAlpha,
				Intent:                "early validation with limited blast radius",
				Rank:                  10,
				AllowedSourceChannels: []Channel{ChannelAlpha},
				RequiresHealthProof:   false,
			},
			{
				Channel:               ChannelBeta,
				Intent:                "controlled canary validation before production rollout",
				Rank:                  20,
				AllowedSourceChannels: []Channel{ChannelAlpha, ChannelBeta},
				RequiresHealthProof:   true,
			},
			{
				Channel:               ChannelStable,
				Intent:                "general-use production baseline",
				Rank:                  30,
				AllowedSourceChannels: []Channel{ChannelBeta, ChannelStable},
				RequiresHealthProof:   true,
			},
		},
	}
}

func EvaluateDefault(req Request) (*Decision, error) {
	return DefaultPolicy().Evaluate(req)
}

func (p Policy) Evaluate(req Request) (*Decision, error) {
	if err := p.Validate(); err != nil {
		return nil, err
	}
	if err := req.Validate(); err != nil {
		return nil, err
	}

	rule, ok := p.RuleFor(req.TargetChannel)
	if !ok {
		return nil, fmt.Errorf("no promotion rule for target channel %q", req.TargetChannel)
	}

	decision := &Decision{
		Allowed: true,
		Transition: ChannelTransition{
			Line:                req.Candidate.Line,
			FromRevision:        strings.TrimSpace(req.Candidate.Replacing),
			ToRevision:          req.Candidate.Revision,
			SourceChannel:       req.Candidate.SourceChannel,
			TargetChannel:       req.TargetChannel,
			HealthProofRequired: rule.RequiresHealthProof,
		},
		ChannelRule: rule,
		HealthProof: req.HealthProof,
	}

	if !channelIn(req.Candidate.SourceChannel, rule.AllowedSourceChannels) {
		decision.addViolation(ViolationSourceChannelBlocked,
			fmt.Sprintf("candidate source channel %q cannot promote to target channel %q", req.Candidate.SourceChannel, req.TargetChannel))
	}
	if rule.RequiresHealthProof {
		switch {
		case !req.HealthProof.Provided:
			decision.addViolation(ViolationHealthProofRequired,
				fmt.Sprintf("target channel %q requires passed health proof", req.TargetChannel))
		case !req.HealthProof.Passed:
			decision.addViolation(ViolationHealthProofFailed,
				"health proof did not pass")
		}
	}
	if len(req.HealthProof.FailedSignals) > 0 {
		decision.addViolation(ViolationHealthProofFailed,
			fmt.Sprintf("health proof has failed signal(s): %s", strings.Join(req.HealthProof.FailedSignals, ", ")))
	}

	sort.Slice(decision.Violations, func(i, j int) bool {
		if decision.Violations[i].Code != decision.Violations[j].Code {
			return decision.Violations[i].Code < decision.Violations[j].Code
		}
		return decision.Violations[i].Message < decision.Violations[j].Message
	})
	return decision, nil
}

func (p Policy) Validate() error {
	if len(p.ChannelRules) == 0 {
		return fmt.Errorf("channelRules cannot be empty")
	}
	seen := make(map[Channel]struct{}, len(p.ChannelRules))
	for i, rule := range p.ChannelRules {
		if err := rule.Validate(); err != nil {
			return fmt.Errorf("channelRules[%d]: %w", i, err)
		}
		if _, ok := seen[rule.Channel]; ok {
			return fmt.Errorf("channelRules[%d]: duplicate channel %q", i, rule.Channel)
		}
		seen[rule.Channel] = struct{}{}
	}
	return nil
}

func (p Policy) RuleFor(channel Channel) (ChannelRule, bool) {
	for _, rule := range p.ChannelRules {
		if rule.Channel == channel {
			return rule, true
		}
	}
	return ChannelRule{}, false
}

func (r Request) Validate() error {
	if err := r.TargetChannel.Validate(); err != nil {
		return fmt.Errorf("targetChannel: %w", err)
	}
	if err := r.Candidate.Validate(); err != nil {
		return fmt.Errorf("candidate: %w", err)
	}
	return nil
}

func (r ChannelRule) Validate() error {
	if err := r.Channel.Validate(); err != nil {
		return fmt.Errorf("channel: %w", err)
	}
	if strings.TrimSpace(r.Intent) == "" {
		return fmt.Errorf("intent cannot be empty")
	}
	if r.Rank <= 0 {
		return fmt.Errorf("rank must be positive")
	}
	if len(r.AllowedSourceChannels) == 0 {
		return fmt.Errorf("allowedSourceChannels cannot be empty")
	}
	seen := make(map[Channel]struct{}, len(r.AllowedSourceChannels))
	for i, channel := range r.AllowedSourceChannels {
		if err := channel.Validate(); err != nil {
			return fmt.Errorf("allowedSourceChannels[%d]: %w", i, err)
		}
		if _, ok := seen[channel]; ok {
			return fmt.Errorf("allowedSourceChannels[%d]: duplicate channel %q", i, channel)
		}
		seen[channel] = struct{}{}
	}
	return nil
}

func (c CandidateRevision) Validate() error {
	if strings.TrimSpace(c.Line) == "" {
		return fmt.Errorf("line cannot be empty")
	}
	if strings.TrimSpace(c.Revision) == "" {
		return fmt.Errorf("revision cannot be empty")
	}
	if err := c.SourceChannel.Validate(); err != nil {
		return fmt.Errorf("sourceChannel: %w", err)
	}
	return nil
}

func (c Channel) Validate() error {
	switch c {
	case ChannelAlpha, ChannelBeta, ChannelStable:
		return nil
	default:
		return fmt.Errorf("invalid channel %q", c)
	}
}

func (d *Decision) addViolation(code ViolationCode, message string) {
	d.Allowed = false
	d.Violations = append(d.Violations, Violation{
		Code:    code,
		Message: message,
	})
}

func channelIn(channel Channel, candidates []Channel) bool {
	for _, candidate := range candidates {
		if candidate == channel {
			return true
		}
	}
	return false
}
