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

// TestDeepseekStreamingFormat tests the exact SSE format that deepseek-v4-pro
// returns: reasoning_content chunks followed by content chunks, with usage in
// the final chunk (with finish_reason:"stop").
func TestDeepseekStreamingFormat(t *testing.T) {
	sse := []byte("data: {\"id\":\"test\",\"model\":\"deepseek-v4-pro\",\"choices\":[{\"index\":0,\"delta\":{\"role\":\"assistant\",\"content\":null,\"reasoning_content\":\"\"},\"finish_reason\":null}],\"usage\":null}\n\n" +
		"data: {\"id\":\"test\",\"model\":\"deepseek-v4-pro\",\"choices\":[{\"index\":0,\"delta\":{\"content\":null,\"reasoning_content\":\"Thinking about this...\"},\"finish_reason\":null}],\"usage\":null}\n\n" +
		"data: {\"id\":\"test\",\"model\":\"deepseek-v4-pro\",\"choices\":[{\"index\":0,\"delta\":{\"content\":\"Hi! How can I help you?\",\"reasoning_content\":null},\"finish_reason\":null}],\"usage\":null}\n\n" +
		"data: {\"id\":\"test\",\"model\":\"deepseek-v4-pro\",\"choices\":[{\"index\":0,\"delta\":{\"content\":\"\",\"reasoning_content\":null},\"finish_reason\":\"stop\"}],\"usage\":{\"prompt_tokens\":602,\"completion_tokens\":111,\"total_tokens\":713,\"prompt_tokens_details\":{\"cached_tokens\":0},\"completion_tokens_details\":{\"reasoning_tokens\":81},\"prompt_cache_hit_tokens\":0,\"prompt_cache_miss_tokens\":602}}\n\n" +
		"data: [DONE]\n\n")

	mock := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.Write(sse)
	}))
	defer mock.Close()

	st, _ := store.New("", 100)
	bus := event.NewBus()
	p := New(st, bus, 0)
	p.OpenAITarget = mock.URL
	proxySrv := httptest.NewServer(p)
	defer proxySrv.Close()

	body := map[string]any{
		"model":    "deepseek-v4-pro",
		"stream":   true,
		"messages": []map[string]any{{"role": "user", "content": "hi"}},
	}
	b, _ := json.Marshal(body)
	req, _ := http.NewRequest("POST", proxySrv.URL+"/v1/chat/completions", bytes.NewReader(b))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("x-agenttop-upstream", mock.URL)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	raw, _ := io.ReadAll(resp.Body)
	resp.Body.Close()

	if !bytes.Contains(raw, []byte("Hi! How can I help you?")) {
		t.Fatalf("content not passed through: %s", raw)
	}

	ev := lastNonInflight(st)
	if ev.Provider != "openai" {
		t.Fatalf("provider = %q, want openai", ev.Provider)
	}
	if ev.InputTokens != 602 {
		t.Fatalf("input tokens = %d, want 602", ev.InputTokens)
	}
	if ev.OutputTokens != 111 {
		t.Fatalf("output tokens = %d, want 111", ev.OutputTokens)
	}
	if ev.ResponsePreview != "Hi! How can I help you?" {
		t.Fatalf("response preview = %q, want 'Hi! How can I help you?'", ev.ResponsePreview)
	}
	wantCost := pricing.Cost("deepseek-v4-pro", 602, 111, 0, 0)
	if !almost(ev.CostUSD, wantCost) {
		t.Fatalf("cost = %.6f, want %.6f", ev.CostUSD, wantCost)
	}
}
