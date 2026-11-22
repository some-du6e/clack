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
# packages by module (third_party/* are modules of their own) and GOOS
declare -A linux=() windows=() darwin=()
for f in "${files[@]}"; do
	d=$(dirname "$f")
	[ -d "$d" ] || continue
	mod=.
	for m in third_party/gioui.org third_party/typesetting third_party/rnnoise-go; do
		case $d/ in "$m"/*) mod=$m ;; esac
	done
	rel=${d#"$mod"}
	rel=./${rel#/}
	[ "$rel" = ./ ] && rel=.
	key="$mod|$rel"
	case $f in
	*_windows.go | *_windows_test.go) windows[$key]=1 ;;
	*_darwin.go | *_darwin_test.go) darwin[$key]=1 ;;
	*) linux[$key]=1 ;;
	esac
done
vet() { # vet GOOS ENV... -- keys...
	local goos=$1
	shift
	local envs=()
	while [ "$1" != -- ]; do
		envs+=("$1")
		shift
	done
	shift
	declare -A bymod=()
	local k
	for k in "$@"; do
		bymod[${k%%|*}]+=" ${k#*|}"
	done
	local m
	for m in "${!bymod[@]}"; do
		echo "prepush: go vet ($goos) in $m:${bymod[$m]}"
		# shellcheck disable=SC2086
		if ! (cd "$m" && env GOOS="$goos" "${envs[@]}" go vet ${bymod[$m]}); then
			rc=1
		fi
	done
}
[ ${#linux[@]} -gt 0 ] && vet linux CGO_ENABLED=1 -- "${!linux[@]}"
if [ ${#windows[@]} -gt 0 ]; then
	if command -v x86_64-w64-mingw32-gcc >/dev/null; then
		vet windows GOARCH=amd64 CGO_ENABLED=1 CC=x86_64-w64-mingw32-gcc -- "${!windows[@]}"
	else
		echo "prepush: no x86_64-w64-mingw32-gcc: the windows vet skips cgo files"
		vet windows GOARCH=amd64 CGO_ENABLED=0 -- "${!windows[@]}"
	fi
fi
# no darwin cgo toolchain here: the darwin vet skips cgo files (CI's macos jobs build them)
[ ${#darwin[@]} -gt 0 ] && vet darwin GOARCH=arm64 CGO_ENABLED=0 -- "${!darwin[@]}"
if [ $rc -eq 0 ]; then
	echo "prepush: gofmt and go vet clean on ${#files[@]} changed Go files"
fi
exit $rc
