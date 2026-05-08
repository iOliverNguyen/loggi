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
	RingBytes   string `toml:"ring_bytes"`   // size string, default "256MB"
	HTTPBind    string `toml:"http_bind"`    // default "127.0.0.1:9199"
}

type UI struct {
	Theme           string `toml:"theme"`           // "auto" | "light" | "dark"
	DefaultProfile  string `toml:"default_profile"`
	TimestampFormat string `toml:"timestamp_format"`
}

type Sources struct {
	Defaults SourceDefaults `toml:"defaults"`
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
	SavedFilters    []SavedFilter `toml:"saved_filters"`
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
			RingBytes:   "256MB",
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
