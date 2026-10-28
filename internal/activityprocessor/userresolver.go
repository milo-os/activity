package activityprocessor

import (
	"context"
	"fmt"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"go.miloapis.com/activity/internal/processor"
)

// userGVK identifies the iam User custom resource we resolve display names
// from. The activity processor never serves these objects, so we query them
// as Unstructured to avoid pulling in the milo iam Go types.
var userGVK = schema.GroupVersionKind{
	Group:   "iam.miloapis.com",
	Version: "v1alpha1",
	Kind:    "User",
}

// IAMUserResolver implements processor.UserResolver against an iam User CR
// store reached through a controller-runtime client. It is safe for
// concurrent use; wrap with processor.NewCachedUserResolver to add caching
// and per-key single-flight.
type IAMUserResolver struct {
	Client client.Client
}

// NewIAMUserResolver returns a resolver that fetches iam Users via c.
func NewIAMUserResolver(c client.Client) *IAMUserResolver {
	return &IAMUserResolver{Client: c}
}

// LookupByEmail finds the first iam User whose spec.email matches the given
// address. Returns ok=false when no match is found or email is empty.
func (r *IAMUserResolver) LookupByEmail(ctx context.Context, email string) (processor.UserInfo, bool, error) {
	if email == "" || r == nil || r.Client == nil {
		return processor.UserInfo{}, false, nil
	}

	list := &unstructured.UnstructuredList{}
	list.SetGroupVersionKind(schema.GroupVersionKind{
		Group:   userGVK.Group,
		Version: userGVK.Version,
		Kind:    userGVK.Kind + "List",
	})

	// Most clusters do not index spec.email server-side; list all and filter
	// in process. The cached wrapper amortizes this across calls; if scale
	// becomes a concern, register a field indexer for spec.email in the
	// manager's cache.
	if err := r.Client.List(ctx, list); err != nil {
		return processor.UserInfo{}, false, fmt.Errorf("list iam users: %w", err)
	}

	for i := range list.Items {
		item := &list.Items[i]
		got, _, _ := unstructured.NestedString(item.Object, "spec", "email")
		if got == email {
			return userInfoFromUnstructured(item), true, nil
		}
	}

	return processor.UserInfo{}, false, nil
}

// LookupByName fetches an iam User by metadata.name and returns its display
// fields. Returns ok=false on NotFound.
func (r *IAMUserResolver) LookupByName(ctx context.Context, name string) (processor.UserInfo, bool, error) {
	if name == "" || r == nil || r.Client == nil {
		return processor.UserInfo{}, false, nil
	}

	obj := &unstructured.Unstructured{}
	obj.SetGroupVersionKind(userGVK)

	if err := r.Client.Get(ctx, client.ObjectKey{Name: name}, obj); err != nil {
		if apierrors.IsNotFound(err) {
			return processor.UserInfo{}, false, nil
		}
		return processor.UserInfo{}, false, fmt.Errorf("get iam user %q: %w", name, err)
	}

	return userInfoFromUnstructured(obj), true, nil
}

func userInfoFromUnstructured(obj *unstructured.Unstructured) processor.UserInfo {
	given, _, _ := unstructured.NestedString(obj.Object, "spec", "givenName")
	family, _, _ := unstructured.NestedString(obj.Object, "spec", "familyName")
	email, _, _ := unstructured.NestedString(obj.Object, "spec", "email")
	return processor.UserInfo{
		Name:       obj.GetName(),
		GivenName:  given,
		FamilyName: family,
		Email:      email,
		UID:        string(obj.GetUID()),
	}
}
