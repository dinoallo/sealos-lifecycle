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

package state

import (
	"fmt"
	"os"
	"path/filepath"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/labring/sealos/pkg/constants"
	yamlutil "github.com/labring/sealos/pkg/utils/yaml"
)

const (
	StoreDirName            = "distribution"
	AppliedRevisionFileName = "applied-revision.yaml"
)

func CurrentAppliedRevisionName(clusterName string) string {
	if clusterName == "" {
		return ""
	}
	return clusterName + "-current"
}

func AppliedRevisionPath(clusterName string) string {
	return filepath.Join(constants.NewPathResolver(clusterName).RunRoot(), StoreDirName, AppliedRevisionFileName)
}

func LoadAppliedRevision(clusterName string) (*AppliedRevision, error) {
	if clusterName == "" {
		return nil, fmt.Errorf("cluster name cannot be empty")
	}

	path := AppliedRevisionPath(clusterName)
	var doc AppliedRevision
	if err := yamlutil.UnmarshalFile(path, &doc); err != nil {
		return nil, fmt.Errorf("load applied revision %q: %w", path, err)
	}
	if err := doc.Validate(); err != nil {
		return nil, fmt.Errorf("validate applied revision %q: %w", path, err)
	}
	return &doc, nil
}

func SaveAppliedRevision(doc *AppliedRevision) error {
	if doc == nil {
		return fmt.Errorf("applied revision cannot be nil")
	}
	if err := doc.Validate(); err != nil {
		return fmt.Errorf("validate applied revision: %w", err)
	}

	path := AppliedRevisionPath(doc.Spec.ClusterName)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("create applied revision directory %q: %w", filepath.Dir(path), err)
	}
	if err := yamlutil.MarshalFile(path, doc); err != nil {
		return fmt.Errorf("write applied revision %q: %w", path, err)
	}
	return nil
}

func PersistSuccessfulApply(clusterName string, ref BOMReference, desiredStateDigest, localPatchRevision string) (*AppliedRevision, error) {
	doc := NewAppliedRevision(CurrentAppliedRevisionName(clusterName), clusterName, ref, desiredStateDigest)
	doc.Spec.LocalPatchRevision = localPatchRevision

	now := metav1.Now()
	doc.Status.State = StateClean
	doc.Status.LastAppliedTime = &now
	doc.Status.LastSuccessfulRevision = &RevisionSnapshot{
		BOM:                ref,
		LocalPatchRevision: localPatchRevision,
		DesiredStateDigest: desiredStateDigest,
	}
	doc.Status.Conditions = []Condition{
		NewCondition("Applied", corev1.ConditionTrue, "ReconcileSucceeded", "desired revision applied"),
	}

	if err := SaveAppliedRevision(doc); err != nil {
		return nil, err
	}
	return doc, nil
}
