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

package cmd

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/opencontainers/go-digest"
	"github.com/spf13/cobra"
	"sigs.k8s.io/yaml"

	"github.com/labring/sealos/pkg/buildah"
	"github.com/labring/sealos/pkg/constants"
	"github.com/labring/sealos/pkg/distribution/bom"
	distcommit "github.com/labring/sealos/pkg/distribution/commit"
	"github.com/labring/sealos/pkg/distribution/compare"
	"github.com/labring/sealos/pkg/distribution/hydrate"
	"github.com/labring/sealos/pkg/distribution/localrepo"
	"github.com/labring/sealos/pkg/distribution/ocipackage"
	"github.com/labring/sealos/pkg/distribution/ownership"
	"github.com/labring/sealos/pkg/distribution/packageformat"
	"github.com/labring/sealos/pkg/distribution/policyreport"
	"github.com/labring/sealos/pkg/distribution/reconcile"
	"github.com/labring/sealos/pkg/distribution/state"
)

type syncOutputFormat string

const (
	syncOutputFormatYAML syncOutputFormat = "yaml"
	syncOutputFormatJSON syncOutputFormat = "json"
)

func addSyncOutputFlag(cmd *cobra.Command, target *string) {
	cmd.Flags().StringVar(target, "output", string(syncOutputFormatYAML), "output format: yaml or json")
}

func writeSyncOutput(cmd *cobra.Command, value any, format, subject string) error {
	switch syncOutputFormat(strings.ToLower(strings.TrimSpace(format))) {
	case "", syncOutputFormatYAML:
		data, err := yaml.Marshal(value)
		if err != nil {
			return fmt.Errorf("marshal %s as yaml: %w", subject, err)
		}
		_, err = cmd.OutOrStdout().Write(data)
		return err
	case syncOutputFormatJSON:
		data, err := json.MarshalIndent(value, "", "  ")
		if err != nil {
			return fmt.Errorf("marshal %s as json: %w", subject, err)
		}
		data = append(data, '\n')
		_, err = cmd.OutOrStdout().Write(data)
		return err
	default:
		return fmt.Errorf("unsupported output format %q, want yaml or json", format)
	}
}

type syncBuildahFactory func(id string) (buildah.Interface, error)

var newSyncBuildah syncBuildahFactory = buildah.New
var newSyncMountedArtifactResolver = defaultSyncMountedArtifactResolver
var newSyncCachedArtifactResolver = defaultSyncCachedArtifactResolver
var outputSyncKubectl = defaultSyncKubectlOutput
var runSyncApply = reconcile.Apply
var repairSyncGeneratedControlPlaneHost = reconcile.RepairGeneratedControlPlaneHost

type syncTargetOptions struct {
	BOMPath                 string
	DistributionChannelPath string
}

type syncResolvedTarget struct {
	BOM                     *bom.BOM
	BOMPath                 string
	DistributionChannel     *bom.DistributionChannel
	DistributionChannelPath string
}

func addSyncTargetFlags(cmd *cobra.Command, bomPath, distributionChannelPath *string, bomUsage string) {
	cmd.Flags().StringVarP(bomPath, "file", "f", "", bomUsage)
	cmd.Flags().StringVar(distributionChannelPath, "distribution-channel", "", "path to a DistributionChannel file to resolve before loading the target BOM")
}

func resolveSyncTarget(opts syncTargetOptions) (*syncResolvedTarget, error) {
	bomPath := strings.TrimSpace(opts.BOMPath)
	channelPath := strings.TrimSpace(opts.DistributionChannelPath)
	switch {
	case bomPath == "" && channelPath == "":
		return nil, fmt.Errorf("one of --file or --distribution-channel is required")
	case bomPath != "" && channelPath != "":
		return nil, fmt.Errorf("use either --file or --distribution-channel, not both")
	case channelPath != "":
		resolved, err := bom.ResolveDistributionChannelFile(channelPath)
		if err != nil {
			return nil, err
		}
		return &syncResolvedTarget{
			BOM:                     resolved.BOM,
			BOMPath:                 resolved.BOMPath,
			DistributionChannel:     resolved.Channel,
			DistributionChannelPath: channelPath,
		}, nil
	default:
		doc, err := bom.LoadFile(bomPath)
		if err != nil {
			return nil, err
		}
		return &syncResolvedTarget{
			BOM:     doc,
			BOMPath: bomPath,
		}, nil
	}
}

func addSyncRuntimeRootFlag(cmd *cobra.Command) {
	var runtimeRoot string
	cmd.Flags().StringVar(&runtimeRoot, "runtime-root", "", "override the cluster runtime root used to resolve sync state, bundles, and inventory")
	previousPreRunE := cmd.PreRunE
	cmd.PreRunE = func(cmd *cobra.Command, args []string) error {
		if previousPreRunE != nil {
			if err := previousPreRunE(cmd, args); err != nil {
				return err
			}
		}
		if strings.TrimSpace(runtimeRoot) != "" {
			constants.DefaultRuntimeRootDir = runtimeRoot
		}
		return nil
	}
}

func newSyncCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "sync",
		Short: "Experimental distribution workflows for package-based cluster state",
		Args:  cobra.NoArgs,
	}
	cmd.AddCommand(newSyncPackageCmd())
	cmd.AddCommand(newSyncLocalRepoCmd())
	validateCmd := newSyncValidateCmd()
	addSyncRuntimeRootFlag(validateCmd)
	cmd.AddCommand(validateCmd)
	renderCmd := newSyncRenderCmd()
	addSyncRuntimeRootFlag(renderCmd)
	cmd.AddCommand(renderCmd)
	planCmd := newSyncPlanCmd()
	addSyncRuntimeRootFlag(planCmd)
	cmd.AddCommand(planCmd)
	preflightCmd := newSyncPreflightCmd()
	addSyncRuntimeRootFlag(preflightCmd)
	cmd.AddCommand(preflightCmd)
	applyCmd := newSyncApplyCmd()
	addSyncRuntimeRootFlag(applyCmd)
	cmd.AddCommand(applyCmd)
	diffCmd := newSyncDiffCmd()
	addSyncRuntimeRootFlag(diffCmd)
	cmd.AddCommand(diffCmd)
	statusCmd := newSyncStatusCmd()
	addSyncRuntimeRootFlag(statusCmd)
	cmd.AddCommand(statusCmd)
	commitCmd := newSyncCommitCmd()
	addSyncRuntimeRootFlag(commitCmd)
	cmd.AddCommand(commitCmd)
	revertCmd := newSyncRevertCmd()
	addSyncRuntimeRootFlag(revertCmd)
	cmd.AddCommand(revertCmd)
	policyApprovalScanCmd := newSyncPolicyApprovalScanCmd()
	addSyncRuntimeRootFlag(policyApprovalScanCmd)
	cmd.AddCommand(policyApprovalScanCmd)
	cmd.AddCommand(newSyncPolicyReportCmd())
	cmd.AddCommand(newSyncPolicyGateCmd())
	return cmd
}

func newSyncPolicyApprovalScanCmd() *cobra.Command {
	var flags struct {
		root                        string
		approvalExpiryWarningDays   int
		failWhenApprovalExpiresSoon bool
		output                      string
	}

	defaultConfig := policyreport.DefaultApprovalScanConfig()

	cmd := &cobra.Command{
		Use:          "policy-approval-scan",
		Short:        "Scan LocalPatchPolicyGateApproval files for invalid, expired, or near-expiry exceptions",
		Args:         cobra.NoArgs,
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			scan, err := policyreport.ScanApprovals(flags.root, policyreport.ApprovalScanConfig{
				ApprovalExpiryWarningDays: flags.approvalExpiryWarningDays,
				FailOnApprovalExpiresSoon: flags.failWhenApprovalExpiresSoon,
			})
			if err != nil {
				return err
			}

			out := syncPolicyApprovalScanOutput{
				Root: flags.root,
				Scan: scan,
			}
			if err := writeSyncOutput(cmd, out, flags.output, "policy approval scan result"); err != nil {
				return err
			}
			if !scan.Passed {
				return errors.New("local patch policy approval scan failed")
			}
			return nil
		},
	}
	cmd.Flags().StringVar(&flags.root, "root", ".", "root directory to recursively scan for LocalPatchPolicyGateApproval YAML files")
	cmd.Flags().IntVar(&flags.approvalExpiryWarningDays, "approval-expiry-warning-days", defaultConfig.ApprovalExpiryWarningDays, "number of days before approval expiry that should be treated as near-expiry")
	cmd.Flags().BoolVar(&flags.failWhenApprovalExpiresSoon, "fail-when-approval-expires-soon", defaultConfig.FailOnApprovalExpiresSoon, "treat near-expiry approvals as blocking scan failures instead of warnings")
	addSyncOutputFlag(cmd, &flags.output)
	return cmd
}

func newSyncPolicyReportCmd() *cobra.Command {
	var flags struct {
		oldPolicy string
		newPolicy string
		localRepo string
		output    string
	}

	cmd := &cobra.Command{
		Use:          "policy-report",
		Short:        "Compare two LocalPatchPolicy documents and report widening, narrowing, and local patch compatibility impact",
		Args:         cobra.NoArgs,
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			oldDoc, err := ownership.LoadLocalPatchPolicyFile(flags.oldPolicy)
			if err != nil {
				return err
			}
			newDoc, err := ownership.LoadLocalPatchPolicyFile(flags.newPolicy)
			if err != nil {
				return err
			}

			var repo *localrepo.Repo
			if strings.TrimSpace(flags.localRepo) != "" {
				repo, err = localrepo.Load(flags.localRepo)
				if err != nil {
					return err
				}
			}

			report, err := policyreport.Build(oldDoc, newDoc, repo)
			if err != nil {
				return err
			}
			out := syncPolicyReportOutput{
				OldPolicyPath: flags.oldPolicy,
				NewPolicyPath: flags.newPolicy,
				LocalRepo:     flags.localRepo,
				Report:        report,
			}
			return writeSyncOutput(cmd, out, flags.output, "policy report result")
		},
	}
	cmd.Flags().StringVar(&flags.oldPolicy, "old-policy", "", "path to the previous LocalPatchPolicy YAML")
	cmd.Flags().StringVar(&flags.newPolicy, "new-policy", "", "path to the candidate LocalPatchPolicy YAML")
	cmd.Flags().StringVar(&flags.localRepo, "local-repo", "", "optional local repo root used to evaluate current patch compatibility")
	addSyncOutputFlag(cmd, &flags.output)
	if err := cmd.MarkFlagRequired("old-policy"); err != nil {
		panic(err)
	}
	if err := cmd.MarkFlagRequired("new-policy"); err != nil {
		panic(err)
	}
	return cmd
}

func newSyncPolicyGateCmd() *cobra.Command {
	var flags struct {
		oldPolicy                   string
		newPolicy                   string
		localRepo                   string
		approvalFile                string
		allowWidening               bool
		allowIncompatiblePatch      bool
		approvalExpiryWarningDays   int
		failWhenApprovalExpiresSoon bool
		output                      string
	}

	defaultGateConfig := policyreport.DefaultGateConfig()

	cmd := &cobra.Command{
		Use:          "policy-gate",
		Short:        "Evaluate whether a LocalPatchPolicy change should be blocked based on widening and existing local patch compatibility",
		Args:         cobra.NoArgs,
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			oldDoc, err := ownership.LoadLocalPatchPolicyFile(flags.oldPolicy)
			if err != nil {
				return err
			}
			newDoc, err := ownership.LoadLocalPatchPolicyFile(flags.newPolicy)
			if err != nil {
				return err
			}

			var repo *localrepo.Repo
			if strings.TrimSpace(flags.localRepo) != "" {
				repo, err = localrepo.Load(flags.localRepo)
				if err != nil {
					return err
				}
			}
			var approval *policyreport.GateApprovalDocument
			if strings.TrimSpace(flags.approvalFile) != "" {
				approval, err = policyreport.LoadGateApprovalFile(flags.approvalFile)
				if err != nil {
					return err
				}
			}

			report, err := policyreport.Build(oldDoc, newDoc, repo)
			if err != nil {
				return err
			}
			gate, err := policyreport.EvaluateGateWithApproval(report, policyreport.GateConfig{
				FailOnWidening:            !flags.allowWidening,
				FailOnIncompatiblePatches: !flags.allowIncompatiblePatch,
				ApprovalExpiryWarningDays: flags.approvalExpiryWarningDays,
				FailOnApprovalExpiresSoon: flags.failWhenApprovalExpiresSoon,
			}, approval)
			if err != nil {
				return err
			}

			out := syncPolicyGateOutput{
				OldPolicyPath: flags.oldPolicy,
				NewPolicyPath: flags.newPolicy,
				LocalRepo:     flags.localRepo,
				ApprovalFile:  flags.approvalFile,
				Gate:          gate,
				Report:        report,
			}
			if err := writeSyncOutput(cmd, out, flags.output, "policy gate result"); err != nil {
				return err
			}
			if !gate.Passed {
				return errors.New("local patch policy gate failed")
			}
			return nil
		},
	}
	cmd.Flags().StringVar(&flags.oldPolicy, "old-policy", "", "path to the previous LocalPatchPolicy YAML")
	cmd.Flags().StringVar(&flags.newPolicy, "new-policy", "", "path to the candidate LocalPatchPolicy YAML")
	cmd.Flags().StringVar(&flags.localRepo, "local-repo", "", "optional local repo root used to evaluate current patch compatibility")
	cmd.Flags().StringVar(&flags.approvalFile, "approval-file", "", "optional LocalPatchPolicyGateApproval YAML that explicitly approves specific gate violations")
	cmd.Flags().BoolVar(&flags.allowWidening, "allow-widening", false, "allow widening LocalPatchPolicy changes without failing the gate")
	cmd.Flags().BoolVar(&flags.allowIncompatiblePatch, "allow-incompatible-patches", false, "allow the candidate policy to reject existing local patches without failing the gate")
	cmd.Flags().IntVar(&flags.approvalExpiryWarningDays, "approval-expiry-warning-days", defaultGateConfig.ApprovalExpiryWarningDays, "number of days before approval expiry that should be treated as near-expiry")
	cmd.Flags().BoolVar(&flags.failWhenApprovalExpiresSoon, "fail-when-approval-expires-soon", defaultGateConfig.FailOnApprovalExpiresSoon, "treat near-expiry approvals as blocking gate violations instead of warnings")
	addSyncOutputFlag(cmd, &flags.output)
	if err := cmd.MarkFlagRequired("old-policy"); err != nil {
		panic(err)
	}
	if err := cmd.MarkFlagRequired("new-policy"); err != nil {
		panic(err)
	}
	return cmd
}

func newSyncRenderCmd() *cobra.Command {
	var flags struct {
		bomFile                 string
		distributionChannelFile string
		clusterName             string
		localRepo               string
		localPatchRevision      string
		packageSources          []string
		skipSourcePreflight     bool
		output                  string
	}

	cmd := &cobra.Command{
		Use:          "render",
		Short:        "Render a BOM into a cluster-local desired-state bundle from package artifacts",
		SilenceUsage: true,
		Long: strings.TrimSpace(`
Render resolves component packages from the BOM's OCI image and digest
references by default.

Use --package-source only as a local development override for individual
components when iterating on package directories in-tree.
`),
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			target, err := resolveSyncTarget(syncTargetOptions{
				BOMPath:                 flags.bomFile,
				DistributionChannelPath: flags.distributionChannelFile,
			})
			if err != nil {
				return err
			}

			var sourcePreflight *syncSourcePreflightOutput
			if !flags.skipSourcePreflight {
				out := runSyncSourcePreflight(syncSourcePreflightOptions{
					ClusterName:             flags.clusterName,
					BOMPath:                 flags.bomFile,
					DistributionChannelPath: flags.distributionChannelFile,
					LocalRepoPath:           flags.localRepo,
					PackageSources:          flags.packageSources,
				})
				sourcePreflight = &out
				if out.Blocked {
					renderOut := syncRenderOutput{
						ClusterName:     flags.clusterName,
						SourcePreflight: sourcePreflight,
					}
					if err := writeSyncOutput(cmd, renderOut, flags.output, "render result"); err != nil {
						return err
					}
					fmt.Fprintf(cmd.ErrOrStderr(), "error: sync render source preflight blocked: %s\n", strings.Join(out.BlockedReasons, "; "))
					return fmt.Errorf("sync render source preflight blocked: %s", strings.Join(out.BlockedReasons, "; "))
				}
			}

			opts, err := newSyncMaterializeOptions(target, flags.clusterName, flags.localRepo, flags.localPatchRevision, flags.packageSources)
			if err != nil {
				return err
			}
			if sourcePreflight != nil {
				opts.SourcePreflight = syncSourcePreflightBundleSummary(*sourcePreflight)
			}

			result, err := reconcile.Materialize(target.BOM, opts)
			if err != nil {
				return err
			}
			preflight, err := syncApplyPreflight(flags.clusterName, result.BundlePath, false, false)
			if err != nil {
				return err
			}
			if preflight.Blocked {
				fmt.Fprintf(cmd.ErrOrStderr(), "warning: rendered bundle would be blocked by sync apply preflight: %s\n", strings.Join(preflight.BlockedReasons, "; "))
			}

			out := syncRenderOutput{
				ClusterName:        flags.clusterName,
				BOMName:            result.AppliedRevision.Spec.BOM.Name,
				Revision:           result.AppliedRevision.Spec.BOM.Revision,
				Channel:            string(result.AppliedRevision.Spec.BOM.Channel),
				LocalRepoRevision:  result.AppliedRevision.Spec.LocalRepoRevision,
				RenderProvenance:   result.Bundle.Spec.RenderProvenance,
				BundlePath:         result.BundlePath,
				DesiredStateDigest: result.DesiredStateDigest,
				AppliedRevision:    state.AppliedRevisionPath(flags.clusterName),
				SourcePreflight:    sourcePreflight,
				Preflight:          newSyncPreflightOutput(flags.clusterName, result.BundlePath, preflight),
			}
			return writeSyncOutput(cmd, out, flags.output, "render result")
		},
	}
	addSyncTargetFlags(cmd, &flags.bomFile, &flags.distributionChannelFile, "path to the BOM file to render")
	cmd.Flags().StringVarP(&flags.clusterName, "cluster", "c", "default", "name of cluster to materialize desired state for")
	cmd.Flags().StringVar(&flags.localRepo, "local-repo", "", "path to a cluster-local repo that provides input bindings during render")
	cmd.Flags().StringVar(&flags.localPatchRevision, "local-patch-revision", "", "optional local patch revision recorded in applied state")
	cmd.Flags().StringSliceVar(&flags.packageSources, "package-source", nil, "override a BOM component package source as component=dir for local development")
	cmd.Flags().BoolVar(&flags.skipSourcePreflight, "skip-source-preflight", false, "skip source readiness preflight before render; intended only for development or debugging")
	addSyncOutputFlag(cmd, &flags.output)
	return cmd
}

type syncRenderOutput struct {
	ClusterName        string                     `json:"clusterName" yaml:"clusterName"`
	BOMName            string                     `json:"bomName,omitempty" yaml:"bomName,omitempty"`
	Revision           string                     `json:"revision,omitempty" yaml:"revision,omitempty"`
	Channel            string                     `json:"channel,omitempty" yaml:"channel,omitempty"`
	LocalRepoRevision  string                     `json:"localRepoRevision,omitempty" yaml:"localRepoRevision,omitempty"`
	RenderProvenance   hydrate.RenderProvenance   `json:"renderProvenance,omitempty" yaml:"renderProvenance,omitempty"`
	BundlePath         string                     `json:"bundlePath,omitempty" yaml:"bundlePath,omitempty"`
	DesiredStateDigest string                     `json:"desiredStateDigest,omitempty" yaml:"desiredStateDigest,omitempty"`
	AppliedRevision    string                     `json:"appliedRevisionPath,omitempty" yaml:"appliedRevisionPath,omitempty"`
	SourcePreflight    *syncSourcePreflightOutput `json:"sourcePreflight,omitempty" yaml:"sourcePreflight,omitempty"`
	Preflight          syncPreflightOutput        `json:"preflight,omitempty" yaml:"preflight,omitempty"`
}

type syncPolicyApprovalScanOutput struct {
	Root string                           `json:"root" yaml:"root"`
	Scan *policyreport.ApprovalScanResult `json:"scan" yaml:"scan"`
}

func newSyncPreflightCmd() *cobra.Command {
	var flags struct {
		clusterName             string
		bomFile                 string
		distributionChannelFile string
		localRepo               string
		bundleDir               string
		kubeconfigPath          string
		hostRoot                string
		packageSources          []string
		allowStaleTopology      bool
		allowStaleRenderInputs  bool
		output                  string
	}

	cmd := &cobra.Command{
		Use:   "preflight",
		Short: "Run source readiness checks or rendered bundle apply gates before sync apply",
		Long: strings.TrimSpace(`
Run source preflight with --file to check the BOM, package sources, local repo
doctor results, and topology contract before render.

Run bundle preflight without --file to check whether an already rendered bundle
would pass sync apply freshness and runtime readiness gates.
`),
		Args:         cobra.NoArgs,
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			if strings.TrimSpace(flags.bomFile) != "" || strings.TrimSpace(flags.distributionChannelFile) != "" {
				if strings.TrimSpace(flags.bundleDir) != "" {
					return errors.New("use either --file/--distribution-channel for source preflight or --bundle-dir for rendered bundle preflight, not both")
				}
				out := runSyncSourcePreflight(syncSourcePreflightOptions{
					ClusterName:             flags.clusterName,
					BOMPath:                 flags.bomFile,
					DistributionChannelPath: flags.distributionChannelFile,
					LocalRepoPath:           flags.localRepo,
					PackageSources:          flags.packageSources,
				})
				if err := writeSyncOutput(cmd, out, flags.output, "source preflight result"); err != nil {
					return err
				}
				if out.Blocked {
					fmt.Fprintf(cmd.ErrOrStderr(), "error: sync source preflight blocked: %s\n", strings.Join(out.BlockedReasons, "; "))
					return fmt.Errorf("sync source preflight blocked: %s", strings.Join(out.BlockedReasons, "; "))
				}
				return nil
			}

			bundlePath := flags.bundleDir
			if bundlePath == "" {
				bundlePath = reconcile.CurrentBundlePath(flags.clusterName)
			}
			preflight, err := syncApplyPreflightWithRuntime(syncApplyPreflightOptions{
				ClusterName:            flags.clusterName,
				BundlePath:             bundlePath,
				KubeconfigPath:         flags.kubeconfigPath,
				HostRoot:               flags.hostRoot,
				AllowStaleTopology:     flags.allowStaleTopology,
				AllowStaleRenderInputs: flags.allowStaleRenderInputs,
			})
			if err != nil {
				return err
			}
			syncWarnApplyPreflight(cmd, preflight)
			out := newSyncPreflightOutput(flags.clusterName, bundlePath, preflight)
			if err := writeSyncPreflightOutput(cmd, out, flags.output); err != nil {
				return err
			}
			if preflight.Blocked {
				fmt.Fprintf(cmd.ErrOrStderr(), "error: sync preflight blocked: %s\n", strings.Join(preflight.BlockedReasons, "; "))
				return fmt.Errorf("sync preflight blocked: %s", strings.Join(preflight.BlockedReasons, "; "))
			}
			return nil
		},
	}
	cmd.Flags().StringVarP(&flags.clusterName, "cluster", "c", "default", "name of cluster to preflight desired state for")
	addSyncTargetFlags(cmd, &flags.bomFile, &flags.distributionChannelFile, "path to the BOM file for source preflight")
	cmd.Flags().StringVar(&flags.localRepo, "local-repo", "", "cluster-local repo root for source preflight")
	cmd.Flags().StringVar(&flags.bundleDir, "bundle-dir", "", "path to a rendered bundle directory; defaults to the cluster current bundle")
	cmd.Flags().StringVar(&flags.kubeconfigPath, "kubeconfig", "/etc/kubernetes/admin.conf", "path to the admin kubeconfig used for rendered bundle runtime preflight")
	cmd.Flags().StringVar(&flags.hostRoot, "host-root", string(os.PathSeparator), "host filesystem root used for rendered bundle runtime preflight")
	cmd.Flags().StringSliceVar(&flags.packageSources, "package-source", nil, "override a BOM component package source as component=dir for source preflight")
	cmd.Flags().BoolVar(&flags.allowStaleTopology, "allow-stale-topology", false, "treat a stale executionTopology snapshot as allowed for this preflight check")
	cmd.Flags().BoolVar(&flags.allowStaleRenderInputs, "allow-stale-render-inputs", false, "treat stale render inputs as allowed for this preflight check")
	addSyncOutputFlag(cmd, &flags.output)
	return cmd
}

type syncPreflightOutput struct {
	ClusterName       string                         `json:"clusterName" yaml:"clusterName"`
	BundlePath        string                         `json:"bundlePath" yaml:"bundlePath"`
	BOMName           string                         `json:"bomName" yaml:"bomName"`
	Revision          string                         `json:"revision" yaml:"revision"`
	Channel           string                         `json:"channel" yaml:"channel"`
	State             syncPreflightState             `json:"state" yaml:"state"`
	Summary           string                         `json:"summary" yaml:"summary"`
	RecommendedAction syncPreflightRecommendedAction `json:"recommendedAction" yaml:"recommendedAction"`
	RefreshCommand    string                         `json:"refreshCommand,omitempty" yaml:"refreshCommand,omitempty"`
	TopologyStatus    syncTopologyStatus             `json:"topologyStatus" yaml:"topologyStatus"`
	RenderInputStatus syncRenderInputStatus          `json:"renderInputStatus" yaml:"renderInputStatus"`
	RuntimeStatus     *syncRuntimePreflightOutput    `json:"runtimeStatus,omitempty" yaml:"runtimeStatus,omitempty"`
	Blocked           bool                           `json:"blocked" yaml:"blocked"`
	BlockedReasons    []string                       `json:"blockedReasons,omitempty" yaml:"blockedReasons,omitempty"`
}

type syncSourcePreflightOptions struct {
	ClusterName             string
	BOMPath                 string
	DistributionChannelPath string
	LocalRepoPath           string
	PackageSources          []string
}

type syncSourcePreflightSummary struct {
	Components       int `json:"components" yaml:"components"`
	RequiredInputs   int `json:"requiredInputs" yaml:"requiredInputs"`
	BoundInputs      int `json:"boundInputs" yaml:"boundInputs"`
	LocalResources   int `json:"localResources" yaml:"localResources"`
	LocalPatches     int `json:"localPatches" yaml:"localPatches"`
	DoctorErrors     int `json:"doctorErrors" yaml:"doctorErrors"`
	DoctorWarnings   int `json:"doctorWarnings" yaml:"doctorWarnings"`
	ValidateErrors   int `json:"validateErrors" yaml:"validateErrors"`
	ValidateWarnings int `json:"validateWarnings" yaml:"validateWarnings"`
}

type syncSourcePreflightOutput struct {
	ClusterName             string                         `json:"clusterName" yaml:"clusterName"`
	BOMPath                 string                         `json:"bomPath" yaml:"bomPath"`
	DistributionChannelPath string                         `json:"distributionChannelPath,omitempty" yaml:"distributionChannelPath,omitempty"`
	LocalRepo               string                         `json:"localRepo,omitempty" yaml:"localRepo,omitempty"`
	State                   syncPreflightState             `json:"state" yaml:"state"`
	Summary                 string                         `json:"summary" yaml:"summary"`
	RecommendedAction       syncPreflightRecommendedAction `json:"recommendedAction" yaml:"recommendedAction"`
	RenderCommand           string                         `json:"renderCommand,omitempty" yaml:"renderCommand,omitempty"`
	Blocked                 bool                           `json:"blocked" yaml:"blocked"`
	BlockedReasons          []string                       `json:"blockedReasons,omitempty" yaml:"blockedReasons,omitempty"`
	Counts                  syncSourcePreflightSummary     `json:"counts" yaml:"counts"`
	LocalRepoDoctor         *syncLocalRepoDoctorOutput     `json:"localRepoDoctor,omitempty" yaml:"localRepoDoctor,omitempty"`
	Validate                syncValidateOutput             `json:"validate" yaml:"validate"`
}

func runSyncSourcePreflight(opts syncSourcePreflightOptions) syncSourcePreflightOutput {
	clusterName := strings.TrimSpace(opts.ClusterName)
	if clusterName == "" {
		clusterName = "default"
	}
	out := syncSourcePreflightOutput{
		ClusterName:             clusterName,
		BOMPath:                 strings.TrimSpace(opts.BOMPath),
		DistributionChannelPath: strings.TrimSpace(opts.DistributionChannelPath),
		LocalRepo:               strings.TrimSpace(opts.LocalRepoPath),
	}

	if strings.TrimSpace(opts.LocalRepoPath) != "" {
		doctor := runSyncLocalRepoDoctor(syncLocalRepoDoctorOptions{
			ClusterName:             clusterName,
			BOMPath:                 opts.BOMPath,
			DistributionChannelPath: opts.DistributionChannelPath,
			LocalRepoPath:           opts.LocalRepoPath,
			PackageSources:          opts.PackageSources,
		})
		out.LocalRepoDoctor = &doctor
		out.LocalRepo = doctor.LocalRepo
		out.Counts.DoctorErrors = doctor.Summary.Errors
		out.Counts.DoctorWarnings = doctor.Summary.Warnings
		if !doctor.Passed {
			out.BlockedReasons = append(out.BlockedReasons, fmt.Sprintf("local repo doctor found %d blocking issue(s)", doctor.Summary.Errors))
		}
	}

	validate := runSyncValidate(syncValidateOptions{
		ClusterName:             clusterName,
		BOMPath:                 opts.BOMPath,
		DistributionChannelPath: opts.DistributionChannelPath,
		LocalRepoPath:           opts.LocalRepoPath,
		PackageSources:          opts.PackageSources,
	})
	out.Validate = validate
	if strings.TrimSpace(validate.BOMPath) != "" {
		out.BOMPath = validate.BOMPath
	}
	if strings.TrimSpace(validate.DistributionChannelPath) != "" {
		out.DistributionChannelPath = validate.DistributionChannelPath
	}
	if out.LocalRepo == "" {
		out.LocalRepo = validate.LocalRepo
	}
	out.Counts.Components = validate.Summary.Components
	out.Counts.RequiredInputs = validate.Summary.RequiredInputs
	out.Counts.BoundInputs = validate.Summary.BoundInputs
	out.Counts.LocalResources = validate.Summary.LocalResources
	out.Counts.LocalPatches = validate.Summary.LocalPatches
	out.Counts.ValidateErrors = validate.Summary.Errors
	out.Counts.ValidateWarnings = validate.Summary.Warnings
	if !validate.Passed {
		out.BlockedReasons = append(out.BlockedReasons, fmt.Sprintf("sync validate found %d blocking issue(s)", validate.Summary.Errors))
	}

	out.Blocked = len(out.BlockedReasons) > 0
	if out.Blocked {
		out.State = syncPreflightStateBlocked
		out.Summary = "source inputs are not ready to render"
		out.RecommendedAction = syncPreflightRecommendedActionInspect
	} else if out.Counts.DoctorWarnings > 0 || out.Counts.ValidateWarnings > 0 {
		out.State = syncPreflightStateWarning
		out.Summary = "source inputs passed blocking checks with warnings"
		out.RecommendedAction = syncPreflightRecommendedActionInspect
		out.RenderCommand = syncSourcePreflightRenderCommand(clusterName, opts)
	} else {
		out.State = syncPreflightStateReady
		out.Summary = "source inputs are ready to render"
		out.RecommendedAction = syncPreflightRecommendedActionRender
		out.RenderCommand = syncSourcePreflightRenderCommand(clusterName, opts)
	}
	return out
}

func syncSourcePreflightRenderCommand(clusterName string, opts syncSourcePreflightOptions) string {
	args := []string{"sealos", "sync", "render", "--cluster", clusterName}
	if strings.TrimSpace(opts.DistributionChannelPath) != "" {
		args = append(args, "--distribution-channel", strings.TrimSpace(opts.DistributionChannelPath))
	} else {
		args = append(args, "--file", strings.TrimSpace(opts.BOMPath))
	}
	if strings.TrimSpace(opts.LocalRepoPath) != "" {
		args = append(args, "--local-repo", strings.TrimSpace(opts.LocalRepoPath))
	}
	for _, source := range opts.PackageSources {
		source = strings.TrimSpace(source)
		if source == "" {
			continue
		}
		args = append(args, "--package-source", source)
	}
	return syncShellCommand(args...)
}

func syncSourcePreflightBundleSummary(out syncSourcePreflightOutput) *hydrate.SourcePreflight {
	summary := &hydrate.SourcePreflight{
		State:             string(out.State),
		Summary:           out.Summary,
		RecommendedAction: string(out.RecommendedAction),
		Blocked:           out.Blocked,
		BlockedReasons:    append([]string(nil), out.BlockedReasons...),
		Counts: hydrate.SourcePreflightCounts{
			Components:       out.Counts.Components,
			RequiredInputs:   out.Counts.RequiredInputs,
			BoundInputs:      out.Counts.BoundInputs,
			LocalResources:   out.Counts.LocalResources,
			LocalPatches:     out.Counts.LocalPatches,
			DoctorErrors:     out.Counts.DoctorErrors,
			DoctorWarnings:   out.Counts.DoctorWarnings,
			ValidateErrors:   out.Counts.ValidateErrors,
			ValidateWarnings: out.Counts.ValidateWarnings,
		},
		Validate: hydrate.SourcePreflightCheck{
			Passed:   out.Validate.Passed,
			Errors:   out.Counts.ValidateErrors,
			Warnings: out.Counts.ValidateWarnings,
		},
	}
	if out.LocalRepoDoctor != nil {
		summary.LocalRepoDoctor = &hydrate.SourcePreflightCheck{
			Passed:   out.LocalRepoDoctor.Passed,
			Errors:   out.LocalRepoDoctor.Summary.Errors,
			Warnings: out.LocalRepoDoctor.Summary.Warnings,
		}
	}
	return summary
}

const syncSourcePreflightMissingWarning = "bundle is missing sourcePreflight metadata; re-run sync render with current sealos before production apply"

func syncSourcePreflightBundleWarnings(bundle *hydrate.Bundle) []string {
	if bundle == nil {
		return nil
	}
	var warnings []string
	if bundle.Spec.SourcePreflight == nil {
		warnings = append(warnings, syncSourcePreflightMissingWarning)
	} else if bundle.Spec.SourcePreflight.Blocked {
		warnings = append(warnings, "bundle sourcePreflight is blocked; inspect source readiness metadata before using this bundle")
	}
	return warnings
}

func newSyncApplyCmd() *cobra.Command {
	var flags struct {
		clusterName            string
		bundleDir              string
		kubeconfigPath         string
		hostRoot               string
		allowStaleTopology     bool
		allowStaleRenderInputs bool
		output                 string
	}

	cmd := &cobra.Command{
		Use:          "apply",
		Short:        "Apply a rendered desired-state bundle to the hosts and cluster targets resolved for the selected cluster",
		Args:         cobra.NoArgs,
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			bundlePath := flags.bundleDir
			if bundlePath == "" {
				bundlePath = reconcile.CurrentBundlePath(flags.clusterName)
			}

			preflight, err := syncApplyPreflightWithRuntime(syncApplyPreflightOptions{
				ClusterName:            flags.clusterName,
				BundlePath:             bundlePath,
				KubeconfigPath:         flags.kubeconfigPath,
				HostRoot:               flags.hostRoot,
				AllowStaleTopology:     flags.allowStaleTopology,
				AllowStaleRenderInputs: flags.allowStaleRenderInputs,
			})
			if err != nil {
				return err
			}
			syncWarnApplyPreflight(cmd, preflight)
			if preflight.Blocked {
				out := newSyncApplyOutput(flags.clusterName, bundlePath, "", preflight)
				if err := writeSyncApplyOutput(cmd, out, flags.output); err != nil {
					return err
				}
				fmt.Fprintf(cmd.ErrOrStderr(), "error: sync apply blocked: %s\n", strings.Join(preflight.BlockedReasons, "; "))
				return fmt.Errorf("sync apply blocked: %s", strings.Join(preflight.BlockedReasons, "; "))
			}

			result, err := runSyncApply(reconcile.ApplyOptions{
				ClusterName:    flags.clusterName,
				BundlePath:     bundlePath,
				KubeconfigPath: flags.kubeconfigPath,
				HostRoot:       flags.hostRoot,
				Stderr:         cmd.ErrOrStderr(),
			})
			if err != nil {
				return err
			}
			if result == nil {
				return fmt.Errorf("sync apply returned nil result")
			}
			appliedBundle := result.Bundle
			if appliedBundle == nil {
				appliedBundle = preflight.Bundle
			}
			outputPreflight := preflight
			outputPreflight.Bundle = appliedBundle
			out := newSyncApplyOutput(flags.clusterName, result.BundlePath, result.DesiredStateDigest, outputPreflight)
			return writeSyncApplyOutput(cmd, out, flags.output)
		},
	}
	cmd.Flags().StringVarP(&flags.clusterName, "cluster", "c", "default", "name of single-node cluster to apply desired state for")
	cmd.Flags().StringVar(&flags.bundleDir, "bundle-dir", "", "path to a rendered bundle directory that matches the cluster rendered state; defaults to the cluster current bundle")
	cmd.Flags().StringVar(&flags.kubeconfigPath, "kubeconfig", "/etc/kubernetes/admin.conf", "path to the admin kubeconfig used for manifest and healthcheck steps")
	cmd.Flags().StringVar(&flags.hostRoot, "host-root", string(os.PathSeparator), "host filesystem root used for rootfs and file projection during apply")
	cmd.Flags().BoolVar(&flags.allowStaleTopology, "allow-stale-topology", false, "allow applying a bundle whose executionTopology snapshot differs from the current Clusterfile topology")
	cmd.Flags().BoolVar(&flags.allowStaleRenderInputs, "allow-stale-render-inputs", false, "allow applying a bundle whose recorded render inputs differ from current local inputs")
	addSyncOutputFlag(cmd, &flags.output)
	return cmd
}

type syncApplyOutput struct {
	ClusterName        string                         `json:"clusterName" yaml:"clusterName"`
	BOMName            string                         `json:"bomName" yaml:"bomName"`
	Revision           string                         `json:"revision" yaml:"revision"`
	Channel            string                         `json:"channel" yaml:"channel"`
	BundlePath         string                         `json:"bundlePath" yaml:"bundlePath"`
	DesiredStateDigest string                         `json:"desiredStateDigest" yaml:"desiredStateDigest"`
	AppliedRevision    string                         `json:"appliedRevisionPath" yaml:"appliedRevisionPath"`
	SourcePreflight    *hydrate.SourcePreflight       `json:"sourcePreflight,omitempty" yaml:"sourcePreflight,omitempty"`
	State              syncPreflightState             `json:"state,omitempty" yaml:"state,omitempty"`
	Summary            string                         `json:"summary,omitempty" yaml:"summary,omitempty"`
	RecommendedAction  syncPreflightRecommendedAction `json:"recommendedAction,omitempty" yaml:"recommendedAction,omitempty"`
	RefreshCommand     string                         `json:"refreshCommand,omitempty" yaml:"refreshCommand,omitempty"`
	TopologyStatus     syncTopologyStatus             `json:"topologyStatus" yaml:"topologyStatus"`
	RenderInputStatus  syncRenderInputStatus          `json:"renderInputStatus" yaml:"renderInputStatus"`
	RuntimeStatus      *syncRuntimePreflightOutput    `json:"runtimeStatus,omitempty" yaml:"runtimeStatus,omitempty"`
	Blocked            bool                           `json:"blocked,omitempty" yaml:"blocked,omitempty"`
	BlockedReasons     []string                       `json:"blockedReasons,omitempty" yaml:"blockedReasons,omitempty"`
	Warnings           []string                       `json:"warnings,omitempty" yaml:"warnings,omitempty"`
}

type syncPreflightState string

const (
	syncPreflightStateReady   syncPreflightState = "Ready"
	syncPreflightStateBlocked syncPreflightState = "Blocked"
	syncPreflightStateWarning syncPreflightState = "Warning"
)

type syncPreflightRecommendedAction string

const (
	syncPreflightRecommendedActionApply             syncPreflightRecommendedAction = "apply"
	syncPreflightRecommendedActionApplyWithOverride syncPreflightRecommendedAction = "applyWithOverride"
	syncPreflightRecommendedActionInspect           syncPreflightRecommendedAction = "inspect"
	syncPreflightRecommendedActionRender            syncPreflightRecommendedAction = "render"
	syncPreflightRecommendedActionRerender          syncPreflightRecommendedAction = "rerender"
)

type syncApplyPreflightResult struct {
	Bundle            *hydrate.Bundle
	TopologyStatus    syncTopologyStatus
	RenderInputStatus syncRenderInputStatus
	RuntimeStatus     *syncRuntimePreflightOutput
	Blocked           bool
	BlockedReasons    []string
}

type syncApplyPreflightOptions struct {
	ClusterName            string
	BundlePath             string
	KubeconfigPath         string
	HostRoot               string
	AllowStaleTopology     bool
	AllowStaleRenderInputs bool
}

func syncApplyPreflight(clusterName, bundlePath string, allowStaleTopology, allowStaleRenderInputs bool) (syncApplyPreflightResult, error) {
	bundle, err := reconcile.LoadBundle(bundlePath)
	if err != nil {
		return syncApplyPreflightResult{}, err
	}
	return syncApplyPreflightForBundle(clusterName, bundle, allowStaleTopology, allowStaleRenderInputs), nil
}

func syncApplyPreflightWithRuntime(opts syncApplyPreflightOptions) (syncApplyPreflightResult, error) {
	bundle, err := reconcile.LoadBundle(opts.BundlePath)
	if err != nil {
		return syncApplyPreflightResult{}, err
	}
	return syncApplyPreflightForBundleWithRuntime(opts.ClusterName, bundle, opts.AllowStaleTopology, opts.AllowStaleRenderInputs, syncRuntimePreflightOptions{
		ClusterName:    opts.ClusterName,
		Bundle:         bundle,
		HostRoot:       opts.HostRoot,
		KubeconfigPath: opts.KubeconfigPath,
	}), nil
}

func syncApplyPreflightForBundle(clusterName string, bundle *hydrate.Bundle, allowStaleTopology, allowStaleRenderInputs bool) syncApplyPreflightResult {
	topologyStatus := syncTopologyStatusForBundle(clusterName, bundle)
	renderInputStatus := syncRenderInputStatusForBundle(bundle)
	blockedReasons := syncApplyBlockedReasons(topologyStatus, renderInputStatus, nil, allowStaleTopology, allowStaleRenderInputs)
	return syncApplyPreflightResult{
		Bundle:            bundle,
		TopologyStatus:    topologyStatus,
		RenderInputStatus: renderInputStatus,
		Blocked:           len(blockedReasons) > 0,
		BlockedReasons:    blockedReasons,
	}
}

func syncApplyPreflightForBundleWithRuntime(clusterName string, bundle *hydrate.Bundle, allowStaleTopology, allowStaleRenderInputs bool, runtimeOpts syncRuntimePreflightOptions) syncApplyPreflightResult {
	topologyStatus := syncTopologyStatusForBundle(clusterName, bundle)
	renderInputStatus := syncRenderInputStatusForBundle(bundle)
	runtimeStatus := runSyncRuntimePreflight(runtimeOpts)
	blockedReasons := syncApplyBlockedReasons(topologyStatus, renderInputStatus, &runtimeStatus, allowStaleTopology, allowStaleRenderInputs)
	return syncApplyPreflightResult{
		Bundle:            bundle,
		TopologyStatus:    topologyStatus,
		RenderInputStatus: renderInputStatus,
		RuntimeStatus:     &runtimeStatus,
		Blocked:           len(blockedReasons) > 0,
		BlockedReasons:    blockedReasons,
	}
}

func newSyncPreflightOutput(clusterName, bundlePath string, preflight syncApplyPreflightResult) syncPreflightOutput {
	state, summary, action := syncPreflightDecision(preflight)
	out := syncPreflightOutput{
		ClusterName:       clusterName,
		BundlePath:        bundlePath,
		State:             state,
		Summary:           summary,
		RecommendedAction: action,
		RefreshCommand:    preflight.TopologyStatus.RefreshCommand,
		TopologyStatus:    preflight.TopologyStatus,
		RenderInputStatus: preflight.RenderInputStatus,
		RuntimeStatus:     preflight.RuntimeStatus,
		Blocked:           preflight.Blocked,
		BlockedReasons:    preflight.BlockedReasons,
	}
	if preflight.Bundle != nil {
		out.BOMName = preflight.Bundle.Spec.BOMName
		out.Revision = preflight.Bundle.Spec.Revision
		out.Channel = string(preflight.Bundle.Spec.Channel)
	}
	return out
}

func syncPreflightDecision(preflight syncApplyPreflightResult) (syncPreflightState, string, syncPreflightRecommendedAction) {
	if preflight.Blocked {
		if preflight.TopologyStatus.State == syncTopologyStateStale || preflight.RenderInputStatus.State == syncRenderInputStateStale {
			return syncPreflightStateBlocked, "bundle is not safe to apply without refreshing or explicitly overriding stale checks", syncPreflightRecommendedActionRerender
		}
		if preflight.RuntimeStatus != nil && preflight.RuntimeStatus.Blocked {
			return syncPreflightStateBlocked, "runtime environment is not ready for apply", syncPreflightRecommendedActionInspect
		}
		return syncPreflightStateBlocked, "bundle is not safe to apply", syncPreflightRecommendedActionInspect
	}
	if preflight.TopologyStatus.State == syncTopologyStateStale || preflight.RenderInputStatus.State == syncRenderInputStateStale {
		return syncPreflightStateWarning, "bundle has stale inputs or topology but this check allowed override flags", syncPreflightRecommendedActionApplyWithOverride
	}
	if preflight.RuntimeStatus != nil && preflight.RuntimeStatus.State == syncPreflightStateWarning {
		return syncPreflightStateWarning, "bundle passed blocking checks with runtime warnings", syncPreflightRecommendedActionInspect
	}
	if preflight.TopologyStatus.State == syncTopologyStateUnknown || preflight.RenderInputStatus.State == syncRenderInputStateUnknown {
		return syncPreflightStateWarning, "bundle freshness could not be fully verified", syncPreflightRecommendedActionInspect
	}
	if preflight.TopologyStatus.State == syncTopologyStateMissing || preflight.RenderInputStatus.State == syncRenderInputStateMissing {
		return syncPreflightStateWarning, "bundle is missing freshness metadata; re-render to initialize tracking before production apply", syncPreflightRecommendedActionInspect
	}
	return syncPreflightStateReady, "bundle passed apply preflight checks", syncPreflightRecommendedActionApply
}

func writeSyncPreflightOutput(cmd *cobra.Command, out syncPreflightOutput, format string) error {
	return writeSyncOutput(cmd, out, format, "preflight result")
}

func newSyncApplyOutput(clusterName, bundlePath, desiredStateDigest string, preflight syncApplyPreflightResult) syncApplyOutput {
	preflightState, summary, action := syncPreflightDecision(preflight)
	out := syncApplyOutput{
		ClusterName:        clusterName,
		BundlePath:         bundlePath,
		DesiredStateDigest: desiredStateDigest,
		AppliedRevision:    state.AppliedRevisionPath(clusterName),
		State:              preflightState,
		Summary:            summary,
		RecommendedAction:  action,
		RefreshCommand:     preflight.TopologyStatus.RefreshCommand,
		TopologyStatus:     preflight.TopologyStatus,
		RenderInputStatus:  preflight.RenderInputStatus,
		RuntimeStatus:      preflight.RuntimeStatus,
		Blocked:            preflight.Blocked,
		BlockedReasons:     preflight.BlockedReasons,
	}
	if preflight.Bundle != nil {
		out.BOMName = preflight.Bundle.Spec.BOMName
		out.Revision = preflight.Bundle.Spec.Revision
		out.Channel = string(preflight.Bundle.Spec.Channel)
		out.SourcePreflight = preflight.Bundle.Spec.SourcePreflight
	}
	out.Warnings = syncSourcePreflightBundleWarnings(preflight.Bundle)
	return out
}

func writeSyncApplyOutput(cmd *cobra.Command, out syncApplyOutput, format string) error {
	return writeSyncOutput(cmd, out, format, "apply result")
}

func syncApplyBlockedReasons(topologyStatus syncTopologyStatus, renderInputStatus syncRenderInputStatus, runtimeStatus *syncRuntimePreflightOutput, allowStaleTopology, allowStaleRenderInputs bool) []string {
	var reasons []string
	if topologyStatus.State == syncTopologyStateStale && !allowStaleTopology {
		reasons = append(reasons, "bundle executionTopology is stale; re-run sync render or pass --allow-stale-topology")
	}
	if renderInputStatus.State == syncRenderInputStateStale && !allowStaleRenderInputs {
		reasons = append(reasons, "render inputs are stale; re-run sync render or pass --allow-stale-render-inputs")
	}
	if runtimeStatus != nil && runtimeStatus.Blocked {
		reasons = append(reasons, runtimeStatus.BlockedReasons...)
	}
	return reasons
}

func syncWarnApplyPreflight(cmd *cobra.Command, preflight syncApplyPreflightResult) {
	topologyStatus := preflight.TopologyStatus
	renderInputStatus := preflight.RenderInputStatus
	if topologyStatus.State == syncTopologyStateStale {
		fmt.Fprintf(cmd.ErrOrStderr(), "warning: %s; refresh with: %s\n", topologyStatus.Message, topologyStatus.RefreshCommand)
	}
	if renderInputStatus.State == syncRenderInputStateStale {
		fmt.Fprintf(cmd.ErrOrStderr(), "warning: %s\n", renderInputStatus.Message)
	}
}

func newSyncDiffCmd() *cobra.Command {
	var flags struct {
		clusterName    string
		bundleDir      string
		kubeconfigPath string
		hostRoot       string
		output         string
	}

	cmd := &cobra.Command{
		Use:          "diff",
		Short:        "Compare tracked Kubernetes objects in a rendered bundle with live cluster state",
		Args:         cobra.NoArgs,
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			bundlePath := flags.bundleDir
			if bundlePath == "" {
				bundlePath = reconcile.CurrentBundlePath(flags.clusterName)
			}

			bundle, err := reconcile.LoadBundle(bundlePath)
			if err != nil {
				return err
			}
			applied, err := state.LoadAppliedRevision(flags.clusterName)
			if err != nil && flags.bundleDir == "" {
				return err
			}

			result, err := syncCompareBundle(flags.clusterName, bundle, bundlePath, flags.kubeconfigPath, flags.hostRoot)
			if err != nil {
				return err
			}
			preflight := syncApplyPreflightForBundle(flags.clusterName, bundle, false, false)
			if err := syncAnnotateRemediationGuidance(result, bundlePath, applied); err != nil {
				return err
			}
			statusSummary := compare.SummarizeStatus(result)
			persisted, updated, err := persistSyncObservedState(flags.clusterName, bundlePath, result.Summary, statusSummary)
			if err != nil {
				return err
			}
			if persisted != nil {
				applied = persisted
			}

			out := syncDiffOutput{
				ClusterName:                 flags.clusterName,
				BOMName:                     bundle.Spec.BOMName,
				Revision:                    bundle.Spec.Revision,
				Channel:                     string(bundle.Spec.Channel),
				BundlePath:                  bundlePath,
				AppliedRevision:             state.AppliedRevisionPath(flags.clusterName),
				LocalPatchPolicy:            syncLocalPatchPolicyFromBundle(bundle),
				SourcePreflight:             bundle.Spec.SourcePreflight,
				Preflight:                   newSyncPreflightOutput(flags.clusterName, bundlePath, preflight),
				TopologyStatus:              preflight.TopologyStatus,
				RenderInputStatus:           preflight.RenderInputStatus,
				CurrentState:                statusSummary.State,
				Headline:                    syncStatusHeadline(statusSummary),
				OperatorActionSummary:       statusSummary.OperatorActionSummary,
				HostPathConflicts:           statusSummary.HostPathConflicts,
				LocalInputHostSplits:        statusSummary.LocalInputHostSplits,
				CurrentCompare:              result,
				PolicyEligibleOrphanObjects: statusSummary.PolicyEligibleOrphanObjects,
				ObservationPersisted:        updated,
				Warnings:                    syncSourcePreflightBundleWarnings(bundle),
			}
			if persisted != nil {
				out.PersistedObservedSummary = persisted.Status.ObservedSummary
			}
			if applied != nil {
				out.RecordedRevision = syncRecordedRevisionFromApplied(applied)
			}

			return writeSyncOutput(cmd, out, flags.output, "diff result")
		},
	}
	cmd.Flags().StringVarP(&flags.clusterName, "cluster", "c", "default", "name of cluster to diff against")
	cmd.Flags().StringVar(&flags.bundleDir, "bundle-dir", "", "path to a rendered bundle directory; defaults to the cluster current bundle")
	cmd.Flags().StringVar(&flags.kubeconfigPath, "kubeconfig", "/etc/kubernetes/admin.conf", "path to the admin kubeconfig used for live object lookup")
	cmd.Flags().StringVar(&flags.hostRoot, "host-root", string(os.PathSeparator), "host filesystem root used for tracked host path lookup")
	addSyncOutputFlag(cmd, &flags.output)
	return cmd
}

func newSyncStatusCmd() *cobra.Command {
	var flags struct {
		clusterName    string
		bundleDir      string
		kubeconfigPath string
		hostRoot       string
		output         string
	}

	cmd := &cobra.Command{
		Use:          "status",
		Short:        "Show rendered revision metadata and summarize live ownership drift for a cluster",
		Args:         cobra.NoArgs,
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			bundlePath := flags.bundleDir
			if bundlePath == "" {
				bundlePath = reconcile.CurrentBundlePath(flags.clusterName)
			}

			bundle, err := reconcile.LoadBundle(bundlePath)
			if err != nil {
				return err
			}
			applied, err := state.LoadAppliedRevision(flags.clusterName)
			if err != nil && flags.bundleDir == "" {
				return err
			}

			result, err := syncCompareBundle(flags.clusterName, bundle, bundlePath, flags.kubeconfigPath, flags.hostRoot)
			if err != nil {
				return err
			}
			preflight := syncApplyPreflightForBundle(flags.clusterName, bundle, false, false)
			if err := syncAnnotateRemediationGuidance(result, bundlePath, applied); err != nil {
				return err
			}
			statusSummary := compare.SummarizeStatus(result)
			if persisted, _, err := persistSyncObservedState(flags.clusterName, bundlePath, result.Summary, statusSummary); err != nil {
				return err
			} else if persisted != nil {
				applied = persisted
			}

			out := syncStatusOutput{
				ClusterName:                 flags.clusterName,
				BOMName:                     bundle.Spec.BOMName,
				Revision:                    bundle.Spec.Revision,
				Channel:                     string(bundle.Spec.Channel),
				BundlePath:                  bundlePath,
				AppliedRevision:             state.AppliedRevisionPath(flags.clusterName),
				LocalPatchPolicy:            syncLocalPatchPolicyFromBundle(bundle),
				SourcePreflight:             bundle.Spec.SourcePreflight,
				Preflight:                   newSyncPreflightOutput(flags.clusterName, bundlePath, preflight),
				TopologyStatus:              preflight.TopologyStatus,
				RenderInputStatus:           preflight.RenderInputStatus,
				CurrentState:                statusSummary.State,
				Headline:                    syncStatusHeadline(statusSummary),
				Summary:                     result.Summary,
				OperatorActionSummary:       statusSummary.OperatorActionSummary,
				MixedOwnershipObjects:       statusSummary.MixedOwnershipObjects,
				DirtyObjects:                statusSummary.DirtyObjects,
				OrphanObjects:               statusSummary.OrphanObjects,
				PolicyEligibleOrphanObjects: statusSummary.PolicyEligibleOrphanObjects,
				HostPathConflicts:           statusSummary.HostPathConflicts,
				LocalInputHostSplits:        statusSummary.LocalInputHostSplits,
				DirtyHostPaths:              statusSummary.DirtyHostPaths,
				OrphanHostPaths:             statusSummary.OrphanHostPaths,
				Warnings:                    syncSourcePreflightBundleWarnings(bundle),
			}
			if applied != nil {
				out.DesiredStateDigest = applied.Spec.DesiredStateDigest
				out.LocalRepoRevision = applied.Spec.LocalRepoRevision
				out.LocalPatchRevision = applied.Spec.LocalPatchRevision
				out.RecordedState = applied.Status.State
				out.RecordedObservedSummary = applied.Status.ObservedSummary
				out.LastAppliedTime = formatAppliedTime(applied)
			}
			return writeSyncOutput(cmd, out, flags.output, "status result")
		},
	}
	cmd.Flags().StringVarP(&flags.clusterName, "cluster", "c", "default", "name of cluster to inspect")
	cmd.Flags().StringVar(&flags.bundleDir, "bundle-dir", "", "path to a rendered bundle directory; defaults to the cluster current bundle")
	cmd.Flags().StringVar(&flags.kubeconfigPath, "kubeconfig", "/etc/kubernetes/admin.conf", "path to the admin kubeconfig used for live object lookup")
	cmd.Flags().StringVar(&flags.hostRoot, "host-root", string(os.PathSeparator), "host filesystem root used for tracked host path lookup")
	addSyncOutputFlag(cmd, &flags.output)
	return cmd
}

func newSyncCommitCmd() *cobra.Command {
	var flags struct {
		clusterName    string
		bundleDir      string
		localRepo      string
		kubeconfigPath string
		hostRoot       string
		host           string
		output         string
	}

	cmd := &cobra.Command{
		Use:          "commit",
		Short:        "Persist supported local-owned drift back into local repo patch files",
		Args:         cobra.NoArgs,
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			if strings.TrimSpace(flags.localRepo) == "" {
				return fmt.Errorf("local repo path cannot be empty")
			}

			bundlePath := flags.bundleDir
			if bundlePath == "" {
				bundlePath = reconcile.CurrentBundlePath(flags.clusterName)
			}

			bundle, err := reconcile.LoadBundle(bundlePath)
			if err != nil {
				return err
			}
			repo, err := localrepo.Load(flags.localRepo)
			if err != nil {
				return err
			}

			resolver := newSyncKubectlResolver(flags.kubeconfigPath)
			current, err := syncCompareBundle(flags.clusterName, bundle, bundlePath, flags.kubeconfigPath, flags.hostRoot)
			if err != nil {
				return err
			}
			commitHostRoot := flags.hostRoot
			cleanupCommitHostRoot := func() {}
			if strings.TrimSpace(flags.host) != "" {
				commitHostRoot, cleanupCommitHostRoot, err = syncPrepareCommitHostRoot(flags.clusterName, bundle, flags.hostRoot, flags.host)
				if err != nil {
					return err
				}
				defer cleanupCommitHostRoot()
			}

			commitResult, err := distcommit.LocalPatches(distcommit.Options{
				Bundle:        bundle,
				BundleRoot:    bundlePath,
				LocalRepo:     repo,
				CompareResult: current,
				Resolver:      resolver,
				HostRoot:      commitHostRoot,
				SelectedHost:  flags.host,
			})
			if err != nil {
				return err
			}

			applied, err := persistSyncCommittedState(flags.clusterName, bundle, commitResult)
			if err != nil {
				return err
			}

			updatedCompare, err := syncCompareBundle(flags.clusterName, bundle, bundlePath, flags.kubeconfigPath, flags.hostRoot)
			if err != nil {
				return err
			}
			statusSummary := compare.SummarizeStatus(updatedCompare)
			persisted, observedUpdated, err := persistSyncObservedState(flags.clusterName, bundlePath, updatedCompare.Summary, statusSummary)
			if err != nil {
				return err
			}
			if persisted != nil {
				applied = persisted
			}

			out := syncCommitOutput{
				ClusterName:          flags.clusterName,
				BOMName:              bundle.Spec.BOMName,
				Revision:             bundle.Spec.Revision,
				Channel:              string(bundle.Spec.Channel),
				BundlePath:           bundlePath,
				AppliedRevision:      state.AppliedRevisionPath(flags.clusterName),
				DesiredStateDigest:   commitResult.DesiredStateDigest,
				LocalRepoRevision:    commitResult.LocalRepoRevision,
				CommittedObjects:     commitResult.CommittedObjects,
				CommittedHostPaths:   commitResult.CommittedHostPaths,
				RequestedHost:        flags.host,
				CurrentState:         statusSummary.State,
				CurrentCompare:       updatedCompare,
				ObservationPersisted: observedUpdated,
			}
			if applied != nil {
				out.RecordedRevision = syncRecordedRevisionFromApplied(applied)
			}

			return writeSyncOutput(cmd, out, flags.output, "commit result")
		},
	}
	cmd.Flags().StringVarP(&flags.clusterName, "cluster", "c", "default", "name of cluster to commit local-owned drift for")
	cmd.Flags().StringVar(&flags.bundleDir, "bundle-dir", "", "path to a rendered bundle directory; defaults to the cluster current bundle")
	cmd.Flags().StringVar(&flags.localRepo, "local-repo", "", "path to the cluster-local repo whose patch files should be updated")
	cmd.Flags().StringVar(&flags.kubeconfigPath, "kubeconfig", "/etc/kubernetes/admin.conf", "path to the admin kubeconfig used for live object lookup")
	cmd.Flags().StringVar(&flags.hostRoot, "host-root", string(os.PathSeparator), "host filesystem root used for tracked host path lookup")
	cmd.Flags().StringVar(&flags.host, "host", "", "optional host identity to commit a multi-node local-input host path from")
	addSyncOutputFlag(cmd, &flags.output)
	return cmd
}

func newSyncRevertCmd() *cobra.Command {
	var flags struct {
		clusterName    string
		bundleDir      string
		kubeconfigPath string
		hostRoot       string
		scope          string
		apiVersion     string
		kind           string
		namespace      string
		name           string
		host           string
		component      string
		hostPath       string
		output         string
	}

	cmd := &cobra.Command{
		Use:          "revert",
		Short:        "Re-apply the current desired-state bundle to revert live drift on a single-node cluster",
		Args:         cobra.NoArgs,
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			scope := syncRevertScope(flags.scope)
			if err := scope.Validate(); err != nil {
				return err
			}
			selector := syncObjectSelector{
				APIVersion: flags.apiVersion,
				Kind:       flags.kind,
				Namespace:  flags.namespace,
				Name:       flags.name,
			}
			if err := selector.Validate(); err != nil {
				return err
			}
			if err := validateSyncRevertSelection(selector, flags.hostPath, flags.component); err != nil {
				return err
			}

			bundlePath := flags.bundleDir
			if bundlePath == "" {
				bundlePath = reconcile.CurrentBundlePath(flags.clusterName)
			}

			bundle, err := reconcile.LoadBundle(bundlePath)
			if err != nil {
				return err
			}
			applied, err := state.LoadAppliedRevision(flags.clusterName)
			if err != nil && flags.bundleDir == "" {
				return err
			}

			beforeCompare, err := syncCompareBundle(flags.clusterName, bundle, bundlePath, flags.kubeconfigPath, flags.hostRoot)
			if err != nil {
				return err
			}
			beforeStatus := compare.SummarizeStatus(beforeCompare)
			revertTargets, err := planSyncRevertTargets(bundle, beforeCompare, scope, selector, flags.host, flags.component, flags.hostPath)
			if err != nil {
				return err
			}

			reverted := false
			if !revertTargets.empty() {
				if err := applySyncRevertTargets(flags.clusterName, bundlePath, flags.kubeconfigPath, flags.hostRoot, revertTargets); err != nil {
					return err
				}
				reverted = true
			}

			currentCompare, err := syncCompareBundle(flags.clusterName, bundle, bundlePath, flags.kubeconfigPath, flags.hostRoot)
			if err != nil {
				return err
			}
			currentStatus := compare.SummarizeStatus(currentCompare)
			persisted, updated, err := persistSyncObservedState(flags.clusterName, bundlePath, currentCompare.Summary, currentStatus)
			if err != nil {
				return err
			}
			if persisted != nil {
				applied = persisted
			}

			out := syncRevertOutput{
				ClusterName:          flags.clusterName,
				BOMName:              bundle.Spec.BOMName,
				Revision:             bundle.Spec.Revision,
				Channel:              string(bundle.Spec.Channel),
				BundlePath:           bundlePath,
				AppliedRevision:      state.AppliedRevisionPath(flags.clusterName),
				RequestedScope:       scope,
				BeforeState:          beforeStatus.State,
				CurrentState:         currentStatus.State,
				Reverted:             reverted,
				RequestedObject:      selector.output(),
				RequestedHost:        flags.host,
				RequestedComponent:   flags.component,
				RequestedHostPath:    flags.hostPath,
				RevertedObjects:      summarizeSyncRevertObjects(revertTargets.Objects),
				RevertedHostPaths:    summarizeSyncRevertHostPaths(revertTargets.HostPaths),
				CurrentCompare:       currentCompare,
				ObservationPersisted: updated,
			}
			if applied != nil {
				out.RecordedRevision = syncRecordedRevisionFromApplied(applied)
			}

			return writeSyncOutput(cmd, out, flags.output, "revert result")
		},
	}
	cmd.Flags().StringVarP(&flags.clusterName, "cluster", "c", "default", "name of cluster to revert live drift for")
	cmd.Flags().StringVar(&flags.bundleDir, "bundle-dir", "", "path to a rendered bundle directory; defaults to the cluster current bundle")
	cmd.Flags().StringVar(&flags.kubeconfigPath, "kubeconfig", "/etc/kubernetes/admin.conf", "path to the admin kubeconfig used for live object lookup")
	cmd.Flags().StringVar(&flags.hostRoot, "host-root", string(os.PathSeparator), "host filesystem root used for tracked host path lookup")
	cmd.Flags().StringVar(&flags.scope, "scope", string(syncRevertScopeAll), "revert scope: all or local")
	cmd.Flags().StringVar(&flags.apiVersion, "api-version", "", "optional apiVersion for exact object-scoped revert")
	cmd.Flags().StringVar(&flags.kind, "kind", "", "optional kind for exact object-scoped revert; must be paired with --name")
	cmd.Flags().StringVar(&flags.namespace, "namespace", "", "optional namespace for exact object-scoped revert")
	cmd.Flags().StringVar(&flags.name, "name", "", "optional object name for exact object-scoped revert; must be paired with --kind")
	cmd.Flags().StringVar(&flags.host, "host", "", "optional execution host for exact host-scoped revert; required only when the same tracked host path drifts on multiple nodes")
	cmd.Flags().StringVar(&flags.component, "component", "", "optional component name for exact host-scoped revert when the same tracked host path drifts from multiple components")
	cmd.Flags().StringVar(&flags.hostPath, "host-path", "", "optional absolute tracked host path for exact host-scoped revert")
	addSyncOutputFlag(cmd, &flags.output)
	return cmd
}

func defaultSyncKubectlOutput(args ...string) ([]byte, error) {
	output, err := exec.Command("kubectl", args...).CombinedOutput() // #nosec G204
	if err != nil {
		message := strings.TrimSpace(string(output))
		if message == "" {
			return nil, fmt.Errorf("run kubectl %s: %w", strings.Join(args, " "), err)
		}
		return nil, fmt.Errorf("%s", message)
	}
	return output, nil
}

func newSyncKubectlResolver(kubeconfigPath string) compare.ObjectResolver {
	return compare.NewKubectlResolver(func(args ...string) ([]byte, error) {
		baseArgs := append([]string{"--kubeconfig", kubeconfigPath}, args...)
		return outputSyncKubectl(baseArgs...)
	})
}

type syncStatusOutput struct {
	ClusterName                 string                         `json:"clusterName" yaml:"clusterName"`
	BOMName                     string                         `json:"bomName" yaml:"bomName"`
	Revision                    string                         `json:"revision" yaml:"revision"`
	Channel                     string                         `json:"channel" yaml:"channel"`
	BundlePath                  string                         `json:"bundlePath" yaml:"bundlePath"`
	AppliedRevision             string                         `json:"appliedRevisionPath" yaml:"appliedRevisionPath"`
	LocalPatchPolicy            *syncLocalPatchPolicyOutput    `json:"localPatchPolicy,omitempty" yaml:"localPatchPolicy,omitempty"`
	SourcePreflight             *hydrate.SourcePreflight       `json:"sourcePreflight,omitempty" yaml:"sourcePreflight,omitempty"`
	Preflight                   syncPreflightOutput            `json:"preflight" yaml:"preflight"`
	TopologyStatus              syncTopologyStatus             `json:"topologyStatus" yaml:"topologyStatus"`
	RenderInputStatus           syncRenderInputStatus          `json:"renderInputStatus" yaml:"renderInputStatus"`
	Headline                    string                         `json:"headline" yaml:"headline"`
	DesiredStateDigest          string                         `json:"desiredStateDigest,omitempty" yaml:"desiredStateDigest,omitempty"`
	LocalRepoRevision           string                         `json:"localRepoRevision,omitempty" yaml:"localRepoRevision,omitempty"`
	LocalPatchRevision          string                         `json:"localPatchRevision,omitempty" yaml:"localPatchRevision,omitempty"`
	RecordedState               state.ClusterState             `json:"recordedState,omitempty" yaml:"recordedState,omitempty"`
	RecordedObservedSummary     *state.ObservedSummary         `json:"recordedObservedSummary,omitempty" yaml:"recordedObservedSummary,omitempty"`
	CurrentState                state.ClusterState             `json:"currentState" yaml:"currentState"`
	LastAppliedTime             string                         `json:"lastAppliedTime,omitempty" yaml:"lastAppliedTime,omitempty"`
	Summary                     compare.Summary                `json:"summary" yaml:"summary"`
	OperatorActionSummary       compare.OperatorActionSummary  `json:"operatorActionSummary" yaml:"operatorActionSummary"`
	MixedOwnershipObjects       []compare.MixedOwnershipObject `json:"mixedOwnershipObjects,omitempty" yaml:"mixedOwnershipObjects,omitempty"`
	DirtyObjects                []compare.ObjectIssue          `json:"dirtyObjects,omitempty" yaml:"dirtyObjects,omitempty"`
	OrphanObjects               []compare.ObjectIssue          `json:"orphanObjects,omitempty" yaml:"orphanObjects,omitempty"`
	PolicyEligibleOrphanObjects []compare.ObjectIssue          `json:"policyEligibleOrphanObjects,omitempty" yaml:"policyEligibleOrphanObjects,omitempty"`
	HostPathConflicts           []compare.HostPathConflict     `json:"hostPathConflicts,omitempty" yaml:"hostPathConflicts,omitempty"`
	LocalInputHostSplits        []compare.LocalInputHostSplit  `json:"localInputHostSplits,omitempty" yaml:"localInputHostSplits,omitempty"`
	DirtyHostPaths              []compare.HostPathIssue        `json:"dirtyHostPaths,omitempty" yaml:"dirtyHostPaths,omitempty"`
	OrphanHostPaths             []compare.HostPathIssue        `json:"orphanHostPaths,omitempty" yaml:"orphanHostPaths,omitempty"`
	Warnings                    []string                       `json:"warnings,omitempty" yaml:"warnings,omitempty"`
}

type syncCommitOutput struct {
	ClusterName          string                         `json:"clusterName" yaml:"clusterName"`
	BOMName              string                         `json:"bomName" yaml:"bomName"`
	Revision             string                         `json:"revision" yaml:"revision"`
	Channel              string                         `json:"channel" yaml:"channel"`
	BundlePath           string                         `json:"bundlePath" yaml:"bundlePath"`
	AppliedRevision      string                         `json:"appliedRevisionPath" yaml:"appliedRevisionPath"`
	DesiredStateDigest   string                         `json:"desiredStateDigest" yaml:"desiredStateDigest"`
	LocalRepoRevision    string                         `json:"localRepoRevision" yaml:"localRepoRevision"`
	CommittedObjects     []distcommit.CommittedObject   `json:"committedObjects,omitempty" yaml:"committedObjects,omitempty"`
	CommittedHostPaths   []distcommit.CommittedHostPath `json:"committedHostPaths,omitempty" yaml:"committedHostPaths,omitempty"`
	RequestedHost        string                         `json:"requestedHost,omitempty" yaml:"requestedHost,omitempty"`
	CurrentState         state.ClusterState             `json:"currentState" yaml:"currentState"`
	CurrentCompare       *compare.Result                `json:"currentCompare" yaml:"currentCompare"`
	ObservationPersisted bool                           `json:"observationPersisted" yaml:"observationPersisted"`
	RecordedRevision     *syncRecordedRevisionOutput    `json:"recordedRevision,omitempty" yaml:"recordedRevision,omitempty"`
}

type syncRevertOutput struct {
	ClusterName          string                      `json:"clusterName" yaml:"clusterName"`
	BOMName              string                      `json:"bomName" yaml:"bomName"`
	Revision             string                      `json:"revision" yaml:"revision"`
	Channel              string                      `json:"channel" yaml:"channel"`
	BundlePath           string                      `json:"bundlePath" yaml:"bundlePath"`
	AppliedRevision      string                      `json:"appliedRevisionPath" yaml:"appliedRevisionPath"`
	RequestedScope       syncRevertScope             `json:"requestedScope" yaml:"requestedScope"`
	RequestedObject      *syncObjectSelectorOutput   `json:"requestedObject,omitempty" yaml:"requestedObject,omitempty"`
	RequestedHost        string                      `json:"requestedHost,omitempty" yaml:"requestedHost,omitempty"`
	RequestedComponent   string                      `json:"requestedComponent,omitempty" yaml:"requestedComponent,omitempty"`
	RequestedHostPath    string                      `json:"requestedHostPath,omitempty" yaml:"requestedHostPath,omitempty"`
	BeforeState          state.ClusterState          `json:"beforeState" yaml:"beforeState"`
	CurrentState         state.ClusterState          `json:"currentState" yaml:"currentState"`
	Reverted             bool                        `json:"reverted" yaml:"reverted"`
	RevertedObjects      []compare.ObjectIssue       `json:"revertedObjects,omitempty" yaml:"revertedObjects,omitempty"`
	RevertedHostPaths    []compare.HostPathIssue     `json:"revertedHostPaths,omitempty" yaml:"revertedHostPaths,omitempty"`
	CurrentCompare       *compare.Result             `json:"currentCompare" yaml:"currentCompare"`
	ObservationPersisted bool                        `json:"observationPersisted" yaml:"observationPersisted"`
	RecordedRevision     *syncRecordedRevisionOutput `json:"recordedRevision,omitempty" yaml:"recordedRevision,omitempty"`
}

type syncRevertScope string

const (
	syncRevertScopeAll   syncRevertScope = "all"
	syncRevertScopeLocal syncRevertScope = "local"
)

type syncObjectSelector struct {
	APIVersion string
	Kind       string
	Namespace  string
	Name       string
}

type syncObjectSelectorOutput struct {
	APIVersion string `json:"apiVersion,omitempty" yaml:"apiVersion,omitempty"`
	Kind       string `json:"kind" yaml:"kind"`
	Namespace  string `json:"namespace,omitempty" yaml:"namespace,omitempty"`
	Name       string `json:"name" yaml:"name"`
}

type syncDiffOutput struct {
	ClusterName                 string                        `json:"clusterName" yaml:"clusterName"`
	BOMName                     string                        `json:"bomName" yaml:"bomName"`
	Revision                    string                        `json:"revision" yaml:"revision"`
	Channel                     string                        `json:"channel" yaml:"channel"`
	BundlePath                  string                        `json:"bundlePath" yaml:"bundlePath"`
	AppliedRevision             string                        `json:"appliedRevisionPath" yaml:"appliedRevisionPath"`
	LocalPatchPolicy            *syncLocalPatchPolicyOutput   `json:"localPatchPolicy,omitempty" yaml:"localPatchPolicy,omitempty"`
	SourcePreflight             *hydrate.SourcePreflight      `json:"sourcePreflight,omitempty" yaml:"sourcePreflight,omitempty"`
	Preflight                   syncPreflightOutput           `json:"preflight" yaml:"preflight"`
	TopologyStatus              syncTopologyStatus            `json:"topologyStatus" yaml:"topologyStatus"`
	RenderInputStatus           syncRenderInputStatus         `json:"renderInputStatus" yaml:"renderInputStatus"`
	CurrentState                state.ClusterState            `json:"currentState" yaml:"currentState"`
	Headline                    string                        `json:"headline" yaml:"headline"`
	OperatorActionSummary       compare.OperatorActionSummary `json:"operatorActionSummary" yaml:"operatorActionSummary"`
	HostPathConflicts           []compare.HostPathConflict    `json:"hostPathConflicts,omitempty" yaml:"hostPathConflicts,omitempty"`
	LocalInputHostSplits        []compare.LocalInputHostSplit `json:"localInputHostSplits,omitempty" yaml:"localInputHostSplits,omitempty"`
	CurrentCompare              *compare.Result               `json:"currentCompare" yaml:"currentCompare"`
	PolicyEligibleOrphanObjects []compare.ObjectIssue         `json:"policyEligibleOrphanObjects,omitempty" yaml:"policyEligibleOrphanObjects,omitempty"`
	ObservationPersisted        bool                          `json:"observationPersisted" yaml:"observationPersisted"`
	PersistedObservedSummary    *state.ObservedSummary        `json:"persistedObservedSummary,omitempty" yaml:"persistedObservedSummary,omitempty"`
	RecordedRevision            *syncRecordedRevisionOutput   `json:"recordedRevision,omitempty" yaml:"recordedRevision,omitempty"`
	Warnings                    []string                      `json:"warnings,omitempty" yaml:"warnings,omitempty"`
}

type syncPolicyReportOutput struct {
	OldPolicyPath string               `json:"oldPolicyPath" yaml:"oldPolicyPath"`
	NewPolicyPath string               `json:"newPolicyPath" yaml:"newPolicyPath"`
	LocalRepo     string               `json:"localRepo,omitempty" yaml:"localRepo,omitempty"`
	Report        *policyreport.Report `json:"report" yaml:"report"`
}

type syncPolicyGateOutput struct {
	OldPolicyPath string                   `json:"oldPolicyPath" yaml:"oldPolicyPath"`
	NewPolicyPath string                   `json:"newPolicyPath" yaml:"newPolicyPath"`
	LocalRepo     string                   `json:"localRepo,omitempty" yaml:"localRepo,omitempty"`
	ApprovalFile  string                   `json:"approvalFile,omitempty" yaml:"approvalFile,omitempty"`
	Gate          *policyreport.GateResult `json:"gate" yaml:"gate"`
	Report        *policyreport.Report     `json:"report" yaml:"report"`
}

type syncRecordedRevisionOutput struct {
	DesiredStateDigest string                 `json:"desiredStateDigest" yaml:"desiredStateDigest"`
	LocalRepoRevision  string                 `json:"localRepoRevision,omitempty" yaml:"localRepoRevision,omitempty"`
	LocalPatchRevision string                 `json:"localPatchRevision,omitempty" yaml:"localPatchRevision,omitempty"`
	State              state.ClusterState     `json:"state" yaml:"state"`
	ObservedSummary    *state.ObservedSummary `json:"observedSummary,omitempty" yaml:"observedSummary,omitempty"`
	LastAppliedTime    string                 `json:"lastAppliedTime,omitempty" yaml:"lastAppliedTime,omitempty"`
}

type syncLocalPatchPolicyOutput struct {
	Source ownership.LocalPatchPolicySource `json:"source" yaml:"source"`
	Scope  ownership.LocalPatchPolicyScope  `json:"scope" yaml:"scope"`
	Name   string                           `json:"name" yaml:"name"`
	Path   string                           `json:"path,omitempty" yaml:"path,omitempty"`
	Digest string                           `json:"digest,omitempty" yaml:"digest,omitempty"`
}

func formatAppliedTime(applied *state.AppliedRevision) string {
	if applied == nil || applied.Status.LastAppliedTime == nil {
		return ""
	}
	return applied.Status.LastAppliedTime.Time.UTC().Format(time.RFC3339)
}

func syncRecordedRevisionFromApplied(applied *state.AppliedRevision) *syncRecordedRevisionOutput {
	if applied == nil {
		return nil
	}
	return &syncRecordedRevisionOutput{
		DesiredStateDigest: applied.Spec.DesiredStateDigest,
		LocalRepoRevision:  applied.Spec.LocalRepoRevision,
		LocalPatchRevision: applied.Spec.LocalPatchRevision,
		State:              applied.Status.State,
		ObservedSummary:    applied.Status.ObservedSummary,
		LastAppliedTime:    formatAppliedTime(applied),
	}
}

func syncLocalPatchPolicyFromBundle(bundle *hydrate.Bundle) *syncLocalPatchPolicyOutput {
	if bundle == nil {
		return nil
	}

	source := bundle.Spec.LocalPatchPolicySource
	if source == "" {
		source = ownership.LocalPatchPolicySourceBuiltInDefault
	}
	scope := bundle.Spec.LocalPatchPolicyScope
	if scope == "" {
		scope = ownership.LocalPatchPolicyScopeClusterLocal
	}
	name := strings.TrimSpace(bundle.Spec.LocalPatchPolicyName)
	if name == "" {
		name = ownership.DefaultLocalPatchPolicyName
	}
	return &syncLocalPatchPolicyOutput{
		Source: source,
		Scope:  scope,
		Name:   name,
		Path:   strings.TrimSpace(bundle.Spec.LocalPatchPolicyPath),
		Digest: strings.TrimSpace(bundle.Spec.LocalPatchPolicyDigest),
	}
}

func persistSyncObservedState(clusterName, bundlePath string, resultSummary compare.Summary, statusSummary compare.StatusSummary) (*state.AppliedRevision, bool, error) {
	if strings.TrimSpace(clusterName) == "" || strings.TrimSpace(bundlePath) == "" {
		return nil, false, nil
	}

	bundleDigest, err := hydrate.DigestBundle(bundlePath)
	if err != nil {
		return nil, false, fmt.Errorf("digest bundle for observed state %q: %w", bundlePath, err)
	}

	return state.PersistObservedState(clusterName, bundleDigest.String(), statusSummary.State, syncObservedSummary(resultSummary, statusSummary), syncObservedStateMessage(statusSummary))
}

func syncObservedSummary(resultSummary compare.Summary, statusSummary compare.StatusSummary) *state.ObservedSummary {
	return &state.ObservedSummary{
		Total:                resultSummary.Total,
		Present:              resultSummary.Present,
		Missing:              resultSummary.Missing,
		Matched:              resultSummary.Matched,
		Drifted:              resultSummary.Drifted,
		Clean:                resultSummary.Clean,
		Dirty:                resultSummary.Dirty,
		Orphan:               resultSummary.Orphan,
		MixedOwnershipObject: len(statusSummary.MixedOwnershipObjects),
		DirectCommitEligible: statusSummary.OperatorActionSummary.DirectCommitEligible,
		DirectRevertEligible: statusSummary.OperatorActionSummary.DirectRevertEligible,
		BundleMatchRequired:  statusSummary.OperatorActionSummary.BundleMatchRequired,
	}
}

func syncObservedStateMessage(statusSummary compare.StatusSummary) string {
	actionSummary := fmt.Sprintf(
		"%d direct commit-eligible, %d direct revert-eligible, %d bundle-match-guarded",
		statusSummary.OperatorActionSummary.DirectCommitEligible,
		statusSummary.OperatorActionSummary.DirectRevertEligible,
		statusSummary.OperatorActionSummary.BundleMatchRequired,
	)
	switch statusSummary.State {
	case state.StateClean:
		return fmt.Sprintf(
			"all tracked desired-state projections match desired ownership state; %s drift item(s)",
			actionSummary,
		)
	case state.StateDirty:
		return fmt.Sprintf(
			"detected %d dirty object(s), %d dirty host path(s), %d orphan object(s), and %d orphan host path(s); %s drift item(s)",
			len(statusSummary.DirtyObjects),
			len(statusSummary.DirtyHostPaths),
			len(statusSummary.OrphanObjects),
			len(statusSummary.OrphanHostPaths),
			actionSummary,
		)
	case state.StateOrphan:
		return fmt.Sprintf(
			"detected %d orphan object(s), %d orphan host path(s), %d dirty object(s), and %d dirty host path(s); %s drift item(s)",
			len(statusSummary.OrphanObjects),
			len(statusSummary.OrphanHostPaths),
			len(statusSummary.DirtyObjects),
			len(statusSummary.DirtyHostPaths),
			actionSummary,
		)
	default:
		return fmt.Sprintf(
			"tracked desired-state ownership state could not be fully determined; %s drift item(s)",
			actionSummary,
		)
	}
}

func syncStatusHeadline(statusSummary compare.StatusSummary) string {
	return fmt.Sprintf(
		"state=%s; dirtyObjects=%d; orphanObjects=%d; dirtyHostPaths=%d; orphanHostPaths=%d; directCommitEligible=%d; directRevertEligible=%d; bundleMatchRequired=%d; policyEligibleOrphanObjects=%d; hostPathConflicts=%d; localInputHostSplits=%d",
		statusSummary.State,
		len(statusSummary.DirtyObjects),
		len(statusSummary.OrphanObjects),
		len(statusSummary.DirtyHostPaths),
		len(statusSummary.OrphanHostPaths),
		statusSummary.OperatorActionSummary.DirectCommitEligible,
		statusSummary.OperatorActionSummary.DirectRevertEligible,
		statusSummary.OperatorActionSummary.BundleMatchRequired,
		len(statusSummary.PolicyEligibleOrphanObjects),
		len(statusSummary.HostPathConflicts),
		len(statusSummary.LocalInputHostSplits),
	)
}

func syncAnnotateRemediationGuidance(result *compare.Result, bundlePath string, applied *state.AppliedRevision) error {
	if result == nil {
		return nil
	}

	hasGuidance := false
	for _, object := range result.Objects {
		if object.Remediation == nil || len(object.Remediation.CommandGuidance) == 0 {
			continue
		}
		hasGuidance = true
		break
	}
	if !hasGuidance {
		for _, hostPath := range result.HostPaths {
			if hostPath.Remediation == nil || len(hostPath.Remediation.CommandGuidance) == 0 {
				continue
			}
			hasGuidance = true
			break
		}
	}
	if !hasGuidance {
		return nil
	}

	bundleDigest := ""
	if strings.TrimSpace(bundlePath) != "" {
		digest, err := hydrate.DigestBundle(bundlePath)
		if err != nil {
			return fmt.Errorf("digest bundle for remediation guidance %q: %w", bundlePath, err)
		}
		bundleDigest = digest.String()
	}

	hasRecordedDesiredState := applied != nil && strings.TrimSpace(applied.Spec.DesiredStateDigest) != ""
	bundleMatchesRecordedDesiredState := hasRecordedDesiredState && bundleDigest == applied.Spec.DesiredStateDigest

	for i := range result.Objects {
		remediation := result.Objects[i].Remediation
		if remediation == nil || len(remediation.CommandGuidance) == 0 {
			continue
		}
		for j := range remediation.CommandGuidance {
			remediation.CommandGuidance[j] = syncEvaluateRemediationCommand(
				remediation.CommandGuidance[j],
				hasRecordedDesiredState,
				bundleMatchesRecordedDesiredState,
			)
		}
	}
	for i := range result.HostPaths {
		remediation := result.HostPaths[i].Remediation
		if remediation == nil || len(remediation.CommandGuidance) == 0 {
			continue
		}
		for j := range remediation.CommandGuidance {
			remediation.CommandGuidance[j] = syncEvaluateRemediationCommand(
				remediation.CommandGuidance[j],
				hasRecordedDesiredState,
				bundleMatchesRecordedDesiredState,
			)
		}
	}
	return nil
}

func syncEvaluateRemediationCommand(guidance compare.HostPathCommandGuidance, hasRecordedDesiredState, bundleMatchesRecordedDesiredState bool) compare.HostPathCommandGuidance {
	if len(guidance.Preconditions) == 0 {
		guidance.Availability = "available"
		guidance.Reason = ""
		return guidance
	}

	blockers := make([]string, 0, len(guidance.Preconditions))
	for _, requirement := range guidance.Preconditions {
		switch requirement {
		case "bundleMatchesRecordedDesiredStateDigest":
			switch {
			case !hasRecordedDesiredState:
				blockers = append(blockers, "no recorded desired state digest is available for this cluster")
			case !bundleMatchesRecordedDesiredState:
				blockers = append(blockers, "the inspected bundle digest does not match the cluster recorded desired state digest")
			}
		}
	}
	if len(blockers) == 0 {
		guidance.Availability = "available"
		guidance.Reason = ""
		return guidance
	}
	guidance.Availability = "blocked"
	guidance.Reason = strings.Join(blockers, "; ")
	return guidance
}

func syncMergeObjectIssues(groups ...[]compare.ObjectIssue) []compare.ObjectIssue {
	total := 0
	for _, group := range groups {
		total += len(group)
	}
	if total == 0 {
		return nil
	}

	merged := make([]compare.ObjectIssue, 0, total)
	for _, group := range groups {
		merged = append(merged, group...)
	}
	return merged
}

func (s syncRevertScope) Validate() error {
	switch s {
	case syncRevertScopeAll, syncRevertScopeLocal:
		return nil
	default:
		return fmt.Errorf("unsupported revert scope %q", s)
	}
}

func (s syncObjectSelector) Validate() error {
	if strings.TrimSpace(s.Kind) == "" && strings.TrimSpace(s.Name) == "" &&
		strings.TrimSpace(s.Namespace) == "" && strings.TrimSpace(s.APIVersion) == "" {
		return nil
	}
	if strings.TrimSpace(s.Kind) == "" || strings.TrimSpace(s.Name) == "" {
		return fmt.Errorf("object-scoped revert requires both --kind and --name")
	}
	return nil
}

func validateSyncRevertSelection(selector syncObjectSelector, hostPath, component string) error {
	if selector.active() && strings.TrimSpace(hostPath) != "" {
		return fmt.Errorf("object-scoped revert and host-scoped revert cannot be combined")
	}
	if selector.active() && strings.TrimSpace(component) != "" {
		return fmt.Errorf("object-scoped revert and component-scoped host revert cannot be combined")
	}
	if strings.TrimSpace(component) != "" && strings.TrimSpace(hostPath) == "" {
		return fmt.Errorf("component-scoped host revert requires --host-path")
	}
	if strings.TrimSpace(hostPath) == "" {
		return nil
	}
	if !filepath.IsAbs(hostPath) {
		return fmt.Errorf("host-scoped revert requires an absolute --host-path")
	}
	return nil
}

func (s syncObjectSelector) active() bool {
	return strings.TrimSpace(s.Kind) != "" || strings.TrimSpace(s.Name) != ""
}

func (s syncObjectSelector) output() *syncObjectSelectorOutput {
	if !s.active() {
		return nil
	}
	return &syncObjectSelectorOutput{
		APIVersion: s.APIVersion,
		Kind:       s.Kind,
		Namespace:  s.Namespace,
		Name:       s.Name,
	}
}

func (s syncObjectSelector) matches(object hydrate.TrackedK8sObject) bool {
	if !s.active() {
		return true
	}
	if strings.TrimSpace(s.APIVersion) != "" && object.APIVersion != s.APIVersion {
		return false
	}
	if object.Kind != s.Kind || object.Name != s.Name {
		return false
	}
	if strings.TrimSpace(s.Namespace) != "" && object.Namespace != s.Namespace {
		return false
	}
	return true
}

type syncRevertTargets struct {
	Objects   []compare.ObjectStatus
	HostPaths []compare.HostPathStatus
}

func (t syncRevertTargets) empty() bool {
	return len(t.Objects) == 0 && len(t.HostPaths) == 0
}

func planSyncRevertTargets(bundle *hydrate.Bundle, result *compare.Result, scope syncRevertScope, selector syncObjectSelector, host, component, hostPath string) (syncRevertTargets, error) {
	if result == nil {
		return syncRevertTargets{}, fmt.Errorf("compare result cannot be nil")
	}

	drifted := make([]compare.ObjectStatus, 0, len(result.Objects))
	for _, object := range result.Objects {
		if object.State == state.StateClean {
			continue
		}
		drifted = append(drifted, object)
	}
	driftedHostPaths := make([]compare.HostPathStatus, 0, len(result.HostPaths))
	for _, path := range result.HostPaths {
		if path.State == state.StateClean {
			continue
		}
		driftedHostPaths = append(driftedHostPaths, path)
	}
	if len(drifted) == 0 && len(driftedHostPaths) == 0 {
		return syncRevertTargets{}, nil
	}

	if strings.TrimSpace(hostPath) != "" {
		targets := make([]compare.HostPathStatus, 0, 1)
		for _, path := range driftedHostPaths {
			if path.Tracked.HostPath != hostPath {
				continue
			}
			if strings.TrimSpace(component) != "" && path.Tracked.Component != component {
				continue
			}
			if strings.TrimSpace(host) != "" && path.Host != host {
				continue
			}
			if !syncCanRevertHostPath(bundle, path) {
				return syncRevertTargets{}, fmt.Errorf("generated host path %s cannot be reverted yet", path.Tracked.HostPath)
			}
			targets = append(targets, path)
		}
		switch len(targets) {
		case 0:
			if strings.TrimSpace(host) != "" && strings.TrimSpace(component) != "" {
				return syncRevertTargets{}, fmt.Errorf("no drifted host path matches %s for component %s on host %s", hostPath, component, host)
			}
			if strings.TrimSpace(host) != "" {
				return syncRevertTargets{}, fmt.Errorf("no drifted host path matches %s on host %s", hostPath, host)
			}
			if strings.TrimSpace(component) != "" {
				return syncRevertTargets{}, fmt.Errorf("no drifted host path matches %s for component %s", hostPath, component)
			}
			return syncRevertTargets{}, fmt.Errorf("no drifted host path matches %s", hostPath)
		case 1:
			if scope == syncRevertScopeLocal && targets[0].State != state.StateDirty {
				return syncRevertTargets{}, fmt.Errorf("local-scoped revert requires a local-owned drift target, got host path %s%s", targets[0].Tracked.HostPath, syncHostPathHostSuffix(targets[0].Host))
			}
			return syncRevertTargets{HostPaths: targets}, nil
		default:
			if strings.TrimSpace(component) != "" {
				return syncRevertTargets{}, fmt.Errorf("host-scoped revert is ambiguous for %s in component %s; specify --host", hostPath, component)
			}
			return syncRevertTargets{}, fmt.Errorf("host-scoped revert is ambiguous for %s; specify --host or --component from [%s]", hostPath, strings.Join(syncHostPathTargetComponents(targets), ", "))
		}
	}

	if selector.active() {
		targets := make([]compare.ObjectStatus, 0, 1)
		for _, object := range drifted {
			if !selector.matches(object.Tracked) {
				continue
			}
			targets = append(targets, object)
		}
		switch len(targets) {
		case 0:
			return syncRevertTargets{}, fmt.Errorf("no drifted object matches %s", syncSelectorIdentity(selector))
		case 1:
			if scope == syncRevertScopeLocal && targets[0].State != state.StateDirty {
				return syncRevertTargets{}, fmt.Errorf("local-scoped revert requires a local-owned drift target, got %s", syncTrackedObjectIdentity(targets[0].Tracked))
			}
			return syncRevertTargets{Objects: targets}, nil
		default:
			return syncRevertTargets{}, fmt.Errorf("object-scoped revert is ambiguous for %s", syncSelectorIdentity(selector))
		}
	}

	if scope == syncRevertScopeLocal {
		targets := syncRevertTargets{
			Objects:   make([]compare.ObjectStatus, 0, len(drifted)),
			HostPaths: make([]compare.HostPathStatus, 0, len(driftedHostPaths)),
		}
		for _, object := range drifted {
			if object.State == state.StateDirty {
				targets.Objects = append(targets.Objects, object)
			}
		}
		for _, path := range driftedHostPaths {
			if path.State == state.StateDirty && syncCanRevertHostPath(bundle, path) {
				targets.HostPaths = append(targets.HostPaths, path)
			}
		}
		if targets.empty() {
			return syncRevertTargets{}, fmt.Errorf("no local-owned drift found to revert")
		}
		return targets, nil
	}

	targets := syncRevertTargets{
		Objects:   drifted,
		HostPaths: make([]compare.HostPathStatus, 0, len(driftedHostPaths)),
	}
	for _, path := range driftedHostPaths {
		if !syncCanRevertHostPath(bundle, path) {
			continue
		}
		targets.HostPaths = append(targets.HostPaths, path)
	}
	return targets, nil
}

func syncCanRevertHostPath(bundle *hydrate.Bundle, target compare.HostPathStatus) bool {
	if target.Tracked.ProjectionClass != hydrate.HostPathProjectionClassGenerated {
		return true
	}
	return syncCanRepairGeneratedControlPlaneHostPath(bundle, target)
}

func syncCanRepairGeneratedControlPlaneHostPath(bundle *hydrate.Bundle, target compare.HostPathStatus) bool {
	if bundle == nil {
		return false
	}
	if !reconcile.IsRepairableGeneratedControlPlaneHostPath(target.Tracked.HostPath) {
		return false
	}
	for _, component := range bundle.Spec.Components {
		if component.PackageName != "kubernetes-rootfs" && component.Name != "kubernetes" {
			continue
		}
		for _, step := range component.Steps {
			if step.Kind != hydrate.StepContent {
				continue
			}
			if step.SourcePath == "files/etc/kubernetes/kubeadm.yaml" {
				return true
			}
		}
	}
	return false
}

func syncHostPathTargetComponents(targets []compare.HostPathStatus) []string {
	seen := make(map[string]struct{}, len(targets))
	components := make([]string, 0, len(targets))
	for _, target := range targets {
		component := strings.TrimSpace(target.Tracked.Component)
		if component == "" {
			continue
		}
		if _, ok := seen[component]; ok {
			continue
		}
		seen[component] = struct{}{}
		components = append(components, component)
	}
	sort.Strings(components)
	return components
}

func summarizeSyncRevertObjects(targets []compare.ObjectStatus) []compare.ObjectIssue {
	if len(targets) == 0 {
		return nil
	}

	issues := make([]compare.ObjectIssue, 0, len(targets))
	for _, object := range targets {
		issue := compare.ObjectIssue{
			APIVersion: object.Tracked.APIVersion,
			Kind:       object.Tracked.Kind,
			Namespace:  object.Tracked.Namespace,
			Name:       object.Tracked.Name,
			Presence:   object.Presence,
			Comparison: object.Comparison,
			State:      object.State,
		}
		paths := make([]string, 0, len(object.Mismatches))
		seen := make(map[string]struct{}, len(object.Mismatches))
		for _, mismatch := range object.Mismatches {
			if mismatch.Path == "" {
				continue
			}
			if _, ok := seen[mismatch.Path]; ok {
				continue
			}
			seen[mismatch.Path] = struct{}{}
			paths = append(paths, mismatch.Path)
		}
		sort.Strings(paths)
		issue.Paths = paths
		issues = append(issues, issue)
	}
	sort.Slice(issues, func(i, j int) bool {
		return syncObjectIdentityLess(issues[i], issues[j])
	})
	return issues
}

func summarizeSyncRevertHostPaths(targets []compare.HostPathStatus) []compare.HostPathIssue {
	if len(targets) == 0 {
		return nil
	}
	issues := make([]compare.HostPathIssue, 0, len(targets))
	for _, target := range targets {
		issues = append(issues, compare.HostPathIssue{
			Host:       target.Host,
			Path:       target.Tracked.HostPath,
			Component:  target.Tracked.Component,
			Presence:   target.Presence,
			Comparison: target.Comparison,
			State:      target.State,
			Reasons:    summarizeSyncRevertHostPathReasons(target.Presence, target.Mismatches),
		})
	}
	sort.Slice(issues, func(i, j int) bool {
		if issues[i].Host != issues[j].Host {
			return issues[i].Host < issues[j].Host
		}
		return issues[i].Path < issues[j].Path
	})
	return issues
}

func summarizeSyncRevertHostPathReasons(presence compare.ObjectPresence, mismatches []compare.HostPathMismatch) []string {
	if len(mismatches) == 0 {
		if presence == compare.ObjectPresenceMissing {
			return []string{"missing"}
		}
		return nil
	}
	reasons := make([]string, 0, len(mismatches))
	seen := make(map[string]struct{}, len(mismatches))
	for _, mismatch := range mismatches {
		if mismatch.Reason == "" {
			continue
		}
		if _, ok := seen[mismatch.Reason]; ok {
			continue
		}
		seen[mismatch.Reason] = struct{}{}
		reasons = append(reasons, mismatch.Reason)
	}
	sort.Strings(reasons)
	if len(reasons) == 0 && presence == compare.ObjectPresenceMissing {
		return []string{"missing"}
	}
	return reasons
}

func syncTrackedObjectIdentity(object hydrate.TrackedK8sObject) string {
	return syncObjectIdentity(object.APIVersion, object.Kind, object.Namespace, object.Name)
}

func syncSelectorIdentity(selector syncObjectSelector) string {
	return syncObjectIdentity(selector.APIVersion, selector.Kind, selector.Namespace, selector.Name)
}

func syncObjectIdentity(apiVersion, kind, namespace, name string) string {
	if strings.TrimSpace(apiVersion) == "" {
		apiVersion = "<any>"
	}
	if strings.TrimSpace(namespace) == "" {
		return fmt.Sprintf("%s %s/%s", apiVersion, kind, name)
	}
	return fmt.Sprintf("%s %s/%s in namespace %s", apiVersion, kind, name, namespace)
}

func syncObjectIdentityLess(left, right compare.ObjectIssue) bool {
	if left.Kind != right.Kind {
		return left.Kind < right.Kind
	}
	if left.Namespace != right.Namespace {
		return left.Namespace < right.Namespace
	}
	return left.Name < right.Name
}

func applySyncRevertTargets(clusterName, bundlePath, kubeconfigPath, hostRoot string, targets syncRevertTargets) error {
	if targets.empty() {
		return nil
	}

	var remoteExec syncRemoteExecutor
	if syncRevertTargetsNeedRemoteHostAccess(targets.HostPaths) {
		topology, err := loadSyncExecutionTopology(clusterName)
		if err != nil {
			return err
		}
		remoteExec, err = newSyncRemoteExecutor(topology)
		if err != nil {
			return err
		}
	}

	for _, path := range targets.HostPaths {
		if err := applySyncRevertHostPath(clusterName, bundlePath, kubeconfigPath, hostRoot, remoteExec, path); err != nil {
			return err
		}
	}

	if len(targets.Objects) == 0 {
		return nil
	}

	stageDir, err := os.MkdirTemp("", "sealos-sync-revert-")
	if err != nil {
		return fmt.Errorf("create revert staging dir: %w", err)
	}
	defer func() {
		_ = os.RemoveAll(stageDir)
	}()

	for i, object := range targets.Objects {
		desired, err := compare.DesiredObjectYAML(bundlePath, object)
		if err != nil {
			return fmt.Errorf("render desired object %s: %w", syncTrackedObjectIdentity(object.Tracked), err)
		}
		filename := fmt.Sprintf("%03d-%s-%s.yaml", i, sanitizeSyncFilename(object.Tracked.Kind), sanitizeSyncFilename(object.Tracked.Name))
		path := filepath.Join(stageDir, filename)
		if err := os.WriteFile(path, desired, 0o644); err != nil {
			return fmt.Errorf("write desired object %s: %w", syncTrackedObjectIdentity(object.Tracked), err)
		}
		if _, err := outputSyncKubectl("--kubeconfig", kubeconfigPath, "apply", "-f", path); err != nil {
			return fmt.Errorf("apply desired object %s: %w", syncTrackedObjectIdentity(object.Tracked), err)
		}
	}
	return nil
}

func applySyncRevertHostPath(clusterName, bundlePath, kubeconfigPath, hostRoot string, remoteExec syncRemoteExecutor, target compare.HostPathStatus) error {
	if target.Tracked.ProjectionClass == hydrate.HostPathProjectionClassGenerated {
		host := target.Host
		if strings.TrimSpace(host) == "" {
			host = syncLocalExecutionHost
		}
		return repairSyncGeneratedControlPlaneHost(reconcile.RepairGeneratedControlPlaneHostOptions{
			ClusterName:    clusterName,
			BundlePath:     bundlePath,
			KubeconfigPath: kubeconfigPath,
			HostRoot:       hostRoot,
			Host:           host,
		})
	}
	if strings.TrimSpace(target.Host) != "" && !syncIsLocalExecutionHost(target.Host) {
		return applySyncRevertRemoteHostPath(bundlePath, remoteExec, target)
	}

	desiredBundlePath := syncHostPathDesiredBundlePath(target)
	src, err := resolveSyncBundlePath(bundlePath, desiredBundlePath)
	if err != nil {
		return fmt.Errorf("resolve desired host path %s: %w", target.Tracked.HostPath, err)
	}
	dst, err := resolveSyncHostPath(hostRoot, target.Tracked.HostPath)
	if err != nil {
		return fmt.Errorf("resolve live host path %s: %w", target.Tracked.HostPath, err)
	}

	switch target.Tracked.Type {
	case hydrate.HostPathRegularFile:
		return copySyncRegularFile(src, dst, target.Tracked.HostPath)
	case hydrate.HostPathSymlink:
		return copySyncSymlink(src, dst, target.Tracked.HostPath)
	default:
		return fmt.Errorf("unsupported host path type %q for %s", target.Tracked.Type, target.Tracked.HostPath)
	}
}

func syncRevertTargetsNeedRemoteHostAccess(targets []compare.HostPathStatus) bool {
	for _, target := range targets {
		if strings.TrimSpace(target.Host) != "" && !syncIsLocalExecutionHost(target.Host) {
			return true
		}
	}
	return false
}

func copySyncRegularFile(src, dst, trackedPath string) error {
	info, err := os.Lstat(src)
	if err != nil {
		return fmt.Errorf("stat desired host path %q: %w", trackedPath, err)
	}
	if !info.Mode().IsRegular() {
		return fmt.Errorf("desired host path %q is not a regular file", trackedPath)
	}
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return fmt.Errorf("create host path parent for %q: %w", trackedPath, err)
	}
	if err := prepareSyncRegularDestination(dst, trackedPath); err != nil {
		return err
	}
	data, err := os.ReadFile(src)
	if err != nil {
		return fmt.Errorf("read desired host path %q: %w", trackedPath, err)
	}
	if err := os.WriteFile(dst, data, info.Mode().Perm()); err != nil {
		return fmt.Errorf("write reverted host path %q: %w", trackedPath, err)
	}
	if err := os.Chmod(dst, info.Mode().Perm()); err != nil {
		return fmt.Errorf("chmod reverted host path %q: %w", trackedPath, err)
	}
	return nil
}

func copySyncSymlink(src, dst, trackedPath string) error {
	info, err := os.Lstat(src)
	if err != nil {
		return fmt.Errorf("stat desired host path %q: %w", trackedPath, err)
	}
	if info.Mode()&os.ModeSymlink == 0 {
		return fmt.Errorf("desired host path %q is not a symlink", trackedPath)
	}
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return fmt.Errorf("create host path parent for %q: %w", trackedPath, err)
	}
	if err := removeSyncExistingPath(dst, trackedPath); err != nil {
		return err
	}
	target, err := os.Readlink(src)
	if err != nil {
		return fmt.Errorf("read desired symlink for host path %q: %w", trackedPath, err)
	}
	if err := os.Symlink(target, dst); err != nil {
		return fmt.Errorf("write reverted symlink %q: %w", trackedPath, err)
	}
	return nil
}

func applySyncRevertRemoteHostPath(bundlePath string, remoteExec syncRemoteExecutor, target compare.HostPathStatus) error {
	if remoteExec == nil {
		return fmt.Errorf("remote execution client is not configured for host %q", target.Host)
	}

	desiredBundlePath := syncHostPathDesiredBundlePath(target)
	src, err := resolveSyncBundlePath(bundlePath, desiredBundlePath)
	if err != nil {
		return fmt.Errorf("resolve desired host path %s%s: %w", target.Tracked.HostPath, syncHostPathHostSuffix(target.Host), err)
	}

	switch target.Tracked.Type {
	case hydrate.HostPathRegularFile:
		if err := prepareSyncRemoteRegularDestination(remoteExec, target.Host, target.Tracked.HostPath); err != nil {
			return err
		}
		if err := remoteExec.Copy(target.Host, src, target.Tracked.HostPath); err != nil {
			return fmt.Errorf("copy reverted host path %s%s: %w", target.Tracked.HostPath, syncHostPathHostSuffix(target.Host), err)
		}
		return nil
	case hydrate.HostPathSymlink:
		return copySyncRemoteSymlink(remoteExec, src, target)
	default:
		return fmt.Errorf("unsupported host path type %q for %s%s", target.Tracked.Type, target.Tracked.HostPath, syncHostPathHostSuffix(target.Host))
	}
}

func prepareSyncRemoteRegularDestination(remoteExec syncRemoteExecutor, host, trackedPath string) error {
	command := strings.Join([]string{
		"dst=" + syncShellQuote(trackedPath),
		`if [ -d "$dst" ]; then`,
		`  echo directory`,
		`  exit 0`,
		`elif [ -L "$dst" ]; then`,
		`  rm -f "$dst"`,
		`elif [ -e "$dst" ]; then`,
		`  :`,
		`fi`,
		`printf ok`,
	}, "\n")
	output, err := remoteExec.CmdToString(host, "/bin/bash -lc "+syncShellQuote(command), "\n")
	if err != nil {
		return fmt.Errorf("prepare remote host path %s%s: %w", trackedPath, syncHostPathHostSuffix(host), err)
	}
	if strings.Contains(output, "directory") {
		return fmt.Errorf("cannot revert host path %q onto existing directory on host %s", trackedPath, host)
	}
	return nil
}

func copySyncRemoteSymlink(remoteExec syncRemoteExecutor, src string, target compare.HostPathStatus) error {
	info, err := os.Lstat(src)
	if err != nil {
		return fmt.Errorf("stat desired host path %q%s: %w", target.Tracked.HostPath, syncHostPathHostSuffix(target.Host), err)
	}
	if info.Mode()&os.ModeSymlink == 0 {
		return fmt.Errorf("desired host path %q is not a symlink", target.Tracked.HostPath)
	}
	linkTarget, err := os.Readlink(src)
	if err != nil {
		return fmt.Errorf("read desired symlink for host path %q%s: %w", target.Tracked.HostPath, syncHostPathHostSuffix(target.Host), err)
	}

	command := strings.Join([]string{
		"dst=" + syncShellQuote(target.Tracked.HostPath),
		"parent=$(dirname \"$dst\")",
		`mkdir -p "$parent"`,
		`if [ -d "$dst" ]; then`,
		`  echo directory`,
		`  exit 0`,
		`fi`,
		`rm -f "$dst"`,
		`ln -s ` + syncShellQuote(linkTarget) + ` "$dst"`,
		`printf ok`,
	}, "\n")
	output, err := remoteExec.CmdToString(target.Host, "/bin/bash -lc "+syncShellQuote(command), "\n")
	if err != nil {
		return fmt.Errorf("write reverted symlink %q%s: %w", target.Tracked.HostPath, syncHostPathHostSuffix(target.Host), err)
	}
	if strings.Contains(output, "directory") {
		return fmt.Errorf("cannot revert host path %q onto existing directory on host %s", target.Tracked.HostPath, target.Host)
	}
	return nil
}

func prepareSyncRegularDestination(dst, trackedPath string) error {
	info, err := os.Lstat(dst)
	if err == nil {
		if info.IsDir() {
			return fmt.Errorf("cannot revert host path %q onto existing directory %q", trackedPath, dst)
		}
		if info.Mode()&os.ModeSymlink != 0 {
			if err := os.Remove(dst); err != nil {
				return fmt.Errorf("remove existing symlink for host path %q: %w", trackedPath, err)
			}
		}
		return nil
	}
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	return fmt.Errorf("stat host path %q: %w", trackedPath, err)
}

func removeSyncExistingPath(dst, trackedPath string) error {
	info, err := os.Lstat(dst)
	if err == nil {
		if info.IsDir() {
			return fmt.Errorf("cannot revert host path %q onto existing directory %q", trackedPath, dst)
		}
		if err := os.Remove(dst); err != nil {
			return fmt.Errorf("remove existing host path %q: %w", trackedPath, err)
		}
		return nil
	}
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	return fmt.Errorf("stat host path %q: %w", trackedPath, err)
}

func resolveSyncBundlePath(bundleRoot, bundleRel string) (string, error) {
	if strings.TrimSpace(bundleRel) == "" {
		return "", fmt.Errorf("bundle path cannot be empty")
	}
	if filepath.IsAbs(bundleRel) {
		return "", fmt.Errorf("bundle path %q must be relative", bundleRel)
	}
	resolved := filepath.Join(bundleRoot, filepath.FromSlash(bundleRel))
	relative, err := filepath.Rel(bundleRoot, resolved)
	if err != nil {
		return "", err
	}
	if relative == ".." || strings.HasPrefix(relative, ".."+string(os.PathSeparator)) {
		return "", fmt.Errorf("bundle path %q escapes bundle root", bundleRel)
	}
	return resolved, nil
}

func syncHostPathDesiredBundlePath(target compare.HostPathStatus) string {
	if path := syncHostInputBundlePathForHost(target.Tracked.HostInputBindings, target.Host); path != "" {
		return path
	}
	return target.Tracked.BundlePath
}

func syncHostInputBundlePathForHost(bindings map[string]string, host string) string {
	trimmed := strings.TrimSpace(host)
	if trimmed == "" || len(bindings) == 0 {
		return ""
	}
	if path := strings.TrimSpace(bindings[trimmed]); path != "" {
		return path
	}
	hostWithoutPort := strings.TrimSpace(strings.Split(trimmed, ":")[0])
	if hostWithoutPort == "" || hostWithoutPort == trimmed {
		return ""
	}
	return strings.TrimSpace(bindings[hostWithoutPort])
}

func resolveSyncHostPath(hostRoot, trackedHostPath string) (string, error) {
	if strings.TrimSpace(trackedHostPath) == "" {
		return "", fmt.Errorf("tracked host path cannot be empty")
	}
	if !filepath.IsAbs(trackedHostPath) {
		return "", fmt.Errorf("tracked host path %q must be absolute", trackedHostPath)
	}
	if strings.TrimSpace(hostRoot) == "" {
		hostRoot = string(os.PathSeparator)
	}
	cleanRoot := filepath.Clean(hostRoot)
	relative := strings.TrimPrefix(filepath.Clean(trackedHostPath), string(os.PathSeparator))
	resolved := filepath.Join(cleanRoot, relative)
	rel, err := filepath.Rel(cleanRoot, resolved)
	if err != nil {
		return "", err
	}
	if rel == ".." || strings.HasPrefix(rel, ".."+string(os.PathSeparator)) {
		return "", fmt.Errorf("tracked host path %q escapes host root %q", trackedHostPath, hostRoot)
	}
	return resolved, nil
}

func syncHostPathHostSuffix(host string) string {
	host = strings.TrimSpace(host)
	if host == "" {
		return ""
	}
	return " on host " + host
}

func sanitizeSyncFilename(value string) string {
	value = strings.ToLower(strings.TrimSpace(value))
	if value == "" {
		return "object"
	}
	replacer := strings.NewReplacer("/", "-", "\\", "-", " ", "-", "_", "-")
	value = replacer.Replace(value)
	return value
}

func persistSyncCommittedState(clusterName string, bundle *hydrate.Bundle, result *distcommit.Result) (*state.AppliedRevision, error) {
	if strings.TrimSpace(clusterName) == "" || bundle == nil || result == nil {
		return nil, nil
	}

	ref := state.BOMReference{
		Name:     bundle.Spec.BOMName,
		Revision: bundle.Spec.Revision,
		Channel:  bundle.Spec.Channel,
	}

	localPatchRevision := ""
	existing, err := state.LoadAppliedRevision(clusterName)
	switch {
	case err == nil:
		localPatchRevision = existing.Spec.LocalPatchRevision
		if existing.Spec.BOM.Digest != "" {
			ref.Digest = existing.Spec.BOM.Digest
		}
	case errors.Is(err, os.ErrNotExist):
		// allow commit to seed rendered state from bundle metadata when no applied revision exists yet
	default:
		return nil, err
	}

	return state.PersistRenderedState(clusterName, ref, result.DesiredStateDigest, result.LocalRepoRevision, localPatchRevision)
}

func newSyncMaterializeOptions(target *syncResolvedTarget, clusterName, localRepoPath, localPatchRevision string, packageSources []string) (reconcile.Options, error) {
	if target == nil || target.BOM == nil {
		return reconcile.Options{}, fmt.Errorf("sync target cannot be nil")
	}
	doc := target.BOM
	localRootsByComponent, localPackagesByArtifact, err := resolveSyncPackageSources(doc, packageSources)
	if err != nil {
		return reconcile.Options{}, err
	}

	var repo *localrepo.Repo
	localRepoAbsPath := ""
	if localRepoPath != "" {
		localRepoAbsPath, err = filepath.Abs(localRepoPath)
		if err != nil {
			return reconcile.Options{}, fmt.Errorf("resolve local repo path %q: %w", localRepoPath, err)
		}
		repo, err = localrepo.Load(localRepoAbsPath)
		if err != nil {
			return reconcile.Options{}, err
		}
	}

	var fallbackLoader packageformat.Loader
	var fallbackSources hydrate.SourceProvider
	if len(localRootsByComponent) < len(doc.Spec.Components) {
		fallbackLoader, fallbackSources, err = newSyncCachedArtifactResolver(clusterName)
		if err != nil {
			return reconcile.Options{}, err
		}
	}

	provenance, err := syncRenderProvenance(target, localRepoAbsPath, repo, localPatchRevision, localRootsByComponent)
	if err != nil {
		return reconcile.Options{}, err
	}

	return reconcile.Options{
		ClusterName:        clusterName,
		RenderProvenance:   provenance,
		LocalRepo:          repo,
		LocalPatchRevision: localPatchRevision,
		PackageLoader: syncPackageLoader{
			local:    localPackagesByArtifact,
			fallback: fallbackLoader,
		},
		Sources: &syncSourceProvider{
			local:    localRootsByComponent,
			fallback: fallbackSources,
		},
	}, nil
}

func syncRenderProvenance(target *syncResolvedTarget, localRepoPath string, repo *localrepo.Repo, localPatchRevision string, packageSources map[string]string) (hydrate.RenderProvenance, error) {
	provenance := hydrate.RenderProvenance{
		LocalRepoPath:      strings.TrimSpace(localRepoPath),
		LocalPatchRevision: strings.TrimSpace(localPatchRevision),
	}
	if target != nil && target.DistributionChannel != nil {
		provenance.DistributionLine = strings.TrimSpace(target.DistributionChannel.Spec.Line)
	}
	if target != nil && strings.TrimSpace(target.DistributionChannelPath) != "" {
		absChannelPath, err := filepath.Abs(target.DistributionChannelPath)
		if err != nil {
			return hydrate.RenderProvenance{}, fmt.Errorf("resolve DistributionChannel path %q: %w", target.DistributionChannelPath, err)
		}
		provenance.DistributionChannelPath = absChannelPath
		data, err := os.ReadFile(absChannelPath)
		if err != nil {
			return hydrate.RenderProvenance{}, fmt.Errorf("read DistributionChannel path %q: %w", absChannelPath, err)
		}
		provenance.DistributionChannelDigest = digest.Canonical.FromBytes(data).String()
	}
	if target != nil && strings.TrimSpace(target.BOMPath) != "" {
		absBOMPath, err := filepath.Abs(target.BOMPath)
		if err != nil {
			return hydrate.RenderProvenance{}, fmt.Errorf("resolve BOM path %q: %w", target.BOMPath, err)
		}
		provenance.BOMPath = absBOMPath
		data, err := os.ReadFile(absBOMPath)
		if err != nil {
			return hydrate.RenderProvenance{}, fmt.Errorf("read BOM path %q: %w", absBOMPath, err)
		}
		provenance.BOMDigest = digest.Canonical.FromBytes(data).String()
	}
	if repo != nil {
		provenance.LocalRepoRevision = repo.Revision
	}
	if len(packageSources) > 0 {
		names := make([]string, 0, len(packageSources))
		for name := range packageSources {
			names = append(names, name)
		}
		sort.Strings(names)
		provenance.PackageSources = make([]hydrate.RenderProvenancePackageSource, 0, len(names))
		for _, name := range names {
			sourceDigest, err := hydrate.DigestDirectory(packageSources[name])
			if err != nil {
				return hydrate.RenderProvenance{}, fmt.Errorf("digest package source %q: %w", packageSources[name], err)
			}
			provenance.PackageSources = append(provenance.PackageSources, hydrate.RenderProvenancePackageSource{
				Component: name,
				Path:      packageSources[name],
				Digest:    sourceDigest.String(),
			})
		}
	}
	return provenance, nil
}

func defaultSyncMountedArtifactResolver() (packageformat.Loader, hydrate.SourceProvider, error) {
	mounter, err := newSyncPackageImageMounter("sync")
	if err != nil {
		return nil, nil, err
	}
	return packageformat.MountedImageLoader{Mounter: mounter}, hydrate.NewMountedArtifactSourceProvider(mounter), nil
}

type syncCachedArtifactResolver struct {
	cache *ocipackage.Cache
}

func defaultSyncCachedArtifactResolver(clusterName string) (packageformat.Loader, hydrate.SourceProvider, error) {
	clusterName = strings.TrimSpace(clusterName)
	if clusterName == "" {
		clusterName = "default"
	}
	mounter, err := newSyncPackageImageMounter("sync-package-cache")
	if err != nil {
		return nil, nil, err
	}
	resolver := &syncCachedArtifactResolver{
		cache: &ocipackage.Cache{
			Root:    filepath.Join(reconcile.DistributionRootPath(clusterName), "package-cache"),
			Mounter: mounter,
		},
	}
	return resolver, resolver, nil
}

func (r *syncCachedArtifactResolver) Load(image string) (*packageformat.ComponentPackage, error) {
	if r == nil || r.cache == nil {
		return nil, fmt.Errorf("cached artifact resolver cannot be nil")
	}
	return r.cache.Load(image)
}

func (r *syncCachedArtifactResolver) Source(component hydrate.ComponentPlan) (hydrate.Source, error) {
	if r == nil || r.cache == nil {
		return hydrate.Source{}, fmt.Errorf("cached artifact resolver cannot be nil")
	}
	if strings.TrimSpace(component.Artifact) == "" {
		return hydrate.Source{}, fmt.Errorf("artifact reference for component %q cannot be empty", component.Name)
	}
	root, _, err := r.cache.Ensure(component.Artifact)
	if err != nil {
		return hydrate.Source{}, fmt.Errorf("cache component %q artifact %q: %w", component.Name, component.Artifact, err)
	}
	return hydrate.Source{Root: root}, nil
}

func resolveSyncPackageSources(doc *bom.BOM, values []string) (map[string]string, map[string]*packageformat.ComponentPackage, error) {
	localRootsByComponent := make(map[string]string, len(values))
	localPackagesByArtifact := make(map[string]*packageformat.ComponentPackage, len(values))
	if len(values) == 0 {
		return localRootsByComponent, localPackagesByArtifact, nil
	}

	if doc == nil {
		return nil, nil, fmt.Errorf("bom cannot be nil")
	}

	componentIndex := make(map[string]bom.Component, len(doc.Spec.Components))
	for _, component := range doc.Spec.Components {
		componentIndex[component.Name] = component
	}

	for _, value := range values {
		componentName, root, ok := strings.Cut(value, "=")
		if !ok {
			return nil, nil, fmt.Errorf("invalid package source %q, want component=dir", value)
		}
		componentName = strings.TrimSpace(componentName)
		root = strings.TrimSpace(root)
		if componentName == "" || root == "" {
			return nil, nil, fmt.Errorf("invalid package source %q, want component=dir", value)
		}
		component, ok := componentIndex[componentName]
		if !ok {
			return nil, nil, fmt.Errorf("component %q not found in BOM", componentName)
		}
		if _, ok := localRootsByComponent[componentName]; ok {
			return nil, nil, fmt.Errorf("duplicate package source for component %q", componentName)
		}

		absRoot, err := filepath.Abs(root)
		if err != nil {
			return nil, nil, fmt.Errorf("resolve package source %q: %w", value, err)
		}
		pkg, err := packageformat.LoadDir(absRoot)
		if err != nil {
			return nil, nil, err
		}

		localRootsByComponent[componentName] = absRoot
		localPackagesByArtifact[component.Artifact.Reference()] = pkg
	}

	return localRootsByComponent, localPackagesByArtifact, nil
}

type syncPackageLoader struct {
	local    map[string]*packageformat.ComponentPackage
	fallback packageformat.Loader
}

func (l syncPackageLoader) Load(image string) (*packageformat.ComponentPackage, error) {
	if pkg, ok := l.local[image]; ok {
		return pkg, nil
	}
	if l.fallback == nil {
		return nil, fmt.Errorf("package source for artifact %q not found", image)
	}
	return l.fallback.Load(image)
}

type syncSourceProvider struct {
	local    map[string]string
	fallback hydrate.SourceProvider
}

func (p *syncSourceProvider) Source(component hydrate.ComponentPlan) (hydrate.Source, error) {
	if root, ok := p.local[component.Name]; ok {
		return hydrate.Source{Root: root}, nil
	}
	if p.fallback == nil {
		return hydrate.Source{}, fmt.Errorf("source for component %q not found", component.Name)
	}
	return p.fallback.Source(component)
}

func (p *syncSourceProvider) Close() error {
	if p == nil || p.fallback == nil {
		return nil
	}
	closer, ok := p.fallback.(interface{ Close() error })
	if !ok {
		return nil
	}
	return closer.Close()
}
