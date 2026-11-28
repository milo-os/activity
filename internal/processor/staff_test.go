package processor

import (
	"encoding/json"
	"strings"
	"testing"

	authnv1 "k8s.io/api/authentication/v1"
	"k8s.io/apimachinery/pkg/types"
	auditv1 "k8s.io/apiserver/pkg/apis/audit/v1"

	"go.miloapis.com/activity/internal/cel"
	"go.miloapis.com/activity/pkg/apis/activity/v1alpha1"
)

// fakeStaff treats the listed User names as staff acting as support.
type fakeStaff struct {
	staff map[string]bool
	calls int
	first StaffCandidate // the actor check comes first
	last  StaffCandidate
}

func (f *fakeStaff) ActsAsSupport(c StaffCandidate, _ v1alpha1.ActivityTenant) bool {
	f.calls++
	if f.calls == 1 {
		f.first = c
	}
	f.last = c
	return f.staff[c.UserName]
}

var sam = UserInfo{Name: "sam-sub", UID: "sam-uid", GivenName: "Sam", FamilyName: "Staff", AvatarURL: "https://a.example/sam.png"}

func staffAudit(username, uid string) *auditv1.Event {
	return &auditv1.Event{
		AuditID:          types.UID("audit-1"),
		Verb:             "delete",
		UserAgent:        "sams-personal-tool/1.0",
		SourceIPs:        []string{"203.0.113.7"},
		ImpersonatedUser: &authnv1.UserInfo{Username: "imp@datum.net"},
		User: authnv1.UserInfo{Username: username, UID: uid, Extra: map[string]authnv1.ExtraValue{
			"iam.miloapis.com/parent-type": {"Organization"},
			"iam.miloapis.com/parent-name": {"acme"},
		}},
		ObjectRef: &auditv1.ObjectReference{APIGroup: "networking.datumapis.com", Resource: "httpproxies", Name: "api"},
	}
}

func evalStaff(t *testing.T, template, match string, audit *auditv1.Event, e Enrichment) *v1alpha1.Activity {
	t.Helper()
	spec := &v1alpha1.ActivityPolicySpec{
		Resource:   v1alpha1.ActivityPolicyResource{APIGroup: "networking.datumapis.com", Kind: "HTTPProxy"},
		AuditRules: []v1alpha1.ActivityPolicyRule{{Name: "r", Match: match, Summary: template}},
	}
	res, err := EvaluateAuditRulesWithEnrichment(spec, audit, nil, e)
	if err != nil {
		t.Fatalf("evaluate: %v", err)
	}
	if res.Activity == nil {
		t.Fatal("no activity")
	}
	return res.Activity
}

func TestStaffActor_ShownAsDatumSupport(t *testing.T) {
	users := &fakeResolver{names: map[string]UserInfo{sam.Name: sam}}
	e := Enrichment{Users: users, Staff: &fakeStaff{staff: map[string]bool{sam.Name: true}}}

	for _, template := range []string{
		"{{ actor }} deleted HTTPProxy {{ audit.objectRef.name }}",
		"{{ actor.startsWith('system:') ? 'System' : link(actor, actorRef) }} deleted HTTPProxy {{ audit.objectRef.name }}",
		"{{ audit.user.username }} deleted HTTPProxy {{ audit.objectRef.name }}",
		"{{ link(actor, {'apiGroup': 'iam.miloapis.com', 'kind': 'User', 'name': audit.user.uid}) }} deleted HTTPProxy {{ audit.objectRef.name }}",
		"{{ actor }}{{ audit.user.uid }}{{ interface.client }}{{ size(audit.user.groups) > 0 ? 'groups' : '' }}{{ has(audit.impersonatedUser) ? 'imp' : '' }}{{ has(audit.userAgent) ? 'ua' : '' }}{{ has(audit.sourceIPs) ? 'ip' : '' }} deleted HTTPProxy {{ audit.objectRef.name }}",
	} {
		t.Run(template, func(t *testing.T) {
			a := evalStaff(t, template, "true", staffAudit("sam@datum.net", sam.Name), e)

			if want := "Datum Support deleted HTTPProxy api"; a.Spec.Summary != want {
				t.Errorf("Summary = %q, want %q", a.Spec.Summary, want)
			}
			want := v1alpha1.ActivityActor{Type: ActorTypeStaff, Name: SupportActorName}
			if a.Spec.Actor != want {
				t.Errorf("actor = %+v, want %+v", a.Spec.Actor, want)
			}
			if a.Spec.ChangeSource != ChangeSourceHuman {
				t.Errorf("changeSource = %q, want human", a.Spec.ChangeSource)
			}
			if len(a.Spec.Links) != 0 {
				t.Errorf("staff actor has links: %+v", a.Spec.Links)
			}
			raw, _ := json.Marshal(a)
			for _, leak := range []string{"sam@datum.net", "Sam Staff", "sam.png", sam.Name, sam.UID, "sams-personal-tool", "203.0.113.7", "imp@datum.net"} {
				if strings.Contains(string(raw), leak) {
					t.Errorf("activity contains %q: %s", leak, raw)
				}
			}
			if a.Spec.Interface == nil || *a.Spec.Interface != (v1alpha1.ActivityInterface{Type: "api"}) {
				t.Errorf("Interface = %+v, want type api with no client", a.Spec.Interface)
			}
		})
	}
}

func TestStaffActor_MatchSeesRealIdentity(t *testing.T) {
	e := Enrichment{Staff: &fakeStaff{staff: map[string]bool{sam.Name: true}}}
	a := evalStaff(t, "{{ actor }} did it", "audit.user.username.endsWith('@datum.net')", staffAudit("sam@datum.net", sam.Name), e)
	if a.Spec.Summary != "Datum Support did it" {
		t.Errorf("Summary = %q", a.Spec.Summary)
	}
}

func TestStaffActor_OtherUserLinksStillEnriched(t *testing.T) {
	users := &fakeResolver{names: map[string]UserInfo{sam.Name: sam, alice.Name: alice}}
	e := Enrichment{Users: users, Staff: &fakeStaff{staff: map[string]bool{sam.Name: true}}}
	a := evalStaff(t,
		"{{ actor }} removed {{ link('alice', {'apiGroup': 'iam.miloapis.com', 'kind': 'User', 'name': 'alice-sub'}) }}",
		"true", staffAudit("sam@datum.net", sam.Name), e)
	if len(a.Spec.Links) != 1 || a.Spec.Links[0].DisplayName != "Alice Smith" {
		t.Errorf("links = %+v, want the customer's User link enriched", a.Spec.Links)
	}
}

func TestStaffActor_NonStaffAndStaffInOwnOrgAreNormal(t *testing.T) {
	users := &fakeResolver{names: map[string]UserInfo{sam.Name: sam}}
	staff := &fakeStaff{staff: map[string]bool{}} // the resolver says: not acting as support
	a := evalStaff(t, "{{ actor }} deleted it", "true", staffAudit("sam@datum.net", sam.Name), Enrichment{Users: users, Staff: staff})
	if a.Spec.Summary != "sam@datum.net deleted it" || a.Spec.Actor.DisplayName != "Sam Staff" || a.Spec.Actor.Type != ActorTypeUser {
		t.Errorf("activity = %+v", a.Spec)
	}
}

func TestStaffActor_SystemAndServiceAccountsAreNeverStaff(t *testing.T) {
	staff := &fakeStaff{staff: map[string]bool{"x": true}}
	for _, username := range []string{"system:control@compute.datumapis.com", "ci@proj.identity.miloapis.com"} {
		audit := staffAudit(username, "x")
		audit.ImpersonatedUser = nil
		a := evalStaff(t, "{{ actor }} did it", "true", audit, Enrichment{Staff: staff})
		if a.Spec.Actor.Type == ActorTypeStaff {
			t.Errorf("%s classified as staff", username)
		}
	}
	if staff.calls != 0 {
		t.Errorf("staff resolver consulted %d times for non-human actors", staff.calls)
	}
}

func TestRedactSupportActor_DoesNotMutateInput(t *testing.T) {
	in := map[string]any{
		"user":             map[string]any{"username": "sam@datum.net", "uid": "u", "groups": []any{"g"}, "extra": map[string]any{"k": []any{"v"}}},
		"impersonatedUser": map[string]any{"username": "imp"},
		"userAgent":        "tool/1",
		"sourceIPs":        []any{"203.0.113.7"},
		"verb":             "get",
		"requestObject":    map[string]any{"a": "b"},
	}
	out := redactSupportActor(in)
	if in["user"].(map[string]any)["username"] != "sam@datum.net" || in["userAgent"] != "tool/1" {
		t.Error("input map mutated")
	}
	user := out["user"].(map[string]any)
	if user["username"] != SupportActorName || user["uid"] != "" || len(user["groups"].([]any)) != 0 || len(user["extra"].(map[string]any)) != 0 {
		t.Errorf("redacted user = %+v", user)
	}
	for _, k := range []string{"impersonatedUser", "userAgent", "sourceIPs"} {
		if _, ok := out[k]; ok {
			t.Errorf("redacted map still has %s", k)
		}
	}
	if out["verb"] != "get" || out["requestObject"] == nil {
		t.Errorf("redacted map lost other fields: %+v", out)
	}
}

// The audit UID may be the User's metadata.uid rather than its name; staff
// detection, link dropping and enrichment exclusion all accept either.
func TestStaffActor_JoinsOnMetadataNameAndUID(t *testing.T) {
	users := &fakeResolver{names: map[string]UserInfo{sam.Name: sam, alice.Name: alice}, uids: map[string]UserInfo{sam.UID: sam}}
	staff := &fakeStaff{staff: map[string]bool{sam.Name: true}}
	a := evalStaff(t,
		"{{ actor }} touched {{ link('u1', {'apiGroup': 'iam.miloapis.com', 'kind': 'User', 'name': 'sam-sub'}) }} "+
			"{{ link('u2', {'apiGroup': 'iam.miloapis.com', 'kind': 'User', 'name': 'sam-uid'}) }} "+
			"{{ link('u3', {'kind': 'Thing', 'name': 'x', 'uid': 'sam-uid'}) }} "+
			"{{ link('u4', {'apiGroup': 'iam.miloapis.com', 'kind': 'User', 'name': 'alice-sub'}) }}",
		"true", staffAudit("sam@datum.net", sam.UID), Enrichment{Users: users, Staff: staff})

	if staff.first.UserName != sam.Name || staff.first.UserUID != sam.UID || staff.first.AuditUID != sam.UID {
		t.Errorf("staff candidate = %+v, want name %q and uid %q", staff.first, sam.Name, sam.UID)
	}
	if a.Spec.Actor.Type != ActorTypeStaff {
		t.Fatalf("actor = %+v, want staff", a.Spec.Actor)
	}
	if len(a.Spec.Links) != 1 || a.Spec.Links[0].Resource.Name != "alice-sub" {
		t.Errorf("links = %+v, want only the customer's link", a.Spec.Links)
	}

	ex := excludingUser{UserResolver: users, candidate: StaffCandidate{UserName: sam.Name, UserUID: sam.UID}}
	if _, ok := ex.LookupByName(sam.Name); ok {
		t.Error("excludingUser returned the staff User by name")
	}
	if _, ok := ex.LookupByUID(sam.UID); ok {
		t.Error("excludingUser returned the staff User by uid")
	}
	if _, ok := ex.LookupByName(alice.Name); !ok {
		t.Error("excludingUser hid another User")
	}
}

// Staff detection uses the audit UID directly, so it does not depend on the
// User index (which --allow-unenriched may leave unsynced).
func TestStaffActor_DetectedWithoutUserIndex(t *testing.T) {
	staff := &fakeStaff{staff: map[string]bool{sam.Name: true}}
	for name, users := range map[string]UserResolver{"no index": nil, "unsynced index": NoopUserResolver{}} {
		t.Run(name, func(t *testing.T) {
			a := evalStaff(t, "{{ actor }} did it", "true", staffAudit("sam@datum.net", sam.Name), Enrichment{Users: users, Staff: staff})
			if a.Spec.Actor.Type != ActorTypeStaff || staff.first.AuditUID != sam.Name {
				t.Errorf("actor = %+v (candidate %+v), want staff detected from the audit UID", a.Spec.Actor, staff.first)
			}
		})
	}
}

func TestStaffCandidate_IDs(t *testing.T) {
	c := StaffCandidate{AuditUID: "u", UserName: "n", UserUID: "u"}
	if got := strings.Join(c.IDs(), ","); got != "u,n" {
		t.Errorf("IDs() = %q", got)
	}
	if !c.Matches("n") || !c.Matches("u") || c.Matches("") || c.Matches("x") {
		t.Error("Matches wrong")
	}
}

// A non-staff caller impersonating a staff member: the staff identity is
// removed from what the template can render.
func TestImpersonatedStaff_NotRendered(t *testing.T) {
	staff := &fakeStaff{staff: map[string]bool{sam.Name: true}}
	audit := staffAudit("alice@x.com", "alice-sub")
	audit.ImpersonatedUser = &authnv1.UserInfo{Username: "sam@datum.net", UID: sam.Name}

	a := evalStaff(t, "{{ actor }} as {{ has(audit.impersonatedUser) ? audit.impersonatedUser.username : 'nobody' }}",
		"true", audit, Enrichment{Staff: staff})
	if a.Spec.Summary != "alice@x.com as nobody" {
		t.Errorf("Summary = %q, want the impersonated staff identity hidden", a.Spec.Summary)
	}

	// A non-staff impersonated user is rendered as before.
	audit.ImpersonatedUser = &authnv1.UserInfo{Username: "bob@x.com", UID: "bob-sub"}
	a = evalStaff(t, "{{ actor }} as {{ audit.impersonatedUser.username }}", "true", audit, Enrichment{Staff: staff})
	if a.Spec.Summary != "alice@x.com as bob@x.com" {
		t.Errorf("Summary = %q", a.Spec.Summary)
	}
}

// iam User links never show a staff member's display name in a tenant the
// staff member does not belong to, in audit- and event-derived activities.
func TestUserLinks_StaffDisplayNameHidden(t *testing.T) {
	users := &fakeResolver{names: map[string]UserInfo{sam.Name: sam, alice.Name: alice}}
	staff := &fakeStaff{staff: map[string]bool{sam.Name: true}}

	a := evalStaff(t,
		"{{ actor }} touched {{ link('s', {'apiGroup': 'iam.miloapis.com', 'kind': 'User', 'name': 'sam-sub'}) }} and "+
			"{{ link('a', {'apiGroup': 'iam.miloapis.com', 'kind': 'User', 'name': 'alice-sub'}) }}",
		"true", staffAudit("c@acme.com", "cust-1"), Enrichment{Users: users, Staff: staff})
	byName := map[string]string{}
	for _, l := range a.Spec.Links {
		byName[l.Resource.Name] = l.DisplayName
	}
	if byName["sam-sub"] != "" || byName["alice-sub"] != "Alice Smith" {
		t.Errorf("link display names = %v", byName)
	}

	event := map[string]interface{}{
		"metadata": map[string]interface{}{"uid": "e1", "annotations": map[string]interface{}{
			"platform.miloapis.com/scope.type": "Organization", "platform.miloapis.com/scope.name": "acme",
		}},
		"reportingController": "iam-controller",
		"regarding":           map[string]interface{}{"apiVersion": "iam.miloapis.com/v1alpha1", "kind": "User", "name": "sam-sub"},
	}
	links := []cel.Link{{Marker: "sam-sub", Resource: map[string]interface{}{"apiGroup": "iam.miloapis.com", "kind": "User", "name": "sam-sub"}}}
	b := &ActivityBuilder{APIGroup: "iam.miloapis.com", Kind: "User", UserResolver: users, StaffResolver: staff}
	fromReindex, err := b.BuildFromEvent(event, "updated sam-sub", links, nil)
	if err != nil {
		t.Fatalf("BuildFromEvent: %v", err)
	}
	ep := &EventProcessor{}
	ep.SetUserResolver(users)
	ep.SetStaffResolver(staff)
	fromLive := ep.buildActivity(event, &MatchedPolicy{APIGroup: "iam.miloapis.com", Kind: "User"}, "updated sam-sub", links)
	for name, act := range map[string]*v1alpha1.Activity{"reindex": fromReindex, "live": fromLive} {
		if len(act.Spec.Links) != 1 || act.Spec.Links[0].DisplayName != "" {
			t.Errorf("%s: links = %+v, want no staff display name", name, act.Spec.Links)
		}
	}
}
