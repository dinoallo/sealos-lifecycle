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
	"errors"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/labring/sealos/pkg/distribution/packageformat"
	"github.com/spf13/cobra"
	"sigs.k8s.io/yaml"
)

func TestSyncPackageBuildCmd(t *testing.T) {
	fixtureRoot, err := filepath.Abs(syncFixtureRoot())
	if err != nil {
		t.Fatalf("Abs() error = %v", err)
	}

	var invocations [][]string
	previousRunner := runSyncPackageSubcommand
	runSyncPackageSubcommand = func(args []string, _ io.Writer) error {
		invocations = append(invocations, slices.Clone(args))
		if len(args) > 0 && args[0] == "build" {
			contextDir := args[len(args)-1]
			for _, rel := range []string{
				"package.yaml",
				"rootfs/README",
				"manifests/bootstrap.yaml",
				"hooks/bootstrap.sh",
			} {
				if _, err := os.Stat(filepath.Join(contextDir, filepath.FromSlash(rel))); err != nil {
					t.Fatalf("staged context missing %q: %v", rel, err)
				}
			}
		}
		return nil
	}
	t.Cleanup(func() {
		runSyncPackageSubcommand = previousRunner
	})

	buf := bytes.NewBuffer(nil)
	cmd := newSyncCmd()
	cmd.SetOut(buf)
	cmd.SetErr(buf)
	cmd.SetArgs([]string{
		"package",
		"build",
		"--package-dir", fixtureRoot,
		"--image", "localhost:5000/test/kubernetes-rootfs:v1.30.3",
		"--distribution", "poc",
		"--label", "example=true",
	})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute() error = %v", err)
	}

	if got, want := len(invocations), 2; got != want {
		t.Fatalf("len(invocations) = %d, want %d", got, want)
	}
	if got, want := invocations[0][0], "build"; got != want {
		t.Fatalf("invocations[0][0] = %q, want %q", got, want)
	}
	if !slices.Contains(invocations[0], "--save-image=false") {
		t.Fatalf("build args = %v, want --save-image=false", invocations[0])
	}
	if got, want := invocations[1], []string{"inspect", "--type", "image", "localhost:5000/test/kubernetes-rootfs:v1.30.3"}; !slices.Equal(
		got,
		want,
	) {
		t.Fatalf("inspect args = %v, want %v", got, want)
	}

	var out syncPackageBuildOutput
	if err := yaml.Unmarshal(buf.Bytes(), &out); err != nil {
		t.Fatalf("Unmarshal() error = %v\noutput=%s", err, buf.String())
	}
	if got, want := out.PackageComponent, "kubernetes"; got != want {
		t.Fatalf("out.PackageComponent = %q, want %q", got, want)
	}
	if got, want := out.PackageClass, "rootfs"; got != want {
		t.Fatalf("out.PackageClass = %q, want %q", got, want)
	}
}

func TestSyncPackageInspectCmd(t *testing.T) {
	fixtureRoot, err := filepath.Abs(syncFixtureRoot())
	if err != nil {
		t.Fatalf("Abs() error = %v", err)
	}

	buf := bytes.NewBuffer(nil)
	cmd := newSyncCmd()
	cmd.SetOut(buf)
	cmd.SetErr(buf)
	cmd.SetArgs([]string{
		"package",
		"inspect",
		"--package-dir", fixtureRoot,
	})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute() error = %v", err)
	}

	var out syncPackageBuildOutput
	if err := yaml.Unmarshal(buf.Bytes(), &out); err != nil {
		t.Fatalf("Unmarshal() error = %v\noutput=%s", err, buf.String())
	}
	if got, want := out.PackageName, "kubernetes-rootfs"; got != want {
		t.Fatalf("out.PackageName = %q, want %q", got, want)
	}
	if got, want := out.PackageVersion, "v1.30.3"; got != want {
		t.Fatalf("out.PackageVersion = %q, want %q", got, want)
	}
}

func TestSyncPackagePushCmd(t *testing.T) {
	const digest = "sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	previousRunner := runSyncPackageSubcommand
	runSyncPackageSubcommand = func(args []string, _ io.Writer) error {
		for i := 0; i < len(args); i++ {
			if args[i] == "--digestfile" {
				if err := os.WriteFile(args[i+1], []byte(digest+"\n"), 0o644); err != nil {
					t.Fatalf("WriteFile() error = %v", err)
				}
				break
			}
		}
		return nil
	}
	t.Cleanup(func() {
		runSyncPackageSubcommand = previousRunner
	})

	buf := bytes.NewBuffer(nil)
	cmd := newSyncCmd()
	cmd.SetOut(buf)
	cmd.SetErr(buf)
	cmd.SetArgs([]string{
		"package",
		"push",
		"--image", "localhost:5000/test/kubernetes-rootfs:v1.30.3",
		"--destination", "registry.example.io/sealos/kubernetes-rootfs:v1.30.3",
	})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute() error = %v", err)
	}

	var out syncPackagePushOutput
	if err := yaml.Unmarshal(buf.Bytes(), &out); err != nil {
		t.Fatalf("Unmarshal() error = %v\noutput=%s", err, buf.String())
	}
	if got, want := out.Destination, "docker://registry.example.io/sealos/kubernetes-rootfs:v1.30.3"; got != want {
		t.Fatalf("out.Destination = %q, want %q", got, want)
	}
	if got, want := out.Image, "registry.example.io/sealos/kubernetes-rootfs:v1.30.3"; got != want {
		t.Fatalf("out.Image = %q, want %q", got, want)
	}
	if got, want := out.Digest, digest; got != want {
		t.Fatalf("out.Digest = %q, want %q", got, want)
	}
	if got, want := out.Reference, "registry.example.io/sealos/kubernetes-rootfs:v1.30.3@"+digest; got != want {
		t.Fatalf("out.Reference = %q, want %q", got, want)
	}
	if got, want := out.Provenance.Transport, "docker"; got != want {
		t.Fatalf("out.Provenance.Transport = %q, want %q", got, want)
	}
	if got, want := out.Provenance.DigestAlgorithm, "sha256"; got != want {
		t.Fatalf("out.Provenance.DigestAlgorithm = %q, want %q", got, want)
	}
	if got, want := out.Provenance.DigestEncoded, strings.Repeat("a", 64); got != want {
		t.Fatalf("out.Provenance.DigestEncoded = %q, want %q", got, want)
	}
	if got := out.Provenance.AuthMode; got.Authfile || got.CertDir || got.Creds {
		t.Fatalf("out.Provenance.AuthMode = %+v, want all false", got)
	}
}

func TestSyncPackagePushCmdWritesProvenanceWithoutLeakingCreds(t *testing.T) {
	const (
		digest = "sha256:bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"
		creds  = "user:example"
	)
	var pushArgs []string
	previousRunner := runSyncPackageSubcommand
	runSyncPackageSubcommand = func(args []string, _ io.Writer) error {
		pushArgs = slices.Clone(args)
		for i := range args {
			if args[i] == "--digestfile" {
				if err := os.WriteFile(args[i+1], []byte(digest+"\n"), 0o644); err != nil {
					t.Fatalf("WriteFile() error = %v", err)
				}
				break
			}
		}
		return nil
	}
	t.Cleanup(func() {
		runSyncPackageSubcommand = previousRunner
	})

	provenanceFile := filepath.Join(t.TempDir(), "push", "provenance.yaml")
	buf := bytes.NewBuffer(nil)
	cmd := newSyncCmd()
	cmd.SetOut(buf)
	cmd.SetErr(buf)
	cmd.SetArgs([]string{
		"package",
		"push",
		"--image", "localhost:5000/test/kubernetes-rootfs:v1.30.3",
		"--destination", "registry.example.io/sealos/kubernetes-rootfs:v1.30.3",
		"--authfile", "/tmp/auth.json",
		"--cert-dir", "/tmp/certs",
		"--creds", creds,
		"--provenance-file", provenanceFile,
	})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute() error = %v", err)
	}
	if !slices.Contains(pushArgs, "--creds") {
		t.Fatalf("push args = %v, want --creds forwarded", pushArgs)
	}
	if strings.Contains(buf.String(), creds) {
		t.Fatalf("stdout leaked credentials: %s", buf.String())
	}

	var out syncPackagePushOutput
	if err := yaml.Unmarshal(buf.Bytes(), &out); err != nil {
		t.Fatalf("Unmarshal() error = %v\noutput=%s", err, buf.String())
	}
	if got, want := out.ProvenanceFile, provenanceFile; got != want {
		t.Fatalf("out.ProvenanceFile = %q, want %q", got, want)
	}
	if got := out.Provenance.AuthMode; !got.Authfile || !got.CertDir || !got.Creds {
		t.Fatalf("out.Provenance.AuthMode = %+v, want all true", got)
	}

	provenanceBytes, err := os.ReadFile(provenanceFile)
	if err != nil {
		t.Fatalf("ReadFile() error = %v", err)
	}
	if strings.Contains(string(provenanceBytes), creds) {
		t.Fatalf("provenance leaked credentials: %s", string(provenanceBytes))
	}
	var provenance syncPackagePushOutput
	if err := yaml.Unmarshal(provenanceBytes, &provenance); err != nil {
		t.Fatalf("Unmarshal(provenance) error = %v\noutput=%s", err, string(provenanceBytes))
	}
	if got, want := provenance.Reference, out.Reference; got != want {
		t.Fatalf("provenance.Reference = %q, want %q", got, want)
	}
}

func TestSyncPackagePushCmdRejectsInvalidDigest(t *testing.T) {
	previousRunner := runSyncPackageSubcommand
	runSyncPackageSubcommand = func(args []string, _ io.Writer) error {
		for i := range args {
			if args[i] == "--digestfile" {
				if err := os.WriteFile(args[i+1], []byte("not-a-digest\n"), 0o644); err != nil {
					t.Fatalf("WriteFile() error = %v", err)
				}
				break
			}
		}
		return nil
	}
	t.Cleanup(func() {
		runSyncPackageSubcommand = previousRunner
	})

	cmd := newSyncCmd()
	cmd.SetOut(bytes.NewBuffer(nil))
	cmd.SetErr(bytes.NewBuffer(nil))
	cmd.SetArgs([]string{
		"package",
		"push",
		"--image", "localhost:5000/test/kubernetes-rootfs:v1.30.3",
		"--destination", "registry.example.io/sealos/kubernetes-rootfs:v1.30.3",
	})

	err := cmd.Execute()
	if err == nil {
		t.Fatal("Execute() error = nil, want invalid digest error")
	}
	if !strings.Contains(err.Error(), "parse pushed digest") {
		t.Fatalf("Execute() error = %v, want parse pushed digest", err)
	}
}

func TestSyncPackagePushCmdFailureDiagnosticsRedactCreds(t *testing.T) {
	const creds = "user:example"
	previousRunner := runSyncPackageSubcommand
	runSyncPackageSubcommand = func(_ []string, _ io.Writer) error {
		return errors.New("registry unavailable")
	}
	t.Cleanup(func() {
		runSyncPackageSubcommand = previousRunner
	})

	cmd := newSyncCmd()
	cmd.SetOut(bytes.NewBuffer(nil))
	cmd.SetErr(bytes.NewBuffer(nil))
	cmd.SetArgs([]string{
		"package",
		"push",
		"--image", "localhost:5000/test/kubernetes-rootfs:v1.30.3",
		"--destination", "registry.example.io/sealos/kubernetes-rootfs:v1.30.3",
		"--creds", creds,
	})

	err := cmd.Execute()
	if err == nil {
		t.Fatal("Execute() error = nil, want push failure")
	}
	if strings.Contains(err.Error(), creds) {
		t.Fatalf("Execute() error leaked credentials: %v", err)
	}
	if !strings.Contains(
		err.Error(),
		"registry diagnostics: authfile=false cert-dir=false creds=true",
	) {
		t.Fatalf("Execute() error = %v, want redacted registry diagnostics", err)
	}
}

func TestSyncPackagePullCmd(t *testing.T) {
	fixtureRoot, err := filepath.Abs(syncFixtureRoot())
	if err != nil {
		t.Fatalf("Abs() error = %v", err)
	}
	mounter := &fakeSyncPackageImageMounter{
		mounts: map[string]packageformat.MountedImage{
			"registry.example.io/sealos/kubernetes-rootfs:v1.30.3": {
				Name:       "mounted-package",
				MountPoint: fixtureRoot,
			},
		},
	}
	previousMounterFactory := newSyncPackageImageMounter
	newSyncPackageImageMounter = func(id string) (packageformat.ImageMounter, error) {
		if got, want := id, "sync-package-pull"; got != want {
			t.Fatalf("mounter id = %q, want %q", got, want)
		}
		return mounter, nil
	}
	t.Cleanup(func() {
		newSyncPackageImageMounter = previousMounterFactory
	})

	outputDir := filepath.Join(t.TempDir(), "pulled-package")
	buf := bytes.NewBuffer(nil)
	cmd := newSyncCmd()
	cmd.SetOut(buf)
	cmd.SetErr(buf)
	cmd.SetArgs([]string{
		"package",
		"pull",
		"--image", "registry.example.io/sealos/kubernetes-rootfs:v1.30.3",
		"--output-dir", outputDir,
	})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute() error = %v", err)
	}

	var out syncPackagePullOutput
	if err := yaml.Unmarshal(buf.Bytes(), &out); err != nil {
		t.Fatalf("Unmarshal() error = %v\noutput=%s", err, buf.String())
	}
	if got, want := out.OutputDir, outputDir; got != want {
		t.Fatalf("out.OutputDir = %q, want %q", got, want)
	}
	if got, want := out.PackageComponent, "kubernetes"; got != want {
		t.Fatalf("out.PackageComponent = %q, want %q", got, want)
	}
	if _, err := os.Stat(filepath.Join(outputDir, "package.yaml")); err != nil {
		t.Fatalf("pulled package manifest missing: %v", err)
	}
	if got, want := mounter.unmounted[0], "mounted-package"; got != want {
		t.Fatalf("unmounted = %q, want %q", got, want)
	}
}

type fakeSyncPackageImageMounter struct {
	mounts    map[string]packageformat.MountedImage
	unmounted []string
}

func (m *fakeSyncPackageImageMounter) Mount(image string) (packageformat.MountedImage, error) {
	info, ok := m.mounts[image]
	if !ok {
		return packageformat.MountedImage{}, os.ErrNotExist
	}
	return info, nil
}

func (m *fakeSyncPackageImageMounter) Unmount(name string) error {
	m.unmounted = append(m.unmounted, name)
	return nil
}

func TestSyncPackageRootArgs(t *testing.T) {
	root := &cobra.Command{Use: "sealos"}
	root.PersistentFlags().Bool("debug", false, "")
	root.PersistentFlags().StringSlice("storage-opt", nil, "")

	child := &cobra.Command{Use: "package"}
	root.AddCommand(child)

	if err := root.PersistentFlags().Set("debug", "true"); err != nil {
		t.Fatalf("Set(debug) error = %v", err)
	}
	if err := root.PersistentFlags().Set("storage-opt", "overlay.mount_program=/usr/bin/fuse-overlayfs"); err != nil {
		t.Fatalf("Set(storage-opt) error = %v", err)
	}

	got, err := syncPackageRootArgs(child)
	if err != nil {
		t.Fatalf("syncPackageRootArgs() error = %v", err)
	}
	if !slices.Contains(got, "--debug=true") {
		t.Fatalf("root args = %v, want --debug=true", got)
	}
	if !slices.Contains(got, "--storage-opt=overlay.mount_program=/usr/bin/fuse-overlayfs") {
		t.Fatalf("root args = %v, want forwarded storage-opt", got)
	}
}
