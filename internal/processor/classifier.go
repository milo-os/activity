package processor

import (
	"context"
	"strings"

	authnv1 "k8s.io/api/authentication/v1"

	"go.miloapis.com/activity/pkg/apis/activity/v1alpha1"
)

// ChangeSource constants for activity classification.
const (
	ChangeSourceHuman  = "human"
	ChangeSourceSystem = "system"
)

// ClassifyChangeSource determines whether an activity was initiated by a human
// or by the system (controllers, service accounts, etc.).
// System accounts always use a "system:" prefix for the username.
func ClassifyChangeSource(user authnv1.UserInfo) string {
	if strings.HasPrefix(user.Username, "system:") {
		return ChangeSourceSystem
	}

	return ChangeSourceHuman
}

// ActorType constants for actor classification.
const (
	ActorTypeUser       = "user"
	ActorTypeSystem     = "system"
	ActorTypeController = "controller"
)

// ResolveActor extracts actor information from the audit user field.
//
// Actor types:
//   - user: Human users authenticated via OIDC or other providers
//   - system: Kubernetes controllers, service accounts, and other system components
func ResolveActor(user authnv1.UserInfo) v1alpha1.ActivityActor {
	return ResolveActorWithResolver(context.Background(), user, nil)
}

// ResolveActorWithResolver behaves like ResolveActor but additionally
// populates ActivityActor.DisplayName for human users when resolver is non-nil
// and a matching User record is found. Resolver errors are silently ignored:
// the activity is still emitted with whatever data is available.
func ResolveActorWithResolver(ctx context.Context, user authnv1.UserInfo, resolver UserResolver) v1alpha1.ActivityActor {
	actor := v1alpha1.ActivityActor{
		UID: user.UID,
	}

	// Detect actor type based on username pattern
	if strings.HasPrefix(user.Username, "system:") {
		// System component (controller, service account, node, etc.)
		actor.Type = ActorTypeSystem
		actor.Name = strings.TrimPrefix(user.Username, "system:")
	} else {
		// Human user
		actor.Type = ActorTypeUser
		actor.Name = user.Username
	}

	// Populate email field if username looks like an email
	if strings.Contains(user.Username, "@") {
		actor.Email = user.Username
	}

	if actor.Name == "" {
		actor.Name = "unknown"
	}

	// Enrich with display name for human users when a resolver is available.
	if resolver != nil && actor.Type == ActorTypeUser && actor.Email != "" {
		if info, ok, err := resolver.LookupByEmail(ctx, actor.Email); err == nil && ok {
			if dn := info.DisplayName(); dn != "" {
				actor.DisplayName = dn
			}
		}
	}

	return actor
}

// IsSystemActor returns true if the actor represents a system component.
func IsSystemActor(actor v1alpha1.ActivityActor) bool {
	return actor.Type == ActorTypeSystem
}

// IsHumanActor returns true if the actor represents a human user.
func IsHumanActor(actor v1alpha1.ActivityActor) bool {
	return actor.Type == ActorTypeUser
}
