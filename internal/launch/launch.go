package launch

import (
	"fmt"
	"os"
	"os/exec"
	"syscall"
)

func Instructions(port int) string {
	return fmt.Sprintf(`agenttop listening on http://127.0.0.1:%d

Point your agent at it:
  ANTHROPIC_BASE_URL=http://127.0.0.1:%d ANTHROPIC_API_KEY=$ANTHROPIC_API_KEY claude
  OPENAI_BASE_URL=http://127.0.0.1:%d/v1 OPENAI_API_KEY=$OPENAI_API_KEY codex

Or use the wrapper:
  agenttop run -- claude
`, port, port, port)
}

func PrintInstructions(port int) {
	fmt.Fprintln(os.Stderr, Instructions(port))
}

func Wrap(args []string, port int) error {
	if len(args) == 0 {
		return fmt.Errorf("no command given after --")
	}
	cmd := exec.Command(args[0], args[1:]...)
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	cmd.Env = append(os.Environ(),
		fmt.Sprintf("ANTHROPIC_BASE_URL=http://127.0.0.1:%d", port),
		fmt.Sprintf("OPENAI_BASE_URL=http://127.0.0.1:%d/v1", port),
		fmt.Sprintf("OPENAI_API_BASE=http://127.0.0.1:%d/v1", port),
	)
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
