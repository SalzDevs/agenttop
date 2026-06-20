package launch

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestIsOpencode(t *testing.T) {
	cases := map[string]bool{
		"opencode":                  true,
		"/usr/local/bin/opencode":   true,
		"./opencode":                true,
		"opencode-tui":              true,
		"claude":                    false,
		"codex":                     false,
		"/opt/homebrew/bin/claude":  false,
	}
	for cmd, want := range cases {
		if got := isOpencode(cmd); got != want {
			t.Errorf("isOpencode(%q) = %v, want %v", cmd, got, want)
		}
	}
}

func TestOpencodeConfigContent(t *testing.T) {
	raw := opencodeConfigContent(7331)
	var cfg map[string]any
	if err := json.Unmarshal([]byte(raw), &cfg); err != nil {
		t.Fatalf("config content is not valid JSON: %v", err)
	}
	prov, ok := cfg["provider"].(map[string]any)
	if !ok {
		t.Fatal("missing provider map")
	}
	// Standard providers
	anth, _ := prov["anthropic"].(map[string]any)
	oai, _ := prov["openai"].(map[string]any)
	anthOpts, _ := getOpts(anth)
	oaiOpts, _ := getOpts(oai)
	if anthOpts["baseURL"] != "http://127.0.0.1:7331/v1" {
		t.Fatalf("anthropic baseURL = %v", anthOpts["baseURL"])
	}
	if oaiOpts["baseURL"] != "http://127.0.0.1:7331/v1" {
		t.Fatalf("openai baseURL = %v", oaiOpts["baseURL"])
	}
	// opencode-go and opencode (Zen) must have baseURL + upstream header
	for _, pid := range []string{"opencode-go", "opencode"} {
		p, ok := prov[pid].(map[string]any)
		if !ok {
			t.Fatalf("missing %s provider in config", pid)
		}
		opts, _ := p["options"].(map[string]any)
		if opts["baseURL"] != "http://127.0.0.1:7331/v1" {
			t.Fatalf("%s baseURL = %v", pid, opts["baseURL"])
		}
		hdrs, _ := opts["headers"].(map[string]any)
		if hdrs["x-agenttop-upstream"] == nil || hdrs["x-agenttop-upstream"] == "" {
			t.Fatalf("%s missing x-agenttop-upstream header", pid)
		}
	}
	// opencode-go upstream should point to the real API (without /v1)
	goOpts, _ := getOpts(prov["opencode-go"].(map[string]any))
	goHdrs, _ := goOpts["headers"].(map[string]any)
	if goHdrs["x-agenttop-upstream"] != "https://opencode.ai/zen/go" {
		t.Fatalf("opencode-go upstream = %v, want https://opencode.ai/zen/go", goHdrs["x-agenttop-upstream"])
	}
	if !strings.Contains(raw, "baseURL") {
		t.Fatal("expected baseURL in content")
	}
}

func getOpts(p map[string]any) (map[string]any, bool) {
	if p == nil {
		return nil, false
	}
	return p["options"].(map[string]any), true
}

func TestEnvForAgent(t *testing.T) {
	env := envForAgent("claude", 7331)
	joined := strings.Join(env, "\n")
	if !strings.Contains(joined, "ANTHROPIC_BASE_URL=http://127.0.0.1:7331") {
		t.Fatal("missing ANTHROPIC_BASE_URL")
	}
	if strings.Contains(joined, "OPENCODE_CONFIG_CONTENT") {
		t.Fatal("non-opencode command must not set OPENCODE_CONFIG_CONTENT")
	}

	envOC := envForAgent("opencode", 7331)
	joinedOC := strings.Join(envOC, "\n")
	if !strings.Contains(joinedOC, "OPENCODE_CONFIG_CONTENT=") {
		t.Fatal("opencode command must set OPENCODE_CONFIG_CONTENT")
	}
}

func TestShellQuote(t *testing.T) {
	if got := shellQuote("simple"); got != "'simple'" {
		t.Fatalf("shellQuote(simple) = %q", got)
	}
	if got := shellQuote("it's"); got != "'it'\\''s'" {
		t.Fatalf("shellQuote(it's) = %q", got)
	}
}

func TestBuildTmuxCommandsClaude(t *testing.T) {
	steps := buildTmuxCommands("/usr/local/bin/agenttop", "agenttop", []string{"claude"}, 7331)

	var newSession, splitWindow string
	var setEnvVars []string
	for _, s := range steps {
		switch s.args[1] {
		case "new-session":
			newSession = strings.Join(s.args, " ")
		case "set-environment":
			// argv: tmux set-environment -t <sess> VAR value  → VAR at index 4
			setEnvVars = append(setEnvVars, s.args[4])
		case "split-window":
			splitWindow = strings.Join(s.args, " ")
		}
	}
	if !strings.Contains(newSession, "--port 7331 monitor") {
		t.Fatalf("new-session must run monitor subcommand: %s", newSession)
	}
	if !strings.Contains(newSession, "'/usr/local/bin/agenttop'") {
		t.Fatalf("self path must be shell-quoted: %s", newSession)
	}
	if !contains(setEnvVars, "ANTHROPIC_BASE_URL") || !contains(setEnvVars, "OPENAI_BASE_URL") {
		t.Fatalf("missing base URL env vars: %v", setEnvVars)
	}
	if contains(setEnvVars, "OPENCODE_CONFIG_CONTENT") {
		t.Fatalf("claude must not set OPENCODE_CONFIG_CONTENT: %v", setEnvVars)
	}
	// bottom pane: wait, then agent, then kill-session
	if !strings.Contains(splitWindow, "--port 7331 wait") {
		t.Fatalf("pane must wait for proxy: %s", splitWindow)
	}
	if !strings.Contains(splitWindow, "'claude'") {
		t.Fatalf("pane must run the agent shell-quoted: %s", splitWindow)
	}
	if !strings.Contains(splitWindow, "tmux kill-session -t 'agenttop'") {
		t.Fatalf("pane must kill session on exit: %s", splitWindow)
	}
	if !strings.HasPrefix(splitWindow, "tmux split-window -v -p 60 -t agenttop ") {
		t.Fatalf("unexpected split-window argv: %s", splitWindow)
	}
}

func TestBuildTmuxCommandsOpencode(t *testing.T) {
	steps := buildTmuxCommands("/bin/agenttop", "agenttop", []string{"opencode", "run", "hi"}, 9000)
	var setEnvVars []string
	var splitWindow string
	for _, s := range steps {
		if s.args[1] == "set-environment" {
			setEnvVars = append(setEnvVars, s.args[4])
		}
		if s.args[1] == "split-window" {
			splitWindow = strings.Join(s.args, " ")
		}
	}
	if !contains(setEnvVars, "OPENCODE_CONFIG_CONTENT") {
		t.Fatalf("opencode must set OPENCODE_CONFIG_CONTENT: %v", setEnvVars)
	}
	if !strings.Contains(splitWindow, "'opencode' 'run' 'hi'") {
		t.Fatalf("pane must shell-quote all agent args: %s", splitWindow)
	}
}

func contains(slice []string, want string) bool {
	for _, s := range slice {
		if s == want {
			return true
		}
	}
	return false
}
