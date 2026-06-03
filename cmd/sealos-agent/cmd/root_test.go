package cmd

import (
	"context"
	"io"
	"path/filepath"
	"testing"

	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/healthz"

	"github.com/labring/sealos/pkg/distribution/agent"
	distributioncontroller "github.com/labring/sealos/pkg/distribution/controller"
)

func TestTargetOptions(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		bom     string
		channel string
		want    agent.TargetOptions
		wantErr bool
	}{
		{
			name: "bom",
			bom:  "bom.yaml",
			want: agent.TargetOptions{BOMPath: "bom.yaml"},
		},
		{
			name:    "channel",
			channel: "channel.yaml",
			want:    agent.TargetOptions{ReleaseChannelPath: "channel.yaml"},
		},
		{
			name:    "missing",
			wantErr: true,
		},
		{
			name:    "ambiguous",
			bom:     "bom.yaml",
			channel: "channel.yaml",
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got, err := targetOptions(tt.bom, tt.channel)
			if tt.wantErr {
				if err == nil {
					t.Fatal("targetOptions() error = nil, want error")
				}
				return
			}
			if err != nil {
				t.Fatalf("targetOptions() error = %v", err)
			}
			if got != tt.want {
				t.Fatalf("targetOptions() = %#v, want %#v", got, tt.want)
			}
		})
	}
}

func TestParsePackageSources(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	source := filepath.Join(root, "runtime")
	got, err := parsePackageSources([]string{"runtime=" + source})
	if err != nil {
		t.Fatalf("parsePackageSources() error = %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("len(sources) = %d, want 1", len(got))
	}
	if got[0].Component != "runtime" {
		t.Fatalf("component = %q, want runtime", got[0].Component)
	}
	if want, err := filepath.Abs(source); err != nil {
		t.Fatalf("Abs(source) error = %v", err)
	} else if got[0].Root != want {
		t.Fatalf("root = %q, want %q", got[0].Root, want)
	}

	if _, err := parsePackageSources([]string{"runtime=" + source, "runtime=" + source}); err == nil {
		t.Fatal("duplicate package source error = nil, want error")
	}
	if _, err := parsePackageSources([]string{"invalid"}); err == nil {
		t.Fatal("invalid package source error = nil, want error")
	}
}

func TestRunControllerConfiguresManager(t *testing.T) {
	manager := &fakeControllerManager{}
	var gotOptions ctrl.Options
	var gotDefaults distributioncontroller.Defaults
	err := runController(context.Background(), io.Discard, controllerOptions{
		defaults: distributioncontroller.Defaults{
			ClusterName:    "cluster-a",
			KubeconfigPath: "/etc/kubernetes/admin.conf",
			HostRoot:       "/",
		},
		namespace:               "sealos-system",
		metricsBindAddress:      ":8080",
		healthProbeBindAddress:  ":8081",
		leaderElection:          true,
		leaderElectionNamespace: "kube-system",
		newManager: func(opts ctrl.Options) (controllerManager, error) {
			gotOptions = opts
			return manager, nil
		},
		setupDistributionTarget: func(_ controllerManager, defaults distributioncontroller.Defaults) error {
			gotDefaults = defaults
			return nil
		},
	})
	if err != nil {
		t.Fatalf("runController() error = %v", err)
	}
	if gotOptions.Scheme == nil {
		t.Fatal("manager scheme = nil, want registered scheme")
	}
	if gotOptions.Cache.DefaultNamespaces == nil {
		t.Fatal("DefaultNamespaces = nil, want namespace-scoped cache")
	}
	if _, ok := gotOptions.Cache.DefaultNamespaces["sealos-system"]; !ok {
		t.Fatalf("DefaultNamespaces = %#v, want sealos-system", gotOptions.Cache.DefaultNamespaces)
	}
	if got, want := gotOptions.Metrics.BindAddress, ":8080"; got != want {
		t.Fatalf("Metrics.BindAddress = %q, want %q", got, want)
	}
	if got, want := gotOptions.HealthProbeBindAddress, ":8081"; got != want {
		t.Fatalf("HealthProbeBindAddress = %q, want %q", got, want)
	}
	if !gotOptions.LeaderElection {
		t.Fatal("LeaderElection = false, want true")
	}
	if got, want := gotOptions.LeaderElectionNamespace, "kube-system"; got != want {
		t.Fatalf("LeaderElectionNamespace = %q, want %q", got, want)
	}
	if got, want := gotDefaults.ClusterName, "cluster-a"; got != want {
		t.Fatalf("defaults.ClusterName = %q, want %q", got, want)
	}
	if manager.healthz != "healthz" || manager.readyz != "readyz" || !manager.started {
		t.Fatalf("manager = %#v, want checks and started", manager)
	}
}

type fakeControllerManager struct {
	healthz string
	readyz  string
	started bool
}

func (m *fakeControllerManager) Start(context.Context) error {
	m.started = true
	return nil
}

func (m *fakeControllerManager) AddHealthzCheck(name string, _ healthz.Checker) error {
	m.healthz = name
	return nil
}

func (m *fakeControllerManager) AddReadyzCheck(name string, _ healthz.Checker) error {
	m.readyz = name
	return nil
}
