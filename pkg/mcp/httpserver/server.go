// Package httpserver serves the Activity MCP tools over Streamable HTTP for
// Datum's assistant (Patch), alongside the documents it reads before calling
// them:
//
//	POST /mcp                  Streamable HTTP MCP, stateless
//	GET  /llms-full.txt        Knowledge: the Activity model and tools
//	GET  /runbooks/<name>.md   Skills: investigation procedures
//	GET  /healthz              Liveness
//
// The server holds no credential of its own. Each request is served by a
// fresh MCP server whose tools read through a client built from the caller's
// bearer token and pointed at the project named in the X-Datum-Project
// header, via Milo. A tool call therefore never sees more than the person who
// asked, Milo and Activity remain the only enforcement points, and a request
// with a missing or invalid project fails before any API request is made.
package httpserver

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"k8s.io/client-go/rest"
	"k8s.io/klog/v2"

	"go.miloapis.com/activity/pkg/mcp/tools"
)

const (
	// MCPPath serves the Streamable HTTP MCP endpoint.
	MCPPath = "/mcp"

	// HealthzPath serves liveness.
	HealthzPath = "/healthz"

	readHeaderTimeout = 10 * time.Second
	readTimeout       = 30 * time.Second
	idleTimeout       = 120 * time.Second
	shutdownTimeout   = 25 * time.Second // inside the default 30s termination grace period

	// maxRequestBytes caps an /mcp request body. Tool arguments are a few
	// short strings; anything near this is not a legitimate call.
	maxRequestBytes = 1 << 20
)

// ExposedTools is the complete set of tools served over HTTP.
//
// Raw audit log tools (query_audit_logs, get_audit_log_facets) are left out:
// audit records carry request and response bodies and are an operator's view,
// not a project member's. Policy tools (list_activity_policies,
// preview_activity_policy) are left out because ActivityPolicies are
// platform-managed translation rules, not project data. find_failed_operations
// does read audit logs, but returns only a fixed, summarised set of fields.
var ExposedTools = []string{
	tools.ToolQueryActivities,
	tools.ToolGetActivityFacets,
	tools.ToolGetResourceHistory,
	tools.ToolSummarizeRecentActivity,
	tools.ToolGetActivityTimeline,
	tools.ToolCompareActivityPeriods,
	tools.ToolFindFailedOperations,
	tools.ToolGetUserActivitySummary,
	tools.ToolQueryEvents,
	tools.ToolGetEventFacets,
}

// Options configures the HTTP handler.
type Options struct {
	// BaseConfig supplies the Milo API endpoint and CA. Any credentials it
	// carries are ignored. It must pass CheckBaseConfig.
	BaseConfig *rest.Config

	// Name and Version identify the server to MCP clients.
	Name    string
	Version string
}

// NewHandler returns the mux serving every route of the HTTP mode.
func NewHandler(opts Options) (http.Handler, error) {
	if err := CheckBaseConfig(opts.BaseConfig); err != nil {
		return nil, err
	}
	if opts.Name == "" {
		opts.Name = "activity"
	}

	docs, err := newKnowledgeHandler()
	if err != nil {
		return nil, err
	}

	mux := http.NewServeMux()
	mux.Handle(MCPPath, http.MaxBytesHandler(newMCPHandler(opts), maxRequestBytes))
	mux.Handle(knowledgePath, docs)
	mux.Handle(runbookPrefix, docs)
	mux.HandleFunc(HealthzPath, func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = fmt.Fprintln(w, "ok")
	})
	return mux, nil
}

// newMCPHandler returns the stateless Streamable HTTP handler. Every request
// gets its own MCP server bound to that caller's identity and project, so
// nothing is shared between callers.
func newMCPHandler(opts Options) http.Handler {
	return mcp.NewStreamableHTTPHandler(
		func(r *http.Request) *mcp.Server {
			s := mcp.NewServer(&mcp.Implementation{
				Name:    opts.Name,
				Version: opts.Version,
			}, nil)
			// A panic in one caller's tool call must not take the process,
			// and every other caller's request, down with it.
			s.AddReceivingMiddleware(tools.RecoverMiddleware)
			provider := tools.NewToolProviderWithResolver(ResolverFromRequest(r, opts.BaseConfig))
			provider.RegisterToolsWithOptions(s, tools.RegisterOptions{Tools: ExposedTools})
			return s
		},
		// Stateless: the tools are read-only and need no session, and it
		// keeps the server robust against client restarts and replica changes.
		&mcp.StreamableHTTPOptions{Stateless: true},
	)
}

// Serve listens on addr and serves handler until ctx is cancelled, then shuts
// down gracefully.
func Serve(ctx context.Context, addr string, handler http.Handler) error {
	ln, err := net.Listen("tcp", addr)
	if err != nil {
		return fmt.Errorf("listening on %s: %w", addr, err)
	}
	return serve(ctx, ln, handler)
}

// serve serves handler on ln until ctx is cancelled, then drains in-flight
// requests for up to shutdownTimeout.
func serve(ctx context.Context, ln net.Listener, handler http.Handler) error {
	addr := ln.Addr().String()
	server := &http.Server{
		Handler:           handler,
		ReadHeaderTimeout: readHeaderTimeout,
		ReadTimeout:       readTimeout,
		IdleTimeout:       idleTimeout,
		// Request contexts carry ctx's values but not its cancellation: on
		// SIGTERM, Shutdown stops accepting and lets in-flight tool calls
		// finish instead of cancelling them mid-query.
		BaseContext: func(net.Listener) context.Context { return context.WithoutCancel(ctx) },
	}

	errCh := make(chan error, 1)
	go func() {
		klog.InfoS("Serving Activity MCP over HTTP", "addr", addr, "mcp", MCPPath,
			"knowledge", knowledgePath, "runbooks", runbookPrefix, "tools", ExposedTools)
		errCh <- server.Serve(ln)
	}()

	select {
	case err := <-errCh:
		if err != nil && !errors.Is(err, http.ErrServerClosed) {
			return fmt.Errorf("serving on %s: %w", addr, err)
		}
		return nil
	case <-ctx.Done():
		shutdownCtx, cancel := context.WithTimeout(context.Background(), shutdownTimeout)
		defer cancel()
		if err := server.Shutdown(shutdownCtx); err != nil {
			return fmt.Errorf("shutting down: %w", err)
		}
		return nil
	}
}
