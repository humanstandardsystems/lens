# lens

Token usage and cache health tracker for Claude Code. See where your tokens go — and how well your cache is working.

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
lens show                  # this week's sessions with cache hit rate
lens show --all            # all time
lens show --project myapp  # filter to one project
lens session 04-24         # drill into a session by date (MM-DD or MM-DD HH:MM)
```

Example output:

```
WEEK · 04-28 → 05-05
─────────────────────────────────────────────────────────
 session         project         tokens   cache
─────────────────────────────────────────────────────────
 04-29 14:32     latent-space    247k     ▓▓▓▓▓▓▓▓░ 82%
 04-30 09:15     lens            1.2M     ▓▓▓░░░░░░ 34% ⚠
 05-01 11:02     r29k            890k     ▓▓▓▓▓▓▓░░ 71%
─────────────────────────────────────────────────────────
 week total                      2.3M     ▓▓▓▓▓▓░░░ 67%
```

`⚠` appears on sessions with cache hit rate below 50%.

`lens session` shows a two-section drill-in — tool call counts plus a per-turn cache timeline:

```
SESSION  2026-04-30 09:15  ·  lens
──────────────────────────────────────────────────
 tool                  calls
──────────────────────────────────────────────────
 Read                  47
 Edit                  12
 Bash                  8
──────────────────────────────────────────────────
 total                 67 tool calls

CACHE TIMELINE  ·  18 turns  ·  1.2M tokens  ·  34% ⚠
──────────────────────────────────────────────────
 turn   time   in      out    cache
──────────────────────────────────────────────────
 1      09:15  8.2k    320    ▓░░░░░░░░  4% (cold start)
 5      09:22  1.1k    180    ▓▓▓▓▓▓▓▓░ 88%
 14     09:58  12.0k   450    ░░░░░░░░░  0% ⚠ (cache invalidated)
──────────────────────────────────────────────────
```

## What's new in v0.2.0

Token counts and cache hit rates now come from Claude Code's transcript files (`~/.claude/projects/`), giving exact per-turn numbers instead of character-count estimates. The first run after upgrading backfills all historical transcripts automatically. Subsequent runs only parse new turns via a byte-offset watermark — no new hooks needed.

The `events` table and PostToolUse hook are unchanged; they still power tool call counts in `lens session`.

## How it works

lens hooks into Claude Code's `PostToolUse` event to record tool calls into a local SQLite database. On every `lens show` or `lens session` call, it lazily parses Claude Code's JSONL transcript files to extract real token counts and cache breakdowns, caching parsed turns in the DB with a watermark so subsequent runs are fast.

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
