#!/usr/bin/env bash
# The macOS launch budget (ci.yml's macos job; flake-check's launch-budget
# job): Clack.app made and signed as release.yml makes it (a perfharness
# build, which accepts the loopback fakeslackd), launched through
# LaunchServices as the Dock launches it, once cold and then seven times
# warm (.github/perf/launch.py), with a Slick theme and user CSS seeded and
# plugins that show nothing of their own (NoTrack, ShutUpSlackbot: a warm
# launch never waits for them; flake-check's launch-speed-* jobs measure
# Karim's set, whose patches and CSS the reveal may wait for), against a
# fakeslackd whose account caches what Karim's does: meta.json with 6.6 MB
# of packed custom emoji and 2,000 user groups (launch.py --emoji).
#
# Karim (2026-11-21): "Make launching the app take less than a Mac app
# bounce." macOS bounces the Dock icon from the launch until the app
# finishes launching; one bounce on the runner is 700 ms (bounce.sh, the
# launch-speed-mac job). The checks:
#   - every launch's first real frame is in the theme (window, sidebar,
#     pane), with no repaint into it after the client showed
#   - the median warm launch shows the boot placeholder at most one frame
#     (16 ms) before the client: its first frame is the client from the
#     cache, as Slack renders from its persisted store
#   - the fastest warm launch shows the client within one bounce of the
#     launch call (LaunchServices' exec included), at the speed of the
#     fastest runner seen: the runners' speed varies 3-4x from run to run
#     (two VMs a host), macOS's own part of a launch as much as Clack's, so
#     a runner whose exec + AppKit finishLaunching takes 3x the fastest
#     runner's 71 ms gets 3x the bounce (launch.py's runner_speed). Over it,
#     seven more warm launches run once (the fastest of all counts).
#     LAUNCH_SOFT_BOUNCE=1 (ci's macos job) makes it a warning: a slow
#     runner must not turn an unrelated branch red; flake-check's
#     launch-budget job keeps it a failure.
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
soft=()
[ "${LAUNCH_SOFT_BOUNCE:-}" = 1 ] && soft=(--soft-bounce)
python3 -I .github/perf/launch.py --app "$app" --fake "$w/fakeslackd" --out "$w/out" --library "$w/library" \
  --plugins quiet --theme-check --warm 7 --bounce-ms "${LAUNCH_BOUNCE_MS:-700}" --bounce-retry --placeholder-ms "${LAUNCH_PLACEHOLDER_MS:-16}" \
  ${soft[@]+"${soft[@]}"} --label "macOS launch budget"
