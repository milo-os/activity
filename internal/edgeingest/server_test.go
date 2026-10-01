package edgeingest

import (
	"bytes"
	"context"
	"encoding/json"
	"net"
	"net/http"
	"testing"

	auditv1 "k8s.io/apiserver/pkg/apis/audit/v1"

	"go.miloapis.com/activity/internal/testpki"
)

// testServer starts a real mTLS ingest listener and returns its base URL.
func testServer(t *testing.T, p *testpki.PKI, registry *ClusterRegistry, pipeline *Pipeline, resolver UpstreamNamespaceResolver) string {
	t.Helper()

	server, err := NewServer(ServerConfig{
		Address:      "127.0.0.1:0",
		TLSCertFile:  p.ServingCertFile,
		TLSKeyFile:   p.ServingKeyFile,
		ClientCAFile: p.CAFile,
	}, registry, pipeline, resolver)
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
			t.Errorf("server returned %v", err)
		}
	}()

	t.Cleanup(func() {
		cancel()
		<-done
	})

	return "https://" + listener.Addr().String()
}

func postBatch(t *testing.T, client *http.Client, baseURL string, events ...*auditv1.Event) *http.Response {
	t.Helper()

	list := auditv1.EventList{}
	for _, event := range events {
		list.Items = append(list.Items, *event)
	}

	body, err := json.Marshal(list)
	if err != nil {
		t.Fatalf("failed encoding batch: %v", err)
	}

	response, err := client.Post(baseURL+AuditPath, "application/json", bytes.NewReader(body))
	if err != nil {
		t.Fatalf("failed posting batch: %v", err)
	}
	return response
}

func registryForTest() *ClusterRegistry {
	return NewClusterRegistry(map[string]ClusterIdentity{
		"audit-shipper.dfw1.edge.datum.net": {Name: "dfw1", Location: "us-central-1"},
	})
}

func TestServerIdentifiesClusterFromCertificate(t *testing.T) {
	p := testpki.New(t)
	index := syncedIndex()
	index.Upsert(downstreamNS, UpstreamNamespaceRef{ClusterName: "acme-prod", Namespace: "default"})

	pipeline, emitter := newPipeline(index)
	baseURL := testServer(t, p, registryForTest(), pipeline, index)

	response := postBatch(t, p.Client(t, "audit-shipper.dfw1.edge.datum.net"), baseURL, edgeEvent(t))
	defer response.Body.Close()

	if response.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", response.StatusCode)
	}
	if len(emitter.clusters) != 1 || emitter.clusters[0] != "dfw1" {
		t.Fatalf("emitted for clusters %v, want [dfw1]", emitter.clusters)
	}
	if got := emitter.events[0].Annotations[LocationAnnotation]; got != "us-central-1" {
		t.Errorf("location = %q, want us-central-1", got)
	}
}

func TestServerRejectsUnregisteredClient(t *testing.T) {
	p := testpki.New(t)
	index := syncedIndex()
	pipeline, emitter := newPipeline(index)
	baseURL := testServer(t, p, registryForTest(), pipeline, index)

	response := postBatch(t, p.Client(t, "attacker.example.com"), baseURL, edgeEvent(t))
	defer response.Body.Close()

	if response.StatusCode != http.StatusForbidden {
		t.Fatalf("status = %d, want 403", response.StatusCode)
	}
	if len(emitter.events) != 0 {
		t.Fatal("an unregistered client had records accepted")
	}
}

func TestServerRequiresClientCertificate(t *testing.T) {
	p := testpki.New(t)
	index := syncedIndex()
	pipeline, _ := newPipeline(index)
	baseURL := testServer(t, p, registryForTest(), pipeline, index)

	if _, err := p.AnonymousClient().Post(baseURL+AuditPath, "application/json", bytes.NewReader([]byte("{}"))); err == nil {
		t.Fatal("a client with no certificate completed a request")
	}
}

func TestServerReadinessGatesOnCacheSync(t *testing.T) {
	p := testpki.New(t)

	index := NewNamespaceIndex()
	synced := false
	index.AddSyncedFunc(func() bool { return synced })

	pipeline, _ := newPipeline(index)
	baseURL := testServer(t, p, registryForTest(), pipeline, index)
	client := p.Client(t, "audit-shipper.dfw1.edge.datum.net")

	response, err := client.Get(baseURL + "/readyz")
	if err != nil {
		t.Fatalf("readyz failed: %v", err)
	}
	response.Body.Close()
	if response.StatusCode != http.StatusServiceUnavailable {
		t.Fatalf("cold cache readyz = %d, want 503", response.StatusCode)
	}

	// healthz stays green: the process is alive, it just must not be routed to.
	response, err = client.Get(baseURL + "/healthz")
	if err != nil {
		t.Fatalf("healthz failed: %v", err)
	}
	response.Body.Close()
	if response.StatusCode != http.StatusOK {
		t.Fatalf("healthz = %d, want 200", response.StatusCode)
	}

	synced = true
	response, err = client.Get(baseURL + "/readyz")
	if err != nil {
		t.Fatalf("readyz failed: %v", err)
	}
	response.Body.Close()
	if response.StatusCode != http.StatusOK {
		t.Fatalf("warm cache readyz = %d, want 200", response.StatusCode)
	}
}

func TestServerAsksForRetryOnColdCache(t *testing.T) {
	p := testpki.New(t)

	index := NewNamespaceIndex()
	index.AddSyncedFunc(func() bool { return false })

	pipeline, emitter := newPipeline(index)
	baseURL := testServer(t, p, registryForTest(), pipeline, index)

	response := postBatch(t, p.Client(t, "audit-shipper.dfw1.edge.datum.net"), baseURL, edgeEvent(t))
	defer response.Body.Close()

	if response.StatusCode != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want 503", response.StatusCode)
	}
	if response.Header.Get("Retry-After") == "" {
		t.Error("no Retry-After header on a retryable rejection")
	}
	if len(emitter.events) != 0 {
		t.Fatal("a record was emitted against a cold cache")
	}
}

func TestServerDropsUnresolvableRecordsAndKeepsTheRest(t *testing.T) {
	p := testpki.New(t)
	index := syncedIndex()
	index.Upsert(downstreamNS, UpstreamNamespaceRef{ClusterName: "acme-prod", Namespace: "default"})

	pipeline, emitter := newPipeline(index)
	baseURL := testServer(t, p, registryForTest(), pipeline, index)

	unresolvable := edgeEvent(t)
	unresolvable.AuditID = "unresolvable-1"
	unresolvable.RequestURI = "/api/v1/namespaces/" + otherNS + "/pods"
	unresolvable.ObjectRef = &auditv1.ObjectReference{Resource: "pods", Namespace: otherNS}
	unresolvable.RequestObject = nil
	unresolvable.ResponseObject = nil

	response := postBatch(t, p.Client(t, "audit-shipper.dfw1.edge.datum.net"), baseURL, edgeEvent(t), unresolvable)
	defer response.Body.Close()

	if response.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", response.StatusCode)
	}

	var result ingestResult
	if err := json.NewDecoder(response.Body).Decode(&result); err != nil {
		t.Fatalf("failed decoding result: %v", err)
	}
	if result.Received != 2 || result.Emitted != 1 || result.Dropped != 1 {
		t.Fatalf("result = %+v, want received 2 emitted 1 dropped 1", result)
	}

	for _, emitted := range emitter.events {
		encoded, err := json.Marshal(emitted)
		if err != nil {
			t.Fatalf("failed encoding emitted event: %v", err)
		}
		if downstreamNamespacePattern.Match(encoded) {
			t.Errorf("emitted event leaks a downstream namespace: %s", encoded)
		}
	}
}

func TestServerRejectsNonPostRequests(t *testing.T) {
	p := testpki.New(t)
	index := syncedIndex()
	pipeline, _ := newPipeline(index)
	baseURL := testServer(t, p, registryForTest(), pipeline, index)

	response, err := p.Client(t, "audit-shipper.dfw1.edge.datum.net").Get(baseURL + AuditPath)
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	defer response.Body.Close()

	if response.StatusCode != http.StatusMethodNotAllowed {
		t.Fatalf("status = %d, want 405", response.StatusCode)
	}
}

func TestNewServerRequiresClientCA(t *testing.T) {
	p := testpki.New(t)
	index := syncedIndex()
	pipeline, _ := newPipeline(index)

	_, err := NewServer(ServerConfig{
		Address:     "127.0.0.1:0",
		TLSCertFile: p.ServingCertFile,
		TLSKeyFile:  p.ServingKeyFile,
	}, registryForTest(), pipeline, index)

	if err == nil {
		t.Fatal("a listener with no client CA was accepted")
	}
	if want := "--client-ca-file is required"; !bytes.Contains([]byte(err.Error()), []byte(want)) {
		t.Errorf("err = %v, want it to mention %q", err, want)
	}
}
