package hydrate

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/opencontainers/go-digest"

	"github.com/labring/sealos/pkg/distribution/ownership"
	yamlutil "github.com/labring/sealos/pkg/utils/yaml"
)

func TestLoadBundleLocalPatchPolicyLegacyDefault(t *testing.T) {
	t.Parallel()

	doc, err := LoadBundleLocalPatchPolicy(&Bundle{}, t.TempDir())
	if err != nil {
		t.Fatalf("LoadBundleLocalPatchPolicy() error = %v", err)
	}
	if got, want := doc.EffectiveName(), ownership.DefaultLocalPatchPolicyName; got != want {
		t.Fatalf("doc.EffectiveName() = %q, want %q", got, want)
	}
	if got, want := doc.Spec.EffectiveScope(), ownership.LocalPatchPolicyScopeClusterLocal; got != want {
		t.Fatalf("doc.Spec.EffectiveScope() = %q, want %q", got, want)
	}
}

func TestLoadBundleLocalPatchPolicyRejectsUnsupportedSource(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "policy"), 0o755); err != nil {
		t.Fatalf("MkdirAll() error = %v", err)
	}
	path := filepath.Join(root, "policy", ownership.LocalPatchPolicyFileName)
	if err := os.WriteFile(path, testLocalPatchPolicyYAML("custom", "data"), 0o644); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	_, err := LoadBundleLocalPatchPolicy(&Bundle{
		Spec: BundleSpec{
			LocalPatchPolicySource: ownership.LocalPatchPolicySource("unsupported"),
			LocalPatchPolicyPath:   "policy/local-patch-policy.yaml",
		},
	}, root)
	if err == nil {
		t.Fatal("LoadBundleLocalPatchPolicy() error = nil, want error")
	}
	if !strings.Contains(err.Error(), "unsupported local patch policy source") {
		t.Fatalf("LoadBundleLocalPatchPolicy() error = %v, want unsupported source", err)
	}
}

func TestLoadBundleLocalPatchPolicyRejectsMissingSourceForExplicitPath(t *testing.T) {
	t.Parallel()

	_, err := LoadBundleLocalPatchPolicy(&Bundle{
		Spec: BundleSpec{
			LocalPatchPolicyPath: "policy/local-patch-policy.yaml",
		},
	}, t.TempDir())
	if err == nil {
		t.Fatal("LoadBundleLocalPatchPolicy() error = nil, want error")
	}
	if !strings.Contains(err.Error(), "local patch policy source must be set") {
		t.Fatalf("LoadBundleLocalPatchPolicy() error = %v, want missing source", err)
	}
}

func TestLoadBundleLocalPatchPolicyRejectsNameMismatch(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	path := filepath.Join(root, "policy", ownership.LocalPatchPolicyFileName)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("MkdirAll() error = %v", err)
	}
	content := testLocalPatchPolicyYAML("custom-name", "data")
	if err := os.WriteFile(path, content, 0o644); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	_, err := LoadBundleLocalPatchPolicy(&Bundle{
		Spec: BundleSpec{
			LocalPatchPolicySource: ownership.LocalPatchPolicySourceLocalRepo,
			LocalPatchPolicyName:   "expected-name",
			LocalPatchPolicyPath:   "policy/local-patch-policy.yaml",
			LocalPatchPolicyDigest: digest.Canonical.FromBytes(content).String(),
		},
	}, root)
	if err == nil {
		t.Fatal("LoadBundleLocalPatchPolicy() error = nil, want error")
	}
	if !strings.Contains(err.Error(), "local patch policy name mismatch") {
		t.Fatalf("LoadBundleLocalPatchPolicy() error = %v, want name mismatch", err)
	}
}

func TestLoadBundleLocalPatchPolicyRejectsDigestMismatch(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	path := filepath.Join(root, "policy", ownership.LocalPatchPolicyFileName)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("MkdirAll() error = %v", err)
	}
	content := testLocalPatchPolicyYAML("custom-name", "data")
	if err := os.WriteFile(path, content, 0o644); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	_, err := LoadBundleLocalPatchPolicy(&Bundle{
		Spec: BundleSpec{
			LocalPatchPolicySource: ownership.LocalPatchPolicySourceLocalRepo,
			LocalPatchPolicyName:   "custom-name",
			LocalPatchPolicyPath:   "policy/local-patch-policy.yaml",
			LocalPatchPolicyDigest: digest.Canonical.FromBytes([]byte("different")).String(),
		},
	}, root)
	if err == nil {
		t.Fatal("LoadBundleLocalPatchPolicy() error = nil, want error")
	}
	if !strings.Contains(err.Error(), "local patch policy digest mismatch") {
		t.Fatalf("LoadBundleLocalPatchPolicy() error = %v, want digest mismatch", err)
	}
}

func TestLoadBundleLocalPatchPolicyRejectsScopeMismatch(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	path := filepath.Join(root, "policy", ownership.LocalPatchPolicyFileName)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("MkdirAll() error = %v", err)
	}
	content := testLocalPatchPolicyYAML("custom-name", "data")
	if err := os.WriteFile(path, content, 0o644); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	_, err := LoadBundleLocalPatchPolicy(&Bundle{
		Spec: BundleSpec{
			LocalPatchPolicySource: ownership.LocalPatchPolicySourceLocalRepo,
			LocalPatchPolicyScope:  ownership.LocalPatchPolicyScope("packageBaseline"),
			LocalPatchPolicyName:   "custom-name",
			LocalPatchPolicyPath:   "policy/local-patch-policy.yaml",
			LocalPatchPolicyDigest: digest.Canonical.FromBytes(content).String(),
		},
	}, root)
	if err == nil {
		t.Fatal("LoadBundleLocalPatchPolicy() error = nil, want error")
	}
	if !strings.Contains(err.Error(), "local patch policy scope mismatch") {
		t.Fatalf("LoadBundleLocalPatchPolicy() error = %v, want scope mismatch", err)
	}
}

func TestLoadBundleLocalPatchPolicyFixtureRejectsDigestMismatch(t *testing.T) {
	t.Parallel()

	fixtureRoot := filepath.Join("testdata", "local-patch-policy-digest-mismatch")
	var bundle Bundle
	if err := yamlutil.UnmarshalFile(filepath.Join(fixtureRoot, BundleFileName), &bundle); err != nil {
		t.Fatalf("UnmarshalFile() error = %v", err)
	}

	_, err := LoadBundleLocalPatchPolicy(&bundle, fixtureRoot)
	if err == nil {
		t.Fatal("LoadBundleLocalPatchPolicy() error = nil, want digest mismatch")
	}
	if !strings.Contains(err.Error(), "local patch policy digest mismatch") {
		t.Fatalf("LoadBundleLocalPatchPolicy() error = %v, want digest mismatch", err)
	}
}

func testLocalPatchPolicyYAML(name string, allowedPrefixes ...string) []byte {
	prefixLines := make([]string, 0, len(allowedPrefixes))
	for _, prefix := range allowedPrefixes {
		prefixLines = append(prefixLines, "        - "+prefix)
	}
	return []byte("apiVersion: distribution.sealos.io/v1alpha1\nkind: LocalPatchPolicy\nmetadata:\n  name: " + name + "\nspec:\n  scope: clusterLocal\n  forbiddenExactPaths:\n    - status\n    - spec.selector\n  forbiddenMetadataKeys:\n    - uid\n    - resourceVersion\n    - generation\n    - creationTimestamp\n    - managedFields\n    - ownerReferences\n    - finalizers\n    - generateName\n    - selfLink\n    - deletionTimestamp\n    - deletionGracePeriodSeconds\n  forbiddenContainerFields:\n    - image\n  kindRules:\n    - kind: ConfigMap\n      allowedPrefixes:\n" + strings.Join(prefixLines, "\n") + "\n")
}
