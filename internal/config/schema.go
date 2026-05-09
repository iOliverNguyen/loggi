package config

// Config is the merged configuration loaded from user + repo + local TOML
// files. Fields use defaults if absent.
type Config struct {
	Server   Server    `toml:"server"`
	UI       UI        `toml:"ui"`
	Sources  Sources   `toml:"sources"`
	Profiles []Profile `toml:"profiles"`
}

type Server struct {
	IdleTimeout string `toml:"idle_timeout"` // duration string, default "5m"
	RingBuffer  int    `toml:"ring_buffer"`  // entries, default 524288
	HTTPBind    string `toml:"http_bind"`    // default "127.0.0.1:9199"
}

type UI struct {
	Theme           string `toml:"theme"`             // "auto" | "light" | "dark"
	DefaultProfile  string `toml:"default_profile"`
	TimestampFormat string `toml:"timestamp_format"`
	// Density is round-tripped from the SettingsModal so it follows the
	// user across devices. Server doesn't act on it: "compact"/"cozy"/"comfortable".
	Density string `toml:"density,omitempty"`
}

type Sources struct {
	Defaults  SourceDefaults `toml:"defaults"`
	Autostart []SourceRef    `toml:"autostart,omitempty"`
}

// SourceRef identifies a log source to start: a kind ("file" | "docker"),
// the source-specific name (file path / container name), and any
// kind-specific args. Used by Sources.Autostart and Profile.Sources.
//
// JSON tags are required: the SettingsModal types this as { kind, name,
// args } and reads the lowercase form, so without tags the autostart UI
// would render blank rows.
type SourceRef struct {
	Kind string         `toml:"kind" json:"kind"`
	Name string         `toml:"name" json:"name"`
	Args map[string]any `toml:"args,omitempty" json:"args,omitempty"`
}

type SourceDefaults struct {
	FilePollMS int `toml:"file_poll_ms"`
	// DockerTail is the number of historical log lines pulled from the
	// Docker engine when a container source is added. The client only
	// renders the most recent ~300; the rest become available via the
	// scroll-back history RPC. Default: 1000.
	DockerTail int `toml:"docker_tail"`
}

type Profile struct {
	Name            string        `toml:"name"`
	Filter          string        `toml:"filter"`
	Columns         []string      `toml:"columns"`
	CollapsedFields []string      `toml:"collapsed_fields"`
	// TODO: SavedFilters is in the schema but no UI consumer reads/writes
	// it yet. Either wire it up or drop it; left in place because tests
	// already cover it.
	SavedFilters []SavedFilter `toml:"saved_filters"`
	// Sources is the per-profile overlay applied on activation. Server's
	// ActivateProfile diffs against what the previous profile declared
	// (NOT current live sources): refs unique to the previous profile
	// are stopped, refs unique to the new profile are started. Manual
	// adds and Sources.Autostart are not touched.
	//
	// SaveProfileModal "Include current sources" checkbox controls whether
	// the UI bundles the live (kind, name) pairs into this field on save.
	Sources []SourceRef `toml:"sources,omitempty"`
}

type SavedFilter struct {
	Name string `toml:"name"`
	Expr string `toml:"expr"`
}

// Defaults returns a config preloaded with sensible defaults.
func Defaults() Config {
	return Config{
		Server: Server{
			IdleTimeout: "5m",
			RingBuffer:  524288,
			HTTPBind:    "127.0.0.1:9199",
		},
		UI: UI{
			Theme:           "auto",
			DefaultProfile:  "default",
			TimestampFormat: "15:04:05.000",
		},
		Sources: Sources{
			Defaults: SourceDefaults{
				FilePollMS: 50,
				DockerTail: 1000,
			},
		},
		Profiles: []Profile{
			{
				Name:    "default",
				Filter:  "",
				Columns: []string{"ts", "level", "service", "msg"},
			},
		},
	}
}
