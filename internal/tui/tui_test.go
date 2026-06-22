package tui

import (
	"strings"
	"testing"
	"time"

	"github.com/SalzDevs/agenttop/internal/event"
	"github.com/SalzDevs/agenttop/internal/store"
)

func TestViewEmpty(t *testing.T) {
	st, _ := store.New("", 100)
	bus := event.NewBus()
	m := New(st, bus, 7331)
	m.ready = true
	m.width = 120
	m.height = 40

	out := m.View()
	if !strings.Contains(out, "agenttop") {
		t.Fatalf("View() should contain 'agenttop' brand, got:\n%s", out)
	}
	if !strings.Contains(out, "waiting for requests") {
		t.Fatalf("View() should show waiting message when empty, got:\n%s", out)
	}
	if !strings.Contains(out, "TOTAL COST") {
		t.Fatalf("View() should show stat box labels, got:\n%s", out)
	}
	if !strings.Contains(out, "BURN/HR") {
		t.Fatalf("View() should show burn rate box, got:\n%s", out)
	}
}

func TestViewWithEvents(t *testing.T) {
	st, _ := store.New("", 100)
	bus := event.NewBus()
	m := New(st, bus, 7331)
	m.ready = true
	m.width = 120
	m.height = 40

	startEvt := event.Event{
		TraceID:       1,
		Time:          time.Now(),
		Provider:      "anthropic",
		Model:         "claude-sonnet-4-5",
		Endpoint:      "/v1/messages",
		Method:        "POST",
		Streaming:     true,
		PromptPreview: "refactor the auth module",
	}
	m.applyEvent(startEvt)

	out := m.View()
	if !strings.Contains(out, "claude-sonnet-4-5") {
		t.Fatalf("View() should show model name, got:\n%s", out)
	}
	if !strings.Contains(out, "anth") {
		t.Fatalf("View() should show provider badge, got:\n%s", out)
	}
	if !strings.Contains(out, "refactor the auth module") {
		t.Fatalf("View() should show prompt in detail, got:\n%s", out)
	}

	endEvt := event.Event{
		TraceID:          1,
		Time:             time.Now(),
		Provider:         "anthropic",
		Model:            "claude-sonnet-4-5",
		Endpoint:         "/v1/messages",
		Method:           "POST",
		Status:           200,
		Streaming:        true,
		Duration:         2 * time.Second,
		InputTokens:      1500,
		OutputTokens:     300,
		CacheReadTokens:  500,
		CacheWriteTokens: 0,
		CostUSD:          0.009,
		PromptPreview:    "refactor the auth module",
		ResponsePreview:  "I'll refactor the auth module to use...",
	}
	st.Append(endEvt)
	m.applyEvent(endEvt)

	out = m.View()
	if !strings.Contains(out, "200") {
		t.Fatalf("View() should show status 200, got:\n%s", out)
	}
	if !strings.Contains(out, "1500") {
		t.Fatalf("View() should show input tokens 1500, got:\n%s", out)
	}
	if !strings.Contains(out, "$0.009") {
		t.Fatalf("View() should show cost $0.009, got:\n%s", out)
	}
	if !strings.Contains(out, "I'll refactor the auth module") {
		t.Fatalf("View() should show response preview in detail, got:\n%s", out)
	}
}

func TestViewMultipleProvidersAndCost(t *testing.T) {
	st, _ := store.New("", 100)
	bus := event.NewBus()
	m := New(st, bus, 7331)
	m.ready = true
	m.width = 120
	m.height = 40

	for i, prov := range []string{"anthropic", "openai", "opencode-go"} {
		model := "claude-sonnet-4-5"
		if prov == "openai" {
			model = "gpt-4o"
		}
		if prov == "opencode-go" {
			model = "glm-5.2"
		}
		e := event.Event{
			TraceID: int64(i + 1), Time: time.Now(), Provider: prov, Model: model,
			Status: 200, InputTokens: 1000, OutputTokens: 200, CostUSD: 0.005,
			PromptPreview: "test query", ResponsePreview: "test response",
		}
		st.Append(e)
		m.applyEvent(e)
	}

	out := m.View()
	if !strings.Contains(out, "claude-sonnet-4-5") {
		t.Fatalf("should show claude model, got:\n%s", out)
	}
	if !strings.Contains(out, "gpt-4o") {
		t.Fatalf("should show gpt-4o model, got:\n%s", out)
	}
	if !strings.Contains(out, "glm-5.2") {
		t.Fatalf("should show glm-5.2 model, got:\n%s", out)
	}
	if !strings.Contains(out, "oai") {
		t.Fatalf("should show openai provider badge, got:\n%s", out)
	}
	if !strings.Contains(out, "oc") {
		t.Fatalf("should show opencode provider badge, got:\n%s", out)
	}
	if !strings.Contains(out, "$0.015") {
		t.Fatalf("should show total cost $0.015 in header, got:\n%s", out)
	}
}

func TestSparklineRendering(t *testing.T) {
	st, _ := store.New("", 100)
	bus := event.NewBus()
	m := New(st, bus, 7331)
	m.ready = true
	m.width = 120
	m.height = 40

	// Add some cost events to populate spark data
	for i := 0; i < 10; i++ {
		e := event.Event{
			TraceID: int64(i + 1), Time: time.Now(), Provider: "anthropic", Model: "claude-sonnet-4-5",
			Status: 200, InputTokens: 100, OutputTokens: 50, CostUSD: 0.001 * float64(i+1),
		}
		st.Append(e)
		m.applyEvent(e)
	}

	// Simulate ticks to fill spark buckets (updateSpark is called every 500ms tick,
	// and commits a bucket every 4 ticks = 2 seconds)
	for i := 0; i < 20; i++ {
		m.updateSpark()
	}

	out := m.View()
	if len(m.sparkData) >= 2 {
		if !strings.Contains(out, "burn") {
			t.Fatalf("should show sparkline with burn label, got:\n%s", out)
		}
		if !strings.Contains(out, "/h") {
			t.Fatalf("should show burn rate per hour in sparkline, got:\n%s", out)
		}
	}
}

func TestProviderBadge(t *testing.T) {
	cases := map[string]string{
		"anthropic":   "anth",
		"openai":      "oai",
		"opencode":    "oc",
		"opencode-go": "oc",
	}
	for provider, want := range cases {
		got := providerBadge(provider)
		if !strings.Contains(got, want) {
			t.Errorf("providerBadge(%q) = %q, want to contain %q", provider, got, want)
		}
	}
}
