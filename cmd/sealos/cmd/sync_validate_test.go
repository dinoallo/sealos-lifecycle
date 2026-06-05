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

package cmd

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/labring/sealos/pkg/constants"
	"github.com/labring/sealos/pkg/distribution/hydrate"
	"github.com/labring/sealos/pkg/distribution/localrepo"
	"github.com/labring/sealos/pkg/distribution/packageformat"
	v1beta1 "github.com/labring/sealos/pkg/types/v1beta1"
	"sigs.k8s.io/yaml"
)

func TestSyncValidateCmdPassesWithLocalRepoBindings(t *testing.T) {
	tempDir := t.TempDir()
	bomPath := filepath.Join(tempDir, "bom.yaml")
	packageRoot := filepath.Join(tempDir, "package")
	localRepoRoot := filepath.Join(tempDir, "local-repo")

	writeSyncValidateBOM(t, bomPath)
	writeSyncTestPackage(t, packageRoot)
	writeSyncLocalInput(t, localRepoRoot, "runtime", "runtime-config.yaml", "clusterName: local\n")
	writeSyncTestFile(
		t,
		filepath.Join(
			localRepoRoot,
			localrepo.ResourcesDirName,
			"secrets",
			"grafana-admin-credentials.yaml",
		),
		strings.TrimSpace(`
apiVersion: v1
kind: Secret
metadata:
  name: grafana-admin-credentials
  namespace: observability
type: Opaque
stringData:
  username: admin
  password: passw0rd
`)+"\n",
		0o600,
	)
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
	writeSyncTestFile(
		t,
		filepath.Join(
			localRepoRoot,
			localrepo.ResourcesDirName,
			"secrets",
			"grafana-admin-credentials.yaml",
		),
		strings.TrimSpace(`
apiVersion: v1
kind: Secret
metadata:
  name: grafana-admin-credentials
  namespace: observability
type: Opaque
stringData:
  username: admin
  password: passw0rd
`)+"\n",
		0o644,
	)
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

func TestSyncValidateUsesPackageLocalPatchPolicy(t *testing.T) {
	tempDir := t.TempDir()
	bomPath := filepath.Join(tempDir, "bom.yaml")
	packageRoot := filepath.Join(tempDir, "package")
	localRepoRoot := filepath.Join(tempDir, "local-repo")

	writeSyncValidateBOM(t, bomPath)
	writeSyncTestPackage(t, packageRoot)
	writeSyncPackagePatchPolicy(t, packageRoot, strings.TrimSpace(`
apiVersion: distribution.sealos.io/v1alpha1
kind: LocalPatchPolicy
metadata:
  name: package-local-patch-policy
spec:
  scope: clusterLocal
  forbiddenExactPaths:
    - status
    - spec.selector
  forbiddenMetadataKeys:
    - uid
    - resourceVersion
    - generation
    - creationTimestamp
    - managedFields
    - ownerReferences
    - finalizers
    - generateName
    - selfLink
    - deletionTimestamp
    - deletionGracePeriodSeconds
  forbiddenContainerFields:
    - image
  kindRules:
    - kind: Deployment
      allowedPrefixes:
        - spec.template.metadata.annotations
`)+"\n")
	writeSyncLocalPatch(t, localRepoRoot, "runtime", "placement.patch.yaml", strings.TrimSpace(`
apiVersion: apps/v1
kind: Deployment
metadata:
  name: runtime
  namespace: default
spec:
  template:
    metadata:
      annotations:
        package-policy.sealos.io/managed: "true"
`)+"\n")

	out := runSyncValidate(syncValidateOptions{
		BOMPath:        bomPath,
		LocalRepoPath:  localRepoRoot,
		PackageSources: []string{"runtime=" + packageRoot},
	})
	if !out.Passed {
		t.Fatalf("validate passed = false, issues=%#v", out.Issues)
	}
	if got, want := out.LocalPolicySource, "package"; got != want {
		t.Fatalf("localPolicySource = %q, want %q", got, want)
	}
	if got, want := out.LocalPolicy, "policy/local-patch-policy.yaml"; got != want {
		t.Fatalf("localPolicy = %q, want %q", got, want)
	}
	if got, want := out.LocalPolicyName, "package-local-patch-policy"; got != want {
		t.Fatalf("localPolicyName = %q, want %q", got, want)
	}
	if got, want := out.LocalPolicyScope, "clusterLocal"; got != want {
		t.Fatalf("localPolicyScope = %q, want %q", got, want)
	}
	if !syncValidateHasSelectedPolicyCandidate(
		out.LocalPolicyCandidates,
		"package",
		"runtime",
	) {
		t.Fatalf(
			"localPolicyCandidates missing selected package policy: %#v",
			out.LocalPolicyCandidates,
		)
	}
}

func TestSyncValidateUsesBOMLocalPatchPolicy(t *testing.T) {
	tempDir := t.TempDir()
	bomPath := filepath.Join(tempDir, "bom.yaml")
	packageRoot := filepath.Join(tempDir, "package")
	localRepoRoot := filepath.Join(tempDir, "local-repo")

	bomDoc := syncLifecycleBOM()
	bomDoc.Spec.LocalPatchPolicy = "policy/local-patch-policy.yaml"
	data, err := yaml.Marshal(bomDoc)
	if err != nil {
		t.Fatalf("yaml.Marshal(BOM) error = %v", err)
	}
	writeSyncTestFile(t, bomPath, string(data), 0o644)
	writeSyncTestPackage(t, packageRoot)
	writeSyncPolicyFile(t, filepath.Dir(bomPath), strings.TrimSpace(`
apiVersion: distribution.sealos.io/v1alpha1
kind: LocalPatchPolicy
metadata:
  name: bom-local-patch-policy
spec:
  scope: clusterLocal
  forbiddenExactPaths:
    - status
    - spec.selector
  forbiddenMetadataKeys:
    - uid
    - resourceVersion
    - generation
    - creationTimestamp
    - managedFields
    - ownerReferences
    - finalizers
    - generateName
    - selfLink
    - deletionTimestamp
    - deletionGracePeriodSeconds
  forbiddenContainerFields:
    - image
  kindRules:
    - kind: Deployment
      allowedPrefixes:
        - spec.template.metadata.annotations
`)+"\n")
	writeSyncLocalPatch(t, localRepoRoot, "runtime", "placement.patch.yaml", strings.TrimSpace(`
apiVersion: apps/v1
kind: Deployment
metadata:
  name: runtime
  namespace: default
spec:
  template:
    metadata:
      annotations:
        bom-policy.sealos.io/managed: "true"
`)+"\n")

	out := runSyncValidate(syncValidateOptions{
		BOMPath:        bomPath,
		LocalRepoPath:  localRepoRoot,
		PackageSources: []string{"runtime=" + packageRoot},
	})
	if !out.Passed {
		t.Fatalf("validate passed = false, issues=%#v", out.Issues)
	}
	if got, want := out.LocalPolicySource, "bom"; got != want {
		t.Fatalf("localPolicySource = %q, want %q", got, want)
	}
	if got, want := out.LocalPolicy, "policy/local-patch-policy.yaml"; got != want {
		t.Fatalf("localPolicy = %q, want %q", got, want)
	}
	if got, want := out.LocalPolicyName, "bom-local-patch-policy"; got != want {
		t.Fatalf("localPolicyName = %q, want %q", got, want)
	}
	if got, want := out.LocalPolicyScope, "clusterLocal"; got != want {
		t.Fatalf("localPolicyScope = %q, want %q", got, want)
	}
	if !syncValidateHasSelectedPolicyCandidate(
		out.LocalPolicyCandidates,
		"bom",
		"",
	) {
		t.Fatalf(
			"localPolicyCandidates missing selected BOM policy: %#v",
			out.LocalPolicyCandidates,
		)
	}
}

func TestSyncEffectiveLocalPatchPolicyUsesHigherPrecedenceSourcesBeforePackage(t *testing.T) {
	tempDir := t.TempDir()
	bomRoot := filepath.Join(tempDir, "bom")
	packageRoot := filepath.Join(tempDir, "package")
	localRepoRoot := filepath.Join(tempDir, "local-repo")

	writeSyncTestPackage(t, packageRoot)
	writeSyncPackagePatchPolicy(t, packageRoot, strings.TrimSpace(`
apiVersion: distribution.sealos.io/v1alpha1
kind: LocalPatchPolicy
metadata:
  name: package-local-patch-policy
spec:
  scope: clusterLocal
  forbiddenExactPaths:
    - status
    - spec.selector
  forbiddenMetadataKeys:
    - uid
    - resourceVersion
    - generation
    - creationTimestamp
    - managedFields
    - ownerReferences
    - finalizers
    - generateName
    - selfLink
    - deletionTimestamp
    - deletionGracePeriodSeconds
  forbiddenContainerFields:
    - image
  kindRules:
    - kind: ConfigMap
      allowedPrefixes:
        - data
`)+"\n")
	pkg, err := packageformat.LoadDir(packageRoot)
	if err != nil {
		t.Fatalf("packageformat.LoadDir() error = %v", err)
	}

	bomDoc := syncLifecycleBOM()
	bomDoc.Spec.LocalPatchPolicy = "policy/local-patch-policy.yaml"
	writeSyncPolicyFile(t, bomRoot, strings.TrimSpace(`
apiVersion: distribution.sealos.io/v1alpha1
kind: LocalPatchPolicy
metadata:
  name: bom-local-patch-policy
spec:
  scope: clusterLocal
  forbiddenExactPaths:
    - status
    - spec.selector
  forbiddenMetadataKeys:
    - uid
    - resourceVersion
    - generation
    - creationTimestamp
    - managedFields
    - ownerReferences
    - finalizers
    - generateName
    - selfLink
    - deletionTimestamp
    - deletionGracePeriodSeconds
  forbiddenContainerFields:
    - image
  kindRules:
    - kind: ConfigMap
      allowedPrefixes:
        - data
`)+"\n")

	selection, err := syncEffectiveLocalPatchPolicy(
		bomDoc,
		map[string]*packageformat.ComponentPackage{"runtime": pkg},
		bomRoot,
		nil,
		nil,
	)
	if err != nil {
		t.Fatalf("syncEffectiveLocalPatchPolicy() error = %v", err)
	}
	if got, want := string(selection.Source), "bom"; got != want {
		t.Fatalf("source = %q, want %q", got, want)
	}
	if got, want := selection.Path, "policy/local-patch-policy.yaml"; got != want {
		t.Fatalf("policyPath = %q, want %q", got, want)
	}
	if got, want := selection.Document.EffectiveName(), "bom-local-patch-policy"; got != want {
		t.Fatalf("policy.EffectiveName() = %q, want %q", got, want)
	}
	if !syncValidateHasSelectedPolicyCandidate(
		selection.Candidates,
		"bom",
		"",
	) {
		t.Fatalf("selection.Candidates missing selected BOM policy: %#v", selection.Candidates)
	}
	if !syncValidateHasPolicyCandidate(
		selection.Candidates,
		"package",
		"runtime",
		false,
	) {
		t.Fatalf(
			"selection.Candidates missing unselected package policy: %#v",
			selection.Candidates,
		)
	}

	writeSyncPolicyFile(t, localRepoRoot, strings.TrimSpace(`
apiVersion: distribution.sealos.io/v1alpha1
kind: LocalPatchPolicy
metadata:
  name: repo-local-patch-policy
spec:
  scope: clusterLocal
  forbiddenExactPaths:
    - status
    - spec.selector
  forbiddenMetadataKeys:
    - uid
    - resourceVersion
    - generation
    - creationTimestamp
    - managedFields
    - ownerReferences
    - finalizers
    - generateName
    - selfLink
    - deletionTimestamp
    - deletionGracePeriodSeconds
  forbiddenContainerFields:
    - image
  kindRules:
    - kind: ConfigMap
      allowedPrefixes:
        - data
`)+"\n")
	repo, err := localrepo.Load(localRepoRoot)
	if err != nil {
		t.Fatalf("localrepo.Load() error = %v", err)
	}

	selection, err = syncEffectiveLocalPatchPolicy(
		bomDoc,
		map[string]*packageformat.ComponentPackage{"runtime": pkg},
		bomRoot,
		nil,
		repo,
	)
	if err != nil {
		t.Fatalf("syncEffectiveLocalPatchPolicy() error = %v", err)
	}
	if got, want := string(selection.Source), "localRepo"; got != want {
		t.Fatalf("source = %q, want %q", got, want)
	}
	if got, want := selection.Path, "policy/local-patch-policy.yaml"; got != want {
		t.Fatalf("policyPath = %q, want %q", got, want)
	}
	if got, want := selection.Document.EffectiveName(), "repo-local-patch-policy"; got != want {
		t.Fatalf("policy.EffectiveName() = %q, want %q", got, want)
	}
	if !syncValidateHasSelectedPolicyCandidate(
		selection.Candidates,
		"localRepo",
		"",
	) {
		t.Fatalf(
			"selection.Candidates missing selected localRepo policy: %#v",
			selection.Candidates,
		)
	}
	if !syncValidateHasPolicyCandidate(
		selection.Candidates,
		"bom",
		"",
		false,
	) {
		t.Fatalf("selection.Candidates missing unselected BOM policy: %#v", selection.Candidates)
	}
	if !syncValidateHasPolicyCandidate(
		selection.Candidates,
		"package",
		"runtime",
		false,
	) {
		t.Fatalf(
			"selection.Candidates missing unselected package policy: %#v",
			selection.Candidates,
		)
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
	writeSyncTestFile(
		t,
		filepath.Join(
			localRepoRoot,
			localrepo.InputsDirName,
			"runtime",
			"hosts",
			"10.0.0.13",
			"runtime-config.yaml",
		),
		"clusterName: stale\n",
		0o644,
	)

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

func writeSyncPackagePatchPolicy(t *testing.T, packageRoot, content string) {
	t.Helper()

	manifestPath := filepath.Join(packageRoot, "package.yaml")
	data, err := os.ReadFile(manifestPath)
	if err != nil {
		t.Fatalf("ReadFile(%q) error = %v", manifestPath, err)
	}
	needle := "  contents:\n"
	if !strings.Contains(string(data), needle) {
		t.Fatalf("package manifest missing %q", needle)
	}
	updated := strings.Replace(
		string(data),
		needle,
		"  localPatchPolicy: policy/local-patch-policy.yaml\n"+needle,
		1,
	)
	if err := os.WriteFile(manifestPath, []byte(updated), 0o644); err != nil {
		t.Fatalf("WriteFile(%q) error = %v", manifestPath, err)
	}
	path := filepath.Join(packageRoot, "policy", "local-patch-policy.yaml")
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("MkdirAll(%q) error = %v", filepath.Dir(path), err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("WriteFile(%q) error = %v", path, err)
	}
}

func writeSyncPolicyFile(t *testing.T, root, content string) {
	t.Helper()

	path := filepath.Join(root, "policy", "local-patch-policy.yaml")
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("MkdirAll(%q) error = %v", filepath.Dir(path), err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("WriteFile(%q) error = %v", path, err)
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

	writeSyncTestFile(
		t,
		filepath.Join(root, "package.yaml"),
		`apiVersion: distribution.sealos.io/v1alpha1
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
`,
		0o644,
	)
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

func syncValidateHasSelectedPolicyCandidate(
	candidates []hydrate.LocalPatchPolicyCandidate,
	source, component string,
) bool {
	return syncValidateHasPolicyCandidate(
		candidates,
		source,
		component,
		true,
	)
}

func syncValidateHasPolicyCandidate(
	candidates []hydrate.LocalPatchPolicyCandidate,
	source, component string,
	selected bool,
) bool {
	for _, candidate := range candidates {
		if string(candidate.Source) == source &&
			candidate.Path == "policy/local-patch-policy.yaml" &&
			candidate.Component == component &&
			candidate.Selected == selected {
			return true
		}
	}
	return false
}
