#!/usr/bin/env bash
set -euo pipefail

INSTALL_DIR="/usr/local/bin"
BINARY="lens"
LENS_DIR="$HOME/.lens"
SETTINGS="$HOME/.claude/settings.json"

echo "Uninstalling lens..."
echo ""

# 1. Remove the binary
if [ -f "$INSTALL_DIR/$BINARY" ]; then
  if [ -w "$INSTALL_DIR" ]; then
    rm -f "$INSTALL_DIR/$BINARY"
  else
    sudo rm -f "$INSTALL_DIR/$BINARY"
  fi
  echo "  ✓ removed $INSTALL_DIR/$BINARY"
else
  echo "  · $INSTALL_DIR/$BINARY not found (skipped)"
fi

# 2. Surgically remove lens entries from ~/.claude/settings.json
if [ -f "$SETTINGS" ]; then
  python3 - "$SETTINGS" <<'PY'
import json, sys, os, shutil

path = sys.argv[1]
with open(path) as f:
    settings = json.load(f)

changed = False

# Drop the statusLine if it points at lens
sl = settings.get("statusLine")
if isinstance(sl, dict) and ".lens/statusline.sh" in str(sl.get("command", "")):
    settings.pop("statusLine", None)
    changed = True

# Drop any PostToolUse hook entry that references lens/hook.sh
hooks = settings.get("hooks", {})
post = hooks.get("PostToolUse", [])
if isinstance(post, list):
    cleaned = []
    for entry in post:
        if not isinstance(entry, dict):
            cleaned.append(entry); continue
        inner = entry.get("hooks", [])
        kept = [h for h in inner if not (
            isinstance(h, dict)
            and ".lens/hook.sh" in str(h.get("command", ""))
        )]
        if kept:
            entry["hooks"] = kept
            cleaned.append(entry)
        else:
            changed = True  # whole entry dropped
    if cleaned != post:
        hooks["PostToolUse"] = cleaned
        changed = True
    if not hooks["PostToolUse"]:
        hooks.pop("PostToolUse", None)
        changed = True
    if not hooks:
        settings.pop("hooks", None)

if changed:
    backup = path + ".bak"
    shutil.copy(path, backup)
    with open(path, "w") as f:
        json.dump(settings, f, indent=2)
        f.write("\n")
    print(f"  ✓ removed lens entries from {path}")
    print(f"    (backup at {backup})")
else:
    print(f"  · no lens entries found in {path} (skipped)")
PY
else
  echo "  · $SETTINGS not found (skipped)"
fi

# 3. Ask before nuking ~/.lens/
echo ""
if [ -d "$LENS_DIR" ]; then
  echo "Your historical data lives at $LENS_DIR ($(du -sh "$LENS_DIR" 2>/dev/null | cut -f1))."
  echo "This is the only thing left. You can keep it (re-installing later picks it back up)"
  echo "or delete it (gone for good)."
  echo ""
  read -p "Delete $LENS_DIR? [y/N] " -r REPLY
  if [[ "$REPLY" =~ ^[Yy]$ ]]; then
    rm -rf "$LENS_DIR"
    echo "  ✓ removed $LENS_DIR"
  else
    echo "  · kept $LENS_DIR"
  fi
fi

echo ""
echo "lens uninstalled. Restart Claude Code to drop the hook + statusline from the live session."
