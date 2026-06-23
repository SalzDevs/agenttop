import { spawn, spawnSync } from "node:child_process";
import { existsSync } from "node:fs";
import { resolve, basename } from "node:path";

export const SESSION = "agenttop";

// Instructions returns the printed help block when agenttop starts.
export function instructions(port: number): string {
  return `agenttop listening on http://127.0.0.1:${port}

One command (recommended, needs tmux):
  agenttop claude
  agenttop opencode
  agenttop codex

Two-terminal mode (no tmux):
  terminal 1: agenttop
  terminal 2: agenttop run -- claude
`;
}

// isOpencode reports whether the command being launched is opencode.
// opencode does not read ANTHROPIC_BASE_URL / OPENAI_BASE_URL; instead it
// configures provider base URLs via its config file. We detect opencode by
// command basename and inject an inline runtime config (OPENCODE_CONFIG_CONTENT)
// that redirects its providers to the agenttop proxy.
export function isOpencode(cmd: string): boolean {
  return basename(resolve(cmd)).toLowerCase().includes("opencode");
}

// opencodeConfigContent returns a minimal opencode config JSON that overrides
// the provider base URLs to point at the agenttop proxy.
//
// For anthropic and openai (standard API providers), we just set baseURL — the
// proxy knows their upstreams (api.anthropic.com / api.openai.com) from its
// routing logic.
//
// For opencode-go and opencode (Zen) — OpenCode's own subscription providers —
// also set a custom header `x-agenttop-upstream` that tells the proxy the real
// upstream URL, since these use OpenAI-format requests that the proxy would
// otherwise misroute to api.openai.com.
export function opencodeConfigContent(port: number): string {
  const base = `http://127.0.0.1:${port}/v1`;
  return JSON.stringify({
    provider: {
      anthropic: { options: { baseURL: base } },
      openai: { options: { baseURL: base } },
      "opencode-go": {
        options: {
          baseURL: base,
          headers: { "x-agenttop-upstream": "https://opencode.ai/zen/go" },
        },
      },
      opencode: {
        options: {
          baseURL: base,
          headers: { "x-agenttop-upstream": "https://opencode.ai/zen" },
        },
      },
    },
  });
}

// envForAgent returns the extra environment entries needed to point an agent
// at the agenttop proxy on the given port. For opencode it also injects
// OPENCODE_CONFIG_CONTENT so its provider config redirects to the proxy.
export function envForAgent(cmd: string, port: number): string[] {
  const env = [
    `ANTHROPIC_BASE_URL=http://127.0.0.1:${port}`,
    `OPENAI_BASE_URL=http://127.0.0.1:${port}/v1`,
    `OPENAI_API_BASE=http://127.0.0.1:${port}/v1`,
  ];
  if (isOpencode(cmd)) {
    env.push(`OPENCODE_CONFIG_CONTENT=${opencodeConfigContent(port)}`);
  }
  return env;
}

// selfParts returns the argv parts needed to re-invoke agenttop.
// When launched as a compiled binary, this is just [binaryPath].
// When launched as `bun run src/cli.ts ...`, this is [bunPath, absScriptPath]
// so the tmux pane command works regardless of the pane's cwd.
export function selfParts(): string[] {
  const exec = process.execPath;
  const argv1 = process.argv[1] || "";
  // Compiled binary: argv[1] is the first user arg (e.g. "claude"), not a path.
  if (!argv1.endsWith(".ts") && !argv1.endsWith(".js")) {
    return [exec];
  }
  // Dev mode: resolve to absolute path so the tmux pane can find it
  // regardless of its working directory.
  return [exec, resolve(argv1)];
}

// shellSelf returns the shell-quoted self invocation string.
export function shellSelf(): string {
  return selfParts().map(shellQuote).join(" ");
}
// waitPort blocks until something is listening on 127.0.0.1:port (up to ~10s).
// Used so an agent doesn't start before the monitor's proxy is up.
export async function waitPort(port: number): Promise<void> {
  for (let i = 0; i < 200; i++) {
    try {
      const resp = await fetch(`http://127.0.0.1:${port}/`, { method: "GET" }).catch(() => null);
      if (resp) return;
    } catch {
      // not up yet
    }
    await Bun.sleep(50);
  }
}

// Wrap runs a single agent command with the proxy env vars injected, after
// waiting for the proxy to be ready. Returns the child's exit code.
export async function wrap(args: string[], port: number): Promise<number> {
  if (args.length === 0) {
    process.stderr.write("agenttop: no command given after --\n");
    return 2;
  }
  await waitPort(port);
  return new Promise<number>((resolveP) => {
    const child = spawn(args[0], args.slice(1), {
      stdio: "inherit",
      env: { ...process.env, ...Object.fromEntries(envForAgent(args[0], port).map((kv) => kv.split(/=(.+)/) as [string, string])) },
    });
    child.on("exit", (code) => resolveP(code ?? 0));
    child.on("error", () => resolveP(1));
  });
}

// shellQuote wraps s in single quotes for safe shell use, escaping any inner
// single quotes via the standard '\'' idiom.
export function shellQuote(s: string): string {
  return "'" + s.replace(/'/g, "'\\''") + "'";
}

export function shellJoin(args: string[]): string {
  return args.map(shellQuote).join(" ");
}

export interface TmuxStep {
  args: string[];
}

// buildTmuxCommands constructs the ordered tmux argv list for a one-command
// split session. Pure function so it can be unit-tested without tmux.
//
// Top pane:    "<self> --port <port> monitor"   (starts proxy + monitor TUI)
// Bottom pane: "<self> --port <port> wait; <agent>; tmux kill-session -t <sess>"
//
// The agent inherits ANTHROPIC_BASE_URL / OPENAI_BASE_URL / OPENAI_API_BASE
// (and OPENCODE_CONFIG_CONTENT for opencode) from the session environment, set
// via `tmux set-environment` so the agent pane receives them.
export function buildTmuxCommands(
  selfParts: string[],
  sess: string,
  agentCmd: string[],
  port: number,
): TmuxStep[] {
  const baseURL = `http://127.0.0.1:${port}`;
  const baseURLv1 = `http://127.0.0.1:${port}/v1`;

  const selfQuoted = selfParts.map(shellQuote).join(" ");
  const monitorShell = `${selfQuoted} --port ${port} monitor`;
  const waitShell = `${selfQuoted} --port ${port} wait`;
  const agentShell = shellJoin(agentCmd);
  const killShell = `tmux kill-session -t ${shellQuote(sess)}`;
  const paneShell = `${waitShell}; ${agentShell}; ${killShell}`;

  const steps: TmuxStep[] = [
    // `new-session -d` auto-starts the tmux server, so this single command
    // is enough to bring up a fresh server (and ignore the no-op kill
    // if no prior session exists).
    { args: ["tmux", "kill-session", "-t", sess] },
    { args: ["tmux", "new-session", "-d", "-s", sess, monitorShell] },
    { args: ["tmux", "set-environment", "-t", sess, "ANTHROPIC_BASE_URL", baseURL] },
    { args: ["tmux", "set-environment", "-t", sess, "OPENAI_BASE_URL", baseURLv1] },
    { args: ["tmux", "set-environment", "-t", sess, "OPENAI_API_BASE", baseURLv1] },
  ];

  if (agentCmd.length > 0 && isOpencode(agentCmd[0])) {
    steps.push({
      args: [
        "tmux",
        "set-environment",
        "-t",
        sess,
        "OPENCODE_CONFIG_CONTENT",
        opencodeConfigContent(port),
      ],
    });
  }

  steps.push(
    // Create the bottom (agent) pane.
    { args: ["tmux", "split-window", "-v", "-t", sess, paneShell] },
    // Resize the top (monitor) pane to 5 rows.
    { args: ["tmux", "resize-pane", "-t", `${sess}:0.0`, "-y", "5"] },
  );

  return steps;
}

// tmuxSplit creates a tmux session with the monitor on top and the agent on
// the bottom, then attaches. When the agent exits, the pane shell kills the
// session (so detaching returns). If tmux isn't installed, it prints install
// hints and the two-terminal fallback and returns an error.
export async function tmuxSplit(
  agentCmd: string[],
  port: number,
): Promise<number> {
  if (agentCmd.length === 0) {
    process.stderr.write("agenttop: no command given\n");
    return 1;
  }
  if (!existsSync("/usr/bin/tmux") && !existsSync("/usr/local/bin/tmux") && !existsSync("/opt/homebrew/bin/tmux")) {
    const which = spawnSync("which", ["tmux"]);
    if (which.status !== 0) {
      process.stderr.write(
        "tmux not found — install it for the one-command split:\n" +
          "  brew install tmux   (macOS)\n" +
          "  apt-get install tmux (Ubuntu/Debian)\n\n" +
          "Until then, use two terminals:\n" +
          `  1) agenttop --port ${port}\n` +
          `  2) agenttop run -- ${agentCmd.join(" ")}\n`,
      );
      return 1;
    }
  }

  const self = selfParts();
  for (const s of buildTmuxCommands(self, SESSION, agentCmd, port)) {
    const r = spawnSync(s.args[0], s.args.slice(1), {
      stdio: ["ignore", "inherit", "pipe"],
    });
    // kill-session fails if no session exists; ignore that one.
    if (r.status !== 0 && s.args[1] !== "kill-session") {
      const msg = r.stderr?.toString().trim() || r.error?.message || `exit ${r.status}`;
      process.stderr.write(`agenttop: tmux ${s.args.slice(1).join(" ")}: ${msg}\n`);
      return 1;
    }
  }

  const attach = spawn("tmux", ["attach", "-t", SESSION], {
    stdio: "inherit",
  });
  return new Promise<number>((resolveP) => {
    attach.on("exit", (code) => resolveP(code ?? 0));
    attach.on("error", () => resolveP(1));
  });
}
