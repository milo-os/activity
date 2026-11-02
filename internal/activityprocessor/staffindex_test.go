package activityprocessor

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"testing"
	"time"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	dynamicfake "k8s.io/client-go/dynamic/fake"
	k8stesting "k8s.io/client-go/testing"

	"go.miloapis.com/activity/internal/processor"
	"go.miloapis.com/activity/internal/storage"
	activitytypes "go.miloapis.com/activity/internal/types"
	"go.miloapis.com/activity/pkg/apis/activity/v1alpha1"
)

func obj(apiVersion, kind, namespace, name string, spec map[string]any) *unstructured.Unstructured {
	meta := map[string]any{"name": name, "resourceVersion": "1"}
	if namespace != "" {
		meta["namespace"] = namespace
	}
	return &unstructured.Unstructured{Object: map[string]any{
		"apiVersion": apiVersion, "kind": kind, "metadata": meta, "spec": spec,
	}}
}

func groupMembership(name, user, group, groupNS string) *unstructured.Unstructured {
	return obj("iam.miloapis.com/v1alpha1", "GroupMembership", StaffGroupNamespace, name, map[string]any{
		"userRef":  map[string]any{"name": user},
		"groupRef": map[string]any{"name": group, "namespace": groupNS},
	})
}

func orgMembership(name, user, org string) *unstructured.Unstructured {
	return obj("resourcemanager.miloapis.com/v1alpha1", "OrganizationMembership", "organization-"+org, name, map[string]any{
		"userRef":         map[string]any{"name": user},
		"organizationRef": map[string]any{"name": org},
	})
}

func project(name, org string) *unstructured.Unstructured {
	return obj("resourcemanager.miloapis.com/v1alpha1", "Project", "", name, map[string]any{
		"ownerRef": map[string]any{"kind": "Organization", "name": org},
	})
}

func newFakeStaffClient(objs ...runtime.Object) *dynamicfake.FakeDynamicClient {
	return dynamicfake.NewSimpleDynamicClientWithCustomListKinds(runtime.NewScheme(), map[schema.GroupVersionResource]string{
		groupMembershipGVR: "GroupMembershipList",
		orgMembershipGVR:   "OrganizationMembershipList",
		projectGVR:         "ProjectList",
	}, objs...)
}

func startStaffIndex(t *testing.T, client *dynamicfake.FakeDynamicClient, grace time.Duration) *StaffIndex {
	t.Helper()
	x, err := NewStaffIndexForClient(client, []string{"datum.net"})
	if err != nil {
		t.Fatalf("NewStaffIndexForClient: %v", err)
	}
	for _, idx := range x.indexes() {
		idx.fatalGrace = grace
	}
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	x.Start(ctx)
	return x
}

func tenant(typ, name string) v1alpha1.ActivityTenant {
	return v1alpha1.ActivityTenant{Type: typ, Name: name}
}

func cand(name, username string) processor.StaffCandidate {
	return processor.StaffCandidate{UserName: name, Username: username}
}

func TestStaffIndex_MembershipRule(t *testing.T) {
	client := newFakeStaffClient(
		groupMembership("gm-staff", "staff-1", StaffGroupName, StaffGroupNamespace),
		groupMembership("gm-other", "eng-1", "engineering", StaffGroupNamespace),
		orgMembership("om-1", "staff-1", "datum"),
		orgMembership("om-2", "cust-1", "acme"),
		orgMembership("om-3", "eng-1", "datum"),
		project("acme-web", "acme"),
		project("datum-internal", "datum"),
	)
	x := startStaffIndex(t, client, defaultFatalGrace)
	if err := x.WaitForSync(context.Background(), 5*time.Second); err != nil {
		t.Fatalf("WaitForSync: %v", err)
	}

	cases := []struct {
		name   string
		c      processor.StaffCandidate
		tenant v1alpha1.ActivityTenant
		want   bool
	}{
		{"staff in a customer org", cand("staff-1", "sam@datum.net"), tenant(activitytypes.TenantTypeOrganization, "acme"), true},
		{"staff in their own org", cand("staff-1", "sam@datum.net"), tenant(activitytypes.TenantTypeOrganization, "datum"), false},
		{"staff in a customer project", cand("staff-1", "sam@datum.net"), tenant(activitytypes.TenantTypeProject, "acme-web"), true},
		{"staff in their own org's project", cand("staff-1", "sam@datum.net"), tenant(activitytypes.TenantTypeProject, "datum-internal"), false},
		{"staff in an unknown project fails closed", cand("staff-1", "sam@datum.net"), tenant(activitytypes.TenantTypeProject, "missing"), true},
		{"staff in someone else's user scope", cand("staff-1", "sam@datum.net"), tenant(activitytypes.TenantTypeUser, "cust-1"), true},
		{"staff in their own user scope", cand("staff-1", "sam@datum.net"), tenant(activitytypes.TenantTypeUser, "staff-1"), false},
		{"staff at platform scope", cand("staff-1", "sam@datum.net"), tenant(activitytypes.TenantTypePlatform, ""), false},
		{"staff-users member without a staff email", cand("staff-1", "sam@gmail.com"), tenant(activitytypes.TenantTypeOrganization, "acme"), true},
		{"customer in their org", cand("cust-1", "c@acme.com"), tenant(activitytypes.TenantTypeOrganization, "acme"), false},
		{"staff-domain email, not in staff-users, in a customer org", cand("eng-1", "eve@datum.net"), tenant(activitytypes.TenantTypeOrganization, "acme"), true},
		{"staff-domain email, not in staff-users, in their own org", cand("eng-1", "eve@datum.net"), tenant(activitytypes.TenantTypeOrganization, "datum"), false},
		{"synced but no User identifiers fails closed by domain", cand("", "sam@datum.net"), tenant(activitytypes.TenantTypeOrganization, "acme"), true},
		{"membership found by metadata.uid", processor.StaffCandidate{UserName: "other-name", UserUID: "staff-1", Username: "x@gmail.com"}, tenant(activitytypes.TenantTypeOrganization, "datum"), false},
		{"staff found by metadata.uid", processor.StaffCandidate{UserName: "other-name", UserUID: "staff-1", Username: "x@gmail.com"}, tenant(activitytypes.TenantTypeOrganization, "acme"), true},
		{"own user scope by metadata.uid", processor.StaffCandidate{UserName: "n", UserUID: "u", Username: "sam@datum.net"}, tenant(activitytypes.TenantTypeUser, "u"), false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := x.ActsAsSupport(tc.c, tc.tenant); got != tc.want {
				t.Errorf("ActsAsSupport(%+v, %+v) = %v, want %v", tc.c, tc.tenant, got, tc.want)
			}
		})
	}
}

func TestStaffIndex_UnsyncedFailsClosedByEmailDomain(t *testing.T) {
	client := newFakeStaffClient(groupMembership("gm-staff", "staff-1", StaffGroupName, StaffGroupNamespace))
	client.PrependReactor("list", "organizationmemberships", func(k8stesting.Action) (bool, runtime.Object, error) {
		return true, nil, apierrors.NewForbidden(orgMembershipGVR.GroupResource(), "", nil)
	})
	x := startStaffIndex(t, client, 200*time.Millisecond)

	if err := x.WaitForSync(context.Background(), 10*time.Second); err == nil {
		t.Fatal("WaitForSync succeeded although organization memberships are forbidden")
	}
	if x.Synced() {
		t.Fatal("Synced() = true")
	}
	acme := tenant(activitytypes.TenantTypeOrganization, "acme")
	for username, want := range map[string]bool{
		"sam@datum.net":     true,
		"sam@eng.datum.net": true,
		"SAM@DATUM.NET":     true,
		"c@acme.com":        false,
		"c@notdatum.net":    false,
		"datum.net":         false,
	} {
		if got := x.ActsAsSupport(cand("staff-1", username), acme); got != want {
			t.Errorf("unsynced ActsAsSupport(%q) = %v, want %v", username, got, want)
		}
	}
	if x.ActsAsSupport(cand("staff-1", "sam@datum.net"), tenant(activitytypes.TenantTypePlatform, "")) {
		t.Error("platform-scoped activity redacted")
	}
}

func TestStaffIndex_NoClientIsDomainOnly(t *testing.T) {
	x := &StaffIndex{domains: ResolveStaffEmailDomains([]string{" @Datum.net "})}
	if x.Synced() {
		t.Fatal("Synced() = true without a client")
	}
	if !x.ActsAsSupport(cand("u", "a@datum.net"), tenant(activitytypes.TenantTypeOrganization, "acme")) {
		t.Error("domain fallback did not apply")
	}
	if err := x.WaitForSync(context.Background(), time.Millisecond); err == nil {
		t.Error("WaitForSync succeeded without a client")
	}
}

func TestResolveStaffEmailDomains(t *testing.T) {
	cases := []struct {
		in   []string
		want string
	}{
		{nil, "datum.net"},
		{[]string{}, "datum.net"},
		{[]string{"", " "}, "datum.net"},
		{[]string{"@Example.COM", "datum.net"}, "example.com,datum.net"},
		{[]string{"none"}, ""},
		{[]string{"NONE"}, ""},
	}
	for _, tc := range cases {
		if got := strings.Join(ResolveStaffEmailDomains(tc.in), ","); got != tc.want {
			t.Errorf("ResolveStaffEmailDomains(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
	x := &StaffIndex{domains: ResolveStaffEmailDomains([]string{"none"})}
	if x.ActsAsSupport(cand("u", "a@datum.net"), tenant(activitytypes.TenantTypeOrganization, "acme")) {
		t.Error(`"none" still applied a domain rule`)
	}
}

func TestStaffIndex_TransformsTrim(t *testing.T) {
	gm := groupMembership("gm", "u", StaffGroupName, StaffGroupNamespace)
	gm.SetLabels(map[string]string{"x": "y"})
	out, _ := trimGroupMembership(gm)
	m := out.(*cachedGroupMembership)
	if m.UserName != "u" || m.GroupName != StaffGroupName || m.Labels != nil {
		t.Errorf("trimmed group membership = %+v", m)
	}
	out, _ = trimProject(project("p", "acme"))
	if p := out.(*cachedProject); p.OwnerName != "acme" || p.OwnerKind != "Organization" {
		t.Errorf("trimmed project = %+v", p)
	}
	if keys, _ := indexStaffUser(&cachedGroupMembership{UserName: "u", GroupName: "staff-users", GroupNamespace: "other-ns"}); len(keys) != 0 {
		t.Errorf("staff-users in another namespace indexed as staff: %v", keys)
	}
}

func TestStaffIndex_StaffScopeForAuditLogs(t *testing.T) {
	client := newFakeStaffClient(
		groupMembership("gm-1", "staff-1", StaffGroupName, StaffGroupNamespace),
		groupMembership("gm-2", "staff-2", StaffGroupName, StaffGroupNamespace),
		orgMembership("om-1", "staff-2", "acme"),
		orgMembership("om-2", "cust-1", "acme"),
		orgMembership("om-3", "staff-1", "datum"),
		project("acme-web", "acme"),
	)
	x := startStaffIndex(t, client, defaultFatalGrace)
	if err := x.WaitForSync(context.Background(), 5*time.Second); err != nil {
		t.Fatalf("WaitForSync: %v", err)
	}
	sorted := func(s []string) string {
		c := append([]string(nil), s...)
		sort.Strings(c)
		return strings.Join(c, ",")
	}

	// staff-2 is a member of acme, so only staff-1 is staff there.
	org := x.StaffScope(storage.ScopeContext{Type: activitytypes.TenantTypeOrganization, Name: "acme"})
	if !org.Redact || !org.Synced || org.DomainOnly || sorted(org.StaffOutsideOrg) != "staff-1" ||
		strings.Join(org.Domains, ",") != "datum.net" {
		t.Errorf("org scope = %+v", org)
	}
	proj := x.StaffScope(storage.ScopeContext{Type: activitytypes.TenantTypeProject, Name: "acme-web"})
	if sorted(proj.StaffOutsideOrg) != "staff-1" {
		t.Errorf("project scope staff = %v, want the owning org's outsiders", proj.StaffOutsideOrg)
	}
	unknown := x.StaffScope(storage.ScopeContext{Type: activitytypes.TenantTypeProject, Name: "missing"})
	if !unknown.Redact || sorted(unknown.StaffOutsideOrg) != "staff-1,staff-2" {
		t.Errorf("unknown project scope = %+v, want every staff member redacted", unknown)
	}
	for _, scope := range []storage.ScopeContext{{Type: activitytypes.TenantTypePlatform}, {Type: activitytypes.TenantTypeUser, Name: "staff-1"}} {
		if x.StaffScope(scope).Redact {
			t.Errorf("%s scope redacted", scope.Type)
		}
	}

	unsynced := NewUnavailableStaffIndex(nil).StaffScope(storage.ScopeContext{Type: activitytypes.TenantTypeOrganization, Name: "acme"})
	if !unsynced.Redact || unsynced.Synced || unsynced.DomainOnly {
		t.Errorf("unsynced scope = %+v, want one the query refuses", unsynced)
	}
	domainOnly := NewDomainOnlyStaffIndex(nil).StaffScope(storage.ScopeContext{Type: activitytypes.TenantTypeOrganization, Name: "acme"})
	if !domainOnly.Redact || !domainOnly.DomainOnly || strings.Join(domainOnly.Domains, ",") != "datum.net" {
		t.Errorf("domain-only scope = %+v", domainOnly)
	}
}

// Membership checks work from the audit UID alone, without the User index.
func TestStaffIndex_AuditUIDAloneSuffices(t *testing.T) {
	client := newFakeStaffClient(
		groupMembership("gm-1", "staff-1", StaffGroupName, StaffGroupNamespace),
		orgMembership("om-1", "staff-1", "datum"),
	)
	x := startStaffIndex(t, client, defaultFatalGrace)
	if err := x.WaitForSync(context.Background(), 5*time.Second); err != nil {
		t.Fatalf("WaitForSync: %v", err)
	}
	c := processor.StaffCandidate{AuditUID: "staff-1", Username: "sam@gmail.com"}
	if !x.ActsAsSupport(c, tenant(activitytypes.TenantTypeOrganization, "acme")) {
		t.Error("staff not detected from the audit UID")
	}
	if x.ActsAsSupport(c, tenant(activitytypes.TenantTypeOrganization, "datum")) {
		t.Error("own-org membership not detected from the audit UID")
	}
}

type uidResolver map[string]processor.UserInfo

func (u uidResolver) LookupByName(n string) (processor.UserInfo, bool) { i, ok := u[n]; return i, ok }
func (u uidResolver) LookupByUID(string) (processor.UserInfo, bool) {
	return processor.UserInfo{}, false
}

// StaffScope lists both forms of each identity, so audit rows recorded with
// a metadata.uid also match, and spares members with staff-domain emails.
func TestStaffIndex_StaffScopeIncludesUIDs(t *testing.T) {
	client := newFakeStaffClient(
		groupMembership("gm-1", "staff-1", StaffGroupName, StaffGroupNamespace),
		orgMembership("om-1", "cust-1", "acme"),
		orgMembership("om-2", "jo", "acme"),
	)
	x := startStaffIndex(t, client, defaultFatalGrace)
	x.SetUserResolver(uidResolver{
		"staff-1": {Name: "staff-1", UID: "staff-1-uid", Email: "sam@datum.net"},
		"cust-1":  {Name: "cust-1", UID: "cust-1-uid", Email: "c@acme.com"},
		"jo":      {Name: "jo", UID: "jo-uid", Email: "jo@datum.net"},
	})
	if err := x.WaitForSync(context.Background(), 5*time.Second); err != nil {
		t.Fatalf("WaitForSync: %v", err)
	}
	ss := x.StaffScope(storage.ScopeContext{Type: activitytypes.TenantTypeOrganization, Name: "acme"})
	if strings.Join(ss.StaffOutsideOrg, ",") != "staff-1,staff-1-uid" || strings.Join(ss.DomainMembers, ",") != "jo,jo-uid" {
		t.Errorf("scope = %+v", ss)
	}
	if !ss.IsStaff("staff-1-uid", "sam@datum.net") || ss.IsStaff("jo-uid", "jo@datum.net") || ss.IsStaff("cust-1", "c@acme.com") {
		t.Error("IsStaff disagrees with the scope lists")
	}
}

// A large organization does not grow the query: only staff outside the
// organization and members with staff-domain emails are listed.
func TestStaffIndex_LargeOrgKeepsQuerySmall(t *testing.T) {
	const members = 10000
	objs := []runtime.Object{
		groupMembership("gm-1", "staff-1", StaffGroupName, StaffGroupNamespace),
		groupMembership("gm-2", "staff-2", StaffGroupName, StaffGroupNamespace),
	}
	users := uidResolver{"staff-1": {Name: "staff-1", UID: "s1-uid", Email: "a@datum.net"}}
	for i := 0; i < members; i++ {
		name := fmt.Sprintf("member-%d", i)
		objs = append(objs, orgMembership("om-"+name, name, "big"))
		email := name + "@big.example"
		if i < 3 {
			email = name + "@datum.net" // a few Datum employees are members
		}
		users[name] = processor.UserInfo{Name: name, UID: name + "-uid", Email: email}
	}
	objs = append(objs, orgMembership("om-staff-2", "staff-2", "big"))
	x := startStaffIndex(t, newFakeStaffClient(objs...), defaultFatalGrace)
	x.SetUserResolver(users)
	if err := x.WaitForSync(context.Background(), 30*time.Second); err != nil {
		t.Fatalf("WaitForSync: %v", err)
	}

	ss := x.StaffScope(storage.ScopeContext{Type: activitytypes.TenantTypeOrganization, Name: "big"})
	if len(ss.StaffOutsideOrg) != 2 || len(ss.DomainMembers) != 6 {
		t.Fatalf("scope lists: %d staff outside, %d domain members; want 2 and 6", len(ss.StaffOutsideOrg), len(ss.DomainMembers))
	}

	s := &storage.ClickHouseStorage{}
	query, args := storage.AuditRedactionSQLForTest(s, fakeScopes{ss}, storage.ScopeContext{Type: activitytypes.TenantTypeOrganization, Name: "big"})
	if len(query) > 2000 || len(args) > 20 {
		t.Errorf("redaction SQL grew with the organization: %d bytes, %d args", len(query), len(args))
	}
}

type fakeScopes struct{ ss storage.StaffScope }

func (f fakeScopes) StaffScope(storage.ScopeContext) storage.StaffScope { return f.ss }
