//go:build !windows

package desktop

import "errors"

// Tray is the Windows notification-area icon; other platforms have none
// (macOS has the Dock, Linux desktops differ too much).
type Tray struct{}

// StartTray is Windows-only.
func StartTray(tooltip string, onOpen, onQuit func()) (*Tray, error) {
	return nil, errors.New("desktop: no tray on this platform")
}

// SetTooltip is a no-op.
func (t *Tray) SetTooltip(s string) {}

// Close is a no-op.
func (t *Tray) Close() {}
