//go:build windows

package desktop

import (
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
		if err := setQuietHoursProfile(orig); err != nil {
			t.Errorf("putting %s back: %v", orig, err)
		}
		_ = allowApp(AppUserModelID, false)
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
		if err := setQuietHoursProfile(c.profile); err != nil {
			t.Fatalf("put_UserSelectedProfile(%s): %v", c.profile, err)
		}
		deadline := time.Now().Add(5 * time.Second)
		for focusAssistWNF() != c.mode && time.Now().Before(deadline) {
			time.Sleep(50 * time.Millisecond)
		}
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
	if err := setQuietHoursProfile(quietHoursPriorityOnly); err != nil {
		t.Fatal(err)
	}
	if err := allowApp(AppUserModelID, true); err != nil {
		t.Fatalf("AddAllowedApp: %v", err)
	}
	if got := FocusAssistPriority(AppUserModelID); got != 1 {
		t.Errorf("isPriority with Clack allowed: %d, want 1", got)
	}
	resetFocusAssistCache()
	if FocusAssistActive() {
		t.Error("priority only with Clack allowed counts as muted")
	}
	// the cache: a change within 5 s isn't seen
	if err := allowApp(AppUserModelID, false); err != nil {
		t.Fatalf("RemoveAllowedApp: %v", err)
	}
	if FocusAssistActive() {
		t.Error("the 5 s cache didn't hold the last answer")
	}
	if got := FocusAssistPriority(AppUserModelID); got != 0 {
		t.Errorf("isPriority after the removal: %d, want 0", got)
	}
	resetFocusAssistCache()
	if !FocusAssistActive() {
		t.Error("priority only without Clack allowed isn't muted")
	}
}
