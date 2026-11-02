package record

import (
	"context"
	"fmt"
	"net/url"
	"strings"
	"testing"
	"time"

	"k8s.io/apimachinery/pkg/api/errors"
	metainternalversion "k8s.io/apimachinery/pkg/apis/meta/internalversion"
	metainternalversionscheme "k8s.io/apimachinery/pkg/apis/meta/internalversion/scheme"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/fields"
	"k8s.io/apimachinery/pkg/watch"
	"k8s.io/apiserver/pkg/authentication/user"
	"k8s.io/apiserver/pkg/endpoints/request"

	"go.miloapis.com/activity/internal/cel"
	"go.miloapis.com/activity/internal/registry/scope"
	"go.miloapis.com/activity/internal/storage"
	activitywatch "go.miloapis.com/activity/internal/watch"
	"go.miloapis.com/activity/pkg/apis/activity/v1alpha1"
)

// fakeWatcher records the filter it was called with.
type fakeWatcher struct {
	called bool
	scope  storage.ScopeContext
	filter activitywatch.WatchFilter
}

func (f *fakeWatcher) Watch(_ context.Context, s storage.ScopeContext, filter activitywatch.WatchFilter) (watch.Interface, error) {
	f.called = true
	f.scope = s
	f.filter = filter
	return watch.NewEmptyWatch(), nil
}

// errWatcher returns a fixed error.
type errWatcher struct{ err error }

func (e errWatcher) Watch(context.Context, storage.ScopeContext, activitywatch.WatchFilter) (watch.Interface, error) {
	return nil, e.err
}

// fakeActivityStorage records the query spec it was called with.
type fakeActivityStorage struct {
	spec storage.ActivityQuerySpec
}

func (f *fakeActivityStorage) QueryActivities(_ context.Context, spec storage.ActivityQuerySpec, _ storage.ScopeContext) (*storage.ActivityQueryResult, error) {
	f.spec = spec
	return &storage.ActivityQueryResult{}, nil
}

func requestContext(namespace string) context.Context {
	ctx := request.WithUser(context.Background(), &user.DefaultInfo{
		Name: "alice",
		Extra: map[string][]string{
			scope.ParentKindExtraKey: {"Project"},
			scope.ParentNameExtraKey: {"prod"},
		},
	})
	if namespace != "" {
		ctx = request.WithNamespace(ctx, namespace)
	}
	return ctx
}

// listOptions decodes a raw fieldSelector query parameter the way the API
// server handler does: query-parameter decoding (fields.ParseSelector,
// which unescapes values) followed by the Activity field label conversion.
func listOptions(t *testing.T, rawFieldSelector string) *metainternalversion.ListOptions {
	t.Helper()
	opts := &metainternalversion.ListOptions{}
	values := url.Values{"fieldSelector": {rawFieldSelector}}
	if err := metainternalversionscheme.ParameterCodec.DecodeParameters(values, metav1.SchemeGroupVersion, opts); err != nil {
		t.Fatalf("decode parameters: %v", err)
	}
	if opts.FieldSelector != nil {
		sel, err := opts.FieldSelector.Transform(v1alpha1.ActivityFieldLabelConversionFunc)
		if err != nil {
			t.Fatalf("field label conversion: %v", err)
		}
		opts.FieldSelector = sel
	}
	return opts
}

func TestWatchFilterFieldSelectorEndToEnd(t *testing.T) {
	expr := `spec.actor.name == "alice" && spec.resource.kind != "Secret" && spec.resource.name in ["a,b", "c=d"]`
	raw := "spec.changeSource=human,filter=" + fields.EscapeValue(expr)

	// The raw selector would not parse without escaping.
	if _, err := fields.ParseSelector("filter=" + expr); err == nil {
		t.Fatal("expected unescaped filter to be rejected by fields.ParseSelector")
	}

	w := &fakeWatcher{}
	s := NewActivityStorageWithWatcher(nil, w)
	if _, err := s.Watch(requestContext("default"), listOptions(t, raw)); err != nil {
		t.Fatalf("Watch() error = %v", err)
	}

	want := "metadata.namespace == 'default' && (" + expr + "\n) && spec.changeSource == 'human'"
	if w.filter.CELFilter != want {
		t.Errorf("CELFilter = %q\nwant        %q", w.filter.CELFilter, want)
	}
	if w.filter.ChangeSource != "human" || w.filter.Namespace != "default" {
		t.Errorf("subject filters = %+v, want changeSource=human namespace=default", w.filter)
	}
	if w.scope.Type != "Project" || w.scope.Name != "prod" {
		t.Errorf("scope = %+v, want Project/prod", w.scope)
	}

	// The combined expression must compile in the watcher.
	if _, err := cel.CompileActivityFilterProgram(w.filter.CELFilter); err != nil {
		t.Errorf("combined CEL does not compile: %v", err)
	}
}

func TestWatchSelectorTranslation(t *testing.T) {
	raw := strings.Join([]string{
		"spec.resource.apiGroup=networking.datumapis.com",
		"spec.resource.kind==HTTPProxy",
		"spec.actor.name!=o'brien",
		"spec.resource.name=a\\\\b",
		"spec.resource.namespace=team-a",
		"spec.resource.uid=uid-1",
		"spec.actor.type=user",
		"spec.actor.uid!=system",
		"metadata.name=act-1",
	}, ",")

	w := &fakeWatcher{}
	s := NewActivityStorageWithWatcher(nil, w)
	if _, err := s.Watch(requestContext(""), listOptions(t, raw)); err != nil {
		t.Fatalf("Watch() error = %v", err)
	}

	wantClauses := []string{
		`metadata.name == 'act-1'`,
		`spec.actor.name != 'o\'brien'`,
		`spec.actor.type == 'user'`,
		`spec.actor.uid != 'system'`,
		`spec.resource.apiGroup == 'networking.datumapis.com'`,
		`spec.resource.kind == 'HTTPProxy'`,
		`spec.resource.name == 'a\\b'`,
		`spec.resource.namespace == 'team-a'`,
		`spec.resource.uid == 'uid-1'`,
	}
	for _, clause := range wantClauses {
		if !strings.Contains(w.filter.CELFilter, clause) {
			t.Errorf("CELFilter %q missing clause %q", w.filter.CELFilter, clause)
		}
	}
	if strings.Contains(w.filter.CELFilter, "metadata.namespace") {
		t.Errorf("CELFilter %q should not constrain namespace for a cluster-wide request", w.filter.CELFilter)
	}

	// Exact matches still narrow the subject; inequalities do not.
	if w.filter.APIGroup != "networking.datumapis.com" || w.filter.ResourceKind != "HTTPProxy" {
		t.Errorf("subject filters = %+v", w.filter)
	}

	compiled, err := cel.CompileActivityFilterProgram(w.filter.CELFilter)
	if err != nil {
		t.Fatalf("combined CEL does not compile: %v", err)
	}
	activity := &v1alpha1.Activity{
		ObjectMeta: metav1.ObjectMeta{Name: "act-1"},
		Spec: v1alpha1.ActivitySpec{
			Actor: v1alpha1.ActivityActor{Name: "alice", Type: "user", UID: "u-1"},
			Resource: v1alpha1.ActivityResource{
				APIGroup: "networking.datumapis.com", Kind: "HTTPProxy",
				Name: `a\b`, Namespace: "team-a", UID: "uid-1",
			},
		},
	}
	if ok, err := compiled.EvaluateActivity(activity); err != nil || !ok {
		t.Errorf("EvaluateActivity() = %v, %v; want true", ok, err)
	}
	activity.Spec.Actor.Name = "o'brien"
	if ok, _ := compiled.EvaluateActivity(activity); ok {
		t.Error("expected inequality to exclude o'brien")
	}
}

func TestWatchBadRequests(t *testing.T) {
	tests := []struct {
		name string
		raw  string
	}{
		{"invalid CEL", "filter=" + fields.EscapeValue("spec.actor.name ==")},
		{"unknown CEL field", "filter=" + fields.EscapeValue("spec.nope == 'x'")},
		{"non-boolean CEL", "filter=" + fields.EscapeValue("spec.actor.name")},
		{"parenthesis escape", "filter=" + fields.EscapeValue("true) || (true")},
		{"empty filter", "filter="},
		{"filter with !=", "filter!=" + fields.EscapeValue("true")},
		{"unsupported actor email", "spec.actor.email=a@example.com"},
		{"invalid startTime", "startTime=yesterday"},
		{"future relative startTime", "startTime=now+5m"},
		{"startTime with !=", "startTime!=now-5m"},
		{"invalid maxAge", "maxAge=10 minutes"},
		{"zero maxAge", "maxAge=0s"},
		{"negative maxAge", "maxAge=-5m"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			w := &fakeWatcher{}
			s := NewActivityStorageWithWatcher(nil, w)
			_, err := s.Watch(requestContext("default"), listOptions(t, tt.raw))
			if !errors.IsBadRequest(err) {
				t.Fatalf("Watch() error = %v, want 400 Bad Request", err)
			}
			if w.called {
				t.Error("watcher should not be called for an invalid request")
			}
		})
	}
}

func TestWatchTimeBounds(t *testing.T) {
	w := &fakeWatcher{}
	s := NewActivityStorageWithWatcher(nil, w)
	opts := listOptions(t, "startTime=2026-01-02T03:04:05Z,maxAge=10m")
	if _, err := s.Watch(requestContext(""), opts); err != nil {
		t.Fatalf("Watch() error = %v", err)
	}
	if want := time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC); !w.filter.StartTime.Equal(want) {
		t.Errorf("StartTime = %v, want %v", w.filter.StartTime, want)
	}
	if w.filter.MaxAge != 10*time.Minute {
		t.Errorf("MaxAge = %v, want 10m", w.filter.MaxAge)
	}
	if w.filter.CELFilter != "" {
		t.Errorf("CELFilter = %q, want empty: time bounds are not CEL", w.filter.CELFilter)
	}
}

func TestParseWatchTimeBoundsRelative(t *testing.T) {
	now := time.Date(2026, 11, 2, 12, 0, 0, 0, time.UTC)
	sel, err := fields.ParseSelector("startTime=now-5m")
	if err != nil {
		t.Fatal(err)
	}
	start, maxAge, err := parseWatchTimeBounds(sel, now)
	if err != nil {
		t.Fatalf("parseWatchTimeBounds() error = %v", err)
	}
	if want := now.Add(-5 * time.Minute); !start.Equal(want) {
		t.Errorf("startTime = %v, want %v", start, want)
	}
	if maxAge != 0 {
		t.Errorf("maxAge = %v, want 0", maxAge)
	}
}

func TestListHonoursFilterAndIgnoresTimeBounds(t *testing.T) {
	st := &fakeActivityStorage{}
	s := NewActivityStorage(st)
	raw := "filter=" + fields.EscapeValue("spec.changeSource == 'human'") + ",maxAge=10m,startTime=now-5m"
	if _, err := s.List(requestContext("default"), listOptions(t, raw)); err != nil {
		t.Fatalf("List() error = %v", err)
	}
	want := "(metadata.namespace == 'default') && (spec.changeSource == 'human'\n)"
	if st.spec.Filter != want {
		t.Errorf("spec.Filter = %q, want %q", st.spec.Filter, want)
	}
	if st.spec.StartTime != DefaultListTimeWindow {
		t.Errorf("spec.StartTime = %q, want %q", st.spec.StartTime, DefaultListTimeWindow)
	}
}

func TestListInvalidFilterIsBadRequest(t *testing.T) {
	s := NewActivityStorage(&fakeActivityStorage{})
	_, err := s.List(requestContext(""), listOptions(t, "filter="+fields.EscapeValue("spec.actor.name ==")))
	if !errors.IsBadRequest(err) {
		t.Fatalf("List() error = %v, want 400 Bad Request", err)
	}
}

func TestWatchUnavailableWithoutWatcher(t *testing.T) {
	s := NewActivityStorage(&fakeActivityStorage{})
	_, err := s.Watch(requestContext(""), &metainternalversion.ListOptions{})
	if !errors.IsServiceUnavailable(err) {
		t.Fatalf("Watch() error = %v, want 503", err)
	}
}

func TestListUnsupportedSQLFilterIsBadRequest(t *testing.T) {
	// These compile as CEL (Watch accepts them) but have no ClickHouse
	// translation, so List must reject them up front instead of failing in
	// storage with a 503.
	for _, expr := range []string{
		"spec.actor.name == 'alice' ? true : false",
		"size(spec.actor.name) / 2 == 1",
	} {
		t.Run(expr, func(t *testing.T) {
			if _, err := cel.CompileActivityFilter(expr); err != nil {
				t.Fatalf("precondition: %q should compile as CEL: %v", expr, err)
			}
			st := &fakeActivityStorage{}
			s := NewActivityStorage(st)
			_, err := s.List(requestContext(""), listOptions(t, "filter="+fields.EscapeValue(expr)))
			if !errors.IsBadRequest(err) {
				t.Fatalf("List() error = %v, want 400 Bad Request", err)
			}
		})
	}
}

func TestFilterTrailingCommentDoesNotEatParen(t *testing.T) {
	raw := "spec.changeSource=human,filter=" + fields.EscapeValue("spec.actor.name == 'alice' // only alice")

	w := &fakeWatcher{}
	if _, err := NewActivityStorageWithWatcher(nil, w).Watch(requestContext("default"), listOptions(t, raw)); err != nil {
		t.Fatalf("Watch() error = %v", err)
	}
	compiled, err := cel.CompileActivityFilterProgram(w.filter.CELFilter)
	if err != nil {
		t.Fatalf("combined CEL %q does not compile: %v", w.filter.CELFilter, err)
	}
	activity := &v1alpha1.Activity{
		ObjectMeta: metav1.ObjectMeta{Namespace: "default"},
		Spec: v1alpha1.ActivitySpec{
			ChangeSource: "system",
			Actor:        v1alpha1.ActivityActor{Name: "alice"},
		},
	}
	// The changeSource clause after the filter must still apply.
	if ok, _ := compiled.EvaluateActivity(activity); ok {
		t.Error("expected changeSource clause after the commented filter to still apply")
	}

	st := &fakeActivityStorage{}
	if _, err := NewActivityStorage(st).List(requestContext("default"), listOptions(t, raw)); err != nil {
		t.Fatalf("List() error = %v", err)
	}
}

func TestControlCharactersInSelectorValues(t *testing.T) {
	// Field selector values arrive unescaped from the query string, so they
	// may contain raw newlines and other control characters.
	sel := fields.AndSelectors(
		fields.OneTermEqualSelector("spec.actor.name", "ali\nce\r\x00'\\"),
		fields.OneTermNotEqualSelector("spec.resource.name", "a\tb"),
	)
	opts := &metainternalversion.ListOptions{FieldSelector: sel}
	namespace := "team\na"

	w := &fakeWatcher{}
	if _, err := NewActivityStorageWithWatcher(nil, w).Watch(requestContext(namespace), opts); err != nil {
		t.Fatalf("Watch() error = %v", err)
	}
	compiled, err := cel.CompileActivityFilterProgram(w.filter.CELFilter)
	if err != nil {
		t.Fatalf("combined CEL %q does not compile: %v", w.filter.CELFilter, err)
	}
	activity := &v1alpha1.Activity{
		ObjectMeta: metav1.ObjectMeta{Namespace: namespace},
		Spec: v1alpha1.ActivitySpec{
			Actor:    v1alpha1.ActivityActor{Name: "ali\nce\r\x00'\\"},
			Resource: v1alpha1.ActivityResource{Name: "other"},
		},
	}
	if ok, err := compiled.EvaluateActivity(activity); err != nil || !ok {
		t.Errorf("EvaluateActivity() = %v, %v; want true (values must round-trip exactly)", ok, err)
	}
	activity.Spec.Resource.Name = "a\tb"
	if ok, _ := compiled.EvaluateActivity(activity); ok {
		t.Error("expected inequality on a tab-containing value to exclude it")
	}

	st := &fakeActivityStorage{}
	if _, err := NewActivityStorage(st).List(requestContext(namespace), opts); err != nil {
		t.Fatalf("List() error = %v", err)
	}
}

func TestWatchCombinedFilterBackstop(t *testing.T) {
	// A filter just under CEL's 100k code point limit is valid on its own,
	// but the combined expression (namespace and selector clauses added)
	// exceeds it. That must be a 400, not a 500 from the watcher.
	expr := "spec.actor.name == '" + strings.Repeat("a", 99_950) + "'"
	if _, err := cel.CompileActivityFilter(expr); err != nil {
		t.Fatalf("precondition: filter should compile on its own: %v", err)
	}
	raw := "spec.changeSource=human,spec.resource.kind=HTTPProxy,filter=" + fields.EscapeValue(expr)

	w := &fakeWatcher{}
	_, err := NewActivityStorageWithWatcher(nil, w).Watch(requestContext("default"), listOptions(t, raw))
	if !errors.IsBadRequest(err) {
		t.Fatalf("Watch() error = %v, want 400 Bad Request", err)
	}
	if w.called {
		t.Error("watcher should not be called when the combined filter is invalid")
	}

	_, err = NewActivityStorage(&fakeActivityStorage{}).List(requestContext("default"), listOptions(t, raw))
	if !errors.IsBadRequest(err) {
		t.Fatalf("List() error = %v, want 400 Bad Request", err)
	}
}

func TestWatchMapsWatcherInvalidFilterToBadRequest(t *testing.T) {
	s := NewActivityStorageWithWatcher(nil, errWatcher{err: fmt.Errorf("%w: boom", activitywatch.ErrInvalidFilter)})
	if _, err := s.Watch(requestContext(""), &metainternalversion.ListOptions{}); !errors.IsBadRequest(err) {
		t.Fatalf("Watch() error = %v, want 400 Bad Request", err)
	}

	other := NewActivityStorageWithWatcher(nil, errWatcher{err: fmt.Errorf("nats down")})
	if _, err := other.Watch(requestContext(""), &metainternalversion.ListOptions{}); errors.IsBadRequest(err) {
		t.Fatalf("Watch() error = %v, other watcher errors must not become 400", err)
	}
}

func TestParseWatchStartTimeClampsFuture(t *testing.T) {
	now := time.Date(2026, 11, 2, 12, 0, 0, 0, time.UTC)
	tests := []struct {
		name    string
		value   string
		want    time.Time
		wantErr bool
	}{
		{"past RFC3339", "2026-11-02T11:00:00Z", now.Add(-time.Hour), false},
		{"slightly future (clock skew)", "2026-11-02T12:00:02Z", now, false},
		{"far future", "2999-01-01T00:00:00Z", now, false},
		{"future RFC3339Nano", "2026-11-02T12:00:00.5Z", now, false},
		{"relative", "now-5m", now.Add(-5 * time.Minute), false},
		{"now", "now", now, false},
		{"relative future unsupported", "now+5m", time.Time{}, true},
		{"garbage", "yesterday", time.Time{}, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := parseWatchStartTime(tt.value, now)
			if (err != nil) != tt.wantErr {
				t.Fatalf("parseWatchStartTime(%q) error = %v, wantErr %v", tt.value, err, tt.wantErr)
			}
			if !tt.wantErr && !got.Equal(tt.want) {
				t.Errorf("parseWatchStartTime(%q) = %v, want %v", tt.value, got, tt.want)
			}
		})
	}
}

func TestWatchFutureStartTimeAccepted(t *testing.T) {
	w := &fakeWatcher{}
	before := time.Now()
	if _, err := NewActivityStorageWithWatcher(nil, w).Watch(requestContext(""), listOptions(t, "startTime=2999-01-01T00:00:00Z")); err != nil {
		t.Fatalf("Watch() error = %v", err)
	}
	if w.filter.StartTime.Before(before) || w.filter.StartTime.After(time.Now()) {
		t.Errorf("StartTime = %v, want clamped to the time of the request", w.filter.StartTime)
	}
}
