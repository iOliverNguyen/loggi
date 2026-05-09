package app

import (
	"fmt"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/iOliverNguyen/loggi/internal/client"
	"github.com/iOliverNguyen/loggi/internal/config"
	"github.com/iOliverNguyen/loggi/internal/server"
	"github.com/spf13/cobra"
)

// NewServerCmd is `loggi server [--daemon] | server stop | server status`.
func NewServerCmd() *cobra.Command {
	var daemon bool
	var debug bool
	cmd := &cobra.Command{
		Use:   "server",
		Short: "Run the loggi server (foreground or detached)",
		RunE: func(_ *cobra.Command, _ []string) error {
			return runServer(daemon, debug)
		},
	}
	cmd.Flags().BoolVar(&daemon, "daemon", false, "detach and write a pidfile")
	cmd.Flags().BoolVar(&debug, "debug", false, "enable /api/debug/* endpoints for runtime introspection")

	cmd.AddCommand(&cobra.Command{
		Use:   "stop",
		Short: "Stop a running server",
		RunE: func(_ *cobra.Command, _ []string) error {
			return stopServer()
		},
	})
	cmd.AddCommand(&cobra.Command{
		Use:   "status",
		Short: "Show server status",
		RunE: func(_ *cobra.Command, _ []string) error {
			return statusServer()
		},
	})
	return cmd
}

func runServer(_ bool, debug bool) error {
	loaded, err := config.Load(mustGetwd())
	if err != nil {
		return err
	}
	cfg := loaded.Config

	idle, err := time.ParseDuration(cfg.Server.IdleTimeout)
	if err != nil {
		idle = 5 * time.Minute
	}

	if _, err := config.EnsureUserDir(); err != nil {
		return err
	}

	profiles := make([]server.ProfileInfo, 0, len(cfg.Profiles))
	for _, p := range cfg.Profiles {
		profiles = append(profiles, server.ProfileInfo{
			Name: p.Name, Filter: p.Filter, Columns: p.Columns,
		})
	}
	srv := server.NewServer(server.Options{
		SocketPath:      config.SocketPath(),
		HTTPBind:        cfg.Server.HTTPBind,
		IdleTimeout:     idle,
		StoreCap:        uint64(cfg.Server.RingBuffer),
		StaticFS:        embeddedSPA(),
		Profiles:        profiles,
		Theme:           cfg.UI.Theme,
		Density:         cfg.UI.Density,
		DefaultProfile:  cfg.UI.DefaultProfile,
		TimestampFormat: cfg.UI.TimestampFormat,
		DockerTail:      cfg.Sources.Defaults.DockerTail,
		FilePollMS:      cfg.Sources.Defaults.FilePollMS,
		Autostart:       cfg.Sources.Autostart,
		RepoRoot:        config.FindRepoRoot(mustGetwd()),
		Debug:           debug,
	})
	if err := srv.Start(); err != nil {
		return err
	}

	pidPath := config.PidPath()
	_ = os.WriteFile(pidPath, []byte(strconv.Itoa(os.Getpid())), 0o600)
	defer os.Remove(pidPath)

	_ = client.WriteRuntime(&client.RuntimeInfo{
		PID:     os.Getpid(),
		Socket:  config.SocketPath(),
		HTTP:    srv.HTTPURL(),
		Started: time.Now(),
	})

	fmt.Fprintf(os.Stderr, "loggi server: socket=%s http=%s\n", srv.SocketPath(), srv.HTTPURL())

	sig := make(chan os.Signal, 1)
	signal.Notify(sig, syscall.SIGINT, syscall.SIGTERM)
	select {
	case <-sig:
	case <-srv.Done():
	}
	srv.Shutdown()
	return nil
}

func stopServer() error {
	pidStr, err := os.ReadFile(config.PidPath())
	if err != nil {
		return fmt.Errorf("no server running: %w", err)
	}
	pid, err := strconv.Atoi(strings.TrimSpace(string(pidStr)))
	if err != nil {
		return err
	}
	if err := syscall.Kill(pid, syscall.SIGTERM); err != nil {
		return err
	}
	fmt.Println("sent SIGTERM to pid", pid)
	return nil
}

func statusServer() error {
	info, err := client.ReadRuntime()
	if err != nil {
		fmt.Println("no server running (no runtime.json)")
		return nil
	}
	fmt.Printf("pid:    %d\n", info.PID)
	fmt.Printf("socket: %s\n", info.Socket)
	fmt.Printf("http:   %s\n", info.HTTP)
	fmt.Printf("uptime: %s\n", time.Since(info.Started).Round(time.Second))
	return nil
}

func mustGetwd() string {
	wd, err := os.Getwd()
	if err != nil {
		return "."
	}
	return wd
}
