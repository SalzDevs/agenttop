import { describe, expect, test } from "bun:test";
import { lookup, cost } from "../src/pricing/pricing.js";

describe("pricing", () => {
  test("exact match", () => {
    const r = lookup("claude-sonnet-4-5");
    expect(r).not.toBeNull();
    expect(r!.input).toBe(3);
    expect(r!.output).toBe(15);
  });

  test("prefix match: claude-sonnet-4-5-20251020 matches claude-sonnet-4-5", () => {
    const r = lookup("claude-sonnet-4-5-20251020");
    expect(r).not.toBeNull();
    expect(r!.input).toBe(3);
  });

  test("longest prefix wins: gpt-4o-mini matches gpt-4o-mini, not gpt-4o", () => {
    const r = lookup("gpt-4o-mini");
    expect(r).not.toBeNull();
    expect(r!.input).toBe(0.15);
  });

  test("opencode-go models are present", () => {
    expect(lookup("glm-5.2")).not.toBeNull();
    expect(lookup("deepseek-v4-pro")).not.toBeNull();
    expect(lookup("kimi-k2.7-code")).not.toBeNull();
    expect(lookup("qwen3.7-max")).not.toBeNull();
    expect(lookup("minimax-m3")).not.toBeNull();
    expect(lookup("mimo-v2.5-pro")).not.toBeNull();
  });

  test("unknown model returns null", () => {
    expect(lookup("unknown-model-xyz")).toBeNull();
  });

  test("cost calculation", () => {
    // 1M input tokens of claude-sonnet-4-5 = $3
    expect(cost("claude-sonnet-4-5", 1_000_000, 0)).toBeCloseTo(3, 6);
    // 1M output tokens = $15
    expect(cost("claude-sonnet-4-5", 0, 1_000_000)).toBeCloseTo(15, 6);
    // Unknown model = 0
    expect(cost("unknown", 1000, 1000)).toBe(0);
  });

  test("cost includes cache read/write", () => {
    // claude-sonnet-4-5: cache_read=$0.30/M, cache_write=$3.75/M
    const c = cost("claude-sonnet-4-5", 0, 0, 1_000_000, 1_000_000);
    expect(c).toBeCloseTo(0.3 + 3.75, 6);
  });
});
