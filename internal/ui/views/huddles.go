package views

import (
	"context"
	"image"
	"sort"
	"strconv"
	"sync"
	"time"

	"gioui.org/layout"
	"gioui.org/widget"
)

// joinUnsupported is shown when the user tries to start or join a huddle:
// huddle audio/video runs on Amazon Chime media servers that yoloslack
// doesn't implement.
const joinUnsupported = "Huddles need Slack's audio/video stack (Amazon Chime), which yoloslack doesn't support yet"

// Huddles lists live huddles (from realtime sh_room_* events) and recent
// ones. Joining isn't supported; the buttons explain why.
type Huddles struct {
	base

	mu   sync.Mutex
	live map[string]huddleRoom // by room id
	sub  bool

	recent async[[]huddleRoom]
	list   widget.List
	start  widget.Clickable
	retry  widget.Clickable
	joins  map[string]*widget.Clickable
	opens  map[string]*widget.Clickable
}

// NewHuddles builds the Huddles view (route "huddles").
func NewHuddles(h Host) *Huddles {
	v := &Huddles{base: newBase(h), live: map[string]huddleRoom{}, joins: map[string]*widget.Clickable{}, opens: map[string]*widget.Clickable{}}
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

func (v *Huddles) clickPair(id string) (*widget.Clickable, *widget.Clickable) {
	j := v.joins[id]
	if j == nil {
		j = new(widget.Clickable)
		v.joins[id] = j
	}
	o := v.opens[id]
	if o == nil {
		o = new(widget.Clickable)
		v.opens[id] = o
	}
	return j, o
}

// Layout draws the view.
func (v *Huddles) Layout(gtx C) D {
	v.frame()
	v.subscribe()
	if !v.recent.started() || v.retry.Clicked(gtx) {
		a := v.h.Core()
		v.recent.load(v.h, false, func(ctx context.Context) ([]huddleRoom, error) { return recentHuddles(ctx, a) })
	}
	if v.start.Clicked(gtx) {
		v.h.Toast(joinUnsupported)
	}
	live := v.liveRooms()
	recent, loaded, err := v.recent.get()
	for _, r := range append(append([]huddleRoom(nil), live...), recent...) {
		j, o := v.clickPair(r.ID)
		if j.Clicked(gtx) {
			v.h.Toast(joinUnsupported)
		}
		if o.Clicked(gtx) && len(r.Channels) > 0 {
			v.h.Navigate("c/" + r.Channels[0])
		}
	}

	type item struct {
		kind int // 0 section title, 1 live, 2 recent, 3 empty text, 4 state
		r    huddleRoom
		text string
	}
	items := make([]item, 0, len(live)+len(recent)+4)
	items = append(items, item{kind: 0, text: "Active now"})
	if len(live) == 0 {
		items = append(items, item{kind: 3, text: "No huddles are happening in your channels right now."})
	}
	for _, r := range live {
		items = append(items, item{kind: 1, r: r})
	}
	items = append(items, item{kind: 0, text: "Recent huddles"})
	switch {
	case !loaded:
		items = append(items, item{kind: 4, text: "loading"})
	case err != nil || len(recent) == 0:
		items = append(items, item{kind: 3, text: "Huddles you join will show up here."})
	}
	for _, r := range recent {
		if !r.Active {
			items = append(items, item{kind: 2, r: r})
		}
	}

	return col(gtx,
		layout.Rigid(func(gtx C) D {
			return v.header(gtx, "Huddles",
				layout.Rigid(func(gtx C) D { return v.button(gtx, &v.start, btnOutline, true, "Start a huddle") }),
			)
		}),
		layout.Rigid(func(gtx C) D { return hline(gtx, v.p.Border) }),
		layout.Flexed(1, func(gtx C) D {
			gtx.Constraints.Min = gtx.Constraints.Max
			return v.scrollList(gtx, &v.list, len(items), func(gtx C, i int) D {
				it := items[i]
				switch it.kind {
				case 0:
					return layout.Inset{Top: 20, Left: 16, Right: 16, Bottom: 8}.Layout(gtx, func(gtx C) D {
						return v.label(gtx, 15, wBold, v.p.Text, it.text)
					})
				case 3:
					return layout.Inset{Left: 16, Right: 16, Bottom: 8}.Layout(gtx, func(gtx C) D {
						return v.para(gtx, 15, 22, wReg, v.p.Muted, 0, it.text)
					})
				case 4:
					return v.skeleton(gtx, 3, 36)
				}
				return v.room(gtx, it.r, it.kind == 1)
			})
		}),
	)
}

// room draws one huddle row: a headphones tile (green when live), the
// conversation, who's there, timing, and Join / View buttons.
func (v *Huddles) room(gtx C, r huddleRoom, live bool) D {
	join, open := v.clickPair(r.ID)
	return layout.Inset{Left: 8, Right: 8}.Layout(gtx, func(gtx C) D {
		return v.hoverable(gtx, open, false, 8, func(gtx C) D {
			gtx.Constraints.Min.X = gtx.Constraints.Max.X
			return layout.Inset{Top: 10, Bottom: 10, Left: 8, Right: 8}.Layout(gtx, func(gtx C) D {
				return row(gtx, layout.Middle,
					layout.Rigid(func(gtx C) D {
						s := gtx.Dp(36)
						bg := v.p.Track
						fg := v.p.Sec
						if live {
							bg, fg = v.p.Green, rgb(0xffffff)
						}
						rrect(gtx, image.Rect(0, 0, s, s), gtx.Dp(8), bg)
						g := gtx
						g.Constraints = layout.Exact(image.Pt(s, s))
						layout.Center.Layout(g, func(gtx C) D { return v.icon(gtx, "headphones", 20, fg) })
						return D{Size: image.Pt(s, s)}
					}),
					hgap(12),
					layout.Flexed(1, func(gtx C) D {
						return col(gtx,
							layout.Rigid(func(gtx C) D { return v.label(gtx, 15, wBlack, v.p.Text, v.roomTitle(r)) }),
							vgap(2),
							layout.Rigid(func(gtx C) D { return v.label(gtx, 13, wReg, v.p.Muted, v.roomMeta(r, live)) }),
						)
					}),
					layout.Rigid(func(gtx C) D { return v.faces(gtx, r) }),
					hgap(12),
					layout.Rigid(func(gtx C) D {
						if live {
							return v.button(gtx, join, btnPrimary, true, "Join")
						}
						return D{}
					}),
				)
			})
		})
	})
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

func (v *Huddles) roomMeta(r huddleRoom, live bool) string {
	people := r.Participants
	if !live && len(r.History) > 0 {
		people = r.History
	}
	n := len(people)
	var who string
	switch {
	case n == 0:
		who = ""
	case n == 1:
		who = v.userName(people[0])
	case n == 2:
		who = v.userName(people[0]) + " and " + v.userName(people[1])
	default:
		who = v.userName(people[0]) + " and " + strconv.Itoa(n-1) + " others"
	}
	var when string
	if live {
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
