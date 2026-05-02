package main

import (
	"fmt"
	"strings"
	"time"

	"github.com/spf13/cobra"
)

type sessionRow struct {
	sessionID   string
	project     string
	firstSeen   time.Time
	totalTokens int64
	hitRate     float64
	hasCache    bool
}

var (
	showProject string
	showAll     bool
)

var showCmd = &cobra.Command{
	Use:   "show",
	Short: "Show sessions with cache hit rate",
	RunE:  runShow,
}

func init() {
	showCmd.Flags().StringVar(&showProject, "project", "", "filter to one project")
	showCmd.Flags().BoolVar(&showAll, "all", false, "show all weeks")
}

func runShow(cmd *cobra.Command, args []string) error {
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

	ws := weekStart(cfg)
	wsStr := ws.UTC().Format("2006-01-02T15:04:05Z")

	var whereExtra string
	var qargs []interface{}

	if !showAll {
		whereExtra += " AND t.timestamp >= ?"
		qargs = append(qargs, wsStr)
	}
	if showProject != "" {
		whereExtra += " AND t.project = ?"
		qargs = append(qargs, showProject)
	}

	query := fmt.Sprintf(`
		SELECT
		  t.session_id,
		  t.project,
		  MIN(t.timestamp) AS started,
		  SUM(t.input_tokens + t.cache_create + t.cache_read + t.output_tokens) AS total_tokens,
		  CAST(SUM(t.cache_read) AS REAL) /
		    NULLIF(SUM(t.input_tokens + t.cache_create + t.cache_read), 0) AS hit_rate
		FROM turns t
		WHERE 1=1 %s
		GROUP BY t.session_id
		ORDER BY started DESC
		LIMIT 20`, whereExtra)

	rows, err := db.Query(query, qargs...)
	if err != nil {
		return err
	}
	defer rows.Close()

	var sessions []sessionRow
	for rows.Next() {
		var s sessionRow
		var tsStr string
		var hitRateNull *float64
		if err := rows.Scan(&s.sessionID, &s.project, &tsStr, &s.totalTokens, &hitRateNull); err != nil {
			continue
		}
		s.firstSeen = parseTimestamp(tsStr)
		if hitRateNull != nil {
			s.hitRate = *hitRateNull
			s.hasCache = true
		}
		sessions = append(sessions, s)
	}

	if len(sessions) == 0 {
		fmt.Println("No data yet. Make sure Claude Code is running with the hook active.")
		return nil
	}

	we := ws.Add(7 * 24 * time.Hour)
	header := fmt.Sprintf("WEEK · %s → %s", ws.Format("01-02"), we.Format("01-02"))
	if showAll {
		header = "ALL TIME"
	}

	const width = 57
	divider := strings.Repeat("─", width)

	fmt.Println(header)
	fmt.Println(divider)
	fmt.Printf(" %-14s  %-14s  %-7s  %s\n", "session", "project", "tokens", "cache")
	fmt.Println(divider)

	var totalTokens int64
	var totalCacheRead, totalCacheInput int64

	for _, s := range sessions {
		dateStr := s.firstSeen.Local().Format("01-02 15:04")
		project := s.project
		if len(project) > 14 {
			project = project[:13] + "…"
		}
		tokStr := formatTokensShort(s.totalTokens)

		var cacheStr string
		if !s.hasCache {
			cacheStr = "—"
		} else {
			bar := cacheBar(s.hitRate)
			pct := int(s.hitRate * 100)
			warn := ""
			if s.hitRate < 0.50 {
				warn = " ⚠"
			}
			cacheStr = fmt.Sprintf("%s %d%%%s", bar, pct, warn)
		}

		fmt.Printf(" %-14s  %-14s  %-7s  %s\n", dateStr, project, tokStr, cacheStr)
		totalTokens += s.totalTokens
	}

	// accumulate totals from turns for week summary
	if !showAll {
		db.QueryRow(`
			SELECT
			  SUM(cache_read),
			  SUM(input_tokens + cache_create + cache_read)
			FROM turns WHERE timestamp >= ?`, wsStr).
			Scan(&totalCacheRead, &totalCacheInput)
	} else {
		db.QueryRow(`
			SELECT SUM(cache_read), SUM(input_tokens + cache_create + cache_read)
			FROM turns`).Scan(&totalCacheRead, &totalCacheInput)
	}

	fmt.Println(divider)
	label := "week total"
	if showAll {
		label = "all time"
	}
	var weekCacheStr string
	if totalCacheInput > 0 {
		rate := float64(totalCacheRead) / float64(totalCacheInput)
		bar := cacheBar(rate)
		pct := int(rate * 100)
		warn := ""
		if rate < 0.50 {
			warn = " ⚠"
		}
		weekCacheStr = fmt.Sprintf("%s %d%%%s", bar, pct, warn)
	} else {
		weekCacheStr = "—"
	}
	fmt.Printf(" %-31s  %-7s  %s\n", label, formatTokensShort(totalTokens), weekCacheStr)

	return nil
}

// parseTimestamp handles both "2006-01-02T15:04:05Z" and "2006-01-02T15:04:05.999Z".
func parseTimestamp(s string) time.Time {
	t, err := time.Parse(time.RFC3339Nano, s)
	if err != nil {
		t, _ = time.Parse("2006-01-02T15:04:05Z", s)
	}
	return t
}

func cacheBar(rate float64) string {
	const cells = 9
	filled := int(rate*float64(cells) + 0.5)
	if filled > cells {
		filled = cells
	}
	return strings.Repeat("▓", filled) + strings.Repeat("░", cells-filled)
}

func formatTokensShort(n int64) string {
	if n >= 1_000_000 {
		return fmt.Sprintf("%.1fM", float64(n)/1_000_000)
	}
	if n >= 1_000 {
		return fmt.Sprintf("%.0fk", float64(n)/1_000)
	}
	return fmt.Sprintf("%d", n)
}

func formatTokens(n int64) string {
	if n >= 1_000_000 {
		return fmt.Sprintf("%d,%03d,%03d", n/1_000_000, (n/1000)%1000, n%1000)
	}
	if n >= 1000 {
		return fmt.Sprintf("%d,%03d", n/1000, n%1000)
	}
	return fmt.Sprintf("%d", n)
}
