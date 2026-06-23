import { Event, Bus } from "../event/bus.js";
import { Store } from "../store/store.js";
import { cost as pricingCost } from "../pricing/pricing.js";

interface Route {
  provider: "anthropic" | "openai";
  target: string;
}

interface RequestMeta {
  model: string;
  streaming: boolean;
  promptPreview: string;
}

interface Usage {
  in: number;
  out: number;
  cacheRead: number;
  cacheWrite: number;
}

// CaptureWriter is a 1MB ring buffer that retains the tail of a stream.
// The final SSE chunk (which contains the usage payload) arrives last, so
// we keep the last 1MB instead of the first 1MB.
class CaptureWriter {
  private buf: Buffer = Buffer.alloc(0);
  private readonly max = 1 << 20;

  write(chunk: Buffer): void {
    const n = chunk.length;
    if (this.buf.length + n <= this.max) {
      this.buf = Buffer.concat([this.buf, chunk]);
      return;
    }
    // Overflow: keep the last max bytes.
    const combined = Buffer.concat([this.buf, chunk]);
    if (combined.length > this.max) {
      this.buf = combined.subarray(combined.length - this.max);
    } else {
      this.buf = combined;
    }
  }

  bytes(): Buffer {
    return this.buf;
  }
}

export interface ProxyOptions {
  port: number;
  anthropicURL?: string;
  openaiURL?: string;
}

export class Proxy {
  private store: Store;
  private bus: Bus;
  private port: number;
  private anthropicTarget: string;
  private openaiTarget: string;
  private traceId = 0;

  constructor(store: Store, bus: Bus, opts: ProxyOptions) {
    this.store = store;
    this.bus = bus;
    this.port = opts.port;
    this.anthropicTarget = opts.anthropicURL || "https://api.anthropic.com";
    this.openaiTarget = opts.openaiURL || "https://api.openai.com";
  }

  listenAddr(): string {
    return `127.0.0.1:${this.port}`;
  }

  private route(req: Request, path: string): Route {
    // x-agenttop-upstream: explicit upstream override (used by opencode-go,
    // opencode-zen, and any custom provider that sets this header). This lets
    // the proxy forward to the real API even for providers it doesn't know.
    const upstream = req.headers.get("x-agenttop-upstream");
    if (upstream) {
      const isAnthropic =
        req.headers.get("anthropic-version") !== null ||
        path.startsWith("/v1/messages");
      return {
        provider: isAnthropic ? "anthropic" : "openai",
        target: upstream,
      };
    }
    const explicit = req.headers.get("x-agenttop-provider");
    if (explicit === "anthropic")
      return { provider: "anthropic", target: this.anthropicTarget };
    if (explicit === "openai")
      return { provider: "openai", target: this.openaiTarget };
    // The Anthropic SDK (used by opencode, Claude Code, etc.) sends
    // `anthropic-version` on every request. This is more robust than
    // path matching: it also catches /v1/messages/count_tokens and any
    // future endpoints, routing them to Anthropic instead of OpenAI.
    if (req.headers.get("anthropic-version") !== null)
      return { provider: "anthropic", target: this.anthropicTarget };
    if (path.startsWith("/v1/messages"))
      return { provider: "anthropic", target: this.anthropicTarget };
    return { provider: "openai", target: this.openaiTarget };
  }

  private async emit(e: Event): Promise<void> {
    const stored = await this.store.append(e);
    this.bus.emit(stored);
  }

  async handle(req: Request, path: string): Promise<Response> {
    const rt = this.route(req, path);
    const traceId = ++this.traceId;
    const start = new Date();

    let body: Buffer;
    try {
      const ab = await req.arrayBuffer();
      body = Buffer.from(ab);
    } catch (err) {
      return new Response(String(err), { status: 400 });
    }

    const meta = parseRequest(body);

    const startEvt: Event = {
      id: 0,
      traceId,
      time: start,
      provider: rt.provider,
      model: meta.model,
      endpoint: path,
      method: req.method,
      status: 0,
      streaming: meta.streaming,
      durationMs: 0,
      inputTokens: 0,
      outputTokens: 0,
      cacheReadTokens: 0,
      cacheWriteTokens: 0,
      costUSD: 0,
      promptPreview: meta.promptPreview,
      responsePreview: "",
      err: "",
    };
    await this.emit(startEvt);

    // Build upstream URL.
    const inUrl = new URL(req.url);
    const outURL = rt.target + inUrl.pathname + inUrl.search;

    // Copy headers, dropping Host/Content-Length and the agenttop hints.
    const outHeaders = new Headers();
    for (const [k, v] of req.headers.entries()) {
      const lower = k.toLowerCase();
      if (
        lower === "host" ||
        lower === "content-length" ||
        lower === "x-agenttop-provider" ||
        lower === "x-agenttop-upstream"
      ) {
        continue;
      }
      outHeaders.set(k, v);
    }

    let upstream: Response;
    try {
      upstream = await fetch(outURL, {
        method: req.method,
        headers: outHeaders,
        body: req.method === "GET" || req.method === "HEAD" ? undefined : body,
      });
    } catch (err) {
      const fail = makeFail(startEvt, start, String(err));
      await this.emit(fail);
      return new Response(String(err), { status: 502 });
    }

    // Build response headers (drop content-length since we're streaming
    // a different body).
    const respHeaders = new Headers();
    for (const [k, v] of upstream.headers.entries()) {
      if (k.toLowerCase() === "content-length") continue;
      respHeaders.set(k, v);
    }

    const ct = upstream.headers.get("content-type") || "";
    const isSSE = ct.includes("text/event-stream");
    const capture = new CaptureWriter();

    // If there's no body (e.g. HEAD, 204), return immediately.
    if (!upstream.body) {
      const endEvt = makeEndEvt(startEvt, start, upstream.status, { in: 0, out: 0, cacheRead: 0, cacheWrite: 0 }, "", meta.model);
      await this.emit(endEvt);
      return new Response(null, { status: upstream.status, headers: respHeaders });
    }

    // Stream the upstream body to the client in real-time while
    // capturing chunks in parallel. When the stream ends, parse the
    // captured tail for usage and emit the end event.
    const reader = upstream.body.getReader();

    const stream = new ReadableStream({
      pull: async (controller): Promise<void> => {
        const { done, value } = await reader.read();
        if (done) {
          controller.close();
          // Parse usage from the captured tail.
          const { usage, responsePreview } = isSSE
            ? parseSSE(rt.provider, capture.bytes())
            : parseJSON(rt.provider, capture.bytes());
          const endEvt = makeEndEvt(startEvt, start, upstream.status, usage, responsePreview, meta.model);
          await this.emit(endEvt);
          return;
        }
        if (value) {
          capture.write(Buffer.from(value));
          controller.enqueue(value);
        }
      },
      cancel: () => {
        reader.cancel().catch(() => {});
      },
    });

    return new Response(stream, {
      status: upstream.status,
      headers: respHeaders,
    });
  }
}

function makeFail(e: Event, start: Date, msg: string): Event {
  return {
    ...e,
    time: new Date(),
    status: 0,
    durationMs: Date.now() - start.getTime(),
    err: msg,
  };
}

function makeEndEvt(
  startEvt: Event,
  start: Date,
  status: number,
  usage: Usage,
  responsePreview: string,
  model: string,
): Event {
  return {
    ...startEvt,
    time: new Date(),
    status,
    durationMs: Date.now() - start.getTime(),
    inputTokens: usage.in,
    outputTokens: usage.out,
    cacheReadTokens: usage.cacheRead,
    cacheWriteTokens: usage.cacheWrite,
    responsePreview,
    costUSD: pricingCost(model, usage.in, usage.out, usage.cacheRead, usage.cacheWrite),
  };
}

function trim(s: string, n: number): string {
  const oneLine = s.replace(/\s+/g, " ").trim();
  if (oneLine.length > n) return oneLine.slice(0, n) + "…";
  return oneLine;
}

function parseRequest(body: Buffer): RequestMeta {
  try {
    const j = JSON.parse(body.toString("utf-8")) as {
      model?: string;
      stream?: boolean;
      messages?: Array<{ role: string; content: unknown }>;
    };
    let preview = "";
    if (Array.isArray(j.messages)) {
      for (let i = j.messages.length - 1; i >= 0; i--) {
        if (j.messages[i].role?.toLowerCase() === "user") {
          preview = extractText(j.messages[i].content);
          break;
        }
      }
    }
    return {
      model: j.model || "",
      streaming: Boolean(j.stream),
      promptPreview: trim(preview, 120),
    };
  } catch {
    return { model: "", streaming: false, promptPreview: "" };
  }
}

function extractText(content: unknown): string {
  if (typeof content === "string") return content;
  if (Array.isArray(content)) {
    return content
      .map((p) => {
        if (typeof p === "object" && p !== null) {
          const pp = p as { type?: string; text?: string };
          if (!pp.type || pp.type === "text") return pp.text || "";
        }
        return "";
      })
      .join("");
  }
  return "";
}

function parseSSE(
  provider: "anthropic" | "openai",
  body: Buffer,
): { usage: Usage; responsePreview: string } {
  const u: Usage = { in: 0, out: 0, cacheRead: 0, cacheWrite: 0 };
  const text: string[] = [];

  for (const rawLine of body.toString("utf-8").split("\n")) {
    const line = rawLine.trim();
    if (!line.startsWith("data:")) continue;
    const data = line.slice(5).trim();
    if (data === "[DONE]") continue;
    let obj: Record<string, unknown>;
    try {
      obj = JSON.parse(data) as Record<string, unknown>;
    } catch {
      continue;
    }
    if (provider === "anthropic") {
      const t = obj.type as string | undefined;
      if (t === "message_start") {
        const wrap = obj as {
          message?: { usage?: Record<string, number> };
        };
        const usage = wrap.message?.usage;
        if (usage) {
          u.in = usage.input_tokens || 0;
          u.cacheWrite = usage.cache_creation_input_tokens || 0;
          u.cacheRead = usage.cache_read_input_tokens || 0;
        }
      } else if (t === "message_delta") {
        const wrap = obj as { usage?: { output_tokens?: number } };
        const out = wrap.usage?.output_tokens;
        if (out && out > 0) u.out = out;
      }
      // Extract text deltas
      const d = obj.delta as { type?: string; text?: string } | undefined;
      if (d?.type === "text_delta" && d.text) text.push(d.text);
    } else {
      const usage = obj.usage as
        | {
            prompt_tokens?: number;
            completion_tokens?: number;
            prompt_cache_hit_tokens?: number;
          }
        | null;
      if (usage) {
        if (usage.prompt_tokens && usage.prompt_tokens > 0)
          u.in = usage.prompt_tokens;
        if (usage.prompt_cache_hit_tokens && usage.prompt_cache_hit_tokens > 0)
          u.cacheRead = usage.prompt_cache_hit_tokens;
        if (usage.completion_tokens && usage.completion_tokens > 0)
          u.out = usage.completion_tokens;
      }
      const choices = obj.choices as
        | Array<{ delta?: { content?: string; reasoning_content?: string } }>
        | undefined;
      if (Array.isArray(choices)) {
        for (const c of choices) {
          if (c.delta?.content) text.push(c.delta.content);
        }
      }
    }
  }
  return { usage: u, responsePreview: trim(text.join(""), 400) };
}

function parseJSON(
  provider: "anthropic" | "openai",
  body: Buffer,
): { usage: Usage; responsePreview: string } {
  const u: Usage = { in: 0, out: 0, cacheRead: 0, cacheWrite: 0 };
  let preview = "";
  try {
    const j = JSON.parse(body.toString("utf-8")) as Record<string, unknown>;
    if (provider === "anthropic") {
      const usage = j.usage as
        | {
            input_tokens?: number;
            output_tokens?: number;
            cache_creation_input_tokens?: number;
            cache_read_input_tokens?: number;
          }
        | undefined;
      if (usage) {
        u.in = usage.input_tokens || 0;
        u.out = usage.output_tokens || 0;
        u.cacheWrite = usage.cache_creation_input_tokens || 0;
        u.cacheRead = usage.cache_read_input_tokens || 0;
      }
      const content = j.content as Array<{ text?: string }> | undefined;
      if (Array.isArray(content)) {
        preview = content.map((c) => c.text || "").join("");
      }
    } else {
      const usage = j.usage as
        | { prompt_tokens?: number; completion_tokens?: number }
        | undefined;
      if (usage) {
        u.in = usage.prompt_tokens || 0;
        u.out = usage.completion_tokens || 0;
      }
      const choices = j.choices as
        | Array<{ message?: { content?: string } }>
        | undefined;
      if (Array.isArray(choices) && choices[0]?.message) {
        preview = choices[0].message.content || "";
      }
    }
  } catch {
    // Not JSON, leave usage at zero.
  }
  return { usage: u, responsePreview: trim(preview, 400) };
}

// startProxy starts a Bun HTTP server that routes all traffic through
// the proxy logic. Returns the actual port (useful when the caller
// passed 0 for "pick any free port").
export async function startProxy(
  proxy: Proxy,
  port: number,
): Promise<{ server: ReturnType<typeof Bun.serve>; port: number }> {
  const server = Bun.serve({
    port,
    hostname: "127.0.0.1",
    async fetch(req) {
      const url = new URL(req.url);
      return proxy.handle(req, url.pathname);
    },
  });
  return { server, port: server.port ?? port };
}
