#!/usr/bin/env bash
# prepush.sh [BASE]: before a push, what ci's linux-build would fail on
# (docs/CI-SPEED.md, "Before a push"): gofmt -l on the Go files changed
# since BASE (default: the merge base with origin/ui) plus the working
# tree's, then go vet on each package they're in, with the GOOS their
# _windows/_darwin files need. Exits non-zero on anything found.
#
# On swiss go vet goes through the go gate (2 at a time, nice 19): a
# package like internal/ui takes minutes there. vet type-checks the tests
# too, so a test that doesn't compile shows here. The 2026-11-21 audit
# (93 failed agent-branch runs): it would have caught the 3 build and vet
# failures (a method declared twice, a call short of an argument, vet's
# unreachable code); gofmt had none.
set -uo pipefail
cd "$(git rev-parse --show-toplevel)" || exit 2
base=${1:-$(git merge-base HEAD origin/ui 2>/dev/null)}
if [ -z "$base" ]; then
	echo "prepush: no base (pass one, or fetch origin/ui)" >&2
	exit 2
fi
mapfile -t files < <({
	git diff --name-only --diff-filter=ACMR "$base" HEAD
	git diff --name-only --diff-filter=ACMR HEAD
	git ls-files --others --exclude-standard
} | grep -E '\.go$' | grep -v '/testdata/' | sort -u)
if [ ${#files[@]} -eq 0 ]; then
	echo "prepush: no Go files changed since $(git rev-parse --short "$base")"
	exit 0
fi
rc=0
present=()
for f in "${files[@]}"; do
	[ -f "$f" ] && present+=("$f")
done
if [ ${#present[@]} -gt 0 ]; then
	bad=$(gofmt -l "${present[@]}")
	if [ -n "$bad" ]; then
		echo "prepush: gofmt -l lists (run gofmt -w on them):"
		echo "$bad" | sed 's/^/  /'
		rc=1
	fi
fi
# packages by GOOS, as import paths of the main module (third_party/*
# are modules of their own, but the main module's replace is what builds
# them: vetting one alone took its own go.mod's other typesetting)
declare -A linux=() windows=() darwin=()
modpath() { # dir -> import path, through the module the dir is in
	local d=$1 m
	for m in third_party/gioui.org third_party/typesetting third_party/rnnoise-go; do
		case $d/ in "$m"/*)
			local mp
			mp=$(awk '/^module /{print $2; exit}' "$m/go.mod")
			local rel=${d#"$m"}
			echo "$mp${rel}"
			return
			;;
		esac
	done
	echo "./$d"
}
for f in "${files[@]}"; do
	d=$(dirname "$f")
	[ -d "$d" ] || continue
	p=$(modpath "$d")
	case $f in
	*_windows.go | *_windows_test.go) windows[$p]=1 ;;
	*_darwin.go | *_darwin_test.go) darwin[$p]=1 ;;
	*) linux[$p]=1 ;;
	esac
done
# a package with no files for GOOS (build constraints): nothing to vet there
hasfiles() { # GOOS pkg
	local n
	n=$(GOOS=$1 go list -e -f '{{len .GoFiles}}{{len .CgoFiles}}{{len .TestGoFiles}}{{len .XTestGoFiles}}' "$2" 2>/dev/null)
	[ -n "$n" ] && [ "$n" != 0000 ]
}
vet() { # vet GOOS warn ENV... -- pkgs...
	local goos=$1 warn=$2
	shift 2
	local envs=()
	while [ "$1" != -- ]; do
		envs+=("$1")
		shift
	done
	shift
	local pkgs=() p
	for p in "$@"; do
		hasfiles "$goos" "$p" && pkgs+=("$p")
	done
	[ ${#pkgs[@]} -gt 0 ] || return 0
	echo "prepush: go vet ($goos): ${pkgs[*]}"
	if ! env GOOS="$goos" "${envs[@]}" go vet "${pkgs[@]}"; then
		if [ "$warn" = warn ]; then
			echo "prepush: the $goos vet above needs a $goos cgo toolchain this machine lacks: a warning only (CI's macos jobs build it)"
		else
			rc=1
		fi
	fi
}
[ ${#linux[@]} -gt 0 ] && vet linux fail CGO_ENABLED=1 -- "${!linux[@]}"
if [ ${#windows[@]} -gt 0 ]; then
	if command -v x86_64-w64-mingw32-gcc >/dev/null; then
		vet windows fail GOARCH=amd64 CGO_ENABLED=1 CC=x86_64-w64-mingw32-gcc -- "${!windows[@]}"
	else
		echo "prepush: no x86_64-w64-mingw32-gcc: the windows vet skips cgo files"
		vet windows fail GOARCH=amd64 CGO_ENABLED=0 -- "${!windows[@]}"
	fi
fi
# no darwin cgo toolchain here: Gio's packages need cgo on darwin, so the
# darwin vet only warns
[ ${#darwin[@]} -gt 0 ] && vet darwin warn GOARCH=arm64 CGO_ENABLED=0 -- "${!darwin[@]}"
if [ $rc -eq 0 ]; then
	echo "prepush: gofmt and go vet clean on ${#files[@]} changed Go files"
fi
exit $rc
