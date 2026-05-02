package main

import (
	"bufio"
	"encoding/json"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
)

type Turn struct {
	MessageID    string
	Model        string
	Timestamp    string
	InputTokens  int64
	CacheCreate  int64
	CacheRead    int64
	OutputTokens int64
}

type jsonlLine struct {
	Type      string          `json:"type"`
	Timestamp string          `json:"timestamp"`
	CWD       string          `json:"cwd"`
	Message   *jsonlMessage   `json:"message"`
}

type jsonlMessage struct {
	ID    string        `json:"id"`
	Model string        `json:"model"`
	Role  string        `json:"role"`
	Usage *jsonlUsage   `json:"usage"`
}

type jsonlUsage struct {
	InputTokens              int64 `json:"input_tokens"`
	CacheCreationInputTokens int64 `json:"cache_creation_input_tokens"`
	CacheReadInputTokens     int64 `json:"cache_read_input_tokens"`
	OutputTokens             int64 `json:"output_tokens"`
}

// parseTranscriptIncremental reads a JSONL transcript from fromOffset,
// returns parsed assistant turns, the new file offset, and the project (from cwd).
func parseTranscriptIncremental(path string, fromOffset int64) (turns []Turn, newOffset int64, project string, err error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, fromOffset, "", err
	}
	defer f.Close()

	// Read project from start of file regardless of offset
	project = projectFromPath(path)
	if fromOffset == 0 {
		project = readProjectFromFile(f)
		if _, err2 := f.Seek(0, io.SeekStart); err2 != nil {
			return nil, fromOffset, project, err2
		}
	}

	if fromOffset > 0 {
		if _, err2 := f.Seek(fromOffset, io.SeekStart); err2 != nil {
			return nil, fromOffset, project, err2
		}
	}

	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 1024*1024), 1024*1024)

	for scanner.Scan() {
		var line jsonlLine
		if err2 := json.Unmarshal(scanner.Bytes(), &line); err2 != nil {
			continue
		}
		// Snag project from cwd if we haven't gotten it yet
		if project == "" && line.CWD != "" {
			project = filepath.Base(line.CWD)
		}
		if line.Type != "assistant" || line.Message == nil || line.Message.Usage == nil {
			continue
		}
		if line.Message.ID == "" {
			continue
		}
		turns = append(turns, Turn{
			MessageID:    line.Message.ID,
			Model:        line.Message.Model,
			Timestamp:    line.Timestamp,
			InputTokens:  line.Message.Usage.InputTokens,
			CacheCreate:  line.Message.Usage.CacheCreationInputTokens,
			CacheRead:    line.Message.Usage.CacheReadInputTokens,
			OutputTokens: line.Message.Usage.OutputTokens,
		})
	}
	if err2 := scanner.Err(); err2 != nil {
		return turns, fromOffset, project, err2
	}

	pos, _ := f.Seek(0, io.SeekCurrent)
	if pos == 0 {
		if info, serr := f.Stat(); serr == nil {
			pos = info.Size()
		}
	}
	return turns, pos, project, nil
}

// readProjectFromFile reads the first ~20 lines to find a cwd field.
func readProjectFromFile(f *os.File) string {
	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 256*1024), 256*1024)
	for i := 0; i < 30 && scanner.Scan(); i++ {
		var line jsonlLine
		if json.Unmarshal(scanner.Bytes(), &line) == nil && line.CWD != "" {
			return filepath.Base(line.CWD)
		}
	}
	return ""
}

// projectFromPath derives a best-guess project name from the JSONL folder path.
// Folder format: -Users-foo-bar-myproject → "myproject"
func projectFromPath(path string) string {
	folder := filepath.Base(filepath.Dir(path))
	// strip leading dash
	folder = strings.TrimPrefix(folder, "-")
	// last segment after all dashes is a rough approximation only
	parts := strings.Split(folder, "-")
	if len(parts) > 0 {
		return parts[len(parts)-1]
	}
	return "unknown"
}

// walkTranscripts calls cb for each JSONL transcript under ~/.claude/projects/.
func walkTranscripts(cb func(path, sessionID string)) {
	home, err := os.UserHomeDir()
	if err != nil {
		return
	}
	projectsDir := filepath.Join(home, ".claude", "projects")

	filepath.WalkDir(projectsDir, func(p string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return nil
		}
		name := d.Name()
		if !strings.HasSuffix(name, ".jsonl") {
			return nil
		}
		sessionID := strings.TrimSuffix(name, ".jsonl")
		cb(p, sessionID)
		return nil
	})
}
