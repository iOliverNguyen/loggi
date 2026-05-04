package config

import (
	"errors"
	"os"
	"os/user"
	"path/filepath"
	"strconv"
)

// UserConfigDir returns ~/.zz/loggi.
func UserConfigDir() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".zz", "loggi"), nil
}

// UserConfigFile is ~/.zz/loggi/config.toml.
func UserConfigFile() (string, error) {
	dir, err := UserConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "config.toml"), nil
}

// RuntimeFile is ~/.zz/loggi/runtime.json.
func RuntimeFile() (string, error) {
	dir, err := UserConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "runtime.json"), nil
}

// ServerLogFile is ~/.zz/loggi/server.log.
func ServerLogFile() (string, error) {
	dir, err := UserConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "server.log"), nil
}

// SocketPath is /tmp/loggi-$UID.sock.
func SocketPath() string {
	return filepath.Join(os.TempDir(), "loggi-"+uid()+".sock")
}

// LockPath is /tmp/loggi-$UID.lock.
func LockPath() string {
	return filepath.Join(os.TempDir(), "loggi-"+uid()+".lock")
}

// PidPath is /tmp/loggi-$UID.pid.
func PidPath() string {
	return filepath.Join(os.TempDir(), "loggi-"+uid()+".pid")
}

func uid() string {
	u, err := user.Current()
	if err == nil && u.Uid != "" {
		return u.Uid
	}
	return strconv.Itoa(os.Getuid())
}

// FindRepoRoot walks up from start looking for a .git or go.mod marker. Returns
// "" if none found.
func FindRepoRoot(start string) string {
	if start == "" {
		var err error
		start, err = os.Getwd()
		if err != nil {
			return ""
		}
	}
	dir := start
	for {
		for _, marker := range []string{".git", "go.mod"} {
			if _, err := os.Stat(filepath.Join(dir, marker)); err == nil {
				return dir
			}
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return ""
		}
		dir = parent
	}
}

// RepoConfigPaths returns the candidate config files for a repo, in order:
//
//	REPO/cmd/.config/loggi.toml or REPO/.loggi/config.toml   (committed)
//	REPO/cmd/.config/loggi.local.toml or REPO/.loggi/config.local.toml (gitignored)
//
// Picks the cmd/ variant if REPO/cmd exists.
func RepoConfigPaths(repoRoot string) (committed, local string) {
	if repoRoot == "" {
		return "", ""
	}
	if st, err := os.Stat(filepath.Join(repoRoot, "cmd")); err == nil && st.IsDir() {
		return filepath.Join(repoRoot, "cmd", ".config", "loggi.toml"),
			filepath.Join(repoRoot, "cmd", ".config", "loggi.local.toml")
	}
	return filepath.Join(repoRoot, ".loggi", "config.toml"),
		filepath.Join(repoRoot, ".loggi", "config.local.toml")
}

// EnsureUserDir makes ~/.zz/loggi.
func EnsureUserDir() (string, error) {
	dir, err := UserConfigDir()
	if err != nil {
		return "", err
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return "", err
	}
	return dir, nil
}

// ErrNoRepo signals no repo root could be located.
var ErrNoRepo = errors.New("no repo root (.git or go.mod) found above cwd")
