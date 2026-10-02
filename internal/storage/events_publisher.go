package storage

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"fmt"
	"os"
	"time"

	"github.com/nats-io/nats.go"
	eventsv1 "k8s.io/api/events/v1"
	"k8s.io/klog/v2"

	activitymetrics "go.miloapis.com/activity/internal/metrics"
	"go.miloapis.com/activity/internal/types"
)

// EventsPublisher publishes Kubernetes Events to NATS JetStream.
// This allows the activity-apiserver to publish events that Vector will
// consume and write to ClickHouse, rather than writing directly.
type EventsPublisher struct {
	js            nats.JetStreamContext
	subjectPrefix string
}

// EventsPublisherConfig configures the events publisher.
type EventsPublisherConfig struct {
	URL           string
	StreamName    string
	SubjectPrefix string
	TLSEnabled    bool
	TLSCertFile   string
	TLSKeyFile    string
	TLSCAFile     string
}

// buildEventsTLSConfig creates a TLS configuration for NATS connections.
func buildEventsTLSConfig(config EventsPublisherConfig) (*tls.Config, error) {
	tlsConfig := &tls.Config{
		MinVersion: tls.VersionTLS12,
	}

	// Load client certificate if provided
	if config.TLSCertFile != "" && config.TLSKeyFile != "" {
		cert, err := tls.LoadX509KeyPair(config.TLSCertFile, config.TLSKeyFile)
		if err != nil {
			return nil, fmt.Errorf("failed to load NATS client certificate: %w", err)
		}
		tlsConfig.Certificates = []tls.Certificate{cert}
		klog.V(2).InfoS("Loaded NATS client certificate for events publisher", "certFile", config.TLSCertFile)
	}

	// Load CA certificate if provided
	if config.TLSCAFile != "" {
		caCert, err := os.ReadFile(config.TLSCAFile)
		if err != nil {
			return nil, fmt.Errorf("failed to read NATS CA certificate: %w", err)
		}
		caCertPool := x509.NewCertPool()
		if !caCertPool.AppendCertsFromPEM(caCert) {
			return nil, fmt.Errorf("failed to parse NATS CA certificate")
		}
		tlsConfig.RootCAs = caCertPool
		klog.V(2).InfoS("Loaded NATS CA certificate for events publisher", "caFile", config.TLSCAFile)
	}

	return tlsConfig, nil
}

// NewEventsPublisher creates a new NATS JetStream publisher for events.
// Returns nil if URL is empty (publishing disabled).
func NewEventsPublisher(config EventsPublisherConfig) (*EventsPublisher, error) {
	if config.URL == "" {
		klog.Info("NATS URL not configured for events publishing, events will only be written to ClickHouse")
		return nil, nil
	}

	opts := []nats.Option{
		nats.Name("activity-events-publisher"),
		nats.RetryOnFailedConnect(true),
		nats.MaxReconnects(-1),
		nats.DisconnectErrHandler(func(nc *nats.Conn, err error) {
			if err != nil {
				klog.ErrorS(err, "Events NATS publisher disconnected")
			}
			activitymetrics.EventsNATSConnectionStatus.Set(0)
		}),
		nats.ReconnectHandler(func(nc *nats.Conn) {
			klog.Info("Events NATS publisher reconnected", "url", nc.ConnectedUrl())
			activitymetrics.EventsNATSConnectionStatus.Set(1)
		}),
	}

	// Add TLS configuration if enabled
	if config.TLSEnabled {
		tlsConfig, err := buildEventsTLSConfig(config)
		if err != nil {
			return nil, fmt.Errorf("failed to build NATS TLS config for events publisher: %w", err)
		}
		opts = append(opts, nats.Secure(tlsConfig))
		klog.V(2).InfoS("NATS TLS enabled for events publisher")
	}

	conn, err := nats.Connect(config.URL, opts...)
	if err != nil {
		return nil, fmt.Errorf("failed to connect to NATS for events publishing: %w", err)
	}

	// Create JetStream context
	js, err := conn.JetStream()
	if err != nil {
		conn.Close()
		return nil, fmt.Errorf("failed to create JetStream context for events publisher: %w", err)
	}

	prefix := config.SubjectPrefix
	if prefix == "" {
		prefix = "events.k8s"
	}

	streamName := config.StreamName
	if streamName == "" {
		streamName = "EVENTS"
	}

	klog.Info("Connected to NATS JetStream for events publishing",
		"url", config.URL,
		"stream", streamName,
		"subjectPrefix", prefix,
	)

	// Set connection status to 1 (connected)
	activitymetrics.EventsNATSConnectionStatus.Set(1)

	return &EventsPublisher{
		js:            js,
		subjectPrefix: prefix,
	}, nil
}

// Publish publishes an event to NATS JetStream.
// Subject format: <subjectPrefix>.{namespace}
// Message ID is derived from event UID and ResourceVersion for deduplication.
func (p *EventsPublisher) Publish(ctx context.Context, event *eventsv1.Event) error {
	if p == nil || p.js == nil {
		// Publisher not configured - this is okay, events will be written directly to ClickHouse
		return nil
	}

	start := time.Now()

	// Serialize event to JSON
	data, err := json.Marshal(event)
	if err != nil {
		activitymetrics.EventsPublishErrorsTotal.Inc()
		return fmt.Errorf("failed to marshal event: %w", err)
	}

	subject := types.EventSubject(p.subjectPrefix, "", event.Namespace)

	// Generate message ID for deduplication
	// For creates: use UID only
	// For updates: use UID + ResourceVersion to allow updates through
	msgID := string(event.UID)
	if event.ResourceVersion != "" && event.ResourceVersion != "0" {
		msgID = fmt.Sprintf("%s-%s", event.UID, event.ResourceVersion)
	}

	// Publish to NATS with deduplication
	_, err = p.js.Publish(subject, data,
		nats.MsgId(msgID),
		nats.Context(ctx),
	)
	if err != nil {
		activitymetrics.EventsPublishErrorsTotal.Inc()
		return fmt.Errorf("failed to publish event to NATS: %w", err)
	}

	// Record metrics
	activitymetrics.EventsPublishLatencySeconds.Observe(time.Since(start).Seconds())
	activitymetrics.EventsPublishedTotal.WithLabelValues(event.Namespace, event.Reason).Inc()

	klog.V(4).InfoS("Published event to NATS",
		"namespace", event.Namespace,
		"name", event.Name,
		"reason", event.Reason,
		"subject", subject,
		"msgID", msgID,
	)

	return nil
}
