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
	anth, _ := prov["anthropic"].(map[string]any)
	oai, _ := prov["openai"].(map[string]any)
	anthOpts, _ := anth["options"].(map[string]any)
	oaiOpts, _ := oai["options"].(map[string]any)
	if anthOpts["baseURL"] != "http://127.0.0.1:7331/v1" {
		t.Fatalf("anthropic baseURL = %v", anthOpts["baseURL"])
	}
	if oaiOpts["baseURL"] != "http://127.0.0.1:7331/v1" {
		t.Fatalf("openai baseURL = %v", oaiOpts["baseURL"])
	}
	if !strings.Contains(raw, "baseURL") {
		t.Fatal("expected baseURL in content")
	}
}
