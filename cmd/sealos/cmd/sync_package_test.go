package cmd

import (
	"bytes"
	"io"
	"os"
	"path/filepath"
	"slices"
	"testing"

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
	if got, want := invocations[1], []string{"inspect", "--type", "image", "localhost:5000/test/kubernetes-rootfs:v1.30.3"}; !slices.Equal(got, want) {
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
	previousRunner := runSyncPackageSubcommand
	runSyncPackageSubcommand = func(args []string, _ io.Writer) error {
		for i := 0; i < len(args); i++ {
			if args[i] == "--digestfile" {
				if err := os.WriteFile(args[i+1], []byte("sha256:1234\n"), 0o644); err != nil {
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
	if got, want := out.Digest, "sha256:1234"; got != want {
		t.Fatalf("out.Digest = %q, want %q", got, want)
	}
	if got, want := out.Reference, "registry.example.io/sealos/kubernetes-rootfs:v1.30.3@sha256:1234"; got != want {
		t.Fatalf("out.Reference = %q, want %q", got, want)
	}
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
