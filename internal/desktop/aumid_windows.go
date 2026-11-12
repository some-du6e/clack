package desktop

import (
	"errors"
	"os"
	"path/filepath"
	"sync"

	"golang.org/x/sys/windows/registry"
)

// Clack's AppUserModelID, what Windows files its toasts under (and the
// macOS bundle id, packaging/macos/Info.plist).
//
// Windows drops a toast from an app id it doesn't know, without an error:
// ToastNotifier.Show succeeds and nothing appears. Slack desktop's id is
// com.squirrel.slack.slack (main.bundle.cjs 4.53.276 win32, module 92049
// `ys`; the MSIX build's is `${familyName}!Slack`), set at start with
// app.setAppUserModelId and known to Windows through the Start menu
// shortcut Squirrel makes with that id. Clack registers its id the way
// an unpackaged app can without a shortcut, the key the Windows App SDK
// writes for one (RegisterAssets, dev/AppNotifications/
// AppNotificationUtility.cpp at f753170e; with no IconUri "the OS will
// render a default icon"):
//
//	HKCU\Software\Classes\AppUserModelId\io.github.some-du6e.clack
//	  DisplayName  Clack                                  (REG_EXPAND_SZ)
//	  IconUri      %LOCALAPPDATA%\Clack\clack.png         (REG_EXPAND_SZ, the path itself)
//
// so the installed and the portable Clack.exe are the same to Windows,
// and taskbar pins made before this keep grouping with the window (an
// explicit process id would split them). The installer writes the same
// key and file and removes them at uninstall (packaging/windows/clack.iss);
// Clack.exe writes whatever is missing before its first toast.
const AppUserModelID = "io.github.some-du6e.clack"

const aumidDisplayName = "Clack"

func aumidKey(id string) string { return `Software\Classes\AppUserModelId\` + id }

// AUMIDIconPath is the icon file the registration names:
// %LOCALAPPDATA%\Clack\clack.png.
func AUMIDIconPath() (string, error) {
	d := os.Getenv("LOCALAPPDATA")
	if d == "" {
		return "", errors.New("desktop: LOCALAPPDATA is not set")
	}
	return filepath.Join(d, "Clack", "clack.png"), nil
}

// aumidMu keeps two registrations from interleaving.
var aumidMu sync.Mutex

// RegisterAppUserModelID registers id for toasts as name with the icon
// at iconPath, writing png there first when the file is missing. It
// writes only what differs and reports whether it wrote anything.
func RegisterAppUserModelID(id, name, iconPath string, png []byte) (changed bool, err error) {
	aumidMu.Lock()
	defer aumidMu.Unlock()
	if iconPath != "" && len(png) > 0 {
		if st, err := os.Stat(iconPath); err != nil || st.Size() == 0 {
			if err := os.MkdirAll(filepath.Dir(iconPath), 0o755); err != nil {
				return false, err
			}
			if err := os.WriteFile(iconPath, png, 0o644); err != nil {
				return false, err
			}
			changed = true
		}
	}
	k, _, err := registry.CreateKey(registry.CURRENT_USER, aumidKey(id), registry.QUERY_VALUE|registry.SET_VALUE)
	if err != nil {
		return changed, err
	}
	defer k.Close()
	for _, v := range [][2]string{{"DisplayName", name}, {"IconUri", iconPath}} {
		if v[1] == "" {
			continue
		}
		if cur, _, err := k.GetStringValue(v[0]); err == nil && cur == v[1] {
			continue
		}
		if err := k.SetExpandStringValue(v[0], v[1]); err != nil {
			return changed, err
		}
		changed = true
	}
	return changed, nil
}

// EnsureAppUserModelID is Clack's own registration: AppUserModelID as
// "Clack", with png (the app icon) at AUMIDIconPath.
func EnsureAppUserModelID(png []byte) (changed bool, err error) {
	icon, err := AUMIDIconPath()
	if err != nil {
		icon = "" // no icon: Windows shows a blank square, the toast still shows
	}
	return RegisterAppUserModelID(AppUserModelID, aumidDisplayName, icon, png)
}

// AppUserModelIDRegistered reports whether id has a registration with a
// display name (what Windows needs to take its toasts).
func AppUserModelIDRegistered(id string) bool {
	k, err := registry.OpenKey(registry.CURRENT_USER, aumidKey(id), registry.QUERY_VALUE)
	if err != nil {
		return false
	}
	defer k.Close()
	name, _, err := k.GetStringValue("DisplayName")
	return err == nil && name != ""
}

// UnregisterAppUserModelID removes id's registration (tests; the
// uninstaller removes Clack's).
func UnregisterAppUserModelID(id string) error {
	aumidMu.Lock()
	defer aumidMu.Unlock()
	err := registry.DeleteKey(registry.CURRENT_USER, aumidKey(id))
	if errors.Is(err, registry.ErrNotExist) {
		return nil
	}
	return err
}
