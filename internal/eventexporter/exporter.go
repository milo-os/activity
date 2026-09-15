// Package eventexporter implements a Kubernetes Event exporter that watches for Events
// and publishes them to NATS JetStream for ingestion into ClickHouse.
//
// This exporter uses events.k8s.io/v1 Event format for consistency with the
// EventRecord API and ClickHouse schema.
package eventexporter

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
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
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/healthz"
	"sigs.k8s.io/controller-runtime/pkg/metrics"

	"go.miloapis.com/activity/internal/types"
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

	unscopedEvents = prometheus.NewCounter(
		prometheus.CounterOpts{
			Namespace: "event_exporter",
			Name:      "unscoped_events_total",
			Help:      "Total number of edge events published without a recovered tenant scope",
		},
	)

	noCityEvents = prometheus.NewCounter(
		prometheus.CounterOpts{
			Namespace: "event_exporter",
			Name:      "no_city_events_total",
			Help:      "Total number of edge events published before this cell's city resolved",
		},
	)

	droppedEvents = prometheus.NewCounter(
		prometheus.CounterOpts{
			Namespace: "event_exporter",
			Name:      "dropped_events_total",
			Help:      "Total number of events dropped because the publish queue was full. The newest event is dropped, not an already-queued one.",
		},
	)

	queueDepth = prometheus.NewGauge(
		prometheus.GaugeOpts{
			Namespace: "event_exporter",
			Name:      "queue_depth",
			Help:      "Current number of events buffered in the publish queue awaiting NATS publish.",
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
		unscopedEvents,
		noCityEvents,
		droppedEvents,
		queueDepth,
	)
}

// planeTypeEdge is the Config.PlaneType value for an edge deployment: one
// running in a workload cluster rather than the management plane.
const planeTypeEdge = "edge"

// publishQueueSize bounds the publish queue between the informer callback
// and the NATS publish call.
const publishQueueSize = 1000

// eventJob is a queued event awaiting publish.
type eventJob struct {
	event     *eventsv1.Event
	eventType string
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

	// PlaneType is this deployment's plane: "management" or "edge". Empty
	// means this exporter does not tag events with source metadata.
	PlaneType string

	// ClusterName identifies the Kubernetes cluster this exporter runs in,
	// e.g. "us-central-1-alice".
	ClusterName string

	// ClusterRegion is this cluster's region, e.g. "us-central-1".
	ClusterRegion string

	// LocationName is a fallback Location name for city resolution, used
	// only until a ServingLocation is delivered to this cell.
	LocationName string
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

	restConfig, err := buildRestConfig(cfg.Kubeconfig)
	if err != nil {
		return fmt.Errorf("failed to build Kubernetes client config: %w", err)
	}

	// Create Kubernetes client
	k8sClient, err := createK8sClient(restConfig)
	if err != nil {
		return fmt.Errorf("failed to create Kubernetes client: %w", err)
	}

	var city *cityResolver
	if cfg.PlaneType == planeTypeEdge {
		locationsClient, err := client.New(restConfig, client.Options{Scheme: locationsScheme})
		if err != nil {
			return fmt.Errorf("failed to create locations client: %w", err)
		}
		city = newCityResolver(locationsClient, cfg.LocationName)
		go city.Run(ctx)
	}

	// Connect to NATS with metrics tracking
	natsConnectionStatus.Set(0)
	nc, err := nats.Connect(cfg.NATSUrl,
		nats.Name("k8s-event-exporter"),
		nats.ReconnectWait(2*time.Second),
		nats.MaxReconnects(-1), // Unlimited reconnects
		nats.DisconnectErrHandler(func(_ *nats.Conn, err error) {
			klog.ErrorS(err, "NATS disconnected")
			natsConnectionStatus.Set(0)
		}),
		nats.ReconnectHandler(func(_ *nats.Conn) {
			klog.InfoS("NATS reconnected")
			natsConnectionStatus.Set(1)
		}),
	)
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

	// Create informer factory for all namespaces
	factory := informers.NewSharedInformerFactory(k8sClient, cfg.ResyncPeriod)
	eventInformer := factory.Events().V1().Events().Informer()

	var scopeResolver *namespaceScopeResolver
	cacheSyncs := []cache.InformerSynced{eventInformer.HasSynced}
	if cfg.PlaneType == planeTypeEdge {
		namespaceInformer := factory.Core().V1().Namespaces()
		scopeResolver = newNamespaceScopeResolver(namespaceInformer.Lister())
		cacheSyncs = append(cacheSyncs, namespaceInformer.Informer().HasSynced)
	}

	// Create the event exporter
	exporter := &Exporter{
		nc:            nc,
		js:            js,
		subjectPrefix: cfg.SubjectPrefix,
		scopeType:     cfg.ScopeType,
		scopeName:     cfg.ScopeName,
		planeType:     cfg.PlaneType,
		clusterName:   cfg.ClusterName,
		clusterRegion: cfg.ClusterRegion,
		scope:         scopeResolver,
		city:          city,
		queue:         make(chan eventJob, publishQueueSize),
	}
	go exporter.drainQueue(ctx)

	// Register event handlers
	eventInformer.AddEventHandler(cache.ResourceEventHandlerFuncs{
		AddFunc: func(obj interface{}) {
			event, ok := obj.(*eventsv1.Event)
			if !ok {
				return
			}
			exporter.enqueue(event, "ADDED")
		},
		UpdateFunc: func(oldObj, newObj interface{}) {
			event, ok := newObj.(*eventsv1.Event)
			if !ok {
				return
			}
			exporter.enqueue(event, "MODIFIED")
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
	if !cache.WaitForCacheSync(ctx.Done(), cacheSyncs...) {
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
	planeType     string
	clusterName   string
	clusterRegion string
	scope         *namespaceScopeResolver
	city          *cityResolver
	queue         chan eventJob
}

// resolveScope returns the tenant scope to stamp on an event from the given
// namespace. A management deployment always uses its static, per-deployment
// scope flags. An edge deployment serves many projects, so its static flags
// don't name a real tenant; it recovers the tenant per event instead, and
// emits unscoped - rather than misattributing to whatever the flags happen
// to hold - when recovery fails.
func (e *Exporter) resolveScope(namespace string) (scopeType, scopeName string) {
	if e.planeType != planeTypeEdge || e.scope == nil {
		return e.scopeType, e.scopeName
	}

	if scopeType, scopeName, ok := e.scope.Resolve(namespace); ok {
		return scopeType, scopeName
	}

	unscopedEvents.Inc()
	return "", ""
}

// sourceAnnotations returns this cell's plane type, cluster, region, and
// city for stamping onto a published event. City is empty until this cell's
// cityResolver first resolves; an edge deployment still publishes in that
// window rather than waiting, so noCityEvents counts how often that happens.
func (e *Exporter) sourceAnnotations() (planeType, cluster, region, city string) {
	if e.city != nil {
		city = e.city.City()
		if e.planeType == planeTypeEdge && city == "" {
			noCityEvents.Inc()
		}
	}
	return e.planeType, e.clusterName, e.clusterRegion, city
}

// qualifiedMsgID returns the NATS message ID for event, qualified with this
// cell's plane and cluster so the same UID from a different cluster can't
// collide. It must stay in sync with the Activity origin ID the processor
// derives from this event's source annotations.
func (e *Exporter) qualifiedMsgID(event *eventsv1.Event, eventType string) string {
	msgID := string(event.UID)
	if eventType == "MODIFIED" {
		// For updates, include resource version to allow updates through
		msgID = fmt.Sprintf("%s-%s", event.UID, event.ResourceVersion)
	}
	return types.PrefixWithSource(e.planeType, e.clusterName, msgID)
}

// enqueue copies event and queues it for publish. If the queue is full, the
// incoming event is dropped rather than blocking the informer callback.
func (e *Exporter) enqueue(event *eventsv1.Event, eventType string) {
	job := eventJob{event: event.DeepCopy(), eventType: eventType}

	select {
	case e.queue <- job:
	default:
		droppedEvents.Inc()
		klog.ErrorS(fmt.Errorf("publish queue full (size %d)", cap(e.queue)), "Dropped event",
			"namespace", event.Namespace,
			"name", event.Name,
		)
	}
	queueDepth.Set(float64(len(e.queue)))
}

// drainQueue publishes queued events until ctx is cancelled. A job already
// pulled off the queue when ctx is cancelled is dropped rather than
// published, since publishEvent would otherwise run with an already-dead
// ctx and fail.
func (e *Exporter) drainQueue(ctx context.Context) {
	for {
		select {
		case <-ctx.Done():
			return
		case job := <-e.queue:
			if ctx.Err() != nil {
				return
			}
			if err := e.publishEvent(ctx, job.event, job.eventType); err != nil {
				klog.ErrorS(err, "Failed to publish event",
					"namespace", job.event.Namespace,
					"name", job.event.Name,
				)
			}
			queueDepth.Set(float64(len(e.queue)))
		}
	}
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

	scopeType, scopeName := e.resolveScope(event.Namespace)
	eventCopy.Annotations[types.ScopeTypeAnnotation] = scopeType
	eventCopy.Annotations[types.ScopeNameAnnotation] = scopeName

	planeType, cluster, region, city := e.sourceAnnotations()
	eventCopy.Annotations[types.SourcePlaneTypeAnnotation] = planeType
	eventCopy.Annotations[types.SourceClusterAnnotation] = cluster
	eventCopy.Annotations[types.SourceRegionAnnotation] = region
	eventCopy.Annotations[types.SourceCityAnnotation] = city

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

	_, err = e.js.Publish(subject, data,
		nats.MsgId(e.qualifiedMsgID(event, eventType)),
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

// buildRestConfig loads the Kubernetes client configuration, from a
// kubeconfig file if given, or in-cluster config otherwise.
func buildRestConfig(kubeconfig string) (*rest.Config, error) {
	if kubeconfig != "" {
		return clientcmd.BuildConfigFromFlags("", kubeconfig)
	}
	return rest.InClusterConfig()
}

// createK8sClient creates a Kubernetes client.
func createK8sClient(config *rest.Config) (*kubernetes.Clientset, error) {
	clientset, err := kubernetes.NewForConfig(config)
	if err != nil {
		return nil, fmt.Errorf("failed to create client: %w", err)
	}

	return clientset, nil
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
