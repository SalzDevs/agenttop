// Pricing table — model → rate (USD per 1M tokens).
// Source: models.dev (opencode-go) + vendor pricing pages (Anthropic,
// OpenAI, Google). Cache read/write priced where supported.
//
// Models are matched with the longest prefix wins (e.g. gpt-4o-mini
// matches gpt-4o-mini, not gpt-4o), so the table is sorted by key
// length descending before lookup.
export interface Rate {
  input: number;
  output: number;
  cacheRead: number;
  cacheWrite: number;
}

const table: Record<string, Rate> = {
  // Anthropic
  "claude-opus-4": { input: 15, output: 75, cacheRead: 1.5, cacheWrite: 18.75 },
  "claude-sonnet-4": { input: 3, output: 15, cacheRead: 0.3, cacheWrite: 3.75 },
  "claude-sonnet-4-5": { input: 3, output: 15, cacheRead: 0.3, cacheWrite: 3.75 },
  "claude-haiku-4": { input: 0.8, output: 4, cacheRead: 0.08, cacheWrite: 1 },
  "claude-3-5-sonnet": { input: 3, output: 15, cacheRead: 0.3, cacheWrite: 3.75 },
  "claude-3-5-haiku": { input: 0.8, output: 4, cacheRead: 0.08, cacheWrite: 1 },
  // OpenAI
  "gpt-4o": { input: 2.5, output: 10, cacheRead: 0, cacheWrite: 0 },
  "gpt-4o-mini": { input: 0.15, output: 0.6, cacheRead: 0, cacheWrite: 0 },
  "gpt-4.1": { input: 2, output: 8, cacheRead: 0, cacheWrite: 0 },
  "gpt-4.1-mini": { input: 0.4, output: 1.6, cacheRead: 0, cacheWrite: 0 },
  "gpt-4.1-nano": { input: 0.1, output: 0.4, cacheRead: 0, cacheWrite: 0 },
  o1: { input: 15, output: 60, cacheRead: 0, cacheWrite: 0 },
  o3: { input: 10, output: 40, cacheRead: 0, cacheWrite: 0 },
  "o3-mini": { input: 1.1, output: 4.2, cacheRead: 0, cacheWrite: 0 },
  "o4-mini": { input: 1.1, output: 4.2, cacheRead: 0, cacheWrite: 0 },
  // Google
  "gemini-2.5-pro": { input: 1.25, output: 10, cacheRead: 0, cacheWrite: 0 },
  "gemini-2.5-flash": { input: 0.075, output: 0.3, cacheRead: 0, cacheWrite: 0 },
  // OpenCode Go models (pricing from models.dev)
  "glm-5.2": { input: 1.4, output: 4.4, cacheRead: 0.26, cacheWrite: 0 },
  "glm-5.1": { input: 1.4, output: 4.4, cacheRead: 0.26, cacheWrite: 0 },
  "glm-5": { input: 1.4, output: 4.4, cacheRead: 0.26, cacheWrite: 0 },
  "deepseek-v4-pro": { input: 1.74, output: 3.48, cacheRead: 0.0145, cacheWrite: 0 },
  "deepseek-v4-flash": { input: 0.55, output: 1.1, cacheRead: 0.014, cacheWrite: 0 },
  "kimi-k2.7-code": { input: 0.95, output: 4.0, cacheRead: 0.19, cacheWrite: 0 },
  "kimi-k2.6": { input: 0.95, output: 4.0, cacheRead: 0.19, cacheWrite: 0 },
  "kimi-k2.5": { input: 0.95, output: 4.0, cacheRead: 0.19, cacheWrite: 0 },
  "qwen3.7-max": { input: 2.5, output: 7.5, cacheRead: 0.5, cacheWrite: 3.125 },
  "qwen3.7-plus": { input: 0.85, output: 2.6, cacheRead: 0.17, cacheWrite: 0 },
  "qwen3.6-plus": { input: 0.85, output: 2.6, cacheRead: 0.17, cacheWrite: 0 },
  "qwen3.5-plus": { input: 0.85, output: 2.6, cacheRead: 0.17, cacheWrite: 0 },
  "minimax-m3": { input: 0.1, output: 0.4, cacheRead: 0.02, cacheWrite: 0 },
  "minimax-m2.7": { input: 0.1, output: 0.4, cacheRead: 0.02, cacheWrite: 0 },
  "minimax-m2.5": { input: 0.1, output: 0.4, cacheRead: 0.02, cacheWrite: 0 },
  "mimo-v2.5-pro": { input: 0.3, output: 1.1, cacheRead: 0.06, cacheWrite: 0 },
  "mimo-v2.5": { input: 0.15, output: 0.6, cacheRead: 0.03, cacheWrite: 0 },
  "mimo-v2-pro": { input: 0.15, output: 0.6, cacheRead: 0.03, cacheWrite: 0 },
};

// sortedKeys: pricing table keys sorted by length descending so that
// prefix matching picks the most specific entry first.
const sortedKeys: string[] = Object.keys(table).sort(
  (a, b) => b.length - a.length,
);

export function lookup(model: string): Rate | null {
  // Exact match first.
  const exact = table[model];
  if (exact) return exact;
  // Longest-prefix fallback.
  for (const k of sortedKeys) {
    if (model.startsWith(k)) return table[k];
  }
  return null;
}

export function cost(
  model: string,
  inputTokens: number,
  outputTokens: number,
  cacheRead = 0,
  cacheWrite = 0,
): number {
  const r = lookup(model);
  if (!r) return 0;
  return (
    (inputTokens * r.input +
      outputTokens * r.output +
      cacheRead * r.cacheRead +
      cacheWrite * r.cacheWrite) /
    1e6
  );
}
