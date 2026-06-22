package proxy

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync/atomic"
	"time"

	"github.com/SalzDevs/agenttop/internal/event"
	"github.com/SalzDevs/agenttop/internal/pricing"
	"github.com/SalzDevs/agenttop/internal/store"
)

type Proxy struct {
	Store           *store.Store
	Bus             *event.Bus
	Client          *http.Client
	Port            int
	traceID         int64
	AnthropicTarget string
	OpenAITarget    string
}

func New(s *store.Store, b *event.Bus, port int) *Proxy {
	return &Proxy{
		Store:           s,
		Bus:             b,
		Port:            port,
		Client:          &http.Client{Timeout: 0},
		AnthropicTarget: "https://api.anthropic.com",
		OpenAITarget:    "https://api.openai.com",
	}
}

// NewWithUpstreams is like New but lets the caller override the upstream
// provider URLs. Useful for local LLMs, corporate proxies, or testing.
func NewWithUpstreams(s *store.Store, b *event.Bus, port int, anthropicURL, openaiURL string) *Proxy {
	p := New(s, b, port)
	if anthropicURL != "" {
		p.AnthropicTarget = anthropicURL
	}
	if openaiURL != "" {
		p.OpenAITarget = openaiURL
	}
	return p
}

type route struct {
	provider string
	target   string
}

func (p *Proxy) route(r *http.Request) route {
	// x-agenttop-upstream: explicit upstream override (used by opencode-go,
	// opencode-zen, and any custom provider that sets this header). This lets
	// the proxy forward to the real API even for providers it doesn't know about.
	if upstream := r.Header.Get("x-agenttop-upstream"); upstream != "" {
		// Determine provider format from the path/header for parsing purposes.
		provider := "openai"
		if r.Header.Get("anthropic-version") != "" || strings.HasPrefix(r.URL.Path, "/v1/messages") {
			provider = "anthropic"
		}
		return route{provider, upstream}
	}
	if h := r.Header.Get("x-agenttop-provider"); h != "" {
		if h == "anthropic" {
			return route{"anthropic", p.AnthropicTarget}
		}
		return route{"openai", p.OpenAITarget}
	}
	// The Anthropic SDK (used by opencode, Claude Code, etc.) sends
	// `anthropic-version` on every request. This is more robust than
	// path matching: it also catches /v1/messages/count_tokens and any
	// future endpoints, routing them to Anthropic instead of OpenAI.
	if r.Header.Get("anthropic-version") != "" {
		return route{"anthropic", p.AnthropicTarget}
	}
	if strings.HasPrefix(r.URL.Path, "/v1/messages") {
		return route{"anthropic", p.AnthropicTarget}
	}
	return route{"openai", p.OpenAITarget}
}

func (p *Proxy) emit(e event.Event) {
	e = p.Store.Append(e)
	p.Bus.Emit(e)
}

func (p *Proxy) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	rt := p.route(r)
	traceID := atomic.AddInt64(&p.traceID, 1)
	start := time.Now()

	body, err := io.ReadAll(r.Body)
	_ = r.Body.Close()
	if err != nil {
		http.Error(w, err.Error(), 400)
		return
	}

	meta := parseRequest(rt.provider, body, r)

	startEvt := event.Event{
		TraceID:       traceID,
		Time:          start,
		Provider:      rt.provider,
		Model:         meta.model,
		Endpoint:      r.URL.Path,
		Method:        r.Method,
		Streaming:     meta.streaming,
		PromptPreview: meta.promptPreview,
	}
	p.emit(startEvt)

	outURL := rt.target + r.URL.Path
	if r.URL.RawQuery != "" {
		outURL += "?" + r.URL.RawQuery
	}
	outReq, err := http.NewRequestWithContext(r.Context(), r.Method, outURL, bytes.NewReader(body))
	if err != nil {
		p.emit(fail(startEvt, start, err.Error()))
		http.Error(w, err.Error(), 502)
		return
	}
	copyHeaders(outReq.Header, r.Header)
	outReq.Header.Del("x-agenttop-provider")
	outReq.Header.Del("x-agenttop-upstream")

	resp, err := p.Client.Do(outReq)
	if err != nil {
		p.emit(fail(startEvt, start, err.Error()))
		http.Error(w, err.Error(), 502)
		return
	}

	hdr := w.Header()
	copyHeaders(hdr, resp.Header)
	hdr.Del("Content-Length")
	w.WriteHeader(resp.StatusCode)

	capture := newCaptureWriter()
	flusher, _ := w.(http.Flusher)
	buf := make([]byte, 4096)
	for {
		n, rerr := resp.Body.Read(buf)
		if n > 0 {
			w.Write(buf[:n])
			capture.Write(buf[:n])
			if flusher != nil {
				flusher.Flush()
			}
		}
		if rerr != nil {
			break
		}
	}
	resp.Body.Close()

	usage, respPreview := parseResponse(rt.provider, resp.Header, capture.Bytes(), meta)

	endEvt := startEvt
	endEvt.Time = time.Now()
	endEvt.Status = resp.StatusCode
	endEvt.Duration = time.Since(start)
	endEvt.InputTokens = usage.in
	endEvt.OutputTokens = usage.out
	endEvt.CacheReadTokens = usage.cacheRead
	endEvt.CacheWriteTokens = usage.cacheWrite
	endEvt.ResponsePreview = respPreview
	endEvt.CostUSD = pricing.Cost(meta.model, usage.in, usage.out, usage.cacheRead, usage.cacheWrite)
	p.emit(endEvt)
}

func fail(e event.Event, start time.Time, msg string) event.Event {
	e.Time = time.Now()
	e.Status = 0
	e.Duration = time.Since(start)
	e.Err = msg
	return e
}

func copyHeaders(dst, src http.Header) {
	for k, v := range src {
		if strings.EqualFold(k, "Host") || strings.EqualFold(k, "Content-Length") {
			continue
		}
		dst[k] = v
	}
}

func hostOnly(u string) string {
	u = strings.TrimPrefix(u, "https://")
	u = strings.TrimPrefix(u, "http://")
	if i := strings.IndexByte(u, '/'); i >= 0 {
		u = u[:i]
	}
	return u
}

type captureWriter struct {
	buf   []byte
	maxSz int
}

func newCaptureWriter() *captureWriter {
	return &captureWriter{maxSz: 1 << 20} // 1MB — plenty for the final usage chunk
}

func (c *captureWriter) Write(b []byte) (int, error) {
	n := len(b)
	if len(c.buf)+n <= c.maxSz {
		c.buf = append(c.buf, b...)
	} else {
		// Keep the last maxSz bytes (ring buffer) so the final usage SSE
		// chunk — which arrives at the end of the stream — is always captured.
		overflow := len(c.buf) + n - c.maxSz
		if overflow > 0 {
			if overflow >= len(c.buf) {
				c.buf = c.buf[:0]
			} else {
				c.buf = c.buf[overflow:]
			}
		}
		remaining := c.maxSz - len(c.buf)
		if remaining > n {
			remaining = n
		}
		c.buf = append(c.buf, b[n-remaining:]...)
	}
	return n, nil
}

func (c *captureWriter) Bytes() []byte { return c.buf }

type reqMeta struct {
	model         string
	streaming     bool
	promptPreview string
}

type rawMessage struct {
	Role    string          `json:"role"`
	Content json.RawMessage `json:"content"`
}

type reqBody struct {
	Model    string       `json:"model"`
	Stream   bool         `json:"stream"`
	Messages []rawMessage `json:"messages"`
}

func parseRequest(provider string, body []byte, r *http.Request) reqMeta {
	var m reqBody
	if err := json.Unmarshal(body, &m); err != nil {
		return reqMeta{}
	}
	preview := ""
	for i := len(m.Messages) - 1; i >= 0; i-- {
		if strings.EqualFold(m.Messages[i].Role, "user") {
			preview = extractText(m.Messages[i].Content)
			break
		}
	}
	return reqMeta{model: m.Model, streaming: m.Stream, promptPreview: trim(preview, 120)}
}

func extractText(content json.RawMessage) string {
	var s string
	if json.Unmarshal(content, &s) == nil {
		return s
	}
	var parts []struct {
		Type string `json:"type"`
		Text string `json:"text"`
	}
	if json.Unmarshal(content, &parts) == nil {
		var b strings.Builder
		for _, p := range parts {
			if p.Type == "text" || p.Type == "" {
				b.WriteString(p.Text)
			}
		}
		return b.String()
	}
	return ""
}

type usage struct {
	in, out, cacheRead, cacheWrite int
}

func parseResponse(provider string, hdr http.Header, body []byte, meta reqMeta) (usage, string) {
	ct := hdr.Get("Content-Type")
	if strings.Contains(ct, "text/event-stream") {
		return parseSSE(provider, body, meta)
	}
	return parseJSON(provider, body, meta)
}

func parseSSE(provider string, body []byte, meta reqMeta) (usage, string) {
	var u usage
	var sb strings.Builder
	for _, line := range bytes.Split(body, []byte("\n")) {
		line = bytes.TrimSpace(line)
		if !bytes.HasPrefix(line, []byte("data:")) {
			continue
		}
		data := bytes.TrimSpace(bytes.TrimPrefix(line, []byte("data:")))
		if bytes.Equal(data, []byte("[DONE]")) {
			continue
		}
		var raw map[string]json.RawMessage
		if json.Unmarshal(data, &raw) != nil {
			continue
		}
		if provider == "anthropic" {
			if t, ok := raw["type"]; ok {
				var ts string
				json.Unmarshal(t, &ts)
				if ts == "message_start" {
					var wrap struct {
						Message struct {
							Usage struct {
								InputTokens                int `json:"input_tokens"`
								CacheCreationInputTokens   int `json:"cache_creation_input_tokens"`
								CacheReadInputTokens       int `json:"cache_read_input_tokens"`
								OutputTokens               int `json:"output_tokens"`
							} `json:"usage"`
						} `json:"message"`
					}
					json.Unmarshal(data, &wrap)
					u.in = wrap.Message.Usage.InputTokens
					u.cacheWrite = wrap.Message.Usage.CacheCreationInputTokens
					u.cacheRead = wrap.Message.Usage.CacheReadInputTokens
				}
				if ts == "message_delta" {
					var wrap struct {
						Usage struct{ OutputTokens int `json:"output_tokens"` } `json:"usage"`
					}
					json.Unmarshal(data, &wrap)
					if wrap.Usage.OutputTokens > 0 {
						u.out = wrap.Usage.OutputTokens
					}
				}
			}
			extractDeltaText(raw, &sb)
		} else {
			if usageRaw, ok := raw["usage"]; ok && string(usageRaw) != "null" {
				var uu struct {
					PromptTokens         int `json:"prompt_tokens"`
					CompletionTokens     int `json:"completion_tokens"`
					PromptCacheHitTokens int `json:"prompt_cache_hit_tokens"`
				}
				json.Unmarshal(usageRaw, &uu)
				if uu.PromptTokens > 0 {
					u.in = uu.PromptTokens
				}
				if uu.PromptCacheHitTokens > 0 {
					u.cacheRead = uu.PromptCacheHitTokens
				}
				if uu.CompletionTokens > 0 {
					u.out = uu.CompletionTokens
				}
			}
			extractOpenAIDelta(raw, &sb)
		}
	}
	return u, trim(sb.String(), 400)
}

func extractDeltaText(raw map[string]json.RawMessage, sb *strings.Builder) {
	if d, ok := raw["delta"]; ok {
		var dd struct {
			Type string `json:"type"`
			Text string `json:"text"`
		}
		json.Unmarshal(d, &dd)
		if dd.Type == "text_delta" {
			sb.WriteString(dd.Text)
		}
	}
}

func extractOpenAIDelta(raw map[string]json.RawMessage, sb *strings.Builder) {
	if d, ok := raw["choices"]; ok {
		var choices []struct {
			Delta struct {
				Content          string `json:"content"`
				ReasoningContent string `json:"reasoning_content"`
			} `json:"delta"`
		}
		json.Unmarshal(d, &choices)
		for _, c := range choices {
			if c.Delta.Content != "" {
				sb.WriteString(c.Delta.Content)
			}
		}
	}
}

func parseJSON(provider string, body []byte, meta reqMeta) (usage, string) {
	var u usage
	var preview string
	if provider == "anthropic" {
		var r struct {
			Usage struct {
				InputTokens              int `json:"input_tokens"`
				OutputTokens             int `json:"output_tokens"`
				CacheCreationInputTokens int `json:"cache_creation_input_tokens"`
				CacheReadInputTokens     int `json:"cache_read_input_tokens"`
			} `json:"usage"`
			Content []struct {
				Text string `json:"text"`
			} `json:"content"`
		}
		if json.Unmarshal(body, &r) == nil {
			u.in = r.Usage.InputTokens
			u.out = r.Usage.OutputTokens
			u.cacheWrite = r.Usage.CacheCreationInputTokens
			u.cacheRead = r.Usage.CacheReadInputTokens
			var sb strings.Builder
			for _, c := range r.Content {
				sb.WriteString(c.Text)
			}
			preview = sb.String()
		}
	} else {
		var r struct {
			Usage struct {
				PromptTokens     int `json:"prompt_tokens"`
				CompletionTokens int `json:"completion_tokens"`
			} `json:"usage"`
			Choices []struct {
				Message struct {
					Content string `json:"content"`
				} `json:"message"`
			} `json:"choices"`
		}
		if json.Unmarshal(body, &r) == nil {
			u.in = r.Usage.PromptTokens
			u.out = r.Usage.CompletionTokens
			if len(r.Choices) > 0 {
				preview = r.Choices[0].Message.Content
			}
		}
	}
	return u, trim(preview, 400)
}

func trim(s string, n int) string {
	s = strings.ReplaceAll(s, "\n", " ")
	s = strings.TrimSpace(s)
	if len(s) > n {
		return s[:n] + "..."
	}
	return s
}

func (p *Proxy) ListenAddr() string {
	return fmt.Sprintf("127.0.0.1:%d", p.Port)
}
