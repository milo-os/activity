package edgeingest

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"fmt"
	"os"
	"time"

	"github.com/nats-io/nats.go"
	auditv1 "k8s.io/apiserver/pkg/apis/audit/v1"
	"k8s.io/klog/v2"

	"go.miloapis.com/activity/internal/metrics"
)

// NATSEmitterConfig configures publication of rewritten edge audit events.
type NATSEmitterConfig struct {
	URL string

	// StreamName is the JetStream stream the core audit pipeline already
	// consumes. Edge records join it rather than getting a stream of their own,
	// so Vector, the processor and ClickHouse need no edge-specific wiring.
	StreamName string

	// SubjectPrefix is prepended to the cluster name to form the subject.
	SubjectPrefix string

	TLSEnabled  bool
	TLSCertFile string
	TLSKeyFile  string
	TLSCAFile   string
}

// NATSEmitter publishes rewritten audit events to NATS JetStream.
type NATSEmitter struct {
	conn          *nats.Conn
	js            nats.JetStreamContext
	subjectPrefix string
}

var _ Emitter = (*NATSEmitter)(nil)

// NewNATSEmitter connects to NATS and returns an emitter.
func NewNATSEmitter(config NATSEmitterConfig) (*NATSEmitter, error) {
	if config.URL == "" {
		return nil, fmt.Errorf("NATS URL is required for edge audit ingest")
	}

	opts := []nats.Option{
		nats.Name("activity-edge-audit-ingest"),
		nats.RetryOnFailedConnect(true),
		nats.MaxReconnects(-1),
	}

	if config.TLSEnabled {
		tlsConfig, err := emitterTLSConfig(config)
		if err != nil {
			return nil, err
		}
		opts = append(opts, nats.Secure(tlsConfig))
	}

	conn, err := nats.Connect(config.URL, opts...)
	if err != nil {
		return nil, fmt.Errorf("failed connecting to NATS: %w", err)
	}

	js, err := conn.JetStream()
	if err != nil {
		conn.Close()
		return nil, fmt.Errorf("failed creating JetStream context: %w", err)
	}

	prefix := config.SubjectPrefix
	if prefix == "" {
		prefix = "audit.k8s.edge"
	}

	klog.InfoS("Connected to NATS for edge audit ingest",
		"url", config.URL,
		"stream", config.StreamName,
		"subjectPrefix", prefix,
	)

	return &NATSEmitter{conn: conn, js: js, subjectPrefix: prefix}, nil
}

func emitterTLSConfig(config NATSEmitterConfig) (*tls.Config, error) {
	tlsConfig := &tls.Config{MinVersion: tls.VersionTLS12}

	if config.TLSCertFile != "" && config.TLSKeyFile != "" {
		cert, err := tls.LoadX509KeyPair(config.TLSCertFile, config.TLSKeyFile)
		if err != nil {
			return nil, fmt.Errorf("failed loading NATS client certificate: %w", err)
		}
		tlsConfig.Certificates = []tls.Certificate{cert}
	}

	if config.TLSCAFile != "" {
		caCert, err := os.ReadFile(config.TLSCAFile)
		if err != nil {
			return nil, fmt.Errorf("failed reading NATS CA certificate: %w", err)
		}
		pool := x509.NewCertPool()
		if !pool.AppendCertsFromPEM(caCert) {
			return nil, fmt.Errorf("failed parsing NATS CA certificate")
		}
		tlsConfig.RootCAs = pool
	}

	return tlsConfig, nil
}

// Emit publishes an event on <prefix>.<cluster>, keyed by audit ID so a shipper
// that retries a batch does not duplicate records.
func (e *NATSEmitter) Emit(ctx context.Context, cluster string, event *auditv1.Event) error {
	data, err := json.Marshal(event)
	if err != nil {
		return fmt.Errorf("failed encoding audit event: %w", err)
	}

	start := time.Now()
	subject := fmt.Sprintf("%s.%s", e.subjectPrefix, cluster)

	if _, err := e.js.Publish(subject, data, nats.MsgId(string(event.AuditID)), nats.Context(ctx)); err != nil {
		metrics.EdgeAuditEventsDropped.WithLabelValues(cluster, "emit_failed").Inc()
		return fmt.Errorf("failed publishing audit event: %w", err)
	}

	metrics.EdgeAuditEmitLatencySeconds.Observe(time.Since(start).Seconds())
	metrics.EdgeAuditEventsEmitted.WithLabelValues(cluster).Inc()

	return nil
}

// Close releases the NATS connection.
func (e *NATSEmitter) Close() {
	if e.conn != nil {
		e.conn.Close()
	}
}
