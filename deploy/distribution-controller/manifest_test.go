// Copyright 2026 sealos.
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

package distributioncontroller_test

import (
	"bytes"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/runtime/serializer"
	utilyaml "k8s.io/apimachinery/pkg/util/yaml"

	distributioncontroller "github.com/labring/sealos/pkg/distribution/controller"
)

func TestDistributionControllerManifestsDecode(t *testing.T) {
	t.Parallel()

	objects := loadManifestObjects(t,
		filepath.Join("base", "namespace.yaml"),
		filepath.Join("base", "crd.yaml"),
		filepath.Join("base", "rbac.yaml"),
		filepath.Join("base", "deployment.yaml"),
		filepath.Join("examples", "distribution-rollout-policy.yaml"),
		filepath.Join("examples", "distribution-target-bom.yaml"),
		filepath.Join("examples", "distribution-target-channel.yaml"),
	)

	want := []schema.GroupVersionKind{
		{Version: "v1", Kind: "Namespace"},
		{Group: "apiextensions.k8s.io", Version: "v1", Kind: "CustomResourceDefinition"},
		{Group: "apiextensions.k8s.io", Version: "v1", Kind: "CustomResourceDefinition"},
		{Version: "v1", Kind: "ServiceAccount"},
		{Group: "rbac.authorization.k8s.io", Version: "v1", Kind: "Role"},
		{Group: "rbac.authorization.k8s.io", Version: "v1", Kind: "RoleBinding"},
		{Group: "apps", Version: "v1", Kind: "Deployment"},
		{Group: "distribution.sealos.io", Version: "v1alpha1", Kind: "DistributionRolloutPolicy"},
		{Group: "distribution.sealos.io", Version: "v1alpha1", Kind: "DistributionTarget"},
		{Group: "distribution.sealos.io", Version: "v1alpha1", Kind: "DistributionTarget"},
	}
	if got := objectKinds(objects); !sameKinds(got, want) {
		t.Fatalf("manifest kinds = %v, want %v", got, want)
	}
}

func TestDistributionControllerDirectApplyFileSet(t *testing.T) {
	t.Parallel()

	objects := loadManifestObjects(t,
		filepath.Join("base", "namespace.yaml"),
		filepath.Join("base", "crd.yaml"),
		filepath.Join("base", "rbac.yaml"),
		filepath.Join("base", "deployment.yaml"),
	)

	want := []schema.GroupVersionKind{
		{Version: "v1", Kind: "Namespace"},
		{Group: "apiextensions.k8s.io", Version: "v1", Kind: "CustomResourceDefinition"},
		{Group: "apiextensions.k8s.io", Version: "v1", Kind: "CustomResourceDefinition"},
		{Version: "v1", Kind: "ServiceAccount"},
		{Group: "rbac.authorization.k8s.io", Version: "v1", Kind: "Role"},
		{Group: "rbac.authorization.k8s.io", Version: "v1", Kind: "RoleBinding"},
		{Group: "apps", Version: "v1", Kind: "Deployment"},
	}
	if got := objectKinds(objects); !sameKinds(got, want) {
		t.Fatalf("direct apply manifest kinds = %v, want %v", got, want)
	}
}

func TestDistributionTargetCRDMatchesControllerContract(t *testing.T) {
	t.Parallel()

	crd := loadCRD(t, "distributiontargets.distribution.sealos.io")

	if got, want := crd.Name, "distributiontargets.distribution.sealos.io"; got != want {
		t.Fatalf("CRD name = %q, want %q", got, want)
	}
	if got, want := crd.Spec.Group, "distribution.sealos.io"; got != want {
		t.Fatalf("CRD group = %q, want %q", got, want)
	}
	if got, want := crd.Spec.Names.Kind, distributioncontroller.KindDistributionTarget; got != want {
		t.Fatalf("CRD kind = %q, want %q", got, want)
	}
	if got, want := crd.Spec.Scope, apiextensionsv1.NamespaceScoped; got != want {
		t.Fatalf("CRD scope = %q, want %q", got, want)
	}
	version := crdVersion(t, crd, "v1alpha1")
	if version.Subresources == nil || version.Subresources.Status == nil {
		t.Fatal("CRD v1alpha1 status subresource is not enabled")
	}
	if version.Schema == nil || version.Schema.OpenAPIV3Schema == nil {
		t.Fatal("CRD v1alpha1 schema is missing")
	}
	spec := version.Schema.OpenAPIV3Schema.Properties["spec"]
	for _, field := range []string{
		"clusterName",
		"bomPath",
		"distributionChannelPath",
		"localRepoPath",
		"localPatchRevision",
		"packageSources",
		"cacheRoot",
		"kubeconfigPath",
		"hostRoot",
		"rolloutPolicyRef",
		"rolloutBatchSize",
		"requeueAfter",
	} {
		if _, ok := spec.Properties[field]; !ok {
			t.Fatalf("CRD spec schema missing field %q", field)
		}
	}
	if len(spec.OneOf) != 2 {
		t.Fatalf("CRD spec oneOf target validation count = %d, want 2", len(spec.OneOf))
	}
	status := version.Schema.OpenAPIV3Schema.Properties["status"]
	for _, field := range []string{
		"observedGeneration",
		"lastReconcileTime",
		"lastResult",
		"conditions",
	} {
		if _, ok := status.Properties[field]; !ok {
			t.Fatalf("CRD status schema missing field %q", field)
		}
	}
}

func TestDistributionRolloutPolicyCRDMatchesControllerContract(t *testing.T) {
	t.Parallel()

	crd := loadCRD(t, "distributionrolloutpolicies.distribution.sealos.io")

	if got, want := crd.Spec.Group, "distribution.sealos.io"; got != want {
		t.Fatalf("CRD group = %q, want %q", got, want)
	}
	if got, want := crd.Spec.Names.Kind, distributioncontroller.KindDistributionRolloutPolicy; got != want {
		t.Fatalf("CRD kind = %q, want %q", got, want)
	}
	if got, want := crd.Spec.Scope, apiextensionsv1.NamespaceScoped; got != want {
		t.Fatalf("CRD scope = %q, want %q", got, want)
	}
	version := crdVersion(t, crd, "v1alpha1")
	if version.Subresources == nil || version.Subresources.Status == nil {
		t.Fatal("CRD v1alpha1 status subresource is not enabled")
	}
	spec := version.Schema.OpenAPIV3Schema.Properties["spec"]
	strategy := spec.Properties["strategy"]
	if _, ok := strategy.Properties["batchSize"]; !ok {
		t.Fatal("CRD rollout policy strategy schema missing batchSize")
	}
}

func TestDistributionControllerDeploymentContract(t *testing.T) {
	t.Parallel()

	var deployment appsv1.Deployment
	loadSingleManifestObject(t, filepath.Join("base", "deployment.yaml"), &deployment)

	if got, want := deployment.Namespace, "sealos-system"; got != want {
		t.Fatalf("Deployment namespace = %q, want %q", got, want)
	}
	if got, want := deployment.Spec.Template.Spec.ServiceAccountName, "sealos-distribution-controller"; got != want {
		t.Fatalf("service account = %q, want %q", got, want)
	}
	assertRequiredNodeAffinity(t, deployment.Spec.Template.Spec.Affinity, "node-role.kubernetes.io/control-plane")
	assertRequiredNodeAffinity(t, deployment.Spec.Template.Spec.Affinity, "node-role.kubernetes.io/master")
	assertToleration(t, deployment.Spec.Template.Spec.Tolerations, "node-role.kubernetes.io/control-plane", corev1.TaintEffectNoSchedule)
	assertToleration(t, deployment.Spec.Template.Spec.Tolerations, "node-role.kubernetes.io/master", corev1.TaintEffectNoSchedule)
	if len(deployment.Spec.Template.Spec.Containers) != 1 {
		t.Fatalf("container count = %d, want 1", len(deployment.Spec.Template.Spec.Containers))
	}
	container := deployment.Spec.Template.Spec.Containers[0]
	if !contains(container.Command, "/usr/bin/sealos-agent") {
		t.Fatalf("container command missing /usr/bin/sealos-agent: %v", container.Command)
	}
	for _, arg := range []string{
		"--controller",
		"--controller-namespace=sealos-system",
		"--leader-elect",
		"--kubeconfig=/host/etc/kubernetes/admin.conf",
		"--host-root=/host",
	} {
		if !contains(container.Args, arg) {
			t.Fatalf("container args missing %q: %v", arg, container.Args)
		}
	}
	if container.SecurityContext == nil || container.SecurityContext.Privileged == nil || !*container.SecurityContext.Privileged {
		t.Fatal("controller container must be privileged for host apply steps")
	}
	volumeMounts := map[string]string{}
	for _, mount := range container.VolumeMounts {
		volumeMounts[mount.Name] = mount.MountPath
	}
	if got, want := volumeMounts["host-root"], "/host"; got != want {
		t.Fatalf("host-root mount = %q, want %q", got, want)
	}
	if got, want := volumeMounts["var-lib-sealos"], "/var/lib/sealos"; got != want {
		t.Fatalf("var-lib-sealos mount = %q, want %q", got, want)
	}
	if !pathEnvIncludesHostBins(container.Env) {
		t.Fatalf("PATH env does not include host bin paths: %v", container.Env)
	}
}

func TestDistributionControllerRBACContract(t *testing.T) {
	t.Parallel()

	objects := loadManifestObjects(t, filepath.Join("base", "rbac.yaml"))
	var role *rbacv1.Role
	for _, object := range objects {
		if typed, ok := object.(*rbacv1.Role); ok {
			role = typed
			break
		}
	}
	if role == nil {
		t.Fatal("Role not found")
	}

	assertRule(t, role.Rules, []string{"distribution.sealos.io"}, []string{"distributiontargets", "distributionrolloutpolicies"}, []string{"get", "list", "watch"})
	assertRule(t, role.Rules, []string{"distribution.sealos.io"}, []string{"distributiontargets/status"}, []string{"get", "patch", "update"})
	assertRule(t, role.Rules, []string{"coordination.k8s.io"}, []string{"leases"}, []string{"create", "get", "list", "update", "watch"})
	assertRule(t, role.Rules, []string{""}, []string{"events"}, []string{"create", "patch"})
}

func loadCRD(t *testing.T, name string) apiextensionsv1.CustomResourceDefinition {
	t.Helper()

	objects := loadManifestObjects(t, filepath.Join("base", "crd.yaml"))
	for _, object := range objects {
		crd, ok := object.(*apiextensionsv1.CustomResourceDefinition)
		if ok && crd.Name == name {
			return *crd
		}
	}
	t.Fatalf("CRD %q not found", name)
	return apiextensionsv1.CustomResourceDefinition{}
}

func loadSingleManifestObject(t *testing.T, relPath string, into runtime.Object) {
	t.Helper()

	objects := loadManifestObjects(t, relPath)
	if len(objects) != 1 {
		t.Fatalf("%s object count = %d, want 1", relPath, len(objects))
	}
	data := readManifest(t, relPath)
	decoder := newManifestDecoder(t)
	if _, _, err := decoder.Decode(data, nil, into); err != nil {
		t.Fatalf("decode %s into %T: %v", relPath, into, err)
	}
}

func loadManifestObjects(t *testing.T, paths ...string) []runtime.Object {
	t.Helper()

	decoder := newManifestDecoder(t)
	objects := make([]runtime.Object, 0)
	for _, relPath := range paths {
		yamlDecoder := utilyaml.NewYAMLOrJSONDecoder(bytes.NewReader(readManifest(t, relPath)), 4096)
		for {
			var raw runtime.RawExtension
			err := yamlDecoder.Decode(&raw)
			if err == io.EOF {
				break
			}
			if err != nil {
				t.Fatalf("split %s: %v", relPath, err)
			}
			if len(bytes.TrimSpace(raw.Raw)) == 0 {
				continue
			}
			object, _, err := decoder.Decode(raw.Raw, nil, nil)
			if err != nil {
				t.Fatalf("decode %s: %v", relPath, err)
			}
			objects = append(objects, object)
		}
	}
	return objects
}

func readManifest(t *testing.T, relPath string) []byte {
	t.Helper()

	data, err := os.ReadFile(relPath)
	if err != nil {
		t.Fatalf("read %s: %v", relPath, err)
	}
	return data
}

func newManifestDecoder(t *testing.T) runtime.Decoder {
	t.Helper()

	scheme := runtime.NewScheme()
	for _, add := range []func(*runtime.Scheme) error{
		corev1.AddToScheme,
		appsv1.AddToScheme,
		rbacv1.AddToScheme,
		apiextensionsv1.AddToScheme,
		distributioncontroller.AddToScheme,
	} {
		if err := add(scheme); err != nil {
			t.Fatalf("add scheme: %v", err)
		}
	}
	return serializer.NewCodecFactory(scheme).UniversalDeserializer()
}

func crdVersion(t *testing.T, crd apiextensionsv1.CustomResourceDefinition, name string) apiextensionsv1.CustomResourceDefinitionVersion {
	t.Helper()

	for _, version := range crd.Spec.Versions {
		if version.Name == name {
			return version
		}
	}
	t.Fatalf("CRD version %q not found", name)
	return apiextensionsv1.CustomResourceDefinitionVersion{}
}

func objectKinds(objects []runtime.Object) []schema.GroupVersionKind {
	kinds := make([]schema.GroupVersionKind, 0, len(objects))
	for _, object := range objects {
		kinds = append(kinds, object.GetObjectKind().GroupVersionKind())
	}
	sort.Slice(kinds, func(i, j int) bool {
		return kinds[i].String() < kinds[j].String()
	})
	return kinds
}

func sameKinds(got, want []schema.GroupVersionKind) bool {
	sort.Slice(want, func(i, j int) bool {
		return want[i].String() < want[j].String()
	})
	if len(got) != len(want) {
		return false
	}
	for i := range got {
		if got[i] != want[i] {
			return false
		}
	}
	return true
}

func contains(values []string, want string) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}

func pathEnvIncludesHostBins(env []corev1.EnvVar) bool {
	for _, item := range env {
		if item.Name != "PATH" {
			continue
		}
		parts := strings.Split(item.Value, ":")
		return contains(parts, "/host/usr/bin") && contains(parts, "/host/bin")
	}
	return false
}

func assertRule(t *testing.T, rules []rbacv1.PolicyRule, apiGroups, resources, verbs []string) {
	t.Helper()

	for _, rule := range rules {
		if sameStringSet(rule.APIGroups, apiGroups) &&
			sameStringSet(rule.Resources, resources) &&
			containsAll(rule.Verbs, verbs) {
			return
		}
	}
	t.Fatalf("RBAC rule not found for apiGroups=%v resources=%v verbs=%v", apiGroups, resources, verbs)
}

func assertToleration(t *testing.T, tolerations []corev1.Toleration, key string, effect corev1.TaintEffect) {
	t.Helper()

	for _, toleration := range tolerations {
		if toleration.Key == key && toleration.Operator == corev1.TolerationOpExists && toleration.Effect == effect {
			return
		}
	}
	t.Fatalf("toleration not found for key=%q effect=%q", key, effect)
}

func assertRequiredNodeAffinity(t *testing.T, affinity *corev1.Affinity, key string) {
	t.Helper()

	if affinity == nil ||
		affinity.NodeAffinity == nil ||
		affinity.NodeAffinity.RequiredDuringSchedulingIgnoredDuringExecution == nil {
		t.Fatalf("required node affinity missing for key=%q", key)
	}
	for _, term := range affinity.NodeAffinity.RequiredDuringSchedulingIgnoredDuringExecution.NodeSelectorTerms {
		for _, expression := range term.MatchExpressions {
			if expression.Key == key && expression.Operator == corev1.NodeSelectorOpExists {
				return
			}
		}
	}
	t.Fatalf("required node affinity missing for key=%q", key)
}

func sameStringSet(got, want []string) bool {
	if len(got) != len(want) {
		return false
	}
	gotCopy := append([]string(nil), got...)
	wantCopy := append([]string(nil), want...)
	sort.Strings(gotCopy)
	sort.Strings(wantCopy)
	for i := range gotCopy {
		if gotCopy[i] != wantCopy[i] {
			return false
		}
	}
	return true
}

func containsAll(values, wants []string) bool {
	for _, want := range wants {
		if !contains(values, want) {
			return false
		}
	}
	return true
}
