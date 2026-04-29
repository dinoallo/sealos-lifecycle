package kubernetes

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/labring/sealos/pkg/constants"
	runtimetypes "github.com/labring/sealos/pkg/runtime/kubernetes/types"
)

func TestRefreshKubeadmCacheTokenOnlyPreservesCertificateKey(t *testing.T) {
	previousRuntimeRoot := constants.DefaultRuntimeRootDir
	previousClusterRoot := constants.DefaultClusterRootFsDir
	previousWriteToken := writeKubeadmTokenCache
	constants.DefaultRuntimeRootDir = t.TempDir()
	constants.DefaultClusterRootFsDir = t.TempDir()
	t.Cleanup(func() {
		constants.DefaultRuntimeRootDir = previousRuntimeRoot
		constants.DefaultClusterRootFsDir = previousClusterRoot
		writeKubeadmTokenCache = previousWriteToken
	})

	pathResolver := constants.NewPathResolver("default")
	if err := os.MkdirAll(pathResolver.EtcPath(), 0o755); err != nil {
		t.Fatalf("MkdirAll(etc) error = %v", err)
	}

	certificateKeyPath := filepath.Join(pathResolver.EtcPath(), defaultCertificateKeyFileName)
	if err := os.WriteFile(certificateKeyPath, []byte("existing-key"), 0o600); err != nil {
		t.Fatalf("WriteFile(certificateKey) error = %v", err)
	}

	writeKubeadmTokenCache = func(_ *KubeadmRuntime, file string) error {
		return os.WriteFile(file, []byte(`{"joinToken":"token-a"}`), 0o600)
	}

	rt := &KubeadmRuntime{
		kubeadmConfig: runtimetypes.NewKubeadmConfig(),
		pathResolver:  pathResolver,
	}
	if err := rt.RefreshKubeadmCache(true, false); err != nil {
		t.Fatalf("RefreshKubeadmCache(true, false) error = %v", err)
	}

	data, err := os.ReadFile(certificateKeyPath)
	if err != nil {
		t.Fatalf("ReadFile(certificateKey) error = %v", err)
	}
	if got, want := string(data), "existing-key"; got != want {
		t.Fatalf("certificate key = %q, want %q", got, want)
	}
	if _, err := os.Stat(filepath.Join(pathResolver.EtcPath(), defaultKubeadmTokenFileName)); err != nil {
		t.Fatalf("Stat(token cache) error = %v", err)
	}
}

func TestRefreshKubeadmCacheCertificateKeyRefreshesToken(t *testing.T) {
	previousRuntimeRoot := constants.DefaultRuntimeRootDir
	previousClusterRoot := constants.DefaultClusterRootFsDir
	previousWriteToken := writeKubeadmTokenCache
	previousGenerateKey := generateKubeadmCertificateKey
	constants.DefaultRuntimeRootDir = t.TempDir()
	constants.DefaultClusterRootFsDir = t.TempDir()
	t.Cleanup(func() {
		constants.DefaultRuntimeRootDir = previousRuntimeRoot
		constants.DefaultClusterRootFsDir = previousClusterRoot
		writeKubeadmTokenCache = previousWriteToken
		generateKubeadmCertificateKey = previousGenerateKey
	})

	pathResolver := constants.NewPathResolver("default")
	if err := os.MkdirAll(pathResolver.EtcPath(), 0o755); err != nil {
		t.Fatalf("MkdirAll(etc) error = %v", err)
	}

	generateKubeadmCertificateKey = func() (string, error) {
		return "rotated-key", nil
	}
	writeCalls := 0
	writeKubeadmTokenCache = func(_ *KubeadmRuntime, file string) error {
		writeCalls++
		return os.WriteFile(file, []byte(`{"joinToken":"token-b","certificateKey":"rotated-key"}`), 0o600)
	}

	rt := &KubeadmRuntime{
		kubeadmConfig: runtimetypes.NewKubeadmConfig(),
		pathResolver:  pathResolver,
	}
	if err := rt.RefreshKubeadmCache(false, true); err != nil {
		t.Fatalf("RefreshKubeadmCache(false, true) error = %v", err)
	}

	data, err := os.ReadFile(filepath.Join(pathResolver.EtcPath(), defaultCertificateKeyFileName))
	if err != nil {
		t.Fatalf("ReadFile(certificateKey) error = %v", err)
	}
	if got, want := string(data), "rotated-key"; got != want {
		t.Fatalf("certificate key = %q, want %q", got, want)
	}
	if writeCalls != 1 {
		t.Fatalf("writeKubeadmTokenCache calls = %d, want 1", writeCalls)
	}
	if _, err := os.Stat(filepath.Join(pathResolver.EtcPath(), defaultKubeadmTokenFileName)); err != nil {
		t.Fatalf("Stat(token cache) error = %v", err)
	}
}
