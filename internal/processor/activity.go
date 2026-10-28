package processor

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	auditv1 "k8s.io/apiserver/pkg/apis/audit/v1"

	"go.miloapis.com/activity/internal/cel"
	"go.miloapis.com/activity/pkg/apis/activity/v1alpha1"
)

// iamGroup is the API group for Milo IAM resources we enrich with user
// display names.
const iamGroup = "iam.miloapis.com"

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

	// UserResolver is consulted (when non-nil) to enrich the actor and any
	// User-typed link targets with human-readable display names.
	UserResolver UserResolver
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
	ctx := context.Background()
	changeSource := ClassifyChangeSource(audit.User)
	actor := ResolveActorWithResolver(ctx, audit.User, b.UserResolver)
	tenant := ExtractTenant(audit.User)

	// Generate activity name
	name := activityName("audit", string(audit.AuditID), b.APIGroup, b.Kind)

	// Convert links
	activityLinks, err := ConvertLinks(links, resolveKind)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrActivityBuild, err)
	}

	// Enrich: replace actor email with display name in summary, attach actor
	// link, and hydrate any User-typed link targets with display names.
	summary, activityLinks = enrichSummaryWithDisplayNames(ctx, summary, actor, activityLinks, b.UserResolver)

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

// enrichSummaryWithDisplayNames rewrites the summary to use human-readable
// display names for the actor and any User-typed link targets, and appends
// link metadata so the UI can render an email/UID tooltip.
//
// Behavior:
//   - When the actor has a DisplayName, the first occurrence of the actor's
//     Name (typically an email) in the summary is replaced with the
//     DisplayName, and a synthetic actor link is appended carrying the
//     DisplayName, Email, and UID.
//   - For each existing link whose resource is an iam User, the resolver is
//     queried by the link's resource name; on hit, the link's Marker is
//     replaced in the summary with the user's DisplayName and the link's
//     DisplayName/Email fields are populated.
//
// Returns the rewritten summary and links. When resolver is nil or no
// matches occur, the inputs are returned unchanged.
func enrichSummaryWithDisplayNames(
	ctx context.Context,
	summary string,
	actor v1alpha1.ActivityActor,
	links []v1alpha1.ActivityLink,
	resolver UserResolver,
) (string, []v1alpha1.ActivityLink) {
	// Actor: if we have a display name distinct from the name, swap it into
	// the summary. If the policy template already wrapped the actor with
	// link() (so a link entry exists with marker == actor.Name), upgrade
	// that entry in place; otherwise append a synthetic actor link so the
	// UI can render the hover tooltip.
	if actor.DisplayName != "" && actor.DisplayName != actor.Name && actor.Name != "" {
		summaryHadActor := strings.Contains(summary, actor.Name)
		if summaryHadActor {
			summary = strings.Replace(summary, actor.Name, actor.DisplayName, 1)
		}

		upgraded := false
		for i := range links {
			if links[i].Marker == actor.Name {
				links[i].Marker = actor.DisplayName
				links[i].DisplayName = actor.DisplayName
				if links[i].Email == "" {
					links[i].Email = actor.Email
				}
				upgraded = true
				break
			}
		}
		if !upgraded && summaryHadActor {
			links = append(links, v1alpha1.ActivityLink{
				Marker: actor.DisplayName,
				Resource: v1alpha1.ActivityResource{
					APIGroup: iamGroup,
					Kind:     "User",
					UID:      actor.UID,
				},
				DisplayName: actor.DisplayName,
				Email:       actor.Email,
			})
		}
	}

	// User-typed link targets: hydrate via resolver and rewrite the summary.
	if resolver != nil {
		for i := range links {
			link := &links[i]
			if !isUserLink(link.Resource) {
				continue
			}
			if link.Resource.Name == "" || link.DisplayName != "" {
				continue
			}
			info, ok, err := resolver.LookupByName(ctx, link.Resource.Name)
			if err != nil || !ok {
				continue
			}
			displayName := info.DisplayName()
			if displayName == "" {
				continue
			}
			if link.Marker != "" && link.Marker != displayName {
				summary = strings.Replace(summary, link.Marker, displayName, 1)
				link.Marker = displayName
			}
			link.DisplayName = displayName
			link.Email = info.Email
		}
	}

	return summary, links
}

// isUserLink reports whether the resource targets an iam User CR.
func isUserLink(r v1alpha1.ActivityResource) bool {
	return r.APIGroup == iamGroup && r.Kind == "User"
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
	regarding, _ := eventMap["regarding"].(map[string]interface{})

	// Extract timestamps
	var timestamp time.Time
	if ts, ok := eventMap["eventTime"].(string); ok {
		if t, err := time.Parse(time.RFC3339Nano, ts); err == nil {
			timestamp = t
		}
	}
	if timestamp.IsZero() {
		if metadata, ok := eventMap["metadata"].(map[string]interface{}); ok {
			if ts, ok := metadata["creationTimestamp"].(string); ok {
				if t, err := time.Parse(time.RFC3339, ts); err == nil {
					timestamp = t
				}
			}
		}
	}
	if timestamp.IsZero() {
		timestamp = time.Now()
	}

	// Extract resource info from regarding
	namespace := GetNestedString(regarding, "namespace")
	resourceName := GetNestedString(regarding, "name")
	resourceUID := GetNestedString(regarding, "uid")
	apiVersion := GetNestedString(regarding, "apiVersion")

	// Events are typically system-generated
	changeSource := ChangeSourceSystem

	// For events, actor is usually the reporting component
	reportingController := GetNestedString(eventMap, "reportingController")
	actor := v1alpha1.ActivityActor{
		Type: ActorTypeSystem,
		Name: reportingController,
	}
	if actor.Name == "" {
		actor.Name = "unknown"
	}

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

	// Hydrate User-typed links with display names (event actors are system
	// components, so no actor enrichment is needed).
	summary, activityLinks = enrichSummaryWithDisplayNames(context.Background(), summary, actor, activityLinks, b.UserResolver)

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
				"activity.miloapis.com/origin-type":   "event",
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
				Type: "event",
				ID:   eventUID,
			},
		},
	}, nil
}
