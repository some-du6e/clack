#!/usr/bin/env bash
# How long one bounce of the Dock's launch animation lasts (macOS), which
# is what a launch should beat (Karim, 2026-11-21: "less than a Mac app
# bounce"). macOS bounces an app's Dock icon from the launch until the app
# finishes launching, so Bouncer.app takes BOUNCE_SECS (6) in
# applicationWillFinishLaunching while screencapture films the screen; then
# bounce.swift reads the film frame by frame: the icon's height above its
# place in the Dock, each bounce's start, peak and landing.
#
# usage: bounce.sh <outdir>   (screencapture needs Screen Recording, which
# GitHub's macOS runners give it; a test binary gets the desktop alone)
set -u
out=$1
secs=${BOUNCE_SECS:-6}
here=$(cd "$(dirname "$0")" && pwd)
mkdir -p "$out"
d=$(mktemp -d)
app="$d/Bouncer.app"
mkdir -p "$app/Contents/MacOS"
cat > "$app/Contents/Info.plist" <<EOF
<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
	<key>CFBundleExecutable</key><string>bouncer</string>
	<key>CFBundleIdentifier</key><string>io.github.some-du6e.clack.bouncer</string>
	<key>CFBundleName</key><string>Bouncer</string>
	<key>CFBundlePackageType</key><string>APPL</string>
	<key>CFBundleShortVersionString</key><string>1.0</string>
	<key>CFBundleVersion</key><string>1</string>
	<key>LSMinimumSystemVersion</key><string>11.0</string>
	<key>NSPrincipalClass</key><string>NSApplication</string>
</dict>
</plist>
EOF
cat > "$d/bouncer.m" <<EOF
#import <Cocoa/Cocoa.h>
#include <unistd.h>
@interface D : NSObject <NSApplicationDelegate>
@end
@implementation D
- (void)applicationWillFinishLaunching:(NSNotification *)n {
	sleep(${secs}); // the Dock bounces until didFinishLaunching
}
- (void)applicationDidFinishLaunching:(NSNotification *)n {
	[NSApp terminate:nil];
}
@end
int main(int argc, const char **argv) {
	@autoreleasepool {
		D *d = [D new];
		[NSApplication sharedApplication].delegate = d;
		[NSApp run];
	}
	return 0;
}
EOF
clang -fobjc-arc -framework Cocoa -o "$app/Contents/MacOS/bouncer" "$d/bouncer.m" || exit 1
codesign --force --sign - "$app" || exit 1
osascript -e 'tell application "System Events" to get the autohide of the dock preferences' 2>&1 | sed 's/^/dock autohide: /'
defaults read com.apple.dock 2>/dev/null | grep -E '"?(autohide|launchanim|tilesize|orientation)"? =' | sed 's/^/dock: /'
screencapture -x "$out/before.png"
screencapture -v -V $((secs + 3)) -x "$out/bounce.mov" > "$out/screencapture.log" 2>&1 &
rec=$!
sleep 1.5
python3 -c 'import time; print("open at", time.time())'
open "$app"
sleep 2
screencapture -x "$out/bouncing.png"
wait $rec
echo "screencapture exited $? ($(ls -l "$out/bounce.mov" 2>/dev/null | awk '{print $5}') bytes)"
cat "$out/screencapture.log"
pkill -f "[B]ouncer.app/Contents/MacOS/bouncer" || true
swift "$here/bounce.swift" "$out/bounce.mov" "$out"
