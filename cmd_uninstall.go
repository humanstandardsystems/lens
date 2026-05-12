package main

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"
	"github.com/tidwall/gjson"
	"github.com/tidwall/sjson"
)

var uninstallCmd = &cobra.Command{
	Use:   "uninstall",
	Short: "Unwire lens from Claude Code (strip settings.json entries, optionally remove ~/.lens/)",
	RunE:  runUninstall,
}

func runUninstall(cmd *cobra.Command, args []string) error {
	fmt.Println("Uninstalling lens...")
	fmt.Println()

	if err := unwireSettings(); err != nil {
		fmt.Fprintf(os.Stderr, "warning: %v\n", err)
	}

	promptRemoveLensDir()

	fmt.Println()
	fmt.Println("Next step: brew uninstall lens")
	fmt.Println()
	fmt.Println("Note: Claude Code's running session holds the old hook reference in memory")
	fmt.Println("until restart. The self-defense guard in hook.sh + statusline.sh exits clean")
	fmt.Println("once the lens binary is gone, so no error spam in the meantime.")
	return nil
}

// unwireSettings strips lens entries from ~/.claude/settings.json using
// path-based JSON edits, so non-lens keys keep their original byte-for-byte
// formatting (key order, whitespace, escaping). The bash version used
// python3's json.dump which naturally preserved insertion order; this is
// the Go-native equivalent.
func unwireSettings() error {
	settingsPath := filepath.Join(os.Getenv("HOME"), ".claude", "settings.json")
	data, err := os.ReadFile(settingsPath)
	if err != nil {
		if os.IsNotExist(err) {
			fmt.Printf("  · %s not found (skipped)\n", settingsPath)
			return nil
		}
		return fmt.Errorf("reading %s: %w", settingsPath, err)
	}

	out := data
	changed := false

	// 1. statusLine
	if strings.Contains(gjson.GetBytes(out, "statusLine.command").String(), ".lens/statusline.sh") {
		next, err := sjson.DeleteBytes(out, "statusLine")
		if err != nil {
			return fmt.Errorf("removing statusLine: %w", err)
		}
		out = next
		changed = true
	}

	// 2. PostToolUse — walk array in reverse so deletes don't shift pending indices.
	post := gjson.GetBytes(out, "hooks.PostToolUse")
	if post.IsArray() {
		entries := post.Array()
		for i := len(entries) - 1; i >= 0; i-- {
			inner := entries[i].Get("hooks")
			if !inner.IsArray() {
				continue
			}
			innerArr := inner.Array()
			var matches []int
			for j, h := range innerArr {
				if strings.Contains(h.Get("command").String(), ".lens/hook.sh") {
					matches = append(matches, j)
				}
			}
			if len(matches) == 0 {
				continue
			}
			changed = true
			if len(matches) == len(innerArr) {
				// All inner hooks match → drop the whole outer entry.
				next, err := sjson.DeleteBytes(out, fmt.Sprintf("hooks.PostToolUse.%d", i))
				if err != nil {
					return fmt.Errorf("removing PostToolUse entry: %w", err)
				}
				out = next
			} else {
				// Drop only the matching inner hooks (reverse to keep indices stable).
				for k := len(matches) - 1; k >= 0; k-- {
					next, err := sjson.DeleteBytes(out, fmt.Sprintf("hooks.PostToolUse.%d.hooks.%d", i, matches[k]))
					if err != nil {
						return fmt.Errorf("removing inner hook: %w", err)
					}
					out = next
				}
			}
		}

		// If PostToolUse is now empty, drop the key. Same for hooks itself.
		if p := gjson.GetBytes(out, "hooks.PostToolUse"); p.IsArray() && len(p.Array()) == 0 {
			if next, err := sjson.DeleteBytes(out, "hooks.PostToolUse"); err == nil {
				out = next
			}
		}
		if h := gjson.GetBytes(out, "hooks"); h.IsObject() {
			empty := true
			h.ForEach(func(_, _ gjson.Result) bool {
				empty = false
				return false
			})
			if empty {
				if next, err := sjson.DeleteBytes(out, "hooks"); err == nil {
					out = next
				}
			}
		}
	}

	if !changed {
		fmt.Printf("  · no lens entries found in %s (skipped)\n", settingsPath)
		return nil
	}

	backup := settingsPath + ".bak"
	if err := os.WriteFile(backup, data, 0644); err != nil {
		return fmt.Errorf("writing backup %s: %w", backup, err)
	}

	if err := os.WriteFile(settingsPath, out, 0644); err != nil {
		return fmt.Errorf("writing %s: %w", settingsPath, err)
	}

	fmt.Printf("  ✓ removed lens entries from %s\n", settingsPath)
	fmt.Printf("    (backup at %s)\n", backup)
	return nil
}

func promptRemoveLensDir() {
	dir := lensDir()
	info, err := os.Stat(dir)
	if err != nil || !info.IsDir() {
		return
	}

	size := dirSizeHuman(dir)

	fmt.Println()
	fmt.Printf("Your historical data lives at %s (%s).\n", dir, size)
	fmt.Println("You can keep it (a future re-install picks it back up) or delete it (gone for good).")
	fmt.Println()

	reader, closer, ok := openInteractiveReader()
	if !ok {
		fmt.Printf("  · no terminal attached; kept %s\n", dir)
		return
	}
	defer closer()

	fmt.Printf("Delete %s? [y/N] ", dir)
	input, err := reader.ReadString('\n')
	if err != nil && !errors.Is(err, io.EOF) {
		fmt.Printf("  · kept %s\n", dir)
		return
	}
	input = strings.TrimSpace(strings.ToLower(input))
	if input == "y" || input == "yes" {
		if err := os.RemoveAll(dir); err != nil {
			fmt.Fprintf(os.Stderr, "  ! failed to remove %s: %v\n", dir, err)
			return
		}
		fmt.Printf("  ✓ removed %s\n", dir)
		return
	}
	fmt.Printf("  · kept %s\n", dir)
}

func dirSizeHuman(path string) string {
	var total int64
	filepath.Walk(path, func(_ string, info os.FileInfo, err error) error {
		if err != nil {
			return nil
		}
		if !info.IsDir() {
			total += info.Size()
		}
		return nil
	})
	const KB, MB, GB int64 = 1024, 1024 * 1024, 1024 * 1024 * 1024
	switch {
	case total >= GB:
		return fmt.Sprintf("%.1fG", float64(total)/float64(GB))
	case total >= MB:
		return fmt.Sprintf("%.1fM", float64(total)/float64(MB))
	case total >= KB:
		return fmt.Sprintf("%.1fK", float64(total)/float64(KB))
	default:
		return fmt.Sprintf("%dB", total)
	}
}
