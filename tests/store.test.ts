import { describe, expect, test } from "bun:test";
import { Store } from "../src/store/store.js";
import { Event, isInFlight } from "../src/event/bus.js";

function ev(partial: Partial<Event>): Event {
  return {
    id: 0,
    traceId: 0,
    time: new Date(),
    provider: "anthropic",
    model: "claude-sonnet-4-5",
    endpoint: "/v1/messages",
    method: "POST",
    status: 0,
    streaming: true,
    durationMs: 0,
    inputTokens: 0,
    outputTokens: 0,
    cacheReadTokens: 0,
    cacheWriteTokens: 0,
    costUSD: 0,
    promptPreview: "",
    responsePreview: "",
    err: "",
    ...partial,
  };
}

describe("store", () => {
  test("appends and counts in-flight correctly", async () => {
    const s = new Store("", 100);
    await s.append(ev({ traceId: 1 })); // in-flight
    await s.append(ev({ traceId: 2 })); // in-flight
    await s.append(ev({ traceId: 1, status: 200, inputTokens: 100, outputTokens: 50, costUSD: 0.001 }));
    let stats = await s.stats();
    expect(stats.inFlight).toBe(1);
    expect(stats.reqs).toBe(1);
    expect(stats.in).toBe(100);
    expect(stats.out).toBe(50);
    expect(stats.cost).toBeCloseTo(0.001);

    await s.append(ev({ traceId: 2, status: 200, inputTokens: 200, outputTokens: 100, costUSD: 0.002 }));
    stats = await s.stats();
    expect(stats.inFlight).toBe(0);
    expect(stats.reqs).toBe(2);
    expect(stats.in).toBe(300);
    expect(stats.out).toBe(150);
  });

  test("ring buffer caps at capacity", async () => {
    const s = new Store("", 3);
    for (let i = 0; i < 10; i++) {
      await s.append(ev({ traceId: i, status: 200, inputTokens: i, outputTokens: i }));
    }
    const recent = await s.recent(100);
    expect(recent.length).toBe(3);
    expect(recent[0].inputTokens).toBe(7);
    expect(recent[2].inputTokens).toBe(9);
  });

  test("in-flight counter is per-trace, not monotonic", async () => {
    const s = new Store("", 100);
    await s.append(ev({ traceId: 1 }));
    await s.append(ev({ traceId: 2 }));
    await s.append(ev({ traceId: 3 }));
    let stats = await s.stats();
    expect(stats.inFlight).toBe(3);

    // End trace 1, then 2, then 3.
    await s.append(ev({ traceId: 1, status: 200 }));
    stats = await s.stats();
    expect(stats.inFlight).toBe(2);

    await s.append(ev({ traceId: 2, status: 200 }));
    stats = await s.stats();
    expect(stats.inFlight).toBe(1);

    await s.append(ev({ traceId: 3, status: 200 }));
    stats = await s.stats();
    expect(stats.inFlight).toBe(0);
  });

  test("isInFlight helper", () => {
    expect(isInFlight(ev({ status: 0, err: "" }))).toBe(true);
    expect(isInFlight(ev({ status: 200 }))).toBe(false);
    expect(isInFlight(ev({ err: "boom" }))).toBe(false);
  });
});
