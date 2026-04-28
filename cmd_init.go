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

	if err := wireHook(hookPath); err != nil {
		fmt.Fprintf(os.Stderr, "warning: could not auto-wire hook (%v)\n", err)
		fmt.Printf("Add this to ~/.claude/settings.json manually:\n")
		fmt.Printf(`  {"hooks":{"PostToolUse":[{"matcher":"","hooks":[{"type":"command","command":"bash %s"}]}]}}`+"\n", hookPath)
	} else {
		fmt.Println("Hook wired. Restart Claude Code to activate.")
	}

	return nil
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
