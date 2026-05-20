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

package ownership

import (
	"fmt"
	"strings"
)

func ValidateLocalPatchOverlay(document map[string]interface{}) error {
	return ValidateLocalPatchOverlayWithPolicy(DefaultLocalPatchPolicy(), document)
}

func ValidateLocalPatchOverlayWithPolicy(policy LocalPatchPolicy, document map[string]interface{}) error {
	if err := policy.Validate(); err != nil {
		return fmt.Errorf("local patch policy: %w", err)
	}
	if len(document) == 0 {
		return fmt.Errorf("local patch document cannot be empty")
	}
	kind, _ := document["kind"].(string)
	if err := validateLocalPatchMap(policy, kind, nil, document); err != nil {
		return err
	}
	if !hasNonIdentityMutation(document) {
		return fmt.Errorf("local patch document must modify at least one non-identity field")
	}
	return nil
}

func ExtractAllowedLocalPatchOverlay(document map[string]interface{}) (map[string]interface{}, error) {
	return ExtractAllowedLocalPatchOverlayWithPolicy(DefaultLocalPatchPolicy(), document)
}

func ExtractAllowedLocalPatchOverlayWithPolicy(policy LocalPatchPolicy, document map[string]interface{}) (map[string]interface{}, error) {
	if err := policy.Validate(); err != nil {
		return nil, fmt.Errorf("local patch policy: %w", err)
	}
	if len(document) == 0 {
		return nil, fmt.Errorf("local patch source document cannot be empty")
	}

	kind, _ := document["kind"].(string)
	filtered, ok := filterAllowedLocalPatchValue(policy, kind, nil, document).(map[string]interface{})
	if !ok || len(filtered) == 0 {
		return nil, fmt.Errorf("local patch source document did not yield an allowed overlay")
	}
	if err := ValidateLocalPatchOverlayWithPolicy(policy, filtered); err != nil {
		return nil, err
	}
	return filtered, nil
}

func validateLocalPatchMap(policy LocalPatchPolicy, kind string, path []string, value map[string]interface{}) error {
	for key, child := range value {
		next := appendPath(path, key)
		if err := validateLocalPatchEntry(policy, kind, next, child); err != nil {
			return err
		}
	}
	return nil
}

func validateLocalPatchEntry(policy LocalPatchPolicy, kind string, path []string, value interface{}) error {
	if policy.IsForbidden(path) {
		return fmt.Errorf("local patch cannot modify %q", pathString(path))
	}
	if !isIdentityPath(path) && !policy.IsAllowed(kind, path) {
		return fmt.Errorf("local patch path %q is not allowed for kind %q", pathString(path), kind)
	}

	switch typed := value.(type) {
	case map[string]interface{}:
		return validateLocalPatchMap(policy, kind, path, typed)
	case []interface{}:
		for _, item := range typed {
			if nested, ok := item.(map[string]interface{}); ok {
				if err := validateLocalPatchMap(policy, kind, path, nested); err != nil {
					return err
				}
			}
		}
		return nil
	default:
		if isIdentityStringPath(path) {
			text, ok := value.(string)
			if !ok || strings.TrimSpace(text) == "" {
				return fmt.Errorf("%q must be a non-empty string", pathString(path))
			}
			return nil
		}
		if isMetadataStringMapValue(path) {
			if _, ok := value.(string); !ok {
				return fmt.Errorf("%q must be a string map value", pathString(path))
			}
		}
		return nil
	}
}

func hasNonIdentityMutation(document map[string]interface{}) bool {
	for key, value := range document {
		switch key {
		case "apiVersion", "kind":
			continue
		case "metadata":
			metadata, ok := value.(map[string]interface{})
			if !ok {
				return true
			}
			for metadataKey := range metadata {
				if metadataKey != "name" && metadataKey != "namespace" {
					return true
				}
			}
		default:
			return true
		}
	}
	return false
}

func containsContainerPath(path []string) bool {
	for _, segment := range path {
		switch segment {
		case "containers", "initContainers", "ephemeralContainers":
			return true
		}
	}
	return false
}

func templatePlacementPatchPrefixes(root string) []string {
	return []string{
		root + ".spec.nodeSelector",
		root + ".spec.tolerations",
		root + ".spec.affinity",
		root + ".spec.topologySpreadConstraints",
		root + ".spec.imagePullSecrets",
		root + ".spec.volumes.name",
		root + ".spec.volumes.secret.secretName",
		root + ".spec.containers.env.name",
		root + ".spec.containers.env.valueFrom.secretKeyRef.name",
		root + ".spec.containers.envFrom.secretRef.name",
		root + ".spec.initContainers.env.name",
		root + ".spec.initContainers.env.valueFrom.secretKeyRef.name",
		root + ".spec.initContainers.envFrom.secretRef.name",
	}
}

func isIdentityPath(path []string) bool {
	switch pathString(path) {
	case "apiVersion", "kind", "metadata", "metadata.name", "metadata.namespace":
		return true
	default:
		return false
	}
}

func isIdentityStringPath(path []string) bool {
	switch pathString(path) {
	case "apiVersion", "kind", "metadata.name", "metadata.namespace":
		return true
	default:
		return false
	}
}

func isMetadataStringMapValue(path []string) bool {
	return len(path) >= 3 && path[0] == "metadata" && (path[1] == "labels" || path[1] == "annotations")
}

func filterAllowedLocalPatchValue(policy LocalPatchPolicy, kind string, path []string, value interface{}) interface{} {
	switch typed := value.(type) {
	case map[string]interface{}:
		result := make(map[string]interface{})
		for key, child := range typed {
			next := appendPath(path, key)
			if !isIdentityPath(next) && policy.IsForbidden(next) {
				continue
			}
			if !isIdentityPath(next) && !policy.IsAllowed(kind, next) {
				continue
			}
			filtered := filterAllowedLocalPatchValue(policy, kind, next, child)
			if filtered == nil {
				continue
			}
			result[key] = filtered
		}
		if len(result) == 0 {
			return nil
		}
		return result
	case []interface{}:
		if !isIdentityPath(path) && !policy.IsAllowed(kind, path) {
			return nil
		}
		result := make([]interface{}, 0, len(typed))
		for _, item := range typed {
			if nested, ok := item.(map[string]interface{}); ok {
				filtered := filterAllowedLocalPatchValue(policy, kind, path, nested)
				if filtered == nil {
					continue
				}
				result = append(result, filtered)
				continue
			}
			result = append(result, item)
		}
		if len(result) == 0 {
			return nil
		}
		return result
	default:
		if isIdentityPath(path) || policy.IsAllowed(kind, path) {
			return typed
		}
		return nil
	}
}

func appendPath(path []string, segment string) []string {
	next := append([]string(nil), path...)
	return append(next, segment)
}

func pathString(path []string) string {
	return strings.Join(path, ".")
}
