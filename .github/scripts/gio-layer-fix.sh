#!/usr/bin/env bash
# The Gio drawLayers fix (third_party/gioui.org/CLACK_PATCHES.md): its
# regression tests pass on the patched copy and panic on stock v0.10.3.
# Usage: gio-layer-fix.sh [runner...], e.g. `xvfb-run -a` on Linux.
# Needs a GPU for pixeltest (CLACK_PIXEL_TESTS=1 makes a missing one fatal).
set -euo pipefail
tests='^(TestOpaqueFillAfterOpacityLayer|TestDimmedViewUnderOpaqueFill)$'
pkgs=(./internal/ui/theme/pixeltest ./internal/ui/richview)

"$@" go test -count=1 -v -run "$tests" "${pkgs[@]}"

stock=$(mktemp -d)
sed '/^replace gioui.org /d' go.mod > "$stock/go.mod"
cp go.sum "$stock/go.sum"
go get -modfile="$stock/go.mod" gioui.org@v0.10.3
for p in "${pkgs[@]}"; do
	# stockgio: theme's patch-12 calls (letter spacing) become no-ops
	if "$@" go test -modfile="$stock/go.mod" -tags stockgio -count=1 -v -run "$tests" "$p" > "$stock/out" 2>&1; then
		cat "$stock/out"
		echo "::error::$p: the regression test passed on stock Gio v0.10.3"
		exit 1
	fi
	if ! grep -q 'panic: runtime error: index out of range' "$stock/out" || ! grep -q 'gpu.(\*renderer).drawLayers' "$stock/out"; then
		cat "$stock/out"
		echo "::error::$p failed on stock Gio v0.10.3, but not with the drawLayers panic"
		exit 1
	fi
	echo "$p on stock Gio v0.10.3: $(grep -m1 'panic: runtime error' "$stock/out")"
done
