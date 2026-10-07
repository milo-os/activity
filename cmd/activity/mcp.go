package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/spf13/cobra"
	"github.com/spf13/pflag"

	"go.miloapis.com/activity/internal/version"
	"go.miloapis.com/activity/pkg/mcp/httpserver"
	"go.miloapis.com/activity/pkg/mcp/tools"
)

const (
	// mcpTransportStdio serves one local client over stdin/stdout, reading
	// with the kubeconfig's own credentials.
	mcpTransportStdio = "stdio"

	// mcpTransportHTTP serves Streamable HTTP for many callers, reading with
	// each caller's own bearer token and project.
	mcpTransportHTTP = "http"
)

// MCPServerOptions contains configuration for the MCP server.
type MCPServerOptions struct {
	// Kubernetes client configuration
	Kubeconfig string
	Context    string
	Namespace  string

	// Transport selects stdio (default) or http.
	Transport string

	// Addr is the listen address in http mode.
	Addr string
}

// NewMCPServerOptions creates options with default values.
func NewMCPServerOptions() *MCPServerOptions {
	return &MCPServerOptions{
		Namespace: "default",
		Transport: mcpTransportStdio,
		Addr:      ":8080",
	}
}

// AddFlags adds MCP server flags to the flag set.
func (o *MCPServerOptions) AddFlags(fs *pflag.FlagSet) {
	fs.StringVar(&o.Kubeconfig, "kubeconfig", o.Kubeconfig,
		"Path to kubeconfig file. In stdio mode, if not set, uses in-cluster config or the default kubeconfig "+
			"location (~/.kube/config). In http mode it supplies only the Milo API server address and CA; "+
			"in-cluster config is never used")
	fs.StringVar(&o.Context, "context", o.Context,
		"Kubeconfig context to use. If not set, uses the current context")
	fs.StringVar(&o.Namespace, "namespace", o.Namespace,
		"Namespace for namespaced resources like Activities (default: 'default')")
	fs.StringVar(&o.Transport, "transport", o.Transport,
		"MCP transport: 'stdio' for a local client, or 'http' to serve Streamable HTTP for the Patch assistant")
	fs.StringVar(&o.Addr, "addr", o.Addr,
		"Listen address in http mode")
}

// Validate checks the options.
func (o *MCPServerOptions) Validate() error {
	switch o.Transport {
	case mcpTransportStdio, mcpTransportHTTP:
		return nil
	default:
		return fmt.Errorf("invalid --transport %q: must be %q or %q", o.Transport, mcpTransportStdio, mcpTransportHTTP)
	}
}

// NewMCPCommand creates the mcp subcommand that starts the MCP server.
func NewMCPCommand() *cobra.Command {
	options := NewMCPServerOptions()

	cmd := &cobra.Command{
		Use:   "mcp",
		Short: "Start the MCP server for AI tool integration",
		Long: `Start an MCP (Model Context Protocol) server that exposes audit log
and activity query tools for AI assistants.

Transports:

  --transport=stdio (default)
    Communicates via stdio and can be connected to Claude Desktop, VS Code
    extensions, or other MCP-compatible clients. Reads the Activity API with
    the kubeconfig's own credentials, so it requires a valid kubeconfig with
    access to the Activity API resources. Registers every tool below.

  --transport=http
    Serves Streamable HTTP (stateless) for Datum's assistant, Patch:
      POST /mcp                  MCP endpoint
      GET  /llms-full.txt        knowledge document for the assistant
      GET  /runbooks/<name>.md   investigation skills
      GET  /healthz              liveness
    Each request must carry "Authorization: Bearer <token>" and
    "X-Datum-Project: <project>". Tools read as that caller, through Milo, at
    the project's control plane, so results are scoped to that one project.
    The kubeconfig supplies only the Milo API server address and CA; its
    credentials are never used, and in-cluster config is refused.
    Only the activity, investigation, analytics and event tools are exposed:
    raw audit log and policy tools are not.

Available tools:

  Audit Log Tools:
    - query_audit_logs: Search audit logs with CEL filters
    - get_audit_log_facets: Get distinct values for audit log fields

  Activity Tools (human-readable summaries):
    - query_activities: Search human-readable activity summaries
    - get_activity_facets: Get distinct values for activity fields

  Investigation Tools:
    - find_failed_operations: Find operations that failed (4xx/5xx)
    - get_resource_history: Get change history for a specific resource
    - get_user_activity_summary: Get a user's recent actions

  Analytics Tools:
    - get_activity_timeline: Activity counts grouped by time buckets
    - summarize_recent_activity: Summary with top actors and resources
    - compare_activity_periods: Compare activity between time periods

  Policy Tools:
    - list_activity_policies: List configured ActivityPolicies
    - preview_activity_policy: Test a policy against sample inputs

Example configuration for Claude Desktop (claude_desktop_config.json):
  {
    "mcpServers": {
      "activity": {
        "command": "activity",
        "args": ["mcp", "--kubeconfig", "~/.kube/config"]
      }
    }
  }`,
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := options.Validate(); err != nil {
				return err
			}
			if options.Transport == mcpTransportHTTP {
				return RunMCPHTTPServer(options)
			}
			return RunMCPServer(options)
		},
	}

	flags := cmd.Flags()
	options.AddFlags(flags)

	return cmd
}

// RunMCPServer starts the MCP server with the given options.
func RunMCPServer(options *MCPServerOptions) error {
	// Create tool provider
	cfg := tools.Config{
		Kubeconfig: options.Kubeconfig,
		Context:    options.Context,
		Namespace:  options.Namespace,
	}

	provider, err := tools.NewToolProvider(cfg)
	if err != nil {
		return fmt.Errorf("failed to create tool provider: %w", err)
	}
	defer provider.Close()

	// Create MCP server
	mcpServer := provider.NewMCPServer(tools.ServerConfig{
		Name:    "activity",
		Version: version.Version,
	})

	// Start server on stdio
	fmt.Fprintln(os.Stderr, "Starting Activity MCP server...")
	if options.Kubeconfig != "" {
		fmt.Fprintln(os.Stderr, "Using kubeconfig:", options.Kubeconfig)
	} else {
		fmt.Fprintln(os.Stderr, "Using default kubeconfig")
	}
	if options.Context != "" {
		fmt.Fprintln(os.Stderr, "Using context:", options.Context)
	}

	return mcpServer.Run(context.Background(), &mcp.StdioTransport{})
}

// RunMCPHTTPServer serves the MCP tools over Streamable HTTP until SIGINT or
// SIGTERM.
func RunMCPHTTPServer(options *MCPServerOptions) error {
	baseConfig, err := httpserver.LoadBaseConfig(options.Kubeconfig, options.Context)
	if err != nil {
		return err
	}

	handler, err := httpserver.NewHandler(httpserver.Options{
		BaseConfig: baseConfig,
		Name:       "activity",
		Version:    version.Version,
	})
	if err != nil {
		return fmt.Errorf("refusing to start: %w", err)
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	return httpserver.Serve(ctx, options.Addr, handler)
}
