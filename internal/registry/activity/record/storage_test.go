package record

import (
	"context"
	"strings"
	"testing"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metainternalversion "k8s.io/apimachinery/pkg/apis/meta/internalversion"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/fields"
	"k8s.io/apimachinery/pkg/watch"
	"k8s.io/apiserver/pkg/authentication/user"
	"k8s.io/apiserver/pkg/endpoints/request"

	"go.miloapis.com/activity/internal/apiserver/filterparam"
	activitycel "go.miloapis.com/activity/internal/cel"
	"go.miloapis.com/activity/internal/storage"
	activitywatch "go.miloapis.com/activity/internal/watch"
	"go.miloapis.com/activity/pkg/apis/activity/v1alpha1"
)

// mockWatcher records the filter it was asked to watch with.
type mockWatcher struct {
	filter activitywatch.WatchFilter
}

func (m *mockWatcher) Watch(_ context.Context, _ storage.ScopeContext, filter activitywatch.WatchFilter) (watch.Interface, error) {
	m.filter = filter
	return watch.NewEmptyWatch(), nil
}

// mockActivityStorage records the query spec it was asked to run.
type mockActivityStorage struct {
	spec storage.ActivityQuerySpec
}

func (m *mockActivityStorage) QueryActivities(_ context.Context, spec storage.ActivityQuerySpec, _ storage.ScopeContext) (*storage.ActivityQueryResult, error) {
	m.spec = spec
	return &storage.ActivityQueryResult{}, nil
}

func userContext() context.Context {
	return request.WithUser(context.Background(), &user.DefaultInfo{Name: "alice"})
}

func mustParseSelector(t *testing.T, s string) fields.Selector {
	t.Helper()
	sel, err := fields.ParseSelector(s)
	if err != nil {
		t.Fatalf("fields.ParseSelector(%q) error = %v", s, err)
	}
	return sel
}

// mustMatch compiles filter and reports whether it matches activity.
func mustMatch(t *testing.T, filter string, activity *v1alpha1.Activity) bool {
	t.Helper()
	prog, err := activitycel.CompileActivityFilterProgram(filter)
	if err != nil {
		t.Fatalf("CompileActivityFilterProgram(%q) error = %v", filter, err)
	}
	match, err := prog.EvaluateActivity(activity)
	if err != nil {
		t.Fatalf("EvaluateActivity() error = %v", err)
	}
	return match
}

func TestFieldSelectorToCEL(t *testing.T) {
	tests := []struct {
		name     string
		selector string
		want     string
		wantErr  bool
	}{
		{name: "empty", selector: "", want: ""},
		{name: "equals", selector: "spec.resource.uid=abc", want: "spec.resource.uid == 'abc'"},
		{name: "double equals", selector: "spec.actor.name==alice", want: "spec.actor.name == 'alice'"},
		{name: "not equals", selector: "spec.changeSource!=system", want: "spec.changeSource != 'system'"},
		{
			name:     "multiple requirements",
			selector: "spec.resource.namespace=default,spec.resource.apiGroup=compute.datumapis.com",
			want:     "spec.resource.apiGroup == 'compute.datumapis.com' && spec.resource.namespace == 'default'",
		},
		{name: "escapes quotes", selector: "spec.resource.name=it's", want: `spec.resource.name == 'it\'s'`},
		{name: "unsupported field", selector: "spec.actor.email=a@b.c", wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := fieldSelectorToCEL(mustParseSelector(t, tt.selector))
			if (err != nil) != tt.wantErr {
				t.Fatalf("fieldSelectorToCEL(%q) error = %v, wantErr %v", tt.selector, err, tt.wantErr)
			}
			if got != tt.want {
				t.Errorf("fieldSelectorToCEL(%q) = %q, want %q", tt.selector, got, tt.want)
			}
		})
	}
}

// TestFieldSelectorToCEL_SupportedSelectors keeps the registered field
// selectors in sync with what the watch evaluator and ClickHouse can filter on.
func TestFieldSelectorToCEL_SupportedSelectors(t *testing.T) {
	for _, label := range v1alpha1.SupportedActivityFieldSelectors {
		t.Run(label, func(t *testing.T) {
			if _, _, err := v1alpha1.ActivityFieldLabelConversionFunc(label, "x"); err != nil {
				t.Errorf("ActivityFieldLabelConversionFunc(%q) error = %v", label, err)
			}
			got, err := fieldSelectorToCEL(fields.OneTermEqualSelector(label, "x"))
			if err != nil {
				t.Fatalf("fieldSelectorToCEL(%q) error = %v", label, err)
			}
			if _, err := activitycel.CompileActivityFilterProgram(got); err != nil {
				t.Errorf("CompileActivityFilterProgram(%q) error = %v", got, err)
			}
			if _, _, err := activitycel.ConvertActivityToClickHouseSQL(context.Background(), got); err != nil {
				t.Errorf("ConvertActivityToClickHouseSQL(%q) error = %v", got, err)
			}
		})
	}
}

func TestEscapeCELString_RoundTrip(t *testing.T) {
	for _, value := range []string{"plain", `it's`, `back\slash`, "multi\nline\r\n", "tab\there"} {
		filter := celComparison("spec.resource.name", "==", value)
		activity := &v1alpha1.Activity{Spec: v1alpha1.ActivitySpec{Resource: v1alpha1.ActivityResource{Name: value}}}
		if !mustMatch(t, filter, activity) {
			t.Errorf("filter %q does not match resource name %q", filter, value)
		}
	}
}

func TestRequestFilter(t *testing.T) {
	apiGroup := fields.OneTermEqualSelector("spec.resource.apiGroup", "compute.datumapis.com")

	tests := []struct {
		name      string
		namespace string
		selector  fields.Selector
		filter    string
		want      string
		wantErr   bool
	}{
		{name: "none", want: ""},
		{name: "namespace only", namespace: "default", want: "metadata.namespace == 'default'"},
		{
			name:      "namespace, selector and filter",
			namespace: "default",
			selector:  apiGroup,
			filter:    "spec.resource.name == 'a'",
			want:      "metadata.namespace == 'default' && spec.resource.apiGroup == 'compute.datumapis.com' && (spec.resource.name == 'a'\n)",
		},
		{name: "wildcard namespace", namespace: "*", wantErr: true},
		{name: "dotted namespace", namespace: "a.b", wantErr: true},
		{name: "selector only", selector: apiGroup, want: "spec.resource.apiGroup == 'compute.datumapis.com'"},
		{name: "filter only", filter: "spec.changeSource == 'human'", want: "(spec.changeSource == 'human'\n)"},
		{
			name:     "selector and filter",
			selector: apiGroup,
			filter:   "spec.resource.name == 'a'",
			want:     "spec.resource.apiGroup == 'compute.datumapis.com' && (spec.resource.name == 'a'\n)",
		},
		{
			name:     "filter with trailing line comment",
			selector: apiGroup,
			filter:   "spec.resource.name == 'a' // workload a",
			want:     "spec.resource.apiGroup == 'compute.datumapis.com' && (spec.resource.name == 'a' // workload a\n)",
		},
		{name: "unknown field in filter", filter: "spec.nope == 'x'", wantErr: true},
		{name: "filter escaping its parentheses", selector: apiGroup, filter: "true) || (true", wantErr: true},
		{name: "selector value with newline", selector: fields.OneTermEqualSelector("spec.resource.name", "a\nb\r'c\\"), want: `spec.resource.name == 'a\nb\r\'c\\'`},
		{name: "filter too long", filter: "spec.resource.name == '" + strings.Repeat("a", maxFilterLength) + "'", wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx := request.WithNamespace(context.Background(), tt.namespace)
			ctx = filterparam.NewContext(ctx, tt.filter)

			got, err := requestFilter(ctx, tt.selector)
			if (err != nil) != tt.wantErr {
				t.Fatalf("requestFilter() error = %v, wantErr %v", err, tt.wantErr)
			}
			if got != tt.want {
				t.Errorf("requestFilter() = %q, want %q", got, tt.want)
			}
		})
	}
}

// TestActivityStorage_Watch_Filter is the regression test for #262: the
// namespace, every field selector and the `filter` parameter must reach the
// watcher's CEL filter.
func TestActivityStorage_Watch_Filter(t *testing.T) {
	mw := &mockWatcher{}
	s := NewActivityStorageWithWatcher(nil, mw)

	ctx := request.WithNamespace(userContext(), "default")
	ctx = filterparam.NewContext(ctx, "spec.resource.name == 'a'")
	opts := &metainternalversion.ListOptions{
		FieldSelector: mustParseSelector(t, "spec.resource.apiGroup=compute.datumapis.com,spec.resource.uid=uid-a"),
	}
	if _, err := s.Watch(ctx, opts); err != nil {
		t.Fatalf("Watch() error = %v", err)
	}

	match := v1alpha1.ActivityResource{APIGroup: "compute.datumapis.com", Name: "a", UID: "uid-a"}
	tests := []struct {
		name      string
		namespace string
		resource  v1alpha1.ActivityResource
		want      bool
	}{
		{name: "matching resource", namespace: "default", resource: match, want: true},
		{name: "other workload", namespace: "default", resource: v1alpha1.ActivityResource{APIGroup: "compute.datumapis.com", Name: "b", UID: "uid-b"}, want: false},
		{name: "uid mismatch", namespace: "default", resource: v1alpha1.ActivityResource{APIGroup: "compute.datumapis.com", Name: "a", UID: "uid-b"}, want: false},
		{name: "other namespace", namespace: "other", resource: match, want: false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			activity := &v1alpha1.Activity{
				ObjectMeta: metav1.ObjectMeta{Namespace: tt.namespace},
				Spec:       v1alpha1.ActivitySpec{Resource: tt.resource},
			}
			if got := mustMatch(t, mw.filter.CELFilter, activity); got != tt.want {
				t.Errorf("CELFilter %q matched = %v, want %v", mw.filter.CELFilter, got, tt.want)
			}
		})
	}
}

func TestActivityStorage_BadRequest(t *testing.T) {
	tests := []struct {
		name      string
		namespace string
		selector  string
		filter    string
	}{
		{name: "wildcard namespace", namespace: "*"},
		{name: "subject-breaking namespace", namespace: ">"},
		{name: "unsupported selector", selector: "spec.actor.email=a@b.c"},
		{name: "invalid filter", filter: "not valid cel ((("},
		// Valid CEL that ClickHouse can't run is rejected on both, matching ActivityQuery.
		{name: "comprehension", filter: "spec.relatedUIDs.all(a, true)"},
		{name: "size", filter: "size(spec.resource.name) > 1"},
		{name: "matches", filter: "spec.resource.name.matches('^a')"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := NewActivityStorageWithWatcher(&mockActivityStorage{}, &mockWatcher{})
			ctx := request.WithNamespace(userContext(), tt.namespace)
			ctx = filterparam.NewContext(ctx, tt.filter)
			opts := &metainternalversion.ListOptions{FieldSelector: mustParseSelector(t, tt.selector)}

			if _, err := s.List(ctx, opts); !apierrors.IsBadRequest(err) {
				t.Errorf("List() error = %v, want BadRequest", err)
			}
			if _, err := s.Watch(ctx, opts); !apierrors.IsBadRequest(err) {
				t.Errorf("Watch() error = %v, want BadRequest", err)
			}
		})
	}
}

func TestActivityStorage_List_Filter(t *testing.T) {
	tests := []struct {
		name      string
		selector  string
		filter    string
		namespace string
		want      bool
	}{
		{name: "matches selector and filter", selector: "spec.resource.namespace=prod", filter: "spec.resource.name == 'a'", namespace: "default", want: true},
		{name: "selector excludes", selector: "spec.resource.namespace=staging", filter: "spec.resource.name == 'a'", namespace: "default", want: false},
		{name: "filter excludes", selector: "spec.resource.namespace=prod", filter: "spec.resource.name == 'b'", namespace: "default", want: false},
		// The request namespace must stay a hard boundary, even for `|| true`.
		{name: "filter cannot escape namespace", filter: "spec.resource.name == 'x' || true", namespace: "other", want: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ms := &mockActivityStorage{}
			s := NewActivityStorage(ms)

			ctx := request.WithNamespace(userContext(), "default")
			ctx = filterparam.NewContext(ctx, tt.filter)
			opts := &metainternalversion.ListOptions{FieldSelector: mustParseSelector(t, tt.selector)}
			if _, err := s.List(ctx, opts); err != nil {
				t.Fatalf("List() error = %v", err)
			}

			activity := &v1alpha1.Activity{
				ObjectMeta: metav1.ObjectMeta{Name: "act", Namespace: tt.namespace},
				Spec: v1alpha1.ActivitySpec{
					Resource: v1alpha1.ActivityResource{Name: "a", Namespace: "prod"},
				},
			}
			if got := mustMatch(t, ms.spec.Filter, activity); got != tt.want {
				t.Errorf("Filter %q matched = %v, want %v", ms.spec.Filter, got, tt.want)
			}
		})
	}
}
