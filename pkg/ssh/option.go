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
	"net"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/containers/storage/pkg/homedir"
	"github.com/spf13/pflag"
	"golang.org/x/crypto/ssh"

	"github.com/labring/sealos/pkg/utils/file"
)

type Option struct {
	stdout            bool
	sudo              bool
	user              string
	password          string
	privateKey        string
	rawPrivateKeyData string
	passphrase        string
	timeout           time.Duration
	hostKeyCallback   ssh.HostKeyCallback
}

func (o *Option) BindFlags(fs *pflag.FlagSet) {
	fs.BoolVar(&o.stdout, "stdout", o.stdout, "print logs to stdout")
	fs.BoolVar(&o.sudo, "sudo", o.sudo, "enable sudo, user provide must be a superuser or sudoer")
	fs.StringVarP(&o.user, "user", "u", o.user, "username to authenticate as")
	fs.StringVarP(&o.password, "password", "p", o.password, "use given password to authenticate with")
	fs.StringVarP(&o.privateKey, "private-key", "i", o.privateKey,
		"selects a file from which the identity (private key) for public key authentication is read")
	fs.StringVar(&o.passphrase, "passphrase", o.passphrase, "passphrase for decrypting a PEM encoded private key")
	fs.DurationVar(&o.timeout, "timeout", o.timeout, "ssh connection establish timeout")
}

const (
	defaultUsername = "root"
)

func NewOption() *Option {
	homeDir := homedir.Get()
	opt := &Option{
		user:       defaultUsername,
		privateKey: defaultSSHPrivateKey(homeDir),
		timeout:    10 * time.Second,
		hostKeyCallback: func(hostname string, remote net.Addr, key ssh.PublicKey) error {
			return nil
		},
	}
	return opt
}

func defaultSSHPrivateKey(homeDir string) string {
	if key := identityFileFromSSHConfig(homeDir); key != "" {
		return key
	}
	return firstExistingSSHFile(homeDir, "id_rsa", "id_dsa", "id_ecdsa", "id_ed25519")
}

func firstExistingSSHFile(homeDir string, filenames ...string) string {
	for _, fn := range filenames {
		absPath := filepath.Join(homeDir, ".ssh", fn)
		if file.IsExist(absPath) {
			return absPath
		}
	}
	return ""
}

func identityFileFromSSHConfig(homeDir string) string {
	data, err := os.ReadFile(filepath.Join(homeDir, ".ssh", "config"))
	if err != nil {
		return ""
	}

	active := true
	for _, rawLine := range strings.Split(string(data), "\n") {
		line := strings.TrimSpace(rawLine)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		fields := strings.Fields(line)
		if len(fields) < 2 {
			continue
		}
		switch strings.ToLower(fields[0]) {
		case "host":
			active = false
			for _, pattern := range fields[1:] {
				if pattern == "*" {
					active = true
					break
				}
			}
		case "identityfile":
			if !active {
				continue
			}
			identityFile := expandSSHIdentityFile(homeDir, fields[1])
			if file.IsExist(identityFile) {
				return identityFile
			}
		}
	}
	return ""
}

func expandSSHIdentityFile(homeDir, identityFile string) string {
	identityFile = strings.Trim(identityFile, `"'`)
	if identityFile == "~" {
		return homeDir
	}
	if strings.HasPrefix(identityFile, "~/") {
		return filepath.Join(homeDir, identityFile[2:])
	}
	identityFile = os.Expand(identityFile, func(key string) string {
		if key == "HOME" {
			return homeDir
		}
		return ""
	})
	if filepath.IsAbs(identityFile) {
		return identityFile
	}
	return filepath.Join(homeDir, ".ssh", identityFile)
}

type OptionFunc func(*Option)

func WithSudoEnable(b bool) OptionFunc {
	return func(o *Option) {
		o.sudo = b
	}
}

func WithStdoutEnable(b bool) OptionFunc {
	return func(o *Option) {
		o.stdout = b
	}
}

func WithUsername(u string) OptionFunc {
	return func(o *Option) {
		o.user = u
	}
}

func WithPassword(p string) OptionFunc {
	return func(o *Option) {
		o.password = p
	}
}

func WithRawPrivateKeyDataAndPhrase(raw, passphrase string) OptionFunc {
	return func(o *Option) {
		o.rawPrivateKeyData = raw
		o.passphrase = passphrase
	}
}

func WithPrivateKeyAndPhrase(pk, passphrase string) OptionFunc {
	return func(o *Option) {
		o.privateKey = pk
		o.passphrase = passphrase
	}
}

func WithTimeout(timeout time.Duration) OptionFunc {
	if timeout == 0 {
		timeout = 10 * time.Second
	}
	return func(o *Option) {
		o.timeout = timeout
	}
}

func WithHostKeyCallback(fn ssh.HostKeyCallback) OptionFunc {
	return func(o *Option) {
		o.hostKeyCallback = fn
	}
}
