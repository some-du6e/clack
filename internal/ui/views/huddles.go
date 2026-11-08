package views

import (
	"context"
	"encoding/json"
	"image"
	"sort"
	"strconv"
	"sync"
	"time"

	"gioui.org/layout"
	"gioui.org/op"
	"gioui.org/widget"

	"github.com/some-du6e/yoloslackgoclient/internal/core"
	"github.com/some-du6e/yoloslackgoclient/internal/ui/slackc"
	"github.com/some-du6e/yoloslackgoclient/internal/ui/theme"
)

// huddleRoom is an active or past huddle.
type huddleRoom = core.Huddle

// rawRoom is the room object carried by sh_room_* realtime events, which
// core forwards raw (Event.Raw).
type rawRoom struct {
	ID                 string   `json:"id"`
	Name               string   `json:"name"`
	CreatedBy          string   `json:"created_by"`
	DateStart          int64    `json:"date_start"`
	DateEnd            int64    `json:"date_end"`
	Participants       []string `json:"participants"`
	ParticipantHistory []string `json:"participant_history"`
	Channels           []string `json:"channels"`
	IsDMCall           bool     `json:"is_dm_call"`
	HasEnded           bool     `json:"has_ended"`
}

// roomFromEvent decodes the room of an sh_room_* event.
func roomFromEvent(raw json.RawMessage) (huddleRoom, bool) {
	var ev struct {
		Room   *rawRoom `json:"room"`
		Huddle *rawRoom `json:"huddle"`
	}
	if json.Unmarshal(raw, &ev) != nil {
		return huddleRoom{}, false
	}
	r := ev.Room
	if r == nil {
		r = ev.Huddle
	}
	if r == nil || r.ID == "" {
		return huddleRoom{}, false
	}
	return huddleRoom{ID: r.ID, Name: r.Name, CreatedBy: r.CreatedBy, Channels: r.Channels,
		Participants: r.Participants, History: r.ParticipantHistory, Start: r.DateStart, End: r.DateEnd,
		Active: r.DateEnd == 0 && !r.HasEnded && len(r.Participants) > 0, DM: r.IsDMCall}, true
}

// joinUnsupported is shown when the user tries to start or join a huddle:
// huddle audio/video runs on Amazon Chime media servers that yoloslack
// doesn't implement.
const joinUnsupported = "Huddles need Slack's audio/video stack (Amazon Chime), which yoloslack doesn't support yet"

// Huddles is the Home-sidebar Huddles page: header, the mint hero banner,
// live huddles (from realtime sh_room_* events), recent huddles
// (huddles.history), then "Direct messages" suggestion tiles and
// "Channels" suggestion rows. Every start/join button explains that
// huddle media isn't supported.
type Huddles struct {
	base

	mu   sync.Mutex
	live map[string]huddleRoom // by room id
	sub  bool

	recent   async[[]huddleRoom]
	list     widget.List
	newBtn   widget.Clickable
	heroBtn  widget.Clickable
	heroX    widget.Clickable
	hideHero bool
	top      widget.Clickable
	rooms    map[string]*widget.Clickable
	people   map[string]*widget.Clickable
	chans    map[string]*widget.Clickable

	bgFetched map[string]bool // live huddles maybeFetchBackgrounds ran for
	// joinFn is the onHuddleJoin a HuddleInviteWindowWithPreviewContent
	// patch handed the Join point, by room (slick.go).
	joinFn map[string]slackc.JSFunc

	sugAt    time.Time
	sugUsers []string // IM partners by priority
	sugChans []core.Conv
}

// NewHuddles builds the Huddles view (route "huddles").
func NewHuddles(h Host) *Huddles {
	v := &Huddles{base: newBase(h), live: map[string]huddleRoom{}, rooms: map[string]*widget.Clickable{},
		people: map[string]*widget.Clickable{}, chans: map[string]*widget.Clickable{}}
	v.list.Axis = layout.Vertical
	return v
}

func (v *Huddles) subscribe() {
	if v.sub {
		return
	}
	v.sub = true
	ch, _ := v.h.Core().Subscribe()
	go func() {
		for ev := range ch {
			if ev.Type != "huddle" {
				continue
			}
			r, ok := roomFromEvent(ev.Raw)
			if !ok {
				continue
			}
			if len(r.Channels) == 0 && ev.Channel != "" {
				r.Channels = []string{ev.Channel}
			}
			v.mu.Lock()
			if r.Active {
				v.live[r.ID] = r
			} else {
				delete(v.live, r.ID)
			}
			v.mu.Unlock()
			v.h.Core().EnsureUsers(context.Background(), r.Participants)
			v.h.Invalidate()
		}
	}()
}

func (v *Huddles) liveRooms() []huddleRoom {
	v.mu.Lock()
	out := make([]huddleRoom, 0, len(v.live))
	for _, r := range v.live {
		out = append(out, r)
	}
	v.mu.Unlock()
	sort.Slice(out, func(i, j int) bool { return out[i].Start > out[j].Start })
	return out
}

// suggestions refreshes the DM/channel suggestions from the sidebar
// snapshot (highest priority first), at most every 30s.
func (v *Huddles) suggestions(now time.Time) {
	if !v.sugAt.IsZero() && now.Sub(v.sugAt) < 30*time.Second {
		return
	}
	v.sugAt = now
	snap := v.h.Core().Snapshot()
	convs := append([]core.Conv(nil), snap.Convs...)
	sort.SliceStable(convs, func(i, j int) bool { return convs[i].Priority > convs[j].Priority })
	self := snap.Self.ID
	v.sugUsers, v.sugChans = v.sugUsers[:0], v.sugChans[:0]
	for _, c := range convs {
		switch {
		case c.Kind == "im" && c.User != "" && c.User != self && c.User != "USLACKBOT" && len(v.sugUsers) < 6:
			if u, ok := v.user(c.User); !ok || u.IsBot || u.Deleted {
				continue
			}
			v.sugUsers = append(v.sugUsers, c.User)
		case (c.Kind == "channel" || c.Kind == "private") && !c.IsArchived && len(v.sugChans) < 5:
			v.sugChans = append(v.sugChans, c)
		}
	}
}

func clickIn(m map[string]*widget.Clickable, id string) *widget.Clickable {
	c := m[id]
	if c == nil {
		c = new(widget.Clickable)
		m[id] = c
	}
	return c
}

func (v *Huddles) update(gtx C) {
	if !v.recent.started() {
		a := v.h.Core()
		v.recent.load(v.h, false, func(ctx context.Context) ([]huddleRoom, error) {
			// seed live rooms (edge huddles/list), then the history
			if live, err := a.ActiveHuddles(ctx); err == nil {
				v.mu.Lock()
				for _, r := range live {
					if r.Active {
						v.live[r.ID] = r
					}
				}
				v.mu.Unlock()
			}
			rooms, _, err := a.HuddleHistory(ctx, "")
			return rooms, err
		})
	}
	if v.newBtn.Clicked(gtx) || v.heroBtn.Clicked(gtx) {
		startHuddle(v.h, v.tt, startHuddleArgs{IsStandaloneHuddle: true})
	}
	if v.heroX.Clicked(gtx) {
		v.hideHero = true
	}
	if v.top.Clicked(gtx) {
		v.list.Position = layout.Position{}
	}
	for i, m := range [...]map[string]*widget.Clickable{v.rooms, v.people, v.chans} {
		for id, c := range m {
			if !c.Clicked(gtx) {
				continue
			}
			switch i {
			case 0: // a live huddle: the plugin's join, else join it in its conversation
				v.joinClicked(id)
			case 1: // a person: huddle in your DM with them
				uid := id
				go func() {
					ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
					defer cancel()
					conv, err := v.h.Core().OpenDM(ctx, uid)
					if err != nil {
						v.h.Toast("Couldn't open the DM: " + err.Error())
						return
					}
					startHuddle(v.h, v.tt, startHuddleArgs{ChannelID: conv.ID})
				}()
			default:
				startHuddle(v.h, v.tt, startHuddleArgs{ChannelID: id})
			}
		}
	}
	// Slack fetches a live huddle's backgrounds as it appears.
	if v.tt.ThunkPatched("maybeFetchBackgrounds") {
		for _, r := range v.liveRooms() {
			if !v.bgFetched[r.ID] {
				if v.bgFetched == nil {
					v.bgFetched = map[string]bool{}
				}
				v.bgFetched[r.ID] = true
				fetchBackgrounds(v.tt, r.ID)
			}
		}
	}
}

type huddleItem struct {
	kind      int // see hk* constants
	r         huddleRoom
	title, tx string
	row       int // channel row index (for rounded ends)
}

const (
	hkHero = iota
	hkSection
	hkRoom
	hkTiles
	hkChannel
	hkPad
)

// Layout draws the view.
func (v *Huddles) Layout(gtx C) D {
	v.frame()
	v.subscribe()
	v.update(gtx)
	v.suggestions(gtx.Now)

	live := v.liveRooms()
	recent, _, _ := v.recent.get()
	items := make([]huddleItem, 0, 16)
	if !v.hideHero {
		items = append(items, huddleItem{kind: hkHero})
	}
	if len(live) > 0 {
		items = append(items, huddleItem{kind: hkSection, title: "Happening now", tx: "Huddles in your channels and DMs"})
		for _, r := range live {
			items = append(items, huddleItem{kind: hkRoom, r: r})
		}
	}
	var past []huddleRoom
	for _, r := range recent {
		if !r.Active {
			past = append(past, r)
		}
	}
	if len(past) > 0 {
		items = append(items, huddleItem{kind: hkSection, title: "Recent", tx: "Huddles you were part of"})
		for _, r := range past {
			items = append(items, huddleItem{kind: hkRoom, r: r})
		}
	}
	if len(v.sugUsers) > 0 {
		items = append(items, huddleItem{kind: hkSection, title: "Direct messages", tx: "Talk privately 1:1 with someone"})
		items = append(items, huddleItem{kind: hkTiles})
	}
	if len(v.sugChans) > 0 {
		items = append(items, huddleItem{kind: hkSection, title: "Channels", tx: "Meet with a whole team, or just let people drop in and out"})
		for i := range v.sugChans {
			items = append(items, huddleItem{kind: hkChannel, row: i})
		}
	}
	items = append(items, huddleItem{kind: hkPad})

	size := gtx.Constraints.Max
	fill(gtx, image.Rectangle{Max: size}, v.p.Bg)
	gtx.Constraints = layout.Exact(size)
	d := col(gtx,
		layout.Rigid(v.layoutHeader),
		layout.Flexed(1, func(gtx C) D {
			gtx.Constraints.Min = gtx.Constraints.Max
			return v.scrollList(gtx, &v.list, len(items), func(gtx C, i int) D { return v.item(gtx, items[i]) })
		}),
	)
	// Scroll-to-top button once scrolled: a 42px green square, white arrow.
	if v.list.Position.First > 0 || v.list.Position.Offset > gtx.Dp(200) {
		s := gtx.Dp(42)
		st := op.Offset(image.Pt(size.X-s-gtx.Dp(35), size.Y-s-gtx.Dp(24))).Push(gtx.Ops)
		v.top.Layout(gtx, func(gtx C) D {
			pointerCursor(gtx)
			bg := v.p.Green
			if v.top.Hovered() {
				bg = v.p.GreenHover
			}
			rrect(gtx, image.Rect(0, 0, s, s), gtx.Dp(8), bg)
			g := gtx
			g.Constraints = layout.Exact(image.Pt(s, s))
			layout.Center.Layout(g, func(gtx C) D { return v.icon(gtx, "arrow-up", 20, rgb(0xffffff)) })
			return D{Size: image.Pt(s, s)}
		})
		st.Pop()
	}
	return d
}

// layoutHeader: h 49 with a bottom hairline, title 18/24 900 #1d1c1d at a
// 34px inset, "+ New Huddle" (outline small, radius 4) on the right.
func (v *Huddles) layoutHeader(gtx C) D {
	w, h := gtx.Constraints.Max.X, gtx.Dp(49)
	g := gtx
	g.Constraints = layout.Exact(image.Pt(w, h))
	layout.Inset{Left: 34, Right: 36}.Layout(g, func(gtx C) D {
		return row(gtx, layout.Middle,
			layout.Flexed(1, func(gtx C) D { return v.label(gtx, 18, wBlack, v.p.Text, "Huddles") }),
			layout.Rigid(func(gtx C) D { return v.smallOutline(gtx, &v.newBtn, "plus", "New Huddle") }),
		)
	})
	return D{Size: image.Pt(w, h)}
}

// smallOutline: h 28, padding 0 12px, radius 4, 13px bold, 1px
// #5e5d60@.45 border, optional 16px icon.
func (v *Huddles) smallOutline(gtx C, c *widget.Clickable, icon, label string) D {
	return c.Layout(gtx, func(gtx C) D {
		pointerCursor(gtx)
		m := op.Record(gtx.Ops)
		d := layout.Inset{Left: 12, Right: 12}.Layout(gtx, func(gtx C) D {
			g := gtx
			g.Constraints.Min.Y = gtx.Dp(28)
			return layout.W.Layout(g, func(gtx C) D {
				return row(gtx, layout.Middle,
					layout.Rigid(func(gtx C) D {
						if icon == "" {
							return D{}
						}
						return layout.Inset{Right: 4}.Layout(gtx, func(gtx C) D { return v.icon(gtx, icon, 16, v.p.Text) })
					}),
					layout.Rigid(func(gtx C) D { return v.label(gtx, 13, wBold, v.p.Text, label) }),
				)
			})
		})
		call := m.Stop()
		r := image.Rectangle{Max: d.Size}
		rad := gtx.Dp(4)
		rrect(gtx, r, rad, v.p.Bg)
		if c.Hovered() {
			rrect(gtx, r, rad, v.p.Hover)
		}
		border(gtx, r, rad, max(1, gtx.Dp(1)), v.p.BorderStr)
		call.Add(gtx.Ops)
		return d
	})
}

func (v *Huddles) item(gtx C, it huddleItem) D {
	switch it.kind {
	case hkHero:
		return v.hero(gtx)
	case hkSection:
		return layout.Inset{Left: 55, Right: 55, Top: 20, Bottom: 10}.Layout(gtx, func(gtx C) D {
			return row(gtx, layout.Middle,
				layout.Rigid(func(gtx C) D { return v.label(gtx, 15, wBold, v.p.Text, it.title) }),
				layout.Flexed(1, func(gtx C) D { return v.label(gtx, 15, wReg, v.p.Text, " — "+it.tx) }),
			)
		})
	case hkRoom:
		return layout.Inset{Left: 55, Right: 55}.Layout(gtx, func(gtx C) D {
			if !it.r.Active {
				return v.room(gtx, it.r)
			}
			return col(gtx,
				layout.Rigid(func(gtx C) D { return v.room(gtx, it.r) }),
			)
		})
	case hkTiles:
		return layout.Inset{Left: 55, Right: 55, Bottom: 10}.Layout(gtx, v.tiles)
	case hkChannel:
		return layout.Inset{Left: 55, Right: 55}.Layout(gtx, func(gtx C) D { return v.channelRow(gtx, it.row) })
	}
	return D{Size: image.Pt(0, gtx.Dp(80))}
}

// hero: h 266, bg #e9ffec, hairlines top and bottom, the 22/30 900 title,
// 15/22 body at 70% and the green "Start a Huddle" button, a drawn huddle
// window on the right, and a dismiss x.
func (v *Huddles) hero(gtx C) D {
	w, h := gtx.Constraints.Max.X, gtx.Dp(266)
	bg := rgb(0xe9ffec)
	body := argb(0xb31d1c1d)
	if v.th.Dark {
		bg, body = rgb(0x00240f), argb(0xb3f8f8f8) // plt-mojito-0, content-pry@.7
	}
	hair := max(1, gtx.Dp(1))
	fill(gtx, image.Rect(0, 0, w, h), bg)
	fill(gtx, image.Rect(0, 0, w, hair), v.p.Border)
	fill(gtx, image.Rect(0, h-hair, w, h), v.p.Border)
	illW, illH := gtx.Dp(272), gtx.Dp(196)
	// Slack (1440×900): text block at +76/+66 inside the banner (a 561px
	// column inset 32/22, padded 44).
	textW := max(0, min(gtx.Dp(561-44), w-gtx.Dp(76+44)))
	func() {
		defer opOffset(gtx, image.Pt(gtx.Dp(76), gtx.Dp(66))).Pop()
		g := gtx
		g.Constraints = layout.Constraints{Max: image.Pt(textW, h-gtx.Dp(66+22))}
		col(g,
			layout.Rigid(func(gtx C) D {
				return layout.Inset{Bottom: 8}.Layout(gtx, func(gtx C) D {
					return v.para(gtx, 22, 30, wBlack, v.p.Text, 2, "Instantly connect over audio or video")
				})
			}),
			layout.Rigid(func(gtx C) D {
				gtx.Constraints.Max.X = min(gtx.Constraints.Max.X, gtx.Dp(517))
				return layout.Inset{Bottom: 16}.Layout(gtx, func(gtx C) D {
					return v.para(gtx, 15, 22, wReg, body, 3, "Talk it out in real time on a huddle, with screen-sharing, expressive reactions and a message thread that automatically saves for later reference.")
				})
			}),
			layout.Rigid(func(gtx C) D { return v.greenButton(gtx, &v.heroBtn, "headphones", "Start a Huddle") }),
		)
	}()
	if w > gtx.Dp(760) {
		x := min(gtx.Dp(76)+textW+gtx.Dp(20), w-illW-gtx.Dp(56))
		func() {
			defer opOffset(gtx, image.Pt(x, (h-illH)/2)).Pop()
			v.illustration(gtx, image.Pt(illW, illH))
		}()
	}
	func() {
		s := gtx.Dp(36)
		defer opOffset(gtx, image.Pt(w-s-gtx.Dp(16), gtx.Dp(17))).Pop()
		v.iconButton(gtx, &v.heroX, "close", 36, 20, v.p.Sec)
	}()
	return D{Size: image.Pt(w, h)}
}

// greenButton: primary medium, h 36, padding 0 12px, radius 8, 15px bold.
func (v *Huddles) greenButton(gtx C, c *widget.Clickable, icon, label string) D {
	return c.Layout(gtx, func(gtx C) D {
		pointerCursor(gtx)
		tw := v.th.Measure(gtx, 15, wBold, label)
		sz := image.Pt(tw+gtx.Dp(24+20+6), gtx.Dp(36))
		bg := v.p.Green
		if c.Hovered() {
			bg = v.p.GreenHover
		}
		rrect(gtx, image.Rectangle{Max: sz}, gtx.Dp(8), bg)
		g := gtx
		g.Constraints = layout.Exact(sz)
		layout.Center.Layout(g, func(gtx C) D {
			return row(gtx, layout.Middle,
				layout.Rigid(func(gtx C) D { return v.icon(gtx, icon, 20, rgb(0xffffff)) }),
				hgap(6),
				layout.Rigid(func(gtx C) D { return v.label(gtx, 15, wBold, rgb(0xffffff), label) }),
			)
		})
		return D{Size: sz}
	})
}

// illustration is our own vector drawing (not Slack's artwork): an
// aubergine huddle window with two participant tiles and a control bar.
func (v *Huddles) illustration(gtx C, sz image.Point) {
	rad := gtx.Dp(12)
	r := image.Rectangle{Max: sz}
	theme.Shadow(gtx, r, rad, gtx.Dp(10), argb(0x26000000))
	rrect(gtx, r, rad, rgb(0x4a154b))
	pad := gtx.Dp(14)
	tileW := (sz.X - 3*pad) / 2
	tileH := sz.Y - 2*pad - gtx.Dp(40)
	ids := []string{v.selfID()}
	if len(v.sugUsers) > 0 {
		ids = append(ids, v.sugUsers[0])
	}
	for i := 0; i < 2; i++ {
		x := pad + i*(tileW+pad)
		tr := image.Rect(x, pad, x+tileW, pad+tileH)
		tint := rgb(0x611f69)
		if i == 1 {
			tint = rgb(0x2b6f8e)
		}
		rrect(gtx, tr, gtx.Dp(8), tint)
		a := gtx.Dp(44)
		func() {
			defer opOffset(gtx, image.Pt(tr.Min.X+(tileW-a)/2, tr.Min.Y+(tileH-a)/2)).Pop()
			if i < len(ids) {
				u, _ := v.user(ids[i])
				v.avatar(gtx, u.Avatar, 44)
			} else {
				rrect(gtx, image.Rect(0, 0, a, a), gtx.Dp(10), argb(0x40ffffff))
			}
		}()
	}
	cy := sz.Y - pad - gtx.Dp(14)
	cx := sz.X/2 - gtx.Dp(40)
	for i, c := range [...]uint32{0x40ffffff, 0x40ffffff, 0xffe01e5a} {
		theme.Circle(gtx, image.Pt(cx+i*gtx.Dp(40), cy), gtx.Dp(28), argb(c))
	}
}

// room is a live or past huddle: a bordered 12px-radius row with a 36px
// headphones tile (green when live), title, people + timing, a facepile,
// and Join for live rooms.
func (v *Huddles) room(gtx C, r huddleRoom) D {
	c := clickIn(v.rooms, r.ID)
	d := c.Layout(gtx, func(gtx C) D {
		pointerCursor(gtx)
		m := op.Record(gtx.Ops)
		d := layout.Inset{Top: 10, Bottom: 10, Left: 12, Right: 12}.Layout(gtx, func(gtx C) D {
			gtx.Constraints.Min.X = gtx.Constraints.Max.X
			return row(gtx, layout.Middle,
				layout.Rigid(func(gtx C) D {
					s := gtx.Dp(36)
					bg, fg := v.p.Chip, v.p.Text
					if r.Active {
						bg, fg = v.p.Green, rgb(0xffffff)
					}
					rrect(gtx, image.Rect(0, 0, s, s), gtx.Dp(8), bg)
					g := gtx
					g.Constraints = layout.Exact(image.Pt(s, s))
					layout.Center.Layout(g, func(gtx C) D { return v.icon(gtx, "headphones", 20, fg) })
					return D{Size: image.Pt(s, s)}
				}),
				hgap(8),
				layout.Flexed(1, func(gtx C) D {
					return col(gtx,
						layout.Rigid(func(gtx C) D {
							if r.Active {
								return v.huddleTitle(gtx, r)
							}
							return v.label(gtx, 15, wBold, v.p.Text, v.roomTitle(r))
						}),
						layout.Rigid(func(gtx C) D { return v.label(gtx, 13, wReg, v.p.Muted, v.roomMeta(r)) }),
					)
				}),
				layout.Rigid(func(gtx C) D {
					if r.Active {
						return v.huddleBar(gtx, r)
					}
					return v.faces(gtx, r)
				}),
			)
		})
		call := m.Stop()
		rr := image.Rectangle{Max: d.Size}
		rrect(gtx, rr, gtx.Dp(12), v.p.Bg)
		if c.Hovered() {
			rrect(gtx, rr, gtx.Dp(12), v.p.Hover)
		}
		border(gtx, rr, gtx.Dp(12), max(1, gtx.Dp(1)), v.p.CardLine)
		call.Add(gtx.Ops)
		return d
	})
	return D{Size: image.Pt(d.Size.X, d.Size.Y+gtx.Dp(8))}
}

// joinPill draws the small green "Join" button face.
func (v *Huddles) joinPill(gtx C) D {
	tw := v.th.Measure(gtx, 13, wBold, "Join")
	sz := image.Pt(tw+gtx.Dp(24), gtx.Dp(28))
	rrect(gtx, image.Rectangle{Max: sz}, gtx.Dp(4), v.p.Green)
	g := gtx
	g.Constraints = layout.Exact(sz)
	layout.Center.Layout(g, func(gtx C) D { return v.label(gtx, 13, wBold, rgb(0xffffff), "Join") })
	return D{Size: sz}
}

func (v *Huddles) roomTitle(r huddleRoom) string {
	if r.Name != "" {
		return r.Name
	}
	if len(r.Channels) > 0 {
		return v.convTitle(r.Channels[0], nil)
	}
	return "Huddle"
}

func (v *Huddles) roomMeta(r huddleRoom) string {
	people := r.Participants
	if !r.Active && len(r.History) > 0 {
		people = r.History
	}
	var who string
	switch n := len(people); {
	case n == 1:
		who = v.userName(people[0])
	case n == 2:
		who = v.userName(people[0]) + " and " + v.userName(people[1])
	case n > 2:
		who = v.userName(people[0]) + " and " + strconv.Itoa(n-1) + " others"
	}
	var when string
	if r.Active {
		if r.Start > 0 {
			when = "Started " + agoText(time.Since(unixTime(r.Start)))
		}
	} else if r.Start > 0 {
		when = listTime(unixTime(r.Start))
		if r.End > r.Start {
			when += " · " + durText(time.Duration(r.End-r.Start)*time.Second)
		}
	}
	switch {
	case who != "" && when != "":
		return who + " · " + when
	case who != "":
		return who
	}
	return when
}

// faces draws up to 4 overlapping 24px participant avatars.
func (v *Huddles) faces(gtx C, r huddleRoom) D {
	ids := r.Participants
	if len(ids) == 0 {
		ids = r.History
	}
	if len(ids) > 4 {
		ids = ids[:4]
	}
	if len(ids) == 0 {
		return D{}
	}
	s := gtx.Dp(24)
	step := s - gtx.Dp(6)
	ring := gtx.Dp(2)
	w := step*(len(ids)-1) + s
	for i := len(ids) - 1; i >= 0; i-- {
		func() {
			defer opOffset(gtx, image.Pt(i*step, 0)).Pop()
			rrect(gtx, image.Rect(-ring, -ring, s+ring, s+ring), avatarRadius(gtx, 24)+ring, v.p.Bg)
			u, _ := v.user(ids[i])
			v.avatar(gtx, u.Avatar, 24)
		}()
	}
	return D{Size: image.Pt(w, s)}
}

// tiles: DM suggestions, 3 per row (w 301, h 258, gap 12): a tinted header
// (h 136) with a facepile of your avatar and theirs (60px), then the name
// (15px bold) and their status (13px, up to 3 lines).
func (v *Huddles) tiles(gtx C) D {
	gap := gtx.Dp(12)
	tw := min(gtx.Dp(301), (gtx.Constraints.Max.X-2*gap)/3)
	th := gtx.Dp(239) // Slack: 301×239 cards
	const cols = 3
	rows := (len(v.sugUsers) + cols - 1) / cols
	y := 0
	for r := 0; r < rows; r++ {
		for c := 0; c < cols; c++ {
			i := r*cols + c
			if i >= len(v.sugUsers) {
				break
			}
			func() {
				defer opOffset(gtx, image.Pt(c*(tw+gap), y)).Pop()
				g := gtx
				g.Constraints = layout.Exact(image.Pt(tw, th))
				v.tile(g, v.sugUsers[i], image.Pt(tw, th))
			}()
		}
		y += th + gap
	}
	return D{Size: image.Pt(gtx.Constraints.Max.X, max(0, y-gap))}
}

// tileTints stand in for the person's huddle background image.
var tileTints = [...]uint32{0xd8f5e5, 0xe0e7ff, 0xfde4cf, 0xf3e1f7, 0xd9f1fb, 0xfff2c4}

func (v *Huddles) tile(gtx C, uid string, sz image.Point) D {
	c := clickIn(v.people, uid)
	return c.Layout(gtx, func(gtx C) D {
		pointerCursor(gtx)
		rad := gtx.Dp(8)
		r := image.Rectangle{Max: sz}
		theme.Shadow(gtx, r, rad, gtx.Dp(3), argb(0x14000000))
		rrect(gtx, r, rad, v.p.Surface)
		hh := gtx.Dp(136)
		tint := rgb(tileTints[0])
		if uid != "" {
			tint = rgb(tileTints[int(uid[len(uid)-1])%len(tileTints)])
		}
		if v.th.Dark {
			tint = theme.Mix(tint, v.p.Surface, 0.8)
		}
		func() {
			defer clipRR(gtx, image.Rect(0, 0, sz.X, hh), rad, rad, 0, 0).Pop()
			fill(gtx, image.Rect(0, 0, sz.X, hh), tint)
		}()
		a := gtx.Dp(60)
		overlap := gtx.Dp(8)
		x0 := (sz.X - (2*a - overlap)) / 2
		y0 := (hh - a) / 2
		me, _ := v.user(v.selfID())
		them, _ := v.user(uid)
		for i, u := range [...]core.User{me, them} {
			func() {
				defer opOffset(gtx, image.Pt(x0+i*(a-overlap), y0)).Pop()
				ring := gtx.Dp(2)
				rrect(gtx, image.Rect(-ring, -ring, a+ring, a+ring), gtx.Dp(8)+ring, tint)
				v.avatar(gtx, u.Avatar, 60)
			}()
		}
		func() {
			defer opOffset(gtx, image.Pt(gtx.Dp(12), hh+gtx.Dp(12))).Pop()
			g := gtx
			g.Constraints = layout.Constraints{Max: image.Pt(sz.X-gtx.Dp(24), max(0, sz.Y-hh-gtx.Dp(20)))}
			col(g,
				layout.Rigid(func(gtx C) D {
					return row(gtx, layout.Middle,
						layout.Flexed(1, func(gtx C) D { return v.label(gtx, 15, wBold, v.p.Text, displayName(them)) }),
						layout.Rigid(func(gtx C) D {
							if them.StatusEmojiURL == "" {
								return D{}
							}
							return layout.Inset{Left: 4}.Layout(gtx, func(gtx C) D { return v.emojiImg(gtx, them.StatusEmojiURL, 16) })
						}),
					)
				}),
				layout.Rigid(func(gtx C) D {
					t := them.StatusText
					if t == "" {
						t = them.Title
					}
					return v.para(gtx, 13, 19, wReg, v.p.Text, 3, t)
				}),
			)
		}()
		border(gtx, r, rad, max(1, gtx.Dp(1)), v.p.Ring)
		if c.Hovered() {
			rrect(gtx, r, rad, v.p.Hover)
		}
		return D{Size: sz}
	})
}

// channelRow: bordered list rows h 56, padding 10px 12px, first/last
// rounded 12; a 36px tile (#1d1c1d@.06, radius 8) with a "#" glyph, then
// "#name" 15px bold.
func (v *Huddles) channelRow(gtx C, i int) D {
	ch := v.sugChans[i]
	c := clickIn(v.chans, ch.ID)
	first, last := i == 0, i == len(v.sugChans)-1
	return c.Layout(gtx, func(gtx C) D {
		pointerCursor(gtx)
		w, h := gtx.Constraints.Max.X, gtx.Dp(56)
		rad := gtx.Dp(12)
		var top, bot int
		if first {
			top = rad
		}
		if last {
			bot = rad
		}
		r := image.Rect(0, 0, w, h)
		px1 := max(1, gtx.Dp(1))
		func() {
			defer clipRR(gtx, r, top, top, bot, bot).Pop()
			fill(gtx, r, v.p.CardLine)
			inner := image.Rect(px1, px1, w-px1, h-px1)
			if !first {
				inner.Min.Y = 0
			}
			innerClip := clipRR(gtx, inner, max(0, top-px1), max(0, top-px1), max(0, bot-px1), max(0, bot-px1))
			fill(gtx, inner, v.p.Bg)
			if c.Hovered() {
				fill(gtx, inner, v.p.Hover)
			}
			innerClip.Pop()
		}()
		g := gtx
		g.Constraints = layout.Exact(image.Pt(w, h))
		layout.Inset{Left: 12, Right: 12}.Layout(g, func(gtx C) D {
			return layout.W.Layout(gtx, func(gtx C) D {
				return row(gtx, layout.Middle,
					layout.Rigid(func(gtx C) D {
						s := gtx.Dp(36)
						rrect(gtx, image.Rect(0, 0, s, s), gtx.Dp(8), v.p.Chip)
						gg := gtx
						gg.Constraints = layout.Exact(image.Pt(s, s))
						icon := "hash"
						if ch.Kind == "private" {
							icon = "lock"
						}
						layout.Center.Layout(gg, func(gtx C) D { return v.icon(gtx, icon, 20, v.p.Text) })
						return D{Size: image.Pt(s, s)}
					}),
					hgap(8),
					layout.Rigid(func(gtx C) D { return v.label(gtx, 15, wBold, v.p.Text, "#"+ch.Name) }),
				)
			})
		})
		return D{Size: image.Pt(w, h)}
	})
}

func agoText(d time.Duration) string {
	switch {
	case d < time.Minute:
		return "just now"
	case d < time.Hour:
		return plural(int(d.Minutes()), "min") + " ago"
	default:
		return plural(int(d.Hours()), "hour") + " ago"
	}
}

func durText(d time.Duration) string {
	switch {
	case d < time.Minute:
		return plural(int(d.Seconds()), "sec")
	case d < time.Hour:
		return plural(int(d.Minutes()), "min")
	default:
		h := int(d.Hours())
		m := int(d.Minutes()) % 60
		if m == 0 {
			return plural(h, "hr")
		}
		return strconv.Itoa(h) + " hr " + strconv.Itoa(m) + " min"
	}
}
