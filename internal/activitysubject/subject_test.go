package activitysubject

import (
	"testing"

	"github.com/nats-io/nats-server/v2/server"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"go.miloapis.com/activity/internal/types"
	"go.miloapis.com/activity/pkg/apis/activity/v1alpha1"
)

func TestEncodeToken(t *testing.T) {
	tests := []struct {
		in, want string
	}{
		{"", "_"},
		{"HTTPProxy", "HTTPProxy"},
		{"networking.datumapis.com", "networking_datumapis_com"},
		{"a*b>c", "a_b_c"},
		{"has space\tand\ttab", "has_space_and_tab"},
		{"prod-1", "prod-1"},
	}
	for _, tt := range tests {
		if got := EncodeToken(tt.in); got != tt.want {
			t.Errorf("EncodeToken(%q) = %q, want %q", tt.in, got, tt.want)
		}
	}
}

func newActivity(tenantType, tenantName, apiGroup, kind, namespace string) *v1alpha1.Activity {
	return &v1alpha1.Activity{
		ObjectMeta: metav1.ObjectMeta{Name: "act-0123456789ab", Namespace: namespace},
		Spec: v1alpha1.ActivitySpec{
			Tenant:   v1alpha1.ActivityTenant{Type: tenantType, Name: tenantName},
			Origin:   v1alpha1.ActivityOrigin{Type: "audit"},
			Resource: v1alpha1.ActivityResource{APIGroup: apiGroup, Kind: kind, Namespace: namespace},
		},
	}
}

func TestForActivity(t *testing.T) {
	tests := []struct {
		name     string
		activity *v1alpha1.Activity
		want     string
	}{
		{
			name:     "dotted API group is one token",
			activity: newActivity(types.TenantTypeProject, "prod", "networking.datumapis.com", "HTTPProxy", "default"),
			want:     "activities.Project.prod.networking_datumapis_com.audit.HTTPProxy.default.act-0123456789ab",
		},
		{
			name:     "defaults for platform tenant, core group and cluster-scoped resource",
			activity: newActivity("", "", "", "Namespace", ""),
			want:     "activities.platform._.core.audit.Namespace._.act-0123456789ab",
		},
		{
			name:     "empty kind and origin never leave an empty token",
			activity: &v1alpha1.Activity{ObjectMeta: metav1.ObjectMeta{Name: "act-x"}},
			want:     "activities.platform._.core._._._.act-x",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ForActivity("activities", tt.activity)
			if got != tt.want {
				t.Errorf("ForActivity() = %q, want %q", got, tt.want)
			}
			if !server.IsValidPublishSubject(got) {
				t.Errorf("ForActivity() = %q is not a valid NATS publish subject", got)
			}
		})
	}
}

// TestPublishAndWatchSubjectsAgree checks the publisher and watcher sides
// against the NATS server's own subject matcher.
func TestPublishAndWatchSubjectsAgree(t *testing.T) {
	dotted := newActivity(types.TenantTypeProject, "prod", "networking.datumapis.com", "HTTPProxy", "default")
	core := newActivity(types.TenantTypeOrganization, "acme", "", "Secret", "kube-system")

	tests := []struct {
		name     string
		activity *v1alpha1.Activity
		scope    [2]string // tenant type, tenant name
		apiGroup string
		kind     string
		ns       string
		want     bool
	}{
		{"dotted group, all filters", dotted, [2]string{types.TenantTypeProject, "prod"}, "networking.datumapis.com", "HTTPProxy", "default", true},
		{"dotted group, group only", dotted, [2]string{types.TenantTypeProject, "prod"}, "networking.datumapis.com", "", "", true},
		{"dotted group, namespace only", dotted, [2]string{types.TenantTypeProject, "prod"}, "", "", "default", true},
		{"dotted group, no filters", dotted, [2]string{types.TenantTypeProject, "prod"}, "", "", "", true},
		{"platform scope", dotted, [2]string{types.TenantTypePlatform, ""}, "networking.datumapis.com", "HTTPProxy", "default", true},
		{"core group, kind and namespace", core, [2]string{types.TenantTypeOrganization, "acme"}, "", "Secret", "kube-system", true},
		{"core group, explicit core", core, [2]string{types.TenantTypeOrganization, "acme"}, "core", "", "", true},

		{"other group", dotted, [2]string{types.TenantTypeProject, "prod"}, "compute.datumapis.com", "", "", false},
		{"group prefix only", dotted, [2]string{types.TenantTypeProject, "prod"}, "networking", "", "", false},
		{"other kind", dotted, [2]string{types.TenantTypeProject, "prod"}, "networking.datumapis.com", "Gateway", "", false},
		{"other namespace", dotted, [2]string{types.TenantTypeProject, "prod"}, "", "", "other", false},
		{"other tenant", dotted, [2]string{types.TenantTypeProject, "staging"}, "", "", "", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			subject := ForActivity("activities", tt.activity)
			filter := ForWatch("activities", tt.scope[0], tt.scope[1], tt.apiGroup, tt.kind, tt.ns)
			if !server.IsValidSubject(filter) {
				t.Fatalf("ForWatch() = %q is not a valid NATS subject", filter)
			}
			if got := server.SubjectMatchesFilter(subject, filter); got != tt.want {
				t.Errorf("filter %q matching subject %q = %v, want %v", filter, subject, got, tt.want)
			}
		})
	}
}

// TestLegacyDottedSubjectsMissFilteredWatches documents the compatibility
// gap: messages published before the encoding fix keep the dotted group, so
// tenant-scoped watches filtered by group or namespace do not match them,
// while unfiltered and platform-scope watches still do.
func TestLegacyDottedSubjectsMissFilteredWatches(t *testing.T) {
	legacy := "activities.Project.prod.networking.datumapis.com.audit.HTTPProxy.default.act-0123456789ab"

	if server.SubjectMatchesFilter(legacy, ForWatch("activities", types.TenantTypeProject, "prod", "networking.datumapis.com", "", "")) {
		t.Error("legacy subject unexpectedly matched an apiGroup-filtered watch")
	}
	if server.SubjectMatchesFilter(legacy, ForWatch("activities", types.TenantTypeProject, "prod", "", "", "default")) {
		t.Error("legacy subject unexpectedly matched a namespace-filtered watch")
	}
	if server.SubjectMatchesFilter(legacy, ForWatch("activities", types.TenantTypeProject, "prod", "", "HTTPProxy", "")) {
		t.Error("legacy subject unexpectedly matched a kind-filtered watch")
	}
	if !server.SubjectMatchesFilter(legacy, ForWatch("activities", types.TenantTypeProject, "prod", "", "", "")) {
		t.Error("legacy subject should match an unfiltered tenant-scoped watch")
	}
	if !server.SubjectMatchesFilter(legacy, ForWatch("activities", types.TenantTypePlatform, "", "networking.datumapis.com", "", "default")) {
		t.Error("legacy subject should match a platform-scope watch")
	}
}
