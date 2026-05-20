package cmd

import (
	"bytes"
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"

	"sigs.k8s.io/yaml"

	"github.com/labring/sealos/pkg/constants"
	"github.com/labring/sealos/pkg/distribution/localrepo"
	v1beta1 "github.com/labring/sealos/pkg/types/v1beta1"
)

func TestSyncValidateCmdPassesWithLocalRepoBindings(t *testing.T) {
	tempDir := t.TempDir()
	bomPath := filepath.Join(tempDir, "bom.yaml")
	packageRoot := filepath.Join(tempDir, "package")
	localRepoRoot := filepath.Join(tempDir, "local-repo")

	writeSyncValidateBOM(t, bomPath)
	writeSyncTestPackage(t, packageRoot)
	writeSyncLocalInput(t, localRepoRoot, "runtime", "runtime-config.yaml", "clusterName: local\n")
	writeSyncTestFile(t, filepath.Join(localRepoRoot, localrepo.ResourcesDirName, "secrets", "grafana-admin-credentials.yaml"), strings.TrimSpace(`
apiVersion: v1
kind: Secret
metadata:
  name: grafana-admin-credentials
  namespace: observability
type: Opaque
stringData:
  username: admin
  password: passw0rd
`)+"\n", 0o600)
	writeSyncLocalPatch(t, localRepoRoot, "runtime", "settings.patch.yaml", strings.TrimSpace(`
apiVersion: v1
kind: ConfigMap
metadata:
  name: runtime-settings
  namespace: default
data:
  featureGate: enabled
`)+"\n")

	buf := bytes.NewBuffer(nil)
	cmd := newSyncCmd()
	cmd.SetOut(buf)
	cmd.SetErr(bytes.NewBuffer(nil))
	cmd.SetArgs([]string{
		"validate",
		"--file", bomPath,
		"--package-source", "runtime=" + packageRoot,
		"--local-repo", localRepoRoot,
		"--output", "json",
	})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute() error = %v\noutput=%s", err, buf.String())
	}

	var out syncValidateOutput
	if err := json.Unmarshal(buf.Bytes(), &out); err != nil {
		t.Fatalf("json.Unmarshal() error = %v\noutput=%s", err, buf.String())
	}
	if !out.Passed {
		t.Fatalf("out.Passed = false, want true; issues=%v", out.Issues)
	}
	if got, want := out.Summary.Components, 1; got != want {
		t.Fatalf("summary.components = %d, want %d", got, want)
	}
	if got, want := out.Summary.PackagesResolved, 1; got != want {
		t.Fatalf("summary.packagesResolved = %d, want %d", got, want)
	}
	if got, want := out.Summary.BoundInputs, 1; got != want {
		t.Fatalf("summary.boundInputs = %d, want %d", got, want)
	}
	if got, want := out.Summary.LocalResources, 1; got != want {
		t.Fatalf("summary.localResources = %d, want %d", got, want)
	}
	if got, want := out.Summary.LocalPatches, 1; got != want {
		t.Fatalf("summary.localPatches = %d, want %d", got, want)
	}
	if got, want := len(out.Packages), 1; got != want {
		t.Fatalf("len(packages) = %d, want %d", got, want)
	}
	if got, want := out.Packages[0].Component, "runtime"; got != want {
		t.Fatalf("packages[0].component = %q, want %q", got, want)
	}
}

func TestSyncValidateCmdReportsConformanceIssues(t *testing.T) {
	tempDir := t.TempDir()
	bomPath := filepath.Join(tempDir, "bom.yaml")
	packageRoot := filepath.Join(tempDir, "package")
	localRepoRoot := filepath.Join(tempDir, "local-repo")

	writeSyncValidateBOM(t, bomPath)
	writeSyncValidatePackageWithRequiredInput(t, packageRoot)
	writeSyncTestFile(t, filepath.Join(localRepoRoot, localrepo.ResourcesDirName, "secrets", "grafana-admin-credentials.yaml"), strings.TrimSpace(`
apiVersion: v1
kind: Secret
metadata:
  name: grafana-admin-credentials
  namespace: observability
type: Opaque
stringData:
  username: admin
  password: passw0rd
`)+"\n", 0o644)
	writeSyncLocalPatch(t, localRepoRoot, "runtime", "image.patch.yaml", strings.TrimSpace(`
apiVersion: apps/v1
kind: Deployment
metadata:
  name: runtime
  namespace: default
spec:
  template:
    spec:
      containers:
        - name: runtime
          image: example/runtime:v2
`)+"\n")

	buf := bytes.NewBuffer(nil)
	cmd := newSyncCmd()
	cmd.SetOut(buf)
	cmd.SetErr(bytes.NewBuffer(nil))
	cmd.SetArgs([]string{
		"validate",
		"--file", bomPath,
		"--package-source", "runtime=" + packageRoot,
		"--local-repo", localRepoRoot,
	})
	if err := cmd.Execute(); err == nil {
		t.Fatalf("Execute() error = nil, want validation failure\noutput=%s", buf.String())
	}

	var out syncValidateOutput
	if err := yaml.Unmarshal(buf.Bytes(), &out); err != nil {
		t.Fatalf("yaml.Unmarshal() error = %v\noutput=%s", err, buf.String())
	}
	if out.Passed {
		t.Fatal("out.Passed = true, want false")
	}
	for _, code := range []string{
		"hookNotExecutable",
		"localPatchPolicyViolation",
		"requiredInputMissing",
		"secretManifestModeTooOpen",
	} {
		if !syncValidateHasIssueCode(out.Issues, code) {
			t.Fatalf("issues missing code %q: %#v", code, out.Issues)
		}
	}
	if got, want := out.Summary.Errors, 4; got != want {
		t.Fatalf("summary.errors = %d, want %d; issues=%#v", got, want, out.Issues)
	}
}

func TestSyncValidateCmdChecksTopologyHostInputs(t *testing.T) {
	previousRuntimeRoot := constants.DefaultRuntimeRootDir
	runtimeRoot := t.TempDir()
	constants.DefaultRuntimeRootDir = runtimeRoot
	t.Cleanup(func() {
		constants.DefaultRuntimeRootDir = previousRuntimeRoot
	})

	tempDir := t.TempDir()
	clusterName := uniqueSyncClusterName(t, "validate-topology")
	bomPath := filepath.Join(tempDir, "bom.yaml")
	packageRoot := filepath.Join(tempDir, "package")
	localRepoRoot := filepath.Join(tempDir, "local-repo")

	writeSyncValidateBOM(t, bomPath)
	writeSyncTestPackage(t, packageRoot)
	writeSyncClusterInventory(t, clusterName, []v1beta1.Host{
		{IPS: []string{"10.0.0.11:22"}, Roles: []string{v1beta1.MASTER}},
		{IPS: []string{"10.0.0.12:22"}, Roles: []string{v1beta1.NODE}},
	})
	writeSyncLocalInput(t, localRepoRoot, "runtime", "runtime-config.yaml", "clusterName: local\n")
	writeSyncTestFile(t, filepath.Join(localRepoRoot, localrepo.InputsDirName, "runtime", "hosts", "10.0.0.13", "runtime-config.yaml"), "clusterName: stale\n", 0o644)

	buf := bytes.NewBuffer(nil)
	cmd := newSyncCmd()
	cmd.SetOut(buf)
	cmd.SetErr(bytes.NewBuffer(nil))
	cmd.SetArgs([]string{
		"validate",
		"--cluster", clusterName,
		"--runtime-root", runtimeRoot,
		"--file", bomPath,
		"--package-source", "runtime=" + packageRoot,
		"--local-repo", localRepoRoot,
		"--output", "json",
	})
	if err := cmd.Execute(); err == nil {
		t.Fatalf("Execute() error = nil, want validation failure\noutput=%s", buf.String())
	}

	var out syncValidateOutput
	if err := json.Unmarshal(buf.Bytes(), &out); err != nil {
		t.Fatalf("json.Unmarshal() error = %v\noutput=%s", err, buf.String())
	}
	if got, want := out.ClusterName, clusterName; got != want {
		t.Fatalf("clusterName = %q, want %q", got, want)
	}
	if got, want := out.Summary.Hosts, 2; got != want {
		t.Fatalf("summary.hosts = %d, want %d", got, want)
	}
	if got, want := out.ExecutionTopology.FirstMaster, "10.0.0.11:22"; got != want {
		t.Fatalf("executionTopology.firstMaster = %q, want %q", got, want)
	}
	if !syncValidateHasIssueCode(out.Issues, "hostInputUnknownHost") {
		t.Fatalf("issues missing hostInputUnknownHost: %#v", out.Issues)
	}
}

func writeSyncValidateBOM(t *testing.T, path string) {
	t.Helper()

	data, err := yaml.Marshal(syncLifecycleBOM())
	if err != nil {
		t.Fatalf("yaml.Marshal(BOM) error = %v", err)
	}
	writeSyncTestFile(t, path, string(data), 0o644)
}

func writeSyncValidatePackageWithRequiredInput(t *testing.T, root string) {
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
    - name: runtime-secret
      type: configFile
      path: files/etc/demo/secret.yaml
      required: true
  contents:
    - name: runtime-rootfs
      type: rootfs
      path: rootfs/
      required: true
  hooks:
    - name: bootstrap
      phase: bootstrap
      target: allNodes
      path: hooks/bootstrap.sh
      timeoutSeconds: 5
`, 0o644)
	writeSyncTestFile(t, filepath.Join(root, "rootfs", "README"), "runtime rootfs\n", 0o644)
	writeSyncTestFile(t, filepath.Join(root, "hooks", "bootstrap.sh"), "#!/bin/sh\nexit 0\n", 0o644)
}

func syncValidateHasIssueCode(issues []syncValidateIssue, code string) bool {
	for _, issue := range issues {
		if issue.Code == code {
			return true
		}
	}
	return false
}
