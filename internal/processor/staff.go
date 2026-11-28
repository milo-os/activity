package processor

import (
	"slices"
	"strings"

	authnv1 "k8s.io/api/authentication/v1"
	auditv1 "k8s.io/apiserver/pkg/apis/audit/v1"

	"go.miloapis.com/activity/internal/cel"
	activitytypes "go.miloapis.com/activity/internal/types"
	"go.miloapis.com/activity/pkg/apis/activity/v1alpha1"
)

const (
	// ActorTypeStaff is the actor type of a Datum staff member acting
	// outside their own organization. Customers see such an actor only as
	// SupportActorName.
	ActorTypeStaff = "staff"

	// SupportActorName is how a staff actor is shown to customers, in
	// spec.actor.name and wherever a policy template renders the actor.
	SupportActorName = "Datum Support"
)

// StaffCandidate identifies a human actor to a StaffResolver.
type StaffCandidate struct {
	// AuditUID is the audit user.UID as recorded. In Milo it equals the iam
	// User's metadata.name and membership userRef.name, so staff detection
	// works from it alone, without the User index.
	AuditUID string
	// UserName is the actor's iam User metadata.name, when known: the audit
	// UID, or the name of the User whose metadata.uid it is.
	UserName string
	// UserUID is the actor's iam User metadata.uid, when known.
	UserUID string
	// Username is the audit username, typically an email address.
	Username string
}

// IDs returns the candidate's distinct, non-empty User identifiers.
func (c StaffCandidate) IDs() []string {
	var ids []string
	for _, id := range []string{c.AuditUID, c.UserName, c.UserUID} {
		if id != "" && !slices.Contains(ids, id) {
			ids = append(ids, id)
		}
	}
	return ids
}

// Matches reports whether id is one of the candidate's User identifiers.
func (c StaffCandidate) Matches(id string) bool {
	return id != "" && slices.Contains(c.IDs(), id)
}

// StaffResolver decides whether an actor is Datum staff acting outside their
// own organization, in which case the activity discloses the actor only as
// "Datum Support". Implementations MUST be safe for concurrent use and SHOULD
// fail closed: when membership cannot be checked, they should treat a likely
// staff actor (for example one with a Datum email domain) as staff.
type StaffResolver interface {
	// ActsAsSupport reports whether the actor is staff acting in a tenant
	// they do not belong to. Membership is evaluated now, at processing or
	// re-index time, not at event time.
	ActsAsSupport(actor StaffCandidate, tenant v1alpha1.ActivityTenant) bool
}

// Enrichment holds the indexes used to enrich and classify actors. The zero
// value disables both: actors are neither enriched nor disclosed as staff.
type Enrichment struct {
	// Users enriches human actors and iam User links with display names. It
	// also maps an audit UID to the User's metadata.name and metadata.uid.
	Users UserResolver
	// Staff decides which actors are shown as "Datum Support".
	Staff StaffResolver
	// KeepSupport names activities that must stay "Datum Support" whatever
	// Staff decides now: a re-index sets it to the activities already stored
	// with a staff actor, so a staff member who has since joined the
	// organization is not disclosed retroactively. Keys are activity names
	// (see AuditActivityName).
	KeepSupport map[string]bool
}

// AuditActivityName is the deterministic name of the activity a policy
// targeting apiGroup/kind produces from the audit event auditID.
func AuditActivityName(auditID, apiGroup, kind string) string {
	return activityName("audit", auditID, apiGroup, kind)
}

// candidate identifies the audit actor by every User identifier known for
// it. The audit user.UID is normally the User's metadata.name; if it is
// instead the metadata.uid, the User index maps it back.
func (e Enrichment) candidate(user authnv1.UserInfo) StaffCandidate {
	return candidateFor(user.UID, user.Username, e.Users)
}

// candidateFor identifies a user by every identifier known for it. The audit
// UID is always included; the User index, when available and synced, adds
// the metadata.name/metadata.uid counterpart.
func candidateFor(uid, username string, users UserResolver) StaffCandidate {
	c := StaffCandidate{AuditUID: uid, UserName: uid, Username: username}
	if users == nil || uid == "" {
		return c
	}
	if info, ok := users.LookupByName(uid); ok {
		c.UserUID = info.UID
	} else if info, ok := users.LookupByUID(uid); ok {
		c.UserName, c.UserUID = info.Name, info.UID
	}
	return c
}

// actsAsSupport reports whether the audit actor must be disclosed as Datum
// Support. Only human users can be staff: system identities and Milo
// ServiceAccounts never are.
func (e Enrichment) actsAsSupport(user authnv1.UserInfo, c StaffCandidate) bool {
	if e.Staff == nil || strings.HasPrefix(user.Username, "system:") || activitytypes.IsMiloServiceAccount(user.Username) {
		return false
	}
	return e.Staff.ActsAsSupport(c, ExtractTenant(user))
}

// impersonatesStaff reports whether the audit event's impersonatedUser is
// staff relative to the event's tenant. (Milo does not currently record
// impersonation in audit events, but it is handled anyway.)
func (e Enrichment) impersonatesStaff(audit *auditv1.Event) bool {
	imp := audit.ImpersonatedUser
	if imp == nil || e.Staff == nil || strings.HasPrefix(imp.Username, "system:") || activitytypes.IsMiloServiceAccount(imp.Username) {
		return false
	}
	return e.Staff.ActsAsSupport(candidateFor(imp.UID, imp.Username, e.Users), ExtractTenant(audit.User))
}

// withoutKey returns a shallow copy of m without key.
func withoutKey(m map[string]any, key string) map[string]any {
	out := make(map[string]any, len(m))
	for k, v := range m {
		if k != key {
			out[k] = v
		}
	}
	return out
}

// staffLinkFilter returns the check enrichLinks uses to avoid hydrating an
// iam User link with a staff member's display name in a tenant they do not
// belong to. nil when there is no staff resolver.
func staffLinkFilter(staff StaffResolver, tenant v1alpha1.ActivityTenant) func(name string, info UserInfo) bool {
	if staff == nil {
		return nil
	}
	return func(name string, info UserInfo) bool {
		return staff.ActsAsSupport(StaffCandidate{AuditUID: name, UserName: info.Name, UserUID: info.UID, Username: info.Email}, tenant)
	}
}

// RenderFunc renders a policy summary template for one audit event.
type RenderFunc func(template string) (string, []cel.Link, error)

// AuditBuilder prepares to build an Activity from a matched audit rule. It
// classifies the actor BEFORE the summary is rendered, and returns the
// builder together with the function to render the rule's summary.
//
// For a staff actor acting as Datum Support, the renderer sees a redacted
// copy of the audit event: the whole user object is replaced by
// {username: "Datum Support"} with no uid, groups or extra; impersonatedUser,
// userAgent and sourceIPs are removed; and the interface variable keeps only
// its type. So actor, actorRef, audit.user.* and interface.client cannot
// reveal who it was. requestObject and responseObject are left as they are,
// so a template that renders object content (for example a created-by
// annotation) can still include an identity. Otherwise the event renders
// unchanged and the builder enriches the actor through e.Users.
func (e Enrichment) AuditBuilder(apiGroup, kind string, audit *auditv1.Event, auditMap map[string]any) (*ActivityBuilder, RenderFunc) {
	b := &ActivityBuilder{APIGroup: apiGroup, Kind: kind, UserResolver: e.Users, StaffResolver: e.Staff}
	c := e.candidate(audit.User)
	keep := e.KeepSupport[AuditActivityName(string(audit.AuditID), apiGroup, kind)]
	if !keep && !e.actsAsSupport(audit.User, c) {
		renderMap := auditMap
		if e.impersonatesStaff(audit) {
			// A non-staff caller impersonating a staff member: the staff
			// identity must not reach the summary either.
			renderMap = withoutKey(auditMap, "impersonatedUser")
		}
		return b, func(template string) (string, []cel.Link, error) {
			return cel.EvaluateAuditSummaryMap(template, renderMap)
		}
	}
	b.SupportActor = true
	b.supportCandidate = c
	vars := cel.BuildAuditVars(redactSupportActor(auditMap))
	ifaceType, _ := activitytypes.ClassifyInterface(audit.User.Username, audit.UserAgent)
	vars["interface"] = map[string]string{"type": ifaceType, "client": ""}
	return b, func(template string) (string, []cel.Link, error) {
		return cel.EvaluateAuditSummaryVars(template, vars)
	}
}

// supportActor is the actor recorded for a staff member acting as Datum
// Support: a fixed name and nothing that identifies the person, not even
// their UID, so tenants cannot track an individual across incidents. Datum's
// record of who it was is the raw audit event.
func supportActor() v1alpha1.ActivityActor {
	return v1alpha1.ActivityActor{
		Type: ActorTypeStaff,
		Name: SupportActorName,
	}
}

// redactSupportActor returns a copy of auditMap for rendering a staff actor's
// summary: user becomes {username: "Datum Support", uid: "", groups: [],
// extra: {}}, and impersonatedUser, userAgent and sourceIPs are removed. The
// original map is not modified.
func redactSupportActor(auditMap map[string]any) map[string]any {
	out := make(map[string]any, len(auditMap))
	for k, v := range auditMap {
		switch k {
		case "impersonatedUser", "userAgent", "sourceIPs":
			continue
		}
		out[k] = v
	}
	out["user"] = map[string]any{
		"username": SupportActorName,
		"uid":      "",
		"groups":   []any{},
		"extra":    map[string]any{},
	}
	return out
}

// dropSupportActorLinks removes the links that would identify a staff actor:
// links wrapped around the redacted actor (marker "Datum Support", such as
// link(actor, actorRef) or a bare {{ actor }}) and links to the staff
// member's own iam User, by metadata.name or
// metadata.uid. A staff actor gets no link, so nothing points at their User
// and their UID does not reach spec.relatedUIDs.
func dropSupportActorLinks(links []v1alpha1.ActivityLink, c StaffCandidate) []v1alpha1.ActivityLink {
	out := links[:0]
	for _, l := range links {
		if l.Marker == SupportActorName || isActorLink(l.Resource, SupportActorName) || c.Matches(l.Resource.UID) ||
			(isUserLink(l.Resource) && c.Matches(l.Resource.Name)) {
			continue
		}
		out = append(out, l)
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

// excludingUser hides a staff actor's own iam User from a resolver, so it is
// never used to hydrate a link in an activity that discloses them only as
// Datum Support.
type excludingUser struct {
	UserResolver
	candidate StaffCandidate
}

func (r excludingUser) hides(info UserInfo) bool {
	return r.candidate.Matches(info.Name) || r.candidate.Matches(info.UID)
}

func (r excludingUser) LookupByName(name string) (UserInfo, bool) {
	if r.candidate.Matches(name) {
		return UserInfo{}, false
	}
	info, ok := r.UserResolver.LookupByName(name)
	if !ok || r.hides(info) {
		return UserInfo{}, false
	}
	return info, true
}

func (r excludingUser) LookupByUID(uid string) (UserInfo, bool) {
	if r.candidate.Matches(uid) {
		return UserInfo{}, false
	}
	info, ok := r.UserResolver.LookupByUID(uid)
	if !ok || r.hides(info) {
		return UserInfo{}, false
	}
	return info, true
}
