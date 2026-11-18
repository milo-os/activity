package activityprocessor

import (
	"context"
	"errors"
	"fmt"
	"io"
	"sync"
	"sync/atomic"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/apimachinery/pkg/watch"
	"k8s.io/client-go/dynamic"
	toolscache "k8s.io/client-go/tools/cache"
	"k8s.io/klog/v2"
)

// defaultFatalGrace is how long a forbidden, unauthorized or not-found
// list/watch error must persist before WaitForSync gives up early, so a
// transient 401/403 (for example during credential rotation) does not fail a
// re-index.
const defaultFatalGrace = 10 * time.Second

// ErrIndexUnavailable is returned by WaitForSync when an index cannot sync:
// the API server denied list/watch, or does not serve the resource. Retrying
// without a configuration change will not help.
var ErrIndexUnavailable = errors.New("index unavailable")

// ErrIndexSyncTimeout is returned by WaitForSync when an index did not sync
// within the timeout.
var ErrIndexSyncTimeout = errors.New("index did not sync in time")

// Aliases kept for the User index's callers.
var (
	ErrUserIndexUnavailable = ErrIndexUnavailable
	ErrUserIndexSyncTimeout = ErrIndexSyncTimeout
)

// indexGauges are an index's readiness metrics.
type indexGauges struct {
	synced, healthy, lastSync prometheus.Gauge
}

// indexConfig describes one informer-backed index.
type indexConfig struct {
	// description names the resource in logs, e.g. "iam User".
	description string
	// resource is the RBAC resource name, e.g. "users.iam.miloapis.com".
	resource string
	// unsyncedImpact says what happens while the index has not synced, e.g.
	// "activities are emitted without actor display names".
	unsyncedImpact string
	transform      toolscache.TransformFunc
	indexers       toolscache.Indexers
	gauges         indexGauges
}

// noWatchList marks an index's ListerWatcher as not supporting watch-list
// streaming, so the reflector always performs a real LIST. That keeps
// onListSuccess as the single signal for a successful (re)sync.
type noWatchList struct{}

func (noWatchList) IsWatchListSemanticsUnSupported() bool { return true }

// informerIndex is an informer over one Milo resource, kept as trimmed
// objects in memory, with readiness tracking shared by the activity
// enrichment indexes:
//
//   - Until the initial sync completes, Synced is false and callers must not
//     trust lookups.
//   - A forbidden, unauthorized or not-found error that persists across two
//     attempts and the grace period makes WaitForSync fail early with
//     ErrIndexUnavailable; anything else waits for the timeout.
//   - Each kind of failure is logged once per outage; a successful list
//     resets that, marks the index healthy and records the sync time.
//   - After the initial sync, an outage keeps serving the last-known data.
type informerIndex struct {
	cfg      indexConfig
	informer toolscache.SharedIndexInformer
	synced   atomic.Bool
	healthy  atomic.Bool

	// A streak of errors that retrying is unlikely to fix (forbidden,
	// unauthorized, not served): the latest such error, when the streak
	// started, and how many list/watch attempts it has spanned. A successful
	// list clears it. See unavailableErr.
	fatalErr   atomic.Pointer[error]
	fatalSince atomic.Int64 // unix nanoseconds; 0 when there is no streak
	fatalCount atomic.Int32
	fatalGrace time.Duration

	// loggedErrors records which error classes have been logged at error
	// level during the current outage, so a persistent failure logs once
	// rather than on every retry. A successful list resets it.
	loggedErrors sync.Map
}

// newInformerIndex builds an index over resource. Call Start to begin
// watching.
func newInformerIndex(resource dynamic.ResourceInterface, cfg indexConfig) (*informerIndex, error) {
	idx := &informerIndex{cfg: cfg, fatalGrace: defaultFatalGrace}
	lw := toolscache.ToListWatcherWithWatchListSemantics(&toolscache.ListWatch{
		ListWithContextFunc: func(ctx context.Context, opts metav1.ListOptions) (runtime.Object, error) {
			list, err := resource.List(ctx, opts)
			if err != nil {
				return nil, err
			}
			if list.GetContinue() == "" {
				idx.onListSuccess()
			}
			return list, nil
		},
		WatchFuncWithContext: func(ctx context.Context, opts metav1.ListOptions) (watch.Interface, error) {
			return resource.Watch(ctx, opts)
		},
	}, noWatchList{})

	idx.informer = toolscache.NewSharedIndexInformerWithOptions(lw, &unstructured.Unstructured{}, toolscache.SharedIndexInformerOptions{
		// No resync: lookups only read the store.
		Indexers:          cfg.indexers,
		ObjectDescription: cfg.description,
	})
	if err := idx.informer.SetTransform(cfg.transform); err != nil {
		return nil, err
	}
	if err := idx.informer.SetWatchErrorHandlerWithContext(idx.handleWatchError); err != nil {
		return nil, err
	}
	return idx, nil
}

// Start runs the informer in the background and returns immediately. The
// informer stops when ctx is cancelled. Use WaitForSync for a bounded wait.
func (x *informerIndex) Start(ctx context.Context) {
	x.cfg.gauges.synced.Set(0)
	x.cfg.gauges.healthy.Set(0)
	go x.informer.RunWithContext(ctx)
	go func() {
		err := wait.PollUntilContextCancel(ctx, 100*time.Millisecond, true, func(context.Context) (bool, error) {
			return x.informer.HasSynced(), nil
		})
		if err != nil {
			return
		}
		x.synced.Store(true)
		x.cfg.gauges.synced.Set(1)
		klog.InfoS(x.cfg.description+" index synced", "objects", len(x.informer.GetStore().ListKeys()))
	}()
}

// WaitForSync blocks until the index has synced, an error that retrying is
// unlikely to fix has persisted, timeout elapses, or ctx is cancelled. It
// returns nil once synced; an error wrapping ErrIndexUnavailable once list or
// watch has been forbidden, unauthorized or not served across at least two
// attempts spanning the grace period (10s by default); and an error wrapping
// ErrIndexSyncTimeout otherwise. The index keeps retrying in the background
// either way.
func (x *informerIndex) WaitForSync(ctx context.Context, timeout time.Duration) error {
	waitCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	_ = wait.PollUntilContextCancel(waitCtx, 50*time.Millisecond, true, func(context.Context) (bool, error) {
		return x.synced.Load() || x.unavailableErr() != nil, nil
	})
	if x.synced.Load() {
		return nil
	}
	if err := x.unavailableErr(); err != nil {
		return fmt.Errorf("%s %w: %v", x.cfg.description, ErrIndexUnavailable, err)
	}
	return fmt.Errorf("%s %w after %s", x.cfg.description, ErrIndexSyncTimeout, timeout)
}

// unavailableErr returns the latest forbidden, unauthorized or not-found
// error once such errors have persisted across at least two list/watch
// attempts and for at least the grace period, and nil otherwise.
func (x *informerIndex) unavailableErr() error {
	since := x.fatalSince.Load()
	if since == 0 || x.fatalCount.Load() < 2 || time.Since(time.Unix(0, since)) < x.fatalGrace {
		return nil
	}
	if errp := x.fatalErr.Load(); errp != nil {
		return *errp
	}
	return nil
}

// Synced reports whether the index has completed its initial sync.
func (x *informerIndex) Synced() bool {
	return x.synced.Load()
}

// Healthy reports whether the last list succeeded with no list/watch error
// since. An unhealthy index that has synced still serves its last-known data.
func (x *informerIndex) Healthy() bool {
	return x.healthy.Load()
}

// onListSuccess records a complete, successful LIST: the index is healthy,
// and the next outage should log at error level again.
func (x *informerIndex) onListSuccess() {
	x.fatalSince.Store(0)
	x.fatalCount.Store(0)
	x.fatalErr.Store(nil)
	x.loggedErrors.Clear()
	if !x.healthy.Swap(true) && x.synced.Load() {
		klog.InfoS(x.cfg.description + " index recovered")
	}
	x.cfg.gauges.healthy.Set(1)
	x.cfg.gauges.lastSync.SetToCurrentTime()
}

// handleWatchError replaces client-go's default handler so a persistent
// failure (RBAC denial, missing resource) logs once per outage instead of on
// every retry. Routine watch expiry and closes are logged at high verbosity
// only and do not count as an outage.
func (x *informerIndex) handleWatchError(_ context.Context, _ *toolscache.Reflector, err error) {
	var class string
	switch {
	case errors.Is(err, io.EOF), apierrors.IsResourceExpired(err), apierrors.IsGone(err):
		klog.V(4).InfoS(x.cfg.description+" watch closed; will re-establish", "err", err)
		return
	case apierrors.IsForbidden(err), apierrors.IsUnauthorized(err):
		class = "forbidden"
	case apierrors.IsNotFound(err), meta.IsNoMatchError(err):
		class = "notfound"
	default:
		class = "other"
	}

	x.healthy.Store(false)
	x.cfg.gauges.healthy.Set(0)
	if class != "other" {
		x.fatalErr.Store(&err)
		x.fatalSince.CompareAndSwap(0, time.Now().UnixNano())
		x.fatalCount.Add(1)
	}

	if _, seen := x.loggedErrors.LoadOrStore(class, struct{}{}); seen {
		klog.V(4).InfoS(x.cfg.description+" index list/watch failed; retrying", "err", err)
		return
	}

	impact := x.cfg.unsyncedImpact + " until the index syncs"
	if x.synced.Load() {
		impact = "serving the last-known index until list/watch recovers"
	}
	switch class {
	case "forbidden":
		klog.ErrorS(err, fmt.Sprintf("%s index cannot list/watch (grant get, list, watch on %s); %s",
			x.cfg.description, x.cfg.resource, impact))
	case "notfound":
		klog.ErrorS(err, fmt.Sprintf("%s resource not found on the API server; %s", x.cfg.description, impact))
	default:
		klog.ErrorS(err, fmt.Sprintf("%s index list/watch failed; retrying with backoff; %s", x.cfg.description, impact))
	}
}

// getByKey returns the stored object for key, or nil.
func (x *informerIndex) getByKey(key string) any {
	obj, exists, err := x.informer.GetIndexer().GetByKey(key)
	if err != nil || !exists {
		return nil
	}
	return obj
}

// byIndex returns the stored objects under key in the named index.
func (x *informerIndex) byIndex(index, key string) []any {
	objs, err := x.informer.GetIndexer().ByIndex(index, key)
	if err != nil {
		return nil
	}
	return objs
}
