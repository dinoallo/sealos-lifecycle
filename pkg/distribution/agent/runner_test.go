package agent

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/labring/sealos/pkg/constants"
	"github.com/labring/sealos/pkg/distribution/bom"
	"github.com/labring/sealos/pkg/distribution/hydrate"
	"github.com/labring/sealos/pkg/distribution/packageformat"
	"github.com/labring/sealos/pkg/distribution/reconcile"
	"github.com/labring/sealos/pkg/distribution/state"
	yamlutil "github.com/labring/sealos/pkg/utils/yaml"
)

func TestRunnerRunOnceWithExplicitBOM(t *testing.T) {
	withRuntimeRoot(t)
	root := t.TempDir()
	packageRoot := writeAgentPackage(t, root)
	doc := agentBOM()
	bomPath := filepath.Join(root, "bom.yaml")
	if err := yamlutil.MarshalFile(bomPath, doc); err != nil {
		t.Fatalf("MarshalFile(bom) error = %v", err)
	}

	var materializeProvenance hydrate.RenderProvenance
	runner := Runner{
		Materialize: func(got *bom.BOM, opts reconcile.Options) (*reconcile.Result, error) {
			if got.Spec.Revision != doc.Spec.Revision {
				t.Fatalf("materialize revision = %q, want %q", got.Spec.Revision, doc.Spec.Revision)
			}
			materializeProvenance = opts.RenderProvenance
			return &reconcile.Result{BundlePath: filepath.Join(root, "bundle")}, nil
		},
		Apply: func(opts reconcile.ApplyOptions) (*reconcile.ApplyResult, error) {
			applied, err := state.PersistSuccessfulApply(opts.ClusterName, state.BOMReference{
				Name:     doc.Metadata.Name,
				Revision: doc.Spec.Revision,
				Channel:  doc.Spec.Channel,
			}, "sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", "", "")
			if err != nil {
				return nil, err
			}
			return &reconcile.ApplyResult{
				BundlePath:         opts.BundlePath,
				DesiredStateDigest: applied.Spec.DesiredStateDigest,
				AppliedRevision:    applied,
			}, nil
		},
	}

	result, err := runner.Run(context.Background(), Options{
		ClusterName: "agent-explicit",
		Target: TargetOptions{
			BOMPath: bomPath,
		},
		PackageSources: []PackageSource{{Component: "runtime", Root: packageRoot}},
		Once:           true,
	})
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if got, want := result.Revision, doc.Spec.Revision; got != want {
		t.Fatalf("result.Revision = %q, want %q", got, want)
	}
	if got, want := materializeProvenance.BOMPath, bomPath; got != want {
		t.Fatalf("provenance.BOMPath = %q, want %q", got, want)
	}
	if materializeProvenance.BOMDigest == "" {
		t.Fatal("provenance.BOMDigest is empty")
	}
}

func TestRunnerRunOnceWithDistributionChannel(t *testing.T) {
	withRuntimeRoot(t)
	root := t.TempDir()
	packageRoot := writeAgentPackage(t, root)
	doc := agentBOM()
	doc.Spec.Channel = bom.ChannelAlpha
	bomPath := filepath.Join(root, "bom.yaml")
	if err := yamlutil.MarshalFile(bomPath, doc); err != nil {
		t.Fatalf("MarshalFile(bom) error = %v", err)
	}
	channelPath := filepath.Join(root, "channel.yaml")
	channel := bom.NewDistributionChannel("agent-runtime-stable", doc.Metadata.Name, bom.ChannelStable, doc.Spec.Revision, "bom.yaml")
	if err := yamlutil.MarshalFile(channelPath, channel); err != nil {
		t.Fatalf("MarshalFile(channel) error = %v", err)
	}

	var selectedChannel bom.ReleaseChannel
	var provenance hydrate.RenderProvenance
	runner := Runner{
		Materialize: func(got *bom.BOM, opts reconcile.Options) (*reconcile.Result, error) {
			selectedChannel = got.Spec.Channel
			provenance = opts.RenderProvenance
			return &reconcile.Result{BundlePath: filepath.Join(root, "bundle")}, nil
		},
		Apply: func(opts reconcile.ApplyOptions) (*reconcile.ApplyResult, error) {
			applied, err := state.PersistSuccessfulApply(opts.ClusterName, state.BOMReference{
				Name:     doc.Metadata.Name,
				Revision: doc.Spec.Revision,
				Channel:  bom.ChannelStable,
			}, "sha256:bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb", "", "")
			if err != nil {
				return nil, err
			}
			return &reconcile.ApplyResult{
				BundlePath:         opts.BundlePath,
				DesiredStateDigest: applied.Spec.DesiredStateDigest,
				AppliedRevision:    applied,
			}, nil
		},
	}

	result, err := runner.Run(context.Background(), Options{
		ClusterName: "agent-channel",
		Target: TargetOptions{
			DistributionChannelPath: channelPath,
		},
		PackageSources: []PackageSource{{Component: "runtime", Root: packageRoot}},
		Once:           true,
	})
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if got, want := selectedChannel, bom.ChannelStable; got != want {
		t.Fatalf("selected channel = %q, want %q", got, want)
	}
	if got, want := result.Channel, string(bom.ChannelStable); got != want {
		t.Fatalf("result.Channel = %q, want %q", got, want)
	}
	if got, want := provenance.DistributionLine, doc.Metadata.Name; got != want {
		t.Fatalf("provenance.DistributionLine = %q, want %q", got, want)
	}
	if provenance.DistributionChannelDigest == "" {
		t.Fatal("provenance.DistributionChannelDigest is empty")
	}
}

func TestRunnerMarksDegradedAfterApplyFailure(t *testing.T) {
	withRuntimeRoot(t)
	clusterName := "agent-degraded"
	root := t.TempDir()
	packageRoot := writeAgentPackage(t, root)
	doc := agentBOM()
	bomPath := filepath.Join(root, "bom.yaml")
	if err := yamlutil.MarshalFile(bomPath, doc); err != nil {
		t.Fatalf("MarshalFile(bom) error = %v", err)
	}

	runner := Runner{
		Materialize: func(got *bom.BOM, opts reconcile.Options) (*reconcile.Result, error) {
			if _, err := state.PersistRenderedState(clusterName, state.BOMReference{
				Name:     got.Metadata.Name,
				Revision: got.Spec.Revision,
				Channel:  got.Spec.Channel,
			}, "sha256:cccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccc", "", ""); err != nil {
				return nil, err
			}
			return &reconcile.Result{BundlePath: filepath.Join(root, "bundle")}, nil
		},
		Apply: func(reconcile.ApplyOptions) (*reconcile.ApplyResult, error) {
			return nil, fmt.Errorf("apply failed")
		},
	}

	_, err := runner.Run(context.Background(), Options{
		ClusterName:    clusterName,
		Target:         TargetOptions{BOMPath: bomPath},
		PackageSources: []PackageSource{{Component: "runtime", Root: packageRoot}},
		Once:           true,
	})
	if err == nil {
		t.Fatal("Run() error = nil, want apply failure")
	}
	applied, err := state.LoadAppliedRevision(clusterName)
	if err != nil {
		t.Fatalf("LoadAppliedRevision() error = %v", err)
	}
	if got, want := applied.Status.State, state.StateDegraded; got != want {
		t.Fatalf("status.state = %q, want %q", got, want)
	}
}

func TestRunnerLoopStopsOnContextCancellation(t *testing.T) {
	withRuntimeRoot(t)
	root := t.TempDir()
	packageRoot := writeAgentPackage(t, root)
	doc := agentBOM()
	bomPath := filepath.Join(root, "bom.yaml")
	if err := yamlutil.MarshalFile(bomPath, doc); err != nil {
		t.Fatalf("MarshalFile(bom) error = %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	runs := 0
	runner := Runner{
		Materialize: func(*bom.BOM, reconcile.Options) (*reconcile.Result, error) {
			runs++
			return &reconcile.Result{BundlePath: filepath.Join(root, "bundle")}, nil
		},
		Apply: func(opts reconcile.ApplyOptions) (*reconcile.ApplyResult, error) {
			applied, err := state.PersistSuccessfulApply(opts.ClusterName, state.BOMReference{
				Name:     doc.Metadata.Name,
				Revision: doc.Spec.Revision,
				Channel:  doc.Spec.Channel,
			}, "sha256:dddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddd", "", "")
			if err != nil {
				return nil, err
			}
			return &reconcile.ApplyResult{
				BundlePath:         opts.BundlePath,
				DesiredStateDigest: applied.Spec.DesiredStateDigest,
				AppliedRevision:    applied,
			}, nil
		},
		Sleep: func(context.Context, time.Duration) error {
			cancel()
			return context.Canceled
		},
	}

	_, err := runner.Run(ctx, Options{
		ClusterName:    "agent-loop",
		Target:         TargetOptions{BOMPath: bomPath},
		PackageSources: []PackageSource{{Component: "runtime", Root: packageRoot}},
		Interval:       time.Second,
	})
	if err == nil {
		t.Fatal("Run() error = nil, want context cancellation")
	}
	if got, want := runs, 1; got != want {
		t.Fatalf("runs = %d, want %d", got, want)
	}
}

func withRuntimeRoot(t *testing.T) {
	t.Helper()
	previous := constants.DefaultRuntimeRootDir
	constants.DefaultRuntimeRootDir = t.TempDir()
	t.Cleanup(func() {
		constants.DefaultRuntimeRootDir = previous
	})
}

func agentBOM() *bom.BOM {
	doc := bom.New("agent-runtime", "rev-agent-1", bom.ChannelBeta)
	doc.Spec.Components = []bom.Component{
		{
			Name:    "runtime",
			Kind:    "infra",
			Version: "v0.1.0",
			Artifact: bom.ArtifactReference{
				Name:   "runtime-rootfs",
				Image:  "registry.example.io/sealos/runtime-rootfs:v0.1.0",
				Digest: "sha256:eeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeee",
			},
		},
	}
	return doc
}

func writeAgentPackage(t *testing.T, root string) string {
	t.Helper()
	packageRoot := filepath.Join(root, "runtime-rootfs")
	if err := os.MkdirAll(filepath.Join(packageRoot, "rootfs"), 0o755); err != nil {
		t.Fatalf("MkdirAll(rootfs) error = %v", err)
	}
	manifest := &packageformat.ComponentPackage{
		APIVersion: "distribution.sealos.io/v1alpha1",
		Kind:       "ComponentPackage",
		Metadata: packageformat.Metadata{
			Name: "runtime-rootfs",
		},
		Spec: packageformat.Spec{
			Component: "runtime",
			Version:   "v0.1.0",
			Class:     packageformat.ClassRootfs,
			Contents: []packageformat.Content{
				{
					Name:     "runtime-rootfs",
					Type:     packageformat.ContentRootfs,
					Path:     "rootfs",
					Required: true,
				},
			},
		},
	}
	if err := yamlutil.MarshalFile(filepath.Join(packageRoot, packageformat.ManifestFileName), manifest); err != nil {
		t.Fatalf("MarshalFile(package) error = %v", err)
	}
	return packageRoot
}
