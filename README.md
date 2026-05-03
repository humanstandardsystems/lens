# lens

A token-usage and cache-health tracker for Claude Code. Lens watches your transcripts and tells you where your tokens go — and how well your cache is working.

All data lives in `~/.lens/` on your machine. Nothing leaves.

---

## 1. What it is

Claude Code burns tokens. Most of them quietly. Some of those tokens are cache hits (cheap, fast). Many are not (expensive, slow). Without instrumentation you have no idea which is which, and your weekly limit shows up as a surprise.

Lens fixes that. It reads Claude Code's transcript files directly — the same JSONL Claude itself writes — so the numbers are exact, not estimates. It exposes them in two places:

- **A statusline** always visible inside Claude Code: this session's tokens, this week's tokens, current cache hit rate.
- **A CLI** (`lens show`, `lens session`) for drilling into history.

Lens is read-only on Claude Code. It hooks one event (`PostToolUse`) to record tool calls, and otherwise just observes the transcripts Claude has already written.

---

## 2. Install

```bash
git clone https://github.com/humanstandardsystems/lens.git ~/lens
bash ~/lens/install.sh
lens init
```

Restart Claude Code after `lens init` to activate the hook.

`lens init` will prompt for your Anthropic weekly reset day and time, then auto-wire the statusline and the `PostToolUse` hook into `~/.claude/settings.json`. It's idempotent — running it twice is safe.

Requirements: macOS (Apple Silicon or Intel). Linux support is on the roadmap.

---

## 3. Use

### The statusline

Once installed, every Claude Code prompt shows a line like:

```
⬡ 247k tok/sess   ⏺ 2.3M tok/wk   67% cache
```

Below 50% cache hit rate the percentage flips orange and a `⚠` appears. That's your cue to look at what's invalidating cache.

### The CLI

```bash
lens show                    # this week's sessions with cache hit rate
lens show --all              # all time
lens show --project myapp    # filter to one project
lens session 04-24           # drill into a session by date (MM-DD or MM-DD HH:MM)
lens sync                    # force a transcript re-parse (rarely needed; the hook calls this)
```

`lens show` example:

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

---

## 4. How it works

The one critical thing to know:

**Claude Code's own JSONL transcripts are ground truth.** The hook session ID is not the same as the JSONL UUID — early versions of lens trusted hook IDs and got wrong numbers. The fix was to walk the transcripts directly.

So lens has two data sources:

1. **A `PostToolUse` hook** at `~/.lens/hook.sh` records tool calls into `events`. This powers the per-tool counts in `lens session`.
2. **A transcript parser** lazily reads `~/.claude/projects/**/*.jsonl` on every `lens show` / `lens session` / `lens sync` call. It extracts real per-turn token counts (input, cache create, cache read, output) and writes them to `turns`. A byte-offset watermark in `transcript_watermark` keeps re-parses fast — only new bytes get touched.

Two wrinkles worth knowing because they were the bugs that bit:

- **Sessions are matched by timestamp**, not by ID, since the hook's session ID and the JSONL UUID don't agree. The match window is 10 minutes via `julianday()` arithmetic. SQLite's `datetime()` text comparison was previously used and quietly broke on format mismatch.
- **The hook calls `lens sync` in the background** after each tool invocation, so the statusline reflects the current turn within seconds of it landing.

---

## 5. Architecture

```
~/.lens/
├── lens.db            ← SQLite. Three tables: events, turns, transcript_watermark.
├── config.toml        ← reset day/time/timezone, db path
├── hook.sh            ← PostToolUse hook (writes to events, kicks off `lens sync`)
├── statusline.sh      ← bash + embedded python; reads lens.db, paints the line
└── session_id         ← current Claude Code session ID (15-char timestamp)
```

Schema (in `db.go`):

- **events** — one row per tool call (session_id, project, timestamp, tool_name, input/output chars, file_path).
- **turns** — one row per Claude turn parsed from JSONL (session_id, project, timestamp, model, input_tokens, cache_create, cache_read, output_tokens, message_id). Primary key `(session_id, message_id)` so re-parses are idempotent.
- **transcript_watermark** — one row per session tracking how many bytes of its JSONL have been parsed.

Code layout:

```
main.go            ← cobra root
cmd_init.go        ← lens init: writes hook.sh + statusline.sh, wires settings.json
cmd_show.go        ← lens show
cmd_session.go     ← lens session
cmd_sync.go        ← lens sync (force a transcript re-parse)
sync.go            ← the parser itself; respects the watermark
transcript.go      ← JSONL reader
config.go          ← config.toml load/save, timezone detection
db.go              ← schema + open
```

---

## 6. Maintain

### Update

```bash
bash ~/lens/update.sh
```

That's `git pull` + re-run `install.sh`. Install is idempotent.

### Uninstall

```bash
bash ~/lens/uninstall.sh
```

Leaves cleanly:

1. Removes `/usr/local/bin/lens` (the binary).
2. Surgically removes lens's entries from `~/.claude/settings.json` (the `PostToolUse` hook and the `statusLine`). Other hooks and your other settings are untouched. A `.bak` file is written next to it before edit.
3. **Asks** before deleting `~/.lens/` — that's your historical data. Keeping it means a future re-install picks up where you left off. Deleting it is forever.

Restart Claude Code after uninstall to drop the hook + statusline from the live session.

---

## 7. Roadmap

Phase 2 shipped (statusline + accurate JSONL-based tracking). Phase 3 is three separate features, listed in priority order:

1. **Per-hook injection cost** — every `UserPromptSubmit` / `SessionStart` hook in `settings.json` injects context into each turn. Lens currently can't tell you which hook is costing what. Goal: per-hook token cost in `lens session` and a warning when any single hook exceeds 500 tok/turn. _Why first: it's the missing piece that would have caught the ICM hook bloat without having to instrument by hand._
2. **Cache crater analysis** — when `lens session` shows a turn at 0% cache, name the likely cause (file edit invalidating the cache, new system message, etc.). Goal: don't just show the crater, point at what dug it.
3. **Pattern detection across sessions** — surface recurring shapes (e.g. "this project averages 38% cache" / "Read-heavy sessions cost 2x more than Edit-heavy"). Goal: weekly insight, not just per-session.

Other open items:

- **Linux support** — `install.sh` currently bails on non-Darwin.

---

## Build from source

Requires Go 1.21+.

```bash
git clone https://github.com/humanstandardsystems/lens.git
cd lens
make install
```
