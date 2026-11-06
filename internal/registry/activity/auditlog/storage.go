package auditlog

import (
	"context"
	stderrors "errors"
	"fmt"
	"time"

	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/util/validation/field"
	"k8s.io/apiserver/pkg/endpoints/request"
	"k8s.io/apiserver/pkg/registry/rest"
	"k8s.io/klog/v2"

	"go.miloapis.com/activity/internal/cel"
	"go.miloapis.com/activity/internal/metrics"
	"go.miloapis.com/activity/internal/registry/scope"
	"go.miloapis.com/activity/internal/storage"
	"go.miloapis.com/activity/internal/timeutil"
	"go.miloapis.com/activity/pkg/apis/activity/v1alpha1"
)

// StorageInterface defines the interface for storage operations needed by QueryStorage
type StorageInterface interface {
	QueryAuditLogs(ctx context.Context, spec v1alpha1.AuditLogQuerySpec, scope storage.ScopeContext) (*storage.QueryResult, error)
	GetMaxQueryWindow() time.Duration
	GetMaxPageSize() int32
}

// QueryStorage implements REST storage for AuditLogQuery
type QueryStorage struct {
	storage StorageInterface
}

// NewQueryStorage returns a RESTStorage object for AuditLogQuery
func NewQueryStorage(storage *storage.ClickHouseStorage) *QueryStorage {
	return &QueryStorage{
		storage: storage,
	}
}

var (
	_ rest.Scoper               = &QueryStorage{}
	_ rest.Creater              = &QueryStorage{}
	_ rest.Storage              = &QueryStorage{}
	_ rest.SingularNameProvider = &QueryStorage{}
	// Note: Get and List are intentionally NOT implemented.
	// AuditLogQuery is an ephemeral resource (like TokenReview/SubjectAccessReview)
	// that only supports Create. Queries execute immediately and return results
	// without persisting the resource.
)

// New returns an empty AuditLogQuery
func (r *QueryStorage) New() runtime.Object {
	return &v1alpha1.AuditLogQuery{}
}

// Destroy cleans up resources
func (r *QueryStorage) Destroy() {
	// Nothing to destroy
}

// NamespaceScoped returns false
func (r *QueryStorage) NamespaceScoped() bool {
	return false
}

// GetSingularName returns the singular name of the resource
func (r *QueryStorage) GetSingularName() string {
	return "auditlogquery"
}

// Create implements the ephemeral query pattern (like TokenReview). The query
// executes immediately and returns results without persisting the resource,
// ensuring audit logs remain queryable without cluttering etcd.
func (r *QueryStorage) Create(ctx context.Context, obj runtime.Object, createValidation rest.ValidateObjectFunc, options *metav1.CreateOptions) (runtime.Object, error) {
	query, ok := obj.(*v1alpha1.AuditLogQuery)
	if !ok {
		return nil, fmt.Errorf("not an AuditLogQuery: %#v", obj)
	}

	klog.V(4).Infof("Executing AuditLogQuery: %+v", query.Spec)

	// Authenticate the request to ensure proper RBAC enforcement
	reqUser, ok := request.UserFrom(ctx)
	if !ok {
		return nil, errors.NewInternalError(fmt.Errorf("no user in context"))
	}

	// Apply tenant isolation by extracting scope boundaries from user context.
	// Platform admins see all events; organization/project users see only their scope.
	scopeCtx := scope.ExtractScopeFromUser(reqUser)

	metrics.AuditLogQueriesByScope.WithLabelValues(scopeCtx.Type).Inc()

	klog.InfoS("Executing scope-aware audit log query",
		"query", query.Name,
		"scopeType", scopeCtx.Type,
		"scopeName", scopeCtx.Name,
		"startTime", query.Spec.StartTime,
		"endTime", query.Spec.EndTime,
	)

	// Reject invalid queries early to prevent expensive database operations
	if errs := r.validateQuerySpec(query); len(errs) > 0 {
		return nil, errors.NewInvalid(
			v1alpha1.SchemeGroupVersion.WithKind("AuditLogQuery").GroupKind(),
			query.Name,
			errs,
		)
	}

	// Parse effective timestamps using a single reference time for consistency
	now := time.Now()
	effectiveStartTime, err := timeutil.ParseFlexibleTime(query.Spec.StartTime, now)
	if err != nil {
		// This should not happen as validation already passed, but handle defensively
		return nil, errors.NewInternalError(fmt.Errorf("failed to parse startTime: %w", err))
	}
	effectiveEndTime, err := timeutil.ParseFlexibleTime(query.Spec.EndTime, now)
	if err != nil {
		// This should not happen as validation already passed, but handle defensively
		return nil, errors.NewInternalError(fmt.Errorf("failed to parse endTime: %w", err))
	}

	result, err := r.storage.QueryAuditLogs(ctx, query.Spec, scopeCtx)
	if err != nil {
		return nil, r.convertToStructuredError(query, err)
	}

	query.Status.Results = result.Events
	query.Status.Continue = result.Continue
	query.Status.EffectiveStartTime = effectiveStartTime.Format(time.RFC3339)
	query.Status.EffectiveEndTime = effectiveEndTime.Format(time.RFC3339)

	return query, nil
}

// validateQuerySpec validates the query specification and returns field errors
func (r *QueryStorage) validateQuerySpec(query *v1alpha1.AuditLogQuery) field.ErrorList {
	allErrs := field.ErrorList{}
	specPath := field.NewPath("spec")

	// Use a single reference time for all time parsing to prevent sub-second drift
	// when using relative times like "now-7d" and "now"
	now := time.Now()

	if query.Spec.StartTime == "" {
		allErrs = append(allErrs, field.Required(specPath.Child("startTime"), "must specify a start time"))
	} else {
		_, err := timeutil.ParseFlexibleTime(query.Spec.StartTime, now)
		if err != nil {
			allErrs = append(allErrs, field.Invalid(specPath.Child("startTime"), query.Spec.StartTime, err.Error()))
		}
	}

	if query.Spec.EndTime == "" {
		allErrs = append(allErrs, field.Required(specPath.Child("endTime"), "must specify an end time"))
	} else {
		_, err := timeutil.ParseFlexibleTime(query.Spec.EndTime, now)
		if err != nil {
			allErrs = append(allErrs, field.Invalid(specPath.Child("endTime"), query.Spec.EndTime, err.Error()))
		}
	}

	if query.Spec.StartTime != "" && query.Spec.EndTime != "" {
		startTime, err1 := timeutil.ParseFlexibleTime(query.Spec.StartTime, now)
		endTime, err2 := timeutil.ParseFlexibleTime(query.Spec.EndTime, now)

		if err1 == nil && err2 == nil {
			if !endTime.After(startTime) {
				allErrs = append(allErrs, field.Invalid(specPath.Child("endTime"), query.Spec.EndTime, "endTime must be after startTime"))
			}

			queryWindow := endTime.Sub(startTime)
			maxWindow := r.storage.GetMaxQueryWindow()
			if maxWindow > 0 && queryWindow > maxWindow {
				allErrs = append(allErrs, field.Invalid(specPath, fmt.Sprintf("%s to %s", query.Spec.StartTime, query.Spec.EndTime),
					fmt.Sprintf("time range of %v exceeds maximum of %v. Reduce the time range or split into smaller queries", queryWindow, maxWindow)))
			}

			// Capture query patterns to identify optimization opportunities and
			// data retention requirements.
			lookbackDuration := now.Sub(startTime)
			metrics.AuditLogQueryLookbackDuration.Observe(lookbackDuration.Seconds())
			metrics.AuditLogQueryTimeRange.Observe(queryWindow.Seconds())
		}
	}

	if query.Spec.Limit < 0 {
		allErrs = append(allErrs, field.Invalid(specPath.Child("limit"), query.Spec.Limit, "limit must be non-negative"))
	}

	maxPageSize := r.storage.GetMaxPageSize()
	if maxPageSize > 0 && query.Spec.Limit > maxPageSize {
		allErrs = append(allErrs, field.Invalid(specPath.Child("limit"), query.Spec.Limit,
			fmt.Sprintf("limit of %d exceeds maximum of %d. Set limit to %d or less", query.Spec.Limit, maxPageSize, maxPageSize)))
	}

	// Validate cursor if provided (delegates to storage layer for cursor internals)
	if query.Spec.Continue != "" {
		if err := storage.ValidateCursor(query.Spec.Continue, query.Spec); err != nil {
			allErrs = append(allErrs, field.Invalid(specPath.Child("continue"), query.Spec.Continue, err.Error()))
		}
	}

	// Validate CEL filter syntax at API layer to fail fast before database operations
	if query.Spec.Filter != "" {
		_, err := cel.CompileFilter(query.Spec.Filter)
		if err != nil {
			// CompileFilter returns friendly error messages with helpful context
			allErrs = append(allErrs, field.Invalid(specPath.Child("filter"), query.Spec.Filter, err.Error()))
		}
	}

	return allErrs
}

// convertToStructuredError translates internal database errors into actionable
// Kubernetes status errors with appropriate HTTP codes and retry semantics.
func (r *QueryStorage) convertToStructuredError(query *v1alpha1.AuditLogQuery, err error) error {
	if stderrors.Is(err, storage.ErrStaffRedactionNotReady) {
		return storage.StaffRedactionNotReadyError()
	}
	klog.ErrorS(err, "failed to execute query against clickhouse")

	return errors.NewServiceUnavailable("Failed to execute query. Please try again later or contact support for help.")
}

// ConvertToTable converts to table format
func (r *QueryStorage) ConvertToTable(ctx context.Context, object runtime.Object, tableOptions runtime.Object) (*metav1.Table, error) {
	return rest.NewDefaultTableConvertor(v1alpha1.Resource("auditlogquery")).ConvertToTable(ctx, object, tableOptions)
}
