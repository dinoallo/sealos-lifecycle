package main

import (
	"flag"
	"fmt"
	"os"
	"sort"
	"strings"

	"sigs.k8s.io/yaml"
)

type gateOutput struct {
	Skipped bool   `json:"skipped,omitempty" yaml:"skipped,omitempty"`
	Reason  string `json:"reason,omitempty" yaml:"reason,omitempty"`
	Gate    *struct {
		Config *struct {
			ApprovalExpiryWarningDays int  `json:"approvalExpiryWarningDays" yaml:"approvalExpiryWarningDays"`
			FailOnApprovalExpiresSoon bool `json:"failOnApprovalExpiresSoon" yaml:"failOnApprovalExpiresSoon"`
		} `json:"config,omitempty" yaml:"config,omitempty"`
		ApprovalSummary *struct {
			ApprovalProvided       bool     `json:"approvalProvided" yaml:"approvalProvided"`
			ApprovalApplied        bool     `json:"approvalApplied" yaml:"approvalApplied"`
			Owner                  string   `json:"owner,omitempty" yaml:"owner,omitempty"`
			ApprovedBy             string   `json:"approvedBy,omitempty" yaml:"approvedBy,omitempty"`
			ChangeRef              string   `json:"changeRef,omitempty" yaml:"changeRef,omitempty"`
			ExpiresAt              string   `json:"expiresAt,omitempty" yaml:"expiresAt,omitempty"`
			ExpiresSoon            bool     `json:"expiresSoon,omitempty" yaml:"expiresSoon,omitempty"`
			DaysUntilExpiry        int      `json:"daysUntilExpiry,omitempty" yaml:"daysUntilExpiry,omitempty"`
			FollowUpAction         string   `json:"followUpAction,omitempty" yaml:"followUpAction,omitempty"`
			ApprovedViolationCodes []string `json:"approvedViolationCodes,omitempty" yaml:"approvedViolationCodes,omitempty"`
		} `json:"approvalSummary,omitempty" yaml:"approvalSummary,omitempty"`
	} `json:"gate,omitempty" yaml:"gate,omitempty"`
}

func main() {
	reportPath := flag.String("report", "", "path to a sync policy-gate YAML output file")
	flag.Parse()

	if strings.TrimSpace(*reportPath) == "" {
		fmt.Fprintln(os.Stderr, "--report is required")
		os.Exit(1)
	}

	data, err := os.ReadFile(*reportPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "read report %q: %v\n", *reportPath, err)
		os.Exit(1)
	}

	var report gateOutput
	if err := yaml.Unmarshal(data, &report); err != nil {
		fmt.Fprintf(os.Stderr, "decode report %q: %v\n", *reportPath, err)
		os.Exit(1)
	}

	fmt.Println("## Approval follow-up")
	fmt.Println()

	if report.Skipped {
		fmt.Printf("- Gate report skipped: `%s`\n", safeValue(report.Reason))
		return
	}
	if report.Gate == nil || report.Gate.ApprovalSummary == nil {
		fmt.Println("- No approval summary was present in this gate report.")
		return
	}

	summary := report.Gate.ApprovalSummary
	if report.Gate.Config != nil {
		fmt.Printf("- Near-expiry warning window: `%d` day(s)\n", report.Gate.Config.ApprovalExpiryWarningDays)
		fmt.Printf("- Near-expiry is blocking: `%s`\n", yesNo(report.Gate.Config.FailOnApprovalExpiresSoon))
	}
	if !summary.ApprovalProvided {
		fmt.Println("- No approval file was provided for this gate run.")
		return
	}

	fmt.Println("- Approval file provided: `yes`")
	fmt.Printf("- Approval used to pass gate: `%s`\n", yesNo(summary.ApprovalApplied))
	fmt.Printf("- Owner: `%s`\n", safeValue(summary.Owner))
	fmt.Printf("- Approved by: `%s`\n", safeValue(summary.ApprovedBy))
	fmt.Printf("- Change ref: `%s`\n", safeValue(summary.ChangeRef))
	fmt.Printf("- Expires at: `%s`\n", safeValue(summary.ExpiresAt))
	fmt.Printf("- Expires soon: `%s`\n", yesNo(summary.ExpiresSoon))
	if summary.DaysUntilExpiry > 0 {
		fmt.Printf("- Days until expiry: `%d`\n", summary.DaysUntilExpiry)
	}
	if strings.TrimSpace(summary.FollowUpAction) != "" {
		fmt.Printf("- Follow-up action: `%s`\n", summary.FollowUpAction)
	}

	codes := append([]string(nil), summary.ApprovedViolationCodes...)
	sort.Strings(codes)
	if len(codes) > 0 {
		fmt.Printf("- Approved violation codes: `%s`\n", strings.Join(codes, "`, `"))
	}
	if summary.ApprovalProvided && !summary.ApprovalApplied {
		fmt.Println("- No approval-based exception was consumed in this gate run.")
	}
}

func yesNo(value bool) string {
	if value {
		return "yes"
	}
	return "no"
}

func safeValue(value string) string {
	if strings.TrimSpace(value) == "" {
		return "n/a"
	}
	return value
}
