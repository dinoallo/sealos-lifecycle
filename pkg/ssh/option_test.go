// Copyright © 2023 sealos.
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

package ssh

import (
	"os"
	"path/filepath"
	"testing"
)

func TestDefaultSSHPrivateKeyUsesHostStarIdentityFile(t *testing.T) {
	homeDir := t.TempDir()
	sshDir := filepath.Join(homeDir, ".ssh")
	if err := os.MkdirAll(sshDir, 0o700); err != nil {
		t.Fatalf("MkdirAll() error = %v", err)
	}
	keyPath := filepath.Join(sshDir, "id_ed25519_tofu")
	if err := os.WriteFile(keyPath, []byte("private-key"), 0o600); err != nil {
		t.Fatalf("WriteFile(key) error = %v", err)
	}
	config := "Host *\n  IdentityFile ~/.ssh/id_ed25519_tofu\n"
	if err := os.WriteFile(filepath.Join(sshDir, "config"), []byte(config), 0o600); err != nil {
		t.Fatalf("WriteFile(config) error = %v", err)
	}

	if got := defaultSSHPrivateKey(homeDir); got != keyPath {
		t.Fatalf("defaultSSHPrivateKey() = %q, want %q", got, keyPath)
	}
}

func TestDefaultSSHPrivateKeyFallsBackToEd25519(t *testing.T) {
	homeDir := t.TempDir()
	sshDir := filepath.Join(homeDir, ".ssh")
	if err := os.MkdirAll(sshDir, 0o700); err != nil {
		t.Fatalf("MkdirAll() error = %v", err)
	}
	keyPath := filepath.Join(sshDir, "id_ed25519")
	if err := os.WriteFile(keyPath, []byte("private-key"), 0o600); err != nil {
		t.Fatalf("WriteFile(key) error = %v", err)
	}

	if got := defaultSSHPrivateKey(homeDir); got != keyPath {
		t.Fatalf("defaultSSHPrivateKey() = %q, want %q", got, keyPath)
	}
}
