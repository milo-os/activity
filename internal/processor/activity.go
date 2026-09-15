package processor

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	auditv1 "k8s.io/apiserver/pkg/apis/audit/v1"

	"go.miloapis.com/activity/internal/cel"
	"go.miloapis.com/activity/pkg/apis/activity/v1alpha1"
)

// activityName generates a deterministic activity name from the origin event
// identifier and the policy's resource target. The same input always produces
// the same name, enabling NATS message deduplication on retries.
func activityName(originType, originID, apiGroup, kind string) string {
	h := sha256.New()
	h.Write([]byte(originType))
	h.Write([]byte{0}) // separator
	h.Write([]byte(originID))
	h.Write([]byte{0})
	h.Write([]byte(apiGroup))
	h.Write([]byte{0})
	h.Write([]byte(kind))
	return "act-" + hex.EncodeToString(h.Sum(nil))[:12]
}

// ActivityBuilder contains the common fields needed to build an Activity.
type ActivityBuilder struct {
	// Resource information from the policy
	APIGroup string
	Kind     string
}

// BuildFromAudit constructs an Activity from an audit event.
// If resolveKind is provided, it will be used to resolve resource names to Kind in links.
// Returns error if link conversion fails.
func (b *ActivityBuilder) BuildFromAudit(
	audit *auditv1.Event,
	summary string,
	links []cel.Link,
	resolveKind KindResolver,
) (*v1alpha1.Activity, error) {
	// Extract timestamps
	timestamp := audit.RequestReceivedTimestamp.Time
	if timestamp.IsZero() {
		timestamp = time.Now()
	}

	// Extract resource info from ObjectRef
	var namespace, resourceName, apiVersion string
	if audit.ObjectRef != nil {
		namespace = audit.ObjectRef.Namespace
		resourceName = audit.ObjectRef.Name
		apiVersion = audit.ObjectRef.APIVersion
	}

	// Try to get UID from responseObject metadata
	resourceUID := extractResponseUID(audit.ResponseObject)

	// Classify change source and resolve actor
	changeSource := ClassifyChangeSource(audit.User)
	actor := ResolveActor(audit.User)
	tenant := ExtractTenant(audit.User)

	// Generate activity name
	name := activityName("audit", string(audit.AuditID), b.APIGroup, b.Kind)

	// Convert links
	activityLinks, err := ConvertLinks(links, resolveKind)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrActivityBuild, err)
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
			Labels: map[string]string{
				"activity.miloapis.com/origin-type":   "audit",
				"activity.miloapis.com/change-source": changeSource,
				"activity.miloapis.com/api-group":     b.APIGroup,
				"activity.miloapis.com/resource-kind": b.Kind,
			},
		},
		Spec: v1alpha1.ActivitySpec{
			Summary:      summary,
			ChangeSource: changeSource,
			Actor:        actor,
			Resource: v1alpha1.ActivityResource{
				APIGroup:   b.APIGroup,
				APIVersion: apiVersion,
				Kind:       b.Kind,
				Name:       resourceName,
				Namespace:  namespace,
				UID:        resourceUID,
			},
			Links:  activityLinks,
			Tenant: tenant,
			Origin: v1alpha1.ActivityOrigin{
				Type: "audit",
				ID:   string(audit.AuditID),
			},
		},
	}, nil
}

// extractResponseUID extracts the UID from an audit response object's metadata.
func extractResponseUID(responseObject *runtime.Unknown) string {
	if responseObject == nil || len(responseObject.Raw) == 0 {
		return ""
	}

	// We still need to unmarshal the raw response to get metadata.uid
	var obj struct {
		Metadata struct {
			UID string `json:"uid"`
		} `json:"metadata"`
	}
	if err := json.Unmarshal(responseObject.Raw, &obj); err != nil {
		return ""
	}
	return obj.Metadata.UID
}

// BuildFromEvent constructs an Activity from a Kubernetes event.
// If resolveKind is provided, it will be used to resolve resource names to Kind in links.
// Returns error if link conversion fails.
func (b *ActivityBuilder) BuildFromEvent(
	eventMap map[string]interface{},
	summary string,
	links []cel.Link,
	resolveKind KindResolver,
) (*v1alpha1.Activity, error) {
	// Extract the involved/regarding object using the shared fallback chain:
	// "regarding" (events.k8s.io/v1) -> "involvedObject" (core/v1).
	regarding := ResolveInvolvedObject(eventMap)

	// Extract timestamp using the shared fallback chain: eventTime -> lastTimestamp
	// -> firstTimestamp -> metadata.creationTimestamp -> now().
	timestamp := resolveEventTimestamp(eventMap)

	// Extract resource info from regarding
	namespace := GetNestedString(regarding, "namespace")
	resourceName := GetNestedString(regarding, "name")
	resourceUID := GetNestedString(regarding, "uid")
	apiVersion := GetNestedString(regarding, "apiVersion")

	// Events are typically system-generated
	changeSource := ChangeSourceSystem

	// Resolve actor from reporting controller or source component, using the
	// fallback chain shared with the live event processor.
	actor := resolveActorFromEvent(eventMap)

	// Extract tenant from scope annotations; fall back to platform scope when absent.
	tenant := ExtractTenantFromAnnotations(eventMap)

	// Get event UID for origin (extracted before name generation so it can be
	// used as input to the deterministic name hash).
	eventUID := ""
	if metadata, ok := eventMap["metadata"].(map[string]interface{}); ok {
		eventUID = GetNestedString(metadata, "uid")
	}

	// Generate activity name
	name := activityName("event", eventUID, b.APIGroup, b.Kind)

	// Convert links
	activityLinks, err := ConvertLinks(links, resolveKind)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrActivityBuild, err)
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
			Labels:            eventActivityLabels(changeSource, b.APIGroup, b.Kind, getStringFromMap(eventMap, "reason")),
		},
		Spec: v1alpha1.ActivitySpec{
			Summary:      summary,
			ChangeSource: changeSource,
			Actor:        actor,
			Resource: v1alpha1.ActivityResource{
				APIGroup:   b.APIGroup,
				APIVersion: apiVersion,
				Kind:       b.Kind,
				Name:       resourceName,
				Namespace:  namespace,
				UID:        resourceUID,
			},
			Links:  activityLinks,
			Tenant: tenant,
			Origin: v1alpha1.ActivityOrigin{
				Type: "event",
				ID:   eventUID,
			},
		},
	}, nil
}
