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

package kubernetes

import (
	"fmt"
	"os"
	"path"

	fileutil "github.com/labring/sealos/pkg/utils/file"
	"github.com/labring/sealos/pkg/utils/rand"
)

var generateKubeadmCertificateKey = rand.CreateCertificateKey

var writeKubeadmTokenCache = func(k *KubeadmRuntime, file string) error {
	return k.writeTokenFile(file)
}

func (k *KubeadmRuntime) RefreshKubeadmCache(refreshToken, refreshCertificateKey bool) error {
	if !refreshToken && !refreshCertificateKey {
		refreshToken = true
		refreshCertificateKey = true
	}
	if err := os.MkdirAll(k.pathResolver.EtcPath(), 0o755); err != nil {
		return err
	}

	if refreshCertificateKey {
		if err := k.rotateLocalCertificateKey(); err != nil {
			return err
		}
		// kubeadm-token.json embeds the certificate key, so it must be kept in sync.
		refreshToken = true
	}

	if refreshToken {
		k.token = nil
		tokenFile := path.Join(k.pathResolver.EtcPath(), defaultKubeadmTokenFileName)
		if err := os.Remove(tokenFile); err != nil && !os.IsNotExist(err) {
			return err
		}
		if err := writeKubeadmTokenCache(k, tokenFile); err != nil {
			return fmt.Errorf("failed to refresh kubeadm token cache: %w", err)
		}
	}

	return nil
}

func (k *KubeadmRuntime) rotateLocalCertificateKey() error {
	key, err := generateKubeadmCertificateKey()
	if err != nil {
		return err
	}

	certificateKeyFile := path.Join(k.pathResolver.EtcPath(), defaultCertificateKeyFileName)
	if err := fileutil.WriteFile(certificateKeyFile, []byte(key)); err != nil {
		return err
	}
	k.setInitCertificateKey(key)
	return nil
}
