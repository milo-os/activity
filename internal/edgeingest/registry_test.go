package edgeingest

import (
	"crypto/x509"
	"crypto/x509/pkix"
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func writeRegistry(t *testing.T, contents string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "clusters.yaml")
	if err := os.WriteFile(path, []byte(contents), 0o600); err != nil {
		t.Fatalf("failed writing registry: %v", err)
	}
	return path
}

func chainFor(commonName string) [][]*x509.Certificate {
	return [][]*x509.Certificate{{{Subject: pkix.Name{CommonName: commonName}}}}
}

func TestLoadClusterRegistry(t *testing.T) {
	path := writeRegistry(t, `
clusters:
  - clientCommonName: audit-shipper.dfw1.edge.datum.net
    name: dfw1
    location: us-central-1
  - clientCommonName: audit-shipper.iad1.edge.datum.net
    name: iad1
    location: us-east-1
`)

	registry, err := LoadClusterRegistry(path)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if registry.Len() != 2 {
		t.Fatalf("loaded %d clusters, want 2", registry.Len())
	}

	identity, err := registry.Identify(chainFor("audit-shipper.iad1.edge.datum.net"))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if identity.Name != "iad1" || identity.Location != "us-east-1" {
		t.Errorf("identity = %+v", identity)
	}
}

func TestLoadClusterRegistryRejectsIncompleteEntries(t *testing.T) {
	tests := map[string]string{
		"no common name":  "clusters:\n  - name: dfw1\n    location: us-central-1\n",
		"no cluster name": "clusters:\n  - clientCommonName: a\n    location: us-central-1\n",
		"no location":     "clusters:\n  - clientCommonName: a\n    name: dfw1\n",
		"no clusters":     "clusters: []\n",
		"duplicate":       "clusters:\n  - clientCommonName: a\n    name: dfw1\n    location: us-central-1\n  - clientCommonName: a\n    name: iad1\n    location: us-east-1\n",
	}

	for name, contents := range tests {
		t.Run(name, func(t *testing.T) {
			if _, err := LoadClusterRegistry(writeRegistry(t, contents)); err == nil {
				t.Fatal("registry was accepted")
			}
		})
	}
}

func TestIdentifyRejectsUnknownAndAnonymousClients(t *testing.T) {
	registry := registryForTest()

	if _, err := registry.Identify(chainFor("attacker.example.com")); !errors.Is(err, ErrUnknownClient) {
		t.Errorf("unknown client returned %v, want ErrUnknownClient", err)
	}

	if _, err := registry.Identify(nil); !errors.Is(err, ErrUnknownClient) {
		t.Errorf("anonymous client returned %v, want ErrUnknownClient", err)
	}
}

func TestReloadReplacesRegistryContents(t *testing.T) {
	path := writeRegistry(t, "clusters:\n  - clientCommonName: a\n    name: dfw1\n    location: us-central-1\n")

	registry, err := LoadClusterRegistry(path)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if err := os.WriteFile(path, []byte("clusters:\n  - clientCommonName: b\n    name: iad1\n    location: us-east-1\n"), 0o600); err != nil {
		t.Fatalf("failed rewriting registry: %v", err)
	}
	if err := registry.Reload(path); err != nil {
		t.Fatalf("reload returned %v", err)
	}

	if _, err := registry.Identify(chainFor("a")); !errors.Is(err, ErrUnknownClient) {
		t.Error("a revoked identity still resolves after reload")
	}
	if _, err := registry.Identify(chainFor("b")); err != nil {
		t.Errorf("the new identity does not resolve after reload: %v", err)
	}
}
