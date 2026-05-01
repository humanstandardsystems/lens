# lens

Token usage tracker for Claude Code. Know where your AI spend is going.

## Install

```bash
git clone https://github.com/humanstandardsystems/lens.git ~/lens
bash ~/lens/install.sh
```

## Setup

```bash
lens init
```

Prompts for your Anthropic reset day and time, then wires a Claude Code hook automatically. Restart Claude Code after running.

## Usage

```bash
lens show                  # this week's sessions, ranked by token usage
lens show --all            # all time
lens show --project myapp  # filter to one project
lens session 04-24         # drill into a session by date (MM-DD or MM-DD HH:MM)
```

Example output:

```
LENS — AI Spend  ·  week of Apr 24–May 1
──────────────────────────────────────────────────
 #   date            project           est. tokens
──────────────────────────────────────────────────
 1   04-28 14:32     latent-space      ~84,201
 2   04-27 09:11     americana         ~61,430
 3   04-25 22:04     r29k              ~33,882
──────────────────────────────────────────────────
 total this week                       ~179,513
```

## How it works

lens hooks into Claude Code's `PostToolUse` event to record every tool call into a local SQLite database. Token usage is estimated from character counts (÷ 4), grouped by session and project.

All data lives in `~/.lens/` — never leaves your machine.

## Requirements

macOS (Apple Silicon or Intel). Linux support coming.

## Build from source

Requires Go 1.21+.

```bash
git clone https://github.com/humanstandardsystems/lens.git
cd lens
make install
```
