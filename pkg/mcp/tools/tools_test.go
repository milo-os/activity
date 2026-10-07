package tools

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	authnv1 "k8s.io/api/authentication/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/watch"
	auditv1 "k8s.io/apiserver/pkg/apis/audit/v1"
	"k8s.io/client-go/rest"

	"go.miloapis.com/activity/pkg/apis/activity/v1alpha1"
	activityclient "go.miloapis.com/activity/pkg/client/clientset/versioned/typed/activity/v1alpha1"
)

// =============================================================================
// Mock Client Implementation
// =============================================================================

type mockActivityV1alpha1Client struct {
	auditLogQueries       *mockAuditLogQueryInterface
	auditLogFacetsQueries *mockAuditLogFacetsQueryInterface
	activityQueries       *mockActivityQueryInterface
	activityFacetQueries  *mockActivityFacetQueryInterface
	activityPolicies      *mockActivityPolicyInterface
	policyPreviews        *mockPolicyPreviewInterface
	activities            *mockActivityInterface
	eventFacetQueries     *mockEventFacetQueryInterface
	eventQueries          *mockEventQueryInterface
	reindexJobs           *mockReindexJobInterface
}

func newMockClient() *mockActivityV1alpha1Client {
	return &mockActivityV1alpha1Client{
		auditLogQueries:       &mockAuditLogQueryInterface{},
		auditLogFacetsQueries: &mockAuditLogFacetsQueryInterface{},
		activityQueries:       &mockActivityQueryInterface{},
		activityFacetQueries:  &mockActivityFacetQueryInterface{},
		activityPolicies:      &mockActivityPolicyInterface{},
		policyPreviews:        &mockPolicyPreviewInterface{},
		activities:            &mockActivityInterface{},
		eventFacetQueries:     &mockEventFacetQueryInterface{},
		eventQueries:          &mockEventQueryInterface{},
		reindexJobs:           &mockReindexJobInterface{},
	}
}

func (m *mockActivityV1alpha1Client) AuditLogQueries() activityclient.AuditLogQueryInterface {
	return m.auditLogQueries
}

func (m *mockActivityV1alpha1Client) AuditLogFacetsQueries() activityclient.AuditLogFacetsQueryInterface {
	return m.auditLogFacetsQueries
}

func (m *mockActivityV1alpha1Client) ActivityQueries() activityclient.ActivityQueryInterface {
	return m.activityQueries
}

func (m *mockActivityV1alpha1Client) ActivityFacetQueries() activityclient.ActivityFacetQueryInterface {
	return m.activityFacetQueries
}

func (m *mockActivityV1alpha1Client) ActivityPolicies() activityclient.ActivityPolicyInterface {
	return m.activityPolicies
}

func (m *mockActivityV1alpha1Client) PolicyPreviews() activityclient.PolicyPreviewInterface {
	return m.policyPreviews
}

func (m *mockActivityV1alpha1Client) Activities(namespace string) activityclient.ActivityInterface {
	return m.activities
}

func (m *mockActivityV1alpha1Client) EventFacetQueries() activityclient.EventFacetQueryInterface {
	return m.eventFacetQueries
}

func (m *mockActivityV1alpha1Client) EventQueries() activityclient.EventQueryInterface {
	return m.eventQueries
}

func (m *mockActivityV1alpha1Client) ReindexJobs() activityclient.ReindexJobInterface {
	return m.reindexJobs
}

func (m *mockActivityV1alpha1Client) RESTClient() rest.Interface {
	return nil
}

// =============================================================================
// Mock AuditLogQuery Interface
// =============================================================================

type mockAuditLogQueryInterface struct {
	createFunc func(ctx context.Context, query *v1alpha1.AuditLogQuery, opts metav1.CreateOptions) (*v1alpha1.AuditLogQuery, error)
}

func (m *mockAuditLogQueryInterface) Create(ctx context.Context, query *v1alpha1.AuditLogQuery, opts metav1.CreateOptions) (*v1alpha1.AuditLogQuery, error) {
	if m.createFunc != nil {
		return m.createFunc(ctx, query, opts)
	}
	// Default response
	now := metav1.NewMicroTime(time.Now())
	return &v1alpha1.AuditLogQuery{
		ObjectMeta: metav1.ObjectMeta{Name: "test-query"},
		Spec:       query.Spec,
		Status: v1alpha1.AuditLogQueryStatus{
			Results: []auditv1.Event{
				{
					TypeMeta:                 metav1.TypeMeta{Kind: "Event", APIVersion: "audit.k8s.io/v1"},
					Level:                    auditv1.LevelRequestResponse,
					AuditID:                  "test-audit-id",
					Stage:                    auditv1.StageResponseComplete,
					RequestURI:               "/api/v1/namespaces/default/pods",
					Verb:                     "create",
					User:                     authnv1.UserInfo{Username: "alice@example.com", UID: "user-123"},
					ObjectRef:                &auditv1.ObjectReference{Resource: "pods", Namespace: "default", Name: "my-pod", APIGroup: "", APIVersion: "v1"},
					ResponseStatus:           &metav1.Status{Code: 201},
					RequestReceivedTimestamp: now,
					StageTimestamp:           now,
				},
			},
			Continue:           "",
			EffectiveStartTime: "2024-01-01T00:00:00Z",
			EffectiveEndTime:   "2024-01-07T00:00:00Z",
		},
	}, nil
}

// =============================================================================
// Mock AuditLogFacetsQuery Interface
// =============================================================================

type mockAuditLogFacetsQueryInterface struct {
	createFunc func(ctx context.Context, query *v1alpha1.AuditLogFacetsQuery, opts metav1.CreateOptions) (*v1alpha1.AuditLogFacetsQuery, error)
}

func (m *mockAuditLogFacetsQueryInterface) Create(ctx context.Context, query *v1alpha1.AuditLogFacetsQuery, opts metav1.CreateOptions) (*v1alpha1.AuditLogFacetsQuery, error) {
	if m.createFunc != nil {
		return m.createFunc(ctx, query, opts)
	}
	// Default response
	facets := make([]v1alpha1.FacetResult, 0, len(query.Spec.Facets))
	for _, spec := range query.Spec.Facets {
		facets = append(facets, v1alpha1.FacetResult{
			Field: spec.Field,
			Values: []v1alpha1.FacetValue{
				{Value: "test-value-1", Count: 100},
				{Value: "test-value-2", Count: 50},
			},
		})
	}
	return &v1alpha1.AuditLogFacetsQuery{
		ObjectMeta: metav1.ObjectMeta{Name: "test-facets"},
		Spec:       query.Spec,
		Status:     v1alpha1.AuditLogFacetsQueryStatus{Facets: facets},
	}, nil
}

// =============================================================================
// Mock ActivityQuery Interface
// =============================================================================

type mockActivityQueryInterface struct {
	createFunc func(ctx context.Context, query *v1alpha1.ActivityQuery, opts metav1.CreateOptions) (*v1alpha1.ActivityQuery, error)
}

func (m *mockActivityQueryInterface) Create(ctx context.Context, query *v1alpha1.ActivityQuery, opts metav1.CreateOptions) (*v1alpha1.ActivityQuery, error) {
	if m.createFunc != nil {
		return m.createFunc(ctx, query, opts)
	}
	// Default response
	return &v1alpha1.ActivityQuery{
		ObjectMeta: metav1.ObjectMeta{Name: "test-activity-query"},
		Spec:       query.Spec,
		Status: v1alpha1.ActivityQueryStatus{
			Results: []v1alpha1.Activity{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name:              "activity-1",
						CreationTimestamp: metav1.NewTime(time.Now()),
					},
					Spec: v1alpha1.ActivitySpec{
						Summary:      "alice created HTTP proxy api-gateway",
						ChangeSource: "human",
						Actor:        v1alpha1.ActivityActor{Type: "user", Name: "alice@example.com"},
						Resource:     v1alpha1.ActivityResource{APIGroup: "networking.datumapis.com", APIVersion: "v1", Kind: "HTTPProxy", Name: "api-gateway", Namespace: "default"},
						Tenant:       v1alpha1.ActivityTenant{Type: "organization", Name: "acme"},
						Origin:       v1alpha1.ActivityOrigin{Type: "audit", ID: "audit-123"},
					},
				},
			},
			Continue:           "",
			EffectiveStartTime: "2024-01-01T00:00:00Z",
			EffectiveEndTime:   "2024-01-07T00:00:00Z",
		},
	}, nil
}

// =============================================================================
// Mock ActivityFacetQuery Interface
// =============================================================================

type mockActivityFacetQueryInterface struct {
	createFunc func(ctx context.Context, query *v1alpha1.ActivityFacetQuery, opts metav1.CreateOptions) (*v1alpha1.ActivityFacetQuery, error)
}

func (m *mockActivityFacetQueryInterface) Create(ctx context.Context, query *v1alpha1.ActivityFacetQuery, opts metav1.CreateOptions) (*v1alpha1.ActivityFacetQuery, error) {
	if m.createFunc != nil {
		return m.createFunc(ctx, query, opts)
	}
	// Default response
	facets := make([]v1alpha1.FacetResult, 0, len(query.Spec.Facets))
	for _, spec := range query.Spec.Facets {
		facets = append(facets, v1alpha1.FacetResult{
			Field: spec.Field,
			Values: []v1alpha1.FacetValue{
				{Value: "alice@example.com", Count: 42},
				{Value: "bob@example.com", Count: 28},
			},
		})
	}
	return &v1alpha1.ActivityFacetQuery{
		ObjectMeta: metav1.ObjectMeta{Name: "test-activity-facets"},
		Spec:       query.Spec,
		Status:     v1alpha1.ActivityFacetQueryStatus{Facets: facets},
	}, nil
}

// =============================================================================
// Mock ActivityPolicy Interface
// =============================================================================

type mockActivityPolicyInterface struct {
	listFunc func(ctx context.Context, opts metav1.ListOptions) (*v1alpha1.ActivityPolicyList, error)
}

func (m *mockActivityPolicyInterface) Create(ctx context.Context, policy *v1alpha1.ActivityPolicy, opts metav1.CreateOptions) (*v1alpha1.ActivityPolicy, error) {
	return policy, nil
}

func (m *mockActivityPolicyInterface) Update(ctx context.Context, policy *v1alpha1.ActivityPolicy, opts metav1.UpdateOptions) (*v1alpha1.ActivityPolicy, error) {
	return policy, nil
}

func (m *mockActivityPolicyInterface) UpdateStatus(ctx context.Context, policy *v1alpha1.ActivityPolicy, opts metav1.UpdateOptions) (*v1alpha1.ActivityPolicy, error) {
	return policy, nil
}

func (m *mockActivityPolicyInterface) Delete(ctx context.Context, name string, opts metav1.DeleteOptions) error {
	return nil
}

func (m *mockActivityPolicyInterface) DeleteCollection(ctx context.Context, opts metav1.DeleteOptions, listOpts metav1.ListOptions) error {
	return nil
}

func (m *mockActivityPolicyInterface) Get(ctx context.Context, name string, opts metav1.GetOptions) (*v1alpha1.ActivityPolicy, error) {
	return &v1alpha1.ActivityPolicy{ObjectMeta: metav1.ObjectMeta{Name: name}}, nil
}

func (m *mockActivityPolicyInterface) List(ctx context.Context, opts metav1.ListOptions) (*v1alpha1.ActivityPolicyList, error) {
	if m.listFunc != nil {
		return m.listFunc(ctx, opts)
	}
	return &v1alpha1.ActivityPolicyList{
		Items: []v1alpha1.ActivityPolicy{
			{
				ObjectMeta: metav1.ObjectMeta{Name: "networking-httpproxy"},
				Spec: v1alpha1.ActivityPolicySpec{
					Resource: v1alpha1.ActivityPolicyResource{APIGroup: "networking.datumapis.com", Kind: "HTTPProxy"},
					AuditRules: []v1alpha1.ActivityPolicyRule{
						{Match: "audit.verb == 'create'", Summary: "{{ actor }} created HTTPProxy"},
					},
				},
				Status: v1alpha1.ActivityPolicyStatus{
					Conditions: []metav1.Condition{
						{Type: "Ready", Status: metav1.ConditionTrue},
					},
				},
			},
		},
	}, nil
}

func (m *mockActivityPolicyInterface) Watch(ctx context.Context, opts metav1.ListOptions) (watch.Interface, error) {
	return nil, nil
}

func (m *mockActivityPolicyInterface) Patch(ctx context.Context, name string, pt types.PatchType, data []byte, opts metav1.PatchOptions, subresources ...string) (*v1alpha1.ActivityPolicy, error) {
	return &v1alpha1.ActivityPolicy{ObjectMeta: metav1.ObjectMeta{Name: name}}, nil
}

// =============================================================================
// Mock PolicyPreview Interface
// =============================================================================

type mockPolicyPreviewInterface struct {
	createFunc func(ctx context.Context, preview *v1alpha1.PolicyPreview, opts metav1.CreateOptions) (*v1alpha1.PolicyPreview, error)
}

func (m *mockPolicyPreviewInterface) Create(ctx context.Context, preview *v1alpha1.PolicyPreview, opts metav1.CreateOptions) (*v1alpha1.PolicyPreview, error) {
	if m.createFunc != nil {
		return m.createFunc(ctx, preview, opts)
	}
	return &v1alpha1.PolicyPreview{
		ObjectMeta: metav1.ObjectMeta{Name: "test-preview"},
		Spec:       preview.Spec,
		Status: v1alpha1.PolicyPreviewStatus{
			Results: []v1alpha1.PolicyPreviewInputResult{
				{InputIndex: 0, Matched: true, MatchedRuleIndex: 0, MatchedRuleType: "audit"},
			},
			Activities: []v1alpha1.Activity{
				{
					Spec: v1alpha1.ActivitySpec{
						Summary: "alice created HTTPProxy",
						Actor:   v1alpha1.ActivityActor{Type: "user", Name: "alice@example.com"},
						Resource: v1alpha1.ActivityResource{
							Kind: "HTTPProxy",
							Name: "my-proxy",
						},
					},
				},
			},
		},
	}, nil
}

// =============================================================================
// Mock Activity Interface (for namespaced activities)
// =============================================================================

type mockActivityInterface struct{}

func (m *mockActivityInterface) Create(ctx context.Context, activity *v1alpha1.Activity, opts metav1.CreateOptions) (*v1alpha1.Activity, error) {
	return activity, nil
}

func (m *mockActivityInterface) Update(ctx context.Context, activity *v1alpha1.Activity, opts metav1.UpdateOptions) (*v1alpha1.Activity, error) {
	return activity, nil
}

func (m *mockActivityInterface) Delete(ctx context.Context, name string, opts metav1.DeleteOptions) error {
	return nil
}

func (m *mockActivityInterface) DeleteCollection(ctx context.Context, opts metav1.DeleteOptions, listOpts metav1.ListOptions) error {
	return nil
}

func (m *mockActivityInterface) Get(ctx context.Context, name string, opts metav1.GetOptions) (*v1alpha1.Activity, error) {
	return &v1alpha1.Activity{ObjectMeta: metav1.ObjectMeta{Name: name}}, nil
}

func (m *mockActivityInterface) List(ctx context.Context, opts metav1.ListOptions) (*v1alpha1.ActivityList, error) {
	return &v1alpha1.ActivityList{}, nil
}

func (m *mockActivityInterface) Watch(ctx context.Context, opts metav1.ListOptions) (watch.Interface, error) {
	return nil, nil
}

func (m *mockActivityInterface) Patch(ctx context.Context, name string, pt types.PatchType, data []byte, opts metav1.PatchOptions, subresources ...string) (*v1alpha1.Activity, error) {
	return &v1alpha1.Activity{ObjectMeta: metav1.ObjectMeta{Name: name}}, nil
}

// =============================================================================
// Mock EventFacetQuery Interface
// =============================================================================

type mockEventFacetQueryInterface struct{}

func (m *mockEventFacetQueryInterface) Create(ctx context.Context, query *v1alpha1.EventFacetQuery, opts metav1.CreateOptions) (*v1alpha1.EventFacetQuery, error) {
	return query, nil
}

func (m *mockEventFacetQueryInterface) Update(ctx context.Context, query *v1alpha1.EventFacetQuery, opts metav1.UpdateOptions) (*v1alpha1.EventFacetQuery, error) {
	return query, nil
}

func (m *mockEventFacetQueryInterface) Delete(ctx context.Context, name string, opts metav1.DeleteOptions) error {
	return nil
}

func (m *mockEventFacetQueryInterface) DeleteCollection(ctx context.Context, opts metav1.DeleteOptions, listOpts metav1.ListOptions) error {
	return nil
}

func (m *mockEventFacetQueryInterface) Get(ctx context.Context, name string, opts metav1.GetOptions) (*v1alpha1.EventFacetQuery, error) {
	return &v1alpha1.EventFacetQuery{ObjectMeta: metav1.ObjectMeta{Name: name}}, nil
}

func (m *mockEventFacetQueryInterface) Watch(ctx context.Context, opts metav1.ListOptions) (watch.Interface, error) {
	return nil, nil
}

func (m *mockEventFacetQueryInterface) Patch(ctx context.Context, name string, pt types.PatchType, data []byte, opts metav1.PatchOptions, subresources ...string) (*v1alpha1.EventFacetQuery, error) {
	return &v1alpha1.EventFacetQuery{ObjectMeta: metav1.ObjectMeta{Name: name}}, nil
}

// =============================================================================
// Mock EventQuery Interface
// =============================================================================

type mockEventQueryInterface struct {
	createFunc func(ctx context.Context, query *v1alpha1.EventQuery, opts metav1.CreateOptions) (*v1alpha1.EventQuery, error)
}

func (m *mockEventQueryInterface) Create(ctx context.Context, query *v1alpha1.EventQuery, opts metav1.CreateOptions) (*v1alpha1.EventQuery, error) {
	if m.createFunc != nil {
		return m.createFunc(ctx, query, opts)
	}
	return query, nil
}

type mockReindexJobInterface struct{}

func (m *mockReindexJobInterface) Create(ctx context.Context, job *v1alpha1.ReindexJob, opts metav1.CreateOptions) (*v1alpha1.ReindexJob, error) {
	return job, nil
}

func (m *mockReindexJobInterface) Update(ctx context.Context, job *v1alpha1.ReindexJob, opts metav1.UpdateOptions) (*v1alpha1.ReindexJob, error) {
	return job, nil
}

func (m *mockReindexJobInterface) UpdateStatus(ctx context.Context, job *v1alpha1.ReindexJob, opts metav1.UpdateOptions) (*v1alpha1.ReindexJob, error) {
	return job, nil
}

func (m *mockReindexJobInterface) Delete(ctx context.Context, name string, opts metav1.DeleteOptions) error {
	return nil
}

func (m *mockReindexJobInterface) DeleteCollection(ctx context.Context, opts metav1.DeleteOptions, listOpts metav1.ListOptions) error {
	return nil
}

func (m *mockReindexJobInterface) Get(ctx context.Context, name string, opts metav1.GetOptions) (*v1alpha1.ReindexJob, error) {
	return &v1alpha1.ReindexJob{}, nil
}

func (m *mockReindexJobInterface) List(ctx context.Context, opts metav1.ListOptions) (*v1alpha1.ReindexJobList, error) {
	return &v1alpha1.ReindexJobList{}, nil
}

func (m *mockReindexJobInterface) Watch(ctx context.Context, opts metav1.ListOptions) (watch.Interface, error) {
	return nil, nil
}

func (m *mockReindexJobInterface) Patch(ctx context.Context, name string, pt types.PatchType, data []byte, opts metav1.PatchOptions, subresources ...string) (*v1alpha1.ReindexJob, error) {
	return &v1alpha1.ReindexJob{}, nil
}

func (m *mockEventQueryInterface) Update(ctx context.Context, query *v1alpha1.EventQuery, opts metav1.UpdateOptions) (*v1alpha1.EventQuery, error) {
	return query, nil
}

func (m *mockEventQueryInterface) Delete(ctx context.Context, name string, opts metav1.DeleteOptions) error {
	return nil
}

func (m *mockEventQueryInterface) DeleteCollection(ctx context.Context, opts metav1.DeleteOptions, listOpts metav1.ListOptions) error {
	return nil
}

func (m *mockEventQueryInterface) Get(ctx context.Context, name string, opts metav1.GetOptions) (*v1alpha1.EventQuery, error) {
	return &v1alpha1.EventQuery{ObjectMeta: metav1.ObjectMeta{Name: name}}, nil
}

func (m *mockEventQueryInterface) List(ctx context.Context, opts metav1.ListOptions) (*v1alpha1.EventQueryList, error) {
	return &v1alpha1.EventQueryList{}, nil
}

func (m *mockEventQueryInterface) Watch(ctx context.Context, opts metav1.ListOptions) (watch.Interface, error) {
	return nil, nil
}

func (m *mockEventQueryInterface) Patch(ctx context.Context, name string, pt types.PatchType, data []byte, opts metav1.PatchOptions, subresources ...string) (*v1alpha1.EventQuery, error) {
	return &v1alpha1.EventQuery{ObjectMeta: metav1.ObjectMeta{Name: name}}, nil
}

// =============================================================================
// Test Helper Functions
// =============================================================================

func createTestProvider(client *mockActivityV1alpha1Client) *ToolProvider {
	return NewToolProviderWithClient(client, "default")
}

func parseJSONResult(t *testing.T, result *mcp.CallToolResult) map[string]any {
	t.Helper()
	if result.IsError {
		t.Fatalf("Expected success but got error: %v", result.Content)
	}
	if len(result.Content) == 0 {
		t.Fatal("Expected content but got none")
	}
	textContent, ok := result.Content[0].(*mcp.TextContent)
	if !ok {
		t.Fatalf("Expected TextContent but got %T", result.Content[0])
	}

	var output map[string]any
	if err := json.Unmarshal([]byte(textContent.Text), &output); err != nil {
		t.Fatalf("Failed to parse JSON output: %v\nContent: %s", err, textContent.Text)
	}
	return output
}

// =============================================================================
// Tests
// =============================================================================

func TestQueryAuditLogs(t *testing.T) {
	client := newMockClient()
	provider := createTestProvider(client)

	args := QueryAuditLogsArgs{
		StartTime: "now-7d",
		EndTime:   "now",
		Filter:    "verb == 'create'",
		Limit:     100,
	}

	result, _, err := provider.handleQueryAuditLogs(context.Background(), nil, args)
	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}

	output := parseJSONResult(t, result)

	if output["count"].(float64) != 1 {
		t.Errorf("Expected count=1, got %v", output["count"])
	}
	if output["effectiveStartTime"] != "2024-01-01T00:00:00Z" {
		t.Errorf("Expected effectiveStartTime, got %v", output["effectiveStartTime"])
	}

	events := output["events"].([]any)
	if len(events) != 1 {
		t.Errorf("Expected 1 event, got %d", len(events))
	}

	t.Log("✓ query_audit_logs works correctly")
}

func TestGetAuditLogFacets(t *testing.T) {
	client := newMockClient()
	provider := createTestProvider(client)

	args := GetAuditLogFacetsArgs{
		Fields:    []string{"verb", "user.username"},
		StartTime: "now-7d",
		EndTime:   "now",
		Limit:     20,
	}

	result, _, err := provider.handleGetAuditLogFacets(context.Background(), nil, args)
	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}

	output := parseJSONResult(t, result)

	verbFacets := output["verb"].([]any)
	if len(verbFacets) != 2 {
		t.Errorf("Expected 2 verb facet values, got %d", len(verbFacets))
	}

	userFacets := output["user.username"].([]any)
	if len(userFacets) != 2 {
		t.Errorf("Expected 2 user facet values, got %d", len(userFacets))
	}

	t.Log("✓ get_audit_log_facets works correctly")
}

func TestQueryActivities(t *testing.T) {
	client := newMockClient()
	provider := createTestProvider(client)

	args := QueryActivitiesArgs{
		StartTime:    "now-7d",
		EndTime:      "now",
		ChangeSource: "human",
		Limit:        100,
	}

	result, _, err := provider.handleQueryActivities(context.Background(), nil, args)
	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}

	output := parseJSONResult(t, result)

	if output["count"].(float64) != 1 {
		t.Errorf("Expected count=1, got %v", output["count"])
	}

	activities := output["activities"].([]any)
	if len(activities) != 1 {
		t.Errorf("Expected 1 activity, got %d", len(activities))
	}

	activity := activities[0].(map[string]any)
	if activity["summary"] != "alice created HTTP proxy api-gateway" {
		t.Errorf("Expected summary, got %v", activity["summary"])
	}

	t.Log("✓ query_activities works correctly")
}

func TestGetActivityFacets(t *testing.T) {
	client := newMockClient()
	provider := createTestProvider(client)

	args := GetActivityFacetsArgs{
		Fields:    []string{"spec.actor.name", "spec.resource.kind"},
		StartTime: "now-7d",
		EndTime:   "now",
		Limit:     20,
	}

	result, _, err := provider.handleGetActivityFacets(context.Background(), nil, args)
	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}

	output := parseJSONResult(t, result)

	actorFacets := output["spec.actor.name"].([]any)
	if len(actorFacets) != 2 {
		t.Errorf("Expected 2 actor facet values, got %d", len(actorFacets))
	}

	t.Log("✓ get_activity_facets works correctly")
}

func TestFindFailedOperations(t *testing.T) {
	client := newMockClient()

	// Setup mock to return a failed operation
	client.auditLogQueries.createFunc = func(ctx context.Context, query *v1alpha1.AuditLogQuery, opts metav1.CreateOptions) (*v1alpha1.AuditLogQuery, error) {
		now := metav1.NewMicroTime(time.Now())
		return &v1alpha1.AuditLogQuery{
			ObjectMeta: metav1.ObjectMeta{Name: "test-failed-ops"},
			Status: v1alpha1.AuditLogQueryStatus{
				Results: []auditv1.Event{
					{
						Verb:                     "create",
						User:                     authnv1.UserInfo{Username: "alice@example.com"},
						ObjectRef:                &auditv1.ObjectReference{Resource: "pods", Name: "bad-pod", Namespace: "default"},
						ResponseStatus:           &metav1.Status{Code: 403, Message: "Forbidden"},
						RequestReceivedTimestamp: now,
					},
				},
				EffectiveStartTime: "2024-01-01T00:00:00Z",
				EffectiveEndTime:   "2024-01-07T00:00:00Z",
			},
		}, nil
	}

	provider := createTestProvider(client)

	args := FindFailedOperationsArgs{
		StartTime: "now-7d",
		Limit:     100,
	}

	result, _, err := provider.handleFindFailedOperations(context.Background(), nil, args)
	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}

	output := parseJSONResult(t, result)

	if output["count"].(float64) != 1 {
		t.Errorf("Expected count=1, got %v", output["count"])
	}

	byStatusCode := output["byStatusCode"].(map[string]any)
	if byStatusCode["403"].(float64) != 1 {
		t.Errorf("Expected 403 count=1, got %v", byStatusCode["403"])
	}

	t.Log("✓ find_failed_operations works correctly")
}

func TestGetResourceHistory(t *testing.T) {
	client := newMockClient()

	// Setup mock to return activities for the resource
	client.activityQueries.createFunc = func(ctx context.Context, query *v1alpha1.ActivityQuery, opts metav1.CreateOptions) (*v1alpha1.ActivityQuery, error) {
		now := metav1.NewTime(time.Now())
		return &v1alpha1.ActivityQuery{
			ObjectMeta: metav1.ObjectMeta{Name: "test-history"},
			Status: v1alpha1.ActivityQueryStatus{
				Results: []v1alpha1.Activity{
					{
						ObjectMeta: metav1.ObjectMeta{Name: "activity-1", CreationTimestamp: now},
						Spec: v1alpha1.ActivitySpec{
							Summary:      "alice created Deployment my-app",
							ChangeSource: "human",
							Actor:        v1alpha1.ActivityActor{Type: "user", Name: "alice@example.com"},
							Resource:     v1alpha1.ActivityResource{APIGroup: "apps", APIVersion: "v1", Kind: "Deployment", Name: "my-app", Namespace: "default"},
							Tenant:       v1alpha1.ActivityTenant{Type: "organization", Name: "acme"},
							Origin:       v1alpha1.ActivityOrigin{Type: "audit", ID: "audit-1"},
						},
					},
					{
						ObjectMeta: metav1.ObjectMeta{Name: "activity-2", CreationTimestamp: now},
						Spec: v1alpha1.ActivitySpec{
							Summary:      "bob updated Deployment my-app",
							ChangeSource: "human",
							Actor:        v1alpha1.ActivityActor{Type: "user", Name: "bob@example.com"},
							Resource:     v1alpha1.ActivityResource{APIGroup: "apps", APIVersion: "v1", Kind: "Deployment", Name: "my-app", Namespace: "default"},
							Tenant:       v1alpha1.ActivityTenant{Type: "organization", Name: "acme"},
							Origin:       v1alpha1.ActivityOrigin{Type: "audit", ID: "audit-2"},
						},
					},
				},
				EffectiveStartTime: "2024-01-01T00:00:00Z",
				EffectiveEndTime:   "2024-01-07T00:00:00Z",
			},
		}, nil
	}

	provider := createTestProvider(client)

	args := GetResourceHistoryArgs{
		Name:      "my-app",
		Kind:      "Deployment",
		Namespace: "default",
	}

	result, _, err := provider.handleGetResourceHistory(context.Background(), nil, args)
	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}

	output := parseJSONResult(t, result)

	if output["count"].(float64) != 2 {
		t.Errorf("Expected count=2, got %v", output["count"])
	}

	history := output["history"].([]any)
	if len(history) != 2 {
		t.Errorf("Expected 2 history entries, got %d", len(history))
	}

	t.Log("✓ get_resource_history works correctly")
}

func TestGetResourceHistoryRequiresName(t *testing.T) {
	client := newMockClient()
	provider := createTestProvider(client)

	args := GetResourceHistoryArgs{
		// No name or resourceUID provided
	}

	result, _, _ := provider.handleGetResourceHistory(context.Background(), nil, args)

	if !result.IsError {
		t.Error("Expected error when neither name nor resourceUID provided")
	}

	t.Log("✓ get_resource_history validates required fields")
}

func TestGetUserActivitySummary(t *testing.T) {
	client := newMockClient()

	// Setup mock with user activities
	client.activityQueries.createFunc = func(ctx context.Context, query *v1alpha1.ActivityQuery, opts metav1.CreateOptions) (*v1alpha1.ActivityQuery, error) {
		now := metav1.NewTime(time.Now())
		return &v1alpha1.ActivityQuery{
			ObjectMeta: metav1.ObjectMeta{Name: "test-user-summary"},
			Status: v1alpha1.ActivityQueryStatus{
				Results: []v1alpha1.Activity{
					{
						ObjectMeta: metav1.ObjectMeta{Name: "activity-1", CreationTimestamp: now},
						Spec: v1alpha1.ActivitySpec{
							Summary:      "alice created Pod pod-1",
							ChangeSource: "human",
							Actor:        v1alpha1.ActivityActor{Type: "user", Name: "alice@example.com"},
							Resource:     v1alpha1.ActivityResource{APIVersion: "v1", Kind: "Pod", Name: "pod-1"},
							Tenant:       v1alpha1.ActivityTenant{Type: "organization", Name: "acme"},
							Origin:       v1alpha1.ActivityOrigin{Type: "audit", ID: "audit-1"},
						},
					},
					{
						ObjectMeta: metav1.ObjectMeta{Name: "activity-2", CreationTimestamp: now},
						Spec: v1alpha1.ActivitySpec{
							Summary:      "alice updated Deployment deploy-1",
							ChangeSource: "human",
							Actor:        v1alpha1.ActivityActor{Type: "user", Name: "alice@example.com"},
							Resource:     v1alpha1.ActivityResource{APIGroup: "apps", APIVersion: "v1", Kind: "Deployment", Name: "deploy-1"},
							Tenant:       v1alpha1.ActivityTenant{Type: "organization", Name: "acme"},
							Origin:       v1alpha1.ActivityOrigin{Type: "audit", ID: "audit-2"},
						},
					},
				},
				EffectiveStartTime: "2024-01-01T00:00:00Z",
				EffectiveEndTime:   "2024-01-07T00:00:00Z",
			},
		}, nil
	}

	provider := createTestProvider(client)

	args := GetUserActivitySummaryArgs{
		Username: "alice@example.com",
	}

	result, _, err := provider.handleGetUserActivitySummary(context.Background(), nil, args)
	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}

	output := parseJSONResult(t, result)

	if output["totalActivities"].(float64) != 2 {
		t.Errorf("Expected totalActivities=2, got %v", output["totalActivities"])
	}

	user := output["user"].(map[string]any)
	if user["username"] != "alice@example.com" {
		t.Errorf("Expected username=alice@example.com, got %v", user["username"])
	}

	t.Log("✓ get_user_activity_summary works correctly")
}

func TestGetUserActivitySummaryRequiresUser(t *testing.T) {
	client := newMockClient()
	provider := createTestProvider(client)

	args := GetUserActivitySummaryArgs{
		// No username or userUID
	}

	result, _, _ := provider.handleGetUserActivitySummary(context.Background(), nil, args)

	if !result.IsError {
		t.Error("Expected error when neither username nor userUID provided")
	}

	t.Log("✓ get_user_activity_summary validates required fields")
}

func TestGetActivityTimeline(t *testing.T) {
	client := newMockClient()
	provider := createTestProvider(client)

	args := GetActivityTimelineArgs{
		StartTime:  "now-7d",
		EndTime:    "now",
		BucketSize: "day",
	}

	result, _, err := provider.handleGetActivityTimeline(context.Background(), nil, args)
	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}

	output := parseJSONResult(t, result)

	if output["bucketSize"] != "day" {
		t.Errorf("Expected bucketSize=day, got %v", output["bucketSize"])
	}

	buckets := output["buckets"].([]any)
	if len(buckets) == 0 {
		t.Error("Expected at least 1 bucket")
	}

	t.Log("✓ get_activity_timeline works correctly")
}

func TestSummarizeRecentActivity(t *testing.T) {
	client := newMockClient()

	// Setup mock with varied activity
	client.activityQueries.createFunc = func(ctx context.Context, query *v1alpha1.ActivityQuery, opts metav1.CreateOptions) (*v1alpha1.ActivityQuery, error) {
		now := metav1.NewTime(time.Now())
		return &v1alpha1.ActivityQuery{
			ObjectMeta: metav1.ObjectMeta{Name: "test-summary"},
			Status: v1alpha1.ActivityQueryStatus{
				Results: []v1alpha1.Activity{
					{
						ObjectMeta: metav1.ObjectMeta{Name: "activity-1", CreationTimestamp: now},
						Spec: v1alpha1.ActivitySpec{
							Summary:      "alice created Pod my-pod",
							ChangeSource: "human",
							Actor:        v1alpha1.ActivityActor{Type: "user", Name: "alice@example.com"},
							Resource:     v1alpha1.ActivityResource{APIVersion: "v1", Kind: "Pod", Name: "my-pod"},
							Tenant:       v1alpha1.ActivityTenant{Type: "organization", Name: "acme"},
							Origin:       v1alpha1.ActivityOrigin{Type: "audit", ID: "audit-1"},
						},
					},
					{
						ObjectMeta: metav1.ObjectMeta{Name: "activity-2", CreationTimestamp: now},
						Spec: v1alpha1.ActivitySpec{
							Summary:      "controller deleted Pod old-pod",
							ChangeSource: "system",
							Actor:        v1alpha1.ActivityActor{Type: "controller", Name: "system:controller"},
							Resource:     v1alpha1.ActivityResource{APIVersion: "v1", Kind: "Pod", Name: "old-pod", Namespace: "default"},
							Tenant:       v1alpha1.ActivityTenant{Type: "organization", Name: "acme"},
							Origin:       v1alpha1.ActivityOrigin{Type: "audit", ID: "audit-2"},
						},
					},
				},
				EffectiveStartTime: "2024-01-01T00:00:00Z",
				EffectiveEndTime:   "2024-01-07T00:00:00Z",
			},
		}, nil
	}

	provider := createTestProvider(client)

	args := SummarizeRecentActivityArgs{
		StartTime: "now-24h",
		TopN:      5,
	}

	result, _, err := provider.handleSummarizeRecentActivity(context.Background(), nil, args)
	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}

	output := parseJSONResult(t, result)

	if output["totalActivities"].(float64) != 2 {
		t.Errorf("Expected totalActivities=2, got %v", output["totalActivities"])
	}

	if output["humanChanges"].(float64) != 1 {
		t.Errorf("Expected humanChanges=1, got %v", output["humanChanges"])
	}

	if output["systemChanges"].(float64) != 1 {
		t.Errorf("Expected systemChanges=1, got %v", output["systemChanges"])
	}

	highlights := output["highlights"].([]any)
	if len(highlights) == 0 {
		t.Error("Expected highlights")
	}

	t.Log("✓ summarize_recent_activity works correctly")
}

func TestCompareActivityPeriods(t *testing.T) {
	client := newMockClient()

	callCount := 0
	client.activityQueries.createFunc = func(ctx context.Context, query *v1alpha1.ActivityQuery, opts metav1.CreateOptions) (*v1alpha1.ActivityQuery, error) {
		callCount++
		now := metav1.NewTime(time.Now())

		var results []v1alpha1.Activity
		if callCount == 1 {
			// Baseline: 2 activities
			results = []v1alpha1.Activity{
				{
					ObjectMeta: metav1.ObjectMeta{Name: "baseline-1", CreationTimestamp: now},
					Spec: v1alpha1.ActivitySpec{
						Summary:      "alice created pod test-pod",
						ChangeSource: "human",
						Actor:        v1alpha1.ActivityActor{Name: "alice", Type: "user"},
						Resource:     v1alpha1.ActivityResource{Kind: "Pod", APIVersion: "v1"},
						Tenant:       v1alpha1.ActivityTenant{Type: "global", Name: "default"},
						Origin:       v1alpha1.ActivityOrigin{Type: "audit", ID: "test-1"},
					},
				},
				{
					ObjectMeta: metav1.ObjectMeta{Name: "baseline-2", CreationTimestamp: now},
					Spec: v1alpha1.ActivitySpec{
						Summary:      "alice updated pod test-pod",
						ChangeSource: "human",
						Actor:        v1alpha1.ActivityActor{Name: "alice", Type: "user"},
						Resource:     v1alpha1.ActivityResource{Kind: "Pod", APIVersion: "v1"},
						Tenant:       v1alpha1.ActivityTenant{Type: "global", Name: "default"},
						Origin:       v1alpha1.ActivityOrigin{Type: "audit", ID: "test-2"},
					},
				},
			}
		} else {
			// Comparison: 4 activities (100% increase)
			results = []v1alpha1.Activity{
				{
					ObjectMeta: metav1.ObjectMeta{Name: "compare-1", CreationTimestamp: now},
					Spec: v1alpha1.ActivitySpec{
						Summary:      "alice created pod test-pod",
						ChangeSource: "human",
						Actor:        v1alpha1.ActivityActor{Name: "alice", Type: "user"},
						Resource:     v1alpha1.ActivityResource{Kind: "Pod", APIVersion: "v1"},
						Tenant:       v1alpha1.ActivityTenant{Type: "global", Name: "default"},
						Origin:       v1alpha1.ActivityOrigin{Type: "audit", ID: "test-3"},
					},
				},
				{
					ObjectMeta: metav1.ObjectMeta{Name: "compare-2", CreationTimestamp: now},
					Spec: v1alpha1.ActivitySpec{
						Summary:      "alice updated pod test-pod",
						ChangeSource: "human",
						Actor:        v1alpha1.ActivityActor{Name: "alice", Type: "user"},
						Resource:     v1alpha1.ActivityResource{Kind: "Pod", APIVersion: "v1"},
						Tenant:       v1alpha1.ActivityTenant{Type: "global", Name: "default"},
						Origin:       v1alpha1.ActivityOrigin{Type: "audit", ID: "test-4"},
					},
				},
				{
					ObjectMeta: metav1.ObjectMeta{Name: "compare-3", CreationTimestamp: now},
					Spec: v1alpha1.ActivitySpec{
						Summary:      "bob created deployment test-deploy",
						ChangeSource: "human",
						Actor:        v1alpha1.ActivityActor{Name: "bob", Type: "user"},
						Resource:     v1alpha1.ActivityResource{Kind: "Deployment", APIVersion: "apps/v1"},
						Tenant:       v1alpha1.ActivityTenant{Type: "global", Name: "default"},
						Origin:       v1alpha1.ActivityOrigin{Type: "audit", ID: "test-5"},
					},
				},
				{
					ObjectMeta: metav1.ObjectMeta{Name: "compare-4", CreationTimestamp: now},
					Spec: v1alpha1.ActivitySpec{
						Summary:      "bob updated deployment test-deploy",
						ChangeSource: "human",
						Actor:        v1alpha1.ActivityActor{Name: "bob", Type: "user"},
						Resource:     v1alpha1.ActivityResource{Kind: "Deployment", APIVersion: "apps/v1"},
						Tenant:       v1alpha1.ActivityTenant{Type: "global", Name: "default"},
						Origin:       v1alpha1.ActivityOrigin{Type: "audit", ID: "test-6"},
					},
				},
			}
		}

		return &v1alpha1.ActivityQuery{
			ObjectMeta: metav1.ObjectMeta{Name: "test-compare"},
			Status: v1alpha1.ActivityQueryStatus{
				Results:            results,
				EffectiveStartTime: query.Spec.StartTime,
				EffectiveEndTime:   query.Spec.EndTime,
			},
		}, nil
	}

	provider := createTestProvider(client)

	args := CompareActivityPeriodsArgs{
		BaselineStart:   "now-14d",
		BaselineEnd:     "now-7d",
		ComparisonStart: "now-7d",
		ComparisonEnd:   "now",
	}

	result, _, err := provider.handleCompareActivityPeriods(context.Background(), nil, args)
	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}

	output := parseJSONResult(t, result)

	baseline := output["baseline"].(map[string]any)
	if baseline["count"].(float64) != 2 {
		t.Errorf("Expected baseline count=2, got %v", baseline["count"])
	}

	comparison := output["comparison"].(map[string]any)
	if comparison["count"].(float64) != 4 {
		t.Errorf("Expected comparison count=4, got %v", comparison["count"])
	}

	changePercent := output["changePercent"].(float64)
	if changePercent != 100 {
		t.Errorf("Expected changePercent=100, got %v", changePercent)
	}

	analysis := output["analysis"].(string)
	if analysis == "" {
		t.Error("Expected analysis string")
	}

	t.Log("✓ compare_activity_periods works correctly")
}

func TestListActivityPolicies(t *testing.T) {
	client := newMockClient()
	provider := createTestProvider(client)

	args := ListActivityPoliciesArgs{}

	result, _, err := provider.handleListActivityPolicies(context.Background(), nil, args)
	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}

	output := parseJSONResult(t, result)

	policies := output["policies"].([]any)
	if len(policies) != 1 {
		t.Errorf("Expected 1 policy, got %d", len(policies))
	}

	policy := policies[0].(map[string]any)
	if policy["name"] != "networking-httpproxy" {
		t.Errorf("Expected name=networking-httpproxy, got %v", policy["name"])
	}
	if policy["status"] != "Ready" {
		t.Errorf("Expected status=Ready, got %v", policy["status"])
	}

	t.Log("✓ list_activity_policies works correctly")
}

func TestListActivityPoliciesWithFilter(t *testing.T) {
	client := newMockClient()
	provider := createTestProvider(client)

	args := ListActivityPoliciesArgs{
		Kind: "SomethingElse", // Won't match
	}

	result, _, err := provider.handleListActivityPolicies(context.Background(), nil, args)
	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}

	output := parseJSONResult(t, result)

	policies := output["policies"].([]any)
	if len(policies) != 0 {
		t.Errorf("Expected 0 policies after filter, got %d", len(policies))
	}

	t.Log("✓ list_activity_policies filtering works correctly")
}

func TestPreviewActivityPolicy(t *testing.T) {
	client := newMockClient()
	provider := createTestProvider(client)

	args := PreviewActivityPolicyArgs{
		Policy: v1alpha1.ActivityPolicySpec{
			Resource: v1alpha1.ActivityPolicyResource{
				APIGroup: "networking.datumapis.com",
				Kind:     "HTTPProxy",
			},
			AuditRules: []v1alpha1.ActivityPolicyRule{
				{Match: "audit.verb == 'create'", Summary: "{{ actor }} created HTTPProxy"},
			},
		},
		Inputs: []json.RawMessage{
			json.RawMessage(`{"type":"audit"}`),
		},
	}

	result, _, err := provider.handlePreviewActivityPolicy(context.Background(), nil, args)
	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}

	output := parseJSONResult(t, result)

	results := output["results"].([]any)
	if len(results) != 1 {
		t.Errorf("Expected 1 result, got %d", len(results))
	}

	resultEntry := results[0].(map[string]any)
	if resultEntry["matched"] != true {
		t.Error("Expected matched=true")
	}

	activities := output["activities"].([]any)
	if len(activities) != 1 {
		t.Errorf("Expected 1 activity, got %d", len(activities))
	}

	t.Log("✓ preview_activity_policy works correctly")
}

// =============================================================================
// Test Tool Registration
// =============================================================================

func TestRegisterTools(t *testing.T) {
	client := newMockClient()
	provider := createTestProvider(client)

	server := mcp.NewServer(&mcp.Implementation{Name: "test", Version: "1.0.0"}, nil)
	provider.RegisterTools(server)

	t.Log("✓ RegisterTools completed without error")
}

// =============================================================================
// Test Helper Functions
// =============================================================================

func TestIsSystemUser(t *testing.T) {
	tests := []struct {
		username string
		expected bool
	}{
		{"alice@example.com", false},
		{"bob", false},
		{"system:serviceaccount:default:my-sa", true},
		{"system:controller", true},
		{"my-controller", true},
	}

	for _, tc := range tests {
		result := isSystemUser(tc.username)
		if result != tc.expected {
			t.Errorf("isSystemUser(%q) = %v, expected %v", tc.username, result, tc.expected)
		}
	}

	t.Log("✓ isSystemUser works correctly")
}

func TestGetTopN(t *testing.T) {
	counts := map[string]int{
		"a": 10,
		"b": 50,
		"c": 30,
		"d": 5,
	}

	result := getTopN(counts, 2)

	if len(result) != 2 {
		t.Errorf("Expected 2 items, got %d", len(result))
	}

	if result[0]["name"] != "b" {
		t.Errorf("Expected first item to be 'b', got %v", result[0]["name"])
	}

	if result[1]["name"] != "c" {
		t.Errorf("Expected second item to be 'c', got %v", result[1]["name"])
	}

	t.Log("✓ getTopN works correctly")
}

func TestAbsFloat(t *testing.T) {
	if absFloat(-5.0) != 5.0 {
		t.Error("absFloat(-5.0) should be 5.0")
	}
	if absFloat(5.0) != 5.0 {
		t.Error("absFloat(5.0) should be 5.0")
	}
	if absFloat(0) != 0 {
		t.Error("absFloat(0) should be 0")
	}

	t.Log("✓ absFloat works correctly")
}
