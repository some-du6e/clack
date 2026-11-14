//go:build windows

package desktop

import (
	"errors"
	"os"
	"sync"
	"testing"
	"time"
	"unsafe"

	"golang.org/x/sys/windows"
)

// Focus Assist on the runner (CLACK_WINDOWS_TRAY_TESTS, ci's
// windows-shell job): the WNF state reads, and the whole check answers
// well inside Slack's 2 s. The WNF state and IQuietHoursSettings needn't
// agree: Windows 11's Do not disturb (on by default on the runner) sets
// the state to priority only while the settings' profile stays
// Unrestricted, and the state is what Windows applies (and Slack reads
// first). With
// CLACK_WINDOWS_FOCUS_ASSIST_TESTS (it changes the runner user's Do not
// disturb) each profile set through IQuietHoursSettings shows in the WNF
// state, FocusAssist and FocusAssistActive (a9), and Clack among the
// priority-only profile's apps counts as priority. All of it is put back.
//
// On Windows Server 2025 a Do not disturb switch can crash the service
// behind IQuietHoursSettings (WpnUserService, restarted after 1 to 8 s):
// the next call fails with an RPC error, and a switch or an allowed app
// made just before the crash can be lost without one (isPriority read 0
// after an AddAllowedApp that had succeeded, ci 2026-11-14). So every
// change is made again until the state reads back, the checks that ask
// the service wait for its answer, and the service staying gone (an RPC
// error past 30 s) skips the test as the runner's: Clack's reads are
// what's tested, not the service. TestFocusAssistActiveCachesLikeSlack
// covers the cache and a9 without the OS.

const quietHoursUnrestricted = "Microsoft.QuietHoursProfile.Unrestricted"

const (
	vtQHSettingsPutUserSelectedProfile = 4  // IQuietHoursSettings
	vtQHProfileAddAllowedApp           = 12 // IQuietHoursProfile
	vtQHProfileRemoveAllowedApp        = 13
)

func resetFocusAssistCache() {
	focusAssist.Lock()
	focusAssist.at = time.Time{}
	focusAssist.Unlock()
}

func TestFocusAssistRead(t *testing.T) {
	trayShellTests(t)
	start := time.Now()
	wnf, com := focusAssistWNF(), focusAssistCOM()
	mode, prio := FocusAssist(), FocusAssistPriority(AppUserModelID)
	resetFocusAssistCache()
	active := FocusAssistActive()
	took := time.Since(start)
	t.Logf("WNF %d, COM %d: FocusAssist %d, isPriority(%s) %d, active %v (%v)", wnf, com, mode, AppUserModelID, prio, active, took)
	if mode < FocusAssistNotSupported || mode > FocusAssistAlarmsOnly {
		t.Errorf("FocusAssist %d is no mode", mode)
	}
	if mode != wnf && wnf != FocusAssistFailed {
		t.Errorf("FocusAssist %d, the WNF state %d: the state comes first", mode, wnf)
	}
	if wnf == FocusAssistFailed {
		t.Errorf("NtQueryWnfStateData(WNF_SHEL_QUIETHOURS_ACTIVE_PROFILE_CHANGED) failed")
	}
	if took > 2*time.Second {
		t.Errorf("the check took %v", took)
	}
	if active != focusAssistOn(mode, mode == FocusAssistPriorityOnly && prio == 1) {
		t.Errorf("FocusAssistActive %v for mode %d, priority %d", active, mode, prio)
	}
}

// quietHours runs f with IQuietHoursSettings on a COM thread.
func quietHours(f func(s *rtObject) error) error {
	return comThread(func() error {
		s, err := newQuietHoursSettings()
		if err != nil {
			return err
		}
		defer s.release()
		return f(s)
	})
}

// rpcGone reports whether err is the COM server gone (crashed, or not
// back yet): RPC_S_CALL_FAILED, RPC_S_SERVER_UNAVAILABLE,
// RPC_E_DISCONNECTED, CO_E_SERVER_EXEC_FAILURE.
func rpcGone(err error) bool {
	var hr HResultError
	if !errors.As(err, &hr) {
		return false
	}
	switch uint32(hr) {
	case 0x800706BE, 0x800706BA, 0x80010108, 0x80080005:
		return true
	}
	return false
}

// untilServer retries f while the QuietHours server is gone, up to 30 s.
func untilServer(t *testing.T, what string, f func() error) error {
	t.Helper()
	deadline := time.Now().Add(30 * time.Second)
	for {
		err := f()
		if err == nil || !rpcGone(err) || time.Now().After(deadline) {
			return err
		}
		t.Logf("%s: %v; the service is restarting, again", what, err)
		time.Sleep(time.Second)
	}
}

// mustServer is untilServer for a change the test needs: an error fails
// it, the service still gone after 30 s skips it (the runner's crash).
func mustServer(t *testing.T, what string, f func() error) {
	t.Helper()
	err := untilServer(t, what, f)
	switch {
	case err == nil:
	case rpcGone(err):
		t.Skipf("%s: %v: the QuietHours service didn't come back in 30 s (the runner's, not Clack's)", what, err)
	default:
		t.Fatalf("%s: %v", what, err)
	}
}

// switchProfile sets the profile and waits for the WNF state to say
// mode, setting it again when a crash lost the switch.
func switchProfile(t *testing.T, id string, mode int) {
	t.Helper()
	for try := 0; try < 4; try++ {
		mustServer(t, "put_UserSelectedProfile("+id+")", func() error { return setQuietHoursProfile(id) })
		deadline := time.Now().Add(5 * time.Second)
		for focusAssistWNF() != mode && time.Now().Before(deadline) {
			time.Sleep(50 * time.Millisecond)
		}
		if focusAssistWNF() == mode {
			return
		}
		t.Logf("%s: the WNF state is still %d (want %d); setting it again", id, focusAssistWNF(), mode)
	}
}

func setQuietHoursProfile(id string) error {
	return quietHours(func(s *rtObject) error {
		p, err := windows.UTF16PtrFromString(id)
		if err != nil {
			return err
		}
		if hr := s.call(vtQHSettingsPutUserSelectedProfile, uintptr(unsafe.Pointer(p))); failed(hr) {
			return HResultError(hr)
		}
		return nil
	})
}

func allowApp(aumid string, add bool) error {
	return quietHours(func(s *rtObject) error {
		name, _ := windows.UTF16PtrFromString(quietHoursPriorityOnly)
		var prof *rtObject
		if hr := s.call(vtQHSettingsGetProfile, uintptr(unsafe.Pointer(name)), uintptr(unsafe.Pointer(&prof))); failed(hr) || prof == nil {
			return HResultError(hr)
		}
		defer prof.release()
		id, _ := windows.UTF16PtrFromString(aumid)
		slot := vtQHProfileRemoveAllowedApp
		if add {
			slot = vtQHProfileAddAllowedApp
		}
		if hr := prof.call(slot, uintptr(unsafe.Pointer(id))); failed(hr) {
			return HResultError(hr)
		}
		return nil
	})
}

// allowClack adds Clack to the priority-only profile's apps (or removes
// it) and waits for isPriority to say so, making the change again when
// it reads back the old answer: a crash just after the call loses it.
func allowClack(t *testing.T, allow bool) {
	t.Helper()
	what, want := "RemoveAllowedApp", 0
	if allow {
		what, want = "AddAllowedApp", 1
	}
	got := FocusAssistFailed
	for try := 0; try < 4; try++ {
		mustServer(t, what, func() error { return allowApp(AppUserModelID, allow) })
		// FocusAssistFailed while the service restarts
		for deadline := time.Now().Add(10 * time.Second); ; time.Sleep(250 * time.Millisecond) {
			if got = FocusAssistPriority(AppUserModelID); got == want || time.Now().After(deadline) {
				break
			}
		}
		if got == want {
			return
		}
		t.Logf("%s: isPriority is %d (want %d); again", what, got, want)
	}
	t.Errorf("%s: isPriority is %d, want %d", what, got, want)
}

// untilActive waits (30 s) for FocusAssistActive, uncached, to be want:
// its isPriority half reads false while the service restarts.
func untilActive(t *testing.T, what string, want bool) {
	t.Helper()
	var got bool
	for deadline := time.Now().Add(30 * time.Second); ; time.Sleep(500 * time.Millisecond) {
		resetFocusAssistCache()
		if got = FocusAssistActive(); got == want || time.Now().After(deadline) {
			break
		}
	}
	if got != want {
		t.Errorf("%s: FocusAssistActive %v, want %v", what, got, want)
	}
}

func TestFocusAssistModes(t *testing.T) {
	trayShellTests(t)
	if os.Getenv("CLACK_WINDOWS_FOCUS_ASSIST_TESTS") == "" {
		t.Skip("CLACK_WINDOWS_FOCUS_ASSIST_TESTS not set (it changes Do not disturb)")
	}
	var orig string
	if err := quietHours(func(s *rtObject) error {
		var err error
		orig, err = userSelectedProfile(s)
		return err
	}); err != nil {
		t.Skipf("no IQuietHoursSettings here: %v", err)
	}
	t.Logf("the runner's profile: %s", orig)
	t.Cleanup(func() {
		switch err := untilServer(t, "putting "+orig+" back", func() error { return setQuietHoursProfile(orig) }); {
		case err == nil:
		case rpcGone(err): // the runner's service, still down: nothing after this step uses it
			t.Logf("putting %s back: %v (the service is still restarting)", orig, err)
		default:
			t.Errorf("putting %s back: %v", orig, err)
		}
		_ = untilServer(t, "RemoveAllowedApp", func() error { return allowApp(AppUserModelID, false) })
		resetFocusAssistCache()
	})
	allowClack(t, false) // as on a new runner
	for _, c := range []struct {
		profile string
		mode    int
	}{
		{quietHoursPriorityOnly, FocusAssistPriorityOnly},
		{quietHoursAlarmsOnly, FocusAssistAlarmsOnly},
		{quietHoursUnrestricted, FocusAssistOff},
	} {
		switchProfile(t, c.profile, c.mode)
		if got := focusAssistWNF(); got != c.mode {
			t.Errorf("%s: the WNF state is %d, want %d", c.profile, got, c.mode)
		}
		if got := FocusAssist(); got != c.mode {
			t.Errorf("%s: FocusAssist %d, want %d", c.profile, got, c.mode)
		}
		resetFocusAssistCache()
		if got, want := FocusAssistActive(), c.mode != FocusAssistOff; got != want {
			t.Errorf("%s: FocusAssistActive %v, want %v", c.profile, got, want)
		}
	}

	// priority only, with Clack among the profile's apps: not muted
	switchProfile(t, quietHoursPriorityOnly, FocusAssistPriorityOnly)
	allowClack(t, true)
	untilActive(t, "priority only with Clack allowed", false)
	// the cache: a change within 5 s isn't seen, whatever the last
	// answer was
	resetFocusAssistCache()
	last := FocusAssistActive()
	asked := time.Now()
	mustServer(t, "RemoveAllowedApp", func() error { return allowApp(AppUserModelID, false) })
	if time.Since(asked) < 4*time.Second && FocusAssistActive() != last {
		t.Error("the 5 s cache didn't hold the last answer")
	}
	allowClack(t, false)
	untilActive(t, "priority only without Clack allowed", true)
}

// a6's cache and a9 with the status swapped in (readFocusAssist): asked
// with Clack's app id and Slack's 2 s, at most every 5 s (an answer
// within them is the last one), and muting per a9: priority only unless
// Clack has priority, alarms only, never off, a failed read or no Focus
// Assist.
func TestFocusAssistActiveCachesLikeSlack(t *testing.T) {
	var mu sync.Mutex
	mode, priority, asked := FocusAssistPriorityOnly, false, 0
	real := readFocusAssist
	readFocusAssist = func(id string, timeout time.Duration) (int, bool) {
		mu.Lock()
		defer mu.Unlock()
		if id != AppUserModelID || timeout != 2*time.Second {
			t.Errorf("asked for %q within %v, want %q within 2s", id, timeout, AppUserModelID)
		}
		asked++
		return mode, priority
	}
	t.Cleanup(func() { readFocusAssist = real; resetFocusAssistCache() })
	set := func(m int, p bool) { mu.Lock(); mode, priority = m, p; mu.Unlock() }
	count := func() int { mu.Lock(); defer mu.Unlock(); return asked }
	expire := func() { // the last answer 5 s old
		focusAssist.Lock()
		focusAssist.at = time.Now().Add(-5*time.Second - time.Millisecond)
		focusAssist.Unlock()
	}

	resetFocusAssistCache()
	if !FocusAssistActive() {
		t.Error("priority only without priority: not muted")
	}
	set(FocusAssistOff, false)
	if !FocusAssistActive() || count() != 1 {
		t.Errorf("within 5 s: asked %d times; want the one answer, still muted", count())
	}
	expire()
	if FocusAssistActive() || count() != 2 {
		t.Errorf("5 s on, off: asked %d times; want 2, not muted", count())
	}
	for _, c := range []struct {
		mode     int
		priority bool
		want     bool
	}{
		{FocusAssistPriorityOnly, true, false},
		{FocusAssistPriorityOnly, false, true},
		{FocusAssistAlarmsOnly, false, true},
		{FocusAssistAlarmsOnly, true, true},
		{FocusAssistFailed, false, false},
		{FocusAssistNotSupported, false, false},
	} {
		set(c.mode, c.priority)
		expire()
		if got := FocusAssistActive(); got != c.want {
			t.Errorf("mode %d, priority %v: FocusAssistActive %v, want %v", c.mode, c.priority, got, c.want)
		}
	}
}
