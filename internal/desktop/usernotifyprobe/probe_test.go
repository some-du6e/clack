package usernotifyprobe

import (
	"flag"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

var (
	variant = flag.String("probe", "", "run TestUserNotifyProbe as this variant (ci.yml's macos job, from a test Clack.app)")
	steps   = flag.String("probe.steps", "prompt,post", "what to do, in order: prompt (ask for alert, sound and badge), provisional (the same plus provisional), post; none only reports the settings")
	outDir  = flag.String("probe.out", "", "where the screenshots go")
	answer  = flag.Duration("probe.wait", 20*time.Second, "how long to wait for each answer")
)

// TestUserNotifyProbe logs what UNUserNotificationCenter says to this
// process, one "PROBE <variant> <what>: <answer>" line each: the bundle,
// session and signature macOS sees, whether the center exists, the
// settings before and after each step, and each step's answer. A prompt
// nobody answers leaves the request without an answer, so a screenshot is
// taken while it waits. It reports rather than asserts: the answers are
// the finding.
func TestUserNotifyProbe(t *testing.T) {
	if *variant == "" {
		t.Skip("run with -probe=<variant> from a test Clack.app (ci.yml's macos job)")
	}
	secs := int(answer.Seconds())
	report := func(what, v string) { t.Logf("PROBE %s %s: %s", *variant, what, v) }
	report("process", bundle()+"; build "+build)
	if why := centerErr(); why != "" {
		report("center", why)
		return
	}
	report("center", "ok")
	report("settings", settings(secs))
	if *steps == "none" {
		return
	}
	for _, step := range strings.Split(*steps, ",") {
		switch step {
		case "prompt", "provisional":
			opts := OptAlert | OptSound | OptBadge
			if step == "provisional" {
				opts |= OptProvisional
			}
			done := make(chan string, 1)
			go func() { done <- request(opts, secs) }()
			select {
			case v := <-done:
				report(step, v)
			case <-time.After(6 * time.Second):
				shot(t, step+"-waiting")
				report(step, <-done)
			}
		case "post":
			id := "probe-" + *variant
			report(step, post(id, "Clack probe ("+*variant+")", "#probe", "Can an ad-hoc signed Clack.app notify?", "probe-thread", true, secs))
			time.Sleep(3 * time.Second)
			shot(t, step)
			report("delivered", delivered(secs))
		default:
			t.Fatalf("unknown step %q", step)
		}
		report("settings after "+step, settings(secs))
	}
}

// shot saves the screen, for a prompt or a banner the log can't show.
func shot(t *testing.T, name string) {
	if *outDir == "" {
		return
	}
	p := filepath.Join(*outDir, *variant+"-"+name+".png")
	if out, err := exec.Command("screencapture", "-x", p).CombinedOutput(); err != nil {
		t.Logf("screencapture %s: %v %s", p, err, out)
	}
}
