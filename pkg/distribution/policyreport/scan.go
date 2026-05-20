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

package policyreport

import (
	"fmt"
	"io/fs"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

type ApprovalScanConfig struct {
	ApprovalExpiryWarningDays int  `json:"approvalExpiryWarningDays" yaml:"approvalExpiryWarningDays"`
	FailOnApprovalExpiresSoon bool `json:"failOnApprovalExpiresSoon" yaml:"failOnApprovalExpiresSoon"`
}

func DefaultApprovalScanConfig() ApprovalScanConfig {
	return ApprovalScanConfig{
		ApprovalExpiryWarningDays: DefaultGateConfig().ApprovalExpiryWarningDays,
		FailOnApprovalExpiresSoon: DefaultGateConfig().FailOnApprovalExpiresSoon,
	}
}

type ApprovalScanStatus string

const (
	ApprovalScanStatusValid       ApprovalScanStatus = "valid"
	ApprovalScanStatusExpiresSoon ApprovalScanStatus = "expiresSoon"
	ApprovalScanStatusExpired     ApprovalScanStatus = "expired"
	ApprovalScanStatusInvalid     ApprovalScanStatus = "invalid"
)

const (
	ApprovalScanFollowUpRepairInvalidDocument = "repairInvalidApprovalDocument"
	ApprovalScanFollowUpRemoveExpiredApproval = "removeOrReplaceExpiredApproval"
)

type ApprovalScanResult struct {
	Passed    bool                `json:"passed" yaml:"passed"`
	Config    ApprovalScanConfig  `json:"config" yaml:"config"`
	Summary   ApprovalScanSummary `json:"summary" yaml:"summary"`
	Approvals []ApprovalScanItem  `json:"approvals,omitempty" yaml:"approvals,omitempty"`
	Warnings  []string            `json:"warnings,omitempty" yaml:"warnings,omitempty"`
}

type ApprovalScanSummary struct {
	Scanned     int `json:"scanned" yaml:"scanned"`
	Valid       int `json:"valid" yaml:"valid"`
	ExpiresSoon int `json:"expiresSoon" yaml:"expiresSoon"`
	Expired     int `json:"expired" yaml:"expired"`
	Invalid     int `json:"invalid" yaml:"invalid"`
	Blocking    int `json:"blocking" yaml:"blocking"`
}

type ApprovalScanItem struct {
	Path            string             `json:"path" yaml:"path"`
	Name            string             `json:"name,omitempty" yaml:"name,omitempty"`
	Status          ApprovalScanStatus `json:"status" yaml:"status"`
	Blocking        bool               `json:"blocking,omitempty" yaml:"blocking,omitempty"`
	Error           string             `json:"error,omitempty" yaml:"error,omitempty"`
	Owner           string             `json:"owner,omitempty" yaml:"owner,omitempty"`
	ApprovedBy      string             `json:"approvedBy,omitempty" yaml:"approvedBy,omitempty"`
	ChangeRef       string             `json:"changeRef,omitempty" yaml:"changeRef,omitempty"`
	ExpiresAt       string             `json:"expiresAt,omitempty" yaml:"expiresAt,omitempty"`
	DaysUntilExpiry int                `json:"daysUntilExpiry,omitempty" yaml:"daysUntilExpiry,omitempty"`
	FollowUpAction  string             `json:"followUpAction,omitempty" yaml:"followUpAction,omitempty"`
}

var approvalScanNow = time.Now

func ScanApprovals(root string, config ApprovalScanConfig) (*ApprovalScanResult, error) {
	if strings.TrimSpace(root) == "" {
		return nil, fmt.Errorf("approval scan root cannot be empty")
	}
	cleanRoot := filepath.Clean(root)

	result := &ApprovalScanResult{
		Passed: true,
		Config: config,
	}

	now := approvalScanNow().UTC()
	err := filepath.WalkDir(cleanRoot, func(path string, d fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if d.IsDir() {
			return nil
		}
		if !isApprovalScanCandidate(d.Name()) {
			return nil
		}

		itemPath := path
		if rel, err := filepath.Rel(cleanRoot, path); err == nil {
			itemPath = filepath.ToSlash(rel)
		}
		item := ApprovalScanItem{Path: itemPath}
		result.Summary.Scanned++

		doc, err := LoadGateApprovalFile(path)
		if err != nil {
			item.Status = ApprovalScanStatusInvalid
			item.Blocking = true
			item.Error = err.Error()
			item.FollowUpAction = ApprovalScanFollowUpRepairInvalidDocument
			result.Summary.Invalid++
			result.Summary.Blocking++
			result.Warnings = append(result.Warnings, fmt.Sprintf("approval file %q is invalid: %v", item.Path, err))
			result.Approvals = append(result.Approvals, item)
			result.Passed = false
			return nil
		}

		expiresAt, err := doc.ExpirationTime()
		if err != nil {
			item.Status = ApprovalScanStatusInvalid
			item.Blocking = true
			item.Error = err.Error()
			item.FollowUpAction = ApprovalScanFollowUpRepairInvalidDocument
			result.Summary.Invalid++
			result.Summary.Blocking++
			result.Warnings = append(result.Warnings, fmt.Sprintf("approval file %q is invalid: %v", item.Path, err))
			result.Approvals = append(result.Approvals, item)
			result.Passed = false
			return nil
		}

		item.Name = strings.TrimSpace(doc.Metadata.Name)
		item.Owner = doc.Spec.Owner
		item.ApprovedBy = doc.Spec.ApprovedBy
		item.ChangeRef = doc.Spec.ChangeRef
		item.ExpiresAt = expiresAt.UTC().Format(time.RFC3339)

		switch {
		case !expiresAt.After(now):
			item.Status = ApprovalScanStatusExpired
			item.Blocking = true
			item.FollowUpAction = ApprovalScanFollowUpRemoveExpiredApproval
			result.Summary.Expired++
			result.Summary.Blocking++
			result.Warnings = append(result.Warnings,
				fmt.Sprintf("approval file %q expired at %s; remove or replace it",
					item.Path, item.ExpiresAt))
			result.Passed = false
		case approvalScanWarningWindow(config) > 0 && expiresAt.Sub(now) <= approvalScanWarningWindow(config):
			item.Status = ApprovalScanStatusExpiresSoon
			item.DaysUntilExpiry = gateApprovalDaysUntilExpiry(now, expiresAt)
			item.FollowUpAction = GateApprovalFollowUpRenewOrRemoveBeforeExpiry
			result.Summary.ExpiresSoon++
			result.Warnings = append(result.Warnings,
				fmt.Sprintf("approval file %q expires in %d day(s); renew or remove it before %s",
					item.Path, item.DaysUntilExpiry, item.ExpiresAt))
			if config.FailOnApprovalExpiresSoon {
				item.Blocking = true
				result.Summary.Blocking++
				result.Passed = false
			}
		default:
			item.Status = ApprovalScanStatusValid
			item.DaysUntilExpiry = gateApprovalDaysUntilExpiry(now, expiresAt)
			result.Summary.Valid++
		}

		result.Approvals = append(result.Approvals, item)
		return nil
	})
	if err != nil {
		return nil, err
	}

	sort.Slice(result.Approvals, func(i, j int) bool {
		return result.Approvals[i].Path < result.Approvals[j].Path
	})

	return result, nil
}

func approvalScanWarningWindow(config ApprovalScanConfig) time.Duration {
	if config.ApprovalExpiryWarningDays <= 0 {
		return 0
	}
	return time.Duration(config.ApprovalExpiryWarningDays) * 24 * time.Hour
}

func isApprovalScanCandidate(name string) bool {
	if !strings.HasPrefix(name, "local-patch-policy-approval") {
		return false
	}
	return strings.HasSuffix(name, ".yaml") || strings.HasSuffix(name, ".yml")
}
