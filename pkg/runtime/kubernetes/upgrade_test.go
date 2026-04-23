package kubernetes

import (
	"errors"
	"strings"
	"testing"
)

func TestUpgradeApplyCommandDoesNotMixConfigFlag(t *testing.T) {
	version := "v1.33.0"
	cmd := buildUpgradeApplyCmd(version, false)

	if strings.Contains(cmd, "--config") {
		t.Fatalf("upgrade apply command must not mix --config with other flags: %s", cmd)
	}
	if !strings.Contains(cmd, "--certificate-renewal=false") {
		t.Fatalf("upgrade apply command must keep certificate renewal disabled: %s", cmd)
	}
	if !strings.Contains(cmd, "--yes "+version) {
		t.Fatalf("upgrade apply command must pass the target version positionally: %s", cmd)
	}
}

func TestUpgradeApplyRetryCommandIgnoresCreateJob(t *testing.T) {
	version := "v1.33.10"
	cmd := buildUpgradeApplyCmd(version, true)

	if !strings.Contains(cmd, "--ignore-preflight-errors=CreateJob") {
		t.Fatalf("retry command must ignore the CreateJob preflight check: %s", cmd)
	}
	if !strings.Contains(cmd, "--yes "+version) {
		t.Fatalf("retry command must pass the target version positionally: %s", cmd)
	}
	if strings.Contains(cmd, "--config") {
		t.Fatalf("retry command must not mix --config with other flags: %s", cmd)
	}
}

func TestShouldRetryUpgradeApply(t *testing.T) {
	tests := []struct {
		name string
		err  error
		want bool
	}{
		{
			name: "create job preflight error",
			err: errors.New(`[upgrade] Running cluster health checks
error execution phase preflight: [preflight] Some fatal errors occurred:
        [ERROR CreateJob]: Job "upgrade-health-check-1776925836348" in the namespace "kube-system" did not complete in 15s: no condition of type Complete
[preflight] If you know what you are doing, you can make a check non-fatal with --ignore-preflight-errors=...
To see the stack trace of this error execute with --v=5 or higher`),
			want: true,
		},
		{
			name: "upgrade health check timeout",
			err:  errors.New(`Job "upgrade-health-check-1" did not complete in 15s`),
			want: false,
		},
		{
			name: "unrelated kubeadm error",
			err:  errors.New("error execution phase preflight: some other fatal error"),
			want: false,
		},
		{
			name: "nil error",
			err:  nil,
			want: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := shouldRetryUpgradeApply(tt.err); got != tt.want {
				t.Fatalf("shouldRetryUpgradeApply(%v) = %v, want %v", tt.err, got, tt.want)
			}
		})
	}
}
