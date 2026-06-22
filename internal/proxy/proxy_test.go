package proxy

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/SalzDevs/agenttop/internal/event"
	"github.com/SalzDevs/agenttop/internal/pricing"
	"github.com/SalzDevs/agenttop/internal/store"
)

func flushSSE(w http.ResponseWriter, f http.Flusher, s string) {
	w.Write([]byte(s))
	f.Flush()
}

func anthropicStreamMock(t *testing.T) *httptest.Server {
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		f := w.(http.Flusher)
		flushSSE(w, f, "event: message_start\n")
		flushSSE(w, f, `data: {"type":"message_start","message":{"id":"msg_1","type":"message","role":"assistant","model":"claude-sonnet-4-5","usage":{"input_tokens":100,"cache_read_input_tokens":50,"cache_creation_input_tokens":0,"output_tokens":0}}}`+"\n\n")
		flushSSE(w, f, "event: content_block_delta\n")
		flushSSE(w, f, `data: {"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":"Hello world"}}`+"\n\n")
		flushSSE(w, f, "event: message_delta\n")
		flushSSE(w, f, `data: {"type":"message_delta","delta":{"stop_reason":"end_turn"},"usage":{"output_tokens":200}}`+"\n\n")
		flushSSE(w, f, "event: message_stop\n")
		flushSSE(w, f, `data: {"type":"message_stop"}`+"\n\n")
	}))
}

func openAIStreamMock(t *testing.T) *httptest.Server {
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		f := w.(http.Flusher)
		flushSSE(w, f, `data: {"choices":[{"delta":{"content":"Hi there"}}]}`+"\n\n")
		flushSSE(w, f, `data: {"choices":[],"usage":{"prompt_tokens":80,"completion_tokens":40}}`+"\n\n")
		flushSSE(w, f, "data: [DONE]\n\n")
	}))
}

func openAIJSONMock(t *testing.T) *httptest.Server {
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"model":  "gpt-4o",
			"usage":  map[string]int{"prompt_tokens": 80, "completion_tokens": 40, "total_tokens": 120},
			"choices": []map[string]any{{"message": map[string]string{"content": "Hi there"}}},
		})
	}))
}

func post(t *testing.T, proxyURL, path string, body any) []byte {
	b, err := json.Marshal(body)
	if err != nil {
		t.Fatal(err)
	}
	req, _ := http.NewRequest("POST", proxyURL+path, bytes.NewReader(b))
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	out, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != 200 {
		t.Fatalf("status %d body %s", resp.StatusCode, out)
	}
	return out
}

func TestAnthropicStreamingCapture(t *testing.T) {
	mock := anthropicStreamMock(t)
	defer mock.Close()

	st, _ := store.New("", 100)
	bus := event.NewBus()
	p := New(st, bus, 0)
	p.AnthropicTarget = mock.URL
	proxySrv := httptest.NewServer(p)
	defer proxySrv.Close()

	body := map[string]any{
		"model":    "claude-sonnet-4-5",
		"stream":   true,
		"messages": []map[string]any{{"role": "user", "content": "say hi"}},
	}
	out := post(t, proxySrv.URL, "/v1/messages", body)
	if !bytes.Contains(out, []byte("Hello world")) {
		t.Fatalf("streamed body did not pass through: %s", out)
	}

	ev := lastNonInflight(st)
	if ev.Provider != "anthropic" {
		t.Fatalf("provider = %q", ev.Provider)
	}
	if ev.InputTokens != 100 || ev.OutputTokens != 200 || ev.CacheReadTokens != 50 {
		t.Fatalf("tokens: in=%d out=%d cacheRead=%d", ev.InputTokens, ev.OutputTokens, ev.CacheReadTokens)
	}
	want := pricing.Cost("claude-sonnet-4-5", 100, 200, 50, 0)
	if !almost(ev.CostUSD, want) {
		t.Fatalf("cost = %.6f want %.6f", ev.CostUSD, want)
	}
	if ev.ResponsePreview == "" {
		t.Fatalf("response preview empty")
	}
	if ev.PromptPreview != "say hi" {
		t.Fatalf("prompt preview = %q", ev.PromptPreview)
	}
}

func TestOpenAIStreamingCapture(t *testing.T) {
	mock := openAIStreamMock(t)
	defer mock.Close()

	st, _ := store.New("", 100)
	bus := event.NewBus()
	p := New(st, bus, 0)
	p.OpenAITarget = mock.URL
	proxySrv := httptest.NewServer(p)
	defer proxySrv.Close()

	body := map[string]any{
		"model":    "gpt-4o",
		"stream":   true,
		"messages": []map[string]any{{"role": "user", "content": "hello"}},
	}
	out := post(t, proxySrv.URL, "/v1/chat/completions", body)
	if !bytes.Contains(out, []byte("Hi there")) {
		t.Fatalf("streamed body did not pass through: %s", out)
	}
	ev := lastNonInflight(st)
	if ev.Provider != "openai" {
		t.Fatalf("provider = %q", ev.Provider)
	}
	if ev.InputTokens != 80 || ev.OutputTokens != 40 {
		t.Fatalf("tokens: in=%d out=%d", ev.InputTokens, ev.OutputTokens)
	}
	want := pricing.Cost("gpt-4o", 80, 40, 0, 0)
	if !almost(ev.CostUSD, want) {
		t.Fatalf("cost = %.6f want %.6f", ev.CostUSD, want)
	}
}

func TestRoutingByUpstreamHeader(t *testing.T) {
	// opencode-go and opencode-zen set x-agenttop-upstream to tell the proxy
	// the real API URL. The proxy must forward there, not to api.openai.com.
	upstreamMock := openAIStreamMock(t)
	defer upstreamMock.Close()

	st, _ := store.New("", 100)
	bus := event.NewBus()
	p := New(st, bus, 0)
	// AnthropicTarget and OpenAITarget are the defaults — the upstream header
	// should override them.
	proxySrv := httptest.NewServer(p)
	defer proxySrv.Close()

	body := map[string]any{
		"model":    "qwen-3-coder-480b",
		"stream":   true,
		"messages": []map[string]any{{"role": "user", "content": "hi"}},
	}
	b, _ := json.Marshal(body)
	req, _ := http.NewRequest("POST", proxySrv.URL+"/v1/chat/completions", bytes.NewReader(b))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer test-key")
	req.Header.Set("x-agenttop-upstream", upstreamMock.URL)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	out, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	if resp.StatusCode != 200 {
		t.Fatalf("status %d body %s (should have routed to upstream mock)", resp.StatusCode, out)
	}
	if !bytes.Contains(out, []byte("Hi there")) {
		t.Fatalf("expected upstream mock response, got: %s", out)
	}
}

func TestUpstreamHeaderStripped(t *testing.T) {
	// The x-agenttop-upstream header must NOT be forwarded to the real API.
	upstreamMock := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("x-agenttop-upstream") != "" {
			t.Errorf("x-agenttop-upstream should be stripped, got: %q", r.Header.Get("x-agenttop-upstream"))
		}
		w.Header().Set("Content-Type", "text/event-stream")
		f := w.(http.Flusher)
		flushSSE(w, f, `data: {"choices":[{"delta":{"content":"ok"}}]}`+"\n\n")
		flushSSE(w, f, `data: {"usage":{"prompt_tokens":10,"completion_tokens":5}}`+"\n\n")
		flushSSE(w, f, "data: [DONE]\n\n")
	}))
	defer upstreamMock.Close()

	st, _ := store.New("", 100)
	bus := event.NewBus()
	p := New(st, bus, 0)
	proxySrv := httptest.NewServer(p)
	defer proxySrv.Close()

	b, _ := json.Marshal(map[string]any{"model": "test", "messages": []map[string]any{{"role": "user", "content": "hi"}}})
	req, _ := http.NewRequest("POST", proxySrv.URL+"/v1/chat/completions", bytes.NewReader(b))
	req.Header.Set("x-agenttop-upstream", upstreamMock.URL)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
}

func TestRoutingByAnthropicVersionHeader(t *testing.T) {
	// opencode's Anthropic SDK sends `anthropic-version` on every request,
	// including endpoints like /v1/messages/count_tokens. Routing must key off
	// that header, not just /v1/messages, so these reach Anthropic.
	anthMock := anthropicStreamMock(t)
	defer anthMock.Close()
	oaiMock := openAIStreamMock(t)
	defer oaiMock.Close()

	st, _ := store.New("", 100)
	bus := event.NewBus()
	p := New(st, bus, 0)
	p.AnthropicTarget = anthMock.URL
	p.OpenAITarget = oaiMock.URL
	proxySrv := httptest.NewServer(p)
	defer proxySrv.Close()

	body := map[string]any{
		"model":    "claude-sonnet-4-5",
		"stream":   true,
		"messages": []map[string]any{{"role": "user", "content": "hi"}},
	}
	b, _ := json.Marshal(body)
	// Hit a non-/v1/messages path but with the Anthropic header → must route to anthropic.
	req, _ := http.NewRequest("POST", proxySrv.URL+"/v1/messages/count_tokens", bytes.NewReader(b))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("anthropic-version", "2023-06-01")
	req.Header.Set("x-api-key", "test")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	out, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	if resp.StatusCode != 200 {
		t.Fatalf("status %d body %s (should have routed to anthropic mock)", resp.StatusCode, out)
	}
	if !bytes.Contains(out, []byte("Hello world")) {
		t.Fatalf("expected anthropic mock response, got: %s", out)
	}
	ev := lastNonInflight(st)
	if ev.Provider != "anthropic" {
		t.Fatalf("provider = %q, want anthropic (header-based routing)", ev.Provider)
	}
}

func TestOpenAINonStreamingCapture(t *testing.T) {
	mock := openAIJSONMock(t)
	defer mock.Close()

	st, _ := store.New("", 100)
	bus := event.NewBus()
	p := New(st, bus, 0)
	p.OpenAITarget = mock.URL
	proxySrv := httptest.NewServer(p)
	defer proxySrv.Close()

	body := map[string]any{
		"model":    "gpt-4o",
		"messages": []map[string]any{{"role": "user", "content": "hello"}},
	}
	post(t, proxySrv.URL, "/v1/chat/completions", body)
	ev := lastNonInflight(st)
	if ev.InputTokens != 80 || ev.OutputTokens != 40 {
		t.Fatalf("tokens: in=%d out=%d", ev.InputTokens, ev.OutputTokens)
	}
	if ev.ResponsePreview != "Hi there" {
		t.Fatalf("response preview = %q", ev.ResponsePreview)
	}
}

func TestPricingFallback(t *testing.T) {
	if c := Cost("unknown-model", 1000, 1000, 0, 0); c != 0 {
		t.Fatalf("unknown model should cost 0, got %.6f", c)
	}
	if c := Cost("claude-sonnet-4-5-20260620", 1000000, 0, 0, 0); !almost(c, 3) {
		t.Fatalf("prefix match: 1M input of sonnet-4-5 should be $3, got %.6f", c)
	}
}

func lastNonInflight(st *store.Store) event.Event {
	for i := len(st.Recent(0)) - 1; i >= 0; i-- {
		ev := st.Recent(0)[i]
		if !ev.InFlight() {
			return ev
		}
	}
	panic("no non-inflight event")
}

func almost(a, b float64) bool {
	d := a - b
	if d < 0 {
		d = -d
	}
	return d < 1e-9
}

func Cost(model string, in, out, cr, cw int) float64 { return pricing.Cost(model, in, out, cr, cw) }
