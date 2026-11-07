package ui

// Interactive apps (internal/ui/blockkit): realtime modals, App Home
// tabs of app DMs, the app shortcuts in the message menu and the
// composer's + menu.

import (
	"image"
	"sync"

	"github.com/some-du6e/yoloslackgoclient/internal/core"
	"github.com/some-du6e/yoloslackgoclient/internal/ui/blockkit"
	"github.com/some-du6e/yoloslackgoclient/internal/ui/composer"
	"github.com/some-du6e/yoloslackgoclient/internal/ui/popups"
)

var (
	bkMu    sync.Mutex
	bkStops = map[*backend]func(){}
)

func init() {
	popups.MessageShortcuts = func(h popups.Host, m core.Message) []popups.MenuItem {
		return blockkit.MessageShortcutItems(h, m)
	}
	composer.ShortcutItems = func(h popups.Host, ch string, anchor image.Rectangle) []popups.MenuItem {
		return blockkit.ComposerShortcutItems(h, ch, anchor)
	}
}

// startBlockkit starts the view event pump once core is booted.
func (b *backend) startBlockkit(a *App) {
	if b.core == nil {
		return
	}
	stop := blockkit.Start(a)
	bkMu.Lock()
	bkStops[b] = stop
	bkMu.Unlock()
}

func (b *backend) stopBlockkit() {
	bkMu.Lock()
	stop := bkStops[b]
	delete(bkStops, b)
	bkMu.Unlock()
	if stop != nil {
		stop()
	}
}

// appDM is the open app DM's tabs state (nil for other conversations).
func (a *App) appDM(ch string) *blockkit.AppDM {
	if a.data == nil || a.data.core == nil {
		return nil
	}
	return blockkit.AppDMFor(a, ch)
}

// appDMRoadblock swaps the composer of a read-only app DM's Messages tab
// for Slack's notice (MessageInputAnnounceOnlyAppDmRoadblock).
func (a *App) appDMRoadblock(c *convo, comp composerView) {
	r, ok := comp.(interface{ SetRoadblock(string) })
	if !ok || c.ts != "" || comp == c.edit {
		return
	}
	msg := ""
	if dm := a.appDM(c.ch); dm != nil && dm.ReadOnly() {
		msg = "Sending messages to this app has been turned off."
	}
	r.SetRoadblock(msg)
}
