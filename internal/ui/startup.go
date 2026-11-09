package ui

import (
	"fmt"
	"hash/fnv"
	"strconv"
	"time"

	"github.com/some-du6e/yoloslackgoclient/internal/applog"
)

// The startup timeline's UI half (applog/startup.go has the format): the
// milestones only the window knows, and the re-layout count. Each frame
// until the timeline settles, startupFrame compares what the sidebar and
// the open channel drew with the frame before:
//
//   - sidebar: the rows (sections, conversations, their order and labels)
//     changed after they were painted;
//   - unreads: a painted row's unread/mention state changed;
//   - messages: a painted message row moved or resized;
//   - theme: the account theme changed after the first frame (setAcct).
//
// It settles once Slack answered (sidebar-live), the open channel's
// messages and the visible images are in, the plugins (if any) attached,
// and nothing moved for settleQuiet; or at settleTimeout.

const (
	settleQuiet   = 1500 * time.Millisecond
	settleTimeout = 60 * time.Second
)

type startupWatch struct {
	done       bool
	painted    bool
	sideGen    int
	sideSig    uint64
	unreadSig  uint64
	msgCh      string
	msgSig     uint64
	msgN       int
	lastChange time.Time
	wake       *time.Timer
	themeFinal bool // the account's prefs (cached or live) are applied
}

func (a *App) startupChange(now time.Time) { a.sw.lastChange = now }

// startupMark is applog.Mark that also counts as a change for settling.
func (a *App) startupMark(name, detail string) {
	if !applog.Marked(name) {
		applog.Mark(name, detail)
		a.sw.lastChange = time.Now()
	}
}

// startupFrame runs at the end of every laid-out frame (UI goroutine).
func (a *App) startupFrame() {
	sw := &a.sw
	if sw.done {
		return
	}
	now := time.Now()
	if sw.lastChange.IsZero() {
		sw.lastChange = now
	}
	a.startupMark("first-text", "")
	if sw.themeFinal {
		a.startupMark("theme", map[bool]string{true: "account", false: "default"}[a.acct.set])
	}
	warm := a.data != nil && a.data.warm
	// the sidebar
	m := a.model()
	if a.sidebar.rowCount > 0 && len(m.Convs) > 0 {
		src := "live"
		if warm {
			src = "cached"
		}
		a.startupMark("sidebar", fmt.Sprintf("%s, %d rows", src, a.sidebar.rowCount))
		if a.sidebar.gen != sw.sideGen {
			sw.sideGen = a.sidebar.gen
			h := fnv.New64a()
			for _, r := range a.sidebar.rows {
				h.Write([]byte{byte(r.kind)})
				h.Write([]byte(r.key))
				h.Write([]byte{0})
				h.Write([]byte(r.label))
				h.Write([]byte{0})
			}
			if s := h.Sum64(); s != sw.sideSig {
				if sw.sideSig != 0 {
					applog.Relayout("sidebar")
					a.startupChange(now)
				}
				sw.sideSig = s
			}
		}
		h := fnv.New64a()
		var buf []byte
		for _, r := range a.sidebar.rows {
			if r.kind != sbConv {
				continue
			}
			if c := m.Convs[r.key]; c != nil {
				buf = append(buf[:0], r.key...)
				buf = strconv.AppendBool(buf, c.Unread)
				buf = strconv.AppendInt(buf, int64(c.Mentions), 10)
				h.Write(buf)
			}
		}
		if s := h.Sum64(); s != sw.unreadSig {
			if sw.unreadSig != 0 {
				applog.Relayout("unreads")
				a.startupChange(now)
			}
			sw.unreadSig = s
		}
	}
	// the open channel
	ch := a.router.Current().Channel()
	if ch != sw.msgCh { // navigated: a new list isn't a move
		sw.msgCh, sw.msgSig, sw.msgN = ch, 0, 0
	}
	if cv := a.convos[ch+"/"]; ch != "" && cv != nil {
		if l, ok := cv.list.(interface{ LayoutSig() (int, bool, uint64) }); ok {
			n, loaded, sig := l.LayoutSig()
			if n > 0 && loaded {
				src := "live"
				if c := a.Core(); c != nil && c.Warm() && !applog.Marked("history-live") {
					src = "cached"
				}
				a.startupMark("messages", fmt.Sprintf("%s, %d msgs", src, n))
				if sig != sw.msgSig {
					if sw.msgSig != 0 && sw.msgN > 0 {
						applog.Relayout("messages")
						a.startupChange(now)
					}
					sw.msgSig, sw.msgN = sig, n
				}
			}
		}
	}
	if applog.Marked("messages") && applog.Marked("sidebar") && a.images.Pending() == 0 {
		a.startupMark("images", "")
	}
	sw.painted = true
	// settled?
	ready := applog.Marked("sidebar-live") && applog.Marked("images") && (ch == "" || applog.Marked("messages"))
	if c := a.Core(); ready && c != nil && c.Warm() && c.RestoredHistory() != "" {
		ready = applog.Marked("history-live") || applog.Marked("history-failed")
	}
	if !applog.Marked("plugins") {
		ready = false // plugins still starting (startTautAsync marks it, with or without any)
	}
	started := a.opts.Started
	switch {
	case ready && now.Sub(sw.lastChange) >= settleQuiet:
		sw.done = true
		applog.Settle(sw.lastChange, "quiet")
	case !started.IsZero() && now.Sub(started) > settleTimeout:
		sw.done = true
		applog.Settle(now, "timeout")
	default: // come back when the quiet period would end
		if sw.wake == nil {
			sw.wake = time.AfterFunc(settleQuiet, func() {
				a.Post(func() { a.sw.wake = nil })
			})
		}
	}
}

// startupTheme counts an account theme change after the first painted
// frame (the flash a cold start shows) and notes that the account's prefs
// arrived.
func (a *App) startupTheme(changed bool) {
	if a.sw.done {
		return
	}
	if changed && a.sw.painted {
		applog.Relayout("theme")
		a.startupChange(time.Now())
	}
}
