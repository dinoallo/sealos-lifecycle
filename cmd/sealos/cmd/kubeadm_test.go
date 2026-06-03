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

package cmd

import "testing"

func TestResolveKubeadmCacheRefreshSelection(t *testing.T) {
	tests := []struct {
		name                   string
		refreshAll             bool
		refreshToken           bool
		refreshCertificateKey  bool
		wantRefreshToken       bool
		wantRefreshCertificate bool
	}{
		{
			name:                   "default all",
			wantRefreshToken:       true,
			wantRefreshCertificate: true,
		},
		{
			name:                   "explicit all",
			refreshAll:             true,
			wantRefreshToken:       true,
			wantRefreshCertificate: true,
		},
		{
			name:                   "token only",
			refreshToken:           true,
			wantRefreshToken:       true,
			wantRefreshCertificate: false,
		},
		{
			name:                   "certificate key implies token",
			refreshCertificateKey:  true,
			wantRefreshToken:       true,
			wantRefreshCertificate: true,
		},
		{
			name:                   "both explicit",
			refreshToken:           true,
			refreshCertificateKey:  true,
			wantRefreshToken:       true,
			wantRefreshCertificate: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gotToken, gotCertificate := resolveKubeadmCacheRefreshSelection(tt.refreshAll, tt.refreshToken, tt.refreshCertificateKey)
			if gotToken != tt.wantRefreshToken || gotCertificate != tt.wantRefreshCertificate {
				t.Fatalf("resolveKubeadmCacheRefreshSelection(%t, %t, %t) = (%t, %t), want (%t, %t)",
					tt.refreshAll, tt.refreshToken, tt.refreshCertificateKey,
					gotToken, gotCertificate,
					tt.wantRefreshToken, tt.wantRefreshCertificate)
			}
		})
	}
}
