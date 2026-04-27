// Copyright 2024 sealos.
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

package hydrate

import (
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strconv"

	"github.com/opencontainers/go-digest"
)

func DigestBundle(root string) (digest.Digest, error) {
	if root == "" {
		return "", fmt.Errorf("bundle root cannot be empty")
	}

	root = filepath.Clean(root)
	info, err := os.Stat(root)
	if err != nil {
		return "", fmt.Errorf("stat bundle root %q: %w", root, err)
	}
	if !info.IsDir() {
		return "", fmt.Errorf("bundle root %q must be a directory", root)
	}

	digester := digest.Canonical.Digester()
	if err := filepath.WalkDir(root, func(current string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if current == root {
			return nil
		}

		relative, err := filepath.Rel(root, current)
		if err != nil {
			return err
		}
		relative = filepath.ToSlash(relative)

		info, err := os.Lstat(current)
		if err != nil {
			return err
		}

		switch mode := info.Mode(); {
		case mode.IsDir():
			return writeDigestRecord(digester.Hash(), "dir", relative)
		case mode.Type()&os.ModeSymlink != 0:
			target, err := os.Readlink(current)
			if err != nil {
				return err
			}
			return writeDigestRecord(digester.Hash(), "symlink", relative, target)
		case mode.IsRegular():
			fileDigest, size, err := digestFile(current)
			if err != nil {
				return err
			}
			return writeDigestRecord(digester.Hash(),
				"file",
				relative,
				strconv.FormatUint(uint64(info.Mode().Perm()), 8),
				strconv.FormatInt(size, 10),
				fileDigest.String(),
			)
		default:
			return fmt.Errorf("unsupported bundle entry %q with mode %v", relative, info.Mode())
		}
	}); err != nil {
		return "", fmt.Errorf("walk bundle %q: %w", root, err)
	}

	return digester.Digest(), nil
}

func digestFile(path string) (digest.Digest, int64, error) {
	file, err := os.Open(path)
	if err != nil {
		return "", 0, err
	}
	defer file.Close()

	digester := digest.Canonical.Digester()
	size, err := io.Copy(digester.Hash(), file)
	if err != nil {
		return "", 0, err
	}
	return digester.Digest(), size, nil
}

func writeDigestRecord(w io.Writer, fields ...string) error {
	for _, field := range fields {
		if _, err := io.WriteString(w, field); err != nil {
			return err
		}
		if _, err := w.Write([]byte{0}); err != nil {
			return err
		}
	}
	return nil
}
