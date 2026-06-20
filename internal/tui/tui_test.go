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
		t.Fatalf("View() should contain 'agenttop' title, got:\n%s", out)
	}
	if !strings.Contains(out, "waiting for requests") {
		t.Fatalf("View() should show waiting message when empty, got:\n%s", out)
	}
}

func TestViewWithEvents(t *testing.T) {
	st, _ := store.New("", 100)
	bus := event.NewBus()
	m := New(st, bus, 7331)
	m.ready = true
	m.width = 120
	m.height = 40

	// Simulate an in-flight request (start event)
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
		t.Fatalf("View() should show provider, got:\n%s", out)
	}
	if !strings.Contains(out, "refactor the auth module") {
		// prompt should appear in the detail pane
		t.Fatalf("View() should show prompt preview in detail, got:\n%s", out)
	}

	// Simulate the response (end event with usage)
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
		CostUSD:          0.009, // 1500*3/1e6 + 300*15/1e6 + 500*0.3/1e6
		PromptPreview:    "refactor the auth module",
		ResponsePreview:  "I'll refactor the auth module to use...",
	}
	m.applyEvent(endEvt)

	out = m.View()
	if !strings.Contains(out, "200") {
		t.Fatalf("View() should show status 200, got:\n%s", out)
	}
	if !strings.Contains(out, "1500") {
		t.Fatalf("View() should show input tokens 1500, got:\n%s", out)
	}
	if !strings.Contains(out, "300") {
		t.Fatalf("View() should show output tokens 300, got:\n%s", out)
	}
	if !strings.Contains(out, "$0.009") {
		t.Fatalf("View() should show cost $0.009, got:\n%s", out)
	}
	// Detail pane should show the response
	if !strings.Contains(out, "I'll refactor the auth module") {
		t.Fatalf("View() should show response preview in detail, got:\n%s", out)
	}
}

func TestViewMultipleAgentsAndBurnRate(t *testing.T) {
	st, _ := store.New("", 100)
	bus := event.NewBus()
	m := New(st, bus, 7331)
	m.ready = true
	m.width = 120
	m.height = 40

	// Two completed requests from different providers.
	// In production the proxy appends to the store AND emits to the bus;
	// we simulate that here so header stats (from store) are populated too.
	for i, prov := range []string{"anthropic", "openai"} {
		model := "claude-sonnet-4-5"
		if prov == "openai" {
			model = "gpt-4o"
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
	if !strings.Contains(out, "oai") {
		t.Fatalf("should show openai provider abbreviation, got:\n%s", out)
	}
	// Total cost should be $0.01 (0.005 + 0.005) — from store stats in header
	if !strings.Contains(out, "$0.010") {
		t.Fatalf("should show total cost $0.010 in header, got:\n%s", out)
	}
	// Burn rate: $0.01 in last 60s → $0.60/h
	if !strings.Contains(out, "burn") {
		t.Fatalf("should show burn rate, got:\n%s", out)
	}
}
