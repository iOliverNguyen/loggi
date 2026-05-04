package client

import (
	"encoding/json"
	"os"
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
func WriteRuntime(info *RuntimeInfo) error {
	p, err := config.RuntimeFile()
	if err != nil {
		return err
	}
	data, err := json.Marshal(info)
	if err != nil {
		return err
	}
	return os.WriteFile(p, data, 0o600)
}
