// Package natsconn describes a NATS endpoint: a server URL plus the TLS
// material needed to reach it. A component that talks to more than one broker
// expresses every connection the same way.
package natsconn

import (
	"crypto/tls"
	"crypto/x509"
	"fmt"
	"os"

	"github.com/nats-io/nats.go"
	"k8s.io/klog/v2"
)

// TLSFiles is the TLS material for a single NATS connection.
type TLSFiles struct {
	Enabled  bool
	CertFile string
	KeyFile  string
	CAFile   string
}

// IsZero reports whether no TLS material was configured at all.
func (t TLSFiles) IsZero() bool {
	return !t.Enabled && t.CertFile == "" && t.KeyFile == "" && t.CAFile == ""
}

// InheritFrom fills each unset field from base, so a second broker sharing the
// first's CA and client identity needs no extra configuration. Enabled is a
// bool and cannot be "unset" on its own, so a wholly empty t takes base as-is.
func (t TLSFiles) InheritFrom(base TLSFiles) TLSFiles {
	if t.IsZero() {
		return base
	}
	if t.CertFile == "" {
		t.CertFile = base.CertFile
	}
	if t.KeyFile == "" {
		t.KeyFile = base.KeyFile
	}
	if t.CAFile == "" {
		t.CAFile = base.CAFile
	}
	return t
}

// Config builds the TLS configuration these files describe.
func (t TLSFiles) Config() (*tls.Config, error) {
	tlsConfig := &tls.Config{
		MinVersion: tls.VersionTLS12,
	}

	// Neither file means server verification only, which is legitimate; only one
	// would present no certificate and fail later as an opaque handshake rejection.
	switch {
	case t.CertFile != "" && t.KeyFile != "":
		cert, err := tls.LoadX509KeyPair(t.CertFile, t.KeyFile)
		if err != nil {
			return nil, fmt.Errorf("failed to load NATS client certificate: %w", err)
		}
		tlsConfig.Certificates = []tls.Certificate{cert}
		klog.V(2).InfoS("Loaded NATS client certificate",
			"certFile", t.CertFile,
			"keyFile", t.KeyFile,
		)
	case t.CertFile != "":
		return nil, fmt.Errorf("failed to load NATS client certificate: no key file configured for %s", t.CertFile)
	case t.KeyFile != "":
		return nil, fmt.Errorf("failed to load NATS client certificate: no certificate file configured for %s", t.KeyFile)
	}

	if t.CAFile != "" {
		caCert, err := os.ReadFile(t.CAFile)
		if err != nil {
			return nil, fmt.Errorf("failed to read NATS CA certificate: %w", err)
		}
		caCertPool := x509.NewCertPool()
		if !caCertPool.AppendCertsFromPEM(caCert) {
			return nil, fmt.Errorf("failed to parse NATS CA certificate")
		}
		tlsConfig.RootCAs = caCertPool
		klog.V(2).InfoS("Loaded NATS CA certificate", "caFile", t.CAFile)
	}

	return tlsConfig, nil
}

// Endpoint is a NATS server and the TLS material needed to reach it.
type Endpoint struct {
	URL string
	TLS TLSFiles
}

// IsSet reports whether a server was configured for this endpoint.
func (e Endpoint) IsSet() bool {
	return e.URL != ""
}

// options builds the option list Connect dials with. Reconnect behaviour and
// handlers stay with the caller: connections differ in what they instrument.
func (e Endpoint) options(name string, extra ...nats.Option) ([]nats.Option, error) {
	opts := make([]nats.Option, 0, len(extra)+2)
	opts = append(opts, nats.Name(name))
	opts = append(opts, extra...)

	if e.TLS.Enabled {
		tlsConfig, err := e.TLS.Config()
		if err != nil {
			return nil, fmt.Errorf("failed to build NATS TLS config: %w", err)
		}
		opts = append(opts, nats.Secure(tlsConfig))
		klog.InfoS("NATS TLS enabled", "client", name)
	}

	return opts, nil
}

// Connect dials the endpoint, identifying itself to the server as name.
func (e Endpoint) Connect(name string, opts ...nats.Option) (*nats.Conn, error) {
	connOpts, err := e.options(name, opts...)
	if err != nil {
		return nil, err
	}
	return nats.Connect(e.URL, connOpts...)
}
