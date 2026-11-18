package activityprocessor

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/rest"
	toolscache "k8s.io/client-go/tools/cache"

	"go.miloapis.com/activity/internal/processor"
)

// userGVR identifies Milo's iam User resource. Users are watched as
// Unstructured so the processor does not depend on Milo's Go types.
var userGVR = schema.GroupVersionResource{
	Group:    "iam.miloapis.com",
	Version:  "v1alpha1",
	Resource: "users",
}

// userUIDIndex indexes cached users by metadata.uid.
const userUIDIndex = "uid"

// cachedUser is the trimmed form of an iam User kept in the informer's store.
// Only the fields needed for enrichment are retained to keep memory small.
// It embeds ObjectMeta (with only Name, UID and ResourceVersion set) so the
// informer's key functions can read it like any other object.
type cachedUser struct {
	metav1.ObjectMeta

	// Email is kept only for the staff email-domain rule.
	Email      string
	GivenName  string
	FamilyName string
	AvatarURL  string
}

// GetObjectKind implements runtime.Object.
func (u *cachedUser) GetObjectKind() schema.ObjectKind { return schema.EmptyObjectKind }

// DeepCopyObject implements runtime.Object.
func (u *cachedUser) DeepCopyObject() runtime.Object {
	out := *u
	u.DeepCopyInto(&out.ObjectMeta)
	return &out
}

func (u *cachedUser) userInfo() processor.UserInfo {
	return processor.UserInfo{
		Name:       u.Name,
		UID:        string(u.UID),
		Email:      u.Email,
		GivenName:  u.GivenName,
		FamilyName: u.FamilyName,
		AvatarURL:  u.AvatarURL,
	}
}

// trimUser is the informer transform. It reduces an Unstructured iam User to
// a cachedUser before it is stored. It is idempotent, as client-go requires.
func trimUser(obj any) (any, error) {
	switch o := obj.(type) {
	case *cachedUser:
		return o, nil
	case *unstructured.Unstructured:
		email, _, _ := unstructured.NestedString(o.Object, "spec", "email")
		given, _, _ := unstructured.NestedString(o.Object, "spec", "givenName")
		family, _, _ := unstructured.NestedString(o.Object, "spec", "familyName")
		avatar, _, _ := unstructured.NestedString(o.Object, "status", "avatarUrl")
		return &cachedUser{
			ObjectMeta: metav1.ObjectMeta{
				Name:            o.GetName(),
				UID:             o.GetUID(),
				ResourceVersion: o.GetResourceVersion(),
			},
			Email:      email,
			GivenName:  given,
			FamilyName: family,
			AvatarURL:  avatar,
		}, nil
	default:
		// Tombstones and anything unexpected pass through untouched.
		return obj, nil
	}
}

func indexUserByUID(obj any) ([]string, error) {
	u, ok := obj.(*cachedUser)
	if !ok || u.UID == "" {
		return nil, nil
	}
	return []string{string(u.UID)}, nil
}

// UserIndex is an informer-backed, in-memory index of Milo iam Users. It
// implements processor.UserResolver with O(1) map reads and no API calls per
// lookup, and is safe for concurrent use.
//
// Until the informer has completed its initial sync, every lookup misses, so
// activities are emitted unenriched. See informerIndex for how failures and
// outages are handled; user_index_healthy reports 0 during an outage while the
// last-known index keeps being served.
type UserIndex struct {
	*informerIndex
}

// NewUserIndex creates a UserIndex that watches iam Users on the API server
// described by restConfig. Call Start to begin watching.
func NewUserIndex(restConfig *rest.Config) (*UserIndex, error) {
	dc, err := dynamic.NewForConfig(restConfig)
	if err != nil {
		return nil, err
	}
	return NewUserIndexForClient(dc)
}

// NewUserIndexForClient creates a UserIndex backed by the given dynamic
// client. Call Start to begin watching.
func NewUserIndexForClient(dc dynamic.Interface) (*UserIndex, error) {
	idx, err := newInformerIndex(dc.Resource(userGVR), indexConfig{
		description:    "iam User",
		resource:       "users.iam.miloapis.com",
		unsyncedImpact: "activities are emitted without actor display names",
		transform:      trimUser,
		indexers:       toolscache.Indexers{userUIDIndex: indexUserByUID},
		gauges:         currentIndexMetrics().userGauges(),
	})
	if err != nil {
		return nil, err
	}
	return &UserIndex{informerIndex: idx}, nil
}

// LookupByName implements processor.UserResolver. Users are cluster-scoped,
// so metadata.name is the informer's store key.
func (x *UserIndex) LookupByName(name string) (processor.UserInfo, bool) {
	if name == "" || !x.Synced() {
		return processor.UserInfo{}, false
	}
	if u, ok := x.getByKey(name).(*cachedUser); ok {
		return u.userInfo(), true
	}
	return processor.UserInfo{}, false
}

// LookupByUID implements processor.UserResolver.
func (x *UserIndex) LookupByUID(uid string) (processor.UserInfo, bool) {
	if uid == "" || !x.Synced() {
		return processor.UserInfo{}, false
	}
	for _, obj := range x.byIndex(userUIDIndex, uid) {
		if u, ok := obj.(*cachedUser); ok {
			return u.userInfo(), true
		}
	}
	return processor.UserInfo{}, false
}

var _ processor.UserResolver = (*UserIndex)(nil)
