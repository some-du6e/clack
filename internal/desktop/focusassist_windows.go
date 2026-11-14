//go:build windows

package desktop

import (
	"fmt"
	"log"
	"sync"
	"time"
	"unsafe"

	"golang.org/x/sys/windows"
)

// Focus Assist (Do not disturb on Windows 11), as Slack desktop reads it
// before a message sound (main.bundle.cjs 4.53.276 win32):
// checkNotificationState("sound playback") (dt @1003789) is
// isWindowsToastNotificationEnabled() && !isFocusAssistActive(), and
// isFocusAssistActive (a6 @924384) asks a utility process
// (call-get-windows-focus-assist-entry-point, @923361: killed after 2 s,
// which answers mode -1) at most every 5 s. The process runs the
// windows-focus-assist module (1.4.0, lib/focus-assist.cc and lib/index.js):
//
//   - getFocusAssist: the WNF state
//     WNF_SHEL_QUIETHOURS_ACTIVE_PROFILE_CHANGED read with
//     NtQueryWnfStateData into a DWORD (0 off, 1 priority only, 2 alarms
//     only); when that call fails, IQuietHoursSettings'
//     UserSelectedProfile (CLSID_QuietHoursSettings, an out-of-process
//     server) by profile id. index.js turns any other value into
//     undefined, which the process sends as -1.
//   - only in priority-only mode, isPriority(app id): whether the app is
//     among the PriorityOnly profile's allowed apps
//     (IQuietHoursProfile::GetAllowedApps, compared exactly).
//
// a9 (focusAssistOn) then says whether that mutes the sound.

var (
	ntdll                   = windows.NewLazySystemDLL("ntdll.dll")
	procNtQueryWnfStateData = ntdll.NewProc("NtQueryWnfStateData")
	procCoTaskMemFree       = ole32.NewProc("CoTaskMemFree")
)

var (
	clsidQuietHoursSettings = guid(0xF53321FA, 0x34F8, 0x4B7F, 0xB9, 0xA3, 0x36, 0x18, 0x77, 0xCB, 0x94, 0xCF)
	iidIQuietHoursSettings  = guid(0x6BFF4732, 0x81EC, 0x4FFB, 0xAE, 0x67, 0xB6, 0xC1, 0xBC, 0x29, 0x63, 0x1F)
)

const (
	vtQHSettingsGetUserSelectedProfile = 3 // IQuietHoursSettings
	vtQHSettingsGetProfile             = 5
	vtQHProfileGetAllowedApps          = 11 // IQuietHoursProfile
	clsctxLocalServer                  = 4

	quietHoursPriorityOnly = "Microsoft.QuietHoursProfile.PriorityOnly"
	quietHoursAlarmsOnly   = "Microsoft.QuietHoursProfile.AlarmsOnly"
)

// wnfQuietHoursActiveProfileChanged is
// WNF_SHEL_QUIETHOURS_ACTIVE_PROFILE_CHANGED.
var wnfQuietHoursActiveProfileChanged = [2]uint32{0xA3BF1C75, 0x0D83063E}

// FocusAssist is windows-focus-assist's getFocusAssist: the WNF state,
// else the COM settings' profile, as one of the FocusAssist* modes.
func FocusAssist() int {
	mode := focusAssistWNF()
	if mode == FocusAssistFailed {
		mode = focusAssistCOM()
	}
	if mode < FocusAssistNotSupported || mode > FocusAssistAlarmsOnly {
		return FocusAssistFailed // index.js's undefined, sent as -1
	}
	return mode
}

// focusAssistWNF is InternalGetFocusAssistWNF.
func focusAssistWNF() int {
	if procNtQueryWnfStateData.Find() != nil {
		return FocusAssistFailed
	}
	var stamp, buf uint32
	size := uint32(unsafe.Sizeof(buf))
	st, _, _ := procNtQueryWnfStateData.Call(uintptr(unsafe.Pointer(&wnfQuietHoursActiveProfileChanged)), 0, 0,
		uintptr(unsafe.Pointer(&stamp)), uintptr(unsafe.Pointer(&buf)), uintptr(unsafe.Pointer(&size)))
	if int32(uint32(st)) < 0 { // !NT_SUCCESS
		return FocusAssistFailed
	}
	return int(int32(buf))
}

func newQuietHoursSettings() (*rtObject, error) {
	var p *rtObject
	hr, _, _ := procCoCreateInstance.Call(uintptr(unsafe.Pointer(&clsidQuietHoursSettings)), 0, clsctxLocalServer,
		uintptr(unsafe.Pointer(&iidIQuietHoursSettings)), uintptr(unsafe.Pointer(&p)))
	if failed(hr) || p == nil {
		return nil, fmt.Errorf("CoCreateInstance(CLSID_QuietHoursSettings): %w", HResultError(hr))
	}
	return p, nil
}

// userSelectedProfile is IQuietHoursSettings::get_UserSelectedProfile.
func userSelectedProfile(s *rtObject) (string, error) {
	var id *uint16
	if hr := s.call(vtQHSettingsGetUserSelectedProfile, uintptr(unsafe.Pointer(&id))); failed(hr) || id == nil {
		return "", fmt.Errorf("IQuietHoursSettings::get_UserSelectedProfile: %w", HResultError(hr))
	}
	defer procCoTaskMemFree.Call(uintptr(unsafe.Pointer(id)))
	return windows.UTF16PtrToString(id), nil
}

// focusAssistCOM is InternalGetFocusAssistCOM.
func focusAssistCOM() int {
	mode := FocusAssistFailed
	_ = comThread(func() error {
		s, err := newQuietHoursSettings()
		if err != nil {
			return err
		}
		defer s.release()
		p, err := userSelectedProfile(s)
		if err != nil {
			return err
		}
		switch p {
		case quietHoursPriorityOnly:
			mode = FocusAssistPriorityOnly
		case quietHoursAlarmsOnly:
			mode = FocusAssistAlarmsOnly
		default:
			mode = FocusAssistOff
		}
		return nil
	})
	return mode
}

// FocusAssistPriority is windows-focus-assist's isPriority: 1 when
// appUserModelID is one of the priority-only profile's allowed apps, 0
// when it isn't, FocusAssistFailed when Windows won't say.
func FocusAssistPriority(appUserModelID string) int {
	res := FocusAssistFailed
	_ = comThread(func() error {
		s, err := newQuietHoursSettings()
		if err != nil {
			return err
		}
		defer s.release()
		if _, err := userSelectedProfile(s); err != nil {
			return err
		}
		name, err := windows.UTF16PtrFromString(quietHoursPriorityOnly)
		if err != nil {
			return err
		}
		var prof *rtObject
		if hr := s.call(vtQHSettingsGetProfile, uintptr(unsafe.Pointer(name)), uintptr(unsafe.Pointer(&prof))); failed(hr) || prof == nil {
			return HResultError(hr)
		}
		defer prof.release()
		var count uint32
		var apps **uint16
		if hr := prof.call(vtQHProfileGetAllowedApps, uintptr(unsafe.Pointer(&count)), uintptr(unsafe.Pointer(&apps))); failed(hr) {
			return HResultError(hr)
		}
		res = 0
		if apps == nil {
			return nil
		}
		for _, p := range unsafe.Slice(apps, count) {
			if p != nil && windows.UTF16PtrToString(p) == appUserModelID {
				res = 1
			}
			procCoTaskMemFree.Call(uintptr(unsafe.Pointer(p)))
		}
		procCoTaskMemFree.Call(uintptr(unsafe.Pointer(apps)))
		return nil
	})
	return res
}

// focusAssistStatus is the utility process's answer: the mode and, in
// priority-only mode, whether the app has priority. After timeout it is
// -1 and false, as when Slack kills the process.
func focusAssistStatus(appUserModelID string, timeout time.Duration) (mode int, priority bool) {
	type answer struct {
		mode     int
		priority bool
	}
	c := make(chan answer, 1)
	go func() {
		m := FocusAssist()
		p := false
		if m == FocusAssistPriorityOnly && appUserModelID != "" {
			p = FocusAssistPriority(appUserModelID) == 1
		}
		c <- answer{m, p}
	}()
	select {
	case a := <-c:
		return a.mode, a.priority
	case <-time.After(timeout):
		return FocusAssistFailed, false
	}
}

// focusAssist is a6's cache: the last answer and when it came.
var focusAssist struct {
	sync.Mutex
	mode     int
	priority bool
	at       time.Time
	logged   string
}

// FocusAssistActive is Slack's isFocusAssistActive for Clack's app id: the
// status at most every 5 s (2 s to answer), priority-only counting unless
// Clack has priority, alarms-only always. Blocking: not the UI goroutine.
func FocusAssistActive() bool {
	fa := &focusAssist
	fa.Lock()
	defer fa.Unlock()
	if !fa.at.IsZero() && time.Since(fa.at) < 5*time.Second {
		return focusAssistOn(fa.mode, fa.priority)
	}
	fa.mode, fa.priority = focusAssistStatus(AppUserModelID, 2*time.Second)
	fa.at = time.Now()
	if s := fmt.Sprintf("mode=%d, hasPriority=%v", fa.mode, fa.priority); s != fa.logged {
		fa.logged = s
		log.Printf("desktop: Focus Assist status is %s", s)
	}
	return focusAssistOn(fa.mode, fa.priority)
}
