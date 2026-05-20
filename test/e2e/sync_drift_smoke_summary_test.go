package e2e

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	ginkgotypes "github.com/onsi/ginkgo/v2/types"
	"sigs.k8s.io/yaml"
)

func TestSyncDriftSmokeWriteFailureSummary(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()
	ctx := &syncDriftSmokeContext{
		clusterName:        "sync-smoke-test",
		tmpDir:             tmpDir,
		bundleDir:          filepath.Join(tmpDir, "bundle"),
		localRepoDir:       filepath.Join(tmpDir, "local-repo"),
		hostRoot:           filepath.Join(tmpDir, "host-root"),
		runtimeRoot:        filepath.Join(tmpDir, "runtime-root"),
		commandLogPath:     filepath.Join(tmpDir, "commands.log"),
		commandsPath:       filepath.Join(tmpDir, "commands.yaml"),
		failureSummaryPath: filepath.Join(tmpDir, "failure-summary.yaml"),
		metadataPath:       filepath.Join(tmpDir, "metadata.yaml"),
	}

	commands := []syncDriftSmokeCommand{
		{
			StartedAt: time.Date(2026, 5, 9, 10, 0, 0, 0, time.UTC),
			Command:   "/tmp/sealos",
			Args:      []string{"sync", "diff"},
			ExitCode:  0,
			Success:   true,
		},
		{
			StartedAt: time.Date(2026, 5, 9, 10, 1, 0, 0, time.UTC),
			Command:   "/tmp/sealos",
			Args:      []string{"sync", "commit"},
			ExitCode:  1,
			Success:   false,
			Output:    "commit failed",
		},
	}
	if err := ctx.writeCommands(commands); err != nil {
		t.Fatalf("write commands: %v", err)
	}

	report := ginkgotypes.SpecReport{
		ContainerHierarchyTexts: []string{"E2E_sealos_sync_drift_smoke_test"},
		LeafNodeText:            "runs commit and revert loops against the docs drift fixture",
		State:                   ginkgotypes.SpecStateFailed,
		Failure: ginkgotypes.Failure{
			Message:  "expected currentState to be Clean",
			Location: ginkgotypes.CodeLocation{FileName: "/tmp/smoke_test.go", LineNumber: 42},
		},
	}

	summary, err := ctx.writeFailureSummary(report)
	if err != nil {
		t.Fatalf("write failure summary: %v", err)
	}
	if summary.CommandCount != 2 {
		t.Fatalf("unexpected command count: %d", summary.CommandCount)
	}
	if summary.LastCommand == nil || len(summary.LastCommand.Args) < 2 || summary.LastCommand.Args[1] != "commit" {
		t.Fatalf("unexpected last command: %#v", summary.LastCommand)
	}
	if summary.LastFailedCommand == nil || summary.LastFailedCommand.ExitCode != 1 {
		t.Fatalf("unexpected last failed command: %#v", summary.LastFailedCommand)
	}
	if summary.FailureLocation != "/tmp/smoke_test.go:42" {
		t.Fatalf("unexpected failure location: %q", summary.FailureLocation)
	}

	data, err := os.ReadFile(ctx.failureSummaryPath)
	if err != nil {
		t.Fatalf("read failure summary: %v", err)
	}

	var persisted syncDriftSmokeFailureSummary
	if err := yaml.Unmarshal(data, &persisted); err != nil {
		t.Fatalf("unmarshal failure summary: %v", err)
	}
	if persisted.LastFailedCommand == nil || persisted.LastFailedCommand.Output != "commit failed" {
		t.Fatalf("unexpected persisted last failed command: %#v", persisted.LastFailedCommand)
	}
}
