import { createCliRenderer, Box, Text, t, fg, bold } from "@opentui/core";
import { Bus, Event, isInFlight } from "../event/bus.js";
import { Store } from "../store/store.js";

const SPARK_BLOCKS = "▁▂▃▄▅▆▇█";

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

function buildSparkline(sparkData: number[]): string {
  if (sparkData.length < 2) {
    return "    ▁▂▃▄▅▆▇█";
  }
  const max = Math.max(0.0001, ...sparkData);
  let result = "    ";
  for (let i = 0; i < sparkData.length; i++) {
    const v = sparkData[i];
    const idx = Math.min(SPARK_BLOCKS.length - 1, Math.max(0, Math.floor((v / max) * (SPARK_BLOCKS.length - 1))));
    result += SPARK_BLOCKS[idx];
  }
  result += `  $${(sparkData[sparkData.length - 1] || 0).toFixed(2)}/h`;
  return result;
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

    // Remove old children
    for (const child of renderer.root.getChildren()) {
      renderer.root.remove(child.id);
    }

    // Header line 1: logo + brand + stats
    const headerLine1 = Text({
      content: t`  ${fg(C.dim)("▁")}${fg(C.muted)("▃")}${fg(C.purple)("▅")}${fg(C.cyan)("▇█")}  ${fg(C.cyan)(bold("agenttop"))}   ${fg(C.muted)("cost")} ${fg(C.cyan)(bold(formatCost(totalCost)))}   ${fg(C.muted)("burn")} ${fg(C.purple)(bold(`$${burn.toFixed(2)}/h`))}   ${fg(C.muted)("live")} ${fg(C.cyan)(bold(`${inFlightCount}`))}   ${fg(C.muted)("in")} ${fg(C.muted)(bold(`${totalIn}`))}   ${fg(C.muted)("out")} ${fg(C.muted)(bold(`${totalOut}`))}   ${fg(C.muted)("reqs")} ${fg(C.muted)(bold(`${totalReqs}`))}`,
    });

    // Header line 2: sparkline
    const sparkStr = buildSparkline(sparkData);
    const headerLine2 = Text({ content: sparkStr, fg: C.muted });

    // Separator
    const sep = Text({ content: `  ${"─".repeat(Math.max(8, width - 4))}`, fg: C.dim });

    // Build the children array
    const children: any[] = [headerLine1, Text({ content: "" }), Text({ content: "" }), headerLine2, Text({ content: "" }), sep];

    // Request list
    const listMax = height < 20 ? 2 : 4;
    const listStart = Math.max(0, rows.length - listMax);
    for (const r of rows.slice(listStart)) {
      const modelName = (r.model || "-").padEnd(22);
      const durStr = formatDuration(r);
      const line = Text({
        content: t`    ${r.inFlight ? fg(C.cyan)("●") : r.err ? fg(C.muted)("✗") : fg(C.dim)("✓")}  ${fg(providerColor(r.provider))(modelName)}   ${r.inFlight ? fg(C.cyan)(durStr) : fg(C.muted)(durStr)}   ${fg(C.muted)("in")} ${r.inTok}  ${fg(C.muted)("out")} ${r.outTok}   ${fg(C.muted)("cost")}  ${r.inFlight ? fg(C.muted)("…") : r.cost > 0 ? fg(C.cyan)(formatCost(r.cost)) : formatCost(r.cost)}`,
      });
      children.push(line);
    }

    if (rows.length === 0) {
      children.push(Text({ content: "    waiting for requests…", fg: C.muted }));
    }

    // Blank lines before detail
    children.push(Text({ content: "" }));
    children.push(Text({ content: "" }));

    // Detail
    if (rows.length > 0) {
      const r = rows[rows.length - 1];
      const modelName = r.model || "-";
      const durStr = formatDuration(r);
      children.push(Text({
        content: t`    ${fg(C.muted)("model")}  ${fg(providerColor(r.provider))(modelName)}   ${fg(C.muted)("provider")}  ${fg(C.muted)(r.provider)}   ${fg(C.muted)("dur")}  ${r.inFlight ? fg(C.cyan)(durStr) : fg(C.muted)(durStr)}`,
      }));
      children.push(Text({
        content: t`    ${fg(C.muted)("tokens")}  ${fg(C.muted)(bold(`${r.inTok} ↑`))}   ${fg(C.muted)(bold(`${r.outTok} ↓`))}   ${fg(C.muted)("cost")}  ${fg(C.cyan)(formatCost(r.cost))}`,
      }));
      children.push(Text({ content: "" }));
      children.push(Text({ content: `    prompt  ${r.prompt || "(empty)"}`, fg: C.muted }));
    }

    renderer.root.add(
      Box(
        { flexDirection: "column", width, height },
        ...children,
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
