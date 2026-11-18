package preview

import (
	"context"
	"encoding/json"
	stderrors "errors"
	"fmt"
	"regexp"
	"strings"
	"time"

	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/util/validation/field"
	"k8s.io/apiserver/pkg/endpoints/request"
	"k8s.io/apiserver/pkg/registry/rest"

	"go.miloapis.com/activity/internal/apierrors"
	"go.miloapis.com/activity/internal/processor"
	"go.miloapis.com/activity/internal/registry/activity/policy"
	"go.miloapis.com/activity/internal/registry/scope"
	"go.miloapis.com/activity/internal/storage"
	"go.miloapis.com/activity/internal/timeutil"
	"go.miloapis.com/activity/pkg/apis/activity"
	"go.miloapis.com/activity/pkg/apis/activity/v1alpha1"
)

// AuditLogStorageBackend defines the interface for querying audit logs.
type AuditLogStorageBackend interface {
	QueryAuditLogs(ctx context.Context, spec v1alpha1.AuditLogQuerySpec, scope storage.ScopeContext) (*storage.QueryResult, error)
}

// EventQueryStorageBackend defines the interface for querying events.
type EventQueryStorageBackend interface {
	QueryEvents(ctx context.Context, spec v1alpha1.EventQuerySpec, scope storage.ScopeContext) (*storage.EventQueryResult, error)
}

// PolicyPreviewStorage implements REST storage for PolicyPreview resources.
// This is an ephemeral resource - it only supports Create operations and
// evaluates the policy against the provided inputs without persisting anything.
type PolicyPreviewStorage struct {
	auditLogBackend AuditLogStorageBackend
	eventBackend    EventQueryStorageBackend
}

// NewPolicyPreviewStorage creates a new REST storage for PolicyPreview.
func NewPolicyPreviewStorage(auditLogBackend AuditLogStorageBackend, eventBackend EventQueryStorageBackend) *PolicyPreviewStorage {
	return &PolicyPreviewStorage{
		auditLogBackend: auditLogBackend,
		eventBackend:    eventBackend,
	}
}

var (
	_ rest.Scoper               = &PolicyPreviewStorage{}
	_ rest.Storage              = &PolicyPreviewStorage{}
	_ rest.Creater              = &PolicyPreviewStorage{}
	_ rest.SingularNameProvider = &PolicyPreviewStorage{}
)

// New returns an empty PolicyPreview.
func (s *PolicyPreviewStorage) New() runtime.Object {
	return &v1alpha1.PolicyPreview{}
}

// Destroy cleans up resources.
func (s *PolicyPreviewStorage) Destroy() {}

// NamespaceScoped returns false because PolicyPreview is cluster-scoped.
func (s *PolicyPreviewStorage) NamespaceScoped() bool {
	return false
}

// GetSingularName returns the singular name of the resource.
func (s *PolicyPreviewStorage) GetSingularName() string {
	return "policypreview"
}

// Create evaluates the policy against all inputs and returns the result.
func (s *PolicyPreviewStorage) Create(ctx context.Context, obj runtime.Object, createValidation rest.ValidateObjectFunc, options *metav1.CreateOptions) (runtime.Object, error) {
	preview, ok := obj.(*v1alpha1.PolicyPreview)
	if !ok {
		return nil, errors.NewBadRequest("expected PolicyPreview object")
	}

	// Validate the entire preview spec
	if errs := validatePolicyPreview(preview); len(errs) > 0 {
		return nil, apierrors.NewValidationStatusError(
			v1alpha1.SchemeGroupVersion.WithKind("PolicyPreview").GroupKind(), "", errs)
	}

	// Auto-fetch inputs if requested
	var inputsToEvaluate []v1alpha1.PolicyPreviewInput
	var fetchedInputs []v1alpha1.PolicyPreviewInput

	if preview.Spec.AutoFetch != nil {
		// Fetch samples in the caller's scope, exactly as AuditLogQuery and
		// EventQuery would: an organization or project caller only sees that
		// tenant's events, with Datum staff redacted. Platform callers keep
		// platform-wide samples.
		reqUser, ok := request.UserFrom(ctx)
		if !ok {
			return nil, errors.NewInternalError(fmt.Errorf("no user in context"))
		}
		scopeCtx := scope.ExtractScopeFromUser(reqUser)

		fetched, err := s.autoFetchInputs(ctx, &preview.Spec, scopeCtx)
		if stderrors.Is(err, storage.ErrStaffRedactionNotReady) {
			return nil, storage.StaffRedactionNotReadyError()
		}
		if err != nil {
			return nil, errors.NewInternalError(fmt.Errorf("failed to auto-fetch sample inputs: %w", err))
		}

		inputsToEvaluate = fetched
		fetchedInputs = fetched
	} else {
		inputsToEvaluate = preview.Spec.Inputs
	}

	// Create a copy of the preview with the inputs to evaluate
	previewWithInputs := preview.DeepCopy()
	previewWithInputs.Spec.Inputs = inputsToEvaluate

	// Evaluate the policy against all inputs
	result := evaluatePolicy(previewWithInputs)

	// Add fetched inputs to status if auto-fetch was used
	if len(fetchedInputs) > 0 {
		result.Status.FetchedInputs = fetchedInputs
	}

	return result, nil
}

// validatePolicyPreview validates the entire PolicyPreview spec.
func validatePolicyPreview(preview *v1alpha1.PolicyPreview) field.ErrorList {
	allErrs := field.ErrorList{}
	specPath := field.NewPath("spec")

	// Convert v1alpha1 ActivityPolicySpec to internal type for validation
	internalSpec := convertPolicySpecToInternal(&preview.Spec.Policy)

	// Validate the policy spec
	policyPath := specPath.Child("policy")
	allErrs = append(allErrs, policy.ValidateActivityPolicySpec(internalSpec, policyPath)...)

	// Validate that exactly one of inputs or autoFetch is provided
	hasInputs := len(preview.Spec.Inputs) > 0
	hasAutoFetch := preview.Spec.AutoFetch != nil

	if !hasInputs && !hasAutoFetch {
		allErrs = append(allErrs, field.Required(specPath, "provide either 'inputs' or 'autoFetch'"))
	}

	if hasInputs && hasAutoFetch {
		allErrs = append(allErrs, field.Invalid(
			specPath,
			preview.Spec,
			"cannot specify both 'inputs' and 'autoFetch' - use one or the other",
		))
	}

	// Validate inputs if provided
	if hasInputs {
		allErrs = append(allErrs, validatePreviewInputs(preview.Spec.Inputs, specPath.Child("inputs"))...)
	}

	// Validate autoFetch if provided
	if hasAutoFetch {
		allErrs = append(allErrs, validateAutoFetch(preview.Spec.AutoFetch, specPath.Child("autoFetch"))...)
	}

	return allErrs
}

// convertPolicySpecToInternal converts a v1alpha1 ActivityPolicySpec to the internal type.
func convertPolicySpecToInternal(in *v1alpha1.ActivityPolicySpec) *activity.ActivityPolicySpec {
	out := &activity.ActivityPolicySpec{
		Resource: activity.ActivityPolicyResource{
			APIGroup: in.Resource.APIGroup,
			Kind:     in.Resource.Kind,
		},
	}

	out.AuditRules = make([]activity.ActivityPolicyRule, len(in.AuditRules))
	for i, rule := range in.AuditRules {
		out.AuditRules[i] = activity.ActivityPolicyRule{
			Name:        rule.Name,
			Description: rule.Description,
			Match:       rule.Match,
			Summary:     rule.Summary,
		}
	}

	out.EventRules = make([]activity.ActivityPolicyRule, len(in.EventRules))
	for i, rule := range in.EventRules {
		out.EventRules[i] = activity.ActivityPolicyRule{
			Name:        rule.Name,
			Description: rule.Description,
			Match:       rule.Match,
			Summary:     rule.Summary,
		}
	}

	return out
}

// validatePreviewInputs validates the PolicyPreview inputs.
func validatePreviewInputs(inputs []v1alpha1.PolicyPreviewInput, inputsPath *field.Path) field.ErrorList {
	allErrs := field.ErrorList{}

	for i, input := range inputs {
		inputPath := inputsPath.Index(i)

		if input.Type == "" {
			allErrs = append(allErrs, field.Required(inputPath.Child("type"), "specify whether this input is an 'audit' log or 'event'"))
			continue
		}

		switch input.Type {
		case "audit":
			if input.Audit == nil {
				allErrs = append(allErrs, field.Required(inputPath.Child("audit"), "provide the audit log data to test"))
			}
		case "event":
			if input.Event == nil {
				allErrs = append(allErrs, field.Required(inputPath.Child("event"), "provide the event data to test"))
			}
		default:
			allErrs = append(allErrs, field.NotSupported(inputPath.Child("type"), input.Type, []string{"audit", "event"}))
		}
	}

	return allErrs
}

// validateAutoFetch validates the AutoFetch spec.
func validateAutoFetch(autoFetch *v1alpha1.AutoFetchSpec, fldPath *field.Path) field.ErrorList {
	allErrs := field.ErrorList{}

	// Validate limit
	if autoFetch.Limit < 0 {
		allErrs = append(allErrs, field.Invalid(fldPath.Child("limit"), autoFetch.Limit, "must be >= 0"))
	}
	if autoFetch.Limit > 50 {
		allErrs = append(allErrs, field.Invalid(fldPath.Child("limit"), autoFetch.Limit, "must be <= 50"))
	}

	// Validate timeRange
	if autoFetch.TimeRange != "" {
		// Try to parse as relative time
		// The timeRange should be a duration like "24h", "7d", etc.
		// We'll prepend "now-" to validate it
		timeRangeExpr := autoFetch.TimeRange
		if !strings.HasPrefix(autoFetch.TimeRange, "now") {
			timeRangeExpr = "now-" + autoFetch.TimeRange
		}
		now := time.Now()
		_, err := timeutil.ParseFlexibleTime(timeRangeExpr, now)
		if err != nil {
			allErrs = append(allErrs, field.Invalid(
				fldPath.Child("timeRange"),
				autoFetch.TimeRange,
				"must be a valid relative time (e.g., '1h', '24h', '7d')",
			))
		}
	}

	// Validate sources
	if autoFetch.Sources != "" {
		validSources := []string{"audit", "events", "both"}
		valid := false
		for _, s := range validSources {
			if autoFetch.Sources == s {
				valid = true
				break
			}
		}
		if !valid {
			allErrs = append(allErrs, field.NotSupported(
				fldPath.Child("sources"),
				autoFetch.Sources,
				validSources,
			))
		}
	}

	return allErrs
}

// evaluatePolicy evaluates the policy spec against all inputs and returns the result.
func evaluatePolicy(preview *v1alpha1.PolicyPreview) *v1alpha1.PolicyPreview {
	result := preview.DeepCopy()
	result.Status = v1alpha1.PolicyPreviewStatus{
		Activities: []v1alpha1.Activity{},
		Results:    make([]v1alpha1.PolicyPreviewInputResult, len(preview.Spec.Inputs)),
	}

	spec := &preview.Spec.Policy

	// Process each input
	for i, input := range preview.Spec.Inputs {
		inputResult := v1alpha1.PolicyPreviewInputResult{
			InputIndex:       i,
			Matched:          false,
			MatchedRuleIndex: -1,
		}

		switch input.Type {
		case "audit":
			evalResult, err := evaluateAuditInput(spec, &input)
			if err != nil {
				inputResult.Error = err.Error()
			} else if evalResult.Activity != nil {
				inputResult.Matched = true
				inputResult.MatchedRuleIndex = evalResult.MatchedRuleIndex
				inputResult.MatchedRuleType = evalResult.MatchedRuleType
				inputResult.MatchedRuleName = evalResult.MatchedRuleName
				result.Status.Activities = append(result.Status.Activities, *evalResult.Activity)
			}

		case "event":
			evalResult, err := evaluateEventInput(spec, &input)
			if err != nil {
				inputResult.Error = err.Error()
			} else if evalResult.Activity != nil {
				inputResult.Matched = true
				inputResult.MatchedRuleIndex = evalResult.MatchedRuleIndex
				inputResult.MatchedRuleType = evalResult.MatchedRuleType
				inputResult.MatchedRuleName = evalResult.MatchedRuleName
				result.Status.Activities = append(result.Status.Activities, *evalResult.Activity)
			}
		}

		result.Status.Results[i] = inputResult
	}

	return result
}

// evaluateAuditInput evaluates audit rules against an audit log input using the shared processor.
func evaluateAuditInput(spec *v1alpha1.ActivityPolicySpec, input *v1alpha1.PolicyPreviewInput) (*processor.EvaluationResult, error) {
	if input.Audit == nil {
		return nil, fmt.Errorf("audit input is nil")
	}

	// Pass nil for KindResolver since preview doesn't need full kind resolution
	return processor.EvaluateAuditRules(spec, input.Audit, nil)
}

// evaluateEventInput evaluates event rules against a Kubernetes event input using the shared processor.
func evaluateEventInput(spec *v1alpha1.ActivityPolicySpec, input *v1alpha1.PolicyPreviewInput) (*processor.EvaluationResult, error) {
	if input.Event == nil || len(input.Event.Raw) == 0 {
		return nil, fmt.Errorf("event input is nil or empty")
	}

	var eventMap map[string]interface{}
	if err := json.Unmarshal(input.Event.Raw, &eventMap); err != nil {
		return nil, fmt.Errorf("failed to unmarshal event: %w", err)
	}

	// Pass nil for KindResolver since preview doesn't need full kind resolution
	return processor.EvaluateEventRules(spec, eventMap, nil)
}

// autoFetchInputs retrieves sample inputs based on the policy resource type.
func (s *PolicyPreviewStorage) autoFetchInputs(
	ctx context.Context,
	spec *v1alpha1.PolicyPreviewSpec,
	scope storage.ScopeContext,
) ([]v1alpha1.PolicyPreviewInput, error) {
	inputs := []v1alpha1.PolicyPreviewInput{}
	autoFetch := spec.AutoFetch

	// Determine time range (default 24h)
	timeRange := autoFetch.TimeRange
	if timeRange == "" {
		timeRange = "24h"
	}

	// Parse time range to create startTime and endTime
	// The timeRange is expected to be a relative duration like "24h", "7d", etc.
	// We need to convert it to an absolute timestamp
	now := time.Now()

	// Try parsing as a relative time expression (e.g., "now-24h")
	// If it's already in the format "24h", prepend "now-"
	timeRangeExpr := timeRange
	if !strings.HasPrefix(timeRange, "now") {
		timeRangeExpr = "now-" + timeRange
	}

	startTime, err := timeutil.ParseFlexibleTime(timeRangeExpr, now)
	if err != nil {
		return nil, fmt.Errorf("invalid timeRange: %w", err)
	}
	startTimeStr := startTime.Format(time.RFC3339)
	endTimeStr := now.Format(time.RFC3339)

	// Determine limit (default 10)
	limit := autoFetch.Limit
	if limit == 0 {
		limit = 10
	}

	// Determine sources (default "both")
	sources := autoFetch.Sources
	if sources == "" {
		sources = "both"
	}

	// Fetch audit logs if requested and policy has audit rules
	if (sources == "audit" || sources == "both") && len(spec.Policy.AuditRules) > 0 {
		auditInputs, err := s.fetchAuditLogSamples(ctx, spec, startTimeStr, endTimeStr, limit, scope)
		if err != nil {
			return nil, fmt.Errorf("failed to fetch audit log samples: %w", err)
		}
		inputs = append(inputs, auditInputs...)
	}

	// Fetch events if requested and policy has event rules
	if (sources == "events" || sources == "both") && len(spec.Policy.EventRules) > 0 {
		eventInputs, err := s.fetchEventSamples(ctx, spec, startTimeStr, endTimeStr, limit, scope)
		if err != nil {
			return nil, fmt.Errorf("failed to fetch event samples: %w", err)
		}
		inputs = append(inputs, eventInputs...)
	}

	return inputs, nil
}

// fetchAuditLogSamples queries ClickHouse for audit logs matching the policy resource.
func (s *PolicyPreviewStorage) fetchAuditLogSamples(
	ctx context.Context,
	spec *v1alpha1.PolicyPreviewSpec,
	startTime, endTime string,
	limit int32,
	scope storage.ScopeContext,
) ([]v1alpha1.PolicyPreviewInput, error) {
	resource := spec.Policy.Resource

	// Build filter to match policy resource
	// Match both apiGroup and kind (resource name is pluralized in audit logs)
	var filterParts []string

	if resource.APIGroup != "" {
		filterParts = append(filterParts, fmt.Sprintf(`objectRef.apiGroup == "%s"`, resource.APIGroup))
	} else {
		// Core API resources have empty apiGroup
		filterParts = append(filterParts, `objectRef.apiGroup == ""`)
	}

	// Match kind by checking if the resource field contains the lowercase kind
	// This is a heuristic since we don't have exact resource name mapping
	filterParts = append(filterParts, fmt.Sprintf(`objectRef.resource.contains("%s")`, strings.ToLower(resource.Kind)))

	// Add rule match expressions to fetch data that will actually match the rules.
	// We OR the rules together since we want data that matches ANY rule.
	// Match expressions now use the same format as AuditLogQuery filters (no prefix).
	if ruleFilter := buildRuleFilter(spec.Policy.AuditRules); ruleFilter != "" {
		filterParts = append(filterParts, ruleFilter)
	}

	filter := strings.Join(filterParts, " && ")

	// Query audit logs
	querySpec := v1alpha1.AuditLogQuerySpec{
		StartTime: startTime,
		EndTime:   endTime,
		Filter:    filter,
		Limit:     limit,
	}

	result, err := s.auditLogBackend.QueryAuditLogs(ctx, querySpec, scope)
	if err != nil {
		return nil, err
	}

	// Convert to PolicyPreviewInput
	inputs := make([]v1alpha1.PolicyPreviewInput, len(result.Events))
	for i, event := range result.Events {
		inputs[i] = v1alpha1.PolicyPreviewInput{
			Type:  "audit",
			Audit: &event,
		}
	}

	return inputs, nil
}

// buildRuleFilter takes the policy's audit rules and builds a filter expression
// that matches ANY of the rules. ActivityPolicy audit rule expressions use the
// "audit." prefix (e.g., audit.verb == 'create'), but AuditLogQuery filters use
// the flat format (e.g., verb == 'create'). This function translates between the
// two by stripping the "audit." prefix before building the query filter.
func buildRuleFilter(rules []v1alpha1.ActivityPolicyRule) string {
	if len(rules) == 0 {
		return ""
	}

	var parts []string
	for _, rule := range rules {
		if rule.Match == "" || rule.Match == "true" {
			continue
		}
		// Strip "audit." prefix from identifier positions to convert ActivityPolicy CEL
		// to AuditLogQuery filter format. Only strips occurrences outside of quoted strings.
		filterExpr := stripAuditPrefix(rule.Match)
		parts = append(parts, "("+filterExpr+")")
	}

	if len(parts) == 0 {
		return ""
	}
	if len(parts) == 1 {
		return parts[0]
	}
	// OR the rules together - we want data that matches ANY rule
	return "(" + strings.Join(parts, " || ") + ")"
}

// stripAuditPrefix removes the "audit." prefix from CEL identifier positions,
// preserving any occurrences inside string literals. This converts ActivityPolicy
// CEL expressions (audit.verb == 'create') to AuditLogQuery filter format
// (verb == 'create').
func stripAuditPrefix(expr string) string {
	var result strings.Builder
	i := 0
	for i < len(expr) {
		// Skip over single-quoted strings
		if expr[i] == '\'' {
			result.WriteByte(expr[i])
			i++
			for i < len(expr) && expr[i] != '\'' {
				if expr[i] == '\\' && i+1 < len(expr) {
					result.WriteByte(expr[i])
					i++
				}
				result.WriteByte(expr[i])
				i++
			}
			if i < len(expr) {
				result.WriteByte(expr[i])
				i++
			}
			continue
		}
		// Skip over double-quoted strings
		if expr[i] == '"' {
			result.WriteByte(expr[i])
			i++
			for i < len(expr) && expr[i] != '"' {
				if expr[i] == '\\' && i+1 < len(expr) {
					result.WriteByte(expr[i])
					i++
				}
				result.WriteByte(expr[i])
				i++
			}
			if i < len(expr) {
				result.WriteByte(expr[i])
				i++
			}
			continue
		}
		// Check for "audit." prefix outside of strings
		if strings.HasPrefix(expr[i:], "audit.") {
			i += len("audit.")
			continue
		}
		result.WriteByte(expr[i])
		i++
	}
	return result.String()
}

// fetchEventSamples queries ClickHouse for K8s events matching the policy resource.
func (s *PolicyPreviewStorage) fetchEventSamples(
	ctx context.Context,
	spec *v1alpha1.PolicyPreviewSpec,
	startTime, endTime string,
	limit int32,
	scope storage.ScopeContext,
) ([]v1alpha1.PolicyPreviewInput, error) {
	resource := spec.Policy.Resource

	// Build field selector to match policy resource
	// Events use regarding.apiVersion and regarding.kind
	apiVersion := resource.APIGroup
	if apiVersion == "" {
		apiVersion = "v1" // Core API
	}

	var fieldSelectorParts []string
	fieldSelectorParts = append(fieldSelectorParts, fmt.Sprintf("regarding.apiVersion=%s", apiVersion))
	fieldSelectorParts = append(fieldSelectorParts, fmt.Sprintf("regarding.kind=%s", resource.Kind))

	// Extract additional filters from the rule's CEL match expressions
	// This helps fetch data that will actually match the rules
	for _, rule := range spec.Policy.EventRules {
		if ruleFilters := extractEventFiltersFromCEL(rule.Match); len(ruleFilters) > 0 {
			fieldSelectorParts = append(fieldSelectorParts, ruleFilters...)
		}
	}

	fieldSelector := strings.Join(fieldSelectorParts, ",")

	// Query events
	querySpec := v1alpha1.EventQuerySpec{
		StartTime:     startTime,
		EndTime:       endTime,
		FieldSelector: fieldSelector,
		Limit:         limit,
	}

	result, err := s.eventBackend.QueryEvents(ctx, querySpec, scope)
	if err != nil {
		return nil, err
	}

	// Convert to PolicyPreviewInput
	// The processor expects events as map[string]interface{}, so we need to
	// convert the eventsv1.Event structure to a map by marshaling and unmarshaling
	inputs := make([]v1alpha1.PolicyPreviewInput, len(result.Events))
	for i, eventRecord := range result.Events {
		// Marshal the eventsv1.Event to JSON
		eventJSON, err := json.Marshal(eventRecord.Event)
		if err != nil {
			return nil, fmt.Errorf("failed to marshal event: %w", err)
		}

		inputs[i] = v1alpha1.PolicyPreviewInput{
			Type:  "event",
			Event: &runtime.RawExtension{Raw: eventJSON},
		}
	}

	return inputs, nil
}

// extractEventFiltersFromCEL extracts simple filters from a CEL match expression.
// This is a best-effort extraction to help fetch relevant data for preview.
// It handles patterns like:
//   - event.reason == "Ready" → reason=Ready
//   - event.type == "Warning" → type=Warning
func extractEventFiltersFromCEL(celExpr string) []string {
	var filters []string

	// Pattern: event.reason == "value"
	reasonPattern := regexp.MustCompile(`event\.reason\s*==\s*"([^"]+)"`)
	if matches := reasonPattern.FindStringSubmatch(celExpr); len(matches) > 1 {
		filters = append(filters, fmt.Sprintf("reason=%s", matches[1]))
	}

	// Pattern: event.type == "value"
	typePattern := regexp.MustCompile(`event\.type\s*==\s*"([^"]+)"`)
	if matches := typePattern.FindStringSubmatch(celExpr); len(matches) > 1 {
		filters = append(filters, fmt.Sprintf("type=%s", matches[1]))
	}

	return filters
}
