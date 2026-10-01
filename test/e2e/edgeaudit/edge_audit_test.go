// Package edgeaudit end-to-end tests the edge audit ingest path: a recorded
// audit batch is posted over mutual TLS to a real ingest listener, published to
// a real NATS JetStream, landed in a real ClickHouse, and then queried back
// through the same storage layer the API server uses.
//
// Only Vector is stood in for, by a drain that copies published events into
// ClickHouse exactly as its NATS-to-ClickHouse sink does.
package edgeaudit

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/nats-io/nats.go"
	auditv1 "k8s.io/apiserver/pkg/apis/audit/v1"

	"go.miloapis.com/activity/internal/edgeingest"
	"go.miloapis.com/activity/internal/storage"
	"go.miloapis.com/activity/internal/testpki"
	"go.miloapis.com/activity/internal/types"
	"go.miloapis.com/activity/pkg/apis/activity/v1alpha1"
)

const (
	acmeDefaultNS  = "ns-9f3c8a21-4c1b-4d0e-9a3f-2b1c7e5d8a90"
	acmeBatchNS    = "ns-bb22cc33-dd44-ee55-ff66-778899001122"
	globexDefault  = "ns-aa11bb22-cc33-dd44-ee55-ff6677889900"
	shipperDFW     = "audit-shipper.dfw1.edge.datum.net"
	locationDFW    = "us-central-1"
	acmeProject    = "acme-prod"
	globexProject  = "globex-dev"
	queryStartTime = "now-24h"
	queryEndTime   = "now"
)

// downstreamNamespacePattern mirrors the guard inside the ingest path. Every
// record that comes back out of ClickHouse is checked against it: an internal
// ns-<uid> reaching a customer is the failure this whole path exists to
// prevent.
var downstreamNamespacePattern = regexp.MustCompile(`(?i)ns-[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}`)

func readTestdata(name string) ([]byte, error) {
	return os.ReadFile(filepath.Join("testdata", name))
}

// harness is the whole ingest path, wired to real infrastructure.
type harness struct {
	baseURL string
	pki     *testpki.PKI
	store   *storage.ClickHouseStorage
	js      nats.JetStreamContext
	index   *edgeingest.NamespaceIndex
}

func newHarness(t *testing.T) *harness {
	t.Helper()
	requireDocker(t)

	store := startClickHouse(t)
	natsURL, js := startNATS(t)
	pki := testpki.New(t)

	index := edgeingest.NewNamespaceIndex()
	index.AddSyncedFunc(func() bool { return true })
	index.Upsert(acmeDefaultNS, edgeingest.UpstreamNamespaceRef{ClusterName: acmeProject, Namespace: "default"})
	index.Upsert(globexDefault, edgeingest.UpstreamNamespaceRef{ClusterName: globexProject, Namespace: "default"})
	// Indexed while the namespace existed, then deleted upstream. The index
	// never drops it, so records about it keep resolving.
	index.Upsert(acmeBatchNS, edgeingest.UpstreamNamespaceRef{ClusterName: acmeProject, Namespace: "batch"})

	emitter, err := edgeingest.NewNATSEmitter(edgeingest.NATSEmitterConfig{
		URL:           natsURL,
		StreamName:    "AUDIT_EVENTS",
		SubjectPrefix: "audit.k8s.edge",
	})
	if err != nil {
		t.Fatalf("failed creating emitter: %v", err)
	}
	t.Cleanup(emitter.Close)

	registry := edgeingest.NewClusterRegistry(map[string]edgeingest.ClusterIdentity{
		shipperDFW: {Name: "dfw1", Location: locationDFW},
	})

	pipeline := &edgeingest.Pipeline{Resolver: index, Emitter: emitter}

	server, err := edgeingest.NewServer(edgeingest.ServerConfig{
		Address:      "127.0.0.1:0",
		TLSCertFile:  pki.ServingCertFile,
		TLSKeyFile:   pki.ServingKeyFile,
		ClientCAFile: pki.CAFile,
	}, registry, pipeline, index)
	if err != nil {
		t.Fatalf("failed creating server: %v", err)
	}

	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("failed listening: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		defer close(done)
		if err := server.Serve(ctx, listener); err != nil {
			t.Errorf("ingest server returned %v", err)
		}
	}()
	t.Cleanup(func() {
		cancel()
		<-done
	})

	return &harness{
		baseURL: "https://" + listener.Addr().String(),
		pki:     pki,
		store:   store,
		js:      js,
		index:   index,
	}
}

// ship posts a recorded fixture as the DFW edge shipper and returns the result.
//
// Fixture timestamps are rebased onto the present so the recorded batches stay
// inside the query window no matter when the suite runs.
func (h *harness) ship(t *testing.T, fixture string) (int, ingestResult) {
	t.Helper()

	raw, err := readTestdata(fixture)
	if err != nil {
		t.Fatalf("failed reading fixture %s: %v", fixture, err)
	}

	body := rebaseTimestamps(t, raw)

	response, err := h.pki.Client(t, shipperDFW).Post(h.baseURL+edgeingest.AuditPath, "application/json", bytes.NewReader(body))
	if err != nil {
		t.Fatalf("failed shipping %s: %v", fixture, err)
	}
	defer response.Body.Close()

	payload, err := io.ReadAll(response.Body)
	if err != nil {
		t.Fatalf("failed reading ingest response: %v", err)
	}

	result := ingestResult{}
	if response.StatusCode == http.StatusOK {
		if err := json.Unmarshal(payload, &result); err != nil {
			t.Fatalf("failed decoding ingest result: %v", err)
		}
	} else {
		t.Logf("%s rejected with %d: %s", fixture, response.StatusCode, payload)
	}

	return response.StatusCode, result
}

// rebaseTimestamps rewrites every audit timestamp in a fixture to an hour ago.
func rebaseTimestamps(t *testing.T, raw []byte) []byte {
	t.Helper()

	var list map[string]any
	if err := json.Unmarshal(raw, &list); err != nil {
		t.Fatalf("failed decoding fixture: %v", err)
	}

	// metav1.MicroTime parses exactly six fractional digits, which is what a
	// kube-apiserver emits. RFC3339Nano trims trailing zeros and would produce
	// batches the API rejects.
	when := time.Now().UTC().Add(-time.Hour).Format("2006-01-02T15:04:05.000000Z07:00")
	items, _ := list["items"].([]any)
	for _, item := range items {
		event, ok := item.(map[string]any)
		if !ok {
			continue
		}
		event["requestReceivedTimestamp"] = when
		event["stageTimestamp"] = when
	}

	rebased, err := json.Marshal(list)
	if err != nil {
		t.Fatalf("failed re-encoding fixture: %v", err)
	}
	return rebased
}

type ingestResult struct {
	Received int `json:"received"`
	Emitted  int `json:"emitted"`
	Dropped  int `json:"dropped"`
}

func (h *harness) query(t *testing.T, scope storage.ScopeContext, filter string) []auditv1.Event {
	t.Helper()

	result, err := h.store.QueryAuditLogs(context.Background(), v1alpha1.AuditLogQuerySpec{
		StartTime: queryStartTime,
		EndTime:   queryEndTime,
		Filter:    filter,
		Limit:     100,
	}, scope)
	if err != nil {
		t.Fatalf("query failed: %v", err)
	}
	return result.Events
}

func projectScope(name string) storage.ScopeContext {
	return storage.ScopeContext{Type: types.TenantTypeProject, Name: name}
}

func platformScope() storage.ScopeContext {
	return storage.ScopeContext{Type: types.TenantTypePlatform}
}

func auditIDs(events []auditv1.Event) []string {
	ids := make([]string, 0, len(events))
	for _, event := range events {
		ids = append(ids, string(event.AuditID))
	}
	return ids
}

func requireNoDownstreamNamespace(t *testing.T, events []auditv1.Event) {
	t.Helper()

	for _, event := range events {
		encoded, err := json.Marshal(event)
		if err != nil {
			t.Fatalf("failed encoding returned event: %v", err)
		}
		if match := downstreamNamespacePattern.FindString(string(encoded)); match != "" {
			t.Errorf("returned record leaks downstream namespace %s: %s", match, encoded)
		}
		if downstreamNamespacePattern.MatchString(event.RequestURI) {
			t.Errorf("returned requestURI leaks a downstream namespace: %s", event.RequestURI)
		}
	}
}

func TestEdgeAuditIngest(t *testing.T) {
	h := newHarness(t)

	fixtures := []struct {
		file    string
		emitted int
		dropped int
	}{
		{"project-namespaced.json", 1, 0},
		{"platform-scoped.json", 1, 0},
		{"deleted-namespace.json", 1, 0},
		{"other-project.json", 1, 0},
		{"claimed-tenancy.json", 1, 0},
		{"unresolvable-namespace.json", 0, 1},
	}

	totalEmitted := 0
	for _, fixture := range fixtures {
		status, result := h.ship(t, fixture.file)
		if status != http.StatusOK {
			t.Fatalf("%s: status = %d, want 200", fixture.file, status)
		}
		if result.Emitted != fixture.emitted || result.Dropped != fixture.dropped {
			t.Fatalf("%s: result = %+v, want emitted %d dropped %d",
				fixture.file, result, fixture.emitted, fixture.dropped)
		}
		totalEmitted += result.Emitted
	}

	drainToClickHouse(t, h.js, h.store, totalEmitted)

	t.Run("project namespaced request lands in its project", func(t *testing.T) {
		events := h.query(t, projectScope(acmeProject), "objectRef.resource == 'instances'")
		if len(events) != 1 {
			t.Fatalf("returned %d events (%v), want 1", len(events), auditIDs(events))
		}

		event := events[0]
		if event.ObjectRef.Namespace != "default" {
			t.Errorf("objectRef.namespace = %q, want default", event.ObjectRef.Namespace)
		}
		if !strings.Contains(event.RequestURI, "/namespaces/default/") {
			t.Errorf("requestURI was not un-mapped: %s", event.RequestURI)
		}
		if got := event.Annotations[edgeingest.LocationAnnotation]; got != locationDFW {
			t.Errorf("location = %q, want %q", got, locationDFW)
		}
		if got := event.Annotations[edgeingest.SourceClusterAnnotation]; got != "dfw1" {
			t.Errorf("source cluster = %q, want dfw1", got)
		}

		requireNoDownstreamNamespace(t, events)
	})

	t.Run("platform scoped request is not misfiled into a project", func(t *testing.T) {
		projectEvents := h.query(t, projectScope(acmeProject), "objectRef.resource == 'nodes'")
		if len(projectEvents) != 0 {
			t.Fatalf("platform-scoped records appeared in a project: %v", auditIDs(projectEvents))
		}

		platformEvents := h.query(t, platformScope(), "objectRef.resource == 'nodes'")
		if len(platformEvents) != 2 {
			t.Fatalf("platform query returned %d events (%v), want 2", len(platformEvents), auditIDs(platformEvents))
		}
		for _, event := range platformEvents {
			if got := event.Annotations[edgeingest.ScopeTypeAnnotation]; got != edgeingest.ScopeTypeGlobal {
				t.Errorf("scope.type = %q, want %q", got, edgeingest.ScopeTypeGlobal)
			}
			if got := event.Annotations[edgeingest.ScopeNameAnnotation]; got != "" {
				t.Errorf("platform record carries scope.name %q", got)
			}
			if got := event.Annotations[edgeingest.LocationAnnotation]; got != locationDFW {
				t.Errorf("location = %q, want %q", got, locationDFW)
			}
		}
	})

	t.Run("tenancy claimed by the shipper is overwritten", func(t *testing.T) {
		events := h.query(t, projectScope(acmeProject), "objectRef.name == 'edge-worker-3'")
		if len(events) != 0 {
			t.Fatalf("a shipper claimed its way into a project: %v", auditIDs(events))
		}

		platformEvents := h.query(t, platformScope(), "objectRef.name == 'edge-worker-3'")
		if len(platformEvents) != 1 {
			t.Fatalf("returned %d events, want 1", len(platformEvents))
		}
		if got := platformEvents[0].User.Extra[edgeingest.ParentNameExtraKey]; len(got) != 0 {
			t.Errorf("shipper-claimed user extras survived: %v", got)
		}
		if got := platformEvents[0].Annotations[edgeingest.LocationAnnotation]; got != locationDFW {
			t.Errorf("shipper-claimed location survived: %q", got)
		}
	})

	t.Run("unresolvable namespace never reaches storage", func(t *testing.T) {
		for _, scope := range []storage.ScopeContext{platformScope(), projectScope(acmeProject), projectScope(globexProject)} {
			events := h.query(t, scope, "objectRef.resource == 'secrets'")
			if len(events) != 0 {
				t.Errorf("scope %s/%s returned an unresolvable record: %v", scope.Type, scope.Name, auditIDs(events))
			}
			requireNoDownstreamNamespace(t, events)
		}
	})

	t.Run("record about a deleted namespace still resolves", func(t *testing.T) {
		events := h.query(t, projectScope(acmeProject), "verb == 'delete'")
		if len(events) != 1 {
			t.Fatalf("returned %d events (%v), want 1", len(events), auditIDs(events))
		}
		if events[0].ObjectRef.Namespace != "batch" {
			t.Errorf("objectRef.namespace = %q, want batch", events[0].ObjectRef.Namespace)
		}
		requireNoDownstreamNamespace(t, events)
	})

	t.Run("a project cannot see another project's edge records", func(t *testing.T) {
		acme := h.query(t, projectScope(acmeProject), "")
		globex := h.query(t, projectScope(globexProject), "")

		if len(globex) != 1 {
			t.Fatalf("globex returned %d events (%v), want 1", len(globex), auditIDs(globex))
		}
		if globex[0].ObjectRef.Name != "api-0" {
			t.Errorf("globex saw %q", globex[0].ObjectRef.Name)
		}

		globexIDs := map[string]struct{}{}
		for _, id := range auditIDs(globex) {
			globexIDs[id] = struct{}{}
		}
		for _, id := range auditIDs(acme) {
			if _, shared := globexIDs[id]; shared {
				t.Errorf("audit %s is visible to both projects", id)
			}
		}

		requireNoDownstreamNamespace(t, acme)
		requireNoDownstreamNamespace(t, globex)
	})

	t.Run("records are queryable by location", func(t *testing.T) {
		matched := h.query(t, platformScope(), fmt.Sprintf("location == '%s'", locationDFW))
		if len(matched) != 5 {
			t.Fatalf("location filter returned %d events (%v), want 5", len(matched), auditIDs(matched))
		}

		unmatched := h.query(t, platformScope(), "location == 'somewhere-else'")
		if len(unmatched) != 0 {
			t.Fatalf("a record was stored under a location the shipper claimed: %v", auditIDs(unmatched))
		}

		scoped := h.query(t, projectScope(acmeProject), fmt.Sprintf("location == '%s'", locationDFW))
		if len(scoped) != 2 {
			t.Fatalf("scoped location filter returned %d events (%v), want 2", len(scoped), auditIDs(scoped))
		}
	})

	t.Run("location is offered as a facet", func(t *testing.T) {
		if !storage.IsValidAuditLogFacetField("location") {
			t.Fatal("location is not a supported audit log facet field")
		}

		result, err := h.store.QueryAuditLogFacets(context.Background(), storage.AuditLogFacetQuerySpec{
			StartTime: queryStartTime,
			EndTime:   queryEndTime,
			Facets:    []storage.FacetFieldSpec{{Field: "location", Limit: 10}},
		}, platformScope())
		if err != nil {
			t.Fatalf("facet query failed: %v", err)
		}

		if len(result.Facets) != 1 {
			t.Fatalf("returned %d facets, want 1", len(result.Facets))
		}

		values := map[string]int64{}
		for _, value := range result.Facets[0].Values {
			values[value.Value] = value.Count
		}
		if values[locationDFW] != 5 {
			t.Errorf("facet counts = %v, want %s to be 5", values, locationDFW)
		}
	})
}

func TestSchemaFixtureMatchesMigration(t *testing.T) {
	fixture, err := readTestdata("schema.sql")
	if err != nil {
		t.Fatalf("failed reading schema fixture: %v", err)
	}

	migration, err := os.ReadFile(filepath.Join("..", "..", "..", "migrations", "010_audit_logs_location.sql"))
	if err != nil {
		t.Fatalf("failed reading migration: %v", err)
	}

	expression := "JSONExtractString(event_json, 'annotations', 'locations.miloapis.com/location')"
	if !strings.Contains(string(migration), expression) {
		t.Fatalf("migration no longer materializes location with %s", expression)
	}
	if !strings.Contains(string(fixture), expression) {
		t.Fatalf("schema fixture has drifted from the migration")
	}
}
