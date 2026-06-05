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

package bom

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	yamlutil "github.com/labring/sealos/pkg/utils/yaml"
	"github.com/opencontainers/go-digest"
)

type DeriveDistributionOptions struct {
	SourceBOMPath string
	OutputRoot    string
	Line          string
	Revision      string
	Channel       ReleaseChannel
	ChannelName   string
	Labels        map[string]string
	Replacements  []ArtifactReplacement
}

type ArtifactReplacement struct {
	PackageName  string
	ArtifactName string
	Image        string
	Digest       string
	Version      string
	SourcePath   string
	SourceDigest string
}

type DeriveDistributionResult struct {
	SourceBOMPath  string
	SourceLine     string
	SourceRevision string
	OutputRoot     string
	BOM            *BOM
	BOMPath        string
	BOMDigest      string
	Channel        *ReleaseChannelDocument
	ChannelPath    string
	Replacements   []ArtifactReplacementResult
}

type ArtifactReplacementResult struct {
	PackageName string            `json:"packageName"           yaml:"packageName"`
	Before      ArtifactReference `json:"before"                yaml:"before"`
	After       ArtifactReference `json:"after"                 yaml:"after"`
	VersionFrom string            `json:"versionFrom,omitempty" yaml:"versionFrom,omitempty"`
	VersionTo   string            `json:"versionTo,omitempty"   yaml:"versionTo,omitempty"`
}

func DeriveDistributionFile(opts DeriveDistributionOptions) (*DeriveDistributionResult, error) {
	sourceBOMPath := strings.TrimSpace(opts.SourceBOMPath)
	if sourceBOMPath == "" {
		return nil, errors.New("source BOM path cannot be empty")
	}
	outputRoot := strings.TrimSpace(opts.OutputRoot)
	if outputRoot == "" {
		return nil, errors.New("output root cannot be empty")
	}
	line := strings.TrimSpace(opts.Line)
	if line == "" {
		return nil, errors.New("derived distribution line cannot be empty")
	}
	if err := validateReleaseStorePathSegment("derived distribution line", line); err != nil {
		return nil, err
	}
	revision := strings.TrimSpace(opts.Revision)
	if revision == "" {
		return nil, errors.New("derived revision cannot be empty")
	}
	if err := validateReleaseStorePathSegment("derived revision", revision); err != nil {
		return nil, err
	}
	channel := opts.Channel
	if channel == "" {
		channel = ChannelAlpha
	}
	if err := channel.ValidateRequired(); err != nil {
		return nil, fmt.Errorf("channel: %w", err)
	}

	source, err := LoadFile(sourceBOMPath)
	if err != nil {
		return nil, err
	}
	derived := cloneBOM(source)
	derived.Metadata.Name = line
	derived.Spec.Revision = revision
	derived.RuntimeChannel = channel
	derived.Metadata.Labels = mergeDerivedLabels(
		source.Metadata.Labels,
		opts.Labels,
		source.Metadata.Name,
		source.Spec.Revision,
	)

	replacements, err := applyArtifactReplacements(&derived, opts.Replacements)
	if err != nil {
		return nil, err
	}
	if err := derived.Validate(); err != nil {
		return nil, fmt.Errorf("validate derived BOM: %w", err)
	}

	bomPath := filepath.Join(outputRoot, "releases", line, revision, "bom.yaml")
	if err := yamlutil.MarshalFile(bomPath, &derived); err != nil {
		return nil, fmt.Errorf("write derived BOM %q: %w", bomPath, err)
	}
	bomData, err := os.ReadFile(bomPath)
	if err != nil {
		return nil, fmt.Errorf("read derived BOM %q: %w", bomPath, err)
	}
	bomDigest := digest.Canonical.FromBytes(bomData).String()

	channelName := strings.TrimSpace(opts.ChannelName)
	if channelName == "" {
		channelName = line + "-" + string(channel)
	}
	channelPath := filepath.Join(outputRoot, "channels", line, string(channel)+".yaml")
	bomPathForChannel := releaseChannelRelativePath(channelPath, bomPath)
	channelDoc := NewReleaseChannel(channelName, line, channel, revision, bomPathForChannel)
	channelDoc.Spec.BOMDigest = bomDigest
	if err := channelDoc.Validate(); err != nil {
		return nil, fmt.Errorf("validate derived release channel: %w", err)
	}
	if err := yamlutil.MarshalFile(channelPath, channelDoc); err != nil {
		return nil, fmt.Errorf("write derived release channel %q: %w", channelPath, err)
	}

	return &DeriveDistributionResult{
		SourceBOMPath:  sourceBOMPath,
		SourceLine:     source.Metadata.Name,
		SourceRevision: source.Spec.Revision,
		OutputRoot:     outputRoot,
		BOM:            &derived,
		BOMPath:        bomPath,
		BOMDigest:      bomDigest,
		Channel:        channelDoc,
		ChannelPath:    channelPath,
		Replacements:   replacements,
	}, nil
}

func cloneBOM(source *BOM) BOM {
	if source == nil {
		return BOM{}
	}
	clone := *source
	clone.Metadata.Labels = cloneStringMap(source.Metadata.Labels)
	clone.Spec.BaseArtifacts = append([]ArtifactReference(nil), source.Spec.BaseArtifacts...)
	clone.Spec.Packages = make([]Package, len(source.Spec.Packages))
	for i, pkg := range source.Spec.Packages {
		clone.Spec.Packages[i] = pkg
		clone.Spec.Packages[i].Dependencies = append([]string(nil), pkg.Dependencies...)
	}
	return clone
}

func cloneStringMap(source map[string]string) map[string]string {
	if len(source) == 0 {
		return nil
	}
	clone := make(map[string]string, len(source))
	for key, value := range source {
		clone[key] = value
	}
	return clone
}

func mergeDerivedLabels(
	source, override map[string]string,
	sourceLine, sourceRevision string,
) map[string]string {
	labels := cloneStringMap(source)
	if labels == nil {
		labels = make(map[string]string)
	}
	labels["distribution.sealos.io/derived-from-line"] = sourceLine
	labels["distribution.sealos.io/derived-from-revision"] = sourceRevision
	keys := make([]string, 0, len(override))
	for key := range override {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, key := range keys {
		value := strings.TrimSpace(override[key])
		if value == "" {
			delete(labels, key)
			continue
		}
		labels[key] = value
	}
	return labels
}

func validateReleaseStorePathSegment(field, value string) error {
	if value == "." || value == ".." {
		return fmt.Errorf("%s cannot be %q", field, value)
	}
	if strings.Contains(value, "/") || strings.Contains(value, string(filepath.Separator)) {
		return fmt.Errorf("%s cannot contain path separators", field)
	}
	return nil
}

func applyArtifactReplacements(
	doc *BOM,
	replacements []ArtifactReplacement,
) ([]ArtifactReplacementResult, error) {
	if doc == nil {
		return nil, errors.New("BOM cannot be nil")
	}
	results := make([]ArtifactReplacementResult, 0, len(replacements))
	seen := make(map[string]struct{}, len(replacements))
	for i, replacement := range replacements {
		packageName := strings.TrimSpace(replacement.PackageName)
		if packageName == "" {
			return nil, fmt.Errorf("replacements[%d]: package name cannot be empty", i)
		}
		if _, ok := seen[packageName]; ok {
			return nil, fmt.Errorf("replacements[%d]: duplicate package %q", i, packageName)
		}
		seen[packageName] = struct{}{}

		packageIndex := -1
		for j, pkg := range doc.Spec.Packages {
			if pkg.Name == packageName {
				packageIndex = j
				break
			}
		}
		if packageIndex < 0 {
			return nil, fmt.Errorf(
				"replacements[%d]: package %q not found in source BOM",
				i,
				packageName,
			)
		}
		pkg := &doc.Spec.Packages[packageIndex]
		before := pkg.Artifact
		versionFrom := pkg.Version
		after := before
		if value := strings.TrimSpace(replacement.ArtifactName); value != "" {
			after.Name = value
		}
		if value := strings.TrimSpace(replacement.Image); value != "" {
			after.Image = value
		}
		if value := strings.TrimSpace(replacement.Digest); value != "" {
			after.Digest = value
		}
		if err := after.Validate(); err != nil {
			return nil, fmt.Errorf("replacements[%d]: artifact: %w", i, err)
		}
		if value := strings.TrimSpace(replacement.Version); value != "" {
			pkg.Version = value
		}
		if value := strings.TrimSpace(replacement.SourcePath); value != "" {
			pkg.Source.Path = value
		}
		if value := strings.TrimSpace(replacement.SourceDigest); value != "" {
			pkg.Source.Digest = value
		}
		pkg.Artifact = after
		results = append(results, ArtifactReplacementResult{
			PackageName: packageName,
			Before:      before,
			After:       after,
			VersionFrom: versionFrom,
			VersionTo:   pkg.Version,
		})
	}
	return results, nil
}
