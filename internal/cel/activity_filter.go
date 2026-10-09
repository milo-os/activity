package cel

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/google/cel-go/cel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"
	expr "google.golang.org/genproto/googleapis/api/expr/v1alpha1"

	"go.miloapis.com/activity/internal/metrics"
	"go.miloapis.com/activity/pkg/apis/activity/v1alpha1"
)

// ActivityFieldValidator implements FieldValidator for activity CEL expressions.
type ActivityFieldValidator struct{}

// ValidateSelectExpr validates field access for activity expressions.
// Handles both single-level (spec.changeSource) and nested (spec.actor.name) field access.
func (v *ActivityFieldValidator) ValidateSelectExpr(sel *expr.Expr_Select) error {
	operand := sel.GetOperand()
	if operand == nil {
		return nil
	}

	// Handle nested field access like spec.actor.name
	if nestedSel := operand.GetSelectExpr(); nestedSel != nil {
		if baseIdent := nestedSel.GetOperand().GetIdentExpr(); baseIdent != nil {
			baseName := baseIdent.GetName()
			parentField := nestedSel.GetField()
			field := sel.GetField()
			fullPath := baseName + "." + parentField

			if allowedFields, ok := activityValidFields[fullPath]; ok {
				if !allowedFields[field] {
					return fmt.Errorf("field '%s.%s' is not available for filtering", fullPath, field)
				}
			}
		}
		return nil
	}

	// Direct field access like spec.changeSource
	if identExpr := operand.GetIdentExpr(); identExpr != nil {
		baseObject := identExpr.GetName()
		field := sel.GetField()

		if allowedFields, ok := activityValidFields[baseObject]; ok {
			if !allowedFields[field] {
				return fmt.Errorf("field '%s.%s' is not available for filtering", baseObject, field)
			}
		}
	}

	return nil
}

// ActivityFieldMapper implements FieldMapper for activity CEL expressions.
type ActivityFieldMapper struct{}

// MapIdentExpr maps bare identifiers to ClickHouse columns for activities.
func (m *ActivityFieldMapper) MapIdentExpr(ident *expr.Expr_Ident) (string, error) {
	switch ident.Name {
	case "spec", "metadata":
		return "", fmt.Errorf("field '%s' must be accessed with dot notation (e.g., spec.changeSource, metadata.namespace)", ident.Name)
	default:
		return "", fmt.Errorf("field '%s' is not available for filtering", ident.Name)
	}
}

// MapSelectExpr maps field selectors to ClickHouse columns for activities.
func (m *ActivityFieldMapper) MapSelectExpr(sel *expr.Expr_Select) (string, error) {
	operand := sel.GetOperand()
	if operand == nil {
		return "", fmt.Errorf("select expression missing operand")
	}

	field := sel.GetField()

	// Check for nested select (e.g., spec.actor.name)
	if nestedSel := operand.GetSelectExpr(); nestedSel != nil {
		if baseIdent := nestedSel.GetOperand().GetIdentExpr(); baseIdent != nil {
			baseName := baseIdent.GetName()
			parentField := nestedSel.GetField()
			return m.mapNestedField(baseName, parentField, field)
		}
	}

	// Direct field access (e.g., spec.changeSource)
	if identExpr := operand.GetIdentExpr(); identExpr != nil {
		baseName := identExpr.GetName()
		return m.mapDirectField(baseName, field)
	}

	return "", fmt.Errorf("unsupported select expression structure")
}

// mapNestedField maps nested activity CEL field paths to ClickHouse columns.
func (m *ActivityFieldMapper) mapNestedField(baseName, parentField, field string) (string, error) {
	switch {
	// spec.actor.*
	case baseName == "spec" && parentField == "actor" && field == "name":
		return "actor_name", nil
	case baseName == "spec" && parentField == "actor" && field == "type":
		return "actor_type", nil
	case baseName == "spec" && parentField == "actor" && field == "uid":
		return "actor_uid", nil

	// spec.resource.*
	case baseName == "spec" && parentField == "resource" && field == "apiGroup":
		return "api_group", nil
	case baseName == "spec" && parentField == "resource" && field == "kind":
		return "resource_kind", nil
	case baseName == "spec" && parentField == "resource" && field == "name":
		return "resource_name", nil
	case baseName == "spec" && parentField == "resource" && field == "namespace":
		return "resource_namespace", nil
	case baseName == "spec" && parentField == "resource" && field == "uid":
		return "resource_uid", nil

	// spec.origin.*
	case baseName == "spec" && parentField == "origin" && field == "type":
		return "origin_type", nil

	// spec.source.*
	case baseName == "spec" && parentField == "source" && field == "planeType":
		return "source_plane_type", nil
	case baseName == "spec" && parentField == "source" && field == "cluster":
		return "source_cluster", nil
	case baseName == "spec" && parentField == "source" && field == "region":
		return "source_region", nil
	case baseName == "spec" && parentField == "source" && field == "city":
		return "source_city", nil

	default:
		return "", fmt.Errorf("field '%s.%s.%s' is not available for filtering", baseName, parentField, field)
	}
}

// mapDirectField maps direct activity CEL field paths to ClickHouse columns.
func (m *ActivityFieldMapper) mapDirectField(baseName, field string) (string, error) {
	switch {
	case baseName == "spec" && field == "changeSource":
		return "change_source", nil
	case baseName == "spec" && field == "summary":
		return "summary", nil
	case baseName == "metadata" && field == "namespace":
		return "activity_namespace", nil
	case baseName == "metadata" && field == "name":
		return "activity_name", nil
	case baseName == "spec" && field == "relatedUIDs":
		return "", fmt.Errorf("field 'spec.relatedUIDs' is a list and only supports 'in', e.g. 'abc-123' in spec.relatedUIDs")

	default:
		return "", fmt.Errorf("field '%s.%s' is not available for filtering", baseName, field)
	}
}

// MapArraySelectExpr maps list-valued activity fields to ClickHouse Array columns.
func (m *ActivityFieldMapper) MapArraySelectExpr(sel *expr.Expr_Select) (string, bool) {
	ident := sel.GetOperand().GetIdentExpr()
	if ident != nil && ident.GetName() == "spec" && sel.GetField() == "relatedUIDs" {
		return "related_uids", true
	}
	return "", false
}

// ActivityEnvironment creates a CEL environment for activity filtering.
//
// Available fields:
//   - spec.changeSource - "human" or "system"
//   - spec.actor.name - actor display name
//   - spec.actor.type - actor type
//   - spec.actor.uid - actor UID
//   - spec.resource.apiGroup - resource API group
//   - spec.resource.kind - resource kind
//   - spec.resource.name - resource name
//   - spec.resource.namespace - resource namespace
//   - spec.resource.uid - resource UID
//   - spec.summary - activity summary text
//   - spec.origin.type - origin type (audit/event)
//   - spec.source.planeType, spec.source.cluster, spec.source.region,
//     spec.source.city - where the underlying record originated
//   - spec.relatedUIDs - resource, related, and linked UIDs; only valid as
//     'uid' in spec.relatedUIDs
//   - metadata.namespace - activity namespace
//
// Supports standard CEL operators (==, !=, &&, ||, !, in) and string methods
// (startsWith, endsWith, contains).
func ActivityEnvironment() (*cel.Env, error) {
	specType := cel.MapType(cel.StringType, cel.DynType)
	metadataType := cel.MapType(cel.StringType, cel.DynType)

	return cel.NewEnv(
		cel.Variable("spec", specType),
		cel.Variable("metadata", metadataType),
	)
}

// activityValidFields defines the allowed fields for activity filtering
var activityValidFields = map[string]map[string]bool{
	"spec": {
		"changeSource": true,
		"summary":      true,
		"relatedUIDs":  true,
		// Parent fields - these are intermediate paths to nested fields
		"actor":    true,
		"resource": true,
		"origin":   true,
		"source":   true,
	},
	"spec.actor": {
		"name": true,
		"type": true,
		"uid":  true,
	},
	"spec.resource": {
		"apiGroup":  true,
		"kind":      true,
		"name":      true,
		"namespace": true,
		"uid":       true,
	},
	"spec.origin": {
		"type": true,
	},
	"spec.source": {
		"planeType": true,
		"cluster":   true,
		"region":    true,
		"city":      true,
	},
	"metadata": {
		"namespace": true,
		"name":      true,
	},
}

// CompileActivityFilter compiles and validates a CEL filter expression for activities.
// Returns user-friendly error messages with helpful context.
func CompileActivityFilter(filterExpr string) (*cel.Ast, error) {
	startTime := time.Now()

	if filterExpr == "" {
		metrics.CELFilterErrors.WithLabelValues("empty").Inc()
		return nil, fmt.Errorf("filter expression cannot be empty")
	}

	env, err := ActivityEnvironment()
	if err != nil {
		metrics.CELFilterErrors.WithLabelValues("environment").Inc()
		return nil, fmt.Errorf("unable to process filter expression. Try again or contact support if the problem persists")
	}

	ast, issues := env.Compile(filterExpr)
	if issues != nil && issues.Err() != nil {
		metrics.CELFilterErrors.WithLabelValues("compilation").Inc()
		metrics.CELFilterParseDuration.Observe(time.Since(startTime).Seconds())
		return nil, fmt.Errorf("%s", formatActivityFilterError(issues.Err()))
	}

	if !ast.OutputType().IsExactType(cel.BoolType) {
		metrics.CELFilterErrors.WithLabelValues("type_mismatch").Inc()
		metrics.CELFilterParseDuration.Observe(time.Since(startTime).Seconds())
		typeErr := fmt.Errorf("filter expression must return a boolean, got %v", ast.OutputType())
		return nil, fmt.Errorf("%s", formatActivityFilterError(typeErr))
	}

	// Validate that only allowed fields are accessed
	if err := ValidateFieldAccess(ast.Expr(), &ActivityFieldValidator{}); err != nil {
		metrics.CELFilterErrors.WithLabelValues("invalid_field").Inc()
		metrics.CELFilterParseDuration.Observe(time.Since(startTime).Seconds())
		return nil, fmt.Errorf("%s", formatActivityFilterError(err))
	}

	metrics.CELFilterParseDuration.Observe(time.Since(startTime).Seconds())
	return ast, nil
}

// CompiledActivityFilter holds a pre-compiled CEL program for reuse in watch operations.
type CompiledActivityFilter struct {
	program cel.Program
}

// CompileActivityFilterProgram compiles a CEL filter for watch evaluation.
// Unlike CompileActivityFilter which returns an AST for SQL conversion, this
// returns a compiled program that can evaluate Activity objects directly.
func CompileActivityFilterProgram(filterExpr string) (*CompiledActivityFilter, error) {
	if filterExpr == "" {
		return nil, fmt.Errorf("filter expression cannot be empty")
	}

	env, err := ActivityEnvironment()
	if err != nil {
		return nil, fmt.Errorf("unable to process filter expression. Try again or contact support if the problem persists")
	}

	ast, issues := env.Compile(filterExpr)
	if issues != nil && issues.Err() != nil {
		return nil, fmt.Errorf("%s", formatActivityFilterError(issues.Err()))
	}

	if !ast.OutputType().IsExactType(cel.BoolType) {
		typeErr := fmt.Errorf("filter expression must return a boolean, got %v", ast.OutputType())
		return nil, fmt.Errorf("%s", formatActivityFilterError(typeErr))
	}

	// Validate that only allowed fields are accessed
	if err := ValidateFieldAccess(ast.Expr(), &ActivityFieldValidator{}); err != nil {
		return nil, fmt.Errorf("%s", formatActivityFilterError(err))
	}

	// Create program from compiled AST
	program, err := env.Program(ast)
	if err != nil {
		return nil, fmt.Errorf("unable to process filter expression. Try again or contact support if the problem persists")
	}

	return &CompiledActivityFilter{program: program}, nil
}

// EvaluateActivity evaluates the filter against an Activity object.
// Returns true if the activity matches the filter, false otherwise.
func (f *CompiledActivityFilter) EvaluateActivity(activity *v1alpha1.Activity) (bool, error) {
	if f == nil || f.program == nil {
		return true, nil // No filter means match all
	}

	// Convert Activity to map format for CEL evaluation
	input := ActivityToMap(activity)

	// Evaluate the program
	result, _, err := f.program.Eval(input)
	if err != nil {
		return false, fmt.Errorf("unable to evaluate filter expression. Verify the filter syntax is correct")
	}

	// Extract boolean result
	boolVal, ok := result.Value().(bool)
	if !ok {
		return false, fmt.Errorf("filter expression must return a boolean result. Verify your filter uses comparison operators")
	}

	return boolVal, nil
}

// ActivityToMap converts an Activity struct to a map format for CEL evaluation.
// The map structure matches the CEL variables defined in ActivityEnvironment.
func ActivityToMap(activity *v1alpha1.Activity) map[string]interface{} {
	var source v1alpha1.ActivitySource
	if activity.Spec.Source != nil {
		source = *activity.Spec.Source
	}

	return map[string]interface{}{
		"spec": map[string]interface{}{
			"changeSource": activity.Spec.ChangeSource,
			"summary":      activity.Spec.Summary,
			"relatedUIDs":  RelatedUIDs(activity),
			"source": map[string]interface{}{
				"planeType": source.PlaneType,
				"cluster":   source.Cluster,
				"region":    source.Region,
				"city":      source.City,
			},
			"actor": map[string]interface{}{
				"name": activity.Spec.Actor.Name,
				"type": activity.Spec.Actor.Type,
				"uid":  activity.Spec.Actor.UID,
			},
			"resource": map[string]interface{}{
				"apiGroup":  activity.Spec.Resource.APIGroup,
				"kind":      activity.Spec.Resource.Kind,
				"name":      activity.Spec.Resource.Name,
				"namespace": activity.Spec.Resource.Namespace,
				"uid":       activity.Spec.Resource.UID,
			},
			"origin": map[string]interface{}{
				"type": activity.Spec.Origin.Type,
			},
		},
		"metadata": map[string]interface{}{
			"namespace": activity.Namespace,
			"name":      activity.Name,
		},
	}
}

// RelatedUIDs returns the distinct, non-empty UIDs of an activity's resource,
// related resources, and linked resources. Keep in sync with the related_uids
// column in migrations/011_activities_related_uids.sql.
func RelatedUIDs(activity *v1alpha1.Activity) []string {
	uids := make([]string, 0, 1+len(activity.Spec.Related)+len(activity.Spec.Links))
	seen := make(map[string]bool, cap(uids))
	add := func(uid string) {
		if uid != "" && !seen[uid] {
			seen[uid] = true
			uids = append(uids, uid)
		}
	}
	add(activity.Spec.Resource.UID)
	for _, r := range activity.Spec.Related {
		add(r.UID)
	}
	for _, l := range activity.Spec.Links {
		add(l.Resource.UID)
	}
	return uids
}

// formatActivityFilterError formats error messages for activity filter expressions
func formatActivityFilterError(err error) string {
	errMsg := err.Error()

	// Provide helpful suggestions for common errors
	if strings.Contains(errMsg, "undeclared reference") {
		return fmt.Sprintf(`%s

Available fields for activity filtering:
  - spec.changeSource - "human" or "system"
  - spec.actor.name, spec.actor.type, spec.actor.uid
  - spec.resource.apiGroup, spec.resource.kind, spec.resource.name
  - spec.resource.namespace, spec.resource.uid
  - spec.summary, spec.origin.type
  - spec.source.planeType, spec.source.cluster, spec.source.region, spec.source.city
  - spec.relatedUIDs (list; use "<uid>" in spec.relatedUIDs)
  - metadata.namespace, metadata.name

Example: spec.changeSource == "human" && spec.resource.kind == "Deployment"`, errMsg)
	}

	return errMsg
}

// ConvertActivityToClickHouseSQL converts a CEL expression for activities to a ClickHouse WHERE clause.
func ConvertActivityToClickHouseSQL(ctx context.Context, filterExpr string) (string, []any, error) {
	_, span := tracer.Start(ctx, "cel.activity_filter.convert",
		trace.WithAttributes(attribute.String("cel.expression", filterExpr)),
	)
	defer span.End()

	if filterExpr == "" {
		span.SetStatus(codes.Ok, "empty filter")
		return "", nil, nil
	}

	ast, err := CompileActivityFilter(filterExpr)
	if err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, "compilation failed")
		return "", nil, err
	}

	span.SetAttributes(attribute.Bool("cel.valid", true))

	converter := NewBaseSQLConverter(&ActivityFieldMapper{})

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
