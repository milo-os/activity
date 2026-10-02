package activityprocessor

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"sync"
	"time"

	"github.com/nats-io/nats.go"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/meta"
	auditv1 "k8s.io/apiserver/pkg/apis/audit/v1"
	"k8s.io/client-go/discovery"
	"k8s.io/client-go/discovery/cached/memory"
	"k8s.io/client-go/kubernetes"
	typedcorev1 "k8s.io/client-go/kubernetes/typed/core/v1"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/restmapper"
	toolscache "k8s.io/client-go/tools/cache"
	"k8s.io/client-go/tools/record"
	"k8s.io/klog/v2"
	"sigs.k8s.io/controller-runtime/pkg/cache"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/healthz"
	"sigs.k8s.io/controller-runtime/pkg/metrics"

	"go.miloapis.com/activity/internal/controller"
	"go.miloapis.com/activity/internal/natsconn"
	"go.miloapis.com/activity/internal/processor"
	"go.miloapis.com/activity/pkg/apis/activity/v1alpha1"
)

var (
	eventsReceived = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Namespace: "activity_processor",
			Name:      "events_received_total",
			Help:      "Total number of events received from NATS",
		},
		[]string{"source", "api_group", "resource"},
	)

	eventsEvaluated = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Namespace: "activity_processor",
			Name:      "events_evaluated_total",
			Help:      "Total number of events evaluated against policies",
		},
		[]string{"source", "policy", "api_group", "kind", "matched"},
	)

	eventsSkipped = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Namespace: "activity_processor",
			Name:      "events_skipped_total",
			Help:      "Total number of events skipped",
		},
		[]string{"source", "reason"},
	)

	eventsErrored = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Namespace: "activity_processor",
			Name:      "events_errored_total",
			Help:      "Total number of events that encountered errors",
		},
		[]string{"source", "error_type"},
	)

	activitiesGenerated = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Namespace: "activity_processor",
			Name:      "activities_generated_total",
			Help:      "Total number of activities generated and published",
		},
		[]string{"policy", "api_group", "kind"},
	)

	eventProcessingDuration = prometheus.NewHistogramVec(
		prometheus.HistogramOpts{
			Namespace: "activity_processor",
			Name:      "event_processing_duration_seconds",
			Help:      "Time spent processing events per policy",
			Buckets:   prometheus.DefBuckets,
		},
		[]string{"source", "policy"},
	)

	policyCount = prometheus.NewGauge(
		prometheus.GaugeOpts{
			Namespace: "activity_processor",
			Name:      "active_policies",
			Help:      "Number of active (Ready) policies being used",
		},
	)

	workerCount = prometheus.NewGauge(
		prometheus.GaugeOpts{
			Namespace: "activity_processor",
			Name:      "active_workers",
			Help:      "Number of active worker goroutines",
		},
	)

	// NATS client metrics
	natsConnectionStatus = prometheus.NewGauge(
		prometheus.GaugeOpts{
			Namespace: "activity_processor",
			Subsystem: "nats",
			Name:      "connection_status",
			Help:      "NATS connection status (1 = connected, 0 = disconnected)",
		},
	)

	natsDisconnectsTotal = prometheus.NewCounter(
		prometheus.CounterOpts{
			Namespace: "activity_processor",
			Subsystem: "nats",
			Name:      "disconnects_total",
			Help:      "Total number of NATS disconnection events",
		},
	)

	natsReconnectsTotal = prometheus.NewCounter(
		prometheus.CounterOpts{
			Namespace: "activity_processor",
			Subsystem: "nats",
			Name:      "reconnects_total",
			Help:      "Total number of NATS reconnection events",
		},
	)

	natsErrorsTotal = prometheus.NewCounter(
		prometheus.CounterOpts{
			Namespace: "activity_processor",
			Subsystem: "nats",
			Name:      "errors_total",
			Help:      "Total number of NATS async errors",
		},
	)

	natsMessagesPublished = prometheus.NewCounter(
		prometheus.CounterOpts{
			Namespace: "activity_processor",
			Subsystem: "nats",
			Name:      "messages_published_total",
			Help:      "Total number of messages published to NATS",
		},
	)

	natsPublishLatency = prometheus.NewHistogram(
		prometheus.HistogramOpts{
			Namespace: "activity_processor",
			Subsystem: "nats",
			Name:      "publish_latency_seconds",
			Help:      "Latency of NATS publish operations",
			Buckets:   []float64{.001, .005, .01, .025, .05, .1, .25, .5, 1},
		},
	)
)

func init() {
	// Use controller-runtime's registry so metrics are exposed alongside controller metrics.
	metrics.Registry.MustRegister(
		eventsReceived,
		eventsEvaluated,
		eventsSkipped,
		eventsErrored,
		activitiesGenerated,
		eventProcessingDuration,
		policyCount,
		workerCount,
		// NATS metrics
		natsConnectionStatus,
		natsDisconnectsTotal,
		natsReconnectsTotal,
		natsErrorsTotal,
		natsMessagesPublished,
		natsPublishLatency,
	)
}

// Config contains configuration for the activity processor.
type Config struct {
	// NATS configuration
	Input          natsconn.Endpoint // Broker events are consumed from
	NATSStreamName string            // Source stream for audit events (e.g., "AUDIT_EVENTS")
	ConsumerName   string            // Durable consumer name

	// Event stream configuration
	NATSEventStream   string // Source stream for Kubernetes events (e.g., "EVENTS")
	NATSEventConsumer string // Durable consumer name for event processor

	// Output NATS stream for generated activities
	OutputStreamName    string // Stream for publishing activities (e.g., "ACTIVITIES")
	OutputSubjectPrefix string // Subject prefix for activities (e.g., "activities")

	// Output, when its URL is set, publishes activities to a second broker
	// instead of the one events are consumed from. Unset TLS fields inherit
	// the input connection's; see outputEndpoint.
	Output natsconn.Endpoint

	// Dead-letter queue configuration
	DLQEnabled       bool   // Enable dead-letter queue for failed events
	DLQStreamName    string // NATS stream name for DLQ (e.g., "ACTIVITY_DEAD_LETTER")
	DLQSubjectPrefix string // Subject prefix for DLQ messages (e.g., "activity.dlq")

	// DLQ retry configuration
	DLQRetryEnabled           bool          // Enable automatic retry of DLQ events
	DLQRetryInterval          time.Duration // Interval between retry batches
	DLQRetryBatchSize         int           // Number of events to process per retry batch
	DLQRetryBackoffBase       time.Duration // Initial backoff duration
	DLQRetryBackoffMultiplier float64       // Exponential backoff multiplier
	DLQRetryBackoffMax        time.Duration // Maximum backoff duration
	DLQRetryAlertThreshold    int           // Retry count threshold for alerts
	DLQRetryAuditSubject      string        // Subject for republishing audit events from DLQ
	DLQRetryEventSubject      string        // Subject for republishing Kubernetes events from DLQ

	// Processing configuration
	Workers    int           // Number of concurrent workers
	BatchSize  int           // Messages to fetch per batch
	AckWait    time.Duration // Time before message redelivery
	MaxDeliver int           // Maximum redelivery attempts

	// Health probe configuration
	HealthProbeAddr string // Address for health probe server (e.g., ":8081")

}

// DefaultConfig returns configuration with default values.
func DefaultConfig() Config {
	return Config{
		Input:                     natsconn.Endpoint{URL: "nats://localhost:4222"},
		NATSStreamName:            "AUDIT_EVENTS",
		ConsumerName:              "activity-processor@activity.miloapis.com",
		NATSEventStream:           "EVENTS",
		NATSEventConsumer:         "activity-event-processor",
		OutputStreamName:          "ACTIVITIES",
		OutputSubjectPrefix:       "activities",
		DLQEnabled:                true,
		DLQStreamName:             "ACTIVITY_DEAD_LETTER",
		DLQSubjectPrefix:          "activity.dlq",
		DLQRetryEnabled:           true,
		DLQRetryInterval:          5 * time.Minute,
		DLQRetryBatchSize:         100,
		DLQRetryBackoffBase:       1 * time.Minute,
		DLQRetryBackoffMultiplier: 2.0,
		DLQRetryBackoffMax:        24 * time.Hour,
		DLQRetryAlertThreshold:    10,
		Workers:                   4,
		BatchSize:                 100,
		AckWait:                   30 * time.Second,
		MaxDeliver:                5,
		HealthProbeAddr:           ":8081",
	}
}

// Processor consumes audit events from NATS, evaluates ActivityPolicies,
// and publishes Activity resources to NATS for downstream consumption.
type Processor struct {
	config     Config
	restConfig *rest.Config

	nc *nats.Conn
	js nats.JetStreamContext

	// outputNC is nil unless Config.Output selects a second broker;
	// outputJS always points at the context activities are published on.
	outputNC *nats.Conn
	outputJS nats.JetStreamContext

	cache cache.Cache

	// mapper converts Kind to Resource using API discovery. Requires explicit
	// Reset() on cache miss to discover newly registered CRDs.
	mapper meta.ResettableRESTMapper

	// policyCache holds pre-compiled policies indexed by apiGroup/resource.
	policyCache *PolicyCache

	// eventProcessor processes Kubernetes events from the EVENTS stream.
	eventProcessor *processor.EventProcessor

	// eventEmitter emits Kubernetes Warning events for evaluation failures.
	eventEmitter *EventEmitter

	// dlqPublisher publishes failed events to the dead-letter queue.
	dlqPublisher processor.DLQPublisher

	// dlqRetryController handles automatic retry of DLQ events.
	dlqRetryController *DLQRetryController

	wg     sync.WaitGroup
	ctx    context.Context
	cancel context.CancelFunc

	// Health tracking
	healthMu     sync.RWMutex
	ready        bool // Cache synced and NATS connected
	healthServer *http.Server
}

// New creates a new activity processor.
func New(config Config, restConfig *rest.Config) (*Processor, error) {
	ctx, cancel := context.WithCancel(context.Background())

	p := &Processor{
		config:      config,
		restConfig:  restConfig,
		policyCache: NewPolicyCache(),
		ctx:         ctx,
		cancel:      cancel,
	}

	return p, nil
}

// Start begins processing audit events.
func (p *Processor) Start(ctx context.Context) error {
	// Start health probe server early so Kubernetes can check liveness
	if p.config.HealthProbeAddr != "" {
		p.startHealthServer()
	}

	discoveryClient, err := discovery.NewDiscoveryClientForConfig(p.restConfig)
	if err != nil {
		return fmt.Errorf("failed to create discovery client: %w", err)
	}
	cachedDiscoveryClient := memory.NewMemCacheClient(discoveryClient)
	p.mapper = restmapper.NewDeferredDiscoveryRESTMapper(cachedDiscoveryClient)

	c, err := cache.New(p.restConfig, cache.Options{
		Scheme: controller.Scheme,
	})
	if err != nil {
		return fmt.Errorf("failed to create cache: %w", err)
	}
	p.cache = c

	informer, err := p.cache.GetInformer(ctx, &v1alpha1.ActivityPolicy{})
	if err != nil {
		return fmt.Errorf("failed to get informer for ActivityPolicy: %w", err)
	}

	_, err = informer.AddEventHandler(toolscache.ResourceEventHandlerFuncs{
		AddFunc:    p.onPolicyAdd,
		UpdateFunc: p.onPolicyUpdate,
		DeleteFunc: p.onPolicyDelete,
	})
	if err != nil {
		return fmt.Errorf("failed to add event handler: %w", err)
	}

	p.wg.Add(1)
	go func() {
		defer p.wg.Done()
		if err := p.cache.Start(ctx); err != nil {
			klog.ErrorS(err, "Cache failed")
		}
	}()

	if !p.cache.WaitForCacheSync(ctx) {
		return fmt.Errorf("failed to sync cache")
	}

	klog.InfoS("ActivityPolicy cache synced")

	// Create controller-runtime client for event emission
	k8sClient, err := client.New(p.restConfig, client.Options{
		Scheme: controller.Scheme,
	})
	if err != nil {
		return fmt.Errorf("failed to create kubernetes client: %w", err)
	}

	// Create Kubernetes clientset for event broadcaster
	clientset, err := kubernetes.NewForConfig(p.restConfig)
	if err != nil {
		return fmt.Errorf("failed to create kubernetes clientset: %w", err)
	}

	// Create event broadcaster and recorder for emitting Kubernetes events
	eventBroadcaster := record.NewBroadcaster()
	eventBroadcaster.StartRecordingToSink(&typedcorev1.EventSinkImpl{
		Interface: clientset.CoreV1().Events(""),
	})
	recorder := eventBroadcaster.NewRecorder(controller.Scheme, corev1.EventSource{Component: "activity-processor"})

	// Create event emitter for health reporting
	p.eventEmitter = NewEventEmitter(k8sClient, recorder)

	// Build NATS connection options
	natsOpts := []nats.Option{
		nats.RetryOnFailedConnect(true),
		nats.MaxReconnects(-1),
		nats.ReconnectWait(time.Second),
		nats.ReconnectJitter(100*time.Millisecond, time.Second),
		nats.DisconnectErrHandler(func(nc *nats.Conn, err error) {
			natsConnectionStatus.Set(0)
			natsDisconnectsTotal.Inc()
			if err != nil {
				klog.ErrorS(err, "NATS disconnected")
			} else {
				klog.Info("NATS disconnected")
			}
		}),
		nats.ReconnectHandler(func(nc *nats.Conn) {
			natsConnectionStatus.Set(1)
			natsReconnectsTotal.Inc()
			klog.InfoS("NATS reconnected", "url", nc.ConnectedUrl())
		}),
		nats.ClosedHandler(func(nc *nats.Conn) {
			natsConnectionStatus.Set(0)
			klog.Info("NATS connection closed")
		}),
		nats.ErrorHandler(func(nc *nats.Conn, sub *nats.Subscription, err error) {
			natsErrorsTotal.Inc()
			subName := ""
			if sub != nil {
				subName = sub.Subject
			}
			klog.ErrorS(err, "NATS async error", "subject", subName)
		}),
		nats.LameDuckModeHandler(func(nc *nats.Conn) {
			klog.InfoS("NATS server entering lame duck mode, will reconnect to another server")
		}),
	}

	nc, err := p.config.Input.Connect("activity-processor", natsOpts...)
	if err != nil {
		return fmt.Errorf("failed to connect to NATS: %w", err)
	}
	p.nc = nc
	natsConnectionStatus.Set(1)

	js, err := nc.JetStream()
	if err != nil {
		p.closeNATS()
		return fmt.Errorf("failed to create JetStream context: %w", err)
	}
	p.js = js

	// Without a second broker the output context is the input one, so every
	// publish path below behaves exactly as it did on a single connection.
	p.outputJS = js
	if p.config.usesSeparateOutputBroker() {
		if err := p.connectOutput(); err != nil {
			p.closeNATS()
			return err
		}
	}

	// Streams and consumers are managed declaratively via NATS JetStream controller.
	// Fail fast if they don't exist rather than attempting to create them.
	if p.auditConsumptionEnabled() {
		_, err = js.ConsumerInfo(p.config.NATSStreamName, p.config.ConsumerName)
		if err != nil {
			p.closeNATS()
			return fmt.Errorf("consumer %q not found on stream %q (ensure NATS JetStream resources are deployed): %w",
				p.config.ConsumerName, p.config.NATSStreamName, err)
		}
	} else {
		klog.InfoS("Audit event consumption disabled (no audit stream configured)")
	}

	_, err = p.outputJS.StreamInfo(p.config.OutputStreamName)
	if err != nil {
		p.closeNATS()
		return fmt.Errorf("output stream %q not found (ensure NATS JetStream resources are deployed): %w",
			p.config.OutputStreamName, err)
	}

	dlqConfig := p.initDLQPublisher()
	if dlqConfig.Enabled {
		klog.InfoS("Dead-letter queue enabled",
			"stream", dlqConfig.StreamName,
			"subjectPrefix", dlqConfig.SubjectPrefix,
		)
	}

	// Initialize DLQ retry controller. It republishes to input-side subjects,
	// so it cannot run when the dead-letter stream lives on the output broker.
	if p.config.DLQRetryEnabled && dlqConfig.Enabled && !p.config.usesSeparateOutputBroker() {
		retryConfig := DLQRetryConfig{
			Enabled:           p.config.DLQRetryEnabled,
			Interval:          p.config.DLQRetryInterval,
			BatchSize:         p.config.DLQRetryBatchSize,
			BackoffBase:       p.config.DLQRetryBackoffBase,
			BackoffMultiplier: p.config.DLQRetryBackoffMultiplier,
			BackoffMax:        p.config.DLQRetryBackoffMax,
			AlertThreshold:    p.config.DLQRetryAlertThreshold,
			AuditRetrySubject: p.config.DLQRetryAuditSubject,
			EventRetrySubject: p.config.DLQRetryEventSubject,
		}
		p.dlqRetryController = NewDLQRetryController(
			js,
			retryConfig,
			p.config.NATSStreamName,
			p.config.NATSEventStream,
			dlqConfig.StreamName,
			dlqConfig.SubjectPrefix,
			p.reEvaluateDeadLetter,
		)

		p.wg.Add(1)
		go func() {
			defer p.wg.Done()
			if err := p.dlqRetryController.Start(ctx); err != nil {
				klog.ErrorS(err, "DLQ retry controller failed")
			}
		}()
		klog.InfoS("DLQ retry controller started",
			"interval", retryConfig.Interval,
			"batchSize", retryConfig.BatchSize,
		)
	}

	klog.InfoS("Activity processor starting",
		"stream", p.config.NATSStreamName,
		"consumer", p.config.ConsumerName,
		"outputStream", p.config.OutputStreamName,
		"workers", p.config.Workers,
	)

	// Start audit event workers
	if p.auditConsumptionEnabled() {
		workerErrors := make(chan error, p.config.Workers)
		for i := 0; i < p.config.Workers; i++ {
			p.wg.Add(1)
			go p.worker(ctx, i, workerErrors)
		}
		go p.monitorWorkers(ctx, workerErrors)
	}

	// Check that event stream consumer exists
	eventConsumer, err := js.ConsumerInfo(p.config.NATSEventStream, p.config.NATSEventConsumer)
	if err != nil {
		klog.V(1).InfoS("Event consumer not found, event processing will be disabled",
			"stream", p.config.NATSEventStream,
			"consumer", p.config.NATSEventConsumer,
			"error", err,
		)
	} else {
		// Start event processor
		p.eventProcessor = processor.NewEventProcessor(
			p.js,
			p.outputJS,
			p.config.NATSEventStream,
			p.config.NATSEventConsumer,
			eventConsumer.Config.FilterSubject,
			p.config.OutputSubjectPrefix,
			p.policyCache, // PolicyCache implements EventPolicyLookup
			p.config.Workers,
			p.config.BatchSize,
			p.dlqPublisher,
		)
		p.wg.Add(1)
		go func() {
			defer p.wg.Done()
			if err := p.eventProcessor.Run(ctx); err != nil {
				klog.ErrorS(err, "Event processor failed")
			}
		}()
		klog.InfoS("Event processor started",
			"stream", p.config.NATSEventStream,
			"consumer", p.config.NATSEventConsumer,
		)
	}

	// Mark as ready and healthy now that everything is initialized
	p.setReady(true)

	return nil
}

// auditConsumptionEnabled reports whether the audit stream is configured. An
// empty stream name opts a deployment out of audit consumption entirely.
func (p *Processor) auditConsumptionEnabled() bool {
	return p.config.NATSStreamName != ""
}

// initDLQPublisher preflights the dead-letter stream and builds its publisher,
// both on the output connection: the dead-letter stream lives on the broker
// activities are published to.
func (p *Processor) initDLQPublisher() processor.DLQConfig {
	dlqConfig := processor.DLQConfig{
		Enabled:       p.config.DLQEnabled,
		StreamName:    p.config.DLQStreamName,
		SubjectPrefix: p.config.DLQSubjectPrefix,
	}

	if dlqConfig.Enabled {
		if _, err := p.outputJS.StreamInfo(dlqConfig.StreamName); err != nil {
			klog.V(1).InfoS("DLQ stream not found, dead-letter queue will be disabled",
				"stream", dlqConfig.StreamName,
				"error", err,
			)
			dlqConfig.Enabled = false
		}
	}

	p.dlqPublisher = processor.NewDLQPublisher(p.outputJS, dlqConfig)
	return dlqConfig
}

// usesSeparateOutputBroker reports whether activities are published on their
// own connection. Only an output URL selects one: output TLS settings without
// a URL still publish on the input connection.
func (c Config) usesSeparateOutputBroker() bool {
	return c.Output.IsSet()
}

// outputEndpoint returns the second broker, with each TLS field the output
// did not set inherited from the input connection.
func (c Config) outputEndpoint() natsconn.Endpoint {
	e := c.Output
	e.TLS = e.TLS.InheritFrom(c.Input.TLS)
	return e
}

// connectOutput dials the second broker that activities are published to.
func (p *Processor) connectOutput() error {
	// The NATS gauges track the input connection, so this one only logs;
	// giving both connections the same unlabelled metrics would blur them.
	opts := []nats.Option{
		nats.RetryOnFailedConnect(true),
		nats.MaxReconnects(-1),
		nats.ReconnectWait(time.Second),
		nats.ReconnectJitter(100*time.Millisecond, time.Second),
		nats.DisconnectErrHandler(func(nc *nats.Conn, err error) {
			klog.ErrorS(err, "Output NATS disconnected")
		}),
		nats.ReconnectHandler(func(nc *nats.Conn) {
			klog.InfoS("Output NATS reconnected", "url", nc.ConnectedUrl())
		}),
		nats.ClosedHandler(func(nc *nats.Conn) {
			klog.Info("Output NATS connection closed")
		}),
		nats.ErrorHandler(func(nc *nats.Conn, sub *nats.Subscription, err error) {
			klog.ErrorS(err, "Output NATS async error")
		}),
		nats.LameDuckModeHandler(func(nc *nats.Conn) {
			klog.Info("Output NATS server entering lame duck mode, will reconnect to another server")
		}),
	}

	endpoint := p.config.outputEndpoint()
	nc, err := endpoint.Connect("activity-processor-output", opts...)
	if err != nil {
		return fmt.Errorf("failed to connect to output NATS: %w", err)
	}

	js, err := nc.JetStream()
	if err != nil {
		nc.Close()
		return fmt.Errorf("failed to create output JetStream context: %w", err)
	}

	p.outputNC = nc
	p.outputJS = js
	klog.InfoS("Publishing activities to a separate NATS broker", "url", endpoint.URL)
	return nil
}

// closeNATS tears down whichever connections have been established.
func (p *Processor) closeNATS() {
	if p.outputNC != nil {
		p.outputNC.Close()
	}
	if p.nc != nil {
		p.nc.Close()
	}
}

// drainTimeout is the maximum time to wait for NATS connection to drain.
const drainTimeout = 30 * time.Second

// Stop gracefully shuts down the processor.
func (p *Processor) Stop() {
	klog.Info("Stopping activity processor")

	// Mark as unhealthy immediately
	p.setReady(false)

	p.cancel()
	p.wg.Wait()

	// Shutdown health server
	if p.healthServer != nil {
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := p.healthServer.Shutdown(shutdownCtx); err != nil {
			klog.ErrorS(err, "Failed to shutdown health server gracefully")
		}
	}

	// Drain NATS connection gracefully
	if p.nc != nil && !p.nc.IsClosed() {
		klog.Info("Draining NATS connection")
		done := make(chan struct{})
		go func() {
			if err := p.nc.Drain(); err != nil {
				klog.ErrorS(err, "Failed to drain NATS connection, forcing close")
				p.nc.Close()
			}
			close(done)
		}()

		select {
		case <-done:
			klog.Info("NATS connection drained successfully")
		case <-time.After(drainTimeout):
			klog.Warning("NATS drain timed out, forcing close")
			p.nc.Close()
		}
	}

	// Drained after the input connection so activities produced while it winds
	// down still have somewhere to go.
	if p.outputNC != nil && !p.outputNC.IsClosed() {
		klog.Info("Draining output NATS connection")
		done := make(chan struct{})
		go func() {
			if err := p.outputNC.Drain(); err != nil {
				klog.ErrorS(err, "Failed to drain output NATS connection, forcing close")
				p.outputNC.Close()
			}
			close(done)
		}()

		select {
		case <-done:
			klog.Info("Output NATS connection drained successfully")
		case <-time.After(drainTimeout):
			klog.Warning("Output NATS drain timed out, forcing close")
			p.outputNC.Close()
		}
	}
	klog.Info("Activity processor stopped")
}

// monitorWorkers monitors worker health and logs errors.
func (p *Processor) monitorWorkers(ctx context.Context, workerErrors <-chan error) {
	for {
		select {
		case <-ctx.Done():
			return
		case err := <-workerErrors:
			if err != nil {
				klog.ErrorS(err, "Worker reported error")
			}
		}
	}
}

// kindToResource converts a Kind to its plural resource name using API discovery.
func (p *Processor) kindToResource(apiGroup, kind string) (string, error) {
	return processor.NewResourceResolver(p.mapper)(apiGroup, kind)
}

// resourceToKind converts a plural resource name to its Kind using API discovery.
func (p *Processor) resourceToKind(apiGroup, resource string) (string, error) {
	return processor.NewKindResolver(p.mapper)(apiGroup, resource)
}

func (p *Processor) onPolicyAdd(obj any) {
	policy, ok := obj.(*v1alpha1.ActivityPolicy)
	if !ok {
		klog.Error("Failed to cast object to ActivityPolicy in add handler")
		return
	}

	if !isPolicyReady(policy) {
		klog.V(2).InfoS("Skipping policy that is not ready",
			"policy", policy.Name,
		)
		return
	}

	// Convert Kind to resource (plural) to match audit event ObjectRef format.
	resource, err := p.kindToResource(policy.Spec.Resource.APIGroup, policy.Spec.Resource.Kind)
	if err != nil {
		klog.ErrorS(err, "Failed to resolve resource for policy, skipping",
			"policy", policy.Name,
			"apiGroup", policy.Spec.Resource.APIGroup,
			"kind", policy.Spec.Resource.Kind,
		)
		return
	}

	if err := p.policyCache.Add(policy, resource); err != nil {
		klog.ErrorS(err, "Failed to compile and add policy",
			"policy", policy.Name,
		)
		return
	}

	policyCount.Set(float64(p.policyCache.Len()))

	klog.InfoS("Added ActivityPolicy",
		"policy", policy.Name,
		"kind", policy.Spec.Resource.Kind,
		"resource", resource,
	)
}

func (p *Processor) onPolicyUpdate(oldObj, newObj any) {
	oldPolicy, ok := oldObj.(*v1alpha1.ActivityPolicy)
	if !ok {
		klog.Error("Failed to cast old object to ActivityPolicy in update handler")
		return
	}
	newPolicy, ok := newObj.(*v1alpha1.ActivityPolicy)
	if !ok {
		klog.Error("Failed to cast new object to ActivityPolicy in update handler")
		return
	}

	wasReady := isPolicyReady(oldPolicy)
	isReady := isPolicyReady(newPolicy)

	oldResource, oldErr := p.kindToResource(oldPolicy.Spec.Resource.APIGroup, oldPolicy.Spec.Resource.Kind)
	newResource, newErr := p.kindToResource(newPolicy.Spec.Resource.APIGroup, newPolicy.Spec.Resource.Kind)

	if oldErr != nil && wasReady {
		klog.ErrorS(oldErr, "Failed to resolve old resource for policy update",
			"policy", oldPolicy.Name,
			"apiGroup", oldPolicy.Spec.Resource.APIGroup,
			"kind", oldPolicy.Spec.Resource.Kind,
		)
	}
	if newErr != nil && isReady {
		klog.ErrorS(newErr, "Failed to resolve new resource for policy update",
			"policy", newPolicy.Name,
			"apiGroup", newPolicy.Spec.Resource.APIGroup,
			"kind", newPolicy.Spec.Resource.Kind,
		)
		return
	}

	// Remove old policy if it was ready
	if wasReady && oldErr == nil {
		p.policyCache.Remove(oldPolicy, oldResource)
	}

	// Add new policy if it's ready
	if isReady {
		if err := p.policyCache.Add(newPolicy, newResource); err != nil {
			klog.ErrorS(err, "Failed to compile and add updated policy",
				"policy", newPolicy.Name,
			)
		} else {
			klog.InfoS("Updated ActivityPolicy",
				"policy", newPolicy.Name,
				"resource", policyKey(newPolicy.Spec.Resource.APIGroup, newResource),
				"wasReady", wasReady,
				"isReady", isReady,
			)

			// Trigger DLQ retry for events that failed on this policy
			// Check context before spawning to avoid races during shutdown
			if p.dlqRetryController != nil {
				select {
				case <-p.ctx.Done():
					// Processor is shutting down, don't start new retries
				default:
					go p.dlqRetryController.RetryForPolicy(p.ctx, newPolicy)
				}
			}
		}
	} else if wasReady {
		klog.InfoS("ActivityPolicy no longer ready, removed from processing",
			"policy", newPolicy.Name,
		)
	}

	policyCount.Set(float64(p.policyCache.Len()))
}

func (p *Processor) onPolicyDelete(obj any) {
	policy, ok := obj.(*v1alpha1.ActivityPolicy)
	if !ok {
		// Informer may pass a tombstone when the object was deleted while disconnected.
		tombstone, ok := obj.(toolscache.DeletedFinalStateUnknown)
		if !ok {
			klog.Error("Failed to cast object in delete handler")
			return
		}
		policy, ok = tombstone.Obj.(*v1alpha1.ActivityPolicy)
		if !ok {
			klog.Error("Failed to cast tombstone object to ActivityPolicy")
			return
		}
	}

	resource, err := p.kindToResource(policy.Spec.Resource.APIGroup, policy.Spec.Resource.Kind)
	if err != nil {
		klog.ErrorS(err, "Failed to resolve resource for policy delete",
			"policy", policy.Name,
			"apiGroup", policy.Spec.Resource.APIGroup,
			"kind", policy.Spec.Resource.Kind,
		)
		return
	}

	p.policyCache.Remove(policy, resource)
	policyCount.Set(float64(p.policyCache.Len()))

	klog.InfoS("Deleted ActivityPolicy",
		"policy", policy.Name,
		"resource", policyKey(policy.Spec.Resource.APIGroup, resource),
	)
}

func (p *Processor) worker(ctx context.Context, id int, errors chan<- error) {
	defer p.wg.Done()
	defer workerCount.Dec()

	workerCount.Inc()

	sub, err := p.js.PullSubscribe(
		"audit.k8s.>",
		p.config.ConsumerName,
		nats.Bind(p.config.NATSStreamName, p.config.ConsumerName),
	)
	if err != nil {
		klog.ErrorS(err, "Failed to create pull subscription", "worker", id)
		errors <- fmt.Errorf("worker %d: failed to create subscription: %w", id, err)
		return
	}
	defer sub.Unsubscribe()

	klog.V(2).InfoS("Worker started", "worker", id)

	for {
		select {
		case <-ctx.Done():
			klog.V(2).InfoS("Worker stopping", "worker", id)
			return
		default:
		}

		msgs, err := sub.Fetch(p.config.BatchSize, nats.MaxWait(5*time.Second))
		if err != nil {
			if err == nats.ErrTimeout {
				continue
			}
			klog.ErrorS(err, "Failed to fetch messages", "worker", id)
			continue
		}

		for _, msg := range msgs {
			if err := p.processMessage(msg); err != nil {
				klog.ErrorS(err, "Failed to process message", "worker", id)
				msg.Nak()
				continue
			}
			msg.Ack()
		}
	}
}

func (p *Processor) processMessage(msg *nats.Msg) error {
	// Keep raw payload for DLQ in case of failure
	rawPayload := json.RawMessage(msg.Data)

	var audit auditv1.Event
	if err := json.Unmarshal(msg.Data, &audit); err != nil {
		eventsErrored.WithLabelValues("audit_log", "unmarshal").Inc()
		// Publish to DLQ - unmarshal errors are unrecoverable
		// Tenant is nil since we couldn't unmarshal the event
		if dlqErr := p.dlqPublisher.PublishAuditFailure(
			p.ctx, rawPayload, "", 0, -1, processor.ErrorTypeUnmarshal, err, nil, nil,
		); dlqErr != nil {
			klog.ErrorS(dlqErr, "Failed to publish to DLQ")
			return fmt.Errorf("failed to unmarshal audit event: %w", err)
		}
		// Successfully published to DLQ, message can be ACKed
		return nil
	}

	// Extract tenant info early for DLQ context
	activityTenant := processor.ExtractTenant(audit.User)
	dlqTenant := processor.NewDeadLetterTenantFromActivity(activityTenant.Type, activityTenant.Name)

	if audit.ObjectRef == nil {
		eventsSkipped.WithLabelValues("audit_log", "no_object_ref").Inc()
		return nil
	}

	apiGroup := audit.ObjectRef.APIGroup
	if apiGroup == "" {
		apiGroup = "core"
	}
	eventsReceived.WithLabelValues("audit_log", apiGroup, audit.ObjectRef.Resource).Inc()

	// Get compiled policies for this resource
	policies := p.policyCache.Get(audit.ObjectRef.APIGroup, audit.ObjectRef.Resource)
	if len(policies) == 0 {
		eventsSkipped.WithLabelValues("audit_log", "no_matching_policy").Inc()
		return nil
	}

	// Convert audit event to map for CEL evaluation
	auditMap, err := auditToMap(&audit)
	if err != nil {
		eventsErrored.WithLabelValues("audit_log", "unmarshal").Inc()
		// Publish to DLQ - auditToMap errors are unrecoverable
		dlqResource := &processor.DeadLetterResource{
			APIGroup:  audit.ObjectRef.APIGroup,
			Kind:      "", // Can't resolve kind without the map
			Name:      audit.ObjectRef.Name,
			Namespace: audit.ObjectRef.Namespace,
		}
		// Try to resolve kind for better DLQ context
		if kind, kindErr := p.resourceToKind(audit.ObjectRef.APIGroup, audit.ObjectRef.Resource); kindErr == nil {
			dlqResource.Kind = kind
		}
		if dlqErr := p.dlqPublisher.PublishAuditFailure(
			p.ctx, rawPayload, "", 0, -1, processor.ErrorTypeUnmarshal, err, dlqResource, dlqTenant,
		); dlqErr != nil {
			klog.ErrorS(dlqErr, "Failed to publish to DLQ")
			return fmt.Errorf("failed to convert audit to map: %w", err)
		}
		return nil
	}

	// Build resource info for DLQ context with proper kind resolution
	dlqResource := &processor.DeadLetterResource{
		APIGroup:  audit.ObjectRef.APIGroup,
		Kind:      "", // Will be set from policy or resolved dynamically
		Name:      audit.ObjectRef.Name,
		Namespace: audit.ObjectRef.Namespace,
	}
	// Pre-resolve kind for DLQ context (best effort, will be overwritten by policy.Kind when available)
	if kind, kindErr := p.resourceToKind(audit.ObjectRef.APIGroup, audit.ObjectRef.Resource); kindErr == nil {
		dlqResource.Kind = kind
	}

	// First matching policy wins.
	for _, policy := range policies {
		policyStart := time.Now()

		// Use policy's Kind for DLQ context (more accurate than resolved kind)
		dlqResource.Kind = policy.Kind

		// Evaluate audit rules using pre-compiled programs
		activity, ruleIndex, err := p.evaluateCompiledAuditRules(policy, auditMap, &audit)
		if err != nil {
			// Serialize audit event for debugging
			eventJSON, _ := json.Marshal(auditMap)

			klog.ErrorS(err, "Failed to evaluate policy",
				"policy", policy.Name,
				"auditID", audit.AuditID,
				"eventJSON", truncateString(string(eventJSON), 4096),
			)

			// Emit Kubernetes Warning event
			p.eventEmitter.EmitEvaluationError(p.ctx, policy.Name, ruleIndex, err)

			// Classify error type for DLQ using sentinel errors
			errorType := classifyEvaluationError(err, ruleIndex)

			// Publish to DLQ
			policyVersion := int64(0)
			if policy.OriginalPolicy != nil {
				policyVersion = policy.OriginalPolicy.Generation
			}
			if dlqErr := p.dlqPublisher.PublishAuditFailure(
				p.ctx, rawPayload, policy.Name, policyVersion, ruleIndex, errorType, err, dlqResource, dlqTenant,
			); dlqErr != nil {
				klog.ErrorS(dlqErr, "Failed to publish to DLQ, NAKing message")
				eventsErrored.WithLabelValues("audit_log", "evaluate").Inc()
				eventsEvaluated.WithLabelValues(
					"audit_log",
					policy.Name,
					policy.APIGroup,
					policy.Kind,
					"error",
				).Inc()
				eventProcessingDuration.WithLabelValues("audit_log", policy.Name).Observe(time.Since(policyStart).Seconds())
				// Return error to NAK the message so it gets redelivered
				return fmt.Errorf("failed to evaluate policy and publish to DLQ: %w", dlqErr)
			}

			// Successfully published to DLQ, continue to next policy (message will be ACKed)
			eventsErrored.WithLabelValues("audit_log", "evaluate").Inc()
			eventsEvaluated.WithLabelValues(
				"audit_log",
				policy.Name,
				policy.APIGroup,
				policy.Kind,
				"error",
			).Inc()
			eventProcessingDuration.WithLabelValues("audit_log", policy.Name).Observe(time.Since(policyStart).Seconds())
			continue
		}

		ruleMatched := activity != nil
		eventsEvaluated.WithLabelValues(
			"audit_log",
			policy.Name,
			policy.APIGroup,
			policy.Kind,
			fmt.Sprintf("%t", ruleMatched),
		).Inc()

		if !ruleMatched {
			eventProcessingDuration.WithLabelValues("audit_log", policy.Name).Observe(time.Since(policyStart).Seconds())
			continue
		}

		if err := p.publishActivity(activity, policy); err != nil {
			eventsErrored.WithLabelValues("audit_log", "publish").Inc()
			eventProcessingDuration.WithLabelValues("audit_log", policy.Name).Observe(time.Since(policyStart).Seconds())
			return fmt.Errorf("failed to publish activity: %w", err)
		}

		klog.V(4).InfoS("Generated activity",
			"activity", activity.Name,
			"policy", policy.Name,
			"ruleIndex", ruleIndex,
			"auditID", audit.AuditID,
		)

		eventProcessingDuration.WithLabelValues("audit_log", policy.Name).Observe(time.Since(policyStart).Seconds())
		return nil
	}

	return nil
}

// evaluateCompiledAuditRules evaluates audit rules using pre-compiled CEL programs.
func (p *Processor) evaluateCompiledAuditRules(policy *CompiledPolicy, auditMap map[string]any, audit *auditv1.Event) (*v1alpha1.Activity, int, error) {
	return EvaluateCompiledAuditRules(policy, auditMap, audit, p.resourceToKind)
}

// reEvaluateDeadLetter re-runs audit policy evaluation against a dead-letter
// event's original payload so the DLQ retry worker can distinguish a genuinely
// resolved event from one that merely re-publishes and fails again. It mirrors
// the ingest evaluation path (auditToMap + EvaluateCompiledAuditRules) without
// emitting an activity; the retry worker republishes resolved events itself.
func (p *Processor) reEvaluateDeadLetter(_ context.Context, event *processor.DeadLetterEvent) RetryOutcome {
	var audit auditv1.Event
	if err := json.Unmarshal(event.OriginalPayload, &audit); err != nil {
		return RetryOutcome{ErrorType: processor.ErrorTypeUnmarshal, Err: err}
	}

	auditMap, err := auditToMap(&audit)
	if err != nil {
		return RetryOutcome{ErrorType: processor.ErrorTypeUnmarshal, Err: err}
	}

	policies := p.policyCache.Get(audit.ObjectRef.APIGroup, audit.ObjectRef.Resource)
	if len(policies) == 0 {
		// No policy still targets this resource; nothing left to evaluate, so the
		// event can leave the DLQ rather than retry forever.
		return RetryOutcome{Resolved: true}
	}

	for _, policy := range policies {
		_, ruleIndex, err := p.evaluateCompiledAuditRules(policy, auditMap, &audit)
		if err != nil {
			return RetryOutcome{ErrorType: classifyEvaluationError(err, ruleIndex), Err: err}
		}
	}

	return RetryOutcome{Resolved: true}
}

// classifyEvaluationError maps an audit evaluation error to a DLQ error type,
// matching the classification used on the ingest path.
func classifyEvaluationError(err error, ruleIndex int) processor.ErrorType {
	if ruleIndex < 0 {
		return processor.ErrorTypeCELMatch
	}
	if errors.Is(err, processor.ErrKindResolution) || errors.Is(err, processor.ErrActivityBuild) {
		return processor.ErrorTypeKindResolve
	}
	return processor.ErrorTypeCELSummary
}

// auditToMap converts an audit event to a map for CEL evaluation.
func auditToMap(audit *auditv1.Event) (map[string]any, error) {
	data, err := json.Marshal(audit)
	if err != nil {
		return nil, err
	}
	var m map[string]any
	if err := json.Unmarshal(data, &m); err != nil {
		return nil, err
	}
	return m, nil
}

func (p *Processor) publishActivity(activity *v1alpha1.Activity, policy *CompiledPolicy) error {
	data, err := json.Marshal(activity)
	if err != nil {
		return fmt.Errorf("failed to marshal activity: %w", err)
	}

	subject := p.buildActivitySubject(activity)

	// Activity name is unique per audit event, enabling NATS deduplication.
	publishStart := time.Now()
	_, err = p.outputJS.Publish(subject, data, nats.MsgId(activity.Name))
	natsPublishLatency.Observe(time.Since(publishStart).Seconds())
	if err != nil {
		return fmt.Errorf("failed to publish to NATS: %w", err)
	}

	natsMessagesPublished.Inc()
	activitiesGenerated.WithLabelValues(
		policy.Name,
		policy.APIGroup,
		policy.Kind,
	).Inc()
	return nil
}

// buildActivitySubject returns the NATS subject for routing activities.
// Format: <prefix>.<tenant_type>.<tenant_name>.<api_group>.<origin>.<kind>.<namespace>.<name>
func (p *Processor) buildActivitySubject(activity *v1alpha1.Activity) string {
	prefix := p.config.OutputSubjectPrefix

	tenantType := activity.Spec.Tenant.Type
	if tenantType == "" {
		tenantType = "platform"
	}
	tenantName := activity.Spec.Tenant.Name
	if tenantName == "" {
		tenantName = "_"
	}

	apiGroup := activity.Spec.Resource.APIGroup
	if apiGroup == "" {
		apiGroup = "core"
	}

	origin := activity.Spec.Origin.Type
	kind := activity.Spec.Resource.Kind
	namespace := activity.Spec.Resource.Namespace
	if namespace == "" {
		namespace = "_"
	}
	name := activity.Name

	return fmt.Sprintf("%s.%s.%s.%s.%s.%s.%s.%s",
		prefix, tenantType, tenantName, apiGroup, origin, kind, namespace, name)
}

func policyKey(apiGroup, kindOrResource string) string {
	return fmt.Sprintf("%s/%s", apiGroup, kindOrResource)
}

func isPolicyReady(policy *v1alpha1.ActivityPolicy) bool {
	return meta.IsStatusConditionTrue(policy.Status.Conditions, "Ready")
}

// setReady sets the ready status.
func (p *Processor) setReady(ready bool) {
	p.healthMu.Lock()
	defer p.healthMu.Unlock()
	p.ready = ready
}

// isReady returns true if the processor is ready to serve traffic.
func (p *Processor) isReady() bool {
	p.healthMu.RLock()
	defer p.healthMu.RUnlock()
	return p.ready
}

// startHealthServer starts the HTTP health probe server using controller-runtime healthz.
func (p *Processor) startHealthServer() {
	mux := http.NewServeMux()

	// Liveness probe - checks if the processor is alive and NATS is connected
	mux.Handle("/healthz", http.StripPrefix("/healthz", &healthz.Handler{
		Checks: map[string]healthz.Checker{
			"ping": healthz.Ping,
			"nats": p.natsHealthChecker(),
		},
	}))

	// Readiness probe - checks if the processor is ready to receive traffic
	mux.Handle("/readyz", http.StripPrefix("/readyz", &healthz.Handler{
		Checks: map[string]healthz.Checker{
			"ping":           healthz.Ping,
			"nats":           p.natsHealthChecker(),
			"cache-synced":   p.cacheSyncedChecker(),
			"policies-ready": p.policiesReadyChecker(),
		},
	}))

	// Metrics endpoint for Prometheus scraping
	mux.Handle("/metrics", promhttp.HandlerFor(metrics.Registry, promhttp.HandlerOpts{}))

	p.healthServer = &http.Server{
		Addr:    p.config.HealthProbeAddr,
		Handler: mux,
	}

	p.wg.Add(1)
	go func() {
		defer p.wg.Done()
		klog.InfoS("Starting health probe server", "addr", p.config.HealthProbeAddr)
		if err := p.healthServer.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			klog.ErrorS(err, "Health probe server error")
		}
	}()
}

// natsHealthChecker returns a health checker for NATS connection status.
func (p *Processor) natsHealthChecker() healthz.Checker {
	return func(req *http.Request) error {
		if p.nc == nil {
			return fmt.Errorf("NATS connection not initialized")
		}
		if !p.nc.IsConnected() {
			return fmt.Errorf("NATS connection is disconnected")
		}
		// nil means no second broker is configured, not an unhealthy one.
		if p.outputNC != nil && !p.outputNC.IsConnected() {
			return fmt.Errorf("output NATS connection is disconnected")
		}
		return nil
	}
}

// cacheSyncedChecker returns a health checker for cache sync status.
func (p *Processor) cacheSyncedChecker() healthz.Checker {
	return func(req *http.Request) error {
		if !p.isReady() {
			return fmt.Errorf("cache not synced")
		}
		return nil
	}
}

// policiesReadyChecker returns a health checker that verifies policies are loaded.
func (p *Processor) policiesReadyChecker() healthz.Checker {
	return func(req *http.Request) error {
		// This is a soft check - we allow the processor to be ready even with no policies
		// as policies may be added later. We just verify the cache is initialized.
		if p.policyCache == nil {
			return fmt.Errorf("policy cache not initialized")
		}
		return nil
	}
}
