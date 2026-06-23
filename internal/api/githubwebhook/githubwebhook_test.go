package githubwebhook

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"testing"

	"github.com/stretchr/testify/require"
)

// sign produces the "sha256=<hex>" header GitHub would send for body+secret.
// It uses the stdlib directly as an independent oracle for Verify.
func sign(secret string, body []byte) string {
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write(body)
	return signaturePrefix + hex.EncodeToString(mac.Sum(nil))
}

func TestVerify(t *testing.T) {
	t.Parallel()

	secret := "s3cr3t"
	body := []byte(`{"zen":"Keep it logically awesome."}`)
	valid := sign(secret, body)

	tests := []struct {
		name   string
		secret string
		body   []byte
		header string
		want   bool
	}{
		{name: "valid signature", secret: secret, body: body, header: valid, want: true},
		{name: "wrong secret", secret: "other", body: body, header: valid, want: false},
		{name: "tampered body", secret: secret, body: []byte(`{"zen":"tampered"}`), header: valid, want: false},
		{name: "missing prefix", secret: secret, body: body, header: valid[len(signaturePrefix):], want: false},
		{name: "garbage signature", secret: secret, body: body, header: "sha256=not-hex", want: false},
		{name: "empty secret fails closed", secret: "", body: body, header: valid, want: false},
		{name: "empty header fails closed", secret: secret, body: body, header: "", want: false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			require.Equal(t, tt.want, Verify(tt.secret, tt.body, tt.header))
		})
	}
}

func TestParsePush(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		body      string
		wantOwner string
		wantName  string
		wantOK    bool
	}{
		{
			name:      "owner login and repo name",
			body:      `{"repository":{"name":"widget","full_name":"octo/widget","owner":{"login":"octo"}}}`,
			wantOwner: "octo",
			wantName:  "widget",
			wantOK:    true,
		},
		{
			name:      "falls back to owner.name when login absent",
			body:      `{"repository":{"name":"widget","owner":{"name":"octo"}}}`,
			wantOwner: "octo",
			wantName:  "widget",
			wantOK:    true,
		},
		{
			name:      "falls back to full_name when owner/name absent",
			body:      `{"repository":{"full_name":"octo/widget","owner":{}}}`,
			wantOwner: "octo",
			wantName:  "widget",
			wantOK:    true,
		},
		{
			name:   "no repository object",
			body:   `{"zen":"ping"}`,
			wantOK: false,
		},
		{
			name:   "invalid json",
			body:   `not json`,
			wantOK: false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			owner, name, ok := ParsePush([]byte(tt.body))
			require.Equal(t, tt.wantOK, ok)
			if tt.wantOK {
				require.Equal(t, tt.wantOwner, owner)
				require.Equal(t, tt.wantName, name)
			}
		})
	}
}
