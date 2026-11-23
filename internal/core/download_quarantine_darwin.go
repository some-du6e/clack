package core

import (
	"crypto/rand"
	"fmt"
	"os/exec"
	"strconv"
	"strings"
	"time"
)

// quarantine is the desktop app's quarantine of a completed download on
// macOS (main.bundle.cjs f1): xattr -w com.apple.quarantine
// "0002;<seconds in hex>;<app name>;<UUID>", so Gatekeeper asks before
// an app or script from Slack first runs. A failure is only logged by
// Slack; here it's dropped.
func quarantine(path string) {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		return
	}
	b[6] = b[6]&0x0f | 0x40
	b[8] = b[8]&0x3f | 0x80
	uuid := strings.ToUpper(fmt.Sprintf("%x-%x-%x-%x-%x", b[0:4], b[4:6], b[6:8], b[8:10], b[10:16]))
	ts := strconv.FormatInt(time.Now().Unix(), 16)
	cmd := exec.Command("/usr/bin/xattr", "-w", "com.apple.quarantine", "0002;"+ts+";Clack;"+uuid, path)
	cmd.Env = []string{}
	_ = cmd.Run()
}
