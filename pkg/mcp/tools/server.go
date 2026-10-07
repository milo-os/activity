package tools

import (
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// ServerConfig contains configuration for creating an MCP server.
type ServerConfig struct {
	// Name is the server name reported to clients.
	Name string

	// Version is the server version reported to clients.
	Version string
}

// NewMCPServer creates an MCP server with all activity tools registered.
func (p *ToolProvider) NewMCPServer(cfg ServerConfig) *mcp.Server {
	if cfg.Name == "" {
		cfg.Name = "activity"
	}
	if cfg.Version == "" {
		cfg.Version = "1.0.0"
	}

	server := mcp.NewServer(
		&mcp.Implementation{
			Name:    cfg.Name,
			Version: cfg.Version,
		},
		nil,
	)

	server.AddReceivingMiddleware(RecoverMiddleware)

	// Register all tools
	p.RegisterTools(server)

	return server
}
