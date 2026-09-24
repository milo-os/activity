package nkeygenerator

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/nats-io/nkeys"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	dynamicfake "k8s.io/client-go/dynamic/fake"
	k8sfake "k8s.io/client-go/kubernetes/fake"
	k8stesting "k8s.io/client-go/testing"
	"sigs.k8s.io/yaml"
)

func TestGenerateNKey(t *testing.T) {
	kp, err := generateNKey()
	if err != nil {
		t.Fatalf("generateNKey: %v", err)
	}

	// Seed must be a valid base32-encoded NKey user seed ("SU" prefix).
	if !strings.HasPrefix(string(kp.Seed), "SU") {
		t.Fatalf("seed does not start with SU prefix: %q", kp.Seed)
	}

	decoded, err := nkeys.FromSeed(kp.Seed)
	if err != nil {
		t.Fatalf("FromSeed: %v", err)
	}
	defer decoded.Wipe()

	pubFromSeed, err := decoded.PublicKey()
	if err != nil {
		t.Fatalf("PublicKey: %v", err)
	}
	if pubFromSeed != kp.Public {
		t.Fatalf("public key mismatch: fromSeed=%q pair=%q", pubFromSeed, kp.Public)
	}
}

func TestNkeyUserAllowlist(t *testing.T) {
	u := nkeyUser("activity.federated", "us-east-1", "UD5ULF5HDXSAB42CKHYDGDQ5E53AJNSXYW72MEYMHNVGAQKNRZS55H5N")

	if u["nkey"] != "UD5ULF5HDXSAB42CKHYDGDQ5E53AJNSXYW72MEYMHNVGAQKNRZS55H5N" {
		t.Fatalf("nkey not set")
	}
	if _, hasUser := u["user"]; hasUser {
		t.Fatalf("nkey user must not carry a user field")
	}

	publish := mustStrSlice(t, u, "publish")
	expected := []string{
		"activity.federated.us-east-1.*",
	}
	if len(publish) != len(expected) {
		t.Fatalf("publish allow len=%d want %d: %v", len(publish), len(expected), publish)
	}
	for i, e := range expected {
		if publish[i] != e {
			t.Fatalf("publish[%d]=%q want %q", i, publish[i], e)
		}
	}

	// A leaf must never get JetStream API access; see nkeyUser.
	for _, allow := range publish {
		if allow == ">" ||
			strings.HasPrefix(allow, "activity.federated.*") ||
			strings.HasPrefix(allow, "$JS.") {
			t.Fatalf("leaf allowlist widened beyond its own cluster: %q", allow)
		}
	}

	// A leaf must never hold the shared "_INBOX.>".
	sub := mustStrSlice(t, u, "subscribe")
	if len(sub) != 1 || sub[0] != "_INBOX_us-east-1.>" {
		t.Fatalf("subscribe allow = %v, want [_INBOX_us-east-1.>]", sub)
	}

	// These keys are only ever presented by an edge relay leaf-connecting in.
	if got := mustConnTypes(t, u); len(got) != 1 || got[0] != "LEAFNODE" {
		t.Fatalf("allowed_connection_types = %v, want [LEAFNODE]", got)
	}
}

// TestNkeyUserInboxIsolation pins the per-cluster subscribe grant: one
// cluster's leaf must not be able to subscribe to another's reply inboxes,
// nor to the default _INBOX space the processor's pull deliveries use.
func TestNkeyUserInboxIsolation(t *testing.T) {
	const pubA = "UD5ULF5HDXSAB42CKHYDGDQ5E53AJNSXYW72MEYMHNVGAQKNRZS55H5N"
	const pubB = "UAAAA5HDXSAB42CKHYDGDQ5E53AJNSXYW72MEYMHNVGAQKNRZS55H5N"

	a := mustStrSlice(t, nkeyUser("activity.federated", "us-east-1", pubA), "subscribe")
	b := mustStrSlice(t, nkeyUser("activity.federated", "us-west-1", pubB), "subscribe")

	if len(a) != 1 || len(b) != 1 || a[0] == b[0] {
		t.Fatalf("clusters share a subscribe grant: %v vs %v", a, b)
	}
	// Disjoint as subject token sequences, not merely as unequal strings.
	for _, reply := range []string{
		"_INBOX_us-west-1.pNXQ4o0qLcJ4nT8T",
		"_INBOX_us-west-1.pNXQ4o0qLcJ4nT8T.xY7",
	} {
		if subjectMatches(a[0], reply) {
			t.Fatalf("grant %q matches another cluster's reply %q", a[0], reply)
		}
		if !subjectMatches(b[0], reply) {
			t.Fatalf("grant %q does not cover its own reply %q", b[0], reply)
		}
		if subjectMatches("_INBOX.>", reply) {
			t.Fatalf("processor's _INBOX.> grant still reaches %q", reply)
		}
	}
	if subjectMatches(a[0], "_INBOX.pNXQ4o0qLcJ4nT8T.1") {
		t.Fatalf("grant %q reaches the default _INBOX space", a[0])
	}
}

// subjectMatches reports whether a NATS subject filter matches subject,
// honouring the "*" single-token and ">" tail wildcards.
func subjectMatches(filter, subject string) bool {
	f := strings.Split(filter, ".")
	s := strings.Split(subject, ".")
	for i, tok := range f {
		if tok == ">" {
			return i < len(s)
		}
		if i >= len(s) {
			return false
		}
		if tok != "*" && tok != s[i] {
			return false
		}
	}
	return len(f) == len(s)
}

// TestNkeyUserSubjectPrefix is the drift guard: the grant and the parse that
// recovers a cluster from it must both follow the configured prefix, whatever
// its token count, rather than assuming the "activity.federated" default.
func TestNkeyUserSubjectPrefix(t *testing.T) {
	const pub = "UD5ULF5HDXSAB42CKHYDGDQ5E53AJNSXYW72MEYMHNVGAQKNRZS55H5N"

	for _, prefix := range []string{"evt", "a.b.c"} {
		u := nkeyUser(prefix, "us-east-1", pub)

		publish := mustStrSlice(t, u, "publish")
		want := prefix + ".us-east-1.*"
		if len(publish) != 1 || publish[0] != want {
			t.Fatalf("publish allow = %v, want [%s]", publish, want)
		}

		if got := clusterFromUser(u, prefix, "FALLBACK"); got != "us-east-1" {
			t.Fatalf("clusterFromUser(prefix=%q) = %q, want us-east-1", prefix, got)
		}
		if got := clusterFromUser(u, "activity.federated", "FALLBACK"); got != "FALLBACK" {
			t.Fatalf("clusterFromUser parsed a %q grant with the default prefix: %q", prefix, got)
		}
	}
}

func TestDroppedNkeysDetection(t *testing.T) {
	prev := []map[string]any{
		nkeyUser("activity.federated", "us-east-1", "UD5ULF5HDXSAB42CKHYDGDQ5E53AJNSXYW72MEYMHNVGAQKNRZS55H5N"),
		nkeyUser("activity.federated", "us-west-1", "UAAAA5HDXSAB42CKHYDGDQ5E53AJNSXYW72MEYMHNVGAQKNRZS55H5N"),
	}
	next := []map[string]any{
		nkeyUser("activity.federated", "us-east-1", "UD5ULF5HDXSAB42CKHYDGDQ5E53AJNSXYW72MEYMHNVGAQKNRZS55H5N"),
	}
	cfg := Config{Namespace: "activity-system", SecretPrefix: "activity-leaf-nkey", SubjectPrefix: "activity.federated"}

	g := &generator{cfg: cfg, local: k8sfake.NewSimpleClientset(&corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{Name: "activity-leaf-nkey-us-west-1", Namespace: "activity-system"},
	})}
	dropped, err := g.droppedNkeys(context.Background(), prev, next)
	if err != nil {
		t.Fatalf("droppedNkeys: %v", err)
	}
	if len(dropped) != 1 || dropped[0] != "us-west-1" {
		t.Fatalf("droppedNkeys = %v, want [us-west-1] (its Secret still exists)", dropped)
	}

	// Deleting the Secret is the operator's decommission signal, and the only
	// way to clear a drop.
	g2 := &generator{cfg: cfg, local: k8sfake.NewSimpleClientset()}
	dropped2, err := g2.droppedNkeys(context.Background(), prev, next)
	if err != nil {
		t.Fatalf("droppedNkeys: %v", err)
	}
	if len(dropped2) != 0 {
		t.Fatalf("droppedNkeys = %v, want none once the Secret is gone", dropped2)
	}

	// A rotated key counts as a drop of the old one, which is correct.
	dropped3, err := g.droppedNkeys(context.Background(), prev, prev)
	if err != nil {
		t.Fatalf("droppedNkeys: %v", err)
	}
	if len(dropped3) != 0 {
		t.Fatalf("identical sets must report no drops")
	}
}

func TestStaticUsers(t *testing.T) {
	users := staticUsers()
	if len(users) != 2 {
		t.Fatalf("expected 2 static users, got %d", len(users))
	}

	nack := users[0]
	if nack["user"] != "CN=nack.nats.client" {
		t.Fatalf("nack user = %v", nack["user"])
	}
	if got := mustStrSlice(t, nack, "publish"); len(got) != 1 || got[0] != ">" {
		t.Fatalf("nack publish = %v, want [>]", got)
	}

	// Passwordless certificate-subject identities must stay pinned to the
	// standard client port; see staticUsers.
	for _, u := range users {
		if _, hasPassword := u["password"]; hasPassword {
			t.Fatalf("static user %v carries a password, which breaks the mTLS client path", u["user"])
		}
		if got := mustConnTypes(t, u); len(got) != 1 || got[0] != "STANDARD" {
			t.Fatalf("static user %v allowed_connection_types = %v, want [STANDARD]", u["user"], got)
		}
	}

	processor := users[1]
	if processor["user"] != "CN=activity-processor-nats-client" {
		t.Fatalf("processor user = %v", processor["user"])
	}

	// Without $JS.ACK.> the processor pulls but never acks.
	processorPub := mustStrSlice(t, processor, "publish")
	var hasAck bool
	for _, allow := range processorPub {
		if allow == "$JS.ACK.>" {
			hasAck = true
		}
	}
	if !hasAck {
		t.Fatalf("processor publish = %v, want it to include $JS.ACK.>", processorPub)
	}

	processorSub := mustStrSlice(t, processor, "subscribe")
	expectedSub := []string{"_INBOX.>"}
	if len(processorSub) != len(expectedSub) {
		t.Fatalf("processor subscribe = %v, want %v", processorSub, expectedSub)
	}
	for i, e := range expectedSub {
		if processorSub[i] != e {
			t.Fatalf("processor subscribe[%d]=%q want %q", i, processorSub[i], e)
		}
	}
}

func TestRenderedValuesRoundTrip(t *testing.T) {
	users := append(staticUsers(), nkeyUser("activity.federated", "us-east-1", "UD5ULF5HDXSAB42CKHYDGDQ5E53AJNSXYW72MEYMHNVGAQKNRZS55H5N"))

	// Exercise the same marshal path writeConfigMap uses.
	doc := map[string]any{
		"config": map[string]any{
			"merge": map[string]any{
				"accounts": map[string]any{
					"ACTIVITY": map[string]any{
						"jetstream": "enabled",
						"users":     users,
					},
				},
			},
		},
	}
	out, err := yaml.Marshal(doc)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	for _, want := range []string{
		"ACTIVITY:",
		"jetstream: enabled",
		"nkey: UD5ULF5",
		"activity.federated.us-east-1.*",
		"_INBOX_us-east-1.>",
		"CN=nack.nats.client",
		"CN=activity-processor-nats-client",
	} {
		if !strings.Contains(string(out), want) {
			t.Fatalf("rendered values missing %q:\n%s", want, out)
		}
	}

	// Structure, not substrings: a Contains check cannot tell a sibling
	// allowed_connection_types from one silently nested under permissions.
	var round struct {
		Config struct {
			Merge struct {
				Accounts struct {
					Activity struct {
						Users []map[string]any `json:"users"`
					} `json:"ACTIVITY"`
				} `json:"accounts"`
			} `json:"merge"`
		} `json:"config"`
	}
	if err := yaml.Unmarshal(out, &round); err != nil {
		t.Fatalf("unmarshal rendered values: %v", err)
	}
	got := round.Config.Merge.Accounts.Activity.Users
	if len(got) != len(users) {
		t.Fatalf("round-tripped %d users, want %d", len(got), len(users))
	}
	for _, u := range got {
		if _, ok := u["allowed_connection_types"]; !ok {
			t.Fatalf("user %v lost allowed_connection_types at the top level: %v", u["user"], u)
		}
		perms, ok := u["permissions"].(map[string]any)
		if !ok {
			t.Fatalf("user %v has no permissions map", u["user"])
		}
		if _, nested := perms["allowed_connection_types"]; nested {
			t.Fatalf("user %v nests allowed_connection_types under permissions, where NATS ignores it", u["user"])
		}
	}
}

func mustConnTypes(t *testing.T, m map[string]any) []string {
	t.Helper()
	raw, ok := m["allowed_connection_types"].([]any)
	if !ok {
		t.Fatalf("no top-level allowed_connection_types on user entry: %v", m)
	}
	out := make([]string, 0, len(raw))
	for _, v := range raw {
		s, ok := v.(string)
		if !ok {
			t.Fatalf("allowed_connection_types item not a string")
		}
		out = append(out, s)
	}
	return out
}

func mustStrSlice(t *testing.T, m map[string]any, dir string) []string {
	t.Helper()
	perms, ok := m["permissions"].(map[string]any)
	if !ok {
		t.Fatalf("no permissions map")
	}
	d, ok := perms[dir].(map[string]any)
	if !ok {
		t.Fatalf("no %s map", dir)
	}
	allow, ok := d["allow"].([]any)
	if !ok {
		t.Fatalf("no allow slice")
	}
	out := make([]string, 0, len(allow))
	for _, v := range allow {
		s, ok := v.(string)
		if !ok {
			t.Fatalf("allow item not a string")
		}
		out = append(out, s)
	}
	return out
}

// updateResourceVersion returns the resourceVersion carried by the Update call
// fn triggers, or "" if none was issued. The fake ObjectTracker does not
// enforce resourceVersion, so the outgoing object is inspected directly rather
// than relying on the call succeeding or failing.
func updateResourceVersion(t *testing.T, gvr schema.GroupVersionResource, listKind string, existing *unstructured.Unstructured, fn func(dyn *dynamicfake.FakeDynamicClient) error) string {
	t.Helper()
	dyn := dynamicfake.NewSimpleDynamicClientWithCustomListKinds(runtime.NewScheme(), map[schema.GroupVersionResource]string{
		gvr: listKind,
	}, existing)

	var gotRV string
	dyn.PrependReactor("update", gvr.Resource, func(action k8stesting.Action) (bool, runtime.Object, error) {
		obj := action.(k8stesting.UpdateAction).GetObject().(*unstructured.Unstructured)
		gotRV = obj.GetResourceVersion()
		return false, nil, nil // let the fake tracker still handle it
	})

	if err := fn(dyn); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	return gotRV
}

// TestEnsurePushSecretUpdatesExisting guards against a regression where
// ensurePushSecret discarded its Get result and issued an Update with no
// resourceVersion, which a real API server rejects for every existing object.
func TestEnsurePushSecretUpdatesExisting(t *testing.T) {
	existing := &unstructured.Unstructured{Object: map[string]any{
		"apiVersion": "external-secrets.io/v1alpha1",
		"kind":       "PushSecret",
		"metadata": map[string]any{
			"name":            "activity-leaf-nkey-us-east-1",
			"namespace":       "activity-system",
			"resourceVersion": "42",
		},
	}}

	gotRV := updateResourceVersion(t, pushSecretsGVR, "PushSecretList", existing, func(dyn *dynamicfake.FakeDynamicClient) error {
		g := &generator{dyn: dyn, cfg: Config{
			Namespace:       "activity-system",
			SecretStoreName: "activity-secret-store",
			SecretStoreKind: "SecretStore",
		}}
		return g.ensurePushSecret(context.Background(), "us-east-1", "activity-leaf-nkey-us-east-1")
	})

	if gotRV != "42" {
		t.Fatalf("Update carried resourceVersion %q, want %q (the fetched object's)", gotRV, "42")
	}
}

// TestWriteConfigMapUpdatesExisting guards against the same missing-
// resourceVersion regression in writeConfigMap's update path.
func TestWriteConfigMapUpdatesExisting(t *testing.T) {
	existing := &unstructured.Unstructured{Object: map[string]any{
		"apiVersion": "v1",
		"kind":       "ConfigMap",
		"metadata": map[string]any{
			"name":            "nats-activity-authorized-leafs",
			"namespace":       "activity-system",
			"resourceVersion": "42",
		},
	}}

	users := append(staticUsers(), nkeyUser("activity.federated", "us-east-1", "UD5ULF5HDXSAB42CKHYDGDQ5E53AJNSXYW72MEYMHNVGAQKNRZS55H5N"))
	gotRV := updateResourceVersion(t, configMapsGVR, "ConfigMapList", existing, func(dyn *dynamicfake.FakeDynamicClient) error {
		g := &generator{dyn: dyn, cfg: Config{
			Namespace:     "activity-system",
			ConfigMapName: "nats-activity-authorized-leafs",
			SecretPrefix:  "activity-leaf-nkey",
			SubjectPrefix: "activity.federated",
		}}
		return g.writeConfigMap(context.Background(), users)
	})

	if gotRV != "42" {
		t.Fatalf("Update carried resourceVersion %q, want %q (the fetched object's)", gotRV, "42")
	}
}

// localWithLateSecret returns a typed fake whose first Get reports NotFound and
// whose later Gets return secret: the create-race shape ensureKey must handle.
func localWithLateSecret(secret *corev1.Secret) *k8sfake.Clientset {
	local := k8sfake.NewSimpleClientset(secret)
	var gets int
	local.PrependReactor("get", "secrets", func(k8stesting.Action) (bool, runtime.Object, error) {
		if gets++; gets == 1 {
			return true, nil, apierrors.NewNotFound(schema.GroupResource{Resource: "secrets"}, secret.Name)
		}
		return false, nil, nil
	})
	return local
}

// dynRejectingSecretCreate returns a dynamic fake whose Secret creates always
// lose to an existing object.
func dynRejectingSecretCreate(name string) *dynamicfake.FakeDynamicClient {
	dyn := dynamicfake.NewSimpleDynamicClientWithCustomListKinds(runtime.NewScheme(), map[schema.GroupVersionResource]string{
		secretsGVR:     "SecretList",
		pushSecretsGVR: "PushSecretList",
	})
	dyn.PrependReactor("create", "secrets", func(k8stesting.Action) (bool, runtime.Object, error) {
		return true, nil, apierrors.NewAlreadyExists(schema.GroupResource{Resource: "secrets"}, name)
	})
	return dyn
}

func testGenerator(local *k8sfake.Clientset, dyn *dynamicfake.FakeDynamicClient) *generator {
	return &generator{local: local, dyn: dyn, cfg: Config{
		Namespace:       "activity-system",
		SecretPrefix:    "activity-leaf-nkey",
		ConfigMapName:   "nats-activity-authorized-leafs",
		SecretStoreName: "activity-secret-store",
		SecretStoreKind: "SecretStore",
		SubjectPrefix:   "activity.federated",
	}}
}

// TestEnsureKeyAlreadyExistsReturnsStoredKey pins the create race: the key
// authorized must be the one the Secret already holds, never the one this run
// generated in memory -- no edge cell holds that seed.
func TestEnsureKeyAlreadyExistsReturnsStoredKey(t *testing.T) {
	const secretName = "activity-leaf-nkey-us-east-1"
	const stored = "USTORED5HDXSAB42CKHYDGDQ5E53AJNSXYW72MEYMHNVGAQKNRZS55"

	for _, tc := range []struct {
		name string
		data map[string][]byte
	}{
		{name: "public key", data: map[string][]byte{"public": []byte(stored)}},
		{name: "nkey fallback", data: map[string][]byte{"nkey": []byte(stored)}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			local := localWithLateSecret(&corev1.Secret{
				ObjectMeta: metav1.ObjectMeta{Name: secretName, Namespace: "activity-system"},
				Data:       tc.data,
			})
			g := testGenerator(local, dynRejectingSecretCreate(secretName))

			pub, err := g.ensureKey(context.Background(), "us-east-1")
			if err != nil {
				t.Fatalf("ensureKey: %v", err)
			}
			if pub != stored {
				t.Fatalf("ensureKey = %q, want the stored key %q", pub, stored)
			}
		})
	}
}

// TestEnsureKeyAlreadyExistsWithoutStoredKey: if the pre-existing Secret cannot
// be read back, or carries no public key, the sync must fail rather than
// authorize the generated key.
func TestEnsureKeyAlreadyExistsWithoutStoredKey(t *testing.T) {
	const secretName = "activity-leaf-nkey-us-east-1"

	for _, tc := range []struct {
		name  string
		local *k8sfake.Clientset
	}{
		{name: "re-get fails", local: k8sfake.NewSimpleClientset()},
		{name: "no public key", local: localWithLateSecret(&corev1.Secret{
			ObjectMeta: metav1.ObjectMeta{Name: secretName, Namespace: "activity-system"},
			Data:       map[string][]byte{"seed": []byte("SUSEED")},
		})},
	} {
		t.Run(tc.name, func(t *testing.T) {
			g := testGenerator(tc.local, dynRejectingSecretCreate(secretName))

			pub, err := g.ensureKey(context.Background(), "us-east-1")
			if err == nil {
				t.Fatalf("ensureKey returned %q, want an error", pub)
			}
			if pub != "" {
				t.Fatalf("ensureKey returned key %q on failure", pub)
			}
		})
	}
}

// TestWriteConfigMapAbortsOnPreviousUsersError: a transient failure reading the
// ConfigMap must abort the sync. Collapsing it into "no previous users" would
// disable the drop guard exactly when it is needed and write a truncated list.
func TestWriteConfigMapAbortsOnPreviousUsersError(t *testing.T) {
	const pubA = "UD5ULF5HDXSAB42CKHYDGDQ5E53AJNSXYW72MEYMHNVGAQKNRZS55H5N"
	const pubB = "UAAAA5HDXSAB42CKHYDGDQ5E53AJNSXYW72MEYMHNVGAQKNRZS55H5N"

	prevValues, err := yaml.Marshal(map[string]any{
		"config": map[string]any{"merge": map[string]any{"accounts": map[string]any{"ACTIVITY": map[string]any{
			"users": []map[string]any{
				nkeyUser("activity.federated", "us-east-1", pubA),
				nkeyUser("activity.federated", "us-west-1", pubB),
			},
		}}}},
	})
	if err != nil {
		t.Fatalf("marshal previous values: %v", err)
	}

	existing := &unstructured.Unstructured{Object: map[string]any{
		"apiVersion": "v1",
		"kind":       "ConfigMap",
		"metadata": map[string]any{
			"name":            "nats-activity-authorized-leafs",
			"namespace":       "activity-system",
			"resourceVersion": "42",
		},
		"data": map[string]any{"values.yaml": string(prevValues)},
	}}

	dyn := dynamicfake.NewSimpleDynamicClientWithCustomListKinds(runtime.NewScheme(), map[schema.GroupVersionResource]string{
		configMapsGVR: "ConfigMapList",
	}, existing)
	// Only the previousUsers read fails; writeConfigMap's own Get still works,
	// so a swallowed error would go on to write the truncated list.
	var gets int
	dyn.PrependReactor("get", "configmaps", func(k8stesting.Action) (bool, runtime.Object, error) {
		if gets++; gets == 1 {
			return true, nil, apierrors.NewInternalError(fmt.Errorf("etcd unavailable"))
		}
		return false, nil, nil
	})

	g := testGenerator(k8sfake.NewSimpleClientset(&corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{Name: "activity-leaf-nkey-us-west-1", Namespace: "activity-system"},
	}), dyn)

	users := append(staticUsers(), nkeyUser("activity.federated", "us-east-1", pubA))
	if err := g.writeConfigMap(context.Background(), users); err == nil {
		t.Fatalf("writeConfigMap succeeded despite an unreadable previous ConfigMap")
	}

	for _, action := range dyn.Actions() {
		if verb := action.GetVerb(); verb == "update" || verb == "create" {
			t.Fatalf("writeConfigMap issued a %s after failing to read the previous users", verb)
		}
	}
}

// TestPreviousUsersMissingConfigMap: a genuinely absent ConfigMap is the
// first-run case, not an error.
func TestPreviousUsersMissingConfigMap(t *testing.T) {
	dyn := dynamicfake.NewSimpleDynamicClientWithCustomListKinds(runtime.NewScheme(), map[schema.GroupVersionResource]string{
		configMapsGVR: "ConfigMapList",
	})
	g := testGenerator(k8sfake.NewSimpleClientset(), dyn)

	prev, err := g.previousUsers(context.Background(), "nats-activity-authorized-leafs")
	if err != nil {
		t.Fatalf("previousUsers on a missing ConfigMap: %v", err)
	}
	if prev != nil {
		t.Fatalf("previousUsers = %v, want nil", prev)
	}
}
