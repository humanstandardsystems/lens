package main

import (
	"fmt"
	"strings"
	"time"

	"github.com/spf13/cobra"
)

type sessionRow struct {
	sessionID string
	project   string
	firstSeen time.Time
	estTokens int64
}

var (
	showProject string
	showAll     bool
)

var showCmd = &cobra.Command{
	Use:   "show",
	Short: "Rank sessions by estimated token usage",
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

	base := `
		SELECT session_id, project,
		       MIN(timestamp) AS first_seen,
		       SUM(input_chars + output_chars) / 4 AS est_tokens
		FROM events
		WHERE 1=1`
	var qargs []interface{}

	if !showAll {
		ws := weekStart(cfg)
		base += " AND timestamp >= ?"
		qargs = append(qargs, ws.UTC().Format("2006-01-02T15:04:05Z"))
	}
	if showProject != "" {
		base += " AND project = ?"
		qargs = append(qargs, showProject)
	}

	query := base + " GROUP BY session_id ORDER BY est_tokens DESC LIMIT 10"

	rows, err := db.Query(query, qargs...)
	if err != nil {
		return err
	}
	defer rows.Close()

	var sessions []sessionRow
	for rows.Next() {
		var s sessionRow
		var tsStr string
		if err := rows.Scan(&s.sessionID, &s.project, &tsStr, &s.estTokens); err != nil {
			continue
		}
		s.firstSeen, _ = time.Parse("2006-01-02T15:04:05Z", tsStr)
		sessions = append(sessions, s)
	}

	if len(sessions) == 0 {
		fmt.Println("No data yet. Make sure Claude Code is running with the hook active.")
		return nil
	}

	ws := weekStart(cfg)
	we := ws.Add(7 * 24 * time.Hour)
	header := fmt.Sprintf("LENS — AI Spend  ·  week of %s–%s",
		ws.Format("Jan 2"), we.Format("Jan 2"))
	if showAll {
		header = "LENS — AI Spend  ·  all time"
	}

	const width = 50
	divider := strings.Repeat("─", width)

	fmt.Println(header)
	fmt.Println(divider)
	fmt.Printf(" %-3s  %-14s  %-16s  %s\n", "#", "date", "project", "est. tokens")
	fmt.Println(divider)

	var total int64
	for i, s := range sessions {
		dateStr := s.firstSeen.Local().Format("01-02 15:04")
		project := s.project
		if len(project) > 16 {
			project = project[:15] + "…"
		}
		fmt.Printf(" %-3d  %-14s  %-16s  ~%s\n", i+1, dateStr, project, formatTokens(s.estTokens))
		total += s.estTokens
	}

	fmt.Println(divider)
	totalLabel := "total this week"
	if showAll {
		totalLabel = "total all time"
	}
	fmt.Printf(" %-35s  ~%s\n", totalLabel, formatTokens(total))

	return nil
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
