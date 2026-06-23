// opentui TUI for agenttop.
//
// Renders a 1-line status bar: logo + brand + cost + burn + live + in/out +
// reqs + sparkline, with a separator, the recent request list, and a detail
// pane for the latest request. No keyboard input — the TUI is a passive
// display, and exiting is done by detaching tmux.

import { createCliRenderer, Box, Text } from "@opentui/core";
import { Bus, Event, isInFlight } from "../event/bus.js";
import { Store } from "../store/store.js";

const SPARK_BLOCKS = "▁▂▃▄▅▆▇█";

// Palette — matches the logo exactly: 5 colors, cyan as the accent.
const C = {
  cyan: "#56D4DD",
  purple: "#A78BFA",
  muted: "#6B7280",
  dim: "#3A3F47",
};

interface Row {
  traceId: number;
  time: Date;
  provider: string;
  model: string;
  status: number;
  inFlight: boolean;
  inTok: number;
  outTok: number;
  cost: number;
  durationMs: number;
  prompt: string;
  response: string;
  err: string;
}

function eventToRow(e: Event): Row {
  return {
    traceId: e.traceId,
    time: e.time,
    provider: e.provider,
    model: e.model,
    status: e.status,
    inFlight: isInFlight(e),
    inTok: e.inputTokens,
    outTok: e.outputTokens,
    cost: e.costUSD,
    durationMs: e.durationMs,
    prompt: e.promptPreview,
    response: e.responsePreview,
    err: e.err,
  };
}

function providerColor(provider: string): string {
  if (provider === "anthropic") return C.purple;
  if (provider === "openai" || provider === "opencode" || provider === "opencode-go")
    return C.cyan;
  return C.muted;
}

function statusSymbol(r: Row): { glyph: string; color: string } {
  if (r.inFlight) return { glyph: "●", color: C.cyan };
  if (r.err) return { glyph: "✗", color: C.muted };
  return { glyph: "✓", color: C.dim };
}

function formatDuration(r: Row): string {
  if (r.inFlight) {
    const s = ((Date.now() - r.time.getTime()) / 1000).toFixed(2);
    return `${s}s`;
  }
  return `${(r.durationMs / 1000).toFixed(2)}s`;
}

function formatCost(c: number): string {
  return `$${c.toFixed(4)}`;
}

export async function renderTUI(
  _store: Store,
  bus: Bus,
  _port: number,
): Promise<void> {
  const renderer = await createCliRenderer({ exitOnCtrlC: true });

  const rows: Row[] = [];
  const byTrace = new Map<number, Row>();
  const costWindow: Array<{ time: number; cost: number }> = [];
  const sparkData: number[] = [];
  const SPARK_CAP = 40;
  const COST_WINDOW_MS = 60_000;

  function pushEvent(e: Event) {
    const row = eventToRow(e);
    if (row.inFlight) {
      byTrace.set(row.traceId, row);
    } else {
      byTrace.delete(row.traceId);
    }
    // Replace by trace id, then keep the last 1000 events.
    const idx = rows.findIndex((r) => r.traceId === row.traceId);
    if (idx >= 0) {
      rows[idx] = { ...rows[idx], ...row };
    } else {
      rows.push(row);
    }
    if (rows.length > 1000) rows.splice(0, rows.length - 1000);
    if (!row.inFlight) {
      costWindow.push({ time: Date.now(), cost: row.cost });
    }
    rerender();
  }

  const unsubscribe = bus.subscribe(pushEvent);

  // 1s tick for sparkline + live durations.
  const tick = setInterval(computeSpark, 1000);

  function computeSpark() {
    const now = Date.now();
    while (costWindow.length > 0 && now - costWindow[0].time > COST_WINDOW_MS) {
      costWindow.shift();
    }
    const burn = costWindow.reduce((s, c) => s + c.cost, 0) * (60_000 / COST_WINDOW_MS);
    sparkData.push(burn);
    if (sparkData.length > SPARK_CAP) sparkData.shift();
    rerender();
  }

  function burnPerHour(): number {
    if (costWindow.length === 0) return 0;
    const span = Math.max(1, Date.now() - costWindow[0].time);
    return (costWindow.reduce((s, c) => s + c.cost, 0) / span) * 3_600_000;
  }

  function rerender() {
    const width = renderer.width ?? 80;
    const height = renderer.height ?? 24;
    const totalCost = rows.reduce((s, r) => s + r.cost, 0);
    const totalIn = rows.reduce((s, r) => s + r.inTok, 0);
    const totalOut = rows.reduce((s, r) => s + r.outTok, 0);
    const totalReqs = rows.filter((r) => !r.inFlight).length;
    const inFlightCount = byTrace.size;
    const burn = burnPerHour();

    // ── Sparkline
    let sparkStr = "";
    if (sparkData.length >= 2) {
      const max = Math.max(0.0001, ...sparkData);
      const idxs = sparkData.map((v) => Math.min(SPARK_BLOCKS.length - 1, Math.max(0, Math.floor((v / max) * (SPARK_BLOCKS.length - 1)))));
      sparkStr = idxs
        .map((i, k) => {
          const ch = SPARK_BLOCKS[i];
          const v = sparkData[k];
          if (v > max * 0.66) return `{${C.cyan}-fg}${ch}{/${C.cyan}-fg}`;
          if (v > max * 0.33) return `{${C.purple}-fg}${ch}{/${C.purple}-fg}`;
          return `{${C.dim}-fg}${ch}{/${C.dim}-fg}`;
        })
        .join("");
    } else {
      sparkStr = `{${C.muted}-fg}▁▂▃▄▅▆▇█{/${C.muted}-fg}`;
    }

    // ── Header line 1: logo + brand + stats (joined inline)
    const headerLine1 = [
      `  {${C.dim}-fg}▁{/${C.dim}-fg}{${C.muted}-fg}▃{/${C.muted}-fg}{${C.purple}-fg}▅{/${C.purple}-fg}{${C.cyan}-fg}▇█{/${C.cyan}-fg}`,
      `{${C.cyan}-fg}{bold}agenttop{/bold}{/${C.cyan}-fg}`,
      `   {${C.muted}-fg}cost{/${C.muted}-fg} {${C.cyan}-fg}{bold}${formatCost(totalCost)}{/bold}{/${C.cyan}-fg}`,
      `   {${C.muted}-fg}burn{/${C.muted}-fg} {${C.purple}-fg}{bold}$${burn.toFixed(2)}/h{/bold}{/${C.purple}-fg}`,
      `   {${C.muted}-fg}live{/${C.muted}-fg} {${C.cyan}-fg}{bold}${inFlightCount}{/bold}{/${C.cyan}-fg}`,
      `   {${C.muted}-fg}in{/${C.muted}-fg} {${C.muted}-fg}{bold}${totalIn}{/bold}{/${C.muted}-fg}`,
      `   {${C.muted}-fg}out{/${C.muted}-fg} {${C.muted}-fg}{bold}${totalOut}{/bold}{/${C.muted}-fg}`,
      `   {${C.muted}-fg}reqs{/${C.muted}-fg} {${C.muted}-fg}{bold}${totalReqs}{/bold}{/${C.muted}-fg}`,
    ].join("");

    const headerLine2 = `    ${sparkStr}  {${C.purple}-fg}$${burn.toFixed(2)}/h{/${C.purple}-fg}`;

    // ── Separator
    const sep = `  {${C.dim}-fg}${"─".repeat(Math.max(8, width - 4))}{/${C.dim}-fg}`;

    // ── Recent request list (latest 4)
    const listMax = height < 20 ? 2 : 4;
    const listStart = Math.max(0, rows.length - listMax);
    const listLines = rows.slice(listStart).map((r) => {
      const sym = statusSymbol(r);
      const modelName = r.model || "-";
      const costStr = r.inFlight ? `{${C.muted}-fg}…{/${C.muted}-fg}` : (r.cost > 0 ? `{${C.cyan}-fg}${formatCost(r.cost)}{/${C.cyan}-fg}` : formatCost(r.cost));
      const durStr = formatDuration(r);
      return `    {${sym.color}-fg}${sym.glyph}{/${sym.color}-fg}  {${providerColor(r.provider)}-fg}${modelName.padEnd(22)}{/${providerColor(r.provider)}-fg}   {${C.muted}-fg}${durStr}{/${C.muted}-fg}   {${C.muted}-fg}in{/${C.muted}-fg} ${r.inTok}  {${C.muted}-fg}out{/${C.muted}-fg} ${r.outTok}   {${C.muted}-fg}cost{/${C.muted}-fg}  ${costStr}`;
    }).join("\n");

    // ── Detail (latest request)
    let detail = "";
    if (rows.length > 0) {
      const r = rows[rows.length - 1];
      const modelName = r.model || "-";
      const durStr = r.inFlight ? `{${C.cyan}-fg}${formatDuration(r)}{/${C.cyan}-fg}{${C.muted}-fg}  (live){/${C.muted}-fg}` : `{${C.muted}-fg}${formatDuration(r)}{/${C.muted}-fg}`;
      const inFlight = `{${C.muted}-fg}model{/${C.muted}-fg}  {${providerColor(r.provider)}-fg}${modelName}{/${providerColor(r.provider)}-fg}   {${C.muted}-fg}provider{/${C.muted}-fg}  {${C.muted}-fg}${r.provider}{/${C.muted}-fg}   {${C.muted}-fg}dur{/${C.muted}-fg}  ${durStr}`;
      const tokens = `{${C.muted}-fg}tokens{/${C.muted}-fg}  {${C.muted}-fg}{bold}${r.inTok} ↑{/bold}{/${C.muted}-fg}   {${C.muted}-fg}{bold}${r.outTok} ↓{/bold}{/${C.muted}-fg}   {${C.muted}-fg}cost{/${C.muted}-fg}  {${C.cyan}-fg}${formatCost(r.cost)}{/${C.cyan}-fg}`;
      const prompt = `    {${C.muted}-fg}prompt  {/${C.muted}-fg}${r.prompt || "(empty)"}`;
      detail = [inFlight, tokens, "", prompt].join("\n");
    } else {
      detail = `    {${C.muted}-fg}waiting for requests…{/${C.muted}-fg}`;
    }

    const body = [headerLine1, "", "", headerLine2, "", sep, listLines, "", "", detail].join("\n");

    // Replace root children by removing existing then adding fresh.
    for (const child of renderer.root.getChildren()) {
      renderer.root.remove(child.id);
    }
    renderer.root.add(
      Box(
        { flexDirection: "column", width, height },
        Text({ content: body }),
      ),
    );
  }

  rerender();

  await new Promise<void>((resolveP) => {
    const onSig = () => {
      clearInterval(tick);
      unsubscribe();
      renderer.destroy();
      resolveP();
    };
    process.once("SIGINT", onSig);
    process.once("SIGTERM", onSig);
  });
}
