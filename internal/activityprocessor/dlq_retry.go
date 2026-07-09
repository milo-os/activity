package activityprocessor

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"math"
	"sync"
	"time"

	"github.com/nats-io/nats.go"
	"github.com/prometheus/client_golang/prometheus"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/klog/v2"
	"sigs.k8s.io/controller-runtime/pkg/metrics"

	"go.miloapis.com/activity/internal/processor"
	"go.miloapis.com/activity/pkg/apis/activity/v1alpha1"
)

var (
	dlqRetryAttemptsTotal = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Namespace: "activity_processor",
			Subsystem: "dlq_retry",
			Name:      "attempts_total",
			Help:      "Total number of DLQ retry attempts",
		},
		[]string{"trigger", "api_group", "kind", "result"},
	)

	dlqRetryBatchDuration = prometheus.NewHistogramVec(
		prometheus.HistogramOpts{
			Namespace: "activity_processor",
			Subsystem: "dlq_retry",
			Name:      "batch_duration_seconds",
			Help:      "Duration of DLQ retry batch processing",
			Buckets:   prometheus.DefBuckets,
		},
		[]string{"trigger"},
	)

	dlqEventsHighRetryTotal = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Namespace: "activity_processor",
			Subsystem: "dlq_retry",
			Name:      "events_high_retry_total",
			Help:      "Total number of DLQ events that reached high retry threshold",
		},
		[]string{"api_group", "kind", "policy_name"},
	)

	dlqRetryFailedTotal = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Namespace: "activity_processor",
			Subsystem: "dlq_retry",
			Name:      "failed_total",
			Help:      "Total number of DLQ retries that failed re-evaluation and produced no activity",
		},
		[]string{"api_group", "kind", "policy_name", "error_type"},
	)
)

func init() {
	metrics.Registry.MustRegister(
		dlqRetryAttemptsTotal,
		dlqRetryBatchDuration,
		dlqEventsHighRetryTotal,
		dlqRetryFailedTotal,
	)
}

// DLQRetryConfig holds configuration for the DLQ retry controller.
type DLQRetryConfig struct {
	// Enabled controls whether automatic retry is enabled.
	Enabled bool
	// Interval is how often to check for retry-eligible events.
	Interval time.Duration
	// BatchSize is how many events to process per batch.
	BatchSize int
	// BackoffBase is the initial backoff duration.
	BackoffBase time.Duration
	// BackoffMultiplier is the exponential multiplier (typically 2.0).
	BackoffMultiplier float64
	// BackoffMax is the maximum backoff duration.
	BackoffMax time.Duration
	// AlertThreshold triggers metrics when retry count exceeds this.
	AlertThreshold int
	// AuditRetrySubject is the subject to republish audit events to.
	AuditRetrySubject string
	// EventRetrySubject is the subject to republish Kubernetes events to.
	EventRetrySubject string
}

// DefaultDLQRetryConfig returns sensible defaults for DLQ retry.
func DefaultDLQRetryConfig() DLQRetryConfig {
	return DLQRetryConfig{
		Enabled:           true,
		Interval:          5 * time.Minute,
		BatchSize:         100,
		BackoffBase:       1 * time.Minute,
		BackoffMultiplier: 2.0,
		BackoffMax:        24 * time.Hour,
		AlertThreshold:    10,
		AuditRetrySubject: "audit.k8s.retry",
		EventRetrySubject: "events.retry",
	}
}

// RetryOutcome describes the result of re-evaluating a dead-letter event in place.
type RetryOutcome struct {
	// Resolved is true when re-evaluation succeeded (no error). Only resolved
	// events are republished onto the source stream to generate an activity.
	Resolved bool
	// ErrorType classifies the failure when Resolved is false, for metrics.
	ErrorType processor.ErrorType
	// Err is the underlying re-evaluation error when Resolved is false.
	Err error
}

// RetryEvaluator re-runs policy evaluation against a dead-letter event's original
// payload so the retry worker can tell "republished to NATS" apart from
// "successfully processed". A nil evaluator falls back to publish-ACK accounting.
type RetryEvaluator func(ctx context.Context, event *processor.DeadLetterEvent) RetryOutcome

// dlqRetryDurable is the durable pull consumer the retry controller binds to on
// the workqueue DLQ stream. It is provisioned declaratively
// (config/components/nats-streams/dlq-retry-consumer.yaml); a workqueue stream
// rejects overlapping ephemeral consumers, so all replicas share this one.
const dlqRetryDurable = "dlq-retry-worker"

// DLQRetryController manages automatic retry of dead-letter queue events.
type DLQRetryController struct {
	js     nats.JetStreamContext
	config DLQRetryConfig

	// retrySub is the shared, lazily-bound subscription to the durable DLQ
	// consumer. Guarded by mu (every processRetryBatch caller holds mu).
	retrySub *nats.Subscription

	// evaluator re-runs policy evaluation in place before a retry is treated as
	// resolved. When nil, the controller cannot observe evaluation outcomes and
	// only reports that events were republished.
	evaluator RetryEvaluator

	auditStreamName  string
	eventStreamName  string
	dlqStreamName    string
	dlqSubjectPrefix string

	// mu protects concurrent access during policy-triggered retries
	mu sync.Mutex

	// activeRetries tracks which policies have active retry operations
	// to prevent concurrent retries for the same policy
	activeRetries   map[string]bool
	activeRetriesMu sync.Mutex
}

// NewDLQRetryController creates a new DLQ retry controller. evaluator may be nil,
// in which case retries are reported as "republished" rather than "succeeded".
func NewDLQRetryController(
	js nats.JetStreamContext,
	config DLQRetryConfig,
	auditStreamName string,
	eventStreamName string,
	dlqStreamName string,
	dlqSubjectPrefix string,
	evaluator RetryEvaluator,
) *DLQRetryController {
	return &DLQRetryController{
		js:               js,
		config:           config,
		evaluator:        evaluator,
		auditStreamName:  auditStreamName,
		eventStreamName:  eventStreamName,
		dlqStreamName:    dlqStreamName,
		dlqSubjectPrefix: dlqSubjectPrefix,
		activeRetries:    make(map[string]bool),
	}
}

// Start begins the retry controller with periodic retry.
func (c *DLQRetryController) Start(ctx context.Context) error {
	if !c.config.Enabled {
		klog.Info("DLQ retry controller is disabled")
		return nil
	}

	klog.InfoS("Starting DLQ retry controller",
		"interval", c.config.Interval,
		"batchSize", c.config.BatchSize,
		"backoffBase", c.config.BackoffBase,
		"backoffMax", c.config.BackoffMax,
	)

	ticker := time.NewTicker(c.config.Interval)
	defer ticker.Stop()

	// Initial run
	c.periodicRetry(ctx)

	for {
		select {
		case <-ctx.Done():
			klog.Info("DLQ retry controller stopping")
			return nil
		case <-ticker.C:
			c.periodicRetry(ctx)
		}
	}
}

// maxPeriodicRunDuration caps how long a single periodic drain run may take.
// This prevents unbounded processing when the DLQ is being filled faster than
// it can be drained, ensuring the goroutine remains responsive to cancellation.
const maxPeriodicRunDuration = 2 * time.Minute

// periodicRetry drains the DLQ backlog by repeatedly calling processRetryBatch
// until the queue is empty, the context is cancelled, or maxPeriodicRunDuration
// is exceeded. The batch duration metric is recorded per batch; a summary log is
// emitted once at the end of the full drain run.
func (c *DLQRetryController) periodicRetry(ctx context.Context) {
	c.mu.Lock()
	defer c.mu.Unlock()

	runDeadline := time.Now().Add(maxPeriodicRunDuration)

	var totalProcessed, totalSucceeded, totalFailed int
	runStart := time.Now()

	for {
		// Respect context cancellation between batches.
		if ctx.Err() != nil {
			break
		}

		// Enforce the per-run time cap.
		if time.Now().After(runDeadline) {
			klog.InfoS("DLQ periodic retry reached max run duration, deferring remainder to next tick",
				"maxDuration", maxPeriodicRunDuration,
				"totalProcessed", totalProcessed,
			)
			break
		}

		batchStart := time.Now()
		processed, succeeded, failed := c.processRetryBatch(ctx, "periodic", nil)
		dlqRetryBatchDuration.WithLabelValues("periodic").Observe(time.Since(batchStart).Seconds())

		totalProcessed += processed
		totalSucceeded += succeeded
		totalFailed += failed

		// An empty batch means the DLQ has been fully drained.
		if processed == 0 {
			break
		}
	}

	if totalProcessed > 0 {
		klog.InfoS("Completed periodic DLQ retry run",
			"totalProcessed", totalProcessed,
			"totalSucceeded", totalSucceeded,
			"totalFailed", totalFailed,
			"duration", time.Since(runStart),
		)
	}
}

// RetryForPolicy triggers immediate retry for events that match a specific policy.
// This is called when an ActivityPolicy is updated.
func (c *DLQRetryController) RetryForPolicy(ctx context.Context, policy *v1alpha1.ActivityPolicy) {
	if !c.config.Enabled || policy == nil {
		return
	}

	// Check if retry is already in progress for this policy
	c.activeRetriesMu.Lock()
	if c.activeRetries[policy.Name] {
		c.activeRetriesMu.Unlock()
		klog.V(2).InfoS("Retry already in progress for policy, skipping",
			"policy", policy.Name)
		return
	}
	c.activeRetries[policy.Name] = true
	c.activeRetriesMu.Unlock()

	defer func() {
		c.activeRetriesMu.Lock()
		delete(c.activeRetries, policy.Name)
		c.activeRetriesMu.Unlock()
	}()

	c.mu.Lock()
	defer c.mu.Unlock()

	start := time.Now()

	// Build subject filter for this policy's resource type
	apiGroup := policy.Spec.Resource.APIGroup
	if apiGroup == "" {
		apiGroup = "core"
	}
	kind := policy.Spec.Resource.Kind

	filter := &retryFilter{
		apiGroup:         apiGroup,
		kind:             kind,
		policyName:       policy.Name,
		maxPolicyVersion: policy.Generation, // Only retry events from older policy versions
	}

	processed, succeeded, failed := c.processRetryBatch(ctx, "policy_update", filter)
	dlqRetryBatchDuration.WithLabelValues("policy_update").Observe(time.Since(start).Seconds())

	if processed > 0 {
		klog.InfoS("Completed policy-triggered DLQ retry",
			"policy", policy.Name,
			"apiGroup", apiGroup,
			"kind", kind,
			"processed", processed,
			"succeeded", succeeded,
			"failed", failed,
			"duration", time.Since(start),
		)
	}
}

// retryFilter defines criteria for filtering DLQ events.
type retryFilter struct {
	apiGroup         string
	kind             string
	policyName       string
	maxPolicyVersion int64 // Only retry events with PolicyVersion < this
}

// extractResourceInfo extracts apiGroup and kind from a DeadLetterEvent.
// Returns "core" for empty apiGroup and "unknown" for missing values.
func extractResourceInfo(event *processor.DeadLetterEvent) (apiGroup, kind string) {
	apiGroup = "unknown"
	kind = "unknown"
	if event.Resource != nil {
		if event.Resource.APIGroup != "" {
			apiGroup = event.Resource.APIGroup
		} else {
			apiGroup = "core"
		}
		if event.Resource.Kind != "" {
			kind = event.Resource.Kind
		}
	}
	return apiGroup, kind
}

// bindRetryConsumer lazily binds the shared durable pull consumer on the
// workqueue DLQ stream and caches it. Callers must hold c.mu.
//
// The durable is provisioned out of band (dlq-retry-consumer.yaml) with a fixed
// activity.dlq.> filter. Per-policy narrowing happens client-side in
// processRetryBatch, so we never create a second, overlapping consumer — which a
// workqueue stream rejects with "filtered consumer not unique".
func (c *DLQRetryController) bindRetryConsumer() (*nats.Subscription, error) {
	if c.retrySub != nil {
		return c.retrySub, nil
	}
	sub, err := c.js.PullSubscribe(
		"",
		dlqRetryDurable,
		nats.Bind(c.dlqStreamName, dlqRetryDurable),
	)
	if err != nil {
		return nil, err
	}
	c.retrySub = sub
	return sub, nil
}

// processRetryBatch fetches and processes retry-eligible DLQ events.
func (c *DLQRetryController) processRetryBatch(ctx context.Context, trigger string, filter *retryFilter) (processed, succeeded, failed int) {
	// Bind the shared durable consumer. All replicas compete for messages on the
	// same consumer; the filter argument narrows results client-side below.
	sub, err := c.bindRetryConsumer()
	if err != nil {
		klog.ErrorS(err, "Failed to bind DLQ retry consumer", "durable", dlqRetryDurable)
		return 0, 0, 0
	}

	msgs, err := sub.Fetch(c.config.BatchSize, nats.MaxWait(5*time.Second))
	if err != nil && err != nats.ErrTimeout {
		klog.ErrorS(err, "Failed to fetch DLQ messages", "durable", dlqRetryDurable)
		return 0, 0, 0
	}

	now := time.Now()

	for _, msg := range msgs {
		processed++

		// Parse the DLQ event
		var dlEvent processor.DeadLetterEvent
		if err := json.Unmarshal(msg.Data, &dlEvent); err != nil {
			klog.ErrorS(err, "Failed to unmarshal DLQ event")
			if ackErr := msg.Ack(); ackErr != nil {
				klog.ErrorS(ackErr, "Failed to ack corrupt DLQ event")
			}
			failed++
			continue
		}

		// Extract resource info for metrics
		apiGroup, kind := extractResourceInfo(&dlEvent)

		// Check filter criteria
		if filter != nil {
			// For policy-triggered retry, only retry events that:
			// 1. Match the policy name (if specified)
			// 2. Failed on an older policy version
			if filter.policyName != "" && dlEvent.PolicyName != filter.policyName {
				if nakErr := msg.Nak(); nakErr != nil {
					klog.ErrorS(nakErr, "Failed to NAK DLQ message")
				}
				continue
			}
			if filter.maxPolicyVersion > 0 && dlEvent.PolicyVersion >= filter.maxPolicyVersion {
				// Event failed on same or newer policy version, skip
				if nakErr := msg.Nak(); nakErr != nil {
					klog.ErrorS(nakErr, "Failed to NAK DLQ message")
				}
				continue
			}
		} else {
			// For periodic retry, check backoff eligibility. Defer redelivery
			// until the backoff expires; a plain Nak redelivers immediately, so
			// the same backed-off events are re-fetched every batch and the drain
			// loop spins until the run deadline instead of reaching processed==0.
			if !c.isEligibleForRetry(&dlEvent, now) {
				delay := c.config.BackoffBase
				if dlEvent.NextRetryAfter != nil {
					if d := time.Until(dlEvent.NextRetryAfter.Time); d > 0 {
						delay = d
					}
				}
				if nakErr := msg.NakWithDelay(delay); nakErr != nil {
					klog.ErrorS(nakErr, "Failed to NAK DLQ message")
				}
				continue
			}
		}

		// Re-evaluate the event in place before treating the retry as resolved.
		// A NATS-publish ACK only means the republish was accepted, not that
		// policy evaluation passed, so a re-failing event must count as a real
		// failure and keep accumulating its retry count rather than resetting.
		if c.evaluator != nil && dlEvent.Type == processor.EventTypeAudit {
			outcome := c.evaluator(ctx, &dlEvent)
			if !outcome.Resolved {
				c.handleReEvaluationFailure(ctx, msg, &dlEvent, now, trigger, apiGroup, kind, outcome)
				failed++
				continue
			}
		}

		// Evaluation passed (or no evaluator wired): republish so the event
		// re-enters the ingest path and produces an activity.
		if err := c.republishEvent(ctx, &dlEvent); err != nil {
			klog.ErrorS(err, "Failed to republish DLQ event",
				"eventType", dlEvent.Type,
				"policy", dlEvent.PolicyName,
				"retryCount", dlEvent.RetryCount,
			)
			dlqRetryAttemptsTotal.WithLabelValues(trigger, apiGroup, kind, "failed").Inc()
			failed++

			// Update retry metadata and republish to DLQ
			// Only ACK if metadata update succeeds, otherwise NAK to prevent data loss
			if err := c.updateAndRepublishMetadata(ctx, &dlEvent, now); err != nil {
				klog.ErrorS(err, "Failed to update retry metadata, NAKing to preserve event")
				if nakErr := msg.Nak(); nakErr != nil {
					klog.ErrorS(nakErr, "Failed to NAK DLQ message after metadata update failure")
				}
				continue
			}
			if ackErr := msg.Ack(); ackErr != nil {
				klog.ErrorS(ackErr, "Failed to ack DLQ message after metadata update")
			}
			continue
		}

		// Republish accepted - ack the DLQ message.
		if ackErr := msg.Ack(); ackErr != nil {
			klog.ErrorS(ackErr, "Failed to ack successfully retried DLQ message")
		}
		succeeded++

		// "succeeded" means evaluation passed and an activity will be generated;
		// "republished" means we put the event back without observing the
		// outcome (no evaluator for this event type).
		result := "republished"
		if c.evaluator != nil && dlEvent.Type == processor.EventTypeAudit {
			result = "succeeded"
		}
		dlqRetryAttemptsTotal.WithLabelValues(trigger, apiGroup, kind, result).Inc()

		klog.V(2).InfoS("Retried DLQ event",
			"eventType", dlEvent.Type,
			"policy", dlEvent.PolicyName,
			"retryCount", dlEvent.RetryCount,
			"result", result,
		)
	}

	return processed, succeeded, failed
}

// handleReEvaluationFailure records a re-failing dead-letter event as a genuine
// failure: it counts the failure, advances the retry count via the DLQ metadata
// loop, and avoids republishing into the dedup window where the event would be
// silently dropped. On metadata-update failure the message is NAKed to preserve it.
func (c *DLQRetryController) handleReEvaluationFailure(
	ctx context.Context,
	msg *nats.Msg,
	event *processor.DeadLetterEvent,
	now time.Time,
	trigger, apiGroup, kind string,
	outcome RetryOutcome,
) {
	errorType := outcome.ErrorType
	if errorType == "" {
		errorType = processor.ErrorTypeCELSummary
	}

	klog.ErrorS(outcome.Err, "DLQ event re-failed evaluation",
		"eventType", event.Type,
		"policy", event.PolicyName,
		"errorType", errorType,
		"retryCount", event.RetryCount,
	)

	dlqRetryAttemptsTotal.WithLabelValues(trigger, apiGroup, kind, "failed").Inc()
	dlqRetryFailedTotal.WithLabelValues(apiGroup, kind, event.PolicyName, string(errorType)).Inc()

	// Advance the retry count instead of republishing into ingest, so the
	// failure is durably tracked and high-retry alerting can fire.
	if err := c.updateAndRepublishMetadata(ctx, event, now); err != nil {
		klog.ErrorS(err, "Failed to update retry metadata after re-failure, NAKing to preserve event")
		if nakErr := msg.Nak(); nakErr != nil {
			klog.ErrorS(nakErr, "Failed to NAK DLQ message after metadata update failure")
		}
		return
	}
	if ackErr := msg.Ack(); ackErr != nil {
		klog.ErrorS(ackErr, "Failed to ack DLQ message after re-failure metadata update")
	}
}

// isEligibleForRetry checks if an event's backoff has expired.
func (c *DLQRetryController) isEligibleForRetry(event *processor.DeadLetterEvent, now time.Time) bool {
	// First retry is always eligible
	if event.NextRetryAfter == nil {
		return true
	}
	return now.After(event.NextRetryAfter.Time)
}

// republishEvent sends the original payload back to the source stream for reprocessing.
// The retry subjects (audit.k8s.retry and events.retry) must be captured by the corresponding
// NATS stream consumers. Ensure that:
// - AUDIT_LOGS stream includes subject "audit.k8s.retry" in its subject filter
// - EVENTS stream includes subject "events.retry" in its subject filter
// Without this configuration, retried events will not be picked up by processors.
func (c *DLQRetryController) republishEvent(ctx context.Context, event *processor.DeadLetterEvent) error {
	// Determine target stream based on event type
	var targetStream string
	var subject string

	switch event.Type {
	case processor.EventTypeAudit:
		targetStream = c.auditStreamName
		subject = c.config.AuditRetrySubject
	case processor.EventTypeK8sEvent:
		targetStream = c.eventStreamName
		subject = c.config.EventRetrySubject
	default:
		return fmt.Errorf("unknown event type: %s", event.Type)
	}

	// Use a stable message ID for deduplication - excludes retry count so that
	// repeated retries of the same event are deduplicated on the source stream.
	// This prevents duplicates if the DLQ ACK fails after a successful republish.
	payloadHash := computePayloadHash(event.OriginalPayload)
	msgID := fmt.Sprintf("dlq-retry-%s-%s-%s-%s",
		event.Type,
		event.PolicyName,
		payloadHash,
		event.Timestamp.Format(time.RFC3339Nano),
	)

	_, err := c.js.Publish(
		subject,
		event.OriginalPayload,
		nats.MsgId(msgID),
		nats.ExpectStream(targetStream),
	)
	if err != nil {
		return fmt.Errorf("failed to publish to %s: %w", targetStream, err)
	}

	return nil
}

// updateAndRepublishMetadata updates retry metadata and republishes to DLQ.
// Returns an error if the update fails - caller should NAK the original message.
func (c *DLQRetryController) updateAndRepublishMetadata(ctx context.Context, event *processor.DeadLetterEvent, now time.Time) error {
	// Update retry metadata
	event.RetryCount++
	nowTime := metav1.NewTime(now)
	event.LastRetryAt = &nowTime

	// Calculate next retry time
	backoff := c.calculateBackoff(event.RetryCount)
	nextRetry := metav1.NewTime(now.Add(backoff))
	event.NextRetryAfter = &nextRetry

	// Track high retry count events
	if event.RetryCount >= c.config.AlertThreshold {
		apiGroup, kind := extractResourceInfo(event)
		dlqEventsHighRetryTotal.WithLabelValues(apiGroup, kind, event.PolicyName).Inc()
	}

	// Republish updated event to DLQ
	data, err := json.Marshal(event)
	if err != nil {
		return fmt.Errorf("failed to marshal updated DLQ event: %w", err)
	}

	// Build subject for updated event
	apiGroup, kind := extractResourceInfo(event)
	subject := fmt.Sprintf("%s.%s.%s.%s", c.dlqSubjectPrefix, event.Type, apiGroup, kind)

	// Use message ID for deduplication to prevent duplicates in DLQ
	// Include policy name and payload hash for global uniqueness
	payloadHash := computePayloadHash(event.OriginalPayload)
	msgID := fmt.Sprintf("dlq-%s-%s-%s-%s-%d",
		event.Type,
		event.PolicyName,
		payloadHash,
		event.Timestamp.Format(time.RFC3339Nano),
		event.RetryCount,
	)

	_, err = c.js.Publish(subject, data, nats.MsgId(msgID))
	if err != nil {
		return fmt.Errorf("failed to republish updated DLQ event: %w", err)
	}
	return nil
}

// calculateBackoff computes exponential backoff: min(base * multiplier^retryCount, max)
func (c *DLQRetryController) calculateBackoff(retryCount int) time.Duration {
	// Cap exponent to prevent overflow (2^40 minutes = ~2 million years)
	if retryCount > 40 {
		return c.config.BackoffMax
	}

	// Exponential backoff: base * multiplier^retryCount
	multiplier := math.Pow(c.config.BackoffMultiplier, float64(retryCount))
	backoff := time.Duration(float64(c.config.BackoffBase) * multiplier)

	// Cap at maximum (also handles potential overflow to negative)
	if backoff > c.config.BackoffMax || backoff < 0 {
		return c.config.BackoffMax
	}
	return backoff
}

// computePayloadHash computes a short hash of the payload for message ID uniqueness.
// Returns the first 8 characters of the SHA256 hash (32 bits of entropy).
func computePayloadHash(payload []byte) string {
	hash := sha256.Sum256(payload)
	return hex.EncodeToString(hash[:4]) // 4 bytes = 8 hex characters
}
