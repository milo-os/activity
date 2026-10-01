package edgeingest

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	auditv1 "k8s.io/apiserver/pkg/apis/audit/v1"
)

// ErrAmbiguousProject indicates one record referenced namespaces belonging to
// more than one project, so no single tenant can own it.
var ErrAmbiguousProject = errors.New("audit event references namespaces from more than one project")

// Emitter publishes a rewritten audit event.
type Emitter interface {
	Emit(ctx context.Context, cluster string, event *auditv1.Event) error
}

// Pipeline turns a raw edge audit event into one the rest of Activity can treat
// exactly like a core control plane event: Resolve, Attribute, Stamp, Emit.
//
// Identification happens before the pipeline, at the transport, because that is
// the only place the cluster's identity is proven.
type Pipeline struct {
	Resolver UpstreamNamespaceResolver
	Emitter  Emitter

	// ParkTimeout bounds how long a record waits for cold namespace caches to
	// sync before the pipeline gives up and asks the shipper to retry. Waiting
	// briefly costs a request; failing straight to the platform bucket would
	// misfile every record produced during a deploy.
	ParkTimeout time.Duration

	// ParkPollInterval is how often the pipeline rechecks a cold cache.
	ParkPollInterval time.Duration
}

// Process runs one audit event through the pipeline.
//
// A returned error wrapping [ErrCacheNotSynced] is retryable and the caller
// should ask the shipper to send the record again. Every other error is
// terminal for that record.
func (p *Pipeline) Process(ctx context.Context, identity ClusterIdentity, event *auditv1.Event) error {
	references, err := DownstreamNamespaceReferences(event)
	if err != nil {
		return err
	}

	mapping, project, err := p.resolveAll(ctx, references, event)
	if err != nil {
		return err
	}

	if err := UnmapNamespaces(event, mapping); err != nil {
		return err
	}

	if project == "" {
		AttributeToPlatform(event)
	} else {
		AttributeToProject(event, project)
	}

	StampLocation(event, identity)

	return p.Emitter.Emit(ctx, identity.Name, event)
}

// resolveAll resolves every downstream namespace the event names and returns
// the rewrite mapping plus the project the event belongs to. An empty project
// means the event named no downstream namespace and is platform-scoped.
func (p *Pipeline) resolveAll(ctx context.Context, references []string, event *auditv1.Event) (map[string]string, string, error) {
	if len(references) == 0 {
		return nil, "", nil
	}

	primary, _ := PrimaryDownstreamNamespace(event)
	primary = strings.ToLower(primary)

	mapping := make(map[string]string, len(references))
	clusters := map[string]struct{}{}
	project := ""

	for _, reference := range references {
		ref, err := p.resolveWithPark(ctx, reference)
		if err != nil {
			return nil, "", err
		}

		mapping[reference] = ref.Namespace
		clusters[ref.ClusterName] = struct{}{}

		if reference == primary {
			project = ref.ClusterName
		}
	}

	if len(clusters) > 1 {
		return nil, "", fmt.Errorf("%w: %s", ErrAmbiguousProject, strings.Join(sortedClusterNames(clusters), ", "))
	}

	if project == "" {
		for clusterName := range clusters {
			project = clusterName
		}
	}

	return mapping, project, nil
}

func sortedClusterNames(clusters map[string]struct{}) []string {
	names := make([]string, 0, len(clusters))
	for name := range clusters {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

// resolveWithPark parks a record on a cold cache instead of resolving it
// against one. Readiness already gates on cache sync, so this only covers the
// window where a namespace is genuinely new.
func (p *Pipeline) resolveWithPark(ctx context.Context, downstreamNamespace string) (UpstreamNamespaceRef, error) {
	ref, err := p.Resolver.ResolveUpstreamNamespace(ctx, downstreamNamespace)
	if !errors.Is(err, ErrCacheNotSynced) || p.ParkTimeout <= 0 {
		return ref, err
	}

	interval := p.ParkPollInterval
	if interval <= 0 {
		interval = 250 * time.Millisecond
	}

	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	deadline := time.NewTimer(p.ParkTimeout)
	defer deadline.Stop()

	for {
		select {
		case <-ctx.Done():
			return UpstreamNamespaceRef{}, ctx.Err()
		case <-deadline.C:
			return p.Resolver.ResolveUpstreamNamespace(ctx, downstreamNamespace)
		case <-ticker.C:
			ref, err := p.Resolver.ResolveUpstreamNamespace(ctx, downstreamNamespace)
			if !errors.Is(err, ErrCacheNotSynced) {
				return ref, err
			}
		}
	}
}
