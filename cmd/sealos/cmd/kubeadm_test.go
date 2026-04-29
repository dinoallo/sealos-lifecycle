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
