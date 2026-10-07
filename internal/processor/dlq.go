package processor

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/nats-io/nats.go"
	"github.com/prometheus/client_golang/prometheus"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/klog/v2"
)

var (
	dlqEventsPublished = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Namespace: "activity_processor",
			Subsystem: "dlq",
			Name:      "events_published_total",
			Help:      "Total number of events published to the dead-letter queue",
		},
		[]string{"event_type", "api_group", "kind", "error_type", "policy_name"},
	)

	dlqPublishLatency = prometheus.NewHistogram(
		prometheus.HistogramOpts{
			Namespace: "activity_processor",
			Subsystem: "dlq",
			Name:      "publish_latency_seconds",
			Help:      "Latency of DLQ publish operations",
			Buckets:   []float64{.001, .005, .01, .025, .05, .1, .25, .5, 1},
		},
	)

	dlqPublishErrors = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Namespace: "activity_processor",
			Subsystem: "dlq",
			Name:      "publish_errors_total",
			Help:      "Total number of errors publishing to the dead-letter queue",
		},
		[]string{"event_type", "error_phase"}, // error_phase: "marshal" or "publish"
	)
)

// RegisterDLQMetrics registers the DLQ publisher's collectors. Only the
// processor publishes dead letters; preview and reindex must not expose them.
func RegisterDLQMetrics(reg prometheus.Registerer) error {
	for _, c := range []prometheus.Collector{dlqEventsPublished, dlqPublishLatency, dlqPublishErrors} {
		if err := reg.Register(c); err != nil {
			return err
		}
	}
	return nil
}

// EventType identifies the type of event that failed processing.
type EventType string

const (
	// EventTypeAudit indicates an audit log event.
	EventTypeAudit EventType = "audit"
	// EventTypeK8sEvent indicates a Kubernetes event.
	EventTypeK8sEvent EventType = "k8s-event"
)

// ErrorType classifies the type of error that caused DLQ routing.
type ErrorType string

const (
	// ErrorTypeCELMatch indicates the CEL match expression failed to evaluate.
	// Example: A match expression like "audit.verb == 'create'" fails due to
	// missing fields or type mismatches in the audit event.
	ErrorTypeCELMatch ErrorType = "cel_match"

	// ErrorTypeCELSummary indicates the CEL summary template failed to render.
	// Example: A summary template like "{{ actor }} created {{ resource.name }}"
	// fails because "resource.name" doesn't exist or has an unexpected type.
	ErrorTypeCELSummary ErrorType = "cel_summary"

	// ErrorTypeUnmarshal indicates the event JSON could not be parsed.
	// Example: Malformed JSON in the NATS message payload, or unexpected
	// structure that doesn't match the expected audit event schema.
	ErrorTypeUnmarshal ErrorType = "unmarshal"

	// ErrorTypeKindResolve indicates the resource-to-kind resolution failed.
	// Example: An audit event references resource "widgets" but no CRD or
	// built-in resource with that plural name exists in the API discovery cache.
	ErrorTypeKindResolve ErrorType = "kind_resolve"
)

// Sentinel errors for error classification.
var (
	// ErrKindResolution indicates a failure to resolve a resource name to its Kind.
	// Example: resolving "deployments" -> "Deployment" via API discovery.
	ErrKindResolution = errors.New("kind resolution failed")

	// ErrActivityBuild indicates a failure to build an Activity from the input.
	// This wraps errors from link conversion, kind resolution during activity building, etc.
	ErrActivityBuild = errors.New("activity build failed")
)

// PolicyEvaluationError wraps an error with policy context for DLQ routing.
type PolicyEvaluationError struct {
	PolicyName string
	RuleIndex  int
	Err        error
}

func (e *PolicyEvaluationError) Error() string {
	return e.Err.Error()
}

func (e *PolicyEvaluationError) Unwrap() error {
	return e.Err
}

// NewPolicyEvaluationError creates a new PolicyEvaluationError.
func NewPolicyEvaluationError(policyName string, ruleIndex int, err error) *PolicyEvaluationError {
	return &PolicyEvaluationError{
		PolicyName: policyName,
		RuleIndex:  ruleIndex,
		Err:        err,
	}
}

// DeadLetterEvent wraps a failed event with error context for debugging.
type DeadLetterEvent struct {
	// Type identifies whether this is an audit log or Kubernetes event.
	Type EventType `json:"type"`

	// OriginalPayload contains the raw event JSON that failed processing.
	OriginalPayload json.RawMessage `json:"originalPayload"`

	// Error contains the error message from the failed processing attempt.
	Error string `json:"error"`

	// ErrorType classifies the type of error (cel_match, cel_summary, unmarshal).
	ErrorType ErrorType `json:"errorType"`

	// PolicyName is the name of the ActivityPolicy that failed evaluation.
	PolicyName string `json:"policyName"`

	// PolicyVersion is the policy.Generation when the failure occurred.
	// Used to trigger immediate retry when the policy is updated.
	PolicyVersion int64 `json:"policyVersion"`

	// RuleIndex is the index of the rule within the policy that failed.
	// -1 indicates the error occurred before rule evaluation.
	RuleIndex int `json:"ruleIndex"`

	// Timestamp is when the failure occurred.
	Timestamp metav1.Time `json:"timestamp"`

	// Tenant contains multi-tenancy information if available.
	Tenant *DeadLetterTenant `json:"tenant,omitempty"`

	// Resource contains information about the target resource.
	Resource *DeadLetterResource `json:"resource,omitempty"`

	// RetryCount tracks how many times this event has been retried.
	RetryCount int `json:"retryCount"`

	// LastRetryAt is when the last retry attempt occurred.
	LastRetryAt *metav1.Time `json:"lastRetryAt,omitempty"`

	// NextRetryAfter is when the event becomes eligible for the next retry.
	// Nil means the event has not been retried yet.
	NextRetryAfter *metav1.Time `json:"nextRetryAfter,omitempty"`
}

// DeadLetterTenant contains tenant information for a dead-lettered event.
type DeadLetterTenant struct {
	// Type is the tenant type (platform, organization, project).
	Type string `json:"type,omitempty"`
	// Name is the tenant name.
	Name string `json:"name,omitempty"`
}

// NewDeadLetterTenantFromActivity creates a DeadLetterTenant from an ActivityTenant.
// Returns nil if the input tenant has default/empty values.
func NewDeadLetterTenantFromActivity(tenantType, tenantName string) *DeadLetterTenant {
	if tenantType == "" && tenantName == "" {
		return nil
	}
	return &DeadLetterTenant{
		Type: tenantType,
		Name: tenantName,
	}
}

// DeadLetterResource contains resource information for a dead-lettered event.
type DeadLetterResource struct {
	// APIGroup is the API group of the resource (empty for core resources).
	APIGroup string `json:"apiGroup,omitempty"`
	// Kind is the kind of the resource.
	Kind string `json:"kind,omitempty"`
	// Name is the name of the resource.
	Name string `json:"name,omitempty"`
	// Namespace is the namespace of the resource (empty for cluster-scoped).
	Namespace string `json:"namespace,omitempty"`
}

// DLQConfig contains configuration for the dead-letter queue.
type DLQConfig struct {
	// Enabled controls whether failed events are published to the DLQ.
	Enabled bool

	// StreamName is the NATS JetStream stream name for the DLQ.
	StreamName string

	// SubjectPrefix is the subject prefix for DLQ messages.
	// Messages are published to: <prefix>.<event_type>.<api_group>.<kind>
	SubjectPrefix string
}

// DefaultDLQConfig returns the default DLQ configuration.
func DefaultDLQConfig() DLQConfig {
	return DLQConfig{
		Enabled:       true,
		StreamName:    "ACTIVITY_DEAD_LETTER",
		SubjectPrefix: "activity.dlq",
	}
}

// DLQPublisher publishes failed events to the dead-letter queue.
type DLQPublisher interface {
	// PublishAuditFailure publishes a failed audit event to the DLQ.
	// policyVersion should be the policy.Generation when available, or 0 if no policy.
	// Returns nil if the DLQ is disabled.
	PublishAuditFailure(ctx context.Context, payload json.RawMessage, policyName string, policyVersion int64, ruleIndex int, errorType ErrorType, err error, resource *DeadLetterResource, tenant *DeadLetterTenant) error

	// PublishEventFailure publishes a failed Kubernetes event to the DLQ.
	// policyVersion should be the policy.Generation when available, or 0 if no policy.
	// Returns nil if the DLQ is disabled.
	PublishEventFailure(ctx context.Context, payload json.RawMessage, policyName string, policyVersion int64, ruleIndex int, errorType ErrorType, err error, resource *DeadLetterResource, tenant *DeadLetterTenant) error
}

// NATSDLQPublisher implements DLQPublisher using NATS JetStream.
type NATSDLQPublisher struct {
	js     nats.JetStreamContext
	config DLQConfig
}

// NewDLQPublisher creates a new DLQ publisher.
// Returns nil if the DLQ is disabled.
func NewDLQPublisher(js nats.JetStreamContext, config DLQConfig) DLQPublisher {
	if !config.Enabled {
		return &noopDLQPublisher{}
	}

	return &NATSDLQPublisher{
		js:     js,
		config: config,
	}
}

// PublishAuditFailure publishes a failed audit event to the DLQ.
func (p *NATSDLQPublisher) PublishAuditFailure(ctx context.Context, payload json.RawMessage, policyName string, policyVersion int64, ruleIndex int, errorType ErrorType, err error, resource *DeadLetterResource, tenant *DeadLetterTenant) error {
	return p.publish(ctx, EventTypeAudit, payload, policyName, policyVersion, ruleIndex, errorType, err, resource, tenant)
}

// PublishEventFailure publishes a failed Kubernetes event to the DLQ.
func (p *NATSDLQPublisher) PublishEventFailure(ctx context.Context, payload json.RawMessage, policyName string, policyVersion int64, ruleIndex int, errorType ErrorType, err error, resource *DeadLetterResource, tenant *DeadLetterTenant) error {
	return p.publish(ctx, EventTypeK8sEvent, payload, policyName, policyVersion, ruleIndex, errorType, err, resource, tenant)
}

// publish publishes a dead-letter event to NATS.
func (p *NATSDLQPublisher) publish(ctx context.Context, eventType EventType, payload json.RawMessage, policyName string, policyVersion int64, ruleIndex int, errorType ErrorType, originalErr error, resource *DeadLetterResource, tenant *DeadLetterTenant) error {
	// Safely extract error message
	errMsg := ""
	if originalErr != nil {
		errMsg = originalErr.Error()
	}

	dlEvent := DeadLetterEvent{
		Type:            eventType,
		OriginalPayload: payload,
		Error:           errMsg,
		ErrorType:       errorType,
		PolicyName:      policyName,
		PolicyVersion:   policyVersion,
		RuleIndex:       ruleIndex,
		Timestamp:       metav1.Now(),
		Resource:        resource,
		Tenant:          tenant,
		RetryCount:      0,
		LastRetryAt:     nil,
		NextRetryAfter:  nil,
	}

	data, err := json.Marshal(dlEvent)
	if err != nil {
		dlqPublishErrors.WithLabelValues(string(eventType), "marshal").Inc()
		return fmt.Errorf("failed to marshal DLQ event: %w", err)
	}

	// Build subject: <prefix>.<event_type>.<api_group>.<kind>
	apiGroup := "unknown"
	kind := "unknown"
	if resource != nil {
		if resource.APIGroup != "" {
			apiGroup = resource.APIGroup
		} else {
			apiGroup = "core"
		}
		if resource.Kind != "" {
			kind = resource.Kind
		}
	}
	subject := fmt.Sprintf("%s.%s.%s.%s", p.config.SubjectPrefix, eventType, apiGroup, kind)

	// Use a timeout to prevent indefinite blocking on slow/stuck NATS
	publishCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	publishStart := time.Now()
	_, err = p.js.Publish(subject, data, nats.Context(publishCtx))
	dlqPublishLatency.Observe(time.Since(publishStart).Seconds())

	if err != nil {
		dlqPublishErrors.WithLabelValues(string(eventType), "publish").Inc()
		return fmt.Errorf("failed to publish to DLQ: %w", err)
	}

	dlqEventsPublished.WithLabelValues(
		string(eventType),
		apiGroup,
		kind,
		string(errorType),
		policyName,
	).Inc()

	klog.V(2).InfoS("Published event to DLQ",
		"eventType", eventType,
		"policy", policyName,
		"ruleIndex", ruleIndex,
		"errorType", errorType,
		"subject", subject,
	)

	return nil
}

// noopDLQPublisher is a no-op implementation when DLQ is disabled.
type noopDLQPublisher struct{}

func (p *noopDLQPublisher) PublishAuditFailure(ctx context.Context, payload json.RawMessage, policyName string, policyVersion int64, ruleIndex int, errorType ErrorType, err error, resource *DeadLetterResource, tenant *DeadLetterTenant) error {
	return nil
}

func (p *noopDLQPublisher) PublishEventFailure(ctx context.Context, payload json.RawMessage, policyName string, policyVersion int64, ruleIndex int, errorType ErrorType, err error, resource *DeadLetterResource, tenant *DeadLetterTenant) error {
	return nil
}
