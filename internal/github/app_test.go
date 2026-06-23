package github

import (
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/pem"
	"testing"

	"github.com/stretchr/testify/require"
)

func testPrivateKeyPEM(t *testing.T) []byte {
	t.Helper()
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	require.NoError(t, err)
	return pem.EncodeToMemory(&pem.Block{
		Type:  "RSA PRIVATE KEY",
		Bytes: x509.MarshalPKCS1PrivateKey(key),
	})
}

func TestNewAppTokenProvider(t *testing.T) {
	t.Parallel()

	t.Run("valid key constructs a provider", func(t *testing.T) {
		t.Parallel()
		p, err := NewAppTokenProvider(12345, testPrivateKeyPEM(t))
		require.NoError(t, err)
		require.NotNil(t, p)
	})

	t.Run("invalid key is rejected", func(t *testing.T) {
		t.Parallel()
		_, err := NewAppTokenProvider(12345, []byte("not a pem key"))
		require.Error(t, err)
	})
}
