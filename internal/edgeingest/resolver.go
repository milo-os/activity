package edgeingest

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/types"
	toolscache "k8s.io/client-go/tools/cache"
)

// downstreamNamespacePrefix is the prefix Milo's MappedNamespaceResourceStrategy
// gives every namespace it projects into a downstream cluster.
const downstreamNamespacePrefix = "ns-"

// Label keys written onto projected downstream namespaces by Milo's
// downstreamclient package.
//
// TODO: consume go.miloapis.com/milo/pkg/downstreamclient once a Milo release
// carries its UpstreamNamespaceResolver. The mapping is owned there; it is
// mirrored here only because taking the Milo module as a dependency today would
// pull k8s.io/kubernetes into this server's build graph.
const (
	upstreamClusterNameLabel = "meta.datumapis.com/upstream-cluster-name"
	upstreamNamespaceLabel   = "meta.datumapis.com/upstream-namespace"
)

// Resolution failures an [UpstreamNamespaceResolver] can report. Callers must
// keep them apart: ErrCacheNotSynced says "ask again shortly" and
// ErrUpstreamNamespaceUnknown says "there is no safe answer". Treating either
// as platform scope would publish a customer's records outside their project.
var (
	// ErrCacheNotSynced indicates the namespace caches are still warming.
	ErrCacheNotSynced = errors.New("upstream namespace cache has not synced")

	// ErrUpstreamNamespaceUnknown indicates the downstream namespace maps to no
	// known upstream namespace.
	ErrUpstreamNamespaceUnknown = errors.New("no upstream namespace known for downstream namespace")
)

// UpstreamNamespaceRef identifies the upstream namespace a downstream
// ns-<uid> namespace was projected from. ClusterName is a Milo project control
// plane name, which is also the project name.
type UpstreamNamespaceRef struct {
	ClusterName string `json:"clusterName"`
	Namespace   string `json:"namespace"`
}

// IsZero reports whether the reference carries neither cluster nor namespace.
func (r UpstreamNamespaceRef) IsZero() bool {
	return r.ClusterName == "" && r.Namespace == ""
}

// UpstreamNamespaceResolver reverses the ns-<uid> projection.
//
// Implementations must never return a zero-valued reference with a nil error,
// and must distinguish a cold cache from an unknown namespace.
type UpstreamNamespaceResolver interface {
	ResolveUpstreamNamespace(ctx context.Context, downstreamNamespace string) (UpstreamNamespaceRef, error)

	// HasSynced reports whether every backing cache finished its initial sync.
	// Readiness gates on this, not on the listener being bound: accepting
	// traffic against a cold cache misfiles a window of records on every
	// deploy.
	HasSynced() bool
}

// IsDownstreamNamespace reports whether a namespace name looks like one Milo
// projected into a downstream cluster.
func IsDownstreamNamespace(namespace string) bool {
	return strings.HasPrefix(namespace, downstreamNamespacePrefix) && len(namespace) > len(downstreamNamespacePrefix)
}

// DownstreamNamespaceName renders the downstream namespace name for an upstream
// namespace UID.
func DownstreamNamespaceName(upstreamNamespaceUID types.UID) string {
	return downstreamNamespacePrefix + string(upstreamNamespaceUID)
}

// UpstreamNamespaceRefFromDownstreamNamespace reads the upstream labels off a
// projected namespace. The second return value is false for namespaces that
// carry no upstream labels.
func UpstreamNamespaceRefFromDownstreamNamespace(namespace *corev1.Namespace) (UpstreamNamespaceRef, bool) {
	if namespace == nil {
		return UpstreamNamespaceRef{}, false
	}

	encodedCluster := namespace.Labels[upstreamClusterNameLabel]
	upstreamNamespace := namespace.Labels[upstreamNamespaceLabel]
	if encodedCluster == "" || upstreamNamespace == "" {
		return UpstreamNamespaceRef{}, false
	}

	return UpstreamNamespaceRef{
		ClusterName: decodeUpstreamClusterName(encodedCluster),
		Namespace:   upstreamNamespace,
	}, true
}

// decodeUpstreamClusterName reverses the "cluster-<name>" encoding, restoring
// "_" to "/".
func decodeUpstreamClusterName(encoded string) string {
	return strings.ReplaceAll(strings.TrimPrefix(encoded, "cluster-"), "_", "/")
}

// NamespaceIndex is an [UpstreamNamespaceResolver] backed by a map that only
// ever grows.
//
// Growth-only is the point. Informers drop objects on delete, but audit records
// about a deleted namespace stay queryable for the full retention window and
// must keep resolving long after the namespace is torn down. Entries are
// therefore never removed, and [NamespaceIndex.Save] persists them so a restart
// does not lose the mappings for namespaces deleted while the process was down.
type NamespaceIndex struct {
	mu      sync.RWMutex
	entries map[string]UpstreamNamespaceRef
	dirty   bool

	syncedFuncs []toolscache.InformerSynced
}

var _ UpstreamNamespaceResolver = (*NamespaceIndex)(nil)

// NewNamespaceIndex returns an empty index.
func NewNamespaceIndex() *NamespaceIndex {
	return &NamespaceIndex{entries: map[string]UpstreamNamespaceRef{}}
}

// Upsert records the upstream namespace for a downstream namespace name.
func (i *NamespaceIndex) Upsert(downstreamNamespace string, ref UpstreamNamespaceRef) {
	if downstreamNamespace == "" || ref.IsZero() {
		return
	}

	i.mu.Lock()
	defer i.mu.Unlock()
	if existing, ok := i.entries[downstreamNamespace]; ok && existing == ref {
		return
	}
	i.entries[downstreamNamespace] = ref
	i.dirty = true
}

// Len returns the number of indexed namespaces.
func (i *NamespaceIndex) Len() int {
	i.mu.RLock()
	defer i.mu.RUnlock()
	return len(i.entries)
}

// ResolveUpstreamNamespace implements [UpstreamNamespaceResolver].
func (i *NamespaceIndex) ResolveUpstreamNamespace(_ context.Context, downstreamNamespace string) (UpstreamNamespaceRef, error) {
	i.mu.RLock()
	ref, ok := i.entries[downstreamNamespace]
	i.mu.RUnlock()

	if ok {
		return ref, nil
	}

	if !i.HasSynced() {
		return UpstreamNamespaceRef{}, fmt.Errorf("%w: %q", ErrCacheNotSynced, downstreamNamespace)
	}

	return UpstreamNamespaceRef{}, fmt.Errorf("%w: %q", ErrUpstreamNamespaceUnknown, downstreamNamespace)
}

// HasSynced implements [UpstreamNamespaceResolver]. An index with no registered
// sources never reports synced, so a misconfigured deployment fails readiness
// instead of quietly failing every lookup closed.
func (i *NamespaceIndex) HasSynced() bool {
	i.mu.RLock()
	syncedFuncs := make([]toolscache.InformerSynced, len(i.syncedFuncs))
	copy(syncedFuncs, i.syncedFuncs)
	i.mu.RUnlock()

	if len(syncedFuncs) == 0 {
		return false
	}

	for _, synced := range syncedFuncs {
		if !synced() {
			return false
		}
	}
	return true
}

// AddSyncedFunc registers a cache-sync predicate [HasSynced] must observe.
func (i *NamespaceIndex) AddSyncedFunc(synced toolscache.InformerSynced) {
	i.mu.Lock()
	defer i.mu.Unlock()
	i.syncedFuncs = append(i.syncedFuncs, synced)
}

// Snapshot returns a copy of the index contents.
func (i *NamespaceIndex) Snapshot() map[string]UpstreamNamespaceRef {
	i.mu.RLock()
	defer i.mu.RUnlock()

	out := make(map[string]UpstreamNamespaceRef, len(i.entries))
	for k, v := range i.entries {
		out[k] = v
	}
	return out
}

// Restore merges persisted entries in without displacing live ones.
func (i *NamespaceIndex) Restore(snapshot map[string]UpstreamNamespaceRef) {
	i.mu.Lock()
	defer i.mu.Unlock()

	for downstreamNamespace, ref := range snapshot {
		if downstreamNamespace == "" || ref.IsZero() {
			continue
		}
		if _, exists := i.entries[downstreamNamespace]; !exists {
			i.entries[downstreamNamespace] = ref
		}
	}
}

// Load restores a previously saved index from path. A missing file is not an
// error: it is the normal state on a first start.
func (i *NamespaceIndex) Load(path string) error {
	if path == "" {
		return nil
	}

	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("failed reading namespace index from %s: %w", path, err)
	}

	snapshot := map[string]UpstreamNamespaceRef{}
	if err := json.Unmarshal(data, &snapshot); err != nil {
		return fmt.Errorf("failed parsing namespace index at %s: %w", path, err)
	}

	i.Restore(snapshot)
	return nil
}

// Save writes the index to path through a temporary file and a rename, so a
// crash mid-write cannot leave a truncated index behind. It is a no-op when
// nothing changed since the last save.
func (i *NamespaceIndex) Save(path string) error {
	if path == "" {
		return nil
	}

	i.mu.Lock()
	if !i.dirty {
		i.mu.Unlock()
		return nil
	}
	i.dirty = false
	i.mu.Unlock()

	data, err := json.Marshal(i.Snapshot())
	if err != nil {
		return fmt.Errorf("failed encoding namespace index: %w", err)
	}

	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("failed creating namespace index directory: %w", err)
	}

	tmp, err := os.CreateTemp(filepath.Dir(path), filepath.Base(path)+".*")
	if err != nil {
		return fmt.Errorf("failed creating namespace index temp file: %w", err)
	}
	tmpName := tmp.Name()

	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		os.Remove(tmpName)
		return fmt.Errorf("failed writing namespace index: %w", err)
	}
	if err := tmp.Close(); err != nil {
		os.Remove(tmpName)
		return fmt.Errorf("failed closing namespace index: %w", err)
	}

	if err := os.Rename(tmpName, path); err != nil {
		os.Remove(tmpName)
		return fmt.Errorf("failed replacing namespace index: %w", err)
	}

	return nil
}
