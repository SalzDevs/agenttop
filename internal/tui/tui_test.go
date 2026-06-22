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
		t.Fatalf("should contain brand, got:\n%s", out)
	}
	if !strings.Contains(out, "cost") {
		t.Fatalf("should show cost label, got:\n%s", out)
	}
	if !strings.Contains(out, "burn") {
		t.Fatalf("should show burn label, got:\n%s", out)
	}
	if !strings.Contains(out, "waiting for requests") {
		t.Fatalf("should show waiting message, got:\n%s", out)
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
		TraceID: 1, Time: time.Now(), Provider: "anthropic", Model: "claude-sonnet-4-5",
		Streaming: true, PromptPreview: "refactor the auth module",
	}
	m.applyEvent(startEvt)

	out := m.View()
	if !strings.Contains(out, "claude-sonnet-4-5") {
		t.Fatalf("should show model name, got:\n%s", out)
	}
	if !strings.Contains(out, "refactor the auth module") {
		t.Fatalf("should show prompt in detail, got:\n%s", out)
	}

	endEvt := event.Event{
		TraceID: 1, Time: time.Now(), Provider: "anthropic", Model: "claude-sonnet-4-5",
		Status: 200, Duration: 2 * time.Second, InputTokens: 1500, OutputTokens: 300,
		CostUSD: 0.009, PromptPreview: "refactor the auth module",
		ResponsePreview: "I'll refactor the auth module to use...",
	}
	st.Append(endEvt)
	m.applyEvent(endEvt)

	out = m.View()
	if !strings.Contains(out, "$0.009") {
		t.Fatalf("should show cost, got:\n%s", out)
	}
	if !strings.Contains(out, "I'll refactor the auth module") {
		t.Fatalf("should show response, got:\n%s", out)
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
		}
		st.Append(e)
		m.applyEvent(e)
	}

	out := m.View()
	if !strings.Contains(out, "$0.015") {
		t.Fatalf("should show total cost $0.015, got:\n%s", out)
	}
	if !strings.Contains(out, "3000") {
		t.Fatalf("should show total input tokens, got:\n%s", out)
	}
	if !strings.Contains(out, "600") {
		t.Fatalf("should show total output tokens, got:\n%s", out)
	}
}

func TestSparklineRendering(t *testing.T) {
	st, _ := store.New("", 100)
	bus := event.NewBus()
	m := New(st, bus, 7331)
	m.ready = true
	m.width = 120
	m.height = 40

	for i := 0; i < 10; i++ {
		e := event.Event{
			TraceID: int64(i + 1), Time: time.Now(), Provider: "anthropic", Model: "test",
			Status: 200, CostUSD: 0.001 * float64(i+1),
		}
		st.Append(e)
		m.applyEvent(e)
	}

	for i := 0; i < 20; i++ {
		m.updateSpark()
	}

	out := m.View()
	if len(m.sparkData) >= 2 {
		if !strings.Contains(out, "burn") {
			t.Fatalf("should show sparkline, got:\n%s", out)
		}
	}
}

func TestListShowsRequests(t *testing.T) {
	st, _ := store.New("", 100)
	bus := event.NewBus()
	m := New(st, bus, 7331)
	m.ready = true
	m.width = 120
	m.height = 40

	for i := 0; i < 3; i++ {
		e := event.Event{
			TraceID: int64(i + 1), Time: time.Now(), Provider: "anthropic",
			Model: "claude-sonnet-4-5", Status: 200, InputTokens: 100, OutputTokens: 50,
			CostUSD: 0.005,
		}
		st.Append(e)
		m.applyEvent(e)
	}

	out := m.View()
	if !strings.Contains(out, "claude-sonnet-4-5") {
		t.Fatalf("list should show model name, got:\n%s", out)
	}
}

func TestNoKeyCommands(t *testing.T) {
	st, _ := store.New("", 100)
	bus := event.NewBus()
	m := New(st, bus, 7331)
	m.ready = true
	m.width = 120
	m.height = 40

	out := m.View()
	for _, key := range []string{"quit", "select", "toggle", "bottom", "q quit", "jk", "↑↓"} {
		if strings.Contains(strings.ToLower(out), strings.ToLower(key)) {
			t.Fatalf("should not contain keybinding '%s', got:\n%s", key, out)
		}
	}
}

func TestLogoPresent(t *testing.T) {
	st, _ := store.New("", 100)
	bus := event.NewBus()
	m := New(st, bus, 7331)
	m.ready = true
	m.width = 120
	m.height = 40

	out := m.View()
	if !strings.Contains(out, logo) {
		t.Fatalf("should contain logo %q, got:\n%s", logo, out)
	}
}
