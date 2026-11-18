package processor

import (
	activitytypes "go.miloapis.com/activity/internal/types"
	"go.miloapis.com/activity/pkg/apis/activity/v1alpha1"
)

// ResolveInterface reports how an audited change was made, from the request's
// username and user agent. See activitytypes.ClassifyInterface for the rules.
// The result is client-reported and unverified: a display hint, never an
// input to authorization or alerting.
func ResolveInterface(username, userAgent string) *v1alpha1.ActivityInterface {
	typ, client := activitytypes.ClassifyInterface(username, userAgent)
	return &v1alpha1.ActivityInterface{Type: typ, Client: client}
}

// eventInterface is the interface for an activity built from a Kubernetes
// event: events are reported by controllers, so the type is "controller" and
// the client is the reporting controller (or source component).
func eventInterface(actor v1alpha1.ActivityActor) *v1alpha1.ActivityInterface {
	typ, client := activitytypes.ClassifyInterface("system:"+actor.Name, "")
	return &v1alpha1.ActivityInterface{Type: typ, Client: client}
}
