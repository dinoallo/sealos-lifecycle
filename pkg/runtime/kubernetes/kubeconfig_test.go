package kubernetes

import (
	"os"
	"path/filepath"
	"testing"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/tools/clientcmd"

	"github.com/labring/sealos/pkg/cert"
	"github.com/labring/sealos/pkg/constants"
	runtimetypes "github.com/labring/sealos/pkg/runtime/kubernetes/types"
	v2 "github.com/labring/sealos/pkg/types/v1beta1"
)

func TestRefreshAdminKubeConfigOverwritesStaleFile(t *testing.T) {
	previousRuntimeRoot := constants.DefaultRuntimeRootDir
	previousClusterRoot := constants.DefaultClusterRootFsDir
	constants.DefaultRuntimeRootDir = t.TempDir()
	constants.DefaultClusterRootFsDir = t.TempDir()
	t.Cleanup(func() {
		constants.DefaultRuntimeRootDir = previousRuntimeRoot
		constants.DefaultClusterRootFsDir = previousClusterRoot
	})

	cluster := &v2.Cluster{
		ObjectMeta: metav1.ObjectMeta{Name: "default"},
		Spec: v2.ClusterSpec{
			Hosts: []v2.Host{
				{
					IPS:   []string{"192.168.0.2:22"},
					Roles: []string{v2.MASTER},
				},
			},
		},
		Status: v2.ClusterStatus{
			Mounts: []v2.MountImage{
				{
					Type: v2.RootfsImage,
					Labels: map[string]string{
						v2.ImageKubeVersionKey: "v1.29.0",
						v2.ImageVIPKey:         "10.103.97.2",
					},
				},
			},
		},
	}

	pathResolver := constants.NewPathResolver(cluster.Name)
	if err := os.MkdirAll(pathResolver.PkiPath(), 0o755); err != nil {
		t.Fatalf("MkdirAll(pki) error = %v", err)
	}
	if err := os.MkdirAll(pathResolver.EtcPath(), 0o755); err != nil {
		t.Fatalf("MkdirAll(etc) error = %v", err)
	}

	caCfg := cert.Config{
		Path:       pathResolver.PkiPath(),
		BaseName:   "ca",
		CommonName: "kubernetes",
		Year:       100,
	}
	caCert, caKey, err := cert.NewCaCertAndKey(caCfg)
	if err != nil {
		t.Fatalf("NewCaCertAndKey() error = %v", err)
	}
	if err := cert.WriteCertAndKey(pathResolver.PkiPath(), "ca", caCert, caKey); err != nil {
		t.Fatalf("WriteCertAndKey() error = %v", err)
	}

	adminFile := pathResolver.AdminFile()
	if err := os.WriteFile(adminFile, []byte("stale"), 0o600); err != nil {
		t.Fatalf("WriteFile(%q) error = %v", adminFile, err)
	}

	rt := &KubeadmRuntime{
		cluster:       cluster,
		config:        &runtimetypes.Config{APIServerDomain: constants.DefaultAPIServerDomain},
		kubeadmConfig: runtimetypes.NewKubeadmConfig(),
		pathResolver:  pathResolver,
	}

	if err := rt.RefreshKubeConfigFiles(false); err != nil {
		t.Fatalf("RefreshKubeConfigFiles(false) error = %v", err)
	}

	cfg, err := clientcmd.LoadFromFile(adminFile)
	if err != nil {
		t.Fatalf("LoadFromFile(%q) error = %v", adminFile, err)
	}
	clusterCfg := cfg.Clusters["kubernetes"]
	if clusterCfg == nil {
		t.Fatalf("expected kubernetes cluster entry in admin kubeconfig")
	}
	if got, want := clusterCfg.Server, "https://"+constants.DefaultAPIServerDomain+":6443"; got != want {
		t.Fatalf("cluster.Server = %q, want %q", got, want)
	}
	if _, err := os.Stat(filepath.Join(pathResolver.EtcPath(), "controller-manager.conf")); !os.IsNotExist(err) {
		t.Fatalf("controller-manager.conf should not be created, got err = %v", err)
	}
}

func TestRefreshKubeConfigFilesAllCreatesFullSet(t *testing.T) {
	previousRuntimeRoot := constants.DefaultRuntimeRootDir
	previousClusterRoot := constants.DefaultClusterRootFsDir
	previousResolver := resolveLocalKubeConfigNodeName
	constants.DefaultRuntimeRootDir = t.TempDir()
	constants.DefaultClusterRootFsDir = t.TempDir()
	resolveLocalKubeConfigNodeName = func(*KubeadmRuntime) (string, error) {
		return "master-0", nil
	}
	t.Cleanup(func() {
		constants.DefaultRuntimeRootDir = previousRuntimeRoot
		constants.DefaultClusterRootFsDir = previousClusterRoot
		resolveLocalKubeConfigNodeName = previousResolver
	})

	cluster := &v2.Cluster{
		ObjectMeta: metav1.ObjectMeta{Name: "default"},
		Spec: v2.ClusterSpec{
			Hosts: []v2.Host{
				{
					IPS:   []string{"192.168.0.2:22"},
					Roles: []string{v2.MASTER},
				},
			},
		},
		Status: v2.ClusterStatus{
			Mounts: []v2.MountImage{
				{
					Type: v2.RootfsImage,
					Labels: map[string]string{
						v2.ImageKubeVersionKey: "v1.29.0",
						v2.ImageVIPKey:         "10.103.97.2",
					},
				},
			},
		},
	}

	pathResolver := constants.NewPathResolver(cluster.Name)
	if err := os.MkdirAll(pathResolver.PkiPath(), 0o755); err != nil {
		t.Fatalf("MkdirAll(pki) error = %v", err)
	}
	if err := os.MkdirAll(pathResolver.EtcPath(), 0o755); err != nil {
		t.Fatalf("MkdirAll(etc) error = %v", err)
	}

	caCfg := cert.Config{
		Path:       pathResolver.PkiPath(),
		BaseName:   "ca",
		CommonName: "kubernetes",
		Year:       100,
	}
	caCert, caKey, err := cert.NewCaCertAndKey(caCfg)
	if err != nil {
		t.Fatalf("NewCaCertAndKey() error = %v", err)
	}
	if err := cert.WriteCertAndKey(pathResolver.PkiPath(), "ca", caCert, caKey); err != nil {
		t.Fatalf("WriteCertAndKey() error = %v", err)
	}

	rt := &KubeadmRuntime{
		cluster:       cluster,
		config:        &runtimetypes.Config{APIServerDomain: constants.DefaultAPIServerDomain},
		kubeadmConfig: runtimetypes.NewKubeadmConfig(),
		pathResolver:  pathResolver,
	}

	if err := rt.RefreshKubeConfigFiles(true); err != nil {
		t.Fatalf("RefreshKubeConfigFiles(true) error = %v", err)
	}

	for _, name := range []string{AdminConf, ControllerConf, SchedulerConf, KubeletConf} {
		if _, err := os.Stat(filepath.Join(pathResolver.EtcPath(), name)); err != nil {
			t.Fatalf("Stat(%q) error = %v", name, err)
		}
	}

	kubeletCfg, err := clientcmd.LoadFromFile(filepath.Join(pathResolver.EtcPath(), KubeletConf))
	if err != nil {
		t.Fatalf("LoadFromFile(kubelet.conf) error = %v", err)
	}
	ctx := kubeletCfg.Contexts[kubeletCfg.CurrentContext]
	if ctx == nil {
		t.Fatalf("expected kubelet context")
	}
	if got, want := ctx.AuthInfo, "system:node:master-0"; got != want {
		t.Fatalf("ctx.AuthInfo = %q, want %q", got, want)
	}
}
