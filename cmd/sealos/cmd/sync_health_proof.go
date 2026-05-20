package cmd

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/opencontainers/go-digest"
	"github.com/spf13/cobra"
	"sigs.k8s.io/yaml"

	"github.com/labring/sealos/pkg/distribution"
	"github.com/labring/sealos/pkg/distribution/bom"
	yamlutil "github.com/labring/sealos/pkg/utils/yaml"
)

type syncHealthProofAcceptanceReport struct {
	APIVersion string                              `json:"apiVersion" yaml:"apiVersion"`
	Kind       string                              `json:"kind" yaml:"kind"`
	Metadata   bom.Metadata                        `json:"metadata" yaml:"metadata"`
	Spec       syncHealthProofAcceptanceReportSpec `json:"spec" yaml:"spec"`
}

type syncHealthProofAcceptanceReportSpec struct {
	ClusterName           string                                 `json:"clusterName" yaml:"clusterName"`
	StartedAt             string                                 `json:"startedAt" yaml:"startedAt"`
	FinishedAt            string                                 `json:"finishedAt" yaml:"finishedAt"`
	Status                string                                 `json:"status" yaml:"status"`
	ExitCode              int                                    `json:"exitCode" yaml:"exitCode"`
	MutatingApply         bool                                   `json:"mutatingApply" yaml:"mutatingApply"`
	RevertCheck           bool                                   `json:"revertCheck" yaml:"revertCheck"`
	PackageMode           string                                 `json:"packageMode" yaml:"packageMode"`
	BOMFile               string                                 `json:"bomFile" yaml:"bomFile"`
	BOMName               string                                 `json:"bomName" yaml:"bomName"`
	BOMRevision           string                                 `json:"bomRevision" yaml:"bomRevision"`
	BOMDigest             string                                 `json:"bomDigest" yaml:"bomDigest"`
	Workdir               string                                 `json:"workdir" yaml:"workdir"`
	RuntimeRoot           string                                 `json:"runtimeRoot" yaml:"runtimeRoot"`
	LocalRepo             string                                 `json:"localRepo" yaml:"localRepo"`
	BundleDir             string                                 `json:"bundleDir" yaml:"bundleDir"`
	Kubeconfig            string                                 `json:"kubeconfig" yaml:"kubeconfig"`
	HostRoot              string                                 `json:"hostRoot" yaml:"hostRoot"`
	OutputsFormat         string                                 `json:"outputsFormat" yaml:"outputsFormat"`
	DesiredStateDigest    string                                 `json:"desiredStateDigest" yaml:"desiredStateDigest"`
	LocalRepoRevision     string                                 `json:"localRepoRevision" yaml:"localRepoRevision"`
	SourcePreflightState  string                                 `json:"sourcePreflightState" yaml:"sourcePreflightState"`
	RuntimePreflightState string                                 `json:"runtimePreflightState" yaml:"runtimePreflightState"`
	PostApplyState        string                                 `json:"postApplyState" yaml:"postApplyState"`
	PostRevertState       string                                 `json:"postRevertState" yaml:"postRevertState"`
	Stages                []syncHealthProofAcceptanceReportStage `json:"stages" yaml:"stages"`
	Notes                 []string                               `json:"notes" yaml:"notes"`
}

type syncHealthProofAcceptanceReportStage struct {
	Name       string `json:"name" yaml:"name"`
	Status     string `json:"status" yaml:"status"`
	Mutates    bool   `json:"mutates" yaml:"mutates"`
	StartedAt  string `json:"startedAt" yaml:"startedAt"`
	FinishedAt string `json:"finishedAt" yaml:"finishedAt"`
	Output     string `json:"output,omitempty" yaml:"output,omitempty"`
	Command    string `json:"command,omitempty" yaml:"command,omitempty"`
	Reason     string `json:"reason,omitempty" yaml:"reason,omitempty"`
}

func newSyncHealthProofCmd() *cobra.Command {
	var flags struct {
		bomFile      string
		reportFile   string
		name         string
		summary      string
		collectedAt  string
		outputFile   string
		outputFormat string
	}

	cmd := &cobra.Command{
		Use:          "health-proof",
		Short:        "Generate a DistributionHealthProof from package acceptance evidence",
		Args:         cobra.NoArgs,
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			doc, err := bom.LoadFile(flags.bomFile)
			if err != nil {
				return err
			}
			report, err := loadSyncHealthProofAcceptanceReport(flags.reportFile)
			if err != nil {
				return err
			}

			collectedAt := strings.TrimSpace(flags.collectedAt)
			if collectedAt != "" {
				if _, err := time.Parse(time.RFC3339, collectedAt); err != nil {
					return fmt.Errorf("parse --collected-at as RFC3339: %w", err)
				}
			}
			proof, err := buildSyncHealthProof(doc, report, syncHealthProofOptions{
				Name:        flags.name,
				Summary:     flags.summary,
				CollectedAt: collectedAt,
				BOMPath:     flags.bomFile,
			})
			if err != nil {
				return err
			}

			if strings.TrimSpace(flags.outputFile) != "" {
				if err := yamlutil.MarshalFile(flags.outputFile, proof); err != nil {
					return fmt.Errorf("write health proof %q: %w", flags.outputFile, err)
				}
			}
			return writeSyncOutput(cmd, proof, flags.outputFormat, "health proof")
		},
	}
	cmd.Flags().StringVarP(&flags.bomFile, "file", "f", "", "path to the target BOM file")
	cmd.Flags().StringVar(&flags.reportFile, "acceptance-report", "", "PackageAcceptanceReport file produced by package lifecycle automation")
	cmd.Flags().StringVar(&flags.name, "name", "", "metadata.name for the generated DistributionHealthProof; defaults from BOM line and revision")
	cmd.Flags().StringVar(&flags.summary, "summary", "", "optional summary for the generated health proof")
	cmd.Flags().StringVar(&flags.collectedAt, "collected-at", "", "proof collection timestamp in RFC3339 format; defaults to report spec.finishedAt")
	cmd.Flags().StringVar(&flags.outputFile, "output-file", "", "optional path to write the generated DistributionHealthProof YAML")
	addSyncOutputFlag(cmd, &flags.outputFormat)
	mustMarkFlagRequired(cmd, "file")
	mustMarkFlagRequired(cmd, "acceptance-report")
	return cmd
}

func loadSyncHealthProofAcceptanceReport(path string) (*syncHealthProofAcceptanceReport, error) {
	if strings.TrimSpace(path) == "" {
		return nil, fmt.Errorf("acceptance report path cannot be empty")
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read acceptance report %q: %w", path, err)
	}
	var report syncHealthProofAcceptanceReport
	if err := yaml.Unmarshal(data, &report); err != nil {
		return nil, fmt.Errorf("unmarshal acceptance report %q: %w", path, err)
	}
	if err := validateSyncHealthProofAcceptanceReport(&report); err != nil {
		return nil, fmt.Errorf("validate acceptance report %q: %w", path, err)
	}
	return &report, nil
}

func validateSyncHealthProofAcceptanceReport(report *syncHealthProofAcceptanceReport) error {
	if report == nil {
		return fmt.Errorf("acceptance report cannot be nil")
	}
	if report.APIVersion != distribution.APIVersion {
		return fmt.Errorf("unsupported apiVersion %q", report.APIVersion)
	}
	if report.Kind != distribution.KindPackageAcceptanceReport {
		return fmt.Errorf("unsupported kind %q", report.Kind)
	}
	if strings.TrimSpace(report.Metadata.Name) == "" {
		return fmt.Errorf("metadata.name cannot be empty")
	}
	if strings.TrimSpace(report.Spec.Status) == "" {
		return fmt.Errorf("spec.status cannot be empty")
	}
	if strings.TrimSpace(report.Spec.FinishedAt) != "" {
		if _, err := time.Parse(time.RFC3339, report.Spec.FinishedAt); err != nil {
			return fmt.Errorf("spec.finishedAt must be RFC3339: %w", err)
		}
	}
	for i, stage := range report.Spec.Stages {
		if strings.TrimSpace(stage.Name) == "" {
			return fmt.Errorf("spec.stages[%d].name cannot be empty", i)
		}
		if strings.TrimSpace(stage.Status) == "" {
			return fmt.Errorf("spec.stages[%d].status cannot be empty", i)
		}
	}
	return nil
}

type syncHealthProofOptions struct {
	Name        string
	Summary     string
	CollectedAt string
	BOMPath     string
}

func buildSyncHealthProof(targetBOM *bom.BOM, report *syncHealthProofAcceptanceReport, opts syncHealthProofOptions) (*bom.DistributionHealthProof, error) {
	if targetBOM == nil {
		return nil, fmt.Errorf("target BOM cannot be nil")
	}
	if report == nil {
		return nil, fmt.Errorf("acceptance report cannot be nil")
	}
	proofName := strings.TrimSpace(opts.Name)
	if proofName == "" {
		proofName = defaultSyncHealthProofName(targetBOM.Metadata.Name, targetBOM.Spec.Revision)
	}
	signals := syncHealthProofSignals(report, targetBOM, opts.BOMPath)
	proof := bom.NewDistributionHealthProof(proofName, targetBOM.Metadata.Name, targetBOM.Spec.Revision, syncHealthProofSignalsPassed(signals))
	proof.Spec.Summary = syncHealthProofSummary(report, opts.Summary)
	proof.Spec.CollectedAt = syncHealthProofCollectedAt(report, opts.CollectedAt)
	proof.Spec.Signals = signals
	if err := proof.Validate(); err != nil {
		return nil, fmt.Errorf("validate generated health proof: %w", err)
	}
	return proof, nil
}

func defaultSyncHealthProofName(line, revision string) string {
	return sanitizeSyncHealthProofName(strings.TrimSpace(line) + "-" + strings.TrimSpace(revision) + "-health")
}

func sanitizeSyncHealthProofName(value string) string {
	value = strings.ToLower(strings.TrimSpace(value))
	var b strings.Builder
	lastDash := false
	for _, r := range value {
		valid := (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9')
		if valid {
			b.WriteRune(r)
			lastDash = false
			continue
		}
		if !lastDash {
			b.WriteByte('-')
			lastDash = true
		}
	}
	return strings.Trim(b.String(), "-")
}

func syncHealthProofSummary(report *syncHealthProofAcceptanceReport, override string) string {
	if strings.TrimSpace(override) != "" {
		return strings.TrimSpace(override)
	}
	var parts []string
	if report.Spec.ClusterName != "" {
		parts = append(parts, "cluster "+report.Spec.ClusterName)
	}
	parts = append(parts, "acceptance "+strings.ToLower(strings.TrimSpace(report.Spec.Status)))
	if report.Spec.MutatingApply {
		parts = append(parts, "apply exercised")
	} else {
		parts = append(parts, "safe preflight only")
	}
	if report.Spec.RevertCheck {
		parts = append(parts, "revert check exercised")
	}
	return strings.Join(parts, "; ")
}

func syncHealthProofCollectedAt(report *syncHealthProofAcceptanceReport, override string) string {
	if strings.TrimSpace(override) != "" {
		return strings.TrimSpace(override)
	}
	return strings.TrimSpace(report.Spec.FinishedAt)
}

func syncHealthProofSignals(report *syncHealthProofAcceptanceReport, targetBOM *bom.BOM, targetBOMPath string) []bom.DistributionHealthSignal {
	signals := []bom.DistributionHealthSignal{
		{
			Name:    "acceptance-report",
			Passed:  strings.EqualFold(strings.TrimSpace(report.Spec.Status), "Passed") && report.Spec.ExitCode == 0,
			Message: fmt.Sprintf("status=%s exitCode=%d", strings.TrimSpace(report.Spec.Status), report.Spec.ExitCode),
		},
		syncHealthProofBOMFileSignal(report.Spec.BOMFile, targetBOMPath),
		syncHealthProofBOMIdentitySignal(report.Spec.BOMName, report.Spec.BOMRevision, targetBOM),
		syncHealthProofBOMDigestSignal(report.Spec.BOMDigest, targetBOMPath),
		syncHealthProofDigestValueSignal("desired-state-digest", "desiredStateDigest", report.Spec.DesiredStateDigest),
		syncHealthProofDigestValueSignal("local-repo-revision", "localRepoRevision", report.Spec.LocalRepoRevision),
	}
	signals = append(signals, syncHealthProofContractSignals(report)...)
	signals = append(signals, bom.DistributionHealthSignal{
		Name:    "source-preflight",
		Passed:  syncHealthProofPreflightPassed(report.Spec.SourcePreflightState),
		Message: "state=" + syncHealthProofStateMessage(report.Spec.SourcePreflightState),
	})
	signals = append(signals, bom.DistributionHealthSignal{
		Name:    "runtime-preflight",
		Passed:  syncHealthProofPreflightPassed(report.Spec.RuntimePreflightState),
		Message: "state=" + syncHealthProofStateMessage(report.Spec.RuntimePreflightState),
	})
	signals = append(signals, bom.DistributionHealthSignal{
		Name:    "mutating-apply",
		Passed:  report.Spec.MutatingApply,
		Message: fmt.Sprintf("mutatingApply=%t", report.Spec.MutatingApply),
	})
	if report.Spec.MutatingApply && strings.TrimSpace(report.Spec.PostApplyState) != "" {
		signals = append(signals, bom.DistributionHealthSignal{
			Name:    "post-apply-drift",
			Passed:  strings.EqualFold(strings.TrimSpace(report.Spec.PostApplyState), syncHealthProofCleanState),
			Message: "currentState=" + strings.TrimSpace(report.Spec.PostApplyState),
		})
	} else if report.Spec.MutatingApply {
		signals = append(signals, bom.DistributionHealthSignal{
			Name:    "post-apply-drift",
			Passed:  false,
			Message: "currentState=<missing>",
		})
	}
	if report.Spec.RevertCheck && strings.TrimSpace(report.Spec.PostRevertState) != "" {
		signals = append(signals, bom.DistributionHealthSignal{
			Name:    "post-revert-drift",
			Passed:  strings.EqualFold(strings.TrimSpace(report.Spec.PostRevertState), syncHealthProofCleanState),
			Message: "currentState=" + strings.TrimSpace(report.Spec.PostRevertState),
		})
	} else if report.Spec.RevertCheck {
		signals = append(signals, bom.DistributionHealthSignal{
			Name:    "post-revert-drift",
			Passed:  false,
			Message: "currentState=<missing>",
		})
	}
	for _, stage := range report.Spec.Stages {
		status := strings.TrimSpace(stage.Status)
		if status == "" {
			continue
		}
		passed := strings.EqualFold(status, "Passed") || strings.EqualFold(status, "Skipped")
		message := "status=" + status
		if strings.TrimSpace(stage.Reason) != "" {
			message += " reason=" + strings.TrimSpace(stage.Reason)
		}
		signals = append(signals, bom.DistributionHealthSignal{
			Name:    "stage/" + strings.TrimSpace(stage.Name),
			Passed:  passed,
			Message: message,
		})
	}
	return signals
}

func syncHealthProofBOMIdentitySignal(reportBOMName, reportBOMRevision string, targetBOM *bom.BOM) bom.DistributionHealthSignal {
	reportBOMName = strings.TrimSpace(reportBOMName)
	reportBOMRevision = strings.TrimSpace(reportBOMRevision)
	if reportBOMName == "" || reportBOMRevision == "" {
		return bom.DistributionHealthSignal{
			Name:    "bom-identity",
			Passed:  false,
			Message: fmt.Sprintf("reportBOMName=%s reportBOMRevision=%s", syncHealthProofStateMessage(reportBOMName), syncHealthProofStateMessage(reportBOMRevision)),
		}
	}
	if targetBOM == nil {
		return bom.DistributionHealthSignal{
			Name:    "bom-identity",
			Passed:  false,
			Message: "targetBOM=<missing>",
		}
	}
	targetBOMName := strings.TrimSpace(targetBOM.Metadata.Name)
	targetBOMRevision := strings.TrimSpace(targetBOM.Spec.Revision)
	return bom.DistributionHealthSignal{
		Name:    "bom-identity",
		Passed:  reportBOMName == targetBOMName && reportBOMRevision == targetBOMRevision,
		Message: fmt.Sprintf("report=%s/%s target=%s/%s", reportBOMName, reportBOMRevision, targetBOMName, targetBOMRevision),
	}
}

func syncHealthProofBOMDigestSignal(reportBOMDigest, targetBOMPath string) bom.DistributionHealthSignal {
	reportBOMDigest = strings.TrimSpace(reportBOMDigest)
	if reportBOMDigest == "" {
		return bom.DistributionHealthSignal{
			Name:    "bom-digest",
			Passed:  false,
			Message: "reportBOMDigest=<missing>",
		}
	}
	targetBOMPath = strings.TrimSpace(targetBOMPath)
	if targetBOMPath == "" {
		return bom.DistributionHealthSignal{
			Name:    "bom-digest",
			Passed:  false,
			Message: "targetBOMFile=<missing>",
		}
	}
	data, err := os.ReadFile(targetBOMPath)
	if err != nil {
		return bom.DistributionHealthSignal{
			Name:    "bom-digest",
			Passed:  false,
			Message: fmt.Sprintf("read targetBOMFile: %v", err),
		}
	}
	targetBOMDigest := digest.Canonical.FromBytes(data).String()
	return bom.DistributionHealthSignal{
		Name:    "bom-digest",
		Passed:  reportBOMDigest == targetBOMDigest,
		Message: fmt.Sprintf("report=%s target=%s", reportBOMDigest, targetBOMDigest),
	}
}

func syncHealthProofBOMFileSignal(reportBOMPath, targetBOMPath string) bom.DistributionHealthSignal {
	reportBOMPath = strings.TrimSpace(reportBOMPath)
	targetBOMPath = strings.TrimSpace(targetBOMPath)
	if reportBOMPath == "" {
		return bom.DistributionHealthSignal{
			Name:    "bom-file",
			Passed:  false,
			Message: "reportBOMFile=<missing>",
		}
	}
	if targetBOMPath == "" {
		return bom.DistributionHealthSignal{
			Name:    "bom-file",
			Passed:  false,
			Message: "targetBOMFile=<missing>",
		}
	}
	reportAbs, reportErr := filepath.Abs(reportBOMPath)
	targetAbs, targetErr := filepath.Abs(targetBOMPath)
	if reportErr == nil && targetErr == nil {
		return bom.DistributionHealthSignal{
			Name:    "bom-file",
			Passed:  filepath.Clean(reportAbs) == filepath.Clean(targetAbs),
			Message: fmt.Sprintf("report=%s target=%s", filepath.Clean(reportAbs), filepath.Clean(targetAbs)),
		}
	}
	return bom.DistributionHealthSignal{
		Name:    "bom-file",
		Passed:  filepath.Clean(reportBOMPath) == filepath.Clean(targetBOMPath),
		Message: fmt.Sprintf("report=%s target=%s", filepath.Clean(reportBOMPath), filepath.Clean(targetBOMPath)),
	}
}

func syncHealthProofDigestValueSignal(name, field, value string) bom.DistributionHealthSignal {
	value = strings.TrimSpace(value)
	if value == "" {
		return bom.DistributionHealthSignal{
			Name:    name,
			Passed:  false,
			Message: field + "=<missing>",
		}
	}
	if _, err := digest.Parse(value); err != nil {
		return bom.DistributionHealthSignal{
			Name:    name,
			Passed:  false,
			Message: fmt.Sprintf("%s=%s invalid: %v", field, value, err),
		}
	}
	return bom.DistributionHealthSignal{
		Name:    name,
		Passed:  true,
		Message: field + "=" + value,
	}
}

func syncHealthProofContractSignals(report *syncHealthProofAcceptanceReport) []bom.DistributionHealthSignal {
	stageByName := make(map[string]syncHealthProofAcceptanceReportStage, len(report.Spec.Stages))
	for _, stage := range report.Spec.Stages {
		stage.Name = strings.TrimSpace(stage.Name)
		stage.Status = strings.TrimSpace(stage.Status)
		stageByName[stage.Name] = stage
	}

	type contractStage struct {
		name    string
		mutates bool
	}
	required := []contractStage{
		{name: "package-inspect-containerd"},
		{name: "package-inspect-kubernetes"},
		{name: "package-inspect-cilium"},
		{name: "local-repo-init"},
		{name: "fill-local-repo-inputs"},
		{name: "local-repo-doctor"},
		{name: "validate"},
		{name: "source-preflight"},
		{name: "render"},
		{name: "verify-sourcePreflight-metadata"},
		{name: "runtime-preflight"},
		{name: "plan"},
		{name: "apply", mutates: true},
		{name: "status"},
		{name: "diff"},
		{name: "validate-cluster", mutates: true},
	}
	if report.Spec.RevertCheck {
		required = append(required,
			contractStage{name: "revert-check-drift-inject", mutates: true},
			contractStage{name: "revert-check-drift-diff"},
			contractStage{name: "revert-check-revert", mutates: true},
			contractStage{name: "revert-check-clean-diff"},
			contractStage{name: "validate-cluster-after-revert", mutates: true},
		)
	}

	signals := make([]bom.DistributionHealthSignal, 0, len(required))
	for _, requiredStage := range required {
		stage, ok := stageByName[requiredStage.name]
		status := strings.TrimSpace(stage.Status)
		if !ok || strings.TrimSpace(status) == "" {
			signals = append(signals, bom.DistributionHealthSignal{
				Name:    "contract/" + requiredStage.name,
				Passed:  false,
				Message: "stage=<missing>",
			})
			continue
		}
		passed := strings.EqualFold(status, "Passed")
		message := "status=" + status
		if requiredStage.mutates {
			passed = passed && stage.Mutates
			message += fmt.Sprintf(" mutates=%t", stage.Mutates)
		}
		signals = append(signals, bom.DistributionHealthSignal{
			Name:    "contract/" + requiredStage.name,
			Passed:  passed,
			Message: message,
		})
	}
	return signals
}

func syncHealthProofPreflightPassed(state string) bool {
	state = strings.TrimSpace(state)
	return strings.EqualFold(state, string(syncPreflightStateReady)) || strings.EqualFold(state, string(syncPreflightStateWarning))
}

func syncHealthProofStateMessage(state string) string {
	state = strings.TrimSpace(state)
	if state == "" {
		return "<missing>"
	}
	return state
}

func syncHealthProofSignalsPassed(signals []bom.DistributionHealthSignal) bool {
	if len(signals) == 0 {
		return false
	}
	for _, signal := range signals {
		if !signal.Passed {
			return false
		}
	}
	return true
}

const syncHealthProofCleanState = "Clean"
