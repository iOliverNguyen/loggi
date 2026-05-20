package app

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sync/atomic"

	"github.com/iOliverNguyen/loggi/internal/client"
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
			info, err := ensureServer()
			if err != nil {
				return err
			}
			fmt.Println(info.HTTP)
			if !noOpen {
				_ = openBrowser(info.HTTP)
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
	info, err := ensureServer()
	if err != nil {
		return err
	}
	fmt.Println(info.HTTP)
	if noOpen {
		return nil
	}
	return openBrowser(info.HTTP)
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

func ensureServer() (*client.RuntimeInfo, error) {
	conn, err := client.Dial(true)
	if err != nil {
		return nil, err
	}
	_ = conn.Close()
	return client.ReadRuntime()
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
	info, _ := client.ReadRuntime()
	if openUI && info != nil {
		_ = openBrowser(info.HTTP)
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
	info, _ := client.ReadRuntime()
	if openUI && info != nil {
		_ = openBrowser(info.HTTP)
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
		Type: wire.CMsgAddSource,
		ID:   1,
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
	info, _ := client.ReadRuntime()
	if openUI && info != nil {
		_ = openBrowser(info.HTTP)
	}
	if info != nil {
		fmt.Fprintf(os.Stderr, "loggi stdin: forwarding to source %d (web: %s)\n", srcID, info.HTTP)
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
