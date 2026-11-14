//go:build !windows

package desktop

import "errors"

// Tray is the Windows notification-area icon; other platforms have none
// (macOS has the Dock, Linux desktops differ too much).
type Tray struct{}

// TrayState is the Windows tray icon's state.
type TrayState string

const (
	TrayRest      TrayState = "rest"
	TrayUnread    TrayState = "unread"
	TrayHighlight TrayState = "highlight"
)

// TrayIcons are the Windows tray's .ico files by state.
type TrayIcons map[TrayState][]byte

// TrayMenuItem is an item of the Windows tray's context menu.
type TrayMenuItem struct {
	Sep                   bool
	Label, Accel          string
	Check, Radio, Checked bool
	Disabled              bool
	Sub                   []TrayMenuItem
	Do                    func()
}

// StartTray is Windows-only.
func StartTray(tooltip string, icons TrayIcons, onClick func()) (*Tray, error) {
	return nil, errors.New("desktop: no tray on this platform")
}

// SetTooltip is a no-op.
func (t *Tray) SetTooltip(s string) {}

// SetImage is a no-op.
func (t *Tray) SetImage(s TrayState) {}

// SetMenu is a no-op.
func (t *Tray) SetMenu(items []TrayMenuItem) {}

// Balloon is Windows-only.
func (t *Tray) Balloon(title, content string) error {
	return errors.New("desktop: no tray on this platform")
}

// Close is a no-op.
func (t *Tray) Close() {}
