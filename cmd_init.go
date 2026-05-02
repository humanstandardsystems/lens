package main

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"
)

const statuslineScript = `#!/bin/bash
DB="$HOME/.lens/lens.db"
CONFIG="$HOME/.lens/config.toml"
[ -f "$DB" ] || exit 0

RESET_DAY=$(grep 'reset_day' "$CONFIG" 2>/dev/null | sed 's/.*= *"\(.*\)"/\1/' | tr '[:upper:]' '[:lower:]')
RESET_HOUR=$(grep 'reset_hour' "$CONFIG" 2>/dev/null | grep -o '[0-9]*')
TZ_NAME=$(grep 'reset_timezone' "$CONFIG" 2>/dev/null | sed 's/.*= *"\(.*\)"/\1/')

python3 - <<EOF
import sqlite3, os
from datetime import datetime, timedelta, timezone

try:
    from zoneinfo import ZoneInfo
    tz = ZoneInfo("${TZ_NAME:-America/Los_Angeles}")
except Exception:
    tz = timezone.utc

day_map  = {"monday":0,"tuesday":1,"wednesday":2,"thursday":3,"friday":4,"saturday":5,"sunday":6}
reset_day     = "${RESET_DAY:-tuesday}"
reset_weekday = day_map.get(reset_day, 1)

now = datetime.now(tz)
candidate = now.replace(hour=${RESET_HOUR:-18}, minute=0, second=0, microsecond=0)
while candidate.weekday() != reset_weekday or candidate > now:
    candidate -= timedelta(days=1)
week_start = candidate.astimezone(timezone.utc).strftime("%Y-%m-%dT%H:%M:%SZ")

db = sqlite3.connect(os.path.expanduser("~/.lens/lens.db"))

row = db.execute(
    "SELECT "
    "  SUM(input_tokens + cache_create + cache_read + output_tokens), "
    "  CAST(SUM(cache_read) AS REAL) / MAX(SUM(input_tokens + cache_create + cache_read), 1) "
    "FROM turns WHERE timestamp >= ?",
    (week_start,)
).fetchone()
weekly   = row[0] or 0
hit_rate = row[1] or 0.0

session_tok = 0
session_id_file = os.path.expanduser("~/.lens/session_id")
if os.path.exists(session_id_file):
    with open(session_id_file) as f:
        raw = f.read().strip()
    if raw and len(raw) == 15:
        try:
            sess_dt  = datetime.strptime(raw, "%Y%m%dT%H%M%S").replace(tzinfo=timezone.utc)
            sess_str = sess_dt.strftime("%Y-%m-%dT%H:%M:%SZ")
            r = db.execute(
                "SELECT session_id FROM turns "
                "GROUP BY session_id "
                "HAVING ABS(julianday(MIN(timestamp)) - julianday(?)) < 0.0014 "
                "ORDER BY ABS(julianday(MIN(timestamp)) - julianday(?)) ASC "
                "LIMIT 1",
                (sess_str, sess_str)
            ).fetchone()
            if r:
                r2 = db.execute(
                    "SELECT SUM(input_tokens + cache_create + cache_read + output_tokens) "
                    "FROM turns WHERE session_id = ?",
                    (r[0],)
                ).fetchone()
                session_tok = r2[0] or 0
        except Exception:
            pass

db.close()

since = candidate.strftime("%b %-d")

def fmt(n):
    if n >= 1_000_000: return f"{n/1_000_000:.1f}M"
    if n >= 1_000:     return f"{n/1_000:.0f}k"
    return str(n)

pct = int(hit_rate * 100)
warn = " ⚠" if pct < 50 else ""

def c(n): return f"\033[38;5;{n}m"
R          = "\033[0m"
B          = "\033[1m"
BILL_GREEN = c(34)
HUNDO_BLUE = c(33)
SKY        = c(159)
SKY_DARK   = c(241)
CACHE_CLR  = c(214) if pct < 50 else c(79)

print(f"{BILL_GREEN}⬡{R} {SKY}{B}{fmt(session_tok)}{R} {SKY_DARK}tok/sess{R}   {HUNDO_BLUE}⏺{R} {SKY}{B}{fmt(weekly)}{R} {SKY_DARK}tok/wk{R}   {CACHE_CLR}{B}{pct}%{R} {SKY_DARK}cache{warn}{R}")
EOF
`

const hookScript = `#!/bin/bash
INPUT=$(cat)
SESSION_ID="${CLAUDE_SESSION_ID:-unknown}"
PROJECT=$(basename "$PWD")
TIMESTAMP=$(date -u +"%Y-%m-%dT%H:%M:%SZ")
TOOL_NAME=$(echo "$INPUT" | jq -r '.tool_name // "unknown"')
INPUT_CHARS=$(echo "$INPUT" | jq -r '.tool_input | tostring | length')
OUTPUT_CHARS=$(echo "$INPUT" | jq -r '.tool_response | tostring | length')
FILE_PATH=$(echo "$INPUT" | jq -r '.tool_input.file_path // ""')

sqlite3 ~/.lens/lens.db \
  "INSERT INTO events VALUES('$SESSION_ID','$PROJECT','$TIMESTAMP','$TOOL_NAME',$INPUT_CHARS,$OUTPUT_CHARS,'$FILE_PATH');"
`

var initCmd = &cobra.Command{
	Use:   "init",
	Short: "Set up lens: create DB, write hook, configure reset window",
	RunE:  runInit,
}

func runInit(cmd *cobra.Command, args []string) error {
	fmt.Println("Setting up lens...\n")
	reader := bufio.NewReader(os.Stdin)

	fmt.Print("When does your Anthropic weekly usage reset?\n")
	fmt.Print("  Day   (e.g. tuesday): ")
	day, _ := reader.ReadString('\n')
	day = strings.ToLower(strings.TrimSpace(day))
	if day == "" {
		day = "tuesday"
	}

	fmt.Print("  Time  (e.g. 18:00):   ")
	timeStr, _ := reader.ReadString('\n')
	timeStr = strings.TrimSpace(timeStr)
	hour := 18
	if timeStr != "" {
		fmt.Sscanf(timeStr, "%d:", &hour)
	}

	loc := "America/Chicago"
	if detected, err := detectTimezone(); err == nil {
		loc = detected
	}
	fmt.Printf("  Timezone: auto-detected as %s ✓\n\n", loc)

	cfg := Config{
		ResetDay:      day,
		ResetHour:     hour,
		ResetTimezone: loc,
		DBPath:        filepath.Join(lensDir(), "lens.db"),
	}
	if err := saveConfig(cfg); err != nil {
		return fmt.Errorf("saving config: %w", err)
	}

	db, err := openDB(cfg.DBPath)
	if err != nil {
		return fmt.Errorf("creating database: %w", err)
	}
	db.Close()

	hookPath := filepath.Join(lensDir(), "hook.sh")
	if err := os.WriteFile(hookPath, []byte(hookScript), 0755); err != nil {
		return fmt.Errorf("writing hook: %w", err)
	}

	statuslinePath := filepath.Join(lensDir(), "statusline.sh")
	if err := os.WriteFile(statuslinePath, []byte(statuslineScript), 0755); err != nil {
		return fmt.Errorf("writing statusline: %w", err)
	}

	if err := wireHook(hookPath); err != nil {
		fmt.Fprintf(os.Stderr, "warning: could not auto-wire hook (%v)\n", err)
		fmt.Printf("Add this to ~/.claude/settings.json manually:\n")
		fmt.Printf(`  {"hooks":{"PostToolUse":[{"matcher":"","hooks":[{"type":"command","command":"bash %s"}]}]}}`+"\n", hookPath)
	} else {
		fmt.Println("Hook wired. Restart Claude Code to activate.")
	}

	if err := wireStatusline(statuslinePath); err != nil {
		fmt.Fprintf(os.Stderr, "warning: could not auto-wire statusline (%v)\n", err)
		fmt.Printf("Add to ~/.claude/settings.json manually:\n")
		fmt.Printf(`  {"statusLine":{"type":"command","command":"bash %s"}}`+"\n", statuslinePath)
	} else {
		fmt.Println("Statusline wired.")
	}

	return nil
}

func wireStatusline(statuslinePath string) error {
	settingsPath := filepath.Join(os.Getenv("HOME"), ".claude", "settings.json")
	data, err := os.ReadFile(settingsPath)
	if err != nil {
		return err
	}
	var settings map[string]interface{}
	if err := json.Unmarshal(data, &settings); err != nil {
		return err
	}
	settings["statusLine"] = map[string]interface{}{
		"type":    "command",
		"command": "bash " + statuslinePath,
	}
	out, err := json.MarshalIndent(settings, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(settingsPath, out, 0644)
}

func wireHook(hookPath string) error {
	settingsPath := filepath.Join(os.Getenv("HOME"), ".claude", "settings.json")

	data, err := os.ReadFile(settingsPath)
	if err != nil {
		return err
	}

	var settings map[string]interface{}
	if err := json.Unmarshal(data, &settings); err != nil {
		return err
	}

	hooks, ok := settings["hooks"].(map[string]interface{})
	if !ok {
		hooks = map[string]interface{}{}
		settings["hooks"] = hooks
	}

	existing, _ := hooks["PostToolUse"].([]interface{})

	// Skip if already wired
	for _, h := range existing {
		hmap, ok := h.(map[string]interface{})
		if !ok {
			continue
		}
		hs, _ := hmap["hooks"].([]interface{})
		for _, inner := range hs {
			imap, ok := inner.(map[string]interface{})
			if !ok {
				continue
			}
			if cmd, ok := imap["command"].(string); ok && strings.Contains(cmd, ".lens/hook.sh") {
				return nil
			}
		}
	}

	hookEntry := map[string]interface{}{
		"matcher": "",
		"hooks": []interface{}{
			map[string]interface{}{
				"type":    "command",
				"command": "bash " + hookPath,
			},
		},
	}
	hooks["PostToolUse"] = append(existing, hookEntry)

	out, err := json.MarshalIndent(settings, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(settingsPath, out, 0644)
}
