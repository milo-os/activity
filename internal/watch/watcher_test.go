package watch

import (
	"testing"

	"github.com/nats-io/nats-server/v2/server"

	"go.miloapis.com/activity/internal/storage"
	"go.miloapis.com/activity/pkg/apis/activity/v1alpha1"
)

func TestActivityWatch_MatchesFilter_Scope(t *testing.T) {
	activity := func(tenantType, tenantName, actorUID string) *v1alpha1.Activity {
		return &v1alpha1.Activity{Spec: v1alpha1.ActivitySpec{
			Tenant: v1alpha1.ActivityTenant{Type: tenantType, Name: tenantName},
			Actor:  v1alpha1.ActivityActor{UID: actorUID},
		}}
	}

	tests := []struct {
		name     string
		scope    storage.ScopeContext
		activity *v1alpha1.Activity
		want     bool
	}{
		{name: "platform sees any tenant", scope: storage.ScopeContext{Type: "platform"}, activity: activity("Project", "p1", ""), want: true},
		{name: "project sees itself", scope: storage.ScopeContext{Type: "Project", Name: "p1"}, activity: activity("Project", "p1", ""), want: true},
		{name: "project excludes other project", scope: storage.ScopeContext{Type: "Project", Name: "p1"}, activity: activity("Project", "p2", ""), want: false},
		// activities.Project.p1.> also matches tenant p1.x's subjects; only this check stops it.
		{name: "project excludes dotted name with same prefix", scope: storage.ScopeContext{Type: "Project", Name: "p1"}, activity: activity("Project", "p1.x", ""), want: false},
		{name: "project excludes organization of same name", scope: storage.ScopeContext{Type: "Project", Name: "p1"}, activity: activity("Organization", "p1", ""), want: false},
		{name: "project excludes platform activity", scope: storage.ScopeContext{Type: "Project", Name: "p1"}, activity: activity("", "", ""), want: false},
		// User scope matches the actor, as ClickHouse queries do.
		{name: "user sees own actions in any tenant", scope: storage.ScopeContext{Type: "User", Name: "u1"}, activity: activity("Project", "p1", "u1"), want: true},
		{name: "user excludes others' actions", scope: storage.ScopeContext{Type: "User", Name: "u1"}, activity: activity("Project", "p1", "u2"), want: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			w := &ActivityWatch{scope: tt.scope}
			if got := w.matchesFilter(tt.activity); got != tt.want {
				t.Errorf("matchesFilter() = %v, want %v", got, tt.want)
			}
		})
	}
}

// TestBuildSubject checks watch subscriptions against subjects in the format
// the activity processor publishes: the raw API group adds a token per dot,
// and core resources use "core".
func TestBuildSubject(t *testing.T) {
	w := &NATSWatcher{subjectPrefix: "activities"}

	const (
		dottedInP1 = "activities.Project.p1.compute.datumapis.com.audit.Workload.default.act-1"
		coreInP1   = "activities.Project.p1.core.audit.ConfigMap.default.act-2"
		clusterP1  = "activities.Project.p1.resourcemanager.miloapis.com.audit.Project._.act-3"
		dottedInP2 = "activities.Project.p2.compute.datumapis.com.audit.Workload.default.act-4"
		inOrg      = "activities.Organization.o1.iam.miloapis.com.audit.User._.act-5"
	)

	tests := []struct {
		name      string
		scope     storage.ScopeContext
		matches   []string
		noMatches []string
	}{
		{
			name:      "project sees all its own activities",
			scope:     storage.ScopeContext{Type: "Project", Name: "p1"},
			matches:   []string{dottedInP1, coreInP1, clusterP1},
			noMatches: []string{dottedInP2, inOrg},
		},
		{
			name:      "organization sees only its own activities",
			scope:     storage.ScopeContext{Type: "Organization", Name: "o1"},
			matches:   []string{inOrg},
			noMatches: []string{dottedInP1, dottedInP2},
		},
		{
			name:    "platform sees everything",
			scope:   storage.ScopeContext{Type: "platform"},
			matches: []string{dottedInP1, coreInP1, clusterP1, dottedInP2, inOrg},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			pattern := w.buildSubject(tt.scope)
			for _, subject := range tt.matches {
				if !server.SubjectMatchesFilter(subject, pattern) {
					t.Errorf("buildSubject() = %q, want it to match %q", pattern, subject)
				}
			}
			for _, subject := range tt.noMatches {
				if server.SubjectMatchesFilter(subject, pattern) {
					t.Errorf("buildSubject() = %q, want it not to match %q", pattern, subject)
				}
			}
		})
	}
}
