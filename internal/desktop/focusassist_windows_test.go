//go:build windows

package desktop

import (
	"errors"
	"os"
	"testing"
	"time"
	"unsafe"

	"golang.org/x/sys/windows"
)

// Focus Assist on the runner (CLACK_WINDOWS_TRAY_TESTS, ci's
// windows-shell job): the WNF state and the COM settings agree, and the
// whole check answers well inside Slack's 2 s. With
// CLACK_WINDOWS_FOCUS_ASSIST_TESTS (it changes the runner user's Do not
// disturb) each profile set through IQuietHoursSettings shows in the WNF
// state, FocusAssist and FocusAssistActive (a9), and Clack among the
// priority-only profile's apps counts as priority. All of it is put back.
//
// On Windows Server 2025 a Do not disturb switch can crash the service
// behind IQuietHoursSettings (WpnUserService, restarted after 1 to 8 s):
// the next call fails with an RPC error and the switch may be lost, so
// the switches are retried until the service is back.

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
	if wnf >= 0 && com >= 0 && wnf != com {
		t.Errorf("the WNF state (%d) and IQuietHoursSettings (%d) disagree", wnf, com)
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

// switchProfile sets the profile and waits for the WNF state to say
// mode, setting it again when a crash lost the switch.
func switchProfile(t *testing.T, id string, mode int) {
	t.Helper()
	for try := 0; try < 4; try++ {
		if err := untilServer(t, "put_UserSelectedProfile("+id+")", func() error { return setQuietHoursProfile(id) }); err != nil {
			t.Fatalf("put_UserSelectedProfile(%s): %v", id, err)
		}
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

// priorityAnswer is FocusAssistPriority for Clack once the server answers.
func priorityAnswer(t *testing.T) int {
	t.Helper()
	deadline := time.Now().Add(30 * time.Second)
	for {
		p := FocusAssistPriority(AppUserModelID)
		if p != FocusAssistFailed || time.Now().After(deadline) {
			return p
		}
		time.Sleep(time.Second)
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
		if err := untilServer(t, "putting "+orig+" back", func() error { return setQuietHoursProfile(orig) }); err != nil {
			t.Errorf("putting %s back: %v", orig, err)
		}
		_ = untilServer(t, "RemoveAllowedApp", func() error { return allowApp(AppUserModelID, false) })
		resetFocusAssistCache()
	})
	_ = allowApp(AppUserModelID, false)
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
	if err := untilServer(t, "AddAllowedApp", func() error { return allowApp(AppUserModelID, true) }); err != nil {
		t.Fatalf("AddAllowedApp: %v", err)
	}
	if got := priorityAnswer(t); got != 1 {
		t.Errorf("isPriority with Clack allowed: %d, want 1", got)
	}
	resetFocusAssistCache()
	if FocusAssistActive() {
		t.Error("priority only with Clack allowed counts as muted")
	}
	// the cache: a change within 5 s isn't seen
	asked := time.Now()
	if err := untilServer(t, "RemoveAllowedApp", func() error { return allowApp(AppUserModelID, false) }); err != nil {
		t.Fatalf("RemoveAllowedApp: %v", err)
	}
	if time.Since(asked) < 4*time.Second && FocusAssistActive() {
		t.Error("the 5 s cache didn't hold the last answer")
	}
	if got := priorityAnswer(t); got != 0 {
		t.Errorf("isPriority after the removal: %d, want 0", got)
	}
	resetFocusAssistCache()
	if !FocusAssistActive() {
		t.Error("priority only without Clack allowed isn't muted")
	}
}
