package edgeingest

import (
	"context"
	"errors"
	"path/filepath"
	"testing"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func syncedIndex() *NamespaceIndex {
	index := NewNamespaceIndex()
	index.AddSyncedFunc(func() bool { return true })
	return index
}

func TestNamespaceIndexColdCacheIsRetryable(t *testing.T) {
	index := NewNamespaceIndex()
	synced := false
	index.AddSyncedFunc(func() bool { return synced })

	_, err := index.ResolveUpstreamNamespace(context.Background(), downstreamNS)
	if !errors.Is(err, ErrCacheNotSynced) {
		t.Fatalf("cold cache returned %v, want ErrCacheNotSynced", err)
	}

	synced = true
	_, err = index.ResolveUpstreamNamespace(context.Background(), downstreamNS)
	if !errors.Is(err, ErrUpstreamNamespaceUnknown) {
		t.Fatalf("synced cache returned %v, want ErrUpstreamNamespaceUnknown", err)
	}
}

func TestNamespaceIndexWithNoSourcesIsNeverSynced(t *testing.T) {
	if NewNamespaceIndex().HasSynced() {
		t.Fatal("index with no registered sources reported synced")
	}
}

func TestNamespaceIndexRetainsDeletedNamespaces(t *testing.T) {
	index := syncedIndex()
	ref := UpstreamNamespaceRef{ClusterName: "acme-prod", Namespace: "default"}
	index.Upsert(downstreamNS, ref)

	got, err := index.ResolveUpstreamNamespace(context.Background(), downstreamNS)
	if err != nil {
		t.Fatalf("resolve returned %v", err)
	}
	if got != ref {
		t.Fatalf("resolve returned %+v, want %+v", got, ref)
	}

	// A deletion never reaches the index: there is no removal path at all, so
	// records about the namespace keep resolving for their retention window.
	if index.Len() != 1 {
		t.Fatalf("index holds %d entries", index.Len())
	}
}

func TestNamespaceIndexSurvivesRestart(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state", "namespaces.json")
	ref := UpstreamNamespaceRef{ClusterName: "acme-prod", Namespace: "default"}

	index := syncedIndex()
	index.Upsert(downstreamNS, ref)
	if err := index.Save(path); err != nil {
		t.Fatalf("save returned %v", err)
	}

	restarted := syncedIndex()
	if err := restarted.Load(path); err != nil {
		t.Fatalf("load returned %v", err)
	}

	got, err := restarted.ResolveUpstreamNamespace(context.Background(), downstreamNS)
	if err != nil {
		t.Fatalf("resolve after restart returned %v", err)
	}
	if got != ref {
		t.Errorf("resolve after restart returned %+v, want %+v", got, ref)
	}
}

func TestNamespaceIndexLoadTolerantOfMissingFile(t *testing.T) {
	if err := syncedIndex().Load(filepath.Join(t.TempDir(), "absent.json")); err != nil {
		t.Fatalf("load of a missing file returned %v", err)
	}
}

func TestUpstreamNamespaceRefFromDownstreamNamespace(t *testing.T) {
	namespace := &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{
		Name: downstreamNS,
		Labels: map[string]string{
			upstreamClusterNameLabel: "cluster-acme-prod",
			upstreamNamespaceLabel:   "default",
		},
	}}

	ref, ok := UpstreamNamespaceRefFromDownstreamNamespace(namespace)
	if !ok {
		t.Fatal("labelled namespace was not recognised")
	}
	if ref.ClusterName != "acme-prod" || ref.Namespace != "default" {
		t.Errorf("ref = %+v", ref)
	}

	if _, ok := UpstreamNamespaceRefFromDownstreamNamespace(&corev1.Namespace{}); ok {
		t.Error("unlabelled namespace was recognised")
	}
}

func TestDownstreamNamespaceName(t *testing.T) {
	if got := DownstreamNamespaceName(projectNamespaceUID); got != downstreamNS {
		t.Errorf("DownstreamNamespaceName = %q, want %q", got, downstreamNS)
	}
	if !IsDownstreamNamespace(downstreamNS) {
		t.Error("projected namespace not recognised")
	}
	if IsDownstreamNamespace("ns-") {
		t.Error("bare prefix treated as a projected namespace")
	}
}
