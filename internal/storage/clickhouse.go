package storage

import (
	"context"
	"crypto/sha256"
	"crypto/tls"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/ClickHouse/clickhouse-go/v2"
	"github.com/ClickHouse/clickhouse-go/v2/lib/driver"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"
	auditv1 "k8s.io/apiserver/pkg/apis/audit/v1"
	"k8s.io/klog/v2"

	"go.miloapis.com/activity/internal/cel"
	"go.miloapis.com/activity/internal/metrics"
	"go.miloapis.com/activity/internal/timeutil"
	"go.miloapis.com/activity/internal/types"
	"go.miloapis.com/activity/pkg/apis/activity/v1alpha1"
)

var tracer = otel.Tracer("activity-clickhouse-storage")

const (
	// cursorTTL limits cursor lifetime to prevent replay attacks and stale queries.
	cursorTTL = 1 * time.Hour
)

// cursorData encodes pagination state and query validation information.
type cursorData struct {
	Timestamp time.Time `json:"t"` // Event timestamp for pagination
	AuditID   string    `json:"a"` // Audit ID for tie-breaking
	QueryHash string    `json:"h"` // Hash of query parameters
	IssuedAt  time.Time `json:"i"` // When cursor was created (for expiration)
}

// hashQueryParams creates a hash to validate cursors are used with matching queries.
// Excludes continueAfter since it changes between pagination requests.
func hashQueryParams(spec v1alpha1.AuditLogQuerySpec) string {
	h := sha256.New()
	h.Write([]byte(spec.StartTime))
	h.Write([]byte("|"))
	h.Write([]byte(spec.EndTime))
	h.Write([]byte("|"))
	h.Write([]byte(spec.Filter))
	h.Write([]byte("|"))
	h.Write([]byte(fmt.Sprintf("%d", spec.Limit)))

	return base64.URLEncoding.EncodeToString(h.Sum(nil)[:16])
}

// encodeCursor creates a base64-encoded pagination token containing position and validation data.
func encodeCursor(timestamp time.Time, auditID string, spec v1alpha1.AuditLogQuerySpec) string {
	data := cursorData{
		Timestamp: timestamp,
		AuditID:   auditID,
		QueryHash: hashQueryParams(spec),
		IssuedAt:  time.Now(),
	}

	jsonData, _ := json.Marshal(data)
	return base64.URLEncoding.EncodeToString(jsonData)
}

// ValidateCursor checks if a cursor is valid for the given query spec without extracting data.
// This is called by the API layer during validation to provide early feedback.
// Returns an error if the cursor is malformed, expired, or doesn't match the query parameters.
func ValidateCursor(cursor string, spec v1alpha1.AuditLogQuerySpec) error {
	_, _, err := decodeCursor(cursor, spec)
	return err
}

// decodeCursor validates and extracts pagination state from a cursor token.
// Returns an error if the cursor is malformed, expired, or doesn't match the current query.
func decodeCursor(cursor string, spec v1alpha1.AuditLogQuerySpec) (time.Time, string, error) {
	decoded, err := base64.URLEncoding.DecodeString(cursor)
	if err != nil {
		return time.Time{}, "", fmt.Errorf("cannot decode pagination cursor: %w", err)
	}

	var data cursorData
	if err := json.Unmarshal(decoded, &data); err != nil {
		return time.Time{}, "", fmt.Errorf("cursor format is invalid. Start a new query")
	}

	currentHash := hashQueryParams(spec)
	if data.QueryHash != currentHash {
		return time.Time{}, "", fmt.Errorf("cannot use cursor because query parameters changed. Start a new query without the continueAfter parameter")
	}

	if data.IssuedAt.IsZero() {
		return time.Time{}, "", fmt.Errorf("cursor format is invalid. Start a new query")
	}

	age := time.Since(data.IssuedAt)
	if age > cursorTTL {
		return time.Time{}, "", fmt.Errorf("cursor expired after %v. Cursors are valid for %v. Start a new query without the continueAfter parameter",
			age.Round(time.Second),
			cursorTTL,
		)
	}

	return data.Timestamp, data.AuditID, nil
}

// ClickHouseConfig configures the ClickHouse connection and query limits.
type ClickHouseConfig struct {
	Address  string
	Database string
	Username string
	Password string

	// TLS configuration (optional - disabled by default)
	TLSEnabled  bool   // Enable TLS for ClickHouse connection
	TLSCertFile string // Path to client certificate file
	TLSKeyFile  string // Path to client key file
	TLSCAFile   string // Path to CA certificate file

	MaxQueryWindow time.Duration // Maximum allowed time range for queries
	MaxPageSize    int32         // Maximum results per page
}

// ClickHouseStorage implements audit log storage using ClickHouse.
type ClickHouseStorage struct {
	conn   driver.Conn
	config ClickHouseConfig
}

// NewClickHouseStorage establishes a connection to ClickHouse and validates connectivity.
func NewClickHouseStorage(config ClickHouseConfig) (*ClickHouseStorage, error) {
	options := &clickhouse.Options{
		Addr: []string{config.Address},
		Auth: clickhouse.Auth{
			Database: config.Database,
			Username: config.Username,
			Password: config.Password,
		},
		Settings: clickhouse.Settings{
			"max_execution_time": 60,
		},
		DialTimeout: 5 * time.Second,
		Compression: &clickhouse.Compression{
			Method: clickhouse.CompressionLZ4,
		},
	}

	// Configure TLS if enabled
	if config.TLSEnabled {
		tlsConfig, err := loadTLSConfig(config)
		if err != nil {
			return nil, fmt.Errorf("failed to load TLS configuration: %w", err)
		}
		options.TLS = tlsConfig
		klog.V(2).Info("ClickHouse TLS enabled")
	}

	conn, err := clickhouse.Open(options)
	if err != nil {
		return nil, fmt.Errorf("failed to connect to ClickHouse: %w", err)
	}

	if err := conn.Ping(context.Background()); err != nil {
		return nil, fmt.Errorf("failed to ping ClickHouse: %w", err)
	}

	return &ClickHouseStorage{
		conn:   conn,
		config: config,
	}, nil
}

// loadTLSConfig loads TLS certificates and creates a tls.Config for ClickHouse connection.
func loadTLSConfig(config ClickHouseConfig) (*tls.Config, error) {
	tlsConfig := &tls.Config{}

	// Load client certificate and key if provided
	if config.TLSCertFile != "" && config.TLSKeyFile != "" {
		cert, err := tls.LoadX509KeyPair(config.TLSCertFile, config.TLSKeyFile)
		if err != nil {
			return nil, fmt.Errorf("failed to load client certificate: %w", err)
		}
		tlsConfig.Certificates = []tls.Certificate{cert}
		klog.V(2).Infof("Loaded client certificate from %s", config.TLSCertFile)
	}

	// Load CA certificate if provided
	if config.TLSCAFile != "" {
		caCert, err := os.ReadFile(config.TLSCAFile)
		if err != nil {
			return nil, fmt.Errorf("failed to read CA certificate: %w", err)
		}

		caCertPool := x509.NewCertPool()
		if !caCertPool.AppendCertsFromPEM(caCert) {
			return nil, fmt.Errorf("failed to parse CA certificate")
		}
		tlsConfig.RootCAs = caCertPool
		klog.V(2).Infof("Loaded CA certificate from %s", config.TLSCAFile)
	}

	return tlsConfig, nil
}

func (s *ClickHouseStorage) Close() error {
	if s.conn != nil {
		return s.conn.Close()
	}
	return nil
}

// Conn returns the underlying ClickHouse connection.
func (s *ClickHouseStorage) Conn() driver.Conn {
	return s.conn
}

// Config returns the ClickHouse configuration.
func (s *ClickHouseStorage) Config() ClickHouseConfig {
	return s.config
}

func (s *ClickHouseStorage) GetMaxQueryWindow() time.Duration {
	return s.config.MaxQueryWindow
}

func (s *ClickHouseStorage) GetMaxPageSize() int32 {
	return s.config.MaxPageSize
}

// QueryResult contains audit events and pagination state.
type QueryResult struct {
	Events   []auditv1.Event
	Continue string
}

// ScopeContext defines the hierarchical scope boundary for audit log queries.
type ScopeContext struct {
	Type string // "platform", "Organization", "Project", "User" (PascalCase for K8s Kind convention)
	Name string // scope identifier (org name, project name, etc.)
}

// QueryAuditLogs retrieves audit logs matching the query specification and scope.
// The spec parameter must be pre-validated by the API layer.
func (s *ClickHouseStorage) QueryAuditLogs(ctx context.Context, spec v1alpha1.AuditLogQuerySpec, scope ScopeContext) (*QueryResult, error) {
	ctx, span := tracer.Start(ctx, "clickhouse.query",
		trace.WithSpanKind(trace.SpanKindClient),
		trace.WithAttributes(
			attribute.String("db.system", "clickhouse"),
			attribute.String("db.name", s.config.Database),
			attribute.String("db.operation", "SELECT"),
			attribute.Int("query.limit", int(spec.Limit)),
			attribute.String("query.filter", spec.Filter),
			attribute.String("query.start_time", spec.StartTime),
			attribute.String("query.end_time", spec.EndTime),
		),
	)
	defer span.End()

	// Start timing the overall query operation
	overallStartTime := time.Now()

	query, args, err := s.buildQuery(ctx, spec, scope)
	if err != nil {
		metrics.ClickHouseQueryErrors.WithLabelValues("build_query").Inc()
		span.RecordError(err)
		span.SetStatus(codes.Error, "failed to build query")
		// Return the error directly - buildQuery returns user-friendly validation errors
		return nil, err
	}

	klog.V(3).InfoS("Built ClickHouse query",
		"query", query,
		"argsCount", len(args),
	)

	// Add SQL statement to span (truncated if too long)
	truncatedQuery := query
	if len(query) > 1000 {
		truncatedQuery = query[:1000] + "..."
	}
	span.SetAttributes(attribute.String("db.statement", truncatedQuery))

	// Add trace context as SQL comment for correlation
	spanContext := span.SpanContext()
	if spanContext.IsValid() {
		traceparent := fmt.Sprintf("00-%s-%s-%02x",
			spanContext.TraceID().String(),
			spanContext.SpanID().String(),
			spanContext.TraceFlags())
		query = fmt.Sprintf("/* traceparent: %s */ %s", traceparent, query)
	}

	// Extract trace ID for logging
	traceID := span.SpanContext().TraceID().String()
	spanID := span.SpanContext().SpanID().String()

	klog.InfoS("Executing ClickHouse query",
		"traceID", traceID,
		"spanID", spanID,
		"filter", spec.Filter,
		"limit", spec.Limit,
		"continue", spec.Continue,
		"query", truncatedQuery,
	)

	// Time the actual ClickHouse query execution
	queryStartTime := time.Now()
	rows, err := s.conn.Query(ctx, query, args...)
	queryDuration := time.Since(queryStartTime).Seconds()

	if err != nil {
		metrics.ClickHouseQueryDuration.WithLabelValues("query").Observe(queryDuration)
		metrics.ClickHouseQueryTotal.WithLabelValues("error").Inc()

		// Classify error type
		errorType := "unknown"
		if strings.Contains(err.Error(), "connection") {
			errorType = "connection"
		} else if strings.Contains(err.Error(), "timeout") {
			errorType = "timeout"
		} else if strings.Contains(err.Error(), "syntax") {
			errorType = "syntax"
		} else if strings.Contains(err.Error(), "memory") {
			errorType = "memory"
		} else if strings.Contains(err.Error(), "parameter") {
			errorType = "parameter"
		}
		metrics.ClickHouseQueryErrors.WithLabelValues(errorType).Inc()

		// Record error in span
		span.RecordError(err)
		span.SetStatus(codes.Error, "query execution failed")
		span.SetAttributes(attribute.String("error.type", errorType))

		// Log detailed error with trace context and query details
		klog.ErrorS(err, "ClickHouse query failed",
			"traceID", traceID,
			"spanID", spanID,
			"errorType", errorType,
			"filter", spec.Filter,
			"limit", spec.Limit,
			"continue", spec.Continue,
			"duration", queryDuration,
			"query", truncatedQuery,
		)

		return nil, fmt.Errorf("unable to retrieve audit logs. Try again or contact support if the problem persists")
	}
	defer rows.Close()

	// Record successful query execution time
	metrics.ClickHouseQueryDuration.WithLabelValues("query").Observe(queryDuration)
	span.SetAttributes(attribute.Float64("db.query_duration_seconds", queryDuration))

	// Determine the limit
	limit := spec.Limit
	if limit <= 0 {
		limit = 100
	}
	if limit > s.config.MaxPageSize {
		limit = s.config.MaxPageSize
	}

	var events []auditv1.Event
	var unmarshalErrors int
	for rows.Next() {
		var eventJSON string
		if err := rows.Scan(&eventJSON); err != nil {
			klog.ErrorS(err, "Failed to scan row",
				"traceID", traceID,
				"spanID", spanID,
			)
			return nil, fmt.Errorf("unable to retrieve audit logs. Try again or contact support if the problem persists")
		}

		var event auditv1.Event
		if err := json.Unmarshal([]byte(eventJSON), &event); err != nil {
			unmarshalErrors++
			klog.ErrorS(err, "Failed to unmarshal audit event",
				"traceID", traceID,
				"spanID", spanID,
			)
			continue
		}

		events = append(events, event)
	}

	if err := rows.Err(); err != nil {
		metrics.ClickHouseQueryTotal.WithLabelValues("error").Inc()
		metrics.ClickHouseQueryErrors.WithLabelValues("iteration").Inc()

		klog.ErrorS(err, "Error iterating ClickHouse rows",
			"traceID", traceID,
			"spanID", spanID,
			"filter", spec.Filter,
			"limit", spec.Limit,
		)

		return nil, fmt.Errorf("unable to retrieve audit logs. Try again or contact support if the problem persists")
	}

	if unmarshalErrors > 0 {
		klog.InfoS("Query completed with unmarshal errors",
			"traceID", traceID,
			"spanID", spanID,
			"unmarshalErrors", unmarshalErrors,
			"successfulEvents", len(events),
		)
	}

	// Check if we have more results (we fetched limit+1)
	var continueAfter string
	if int32(len(events)) > limit {
		events = events[:limit]
		if len(events) > 0 {
			lastEvent := events[len(events)-1]
			continueAfter = encodeCursor(lastEvent.StageTimestamp.Time, string(lastEvent.AuditID), spec)
		}
	}

	// Record successful query metrics
	metrics.ClickHouseQueryTotal.WithLabelValues("success").Inc()
	metrics.AuditLogQueryResults.Observe(float64(len(events)))

	// Record end-to-end query duration (includes result processing)
	totalDuration := time.Since(overallStartTime).Seconds()
	metrics.ClickHouseQueryDuration.WithLabelValues("total").Observe(totalDuration)

	// Add result metrics to span
	span.SetAttributes(
		attribute.Int("db.rows_returned", len(events)),
		attribute.Bool("query.has_more", continueAfter != ""),
		attribute.Float64("db.total_duration_seconds", totalDuration),
	)
	span.SetStatus(codes.Ok, "query successful")

	// Log successful query completion
	klog.InfoS("ClickHouse query completed successfully",
		"traceID", traceID,
		"spanID", spanID,
		"rowsReturned", len(events),
		"hasMore", continueAfter != "",
		"queryDuration", queryDuration,
		"totalDuration", totalDuration,
		"filter", spec.Filter,
		"limit", spec.Limit,
	)

	return &QueryResult{
		Events:   events,
		Continue: continueAfter,
	}, nil
}

// hasUserFilter checks if the CEL filter contains user-based filtering
func hasUserFilter(filter string) bool {
	if filter == "" {
		return false
	}
	// Check for common user filter patterns in CEL expressions
	// This is a heuristic - doesn't need to be perfect, just helpful for optimization
	return strings.Contains(filter, "user.username") ||
		strings.Contains(filter, "user.groups") ||
		strings.Contains(filter, "user.uid") ||
		// Also match if someone uses the materialized column directly
		(strings.Contains(filter, "user") && (strings.Contains(filter, "==") || strings.Contains(filter, "!=")))
}

// hasAPIGroupFilter checks if the CEL filter expression contains API group fields.
// Added for future use — not currently wired into buildActivityQuery.
func hasAPIGroupFilter(filter string) bool {
	if filter == "" {
		return false
	}
	return strings.Contains(filter, "resource.apiGroup") ||
		strings.Contains(filter, "api_group")
}

// buildQuery constructs a ClickHouse SQL query from the query spec
func (s *ClickHouseStorage) buildQuery(ctx context.Context, spec v1alpha1.AuditLogQuerySpec, scope ScopeContext) (string, []interface{}, error) {
	var args []interface{}

	query := fmt.Sprintf("SELECT event_json FROM %s.audit_logs", s.config.Database)

	var conditions []string

	// Only add scope filters if not platform-wide query
	if scope.Type != types.TenantTypePlatform {
		if scope.Type == types.TenantTypeUser {
			// For user scope, filter by user.uid instead of scope annotations.
			// This allows querying all activity performed BY a specific user
			// across all organizations and projects on the platform.
			conditions = append(conditions, "user_uid = ?")
			args = append(args, scope.Name)
		} else {
			// For organization/project scope, use the scope annotations
			conditions = append(conditions, "scope_type = ?")
			args = append(args, scope.Type)

			conditions = append(conditions, "scope_name = ?")
			args = append(args, scope.Name)
		}
	}

	// Use a single reference time for both timestamps to prevent sub-second drift
	// when using relative times like "now-7d" and "now"
	now := time.Now()

	if spec.StartTime != "" {
		startTime, err := timeutil.ParseFlexibleTime(spec.StartTime, now)
		if err != nil {
			return "", nil, fmt.Errorf("invalid startTime: %w", err)
		}
		conditions = append(conditions, "timestamp >= ?")
		args = append(args, startTime)
	}

	if spec.EndTime != "" {
		endTime, err := timeutil.ParseFlexibleTime(spec.EndTime, now)
		if err != nil {
			return "", nil, fmt.Errorf("invalid endTime: %w", err)
		}
		conditions = append(conditions, "timestamp < ?")
		args = append(args, endTime)
	}

	if spec.Filter != "" {
		celWhere, celArgs, err := cel.ConvertToClickHouseSQL(ctx, spec.Filter)
		if err != nil {
			// Return the error directly - it already has user-friendly messaging
			return "", nil, err
		}
		if celWhere != "" {
			processedWhere := celWhere
			for i := range celArgs {
				oldParam := fmt.Sprintf("{arg%d}", i+1)
				processedWhere = strings.ReplaceAll(processedWhere, oldParam, "?")
			}
			args = append(args, celArgs...)
			conditions = append(conditions, processedWhere)
		}
	}

	// Cursor pagination using timestamp and audit_id.
	// Since timestamp is the second sort key (after toStartOfHour), we need to handle
	// both hour boundaries and exact timestamps for correct pagination.
	if spec.Continue != "" {
		cursorTime, cursorAuditID, err := decodeCursor(spec.Continue, spec)
		if err != nil {
			return "", nil, err
		}

		// Pagination logic: continue from where we left off
		// 1. Hour bucket is earlier, OR
		// 2. Same hour bucket but timestamp is earlier, OR
		// 3. Same timestamp but audit_id is earlier (for tie-breaking)
		conditions = append(conditions, "(toStartOfHour(timestamp) < toStartOfHour(?) OR (toStartOfHour(timestamp) = toStartOfHour(?) AND timestamp < ?) OR (timestamp = ? AND audit_id < ?))")
		args = append(args, cursorTime, cursorTime, cursorTime, cursorTime, cursorAuditID)
	}

	if len(conditions) > 0 {
		query += " WHERE " + strings.Join(conditions, " AND ")
	}

	// ORDER BY must match projection/primary key sort order for ClickHouse
	// to efficiently use indexes and projections.
	// Timestamp is second to ensure strict chronological ordering within each hour.
	if scope.Type == "platform" {
		if hasUserFilter(spec.Filter) {
			// User filter present: use user_query_projection
			query += " ORDER BY toStartOfHour(timestamp) DESC, timestamp DESC, user DESC, api_group DESC, resource DESC, audit_id DESC"
		} else {
			// No user filter: use platform_query_projection
			query += " ORDER BY toStartOfHour(timestamp) DESC, timestamp DESC, api_group DESC, resource DESC, audit_id DESC"
		}
	} else if scope.Type == types.TenantTypeUser {
		// User-scoped: use user_uid_query_projection to filter by UID
		query += " ORDER BY toStartOfHour(timestamp) DESC, timestamp DESC, user_uid DESC, api_group DESC, resource DESC, audit_id DESC"
	} else {
		// Tenant-scoped: match hour-bucketed primary key for efficient index use
		query += " ORDER BY toStartOfHour(timestamp) DESC, timestamp DESC, scope_type DESC, scope_name DESC, user DESC, audit_id DESC"
	}

	limit := spec.Limit
	if limit <= 0 {
		limit = 100
	}
	if limit > s.config.MaxPageSize {
		limit = s.config.MaxPageSize
	}

	query += fmt.Sprintf(" LIMIT %d", limit+1)

	return query, args, nil
}

// ActivityQuerySpec defines the query parameters for listing activities.
type ActivityQuerySpec struct {
	// StartTime filters activities to those after this time.
	StartTime string

	// EndTime filters activities to those before this time.
	EndTime string

	// Search performs full-text search on summaries.
	Search string

	// Filter is a CEL expression for advanced filtering.
	// This is the sole filtering mechanism beyond time range and full-text search.
	Filter string

	// Limit is the maximum number of results to return.
	Limit int32

	// Continue is the pagination cursor.
	Continue string
}

// ActivityQueryResult contains activities and pagination state.
type ActivityQueryResult struct {
	Activities []string // JSON activity records
	Continue   string
}

// QueryActivities retrieves activities matching the query specification and scope.
func (s *ClickHouseStorage) QueryActivities(ctx context.Context, spec ActivityQuerySpec, scope ScopeContext) (*ActivityQueryResult, error) {
	ctx, span := tracer.Start(ctx, "clickhouse.query_activities",
		trace.WithSpanKind(trace.SpanKindClient),
		trace.WithAttributes(
			attribute.String("db.system", "clickhouse"),
			attribute.String("db.name", s.config.Database),
			attribute.String("db.operation", "SELECT"),
			attribute.Int("query.limit", int(spec.Limit)),
		),
	)
	defer span.End()

	query, args, err := s.buildActivityQuery(ctx, spec, scope)
	if err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, "failed to build query")
		// Return the error directly - buildActivityQuery returns user-friendly validation errors
		return nil, err
	}

	klog.V(3).InfoS("Built activities ClickHouse query",
		"query", query,
		"argsCount", len(args),
	)

	// Add trace context
	spanContext := span.SpanContext()
	if spanContext.IsValid() {
		traceparent := fmt.Sprintf("00-%s-%s-%02x",
			spanContext.TraceID().String(),
			spanContext.SpanID().String(),
			spanContext.TraceFlags())
		query = fmt.Sprintf("/* traceparent: %s */ %s", traceparent, query)
	}

	rows, err := s.conn.Query(ctx, query, args...)
	if err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, "query execution failed")
		klog.ErrorS(err, "Failed to query activities")
		return nil, fmt.Errorf("unable to retrieve activities. Try again or contact support if the problem persists")
	}
	defer rows.Close()

	limit := spec.Limit
	if limit <= 0 {
		limit = 100
	}
	if limit > s.config.MaxPageSize {
		limit = s.config.MaxPageSize
	}

	var activities []string
	for rows.Next() {
		var activityJSON string
		if err := rows.Scan(&activityJSON); err != nil {
			klog.ErrorS(err, "Failed to scan activity row")
			return nil, fmt.Errorf("unable to retrieve activities. Try again or contact support if the problem persists")
		}
		activities = append(activities, activityJSON)
	}

	if err := rows.Err(); err != nil {
		klog.ErrorS(err, "Error iterating activity rows")
		return nil, fmt.Errorf("unable to retrieve activities. Try again or contact support if the problem persists")
	}

	// Check for more results
	var continueToken string
	if int32(len(activities)) > limit {
		activities = activities[:limit]
		// Create continue token from last activity timestamp
		if len(activities) > 0 {
			continueToken = encodeActivityCursor(activities[len(activities)-1], spec)
		}
	}

	span.SetAttributes(
		attribute.Int("db.rows_returned", len(activities)),
		attribute.Bool("query.has_more", continueToken != ""),
	)
	span.SetStatus(codes.Ok, "query successful")

	return &ActivityQueryResult{
		Activities: activities,
		Continue:   continueToken,
	}, nil
}

// buildActivityQuery constructs a ClickHouse SQL query for activities.
func (s *ClickHouseStorage) buildActivityQuery(ctx context.Context, spec ActivityQuerySpec, scope ScopeContext) (string, []interface{}, error) {
	var args []interface{}
	query := fmt.Sprintf("SELECT activity_json FROM %s.activities", s.config.Database)

	var conditions []string

	// Scope filtering
	if scope.Type != types.TenantTypePlatform {
		if scope.Type == types.TenantTypeUser {
			// For user scope, filter by actor_uid to show activities performed by this user
			// across all organizations and projects
			conditions = append(conditions, "actor_uid = ?")
			args = append(args, scope.Name)
		} else {
			// For organization/project scope, filter by tenant
			conditions = append(conditions, "tenant_type = ?")
			args = append(args, scope.Type)
			conditions = append(conditions, "tenant_name = ?")
			args = append(args, scope.Name)
		}
	}

	// Time range
	now := time.Now()
	if spec.StartTime != "" {
		startTime, err := timeutil.ParseFlexibleTime(spec.StartTime, now)
		if err != nil {
			return "", nil, fmt.Errorf("invalid startTime: %w", err)
		}
		conditions = append(conditions, "timestamp >= ?")
		args = append(args, startTime)
	}

	if spec.EndTime != "" {
		endTime, err := timeutil.ParseFlexibleTime(spec.EndTime, now)
		if err != nil {
			return "", nil, fmt.Errorf("invalid endTime: %w", err)
		}
		conditions = append(conditions, "timestamp < ?")
		args = append(args, endTime)
	}

	// Full-text search on summary (substring matching, case-insensitive)
	if spec.Search != "" {
		// Split search into terms and match any term as a substring
		terms := strings.Fields(spec.Search)
		if len(terms) > 0 {
			conditions = append(conditions, "multiSearchAnyCaseInsensitive(summary, ?) > 0")
			args = append(args, terms)
		}
	}

	// CEL filter expression — the sole filtering mechanism beyond time range and search
	if spec.Filter != "" {
		celWhere, celArgs, err := cel.ConvertActivityToClickHouseSQL(ctx, spec.Filter)
		if err != nil {
			return "", nil, err
		}
		if celWhere != "" {
			processedWhere := celWhere
			for i := range celArgs {
				oldParam := fmt.Sprintf("{arg%d}", i+1)
				processedWhere = strings.ReplaceAll(processedWhere, oldParam, "?")
			}
			args = append(args, celArgs...)
			conditions = append(conditions, processedWhere)
		}
	}

	// Pagination cursor aligned with the time-bucketed ORDER BY below.
	// The 3-level toStartOfHour pattern ensures correct pagination across hour boundaries.
	if spec.Continue != "" {
		cursorTime, cursorName, err := decodeActivityCursor(spec.Continue, spec)
		if err != nil {
			return "", nil, err
		}
		// Pagination logic: continue from where we left off
		// 1. Hour bucket is earlier, OR
		// 2. Same hour bucket but timestamp is earlier, OR
		// 3. Same timestamp but activity_name is earlier (for tie-breaking)
		conditions = append(conditions, "(toStartOfHour(timestamp) < toStartOfHour(?) OR (toStartOfHour(timestamp) = toStartOfHour(?) AND timestamp < ?) OR (timestamp = ? AND activity_name < ?))")
		args = append(args, cursorTime, cursorTime, cursorTime, cursorTime, cursorName)
	}

	if len(conditions) > 0 {
		query += " WHERE " + strings.Join(conditions, " AND ")
	}

	// The time prefix matches the primary key and every projection, keeping
	// read-in-order. Ties break on the cursor's column; anything else can skip
	// or repeat rows at a page boundary.
	query += " ORDER BY toStartOfHour(timestamp) DESC, timestamp DESC, activity_name DESC"

	// Limit
	limit := spec.Limit
	if limit <= 0 {
		limit = 100
	}
	if limit > s.config.MaxPageSize {
		limit = s.config.MaxPageSize
	}
	query += fmt.Sprintf(" LIMIT %d", limit+1)

	return query, args, nil
}

// activityCursorData encodes pagination state for activity queries.
type activityCursorData struct {
	Timestamp time.Time `json:"t"`
	// Name has a new key so legacy resource-UID tokens fail validation.
	Name      string    `json:"n"`
	QueryHash string    `json:"h"`
	IssuedAt  time.Time `json:"i"`
}

// hashActivityQueryParams creates a hash to validate cursors.
func hashActivityQueryParams(spec ActivityQuerySpec) string {
	h := sha256.New()
	h.Write([]byte(spec.StartTime))
	h.Write([]byte("|"))
	h.Write([]byte(spec.EndTime))
	h.Write([]byte("|"))
	h.Write([]byte(spec.Filter))
	h.Write([]byte("|"))
	h.Write([]byte(spec.Search))
	h.Write([]byte("|"))
	h.Write([]byte(fmt.Sprintf("%d", spec.Limit)))

	return base64.URLEncoding.EncodeToString(h.Sum(nil)[:16])
}

// encodeActivityCursor creates a pagination token from the last activity.
func encodeActivityCursor(lastActivityJSON string, spec ActivityQuerySpec) string {
	var activity struct {
		Metadata struct {
			Name              string `json:"name"`
			CreationTimestamp string `json:"creationTimestamp"`
		} `json:"metadata"`
	}

	if err := json.Unmarshal([]byte(lastActivityJSON), &activity); err != nil {
		return ""
	}

	timestamp, _ := time.Parse(time.RFC3339, activity.Metadata.CreationTimestamp)

	data := activityCursorData{
		Timestamp: timestamp,
		Name:      activity.Metadata.Name,
		QueryHash: hashActivityQueryParams(spec),
		IssuedAt:  time.Now(),
	}

	jsonData, _ := json.Marshal(data)
	return base64.URLEncoding.EncodeToString(jsonData)
}

// decodeActivityCursor validates and extracts pagination state.
func decodeActivityCursor(cursor string, spec ActivityQuerySpec) (time.Time, string, error) {
	decoded, err := base64.URLEncoding.DecodeString(cursor)
	if err != nil {
		return time.Time{}, "", fmt.Errorf("the continue token is invalid. Remove the continue parameter to start a new query")
	}

	var data activityCursorData
	if err := json.Unmarshal(decoded, &data); err != nil || data.Name == "" {
		return time.Time{}, "", fmt.Errorf("the continue token is invalid. Remove the continue parameter to start a new query")
	}

	currentHash := hashActivityQueryParams(spec)
	if data.QueryHash != currentHash {
		return time.Time{}, "", fmt.Errorf("query parameters changed since the continue token was issued. Remove the continue parameter and use consistent query parameters when paginating")
	}

	if time.Since(data.IssuedAt) > cursorTTL {
		return time.Time{}, "", fmt.Errorf("the continue token expired after %v. Tokens are valid for %v. Remove the continue parameter to start a new query",
			time.Since(data.IssuedAt).Round(time.Second),
			cursorTTL,
		)
	}

	return data.Timestamp, data.Name, nil
}

// FacetFieldSpec defines a single facet field to query.
type FacetFieldSpec struct {
	Field string
	Limit int32
}

// FacetQueryResult contains the results of a facet query.
type FacetQueryResult struct {
	Facets []FacetFieldResult
}

// FacetFieldResult contains the distinct values for a single facet.
type FacetFieldResult struct {
	Field  string
	Values []FacetValueResult
}

// FacetValueResult represents a single distinct value with its count.
type FacetValueResult struct {
	Value string
	Count int64
}

// AuditLogFacetQuerySpec defines the parameters for an audit log facet query.
type AuditLogFacetQuerySpec struct {
	// TimeRange specifies the time window for facet aggregation.
	StartTime string
	EndTime   string

	// Filter is a CEL expression to filter audit logs before computing facets.
	Filter string

	// Facets are the fields to compute distinct values for.
	Facets []FacetFieldSpec
}

// QueryAuditLogFacets retrieves distinct field values with counts for audit log faceted search.
func (s *ClickHouseStorage) QueryAuditLogFacets(ctx context.Context, spec AuditLogFacetQuerySpec, scope ScopeContext) (*FacetQueryResult, error) {
	ctx, span := tracer.Start(ctx, "clickhouse.query_audit_log_facets",
		trace.WithSpanKind(trace.SpanKindClient),
		trace.WithAttributes(
			attribute.String("db.system", "clickhouse"),
			attribute.String("db.name", s.config.Database),
			attribute.String("db.operation", "SELECT"),
			attribute.Int("facet.count", len(spec.Facets)),
		),
	)
	defer span.End()

	result := &FacetQueryResult{
		Facets: make([]FacetFieldResult, 0, len(spec.Facets)),
	}

	// Execute each facet query
	for _, facet := range spec.Facets {
		facetResult, err := s.queryAuditLogFacet(ctx, facet, spec, scope)
		if err != nil {
			span.RecordError(err)
			klog.ErrorS(err, "Failed to query audit log facet", "field", facet.Field)
			// Return the error directly - queryAuditLogFacet returns user-friendly validation errors
			return nil, err
		}
		result.Facets = append(result.Facets, *facetResult)
	}

	span.SetStatus(codes.Ok, "audit log facet query successful")
	return result, nil
}

// queryAuditLogFacet executes a single facet query against the audit logs table.
func (s *ClickHouseStorage) queryAuditLogFacet(ctx context.Context, facet FacetFieldSpec, spec AuditLogFacetQuerySpec, scope ScopeContext) (*FacetFieldResult, error) {
	column, err := GetAuditLogFacetColumn(facet.Field)
	if err != nil {
		return nil, err
	}

	limit := facet.Limit
	if limit <= 0 {
		limit = 20
	}
	if limit > 100 {
		limit = 100
	}

	var args []interface{}
	var conditions []string

	// Scope filtering - same pattern as audit log queries
	if scope.Type != types.TenantTypePlatform {
		if scope.Type == types.TenantTypeUser {
			// For user scope, filter by user_uid
			conditions = append(conditions, "user_uid = ?")
			args = append(args, scope.Name)
		} else {
			// For organization/project scope, use the scope annotations
			conditions = append(conditions, "scope_type = ?")
			args = append(args, scope.Type)
			conditions = append(conditions, "scope_name = ?")
			args = append(args, scope.Name)
		}
	}

	// Time range
	now := time.Now()
	if spec.StartTime != "" {
		startTime, err := timeutil.ParseFlexibleTime(spec.StartTime, now)
		if err != nil {
			return nil, fmt.Errorf("invalid startTime: %w", err)
		}
		conditions = append(conditions, "timestamp >= ?")
		args = append(args, startTime)
	}

	if spec.EndTime != "" {
		endTime, err := timeutil.ParseFlexibleTime(spec.EndTime, now)
		if err != nil {
			return nil, fmt.Errorf("invalid endTime: %w", err)
		}
		conditions = append(conditions, "timestamp < ?")
		args = append(args, endTime)
	}

	// CEL filter (optional)
	if spec.Filter != "" {
		celWhere, celArgs, err := cel.ConvertToClickHouseSQL(ctx, spec.Filter)
		if err != nil {
			return nil, err
		}
		if celWhere != "" {
			processedWhere := celWhere
			for i := range celArgs {
				oldParam := fmt.Sprintf("{arg%d}", i+1)
				processedWhere = strings.ReplaceAll(processedWhere, oldParam, "?")
			}
			args = append(args, celArgs...)
			conditions = append(conditions, processedWhere)
		}
	}

	// Build query against the audit logs table
	// Use toString() to ensure consistent string output for all column types (including UInt16 status_code)
	query := fmt.Sprintf("SELECT toString(%s) as value, COUNT(*) as count FROM %s.audit_logs", column, s.config.Database)

	if len(conditions) > 0 {
		query += " WHERE " + strings.Join(conditions, " AND ")
	}

	// Group by the facet column and order by count descending, then value ascending for stability
	query += fmt.Sprintf(" GROUP BY %s ORDER BY count DESC, value ASC LIMIT %d", column, limit)

	klog.V(4).InfoS("Executing audit log facet query",
		"field", facet.Field,
		"column", column,
		"query", query,
	)

	rows, err := s.conn.Query(ctx, query, args...)
	if err != nil {
		klog.ErrorS(err, "Failed to execute audit log facet query", "field", facet.Field)
		return nil, fmt.Errorf("unable to retrieve facet data for field '%s'. Try again or contact support if the problem persists", facet.Field)
	}
	defer rows.Close()

	result := &FacetFieldResult{
		Field:  facet.Field,
		Values: make([]FacetValueResult, 0),
	}

	for rows.Next() {
		var value string
		var count uint64
		if err := rows.Scan(&value, &count); err != nil {
			klog.ErrorS(err, "Failed to scan audit log facet row", "field", facet.Field)
			return nil, fmt.Errorf("unable to retrieve facet data for field '%s'. Try again or contact support if the problem persists", facet.Field)
		}
		result.Values = append(result.Values, FacetValueResult{
			Value: value,
			Count: int64(count),
		})
	}

	if err := rows.Err(); err != nil {
		klog.ErrorS(err, "Error iterating audit log facet rows", "field", facet.Field)
		return nil, fmt.Errorf("unable to retrieve facet data for field '%s'. Try again or contact support if the problem persists", facet.Field)
	}

	return result, nil
}

// FacetQuerySpec defines the parameters for an activity facet query.
type FacetQuerySpec struct {
	// TimeRange specifies the time window for facet aggregation.
	StartTime string
	EndTime   string

	// Filter is a CEL expression to filter activities before computing facets.
	Filter string

	// Facets are the fields to compute distinct values for.
	Facets []FacetFieldSpec
}

// QueryFacets retrieves distinct field values with counts for faceted search on activities.
func (s *ClickHouseStorage) QueryFacets(ctx context.Context, spec FacetQuerySpec, scope ScopeContext) (*FacetQueryResult, error) {
	ctx, span := tracer.Start(ctx, "clickhouse.query_facets",
		trace.WithSpanKind(trace.SpanKindClient),
		trace.WithAttributes(
			attribute.String("db.system", "clickhouse"),
			attribute.String("db.name", s.config.Database),
			attribute.String("db.operation", "SELECT"),
			attribute.Int("facet.count", len(spec.Facets)),
		),
	)
	defer span.End()

	result := &FacetQueryResult{
		Facets: make([]FacetFieldResult, 0, len(spec.Facets)),
	}

	// Execute each facet query
	for _, facet := range spec.Facets {
		facetResult, err := s.queryFacet(ctx, facet, spec, scope)
		if err != nil {
			span.RecordError(err)
			return nil, fmt.Errorf("failed to query facet %s: %w", facet.Field, err)
		}
		result.Facets = append(result.Facets, *facetResult)
	}

	span.SetStatus(codes.Ok, "facet query successful")
	return result, nil
}

// queryFacet executes a single facet query against the activities table.
func (s *ClickHouseStorage) queryFacet(ctx context.Context, facet FacetFieldSpec, spec FacetQuerySpec, scope ScopeContext) (*FacetFieldResult, error) {
	column, err := GetActivityFacetColumn(facet.Field)
	if err != nil {
		return nil, err
	}

	limit := facet.Limit
	if limit <= 0 {
		limit = 20
	}
	if limit > 100 {
		limit = 100
	}

	var args []interface{}
	var conditions []string

	// Scope filtering
	if scope.Type != types.TenantTypePlatform {
		if scope.Type == types.TenantTypeUser {
			// For user scope, filter by actor_uid to show activities performed by this user
			// across all organizations and projects
			conditions = append(conditions, "actor_uid = ?")
			args = append(args, scope.Name)
		} else {
			// For organization/project scope, filter by tenant
			conditions = append(conditions, "tenant_type = ?")
			args = append(args, scope.Type)
			conditions = append(conditions, "tenant_name = ?")
			args = append(args, scope.Name)
		}
	}

	// Time range
	now := time.Now()
	if spec.StartTime != "" {
		startTime, err := timeutil.ParseFlexibleTime(spec.StartTime, now)
		if err != nil {
			return nil, fmt.Errorf("invalid startTime: %w", err)
		}
		conditions = append(conditions, "timestamp >= ?")
		args = append(args, startTime)
	}

	if spec.EndTime != "" {
		endTime, err := timeutil.ParseFlexibleTime(spec.EndTime, now)
		if err != nil {
			return nil, fmt.Errorf("invalid endTime: %w", err)
		}
		conditions = append(conditions, "timestamp < ?")
		args = append(args, endTime)
	}

	// CEL filter (optional)
	if spec.Filter != "" {
		celWhere, celArgs, err := cel.ConvertActivityToClickHouseSQL(ctx, spec.Filter)
		if err != nil {
			return nil, err
		}
		if celWhere != "" {
			processedWhere := celWhere
			for i := range celArgs {
				oldParam := fmt.Sprintf("{arg%d}", i+1)
				processedWhere = strings.ReplaceAll(processedWhere, oldParam, "?")
			}
			args = append(args, celArgs...)
			conditions = append(conditions, processedWhere)
		}
	}

	query := fmt.Sprintf("SELECT %s, COUNT(*) as count FROM %s.activities", column, s.config.Database)

	if len(conditions) > 0 {
		query += " WHERE " + strings.Join(conditions, " AND ")
	}

	// Group by the facet column and order by count descending, then value ascending for stability
	query += fmt.Sprintf(" GROUP BY %s ORDER BY count DESC, %s ASC LIMIT %d", column, column, limit)

	klog.V(4).InfoS("Executing facet query",
		"field", facet.Field,
		"column", column,
		"query", query,
	)

	rows, err := s.conn.Query(ctx, query, args...)
	if err != nil {
		klog.ErrorS(err, "Failed to execute facet query", "field", facet.Field)
		return nil, fmt.Errorf("unable to retrieve facet data for field '%s'. Try again or contact support if the problem persists", facet.Field)
	}
	defer rows.Close()

	result := &FacetFieldResult{
		Field:  facet.Field,
		Values: make([]FacetValueResult, 0),
	}

	for rows.Next() {
		var value string
		var count uint64
		if err := rows.Scan(&value, &count); err != nil {
			klog.ErrorS(err, "Failed to scan facet row", "field", facet.Field)
			return nil, fmt.Errorf("unable to retrieve facet data for field '%s'. Try again or contact support if the problem persists", facet.Field)
		}
		result.Values = append(result.Values, FacetValueResult{
			Value: value,
			Count: int64(count),
		})
	}

	if err := rows.Err(); err != nil {
		klog.ErrorS(err, "Error iterating facet rows", "field", facet.Field)
		return nil, fmt.Errorf("unable to retrieve facet data for field '%s'. Try again or contact support if the problem persists", facet.Field)
	}

	return result, nil
}
