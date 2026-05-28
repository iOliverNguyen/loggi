package app

import (
	"bufio"
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"

	"github.com/spf13/cobra"
)

// NewMCPCmd is `loggi mcp` — a Model Context Protocol server over stdio.
//
// The subcommand is a thin proxy: it reads newline-delimited JSON-RPC
// frames from stdin, forwards each to the running daemon's /mcp endpoint
// (auto-starting it if absent), and writes responses to stdout. All tool
// logic lives in the daemon (see internal/mcp); the proxy is here so MCP
// clients that only speak stdio (Claude Desktop's default) can still use
// loggi without configuring an HTTP URL.
//
// Configure stdio MCP clients with:
//
//	{ "command": "loggi", "args": ["mcp"] }
func NewMCPCmd() *cobra.Command {
	var host string
	cmd := &cobra.Command{
		Use:   "mcp",
		Short: "Run a Model Context Protocol (MCP) server over stdio",
		Long: `Run an MCP server over stdio that proxies tool calls to the loggi
daemon's /mcp HTTP endpoint. Auto-starts the daemon if it isn't
already running.

Configure stdio MCP clients (e.g. Claude Desktop, Claude Code) with:
  { "command": "loggi", "args": ["mcp"] }

The same tools are also available over HTTP at <daemon-url>/mcp for
clients that prefer the Streamable HTTP transport directly.`,
		RunE: func(_ *cobra.Command, _ []string) error {
			return runMCPProxy(host)
		},
	}
	cmd.Flags().StringVar(&host, "host", "", "daemon URL (default: read from runtime.json, auto-starts if absent)")
	return cmd
}

// runMCPProxy reads JSON-RPC frames from stdin, forwards each to <base>/mcp,
// and writes responses to stdout. Each iteration is independent — if a single
// frame fails, we synthesize a JSON-RPC error reply and continue.
func runMCPProxy(host string) error {
	base, err := resolveMCPDaemonURL(host)
	if err != nil {
		return err
	}
	endpoint := strings.TrimRight(base, "/") + "/mcp"

	// No client-side timeout: tool calls (e.g. histogram over a wide
	// window) can legitimately take a while, and the daemon is local.
	httpc := &http.Client{}

	in := bufio.NewReaderSize(os.Stdin, 1<<20)
	out := bufio.NewWriter(os.Stdout)
	defer out.Flush()

	var sessionID string
	for {
		line, err := in.ReadBytes('\n')
		trimmed := bytes.TrimSpace(line)
		if len(trimmed) > 0 {
			if ferr := forwardFrame(httpc, endpoint, &sessionID, trimmed, out); ferr != nil {
				writeRPCError(out, trimmed, ferr)
			}
			_ = out.Flush()
		}
		if err == io.EOF {
			return nil
		}
		if err != nil {
			return err
		}
	}
}

// resolveMCPDaemonURL returns the daemon's HTTP URL, auto-starting it if
// not already running. Honors --host when explicitly set.
func resolveMCPDaemonURL(hostFlag string) (string, error) {
	if hostFlag != "" {
		return hostFlag, nil
	}
	// ensureServer dials the unix socket (auto-starting via flock-guarded
	// spawn if needed) and returns the HTTP URL once runtime.json is
	// written. Reused so we get the same auto-start semantics as `loggi
	// tail`.
	url, err := ensureServer()
	if err != nil {
		return "", fmt.Errorf("loggi server: %w", err)
	}
	if url == "" {
		return "", errors.New("loggi server is running but did not publish its HTTP URL")
	}
	return url, nil
}

// forwardFrame POSTs one JSON-RPC frame to the daemon's /mcp endpoint and
// writes the response (single JSON object, or SSE-stream of objects) as
// newline-delimited JSON to out.
func forwardFrame(c *http.Client, endpoint string, sessionID *string, body []byte, out *bufio.Writer) error {
	req, err := http.NewRequest(http.MethodPost, endpoint, bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	// Accept both response shapes the Streamable HTTP spec defines.
	req.Header.Set("Accept", "application/json, text/event-stream")
	if *sessionID != "" {
		req.Header.Set("Mcp-Session-Id", *sessionID)
	}
	resp, err := c.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	// The first initialize response sets the session id; subsequent
	// requests must echo it back even for the default stateless manager
	// (the resolver still emits one).
	if sid := resp.Header.Get("Mcp-Session-Id"); sid != "" {
		*sessionID = sid
	}

	switch resp.StatusCode {
	case http.StatusAccepted:
		// Notification: server consumed it, no reply to forward.
		return nil
	case http.StatusOK:
		// Fallthrough to body parsing.
	default:
		b, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("daemon /mcp returned HTTP %d: %s", resp.StatusCode, strings.TrimSpace(string(b)))
	}

	ct := resp.Header.Get("Content-Type")
	switch {
	case strings.HasPrefix(ct, "application/json"):
		b, err := io.ReadAll(resp.Body)
		if err != nil {
			return err
		}
		b = bytes.TrimSpace(b)
		if len(b) == 0 {
			return nil
		}
		_, _ = out.Write(b)
		_ = out.WriteByte('\n')
		return nil
	case strings.HasPrefix(ct, "text/event-stream"):
		return forwardSSE(resp.Body, out)
	default:
		return fmt.Errorf("unexpected Content-Type %q from daemon /mcp", ct)
	}
}

// forwardSSE reads an SSE stream from r and writes each `data:` payload as
// one stdout line. Multi-line `data:` payloads are concatenated per SSE
// rules (newlines between lines, then a blank line ends the event).
func forwardSSE(r io.Reader, out *bufio.Writer) error {
	br := bufio.NewReaderSize(r, 1<<16)
	var data strings.Builder
	flush := func() {
		if data.Len() == 0 {
			return
		}
		_, _ = out.WriteString(data.String())
		_ = out.WriteByte('\n')
		_ = out.Flush()
		data.Reset()
	}
	for {
		line, err := br.ReadString('\n')
		line = strings.TrimRight(line, "\r\n")
		switch {
		case line == "":
			flush()
		case strings.HasPrefix(line, "data: "):
			if data.Len() > 0 {
				data.WriteByte('\n')
			}
			data.WriteString(strings.TrimPrefix(line, "data: "))
		case strings.HasPrefix(line, "data:"):
			if data.Len() > 0 {
				data.WriteByte('\n')
			}
			data.WriteString(strings.TrimPrefix(line, "data:"))
		}
		if err == io.EOF {
			flush()
			return nil
		}
		if err != nil {
			return err
		}
	}
}

// writeRPCError synthesizes a JSON-RPC error reply for one frame so the
// upstream MCP client sees a response. If the frame was a notification
// (no id), no reply is sent.
func writeRPCError(out *bufio.Writer, requestBody []byte, cause error) {
	var msg struct {
		ID json.RawMessage `json:"id"`
	}
	if err := json.Unmarshal(requestBody, &msg); err != nil || len(msg.ID) == 0 {
		// Malformed or notification — nothing to reply to.
		return
	}
	payload, _ := json.Marshal(map[string]any{
		"jsonrpc": "2.0",
		"id":      json.RawMessage(msg.ID),
		"error": map[string]any{
			"code":    -32603,
			"message": cause.Error(),
		},
	})
	_, _ = out.Write(payload)
	_ = out.WriteByte('\n')
	_ = out.Flush()
}

