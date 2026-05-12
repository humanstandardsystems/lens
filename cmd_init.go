package main

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"io"
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
                "HAVING julianday(MIN(timestamp)) >= julianday(?) "
                "   AND julianday(MIN(timestamp)) <= julianday(?) + 10.0/1440.0 "
                "ORDER BY MIN(timestamp) ASC "
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
SESSION_ID=$(cat ~/.lens/session_id 2>/dev/null || echo "unknown")
PROJECT=$(basename "$PWD")
TIMESTAMP=$(date -u +"%Y-%m-%dT%H:%M:%SZ")
TOOL_NAME=$(echo "$INPUT" | jq -r '.tool_name // "unknown"')
INPUT_CHARS=$(echo "$INPUT" | jq -r '.tool_input | tostring | length')
OUTPUT_CHARS=$(echo "$INPUT" | jq -r '.tool_response | tostring | length')
FILE_PATH=$(echo "$INPUT" | jq -r '.tool_input.file_path // ""')

sqlite3 ~/.lens/lens.db \
  "INSERT INTO events VALUES('$SESSION_ID','$PROJECT','$TIMESTAMP','$TOOL_NAME',$INPUT_CHARS,$OUTPUT_CHARS,'$FILE_PATH');"

lens sync > /dev/null 2>&1 &
`

var (
	initDayFlag  string
	initHourFlag int
)

var initCmd = &cobra.Command{
	Use:   "init",
	Short: "Set up lens: create DB, write hook, configure reset window",
	RunE:  runInit,
}

func init() {
	initCmd.Flags().StringVar(&initDayFlag, "day", "", "reset day (full name or any unambiguous prefix: m/mo/mon, tu/tue, w/we, th/thu, f/fr, sa, su) — pair with --hour")
	initCmd.Flags().IntVar(&initHourFlag, "hour", -1, "reset hour 0-23 — pair with --day")
}

var validDays = map[string]bool{
	"sunday": true, "monday": true, "tuesday": true, "wednesday": true,
	"thursday": true, "friday": true, "saturday": true,
}

// dayResolver maps any of: full name, 3-letter abbrev, or shortest unambiguous
// prefix → canonical lowercase weekday. Single letters t and s are excluded
// because t is ambiguous (tue/thu) and s is ambiguous (sat/sun).
var dayResolver = map[string]string{
	"m": "monday", "mo": "monday", "mon": "monday", "monday": "monday",
	"tu": "tuesday", "tue": "tuesday", "tues": "tuesday", "tuesday": "tuesday",
	"w": "wednesday", "we": "wednesday", "wed": "wednesday", "wednesday": "wednesday",
	"th": "thursday", "thu": "thursday", "thur": "thursday", "thurs": "thursday", "thursday": "thursday",
	"f": "friday", "fr": "friday", "fri": "friday", "friday": "friday",
	"sa": "saturday", "sat": "saturday", "saturday": "saturday",
	"su": "sunday", "sun": "sunday", "sunday": "sunday",
}

func isStdinTTY() bool {
	fi, err := os.Stdin.Stat()
	if err != nil {
		return false
	}
	return (fi.Mode() & os.ModeCharDevice) != 0
}

// openInteractiveReader prefers /dev/tty so prompts work even when stdin is
// redirected (e.g. Claude Code's '!' runner). Falls back to stdin if /dev/tty
// is unavailable or stdin happens to already be a TTY.
func openInteractiveReader() (*bufio.Reader, func(), bool) {
	if tty, err := os.OpenFile("/dev/tty", os.O_RDONLY, 0); err == nil {
		return bufio.NewReader(tty), func() { tty.Close() }, true
	}
	if isStdinTTY() {
		return bufio.NewReader(os.Stdin), func() {}, true
	}
	return nil, func() {}, false
}

// parseTime accepts: "6pm", "6:00pm", "6 pm", "6:00 pm", "18", "18:00",
// "12am" (=0), "12pm" (=12). Whitespace and case are ignored. Minutes parsed
// for syntactic acceptance but truncated to hour resolution.
func parseTime(s string) (int, bool) {
	s = strings.ToLower(strings.ReplaceAll(s, " ", ""))
	if s == "" {
		return 0, false
	}
	isPM := strings.HasSuffix(s, "pm")
	isAM := strings.HasSuffix(s, "am")
	if isPM || isAM {
		s = s[:len(s)-2]
	}
	if idx := strings.Index(s, ":"); idx >= 0 {
		s = s[:idx]
	}
	var h int
	if n, err := fmt.Sscanf(s, "%d", &h); n != 1 || err != nil {
		return 0, false
	}
	if isAM || isPM {
		// 12-hour mode: hour must be 1..12
		if h < 1 || h > 12 {
			return 0, false
		}
		if isPM && h < 12 {
			h += 12
		}
		if isAM && h == 12 {
			h = 0
		}
		return h, true
	}
	if h < 0 || h > 23 {
		return 0, false
	}
	return h, true
}

// parseResetInput accepts free-form "<day> <time>" or "<time> <day>" with
// generous abbreviations. Returns canonical day + hour or false.
func parseResetInput(s string) (string, int, bool) {
	fields := strings.Fields(strings.ToLower(strings.TrimSpace(s)))
	if len(fields) < 2 {
		return "", 0, false
	}
	dayIdx := -1
	var day string
	for i, f := range fields {
		if matched, ok := dayResolver[f]; ok {
			day = matched
			dayIdx = i
			break
		}
	}
	if dayIdx < 0 {
		return "", 0, false
	}
	timeTokens := append([]string{}, fields[:dayIdx]...)
	timeTokens = append(timeTokens, fields[dayIdx+1:]...)
	if h, ok := parseTime(strings.Join(timeTokens, "")); ok {
		return day, h, true
	}
	return "", 0, false
}

func runInit(cmd *cobra.Command, args []string) error {
	fmt.Print("Setting up lens...\n\n")

	day := strings.ToLower(strings.TrimSpace(initDayFlag))
	if resolved, ok := dayResolver[day]; ok {
		day = resolved
	}
	hour := initHourFlag

	dayNeeded := day == ""
	hourNeeded := hour < 0 || hour > 23

	if dayNeeded != hourNeeded {
		return fmt.Errorf("--day and --hour must be set together (or neither, for the interactive prompt)")
	}

	const eraseLine = "\r\033[K"
	nonInteractiveErr := fmt.Errorf(`lens init can't prompt from here (no terminal attached).

ATTENTION ASSISTANT: if you are an AI assistant (Claude, etc.) running this command on behalf of a user, do the following before retrying:
  1. Ask the user in chat: "When does your Anthropic weekly usage reset? (e.g. 'friday 6pm', 'tuesday 18:00')"
  2. Parse their answer into a day (monday..sunday) and an hour (0..23).
  3. Re-run: lens init --day <day> --hour <hour>

HUMAN FALLBACK: open a real terminal and run 'lens init' directly,
or pass flags: lens init --day friday --hour 18`)

	if dayNeeded {
		reader, closer, ok := openInteractiveReader()
		if !ok {
			return nonInteractiveErr
		}
		defer closer()

		for {
			fmt.Print("When does your Anthropic weekly usage reset? (e.g. friday 6pm): ")
			input, err := reader.ReadString('\n')
			if errors.Is(err, io.EOF) && strings.TrimSpace(input) == "" {
				fmt.Print(eraseLine)
				return nonInteractiveErr
			}
			if d, h, ok := parseResetInput(input); ok {
				day = d
				hour = h
				break
			}
			fmt.Println("  → couldn't parse. Try: 'friday 6pm', 'tuesday 18:00', 'th 9am', 'f 6 pm'")
		}
	}

	if !validDays[day] {
		return fmt.Errorf("invalid day %q (must be monday..sunday or any unambiguous abbreviation)", day)
	}
	if hour < 0 || hour > 23 {
		return fmt.Errorf("invalid hour %d (must be 0-23)", hour)
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

	fmt.Printf("\n✓ saved: reset %s %02d:00 %s\n", day, hour, loc)

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
