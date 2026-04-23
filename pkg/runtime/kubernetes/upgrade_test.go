package kubernetes

import (
	"fmt"
	"strings"
	"testing"
)

func TestUpgradeApplyCommandDoesNotMixConfigFlag(t *testing.T) {
	version := "v1.33.0"
	cmd := fmt.Sprintf(upgradeApplyCmd, version)

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
