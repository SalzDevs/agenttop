package launch

import (
	"encoding/json"
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"time"
)

func Instructions(port int) string {
	return fmt.Sprintf(`agenttop listening on http://127.0.0.1:%d

One command (recommended, needs tmux):
  agenttop claude
  agenttop opencode
  agenttop codex

Two-terminal mode (no tmux):
  terminal 1: agenttop
  terminal 2: agenttop run -- claude
`, port)
}

func PrintInstructions(port int) {
	fmt.Fprintln(os.Stderr, Instructions(port))
}

// isOpencode reports whether the command being launched is opencode.
// opencode does not read ANTHROPIC_BASE_URL / OPENAI_BASE_URL; instead it
// configures provider base URLs via its config file. We detect opencode by
// command basename and inject an inline runtime config (OPENCODE_CONFIG_CONTENT)
// that redirects its providers to the agenttop proxy.
func isOpencode(cmd string) bool {
	return strings.Contains(strings.ToLower(filepath.Base(cmd)), "opencode")
}

// opencodeConfigContent returns a minimal opencode config JSON that overrides
// the Anthropic and OpenAI provider base URLs to point at the agenttop proxy.
// It is merged with (and overrides) the user's own opencode config; API keys
// are stored separately in ~/.local/share/opencode/auth.json and are unaffected.
func opencodeConfigContent(port int) string {
	base := fmt.Sprintf("http://127.0.0.1:%d/v1", port)
	cfg := map[string]any{
		"provider": map[string]any{
			"anthropic": map[string]any{
				"options": map[string]any{"baseURL": base},
			},
			"openai": map[string]any{
				"options": map[string]any{"baseURL": base},
			},
		},
	}
	b, err := json.Marshal(cfg)
	if err != nil {
		return ""
	}
	return string(b)
}

// envForAgent returns the extra environment entries needed to point an agent at
// the agenttop proxy on the given port. For opencode it also injects
// OPENCODE_CONFIG_CONTENT so its provider config redirects to the proxy.
func envForAgent(cmd string, port int) []string {
	env := []string{
		fmt.Sprintf("ANTHROPIC_BASE_URL=http://127.0.0.1:%d", port),
		fmt.Sprintf("OPENAI_BASE_URL=http://127.0.0.1:%d/v1", port),
		fmt.Sprintf("OPENAI_API_BASE=http://127.0.0.1:%d/v1", port),
	}
	if isOpencode(cmd) {
		env = append(env, fmt.Sprintf("OPENCODE_CONFIG_CONTENT=%s", opencodeConfigContent(port)))
	}
	return env
}

// WaitPort blocks until something is listening on 127.0.0.1:port (up to ~10s),
// then returns. Used so an agent doesn't start before the monitor's proxy is up.
func WaitPort(port int) {
	addr := fmt.Sprintf("127.0.0.1:%d", port)
	for i := 0; i < 100; i++ {
		conn, err := net.DialTimeout("tcp", addr, 200*time.Millisecond)
		if err == nil {
			conn.Close()
			return
		}
		time.Sleep(100 * time.Millisecond)
	}
}

// Wrap runs a single agent command with the proxy env vars injected, after
// waiting for the proxy to be ready. Returns the child's exit code.
func Wrap(args []string, port int) (int, error) {
	if len(args) == 0 {
		return 2, fmt.Errorf("no command given after --")
	}
	WaitPort(port)
	cmd := exec.Command(args[0], args[1:]...)
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	cmd.Env = append(os.Environ(), envForAgent(args[0], port)...)
	if err := cmd.Start(); err != nil {
		return 1, err
	}
	if err := cmd.Wait(); err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			if status, ok := exitErr.Sys().(syscall.WaitStatus); ok {
				return status.ExitStatus(), nil
			}
			return 1, nil
		}
		return 1, err
	}
	return 0, nil
}

// tmuxStep is one tmux invocation (argv) executed in order by TmuxSplit.
type tmuxStep struct{ args []string }

// buildTmuxCommands constructs the ordered tmux argv list for a one-command
// split session. It is a pure function so it can be unit-tested without tmux.
//
// Top pane:    "<self> --port <port> monitor"   (starts proxy + monitor TUI)
// Bottom pane: "<self> --port <port> wait; <agent>; tmux kill-session -t <sess>"
//
// The agent inherits ANTHROPIC_BASE_URL / OPENAI_BASE_URL / OPENAI_API_BASE
// (and OPENCODE_CONFIG_CONTENT for opencode) from the session environment, set
// via `tmux set-environment` so the agent pane receives them.
func buildTmuxCommands(self, sess string, agentCmd []string, port int) []tmuxStep {
	baseURL := fmt.Sprintf("http://127.0.0.1:%d", port)
	baseURLv1 := fmt.Sprintf("http://127.0.0.1:%d/v1", port)
	monitorShell := shellQuote(self) + fmt.Sprintf(" --port %d monitor", port)
	waitShell := shellQuote(self) + fmt.Sprintf(" --port %d wait", port)
	agentShell := shellJoin(agentCmd)
	killShell := "tmux kill-session -t " + shellQuote(sess)
	paneShell := waitShell + "; " + agentShell + "; " + killShell

	steps := []tmuxStep{
		{args: []string{"tmux", "kill-session", "-t", sess}},
		{args: []string{"tmux", "new-session", "-d", "-s", sess, monitorShell}},
		{args: []string{"tmux", "set-environment", "-t", sess, "ANTHROPIC_BASE_URL", baseURL}},
		{args: []string{"tmux", "set-environment", "-t", sess, "OPENAI_BASE_URL", baseURLv1}},
		{args: []string{"tmux", "set-environment", "-t", sess, "OPENAI_API_BASE", baseURLv1}},
	}
	if len(agentCmd) > 0 && isOpencode(agentCmd[0]) {
		steps = append(steps, tmuxStep{args: []string{"tmux", "set-environment", "-t", sess, "OPENCODE_CONFIG_CONTENT", opencodeConfigContent(port)}})
	}
	steps = append(steps,
		tmuxStep{args: []string{"tmux", "split-window", "-v", "-p", "60", "-t", sess, paneShell}},
	)
	return steps
}

// TmuxSplit creates a tmux session with the monitor on top and the agent on the
// bottom, then attaches. When the agent exits, the pane shell kills the session
// (so detaching returns). If tmux isn't installed, it prints install hints and
// the two-terminal fallback and returns an error.
func TmuxSplit(agentCmd []string, port int) error {
	if len(agentCmd) == 0 {
		return fmt.Errorf("no command given")
	}
	if _, err := exec.LookPath("tmux"); err != nil {
		fmt.Fprintln(os.Stderr, "tmux not found — install it for the one-command split:")
		fmt.Fprintln(os.Stderr, "  brew install tmux   (macOS)")
		fmt.Fprintln(os.Stderr, "  apt-get install tmux (Ubuntu/Debian)")
		fmt.Fprintln(os.Stderr)
		fmt.Fprintln(os.Stderr, "Until then, use two terminals:")
		fmt.Fprintf(os.Stderr, "  1) agenttop --port %d\n", port)
		fmt.Fprintf(os.Stderr, "  2) agenttop run -- %s\n", strings.Join(agentCmd, " "))
		return fmt.Errorf("tmux not installed")
	}

	self, err := os.Executable()
	if err != nil {
		return err
	}
	sess := "agenttop"
	for _, s := range buildTmuxCommands(self, sess, agentCmd, port) {
		c := exec.Command(s.args[0], s.args[1:]...)
		c.Stderr = os.Stderr
		if err := c.Run(); err != nil {
			// kill-session fails if no session exists; ignore that one.
			if s.args[1] != "kill-session" {
				return fmt.Errorf("tmux: %v", err)
			}
		}
	}

	attach := exec.Command("tmux", "attach", "-t", sess)
	attach.Stdin = os.Stdin
	attach.Stdout = os.Stdout
	attach.Stderr = os.Stderr
	return attach.Run()
}

// shellQuote wraps s in single quotes for safe shell use, escaping any inner
// single quotes via the standard '\'' idiom.
func shellQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}

// shellJoin joins args into a single shell-quoted command string.
func shellJoin(args []string) string {
	parts := make([]string, len(args))
	for i, a := range args {
		parts[i] = shellQuote(a)
	}
	return strings.Join(parts, " ")
}
