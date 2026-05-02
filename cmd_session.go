package main

import (
	"fmt"
	"strings"
	"time"

	"github.com/spf13/cobra"
)

var sessionCmd = &cobra.Command{
	Use:   "session <session_id>",
	Short: "Drill into a single session: tool counts + cache timeline",
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

	syncAllSessions(db)

	// Resolve short date string (e.g. "04-24 14:32") or partial UUID to full session ID.
	// session IDs in turns are UUIDs (from JSONL filenames).
	sessionID := args[0]
	if len(sessionID) < 36 {
		var resolved string
		err := db.QueryRow(`
			SELECT session_id FROM turns
			WHERE strftime('%m-%d %H:%M', timestamp, 'localtime') = ?
			LIMIT 1`, sessionID).Scan(&resolved)
		if err != nil || resolved == "" {
			return fmt.Errorf("session not found: %s", args[0])
		}
		sessionID = resolved
	}

	// Get project + first seen from turns
	var project, firstSeen string
	err = db.QueryRow(`
		SELECT project, MIN(timestamp) FROM turns WHERE session_id = ?`,
		sessionID).Scan(&project, &firstSeen)
	if err != nil {
		return fmt.Errorf("session not found: %s", args[0])
	}

	ts := parseTimestamp(firstSeen)

	const width = 50
	divider := strings.Repeat("─", width)
	header := fmt.Sprintf("SESSION  %s  ·  %s",
		ts.Local().Format("2006-01-02 15:04"), project)

	fmt.Println(header)

	// ── section 1: tool counts from events ─────────────────────
	// Events use a different session ID (timestamp format), so match by project + time window.
	// Find the events session whose start time is within 15 minutes of our first turn.
	var eventsSessionID string
	db.QueryRow(`
		SELECT session_id FROM events
		WHERE project = ?
		  AND ABS(julianday(timestamp) - julianday(?)) < 0.01
		GROUP BY session_id
		ORDER BY MIN(timestamp) ASC
		LIMIT 1`, project, firstSeen).Scan(&eventsSessionID)

	if eventsSessionID != "" {
		toolRows, err := db.Query(`
			SELECT tool_name, COUNT(*) AS calls
			FROM events
			WHERE session_id = ?
			GROUP BY tool_name
			ORDER BY calls DESC`, eventsSessionID)

		if err == nil {
			type toolRow struct {
				name  string
				calls int64
			}
			var tools []toolRow
			var totalCalls int64
			for toolRows.Next() {
				var t toolRow
				toolRows.Scan(&t.name, &t.calls)
				tools = append(tools, t)
				totalCalls += t.calls
			}
			toolRows.Close()

			if len(tools) > 0 {
				fmt.Println(divider)
				fmt.Printf(" %-20s  %s\n", "tool", "calls")
				fmt.Println(divider)
				for _, t := range tools {
					fmt.Printf(" %-20s  %d\n", t.name, t.calls)
				}
				fmt.Println(divider)
				fmt.Printf(" %-20s  %d tool calls\n", "total", totalCalls)
			}
		}
	}

	// ── section 2: cache timeline from turns ────────────────────
	turnRows, err := db.Query(`
		SELECT timestamp, model, input_tokens, cache_create, cache_read, output_tokens, message_id
		FROM turns
		WHERE session_id = ?
		ORDER BY timestamp ASC`, sessionID)
	if err != nil {
		return nil
	}
	defer turnRows.Close()

	type turnRow struct {
		ts           time.Time
		model        string
		inputTokens  int64
		cacheCreate  int64
		cacheRead    int64
		outputTokens int64
		messageID    string
	}
	var turns []turnRow
	for turnRows.Next() {
		var t turnRow
		var tsStr string
		turnRows.Scan(&tsStr, &t.model, &t.inputTokens, &t.cacheCreate, &t.cacheRead, &t.outputTokens, &t.messageID)
		t.ts = parseTimestamp(tsStr)
		turns = append(turns, t)
	}

	if len(turns) == 0 {
		fmt.Println("\n(No transcript data for this session.)")
		return nil
	}

	var totalTokens, totalCacheRead, totalCacheInput int64
	for _, t := range turns {
		totalTokens += t.inputTokens + t.cacheCreate + t.cacheRead + t.outputTokens
		totalCacheRead += t.cacheRead
		totalCacheInput += t.inputTokens + t.cacheCreate + t.cacheRead
	}

	var hitRateStr string
	warn := ""
	if totalCacheInput > 0 {
		rate := float64(totalCacheRead) / float64(totalCacheInput)
		pct := int(rate * 100)
		if rate < 0.50 {
			warn = " ⚠"
		}
		hitRateStr = fmt.Sprintf("%d%%%s", pct, warn)
	} else {
		hitRateStr = "—"
	}

	fmt.Printf("\nCACHE TIMELINE  ·  %d turns  ·  %s tokens  ·  %s\n",
		len(turns), formatTokensShort(totalTokens), hitRateStr)
	fmt.Println(divider)
	fmt.Printf(" %-5s  %-5s  %-6s  %-5s  %s\n", "turn", "time", "in", "out", "cache")
	fmt.Println(divider)

	indices := selectTurnIndices(len(turns))

	var prevRate float64
	for _, i := range indices {
		t := turns[i]
		timeStr := t.ts.Local().Format("15:04")
		inStr := formatTokensShort(t.inputTokens + t.cacheCreate + t.cacheRead)
		outStr := formatTokensShort(t.outputTokens)

		denom := t.inputTokens + t.cacheCreate + t.cacheRead
		var bar, pctStr, hint string
		if denom > 0 {
			rate := float64(t.cacheRead) / float64(denom)
			bar = cacheBar(rate)
			pct := int(rate * 100)
			turnWarn := ""
			if rate < 0.50 {
				turnWarn = " ⚠"
			}
			pctStr = fmt.Sprintf("%d%%%s", pct, turnWarn)

			if i == 0 {
				hint = " (cold start)"
			} else if prevRate-rate > 0.40 {
				hint = " (cache invalidated)"
			}
			prevRate = rate
		} else {
			bar = strings.Repeat("░", 9)
			pctStr = "—"
		}

		fmt.Printf(" %-5d  %-5s  %-6s  %-5s  %s %s%s\n",
			i+1, timeStr, inStr, outStr, bar, pctStr, hint)
	}
	fmt.Println(divider)

	return nil
}

func selectTurnIndices(n int) []int {
	if n <= 20 {
		out := make([]int, n)
		for i := range out {
			out[i] = i
		}
		return out
	}
	seen := make(map[int]bool)
	var out []int

	add := func(i int) {
		if !seen[i] {
			seen[i] = true
			out = append(out, i)
		}
	}

	for i := 0; i < 5; i++ {
		add(i)
	}
	step := (n - 10) / 10
	if step < 1 {
		step = 1
	}
	for i := 5; i < n-5; i += step {
		add(i)
	}
	for i := n - 5; i < n; i++ {
		add(i)
	}

	for i := 1; i < len(out); i++ {
		for j := i; j > 0 && out[j] < out[j-1]; j-- {
			out[j], out[j-1] = out[j-1], out[j]
		}
	}
	return out
}
