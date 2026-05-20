package main

import (
	"flag"
	"fmt"
	"os"
	"strings"

	"sigs.k8s.io/yaml"
)

type scanOutput struct {
	Root string `json:"root" yaml:"root"`
	Scan *struct {
		Passed bool `json:"passed" yaml:"passed"`
		Config *struct {
			ApprovalExpiryWarningDays int  `json:"approvalExpiryWarningDays" yaml:"approvalExpiryWarningDays"`
			FailOnApprovalExpiresSoon bool `json:"failOnApprovalExpiresSoon" yaml:"failOnApprovalExpiresSoon"`
		} `json:"config,omitempty" yaml:"config,omitempty"`
		Summary *struct {
			Scanned     int `json:"scanned" yaml:"scanned"`
			Valid       int `json:"valid" yaml:"valid"`
			ExpiresSoon int `json:"expiresSoon" yaml:"expiresSoon"`
			Expired     int `json:"expired" yaml:"expired"`
			Invalid     int `json:"invalid" yaml:"invalid"`
			Blocking    int `json:"blocking" yaml:"blocking"`
		} `json:"summary,omitempty" yaml:"summary,omitempty"`
		Approvals []struct {
			Path            string `json:"path" yaml:"path"`
			Status          string `json:"status" yaml:"status"`
			Blocking        bool   `json:"blocking,omitempty" yaml:"blocking,omitempty"`
			Owner           string `json:"owner,omitempty" yaml:"owner,omitempty"`
			ChangeRef       string `json:"changeRef,omitempty" yaml:"changeRef,omitempty"`
			ExpiresAt       string `json:"expiresAt,omitempty" yaml:"expiresAt,omitempty"`
			DaysUntilExpiry int    `json:"daysUntilExpiry,omitempty" yaml:"daysUntilExpiry,omitempty"`
			FollowUpAction  string `json:"followUpAction,omitempty" yaml:"followUpAction,omitempty"`
			Error           string `json:"error,omitempty" yaml:"error,omitempty"`
		} `json:"approvals,omitempty" yaml:"approvals,omitempty"`
	} `json:"scan,omitempty" yaml:"scan,omitempty"`
}

func main() {
	reportPath := flag.String("report", "", "path to a sync policy-approval-scan YAML output file")
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

	var report scanOutput
	if err := yaml.Unmarshal(data, &report); err != nil {
		fmt.Fprintf(os.Stderr, "decode report %q: %v\n", *reportPath, err)
		os.Exit(1)
	}

	fmt.Println("## Approval scan")
	fmt.Println()
	if report.Scan == nil || report.Scan.Summary == nil {
		fmt.Println("- No approval scan summary was present in this report.")
		return
	}

	fmt.Printf("- Scan root: `%s`\n", safeValue(report.Root))
	if report.Scan.Config != nil {
		fmt.Printf("- Near-expiry warning window: `%d` day(s)\n", report.Scan.Config.ApprovalExpiryWarningDays)
		fmt.Printf("- Near-expiry is blocking: `%s`\n", yesNo(report.Scan.Config.FailOnApprovalExpiresSoon))
	}
	fmt.Printf("- Scan passed: `%s`\n", yesNo(report.Scan.Passed))
	fmt.Printf("- Scanned approvals: `%d`\n", report.Scan.Summary.Scanned)
	fmt.Printf("- Valid approvals: `%d`\n", report.Scan.Summary.Valid)
	fmt.Printf("- Near-expiry approvals: `%d`\n", report.Scan.Summary.ExpiresSoon)
	fmt.Printf("- Expired approvals: `%d`\n", report.Scan.Summary.Expired)
	fmt.Printf("- Invalid approvals: `%d`\n", report.Scan.Summary.Invalid)
	fmt.Printf("- Blocking approvals: `%d`\n", report.Scan.Summary.Blocking)

	printSection("Expired approvals", report.Scan.Approvals, func(item scanApprovalItem) bool {
		return item.Status == "expired"
	})
	printSection("Near-expiry approvals", report.Scan.Approvals, func(item scanApprovalItem) bool {
		return item.Status == "expiresSoon"
	})
	printSection("Invalid approvals", report.Scan.Approvals, func(item scanApprovalItem) bool {
		return item.Status == "invalid"
	})
}

type scanApprovalItem struct {
	Path            string
	Status          string
	Blocking        bool
	Owner           string
	ChangeRef       string
	ExpiresAt       string
	DaysUntilExpiry int
	FollowUpAction  string
	Error           string
}

func printSection(title string, items []struct {
	Path            string `json:"path" yaml:"path"`
	Status          string `json:"status" yaml:"status"`
	Blocking        bool   `json:"blocking,omitempty" yaml:"blocking,omitempty"`
	Owner           string `json:"owner,omitempty" yaml:"owner,omitempty"`
	ChangeRef       string `json:"changeRef,omitempty" yaml:"changeRef,omitempty"`
	ExpiresAt       string `json:"expiresAt,omitempty" yaml:"expiresAt,omitempty"`
	DaysUntilExpiry int    `json:"daysUntilExpiry,omitempty" yaml:"daysUntilExpiry,omitempty"`
	FollowUpAction  string `json:"followUpAction,omitempty" yaml:"followUpAction,omitempty"`
	Error           string `json:"error,omitempty" yaml:"error,omitempty"`
}, match func(scanApprovalItem) bool) {
	filtered := make([]scanApprovalItem, 0)
	for _, item := range items {
		converted := scanApprovalItem{
			Path:            item.Path,
			Status:          item.Status,
			Blocking:        item.Blocking,
			Owner:           item.Owner,
			ChangeRef:       item.ChangeRef,
			ExpiresAt:       item.ExpiresAt,
			DaysUntilExpiry: item.DaysUntilExpiry,
			FollowUpAction:  item.FollowUpAction,
			Error:           item.Error,
		}
		if match(converted) {
			filtered = append(filtered, converted)
		}
	}

	fmt.Println()
	fmt.Printf("### %s\n\n", title)
	if len(filtered) == 0 {
		fmt.Println("- None")
		return
	}
	for _, item := range filtered {
		fmt.Printf("- `%s`", safeValue(item.Path))
		if item.Owner != "" {
			fmt.Printf(" owner=`%s`", item.Owner)
		}
		if item.ChangeRef != "" {
			fmt.Printf(" changeRef=`%s`", item.ChangeRef)
		}
		if item.ExpiresAt != "" {
			fmt.Printf(" expiresAt=`%s`", item.ExpiresAt)
		}
		if item.DaysUntilExpiry > 0 {
			fmt.Printf(" daysUntilExpiry=`%d`", item.DaysUntilExpiry)
		}
		if item.FollowUpAction != "" {
			fmt.Printf(" followUpAction=`%s`", item.FollowUpAction)
		}
		if item.Error != "" {
			fmt.Printf(" error=`%s`", item.Error)
		}
		if item.Blocking {
			fmt.Printf(" blocking=`yes`")
		}
		fmt.Println()
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
