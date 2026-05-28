package mcp

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/mark3labs/mcp-go/mcp"
	mcpserver "github.com/mark3labs/mcp-go/server"

	"github.com/iOliverNguyen/loggi/internal/filter"
	"github.com/iOliverNguyen/loggi/internal/server"
	"github.com/iOliverNguyen/loggi/internal/store"
)

const (
	defaultSearchLimit = 200
	maxSearchLimit     = 1000
	defaultTailN       = 50
	defaultBucketSecs  = 60
	maxHistogramBuckets = 5000
)

func registerTools(s *mcpserver.MCPServer, logsrv *server.Server) {
	s.AddTool(searchLogsTool(), bindSearchLogs(logsrv))
	s.AddTool(tailRecentTool(), bindTailRecent(logsrv))
	s.AddTool(histogramTool(), bindHistogram(logsrv))
	s.AddTool(listSourcesTool(), bindListSources(logsrv))
	s.AddTool(getEntryBySeqTool(), bindGetEntryBySeq(logsrv))
	s.AddTool(listProfilesTool(), bindListProfiles(logsrv))
	s.AddTool(getViewURLTool(), bindGetViewURL(logsrv))
}

// --- search_logs ---------------------------------------------------------

func searchLogsTool() mcp.Tool {
	return mcp.NewTool("search_logs",
		mcp.WithDescription(
			"Search captured logs by filter expression (loggi DSL). "+
				"Returns matching entries from the in-memory ring buffer. "+
				"Use `from`/`to` (unix seconds) to bound by time; omit both "+
				"to search all live rows."),
		mcp.WithString("filter",
			mcp.Description("Filter expression in loggi DSL (e.g. \"level:error service:api\"). Empty matches all."),
		),
		mcp.WithNumber("from",
			mcp.Description("Lower bound on entry timestamp (unix seconds, inclusive). Optional."),
		),
		mcp.WithNumber("to",
			mcp.Description("Upper bound on entry timestamp (unix seconds, exclusive). Optional."),
		),
		mcp.WithNumber("limit",
			mcp.Description("Max entries to return (default 200, max 1000)."),
			mcp.Min(1),
			mcp.Max(maxSearchLimit),
		),
	)
}

func bindSearchLogs(logsrv *server.Server) mcpserver.ToolHandlerFunc {
	return func(_ context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		expr := req.GetString("filter", "")
		fromTs := req.GetFloat("from", 0)
		toTs := req.GetFloat("to", 0)
		limit := clamp(req.GetInt("limit", defaultSearchLimit), 1, maxSearchLimit)

		seqs, err := runQuery(logsrv, expr, fromTs, toTs, limit, false)
		if err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}
		return entriesResult(logsrv, seqs)
	}
}

// --- tail_recent ---------------------------------------------------------

func tailRecentTool() mcp.Tool {
	return mcp.NewTool("tail_recent",
		mcp.WithDescription(
			"Return the most recent N entries, optionally filtered. "+
				"For a live look at what's flowing in right now."),
		mcp.WithNumber("n",
			mcp.Description("How many recent entries to return (default 50, max 1000)."),
			mcp.Min(1),
			mcp.Max(maxSearchLimit),
		),
		mcp.WithString("filter",
			mcp.Description("Filter expression in loggi DSL. Empty matches all."),
		),
	)
}

func bindTailRecent(logsrv *server.Server) mcpserver.ToolHandlerFunc {
	return func(_ context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		n := clamp(req.GetInt("n", defaultTailN), 1, maxSearchLimit)
		expr := req.GetString("filter", "")

		seqs, err := runQuery(logsrv, expr, 0, 0, n, true)
		if err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}
		return entriesResult(logsrv, seqs)
	}
}

// --- histogram -----------------------------------------------------------

func histogramTool() mcp.Tool {
	return mcp.NewTool("histogram",
		mcp.WithDescription(
			"Return per-bucket counts of matching entries grouped by level "+
				"(error/warn/info/debug/other). Useful for spotting spikes."),
		mcp.WithString("filter",
			mcp.Description("Filter expression in loggi DSL. Empty matches all."),
		),
		mcp.WithNumber("from",
			mcp.Description("Window lower bound (unix seconds). Default: to - bucket*120."),
		),
		mcp.WithNumber("to",
			mcp.Description("Window upper bound (unix seconds, exclusive). Default: now."),
		),
		mcp.WithNumber("bucket",
			mcp.Description("Bucket width in seconds (default 60, min 1, max 86400)."),
			mcp.Min(1),
			mcp.Max(86400),
		),
	)
}

type histogramBucket struct {
	T     float64 `json:"t"`
	Error uint32  `json:"error"`
	Warn  uint32  `json:"warn"`
	Info  uint32  `json:"info"`
	Debug uint32  `json:"debug"`
	Other uint32  `json:"other"`
}

type histogramResult struct {
	BucketSeconds int               `json:"bucket_seconds"`
	From          float64           `json:"from"`
	To            float64           `json:"to"`
	Buckets       []histogramBucket `json:"buckets"`
}

func bindHistogram(logsrv *server.Server) mcpserver.ToolHandlerFunc {
	return func(_ context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		bucket := req.GetInt("bucket", defaultBucketSecs)
		if bucket <= 0 {
			bucket = defaultBucketSecs
		}
		if bucket > 86400 {
			bucket = 86400
		}
		now := float64(time.Now().UnixNano()) / 1e9
		to := req.GetFloat("to", now)
		from := req.GetFloat("from", to-float64(bucket)*120)
		if !(from < to) {
			return mcp.NewToolResultError("from must be < to"), nil
		}
		span := to - from
		nBuckets := int((span + float64(bucket) - 1) / float64(bucket))
		if nBuckets > maxHistogramBuckets {
			return mcp.NewToolResultError("too many buckets — widen `bucket` or narrow the range"), nil
		}

		var fn func(uint64) bool
		if expr := strings.TrimSpace(req.GetString("filter", "")); expr != "" {
			node, err := filter.Parse(expr)
			if err != nil {
				return mcp.NewToolResultError("filter parse error: " + err.Error()), nil
			}
			fn = filter.Compile(node, logsrv.Store())
		}

		cells := make([]histogramBucket, nBuckets)
		st := logsrv.Store()
		tail := st.Tail()
		head := st.Head()
		for seq := tail; seq < head; seq++ {
			ts, ok := st.HotF64(seq, "ts")
			if !ok || ts < from || ts >= to {
				continue
			}
			if fn != nil && !fn(seq) {
				continue
			}
			idx := int((ts - from) / float64(bucket))
			if idx < 0 || idx >= nBuckets {
				continue
			}
			cells[idx].T = from + float64(idx*bucket)
			switch strings.ToLower(st.HotString(seq, "level")) {
			case "error", "fatal", "panic":
				cells[idx].Error++
			case "warn", "warning":
				cells[idx].Warn++
			case "info", "notice":
				cells[idx].Info++
			case "debug", "trace":
				cells[idx].Debug++
			default:
				cells[idx].Other++
			}
		}
		// Ensure every bucket has its t field set (cells with zero counts
		// otherwise leave T at zero).
		for i := range cells {
			cells[i].T = from + float64(i*bucket)
		}
		return mcp.NewToolResultJSON(histogramResult{
			BucketSeconds: bucket,
			From:          from,
			To:            to,
			Buckets:       cells,
		})
	}
}

// --- list_sources --------------------------------------------------------

func listSourcesTool() mcp.Tool {
	return mcp.NewTool("list_sources",
		mcp.WithDescription(
			"List all sources currently registered with loggi (files, "+
				"docker containers, stdin pipes), with their state and "+
				"recent ingest rate."),
	)
}

func bindListSources(logsrv *server.Server) mcpserver.ToolHandlerFunc {
	return func(_ context.Context, _ mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		return mcp.NewToolResultJSON(map[string]any{
			"sources": logsrv.Sources(),
		})
	}
}

// --- get_entry_by_seq ----------------------------------------------------

func getEntryBySeqTool() mcp.Tool {
	return mcp.NewTool("get_entry_by_seq",
		mcp.WithDescription(
			"Fetch one log entry by its sequence id (seq). Returns the full "+
				"row including all hot and cold fields. Useful as a follow-up "+
				"to search_logs to inspect a specific row."),
		mcp.WithNumber("seq",
			mcp.Required(),
			mcp.Description("Sequence id of the entry to fetch."),
			mcp.Min(0),
		),
	)
}

func bindGetEntryBySeq(logsrv *server.Server) mcpserver.ToolHandlerFunc {
	return func(_ context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		seq := req.GetInt("seq", -1)
		if seq < 0 {
			return mcp.NewToolResultError("seq is required and must be >= 0"), nil
		}
		row := logsrv.Store().Materialize(uint64(seq))
		if row == nil {
			return mcp.NewToolResultError(fmt.Sprintf("entry %d not found (evicted or out of range)", seq)), nil
		}
		return mcp.NewToolResultJSON(rowToMap(logsrv.Store(), row))
	}
}

// --- list_profiles -------------------------------------------------------

func listProfilesTool() mcp.Tool {
	return mcp.NewTool("list_profiles",
		mcp.WithDescription(
			"List saved filter profiles. Each profile bundles a filter "+
				"expression, a column set, and a source list."),
	)
}

func bindListProfiles(logsrv *server.Server) mcpserver.ToolHandlerFunc {
	return func(_ context.Context, _ mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		return mcp.NewToolResultJSON(map[string]any{
			"profiles": logsrv.ProfilesSnapshot(),
		})
	}
}

// --- get_view_url --------------------------------------------------------

func getViewURLTool() mcp.Tool {
	return mcp.NewTool("get_view_url",
		mcp.WithDescription(
			"Build a clickable URL that opens the loggi web UI with the "+
				"given filter (and optional profile/columns) pre-applied. "+
				"Show the returned URL to the user so they can inspect the "+
				"data interactively."),
		mcp.WithString("filter",
			mcp.Description("Filter expression to pre-apply in the UI."),
		),
		mcp.WithString("profile",
			mcp.Description("Saved profile name to activate."),
		),
		mcp.WithArray("columns",
			mcp.Description("Column names to show in the table."),
			mcp.Items(map[string]any{"type": "string"}),
		),
		mcp.WithBoolean("paused",
			mcp.Description("If true, opens the UI in paused mode (no live tail)."),
		),
	)
}

func bindGetViewURL(logsrv *server.Server) mcpserver.ToolHandlerFunc {
	return func(_ context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		cfg := SessionConfig{
			Filter:  req.GetString("filter", ""),
			Profile: req.GetString("profile", ""),
			Columns: req.GetStringSlice("columns", nil),
			Paused:  req.GetBool("paused", false),
		}
		url, err := buildViewURL(logsrv.HTTPURL(), cfg)
		if err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}
		return mcp.NewToolResultJSON(map[string]any{"url": url})
	}
}

// --- shared helpers ------------------------------------------------------

// runQuery compiles expr and returns matching seqs. When recentOnly is
// true, returns the last `limit` matches (newest); otherwise returns up to
// `limit` matches in ascending seq order over [fromTs, toTs).
func runQuery(logsrv *server.Server, expr string, fromTs, toTs float64, limit int, recentOnly bool) ([]uint64, error) {
	st := logsrv.Store()
	var node filter.Node
	if e := strings.TrimSpace(expr); e != "" {
		var err error
		node, err = filter.Parse(e)
		if err != nil {
			return nil, fmt.Errorf("filter parse error: %s", err.Error())
		}
	}
	plan := filter.CompilePlan(node, st)
	residual := plan.Residual
	if fromTs > 0 || toTs > 0 {
		residual = wrapTimeRange(st, residual, fromTs, toTs)
	}
	tail, head := st.Tail(), st.Head()
	// Pull all matches first; for recentOnly we slice to the tail of the
	// result. This mirrors session.queryBacklog (a sparse filter outside the
	// last-N seqs would otherwise return zero rows even with matches in the
	// ring).
	all := st.QueryRangeBitmap(plan.Candidates, residual, tail, head, 0)
	if recentOnly && len(all) > limit {
		return all[len(all)-limit:], nil
	}
	if !recentOnly && len(all) > limit {
		return all[:limit], nil
	}
	return all, nil
}

// wrapTimeRange returns a residual closure that additionally enforces a
// timestamp window. Bounds with value 0 are treated as unbounded.
func wrapTimeRange(st *store.Store, inner func(uint64) bool, fromTs, toTs float64) func(uint64) bool {
	return func(seq uint64) bool {
		ts, ok := st.HotF64(seq, "ts")
		if !ok {
			return false
		}
		if fromTs > 0 && ts < fromTs {
			return false
		}
		if toTs > 0 && ts >= toTs {
			return false
		}
		if inner != nil {
			return inner(seq)
		}
		return true
	}
}

// entriesResult materializes the given seqs and wraps them in a JSON tool
// result. Evicted rows (Materialize returns nil) are skipped silently.
func entriesResult(logsrv *server.Server, seqs []uint64) (*mcp.CallToolResult, error) {
	st := logsrv.Store()
	entries := make([]map[string]any, 0, len(seqs))
	for _, seq := range seqs {
		row := st.Materialize(seq)
		if row == nil {
			continue
		}
		entries = append(entries, rowToMap(st, row))
	}
	return mcp.NewToolResultJSON(map[string]any{
		"count":   len(entries),
		"entries": entries,
	})
}

func rowToMap(st *store.Store, row *store.MaterializedRow) map[string]any {
	out := map[string]any{
		"seq":       row.Seq,
		"ts":        row.Ts,
		"source_id": row.SourceID,
		"source":    st.SourceName(row.SourceID),
		"level":     row.Level,
		"service":   row.Service,
		"msg":       row.Msg,
	}
	if len(row.Fields) > 0 {
		out["fields"] = json.RawMessage(row.Fields)
	}
	if row.Text {
		out["text"] = true
		if row.Ansi != "" {
			out["ansi"] = row.Ansi
		}
	}
	return out
}

func clamp(v, lo, hi int) int {
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}
