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
