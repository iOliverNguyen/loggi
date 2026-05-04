package config

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/BurntSushi/toml"
)

// LoadUser reads only ~/.zz/loggi/config.toml (no repo or local merging).
// Returns an empty Config (no defaults filled in) if the file doesn't exist
// — callers about to mutate-and-save shouldn't have defaults baked in.
func LoadUser() (Config, error) {
	var cfg Config
	path, err := UserConfigFile()
	if err != nil {
		return cfg, err
	}
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return cfg, nil
		}
		return cfg, fmt.Errorf("read %s: %w", path, err)
	}
	if _, err := toml.Decode(string(data), &cfg); err != nil {
		return cfg, fmt.Errorf("parse %s: %w", path, err)
	}
	return cfg, nil
}

// SaveUser writes cfg to ~/.zz/loggi/config.toml, creating the directory if
// necessary. Replaces the file atomically via a tmp+rename.
func SaveUser(cfg Config) error {
	path, err := UserConfigFile()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), ".loggi-config-*.tmp")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name())
	enc := toml.NewEncoder(tmp)
	if err := enc.Encode(cfg); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmp.Name(), path)
}

// Loaded is the result of resolving config from all precedence layers.
type Loaded struct {
	Config    Config
	UserPath  string
	RepoPath  string
	LocalPath string
	Found     []string // paths that actually existed and were merged
}

// Load resolves config in precedence order (last wins): user → repo → local.
// The starting cwd is used to detect the repo root.
func Load(cwd string) (*Loaded, error) {
	cfg := Defaults()
	out := &Loaded{Config: cfg}

	if up, err := UserConfigFile(); err == nil {
		out.UserPath = up
		if err := mergeFile(&cfg, up, &out.Found); err != nil {
			return nil, err
		}
	}

	repo := FindRepoRoot(cwd)
	if repo != "" {
		committed, local := RepoConfigPaths(repo)
		out.RepoPath = committed
		out.LocalPath = local
		if err := mergeFile(&cfg, committed, &out.Found); err != nil {
			return nil, err
		}
		if err := mergeFile(&cfg, local, &out.Found); err != nil {
			return nil, err
		}
	}

	out.Config = cfg
	return out, nil
}

func mergeFile(cfg *Config, path string, found *[]string) error {
	if path == "" {
		return nil
	}
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return fmt.Errorf("read %s: %w", path, err)
	}
	// BurntSushi/toml replaces slice fields wholesale on decode. We want
	// profiles to merge by name (later definition wins), not be clobbered.
	// Strategy: snapshot the previous profiles, decode into cfg (which may
	// overwrite Profiles), then merge the snapshot back in.
	prev := cfg.Profiles
	cfg.Profiles = nil
	if _, err := toml.Decode(string(data), cfg); err != nil {
		return fmt.Errorf("parse %s: %w", path, err)
	}
	cfg.Profiles = mergeProfiles(prev, cfg.Profiles)
	*found = append(*found, path)
	return nil
}

// mergeProfiles merges b into a by name. Order: a first (in original order),
// then any new names from b. When a profile name appears in both, b wins.
func mergeProfiles(a, b []Profile) []Profile {
	if len(b) == 0 {
		return a
	}
	idx := make(map[string]int, len(a))
	out := make([]Profile, 0, len(a)+len(b))
	for _, p := range a {
		idx[p.Name] = len(out)
		out = append(out, p)
	}
	for _, p := range b {
		if i, ok := idx[p.Name]; ok {
			out[i] = p
		} else {
			idx[p.Name] = len(out)
			out = append(out, p)
		}
	}
	return out
}
