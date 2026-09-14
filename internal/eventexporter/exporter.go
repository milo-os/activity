// Package eventexporter implements a Kubernetes Event exporter that watches for Events
// and publishes them to NATS JetStream for ingestion into ClickHouse.
//
// This exporter uses events.k8s.io/v1 Event format for consistency with the
// EventRecord API and ClickHouse schema.
package eventexporter

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"time"

	"github.com/nats-io/nats.go"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	eventsv1 "k8s.io/api/events/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/informers"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/cache"
	"k8s.io/client-go/tools/clientcmd"
	"k8s.io/klog/v2"
	"sigs.k8s.io/controller-runtime/pkg/healthz"
	"sigs.k8s.io/controller-runtime/pkg/metrics"
)

var (
	eventsPublished = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Namespace: "event_exporter",
			Name:      "events_published_total",
			Help:      "Total number of Kubernetes events published to NATS",
		},
		[]string{"namespace", "reason"},
	)

	publishErrors = prometheus.NewCounter(
		prometheus.CounterOpts{
			Namespace: "event_exporter",
			Name:      "publish_errors_total",
			Help:      "Total number of errors publishing events to NATS",
		},
	)

	informerSynced = prometheus.NewGauge(
		prometheus.GaugeOpts{
			Namespace: "event_exporter",
			Name:      "informer_synced",
			Help:      "Informer cache sync status (1 = synced, 0 = not synced)",
		},
	)

	natsConnectionStatus = prometheus.NewGauge(
		prometheus.GaugeOpts{
			Namespace: "event_exporter",
			Name:      "nats_connection_status",
			Help:      "NATS connection status (1 = connected, 0 = disconnected)",
		},
	)

	publishLatency = prometheus.NewHistogram(
		prometheus.HistogramOpts{
			Namespace: "event_exporter",
			Name:      "publish_latency_seconds",
			Help:      "Latency of NATS publish operations",
			Buckets:   prometheus.DefBuckets,
		},
	)
)

func init() {
	// Use controller-runtime's registry so metrics are exposed alongside other metrics.
	metrics.Registry.MustRegister(
		eventsPublished,
		publishErrors,
		informerSynced,
		natsConnectionStatus,
		publishLatency,
	)
}

// Config holds the exporter configuration.
type Config struct {
	// NATS connection URL
	NATSUrl string

	// NATS subject prefix for events (subject will be {prefix}.{namespace})
	SubjectPrefix string

	// Scope annotations to inject for multi-tenant isolation
	ScopeType string
	ScopeName string

	// Kubeconfig path (empty for in-cluster)
	Kubeconfig string

	// Resync period for the informer
	ResyncPeriod time.Duration

	// Health probe server bind address
	HealthProbeAddr string

	NATSTLSEnabled  bool   // Enable TLS for NATS connection
	NATSTLSCertFile string // Path to client certificate file (for mTLS)
	NATSTLSKeyFile  string // Path to client private key file (for mTLS)
	NATSTLSCAFile   string // Path to CA certificate file for server verification
}

// Run starts the event exporter and blocks until the context is cancelled.
func Run(ctx context.Context, cfg Config) error {
	klog.InfoS("Starting k8s-event-exporter",
		"natsUrl", cfg.NATSUrl,
		"subjectPrefix", cfg.SubjectPrefix,
		"scopeType", cfg.ScopeType,
		"scopeName", cfg.ScopeName,
		"healthProbeAddr", cfg.HealthProbeAddr,
	)

	// Create Kubernetes client
	k8sClient, err := createK8sClient(cfg.Kubeconfig)
	if err != nil {
		return fmt.Errorf("failed to create Kubernetes client: %w", err)
	}

	// Connect to NATS with metrics tracking
	natsConnectionStatus.Set(0)
	natsOpts := []nats.Option{
		nats.Name("k8s-event-exporter"),
		nats.ReconnectWait(2 * time.Second),
		nats.MaxReconnects(-1), // Unlimited reconnects
		nats.DisconnectErrHandler(func(_ *nats.Conn, err error) {
			klog.ErrorS(err, "NATS disconnected")
			natsConnectionStatus.Set(0)
		}),
		nats.ReconnectHandler(func(_ *nats.Conn) {
			klog.InfoS("NATS reconnected")
			natsConnectionStatus.Set(1)
		}),
	}

	if cfg.NATSTLSEnabled {
		tlsConfig, err := buildNATSTLSConfig(cfg)
		if err != nil {
			return fmt.Errorf("failed to build NATS TLS config: %w", err)
		}
		natsOpts = append(natsOpts, nats.Secure(tlsConfig))
		klog.InfoS("NATS TLS enabled")
	}

	nc, err := nats.Connect(cfg.NATSUrl, natsOpts...)
	if err != nil {
		return fmt.Errorf("failed to connect to NATS: %w", err)
	}
	defer nc.Close()
	natsConnectionStatus.Set(1)

	// Get JetStream context
	js, err := nc.JetStream()
	if err != nil {
		return fmt.Errorf("failed to get JetStream context: %w", err)
	}

	klog.InfoS("Connected to NATS", "url", cfg.NATSUrl)

	// Create the event exporter
	exporter := &Exporter{
		nc:            nc,
		js:            js,
		subjectPrefix: cfg.SubjectPrefix,
		scopeType:     cfg.ScopeType,
		scopeName:     cfg.ScopeName,
	}

	// Create informer factory for all namespaces
	factory := informers.NewSharedInformerFactory(k8sClient, cfg.ResyncPeriod)
	eventInformer := factory.Events().V1().Events().Informer()

	// Register event handlers
	eventInformer.AddEventHandler(cache.ResourceEventHandlerFuncs{
		AddFunc: func(obj interface{}) {
			event, ok := obj.(*eventsv1.Event)
			if !ok {
				return
			}
			if err := exporter.publishEvent(ctx, event, "ADDED"); err != nil {
				klog.ErrorS(err, "Failed to publish event",
					"namespace", event.Namespace,
					"name", event.Name,
				)
			}
		},
		UpdateFunc: func(oldObj, newObj interface{}) {
			event, ok := newObj.(*eventsv1.Event)
			if !ok {
				return
			}
			if err := exporter.publishEvent(ctx, event, "MODIFIED"); err != nil {
				klog.ErrorS(err, "Failed to publish event",
					"namespace", event.Namespace,
					"name", event.Name,
				)
			}
		},
		// We don't need to handle deletes - events are ephemeral and TTL'd
	})

	// Start health check server early so Kubernetes can check liveness during initialization
	healthServer := startHealthServer(cfg.HealthProbeAddr, exporter, eventInformer)
	defer func() {
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := healthServer.Shutdown(shutdownCtx); err != nil {
			klog.ErrorS(err, "Failed to shutdown health server")
		}
	}()

	// Start the informer
	factory.Start(ctx.Done())

	// Wait for cache sync
	klog.InfoS("Waiting for informer cache to sync")
	informerSynced.Set(0)
	if !cache.WaitForCacheSync(ctx.Done(), eventInformer.HasSynced) {
		return fmt.Errorf("failed to sync informer cache")
	}
	informerSynced.Set(1)
	klog.InfoS("Informer cache synced, watching for events")

	// Wait for shutdown
	<-ctx.Done()
	klog.InfoS("Shutting down")
	return nil
}

// Exporter handles publishing Kubernetes events to NATS.
type Exporter struct {
	nc            *nats.Conn
	js            nats.JetStreamContext
	subjectPrefix string
	scopeType     string
	scopeName     string
}

// publishEvent publishes a Kubernetes event to NATS JetStream.
func (e *Exporter) publishEvent(ctx context.Context, event *eventsv1.Event, eventType string) error {
	start := time.Now()

	// Create a copy to avoid modifying the cached object
	eventCopy := event.DeepCopy()

	// Inject scope annotations
	if eventCopy.Annotations == nil {
		eventCopy.Annotations = make(map[string]string)
	}
	eventCopy.Annotations["platform.miloapis.com/scope.type"] = e.scopeType
	eventCopy.Annotations["platform.miloapis.com/scope.name"] = e.scopeName

	// TypeMeta should be correctly populated by eventsv1.Event marshaling
	// but we'll set it explicitly to ensure consistency
	eventCopy.TypeMeta = metav1.TypeMeta{
		APIVersion: "events.k8s.io/v1",
		Kind:       "Event",
	}

	// Serialize to JSON
	data, err := json.Marshal(eventCopy)
	if err != nil {
		publishErrors.Inc()
		return fmt.Errorf("failed to marshal event: %w", err)
	}

	// Build subject: events.k8s.{namespace}
	subject := fmt.Sprintf("%s.%s", e.subjectPrefix, event.Namespace)

	// Publish with message ID for deduplication
	msgID := string(event.UID)
	if eventType == "MODIFIED" {
		// For updates, include resource version to allow updates through
		msgID = fmt.Sprintf("%s-%s", event.UID, event.ResourceVersion)
	}

	_, err = e.js.Publish(subject, data,
		nats.MsgId(msgID),
		nats.Context(ctx),
	)
	if err != nil {
		publishErrors.Inc()
		return fmt.Errorf("failed to publish to NATS: %w", err)
	}

	// Record metrics
	publishLatency.Observe(time.Since(start).Seconds())
	eventsPublished.WithLabelValues(event.Namespace, event.Reason).Inc()

	klog.V(4).InfoS("Published event",
		"namespace", event.Namespace,
		"name", event.Name,
		"reason", event.Reason,
		"type", eventType,
		"subject", subject,
	)

	return nil
}

// buildNATSTLSConfig creates a TLS configuration for the NATS connection.
func buildNATSTLSConfig(cfg Config) (*tls.Config, error) {
	tlsConfig := &tls.Config{
		MinVersion: tls.VersionTLS12,
	}

	// Load client certificate and key if provided (for mTLS). Require both or
	// neither: silently skipping a half-specified pair would leave the
	// connection looking TLS-enabled while carrying no client identity.
	switch {
	case cfg.NATSTLSCertFile != "" && cfg.NATSTLSKeyFile != "":
		cert, err := tls.LoadX509KeyPair(cfg.NATSTLSCertFile, cfg.NATSTLSKeyFile)
		if err != nil {
			return nil, fmt.Errorf("failed to load NATS client certificate: %w", err)
		}
		tlsConfig.Certificates = []tls.Certificate{cert}
		klog.V(2).InfoS("Loaded NATS client certificate",
			"certFile", cfg.NATSTLSCertFile,
			"keyFile", cfg.NATSTLSKeyFile,
		)
	case cfg.NATSTLSCertFile != "":
		return nil, fmt.Errorf("NATS TLS cert file set without a key file")
	case cfg.NATSTLSKeyFile != "":
		return nil, fmt.Errorf("NATS TLS key file set without a cert file")
	}

	// Load CA certificate if provided for server verification
	if cfg.NATSTLSCAFile != "" {
		caCert, err := os.ReadFile(cfg.NATSTLSCAFile)
		if err != nil {
			return nil, fmt.Errorf("failed to read NATS CA certificate: %w", err)
		}
		caCertPool := x509.NewCertPool()
		if !caCertPool.AppendCertsFromPEM(caCert) {
			return nil, fmt.Errorf("failed to parse NATS CA certificate")
		}
		tlsConfig.RootCAs = caCertPool
		klog.V(2).InfoS("Loaded NATS CA certificate", "caFile", cfg.NATSTLSCAFile)
	}

	return tlsConfig, nil
}

// createK8sClient creates a Kubernetes client.
func createK8sClient(kubeconfig string) (*kubernetes.Clientset, error) {
	var config *rest.Config
	var err error

	if kubeconfig != "" {
		config, err = clientcmd.BuildConfigFromFlags("", kubeconfig)
	} else {
		config, err = rest.InClusterConfig()
	}
	if err != nil {
		return nil, fmt.Errorf("failed to build config: %w", err)
	}

	client, err := kubernetes.NewForConfig(config)
	if err != nil {
		return nil, fmt.Errorf("failed to create client: %w", err)
	}

	return client, nil
}

// startHealthServer starts the HTTP health probe server.
func startHealthServer(addr string, exporter *Exporter, informer cache.SharedIndexInformer) *http.Server {
	mux := http.NewServeMux()

	// Liveness probe - checks if the exporter is alive and NATS is connected
	mux.Handle("/healthz", http.StripPrefix("/healthz", &healthz.Handler{
		Checks: map[string]healthz.Checker{
			"ping": healthz.Ping,
			"nats": natsHealthChecker(exporter),
		},
	}))

	// Readiness probe - checks if the exporter is ready to process events
	mux.Handle("/readyz", http.StripPrefix("/readyz", &healthz.Handler{
		Checks: map[string]healthz.Checker{
			"ping":            healthz.Ping,
			"nats":            natsHealthChecker(exporter),
			"informer-synced": informerSyncedChecker(informer),
		},
	}))

	// Metrics endpoint for Prometheus scraping
	mux.Handle("/metrics", promhttp.HandlerFor(metrics.Registry, promhttp.HandlerOpts{}))

	server := &http.Server{
		Addr:    addr,
		Handler: mux,
	}

	go func() {
		klog.InfoS("Starting health probe server", "addr", addr)
		if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			klog.ErrorS(err, "Health probe server error")
		}
	}()

	return server
}

// natsHealthChecker returns a health checker for NATS connection status.
func natsHealthChecker(exporter *Exporter) healthz.Checker {
	return func(req *http.Request) error {
		if exporter.nc == nil {
			return fmt.Errorf("NATS connection not initialized")
		}
		if !exporter.nc.IsConnected() {
			return fmt.Errorf("NATS connection is disconnected")
		}
		return nil
	}
}

// informerSyncedChecker returns a health checker for informer cache sync status.
func informerSyncedChecker(informer cache.SharedIndexInformer) healthz.Checker {
	return func(req *http.Request) error {
		if !informer.HasSynced() {
			return fmt.Errorf("informer cache not synced")
		}
		return nil
	}
}
