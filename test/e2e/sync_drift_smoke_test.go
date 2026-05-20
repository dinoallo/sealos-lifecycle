package e2e

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/labring/sealos/pkg/distribution/bom"
	"github.com/labring/sealos/pkg/distribution/compare"
	"github.com/labring/sealos/pkg/distribution/hydrate"
	"github.com/labring/sealos/pkg/distribution/localrepo"
	"github.com/labring/sealos/pkg/distribution/reconcile"
	"github.com/labring/sealos/pkg/distribution/state"
	yamlutil "github.com/labring/sealos/pkg/utils/yaml"
	"github.com/labring/sealos/test/e2e/testhelper/settings"

	. "github.com/onsi/ginkgo/v2"
	ginkgotypes "github.com/onsi/ginkgo/v2/types"
	. "github.com/onsi/gomega"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/yaml"
)

const (
	syncDriftSmokeEnv            = "SEALOS_E2E_SYNC_SMOKE"
	syncDriftSmokeKeepTmpEnv     = "SEALOS_E2E_SYNC_SMOKE_KEEP_TMP"
	syncDriftSmokeArtifactDirEnv = "SEALOS_E2E_SYNC_SMOKE_ARTIFACT_DIR"
	syncDriftSmokeGoVersionEnv   = "SYNC_DRIFT_SMOKE_INPUT_GO_VERSION"
	syncDriftSmokeTimeoutEnv     = "SYNC_DRIFT_SMOKE_INPUT_TIMEOUT_MINUTES"
	syncDriftSmokeUploadEnv      = "SYNC_DRIFT_SMOKE_INPUT_UPLOAD_DEBUG_ARTIFACTS"
)

var _ = Describe("E2E_sealos_sync_drift_smoke_test", func() {
	var ctx *syncDriftSmokeContext

	ReportAfterEach(func(report SpecReport) {
		if ctx == nil {
			return
		}
		if report.Failed() {
			summary, err := ctx.writeFailureSummary(report)
			if err != nil {
				fmt.Fprintf(GinkgoWriter, "failed to write sync drift smoke failure summary: %v\n", err)
			} else {
				ctx.printFailureSummary(summary)
			}
		}
		ctx.cleanup()
		ctx = nil
	})

	It("runs commit and revert loops against the docs drift fixture", func() {
		if os.Getenv(syncDriftSmokeEnv) != "true" {
			Skip("set SEALOS_E2E_SYNC_SMOKE=true to enable the sync drift smoke test")
		}

		binPath := settings.E2EConfig.SealosBinPath
		Expect(binPath).NotTo(BeEmpty())

		ctx = newSyncDriftSmokeContext(binPath)

		By("preparing the bundle, applied revision, fake kubectl, and baseline live state")
		Expect(ctx.prepare()).To(Succeed())
		Expect(ctx.metadataPath).To(BeAnExistingFile())
		metadata := ctx.readMetadata()
		Expect(metadata.ClusterName).To(Equal(ctx.clusterName))
		Expect(metadata.BundleDir).To(Equal(ctx.bundleDir))
		Expect(metadata.LocalRepoDir).To(Equal(ctx.localRepoDir))
		Expect(metadata.CommandLogPath).To(Equal(ctx.commandLogPath))
		Expect(metadata.CommandsPath).To(Equal(ctx.commandsPath))
		Expect(metadata.FailureSummaryPath).To(Equal(ctx.failureSummaryPath))
		Expect(metadata.Inputs.SmokeEnabled).To(BeTrue())
		Expect(metadata.Inputs.SealosBinPath).To(Equal(ctx.binPath))

		By("committing supported local drift back into the local repo")
		ctx.mutateLocalDrift()

		diffOutput := ctx.run("sync", "diff",
			"--cluster", ctx.clusterName,
			"--bundle-dir", ctx.bundleDir,
			"--kubeconfig", ctx.kubeconfigPath,
			"--host-root", ctx.hostRoot,
		)
		Expect(diffOutput).To(ContainSubstring("currentState: Dirty"))
		Expect(diffOutput).To(ContainSubstring("changeOwner: localOverlay"))
		Expect(diffOutput).To(ContainSubstring("changeOwner: localInput"))

		statusOutput := ctx.run("sync", "status",
			"--cluster", ctx.clusterName,
			"--bundle-dir", ctx.bundleDir,
			"--kubeconfig", ctx.kubeconfigPath,
			"--host-root", ctx.hostRoot,
		)
		Expect(statusOutput).To(ContainSubstring("currentState: Dirty"))
		Expect(statusOutput).To(ContainSubstring("dirtyObjects:"))
		Expect(statusOutput).To(ContainSubstring("dirtyHostPaths:"))

		commitOutput := ctx.run("sync", "commit",
			"--cluster", ctx.clusterName,
			"--bundle-dir", ctx.bundleDir,
			"--local-repo", ctx.localRepoDir,
			"--kubeconfig", ctx.kubeconfigPath,
			"--host-root", ctx.hostRoot,
		)
		Expect(commitOutput).To(ContainSubstring("currentState: Clean"))
		Expect(commitOutput).To(ContainSubstring("committedObjects:"))
		Expect(commitOutput).To(ContainSubstring("committedHostPaths:"))

		patchData, err := os.ReadFile(filepath.Join(ctx.localRepoDir, localrepo.PatchesDirName, "grafana", "grafana-settings.patch.yaml"))
		Expect(err).NotTo(HaveOccurred())
		Expect(string(patchData)).To(ContainSubstring("adminUser: ops"))

		inputData, err := os.ReadFile(filepath.Join(ctx.localRepoDir, localrepo.InputsDirName, "kubernetes", "kubeadm-cluster-config.yaml"))
		Expect(err).NotTo(HaveOccurred())
		Expect(string(inputData)).To(ContainSubstring("clusterName: ops-demo"))
		Expect(string(inputData)).To(ContainSubstring("serviceSubnet: 10.120.0.0/12"))

		cleanDiff := ctx.run("sync", "diff",
			"--cluster", ctx.clusterName,
			"--bundle-dir", ctx.bundleDir,
			"--kubeconfig", ctx.kubeconfigPath,
			"--host-root", ctx.hostRoot,
		)
		Expect(cleanDiff).To(ContainSubstring("currentState: Clean"))

		By("reverting global drift back to the recorded desired state")
		ctx.mutateGlobalDrift()

		orphanDiff := ctx.run("sync", "diff",
			"--cluster", ctx.clusterName,
			"--bundle-dir", ctx.bundleDir,
			"--kubeconfig", ctx.kubeconfigPath,
			"--host-root", ctx.hostRoot,
		)
		Expect(orphanDiff).To(ContainSubstring("currentState: Orphan"))
		Expect(orphanDiff).To(ContainSubstring("changeOwner: globalBaseline"))

		revertObjectOutput := ctx.run("sync", "revert",
			"--cluster", ctx.clusterName,
			"--bundle-dir", ctx.bundleDir,
			"--kubeconfig", ctx.kubeconfigPath,
			"--host-root", ctx.hostRoot,
			"--kind", "DaemonSet",
			"--namespace", "kube-system",
			"--name", "cilium",
		)
		Expect(revertObjectOutput).To(ContainSubstring("reverted: true"))
		Expect(revertObjectOutput).To(ContainSubstring("name: cilium"))

		revertHostOutput := ctx.run("sync", "revert",
			"--cluster", ctx.clusterName,
			"--bundle-dir", ctx.bundleDir,
			"--kubeconfig", ctx.kubeconfigPath,
			"--host-root", ctx.hostRoot,
			"--host-path", "/usr/bin/kubelet",
		)
		Expect(revertHostOutput).To(ContainSubstring("currentState: Clean"))
		Expect(revertHostOutput).To(ContainSubstring("path: /usr/bin/kubelet"))

		ciliumData, err := os.ReadFile(ctx.objectStatePath("DaemonSet", "kube-system", "cilium"))
		Expect(err).NotTo(HaveOccurred())
		Expect(string(ciliumData)).To(ContainSubstring(`"image":"quay.io/cilium/cilium:v1.15.0"`))

		kubeletData, err := os.ReadFile(filepath.Join(ctx.hostRoot, "usr", "bin", "kubelet"))
		Expect(err).NotTo(HaveOccurred())
		Expect(strings.TrimSpace(string(kubeletData))).To(Equal("desired-kubelet"))

		finalStatus := ctx.run("sync", "status",
			"--cluster", ctx.clusterName,
			"--bundle-dir", ctx.bundleDir,
			"--kubeconfig", ctx.kubeconfigPath,
			"--host-root", ctx.hostRoot,
		)
		Expect(finalStatus).To(ContainSubstring("currentState: Clean"))
		commands := ctx.readCommands()
		Expect(commands).To(HaveLen(8))
		Expect(commands[0].Args).To(ContainElement("diff"))
		Expect(commands[len(commands)-1].Args).To(ContainElement("status"))
	})
})

type syncDriftSmokeContext struct {
	clusterName        string
	tmpDir             string
	keepTmp            bool
	binPath            string
	runtimeRoot        string
	fixtureRoot        string
	bundleDir          string
	localRepoDir       string
	hostRoot           string
	kubeconfigPath     string
	kubectlPath        string
	kubectlState       string
	commandLogPath     string
	commandsPath       string
	failureSummaryPath string
	metadataPath       string
}

type syncDriftSmokeMetadata struct {
	ClusterName        string               `yaml:"clusterName"`
	GeneratedAt        time.Time            `yaml:"generatedAt"`
	Inputs             syncDriftSmokeInputs `yaml:"inputs"`
	KeepTmp            bool                 `yaml:"keepTmp"`
	SealosBinary       string               `yaml:"sealosBinary"`
	FixtureRoot        string               `yaml:"fixtureRoot"`
	TmpDir             string               `yaml:"tmpDir"`
	BundleDir          string               `yaml:"bundleDir"`
	LocalRepoDir       string               `yaml:"localRepoDir"`
	RuntimeRoot        string               `yaml:"runtimeRoot"`
	RuntimeClusterDir  string               `yaml:"runtimeClusterDir"`
	HostRoot           string               `yaml:"hostRoot"`
	KubeconfigPath     string               `yaml:"kubeconfigPath"`
	KubectlPath        string               `yaml:"kubectlPath"`
	KubectlStateDir    string               `yaml:"kubectlStateDir"`
	CommandLogPath     string               `yaml:"commandLogPath"`
	CommandsPath       string               `yaml:"commandsPath"`
	FailureSummaryPath string               `yaml:"failureSummaryPath"`
	CI                 string               `yaml:"ci"`
	GitHubRepository   string               `yaml:"githubRepository"`
	GitHubWorkflow     string               `yaml:"githubWorkflow"`
	GitHubJob          string               `yaml:"githubJob"`
	GitHubRunID        string               `yaml:"githubRunId"`
	GitHubRunAttempt   string               `yaml:"githubRunAttempt"`
	GitHubActor        string               `yaml:"githubActor"`
	GitHubRef          string               `yaml:"githubRef"`
	GitHubSHA          string               `yaml:"githubSha"`
}

type syncDriftSmokeInputs struct {
	SmokeEnabled                 bool   `yaml:"smokeEnabled"`
	KeepTmp                      bool   `yaml:"keepTmp"`
	ArtifactDir                  string `yaml:"artifactDir,omitempty"`
	SealosBinPath                string `yaml:"sealosBinPath"`
	WorkflowGoVersion            string `yaml:"workflowGoVersion,omitempty"`
	WorkflowTimeoutMinutes       string `yaml:"workflowTimeoutMinutes,omitempty"`
	WorkflowUploadDebugArtifacts string `yaml:"workflowUploadDebugArtifacts,omitempty"`
}

type syncDriftSmokeCommand struct {
	StartedAt time.Time `yaml:"startedAt"`
	Command   string    `yaml:"command"`
	Args      []string  `yaml:"args"`
	ExitCode  int       `yaml:"exitCode"`
	Success   bool      `yaml:"success"`
	Output    string    `yaml:"output,omitempty"`
}

type syncDriftSmokeFailureSummary struct {
	GeneratedAt        time.Time              `yaml:"generatedAt"`
	ClusterName        string                 `yaml:"clusterName"`
	Spec               string                 `yaml:"spec"`
	State              string                 `yaml:"state"`
	FailureMessage     string                 `yaml:"failureMessage,omitempty"`
	FailureLocation    string                 `yaml:"failureLocation,omitempty"`
	FailureNode        string                 `yaml:"failureNode,omitempty"`
	MetadataPath       string                 `yaml:"metadataPath"`
	CommandLogPath     string                 `yaml:"commandLogPath"`
	CommandsPath       string                 `yaml:"commandsPath"`
	FailureSummaryPath string                 `yaml:"failureSummaryPath"`
	BundleDir          string                 `yaml:"bundleDir"`
	LocalRepoDir       string                 `yaml:"localRepoDir"`
	HostRoot           string                 `yaml:"hostRoot"`
	RuntimeRoot        string                 `yaml:"runtimeRoot"`
	CommandCount       int                    `yaml:"commandCount"`
	CommandReadError   string                 `yaml:"commandReadError,omitempty"`
	LastCommand        *syncDriftSmokeCommand `yaml:"lastCommand,omitempty"`
	LastFailedCommand  *syncDriftSmokeCommand `yaml:"lastFailedCommand,omitempty"`
}

func newSyncDriftSmokeContext(binPath string) *syncDriftSmokeContext {
	artifactDir := strings.TrimSpace(os.Getenv(syncDriftSmokeArtifactDirEnv))
	keepTmp := os.Getenv(syncDriftSmokeKeepTmpEnv) == "true"
	tmpRoot := ""
	if artifactDir != "" {
		Expect(os.MkdirAll(artifactDir, 0o755)).To(Succeed())
		tmpRoot = artifactDir
		keepTmp = true
	}

	tmpDir, err := os.MkdirTemp(tmpRoot, "sealos-sync-smoke-")
	Expect(err).NotTo(HaveOccurred())

	root, err := filepath.Abs(filepath.Join("..", "..", "docs", "examples", "sync-drift-minimal"))
	Expect(err).NotTo(HaveOccurred())

	return &syncDriftSmokeContext{
		clusterName:        fmt.Sprintf("sync-smoke-%d", time.Now().UnixNano()),
		tmpDir:             tmpDir,
		keepTmp:            keepTmp,
		binPath:            binPath,
		runtimeRoot:        filepath.Join(tmpDir, "runtime-root"),
		fixtureRoot:        root,
		bundleDir:          filepath.Join(tmpDir, "fixture", "bundle"),
		localRepoDir:       filepath.Join(tmpDir, "fixture", "local-repo"),
		hostRoot:           filepath.Join(tmpDir, "host-root"),
		kubeconfigPath:     filepath.Join(tmpDir, "admin.conf"),
		kubectlPath:        filepath.Join(tmpDir, "bin", "kubectl"),
		kubectlState:       filepath.Join(tmpDir, "kubectl-state"),
		commandLogPath:     filepath.Join(tmpDir, "commands.log"),
		commandsPath:       filepath.Join(tmpDir, "commands.yaml"),
		failureSummaryPath: filepath.Join(tmpDir, "failure-summary.yaml"),
		metadataPath:       filepath.Join(tmpDir, "metadata.yaml"),
	}
}

func (c *syncDriftSmokeContext) cleanup() {
	if c.keepTmp {
		return
	}
	_ = os.RemoveAll(c.tmpDir)
}

func (c *syncDriftSmokeContext) prepare() error {
	if err := copyTree(filepath.Join(c.fixtureRoot, "bundle"), c.bundleDir); err != nil {
		return err
	}
	if err := copyTree(filepath.Join(c.fixtureRoot, "local-repo"), c.localRepoDir); err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(c.kubeconfigPath), 0o755); err != nil {
		return err
	}
	if err := os.WriteFile(c.kubeconfigPath, []byte("apiVersion: v1\nkind: Config\n"), 0o644); err != nil {
		return err
	}
	if err := os.MkdirAll(c.runtimeClusterDir(), 0o755); err != nil {
		return err
	}
	if err := os.MkdirAll(c.kubectlState, 0o755); err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(c.kubectlPath), 0o755); err != nil {
		return err
	}

	if err := c.buildFakeKubectl(); err != nil {
		return err
	}
	if err := c.rewriteBundleInputBinding(); err != nil {
		return err
	}
	if err := c.writeAppliedRevision(); err != nil {
		return err
	}
	if err := c.writeCommands(nil); err != nil {
		return err
	}
	if err := c.writeMetadata(); err != nil {
		return err
	}
	return c.seedBaselineState()
}

func (c *syncDriftSmokeContext) writeMetadata() error {
	doc := syncDriftSmokeMetadata{
		ClusterName: c.clusterName,
		GeneratedAt: time.Now().UTC(),
		Inputs: syncDriftSmokeInputs{
			SmokeEnabled:                 os.Getenv(syncDriftSmokeEnv) == "true",
			KeepTmp:                      c.keepTmp,
			ArtifactDir:                  strings.TrimSpace(os.Getenv(syncDriftSmokeArtifactDirEnv)),
			SealosBinPath:                os.Getenv("SEALOS_E2E_TEST_SEALOS_BIN_PATH"),
			WorkflowGoVersion:            os.Getenv(syncDriftSmokeGoVersionEnv),
			WorkflowTimeoutMinutes:       os.Getenv(syncDriftSmokeTimeoutEnv),
			WorkflowUploadDebugArtifacts: os.Getenv(syncDriftSmokeUploadEnv),
		},
		KeepTmp:            c.keepTmp,
		SealosBinary:       c.binPath,
		FixtureRoot:        c.fixtureRoot,
		TmpDir:             c.tmpDir,
		BundleDir:          c.bundleDir,
		LocalRepoDir:       c.localRepoDir,
		RuntimeRoot:        c.runtimeRoot,
		RuntimeClusterDir:  c.runtimeClusterDir(),
		HostRoot:           c.hostRoot,
		KubeconfigPath:     c.kubeconfigPath,
		KubectlPath:        c.kubectlPath,
		KubectlStateDir:    c.kubectlState,
		CommandLogPath:     c.commandLogPath,
		CommandsPath:       c.commandsPath,
		FailureSummaryPath: c.failureSummaryPath,
		CI:                 os.Getenv("CI"),
		GitHubRepository:   os.Getenv("GITHUB_REPOSITORY"),
		GitHubWorkflow:     os.Getenv("GITHUB_WORKFLOW"),
		GitHubJob:          os.Getenv("GITHUB_JOB"),
		GitHubRunID:        os.Getenv("GITHUB_RUN_ID"),
		GitHubRunAttempt:   os.Getenv("GITHUB_RUN_ATTEMPT"),
		GitHubActor:        os.Getenv("GITHUB_ACTOR"),
		GitHubRef:          os.Getenv("GITHUB_REF"),
		GitHubSHA:          os.Getenv("GITHUB_SHA"),
	}
	return yamlutil.MarshalFile(c.metadataPath, &doc)
}

func (c *syncDriftSmokeContext) readMetadata() syncDriftSmokeMetadata {
	var doc syncDriftSmokeMetadata
	Expect(yamlutil.UnmarshalFile(c.metadataPath, &doc)).To(Succeed())
	return doc
}

func (c *syncDriftSmokeContext) readCommands() []syncDriftSmokeCommand {
	commands, err := c.loadCommands()
	Expect(err).NotTo(HaveOccurred())
	return commands
}

func (c *syncDriftSmokeContext) writeCommands(commands []syncDriftSmokeCommand) error {
	data, err := yaml.Marshal(commands)
	if err != nil {
		return err
	}
	return os.WriteFile(c.commandsPath, data, 0o644)
}

func (c *syncDriftSmokeContext) loadCommands() ([]syncDriftSmokeCommand, error) {
	data, err := os.ReadFile(c.commandsPath)
	if err != nil {
		return nil, err
	}
	var commands []syncDriftSmokeCommand
	if err := yaml.Unmarshal(data, &commands); err != nil {
		return nil, err
	}
	return commands, nil
}

func (c *syncDriftSmokeContext) writeFailureSummary(report SpecReport) (syncDriftSmokeFailureSummary, error) {
	summary := syncDriftSmokeFailureSummary{
		GeneratedAt:        time.Now().UTC(),
		ClusterName:        c.clusterName,
		Spec:               report.FullText(),
		State:              report.State.String(),
		FailureMessage:     report.FailureMessage(),
		FailureLocation:    formatSmokeCodeLocation(report.FailureLocation()),
		FailureNode:        formatSmokeFailureNode(report),
		MetadataPath:       c.metadataPath,
		CommandLogPath:     c.commandLogPath,
		CommandsPath:       c.commandsPath,
		FailureSummaryPath: c.failureSummaryPath,
		BundleDir:          c.bundleDir,
		LocalRepoDir:       c.localRepoDir,
		HostRoot:           c.hostRoot,
		RuntimeRoot:        c.runtimeRoot,
	}

	commands, err := c.loadCommands()
	if err != nil {
		summary.CommandReadError = err.Error()
	} else {
		summary.CommandCount = len(commands)
		if len(commands) > 0 {
			lastCommand := commands[len(commands)-1]
			summary.LastCommand = &lastCommand
		}
		for i := len(commands) - 1; i >= 0; i-- {
			if commands[i].Success {
				continue
			}
			lastFailedCommand := commands[i]
			summary.LastFailedCommand = &lastFailedCommand
			break
		}
	}

	data, err := yaml.Marshal(summary)
	if err != nil {
		return summary, err
	}
	if err := os.WriteFile(c.failureSummaryPath, data, 0o644); err != nil {
		return summary, err
	}
	return summary, nil
}

func (c *syncDriftSmokeContext) printFailureSummary(summary syncDriftSmokeFailureSummary) {
	data, err := yaml.Marshal(summary)
	if err != nil {
		fmt.Fprintf(GinkgoWriter, "sync drift smoke failed; summary path: %s\n", c.failureSummaryPath)
		return
	}
	fmt.Fprintf(GinkgoWriter, "\n=== sync drift smoke failure summary ===\n%s=== end sync drift smoke failure summary ===\n", string(data))
}

func (c *syncDriftSmokeContext) buildFakeKubectl() error {
	cmd := exec.Command("go", "build", "-o", c.kubectlPath, "./testhelper/fakekubectl")
	cmd.Dir = "."
	cmd.Env = os.Environ()
	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("build fake kubectl: %w\n%s", err, string(output))
	}
	return nil
}

func (c *syncDriftSmokeContext) rewriteBundleInputBinding() error {
	bundlePath := filepath.Join(c.bundleDir, hydrate.BundleFileName)
	var bundle hydrate.Bundle
	if err := yamlutil.UnmarshalFile(bundlePath, &bundle); err != nil {
		return err
	}
	actualInputPath := filepath.Join(c.localRepoDir, localrepo.InputsDirName, "kubernetes", "kubeadm-cluster-config.yaml")
	for i := range bundle.Spec.Components {
		if bundle.Spec.Components[i].Name != "kubernetes" {
			continue
		}
		if bundle.Spec.Components[i].InputBindings == nil {
			bundle.Spec.Components[i].InputBindings = make(map[string]string)
		}
		bundle.Spec.Components[i].InputBindings["kubeadm-cluster-config"] = actualInputPath
	}
	return yamlutil.MarshalFile(bundlePath, &bundle)
}

func (c *syncDriftSmokeContext) writeAppliedRevision() error {
	bundleDigest, err := hydrate.DigestBundle(c.bundleDir)
	if err != nil {
		return err
	}

	doc := state.NewAppliedRevision(c.clusterName+"-current", c.clusterName, state.BOMReference{
		Name:     "minimal-single-node",
		Revision: "rev-poc-001",
		Channel:  bom.ChannelAlpha,
		Digest:   "sha256:1111111111111111111111111111111111111111111111111111111111111111",
	}, bundleDigest.String())
	doc.Spec.LocalRepoRevision = "sha256:2222222222222222222222222222222222222222222222222222222222222222"
	doc.Spec.LocalPatchRevision = "patch-rev-1"

	now := time.Now().UTC()
	lastApplied := metav1Time(now)
	doc.Status.State = state.StateClean
	doc.Status.LastAppliedTime = &lastApplied
	doc.Status.LastSuccessfulRevision = &state.RevisionSnapshot{
		BOM:                doc.Spec.BOM,
		LocalRepoRevision:  doc.Spec.LocalRepoRevision,
		LocalPatchRevision: doc.Spec.LocalPatchRevision,
		DesiredStateDigest: doc.Spec.DesiredStateDigest,
	}
	doc.Status.Conditions = []state.Condition{
		{
			Type:               "Applied",
			Status:             "True",
			LastTransitionTime: lastApplied,
			Reason:             "ReconcileSucceeded",
			Message:            "desired revision applied",
		},
	}

	return yamlutil.MarshalFile(filepath.Join(c.runtimeClusterDir(), "distribution", "applied-revision.yaml"), doc)
}

func (c *syncDriftSmokeContext) seedBaselineState() error {
	bundle, err := reconcile.LoadBundle(c.bundleDir)
	if err != nil {
		return err
	}

	for _, group := range groupSmokeTrackedObjects(bundle.Spec.TrackedK8sObjects) {
		desiredYAML, err := compare.DesiredObjectYAML(c.bundleDir, compare.ObjectStatus{
			Tracked:   group[0],
			Fragments: group,
		})
		if err != nil {
			return err
		}
		desiredJSON, err := yaml.YAMLToJSON(desiredYAML)
		if err != nil {
			return err
		}
		if err := os.MkdirAll(filepath.Dir(c.objectStatePath(group[0].Kind, group[0].Namespace, group[0].Name)), 0o755); err != nil {
			return err
		}
		if err := os.WriteFile(c.objectStatePath(group[0].Kind, group[0].Namespace, group[0].Name), desiredJSON, 0o644); err != nil {
			return err
		}
	}

	for _, tracked := range bundle.Spec.TrackedHostPaths {
		if tracked.ProjectionClass == hydrate.HostPathProjectionClassGenerated || tracked.BundlePath == "" {
			continue
		}
		sourcePath := filepath.Join(c.bundleDir, filepath.FromSlash(tracked.BundlePath))
		data, err := os.ReadFile(sourcePath)
		if err != nil {
			return err
		}
		if err := os.MkdirAll(filepath.Dir(c.liveHostPath(tracked.HostPath)), 0o755); err != nil {
			return err
		}
		if err := os.WriteFile(c.liveHostPath(tracked.HostPath), data, 0o644); err != nil {
			return err
		}
	}
	return nil
}

func (c *syncDriftSmokeContext) mutateLocalDrift() {
	c.mutateObject("ConfigMap", "default", "grafana-settings", func(object map[string]interface{}) {
		data := ensureSmokeMap(object, "data")
		data["adminUser"] = "ops"
	})
	drifted := "apiVersion: kubeadm.k8s.io/v1beta4\nkind: ClusterConfiguration\nclusterName: ops-demo\nnetworking:\n  podSubnet: 10.244.0.0/16\n  serviceSubnet: 10.120.0.0/12\n"
	Expect(os.WriteFile(c.liveHostPath("/etc/kubernetes/kubeadm.yaml"), []byte(drifted), 0o644)).To(Succeed())
}

func (c *syncDriftSmokeContext) mutateGlobalDrift() {
	c.mutateObject("DaemonSet", "kube-system", "cilium", func(object map[string]interface{}) {
		spec := ensureSmokeMap(object, "spec")
		template := ensureSmokeMap(spec, "template")
		podSpec := ensureSmokeMap(template, "spec")
		containers, ok := podSpec["containers"].([]interface{})
		Expect(ok).To(BeTrue())
		container := containers[0].(map[string]interface{})
		container["image"] = "quay.io/cilium/cilium:v1.15.1"
	})
	Expect(os.WriteFile(c.liveHostPath("/usr/bin/kubelet"), []byte("drifted-kubelet"), 0o755)).To(Succeed())
}

func (c *syncDriftSmokeContext) run(args ...string) string {
	startedAt := time.Now().UTC()
	cmd := exec.Command(c.binPath, args...)
	cmd.Env = append(os.Environ(),
		"SEALOS_RUNTIME_ROOT="+c.runtimeRoot,
		"FAKE_KUBECTL_STATE_DIR="+c.kubectlState,
		"PATH="+filepath.Dir(c.kubectlPath)+string(os.PathListSeparator)+os.Getenv("PATH"),
	)
	output, err := cmd.CombinedOutput()
	c.appendCommandLog(startedAt, args, output, err)
	Expect(err).NotTo(HaveOccurred(), "sealos %s output:\n%s", strings.Join(args, " "), string(output))
	return string(output)
}

func (c *syncDriftSmokeContext) appendCommandLog(startedAt time.Time, args []string, output []byte, execErr error) {
	entry := fmt.Sprintf("$ %s %s\n%s\n", c.binPath, strings.Join(args, " "), string(output))
	file, err := os.OpenFile(c.commandLogPath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	Expect(err).NotTo(HaveOccurred())
	defer file.Close()
	_, err = file.WriteString(entry)
	Expect(err).NotTo(HaveOccurred())

	commands := c.readCommands()
	command := syncDriftSmokeCommand{
		StartedAt: startedAt,
		Command:   c.binPath,
		Args:      append([]string(nil), args...),
		ExitCode:  0,
		Success:   execErr == nil,
		Output:    string(output),
	}
	if execErr != nil {
		var exitErr *exec.ExitError
		if errors.As(execErr, &exitErr) {
			command.ExitCode = exitErr.ExitCode()
		} else {
			command.ExitCode = -1
		}
	}
	commands = append(commands, command)
	Expect(c.writeCommands(commands)).To(Succeed())
}

func formatSmokeCodeLocation(location ginkgotypes.CodeLocation) string {
	if location.FileName == "" {
		return ""
	}
	return fmt.Sprintf("%s:%d", location.FileName, location.LineNumber)
}

func formatSmokeFailureNode(report SpecReport) string {
	if report.Failure.FailureNodeType == 0 {
		return ""
	}
	location := formatSmokeCodeLocation(report.Failure.FailureNodeLocation)
	if location == "" {
		return report.Failure.FailureNodeType.String()
	}
	return fmt.Sprintf("%s@%s", report.Failure.FailureNodeType.String(), location)
}

func (c *syncDriftSmokeContext) objectStatePath(kind, namespace, name string) string {
	safeNamespace := namespace
	if strings.TrimSpace(safeNamespace) == "" {
		safeNamespace = "_cluster"
	}
	return filepath.Join(c.kubectlState, strings.ToLower(kind), safeNamespace, name+".json")
}

func (c *syncDriftSmokeContext) liveHostPath(tracked string) string {
	return filepath.Join(c.hostRoot, strings.TrimPrefix(filepath.Clean(tracked), string(os.PathSeparator)))
}

func (c *syncDriftSmokeContext) runtimeClusterDir() string {
	return filepath.Join(c.runtimeRoot, c.clusterName)
}

func (c *syncDriftSmokeContext) mutateObject(kind, namespace, name string, mutate func(map[string]interface{})) {
	path := c.objectStatePath(kind, namespace, name)
	data, err := os.ReadFile(path)
	Expect(err).NotTo(HaveOccurred())

	var object map[string]interface{}
	Expect(json.Unmarshal(data, &object)).To(Succeed())
	mutate(object)

	updated, err := json.Marshal(object)
	Expect(err).NotTo(HaveOccurred())
	Expect(os.WriteFile(path, updated, 0o644)).To(Succeed())
}

func groupSmokeTrackedObjects(tracked []hydrate.TrackedK8sObject) [][]hydrate.TrackedK8sObject {
	groups := make([][]hydrate.TrackedK8sObject, 0)
	index := make(map[string]int, len(tracked))
	for _, object := range tracked {
		key := strings.Join([]string{object.APIVersion, object.Kind, object.Namespace, object.Name}, "\x00")
		if position, ok := index[key]; ok {
			groups[position] = append(groups[position], object)
			continue
		}
		index[key] = len(groups)
		groups = append(groups, []hydrate.TrackedK8sObject{object})
	}
	return groups
}

func copyTree(src, dst string) error {
	info, err := os.Stat(src)
	if err != nil {
		return err
	}
	if !info.IsDir() {
		return fmt.Errorf("%q must be a directory", src)
	}
	return filepath.Walk(src, func(path string, fileInfo os.FileInfo, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		rel, err := filepath.Rel(src, path)
		if err != nil {
			return err
		}
		target := filepath.Join(dst, rel)
		if fileInfo.IsDir() {
			return os.MkdirAll(target, fileInfo.Mode())
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
			return err
		}
		return os.WriteFile(target, data, fileInfo.Mode())
	})
}

func ensureSmokeMap(object map[string]interface{}, key string) map[string]interface{} {
	if existing, ok := object[key]; ok {
		if typed, ok := existing.(map[string]interface{}); ok {
			return typed
		}
	}
	created := make(map[string]interface{})
	object[key] = created
	return created
}

func metav1Time(ts time.Time) metav1.Time {
	return metav1.NewTime(ts)
}
