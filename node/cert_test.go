package node

import (
	"crypto/tls"
	"os"
	"path/filepath"
	"testing"
)

func Test_generateSelfSslCertificate(t *testing.T) {
	certPath := filepath.Join(t.TempDir(), "cert.pem")
	keyPath := filepath.Join(t.TempDir(), "key.pem")
	if err := generateSelfSslCertificate("domain.com", certPath, keyPath); err != nil {
		t.Fatal(err)
	}
	cert, err := os.ReadFile(certPath)
	if err != nil {
		t.Fatal(err)
	}
	key, err := os.ReadFile(keyPath)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := tls.X509KeyPair(cert, key); err != nil {
		t.Fatalf("generated certificate cannot be used for TLS: %v", err)
	}
}
