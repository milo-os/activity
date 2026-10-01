package cel

import (
	"context"
	"fmt"
	"time"

	"github.com/google/cel-go/cel"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"
	expr "google.golang.org/genproto/googleapis/api/expr/v1alpha1"

	"go.miloapis.com/activity/internal/metrics"
)

var tracer = otel.Tracer("activity-cel-filter")

// AuditLogFieldValidator implements FieldValidator for audit log CEL expressions.
type AuditLogFieldValidator struct{}

// ValidateSelectExpr validates field access for audit log expressions.
func (v *AuditLogFieldValidator) ValidateSelectExpr(sel *expr.Expr_Select) error {
	operand := sel.GetOperand()
	if operand == nil {
		return nil
	}

	identExpr := operand.GetIdentExpr()
	if identExpr == nil {
		return nil
	}

	baseObject := identExpr.GetName()
	field := sel.GetField()

	if allowedFields, ok := validFields[baseObject]; ok {
		if !allowedFields[field] {
			availableFields := make([]string, 0, len(allowedFields))
			for f := range allowedFields {
				availableFields = append(availableFields, baseObject+"."+f)
			}
			return fmt.Errorf("field '%s.%s' is not available for filtering. Available fields for %s: %v",
				baseObject, field, baseObject, availableFields)
		}
	}

	return nil
}

// AuditLogFieldMapper implements FieldMapper for audit log CEL expressions.
type AuditLogFieldMapper struct{}

// MapIdentExpr maps bare identifiers to ClickHouse columns for audit logs.
func (m *AuditLogFieldMapper) MapIdentExpr(ident *expr.Expr_Ident) (string, error) {
	switch ident.Name {
	case "auditID":
		return "audit_id", nil
	case "verb":
		return "verb", nil
	case "requestReceivedTimestamp":
		return "timestamp", nil
	case "location":
		return "location", nil

	case "objectRef", "user", "responseStatus":
		return "", fmt.Errorf("field '%s' must be accessed with dot notation (e.g., objectRef.namespace, user.username, responseStatus.code)", ident.Name)

	default:
		return "", fmt.Errorf("field '%s' is not available for filtering", ident.Name)
	}
}

// MapSelectExpr maps field selectors to ClickHouse columns for audit logs.
func (m *AuditLogFieldMapper) MapSelectExpr(sel *expr.Expr_Select) (string, error) {
	operand := sel.GetOperand()
	if operand == nil {
		return "", fmt.Errorf("select expression missing operand")
	}

	identExpr := operand.GetIdentExpr()
	if identExpr == nil {
		return "", fmt.Errorf("select expression operand must be an identifier")
	}

	baseObject := identExpr.GetName()
	field := sel.GetField()

	switch {
	case baseObject == "objectRef" && field == "namespace":
		return "namespace", nil
	case baseObject == "objectRef" && field == "resource":
		return "resource", nil
	case baseObject == "objectRef" && field == "name":
		return "resource_name", nil
	case baseObject == "objectRef" && field == "apiGroup":
		return "api_group", nil

	case baseObject == "user" && field == "username":
		return "user", nil
	case baseObject == "user" && field == "uid":
		return "user_uid", nil

	case baseObject == "responseStatus" && field == "code":
		return "status_code", nil

	default:
		return "", fmt.Errorf("field '%s.%s' is not available for filtering", baseObject, field)
	}
}

// Environment creates a CEL environment for audit event filtering.
//
// Available fields: auditID, verb, requestReceivedTimestamp, location,
// objectRef.{namespace,resource,name,apiGroup}, user.{username,uid}, responseStatus.code
//
// Note: stageTimestamp is intentionally NOT available for filtering as it should
// only be used for internal pipeline delay calculations, not for querying events.
//
// Supports standard CEL operators (==, !=, <, >, <=, >=, &&, ||, !, in) and string methods
// (startsWith, endsWith, contains).
func Environment() (*cel.Env, error) {
	objectRefType := cel.MapType(cel.StringType, cel.DynType)
	userType := cel.MapType(cel.StringType, cel.DynType)
	responseStatusType := cel.MapType(cel.StringType, cel.DynType)

	return cel.NewEnv(
		cel.Variable("auditID", cel.StringType),
		cel.Variable("verb", cel.StringType),
		cel.Variable("requestReceivedTimestamp", cel.TimestampType),
		cel.Variable("location", cel.StringType),

		cel.Variable("objectRef", objectRefType),
		cel.Variable("user", userType),
		cel.Variable("responseStatus", responseStatusType),
	)
}

// validFields defines the allowed fields for each structured type
var validFields = map[string]map[string]bool{
	"objectRef": {
		"apiGroup":  true,
		"namespace": true,
		"resource":  true,
		"name":      true,
	},
	"user": {
		"username": true,
		"uid":      true,
	},
	"responseStatus": {
		"code": true,
	},
}

// CompileFilter compiles and validates a CEL filter expression, ensuring it returns a boolean.
// Returns user-friendly error messages with helpful context (available fields, documentation links).
func CompileFilter(filterExpr string) (*cel.Ast, error) {
	startTime := time.Now()

	if filterExpr == "" {
		metrics.CELFilterErrors.WithLabelValues("empty").Inc()
		return nil, fmt.Errorf("filter expression cannot be empty")
	}

	env, err := Environment()
	if err != nil {
		metrics.CELFilterErrors.WithLabelValues("environment").Inc()
		return nil, fmt.Errorf("unable to process filter expression. Try again or contact support if the problem persists")
	}

	ast, issues := env.Compile(filterExpr)
	if issues != nil && issues.Err() != nil {
		metrics.CELFilterErrors.WithLabelValues("compilation").Inc()
		metrics.CELFilterParseDuration.Observe(time.Since(startTime).Seconds())
		// Return friendly formatted error instead of raw CEL error
		return nil, fmt.Errorf("%s", formatFilterError(issues.Err()))
	}

	if !ast.OutputType().IsExactType(cel.BoolType) {
		metrics.CELFilterErrors.WithLabelValues("type_mismatch").Inc()
		metrics.CELFilterParseDuration.Observe(time.Since(startTime).Seconds())
		// Format type mismatch error with helpful context
		typeErr := fmt.Errorf("filter expression must return a boolean, got %v", ast.OutputType())
		return nil, fmt.Errorf("%s", formatFilterError(typeErr))
	}

	// Validate that only allowed fields are accessed on structured types
	if err := ValidateFieldAccess(ast.Expr(), &AuditLogFieldValidator{}); err != nil {
		metrics.CELFilterErrors.WithLabelValues("invalid_field").Inc()
		metrics.CELFilterParseDuration.Observe(time.Since(startTime).Seconds())
		return nil, fmt.Errorf("%s", formatFilterError(err))
	}

	metrics.CELFilterParseDuration.Observe(time.Since(startTime).Seconds())
	return ast, nil
}

// ConvertToClickHouseSQL converts a CEL expression to a ClickHouse WHERE clause with tracing.
func ConvertToClickHouseSQL(ctx context.Context, filterExpr string) (string, []any, error) {
	_, span := tracer.Start(ctx, "cel.filter.convert",
		trace.WithAttributes(attribute.String("cel.expression", filterExpr)),
	)
	defer span.End()

	if filterExpr == "" {
		span.SetStatus(codes.Ok, "empty filter")
		return "", nil, nil
	}

	ast, err := CompileFilter(filterExpr)
	if err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, "compilation failed")
		return "", nil, err
	}

	span.SetAttributes(attribute.Bool("cel.valid", true))

	converter := NewBaseSQLConverter(&AuditLogFieldMapper{})

	sql, err := converter.ConvertExpr(ast.Expr())
	if err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, "conversion failed")
		return "", nil, err
	}

	span.SetAttributes(
		attribute.String("sql.where_clause", sql),
		attribute.Int("sql.param_count", len(converter.Args())),
	)
	span.SetStatus(codes.Ok, "conversion successful")

	return sql, converter.Args(), nil
}
