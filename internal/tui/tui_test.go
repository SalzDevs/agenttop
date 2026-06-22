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
		t.Fatalf("View() should show model name in detail, got:\n%s", out)
	}
	if !strings.Contains(out, "anthropic") {
		t.Fatalf("View() should show provider in detail pane, got:\n%s", out)
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
	if !strings.Contains(out, "$0.009") {
		t.Fatalf("View() should show cost $0.009 in detail, got:\n%s", out)
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
	// With the table removed, model names only appear in the detail pane
	// (showing the selected row). The header shows aggregate stats.
	if !strings.Contains(out, "$0.015") {
		t.Fatalf("should show total cost $0.015 in header, got:\n%s", out)
	}
	if !strings.Contains(out, "3000") {
		t.Fatalf("should show total input tokens 3000 in header, got:\n%s", out)
	}
	if !strings.Contains(out, "600") {
		t.Fatalf("should show total output tokens 600 in header, got:\n%s", out)
	}
	if !strings.Contains(out, "REQUESTS") {
		t.Fatalf("should show requests stat box, got:\n%s", out)
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

func TestSelectorShowsRequests(t *testing.T) {
	st, _ := store.New("", 100)
	bus := event.NewBus()
	m := New(st, bus, 7331)
	m.ready = true
	m.width = 120
	m.height = 40

	// Add 3 events
	for i := 0; i < 3; i++ {
		e := event.Event{
			TraceID: int64(i + 1), Time: time.Now(), Provider: "anthropic",
			Model: "claude-sonnet-4-5", Status: 200, InputTokens: 100, OutputTokens: 50,
			CostUSD: 0.005, PromptPreview: "test", ResponsePreview: "resp",
		}
		st.Append(e)
		m.applyEvent(e)
	}

	out := m.View()
	// Should show request selector with all 3 models
	if !strings.Contains(out, "claude-sonnet-4-5") {
		t.Fatalf("selector should show model name, got:\n%s", out)
	}
	// Should show the selected marker
	if !strings.Contains(out, "▶") {
		t.Fatalf("selector should show selection marker, got:\n%s", out)
	}
}
