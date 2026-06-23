#!/usr/bin/env bun
// agenttop CLI — htop for AI coding agents.
//
// Subcommands:
//   (none)             start monitor + proxy in this terminal
//   monitor            same
//   run -- <cmd>       run an agent pointed at the monitor (other terminal)
//   wait               block until the proxy is listening
//   version            print version
//   help               print usage
//
// Default behavior (no args, or unknown first arg):
//   Treat the args as a one-command tmux split. `agenttop claude` opens a
//   tmux session with the monitor on top and `claude` on the bottom.

import { tmuxSplit, wrap, waitPort } from "./launch/launch.js";
import { runMonitor } from "./tui/app.js";

const VERSION = "0.1.0";

interface Flags {
  port: number;
  log: string;
  anthropicURL: string;
  openaiURL: string;
}

function parseFlags(argv: string[]): { flags: Flags; rest: string[] } {
  const flags: Flags = {
    port: 7331,
    log: "",
    anthropicURL: "",
    openaiURL: "",
  };
  const rest: string[] = [];
  for (let i = 0; i < argv.length; i++) {
    const a = argv[i];
    if (a === "--port" || a === "-p") {
      flags.port = Number(argv[++i]);
    } else if (a === "--log" || a === "-l") {
      flags.log = argv[++i];
    } else if (a === "--anthropic-url") {
      flags.anthropicURL = argv[++i];
    } else if (a === "--openai-url") {
      flags.openaiURL = argv[++i];
    } else {
      rest.push(a);
    }
  }
  return { flags, rest };
}

const USAGE = `agenttop — htop for AI coding agents

Watch what Claude Code, Cursor, Codex, OpenCode and Gemini CLI are doing
and what they're costing you. In real time. In your terminal.

One command (recommended — needs tmux):
  agenttop claude            monitor on top, claude below, in one window
  agenttop opencode          same, for OpenCode
  agenttop codex             same, for Codex CLI
  agenttop --port 8000 claude   pick the proxy port

Just the monitor (two-terminal mode, no tmux):
  agenttop                   start the monitor + proxy
  agenttop run -- claude     run an agent pointed at the monitor (other terminal)

Flags:
  --port, -p <n>         proxy listen port (default 7331)
  --log,  -l <p>         append events to a JSONL file (default: none)
  --anthropic-url <url>  override Anthropic upstream (for local LLMs / proxies)
  --openai-url <url>     override OpenAI upstream (for local LLMs / proxies)
`;

async function main(): Promise<void> {
  const { flags, rest } = parseFlags(process.argv.slice(2));

  // Bare `agenttop` → start the monitor (two-terminal mode).
  if (rest.length === 0) {
    await runMonitor(flags);
    return;
  }

  const cmd = rest[0];
  switch (cmd) {
    case "monitor":
      await runMonitor(flags);
      return;
    case "run": {
      // `agenttop run -- <cmd> [args]` → wrapper only (waits for proxy, injects env).
      const after = stripDash(rest.slice(1));
      const code = await wrap(after, flags.port);
      process.exit(code);
      return;
    }
    case "wait":
      await waitPort(flags.port);
      return;
    case "version":
    case "-v":
    case "--version":
      process.stdout.write(`agenttop ${VERSION}\n`);
      return;
    case "help":
    case "-h":
    case "--help":
      process.stdout.write(USAGE);
      return;
    default: {
      // `agenttop <agent> [args]` → one-command tmux split.
      const code = await tmuxSplit(rest, flags.port);
      process.exit(code);
      return;
    }
  }
}

// stripDash drops leading "--" tokens (so `run -- claude` and `run claude` both work).
function stripDash(args: string[]): string[] {
  while (args.length > 0 && args[0] === "--") args = args.slice(1);
  return args;
}

main().catch((err) => {
  process.stderr.write(`agenttop: ${err?.stack || err}\n`);
  process.exit(1);
});
