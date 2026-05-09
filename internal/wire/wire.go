// Package wire defines the JSON envelope schema spoken on both the unix socket
// (length-prefixed JSON) and the WebSocket transport. Same shape on both wires.
package wire

import "encoding/json"

// ClientMsg is sent from a client (CLI tail, web UI) to the server.
type ClientMsg struct {
	Type            string           `json:"type"`
	ID              uint64           `json:"id,omitempty"`
	Subscribe       *Subscribe       `json:"subscribe,omitempty"`
	Unsubscribe     *Unsubscribe     `json:"unsubscribe,omitempty"`
	Pause           *Pause           `json:"pause,omitempty"`
	Resume          *Resume          `json:"resume,omitempty"`
	Filter          *UpdateFilter    `json:"filter,omitempty"`
	AddSource       *AddSource       `json:"add_source,omitempty"`
	RemoveSrc       *RemoveSource    `json:"remove_source,omitempty"`
	StreamData      *StreamData      `json:"stream_data,omitempty"`
	History         *History         `json:"history,omitempty"`
	Ping            *Ping            `json:"ping,omitempty"`
	ActivateProfile *ActivateProfile `json:"activate_profile,omitempty"`
}

// ServerMsg is sent from server to a client.
type ServerMsg struct {
	Type     string       `json:"type"`
	ID       uint64       `json:"id,omitempty"`
	Batch    *LogBatch    `json:"batch,omitempty"`
	Source   *SourceEvent `json:"source,omitempty"`
	Ack      *Ack         `json:"ack,omitempty"`
	Err      *ErrMsg      `json:"err,omitempty"`
	Snapshot *Snapshot    `json:"snapshot,omitempty"`
	Pong     *Pong        `json:"pong,omitempty"`
}

type Subscribe struct {
	SubID    uint64 `json:"sub_id"`
	Filter   string `json:"filter"`             // raw filter expression
	Profile  string `json:"profile,omitempty"`  // optional profile name
	FromSeq  uint64 `json:"from_seq,omitempty"` // 0 = live tail from now
	HistoryN int    `json:"history_n,omitempty"`
}
type Unsubscribe struct{ SubID uint64 `json:"sub_id"` }
type Pause struct{ SubID uint64 `json:"sub_id"` }
type Resume struct{ SubID uint64 `json:"sub_id"` }
type UpdateFilter struct {
	SubID   uint64 `json:"sub_id"`
	Filter  string `json:"filter"`
	Profile string `json:"profile,omitempty"`
}

type AddSource struct {
	Kind string         `json:"kind"` // "file" | "stdin" | "docker"
	Name string         `json:"name,omitempty"`
	Args map[string]any `json:"args,omitempty"`
}
type RemoveSource struct{ SourceID uint64 `json:"source_id"` }
type StreamData struct {
	SourceID uint64 `json:"source_id"`
	// Chunk is base64-encoded raw bytes from a piped stdin client.
	Chunk []byte `json:"chunk"`
	EOF   bool   `json:"eof,omitempty"`
}
type Ping struct{ Nonce uint64 `json:"nonce"` }
type Pong struct{ Nonce uint64 `json:"nonce"` }

// ActivateProfile tells the server which profile this client is now using.
// The server diffs Profile.Sources against the previously-active set and
// adds/removes sources accordingly. Empty Name means "no active profile" —
// the server tears down anything it added on behalf of the previous one.
//
// Activation is server-global, not per-session: with multiple tabs open the
// last activate wins. Manually-added sources (kind "file"/"docker" via
// add_source) are NOT touched on profile switch — only sources the server
// itself attached on a previous activate_profile.
type ActivateProfile struct {
	Name string `json:"name"`
}

// History requests up to Limit matching entries strictly before BeforeSeq on
// the existing subscription's filter. Reply is a LogBatch with IsHistory=true.
// End=true on the reply indicates no more older entries are available
// (we hit the store tail or the buffer floor).
type History struct {
	SubID     uint64 `json:"sub_id"`
	BeforeSeq uint64 `json:"before_seq"`
	Limit     int    `json:"limit"`
}

// LogBatch carries one or more log entries to a subscriber.
type LogBatch struct {
	SubID     uint64  `json:"sub_id"`
	Entries   []Entry `json:"entries"`
	GapN      uint64  `json:"gap_n,omitempty"`     // entries dropped before this batch
	IsHistory bool    `json:"is_history,omitempty"` // reply to a History request — append, don't prepend
	End       bool    `json:"end,omitempty"`        // History exhausted (older entries unavailable)
}

// Entry is the wire shape of a stored log row. The server materializes from
// columnar storage on send.
type Entry struct {
	Seq      uint64          `json:"seq"`
	Ts       float64         `json:"ts"`
	SourceID uint64          `json:"source_id"`
	Level    string          `json:"level,omitempty"`
	Service  string          `json:"service,omitempty"`
	Msg      string          `json:"msg,omitempty"`
	Fields   json.RawMessage `json:"fields,omitempty"` // remaining JSON fields
	Ansi     string          `json:"ansi,omitempty"`   // original ANSI bytes for text-mode rows
	Text     bool            `json:"text,omitempty"`
}

type SourceEvent struct {
	SourceID uint64 `json:"source_id"`
	Kind     string `json:"kind"`            // "file" | "stdin" | "docker"
	Name     string `json:"name"`
	State    string `json:"state"`           // "open" | "closed" | "error"
	Mode     string `json:"mode,omitempty"`  // "json" | "text"
	Detail   string `json:"detail,omitempty"`
}

type Snapshot struct {
	Sources []SourceEvent `json:"sources"`
	Head    uint64        `json:"head"`
}

type Ack struct {
	OK     bool   `json:"ok"`
	RefID  uint64 `json:"ref_id,omitempty"` // echoes ClientMsg.ID
	SubID  uint64 `json:"sub_id,omitempty"`
	SrcID  uint64 `json:"src_id,omitempty"`
	Detail string `json:"detail,omitempty"`
}

type ErrMsg struct {
	RefID  uint64 `json:"ref_id,omitempty"`
	Code   string `json:"code"`
	Detail string `json:"detail"`
}

// Message type constants.
const (
	CMsgSubscribe   = "subscribe"
	CMsgUnsubscribe = "unsubscribe"
	CMsgPause       = "pause"
	CMsgResume      = "resume"
	CMsgFilter      = "filter"
	CMsgAddSource   = "add_source"
	CMsgRemoveSrc   = "remove_source"
	CMsgStreamData  = "stream_data"
	CMsgHistory         = "history"
	CMsgPing            = "ping"
	CMsgActivateProfile = "activate_profile"

	SMsgBatch    = "batch"
	SMsgSource   = "source"
	SMsgAck      = "ack"
	SMsgErr      = "err"
	SMsgSnapshot = "snapshot"
	SMsgPong     = "pong"
)
