package tools

import (
	"context"
	"errors"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	auditv1 "k8s.io/apiserver/pkg/apis/audit/v1"

	"go.miloapis.com/activity/internal/cel"
	"go.miloapis.com/activity/pkg/apis/activity/v1alpha1"
	activityclient "go.miloapis.com/activity/pkg/client/clientset/versioned/typed/activity/v1alpha1"
)

// listTools connects an in-memory client to server and returns its tools.
func listTools(t *testing.T, server *mcp.Server) []*mcp.Tool {
	t.Helper()
	res, err := connectInMemory(t, server).ListTools(context.Background(), nil)
	if err != nil {
		t.Fatalf("ListTools: %v", err)
	}
	return res.Tools
}

// connectInMemory connects an in-memory client session to server.
func connectInMemory(t *testing.T, server *mcp.Server) *mcp.ClientSession {
	t.Helper()
	ctx := context.Background()
	st, ct := mcp.NewInMemoryTransports()
	ss, err := server.Connect(ctx, st, nil)
	if err != nil {
		t.Fatalf("server connect: %v", err)
	}
	t.Cleanup(func() { _ = ss.Close() })

	cs, err := mcp.NewClient(&mcp.Implementation{Name: "test"}, nil).Connect(ctx, ct, nil)
	if err != nil {
		t.Fatalf("client connect: %v", err)
	}
	t.Cleanup(func() { _ = cs.Close() })
	return cs
}

func toolNames(ts []*mcp.Tool) []string {
	out := make([]string, 0, len(ts))
	for _, tool := range ts {
		out = append(out, tool.Name)
	}
	sort.Strings(out)
	return out
}

// TestRegisterToolsRegistersEverythingReadOnly: stdio mode keeps every tool,
// and every tool advertises that it changes nothing.
func TestRegisterToolsRegistersEverythingReadOnly(t *testing.T) {
	server := mcp.NewServer(&mcp.Implementation{Name: "test", Version: "1.0.0"}, nil)
	createTestProvider(newMockClient()).RegisterTools(server)

	got := listTools(t, server)
	if len(got) != 14 {
		t.Errorf("registered %d tools (%v), want 14", len(got), toolNames(got))
	}
	for _, tool := range got {
		if tool.Annotations == nil || !tool.Annotations.ReadOnlyHint {
			t.Errorf("tool %s is not annotated read-only", tool.Name)
		}
	}
}

func TestRegisterToolsWithOptionsHonoursAllowList(t *testing.T) {
	server := mcp.NewServer(&mcp.Implementation{Name: "test", Version: "1.0.0"}, nil)
	createTestProvider(newMockClient()).RegisterToolsWithOptions(server, RegisterOptions{
		Tools: []string{ToolQueryActivities, ToolQueryEvents},
	})

	got := toolNames(listTools(t, server))
	if strings.Join(got, ",") != "query_activities,query_events" {
		t.Errorf("tools = %v, want [query_activities query_events]", got)
	}
}

// TestResolverErrorBecomesToolError: when the client cannot be resolved, the
// call fails as a tool error carrying the resolver's message, and no client
// is ever touched.
func TestResolverErrorBecomesToolError(t *testing.T) {
	provider := NewToolProviderWithResolver(func(context.Context) (activityclient.ActivityV1alpha1Interface, error) {
		return nil, errors.New("no project on this request")
	})
	ctx := context.Background()

	results := map[string]*mcp.CallToolResult{}
	results["query_activities"], _, _ = provider.handleQueryActivities(ctx, nil, QueryActivitiesArgs{StartTime: "now-1h", EndTime: "now"})
	results["get_activity_facets"], _, _ = provider.handleGetActivityFacets(ctx, nil, GetActivityFacetsArgs{Fields: []string{"spec.actor.name"}})
	results["find_failed_operations"], _, _ = provider.handleFindFailedOperations(ctx, nil, FindFailedOperationsArgs{StartTime: "now-1h"})
	results["get_resource_history"], _, _ = provider.handleGetResourceHistory(ctx, nil, GetResourceHistoryArgs{Name: "x"})
	results["get_user_activity_summary"], _, _ = provider.handleGetUserActivitySummary(ctx, nil, GetUserActivitySummaryArgs{Username: "alice"})
	results["get_activity_timeline"], _, _ = provider.handleGetActivityTimeline(ctx, nil, GetActivityTimelineArgs{StartTime: "now-1h"})
	results["summarize_recent_activity"], _, _ = provider.handleSummarizeRecentActivity(ctx, nil, SummarizeRecentActivityArgs{StartTime: "now-1h"})
	results["compare_activity_periods"], _, _ = provider.handleCompareActivityPeriods(ctx, nil, CompareActivityPeriodsArgs{})
	results["query_events"], _, _ = provider.handleQueryEvents(ctx, nil, QueryEventsArgs{StartTime: "now-1h", EndTime: "now"})
	results["get_event_facets"], _, _ = provider.handleGetEventFacets(ctx, nil, GetEventFacetsArgs{Fields: []string{"reason"}})
	results["query_audit_logs"], _, _ = provider.handleQueryAuditLogs(ctx, nil, QueryAuditLogsArgs{})
	results["get_audit_log_facets"], _, _ = provider.handleGetAuditLogFacets(ctx, nil, GetAuditLogFacetsArgs{})
	results["list_activity_policies"], _, _ = provider.handleListActivityPolicies(ctx, nil, ListActivityPoliciesArgs{})
	results["preview_activity_policy"], _, _ = provider.handlePreviewActivityPolicy(ctx, nil, PreviewActivityPolicyArgs{})

	for name, res := range results {
		if res == nil || !res.IsError {
			t.Errorf("%s: want a tool error", name)
			continue
		}
		if text := res.Content[0].(*mcp.TextContent).Text; !strings.Contains(text, "no project on this request") {
			t.Errorf("%s: error = %q, want the resolver's message", name, text)
		}
	}
}

// TestActivityToolsSendTheirFilters: arguments that narrow a query must reach
// the server as a CEL filter, not be dropped, and must be quoted so a value
// cannot extend the expression. Each filter is also compiled with the
// server's own CEL environment, so a field the server does not support fails
// here rather than in production.
func TestActivityToolsSendTheirFilters(t *testing.T) {
	client := newMockClient()
	var filters []string
	client.activityQueries.createFunc = func(_ context.Context, q *v1alpha1.ActivityQuery, _ metav1.CreateOptions) (*v1alpha1.ActivityQuery, error) {
		filters = append(filters, q.Spec.Filter)
		return &v1alpha1.ActivityQuery{}, nil
	}
	provider := createTestProvider(client)
	ctx := context.Background()

	_, _, _ = provider.handleQueryActivities(ctx, nil, QueryActivitiesArgs{
		StartTime: "now-1h", EndTime: "now", ChangeSource: "human", ActorName: "alice", ResourceKind: "HTTPProxy", APIGroup: "networking.datumapis.com",
	})
	_, _, _ = provider.handleGetResourceHistory(ctx, nil, GetResourceHistoryArgs{Name: "api-gateway", Kind: "HTTPProxy"})
	_, _, _ = provider.handleGetUserActivitySummary(ctx, nil, GetUserActivitySummaryArgs{Username: `bob" || true || "`})
	_, _, _ = provider.handleGetUserActivitySummary(ctx, nil, GetUserActivitySummaryArgs{Username: "system:serviceaccount:default:builder"})
	_, _, _ = provider.handleGetActivityTimeline(ctx, nil, GetActivityTimelineArgs{StartTime: "now-1h", ChangeSource: "system"})
	_, _, _ = provider.handleSummarizeRecentActivity(ctx, nil, SummarizeRecentActivityArgs{StartTime: "now-1h", ChangeSource: "human"})

	want := []string{
		`spec.changeSource == "human" && spec.actor.name == "alice" && spec.resource.kind == "HTTPProxy" && spec.resource.apiGroup == "networking.datumapis.com"`,
		`spec.resource.kind == "HTTPProxy" && spec.resource.name == "api-gateway"`,
		`spec.actor.name == "bob\" || true || \""`,
		`spec.actor.name == "serviceaccount:default:builder"`,
		`spec.changeSource == "system"`,
		`spec.changeSource == "human"`,
	}
	if len(filters) != len(want) {
		t.Fatalf("got %d queries, want %d", len(filters), len(want))
	}
	for i := range want {
		if filters[i] != want[i] {
			t.Errorf("query %d filter = %q, want %q", i, filters[i], want[i])
		}
		if _, err := cel.CompileActivityFilter(filters[i]); err != nil {
			t.Errorf("query %d filter %q does not compile as an Activity filter: %v", i, filters[i], err)
		}
		if _, _, err := cel.ConvertActivityToClickHouseSQL(ctx, filters[i]); err != nil {
			t.Errorf("query %d filter %q does not convert to SQL: %v", i, filters[i], err)
		}
	}
}

// TestFindFailedOperationsFilterCompiles checks the audit log filter against
// the server's audit CEL environment, including quoting of hostile values.
func TestFindFailedOperationsFilterCompiles(t *testing.T) {
	client := newMockClient()
	var filter string
	client.auditLogQueries.createFunc = func(_ context.Context, q *v1alpha1.AuditLogQuery, _ metav1.CreateOptions) (*v1alpha1.AuditLogQuery, error) {
		filter = q.Spec.Filter
		return &v1alpha1.AuditLogQuery{}, nil
	}
	ctx := context.Background()
	_, _, _ = createTestProvider(client).handleFindFailedOperations(ctx, nil, FindFailedOperationsArgs{
		StartTime: "now-1h", Username: `alice' || true || '`, Resource: "httpproxies", Verb: "delete",
	})

	want := `responseStatus.code >= 400 && responseStatus.code <= 599 && user.username == "alice' || true || '" && objectRef.resource == "httpproxies" && verb == "delete"`
	if filter != want {
		t.Errorf("filter = %q, want %q", filter, want)
	}
	if _, err := cel.CompileFilter(filter); err != nil {
		t.Errorf("filter %q does not compile as an audit filter: %v", filter, err)
	}
	if _, _, err := cel.ConvertToClickHouseSQL(ctx, filter); err != nil {
		t.Errorf("filter %q does not convert to SQL: %v", filter, err)
	}
}

// TestFindFailedOperationsToleratesNonResourceRequests is a regression test:
// a failed request to a non-resource path has no objectRef, and may have no
// responseStatus. Dereferencing either crashed the whole process.
func TestFindFailedOperationsToleratesNonResourceRequests(t *testing.T) {
	client := newMockClient()
	client.auditLogQueries.createFunc = func(_ context.Context, q *v1alpha1.AuditLogQuery, _ metav1.CreateOptions) (*v1alpha1.AuditLogQuery, error) {
		return &v1alpha1.AuditLogQuery{Status: v1alpha1.AuditLogQueryStatus{Results: []auditv1.Event{
			{Verb: "get", RequestURI: "/apis/foo/v1", ResponseStatus: &metav1.Status{Code: 404, Message: "not found"}},
			{Verb: "get", RequestURI: "/healthz"},
		}}}, nil
	}

	res, _, err := createTestProvider(client).handleFindFailedOperations(context.Background(), nil, FindFailedOperationsArgs{StartTime: "now-1h"})
	if err != nil {
		t.Fatalf("handleFindFailedOperations: %v", err)
	}
	out := parseJSONResult(t, res)
	if out["count"].(float64) != 2 {
		t.Errorf("count = %v, want 2", out["count"])
	}
	first := out["failures"].([]any)[0].(map[string]any)
	if first["requestURI"] != "/apis/foo/v1" || first["statusCode"].(float64) != 404 {
		t.Errorf("first failure = %v, want the request URI and status code", first)
	}
}

func TestQueryEventsRejectsSelectorMetacharacters(t *testing.T) {
	client := newMockClient()
	called := false
	client.eventQueries.createFunc = func(_ context.Context, q *v1alpha1.EventQuery, _ metav1.CreateOptions) (*v1alpha1.EventQuery, error) {
		called = true
		return &v1alpha1.EventQuery{}, nil
	}
	provider := createTestProvider(client)

	for _, args := range []QueryEventsArgs{
		{RegardingName: "web,type=Normal"},
		{Reason: "Failed=1"},
		{Type: "Warning!"},
		{RegardingKind: "Pod,reason!=x"},
		{SourceComponent: "a=b"},
	} {
		args.StartTime, args.EndTime = "now-1h", "now"
		res, _, _ := provider.handleQueryEvents(context.Background(), nil, args)
		if res == nil || !res.IsError {
			t.Errorf("query_events(%+v) succeeded, want a tool error", args)
		}
	}
	if called {
		t.Error("an invalid selector value reached the API")
	}

	res, _, _ := provider.handleQueryEvents(context.Background(), nil, QueryEventsArgs{
		StartTime: "now-1h", EndTime: "now", RegardingName: "web-1", Type: "Warning",
	})
	if res.IsError {
		t.Errorf("valid selector values were rejected: %v", res.Content)
	}
}

// TestToolCallsShareOneDeadline: a multi-query tool runs under a single
// overall deadline no later than ToolCallTimeout, not one per query.
func TestToolCallsShareOneDeadline(t *testing.T) {
	client := newMockClient()
	var deadlines []time.Time
	client.activityQueries.createFunc = func(ctx context.Context, q *v1alpha1.ActivityQuery, _ metav1.CreateOptions) (*v1alpha1.ActivityQuery, error) {
		d, ok := ctx.Deadline()
		if !ok {
			t.Error("tool call context has no deadline")
		}
		deadlines = append(deadlines, d)
		return &v1alpha1.ActivityQuery{}, nil
	}
	server := mcp.NewServer(&mcp.Implementation{Name: "test", Version: "1.0.0"}, nil)
	createTestProvider(client).RegisterTools(server)

	res, err := connectInMemory(t, server).CallTool(context.Background(), &mcp.CallToolParams{
		Name: ToolCompareActivityPeriods,
		Arguments: map[string]any{
			"baselineStart": "now-2h", "baselineEnd": "now-1h", "comparisonStart": "now-1h", "comparisonEnd": "now",
		},
	})
	if err != nil || res.IsError {
		t.Fatalf("CallTool: %v %v", err, res)
	}
	end := time.Now()
	if len(deadlines) != 2 {
		t.Fatalf("got %d queries, want 2", len(deadlines))
	}
	if !deadlines[0].Equal(deadlines[1]) {
		t.Errorf("queries ran under different deadlines %v and %v, want one overall deadline", deadlines[0], deadlines[1])
	}
	if deadlines[0].After(end.Add(ToolCallTimeout)) {
		t.Errorf("deadline %v is more than ToolCallTimeout away", deadlines[0])
	}
}

// TestRecoverMiddlewareTurnsPanicsIntoToolErrors: a panicking tool must fail
// that one call, and the server must keep serving.
func TestRecoverMiddlewareTurnsPanicsIntoToolErrors(t *testing.T) {
	server := mcp.NewServer(&mcp.Implementation{Name: "test", Version: "1.0.0"}, nil)
	server.AddReceivingMiddleware(RecoverMiddleware)
	mcp.AddTool(server, &mcp.Tool{Name: "boom"}, func(context.Context, *mcp.CallToolRequest, struct{}) (*mcp.CallToolResult, any, error) {
		var ref *auditv1.ObjectReference
		_ = ref.Name // nil dereference
		return nil, nil, nil
	})
	session := connectInMemory(t, server)

	res, err := session.CallTool(context.Background(), &mcp.CallToolParams{Name: "boom", Arguments: map[string]any{}})
	if err != nil {
		t.Fatalf("CallTool: %v", err)
	}
	if !res.IsError {
		t.Error("panicking tool did not return a tool error")
	}
	if _, err := session.ListTools(context.Background(), nil); err != nil {
		t.Errorf("server stopped serving after a panic: %v", err)
	}
}
