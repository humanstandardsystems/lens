package main

import (
	"database/sql"
	"os"
	"path/filepath"

	_ "modernc.org/sqlite"
)

const schema = `
CREATE TABLE IF NOT EXISTS events (
    session_id    TEXT NOT NULL,
    project       TEXT NOT NULL,
    timestamp     TEXT NOT NULL,
    tool_name     TEXT NOT NULL,
    input_chars   INTEGER NOT NULL DEFAULT 0,
    output_chars  INTEGER NOT NULL DEFAULT 0,
    file_path     TEXT NOT NULL DEFAULT ''
);
CREATE INDEX IF NOT EXISTS idx_session   ON events (session_id);
CREATE INDEX IF NOT EXISTS idx_project   ON events (project);
CREATE INDEX IF NOT EXISTS idx_timestamp ON events (timestamp);
`

func openDB(dbPath string) (*sql.DB, error) {
	if err := os.MkdirAll(filepath.Dir(dbPath), 0755); err != nil {
		return nil, err
	}
	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		return nil, err
	}
	if _, err := db.Exec(schema); err != nil {
		db.Close()
		return nil, err
	}
	return db, nil
}
