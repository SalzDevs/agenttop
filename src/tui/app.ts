import { Bus } from "../event/bus.js";
import { Store } from "../store/store.js";
import { Proxy, startProxy } from "../proxy/proxy.js";
import { instructions } from "../launch/launch.js";
import { renderTUI } from "./tui.js";

export interface MonitorFlags {
  port: number;
  log: string;
  anthropicURL: string;
  openaiURL: string;
}

// runMonitor starts the proxy + TUI monitor. Used by `agenttop`, `agenttop
// monitor`, and the tmux top pane.
export async function runMonitor(flags: MonitorFlags): Promise<void> {
  const store = new Store(flags.log, 1000);
  const bus = new Bus();
  const proxy = new Proxy(store, bus, { port: flags.port, anthropicURL: flags.anthropicURL, openaiURL: flags.openaiURL });

  const { server, port } = await startProxy(proxy, flags.port);
  process.stderr.write(instructions(port));

  if (!process.stdin.isTTY) {
    process.stderr.write(`agenttop: no TTY, running proxy-only on :${port} (Ctrl+C to stop)\n`);
    await new Promise(() => {});
    return;
  }

  await renderTUI(store, bus, port);
  server.stop();
}

// runMonitorNoTUI runs just the proxy (used as a fallback when there's no
// TTY available — e.g. CI, piped output).
export async function runMonitorNoTUI(flags: MonitorFlags): Promise<void> {
  const store = new Store(flags.log, 1000);
  const bus = new Bus();
  const proxy = new Proxy(store, bus, { port: flags.port, anthropicURL: flags.anthropicURL, openaiURL: flags.openaiURL });
  const { port } = await startProxy(proxy, flags.port);
  process.stderr.write(instructions(port));
  await new Promise(() => {});
}
