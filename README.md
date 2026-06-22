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
# macOS / Linux (one command)
curl -sSf https://raw.githubusercontent.com/SalzDevs/agenttop/main/scripts/install.sh | bash

# Homebrew
brew install SalzDevs/tap/agenttop

# Go
go install github.com/SalzDevs/agenttop@latest

# Build from source
git clone https://github.com/SalzDevs/agenttop && cd agenttop && make build
```

## Quickstart

### One command (recommended)

Needs [`tmux`](https://github.com/tmux/tmux) — install it once: `brew install tmux` (macOS) or `apt install tmux` (Linux).

```bash
agenttop claude          # monitor on top, Claude Code below — one window
agenttop opencode        # same, for OpenCode
agenttop codex           # same, for Codex CLI
agenttop gemini          # same, for Gemini CLI
```

That's it. `agenttop` opens a single tmux window: the live monitor on top, your agent running below. When the agent exits, the window closes. Detach with `Ctrl-b d` (re-attach with `tmux attach -t agenttop`).

> **opencode note:** opencode configures providers through its config file rather than `*_BASE_URL` env vars, so `agenttop opencode` injects an inline runtime config (`OPENCODE_CONFIG_CONTENT`) that redirects its Anthropic and OpenAI providers to the proxy. Your API keys (stored in `~/.local/share/opencode/auth.json`) and the rest of your opencode config are untouched.

### Two-terminal mode (no tmux)

Prefer separate terminals, or don't have tmux? `agenttop` falls back to this automatically:

```bash
# terminal 1
agenttop

# terminal 2
agenttop run -- claude
```

## What you see

- **Live request table** — every model call, with provider, model, status, input/output tokens, and per-request cost. In-flight requests show a spinner and stream in real time.
- **Running total** — cumulative `$` spent, total tokens in/out, request count, and a live **burn rate** in `$/hour` computed over the last 60 seconds.
- **Detail pane** — select any request to see the prompt preview and the response that came back.
- **Per-agent support** — Claude Code, Cursor, Codex CLI, OpenCode, Gemini CLI, and anything else that lets you set `ANTHROPIC_BASE_URL` / `OPENAI_BASE_URL` / `OPENAI_API_BASE` (or, for opencode, a provider `baseURL`).

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
- [x] One-command tmux split (`agenttop claude` — monitor + agent in one window)
- [x] OpenCode support (config injection, no env vars needed)
- [ ] DVR replay — scrub back through any agent's session
- [ ] Tool-call & file-change capture via Claude Code / OpenCode hooks
- [ ] Cost & rate-limit alarms (desktop notifications)
- [ ] Web dashboard mode
- [ ] Community-maintained pricing table (auto-updated)

## Contributing

PRs welcome. The pricing table in `internal/pricing/pricing.go` goes out of date fast — model additions and price corrections are great first PRs.

## License

MIT
