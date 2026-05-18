package client

import (
	"encoding/json"
	"os"
	"path/filepath"
	"time"

	"github.com/iOliverNguyen/loggi/internal/config"
)

// RuntimeInfo is what the server writes to ~/.zz/loggi/runtime.json on start.
type RuntimeInfo struct {
	PID     int       `json:"pid"`
	Socket  string    `json:"socket"`
	HTTP    string    `json:"http"`
	Started time.Time `json:"started"`
}

// ReadRuntime reads runtime.json. Returns os.ErrNotExist if absent.
func ReadRuntime() (*RuntimeInfo, error) {
	p, err := config.RuntimeFile()
	if err != nil {
		return nil, err
	}
	data, err := os.ReadFile(p)
	if err != nil {
		return nil, err
	}
	var info RuntimeInfo
	if err := json.Unmarshal(data, &info); err != nil {
		return nil, err
	}
	return &info, nil
}

// WriteRuntime writes runtime.json (called by the server on Start).
// Writes via tmp + rename so a crash mid-write cannot leave a partial file
// for ReadRuntime to choke on.
func WriteRuntime(info *RuntimeInfo) error {
	p, err := config.RuntimeFile()
	if err != nil {
		return err
	}
	dir := filepath.Dir(p)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	data, err := json.Marshal(info)
	if err != nil {
		return err
	}
	tmp, err := os.CreateTemp(dir, ".runtime-*.tmp")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name())
	if _, err := tmp.Write(data); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Chmod(0o600); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmp.Name(), p)
}
