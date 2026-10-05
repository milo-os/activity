// Package nkeygenerator provisions per-edge-cluster NATS NKey credentials for
// the activity federated-events hub. It runs on a schedule in the hub
// cluster and, for every Karmada Cluster labelled
// activity.miloapis.com/nats-leaf=enabled:
//
//   - generates and stores a NATS NKey (seed + public key) in a Secret named
//     activity-leaf-nkey-<cluster> if it does not yet exist (a regenerated
//     seed would break that edge cluster until the next ESO refresh), and
//   - maintains the ESO PushSecret that mirrors that seed to GCP Secret
//     Manager, and
//   - rewrites the hub's authorized-leafs ConfigMap which the hub
//     HelmRelease consumes via valuesFrom so the per-cluster nkey users land
//     in the ACTIVITY account.
//
// The Karmada API is reached using a secretless kubeconfig whose tokenFile
// points at a projected ServiceAccount token.
package nkeygenerator

import (
	"context"
	"fmt"
	"strings"

	"github.com/nats-io/nkeys"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
	"k8s.io/klog/v2"
	"sigs.k8s.io/yaml"

	"go.miloapis.com/activity/internal/types"
)

const (
	clusterLabel      = "activity.miloapis.com/nats-leaf"
	clusterLabelValue = "enabled"
)

var (
	secretsGVR    = schema.GroupVersionResource{Group: "", Version: "v1", Resource: "secrets"}
	configMapsGVR = schema.GroupVersionResource{Group: "", Version: "v1", Resource: "configmaps"}
	clustersGVR   = schema.GroupVersionResource{
		Group: "cluster.karmada.io", Version: "v1alpha1", Resource: "clusters",
	}
	pushSecretsGVR = schema.GroupVersionResource{
		Group: "external-secrets.io", Version: "v1alpha1", Resource: "pushsecrets",
	}
)

// Config holds the nkey generator configuration.
type Config struct {
	Namespace         string
	KarmadaKubeconfig string
	SecretPrefix      string
	ConfigMapName     string
	SecretStoreName   string
	SecretStoreKind   string
	// SubjectPrefix is the NATS subject prefix the event exporter publishes
	// under; the per-cluster publish grants are derived from it.
	SubjectPrefix string
}

type generator struct {
	local kubernetes.Interface
	dyn   dynamic.Interface
	cfg   Config
}

// Run builds the Karmada and local Kubernetes clients from cfg and performs a
// single provisioning sync.
func Run(ctx context.Context, cfg Config) error {
	kc, err := clientcmd.BuildConfigFromFlags("", cfg.KarmadaKubeconfig)
	if err != nil {
		return fmt.Errorf("build karmada config from %s: %w", cfg.KarmadaKubeconfig, err)
	}
	karmadaDyn, err := dynamic.NewForConfig(kc)
	if err != nil {
		return fmt.Errorf("build karmada dynamic client: %w", err)
	}

	lc, err := rest.InClusterConfig()
	if err != nil {
		return fmt.Errorf("build local in-cluster config: %w", err)
	}
	localClientset, err := kubernetes.NewForConfig(lc)
	if err != nil {
		return fmt.Errorf("build local clientset: %w", err)
	}
	localDyn, err := dynamic.NewForConfig(lc)
	if err != nil {
		return fmt.Errorf("build local dynamic client: %w", err)
	}

	g := &generator{
		local: localClientset,
		dyn:   localDyn,
		cfg:   cfg,
	}

	return g.sync(ctx, karmadaDyn)
}

// sync lists the enabled Karmada clusters, provisions their keys, and rewrites
// the authorized-leafs ConfigMap. Any Karmada API failure aborts so a blip
// never silently truncates the published user list.
func (g *generator) sync(ctx context.Context, karmada dynamic.Interface) error {
	clusters, err := enabledClusters(ctx, karmada)
	if err != nil {
		return fmt.Errorf("list karmada clusters: %w", err)
	}

	users := staticUsers()
	for _, name := range clusters {
		if !types.ValidClusterName(name) {
			klog.InfoS("skipping cluster with invalid name for NATS subject use", "cluster", name)
			continue
		}
		pub, err := g.ensureKey(ctx, name)
		if err != nil {
			return err
		}
		users = append(users, nkeyUser(g.cfg.SubjectPrefix, name, pub))
	}

	if len(users) == 0 {
		return fmt.Errorf("refusing to write an empty authorized-leafs users list")
	}

	if err := g.writeConfigMap(ctx, users); err != nil {
		return err
	}

	klog.InfoS("generator sync complete", "clusters", clusters)
	return nil
}

// NATS allowed_connection_types values. Pinning a user to one listener is what
// stops its name being replayed on another; see staticUsers.
const (
	connTypeStandard = "STANDARD"
	connTypeLeafnode = "LEAFNODE"
)

// staticUsers returns the certificate-mapped ACTIVITY account users that
// never change with cluster membership. Names are the mapped certificate
// subjects (verify_and_map), not passwords.
//
// Both are pinned to STANDARD because they are passwordless: the leafnode
// listener skips certificate-subject mapping and falls back to a username
// lookup plus an empty-vs-empty password compare that succeeds, so without
// the pin an anonymous leaf quoting either name lands in the ACTIVITY
// account. Giving them passwords is not the fix -- the certificate-mapped
// client path still compares a password the real clients never send.
func staticUsers() []map[string]any {
	return []map[string]any{
		userEntry(
			map[string]string{"user": "CN=nack.nats.client"},
			[]string{connTypeStandard},
			[]string{">"}, []string{">"},
		),
		// $JS.ACK.> is where a pull consumer publishes its acks; without it
		// the sink pulls but never acks and the stream only grows. The default
		// _INBOX prefix is the processor's alone -- leaf clusters get their own
		// per-cluster prefixes from nkeyUser.
		userEntry(
			map[string]string{"user": "CN=activity-processor-nats-client"},
			[]string{connTypeStandard},
			[]string{"$JS.API.>", "$JS.ACK.>"}, []string{"_INBOX.>"},
		),
	}
}

// userEntry builds one NATS account user entry. allowed_connection_types is a
// sibling of user/nkey, not a permissions field -- nest it there and the server
// silently ignores it, restoring the anonymous-leaf bypass on staticUsers.
func userEntry(identity map[string]string, connTypes, publish, subscribe []string) map[string]any {
	m := map[string]any{
		"allowed_connection_types": toAny(connTypes),
		"permissions": map[string]any{
			"publish":   map[string]any{"allow": toAny(publish)},
			"subscribe": map[string]any{"allow": toAny(subscribe)},
		},
	}
	for k, v := range identity {
		m[k] = v
	}
	return m
}

func toAny(ss []string) []any {
	out := make([]any, len(ss))
	for i, s := range ss {
		out[i] = s
	}
	return out
}

// enabledClusters returns the names of Karmada Clusters labelled
// activity.miloapis.com/nats-leaf=enabled.
func enabledClusters(ctx context.Context, karmada dynamic.Interface) ([]string, error) {
	list, err := karmada.Resource(clustersGVR).List(ctx, metav1.ListOptions{
		LabelSelector: clusterLabel + "=" + clusterLabelValue,
	})
	if err != nil {
		return nil, err
	}
	names := make([]string, 0, len(list.Items))
	for _, c := range list.Items {
		names = append(names, c.GetName())
	}
	return names, nil
}

// storedPublicKey returns the NKey public key held by the per-cluster Secret.
// Must read through the typed client: the dynamic one hands back data still
// base64-encoded.
func (g *generator) storedPublicKey(ctx context.Context, secretName string) (string, error) {
	secret, err := g.local.CoreV1().Secrets(g.cfg.Namespace).Get(ctx, secretName, metav1.GetOptions{})
	if err != nil {
		return "", fmt.Errorf("get secret %s: %w", secretName, err)
	}
	pub := string(secret.Data["public"])
	if pub == "" {
		pub = string(secret.Data["nkey"])
	}
	if pub == "" {
		return "", fmt.Errorf("secret %s exists but has no public key", secretName)
	}
	return pub, nil
}

// ensureKey ensures the per-cluster NKey Secret and its ESO PushSecret exist
// and returns the NKey public key. A seed is never regenerated once stored.
func (g *generator) ensureKey(ctx context.Context, cluster string) (string, error) {
	secretName := g.cfg.SecretPrefix + "-" + cluster

	pub, err := g.storedPublicKey(ctx, secretName)
	if err == nil {
		if err := g.ensurePushSecret(ctx, cluster, secretName); err != nil {
			return "", err
		}
		return pub, nil
	}
	if !apierrors.IsNotFound(err) {
		return "", err
	}

	kp, err := generateNKey()
	if err != nil {
		return "", fmt.Errorf("generate nkey for %s: %w", cluster, err)
	}

	seedObj := &unstructured.Unstructured{Object: map[string]any{
		"apiVersion": "v1",
		"kind":       "Secret",
		"metadata": map[string]any{
			"name":      secretName,
			"namespace": g.cfg.Namespace,
			"labels": map[string]any{
				clusterLabel:                    clusterLabelValue,
				"activity.miloapis.com/cluster": cluster,
			},
		},
		"type": "Opaque",
		"stringData": map[string]any{
			"seed":   string(kp.Seed),
			"public": kp.Public,
			"nkey":   kp.Public,
		},
	}}

	res := g.dyn.Resource(secretsGVR).Namespace(g.cfg.Namespace)
	if _, err := res.Create(ctx, seedObj, metav1.CreateOptions{}); err != nil {
		if !apierrors.IsAlreadyExists(err) {
			return "", fmt.Errorf("create secret %s: %w", secretName, err)
		}
		// A racing writer stored a different seed. Authorizing the key just
		// generated here would authorize one no edge cell holds.
		if pub, err = g.storedPublicKey(ctx, secretName); err != nil {
			return "", err
		}
	} else {
		pub = kp.Public
		klog.InfoS("provisioned nkey", "cluster", cluster, "secret", secretName)
	}

	if err := g.ensurePushSecret(ctx, cluster, secretName); err != nil {
		return "", err
	}
	return pub, nil
}

// ensurePushSecret upserts the ESO PushSecret that mirrors the per-cluster
// seed Secret to GCP Secret Manager under the same name. The generator never
// talks to GCP directly; IAM stays with ESO.
func (g *generator) ensurePushSecret(ctx context.Context, cluster, secretName string) error {
	obj := &unstructured.Unstructured{Object: map[string]any{
		"apiVersion": "external-secrets.io/v1alpha1",
		"kind":       "PushSecret",
		"metadata": map[string]any{
			"name":      secretName,
			"namespace": g.cfg.Namespace,
		},
		"spec": map[string]any{
			"updatePolicy":    "Replace",
			"deletionPolicy":  "None",
			"refreshInterval": "1h",
			"secretStoreRefs": []any{
				map[string]any{"name": g.cfg.SecretStoreName, "kind": g.cfg.SecretStoreKind},
			},
			"selector": map[string]any{
				"secret": map[string]any{"name": secretName},
			},
			"data": []any{
				map[string]any{
					"match": map[string]any{
						"remoteRef": map[string]any{"remoteKey": secretName},
					},
				},
			},
		},
	}}

	res := g.dyn.Resource(pushSecretsGVR).Namespace(g.cfg.Namespace)
	if existing, err := res.Get(ctx, secretName, metav1.GetOptions{}); err == nil {
		obj.SetResourceVersion(existing.GetResourceVersion())
		_, err := res.Update(ctx, obj, metav1.UpdateOptions{})
		return err
	} else if !apierrors.IsNotFound(err) {
		return fmt.Errorf("get pushsecret %s: %w", secretName, err)
	}
	_, err := res.Create(ctx, obj, metav1.CreateOptions{})
	return err
}

// nkeyUser builds the narrow per-cluster leaf NKey user block. Never widen
// these to ">" or grant $JS.API.* -- JetStream API access lets a holder create
// a consumer over another cluster's subjects and read it back via its reply
// inbox, bypassing the publish scoping entirely.
//
// The subscribe grant is per-cluster for the same reason: "_INBOX.>" is not a
// per-client namespace but matches every reply inbox in the account, including
// the pull deliveries carrying the whole federated stream to the processor. The
// grant cannot simply be dropped -- the exporter needs PubAcks on a reply
// subject -- so each cluster gets its own prefix, which the exporter sets from
// the shared types.EventInboxPrefix via nats.CustomInboxPrefix.
//
// Cluster is interpolated into subjects, so callers must have run it through
// types.ValidClusterName first.
func nkeyUser(subjectPrefix, cluster, publicKey string) map[string]any {
	return userEntry(
		map[string]string{"nkey": publicKey},
		// These keys are only ever presented by an edge relay leaf-connecting
		// in, so they have no business authenticating on any other listener.
		[]string{connTypeLeafnode},
		[]string{
			types.EventSubjectGrant(subjectPrefix, cluster),
		},
		[]string{types.EventInboxGrant(cluster)},
	)
}

// droppedNkeys returns clusters present in prev but absent from next, skipping
// any whose per-cluster Secret is already deleted. That deletion is the
// operator's signal of an intentional decommission, and the only way to clear
// a drop: prev is read back from the very ConfigMap this function guards, so
// otherwise a decommissioned cluster would be reported forever.
func (g *generator) droppedNkeys(ctx context.Context, prev, next []map[string]any) ([]string, error) {
	have := make(map[string]bool, len(next))
	for _, u := range next {
		if pub, ok := u["nkey"].(string); ok {
			have[pub] = true
		}
	}
	var dropped []string
	for _, u := range prev {
		pub, ok := u["nkey"].(string)
		if !ok || have[pub] {
			continue
		}
		cluster := clusterFromUser(u, g.cfg.SubjectPrefix, pub)
		secretName := g.cfg.SecretPrefix + "-" + cluster
		_, err := g.local.CoreV1().Secrets(g.cfg.Namespace).Get(ctx, secretName, metav1.GetOptions{})
		if err == nil {
			dropped = append(dropped, cluster)
		} else if !apierrors.IsNotFound(err) {
			return nil, fmt.Errorf("checking secret %s for dropped cluster %s: %w", secretName, cluster, err)
		}
	}
	return dropped, nil
}

// clusterFromUser recovers the cluster name nkeyUser embeds in the publish
// subject (<subjectPrefix>.<cluster>.*). Falls back to the public key if that
// shape ever changes; callers only use this for display.
func clusterFromUser(u map[string]any, subjectPrefix, fallback string) string {
	perms, ok := u["permissions"].(map[string]any)
	if !ok {
		return fallback
	}
	publish, ok := perms["publish"].(map[string]any)
	if !ok {
		return fallback
	}
	allow, ok := publish["allow"].([]any)
	if !ok {
		return fallback
	}
	for _, s := range allow {
		subj, ok := s.(string)
		if !ok {
			continue
		}
		rest, ok := strings.CutPrefix(subj, subjectPrefix+".")
		if !ok {
			continue
		}
		if cluster, ok := strings.CutSuffix(rest, ".*"); ok {
			return cluster
		}
	}
	return fallback
}

// previousUsers reads the currently-applied ConfigMap's accounts.ACTIVITY.users
// list. A missing ConfigMap or unparseable values.yaml is treated as "no
// previous users" -- nothing to protect against shrinking yet. Any other read
// failure propagates: an empty prev disables the very drop guard that exists to
// survive a transient API error.
func (g *generator) previousUsers(ctx context.Context, name string) ([]map[string]any, error) {
	res := g.dyn.Resource(configMapsGVR).Namespace(g.cfg.Namespace)
	obj, err := res.Get(ctx, name, metav1.GetOptions{})
	if apierrors.IsNotFound(err) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("get configmap %s: %w", name, err)
	}
	raw, found, err := unstructured.NestedString(obj.Object, "data", "values.yaml")
	if err != nil || !found {
		return nil, nil
	}
	var doc struct {
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
	if err := yaml.Unmarshal([]byte(raw), &doc); err != nil {
		return nil, nil
	}
	return doc.Config.Merge.Accounts.Activity.Users, nil
}

// writeConfigMap rewrites the authorized-leafs ConfigMap the hub HelmRelease
// reads via valuesFrom; values.yaml carries the whole ACTIVITY account block,
// so this is the single source of truth for account users. It refuses to drop
// a previously-authorized leaf nkey: a transient Karmada listing glitch must
// not deauthorize a live cluster unnoticed.
func (g *generator) writeConfigMap(ctx context.Context, users []map[string]any) error {
	prev, err := g.previousUsers(ctx, g.cfg.ConfigMapName)
	if err != nil {
		return err
	}
	dropped, err := g.droppedNkeys(ctx, prev, users)
	if err != nil {
		return err
	}
	if len(dropped) > 0 {
		return fmt.Errorf("refusing to write authorized-leafs: cluster(s) %v would be dropped but their nkey Secret %s-<cluster> still exists (delete it once the cluster decommission is confirmed, then retry)", dropped, g.cfg.SecretPrefix)
	}

	valuesDoc := map[string]any{
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

	valuesBytes, err := yaml.Marshal(valuesDoc)
	if err != nil {
		return fmt.Errorf("marshal values.yaml: %w", err)
	}

	cm := &unstructured.Unstructured{Object: map[string]any{
		"apiVersion": "v1",
		"kind":       "ConfigMap",
		"metadata": map[string]any{
			"name":      g.cfg.ConfigMapName,
			"namespace": g.cfg.Namespace,
			"labels": map[string]any{
				"reconcile.fluxcd.io/watch": "Enabled",
			},
		},
		"data": map[string]any{
			"values.yaml": string(valuesBytes),
		},
	}}

	res := g.dyn.Resource(configMapsGVR).Namespace(g.cfg.Namespace)
	if existing, err := res.Get(ctx, g.cfg.ConfigMapName, metav1.GetOptions{}); err == nil {
		cm.SetResourceVersion(existing.GetResourceVersion())
		_, err := res.Update(ctx, cm, metav1.UpdateOptions{})
		return err
	} else if !apierrors.IsNotFound(err) {
		return fmt.Errorf("get configmap %s: %w", g.cfg.ConfigMapName, err)
	}
	_, err = res.Create(ctx, cm, metav1.CreateOptions{})
	return err
}

type nkeyPair struct {
	Seed   []byte
	Public string
}

// generateNKey creates a new user-scoped NATS NKey. The returned seed is an
// independent copy; the temporary KeyPair is wiped on return.
func generateNKey() (nkeyPair, error) {
	kp, err := nkeys.CreateUser()
	if err != nil {
		return nkeyPair{}, err
	}
	defer kp.Wipe()

	seed, err := kp.Seed()
	if err != nil {
		return nkeyPair{}, err
	}
	pub, err := kp.PublicKey()
	if err != nil {
		return nkeyPair{}, err
	}
	return nkeyPair{Seed: append([]byte(nil), seed...), Public: pub}, nil
}
