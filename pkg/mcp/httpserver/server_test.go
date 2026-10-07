package httpserver

import (
	"context"
	"io"
	"io/fs"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"k8s.io/client-go/rest"
	"sigs.k8s.io/yaml"

	"go.miloapis.com/activity/pkg/mcp/agentdocs"
	"go.miloapis.com/activity/pkg/mcp/tools"
)

// stubAPI stands in for Milo. It records every request it receives, so a
// test can assert both where a read went and that none was made at all.
type stubAPI struct {
	*httptest.Server
	count atomic.Int32

	mu    sync.Mutex
	paths []string
	auths []string
}

func newStubAPI(t *testing.T) *stubAPI {
	t.Helper()
	s := &stubAPI{}
	s.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		s.count.Add(1)
		s.mu.Lock()
		s.paths = append(s.paths, r.URL.Path)
		s.auths = append(s.auths, r.Header.Get("Authorization"))
		s.mu.Unlock()

		w.Header().Set("Content-Type", "application/json")
		kind, status := "ActivityQuery", `{}`
		switch {
		case strings.HasSuffix(r.URL.Path, "/eventqueries"):
			kind = "EventQuery"
		case strings.HasSuffix(r.URL.Path, "/auditlogqueries"):
			// A failed non-resource request: no objectRef, and in the
			// second record no responseStatus either.
			kind = "AuditLogQuery"
			status = `{"results":[` +
				`{"level":"Metadata","auditID":"a1","stage":"ResponseComplete","verb":"get",` +
				`"requestURI":"/apis/foo/v1","user":{"username":"alice"},` +
				`"responseStatus":{"metadata":{},"code":404}},` +
				`{"level":"Metadata","auditID":"a2","stage":"ResponseComplete","verb":"get",` +
				`"requestURI":"/healthz","user":{"username":"alice"}}]}`
		}
		_, _ = io.WriteString(w, `{"apiVersion":"activity.miloapis.com/v1alpha1","kind":"`+kind+`",`+
			`"metadata":{"name":"q"},"spec":{"startTime":"now-1h","endTime":"now"},"status":`+status+`}`)
	}))
	t.Cleanup(s.Close)
	return s
}

// headerTransport adds fixed headers to every request the MCP client sends,
// standing in for Patch forwarding the user's token and project.
type headerTransport struct {
	headers map[string]string
}

func (h headerTransport) RoundTrip(r *http.Request) (*http.Response, error) {
	r = r.Clone(r.Context())
	for k, v := range h.headers {
		r.Header.Set(k, v)
	}
	return http.DefaultTransport.RoundTrip(r)
}

// newTestServer serves the full HTTP mode mux, reading from api.
func newTestServer(t *testing.T, api *stubAPI) *httptest.Server {
	t.Helper()
	t.Setenv("KUBERNETES_SERVICE_HOST", "")
	t.Setenv("KUBERNETES_SERVICE_PORT", "")

	handler, err := NewHandler(Options{
		BaseConfig: &rest.Config{Host: api.URL, BearerToken: "server-token"},
		Name:       "activity-test",
		Version:    "test",
	})
	if err != nil {
		t.Fatalf("NewHandler: %v", err)
	}
	srv := httptest.NewServer(handler)
	t.Cleanup(srv.Close)
	return srv
}

// connect opens an MCP session against srv, sending headers on every request.
func connect(t *testing.T, srv *httptest.Server, headers map[string]string) *mcp.ClientSession {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	t.Cleanup(cancel)

	client := mcp.NewClient(&mcp.Implementation{Name: "test-client", Version: "test"}, nil)
	session, err := client.Connect(ctx, &mcp.StreamableClientTransport{
		Endpoint:   srv.URL + MCPPath,
		HTTPClient: &http.Client{Transport: headerTransport{headers: headers}},
	}, nil)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	t.Cleanup(func() { _ = session.Close() })
	return session
}

func callTool(t *testing.T, session *mcp.ClientSession, name string, args map[string]any) *mcp.CallToolResult {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	res, err := session.CallTool(ctx, &mcp.CallToolParams{Name: name, Arguments: args})
	if err != nil {
		t.Fatalf("CallTool(%s): %v", name, err)
	}
	return res
}

func resultText(res *mcp.CallToolResult) string {
	var b strings.Builder
	for _, c := range res.Content {
		if tc, ok := c.(*mcp.TextContent); ok {
			b.WriteString(tc.Text)
		}
	}
	return b.String()
}

// TestHTTPModeExposesExactlyTheAllowedTools pins the HTTP tool surface: the
// ten read-only activity, investigation, analytics and event tools, and no
// raw audit log or policy tool.
func TestHTTPModeExposesExactlyTheAllowedTools(t *testing.T) {
	api := newStubAPI(t)
	session := connect(t, newTestServer(t, api), nil)

	res, err := session.ListTools(context.Background(), nil)
	if err != nil {
		t.Fatalf("ListTools: %v", err)
	}

	got := make([]string, 0, len(res.Tools))
	for _, tool := range res.Tools {
		got = append(got, tool.Name)
		if tool.Annotations == nil || !tool.Annotations.ReadOnlyHint {
			t.Errorf("tool %s is not annotated read-only", tool.Name)
		}
	}
	sort.Strings(got)

	want := []string{
		"compare_activity_periods",
		"find_failed_operations",
		"get_activity_facets",
		"get_activity_timeline",
		"get_event_facets",
		"get_resource_history",
		"get_user_activity_summary",
		"query_activities",
		"query_events",
		"summarize_recent_activity",
	}
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Errorf("tools = %v, want %v", got, want)
	}

	// The capability document Patch reads must approve exactly what is
	// served: a tool served but not included is dead weight, and one included
	// but not served is a dead end.
	include := append([]string(nil), loadCapability(t).toolInclude()...)
	sort.Strings(include)
	if strings.Join(include, ",") != strings.Join(got, ",") {
		t.Errorf("%s toolSelector.include = %v, want the served tools %v", capabilityFile, include, got)
	}

	for _, banned := range []string{
		tools.ToolQueryAuditLogs,
		tools.ToolGetAuditLogFacets,
		tools.ToolListActivityPolicies,
		tools.ToolPreviewActivityPolicy,
	} {
		for _, name := range got {
			if name == banned {
				t.Errorf("HTTP mode exposes %s", banned)
			}
		}
	}

	// Listing tools is not a read: nothing may have reached the API.
	if n := api.count.Load(); n != 0 {
		t.Errorf("listing tools made %d API requests, want 0", n)
	}
}

// TestRequestsWithoutIdentityOrProjectNeverReachTheAPI is the scoping
// guarantee: Activity falls back to platform scope when a request carries no
// project, so a request missing the token or project, or naming an invalid
// one, must fail as a tool error before any API request is made.
func TestRequestsWithoutIdentityOrProjectNeverReachTheAPI(t *testing.T) {
	tests := []struct {
		name    string
		headers map[string]string
		want    string
	}{
		{name: "no token", headers: map[string]string{ProjectHeader: "my-project"}, want: "no credentials"},
		{name: "no project", headers: map[string]string{"Authorization": "Bearer " + testToken}, want: "no project"},
		{
			name:    "invalid project",
			headers: map[string]string{"Authorization": "Bearer " + testToken, ProjectHeader: "../../api/v1"},
			want:    "invalid project",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			api := newStubAPI(t)
			session := connect(t, newTestServer(t, api), tc.headers)

			for _, call := range []struct {
				tool string
				args map[string]any
			}{
				{tools.ToolQueryActivities, map[string]any{"startTime": "now-1h", "endTime": "now"}},
				{tools.ToolSummarizeRecentActivity, map[string]any{"startTime": "now-1h"}},
				{tools.ToolFindFailedOperations, map[string]any{"startTime": "now-1h"}},
				{tools.ToolQueryEvents, map[string]any{"startTime": "now-1h", "endTime": "now"}},
				{tools.ToolGetEventFacets, map[string]any{"fields": []string{"reason"}}},
			} {
				res := callTool(t, session, call.tool, call.args)
				if !res.IsError {
					t.Errorf("%s succeeded, want a tool error", call.tool)
				}
				if text := resultText(res); !strings.Contains(text, tc.want) {
					t.Errorf("%s error = %q, want it to mention %q", call.tool, text, tc.want)
				} else if strings.Contains(text, testToken) {
					t.Errorf("%s error leaks the bearer token", call.tool)
				}
			}

			if n := api.count.Load(); n != 0 {
				t.Errorf("API received %d requests, want 0", n)
			}
		})
	}
}

// TestReadsGoThroughTheProjectControlPlane drives a real tool call end to
// end: the read must land on the project's control-plane path, carrying the
// caller's token and never the server's.
func TestReadsGoThroughTheProjectControlPlane(t *testing.T) {
	api := newStubAPI(t)
	session := connect(t, newTestServer(t, api), map[string]string{
		"Authorization": "Bearer " + testToken,
		ProjectHeader:   "my-project",
	})

	for _, call := range []struct {
		tool     string
		args     map[string]any
		resource string
	}{
		{tools.ToolQueryActivities, map[string]any{"startTime": "now-1h", "endTime": "now"}, "activityqueries"},
		{tools.ToolQueryEvents, map[string]any{"startTime": "now-1h", "endTime": "now"}, "eventqueries"},
	} {
		res := callTool(t, session, call.tool, call.args)
		if res.IsError {
			t.Fatalf("%s: unexpected tool error: %s", call.tool, resultText(res))
		}
	}

	api.mu.Lock()
	defer api.mu.Unlock()
	if len(api.paths) != 2 {
		t.Fatalf("API received %d requests (%v), want 2", len(api.paths), api.paths)
	}
	prefix := "/apis/resourcemanager.miloapis.com/v1alpha1/projects/my-project/control-plane"
	for i, want := range []string{
		prefix + "/apis/activity.miloapis.com/v1alpha1/activityqueries",
		prefix + "/apis/activity.miloapis.com/v1alpha1/eventqueries",
	} {
		if api.paths[i] != want {
			t.Errorf("request %d path = %q, want %q", i, api.paths[i], want)
		}
		if api.auths[i] != "Bearer "+testToken {
			t.Errorf("request %d Authorization = %q, want the caller's token", i, api.auths[i])
		}
	}
}

// TestFailedNonResourceRequestDoesNotCrash is a regression test: an audit
// record with no objectRef (a 404 on a discovery path) used to panic the
// handler goroutine and kill the process.
func TestFailedNonResourceRequestDoesNotCrash(t *testing.T) {
	api := newStubAPI(t)
	session := connect(t, newTestServer(t, api), map[string]string{
		"Authorization": "Bearer " + testToken,
		ProjectHeader:   "my-project",
	})

	res := callTool(t, session, tools.ToolFindFailedOperations, map[string]any{"startTime": "now-1h"})
	if res.IsError {
		t.Fatalf("unexpected tool error: %s", resultText(res))
	}
	if text := resultText(res); !strings.Contains(text, "/apis/foo/v1") || !strings.Contains(text, `"count": 2`) {
		t.Errorf("result = %s, want both failures with the request URI", text)
	}

	// And the server is still alive for the next call.
	if _, err := session.ListTools(context.Background(), nil); err != nil {
		t.Errorf("ListTools after the call: %v", err)
	}
}

func TestMCPRequestBodyIsCapped(t *testing.T) {
	srv := newTestServer(t, newStubAPI(t))
	body := strings.NewReader(`{"jsonrpc":"2.0","id":1,"method":"tools/list","params":{"pad":"` +
		strings.Repeat("x", maxRequestBytes+1) + `"}}`)
	req, _ := http.NewRequest(http.MethodPost, srv.URL+MCPPath, body)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json, text/event-stream")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("POST: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode < 400 {
		t.Errorf("oversized request = %d, want a 4xx refusal", resp.StatusCode)
	}
}

// TestShutdownDrainsInFlightRequests: cancelling the serve context (SIGTERM)
// must let a request already in progress finish, not cancel it.
func TestShutdownDrainsInFlightRequests(t *testing.T) {
	started := make(chan struct{})
	release := make(chan struct{})
	var reqCtxErr error
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		close(started)
		<-release
		reqCtxErr = r.Context().Err()
		_, _ = io.WriteString(w, "done")
	})

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	serveErr := make(chan error, 1)
	go func() { serveErr <- serve(ctx, ln, handler) }()

	type result struct {
		body string
		err  error
	}
	got := make(chan result, 1)
	go func() {
		resp, err := http.Get("http://" + ln.Addr().String() + "/")
		if err != nil {
			got <- result{err: err}
			return
		}
		defer func() { _ = resp.Body.Close() }()
		b, err := io.ReadAll(resp.Body)
		got <- result{body: string(b), err: err}
	}()

	<-started
	cancel()
	time.Sleep(100 * time.Millisecond) // let Shutdown begin
	close(release)

	r := <-got
	if r.err != nil || r.body != "done" {
		t.Errorf("in-flight request = %q, %v; want it to complete", r.body, r.err)
	}
	if reqCtxErr != nil {
		t.Errorf("request context was cancelled during shutdown: %v", reqCtxErr)
	}
	if err := <-serveErr; err != nil {
		t.Errorf("serve: %v", err)
	}
}

func TestNewHandlerRefusesUnsafeBaseConfig(t *testing.T) {
	t.Setenv("KUBERNETES_SERVICE_HOST", "")
	t.Setenv("KUBERNETES_SERVICE_PORT", "")
	if _, err := NewHandler(Options{BaseConfig: &rest.Config{Host: "https://api.example/some/path"}}); err == nil {
		t.Error("NewHandler accepted a base host with a path")
	}
	if _, err := NewHandler(Options{}); err == nil {
		t.Error("NewHandler accepted no base config")
	}
}

func TestHealthz(t *testing.T) {
	srv := newTestServer(t, newStubAPI(t))
	resp, err := http.Get(srv.URL + HealthzPath)
	if err != nil {
		t.Fatalf("GET %s: %v", HealthzPath, err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		t.Errorf("GET %s = %d, want 200", HealthzPath, resp.StatusCode)
	}
}

// ---------------------------------------------------------------------------
// Knowledge and skills
// ---------------------------------------------------------------------------

func get(t *testing.T, h http.Handler, method, target string) *httptest.ResponseRecorder {
	t.Helper()
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(method, target, nil))
	return rec
}

func testMux(t *testing.T) http.Handler {
	t.Helper()
	t.Setenv("KUBERNETES_SERVICE_HOST", "")
	t.Setenv("KUBERNETES_SERVICE_PORT", "")
	h, err := NewHandler(Options{BaseConfig: &rest.Config{Host: "https://api.datum.example"}})
	if err != nil {
		t.Fatalf("NewHandler: %v", err)
	}
	return h
}

// TestSkillsMatchDocumentedSet fails when a skill is added, renamed or removed
// without Patch's capability document being updated: each name is a URL.
func TestSkillsMatchDocumentedSet(t *testing.T) {
	docs, err := newKnowledgeHandler()
	if err != nil {
		t.Fatalf("newKnowledgeHandler: %v", err)
	}
	want := []string{
		"/llms-full.txt",
		"/runbooks/event-investigation.md",
		"/runbooks/failed-operation-triage.md",
		"/runbooks/what-changed.md",
		"/runbooks/who-changed-resource.md",
	}
	if got := docs.paths(); strings.Join(got, ",") != strings.Join(want, ",") {
		t.Errorf("served paths = %v, want %v", got, want)
	}

	// Every skill the capability document names must be an embedded runbook
	// at the URL it gives, and every embedded runbook must be named.
	served := map[string]bool{}
	for _, p := range docs.paths() {
		served[p] = true
	}
	listed := map[string]bool{}
	for _, skill := range loadCapability(t).Spec.Skills {
		p := runbookPrefix + skill.Name + ".md"
		listed[p] = true
		if !served[p] {
			t.Errorf("%s names skill %q, but %s is not embedded", capabilityFile, skill.Name, p)
		}
		if !strings.HasSuffix(skill.Source, p) {
			t.Errorf("%s skill %q source = %q, want it to end in %s", capabilityFile, skill.Name, skill.Source, p)
		}
	}
	for p := range served {
		if strings.HasPrefix(p, runbookPrefix) && !listed[p] {
			t.Errorf("runbook %s is embedded but not listed in %s", p, capabilityFile)
		}
	}
}

// capabilityFile is the ServiceAgentConfiguration Patch reads to learn what
// this server offers.
const capabilityFile = "../../../config/milo/assistant-capability/service-agent-configuration.yaml"

// capability is the subset of a ServiceAgentConfiguration these tests check.
type capability struct {
	Spec struct {
		Knowledge struct {
			Sources []struct {
				URL string `json:"url"`
			} `json:"sources"`
		} `json:"knowledge"`
		Tools struct {
			MCPServers []struct {
				Name         string `json:"name"`
				ToolSelector struct {
					Include []string `json:"include"`
				} `json:"toolSelector"`
			} `json:"mcpServers"`
		} `json:"tools"`
		Skills []struct {
			Name   string `json:"name"`
			Source string `json:"source"`
		} `json:"skills"`
	} `json:"spec"`
}

func (c capability) toolInclude() []string {
	var out []string
	for _, s := range c.Spec.Tools.MCPServers {
		out = append(out, s.ToolSelector.Include...)
	}
	return out
}

func loadCapability(t *testing.T) capability {
	t.Helper()
	raw, err := os.ReadFile(capabilityFile)
	if err != nil {
		t.Fatalf("reading %s: %v", capabilityFile, err)
	}
	var c capability
	if err := yaml.Unmarshal(raw, &c); err != nil {
		t.Fatalf("parsing %s: %v", capabilityFile, err)
	}
	if len(c.toolInclude()) == 0 || len(c.Spec.Skills) == 0 {
		t.Fatalf("%s lists no tools or skills; has its shape changed?", capabilityFile)
	}
	return c
}

func TestServesKnowledgeAndEverySkill(t *testing.T) {
	mux := testMux(t)

	rec := get(t, mux, http.MethodGet, knowledgePath)
	if rec.Code != http.StatusOK {
		t.Fatalf("GET %s = %d, want 200", knowledgePath, rec.Code)
	}
	if got := rec.Header().Get("Content-Type"); got != textContentType {
		t.Errorf("Content-Type = %q, want %q", got, textContentType)
	}
	want, _ := agentdocs.FS.ReadFile(agentdocs.KnowledgeFile)
	if rec.Body.String() != string(want) {
		t.Error("knowledge body does not match the embedded document")
	}

	entries, err := fs.ReadDir(agentdocs.FS, agentdocs.SkillsDir)
	if err != nil {
		t.Fatalf("reading skills: %v", err)
	}
	for _, entry := range entries {
		target := runbookPrefix + entry.Name()
		rec := get(t, mux, http.MethodGet, target)
		if rec.Code != http.StatusOK {
			t.Errorf("GET %s = %d, want 200", target, rec.Code)
			continue
		}
		if got := rec.Header().Get("Content-Type"); got != markdownContentType {
			t.Errorf("GET %s Content-Type = %q, want %q", target, got, markdownContentType)
		}
		want, _ := agentdocs.FS.ReadFile(path.Join(agentdocs.SkillsDir, entry.Name()))
		if rec.Body.String() != string(want) {
			t.Errorf("GET %s body does not match the embedded document", target)
		}
	}
}

// TestDocumentsReferenceOnlyExposedTools keeps the knowledge and skills honest:
// a document that steers the assistant toward a tool HTTP mode does not serve
// would send it to a dead end.
func TestDocumentsReferenceOnlyExposedTools(t *testing.T) {
	exposed := map[string]bool{}
	for _, name := range ExposedTools {
		exposed[name] = true
	}
	hidden := []string{
		tools.ToolQueryAuditLogs,
		tools.ToolGetAuditLogFacets,
		tools.ToolListActivityPolicies,
		tools.ToolPreviewActivityPolicy,
	}

	docs, err := newKnowledgeHandler()
	if err != nil {
		t.Fatalf("newKnowledgeHandler: %v", err)
	}
	for p, doc := range docs.docs {
		for _, name := range hidden {
			if strings.Contains(string(doc.body), name) {
				t.Errorf("%s mentions %s, which HTTP mode does not expose", p, name)
			}
		}
	}

	knowledge := string(docs.docs[knowledgePath].body)
	for name := range exposed {
		if !strings.Contains(knowledge, name) {
			t.Errorf("%s does not describe exposed tool %s", knowledgePath, name)
		}
	}
}

func TestMissingRunbookIs404(t *testing.T) {
	mux := testMux(t)
	for _, target := range []string{
		"/runbooks/does-not-exist.md",
		"/runbooks/",
		"/runbooks/what-changed",
		"/runbooks/skills/what-changed.md",
		"/runbooks/../embed.go",
	} {
		if rec := get(t, mux, http.MethodGet, target); rec.Code == http.StatusOK {
			t.Errorf("GET %s = 200, want a refusal", target)
		}
	}
}

func TestDocumentsRefuseMutatingMethods(t *testing.T) {
	mux := testMux(t)
	for _, method := range []string{http.MethodPost, http.MethodPut, http.MethodDelete, http.MethodPatch} {
		if rec := get(t, mux, method, knowledgePath); rec.Code != http.StatusMethodNotAllowed {
			t.Errorf("%s %s = %d, want 405", method, knowledgePath, rec.Code)
		}
	}
}
