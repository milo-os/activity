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
	"sync"
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

	"go.miloapis.com/activity/internal/natsconn"
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
			Help:      "Total number of events dropped because the publish queue was full. The oldest queued event is evicted to make room for the newest.",
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

const (
	// publishQueueSize bounds the publish queue between the informer callback
	// and the NATS publish call.
	publishQueueSize = 1000

	// publishAsyncMaxPending caps the unacked publishes in flight.
	publishAsyncMaxPending = 256

	// publishStallWait bounds how long a full in-flight window blocks drainQueue
	// before the publish is failed, so saturation usually sheds at the queue.
	publishStallWait = 5 * time.Second

	// publishAckTimeout makes every publish resolve one way or the other;
	// without it a stranded ack would hang the shutdown drain forever.
	publishAckTimeout = 30 * time.Second

	ackDrainTimeout = 5 * time.Second
)

// natsClientName identifies this exporter to the NATS server.
const natsClientName = "k8s-event-exporter"

// natsOptions returns the connection options natsconn.Endpoint leaves to the
// caller: reconnect behaviour, the handlers driving natsConnectionStatus, and
// the federated inbox prefix.
func natsOptions(cfg Config) []nats.Option {
	opts := []nats.Option{
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
	// A federated source's PubAcks must land under the per-cluster inbox its
	// hub leaf grant allows; see types.EventInboxPrefix.
	if cluster := FederatedCluster(cfg.PlaneType, cfg.ClusterName); cluster != "" {
		opts = append(opts, nats.CustomInboxPrefix(types.EventInboxPrefix(cluster)))
	}
	return opts
}

// eventJob is a queued event awaiting publish.
type eventJob struct {
	event     *eventsv1.Event
	eventType string
}

// Config holds the exporter configuration.
type Config struct {
	// NATS is the broker this exporter publishes to, with its TLS material
	NATS natsconn.Endpoint

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
		"natsUrl", cfg.NATS.URL,
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
	nc, err := cfg.NATS.Connect(natsClientName, natsOptions(cfg)...)
	if err != nil {
		return fmt.Errorf("failed to connect to NATS: %w", err)
	}
	defer nc.Close()
	natsConnectionStatus.Set(1)

	// Get JetStream context
	js, err := nc.JetStream(
		nats.PublishAsyncMaxPending(publishAsyncMaxPending),
		nats.PublishAsyncTimeout(publishAckTimeout),
	)
	if err != nil {
		return fmt.Errorf("failed to get JetStream context: %w", err)
	}

	klog.InfoS("Connected to NATS", "url", cfg.NATS.URL)

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
	drained := make(chan struct{})
	go func() {
		defer close(drained)
		exporter.drainQueue(ctx)
	}()

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
	// The deferred nc.Close() must not run until the in-flight acks have drained.
	<-drained
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

	// inFlight tracks the ack waiters so shutdown can drain them.
	inFlight sync.WaitGroup
}

// resolveScope returns the tenant scope to stamp on an event from the given
// namespace. A management deployment always uses its static, per-deployment
// scope flags. An edge deployment serves many projects, so its static flags
// don't name a real tenant; it recovers the tenant per event instead, and
// emits unscoped - rather than misattributing to whatever the flags happen
// to hold - when recovery fails.
func (e *Exporter) resolveScope(namespace string) eventScope {
	if e.planeType != planeTypeEdge || e.scope == nil {
		return eventScope{Type: e.scopeType, Name: e.scopeName}
	}

	if scope, ok := e.scope.Resolve(namespace); ok {
		return scope
	}

	unscopedEvents.Inc()
	return eventScope{}
}

// stampScope writes the tenant scope annotations for an event published from
// namespace. The upstream namespace is stamped only when recovered, leaving
// management-plane events unchanged.
func (e *Exporter) stampScope(annotations map[string]string, namespace string) {
	scope := e.resolveScope(namespace)

	annotations[types.ScopeTypeAnnotation] = scope.Type
	annotations[types.ScopeNameAnnotation] = scope.Name
	if scope.Namespace != "" {
		annotations[types.ScopeNamespaceAnnotation] = scope.Namespace
	}
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

// subject builds the NATS publish subject. A federated source's subject carries
// a cluster token so per-PoP NATS permissions can scope publish access to it.
func (e *Exporter) subject(namespace string) string {
	return types.EventSubject(e.subjectPrefix, FederatedCluster(e.planeType, e.clusterName), namespace)
}

// FederatedCluster is the single gate for both the publish subject's cluster
// token and the reply inbox prefix, so the two cannot be scoped differently.
// Exported so option validation gates on the same notion of "federated".
func FederatedCluster(planeType, clusterName string) string {
	if planeType == "" || clusterName == "" {
		return ""
	}
	return clusterName
}

// enqueue copies event and queues it for publish, never blocking the informer
// callback. A full queue sheds its oldest event rather than the incoming one,
// so a saturated link keeps reporting current state.
func (e *Exporter) enqueue(event *eventsv1.Event, eventType string) {
	job := eventJob{event: event.DeepCopy(), eventType: eventType}
	if e.tryEnqueue(job) {
		return
	}

	// The single producer always wins the slot an eviction frees, so the job is
	// only shed when there was nothing to evict.
	e.evictOldest()
	if !e.tryEnqueue(job) {
		recordDrop(cap(e.queue), job.event)
	}
}

// tryEnqueue queues job without blocking, reporting whether it fit.
func (e *Exporter) tryEnqueue(job eventJob) bool {
	select {
	case e.queue <- job:
		queueDepth.Set(float64(len(e.queue)))
		return true
	default:
		return false
	}
}

// evictOldest drops the oldest queued job, unless drainQueue took it first.
func (e *Exporter) evictOldest() {
	select {
	case evicted := <-e.queue:
		recordDrop(cap(e.queue), evicted.event)
		queueDepth.Set(float64(len(e.queue)))
	default:
	}
}

func recordDrop(queueSize int, event *eventsv1.Event) {
	droppedEvents.Inc()
	klog.ErrorS(fmt.Errorf("publish queue full (size %d)", queueSize), "Dropped event",
		"namespace", event.Namespace,
		"name", event.Name,
	)
}

// drainQueue publishes queued events until ctx is cancelled, then waits out
// the acks still in flight. A job already pulled off the queue when ctx is
// cancelled is dropped rather than published.
func (e *Exporter) drainQueue(ctx context.Context) {
	defer e.awaitPendingAcks()

	for {
		select {
		case <-ctx.Done():
			return
		case job := <-e.queue:
			if ctx.Err() != nil {
				return
			}
			if err := e.publishEvent(job.event, job.eventType); err != nil {
				klog.ErrorS(err, "Failed to publish event",
					"namespace", job.event.Namespace,
					"name", job.event.Name,
				)
			}
			queueDepth.Set(float64(len(e.queue)))
		}
	}
}

// publishEvent publishes a Kubernetes event to NATS JetStream. It returns once
// the publish is in flight; awaitAck records the outcome when the PubAck lands.
func (e *Exporter) publishEvent(event *eventsv1.Event, eventType string) error {
	start := time.Now()

	// Create a copy to avoid modifying the cached object
	eventCopy := event.DeepCopy()

	// Inject scope annotations
	if eventCopy.Annotations == nil {
		eventCopy.Annotations = make(map[string]string)
	}

	e.stampScope(eventCopy.Annotations, event.Namespace)

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

	subject := e.subject(event.Namespace)

	future, err := e.js.PublishAsync(subject, data,
		nats.MsgId(e.qualifiedMsgID(event, eventType)),
		nats.StallWait(publishStallWait),
	)
	if err != nil {
		publishErrors.Inc()
		return fmt.Errorf("failed to publish to NATS: %w", err)
	}

	e.inFlight.Add(1)
	go func() {
		defer e.inFlight.Done()
		e.awaitAck(future, pendingAck{
			start:     start,
			namespace: event.Namespace,
			name:      event.Name,
			reason:    event.Reason,
			eventType: eventType,
		})
	}()

	return nil
}

type pendingAck struct {
	start     time.Time
	namespace string
	name      string
	reason    string
	eventType string
}

// awaitAck records the outcome of one asynchronous publish. A failed ack has
// nothing to retry from - the job left the queue when it was published - so it
// is only counted and logged.
func (e *Exporter) awaitAck(future nats.PubAckFuture, ack pendingAck) {
	select {
	case <-future.Ok():
		publishLatency.Observe(time.Since(ack.start).Seconds())
		eventsPublished.WithLabelValues(ack.namespace, ack.reason).Inc()

		klog.V(4).InfoS("Published event",
			"namespace", ack.namespace,
			"name", ack.name,
			"reason", ack.reason,
			"type", ack.eventType,
			"subject", future.Msg().Subject,
		)
	case err := <-future.Err():
		publishErrors.Inc()
		klog.ErrorS(err, "Failed to publish event",
			"namespace", ack.namespace,
			"name", ack.name,
		)
	}
}

// awaitPendingAcks waits out the acks outstanding at shutdown. A publish still
// unresolved at the deadline has not failed, so it is logged rather than
// counted against publishErrors.
func (e *Exporter) awaitPendingAcks() {
	done := make(chan struct{})
	go func() {
		defer close(done)
		e.inFlight.Wait()
	}()

	select {
	case <-done:
	case <-time.After(ackDrainTimeout):
		klog.ErrorS(fmt.Errorf("ack drain timed out after %s", ackDrainTimeout),
			"Publishes unresolved at shutdown", "pending", e.js.PublishAsyncPending())
	}
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
