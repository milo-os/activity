package reindex

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	auditv1 "k8s.io/apiserver/pkg/apis/audit/v1"
	"k8s.io/klog/v2"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"go.miloapis.com/activity/internal/activityprocessor"
	"go.miloapis.com/activity/internal/processor"
	"go.miloapis.com/activity/pkg/apis/activity/v1alpha1"
)

// fetchAuditLogBatch queries audit logs via the AuditLogQuery API.
// Returns the batch of audit events, the cursor for the next batch, and any error.
func fetchAuditLogBatch(
	ctx context.Context,
	c client.Client,
	startTime, endTime time.Time,
	cursor string,
	batchSize int32,
) ([]*auditv1.Event, string, error) {
	klog.V(4).InfoS("Fetching audit log batch via API",
		"startTime", startTime,
		"endTime", endTime,
		"cursor", cursor,
		"batchSize", batchSize,
	)

	// Create AuditLogQuery resource
	query := &v1alpha1.AuditLogQuery{
		ObjectMeta: metav1.ObjectMeta{
			GenerateName: "reindex-audit-",
		},
		Spec: v1alpha1.AuditLogQuerySpec{
			StartTime: startTime.Format(time.RFC3339),
			EndTime:   endTime.Format(time.RFC3339),
			Limit:     batchSize,
			Continue:  cursor,
		},
	}

	// Create the query - the API server returns results in status immediately
	if err := c.Create(ctx, query); err != nil {
		return nil, "", fmt.Errorf("failed to create AuditLogQuery: %w", err)
	}

	klog.V(4).InfoS("AuditLogQuery executed",
		"resultsCount", len(query.Status.Results),
		"continue", query.Status.Continue,
	)

	// Convert results to pointers
	batch := make([]*auditv1.Event, len(query.Status.Results))
	for i := range query.Status.Results {
		batch[i] = &query.Status.Results[i]
	}

	return batch, query.Status.Continue, nil
}

// fetchEventBatch queries Kubernetes events via the EventQuery API.
// Returns the batch of events as maps, the cursor for the next batch, and any error.
func fetchEventBatch(
	ctx context.Context,
	c client.Client,
	startTime, endTime time.Time,
	cursor string,
	batchSize int32,
) ([]map[string]interface{}, string, error) {
	klog.V(4).InfoS("Fetching event batch via API",
		"startTime", startTime,
		"endTime", endTime,
		"cursor", cursor,
		"batchSize", batchSize,
	)

	// Create EventQuery resource
	query := &v1alpha1.EventQuery{
		ObjectMeta: metav1.ObjectMeta{
			GenerateName: "reindex-event-",
		},
		Spec: v1alpha1.EventQuerySpec{
			StartTime: startTime.Format(time.RFC3339),
			EndTime:   endTime.Format(time.RFC3339),
			Limit:     batchSize,
			Continue:  cursor,
		},
	}

	// Create the query - the API server returns results in status immediately
	if err := c.Create(ctx, query); err != nil {
		return nil, "", fmt.Errorf("failed to create EventQuery: %w", err)
	}

	klog.V(4).InfoS("EventQuery executed",
		"resultsCount", len(query.Status.Results),
		"continue", query.Status.Continue,
	)

	// Convert EventRecord results to map[string]interface{} for processor compatibility
	batch := make([]map[string]interface{}, len(query.Status.Results))
	for i := range query.Status.Results {
		// The processor expects the event data in map format
		// EventRecord has an Event field that contains the actual event data
		eventMap, err := eventRecordToMap(&query.Status.Results[i])
		if err != nil {
			return nil, "", fmt.Errorf("failed to convert event record %d: %w", i, err)
		}
		batch[i] = eventMap
	}

	return batch, query.Status.Continue, nil
}

// evaluateAuditBatch evaluates a batch of audit events using pre-compiled policies.
func (r *Reindexer) evaluateAuditBatch(ctx context.Context, batch []*auditv1.Event) ([]*v1alpha1.Activity, error) {
	var activities []*v1alpha1.Activity

	// Activities already recorded as staff access stay "Datum Support", even
	// if the staff member has since joined the organization.
	enrichment := r.Enrichment
	keep, err := r.storedStaffActivities(ctx, batch)
	if err != nil {
		return nil, err
	}
	enrichment.KeepSupport = keep

	for _, audit := range batch {
		// Extract apiGroup and resource from the audit event's ObjectRef
		if audit.ObjectRef == nil {
			continue
		}
		apiGroup := audit.ObjectRef.APIGroup
		resource := audit.ObjectRef.Resource

		// Look up compiled policies for this resource
		compiledPolicies := r.policyCache.Get(apiGroup, resource)
		if len(compiledPolicies) == 0 {
			continue
		}

		// Convert audit event to map for CEL evaluation
		auditMap, err := auditToMap(audit)
		if err != nil {
			klog.V(2).InfoS("Failed to convert audit event to map",
				"auditID", audit.AuditID,
				"error", err,
			)
			continue
		}

		// Try each policy (first match wins)
		for _, policy := range compiledPolicies {
			activity, _, err := activityprocessor.EvaluateCompiledAuditRulesWithEnrichment(policy, auditMap, audit, r.kindResolver, enrichment)
			if err != nil {
				klog.ErrorS(err, "Failed to evaluate compiled audit rules",
					"policy", policy.Name,
					"auditID", audit.AuditID,
				)
				continue
			}

			if activity != nil {
				if activity.Labels == nil {
					activity.Labels = make(map[string]string)
				}
				activity.Labels["activity.miloapis.com/policy-name"] = policy.Name
				activities = append(activities, activity)
				break // First matching policy wins
			}
		}
	}

	klog.V(3).InfoS("Evaluated audit batch",
		"inputEvents", len(batch),
		"activitiesGenerated", len(activities),
	)

	return activities, nil
}

const (
	// staffLookupChunk caps the names per ActivityQuery, keeping the filter
	// well under CEL's expression size limit.
	staffLookupChunk = 1000
	// staffLookupWindow is the widest time range per ActivityQuery (the
	// ActivityQuery maximum).
	staffLookupWindow = 30 * 24 * time.Hour
)

// storedStaffActivities returns the names of the activities this batch could
// replace that are already stored with a staff actor. The names are grouped
// into time windows of at most 30 days and chunks of at most 1,000, queried
// sequentially; any failure fails the batch. Events without a timestamp are
// skipped: their activities were stamped at processing time, which is not
// known here.
func (r *Reindexer) storedStaffActivities(ctx context.Context, batch []*auditv1.Event) (map[string]bool, error) {
	type candidate struct {
		name string
		ts   time.Time
	}
	var candidates []candidate
	for _, audit := range batch {
		if audit.ObjectRef == nil || audit.RequestReceivedTimestamp.IsZero() {
			continue
		}
		for _, policy := range r.policyCache.Get(audit.ObjectRef.APIGroup, audit.ObjectRef.Resource) {
			candidates = append(candidates, candidate{
				name: processor.AuditActivityName(string(audit.AuditID), policy.APIGroup, policy.Kind),
				ts:   audit.RequestReceivedTimestamp.Time,
			})
		}
	}
	if len(candidates) == 0 {
		return nil, nil
	}
	sort.Slice(candidates, func(i, j int) bool { return candidates[i].ts.Before(candidates[j].ts) })

	lookup := r.staffActivities
	if lookup == nil {
		lookup = r.queryStaffActivities
	}
	keep := map[string]bool{}
	for i := 0; i < len(candidates); {
		// One group: names within staffLookupWindow (minus padding) of the
		// first, at most staffLookupChunk of them.
		windowStart := candidates[i].ts
		j := i
		for j < len(candidates) && j-i < staffLookupChunk &&
			candidates[j].ts.Sub(windowStart) <= staffLookupWindow-2*time.Minute {
			j++
		}
		names := make([]string, 0, j-i)
		for _, c := range candidates[i:j] {
			names = append(names, c.name)
		}
		found, err := lookup(ctx, names, windowStart.Add(-time.Minute), candidates[j-1].ts.Add(time.Minute))
		if err != nil {
			return nil, fmt.Errorf("failed to look up existing staff activities: %w", err)
		}
		for n := range found {
			keep[n] = true
		}
		i = j
	}
	return keep, nil
}

// queryStaffActivities finds which named activities are stored with a staff
// actor, via ActivityQuery (one ClickHouse query over actor_type and
// activity_name per page).
func (r *Reindexer) queryStaffActivities(ctx context.Context, names []string, start, end time.Time) (map[string]bool, error) {
	filter := staffActivityFilter(names)

	keep := map[string]bool{}
	cursor := ""
	for {
		query := &v1alpha1.ActivityQuery{
			ObjectMeta: metav1.ObjectMeta{GenerateName: "reindex-staff-"},
			Spec: v1alpha1.ActivityQuerySpec{
				StartTime: start.UTC().Format(time.RFC3339),
				EndTime:   end.UTC().Format(time.RFC3339),
				Filter:    filter,
				Limit:     500,
				Continue:  cursor,
			},
		}
		if err := r.client.Create(ctx, query); err != nil {
			return nil, fmt.Errorf("failed to create ActivityQuery: %w", err)
		}
		for _, a := range query.Status.Results {
			keep[a.Name] = true
		}
		if query.Status.Continue == "" {
			return keep, nil
		}
		cursor = query.Status.Continue
	}
}

// staffActivityFilter is the ActivityQuery filter selecting the named
// activities that have a staff actor.
func staffActivityFilter(names []string) string {
	quoted := make([]string, len(names))
	for i, n := range names {
		quoted[i] = strconv.Quote(n)
	}
	return fmt.Sprintf(`spec.actor.type == %q && metadata.name in [%s]`, processor.ActorTypeStaff, strings.Join(quoted, ", "))
}

// evaluateEventBatch evaluates a batch of Kubernetes events using pre-compiled policies.
func (r *Reindexer) evaluateEventBatch(ctx context.Context, batch []map[string]interface{}) ([]*v1alpha1.Activity, error) {
	var activities []*v1alpha1.Activity

	for _, eventMap := range batch {
		// Extract apiGroup and kind from the event's involved/regarding object,
		// using the shared fallback chain: "regarding" (events.k8s.io/v1) ->
		// "involvedObject" (core/v1). This must match the resolution used by
		// processor.ActivityBuilder.BuildFromEvent so policy matching and
		// activity building agree on the same involved object.
		regarding := processor.ResolveInvolvedObject(eventMap)
		if regarding == nil {
			continue
		}

		apiVersion, _ := regarding["apiVersion"].(string)
		kind, _ := regarding["kind"].(string)

		// Parse apiGroup from apiVersion (e.g., "networking.datumapis.com/v1alpha1" -> "networking.datumapis.com")
		// Core-group resources have apiVersion "v1" with no slash, so apiGroup is "".
		apiGroup := ""
		if idx := strings.Index(apiVersion, "/"); idx != -1 {
			apiGroup = apiVersion[:idx]
		}

		if kind == "" {
			continue
		}

		// Use PolicyCache.MatchEvent which handles lookup + evaluation
		matched, err := r.policyCache.MatchEvent(apiGroup, kind, eventMap)
		if err != nil {
			klog.ErrorS(err, "Failed to evaluate event rules",
				"eventUID", processor.GetNestedString(eventMap, "metadata", "uid"),
			)
			continue
		}

		if matched == nil {
			continue
		}

		// Build the Activity from the matched result
		builder := &processor.ActivityBuilder{
			APIGroup:      matched.APIGroup,
			Kind:          matched.Kind,
			UserResolver:  r.Enrichment.Users,
			StaffResolver: r.Enrichment.Staff,
		}
		activity, err := builder.BuildFromEvent(eventMap, matched.Summary, matched.Links, r.kindResolver)
		if err != nil {
			klog.ErrorS(err, "Failed to build activity from event match",
				"policy", matched.PolicyName,
				"eventUID", processor.GetNestedString(eventMap, "metadata", "uid"),
			)
			continue
		}

		if activity != nil {
			if activity.Labels == nil {
				activity.Labels = make(map[string]string)
			}
			activity.Labels["activity.miloapis.com/policy-name"] = matched.PolicyName
			activities = append(activities, activity)
		}
	}

	klog.V(3).InfoS("Evaluated event batch",
		"inputEvents", len(batch),
		"activitiesGenerated", len(activities),
	)

	return activities, nil
}

// eventRecordToMap converts an EventRecord to a map[string]interface{} for processor compatibility.
func eventRecordToMap(record *v1alpha1.EventRecord) (map[string]interface{}, error) {
	// The processor expects the event data in a structured map format.
	// We need to convert the eventsv1.Event to map[string]interface{}.

	// Create a map with the event fields that the processor expects
	eventMap := map[string]interface{}{
		"metadata": map[string]interface{}{
			"name":      record.Event.Name,
			"namespace": record.Event.Namespace,
			"uid":       string(record.Event.UID),
		},
		"regarding": map[string]interface{}{
			"apiVersion": record.Event.Regarding.APIVersion,
			"kind":       record.Event.Regarding.Kind,
			"name":       record.Event.Regarding.Name,
			"namespace":  record.Event.Regarding.Namespace,
			"uid":        string(record.Event.Regarding.UID),
		},
		"reason":              record.Event.Reason,
		"note":                record.Event.Note,
		"type":                record.Event.Type,
		"eventTime":           record.Event.EventTime.Time,
		"reportingController": record.Event.ReportingController,
		"reportingInstance":   record.Event.ReportingInstance,
		"action":              record.Event.Action,
	}

	// Add series if present
	if record.Event.Series != nil {
		eventMap["series"] = map[string]interface{}{
			"count":            record.Event.Series.Count,
			"lastObservedTime": record.Event.Series.LastObservedTime.Time,
		}
	}

	// Add related object if present
	if record.Event.Related != nil {
		eventMap["related"] = map[string]interface{}{
			"apiVersion": record.Event.Related.APIVersion,
			"kind":       record.Event.Related.Kind,
			"name":       record.Event.Related.Name,
			"namespace":  record.Event.Related.Namespace,
			"uid":        string(record.Event.Related.UID),
		}
	}

	return eventMap, nil
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
