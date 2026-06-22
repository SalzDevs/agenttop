package store

import (
	"testing"

	"github.com/SalzDevs/agenttop/internal/event"
)

func TestInFlightCounting(t *testing.T) {
	st, _ := New("", 100)

	// Simulate what the proxy does: emit a start event (in-flight), then an
	// end event (completed) for the same trace.
	st.Append(event.Event{TraceID: 1, Provider: "anthropic", Model: "claude", Status: 0})
	st.Append(event.Event{TraceID: 2, Provider: "openai", Model: "gpt-4o", Status: 0})

	_, _, _, _, inFlight := st.Stats()
	if inFlight != 2 {
		t.Fatalf("after 2 start events, inFlight = %d, want 2", inFlight)
	}

	// Complete trace 1
	st.Append(event.Event{TraceID: 1, Provider: "anthropic", Model: "claude", Status: 200, InputTokens: 100, OutputTokens: 50, CostUSD: 0.005})
	_, _, _, _, inFlight = st.Stats()
	if inFlight != 1 {
		t.Fatalf("after completing trace 1, inFlight = %d, want 1", inFlight)
	}

	// Complete trace 2
	st.Append(event.Event{TraceID: 2, Provider: "openai", Model: "gpt-4o", Status: 200, InputTokens: 200, OutputTokens: 100, CostUSD: 0.01})
	_, _, _, _, inFlight = st.Stats()
	if inFlight != 0 {
		t.Fatalf("after completing all traces, inFlight = %d, want 0", inFlight)
	}

	// Verify totals
	cost, in, out, reqs, _ := st.Stats()
	if reqs != 2 {
		t.Fatalf("reqs = %d, want 2", reqs)
	}
	if cost != 0.015 {
		t.Fatalf("cost = %.4f, want 0.015", cost)
	}
	if in != 300 || out != 150 {
		t.Fatalf("tokens: in=%d out=%d, want in=300 out=150", in, out)
	}
}

func TestRingBuffer(t *testing.T) {
	st, _ := New("", 5)
	for i := 0; i < 10; i++ {
		st.Append(event.Event{TraceID: int64(i), Status: 200})
	}
	events := st.Recent(0)
	if len(events) > 5 {
		t.Fatalf("ring buffer should cap at 5, got %d", len(events))
	}
}
