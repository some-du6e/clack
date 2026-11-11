// Package huddleui is Clack's huddle UI: the huddle bar at the bottom of the
// sidebar, the channel header's huddle button, and the huddle window (a
// second native window with participant tiles drawn like Slack's
// HuddlePeerTile (tile.go), screen shares and video, captions and the call
// controls). The media session itself is internal/huddle.
//
// Like Slack, a start or join runs the startHuddle thunk, leaving runs
// stopHuddle (which runs stopEventLog first), turning captions on and off
// runs startEventLog / stopEventLog (the transcription), and notices go
// through enqueueHuddleToast, so Taut plugins that patch them
// (BetterHuddles, HuddleActivity) see the same calls.
package huddleui

import (
	"context"
	"encoding/json"
	"errors"
	"image"
	"log"
	"math"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/some-du6e/clack/internal/core"
	"github.com/some-du6e/clack/internal/huddle"
	"github.com/some-du6e/clack/internal/imgcache"
	"github.com/some-du6e/clack/internal/slack"
	"github.com/some-du6e/clack/internal/taut"
	"github.com/some-du6e/clack/internal/ui/popups"
	"github.com/some-du6e/clack/internal/ui/theme"
)

// Host is what the huddle UI needs from the ui root (names follow the
// shared Host convention in docs/NATIVE.md).
type Host interface {
	Core() *core.App
	Theme() *theme.Theme
	Images() *imgcache.Cache
	Invalidate()
	Navigate(route string)
	OpenThread(channel, ts string)
	Toast(text string)
}

type tautHost interface{ Taut() *taut.Taut }

// liveRoom is a huddle running in a conversation (from sh_room_* events
// and edge huddles/list), for the header button and the sidebar badge.
type liveRoom struct {
	ID           string
	Channel      string
	Participants []string
	Start        int64
}

// Controller owns the huddle you're in. Its exported methods run on the UI
// goroutine unless noted.
type Controller struct {
	h Host

	// Client and SelfID override core's Slack client and user (the fake
	// Chime demo, tests). Set before the first Start.
	Client huddle.Caller
	SelfID string
	// Tweak edits the session options before each join (the demo fakes
	// microphone permission answers with it).
	Tweak func(*huddle.Options)
	// NoWindow keeps the huddle window shut (tests: no display).
	NoWindow bool
	// Ring plays one of Slack's sounds by file name (the
	// huddle_invite_sound_v2 pref's value); nil plays it on the default
	// speakers (ring.go). Tests record it.
	Ring func(file string)
	// MissedAfter is how long an invite rings before the "Huddle missed"
	// notice (0: Slack's 30 s).
	MissedAfter time.Duration

	mu      sync.Mutex
	s       *huddle.Session
	joining string // channel being joined
	live    map[string]liveRoom
	seeded  bool
	gen     uint64 // bumps on every change, for cheap redraw checks

	win  *window
	inv  *incoming      // the incoming invite (invite.go)
	iwin *inviteWindow  // its window (invitewin.go)
	pwin *previewWindow // the join preview (preview.go)
	// pending is the preview's Join: the mic and camera to start with
	pending *pendingJoin
	multi   *pendingMulti // HuddleMultiDeviceModal's answer (multidevice.go)
	card    cardState     // its sidebar card (invitecard.go)
	ring    ringState     // the invite sound (ring.go)
	hand    handState     // auto-lowering your raised hand (hand.go)
	av      avState       // the lock watcher, global mute and lounge music (avprefs.go)
	bar     barState
	dock    dockState // the docked bar (dock.go)
	hdr     map[string]*headerState
	last    time.Time

	dockOrigin image.Point // the dock's window position (menus anchor to it)
	hideSelf   atomic.Bool // the ⋯ menu's "Hide self-view" (isHidingSelf)

	alone  aloneTimer // huddles_alone_timeout (alone.go)
	tick   sessionTick
	mic    micAlerts   // the mic button's permission, NoAudioDetected, "You’re muted." (micalerts.go)
	popout popoutState // huddles_screenshare_popout (popout.go)
}

// sessionTick is what onSession compares between changes.
type sessionTick struct {
	s         *huddle.Session
	connected bool
	remote    int
}

// New builds the controller. Call Handle with core events.
func New(h Host) *Controller {
	return &Controller{h: h, live: map[string]liveRoom{}, hdr: map[string]*headerState{}}
}

func (c *Controller) taut() *taut.Taut {
	if th, ok := c.h.(tautHost); ok {
		return th.Taut()
	}
	return nil
}

// Session is the huddle you're in (nil if none). Safe from any goroutine.
func (c *Controller) Session() *huddle.Session {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.s
}

// In reports whether you're in (or joining) the huddle in channel.
func (c *Controller) In(channel string) bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	return (c.s != nil && c.s.Channel() == channel) || c.joining == channel
}

// Active reports whether you're in or joining any huddle.
func (c *Controller) Active() bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.s != nil || c.joining != ""
}

// Live returns the huddle running in channel, if any.
func (c *Controller) Live(channel string) (liveRoom, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	r, ok := c.live[channel]
	return r, ok && len(r.Participants) > 0
}

// LiveCount is the number of people in channel's huddle (0 = none), for the
// sidebar's headphones badge.
func (c *Controller) LiveCount(channel string) int {
	r, ok := c.Live(channel)
	if !ok {
		return 0
	}
	return len(r.Participants)
}

func (c *Controller) changed() {
	c.mu.Lock()
	c.gen++
	w := c.win
	c.mu.Unlock()
	c.h.Invalidate()
	if w != nil {
		w.invalidate()
	}
	c.invalidatePopouts()
}

// invalidatePopouts redraws the screen share popouts (popout.go).
func (c *Controller) invalidatePopouts() {
	c.popout.mu.Lock()
	ws := make([]*window, 0, len(c.popout.wins))
	for _, w := range c.popout.wins {
		ws = append(ws, w)
	}
	c.popout.mu.Unlock()
	for _, w := range ws {
		w.invalidate()
	}
}

type huddleArgs struct {
	HuddleID  string `json:"huddleId,omitempty"`
	ChannelID string `json:"channelId,omitempty"`
}

type toastArgs struct {
	Content      string         `json:"content"`
	SVGIconProps map[string]any `json:"svgIconProps,omitempty"`
	Duration     int            `json:"duration,omitempty"`
}

// toast shows a huddle notice through Slack's enqueueHuddleToast.
func (c *Controller) toast(text, icon string) { c.toastFor(text, icon, 5000) }

// toastFor is toast with Slack's duration (ms); icon "" shows none.
func (c *Controller) toastFor(text, icon string, ms int) {
	tt := c.taut()
	args := toastArgs{Content: text, Duration: ms}
	if icon != "" {
		args.SVGIconProps = map[string]any{"name": icon}
	}
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_, _ = taut.Dispatch(ctx, tt, "enqueueHuddleToast", args,
			func(ctx context.Context, a toastArgs) (any, error) {
				c.h.Toast(a.Content)
				return nil, nil
			})
	}()
}

// Start starts or joins the huddle in channel (a channel or DM id). If
// you're in another huddle you leave it first, like Slack.
func (c *Controller) Start(channel string) {
	c.mu.Lock()
	if c.joining != "" || (c.s != nil && c.s.Channel() == channel) {
		c.mu.Unlock()
		c.OpenWindow()
		return
	}
	old := c.s
	c.joining = channel
	c.mu.Unlock()
	c.changed()
	app := c.h.Core()
	if app == nil {
		c.mu.Lock()
		c.joining = ""
		c.mu.Unlock()
		return
	}
	tt := c.taut()
	go func() {
		if old != nil {
			c.leave(old)
		}
		c.OpenWindow()
		ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
		defer cancel()
		var s *huddle.Session
		o := c.options(app)
		c.mu.Lock()
		pj := c.pending
		c.pending = nil
		c.mu.Unlock()
		if pj != nil && pj.channel != channel {
			pj = nil
		}
		if pj != nil { // the join preview's mic toggle
			o.Muted = pj.muted
		}
		c.mu.Lock()
		if pm := c.multi; pm != nil && pm.channel == channel { // "Use both devices"
			o.MultiDevice = pm.multi
		}
		c.multi = nil
		c.mu.Unlock()
		// startHuddle's media start (its thunk is Begin's, for plugins);
		// Slack's startEventLog is the captions' transcription, not this
		s, err := huddle.Join(ctx, o, channel)
		if err == nil && s == nil {
			err = errors.New("the huddle didn't start")
		}
		c.mu.Lock()
		c.joining = ""
		if err == nil {
			c.s = s
		}
		c.mu.Unlock()
		if err != nil {
			c.mu.Lock()
			w := c.win
			c.win = nil
			c.mu.Unlock()
			if w != nil {
				w.close()
			}
			log.Printf("huddle: join %s: %v", channel, err)
			c.showErrModal(joinErrorModal(err)) // HuddleErrorModal{isJoinError}
			c.changed()
			return
		}
		if tt != nil {
			tt.SetHuddleReaction(func(emoji, kind, url string) bool { return s.React(emoji, url) == nil })
			tt.SetActiveHuddle(s.RoomID(), channel, s.SelfAttendee(), c.selfID())
		}
		c.sessionStarted(s)
		if pj != nil && pj.camera { // the preview's camera stays on
			s.SetCamera(true)
		}
		c.changed()
	}()
}

// Slack's huddle entry points (DF8l's su enum), passed as startHuddle's
// entryPoint.
const (
	EntryHeaderButton  = "channel_header_huddle_button"
	EntryHeaderMenu    = "channel_header_huddle_menu"
	EntryMemberCard    = "member_card_huddle_button"
	EntryMemberProfile = "member_profile_huddle_button"
	EntryShortcut      = "keyboard_shortcut_join_huddle"
	EntryCreateMenu    = "global_create_menu"
	EntryHuddlesPage   = "huddles_page_start_huddle"
)

// startArgs is Slack's startHuddle thunk argument (what BetterHuddles
// reads: channelId, muted, entryPoint).
type startArgs struct {
	ChannelID     string `json:"channelId,omitempty"`
	Muted         bool   `json:"muted"`
	EntryPoint    string `json:"entryPoint,omitempty"`
	IsMultiDevice bool   `json:"isMultiDevice,omitempty"`
}

// Begin is a start or join click (or shortcut, or /huddle) in channel:
// Slack's startHuddle thunk, so a plugin that wraps it (BetterHuddles)
// sees every start, with Start as the native. Without a wrapper it's
// Start. Any goroutine.
func (c *Controller) Begin(channel, entry string) {
	if channel == "" || !c.SupportsHuddles(channel) {
		return
	}
	// startHuddleWithShoutyRooster: the warning first, then the join
	// preview (huddles_show_join_preview), then startHuddle
	start := func() {
		if c.shouldPreview(channel, entry) {
			c.openPreview(channel, entry)
			return
		}
		c.begin(channel, entry)
	}
	// startHuddleWithConfirmation: in a huddle you're in on another device,
	// huddles_multi_device asks first, and its answer skips the preview
	// (overrideJoinPreview) and sets isMultiDevice
	if devices, others := c.huddlingElsewhere(channel); devices > 0 && c.group(ExpMultiDevice, "treatment") {
		c.multiDeviceModal(devices, others, func(multi bool) {
			c.mu.Lock()
			c.multi = &pendingMulti{channel: channel, multi: multi}
			c.mu.Unlock()
			join := func() { c.begin(channel, entry) }
			if !c.shoutyRooster(channel, join) {
				join()
			}
		})
		return
	}
	if c.shoutyRooster(channel, start) {
		return
	}
	start()
}

// shoutyRoosterMembers is the member count over which starting a huddle
// asks first (shouldShowHuddlesShoutyRoosterWarning: !(count <= 150)).
const shoutyRoosterMembers = 150

// shoutyRooster is shouldShowHuddlesShoutyRoosterWarning and
// HuddleShoutyRoosterModal: starting (not joining) a huddle in a channel
// of more than 150 members, with huddles_show_shouty_rooster not false
// (Audio & video > "Send a warning if you’re starting a huddle in a
// channel with more than 150 members"), asks "Start a huddle in this
// channel?" first. It reports whether it asked; OK runs start.
func (c *Controller) shoutyRooster(channel string, start func()) bool {
	app := c.h.Core()
	ph, ok := c.h.(popups.Host)
	if app == nil || !ok {
		return false
	}
	if _, live := c.Live(channel); live {
		return false
	}
	if s := c.Session(); s != nil && s.Channel() == channel {
		return false
	}
	cv, ok := app.Conv(channel)
	if !ok || cv.Kind == "im" || cv.Kind == "mpim" || cv.NumMembers <= shoutyRoosterMembers || !app.PrefBool("huddles_show_shouty_rooster", true) {
		return false
	}
	ShoutyRoosterModal(ph, cv, start)
	return true
}

// ShoutyRoosterModal is "Start a huddle in this channel?": "You’ll be
// starting a huddle that all N members of #channel can join.", "Don’t
// show this message again." (checked = !huddles_show_shouty_rooster,
// written on change), Cancel / OK.
func ShoutyRoosterModal(h popups.Host, cv core.Conv, start func()) {
	body := nsHuddles.T("You’ll be starting a huddle that every member of {channel} can join.", "channel", "#"+cv.Name)
	if cv.NumMembers > 1 {
		body = nsHuddles.T("You’ll be starting a huddle that all {count} members of {channel} can join.", "count", cv.NumMembers, "channel", "#"+cv.Name)
	}
	popups.ConfirmPrefModal(h, nsHuddles.T("Start a huddle in this channel?"), body, nsHuddles.T("Cancel"), "OK",
		nsHuddles.T("Don’t show this message again."), "huddles_show_shouty_rooster", start)
}

func (c *Controller) begin(channel, entry string) {
	tt := c.taut()
	if !tt.ThunkPatched("startHuddle") {
		c.Start(channel)
		return
	}
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		c.mu.Lock()
		multi := c.multi != nil && c.multi.channel == channel && c.multi.multi
		c.mu.Unlock()
		_, err := taut.Dispatch(ctx, tt, "startHuddle", startArgs{ChannelID: channel, EntryPoint: entry, IsMultiDevice: multi},
			func(ctx context.Context, a startArgs) (any, error) {
				if a.ChannelID != "" {
					c.post(func() { c.Start(a.ChannelID) })
				}
				return nil, nil
			})
		if err != nil {
			log.Printf("huddle: startHuddle %s: %v", channel, err)
			c.showErrModal(joinErrorModal(err))
		}
	}()
}

// ShortcutToggle is Slack's Cmd/Ctrl+Shift+H (toggleHuddleInCurrentView, "Toggle
// huddles start / stop"): leave the huddle you're in, else start or join
// the one in channel (the conversation on screen; nothing without one).
func (c *Controller) ShortcutToggle(channel string) {
	if c.Active() {
		c.mu.Lock()
		joining := c.s == nil
		c.mu.Unlock()
		if !joining {
			c.Leave()
		}
		return
	}
	c.Begin(channel, EntryShortcut)
}

// post runs f on the ui root's goroutine when it has one (Post), else now.
func (c *Controller) post(f func()) {
	if p, ok := c.h.(interface{ Post(func()) }); ok {
		p.Post(f)
		return
	}
	f()
}

func errText(err error) string {
	var se *slack.Error
	if errors.As(err, &se) {
		return se.Code
	}
	return err.Error()
}

func (c *Controller) options(app *core.App) huddle.Options {
	var client huddle.Caller = app.C
	if c.Client != nil {
		client = c.Client
	}
	agc, ns := c.audioPrefs()
	o := huddle.Options{
		Client: client,
		SelfID: c.selfID(),
		// Audio & video > When joining a huddle… "Mute my microphone"
		Muted: app.PrefBool("huddles_mute_by_default", false),
		// "Automatically turn on captions when you’re in a huddle"
		Captions: app.PrefBool("huddles_cc_by_default", false),
		// "Enable automatic gain control" / "Enable noise suppression"
		AGC: agc, NoiseSuppression: ns,
		// "Blur your video background when you’re in a huddle with your
		// camera on" (prefs.video.effect, huddle/videofx.go)
		VideoEffect: c.videoEffect(),
		Logf:        log.Printf,
		OnChange:    c.onSession,
		OnPeer: func(ev huddle.PeerEvent) {
			if tt := c.taut(); tt != nil {
				tt.HuddlePeerMessage(ev)
			}
			if ev.Message != nil && ev.Message.MuteRequest != nil && ev.Message.MuteRequest.PeerID == ev.SelfAttendeeID {
				// HuddlesInformationalBanner "user_was_muted" (microphone-slash)
				c.showBanner(nsHuddles.T("Someone muted you."), "mic-off")
			}
		},
		OnEnd: func(err error) { go c.ended(err) },
	}
	if c.Tweak != nil {
		c.Tweak(&o)
	}
	return o
}

// onSession runs on session goroutines when the huddle's state changed.
func (c *Controller) onSession() {
	s := c.Session()
	if s != nil {
		st := s.State()
		c.syncExperiments(s)
		c.peersTick(s, st)
		c.autoLowerHand(s, st)
		c.musicTick(s, st)
		if tt := c.taut(); tt != nil {
			peers := make(map[string]string, len(st.Participants))
			for _, p := range st.Participants {
				if p.AttendeeID != "" && p.InMedia {
					peers[p.AttendeeID] = p.UserID
				}
			}
			tt.SetHuddlePeers(peers)
		}
	}
	c.mu.Lock()
	c.gen++
	w := c.win
	now := time.Now()
	main := now.Sub(c.last) > 100*time.Millisecond // the sidebar bar needs ~10 Hz at most
	if main {
		c.last = now
	}
	c.mu.Unlock()
	if w != nil {
		w.invalidate()
	}
	c.invalidatePopouts()
	if main {
		c.h.Invalidate()
	}
}

// peersTick runs what Slack's HuddleActionDispatcher does on
// onSelfJoined (the alone timer starts; huddles_cc_by_default turns
// captions on) and on onPeerJoined / onPeerLeft (the alone check).
func (c *Controller) peersTick(s *huddle.Session, st huddle.State) {
	c.mu.Lock()
	t := &c.tick
	if t.s != s {
		*t = sessionTick{s: s}
	}
	joined := !t.connected && st.Phase == huddle.Connected
	if joined {
		t.connected = true
	}
	n := remotePeers(st)
	peers := n != t.remote
	t.remote = n
	c.mu.Unlock()
	if joined {
		c.startAloneTimer(s)
		c.startMicChecks(s)
		c.captionsByDefault(s)
	} else if peers {
		go c.checkAlone()
	}
}

// Leave leaves the huddle you're in.
func (c *Controller) Leave() {
	if s := c.Session(); s != nil {
		go c.leave(s)
	}
}

// leave is Slack's stopHuddle ("Stop the active huddle"): its first step
// is stopEventLog (the captions' transcription, when on), then the
// media leave.
func (c *Controller) leave(s *huddle.Session) {
	tt := c.taut()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_, _ = taut.Dispatch(ctx, tt, "stopHuddle", huddleArgs{HuddleID: s.RoomID()},
		func(ctx context.Context, _ huddleArgs) (any, error) {
			c.stopEventLog(ctx, s)
			s.Leave()
			return nil, nil
		})
	s.Leave() // a plugin that swallowed stopHuddle can't keep the media up
	c.ended(nil)
}

// ended runs once the session is over (you left, it ended, or it failed).
func (c *Controller) ended(err error) {
	c.mu.Lock()
	s := c.s
	c.s = nil
	w := c.win
	c.win = nil
	c.mu.Unlock()
	if s == nil {
		return
	}
	c.sessionEnded()
	c.stopAloneTimer()
	c.stopMicChecks()
	c.closePopouts()
	if w != nil {
		w.close()
	}
	tt := c.taut()
	if tt != nil {
		tt.SetActiveHuddle("", "", "", "")
		tt.SetHuddleReaction(nil)
	}
	if m, ok := endedModal(err); ok {
		c.showErrModal(m)
	}
	c.changed()
}

// Close leaves any huddle (app shutdown). Blocks up to ~2 s.
func (c *Controller) Close() {
	if s := c.Session(); s != nil {
		s.Leave()
	}
}

// ---- live huddles (realtime) ----

type roomEvent struct {
	User   string     `json:"user"`
	Room   *roomShape `json:"room"`
	Huddle *roomShape `json:"huddle"`
}

type roomShape struct {
	ID           string          `json:"id"`
	Channels     []string        `json:"channels"`
	ChannelID    string          `json:"channel_id"`
	Participants json.RawMessage `json:"participants"`
	HasEnded     bool            `json:"has_ended"`
	DateEnd      int64           `json:"date_end"`
	DateStart    int64           `json:"date_start"`
}

func ids(raw json.RawMessage) []string {
	var s []string
	if json.Unmarshal(raw, &s) == nil {
		return s
	}
	var o []struct {
		UserID string `json:"user_id"`
	}
	_ = json.Unmarshal(raw, &o)
	for _, x := range o {
		if x.UserID != "" {
			s = append(s, x.UserID)
		}
	}
	return s
}

// Handle takes core events (call from the event pump; any goroutine).
func (c *Controller) Handle(ev core.Event) {
	if ev.Type != "huddle" {
		return
	}
	switch ev.Name {
	case "huddle_invite":
		c.onInvite(ev.Raw)
		return
	case "huddle_invite_cancel": // huddleInviteCancelRtmHandler
		var e struct {
			Channel string `json:"channel_id"`
		}
		if json.Unmarshal(ev.Raw, &e) == nil && e.Channel != "" {
			c.clearInvite(e.Channel)
		}
		return
	}
	if !strings.HasPrefix(ev.Name, "sh_") {
		return
	}
	if s := c.Session(); s != nil {
		s.HandleRoomEvent(ev.Name, ev.Raw)
	}
	c.inviteRoomEvent(ev.Name, ev.Raw)
	var re roomEvent
	if json.Unmarshal(ev.Raw, &re) != nil {
		return
	}
	r := re.Room
	if r == nil || r.ID == "" {
		r = re.Huddle
	}
	if r == nil || r.ID == "" {
		return
	}
	ch := ev.Channel
	if len(r.Channels) > 0 {
		ch = r.Channels[0]
	} else if r.ChannelID != "" {
		ch = r.ChannelID
	}
	if ch == "" {
		return
	}
	parts := ids(r.Participants)
	c.mu.Lock()
	if r.HasEnded || r.DateEnd > 0 || (r.Participants != nil && len(parts) == 0) {
		delete(c.live, ch)
	} else {
		lr := c.live[ch]
		lr.ID, lr.Channel = r.ID, ch
		if r.DateStart > 0 {
			lr.Start = r.DateStart
		}
		if r.Participants != nil {
			lr.Participants = parts
		} else if re.User != "" {
			switch ev.Name {
			case "sh_room_join":
				if !contains(lr.Participants, re.User) {
					lr.Participants = append(lr.Participants, re.User)
				}
			case "sh_room_leave":
				lr.Participants = remove(lr.Participants, re.User)
			}
		}
		c.live[ch] = lr
	}
	c.gen++
	c.mu.Unlock()
	if app := c.h.Core(); app != nil {
		app.EnsureUsers(context.Background(), parts)
	}
	c.h.Invalidate()
}

// Seed loads the huddles running now (edge huddles/list) once.
func (c *Controller) Seed() {
	c.mu.Lock()
	if c.seeded {
		c.mu.Unlock()
		return
	}
	c.seeded = true
	c.mu.Unlock()
	app := c.h.Core()
	if app == nil {
		return
	}
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		hs, err := app.ActiveHuddles(ctx)
		if err != nil {
			return
		}
		c.mu.Lock()
		for _, h := range hs {
			if !h.Active || len(h.Channels) == 0 {
				continue
			}
			c.live[h.Channels[0]] = liveRoom{ID: h.ID, Channel: h.Channels[0], Participants: h.Participants, Start: h.Start}
		}
		c.gen++
		c.mu.Unlock()
		c.h.Invalidate()
	}()
}

func contains(l []string, s string) bool {
	for _, x := range l {
		if x == s {
			return true
		}
	}
	return false
}

func remove(l []string, s string) []string {
	out := l[:0]
	for _, x := range l {
		if x != s {
			out = append(out, x)
		}
	}
	return out
}

// ---- helpers ----

func (c *Controller) selfID() string {
	if c.SelfID != "" {
		return c.SelfID
	}
	if app := c.h.Core(); app != nil {
		return app.Self().ID
	}
	return ""
}

func (c *Controller) user(id string) (core.User, bool) {
	if app := c.h.Core(); app != nil && id != "" {
		return app.User(id)
	}
	return core.User{}, false
}

func (c *Controller) userName(id string) string {
	if u, ok := c.user(id); ok {
		if u.Name != "" {
			return u.Name
		}
		return u.RealName
	}
	return ""
}

func (c *Controller) avatarURL(id string, px int) string {
	u, ok := c.user(id)
	if !ok {
		return ""
	}
	if px > 96 && u.Avatar512 != "" {
		return u.Avatar512
	}
	if px > 48 && u.Avatar192 != "" {
		return u.Avatar192
	}
	return u.Avatar
}

// channelTitle is "#name" for channels, the person's name for DMs.
func (c *Controller) channelTitle(ch string) string {
	app := c.h.Core()
	if app == nil {
		return ch
	}
	conv, ok := app.Conv(ch)
	if !ok {
		return ch
	}
	switch conv.Kind {
	case "im":
		if n := c.userName(conv.User); n != "" {
			return n
		}
	case "mpim":
		if conv.Name != "" {
			return conv.Name
		}
	}
	if conv.Name != "" {
		return "#" + conv.Name
	}
	return ch
}

// orderedParticipants is the state's list with you first.
func orderedParticipants(st huddle.State) []huddle.Participant {
	ps := append([]huddle.Participant(nil), st.Participants...)
	sort.SliceStable(ps, func(i, j int) bool { return ps[i].Self && !ps[j].Self })
	return ps
}

// galleryOrder is the order Slack's huddle grid takes its tiles in
// (gantry-v2-async-client-boot-shared S5fM): you, then raised hands,
// people sharing their screen, cameras on, then everyone else (each once,
// at its first place). Raised hands go by when they went up: the
// stickyEmoji entries sorted by field_timestamp_ms (OGIR xP: an entry
// without one, from a client without huddles_awareness_timestamp, sorts
// last), or under huddles_raised_hand_queue by the queue (R6cm:
// field_timestamp_ms, else when Clack received it).
func galleryOrder(st huddle.State, queue bool) []huddle.Participant {
	ps := orderedParticipants(st)
	raisedKey := func(p huddle.Participant) int64 {
		switch {
		case p.StickerTS != 0:
			return p.StickerTS
		case queue && !p.StickerAt.IsZero():
			return p.StickerAt.UnixMilli()
		}
		return math.MaxInt64
	}
	var hands []huddle.Participant
	for _, p := range ps {
		if !p.Self && isHand(p.Sticker) {
			hands = append(hands, p)
		}
	}
	sort.SliceStable(hands, func(i, j int) bool { return raisedKey(hands[i]) < raisedKey(hands[j]) })
	var out []huddle.Participant
	seen := map[string]bool{}
	add := func(p huddle.Participant) {
		k := p.UserID + "/" + p.AttendeeID
		if !seen[k] {
			seen[k] = true
			out = append(out, p)
		}
	}
	for _, p := range ps {
		if p.Self {
			add(p)
		}
	}
	for _, p := range hands {
		add(p)
	}
	for _, p := range ps {
		if p.Sharing {
			add(p)
		}
	}
	for _, p := range ps {
		if p.Camera {
			add(p)
		}
	}
	for _, p := range ps {
		add(p)
	}
	return out
}
