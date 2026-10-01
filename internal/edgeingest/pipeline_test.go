package edgeingest

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	authnv1 "k8s.io/api/authentication/v1"
	auditv1 "k8s.io/apiserver/pkg/apis/audit/v1"

	"go.miloapis.com/activity/internal/processor"
	"go.miloapis.com/activity/internal/types"
)

type captureEmitter struct {
	events   []*auditv1.Event
	clusters []string
}

func (c *captureEmitter) Emit(_ context.Context, cluster string, event *auditv1.Event) error {
	c.events = append(c.events, event)
	c.clusters = append(c.clusters, cluster)
	return nil
}

var testIdentity = ClusterIdentity{Name: "dfw1", Location: "us-central-1"}

func newPipeline(index *NamespaceIndex) (*Pipeline, *captureEmitter) {
	emitter := &captureEmitter{}
	return &Pipeline{Resolver: index, Emitter: emitter}, emitter
}

func TestPipelineAttributesToProject(t *testing.T) {
	index := syncedIndex()
	index.Upsert(downstreamNS, UpstreamNamespaceRef{ClusterName: "acme-prod", Namespace: "default"})

	pipeline, emitter := newPipeline(index)

	if err := pipeline.Process(context.Background(), testIdentity, edgeEvent(t)); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(emitter.events) != 1 {
		t.Fatalf("emitted %d events, want 1", len(emitter.events))
	}
	emitted := emitter.events[0]

	if got := emitted.Annotations[ScopeTypeAnnotation]; got != types.TenantTypeProject {
		t.Errorf("scope.type = %q, want %q", got, types.TenantTypeProject)
	}
	if got := emitted.Annotations[ScopeNameAnnotation]; got != "acme-prod" {
		t.Errorf("scope.name = %q, want acme-prod", got)
	}
	if got := emitted.Annotations[LocationAnnotation]; got != "us-central-1" {
		t.Errorf("location = %q, want us-central-1", got)
	}
	if got := emitted.Annotations[SourceClusterAnnotation]; got != "dfw1" {
		t.Errorf("source cluster = %q, want dfw1", got)
	}

	tenant := processor.ExtractTenant(emitted.User)
	if tenant.Type != types.TenantTypeProject || tenant.Name != "acme-prod" {
		t.Errorf("processor read tenant %+v, want Project/acme-prod", tenant)
	}

	encoded, err := json.Marshal(emitted)
	if err != nil {
		t.Fatalf("failed encoding emitted event: %v", err)
	}
	if downstreamNamespacePattern.Match(encoded) {
		t.Errorf("emitted event leaks a downstream namespace: %s", encoded)
	}
}

func TestPipelineAttributesPlatformScopedRequestToPlatform(t *testing.T) {
	index := syncedIndex()
	pipeline, emitter := newPipeline(index)

	event := &auditv1.Event{
		AuditID:    "platform-1",
		RequestURI: "/api/v1/nodes",
		Verb:       "list",
		ObjectRef:  &auditv1.ObjectReference{Resource: "nodes"},
	}

	if err := pipeline.Process(context.Background(), testIdentity, event); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	emitted := emitter.events[0]
	if got := emitted.Annotations[ScopeTypeAnnotation]; got != ScopeTypeGlobal {
		t.Errorf("scope.type = %q, want %q", got, ScopeTypeGlobal)
	}
	if _, present := emitted.Annotations[ScopeNameAnnotation]; present {
		t.Error("platform-scoped record carries a scope name")
	}
	if tenant := processor.ExtractTenant(emitted.User); tenant.Type != types.TenantTypePlatform {
		t.Errorf("processor read tenant %+v, want platform", tenant)
	}
	if got := emitted.Annotations[LocationAnnotation]; got != "us-central-1" {
		t.Errorf("location = %q, want us-central-1", got)
	}
}

func TestPipelineIgnoresTenancyClaimedByTheShipper(t *testing.T) {
	index := syncedIndex()
	pipeline, emitter := newPipeline(index)

	event := &auditv1.Event{
		AuditID:    "claimed-1",
		RequestURI: "/api/v1/nodes",
		ObjectRef:  &auditv1.ObjectReference{Resource: "nodes"},
		Annotations: map[string]string{
			ScopeTypeAnnotation: types.TenantTypeProject,
			ScopeNameAnnotation: "victim-project",
			LocationAnnotation:  "somewhere-else",
		},
		User: authnv1.UserInfo{Extra: map[string]authnv1.ExtraValue{
			ParentTypeExtraKey: {types.TenantTypeProject},
			ParentNameExtraKey: {"victim-project"},
		}},
	}

	if err := pipeline.Process(context.Background(), testIdentity, event); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	emitted := emitter.events[0]
	if got := emitted.Annotations[ScopeNameAnnotation]; got != "" {
		t.Errorf("shipper-claimed scope survived: %q", got)
	}
	if tenant := processor.ExtractTenant(emitted.User); tenant.Type != types.TenantTypePlatform {
		t.Errorf("shipper-claimed tenant survived: %+v", tenant)
	}
	if got := emitted.Annotations[LocationAnnotation]; got != "us-central-1" {
		t.Errorf("shipper-claimed location survived: %q", got)
	}
}

func TestPipelineFailsClosedOnUnresolvableNamespace(t *testing.T) {
	index := syncedIndex()
	pipeline, emitter := newPipeline(index)

	err := pipeline.Process(context.Background(), testIdentity, edgeEvent(t))
	if !errors.Is(err, ErrUpstreamNamespaceUnknown) {
		t.Fatalf("err = %v, want ErrUpstreamNamespaceUnknown", err)
	}
	if len(emitter.events) != 0 {
		t.Fatal("an unresolvable record was emitted")
	}
}

func TestPipelineResolvesRecordsAboutDeletedNamespaces(t *testing.T) {
	index := syncedIndex()
	index.Upsert(downstreamNS, UpstreamNamespaceRef{ClusterName: "acme-prod", Namespace: "default"})

	// Simulate the namespace being deleted upstream: informers would drop it,
	// but the index retains it, so records about it still resolve.
	pipeline, emitter := newPipeline(index)

	event := edgeEvent(t)
	event.Verb = "delete"
	if err := pipeline.Process(context.Background(), testIdentity, event); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got := emitter.events[0].Annotations[ScopeNameAnnotation]; got != "acme-prod" {
		t.Errorf("scope.name = %q, want acme-prod", got)
	}
}

func TestPipelineFailsClosedWhenNamespacesSpanProjects(t *testing.T) {
	index := syncedIndex()
	index.Upsert(downstreamNS, UpstreamNamespaceRef{ClusterName: "acme-prod", Namespace: "default"})
	index.Upsert(otherNS, UpstreamNamespaceRef{ClusterName: "other-project", Namespace: "default"})

	pipeline, emitter := newPipeline(index)

	event := edgeEvent(t)
	event.ResponseObject = newRawObject(t, map[string]any{
		"items": []any{
			map[string]any{"metadata": map[string]any{"namespace": downstreamNS}},
			map[string]any{"metadata": map[string]any{"namespace": otherNS}},
		},
	})

	err := pipeline.Process(context.Background(), testIdentity, event)
	if !errors.Is(err, ErrAmbiguousProject) {
		t.Fatalf("err = %v, want ErrAmbiguousProject", err)
	}
	if len(emitter.events) != 0 {
		t.Fatal("an ambiguous record was emitted")
	}
}

func TestPipelineParksOnColdCacheThenResolves(t *testing.T) {
	index := NewNamespaceIndex()
	synced := false
	index.AddSyncedFunc(func() bool { return synced })

	pipeline, emitter := newPipeline(index)
	pipeline.ParkTimeout = 5 * time.Second
	pipeline.ParkPollInterval = 10 * time.Millisecond

	go func() {
		time.Sleep(50 * time.Millisecond)
		index.Upsert(downstreamNS, UpstreamNamespaceRef{ClusterName: "acme-prod", Namespace: "default"})
		synced = true
	}()

	if err := pipeline.Process(context.Background(), testIdentity, edgeEvent(t)); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got := emitter.events[0].Annotations[ScopeNameAnnotation]; got != "acme-prod" {
		t.Errorf("scope.name = %q, want acme-prod", got)
	}
}

func TestPipelineReportsRetryableWhenParkExpires(t *testing.T) {
	index := NewNamespaceIndex()
	index.AddSyncedFunc(func() bool { return false })

	pipeline, emitter := newPipeline(index)
	pipeline.ParkTimeout = 50 * time.Millisecond
	pipeline.ParkPollInterval = 10 * time.Millisecond

	err := pipeline.Process(context.Background(), testIdentity, edgeEvent(t))
	if !errors.Is(err, ErrCacheNotSynced) {
		t.Fatalf("err = %v, want ErrCacheNotSynced", err)
	}
	if len(emitter.events) != 0 {
		t.Fatal("a record was emitted against a cold cache")
	}
	if !strings.Contains(err.Error(), downstreamNS) {
		t.Errorf("error does not name the unresolved namespace: %v", err)
	}
}
