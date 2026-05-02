#!/usr/bin/env bash
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"

echo "Updating lens..."
git -C "$SCRIPT_DIR" pull
bash "$SCRIPT_DIR/install.sh"
echo "Done. Run 'lens show' to verify."
