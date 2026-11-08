#!/usr/bin/env python3
"""Adds the perf harness hooks to a baseline tree (perf.yml's base leg).

The base commit predates the harness: its main has no profiler hook, its
client can't route the edge API to a loopback fake, and ui.App needs a
window for updateTitle. Each patch is the minimal equivalent of what HEAD
has; a patch that no longer applies fails loudly.
"""
import sys

def patch(path, old, new):
    s = open(path).read()
    if old not in s:
        sys.exit(f"patch-base: {path}: anchor not found")
    open(path, "w").write(s.replace(old, new, 1))

patch("cmd/clack/main.go", "func main() {\n", "func main() {\n\tstartProfiler()\n")
patch("internal/slack/client.go",
      """	return &Client{HTTP: &http.Client{Transport: tr, Timeout: 60 * time.Second}, Creds: c, base: base, ent: ent,
		sem: make(chan struct{}, MaxConcurrent), learned: map[string]bool{}}
}""",
      """	cl := &Client{HTTP: &http.Client{Transport: tr, Timeout: 60 * time.Second}, Creds: c, base: base, ent: ent,
		sem: make(chan struct{}, MaxConcurrent), learned: map[string]bool{}}
	if p, err := url.Parse(base); err == nil {
		if ip := net.ParseIP(p.Hostname()); ip != nil && ip.IsLoopback() {
			cl.EdgeURL = base + "edge/"
		}
	}
	return cl
}""")
patch("internal/ui/app.go", "\t\ta.win.Option(app.Title(t))\n", "\t\tif a.win != nil {\n\t\t\ta.win.Option(app.Title(t))\n\t\t}\n")
# HEAD's harness starts the app with NoRealtime (the sign-out fix's opt-in
# realtime socket; no first-run banner shifting the layout). The baseline
# has no such option: give it the field, unused.
patch("internal/ui/app.go", "type Options struct {\n", "type Options struct {\n\tNoRealtime bool // perf harness: HEAD's option, unused in the baseline\n")
print("patch-base: ok")
