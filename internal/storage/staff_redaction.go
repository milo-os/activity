package storage

import (
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/ClickHouse/clickhouse-go/v2"
	authnv1 "k8s.io/api/authentication/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	auditv1 "k8s.io/apiserver/pkg/apis/audit/v1"

	"go.miloapis.com/activity/internal/types"
)

// SupportActorName replaces a staff member's username in tenant-scoped audit
// log results, matching how activities show staff.
const SupportActorName = "Datum Support"

// staffActorType is the actor type of activities recorded as staff access.
const staffActorType = "staff"

// serviceAccountSuffix ends Milo ServiceAccount usernames, which are never
// staff (see types.IsMiloServiceAccount).
const serviceAccountSuffix = ".identity.miloapis.com"

// ErrStaffRedactionNotReady is returned for tenant-scoped audit log queries
// while staff memberships are not yet known, so staff identities cannot be
// redacted reliably. See StaffRedactionNotReadyError.
var ErrStaffRedactionNotReady = errors.New("staff memberships are not loaded yet")

// staffNotReadyRetryAfter is the Retry-After hint, in seconds, for
// ErrStaffRedactionNotReady.
const staffNotReadyRetryAfter = 10

// StaffRedactionNotReadyError is the API error for ErrStaffRedactionNotReady:
// 503 Service Unavailable with a Retry-After hint.
func StaffRedactionNotReadyError() *apierrors.StatusError {
	return &apierrors.StatusError{ErrStatus: metav1.Status{
		Status: metav1.StatusFailure,
		Code:   503,
		Reason: metav1.StatusReasonServiceUnavailable,
		Message: "Audit logs for this scope are temporarily unavailable while Datum staff memberships load. " +
			"Retry shortly.",
		Details: &metav1.StatusDetails{RetryAfterSeconds: staffNotReadyRetryAfter},
	}}
}

// StaffScope tells the audit log queries which actors are Datum staff
// relative to one tenant scope, so their identities can be redacted. The
// lists are precomputed for the scope so they stay small whatever the size of
// the organization.
type StaffScope struct {
	// Redact is false where no redaction applies: platform scope (not shown
	// to customers) and user scope (the user's own feed).
	Redact bool
	// Synced is true once staff and organization memberships are known.
	// Tenant-scoped queries are refused (ErrStaffRedactionNotReady) until
	// then, unless DomainOnly is set.
	Synced bool
	// DomainOnly means memberships are not used at all (the domain-only
	// staff redaction mode, for clusters without Milo): only the
	// email-domain rule applies.
	DomainOnly bool
	// StaffOutsideOrg identifies the staff-users members who are not members
	// of the scope's organization (for a project, its owning organization),
	// by iam User name (equal to the audit user.uid in Milo) and, where
	// known, metadata.uid.
	StaffOutsideOrg []string
	// DomainMembers identifies the members of the scope's organization whose
	// email is in a staff domain, in the same forms. The domain rule does not
	// apply to them.
	DomainMembers []string
	// Domains are the staff email domains; usernames in them, or in a
	// subdomain, count as staff.
	Domains []string
}

// IsStaff reports whether an identity is staff relative to this scope, with
// the same rule as the SQL in auditRedactionFor: not a system identity or
// Milo ServiceAccount, and either a staff-users member outside the
// organization or a staff-domain email that is not an organization member.
func (ss StaffScope) IsStaff(uid, username string) bool {
	if !ss.Redact || strings.HasPrefix(username, "system:") || types.IsMiloServiceAccount(username) {
		return false
	}
	return contains(ss.StaffOutsideOrg, uid) || (ss.hasStaffDomain(username) && !contains(ss.DomainMembers, uid))
}

func (ss StaffScope) hasStaffDomain(username string) bool {
	return HasStaffDomain(username, ss.Domains)
}

// HasStaffDomain reports whether username is an email address whose domain
// (after the last "@") is one of domains or a subdomain of one.
func HasStaffDomain(username string, domains []string) bool {
	at := strings.LastIndexByte(username, '@')
	if at < 0 {
		return false
	}
	domain := strings.ToLower(username[at+1:])
	for _, d := range domains {
		if domain == d || strings.HasSuffix(domain, "."+d) {
			return true
		}
	}
	return false
}

func contains(list []string, v string) bool {
	if v == "" {
		return false
	}
	for _, s := range list {
		if s == v {
			return true
		}
	}
	return false
}

// StaffScopeProvider resolves the StaffScope for a query scope.
type StaffScopeProvider interface {
	StaffScope(scope ScopeContext) StaffScope
}

// SetStaffScopes enables staff redaction in tenant-scoped audit log queries
// and facets. Without a provider no redaction is applied, so the API server
// must always set one.
func (s *ClickHouseStorage) SetStaffScopes(p StaffScopeProvider) {
	s.staffScopes = p
}

// auditRedaction is the SQL that marks staff rows in a tenant-scoped audit
// log query, and the scope it was built for.
type auditRedaction struct {
	// with is a WITH clause defining is_staff_actor, redacted_user and
	// redacted_user_uid; args are its bound parameters, which must come first.
	with  string
	args  []any
	scope StaffScope
}

// auditColumnOverrides make user.username and user.uid filters (and facets)
// see "Datum Support" and "" for staff rows: filtering on a staff identity
// matches nothing, and filtering on "Datum Support" matches the redacted rows.
var auditColumnOverrides = map[string]string{
	"user":     "redacted_user",
	"user_uid": "redacted_user_uid",
}

// sqlBuilder accumulates SQL fragments and their positional arguments in
// text order.
type sqlBuilder struct{ args []any }

func (b *sqlBuilder) add(sql string, args ...any) string {
	b.args = append(b.args, args...)
	return sql
}

// inSet renders "col IN (…)" for a bound tuple. An empty list matches
// nothing.
func (b *sqlBuilder) inSet(col string, values []string) string {
	if len(values) == 0 {
		return "0"
	}
	set := make([]any, len(values))
	for i, v := range values {
		set[i] = v
	}
	return b.add(col+" IN ?", clickhouse.GroupSet{Value: set})
}

// stickyRetention bounds the sticky lookup when a query has no start time:
// activities are deleted after 60 days (the activities table TTL), so older
// staff activities cannot exist.
const stickyRetention = 60 * 24 * time.Hour

// auditRedactionFor returns the redaction for a scope, or nil when none
// applies. It returns ErrStaffRedactionNotReady for a tenant scope while
// staff memberships are not loaded (except in domain-only mode).
//
// A row is staff when either:
//   - its actor is staff relative to the scope now (StaffScope.IsStaff), or
//   - it produced an activity already stored as staff access in this
//     tenant, so redaction is sticky like re-indexing. Audit events that
//     produced no activity rely on current membership only.
//
// start and end bound the sticky lookup on the activities table (whose
// primary key starts with the timestamp). Without a start, the lookup covers
// the activities retention window.
func (s *ClickHouseStorage) auditRedactionFor(scope ScopeContext, start, end *time.Time) (*auditRedaction, error) {
	if s.staffScopes == nil || scope.Type == types.TenantTypePlatform || scope.Type == types.TenantTypeUser {
		return nil, nil
	}
	ss := s.staffScopes.StaffScope(scope)
	if !ss.Redact {
		return nil, nil
	}
	if !ss.Synced && !ss.DomainOnly {
		return nil, ErrStaffRedactionNotReady
	}

	var b sqlBuilder

	// Same exclusions as Go: never system identities or Milo ServiceAccounts.
	human := b.add("NOT startsWith(user, 'system:') AND NOT endsWith(lower(user), ?)", serviceAccountSuffix)

	// Staff-users members who are not members of this organization.
	staffSQL := b.inSet("user_uid", ss.StaffOutsideOrg)

	// The domain rule: an email whose domain (after the last "@") is a staff
	// domain or a subdomain of one, unless the user is a member of this
	// organization.
	domainSQL := "0"
	if len(ss.Domains) > 0 {
		var terms []string
		for _, d := range ss.Domains {
			terms = append(terms, b.add("staff_domain = ? OR endsWith(staff_domain, ?)", d, "."+d))
		}
		domainSQL = "(position(user, '@') > 0 AND (" + strings.Join(terms, " OR ") + ") AND NOT " +
			b.inSet("user_uid", ss.DomainMembers) + ")"
	}

	// Sticky: the audit event produced an activity recorded as staff access.
	// The UUID audit_id column is compared with origin_id converted to a UUID
	// on the subquery side, so the comparison is typed and the audit_logs
	// column is used as-is (a function applied to audit_id would stop
	// ClickHouse using the primary key on it).
	sticky := b.add(fmt.Sprintf("audit_id IN (SELECT toUUIDOrZero(origin_id) FROM %s.activities WHERE actor_type = ? "+
		"AND origin_type = 'audit' AND tenant_type = ? AND tenant_name = ?", s.config.Database), staffActorType, scope.Type, scope.Name)
	lower := time.Now().Add(-stickyRetention)
	if start != nil {
		lower = start.Add(-time.Hour)
	}
	sticky += b.add(" AND timestamp >= ?", lower)
	if end != nil {
		sticky += b.add(" AND timestamp < ?", end.Add(time.Hour))
	}
	sticky += ")"

	isStaff := "((" + human + " AND (" + staffSQL + " OR " + domainSQL + ")) OR " + sticky + ")"
	return &auditRedaction{
		with: "WITH lower(arrayElement(splitByChar('@', user), -1)) AS staff_domain, " +
			isStaff + " AS is_staff_actor, " +
			"if(is_staff_actor, '" + SupportActorName + "', user) AS redacted_user, " +
			"if(is_staff_actor, '', user_uid) AS redacted_user_uid ",
		args:  b.args,
		scope: ss,
	}, nil
}

// minimalStaffAuditEvent is the event returned to a tenant in place of a
// staff member's audit event: an allowlist of fields that do not record who
// the person is. objectRef keeps the object's name, namespace and uid, which
// the event is useless without; an object named after a person (for example
// a staff member's own User) can still hint at who acted. It keeps the event's identity and timing, what was done to
// which object and the response code, and shows the user as "Datum
// Support". Everything else (requestURI, annotations such as the
// authorization reason, the response status message, reason and details,
// request and response objects, sourceIPs, userAgent, impersonatedUser, and
// the user's uid, groups and extra) is omitted.
func minimalStaffAuditEvent(e *auditv1.Event) auditv1.Event {
	out := auditv1.Event{
		TypeMeta:                 e.TypeMeta,
		Level:                    e.Level,
		AuditID:                  e.AuditID,
		Stage:                    e.Stage,
		Verb:                     e.Verb,
		User:                     authnv1.UserInfo{Username: SupportActorName},
		RequestReceivedTimestamp: e.RequestReceivedTimestamp,
		StageTimestamp:           e.StageTimestamp,
	}
	if e.ObjectRef != nil {
		out.ObjectRef = &auditv1.ObjectReference{
			APIGroup:    e.ObjectRef.APIGroup,
			APIVersion:  e.ObjectRef.APIVersion,
			Resource:    e.ObjectRef.Resource,
			Subresource: e.ObjectRef.Subresource,
			Namespace:   e.ObjectRef.Namespace,
			Name:        e.ObjectRef.Name,
			UID:         e.ObjectRef.UID,
		}
	}
	if e.ResponseStatus != nil {
		out.ResponseStatus = &metav1.Status{Code: e.ResponseStatus.Code}
	}
	return out
}

// redactAuditEventForScope applies staff redaction to one tenant-scoped
// result: a staff row becomes the minimal event; any other row loses its
// impersonatedUser when that is a staff identity relative to the scope.
func redactAuditEventForScope(e auditv1.Event, isStaffRow bool, ss StaffScope) auditv1.Event {
	if isStaffRow {
		return minimalStaffAuditEvent(&e)
	}
	if e.ImpersonatedUser != nil && ss.IsStaff(e.ImpersonatedUser.UID, e.ImpersonatedUser.Username) {
		e.ImpersonatedUser = nil
	}
	return e
}

// AuditRedactionSQLForTest returns the redaction WITH clause and arguments
// for scope under provider p, for tests outside this package.
func AuditRedactionSQLForTest(s *ClickHouseStorage, p StaffScopeProvider, scope ScopeContext) (string, []any) {
	c := *s
	c.staffScopes = p
	r, err := c.auditRedactionFor(scope, nil, nil)
	if err != nil || r == nil {
		return "", nil
	}
	return r.with, r.args
}
