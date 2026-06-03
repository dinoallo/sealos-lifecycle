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

package cert

import (
	"os"
	"path/filepath"
	"testing"

	"k8s.io/client-go/tools/clientcmd"
)

func TestCreateKubeConfigFileWritesRequestedFile(t *testing.T) {
	pkiDir := t.TempDir()
	etcDir := t.TempDir()

	caCfg := Config{
		Path:       pkiDir,
		BaseName:   "ca",
		CommonName: "kubernetes",
		Year:       100,
	}
	caCert, caKey, err := NewCaCertAndKey(caCfg)
	if err != nil {
		t.Fatalf("NewCaCertAndKey() error = %v", err)
	}
	if err := WriteCertAndKey(pkiDir, "ca", caCert, caKey); err != nil {
		t.Fatalf("WriteCertAndKey() error = %v", err)
	}

	if err := CreateKubeConfigFile("admin.conf", etcDir, caCfg, "master-0", "https://sealos-api:6443", "kubernetes"); err != nil {
		t.Fatalf("CreateKubeConfigFile() error = %v", err)
	}

	adminPath := filepath.Join(etcDir, "admin.conf")
	if _, err := os.Stat(adminPath); err != nil {
		t.Fatalf("Stat(%q) error = %v", adminPath, err)
	}
	if _, err := os.Stat(filepath.Join(etcDir, "controller-manager.conf")); !os.IsNotExist(err) {
		t.Fatalf("controller-manager.conf should not be created, got err = %v", err)
	}

	cfg, err := clientcmd.LoadFromFile(adminPath)
	if err != nil {
		t.Fatalf("LoadFromFile(%q) error = %v", adminPath, err)
	}
	cluster := cfg.Clusters["kubernetes"]
	if cluster == nil {
		t.Fatalf("expected kubernetes cluster entry in kubeconfig")
	}
	if got, want := cluster.Server, "https://sealos-api:6443"; got != want {
		t.Fatalf("cluster.Server = %q, want %q", got, want)
	}
}
