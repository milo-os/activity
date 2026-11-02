package watch

import (
	"testing"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"go.miloapis.com/activity/internal/cel"
	"go.miloapis.com/activity/internal/storage"
	"go.miloapis.com/activity/internal/types"
	"go.miloapis.com/activity/pkg/apis/activity/v1alpha1"
)

func TestBuildSubject(t *testing.T) {
	w := &NATSWatcher{subjectPrefix: "activities"}

	tests := []struct {
		name   string
		scope  storage.ScopeContext
		filter WatchFilter
		want   string
	}{
		{
			name:  "platform scope watches everything",
			scope: storage.ScopeContext{Type: types.TenantTypePlatform},
			filter: WatchFilter{
				APIGroup: "networking.datumapis.com", ResourceKind: "HTTPProxy", Namespace: "default",
			},
			want: "activities.>",
		},
		{
			name:  "tenant scope encodes dotted API group",
			scope: storage.ScopeContext{Type: types.TenantTypeProject, Name: "prod"},
			filter: WatchFilter{
				APIGroup: "networking.datumapis.com", ResourceKind: "HTTPProxy", Namespace: "default",
			},
			want: "activities.Project.prod.networking_datumapis_com.*.HTTPProxy.default.>",
		},
		{
			name:  "tenant scope without filters",
			scope: storage.ScopeContext{Type: types.TenantTypeOrganization, Name: "acme"},
			want:  "activities.Organization.acme.*.*.*.*.>",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := w.buildSubject(tt.scope, tt.filter); got != tt.want {
				t.Errorf("buildSubject() = %q, want %q", got, tt.want)
			}
		})
	}
}

func activityAt(ts time.Time) *v1alpha1.Activity {
	return &v1alpha1.Activity{
		ObjectMeta: metav1.ObjectMeta{
			Name:              "act-1",
			Namespace:         "default",
			CreationTimestamp: metav1.NewTime(ts),
		},
		Spec: v1alpha1.ActivitySpec{
			ChangeSource: "human",
			Actor:        v1alpha1.ActivityActor{Name: "alice"},
			Resource: v1alpha1.ActivityResource{
				APIGroup: "networking.datumapis.com",
				Kind:     "HTTPProxy",
				Name:     "api-gateway",
			},
		},
	}
}

func TestMatchesFilterCEL(t *testing.T) {
	compiled, err := cel.CompileActivityFilterProgram(
		"spec.actor.name != 'bob' && spec.resource.kind == 'HTTPProxy' && metadata.namespace == 'default'")
	if err != nil {
		t.Fatalf("compile: %v", err)
	}
	w := &ActivityWatch{celFilter: compiled}

	match := activityAt(time.Now())
	if !w.matchesFilter(match) {
		t.Error("expected activity to match CEL filter")
	}

	other := activityAt(time.Now())
	other.Spec.Actor.Name = "bob"
	if w.matchesFilter(other) {
		t.Error("expected activity by bob to be filtered out")
	}

	otherNS := activityAt(time.Now())
	otherNS.Namespace = "kube-system"
	if w.matchesFilter(otherNS) {
		t.Error("expected activity in another namespace to be filtered out")
	}
}

func TestMatchesFilterStartTime(t *testing.T) {
	start := time.Date(2026, 11, 2, 12, 0, 0, 500_000_000, time.UTC)
	w := &ActivityWatch{filter: WatchFilter{StartTime: start}}

	tests := []struct {
		name    string
		created time.Time
		want    bool
	}{
		{"well before", start.Add(-time.Hour), false},
		{"previous second", start.Truncate(time.Second).Add(-time.Second), false},
		// creationTimestamp has second precision; same-second activities are kept.
		{"same second", start.Truncate(time.Second), true},
		{"exactly at start", start, true},
		{"after", start.Add(time.Minute), true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := w.matchesFilter(activityAt(tt.created)); got != tt.want {
				t.Errorf("matchesFilter(created=%s) = %v, want %v", tt.created, got, tt.want)
			}
		})
	}
}

func TestMatchesFilterMaxAge(t *testing.T) {
	now := time.Date(2026, 11, 2, 12, 0, 0, 0, time.UTC)
	w := &ActivityWatch{
		filter: WatchFilter{MaxAge: 10 * time.Minute},
		now:    func() time.Time { return now },
	}

	tests := []struct {
		name    string
		created time.Time
		want    bool
	}{
		{"fresh", now.Add(-time.Second), true},
		{"exactly max age", now.Add(-10 * time.Minute), true},
		{"one second too old", now.Add(-10*time.Minute - time.Second), false},
		{"replayed from yesterday", now.Add(-24 * time.Hour), false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := w.matchesFilter(activityAt(tt.created)); got != tt.want {
				t.Errorf("matchesFilter(created=%s) = %v, want %v", tt.created, got, tt.want)
			}
		})
	}

	// MaxAge slides: an activity that was fresh becomes stale as time passes.
	created := now.Add(-5 * time.Minute)
	if !w.matchesFilter(activityAt(created)) {
		t.Fatal("expected 5m-old activity to match")
	}
	now = now.Add(6 * time.Minute)
	if w.matchesFilter(activityAt(created)) {
		t.Error("expected activity to be dropped once it is older than maxAge")
	}
}

func TestMatchesFilterNoBounds(t *testing.T) {
	w := &ActivityWatch{}
	if !w.matchesFilter(activityAt(time.Unix(0, 0))) {
		t.Error("expected an unfiltered watch to match old activities")
	}
}
