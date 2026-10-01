// Package testpki issues short-lived certificates for tests that exercise
// mutual TLS. It exists so that tests authenticate the way production does,
// against a real certificate chain, rather than against a stubbed identity.
package testpki

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"math/big"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// PKI is a throwaway certificate authority rooted in a temporary directory.
type PKI struct {
	Dir string

	// CAFile is the PEM-encoded certificate authority on disk.
	CAFile string

	// ServingCertFile and ServingKeyFile are a server certificate valid for
	// localhost and 127.0.0.1.
	ServingCertFile string
	ServingKeyFile  string

	caCert *x509.Certificate
	caKey  *ecdsa.PrivateKey
	caPEM  []byte
}

// New creates a certificate authority and a serving certificate for localhost.
func New(t *testing.T) *PKI {
	t.Helper()
	dir := t.TempDir()

	caKey, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatalf("failed generating CA key: %v", err)
	}

	template := &x509.Certificate{
		SerialNumber:          big.NewInt(1),
		Subject:               pkix.Name{CommonName: "activity-test-ca"},
		NotBefore:             time.Now().Add(-time.Hour),
		NotAfter:              time.Now().Add(24 * time.Hour),
		IsCA:                  true,
		KeyUsage:              x509.KeyUsageCertSign | x509.KeyUsageDigitalSignature,
		BasicConstraintsValid: true,
	}

	der, err := x509.CreateCertificate(rand.Reader, template, template, &caKey.PublicKey, caKey)
	if err != nil {
		t.Fatalf("failed creating CA certificate: %v", err)
	}
	caCert, err := x509.ParseCertificate(der)
	if err != nil {
		t.Fatalf("failed parsing CA certificate: %v", err)
	}

	caPEM := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})
	caFile := filepath.Join(dir, "ca.crt")
	if err := os.WriteFile(caFile, caPEM, 0o600); err != nil {
		t.Fatalf("failed writing CA: %v", err)
	}

	p := &PKI{Dir: dir, CAFile: caFile, caCert: caCert, caKey: caKey, caPEM: caPEM}
	p.ServingCertFile, p.ServingKeyFile = p.IssueToDisk(t, "localhost", true)
	return p
}

// Issue returns a certificate signed by the CA. Server certificates are valid
// for localhost and 127.0.0.1; client certificates carry only the common name,
// which is the identity the ingest path resolves a cluster from.
func (p *PKI) Issue(t *testing.T, commonName string, server bool) tls.Certificate {
	t.Helper()

	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatalf("failed generating key: %v", err)
	}

	serial, err := rand.Int(rand.Reader, new(big.Int).Lsh(big.NewInt(1), 128))
	if err != nil {
		t.Fatalf("failed generating serial: %v", err)
	}

	template := &x509.Certificate{
		SerialNumber: serial,
		Subject:      pkix.Name{CommonName: commonName},
		NotBefore:    time.Now().Add(-time.Hour),
		NotAfter:     time.Now().Add(24 * time.Hour),
		KeyUsage:     x509.KeyUsageDigitalSignature,
	}
	if server {
		template.ExtKeyUsage = []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth}
		template.DNSNames = []string{"localhost"}
		template.IPAddresses = []net.IP{net.ParseIP("127.0.0.1")}
	} else {
		template.ExtKeyUsage = []x509.ExtKeyUsage{x509.ExtKeyUsageClientAuth}
	}

	der, err := x509.CreateCertificate(rand.Reader, template, p.caCert, &key.PublicKey, p.caKey)
	if err != nil {
		t.Fatalf("failed creating certificate: %v", err)
	}

	keyDER, err := x509.MarshalECPrivateKey(key)
	if err != nil {
		t.Fatalf("failed encoding key: %v", err)
	}

	certificate, err := tls.X509KeyPair(
		pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}),
		pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: keyDER}),
	)
	if err != nil {
		t.Fatalf("failed building key pair: %v", err)
	}

	return certificate
}

// IssueToDisk issues a certificate and writes the PEM pair to the PKI
// directory, returning the certificate and key paths.
func (p *PKI) IssueToDisk(t *testing.T, commonName string, server bool) (string, string) {
	t.Helper()

	certificate := p.Issue(t, commonName, server)

	certPath := filepath.Join(p.Dir, commonName+".crt")
	keyPath := filepath.Join(p.Dir, commonName+".key")

	keyDER, err := x509.MarshalECPrivateKey(certificate.PrivateKey.(*ecdsa.PrivateKey))
	if err != nil {
		t.Fatalf("failed encoding key: %v", err)
	}

	certPEM := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: certificate.Certificate[0]})
	if err := os.WriteFile(certPath, certPEM, 0o600); err != nil {
		t.Fatalf("failed writing certificate: %v", err)
	}
	if err := os.WriteFile(keyPath, pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: keyDER}), 0o600); err != nil {
		t.Fatalf("failed writing key: %v", err)
	}

	return certPath, keyPath
}

// Client returns an HTTP client that trusts the CA and presents a client
// certificate with the given common name.
func (p *PKI) Client(t *testing.T, commonName string) *http.Client {
	t.Helper()

	return &http.Client{
		Timeout: 30 * time.Second,
		Transport: &http.Transport{TLSClientConfig: &tls.Config{
			RootCAs:      p.CertPool(),
			Certificates: []tls.Certificate{p.Issue(t, commonName, false)},
			MinVersion:   tls.VersionTLS12,
		}},
	}
}

// AnonymousClient returns an HTTP client that trusts the CA but presents no
// client certificate.
func (p *PKI) AnonymousClient() *http.Client {
	return &http.Client{
		Timeout: 30 * time.Second,
		Transport: &http.Transport{TLSClientConfig: &tls.Config{
			RootCAs:    p.CertPool(),
			MinVersion: tls.VersionTLS12,
		}},
	}
}

// CertPool returns a pool containing the CA certificate.
func (p *PKI) CertPool() *x509.CertPool {
	pool := x509.NewCertPool()
	pool.AppendCertsFromPEM(p.caPEM)
	return pool
}
