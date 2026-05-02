package main

import (
	"database/sql"
	"fmt"
	"time"
)

func syncSession(db *sql.DB, sessionID, path string) error {
	var storedPath string
	var lastOffset int64

	err := db.QueryRow(`
		SELECT transcript_path, last_parsed_offset
		FROM transcript_watermark WHERE session_id = ?`, sessionID).
		Scan(&storedPath, &lastOffset)

	if err == sql.ErrNoRows {
		storedPath = path
		lastOffset = 0
	} else if err != nil {
		return err
	} else if storedPath == "" {
		storedPath = path
	}

	turns, newOffset, project, err := parseTranscriptIncremental(storedPath, lastOffset)
	if err != nil {
		return err
	}
	if len(turns) == 0 && lastOffset > 0 {
		// nothing new
		return nil
	}

	tx, err := db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	for _, t := range turns {
		p := project
		if p == "" {
			p = "unknown"
		}
		_, err := tx.Exec(`
			INSERT OR IGNORE INTO turns
			  (session_id, project, timestamp, model, input_tokens, cache_create, cache_read, output_tokens, message_id)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
			sessionID, p, t.Timestamp, t.Model,
			t.InputTokens, t.CacheCreate, t.CacheRead, t.OutputTokens, t.MessageID)
		if err != nil {
			return err
		}
	}

	now := time.Now().UTC().Format("2006-01-02T15:04:05Z")
	_, err = tx.Exec(`
		INSERT INTO transcript_watermark (session_id, transcript_path, last_parsed_offset, last_parsed_at)
		VALUES (?, ?, ?, ?)
		ON CONFLICT(session_id) DO UPDATE SET
		  transcript_path    = excluded.transcript_path,
		  last_parsed_offset = excluded.last_parsed_offset,
		  last_parsed_at     = excluded.last_parsed_at`,
		sessionID, storedPath, newOffset, now)
	if err != nil {
		return err
	}

	return tx.Commit()
}

func syncAllSessions(db *sql.DB) {
	// Count transcripts and existing turns to detect first run
	var existingTurns int64
	db.QueryRow(`SELECT COUNT(*) FROM turns`).Scan(&existingTurns)

	// Count total JSONL files
	var total int
	walkTranscripts(func(path, sessionID string) {
		total++
	})

	isFirstRun := existingTurns == 0 && total > 0
	if isFirstRun {
		fmt.Printf("First run after Phase 2 upgrade. Backfilling %d transcripts...", total)
	}

	start := time.Now()
	walkTranscripts(func(path, sessionID string) {
		syncSession(db, sessionID, path)
	})
	elapsed := time.Since(start).Seconds()

	if isFirstRun {
		var count int64
		db.QueryRow(`SELECT COUNT(*) FROM turns`).Scan(&count)
		fmt.Printf("  done (%.1fs, %d turns).\n", elapsed, count)
	}
}
