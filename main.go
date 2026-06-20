package main

import (
	"context"
	"flag"
	"fmt"
	"net/http"
	"os"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/SalzDevs/agenttop/internal/event"
	"github.com/SalzDevs/agenttop/internal/launch"
	"github.com/SalzDevs/agenttop/internal/proxy"
	"github.com/SalzDevs/agenttop/internal/store"
	"github.com/SalzDevs/agenttop/internal/tui"
)

const usage = `agenttop — htop for AI coding agents

Watch what Claude Code, Cursor, Codex, OpenCode and Gemini CLI are doing
and what they're costing you. In real time. In your terminal.

One command (recommended — needs tmux):
  agenttop claude            monitor on top, claude below, in one window
  agenttop opencode          same, for OpenCode
  agenttop codex             same, for Codex CLI
  agenttop --port 8000 claude   pick the proxy port

Just the monitor (two-terminal mode, no tmux):
  agenttop                   start the monitor + proxy
  agenttop run -- claude     run an agent pointed at the monitor (other terminal)

Flags:
  --port, -p <n>   proxy listen port (default 7331)
  --log,  -l <p>   append events to a JSONL file (default: none)
`

func main() {
	port := flag.Int("port", 7331, "proxy listen port")
	logPath := flag.String("log", "", "JSONL log path")
	flag.Usage = func() { fmt.Fprint(os.Stderr, usage) }
	flag.Parse()

	args := flag.Args()

	// Bare `agenttop` → start the monitor (two-terminal mode).
	if len(args) == 0 {
		runMonitor(*port, *logPath)
		return
	}

	switch args[0] {
	case "monitor":
		// `agenttop monitor` → just the monitor (used by the tmux top pane too).
		runMonitor(*port, *logPath)
	case "run":
		// `agenttop run -- <cmd> [args]` → wrapper only (waits for proxy, injects env).
		rest := stripDash(args[1:])
		code, err := launch.Wrap(rest, *port)
		if err != nil {
			fmt.Fprintln(os.Stderr, "agenttop:", err)
			os.Exit(1)
		}
		os.Exit(code)
	case "wait":
		// `agenttop wait` → block until the proxy is listening (used by tmux bottom pane).
		launch.WaitPort(*port)
	case "help", "-h", "--help":
		fmt.Fprint(os.Stdout, usage)
	default:
		// `agenttop <agent> [args]` → one-command tmux split.
		if err := launch.TmuxSplit(args, *port); err != nil {
			// Tallback already printed by TmuxSplit; exit non-zero only for real errors
			// (tmux missing is a guidance case, not a crash).
			if err.Error() != "tmux not installed" {
				fmt.Fprintln(os.Stderr, "agenttop:", err)
				os.Exit(1)
			}
			os.Exit(1)
		}
	}
}

// stripDash drops leading "--" tokens (so `run -- claude` and `run claude` both work).
func stripDash(args []string) []string {
	for len(args) > 0 && args[0] == "--" {
		args = args[1:]
	}
	return args
}

// runMonitor starts the proxy + TUI monitor. Used by `agenttop`, `agenttop monitor`,
// and the tmux top pane.
func runMonitor(port int, logPath string) {
	st, err := store.New(logPath, 1000)
	if err != nil {
		fmt.Fprintln(os.Stderr, "agenttop: open store:", err)
		os.Exit(1)
	}
	defer st.Close()

	bus := event.NewBus()
	p := proxy.New(st, bus, port)
	srv := &http.Server{
		Addr:              p.ListenAddr(),
		Handler:           p,
		ReadHeaderTimeout: 10 * time.Second,
	}
	go func() {
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			fmt.Fprintln(os.Stderr, "agenttop: proxy:", err)
		}
	}()

	launch.PrintInstructions(port)

	m := tui.New(st, bus, port)
	prog := tea.NewProgram(m, tea.WithAltScreen())
	if _, err := prog.Run(); err != nil {
		// If there's no TTY (e.g. piped output), fall back to just running the proxy
		// so `agenttop run` in another terminal still works.
		if strings.Contains(err.Error(), "TTY") || strings.Contains(err.Error(), "tty") {
			fmt.Fprintf(os.Stderr, "agenttop: no TTY, running proxy-only on :%d (press Ctrl+C to stop)\n", port)
			select {}
		}
		fmt.Fprintln(os.Stderr, "agenttop: tui:", err)
		os.Exit(1)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	_ = srv.Shutdown(ctx)
}
