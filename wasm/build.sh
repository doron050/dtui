#!/usr/bin/env bash
# Builds the dtui browser wasm (cmd/web) for the Chrome extension.
#
# Three upstream modules ship no js/wasm build support, so a plain
# `GOOS=js GOARCH=wasm go build ./cmd/web` fails to compile:
#   - charmbracelet/bubbletea : no TTY init/resize/suspend for js
#   - atotto/clipboard        : no readAll/writeAll for js
#   - dynatrace-oss/dtctl      : sdk/session/refresh_lock_unix.go (`!windows`)
#                                wrongly compiles on js and needs x/sys/unix;
#                                pkg/output/live_{unix,windows}.go define no
#                                resize hooks for js; pkg/usage/lock_unix.go
#                                has the same `!windows` flaw.
#
# We can't patch these in the module cache (`go build -overlay` forbids GOMODCACHE
# replacements, and the cache is read-only). So we copy just these four modules
# into wasm/.deps/ (gitignored), add the missing js build-tagged files, and wire
# them in through a SCOPED workspace file (wasm/go.work.wasm) selected via the
# GOWORK env var. Nothing here touches the repo's go.mod: the native build,
# `go build ./...`, `make test`, and CI are unaffected because they never set
# GOWORK and never see wasm/.deps.
#
# The real fix for the two dtctl gaps is to upstream `//go:build js` variants
# into dtctl (Dynatrace-owned). See docs/design/WASM_CHROME_EXTENSION.md.
set -euo pipefail

here="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
repo="$(cd "$here/.." && pwd)"
patches="$here/patches"
deps="$here/.deps"
out="$here/ui"
mkdir -p "$out"

gover="$(cd "$repo" && go list -m -f '{{.GoVersion}}' 2>/dev/null || echo 1.26)"

# copy_mod <module-path> <dest-name>
copy_mod() {
  local mod="$1" dest="$deps/$2" src
  src="$(cd "$repo" && go list -m -f '{{.Dir}}' "$mod")"
  rm -rf "$dest"
  mkdir -p "$dest"
  cp -R "$src/." "$dest/"
  chmod -R u+w "$dest"
}

echo "==> copying patched module sources into $deps"
copy_mod github.com/charmbracelet/bubbletea bubbletea
copy_mod github.com/atotto/clipboard clipboard
copy_mod github.com/dynatrace-oss/dtctl dtctl
copy_mod github.com/dynatrace-oss/dtctl/sdk dtctl-sdk

echo "==> applying js/wasm patches"
cp "$patches/bubbletea_tty_wasm.go.txt" "$deps/bubbletea/tty_wasm.go"
cp "$patches/clipboard_js.go.txt" "$deps/clipboard/clipboard_js.go"
# Retag the unix refresh-lock off js, and add the js no-op stub beside it.
sed -i.bak 's|^//go:build !windows$|//go:build !windows \&\& !js|' \
  "$deps/dtctl-sdk/session/refresh_lock_unix.go"
rm -f "$deps/dtctl-sdk/session/refresh_lock_unix.go.bak"
cp "$patches/dtctl_sdk_refresh_lock_js.go.txt" "$deps/dtctl-sdk/session/refresh_lock_js.go"
cp "$patches/dtctl_output_live_js.go.txt" "$deps/dtctl/pkg/output/live_js.go"
# Same retag for the usage-ledger lock (dtctl >= v0.43).
sed -i.bak 's|^//go:build !windows$|//go:build !windows \&\& !js|' \
  "$deps/dtctl/pkg/usage/lock_unix.go"
rm -f "$deps/dtctl/pkg/usage/lock_unix.go.bak"
cp "$patches/dtctl_usage_lock_js.go.txt" "$deps/dtctl/pkg/usage/lock_js.go"

echo "==> writing scoped workspace $here/go.work.wasm"
cat > "$here/go.work.wasm" <<WORK
go $gover

use ..

replace github.com/charmbracelet/bubbletea => ./.deps/bubbletea
replace github.com/atotto/clipboard => ./.deps/clipboard
replace github.com/dynatrace-oss/dtctl => ./.deps/dtctl
replace github.com/dynatrace-oss/dtctl/sdk => ./.deps/dtctl-sdk
WORK

echo "==> building dtui.wasm (GOOS=js GOARCH=wasm)"
(
  cd "$repo"
  GOWORK="$here/go.work.wasm" GOOS=js GOARCH=wasm go build -ldflags="-s -w" \
    -o "$out/dtui.wasm" github.com/dynatrace-oss/dtui/cmd/web
)
raw_size=$(wc -c < "$out/dtui.wasm")
echo "    build: $raw_size bytes ($((raw_size / 1024 / 1024)) MB)"

# Optional Binaryen pass — shrinks ~30-40%. A Chrome extension has no hard asset
# cap, but smaller is a faster first load. Skipped if wasm-opt is unavailable.
if command -v wasm-opt >/dev/null 2>&1; then
  echo "==> optimizing with wasm-opt -Oz"
  wasm-opt -Oz --enable-bulk-memory --enable-sign-ext \
    --enable-mutable-globals --enable-nontrapping-float-to-int \
    "$out/dtui.wasm" -o "$out/dtui.wasm"
  echo "    optimized: $(wc -c < "$out/dtui.wasm") bytes"
else
  echo "==> wasm-opt not found; skipping size optimization (install binaryen to shrink)"
fi

echo "==> copying wasm_exec.js from $(go env GOROOT)"
cp -f "$(go env GOROOT)/lib/wasm/wasm_exec.js" "$out/wasm_exec.js"

# The extension loads dtui.wasm from its own directory (chrome.runtime.getURL),
# so sync the freshly built artifacts there too. Both are gitignored. Skipping
# this is how a stale binary sneaks in — e.g. missing a newly wired feature.
ext="$repo/extension"
echo "==> syncing artifacts into $ext"
cp "$out/dtui.wasm" "$ext/dtui.wasm"
cp -f "$out/wasm_exec.js" "$ext/wasm_exec.js"

echo "==> done: $out/dtui.wasm + $out/wasm_exec.js (also copied into extension/)"
