package activityprocessor

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"

	authnv1 "k8s.io/api/authentication/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	dynamicfake "k8s.io/client-go/dynamic/fake"
	k8stesting "k8s.io/client-go/testing"

	"go.miloapis.com/activity/internal/processor"
)

func newTestUser(name, uid, email, given, family, avatar string) *unstructured.Unstructured {
	u := &unstructured.Unstructured{Object: map[string]any{
		"apiVersion": "iam.miloapis.com/v1alpha1",
		"kind":       "User",
		"metadata": map[string]any{
			"name":            name,
			"uid":             uid,
			"resourceVersion": "1",
		},
		"spec": map[string]any{
			"email":      email,
			"givenName":  given,
			"familyName": family,
		},
	}}
	if avatar != "" {
		_ = unstructured.SetNestedField(u.Object, avatar, "status", "avatarUrl")
	}
	return u
}

func userInfoFixtureFor(username, uid string) authnv1.UserInfo {
	return authnv1.UserInfo{Username: username, UID: uid}
}

func newFakeUserClient(objs ...runtime.Object) *dynamicfake.FakeDynamicClient {
	return dynamicfake.NewSimpleDynamicClientWithCustomListKinds(
		runtime.NewScheme(),
		map[schema.GroupVersionResource]string{userGVR: "UserList"},
		objs...,
	)
}

func newTestIndex(t *testing.T, client *dynamicfake.FakeDynamicClient) *UserIndex {
	t.Helper()
	idx, err := NewUserIndexForClient(client)
	if err != nil {
		t.Fatalf("NewUserIndexForClient: %v", err)
	}
	return idx
}

func startIndex(t *testing.T, idx *UserIndex) {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	idx.Start(ctx)
}

func startTestIndex(t *testing.T, client *dynamicfake.FakeDynamicClient) *UserIndex {
	t.Helper()
	idx := newTestIndex(t, client)
	startIndex(t, idx)
	return idx
}

// eventually polls cond until it returns true or the deadline passes.
func eventually(t *testing.T, msg string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for: %s", msg)
}

func TestTrimUser_KeepsOnlyEnrichmentFields(t *testing.T) {
	u := newTestUser("340583683847098197", "uid-1", "smith@datum.net", "Smith", "Nelson", "https://a.example/s.png")
	u.SetLabels(map[string]string{"big": "label"})
	u.SetAnnotations(map[string]string{"kubectl.kubernetes.io/last-applied-configuration": "{...}"})
	u.SetManagedFields([]metav1.ManagedFieldsEntry{{Manager: "kubectl"}})
	u.SetFinalizers([]string{"iam.miloapis.com/user"})
	_ = unstructured.SetNestedField(u.Object, "Approved", "status", "registrationApproval")

	out, err := trimUser(u)
	if err != nil {
		t.Fatalf("trimUser: %v", err)
	}
	cu, ok := out.(*cachedUser)
	if !ok {
		t.Fatalf("trimUser returned %T, want *cachedUser", out)
	}

	want := cachedUser{
		ObjectMeta: metav1.ObjectMeta{Name: "340583683847098197", UID: "uid-1", ResourceVersion: "1"},
		GivenName:  "Smith",
		FamilyName: "Nelson",
		AvatarURL:  "https://a.example/s.png",
	}
	if cu.Name != want.Name || cu.UID != want.UID || cu.ResourceVersion != want.ResourceVersion ||
		cu.GivenName != want.GivenName || cu.FamilyName != want.FamilyName ||
		cu.AvatarURL != want.AvatarURL {
		t.Fatalf("trimmed = %+v, want %+v", *cu, want)
	}
	if cu.Labels != nil || cu.Annotations != nil || cu.ManagedFields != nil || cu.Finalizers != nil {
		t.Fatalf("trimmed user retained metadata it should drop: %+v", cu.ObjectMeta)
	}

	// The transform must be idempotent.
	again, err := trimUser(cu)
	if err != nil || again != cu {
		t.Fatalf("trimUser on a trimmed user = (%v, %v), want same pointer", again, err)
	}
}

func TestUserIndex_LookupByNameAndUID(t *testing.T) {
	client := newFakeUserClient(
		newTestUser("340583683847098197", "uid-1", "Smith@Datum.net", "Smith", "Nelson", "https://a.example/s.png"),
		newTestUser("340583683847098198", "uid-2", "dean@datum.net", "Dean", "Gaghan", ""),
	)
	idx := startTestIndex(t, client)
	if err := idx.WaitForSync(context.Background(), 5*time.Second); err != nil {
		t.Fatalf("index did not sync: %v", err)
	}

	want := processor.UserInfo{
		Name: "340583683847098197", UID: "uid-1", Email: "Smith@Datum.net",
		GivenName: "Smith", FamilyName: "Nelson", AvatarURL: "https://a.example/s.png",
	}

	if got, ok := idx.LookupByName("340583683847098197"); !ok || got != want {
		t.Errorf("LookupByName = (%+v, %v), want (%+v, true)", got, ok, want)
	}
	if got, ok := idx.LookupByUID("uid-1"); !ok || got != want {
		t.Errorf("LookupByUID = (%+v, %v), want (%+v, true)", got, ok, want)
	}

	if _, ok := idx.LookupByName("missing"); ok {
		t.Error("LookupByName(missing) hit")
	}
	if _, ok := idx.LookupByUID(""); ok {
		t.Error("LookupByUID(empty) hit")
	}
}

func TestUserIndex_ReflectsUpdatesAndDeletes(t *testing.T) {
	client := newFakeUserClient(
		newTestUser("u1", "uid-1", "smith@datum.net", "Smith", "Nelson", ""),
	)
	idx := startTestIndex(t, client)
	if err := idx.WaitForSync(context.Background(), 5*time.Second); err != nil {
		t.Fatalf("index did not sync: %v", err)
	}
	users := client.Resource(userGVR)
	ctx := context.Background()

	// Add
	if _, err := users.Create(ctx, newTestUser("u2", "uid-2", "dean@datum.net", "Dean", "Gaghan", ""), metav1.CreateOptions{}); err != nil {
		t.Fatalf("create: %v", err)
	}
	eventually(t, "added user indexed", func() bool {
		_, ok := idx.LookupByUID("uid-2")
		return ok
	})

	// Update: names and avatar change.
	updated := newTestUser("u1", "uid-1", "smith.nelson@datum.net", "Smithy", "Nelson", "https://a.example/new.png")
	updated.SetResourceVersion("2")
	if _, err := users.Update(ctx, updated, metav1.UpdateOptions{}); err != nil {
		t.Fatalf("update: %v", err)
	}
	eventually(t, "updated user reflected", func() bool {
		got, ok := idx.LookupByName("u1")
		return ok && got.DisplayName() == "Smithy Nelson" && got.AvatarURL == "https://a.example/new.png"
	})
	if got, ok := idx.LookupByUID("uid-1"); !ok || got.GivenName != "Smithy" {
		t.Errorf("LookupByUID after update = (%+v, %v)", got, ok)
	}

	// Delete
	if err := users.Delete(ctx, "u1", metav1.DeleteOptions{}); err != nil {
		t.Fatalf("delete: %v", err)
	}
	eventually(t, "deleted user removed", func() bool {
		_, byName := idx.LookupByName("u1")
		_, byUID := idx.LookupByUID("uid-1")
		return !byName && !byUID
	})
}

func TestUserIndex_NotSyncedMissesAndIsNoopForEnrichment(t *testing.T) {
	client := newFakeUserClient(
		newTestUser("u1", "uid-1", "smith@datum.net", "Smith", "Nelson", ""),
	)
	// RBAC denies list: the informer can never sync.
	client.PrependReactor("list", "users", func(k8stesting.Action) (bool, runtime.Object, error) {
		return true, nil, apierrors.NewForbidden(userGVR.GroupResource(), "", nil)
	})
	idx := newTestIndex(t, client)
	idx.fatalGrace = 300 * time.Millisecond
	startIndex(t, idx)

	// A persistent forbidden is reported once it outlasts the grace period
	// across two attempts, well before the overall timeout.
	start := time.Now()
	err := idx.WaitForSync(context.Background(), 30*time.Second)
	if !errors.Is(err, ErrUserIndexUnavailable) {
		t.Fatalf("WaitForSync error = %v, want ErrUserIndexUnavailable", err)
	}
	waited := time.Since(start)
	if waited < idx.fatalGrace {
		t.Errorf("WaitForSync failed after %s, before the %s grace period", waited, idx.fatalGrace)
	}
	if waited > 10*time.Second {
		t.Errorf("WaitForSync took %s on a persistently forbidden list", waited)
	}
	if idx.Healthy() {
		t.Error("Healthy() = true after a forbidden list")
	}
	if idx.Synced() {
		t.Fatal("Synced() = true, want false")
	}
	if _, ok := idx.LookupByName("u1"); ok {
		t.Error("LookupByName hit before sync")
	}
	if _, ok := idx.LookupByUID("uid-1"); ok {
		t.Error("LookupByUID hit before sync")
	}

	// Enrichment through the index is a no-op until it syncs.
	actor := processor.ResolveActorWithResolver(userInfoFixtureFor("smith@datum.net", "u1"), idx)
	if actor.DisplayName != "" {
		t.Errorf("DisplayName = %q, want empty while unsynced", actor.DisplayName)
	}

	// The forbidden error is recorded once, so it is logged once.
	eventually(t, "forbidden error recorded", func() bool {
		_, ok := idx.loggedErrors.Load("forbidden")
		return ok
	})
}

func TestUserIndex_EnrichesActivitiesEndToEnd(t *testing.T) {
	client := newFakeUserClient(
		newTestUser("340583683847098197", "uid-1", "smith@datum.net", "Smith", "Nelson", "https://a.example/s.png"),
	)
	idx := startTestIndex(t, client)
	if err := idx.WaitForSync(context.Background(), 5*time.Second); err != nil {
		t.Fatalf("index did not sync: %v", err)
	}

	actor := processor.ResolveActorWithResolver(userInfoFixtureFor("smith@datum.net", "340583683847098197"), idx)
	if actor.DisplayName != "Smith Nelson" || actor.AvatarURL != "https://a.example/s.png" {
		t.Fatalf("actor = %+v, want display name and avatar", actor)
	}
}

func TestUserIndex_TimeoutOnTransientErrors(t *testing.T) {
	client := newFakeUserClient()
	client.PrependReactor("list", "users", func(k8stesting.Action) (bool, runtime.Object, error) {
		return true, nil, apierrors.NewInternalError(errors.New("etcd unavailable"))
	})
	idx := startTestIndex(t, client)

	err := idx.WaitForSync(context.Background(), 300*time.Millisecond)
	if !errors.Is(err, ErrUserIndexSyncTimeout) {
		t.Fatalf("WaitForSync error = %v, want ErrUserIndexSyncTimeout", err)
	}
}

func TestUserIndex_RecoversAfterAccessIsGranted(t *testing.T) {
	client := newFakeUserClient(
		newTestUser("u1", "uid-1", "smith@datum.net", "Smith", "Nelson", ""),
	)
	var forbidden atomic.Bool
	forbidden.Store(true)
	client.PrependReactor("list", "users", func(k8stesting.Action) (bool, runtime.Object, error) {
		if forbidden.Load() {
			return true, nil, apierrors.NewForbidden(userGVR.GroupResource(), "", nil)
		}
		return false, nil, nil
	})
	idx := newTestIndex(t, client)
	idx.fatalGrace = 200 * time.Millisecond
	startIndex(t, idx)
	if err := idx.WaitForSync(context.Background(), 10*time.Second); !errors.Is(err, ErrUserIndexUnavailable) {
		t.Fatalf("WaitForSync error = %v, want ErrUserIndexUnavailable", err)
	}

	forbidden.Store(false)
	deadline := time.Now().Add(15 * time.Second)
	for !idx.Synced() && time.Now().Before(deadline) {
		time.Sleep(50 * time.Millisecond)
	}
	if !idx.Synced() || !idx.Healthy() {
		t.Fatalf("index did not recover: synced=%v healthy=%v", idx.Synced(), idx.Healthy())
	}
	if _, ok := idx.loggedErrors.Load("forbidden"); ok {
		t.Error("loggedErrors not reset after a successful list")
	}
	if _, ok := idx.LookupByName("u1"); !ok {
		t.Error("LookupByName missed after recovery")
	}
}

func TestUserIndex_OutageAfterSyncKeepsServingAndLogsAgainNextTime(t *testing.T) {
	client := newFakeUserClient(
		newTestUser("u1", "uid-1", "smith@datum.net", "Smith", "Nelson", ""),
	)
	idx := startTestIndex(t, client)
	if err := idx.WaitForSync(context.Background(), 5*time.Second); err != nil {
		t.Fatalf("index did not sync: %v", err)
	}
	if !idx.Healthy() {
		t.Fatal("Healthy() = false after a successful sync")
	}

	forbidden := apierrors.NewForbidden(userGVR.GroupResource(), "", nil)
	idx.handleWatchError(context.Background(), nil, forbidden)
	if idx.Healthy() {
		t.Error("Healthy() = true during an outage")
	}
	if _, ok := idx.LookupByName("u1"); !ok {
		t.Error("last-known index not served during an outage")
	}
	if _, ok := idx.loggedErrors.Load("forbidden"); !ok {
		t.Error("outage not recorded as logged")
	}

	// Routine watch expiry is not an outage.
	idx.onListSuccess()
	idx.handleWatchError(context.Background(), nil, apierrors.NewResourceExpired("too old"))
	if !idx.Healthy() {
		t.Error("watch expiry marked the index unhealthy")
	}

	// A successful relist resets the log suppression, so the next outage
	// logs at error level again.
	if _, ok := idx.loggedErrors.Load("forbidden"); ok {
		t.Error("loggedErrors not reset by a successful list")
	}
	if idx.fatalErr.Load() != nil || idx.fatalCount.Load() != 0 {
		t.Error("unavailable error not cleared by a successful list")
	}
}

// A single 401/403 (for example during credential rotation) must not make
// WaitForSync give up when the next attempt succeeds.
func TestUserIndex_TransientForbiddenDoesNotFail(t *testing.T) {
	client := newFakeUserClient(
		newTestUser("u1", "uid-1", "smith@datum.net", "Smith", "Nelson", ""),
	)
	var calls atomic.Int32
	client.PrependReactor("list", "users", func(k8stesting.Action) (bool, runtime.Object, error) {
		if calls.Add(1) == 1 {
			return true, nil, apierrors.NewUnauthorized("token expired")
		}
		return false, nil, nil
	})
	idx := newTestIndex(t, client)
	idx.fatalGrace = 0 // even with no grace period, one error is not enough
	startIndex(t, idx)

	if err := idx.WaitForSync(context.Background(), 15*time.Second); err != nil {
		t.Fatalf("WaitForSync error = %v, want nil after a transient 401", err)
	}
	if calls.Load() < 2 {
		t.Errorf("list called %d times, want a retry", calls.Load())
	}
}

func TestUserIndex_UnavailableNeedsTwoAttemptsAndTheGracePeriod(t *testing.T) {
	idx := newTestIndex(t, newFakeUserClient())
	forbidden := apierrors.NewForbidden(userGVR.GroupResource(), "", nil)

	idx.fatalGrace = 0
	idx.handleWatchError(context.Background(), nil, forbidden)
	if err := idx.unavailableErr(); err != nil {
		t.Fatalf("unavailable after one attempt: %v", err)
	}
	idx.handleWatchError(context.Background(), nil, forbidden)
	if err := idx.unavailableErr(); err == nil {
		t.Fatal("not unavailable after two attempts past the grace period")
	}

	// Two attempts inside the grace period are not enough either.
	idx.onListSuccess()
	idx.fatalGrace = time.Hour
	idx.handleWatchError(context.Background(), nil, forbidden)
	idx.handleWatchError(context.Background(), nil, forbidden)
	if err := idx.unavailableErr(); err != nil {
		t.Fatalf("unavailable inside the grace period: %v", err)
	}
}
