// E2E: spin up a fake Anthropic upstream on a random port, point the
// agenttop proxy at it, fire a request, and verify the parsed usage +
// cost show up in the store.
import { describe, expect, test } from "bun:test";
import { Store } from "../src/store/store.js";
import { Bus, Event } from "../src/event/bus.js";
import { Proxy, startProxy } from "../src/proxy/proxy.js";

describe("proxy E2E", () => {
  test("captures Anthropic SSE usage and computes cost", async () => {
    // Fake Anthropic upstream.
    const upstream = Bun.serve({
      port: 0,
      hostname: "127.0.0.1",
      fetch() {
        const sse = [
          `event: message_start\ndata: ${JSON.stringify({
            type: "message_start",
            message: {
              id: "msg_1",
              usage: {
                input_tokens: 1500,
                cache_creation_input_tokens: 0,
                cache_read_input_tokens: 0,
                output_tokens: 1,
              },
            },
          })}\n\n`,
          `event: content_block_delta\ndata: ${JSON.stringify({
            type: "content_block_delta",
            delta: { type: "text_delta", text: "Hello world" },
          })}\n\n`,
          `event: message_delta\ndata: ${JSON.stringify({
            type: "message_delta",
            usage: { output_tokens: 300 },
          })}\n\n`,
          `event: message_stop\ndata: ${"hello"}\n\n`,
        ].join("");
        return new Response(sse, {
          status: 200,
          headers: { "content-type": "text/event-stream" },
        });
      },
    });

    const store = new Store("", 100);
    const bus = new Bus();
    const end: Event[] = [];
    bus.subscribe((e) => {
      if (e.status !== 0) end.push(e);
    });
    const proxy = new Proxy(store, bus, {
      port: 0,
      anthropicURL: `http://127.0.0.1:${upstream.port}`,
    });
    const { server, port: agenttopPort } = await startProxy(proxy, 0);

    const resp = await fetch(`http://127.0.0.1:${agenttopPort}/v1/messages`, {
      method: "POST",
      headers: {
        "content-type": "application/json",
        "anthropic-version": "2023-06-01",
        "x-api-key": "test",
      },
      body: JSON.stringify({
        model: "claude-sonnet-4-5",
        stream: true,
        messages: [{ role: "user", content: "say hi" }],
      }),
    });
    expect(resp.status).toBe(200);
    await resp.text();

    // Give the proxy a tick to emit the end event.
    await new Promise((r) => setTimeout(r, 100));

    server.stop();
    upstream.stop();

    expect(end.length).toBe(1);
    const e = end[0];
    expect(e.provider).toBe("anthropic");
    expect(e.model).toBe("claude-sonnet-4-5");
    expect(e.inputTokens).toBe(1500);
    expect(e.outputTokens).toBe(300);
    // 1500 in * $3/M = $0.0045, 300 out * $15/M = $0.0045 → $0.009
    expect(e.costUSD).toBeCloseTo(0.009, 4);
    expect(e.responsePreview).toContain("Hello world");
  });

  test("captures OpenAI JSON usage", async () => {
    const upstream = Bun.serve({
      port: 0,
      hostname: "127.0.0.1",
      fetch() {
        return new Response(
          JSON.stringify({
            id: "cmpl-1",
            model: "gpt-4o",
            choices: [
              { message: { role: "assistant", content: "Hi" }, index: 0 },
            ],
            usage: { prompt_tokens: 100, completion_tokens: 50 },
          }),
          { status: 200, headers: { "content-type": "application/json" } },
        );
      },
    });

    const store = new Store("", 100);
    const bus = new Bus();
    const end: Event[] = [];
    bus.subscribe((e) => {
      if (e.status !== 0) end.push(e);
    });
    const proxy = new Proxy(store, bus, {
      port: 0,
      openaiURL: `http://127.0.0.1:${upstream.port}`,
    });
    const { server, port: agenttopPort } = await startProxy(proxy, 0);

    const resp = await fetch(`http://127.0.0.1:${agenttopPort}/v1/chat/completions`, {
      method: "POST",
      headers: {
        "content-type": "application/json",
        authorization: "Bearer test",
      },
      body: JSON.stringify({
        model: "gpt-4o",
        stream: false,
        messages: [{ role: "user", content: "hi" }],
      }),
    });
    expect(resp.status).toBe(200);
    await resp.text();

    await new Promise((r) => setTimeout(r, 100));

    server.stop();
    upstream.stop();

    expect(end.length).toBe(1);
    const e = end[0];
    expect(e.provider).toBe("openai");
    expect(e.model).toBe("gpt-4o");
    expect(e.inputTokens).toBe(100);
    expect(e.outputTokens).toBe(50);
    // 100 * $2.5/M + 50 * $10/M = $0.00025 + $0.0005 = $0.00075
    expect(e.costUSD).toBeCloseTo(0.00075, 5);
  });
});
