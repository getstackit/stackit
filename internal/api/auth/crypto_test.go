package auth

import (
	"encoding/base64"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestCipher_RoundTrip(t *testing.T) {
	t.Parallel()

	key, err := GenerateSessionKey()
	require.NoError(t, err)

	c, err := NewCipher(key)
	require.NoError(t, err)

	plain := []byte("ghp_super_secret_personal_access_token")
	blob, err := c.Seal(plain)
	require.NoError(t, err)
	require.NotEqual(t, plain, blob, "ciphertext must differ from plaintext")

	got, err := c.Open(blob)
	require.NoError(t, err)
	require.Equal(t, plain, got)
}

func TestCipher_DifferentNoncesPerSeal(t *testing.T) {
	t.Parallel()

	key, _ := GenerateSessionKey()
	c, _ := NewCipher(key)

	a, _ := c.Seal([]byte("same"))
	b, _ := c.Seal([]byte("same"))

	require.NotEqual(t, a, b, "two seals of the same plaintext must use different nonces")
}

func TestCipher_RejectsTamperedBlob(t *testing.T) {
	t.Parallel()

	key, _ := GenerateSessionKey()
	c, _ := NewCipher(key)

	blob, _ := c.Seal([]byte("hello"))
	blob[len(blob)-1] ^= 0x01 // flip last bit of the auth tag

	_, err := c.Open(blob)
	require.Error(t, err)
}

func TestNewCipher_RejectsBadKeys(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name string
		key  string
	}{
		{"empty", ""},
		{"not base64", "@@@not base64@@@"},
		{"too short", base64.StdEncoding.EncodeToString(make([]byte, 16))},
		{"too long", base64.StdEncoding.EncodeToString(make([]byte, 64))},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			_, err := NewCipher(tc.key)
			require.Error(t, err)
		})
	}
}

func TestCipher_OpenRejectsShortBlob(t *testing.T) {
	t.Parallel()

	key, _ := GenerateSessionKey()
	c, _ := NewCipher(key)

	_, err := c.Open([]byte("x"))
	require.Error(t, err)
	require.True(t, strings.Contains(err.Error(), "too short"))
}
