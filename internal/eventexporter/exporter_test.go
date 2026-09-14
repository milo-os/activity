package eventexporter

import (
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"math/big"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// writeTestCertKeyPair generates a self-signed certificate and key, writes
// them as PEM files under dir, and returns their paths.
func writeTestCertKeyPair(t *testing.T, dir string) (certFile, keyFile string) {
	t.Helper()

	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("failed to generate key: %v", err)
	}

	template := &x509.Certificate{
		SerialNumber: big.NewInt(1),
		Subject:      pkix.Name{CommonName: "test"},
		NotBefore:    time.Now(),
		NotAfter:     time.Now().Add(time.Hour),
	}

	certDER, err := x509.CreateCertificate(rand.Reader, template, template, &key.PublicKey, key)
	if err != nil {
		t.Fatalf("failed to create certificate: %v", err)
	}

	certFile = filepath.Join(dir, "tls.crt")
	certOut, err := os.Create(certFile)
	if err != nil {
		t.Fatalf("failed to create cert file: %v", err)
	}
	defer certOut.Close()
	if err := pem.Encode(certOut, &pem.Block{Type: "CERTIFICATE", Bytes: certDER}); err != nil {
		t.Fatalf("failed to write cert PEM: %v", err)
	}

	keyFile = filepath.Join(dir, "tls.key")
	keyOut, err := os.Create(keyFile)
	if err != nil {
		t.Fatalf("failed to create key file: %v", err)
	}
	defer keyOut.Close()
	if err := pem.Encode(keyOut, &pem.Block{Type: "RSA PRIVATE KEY", Bytes: x509.MarshalPKCS1PrivateKey(key)}); err != nil {
		t.Fatalf("failed to write key PEM: %v", err)
	}

	return certFile, keyFile
}

func TestBuildNATSTLSConfig(t *testing.T) {
	dir := t.TempDir()
	certFile, keyFile := writeTestCertKeyPair(t, dir)

	// Reuse the self-signed cert as a stand-in CA file; buildNATSTLSConfig
	// only needs a well-formed PEM certificate to build a cert pool from.
	caFile := certFile

	tests := []struct {
		name    string
		cfg     Config
		wantErr bool
	}{
		{
			name: "no cert, key, or CA configured",
			cfg:  Config{},
		},
		{
			name: "valid cert and key",
			cfg:  Config{NATSTLSCertFile: certFile, NATSTLSKeyFile: keyFile},
		},
		{
			name:    "missing cert file",
			cfg:     Config{NATSTLSCertFile: filepath.Join(dir, "missing.crt"), NATSTLSKeyFile: keyFile},
			wantErr: true,
		},
		{
			name:    "missing key file",
			cfg:     Config{NATSTLSCertFile: certFile, NATSTLSKeyFile: filepath.Join(dir, "missing.key")},
			wantErr: true,
		},
		{
			name:    "cert file without key file",
			cfg:     Config{NATSTLSCertFile: certFile},
			wantErr: true,
		},
		{
			name:    "key file without cert file",
			cfg:     Config{NATSTLSKeyFile: keyFile},
			wantErr: true,
		},
		{
			name: "valid CA file",
			cfg:  Config{NATSTLSCAFile: caFile},
		},
		{
			name:    "missing CA file",
			cfg:     Config{NATSTLSCAFile: filepath.Join(dir, "missing-ca.crt")},
			wantErr: true,
		},
		{
			name:    "invalid CA file contents",
			cfg:     Config{NATSTLSCAFile: keyFile},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tlsConfig, err := buildNATSTLSConfig(tt.cfg)
			if tt.wantErr {
				if err == nil {
					t.Fatal("buildNATSTLSConfig() error = nil, want an error")
				}
				return
			}
			if err != nil {
				t.Fatalf("buildNATSTLSConfig() error = %v, want nil", err)
			}
			if tlsConfig == nil {
				t.Fatal("buildNATSTLSConfig() returned nil TLS config")
			}
			if tt.cfg.NATSTLSCertFile != "" && len(tlsConfig.Certificates) != 1 {
				t.Errorf("Certificates = %d entries, want 1", len(tlsConfig.Certificates))
			}
			if tt.cfg.NATSTLSCAFile != "" && tlsConfig.RootCAs == nil {
				t.Error("RootCAs = nil, want a populated pool")
			}
		})
	}
}
