#!/usr/bin/env bash
# Stages the built wasm + Go loader + xterm runtime into extension/ so it can be
# loaded unpacked. All copied artifacts are gitignored; rerun after build.sh.
set -euo pipefail
here="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
repo="$(cd "$here/.." && pwd)"

if [ ! -f "$repo/wasm/ui/dtui.wasm" ]; then
  echo "!! $repo/wasm/ui/dtui.wasm not found — run ./wasm/build.sh first" >&2
  exit 1
fi

echo "==> copying wasm + loader into $here"
cp "$repo/wasm/ui/dtui.wasm" "$here/dtui.wasm"
cp "$repo/wasm/ui/wasm_exec.js" "$here/wasm_exec.js"

if [ ! -f "$here/vendor/xterm.js" ]; then
  echo "==> vendoring xterm"
  bash "$here/fetch-xterm.sh"
fi

echo "==> done. Load $here as an unpacked extension (chrome://extensions)."
