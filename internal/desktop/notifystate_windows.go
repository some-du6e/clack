//go:build windows

package desktop

import (
	"unsafe"

	"golang.org/x/sys/windows/registry"
)

var procSHQueryUserNotificationState = shell32.NewProc("SHQueryUserNotificationState")

// NotificationState is SHQueryUserNotificationState: whether the shell
// shows notification banners now (NotifyStateAccepts) or holds them, as a
// NotifyState* value; NotifyStateUnknown when the call fails.
func NotificationState() int {
	if procSHQueryUserNotificationState.Find() != nil {
		return NotifyStateUnknown
	}
	var s uint32
	if hr, _, _ := procSHQueryUserNotificationState.Call(uintptr(unsafe.Pointer(&s))); failed(hr) {
		return NotifyStateUnknown
	}
	if s < NotifyStateNotPresent || s > NotifyStateApp {
		return NotifyStateUnknown
	}
	return int(s)
}

// Settings > System > Notifications in the current user's registry:
// "Notifications" (every app's toasts) is PushNotifications' ToastEnabled,
// an app's own switch Enabled under Notifications\Settings\<app id>, with
// the app's other settings (ShowInActionCenter, the banners', the sound)
// beside it. Windows writes a value only once it's been switched, so a
// missing one is on.
const (
	pushNotificationsKey = `Software\Microsoft\Windows\CurrentVersion\PushNotifications`
	notificationsAppsKey = `Software\Microsoft\Windows\CurrentVersion\Notifications\Settings\`
)

// ToastSwitches reads the notifications' switches for app id aumid: all,
// every app's (ToastEnabled), app, aumid's own (Enabled), each a
// ToastSwitch* value, and every DWORD value under aumid's key by name (nil
// when there's no key). Clack's app id is unpackaged, for which
// ToastNotifier.Setting fails (0x80070490): these are what say its toasts
// are off.
func ToastSwitches(aumid string) (all, app int, values map[string]uint32) {
	all, app = ToastSwitchUnset, ToastSwitchUnset
	if k, err := registry.OpenKey(registry.CURRENT_USER, pushNotificationsKey, registry.QUERY_VALUE); err == nil {
		if v, _, err := k.GetIntegerValue("ToastEnabled"); err == nil {
			all = switchOf(v)
		}
		k.Close()
	}
	k, err := registry.OpenKey(registry.CURRENT_USER, notificationsAppsKey+aumid, registry.QUERY_VALUE)
	if err != nil {
		return all, app, nil
	}
	defer k.Close()
	names, _ := k.ReadValueNames(0)
	values = map[string]uint32{}
	for _, n := range names {
		if v, typ, err := k.GetIntegerValue(n); err == nil && typ == registry.DWORD {
			values[n] = uint32(v)
		}
	}
	if v, ok := values["Enabled"]; ok {
		app = switchOf(uint64(v))
	}
	return all, app, values
}

func switchOf(v uint64) int {
	if v == 0 {
		return ToastSwitchOff
	}
	return ToastSwitchOn
}
