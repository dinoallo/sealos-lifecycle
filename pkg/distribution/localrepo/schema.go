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

package localrepo

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/opencontainers/go-digest"
	"sigs.k8s.io/yaml"

	"github.com/labring/sealos/pkg/distribution"
)

const (
	RepoFileName            = "repo.yaml"
	RevisionsDirName        = "revisions"
	CurrentRevisionFileName = "current.yaml"
)

type Metadata struct {
	Name   string            `json:"name" yaml:"name"`
	Labels map[string]string `json:"labels,omitempty" yaml:"labels,omitempty"`
}

type BOMReference struct {
	Name     string `json:"name" yaml:"name"`
	Revision string `json:"revision" yaml:"revision"`
	Digest   string `json:"digest,omitempty" yaml:"digest,omitempty"`
}

type LocalRepoSpec struct {
	Cluster          string `json:"cluster" yaml:"cluster"`
	DistributionLine string `json:"distributionLine" yaml:"distributionLine"`
	Channel          string `json:"channel,omitempty" yaml:"channel,omitempty"`
	BOM              string `json:"bom,omitempty" yaml:"bom,omitempty"`
	BOMRevision      string `json:"bomRevision,omitempty" yaml:"bomRevision,omitempty"`
}

type LocalRepoDocument struct {
	APIVersion string        `json:"apiVersion" yaml:"apiVersion"`
	Kind       string        `json:"kind" yaml:"kind"`
	Metadata   Metadata      `json:"metadata" yaml:"metadata"`
	Spec       LocalRepoSpec `json:"spec" yaml:"spec"`
}

type LocalRepoRevisionSpec struct {
	Cluster            string       `json:"cluster" yaml:"cluster"`
	DistributionLine   string       `json:"distributionLine" yaml:"distributionLine"`
	Channel            string       `json:"channel,omitempty" yaml:"channel,omitempty"`
	BOM                BOMReference `json:"bom" yaml:"bom"`
	LocalInputRevision string       `json:"localInputRevision" yaml:"localInputRevision"`
	Digest             string       `json:"digest" yaml:"digest"`
	Audit              AuditFields  `json:"audit" yaml:"audit"`
}

type AuditFields struct {
	CreatedAt string `json:"createdAt" yaml:"createdAt"`
	CreatedBy string `json:"createdBy,omitempty" yaml:"createdBy,omitempty"`
	Command   string `json:"command,omitempty" yaml:"command,omitempty"`
}

type LocalRepoRevisionDocument struct {
	APIVersion string                `json:"apiVersion" yaml:"apiVersion"`
	Kind       string                `json:"kind" yaml:"kind"`
	Metadata   Metadata              `json:"metadata" yaml:"metadata"`
	Spec       LocalRepoRevisionSpec `json:"spec" yaml:"spec"`
}

func NewDocument(name, cluster, distributionLine, channel, bomName, bomRevision string) *LocalRepoDocument {
	return &LocalRepoDocument{
		APIVersion: distribution.APIVersion,
		Kind:       distribution.KindLocalRepo,
		Metadata: Metadata{
			Name: strings.TrimSpace(name),
		},
		Spec: LocalRepoSpec{
			Cluster:          strings.TrimSpace(cluster),
			DistributionLine: strings.TrimSpace(distributionLine),
			Channel:          strings.TrimSpace(channel),
			BOM:              strings.TrimSpace(bomName),
			BOMRevision:      strings.TrimSpace(bomRevision),
		},
	}
}

func NewRevisionDocument(name string, spec LocalRepoRevisionSpec) *LocalRepoRevisionDocument {
	return &LocalRepoRevisionDocument{
		APIVersion: distribution.APIVersion,
		Kind:       distribution.KindLocalRepoRevision,
		Metadata: Metadata{
			Name: strings.TrimSpace(name),
		},
		Spec: spec,
	}
}

func (d LocalRepoDocument) Validate() error {
	if strings.TrimSpace(d.APIVersion) != distribution.APIVersion {
		return fmt.Errorf("apiVersion must be %q", distribution.APIVersion)
	}
	if strings.TrimSpace(d.Kind) != distribution.KindLocalRepo {
		return fmt.Errorf("kind must be %q", distribution.KindLocalRepo)
	}
	if strings.TrimSpace(d.Metadata.Name) == "" {
		return fmt.Errorf("metadata.name cannot be empty")
	}
	if strings.TrimSpace(d.Spec.Cluster) == "" {
		return fmt.Errorf("spec.cluster cannot be empty")
	}
	if strings.TrimSpace(d.Spec.DistributionLine) == "" {
		return fmt.Errorf("spec.distributionLine cannot be empty")
	}
	return nil
}

func (d LocalRepoRevisionDocument) Validate() error {
	if strings.TrimSpace(d.APIVersion) != distribution.APIVersion {
		return fmt.Errorf("apiVersion must be %q", distribution.APIVersion)
	}
	if strings.TrimSpace(d.Kind) != distribution.KindLocalRepoRevision {
		return fmt.Errorf("kind must be %q", distribution.KindLocalRepoRevision)
	}
	if strings.TrimSpace(d.Metadata.Name) == "" {
		return fmt.Errorf("metadata.name cannot be empty")
	}
	if strings.TrimSpace(d.Spec.Cluster) == "" {
		return fmt.Errorf("spec.cluster cannot be empty")
	}
	if strings.TrimSpace(d.Spec.DistributionLine) == "" {
		return fmt.Errorf("spec.distributionLine cannot be empty")
	}
	if strings.TrimSpace(d.Spec.BOM.Name) == "" {
		return fmt.Errorf("spec.bom.name cannot be empty")
	}
	if strings.TrimSpace(d.Spec.BOM.Revision) == "" {
		return fmt.Errorf("spec.bom.revision cannot be empty")
	}
	if err := validateDigest("spec.bom.digest", d.Spec.BOM.Digest, true); err != nil {
		return err
	}
	if err := validateDigest("spec.localInputRevision", d.Spec.LocalInputRevision, false); err != nil {
		return err
	}
	if err := validateDigest("spec.digest", d.Spec.Digest, false); err != nil {
		return err
	}
	if strings.TrimSpace(d.Spec.Audit.CreatedAt) == "" {
		return fmt.Errorf("spec.audit.createdAt cannot be empty")
	}
	if _, err := time.Parse(time.RFC3339, strings.TrimSpace(d.Spec.Audit.CreatedAt)); err != nil {
		return fmt.Errorf("spec.audit.createdAt must be RFC3339: %w", err)
	}
	return nil
}

func LoadDocument(root string) (*LocalRepoDocument, error) {
	path := filepath.Join(root, RepoFileName)
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read local repo metadata %q: %w", path, err)
	}
	var doc LocalRepoDocument
	if err := yaml.Unmarshal(data, &doc); err != nil {
		return nil, fmt.Errorf("unmarshal local repo metadata %q: %w", path, err)
	}
	if err := doc.Validate(); err != nil {
		return nil, fmt.Errorf("validate local repo metadata %q: %w", path, err)
	}
	return &doc, nil
}

func IsSchemaValidationError(err error) bool {
	if err == nil {
		return false
	}
	message := err.Error()
	return strings.Contains(message, "validate local repo metadata") ||
		strings.Contains(message, "validate local repo revision")
}

func LoadCurrentRevisionDocument(root string) (*LocalRepoRevisionDocument, error) {
	path := filepath.Join(root, RevisionsDirName, CurrentRevisionFileName)
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read local repo revision %q: %w", path, err)
	}
	var doc LocalRepoRevisionDocument
	if err := yaml.Unmarshal(data, &doc); err != nil {
		return nil, fmt.Errorf("unmarshal local repo revision %q: %w", path, err)
	}
	if err := doc.Validate(); err != nil {
		return nil, fmt.Errorf("validate local repo revision %q: %w", path, err)
	}
	return &doc, nil
}

func validateDigest(field, value string, optional bool) error {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		if optional {
			return nil
		}
		return fmt.Errorf("%s cannot be empty", field)
	}
	if _, err := digest.Parse(trimmed); err != nil {
		return fmt.Errorf("%s invalid digest %q: %w", field, trimmed, err)
	}
	return nil
}
