package activityprocessor

import (
	"context"
	"fmt"
	"strings"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/rest"
	toolscache "k8s.io/client-go/tools/cache"
	"k8s.io/klog/v2"

	"go.miloapis.com/activity/internal/processor"
	"go.miloapis.com/activity/internal/storage"
	activitytypes "go.miloapis.com/activity/internal/types"
	"go.miloapis.com/activity/pkg/apis/activity/v1alpha1"
)

const (
	// StaffGroupName and StaffGroupNamespace identify the Milo Group whose
	// members are Datum staff, matching the staff portal's STAFF_GROUP_NAME
	// default (staff-portal app/utils/config/env.server.ts).
	StaffGroupName      = "staff-users"
	StaffGroupNamespace = "milo-system"

	staffUserIndex = "staffUser"
	userOrgIndex   = "userOrg"
	orgIndex       = "org"
)

// Staff redaction modes.
const (
	// StaffRedactionRequired uses staff-users and organization memberships
	// from Milo. Processing waits and tenant-scoped audit log queries are
	// refused (503) until they have loaded. The default, and the only mode
	// for clusters customers can access.
	StaffRedactionRequired = "required"
	// StaffRedactionDomainOnly uses only the staff email-domain rule, with no
	// membership indexes; it never waits or refuses. For clusters without
	// Milo (development and test infrastructure) only.
	StaffRedactionDomainOnly = "domain-only"
)

// ParseStaffRedactionMode validates a staff redaction mode; empty means
// StaffRedactionRequired.
func ParseStaffRedactionMode(mode string) (string, error) {
	switch mode {
	case "", StaffRedactionRequired:
		return StaffRedactionRequired, nil
	case StaffRedactionDomainOnly:
		return StaffRedactionDomainOnly, nil
	}
	return "", fmt.Errorf("invalid staff redaction mode %q: want %q or %q", mode, StaffRedactionRequired, StaffRedactionDomainOnly)
}

// WarnDomainOnlyStaffRedaction logs the startup warning for the domain-only
// mode.
func WarnDomainOnlyStaffRedaction(component string, domains []string) {
	klog.InfoS("WARNING: staff redaction mode is domain-only: Datum staff are recognised only by email domain, "+
		"without staff-users or organization memberships. Use this only in clusters without Milo that customers "+
		"cannot access.", "component", component, "domains", domains)
}

// DefaultStaffEmailDomains are the email domains treated as staff when no
// domains are configured.
var DefaultStaffEmailDomains = []string{"datum.net"}

// NoStaffEmailDomains, given as the only staff email domain, disables the
// email-domain rule: staff are then only members of staff-users, and nobody
// is treated as staff while the indexes have not synced.
const NoStaffEmailDomains = "none"

// ResolveStaffEmailDomains turns configured staff email domains into the
// list to use: empty means DefaultStaffEmailDomains, and NoStaffEmailDomains
// means none. Entries are lowercased and stripped of a leading "@".
func ResolveStaffEmailDomains(domains []string) []string {
	out := normalizeDomains(domains)
	if len(out) == 0 {
		return append([]string(nil), DefaultStaffEmailDomains...)
	}
	for _, d := range out {
		if d == NoStaffEmailDomains {
			return nil
		}
	}
	return out
}

var (
	groupMembershipGVR = schema.GroupVersionResource{Group: "iam.miloapis.com", Version: "v1alpha1", Resource: "groupmemberships"}
	orgMembershipGVR   = schema.GroupVersionResource{Group: "resourcemanager.miloapis.com", Version: "v1alpha1", Resource: "organizationmemberships"}
	projectGVR         = schema.GroupVersionResource{Group: "resourcemanager.miloapis.com", Version: "v1alpha1", Resource: "projects"}
)

func staffGauges(index string) indexGauges {
	m := currentIndexMetrics()
	return indexGauges{
		synced:   m.staffSynced.WithLabelValues(index),
		healthy:  m.staffHealthy.WithLabelValues(index),
		lastSync: m.staffLastSync.WithLabelValues(index),
	}
}

// StaffIndex implements processor.StaffResolver from three informer-backed
// indexes: memberships of the staff-users Group, OrganizationMemberships
// (user to organizations) and Projects (project to owning organization).
//
// An actor acts as Datum Support when they are staff (a member of
// staff-users, or an email in a staff domain) and not a member of the
// organization the activity belongs to (for a project, its owning
// organization; for a user scope, someone else's). Membership is evaluated
// when the activity is processed or re-indexed, not when the event happened.
//
// It fails closed for privacy: until all three indexes have synced (or if
// they cannot sync, for example because RBAC forbids them), an actor whose
// email domain is one of the configured staff domains is treated as staff in
// every customer-visible tenant.
type StaffIndex struct {
	groups, orgs, projects *informerIndex
	domains                []string

	// domainOnly is the domain-only staff redaction mode: no membership
	// indexes, only the email-domain rule, never waiting or refusing.
	domainOnly bool

	// users, when set, maps iam User names to metadata.uids so StaffScope
	// lists both forms. See SetUserResolver.
	users processor.UserResolver
}

// SetUserResolver lets StaffScope include each User's metadata.uid next to
// its name, so audit events recorded with either form match.
func (x *StaffIndex) SetUserResolver(users processor.UserResolver) {
	x.users = users
}

// NewStaffIndex creates a StaffIndex watching the API server described by
// restConfig. Call Start to begin watching.
func NewStaffIndex(restConfig *rest.Config, domains []string) (*StaffIndex, error) {
	dc, err := dynamic.NewForConfig(restConfig)
	if err != nil {
		return nil, err
	}
	return NewStaffIndexForClient(dc, domains)
}

// NewStaffIndexForClient creates a StaffIndex backed by the given dynamic
// client. Call Start to begin watching.
func NewStaffIndexForClient(dc dynamic.Interface, domains []string) (*StaffIndex, error) {
	x := &StaffIndex{domains: ResolveStaffEmailDomains(domains)}
	const impact = "staff are detected by email domain"
	var err error
	if x.groups, err = newInformerIndex(dc.Resource(groupMembershipGVR).Namespace(StaffGroupNamespace), indexConfig{
		description:    "staff group membership",
		resource:       "groupmemberships.iam.miloapis.com (namespace " + StaffGroupNamespace + ")",
		unsyncedImpact: impact,
		transform:      trimGroupMembership,
		indexers:       toolscache.Indexers{staffUserIndex: indexStaffUser},
		gauges:         staffGauges("groupmemberships"),
	}); err != nil {
		return nil, err
	}
	if x.orgs, err = newInformerIndex(dc.Resource(orgMembershipGVR), indexConfig{
		description:    "organization membership",
		resource:       "organizationmemberships.resourcemanager.miloapis.com",
		unsyncedImpact: impact,
		transform:      trimOrgMembership,
		indexers:       toolscache.Indexers{userOrgIndex: indexUserOrg, orgIndex: indexOrg},
		gauges:         staffGauges("organizationmemberships"),
	}); err != nil {
		return nil, err
	}
	if x.projects, err = newInformerIndex(dc.Resource(projectGVR), indexConfig{
		description:    "project",
		resource:       "projects.resourcemanager.miloapis.com",
		unsyncedImpact: impact,
		transform:      trimProject,
		gauges:         staffGauges("projects"),
	}); err != nil {
		return nil, err
	}
	return x, nil
}

// Start runs the informers in the background and returns immediately.
func (x *StaffIndex) Start(ctx context.Context) {
	for _, idx := range x.indexes() {
		idx.Start(ctx)
	}
}

// WaitForSync waits until all three indexes have synced, with the same
// semantics as the User index: nil once synced, an error wrapping
// ErrIndexUnavailable when one is persistently forbidden or not served, and
// an error wrapping ErrIndexSyncTimeout otherwise.
func (x *StaffIndex) WaitForSync(ctx context.Context, timeout time.Duration) error {
	if x.domainOnly {
		// Nothing to wait for: memberships are not used.
		return nil
	}
	if x.groups == nil {
		return fmt.Errorf("staff indexes %w: no API client", ErrIndexUnavailable)
	}
	deadline := time.Now().Add(timeout)
	for _, idx := range x.indexes() {
		if err := idx.WaitForSync(ctx, time.Until(deadline)); err != nil {
			return err
		}
	}
	return nil
}

// Synced reports whether all three indexes have synced, so the membership
// rule is in force rather than the email-domain fallback.
func (x *StaffIndex) Synced() bool {
	if x.groups == nil {
		return false
	}
	for _, idx := range x.indexes() {
		if !idx.Synced() {
			return false
		}
	}
	return true
}

func (x *StaffIndex) indexes() []*informerIndex {
	if x.groups == nil {
		return nil
	}
	return []*informerIndex{x.groups, x.orgs, x.projects}
}

// ActsAsSupport implements processor.StaffResolver. An actor acts as Datum
// Support when they are staff (a member of staff-users, or an email in one
// of the staff domains) and do not belong to the activity's tenant (for a
// project, its owning organization; for a user scope, their own).
// Platform-scoped activities are not shown to customers and are never
// redacted. Until the indexes have synced, the email-domain rule decides on
// its own and membership counts as unknown, i.e. not a member.
func (x *StaffIndex) ActsAsSupport(c processor.StaffCandidate, tenant v1alpha1.ActivityTenant) bool {
	if x.domainOnly || !x.Synced() {
		return x.hasStaffDomain(c.Username) && !isPlatformTenant(tenant)
	}
	if !x.isStaffMember(c) && !x.hasStaffDomain(c.Username) {
		return false
	}
	return !x.belongsTo(c, tenant)
}

func isPlatformTenant(tenant v1alpha1.ActivityTenant) bool {
	switch tenant.Type {
	case activitytypes.TenantTypeOrganization, activitytypes.TenantTypeProject, activitytypes.TenantTypeUser:
		return false
	}
	return true
}

// belongsTo reports whether the actor is a member of the tenant. Unknown
// membership counts as not a member.
func (x *StaffIndex) belongsTo(c processor.StaffCandidate, tenant v1alpha1.ActivityTenant) bool {
	switch tenant.Type {
	case activitytypes.TenantTypeOrganization:
		return x.isOrgMember(c, tenant.Name)
	case activitytypes.TenantTypeProject:
		org, ok := x.projectOrg(tenant.Name)
		return ok && x.isOrgMember(c, org)
	case activitytypes.TenantTypeUser:
		return c.Matches(tenant.Name)
	default:
		// Platform scope: not shown to customers.
		return true
	}
}

// isStaffMember reports whether the actor is a member of staff-users, by
// User metadata.name or metadata.uid.
func (x *StaffIndex) isStaffMember(c processor.StaffCandidate) bool {
	for _, id := range c.IDs() {
		if len(x.groups.byIndex(staffUserIndex, id)) > 0 {
			return true
		}
	}
	return false
}

func (x *StaffIndex) isOrgMember(c processor.StaffCandidate, org string) bool {
	if org == "" {
		return false
	}
	for _, id := range c.IDs() {
		if len(x.orgs.byIndex(userOrgIndex, id+"/"+org)) > 0 {
			return true
		}
	}
	return false
}

func (x *StaffIndex) projectOrg(project string) (string, bool) {
	p, ok := x.projects.getByKey(project).(*cachedProject)
	if !ok || p.OwnerName == "" || (p.OwnerKind != "" && p.OwnerKind != "Organization") {
		return "", false
	}
	return p.OwnerName, true
}

// hasStaffDomain reports whether username is an email address in one of the
// staff domains, or a subdomain of one.
func (x *StaffIndex) hasStaffDomain(username string) bool {
	return storage.HasStaffDomain(username, x.domains)
}

func normalizeDomains(domains []string) []string {
	out := make([]string, 0, len(domains))
	for _, d := range domains {
		d = strings.ToLower(strings.TrimPrefix(strings.TrimSpace(d), "@"))
		if d != "" {
			out = append(out, d)
		}
	}
	return out
}

// NewDomainOnlyStaffIndex returns a StaffIndex for the domain-only staff
// redaction mode: no membership indexes, only the email-domain rule. It never
// blocks processing and never refuses queries. It is meant for clusters
// without Milo (development, test infrastructure) and must not be used where
// customers have access.
func NewDomainOnlyStaffIndex(domains []string) *StaffIndex {
	return &StaffIndex{domains: ResolveStaffEmailDomains(domains), domainOnly: true}
}

// NewUnavailableStaffIndex returns a StaffIndex that never syncs, for the
// required mode when memberships cannot be watched: processing waits and
// tenant-scoped audit log queries are refused.
func NewUnavailableStaffIndex(domains []string) *StaffIndex {
	return &StaffIndex{domains: ResolveStaffEmailDomains(domains)}
}

// StaffScope implements storage.StaffScopeProvider, for redacting staff
// identities in tenant-scoped audit log queries with the same rule as
// activities. Platform and user scopes are not redacted (the user scope is
// the user's own feed). Until the indexes have synced, only the domain rule
// applies and nobody counts as a member of the scope's organization.
func (x *StaffIndex) StaffScope(scope storage.ScopeContext) storage.StaffScope {
	if scope.Type != activitytypes.TenantTypeOrganization && scope.Type != activitytypes.TenantTypeProject {
		return storage.StaffScope{}
	}
	ss := storage.StaffScope{Redact: true, Domains: x.domains}
	if x.domainOnly {
		ss.DomainOnly = true
		return ss
	}
	if !x.Synced() {
		return ss
	}
	ss.Synced = true

	// The organization's members, by name and (when known) metadata.uid.
	var members []string
	if org, ok := x.scopeOrg(scope); ok {
		for _, obj := range x.orgs.byIndex(orgIndex, org) {
			if m, ok := obj.(*cachedOrgMembership); ok {
				members = append(members, m.UserName)
			}
		}
	}
	memberIDs := map[string]bool{}
	for _, id := range x.withUIDs(members) {
		memberIDs[id] = true
	}

	// Staff-users members outside the organization.
	for _, name := range x.groups.informer.GetIndexer().ListIndexFuncValues(staffUserIndex) {
		ids := x.withUIDs([]string{name})
		member := false
		for _, id := range ids {
			member = member || memberIDs[id]
		}
		if !member {
			ss.StaffOutsideOrg = append(ss.StaffOutsideOrg, ids...)
		}
	}

	// Members whose email is in a staff domain: the domain rule must spare
	// them. Without the User index their emails are unknown, so they are
	// redacted too (over-redaction, never under-redaction).
	if x.users != nil && len(x.domains) > 0 {
		for _, name := range members {
			if info, ok := x.users.LookupByName(name); ok && storage.HasStaffDomain(info.Email, x.domains) {
				ss.DomainMembers = append(ss.DomainMembers, x.withUIDs([]string{name})...)
			}
		}
	}
	return ss
}

// scopeOrg is the organization a tenant scope belongs to: the organization
// itself, or a project's owner. Unknown projects have none.
func (x *StaffIndex) scopeOrg(scope storage.ScopeContext) (string, bool) {
	if scope.Type == activitytypes.TenantTypeProject {
		return x.projectOrg(scope.Name)
	}
	return scope.Name, scope.Name != ""
}

// withUIDs returns names plus, where the User index knows them, each User's
// metadata.uid.
func (x *StaffIndex) withUIDs(names []string) []string {
	out := append([]string(nil), names...)
	if x.users == nil {
		return out
	}
	for _, n := range names {
		if info, ok := x.users.LookupByName(n); ok && info.UID != "" && info.UID != n {
			out = append(out, info.UID)
		}
	}
	return out
}

var (
	_ processor.StaffResolver    = (*StaffIndex)(nil)
	_ storage.StaffScopeProvider = (*StaffIndex)(nil)
)

// cachedGroupMembership is a GroupMembership trimmed to who is in which group.
type cachedGroupMembership struct {
	metav1.ObjectMeta
	UserName, GroupName, GroupNamespace string
}

func (m *cachedGroupMembership) GetObjectKind() schema.ObjectKind { return schema.EmptyObjectKind }
func (m *cachedGroupMembership) DeepCopyObject() runtime.Object {
	out := *m
	m.DeepCopyInto(&out.ObjectMeta)
	return &out
}

// cachedOrgMembership is an OrganizationMembership trimmed to user and
// organization.
type cachedOrgMembership struct {
	metav1.ObjectMeta
	UserName, OrgName string
}

func (m *cachedOrgMembership) GetObjectKind() schema.ObjectKind { return schema.EmptyObjectKind }
func (m *cachedOrgMembership) DeepCopyObject() runtime.Object {
	out := *m
	m.DeepCopyInto(&out.ObjectMeta)
	return &out
}

// cachedProject is a Project trimmed to its owner.
type cachedProject struct {
	metav1.ObjectMeta
	OwnerKind, OwnerName string
}

func (p *cachedProject) GetObjectKind() schema.ObjectKind { return schema.EmptyObjectKind }
func (p *cachedProject) DeepCopyObject() runtime.Object {
	out := *p
	p.DeepCopyInto(&out.ObjectMeta)
	return &out
}

// trimmedMeta keeps only what the informer store needs.
func trimmedMeta(o *unstructured.Unstructured) metav1.ObjectMeta {
	return metav1.ObjectMeta{Name: o.GetName(), Namespace: o.GetNamespace(), ResourceVersion: o.GetResourceVersion()}
}

func nested(o *unstructured.Unstructured, fields ...string) string {
	v, _, _ := unstructured.NestedString(o.Object, fields...)
	return v
}

func trimGroupMembership(obj any) (any, error) {
	o, ok := obj.(*unstructured.Unstructured)
	if !ok {
		return obj, nil
	}
	return &cachedGroupMembership{
		ObjectMeta:     trimmedMeta(o),
		UserName:       nested(o, "spec", "userRef", "name"),
		GroupName:      nested(o, "spec", "groupRef", "name"),
		GroupNamespace: nested(o, "spec", "groupRef", "namespace"),
	}, nil
}

func trimOrgMembership(obj any) (any, error) {
	o, ok := obj.(*unstructured.Unstructured)
	if !ok {
		return obj, nil
	}
	return &cachedOrgMembership{
		ObjectMeta: trimmedMeta(o),
		UserName:   nested(o, "spec", "userRef", "name"),
		OrgName:    nested(o, "spec", "organizationRef", "name"),
	}, nil
}

func trimProject(obj any) (any, error) {
	o, ok := obj.(*unstructured.Unstructured)
	if !ok {
		return obj, nil
	}
	return &cachedProject{
		ObjectMeta: trimmedMeta(o),
		OwnerKind:  nested(o, "spec", "ownerRef", "kind"),
		OwnerName:  nested(o, "spec", "ownerRef", "name"),
	}, nil
}

// indexStaffUser indexes memberships of the staff group by user name.
func indexStaffUser(obj any) ([]string, error) {
	m, ok := obj.(*cachedGroupMembership)
	if !ok || m.UserName == "" || m.GroupName != StaffGroupName ||
		(m.GroupNamespace != "" && m.GroupNamespace != StaffGroupNamespace) {
		return nil, nil
	}
	return []string{m.UserName}, nil
}

// indexOrg indexes organization memberships by organization.
func indexOrg(obj any) ([]string, error) {
	m, ok := obj.(*cachedOrgMembership)
	if !ok || m.UserName == "" || m.OrgName == "" {
		return nil, nil
	}
	return []string{m.OrgName}, nil
}

// indexUserOrg indexes organization memberships by "<user>/<org>".
func indexUserOrg(obj any) ([]string, error) {
	m, ok := obj.(*cachedOrgMembership)
	if !ok || m.UserName == "" || m.OrgName == "" {
		return nil, nil
	}
	return []string{m.UserName + "/" + m.OrgName}, nil
}
