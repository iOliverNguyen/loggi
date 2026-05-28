// Package mcp exposes loggi's read-only query surface over the Model
// Context Protocol (https://modelcontextprotocol.io). Tool handlers run
// in-process against a *server.Server, so the same registration is used
// for the daemon's /mcp HTTP endpoint and any in-process embedding.
//
// The companion `loggi mcp` CLI subcommand is a thin stdio↔HTTP proxy
// that forwards every JSON-RPC frame to the daemon's /mcp; see
// internal/app/mcp_cmd.go.
package mcp

import (
	mcpserver "github.com/mark3labs/mcp-go/server"

	"github.com/iOliverNguyen/loggi/internal/server"
)

// Version is reported in the initialize handshake.
const Version = "0.1.0"

// New returns a configured MCP server with every tool registered. The
// returned server can be passed to mcpserver.NewStreamableHTTPServer for
// HTTP transport or mcpserver.ServeStdio for stdio transport.
//
// All tool handlers borrow logsrv for the lifetime of each call but don't
// retain it. logsrv must outlive the MCP server (true by construction:
// both live for the daemon's lifetime).
func New(logsrv *server.Server) *mcpserver.MCPServer {
	s := mcpserver.NewMCPServer(
		"loggi",
		Version,
		mcpserver.WithToolCapabilities(false),
		mcpserver.WithInstructions(serverInstructions),
	)
	registerTools(s, logsrv)
	return s
}

const serverInstructions = `loggi is a local-first log viewer. The tools below let you query logs
captured by the running daemon: filter by a Datadog-style DSL, fetch
recent entries, draw histograms, and produce a clickable URL the user
can open in the loggi web UI to view the same data interactively.

Filter DSL examples:
  level:error                    — exact match on the level hot field
  service:api level:>=warn       — implicit AND between space-separated terms
  *timeout*                      — substring match on msg/text
  @latency_ms:>=500              — numeric compare on a JSON field
  /pattern/i                     — regex match
  level:error -service:health    — boolean: NOT health-service errors

After a useful search, call get_view_url with the same filter and
present the returned URL to the user so they can inspect interactively.`
