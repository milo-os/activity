package record

import (
	"context"
	"encoding/json"
	stderrors "errors"
	"fmt"
	"strconv"
	"strings"
	"time"
	"unicode"

	"k8s.io/apimachinery/pkg/api/errors"
	metainternalversion "k8s.io/apimachinery/pkg/apis/meta/internalversion"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/fields"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/selection"
	"k8s.io/apimachinery/pkg/util/duration"
	"k8s.io/apimachinery/pkg/watch"
	"k8s.io/apiserver/pkg/endpoints/request"
	"k8s.io/apiserver/pkg/registry/rest"
	"k8s.io/klog/v2"

	"go.miloapis.com/activity/internal/cel"
	"go.miloapis.com/activity/internal/registry/scope"
	"go.miloapis.com/activity/internal/storage"
	"go.miloapis.com/activity/internal/timeutil"
	activitywatch "go.miloapis.com/activity/internal/watch"
	"go.miloapis.com/activity/pkg/apis/activity/v1alpha1"
)

// ActivityStorageInterface defines the storage operations needed by ActivityStorage.
type ActivityStorageInterface interface {
	QueryActivities(ctx context.Context, spec storage.ActivityQuerySpec, scope storage.ScopeContext) (*storage.ActivityQueryResult, error)
}

// ActivityStorage implements REST storage for Activity resources.
// This provides a simplified List/Watch interface for real-time activity streaming.
// For complex queries with time ranges, search, and CEL filters, use ActivityQuery instead.
type ActivityStorage struct {
	storage ActivityStorageInterface
	watcher activitywatch.WatcherInterface
}

// NewActivityStorage creates a new REST storage for Activity.
func NewActivityStorage(s ActivityStorageInterface) *ActivityStorage {
	return &ActivityStorage{
		storage: s,
	}
}

// NewActivityStorageWithWatcher creates a new REST storage for Activity with watch support.
func NewActivityStorageWithWatcher(s ActivityStorageInterface, w activitywatch.WatcherInterface) *ActivityStorage {
	return &ActivityStorage{
		storage: s,
		watcher: w,
	}
}

var (
	_ rest.Scoper               = &ActivityStorage{}
	_ rest.Storage              = &ActivityStorage{}
	_ rest.Lister               = &ActivityStorage{}
	_ rest.Watcher              = &ActivityStorage{}
	_ rest.SingularNameProvider = &ActivityStorage{}
	_ rest.TableConvertor       = &ActivityStorage{}
)

// New returns an empty Activity.
func (s *ActivityStorage) New() runtime.Object {
	return &v1alpha1.Activity{}
}

// Destroy cleans up resources.
func (s *ActivityStorage) Destroy() {}

// NewList returns an empty ActivityList.
func (s *ActivityStorage) NewList() runtime.Object {
	return &v1alpha1.ActivityList{}
}

// NamespaceScoped returns true because Activity is namespace-scoped.
func (s *ActivityStorage) NamespaceScoped() bool {
	return true
}

// GetSingularName returns the singular name of the resource.
func (s *ActivityStorage) GetSingularName() string {
	return "activity"
}

// DefaultListTimeWindow is the default time window for List operations.
// List is intended for real-time use cases (catch-up before Watch).
// For historical queries, use ActivityQuery instead.
const DefaultListTimeWindow = "now-1h"

// List returns a list of activities matching the query.
// Uses standard Kubernetes field selectors for filtering.
// Automatically constrains to the last hour for performance.
// For historical queries with custom time ranges, use ActivityQuery instead.
func (s *ActivityStorage) List(ctx context.Context, options *metainternalversion.ListOptions) (runtime.Object, error) {
	namespace, _ := request.NamespaceFrom(ctx)

	reqUser, ok := request.UserFrom(ctx)
	if !ok {
		klog.Error("No user in context for activity list request")
		return nil, errors.NewServiceUnavailable("Unable to process request. Please try again.")
	}
	scopeCtx := scope.ExtractScopeFromUser(reqUser)

	spec := storage.ActivityQuerySpec{
		Limit:     int32(options.Limit),
		Continue:  options.Continue,
		StartTime: DefaultListTimeWindow, // Default to last hour for performance
	}

	// Build CEL filter for namespace scoping when namespace is set
	if namespace != "" {
		spec.Filter = fmt.Sprintf("metadata.namespace == '%s'", escapeCELString(namespace))
	}

	// Parse standard field selectors. The watch-only startTime and maxAge
	// selectors are ignored; List is always bounded to DefaultListTimeWindow.
	if options.FieldSelector != nil && !options.FieldSelector.Empty() {
		if err := parseFieldSelector(options.FieldSelector, &spec); err != nil {
			return nil, errors.NewBadRequest(err.Error())
		}
	}

	klog.V(4).InfoS("Listing activities",
		"namespace", namespace,
		"scope", scopeCtx.Type,
		"limit", spec.Limit,
		"startTime", spec.StartTime,
	)

	// Validate the final filter through the same CEL-to-SQL conversion the
	// storage layer uses, so unsupported expressions are a 400, not a 503.
	if spec.Filter != "" {
		if _, _, err := cel.ConvertActivityToClickHouseSQL(ctx, spec.Filter); err != nil {
			return nil, errors.NewBadRequest(fmt.Sprintf("invalid field selector: %v", err))
		}
	}

	result, err := s.storage.QueryActivities(ctx, spec, scopeCtx)
	if err != nil {
		klog.ErrorS(err, "Failed to query activities", "namespace", namespace, "scope", scopeCtx.Type)
		return nil, errors.NewServiceUnavailable("Failed to retrieve activities. Please try again later.")
	}

	list := &v1alpha1.ActivityList{
		TypeMeta: metav1.TypeMeta{
			APIVersion: v1alpha1.SchemeGroupVersion.String(),
			Kind:       "ActivityList",
		},
	}

	for _, activityJSON := range result.Activities {
		var activity v1alpha1.Activity
		if err := json.Unmarshal([]byte(activityJSON), &activity); err != nil {
			klog.ErrorS(err, "Failed to unmarshal activity")
			continue
		}
		list.Items = append(list.Items, activity)
	}

	list.Continue = result.Continue

	return list, nil
}

// Watch returns a watch.Interface that watches activities matching the query options.
func (s *ActivityStorage) Watch(ctx context.Context, options *metainternalversion.ListOptions) (watch.Interface, error) {
	if s.watcher == nil {
		return nil, errors.NewServiceUnavailable("Watch API is not available. NATS is not configured.")
	}

	namespace, _ := request.NamespaceFrom(ctx)

	reqUser, ok := request.UserFrom(ctx)
	if !ok {
		klog.Error("No user in context for activity watch request")
		return nil, errors.NewServiceUnavailable("Unable to process request. Please try again.")
	}
	scopeCtx := scope.ExtractScopeFromUser(reqUser)

	filter := activitywatch.WatchFilter{
		Namespace: namespace,
	}

	// Parse resourceVersion for replay support
	if options.ResourceVersion != "" && options.ResourceVersion != "0" {
		rv, err := strconv.ParseUint(options.ResourceVersion, 10, 64)
		if err != nil {
			klog.V(4).InfoS("Invalid resourceVersion, starting from now", "resourceVersion", options.ResourceVersion)
		} else {
			filter.ResourceVersion = rv
		}
	}

	// Every field selector, including the request namespace, is enforced
	// per message through the CEL filter. Platform-scope watches subscribe to
	// every subject, so the subject alone never filters for them.
	var clauses []string
	if namespace != "" {
		clauses = append(clauses, celEquality("metadata.namespace", namespace))
	}

	// Parse field selectors for watch filtering
	if options.FieldSelector != nil && !options.FieldSelector.Empty() {
		selectorCEL, err := fieldSelectorToCEL(options.FieldSelector)
		if err != nil {
			return nil, errors.NewBadRequest(err.Error())
		}
		if selectorCEL != "" {
			clauses = append(clauses, selectorCEL)
		}

		filter.StartTime, filter.MaxAge, err = parseWatchTimeBounds(options.FieldSelector, time.Now())
		if err != nil {
			return nil, errors.NewBadRequest(err.Error())
		}

		// Exact matches also narrow the NATS subject (apiGroup, kind) and
		// short-circuit before CEL (changeSource).
		if value, found := options.FieldSelector.RequiresExactMatch("spec.changeSource"); found {
			filter.ChangeSource = value
		}
		if value, found := options.FieldSelector.RequiresExactMatch("spec.resource.apiGroup"); found {
			filter.APIGroup = value
		}
		if value, found := options.FieldSelector.RequiresExactMatch("spec.resource.kind"); found {
			filter.ResourceKind = value
		}
	}
	filter.CELFilter = strings.Join(clauses, " && ")

	// Backstop: the parts are validated individually, but the combined
	// expression must also compile (e.g. it may exceed the CEL size limit).
	if filter.CELFilter != "" {
		if _, err := cel.CompileActivityFilterProgram(filter.CELFilter); err != nil {
			return nil, errors.NewBadRequest(fmt.Sprintf("invalid field selector: %v", err))
		}
	}

	klog.V(4).InfoS("Starting activity watch",
		"namespace", namespace,
		"scope", scopeCtx.Type,
		"filter", filter,
	)

	w, err := s.watcher.Watch(ctx, scopeCtx, filter)
	if stderrors.Is(err, activitywatch.ErrInvalidFilter) {
		return nil, errors.NewBadRequest(err.Error())
	}
	return w, err
}

// ConvertToTable converts activities to table format for kubectl display.
func (s *ActivityStorage) ConvertToTable(ctx context.Context, object runtime.Object, tableOptions runtime.Object) (*metav1.Table, error) {
	table := &metav1.Table{
		ColumnDefinitions: []metav1.TableColumnDefinition{
			{Name: "Name", Type: "string", Description: "Activity name"},
			{Name: "Summary", Type: "string", Description: "Activity summary"},
			{Name: "Actor", Type: "string", Description: "Who performed the action"},
			{Name: "Resource", Type: "string", Description: "Affected resource"},
			{Name: "Source", Type: "string", Description: "Change source (human/system)"},
			{Name: "Age", Type: "string", Description: "Time since activity"},
		},
	}

	switch t := object.(type) {
	case *v1alpha1.Activity:
		table.Rows = append(table.Rows, activityToTableRow(t))
	case *v1alpha1.ActivityList:
		for i := range t.Items {
			table.Rows = append(table.Rows, activityToTableRow(&t.Items[i]))
		}
	}

	return table, nil
}

func activityToTableRow(activity *v1alpha1.Activity) metav1.TableRow {
	summary := activity.Spec.Summary
	if len(summary) > 60 {
		summary = summary[:57] + "..."
	}

	resource := activity.Spec.Resource.Kind
	if activity.Spec.Resource.Name != "" {
		resource += "/" + activity.Spec.Resource.Name
	}

	age := "<unknown>"
	if !activity.CreationTimestamp.IsZero() {
		age = duration.HumanDuration(time.Since(activity.CreationTimestamp.Time))
	}

	return metav1.TableRow{
		Object: runtime.RawExtension{Object: activity},
		Cells: []interface{}{
			activity.Name,
			summary,
			activity.Spec.Actor.Name,
			resource,
			activity.Spec.ChangeSource,
			age,
		},
	}
}

// escapeCELString escapes a value so it can be safely interpolated into a
// CEL string literal delimited by single quotes: backslashes and single
// quotes are backslash-escaped, and control characters (which may not appear
// raw in a single-quoted literal) become \uXXXX escapes.
func escapeCELString(s string) string {
	var b strings.Builder
	b.Grow(len(s))
	for _, r := range s {
		switch {
		case r == '\\':
			b.WriteString(`\\`)
		case r == '\'':
			b.WriteString(`\'`)
		case unicode.IsControl(r) || r == '\u2028' || r == '\u2029':
			fmt.Fprintf(&b, `\u%04x`, r)
		default:
			b.WriteRune(r)
		}
	}
	return b.String()
}

// celEquality builds a CEL equality expression with proper escaping, e.g.
// celEquality("spec.actor.name", "alice") => "spec.actor.name == 'alice'"
func celEquality(field, value string) string {
	return fmt.Sprintf("%s == '%s'", field, escapeCELString(value))
}

// celInequality builds a CEL inequality expression with proper escaping, e.g.
// celInequality("spec.actor.name", "alice") => "spec.actor.name != 'alice'"
func celInequality(field, value string) string {
	return fmt.Sprintf("%s != '%s'", field, escapeCELString(value))
}

// celFieldSelectors are the field selectors that translate to a CEL
// comparison. Each selector name is also its CEL path.
var celFieldSelectors = map[string]bool{
	"metadata.name":           true,
	"metadata.namespace":      true,
	"spec.changeSource":       true,
	"spec.resource.apiGroup":  true,
	"spec.resource.kind":      true,
	"spec.resource.name":      true,
	"spec.resource.namespace": true,
	"spec.resource.uid":       true,
	"spec.actor.name":         true,
	"spec.actor.type":         true,
	"spec.actor.uid":          true,
}

// fieldSelectorToCEL translates field selector requirements into a single CEL
// expression: '=' and '==' become equality, '!=' inequality, and the filter
// selector's expression is parenthesised. All clauses are ANDed. The
// watch-only startTime and maxAge selectors are skipped.
//
// The filter expression is validated on its own before it is combined, so an
// unbalanced expression such as "true) || (true" is rejected rather than
// escaping its parentheses and widening the other clauses.
func fieldSelectorToCEL(selector fields.Selector) (string, error) {
	if selector == nil || selector.Empty() {
		return "", nil
	}

	var clauses []string
	for _, req := range selector.Requirements() {
		switch {
		case req.Field == v1alpha1.ActivityFilterFieldSelector:
			if req.Operator == selection.NotEquals {
				return "", fmt.Errorf("field selector %q only supports '='", req.Field)
			}
			if _, err := cel.CompileActivityFilter(req.Value); err != nil {
				return "", fmt.Errorf("invalid filter field selector: %w", err)
			}
			// The newline ends a trailing // comment before the closing paren.
			clauses = append(clauses, "("+req.Value+"\n)")

		case req.Field == v1alpha1.ActivityStartTimeFieldSelector,
			req.Field == v1alpha1.ActivityMaxAgeFieldSelector:
			continue

		case celFieldSelectors[req.Field]:
			switch req.Operator {
			case selection.Equals, selection.DoubleEquals:
				clauses = append(clauses, celEquality(req.Field, req.Value))
			case selection.NotEquals:
				clauses = append(clauses, celInequality(req.Field, req.Value))
			default:
				return "", fmt.Errorf("field selector %q does not support operator %q", req.Field, req.Operator)
			}

		case req.Field == "spec.actor.email":
			return "", fmt.Errorf("field selector %q is not supported for filtering; use spec.actor.uid or spec.actor.name", req.Field)

		default:
			return "", fmt.Errorf("field selector %q is not supported for filtering", req.Field)
		}
	}

	return strings.Join(clauses, " && "), nil
}

// parseWatchTimeBounds reads the watch-only startTime and maxAge field
// selectors. startTime accepts RFC3339 or a relative time ("now-5m") and is
// resolved against now; maxAge is a positive Go duration ("10m").
func parseWatchTimeBounds(selector fields.Selector, now time.Time) (time.Time, time.Duration, error) {
	var startTime time.Time
	var maxAge time.Duration

	for _, req := range selector.Requirements() {
		if req.Field != v1alpha1.ActivityStartTimeFieldSelector && req.Field != v1alpha1.ActivityMaxAgeFieldSelector {
			continue
		}
		if req.Operator == selection.NotEquals {
			return time.Time{}, 0, fmt.Errorf("field selector %q only supports '='", req.Field)
		}

		switch req.Field {
		case v1alpha1.ActivityStartTimeFieldSelector:
			t, err := parseWatchStartTime(req.Value, now)
			if err != nil {
				return time.Time{}, 0, fmt.Errorf("invalid startTime field selector: %w", err)
			}
			startTime = t
		case v1alpha1.ActivityMaxAgeFieldSelector:
			d, err := time.ParseDuration(req.Value)
			if err != nil {
				return time.Time{}, 0, fmt.Errorf("invalid maxAge field selector %q: use a Go duration such as 10m or 1h", req.Value)
			}
			if d <= 0 {
				return time.Time{}, 0, fmt.Errorf("invalid maxAge field selector %q: must be positive", req.Value)
			}
			maxAge = d
		}
	}

	return startTime, maxAge, nil
}

// parseWatchStartTime parses startTime like ActivityQuery does, except that
// an absolute time in the future (client clock skew, or a client sending its
// own "now") is clamped to now instead of rejected.
func parseWatchStartTime(value string, now time.Time) (time.Time, error) {
	t, err := timeutil.ParseFlexibleTime(value, now)
	if err == nil {
		return t, nil
	}
	for _, layout := range []string{time.RFC3339, time.RFC3339Nano} {
		if ft, perr := time.Parse(layout, value); perr == nil && ft.After(now) {
			return now, nil
		}
	}
	return time.Time{}, err
}

// parseFieldSelector translates field selectors into CEL (see
// fieldSelectorToCEL) and ANDs the result into spec.Filter. CEL is the sole
// filtering mechanism for ActivityQuerySpec beyond time range and search.
func parseFieldSelector(selector fields.Selector, spec *storage.ActivityQuerySpec) error {
	combined, err := fieldSelectorToCEL(selector)
	if err != nil {
		return err
	}
	if combined == "" {
		return nil
	}

	if spec.Filter != "" {
		spec.Filter = "(" + spec.Filter + ") && " + combined
	} else {
		spec.Filter = combined
	}
	return nil
}
