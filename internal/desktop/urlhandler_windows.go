package desktop

import (
	"fmt"
	"os"
	"strings"

	"golang.org/x/sys/windows/registry"
)

const schemeKey = `Software\Classes\slack`

// RegisterURLHandler makes Clack the current user's slack:// handler
// (HKCU\Software\Classes\slack → "Clack.exe" "%1"). The installer writes
// the same keys; this is for the portable zip.
func RegisterURLHandler() error {
	exe, err := os.Executable()
	if err != nil {
		return err
	}
	k, _, err := registry.CreateKey(registry.CURRENT_USER, schemeKey, registry.ALL_ACCESS)
	if err != nil {
		return err
	}
	defer k.Close()
	if err := k.SetStringValue("", "URL:slack Protocol"); err != nil {
		return err
	}
	if err := k.SetStringValue("URL Protocol", ""); err != nil {
		return err
	}
	for sub, val := range map[string]string{
		`DefaultIcon`:        `"` + exe + `",0`,
		`shell\open\command`: `"` + exe + `" "%1"`,
	} {
		sk, _, err := registry.CreateKey(k, sub, registry.ALL_ACCESS)
		if err != nil {
			return err
		}
		err = sk.SetStringValue("", val)
		sk.Close()
		if err != nil {
			return err
		}
	}
	return nil
}

// UnregisterURLHandler removes the per-user slack:// keys if they point at
// Clack (another app's registration is left alone).
func UnregisterURLHandler() error {
	k, err := registry.OpenKey(registry.CURRENT_USER, schemeKey+`\shell\open\command`, registry.QUERY_VALUE)
	if err != nil {
		return nil // nothing registered for this user
	}
	cmd, _, _ := k.GetStringValue("")
	k.Close()
	if !strings.Contains(strings.ToLower(cmd), "clack") {
		return fmt.Errorf("slack:// belongs to another app (%s); left alone", cmd)
	}
	for _, sub := range []string{`\shell\open\command`, `\shell\open`, `\shell`, `\DefaultIcon`, ``} {
		if err := registry.DeleteKey(registry.CURRENT_USER, schemeKey+sub); err != nil && err != registry.ErrNotExist {
			return err
		}
	}
	return nil
}
