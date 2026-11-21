#!/usr/bin/env bash
# The macOS launch budget (ci.yml's macos job; flake-check's launch-budget
# job): Clack.app made and signed as release.yml makes it (a perfharness
# build, which accepts the loopback fakeslackd), launched through
# LaunchServices as the Dock launches it, with Karim-like plugins, once
# cold and then five times warm (.github/perf/launch.py).
#
# Karim (2026-11-21): "Make launching the app take less than a Mac app
# bounce." macOS bounces the Dock icon from the launch until the app
# finishes launching; one bounce on the runner is 700 ms (bounce.sh, the
# launch-speed-mac job). The budget:
#   - the fastest warm launch shows the client within one bounce of the
#     launch call (LaunchServices' exec included)
#   - the median warm launch shows the boot placeholder at most one frame
#     (16 ms) before the client: a warm launch's first frame is the client
#     from the cache, as Slack renders from its persisted store
#
# usage: launch-budget.sh <workdir>
set -eu
w=$1
mkdir -p "$w"
app="$w/Clack.app"
mkdir -p "$app/Contents/MacOS" "$app/Contents/Resources"
CGO_ENABLED=1 go build -tags perfharness -trimpath -ldflags="-s -w" -o "$app/Contents/MacOS/clack" ./cmd/clack 2> "$w/build.log" || { cat "$w/build.log"; exit 1; }
cp assets/icon/clack.icns "$app/Contents/Resources/clack.icns"
sed "s/@VERSION@/0.0.1/g" packaging/macos/Info.plist > "$app/Contents/Info.plist"
printf 'APPL????' > "$app/Contents/PkgInfo"
codesign --force --deep --sign - --entitlements packaging/macos/Clack.entitlements "$app"
go build -o "$w/fakeslackd" ./internal/fakeslack/fakeslackd
.github/perf/launch-library.sh "$w/library" > /dev/null
python3 -I .github/perf/launch.py --app "$app" --fake "$w/fakeslackd" --out "$w/out" --library "$w/library" \
  --warm 5 --bounce-ms "${LAUNCH_BOUNCE_MS:-700}" --placeholder-ms "${LAUNCH_PLACEHOLDER_MS:-16}" --label "macOS launch budget"
