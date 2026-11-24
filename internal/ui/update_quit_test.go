package ui

import "testing"

// A downloaded update the user didn't restart into installs as Clack
// quits (Slack desktop's is applied on quit), and only then: Karim's Mac
// ran alpha.139 for 19 releases because each launch downloaded the newest
// and nothing ever installed it without "Restart to Apply Update". Cheap:
// no window, no session.
func TestInstallUpdateOnQuit(t *testing.T) {
	var got []string
	a := &App{}
	a.opts.InstallUpdateOnQuit = func(u UpdateInfo) error { got = append(got, u.Tag); return nil }
	a.update.info = UpdateInfo{Tag: "v0.1.0-alpha.157"}
	for _, ph := range []updatePhase{updNone, updAvailable, updDownloading, updInstalling, updFailed, updManual} {
		a.update.phase = ph
		if tag, _ := a.InstallUpdateOnQuit(); tag != "" || len(got) != 0 {
			t.Fatalf("phase %v: installed %q (%v) on quit", ph, tag, got)
		}
	}
	a.update.phase = updReady
	if tag, err := a.InstallUpdateOnQuit(); tag != "v0.1.0-alpha.157" || err != nil || len(got) != 1 || got[0] != tag {
		t.Errorf("a ready update on quit: %q, %v (installed %v)", tag, err, got)
	}
	a.opts.InstallUpdateOnQuit = nil // a build without the updater
	if tag, _ := a.InstallUpdateOnQuit(); tag != "" {
		t.Errorf("no installer: %q", tag)
	}
}
