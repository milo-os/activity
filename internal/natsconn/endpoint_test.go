package natsconn

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"math/big"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/nats-io/nats.go"
)

// writeSelfSignedCert writes a cert/key pair into dir and returns their paths.
func writeSelfSignedCert(t *testing.T, dir string) (certPath, keyPath string) {
	t.Helper()

	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatalf("generate key: %v", err)
	}
	template := x509.Certificate{
		SerialNumber: big.NewInt(1),
		Subject:      pkix.Name{CommonName: "natsconn-test"},
		NotBefore:    time.Now().Add(-time.Hour),
		NotAfter:     time.Now().Add(time.Hour),
		IsCA:         true,
		KeyUsage:     x509.KeyUsageCertSign | x509.KeyUsageDigitalSignature,
	}
	der, err := x509.CreateCertificate(rand.Reader, &template, &template, &key.PublicKey, key)
	if err != nil {
		t.Fatalf("create certificate: %v", err)
	}
	keyDER, err := x509.MarshalECPrivateKey(key)
	if err != nil {
		t.Fatalf("marshal key: %v", err)
	}

	certPath = filepath.Join(dir, "tls.crt")
	keyPath = filepath.Join(dir, "tls.key")
	writeFile(t, certPath, pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}))
	writeFile(t, keyPath, pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: keyDER}))
	return certPath, keyPath
}

func writeFile(t *testing.T, path string, data []byte) {
	t.Helper()
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}

func TestTLSFilesInheritFrom(t *testing.T) {
	base := TLSFiles{
		Enabled:  true,
		CertFile: "/in/tls.crt",
		KeyFile:  "/in/tls.key",
		CAFile:   "/in/ca.crt",
	}

	tests := []struct {
		name string
		in   TLSFiles
		base TLSFiles
		want TLSFiles
	}{
		{
			name: "nothing set takes the base verbatim",
			in:   TLSFiles{},
			base: base,
			want: base,
		},
		{
			name: "only CA set inherits cert and key but not Enabled",
			in:   TLSFiles{CAFile: "/out/ca.crt"},
			base: base,
			want: TLSFiles{Enabled: false, CertFile: "/in/tls.crt", KeyFile: "/in/tls.key", CAFile: "/out/ca.crt"},
		},
		{
			name: "fully specified overrides the base",
			in: TLSFiles{
				Enabled:  true,
				CertFile: "/out/tls.crt",
				KeyFile:  "/out/tls.key",
				CAFile:   "/out/ca.crt",
			},
			base: base,
			want: TLSFiles{
				Enabled:  true,
				CertFile: "/out/tls.crt",
				KeyFile:  "/out/tls.key",
				CAFile:   "/out/ca.crt",
			},
		},
		{
			name: "enabled alone inherits every path",
			in:   TLSFiles{Enabled: true},
			base: base,
			want: base,
		},
		{
			name: "empty base leaves the input untouched",
			in:   TLSFiles{Enabled: true, CAFile: "/out/ca.crt"},
			base: TLSFiles{},
			want: TLSFiles{Enabled: true, CAFile: "/out/ca.crt"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.in.InheritFrom(tt.base); got != tt.want {
				t.Errorf("InheritFrom() = %+v, want %+v", got, tt.want)
			}
		})
	}
}

func TestTLSFilesIsZero(t *testing.T) {
	if !(TLSFiles{}).IsZero() {
		t.Error("empty TLSFiles should be zero")
	}
	for _, s := range []TLSFiles{
		{Enabled: true},
		{CertFile: "a"},
		{KeyFile: "a"},
		{CAFile: "a"},
	} {
		if s.IsZero() {
			t.Errorf("%+v should not be zero", s)
		}
	}
}

func TestTLSFilesConfig(t *testing.T) {
	dir := t.TempDir()
	certPath, keyPath := writeSelfSignedCert(t, dir)

	t.Run("loads client certificate and CA", func(t *testing.T) {
		cfg, err := TLSFiles{Enabled: true, CertFile: certPath, KeyFile: keyPath, CAFile: certPath}.Config()
		if err != nil {
			t.Fatalf("Config() error: %v", err)
		}
		if cfg.MinVersion != tls.VersionTLS12 {
			t.Errorf("MinVersion = %d, want %d", cfg.MinVersion, tls.VersionTLS12)
		}
		if len(cfg.Certificates) != 1 {
			t.Errorf("Certificates = %d, want 1", len(cfg.Certificates))
		}
		if cfg.RootCAs == nil {
			t.Error("RootCAs not set")
		}
	})

	t.Run("no files still yields a minimum version", func(t *testing.T) {
		cfg, err := TLSFiles{Enabled: true}.Config()
		if err != nil {
			t.Fatalf("Config() error: %v", err)
		}
		if len(cfg.Certificates) != 0 || cfg.RootCAs != nil {
			t.Errorf("unexpected material: %+v", cfg)
		}
	})

	t.Run("a cert without a key errors", func(t *testing.T) {
		if _, err := (TLSFiles{Enabled: true, CertFile: certPath}).Config(); err == nil {
			t.Error("expected an error for a certificate without a key")
		}
	})

	t.Run("a key without a cert errors", func(t *testing.T) {
		if _, err := (TLSFiles{Enabled: true, KeyFile: keyPath}).Config(); err == nil {
			t.Error("expected an error for a key without a certificate")
		}
	})

	t.Run("missing CA file errors", func(t *testing.T) {
		if _, err := (TLSFiles{Enabled: true, CAFile: filepath.Join(dir, "absent.crt")}).Config(); err == nil {
			t.Error("expected an error for a missing CA file")
		}
	})

	t.Run("unparseable CA file errors", func(t *testing.T) {
		bad := filepath.Join(dir, "bad-ca.crt")
		writeFile(t, bad, []byte("not a certificate"))
		if _, err := (TLSFiles{Enabled: true, CAFile: bad}).Config(); err == nil {
			t.Error("expected an error for an unparseable CA file")
		}
	})

	t.Run("unreadable client certificate errors", func(t *testing.T) {
		bad := filepath.Join(dir, "bad.crt")
		writeFile(t, bad, []byte("not a certificate"))
		if _, err := (TLSFiles{Enabled: true, CertFile: bad, KeyFile: keyPath}).Config(); err == nil {
			t.Error("expected an error for an unparseable client certificate")
		}
	})
}

// applyOptions resolves an option list the way nats.Connect does.
func applyOptions(t *testing.T, opts []nats.Option) nats.Options {
	t.Helper()
	var o nats.Options
	for _, opt := range opts {
		if err := opt(&o); err != nil {
			t.Fatalf("apply option: %v", err)
		}
	}
	return o
}

func TestEndpointOptions(t *testing.T) {
	dir := t.TempDir()
	certPath, keyPath := writeSelfSignedCert(t, dir)

	t.Run("without TLS no secure transport is requested", func(t *testing.T) {
		e := Endpoint{URL: "nats://localhost:4222"}
		opts, err := e.options("activity-processor")
		if err != nil {
			t.Fatalf("options() error: %v", err)
		}
		o := applyOptions(t, opts)
		if o.Name != "activity-processor" {
			t.Errorf("Name = %q, want %q", o.Name, "activity-processor")
		}
		if o.Secure {
			t.Error("Secure set without TLS enabled")
		}
		if o.TLSConfig != nil {
			t.Error("TLSConfig set without TLS enabled")
		}
	})

	t.Run("TLS files are ignored while disabled", func(t *testing.T) {
		e := Endpoint{
			URL: "nats://localhost:4222",
			TLS: TLSFiles{CertFile: filepath.Join(dir, "absent.crt"), KeyFile: keyPath},
		}
		opts, err := e.options("activity-processor")
		if err != nil {
			t.Fatalf("options() error: %v", err)
		}
		if applyOptions(t, opts).Secure {
			t.Error("Secure set while TLS is disabled")
		}
	})

	t.Run("with TLS the config is attached", func(t *testing.T) {
		e := Endpoint{
			URL: "nats://localhost:4222",
			TLS: TLSFiles{Enabled: true, CertFile: certPath, KeyFile: keyPath, CAFile: certPath},
		}
		opts, err := e.options("activity-processor-output")
		if err != nil {
			t.Fatalf("options() error: %v", err)
		}
		o := applyOptions(t, opts)
		if o.Name != "activity-processor-output" {
			t.Errorf("Name = %q, want %q", o.Name, "activity-processor-output")
		}
		if !o.Secure || o.TLSConfig == nil {
			t.Fatalf("Secure = %v, TLSConfig = %v; want a secure connection", o.Secure, o.TLSConfig)
		}
		if o.TLSConfig.MinVersion != tls.VersionTLS12 {
			t.Errorf("MinVersion = %d, want %d", o.TLSConfig.MinVersion, tls.VersionTLS12)
		}
		if len(o.TLSConfig.Certificates) != 1 || o.TLSConfig.RootCAs == nil {
			t.Error("client certificate or CA missing from the dialled config")
		}
	})

	t.Run("caller options are kept", func(t *testing.T) {
		e := Endpoint{URL: "nats://localhost:4222"}
		opts, err := e.options("activity-processor", nats.MaxReconnects(-1), nats.ReconnectWait(time.Second))
		if err != nil {
			t.Fatalf("options() error: %v", err)
		}
		o := applyOptions(t, opts)
		if o.MaxReconnect != -1 || o.ReconnectWait != time.Second {
			t.Errorf("caller options lost: MaxReconnect=%d ReconnectWait=%v", o.MaxReconnect, o.ReconnectWait)
		}
	})

	t.Run("a broken certificate fails before dialling", func(t *testing.T) {
		e := Endpoint{
			URL: "nats://localhost:4222",
			TLS: TLSFiles{Enabled: true, CAFile: filepath.Join(dir, "absent.crt")},
		}
		if _, err := e.options("activity-processor"); err == nil {
			t.Error("expected an error for a missing CA file")
		}
	})
}

func TestEndpointIsSet(t *testing.T) {
	if (Endpoint{}).IsSet() {
		t.Error("an endpoint without a URL should not be set")
	}
	// TLS material alone must not make an endpoint connectable.
	if (Endpoint{TLS: TLSFiles{Enabled: true, CAFile: "/ca.crt"}}).IsSet() {
		t.Error("TLS material alone should not make an endpoint set")
	}
	if !(Endpoint{URL: "nats://localhost:4222"}).IsSet() {
		t.Error("an endpoint with a URL should be set")
	}
}
