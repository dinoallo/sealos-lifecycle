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
	"os"
	"path/filepath"
	"strings"
	"testing"

	yamlutil "github.com/labring/sealos/pkg/utils/yaml"
)

func TestDeriveDistributionFile(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	sourcePath := filepath.Join(root, "source", "bom.yaml")
	source := validBOM()
	source.Metadata.Labels = map[string]string{
		"distribution.sealos.io/profile": "default",
	}
	if err := yamlutil.MarshalFile(sourcePath, source); err != nil {
		t.Fatalf("MarshalFile(source) error = %v", err)
	}

	outRoot := filepath.Join(root, "release-source")
	result, err := DeriveDistributionFile(DeriveDistributionOptions{
		SourceBOMPath: sourcePath,
		OutputRoot:    outRoot,
		Line:          "corp-platform",
		Revision:      "rev-corp-001",
		Channel:       ChannelBeta,
		Labels: map[string]string{
			"distribution.sealos.io/profile": "corp",
		},
		Replacements: []ArtifactReplacement{
			{
				PackageName:  "calico",
				ArtifactName: "corp-calico-artifact",
				Image:        "registry.example.io/corp/calico:3.28.0-corp.1",
				Digest:       "sha256:dddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddd",
				Version:      "3.28.0-corp.1",
				SourcePath:   "packages/network/calico/corp",
				SourceDigest: "sha256:eeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeee",
			},
		},
	})
	if err != nil {
		t.Fatalf("DeriveDistributionFile() error = %v", err)
	}

	if got, want := result.SourceLine, "default-platform"; got != want {
		t.Fatalf("SourceLine = %q, want %q", got, want)
	}
	if got, want := result.SourceRevision, "rev-20240423"; got != want {
		t.Fatalf("SourceRevision = %q, want %q", got, want)
	}
	if got, want := result.BOMPath, filepath.Join(outRoot, "releases", "corp-platform", "rev-corp-001", "bom.yaml"); got != want {
		t.Fatalf("BOMPath = %q, want %q", got, want)
	}
	if got, want := result.ChannelPath, filepath.Join(outRoot, "channels", "corp-platform", "beta.yaml"); got != want {
		t.Fatalf("ChannelPath = %q, want %q", got, want)
	}
	if !strings.HasPrefix(result.BOMDigest, "sha256:") {
		t.Fatalf("BOMDigest = %q, want sha256 digest", result.BOMDigest)
	}

	derived, err := LoadFile(result.BOMPath)
	if err != nil {
		t.Fatalf("LoadFile(derived) error = %v", err)
	}
	if got, want := derived.Metadata.Name, "corp-platform"; got != want {
		t.Fatalf("derived metadata.name = %q, want %q", got, want)
	}
	if got, want := derived.Spec.Revision, "rev-corp-001"; got != want {
		t.Fatalf("derived revision = %q, want %q", got, want)
	}
	if got, want := derived.Metadata.Labels["distribution.sealos.io/derived-from-line"], "default-platform"; got != want {
		t.Fatalf("derived-from-line label = %q, want %q", got, want)
	}
	if got, want := derived.Metadata.Labels["distribution.sealos.io/derived-from-revision"], "rev-20240423"; got != want {
		t.Fatalf("derived-from-revision label = %q, want %q", got, want)
	}
	if got, want := derived.Metadata.Labels["distribution.sealos.io/profile"], "corp"; got != want {
		t.Fatalf("profile label = %q, want %q", got, want)
	}
	if got, want := derived.Spec.Packages[0].Version, "3.28.0-corp.1"; got != want {
		t.Fatalf("derived calico version = %q, want %q", got, want)
	}
	if got, want := derived.Spec.Packages[0].Artifact.Image, "registry.example.io/corp/calico:3.28.0-corp.1"; got != want {
		t.Fatalf("derived calico artifact image = %q, want %q", got, want)
	}
	if got, want := derived.Spec.Packages[1].Artifact.Image, source.Spec.Packages[1].Artifact.Image; got != want {
		t.Fatalf("unchanged ingress artifact image = %q, want %q", got, want)
	}

	channel, err := LoadReleaseChannelFile(result.ChannelPath)
	if err != nil {
		t.Fatalf("LoadReleaseChannelFile(derived) error = %v", err)
	}
	if got, want := channel.Spec.Distribution, "corp-platform"; got != want {
		t.Fatalf("channel distribution = %q, want %q", got, want)
	}
	if got, want := channel.Spec.Channel, ChannelBeta; got != want {
		t.Fatalf("channel = %q, want %q", got, want)
	}
	if got, want := channel.Spec.TargetRevision, "rev-corp-001"; got != want {
		t.Fatalf("channel targetRevision = %q, want %q", got, want)
	}
	if got, want := channel.Spec.BOMPath, "../../releases/corp-platform/rev-corp-001/bom.yaml"; got != want {
		t.Fatalf("channel bomPath = %q, want %q", got, want)
	}
	if got, want := channel.Spec.BOMDigest, result.BOMDigest; got != want {
		t.Fatalf("channel bomDigest = %q, want %q", got, want)
	}
	if got, want := len(result.Replacements), 1; got != want {
		t.Fatalf("len(Replacements) = %d, want %d", got, want)
	}
	if got, want := result.Replacements[0].VersionFrom, "3.28.0"; got != want {
		t.Fatalf("replacement VersionFrom = %q, want %q", got, want)
	}
	if got, want := result.Replacements[0].VersionTo, "3.28.0-corp.1"; got != want {
		t.Fatalf("replacement VersionTo = %q, want %q", got, want)
	}
}

func TestDeriveDistributionFileRejectsUnknownPackage(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	sourcePath := filepath.Join(root, "bom.yaml")
	if err := yamlutil.MarshalFile(sourcePath, validBOM()); err != nil {
		t.Fatalf("MarshalFile(source) error = %v", err)
	}

	_, err := DeriveDistributionFile(DeriveDistributionOptions{
		SourceBOMPath: sourcePath,
		OutputRoot:    filepath.Join(root, "out"),
		Line:          "corp-platform",
		Revision:      "rev-corp-001",
		Replacements: []ArtifactReplacement{
			{PackageName: "missing", Digest: "sha256:dddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddd"},
		},
	})
	if err == nil {
		t.Fatal("DeriveDistributionFile() error = nil, want error")
	}
	if !strings.Contains(err.Error(), "package \"missing\" not found") {
		t.Fatalf("DeriveDistributionFile() error = %v, want unknown package", err)
	}
	if _, statErr := os.Stat(filepath.Join(root, "out")); !os.IsNotExist(statErr) {
		t.Fatalf("output root exists after failed derive: %v", statErr)
	}
}

func TestDeriveDistributionFileRejectsPathEscapingLine(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	sourcePath := filepath.Join(root, "bom.yaml")
	if err := yamlutil.MarshalFile(sourcePath, validBOM()); err != nil {
		t.Fatalf("MarshalFile(source) error = %v", err)
	}

	_, err := DeriveDistributionFile(DeriveDistributionOptions{
		SourceBOMPath: sourcePath,
		OutputRoot:    filepath.Join(root, "out"),
		Line:          "../escape",
		Revision:      "rev-corp-001",
	})
	if err == nil {
		t.Fatal("DeriveDistributionFile() error = nil, want path segment error")
	}
	if !strings.Contains(err.Error(), "path separators") {
		t.Fatalf("DeriveDistributionFile() error = %v, want path separator rejection", err)
	}
}
