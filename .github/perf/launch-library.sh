#!/usr/bin/env bash
# The plugins launch.py turns on (perf's startup.sh's set: Slick's ChatTabs
# and five Taut plugins), from the published Library's release-download
# URLs: plain github.com redirects, no REST API calls.
#
# usage: launch-library.sh <dir>
set -u
out=$1
base="https://github.com/${GITHUB_REPOSITORY:-some-du6e/clack}/releases/download/plugins"
mkdir -p "$out"
for f in index.json taut-HumanCount.js taut-LastSeen.js taut-WhoReacted.js taut-NoTrack.js taut-ShutUpSlackbot.js slick-ChatTabs.js slick-ChatTabs.main.js; do
  if ! curl -fsSL --retry 5 --retry-delay 10 --retry-all-errors -o "$out/$f" "$base/$f"; then
    rm -f "$out/$f"
    echo "::warning::no $f in the plugin Library"
  fi
done
ls -la "$out"
