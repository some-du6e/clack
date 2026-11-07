// Package huddleui is Clack's huddle UI: the huddle bar at the bottom of the
// sidebar, the channel header's huddle button, and the huddle window (a
// second native window with participant tiles, speaking rings, screen
// shares and video, captions and the call controls). The media session
// itself is internal/huddle.
//
// Like Slack, a start or join runs the startEventLog thunk, leaving runs
// stopHuddle and stopEventLog, and notices go through enqueueHuddleToast,
// so Taut plugins that patch them (HuddleActivity) see the same calls.
package huddleui

import (
	"context"
	"encoding/json"
	"errors"
	"log"
	"sort"
	"sync"
	"time"

	"github.com/some-du6e/yoloslackgoclient/internal/core"
	"github.com/some-du6e/yoloslackgoclient/internal/huddle"
	"github.com/some-du6e/yoloslackgoclient/internal/imgcache"
	"github.com/some-du6e/yoloslackgoclient/internal/slack"
	"github.com/some-du6e/yoloslackgoclient/internal/taut"
	"github.com/some-du6e/yoloslackgoclient/internal/ui/theme"
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

	mu      sync.Mutex
	s       *huddle.Session
	joining string // channel being joined
	live    map[string]liveRoom
	seeded  bool
	gen     uint64 // bumps on every change, for cheap redraw checks

	win  *window
	bar  barState
	hdr  map[string]*headerState
	last time.Time
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
func (c *Controller) toast(text, icon string) {
	tt := c.taut()
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_, _ = taut.Dispatch(ctx, tt, "enqueueHuddleToast",
			toastArgs{Content: text, SVGIconProps: map[string]any{"name": icon}, Duration: 5000},
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
		_, err := taut.Dispatch(ctx, tt, "startEventLog", huddleArgs{ChannelID: channel},
			func(ctx context.Context, _ huddleArgs) (any, error) {
				var err error
				s, err = huddle.Join(ctx, c.options(app), channel)
				return nil, err
			})
		if err == nil && s == nil { // a plugin swallowed the start
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
			c.toast("Couldn't join the huddle: "+errText(err), "headphones")
			c.changed()
			return
		}
		if tt != nil {
			tt.SetHuddleReaction(func(emoji, kind, url string) bool { return s.React(emoji, url) == nil })
			tt.SetActiveHuddle(s.RoomID(), channel, s.SelfAttendee(), c.selfID())
		}
		c.changed()
	}()
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
	return huddle.Options{
		Client:   client,
		SelfID:   c.selfID(),
		Logf:     log.Printf,
		OnChange: c.onSession,
		OnPeer: func(ev huddle.PeerEvent) {
			if tt := c.taut(); tt != nil {
				tt.HuddlePeerMessage(ev)
			}
			if ev.Message != nil && ev.Message.MuteRequest != nil && ev.Message.MuteRequest.PeerID == ev.SelfAttendeeID {
				name := "Someone"
				if s := c.Session(); s != nil {
					if u := c.userName(s.UserOf(ev.SenderAttendeeID)); u != "" {
						name = u
					}
				}
				c.toast(name+" muted you", "microphone-off")
			}
		},
		OnEnd: func(err error) { go c.ended(err) },
	}
}

// onSession runs on session goroutines when the huddle's state changed.
func (c *Controller) onSession() {
	s := c.Session()
	if s != nil {
		if tt := c.taut(); tt != nil {
			st := s.State()
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
	if main {
		c.h.Invalidate()
	}
}

// Leave leaves the huddle you're in.
func (c *Controller) Leave() {
	if s := c.Session(); s != nil {
		go c.leave(s)
	}
}

func (c *Controller) leave(s *huddle.Session) {
	tt := c.taut()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_, _ = taut.Dispatch(ctx, tt, "stopHuddle", huddleArgs{HuddleID: s.RoomID()},
		func(ctx context.Context, _ huddleArgs) (any, error) {
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
	if w != nil {
		w.close()
	}
	tt := c.taut()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_, _ = taut.Dispatch(ctx, tt, "stopEventLog", huddleArgs{HuddleID: s.RoomID()},
		func(ctx context.Context, _ huddleArgs) (any, error) { return nil, nil })
	if tt != nil {
		tt.SetActiveHuddle("", "", "", "")
		tt.SetHuddleReaction(nil)
	}
	switch {
	case errors.Is(err, huddle.ErrEnded):
		c.toast("The huddle ended", "headphones")
	case err != nil:
		c.toast("You were disconnected from the huddle", "headphones")
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
	if s := c.Session(); s != nil {
		s.HandleRoomEvent(ev.Name, ev.Raw)
	}
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

// orderedParticipants is the state's list with speakers and you stable.
func orderedParticipants(st huddle.State) []huddle.Participant {
	ps := append([]huddle.Participant(nil), st.Participants...)
	sort.SliceStable(ps, func(i, j int) bool { return ps[i].Self && !ps[j].Self })
	return ps
}
