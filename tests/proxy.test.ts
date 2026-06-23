import { describe, expect, test } from "bun:test";
import { Store } from "../src/store/store.js";
import { Bus, Event } from "../src/event/bus.js";
import { Proxy, startProxy } from "../src/proxy/proxy.js";
import { isOpencode, opencodeConfigContent, shellQuote, shellJoin, buildTmuxCommands } from "../src/launch/launch.js";

async function setupProxy() {
  const store = new Store("", 100);
  const bus = new Bus();
  const received: Event[] = [];
  bus.subscribe((e) => received.push(e));
  const proxy = new Proxy(store, bus, { port: 0, openaiURL: "http://127.0.0.1:1" });
  const { server, port } = await startProxy(proxy, 0);
  return { proxy, server, port, store, bus, received };
}

describe("proxy routing", () => {
  test("anthropic-version header routes to anthropic", async () => {
    const { server, received } = await setupProxy();
    await fetch(`http://127.0.0.1:${server.port}/v1/messages`, {
      method: "POST",
      headers: {
        "content-type": "application/json",
        "anthropic-version": "2023-06-01",
        "x-api-key": "test",
      },
      body: JSON.stringify({ model: "claude-sonnet-4-5", stream: false }),
    }).catch(() => {});

    // Wait a tick for the event to land.
    await new Promise((r) => setTimeout(r, 50));
    server.stop();
    expect(received.length).toBeGreaterThan(0);
    expect(received[0].provider).toBe("anthropic");
  });

  test("x-agenttop-upstream overrides target", async () => {
    const { server, received } = await setupProxy();
    await fetch(`http://127.0.0.1:${server.port}/v1/chat/completions`, {
      method: "POST",
      headers: {
        "content-type": "application/json",
        "x-agenttop-upstream": "http://127.0.0.1:1",
        authorization: "Bearer test",
      },
      body: JSON.stringify({ model: "gpt-4o", stream: false }),
    }).catch(() => {});

    await new Promise((r) => setTimeout(r, 50));
    server.stop();
    expect(received.length).toBeGreaterThan(0);
    expect(received[0].provider).toBe("openai");
  });
});

describe("launch helpers", () => {
  test("isOpencode detects opencode binary", () => {
    expect(isOpencode("/usr/local/bin/opencode")).toBe(true);
    expect(isOpencode("/usr/local/bin/OpenCode")).toBe(true);
    expect(isOpencode("/usr/local/bin/claude")).toBe(false);
  });

  test("opencodeConfigContent includes all four providers", () => {
    const cfg = opencodeConfigContent(7331);
    const j = JSON.parse(cfg) as Record<string, any>;
    expect(j.provider.anthropic.options.baseURL).toBe("http://127.0.0.1:7331/v1");
    expect(j.provider.openai.options.baseURL).toBe("http://127.0.0.1:7331/v1");
    expect(j.provider["opencode-go"].options.headers["x-agenttop-upstream"]).toBe("https://opencode.ai/zen/go");
    expect(j.provider.opencode.options.headers["x-agenttop-upstream"]).toBe("https://opencode.ai/zen");
  });

  test("shellQuote handles single quotes", () => {
    expect(shellQuote("hello")).toBe("'hello'");
    expect(shellQuote("it's")).toBe("'it'\\''s'");
  });

  test("shellJoin quotes each arg", () => {
    expect(shellJoin(["claude", "--resume"])).toBe("'claude' '--resume'");
  });

  test("buildTmuxCommands has the right shape", () => {
    const steps = buildTmuxCommands(["/usr/local/bin/agenttop"], "agenttop", ["claude"], 7331);
    const join = steps.map((s) => s.args.join(" ")).join("\n");
    expect(join).toContain("tmux new-session -d -s agenttop");
    expect(join).toContain("tmux set-environment -t agenttop ANTHROPIC_BASE_URL http://127.0.0.1:7331");
    expect(join).toContain("tmux split-window -v -t agenttop");
    expect(join).toContain("tmux resize-pane -t agenttop:0.0 -y 5");
    expect(join).not.toContain("tmux start-server");
  });

  test("buildTmuxCommands injects OPENCODE_CONFIG_CONTENT for opencode", () => {
    const steps = buildTmuxCommands(["/usr/local/bin/agenttop"], "agenttop", ["opencode", "run"], 7331);
    const env = steps.find((s) => s.args[1] === "set-environment" && s.args[4] === "OPENCODE_CONFIG_CONTENT");
    expect(env).toBeDefined();
  });
});
