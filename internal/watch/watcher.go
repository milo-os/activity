package watch

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"fmt"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/nats-io/nats.go"
	"k8s.io/apimachinery/pkg/watch"
	"k8s.io/klog/v2"

	"go.miloapis.com/activity/internal/cel"
	"go.miloapis.com/activity/internal/storage"
	"go.miloapis.com/activity/internal/types"
	"go.miloapis.com/activity/pkg/apis/activity/v1alpha1"
)

// NATSWatcher manages NATS JetStream connections for activity watch operations.
type NATSWatcher struct {
	conn          *nats.Conn
	js            nats.JetStreamContext
	streamName    string
	subjectPrefix string
}

// NATSConfig contains NATS connection configuration.
type NATSConfig struct {
	URL           string
	StreamName    string // JetStream stream name (e.g., "ACTIVITIES")
	SubjectPrefix string // Subject prefix (e.g., "activities")
	TLSEnabled    bool
	TLSCertFile   string
	TLSKeyFile    string
	TLSCAFile     string
}

// buildNATSTLSConfig creates a TLS configuration for NATS connections.
func buildNATSTLSConfig(config NATSConfig) (*tls.Config, error) {
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
		klog.V(2).InfoS("Loaded NATS client certificate", "certFile", config.TLSCertFile)
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
		klog.V(2).InfoS("Loaded NATS CA certificate", "caFile", config.TLSCAFile)
	}

	return tlsConfig, nil
}

// NewNATSWatcher creates a new NATS watcher with JetStream support.
// Returns nil if URL is empty (watch disabled).
func NewNATSWatcher(config NATSConfig) (*NATSWatcher, error) {
	if config.URL == "" {
		klog.Info("NATS URL not configured, Watch API will be disabled")
		return nil, nil
	}

	opts := []nats.Option{
		nats.RetryOnFailedConnect(true),
		nats.MaxReconnects(-1),
		nats.DisconnectErrHandler(func(nc *nats.Conn, err error) {
			if err != nil {
				klog.ErrorS(err, "NATS disconnected")
			}
		}),
		nats.ReconnectHandler(func(nc *nats.Conn) {
			klog.Info("NATS reconnected", "url", nc.ConnectedUrl())
		}),
	}

	// Add TLS configuration if enabled
	if config.TLSEnabled {
		tlsConfig, err := buildNATSTLSConfig(config)
		if err != nil {
			return nil, fmt.Errorf("failed to build NATS TLS config: %w", err)
		}
		opts = append(opts, nats.Secure(tlsConfig))
		klog.V(2).InfoS("NATS TLS enabled for Watch API")
	}

	conn, err := nats.Connect(config.URL, opts...)
	if err != nil {
		return nil, fmt.Errorf("failed to connect to NATS: %w", err)
	}

	// Create JetStream context
	js, err := conn.JetStream()
	if err != nil {
		conn.Close()
		return nil, fmt.Errorf("failed to create JetStream context: %w", err)
	}

	prefix := config.SubjectPrefix
	if prefix == "" {
		prefix = "activities"
	}

	streamName := config.StreamName
	if streamName == "" {
		streamName = "ACTIVITIES"
	}

	klog.Info("Connected to NATS JetStream for Watch API",
		"url", config.URL,
		"stream", streamName,
		"subjectPrefix", prefix,
	)

	return &NATSWatcher{
		conn:          conn,
		js:            js,
		streamName:    streamName,
		subjectPrefix: prefix,
	}, nil
}

// Close closes the NATS connection.
func (w *NATSWatcher) Close() {
	if w.conn != nil {
		w.conn.Close()
	}
}

// WatchFilter contains filter criteria for watch operations.
type WatchFilter struct {
	// ResourceVersion is the JetStream sequence number to start from.
	// If 0, starts from the latest message (real-time only).
	// If > 0, replays from that sequence number.
	ResourceVersion uint64
	// CELFilter selects which activities the watch delivers. Every filter
	// beyond tenant scope goes here; the NATS subject only narrows by tenant.
	CELFilter string
}

// Watch creates a new watch for activities matching the given scope and filter.
// Uses JetStream ephemeral push consumers for reliable delivery and replay support.
func (w *NATSWatcher) Watch(ctx context.Context, scope storage.ScopeContext, filter WatchFilter) (watch.Interface, error) {
	if w == nil || w.js == nil {
		return nil, fmt.Errorf("Watch API is not available: NATS not configured")
	}

	// Compile CEL filter if provided
	var celFilter *cel.CompiledActivityFilter
	if filter.CELFilter != "" {
		var err error
		celFilter, err = cel.CompileActivityFilterProgram(filter.CELFilter)
		if err != nil {
			return nil, fmt.Errorf("invalid CEL filter: %w", err)
		}
	}

	// Build the NATS subject filter based on scope
	subject := w.buildSubject(scope)

	// Generate unique consumer name for this watch
	consumerName := fmt.Sprintf("watch-%s", uuid.New().String()[:8])

	// Configure consumer based on resourceVersion
	var deliverPolicy nats.DeliverPolicy
	var optStartSeq uint64

	if filter.ResourceVersion > 0 {
		// Resume from specific sequence (replay missed events)
		deliverPolicy = nats.DeliverByStartSequencePolicy
		optStartSeq = filter.ResourceVersion
		klog.V(4).InfoS("Starting watch from sequence", "subject", subject, "sequence", optStartSeq)
	} else {
		// Start from now (real-time only)
		deliverPolicy = nats.DeliverNewPolicy
		klog.V(4).InfoS("Starting watch from now", "subject", subject)
	}

	// Create ephemeral push consumer with inbox delivery
	inbox := nats.NewInbox()

	consumerConfig := &nats.ConsumerConfig{
		Name:          consumerName,
		FilterSubject: subject,
		DeliverPolicy: deliverPolicy,
		AckPolicy:     nats.AckExplicitPolicy,
		DeliverSubject: inbox,
		// Ephemeral consumer - no durable name
		// Will be automatically cleaned up when subscription ends
		InactiveThreshold: 5 * time.Minute,
		// Flow control for backpressure
		FlowControl: true,
		// Heartbeat to detect stale consumers
		Heartbeat: 30 * time.Second,
	}

	if optStartSeq > 0 {
		consumerConfig.OptStartSeq = optStartSeq
	}

	// Add consumer to stream
	_, err := w.js.AddConsumer(w.streamName, consumerConfig)
	if err != nil {
		return nil, fmt.Errorf("failed to create JetStream consumer: %w", err)
	}

	// Subscribe to the consumer's delivery subject
	sub, err := w.conn.SubscribeSync(inbox)
	if err != nil {
		// Clean up consumer on failure
		w.js.DeleteConsumer(w.streamName, consumerName)
		return nil, fmt.Errorf("failed to subscribe to consumer inbox: %w", err)
	}

	watchCtx, cancel := context.WithCancel(ctx)

	aw := &ActivityWatch{
		resultChan:   make(chan watch.Event, 100),
		sub:          sub,
		js:           w.js,
		streamName:   w.streamName,
		consumerName: consumerName,
		ctx:          watchCtx,
		cancel:       cancel,
		scope:        scope,
		filter:       filter,
		celFilter:    celFilter,
		stopped:      false,
	}

	// Start processing messages
	go aw.processMessages()

	return aw, nil
}

// buildSubject returns the NATS subject pattern for a scope's activities.
// Published subjects are activities.<tenant_type>.<tenant_name>.<api_group>.<origin>.<kind>.<namespace>.<name>,
// but the API group is published raw and its dots add tokens, so nothing after
// the tenant has a fixed position. The pattern narrows by tenant only and the
// CEL filter does the rest.
func (w *NATSWatcher) buildSubject(scope storage.ScopeContext) string {
	if scope.Type == types.TenantTypePlatform {
		return w.subjectPrefix + ".>"
	}
	return strings.Join([]string{w.subjectPrefix, scope.Type, scope.Name, ">"}, ".")
}

// ActivityWatch implements watch.Interface for activity resources using JetStream.
type ActivityWatch struct {
	resultChan   chan watch.Event
	sub          *nats.Subscription
	js           nats.JetStreamContext
	streamName   string
	consumerName string
	ctx          context.Context
	cancel       context.CancelFunc
	scope        storage.ScopeContext
	filter       WatchFilter
	celFilter    *cel.CompiledActivityFilter // Pre-compiled CEL filter for advanced filtering

	mu      sync.Mutex
	stopped bool

	// lastSequence tracks the last processed sequence for resourceVersion
	lastSequence uint64
}

// ResultChan returns the channel for receiving watch events.
func (w *ActivityWatch) ResultChan() <-chan watch.Event {
	return w.resultChan
}

// Stop stops the watch and cleans up resources.
func (w *ActivityWatch) Stop() {
	w.mu.Lock()
	defer w.mu.Unlock()

	if w.stopped {
		return
	}
	w.stopped = true

	// Unsubscribe from inbox
	if w.sub != nil {
		w.sub.Unsubscribe()
	}

	// Delete ephemeral consumer
	if w.js != nil && w.consumerName != "" {
		if err := w.js.DeleteConsumer(w.streamName, w.consumerName); err != nil {
			klog.V(4).InfoS("Failed to delete consumer (may already be deleted)", "consumer", w.consumerName, "error", err)
		}
	}

	w.cancel()
	close(w.resultChan)
}

// processMessages reads messages from JetStream and sends watch events.
func (w *ActivityWatch) processMessages() {
	defer w.Stop()

	for {
		select {
		case <-w.ctx.Done():
			return
		default:
			msg, err := w.sub.NextMsgWithContext(w.ctx)
			if err != nil {
				if w.ctx.Err() != nil {
					// Context cancelled, normal shutdown
					return
				}
				klog.ErrorS(err, "Error receiving JetStream message")
				continue
			}

			// Handle flow control heartbeats
			if len(msg.Data) == 0 && msg.Header.Get("Status") == "100" {
				// Flow control message, respond to it
				msg.Respond(nil)
				continue
			}

			// Parse the activity from the message
			var activity v1alpha1.Activity
			if err := json.Unmarshal(msg.Data, &activity); err != nil {
				klog.ErrorS(err, "Failed to unmarshal activity from JetStream message")
				msg.Nak()
				continue
			}

			// Extract sequence number for resourceVersion
			meta, err := msg.Metadata()
			if err == nil {
				w.lastSequence = meta.Sequence.Stream
				// Set resourceVersion on the activity for client tracking
				activity.ResourceVersion = strconv.FormatUint(meta.Sequence.Stream, 10)
			}

			if !w.matchesFilter(&activity) {
				msg.Ack()
				continue
			}

			// Send the watch event
			select {
			case w.resultChan <- watch.Event{
				Type:   watch.Added,
				Object: &activity,
			}:
				// Acknowledge successful delivery
				msg.Ack()
			case <-w.ctx.Done():
				return
			}
		}
	}
}

// inScope reports whether activity belongs to scope, using the same rules as
// ClickHouse activity queries. Tenant isolation must not depend on the NATS
// subject alone: a tenant name containing a dot would let a subject pattern
// match another tenant's activities.
func inScope(scope storage.ScopeContext, activity *v1alpha1.Activity) bool {
	switch scope.Type {
	case types.TenantTypePlatform:
		return true
	case types.TenantTypeUser:
		return activity.Spec.Actor.UID == scope.Name
	default:
		return activity.Spec.Tenant.Type == scope.Type && activity.Spec.Tenant.Name == scope.Name
	}
}

// matchesFilter checks if an activity is in the watch's scope and matches its filter.
func (w *ActivityWatch) matchesFilter(activity *v1alpha1.Activity) bool {
	if !inScope(w.scope, activity) {
		return false
	}
	if w.celFilter != nil {
		match, err := w.celFilter.EvaluateActivity(activity)
		if err != nil {
			klog.ErrorS(err, "CEL filter evaluation failed",
				"activity", activity.Name)
			return false // Fail closed on error
		}
		if !match {
			return false
		}
	}

	return true
}

// LastResourceVersion returns the last processed resourceVersion (sequence number).
func (w *ActivityWatch) LastResourceVersion() string {
	return strconv.FormatUint(w.lastSequence, 10)
}

// WatcherInterface defines the interface for watch operations.
type WatcherInterface interface {
	Watch(ctx context.Context, scope storage.ScopeContext, filter WatchFilter) (watch.Interface, error)
}
