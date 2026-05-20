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

package hydrate

import (
	"bytes"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"k8s.io/apimachinery/pkg/runtime"
	utilyaml "k8s.io/apimachinery/pkg/util/yaml"
	"sigs.k8s.io/yaml"

	"github.com/labring/sealos/pkg/distribution/ownership"
	yamlutil "github.com/labring/sealos/pkg/utils/yaml"
)

type manifestIdentity struct {
	APIVersion string
	Kind       string
	Namespace  string
	Name       string
}

type manifestPatchFile struct {
	path      string
	documents []map[string]interface{}
	dirty     bool
}

type manifestPatchTarget struct {
	file     *manifestPatchFile
	document int
}

type manifestPatchIndex struct {
	files   []*manifestPatchFile
	objects map[string][]manifestPatchTarget
}

func renderLocalPatches(component ComponentPlan, rendered *RenderedComponent, outputDir string, policy ownership.LocalPatchPolicy) error {
	if rendered == nil || len(component.LocalPatches) == 0 {
		return nil
	}

	patchRoot := filepath.Join(outputDir, "components", component.Name, "local-patches")
	for _, patch := range component.LocalPatches {
		relativePath := patch.RelativePath
		if relativePath == "" {
			relativePath = filepath.Base(patch.Path)
		}
		relativePath = filepath.ToSlash(relativePath)

		dst := filepath.Join(patchRoot, filepath.FromSlash(relativePath))
		if err := copyEntry(patch.Path, dst); err != nil {
			return fmt.Errorf("copy component %q local patch %q: %w", component.Name, patch.Path, err)
		}
		rendered.LocalPatches = append(rendered.LocalPatches, mustBundlePath(outputDir, dst))
	}

	return applyManifestPatches(component.Name, rendered, outputDir, policy)
}

func ReapplyRenderedLocalPatches(bundle *Bundle, outputDir string) error {
	if bundle == nil {
		return fmt.Errorf("bundle cannot be nil")
	}
	if strings.TrimSpace(outputDir) == "" {
		return fmt.Errorf("output directory cannot be empty")
	}

	policyDoc, err := LoadBundleLocalPatchPolicy(bundle, outputDir)
	if err != nil {
		return err
	}

	for i := range bundle.Spec.Components {
		component := &bundle.Spec.Components[i]
		if err := applyManifestPatches(component.Name, component, outputDir, policyDoc.Spec); err != nil {
			return err
		}
	}

	trackedObjects, err := collectTrackedK8sObjects(bundle, outputDir)
	if err != nil {
		return err
	}
	bundle.Spec.TrackedK8sObjects = trackedObjects
	trackedHostPaths, err := collectTrackedHostPaths(bundle, outputDir)
	if err != nil {
		return err
	}
	bundle.Spec.TrackedHostPaths = trackedHostPaths

	bundlePath := filepath.Join(outputDir, BundleFileName)
	if err := yamlutil.MarshalFile(bundlePath, bundle); err != nil {
		return fmt.Errorf("write bundle manifest %q: %w", bundlePath, err)
	}
	return nil
}

func applyManifestPatches(componentName string, rendered *RenderedComponent, outputDir string, policy ownership.LocalPatchPolicy) error {
	if rendered == nil || len(rendered.LocalPatches) == 0 {
		return nil
	}

	index, err := buildManifestPatchIndex(rendered, outputDir)
	if err != nil {
		return fmt.Errorf("index component %q manifest payloads: %w", componentName, err)
	}
	for _, patchBundlePath := range rendered.LocalPatches {
		patchPath, err := resolveInventoryBundlePath(outputDir, patchBundlePath)
		if err != nil {
			return fmt.Errorf("resolve component %q local patch %q: %w", componentName, patchBundlePath, err)
		}
		if err := applyManifestPatchFile(index, patchPath, policy); err != nil {
			return fmt.Errorf("apply component %q local patch %q: %w", componentName, patchBundlePath, err)
		}
	}
	return index.flush()
}

func buildManifestPatchIndex(rendered *RenderedComponent, outputDir string) (*manifestPatchIndex, error) {
	index := &manifestPatchIndex{
		files:   make([]*manifestPatchFile, 0),
		objects: make(map[string][]manifestPatchTarget),
	}

	for _, step := range rendered.Steps {
		if step.Kind != StepContent || step.ContentType != "manifest" {
			continue
		}

		resolved, err := resolveInventoryBundlePath(outputDir, step.BundlePath)
		if err != nil {
			return nil, err
		}
		if err := index.addResolvedManifestPath(resolved); err != nil {
			return nil, err
		}
	}
	return index, nil
}

func (i *manifestPatchIndex) addResolvedManifestPath(resolvedPath string) error {
	info, err := os.Stat(resolvedPath)
	if err != nil {
		return err
	}
	if info.IsDir() {
		paths := make([]string, 0)
		if err := filepath.WalkDir(resolvedPath, func(path string, entry os.DirEntry, walkErr error) error {
			if walkErr != nil {
				return walkErr
			}
			if entry.IsDir() {
				return nil
			}
			paths = append(paths, path)
			return nil
		}); err != nil {
			return err
		}
		sort.Strings(paths)
		for _, path := range paths {
			if err := i.addManifestFile(path); err != nil {
				return err
			}
		}
		return nil
	}
	return i.addManifestFile(resolvedPath)
}

func (i *manifestPatchIndex) addManifestFile(path string) error {
	for _, file := range i.files {
		if file.path == path {
			return nil
		}
	}

	documents, err := loadObjectDocuments(path)
	if err != nil {
		return err
	}
	file := &manifestPatchFile{
		path:      path,
		documents: documents,
	}
	i.files = append(i.files, file)

	for docIndex, document := range file.documents {
		identity, err := requiredManifestIdentity(document)
		if err != nil {
			return fmt.Errorf("index manifest file %q document %d: %w", path, docIndex, err)
		}
		key := manifestIdentityKey(identity)
		i.objects[key] = append(i.objects[key], manifestPatchTarget{file: file, document: docIndex})
	}
	return nil
}

func (i *manifestPatchIndex) flush() error {
	for _, file := range i.files {
		if !file.dirty {
			continue
		}
		if err := writeObjectDocuments(file.path, file.documents); err != nil {
			return err
		}
	}
	return nil
}

func applyManifestPatchFile(index *manifestPatchIndex, patchPath string, policy ownership.LocalPatchPolicy) error {
	documents, err := loadObjectDocuments(patchPath)
	if err != nil {
		return err
	}
	if len(documents) == 0 {
		return nil
	}

	dirty := false
	for docIndex, patchDocument := range documents {
		if err := ownership.ValidateLocalPatchOverlayWithPolicy(policy, patchDocument); err != nil {
			return fmt.Errorf("document %d: %w", docIndex, err)
		}
		if _, exists := patchDocument["status"]; exists {
			delete(patchDocument, "status")
			dirty = true
		}

		targets, identity, err := index.resolvePatchTargets(patchDocument)
		if err != nil {
			return fmt.Errorf("document %d: %w", docIndex, err)
		}
		if len(targets) != 1 {
			return fmt.Errorf("document %d: patch target %s is ambiguous", docIndex, manifestIdentityKey(identity))
		}
		target := targets[0]
		merged, ok := mergeManifestPatchValue(target.file.documents[target.document], patchDocument).(map[string]interface{})
		if !ok {
			return fmt.Errorf("document %d: patch target %s produced a non-object manifest", docIndex, manifestIdentityKey(identity))
		}
		target.file.documents[target.document] = merged
		target.file.dirty = true

		if ensurePatchIdentity(patchDocument, identity) {
			dirty = true
		}
	}

	if dirty {
		if err := writeObjectDocuments(patchPath, documents); err != nil {
			return err
		}
	}
	return nil
}

func (i *manifestPatchIndex) resolvePatchTargets(patchDocument map[string]interface{}) ([]manifestPatchTarget, manifestIdentity, error) {
	identity, err := requiredManifestIdentity(patchDocument)
	if err != nil {
		return nil, manifestIdentity{}, err
	}

	if identity.Namespace != "" {
		targets := append([]manifestPatchTarget(nil), i.objects[manifestIdentityKey(identity)]...)
		if len(targets) == 0 {
			return nil, manifestIdentity{}, fmt.Errorf("patch target %s not found", manifestIdentityKey(identity))
		}
		return targets, identity, nil
	}

	matches := make([]struct {
		identity manifestIdentity
		target   manifestPatchTarget
	}, 0)
	for key, targets := range i.objects {
		candidate := parseManifestIdentityKey(key)
		if candidate.APIVersion != identity.APIVersion || candidate.Kind != identity.Kind || candidate.Name != identity.Name {
			continue
		}
		for _, target := range targets {
			matches = append(matches, struct {
				identity manifestIdentity
				target   manifestPatchTarget
			}{
				identity: candidate,
				target:   target,
			})
		}
	}
	if len(matches) == 0 {
		return nil, manifestIdentity{}, fmt.Errorf("patch target %s not found", manifestIdentityKey(identity))
	}
	if len(matches) > 1 {
		return nil, manifestIdentity{}, fmt.Errorf("patch target %s must specify namespace", manifestIdentityKey(identity))
	}
	return []manifestPatchTarget{matches[0].target}, matches[0].identity, nil
}

func loadObjectDocuments(path string) ([]map[string]interface{}, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read Kubernetes object manifest %q: %w", path, err)
	}

	decoder := utilyaml.NewYAMLOrJSONDecoder(bytes.NewReader(data), 4096)
	documents := make([]map[string]interface{}, 0)
	for {
		var raw runtime.RawExtension
		if err := decoder.Decode(&raw); err != nil {
			if err == io.EOF {
				break
			}
			return nil, fmt.Errorf("decode Kubernetes object manifest %q: %w", path, err)
		}
		if len(bytes.TrimSpace(raw.Raw)) == 0 {
			continue
		}

		var document map[string]interface{}
		if err := yaml.Unmarshal(raw.Raw, &document); err != nil {
			return nil, fmt.Errorf("unmarshal Kubernetes object manifest %q: %w", path, err)
		}
		if len(document) == 0 {
			continue
		}
		documents = append(documents, document)
	}
	return documents, nil
}

func writeObjectDocuments(path string, documents []map[string]interface{}) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}

	rendered := make([][]byte, 0, len(documents))
	for _, document := range documents {
		data, err := yaml.Marshal(document)
		if err != nil {
			return fmt.Errorf("marshal Kubernetes object manifest %q: %w", path, err)
		}
		rendered = append(rendered, bytes.TrimRight(data, "\n"))
	}
	content := strings.Join(byteSlicesToStrings(rendered), "\n---\n")
	if len(content) > 0 {
		content += "\n"
	}
	return os.WriteFile(path, []byte(content), 0o644)
}

func byteSlicesToStrings(values [][]byte) []string {
	if len(values) == 0 {
		return nil
	}
	rendered := make([]string, 0, len(values))
	for _, value := range values {
		rendered = append(rendered, string(value))
	}
	return rendered
}

func requiredManifestIdentity(document map[string]interface{}) (manifestIdentity, error) {
	identity := extractManifestIdentity(document)
	switch {
	case identity.APIVersion == "":
		return manifestIdentity{}, fmt.Errorf("apiVersion cannot be empty")
	case identity.Kind == "":
		return manifestIdentity{}, fmt.Errorf("kind cannot be empty")
	case identity.Name == "":
		return manifestIdentity{}, fmt.Errorf("metadata.name cannot be empty")
	default:
		return identity, nil
	}
}

func extractManifestIdentity(document map[string]interface{}) manifestIdentity {
	identity := manifestIdentity{}
	if document == nil {
		return identity
	}
	if value, ok := document["apiVersion"].(string); ok {
		identity.APIVersion = value
	}
	if value, ok := document["kind"].(string); ok {
		identity.Kind = value
	}
	metadata, _ := document["metadata"].(map[string]interface{})
	if value, ok := metadata["name"].(string); ok {
		identity.Name = value
	}
	if value, ok := metadata["namespace"].(string); ok {
		identity.Namespace = value
	}
	return identity
}

func ensurePatchIdentity(document map[string]interface{}, identity manifestIdentity) bool {
	if document == nil {
		return false
	}

	changed := false
	if current, _ := document["apiVersion"].(string); current != identity.APIVersion {
		document["apiVersion"] = identity.APIVersion
		changed = true
	}
	if current, _ := document["kind"].(string); current != identity.Kind {
		document["kind"] = identity.Kind
		changed = true
	}

	metadata, ok := document["metadata"].(map[string]interface{})
	if !ok {
		metadata = make(map[string]interface{})
		document["metadata"] = metadata
		changed = true
	}
	if current, _ := metadata["name"].(string); current != identity.Name {
		metadata["name"] = identity.Name
		changed = true
	}
	if identity.Namespace == "" {
		if _, exists := metadata["namespace"]; exists {
			delete(metadata, "namespace")
			changed = true
		}
		return changed
	}
	if current, _ := metadata["namespace"].(string); current != identity.Namespace {
		metadata["namespace"] = identity.Namespace
		changed = true
	}
	return changed
}

func manifestIdentityKey(identity manifestIdentity) string {
	return strings.Join([]string{identity.APIVersion, identity.Kind, identity.Namespace, identity.Name}, "\x00")
}

func parseManifestIdentityKey(value string) manifestIdentity {
	parts := strings.Split(value, "\x00")
	identity := manifestIdentity{}
	if len(parts) > 0 {
		identity.APIVersion = parts[0]
	}
	if len(parts) > 1 {
		identity.Kind = parts[1]
	}
	if len(parts) > 2 {
		identity.Namespace = parts[2]
	}
	if len(parts) > 3 {
		identity.Name = parts[3]
	}
	return identity
}

func mergeManifestPatchValue(base, overlay interface{}) interface{} {
	switch overlayTyped := overlay.(type) {
	case map[string]interface{}:
		result := make(map[string]interface{}, len(overlayTyped))
		if baseTyped, ok := base.(map[string]interface{}); ok {
			for key, value := range baseTyped {
				result[key] = cloneManifestValue(value)
			}
		}
		for key, value := range overlayTyped {
			result[key] = mergeManifestPatchValue(result[key], value)
		}
		return result
	case []interface{}:
		if baseTyped, ok := base.([]interface{}); ok {
			if merged, handled := mergeManifestPatchList(baseTyped, overlayTyped); handled {
				return merged
			}
		}
		result := make([]interface{}, len(overlayTyped))
		for i, value := range overlayTyped {
			result[i] = mergeManifestPatchValue(nil, value)
		}
		return result
	default:
		return overlayTyped
	}
}

func mergeManifestPatchList(base, overlay []interface{}) ([]interface{}, bool) {
	matcher := manifestMergeKeyMatcherForList(base, overlay)
	if matcher == nil {
		return nil, false
	}

	result := make([]interface{}, len(base))
	basePositions := make(map[string]int, len(base))
	for i, item := range base {
		itemMap, ok := item.(map[string]interface{})
		if !ok {
			return nil, false
		}
		key, ok := matcher(itemMap)
		if !ok {
			return nil, false
		}
		result[i] = cloneManifestValue(item)
		basePositions[key] = i
	}

	for _, item := range overlay {
		itemMap, ok := item.(map[string]interface{})
		if !ok {
			return nil, false
		}
		key, ok := matcher(itemMap)
		if !ok {
			return nil, false
		}
		if position, exists := basePositions[key]; exists {
			result[position] = mergeManifestPatchValue(result[position], itemMap)
			continue
		}
		result = append(result, mergeManifestPatchValue(nil, itemMap))
		basePositions[key] = len(result) - 1
	}
	return result, true
}

type manifestMergeKeyMatcher func(item map[string]interface{}) (string, bool)

func manifestMergeKeyMatcherForList(base, overlay []interface{}) manifestMergeKeyMatcher {
	for _, matcher := range []manifestMergeKeyMatcher{
		manifestFieldKeyMatcher("name"),
		manifestFieldKeyMatcher("mountPath"),
		manifestPortKeyMatcher,
	} {
		if matcher == nil {
			continue
		}
		if manifestListMatches(base, matcher) && manifestListMatches(overlay, matcher) {
			return matcher
		}
	}
	return nil
}

func manifestListMatches(items []interface{}, matcher manifestMergeKeyMatcher) bool {
	for _, item := range items {
		itemMap, ok := item.(map[string]interface{})
		if !ok {
			return false
		}
		if _, ok := matcher(itemMap); !ok {
			return false
		}
	}
	return true
}

func manifestFieldKeyMatcher(field string) manifestMergeKeyMatcher {
	return func(item map[string]interface{}) (string, bool) {
		value, ok := item[field]
		if !ok {
			return "", false
		}
		return field + ":" + stringifyManifestMergeKey(value), true
	}
}

func manifestPortKeyMatcher(item map[string]interface{}) (string, bool) {
	if value, ok := item["containerPort"]; ok {
		key := stringifyManifestMergeKey(value)
		if protocol, ok := item["protocol"]; ok {
			key += "/" + stringifyManifestMergeKey(protocol)
		}
		return "containerPort:" + key, true
	}
	if value, ok := item["port"]; ok {
		key := stringifyManifestMergeKey(value)
		if protocol, ok := item["protocol"]; ok {
			key += "/" + stringifyManifestMergeKey(protocol)
		}
		return "port:" + key, true
	}
	if value, ok := item["name"]; ok {
		return "name:" + stringifyManifestMergeKey(value), true
	}
	return "", false
}

func stringifyManifestMergeKey(value interface{}) string {
	switch typed := value.(type) {
	case string:
		return typed
	case int:
		return fmt.Sprintf("%d", typed)
	case int32:
		return fmt.Sprintf("%d", typed)
	case int64:
		return fmt.Sprintf("%d", typed)
	case float64:
		return fmt.Sprintf("%.0f", typed)
	default:
		return fmt.Sprintf("%v", typed)
	}
}

func cloneManifestValue(value interface{}) interface{} {
	switch typed := value.(type) {
	case map[string]interface{}:
		cloned := make(map[string]interface{}, len(typed))
		for key, item := range typed {
			cloned[key] = cloneManifestValue(item)
		}
		return cloned
	case []interface{}:
		cloned := make([]interface{}, len(typed))
		for i, item := range typed {
			cloned[i] = cloneManifestValue(item)
		}
		return cloned
	default:
		return typed
	}
}
