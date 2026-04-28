package main

import (
	"fmt"
	"strings"
	"time"

	"github.com/spf13/cobra"
)

var sessionCmd = &cobra.Command{
	Use:   "session <session_id>",
	Short: "Drill into a single session's tool-by-tool breakdown",
	Args:  cobra.ExactArgs(1),
	RunE:  runSession,
}

func runSession(cmd *cobra.Command, args []string) error {
	cfg, err := loadConfig()
	if err != nil {
		return fmt.Errorf("lens not initialized — run `lens init` first")
	}

	db, err := openDB(cfg.DBPath)
	if err != nil {
		return err
	}
	defer db.Close()

	// Resolve short date string (e.g. "04-24 14:32") to full session ID
	sessionID := args[0]
	if len(sessionID) < 20 {
		row := db.QueryRow(`
			SELECT session_id FROM events
			WHERE strftime('%m-%d %H:%M', timestamp, 'localtime') = ?
			LIMIT 1`, sessionID)
		var resolved string
		if err := row.Scan(&resolved); err == nil {
			sessionID = resolved
		}
	}

	var project, firstSeen string
	err = db.QueryRow(`
		SELECT project, MIN(timestamp) FROM events WHERE session_id = ?`,
		sessionID).Scan(&project, &firstSeen)
	if err != nil {
		return fmt.Errorf("session not found: %s", args[0])
	}

	ts, _ := time.Parse("2006-01-02T15:04:05Z", firstSeen)

	rows, err := db.Query(`
		SELECT tool_name,
		       COUNT(*) AS calls,
		       SUM(input_chars + output_chars) / 4 AS est_tokens
		FROM events
		WHERE session_id = ?
		GROUP BY tool_name
		ORDER BY est_tokens DESC`, sessionID)
	if err != nil {
		return err
	}
	defer rows.Close()

	type toolRow struct {
		name   string
		calls  int64
		tokens int64
	}
	var tools []toolRow
	var totalCalls, totalTokens int64
	for rows.Next() {
		var t toolRow
		rows.Scan(&t.name, &t.calls, &t.tokens)
		tools = append(tools, t)
		totalCalls += t.calls
		totalTokens += t.tokens
	}

	const width = 50
	divider := strings.Repeat("─", width)
	header := fmt.Sprintf("SESSION  %s  ·  %s",
		ts.Local().Format("2006-01-02 15:04"), project)

	fmt.Println(header)
	fmt.Println(divider)
	fmt.Printf(" %-16s  %-7s  %s\n", "tool", "calls", "est. tokens")
	fmt.Println(divider)

	for _, t := range tools {
		fmt.Printf(" %-16s  %-7d  ~%s\n", t.name, t.calls, formatTokens(t.tokens))
	}

	fmt.Println(divider)
	fmt.Printf(" %-16s  %-7d  ~%s\n", "total", totalCalls, formatTokens(totalTokens))

	return nil
}
