package app

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync/atomic"
	"time"

	"github.com/iOliverNguyen/loggi/internal/client"
	"github.com/iOliverNguyen/loggi/internal/config"
	"github.com/iOliverNguyen/loggi/internal/server"
	"github.com/iOliverNguyen/loggi/internal/wire"
	"github.com/spf13/cobra"
)

// NewTailCmd is `loggi tail <file>...`.
func NewTailCmd() *cobra.Command {
	var noOpen bool
	cmd := &cobra.Command{
		Use:   "tail [file...]",
		Short: "Tail one or more files into loggi and open the web UI",
		Args:  cobra.MinimumNArgs(1),
		RunE: func(_ *cobra.Command, args []string) error {
			return tailFiles(args, !noOpen)
		},
	}
	cmd.Flags().BoolVar(&noOpen, "no-open", false, "don't open the browser")
	return cmd
}

// NewStdinCmd is `loggi stdin` / `loggi -`.
func NewStdinCmd() *cobra.Command {
	var name string
	var noOpen bool
	cmd := &cobra.Command{
		Use:   "stdin",
		Short: "Pipe stdin into loggi as a named source",
		RunE: func(_ *cobra.Command, _ []string) error {
			return runStdin(name, !noOpen)
		},
	}
	cmd.Aliases = []string{"-"}
	cmd.Flags().StringVar(&name, "name", "stdin", "source name")
	cmd.Flags().BoolVar(&noOpen, "no-open", false, "don't open the browser")
	return cmd
}

// NewDockerCmd is `loggi docker <container>`.
func NewDockerCmd() *cobra.Command {
	var noOpen bool
	cmd := &cobra.Command{
		Use:   "docker <container>",
		Short: "Stream logs from a docker container",
		Args:  cobra.ExactArgs(1),
		RunE: func(_ *cobra.Command, args []string) error {
			return tailDocker(args[0], !noOpen)
		},
	}
	cmd.Flags().BoolVar(&noOpen, "no-open", false, "don't open the browser")
	return cmd
}

// NewWebCmd is `loggi web`.
func NewWebCmd() *cobra.Command {
	var noOpen bool
	cmd := &cobra.Command{
		Use:   "web",
		Short: "Ensure the server is running and open the web UI",
		RunE: func(_ *cobra.Command, _ []string) error {
			url, err := ensureServer()
			if err != nil {
				return err
			}
			fmt.Println(url)
			if !noOpen {
				_ = openBrowser(url)
			}
			return nil
		},
	}
	cmd.Flags().BoolVar(&noOpen, "no-open", false, "don't open the browser")
	return cmd
}

// RunDefault is the bare `loggi` command. It dispatches based on input:
//   - positional file args                 → tail each file as a source
//   - piped stdin (not a tty)              → ingest stdin as a named source
//   - both                                 → both, with --name applying to stdin
//   - neither                              → open the web UI on the default profile
func RunDefault(cmd *cobra.Command, args []string) error {
	noOpen, _ := cmd.Flags().GetBool("no-open")
	name, _ := cmd.Flags().GetString("name")
	hasFiles := len(args) > 0
	hasPipe := isPipedStdin()

	if hasFiles && hasPipe {
		if err := tailFiles(args, false); err != nil {
			return err
		}
		return runStdin(name, !noOpen)
	}
	if hasFiles {
		return tailFiles(args, !noOpen)
	}
	if hasPipe {
		return runStdin(name, !noOpen)
	}
	url, err := ensureServer()
	if err != nil {
		return err
	}
	fmt.Println(url)
	if noOpen {
		return nil
	}
	return openBrowser(url)
}

// isPipedStdin reports whether stdin is a pipe or redirected file (not a tty).
func isPipedStdin() bool {
	info, err := os.Stdin.Stat()
	if err != nil {
		return false
	}
	return info.Mode()&os.ModeCharDevice == 0
}

// --- impl ---

// ensureServer makes sure a server is running (auto-starting it) and returns
// its web URL.
func ensureServer() (string, error) {
	conn, err := client.Dial(true)
	if err == nil {
		_ = conn.Close()
		url := serverHTTPURL()
		if url == "" {
			return "", errors.New("server is running but its web address could not be determined")
		}
		return url, nil
	}
	// Dial failed — e.g. another loggi already holds the HTTP port, so our
	// daemon couldn't bind it. If a loggi is already serving the configured
	// address, reuse it (just open its UI) rather than failing.
	if url := serverHTTPURL(); url != "" && loggiServingAt(url) {
		return url, nil
	}
	return "", err
}

// loggiServingAt reports whether a loggi server answers /api/health at url.
func loggiServingAt(url string) bool {
	c := &http.Client{Timeout: 500 * time.Millisecond}
	resp, err := c.Get(url + "/api/health")
	if err != nil {
		return false
	}
	defer resp.Body.Close()
	return resp.StatusCode == http.StatusOK
}

// serverHTTPURL resolves the running server's web URL. runtime.json is
// authoritative (and the only source for a dynamic ":0" port), so it is tried
// first. When it's absent — a freshly spawned server may not have written it
// yet, or an older server is holding the socket and never wrote it at all — the
// URL is derived from the configured bind address, which the server binds to
// the same way.
func serverHTTPURL() string {
	if info, err := client.ReadRuntime(); err == nil {
		return info.HTTP
	}
	loaded, err := config.Load(mustGetwd())
	if err != nil {
		return ""
	}
	url := httpURLFromBind(loaded.Config.Server.HTTPBind)
	if strings.HasSuffix(url, ":0") {
		// Dynamic port: the bind address can't tell us the real port, so wait
		// for the server to publish it in runtime.json.
		if info, err := client.ReadRuntimeWait(2 * time.Second); err == nil {
			return info.HTTP
		}
	}
	return url
}

// httpURLFromBind turns a "host:port" bind address into an "http://host:port"
// URL, normalizing a wildcard/empty host to a loopback address a browser can
// open. An empty bind maps to the server's default, mirroring how the server
// resolves an unset HTTPBind.
func httpURLFromBind(bind string) string {
	if bind == "" {
		bind = server.DefaultHTTPBind
	}
	host, port, err := net.SplitHostPort(bind)
	if err != nil {
		return "http://" + bind
	}
	if host == "" || host == "0.0.0.0" || host == "::" {
		host = "127.0.0.1"
	}
	return "http://" + net.JoinHostPort(host, port)
}

func tailFiles(paths []string, openUI bool) error {
	conn, err := client.Dial(true)
	if err != nil {
		return err
	}
	defer conn.Close()
	for i, p := range paths {
		abs, _ := filepath.Abs(p)
		if err := conn.Write(&wire.ClientMsg{
			Type: wire.CMsgAddSource,
			ID:   uint64(i + 1),
			AddSource: &wire.AddSource{
				Kind: "file",
				Name: abs,
				Args: map[string]any{"path": abs},
			},
		}); err != nil {
			return err
		}
		var resp wire.ServerMsg
		// Drain until we see ack or err for this id.
		for {
			if err := conn.Read(&resp); err != nil {
				return err
			}
			if resp.Type == wire.SMsgAck && resp.Ack != nil && resp.Ack.RefID == uint64(i+1) {
				if !resp.Ack.OK {
					return errors.New(resp.Ack.Detail)
				}
				break
			}
			if resp.Type == wire.SMsgErr && resp.Err != nil && resp.Err.RefID == uint64(i+1) {
				return errors.New(resp.Err.Detail)
			}
		}
		fmt.Printf("added file source: %s\n", abs)
	}
	if openUI {
		if url := serverHTTPURL(); url != "" {
			_ = openBrowser(url)
		}
	}
	return nil
}

func tailDocker(name string, openUI bool) error {
	conn, err := client.Dial(true)
	if err != nil {
		return err
	}
	defer conn.Close()
	if err := conn.Write(&wire.ClientMsg{
		Type: wire.CMsgAddSource,
		ID:   1,
		AddSource: &wire.AddSource{
			Kind: "docker",
			Name: name,
		},
	}); err != nil {
		return err
	}
	var resp wire.ServerMsg
	for {
		if err := conn.Read(&resp); err != nil {
			return err
		}
		if resp.Type == wire.SMsgAck && resp.Ack != nil && resp.Ack.RefID == 1 {
			if !resp.Ack.OK {
				return errors.New(resp.Ack.Detail)
			}
			break
		}
		if resp.Type == wire.SMsgErr && resp.Err != nil && resp.Err.RefID == 1 {
			return errors.New(resp.Err.Detail)
		}
	}
	fmt.Printf("added docker source: %s\n", name)
	if openUI {
		if url := serverHTTPURL(); url != "" {
			_ = openBrowser(url)
		}
	}
	return nil
}

func runStdin(name string, openUI bool) error {
	conn, err := client.Dial(true)
	if err != nil {
		return err
	}
	defer conn.Close()
	if err := conn.Write(&wire.ClientMsg{
		Type:      wire.CMsgAddSource,
		ID:        1,
		AddSource: &wire.AddSource{Kind: "stdin", Name: name},
	}); err != nil {
		return err
	}
	var srcID uint64
	var resp wire.ServerMsg
	for {
		if err := conn.Read(&resp); err != nil {
			return err
		}
		if resp.Type == wire.SMsgAck && resp.Ack != nil && resp.Ack.RefID == 1 {
			if !resp.Ack.OK {
				return errors.New(resp.Ack.Detail)
			}
			srcID = resp.Ack.SrcID
			break
		}
		if resp.Type == wire.SMsgErr && resp.Err != nil && resp.Err.RefID == 1 {
			return errors.New(resp.Err.Detail)
		}
	}
	url := serverHTTPURL()
	if openUI && url != "" {
		_ = openBrowser(url)
	}
	if url != "" {
		fmt.Fprintf(os.Stderr, "loggi stdin: forwarding to source %d (web: %s)\n", srcID, url)
	} else {
		fmt.Fprintf(os.Stderr, "loggi stdin: forwarding to source %d\n", srcID)
	}

	// Read stdin in chunks; forward as StreamData frames. End with EOF=true.
	br := bufio.NewReaderSize(os.Stdin, 64<<10)
	chunk := make([]byte, 8192)
	var seq atomic.Uint64
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go func() {
		var sm wire.ServerMsg
		for {
			if err := conn.Read(&sm); err != nil {
				cancel()
				return
			}
			if sm.Type == wire.SMsgErr && sm.Err != nil {
				fmt.Fprintf(os.Stderr, "loggi stdin: server error: %s\n", sm.Err.Detail)
			}
		}
	}()
	for {
		select {
		case <-ctx.Done():
			return nil
		default:
		}
		n, err := br.Read(chunk)
		if n > 0 {
			cp := make([]byte, n)
			copy(cp, chunk[:n])
			id := seq.Add(1) + 100
			if err := conn.Write(&wire.ClientMsg{
				Type: wire.CMsgStreamData,
				ID:   id,
				StreamData: &wire.StreamData{
					SourceID: srcID,
					Chunk:    cp,
				},
			}); err != nil {
				return err
			}
		}
		if err != nil {
			_ = conn.Write(&wire.ClientMsg{
				Type: wire.CMsgStreamData,
				ID:   seq.Add(1) + 100,
				StreamData: &wire.StreamData{
					SourceID: srcID,
					EOF:      true,
				},
			})
			return nil
		}
	}
}

func openBrowser(url string) error {
	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "darwin":
		cmd = exec.Command("open", url)
	case "linux":
		cmd = exec.Command("xdg-open", url)
	case "windows":
		cmd = exec.Command("rundll32", "url.dll,FileProtocolHandler", url)
	default:
		return errors.New("unsupported os: " + runtime.GOOS)
	}
	return cmd.Start()
}
