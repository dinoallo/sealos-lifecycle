package cmd

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/opencontainers/go-digest"
	"sigs.k8s.io/yaml"

	"github.com/labring/sealos/pkg/buildah"
	"github.com/labring/sealos/pkg/constants"
	"github.com/labring/sealos/pkg/distribution"
	"github.com/labring/sealos/pkg/distribution/bom"
	"github.com/labring/sealos/pkg/distribution/compare"
	"github.com/labring/sealos/pkg/distribution/hydrate"
	"github.com/labring/sealos/pkg/distribution/localrepo"
	"github.com/labring/sealos/pkg/distribution/ocipackage"
	"github.com/labring/sealos/pkg/distribution/ownership"
	"github.com/labring/sealos/pkg/distribution/packageformat"
	"github.com/labring/sealos/pkg/distribution/policyreport"
	"github.com/labring/sealos/pkg/distribution/reconcile"
	"github.com/labring/sealos/pkg/distribution/state"
	v1beta1 "github.com/labring/sealos/pkg/types/v1beta1"
	yamlutil "github.com/labring/sealos/pkg/utils/yaml"
)

func TestSyncRenderCmdWithLocalPackageSource(t *testing.T) {
	previousRuntimeRoot := constants.DefaultRuntimeRootDir
	constants.DefaultRuntimeRootDir = t.TempDir()
	t.Cleanup(func() {
		constants.DefaultRuntimeRootDir = previousRuntimeRoot
	})

	previousFactory := newSyncBuildah
	newSyncBuildah = func(string) (buildah.Interface, error) {
		return nil, fmt.Errorf("unexpected buildah usage")
	}
	t.Cleanup(func() {
		newSyncBuildah = previousFactory
	})

	bomPath := filepath.Join(t.TempDir(), "bom.yaml")
	if err := yamlutil.MarshalFile(bomPath, testSyncBOM()); err != nil {
		t.Fatalf("MarshalFile() error = %v", err)
	}
	if err := os.Chmod(filepath.Join(syncFixtureRoot(), "hooks", "bootstrap.sh"), 0o755); err != nil {
		t.Fatalf("Chmod(fixture bootstrap hook) error = %v", err)
	}

	buf := bytes.NewBuffer(nil)
	cmd := newSyncCmd()
	cmd.SetOut(buf)
	cmd.SetErr(buf)
	cmd.SetArgs([]string{
		"render",
		"--file", bomPath,
		"--cluster", "cluster-a",
		"--local-patch-revision", "local-rev-1",
		"--package-source", "kubernetes=" + syncFixtureRoot(),
	})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute() error = %v", err)
	}

	var out syncRenderOutput
	if err := yaml.Unmarshal(buf.Bytes(), &out); err != nil {
		t.Fatalf("Unmarshal() error = %v\noutput=%s", err, buf.String())
	}

	if got, want := out.ClusterName, "cluster-a"; got != want {
		t.Fatalf("out.ClusterName = %q, want %q", got, want)
	}
	if !strings.HasPrefix(out.DesiredStateDigest, "sha256:") {
		t.Fatalf("out.DesiredStateDigest = %q, want sha256 digest", out.DesiredStateDigest)
	}
	if out.Preflight.Blocked {
		t.Fatalf("preflight.blocked = true, want false; reasons=%#v", out.Preflight.BlockedReasons)
	}
	if got, want := out.Preflight.State, syncPreflightStateReady; got != want {
		t.Fatalf("preflight.state = %q, want %q", got, want)
	}
	if got, want := out.Preflight.RecommendedAction, syncPreflightRecommendedActionApply; got != want {
		t.Fatalf("preflight.recommendedAction = %q, want %q", got, want)
	}
	if out.Preflight.Summary == "" {
		t.Fatal("preflight.summary is empty")
	}
	if !strings.Contains(out.Preflight.RefreshCommand, "'sealos' 'sync' 'render'") {
		t.Fatalf("preflight.refreshCommand = %q, want sync render command", out.Preflight.RefreshCommand)
	}
	if got, want := out.Preflight.TopologyStatus.State, syncTopologyStateMatched; got != want {
		t.Fatalf("preflight.topologyStatus.state = %q, want %q", got, want)
	}
	if got, want := out.Preflight.RenderInputStatus.State, syncRenderInputStateMatched; got != want {
		t.Fatalf("preflight.renderInputStatus.state = %q, want %q", got, want)
	}
	if out.SourcePreflight == nil {
		t.Fatal("sourcePreflight = nil, want render to report source preflight")
	}
	if out.SourcePreflight.Blocked {
		t.Fatalf("sourcePreflight.blocked = true, reasons=%#v", out.SourcePreflight.BlockedReasons)
	}
	if got, want := out.SourcePreflight.State, syncPreflightStateReady; got != want {
		t.Fatalf("sourcePreflight.state = %q, want %q", got, want)
	}
	if got, want := out.RenderProvenance.BOMPath, bomPath; got != want {
		t.Fatalf("renderProvenance.bomPath = %q, want %q", got, want)
	}
	if !strings.HasPrefix(out.RenderProvenance.BOMDigest, "sha256:") {
		t.Fatalf("renderProvenance.bomDigest = %q, want sha256 digest", out.RenderProvenance.BOMDigest)
	}
	if got, want := out.RenderProvenance.LocalPatchRevision, "local-rev-1"; got != want {
		t.Fatalf("renderProvenance.localPatchRevision = %q, want %q", got, want)
	}
	if got, want := len(out.RenderProvenance.PackageSources), 1; got != want {
		t.Fatalf("len(renderProvenance.packageSources) = %d, want %d", got, want)
	}
	if got, want := out.RenderProvenance.PackageSources[0].Component, "kubernetes"; got != want {
		t.Fatalf("renderProvenance.packageSources[0].component = %q, want %q", got, want)
	}
	absFixtureRoot, err := filepath.Abs(syncFixtureRoot())
	if err != nil {
		t.Fatalf("Abs(syncFixtureRoot) error = %v", err)
	}
	if got, want := out.RenderProvenance.PackageSources[0].Path, absFixtureRoot; got != want {
		t.Fatalf("renderProvenance.packageSources[0].path = %q, want %q", got, want)
	}
	if !strings.HasPrefix(out.RenderProvenance.PackageSources[0].Digest, "sha256:") {
		t.Fatalf("renderProvenance.packageSources[0].digest = %q, want sha256 digest", out.RenderProvenance.PackageSources[0].Digest)
	}
	loadedBundle, err := reconcile.LoadBundle(out.BundlePath)
	if err != nil {
		t.Fatalf("LoadBundle(rendered) error = %v", err)
	}
	if loadedBundle.Spec.SourcePreflight == nil {
		t.Fatal("rendered bundle sourcePreflight = nil, want persisted source preflight summary")
	}
	if got, want := loadedBundle.Spec.SourcePreflight.State, string(syncPreflightStateReady); got != want {
		t.Fatalf("rendered bundle sourcePreflight.state = %q, want %q", got, want)
	}
	if got, want := loadedBundle.Spec.SourcePreflight.Counts.Components, out.SourcePreflight.Counts.Components; got != want {
		t.Fatalf("rendered bundle sourcePreflight.counts.components = %d, want %d", got, want)
	}
	if got := loadedBundle.Spec.SourcePreflight.Validate.Errors; got != 0 {
		t.Fatalf("rendered bundle sourcePreflight.validate.errors = %d, want 0", got)
	}

	doc, err := state.LoadAppliedRevision("cluster-a")
	if err != nil {
		t.Fatalf("LoadAppliedRevision() error = %v", err)
	}
	if got, want := doc.Status.State, state.StateDirty; got != want {
		t.Fatalf("status.state = %q, want %q", got, want)
	}
	if got, want := doc.Spec.LocalPatchRevision, "local-rev-1"; got != want {
		t.Fatalf("spec.localPatchRevision = %q, want %q", got, want)
	}
	if got, want := doc.Spec.DesiredStateDigest, out.DesiredStateDigest; got != want {
		t.Fatalf("spec.desiredStateDigest = %q, want %q", got, want)
	}
}

func TestSyncRenderCmdWithReleaseChannelDocument(t *testing.T) {
	previousRuntimeRoot := constants.DefaultRuntimeRootDir
	runtimeRoot := t.TempDir()
	constants.DefaultRuntimeRootDir = runtimeRoot
	t.Cleanup(func() {
		constants.DefaultRuntimeRootDir = previousRuntimeRoot
	})

	bomPath := filepath.Join(t.TempDir(), "bom.yaml")
	doc := testSyncBOM()
	if err := yamlutil.MarshalFile(bomPath, doc); err != nil {
		t.Fatalf("MarshalFile(bom) error = %v", err)
	}
	channelPath := filepath.Join(filepath.Dir(bomPath), "channel.yaml")
	channel := bom.NewReleaseChannel("test-platform-stable", doc.Metadata.Name, bom.ChannelStable, doc.Spec.Revision, "bom.yaml")
	if err := yamlutil.MarshalFile(channelPath, channel); err != nil {
		t.Fatalf("MarshalFile(channel) error = %v", err)
	}

	buf := bytes.NewBuffer(nil)
	cmd := newSyncCmd()
	cmd.SetOut(buf)
	cmd.SetErr(buf)
	cmd.SetArgs([]string{
		"render",
		"--release-channel", channelPath,
		"--cluster", "cluster-channel",
		"--runtime-root", runtimeRoot,
		"--package-source", "kubernetes=" + syncFixtureRoot(),
	})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute() error = %v\noutput=%s", err, buf.String())
	}

	var out syncRenderOutput
	if err := yaml.Unmarshal(buf.Bytes(), &out); err != nil {
		t.Fatalf("Unmarshal() error = %v\noutput=%s", err, buf.String())
	}
	if got, want := out.Channel, string(bom.ChannelStable); got != want {
		t.Fatalf("out.Channel = %q, want %q", got, want)
	}
	absChannelPath, err := filepath.Abs(channelPath)
	if err != nil {
		t.Fatalf("Abs(channelPath) error = %v", err)
	}
	if got, want := out.RenderProvenance.ReleaseChannelPath, absChannelPath; got != want {
		t.Fatalf("renderProvenance.releaseChannelPath = %q, want %q", got, want)
	}
	if got, want := out.RenderProvenance.DistributionLine, doc.Metadata.Name; got != want {
		t.Fatalf("renderProvenance.distributionLine = %q, want %q", got, want)
	}
	if !strings.HasPrefix(out.RenderProvenance.ReleaseChannelDigest, "sha256:") {
		t.Fatalf("renderProvenance.releaseChannelDigest = %q, want sha256 digest", out.RenderProvenance.ReleaseChannelDigest)
	}
	if out.SourcePreflight == nil {
		t.Fatal("sourcePreflight = nil, want source preflight output")
	}
	if got, want := out.SourcePreflight.ReleaseChannelPath, channelPath; got != want {
		t.Fatalf("sourcePreflight.releaseChannelPath = %q, want %q", got, want)
	}
	if !strings.Contains(out.SourcePreflight.RenderCommand, "--release-channel") {
		t.Fatalf("sourcePreflight.renderCommand = %q, want --release-channel", out.SourcePreflight.RenderCommand)
	}

	loadedBundle, err := reconcile.LoadBundle(out.BundlePath)
	if err != nil {
		t.Fatalf("LoadBundle(rendered) error = %v", err)
	}
	if got, want := loadedBundle.Spec.Channel, bom.ChannelStable; got != want {
		t.Fatalf("bundle spec.channel = %q, want %q", got, want)
	}
	if got, want := loadedBundle.Spec.RenderProvenance.ReleaseChannelPath, absChannelPath; got != want {
		t.Fatalf("bundle renderProvenance.releaseChannelPath = %q, want %q", got, want)
	}
}

func TestSyncRenderCmdRejectsAmbiguousTarget(t *testing.T) {
	bomPath := filepath.Join(t.TempDir(), "bom.yaml")
	if err := yamlutil.MarshalFile(bomPath, testSyncBOM()); err != nil {
		t.Fatalf("MarshalFile(bom) error = %v", err)
	}
	channelPath := filepath.Join(filepath.Dir(bomPath), "channel.yaml")
	doc := testSyncBOM()
	channel := bom.NewReleaseChannel("test-platform-alpha", doc.Metadata.Name, bom.ChannelAlpha, doc.Spec.Revision, "bom.yaml")
	if err := yamlutil.MarshalFile(channelPath, channel); err != nil {
		t.Fatalf("MarshalFile(channel) error = %v", err)
	}

	cmd := newSyncCmd()
	cmd.SetArgs([]string{
		"render",
		"--file", bomPath,
		"--release-channel", channelPath,
		"--package-source", "kubernetes=" + syncFixtureRoot(),
	})

	if err := cmd.Execute(); err == nil {
		t.Fatal("Execute() error = nil, want ambiguous target error")
	}
}

func TestSyncPromoteCmd(t *testing.T) {
	root := t.TempDir()
	channelPath := filepath.Join(root, "channels", "stable.yaml")
	if err := os.MkdirAll(filepath.Dir(channelPath), 0o755); err != nil {
		t.Fatalf("MkdirAll(channel dir) error = %v", err)
	}
	oldBOM := testSyncBOM()
	oldBOM.Spec.Revision = "rev-20240423"
	oldBOMPath := filepath.Join(root, "boms", "rev-20240423.yaml")
	if err := yamlutil.MarshalFile(oldBOMPath, oldBOM); err != nil {
		t.Fatalf("MarshalFile(oldBOM) error = %v", err)
	}
	targetBOM := testSyncBOM()
	targetBOM.Spec.Revision = "rev-20240424"
	targetBOMPath := filepath.Join(root, "boms", "rev-20240424.yaml")
	if err := yamlutil.MarshalFile(targetBOMPath, targetBOM); err != nil {
		t.Fatalf("MarshalFile(targetBOM) error = %v", err)
	}
	healthProofPath := filepath.Join(root, "proofs", "rev-20240424-health.yaml")
	healthProof := bom.NewDistributionHealthProof("test-platform-rev-20240424", targetBOM.Metadata.Name, targetBOM.Spec.Revision, true)
	healthProof.Spec.Summary = "beta cohort passed"
	healthProof.Spec.Signals = []bom.DistributionHealthSignal{{Name: "node-readiness", Passed: true}}
	if err := yamlutil.MarshalFile(healthProofPath, healthProof); err != nil {
		t.Fatalf("MarshalFile(healthProof) error = %v", err)
	}
	channel := bom.NewReleaseChannel("test-platform-stable", targetBOM.Metadata.Name, bom.ChannelStable, oldBOM.Spec.Revision, "../boms/rev-20240423.yaml")
	if err := yamlutil.MarshalFile(channelPath, channel); err != nil {
		t.Fatalf("MarshalFile(channel) error = %v", err)
	}

	buf := bytes.NewBuffer(nil)
	cmd := newSyncCmd()
	cmd.SetOut(buf)
	cmd.SetErr(buf)
	cmd.SetArgs([]string{
		"promote",
		"--release-channel", channelPath,
		"--target-bom", targetBOMPath,
		"--source-channel", string(bom.ChannelBeta),
		"--health-proof", healthProofPath,
		"--reason", "beta cohort passed",
		"--approved-by", "release-team",
		"--approved-at", "2024-04-24T10:30:00Z",
	})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute() error = %v\noutput=%s", err, buf.String())
	}

	var out syncPromoteOutput
	if err := yaml.Unmarshal(buf.Bytes(), &out); err != nil {
		t.Fatalf("Unmarshal() error = %v\noutput=%s", err, buf.String())
	}
	if got, want := out.ReleaseChannelPath, channelPath; got != want {
		t.Fatalf("releaseChannelPath = %q, want %q", got, want)
	}
	if got, want := out.Line, targetBOM.Metadata.Name; got != want {
		t.Fatalf("line = %q, want %q", got, want)
	}
	if got, want := out.Channel, string(bom.ChannelStable); got != want {
		t.Fatalf("channel = %q, want %q", got, want)
	}
	if got, want := out.FromRevision, "rev-20240423"; got != want {
		t.Fatalf("fromRevision = %q, want %q", got, want)
	}
	if got, want := out.ToRevision, "rev-20240424"; got != want {
		t.Fatalf("toRevision = %q, want %q", got, want)
	}
	if !out.Changed {
		t.Fatal("changed = false, want true")
	}
	if got, want := out.Promotion.ApprovedAt, "2024-04-24T10:30:00Z"; got != want {
		t.Fatalf("promotion.approvedAt = %q, want %q", got, want)
	}
	if got, want := out.Promotion.BOMPath, "../boms/rev-20240424.yaml"; got != want {
		t.Fatalf("promotion.bomPath = %q, want %q", got, want)
	}
	if got, want := out.Promotion.HealthProofPath, "../proofs/rev-20240424-health.yaml"; got != want {
		t.Fatalf("promotion.healthProofPath = %q, want %q", got, want)
	}
	if got, want := out.Promotion.HealthProofSummary, "beta cohort passed"; got != want {
		t.Fatalf("promotion.healthProofSummary = %q, want %q", got, want)
	}
	if !strings.HasPrefix(out.Promotion.HealthProofDigest, "sha256:") {
		t.Fatalf("promotion.healthProofDigest = %q, want sha256 digest", out.Promotion.HealthProofDigest)
	}
	if out.PolicyDecision == nil {
		t.Fatal("policyDecision = nil, want promotion policy decision")
	}
	if !out.PolicyDecision.Allowed {
		t.Fatalf("policyDecision.allowed = false, violations=%#v", out.PolicyDecision.Violations)
	}
	if got, want := string(out.PolicyDecision.Transition.SourceChannel), string(bom.ChannelBeta); got != want {
		t.Fatalf("policyDecision.transition.sourceChannel = %q, want %q", got, want)
	}
	if got, want := string(out.PolicyDecision.Transition.TargetChannel), string(bom.ChannelStable); got != want {
		t.Fatalf("policyDecision.transition.targetChannel = %q, want %q", got, want)
	}
	if !out.PolicyDecision.Transition.HealthProofRequired {
		t.Fatal("policyDecision.transition.healthProofRequired = false, want true")
	}

	loaded, err := bom.LoadReleaseChannelFile(channelPath)
	if err != nil {
		t.Fatalf("LoadReleaseChannelFile() error = %v", err)
	}
	if got, want := loaded.Spec.TargetRevision, "rev-20240424"; got != want {
		t.Fatalf("persisted targetRevision = %q, want %q", got, want)
	}
	if got, want := loaded.Spec.BOMPath, "../boms/rev-20240424.yaml"; got != want {
		t.Fatalf("persisted bomPath = %q, want %q", got, want)
	}
	if got, want := len(loaded.Spec.PromotionHistory), 1; got != want {
		t.Fatalf("len(persisted promotionHistory) = %d, want %d", got, want)
	}
}

func TestSyncPromoteCmdRejectsFailedHealthProof(t *testing.T) {
	root := t.TempDir()
	channelPath := filepath.Join(root, "channels", "stable.yaml")
	targetBOMPath := filepath.Join(root, "boms", "rev-20240424.yaml")
	healthProofPath := filepath.Join(root, "proofs", "rev-20240424-health.yaml")
	targetBOM := testSyncBOM()
	targetBOM.Spec.Revision = "rev-20240424"
	if err := yamlutil.MarshalFile(targetBOMPath, targetBOM); err != nil {
		t.Fatalf("MarshalFile(targetBOM) error = %v", err)
	}
	channel := bom.NewReleaseChannel("test-platform-stable", targetBOM.Metadata.Name, bom.ChannelStable, "rev-20240423", "../boms/rev-20240423.yaml")
	if err := yamlutil.MarshalFile(channelPath, channel); err != nil {
		t.Fatalf("MarshalFile(channel) error = %v", err)
	}
	healthProof := bom.NewDistributionHealthProof("test-platform-rev-20240424", targetBOM.Metadata.Name, targetBOM.Spec.Revision, false)
	if err := yamlutil.MarshalFile(healthProofPath, healthProof); err != nil {
		t.Fatalf("MarshalFile(healthProof) error = %v", err)
	}

	cmd := newSyncCmd()
	cmd.SetArgs([]string{
		"promote",
		"--release-channel", channelPath,
		"--target-bom", targetBOMPath,
		"--health-proof", healthProofPath,
		"--reason", "passed canary",
		"--approved-by", "release-team",
	})

	err := cmd.Execute()
	if err == nil {
		t.Fatal("Execute() error = nil, want failed health proof error")
	}
	if !strings.Contains(err.Error(), "did not pass") {
		t.Fatalf("Execute() error = %v, want health proof failure", err)
	}
}

func TestSyncPromoteCmdRejectsEmptyHealthProofSignals(t *testing.T) {
	root := t.TempDir()
	channelPath := filepath.Join(root, "channels", "stable.yaml")
	targetBOMPath := filepath.Join(root, "boms", "rev-20240424.yaml")
	healthProofPath := filepath.Join(root, "proofs", "rev-20240424-health.yaml")
	targetBOM := testSyncBOM()
	targetBOM.Spec.Revision = "rev-20240424"
	if err := yamlutil.MarshalFile(targetBOMPath, targetBOM); err != nil {
		t.Fatalf("MarshalFile(targetBOM) error = %v", err)
	}
	channel := bom.NewReleaseChannel("test-platform-stable", targetBOM.Metadata.Name, bom.ChannelStable, "rev-20240423", "../boms/rev-20240423.yaml")
	if err := yamlutil.MarshalFile(channelPath, channel); err != nil {
		t.Fatalf("MarshalFile(channel) error = %v", err)
	}
	healthProof := bom.NewDistributionHealthProof("test-platform-rev-20240424", targetBOM.Metadata.Name, targetBOM.Spec.Revision, true)
	if err := yamlutil.MarshalFile(healthProofPath, healthProof); err != nil {
		t.Fatalf("MarshalFile(healthProof) error = %v", err)
	}

	cmd := newSyncCmd()
	cmd.SetArgs([]string{
		"promote",
		"--release-channel", channelPath,
		"--target-bom", targetBOMPath,
		"--health-proof", healthProofPath,
		"--reason", "passed canary",
		"--approved-by", "release-team",
	})

	err := cmd.Execute()
	if err == nil {
		t.Fatal("Execute() error = nil, want empty health signal error")
	}
	if !strings.Contains(err.Error(), "has no health signals") {
		t.Fatalf("Execute() error = %v, want empty health signal error", err)
	}
}

func TestSyncPromoteCmdRejectsMissingHealthProofForStable(t *testing.T) {
	root := t.TempDir()
	channelPath := filepath.Join(root, "channels", "stable.yaml")
	targetBOMPath := filepath.Join(root, "boms", "rev-20240424.yaml")
	targetBOM := testSyncBOM()
	targetBOM.Spec.Revision = "rev-20240424"
	if err := yamlutil.MarshalFile(targetBOMPath, targetBOM); err != nil {
		t.Fatalf("MarshalFile(targetBOM) error = %v", err)
	}
	channel := bom.NewReleaseChannel("test-platform-stable", targetBOM.Metadata.Name, bom.ChannelStable, "rev-20240423", "../boms/rev-20240423.yaml")
	if err := yamlutil.MarshalFile(channelPath, channel); err != nil {
		t.Fatalf("MarshalFile(channel) error = %v", err)
	}

	cmd := newSyncCmd()
	cmd.SetArgs([]string{
		"promote",
		"--release-channel", channelPath,
		"--target-bom", targetBOMPath,
		"--reason", "passed canary",
		"--approved-by", "release-team",
	})

	err := cmd.Execute()
	if err == nil {
		t.Fatal("Execute() error = nil, want promotion policy error")
	}
	if !strings.Contains(err.Error(), "healthProofRequired") {
		t.Fatalf("Execute() error = %v, want health proof policy violation", err)
	}
}

func TestSyncPromoteCmdRejectsAlphaCandidateForStable(t *testing.T) {
	root := t.TempDir()
	channelPath := filepath.Join(root, "channels", "stable.yaml")
	targetBOMPath := filepath.Join(root, "boms", "rev-20240424.yaml")
	healthProofPath := filepath.Join(root, "proofs", "rev-20240424-health.yaml")
	targetBOM := testSyncBOM()
	targetBOM.Spec.Revision = "rev-20240424"
	if err := yamlutil.MarshalFile(targetBOMPath, targetBOM); err != nil {
		t.Fatalf("MarshalFile(targetBOM) error = %v", err)
	}
	channel := bom.NewReleaseChannel("test-platform-stable", targetBOM.Metadata.Name, bom.ChannelStable, "rev-20240423", "../boms/rev-20240423.yaml")
	if err := yamlutil.MarshalFile(channelPath, channel); err != nil {
		t.Fatalf("MarshalFile(channel) error = %v", err)
	}
	healthProof := bom.NewDistributionHealthProof("test-platform-rev-20240424", targetBOM.Metadata.Name, targetBOM.Spec.Revision, true)
	healthProof.Spec.Signals = []bom.DistributionHealthSignal{{Name: "node-readiness", Passed: true}}
	if err := yamlutil.MarshalFile(healthProofPath, healthProof); err != nil {
		t.Fatalf("MarshalFile(healthProof) error = %v", err)
	}

	cmd := newSyncCmd()
	cmd.SetArgs([]string{
		"promote",
		"--release-channel", channelPath,
		"--target-bom", targetBOMPath,
		"--source-channel", string(bom.ChannelAlpha),
		"--health-proof", healthProofPath,
		"--reason", "passed canary",
		"--approved-by", "release-team",
	})

	err := cmd.Execute()
	if err == nil {
		t.Fatal("Execute() error = nil, want source channel policy error")
	}
	if !strings.Contains(err.Error(), "sourceChannelBlocked") {
		t.Fatalf("Execute() error = %v, want source channel policy violation", err)
	}
}

func TestSyncPromoteCmdRejectsInvalidApprovedAt(t *testing.T) {
	cmd := newSyncCmd()
	cmd.SetArgs([]string{
		"promote",
		"--release-channel", filepath.Join(t.TempDir(), "channel.yaml"),
		"--target-bom", filepath.Join(t.TempDir(), "bom.yaml"),
		"--reason", "passed canary",
		"--approved-by", "release-team",
		"--approved-at", "not-rfc3339",
	})

	err := cmd.Execute()
	if err == nil {
		t.Fatal("Execute() error = nil, want invalid timestamp")
	}
	if !strings.Contains(err.Error(), "parse --approved-at as RFC3339") {
		t.Fatalf("Execute() error = %v, want approved-at parse error", err)
	}
}

func TestSyncHealthProofCmdFromAcceptanceReport(t *testing.T) {
	root := t.TempDir()
	targetBOM := testSyncBOM()
	targetBOM.Spec.Revision = "rev-20240424"
	bomPath := filepath.Join(root, "boms", "rev-20240424.yaml")
	if err := yamlutil.MarshalFile(bomPath, targetBOM); err != nil {
		t.Fatalf("MarshalFile(targetBOM) error = %v", err)
	}
	reportPath := filepath.Join(root, "reports", "acceptance-report.yaml")
	bomData, err := os.ReadFile(bomPath)
	if err != nil {
		t.Fatalf("ReadFile(targetBOM) error = %v", err)
	}
	writeSyncHealthProofTestReport(t, reportPath, syncHealthProofTestReportOptions{
		BOMFile:               bomPath,
		BOMName:               targetBOM.Metadata.Name,
		BOMRevision:           targetBOM.Spec.Revision,
		BOMDigest:             digest.Canonical.FromBytes(bomData).String(),
		Status:                "Passed",
		ExitCode:              0,
		MutatingApply:         true,
		RevertCheck:           true,
		SourcePreflightState:  "Ready",
		RuntimePreflightState: "Warning",
		PostApplyState:        "Clean",
		PostRevertState:       "Clean",
		Stages:                syncHealthProofTestStages(true),
	})
	proofPath := filepath.Join(root, "proofs", "rev-20240424-health.yaml")

	buf := bytes.NewBuffer(nil)
	cmd := newSyncCmd()
	cmd.SetOut(buf)
	cmd.SetErr(buf)
	cmd.SetArgs([]string{
		"health-proof",
		"--file", bomPath,
		"--acceptance-report", reportPath,
		"--output-file", proofPath,
		"--summary", "beta cohort passed",
	})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute() error = %v\noutput=%s", err, buf.String())
	}

	var out bom.DistributionHealthProof
	if err := yaml.Unmarshal(buf.Bytes(), &out); err != nil {
		t.Fatalf("Unmarshal(output) error = %v\noutput=%s", err, buf.String())
	}
	if got, want := out.Metadata.Name, "default-platform-rev-20240424-health"; got != want {
		t.Fatalf("metadata.name = %q, want %q", got, want)
	}
	if got, want := out.Spec.Line, targetBOM.Metadata.Name; got != want {
		t.Fatalf("spec.line = %q, want %q", got, want)
	}
	if got, want := out.Spec.TargetRevision, targetBOM.Spec.Revision; got != want {
		t.Fatalf("spec.targetRevision = %q, want %q", got, want)
	}
	if !out.Spec.Passed {
		t.Fatal("spec.passed = false, want true")
	}
	if got, want := out.Spec.Summary, "beta cohort passed"; got != want {
		t.Fatalf("spec.summary = %q, want %q", got, want)
	}
	if got, want := out.Spec.CollectedAt, "2024-04-24T11:00:00Z"; got != want {
		t.Fatalf("spec.collectedAt = %q, want %q", got, want)
	}
	if !syncHealthProofTestSignalPassed(out, "runtime-preflight") {
		t.Fatal("runtime-preflight signal did not pass; Warning should be accepted as non-blocking")
	}
	if !syncHealthProofTestSignalPassed(out, "bom-file") {
		t.Fatal("bom-file signal did not pass")
	}
	if !syncHealthProofTestSignalPassed(out, "bom-identity") {
		t.Fatal("bom-identity signal did not pass")
	}
	if !syncHealthProofTestSignalPassed(out, "bom-digest") {
		t.Fatal("bom-digest signal did not pass")
	}
	if !syncHealthProofTestSignalPassed(out, "desired-state-digest") {
		t.Fatal("desired-state-digest signal did not pass")
	}
	if !syncHealthProofTestSignalPassed(out, "local-repo-revision") {
		t.Fatal("local-repo-revision signal did not pass")
	}
	if !syncHealthProofTestSignalPassed(out, "mutating-apply") {
		t.Fatal("mutating-apply signal did not pass")
	}
	if !syncHealthProofTestSignalPassed(out, "contract/apply") {
		t.Fatal("contract/apply signal did not pass")
	}
	if !syncHealthProofTestSignalPassed(out, "contract/validate-cluster-after-revert") {
		t.Fatal("contract/validate-cluster-after-revert signal did not pass")
	}
	if !syncHealthProofTestSignalPassed(out, "stage/apply") {
		t.Fatal("stage/apply signal did not pass")
	}
	loaded, _, err := bom.LoadDistributionHealthProofFile(proofPath)
	if err != nil {
		t.Fatalf("LoadDistributionHealthProofFile() error = %v", err)
	}
	if got, want := loaded.Spec.TargetRevision, targetBOM.Spec.Revision; got != want {
		t.Fatalf("persisted spec.targetRevision = %q, want %q", got, want)
	}
}

func TestSyncHealthProofCmdMatchesAbsoluteReportBOMFileAgainstRelativeTarget(t *testing.T) {
	root := t.TempDir()
	targetBOM := testSyncBOM()
	bomPath := filepath.Join(root, "bom.yaml")
	if err := yamlutil.MarshalFile(bomPath, targetBOM); err != nil {
		t.Fatalf("MarshalFile(targetBOM) error = %v", err)
	}
	bomData, err := os.ReadFile(bomPath)
	if err != nil {
		t.Fatalf("ReadFile(targetBOM) error = %v", err)
	}
	reportPath := filepath.Join(root, "acceptance-report.yaml")
	writeSyncHealthProofTestReport(t, reportPath, syncHealthProofTestReportOptions{
		BOMFile:               bomPath,
		BOMName:               targetBOM.Metadata.Name,
		BOMRevision:           targetBOM.Spec.Revision,
		BOMDigest:             digest.Canonical.FromBytes(bomData).String(),
		Status:                "Passed",
		ExitCode:              0,
		MutatingApply:         true,
		SourcePreflightState:  "Ready",
		RuntimePreflightState: "Ready",
		PostApplyState:        "Clean",
		Stages:                syncHealthProofTestStages(false),
	})
	previousWd, err := os.Getwd()
	if err != nil {
		t.Fatalf("Getwd() error = %v", err)
	}
	if err := os.Chdir(root); err != nil {
		t.Fatalf("Chdir(%q) error = %v", root, err)
	}
	t.Cleanup(func() {
		if err := os.Chdir(previousWd); err != nil {
			t.Fatalf("restore working directory %q: %v", previousWd, err)
		}
	})

	buf := bytes.NewBuffer(nil)
	cmd := newSyncCmd()
	cmd.SetOut(buf)
	cmd.SetErr(buf)
	cmd.SetArgs([]string{
		"health-proof",
		"--file", "bom.yaml",
		"--acceptance-report", reportPath,
	})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute() error = %v\noutput=%s", err, buf.String())
	}
	var out bom.DistributionHealthProof
	if err := yaml.Unmarshal(buf.Bytes(), &out); err != nil {
		t.Fatalf("Unmarshal(output) error = %v\noutput=%s", err, buf.String())
	}
	if !out.Spec.Passed {
		t.Fatal("spec.passed = false, want true")
	}
	if !syncHealthProofTestSignalPassed(out, "bom-file") {
		t.Fatal("bom-file signal did not pass")
	}
	if !syncHealthProofTestSignalPassed(out, "bom-digest") {
		t.Fatal("bom-digest signal did not pass")
	}
}

func TestSyncHealthProofCmdReportsFailedAcceptanceSignal(t *testing.T) {
	root := t.TempDir()
	targetBOM := testSyncBOM()
	bomPath := filepath.Join(root, "bom.yaml")
	if err := yamlutil.MarshalFile(bomPath, targetBOM); err != nil {
		t.Fatalf("MarshalFile(targetBOM) error = %v", err)
	}
	reportPath := filepath.Join(root, "acceptance-report.yaml")
	writeSyncHealthProofTestReport(t, reportPath, syncHealthProofTestReportOptions{
		BOMFile:               bomPath,
		Status:                "Failed",
		ExitCode:              1,
		SourcePreflightState:  "Ready",
		RuntimePreflightState: "Blocked",
		Stages: []syncHealthProofAcceptanceReportStage{
			{Name: "runtime-preflight", Status: "Failed", Reason: "blocked"},
		},
	})

	buf := bytes.NewBuffer(nil)
	cmd := newSyncCmd()
	cmd.SetOut(buf)
	cmd.SetErr(buf)
	cmd.SetArgs([]string{
		"health-proof",
		"--file", bomPath,
		"--acceptance-report", reportPath,
	})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute() error = %v\noutput=%s", err, buf.String())
	}
	var out bom.DistributionHealthProof
	if err := yaml.Unmarshal(buf.Bytes(), &out); err != nil {
		t.Fatalf("Unmarshal(output) error = %v\noutput=%s", err, buf.String())
	}
	if out.Spec.Passed {
		t.Fatal("spec.passed = true, want false")
	}
	if syncHealthProofTestSignalPassed(out, "acceptance-report") {
		t.Fatal("acceptance-report signal passed, want failed")
	}
	if syncHealthProofTestSignalPassed(out, "runtime-preflight") {
		t.Fatal("runtime-preflight signal passed, want failed")
	}
	if syncHealthProofTestSignalPassed(out, "stage/runtime-preflight") {
		t.Fatal("stage/runtime-preflight signal passed, want failed")
	}
}

func TestSyncHealthProofCmdRequiresMutatingApplyEvidence(t *testing.T) {
	root := t.TempDir()
	bomPath := filepath.Join(root, "bom.yaml")
	if err := yamlutil.MarshalFile(bomPath, testSyncBOM()); err != nil {
		t.Fatalf("MarshalFile(targetBOM) error = %v", err)
	}
	reportPath := filepath.Join(root, "acceptance-report.yaml")
	writeSyncHealthProofTestReport(t, reportPath, syncHealthProofTestReportOptions{
		BOMFile:               bomPath,
		Status:                "Passed",
		ExitCode:              0,
		SourcePreflightState:  "Ready",
		RuntimePreflightState: "Ready",
		Stages:                syncHealthProofTestStages(false),
	})

	buf := bytes.NewBuffer(nil)
	cmd := newSyncCmd()
	cmd.SetOut(buf)
	cmd.SetErr(buf)
	cmd.SetArgs([]string{
		"health-proof",
		"--file", bomPath,
		"--acceptance-report", reportPath,
	})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute() error = %v\noutput=%s", err, buf.String())
	}
	var out bom.DistributionHealthProof
	if err := yaml.Unmarshal(buf.Bytes(), &out); err != nil {
		t.Fatalf("Unmarshal(output) error = %v\noutput=%s", err, buf.String())
	}
	if out.Spec.Passed {
		t.Fatal("spec.passed = true, want false")
	}
	if syncHealthProofTestSignalPassed(out, "mutating-apply") {
		t.Fatal("mutating-apply signal passed, want failed")
	}
}

func TestSyncHealthProofCmdFailsWhenReportBOMFileDoesNotMatch(t *testing.T) {
	root := t.TempDir()
	bomPath := filepath.Join(root, "bom.yaml")
	if err := yamlutil.MarshalFile(bomPath, testSyncBOM()); err != nil {
		t.Fatalf("MarshalFile(targetBOM) error = %v", err)
	}
	reportPath := filepath.Join(root, "acceptance-report.yaml")
	writeSyncHealthProofTestReport(t, reportPath, syncHealthProofTestReportOptions{
		BOMFile:               filepath.Join(root, "other-bom.yaml"),
		Status:                "Passed",
		ExitCode:              0,
		MutatingApply:         true,
		SourcePreflightState:  "Ready",
		RuntimePreflightState: "Ready",
		PostApplyState:        "Clean",
		Stages:                syncHealthProofTestStages(false),
	})

	buf := bytes.NewBuffer(nil)
	cmd := newSyncCmd()
	cmd.SetOut(buf)
	cmd.SetErr(buf)
	cmd.SetArgs([]string{
		"health-proof",
		"--file", bomPath,
		"--acceptance-report", reportPath,
	})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute() error = %v\noutput=%s", err, buf.String())
	}
	var out bom.DistributionHealthProof
	if err := yaml.Unmarshal(buf.Bytes(), &out); err != nil {
		t.Fatalf("Unmarshal(output) error = %v\noutput=%s", err, buf.String())
	}
	if out.Spec.Passed {
		t.Fatal("spec.passed = true, want false")
	}
	if syncHealthProofTestSignalPassed(out, "bom-file") {
		t.Fatal("bom-file signal passed, want failed")
	}
}

func TestSyncHealthProofCmdFailsWhenReportBOMIdentityDoesNotMatch(t *testing.T) {
	root := t.TempDir()
	bomPath := filepath.Join(root, "bom.yaml")
	targetBOM := testSyncBOM()
	targetBOM.Spec.Revision = "rev-20240424"
	if err := yamlutil.MarshalFile(bomPath, targetBOM); err != nil {
		t.Fatalf("MarshalFile(targetBOM) error = %v", err)
	}
	reportPath := filepath.Join(root, "acceptance-report.yaml")
	writeSyncHealthProofTestReport(t, reportPath, syncHealthProofTestReportOptions{
		BOMFile:               bomPath,
		BOMName:               targetBOM.Metadata.Name,
		BOMRevision:           "rev-20240423",
		Status:                "Passed",
		ExitCode:              0,
		MutatingApply:         true,
		SourcePreflightState:  "Ready",
		RuntimePreflightState: "Ready",
		PostApplyState:        "Clean",
		Stages:                syncHealthProofTestStages(false),
	})

	buf := bytes.NewBuffer(nil)
	cmd := newSyncCmd()
	cmd.SetOut(buf)
	cmd.SetErr(buf)
	cmd.SetArgs([]string{
		"health-proof",
		"--file", bomPath,
		"--acceptance-report", reportPath,
	})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute() error = %v\noutput=%s", err, buf.String())
	}
	var out bom.DistributionHealthProof
	if err := yaml.Unmarshal(buf.Bytes(), &out); err != nil {
		t.Fatalf("Unmarshal(output) error = %v\noutput=%s", err, buf.String())
	}
	if out.Spec.Passed {
		t.Fatal("spec.passed = true, want false")
	}
	if syncHealthProofTestSignalPassed(out, "bom-identity") {
		t.Fatal("bom-identity signal passed, want failed")
	}
}

func TestSyncHealthProofCmdFailsWhenReportBOMIdentityMissing(t *testing.T) {
	root := t.TempDir()
	bomPath := filepath.Join(root, "bom.yaml")
	if err := yamlutil.MarshalFile(bomPath, testSyncBOM()); err != nil {
		t.Fatalf("MarshalFile(targetBOM) error = %v", err)
	}
	reportPath := filepath.Join(root, "acceptance-report.yaml")
	writeSyncHealthProofTestReport(t, reportPath, syncHealthProofTestReportOptions{
		BOMFile:               bomPath,
		SkipBOMIdentity:       true,
		Status:                "Passed",
		ExitCode:              0,
		MutatingApply:         true,
		SourcePreflightState:  "Ready",
		RuntimePreflightState: "Ready",
		PostApplyState:        "Clean",
		Stages:                syncHealthProofTestStages(false),
	})

	buf := bytes.NewBuffer(nil)
	cmd := newSyncCmd()
	cmd.SetOut(buf)
	cmd.SetErr(buf)
	cmd.SetArgs([]string{
		"health-proof",
		"--file", bomPath,
		"--acceptance-report", reportPath,
	})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute() error = %v\noutput=%s", err, buf.String())
	}
	var out bom.DistributionHealthProof
	if err := yaml.Unmarshal(buf.Bytes(), &out); err != nil {
		t.Fatalf("Unmarshal(output) error = %v\noutput=%s", err, buf.String())
	}
	if out.Spec.Passed {
		t.Fatal("spec.passed = true, want false")
	}
	if syncHealthProofTestSignalPassed(out, "bom-identity") {
		t.Fatal("bom-identity signal passed, want failed")
	}
}

func TestSyncHealthProofCmdFailsWhenReportBOMDigestDoesNotMatch(t *testing.T) {
	root := t.TempDir()
	bomPath := filepath.Join(root, "bom.yaml")
	targetBOM := testSyncBOM()
	if err := yamlutil.MarshalFile(bomPath, targetBOM); err != nil {
		t.Fatalf("MarshalFile(targetBOM) error = %v", err)
	}
	reportPath := filepath.Join(root, "acceptance-report.yaml")
	writeSyncHealthProofTestReport(t, reportPath, syncHealthProofTestReportOptions{
		BOMFile:               bomPath,
		BOMName:               targetBOM.Metadata.Name,
		BOMRevision:           targetBOM.Spec.Revision,
		BOMDigest:             "sha256:cccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccc",
		Status:                "Passed",
		ExitCode:              0,
		MutatingApply:         true,
		SourcePreflightState:  "Ready",
		RuntimePreflightState: "Ready",
		PostApplyState:        "Clean",
		Stages:                syncHealthProofTestStages(false),
	})

	buf := bytes.NewBuffer(nil)
	cmd := newSyncCmd()
	cmd.SetOut(buf)
	cmd.SetErr(buf)
	cmd.SetArgs([]string{
		"health-proof",
		"--file", bomPath,
		"--acceptance-report", reportPath,
	})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute() error = %v\noutput=%s", err, buf.String())
	}
	var out bom.DistributionHealthProof
	if err := yaml.Unmarshal(buf.Bytes(), &out); err != nil {
		t.Fatalf("Unmarshal(output) error = %v\noutput=%s", err, buf.String())
	}
	if out.Spec.Passed {
		t.Fatal("spec.passed = true, want false")
	}
	if syncHealthProofTestSignalPassed(out, "bom-digest") {
		t.Fatal("bom-digest signal passed, want failed")
	}
}

func TestSyncHealthProofCmdFailsWhenContractStageMissing(t *testing.T) {
	root := t.TempDir()
	bomPath := filepath.Join(root, "bom.yaml")
	if err := yamlutil.MarshalFile(bomPath, testSyncBOM()); err != nil {
		t.Fatalf("MarshalFile(targetBOM) error = %v", err)
	}
	reportPath := filepath.Join(root, "acceptance-report.yaml")
	writeSyncHealthProofTestReport(t, reportPath, syncHealthProofTestReportOptions{
		BOMFile:               bomPath,
		Status:                "Passed",
		ExitCode:              0,
		MutatingApply:         true,
		SourcePreflightState:  "Ready",
		RuntimePreflightState: "Ready",
		PostApplyState:        "Clean",
		Stages:                []syncHealthProofAcceptanceReportStage{{Name: "apply", Status: "Passed", Mutates: true}},
	})

	buf := bytes.NewBuffer(nil)
	cmd := newSyncCmd()
	cmd.SetOut(buf)
	cmd.SetErr(buf)
	cmd.SetArgs([]string{
		"health-proof",
		"--file", bomPath,
		"--acceptance-report", reportPath,
	})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute() error = %v\noutput=%s", err, buf.String())
	}
	var out bom.DistributionHealthProof
	if err := yaml.Unmarshal(buf.Bytes(), &out); err != nil {
		t.Fatalf("Unmarshal(output) error = %v\noutput=%s", err, buf.String())
	}
	if out.Spec.Passed {
		t.Fatal("spec.passed = true, want false")
	}
	if syncHealthProofTestSignalPassed(out, "contract/render") {
		t.Fatal("contract/render signal passed, want failed")
	}
}

func TestSyncHealthProofCmdFailsWhenRenderedStateIdentityMissing(t *testing.T) {
	for _, tt := range []struct {
		name       string
		mutateOpts func(*syncHealthProofTestReportOptions)
		signalName string
	}{
		{
			name: "missing desired state digest",
			mutateOpts: func(opts *syncHealthProofTestReportOptions) {
				opts.SkipDesiredStateDigest = true
			},
			signalName: "desired-state-digest",
		},
		{
			name: "invalid desired state digest",
			mutateOpts: func(opts *syncHealthProofTestReportOptions) {
				opts.DesiredStateDigest = "not-a-digest"
			},
			signalName: "desired-state-digest",
		},
		{
			name: "missing local repo revision",
			mutateOpts: func(opts *syncHealthProofTestReportOptions) {
				opts.SkipLocalRepoRevision = true
			},
			signalName: "local-repo-revision",
		},
		{
			name: "invalid local repo revision",
			mutateOpts: func(opts *syncHealthProofTestReportOptions) {
				opts.LocalRepoRevision = "not-a-digest"
			},
			signalName: "local-repo-revision",
		},
	} {
		t.Run(tt.name, func(t *testing.T) {
			root := t.TempDir()
			bomPath := filepath.Join(root, "bom.yaml")
			targetBOM := testSyncBOM()
			if err := yamlutil.MarshalFile(bomPath, targetBOM); err != nil {
				t.Fatalf("MarshalFile(targetBOM) error = %v", err)
			}
			bomData, err := os.ReadFile(bomPath)
			if err != nil {
				t.Fatalf("ReadFile(targetBOM) error = %v", err)
			}

			reportOpts := syncHealthProofTestReportOptions{
				BOMFile:               bomPath,
				BOMName:               targetBOM.Metadata.Name,
				BOMRevision:           targetBOM.Spec.Revision,
				BOMDigest:             digest.Canonical.FromBytes(bomData).String(),
				Status:                "Passed",
				ExitCode:              0,
				MutatingApply:         true,
				SourcePreflightState:  "Ready",
				RuntimePreflightState: "Ready",
				PostApplyState:        "Clean",
				Stages:                syncHealthProofTestStages(false),
			}
			tt.mutateOpts(&reportOpts)
			reportPath := filepath.Join(root, "acceptance-report.yaml")
			writeSyncHealthProofTestReport(t, reportPath, reportOpts)

			buf := bytes.NewBuffer(nil)
			cmd := newSyncCmd()
			cmd.SetOut(buf)
			cmd.SetErr(buf)
			cmd.SetArgs([]string{
				"health-proof",
				"--file", bomPath,
				"--acceptance-report", reportPath,
			})

			if err := cmd.Execute(); err != nil {
				t.Fatalf("Execute() error = %v\noutput=%s", err, buf.String())
			}
			var out bom.DistributionHealthProof
			if err := yaml.Unmarshal(buf.Bytes(), &out); err != nil {
				t.Fatalf("Unmarshal(output) error = %v\noutput=%s", err, buf.String())
			}
			if out.Spec.Passed {
				t.Fatal("spec.passed = true, want false")
			}
			if syncHealthProofTestSignalPassed(out, tt.signalName) {
				t.Fatalf("%s signal passed, want failed", tt.signalName)
			}
		})
	}
}

func TestSyncHealthProofCmdFailsWhenMutatingContractStageIsNotMarkedMutating(t *testing.T) {
	for _, tt := range []struct {
		name        string
		stageName   string
		revertCheck bool
	}{
		{
			name:      "apply",
			stageName: "apply",
		},
		{
			name:        "revert",
			stageName:   "revert-check-revert",
			revertCheck: true,
		},
	} {
		t.Run(tt.name, func(t *testing.T) {
			root := t.TempDir()
			bomPath := filepath.Join(root, "bom.yaml")
			targetBOM := testSyncBOM()
			if err := yamlutil.MarshalFile(bomPath, targetBOM); err != nil {
				t.Fatalf("MarshalFile(targetBOM) error = %v", err)
			}
			bomData, err := os.ReadFile(bomPath)
			if err != nil {
				t.Fatalf("ReadFile(targetBOM) error = %v", err)
			}
			stages := syncHealthProofTestStages(tt.revertCheck)
			for i := range stages {
				if stages[i].Name == tt.stageName {
					stages[i].Mutates = false
					break
				}
			}
			reportPath := filepath.Join(root, "acceptance-report.yaml")
			writeSyncHealthProofTestReport(t, reportPath, syncHealthProofTestReportOptions{
				BOMFile:               bomPath,
				BOMName:               targetBOM.Metadata.Name,
				BOMRevision:           targetBOM.Spec.Revision,
				BOMDigest:             digest.Canonical.FromBytes(bomData).String(),
				Status:                "Passed",
				ExitCode:              0,
				MutatingApply:         true,
				RevertCheck:           tt.revertCheck,
				SourcePreflightState:  "Ready",
				RuntimePreflightState: "Ready",
				PostApplyState:        "Clean",
				PostRevertState:       "Clean",
				Stages:                stages,
			})

			buf := bytes.NewBuffer(nil)
			cmd := newSyncCmd()
			cmd.SetOut(buf)
			cmd.SetErr(buf)
			cmd.SetArgs([]string{
				"health-proof",
				"--file", bomPath,
				"--acceptance-report", reportPath,
			})

			if err := cmd.Execute(); err != nil {
				t.Fatalf("Execute() error = %v\noutput=%s", err, buf.String())
			}
			var out bom.DistributionHealthProof
			if err := yaml.Unmarshal(buf.Bytes(), &out); err != nil {
				t.Fatalf("Unmarshal(output) error = %v\noutput=%s", err, buf.String())
			}
			if out.Spec.Passed {
				t.Fatal("spec.passed = true, want false")
			}
			if syncHealthProofTestSignalPassed(out, "contract/"+tt.stageName) {
				t.Fatalf("contract/%s signal passed, want failed", tt.stageName)
			}
		})
	}
}

func TestSyncHealthProofCmdFailsWhenPostApplyStateMissing(t *testing.T) {
	root := t.TempDir()
	bomPath := filepath.Join(root, "bom.yaml")
	if err := yamlutil.MarshalFile(bomPath, testSyncBOM()); err != nil {
		t.Fatalf("MarshalFile(targetBOM) error = %v", err)
	}
	reportPath := filepath.Join(root, "acceptance-report.yaml")
	writeSyncHealthProofTestReport(t, reportPath, syncHealthProofTestReportOptions{
		BOMFile:               bomPath,
		Status:                "Passed",
		ExitCode:              0,
		MutatingApply:         true,
		SourcePreflightState:  "Ready",
		RuntimePreflightState: "Ready",
		Stages:                syncHealthProofTestStages(false),
	})

	buf := bytes.NewBuffer(nil)
	cmd := newSyncCmd()
	cmd.SetOut(buf)
	cmd.SetErr(buf)
	cmd.SetArgs([]string{
		"health-proof",
		"--file", bomPath,
		"--acceptance-report", reportPath,
	})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute() error = %v\noutput=%s", err, buf.String())
	}
	var out bom.DistributionHealthProof
	if err := yaml.Unmarshal(buf.Bytes(), &out); err != nil {
		t.Fatalf("Unmarshal(output) error = %v\noutput=%s", err, buf.String())
	}
	if out.Spec.Passed {
		t.Fatal("spec.passed = true, want false")
	}
	if syncHealthProofTestSignalPassed(out, "post-apply-drift") {
		t.Fatal("post-apply-drift signal passed, want failed")
	}
}

func TestSyncHealthProofCmdRejectsInvalidCollectedAt(t *testing.T) {
	root := t.TempDir()
	bomPath := filepath.Join(root, "bom.yaml")
	if err := yamlutil.MarshalFile(bomPath, testSyncBOM()); err != nil {
		t.Fatalf("MarshalFile(targetBOM) error = %v", err)
	}
	reportPath := filepath.Join(root, "acceptance-report.yaml")
	writeSyncHealthProofTestReport(t, reportPath, syncHealthProofTestReportOptions{
		BOMFile:               bomPath,
		Status:                "Passed",
		ExitCode:              0,
		SourcePreflightState:  "Ready",
		RuntimePreflightState: "Ready",
	})

	cmd := newSyncCmd()
	cmd.SetArgs([]string{
		"health-proof",
		"--file", bomPath,
		"--acceptance-report", reportPath,
		"--collected-at", "not-rfc3339",
	})

	err := cmd.Execute()
	if err == nil {
		t.Fatal("Execute() error = nil, want invalid timestamp")
	}
	if !strings.Contains(err.Error(), "parse --collected-at as RFC3339") {
		t.Fatalf("Execute() error = %v, want collected-at parse error", err)
	}
}

func TestSyncApplyCmdWithRuntimeRootOverride(t *testing.T) {
	previousRuntimeRoot := constants.DefaultRuntimeRootDir
	defaultRuntimeRoot := t.TempDir()
	overrideRuntimeRoot := t.TempDir()
	constants.DefaultRuntimeRootDir = defaultRuntimeRoot
	t.Cleanup(func() {
		constants.DefaultRuntimeRootDir = previousRuntimeRoot
	})

	clusterName := uniqueSyncClusterName(t, "apply-runtime-root")
	bundleDir := t.TempDir()
	hostRoot := t.TempDir()

	bundle := &hydrate.Bundle{
		APIVersion: distribution.APIVersion,
		Kind:       distribution.KindHydratedBundle,
		Spec: hydrate.BundleSpec{
			BOMName:  "minimal-single-node",
			Revision: "rev-1",
			Channel:  bom.ChannelAlpha,
			Components: []hydrate.RenderedComponent{
				{
					Name:        "runtime",
					PackageName: "runtime-rootfs",
					Version:     "v1.0.0",
					Class:       packageformat.ClassRootfs,
					RootPath:    "components/runtime/files",
					Steps: []hydrate.RenderedStep{
						{
							Name:        "runtime-rootfs",
							Kind:        hydrate.StepContent,
							BundlePath:  "components/runtime/files/rootfs",
							SourcePath:  "rootfs",
							ContentType: packageformat.ContentRootfs,
						},
						{
							Name:        "runtime-config",
							Kind:        hydrate.StepContent,
							BundlePath:  "components/runtime/files/files/etc/demo/config.yaml",
							SourcePath:  "files/etc/demo/config.yaml",
							ContentType: packageformat.ContentFile,
						},
						{
							Name:           "healthcheck",
							Kind:           hydrate.StepHook,
							BundlePath:     "components/runtime/files/hooks/healthcheck.sh",
							SourcePath:     "hooks/healthcheck.sh",
							HookPhase:      packageformat.PhaseHealth,
							Target:         packageformat.TargetAllNodes,
							TimeoutSeconds: 5,
						},
					},
				},
			},
		},
	}
	if err := os.MkdirAll(filepath.Join(bundleDir, "components", "runtime", "files", "rootfs"), 0o755); err != nil {
		t.Fatalf("MkdirAll(rootfs) error = %v", err)
	}
	if err := os.MkdirAll(filepath.Join(bundleDir, "components", "runtime", "files", "files", "etc", "demo"), 0o755); err != nil {
		t.Fatalf("MkdirAll(files) error = %v", err)
	}
	if err := os.MkdirAll(filepath.Join(bundleDir, "components", "runtime", "files", "hooks"), 0o755); err != nil {
		t.Fatalf("MkdirAll(hooks) error = %v", err)
	}
	if err := os.WriteFile(filepath.Join(bundleDir, "components", "runtime", "files", "rootfs", "README"), []byte("runtime rootfs\n"), 0o644); err != nil {
		t.Fatalf("WriteFile(rootfs README) error = %v", err)
	}
	if err := os.WriteFile(filepath.Join(bundleDir, "components", "runtime", "files", "files", "etc", "demo", "config.yaml"), []byte("clusterName: default\n"), 0o644); err != nil {
		t.Fatalf("WriteFile(config) error = %v", err)
	}
	if err := os.WriteFile(filepath.Join(bundleDir, "components", "runtime", "files", "hooks", "healthcheck.sh"), []byte("#!/bin/sh\nexit 0\n"), 0o755); err != nil {
		t.Fatalf("WriteFile(healthcheck) error = %v", err)
	}
	if err := yamlutil.MarshalFile(filepath.Join(bundleDir, hydrate.BundleFileName), bundle); err != nil {
		t.Fatalf("MarshalFile(bundle) error = %v", err)
	}

	desiredStateDigest, err := hydrate.DigestBundle(bundleDir)
	if err != nil {
		t.Fatalf("DigestBundle() error = %v", err)
	}

	constants.DefaultRuntimeRootDir = overrideRuntimeRoot
	if _, err := state.PersistSuccessfulApply(clusterName, state.BOMReference{
		Name:     bundle.Spec.BOMName,
		Revision: bundle.Spec.Revision,
		Channel:  bundle.Spec.Channel,
	}, desiredStateDigest.String(), "local-rev-1", "patch-rev-1"); err != nil {
		t.Fatalf("PersistSuccessfulApply() error = %v", err)
	}
	constants.DefaultRuntimeRootDir = defaultRuntimeRoot

	buf := bytes.NewBuffer(nil)
	errBuf := bytes.NewBuffer(nil)
	cmd := newSyncCmd()
	cmd.SetOut(buf)
	cmd.SetErr(errBuf)
	cmd.SetArgs([]string{
		"apply",
		"--cluster", clusterName,
		"--bundle-dir", bundleDir,
		"--host-root", hostRoot,
		"--runtime-root", overrideRuntimeRoot,
	})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute() error = %v\nstdout=%s\nstderr=%s", err, buf.String(), errBuf.String())
	}

	var out syncApplyOutput
	if err := yaml.Unmarshal(buf.Bytes(), &out); err != nil {
		t.Fatalf("Unmarshal() error = %v\noutput=%s", err, buf.String())
	}
	if got, want := out.ClusterName, clusterName; got != want {
		t.Fatalf("out.ClusterName = %q, want %q", got, want)
	}
	if got, want := out.AppliedRevision, filepath.Join(overrideRuntimeRoot, clusterName, state.StoreDirName, state.AppliedRevisionFileName); got != want {
		t.Fatalf("out.AppliedRevision = %q, want %q", got, want)
	}
	if _, err := os.Stat(filepath.Join(hostRoot, "etc", "demo", "config.yaml")); err != nil {
		t.Fatalf("Stat(applied config) error = %v", err)
	}
}

func TestSyncApplyCmdDelegatesMultiNodeBundleToApplyEngine(t *testing.T) {
	previousRuntimeRoot := constants.DefaultRuntimeRootDir
	runtimeRoot := t.TempDir()
	constants.DefaultRuntimeRootDir = runtimeRoot
	t.Cleanup(func() {
		constants.DefaultRuntimeRootDir = previousRuntimeRoot
	})

	clusterName := uniqueSyncClusterName(t, "apply-delegates-multinode")
	bundleDir := t.TempDir()
	hostRoot := t.TempDir()
	kubeconfigPath := filepath.Join(t.TempDir(), "admin.conf")
	if err := os.WriteFile(kubeconfigPath, []byte("apiVersion: v1\nkind: Config\n"), 0o644); err != nil {
		t.Fatalf("WriteFile(kubeconfig) error = %v", err)
	}

	bundle := &hydrate.Bundle{
		APIVersion: distribution.APIVersion,
		Kind:       distribution.KindHydratedBundle,
		Spec: hydrate.BundleSpec{
			BOMName:  "multi-node-cli",
			Revision: "rev-1",
			Channel:  bom.ChannelAlpha,
			Components: []hydrate.RenderedComponent{{
				Name: "runtime",
				Steps: []hydrate.RenderedStep{{
					Name:        "runtime-rootfs",
					Kind:        hydrate.StepContent,
					BundlePath:  "components/runtime/files/rootfs",
					SourcePath:  "rootfs/",
					ContentType: packageformat.ContentRootfs,
				}},
			}},
		},
	}
	if err := yamlutil.MarshalFile(filepath.Join(bundleDir, hydrate.BundleFileName), bundle); err != nil {
		t.Fatalf("MarshalFile(bundle) error = %v", err)
	}

	var gotOptions reconcile.ApplyOptions
	previousApply := runSyncApply
	runSyncApply = func(opts reconcile.ApplyOptions) (*reconcile.ApplyResult, error) {
		gotOptions = opts
		return &reconcile.ApplyResult{
			Bundle:             bundle,
			BundlePath:         opts.BundlePath,
			DesiredStateDigest: "sha256:3333333333333333333333333333333333333333333333333333333333333333",
			AppliedRevision:    &state.AppliedRevision{},
		}, nil
	}
	t.Cleanup(func() {
		runSyncApply = previousApply
	})

	buf := bytes.NewBuffer(nil)
	errBuf := bytes.NewBuffer(nil)
	cmd := newSyncCmd()
	cmd.SetOut(buf)
	cmd.SetErr(errBuf)
	cmd.SetArgs([]string{
		"apply",
		"--cluster", clusterName,
		"--bundle-dir", bundleDir,
		"--host-root", hostRoot,
		"--kubeconfig", kubeconfigPath,
		"--runtime-root", runtimeRoot,
	})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute() error = %v\nstdout=%s\nstderr=%s", err, buf.String(), errBuf.String())
	}

	if got, want := gotOptions.ClusterName, clusterName; got != want {
		t.Fatalf("ApplyOptions.ClusterName = %q, want %q", got, want)
	}
	if got, want := gotOptions.BundlePath, bundleDir; got != want {
		t.Fatalf("ApplyOptions.BundlePath = %q, want %q", got, want)
	}
	if got, want := gotOptions.HostRoot, hostRoot; got != want {
		t.Fatalf("ApplyOptions.HostRoot = %q, want %q", got, want)
	}
	if got, want := gotOptions.KubeconfigPath, kubeconfigPath; got != want {
		t.Fatalf("ApplyOptions.KubeconfigPath = %q, want %q", got, want)
	}

	var out syncApplyOutput
	if err := yaml.Unmarshal(buf.Bytes(), &out); err != nil {
		t.Fatalf("Unmarshal() error = %v\noutput=%s", err, buf.String())
	}
	if got, want := out.ClusterName, clusterName; got != want {
		t.Fatalf("out.ClusterName = %q, want %q", got, want)
	}
	if got, want := out.BOMName, "multi-node-cli"; got != want {
		t.Fatalf("out.BOMName = %q, want %q", got, want)
	}
	if got, want := out.Warnings, []string{syncSourcePreflightMissingWarning}; strings.Join(got, "\n") != strings.Join(want, "\n") {
		t.Fatalf("out.Warnings = %#v, want %#v", got, want)
	}
}

func TestSyncPlanCmdSummarizesTargetsAndRedactsSecretContent(t *testing.T) {
	previousRuntimeRoot := constants.DefaultRuntimeRootDir
	runtimeRoot := t.TempDir()
	constants.DefaultRuntimeRootDir = runtimeRoot
	t.Cleanup(func() {
		constants.DefaultRuntimeRootDir = previousRuntimeRoot
	})

	clusterName := uniqueSyncClusterName(t, "plan")
	bundleDir := t.TempDir()
	bundle := &hydrate.Bundle{
		APIVersion: distribution.APIVersion,
		Kind:       distribution.KindHydratedBundle,
		Spec: hydrate.BundleSpec{
			BOMName:  "plan-cli",
			Revision: "rev-1",
			Channel:  bom.ChannelAlpha,
			ExecutionTopology: hydrate.ExecutionTopology{
				Source:      "clusterInventory",
				AllNodes:    []string{"10.0.0.10:22", "10.0.0.11:22"},
				FirstMaster: "10.0.0.10:22",
				HostRoles: []hydrate.ExecutionHostRoleList{
					{Host: "10.0.0.10:22", Roles: []string{v1beta1.MASTER}},
					{Host: "10.0.0.11:22", Roles: []string{v1beta1.NODE}},
				},
			},
			LocalResources: []string{"local-resources/grafana/admin-secret.yaml"},
			TrackedK8sObjects: []hydrate.TrackedK8sObject{
				{
					APIVersion: "v1",
					Kind:       "Secret",
					Namespace:  "observability",
					Name:       "grafana-admin-credentials",
					Path:       "local-resources/grafana/admin-secret.yaml",
					Source:     hydrate.InventorySourceLocalResource,
					Ownership:  hydrate.InventoryOwnershipLocal,
				},
				{
					APIVersion: "apps/v1",
					Kind:       "Deployment",
					Namespace:  "observability",
					Name:       "grafana",
					Path:       "components/grafana/files/manifests/grafana.yaml",
					Component:  "grafana",
					Source:     hydrate.InventorySourcePackageManifest,
					Ownership:  hydrate.InventoryOwnershipGlobal,
				},
			},
			TrackedHostPaths: []hydrate.TrackedHostPath{
				{
					HostPath:        "/etc/demo/config.yaml",
					BundlePath:      "components/runtime/files/files/etc/demo/config.yaml",
					Component:       "runtime",
					Source:          hydrate.InventorySourceLocalInput,
					Ownership:       hydrate.InventoryOwnershipLocal,
					Type:            hydrate.HostPathRegularFile,
					ProjectionClass: hydrate.HostPathProjectionClassDirect,
					CompareStrategy: hydrate.HostPathCompareStrategyBytewiseFile,
					InputName:       "runtime-config",
				},
				{
					HostPath:        "/etc/kubernetes/manifests/kube-apiserver.yaml",
					Component:       "kubernetes",
					Source:          hydrate.InventorySourceGeneratedHook,
					Ownership:       hydrate.InventoryOwnershipGlobal,
					Type:            hydrate.HostPathRegularFile,
					ProjectionClass: hydrate.HostPathProjectionClassGenerated,
					CompareStrategy: hydrate.HostPathCompareStrategySemanticGenerated,
					Generated:       &hydrate.GeneratedHostPathSemantics{Tool: "kubeadm"},
				},
			},
			Components: []hydrate.RenderedComponent{
				{
					Name:        "runtime",
					PackageName: "runtime-rootfs",
					Version:     "v0.1.0",
					Class:       packageformat.ClassRootfs,
					Steps: []hydrate.RenderedStep{
						{
							Name:        "runtime-config",
							Kind:        hydrate.StepContent,
							BundlePath:  "components/runtime/files/files/etc/demo/config.yaml",
							SourcePath:  "files/etc/demo/config.yaml",
							ContentType: packageformat.ContentFile,
							Required:    true,
						},
						{
							Name:           "bootstrap",
							Kind:           hydrate.StepHook,
							BundlePath:     "components/runtime/files/hooks/bootstrap.sh",
							SourcePath:     "hooks/bootstrap.sh",
							HookPhase:      packageformat.PhaseBootstrap,
							Target:         packageformat.TargetAllNodes,
							TimeoutSeconds: 5,
						},
						{
							Name:           "healthcheck",
							Kind:           hydrate.StepHook,
							BundlePath:     "components/runtime/files/hooks/healthcheck.sh",
							SourcePath:     "hooks/healthcheck.sh",
							HookPhase:      packageformat.PhaseHealth,
							Target:         packageformat.TargetFirstMaster,
							TimeoutSeconds: 5,
						},
					},
				},
				{
					Name:        "grafana",
					PackageName: "grafana",
					Version:     "v1.0.0",
					Class:       packageformat.ClassApplication,
					Steps: []hydrate.RenderedStep{
						{
							Name:        "grafana",
							Kind:        hydrate.StepContent,
							BundlePath:  "components/grafana/files/manifests/grafana.yaml",
							SourcePath:  "manifests/grafana.yaml",
							ContentType: packageformat.ContentManifest,
						},
						{
							Name:       "postinstall",
							Kind:       hydrate.StepHook,
							BundlePath: "components/grafana/files/hooks/postinstall.sh",
							SourcePath: "hooks/postinstall.sh",
							HookPhase:  packageformat.PhaseInstall,
							Target:     packageformat.TargetCluster,
						},
					},
				},
			},
		},
	}
	if err := yamlutil.MarshalFile(filepath.Join(bundleDir, hydrate.BundleFileName), bundle); err != nil {
		t.Fatalf("MarshalFile(bundle) error = %v", err)
	}

	buf := bytes.NewBuffer(nil)
	cmd := newSyncCmd()
	cmd.SetOut(buf)
	cmd.SetErr(buf)
	cmd.SetArgs([]string{
		"plan",
		"--cluster", clusterName,
		"--bundle-dir", bundleDir,
		"--runtime-root", runtimeRoot,
		"--allow-stale-topology",
	})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute() error = %v\noutput=%s", err, buf.String())
	}

	if strings.Contains(buf.String(), "passw0rd") || strings.Contains(buf.String(), "stringData") {
		t.Fatalf("plan output leaked secret payload:\n%s", buf.String())
	}

	var out syncPlanOutput
	if err := yaml.Unmarshal(buf.Bytes(), &out); err != nil {
		t.Fatalf("Unmarshal() error = %v\noutput=%s", err, buf.String())
	}
	if got, want := out.ClusterName, clusterName; got != want {
		t.Fatalf("out.ClusterName = %q, want %q", got, want)
	}
	if got, want := out.Summary.Components, 2; got != want {
		t.Fatalf("summary.components = %d, want %d", got, want)
	}
	if got, want := out.Summary.Steps, 5; got != want {
		t.Fatalf("summary.steps = %d, want %d", got, want)
	}
	if got, want := out.Summary.SecretObjects, 1; got != want {
		t.Fatalf("summary.secretObjects = %d, want %d", got, want)
	}
	if out.Summary.Blocked {
		t.Fatalf("summary.blocked = true, want false; reasons=%v", out.Preflight.BlockedReasons)
	}
	if got, want := out.Preflight.State, syncPreflightStateWarning; got != want {
		t.Fatalf("preflight.state = %q, want %q", got, want)
	}
	if got, want := out.Warnings, []string{syncSourcePreflightMissingWarning}; strings.Join(got, "\n") != strings.Join(want, "\n") {
		t.Fatalf("warnings = %#v, want %#v", got, want)
	}
	if got, want := out.ExecutionTopology.FirstMaster, "10.0.0.10:22"; got != want {
		t.Fatalf("executionTopology.firstMaster = %q, want %q", got, want)
	}
	if got, want := out.Components[0].Steps[0].Target.Effective, string(packageformat.TargetAllNodes); got != want {
		t.Fatalf("runtime config target.effective = %q, want %q", got, want)
	}
	if got, want := strings.Join(out.Components[0].Steps[0].Target.Hosts, ","), "10.0.0.10:22,10.0.0.11:22"; got != want {
		t.Fatalf("runtime config target.hosts = %q, want %q", got, want)
	}
	if got, want := out.Components[0].Steps[2].Target.Effective, string(packageformat.TargetFirstMaster); got != want {
		t.Fatalf("healthcheck target.effective = %q, want %q", got, want)
	}
	if got, want := strings.Join(out.Components[0].Steps[2].Target.Hosts, ","), "10.0.0.10:22"; got != want {
		t.Fatalf("healthcheck target.hosts = %q, want %q", got, want)
	}
	if got, want := out.Components[1].Steps[1].Target.Scope, "cluster"; got != want {
		t.Fatalf("postinstall target.scope = %q, want %q", got, want)
	}
	if got, want := len(out.LocalResources), 1; got != want {
		t.Fatalf("len(localResources) = %d, want %d", got, want)
	}
	if got, want := len(out.LocalResources[0].Objects), 1; got != want {
		t.Fatalf("len(localResources[0].objects) = %d, want %d", got, want)
	}
	if !out.LocalResources[0].Objects[0].Sensitive {
		t.Fatal("localResources[0].objects[0].sensitive = false, want true")
	}
	if got, want := len(out.TrackedHostPathSet), 2; got != want {
		t.Fatalf("len(trackedHostPathSets) = %d, want %d: %#v", got, want, out.TrackedHostPathSet)
	}
	generated := syncPlanHostPathSetByProjection(out.TrackedHostPathSet, string(hydrate.HostPathProjectionClassGenerated))
	if generated == nil {
		t.Fatalf("generated tracked host path set missing: %#v", out.TrackedHostPathSet)
	}
	if got, want := strings.Join(generated.Hosts, ","), "10.0.0.10:22"; got != want {
		t.Fatalf("generated host path hosts = %q, want %q", got, want)
	}
}

func TestSyncLocalRepoInitCmdCreatesValidSkeleton(t *testing.T) {
	previousRuntimeRoot := constants.DefaultRuntimeRootDir
	constants.DefaultRuntimeRootDir = t.TempDir()
	t.Cleanup(func() {
		constants.DefaultRuntimeRootDir = previousRuntimeRoot
	})

	packageRoot := filepath.Join(t.TempDir(), "runtime-rootfs")
	writeSyncTestPackage(t, packageRoot)
	writeSyncTestFile(t, filepath.Join(packageRoot, "package.yaml"), `apiVersion: distribution.sealos.io/v1alpha1
kind: ComponentPackage
metadata:
  name: runtime-rootfs
spec:
  component: runtime
  version: v0.1.0
  class: rootfs
  inputs:
    - name: runtime-config
      type: configFile
      path: files/etc/demo/config.yaml
      format: yaml
      required: true
    - name: admin-password
      type: configFile
      path: files/etc/demo/admin-password.txt
      required: true
  contents:
    - name: runtime-rootfs
      type: rootfs
      path: rootfs/
      required: true
    - name: runtime-config
      type: file
      path: files/etc/demo/config.yaml
      required: true
    - name: admin-password
      type: file
      path: files/etc/demo/admin-password.txt
      required: true
  hooks:
    - name: bootstrap
      phase: bootstrap
      target: allNodes
      path: hooks/bootstrap.sh
      timeoutSeconds: 5
`, 0o644)
	writeSyncTestFile(t, filepath.Join(packageRoot, "files", "etc", "demo", "admin-password.txt"), "package-default-password\n", 0o600)

	bomPath := filepath.Join(t.TempDir(), "bom.yaml")
	if err := yamlutil.MarshalFile(bomPath, syncLifecycleBOM()); err != nil {
		t.Fatalf("MarshalFile(bom) error = %v", err)
	}
	localRepoRoot := filepath.Join(t.TempDir(), "local-repo")

	buf := bytes.NewBuffer(nil)
	cmd := newSyncCmd()
	cmd.SetOut(buf)
	cmd.SetErr(buf)
	cmd.SetArgs([]string{
		"local-repo", "init",
		"--file", bomPath,
		"--package-source", "runtime=" + packageRoot,
		"--output-dir", localRepoRoot,
		"--output", "yaml",
	})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("local-repo init Execute() error = %v\noutput=%s", err, buf.String())
	}

	var out syncLocalRepoInitOutput
	if err := yaml.Unmarshal(buf.Bytes(), &out); err != nil {
		t.Fatalf("Unmarshal(init) error = %v\noutput=%s", err, buf.String())
	}
	if got, want := out.Summary.RequiredInputs, 2; got != want {
		t.Fatalf("summary.requiredInputs = %d, want %d", got, want)
	}
	if got, want := out.Summary.SecretHints, 1; got != want {
		t.Fatalf("summary.secretHints = %d, want %d", got, want)
	}
	if got, want := len(out.Inputs), 2; got != want {
		t.Fatalf("len(inputs) = %d, want %d", got, want)
	}
	for _, rel := range []string{
		"README.md",
		"repo.yaml",
		"revisions/current.yaml",
		"policy/local-patch-policy.yaml",
		"inputs/runtime/runtime-config.yaml",
		"inputs/runtime/admin-password.txt",
	} {
		if _, err := os.Stat(filepath.Join(localRepoRoot, rel)); err != nil {
			t.Fatalf("generated file %q missing: %v", rel, err)
		}
	}
	if _, err := os.Stat(filepath.Join(localRepoRoot, "resources", "secrets")); err != nil {
		t.Fatalf("resources/secrets dir missing: %v", err)
	}
	if _, err := os.Stat(filepath.Join(localRepoRoot, "resources", "secrets", "README.md")); !os.IsNotExist(err) {
		t.Fatalf("resources/secrets/README.md stat error = %v, want not exist so localrepo.Load does not treat it as a resource", err)
	}
	secretInfo, err := os.Stat(filepath.Join(localRepoRoot, "inputs", "runtime", "admin-password.txt"))
	if err != nil {
		t.Fatalf("Stat(admin-password input) error = %v", err)
	}
	if got, want := secretInfo.Mode().Perm(), os.FileMode(0o600); got != want {
		t.Fatalf("admin-password mode = %s, want %s", got, want)
	}

	repo, err := localrepo.Load(localRepoRoot)
	if err != nil {
		t.Fatalf("localrepo.Load() error = %v", err)
	}
	runtimeConfigBinding, ok := repo.BindingFor("runtime", packageformat.Input{Name: "runtime-config"})
	if !ok {
		t.Fatal("runtime-config binding missing")
	}
	if got, want := filepath.ToSlash(runtimeConfigBinding), filepath.ToSlash(filepath.Join(localRepoRoot, "inputs", "runtime", "runtime-config.yaml")); got != want {
		t.Fatalf("runtime-config binding = %q, want %q", got, want)
	}
	adminPasswordBinding, ok := repo.BindingFor("runtime", packageformat.Input{Name: "admin-password"})
	if !ok {
		t.Fatal("admin-password binding missing")
	}
	if got, want := filepath.ToSlash(adminPasswordBinding), filepath.ToSlash(filepath.Join(localRepoRoot, "inputs", "runtime", "admin-password.txt")); got != want {
		t.Fatalf("admin-password binding = %q, want %q", got, want)
	}
	if resources := repo.Resources(); len(resources) != 0 {
		t.Fatalf("repo.Resources() = %#v, want no resource files from skeleton", resources)
	}
	if repo.LocalPatchPolicy() == nil {
		t.Fatal("repo.LocalPatchPolicy() = nil, want generated default policy")
	}

	validateOut := runSyncValidate(syncValidateOptions{
		ClusterName:    "default",
		BOMPath:        bomPath,
		LocalRepoPath:  localRepoRoot,
		PackageSources: []string{"runtime=" + packageRoot},
	})
	if !validateOut.Passed {
		t.Fatalf("validate after init passed = false, issues=%#v", validateOut.Issues)
	}
	if got, want := validateOut.Summary.BoundInputs, 2; got != want {
		t.Fatalf("validate boundInputs = %d, want %d", got, want)
	}

	if err := os.WriteFile(filepath.Join(localRepoRoot, "inputs", "runtime", "runtime-config.yaml"), []byte("custom: true\n"), 0o644); err != nil {
		t.Fatalf("WriteFile(custom runtime-config) error = %v", err)
	}
	second, err := runSyncLocalRepoInit(syncLocalRepoInitOptions{
		BOMPath:        bomPath,
		OutputDir:      localRepoRoot,
		PackageSources: []string{"runtime=" + packageRoot},
		CreatedAt:      time.Unix(0, 0).UTC(),
	})
	if err != nil {
		t.Fatalf("second runSyncLocalRepoInit() error = %v", err)
	}
	if got := second.Summary.SkippedFiles; got == 0 {
		t.Fatalf("second summary.skippedFiles = %d, want existing files skipped", got)
	}
	data, err := os.ReadFile(filepath.Join(localRepoRoot, "inputs", "runtime", "runtime-config.yaml"))
	if err != nil {
		t.Fatalf("ReadFile(runtime-config after second init) error = %v", err)
	}
	if got, want := string(data), "custom: true\n"; got != want {
		t.Fatalf("runtime-config after second init = %q, want %q", got, want)
	}
	if err := os.WriteFile(filepath.Join(localRepoRoot, "inputs", "runtime", "admin-password.txt"), []byte("cluster-local-password\n"), 0o600); err != nil {
		t.Fatalf("WriteFile(admin-password after init) error = %v", err)
	}
	doctorOut := runSyncLocalRepoDoctor(syncLocalRepoDoctorOptions{
		BOMPath:        bomPath,
		LocalRepoPath:  localRepoRoot,
		PackageSources: []string{"runtime=" + packageRoot},
	})
	if !doctorOut.Passed {
		t.Fatalf("doctor after filling inputs passed = false, issues=%#v", doctorOut.Issues)
	}
	if got, want := doctorOut.Summary.RequiredInputs, 2; got != want {
		t.Fatalf("doctor requiredInputs = %d, want %d", got, want)
	}
	if got := doctorOut.Summary.PlaceholderHits; got != 0 {
		t.Fatalf("doctor placeholderHits = %d, want 0", got)
	}
}

func TestSyncLocalRepoDoctorCmdReportsLocalRepoIssues(t *testing.T) {
	previousRuntimeRoot := constants.DefaultRuntimeRootDir
	constants.DefaultRuntimeRootDir = t.TempDir()
	t.Cleanup(func() {
		constants.DefaultRuntimeRootDir = previousRuntimeRoot
	})

	packageRoot := filepath.Join(t.TempDir(), "runtime-rootfs")
	writeSyncTestPackage(t, packageRoot)
	writeSyncTestFile(t, filepath.Join(packageRoot, "package.yaml"), `apiVersion: distribution.sealos.io/v1alpha1
kind: ComponentPackage
metadata:
  name: runtime-rootfs
spec:
  component: runtime
  version: v0.1.0
  class: rootfs
  inputs:
    - name: runtime-config
      type: configFile
      path: files/etc/demo/config.yaml
      format: yaml
      required: true
    - name: admin-password
      type: configFile
      path: files/etc/demo/admin-password.txt
      required: true
  contents:
    - name: runtime-rootfs
      type: rootfs
      path: rootfs/
      required: true
    - name: runtime-config
      type: file
      path: files/etc/demo/config.yaml
      required: true
    - name: admin-password
      type: file
      path: files/etc/demo/admin-password.txt
      required: true
  hooks:
    - name: bootstrap
      phase: bootstrap
      target: allNodes
      path: hooks/bootstrap.sh
      timeoutSeconds: 5
`, 0o644)
	writeSyncTestFile(t, filepath.Join(packageRoot, "files", "etc", "demo", "admin-password.txt"), "package-default-password\n", 0o600)

	bomPath := filepath.Join(t.TempDir(), "bom.yaml")
	if err := yamlutil.MarshalFile(bomPath, syncLifecycleBOM()); err != nil {
		t.Fatalf("MarshalFile(bom) error = %v", err)
	}
	localRepoRoot := filepath.Join(t.TempDir(), "local-repo")
	placeholderInput := syncLocalRepoInitInput{
		Component: "runtime",
		Name:      "runtime-config",
		Type:      string(packageformat.InputConfigFile),
		Format:    "yaml",
		Required:  true,
		Path:      filepath.ToSlash(filepath.Join(localrepo.InputsDirName, "runtime", "runtime-config.yaml")),
	}
	writeSyncTestFile(t, filepath.Join(localRepoRoot, "inputs", "runtime", "runtime-config.yaml"), syncLocalRepoInputTemplate(placeholderInput), 0o644)
	writeSyncTestFile(t, filepath.Join(localRepoRoot, "inputs", "runtime", "admin-password.txt"), "super-secret-password\n", 0o644)
	writeSyncTestFile(t, filepath.Join(localRepoRoot, "resources", "secrets", "grafana-admin-secret.yaml"), `apiVersion: v1
kind: ConfigMap
metadata:
  name: grafana-admin
`, 0o644)
	writeSyncTestFile(t, filepath.Join(localRepoRoot, "resources", "notes.txt"), "operator note\n", 0o644)
	if err := os.MkdirAll(filepath.Join(localRepoRoot, "inputs", "old-component"), 0o755); err != nil {
		t.Fatalf("MkdirAll(stale inputs) error = %v", err)
	}
	if err := os.MkdirAll(filepath.Join(localRepoRoot, "patches", "old-component"), 0o755); err != nil {
		t.Fatalf("MkdirAll(stale patches) error = %v", err)
	}

	buf := bytes.NewBuffer(nil)
	cmd := newSyncCmd()
	cmd.SetOut(buf)
	cmd.SetErr(buf)
	cmd.SetArgs([]string{
		"local-repo", "doctor",
		"--file", bomPath,
		"--package-source", "runtime=" + packageRoot,
		"--local-repo", localRepoRoot,
		"--output", "yaml",
	})
	if err := cmd.Execute(); err == nil {
		t.Fatalf("local-repo doctor Execute() error = nil, want blocking issues\noutput=%s", buf.String())
	}
	if strings.Contains(buf.String(), "super-secret-password") {
		t.Fatalf("doctor output leaked secret input content:\n%s", buf.String())
	}

	var out syncLocalRepoDoctorOutput
	if err := yaml.Unmarshal(buf.Bytes(), &out); err != nil {
		t.Fatalf("Unmarshal(doctor) error = %v\noutput=%s", err, buf.String())
	}
	if out.Passed {
		t.Fatalf("doctor passed = true, want false; output=%#v", out)
	}
	if got, want := out.Summary.PlaceholderHits, 1; got != want {
		t.Fatalf("summary.placeholderHits = %d, want %d", got, want)
	}
	if out.Summary.Errors == 0 {
		t.Fatalf("summary.errors = 0, want blocking issues; output=%#v", out)
	}
	if out.Summary.Warnings == 0 {
		t.Fatalf("summary.warnings = 0, want warnings; output=%#v", out)
	}

	codes := syncLocalRepoDoctorIssueCodeSet(out.Issues)
	for _, code := range []string{
		"inputPlaceholder",
		"secretInputModeTooOpen",
		"secretResourceKindInvalid",
		"secretResourceModeTooOpen",
		"resourceNonManifestFile",
		"staleComponentDirectory",
		"localPatchPolicyMissing",
	} {
		if _, ok := codes[code]; !ok {
			t.Fatalf("doctor issue code %q missing; codes=%#v output=%#v", code, codes, out)
		}
	}
}

func syncLocalRepoDoctorIssueCodeSet(issues []syncLocalRepoDoctorIssue) map[string]struct{} {
	codes := make(map[string]struct{}, len(issues))
	for _, issue := range issues {
		codes[issue.Code] = struct{}{}
	}
	return codes
}

func TestSyncApplyCmdReportsStaleTopologySnapshot(t *testing.T) {
	previousRuntimeRoot := constants.DefaultRuntimeRootDir
	runtimeRoot := t.TempDir()
	constants.DefaultRuntimeRootDir = runtimeRoot
	t.Cleanup(func() {
		constants.DefaultRuntimeRootDir = previousRuntimeRoot
	})

	clusterName := uniqueSyncClusterName(t, "apply-stale-topology")
	bomPath := filepath.Join(t.TempDir(), "bom with spaces.yaml")
	localRepoPath := filepath.Join(t.TempDir(), "local repo")
	packageSourcePath := filepath.Join(t.TempDir(), "runtime source")
	writeSyncClusterInventory(t, clusterName, []v1beta1.Host{
		{
			IPS:   []string{"10.0.0.10:22", "10.0.0.11:22"},
			Roles: []string{v1beta1.MASTER},
		},
	})

	bundleDir := t.TempDir()
	bundle := &hydrate.Bundle{
		APIVersion: distribution.APIVersion,
		Kind:       distribution.KindHydratedBundle,
		Spec: hydrate.BundleSpec{
			BOMName:  "stale-topology-cli",
			Revision: "rev-1",
			Channel:  bom.ChannelAlpha,
			RenderProvenance: hydrate.RenderProvenance{
				BOMPath:            bomPath,
				LocalRepoPath:      localRepoPath,
				LocalPatchRevision: "patch-rev-1",
				PackageSources: []hydrate.RenderProvenancePackageSource{
					{Component: "runtime", Path: packageSourcePath},
				},
			},
			ExecutionTopology: hydrate.ExecutionTopology{
				Source:      "clusterInventory",
				AllNodes:    []string{"10.0.0.10:22"},
				FirstMaster: "10.0.0.10:22",
				HostRoles: []hydrate.ExecutionHostRoleList{
					{Host: "10.0.0.10:22", Roles: []string{v1beta1.MASTER}},
				},
			},
			Components: []hydrate.RenderedComponent{{Name: "runtime"}},
		},
	}
	if err := yamlutil.MarshalFile(filepath.Join(bundleDir, hydrate.BundleFileName), bundle); err != nil {
		t.Fatalf("MarshalFile(bundle) error = %v", err)
	}

	previousApply := runSyncApply
	applyCalls := 0
	runSyncApply = func(opts reconcile.ApplyOptions) (*reconcile.ApplyResult, error) {
		applyCalls++
		return &reconcile.ApplyResult{
			Bundle:             bundle,
			BundlePath:         opts.BundlePath,
			DesiredStateDigest: "sha256:4444444444444444444444444444444444444444444444444444444444444444",
			AppliedRevision:    &state.AppliedRevision{},
		}, nil
	}
	t.Cleanup(func() {
		runSyncApply = previousApply
	})

	buf := bytes.NewBuffer(nil)
	errBuf := bytes.NewBuffer(nil)
	cmd := newSyncCmd()
	cmd.SetOut(buf)
	cmd.SetErr(errBuf)
	cmd.SetArgs([]string{
		"apply",
		"--cluster", clusterName,
		"--bundle-dir", bundleDir,
		"--runtime-root", runtimeRoot,
	})
	if err := cmd.Execute(); err == nil {
		t.Fatalf("Execute() error = nil, want stale preflight error\nstdout=%s\nstderr=%s", buf.String(), errBuf.String())
	}
	if got, want := applyCalls, 0; got != want {
		t.Fatalf("runSyncApply calls = %d, want %d", got, want)
	}

	var out syncApplyOutput
	if err := yaml.Unmarshal(buf.Bytes(), &out); err != nil {
		t.Fatalf("Unmarshal() error = %v\noutput=%s", err, buf.String())
	}
	if got, want := out.TopologyStatus.State, syncTopologyStateStale; got != want {
		t.Fatalf("topologyStatus.state = %q, want %q", got, want)
	}
	if got, want := strings.Join(out.TopologyStatus.ChangedFields, ","), "allNodes,hostRoles"; got != want {
		t.Fatalf("topologyStatus.changedFields = %q, want %q", got, want)
	}
	if got, want := out.RenderInputStatus.State, syncRenderInputStateStale; got != want {
		t.Fatalf("renderInputStatus.state = %q, want %q", got, want)
	}
	var changedInputs []string
	for _, change := range out.RenderInputStatus.ChangedInputs {
		changedInputs = append(changedInputs, change.Name)
	}
	if got, want := strings.Join(changedInputs, ","), "bom,localRepo,packageSource:runtime"; got != want {
		t.Fatalf("renderInputStatus.changedInputs = %q, want %q", got, want)
	}
	if !out.Blocked {
		t.Fatal("blocked = false, want true")
	}
	if got, want := out.State, syncPreflightStateBlocked; got != want {
		t.Fatalf("state = %q, want %q", got, want)
	}
	if got, want := out.RecommendedAction, syncPreflightRecommendedActionRerender; got != want {
		t.Fatalf("recommendedAction = %q, want %q", got, want)
	}
	if out.Summary == "" {
		t.Fatal("summary is empty")
	}
	if !strings.Contains(out.RefreshCommand, "'sealos' 'sync' 'render'") {
		t.Fatalf("refreshCommand = %q, want sync render command", out.RefreshCommand)
	}
	for _, want := range []string{
		"bundle executionTopology is stale",
		"render inputs are stale",
	} {
		if got := strings.Join(out.BlockedReasons, "\n"); !strings.Contains(got, want) {
			t.Fatalf("blockedReasons = %#v, want substring %q", out.BlockedReasons, want)
		}
	}
	if got := errBuf.String(); !strings.Contains(got, "warning: bundle executionTopology differs") {
		t.Fatalf("stderr = %q, want topology stale warning", got)
	}
	if got := errBuf.String(); !strings.Contains(got, "warning: one or more render inputs differ") {
		t.Fatalf("stderr = %q, want render input stale warning", got)
	}
	if got := errBuf.String(); !strings.Contains(got, "error: sync apply blocked") {
		t.Fatalf("stderr = %q, want blocked error", got)
	}
	for _, want := range []string{
		"'--file' '" + bomPath + "'",
		"'--local-repo' '" + localRepoPath + "'",
		"'--local-patch-revision' 'patch-rev-1'",
		"'--package-source' 'runtime=" + packageSourcePath + "'",
	} {
		if got := out.TopologyStatus.RefreshCommand; !strings.Contains(got, want) {
			t.Fatalf("refreshCommand = %q, want substring %q", got, want)
		}
	}

	buf.Reset()
	errBuf.Reset()
	cmd = newSyncCmd()
	cmd.SetOut(buf)
	cmd.SetErr(errBuf)
	cmd.SetArgs([]string{
		"apply",
		"--cluster", clusterName,
		"--bundle-dir", bundleDir,
		"--runtime-root", runtimeRoot,
		"--allow-stale-topology",
		"--allow-stale-render-inputs",
	})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute() with allow stale error = %v\nstdout=%s\nstderr=%s", err, buf.String(), errBuf.String())
	}
	if got, want := applyCalls, 1; got != want {
		t.Fatalf("runSyncApply calls after allow stale = %d, want %d", got, want)
	}
	var allowedOut syncApplyOutput
	if err := yaml.Unmarshal(buf.Bytes(), &allowedOut); err != nil {
		t.Fatalf("Unmarshal(allowed) error = %v\noutput=%s", err, buf.String())
	}
	if allowedOut.Blocked {
		t.Fatal("allowed blocked = true, want false")
	}
	if got, want := allowedOut.State, syncPreflightStateWarning; got != want {
		t.Fatalf("allowed state = %q, want %q", got, want)
	}
	if got, want := allowedOut.RecommendedAction, syncPreflightRecommendedActionApplyWithOverride; got != want {
		t.Fatalf("allowed recommendedAction = %q, want %q", got, want)
	}
	if got, want := allowedOut.DesiredStateDigest, "sha256:4444444444444444444444444444444444444444444444444444444444444444"; got != want {
		t.Fatalf("allowed desiredStateDigest = %q, want %q", got, want)
	}
	if got, want := allowedOut.TopologyStatus.State, syncTopologyStateStale; got != want {
		t.Fatalf("allowed topologyStatus.state = %q, want %q", got, want)
	}
	if got, want := allowedOut.RenderInputStatus.State, syncRenderInputStateStale; got != want {
		t.Fatalf("allowed renderInputStatus.state = %q, want %q", got, want)
	}
}

func TestSyncPreflightCmdReportsApplyGateStatus(t *testing.T) {
	previousRuntimeRoot := constants.DefaultRuntimeRootDir
	runtimeRoot := t.TempDir()
	constants.DefaultRuntimeRootDir = runtimeRoot
	t.Cleanup(func() {
		constants.DefaultRuntimeRootDir = previousRuntimeRoot
	})

	clusterName := uniqueSyncClusterName(t, "preflight-stale")
	bomPath := filepath.Join(t.TempDir(), "bom.yaml")
	localRepoPath := filepath.Join(t.TempDir(), "local-repo")
	writeSyncClusterInventory(t, clusterName, []v1beta1.Host{
		{
			IPS:   []string{"10.0.0.10:22", "10.0.0.11:22"},
			Roles: []string{v1beta1.MASTER},
		},
	})

	bundleDir := t.TempDir()
	bundle := &hydrate.Bundle{
		APIVersion: distribution.APIVersion,
		Kind:       distribution.KindHydratedBundle,
		Spec: hydrate.BundleSpec{
			BOMName:  "preflight-stale",
			Revision: "rev-1",
			Channel:  bom.ChannelAlpha,
			RenderProvenance: hydrate.RenderProvenance{
				BOMPath:       bomPath,
				LocalRepoPath: localRepoPath,
			},
			ExecutionTopology: hydrate.ExecutionTopology{
				Source:      "clusterInventory",
				AllNodes:    []string{"10.0.0.10:22"},
				FirstMaster: "10.0.0.10:22",
				HostRoles: []hydrate.ExecutionHostRoleList{
					{Host: "10.0.0.10:22", Roles: []string{v1beta1.MASTER}},
				},
			},
			Components: []hydrate.RenderedComponent{{Name: "runtime"}},
		},
	}
	if err := yamlutil.MarshalFile(filepath.Join(bundleDir, hydrate.BundleFileName), bundle); err != nil {
		t.Fatalf("MarshalFile(bundle) error = %v", err)
	}

	buf := bytes.NewBuffer(nil)
	errBuf := bytes.NewBuffer(nil)
	cmd := newSyncCmd()
	cmd.SetOut(buf)
	cmd.SetErr(errBuf)
	cmd.SetArgs([]string{
		"preflight",
		"--cluster", clusterName,
		"--bundle-dir", bundleDir,
		"--runtime-root", runtimeRoot,
	})
	if err := cmd.Execute(); err == nil {
		t.Fatalf("preflight Execute() error = nil, want blocked error\nstdout=%s\nstderr=%s", buf.String(), errBuf.String())
	}
	var out syncPreflightOutput
	if err := yaml.Unmarshal(buf.Bytes(), &out); err != nil {
		t.Fatalf("Unmarshal(preflight) error = %v\noutput=%s", err, buf.String())
	}
	if !out.Blocked {
		t.Fatal("preflight blocked = false, want true")
	}
	if got, want := out.State, syncPreflightStateBlocked; got != want {
		t.Fatalf("preflight state = %q, want %q", got, want)
	}
	if got, want := out.RecommendedAction, syncPreflightRecommendedActionRerender; got != want {
		t.Fatalf("preflight recommendedAction = %q, want %q", got, want)
	}
	if out.Summary == "" {
		t.Fatal("preflight summary is empty")
	}
	if !strings.Contains(out.RefreshCommand, "'sealos' 'sync' 'render'") {
		t.Fatalf("preflight refreshCommand = %q, want sync render command", out.RefreshCommand)
	}
	if got, want := out.TopologyStatus.State, syncTopologyStateStale; got != want {
		t.Fatalf("preflight topologyStatus.state = %q, want %q", got, want)
	}
	if got, want := out.RenderInputStatus.State, syncRenderInputStateStale; got != want {
		t.Fatalf("preflight renderInputStatus.state = %q, want %q", got, want)
	}
	if got := errBuf.String(); !strings.Contains(got, "error: sync preflight blocked") {
		t.Fatalf("stderr = %q, want preflight blocked error", got)
	}

	buf.Reset()
	errBuf.Reset()
	cmd = newSyncCmd()
	cmd.SetOut(buf)
	cmd.SetErr(errBuf)
	cmd.SetArgs([]string{
		"preflight",
		"--cluster", clusterName,
		"--bundle-dir", bundleDir,
		"--runtime-root", runtimeRoot,
		"--allow-stale-topology",
		"--allow-stale-render-inputs",
	})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("preflight Execute() with allow stale error = %v\nstdout=%s\nstderr=%s", err, buf.String(), errBuf.String())
	}
	var allowedOut syncPreflightOutput
	if err := yaml.Unmarshal(buf.Bytes(), &allowedOut); err != nil {
		t.Fatalf("Unmarshal(allowed preflight) error = %v\noutput=%s", err, buf.String())
	}
	if allowedOut.Blocked {
		t.Fatal("allowed preflight blocked = true, want false")
	}
	if got, want := allowedOut.State, syncPreflightStateWarning; got != want {
		t.Fatalf("allowed preflight state = %q, want %q", got, want)
	}
	if got, want := allowedOut.RecommendedAction, syncPreflightRecommendedActionApplyWithOverride; got != want {
		t.Fatalf("allowed preflight recommendedAction = %q, want %q", got, want)
	}
	if got, want := allowedOut.TopologyStatus.State, syncTopologyStateStale; got != want {
		t.Fatalf("allowed preflight topologyStatus.state = %q, want %q", got, want)
	}
	if got, want := allowedOut.RenderInputStatus.State, syncRenderInputStateStale; got != want {
		t.Fatalf("allowed preflight renderInputStatus.state = %q, want %q", got, want)
	}

	previousRuntimePreflight := runSyncRuntimePreflight
	runSyncRuntimePreflight = func(opts syncRuntimePreflightOptions) syncRuntimePreflightOutput {
		out := syncRuntimePreflightOutput{
			HostRoot:       syncRuntimeHostRoot(opts.HostRoot),
			KubeconfigPath: opts.KubeconfigPath,
		}
		out.addCheck("privileges", syncRuntimePreflightCheckBlocked, "sync apply must run as root when mutating the real host root")
		out.finalize()
		return out
	}
	t.Cleanup(func() {
		runSyncRuntimePreflight = previousRuntimePreflight
	})

	buf.Reset()
	errBuf.Reset()
	cmd = newSyncCmd()
	cmd.SetOut(buf)
	cmd.SetErr(errBuf)
	cmd.SetArgs([]string{
		"preflight",
		"--cluster", clusterName,
		"--bundle-dir", bundleDir,
		"--runtime-root", runtimeRoot,
		"--allow-stale-topology",
		"--allow-stale-render-inputs",
	})
	if err := cmd.Execute(); err == nil {
		t.Fatalf("preflight Execute() with runtime block error = nil, want error\nstdout=%s\nstderr=%s", buf.String(), errBuf.String())
	}
	var runtimeBlockedOut syncPreflightOutput
	if err := yaml.Unmarshal(buf.Bytes(), &runtimeBlockedOut); err != nil {
		t.Fatalf("Unmarshal(runtime blocked preflight) error = %v\noutput=%s", err, buf.String())
	}
	if runtimeBlockedOut.RuntimeStatus == nil {
		t.Fatal("runtimeStatus = nil, want runtime preflight details")
	}
	if !runtimeBlockedOut.RuntimeStatus.Blocked {
		t.Fatal("runtimeStatus.blocked = false, want true")
	}
	if got, want := runtimeBlockedOut.RecommendedAction, syncPreflightRecommendedActionRerender; got != want {
		t.Fatalf("runtime blocked recommendedAction = %q, want %q", got, want)
	}
	if got := strings.Join(runtimeBlockedOut.BlockedReasons, "\n"); !strings.Contains(got, "sync apply must run as root") {
		t.Fatalf("blockedReasons = %#v, want runtime reason", runtimeBlockedOut.BlockedReasons)
	}

	buf.Reset()
	errBuf.Reset()
	runSyncRuntimePreflight = previousRuntimePreflight
	cmd = newSyncCmd()
	cmd.SetOut(buf)
	cmd.SetErr(errBuf)
	cmd.SetArgs([]string{
		"preflight",
		"--cluster", clusterName,
		"--bundle-dir", bundleDir,
		"--runtime-root", runtimeRoot,
		"--allow-stale-topology",
		"--allow-stale-render-inputs",
		"--output", "json",
	})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("preflight Execute() json error = %v\nstdout=%s\nstderr=%s", err, buf.String(), errBuf.String())
	}
	var jsonOut struct {
		ClusterName       string `json:"clusterName"`
		State             string `json:"state"`
		RecommendedAction string `json:"recommendedAction"`
		Blocked           bool   `json:"blocked"`
		TopologyStatus    struct {
			State string `json:"state"`
		} `json:"topologyStatus"`
		RenderInputStatus struct {
			State string `json:"state"`
		} `json:"renderInputStatus"`
	}
	if err := json.Unmarshal(buf.Bytes(), &jsonOut); err != nil {
		t.Fatalf("json.Unmarshal(preflight) error = %v\noutput=%s", err, buf.String())
	}
	if got, want := jsonOut.ClusterName, clusterName; got != want {
		t.Fatalf("json clusterName = %q, want %q", got, want)
	}
	if got, want := jsonOut.State, string(syncPreflightStateWarning); got != want {
		t.Fatalf("json state = %q, want %q", got, want)
	}
	if got, want := jsonOut.RecommendedAction, string(syncPreflightRecommendedActionApplyWithOverride); got != want {
		t.Fatalf("json recommendedAction = %q, want %q", got, want)
	}
	if jsonOut.Blocked {
		t.Fatal("json blocked = true, want false")
	}
	if got, want := jsonOut.TopologyStatus.State, string(syncTopologyStateStale); got != want {
		t.Fatalf("json topologyStatus.state = %q, want %q", got, want)
	}
	if got, want := jsonOut.RenderInputStatus.State, string(syncRenderInputStateStale); got != want {
		t.Fatalf("json renderInputStatus.state = %q, want %q", got, want)
	}

	buf.Reset()
	errBuf.Reset()
	cmd = newSyncCmd()
	cmd.SetOut(buf)
	cmd.SetErr(errBuf)
	cmd.SetArgs([]string{
		"preflight",
		"--cluster", clusterName,
		"--bundle-dir", bundleDir,
		"--runtime-root", runtimeRoot,
		"--allow-stale-topology",
		"--allow-stale-render-inputs",
		"--output", "xml",
	})
	if err := cmd.Execute(); err == nil {
		t.Fatalf("preflight Execute() invalid output error = nil, want error\nstdout=%s\nstderr=%s", buf.String(), errBuf.String())
	}
	if got := buf.String(); got != "" {
		t.Fatalf("invalid output stdout = %q, want empty", got)
	}
}

func TestSyncPreflightCmdReportsSourceReadiness(t *testing.T) {
	previousRuntimeRoot := constants.DefaultRuntimeRootDir
	constants.DefaultRuntimeRootDir = t.TempDir()
	t.Cleanup(func() {
		constants.DefaultRuntimeRootDir = previousRuntimeRoot
	})

	packageRoot := filepath.Join(t.TempDir(), "runtime-rootfs")
	writeSyncRequiredInputsTestPackage(t, packageRoot)

	bomPath := filepath.Join(t.TempDir(), "bom.yaml")
	if err := yamlutil.MarshalFile(bomPath, syncLifecycleBOM()); err != nil {
		t.Fatalf("MarshalFile(bom) error = %v", err)
	}
	localRepoRoot := filepath.Join(t.TempDir(), "local-repo")
	initOut, err := runSyncLocalRepoInit(syncLocalRepoInitOptions{
		BOMPath:        bomPath,
		OutputDir:      localRepoRoot,
		PackageSources: []string{"runtime=" + packageRoot},
		CreatedAt:      time.Unix(0, 0).UTC(),
	})
	if err != nil {
		t.Fatalf("runSyncLocalRepoInit() error = %v", err)
	}
	for _, input := range initOut.Inputs {
		content := "value: ready\n"
		mode := os.FileMode(0o644)
		if input.Name == "admin-password" {
			content = "cluster-local-password\n"
			mode = 0o600
		}
		writeSyncTestFile(t, filepath.Join(localRepoRoot, filepath.FromSlash(input.Path)), content, mode)
	}

	buf := bytes.NewBuffer(nil)
	errBuf := bytes.NewBuffer(nil)
	cmd := newSyncCmd()
	cmd.SetOut(buf)
	cmd.SetErr(errBuf)
	cmd.SetArgs([]string{
		"preflight",
		"--cluster", "default",
		"--file", bomPath,
		"--package-source", "runtime=" + packageRoot,
		"--local-repo", localRepoRoot,
		"--output", "yaml",
	})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("source preflight Execute() error = %v\nstdout=%s\nstderr=%s", err, buf.String(), errBuf.String())
	}

	var out syncSourcePreflightOutput
	if err := yaml.Unmarshal(buf.Bytes(), &out); err != nil {
		t.Fatalf("Unmarshal(source preflight) error = %v\noutput=%s", err, buf.String())
	}
	if out.Blocked {
		t.Fatalf("source preflight blocked = true, reasons=%#v", out.BlockedReasons)
	}
	if got, want := out.State, syncPreflightStateReady; got != want {
		t.Fatalf("source preflight state = %q, want %q", got, want)
	}
	if got, want := out.RecommendedAction, syncPreflightRecommendedActionRender; got != want {
		t.Fatalf("source preflight recommendedAction = %q, want %q", got, want)
	}
	if out.LocalRepoDoctor == nil {
		t.Fatal("source preflight localRepoDoctor = nil, want doctor output")
	}
	if got, want := out.Counts.RequiredInputs, 2; got != want {
		t.Fatalf("source preflight requiredInputs = %d, want %d", got, want)
	}
	if got, want := out.Counts.BoundInputs, 2; got != want {
		t.Fatalf("source preflight boundInputs = %d, want %d", got, want)
	}
	for _, want := range []string{
		"'sealos' 'sync' 'render'",
		"'--file' '" + bomPath + "'",
		"'--local-repo' '" + localRepoRoot + "'",
		"'--package-source' 'runtime=" + packageRoot + "'",
	} {
		if !strings.Contains(out.RenderCommand, want) {
			t.Fatalf("renderCommand = %q, want substring %q", out.RenderCommand, want)
		}
	}
}

func TestSyncPreflightCmdBlocksSourceReadinessFailures(t *testing.T) {
	previousRuntimeRoot := constants.DefaultRuntimeRootDir
	constants.DefaultRuntimeRootDir = t.TempDir()
	t.Cleanup(func() {
		constants.DefaultRuntimeRootDir = previousRuntimeRoot
	})

	packageRoot := filepath.Join(t.TempDir(), "runtime-rootfs")
	writeSyncRequiredInputsTestPackage(t, packageRoot)
	bomPath := filepath.Join(t.TempDir(), "bom.yaml")
	if err := yamlutil.MarshalFile(bomPath, syncLifecycleBOM()); err != nil {
		t.Fatalf("MarshalFile(bom) error = %v", err)
	}
	localRepoRoot := filepath.Join(t.TempDir(), "local-repo")
	writeSyncTestFile(t, filepath.Join(localRepoRoot, "inputs", "runtime", "runtime-config.yaml"), syncLocalRepoInputTemplate(syncLocalRepoInitInput{
		Component: "runtime",
		Name:      "runtime-config",
		Type:      string(packageformat.InputConfigFile),
		Format:    "yaml",
		Required:  true,
		Path:      filepath.ToSlash(filepath.Join(localrepo.InputsDirName, "runtime", "runtime-config.yaml")),
	}), 0o644)

	buf := bytes.NewBuffer(nil)
	errBuf := bytes.NewBuffer(nil)
	cmd := newSyncCmd()
	cmd.SetOut(buf)
	cmd.SetErr(errBuf)
	cmd.SetArgs([]string{
		"preflight",
		"--file", bomPath,
		"--package-source", "runtime=" + packageRoot,
		"--local-repo", localRepoRoot,
	})
	if err := cmd.Execute(); err == nil {
		t.Fatalf("source preflight Execute() error = nil, want blocking issues\nstdout=%s\nstderr=%s", buf.String(), errBuf.String())
	}

	var out syncSourcePreflightOutput
	if err := yaml.Unmarshal(buf.Bytes(), &out); err != nil {
		t.Fatalf("Unmarshal(blocked source preflight) error = %v\noutput=%s", err, buf.String())
	}
	if !out.Blocked {
		t.Fatal("source preflight blocked = false, want true")
	}
	if got, want := out.State, syncPreflightStateBlocked; got != want {
		t.Fatalf("source preflight state = %q, want %q", got, want)
	}
	if out.LocalRepoDoctor == nil {
		t.Fatal("source preflight localRepoDoctor = nil, want doctor output")
	}
	if out.Counts.DoctorErrors == 0 {
		t.Fatalf("source preflight doctorErrors = 0, want blocking doctor issues; output=%#v", out)
	}
	if out.Counts.ValidateErrors == 0 {
		t.Fatalf("source preflight validateErrors = 0, want blocking validate issues; output=%#v", out)
	}
	if !strings.Contains(errBuf.String(), "sync source preflight blocked") {
		t.Fatalf("stderr = %q, want source preflight blocked error", errBuf.String())
	}
}

func TestSyncPreflightCmdHandlesSourceModeWithoutLocalRepo(t *testing.T) {
	previousRuntimeRoot := constants.DefaultRuntimeRootDir
	constants.DefaultRuntimeRootDir = t.TempDir()
	t.Cleanup(func() {
		constants.DefaultRuntimeRootDir = previousRuntimeRoot
	})

	packageRoot := filepath.Join(t.TempDir(), "runtime-rootfs")
	writeSyncRequiredInputsTestPackage(t, packageRoot)
	bomPath := filepath.Join(t.TempDir(), "bom.yaml")
	if err := yamlutil.MarshalFile(bomPath, syncLifecycleBOM()); err != nil {
		t.Fatalf("MarshalFile(bom) error = %v", err)
	}

	buf := bytes.NewBuffer(nil)
	errBuf := bytes.NewBuffer(nil)
	cmd := newSyncCmd()
	cmd.SetOut(buf)
	cmd.SetErr(errBuf)
	cmd.SetArgs([]string{
		"preflight",
		"--file", bomPath,
		"--package-source", "runtime=" + packageRoot,
	})
	if err := cmd.Execute(); err == nil {
		t.Fatalf("source preflight without local repo error = nil, want required input failure\nstdout=%s\nstderr=%s", buf.String(), errBuf.String())
	}

	var out syncSourcePreflightOutput
	if err := yaml.Unmarshal(buf.Bytes(), &out); err != nil {
		t.Fatalf("Unmarshal(no repo source preflight) error = %v\noutput=%s", err, buf.String())
	}
	if !out.Blocked {
		t.Fatal("source preflight without local repo blocked = false, want true")
	}
	if out.LocalRepoDoctor != nil {
		t.Fatal("source preflight localRepoDoctor != nil, want nil when --local-repo is omitted")
	}
	if got, want := out.Counts.ValidateErrors, 2; got != want {
		t.Fatalf("source preflight validateErrors = %d, want %d", got, want)
	}
}

func TestSyncPreflightCmdRejectsMixedSourceAndBundleModes(t *testing.T) {
	buf := bytes.NewBuffer(nil)
	errBuf := bytes.NewBuffer(nil)
	cmd := newSyncCmd()
	cmd.SetOut(buf)
	cmd.SetErr(errBuf)
	cmd.SetArgs([]string{
		"preflight",
		"--file", filepath.Join(t.TempDir(), "bom.yaml"),
		"--bundle-dir", t.TempDir(),
	})
	if err := cmd.Execute(); err == nil {
		t.Fatalf("preflight mixed mode error = nil, want error\nstdout=%s\nstderr=%s", buf.String(), errBuf.String())
	}
	if got := buf.String(); got != "" {
		t.Fatalf("preflight mixed mode stdout = %q, want empty", got)
	}
}

func TestSyncRuntimePreflightReportsCustomHostRootWarnings(t *testing.T) {
	hostRoot := t.TempDir()
	for _, path := range []string{
		"/etc/kubernetes/admin.conf",
		"/usr/bin/containerd",
		"/usr/bin/kubeadm",
	} {
		if err := os.MkdirAll(filepath.Dir(syncRuntimePath(hostRoot, path)), 0o755); err != nil {
			t.Fatalf("MkdirAll(%s) error = %v", path, err)
		}
		if err := os.WriteFile(syncRuntimePath(hostRoot, path), []byte("existing"), 0o644); err != nil {
			t.Fatalf("WriteFile(%s) error = %v", path, err)
		}
	}

	out := defaultSyncRuntimePreflight(syncRuntimePreflightOptions{
		Bundle: &hydrate.Bundle{
			Spec: hydrate.BundleSpec{
				ExecutionTopology: hydrate.NewSingleNodeExecutionTopology(),
				LocalResources:    []string{"local/resources.yaml"},
				Components: []hydrate.RenderedComponent{{
					Name:        "kubernetes",
					PackageName: "kubernetes-rootfs",
					Steps: []hydrate.RenderedStep{{
						Name:        "kubernetes-rootfs",
						Kind:        hydrate.StepContent,
						ContentType: packageformat.ContentRootfs,
					}},
				}},
			},
		},
		HostRoot:       hostRoot,
		KubeconfigPath: "/etc/kubernetes/admin.conf",
	})

	if out.Blocked {
		t.Fatalf("runtime preflight blocked = true, want false; reasons=%#v", out.BlockedReasons)
	}
	if got, want := out.State, syncPreflightStateWarning; got != want {
		t.Fatalf("runtime preflight state = %q, want %q", got, want)
	}
	if out.Counts.Warnings == 0 {
		t.Fatalf("runtime preflight warnings = 0, want existing state warnings; checks=%#v", out.Checks)
	}
	if out.Counts.Skipped == 0 {
		t.Fatalf("runtime preflight skipped = 0, want live checks skipped for custom host root; checks=%#v", out.Checks)
	}
	if !syncRuntimePreflightHasCheck(out, "kubernetes-state", syncRuntimePreflightCheckWarning) {
		t.Fatalf("runtime checks = %#v, want kubernetes-state warning", out.Checks)
	}
	if !syncRuntimePreflightHasCheck(out, "systemd", syncRuntimePreflightCheckSkipped) {
		t.Fatalf("runtime checks = %#v, want systemd skipped", out.Checks)
	}
}

func TestSyncRuntimePreflightBlocksMissingClientForManifestOnlyBundle(t *testing.T) {
	out := defaultSyncRuntimePreflight(syncRuntimePreflightOptions{
		Bundle: &hydrate.Bundle{
			Spec: hydrate.BundleSpec{
				ExecutionTopology: hydrate.NewSingleNodeExecutionTopology(),
				Components: []hydrate.RenderedComponent{{
					Name:        "app",
					PackageName: "app",
					Steps: []hydrate.RenderedStep{{
						Name:        "manifest",
						Kind:        hydrate.StepContent,
						ContentType: packageformat.ContentManifest,
					}},
				}},
			},
		},
		HostRoot:       t.TempDir(),
		KubeconfigPath: "/missing/admin.conf",
	})

	if !out.Blocked {
		t.Fatalf("runtime preflight blocked = false, want true; checks=%#v", out.Checks)
	}
	if got, want := out.State, syncPreflightStateBlocked; got != want {
		t.Fatalf("runtime preflight state = %q, want %q", got, want)
	}
	if !syncRuntimePreflightHasCheck(out, "kubernetes-client", syncRuntimePreflightCheckBlocked) {
		t.Fatalf("runtime checks = %#v, want kubernetes-client block", out.Checks)
	}
}

func TestSyncTopologyStatusForBundle(t *testing.T) {
	previousRuntimeRoot := constants.DefaultRuntimeRootDir
	constants.DefaultRuntimeRootDir = t.TempDir()
	t.Cleanup(func() {
		constants.DefaultRuntimeRootDir = previousRuntimeRoot
	})

	clusterName := uniqueSyncClusterName(t, "topology-status")
	writeSyncClusterInventory(t, clusterName, []v1beta1.Host{
		{
			IPS:   []string{"10.0.0.10:22"},
			Roles: []string{v1beta1.MASTER},
		},
		{
			IPS:   []string{"10.0.0.12:22"},
			Roles: []string{v1beta1.NODE},
		},
	})

	matchedBundle := &hydrate.Bundle{
		Spec: hydrate.BundleSpec{
			ExecutionTopology: hydrate.ExecutionTopology{
				Source:      "clusterInventory",
				AllNodes:    []string{"10.0.0.10:22", "10.0.0.12:22"},
				FirstMaster: "10.0.0.10:22",
				HostRoles: []hydrate.ExecutionHostRoleList{
					{Host: "10.0.0.10:22", Roles: []string{v1beta1.MASTER}},
					{Host: "10.0.0.12:22", Roles: []string{v1beta1.NODE}},
				},
			},
		},
	}
	if status := syncTopologyStatusForBundle(clusterName, matchedBundle); status.State != syncTopologyStateMatched {
		t.Fatalf("matched status = %q, want %q; message=%s", status.State, syncTopologyStateMatched, status.Message)
	}

	staleBundle := &hydrate.Bundle{
		Spec: hydrate.BundleSpec{
			ExecutionTopology: hydrate.ExecutionTopology{
				Source:      "clusterInventory",
				AllNodes:    []string{"10.0.0.10:22"},
				FirstMaster: "10.0.0.10:22",
				HostRoles: []hydrate.ExecutionHostRoleList{
					{Host: "10.0.0.10:22", Roles: []string{v1beta1.MASTER}},
				},
			},
		},
	}
	stale := syncTopologyStatusForBundle(clusterName, staleBundle)
	if got, want := stale.State, syncTopologyStateStale; got != want {
		t.Fatalf("stale status = %q, want %q", got, want)
	}
	if got, want := strings.Join(stale.ChangedFields, ","), "allNodes,hostRoles"; got != want {
		t.Fatalf("stale changedFields = %q, want %q", got, want)
	}

	missing := syncTopologyStatusForBundle(clusterName, &hydrate.Bundle{})
	if got, want := missing.State, syncTopologyStateMissing; got != want {
		t.Fatalf("missing status = %q, want %q", got, want)
	}
}

func syncRuntimePreflightHasCheck(out syncRuntimePreflightOutput, name string, state syncRuntimePreflightCheckState) bool {
	for _, check := range out.Checks {
		if check.Name == name && check.State == state {
			return true
		}
	}
	return false
}

func TestSyncRenderInputStatusForBundle(t *testing.T) {
	bomData := []byte("name: render-input-status\n")
	bomPath := filepath.Join(t.TempDir(), "bom.yaml")
	if err := os.WriteFile(bomPath, bomData, 0o644); err != nil {
		t.Fatalf("WriteFile(bom) error = %v", err)
	}

	localRepoPath := t.TempDir()
	writeSyncLocalInput(t, localRepoPath, "runtime", "runtime-config.yaml", "featureGate: enabled\n")
	repo, err := localrepo.Load(localRepoPath)
	if err != nil {
		t.Fatalf("Load(local repo) error = %v", err)
	}

	packageSourcePath := filepath.Join(t.TempDir(), "runtime-source")
	if err := os.MkdirAll(packageSourcePath, 0o755); err != nil {
		t.Fatalf("MkdirAll(package source) error = %v", err)
	}
	if err := os.WriteFile(filepath.Join(packageSourcePath, "package.yaml"), []byte("apiVersion: distribution.sealos.io/v1alpha1\n"), 0o644); err != nil {
		t.Fatalf("WriteFile(package source) error = %v", err)
	}
	packageSourceDigest, err := hydrate.DigestDirectory(packageSourcePath)
	if err != nil {
		t.Fatalf("DigestDirectory(package source) error = %v", err)
	}

	newBundle := func() *hydrate.Bundle {
		return &hydrate.Bundle{
			Spec: hydrate.BundleSpec{
				RenderProvenance: hydrate.RenderProvenance{
					BOMPath:           bomPath,
					BOMDigest:         digest.Canonical.FromBytes(bomData).String(),
					LocalRepoPath:     localRepoPath,
					LocalRepoRevision: repo.Revision,
					PackageSources: []hydrate.RenderProvenancePackageSource{
						{Component: "runtime", Path: packageSourcePath, Digest: packageSourceDigest.String()},
					},
				},
			},
		}
	}

	matched := syncRenderInputStatusForBundle(newBundle())
	if got, want := matched.State, syncRenderInputStateMatched; got != want {
		t.Fatalf("matched state = %q, want %q; changes=%#v", got, want, matched.ChangedInputs)
	}
	if got, want := matched.Provenance.LocalRepoRevision, repo.Revision; got != want {
		t.Fatalf("matched provenance localRepoRevision = %q, want %q", got, want)
	}

	missing := syncRenderInputStatusForBundle(&hydrate.Bundle{})
	if got, want := missing.State, syncRenderInputStateMissing; got != want {
		t.Fatalf("missing state = %q, want %q", got, want)
	}

	if err := os.WriteFile(bomPath, []byte("name: changed\n"), 0o644); err != nil {
		t.Fatalf("WriteFile(changed bom) error = %v", err)
	}
	bomStale := syncRenderInputStatusForBundle(newBundle())
	bomChange, ok := syncRenderInputChangeByName(bomStale.ChangedInputs, "bom")
	if !ok {
		t.Fatalf("bom stale changes = %#v, want bom change", bomStale.ChangedInputs)
	}
	if got, want := bomStale.State, syncRenderInputStateStale; got != want {
		t.Fatalf("bom stale state = %q, want %q", got, want)
	}
	if got, want := bomChange.Reason, "digest mismatch"; got != want {
		t.Fatalf("bom change reason = %q, want %q", got, want)
	}

	if err := os.WriteFile(bomPath, bomData, 0o644); err != nil {
		t.Fatalf("WriteFile(restored bom) error = %v", err)
	}
	writeSyncLocalInput(t, localRepoPath, "runtime", "runtime-config.yaml", "featureGate: disabled\n")
	localRepoStale := syncRenderInputStatusForBundle(newBundle())
	localRepoChange, ok := syncRenderInputChangeByName(localRepoStale.ChangedInputs, "localRepo")
	if !ok {
		t.Fatalf("local repo stale changes = %#v, want localRepo change", localRepoStale.ChangedInputs)
	}
	if got, want := localRepoStale.State, syncRenderInputStateStale; got != want {
		t.Fatalf("local repo stale state = %q, want %q", got, want)
	}
	if got, want := localRepoChange.Reason, "revision mismatch"; got != want {
		t.Fatalf("local repo change reason = %q, want %q", got, want)
	}

	repo, err = localrepo.Load(localRepoPath)
	if err != nil {
		t.Fatalf("Load(updated local repo) error = %v", err)
	}
	packageSourceStale := syncRenderInputStatusForBundle(newBundle())
	if got, want := packageSourceStale.State, syncRenderInputStateMatched; got != want {
		t.Fatalf("package source pre-change state = %q, want %q; changes=%#v", got, want, packageSourceStale.ChangedInputs)
	}
	if err := os.WriteFile(filepath.Join(packageSourcePath, "package.yaml"), []byte("apiVersion: distribution.sealos.io/v1alpha1\nkind: ComponentPackage\n"), 0o644); err != nil {
		t.Fatalf("WriteFile(changed package source) error = %v", err)
	}
	packageSourceStale = syncRenderInputStatusForBundle(newBundle())
	packageSourceChange, ok := syncRenderInputChangeByName(packageSourceStale.ChangedInputs, "packageSource:runtime")
	if !ok {
		t.Fatalf("package source stale changes = %#v, want packageSource:runtime change", packageSourceStale.ChangedInputs)
	}
	if got, want := packageSourceStale.State, syncRenderInputStateStale; got != want {
		t.Fatalf("package source stale state = %q, want %q", got, want)
	}
	if got, want := packageSourceChange.Reason, "digest mismatch"; got != want {
		t.Fatalf("package source change reason = %q, want %q", got, want)
	}
	packageSourceDigest, err = hydrate.DigestDirectory(packageSourcePath)
	if err != nil {
		t.Fatalf("DigestDirectory(updated package source) error = %v", err)
	}
	if err := os.RemoveAll(packageSourcePath); err != nil {
		t.Fatalf("RemoveAll(package source) error = %v", err)
	}
	packageSourceStale = syncRenderInputStatusForBundle(newBundle())
	if got, want := packageSourceStale.State, syncRenderInputStateStale; got != want {
		t.Fatalf("package source stale state = %q, want %q", got, want)
	}
	if _, ok := syncRenderInputChangeByName(packageSourceStale.ChangedInputs, "packageSource:runtime"); !ok {
		t.Fatalf("package source stale changes = %#v, want packageSource:runtime change", packageSourceStale.ChangedInputs)
	}
}

func TestSyncPackageLifecycleAcceptanceWithLocalRepoInputsAndSecret(t *testing.T) {
	previousRuntimeRoot := constants.DefaultRuntimeRootDir
	runtimeRoot := t.TempDir()
	constants.DefaultRuntimeRootDir = runtimeRoot
	t.Cleanup(func() {
		constants.DefaultRuntimeRootDir = previousRuntimeRoot
	})

	previousOutput := outputSyncKubectl
	liveSecret := `{"apiVersion":"v1","kind":"Secret","metadata":{"name":"grafana-admin-credentials","namespace":"observability","resourceVersion":"1"},"type":"Opaque","data":{"username":"YWRtaW4=","password":"cGFzc3cwcmQ="}}`
	outputSyncKubectl = func(args ...string) ([]byte, error) {
		command := strings.Join(args, " ")
		switch {
		case strings.Contains(command, "Secret grafana-admin-credentials"):
			return []byte(liveSecret), nil
		default:
			t.Fatalf("unexpected kubectl invocation: %s", command)
			return nil, nil
		}
	}
	t.Cleanup(func() {
		outputSyncKubectl = previousOutput
	})

	previousRunner := runSyncPackageSubcommand
	var packageBuildArgs [][]string
	runSyncPackageSubcommand = func(args []string, _ io.Writer) error {
		packageBuildArgs = append(packageBuildArgs, append([]string(nil), args...))
		return nil
	}
	t.Cleanup(func() {
		runSyncPackageSubcommand = previousRunner
	})

	kubectlDir := filepath.Join(t.TempDir(), "bin")
	writeSyncTestExecutable(t, filepath.Join(kubectlDir, "kubectl"), `#!/bin/sh
if [ "$1" = "--kubeconfig" ]; then
  shift 2
fi
if [ "$1" = "get" ] && [ "$2" = "--raw=/readyz" ]; then
  exit 0
fi
if [ "$1" = "get" ] && [ "$2" = "nodes" ]; then
  printf 'node-a'
  exit 0
fi
if [ "$1" = "apply" ]; then
  exit 0
fi
if [ "$1" = "taint" ]; then
  exit 0
fi
exit 1
`)
	t.Setenv("PATH", kubectlDir+":"+os.Getenv("PATH"))

	clusterName := uniqueSyncClusterName(t, "lifecycle-acceptance")
	hostRoot := t.TempDir()
	localRepoRoot := t.TempDir()
	packageRoot := filepath.Join(t.TempDir(), "runtime-rootfs")
	bomPath := filepath.Join(t.TempDir(), "bom.yaml")
	kubeconfigPath := filepath.Join(t.TempDir(), "admin.conf")
	if err := os.WriteFile(kubeconfigPath, []byte("apiVersion: v1\nkind: Config\n"), 0o644); err != nil {
		t.Fatalf("WriteFile(kubeconfig) error = %v", err)
	}

	renderedInput := "clusterName: rendered\nfeatureGate: enabled\n"
	committedInput := "clusterName: committed\nfeatureGate: enabled\n"
	discardedDrift := "clusterName: discarded\nfeatureGate: enabled\n"
	secret := "apiVersion: v1\nkind: Secret\nmetadata:\n  name: grafana-admin-credentials\n  namespace: observability\ntype: Opaque\nstringData:\n  username: admin\n  password: passw0rd\n"

	writeSyncTestPackage(t, packageRoot)
	writeSyncTestFile(t, filepath.Join(localRepoRoot, localrepo.InputsDirName, "runtime", "runtime-config.yaml"), renderedInput, 0o644)
	writeSyncTestFile(t, filepath.Join(localRepoRoot, localrepo.ResourcesDirName, "grafana", "admin-secret.yaml"), secret, 0o600)
	if err := yamlutil.MarshalFile(bomPath, syncLifecycleBOM()); err != nil {
		t.Fatalf("MarshalFile(bom) error = %v", err)
	}

	inspectBuf := bytes.NewBuffer(nil)
	inspectCmd := newSyncCmd()
	inspectCmd.SetOut(inspectBuf)
	inspectCmd.SetErr(inspectBuf)
	inspectCmd.SetArgs([]string{
		"package", "inspect",
		"--package-dir", packageRoot,
	})
	if err := inspectCmd.Execute(); err != nil {
		t.Fatalf("package inspect Execute() error = %v\noutput=%s", err, inspectBuf.String())
	}
	var inspectOut syncPackageBuildOutput
	if err := yaml.Unmarshal(inspectBuf.Bytes(), &inspectOut); err != nil {
		t.Fatalf("Unmarshal(package inspect) error = %v\noutput=%s", err, inspectBuf.String())
	}
	if got, want := inspectOut.PackageName, "runtime-rootfs"; got != want {
		t.Fatalf("package inspect packageName = %q, want %q", got, want)
	}
	if got, want := inspectOut.PackageComponent, "runtime"; got != want {
		t.Fatalf("package inspect packageComponent = %q, want %q", got, want)
	}

	buildBuf := bytes.NewBuffer(nil)
	buildCmd := newSyncCmd()
	buildCmd.SetOut(buildBuf)
	buildCmd.SetErr(buildBuf)
	buildCmd.SetArgs([]string{
		"package", "build",
		"--package-dir", packageRoot,
		"--image", "registry.example.io/sealos/runtime-rootfs:v0.1.0",
	})
	if err := buildCmd.Execute(); err != nil {
		t.Fatalf("package build Execute() error = %v\noutput=%s", err, buildBuf.String())
	}
	var buildOut syncPackageBuildOutput
	if err := yaml.Unmarshal(buildBuf.Bytes(), &buildOut); err != nil {
		t.Fatalf("Unmarshal(package build) error = %v\noutput=%s", err, buildBuf.String())
	}
	if got, want := buildOut.Image, "registry.example.io/sealos/runtime-rootfs:v0.1.0"; got != want {
		t.Fatalf("package build image = %q, want %q", got, want)
	}
	if got, want := len(packageBuildArgs), 2; got != want {
		t.Fatalf("package build runner calls = %d, want %d: %#v", got, want, packageBuildArgs)
	}
	if got, want := packageBuildArgs[0][0], "build"; got != want {
		t.Fatalf("package build runner first command = %q, want %q", got, want)
	}
	if got, want := packageBuildArgs[1][0], "inspect"; got != want {
		t.Fatalf("package build runner second command = %q, want %q", got, want)
	}

	renderBuf := bytes.NewBuffer(nil)
	renderCmd := newSyncCmd()
	renderCmd.SetOut(renderBuf)
	renderCmd.SetErr(renderBuf)
	renderCmd.SetArgs([]string{
		"render",
		"--file", bomPath,
		"--cluster", clusterName,
		"--local-repo", localRepoRoot,
		"--package-source", "runtime=" + packageRoot,
		"--runtime-root", runtimeRoot,
	})
	if err := renderCmd.Execute(); err != nil {
		t.Fatalf("sync render Execute() error = %v\noutput=%s", err, renderBuf.String())
	}
	var renderOut syncRenderOutput
	if err := yaml.Unmarshal(renderBuf.Bytes(), &renderOut); err != nil {
		t.Fatalf("Unmarshal(render) error = %v\noutput=%s", err, renderBuf.String())
	}
	if got, want := renderOut.ClusterName, clusterName; got != want {
		t.Fatalf("render clusterName = %q, want %q", got, want)
	}
	bundleDir := renderOut.BundlePath
	if bundleDir == "" {
		t.Fatal("render bundlePath is empty")
	}

	renderedBundleInputPath := filepath.Join(bundleDir, "components", "runtime", "files", "files", "etc", "demo", "config.yaml")
	renderedBundleSecretPath := filepath.Join(bundleDir, "local-resources", "grafana", "admin-secret.yaml")
	for path, want := range map[string]string{
		renderedBundleInputPath:  renderedInput,
		renderedBundleSecretPath: secret,
	} {
		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("ReadFile(%q) error = %v", path, err)
		}
		if got := string(data); got != want {
			t.Fatalf("%q content = %q, want %q", path, got, want)
		}
	}

	applyBuf := bytes.NewBuffer(nil)
	applyCmd := newSyncCmd()
	applyCmd.SetOut(applyBuf)
	applyCmd.SetErr(applyBuf)
	applyCmd.SetArgs([]string{
		"apply",
		"--cluster", clusterName,
		"--bundle-dir", bundleDir,
		"--host-root", hostRoot,
		"--kubeconfig", kubeconfigPath,
		"--runtime-root", runtimeRoot,
	})
	if err := applyCmd.Execute(); err != nil {
		t.Fatalf("sync apply Execute() error = %v\noutput=%s", err, applyBuf.String())
	}
	liveHostPath := filepath.Join(hostRoot, "etc", "demo", "config.yaml")
	data, err := os.ReadFile(liveHostPath)
	if err != nil {
		t.Fatalf("ReadFile(live config) error = %v", err)
	}
	if got, want := string(data), renderedInput; got != want {
		t.Fatalf("live config after apply = %q, want %q", got, want)
	}

	if err := os.WriteFile(liveHostPath, []byte(committedInput), 0o644); err != nil {
		t.Fatalf("WriteFile(committed drift) error = %v", err)
	}
	statusBuf := bytes.NewBuffer(nil)
	statusCmd := newSyncCmd()
	statusCmd.SetOut(statusBuf)
	statusCmd.SetErr(statusBuf)
	statusCmd.SetArgs([]string{
		"status",
		"--cluster", clusterName,
		"--bundle-dir", bundleDir,
		"--host-root", hostRoot,
		"--kubeconfig", kubeconfigPath,
		"--runtime-root", runtimeRoot,
	})
	if err := statusCmd.Execute(); err != nil {
		t.Fatalf("sync status Execute() error = %v\noutput=%s", err, statusBuf.String())
	}
	var statusOut struct {
		CurrentState   string `yaml:"currentState"`
		DirtyHostPaths []struct {
			Path           string `yaml:"path"`
			InputName      string `yaml:"inputName"`
			OperatorAction string `yaml:"operatorAction"`
		} `yaml:"dirtyHostPaths"`
	}
	if err := yaml.Unmarshal(statusBuf.Bytes(), &statusOut); err != nil {
		t.Fatalf("Unmarshal(status) error = %v\noutput=%s", err, statusBuf.String())
	}
	if got, want := statusOut.CurrentState, "Dirty"; got != want {
		t.Fatalf("status currentState = %q, want %q", got, want)
	}
	if got, want := len(statusOut.DirtyHostPaths), 1; got != want {
		t.Fatalf("status dirtyHostPaths = %d, want %d\noutput=%s", got, want, statusBuf.String())
	}
	if got, want := statusOut.DirtyHostPaths[0].Path, "/etc/demo/config.yaml"; got != want {
		t.Fatalf("dirtyHostPaths[0].path = %q, want %q", got, want)
	}
	if got, want := statusOut.DirtyHostPaths[0].InputName, "runtime-config"; got != want {
		t.Fatalf("dirtyHostPaths[0].inputName = %q, want %q", got, want)
	}
	if got, want := statusOut.DirtyHostPaths[0].OperatorAction, "commitOrReapplyLocalInput"; got != want {
		t.Fatalf("dirtyHostPaths[0].operatorAction = %q, want %q", got, want)
	}

	commitBuf := bytes.NewBuffer(nil)
	commitCmd := newSyncCmd()
	commitCmd.SetOut(commitBuf)
	commitCmd.SetErr(commitBuf)
	commitCmd.SetArgs([]string{
		"commit",
		"--cluster", clusterName,
		"--bundle-dir", bundleDir,
		"--local-repo", localRepoRoot,
		"--host-root", hostRoot,
		"--kubeconfig", kubeconfigPath,
		"--runtime-root", runtimeRoot,
	})
	if err := commitCmd.Execute(); err != nil {
		t.Fatalf("sync commit Execute() error = %v\noutput=%s", err, commitBuf.String())
	}
	var commitOut struct {
		CurrentState       string `yaml:"currentState"`
		DesiredStateDigest string `yaml:"desiredStateDigest"`
		CommittedHostPaths []struct {
			Path       string `yaml:"path"`
			InputName  string `yaml:"inputName"`
			RepoPath   string `yaml:"repoPath"`
			BundlePath string `yaml:"bundlePath"`
		} `yaml:"committedHostPaths"`
	}
	if err := yaml.Unmarshal(commitBuf.Bytes(), &commitOut); err != nil {
		t.Fatalf("Unmarshal(commit) error = %v\noutput=%s", err, commitBuf.String())
	}
	if got, want := commitOut.CurrentState, "Clean"; got != want {
		t.Fatalf("commit currentState = %q, want %q\noutput=%s", got, want, commitBuf.String())
	}
	if got, want := len(commitOut.CommittedHostPaths), 1; got != want {
		t.Fatalf("commit committedHostPaths = %d, want %d\noutput=%s", got, want, commitBuf.String())
	}
	if got, want := commitOut.CommittedHostPaths[0].Path, "/etc/demo/config.yaml"; got != want {
		t.Fatalf("committedHostPaths[0].path = %q, want %q", got, want)
	}
	if got, want := commitOut.CommittedHostPaths[0].InputName, "runtime-config"; got != want {
		t.Fatalf("committedHostPaths[0].inputName = %q, want %q", got, want)
	}
	if commitOut.DesiredStateDigest == renderOut.DesiredStateDigest {
		t.Fatalf("commit desiredStateDigest = %q, want changed from render digest", commitOut.DesiredStateDigest)
	}
	for path, want := range map[string]string{
		filepath.Join(localRepoRoot, localrepo.InputsDirName, "runtime", "runtime-config.yaml"): committedInput,
		renderedBundleInputPath: committedInput,
	} {
		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("ReadFile(%q) error = %v", path, err)
		}
		if got := string(data); got != want {
			t.Fatalf("%q after commit = %q, want %q", path, got, want)
		}
	}

	if err := os.WriteFile(liveHostPath, []byte(discardedDrift), 0o644); err != nil {
		t.Fatalf("WriteFile(discarded drift) error = %v", err)
	}
	revertBuf := bytes.NewBuffer(nil)
	revertCmd := newSyncCmd()
	revertCmd.SetOut(revertBuf)
	revertCmd.SetErr(revertBuf)
	revertCmd.SetArgs([]string{
		"revert",
		"--cluster", clusterName,
		"--bundle-dir", bundleDir,
		"--host-root", hostRoot,
		"--host-path", "/etc/demo/config.yaml",
		"--kubeconfig", kubeconfigPath,
		"--runtime-root", runtimeRoot,
	})
	if err := revertCmd.Execute(); err != nil {
		t.Fatalf("sync revert Execute() error = %v\noutput=%s", err, revertBuf.String())
	}
	var revertOut struct {
		BeforeState       string `yaml:"beforeState"`
		CurrentState      string `yaml:"currentState"`
		Reverted          bool   `yaml:"reverted"`
		RevertedHostPaths []struct {
			Path string `yaml:"path"`
		} `yaml:"revertedHostPaths"`
	}
	if err := yaml.Unmarshal(revertBuf.Bytes(), &revertOut); err != nil {
		t.Fatalf("Unmarshal(revert) error = %v\noutput=%s", err, revertBuf.String())
	}
	if got, want := revertOut.BeforeState, "Dirty"; got != want {
		t.Fatalf("revert beforeState = %q, want %q", got, want)
	}
	if got, want := revertOut.CurrentState, "Clean"; got != want {
		t.Fatalf("revert currentState = %q, want %q\noutput=%s", got, want, revertBuf.String())
	}
	if !revertOut.Reverted {
		t.Fatal("revert reverted = false, want true")
	}
	if got, want := len(revertOut.RevertedHostPaths), 1; got != want {
		t.Fatalf("revert revertedHostPaths = %d, want %d", got, want)
	}
	if got, want := revertOut.RevertedHostPaths[0].Path, "/etc/demo/config.yaml"; got != want {
		t.Fatalf("revertedHostPaths[0].path = %q, want %q", got, want)
	}
	data, err = os.ReadFile(liveHostPath)
	if err != nil {
		t.Fatalf("ReadFile(live config after revert) error = %v", err)
	}
	if got, want := string(data), committedInput; got != want {
		t.Fatalf("live config after revert = %q, want %q", got, want)
	}
}

func TestSyncRenderCmdRejectsInvalidPackageSource(t *testing.T) {
	bomPath := filepath.Join(t.TempDir(), "bom.yaml")
	if err := yamlutil.MarshalFile(bomPath, testSyncBOM()); err != nil {
		t.Fatalf("MarshalFile() error = %v", err)
	}

	cmd := newSyncCmd()
	cmd.SetArgs([]string{
		"render",
		"--file", bomPath,
		"--package-source", "invalid",
	})

	if err := cmd.Execute(); err == nil {
		t.Fatal("Execute() error = nil, want error")
	}
}

func TestRunSyncValidateWithReleaseChannelDocument(t *testing.T) {
	previousRuntimeRoot := constants.DefaultRuntimeRootDir
	constants.DefaultRuntimeRootDir = t.TempDir()
	t.Cleanup(func() {
		constants.DefaultRuntimeRootDir = previousRuntimeRoot
	})

	root := t.TempDir()
	bomPath := filepath.Join(root, "bom.yaml")
	doc := testSyncBOM()
	if err := yamlutil.MarshalFile(bomPath, doc); err != nil {
		t.Fatalf("MarshalFile(bom) error = %v", err)
	}
	channelPath := filepath.Join(root, "channel.yaml")
	channel := bom.NewReleaseChannel("test-platform-beta", doc.Metadata.Name, bom.ChannelBeta, doc.Spec.Revision, "bom.yaml")
	if err := yamlutil.MarshalFile(channelPath, channel); err != nil {
		t.Fatalf("MarshalFile(channel) error = %v", err)
	}

	out := runSyncValidate(syncValidateOptions{
		ClusterName:        "default",
		ReleaseChannelPath: channelPath,
		PackageSources:     []string{"kubernetes=" + syncFixtureRoot()},
	})
	if !out.Passed {
		t.Fatalf("validate passed = false, issues=%#v", out.Issues)
	}
	if got, want := out.BOMPath, bomPath; got != want {
		t.Fatalf("validate bomPath = %q, want %q", got, want)
	}
	if got, want := out.ReleaseChannelPath, channelPath; got != want {
		t.Fatalf("validate releaseChannelPath = %q, want %q", got, want)
	}
}

func TestSyncRenderInputStatusDetectsReleaseChannelDocumentDrift(t *testing.T) {
	root := t.TempDir()
	channelPath := filepath.Join(root, "channel.yaml")
	if err := os.WriteFile(channelPath, []byte("revision: one\n"), 0o644); err != nil {
		t.Fatalf("WriteFile(channel) error = %v", err)
	}
	channelData, err := os.ReadFile(channelPath)
	if err != nil {
		t.Fatalf("ReadFile(channel) error = %v", err)
	}
	bomPath := filepath.Join(root, "bom.yaml")
	if err := os.WriteFile(bomPath, []byte("revision: one\n"), 0o644); err != nil {
		t.Fatalf("WriteFile(bom) error = %v", err)
	}
	bomData, err := os.ReadFile(bomPath)
	if err != nil {
		t.Fatalf("ReadFile(bom) error = %v", err)
	}

	bundle := &hydrate.Bundle{
		Spec: hydrate.BundleSpec{
			RenderProvenance: hydrate.RenderProvenance{
				ReleaseChannelPath:   channelPath,
				ReleaseChannelDigest: digest.Canonical.FromBytes(channelData).String(),
				BOMPath:              bomPath,
				BOMDigest:            digest.Canonical.FromBytes(bomData).String(),
			},
		},
	}
	if err := os.WriteFile(channelPath, []byte("revision: two\n"), 0o644); err != nil {
		t.Fatalf("WriteFile(channel drift) error = %v", err)
	}

	status := syncRenderInputStatusForBundle(bundle)
	if got, want := status.State, syncRenderInputStateStale; got != want {
		t.Fatalf("status.state = %q, want %q", got, want)
	}
	change, ok := syncRenderInputChangeByName(status.ChangedInputs, "releaseChannel")
	if !ok {
		t.Fatalf("releaseChannel change missing: %#v", status.ChangedInputs)
	}
	if got, want := change.Path, channelPath; got != want {
		t.Fatalf("releaseChannel change path = %q, want %q", got, want)
	}
}

func TestSyncRenderCmdBlocksOnSourcePreflight(t *testing.T) {
	previousRuntimeRoot := constants.DefaultRuntimeRootDir
	constants.DefaultRuntimeRootDir = t.TempDir()
	t.Cleanup(func() {
		constants.DefaultRuntimeRootDir = previousRuntimeRoot
	})

	packageRoot := filepath.Join(t.TempDir(), "runtime-rootfs")
	writeSyncRequiredInputsTestPackage(t, packageRoot)
	bomPath := filepath.Join(t.TempDir(), "bom.yaml")
	if err := yamlutil.MarshalFile(bomPath, syncLifecycleBOM()); err != nil {
		t.Fatalf("MarshalFile(bom) error = %v", err)
	}
	localRepoRoot := filepath.Join(t.TempDir(), "local-repo")
	writeSyncTestFile(t, filepath.Join(localRepoRoot, "inputs", "runtime", "runtime-config.yaml"), syncLocalRepoInputTemplate(syncLocalRepoInitInput{
		Component: "runtime",
		Name:      "runtime-config",
		Type:      string(packageformat.InputConfigFile),
		Format:    "yaml",
		Required:  true,
		Path:      filepath.ToSlash(filepath.Join(localrepo.InputsDirName, "runtime", "runtime-config.yaml")),
	}), 0o644)

	buf := bytes.NewBuffer(nil)
	errBuf := bytes.NewBuffer(nil)
	cmd := newSyncCmd()
	cmd.SetOut(buf)
	cmd.SetErr(errBuf)
	cmd.SetArgs([]string{
		"render",
		"--file", bomPath,
		"--package-source", "runtime=" + packageRoot,
		"--local-repo", localRepoRoot,
	})
	if err := cmd.Execute(); err == nil {
		t.Fatalf("render Execute() error = nil, want source preflight block\nstdout=%s\nstderr=%s", buf.String(), errBuf.String())
	}

	var out syncRenderOutput
	if err := yaml.Unmarshal(buf.Bytes(), &out); err != nil {
		t.Fatalf("Unmarshal(blocked render) error = %v\noutput=%s", err, buf.String())
	}
	if out.SourcePreflight == nil {
		t.Fatalf("sourcePreflight = nil, want blocking details; output=%#v", out)
	}
	if !out.SourcePreflight.Blocked {
		t.Fatalf("sourcePreflight.blocked = false, want true; output=%#v", out.SourcePreflight)
	}
	if out.BundlePath != "" {
		t.Fatalf("blocked render bundlePath = %q, want empty", out.BundlePath)
	}
	if !strings.Contains(errBuf.String(), "sync render source preflight blocked") {
		t.Fatalf("stderr = %q, want source preflight blocked error", errBuf.String())
	}
}

func TestSyncRenderCmdCanSkipSourcePreflightForDebugging(t *testing.T) {
	previousRuntimeRoot := constants.DefaultRuntimeRootDir
	constants.DefaultRuntimeRootDir = t.TempDir()
	t.Cleanup(func() {
		constants.DefaultRuntimeRootDir = previousRuntimeRoot
	})

	packageRoot := filepath.Join(t.TempDir(), "runtime-rootfs")
	writeSyncRequiredInputsTestPackage(t, packageRoot)
	bomPath := filepath.Join(t.TempDir(), "bom.yaml")
	if err := yamlutil.MarshalFile(bomPath, syncLifecycleBOM()); err != nil {
		t.Fatalf("MarshalFile(bom) error = %v", err)
	}

	buf := bytes.NewBuffer(nil)
	errBuf := bytes.NewBuffer(nil)
	cmd := newSyncCmd()
	cmd.SetOut(buf)
	cmd.SetErr(errBuf)
	cmd.SetArgs([]string{
		"render",
		"--file", bomPath,
		"--package-source", "runtime=" + packageRoot,
		"--skip-source-preflight",
	})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("render Execute() with skip source preflight error = %v\nstdout=%s\nstderr=%s", err, buf.String(), errBuf.String())
	}

	var out syncRenderOutput
	if err := yaml.Unmarshal(buf.Bytes(), &out); err != nil {
		t.Fatalf("Unmarshal(skip render) error = %v\noutput=%s", err, buf.String())
	}
	if out.SourcePreflight != nil {
		t.Fatalf("sourcePreflight = %#v, want nil when skipped", out.SourcePreflight)
	}
	if out.BundlePath == "" {
		t.Fatal("bundlePath is empty, want render to proceed when source preflight is skipped")
	}
}

func TestSyncRenderCmdWithOCIPackageArtifact(t *testing.T) {
	previousRuntimeRoot := constants.DefaultRuntimeRootDir
	runtimeRoot := t.TempDir()
	constants.DefaultRuntimeRootDir = runtimeRoot
	t.Cleanup(func() {
		constants.DefaultRuntimeRootDir = previousRuntimeRoot
	})

	fixtureRoot, err := filepath.Abs(syncFixtureRoot())
	if err != nil {
		t.Fatalf("Abs() error = %v", err)
	}

	doc := testSyncBOM()
	artifactRef := doc.Spec.Packages[0].Artifact.Reference()

	previousResolver := newSyncCachedArtifactResolver
	var cacheRoot string
	newSyncCachedArtifactResolver = func(clusterName string) (packageformat.Loader, hydrate.SourceProvider, error) {
		if got, want := clusterName, "cluster-oci"; got != want {
			t.Fatalf("clusterName = %q, want %q", got, want)
		}
		mounter := &fakeSyncImageMounter{
			mounts: map[string]packageformat.MountedImage{
				artifactRef: {
					Name:       "oci-kubernetes",
					MountPoint: fixtureRoot,
				},
			},
		}
		cacheRoot = filepath.Join(reconcile.DistributionRootPath(clusterName), "package-cache")
		resolver := &syncCachedArtifactResolver{
			cache: &ocipackage.Cache{
				Root:    cacheRoot,
				Mounter: mounter,
			},
		}
		return resolver, resolver, nil
	}
	t.Cleanup(func() {
		newSyncCachedArtifactResolver = previousResolver
	})

	bomPath := filepath.Join(t.TempDir(), "bom.yaml")
	if err := yamlutil.MarshalFile(bomPath, doc); err != nil {
		t.Fatalf("MarshalFile() error = %v", err)
	}

	buf := bytes.NewBuffer(nil)
	cmd := newSyncCmd()
	cmd.SetOut(buf)
	cmd.SetErr(buf)
	cmd.SetArgs([]string{
		"render",
		"--file", bomPath,
		"--cluster", "cluster-oci",
	})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute() error = %v", err)
	}

	var out syncRenderOutput
	if err := yaml.Unmarshal(buf.Bytes(), &out); err != nil {
		t.Fatalf("Unmarshal() error = %v\noutput=%s", err, buf.String())
	}

	if got, want := out.ClusterName, "cluster-oci"; got != want {
		t.Fatalf("out.ClusterName = %q, want %q", got, want)
	}
	if got, want := out.BOMName, "default-platform"; got != want {
		t.Fatalf("out.BOMName = %q, want %q", got, want)
	}
	if !strings.HasPrefix(out.DesiredStateDigest, "sha256:") {
		t.Fatalf("out.DesiredStateDigest = %q, want sha256 digest", out.DesiredStateDigest)
	}
	if cacheRoot == "" {
		t.Fatal("cacheRoot is empty, want cached resolver to be created")
	}
	cacheManifest := filepath.Join(
		cacheRoot,
		"sha256",
		"1111111111111111111111111111111111111111111111111111111111111111",
		"package.yaml",
	)
	if _, err := os.Stat(cacheManifest); err != nil {
		t.Fatalf("cache package manifest missing: %v", err)
	}

	for _, rel := range []string{
		"bundle.yaml",
		"components/kubernetes/package.yaml",
		"components/kubernetes/files/rootfs/README",
		"components/kubernetes/files/hooks/bootstrap.sh",
	} {
		if _, err := os.Stat(filepath.Join(out.BundlePath, rel)); err != nil {
			t.Fatalf("rendered bundle missing %q: %v", rel, err)
		}
	}

	applied, err := state.LoadAppliedRevision("cluster-oci")
	if err != nil {
		t.Fatalf("LoadAppliedRevision() error = %v", err)
	}
	if got, want := applied.Status.State, state.StateDirty; got != want {
		t.Fatalf("status.state = %q, want %q", got, want)
	}
	if got, want := applied.Spec.DesiredStateDigest, out.DesiredStateDigest; got != want {
		t.Fatalf("spec.desiredStateDigest = %q, want %q", got, want)
	}
}

func TestSyncRenderCmdWithMinimalSingleNodePOC(t *testing.T) {
	previousRuntimeRoot := constants.DefaultRuntimeRootDir
	constants.DefaultRuntimeRootDir = t.TempDir()
	t.Cleanup(func() {
		constants.DefaultRuntimeRootDir = previousRuntimeRoot
	})
	clusterName := uniqueSyncClusterName(t, "poc-minimal-sync-test")

	previousFactory := newSyncBuildah
	newSyncBuildah = func(string) (buildah.Interface, error) {
		return nil, fmt.Errorf("unexpected buildah usage")
	}
	t.Cleanup(func() {
		newSyncBuildah = previousFactory
	})

	localRepoRoot := writeSyncPOCLocalRepo(t)
	buf := bytes.NewBuffer(nil)
	cmd := newSyncCmd()
	cmd.SetOut(buf)
	cmd.SetErr(buf)
	cmd.SetArgs([]string{
		"render",
		"--file", syncPOCBOMPath(),
		"--cluster", clusterName,
		"--local-repo", localRepoRoot,
		"--package-source", "containerd=" + syncPOCPackageRoot("containerd"),
		"--package-source", "kubernetes=" + syncPOCPackageRoot("kubernetes"),
		"--package-source", "cilium=" + syncPOCPackageRoot("cilium"),
	})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute() error = %v", err)
	}

	var out syncRenderOutput
	if err := yaml.Unmarshal(buf.Bytes(), &out); err != nil {
		t.Fatalf("Unmarshal() error = %v\noutput=%s", err, buf.String())
	}

	if got, want := out.ClusterName, clusterName; got != want {
		t.Fatalf("out.ClusterName = %q, want %q", got, want)
	}
	if got, want := out.Revision, "rev-poc-001"; got != want {
		t.Fatalf("out.Revision = %q, want %q", got, want)
	}
	if !strings.HasPrefix(out.DesiredStateDigest, "sha256:") {
		t.Fatalf("out.DesiredStateDigest = %q, want sha256 digest", out.DesiredStateDigest)
	}
	if got, want := out.AppliedRevision, state.AppliedRevisionPath(clusterName); got != want {
		t.Fatalf("out.AppliedRevision = %q, want %q", got, want)
	}

	for _, rel := range []string{
		"bundle.yaml",
		"components/containerd/package.yaml",
		"components/containerd/files/rootfs/README",
		"components/kubernetes/files/manifests/bootstrap/kubelet-bootstrap-rbac.yaml",
		"components/cilium/files/manifests/cilium.yaml",
	} {
		if _, err := os.Stat(filepath.Join(out.BundlePath, rel)); err != nil {
			t.Fatalf("rendered bundle missing %q: %v", rel, err)
		}
	}

	doc, err := state.LoadAppliedRevision(clusterName)
	if err != nil {
		t.Fatalf("LoadAppliedRevision() error = %v", err)
	}
	if got, want := doc.Status.State, state.StateDirty; got != want {
		t.Fatalf("status.state = %q, want %q", got, want)
	}
	if got, want := doc.Spec.BOM.Name, "minimal-single-node"; got != want {
		t.Fatalf("spec.bom.name = %q, want %q", got, want)
	}
	if got, want := doc.Spec.DesiredStateDigest, out.DesiredStateDigest; got != want {
		t.Fatalf("spec.desiredStateDigest = %q, want %q", got, want)
	}
}

func TestSyncRenderCmdWithLocalRepo(t *testing.T) {
	previousRuntimeRoot := constants.DefaultRuntimeRootDir
	constants.DefaultRuntimeRootDir = t.TempDir()
	t.Cleanup(func() {
		constants.DefaultRuntimeRootDir = previousRuntimeRoot
	})

	previousFactory := newSyncBuildah
	newSyncBuildah = func(string) (buildah.Interface, error) {
		return nil, fmt.Errorf("unexpected buildah usage")
	}
	t.Cleanup(func() {
		newSyncBuildah = previousFactory
	})

	clusterName := uniqueSyncClusterName(t, "cluster-local-repo")
	localRepoRoot := writeSyncPOCLocalRepo(t)
	writeSyncLocalInput(t, localRepoRoot, "cilium", "cilium-values.yaml", "hubble:\n  enabled: true\n")

	buf := bytes.NewBuffer(nil)
	cmd := newSyncCmd()
	cmd.SetOut(buf)
	cmd.SetErr(buf)
	cmd.SetArgs([]string{
		"render",
		"--file", syncPOCBOMPath(),
		"--cluster", clusterName,
		"--local-repo", localRepoRoot,
		"--package-source", "containerd=" + syncPOCPackageRoot("containerd"),
		"--package-source", "kubernetes=" + syncPOCPackageRoot("kubernetes"),
		"--package-source", "cilium=" + syncPOCPackageRoot("cilium"),
	})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute() error = %v", err)
	}

	var out syncRenderOutput
	if err := yaml.Unmarshal(buf.Bytes(), &out); err != nil {
		t.Fatalf("Unmarshal() error = %v\noutput=%s", err, buf.String())
	}
	if !strings.HasPrefix(out.LocalRepoRevision, "sha256:") {
		t.Fatalf("out.LocalRepoRevision = %q, want sha256 digest", out.LocalRepoRevision)
	}

	renderedPath := filepath.Join(out.BundlePath, "components", "cilium", "files", "files", "values", "basic.yaml")
	data, err := os.ReadFile(renderedPath)
	if err != nil {
		t.Fatalf("ReadFile(%q) error = %v", renderedPath, err)
	}
	if !strings.Contains(string(data), "enabled: true") {
		t.Fatalf("rendered local repo override missing in %q: %s", renderedPath, string(data))
	}

	doc, err := state.LoadAppliedRevision(clusterName)
	if err != nil {
		t.Fatalf("LoadAppliedRevision() error = %v", err)
	}
	if got, want := doc.Spec.LocalRepoRevision, out.LocalRepoRevision; got != want {
		t.Fatalf("spec.localRepoRevision = %q, want %q", got, want)
	}
}

func TestSyncPolicyReportCmd(t *testing.T) {
	tempDir := t.TempDir()
	oldPolicyPath := filepath.Join(tempDir, "old-policy.yaml")
	newPolicyPath := filepath.Join(tempDir, "new-policy.yaml")

	if err := os.WriteFile(oldPolicyPath, []byte(strings.TrimSpace(`
apiVersion: distribution.sealos.io/v1alpha1
kind: LocalPatchPolicy
metadata:
  name: old-policy
spec:
  scope: clusterLocal
  forbiddenExactPaths:
    - status
    - spec.selector
  forbiddenMetadataKeys:
    - creationTimestamp
    - deletionGracePeriodSeconds
    - deletionTimestamp
    - finalizers
    - generateName
    - generation
    - managedFields
    - ownerReferences
    - resourceVersion
    - selfLink
    - uid
  forbiddenContainerFields:
    - command
    - image
  kindRules:
    - kind: ConfigMap
      allowedPrefixes:
        - data
    - kind: Deployment
      allowedPrefixes:
        - spec.template.spec.nodeSelector
`)+"\n"), 0o644); err != nil {
		t.Fatalf("WriteFile(%q) error = %v", oldPolicyPath, err)
	}
	if err := os.WriteFile(newPolicyPath, []byte(strings.TrimSpace(`
apiVersion: distribution.sealos.io/v1alpha1
kind: LocalPatchPolicy
metadata:
  name: new-policy
spec:
  scope: clusterLocal
  forbiddenExactPaths:
    - status
    - spec.selector
  forbiddenMetadataKeys:
    - creationTimestamp
    - deletionGracePeriodSeconds
    - deletionTimestamp
    - finalizers
    - generateName
    - generation
    - managedFields
    - ownerReferences
    - resourceVersion
    - selfLink
    - uid
  forbiddenContainerFields:
    - image
  kindRules:
    - kind: ConfigMap
      allowedPrefixes:
        - data
        - metadata.annotations
`)+"\n"), 0o644); err != nil {
		t.Fatalf("WriteFile(%q) error = %v", newPolicyPath, err)
	}

	localRepoRoot := filepath.Join(tempDir, "local-repo")
	writeSyncLocalPatch(t, localRepoRoot, "grafana", "grafana-settings.patch.yaml", strings.TrimSpace(`
apiVersion: v1
kind: ConfigMap
metadata:
  name: grafana-settings
  namespace: default
data:
  enableHubble: "true"
`)+"\n")
	writeSyncLocalPatch(t, localRepoRoot, "grafana", "grafana-image.patch.yaml", strings.TrimSpace(`
apiVersion: apps/v1
kind: Deployment
metadata:
  name: grafana
  namespace: default
spec:
  template:
    spec:
      containers:
        - name: grafana
          image: grafana/grafana:10.0.0
`)+"\n")

	buf := bytes.NewBuffer(nil)
	cmd := newSyncCmd()
	cmd.SetOut(buf)
	cmd.SetErr(buf)
	cmd.SetArgs([]string{
		"policy-report",
		"--old-policy", oldPolicyPath,
		"--new-policy", newPolicyPath,
		"--local-repo", localRepoRoot,
	})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute() error = %v", err)
	}

	var out syncPolicyReportOutput
	if err := yaml.Unmarshal(buf.Bytes(), &out); err != nil {
		t.Fatalf("Unmarshal() error = %v\noutput=%s", err, buf.String())
	}

	if got, want := filepath.Base(out.OldPolicyPath), "old-policy.yaml"; got != want {
		t.Fatalf("out.OldPolicyPath base = %q, want %q", got, want)
	}
	if got, want := filepath.Base(out.NewPolicyPath), "new-policy.yaml"; got != want {
		t.Fatalf("out.NewPolicyPath base = %q, want %q", got, want)
	}
	if got, want := filepath.Base(out.LocalRepo), "local-repo"; got != want {
		t.Fatalf("out.LocalRepo base = %q, want %q", got, want)
	}
	if out.Report == nil {
		t.Fatal("out.Report = nil, want report")
	}
	if !out.Report.HasWideningChanges {
		t.Fatal("out.Report.HasWideningChanges = false, want true")
	}
	if !out.Report.HasNarrowingChanges {
		t.Fatal("out.Report.HasNarrowingChanges = false, want true")
	}
	if got, want := out.Report.New.Name, "new-policy"; got != want {
		t.Fatalf("out.Report.New.Name = %q, want %q", got, want)
	}
	if got, want := out.Report.New.Scope, ownership.LocalPatchPolicyScopeClusterLocal; got != want {
		t.Fatalf("out.Report.New.Scope = %q, want %q", got, want)
	}
	if got, want := len(out.Report.Impact.AddedAllowedPrefixes), 1; got != want {
		t.Fatalf("len(out.Report.Impact.AddedAllowedPrefixes) = %d, want %d", got, want)
	}
	if got, want := out.Report.Impact.AddedAllowedPrefixes[0].Kind, "ConfigMap"; got != want {
		t.Fatalf("out.Report.Impact.AddedAllowedPrefixes[0].Kind = %q, want %q", got, want)
	}
	if got, want := out.Report.Impact.AddedAllowedPrefixes[0].Path, "metadata.annotations"; got != want {
		t.Fatalf("out.Report.Impact.AddedAllowedPrefixes[0].Path = %q, want %q", got, want)
	}
	if got, want := len(out.Report.Impact.RemovedAllowedPrefixes), 1; got != want {
		t.Fatalf("len(out.Report.Impact.RemovedAllowedPrefixes) = %d, want %d", got, want)
	}
	if got, want := out.Report.Impact.RemovedAllowedPrefixes[0].Kind, "Deployment"; got != want {
		t.Fatalf("out.Report.Impact.RemovedAllowedPrefixes[0].Kind = %q, want %q", got, want)
	}
	if got, want := out.Report.Impact.RemovedAllowedPrefixes[0].Path, "spec.template.spec.nodeSelector"; got != want {
		t.Fatalf("out.Report.Impact.RemovedAllowedPrefixes[0].Path = %q, want %q", got, want)
	}
	if got, want := out.Report.Compatibility.ValidPatchCount, 1; got != want {
		t.Fatalf("out.Report.Compatibility.ValidPatchCount = %d, want %d", got, want)
	}
	if got, want := len(out.Report.Compatibility.InvalidPatches), 1; got != want {
		t.Fatalf("len(out.Report.Compatibility.InvalidPatches) = %d, want %d", got, want)
	}
	if got, want := out.Report.Compatibility.InvalidPatches[0].Component, "grafana"; got != want {
		t.Fatalf("out.Report.Compatibility.InvalidPatches[0].Component = %q, want %q", got, want)
	}

	buf.Reset()
	jsonCmd := newSyncCmd()
	jsonCmd.SetOut(buf)
	jsonCmd.SetErr(buf)
	jsonCmd.SetArgs([]string{
		"policy-report",
		"--old-policy", oldPolicyPath,
		"--new-policy", newPolicyPath,
		"--local-repo", localRepoRoot,
		"--output", "json",
	})
	if err := jsonCmd.Execute(); err != nil {
		t.Fatalf("Execute(--output json) error = %v", err)
	}

	var jsonOut syncPolicyReportOutput
	if err := json.Unmarshal(buf.Bytes(), &jsonOut); err != nil {
		t.Fatalf("json.Unmarshal() error = %v\noutput=%s", err, buf.String())
	}
	if got, want := filepath.Base(jsonOut.OldPolicyPath), "old-policy.yaml"; got != want {
		t.Fatalf("jsonOut.OldPolicyPath base = %q, want %q", got, want)
	}
	if got, want := filepath.Base(jsonOut.LocalRepo), "local-repo"; got != want {
		t.Fatalf("jsonOut.LocalRepo base = %q, want %q", got, want)
	}
	if jsonOut.Report == nil {
		t.Fatal("jsonOut.Report = nil, want report")
	}
	if got, want := jsonOut.Report.New.Name, "new-policy"; got != want {
		t.Fatalf("jsonOut.Report.New.Name = %q, want %q", got, want)
	}
}

func TestSyncPolicyApprovalScanCmd(t *testing.T) {
	t.Run("scans current example approvals", func(t *testing.T) {
		tempDir := t.TempDir()
		for _, item := range []struct {
			path      string
			name      string
			expiresAt string
		}{
			{
				path:      filepath.Join(tempDir, "a", "local-patch-policy-approval.yaml"),
				name:      "approval-a",
				expiresAt: "2099-12-31T00:00:00Z",
			},
			{
				path:      filepath.Join(tempDir, "b", "local-patch-policy-approval-example.yaml"),
				name:      "approval-b",
				expiresAt: "2099-12-31T00:00:00Z",
			},
		} {
			if err := os.MkdirAll(filepath.Dir(item.path), 0o755); err != nil {
				t.Fatalf("MkdirAll(%q) error = %v", filepath.Dir(item.path), err)
			}
			if err := os.WriteFile(item.path, []byte(fmt.Sprintf(strings.TrimSpace(`
apiVersion: distribution.sealos.io/v1alpha1
kind: LocalPatchPolicyGateApproval
metadata:
  name: %s
spec:
  owner: platform-sre
  approvedBy: reviewer@example.com
  changeRef: OPS-1234
  expiresAt: %s
  oldPolicy:
    name: old-policy
    scope: clusterLocal
    digest: sha256:1111111111111111111111111111111111111111111111111111111111111111
  newPolicy:
    name: new-policy
    scope: clusterLocal
    digest: sha256:2222222222222222222222222222222222222222222222222222222222222222
  approvals:
    - code: wideningChange
      expectedCount: 1
      expectedImpact:
        addedAllowedPrefixes:
          - kind: ConfigMap
            path: metadata.annotations
      reason: allow a narrow annotation expansion during this migration window
`), item.name, item.expiresAt)+"\n"), 0o644); err != nil {
				t.Fatalf("WriteFile(%q) error = %v", item.path, err)
			}
		}

		buf := bytes.NewBuffer(nil)
		cmd := newSyncCmd()
		cmd.SetOut(buf)
		cmd.SetErr(buf)
		cmd.SetArgs([]string{
			"policy-approval-scan",
			"--root", tempDir,
		})
		if err := cmd.Execute(); err != nil {
			t.Fatalf("Execute() error = %v", err)
		}

		var out syncPolicyApprovalScanOutput
		if err := yaml.Unmarshal(buf.Bytes(), &out); err != nil {
			t.Fatalf("Unmarshal() error = %v\noutput=%s", err, buf.String())
		}
		if got, want := out.Root, tempDir; got != want {
			t.Fatalf("out.Root = %q, want %q", got, want)
		}
		if out.Scan == nil {
			t.Fatal("out.Scan = nil, want scan result")
		}
		if !out.Scan.Passed {
			t.Fatal("out.Scan.Passed = false, want true")
		}
		if got, want := out.Scan.Config.ApprovalExpiryWarningDays, 30; got != want {
			t.Fatalf("out.Scan.Config.ApprovalExpiryWarningDays = %d, want %d", got, want)
		}
		if out.Scan.Config.FailOnApprovalExpiresSoon {
			t.Fatal("out.Scan.Config.FailOnApprovalExpiresSoon = true, want false")
		}
		if got, want := out.Scan.Summary.Scanned, 2; got != want {
			t.Fatalf("out.Scan.Summary.Scanned = %d, want %d", got, want)
		}
		if got, want := out.Scan.Summary.Valid, 2; got != want {
			t.Fatalf("out.Scan.Summary.Valid = %d, want %d", got, want)
		}
		if got, want := len(out.Scan.Approvals), 2; got != want {
			t.Fatalf("len(out.Scan.Approvals) = %d, want %d", got, want)
		}
		if got, want := out.Scan.Approvals[0].Status, policyreport.ApprovalScanStatusValid; got != want {
			t.Fatalf("out.Scan.Approvals[0].Status = %q, want %q", got, want)
		}
	})

	t.Run("can fail on near-expiry approvals", func(t *testing.T) {
		tempDir := t.TempDir()
		approvalPath := filepath.Join(tempDir, "local-patch-policy-approval.yaml")
		expiresSoonAt := time.Now().UTC().Add(5 * 24 * time.Hour).Format(time.RFC3339)
		if err := os.WriteFile(approvalPath, []byte(fmt.Sprintf(strings.TrimSpace(`
apiVersion: distribution.sealos.io/v1alpha1
kind: LocalPatchPolicyGateApproval
metadata:
  name: example-approval
spec:
  owner: platform-sre
  approvedBy: reviewer@example.com
  changeRef: OPS-1234
  expiresAt: %s
  oldPolicy:
    name: old-policy
    scope: clusterLocal
    digest: sha256:1111111111111111111111111111111111111111111111111111111111111111
  newPolicy:
    name: new-policy
    scope: clusterLocal
    digest: sha256:2222222222222222222222222222222222222222222222222222222222222222
  approvals:
    - code: wideningChange
      expectedCount: 1
      expectedImpact:
        addedAllowedPrefixes:
          - kind: ConfigMap
            path: metadata.annotations
      reason: allow a narrow annotation expansion during this migration window
`), expiresSoonAt)+"\n"), 0o644); err != nil {
			t.Fatalf("WriteFile(%q) error = %v", approvalPath, err)
		}

		buf := bytes.NewBuffer(nil)
		cmd := newSyncCmd()
		cmd.SetOut(buf)
		cmd.SetErr(buf)
		cmd.SetArgs([]string{
			"policy-approval-scan",
			"--root", tempDir,
			"--approval-expiry-warning-days", "30",
			"--fail-when-approval-expires-soon",
		})
		err := cmd.Execute()
		if err == nil {
			t.Fatal("Execute() error = nil, want scan failure")
		}
		if !strings.Contains(err.Error(), "local patch policy approval scan failed") {
			t.Fatalf("Execute() error = %v, want scan failure", err)
		}

		var out syncPolicyApprovalScanOutput
		if err := yaml.Unmarshal(buf.Bytes(), &out); err != nil {
			t.Fatalf("Unmarshal() error = %v\noutput=%s", err, buf.String())
		}
		if out.Scan == nil {
			t.Fatal("out.Scan = nil, want scan result")
		}
		if out.Scan.Passed {
			t.Fatal("out.Scan.Passed = true, want false")
		}
		if !out.Scan.Config.FailOnApprovalExpiresSoon {
			t.Fatal("out.Scan.Config.FailOnApprovalExpiresSoon = false, want true")
		}
		if got, want := out.Scan.Summary.ExpiresSoon, 1; got != want {
			t.Fatalf("out.Scan.Summary.ExpiresSoon = %d, want %d", got, want)
		}
		if got, want := out.Scan.Summary.Blocking, 1; got != want {
			t.Fatalf("out.Scan.Summary.Blocking = %d, want %d", got, want)
		}
		if got, want := out.Scan.Approvals[0].Status, policyreport.ApprovalScanStatusExpiresSoon; got != want {
			t.Fatalf("out.Scan.Approvals[0].Status = %q, want %q", got, want)
		}
		if !out.Scan.Approvals[0].Blocking {
			t.Fatal("out.Scan.Approvals[0].Blocking = false, want true")
		}
		if got := out.Scan.Approvals[0].DaysUntilExpiry; got <= 0 || got > 30 {
			t.Fatalf("out.Scan.Approvals[0].DaysUntilExpiry = %d, want 1..30", got)
		}
	})
}

func TestSyncPolicyGateCmd(t *testing.T) {
	tempDir := t.TempDir()
	oldPolicyPath := filepath.Join(tempDir, "old-policy.yaml")
	newPolicyPath := filepath.Join(tempDir, "new-policy.yaml")

	if err := os.WriteFile(oldPolicyPath, []byte(strings.TrimSpace(`
apiVersion: distribution.sealos.io/v1alpha1
kind: LocalPatchPolicy
metadata:
  name: old-policy
spec:
  scope: clusterLocal
  forbiddenExactPaths:
    - status
    - spec.selector
  forbiddenMetadataKeys:
    - creationTimestamp
    - deletionGracePeriodSeconds
    - deletionTimestamp
    - finalizers
    - generateName
    - generation
    - managedFields
    - ownerReferences
    - resourceVersion
    - selfLink
    - uid
  forbiddenContainerFields:
    - command
    - image
  kindRules:
    - kind: ConfigMap
      allowedPrefixes:
        - data
    - kind: Deployment
      allowedPrefixes:
        - spec.template.spec.nodeSelector
`)+"\n"), 0o644); err != nil {
		t.Fatalf("WriteFile(%q) error = %v", oldPolicyPath, err)
	}
	if err := os.WriteFile(newPolicyPath, []byte(strings.TrimSpace(`
apiVersion: distribution.sealos.io/v1alpha1
kind: LocalPatchPolicy
metadata:
  name: new-policy
spec:
  scope: clusterLocal
  forbiddenExactPaths:
    - status
    - spec.selector
  forbiddenMetadataKeys:
    - creationTimestamp
    - deletionGracePeriodSeconds
    - deletionTimestamp
    - finalizers
    - generateName
    - generation
    - managedFields
    - ownerReferences
    - resourceVersion
    - selfLink
    - uid
  forbiddenContainerFields:
    - image
  kindRules:
    - kind: ConfigMap
      allowedPrefixes:
        - data
        - metadata.annotations
`)+"\n"), 0o644); err != nil {
		t.Fatalf("WriteFile(%q) error = %v", newPolicyPath, err)
	}

	localRepoRoot := filepath.Join(tempDir, "local-repo")
	writeSyncLocalPatch(t, localRepoRoot, "grafana", "grafana-settings.patch.yaml", strings.TrimSpace(`
apiVersion: v1
kind: ConfigMap
metadata:
  name: grafana-settings
  namespace: default
data:
  enableHubble: "true"
`)+"\n")
	writeSyncLocalPatch(t, localRepoRoot, "grafana", "grafana-image.patch.yaml", strings.TrimSpace(`
apiVersion: apps/v1
kind: Deployment
metadata:
  name: grafana
  namespace: default
spec:
  template:
    spec:
      containers:
        - name: grafana
          image: grafana/grafana:10.0.0
`)+"\n")

	buf := bytes.NewBuffer(nil)
	cmd := newSyncCmd()
	cmd.SetOut(buf)
	cmd.SetErr(buf)
	cmd.SetArgs([]string{
		"policy-gate",
		"--old-policy", oldPolicyPath,
		"--new-policy", newPolicyPath,
		"--local-repo", localRepoRoot,
	})
	err := cmd.Execute()
	if err == nil {
		t.Fatal("Execute() error = nil, want gate failure")
	}
	if !strings.Contains(err.Error(), "local patch policy gate failed") {
		t.Fatalf("Execute() error = %v, want gate failure", err)
	}

	var out syncPolicyGateOutput
	if err := yaml.Unmarshal(buf.Bytes(), &out); err != nil {
		t.Fatalf("Unmarshal() error = %v\noutput=%s", err, buf.String())
	}
	if out.Gate == nil {
		t.Fatal("out.Gate = nil, want gate result")
	}
	if out.Gate.Passed {
		t.Fatal("out.Gate.Passed = true, want false")
	}
	if got, want := out.Gate.Config.FailOnWidening, true; got != want {
		t.Fatalf("out.Gate.Config.FailOnWidening = %t, want %t", got, want)
	}
	if got, want := out.Gate.Config.FailOnIncompatiblePatches, true; got != want {
		t.Fatalf("out.Gate.Config.FailOnIncompatiblePatches = %t, want %t", got, want)
	}
	if got, want := out.Gate.Config.ApprovalExpiryWarningDays, 30; got != want {
		t.Fatalf("out.Gate.Config.ApprovalExpiryWarningDays = %d, want %d", got, want)
	}
	if got, want := out.Gate.Config.FailOnApprovalExpiresSoon, false; got != want {
		t.Fatalf("out.Gate.Config.FailOnApprovalExpiresSoon = %t, want %t", got, want)
	}
	if got, want := len(out.Gate.Violations), 2; got != want {
		t.Fatalf("len(out.Gate.Violations) = %d, want %d", got, want)
	}
	if got, want := out.Gate.Violations[0].Code, policyreport.GateViolationWidening; got != want {
		t.Fatalf("out.Gate.Violations[0].Code = %q, want %q", got, want)
	}
	if got, want := out.Gate.Violations[1].Code, policyreport.GateViolationIncompatiblePatches; got != want {
		t.Fatalf("out.Gate.Violations[1].Code = %q, want %q", got, want)
	}
	if got, want := len(out.Gate.Warnings), 1; got != want {
		t.Fatalf("len(out.Gate.Warnings) = %d, want %d", got, want)
	}
}

func TestSyncPolicyGateCmdWithApprovalFile(t *testing.T) {
	tempDir := t.TempDir()
	oldPolicyPath := filepath.Join(tempDir, "old-policy.yaml")
	newPolicyPath := filepath.Join(tempDir, "new-policy.yaml")
	approvalPath := filepath.Join(tempDir, "approval.yaml")

	if err := os.WriteFile(oldPolicyPath, []byte(strings.TrimSpace(`
apiVersion: distribution.sealos.io/v1alpha1
kind: LocalPatchPolicy
metadata:
  name: old-policy
spec:
  scope: clusterLocal
  forbiddenExactPaths:
    - status
    - spec.selector
  forbiddenMetadataKeys:
    - creationTimestamp
    - deletionGracePeriodSeconds
    - deletionTimestamp
    - finalizers
    - generateName
    - generation
    - managedFields
    - ownerReferences
    - resourceVersion
    - selfLink
    - uid
  forbiddenContainerFields:
    - command
    - image
  kindRules:
    - kind: ConfigMap
      allowedPrefixes:
        - data
    - kind: Deployment
      allowedPrefixes:
        - spec.template.spec.nodeSelector
`)+"\n"), 0o644); err != nil {
		t.Fatalf("WriteFile(%q) error = %v", oldPolicyPath, err)
	}
	if err := os.WriteFile(newPolicyPath, []byte(strings.TrimSpace(`
apiVersion: distribution.sealos.io/v1alpha1
kind: LocalPatchPolicy
metadata:
  name: new-policy
spec:
  scope: clusterLocal
  forbiddenExactPaths:
    - status
    - spec.selector
  forbiddenMetadataKeys:
    - creationTimestamp
    - deletionGracePeriodSeconds
    - deletionTimestamp
    - finalizers
    - generateName
    - generation
    - managedFields
    - ownerReferences
    - resourceVersion
    - selfLink
    - uid
  forbiddenContainerFields:
    - image
  kindRules:
    - kind: ConfigMap
      allowedPrefixes:
        - data
        - metadata.annotations
`)+"\n"), 0o644); err != nil {
		t.Fatalf("WriteFile(%q) error = %v", newPolicyPath, err)
	}
	oldPolicyDoc, err := ownership.LoadLocalPatchPolicyFile(oldPolicyPath)
	if err != nil {
		t.Fatalf("LoadLocalPatchPolicyFile(%q) error = %v", oldPolicyPath, err)
	}
	newPolicyDoc, err := ownership.LoadLocalPatchPolicyFile(newPolicyPath)
	if err != nil {
		t.Fatalf("LoadLocalPatchPolicyFile(%q) error = %v", newPolicyPath, err)
	}
	report, err := policyreport.Build(oldPolicyDoc, newPolicyDoc, nil)
	if err != nil {
		t.Fatalf("policyreport.Build() error = %v", err)
	}
	if err := os.WriteFile(approvalPath, []byte(fmt.Sprintf(strings.TrimSpace(`
apiVersion: distribution.sealos.io/v1alpha1
kind: LocalPatchPolicyGateApproval
metadata:
  name: example-approval
spec:
  owner: platform-sre
  approvedBy: reviewer@example.com
  changeRef: OPS-1234
  expiresAt: 2099-12-31T00:00:00Z
  oldPolicy:
    name: old-policy
    scope: clusterLocal
    digest: %s
  newPolicy:
    name: new-policy
    scope: clusterLocal
    digest: %s
  approvals:
    - code: wideningChange
      expectedCount: 2
      expectedImpact:
        addedAllowedPrefixes:
          - kind: ConfigMap
            path: metadata.annotations
        removedForbiddenContainerFields:
          - command
      reason: allow cluster-local annotation expansion for this rollout
    - code: incompatiblePatches
      expectedCount: 1
      expectedImpact:
        incompatiblePatches:
          - component: grafana
            relativePath: grafana-image.patch.yaml
      reason: existing local patches will be migrated in the same change
`), report.Old.Digest, report.New.Digest)+"\n"), 0o644); err != nil {
		t.Fatalf("WriteFile(%q) error = %v", approvalPath, err)
	}

	localRepoRoot := filepath.Join(tempDir, "local-repo")
	writeSyncLocalPatch(t, localRepoRoot, "grafana", "grafana-settings.patch.yaml", strings.TrimSpace(`
apiVersion: v1
kind: ConfigMap
metadata:
  name: grafana-settings
  namespace: default
data:
  enableHubble: "true"
`)+"\n")
	writeSyncLocalPatch(t, localRepoRoot, "grafana", "grafana-image.patch.yaml", strings.TrimSpace(`
apiVersion: apps/v1
kind: Deployment
metadata:
  name: grafana
  namespace: default
spec:
  template:
    spec:
      containers:
        - name: grafana
          image: grafana/grafana:10.0.0
`)+"\n")

	buf := bytes.NewBuffer(nil)
	cmd := newSyncCmd()
	cmd.SetOut(buf)
	cmd.SetErr(buf)
	cmd.SetArgs([]string{
		"policy-gate",
		"--old-policy", oldPolicyPath,
		"--new-policy", newPolicyPath,
		"--local-repo", localRepoRoot,
		"--approval-file", approvalPath,
	})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute() error = %v", err)
	}

	var out syncPolicyGateOutput
	if err := yaml.Unmarshal(buf.Bytes(), &out); err != nil {
		t.Fatalf("Unmarshal() error = %v\noutput=%s", err, buf.String())
	}
	if got, want := filepath.Base(out.ApprovalFile), "approval.yaml"; got != want {
		t.Fatalf("out.ApprovalFile base = %q, want %q", got, want)
	}
	if out.Gate == nil {
		t.Fatal("out.Gate = nil, want gate result")
	}
	if !out.Gate.Passed {
		t.Fatal("out.Gate.Passed = false, want true")
	}
	if !out.Gate.ApprovalSummary.ApprovalProvided {
		t.Fatal("out.Gate.ApprovalSummary.ApprovalProvided = false, want true")
	}
	if !out.Gate.ApprovalSummary.ApprovalApplied {
		t.Fatal("out.Gate.ApprovalSummary.ApprovalApplied = false, want true")
	}
	if got, want := out.Gate.ApprovalSummary.Owner, "platform-sre"; got != want {
		t.Fatalf("out.Gate.ApprovalSummary.Owner = %q, want %q", got, want)
	}
	if got, want := out.Gate.ApprovalSummary.ApprovedBy, "reviewer@example.com"; got != want {
		t.Fatalf("out.Gate.ApprovalSummary.ApprovedBy = %q, want %q", got, want)
	}
	if got, want := out.Gate.ApprovalSummary.ChangeRef, "OPS-1234"; got != want {
		t.Fatalf("out.Gate.ApprovalSummary.ChangeRef = %q, want %q", got, want)
	}
	if got, want := out.Gate.ApprovalSummary.ExpiresAt, "2099-12-31T00:00:00Z"; got != want {
		t.Fatalf("out.Gate.ApprovalSummary.ExpiresAt = %q, want %q", got, want)
	}
	if out.Gate.ApprovalSummary.ExpiresSoon {
		t.Fatal("out.Gate.ApprovalSummary.ExpiresSoon = true, want false")
	}
	if got, want := out.Gate.ApprovalSummary.FollowUpAction, policyreport.GateApprovalFollowUpRemoveAfterChangeRefResolved; got != want {
		t.Fatalf("out.Gate.ApprovalSummary.FollowUpAction = %q, want %q", got, want)
	}
	if got, want := len(out.Gate.ApprovalSummary.ApprovedViolationCodes), 2; got != want {
		t.Fatalf("len(out.Gate.ApprovalSummary.ApprovedViolationCodes) = %d, want %d", got, want)
	}
	if got, want := out.Gate.ApprovalSummary.ApprovedViolationCodes[0], policyreport.GateViolationIncompatiblePatches; got != want {
		t.Fatalf("out.Gate.ApprovalSummary.ApprovedViolationCodes[0] = %q, want %q", got, want)
	}
	if got, want := out.Gate.ApprovalSummary.ApprovedViolationCodes[1], policyreport.GateViolationWidening; got != want {
		t.Fatalf("out.Gate.ApprovalSummary.ApprovedViolationCodes[1] = %q, want %q", got, want)
	}
	if got, want := len(out.Gate.Violations), 0; got != want {
		t.Fatalf("len(out.Gate.Violations) = %d, want %d", got, want)
	}
	if got, want := len(out.Gate.ApprovedViolations), 2; got != want {
		t.Fatalf("len(out.Gate.ApprovedViolations) = %d, want %d", got, want)
	}
	if got, want := out.Gate.ApprovedViolations[0].Code, policyreport.GateViolationWidening; got != want {
		t.Fatalf("out.Gate.ApprovedViolations[0].Code = %q, want %q", got, want)
	}
	if got, want := out.Gate.ApprovedViolations[0].Impact.AddedAllowedPrefixes[0].Path, "metadata.annotations"; got != want {
		t.Fatalf("out.Gate.ApprovedViolations[0].Impact.AddedAllowedPrefixes[0].Path = %q, want %q", got, want)
	}
	if got, want := out.Gate.ApprovedViolations[1].Code, policyreport.GateViolationIncompatiblePatches; got != want {
		t.Fatalf("out.Gate.ApprovedViolations[1].Code = %q, want %q", got, want)
	}
	if got, want := out.Gate.ApprovedViolations[1].Impact.IncompatiblePatches[0].RelativePath, "grafana-image.patch.yaml"; got != want {
		t.Fatalf("out.Gate.ApprovedViolations[1].Impact.IncompatiblePatches[0].RelativePath = %q, want %q", got, want)
	}
	if got, want := len(out.Gate.Warnings), 4; got != want {
		t.Fatalf("len(out.Gate.Warnings) = %d, want %d", got, want)
	}
	if got, want := out.Gate.Warnings[2], "candidate LocalPatchPolicy narrows previously allowed surface; review operator continuity impact"; got != want {
		t.Fatalf("out.Gate.Warnings[2] = %q, want %q", got, want)
	}
	if got, want := out.Gate.Warnings[3], `approval file was used to pass the gate; remove it after changeRef "OPS-1234" is resolved`; got != want {
		t.Fatalf("out.Gate.Warnings[3] = %q, want %q", got, want)
	}
}

func TestSyncPolicyGateCmdWithApprovalExpiryFlags(t *testing.T) {
	tempDir := t.TempDir()
	oldPolicyPath := filepath.Join(tempDir, "old-policy.yaml")
	newPolicyPath := filepath.Join(tempDir, "new-policy.yaml")
	approvalPath := filepath.Join(tempDir, "approval.yaml")

	if err := os.WriteFile(oldPolicyPath, []byte(strings.TrimSpace(`
apiVersion: distribution.sealos.io/v1alpha1
kind: LocalPatchPolicy
metadata:
  name: old-policy
spec:
  scope: clusterLocal
  forbiddenExactPaths:
    - status
    - spec.selector
  forbiddenMetadataKeys:
    - creationTimestamp
    - deletionGracePeriodSeconds
    - deletionTimestamp
    - finalizers
    - generateName
    - generation
    - managedFields
    - ownerReferences
    - resourceVersion
    - selfLink
    - uid
  forbiddenContainerFields:
    - command
    - image
  kindRules:
    - kind: ConfigMap
      allowedPrefixes:
        - data
`)+"\n"), 0o644); err != nil {
		t.Fatalf("WriteFile(%q) error = %v", oldPolicyPath, err)
	}
	if err := os.WriteFile(newPolicyPath, []byte(strings.TrimSpace(`
apiVersion: distribution.sealos.io/v1alpha1
kind: LocalPatchPolicy
metadata:
  name: new-policy
spec:
  scope: clusterLocal
  forbiddenExactPaths:
    - status
    - spec.selector
  forbiddenMetadataKeys:
    - creationTimestamp
    - deletionGracePeriodSeconds
    - deletionTimestamp
    - finalizers
    - generateName
    - generation
    - managedFields
    - ownerReferences
    - resourceVersion
    - selfLink
    - uid
  forbiddenContainerFields:
    - image
  kindRules:
    - kind: ConfigMap
      allowedPrefixes:
        - data
        - metadata.annotations
`)+"\n"), 0o644); err != nil {
		t.Fatalf("WriteFile(%q) error = %v", newPolicyPath, err)
	}
	oldPolicyDoc, err := ownership.LoadLocalPatchPolicyFile(oldPolicyPath)
	if err != nil {
		t.Fatalf("LoadLocalPatchPolicyFile(%q) error = %v", oldPolicyPath, err)
	}
	newPolicyDoc, err := ownership.LoadLocalPatchPolicyFile(newPolicyPath)
	if err != nil {
		t.Fatalf("LoadLocalPatchPolicyFile(%q) error = %v", newPolicyPath, err)
	}
	report, err := policyreport.Build(oldPolicyDoc, newPolicyDoc, nil)
	if err != nil {
		t.Fatalf("policyreport.Build() error = %v", err)
	}
	if err := os.WriteFile(approvalPath, []byte(fmt.Sprintf(strings.TrimSpace(`
apiVersion: distribution.sealos.io/v1alpha1
kind: LocalPatchPolicyGateApproval
metadata:
  name: example-approval
spec:
  owner: platform-sre
  approvedBy: reviewer@example.com
  changeRef: OPS-1234
  expiresAt: 2099-12-31T00:00:00Z
  oldPolicy:
    name: old-policy
    scope: clusterLocal
    digest: %s
  newPolicy:
    name: new-policy
    scope: clusterLocal
    digest: %s
  approvals:
    - code: wideningChange
      expectedCount: 2
      expectedImpact:
        addedAllowedPrefixes:
          - kind: ConfigMap
            path: metadata.annotations
        removedForbiddenContainerFields:
          - command
      reason: allow cluster-local annotation expansion for this rollout
`), report.Old.Digest, report.New.Digest)+"\n"), 0o644); err != nil {
		t.Fatalf("WriteFile(%q) error = %v", approvalPath, err)
	}

	buf := bytes.NewBuffer(nil)
	cmd := newSyncCmd()
	cmd.SetOut(buf)
	cmd.SetErr(buf)
	cmd.SetArgs([]string{
		"policy-gate",
		"--old-policy", oldPolicyPath,
		"--new-policy", newPolicyPath,
		"--approval-file", approvalPath,
		"--approval-expiry-warning-days", "7",
		"--fail-when-approval-expires-soon",
		"--allow-incompatible-patches",
	})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute() error = %v", err)
	}

	var out syncPolicyGateOutput
	if err := yaml.Unmarshal(buf.Bytes(), &out); err != nil {
		t.Fatalf("Unmarshal() error = %v\noutput=%s", err, buf.String())
	}
	if out.Gate == nil {
		t.Fatal("out.Gate = nil, want gate result")
	}
	if got, want := out.Gate.Config.ApprovalExpiryWarningDays, 7; got != want {
		t.Fatalf("out.Gate.Config.ApprovalExpiryWarningDays = %d, want %d", got, want)
	}
	if got, want := out.Gate.Config.FailOnApprovalExpiresSoon, true; got != want {
		t.Fatalf("out.Gate.Config.FailOnApprovalExpiresSoon = %t, want %t", got, want)
	}
}

func TestSyncDiffCmd(t *testing.T) {
	previousRuntimeRoot := constants.DefaultRuntimeRootDir
	runtimeRoot := previousRuntimeRoot
	if runtimeRoot == "" {
		runtimeRoot = constants.GetRuntimeRootDir(constants.AppName)
	}
	constants.DefaultRuntimeRootDir = runtimeRoot
	t.Cleanup(func() {
		constants.DefaultRuntimeRootDir = previousRuntimeRoot
	})

	previousOutput := outputSyncKubectl
	outputSyncKubectl = func(args ...string) ([]byte, error) {
		command := strings.Join(args, " ")
		if strings.Contains(command, "Secret grafana-admin-credentials") {
			return []byte(`{"apiVersion":"v1","kind":"Secret","metadata":{"name":"grafana-admin-credentials","namespace":"default","resourceVersion":"1","uid":"abc","managedFields":[{}],"annotations":{"kubectl.kubernetes.io/last-applied-configuration":"{}"}},"data":{"username":"YWRtaW4=","password":"cGFzc3cwcmQ="}}`), nil
		}
		return []byte(`{"apiVersion":"apps/v1","kind":"DaemonSet","metadata":{"name":"cilium","namespace":"kube-system","resourceVersion":"2"},"spec":{"template":{"spec":{"containers":[{"name":"cilium-agent","image":"quay.io/cilium/cilium:v1.15.1"}]}}}}`), nil
	}
	t.Cleanup(func() {
		outputSyncKubectl = previousOutput
	})

	clusterName := uniqueSyncClusterName(t, "diff")
	t.Cleanup(func() {
		_ = os.RemoveAll(filepath.Join(runtimeRoot, clusterName))
	})
	bundleDir := t.TempDir()
	bundle := &hydrate.Bundle{
		APIVersion: "distribution.sealos.io/v1alpha1",
		Kind:       "HydratedBundle",
		Spec: hydrate.BundleSpec{
			BOMName:  "minimal-single-node",
			Revision: "rev-poc-001",
			Channel:  bom.ChannelAlpha,
			TrackedK8sObjects: []hydrate.TrackedK8sObject{
				{
					APIVersion: "v1",
					Kind:       "Secret",
					Namespace:  "default",
					Name:       "grafana-admin-credentials",
					Path:       "local-resources/grafana-admin-secret.yaml",
					Source:     hydrate.InventorySourceLocalResource,
					Ownership:  hydrate.InventoryOwnershipLocal,
				},
				{
					APIVersion: "apps/v1",
					Kind:       "DaemonSet",
					Namespace:  "kube-system",
					Name:       "cilium",
					Path:       "components/cilium/files/manifests/cilium.yaml",
					Component:  "cilium",
					Source:     hydrate.InventorySourcePackageManifest,
					Ownership:  hydrate.InventoryOwnershipGlobal,
				},
			},
			Components: []hydrate.RenderedComponent{
				{Name: "cilium"},
			},
		},
	}
	if err := os.MkdirAll(filepath.Join(bundleDir, "local-resources"), 0o755); err != nil {
		t.Fatalf("MkdirAll(local-resources) error = %v", err)
	}
	if err := os.MkdirAll(filepath.Join(bundleDir, "components", "cilium", "files", "manifests"), 0o755); err != nil {
		t.Fatalf("MkdirAll(cilium manifests) error = %v", err)
	}
	if err := os.WriteFile(filepath.Join(bundleDir, "local-resources", "grafana-admin-secret.yaml"), []byte("apiVersion: v1\nkind: Secret\nmetadata:\n  name: grafana-admin-credentials\n  namespace: default\nstringData:\n  username: admin\n  password: passw0rd\n"), 0o644); err != nil {
		t.Fatalf("WriteFile(secret) error = %v", err)
	}
	if err := os.WriteFile(filepath.Join(bundleDir, "components", "cilium", "files", "manifests", "cilium.yaml"), []byte("apiVersion: apps/v1\nkind: DaemonSet\nmetadata:\n  name: cilium\n  namespace: kube-system\nspec:\n  template:\n    spec:\n      containers:\n        - name: cilium-agent\n          image: quay.io/cilium/cilium:v1.15.0\n"), 0o644); err != nil {
		t.Fatalf("WriteFile(cilium manifest) error = %v", err)
	}
	if err := yamlutil.MarshalFile(filepath.Join(bundleDir, hydrate.BundleFileName), bundle); err != nil {
		t.Fatalf("MarshalFile(bundle) error = %v", err)
	}
	desiredStateDigest, err := hydrate.DigestBundle(bundleDir)
	if err != nil {
		t.Fatalf("DigestBundle() error = %v", err)
	}
	if _, err := state.PersistSuccessfulApply(clusterName, state.BOMReference{
		Name:     bundle.Spec.BOMName,
		Revision: bundle.Spec.Revision,
		Channel:  bundle.Spec.Channel,
	}, desiredStateDigest.String(), "local-rev-1", "patch-rev-1"); err != nil {
		t.Fatalf("PersistSuccessfulApply() error = %v", err)
	}
	if _, err := state.LoadAppliedRevision(clusterName); err != nil {
		t.Fatalf("LoadAppliedRevision() precondition error = %v", err)
	}

	buf := bytes.NewBuffer(nil)
	constants.DefaultRuntimeRootDir = runtimeRoot
	cmd := newSyncCmd()
	cmd.SetOut(buf)
	cmd.SetErr(buf)
	cmd.SetArgs([]string{
		"diff",
		"--cluster", clusterName,
		"--bundle-dir", bundleDir,
		"--kubeconfig", "/tmp/test-kubeconfig",
	})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute() error = %v", err)
	}

	var out struct {
		CurrentState         string   `yaml:"currentState"`
		Headline             string   `yaml:"headline"`
		ObservationPersisted bool     `yaml:"observationPersisted"`
		Warnings             []string `yaml:"warnings"`
		Preflight            struct {
			State             string `yaml:"state"`
			Summary           string `yaml:"summary"`
			RecommendedAction string `yaml:"recommendedAction"`
			RefreshCommand    string `yaml:"refreshCommand"`
			Blocked           bool   `yaml:"blocked"`
		} `yaml:"preflight"`
		LocalPatchPolicy struct {
			Source string `yaml:"source"`
			Scope  string `yaml:"scope"`
			Name   string `yaml:"name"`
			Path   string `yaml:"path"`
			Digest string `yaml:"digest"`
		} `yaml:"localPatchPolicy"`
		PersistedObservedSummary struct {
			Orphan               int `yaml:"orphan"`
			DirectCommitEligible int `yaml:"directCommitEligible"`
			DirectRevertEligible int `yaml:"directRevertEligible"`
			BundleMatchRequired  int `yaml:"bundleMatchRequired"`
		} `yaml:"persistedObservedSummary"`
		RecordedRevision struct {
			State           string `yaml:"state"`
			ObservedSummary struct {
				Orphan               int `yaml:"orphan"`
				DirectCommitEligible int `yaml:"directCommitEligible"`
				DirectRevertEligible int `yaml:"directRevertEligible"`
				BundleMatchRequired  int `yaml:"bundleMatchRequired"`
			} `yaml:"observedSummary"`
		} `yaml:"recordedRevision"`
		CurrentCompare struct {
			Summary struct {
				Total   int `yaml:"total"`
				Present int `yaml:"present"`
				Missing int `yaml:"missing"`
				Matched int `yaml:"matched"`
				Drifted int `yaml:"drifted"`
				Clean   int `yaml:"clean"`
				Dirty   int `yaml:"dirty"`
				Orphan  int `yaml:"orphan"`
			} `yaml:"summary"`
			Objects []struct {
				Tracked struct {
					Name string `yaml:"name"`
				} `yaml:"tracked"`
				State      string `yaml:"state"`
				Comparison string `yaml:"comparison"`
				Mismatches []struct {
					Path      string `yaml:"path"`
					Reason    string `yaml:"reason"`
					Ownership string `yaml:"ownership"`
					State     string `yaml:"state"`
				} `yaml:"mismatches"`
				Remediation struct {
					Action          string   `yaml:"action"`
					ChangeOwner     string   `yaml:"changeOwner"`
					AllowedCommands []string `yaml:"allowedCommands"`
					CommandGuidance []struct {
						Command      string `yaml:"command"`
						Availability string `yaml:"availability"`
					} `yaml:"commandGuidance"`
				} `yaml:"remediation"`
			} `yaml:"objects"`
		} `yaml:"currentCompare"`
	}
	if err := yaml.Unmarshal(buf.Bytes(), &out); err != nil {
		t.Fatalf("Unmarshal() error = %v\noutput=%s", err, buf.String())
	}
	if got, want := out.CurrentState, "Orphan"; got != want {
		t.Fatalf("currentState = %q, want %q", got, want)
	}
	if got, want := out.Preflight.State, "Warning"; got != want {
		t.Fatalf("preflight.state = %q, want %q", got, want)
	}
	if got, want := out.Preflight.RecommendedAction, "inspect"; got != want {
		t.Fatalf("preflight.recommendedAction = %q, want %q", got, want)
	}
	if got, want := out.Warnings, []string{syncSourcePreflightMissingWarning}; strings.Join(got, "\n") != strings.Join(want, "\n") {
		t.Fatalf("warnings = %#v, want %#v", got, want)
	}
	if out.Preflight.Blocked {
		t.Fatal("preflight.blocked = true, want false")
	}
	if out.Preflight.Summary == "" {
		t.Fatal("preflight.summary is empty")
	}
	if !strings.Contains(out.Preflight.RefreshCommand, "'sealos' 'sync' 'render'") {
		t.Fatalf("preflight.refreshCommand = %q, want sync render command", out.Preflight.RefreshCommand)
	}
	if got, want := out.LocalPatchPolicy.Source, "builtInDefault"; got != want {
		t.Fatalf("localPatchPolicy.source = %q, want %q", got, want)
	}
	if got, want := out.LocalPatchPolicy.Scope, "clusterLocal"; got != want {
		t.Fatalf("localPatchPolicy.scope = %q, want %q", got, want)
	}
	if got, want := out.LocalPatchPolicy.Name, "defaultLocalPatchPolicy"; got != want {
		t.Fatalf("localPatchPolicy.name = %q, want %q", got, want)
	}
	if got := out.LocalPatchPolicy.Path; got != "" {
		t.Fatalf("localPatchPolicy.path = %q, want empty for legacy bundle fixture", got)
	}
	if got := out.LocalPatchPolicy.Digest; got != "" {
		t.Fatalf("localPatchPolicy.digest = %q, want empty for legacy bundle fixture", got)
	}
	if got, want := out.Headline, "state=Orphan; dirtyObjects=0; orphanObjects=1; dirtyHostPaths=0; orphanHostPaths=0; directCommitEligible=0; directRevertEligible=1; bundleMatchRequired=1; policyEligibleOrphanObjects=0; hostPathConflicts=0; localInputHostSplits=0"; got != want {
		t.Fatalf("headline = %q, want %q", got, want)
	}
	if !out.ObservationPersisted {
		t.Fatalf("observationPersisted = false, want true\noutput=%s", buf.String())
	}
	if got, want := out.PersistedObservedSummary.Orphan, 1; got != want {
		t.Fatalf("persistedObservedSummary.orphan = %d, want %d", got, want)
	}
	if got, want := out.PersistedObservedSummary.DirectCommitEligible, 0; got != want {
		t.Fatalf("persistedObservedSummary.directCommitEligible = %d, want %d", got, want)
	}
	if got, want := out.PersistedObservedSummary.DirectRevertEligible, 1; got != want {
		t.Fatalf("persistedObservedSummary.directRevertEligible = %d, want %d", got, want)
	}
	if got, want := out.PersistedObservedSummary.BundleMatchRequired, 1; got != want {
		t.Fatalf("persistedObservedSummary.bundleMatchRequired = %d, want %d", got, want)
	}
	if got, want := out.RecordedRevision.State, "Orphan"; got != want {
		t.Fatalf("recordedRevision.state = %q, want %q", got, want)
	}
	if got, want := out.RecordedRevision.ObservedSummary.Orphan, 1; got != want {
		t.Fatalf("recordedRevision.observedSummary.orphan = %d, want %d", got, want)
	}
	if got, want := out.RecordedRevision.ObservedSummary.DirectCommitEligible, 0; got != want {
		t.Fatalf("recordedRevision.observedSummary.directCommitEligible = %d, want %d", got, want)
	}
	if got, want := out.RecordedRevision.ObservedSummary.DirectRevertEligible, 1; got != want {
		t.Fatalf("recordedRevision.observedSummary.directRevertEligible = %d, want %d", got, want)
	}
	if got, want := out.RecordedRevision.ObservedSummary.BundleMatchRequired, 1; got != want {
		t.Fatalf("recordedRevision.observedSummary.bundleMatchRequired = %d, want %d", got, want)
	}
	if got, want := out.CurrentCompare.Summary.Total, 2; got != want {
		t.Fatalf("summary.total = %d, want %d", got, want)
	}
	if got, want := out.CurrentCompare.Summary.Present, 2; got != want {
		t.Fatalf("summary.present = %d, want %d", got, want)
	}
	if got, want := out.CurrentCompare.Summary.Missing, 0; got != want {
		t.Fatalf("summary.missing = %d, want %d", got, want)
	}
	if got, want := out.CurrentCompare.Summary.Matched, 1; got != want {
		t.Fatalf("summary.matched = %d, want %d", got, want)
	}
	if got, want := out.CurrentCompare.Summary.Drifted, 1; got != want {
		t.Fatalf("summary.drifted = %d, want %d", got, want)
	}
	if got, want := out.CurrentCompare.Summary.Clean, 1; got != want {
		t.Fatalf("summary.clean = %d, want %d", got, want)
	}
	if got, want := out.CurrentCompare.Summary.Dirty, 0; got != want {
		t.Fatalf("summary.dirty = %d, want %d", got, want)
	}
	if got, want := out.CurrentCompare.Summary.Orphan, 1; got != want {
		t.Fatalf("summary.orphan = %d, want %d", got, want)
	}
	if got, want := len(out.CurrentCompare.Objects), 2; got != want {
		t.Fatalf("len(objects) = %d, want %d", got, want)
	}
	drifted := out.CurrentCompare.Objects[1]
	if got, want := drifted.Tracked.Name, "cilium"; got != want {
		t.Fatalf("drifted.tracked.name = %q, want %q", got, want)
	}
	if got, want := drifted.State, "Orphan"; got != want {
		t.Fatalf("drifted.state = %q, want %q", got, want)
	}
	if got, want := drifted.Comparison, "drifted"; got != want {
		t.Fatalf("drifted.comparison = %q, want %q", got, want)
	}
	if got, want := len(drifted.Mismatches), 1; got != want {
		t.Fatalf("len(drifted.mismatches) = %d, want %d", got, want)
	}
	if got, want := drifted.Mismatches[0].Path, "spec.template.spec.containers[name=cilium-agent].image"; got != want {
		t.Fatalf("drifted.mismatches[0].path = %q, want %q", got, want)
	}
	if got, want := drifted.Mismatches[0].Reason, "valueMismatch"; got != want {
		t.Fatalf("drifted.mismatches[0].reason = %q, want %q", got, want)
	}
	if got, want := drifted.Mismatches[0].Ownership, "global"; got != want {
		t.Fatalf("drifted.mismatches[0].ownership = %q, want %q", got, want)
	}
	if got, want := drifted.Mismatches[0].State, "Orphan"; got != want {
		t.Fatalf("drifted.mismatches[0].state = %q, want %q", got, want)
	}
	if got, want := drifted.Remediation.Action, "reviewDistributionBaselineForAppliedObject"; got != want {
		t.Fatalf("drifted.remediation.action = %q, want %q", got, want)
	}
	if got, want := drifted.Remediation.ChangeOwner, "globalBaseline"; got != want {
		t.Fatalf("drifted.remediation.changeOwner = %q, want %q", got, want)
	}
	if got, want := drifted.Remediation.AllowedCommands[2], "sync revert"; got != want {
		t.Fatalf("drifted.remediation.allowedCommands[2] = %q, want %q", got, want)
	}
	if got, want := drifted.Remediation.CommandGuidance[2].Command, "sync revert"; got != want {
		t.Fatalf("drifted.remediation.commandGuidance[2].command = %q, want %q", got, want)
	}
	if got, want := drifted.Remediation.CommandGuidance[2].Availability, "available"; got != want {
		t.Fatalf("drifted.remediation.commandGuidance[2].availability = %q, want %q", got, want)
	}
}

func TestSyncDiffCmdWithPolicyEligibleOrphanObject(t *testing.T) {
	previousRuntimeRoot := constants.DefaultRuntimeRootDir
	runtimeRoot := previousRuntimeRoot
	if runtimeRoot == "" {
		runtimeRoot = constants.GetRuntimeRootDir(constants.AppName)
	}
	constants.DefaultRuntimeRootDir = runtimeRoot
	t.Cleanup(func() {
		constants.DefaultRuntimeRootDir = previousRuntimeRoot
	})

	previousOutput := outputSyncKubectl
	outputSyncKubectl = func(args ...string) ([]byte, error) {
		command := strings.Join(args, " ")
		switch {
		case strings.Contains(command, "ConfigMap cilium-config"):
			return []byte(`{"apiVersion":"v1","kind":"ConfigMap","metadata":{"name":"cilium-config","namespace":"kube-system","resourceVersion":"9"},"data":{"enable-hubble":"true"}}`), nil
		default:
			return nil, fmt.Errorf("unexpected kubectl invocation: %s", command)
		}
	}
	t.Cleanup(func() {
		outputSyncKubectl = previousOutput
	})

	clusterName := uniqueSyncClusterName(t, "diff-policy-eligible")
	t.Cleanup(func() {
		_ = os.RemoveAll(filepath.Join(runtimeRoot, clusterName))
	})
	bundleDir := t.TempDir()
	bundle := &hydrate.Bundle{
		APIVersion: "distribution.sealos.io/v1alpha1",
		Kind:       "HydratedBundle",
		Spec: hydrate.BundleSpec{
			BOMName:                "minimal-single-node",
			Revision:               "rev-poc-001",
			Channel:                bom.ChannelAlpha,
			LocalPatchPolicySource: ownership.LocalPatchPolicySourceLocalRepo,
			LocalPatchPolicyName:   "custom-local-patch-policy",
			LocalPatchPolicyPath:   hydrate.LocalPatchPolicyBundlePath,
			TrackedK8sObjects: []hydrate.TrackedK8sObject{
				{
					APIVersion: "v1",
					Kind:       "ConfigMap",
					Namespace:  "kube-system",
					Name:       "cilium-config",
					Path:       "components/cilium/files/manifests/cilium-config.yaml",
					Component:  "cilium",
					Source:     hydrate.InventorySourcePackageManifest,
					Ownership:  hydrate.InventoryOwnershipGlobal,
				},
			},
			Components: []hydrate.RenderedComponent{
				{Name: "cilium"},
			},
		},
	}
	localPatchPolicy := ownership.LocalPatchPolicyDocument{
		APIVersion: ownership.LocalPatchPolicyAPIVersion,
		Kind:       ownership.LocalPatchPolicyKind,
		Metadata: ownership.PolicyMetadata{
			Name: "custom-local-patch-policy",
		},
		Spec: ownership.LocalPatchPolicy{
			ForbiddenExactPaths: []string{
				"status",
				"spec.selector",
			},
			ForbiddenMetadataKeys: []string{
				"uid",
				"resourceVersion",
				"generation",
				"creationTimestamp",
				"managedFields",
				"ownerReferences",
				"finalizers",
				"generateName",
				"selfLink",
				"deletionTimestamp",
				"deletionGracePeriodSeconds",
			},
			ForbiddenContainerFields: []string{
				"image",
			},
			KindRules: []ownership.LocalPatchKindRule{
				{
					Kind:            "ConfigMap",
					AllowedPrefixes: []string{"data"},
				},
			},
		},
	}
	policyPath := filepath.Join(bundleDir, filepath.FromSlash(hydrate.LocalPatchPolicyBundlePath))
	if err := yamlutil.MarshalFile(policyPath, localPatchPolicy); err != nil {
		t.Fatalf("MarshalFile(policy) error = %v", err)
	}
	policyData, err := os.ReadFile(policyPath)
	if err != nil {
		t.Fatalf("ReadFile(policy) error = %v", err)
	}
	bundle.Spec.LocalPatchPolicyDigest = digest.Canonical.FromBytes(policyData).String()
	manifestPath := filepath.Join(bundleDir, "components", "cilium", "files", "manifests", "cilium-config.yaml")
	if err := os.MkdirAll(filepath.Dir(manifestPath), 0o755); err != nil {
		t.Fatalf("MkdirAll(%q) error = %v", manifestPath, err)
	}
	if err := os.WriteFile(manifestPath, []byte("apiVersion: v1\nkind: ConfigMap\nmetadata:\n  name: cilium-config\n  namespace: kube-system\ndata:\n  enable-hubble: \"false\"\n"), 0o644); err != nil {
		t.Fatalf("WriteFile(%q) error = %v", manifestPath, err)
	}
	if err := yamlutil.MarshalFile(filepath.Join(bundleDir, hydrate.BundleFileName), bundle); err != nil {
		t.Fatalf("MarshalFile(bundle) error = %v", err)
	}

	desiredStateDigestDigest, err := hydrate.DigestBundle(bundleDir)
	if err != nil {
		t.Fatalf("DigestBundle() error = %v", err)
	}
	if _, err := state.PersistSuccessfulApply(clusterName, state.BOMReference{
		Name:     bundle.Spec.BOMName,
		Revision: bundle.Spec.Revision,
		Channel:  bundle.Spec.Channel,
	}, desiredStateDigestDigest.String(), "local-rev-1", "patch-rev-1"); err != nil {
		t.Fatalf("PersistSuccessfulApply() error = %v", err)
	}

	buf := bytes.NewBuffer(nil)
	cmd := newSyncCmd()
	cmd.SetOut(buf)
	cmd.SetErr(buf)
	cmd.SetArgs([]string{
		"diff",
		"--cluster", clusterName,
		"--bundle-dir", bundleDir,
		"--kubeconfig", "/tmp/test-kubeconfig",
	})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute() error = %v", err)
	}

	var out struct {
		CurrentState     string `yaml:"currentState"`
		Headline         string `yaml:"headline"`
		LocalPatchPolicy struct {
			Source string `yaml:"source"`
			Scope  string `yaml:"scope"`
			Name   string `yaml:"name"`
			Path   string `yaml:"path"`
			Digest string `yaml:"digest"`
		} `yaml:"localPatchPolicy"`
		OperatorActionSummary struct {
			DirectCommitEligible int `yaml:"directCommitEligible"`
			DirectRevertEligible int `yaml:"directRevertEligible"`
			BundleMatchRequired  int `yaml:"bundleMatchRequired"`
		} `yaml:"operatorActionSummary"`
		PolicyEligibleOrphanObjects []struct {
			Name                   string `yaml:"name"`
			OperatorAction         string `yaml:"operatorAction"`
			OperatorActionMetadata struct {
				AllowsDirectCommit  bool `yaml:"allowsDirectCommit"`
				AllowsDirectRevert  bool `yaml:"allowsDirectRevert"`
				RequiresBundleMatch bool `yaml:"requiresBundleMatch"`
			} `yaml:"operatorActionMetadata"`
			Paths       []string `yaml:"paths"`
			Remediation struct {
				PolicyName          string   `yaml:"policyName"`
				PolicyEligiblePaths []string `yaml:"policyEligiblePaths"`
			} `yaml:"remediation"`
		} `yaml:"policyEligibleOrphanObjects"`
		CurrentCompare struct {
			Objects []struct {
				Tracked struct {
					Name string `yaml:"name"`
				} `yaml:"tracked"`
				Mismatches []struct {
					Path           string `yaml:"path"`
					PolicyName     string `yaml:"policyName"`
					PolicyEligible bool   `yaml:"policyEligible"`
				} `yaml:"mismatches"`
			} `yaml:"objects"`
		} `yaml:"currentCompare"`
	}
	if err := yaml.Unmarshal(buf.Bytes(), &out); err != nil {
		t.Fatalf("Unmarshal() error = %v\noutput=%s", err, buf.String())
	}
	if got, want := out.CurrentState, "Orphan"; got != want {
		t.Fatalf("currentState = %q, want %q", got, want)
	}
	if got, want := out.LocalPatchPolicy.Source, "localRepo"; got != want {
		t.Fatalf("localPatchPolicy.source = %q, want %q", got, want)
	}
	if got, want := out.LocalPatchPolicy.Scope, "clusterLocal"; got != want {
		t.Fatalf("localPatchPolicy.scope = %q, want %q", got, want)
	}
	if got, want := out.LocalPatchPolicy.Name, "custom-local-patch-policy"; got != want {
		t.Fatalf("localPatchPolicy.name = %q, want %q", got, want)
	}
	if got, want := out.LocalPatchPolicy.Path, hydrate.LocalPatchPolicyBundlePath; got != want {
		t.Fatalf("localPatchPolicy.path = %q, want %q", got, want)
	}
	if got, want := out.LocalPatchPolicy.Digest, bundle.Spec.LocalPatchPolicyDigest; got != want {
		t.Fatalf("localPatchPolicy.digest = %q, want %q", got, want)
	}
	if got, want := out.Headline, "state=Orphan; dirtyObjects=0; orphanObjects=1; dirtyHostPaths=0; orphanHostPaths=0; directCommitEligible=0; directRevertEligible=0; bundleMatchRequired=0; policyEligibleOrphanObjects=1; hostPathConflicts=0; localInputHostSplits=0"; got != want {
		t.Fatalf("headline = %q, want %q", got, want)
	}
	if got, want := out.OperatorActionSummary.DirectCommitEligible, 0; got != want {
		t.Fatalf("operatorActionSummary.directCommitEligible = %d, want %d", got, want)
	}
	if got, want := out.OperatorActionSummary.DirectRevertEligible, 0; got != want {
		t.Fatalf("operatorActionSummary.directRevertEligible = %d, want %d", got, want)
	}
	if got, want := out.OperatorActionSummary.BundleMatchRequired, 0; got != want {
		t.Fatalf("operatorActionSummary.bundleMatchRequired = %d, want %d", got, want)
	}
	if got, want := len(out.PolicyEligibleOrphanObjects), 1; got != want {
		t.Fatalf("len(policyEligibleOrphanObjects) = %d, want %d", got, want)
	}
	if got, want := out.PolicyEligibleOrphanObjects[0].Name, "cilium-config"; got != want {
		t.Fatalf("policyEligibleOrphanObjects[0].name = %q, want %q", got, want)
	}
	if got, want := out.PolicyEligibleOrphanObjects[0].OperatorAction, "promoteToLocalPatch"; got != want {
		t.Fatalf("policyEligibleOrphanObjects[0].operatorAction = %q, want %q", got, want)
	}
	if got, want := out.PolicyEligibleOrphanObjects[0].OperatorActionMetadata.AllowsDirectCommit, false; got != want {
		t.Fatalf("policyEligibleOrphanObjects[0].operatorActionMetadata.allowsDirectCommit = %t, want %t", got, want)
	}
	if got, want := out.PolicyEligibleOrphanObjects[0].OperatorActionMetadata.AllowsDirectRevert, false; got != want {
		t.Fatalf("policyEligibleOrphanObjects[0].operatorActionMetadata.allowsDirectRevert = %t, want %t", got, want)
	}
	if got, want := out.PolicyEligibleOrphanObjects[0].OperatorActionMetadata.RequiresBundleMatch, false; got != want {
		t.Fatalf("policyEligibleOrphanObjects[0].operatorActionMetadata.requiresBundleMatch = %t, want %t", got, want)
	}
	if got, want := out.PolicyEligibleOrphanObjects[0].Paths[0], "data.enable-hubble"; got != want {
		t.Fatalf("policyEligibleOrphanObjects[0].paths[0] = %q, want %q", got, want)
	}
	if got, want := out.PolicyEligibleOrphanObjects[0].Remediation.PolicyName, "custom-local-patch-policy"; got != want {
		t.Fatalf("policyEligibleOrphanObjects[0].remediation.policyName = %q, want %q", got, want)
	}
	if got, want := out.PolicyEligibleOrphanObjects[0].Remediation.PolicyEligiblePaths[0], "data.enable-hubble"; got != want {
		t.Fatalf("policyEligibleOrphanObjects[0].remediation.policyEligiblePaths[0] = %q, want %q", got, want)
	}
	if got, want := len(out.CurrentCompare.Objects), 1; got != want {
		t.Fatalf("len(currentCompare.objects) = %d, want %d", got, want)
	}
	if got, want := out.CurrentCompare.Objects[0].Tracked.Name, "cilium-config"; got != want {
		t.Fatalf("currentCompare.objects[0].tracked.name = %q, want %q", got, want)
	}
	if got, want := out.CurrentCompare.Objects[0].Mismatches[0].PolicyName, "custom-local-patch-policy"; got != want {
		t.Fatalf("currentCompare.objects[0].mismatches[0].policyName = %q, want %q", got, want)
	}
	if got, want := out.CurrentCompare.Objects[0].Mismatches[0].PolicyEligible, true; got != want {
		t.Fatalf("currentCompare.objects[0].mismatches[0].policyEligible = %t, want %t", got, want)
	}
}

func TestSyncDiffCmdWithHostPathDrift(t *testing.T) {
	previousRuntimeRoot := constants.DefaultRuntimeRootDir
	runtimeRoot := previousRuntimeRoot
	if runtimeRoot == "" {
		runtimeRoot = constants.GetRuntimeRootDir(constants.AppName)
	}
	constants.DefaultRuntimeRootDir = runtimeRoot
	t.Cleanup(func() {
		constants.DefaultRuntimeRootDir = previousRuntimeRoot
	})

	previousOutput := outputSyncKubectl
	outputSyncKubectl = func(args ...string) ([]byte, error) {
		t.Fatalf("unexpected kubectl invocation: %s", strings.Join(args, " "))
		return nil, nil
	}
	t.Cleanup(func() {
		outputSyncKubectl = previousOutput
	})

	clusterName := uniqueSyncClusterName(t, "diff-hostpath")
	t.Cleanup(func() {
		_ = os.RemoveAll(filepath.Join(runtimeRoot, clusterName))
	})
	bundleDir := t.TempDir()
	hostRoot := t.TempDir()

	desiredPath := filepath.Join(bundleDir, "components", "kubernetes", "files", "rootfs", "usr", "bin", "kubelet")
	if err := os.MkdirAll(filepath.Dir(desiredPath), 0o755); err != nil {
		t.Fatalf("MkdirAll(desired) error = %v", err)
	}
	if err := os.WriteFile(desiredPath, []byte("desired-kubelet"), 0o755); err != nil {
		t.Fatalf("WriteFile(desired) error = %v", err)
	}

	livePath := filepath.Join(hostRoot, "usr", "bin", "kubelet")
	if err := os.MkdirAll(filepath.Dir(livePath), 0o755); err != nil {
		t.Fatalf("MkdirAll(live) error = %v", err)
	}
	if err := os.WriteFile(livePath, []byte("drifted-kubelet"), 0o755); err != nil {
		t.Fatalf("WriteFile(live) error = %v", err)
	}

	bundle := &hydrate.Bundle{
		APIVersion: "distribution.sealos.io/v1alpha1",
		Kind:       "HydratedBundle",
		Spec: hydrate.BundleSpec{
			BOMName:  "minimal-single-node",
			Revision: "rev-poc-001",
			Channel:  bom.ChannelAlpha,
			TrackedHostPaths: []hydrate.TrackedHostPath{
				{
					HostPath:   "/usr/bin/kubelet",
					BundlePath: "components/kubernetes/files/rootfs/usr/bin/kubelet",
					Component:  "kubernetes",
					Source:     hydrate.InventorySourcePackageManifest,
					Ownership:  hydrate.InventoryOwnershipGlobal,
					Type:       hydrate.HostPathRegularFile,
				},
			},
			Components: []hydrate.RenderedComponent{
				{Name: "kubernetes"},
			},
		},
	}
	if err := yamlutil.MarshalFile(filepath.Join(bundleDir, hydrate.BundleFileName), bundle); err != nil {
		t.Fatalf("MarshalFile(bundle) error = %v", err)
	}
	desiredStateDigest, err := hydrate.DigestBundle(bundleDir)
	if err != nil {
		t.Fatalf("DigestBundle() error = %v", err)
	}

	if _, err := state.PersistSuccessfulApply(clusterName, state.BOMReference{
		Name:     bundle.Spec.BOMName,
		Revision: bundle.Spec.Revision,
		Channel:  bundle.Spec.Channel,
	}, desiredStateDigest.String(), "local-rev-1", "patch-rev-1"); err != nil {
		t.Fatalf("PersistSuccessfulApply() error = %v", err)
	}

	buf := bytes.NewBuffer(nil)
	cmd := newSyncCmd()
	cmd.SetOut(buf)
	cmd.SetErr(buf)
	cmd.SetArgs([]string{
		"diff",
		"--cluster", clusterName,
		"--bundle-dir", bundleDir,
		"--kubeconfig", "/tmp/test-kubeconfig",
		"--host-root", hostRoot,
	})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute() error = %v", err)
	}

	var out struct {
		CurrentState   string `yaml:"currentState"`
		CurrentCompare struct {
			Summary struct {
				Total   int `yaml:"total"`
				Present int `yaml:"present"`
				Drifted int `yaml:"drifted"`
				Orphan  int `yaml:"orphan"`
			} `yaml:"summary"`
			HostPaths []struct {
				Tracked struct {
					HostPath string `yaml:"hostPath"`
				} `yaml:"tracked"`
				State      string `yaml:"state"`
				Comparison string `yaml:"comparison"`
				Mismatches []struct {
					Reason string `yaml:"reason"`
				} `yaml:"mismatches"`
				Remediation struct {
					Action          string   `yaml:"action"`
					ChangeOwner     string   `yaml:"changeOwner"`
					AllowedCommands []string `yaml:"allowedCommands"`
					CommandGuidance []struct {
						Command      string `yaml:"command"`
						Availability string `yaml:"availability"`
					} `yaml:"commandGuidance"`
				} `yaml:"remediation"`
			} `yaml:"hostPaths"`
		} `yaml:"currentCompare"`
	}
	if err := yaml.Unmarshal(buf.Bytes(), &out); err != nil {
		t.Fatalf("Unmarshal() error = %v\noutput=%s", err, buf.String())
	}
	if got, want := out.CurrentState, "Orphan"; got != want {
		t.Fatalf("currentState = %q, want %q", got, want)
	}
	if got, want := out.CurrentCompare.Summary.Total, 1; got != want {
		t.Fatalf("summary.total = %d, want %d", got, want)
	}
	if got, want := out.CurrentCompare.Summary.Present, 1; got != want {
		t.Fatalf("summary.present = %d, want %d", got, want)
	}
	if got, want := out.CurrentCompare.Summary.Drifted, 1; got != want {
		t.Fatalf("summary.drifted = %d, want %d", got, want)
	}
	if got, want := out.CurrentCompare.Summary.Orphan, 1; got != want {
		t.Fatalf("summary.orphan = %d, want %d", got, want)
	}
	if got, want := len(out.CurrentCompare.HostPaths), 1; got != want {
		t.Fatalf("len(hostPaths) = %d, want %d", got, want)
	}
	if got, want := out.CurrentCompare.HostPaths[0].Tracked.HostPath, "/usr/bin/kubelet"; got != want {
		t.Fatalf("hostPaths[0].tracked.hostPath = %q, want %q", got, want)
	}
	if got, want := out.CurrentCompare.HostPaths[0].State, "Orphan"; got != want {
		t.Fatalf("hostPaths[0].state = %q, want %q", got, want)
	}
	if got, want := out.CurrentCompare.HostPaths[0].Comparison, "drifted"; got != want {
		t.Fatalf("hostPaths[0].comparison = %q, want %q", got, want)
	}
	if got, want := out.CurrentCompare.HostPaths[0].Mismatches[0].Reason, "contentMismatch"; got != want {
		t.Fatalf("hostPaths[0].mismatches[0].reason = %q, want %q", got, want)
	}
	if got, want := out.CurrentCompare.HostPaths[0].Remediation.Action, "reviewDistributionBaselineForHostPath"; got != want {
		t.Fatalf("hostPaths[0].remediation.action = %q, want %q", got, want)
	}
	if got, want := out.CurrentCompare.HostPaths[0].Remediation.ChangeOwner, "globalBaseline"; got != want {
		t.Fatalf("hostPaths[0].remediation.changeOwner = %q, want %q", got, want)
	}
	if got, want := out.CurrentCompare.HostPaths[0].Remediation.CommandGuidance[2].Command, "sync revert"; got != want {
		t.Fatalf("hostPaths[0].remediation.commandGuidance[2].command = %q, want %q", got, want)
	}
	if got, want := out.CurrentCompare.HostPaths[0].Remediation.CommandGuidance[2].Availability, "available"; got != want {
		t.Fatalf("hostPaths[0].remediation.commandGuidance[2].availability = %q, want %q", got, want)
	}
}

func TestSyncDiffCmdWithGeneratedHostPathRemediation(t *testing.T) {
	previousRuntimeRoot := constants.DefaultRuntimeRootDir
	runtimeRoot := previousRuntimeRoot
	if runtimeRoot == "" {
		runtimeRoot = constants.GetRuntimeRootDir(constants.AppName)
	}
	constants.DefaultRuntimeRootDir = runtimeRoot
	t.Cleanup(func() {
		constants.DefaultRuntimeRootDir = previousRuntimeRoot
	})

	previousOutput := outputSyncKubectl
	outputSyncKubectl = func(args ...string) ([]byte, error) {
		t.Fatalf("unexpected kubectl invocation: %s", strings.Join(args, " "))
		return nil, nil
	}
	t.Cleanup(func() {
		outputSyncKubectl = previousOutput
	})

	clusterName := uniqueSyncClusterName(t, "diff-generated-hostpath")
	t.Cleanup(func() {
		_ = os.RemoveAll(filepath.Join(runtimeRoot, clusterName))
	})
	bundleDir := t.TempDir()
	hostRoot := t.TempDir()
	livePath := filepath.Join(hostRoot, "etc", "kubernetes", "manifests", "kube-scheduler.yaml")
	if err := os.MkdirAll(filepath.Dir(livePath), 0o755); err != nil {
		t.Fatalf("MkdirAll(live) error = %v", err)
	}
	if err := os.WriteFile(livePath, []byte("apiVersion: v1\nkind: Pod\nmetadata:\n  name: kube-scheduler\n  namespace: kube-system\nspec:\n  containers:\n    - name: kube-scheduler\n      image: registry.k8s.io/kube-scheduler:v1.30.3\n      command:\n        - kube-scheduler\n"), 0o644); err != nil {
		t.Fatalf("WriteFile(live) error = %v", err)
	}

	bundle := &hydrate.Bundle{
		APIVersion: "distribution.sealos.io/v1alpha1",
		Kind:       "HydratedBundle",
		Spec: hydrate.BundleSpec{
			BOMName:  "minimal-single-node",
			Revision: "rev-poc-001",
			Channel:  bom.ChannelAlpha,
			TrackedHostPaths: []hydrate.TrackedHostPath{{
				HostPath:        "/etc/kubernetes/manifests/kube-scheduler.yaml",
				Component:       "kubernetes",
				Source:          hydrate.InventorySourceGeneratedHook,
				Ownership:       hydrate.InventoryOwnershipGlobal,
				Type:            hydrate.HostPathRegularFile,
				ProjectionClass: hydrate.HostPathProjectionClassGenerated,
				CompareStrategy: hydrate.HostPathCompareStrategySemanticGenerated,
				Generated: &hydrate.GeneratedHostPathSemantics{
					Tool:            "kubeadm",
					Hook:            "bootstrap",
					APIVersion:      "v1",
					Kind:            "Pod",
					Namespace:       "kube-system",
					Name:            "kube-scheduler",
					ContainerName:   "kube-scheduler",
					ExpectedImage:   "registry.k8s.io/kube-scheduler:v1.30.3",
					ExpectedCommand: "kube-scheduler",
					ExpectedArgs: map[string]string{
						"bind-address": "0.0.0.0",
					},
					ExpectedVolumeMounts: []string{"/etc/localtime"},
				},
			}},
			Components: []hydrate.RenderedComponent{{Name: "kubernetes"}},
		},
	}
	if err := yamlutil.MarshalFile(filepath.Join(bundleDir, hydrate.BundleFileName), bundle); err != nil {
		t.Fatalf("MarshalFile(bundle) error = %v", err)
	}

	if _, err := state.PersistSuccessfulApply(clusterName, state.BOMReference{
		Name:     bundle.Spec.BOMName,
		Revision: bundle.Spec.Revision,
		Channel:  bundle.Spec.Channel,
	}, "sha256:ffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffff", "local-rev-1", "patch-rev-1"); err != nil {
		t.Fatalf("PersistSuccessfulApply() error = %v", err)
	}

	buf := bytes.NewBuffer(nil)
	cmd := newSyncCmd()
	cmd.SetOut(buf)
	cmd.SetErr(buf)
	cmd.SetArgs([]string{
		"diff",
		"--cluster", clusterName,
		"--bundle-dir", bundleDir,
		"--kubeconfig", "/tmp/test-kubeconfig",
		"--host-root", hostRoot,
	})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute() error = %v", err)
	}

	var out struct {
		CurrentCompare struct {
			HostPaths []struct {
				Remediation struct {
					Action          string   `yaml:"action"`
					ChangeOwner     string   `yaml:"changeOwner"`
					NextSteps       []string `yaml:"nextSteps"`
					AllowedCommands []string `yaml:"allowedCommands"`
					CommandGuidance []struct {
						Command       string   `yaml:"command"`
						Preconditions []string `yaml:"preconditions"`
						Availability  string   `yaml:"availability"`
						Reason        string   `yaml:"reason"`
					} `yaml:"commandGuidance"`
				} `yaml:"remediation"`
			} `yaml:"hostPaths"`
		} `yaml:"currentCompare"`
	}
	if err := yaml.Unmarshal(buf.Bytes(), &out); err != nil {
		t.Fatalf("Unmarshal() error = %v\noutput=%s", err, buf.String())
	}
	if got, want := len(out.CurrentCompare.HostPaths), 1; got != want {
		t.Fatalf("len(hostPaths) = %d, want %d", got, want)
	}
	if got, want := out.CurrentCompare.HostPaths[0].Remediation.Action, "updateLocalBootstrapInputAndRerender"; got != want {
		t.Fatalf("hostPaths[0].remediation.action = %q, want %q", got, want)
	}
	if got, want := out.CurrentCompare.HostPaths[0].Remediation.ChangeOwner, "localInput"; got != want {
		t.Fatalf("hostPaths[0].remediation.changeOwner = %q, want %q", got, want)
	}
	if got, want := out.CurrentCompare.HostPaths[0].Remediation.NextSteps[0], "Update the cluster-local bootstrap input that feeds the rendered kubeadm config."; got != want {
		t.Fatalf("hostPaths[0].remediation.nextSteps[0] = %q, want %q", got, want)
	}
	if got, want := out.CurrentCompare.HostPaths[0].Remediation.AllowedCommands[0], "sync render"; got != want {
		t.Fatalf("hostPaths[0].remediation.allowedCommands[0] = %q, want %q", got, want)
	}
	if got, want := len(out.CurrentCompare.HostPaths[0].Remediation.CommandGuidance), 4; got != want {
		t.Fatalf("len(hostPaths[0].remediation.commandGuidance) = %d, want %d", got, want)
	}
	if got, want := out.CurrentCompare.HostPaths[0].Remediation.CommandGuidance[3].Command, "sync apply"; got != want {
		t.Fatalf("hostPaths[0].remediation.commandGuidance[3].command = %q, want %q", got, want)
	}
	if got, want := out.CurrentCompare.HostPaths[0].Remediation.CommandGuidance[3].Availability, "blocked"; got != want {
		t.Fatalf("hostPaths[0].remediation.commandGuidance[3].availability = %q, want %q", got, want)
	}
	if got, want := out.CurrentCompare.HostPaths[0].Remediation.CommandGuidance[3].Preconditions[0], "bundleMatchesRecordedDesiredStateDigest"; got != want {
		t.Fatalf("hostPaths[0].remediation.commandGuidance[3].preconditions[0] = %q, want %q", got, want)
	}
}

func TestSyncStatusCmd(t *testing.T) {
	previousRuntimeRoot := constants.DefaultRuntimeRootDir
	runtimeRoot := previousRuntimeRoot
	if runtimeRoot == "" {
		runtimeRoot = constants.GetRuntimeRootDir(constants.AppName)
	}
	constants.DefaultRuntimeRootDir = runtimeRoot
	t.Cleanup(func() {
		constants.DefaultRuntimeRootDir = previousRuntimeRoot
	})

	previousOutput := outputSyncKubectl
	outputSyncKubectl = func(args ...string) ([]byte, error) {
		command := strings.Join(args, " ")
		switch {
		case strings.Contains(command, "Secret grafana-admin-credentials"):
			return []byte(`{"apiVersion":"v1","kind":"Secret","metadata":{"name":"grafana-admin-credentials","namespace":"default","resourceVersion":"1"},"data":{"username":"YWRtaW4=","password":"cGFzc3cwcmQ="},"type":"Opaque"}`), nil
		case strings.Contains(command, "ConfigMap grafana-settings"):
			return []byte(`{"apiVersion":"v1","kind":"ConfigMap","metadata":{"name":"grafana-settings","namespace":"default"},"data":{"adminUser":"admin","imageTag":"10.4.0"}}`), nil
		case strings.Contains(command, "ConfigMap cilium-config"):
			return []byte(`{"apiVersion":"v1","kind":"ConfigMap","metadata":{"name":"cilium-config","namespace":"kube-system"},"data":{"enable-hubble":"true"}}`), nil
		default:
			return []byte(`{"apiVersion":"apps/v1","kind":"DaemonSet","metadata":{"name":"cilium","namespace":"kube-system","resourceVersion":"2"},"spec":{"template":{"spec":{"containers":[{"name":"cilium-agent","image":"quay.io/cilium/cilium:v1.15.1"}]}}}}`), nil
		}
	}
	t.Cleanup(func() {
		outputSyncKubectl = previousOutput
	})

	clusterName := uniqueSyncClusterName(t, "status")
	t.Cleanup(func() {
		_ = os.RemoveAll(filepath.Join(runtimeRoot, clusterName))
	})
	bundleDir := t.TempDir()
	bundle := &hydrate.Bundle{
		APIVersion: "distribution.sealos.io/v1alpha1",
		Kind:       "HydratedBundle",
		Spec: hydrate.BundleSpec{
			BOMName:  "minimal-single-node",
			Revision: "rev-poc-001",
			Channel:  bom.ChannelAlpha,
			TrackedK8sObjects: []hydrate.TrackedK8sObject{
				{
					APIVersion: "v1",
					Kind:       "Secret",
					Namespace:  "default",
					Name:       "grafana-admin-credentials",
					Path:       "local-resources/grafana-admin-secret.yaml",
					Source:     hydrate.InventorySourceLocalResource,
					Ownership:  hydrate.InventoryOwnershipLocal,
				},
				{
					APIVersion: "v1",
					Kind:       "ConfigMap",
					Namespace:  "default",
					Name:       "grafana-settings",
					Path:       "components/grafana/files/manifests/grafana-settings.yaml",
					Component:  "grafana",
					Source:     hydrate.InventorySourcePackageManifest,
					Ownership:  hydrate.InventoryOwnershipGlobal,
				},
				{
					APIVersion: "v1",
					Kind:       "ConfigMap",
					Namespace:  "default",
					Name:       "grafana-settings",
					Path:       "local-resources/grafana-settings-local.yaml",
					Source:     hydrate.InventorySourceLocalResource,
					Ownership:  hydrate.InventoryOwnershipLocal,
				},
				{
					APIVersion: "v1",
					Kind:       "ConfigMap",
					Namespace:  "kube-system",
					Name:       "cilium-config",
					Path:       "components/cilium/files/manifests/cilium-config.yaml",
					Component:  "cilium",
					Source:     hydrate.InventorySourcePackageManifest,
					Ownership:  hydrate.InventoryOwnershipGlobal,
				},
				{
					APIVersion: "apps/v1",
					Kind:       "DaemonSet",
					Namespace:  "kube-system",
					Name:       "cilium",
					Path:       "components/cilium/files/manifests/cilium.yaml",
					Component:  "cilium",
					Source:     hydrate.InventorySourcePackageManifest,
					Ownership:  hydrate.InventoryOwnershipGlobal,
				},
			},
			Components: []hydrate.RenderedComponent{
				{Name: "grafana"},
				{Name: "cilium"},
			},
		},
	}
	for rel, content := range map[string]string{
		"local-resources/grafana-admin-secret.yaml":                "apiVersion: v1\nkind: Secret\nmetadata:\n  name: grafana-admin-credentials\n  namespace: default\nstringData:\n  username: admin\n  password: passw0rd\n",
		"components/grafana/files/manifests/grafana-settings.yaml": "apiVersion: v1\nkind: ConfigMap\nmetadata:\n  name: grafana-settings\n  namespace: default\ndata:\n  adminUser: admin\n  imageTag: \"10.4.0\"\n",
		"local-resources/grafana-settings-local.yaml":              "apiVersion: v1\nkind: ConfigMap\nmetadata:\n  name: grafana-settings\n  namespace: default\ndata:\n  adminUser: root\n",
		"components/cilium/files/manifests/cilium-config.yaml":     "apiVersion: v1\nkind: ConfigMap\nmetadata:\n  name: cilium-config\n  namespace: kube-system\ndata:\n  enable-hubble: \"false\"\n",
		"components/cilium/files/manifests/cilium.yaml":            "apiVersion: apps/v1\nkind: DaemonSet\nmetadata:\n  name: cilium\n  namespace: kube-system\nspec:\n  template:\n    spec:\n      containers:\n        - name: cilium-agent\n          image: quay.io/cilium/cilium:v1.15.0\n",
	} {
		path := filepath.Join(bundleDir, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatalf("MkdirAll(%q) error = %v", rel, err)
		}
		if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
			t.Fatalf("WriteFile(%q) error = %v", rel, err)
		}
	}
	if err := yamlutil.MarshalFile(filepath.Join(bundleDir, hydrate.BundleFileName), bundle); err != nil {
		t.Fatalf("MarshalFile(bundle) error = %v", err)
	}

	desiredStateDigestDigest, err := hydrate.DigestBundle(bundleDir)
	if err != nil {
		t.Fatalf("DigestBundle() error = %v", err)
	}
	desiredStateDigest := desiredStateDigestDigest.String()
	if _, err := state.PersistSuccessfulApply(clusterName, state.BOMReference{
		Name:     bundle.Spec.BOMName,
		Revision: bundle.Spec.Revision,
		Channel:  bundle.Spec.Channel,
	}, desiredStateDigest, "local-rev-1", "patch-rev-1"); err != nil {
		t.Fatalf("PersistSuccessfulApply() error = %v", err)
	}
	appliedRevisionPath := state.AppliedRevisionPath(clusterName)
	if _, err := os.Stat(appliedRevisionPath); err != nil {
		t.Fatalf("applied revision missing at %q: %v", appliedRevisionPath, err)
	}
	if _, err := state.LoadAppliedRevision(clusterName); err != nil {
		t.Fatalf("LoadAppliedRevision() precondition error = %v", err)
	}

	buf := bytes.NewBuffer(nil)
	constants.DefaultRuntimeRootDir = runtimeRoot
	cmd := newSyncCmd()
	cmd.SetOut(buf)
	cmd.SetErr(buf)
	cmd.SetArgs([]string{
		"status",
		"--cluster", clusterName,
		"--bundle-dir", bundleDir,
		"--kubeconfig", "/tmp/test-kubeconfig",
	})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute() error = %v", err)
	}

	var out struct {
		RecordedState string   `yaml:"recordedState"`
		CurrentState  string   `yaml:"currentState"`
		Headline      string   `yaml:"headline"`
		Warnings      []string `yaml:"warnings"`
		Preflight     struct {
			State             string `yaml:"state"`
			Summary           string `yaml:"summary"`
			RecommendedAction string `yaml:"recommendedAction"`
			RefreshCommand    string `yaml:"refreshCommand"`
			Blocked           bool   `yaml:"blocked"`
		} `yaml:"preflight"`
		LocalPatchPolicy struct {
			Source string `yaml:"source"`
			Scope  string `yaml:"scope"`
			Name   string `yaml:"name"`
			Path   string `yaml:"path"`
			Digest string `yaml:"digest"`
		} `yaml:"localPatchPolicy"`
		OperatorActionSummary struct {
			DirectCommitEligible int `yaml:"directCommitEligible"`
			DirectRevertEligible int `yaml:"directRevertEligible"`
			BundleMatchRequired  int `yaml:"bundleMatchRequired"`
		} `yaml:"operatorActionSummary"`
		RecordedObservedSummary struct {
			Orphan               int `yaml:"orphan"`
			MixedOwnershipObject int `yaml:"mixedOwnershipObject"`
			DirectCommitEligible int `yaml:"directCommitEligible"`
			DirectRevertEligible int `yaml:"directRevertEligible"`
			BundleMatchRequired  int `yaml:"bundleMatchRequired"`
		} `yaml:"recordedObservedSummary"`
		Summary struct {
			Total  int `yaml:"total"`
			Clean  int `yaml:"clean"`
			Dirty  int `yaml:"dirty"`
			Orphan int `yaml:"orphan"`
		} `yaml:"summary"`
		MixedOwnershipObjects []struct {
			Name       string   `yaml:"name"`
			Ownerships []string `yaml:"ownerships"`
		} `yaml:"mixedOwnershipObjects"`
		DirtyObjects []struct {
			Name                   string `yaml:"name"`
			OperatorAction         string `yaml:"operatorAction"`
			OperatorActionMetadata struct {
				AllowsDirectCommit  bool `yaml:"allowsDirectCommit"`
				AllowsDirectRevert  bool `yaml:"allowsDirectRevert"`
				RequiresBundleMatch bool `yaml:"requiresBundleMatch"`
			} `yaml:"operatorActionMetadata"`
			Paths       []string `yaml:"paths"`
			Remediation struct {
				Action          string `yaml:"action"`
				ChangeOwner     string `yaml:"changeOwner"`
				CommandGuidance []struct {
					Command      string `yaml:"command"`
					Availability string `yaml:"availability"`
				} `yaml:"commandGuidance"`
			} `yaml:"remediation"`
		} `yaml:"dirtyObjects"`
		OrphanObjects []struct {
			Name                   string `yaml:"name"`
			OperatorAction         string `yaml:"operatorAction"`
			OperatorActionMetadata struct {
				AllowsDirectCommit  bool `yaml:"allowsDirectCommit"`
				AllowsDirectRevert  bool `yaml:"allowsDirectRevert"`
				RequiresBundleMatch bool `yaml:"requiresBundleMatch"`
			} `yaml:"operatorActionMetadata"`
			Paths       []string `yaml:"paths"`
			Remediation struct {
				Action          string `yaml:"action"`
				ChangeOwner     string `yaml:"changeOwner"`
				CommandGuidance []struct {
					Command      string `yaml:"command"`
					Availability string `yaml:"availability"`
				} `yaml:"commandGuidance"`
			} `yaml:"remediation"`
		} `yaml:"orphanObjects"`
		PolicyEligibleOrphanObjects []struct {
			Name                   string `yaml:"name"`
			OperatorAction         string `yaml:"operatorAction"`
			OperatorActionMetadata struct {
				AllowsDirectCommit  bool `yaml:"allowsDirectCommit"`
				AllowsDirectRevert  bool `yaml:"allowsDirectRevert"`
				RequiresBundleMatch bool `yaml:"requiresBundleMatch"`
			} `yaml:"operatorActionMetadata"`
			Paths       []string `yaml:"paths"`
			Remediation struct {
				PolicyName          string   `yaml:"policyName"`
				PolicyEligiblePaths []string `yaml:"policyEligiblePaths"`
			} `yaml:"remediation"`
		} `yaml:"policyEligibleOrphanObjects"`
	}
	if err := yaml.Unmarshal(buf.Bytes(), &out); err != nil {
		t.Fatalf("Unmarshal() error = %v\noutput=%s", err, buf.String())
	}
	if got, want := out.CurrentState, "Orphan"; got != want {
		t.Fatalf("currentState = %q, want %q", got, want)
	}
	if got, want := out.Preflight.State, "Warning"; got != want {
		t.Fatalf("preflight.state = %q, want %q", got, want)
	}
	if got, want := out.Preflight.RecommendedAction, "inspect"; got != want {
		t.Fatalf("preflight.recommendedAction = %q, want %q", got, want)
	}
	if got, want := out.Warnings, []string{syncSourcePreflightMissingWarning}; strings.Join(got, "\n") != strings.Join(want, "\n") {
		t.Fatalf("warnings = %#v, want %#v", got, want)
	}
	if out.Preflight.Blocked {
		t.Fatal("preflight.blocked = true, want false")
	}
	if out.Preflight.Summary == "" {
		t.Fatal("preflight.summary is empty")
	}
	if !strings.Contains(out.Preflight.RefreshCommand, "'sealos' 'sync' 'render'") {
		t.Fatalf("preflight.refreshCommand = %q, want sync render command", out.Preflight.RefreshCommand)
	}
	if got, want := out.RecordedState, "Orphan"; got != want {
		t.Fatalf("recordedState = %q, want %q\noutput=%s", got, want, buf.String())
	}
	if got, want := out.LocalPatchPolicy.Source, "builtInDefault"; got != want {
		t.Fatalf("localPatchPolicy.source = %q, want %q", got, want)
	}
	if got, want := out.LocalPatchPolicy.Scope, "clusterLocal"; got != want {
		t.Fatalf("localPatchPolicy.scope = %q, want %q", got, want)
	}
	if got, want := out.LocalPatchPolicy.Name, "defaultLocalPatchPolicy"; got != want {
		t.Fatalf("localPatchPolicy.name = %q, want %q", got, want)
	}
	if got := out.LocalPatchPolicy.Path; got != "" {
		t.Fatalf("localPatchPolicy.path = %q, want empty for legacy bundle fixture", got)
	}
	if got := out.LocalPatchPolicy.Digest; got != "" {
		t.Fatalf("localPatchPolicy.digest = %q, want empty for legacy bundle fixture", got)
	}
	if got, want := out.Headline, "state=Orphan; dirtyObjects=1; orphanObjects=2; dirtyHostPaths=0; orphanHostPaths=0; directCommitEligible=1; directRevertEligible=2; bundleMatchRequired=2; policyEligibleOrphanObjects=1; hostPathConflicts=0; localInputHostSplits=0"; got != want {
		t.Fatalf("headline = %q, want %q", got, want)
	}
	if got, want := out.RecordedObservedSummary.Orphan, 2; got != want {
		t.Fatalf("recordedObservedSummary.orphan = %d, want %d", got, want)
	}
	if got, want := out.RecordedObservedSummary.MixedOwnershipObject, 1; got != want {
		t.Fatalf("recordedObservedSummary.mixedOwnershipObject = %d, want %d", got, want)
	}
	if got, want := out.RecordedObservedSummary.DirectCommitEligible, 1; got != want {
		t.Fatalf("recordedObservedSummary.directCommitEligible = %d, want %d", got, want)
	}
	if got, want := out.RecordedObservedSummary.DirectRevertEligible, 2; got != want {
		t.Fatalf("recordedObservedSummary.directRevertEligible = %d, want %d", got, want)
	}
	if got, want := out.RecordedObservedSummary.BundleMatchRequired, 2; got != want {
		t.Fatalf("recordedObservedSummary.bundleMatchRequired = %d, want %d", got, want)
	}
	if got, want := out.Summary.Total, 4; got != want {
		t.Fatalf("summary.total = %d, want %d", got, want)
	}
	if got, want := out.Summary.Clean, 1; got != want {
		t.Fatalf("summary.clean = %d, want %d", got, want)
	}
	if got, want := out.Summary.Dirty, 1; got != want {
		t.Fatalf("summary.dirty = %d, want %d", got, want)
	}
	if got, want := out.Summary.Orphan, 2; got != want {
		t.Fatalf("summary.orphan = %d, want %d", got, want)
	}
	if got, want := out.OperatorActionSummary.DirectCommitEligible, 1; got != want {
		t.Fatalf("operatorActionSummary.directCommitEligible = %d, want %d", got, want)
	}
	if got, want := out.OperatorActionSummary.DirectRevertEligible, 2; got != want {
		t.Fatalf("operatorActionSummary.directRevertEligible = %d, want %d", got, want)
	}
	if got, want := out.OperatorActionSummary.BundleMatchRequired, 2; got != want {
		t.Fatalf("operatorActionSummary.bundleMatchRequired = %d, want %d", got, want)
	}
	if got, want := len(out.MixedOwnershipObjects), 1; got != want {
		t.Fatalf("len(mixedOwnershipObjects) = %d, want %d", got, want)
	}
	if got, want := out.MixedOwnershipObjects[0].Name, "grafana-settings"; got != want {
		t.Fatalf("mixedOwnershipObjects[0].name = %q, want %q", got, want)
	}
	if got, want := strings.Join(out.MixedOwnershipObjects[0].Ownerships, ","), "global,local"; got != want {
		t.Fatalf("mixedOwnershipObjects[0].ownerships = %q, want %q", got, want)
	}
	if got, want := len(out.DirtyObjects), 1; got != want {
		t.Fatalf("len(dirtyObjects) = %d, want %d", got, want)
	}
	if got, want := out.DirtyObjects[0].Name, "grafana-settings"; got != want {
		t.Fatalf("dirtyObjects[0].name = %q, want %q", got, want)
	}
	if got, want := out.DirtyObjects[0].Paths[0], "data.adminUser"; got != want {
		t.Fatalf("dirtyObjects[0].paths[0] = %q, want %q", got, want)
	}
	if got, want := out.DirtyObjects[0].Remediation.Action, "reviewLocalObjectOverlayAndCommitOrReapply"; got != want {
		t.Fatalf("dirtyObjects[0].remediation.action = %q, want %q", got, want)
	}
	if got, want := out.DirtyObjects[0].Remediation.ChangeOwner, "localOverlay"; got != want {
		t.Fatalf("dirtyObjects[0].remediation.changeOwner = %q, want %q", got, want)
	}
	if got, want := out.DirtyObjects[0].OperatorAction, "commitOrReapplyLocalOverlay"; got != want {
		t.Fatalf("dirtyObjects[0].operatorAction = %q, want %q", got, want)
	}
	if got, want := out.DirtyObjects[0].OperatorActionMetadata.AllowsDirectCommit, true; got != want {
		t.Fatalf("dirtyObjects[0].operatorActionMetadata.allowsDirectCommit = %t, want %t", got, want)
	}
	if got, want := out.DirtyObjects[0].OperatorActionMetadata.AllowsDirectRevert, true; got != want {
		t.Fatalf("dirtyObjects[0].operatorActionMetadata.allowsDirectRevert = %t, want %t", got, want)
	}
	if got, want := out.DirtyObjects[0].OperatorActionMetadata.RequiresBundleMatch, true; got != want {
		t.Fatalf("dirtyObjects[0].operatorActionMetadata.requiresBundleMatch = %t, want %t", got, want)
	}
	if got, want := out.DirtyObjects[0].Remediation.CommandGuidance[2].Command, "sync commit"; got != want {
		t.Fatalf("dirtyObjects[0].remediation.commandGuidance[2].command = %q, want %q", got, want)
	}
	if got, want := out.DirtyObjects[0].Remediation.CommandGuidance[2].Availability, "available"; got != want {
		t.Fatalf("dirtyObjects[0].remediation.commandGuidance[2].availability = %q, want %q", got, want)
	}
	if got, want := len(out.OrphanObjects), 2; got != want {
		t.Fatalf("len(orphanObjects) = %d, want %d", got, want)
	}
	if got, want := out.OrphanObjects[0].Name, "cilium-config"; got != want {
		t.Fatalf("orphanObjects[0].name = %q, want %q", got, want)
	}
	if got, want := out.OrphanObjects[0].Paths[0], "data.enable-hubble"; got != want {
		t.Fatalf("orphanObjects[0].paths[0] = %q, want %q", got, want)
	}
	if got, want := out.OrphanObjects[0].Remediation.Action, "reviewDistributionBaselineForAppliedObject"; got != want {
		t.Fatalf("orphanObjects[0].remediation.action = %q, want %q", got, want)
	}
	if got, want := out.OrphanObjects[0].Remediation.ChangeOwner, "globalBaseline"; got != want {
		t.Fatalf("orphanObjects[0].remediation.changeOwner = %q, want %q", got, want)
	}
	if got, want := out.OrphanObjects[0].OperatorAction, "promoteToLocalPatch"; got != want {
		t.Fatalf("orphanObjects[0].operatorAction = %q, want %q", got, want)
	}
	if got, want := out.OrphanObjects[0].OperatorActionMetadata.AllowsDirectCommit, false; got != want {
		t.Fatalf("orphanObjects[0].operatorActionMetadata.allowsDirectCommit = %t, want %t", got, want)
	}
	if got, want := out.OrphanObjects[0].OperatorActionMetadata.AllowsDirectRevert, false; got != want {
		t.Fatalf("orphanObjects[0].operatorActionMetadata.allowsDirectRevert = %t, want %t", got, want)
	}
	if got, want := out.OrphanObjects[0].OperatorActionMetadata.RequiresBundleMatch, false; got != want {
		t.Fatalf("orphanObjects[0].operatorActionMetadata.requiresBundleMatch = %t, want %t", got, want)
	}
	if got, want := out.OrphanObjects[0].Remediation.CommandGuidance[2].Command, "sync revert"; got != want {
		t.Fatalf("orphanObjects[0].remediation.commandGuidance[2].command = %q, want %q", got, want)
	}
	if got, want := out.OrphanObjects[0].Remediation.CommandGuidance[2].Availability, "available"; got != want {
		t.Fatalf("orphanObjects[0].remediation.commandGuidance[2].availability = %q, want %q", got, want)
	}
	if got, want := out.OrphanObjects[1].Name, "cilium"; got != want {
		t.Fatalf("orphanObjects[1].name = %q, want %q", got, want)
	}
	if got, want := out.OrphanObjects[1].Paths[0], "spec.template.spec.containers[name=cilium-agent].image"; got != want {
		t.Fatalf("orphanObjects[1].paths[0] = %q, want %q", got, want)
	}
	if got, want := out.OrphanObjects[1].OperatorAction, "revertOrUpdateGlobalBaseline"; got != want {
		t.Fatalf("orphanObjects[1].operatorAction = %q, want %q", got, want)
	}
	if got, want := out.OrphanObjects[1].OperatorActionMetadata.AllowsDirectCommit, false; got != want {
		t.Fatalf("orphanObjects[1].operatorActionMetadata.allowsDirectCommit = %t, want %t", got, want)
	}
	if got, want := out.OrphanObjects[1].OperatorActionMetadata.AllowsDirectRevert, true; got != want {
		t.Fatalf("orphanObjects[1].operatorActionMetadata.allowsDirectRevert = %t, want %t", got, want)
	}
	if got, want := out.OrphanObjects[1].OperatorActionMetadata.RequiresBundleMatch, true; got != want {
		t.Fatalf("orphanObjects[1].operatorActionMetadata.requiresBundleMatch = %t, want %t", got, want)
	}
	if got, want := len(out.PolicyEligibleOrphanObjects), 1; got != want {
		t.Fatalf("len(policyEligibleOrphanObjects) = %d, want %d", got, want)
	}
	if got, want := out.PolicyEligibleOrphanObjects[0].Name, "cilium-config"; got != want {
		t.Fatalf("policyEligibleOrphanObjects[0].name = %q, want %q", got, want)
	}
	if got, want := out.PolicyEligibleOrphanObjects[0].OperatorAction, "promoteToLocalPatch"; got != want {
		t.Fatalf("policyEligibleOrphanObjects[0].operatorAction = %q, want %q", got, want)
	}
	if got, want := out.PolicyEligibleOrphanObjects[0].OperatorActionMetadata.AllowsDirectCommit, false; got != want {
		t.Fatalf("policyEligibleOrphanObjects[0].operatorActionMetadata.allowsDirectCommit = %t, want %t", got, want)
	}
	if got, want := out.PolicyEligibleOrphanObjects[0].OperatorActionMetadata.AllowsDirectRevert, false; got != want {
		t.Fatalf("policyEligibleOrphanObjects[0].operatorActionMetadata.allowsDirectRevert = %t, want %t", got, want)
	}
	if got, want := out.PolicyEligibleOrphanObjects[0].OperatorActionMetadata.RequiresBundleMatch, false; got != want {
		t.Fatalf("policyEligibleOrphanObjects[0].operatorActionMetadata.requiresBundleMatch = %t, want %t", got, want)
	}
	if got, want := out.PolicyEligibleOrphanObjects[0].Paths[0], "data.enable-hubble"; got != want {
		t.Fatalf("policyEligibleOrphanObjects[0].paths[0] = %q, want %q", got, want)
	}
	if got, want := out.PolicyEligibleOrphanObjects[0].Remediation.PolicyName, "defaultLocalPatchPolicy"; got != want {
		t.Fatalf("policyEligibleOrphanObjects[0].remediation.policyName = %q, want %q", got, want)
	}
	if got, want := out.PolicyEligibleOrphanObjects[0].Remediation.PolicyEligiblePaths[0], "data.enable-hubble"; got != want {
		t.Fatalf("policyEligibleOrphanObjects[0].remediation.policyEligiblePaths[0] = %q, want %q", got, want)
	}
}

func TestSyncStatusCmdWithGeneratedHostPathRemediation(t *testing.T) {
	previousRuntimeRoot := constants.DefaultRuntimeRootDir
	runtimeRoot := previousRuntimeRoot
	if runtimeRoot == "" {
		runtimeRoot = constants.GetRuntimeRootDir(constants.AppName)
	}
	constants.DefaultRuntimeRootDir = runtimeRoot
	t.Cleanup(func() {
		constants.DefaultRuntimeRootDir = previousRuntimeRoot
	})

	previousOutput := outputSyncKubectl
	outputSyncKubectl = func(args ...string) ([]byte, error) {
		t.Fatalf("unexpected kubectl invocation: %s", strings.Join(args, " "))
		return nil, nil
	}
	t.Cleanup(func() {
		outputSyncKubectl = previousOutput
	})

	clusterName := uniqueSyncClusterName(t, "status-generated-hostpath")
	t.Cleanup(func() {
		_ = os.RemoveAll(filepath.Join(runtimeRoot, clusterName))
	})
	bundleDir := t.TempDir()
	hostRoot := t.TempDir()

	bundle := &hydrate.Bundle{
		APIVersion: "distribution.sealos.io/v1alpha1",
		Kind:       "HydratedBundle",
		Spec: hydrate.BundleSpec{
			BOMName:  "minimal-single-node",
			Revision: "rev-poc-001",
			Channel:  bom.ChannelAlpha,
			TrackedHostPaths: []hydrate.TrackedHostPath{{
				HostPath:        "/etc/kubernetes/manifests/etcd.yaml",
				Component:       "kubernetes",
				Source:          hydrate.InventorySourceGeneratedHook,
				Ownership:       hydrate.InventoryOwnershipGlobal,
				Type:            hydrate.HostPathRegularFile,
				ProjectionClass: hydrate.HostPathProjectionClassGenerated,
				CompareStrategy: hydrate.HostPathCompareStrategySemanticGenerated,
				Generated: &hydrate.GeneratedHostPathSemantics{
					Tool:          "kubeadm",
					Hook:          "bootstrap",
					APIVersion:    "v1",
					Kind:          "Pod",
					Namespace:     "kube-system",
					Name:          "etcd",
					ContainerName: "etcd",
					ExpectedImage: "registry.k8s.io/etcd:3.5.12-0",
				},
			}},
			Components: []hydrate.RenderedComponent{{Name: "kubernetes"}},
		},
	}
	if err := yamlutil.MarshalFile(filepath.Join(bundleDir, hydrate.BundleFileName), bundle); err != nil {
		t.Fatalf("MarshalFile(bundle) error = %v", err)
	}

	desiredStateDigest, err := hydrate.DigestBundle(bundleDir)
	if err != nil {
		t.Fatalf("DigestBundle() error = %v", err)
	}
	if _, err := state.PersistSuccessfulApply(clusterName, state.BOMReference{
		Name:     bundle.Spec.BOMName,
		Revision: bundle.Spec.Revision,
		Channel:  bundle.Spec.Channel,
	}, desiredStateDigest.String(), "local-rev-1", "patch-rev-1"); err != nil {
		t.Fatalf("PersistSuccessfulApply() error = %v", err)
	}

	buf := bytes.NewBuffer(nil)
	cmd := newSyncCmd()
	cmd.SetOut(buf)
	cmd.SetErr(buf)
	cmd.SetArgs([]string{
		"status",
		"--cluster", clusterName,
		"--bundle-dir", bundleDir,
		"--kubeconfig", "/tmp/test-kubeconfig",
		"--host-root", hostRoot,
	})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute() error = %v", err)
	}

	var out struct {
		Headline              string `yaml:"headline"`
		OperatorActionSummary struct {
			DirectCommitEligible int `yaml:"directCommitEligible"`
			DirectRevertEligible int `yaml:"directRevertEligible"`
			BundleMatchRequired  int `yaml:"bundleMatchRequired"`
		} `yaml:"operatorActionSummary"`
		OrphanHostPaths []struct {
			Path                   string `yaml:"path"`
			OperatorAction         string `yaml:"operatorAction"`
			OperatorActionMetadata struct {
				AllowsDirectCommit  bool `yaml:"allowsDirectCommit"`
				AllowsDirectRevert  bool `yaml:"allowsDirectRevert"`
				RequiresBundleMatch bool `yaml:"requiresBundleMatch"`
			} `yaml:"operatorActionMetadata"`
			Remediation struct {
				Action          string   `yaml:"action"`
				ChangeOwner     string   `yaml:"changeOwner"`
				NextSteps       []string `yaml:"nextSteps"`
				AllowedCommands []string `yaml:"allowedCommands"`
				CommandGuidance []struct {
					Command       string   `yaml:"command"`
					Preconditions []string `yaml:"preconditions"`
					Availability  string   `yaml:"availability"`
					Reason        string   `yaml:"reason"`
				} `yaml:"commandGuidance"`
			} `yaml:"remediation"`
		} `yaml:"orphanHostPaths"`
	}
	if err := yaml.Unmarshal(buf.Bytes(), &out); err != nil {
		t.Fatalf("Unmarshal() error = %v\noutput=%s", err, buf.String())
	}
	if got, want := len(out.OrphanHostPaths), 1; got != want {
		t.Fatalf("len(orphanHostPaths) = %d, want %d", got, want)
	}
	if got, want := out.Headline, "state=Orphan; dirtyObjects=0; orphanObjects=0; dirtyHostPaths=0; orphanHostPaths=1; directCommitEligible=0; directRevertEligible=0; bundleMatchRequired=0; policyEligibleOrphanObjects=0; hostPathConflicts=0; localInputHostSplits=0"; got != want {
		t.Fatalf("headline = %q, want %q", got, want)
	}
	if got, want := out.OperatorActionSummary.DirectCommitEligible, 0; got != want {
		t.Fatalf("operatorActionSummary.directCommitEligible = %d, want %d", got, want)
	}
	if got, want := out.OperatorActionSummary.DirectRevertEligible, 0; got != want {
		t.Fatalf("operatorActionSummary.directRevertEligible = %d, want %d", got, want)
	}
	if got, want := out.OperatorActionSummary.BundleMatchRequired, 0; got != want {
		t.Fatalf("operatorActionSummary.bundleMatchRequired = %d, want %d", got, want)
	}
	if got, want := out.OrphanHostPaths[0].Path, "/etc/kubernetes/manifests/etcd.yaml"; got != want {
		t.Fatalf("orphanHostPaths[0].path = %q, want %q", got, want)
	}
	if got, want := out.OrphanHostPaths[0].OperatorAction, "rerenderOrUpdateGlobalBaseline"; got != want {
		t.Fatalf("orphanHostPaths[0].operatorAction = %q, want %q", got, want)
	}
	if got, want := out.OrphanHostPaths[0].OperatorActionMetadata.AllowsDirectCommit, false; got != want {
		t.Fatalf("orphanHostPaths[0].operatorActionMetadata.allowsDirectCommit = %t, want %t", got, want)
	}
	if got, want := out.OrphanHostPaths[0].OperatorActionMetadata.AllowsDirectRevert, false; got != want {
		t.Fatalf("orphanHostPaths[0].operatorActionMetadata.allowsDirectRevert = %t, want %t", got, want)
	}
	if got, want := out.OrphanHostPaths[0].OperatorActionMetadata.RequiresBundleMatch, false; got != want {
		t.Fatalf("orphanHostPaths[0].operatorActionMetadata.requiresBundleMatch = %t, want %t", got, want)
	}
	if got, want := out.OrphanHostPaths[0].Remediation.Action, "reviewDistributionBaselineForGeneratedProjection"; got != want {
		t.Fatalf("orphanHostPaths[0].remediation.action = %q, want %q", got, want)
	}
	if got, want := out.OrphanHostPaths[0].Remediation.ChangeOwner, "globalBaseline"; got != want {
		t.Fatalf("orphanHostPaths[0].remediation.changeOwner = %q, want %q", got, want)
	}
	if got, want := out.OrphanHostPaths[0].Remediation.NextSteps[0], "Review the selected BOM revision and package baseline that define this generated projection."; got != want {
		t.Fatalf("orphanHostPaths[0].remediation.nextSteps[0] = %q, want %q", got, want)
	}
	if got, want := out.OrphanHostPaths[0].Remediation.AllowedCommands[4], "sync package build"; got != want {
		t.Fatalf("orphanHostPaths[0].remediation.allowedCommands[4] = %q, want %q", got, want)
	}
	if got, want := len(out.OrphanHostPaths[0].Remediation.CommandGuidance), 6; got != want {
		t.Fatalf("len(orphanHostPaths[0].remediation.commandGuidance) = %d, want %d", got, want)
	}
	if got, want := out.OrphanHostPaths[0].Remediation.CommandGuidance[3].Command, "sync apply"; got != want {
		t.Fatalf("orphanHostPaths[0].remediation.commandGuidance[3].command = %q, want %q", got, want)
	}
	if got, want := out.OrphanHostPaths[0].Remediation.CommandGuidance[3].Availability, "available"; got != want {
		t.Fatalf("orphanHostPaths[0].remediation.commandGuidance[3].availability = %q, want %q", got, want)
	}
	if got, want := out.OrphanHostPaths[0].Remediation.CommandGuidance[4].Command, "sync package build"; got != want {
		t.Fatalf("orphanHostPaths[0].remediation.commandGuidance[4].command = %q, want %q", got, want)
	}
	if got, want := out.OrphanHostPaths[0].Remediation.CommandGuidance[4].Availability, "available"; got != want {
		t.Fatalf("orphanHostPaths[0].remediation.commandGuidance[4].availability = %q, want %q", got, want)
	}
}

func TestSyncCommitCmd(t *testing.T) {
	previousRuntimeRoot := constants.DefaultRuntimeRootDir
	runtimeRoot := previousRuntimeRoot
	if runtimeRoot == "" {
		runtimeRoot = constants.GetRuntimeRootDir(constants.AppName)
	}
	constants.DefaultRuntimeRootDir = runtimeRoot
	t.Cleanup(func() {
		constants.DefaultRuntimeRootDir = previousRuntimeRoot
	})

	previousOutput := outputSyncKubectl
	outputSyncKubectl = func(args ...string) ([]byte, error) {
		return []byte(`{"apiVersion":"v1","kind":"ConfigMap","metadata":{"name":"grafana-settings","namespace":"default"},"data":{"adminUser":"ops","imageTag":"10.4.0"}}`), nil
	}
	t.Cleanup(func() {
		outputSyncKubectl = previousOutput
	})

	clusterName := uniqueSyncClusterName(t, "commit")
	t.Cleanup(func() {
		_ = os.RemoveAll(filepath.Join(runtimeRoot, clusterName))
	})
	bundleDir := t.TempDir()
	localRepoRoot := t.TempDir()

	bundle := &hydrate.Bundle{
		APIVersion: "distribution.sealos.io/v1alpha1",
		Kind:       "HydratedBundle",
		Spec: hydrate.BundleSpec{
			BOMName:  "minimal-single-node",
			Revision: "rev-poc-001",
			Channel:  bom.ChannelAlpha,
			TrackedK8sObjects: []hydrate.TrackedK8sObject{
				{
					APIVersion: "v1",
					Kind:       "ConfigMap",
					Namespace:  "default",
					Name:       "grafana-settings",
					Path:       "components/grafana/files/manifests/grafana-settings.yaml",
					Component:  "grafana",
					Source:     hydrate.InventorySourcePackageManifest,
					Ownership:  hydrate.InventoryOwnershipGlobal,
				},
				{
					APIVersion: "v1",
					Kind:       "ConfigMap",
					Namespace:  "default",
					Name:       "grafana-settings",
					Path:       "components/grafana/local-patches/grafana-settings.patch.yaml",
					Component:  "grafana",
					Source:     hydrate.InventorySourceLocalPatch,
					Ownership:  hydrate.InventoryOwnershipLocal,
				},
			},
			Components: []hydrate.RenderedComponent{
				{
					Name:         "grafana",
					LocalPatches: []string{"components/grafana/local-patches/grafana-settings.patch.yaml"},
					Steps: []hydrate.RenderedStep{
						{
							Name:        "grafana-manifest",
							Kind:        hydrate.StepContent,
							BundlePath:  "components/grafana/files/manifests/grafana-settings.yaml",
							ContentType: "manifest",
						},
					},
				},
			},
		},
	}

	manifestPath := filepath.Join(bundleDir, "components", "grafana", "files", "manifests", "grafana-settings.yaml")
	patchBundlePath := filepath.Join(bundleDir, "components", "grafana", "local-patches", "grafana-settings.patch.yaml")
	patchRepoPath := filepath.Join(localRepoRoot, localrepo.PatchesDirName, "grafana", "grafana-settings.patch.yaml")
	for _, path := range []string{manifestPath, patchBundlePath, patchRepoPath} {
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatalf("MkdirAll(%q) error = %v", filepath.Dir(path), err)
		}
	}
	manifest := "apiVersion: v1\nkind: ConfigMap\nmetadata:\n  name: grafana-settings\n  namespace: default\ndata:\n  adminUser: admin\n  imageTag: 10.4.0\n"
	patch := "apiVersion: v1\nkind: ConfigMap\nmetadata:\n  name: grafana-settings\n  namespace: default\ndata:\n  adminUser: admin\n"
	if err := os.WriteFile(manifestPath, []byte(manifest), 0o644); err != nil {
		t.Fatalf("WriteFile(%q) error = %v", manifestPath, err)
	}
	if err := os.WriteFile(patchBundlePath, []byte(patch), 0o644); err != nil {
		t.Fatalf("WriteFile(%q) error = %v", patchBundlePath, err)
	}
	if err := os.WriteFile(patchRepoPath, []byte(patch), 0o644); err != nil {
		t.Fatalf("WriteFile(%q) error = %v", patchRepoPath, err)
	}
	if err := yamlutil.MarshalFile(filepath.Join(bundleDir, hydrate.BundleFileName), bundle); err != nil {
		t.Fatalf("MarshalFile(bundle) error = %v", err)
	}

	beforeDigest, err := hydrate.DigestBundle(bundleDir)
	if err != nil {
		t.Fatalf("DigestBundle(before) error = %v", err)
	}
	if _, err := state.PersistSuccessfulApply(clusterName, state.BOMReference{
		Name:     bundle.Spec.BOMName,
		Revision: bundle.Spec.Revision,
		Channel:  bundle.Spec.Channel,
	}, beforeDigest.String(), "local-rev-before", "patch-rev-1"); err != nil {
		t.Fatalf("PersistSuccessfulApply() error = %v", err)
	}

	buf := bytes.NewBuffer(nil)
	cmd := newSyncCmd()
	cmd.SetOut(buf)
	cmd.SetErr(buf)
	cmd.SetArgs([]string{
		"commit",
		"--cluster", clusterName,
		"--bundle-dir", bundleDir,
		"--local-repo", localRepoRoot,
		"--kubeconfig", "/tmp/test-kubeconfig",
	})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute() error = %v", err)
	}

	var out struct {
		CurrentState       string `yaml:"currentState"`
		DesiredStateDigest string `yaml:"desiredStateDigest"`
		LocalRepoRevision  string `yaml:"localRepoRevision"`
		CommittedObjects   []struct {
			Name string `yaml:"name"`
		} `yaml:"committedObjects"`
		RecordedRevision struct {
			State              string `yaml:"state"`
			DesiredStateDigest string `yaml:"desiredStateDigest"`
			LocalRepoRevision  string `yaml:"localRepoRevision"`
		} `yaml:"recordedRevision"`
	}
	if err := yaml.Unmarshal(buf.Bytes(), &out); err != nil {
		t.Fatalf("Unmarshal() error = %v\noutput=%s", err, buf.String())
	}
	if got, want := out.CurrentState, "Clean"; got != want {
		t.Fatalf("currentState = %q, want %q", got, want)
	}
	if got, want := len(out.CommittedObjects), 1; got != want {
		t.Fatalf("len(committedObjects) = %d, want %d", got, want)
	}
	if got, want := out.CommittedObjects[0].Name, "grafana-settings"; got != want {
		t.Fatalf("committedObjects[0].name = %q, want %q", got, want)
	}
	if out.DesiredStateDigest == beforeDigest.String() {
		t.Fatalf("desiredStateDigest = %q, want a new digest", out.DesiredStateDigest)
	}
	if got, want := out.RecordedRevision.State, "Clean"; got != want {
		t.Fatalf("recordedRevision.state = %q, want %q", got, want)
	}
	if got, want := out.RecordedRevision.DesiredStateDigest, out.DesiredStateDigest; got != want {
		t.Fatalf("recordedRevision.desiredStateDigest = %q, want %q", got, want)
	}
	if got, want := out.RecordedRevision.LocalRepoRevision, out.LocalRepoRevision; got != want {
		t.Fatalf("recordedRevision.localRepoRevision = %q, want %q", got, want)
	}

	for _, path := range []string{patchRepoPath, patchBundlePath, manifestPath} {
		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("ReadFile(%q) error = %v", path, err)
		}
		if !strings.Contains(string(data), "adminUser: ops") {
			t.Fatalf("%q missing committed adminUser override:\n%s", path, string(data))
		}
	}
}

func TestSyncCommitCmdWithLocalResource(t *testing.T) {
	previousRuntimeRoot := constants.DefaultRuntimeRootDir
	runtimeRoot := previousRuntimeRoot
	if runtimeRoot == "" {
		runtimeRoot = constants.GetRuntimeRootDir(constants.AppName)
	}
	constants.DefaultRuntimeRootDir = runtimeRoot
	t.Cleanup(func() {
		constants.DefaultRuntimeRootDir = previousRuntimeRoot
	})

	previousOutput := outputSyncKubectl
	outputSyncKubectl = func(args ...string) ([]byte, error) {
		return []byte(`{"apiVersion":"v1","kind":"Secret","metadata":{"name":"grafana-admin-credentials","namespace":"default","resourceVersion":"2","uid":"abcd"},"type":"Opaque","data":{"username":"b3Bz","password":"bmV3LXBhc3M="}}`), nil
	}
	t.Cleanup(func() {
		outputSyncKubectl = previousOutput
	})

	clusterName := uniqueSyncClusterName(t, "commit-resource")
	t.Cleanup(func() {
		_ = os.RemoveAll(filepath.Join(runtimeRoot, clusterName))
	})
	bundleDir := t.TempDir()
	localRepoRoot := t.TempDir()

	bundle := &hydrate.Bundle{
		APIVersion: "distribution.sealos.io/v1alpha1",
		Kind:       "HydratedBundle",
		Spec: hydrate.BundleSpec{
			BOMName:  "minimal-single-node",
			Revision: "rev-poc-001",
			Channel:  bom.ChannelAlpha,
			TrackedK8sObjects: []hydrate.TrackedK8sObject{
				{
					APIVersion: "v1",
					Kind:       "Secret",
					Namespace:  "default",
					Name:       "grafana-admin-credentials",
					Path:       "local-resources/secrets/grafana-admin-credentials.yaml",
					Source:     hydrate.InventorySourceLocalResource,
					Ownership:  hydrate.InventoryOwnershipLocal,
				},
			},
			Components: []hydrate.RenderedComponent{
				{Name: "local-resources"},
			},
		},
	}

	bundleResourcePath := filepath.Join(bundleDir, "local-resources", "secrets", "grafana-admin-credentials.yaml")
	repoResourcePath := filepath.Join(localRepoRoot, localrepo.ResourcesDirName, "secrets", "grafana-admin-credentials.yaml")
	for _, path := range []string{bundleResourcePath, repoResourcePath} {
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatalf("MkdirAll(%q) error = %v", filepath.Dir(path), err)
		}
	}
	resource := "apiVersion: v1\nkind: Secret\nmetadata:\n  name: grafana-admin-credentials\n  namespace: default\nstringData:\n  username: admin\n  password: passw0rd\n"
	if err := os.WriteFile(bundleResourcePath, []byte(resource), 0o644); err != nil {
		t.Fatalf("WriteFile(%q) error = %v", bundleResourcePath, err)
	}
	if err := os.WriteFile(repoResourcePath, []byte(resource), 0o644); err != nil {
		t.Fatalf("WriteFile(%q) error = %v", repoResourcePath, err)
	}
	if err := yamlutil.MarshalFile(filepath.Join(bundleDir, hydrate.BundleFileName), bundle); err != nil {
		t.Fatalf("MarshalFile(bundle) error = %v", err)
	}

	beforeDigest, err := hydrate.DigestBundle(bundleDir)
	if err != nil {
		t.Fatalf("DigestBundle(before) error = %v", err)
	}
	if _, err := state.PersistSuccessfulApply(clusterName, state.BOMReference{
		Name:     bundle.Spec.BOMName,
		Revision: bundle.Spec.Revision,
		Channel:  bundle.Spec.Channel,
	}, beforeDigest.String(), "local-rev-before", "patch-rev-1"); err != nil {
		t.Fatalf("PersistSuccessfulApply() error = %v", err)
	}

	buf := bytes.NewBuffer(nil)
	cmd := newSyncCmd()
	cmd.SetOut(buf)
	cmd.SetErr(buf)
	cmd.SetArgs([]string{
		"commit",
		"--cluster", clusterName,
		"--bundle-dir", bundleDir,
		"--local-repo", localRepoRoot,
		"--kubeconfig", "/tmp/test-kubeconfig",
	})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute() error = %v", err)
	}

	var out struct {
		CurrentState      string `yaml:"currentState"`
		LocalRepoRevision string `yaml:"localRepoRevision"`
		CommittedObjects  []struct {
			Name     string `yaml:"name"`
			RepoPath string `yaml:"repoPath"`
		} `yaml:"committedObjects"`
	}
	if err := yaml.Unmarshal(buf.Bytes(), &out); err != nil {
		t.Fatalf("Unmarshal() error = %v\noutput=%s", err, buf.String())
	}
	if got, want := out.CurrentState, "Clean"; got != want {
		t.Fatalf("currentState = %q, want %q", got, want)
	}
	if got, want := len(out.CommittedObjects), 1; got != want {
		t.Fatalf("len(committedObjects) = %d, want %d", got, want)
	}
	if got, want := out.CommittedObjects[0].RepoPath, repoResourcePath; got != want {
		t.Fatalf("committedObjects[0].repoPath = %q, want %q", got, want)
	}

	for _, path := range []string{repoResourcePath, bundleResourcePath} {
		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("ReadFile(%q) error = %v", path, err)
		}
		text := string(data)
		if !strings.Contains(text, "bmV3LXBhc3M=") {
			t.Fatalf("%q missing committed secret data:\n%s", path, text)
		}
		if strings.Contains(text, "resourceVersion:") {
			t.Fatalf("%q still contains server-managed metadata:\n%s", path, text)
		}
	}
}

func TestSyncCommitCmdWithLocalInputHostPath(t *testing.T) {
	previousRuntimeRoot := constants.DefaultRuntimeRootDir
	runtimeRoot := previousRuntimeRoot
	if runtimeRoot == "" {
		runtimeRoot = constants.GetRuntimeRootDir(constants.AppName)
	}
	constants.DefaultRuntimeRootDir = runtimeRoot
	t.Cleanup(func() {
		constants.DefaultRuntimeRootDir = previousRuntimeRoot
	})

	previousOutput := outputSyncKubectl
	outputSyncKubectl = func(args ...string) ([]byte, error) {
		t.Fatalf("unexpected kubectl invocation: %s", strings.Join(args, " "))
		return nil, nil
	}
	t.Cleanup(func() {
		outputSyncKubectl = previousOutput
	})

	clusterName := uniqueSyncClusterName(t, "commit-hostpath")
	t.Cleanup(func() {
		_ = os.RemoveAll(filepath.Join(runtimeRoot, clusterName))
	})
	bundleDir := t.TempDir()
	localRepoRoot := t.TempDir()
	hostRoot := t.TempDir()

	repoInputPath := filepath.Join(localRepoRoot, localrepo.InputsDirName, "kubernetes", "kubeadm-cluster-config.yaml")
	bundleInputPath := filepath.Join(bundleDir, "components", "kubernetes", "files", "files", "etc", "kubernetes", "kubeadm.yaml")
	liveHostPath := filepath.Join(hostRoot, "etc", "kubernetes", "kubeadm.yaml")
	for _, path := range []string{repoInputPath, bundleInputPath, liveHostPath} {
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatalf("MkdirAll(%q) error = %v", filepath.Dir(path), err)
		}
	}

	desired := "apiVersion: kubeadm.k8s.io/v1beta4\nkind: ClusterConfiguration\nclusterName: baseline\n"
	live := "apiVersion: kubeadm.k8s.io/v1beta4\nkind: ClusterConfiguration\nclusterName: committed-local\n"
	if err := os.WriteFile(repoInputPath, []byte(desired), 0o644); err != nil {
		t.Fatalf("WriteFile(%q) error = %v", repoInputPath, err)
	}
	if err := os.WriteFile(bundleInputPath, []byte(desired), 0o644); err != nil {
		t.Fatalf("WriteFile(%q) error = %v", bundleInputPath, err)
	}
	if err := os.WriteFile(liveHostPath, []byte(live), 0o644); err != nil {
		t.Fatalf("WriteFile(%q) error = %v", liveHostPath, err)
	}

	bundle := &hydrate.Bundle{
		APIVersion: "distribution.sealos.io/v1alpha1",
		Kind:       "HydratedBundle",
		Spec: hydrate.BundleSpec{
			BOMName:  "minimal-single-node",
			Revision: "rev-poc-001",
			Channel:  bom.ChannelAlpha,
			TrackedHostPaths: []hydrate.TrackedHostPath{
				{
					HostPath:   "/etc/kubernetes/kubeadm.yaml",
					BundlePath: "components/kubernetes/files/files/etc/kubernetes/kubeadm.yaml",
					Component:  "kubernetes",
					Source:     hydrate.InventorySourceLocalInput,
					Ownership:  hydrate.InventoryOwnershipLocal,
					Type:       hydrate.HostPathRegularFile,
					InputName:  "kubeadm-cluster-config",
				},
			},
			Components: []hydrate.RenderedComponent{
				{
					Name: "kubernetes",
					InputBindings: map[string]string{
						"kubeadm-cluster-config": repoInputPath,
					},
				},
			},
		},
	}
	if err := yamlutil.MarshalFile(filepath.Join(bundleDir, hydrate.BundleFileName), bundle); err != nil {
		t.Fatalf("MarshalFile(bundle) error = %v", err)
	}

	beforeDigest, err := hydrate.DigestBundle(bundleDir)
	if err != nil {
		t.Fatalf("DigestBundle(before) error = %v", err)
	}
	if _, err := state.PersistSuccessfulApply(clusterName, state.BOMReference{
		Name:     bundle.Spec.BOMName,
		Revision: bundle.Spec.Revision,
		Channel:  bundle.Spec.Channel,
	}, beforeDigest.String(), "local-rev-before", "patch-rev-1"); err != nil {
		t.Fatalf("PersistSuccessfulApply() error = %v", err)
	}

	buf := bytes.NewBuffer(nil)
	cmd := newSyncCmd()
	cmd.SetOut(buf)
	cmd.SetErr(buf)
	cmd.SetArgs([]string{
		"commit",
		"--cluster", clusterName,
		"--bundle-dir", bundleDir,
		"--local-repo", localRepoRoot,
		"--kubeconfig", "/tmp/test-kubeconfig",
		"--host-root", hostRoot,
	})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute() error = %v", err)
	}

	var out struct {
		CurrentState       string `yaml:"currentState"`
		LocalRepoRevision  string `yaml:"localRepoRevision"`
		CommittedHostPaths []struct {
			Path     string `yaml:"path"`
			RepoPath string `yaml:"repoPath"`
		} `yaml:"committedHostPaths"`
	}
	if err := yaml.Unmarshal(buf.Bytes(), &out); err != nil {
		t.Fatalf("Unmarshal() error = %v\noutput=%s", err, buf.String())
	}
	if got, want := out.CurrentState, "Clean"; got != want {
		t.Fatalf("currentState = %q, want %q", got, want)
	}
	if got, want := len(out.CommittedHostPaths), 1; got != want {
		t.Fatalf("len(committedHostPaths) = %d, want %d", got, want)
	}
	if got, want := out.CommittedHostPaths[0].Path, "/etc/kubernetes/kubeadm.yaml"; got != want {
		t.Fatalf("committedHostPaths[0].path = %q, want %q", got, want)
	}
	if got, want := out.CommittedHostPaths[0].RepoPath, repoInputPath; got != want {
		t.Fatalf("committedHostPaths[0].repoPath = %q, want %q", got, want)
	}

	for _, path := range []string{repoInputPath, bundleInputPath} {
		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("ReadFile(%q) error = %v", path, err)
		}
		if got, want := string(data), live; got != want {
			t.Fatalf("%q content = %q, want %q", path, got, want)
		}
	}
}

func TestSyncCommitCmdRejectsMultiNodeLocalInputHostPathWithoutHost(t *testing.T) {
	previousRuntimeRoot := constants.DefaultRuntimeRootDir
	runtimeRoot := previousRuntimeRoot
	if runtimeRoot == "" {
		runtimeRoot = constants.GetRuntimeRootDir(constants.AppName)
	}
	constants.DefaultRuntimeRootDir = runtimeRoot
	t.Cleanup(func() {
		constants.DefaultRuntimeRootDir = previousRuntimeRoot
	})

	previousOutput := outputSyncKubectl
	outputSyncKubectl = func(args ...string) ([]byte, error) {
		t.Fatalf("unexpected kubectl invocation: %s", strings.Join(args, " "))
		return nil, nil
	}
	t.Cleanup(func() {
		outputSyncKubectl = previousOutput
	})

	localHostRoot := t.TempDir()
	remoteHostRoot := t.TempDir()
	previousTopologyLoader := loadSyncExecutionTopology
	previousRemoteExecutor := newSyncRemoteExecutor
	fakeRemote := &fakeSyncRemoteExecutor{
		roots: map[string]string{
			"10.0.0.11:22": remoteHostRoot,
		},
	}
	loadSyncExecutionTopology = func(string) (*syncExecutionTopology, error) {
		return &syncExecutionTopology{
			allNodes:    []string{syncLocalExecutionHost, "10.0.0.11:22"},
			firstMaster: syncLocalExecutionHost,
		}, nil
	}
	newSyncRemoteExecutor = func(*syncExecutionTopology) (syncRemoteExecutor, error) {
		return fakeRemote, nil
	}
	t.Cleanup(func() {
		loadSyncExecutionTopology = previousTopologyLoader
		newSyncRemoteExecutor = previousRemoteExecutor
	})

	clusterName := uniqueSyncClusterName(t, "commit-hostpath-multinode")
	t.Cleanup(func() {
		_ = os.RemoveAll(filepath.Join(runtimeRoot, clusterName))
	})
	bundleDir := t.TempDir()
	localRepoRoot := t.TempDir()

	repoInputPath := filepath.Join(localRepoRoot, localrepo.InputsDirName, "kubernetes", "kubeadm-cluster-config.yaml")
	bundleInputPath := filepath.Join(bundleDir, "components", "kubernetes", "files", "files", "etc", "kubernetes", "kubeadm.yaml")
	localLiveHostPath := filepath.Join(localHostRoot, "etc", "kubernetes", "kubeadm.yaml")
	for _, path := range []string{repoInputPath, bundleInputPath, localLiveHostPath} {
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatalf("MkdirAll(%q) error = %v", filepath.Dir(path), err)
		}
	}

	desired := "apiVersion: kubeadm.k8s.io/v1beta4\nkind: ClusterConfiguration\nclusterName: baseline\n"
	localLive := "apiVersion: kubeadm.k8s.io/v1beta4\nkind: ClusterConfiguration\nclusterName: local-drift\n"
	remoteLive := "apiVersion: kubeadm.k8s.io/v1beta4\nkind: ClusterConfiguration\nclusterName: remote-drift\n"
	if err := os.WriteFile(repoInputPath, []byte(desired), 0o644); err != nil {
		t.Fatalf("WriteFile(%q) error = %v", repoInputPath, err)
	}
	if err := os.WriteFile(bundleInputPath, []byte(desired), 0o644); err != nil {
		t.Fatalf("WriteFile(%q) error = %v", bundleInputPath, err)
	}
	if err := os.WriteFile(localLiveHostPath, []byte(localLive), 0o644); err != nil {
		t.Fatalf("WriteFile(%q) error = %v", localLiveHostPath, err)
	}
	if err := writeSyncRemoteHostFixture(remoteHostRoot, "/etc/kubernetes/kubeadm.yaml", remoteLive, 0o644); err != nil {
		t.Fatalf("writeSyncRemoteHostFixture(remote) error = %v", err)
	}

	bundle := &hydrate.Bundle{
		APIVersion: "distribution.sealos.io/v1alpha1",
		Kind:       "HydratedBundle",
		Spec: hydrate.BundleSpec{
			BOMName:  "minimal-single-node",
			Revision: "rev-poc-001",
			Channel:  bom.ChannelAlpha,
			TrackedHostPaths: []hydrate.TrackedHostPath{
				{
					HostPath:   "/etc/kubernetes/kubeadm.yaml",
					BundlePath: "components/kubernetes/files/files/etc/kubernetes/kubeadm.yaml",
					Component:  "kubernetes",
					Source:     hydrate.InventorySourceLocalInput,
					Ownership:  hydrate.InventoryOwnershipLocal,
					Type:       hydrate.HostPathRegularFile,
					InputName:  "kubeadm-cluster-config",
				},
			},
			Components: []hydrate.RenderedComponent{
				{
					Name: "kubernetes",
					InputBindings: map[string]string{
						"kubeadm-cluster-config": repoInputPath,
					},
				},
			},
		},
	}
	if err := yamlutil.MarshalFile(filepath.Join(bundleDir, hydrate.BundleFileName), bundle); err != nil {
		t.Fatalf("MarshalFile(bundle) error = %v", err)
	}

	beforeDigest, err := hydrate.DigestBundle(bundleDir)
	if err != nil {
		t.Fatalf("DigestBundle(before) error = %v", err)
	}
	if _, err := state.PersistSuccessfulApply(clusterName, state.BOMReference{
		Name:     bundle.Spec.BOMName,
		Revision: bundle.Spec.Revision,
		Channel:  bundle.Spec.Channel,
	}, beforeDigest.String(), "local-rev-before", "patch-rev-1"); err != nil {
		t.Fatalf("PersistSuccessfulApply() error = %v", err)
	}

	cmd := newSyncCmd()
	cmd.SetArgs([]string{
		"commit",
		"--cluster", clusterName,
		"--bundle-dir", bundleDir,
		"--local-repo", localRepoRoot,
		"--kubeconfig", "/tmp/test-kubeconfig",
		"--host-root", localHostRoot,
	})
	err = cmd.Execute()
	if err == nil {
		t.Fatal("Execute() error = nil, want host selection error")
	}
	if got := err.Error(); !strings.Contains(got, "specify --host from [10.0.0.11:22, localhost]") {
		t.Fatalf("Execute() error = %q, want host guidance", got)
	}
}

func TestSyncCompareBundleUsesExecutionTopologySnapshot(t *testing.T) {
	previousRuntimeRoot := constants.DefaultRuntimeRootDir
	runtimeRoot := previousRuntimeRoot
	if runtimeRoot == "" {
		runtimeRoot = constants.GetRuntimeRootDir(constants.AppName)
	}
	constants.DefaultRuntimeRootDir = runtimeRoot
	t.Cleanup(func() {
		constants.DefaultRuntimeRootDir = previousRuntimeRoot
	})

	previousOutput := outputSyncKubectl
	outputSyncKubectl = func(args ...string) ([]byte, error) {
		t.Fatalf("unexpected kubectl invocation: %s", strings.Join(args, " "))
		return nil, nil
	}
	t.Cleanup(func() {
		outputSyncKubectl = previousOutput
	})

	previousTopologyLoader := loadSyncExecutionTopology
	previousRemoteExecutor := newSyncRemoteExecutor
	topologyLoaded := false
	loadSyncExecutionTopology = func(string) (*syncExecutionTopology, error) {
		topologyLoaded = true
		return &syncExecutionTopology{
			cluster: &v1beta1.Cluster{},
		}, nil
	}
	remoteHostRoot := t.TempDir()
	fakeRemote := &fakeSyncRemoteExecutor{
		roots: map[string]string{
			"10.0.0.11:22": remoteHostRoot,
		},
	}
	newSyncRemoteExecutor = func(topology *syncExecutionTopology) (syncRemoteExecutor, error) {
		if got, want := strings.Join(topology.nodeExecutionHosts(), ","), "10.0.0.11:22"; got != want {
			t.Fatalf("topology nodes = %q, want %q", got, want)
		}
		if got, want := strings.Join(topology.rolesForHost("10.0.0.11:22"), ","), v1beta1.MASTER; got != want {
			t.Fatalf("topology roles = %q, want %q", got, want)
		}
		return fakeRemote, nil
	}
	t.Cleanup(func() {
		loadSyncExecutionTopology = previousTopologyLoader
		newSyncRemoteExecutor = previousRemoteExecutor
	})

	bundleDir := t.TempDir()
	bundlePath := "components/kubernetes/files/files/etc/kubernetes/kubeadm.yaml"
	writeSyncTestFile(t, filepath.Join(bundleDir, bundlePath), "clusterName: baseline\n", 0o644)
	if err := writeSyncRemoteHostFixture(remoteHostRoot, "/etc/kubernetes/kubeadm.yaml", "clusterName: baseline\n", 0o644); err != nil {
		t.Fatalf("writeSyncRemoteHostFixture(remote) error = %v", err)
	}

	bundle := &hydrate.Bundle{
		APIVersion: distribution.APIVersion,
		Kind:       distribution.KindHydratedBundle,
		Spec: hydrate.BundleSpec{
			BOMName:  "snapshot-compare",
			Revision: "rev-1",
			Channel:  bom.ChannelAlpha,
			ExecutionTopology: hydrate.ExecutionTopology{
				Source:      "testSnapshot",
				AllNodes:    []string{"10.0.0.11:22"},
				FirstMaster: "10.0.0.11:22",
				HostRoles: []hydrate.ExecutionHostRoleList{
					{Host: "10.0.0.11:22", Roles: []string{v1beta1.MASTER}},
				},
			},
			TrackedHostPaths: []hydrate.TrackedHostPath{
				{
					HostPath:   "/etc/kubernetes/kubeadm.yaml",
					BundlePath: bundlePath,
					Component:  "kubernetes",
					Source:     hydrate.InventorySourceLocalInput,
					Ownership:  hydrate.InventoryOwnershipLocal,
					Type:       hydrate.HostPathRegularFile,
				},
			},
			Components: []hydrate.RenderedComponent{{Name: "kubernetes"}},
		},
	}

	result, err := syncCompareBundle("snapshot-compare", bundle, bundleDir, "/tmp/test-kubeconfig", t.TempDir())
	if err != nil {
		t.Fatalf("syncCompareBundle() error = %v", err)
	}
	if !topologyLoaded {
		t.Fatal("loadSyncExecutionTopology was not called for remote executor credentials")
	}
	if got, want := len(result.HostPaths), 1; got != want {
		t.Fatalf("len(result.HostPaths) = %d, want %d", got, want)
	}
	if got, want := result.HostPaths[0].Host, "10.0.0.11:22"; got != want {
		t.Fatalf("result.HostPaths[0].Host = %q, want %q", got, want)
	}
}

func TestSyncCommitCmdRejectsDivergentSelectedHostWithoutScopedInput(t *testing.T) {
	previousRuntimeRoot := constants.DefaultRuntimeRootDir
	runtimeRoot := previousRuntimeRoot
	if runtimeRoot == "" {
		runtimeRoot = constants.GetRuntimeRootDir(constants.AppName)
	}
	constants.DefaultRuntimeRootDir = runtimeRoot
	t.Cleanup(func() {
		constants.DefaultRuntimeRootDir = previousRuntimeRoot
	})

	previousOutput := outputSyncKubectl
	outputSyncKubectl = func(args ...string) ([]byte, error) {
		t.Fatalf("unexpected kubectl invocation: %s", strings.Join(args, " "))
		return nil, nil
	}
	t.Cleanup(func() {
		outputSyncKubectl = previousOutput
	})

	localHostRoot := t.TempDir()
	remoteHostRoot := t.TempDir()
	previousTopologyLoader := loadSyncExecutionTopology
	previousRemoteExecutor := newSyncRemoteExecutor
	fakeRemote := &fakeSyncRemoteExecutor{
		roots: map[string]string{
			"10.0.0.11:22": remoteHostRoot,
		},
	}
	loadSyncExecutionTopology = func(string) (*syncExecutionTopology, error) {
		return &syncExecutionTopology{
			allNodes:    []string{syncLocalExecutionHost, "10.0.0.11:22"},
			firstMaster: syncLocalExecutionHost,
		}, nil
	}
	newSyncRemoteExecutor = func(*syncExecutionTopology) (syncRemoteExecutor, error) {
		return fakeRemote, nil
	}
	t.Cleanup(func() {
		loadSyncExecutionTopology = previousTopologyLoader
		newSyncRemoteExecutor = previousRemoteExecutor
	})

	clusterName := uniqueSyncClusterName(t, "commit-hostpath-selected-no-scope")
	t.Cleanup(func() {
		_ = os.RemoveAll(filepath.Join(runtimeRoot, clusterName))
	})
	bundleDir := t.TempDir()
	localRepoRoot := t.TempDir()

	repoInputPath := filepath.Join(localRepoRoot, localrepo.InputsDirName, "kubernetes", "kubeadm-cluster-config.yaml")
	bundleInputPath := filepath.Join(bundleDir, "components", "kubernetes", "files", "files", "etc", "kubernetes", "kubeadm.yaml")
	localLiveHostPath := filepath.Join(localHostRoot, "etc", "kubernetes", "kubeadm.yaml")
	for _, path := range []string{repoInputPath, bundleInputPath, localLiveHostPath} {
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatalf("MkdirAll(%q) error = %v", filepath.Dir(path), err)
		}
	}

	desired := "apiVersion: kubeadm.k8s.io/v1beta4\nkind: ClusterConfiguration\nclusterName: baseline\n"
	localLive := "apiVersion: kubeadm.k8s.io/v1beta4\nkind: ClusterConfiguration\nclusterName: local-drift\n"
	remoteLive := "apiVersion: kubeadm.k8s.io/v1beta4\nkind: ClusterConfiguration\nclusterName: remote-drift\n"
	if err := os.WriteFile(repoInputPath, []byte(desired), 0o644); err != nil {
		t.Fatalf("WriteFile(%q) error = %v", repoInputPath, err)
	}
	if err := os.WriteFile(bundleInputPath, []byte(desired), 0o644); err != nil {
		t.Fatalf("WriteFile(%q) error = %v", bundleInputPath, err)
	}
	if err := os.WriteFile(localLiveHostPath, []byte(localLive), 0o644); err != nil {
		t.Fatalf("WriteFile(%q) error = %v", localLiveHostPath, err)
	}
	if err := writeSyncRemoteHostFixture(remoteHostRoot, "/etc/kubernetes/kubeadm.yaml", remoteLive, 0o644); err != nil {
		t.Fatalf("writeSyncRemoteHostFixture(remote) error = %v", err)
	}

	bundle := &hydrate.Bundle{
		APIVersion: "distribution.sealos.io/v1alpha1",
		Kind:       "HydratedBundle",
		Spec: hydrate.BundleSpec{
			BOMName:  "minimal-single-node",
			Revision: "rev-poc-001",
			Channel:  bom.ChannelAlpha,
			TrackedHostPaths: []hydrate.TrackedHostPath{
				{
					HostPath:   "/etc/kubernetes/kubeadm.yaml",
					BundlePath: "components/kubernetes/files/files/etc/kubernetes/kubeadm.yaml",
					Component:  "kubernetes",
					Source:     hydrate.InventorySourceLocalInput,
					Ownership:  hydrate.InventoryOwnershipLocal,
					Type:       hydrate.HostPathRegularFile,
					InputName:  "kubeadm-cluster-config",
				},
			},
			Components: []hydrate.RenderedComponent{
				{
					Name: "kubernetes",
					InputBindings: map[string]string{
						"kubeadm-cluster-config": repoInputPath,
					},
				},
			},
		},
	}
	if err := yamlutil.MarshalFile(filepath.Join(bundleDir, hydrate.BundleFileName), bundle); err != nil {
		t.Fatalf("MarshalFile(bundle) error = %v", err)
	}

	beforeDigest, err := hydrate.DigestBundle(bundleDir)
	if err != nil {
		t.Fatalf("DigestBundle(before) error = %v", err)
	}
	if _, err := state.PersistSuccessfulApply(clusterName, state.BOMReference{
		Name:     bundle.Spec.BOMName,
		Revision: bundle.Spec.Revision,
		Channel:  bundle.Spec.Channel,
	}, beforeDigest.String(), "local-rev-before", "patch-rev-1"); err != nil {
		t.Fatalf("PersistSuccessfulApply() error = %v", err)
	}

	cmd := newSyncCmd()
	cmd.SetArgs([]string{
		"commit",
		"--cluster", clusterName,
		"--bundle-dir", bundleDir,
		"--local-repo", localRepoRoot,
		"--kubeconfig", "/tmp/test-kubeconfig",
		"--host-root", localHostRoot,
		"--host", "10.0.0.11:22",
	})
	err = cmd.Execute()
	if err == nil {
		t.Fatal("Execute() error = nil, want host-scoped input error")
	}
	if got := err.Error(); !strings.Contains(got, "has no host-scoped input binding") {
		t.Fatalf("Execute() error = %q, want host-scoped input guidance", got)
	}

	for _, path := range []string{repoInputPath, bundleInputPath} {
		data, readErr := os.ReadFile(path)
		if readErr != nil {
			t.Fatalf("ReadFile(%q) error = %v", path, readErr)
		}
		if got, want := string(data), desired; got != want {
			t.Fatalf("%q content = %q, want unchanged %q", path, got, want)
		}
	}
}

func TestSyncCommitCmdWithRemoteLocalInputHostPath(t *testing.T) {
	previousRuntimeRoot := constants.DefaultRuntimeRootDir
	runtimeRoot := previousRuntimeRoot
	if runtimeRoot == "" {
		runtimeRoot = constants.GetRuntimeRootDir(constants.AppName)
	}
	constants.DefaultRuntimeRootDir = runtimeRoot
	t.Cleanup(func() {
		constants.DefaultRuntimeRootDir = previousRuntimeRoot
	})

	previousOutput := outputSyncKubectl
	outputSyncKubectl = func(args ...string) ([]byte, error) {
		t.Fatalf("unexpected kubectl invocation: %s", strings.Join(args, " "))
		return nil, nil
	}
	t.Cleanup(func() {
		outputSyncKubectl = previousOutput
	})

	localHostRoot := t.TempDir()
	remoteHostRoot := t.TempDir()
	previousTopologyLoader := loadSyncExecutionTopology
	previousRemoteExecutor := newSyncRemoteExecutor
	fakeRemote := &fakeSyncRemoteExecutor{
		roots: map[string]string{
			"10.0.0.11:22": remoteHostRoot,
		},
	}
	loadSyncExecutionTopology = func(string) (*syncExecutionTopology, error) {
		return &syncExecutionTopology{
			allNodes:    []string{syncLocalExecutionHost, "10.0.0.11:22"},
			firstMaster: syncLocalExecutionHost,
		}, nil
	}
	newSyncRemoteExecutor = func(*syncExecutionTopology) (syncRemoteExecutor, error) {
		return fakeRemote, nil
	}
	t.Cleanup(func() {
		loadSyncExecutionTopology = previousTopologyLoader
		newSyncRemoteExecutor = previousRemoteExecutor
	})

	clusterName := uniqueSyncClusterName(t, "commit-remote-hostpath")
	t.Cleanup(func() {
		_ = os.RemoveAll(filepath.Join(runtimeRoot, clusterName))
	})
	bundleDir := t.TempDir()
	localRepoRoot := t.TempDir()

	repoInputPath := filepath.Join(localRepoRoot, localrepo.InputsDirName, "kubernetes", "kubeadm-cluster-config.yaml")
	hostRepoInputPath := filepath.Join(localRepoRoot, localrepo.InputsDirName, "kubernetes", "hosts", "10.0.0.11", "kubeadm-cluster-config.yaml")
	bundleInputPath := filepath.Join(bundleDir, "components", "kubernetes", "files", "files", "etc", "kubernetes", "kubeadm.yaml")
	hostBundleInputPath := filepath.Join(bundleDir, "components", "kubernetes", "host-inputs", "10.0.0.11", "files", "etc", "kubernetes", "kubeadm.yaml")
	localLiveHostPath := filepath.Join(localHostRoot, "etc", "kubernetes", "kubeadm.yaml")
	for _, path := range []string{repoInputPath, hostRepoInputPath, bundleInputPath, hostBundleInputPath, localLiveHostPath} {
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatalf("MkdirAll(%q) error = %v", filepath.Dir(path), err)
		}
	}

	desired := "apiVersion: kubeadm.k8s.io/v1beta4\nkind: ClusterConfiguration\nclusterName: baseline\n"
	localLive := "apiVersion: kubeadm.k8s.io/v1beta4\nkind: ClusterConfiguration\nclusterName: local-drift\n"
	remoteLive := "apiVersion: kubeadm.k8s.io/v1beta4\nkind: ClusterConfiguration\nclusterName: remote-committed\n"
	if err := os.WriteFile(repoInputPath, []byte(desired), 0o644); err != nil {
		t.Fatalf("WriteFile(%q) error = %v", repoInputPath, err)
	}
	if err := os.WriteFile(hostRepoInputPath, []byte(desired), 0o644); err != nil {
		t.Fatalf("WriteFile(%q) error = %v", hostRepoInputPath, err)
	}
	if err := os.WriteFile(bundleInputPath, []byte(desired), 0o644); err != nil {
		t.Fatalf("WriteFile(%q) error = %v", bundleInputPath, err)
	}
	if err := os.WriteFile(hostBundleInputPath, []byte(desired), 0o644); err != nil {
		t.Fatalf("WriteFile(%q) error = %v", hostBundleInputPath, err)
	}
	if err := os.WriteFile(localLiveHostPath, []byte(localLive), 0o644); err != nil {
		t.Fatalf("WriteFile(%q) error = %v", localLiveHostPath, err)
	}
	if err := writeSyncRemoteHostFixture(remoteHostRoot, "/etc/kubernetes/kubeadm.yaml", remoteLive, 0o644); err != nil {
		t.Fatalf("writeSyncRemoteHostFixture(remote) error = %v", err)
	}

	bundle := &hydrate.Bundle{
		APIVersion: "distribution.sealos.io/v1alpha1",
		Kind:       "HydratedBundle",
		Spec: hydrate.BundleSpec{
			BOMName:  "minimal-single-node",
			Revision: "rev-poc-001",
			Channel:  bom.ChannelAlpha,
			TrackedHostPaths: []hydrate.TrackedHostPath{
				{
					HostPath:   "/etc/kubernetes/kubeadm.yaml",
					BundlePath: "components/kubernetes/files/files/etc/kubernetes/kubeadm.yaml",
					Component:  "kubernetes",
					Source:     hydrate.InventorySourceLocalInput,
					Ownership:  hydrate.InventoryOwnershipLocal,
					Type:       hydrate.HostPathRegularFile,
					InputName:  "kubeadm-cluster-config",
					HostInputBindings: map[string]string{
						"10.0.0.11": "components/kubernetes/host-inputs/10.0.0.11/files/etc/kubernetes/kubeadm.yaml",
					},
				},
			},
			Components: []hydrate.RenderedComponent{
				{
					Name: "kubernetes",
					InputBindings: map[string]string{
						"kubeadm-cluster-config": repoInputPath,
					},
				},
			},
		},
	}
	if err := yamlutil.MarshalFile(filepath.Join(bundleDir, hydrate.BundleFileName), bundle); err != nil {
		t.Fatalf("MarshalFile(bundle) error = %v", err)
	}

	beforeDigest, err := hydrate.DigestBundle(bundleDir)
	if err != nil {
		t.Fatalf("DigestBundle(before) error = %v", err)
	}
	if _, err := state.PersistSuccessfulApply(clusterName, state.BOMReference{
		Name:     bundle.Spec.BOMName,
		Revision: bundle.Spec.Revision,
		Channel:  bundle.Spec.Channel,
	}, beforeDigest.String(), "local-rev-before", "patch-rev-1"); err != nil {
		t.Fatalf("PersistSuccessfulApply() error = %v", err)
	}

	buf := bytes.NewBuffer(nil)
	cmd := newSyncCmd()
	cmd.SetOut(buf)
	cmd.SetErr(buf)
	cmd.SetArgs([]string{
		"commit",
		"--cluster", clusterName,
		"--bundle-dir", bundleDir,
		"--local-repo", localRepoRoot,
		"--kubeconfig", "/tmp/test-kubeconfig",
		"--host-root", localHostRoot,
		"--host", "10.0.0.11:22",
	})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute() error = %v", err)
	}

	var out struct {
		RequestedHost      string `yaml:"requestedHost"`
		CurrentState       string `yaml:"currentState"`
		CommittedHostPaths []struct {
			Host     string `yaml:"host"`
			Path     string `yaml:"path"`
			RepoPath string `yaml:"repoPath"`
		} `yaml:"committedHostPaths"`
	}
	if err := yaml.Unmarshal(buf.Bytes(), &out); err != nil {
		t.Fatalf("Unmarshal() error = %v\noutput=%s", err, buf.String())
	}
	if got, want := out.RequestedHost, "10.0.0.11:22"; got != want {
		t.Fatalf("requestedHost = %q, want %q", got, want)
	}
	if got, want := out.CurrentState, "Dirty"; got != want {
		t.Fatalf("currentState = %q, want %q", got, want)
	}
	if got, want := len(out.CommittedHostPaths), 1; got != want {
		t.Fatalf("len(committedHostPaths) = %d, want %d", got, want)
	}
	if got, want := out.CommittedHostPaths[0].Host, "10.0.0.11:22"; got != want {
		t.Fatalf("committedHostPaths[0].host = %q, want %q", got, want)
	}
	if got, want := out.CommittedHostPaths[0].RepoPath, hostRepoInputPath; got != want {
		t.Fatalf("committedHostPaths[0].repoPath = %q, want %q", got, want)
	}
	if got, want := out.CommittedHostPaths[0].Path, "/etc/kubernetes/kubeadm.yaml"; got != want {
		t.Fatalf("committedHostPaths[0].path = %q, want %q", got, want)
	}

	for _, path := range []string{hostRepoInputPath, hostBundleInputPath} {
		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("ReadFile(%q) error = %v", path, err)
		}
		if got, want := string(data), remoteLive; got != want {
			t.Fatalf("%q content = %q, want %q", path, got, want)
		}
	}
	for _, path := range []string{repoInputPath, bundleInputPath} {
		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("ReadFile(%q) error = %v", path, err)
		}
		if got, want := string(data), desired; got != want {
			t.Fatalf("%q content = %q, want unchanged %q", path, got, want)
		}
	}
}

func TestSyncRevertCmd(t *testing.T) {
	previousRuntimeRoot := constants.DefaultRuntimeRootDir
	runtimeRoot := previousRuntimeRoot
	if runtimeRoot == "" {
		runtimeRoot = constants.GetRuntimeRootDir(constants.AppName)
	}
	constants.DefaultRuntimeRootDir = runtimeRoot
	t.Cleanup(func() {
		constants.DefaultRuntimeRootDir = previousRuntimeRoot
	})

	reverted := false
	applyCount := 0
	previousOutput := outputSyncKubectl
	outputSyncKubectl = func(args ...string) ([]byte, error) {
		command := strings.Join(args, " ")
		if strings.Contains(command, "apply -f ") {
			if len(args) == 0 {
				t.Fatal("apply called without arguments")
			}
			manifestPath := args[len(args)-1]
			data, err := os.ReadFile(manifestPath)
			if err != nil {
				t.Fatalf("ReadFile(%q) error = %v", manifestPath, err)
			}
			if !strings.Contains(string(data), "quay.io/cilium/cilium:v1.15.0") {
				t.Fatalf("applied manifest missing desired image:\n%s", string(data))
			}
			reverted = true
			applyCount++
			return []byte("daemonset.apps/cilium configured\n"), nil
		}
		if reverted {
			return []byte(`{"apiVersion":"apps/v1","kind":"DaemonSet","metadata":{"name":"cilium","namespace":"kube-system","resourceVersion":"2"},"spec":{"template":{"spec":{"containers":[{"name":"cilium-agent","image":"quay.io/cilium/cilium:v1.15.0"}]}}}}`), nil
		}
		return []byte(`{"apiVersion":"apps/v1","kind":"DaemonSet","metadata":{"name":"cilium","namespace":"kube-system","resourceVersion":"2"},"spec":{"template":{"spec":{"containers":[{"name":"cilium-agent","image":"quay.io/cilium/cilium:v1.15.1"}]}}}}`), nil
	}
	t.Cleanup(func() {
		outputSyncKubectl = previousOutput
	})

	clusterName := uniqueSyncClusterName(t, "revert")
	t.Cleanup(func() {
		_ = os.RemoveAll(filepath.Join(runtimeRoot, clusterName))
	})
	bundleDir := t.TempDir()

	bundle := &hydrate.Bundle{
		APIVersion: "distribution.sealos.io/v1alpha1",
		Kind:       "HydratedBundle",
		Spec: hydrate.BundleSpec{
			BOMName:  "minimal-single-node",
			Revision: "rev-poc-001",
			Channel:  bom.ChannelAlpha,
			TrackedK8sObjects: []hydrate.TrackedK8sObject{
				{
					APIVersion: "apps/v1",
					Kind:       "DaemonSet",
					Namespace:  "kube-system",
					Name:       "cilium",
					Path:       "components/cilium/files/manifests/cilium.yaml",
					Component:  "cilium",
					Source:     hydrate.InventorySourcePackageManifest,
					Ownership:  hydrate.InventoryOwnershipGlobal,
				},
			},
			Components: []hydrate.RenderedComponent{
				{Name: "cilium"},
			},
		},
	}
	manifestPath := filepath.Join(bundleDir, "components", "cilium", "files", "manifests", "cilium.yaml")
	if err := os.MkdirAll(filepath.Dir(manifestPath), 0o755); err != nil {
		t.Fatalf("MkdirAll(%q) error = %v", filepath.Dir(manifestPath), err)
	}
	if err := os.WriteFile(manifestPath, []byte("apiVersion: apps/v1\nkind: DaemonSet\nmetadata:\n  name: cilium\n  namespace: kube-system\nspec:\n  template:\n    spec:\n      containers:\n        - name: cilium-agent\n          image: quay.io/cilium/cilium:v1.15.0\n"), 0o644); err != nil {
		t.Fatalf("WriteFile(%q) error = %v", manifestPath, err)
	}
	if err := yamlutil.MarshalFile(filepath.Join(bundleDir, hydrate.BundleFileName), bundle); err != nil {
		t.Fatalf("MarshalFile(bundle) error = %v", err)
	}

	desiredStateDigest, err := hydrate.DigestBundle(bundleDir)
	if err != nil {
		t.Fatalf("DigestBundle() error = %v", err)
	}
	if _, err := state.PersistSuccessfulApply(clusterName, state.BOMReference{
		Name:     bundle.Spec.BOMName,
		Revision: bundle.Spec.Revision,
		Channel:  bundle.Spec.Channel,
	}, desiredStateDigest.String(), "local-rev-1", "patch-rev-1"); err != nil {
		t.Fatalf("PersistSuccessfulApply() error = %v", err)
	}

	buf := bytes.NewBuffer(nil)
	cmd := newSyncCmd()
	cmd.SetOut(buf)
	cmd.SetErr(buf)
	cmd.SetArgs([]string{
		"revert",
		"--cluster", clusterName,
		"--bundle-dir", bundleDir,
		"--kubeconfig", "/tmp/test-kubeconfig",
	})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute() error = %v", err)
	}

	var out struct {
		BeforeState          string `yaml:"beforeState"`
		CurrentState         string `yaml:"currentState"`
		Reverted             bool   `yaml:"reverted"`
		ObservationPersisted bool   `yaml:"observationPersisted"`
		RevertedObjects      []struct {
			Name  string `yaml:"name"`
			State string `yaml:"state"`
		} `yaml:"revertedObjects"`
		RecordedRevision struct {
			State string `yaml:"state"`
		} `yaml:"recordedRevision"`
	}
	if err := yaml.Unmarshal(buf.Bytes(), &out); err != nil {
		t.Fatalf("Unmarshal() error = %v\noutput=%s", err, buf.String())
	}
	if got, want := out.BeforeState, "Orphan"; got != want {
		t.Fatalf("beforeState = %q, want %q", got, want)
	}
	if got, want := out.CurrentState, "Clean"; got != want {
		t.Fatalf("currentState = %q, want %q", got, want)
	}
	if !out.Reverted {
		t.Fatal("reverted = false, want true")
	}
	if !out.ObservationPersisted {
		t.Fatal("observationPersisted = false, want true")
	}
	if got, want := len(out.RevertedObjects), 1; got != want {
		t.Fatalf("len(revertedObjects) = %d, want %d", got, want)
	}
	if got, want := out.RevertedObjects[0].Name, "cilium"; got != want {
		t.Fatalf("revertedObjects[0].name = %q, want %q", got, want)
	}
	if got, want := out.RevertedObjects[0].State, "Orphan"; got != want {
		t.Fatalf("revertedObjects[0].state = %q, want %q", got, want)
	}
	if got, want := out.RecordedRevision.State, "Clean"; got != want {
		t.Fatalf("recordedRevision.state = %q, want %q", got, want)
	}
	if got, want := applyCount, 1; got != want {
		t.Fatalf("applyCount = %d, want %d", got, want)
	}
}

func TestSyncRevertCmdWithLocalScopeAndObjectSelector(t *testing.T) {
	previousRuntimeRoot := constants.DefaultRuntimeRootDir
	runtimeRoot := previousRuntimeRoot
	if runtimeRoot == "" {
		runtimeRoot = constants.GetRuntimeRootDir(constants.AppName)
	}
	constants.DefaultRuntimeRootDir = runtimeRoot
	t.Cleanup(func() {
		constants.DefaultRuntimeRootDir = previousRuntimeRoot
	})

	secretReverted := false
	applyCount := 0
	previousOutput := outputSyncKubectl
	outputSyncKubectl = func(args ...string) ([]byte, error) {
		command := strings.Join(args, " ")
		switch {
		case strings.Contains(command, "apply -f "):
			manifestPath := args[len(args)-1]
			data, err := os.ReadFile(manifestPath)
			if err != nil {
				t.Fatalf("ReadFile(%q) error = %v", manifestPath, err)
			}
			text := string(data)
			if !strings.Contains(text, "name: grafana-admin-credentials") {
				t.Fatalf("applied manifest targets unexpected object:\n%s", text)
			}
			secretReverted = true
			applyCount++
			return []byte("secret/grafana-admin-credentials configured\n"), nil
		case strings.Contains(command, "get Secret grafana-admin-credentials"):
			if secretReverted {
				return []byte(`{"apiVersion":"v1","kind":"Secret","metadata":{"name":"grafana-admin-credentials","namespace":"default","resourceVersion":"2"},"type":"Opaque","data":{"username":"YWRtaW4=","password":"cGFzc3cwcmQ="}}`), nil
			}
			return []byte(`{"apiVersion":"v1","kind":"Secret","metadata":{"name":"grafana-admin-credentials","namespace":"default","resourceVersion":"2"},"type":"Opaque","data":{"username":"b3Bz","password":"bmV3LXBhc3M="}}`), nil
		case strings.Contains(command, "get DaemonSet cilium"):
			return []byte(`{"apiVersion":"apps/v1","kind":"DaemonSet","metadata":{"name":"cilium","namespace":"kube-system","resourceVersion":"2"},"spec":{"template":{"spec":{"containers":[{"name":"cilium-agent","image":"quay.io/cilium/cilium:v1.15.1"}]}}}}`), nil
		default:
			t.Fatalf("unexpected kubectl invocation: %s", command)
			return nil, nil
		}
	}
	t.Cleanup(func() {
		outputSyncKubectl = previousOutput
	})

	clusterName := uniqueSyncClusterName(t, "revert-local")
	t.Cleanup(func() {
		_ = os.RemoveAll(filepath.Join(runtimeRoot, clusterName))
	})
	bundleDir := t.TempDir()

	bundle := &hydrate.Bundle{
		APIVersion: "distribution.sealos.io/v1alpha1",
		Kind:       "HydratedBundle",
		Spec: hydrate.BundleSpec{
			BOMName:  "minimal-single-node",
			Revision: "rev-poc-001",
			Channel:  bom.ChannelAlpha,
			TrackedK8sObjects: []hydrate.TrackedK8sObject{
				{
					APIVersion: "apps/v1",
					Kind:       "DaemonSet",
					Namespace:  "kube-system",
					Name:       "cilium",
					Path:       "components/cilium/files/manifests/cilium.yaml",
					Component:  "cilium",
					Source:     hydrate.InventorySourcePackageManifest,
					Ownership:  hydrate.InventoryOwnershipGlobal,
				},
				{
					APIVersion: "v1",
					Kind:       "Secret",
					Namespace:  "default",
					Name:       "grafana-admin-credentials",
					Path:       "local-resources/secrets/grafana-admin-credentials.yaml",
					Source:     hydrate.InventorySourceLocalResource,
					Ownership:  hydrate.InventoryOwnershipLocal,
				},
			},
			Components: []hydrate.RenderedComponent{
				{Name: "cilium"},
				{Name: "local-resources"},
			},
		},
	}
	manifestPath := filepath.Join(bundleDir, "components", "cilium", "files", "manifests", "cilium.yaml")
	resourcePath := filepath.Join(bundleDir, "local-resources", "secrets", "grafana-admin-credentials.yaml")
	for _, path := range []string{manifestPath, resourcePath} {
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatalf("MkdirAll(%q) error = %v", filepath.Dir(path), err)
		}
	}
	if err := os.WriteFile(manifestPath, []byte("apiVersion: apps/v1\nkind: DaemonSet\nmetadata:\n  name: cilium\n  namespace: kube-system\nspec:\n  template:\n    spec:\n      containers:\n        - name: cilium-agent\n          image: quay.io/cilium/cilium:v1.15.0\n"), 0o644); err != nil {
		t.Fatalf("WriteFile(%q) error = %v", manifestPath, err)
	}
	if err := os.WriteFile(resourcePath, []byte("apiVersion: v1\nkind: Secret\nmetadata:\n  name: grafana-admin-credentials\n  namespace: default\nstringData:\n  username: admin\n  password: passw0rd\n"), 0o644); err != nil {
		t.Fatalf("WriteFile(%q) error = %v", resourcePath, err)
	}
	if err := yamlutil.MarshalFile(filepath.Join(bundleDir, hydrate.BundleFileName), bundle); err != nil {
		t.Fatalf("MarshalFile(bundle) error = %v", err)
	}

	desiredStateDigest, err := hydrate.DigestBundle(bundleDir)
	if err != nil {
		t.Fatalf("DigestBundle() error = %v", err)
	}
	if _, err := state.PersistSuccessfulApply(clusterName, state.BOMReference{
		Name:     bundle.Spec.BOMName,
		Revision: bundle.Spec.Revision,
		Channel:  bundle.Spec.Channel,
	}, desiredStateDigest.String(), "local-rev-1", "patch-rev-1"); err != nil {
		t.Fatalf("PersistSuccessfulApply() error = %v", err)
	}

	buf := bytes.NewBuffer(nil)
	cmd := newSyncCmd()
	cmd.SetOut(buf)
	cmd.SetErr(buf)
	cmd.SetArgs([]string{
		"revert",
		"--cluster", clusterName,
		"--bundle-dir", bundleDir,
		"--kubeconfig", "/tmp/test-kubeconfig",
		"--scope", "local",
		"--kind", "Secret",
		"--namespace", "default",
		"--name", "grafana-admin-credentials",
	})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute() error = %v", err)
	}

	var out struct {
		RequestedScope  string `yaml:"requestedScope"`
		RequestedObject struct {
			Kind      string `yaml:"kind"`
			Namespace string `yaml:"namespace"`
			Name      string `yaml:"name"`
		} `yaml:"requestedObject"`
		BeforeState          string `yaml:"beforeState"`
		CurrentState         string `yaml:"currentState"`
		Reverted             bool   `yaml:"reverted"`
		ObservationPersisted bool   `yaml:"observationPersisted"`
		RevertedObjects      []struct {
			Name  string `yaml:"name"`
			State string `yaml:"state"`
		} `yaml:"revertedObjects"`
	}
	if err := yaml.Unmarshal(buf.Bytes(), &out); err != nil {
		t.Fatalf("Unmarshal() error = %v\noutput=%s", err, buf.String())
	}
	if got, want := out.RequestedScope, "local"; got != want {
		t.Fatalf("requestedScope = %q, want %q", got, want)
	}
	if got, want := out.RequestedObject.Kind, "Secret"; got != want {
		t.Fatalf("requestedObject.kind = %q, want %q", got, want)
	}
	if got, want := out.RequestedObject.Namespace, "default"; got != want {
		t.Fatalf("requestedObject.namespace = %q, want %q", got, want)
	}
	if got, want := out.RequestedObject.Name, "grafana-admin-credentials"; got != want {
		t.Fatalf("requestedObject.name = %q, want %q", got, want)
	}
	if got, want := out.BeforeState, "Orphan"; got != want {
		t.Fatalf("beforeState = %q, want %q", got, want)
	}
	if got, want := out.CurrentState, "Orphan"; got != want {
		t.Fatalf("currentState = %q, want %q", got, want)
	}
	if !out.Reverted {
		t.Fatal("reverted = false, want true")
	}
	if !out.ObservationPersisted {
		t.Fatal("observationPersisted = false, want true")
	}
	if got, want := len(out.RevertedObjects), 1; got != want {
		t.Fatalf("len(revertedObjects) = %d, want %d", got, want)
	}
	if got, want := out.RevertedObjects[0].Name, "grafana-admin-credentials"; got != want {
		t.Fatalf("revertedObjects[0].name = %q, want %q", got, want)
	}
	if got, want := out.RevertedObjects[0].State, "Dirty"; got != want {
		t.Fatalf("revertedObjects[0].state = %q, want %q", got, want)
	}
	if got, want := applyCount, 1; got != want {
		t.Fatalf("applyCount = %d, want %d", got, want)
	}
}

func TestSyncRevertCmdRejectsLocalScopeForOrphan(t *testing.T) {
	previousRuntimeRoot := constants.DefaultRuntimeRootDir
	runtimeRoot := previousRuntimeRoot
	if runtimeRoot == "" {
		runtimeRoot = constants.GetRuntimeRootDir(constants.AppName)
	}
	constants.DefaultRuntimeRootDir = runtimeRoot
	t.Cleanup(func() {
		constants.DefaultRuntimeRootDir = previousRuntimeRoot
	})

	previousOutput := outputSyncKubectl
	applyCalled := false
	outputSyncKubectl = func(args ...string) ([]byte, error) {
		command := strings.Join(args, " ")
		if strings.Contains(command, "apply -f ") {
			applyCalled = true
			return nil, fmt.Errorf("unexpected apply")
		}
		return []byte(`{"apiVersion":"apps/v1","kind":"DaemonSet","metadata":{"name":"cilium","namespace":"kube-system","resourceVersion":"2"},"spec":{"template":{"spec":{"containers":[{"name":"cilium-agent","image":"quay.io/cilium/cilium:v1.15.1"}]}}}}`), nil
	}
	t.Cleanup(func() {
		outputSyncKubectl = previousOutput
	})

	clusterName := uniqueSyncClusterName(t, "revert-local-orphan")
	t.Cleanup(func() {
		_ = os.RemoveAll(filepath.Join(runtimeRoot, clusterName))
	})
	bundleDir := t.TempDir()

	bundle := &hydrate.Bundle{
		APIVersion: "distribution.sealos.io/v1alpha1",
		Kind:       "HydratedBundle",
		Spec: hydrate.BundleSpec{
			BOMName:  "minimal-single-node",
			Revision: "rev-poc-001",
			Channel:  bom.ChannelAlpha,
			TrackedK8sObjects: []hydrate.TrackedK8sObject{
				{
					APIVersion: "apps/v1",
					Kind:       "DaemonSet",
					Namespace:  "kube-system",
					Name:       "cilium",
					Path:       "components/cilium/files/manifests/cilium.yaml",
					Component:  "cilium",
					Source:     hydrate.InventorySourcePackageManifest,
					Ownership:  hydrate.InventoryOwnershipGlobal,
				},
			},
			Components: []hydrate.RenderedComponent{
				{Name: "cilium"},
			},
		},
	}
	manifestPath := filepath.Join(bundleDir, "components", "cilium", "files", "manifests", "cilium.yaml")
	if err := os.MkdirAll(filepath.Dir(manifestPath), 0o755); err != nil {
		t.Fatalf("MkdirAll(%q) error = %v", filepath.Dir(manifestPath), err)
	}
	if err := os.WriteFile(manifestPath, []byte("apiVersion: apps/v1\nkind: DaemonSet\nmetadata:\n  name: cilium\n  namespace: kube-system\nspec:\n  template:\n    spec:\n      containers:\n        - name: cilium-agent\n          image: quay.io/cilium/cilium:v1.15.0\n"), 0o644); err != nil {
		t.Fatalf("WriteFile(%q) error = %v", manifestPath, err)
	}
	if err := yamlutil.MarshalFile(filepath.Join(bundleDir, hydrate.BundleFileName), bundle); err != nil {
		t.Fatalf("MarshalFile(bundle) error = %v", err)
	}

	desiredStateDigest, err := hydrate.DigestBundle(bundleDir)
	if err != nil {
		t.Fatalf("DigestBundle() error = %v", err)
	}
	if _, err := state.PersistSuccessfulApply(clusterName, state.BOMReference{
		Name:     bundle.Spec.BOMName,
		Revision: bundle.Spec.Revision,
		Channel:  bundle.Spec.Channel,
	}, desiredStateDigest.String(), "local-rev-1", "patch-rev-1"); err != nil {
		t.Fatalf("PersistSuccessfulApply() error = %v", err)
	}

	cmd := newSyncCmd()
	cmd.SetArgs([]string{
		"revert",
		"--cluster", clusterName,
		"--bundle-dir", bundleDir,
		"--kubeconfig", "/tmp/test-kubeconfig",
		"--scope", "local",
	})
	if err := cmd.Execute(); err == nil {
		t.Fatal("Execute() error = nil, want error")
	}
	if applyCalled {
		t.Fatal("runSyncApply called, want revert preflight rejection")
	}
}

func TestSyncRevertCmdRestoresMissingObject(t *testing.T) {
	previousRuntimeRoot := constants.DefaultRuntimeRootDir
	runtimeRoot := previousRuntimeRoot
	if runtimeRoot == "" {
		runtimeRoot = constants.GetRuntimeRootDir(constants.AppName)
	}
	constants.DefaultRuntimeRootDir = runtimeRoot
	t.Cleanup(func() {
		constants.DefaultRuntimeRootDir = previousRuntimeRoot
	})

	recreated := false
	applyCount := 0
	previousOutput := outputSyncKubectl
	outputSyncKubectl = func(args ...string) ([]byte, error) {
		command := strings.Join(args, " ")
		switch {
		case strings.Contains(command, "apply -f "):
			manifestPath := args[len(args)-1]
			data, err := os.ReadFile(manifestPath)
			if err != nil {
				t.Fatalf("ReadFile(%q) error = %v", manifestPath, err)
			}
			if !strings.Contains(string(data), "name: cilium") {
				t.Fatalf("applied manifest targets unexpected object:\n%s", string(data))
			}
			recreated = true
			applyCount++
			return []byte("daemonset.apps/cilium created\n"), nil
		case strings.Contains(command, "get DaemonSet cilium"):
			if recreated {
				return []byte(`{"apiVersion":"apps/v1","kind":"DaemonSet","metadata":{"name":"cilium","namespace":"kube-system","resourceVersion":"2"},"spec":{"template":{"spec":{"containers":[{"name":"cilium-agent","image":"quay.io/cilium/cilium:v1.15.0"}]}}}}`), nil
			}
			return nil, fmt.Errorf("Error from server (NotFound): daemonsets.apps \"cilium\" not found")
		default:
			t.Fatalf("unexpected kubectl invocation: %s", command)
			return nil, nil
		}
	}
	t.Cleanup(func() {
		outputSyncKubectl = previousOutput
	})

	clusterName := uniqueSyncClusterName(t, "revert-missing-object")
	t.Cleanup(func() {
		_ = os.RemoveAll(filepath.Join(runtimeRoot, clusterName))
	})
	bundleDir := t.TempDir()

	bundle := &hydrate.Bundle{
		APIVersion: "distribution.sealos.io/v1alpha1",
		Kind:       "HydratedBundle",
		Spec: hydrate.BundleSpec{
			BOMName:  "minimal-single-node",
			Revision: "rev-poc-001",
			Channel:  bom.ChannelAlpha,
			TrackedK8sObjects: []hydrate.TrackedK8sObject{
				{
					APIVersion: "apps/v1",
					Kind:       "DaemonSet",
					Namespace:  "kube-system",
					Name:       "cilium",
					Path:       "components/cilium/files/manifests/cilium.yaml",
					Component:  "cilium",
					Source:     hydrate.InventorySourcePackageManifest,
					Ownership:  hydrate.InventoryOwnershipGlobal,
				},
			},
			Components: []hydrate.RenderedComponent{
				{Name: "cilium"},
			},
		},
	}
	manifestPath := filepath.Join(bundleDir, "components", "cilium", "files", "manifests", "cilium.yaml")
	if err := os.MkdirAll(filepath.Dir(manifestPath), 0o755); err != nil {
		t.Fatalf("MkdirAll(%q) error = %v", filepath.Dir(manifestPath), err)
	}
	if err := os.WriteFile(manifestPath, []byte("apiVersion: apps/v1\nkind: DaemonSet\nmetadata:\n  name: cilium\n  namespace: kube-system\nspec:\n  template:\n    spec:\n      containers:\n        - name: cilium-agent\n          image: quay.io/cilium/cilium:v1.15.0\n"), 0o644); err != nil {
		t.Fatalf("WriteFile(%q) error = %v", manifestPath, err)
	}
	if err := yamlutil.MarshalFile(filepath.Join(bundleDir, hydrate.BundleFileName), bundle); err != nil {
		t.Fatalf("MarshalFile(bundle) error = %v", err)
	}

	desiredStateDigest, err := hydrate.DigestBundle(bundleDir)
	if err != nil {
		t.Fatalf("DigestBundle() error = %v", err)
	}
	if _, err := state.PersistSuccessfulApply(clusterName, state.BOMReference{
		Name:     bundle.Spec.BOMName,
		Revision: bundle.Spec.Revision,
		Channel:  bundle.Spec.Channel,
	}, desiredStateDigest.String(), "local-rev-1", "patch-rev-1"); err != nil {
		t.Fatalf("PersistSuccessfulApply() error = %v", err)
	}

	buf := bytes.NewBuffer(nil)
	cmd := newSyncCmd()
	cmd.SetOut(buf)
	cmd.SetErr(buf)
	cmd.SetArgs([]string{
		"revert",
		"--cluster", clusterName,
		"--bundle-dir", bundleDir,
		"--kubeconfig", "/tmp/test-kubeconfig",
	})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute() error = %v", err)
	}

	var out struct {
		BeforeState     string `yaml:"beforeState"`
		CurrentState    string `yaml:"currentState"`
		Reverted        bool   `yaml:"reverted"`
		RevertedObjects []struct {
			Name     string `yaml:"name"`
			State    string `yaml:"state"`
			Presence string `yaml:"presence"`
		} `yaml:"revertedObjects"`
	}
	if err := yaml.Unmarshal(buf.Bytes(), &out); err != nil {
		t.Fatalf("Unmarshal() error = %v\noutput=%s", err, buf.String())
	}
	if got, want := out.BeforeState, "Orphan"; got != want {
		t.Fatalf("beforeState = %q, want %q", got, want)
	}
	if got, want := out.CurrentState, "Clean"; got != want {
		t.Fatalf("currentState = %q, want %q", got, want)
	}
	if !out.Reverted {
		t.Fatal("reverted = false, want true")
	}
	if got, want := len(out.RevertedObjects), 1; got != want {
		t.Fatalf("len(revertedObjects) = %d, want %d", got, want)
	}
	if got, want := out.RevertedObjects[0].Name, "cilium"; got != want {
		t.Fatalf("revertedObjects[0].name = %q, want %q", got, want)
	}
	if got, want := out.RevertedObjects[0].State, "Orphan"; got != want {
		t.Fatalf("revertedObjects[0].state = %q, want %q", got, want)
	}
	if got, want := out.RevertedObjects[0].Presence, "missing"; got != want {
		t.Fatalf("revertedObjects[0].presence = %q, want %q", got, want)
	}
	if got, want := applyCount, 1; got != want {
		t.Fatalf("applyCount = %d, want %d", got, want)
	}
}

func TestSyncRevertCmdWithHostPathSelector(t *testing.T) {
	previousRuntimeRoot := constants.DefaultRuntimeRootDir
	runtimeRoot := previousRuntimeRoot
	if runtimeRoot == "" {
		runtimeRoot = constants.GetRuntimeRootDir(constants.AppName)
	}
	constants.DefaultRuntimeRootDir = runtimeRoot
	t.Cleanup(func() {
		constants.DefaultRuntimeRootDir = previousRuntimeRoot
	})

	previousOutput := outputSyncKubectl
	outputSyncKubectl = func(args ...string) ([]byte, error) {
		command := strings.Join(args, " ")
		switch {
		case strings.Contains(command, "get DaemonSet cilium"):
			return []byte(`{"apiVersion":"apps/v1","kind":"DaemonSet","metadata":{"name":"cilium","namespace":"kube-system","resourceVersion":"2"},"spec":{"template":{"spec":{"containers":[{"name":"cilium-agent","image":"quay.io/cilium/cilium:v1.15.1"}]}}}}`), nil
		default:
			t.Fatalf("unexpected kubectl invocation: %s", command)
			return nil, nil
		}
	}
	t.Cleanup(func() {
		outputSyncKubectl = previousOutput
	})

	clusterName := uniqueSyncClusterName(t, "revert-hostpath")
	t.Cleanup(func() {
		_ = os.RemoveAll(filepath.Join(runtimeRoot, clusterName))
	})
	bundleDir := t.TempDir()
	hostRoot := t.TempDir()

	bundle := &hydrate.Bundle{
		APIVersion: "distribution.sealos.io/v1alpha1",
		Kind:       "HydratedBundle",
		Spec: hydrate.BundleSpec{
			BOMName:  "minimal-single-node",
			Revision: "rev-poc-001",
			Channel:  bom.ChannelAlpha,
			TrackedK8sObjects: []hydrate.TrackedK8sObject{
				{
					APIVersion: "apps/v1",
					Kind:       "DaemonSet",
					Namespace:  "kube-system",
					Name:       "cilium",
					Path:       "components/cilium/files/manifests/cilium.yaml",
					Component:  "cilium",
					Source:     hydrate.InventorySourcePackageManifest,
					Ownership:  hydrate.InventoryOwnershipGlobal,
				},
			},
			TrackedHostPaths: []hydrate.TrackedHostPath{
				{
					HostPath:   "/usr/bin/kubelet",
					BundlePath: "components/kubernetes/files/rootfs/usr/bin/kubelet",
					Component:  "kubernetes",
					Source:     hydrate.InventorySourcePackageManifest,
					Ownership:  hydrate.InventoryOwnershipGlobal,
					Type:       hydrate.HostPathRegularFile,
				},
			},
			Components: []hydrate.RenderedComponent{
				{Name: "kubernetes"},
				{Name: "cilium"},
			},
		},
	}

	manifestPath := filepath.Join(bundleDir, "components", "cilium", "files", "manifests", "cilium.yaml")
	desiredHostPath := filepath.Join(bundleDir, "components", "kubernetes", "files", "rootfs", "usr", "bin", "kubelet")
	for _, path := range []string{manifestPath, desiredHostPath} {
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatalf("MkdirAll(%q) error = %v", filepath.Dir(path), err)
		}
	}
	if err := os.WriteFile(manifestPath, []byte("apiVersion: apps/v1\nkind: DaemonSet\nmetadata:\n  name: cilium\n  namespace: kube-system\nspec:\n  template:\n    spec:\n      containers:\n        - name: cilium-agent\n          image: quay.io/cilium/cilium:v1.15.0\n"), 0o644); err != nil {
		t.Fatalf("WriteFile(%q) error = %v", manifestPath, err)
	}
	if err := os.WriteFile(desiredHostPath, []byte("desired-kubelet"), 0o755); err != nil {
		t.Fatalf("WriteFile(%q) error = %v", desiredHostPath, err)
	}
	liveHostPath := filepath.Join(hostRoot, "usr", "bin", "kubelet")
	if err := os.MkdirAll(filepath.Dir(liveHostPath), 0o755); err != nil {
		t.Fatalf("MkdirAll(%q) error = %v", filepath.Dir(liveHostPath), err)
	}
	if err := os.WriteFile(liveHostPath, []byte("drifted-kubelet"), 0o755); err != nil {
		t.Fatalf("WriteFile(%q) error = %v", liveHostPath, err)
	}
	if err := yamlutil.MarshalFile(filepath.Join(bundleDir, hydrate.BundleFileName), bundle); err != nil {
		t.Fatalf("MarshalFile(bundle) error = %v", err)
	}

	desiredStateDigest, err := hydrate.DigestBundle(bundleDir)
	if err != nil {
		t.Fatalf("DigestBundle() error = %v", err)
	}
	if _, err := state.PersistSuccessfulApply(clusterName, state.BOMReference{
		Name:     bundle.Spec.BOMName,
		Revision: bundle.Spec.Revision,
		Channel:  bundle.Spec.Channel,
	}, desiredStateDigest.String(), "local-rev-1", "patch-rev-1"); err != nil {
		t.Fatalf("PersistSuccessfulApply() error = %v", err)
	}

	buf := bytes.NewBuffer(nil)
	cmd := newSyncCmd()
	cmd.SetOut(buf)
	cmd.SetErr(buf)
	cmd.SetArgs([]string{
		"revert",
		"--cluster", clusterName,
		"--bundle-dir", bundleDir,
		"--kubeconfig", "/tmp/test-kubeconfig",
		"--host-root", hostRoot,
		"--host-path", "/usr/bin/kubelet",
	})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute() error = %v", err)
	}

	data, err := os.ReadFile(liveHostPath)
	if err != nil {
		t.Fatalf("ReadFile(%q) error = %v", liveHostPath, err)
	}
	if got, want := string(data), "desired-kubelet"; got != want {
		t.Fatalf("live host path content = %q, want %q", got, want)
	}

	var out struct {
		RequestedHostPath string `yaml:"requestedHostPath"`
		BeforeState       string `yaml:"beforeState"`
		CurrentState      string `yaml:"currentState"`
		Reverted          bool   `yaml:"reverted"`
		RevertedHostPaths []struct {
			Path    string   `yaml:"path"`
			State   string   `yaml:"state"`
			Reasons []string `yaml:"reasons"`
		} `yaml:"revertedHostPaths"`
	}
	if err := yaml.Unmarshal(buf.Bytes(), &out); err != nil {
		t.Fatalf("Unmarshal() error = %v\noutput=%s", err, buf.String())
	}
	if got, want := out.RequestedHostPath, "/usr/bin/kubelet"; got != want {
		t.Fatalf("requestedHostPath = %q, want %q", got, want)
	}
	if got, want := out.BeforeState, "Orphan"; got != want {
		t.Fatalf("beforeState = %q, want %q", got, want)
	}
	if got, want := out.CurrentState, "Orphan"; got != want {
		t.Fatalf("currentState = %q, want %q", got, want)
	}
	if !out.Reverted {
		t.Fatal("reverted = false, want true")
	}
	if got, want := len(out.RevertedHostPaths), 1; got != want {
		t.Fatalf("len(revertedHostPaths) = %d, want %d", got, want)
	}
	if got, want := out.RevertedHostPaths[0].Path, "/usr/bin/kubelet"; got != want {
		t.Fatalf("revertedHostPaths[0].path = %q, want %q", got, want)
	}
	if got, want := out.RevertedHostPaths[0].State, "Orphan"; got != want {
		t.Fatalf("revertedHostPaths[0].state = %q, want %q", got, want)
	}
	if got, want := out.RevertedHostPaths[0].Reasons[0], "contentMismatch"; got != want {
		t.Fatalf("revertedHostPaths[0].reasons[0] = %q, want %q", got, want)
	}
}

func TestSyncRevertCmdRestoresMissingLocalHostPath(t *testing.T) {
	previousRuntimeRoot := constants.DefaultRuntimeRootDir
	runtimeRoot := previousRuntimeRoot
	if runtimeRoot == "" {
		runtimeRoot = constants.GetRuntimeRootDir(constants.AppName)
	}
	constants.DefaultRuntimeRootDir = runtimeRoot
	t.Cleanup(func() {
		constants.DefaultRuntimeRootDir = previousRuntimeRoot
	})

	previousOutput := outputSyncKubectl
	outputSyncKubectl = func(args ...string) ([]byte, error) {
		t.Fatalf("unexpected kubectl invocation: %s", strings.Join(args, " "))
		return nil, nil
	}
	t.Cleanup(func() {
		outputSyncKubectl = previousOutput
	})

	clusterName := uniqueSyncClusterName(t, "revert-missing-local-hostpath")
	t.Cleanup(func() {
		_ = os.RemoveAll(filepath.Join(runtimeRoot, clusterName))
	})
	bundleDir := t.TempDir()
	hostRoot := t.TempDir()

	bundle := &hydrate.Bundle{
		APIVersion: "distribution.sealos.io/v1alpha1",
		Kind:       "HydratedBundle",
		Spec: hydrate.BundleSpec{
			BOMName:  "minimal-single-node",
			Revision: "rev-poc-001",
			Channel:  bom.ChannelAlpha,
			TrackedHostPaths: []hydrate.TrackedHostPath{
				{
					HostPath:   "/etc/kubernetes/kubeadm.yaml",
					BundlePath: "components/kubernetes/files/files/etc/kubernetes/kubeadm.yaml",
					Component:  "kubernetes",
					Source:     hydrate.InventorySourceLocalInput,
					Ownership:  hydrate.InventoryOwnershipLocal,
					Type:       hydrate.HostPathRegularFile,
					InputName:  "kubeadm-cluster-config",
					HostInputBindings: map[string]string{
						"10.0.0.11": "components/kubernetes/host-inputs/10.0.0.11/files/etc/kubernetes/kubeadm.yaml",
					},
				},
			},
			Components: []hydrate.RenderedComponent{
				{Name: "kubernetes"},
			},
		},
	}
	desired := "apiVersion: kubeadm.k8s.io/v1beta4\nkind: ClusterConfiguration\nclusterName: restored-local\n"
	desiredHostPath := filepath.Join(bundleDir, "components", "kubernetes", "files", "files", "etc", "kubernetes", "kubeadm.yaml")
	hostDesiredHostPath := filepath.Join(bundleDir, "components", "kubernetes", "host-inputs", "10.0.0.11", "files", "etc", "kubernetes", "kubeadm.yaml")
	for _, path := range []string{desiredHostPath, hostDesiredHostPath} {
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatalf("MkdirAll(%q) error = %v", filepath.Dir(path), err)
		}
		if err := os.WriteFile(path, []byte(desired), 0o644); err != nil {
			t.Fatalf("WriteFile(%q) error = %v", path, err)
		}
	}
	if err := yamlutil.MarshalFile(filepath.Join(bundleDir, hydrate.BundleFileName), bundle); err != nil {
		t.Fatalf("MarshalFile(bundle) error = %v", err)
	}

	desiredStateDigest, err := hydrate.DigestBundle(bundleDir)
	if err != nil {
		t.Fatalf("DigestBundle() error = %v", err)
	}
	if _, err := state.PersistSuccessfulApply(clusterName, state.BOMReference{
		Name:     bundle.Spec.BOMName,
		Revision: bundle.Spec.Revision,
		Channel:  bundle.Spec.Channel,
	}, desiredStateDigest.String(), "local-rev-1", "patch-rev-1"); err != nil {
		t.Fatalf("PersistSuccessfulApply() error = %v", err)
	}

	buf := bytes.NewBuffer(nil)
	cmd := newSyncCmd()
	cmd.SetOut(buf)
	cmd.SetErr(buf)
	cmd.SetArgs([]string{
		"revert",
		"--cluster", clusterName,
		"--bundle-dir", bundleDir,
		"--kubeconfig", "/tmp/test-kubeconfig",
		"--host-root", hostRoot,
		"--scope", "local",
		"--host-path", "/etc/kubernetes/kubeadm.yaml",
	})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute() error = %v", err)
	}

	liveHostPath := filepath.Join(hostRoot, "etc", "kubernetes", "kubeadm.yaml")
	data, err := os.ReadFile(liveHostPath)
	if err != nil {
		t.Fatalf("ReadFile(%q) error = %v", liveHostPath, err)
	}
	if got, want := string(data), desired; got != want {
		t.Fatalf("live host path content = %q, want %q", got, want)
	}

	var out struct {
		RequestedScope    string `yaml:"requestedScope"`
		RequestedHostPath string `yaml:"requestedHostPath"`
		BeforeState       string `yaml:"beforeState"`
		CurrentState      string `yaml:"currentState"`
		Reverted          bool   `yaml:"reverted"`
		RevertedHostPaths []struct {
			Path     string   `yaml:"path"`
			State    string   `yaml:"state"`
			Presence string   `yaml:"presence"`
			Reasons  []string `yaml:"reasons"`
		} `yaml:"revertedHostPaths"`
	}
	if err := yaml.Unmarshal(buf.Bytes(), &out); err != nil {
		t.Fatalf("Unmarshal() error = %v\noutput=%s", err, buf.String())
	}
	if got, want := out.RequestedScope, "local"; got != want {
		t.Fatalf("requestedScope = %q, want %q", got, want)
	}
	if got, want := out.RequestedHostPath, "/etc/kubernetes/kubeadm.yaml"; got != want {
		t.Fatalf("requestedHostPath = %q, want %q", got, want)
	}
	if got, want := out.BeforeState, "Dirty"; got != want {
		t.Fatalf("beforeState = %q, want %q", got, want)
	}
	if got, want := out.CurrentState, "Clean"; got != want {
		t.Fatalf("currentState = %q, want %q", got, want)
	}
	if !out.Reverted {
		t.Fatal("reverted = false, want true")
	}
	if got, want := len(out.RevertedHostPaths), 1; got != want {
		t.Fatalf("len(revertedHostPaths) = %d, want %d", got, want)
	}
	if got, want := out.RevertedHostPaths[0].Path, "/etc/kubernetes/kubeadm.yaml"; got != want {
		t.Fatalf("revertedHostPaths[0].path = %q, want %q", got, want)
	}
	if got, want := out.RevertedHostPaths[0].State, "Dirty"; got != want {
		t.Fatalf("revertedHostPaths[0].state = %q, want %q", got, want)
	}
	if got, want := out.RevertedHostPaths[0].Presence, "missing"; got != want {
		t.Fatalf("revertedHostPaths[0].presence = %q, want %q", got, want)
	}
	if got, want := len(out.RevertedHostPaths[0].Reasons), 1; got != want {
		t.Fatalf("len(revertedHostPaths[0].reasons) = %d, want %d", got, want)
	}
	if got, want := out.RevertedHostPaths[0].Reasons[0], "missing"; got != want {
		t.Fatalf("revertedHostPaths[0].reasons[0] = %q, want %q", got, want)
	}
}

func TestSyncRevertCmdSkipsGeneratedHostPathDrift(t *testing.T) {
	previousRuntimeRoot := constants.DefaultRuntimeRootDir
	runtimeRoot := previousRuntimeRoot
	if runtimeRoot == "" {
		runtimeRoot = constants.GetRuntimeRootDir(constants.AppName)
	}
	constants.DefaultRuntimeRootDir = runtimeRoot
	t.Cleanup(func() {
		constants.DefaultRuntimeRootDir = previousRuntimeRoot
	})

	previousOutput := outputSyncKubectl
	outputSyncKubectl = func(args ...string) ([]byte, error) {
		t.Fatalf("unexpected kubectl invocation: %s", strings.Join(args, " "))
		return nil, nil
	}
	t.Cleanup(func() {
		outputSyncKubectl = previousOutput
	})

	clusterName := uniqueSyncClusterName(t, "revert-generated-skip")
	t.Cleanup(func() {
		_ = os.RemoveAll(filepath.Join(runtimeRoot, clusterName))
	})
	bundleDir := t.TempDir()
	hostRoot := t.TempDir()

	bundle := &hydrate.Bundle{
		APIVersion: "distribution.sealos.io/v1alpha1",
		Kind:       "HydratedBundle",
		Spec: hydrate.BundleSpec{
			BOMName:  "minimal-single-node",
			Revision: "rev-poc-001",
			Channel:  bom.ChannelAlpha,
			TrackedHostPaths: []hydrate.TrackedHostPath{
				{
					HostPath:        "/etc/kubernetes/manifests/kube-apiserver.yaml",
					Component:       "kubernetes",
					Source:          hydrate.InventorySourceGeneratedHook,
					Ownership:       hydrate.InventoryOwnershipGlobal,
					Type:            hydrate.HostPathRegularFile,
					ProjectionClass: hydrate.HostPathProjectionClassGenerated,
					CompareStrategy: hydrate.HostPathCompareStrategySemanticGenerated,
					Generated: &hydrate.GeneratedHostPathSemantics{
						Tool:          "kubeadm",
						Hook:          "bootstrap",
						APIVersion:    "v1",
						Kind:          "Pod",
						Namespace:     "kube-system",
						Name:          "kube-apiserver",
						ContainerName: "kube-apiserver",
					},
				},
			},
			Components: []hydrate.RenderedComponent{
				{Name: "kubernetes"},
			},
		},
	}
	if err := yamlutil.MarshalFile(filepath.Join(bundleDir, hydrate.BundleFileName), bundle); err != nil {
		t.Fatalf("MarshalFile(bundle) error = %v", err)
	}

	desiredStateDigest, err := hydrate.DigestBundle(bundleDir)
	if err != nil {
		t.Fatalf("DigestBundle() error = %v", err)
	}
	if _, err := state.PersistSuccessfulApply(clusterName, state.BOMReference{
		Name:     bundle.Spec.BOMName,
		Revision: bundle.Spec.Revision,
		Channel:  bundle.Spec.Channel,
	}, desiredStateDigest.String(), "local-rev-1", "patch-rev-1"); err != nil {
		t.Fatalf("PersistSuccessfulApply() error = %v", err)
	}

	buf := bytes.NewBuffer(nil)
	cmd := newSyncCmd()
	cmd.SetOut(buf)
	cmd.SetErr(buf)
	cmd.SetArgs([]string{
		"revert",
		"--cluster", clusterName,
		"--bundle-dir", bundleDir,
		"--kubeconfig", "/tmp/test-kubeconfig",
		"--host-root", hostRoot,
	})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute() error = %v", err)
	}

	var out struct {
		BeforeState       string `yaml:"beforeState"`
		CurrentState      string `yaml:"currentState"`
		Reverted          bool   `yaml:"reverted"`
		RevertedHostPaths []struct {
			Path string `yaml:"path"`
		} `yaml:"revertedHostPaths"`
	}
	if err := yaml.Unmarshal(buf.Bytes(), &out); err != nil {
		t.Fatalf("Unmarshal() error = %v\noutput=%s", err, buf.String())
	}
	if got, want := out.BeforeState, "Orphan"; got != want {
		t.Fatalf("beforeState = %q, want %q", got, want)
	}
	if got, want := out.CurrentState, "Orphan"; got != want {
		t.Fatalf("currentState = %q, want %q", got, want)
	}
	if out.Reverted {
		t.Fatal("reverted = true, want false")
	}
	if got, want := len(out.RevertedHostPaths), 0; got != want {
		t.Fatalf("len(revertedHostPaths) = %d, want %d", got, want)
	}
}

func TestSyncRevertCmdRepairsGeneratedControlPlaneHostPath(t *testing.T) {
	previousRuntimeRoot := constants.DefaultRuntimeRootDir
	runtimeRoot := previousRuntimeRoot
	if runtimeRoot == "" {
		runtimeRoot = constants.GetRuntimeRootDir(constants.AppName)
	}
	constants.DefaultRuntimeRootDir = runtimeRoot
	t.Cleanup(func() {
		constants.DefaultRuntimeRootDir = previousRuntimeRoot
	})

	previousOutput := outputSyncKubectl
	outputSyncKubectl = func(args ...string) ([]byte, error) {
		t.Fatalf("unexpected kubectl invocation: %s", strings.Join(args, " "))
		return nil, nil
	}
	t.Cleanup(func() {
		outputSyncKubectl = previousOutput
	})

	previousRepair := repairSyncGeneratedControlPlaneHost
	var repairCalls []reconcile.RepairGeneratedControlPlaneHostOptions
	repairSyncGeneratedControlPlaneHost = func(opts reconcile.RepairGeneratedControlPlaneHostOptions) error {
		repairCalls = append(repairCalls, opts)
		targetPath := filepath.Join(opts.HostRoot, "etc", "kubernetes", "manifests", "kube-scheduler.yaml")
		if err := os.MkdirAll(filepath.Dir(targetPath), 0o755); err != nil {
			return err
		}
		return os.WriteFile(targetPath, []byte(`apiVersion: v1
kind: Pod
metadata:
  name: kube-scheduler
  namespace: kube-system
spec:
  containers:
    - name: kube-scheduler
      image: registry.k8s.io/kube-scheduler:v1.30.3
      command:
        - kube-scheduler
`), 0o644)
	}
	t.Cleanup(func() {
		repairSyncGeneratedControlPlaneHost = previousRepair
	})

	clusterName := uniqueSyncClusterName(t, "revert-generated-repair")
	t.Cleanup(func() {
		_ = os.RemoveAll(filepath.Join(runtimeRoot, clusterName))
	})
	bundleDir := t.TempDir()
	hostRoot := t.TempDir()
	livePath := filepath.Join(hostRoot, "etc", "kubernetes", "manifests", "kube-scheduler.yaml")
	if err := os.MkdirAll(filepath.Dir(livePath), 0o755); err != nil {
		t.Fatalf("MkdirAll(live) error = %v", err)
	}
	if err := os.WriteFile(livePath, []byte(`apiVersion: v1
kind: Pod
metadata:
  name: kube-scheduler
  namespace: kube-system
spec:
  containers:
    - name: kube-scheduler
      image: registry.k8s.io/kube-scheduler:v1.30.2
      command:
        - kube-scheduler
`), 0o644); err != nil {
		t.Fatalf("WriteFile(live) error = %v", err)
	}

	bundle := &hydrate.Bundle{
		APIVersion: "distribution.sealos.io/v1alpha1",
		Kind:       "HydratedBundle",
		Spec: hydrate.BundleSpec{
			BOMName:  "minimal-single-node",
			Revision: "rev-poc-001",
			Channel:  bom.ChannelAlpha,
			TrackedHostPaths: []hydrate.TrackedHostPath{{
				HostPath:        "/etc/kubernetes/manifests/kube-scheduler.yaml",
				Component:       "kubernetes",
				Source:          hydrate.InventorySourceGeneratedHook,
				Ownership:       hydrate.InventoryOwnershipGlobal,
				Type:            hydrate.HostPathRegularFile,
				ProjectionClass: hydrate.HostPathProjectionClassGenerated,
				CompareStrategy: hydrate.HostPathCompareStrategySemanticGenerated,
				Generated: &hydrate.GeneratedHostPathSemantics{
					Tool:            "kubeadm",
					Hook:            "bootstrap",
					APIVersion:      "v1",
					Kind:            "Pod",
					Namespace:       "kube-system",
					Name:            "kube-scheduler",
					ContainerName:   "kube-scheduler",
					ExpectedImage:   "registry.k8s.io/kube-scheduler:v1.30.3",
					ExpectedCommand: "kube-scheduler",
				},
			}},
			Components: []hydrate.RenderedComponent{{
				Name:        "kubernetes",
				PackageName: "kubernetes-rootfs",
				Steps: []hydrate.RenderedStep{{
					Name:        "kubeadm-defaults",
					Kind:        hydrate.StepContent,
					BundlePath:  "components/kubernetes/files/files/etc/kubernetes/kubeadm.yaml",
					SourcePath:  "files/etc/kubernetes/kubeadm.yaml",
					ContentType: packageformat.ContentFile,
				}},
			}},
		},
	}
	if err := yamlutil.MarshalFile(filepath.Join(bundleDir, hydrate.BundleFileName), bundle); err != nil {
		t.Fatalf("MarshalFile(bundle) error = %v", err)
	}

	desiredStateDigest, err := hydrate.DigestBundle(bundleDir)
	if err != nil {
		t.Fatalf("DigestBundle() error = %v", err)
	}
	if _, err := state.PersistSuccessfulApply(clusterName, state.BOMReference{
		Name:     bundle.Spec.BOMName,
		Revision: bundle.Spec.Revision,
		Channel:  bundle.Spec.Channel,
	}, desiredStateDigest.String(), "local-rev-1", "patch-rev-1"); err != nil {
		t.Fatalf("PersistSuccessfulApply() error = %v", err)
	}

	buf := bytes.NewBuffer(nil)
	cmd := newSyncCmd()
	cmd.SetOut(buf)
	cmd.SetErr(buf)
	cmd.SetArgs([]string{
		"revert",
		"--cluster", clusterName,
		"--bundle-dir", bundleDir,
		"--kubeconfig", "/tmp/test-kubeconfig",
		"--host-root", hostRoot,
	})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute() error = %v", err)
	}

	if got, want := len(repairCalls), 1; got != want {
		t.Fatalf("len(repairCalls) = %d, want %d", got, want)
	}
	if got, want := repairCalls[0].ClusterName, clusterName; got != want {
		t.Fatalf("repairCalls[0].ClusterName = %q, want %q", got, want)
	}
	if got, want := repairCalls[0].Host, syncLocalExecutionHost; got != want {
		t.Fatalf("repairCalls[0].Host = %q, want %q", got, want)
	}

	var out struct {
		BeforeState       string `yaml:"beforeState"`
		CurrentState      string `yaml:"currentState"`
		Reverted          bool   `yaml:"reverted"`
		RevertedHostPaths []struct {
			Path  string `yaml:"path"`
			State string `yaml:"state"`
		} `yaml:"revertedHostPaths"`
	}
	if err := yaml.Unmarshal(buf.Bytes(), &out); err != nil {
		t.Fatalf("Unmarshal() error = %v\noutput=%s", err, buf.String())
	}
	if got, want := out.BeforeState, "Orphan"; got != want {
		t.Fatalf("beforeState = %q, want %q", got, want)
	}
	if got, want := out.CurrentState, "Clean"; got != want {
		t.Fatalf("currentState = %q, want %q", got, want)
	}
	if !out.Reverted {
		t.Fatal("reverted = false, want true")
	}
	if got, want := len(out.RevertedHostPaths), 1; got != want {
		t.Fatalf("len(revertedHostPaths) = %d, want %d", got, want)
	}
	if got, want := out.RevertedHostPaths[0].Path, "/etc/kubernetes/manifests/kube-scheduler.yaml"; got != want {
		t.Fatalf("revertedHostPaths[0].path = %q, want %q", got, want)
	}
}

func TestSyncRevertCmdRejectsLocalScopeForGlobalHostPath(t *testing.T) {
	previousRuntimeRoot := constants.DefaultRuntimeRootDir
	runtimeRoot := previousRuntimeRoot
	if runtimeRoot == "" {
		runtimeRoot = constants.GetRuntimeRootDir(constants.AppName)
	}
	constants.DefaultRuntimeRootDir = runtimeRoot
	t.Cleanup(func() {
		constants.DefaultRuntimeRootDir = previousRuntimeRoot
	})

	previousOutput := outputSyncKubectl
	outputSyncKubectl = func(args ...string) ([]byte, error) {
		t.Fatalf("unexpected kubectl invocation: %s", strings.Join(args, " "))
		return nil, nil
	}
	t.Cleanup(func() {
		outputSyncKubectl = previousOutput
	})

	clusterName := uniqueSyncClusterName(t, "revert-local-hostpath-orphan")
	t.Cleanup(func() {
		_ = os.RemoveAll(filepath.Join(runtimeRoot, clusterName))
	})
	bundleDir := t.TempDir()
	hostRoot := t.TempDir()

	bundle := &hydrate.Bundle{
		APIVersion: "distribution.sealos.io/v1alpha1",
		Kind:       "HydratedBundle",
		Spec: hydrate.BundleSpec{
			BOMName:  "minimal-single-node",
			Revision: "rev-poc-001",
			Channel:  bom.ChannelAlpha,
			TrackedHostPaths: []hydrate.TrackedHostPath{
				{
					HostPath:   "/usr/bin/kubelet",
					BundlePath: "components/kubernetes/files/rootfs/usr/bin/kubelet",
					Component:  "kubernetes",
					Source:     hydrate.InventorySourcePackageManifest,
					Ownership:  hydrate.InventoryOwnershipGlobal,
					Type:       hydrate.HostPathRegularFile,
				},
			},
			Components: []hydrate.RenderedComponent{
				{Name: "kubernetes"},
			},
		},
	}

	desiredHostPath := filepath.Join(bundleDir, "components", "kubernetes", "files", "rootfs", "usr", "bin", "kubelet")
	if err := os.MkdirAll(filepath.Dir(desiredHostPath), 0o755); err != nil {
		t.Fatalf("MkdirAll(%q) error = %v", filepath.Dir(desiredHostPath), err)
	}
	if err := os.WriteFile(desiredHostPath, []byte("desired-kubelet"), 0o755); err != nil {
		t.Fatalf("WriteFile(%q) error = %v", desiredHostPath, err)
	}
	liveHostPath := filepath.Join(hostRoot, "usr", "bin", "kubelet")
	if err := os.MkdirAll(filepath.Dir(liveHostPath), 0o755); err != nil {
		t.Fatalf("MkdirAll(%q) error = %v", filepath.Dir(liveHostPath), err)
	}
	if err := os.WriteFile(liveHostPath, []byte("drifted-kubelet"), 0o755); err != nil {
		t.Fatalf("WriteFile(%q) error = %v", liveHostPath, err)
	}
	if err := yamlutil.MarshalFile(filepath.Join(bundleDir, hydrate.BundleFileName), bundle); err != nil {
		t.Fatalf("MarshalFile(bundle) error = %v", err)
	}

	desiredStateDigest, err := hydrate.DigestBundle(bundleDir)
	if err != nil {
		t.Fatalf("DigestBundle() error = %v", err)
	}
	if _, err := state.PersistSuccessfulApply(clusterName, state.BOMReference{
		Name:     bundle.Spec.BOMName,
		Revision: bundle.Spec.Revision,
		Channel:  bundle.Spec.Channel,
	}, desiredStateDigest.String(), "local-rev-1", "patch-rev-1"); err != nil {
		t.Fatalf("PersistSuccessfulApply() error = %v", err)
	}

	cmd := newSyncCmd()
	cmd.SetArgs([]string{
		"revert",
		"--cluster", clusterName,
		"--bundle-dir", bundleDir,
		"--kubeconfig", "/tmp/test-kubeconfig",
		"--host-root", hostRoot,
		"--scope", "local",
		"--host-path", "/usr/bin/kubelet",
	})
	if err := cmd.Execute(); err == nil {
		t.Fatal("Execute() error = nil, want error")
	}

	data, err := os.ReadFile(liveHostPath)
	if err != nil {
		t.Fatalf("ReadFile(%q) error = %v", liveHostPath, err)
	}
	if got, want := string(data), "drifted-kubelet"; got != want {
		t.Fatalf("live host path content = %q, want %q", got, want)
	}
}

func TestSyncStatusCmdWithMultiNodeRemoteHostPathDrift(t *testing.T) {
	previousRuntimeRoot := constants.DefaultRuntimeRootDir
	runtimeRoot := previousRuntimeRoot
	if runtimeRoot == "" {
		runtimeRoot = constants.GetRuntimeRootDir(constants.AppName)
	}
	constants.DefaultRuntimeRootDir = runtimeRoot
	t.Cleanup(func() {
		constants.DefaultRuntimeRootDir = previousRuntimeRoot
	})

	previousOutput := outputSyncKubectl
	outputSyncKubectl = func(args ...string) ([]byte, error) {
		t.Fatalf("unexpected kubectl invocation: %s", strings.Join(args, " "))
		return nil, nil
	}
	t.Cleanup(func() {
		outputSyncKubectl = previousOutput
	})

	localHostRoot := t.TempDir()
	remoteHostRoot := t.TempDir()
	desiredContent := "desired-kubelet"
	if err := writeSyncRemoteHostFixture(localHostRoot, "/usr/bin/kubelet", desiredContent, 0o755); err != nil {
		t.Fatalf("writeSyncRemoteHostFixture(local) error = %v", err)
	}
	if err := writeSyncRemoteHostFixture(remoteHostRoot, "/usr/bin/kubelet", "drifted-kubelet", 0o755); err != nil {
		t.Fatalf("writeSyncRemoteHostFixture(remote) error = %v", err)
	}

	previousTopologyLoader := loadSyncExecutionTopology
	previousRemoteExecutor := newSyncRemoteExecutor
	fakeRemote := &fakeSyncRemoteExecutor{
		roots: map[string]string{
			"10.0.0.11:22": remoteHostRoot,
		},
	}
	loadSyncExecutionTopology = func(string) (*syncExecutionTopology, error) {
		return &syncExecutionTopology{
			allNodes:    []string{syncLocalExecutionHost, "10.0.0.11:22"},
			firstMaster: syncLocalExecutionHost,
		}, nil
	}
	newSyncRemoteExecutor = func(*syncExecutionTopology) (syncRemoteExecutor, error) {
		return fakeRemote, nil
	}
	t.Cleanup(func() {
		loadSyncExecutionTopology = previousTopologyLoader
		newSyncRemoteExecutor = previousRemoteExecutor
	})

	clusterName := uniqueSyncClusterName(t, "status-multinode-hostpath")
	t.Cleanup(func() {
		_ = os.RemoveAll(filepath.Join(runtimeRoot, clusterName))
	})
	bundleDir := t.TempDir()
	desiredPath := filepath.Join(bundleDir, "components", "kubernetes", "files", "rootfs", "usr", "bin", "kubelet")
	hostDesiredPath := filepath.Join(bundleDir, "components", "kubernetes", "host-inputs", "10.0.0.11", "rootfs", "usr", "bin", "kubelet")
	hostDesiredContent := "desired-kubelet-host-11"
	for path, content := range map[string]string{
		desiredPath:     desiredContent,
		hostDesiredPath: hostDesiredContent,
	} {
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatalf("MkdirAll(%q) error = %v", path, err)
		}
		if err := os.WriteFile(path, []byte(content), 0o755); err != nil {
			t.Fatalf("WriteFile(%q) error = %v", path, err)
		}
	}

	bundle := &hydrate.Bundle{
		APIVersion: "distribution.sealos.io/v1alpha1",
		Kind:       "HydratedBundle",
		Spec: hydrate.BundleSpec{
			BOMName:  "minimal-single-node",
			Revision: "rev-poc-001",
			Channel:  bom.ChannelAlpha,
			TrackedHostPaths: []hydrate.TrackedHostPath{
				{
					HostPath:   "/usr/bin/kubelet",
					BundlePath: "components/kubernetes/files/rootfs/usr/bin/kubelet",
					Component:  "kubernetes",
					Source:     hydrate.InventorySourcePackageManifest,
					Ownership:  hydrate.InventoryOwnershipGlobal,
					Type:       hydrate.HostPathRegularFile,
					HostInputBindings: map[string]string{
						"10.0.0.11": "components/kubernetes/host-inputs/10.0.0.11/rootfs/usr/bin/kubelet",
					},
				},
			},
			Components: []hydrate.RenderedComponent{
				{Name: "kubernetes"},
			},
		},
	}
	if err := yamlutil.MarshalFile(filepath.Join(bundleDir, hydrate.BundleFileName), bundle); err != nil {
		t.Fatalf("MarshalFile(bundle) error = %v", err)
	}
	desiredStateDigest, err := hydrate.DigestBundle(bundleDir)
	if err != nil {
		t.Fatalf("DigestBundle() error = %v", err)
	}
	if _, err := state.PersistSuccessfulApply(clusterName, state.BOMReference{
		Name:     bundle.Spec.BOMName,
		Revision: bundle.Spec.Revision,
		Channel:  bundle.Spec.Channel,
	}, desiredStateDigest.String(), "local-rev-1", "patch-rev-1"); err != nil {
		t.Fatalf("PersistSuccessfulApply() error = %v", err)
	}

	buf := bytes.NewBuffer(nil)
	cmd := newSyncCmd()
	cmd.SetOut(buf)
	cmd.SetErr(buf)
	cmd.SetArgs([]string{
		"status",
		"--cluster", clusterName,
		"--bundle-dir", bundleDir,
		"--kubeconfig", "/tmp/test-kubeconfig",
		"--host-root", localHostRoot,
	})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute() error = %v", err)
	}

	var out struct {
		CurrentState string `yaml:"currentState"`
		Headline     string `yaml:"headline"`
		Summary      struct {
			Total  int `yaml:"total"`
			Clean  int `yaml:"clean"`
			Orphan int `yaml:"orphan"`
		} `yaml:"summary"`
		OrphanHostPaths []struct {
			Host           string `yaml:"host"`
			Path           string `yaml:"path"`
			OperatorAction string `yaml:"operatorAction"`
		} `yaml:"orphanHostPaths"`
	}
	if err := yaml.Unmarshal(buf.Bytes(), &out); err != nil {
		t.Fatalf("Unmarshal() error = %v\noutput=%s", err, buf.String())
	}
	if got, want := out.CurrentState, "Orphan"; got != want {
		t.Fatalf("currentState = %q, want %q", got, want)
	}
	if got, want := out.Summary.Total, 2; got != want {
		t.Fatalf("summary.total = %d, want %d", got, want)
	}
	if got, want := out.Summary.Clean, 1; got != want {
		t.Fatalf("summary.clean = %d, want %d", got, want)
	}
	if got, want := out.Summary.Orphan, 1; got != want {
		t.Fatalf("summary.orphan = %d, want %d", got, want)
	}
	if got, want := len(out.OrphanHostPaths), 1; got != want {
		t.Fatalf("len(orphanHostPaths) = %d, want %d", got, want)
	}
	if got, want := out.OrphanHostPaths[0].Host, "10.0.0.11:22"; got != want {
		t.Fatalf("orphanHostPaths[0].host = %q, want %q", got, want)
	}
	if got, want := out.OrphanHostPaths[0].Path, "/usr/bin/kubelet"; got != want {
		t.Fatalf("orphanHostPaths[0].path = %q, want %q", got, want)
	}
	if got, want := out.OrphanHostPaths[0].OperatorAction, "revertOrUpdateGlobalBaseline"; got != want {
		t.Fatalf("orphanHostPaths[0].operatorAction = %q, want %q", got, want)
	}
	if !strings.Contains(out.Headline, "orphanHostPaths=1") {
		t.Fatalf("headline = %q, want orphanHostPaths=1 and hostPathConflicts=0", out.Headline)
	}
}

func TestSyncStatusCmdReportsHostPathConflicts(t *testing.T) {
	previousRuntimeRoot := constants.DefaultRuntimeRootDir
	runtimeRoot := previousRuntimeRoot
	if runtimeRoot == "" {
		runtimeRoot = constants.GetRuntimeRootDir(constants.AppName)
	}
	constants.DefaultRuntimeRootDir = runtimeRoot
	t.Cleanup(func() {
		constants.DefaultRuntimeRootDir = previousRuntimeRoot
	})

	previousOutput := outputSyncKubectl
	outputSyncKubectl = func(args ...string) ([]byte, error) {
		t.Fatalf("unexpected kubectl invocation: %s", strings.Join(args, " "))
		return nil, nil
	}
	t.Cleanup(func() {
		outputSyncKubectl = previousOutput
	})

	localHostRoot := t.TempDir()
	remoteHostRoot := t.TempDir()
	if err := writeSyncRemoteHostFixture(localHostRoot, "/README", "desired-containerd", 0o644); err != nil {
		t.Fatalf("writeSyncRemoteHostFixture(local) error = %v", err)
	}
	if err := writeSyncRemoteHostFixture(remoteHostRoot, "/README", "drifted-readme", 0o644); err != nil {
		t.Fatalf("writeSyncRemoteHostFixture(remote) error = %v", err)
	}

	previousTopologyLoader := loadSyncExecutionTopology
	previousRemoteExecutor := newSyncRemoteExecutor
	fakeRemote := &fakeSyncRemoteExecutor{
		roots: map[string]string{
			"10.0.0.11:22": remoteHostRoot,
		},
	}
	loadSyncExecutionTopology = func(string) (*syncExecutionTopology, error) {
		return &syncExecutionTopology{
			allNodes:    []string{syncLocalExecutionHost, "10.0.0.11:22"},
			firstMaster: syncLocalExecutionHost,
		}, nil
	}
	newSyncRemoteExecutor = func(*syncExecutionTopology) (syncRemoteExecutor, error) {
		return fakeRemote, nil
	}
	t.Cleanup(func() {
		loadSyncExecutionTopology = previousTopologyLoader
		newSyncRemoteExecutor = previousRemoteExecutor
	})

	clusterName := uniqueSyncClusterName(t, "status-hostpath-conflict")
	t.Cleanup(func() {
		_ = os.RemoveAll(filepath.Join(runtimeRoot, clusterName))
	})
	bundleDir := t.TempDir()
	for _, entry := range []struct {
		component string
		content   string
	}{
		{component: "containerd", content: "desired-containerd"},
		{component: "kubernetes", content: "desired-kubernetes"},
	} {
		desiredPath := filepath.Join(bundleDir, "components", entry.component, "files", "rootfs", "README")
		if err := os.MkdirAll(filepath.Dir(desiredPath), 0o755); err != nil {
			t.Fatalf("MkdirAll(%s) error = %v", entry.component, err)
		}
		if err := os.WriteFile(desiredPath, []byte(entry.content), 0o644); err != nil {
			t.Fatalf("WriteFile(%s) error = %v", entry.component, err)
		}
	}

	bundle := &hydrate.Bundle{
		APIVersion: "distribution.sealos.io/v1alpha1",
		Kind:       "HydratedBundle",
		Spec: hydrate.BundleSpec{
			BOMName:  "minimal-single-node",
			Revision: "rev-poc-001",
			Channel:  bom.ChannelAlpha,
			TrackedHostPaths: []hydrate.TrackedHostPath{
				{
					HostPath:   "/README",
					BundlePath: "components/containerd/files/rootfs/README",
					Component:  "containerd",
					Source:     hydrate.InventorySourcePackageManifest,
					Ownership:  hydrate.InventoryOwnershipGlobal,
					Type:       hydrate.HostPathRegularFile,
				},
				{
					HostPath:   "/README",
					BundlePath: "components/kubernetes/files/rootfs/README",
					Component:  "kubernetes",
					Source:     hydrate.InventorySourcePackageManifest,
					Ownership:  hydrate.InventoryOwnershipGlobal,
					Type:       hydrate.HostPathRegularFile,
				},
			},
			Components: []hydrate.RenderedComponent{
				{Name: "containerd"},
				{Name: "kubernetes"},
			},
		},
	}
	if err := yamlutil.MarshalFile(filepath.Join(bundleDir, hydrate.BundleFileName), bundle); err != nil {
		t.Fatalf("MarshalFile(bundle) error = %v", err)
	}
	desiredStateDigest, err := hydrate.DigestBundle(bundleDir)
	if err != nil {
		t.Fatalf("DigestBundle() error = %v", err)
	}
	if _, err := state.PersistSuccessfulApply(clusterName, state.BOMReference{
		Name:     bundle.Spec.BOMName,
		Revision: bundle.Spec.Revision,
		Channel:  bundle.Spec.Channel,
	}, desiredStateDigest.String(), "local-rev-1", "patch-rev-1"); err != nil {
		t.Fatalf("PersistSuccessfulApply() error = %v", err)
	}

	buf := bytes.NewBuffer(nil)
	cmd := newSyncCmd()
	cmd.SetOut(buf)
	cmd.SetErr(buf)
	cmd.SetArgs([]string{
		"status",
		"--cluster", clusterName,
		"--bundle-dir", bundleDir,
		"--kubeconfig", "/tmp/test-kubeconfig",
		"--host-root", localHostRoot,
	})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute() error = %v", err)
	}

	var out struct {
		HostPathConflicts []struct {
			Host       string   `yaml:"host"`
			Path       string   `yaml:"path"`
			Components []string `yaml:"components"`
			Hint       string   `yaml:"hint"`
		} `yaml:"hostPathConflicts"`
	}
	if err := yaml.Unmarshal(buf.Bytes(), &out); err != nil {
		t.Fatalf("Unmarshal() error = %v\noutput=%s", err, buf.String())
	}
	if got := len(out.HostPathConflicts); got < 1 {
		t.Fatalf("len(hostPathConflicts) = %d, want at least 1", got)
	}
	var remoteConflict *struct {
		Host       string   `yaml:"host"`
		Path       string   `yaml:"path"`
		Components []string `yaml:"components"`
		Hint       string   `yaml:"hint"`
	}
	for i := range out.HostPathConflicts {
		if out.HostPathConflicts[i].Host == "10.0.0.11:22" && out.HostPathConflicts[i].Path == "/README" {
			remoteConflict = &out.HostPathConflicts[i]
			break
		}
	}
	if remoteConflict == nil {
		t.Fatalf("hostPathConflicts missing remote /README entry: %#v", out.HostPathConflicts)
	}
	if got, want := len(remoteConflict.Components), 2; got != want {
		t.Fatalf("len(remoteConflict.components) = %d, want %d", got, want)
	}
	if got, want := remoteConflict.Hint, "select a single component when reverting this host path"; got != want {
		t.Fatalf("remoteConflict.hint = %q, want %q", got, want)
	}
}

func TestSyncStatusCmdReportsLocalInputHostSplits(t *testing.T) {
	previousRuntimeRoot := constants.DefaultRuntimeRootDir
	runtimeRoot := previousRuntimeRoot
	if runtimeRoot == "" {
		runtimeRoot = constants.GetRuntimeRootDir(constants.AppName)
	}
	constants.DefaultRuntimeRootDir = runtimeRoot
	t.Cleanup(func() {
		constants.DefaultRuntimeRootDir = previousRuntimeRoot
	})

	previousOutput := outputSyncKubectl
	outputSyncKubectl = func(args ...string) ([]byte, error) {
		t.Fatalf("unexpected kubectl invocation: %s", strings.Join(args, " "))
		return nil, nil
	}
	t.Cleanup(func() {
		outputSyncKubectl = previousOutput
	})

	localHostRoot := t.TempDir()
	remoteHostRoot := t.TempDir()
	if err := writeSyncRemoteHostFixture(localHostRoot, "/etc/kubernetes/kubeadm.yaml", "local-drift", 0o644); err != nil {
		t.Fatalf("writeSyncRemoteHostFixture(local) error = %v", err)
	}
	if err := writeSyncRemoteHostFixture(remoteHostRoot, "/etc/kubernetes/kubeadm.yaml", "remote-drift", 0o644); err != nil {
		t.Fatalf("writeSyncRemoteHostFixture(remote) error = %v", err)
	}

	previousTopologyLoader := loadSyncExecutionTopology
	previousRemoteExecutor := newSyncRemoteExecutor
	fakeRemote := &fakeSyncRemoteExecutor{
		roots: map[string]string{
			"10.0.0.11:22": remoteHostRoot,
		},
	}
	loadSyncExecutionTopology = func(string) (*syncExecutionTopology, error) {
		return &syncExecutionTopology{
			allNodes:    []string{syncLocalExecutionHost, "10.0.0.11:22"},
			firstMaster: syncLocalExecutionHost,
		}, nil
	}
	newSyncRemoteExecutor = func(*syncExecutionTopology) (syncRemoteExecutor, error) {
		return fakeRemote, nil
	}
	t.Cleanup(func() {
		loadSyncExecutionTopology = previousTopologyLoader
		newSyncRemoteExecutor = previousRemoteExecutor
	})

	clusterName := uniqueSyncClusterName(t, "status-local-input-split")
	t.Cleanup(func() {
		_ = os.RemoveAll(filepath.Join(runtimeRoot, clusterName))
	})
	bundleDir := t.TempDir()
	desiredPath := filepath.Join(bundleDir, "components", "kubernetes", "files", "files", "etc", "kubernetes", "kubeadm.yaml")
	hostDesiredPath := filepath.Join(bundleDir, "components", "kubernetes", "host-inputs", "10.0.0.11", "files", "etc", "kubernetes", "kubeadm.yaml")
	for _, path := range []string{desiredPath, hostDesiredPath} {
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatalf("MkdirAll(%q) error = %v", filepath.Dir(path), err)
		}
		if err := os.WriteFile(path, []byte("desired"), 0o644); err != nil {
			t.Fatalf("WriteFile(%q) error = %v", path, err)
		}
	}

	bundle := &hydrate.Bundle{
		APIVersion: "distribution.sealos.io/v1alpha1",
		Kind:       "HydratedBundle",
		Spec: hydrate.BundleSpec{
			BOMName:  "minimal-single-node",
			Revision: "rev-poc-001",
			Channel:  bom.ChannelAlpha,
			TrackedHostPaths: []hydrate.TrackedHostPath{
				{
					HostPath:   "/etc/kubernetes/kubeadm.yaml",
					BundlePath: "components/kubernetes/files/files/etc/kubernetes/kubeadm.yaml",
					Component:  "kubernetes",
					Source:     hydrate.InventorySourceLocalInput,
					Ownership:  hydrate.InventoryOwnershipLocal,
					Type:       hydrate.HostPathRegularFile,
					InputName:  "kubeadm-cluster-config",
					HostInputBindings: map[string]string{
						"10.0.0.11": "components/kubernetes/host-inputs/10.0.0.11/files/etc/kubernetes/kubeadm.yaml",
					},
				},
			},
			Components: []hydrate.RenderedComponent{
				{
					Name: "kubernetes",
					InputBindings: map[string]string{
						"kubeadm-cluster-config": filepath.Join(t.TempDir(), "inputs", "kubernetes", "kubeadm-cluster-config.yaml"),
					},
				},
			},
		},
	}
	if err := yamlutil.MarshalFile(filepath.Join(bundleDir, hydrate.BundleFileName), bundle); err != nil {
		t.Fatalf("MarshalFile(bundle) error = %v", err)
	}
	desiredStateDigest, err := hydrate.DigestBundle(bundleDir)
	if err != nil {
		t.Fatalf("DigestBundle() error = %v", err)
	}
	if _, err := state.PersistSuccessfulApply(clusterName, state.BOMReference{
		Name:     bundle.Spec.BOMName,
		Revision: bundle.Spec.Revision,
		Channel:  bundle.Spec.Channel,
	}, desiredStateDigest.String(), "local-rev-1", "patch-rev-1"); err != nil {
		t.Fatalf("PersistSuccessfulApply() error = %v", err)
	}

	buf := bytes.NewBuffer(nil)
	cmd := newSyncCmd()
	cmd.SetOut(buf)
	cmd.SetErr(buf)
	cmd.SetArgs([]string{
		"status",
		"--cluster", clusterName,
		"--bundle-dir", bundleDir,
		"--kubeconfig", "/tmp/test-kubeconfig",
		"--host-root", localHostRoot,
	})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute() error = %v", err)
	}

	var out struct {
		Headline       string `yaml:"headline"`
		DirtyHostPaths []struct {
			Host                 string `yaml:"host"`
			InputName            string `yaml:"inputName"`
			UsesHostScopedInput  bool   `yaml:"usesHostScopedInput"`
			HostInputBindingPath string `yaml:"hostInputBindingPath"`
		} `yaml:"dirtyHostPaths"`
		LocalInputHostSplits []struct {
			Path                    string   `yaml:"path"`
			Component               string   `yaml:"component"`
			InputName               string   `yaml:"inputName"`
			Hosts                   []string `yaml:"hosts"`
			HostsWithScopedInput    []string `yaml:"hostsWithScopedInput"`
			HostsWithoutScopedInput []string `yaml:"hostsWithoutScopedInput"`
			Hint                    string   `yaml:"hint"`
		} `yaml:"localInputHostSplits"`
	}
	if err := yaml.Unmarshal(buf.Bytes(), &out); err != nil {
		t.Fatalf("Unmarshal() error = %v\noutput=%s", err, buf.String())
	}
	if got, want := len(out.LocalInputHostSplits), 1; got != want {
		t.Fatalf("len(localInputHostSplits) = %d, want %d", got, want)
	}
	if got, want := out.LocalInputHostSplits[0].Path, "/etc/kubernetes/kubeadm.yaml"; got != want {
		t.Fatalf("localInputHostSplits[0].path = %q, want %q", got, want)
	}
	if got, want := out.LocalInputHostSplits[0].Component, "kubernetes"; got != want {
		t.Fatalf("localInputHostSplits[0].component = %q, want %q", got, want)
	}
	if got, want := out.LocalInputHostSplits[0].InputName, "kubeadm-cluster-config"; got != want {
		t.Fatalf("localInputHostSplits[0].inputName = %q, want %q", got, want)
	}
	if got, want := len(out.LocalInputHostSplits[0].Hosts), 2; got != want {
		t.Fatalf("len(localInputHostSplits[0].hosts) = %d, want %d", got, want)
	}
	if got, want := strings.Join(out.LocalInputHostSplits[0].HostsWithScopedInput, ","), "10.0.0.11:22"; got != want {
		t.Fatalf("localInputHostSplits[0].hostsWithScopedInput = %q, want %q", got, want)
	}
	if got, want := strings.Join(out.LocalInputHostSplits[0].HostsWithoutScopedInput, ","), "localhost"; got != want {
		t.Fatalf("localInputHostSplits[0].hostsWithoutScopedInput = %q, want %q", got, want)
	}
	if got, want := out.LocalInputHostSplits[0].Hint, "some hosts already have host-scoped input payloads; add host-scoped inputs for the remaining divergent hosts or commit an explicit host"; got != want {
		t.Fatalf("localInputHostSplits[0].hint = %q, want %q", got, want)
	}
	var remoteDirty *struct {
		Host                 string `yaml:"host"`
		InputName            string `yaml:"inputName"`
		UsesHostScopedInput  bool   `yaml:"usesHostScopedInput"`
		HostInputBindingPath string `yaml:"hostInputBindingPath"`
	}
	for i := range out.DirtyHostPaths {
		if out.DirtyHostPaths[i].Host == "10.0.0.11:22" {
			remoteDirty = &out.DirtyHostPaths[i]
			break
		}
	}
	if remoteDirty == nil {
		t.Fatalf("dirtyHostPaths missing remote host entry: %+v", out.DirtyHostPaths)
	}
	if !remoteDirty.UsesHostScopedInput {
		t.Fatalf("remote dirty host path usesHostScopedInput = false, want true")
	}
	if got, want := remoteDirty.InputName, "kubeadm-cluster-config"; got != want {
		t.Fatalf("remote dirty host path inputName = %q, want %q", got, want)
	}
	if got, want := remoteDirty.HostInputBindingPath, "components/kubernetes/host-inputs/10.0.0.11/files/etc/kubernetes/kubeadm.yaml"; got != want {
		t.Fatalf("remote dirty host path hostInputBindingPath = %q, want %q", got, want)
	}
	if !strings.Contains(out.Headline, "localInputHostSplits=1") {
		t.Fatalf("headline = %q, want localInputHostSplits=1", out.Headline)
	}
}

func TestStageSyncRemoteTrackedHostPathIsIdempotentForExistingFile(t *testing.T) {
	tempRoot := t.TempDir()
	remoteRoot := t.TempDir()
	fakeRemote := &fakeSyncRemoteExecutor{
		roots: map[string]string{
			"192.168.0.240": remoteRoot,
		},
	}
	tracked := hydrate.TrackedHostPath{
		HostPath:   "/README",
		BundlePath: "components/kubernetes/files/rootfs/README",
		Component:  "kubernetes",
		Source:     hydrate.InventorySourcePackageManifest,
		Ownership:  hydrate.InventoryOwnershipGlobal,
		Type:       hydrate.HostPathRegularFile,
	}
	if err := writeSyncRemoteHostFixture(remoteRoot, tracked.HostPath, "first", 0o644); err != nil {
		t.Fatalf("writeSyncRemoteHostFixture() error = %v", err)
	}

	if err := stageSyncRemoteTrackedHostPath(fakeRemote, "192.168.0.240", tempRoot, tracked); err != nil {
		t.Fatalf("stageSyncRemoteTrackedHostPath(first) error = %v", err)
	}
	if err := writeSyncRemoteHostFixture(remoteRoot, tracked.HostPath, "second", 0o644); err != nil {
		t.Fatalf("writeSyncRemoteHostFixture(update) error = %v", err)
	}
	if err := stageSyncRemoteTrackedHostPath(fakeRemote, "192.168.0.240", tempRoot, tracked); err != nil {
		t.Fatalf("stageSyncRemoteTrackedHostPath(second) error = %v", err)
	}

	data, err := os.ReadFile(filepath.Join(tempRoot, "README"))
	if err != nil {
		t.Fatalf("ReadFile(staged README) error = %v", err)
	}
	if got, want := string(data), "second"; got != want {
		t.Fatalf("staged README content = %q, want %q", got, want)
	}
}

func TestSyncRevertCmdWithRemoteHostPathSelector(t *testing.T) {
	previousRuntimeRoot := constants.DefaultRuntimeRootDir
	runtimeRoot := previousRuntimeRoot
	if runtimeRoot == "" {
		runtimeRoot = constants.GetRuntimeRootDir(constants.AppName)
	}
	constants.DefaultRuntimeRootDir = runtimeRoot
	t.Cleanup(func() {
		constants.DefaultRuntimeRootDir = previousRuntimeRoot
	})

	previousOutput := outputSyncKubectl
	outputSyncKubectl = func(args ...string) ([]byte, error) {
		t.Fatalf("unexpected kubectl invocation: %s", strings.Join(args, " "))
		return nil, nil
	}
	t.Cleanup(func() {
		outputSyncKubectl = previousOutput
	})

	localHostRoot := t.TempDir()
	remoteHostRoot := t.TempDir()
	desiredContent := "desired-kubelet"
	if err := writeSyncRemoteHostFixture(localHostRoot, "/usr/bin/kubelet", desiredContent, 0o755); err != nil {
		t.Fatalf("writeSyncRemoteHostFixture(local) error = %v", err)
	}
	if err := writeSyncRemoteHostFixture(remoteHostRoot, "/usr/bin/kubelet", "drifted-kubelet", 0o755); err != nil {
		t.Fatalf("writeSyncRemoteHostFixture(remote) error = %v", err)
	}

	previousTopologyLoader := loadSyncExecutionTopology
	previousRemoteExecutor := newSyncRemoteExecutor
	fakeRemote := &fakeSyncRemoteExecutor{
		roots: map[string]string{
			"10.0.0.11:22": remoteHostRoot,
		},
	}
	loadSyncExecutionTopology = func(string) (*syncExecutionTopology, error) {
		return &syncExecutionTopology{
			allNodes:    []string{syncLocalExecutionHost, "10.0.0.11:22"},
			firstMaster: syncLocalExecutionHost,
		}, nil
	}
	newSyncRemoteExecutor = func(*syncExecutionTopology) (syncRemoteExecutor, error) {
		return fakeRemote, nil
	}
	t.Cleanup(func() {
		loadSyncExecutionTopology = previousTopologyLoader
		newSyncRemoteExecutor = previousRemoteExecutor
	})

	clusterName := uniqueSyncClusterName(t, "revert-remote-hostpath")
	t.Cleanup(func() {
		_ = os.RemoveAll(filepath.Join(runtimeRoot, clusterName))
	})
	bundleDir := t.TempDir()
	desiredPath := filepath.Join(bundleDir, "components", "kubernetes", "files", "rootfs", "usr", "bin", "kubelet")
	hostDesiredPath := filepath.Join(bundleDir, "components", "kubernetes", "host-inputs", "10.0.0.11", "rootfs", "usr", "bin", "kubelet")
	hostDesiredContent := "desired-kubelet-host-11"
	for path, content := range map[string]string{
		desiredPath:     desiredContent,
		hostDesiredPath: hostDesiredContent,
	} {
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatalf("MkdirAll(%q) error = %v", path, err)
		}
		if err := os.WriteFile(path, []byte(content), 0o755); err != nil {
			t.Fatalf("WriteFile(%q) error = %v", path, err)
		}
	}

	bundle := &hydrate.Bundle{
		APIVersion: "distribution.sealos.io/v1alpha1",
		Kind:       "HydratedBundle",
		Spec: hydrate.BundleSpec{
			BOMName:  "minimal-single-node",
			Revision: "rev-poc-001",
			Channel:  bom.ChannelAlpha,
			TrackedHostPaths: []hydrate.TrackedHostPath{
				{
					HostPath:   "/usr/bin/kubelet",
					BundlePath: "components/kubernetes/files/rootfs/usr/bin/kubelet",
					Component:  "kubernetes",
					Source:     hydrate.InventorySourcePackageManifest,
					Ownership:  hydrate.InventoryOwnershipGlobal,
					Type:       hydrate.HostPathRegularFile,
					HostInputBindings: map[string]string{
						"10.0.0.11": "components/kubernetes/host-inputs/10.0.0.11/rootfs/usr/bin/kubelet",
					},
				},
			},
			Components: []hydrate.RenderedComponent{
				{Name: "kubernetes"},
			},
		},
	}
	if err := yamlutil.MarshalFile(filepath.Join(bundleDir, hydrate.BundleFileName), bundle); err != nil {
		t.Fatalf("MarshalFile(bundle) error = %v", err)
	}
	desiredStateDigest, err := hydrate.DigestBundle(bundleDir)
	if err != nil {
		t.Fatalf("DigestBundle() error = %v", err)
	}
	if _, err := state.PersistSuccessfulApply(clusterName, state.BOMReference{
		Name:     bundle.Spec.BOMName,
		Revision: bundle.Spec.Revision,
		Channel:  bundle.Spec.Channel,
	}, desiredStateDigest.String(), "local-rev-1", "patch-rev-1"); err != nil {
		t.Fatalf("PersistSuccessfulApply() error = %v", err)
	}

	buf := bytes.NewBuffer(nil)
	cmd := newSyncCmd()
	cmd.SetOut(buf)
	cmd.SetErr(buf)
	cmd.SetArgs([]string{
		"revert",
		"--cluster", clusterName,
		"--bundle-dir", bundleDir,
		"--kubeconfig", "/tmp/test-kubeconfig",
		"--host-root", localHostRoot,
		"--host-path", "/usr/bin/kubelet",
		"--host", "10.0.0.11:22",
	})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute() error = %v", err)
	}

	localLivePath := filepath.Join(localHostRoot, "usr", "bin", "kubelet")
	localData, err := os.ReadFile(localLivePath)
	if err != nil {
		t.Fatalf("ReadFile(local) error = %v", err)
	}
	if got, want := string(localData), desiredContent; got != want {
		t.Fatalf("local host path content = %q, want %q", got, want)
	}
	remoteData, err := os.ReadFile(filepath.Join(remoteHostRoot, "usr", "bin", "kubelet"))
	if err != nil {
		t.Fatalf("ReadFile(remote) error = %v", err)
	}
	if got, want := string(remoteData), hostDesiredContent; got != want {
		t.Fatalf("remote host path content = %q, want %q", got, want)
	}

	var out struct {
		RequestedHost     string `yaml:"requestedHost"`
		RequestedHostPath string `yaml:"requestedHostPath"`
		BeforeState       string `yaml:"beforeState"`
		CurrentState      string `yaml:"currentState"`
		Reverted          bool   `yaml:"reverted"`
		CurrentCompare    struct {
			HostPaths []struct {
				Host       string `yaml:"host"`
				Comparison string `yaml:"comparison"`
				State      string `yaml:"state"`
				Tracked    struct {
					HostPath string `yaml:"hostPath"`
				} `yaml:"tracked"`
			} `yaml:"hostPaths"`
		} `yaml:"currentCompare"`
		RevertedHostPaths []struct {
			Host    string   `yaml:"host"`
			Path    string   `yaml:"path"`
			State   string   `yaml:"state"`
			Reasons []string `yaml:"reasons"`
		} `yaml:"revertedHostPaths"`
	}
	if err := yaml.Unmarshal(buf.Bytes(), &out); err != nil {
		t.Fatalf("Unmarshal() error = %v\noutput=%s", err, buf.String())
	}
	if got, want := out.RequestedHost, "10.0.0.11:22"; got != want {
		t.Fatalf("requestedHost = %q, want %q", got, want)
	}
	if got, want := out.RequestedHostPath, "/usr/bin/kubelet"; got != want {
		t.Fatalf("requestedHostPath = %q, want %q", got, want)
	}
	if got, want := out.BeforeState, "Orphan"; got != want {
		t.Fatalf("beforeState = %q, want %q", got, want)
	}
	if got, want := out.CurrentState, "Clean"; got != want {
		t.Fatalf("currentState = %q, want %q; currentHostPaths=%+v; cmds=%v; output=%s", got, want, out.CurrentCompare.HostPaths, fakeRemote.cmds, buf.String())
	}
	if !out.Reverted {
		t.Fatal("reverted = false, want true")
	}
	if got, want := len(out.RevertedHostPaths), 1; got != want {
		t.Fatalf("len(revertedHostPaths) = %d, want %d", got, want)
	}
	if got, want := out.RevertedHostPaths[0].Host, "10.0.0.11:22"; got != want {
		t.Fatalf("revertedHostPaths[0].host = %q, want %q", got, want)
	}
	if got, want := out.RevertedHostPaths[0].Path, "/usr/bin/kubelet"; got != want {
		t.Fatalf("revertedHostPaths[0].path = %q, want %q", got, want)
	}
	if got, want := out.RevertedHostPaths[0].State, "Orphan"; got != want {
		t.Fatalf("revertedHostPaths[0].state = %q, want %q", got, want)
	}
	if got, want := out.RevertedHostPaths[0].Reasons[0], "contentMismatch"; got != want {
		t.Fatalf("revertedHostPaths[0].reasons[0] = %q, want %q", got, want)
	}
}

func TestSyncRevertCmdRejectsAmbiguousRemoteHostPathWithoutComponent(t *testing.T) {
	previousRuntimeRoot := constants.DefaultRuntimeRootDir
	runtimeRoot := previousRuntimeRoot
	if runtimeRoot == "" {
		runtimeRoot = constants.GetRuntimeRootDir(constants.AppName)
	}
	constants.DefaultRuntimeRootDir = runtimeRoot
	t.Cleanup(func() {
		constants.DefaultRuntimeRootDir = previousRuntimeRoot
	})

	previousOutput := outputSyncKubectl
	outputSyncKubectl = func(args ...string) ([]byte, error) {
		t.Fatalf("unexpected kubectl invocation: %s", strings.Join(args, " "))
		return nil, nil
	}
	t.Cleanup(func() {
		outputSyncKubectl = previousOutput
	})

	localHostRoot := t.TempDir()
	remoteHostRoot := t.TempDir()
	desiredContainerd := "desired-containerd"
	desiredKubernetes := "desired-kubernetes"
	if err := writeSyncRemoteHostFixture(localHostRoot, "/README", desiredContainerd, 0o644); err != nil {
		t.Fatalf("writeSyncRemoteHostFixture(local) error = %v", err)
	}
	if err := writeSyncRemoteHostFixture(remoteHostRoot, "/README", "drifted-readme", 0o644); err != nil {
		t.Fatalf("writeSyncRemoteHostFixture(remote) error = %v", err)
	}

	previousTopologyLoader := loadSyncExecutionTopology
	previousRemoteExecutor := newSyncRemoteExecutor
	fakeRemote := &fakeSyncRemoteExecutor{
		roots: map[string]string{
			"10.0.0.11:22": remoteHostRoot,
		},
	}
	loadSyncExecutionTopology = func(string) (*syncExecutionTopology, error) {
		return &syncExecutionTopology{
			allNodes:    []string{syncLocalExecutionHost, "10.0.0.11:22"},
			firstMaster: syncLocalExecutionHost,
		}, nil
	}
	newSyncRemoteExecutor = func(*syncExecutionTopology) (syncRemoteExecutor, error) {
		return fakeRemote, nil
	}
	t.Cleanup(func() {
		loadSyncExecutionTopology = previousTopologyLoader
		newSyncRemoteExecutor = previousRemoteExecutor
	})

	clusterName := uniqueSyncClusterName(t, "revert-ambiguous-remote-readme")
	t.Cleanup(func() {
		_ = os.RemoveAll(filepath.Join(runtimeRoot, clusterName))
	})
	bundleDir := t.TempDir()
	for _, entry := range []struct {
		component string
		content   string
	}{
		{component: "containerd", content: desiredContainerd},
		{component: "kubernetes", content: desiredKubernetes},
	} {
		desiredPath := filepath.Join(bundleDir, "components", entry.component, "files", "rootfs", "README")
		if err := os.MkdirAll(filepath.Dir(desiredPath), 0o755); err != nil {
			t.Fatalf("MkdirAll(%s) error = %v", entry.component, err)
		}
		if err := os.WriteFile(desiredPath, []byte(entry.content), 0o644); err != nil {
			t.Fatalf("WriteFile(%s) error = %v", entry.component, err)
		}
	}

	bundle := &hydrate.Bundle{
		APIVersion: "distribution.sealos.io/v1alpha1",
		Kind:       "HydratedBundle",
		Spec: hydrate.BundleSpec{
			BOMName:  "minimal-single-node",
			Revision: "rev-poc-001",
			Channel:  bom.ChannelAlpha,
			TrackedHostPaths: []hydrate.TrackedHostPath{
				{
					HostPath:   "/README",
					BundlePath: "components/containerd/files/rootfs/README",
					Component:  "containerd",
					Source:     hydrate.InventorySourcePackageManifest,
					Ownership:  hydrate.InventoryOwnershipGlobal,
					Type:       hydrate.HostPathRegularFile,
				},
				{
					HostPath:   "/README",
					BundlePath: "components/kubernetes/files/rootfs/README",
					Component:  "kubernetes",
					Source:     hydrate.InventorySourcePackageManifest,
					Ownership:  hydrate.InventoryOwnershipGlobal,
					Type:       hydrate.HostPathRegularFile,
				},
			},
			Components: []hydrate.RenderedComponent{
				{Name: "containerd"},
				{Name: "kubernetes"},
			},
		},
	}
	if err := yamlutil.MarshalFile(filepath.Join(bundleDir, hydrate.BundleFileName), bundle); err != nil {
		t.Fatalf("MarshalFile(bundle) error = %v", err)
	}
	desiredStateDigest, err := hydrate.DigestBundle(bundleDir)
	if err != nil {
		t.Fatalf("DigestBundle() error = %v", err)
	}
	if _, err := state.PersistSuccessfulApply(clusterName, state.BOMReference{
		Name:     bundle.Spec.BOMName,
		Revision: bundle.Spec.Revision,
		Channel:  bundle.Spec.Channel,
	}, desiredStateDigest.String(), "local-rev-1", "patch-rev-1"); err != nil {
		t.Fatalf("PersistSuccessfulApply() error = %v", err)
	}

	cmd := newSyncCmd()
	cmd.SetArgs([]string{
		"revert",
		"--cluster", clusterName,
		"--bundle-dir", bundleDir,
		"--kubeconfig", "/tmp/test-kubeconfig",
		"--host-root", localHostRoot,
		"--host-path", "/README",
		"--host", "10.0.0.11:22",
	})
	err = cmd.Execute()
	if err == nil {
		t.Fatal("Execute() error = nil, want ambiguity error")
	}
	if got := err.Error(); !strings.Contains(got, "specify --host or --component from [containerd, kubernetes]") {
		t.Fatalf("Execute() error = %q, want component guidance", got)
	}
}

func TestSyncRevertCmdWithRemoteHostPathAndComponentSelector(t *testing.T) {
	previousRuntimeRoot := constants.DefaultRuntimeRootDir
	runtimeRoot := previousRuntimeRoot
	if runtimeRoot == "" {
		runtimeRoot = constants.GetRuntimeRootDir(constants.AppName)
	}
	constants.DefaultRuntimeRootDir = runtimeRoot
	t.Cleanup(func() {
		constants.DefaultRuntimeRootDir = previousRuntimeRoot
	})

	previousOutput := outputSyncKubectl
	outputSyncKubectl = func(args ...string) ([]byte, error) {
		t.Fatalf("unexpected kubectl invocation: %s", strings.Join(args, " "))
		return nil, nil
	}
	t.Cleanup(func() {
		outputSyncKubectl = previousOutput
	})

	localHostRoot := t.TempDir()
	remoteHostRoot := t.TempDir()
	if err := writeSyncRemoteHostFixture(localHostRoot, "/README", "local-readme", 0o644); err != nil {
		t.Fatalf("writeSyncRemoteHostFixture(local) error = %v", err)
	}
	if err := writeSyncRemoteHostFixture(remoteHostRoot, "/README", "drifted-readme", 0o644); err != nil {
		t.Fatalf("writeSyncRemoteHostFixture(remote) error = %v", err)
	}

	previousTopologyLoader := loadSyncExecutionTopology
	previousRemoteExecutor := newSyncRemoteExecutor
	fakeRemote := &fakeSyncRemoteExecutor{
		roots: map[string]string{
			"10.0.0.11:22": remoteHostRoot,
		},
	}
	loadSyncExecutionTopology = func(string) (*syncExecutionTopology, error) {
		return &syncExecutionTopology{
			allNodes:    []string{syncLocalExecutionHost, "10.0.0.11:22"},
			firstMaster: syncLocalExecutionHost,
		}, nil
	}
	newSyncRemoteExecutor = func(*syncExecutionTopology) (syncRemoteExecutor, error) {
		return fakeRemote, nil
	}
	t.Cleanup(func() {
		loadSyncExecutionTopology = previousTopologyLoader
		newSyncRemoteExecutor = previousRemoteExecutor
	})

	clusterName := uniqueSyncClusterName(t, "revert-remote-readme-component")
	t.Cleanup(func() {
		_ = os.RemoveAll(filepath.Join(runtimeRoot, clusterName))
	})
	bundleDir := t.TempDir()
	for _, entry := range []struct {
		component string
		content   string
	}{
		{component: "containerd", content: "desired-containerd"},
		{component: "kubernetes", content: "desired-kubernetes"},
	} {
		desiredPath := filepath.Join(bundleDir, "components", entry.component, "files", "rootfs", "README")
		if err := os.MkdirAll(filepath.Dir(desiredPath), 0o755); err != nil {
			t.Fatalf("MkdirAll(%s) error = %v", entry.component, err)
		}
		if err := os.WriteFile(desiredPath, []byte(entry.content), 0o644); err != nil {
			t.Fatalf("WriteFile(%s) error = %v", entry.component, err)
		}
	}

	bundle := &hydrate.Bundle{
		APIVersion: "distribution.sealos.io/v1alpha1",
		Kind:       "HydratedBundle",
		Spec: hydrate.BundleSpec{
			BOMName:  "minimal-single-node",
			Revision: "rev-poc-001",
			Channel:  bom.ChannelAlpha,
			TrackedHostPaths: []hydrate.TrackedHostPath{
				{
					HostPath:   "/README",
					BundlePath: "components/containerd/files/rootfs/README",
					Component:  "containerd",
					Source:     hydrate.InventorySourcePackageManifest,
					Ownership:  hydrate.InventoryOwnershipGlobal,
					Type:       hydrate.HostPathRegularFile,
				},
				{
					HostPath:   "/README",
					BundlePath: "components/kubernetes/files/rootfs/README",
					Component:  "kubernetes",
					Source:     hydrate.InventorySourcePackageManifest,
					Ownership:  hydrate.InventoryOwnershipGlobal,
					Type:       hydrate.HostPathRegularFile,
				},
			},
			Components: []hydrate.RenderedComponent{
				{Name: "containerd"},
				{Name: "kubernetes"},
			},
		},
	}
	if err := yamlutil.MarshalFile(filepath.Join(bundleDir, hydrate.BundleFileName), bundle); err != nil {
		t.Fatalf("MarshalFile(bundle) error = %v", err)
	}
	desiredStateDigest, err := hydrate.DigestBundle(bundleDir)
	if err != nil {
		t.Fatalf("DigestBundle() error = %v", err)
	}
	if _, err := state.PersistSuccessfulApply(clusterName, state.BOMReference{
		Name:     bundle.Spec.BOMName,
		Revision: bundle.Spec.Revision,
		Channel:  bundle.Spec.Channel,
	}, desiredStateDigest.String(), "local-rev-1", "patch-rev-1"); err != nil {
		t.Fatalf("PersistSuccessfulApply() error = %v", err)
	}

	buf := bytes.NewBuffer(nil)
	cmd := newSyncCmd()
	cmd.SetOut(buf)
	cmd.SetErr(buf)
	cmd.SetArgs([]string{
		"revert",
		"--cluster", clusterName,
		"--bundle-dir", bundleDir,
		"--kubeconfig", "/tmp/test-kubeconfig",
		"--host-root", localHostRoot,
		"--host-path", "/README",
		"--host", "10.0.0.11:22",
		"--component", "containerd",
	})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute() error = %v", err)
	}

	remoteData, err := os.ReadFile(filepath.Join(remoteHostRoot, "README"))
	if err != nil {
		t.Fatalf("ReadFile(remote README) error = %v", err)
	}
	if got, want := string(remoteData), "desired-containerd"; got != want {
		t.Fatalf("remote README content = %q, want %q", got, want)
	}

	var out struct {
		RequestedHost      string `yaml:"requestedHost"`
		RequestedComponent string `yaml:"requestedComponent"`
		RequestedHostPath  string `yaml:"requestedHostPath"`
		RevertedHostPaths  []struct {
			Host      string `yaml:"host"`
			Path      string `yaml:"path"`
			Component string `yaml:"component"`
		} `yaml:"revertedHostPaths"`
	}
	if err := yaml.Unmarshal(buf.Bytes(), &out); err != nil {
		t.Fatalf("Unmarshal() error = %v\noutput=%s", err, buf.String())
	}
	if got, want := out.RequestedComponent, "containerd"; got != want {
		t.Fatalf("requestedComponent = %q, want %q", got, want)
	}
	if got, want := len(out.RevertedHostPaths), 1; got != want {
		t.Fatalf("len(revertedHostPaths) = %d, want %d", got, want)
	}
	if got, want := out.RevertedHostPaths[0].Component, "containerd"; got != want {
		t.Fatalf("revertedHostPaths[0].component = %q, want %q", got, want)
	}
}

func TestPersistSyncObservedState(t *testing.T) {
	previousRuntimeRoot := constants.DefaultRuntimeRootDir
	runtimeRoot := previousRuntimeRoot
	if runtimeRoot == "" {
		runtimeRoot = constants.GetRuntimeRootDir(constants.AppName)
	}
	constants.DefaultRuntimeRootDir = runtimeRoot
	t.Cleanup(func() {
		constants.DefaultRuntimeRootDir = previousRuntimeRoot
	})

	clusterName := uniqueSyncClusterName(t, "persist-observed")
	t.Cleanup(func() {
		_ = os.RemoveAll(filepath.Join(runtimeRoot, clusterName))
	})
	bundleDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(bundleDir, "bundle.yaml"), []byte("apiVersion: distribution.sealos.io/v1alpha1\nkind: HydratedBundle\n"), 0o644); err != nil {
		t.Fatalf("WriteFile(bundle.yaml) error = %v", err)
	}
	desiredStateDigest, err := hydrate.DigestBundle(bundleDir)
	if err != nil {
		t.Fatalf("DigestBundle() error = %v", err)
	}
	if _, err := state.PersistSuccessfulApply(clusterName, state.BOMReference{
		Name:     "minimal-single-node",
		Revision: "rev-poc-001",
		Channel:  bom.ChannelAlpha,
	}, desiredStateDigest.String(), "local-rev-1", "patch-rev-1"); err != nil {
		t.Fatalf("PersistSuccessfulApply() error = %v", err)
	}

	persisted, updated, err := persistSyncObservedState(clusterName, bundleDir, compare.Summary{
		Total:   2,
		Present: 2,
		Drifted: 2,
		Dirty:   1,
		Orphan:  1,
	}, compare.StatusSummary{
		State: state.StateOrphan,
		OperatorActionSummary: compare.OperatorActionSummary{
			DirectCommitEligible: 1,
			DirectRevertEligible: 2,
			BundleMatchRequired:  2,
		},
		DirtyObjects: []compare.ObjectIssue{
			{Name: "grafana-settings"},
		},
		OrphanObjects: []compare.ObjectIssue{
			{Name: "cilium"},
		},
		MixedOwnershipObjects: []compare.MixedOwnershipObject{
			{Name: "grafana-settings"},
		},
	})
	if err != nil {
		t.Fatalf("persistSyncObservedState() error = %v", err)
	}
	if !updated {
		t.Fatal("persistSyncObservedState() updated = false, want true")
	}
	if persisted == nil {
		t.Fatal("persistSyncObservedState() persisted = nil, want value")
	}
	if got, want := persisted.Status.State, state.StateOrphan; got != want {
		t.Fatalf("persisted.status.state = %q, want %q", got, want)
	}
	if persisted.Status.ObservedSummary == nil {
		t.Fatal("persisted.status.observedSummary = nil, want value")
	}
	if got, want := persisted.Status.ObservedSummary.MixedOwnershipObject, 1; got != want {
		t.Fatalf("persisted.status.observedSummary.mixedOwnershipObject = %d, want %d", got, want)
	}
	if got, want := persisted.Status.ObservedSummary.DirectCommitEligible, 1; got != want {
		t.Fatalf("persisted.status.observedSummary.directCommitEligible = %d, want %d", got, want)
	}
	if got, want := persisted.Status.ObservedSummary.DirectRevertEligible, 2; got != want {
		t.Fatalf("persisted.status.observedSummary.directRevertEligible = %d, want %d", got, want)
	}
	if got, want := persisted.Status.ObservedSummary.BundleMatchRequired, 2; got != want {
		t.Fatalf("persisted.status.observedSummary.bundleMatchRequired = %d, want %d", got, want)
	}
	var observed *state.Condition
	for i := range persisted.Status.Conditions {
		if persisted.Status.Conditions[i].Type == state.ConditionTypeObserved {
			observed = &persisted.Status.Conditions[i]
			break
		}
	}
	if observed == nil {
		t.Fatal("persisted.status.conditions missing Observed condition")
	}
	if got, want := observed.Message, "detected 1 orphan object(s), 0 orphan host path(s), 1 dirty object(s), and 0 dirty host path(s); 1 direct commit-eligible, 2 direct revert-eligible, 2 bundle-match-guarded drift item(s)"; got != want {
		t.Fatalf("persisted.status.conditions[Observed].message = %q, want %q", got, want)
	}
}

func testSyncBOM() *bom.BOM {
	doc := bom.New("default-platform", "rev-20240423", bom.ChannelBeta)
	doc.Spec.Packages = []bom.Package{
		{
			Name:     "kubernetes",
			Category: "infra",
			Version:  "v1.30.3",
			Artifact: bom.ArtifactReference{
				Name:   "kubernetes-rootfs",
				Image:  "registry.example.io/sealos/kubernetes-rootfs:v1.30.3",
				Digest: "sha256:1111111111111111111111111111111111111111111111111111111111111111",
			},
		},
	}
	return doc
}

type syncHealthProofTestReportOptions struct {
	BOMFile                string
	BOMName                string
	BOMRevision            string
	BOMDigest              string
	SkipBOMIdentity        bool
	Status                 string
	ExitCode               int
	MutatingApply          bool
	RevertCheck            bool
	SourcePreflightState   string
	RuntimePreflightState  string
	PostApplyState         string
	PostRevertState        string
	DesiredStateDigest     string
	LocalRepoRevision      string
	SkipDesiredStateDigest bool
	SkipLocalRepoRevision  bool
	Stages                 []syncHealthProofAcceptanceReportStage
}

func writeSyncHealthProofTestReport(t *testing.T, path string, opts syncHealthProofTestReportOptions) {
	t.Helper()

	bomFile := opts.BOMFile
	if bomFile == "" {
		bomFile = "/work/bom.yaml"
	}
	bomName := opts.BOMName
	bomRevision := opts.BOMRevision
	if !opts.SkipBOMIdentity {
		if bomName == "" {
			bomName = testSyncBOM().Metadata.Name
		}
		if bomRevision == "" {
			bomRevision = testSyncBOM().Spec.Revision
		}
	}
	bomDigest := opts.BOMDigest
	if bomDigest == "" {
		bomDigest = "sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	}
	desiredStateDigest := opts.DesiredStateDigest
	if desiredStateDigest == "" && !opts.SkipDesiredStateDigest {
		desiredStateDigest = "sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	}
	localRepoRevision := opts.LocalRepoRevision
	if localRepoRevision == "" && !opts.SkipLocalRepoRevision {
		localRepoRevision = "sha256:bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"
	}

	report := syncHealthProofAcceptanceReport{
		APIVersion: distribution.APIVersion,
		Kind:       distribution.KindPackageAcceptanceReport,
		Metadata: bom.Metadata{
			Name: "poc-minimal",
		},
		Spec: syncHealthProofAcceptanceReportSpec{
			ClusterName:           "poc-minimal",
			StartedAt:             "2024-04-24T10:00:00Z",
			FinishedAt:            "2024-04-24T11:00:00Z",
			Status:                opts.Status,
			ExitCode:              opts.ExitCode,
			MutatingApply:         opts.MutatingApply,
			RevertCheck:           opts.RevertCheck,
			PackageMode:           "local",
			BOMFile:               bomFile,
			BOMName:               bomName,
			BOMRevision:           bomRevision,
			BOMDigest:             bomDigest,
			Workdir:               "/work",
			RuntimeRoot:           "/work/runtime",
			LocalRepo:             "/work/local-repo",
			BundleDir:             "/work/bundle",
			Kubeconfig:            "/etc/kubernetes/admin.conf",
			HostRoot:              "/",
			OutputsFormat:         "yaml",
			DesiredStateDigest:    desiredStateDigest,
			LocalRepoRevision:     localRepoRevision,
			SourcePreflightState:  opts.SourcePreflightState,
			RuntimePreflightState: opts.RuntimePreflightState,
			PostApplyState:        opts.PostApplyState,
			PostRevertState:       opts.PostRevertState,
			Stages:                opts.Stages,
			Notes: []string{
				"Secret values are not captured in this report.",
			},
		},
	}
	if err := yamlutil.MarshalFile(path, report); err != nil {
		t.Fatalf("MarshalFile(acceptance report) error = %v", err)
	}
}

func syncHealthProofTestStages(revertCheck bool) []syncHealthProofAcceptanceReportStage {
	stages := []syncHealthProofAcceptanceReportStage{
		{Name: "package-inspect-containerd", Status: "Passed"},
		{Name: "package-inspect-kubernetes", Status: "Passed"},
		{Name: "package-inspect-cilium", Status: "Passed"},
		{Name: "local-repo-init", Status: "Passed"},
		{Name: "fill-local-repo-inputs", Status: "Passed"},
		{Name: "local-repo-doctor", Status: "Passed"},
		{Name: "validate", Status: "Passed"},
		{Name: "source-preflight", Status: "Passed"},
		{Name: "render", Status: "Passed"},
		{Name: "verify-sourcePreflight-metadata", Status: "Passed"},
		{Name: "runtime-preflight", Status: "Passed"},
		{Name: "plan", Status: "Passed"},
		{Name: "apply", Status: "Passed", Mutates: true},
		{Name: "status", Status: "Passed"},
		{Name: "diff", Status: "Passed"},
		{Name: "validate-cluster", Status: "Passed", Mutates: true},
		{Name: "revert-check-drift-inject", Status: "Skipped", Mutates: true},
		{Name: "revert-check-drift-diff", Status: "Skipped"},
		{Name: "revert-check-revert", Status: "Skipped", Mutates: true},
		{Name: "revert-check-clean-diff", Status: "Skipped"},
		{Name: "validate-cluster-after-revert", Status: "Skipped", Mutates: true},
	}
	if !revertCheck {
		return stages
	}
	for i := range stages {
		switch stages[i].Name {
		case "revert-check-drift-inject", "revert-check-drift-diff", "revert-check-revert", "revert-check-clean-diff", "validate-cluster-after-revert":
			stages[i].Status = "Passed"
		}
	}
	return stages
}

func syncHealthProofTestSignalPassed(proof bom.DistributionHealthProof, name string) bool {
	for _, signal := range proof.Spec.Signals {
		if signal.Name == name {
			return signal.Passed
		}
	}
	return false
}

func syncFixtureRoot() string {
	return filepath.Join("..", "..", "..", "pkg", "distribution", "packageformat", "testdata", "kubernetes-rootfs")
}

func syncLifecycleBOM() *bom.BOM {
	doc := bom.New("lifecycle-acceptance", "rev-1", bom.ChannelAlpha)
	doc.Spec.Packages = []bom.Package{{
		Name:     "runtime",
		Category: "infra",
		Version:  "v0.1.0",
		Artifact: bom.ArtifactReference{
			Name:   "runtime-rootfs",
			Image:  "registry.example.io/sealos/runtime-rootfs:v0.1.0",
			Digest: "sha256:2222222222222222222222222222222222222222222222222222222222222222",
		},
	}}
	return doc
}

func writeSyncTestPackage(t *testing.T, root string) {
	t.Helper()

	writeSyncTestFile(t, filepath.Join(root, "package.yaml"), `apiVersion: distribution.sealos.io/v1alpha1
kind: ComponentPackage
metadata:
  name: runtime-rootfs
spec:
  component: runtime
  version: v0.1.0
  class: rootfs
  inputs:
    - name: runtime-config
      type: configFile
      path: files/etc/demo/config.yaml
      format: yaml
  contents:
    - name: runtime-rootfs
      type: rootfs
      path: rootfs/
      required: true
    - name: runtime-config
      type: file
      path: files/etc/demo/config.yaml
      required: true
  hooks:
    - name: bootstrap
      phase: bootstrap
      target: allNodes
      path: hooks/bootstrap.sh
      timeoutSeconds: 5
`, 0o644)
	writeSyncTestFile(t, filepath.Join(root, "rootfs", "README"), "runtime rootfs\n", 0o644)
	writeSyncTestFile(t, filepath.Join(root, "files", "etc", "demo", "config.yaml"), "clusterName: package-default\nfeatureGate: disabled\n", 0o644)
	writeSyncTestExecutable(t, filepath.Join(root, "hooks", "bootstrap.sh"), "#!/bin/sh\nexit 0\n")
}

func writeSyncRequiredInputsTestPackage(t *testing.T, root string) {
	t.Helper()

	writeSyncTestPackage(t, root)
	writeSyncTestFile(t, filepath.Join(root, "package.yaml"), `apiVersion: distribution.sealos.io/v1alpha1
kind: ComponentPackage
metadata:
  name: runtime-rootfs
spec:
  component: runtime
  version: v0.1.0
  class: rootfs
  inputs:
    - name: runtime-config
      type: configFile
      path: files/etc/demo/config.yaml
      format: yaml
      required: true
    - name: admin-password
      type: configFile
      path: files/etc/demo/admin-password.txt
      required: true
  contents:
    - name: runtime-rootfs
      type: rootfs
      path: rootfs/
      required: true
    - name: runtime-config
      type: file
      path: files/etc/demo/config.yaml
      required: true
    - name: admin-password
      type: file
      path: files/etc/demo/admin-password.txt
      required: true
  hooks:
    - name: bootstrap
      phase: bootstrap
      target: allNodes
      path: hooks/bootstrap.sh
      timeoutSeconds: 5
`, 0o644)
	writeSyncTestFile(t, filepath.Join(root, "files", "etc", "demo", "admin-password.txt"), "package-default-password\n", 0o600)
}

func syncPOCBOMPath() string {
	return filepath.Join(syncPOCRoot(), "bom.yaml")
}

func syncPOCPackageRoot(name string) string {
	return filepath.Join(syncPOCRoot(), "packages", name)
}

func syncPOCRoot() string {
	return filepath.Join("..", "..", "..", "scripts", "poc", "minimal-single-node")
}

func uniqueSyncClusterName(t *testing.T, prefix string) string {
	t.Helper()
	return prefix + "-" + filepath.Base(t.TempDir())
}

func writeSyncClusterInventory(t *testing.T, clusterName string, hosts []v1beta1.Host) {
	t.Helper()
	cluster := &v1beta1.Cluster{
		Spec: v1beta1.ClusterSpec{
			Hosts: hosts,
		},
	}
	if err := os.MkdirAll(filepath.Dir(constants.Clusterfile(clusterName)), 0o755); err != nil {
		t.Fatalf("MkdirAll(clusterfile dir) error = %v", err)
	}
	if err := yamlutil.MarshalFile(constants.Clusterfile(clusterName), cluster); err != nil {
		t.Fatalf("MarshalFile(clusterfile) error = %v", err)
	}
}

func writeSyncLocalPatch(t *testing.T, root, component, filename, content string) {
	t.Helper()

	path := filepath.Join(root, localrepo.PatchesDirName, component, filename)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("MkdirAll(%q) error = %v", filepath.Dir(path), err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("WriteFile(%q) error = %v", path, err)
	}
}

func writeSyncLocalInput(t *testing.T, root, component, filename, content string) {
	t.Helper()

	path := filepath.Join(root, "inputs", component, filename)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("MkdirAll(%q) error = %v", filepath.Dir(path), err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("WriteFile(%q) error = %v", path, err)
	}
}

func writeSyncPOCLocalRepo(t *testing.T) string {
	t.Helper()

	root := t.TempDir()
	writeSyncLocalInput(t, root, "containerd", "containerd-config.toml", "version = 2\n")
	writeSyncLocalInput(t, root, "kubernetes", "kubeadm-cluster-config.yaml", "apiVersion: kubeadm.k8s.io/v1beta3\nkind: ClusterConfiguration\n")
	return root
}

func syncRenderInputChangeByName(changes []syncRenderInputChange, name string) (syncRenderInputChange, bool) {
	for _, change := range changes {
		if change.Name == name {
			return change, true
		}
	}
	return syncRenderInputChange{}, false
}

func syncPlanHostPathSetByProjection(sets []syncPlanHostPathSet, projectionClass string) *syncPlanHostPathSet {
	for i := range sets {
		if sets[i].ProjectionClass == projectionClass {
			return &sets[i]
		}
	}
	return nil
}

func writeSyncTestExecutable(t *testing.T, path, content string) {
	t.Helper()
	writeSyncTestFile(t, path, content, 0o755)
}

func writeSyncTestFile(t *testing.T, path, content string, mode os.FileMode) {
	t.Helper()

	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("MkdirAll(%q) error = %v", filepath.Dir(path), err)
	}
	if err := os.WriteFile(path, []byte(content), mode); err != nil {
		t.Fatalf("WriteFile(%q) error = %v", path, err)
	}
}

type fakeSyncImageMounter struct {
	mounts map[string]packageformat.MountedImage
}

func (m *fakeSyncImageMounter) Mount(image string) (packageformat.MountedImage, error) {
	info, ok := m.mounts[image]
	if !ok {
		return packageformat.MountedImage{}, os.ErrNotExist
	}
	return info, nil
}

func (m *fakeSyncImageMounter) Unmount(string) error {
	return nil
}

type fakeSyncRemoteExecutor struct {
	roots map[string]string
	cmds  []string
}

func (f *fakeSyncRemoteExecutor) Copy(host, src, dst string) error {
	root, err := f.rootForHost(host)
	if err != nil {
		return err
	}
	info, err := os.Lstat(src)
	if err != nil {
		return err
	}
	targetPath := filepath.Join(root, filepath.FromSlash(strings.TrimPrefix(dst, "/")))
	if err := os.MkdirAll(filepath.Dir(targetPath), 0o755); err != nil {
		return err
	}
	if info.Mode()&os.ModeSymlink != 0 {
		linkTarget, err := os.Readlink(src)
		if err != nil {
			return err
		}
		_ = os.Remove(targetPath)
		return os.Symlink(linkTarget, targetPath)
	}
	data, err := os.ReadFile(src)
	if err != nil {
		return err
	}
	return os.WriteFile(targetPath, data, info.Mode().Perm())
}

func (f *fakeSyncRemoteExecutor) Fetch(host, src, dst string) error {
	root, err := f.rootForHost(host)
	if err != nil {
		return err
	}
	sourcePath := filepath.Join(root, filepath.FromSlash(strings.TrimPrefix(src, "/")))
	info, err := os.Lstat(sourcePath)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return err
	}
	if info.Mode()&os.ModeSymlink != 0 {
		linkTarget, err := os.Readlink(sourcePath)
		if err != nil {
			return err
		}
		_ = os.Remove(dst)
		return os.Symlink(linkTarget, dst)
	}
	data, err := os.ReadFile(sourcePath)
	if err != nil {
		return err
	}
	return os.WriteFile(dst, data, info.Mode().Perm())
}

func (f *fakeSyncRemoteExecutor) CmdAsyncWithContext(_ context.Context, host string, cmds ...string) error {
	_, err := f.rootForHost(host)
	if err != nil {
		return err
	}
	f.cmds = append(f.cmds, host+":async:"+strings.Join(cmds, " && "))
	return nil
}

func (f *fakeSyncRemoteExecutor) CmdToString(host, cmd, _ string) (string, error) {
	root, err := f.rootForHost(host)
	if err != nil {
		return "", err
	}
	f.cmds = append(f.cmds, host+":string:"+cmd)
	switch {
	case strings.Contains(cmd, `readlink "$path"`):
		remotePath, ok := extractFakeSyncQuotedValue(cmd, "path=")
		if !ok {
			return "", fmt.Errorf("missing path assignment in %q", cmd)
		}
		resolvedPath := filepath.Join(root, filepath.FromSlash(strings.TrimPrefix(remotePath, "/")))
		f.cmds = append(f.cmds, "inspect:"+host+":"+remotePath+":"+resolvedPath)
		info, err := os.Lstat(resolvedPath)
		if err != nil {
			if os.IsNotExist(err) {
				return "missing\n", nil
			}
			return "", err
		}
		switch {
		case info.Mode()&os.ModeSymlink != 0:
			target, err := os.Readlink(resolvedPath)
			if err != nil {
				return "", err
			}
			return "symlink\n" + target, nil
		case info.Mode().IsRegular():
			return "file\n", nil
		case info.IsDir():
			return "dir\n", nil
		default:
			return "other\n", nil
		}
	case strings.Contains(cmd, `elif [ -L "$dst" ]`):
		remotePath, ok := extractFakeSyncQuotedValue(cmd, "dst=")
		if !ok {
			return "", fmt.Errorf("missing dst assignment in %q", cmd)
		}
		resolvedPath := filepath.Join(root, filepath.FromSlash(strings.TrimPrefix(remotePath, "/")))
		info, err := os.Lstat(resolvedPath)
		if err == nil {
			if info.IsDir() {
				return "directory", nil
			}
			if info.Mode()&os.ModeSymlink != 0 {
				if err := os.Remove(resolvedPath); err != nil {
					return "", err
				}
			}
		} else if !os.IsNotExist(err) {
			return "", err
		}
		return "ok", nil
	case strings.Contains(cmd, `ln -s `):
		remotePath, ok := extractFakeSyncQuotedValue(cmd, "dst=")
		if !ok {
			return "", fmt.Errorf("missing dst assignment in %q", cmd)
		}
		linkTarget, ok := extractFakeSyncQuotedValue(cmd, `ln -s `)
		if !ok {
			return "", fmt.Errorf("missing symlink target in %q", cmd)
		}
		resolvedPath := filepath.Join(root, filepath.FromSlash(strings.TrimPrefix(remotePath, "/")))
		if err := os.MkdirAll(filepath.Dir(resolvedPath), 0o755); err != nil {
			return "", err
		}
		info, err := os.Lstat(resolvedPath)
		if err == nil {
			if info.IsDir() {
				return "directory", nil
			}
			if err := os.Remove(resolvedPath); err != nil {
				return "", err
			}
		} else if !os.IsNotExist(err) {
			return "", err
		}
		if err := os.Symlink(linkTarget, resolvedPath); err != nil {
			return "", err
		}
		return "ok", nil
	default:
		return "", fmt.Errorf("unsupported remote command %q", cmd)
	}
}

func (f *fakeSyncRemoteExecutor) rootForHost(host string) (string, error) {
	root, ok := f.roots[host]
	if !ok {
		return "", fmt.Errorf("no fake remote root configured for host %q", host)
	}
	return root, nil
}

func writeSyncRemoteHostFixture(root, trackedPath, content string, mode os.FileMode) error {
	path := filepath.Join(root, filepath.FromSlash(strings.TrimPrefix(trackedPath, "/")))
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	return os.WriteFile(path, []byte(content), mode)
}

func extractFakeSyncQuotedValue(command, prefix string) (string, bool) {
	start := strings.Index(command, prefix)
	if start < 0 {
		return "", false
	}
	rest := command[start+len(prefix):]
	end := strings.IndexAny(rest, " \n\t")
	if end >= 0 {
		rest = rest[:end]
	}
	return normalizeFakeSyncQuotedToken(rest)
}

func normalizeFakeSyncQuotedToken(token string) (string, bool) {
	const escapedSingleQuote = "'\"'\"'"
	switch {
	case strings.HasPrefix(token, escapedSingleQuote) && strings.HasSuffix(token, escapedSingleQuote):
		return token[len(escapedSingleQuote) : len(token)-len(escapedSingleQuote)], true
	case strings.HasPrefix(token, `'`) && strings.HasSuffix(token, `'`):
		return token[1 : len(token)-1], true
	default:
		return "", false
	}
}
