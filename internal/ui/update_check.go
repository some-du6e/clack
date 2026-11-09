package ui

import (
	"context"
	"errors"
	"fmt"
	"image"
	"log"
	"net"
	"runtime"
	"strconv"
	"time"

	"gioui.org/layout"
	"gioui.org/op"

	"github.com/some-du6e/yoloslackgoclient/internal/ui/popups"
	"github.com/some-du6e/yoloslackgoclient/internal/ui/theme"
)

// Update checks: when they run (a wall-clock schedule), "Check now"
// (Preferences > Advanced > Updates), Clack > Check for Updates… on macOS
// and the workspace menu's item elsewhere. Every check runs on
// updateLoop's goroutine; results come back to the UI goroutine through
// Post. desktop.go has the download/install phases, update_ui.go what
// they look like (Slack desktop's Help dot, dialog and menu titles).

const (
	updateFirst = 15 * time.Second // after launch, so it doesn't slow startup
	updateTick  = 5 * time.Minute  // how often the loop looks at the clock
	updateEvery = time.Hour        // automatic checks, by the wall clock
	updateRetry = 5 * time.Minute  // after a failed check, doubling to updateEvery
	// updateSkew: a last check further than this in the future means the
	// clock was set back; check rather than wait for it to catch up.
	updateSkew = 5 * time.Minute
)

// updateSched decides when an automatic check is due. Times are wall
// clock (Round(0) strips the monotonic reading): Go's monotonic clock
// stops while a Mac sleeps, so "an hour" of sleeping timers can be days.
// Last and Limited persist in ui.json, so a restart doesn't ask GitHub
// again; the error backoff doesn't.
type updateSched struct {
	Last    time.Time // the last check GitHub answered
	Limited time.Time // GitHub's rate limit: no request before this
	retry   time.Time // a failed check: no automatic retry before this
	fails   int
}

// due reports whether an automatic check should run at now.
func (s *updateSched) due(now time.Time) bool {
	now = now.Round(0)
	if now.Before(s.Limited) && s.Limited.Sub(now) <= 24*time.Hour {
		return false
	}
	if now.Before(s.retry) {
		return false
	}
	if s.Last.IsZero() {
		return true
	}
	since := now.Sub(s.Last)
	return since >= updateEvery || since < -updateSkew
}

// limited reports whether GitHub's rate limit is still on at now, and
// until when.
func (s *updateSched) limited(now time.Time) (time.Time, bool) {
	now = now.Round(0)
	return s.Limited, now.Before(s.Limited) && s.Limited.Sub(now) <= 24*time.Hour
}

// retryAter is desktop.RateLimitError (ui doesn't import desktop).
type retryAter interface{ RetryAt() time.Time }

// record takes a check's outcome at now.
func (s *updateSched) record(now time.Time, err error) {
	now = now.Round(0)
	var rl retryAter
	switch {
	case err == nil:
		s.Last, s.Limited, s.retry, s.fails = now, time.Time{}, time.Time{}, 0
	case errors.As(err, &rl):
		s.Limited = rl.RetryAt().Round(0)
		if s.Limited.Before(now.Add(time.Minute)) {
			s.Limited = now.Add(time.Minute)
		}
	default:
		s.fails = min(s.fails+1, 8)
		s.retry = now.Add(min(updateEvery, updateRetry<<(s.fails-1)))
	}
}

// updateClock is updateLoop's time: the real one, or a test's.
type updateClock struct {
	now   func() time.Time
	first <-chan time.Time // the first automatic check after launch
	tick  <-chan time.Time
	stop  <-chan struct{} // nil: runs for the life of the process
}

func realUpdateClock() updateClock {
	return updateClock{now: time.Now, first: time.After(updateFirst), tick: time.NewTicker(updateTick).C}
}

// updateResult is one check's outcome, for the UI goroutine.
type updateResult struct {
	info   UpdateInfo
	ok     bool
	err    error
	manual bool
	at     time.Time   // when it finished (wall clock)
	sched  updateSched // the schedule after it, to persist
}

// updateSchedule is the persisted schedule (UI goroutine: Run).
func (a *App) updateSchedule() updateSched {
	var s updateSched
	if a.prefs.UpdateCheckedAt > 0 {
		s.Last = time.Unix(a.prefs.UpdateCheckedAt, 0)
	}
	if a.prefs.UpdateLimitedUntil > 0 {
		s.Limited = time.Unix(a.prefs.UpdateLimitedUntil, 0)
	}
	return s
}

// updateLoop runs the checks: automatically once due (and "Check for
// updates" is on), and right away for "Check now" whatever that setting
// says. It is the only goroutine that calls Options.CheckUpdate, so a
// check never overlaps another.
func (a *App) updateLoop(s updateSched, clk updateClock) {
	if a.opts.CheckUpdate == nil {
		return
	}
	first := clk.first
	for {
		manual := false
		select {
		case <-clk.stop:
			return
		case <-first:
			first = nil
		case <-clk.tick:
		case <-a.updWake:
		case <-a.updNow:
			manual = true
		}
		now := clk.now()
		if !manual && (a.updatesOff.Load() || !s.due(now)) {
			continue
		}
		var r updateResult
		if until, on := s.limited(now); on {
			// rate limited: asking now only pushes the limit out
			r = updateResult{err: &updateLimited{until}, manual: manual, at: now.Round(0)}
		} else {
			ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
			info, ok, err := a.opts.CheckUpdate(ctx)
			cancel()
			now = clk.now()
			s.record(now, err)
			if err != nil {
				log.Printf("ui: update check: %v", err)
			}
			r = updateResult{info: info, ok: ok, err: err, manual: manual, at: now.Round(0)}
		}
		r.sched = s
		a.Post(func() { a.updateChecked(r) })
	}
}

// updateLimited is a check Clack didn't send: GitHub's rate limit is on.
type updateLimited struct{ until time.Time }

func (e *updateLimited) Error() string      { return "update check: rate limited" }
func (e *updateLimited) RetryAt() time.Time { return e.until }

// nudgeUpdates asks the loop to look at the clock now (the window got
// focus: maybe after the machine slept). It checks only if one is due.
func (a *App) nudgeUpdates() {
	select {
	case a.updWake <- struct{}{}:
	default:
	}
}

// checkForUpdates is "Check now" and the menus' Check for Updates…: a
// check right away, even with "Check for updates" off. notify (the
// menus) answers like Slack's manual check: a message box when there's
// nothing new or the check failed, else the download starts (the menus
// say Downloading Update, then Help gets its dot). The Preferences row
// shows it in place. UI goroutine.
func (a *App) checkForUpdates(notify bool) {
	if a.opts.CheckUpdate == nil {
		if notify {
			a.Toast("This build of " + AppName + " doesn’t check for updates.")
		}
		return
	}
	c := &a.update.check
	c.notify = c.notify || notify
	if c.checking {
		return // one is on its way; it answers both
	}
	c.checking = true
	select {
	case a.updNow <- struct{}{}:
	default:
	}
	a.Invalidate()
}

// updateCheckState is the result of the last check this session, for
// Preferences.
type updateCheckState struct {
	checking bool   // a manual check is running
	notify   bool   // a menu asked: answer in a message box
	upToDate bool   // the last answer: nothing newer
	err      string // the last manual check failed: why, in plain words
	at       time.Time
}

// updateChecked takes a check's result (UI goroutine).
func (a *App) updateChecked(r updateResult) {
	c := &a.update.check
	notify := false
	if r.manual {
		notify = c.notify
		c.checking, c.notify = false, false
	}
	// persist the schedule
	last, lim := unixOrZero(r.sched.Last), unixOrZero(r.sched.Limited)
	if last != a.prefs.UpdateCheckedAt || lim != a.prefs.UpdateLimitedUntil {
		a.prefs.UpdateCheckedAt, a.prefs.UpdateLimitedUntil = last, lim
		a.savePrefs()
	}
	switch {
	case r.err != nil:
		if !r.manual {
			break // automatic: logged; Preferences keeps the last answer
		}
		c.err = updateErrText(r.err, r.at)
		if notify { // Slack's yR
			a.updateMessageBox("Couldn’t update "+AppName, c.err)
		}
	case r.ok:
		c.err, c.upToDate, c.at = "", false, r.at
		a.updateFound(r.info, r.manual)
		if notify && a.update.phase == updAvailable {
			// Slack's manual check downloads what it finds; Clack's
			// does too when automatic downloads are off. Without a
			// downloader (a package-managed install), Preferences has
			// the release page.
			if a.opts.DownloadUpdate != nil {
				a.startUpdateDownload(true)
			} else {
				a.openPrefsSection("advanced")
			}
		}
	default:
		c.err, c.upToDate, c.at = "", true, r.at
		if notify { // Slack's yI
			a.updateMessageBox("You’re all good", "You’ve got the latest version of "+AppName+"; thanks for staying on the ball.")
		}
	}
	a.Invalidate()
}

func unixOrZero(t time.Time) int64 {
	if t.IsZero() {
		return 0
	}
	return t.Unix()
}

// updateErrText says why a check failed, in plain words.
func updateErrText(err error, now time.Time) string {
	var rl retryAter
	if errors.As(err, &rl) {
		return "GitHub is limiting update checks from your network. Try again in " + waitText(rl.RetryAt().Round(0).Sub(now.Round(0))) + "."
	}
	var st interface{ HTTPStatus() string }
	if errors.As(err, &st) {
		return "GitHub couldn’t answer right now (" + st.HTTPStatus() + "). Try again later."
	}
	var ne net.Error
	if errors.Is(err, context.DeadlineExceeded) || errors.As(err, &ne) && ne.Timeout() {
		return "GitHub didn’t answer in time. Check your internet connection and try again."
	}
	var oe *net.OpError
	var de *net.DNSError
	if errors.As(err, &oe) || errors.As(err, &de) {
		return "Couldn’t reach GitHub. Check your internet connection and try again."
	}
	return "Couldn’t check for updates: " + err.Error()
}

// waitText is a wait in minutes ("1 minute", "12 minutes", "2 hours").
func waitText(d time.Duration) string {
	m := int((d + time.Minute - 1) / time.Minute)
	switch {
	case m <= 1:
		return "a minute"
	case m < 90:
		return strconv.Itoa(m) + " minutes"
	}
	return strconv.Itoa((m+30)/60) + " hours"
}

// agoText is when something happened, relative to now.
func agoText(t, now time.Time) string {
	d := now.Round(0).Sub(t.Round(0))
	switch {
	case d < time.Minute:
		return "just now"
	case d < 2*time.Minute:
		return "a minute ago"
	case d < time.Hour:
		return fmt.Sprintf("%d minutes ago", int(d/time.Minute))
	case d < 2*time.Hour:
		return "an hour ago"
	case d < 24*time.Hour:
		return fmt.Sprintf("%d hours ago", int(d/time.Hour))
	}
	return "on " + t.Local().Format("Jan 2")
}

// updateStatus is what the Preferences row says: a line, an optional
// second (wrapped) one, its buttons, and whether Check now is clickable.
type updateStatus struct {
	msg, detail string
	muted       bool     // msg is a quiet note, not news
	buttons     []string // the first is primary, unless quiet
	quiet       bool     // no primary button (Cancel alone)
	canCheck    bool
	// bar: a progress bar under the line, filled frac (0..1), or
	// indeterminate when frac < 0
	bar  bool
	frac float32
}

// updateStatusAt is the row's state at now (UI goroutine).
func (a *App) updateStatusAt(now time.Time) updateStatus {
	u := &a.update
	c := &u.check
	if c.checking {
		return updateStatus{msg: "Checking…"}
	}
	s := updateStatus{canCheck: true}
	if u.info.Tag != "" {
		tag := u.info.Tag
		switch u.phase {
		case updAvailable:
			s.msg = AppName + " " + tag + " is available"
			dl := "Download"
			if u.dlErr != "" {
				s.msg, s.detail, dl = "Couldn’t download "+AppName+" "+tag, "The download didn’t work: "+u.dlErr, "Retry"
			}
			if a.opts.DownloadUpdate != nil {
				s.buttons = []string{dl}
			}
			s.buttons = append(s.buttons, "View release")
			return s
		case updDownloading:
			s.msg = updateProgressText(tag, u.prog)
			s.bar, s.frac = true, u.prog.frac()
			s.buttons, s.quiet = []string{"Cancel"}, true
			return s
		case updReady:
			s.msg = AppName + " " + tag + " is ready to install"
			s.buttons = []string{"Install and restart"}
			return s
		case updInstalling:
			return updateStatus{msg: "Installing " + AppName + " " + tag + "…", bar: true, frac: -1}
		case updManual:
			s.msg = "Finish installing " + AppName + " " + tag + " in the window that opened, then restart " + AppName
			return s
		case updFailed:
			s.msg = "Couldn’t install " + AppName + " " + tag
			s.detail = u.err
			s.buttons = []string{"View release"}
			return s
		}
	}
	switch {
	case c.err != "":
		s.msg, s.detail = "Couldn’t check for updates", c.err
	case c.upToDate:
		s.msg = "You’re up to date · checked " + agoText(c.at, now)
	case a.prefs.UpdateCheckedAt > 0:
		s.msg, s.muted = "Last checked "+agoText(time.Unix(a.prefs.UpdateCheckedAt, 0), now), true
	}
	return s
}

// updateRowClick runs a status button (UI goroutine).
func (a *App) updateRowClick(label string) {
	u := &a.update
	switch label {
	case "Download", "Retry":
		u.manual = true
		a.startUpdateDownload(true)
	case "Cancel":
		a.cancelUpdateDownload()
	case "View release":
		openURL(u.info.URL)
	case "Install and restart":
		a.installUpdate()
	}
	a.Invalidate()
}

// updatesSection is Preferences > Advanced > Updates.
func (p *prefsModal) updatesSection(f *prefsForm) {
	a := p.a
	f.subheading("Updates")
	f.option("updates", "Check for updates", "Looks for a new Clack release on GitHub every hour.", a.updateChecksOn(), false, a.toggleUpdateChecks)
	f.optionAt("update_download", "Download updates automatically", "Fetched in the background and checked, then Help shows a dot. Installs only when you choose Restart Clack.", a.updateDownloadsOn(), false, a.updateChecksOn(), a.toggleUpdateDownloads)
	if buildVersion != "" {
		f.note("You have Clack "+buildVersion, true, 1)
	}
	if a.opts.CheckUpdate == nil {
		return // dev builds: no checks
	}
	p.updateCheckRow(f, a.updateStatusAt(time.Now()))
}

// updateCheckRow is "Check now" with the status beside it, then the
// status's second line and buttons (Download, Install and restart, View
// release) below.
func (p *prefsModal) updateCheckRow(f *prefsForm, st updateStatus) {
	gtx, a := f.gtx, p.a
	c := &a.th.C
	x := gtx.Dp(28)
	bh := gtx.Dp(28)
	b := p.btn("update_check")
	kind := popups.BtnOutline
	if !st.canCheck {
		kind = popups.BtnDisabled
	}
	hit := b.Clicked(gtx) // popups.Button lays the clickable out: read it first
	d := at(gtx, image.Pt(x, f.y), image.Pt(f.w-x, bh), func(gtx layout.Context) layout.Dimensions {
		return popups.Button(gtx, a.th, b, kind, popups.BtnSmall, "Check now", "", 0)
	})
	if hit && st.canCheck {
		a.checkForUpdates(false)
	}
	if st.msg != "" {
		tx := x + d.Size.X + gtx.Dp(12)
		col, wt := c.Fg, theme.Bold
		if st.muted {
			col, wt = c.FgMuted, theme.Regular
		}
		at(gtx, image.Pt(tx, f.y), image.Pt(max(0, f.w-tx), bh), func(gtx layout.Context) layout.Dimensions {
			return vcenter(gtx, bh, func(gtx layout.Context) layout.Dimensions {
				return a.th.Label(gtx, 13, wt, col, st.msg)
			})
		})
	}
	f.y += bh + gtx.Dp(8)
	if st.bar {
		// a thin bar under the line, as wide as the row allows (the
		// composer's upload bar: 4dp, the track the text colour at 15%)
		h := max(gtx.Dp(4), 2)
		w := min(f.w-2*x, gtx.Dp(360))
		if w > 0 {
			off := op.Offset(image.Pt(x, f.y)).Push(gtx.Ops)
			theme.ProgressBar(gtx, image.Rect(0, 0, w, h), c.Fg, c.Link, st.frac)
			off.Pop()
		}
		f.y += h + gtx.Dp(10)
	}
	if st.detail != "" {
		f.note(st.detail, true, 2)
	}
	if len(st.buttons) > 0 {
		bx := x
		for i, label := range st.buttons {
			k := popups.BtnOutline
			if i == 0 && !st.quiet {
				k = popups.BtnPrimary
			}
			b := p.btn("update_action." + label)
			hit := b.Clicked(gtx)
			d := at(gtx, image.Pt(bx, f.y), image.Pt(max(0, f.w-bx), bh), func(gtx layout.Context) layout.Dimensions {
				return popups.Button(gtx, a.th, b, k, popups.BtnSmall, label, "", 0)
			})
			bx += d.Size.X + gtx.Dp(8)
			if hit {
				a.updateRowClick(label)
			}
		}
		f.y += bh + gtx.Dp(8)
	}
}

// menuCheckForUpdates is the workspace menu's update item on Linux and
// Windows (macOS has it in the menu bar), nil without update checks. It
// does what its title says (updateMenuItem).
func (a *App) menuCheckForUpdates() func() {
	return a.menuCheckForUpdatesOn(runtime.GOOS)
}

func (a *App) menuCheckForUpdatesOn(goos string) func() {
	if goos == "darwin" || a.opts.CheckUpdate == nil {
		return nil
	}
	return a.updateMenuClick
}
