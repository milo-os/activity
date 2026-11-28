package processor

import (
	"encoding/json"
	"strings"
	"sync/atomic"
	"testing"

	authnv1 "k8s.io/api/authentication/v1"
	"k8s.io/apimachinery/pkg/types"
	auditv1 "k8s.io/apiserver/pkg/apis/audit/v1"

	"go.miloapis.com/activity/internal/cel"
	"go.miloapis.com/activity/pkg/apis/activity/v1alpha1"
)

// fakeResolver is a map-backed UserResolver that counts calls.
type fakeResolver struct {
	nameCalls atomic.Int64
	uidCalls  atomic.Int64
	names     map[string]UserInfo
	uids      map[string]UserInfo
}

func (f *fakeResolver) LookupByName(name string) (UserInfo, bool) {
	f.nameCalls.Add(1)
	info, ok := f.names[name]
	return info, ok
}

func (f *fakeResolver) LookupByUID(uid string) (UserInfo, bool) {
	f.uidCalls.Add(1)
	info, ok := f.uids[uid]
	return info, ok
}

func (f *fakeResolver) totalCalls() int64 {
	return f.nameCalls.Load() + f.uidCalls.Load()
}

func userInfoFixture(username, uid string) authnv1.UserInfo {
	return authnv1.UserInfo{Username: username, UID: uid}
}

var alice = UserInfo{
	Name:       "alice-sub",
	UID:        "8a6c1c43-5b2f-4d43-9a1e-1d7d1c1f0a01",
	GivenName:  "Alice",
	FamilyName: "Smith",
	AvatarURL:  "https://avatars.example.com/alice.png",
}

// actorLink is the link ConvertLinks produces for link(actor, actorRef) or a
// bare {{ actor }} with a human actor.
func actorLink(marker, username string) v1alpha1.ActivityLink {
	return v1alpha1.ActivityLink{Marker: marker, Resource: v1alpha1.ActivityResource{Kind: ActorTypeUser, Name: username}}
}

func iamUserLink(marker, name string) v1alpha1.ActivityLink {
	return v1alpha1.ActivityLink{Marker: marker, Resource: v1alpha1.ActivityResource{APIGroup: iamGroup, Kind: "User", Name: name}}
}

// buildAudit runs the full audit path (CEL template rendering, link
// conversion, enrichment) for one rule.
func buildAudit(t *testing.T, summaryTemplate string, user authnv1.UserInfo, resolver UserResolver) *v1alpha1.Activity {
	t.Helper()
	spec := &v1alpha1.ActivityPolicySpec{
		Resource:   v1alpha1.ActivityPolicyResource{APIGroup: "networking.datumapis.com", Kind: "HTTPProxy"},
		AuditRules: []v1alpha1.ActivityPolicyRule{{Name: "r", Match: "true", Summary: summaryTemplate}},
	}
	audit := &auditv1.Event{
		AuditID: types.UID("audit-1"),
		Verb:    "delete",
		User:    user,
		ObjectRef: &auditv1.ObjectReference{
			APIGroup: "networking.datumapis.com", Resource: "httpproxies", Name: "banner-api",
		},
	}
	res, err := EvaluateAuditRulesWithResolver(spec, audit, nil, resolver)
	if err != nil {
		t.Fatalf("evaluate: %v", err)
	}
	if res.Activity == nil {
		t.Fatal("no activity")
	}
	return res.Activity
}

func TestUserInfo_DisplayName(t *testing.T) {
	cases := []struct {
		name string
		in   UserInfo
		want string
	}{
		{"both", UserInfo{GivenName: "Smith", FamilyName: "Nelson"}, "Smith Nelson"},
		{"given only", UserInfo{GivenName: "Smith"}, "Smith"},
		{"family only", UserInfo{FamilyName: "Nelson"}, "Nelson"},
		{"whitespace-only names are empty", UserInfo{GivenName: "  ", FamilyName: "\t"}, ""},
		{"empty", UserInfo{}, ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := tc.in.DisplayName(); got != tc.want {
				t.Fatalf("DisplayName() = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestSanitizeDisplayName(t *testing.T) {
	cases := []struct {
		name, in, want string
	}{
		{"trims and collapses whitespace", "  Alice \t\n  Smith  ", "Alice Smith"},
		{"strips control characters", "Ali\x00ce\x1b[31m Smith\x7f", "Alice[31m Smith"},
		{"strips bidi overrides and isolates", "Alice\u202eevil\u202c \u2066Smith\u2069\u200f", "Aliceevil Smith"},
		{"keeps non-Latin scripts", "Zoë 山田", "Zoë 山田"},
		{"caps at 64 runes", strings.Repeat("é", 70), strings.Repeat("é", 64)},
		{"trims after capping", strings.Repeat("a", 63) + " b", strings.Repeat("a", 63)},
		{"drops invalid UTF-8", "Al\xffice", "Alice"},
		{"strips zero-width and format characters", "A\u200bl\u200ci\u200dc\u2060e\ufeff Sm\u00adith", "Alice Smith"},
		{"strips tag characters", "Alice\U000E0041\U000E007F", "Alice"},
		{"strips variation selectors", "Alice\ufe0f\U000E0100", "Alice"},
		{"strips blank fillers", "\u3164Alice\u115f\u1160\uffa0 \u2800Smith", "Alice Smith"},
		{"only a zero-width space is empty", "\u200b", ""},
		{"only fillers and spaces is empty", "\u3164 \u2800\u3000\u115f", ""},
		{"only combining marks is empty", "\u0301\u0302 \u20dd", ""},
		{"combining marks on letters are kept", "Jose\u0301", "Jose\u0301"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := SanitizeDisplayName(tc.in); got != tc.want {
				t.Fatalf("SanitizeDisplayName(%q) = %q, want %q", tc.in, got, tc.want)
			}
		})
	}
}

func TestSanitizeAvatarURL(t *testing.T) {
	cases := []struct {
		name, in, want string
	}{
		{"https kept", "https://avatars.example.com/a.png?s=64", "https://avatars.example.com/a.png?s=64"},
		{"http rejected", "http://avatars.example.com/a.png", ""},
		{"javascript rejected", "javascript:alert(1)", ""},
		{"data rejected", "data:image/png;base64,AAAA", ""},
		{"relative rejected", "/a.png", ""},
		{"no host rejected", "https:///a.png", ""},
		{"userinfo rejected", "https://user:pass@example.com/a.png", ""},
		{"whitespace rejected", "https://example.com/a b.png", ""},
		{"control character rejected", "https://example.com/a\n.png", ""},
		{"too long rejected", "https://example.com/" + strings.Repeat("a", 2048), ""},
		{"empty", "", ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := SanitizeAvatarURL(tc.in); got != tc.want {
				t.Fatalf("SanitizeAvatarURL(%q) = %q, want %q", tc.in, got, tc.want)
			}
		})
	}
}

func TestNoopUserResolver_AlwaysMisses(t *testing.T) {
	var r UserResolver = NoopUserResolver{}
	if _, ok := r.LookupByName("x"); ok {
		t.Error("LookupByName hit")
	}
	if _, ok := r.LookupByUID("x"); ok {
		t.Error("LookupByUID hit")
	}
}

// The summary is never rewritten, whatever the actor's name collides with.
func TestBuildFromAudit_SummaryIsNeverRewritten(t *testing.T) {
	ann := UserInfo{Name: "ann", GivenName: "Ann", FamilyName: "Lee"}
	cases := []struct {
		name     string
		user     authnv1.UserInfo
		resolver *fakeResolver
		template string
		want     string
	}{
		{
			name:     "username is a substring of other text",
			user:     userInfoFixture("ann", "ann"),
			resolver: &fakeResolver{names: map[string]UserInfo{"ann": ann}},
			template: "HTTPProxy banner-api was deleted by {{ actor }}",
			want:     "HTTPProxy banner-api was deleted by ann",
		},
		{
			name:     "actor email inside another link's marker",
			user:     userInfoFixture("alice@x.com", alice.Name),
			resolver: &fakeResolver{names: map[string]UserInfo{alice.Name: alice}},
			template: "{{ link('API key for ' + actor, {'kind': 'APIKey', 'name': 'k1'}) }} was created by {{ actor }}",
			want:     "API key for alice@x.com was created by alice@x.com",
		},
		{
			name:     "repeated occurrences",
			user:     userInfoFixture("alice@x.com", alice.Name),
			resolver: &fakeResolver{names: map[string]UserInfo{alice.Name: alice}},
			template: "{{ actor }} added {{ actor }} to {{ link(actor, actorRef) }}",
			want:     "alice@x.com added alice@x.com to alice@x.com",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			a := buildAudit(t, tc.template, tc.user, tc.resolver)
			if a.Spec.Summary != tc.want {
				t.Fatalf("Summary = %q, want %q", a.Spec.Summary, tc.want)
			}
			for _, l := range a.Spec.Links {
				if !strings.Contains(a.Spec.Summary, l.Marker) {
					t.Errorf("marker %q does not appear in summary %q", l.Marker, a.Spec.Summary)
				}
			}
		})
	}
}

func TestBuildFromAudit_ActorLinkPointsAtMatchedUser(t *testing.T) {
	for _, template := range []string{
		"{{ actor }} deleted HTTPProxy {{ audit.objectRef.name }}",
		"{{ actor.startsWith('system:') ? 'System' : link(actor, actorRef) }} deleted HTTPProxy {{ audit.objectRef.name }}",
	} {
		t.Run(template, func(t *testing.T) {
			resolver := &fakeResolver{names: map[string]UserInfo{alice.Name: alice}}
			a := buildAudit(t, template, userInfoFixture("alice@x.com", alice.Name), resolver)

			if want := "alice@x.com deleted HTTPProxy banner-api"; a.Spec.Summary != want {
				t.Errorf("Summary = %q, want %q", a.Spec.Summary, want)
			}
			if len(a.Spec.Links) != 1 {
				t.Fatalf("links = %+v, want exactly one actor link", a.Spec.Links)
			}
			got := a.Spec.Links[0]
			want := v1alpha1.ActivityLink{
				Marker: "alice@x.com",
				Resource: v1alpha1.ActivityResource{
					APIGroup: iamGroup, Kind: "User", Name: alice.Name, UID: alice.UID,
				},
				DisplayName: "Alice Smith",
			}
			if got != want {
				t.Errorf("actor link = %+v, want %+v", got, want)
			}
			if a.Spec.Actor.DisplayName != "Alice Smith" || a.Spec.Actor.AvatarURL != alice.AvatarURL {
				t.Errorf("actor = %+v", a.Spec.Actor)
			}
		})
	}
}

// The actor link names the User that actually matched, whichever UID lookup
// found it, and is never invented when nothing matched.
func TestBuildFromAudit_ActorLinkNameComesFromMatch(t *testing.T) {
	bob := UserInfo{Name: "bob-sub", UID: "bob-uid", GivenName: "Bob"}
	cases := []struct {
		name     string
		user     authnv1.UserInfo
		resolver *fakeResolver
		wantLink v1alpha1.ActivityResource
		wantName string
	}{
		{
			name:     "audit uid is the User name",
			user:     userInfoFixture("bob@x.com", "bob-sub"),
			resolver: &fakeResolver{names: map[string]UserInfo{"bob-sub": bob}},
			wantLink: v1alpha1.ActivityResource{APIGroup: iamGroup, Kind: "User", Name: "bob-sub", UID: "bob-uid"},
			wantName: "Bob",
		},
		{
			name:     "metadata.uid fallback",
			user:     userInfoFixture("bob@x.com", "bob-uid"),
			resolver: &fakeResolver{uids: map[string]UserInfo{"bob-uid": bob}},
			wantLink: v1alpha1.ActivityResource{APIGroup: iamGroup, Kind: "User", Name: "bob-sub", UID: "bob-uid"},
			wantName: "Bob",
		},
		{
			name:     "no match leaves the actorRef link alone",
			user:     userInfoFixture("bob@x.com", "unknown-id"),
			resolver: &fakeResolver{names: map[string]UserInfo{"bob-sub": bob}},
			wantLink: v1alpha1.ActivityResource{Kind: ActorTypeUser, Name: "bob@x.com"},
		},
		{
			name:     "no audit uid is never looked up",
			user:     userInfoFixture("bob@x.com", ""),
			resolver: &fakeResolver{names: map[string]UserInfo{"": bob}, uids: map[string]UserInfo{"": bob}},
			wantLink: v1alpha1.ActivityResource{Kind: ActorTypeUser, Name: "bob@x.com"},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			a := buildAudit(t, "{{ actor }} deleted zone z", tc.user, tc.resolver)
			if len(a.Spec.Links) != 1 {
				t.Fatalf("links = %+v", a.Spec.Links)
			}
			if got := a.Spec.Links[0]; got.Resource != tc.wantLink || got.DisplayName != tc.wantName {
				t.Errorf("link = %+v, want resource %+v and displayName %q", got, tc.wantLink, tc.wantName)
			}
		})
	}
}

// An actor from another authenticator whose username happens to equal a Milo
// User's email must not be enriched with that User's identity: lookups use
// the audit UID only.
func TestResolveActorWithResolver_UsernameMatchingAnEmailIsNotEnough(t *testing.T) {
	resolver := &fakeResolver{names: map[string]UserInfo{alice.Name: alice}, uids: map[string]UserInfo{alice.UID: alice}}
	actor := ResolveActorWithResolver(userInfoFixture("alice@x.com", "oidc|other-idp-123"), resolver)
	if actor.DisplayName != "" || actor.AvatarURL != "" {
		t.Fatalf("foreign actor enriched with another user's identity: %+v", actor)
	}
}

func TestEnrichLinks_NonUserLinksAreNeverTouched(t *testing.T) {
	resolver := &fakeResolver{names: map[string]UserInfo{alice.Name: alice, "inv-1": alice, "k1": alice}}
	links := []v1alpha1.ActivityLink{
		// Marker equals the actor's email but the resource is an invitation.
		{Marker: "alice@x.com", Resource: v1alpha1.ActivityResource{APIGroup: iamGroup, Kind: "UserInvitation", Name: "inv-1"}},
		// Marker contains the actor's email.
		{Marker: "API key for alice@x.com", Resource: v1alpha1.ActivityResource{Kind: "APIKey", Name: "k1"}},
		// Same kind name but a different group.
		{Marker: "alice@x.com", Resource: v1alpha1.ActivityResource{APIGroup: "example.com", Kind: "User", Name: alice.Name}},
		// actorRef shape, but for someone else.
		actorLink("carol@x.com", "carol@x.com"),
	}
	before := append([]v1alpha1.ActivityLink(nil), links...)

	got := enrichLinks("alice@x.com", &alice, links, resolver, nil)

	for i := range got {
		if got[i] != before[i] {
			t.Errorf("link %d changed: %+v -> %+v", i, before[i], got[i])
		}
	}
	if resolver.totalCalls() != 0 {
		t.Errorf("resolver called %d times for non-User links", resolver.totalCalls())
	}
}

func TestEnrichLinks_IAMUserLinkHydration(t *testing.T) {
	dean := UserInfo{Name: "340583683847098198", GivenName: "Dean", FamilyName: "Gaghan"}
	resolver := &fakeResolver{names: map[string]UserInfo{dean.Name: dean}}

	got := enrichLinks("alice@x.com", nil, []v1alpha1.ActivityLink{iamUserLink(dean.Name, dean.Name)}, resolver, nil)

	want := iamUserLink(dean.Name, dean.Name)
	want.DisplayName = "Dean Gaghan"
	if got[0] != want {
		t.Fatalf("link = %+v, want %+v", got[0], want)
	}
}

func TestEnrichLinks_UserWithoutNamesGetsNoDisplayName(t *testing.T) {
	noName := UserInfo{Name: "u-1"}
	resolver := &fakeResolver{names: map[string]UserInfo{"u-1": noName}}

	links := enrichLinks("secret@x.com", &noName,
		[]v1alpha1.ActivityLink{actorLink("secret@x.com", "secret@x.com"), iamUserLink("u-1", "u-1")}, resolver, nil)

	for _, l := range links {
		if l.DisplayName != "" {
			t.Errorf("link %+v has a displayName although the User has no names", l)
		}
	}
	raw, _ := json.Marshal(links)
	if strings.Count(string(raw), "secret@x.com") != 1 { // only the actor link's own marker
		t.Errorf("email copied into links: %s", raw)
	}
}

// Two different users with the same display name stay distinguishable: the
// summary keeps each one's verified identity and each link names its own User.
func TestBuildFromAudit_DuplicateDisplayNamesStayDistinct(t *testing.T) {
	other := UserInfo{Name: "other-sub", GivenName: "Alice", FamilyName: "Smith"}
	resolver := &fakeResolver{names: map[string]UserInfo{alice.Name: alice, other.Name: other}}

	a := buildAudit(t,
		"{{ actor }} removed {{ link('other-sub', {'apiGroup': 'iam.miloapis.com', 'kind': 'User', 'name': 'other-sub'}) }} from the org",
		userInfoFixture("alice@x.com", alice.Name), resolver)

	if want := "alice@x.com removed other-sub from the org"; a.Spec.Summary != want {
		t.Fatalf("Summary = %q, want %q", a.Spec.Summary, want)
	}
	if len(a.Spec.Links) != 2 {
		t.Fatalf("links = %+v", a.Spec.Links)
	}
	actorL, otherL := a.Spec.Links[0], a.Spec.Links[1]
	if actorL.Marker == otherL.Marker || actorL.Resource.Name == otherL.Resource.Name {
		t.Errorf("links are indistinguishable: %+v / %+v", actorL, otherL)
	}
	if actorL.DisplayName != "Alice Smith" || otherL.DisplayName != "Alice Smith" {
		t.Errorf("display names = %q, %q", actorL.DisplayName, otherL.DisplayName)
	}
}

func TestResolveActorWithResolver_LooksUpByAuditUIDFirst(t *testing.T) {
	resolver := &fakeResolver{
		names: map[string]UserInfo{alice.Name: alice},
		// A different user under the same value as metadata.uid proves the
		// metadata.name lookup wins.
		uids: map[string]UserInfo{alice.Name: {GivenName: "Wrong", FamilyName: "User"}},
	}

	actor := ResolveActorWithResolver(userInfoFixture("alice@x.com", alice.Name), resolver)

	if actor.DisplayName != "Alice Smith" {
		t.Fatalf("DisplayName = %q, want %q", actor.DisplayName, "Alice Smith")
	}
	if actor.AvatarURL != alice.AvatarURL {
		t.Errorf("AvatarURL = %q, want %q", actor.AvatarURL, alice.AvatarURL)
	}
	if resolver.uidCalls.Load() != 0 {
		t.Errorf("metadata.uid lookup should be skipped on a metadata.name hit, got %d calls", resolver.uidCalls.Load())
	}
}

func TestResolveActorWithResolver_SanitizesUserFields(t *testing.T) {
	evil := UserInfo{Name: "e", GivenName: "\u202eAlice\x00", FamilyName: strings.Repeat("x", 100), AvatarURL: "javascript:alert(1)"}
	actor := ResolveActorWithResolver(userInfoFixture("e@x.com", "e"), &fakeResolver{names: map[string]UserInfo{"e": evil}})
	if want := "Alice " + strings.Repeat("x", 58); actor.DisplayName != want {
		t.Errorf("DisplayName = %q, want %q", actor.DisplayName, want)
	}
	if actor.AvatarURL != "" {
		t.Errorf("AvatarURL = %q, want empty", actor.AvatarURL)
	}
}

func TestResolveActorWithResolver_DoesNotBackfillEmail(t *testing.T) {
	resolver := &fakeResolver{names: map[string]UserInfo{alice.Name: alice}}
	actor := ResolveActorWithResolver(userInfoFixture("alice", alice.Name), resolver)
	if actor.Email != "" {
		t.Fatalf("Email = %q, want empty for a non-email username", actor.Email)
	}
	if actor.Name != "alice" || actor.DisplayName != "Alice Smith" {
		t.Errorf("actor = %+v", actor)
	}
}

func TestResolveActorWithResolver_MissLeavesActorUnenriched(t *testing.T) {
	actor := ResolveActorWithResolver(userInfoFixture("alice@x.com", "uid-1"), &fakeResolver{})
	if actor.DisplayName != "" || actor.AvatarURL != "" {
		t.Fatalf("expected no enrichment, got %+v", actor)
	}
}

func TestResolveActorWithResolver_NilResolverNoDisplayName(t *testing.T) {
	actor := ResolveActorWithResolver(userInfoFixture("alice@x.com", "uid-1"), nil)
	if actor.DisplayName != "" {
		t.Fatalf("DisplayName should be empty without resolver, got %q", actor.DisplayName)
	}
}

func TestResolveActorWithResolver_SystemActorSkipsLookup(t *testing.T) {
	resolver := &fakeResolver{}
	actor := ResolveActorWithResolver(userInfoFixture("system:admin", "uid-system"), resolver)
	if actor.Type != ActorTypeSystem {
		t.Fatalf("Type = %q, want %q", actor.Type, ActorTypeSystem)
	}
	if resolver.totalCalls() != 0 {
		t.Fatalf("resolver should not be called for system actors, got %d calls", resolver.totalCalls())
	}
}

func TestBuildFromAudit_EnrichedFieldsSerialize(t *testing.T) {
	resolver := &fakeResolver{names: map[string]UserInfo{alice.Name: alice}}
	a := buildAudit(t, "{{ actor }} deleted HTTPProxy banner-api", userInfoFixture("alice@x.com", alice.Name), resolver)

	raw, err := json.Marshal(a)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	for _, want := range []string{`"displayName":"Alice Smith"`, `"avatarUrl":"https://avatars.example.com/alice.png"`} {
		if !strings.Contains(string(raw), want) {
			t.Errorf("serialized activity missing %s", want)
		}
	}
}

// PolicyPreview evaluates without a resolver; its output must equal the
// production output minus the display fields.
func TestBuildFromAudit_NoResolverMatchesEnrichedExceptDisplayFields(t *testing.T) {
	user := userInfoFixture("alice@x.com", alice.Name)
	plain := buildAudit(t, "{{ actor }} deleted HTTPProxy banner-api", user, nil)
	enriched := buildAudit(t, "{{ actor }} deleted HTTPProxy banner-api", user, &fakeResolver{names: map[string]UserInfo{alice.Name: alice}})

	if plain.Spec.Summary != enriched.Spec.Summary {
		t.Errorf("summary differs: %q vs %q", plain.Spec.Summary, enriched.Spec.Summary)
	}
	if plain.Spec.Actor.DisplayName != "" || plain.Spec.Actor.AvatarURL != "" {
		t.Errorf("unenriched actor = %+v", plain.Spec.Actor)
	}
	if len(plain.Spec.Links) != 1 || plain.Spec.Links[0].Marker != enriched.Spec.Links[0].Marker {
		t.Errorf("link markers differ: %+v vs %+v", plain.Spec.Links, enriched.Spec.Links)
	}
	raw, _ := json.Marshal(plain)
	if strings.Contains(string(raw), "displayName") || strings.Contains(string(raw), "avatarUrl") {
		t.Errorf("empty enrichment fields should be omitted: %s", raw)
	}
}

// Live event processing and re-indexing (BuildFromEvent) must produce
// identical links, including apiGroup derived from apiVersion and display
// names on iam User links.
func TestEventPaths_LinkParity(t *testing.T) {
	dean := UserInfo{Name: "dean-sub", GivenName: "Dean", FamilyName: "Gaghan"}
	resolver := &fakeResolver{names: map[string]UserInfo{dean.Name: dean}}
	event := map[string]interface{}{
		"metadata": map[string]interface{}{"uid": "event-1"},
		"reason":   "Invited",
		"regarding": map[string]interface{}{
			"apiVersion": "iam.miloapis.com/v1alpha1",
			"kind":       "UserInvitation",
			"name":       "inv-1",
		},
	}
	links := []cel.Link{
		{Marker: "dean-sub", Resource: map[string]interface{}{"apiVersion": "iam.miloapis.com/v1alpha1", "kind": "User", "name": "dean-sub"}},
		{Marker: "inv-1", Resource: map[string]interface{}{"apiVersion": "iam.miloapis.com/v1alpha1", "kind": "UserInvitation", "metadata": map[string]interface{}{"name": "inv-1", "uid": "u1"}}},
	}
	summary := "Invitation inv-1 accepted by dean-sub"

	builder := &ActivityBuilder{APIGroup: iamGroup, Kind: "UserInvitation", UserResolver: resolver}
	fromReindex, err := builder.BuildFromEvent(event, summary, links, nil)
	if err != nil {
		t.Fatalf("BuildFromEvent: %v", err)
	}

	ep := &EventProcessor{}
	ep.SetUserResolver(resolver)
	fromLive := ep.buildActivity(event, &MatchedPolicy{APIGroup: iamGroup, Kind: "UserInvitation"}, summary, links)

	if fromLive.Spec.Summary != summary || fromReindex.Spec.Summary != summary {
		t.Errorf("summaries rewritten: live=%q reindex=%q", fromLive.Spec.Summary, fromReindex.Spec.Summary)
	}
	if len(fromLive.Spec.Links) != len(fromReindex.Spec.Links) {
		t.Fatalf("live links %+v != reindex links %+v", fromLive.Spec.Links, fromReindex.Spec.Links)
	}
	for i := range fromLive.Spec.Links {
		if fromLive.Spec.Links[i] != fromReindex.Spec.Links[i] {
			t.Errorf("link %d: live %+v != reindex %+v", i, fromLive.Spec.Links[i], fromReindex.Spec.Links[i])
		}
	}
	userLink := fromLive.Spec.Links[0]
	if userLink.Resource.APIGroup != iamGroup || userLink.DisplayName != "Dean Gaghan" {
		t.Errorf("user link = %+v, want apiGroup %q and display name", userLink, iamGroup)
	}
}
