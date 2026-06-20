# agenttop

> **htop, but for AI coding agents.** Watch what Claude Code, Cursor, Codex and Gemini CLI are doing — and what they're costing you — in real time, right in your terminal.

<p align="center">
  <img src="docs/demo.gif" width="880" alt="agenttop demo: live agents, token counts and cost burn ticking up">
</p>

`agenttop` is a tiny local proxy that sits between your AI coding agents and the model providers. Every request that flows through it shows up live: the model, token counts, latency, the prompt, the response — and a running dollar total that ticks up as your agents work.

One binary. No config. No account. Your API keys never touch `agenttop` — it just forwards them.

## Why

You've got 3 agents running. One is burning your $200 Claude plan. One is stuck in a loop. One just sent your entire codebase to the model *again*. You have no idea which is which.

`agenttop` fixes that. It's the dashboard you wished you had when the bill arrived.

## Install

```bash
# macOS / Linux
curl -sSf https://raw.githubusercontent.com/SalzDevs/agenttop/main/scripts/install.sh | bash

# Go
go install github.com/SalzDevs/agenttop@latest

# Build from source
git clone https://github.com/SalzDevs/agenttop && cd agenttop && make build
```

## Quickstart

Terminal 1 — start the monitor:

```bash
agenttop
```

Terminal 2 — run your agent through it:

```bash
agenttop run -- claude          # Claude Code
agenttop run -- codex           # OpenAI Codex CLI
agenttop run -- gemini          # Gemini CLI
```

That's it. Watch the dashboard light up.

Want them side by side? Throw it in tmux:

```bash
tmux new-session 'agenttop' \; split-window -h 'agenttop run -- claude'
```

## What you see

- **Live request table** — every model call, with provider, model, status, input/output tokens, and per-request cost. In-flight requests show a spinner and stream in real time.
- **Running total** — cumulative `$` spent, total tokens in/out, request count, and a live **burn rate** in `$/hour` computed over the last 60 seconds.
- **Detail pane** — select any request to see the prompt preview and the response that came back.
- **Per-agent support** — Claude Code, Cursor, Codex CLI, Gemini CLI, OpenCode, and anything else that lets you set `ANTHROPIC_BASE_URL` / `OPENAI_BASE_URL` / `OPENAI_API_BASE`.

## How it works

```
your agent ──HTTP──▶ agenttop (localhost:7331) ──forward──▶ Anthropic / OpenAI
                          │
                          └─ count tokens · compute $ · capture prompt & response
                             └─▶ live TUI  (+ optional JSONL log)
```

`agenttop` is a transparent reverse proxy. It streams responses straight back to your agent with zero buffering, so latency is unaffected. In parallel it parses the token `usage` from each response and computes cost from a built-in pricing table. Keys are forwarded unchanged and never stored.

## Flags

```
--port, -p <n>   proxy listen port (default 7331)
--log,  -l <p>   append every event to a JSONL file
```

## Roadmap

- [x] Live multi-request table + cost burn
- [x] Anthropic & OpenAI streaming + non-streaming usage
- [ ] DVR replay — scrub back through any agent's session
- [ ] Tool-call & file-change capture via Claude Code / OpenCode hooks
- [ ] Cost & rate-limit alarms (desktop notifications)
- [ ] tmux split mode (`agenttop -- claude` does the split for you)
- [ ] Web dashboard mode
- [ ] Community-maintained pricing table (auto-updated)

## Contributing

PRs welcome. The pricing table in `internal/pricing/pricing.go` goes out of date fast — model additions and price corrections are great first PRs.

## License

MIT
