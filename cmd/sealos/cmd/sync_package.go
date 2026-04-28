package cmd

import (
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
	"sigs.k8s.io/yaml"

	"github.com/labring/sealos/pkg/distribution/ocipackage"
)

type syncPackageCommandRunner func(args []string, stderr io.Writer) error

var runSyncPackageSubcommand syncPackageCommandRunner = defaultSyncPackageCommandRunner

type syncPackageBuildOutput struct {
	PackageDir       string `json:"packageDir" yaml:"packageDir"`
	Image            string `json:"image" yaml:"image"`
	PackageName      string `json:"packageName" yaml:"packageName"`
	PackageComponent string `json:"packageComponent" yaml:"packageComponent"`
	PackageVersion   string `json:"packageVersion" yaml:"packageVersion"`
	PackageClass     string `json:"packageClass" yaml:"packageClass"`
	Platform         string `json:"platform" yaml:"platform"`
}

type syncPackagePushOutput struct {
	SourceImage string `json:"sourceImage" yaml:"sourceImage"`
	Destination string `json:"destination" yaml:"destination"`
	Image       string `json:"image" yaml:"image"`
	Digest      string `json:"digest" yaml:"digest"`
	Reference   string `json:"reference" yaml:"reference"`
}

func newSyncPackageCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "package",
		Short: "Build and push OCI component package images",
		Args:  cobra.NoArgs,
	}
	cmd.AddCommand(newSyncPackageInspectCmd())
	cmd.AddCommand(newSyncPackageBuildCmd())
	cmd.AddCommand(newSyncPackagePushCmd())
	return cmd
}

func newSyncPackageInspectCmd() *cobra.Command {
	var flags struct {
		packageDir string
		output     string
	}

	cmd := &cobra.Command{
		Use:   "inspect",
		Short: "Inspect component package metadata and manifest-referenced paths",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			meta, err := ocipackage.LoadMetadata(flags.packageDir)
			if err != nil {
				return err
			}
			out := syncPackageBuildOutput{
				PackageDir:       meta.PackageDir,
				PackageName:      meta.Name,
				PackageComponent: meta.Component,
				PackageVersion:   meta.Version,
				PackageClass:     string(meta.Class),
			}
			return writeSyncPackageOutput(cmd, flags.output, out, [][2]string{
				{"package_dir", out.PackageDir},
				{"package_name", out.PackageName},
				{"package_component", out.PackageComponent},
				{"package_version", out.PackageVersion},
				{"package_class", out.PackageClass},
			})
		},
	}

	cmd.Flags().StringVar(&flags.packageDir, "package-dir", "", "component package directory to inspect")
	cmd.Flags().StringVar(&flags.output, "output", "yaml", "output format: yaml or env")
	mustMarkFlagRequired(cmd, "package-dir")
	return cmd
}

func newSyncPackageBuildCmd() *cobra.Command {
	var flags struct {
		packageDir   string
		image        string
		platform     string
		timestamp    int64
		distribution string
		labels       []string
		output       string
	}

	cmd := &cobra.Command{
		Use:   "build",
		Short: "Build an OCI image from a component package directory",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			tmpDir, err := os.MkdirTemp("", "sealos-sync-package-build-")
			if err != nil {
				return fmt.Errorf("create build tempdir: %w", err)
			}
			defer os.RemoveAll(tmpDir)

			contextDir := filepath.Join(tmpDir, "context")
			meta, err := ocipackage.StageContext(flags.packageDir, contextDir, time.Unix(flags.timestamp, 0).UTC())
			if err != nil {
				return err
			}

			containerfile := filepath.Join(tmpDir, "Containerfile")
			if err := os.WriteFile(containerfile, []byte("FROM scratch\nCOPY . /\n"), 0o644); err != nil {
				return fmt.Errorf("write containerfile: %w", err)
			}

			buildArgs := []string{
				"build",
				"--file", containerfile,
				"--format", "oci",
				"--pull=false",
				"--save-image=false",
				"--timestamp", fmt.Sprintf("%d", flags.timestamp),
				"--platform", flags.platform,
				"--tag", flags.image,
				"--label", "sealos.io.type=" + string(meta.Class),
				"--label", "sealos.io.version=" + meta.Version,
				"--label", "distribution.sealos.io/api-version=v1alpha1",
				"--label", "distribution.sealos.io/kind=ComponentPackage",
				"--label", "distribution.sealos.io/package-name=" + meta.Name,
				"--label", "distribution.sealos.io/component=" + meta.Component,
				"--label", "distribution.sealos.io/version=" + meta.Version,
			}
			if flags.distribution != "" {
				buildArgs = append(buildArgs, "--label", "sealos.io.distribution="+flags.distribution)
			}
			for _, label := range flags.labels {
				buildArgs = append(buildArgs, "--label", label)
			}
			buildArgs = append(buildArgs, contextDir)

			buildArgs, err = syncPackageSubcommandArgs(cmd, buildArgs)
			if err != nil {
				return err
			}
			if err := runSyncPackageSubcommand(buildArgs, cmd.ErrOrStderr()); err != nil {
				return fmt.Errorf("build package image: %w", err)
			}
			inspectArgs, err := syncPackageSubcommandArgs(cmd, []string{"inspect", "--type", "image", flags.image})
			if err != nil {
				return err
			}
			if err := runSyncPackageSubcommand(inspectArgs, cmd.ErrOrStderr()); err != nil {
				return fmt.Errorf("inspect package image: %w", err)
			}

			out := syncPackageBuildOutput{
				PackageDir:       meta.PackageDir,
				Image:            flags.image,
				PackageName:      meta.Name,
				PackageComponent: meta.Component,
				PackageVersion:   meta.Version,
				PackageClass:     string(meta.Class),
				Platform:         flags.platform,
			}
			return writeSyncPackageOutput(cmd, flags.output, out, [][2]string{
				{"package_dir", out.PackageDir},
				{"image", out.Image},
				{"package_name", out.PackageName},
				{"package_component", out.PackageComponent},
				{"package_version", out.PackageVersion},
				{"package_class", out.PackageClass},
				{"platform", out.Platform},
			})
		},
	}

	cmd.Flags().StringVar(&flags.packageDir, "package-dir", "", "component package directory to build")
	cmd.Flags().StringVar(&flags.image, "image", "", "image reference to tag locally after build")
	cmd.Flags().StringVar(&flags.platform, "platform", "linux/amd64", "target platform in os/arch form")
	cmd.Flags().Int64Var(&flags.timestamp, "timestamp", 0, "created timestamp in epoch seconds for deterministic builds")
	cmd.Flags().StringVar(&flags.distribution, "distribution", "", "optional distribution label to apply to the package image")
	cmd.Flags().StringArrayVar(&flags.labels, "label", nil, "additional OCI labels in key=value form")
	cmd.Flags().StringVar(&flags.output, "output", "yaml", "output format: yaml or env")
	mustMarkFlagRequired(cmd, "package-dir")
	mustMarkFlagRequired(cmd, "image")
	return cmd
}

func newSyncPackagePushCmd() *cobra.Command {
	var flags struct {
		image       string
		destination string
		authfile    string
		certDir     string
		creds       string
		digestFile  string
		output      string
	}

	cmd := &cobra.Command{
		Use:   "push",
		Short: "Push a previously built OCI component package image",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			destination := normalizeSyncPackagePushDestination(flags.destination)
			digestFile := flags.digestFile
			ownedDigestFile := false
			if digestFile == "" {
				tmpFile, err := os.CreateTemp("", "sealos-sync-package-digest-")
				if err != nil {
					return fmt.Errorf("create digest file: %w", err)
				}
				if err := tmpFile.Close(); err != nil {
					return fmt.Errorf("close digest file: %w", err)
				}
				digestFile = tmpFile.Name()
				ownedDigestFile = true
			}
			if ownedDigestFile {
				defer os.Remove(digestFile)
			}

			pushArgs := []string{"push", "--digestfile", digestFile}
			if flags.authfile != "" {
				pushArgs = append(pushArgs, "--authfile", flags.authfile)
			}
			if flags.certDir != "" {
				pushArgs = append(pushArgs, "--cert-dir", flags.certDir)
			}
			if flags.creds != "" {
				pushArgs = append(pushArgs, "--creds", flags.creds)
			}
			pushArgs = append(pushArgs, flags.image, destination)

			pushArgs, err := syncPackageSubcommandArgs(cmd, pushArgs)
			if err != nil {
				return err
			}
			if err := runSyncPackageSubcommand(pushArgs, cmd.ErrOrStderr()); err != nil {
				return fmt.Errorf("push package image: %w", err)
			}

			digestBytes, err := os.ReadFile(digestFile)
			if err != nil {
				return fmt.Errorf("read digest file %q: %w", digestFile, err)
			}
			digest := strings.TrimSpace(string(digestBytes))
			if digest == "" {
				return fmt.Errorf("push completed without digest output")
			}

			image := imageRefFromSyncPackageDestination(destination)
			out := syncPackagePushOutput{
				SourceImage: flags.image,
				Destination: destination,
				Image:       image,
				Digest:      digest,
				Reference:   image + "@" + digest,
			}
			return writeSyncPackageOutput(cmd, flags.output, out, [][2]string{
				{"source_image", out.SourceImage},
				{"destination", out.Destination},
				{"image", out.Image},
				{"digest", out.Digest},
				{"reference", out.Reference},
			})
		},
	}

	cmd.Flags().StringVar(&flags.image, "image", "", "local image reference to push")
	cmd.Flags().StringVar(&flags.destination, "destination", "", "destination image reference, docker:// implied when omitted")
	cmd.Flags().StringVar(&flags.authfile, "authfile", "", "path to a registry auth file")
	cmd.Flags().StringVar(&flags.certDir, "cert-dir", "", "path to registry certificates")
	cmd.Flags().StringVar(&flags.creds, "creds", "", "registry credentials in user[:pass] form")
	cmd.Flags().StringVar(&flags.digestFile, "digest-file", "", "write the pushed digest to this path")
	cmd.Flags().StringVar(&flags.output, "output", "yaml", "output format: yaml or env")
	mustMarkFlagRequired(cmd, "image")
	mustMarkFlagRequired(cmd, "destination")
	return cmd
}

func defaultSyncPackageCommandRunner(args []string, stderr io.Writer) error {
	executable, err := os.Executable()
	if err != nil {
		return fmt.Errorf("resolve current executable: %w", err)
	}

	command := exec.Command(executable, args...) //nolint:gosec
	command.Env = os.Environ()
	command.Stdout = stderr
	command.Stderr = stderr
	return command.Run()
}

func syncPackageSubcommandArgs(cmd *cobra.Command, args []string) ([]string, error) {
	rootArgs, err := syncPackageRootArgs(cmd)
	if err != nil {
		return nil, err
	}
	return append(rootArgs, args...), nil
}

func syncPackageRootArgs(cmd *cobra.Command) ([]string, error) {
	if cmd == nil || cmd.Root() == nil {
		return nil, nil
	}

	root := cmd.Root().PersistentFlags()
	var args []string
	var rootErr error
	root.Visit(func(flag *pflag.Flag) {
		if rootErr != nil {
			return
		}
		switch flag.Value.Type() {
		case "stringSlice":
			values, err := root.GetStringSlice(flag.Name)
			if err != nil {
				rootErr = err
				return
			}
			for _, value := range values {
				args = append(args, fmt.Sprintf("--%s=%s", flag.Name, value))
			}
		case "stringArray":
			values, err := root.GetStringArray(flag.Name)
			if err != nil {
				rootErr = err
				return
			}
			for _, value := range values {
				args = append(args, fmt.Sprintf("--%s=%s", flag.Name, value))
			}
		default:
			args = append(args, fmt.Sprintf("--%s=%s", flag.Name, flag.Value.String()))
		}
	})
	if rootErr != nil {
		return nil, fmt.Errorf("collect root flags: %w", rootErr)
	}
	return args, nil
}

func writeSyncPackageOutput(cmd *cobra.Command, format string, value any, envPairs [][2]string) error {
	switch format {
	case "yaml":
		data, err := yaml.Marshal(value)
		if err != nil {
			return fmt.Errorf("marshal output: %w", err)
		}
		_, err = cmd.OutOrStdout().Write(data)
		return err
	case "env":
		for _, pair := range envPairs {
			if _, err := fmt.Fprintf(cmd.OutOrStdout(), "%s=%s\n", pair[0], shellQuoteForEnv(pair[1])); err != nil {
				return err
			}
		}
		return nil
	default:
		return fmt.Errorf("unsupported output format %q, want yaml or env", format)
	}
}

func shellQuoteForEnv(value string) string {
	if value == "" {
		return "''"
	}
	return "'" + strings.ReplaceAll(value, "'", "'\"'\"'") + "'"
}

func normalizeSyncPackagePushDestination(destination string) string {
	switch {
	case strings.HasPrefix(destination, "containers-storage:"),
		strings.HasPrefix(destination, "dir:"),
		strings.HasPrefix(destination, "docker://"),
		strings.HasPrefix(destination, "docker-archive:"),
		strings.HasPrefix(destination, "docker-daemon:"),
		strings.HasPrefix(destination, "oci:"),
		strings.HasPrefix(destination, "oci-archive:"),
		strings.HasPrefix(destination, "ostree:"),
		strings.HasPrefix(destination, "sif:"):
		return destination
	default:
		return "docker://" + destination
	}
}

func imageRefFromSyncPackageDestination(destination string) string {
	if strings.HasPrefix(destination, "docker://") {
		return strings.TrimPrefix(destination, "docker://")
	}
	return destination
}

func mustMarkFlagRequired(cmd *cobra.Command, name string) {
	if err := cmd.MarkFlagRequired(name); err != nil {
		panic(err)
	}
}
