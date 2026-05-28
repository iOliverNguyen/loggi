package app

import (
	"errors"
	"fmt"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"syscall"
	"time"

	mcpserver "github.com/mark3labs/mcp-go/server"

	"github.com/iOliverNguyen/loggi/internal/client"
	"github.com/iOliverNguyen/loggi/internal/config"
	"github.com/iOliverNguyen/loggi/internal/mcp"
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
		Use:           "stop",
		Short:         "Stop a running server",
		SilenceUsage:  true, // "couldn't find a pid" isn't a usage error
		SilenceErrors: true, // main.go already prints the error once
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
			Name: p.Name, Filter: p.Filter, Columns: p.Columns, Sources: p.Sources,
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
	// Mount the MCP Streamable HTTP handler at /mcp before Start so the
	// listener begins serving it on the first request.
	srv.SetMCPHandler(mcpserver.NewStreamableHTTPServer(mcp.New(srv)))

	if err := srv.Start(); err != nil {
		return err
	}

	pidPath := config.PidPath()
	if err := os.WriteFile(pidPath, []byte(strconv.Itoa(os.Getpid())), 0o600); err != nil {
		srv.Shutdown()
		return fmt.Errorf("write pidfile %s: %w", pidPath, err)
	}
	defer os.Remove(pidPath)

	if err := client.WriteRuntime(&client.RuntimeInfo{
		PID:     os.Getpid(),
		Socket:  config.SocketPath(),
		HTTP:    srv.HTTPURL(),
		Started: time.Now(),
	}); err != nil {
		srv.Shutdown()
		return fmt.Errorf("write runtime info: %w", err)
	}

	fmt.Fprintf(os.Stderr, "loggi server: socket=%s http=%s\n", srv.SocketPath(), srv.HTTPURL())
	fmt.Fprintf(os.Stderr, "loggi mcp:    %s/mcp  (or `loggi mcp` over stdio)\n", srv.HTTPURL())

	sig := make(chan os.Signal, 1)
	signal.Notify(sig, syscall.SIGINT, syscall.SIGTERM)
	defer signal.Stop(sig)
	select {
	case <-sig:
	case <-srv.Done():
	}
	srv.Shutdown()
	return nil
}

// stopServer SIGTERMs the running daemon. Prefers the pidfile (managed
// daemons) but falls back to discovering a running loggi via /api/health
// when the pidfile is missing — e.g. the daemon was started under a
// different $TMPDIR, or the pidfile was wiped. Returns "no server
// running" only when neither lookup finds anything.
func stopServer() error {
	if pid, ok := readPidfile(); ok {
		return signalAndReport(pid, "")
	}
	if h, url := client.DiscoverRunningDaemon(); h != nil {
		if h.PID == 0 {
			// Older daemon — /api/health doesn't expose pid. We must
			// NOT call kill(0, ...) (that signals every process in the
			// caller's group). Tell the user how to find it.
			return fmt.Errorf(
				"loggi is running at %s but doesn't expose its pid via /api/health "+
					"(older version). Find and stop it manually with:\n"+
					"  lsof -nP -iTCP -sTCP:LISTEN -P | grep loggi", url)
		}
		return signalAndReport(h.PID, fmt.Sprintf(" (found via %s/api/health)", url))
	}
	fmt.Println("no server running")
	return nil
}

// signalAndReport sends SIGTERM to pid and prints a confirmation line.
// EPERM (different user) becomes a directly-actionable message — there's
// no recovery loggi itself can do.
func signalAndReport(pid int, suffix string) error {
	if err := syscall.Kill(pid, syscall.SIGTERM); err != nil {
		if errors.Is(err, syscall.EPERM) {
			return fmt.Errorf("cannot stop pid %d: permission denied; kill it manually", pid)
		}
		return err
	}
	fmt.Printf("sent SIGTERM to pid %d%s\n", pid, suffix)
	return nil
}

// readPidfile returns the pid from the local pidfile, or (0, false) if
// the file is missing or unparseable.
func readPidfile() (int, bool) {
	b, err := os.ReadFile(config.PidPath())
	if err != nil {
		return 0, false
	}
	pid, err := strconv.Atoi(strings.TrimSpace(string(b)))
	if err != nil || pid <= 0 {
		return 0, false
	}
	return pid, true
}

// StatusServer is the public entry point for `loggi server status` and
// the top-level `loggi status` alias. Uses runtime.json when available;
// otherwise probes /api/health on the configured port so a daemon
// running without a usable runtime.json still shows up.
func StatusServer() error { return statusServer() }

func statusServer() error {
	if info, err := client.ReadRuntime(); err == nil {
		// runtime.json is authoritative. Cross-check by probing
		// /api/health so a stale runtime (pid recycled, daemon dead)
		// doesn't masquerade as healthy.
		if h, err := client.DiscoverViaHealth(info.HTTP); err == nil {
			printStatus(info.PID, info.Socket, info.HTTP, time.Since(info.Started).Round(time.Second), h, "")
			return nil
		}
		fmt.Printf("pid:    %d\n", info.PID)
		fmt.Printf("socket: %s\n", info.Socket)
		fmt.Printf("http:   %s\n", info.HTTP)
		fmt.Printf("mcp:    %s/mcp\n", info.HTTP)
		fmt.Printf("uptime: %s\n", time.Since(info.Started).Round(time.Second))
		fmt.Println("note:   /api/health is not responding — daemon may be stuck or dead")
		return nil
	}
	// No runtime.json — try HTTP discovery against config-bind.
	if h, url := client.DiscoverRunningDaemon(); h != nil {
		uptime := time.Duration(0)
		if h.StartedUnix > 0 {
			uptime = time.Since(time.Unix(h.StartedUnix, 0)).Round(time.Second)
		}
		printStatus(h.PID, h.Socket, url, uptime, h, "(no runtime.json; discovered via HTTP)")
		return nil
	}
	fmt.Println("no server running")
	return nil
}

// printStatus emits the multi-line status block. note is printed last
// (with a "note:" prefix) when non-empty — used to flag a degraded path.
// Older daemons that don't expose pid/socket get an additional
// "restart to enable" hint appended automatically.
func printStatus(pid int, socket, httpURL string, uptime time.Duration, h *client.Health, note string) {
	if pid > 0 {
		fmt.Printf("pid:    %d\n", pid)
	} else {
		fmt.Printf("pid:    (older daemon — restart to expose)\n")
	}
	if socket != "" {
		fmt.Printf("socket: %s\n", socket)
	}
	fmt.Printf("http:   %s\n", httpURL)
	fmt.Printf("mcp:    %s/mcp\n", httpURL)
	fmt.Printf("uptime: %s\n", uptime)
	if h != nil {
		fmt.Printf("rows:   %d  sources: %d (%d open)  sessions: %d\n",
			h.Rows, h.Sources, h.SourcesOpen, h.Sessions)
	}
	if pid == 0 || socket == "" {
		extra := "running an older loggi — restart it to enable full status/stop discovery"
		if note != "" {
			note = note + "; " + extra
		} else {
			note = extra
		}
	}
	if note != "" {
		fmt.Printf("note:   %s\n", note)
	}
}

func mustGetwd() string {
	wd, err := os.Getwd()
	if err != nil {
		return "."
	}
	return wd
}
