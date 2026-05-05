package config

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/BurntSushi/toml"
)

// SaveProfileDest is where a UI-saved profile should land.
type SaveProfileDest string

const (
	SaveDestUser SaveProfileDest = "user" // ~/.zz/loggi/config.toml
	SaveDestRepo SaveProfileDest = "repo" // REPO/.loggi/config.toml or REPO/cmd/.config/loggi.toml
)

// SaveProfile reads the destination config file (creating it if absent),
// inserts or replaces a profile by name, and writes the file back atomically.
// It preserves all other top-level config sections.
func SaveProfile(p Profile, dest SaveProfileDest, repoRoot string) (string, error) {
	path, err := profilePath(dest, repoRoot)
	if err != nil {
		return "", err
	}

	cfg, err := readPartial(path)
	if err != nil {
		return path, err
	}

	replaced := false
	for i := range cfg.Profiles {
		if cfg.Profiles[i].Name == p.Name {
			cfg.Profiles[i] = p
			replaced = true
			break
		}
	}
	if !replaced {
		cfg.Profiles = append(cfg.Profiles, p)
	}

	if err := writeAtomic(path, cfg); err != nil {
		return path, err
	}
	return path, nil
}

// DeleteProfile removes a profile by name from the destination file.
// Returns nil if the file or profile is absent.
func DeleteProfile(name string, dest SaveProfileDest, repoRoot string) (string, error) {
	path, err := profilePath(dest, repoRoot)
	if err != nil {
		return "", err
	}
	cfg, err := readPartial(path)
	if err != nil {
		return path, err
	}
	out := cfg.Profiles[:0]
	for _, prof := range cfg.Profiles {
		if prof.Name != name {
			out = append(out, prof)
		}
	}
	cfg.Profiles = out
	if err := writeAtomic(path, cfg); err != nil {
		return path, err
	}
	return path, nil
}

func profilePath(dest SaveProfileDest, repoRoot string) (string, error) {
	switch dest {
	case "", SaveDestUser:
		return UserConfigFile()
	case SaveDestRepo:
		if repoRoot == "" {
			return "", fmt.Errorf("no repo root (run from inside a git/go repo)")
		}
		committed, _ := RepoConfigPaths(repoRoot)
		return committed, nil
	default:
		return "", fmt.Errorf("unknown destination %q", dest)
	}
}

func readPartial(path string) (Config, error) {
	var cfg Config
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

func writeAtomic(path string, cfg Config) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), ".loggi-config-*.tmp")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name())
	if err := toml.NewEncoder(tmp).Encode(cfg); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmp.Name(), path)
}
