package processor

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/nats-io/nats.go"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/klog/v2"

	"go.miloapis.com/activity/internal/cel"
	"go.miloapis.com/activity/pkg/apis/activity/v1alpha1"
)

// EventProcessor processes Kubernetes events from a NATS JetStream pull consumer
// and generates Activity records via ActivityPolicy event rules.
type EventProcessor struct {
	js             nats.JetStreamContext
	streamName     string
	consumerName   string
	activityPrefix string
	batchSize      int
	policyLookup   EventPolicyLookup
	workers        int
	dlqPublisher   DLQPublisher
}

// NewEventProcessor creates a new event processor.
// js is the JetStream context used for both consuming events and publishing activities.
// streamName is the NATS stream to consume from (e.g., "EVENTS").
// consumerName is the durable pull consumer name.
// activityPrefix is the subject prefix for publishing generated activities.
// policyLookup is used to evaluate events against ActivityPolicy event rules.
// dlqPublisher is used to publish failed events to the dead-letter queue.
func NewEventProcessor(
	js nats.JetStreamContext,
	streamName string,
	consumerName string,
	activityPrefix string,
	policyLookup EventPolicyLookup,
	workers int,
	batchSize int,
	dlqPublisher DLQPublisher,
) *EventProcessor {
	return &EventProcessor{
		js:             js,
		streamName:     streamName,
		consumerName:   consumerName,
		activityPrefix: activityPrefix,
		policyLookup:   policyLookup,
		workers:        workers,
		batchSize:      batchSize,
		dlqPublisher:   dlqPublisher,
	}
}

// Run starts the event processor workers and blocks until ctx is cancelled.
func (p *EventProcessor) Run(ctx context.Context) error {
	klog.InfoS("Starting event processor",
		"stream", p.streamName,
		"consumer", p.consumerName,
		"workers", p.workers,
	)

	var wg sync.WaitGroup
	for i := 0; i < p.workers; i++ {
		wg.Add(1)
		go func(workerID int) {
			defer wg.Done()
			p.worker(ctx, workerID)
		}(i)
	}

	klog.InfoS("Event processor running", "workers", p.workers)

	<-ctx.Done()
	wg.Wait()

	klog.Info("Event processor stopped")
	return nil
}

// worker consumes messages from the JetStream pull consumer and processes them.
// Messages are explicitly Acked on success and Naked on failure so that failed
// messages are redelivered (up to MaxDeliver on the consumer config).
func (p *EventProcessor) worker(ctx context.Context, id int) {
	klog.V(4).InfoS("Event worker started", "worker", id)

	sub, err := p.js.PullSubscribe(
		"events.>",
		p.consumerName,
		nats.Bind(p.streamName, p.consumerName),
	)
	if err != nil {
		klog.ErrorS(err, "Failed to create pull subscription for events", "worker", id)
		return
	}
	defer sub.Unsubscribe()

	for {
		select {
		case <-ctx.Done():
			klog.V(4).InfoS("Event worker stopping", "worker", id)
			return
		default:
		}

		msgs, err := sub.Fetch(p.batchSize, nats.MaxWait(5*time.Second))
		if err != nil {
			if err == nats.ErrTimeout {
				continue
			}
			klog.ErrorS(err, "Failed to fetch event messages", "worker", id)
			continue
		}

		for _, msg := range msgs {
			if err := p.processMessage(ctx, msg); err != nil {
				klog.ErrorS(err, "Failed to process event message", "worker", id)
				msg.Nak()
				continue
			}
			msg.Ack()
		}
	}
}

// processMessage processes a single Kubernetes event message.
func (p *EventProcessor) processMessage(ctx context.Context, msg *nats.Msg) error {
	// Keep raw payload for DLQ in case of failure
	rawPayload := json.RawMessage(msg.Data)

	// Parse the Kubernetes event.
	var event map[string]interface{}
	if err := json.Unmarshal(msg.Data, &event); err != nil {
		// Publish to DLQ - unmarshal errors are unrecoverable
		// Tenant is nil for events as they don't have user context
		if dlqErr := p.dlqPublisher.PublishEventFailure(
			ctx, rawPayload, "", 0, -1, ErrorTypeUnmarshal, err, nil, nil,
		); dlqErr != nil {
			klog.ErrorS(dlqErr, "Failed to publish to DLQ")
			return fmt.Errorf("failed to unmarshal event: %w", err)
		}
		// Successfully published to DLQ, message can be ACKed
		return nil
	}

	// Extract involved object info to find matching policy.
	// Kubernetes events have either "regarding" (events.k8s.io/v1) or
	// "involvedObject" (core/v1) to identify the subject resource.
	involvedObject := ResolveInvolvedObject(event)
	if involvedObject == nil {
		klog.V(4).Info("Event has no involved object, skipping")
		return nil
	}

	apiGroup := getStringFromMap(involvedObject, "apiGroup")
	// For core resources, apiVersion is "v1" with empty apiGroup.
	if apiGroup == "" {
		if apiVersion := getStringFromMap(involvedObject, "apiVersion"); apiVersion != "" && apiVersion != "v1" {
			apiGroup = parseAPIGroup(apiVersion)
		}
	}
	kind := getStringFromMap(involvedObject, "kind")

	if kind == "" {
		klog.V(4).InfoS("Could not determine kind from event, skipping")
		return nil
	}

	// Build resource info for DLQ context
	dlqResource := &DeadLetterResource{
		APIGroup:  apiGroup,
		Kind:      kind,
		Name:      getStringFromMap(involvedObject, "name"),
		Namespace: getStringFromMap(involvedObject, "namespace"),
	}

	// Normalize event so CEL expressions can always use event.regarding.
	normalizedEvent := p.normalizeEvent(event, involvedObject)

	// Delegate policy matching to the lookup (avoids import cycle with activityprocessor).
	matched, err := p.policyLookup.MatchEvent(apiGroup, kind, normalizedEvent)
	if err != nil {
		// Extract policy context from error if available
		errorType := ErrorTypeCELSummary // Default to summary since match errors are logged and skipped
		policyName := ""
		policyVersion := int64(0)
		ruleIndex := -1

		var policyErr *PolicyEvaluationError
		if errors.As(err, &policyErr) {
			policyName = policyErr.PolicyName
			ruleIndex = policyErr.RuleIndex
		}

		// Publish to DLQ
		// Tenant is nil for events as they don't have user context
		if dlqErr := p.dlqPublisher.PublishEventFailure(
			ctx, rawPayload, policyName, policyVersion, ruleIndex, errorType, err, dlqResource, nil,
		); dlqErr != nil {
			klog.ErrorS(dlqErr, "Failed to publish to DLQ, NAKing message")
			return fmt.Errorf("failed to match event against policies: %w", err)
		}

		klog.ErrorS(err, "Failed to match event against policies, published to DLQ",
			"apiGroup", apiGroup, "kind", kind, "policy", policyName, "ruleIndex", ruleIndex)
		// Successfully published to DLQ, message can be ACKed
		return nil
	}

	if matched == nil {
		klog.V(4).InfoS("No policy matched event",
			"apiGroup", apiGroup, "kind", kind)
		return nil
	}

	activity := p.buildActivity(event, matched, involvedObject, matched.Summary, matched.Links)

	if err := p.publishActivity(ctx, activity); err != nil {
		return fmt.Errorf("failed to publish activity: %w", err)
	}

	klog.V(3).InfoS("Generated activity from event",
		"activity", activity.Name,
		"summary", activity.Spec.Summary,
		"reason", getStringFromMap(event, "reason"),
	)

	return nil
}

// normalizeEvent creates a copy of the event with a "regarding" field.
// This ensures CEL expressions can consistently use event.regarding regardless
// of whether the original event used "regarding" or "involvedObject".
func (p *EventProcessor) normalizeEvent(event map[string]interface{}, involvedObject map[string]interface{}) map[string]interface{} {
	// If the event already has "regarding", return as-is.
	if _, ok := event["regarding"]; ok {
		return event
	}

	// Create a shallow copy with "regarding" added.
	normalized := make(map[string]interface{}, len(event)+1)
	for k, v := range event {
		normalized[k] = v
	}
	normalized["regarding"] = involvedObject

	return normalized
}

// buildActivity constructs an Activity resource from event data.
func (p *EventProcessor) buildActivity(
	event map[string]interface{},
	matched *MatchedPolicy,
	involvedObject map[string]interface{},
	summary string,
	links []cel.Link,
) *v1alpha1.Activity {
	// Extract timestamp using the shared fallback chain: eventTime -> lastTimestamp
	// -> firstTimestamp -> metadata.creationTimestamp -> now().
	timestamp := resolveEventTimestamp(event)

	// Extract resource info from involved object.
	namespace := getStringFromMap(involvedObject, "namespace")
	resourceName := getStringFromMap(involvedObject, "name")
	resourceUID := getStringFromMap(involvedObject, "uid")
	apiVersion := getStringFromMap(involvedObject, "apiVersion")

	// Resolve actor from reporting controller or source component.
	actor := resolveActorFromEvent(event)

	// Events from controllers are always system-initiated.
	changeSource := ChangeSourceSystem

	// Extract event UID for origin tracking.
	eventUID := ""
	if metadata, ok := event["metadata"].(map[string]interface{}); ok {
		eventUID = getStringFromMap(metadata, "uid")
	}

	// Generate activity name.
	name := activityName("event", eventUID, matched.APIGroup, matched.Kind)

	// Convert links.
	var activityLinks []v1alpha1.ActivityLink
	for _, link := range links {
		activityLinks = append(activityLinks, v1alpha1.ActivityLink{
			Marker: link.Marker,
			Resource: v1alpha1.ActivityResource{
				APIGroup:  getStringFromMap(link.Resource, "apiGroup"),
				Kind:      getStringFromMap(link.Resource, "kind"),
				Name:      getStringFromMap(link.Resource, "name"),
				Namespace: getStringFromMap(link.Resource, "namespace"),
				UID:       getStringFromMap(link.Resource, "uid"),
			},
		})
	}

	return &v1alpha1.Activity{
		TypeMeta: metav1.TypeMeta{
			APIVersion: v1alpha1.SchemeGroupVersion.String(),
			Kind:       "Activity",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:              name,
			Namespace:         namespace,
			CreationTimestamp: metav1.NewTime(timestamp),
			Labels:            eventActivityLabels(changeSource, matched.APIGroup, matched.Kind, getStringFromMap(event, "reason")),
		},
		Spec: v1alpha1.ActivitySpec{
			Summary:      summary,
			ChangeSource: changeSource,
			Actor:        actor,
			Resource: v1alpha1.ActivityResource{
				APIGroup:   matched.APIGroup,
				APIVersion: apiVersion,
				Kind:       matched.Kind,
				Name:       resourceName,
				Namespace:  namespace,
				UID:        resourceUID,
			},
			Links: activityLinks,
			// Extract tenant from scope annotations; fall back to platform scope when absent.
			Tenant: ExtractTenantFromAnnotations(event),
			Origin: v1alpha1.ActivityOrigin{
				Type: "event",
				ID:   eventUID,
			},
		},
	}
}

// publishActivity serializes and publishes an Activity to the NATS ACTIVITIES stream.
func (p *EventProcessor) publishActivity(ctx context.Context, activity *v1alpha1.Activity) error {
	data, err := json.Marshal(activity)
	if err != nil {
		return fmt.Errorf("failed to marshal activity: %w", err)
	}

	subject := p.buildActivitySubject(activity)

	// Use activity name as MsgID for NATS deduplication.
	_, err = p.js.Publish(subject, data, nats.MsgId(activity.Name))
	if err != nil {
		return fmt.Errorf("failed to publish activity to NATS: %w", err)
	}

	return nil
}

// buildActivitySubject returns the NATS subject for routing activities.
// Format: <prefix>.<tenant_type>.<tenant_name>.<api_group>.<origin>.<kind>.<namespace>.<name>
func (p *EventProcessor) buildActivitySubject(activity *v1alpha1.Activity) string {
	prefix := p.activityPrefix

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

// parseAPIGroup extracts the API group from an apiVersion string.
// For "apps/v1", returns "apps". For "v1", returns "".
func parseAPIGroup(apiVersion string) string {
	for i := len(apiVersion) - 1; i >= 0; i-- {
		if apiVersion[i] == '/' {
			return apiVersion[:i]
		}
	}
	return ""
}
