package commit

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/labring/sealos/pkg/distribution/bom"
	"github.com/labring/sealos/pkg/distribution/compare"
	"github.com/labring/sealos/pkg/distribution/hydrate"
	"github.com/labring/sealos/pkg/distribution/localrepo"
	yamlutil "github.com/labring/sealos/pkg/utils/yaml"
)

type resolverFunc func(apiVersion, kind, namespace, name string) ([]byte, error)

func (f resolverFunc) Get(apiVersion, kind, namespace, name string) ([]byte, error) {
	return f(apiVersion, kind, namespace, name)
}

func TestLocalPatches(t *testing.T) {
	t.Parallel()

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

	repo, err := localrepo.Load(localRepoRoot)
	if err != nil {
		t.Fatalf("localrepo.Load() error = %v", err)
	}
	beforeRevision := repo.Revision

	resolver := compare.NewKubectlResolver(func(args ...string) ([]byte, error) {
		return []byte(`{"apiVersion":"v1","kind":"ConfigMap","metadata":{"name":"grafana-settings","namespace":"default"},"data":{"adminUser":"ops","imageTag":"10.4.0"}}`), nil
	})
	current, err := compare.CompareBundle(bundle, bundleDir, resolver)
	if err != nil {
		t.Fatalf("CompareBundle(before) error = %v", err)
	}
	if got, want := current.Summary.Dirty, 1; got != want {
		t.Fatalf("before summary.dirty = %d, want %d", got, want)
	}

	result, err := LocalPatches(Options{
		Bundle:        bundle,
		BundleRoot:    bundleDir,
		LocalRepo:     repo,
		CompareResult: current,
		Resolver:      resolver,
	})
	if err != nil {
		t.Fatalf("LocalPatches() error = %v", err)
	}
	if got, want := len(result.CommittedObjects), 1; got != want {
		t.Fatalf("len(result.CommittedObjects) = %d, want %d", got, want)
	}
	if got, want := result.CommittedObjects[0].RepoPath, patchRepoPath; got != want {
		t.Fatalf("CommittedObjects[0].RepoPath = %q, want %q", got, want)
	}
	if got := result.LocalRepoRevision; got == beforeRevision {
		t.Fatalf("LocalRepoRevision = %q, want a new revision", got)
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

	updatedBundle, err := loadTestBundle(filepath.Join(bundleDir, hydrate.BundleFileName))
	if err != nil {
		t.Fatalf("loadTestBundle() error = %v", err)
	}
	after, err := compare.CompareBundle(updatedBundle, bundleDir, resolver)
	if err != nil {
		t.Fatalf("CompareBundle(after) error = %v", err)
	}
	if got, want := after.Summary.Dirty, 0; got != want {
		t.Fatalf("after summary.dirty = %d, want %d", got, want)
	}
	if got, want := after.Summary.Clean, 1; got != want {
		t.Fatalf("after summary.clean = %d, want %d", got, want)
	}
	if result.DesiredStateDigest == "" {
		t.Fatal("DesiredStateDigest = empty, want sha256 digest")
	}
}

func TestLocalPatchesUsesRenderedLocalPatchPolicy(t *testing.T) {
	t.Parallel()

	bundleDir := t.TempDir()
	localRepoRoot := t.TempDir()

	bundle := &hydrate.Bundle{
		APIVersion: "distribution.sealos.io/v1alpha1",
		Kind:       "HydratedBundle",
		Spec: hydrate.BundleSpec{
			BOMName:                "minimal-single-node",
			Revision:               "rev-poc-001",
			Channel:                bom.ChannelAlpha,
			LocalPatchPolicySource: "localRepo",
			LocalPatchPolicyName:   "custom-configmap-policy",
			LocalPatchPolicyPath:   "policy/local-patch-policy.yaml",
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
	policyBundlePath := filepath.Join(bundleDir, "policy", "local-patch-policy.yaml")
	for _, path := range []string{manifestPath, patchBundlePath, patchRepoPath, policyBundlePath} {
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatalf("MkdirAll(%q) error = %v", filepath.Dir(path), err)
		}
	}

	manifest := "apiVersion: v1\nkind: ConfigMap\nmetadata:\n  name: grafana-settings\n  namespace: default\n  annotations:\n    localFeature: disabled\ndata:\n  adminUser: admin\n"
	patch := "apiVersion: v1\nkind: ConfigMap\nmetadata:\n  name: grafana-settings\n  namespace: default\n  annotations:\n    localFeature: disabled\n"
	policy := "apiVersion: distribution.sealos.io/v1alpha1\nkind: LocalPatchPolicy\nmetadata:\n  name: custom-configmap-policy\nspec:\n  scope: clusterLocal\n  forbiddenExactPaths:\n    - status\n    - spec.selector\n  forbiddenMetadataKeys:\n    - uid\n    - resourceVersion\n    - generation\n    - creationTimestamp\n    - managedFields\n    - ownerReferences\n    - finalizers\n    - generateName\n    - selfLink\n    - deletionTimestamp\n    - deletionGracePeriodSeconds\n  forbiddenContainerFields:\n    - image\n  kindRules:\n    - kind: ConfigMap\n      allowedPrefixes:\n        - metadata.annotations\n"
	if err := os.WriteFile(manifestPath, []byte(manifest), 0o644); err != nil {
		t.Fatalf("WriteFile(%q) error = %v", manifestPath, err)
	}
	if err := os.WriteFile(patchBundlePath, []byte(patch), 0o644); err != nil {
		t.Fatalf("WriteFile(%q) error = %v", patchBundlePath, err)
	}
	if err := os.WriteFile(patchRepoPath, []byte(patch), 0o644); err != nil {
		t.Fatalf("WriteFile(%q) error = %v", patchRepoPath, err)
	}
	if err := os.WriteFile(policyBundlePath, []byte(policy), 0o644); err != nil {
		t.Fatalf("WriteFile(%q) error = %v", policyBundlePath, err)
	}
	if err := yamlutil.MarshalFile(filepath.Join(bundleDir, hydrate.BundleFileName), bundle); err != nil {
		t.Fatalf("MarshalFile(bundle) error = %v", err)
	}

	repo, err := localrepo.Load(localRepoRoot)
	if err != nil {
		t.Fatalf("localrepo.Load() error = %v", err)
	}

	resolver := compare.NewKubectlResolver(func(args ...string) ([]byte, error) {
		return []byte(`{"apiVersion":"v1","kind":"ConfigMap","metadata":{"name":"grafana-settings","namespace":"default","annotations":{"localFeature":"enabled"}},"data":{"adminUser":"admin"}}`), nil
	})
	current, err := compare.CompareBundle(bundle, bundleDir, resolver)
	if err != nil {
		t.Fatalf("CompareBundle(before) error = %v", err)
	}
	if got, want := current.Summary.Dirty, 1; got != want {
		t.Fatalf("before summary.dirty = %d, want %d", got, want)
	}
	if got, want := current.Objects[0].Mismatches[0].PolicyName, "custom-configmap-policy"; got != want {
		t.Fatalf("before mismatches[0].policyName = %q, want %q", got, want)
	}

	result, err := LocalPatches(Options{
		Bundle:        bundle,
		BundleRoot:    bundleDir,
		LocalRepo:     repo,
		CompareResult: current,
		Resolver:      resolver,
	})
	if err != nil {
		t.Fatalf("LocalPatches() error = %v", err)
	}
	if got, want := len(result.CommittedObjects), 1; got != want {
		t.Fatalf("len(result.CommittedObjects) = %d, want %d", got, want)
	}

	for _, path := range []string{patchRepoPath, patchBundlePath, manifestPath} {
		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("ReadFile(%q) error = %v", path, err)
		}
		if !strings.Contains(string(data), "localFeature: enabled") {
			t.Fatalf("%q missing committed local annotation override:\n%s", path, string(data))
		}
	}
}

func TestLocalPatchesCommitsStandaloneLocalResource(t *testing.T) {
	t.Parallel()

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

	repo, err := localrepo.Load(localRepoRoot)
	if err != nil {
		t.Fatalf("localrepo.Load() error = %v", err)
	}

	resolver := compare.NewKubectlResolver(func(args ...string) ([]byte, error) {
		return []byte(`{"apiVersion":"v1","kind":"Secret","metadata":{"name":"grafana-admin-credentials","namespace":"default","resourceVersion":"7","uid":"abcd"},"type":"Opaque","data":{"username":"b3Bz","password":"bmV3LXBhc3M="}}`), nil
	})
	current, err := compare.CompareBundle(bundle, bundleDir, resolver)
	if err != nil {
		t.Fatalf("CompareBundle(before) error = %v", err)
	}
	if got, want := current.Summary.Dirty, 1; got != want {
		t.Fatalf("before summary.dirty = %d, want %d", got, want)
	}

	result, err := LocalPatches(Options{
		Bundle:        bundle,
		BundleRoot:    bundleDir,
		LocalRepo:     repo,
		CompareResult: current,
		Resolver:      resolver,
	})
	if err != nil {
		t.Fatalf("LocalPatches() error = %v", err)
	}
	if got, want := len(result.CommittedObjects), 1; got != want {
		t.Fatalf("len(result.CommittedObjects) = %d, want %d", got, want)
	}
	if got, want := result.CommittedObjects[0].RepoPath, repoResourcePath; got != want {
		t.Fatalf("CommittedObjects[0].RepoPath = %q, want %q", got, want)
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

	updatedBundle, err := loadTestBundle(filepath.Join(bundleDir, hydrate.BundleFileName))
	if err != nil {
		t.Fatalf("loadTestBundle() error = %v", err)
	}
	after, err := compare.CompareBundle(updatedBundle, bundleDir, resolver)
	if err != nil {
		t.Fatalf("CompareBundle(after) error = %v", err)
	}
	if got, want := after.Summary.Dirty, 0; got != want {
		t.Fatalf("after summary.dirty = %d, want %d", got, want)
	}
}

func TestLocalPatchesCommitsLocalInputHostPath(t *testing.T) {
	t.Parallel()

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

	repo, err := localrepo.Load(localRepoRoot)
	if err != nil {
		t.Fatalf("localrepo.Load() error = %v", err)
	}
	beforeRevision := repo.Revision

	notFoundResolver := resolverFunc(func(apiVersion, kind, namespace, name string) ([]byte, error) {
		return nil, compare.NewNotFoundError(apiVersion, kind, namespace, name)
	})
	current, err := compare.CompareBundleWithOptions(bundle, bundleDir, notFoundResolver, compare.CompareOptions{HostRoot: hostRoot})
	if err != nil {
		t.Fatalf("CompareBundleWithOptions(before) error = %v", err)
	}
	if got, want := current.Summary.Dirty, 1; got != want {
		t.Fatalf("before summary.dirty = %d, want %d", got, want)
	}

	result, err := LocalPatches(Options{
		Bundle:        bundle,
		BundleRoot:    bundleDir,
		LocalRepo:     repo,
		CompareResult: current,
		Resolver:      notFoundResolver,
		HostRoot:      hostRoot,
	})
	if err != nil {
		t.Fatalf("LocalPatches() error = %v", err)
	}
	if got, want := len(result.CommittedHostPaths), 1; got != want {
		t.Fatalf("len(result.CommittedHostPaths) = %d, want %d", got, want)
	}
	if got, want := result.CommittedHostPaths[0].RepoPath, repoInputPath; got != want {
		t.Fatalf("CommittedHostPaths[0].RepoPath = %q, want %q", got, want)
	}
	if got := result.LocalRepoRevision; got == beforeRevision {
		t.Fatalf("LocalRepoRevision = %q, want a new revision", got)
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

	updatedBundle, err := loadTestBundle(filepath.Join(bundleDir, hydrate.BundleFileName))
	if err != nil {
		t.Fatalf("loadTestBundle() error = %v", err)
	}
	after, err := compare.CompareBundleWithOptions(updatedBundle, bundleDir, notFoundResolver, compare.CompareOptions{HostRoot: hostRoot})
	if err != nil {
		t.Fatalf("CompareBundleWithOptions(after) error = %v", err)
	}
	if got, want := after.Summary.Dirty, 0; got != want {
		t.Fatalf("after summary.dirty = %d, want %d", got, want)
	}
	if got, want := after.Summary.Clean, 1; got != want {
		t.Fatalf("after summary.clean = %d, want %d", got, want)
	}
}

func loadTestBundle(path string) (*hydrate.Bundle, error) {
	var bundle hydrate.Bundle
	if err := yamlutil.UnmarshalFile(path, &bundle); err != nil {
		return nil, err
	}
	return &bundle, nil
}

func TestLocalPatchesRejectsOrphan(t *testing.T) {
	t.Parallel()

	bundle := &hydrate.Bundle{
		APIVersion: "distribution.sealos.io/v1alpha1",
		Kind:       "HydratedBundle",
	}
	result := &compare.Result{
		Objects: []compare.ObjectStatus{
			{
				Tracked: hydrate.TrackedK8sObject{
					APIVersion: "apps/v1",
					Kind:       "DaemonSet",
					Namespace:  "kube-system",
					Name:       "cilium",
				},
				State: "Orphan",
			},
		},
	}
	repo, err := localrepo.Load(t.TempDir())
	if err != nil {
		t.Fatalf("localrepo.Load() error = %v", err)
	}

	if _, err := LocalPatches(Options{
		Bundle:        bundle,
		BundleRoot:    t.TempDir(),
		LocalRepo:     repo,
		CompareResult: result,
		Resolver:      compare.NewKubectlResolver(func(args ...string) ([]byte, error) { return nil, nil }),
	}); err == nil {
		t.Fatal("LocalPatches() error = nil, want error")
	}
}
