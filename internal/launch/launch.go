package launch

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
)

func Instructions(port int) string {
	return fmt.Sprintf(`agenttop listening on http://127.0.0.1:%d

Point your agent at it:
  ANTHROPIC_BASE_URL=http://127.0.0.1:%d ANTHROPIC_API_KEY=$ANTHROPIC_API_KEY claude
  OPENAI_BASE_URL=http://127.0.0.1:%d/v1 OPENAI_API_KEY=$OPENAI_API_KEY codex

Or use the wrapper (env vars / config injected automatically):
  agenttop run -- claude
  agenttop run -- codex
  agenttop run -- opencode
`, port, port, port)
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

func Wrap(args []string, port int) error {
	if len(args) == 0 {
		return fmt.Errorf("no command given after --")
	}
	cmd := exec.Command(args[0], args[1:]...)
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	env := append(os.Environ(),
		fmt.Sprintf("ANTHROPIC_BASE_URL=http://127.0.0.1:%d", port),
		fmt.Sprintf("OPENAI_BASE_URL=http://127.0.0.1:%d/v1", port),
		fmt.Sprintf("OPENAI_API_BASE=http://127.0.0.1:%d/v1", port),
	)
	if isOpencode(args[0]) {
		env = append(env, fmt.Sprintf("OPENCODE_CONFIG_CONTENT=%s", opencodeConfigContent(port)))
	}
	cmd.Env = env
	if err := cmd.Start(); err != nil {
		return err
	}
	if err := cmd.Wait(); err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			if status, ok := exitErr.Sys().(syscall.WaitStatus); ok {
				os.Exit(status.ExitStatus())
			}
			os.Exit(1)
		}
		return err
	}
	return nil
}
