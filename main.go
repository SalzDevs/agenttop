package main

import (
	"context"
	"flag"
	"fmt"
	"net/http"
	"os"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/SalzDevs/agenttop/internal/event"
	"github.com/SalzDevs/agenttop/internal/launch"
	"github.com/SalzDevs/agenttop/internal/proxy"
	"github.com/SalzDevs/agenttop/internal/store"
	"github.com/SalzDevs/agenttop/internal/tui"
)

const usage = `agenttop — htop for AI coding agents

Watch what Claude Code, Cursor, Codex, Gemini CLI and friends are doing
and what they're costing you. In real time. In your terminal.

Usage:
  agenttop                      start the monitor + proxy
  agenttop run -- <cmd> [args]  run an agent pointed at the monitor

Flags:
  --port, -p <n>   proxy listen port (default 7331)
  --log,  -l <p>   append events to a JSONL file (default: none)

Examples:
  agenttop
  # then in another terminal:
  agenttop run -- claude
  agenttop run -- codex
`

func main() {
	port := flag.Int("port", 7331, "proxy listen port")
	logPath := flag.String("log", "", "JSONL log path")
	flag.Usage = func() { fmt.Fprint(os.Stderr, usage) }
	flag.Parse()

	args := flag.Args()
	if len(args) > 0 && args[0] == "run" {
		rest := args[1:]
		for len(rest) > 0 && rest[0] == "--" {
			rest = rest[1:]
		}
		if err := launch.Wrap(rest, *port); err != nil {
			fmt.Fprintln(os.Stderr, "agenttop:", err)
			os.Exit(1)
		}
		return
	}

	st, err := store.New(*logPath, 1000)
	if err != nil {
		fmt.Fprintln(os.Stderr, "agenttop: open store:", err)
		os.Exit(1)
	}
	defer st.Close()

	bus := event.NewBus()
	p := proxy.New(st, bus, *port)
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

	launch.PrintInstructions(*port)

	m := tui.New(st, bus, *port)
	prog := tea.NewProgram(m, tea.WithAltScreen())
	if _, err := prog.Run(); err != nil {
		fmt.Fprintln(os.Stderr, "agenttop: tui:", err)
		os.Exit(1)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	_ = srv.Shutdown(ctx)
}
