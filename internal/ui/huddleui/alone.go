package huddleui

import (
	"sync"
	"time"

	"gioui.org/layout"
	"gioui.org/op"

	"github.com/some-du6e/clack/internal/huddle"
	"github.com/some-du6e/clack/internal/ui/popups"
	"github.com/some-du6e/clack/internal/ui/theme"
)

// The alone timeout (huddles_alone_timeout), ported from client-boot M3Ec
// and iKHp (build 4d0c78085232):
//
//   - HuddleAloneTimeoutTimer starts when you join (onSelfJoined), checks
//     every 5 s and whenever someone joins or leaves (onPeerJoined,
//     onPeerLeft); checkHuddleAloneTimeout does nothing unless the
//     experiment is on. Alone means no remote peers (b0mi Sx, S6Jx's
//     getRemotePeers).
//   - getHuddleAloneTimeoutStep (iKHp $): not alone clears the state;
//     alone starts aloneSinceMs; 10 minutes alone (30 s under
//     huddles_alone_timeout_shortened) prompts with a deadline 60 s out
//     (15 s shortened); at the deadline, with the prompt showing, you
//     leave; a prompt that isn't showing (or shows in another window)
//     shows again.
//   - HuddleStillHereModal: "Are you still there?", "You’ve been in this
//     huddle by yourself for a while. You’ll leave it in {seconds} …
//     unless you’re still here.", counting down every second; "I’m Still
//     Here" (and closing it) starts the alone time again, "Leave Huddle"
//     leaves (leaveHuddleForAloneTimeout → stopHuddle). It closes itself
//     once the deadline is gone (someone joined).

type aloneStepKind int

const (
	aloneNone aloneStepKind = iota
	aloneUpdate
	alonePrompt
	aloneLeave
)

// aloneState is Slack's {aloneSinceMs, deadlineMs, promptWindowId}.
type aloneState struct {
	since, deadline time.Time
	promptWin       string
}

// aloneTimeoutStep is getHuddleAloneTimeoutStep.
func aloneTimeoutStep(cur aloneState, isAlone, promptShown bool, promptWin string, now time.Time, threshold, response time.Duration) (aloneStepKind, aloneState) {
	if !isAlone {
		if !cur.since.IsZero() || !cur.deadline.IsZero() {
			return aloneUpdate, aloneState{}
		}
		return aloneNone, cur
	}
	if cur.since.IsZero() {
		return aloneUpdate, aloneState{since: now}
	}
	prompt := aloneState{since: cur.since, deadline: now.Add(response), promptWin: promptWin}
	if cur.deadline.IsZero() {
		if now.Sub(cur.since) >= threshold {
			return alonePrompt, prompt
		}
		return aloneNone, cur
	}
	if !now.Before(cur.deadline) {
		if promptShown {
			return aloneLeave, cur
		}
		return alonePrompt, prompt
	}
	if promptShown || cur.promptWin == promptWin {
		return aloneNone, cur
	}
	next := cur
	next.promptWin = promptWin
	return alonePrompt, next
}

// aloneTimer is the controller's HuddleAloneTimeoutTimer and the client
// store's aloneTimeout slice.
type aloneTimer struct {
	mu     sync.Mutex
	st     aloneState
	shown  bool   // isPromptShown (the modal is mounted)
	modal  string // the window it shows in ("" = none)
	stop   chan struct{}
	s      *huddle.Session
	closer func() // closes the open modal
}

// aloneEvery is the timer's interval (setInterval 5e3; tests shorten it).
var aloneEvery = 5 * time.Second

// aloneThresholds are the thresholds checkHuddleAloneTimeout passes: 10
// minutes alone and 60 s to answer, or 30 s and 15 s shortened (tests
// shorten them further).
var aloneThresholds = func(shortened bool) (threshold, response time.Duration) {
	if shortened {
		return 30 * time.Second, 15 * time.Second
	}
	return 10 * time.Minute, 60 * time.Second
}

// startAloneTimer is HuddleAloneTimeoutTimer.start (you joined).
func (c *Controller) startAloneTimer(s *huddle.Session) {
	a := &c.alone
	a.mu.Lock()
	if a.stop != nil {
		close(a.stop)
	}
	stop := make(chan struct{})
	a.stop, a.s, a.st = stop, s, aloneState{}
	a.mu.Unlock()
	go func() {
		t := time.NewTicker(aloneEvery)
		defer t.Stop()
		c.checkAlone()
		for {
			select {
			case <-stop:
				return
			case <-s.Done():
				return
			case <-t.C:
				c.checkAlone()
			}
		}
	}()
}

// stopAloneTimer is HuddleAloneTimeoutTimer.stop (the huddle is over).
func (c *Controller) stopAloneTimer() {
	a := &c.alone
	a.mu.Lock()
	if a.stop != nil {
		close(a.stop)
	}
	a.stop, a.s, a.st = nil, nil, aloneState{}
	closer := a.closer
	a.closer = nil
	a.mu.Unlock()
	if closer != nil {
		closer()
	}
}

// aloneCount tracks the remote peers between checks (onPeerJoined /
// onPeerLeft check at once).
func remotePeers(st huddle.State) int {
	n := 0
	for _, p := range st.Participants {
		if !p.Self && p.InMedia {
			n++
		}
	}
	return n
}

// checkAlone is checkHuddleAloneTimeout.
func (c *Controller) checkAlone() {
	if !c.on(ExpAloneTimeout) {
		return
	}
	a := &c.alone
	a.mu.Lock()
	s := a.s
	a.mu.Unlock()
	if s == nil || s.State().Phase == huddle.Ended {
		return
	}
	st := s.State()
	threshold, response := aloneThresholds(c.on(ExpAloneShortened))
	win := "main"
	if c.WindowOpen() {
		win = "huddle" // getHuddleStillHereModalWindowId: the huddle window when it's open
	}
	a.mu.Lock()
	kind, next := aloneTimeoutStep(a.st, remotePeers(st) == 0, a.shown, win, time.Now(), threshold, response)
	switch kind {
	case aloneUpdate:
		a.st = next
	case alonePrompt:
		a.st = next
	}
	closer := a.closer
	if kind == aloneUpdate && next.deadline.IsZero() && closer != nil {
		a.closer = nil // the modal closes once the deadline is gone
	} else {
		closer = nil
	}
	a.mu.Unlock()
	switch kind {
	case aloneUpdate:
		if closer != nil {
			closer()
		}
	case alonePrompt:
		c.showStillHere(win)
	case aloneLeave:
		c.leaveAlone(s)
	}
}

// leaveAlone is leaveHuddleForAloneTimeout: the state reset, then
// stopHuddle for the huddle you're in.
func (c *Controller) leaveAlone(s *huddle.Session) {
	a := &c.alone
	a.mu.Lock()
	a.st = aloneState{}
	closer := a.closer
	a.closer = nil
	a.mu.Unlock()
	if closer != nil {
		closer()
	}
	if cur := c.Session(); cur == s && s != nil && s.State().Phase != huddle.Ended {
		go c.leave(s)
	}
}

// stillHereText is the modal's body for the seconds left.
func stillHereText(sec int) string {
	return nsHuddles.T("You’ve been in this huddle by yourself for a while. You’ll leave it in {seconds, plural, =1 {# second} other {# seconds}} unless you’re still here.", "seconds", sec) // HuddleStillHereModal
}

// showStillHere opens HuddleStillHereModal in win's window (the huddle
// window when it's open, else the main window).
func (c *Controller) showStillHere(win string) {
	a := &c.alone
	a.mu.Lock()
	if a.closer != nil && a.modal == win {
		a.mu.Unlock()
		return
	}
	old := a.closer
	a.closer = nil
	a.mu.Unlock()
	if old != nil {
		old()
	}
	var ph popups.Host
	run := c.post // where the modal is made and closed: the window's goroutine
	if win == "huddle" {
		c.mu.Lock()
		w := c.win
		c.mu.Unlock()
		if w != nil {
			wh := winHost{w}
			ph, run = wh, wh.post
		}
	}
	if ph == nil {
		h, ok := c.h.(popups.Host)
		if !ok {
			return
		}
		ph, win = h, "main"
	}
	var m *popups.Modal
	left, auto := false, false // closed by Leave Huddle, or because the deadline went away
	leave := &popups.ModalButton{Label: nsHuddles.T("Leave Huddle"), Kind: popups.BtnOutline, Do: func() bool {
		left = true
		a.mu.Lock()
		s := a.s
		a.mu.Unlock()
		c.leaveAlone(s)
		return true
	}}
	// I’m Still Here, Escape, the close button and the backdrop are all
	// Slack's onSubmit / onClose: the alone time starts again
	here := &popups.ModalButton{Label: nsHuddles.T("I’m Still Here"), Kind: popups.BtnPrimary, Do: func() bool { return true }}
	a.mu.Lock()
	a.modal = win
	a.closer = func() {
		run(func() {
			auto = true
			if m != nil {
				m.Dismiss()
			}
		})
	}
	a.mu.Unlock()
	run(func() {
		th := ph.Theme()
		a.mu.Lock()
		a.shown = true // isPromptShown: set as the modal mounts (ni)
		a.mu.Unlock()
		m = popups.NewModal(ph, popups.ModalOpts{
			Title:     nsHuddles.T("Are you still there?"),
			Class:     "huddle_still_here_modal",
			AutoFocus: here,
			OnClose: func() {
				a.mu.Lock()
				a.shown = false
				if a.modal == win {
					a.modal, a.closer = "", nil
				}
				if !left && !auto {
					a.st = aloneState{since: time.Now()} // {aloneSinceMs: Date.now(), deadlineMs: null}
				}
				a.mu.Unlock()
			},
			Body: func(gtx layout.Context) layout.Dimensions {
				a.mu.Lock()
				dl := a.st.deadline
				a.mu.Unlock()
				sec := 0
				if !dl.IsZero() {
					left := time.Until(dl)
					sec = max(0, int((left+time.Second-1)/time.Second)) // Math.ceil
					next := left % time.Second
					if next <= 0 {
						next = time.Second
					}
					gtx.Execute(op.InvalidateCmd{At: time.Now().Add(next)})
				}
				return th.Para(gtx, 15, 22, theme.Regular, th.C.Fg, stillHereText(sec))
			},
			Buttons: []*popups.ModalButton{leave, here},
		})
		ph.ShowOverlay(m)
		ph.Invalidate()
	})
}
