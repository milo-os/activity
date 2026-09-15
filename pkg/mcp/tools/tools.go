// Package tools provides MCP (Model Context Protocol) tools for interacting with
// the Activity service. These tools can be used standalone or embedded into an
// external MCP server.
package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"

	"go.miloapis.com/activity/pkg/apis/activity/v1alpha1"
	activityclient "go.miloapis.com/activity/pkg/client/clientset/versioned/typed/activity/v1alpha1"
)

// ToolProvider provides MCP tools for interacting with the Activity API.
// It wraps an Activity API client and exposes query capabilities as MCP tools.
type ToolProvider struct {
	client    activityclient.ActivityV1alpha1Interface
	namespace string
}

// Config contains configuration for the ToolProvider.
type Config struct {
	// Kubeconfig is the path to a kubeconfig file.
	// If empty, uses in-cluster config or default kubeconfig location.
	Kubeconfig string

	// Context is the kubeconfig context to use.
	// If empty, uses the current context.
	Context string

	// Namespace for namespaced resources (e.g., Activities).
	// If empty, uses "default".
	Namespace string
}

// NewToolProvider creates a new ToolProvider with the given configuration.
func NewToolProvider(cfg Config) (*ToolProvider, error) {
	var restConfig *rest.Config
	var err error

	if cfg.Kubeconfig != "" {
		// Load from specified kubeconfig file
		loadingRules := &clientcmd.ClientConfigLoadingRules{ExplicitPath: cfg.Kubeconfig}
		configOverrides := &clientcmd.ConfigOverrides{}
		if cfg.Context != "" {
			configOverrides.CurrentContext = cfg.Context
		}
		kubeConfig := clientcmd.NewNonInteractiveDeferredLoadingClientConfig(loadingRules, configOverrides)
		restConfig, err = kubeConfig.ClientConfig()
	} else {
		// Try in-cluster config first, fall back to default kubeconfig
		restConfig, err = rest.InClusterConfig()
		if err != nil {
			loadingRules := clientcmd.NewDefaultClientConfigLoadingRules()
			configOverrides := &clientcmd.ConfigOverrides{}
			if cfg.Context != "" {
				configOverrides.CurrentContext = cfg.Context
			}
			kubeConfig := clientcmd.NewNonInteractiveDeferredLoadingClientConfig(loadingRules, configOverrides)
			restConfig, err = kubeConfig.ClientConfig()
		}
	}

	if err != nil {
		return nil, fmt.Errorf("failed to create kubernetes config: %w", err)
	}

	client, err := activityclient.NewForConfig(restConfig)
	if err != nil {
		return nil, fmt.Errorf("failed to create activity client: %w", err)
	}

	namespace := cfg.Namespace
	if namespace == "" {
		namespace = "default"
	}

	return &ToolProvider{
		client:    client,
		namespace: namespace,
	}, nil
}

// NewToolProviderWithClient creates a ToolProvider with an existing client.
// This is useful for embedding the tools into an existing application.
func NewToolProviderWithClient(client activityclient.ActivityV1alpha1Interface, namespace string) *ToolProvider {
	if namespace == "" {
		namespace = "default"
	}
	return &ToolProvider{
		client:    client,
		namespace: namespace,
	}
}

// Close releases resources held by the ToolProvider.
func (p *ToolProvider) Close() error {
	// Kubernetes client doesn't need explicit cleanup
	return nil
}

// RegisterTools registers all activity tools with an MCP server.
func (p *ToolProvider) RegisterTools(server *mcp.Server) {
	// Audit log tools
	mcp.AddTool(server, &mcp.Tool{
		Name:        "query_audit_logs",
		Description: "Search audit logs from the Kubernetes control plane. Use this to investigate incidents, track resource changes, or analyze user activity. Results are returned newest-first.",
	}, p.handleQueryAuditLogs)

	mcp.AddTool(server, &mcp.Tool{
		Name:        "get_audit_log_facets",
		Description: "Get distinct values and counts for audit log fields. Use this to discover what verbs, users, resources, and namespaces appear in the audit logs. Useful for building filters or understanding activity patterns.",
	}, p.handleGetAuditLogFacets)

	// Activity tools (human-readable summaries)
	mcp.AddTool(server, &mcp.Tool{
		Name:        "query_activities",
		Description: "Search human-readable activity summaries. Activities are translated from audit logs into friendly descriptions like 'alice created HTTP proxy api-gateway'. Use this to understand what changed in plain language.",
	}, p.handleQueryActivities)

	mcp.AddTool(server, &mcp.Tool{
		Name:        "get_activity_facets",
		Description: "Get distinct values and counts for activity fields. Discover who's active, what resources are changing, and whether changes are human or automated. Valid fields: spec.changeSource, spec.actor.name, spec.actor.type, spec.resource.apiGroup, spec.resource.kind, spec.resource.namespace.",
	}, p.handleGetActivityFacets)

	// Investigation tools
	mcp.AddTool(server, &mcp.Tool{
		Name:        "find_failed_operations",
		Description: "Find operations that failed (HTTP 4xx/5xx responses). Use this to debug permission issues, find failed deployments, or investigate security events.",
	}, p.handleFindFailedOperations)

	mcp.AddTool(server, &mcp.Tool{
		Name:        "get_resource_history",
		Description: "Get the change history for a specific resource. See who changed what, when, with field-level diffs where available. Use this to understand how a resource evolved over time.",
	}, p.handleGetResourceHistory)

	mcp.AddTool(server, &mcp.Tool{
		Name:        "get_user_activity_summary",
		Description: "Get a summary of a specific user's recent actions. See what resources they modified, when, and how often. Useful for security reviews and understanding user behavior.",
	}, p.handleGetUserActivitySummary)

	// Analytics tools
	mcp.AddTool(server, &mcp.Tool{
		Name:        "get_activity_timeline",
		Description: "Get activity counts grouped by time buckets (hourly/daily). Use this to visualize activity patterns, identify peak periods, and correlate with incidents.",
	}, p.handleGetActivityTimeline)

	mcp.AddTool(server, &mcp.Tool{
		Name:        "summarize_recent_activity",
		Description: "Generate a summary of recent activity including top actors, most changed resources, and key highlights. Perfect for status updates and handoffs.",
	}, p.handleSummarizeRecentActivity)

	mcp.AddTool(server, &mcp.Tool{
		Name:        "compare_activity_periods",
		Description: "Compare activity between two time periods. Identify what changed, new actors, increased/decreased activity. Use this for incident investigation and trend analysis.",
	}, p.handleCompareActivityPeriods)

	// Policy tools
	mcp.AddTool(server, &mcp.Tool{
		Name:        "list_activity_policies",
		Description: "List configured ActivityPolicies that translate audit logs into human-readable summaries. See what resource types have translation rules and their status.",
	}, p.handleListActivityPolicies)

	mcp.AddTool(server, &mcp.Tool{
		Name:        "preview_activity_policy",
		Description: "Test an ActivityPolicy against sample audit events to see what activities would be generated. Use this to develop and debug policies before deployment.",
	}, p.handlePreviewActivityPolicy)

	// Event tools
	mcp.AddTool(server, &mcp.Tool{
		Name:        "query_events",
		Description: "Search control plane events stored in the Activity service. Events capture resource lifecycle changes, provisioning status, warnings, and errors. Use this to investigate issues, debug deployments, or monitor system health. Results are returned newest-first.",
	}, p.handleQueryEvents)

	mcp.AddTool(server, &mcp.Tool{
		Name:        "get_event_facets",
		Description: "Get distinct values and counts for event fields. Use this to discover what event types, reasons, source components, and involved resources appear in the event stream. Useful for building filters or understanding event patterns.",
	}, p.handleGetEventFacets)
}

// =============================================================================
// Query Audit Logs
// =============================================================================

// QueryAuditLogsArgs contains the arguments for the query_audit_logs tool.
type QueryAuditLogsArgs struct {
	// StartTime is the beginning of the search window.
	StartTime string `json:"startTime"`

	// EndTime is the end of the search window.
	EndTime string `json:"endTime"`

	// Filter is a CEL filter expression to narrow results.
	Filter string `json:"filter,omitempty"`

	// Limit is the maximum number of results to return.
	Limit int `json:"limit,omitempty"`
}

func (p *ToolProvider) handleQueryAuditLogs(ctx context.Context, req *mcp.CallToolRequest, args QueryAuditLogsArgs) (*mcp.CallToolResult, any, error) {
	limit := int32(args.Limit)
	if limit == 0 {
		limit = 100
	}

	query := &v1alpha1.AuditLogQuery{
		ObjectMeta: metav1.ObjectMeta{
			GenerateName: "mcp-query-",
		},
		Spec: v1alpha1.AuditLogQuerySpec{
			StartTime: args.StartTime,
			EndTime:   args.EndTime,
			Filter:    args.Filter,
			Limit:     limit,
		},
	}

	result, err := p.client.AuditLogQueries().Create(ctx, query, metav1.CreateOptions{})
	if err != nil {
		return errorResult(fmt.Sprintf("Query failed: %v", err)), nil, nil
	}

	output := map[string]any{
		"count":              len(result.Status.Results),
		"continue":           result.Status.Continue,
		"effectiveStartTime": result.Status.EffectiveStartTime,
		"effectiveEndTime":   result.Status.EffectiveEndTime,
		"events":             result.Status.Results,
	}

	return jsonResult(output)
}

// =============================================================================
// Get Audit Log Facets
// =============================================================================

// GetAuditLogFacetsArgs contains the arguments for the get_audit_log_facets tool.
type GetAuditLogFacetsArgs struct {
	// Fields to get facets for.
	Fields []string `json:"fields"`

	// StartTime is the beginning of the time window.
	StartTime string `json:"startTime,omitempty"`

	// EndTime is the end of the time window.
	EndTime string `json:"endTime,omitempty"`

	// Filter is a CEL filter to narrow down audit logs before computing facets.
	Filter string `json:"filter,omitempty"`

	// Limit is the maximum number of distinct values per field.
	Limit int `json:"limit,omitempty"`
}

func (p *ToolProvider) handleGetAuditLogFacets(ctx context.Context, req *mcp.CallToolRequest, args GetAuditLogFacetsArgs) (*mcp.CallToolResult, any, error) {
	limit := int32(args.Limit)
	if limit == 0 {
		limit = 20
	}

	startTime := args.StartTime
	if startTime == "" {
		startTime = "now-7d"
	}

	endTime := args.EndTime
	if endTime == "" {
		endTime = "now"
	}

	facetSpecs := make([]v1alpha1.FacetSpec, 0, len(args.Fields))
	for _, field := range args.Fields {
		facetSpecs = append(facetSpecs, v1alpha1.FacetSpec{
			Field: field,
			Limit: limit,
		})
	}

	query := &v1alpha1.AuditLogFacetsQuery{
		ObjectMeta: metav1.ObjectMeta{
			GenerateName: "mcp-facets-",
		},
		Spec: v1alpha1.AuditLogFacetsQuerySpec{
			TimeRange: v1alpha1.FacetTimeRange{
				Start: startTime,
				End:   endTime,
			},
			Filter: args.Filter,
			Facets: facetSpecs,
		},
	}

	result, err := p.client.AuditLogFacetsQueries().Create(ctx, query, metav1.CreateOptions{})
	if err != nil {
		return errorResult(fmt.Sprintf("Query failed: %v", err)), nil, nil
	}

	output := make(map[string]any)
	for _, facet := range result.Status.Facets {
		values := make([]map[string]any, 0, len(facet.Values))
		for _, v := range facet.Values {
			values = append(values, map[string]any{
				"value": v.Value,
				"count": v.Count,
			})
		}
		output[facet.Field] = values
	}

	return jsonResult(output)
}

// =============================================================================
// Query Activities
// =============================================================================

// QueryActivitiesArgs contains the arguments for the query_activities tool.
type QueryActivitiesArgs struct {
	// StartTime is the beginning of the search window.
	StartTime string `json:"startTime"`

	// EndTime is the end of the search window.
	EndTime string `json:"endTime"`

	// ChangeSource filters by change source.
	ChangeSource string `json:"changeSource,omitempty"`

	// ActorName filters by actor name.
	ActorName string `json:"actorName,omitempty"`

	// ResourceKind filters by resource kind.
	ResourceKind string `json:"resourceKind,omitempty"`

	// APIGroup filters by API group.
	APIGroup string `json:"apiGroup,omitempty"`

	// Search performs full-text search on summary.
	Search string `json:"search,omitempty"`

	// Limit is the maximum number of results to return.
	Limit int `json:"limit,omitempty"`
}

func (p *ToolProvider) handleQueryActivities(ctx context.Context, req *mcp.CallToolRequest, args QueryActivitiesArgs) (*mcp.CallToolResult, any, error) {
	limit := int32(args.Limit)
	if limit == 0 {
		limit = 100
	}

	query := &v1alpha1.ActivityQuery{
		ObjectMeta: metav1.ObjectMeta{
			GenerateName: "mcp-activity-query-",
		},
		Spec: v1alpha1.ActivityQuerySpec{
			StartTime: args.StartTime,
			EndTime:   args.EndTime,
			Search:    args.Search,
			Limit:     limit,
		},
	}

	result, err := p.client.ActivityQueries().Create(ctx, query, metav1.CreateOptions{})
	if err != nil {
		return errorResult(fmt.Sprintf("Query failed: %v", err)), nil, nil
	}

	// Format results for readability
	activities := make([]map[string]any, 0, len(result.Status.Results))
	for _, activity := range result.Status.Results {
		activityMap := map[string]any{
			"name":         activity.Name,
			"summary":      activity.Spec.Summary,
			"changeSource": activity.Spec.ChangeSource,
			"actor": map[string]any{
				"type": activity.Spec.Actor.Type,
				"name": activity.Spec.Actor.Name,
			},
			"resource": map[string]any{
				"apiGroup":  activity.Spec.Resource.APIGroup,
				"kind":      activity.Spec.Resource.Kind,
				"name":      activity.Spec.Resource.Name,
				"namespace": activity.Spec.Resource.Namespace,
			},
			"timestamp": activity.CreationTimestamp.UTC().Format(time.RFC3339),
		}
		activities = append(activities, activityMap)
	}

	output := map[string]any{
		"count":              len(activities),
		"continue":           result.Status.Continue,
		"effectiveStartTime": result.Status.EffectiveStartTime,
		"effectiveEndTime":   result.Status.EffectiveEndTime,
		"activities":         activities,
	}

	return jsonResult(output)
}

// =============================================================================
// Get Activity Facets
// =============================================================================

// GetActivityFacetsArgs contains the arguments for the get_activity_facets tool.
type GetActivityFacetsArgs struct {
	// Fields to get facets for.
	// Valid values: spec.changeSource, spec.actor.name, spec.actor.type,
	// spec.resource.apiGroup, spec.resource.kind, spec.resource.namespace
	Fields []string `json:"fields"`

	// StartTime is the beginning of the time window.
	StartTime string `json:"startTime,omitempty"`

	// EndTime is the end of the time window.
	EndTime string `json:"endTime,omitempty"`

	// Filter narrows the activities before computing facets.
	Filter string `json:"filter,omitempty"`

	// Limit is the maximum number of distinct values per field.
	Limit int `json:"limit,omitempty"`
}

func (p *ToolProvider) handleGetActivityFacets(ctx context.Context, req *mcp.CallToolRequest, args GetActivityFacetsArgs) (*mcp.CallToolResult, any, error) {
	limit := int32(args.Limit)
	if limit == 0 {
		limit = 20
	}

	startTime := args.StartTime
	if startTime == "" {
		startTime = "now-7d"
	}

	endTime := args.EndTime
	if endTime == "" {
		endTime = "now"
	}

	facetSpecs := make([]v1alpha1.FacetSpec, 0, len(args.Fields))
	for _, field := range args.Fields {
		facetSpecs = append(facetSpecs, v1alpha1.FacetSpec{
			Field: field,
			Limit: limit,
		})
	}

	query := &v1alpha1.ActivityFacetQuery{
		ObjectMeta: metav1.ObjectMeta{
			GenerateName: "mcp-activity-facets-",
		},
		Spec: v1alpha1.ActivityFacetQuerySpec{
			TimeRange: v1alpha1.FacetTimeRange{
				Start: startTime,
				End:   endTime,
			},
			Filter: args.Filter,
			Facets: facetSpecs,
		},
	}

	result, err := p.client.ActivityFacetQueries().Create(ctx, query, metav1.CreateOptions{})
	if err != nil {
		return errorResult(fmt.Sprintf("Query failed: %v", err)), nil, nil
	}

	output := make(map[string]any)
	for _, facet := range result.Status.Facets {
		values := make([]map[string]any, 0, len(facet.Values))
		for _, v := range facet.Values {
			values = append(values, map[string]any{
				"value": v.Value,
				"count": v.Count,
			})
		}
		output[facet.Field] = values
	}

	return jsonResult(output)
}

// =============================================================================
// Find Failed Operations
// =============================================================================

// FindFailedOperationsArgs contains the arguments for the find_failed_operations tool.
// Note: This tool queries audit logs directly since Activities don't capture failed operations.
type FindFailedOperationsArgs struct {
	// StartTime is the beginning of the search window.
	StartTime string `json:"startTime"`

	// EndTime is the end of the search window.
	EndTime string `json:"endTime,omitempty"`

	// StatusCodeMin is the minimum status code to include.
	StatusCodeMin int `json:"statusCodeMin,omitempty"`

	// StatusCodeMax is the maximum status code to include.
	StatusCodeMax int `json:"statusCodeMax,omitempty"`

	// Username filters by actor.
	Username string `json:"username,omitempty"`

	// Resource filters by resource type.
	Resource string `json:"resource,omitempty"`

	// Verb filters by verb.
	Verb string `json:"verb,omitempty"`

	// Limit is the maximum number of results.
	Limit int `json:"limit,omitempty"`
}

func (p *ToolProvider) handleFindFailedOperations(ctx context.Context, req *mcp.CallToolRequest, args FindFailedOperationsArgs) (*mcp.CallToolResult, any, error) {
	limit := int32(args.Limit)
	if limit == 0 {
		limit = 100
	}

	endTime := args.EndTime
	if endTime == "" {
		endTime = "now"
	}

	statusCodeMin := args.StatusCodeMin
	if statusCodeMin == 0 {
		statusCodeMin = 400
	}

	statusCodeMax := args.StatusCodeMax
	if statusCodeMax == 0 {
		statusCodeMax = 599
	}

	// Build CEL filter for failed operations
	filters := []string{
		fmt.Sprintf("responseStatus.code >= %d", statusCodeMin),
		fmt.Sprintf("responseStatus.code <= %d", statusCodeMax),
	}

	if args.Username != "" {
		filters = append(filters, fmt.Sprintf("user.username == '%s'", args.Username))
	}
	if args.Resource != "" {
		filters = append(filters, fmt.Sprintf("objectRef.resource == '%s'", args.Resource))
	}
	if args.Verb != "" {
		filters = append(filters, fmt.Sprintf("verb == '%s'", args.Verb))
	}

	filter := strings.Join(filters, " && ")

	// Note: Failed operations are queried from audit logs since Activities
	// are only created for successful operations that match ActivityPolicies.
	query := &v1alpha1.AuditLogQuery{
		ObjectMeta: metav1.ObjectMeta{
			GenerateName: "mcp-failed-ops-",
		},
		Spec: v1alpha1.AuditLogQuerySpec{
			StartTime: args.StartTime,
			EndTime:   endTime,
			Filter:    filter,
			Limit:     limit,
		},
	}

	result, err := p.client.AuditLogQueries().Create(ctx, query, metav1.CreateOptions{})
	if err != nil {
		return errorResult(fmt.Sprintf("Query failed: %v", err)), nil, nil
	}

	// Count by status code
	statusCodeCounts := make(map[int]int)
	failures := make([]map[string]any, 0, len(result.Status.Results))

	for _, event := range result.Status.Results {
		code := int(event.ResponseStatus.Code)
		statusCodeCounts[code]++

		failure := map[string]any{
			"timestamp":  event.RequestReceivedTimestamp.UTC().Format(time.RFC3339),
			"user":       event.User.Username,
			"verb":       event.Verb,
			"resource":   event.ObjectRef.Resource,
			"name":       event.ObjectRef.Name,
			"namespace":  event.ObjectRef.Namespace,
			"statusCode": code,
		}

		if event.ResponseStatus.Message != "" {
			failure["message"] = event.ResponseStatus.Message
		}

		failures = append(failures, failure)
	}

	output := map[string]any{
		"count":        len(failures),
		"byStatusCode": statusCodeCounts,
		"failures":     failures,
	}

	return jsonResult(output)
}

// =============================================================================
// Get Resource History
// =============================================================================

// GetResourceHistoryArgs contains the arguments for the get_resource_history tool.
type GetResourceHistoryArgs struct {
	// ResourceUID is the UID of the resource.
	ResourceUID string `json:"resourceUID,omitempty"`

	// APIGroup of the resource.
	APIGroup string `json:"apiGroup,omitempty"`

	// Kind of the resource.
	Kind string `json:"kind,omitempty"`

	// Name of the resource.
	Name string `json:"name,omitempty"`

	// Namespace of the resource.
	Namespace string `json:"namespace,omitempty"`

	// StartTime limits history to after this time.
	StartTime string `json:"startTime,omitempty"`

	// EndTime limits history to before this time.
	EndTime string `json:"endTime,omitempty"`

	// Limit is the maximum number of events to return.
	Limit int `json:"limit,omitempty"`
}

func (p *ToolProvider) handleGetResourceHistory(ctx context.Context, req *mcp.CallToolRequest, args GetResourceHistoryArgs) (*mcp.CallToolResult, any, error) {
	limit := int32(args.Limit)
	if limit == 0 {
		limit = 100
	}

	startTime := args.StartTime
	if startTime == "" {
		startTime = "now-30d"
	}

	endTime := args.EndTime
	if endTime == "" {
		endTime = "now"
	}

	if args.ResourceUID == "" && args.Name == "" {
		return errorResult("Either resourceUID or name is required"), nil, nil
	}

	// Query activities for this resource
	query := &v1alpha1.ActivityQuery{
		ObjectMeta: metav1.ObjectMeta{
			GenerateName: "mcp-resource-history-",
		},
		Spec: v1alpha1.ActivityQuerySpec{
			StartTime: startTime,
			EndTime:   endTime,
			Limit:     limit,
		},
	}

	result, err := p.client.ActivityQueries().Create(ctx, query, metav1.CreateOptions{})
	if err != nil {
		return errorResult(fmt.Sprintf("Query failed: %v", err)), nil, nil
	}

	// Filter by name if specified (ActivityQuery doesn't support name filter directly)
	history := make([]map[string]any, 0, len(result.Status.Results))
	for _, activity := range result.Status.Results {
		// Skip if name filter specified and doesn't match
		if args.Name != "" && activity.Spec.Resource.Name != args.Name {
			continue
		}

		entry := map[string]any{
		"timestamp":    activity.CreationTimestamp.UTC().Format(time.RFC3339),
		"actor":        activity.Spec.Actor.Name,
		"summary":      activity.Spec.Summary,
		"changeSource": activity.Spec.ChangeSource,
		}

		history = append(history, entry)
	}

	// Build resource identifier for output
	resource := map[string]any{
		"name":      args.Name,
		"kind":      args.Kind,
		"apiGroup":  args.APIGroup,
		"namespace": args.Namespace,
	}
	if len(result.Status.Results) > 0 {
		r := result.Status.Results[0].Spec.Resource
		resource["apiGroup"] = r.APIGroup
		resource["kind"] = r.Kind
		resource["name"] = r.Name
		resource["namespace"] = r.Namespace
	}

	output := map[string]any{
		"resource":  resource,
		"count":     len(history),
		"timeRange": map[string]any{"start": result.Status.EffectiveStartTime, "end": result.Status.EffectiveEndTime},
		"history":   history,
	}

	return jsonResult(output)
}

// =============================================================================
// Get User Activity Summary
// =============================================================================

// GetUserActivitySummaryArgs contains the arguments for the get_user_activity_summary tool.
type GetUserActivitySummaryArgs struct {
	// Username is the username or email to get activity for.
	Username string `json:"username,omitempty"`

	// StartTime is the beginning of the time window.
	StartTime string `json:"startTime,omitempty"`

	// EndTime is the end of the time window.
	EndTime string `json:"endTime,omitempty"`

	// IncludeDetails includes individual activities in the response.
	IncludeDetails bool `json:"includeDetails,omitempty"`
}

func (p *ToolProvider) handleGetUserActivitySummary(ctx context.Context, req *mcp.CallToolRequest, args GetUserActivitySummaryArgs) (*mcp.CallToolResult, any, error) {
	if args.Username == "" {
		return errorResult("Username is required"), nil, nil
	}

	startTime := args.StartTime
	if startTime == "" {
		startTime = "now-7d"
	}

	endTime := args.EndTime
	if endTime == "" {
		endTime = "now"
	}

	// Query activities by actor name
	query := &v1alpha1.ActivityQuery{
		ObjectMeta: metav1.ObjectMeta{
			GenerateName: "mcp-user-summary-",
		},
		Spec: v1alpha1.ActivityQuerySpec{
			StartTime: startTime,
			EndTime:   endTime,
			Limit:     1000, // Get more activities for summary
		},
	}

	result, err := p.client.ActivityQueries().Create(ctx, query, metav1.CreateOptions{})
	if err != nil {
		return errorResult(fmt.Sprintf("Query failed: %v", err)), nil, nil
	}

	// Build summary
	changeSourceCounts := make(map[string]int)
	resourceKindCounts := make(map[string]int)
	dayCounts := make(map[string]int)
	var recentActivities []map[string]any

	for i, activity := range result.Status.Results {
		changeSourceCounts[activity.Spec.ChangeSource]++
		resourceKindCounts[activity.Spec.Resource.Kind]++

		day := activity.CreationTimestamp.Format("2006-01-02")
		dayCounts[day]++

		if args.IncludeDetails && i < 20 {
			recentActivities = append(recentActivities, map[string]any{
				"timestamp":    activity.CreationTimestamp.UTC().Format(time.RFC3339),
				"summary":      activity.Spec.Summary,
				"changeSource": activity.Spec.ChangeSource,
				"resource": map[string]any{
					"kind":      activity.Spec.Resource.Kind,
					"name":      activity.Spec.Resource.Name,
					"namespace": activity.Spec.Resource.Namespace,
				},
			})
		}
	}

	// Convert day counts to sorted list
	dayList := make([]map[string]any, 0, len(dayCounts))
	for day, count := range dayCounts {
		dayList = append(dayList, map[string]any{
			"date":  day,
			"count": count,
		})
	}

	output := map[string]any{
		"user": map[string]any{
			"username": args.Username,
		},
		"timeRange": map[string]any{
			"start": result.Status.EffectiveStartTime,
			"end":   result.Status.EffectiveEndTime,
		},
		"totalActivities": len(result.Status.Results),
		"breakdown": map[string]any{
			"byChangeSource": changeSourceCounts,
			"byResourceKind": resourceKindCounts,
			"byDay":          dayList,
		},
	}

	if args.IncludeDetails && len(recentActivities) > 0 {
		output["recentActivities"] = recentActivities
	}

	return jsonResult(output)
}

// =============================================================================
// Get Activity Timeline
// =============================================================================

// GetActivityTimelineArgs contains the arguments for the get_activity_timeline tool.
type GetActivityTimelineArgs struct {
	// StartTime is the beginning of the timeline.
	StartTime string `json:"startTime"`

	// EndTime is the end of the timeline.
	EndTime string `json:"endTime,omitempty"`

	// BucketSize is the time bucket size (hour, day).
	BucketSize string `json:"bucketSize,omitempty"`

	// ChangeSource filters by change source (human, system).
	ChangeSource string `json:"changeSource,omitempty"`
}

func (p *ToolProvider) handleGetActivityTimeline(ctx context.Context, req *mcp.CallToolRequest, args GetActivityTimelineArgs) (*mcp.CallToolResult, any, error) {
	endTime := args.EndTime
	if endTime == "" {
		endTime = "now"
	}

	query := &v1alpha1.ActivityQuery{
		ObjectMeta: metav1.ObjectMeta{
			GenerateName: "mcp-timeline-",
		},
		Spec: v1alpha1.ActivityQuerySpec{
			StartTime: args.StartTime,
			EndTime:   endTime,
			Limit:     1000,
		},
	}

	result, err := p.client.ActivityQueries().Create(ctx, query, metav1.CreateOptions{})
	if err != nil {
		return errorResult(fmt.Sprintf("Query failed: %v", err)), nil, nil
	}

	// Determine bucket size
	bucketFormat := "2006-01-02T15:00:00Z" // hourly default
	bucketSize := args.BucketSize
	if bucketSize == "" {
		bucketSize = "hour"
	}

	if bucketSize == "day" {
		bucketFormat = "2006-01-02T00:00:00Z"
	}

	// Count by bucket
	bucketCounts := make(map[string]int)
	var peakBucket string
	var peakCount int

	for _, activity := range result.Status.Results {
		bucket := activity.CreationTimestamp.Format(bucketFormat)
		bucketCounts[bucket]++

		if bucketCounts[bucket] > peakCount {
			peakCount = bucketCounts[bucket]
			peakBucket = bucket
		}
	}

	// Convert to sorted list
	buckets := make([]map[string]any, 0, len(bucketCounts))
	for bucket, count := range bucketCounts {
		entry := map[string]any{
			"timestamp": bucket,
			"count":     count,
		}
		if bucket == peakBucket {
			entry["note"] = "peak"
		}
		buckets = append(buckets, entry)
	}

	// Calculate average
	var avg float64
	if len(buckets) > 0 {
		avg = float64(len(result.Status.Results)) / float64(len(buckets))
	}

	output := map[string]any{
		"timeRange": map[string]any{
			"start": result.Status.EffectiveStartTime,
			"end":   result.Status.EffectiveEndTime,
		},
		"bucketSize":       bucketSize,
		"totalCount":       len(result.Status.Results),
		"buckets":          buckets,
		"peakBucket":       map[string]any{"timestamp": peakBucket, "count": peakCount},
		"averagePerBucket": avg,
	}

	return jsonResult(output)
}

// =============================================================================
// Summarize Recent Activity
// =============================================================================

// SummarizeRecentActivityArgs contains the arguments for the summarize_recent_activity tool.
type SummarizeRecentActivityArgs struct {
	// StartTime is the beginning of the summary window.
	StartTime string `json:"startTime"`

	// EndTime is the end of the summary window.
	EndTime string `json:"endTime,omitempty"`

	// ChangeSource filters by change source (human, system).
	ChangeSource string `json:"changeSource,omitempty"`

	// TopN is the number of top items per category.
	TopN int `json:"topN,omitempty"`
}

func (p *ToolProvider) handleSummarizeRecentActivity(ctx context.Context, req *mcp.CallToolRequest, args SummarizeRecentActivityArgs) (*mcp.CallToolResult, any, error) {
	endTime := args.EndTime
	if endTime == "" {
		endTime = "now"
	}

	topN := args.TopN
	if topN == 0 {
		topN = 5
	}

	query := &v1alpha1.ActivityQuery{
		ObjectMeta: metav1.ObjectMeta{
			GenerateName: "mcp-summary-",
		},
		Spec: v1alpha1.ActivityQuerySpec{
			StartTime: args.StartTime,
			EndTime:   endTime,
			Limit:     1000,
		},
	}

	result, err := p.client.ActivityQueries().Create(ctx, query, metav1.CreateOptions{})
	if err != nil {
		return errorResult(fmt.Sprintf("Query failed: %v", err)), nil, nil
	}

	// Build summary statistics
	actorCounts := make(map[string]int)
	resourceKindCounts := make(map[string]int)
	var humanChanges, systemChanges int
	var recentSummaries []string

	for i, activity := range result.Status.Results {
		actorCounts[activity.Spec.Actor.Name]++
		resourceKindCounts[activity.Spec.Resource.Kind]++

		// Classify as human or system
		if activity.Spec.ChangeSource == "human" {
			humanChanges++
		} else {
			systemChanges++
		}

		// Collect recent summaries
		if i < topN {
			recentSummaries = append(recentSummaries, activity.Spec.Summary)
		}
	}

	// Get top actors and resources
	topActors := getTopN(actorCounts, topN)
	topResources := getTopN(resourceKindCounts, topN)

	// Build highlights
	highlights := []string{
		fmt.Sprintf("%d total activities (%d human, %d system)", len(result.Status.Results), humanChanges, systemChanges),
	}

	if len(topActors) > 0 {
		highlights = append(highlights, fmt.Sprintf("Most active: %s (%d activities)", topActors[0]["name"], topActors[0]["count"]))
	}

	if len(topResources) > 0 {
		highlights = append(highlights, fmt.Sprintf("Most changed resource type: %s (%d activities)", topResources[0]["name"], topResources[0]["count"]))
	}

	output := map[string]any{
		"timeRange": map[string]any{
			"start": result.Status.EffectiveStartTime,
			"end":   result.Status.EffectiveEndTime,
		},
		"totalActivities": len(result.Status.Results),
		"humanChanges":    humanChanges,
		"systemChanges":   systemChanges,
		"highlights":      highlights,
		"topActors":       topActors,
		"topResources":    topResources,
		"recentSummaries": recentSummaries,
	}

	return jsonResult(output)
}

// =============================================================================
// Compare Activity Periods
// =============================================================================

// CompareActivityPeriodsArgs contains the arguments for the compare_activity_periods tool.
type CompareActivityPeriodsArgs struct {
	// BaselineStart is the start of the baseline period.
	BaselineStart string `json:"baselineStart"`

	// BaselineEnd is the end of the baseline period.
	BaselineEnd string `json:"baselineEnd"`

	// ComparisonStart is the start of the comparison period.
	ComparisonStart string `json:"comparisonStart"`

	// ComparisonEnd is the end of the comparison period.
	ComparisonEnd string `json:"comparisonEnd"`
}

func (p *ToolProvider) handleCompareActivityPeriods(ctx context.Context, req *mcp.CallToolRequest, args CompareActivityPeriodsArgs) (*mcp.CallToolResult, any, error) {
	// Query baseline period
	baselineQuery := &v1alpha1.ActivityQuery{
		ObjectMeta: metav1.ObjectMeta{
			GenerateName: "mcp-compare-baseline-",
		},
		Spec: v1alpha1.ActivityQuerySpec{
			StartTime: args.BaselineStart,
			EndTime:   args.BaselineEnd,
			Limit:     1000,
		},
	}

	baselineResult, err := p.client.ActivityQueries().Create(ctx, baselineQuery, metav1.CreateOptions{})
	if err != nil {
		return errorResult(fmt.Sprintf("Baseline query failed: %v", err)), nil, nil
	}

	// Query comparison period
	comparisonQuery := &v1alpha1.ActivityQuery{
		ObjectMeta: metav1.ObjectMeta{
			GenerateName: "mcp-compare-comparison-",
		},
		Spec: v1alpha1.ActivityQuerySpec{
			StartTime: args.ComparisonStart,
			EndTime:   args.ComparisonEnd,
			Limit:     1000,
		},
	}

	comparisonResult, err := p.client.ActivityQueries().Create(ctx, comparisonQuery, metav1.CreateOptions{})
	if err != nil {
		return errorResult(fmt.Sprintf("Comparison query failed: %v", err)), nil, nil
	}

	// Build counts for both periods
	baselineCounts := buildActivityCounts(baselineResult.Status.Results)
	comparisonCounts := buildActivityCounts(comparisonResult.Status.Results)

	// Find differences
	newInComparison := findNew(baselineCounts.actors, comparisonCounts.actors)
	increasedActivity := findIncreased(baselineCounts.resourceKinds, comparisonCounts.resourceKinds)
	decreasedActivity := findDecreased(baselineCounts.resourceKinds, comparisonCounts.resourceKinds)

	// Calculate change percentage
	var changePercent float64
	if baselineCounts.total > 0 {
		changePercent = float64(comparisonCounts.total-baselineCounts.total) / float64(baselineCounts.total) * 100
	}

	output := map[string]any{
		"baseline": map[string]any{
			"start": baselineResult.Status.EffectiveStartTime,
			"end":   baselineResult.Status.EffectiveEndTime,
			"count": baselineCounts.total,
		},
		"comparison": map[string]any{
			"start": comparisonResult.Status.EffectiveStartTime,
			"end":   comparisonResult.Status.EffectiveEndTime,
			"count": comparisonCounts.total,
		},
		"changePercent":     changePercent,
		"newInComparison":   newInComparison,
		"increasedActivity": increasedActivity,
		"decreasedActivity": decreasedActivity,
	}

	// Add analysis summary
	direction := "more"
	if changePercent < 0 {
		direction = "less"
	}
	analysis := fmt.Sprintf("Comparison period shows %.0f%% %s activity.", absFloat(changePercent), direction)

	if len(newInComparison) > 0 {
		analysis += fmt.Sprintf(" %d new actors appeared.", len(newInComparison))
	}

	output["analysis"] = analysis

	return jsonResult(output)
}

// =============================================================================
// List Activity Policies
// =============================================================================

// ListActivityPoliciesArgs contains the arguments for the list_activity_policies tool.
type ListActivityPoliciesArgs struct {
	// APIGroup filters by resource API group.
	APIGroup string `json:"apiGroup,omitempty"`

	// Kind filters by resource kind.
	Kind string `json:"kind,omitempty"`

	// IncludeRules includes full rule definitions in output.
	IncludeRules bool `json:"includeRules,omitempty"`
}

func (p *ToolProvider) handleListActivityPolicies(ctx context.Context, req *mcp.CallToolRequest, args ListActivityPoliciesArgs) (*mcp.CallToolResult, any, error) {
	result, err := p.client.ActivityPolicies().List(ctx, metav1.ListOptions{})
	if err != nil {
		return errorResult(fmt.Sprintf("Query failed: %v", err)), nil, nil
	}

	policies := make([]map[string]any, 0, len(result.Items))
	for _, policy := range result.Items {
		// Apply filters
		if args.APIGroup != "" && policy.Spec.Resource.APIGroup != args.APIGroup {
			continue
		}
		if args.Kind != "" && policy.Spec.Resource.Kind != args.Kind {
			continue
		}

		policyMap := map[string]any{
			"name": policy.Name,
			"resource": map[string]any{
				"apiGroup": policy.Spec.Resource.APIGroup,
				"kind":     policy.Spec.Resource.Kind,
			},
			"auditRuleCount": len(policy.Spec.AuditRules),
			"eventRuleCount": len(policy.Spec.EventRules),
		}

		// Get status
		status := "Unknown"
		for _, cond := range policy.Status.Conditions {
			if cond.Type == "Ready" {
				if cond.Status == "True" {
					status = "Ready"
				} else {
					status = cond.Reason
				}
				break
			}
		}
		policyMap["status"] = status

		if args.IncludeRules {
			policyMap["auditRules"] = policy.Spec.AuditRules
			policyMap["eventRules"] = policy.Spec.EventRules
		}

		policies = append(policies, policyMap)
	}

	output := map[string]any{
		"policies": policies,
		"summary":  fmt.Sprintf("%d policies covering %d resource types", len(policies), len(policies)),
	}

	return jsonResult(output)
}

// =============================================================================
// Preview Activity Policy
// =============================================================================

// PreviewActivityPolicyArgs contains the arguments for the preview_activity_policy tool.
type PreviewActivityPolicyArgs struct {
	// Policy is the ActivityPolicy spec to test.
	Policy v1alpha1.ActivityPolicySpec `json:"policy"`

	// Inputs are sample audit/event inputs to test.
	// Each element is a JSON object with a "type" field ("audit" or "event")
	// and either an "audit" field (audit log entry) or an "event" field (Kubernetes event).
	Inputs []json.RawMessage `json:"inputs"`
}

func (p *ToolProvider) handlePreviewActivityPolicy(ctx context.Context, req *mcp.CallToolRequest, args PreviewActivityPolicyArgs) (*mcp.CallToolResult, any, error) {
	// Unmarshal inputs from raw JSON into the API type.
	// We use json.RawMessage in the args struct to avoid schema inference failures
	// caused by embedded Kubernetes types (e.g., auditv1.Event) that use
	// time.Time fields which the JSON schema library maps to "string" type,
	// violating the requirement that embedded struct schemas have type "object".
	var inputs []v1alpha1.PolicyPreviewInput
	for i, raw := range args.Inputs {
		var input v1alpha1.PolicyPreviewInput
		if err := json.Unmarshal(raw, &input); err != nil {
			return errorResult(fmt.Sprintf("Invalid input at index %d: %v", i, err)), nil, nil
		}
		inputs = append(inputs, input)
	}

	preview := &v1alpha1.PolicyPreview{
		ObjectMeta: metav1.ObjectMeta{
			GenerateName: "mcp-preview-",
		},
		Spec: v1alpha1.PolicyPreviewSpec{
			Policy: args.Policy,
			Inputs: inputs,
		},
	}

	result, err := p.client.PolicyPreviews().Create(ctx, preview, metav1.CreateOptions{})
	if err != nil {
		return errorResult(fmt.Sprintf("Preview failed: %v", err)), nil, nil
	}

	if result.Status.Error != "" {
		return errorResult(fmt.Sprintf("Preview error: %s", result.Status.Error)), nil, nil
	}

	// Format results
	results := make([]map[string]any, 0, len(result.Status.Results))
	for _, r := range result.Status.Results {
		resultMap := map[string]any{
			"inputIndex": r.InputIndex,
			"matched":    r.Matched,
		}

		if r.Matched {
			resultMap["matchedRule"] = map[string]any{
				"type":  r.MatchedRuleType,
				"index": r.MatchedRuleIndex,
			}
		}

		if r.Error != "" {
			resultMap["error"] = r.Error
		}

		results = append(results, resultMap)
	}

	// Format generated activities
	activities := make([]map[string]any, 0, len(result.Status.Activities))
	for _, a := range result.Status.Activities {
		activities = append(activities, map[string]any{
			"summary": a.Spec.Summary,
			"actor": map[string]any{
				"type": a.Spec.Actor.Type,
				"name": a.Spec.Actor.Name,
			},
			"resource": map[string]any{
				"kind": a.Spec.Resource.Kind,
				"name": a.Spec.Resource.Name,
			},
		})
	}

	output := map[string]any{
		"results":    results,
		"activities": activities,
	}

	return jsonResult(output)
}

// =============================================================================
// Query Events
// =============================================================================

// QueryEventsArgs contains the arguments for the query_events tool.
type QueryEventsArgs struct {
	// StartTime is the beginning of the search window.
	StartTime string `json:"startTime"`

	// EndTime is the end of the search window.
	EndTime string `json:"endTime"`

	// Namespace limits results to events from a specific namespace.
	Namespace string `json:"namespace,omitempty"`

	// RegardingKind filters by the kind of the regarding object.
	RegardingKind string `json:"regardingKind,omitempty"`

	// RegardingName filters by the name of the regarding object.
	RegardingName string `json:"regardingName,omitempty"`

	// Reason filters by event reason.
	Reason string `json:"reason,omitempty"`

	// Type filters by event type (Normal or Warning).
	Type string `json:"type,omitempty"`

	// SourceComponent filters by source component.
	SourceComponent string `json:"sourceComponent,omitempty"`

	// Limit is the maximum number of results to return.
	Limit int `json:"limit,omitempty"`
}

func (p *ToolProvider) handleQueryEvents(ctx context.Context, req *mcp.CallToolRequest, args QueryEventsArgs) (*mcp.CallToolResult, any, error) {
	limit := int32(args.Limit)
	if limit == 0 {
		limit = 100
	}

	// Build field selector for filtering
	var fieldSelectors []string
	if args.RegardingKind != "" {
		fieldSelectors = append(fieldSelectors, fmt.Sprintf("regarding.kind=%s", args.RegardingKind))
	}
	if args.RegardingName != "" {
		fieldSelectors = append(fieldSelectors, fmt.Sprintf("regarding.name=%s", args.RegardingName))
	}
	if args.Reason != "" {
		fieldSelectors = append(fieldSelectors, fmt.Sprintf("reason=%s", args.Reason))
	}
	if args.Type != "" {
		fieldSelectors = append(fieldSelectors, fmt.Sprintf("type=%s", args.Type))
	}
	if args.SourceComponent != "" {
		fieldSelectors = append(fieldSelectors, fmt.Sprintf("source.component=%s", args.SourceComponent))
	}

	fieldSelector := ""
	if len(fieldSelectors) > 0 {
		fieldSelector = strings.Join(fieldSelectors, ",")
	}

	query := &v1alpha1.EventQuery{
		ObjectMeta: metav1.ObjectMeta{
			GenerateName: "mcp-event-query-",
		},
		Spec: v1alpha1.EventQuerySpec{
			StartTime:     args.StartTime,
			EndTime:       args.EndTime,
			Namespace:     args.Namespace,
			FieldSelector: fieldSelector,
			Limit:         limit,
		},
	}

	result, err := p.client.EventQueries().Create(ctx, query, metav1.CreateOptions{})
	if err != nil {
		return errorResult(fmt.Sprintf("Query failed: %v", err)), nil, nil
	}

	// Format results for readability
	// EventRecord wraps eventsv1.Event, so access event data via record.Event
	events := make([]map[string]any, 0, len(result.Status.Results))
	for _, record := range result.Status.Results {
		event := record.Event
		eventMap := map[string]any{
			"name":      event.Name,
			"namespace": event.Namespace,
			"type":      event.Type,
			"reason":    event.Reason,
			"message":   event.Note,
		}

		// eventsv1.Event uses Regarding
		if event.Regarding.Name != "" || event.Regarding.Kind != "" {
			eventMap["regarding"] = map[string]any{
				"kind":      event.Regarding.Kind,
				"name":      event.Regarding.Name,
				"namespace": event.Regarding.Namespace,
			}
		}

		// eventsv1.Event uses ReportingController/ReportingInstance instead of Source
		eventMap["source"] = map[string]any{
			"component": event.ReportingController,
			"host":      event.ReportingInstance,
		}

		// eventsv1.Event uses Series.Count (Series is pointer), otherwise default to 1
		if event.Series != nil {
			eventMap["count"] = event.Series.Count
		} else {
			eventMap["count"] = int32(1)
		}

		// eventsv1 uses EventTime, or Series.LastObservedTime for recurring events
		if event.Series != nil && !event.Series.LastObservedTime.IsZero() {
			eventMap["timestamp"] = event.Series.LastObservedTime.UTC().Format(time.RFC3339)
		} else if !event.EventTime.IsZero() {
			eventMap["timestamp"] = event.EventTime.UTC().Format(time.RFC3339)
		}

		events = append(events, eventMap)
	}

	output := map[string]any{
		"count":              len(events),
		"continue":           result.Status.Continue,
		"effectiveStartTime": result.Status.EffectiveStartTime,
		"effectiveEndTime":   result.Status.EffectiveEndTime,
		"events":             events,
	}

	return jsonResult(output)
}

// =============================================================================
// Get Event Facets
// =============================================================================

// GetEventFacetsArgs contains the arguments for the get_event_facets tool.
type GetEventFacetsArgs struct {
	// Fields to get facets for.
	Fields []string `json:"fields"`

	// StartTime is the beginning of the time window for facet aggregation.
	StartTime string `json:"startTime,omitempty"`

	// EndTime is the end of the time window for facet aggregation.
	EndTime string `json:"endTime,omitempty"`

	// Limit is the maximum number of distinct values per field.
	Limit int `json:"limit,omitempty"`
}

func (p *ToolProvider) handleGetEventFacets(ctx context.Context, req *mcp.CallToolRequest, args GetEventFacetsArgs) (*mcp.CallToolResult, any, error) {
	limit := int32(args.Limit)
	if limit == 0 {
		limit = 20
	}

	startTime := args.StartTime
	if startTime == "" {
		startTime = "now-7d"
	}

	endTime := args.EndTime
	if endTime == "" {
		endTime = "now"
	}

	facetSpecs := make([]v1alpha1.FacetSpec, 0, len(args.Fields))
	for _, field := range args.Fields {
		facetSpecs = append(facetSpecs, v1alpha1.FacetSpec{
			Field: field,
			Limit: limit,
		})
	}

	query := &v1alpha1.EventFacetQuery{
		ObjectMeta: metav1.ObjectMeta{
			GenerateName: "mcp-event-facets-",
		},
		Spec: v1alpha1.EventFacetQuerySpec{
			TimeRange: v1alpha1.FacetTimeRange{
				Start: startTime,
				End:   endTime,
			},
			Facets: facetSpecs,
		},
	}

	result, err := p.client.EventFacetQueries().Create(ctx, query, metav1.CreateOptions{})
	if err != nil {
		return errorResult(fmt.Sprintf("Query failed: %v", err)), nil, nil
	}

	output := make(map[string]any)
	for _, facet := range result.Status.Facets {
		values := make([]map[string]any, 0, len(facet.Values))
		for _, v := range facet.Values {
			values = append(values, map[string]any{
				"value": v.Value,
				"count": v.Count,
			})
		}
		output[facet.Field] = values
	}

	return jsonResult(output)
}

// =============================================================================
// Helper Functions
// =============================================================================

func textResult(text string) *mcp.CallToolResult {
	return &mcp.CallToolResult{
		Content: []mcp.Content{
			&mcp.TextContent{Text: text},
		},
	}
}

func errorResult(message string) *mcp.CallToolResult {
	return &mcp.CallToolResult{
		IsError: true,
		Content: []mcp.Content{
			&mcp.TextContent{Text: message},
		},
	}
}

func jsonResult(output any) (*mcp.CallToolResult, any, error) {
	jsonBytes, err := json.MarshalIndent(output, "", "  ")
	if err != nil {
		return errorResult(fmt.Sprintf("Failed to format results: %v", err)), nil, nil
	}
	return textResult(string(jsonBytes)), nil, nil
}

func isSystemUser(username string) bool {
	return strings.HasPrefix(username, "system:") ||
		strings.Contains(username, "serviceaccount") ||
		strings.Contains(username, "controller")
}

func getTopN(counts map[string]int, n int) []map[string]any {
	// Convert to slice and sort
	type kv struct {
		Key   string
		Value int
	}
	var sorted []kv
	for k, v := range counts {
		sorted = append(sorted, kv{k, v})
	}

	// Sort by count descending
	for i := 0; i < len(sorted)-1; i++ {
		for j := i + 1; j < len(sorted); j++ {
			if sorted[j].Value > sorted[i].Value {
				sorted[i], sorted[j] = sorted[j], sorted[i]
			}
		}
	}

	// Take top N
	result := make([]map[string]any, 0, n)
	for i := 0; i < len(sorted) && i < n; i++ {
		result = append(result, map[string]any{
			"name":  sorted[i].Key,
			"count": sorted[i].Value,
		})
	}
	return result
}

type activityCounts struct {
	total         int
	actors        map[string]int
	resourceKinds map[string]int
	changeSources map[string]int
}

func buildActivityCounts(activities []v1alpha1.Activity) activityCounts {
	counts := activityCounts{
		total:         len(activities),
		actors:        make(map[string]int),
		resourceKinds: make(map[string]int),
		changeSources: make(map[string]int),
	}

	for _, activity := range activities {
		counts.actors[activity.Spec.Actor.Name]++
		counts.resourceKinds[activity.Spec.Resource.Kind]++
		counts.changeSources[activity.Spec.ChangeSource]++
	}

	return counts
}

func findNew(baseline, comparison map[string]int) []map[string]any {
	var result []map[string]any
	for k, v := range comparison {
		if _, exists := baseline[k]; !exists {
			result = append(result, map[string]any{
				"name":  k,
				"count": v,
				"note":  "Not present in baseline",
			})
		}
	}
	return result
}

func findIncreased(baseline, comparison map[string]int) []map[string]any {
	var result []map[string]any
	for k, v := range comparison {
		if baselineV, exists := baseline[k]; exists && v > baselineV {
			changePercent := float64(v-baselineV) / float64(baselineV) * 100
			if changePercent >= 50 { // Only include significant increases
				result = append(result, map[string]any{
					"name":          k,
					"baseline":      baselineV,
					"comparison":    v,
					"changePercent": changePercent,
				})
			}
		}
	}
	return result
}

func findDecreased(baseline, comparison map[string]int) []map[string]any {
	var result []map[string]any
	for k, v := range baseline {
		if compV, exists := comparison[k]; exists && compV < v {
			changePercent := float64(v-compV) / float64(v) * 100
			if changePercent >= 50 { // Only include significant decreases
				result = append(result, map[string]any{
					"name":          k,
					"baseline":      v,
					"comparison":    compV,
					"changePercent": -changePercent,
				})
			}
		}
	}
	return result
}

func absFloat(f float64) float64 {
	if f < 0 {
		return -f
	}
	return f
}
