package storage

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/ClickHouse/clickhouse-go/v2"
	authnv1 "k8s.io/api/authentication/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	auditv1 "k8s.io/apiserver/pkg/apis/audit/v1"

	"go.miloapis.com/activity/pkg/apis/activity/v1alpha1"
)

type fakeStaffScopes struct{ scope StaffScope }

func (f fakeStaffScopes) StaffScope(ScopeContext) StaffScope { return f.scope }

func newRedactingStorage(ss StaffScope) *ClickHouseStorage {
	s := &ClickHouseStorage{config: ClickHouseConfig{Database: "audit", MaxPageSize: 1000}}
	s.SetStaffScopes(fakeStaffScopes{scope: ss})
	return s
}

// syncedStaff: staff-1 is a staff-users member outside acme (by name and
// metadata.uid); jo (uid staff-2) is a member of acme with a datum.net email.
var syncedStaff = StaffScope{
	Redact:          true,
	Synced:          true,
	StaffOutsideOrg: []string{"staff-1", "staff-1-uid"},
	DomainMembers:   []string{"staff-2"},
	Domains:         []string{"datum.net"},
}

var orgScope = ScopeContext{Type: "Organization", Name: "acme"}

func placeholders(q string) int { return strings.Count(q, "?") }

func argString(a any) string {
	if gs, ok := a.(clickhouse.GroupSet); ok {
		parts := make([]string, len(gs.Value))
		for i, v := range gs.Value {
			parts[i], _ = v.(string)
		}
		return "(" + strings.Join(parts, ",") + ")"
	}
	if t, ok := a.(time.Time); ok {
		return t.UTC().Format(time.RFC3339)
	}
	s, _ := a.(string)
	return s
}

func TestBuildQuery_TenantScopeRedactsStaff(t *testing.T) {
	s := newRedactingStorage(syncedStaff)
	spec := v1alpha1.AuditLogQuerySpec{StartTime: "2026-10-01T00:00:00Z", EndTime: "2026-10-02T00:00:00Z", Filter: "user.username == 'sam@datum.net'"}

	query, args, redaction, err := s.buildQuery(context.Background(), spec, orgScope)
	if err != nil {
		t.Fatalf("buildQuery: %v", err)
	}
	if redaction == nil {
		t.Fatal("no redaction for an organization scope")
	}
	for _, want := range []string{
		"WITH lower(arrayElement(splitByChar('@', user), -1)) AS staff_domain, ",
		"((NOT startsWith(user, 'system:') AND NOT endsWith(lower(user), ?) AND (user_uid IN ? OR (position(user, '@') > 0 AND (staff_domain = ? OR endsWith(staff_domain, ?)) AND NOT user_uid IN ?)))",
		"OR audit_id IN (SELECT toUUIDOrZero(origin_id) FROM audit.activities WHERE actor_type = ? AND origin_type = 'audit' AND tenant_type = ? AND tenant_name = ? AND timestamp >= ? AND timestamp < ?))",
		"AS is_staff_actor",
		"SELECT event_json, is_staff_actor FROM audit.audit_logs",
		// The user filter runs against the redacted value, so a staff email
		// matches nothing in tenant scope.
		"redacted_user = ?",
	} {
		if !strings.Contains(query, want) {
			t.Errorf("query missing %q:\n%s", want, query)
		}
	}
	if strings.Contains(query, " user = ?") {
		t.Errorf("user filter applied to the raw column: %s", query)
	}
	if placeholders(query) != len(args) {
		t.Fatalf("%d placeholders but %d args: %s %v", placeholders(query), len(args), query, args)
	}
	want := []string{
		serviceAccountSuffix, "(staff-1,staff-1-uid)", "datum.net", ".datum.net", "(staff-2)",
		"staff", "Organization", "acme", "2026-09-30T23:00:00Z", "2026-10-02T01:00:00Z",
	}
	for i, w := range want {
		if got := argString(args[i]); got != w {
			t.Errorf("args[%d] = %q, want %q", i, got, w)
		}
	}
	if !containsArg(args, "sam@datum.net") {
		t.Errorf("filter value not bound: %v", args)
	}
}

func TestBuildQuery_FilterOnDatumSupportMatchesRedactedRows(t *testing.T) {
	s := newRedactingStorage(syncedStaff)
	query, args, _, err := s.buildQuery(context.Background(),
		v1alpha1.AuditLogQuerySpec{Filter: "user.username == 'Datum Support' || user.uid == 'staff-1'"},
		ScopeContext{Type: "Project", Name: "p"})
	if err != nil {
		t.Fatalf("buildQuery: %v", err)
	}
	if !strings.Contains(query, "redacted_user = ?") || !strings.Contains(query, "redacted_user_uid = ?") {
		t.Errorf("filters not on redacted values: %s", query)
	}
	if !strings.Contains(query, "if(is_staff_actor, 'Datum Support', user) AS redacted_user") ||
		!strings.Contains(query, "if(is_staff_actor, '', user_uid) AS redacted_user_uid") {
		t.Errorf("redacted aliases missing: %s", query)
	}
	// No start time: the sticky lookup is bounded to the activities
	// retention window.
	if !strings.Contains(query, "AND timestamp >= ?)") {
		t.Errorf("sticky lookup not bounded: %s", query)
	}
	var lower time.Time
	for _, a := range args {
		if ts, ok := a.(time.Time); ok {
			lower = ts
		}
	}
	if d := time.Since(lower); d < stickyRetention-time.Minute || d > stickyRetention+time.Minute {
		t.Errorf("sticky lower bound %s ago, want %s", d, stickyRetention)
	}
	if placeholders(query) != len(args) {
		t.Fatalf("%d placeholders but %d args", placeholders(query), len(args))
	}
}

// Until staff memberships are loaded, tenant-scoped queries are refused
// rather than falling back to a weaker rule.
func TestBuildQuery_UnsyncedTenantScopeIsRefused(t *testing.T) {
	s := newRedactingStorage(StaffScope{Redact: true, Domains: []string{"datum.net"}})
	_, _, _, err := s.buildQuery(context.Background(), v1alpha1.AuditLogQuerySpec{}, orgScope)
	if !errors.Is(err, ErrStaffRedactionNotReady) {
		t.Fatalf("err = %v, want ErrStaffRedactionNotReady", err)
	}
	_, _, _, err = s.buildAuditLogFacetQuery(context.Background(), FacetFieldSpec{Field: "verb"}, AuditLogFacetQuerySpec{}, orgScope)
	if !errors.Is(err, ErrStaffRedactionNotReady) {
		t.Fatalf("facet err = %v, want ErrStaffRedactionNotReady", err)
	}
	status := StaffRedactionNotReadyError().ErrStatus
	if status.Code != 503 || status.Details.RetryAfterSeconds == 0 {
		t.Errorf("status = %+v, want 503 with Retry-After", status)
	}
	// Platform scope is unaffected.
	if _, _, _, err := s.buildQuery(context.Background(), v1alpha1.AuditLogQuerySpec{}, ScopeContext{Type: "platform"}); err != nil {
		t.Errorf("platform scope refused: %v", err)
	}
}

func TestBuildQuery_EmptyListsAndNoDomains(t *testing.T) {
	s := newRedactingStorage(StaffScope{Redact: true, Synced: true})
	query, args, _, err := s.buildQuery(context.Background(), v1alpha1.AuditLogQuerySpec{}, orgScope)
	if err != nil {
		t.Fatalf("buildQuery: %v", err)
	}
	if !strings.Contains(query, "AND (0 OR 0)) OR audit_id IN (") {
		t.Errorf("empty staff, domain and member lists not rendered as constants: %s", query)
	}
	if placeholders(query) != len(args) {
		t.Fatalf("%d placeholders but %d args", placeholders(query), len(args))
	}
}

func TestBuildQuery_PlatformAndUserScopesAreNotRedacted(t *testing.T) {
	s := newRedactingStorage(syncedStaff)
	for _, scope := range []ScopeContext{{Type: "platform"}, {Type: "User", Name: "staff-1"}} {
		query, _, redaction, err := s.buildQuery(context.Background(), v1alpha1.AuditLogQuerySpec{Filter: "user.username == 'x'"}, scope)
		if err != nil {
			t.Fatalf("buildQuery: %v", err)
		}
		if redaction != nil || strings.Contains(query, "is_staff_actor") || strings.Contains(query, "redacted_user") {
			t.Errorf("%s scope redacted: %s", scope.Type, query)
		}
	}
	s = newRedactingStorage(StaffScope{})
	if _, _, redaction, _ := s.buildQuery(context.Background(), v1alpha1.AuditLogQuerySpec{}, orgScope); redaction != nil {
		t.Error("redacting with Redact=false")
	}
}

func TestBuildAuditLogFacetQuery_StaffMergeIntoOneBucket(t *testing.T) {
	s := newRedactingStorage(syncedStaff)
	spec := AuditLogFacetQuerySpec{StartTime: "now-1d", Filter: "user.uid == 'staff-1'"}

	query, args, column, err := s.buildAuditLogFacetQuery(context.Background(), FacetFieldSpec{Field: "user.username"}, spec, orgScope)
	if err != nil {
		t.Fatalf("buildAuditLogFacetQuery: %v", err)
	}
	if column != "redacted_user" {
		t.Errorf("column = %q, want redacted_user", column)
	}
	if !strings.HasPrefix(query, "WITH ") || !strings.Contains(query, "SELECT toString(redacted_user) as value") ||
		!strings.Contains(query, "GROUP BY redacted_user") || !strings.Contains(query, "redacted_user_uid = ?") {
		t.Errorf("facet query does not group or filter on redacted values: %s", query)
	}
	if placeholders(query) != len(args) {
		t.Fatalf("%d placeholders but %d args", placeholders(query), len(args))
	}

	query, _, column, _ = s.buildAuditLogFacetQuery(context.Background(), FacetFieldSpec{Field: "verb"}, AuditLogFacetQuerySpec{}, orgScope)
	if column != "verb" || !strings.Contains(query, "GROUP BY verb") {
		t.Errorf("verb facet = %s (%s)", query, column)
	}
	_, _, column, _ = s.buildAuditLogFacetQuery(context.Background(), FacetFieldSpec{Field: "user.username"}, AuditLogFacetQuerySpec{}, ScopeContext{Type: "platform"})
	if column != "user" {
		t.Errorf("platform facet column = %q, want user", column)
	}
}

// The reviewer's probe: a staff row must not leak the identity through any
// field of the raw event.
func TestMinimalStaffAuditEvent_AllowlistOnly(t *testing.T) {
	e := auditv1.Event{
		TypeMeta:         metav1.TypeMeta{APIVersion: "audit.k8s.io/v1", Kind: "Event"},
		Level:            auditv1.LevelRequestResponse,
		AuditID:          "audit-1",
		Stage:            auditv1.StageResponseComplete,
		RequestURI:       "/apis/x/v1/namespaces/default/things?fieldSelector=owner%3Dsam@datum.net",
		Verb:             "delete",
		User:             authnv1.UserInfo{Username: "sam@datum.net", UID: "staff-1", Groups: []string{"g"}, Extra: map[string]authnv1.ExtraValue{"k": {"v"}}},
		ImpersonatedUser: &authnv1.UserInfo{Username: "imp@datum.net"},
		SourceIPs:        []string{"203.0.113.7"},
		UserAgent:        "sams-tool/1.0",
		ObjectRef: &auditv1.ObjectReference{
			APIGroup: "g", APIVersion: "v1", Resource: "things", Subresource: "status",
			Namespace: "default", Name: "x", UID: "obj-uid", ResourceVersion: "42",
		},
		ResponseStatus: &metav1.Status{Code: 403, Message: `things "x" is forbidden: User "sam@datum.net" cannot delete resource "things"`,
			Reason: metav1.StatusReasonForbidden, Details: &metav1.StatusDetails{Name: "sam@datum.net"}},
		RequestObject:            &runtime.Unknown{Raw: []byte(`{"owner":"sam@datum.net"}`)},
		ResponseObject:           &runtime.Unknown{Raw: []byte(`{"owner":"sam@datum.net"}`)},
		RequestReceivedTimestamp: metav1.NewMicroTime(time.Date(2026, 10, 1, 12, 0, 0, 0, time.UTC)),
		StageTimestamp:           metav1.NewMicroTime(time.Date(2026, 10, 1, 12, 0, 1, 0, time.UTC)),
		Annotations: map[string]string{
			"authorization.k8s.io/reason": `RBAC: allowed by RoleBinding "b" to User "sam@datum.net"`,
		},
	}

	got := redactAuditEventForScope(e, true, syncedStaff)

	raw, _ := json.Marshal(got)
	for _, leak := range []string{"sam@datum.net", "staff-1", "imp@datum.net", "203.0.113.7", "sams-tool", "fieldSelector", "RoleBinding", "forbidden"} {
		if strings.Contains(string(raw), leak) {
			t.Errorf("redacted event contains %q: %s", leak, raw)
		}
	}
	want := auditv1.Event{
		TypeMeta: e.TypeMeta, Level: e.Level, AuditID: e.AuditID, Stage: e.Stage, Verb: e.Verb,
		User: authnv1.UserInfo{Username: SupportActorName},
		ObjectRef: &auditv1.ObjectReference{APIGroup: "g", APIVersion: "v1", Resource: "things", Subresource: "status",
			Namespace: "default", Name: "x", UID: "obj-uid"},
		ResponseStatus:           &metav1.Status{Code: 403},
		RequestReceivedTimestamp: e.RequestReceivedTimestamp,
		StageTimestamp:           e.StageTimestamp,
	}
	wantRaw, _ := json.Marshal(want)
	if string(raw) != string(wantRaw) {
		t.Errorf("redacted event =\n%s\nwant\n%s", raw, wantRaw)
	}
}

// A non-staff row loses impersonatedUser only when that is a staff identity.
func TestRedactAuditEventForScope_ImpersonatedStaff(t *testing.T) {
	base := auditv1.Event{User: authnv1.UserInfo{Username: "c@acme.com", UID: "cust-1"}, Verb: "get"}

	e := base
	e.ImpersonatedUser = &authnv1.UserInfo{Username: "sam@datum.net", UID: "staff-1"}
	if got := redactAuditEventForScope(e, false, syncedStaff); got.ImpersonatedUser != nil || got.User.Username != "c@acme.com" {
		t.Errorf("impersonated staff kept: %+v", got)
	}

	e = base
	e.ImpersonatedUser = &authnv1.UserInfo{Username: "bob@acme.com", UID: "cust-2"}
	if got := redactAuditEventForScope(e, false, syncedStaff); got.ImpersonatedUser == nil {
		t.Error("impersonated customer removed")
	}
}

func TestStaffScope_IsStaff(t *testing.T) {
	cases := []struct {
		uid, username string
		want          bool
	}{
		{"staff-1", "sam@datum.net", true},
		{"staff-1-uid", "sam@gmail.com", true},         // staff-users member by metadata.uid
		{"staff-2", "jo@datum.net", false},             // staff-domain email, but a member of this org
		{"x", "eve@datum.net", true},                   // staff domain
		{"x", "eve@eng.datum.net", true},               // staff subdomain
		{"x", "eve@notdatum.net", false},               // look-alike domain
		{"x", "datum.net", false},                      // no "@"
		{"x", "system:control@datum.net", false},       // system identity
		{"x", "ci@datum.identity.miloapis.com", false}, // Milo ServiceAccount
		{"cust-1", "c@acme.com", false},
	}
	for _, tc := range cases {
		if got := syncedStaff.IsStaff(tc.uid, tc.username); got != tc.want {
			t.Errorf("IsStaff(%q, %q) = %v, want %v", tc.uid, tc.username, got, tc.want)
		}
	}
	if (StaffScope{}).IsStaff("staff-1", "sam@datum.net") {
		t.Error("IsStaff true with Redact=false")
	}
}

func containsArg(args []any, v any) bool {
	for _, a := range args {
		if a == v {
			return true
		}
	}
	return false
}

// The domain-only mode (clusters without Milo) never refuses and uses only
// the email-domain rule.
func TestBuildQuery_DomainOnlyModeIsNotRefused(t *testing.T) {
	s := newRedactingStorage(StaffScope{Redact: true, DomainOnly: true, Domains: []string{"datum.net"}})
	query, args, redaction, err := s.buildQuery(context.Background(), v1alpha1.AuditLogQuerySpec{}, orgScope)
	if err != nil || redaction == nil {
		t.Fatalf("buildQuery = %v, redaction %v", err, redaction)
	}
	if !strings.Contains(query, "AND (0 OR (position(user, '@') > 0 AND (staff_domain = ? OR endsWith(staff_domain, ?)) AND NOT 0))") {
		t.Errorf("domain-only query = %s", query)
	}
	if placeholders(query) != len(args) {
		t.Fatalf("%d placeholders but %d args", placeholders(query), len(args))
	}
}
