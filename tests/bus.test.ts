import { describe, expect, test } from "bun:test";
import { Bus, Event } from "../src/event/bus.js";

function ev(partial: Partial<Event> = {}): Event {
  return {
    id: 0,
    traceId: 0,
    time: new Date(),
    provider: "anthropic",
    model: "claude-sonnet-4-5",
    endpoint: "/v1/messages",
    method: "POST",
    status: 200,
    streaming: false,
    durationMs: 100,
    inputTokens: 100,
    outputTokens: 50,
    cacheReadTokens: 0,
    cacheWriteTokens: 0,
    costUSD: 0.001,
    promptPreview: "",
    responsePreview: "",
    err: "",
    ...partial,
  };
}

describe("bus", () => {
  test("delivers events to all subscribers", () => {
    const b = new Bus();
    const got1: Event[] = [];
    const got2: Event[] = [];
    b.subscribe((e) => got1.push(e));
    b.subscribe((e) => got2.push(e));
    b.emit(ev({ traceId: 1 }));
    b.emit(ev({ traceId: 2 }));
    expect(got1.length).toBe(2);
    expect(got2.length).toBe(2);
  });

  test("unsubscribe stops delivery", () => {
    const b = new Bus();
    const got: Event[] = [];
    const off = b.subscribe((e) => got.push(e));
    b.emit(ev());
    off();
    b.emit(ev());
    expect(got.length).toBe(1);
  });

  test("subscriber exceptions don't break others", () => {
    const b = new Bus();
    const got: Event[] = [];
    b.subscribe(() => {
      throw new Error("boom");
    });
    b.subscribe((e) => got.push(e));
    b.emit(ev());
    expect(got.length).toBe(1);
  });
});
