package processor

import (
	"strings"

	authnv1 "k8s.io/api/authentication/v1"

	activitytypes "go.miloapis.com/activity/internal/types"
	"go.miloapis.com/activity/pkg/apis/activity/v1alpha1"
)

// ChangeSource constants for activity classification.
const (
	ChangeSourceHuman  = "human"
	ChangeSourceSystem = "system"
)

// ClassifyChangeSource determines whether an activity was initiated by a human
// or by the system. "system:" identities (controllers, Kubernetes service
// accounts) and Milo ServiceAccounts are system: a ServiceAccount is
// automation (CI, Terraform, scripts), so its changes belong with other
// non-human changes rather than in the people feed.
func ClassifyChangeSource(user authnv1.UserInfo) string {
	if strings.HasPrefix(user.Username, "system:") || activitytypes.IsMiloServiceAccount(user.Username) {
		return ChangeSourceSystem
	}

	return ChangeSourceHuman
}

// ActorType constants for actor classification.
const (
	ActorTypeUser           = "user"
	ActorTypeServiceAccount = "serviceaccount"
	ActorTypeSystem         = "system"
	ActorTypeController     = "controller"
)

// ResolveActor extracts actor information from the audit user field.
//
// Actor types:
//   - user: Human users authenticated via OIDC or other providers
//   - system: Kubernetes controllers, service accounts, and other system components
func ResolveActor(user authnv1.UserInfo) v1alpha1.ActivityActor {
	return ResolveActorWithResolver(user, nil)
}

// ResolveActorWithResolver behaves like ResolveActor but, for human users,
// also populates DisplayName and AvatarURL from the matching iam User. The
// user is found by the audit user.UID only (as metadata.name, then
// metadata.uid), never by email. System actors are never looked up. A nil resolver, or a miss, leaves the actor unenriched.
func ResolveActorWithResolver(user authnv1.UserInfo, resolver UserResolver) v1alpha1.ActivityActor {
	actor, _ := resolveActor(user, resolver)
	return actor
}

// resolveActor is ResolveActorWithResolver that also returns the matched iam
// User, or nil when no User matched, so callers can point the actor's summary
// link at that exact User.
func resolveActor(user authnv1.UserInfo, resolver UserResolver) (v1alpha1.ActivityActor, *UserInfo) {
	actor := v1alpha1.ActivityActor{
		UID: user.UID,
	}

	// Detect actor type based on username pattern
	switch {
	case strings.HasPrefix(user.Username, "system:"):
		// System component (controller, Kubernetes service account, node, etc.)
		actor.Type = ActorTypeSystem
		actor.Name = strings.TrimPrefix(user.Username, "system:")
	case activitytypes.IsMiloServiceAccount(user.Username):
		// Milo ServiceAccount: its username looks like an email but is not a
		// mailbox, and it has no iam User to enrich from.
		actor.Type = ActorTypeServiceAccount
		actor.Name = user.Username
	default:
		// Human user
		actor.Type = ActorTypeUser
		actor.Name = user.Username
		// Populate email field if username looks like an email
		if strings.Contains(user.Username, "@") {
			actor.Email = user.Username
		}
	}

	if actor.Name == "" {
		actor.Name = "unknown"
	}

	if actor.Type != ActorTypeUser {
		return actor, nil
	}

	// Policy hook: staff and platform actors are currently enriched like any
	// other user. If their names or avatars should be hidden from tenants,
	// decide that here, before the lookup, based on the actor and the
	// activity's tenant.
	info, ok := lookupActor(resolver, user.UID)
	if !ok {
		return actor, nil
	}
	actor.DisplayName = info.DisplayName()
	actor.AvatarURL = info.SafeAvatarURL()
	return actor, &info
}

// IsSystemActor returns true if the actor represents a system component.
func IsSystemActor(actor v1alpha1.ActivityActor) bool {
	return actor.Type == ActorTypeSystem
}

// IsHumanActor returns true if the actor represents a human user.
func IsHumanActor(actor v1alpha1.ActivityActor) bool {
	return actor.Type == ActorTypeUser
}
