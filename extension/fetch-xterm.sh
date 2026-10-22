#!/usr/bin/env bash
# Vendors the xterm.js runtime into extension/vendor/ (gitignored). MV3's CSP
# forbids loading remote scripts, so the terminal emulator must be a local file.
set -euo pipefail
here="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
vendor="$here/vendor"
mkdir -p "$vendor"

XTERM=5.5.0
FIT=0.10.0
base="https://cdn.jsdelivr.net/npm"

echo "==> fetching xterm@$XTERM + addon-fit@$FIT into $vendor"
curl -fsSL "$base/@xterm/xterm@$XTERM/lib/xterm.js"        -o "$vendor/xterm.js"
curl -fsSL "$base/@xterm/xterm@$XTERM/css/xterm.css"       -o "$vendor/xterm.css"
curl -fsSL "$base/@xterm/addon-fit@$FIT/lib/addon-fit.js"  -o "$vendor/addon-fit.js"
echo "==> done"
