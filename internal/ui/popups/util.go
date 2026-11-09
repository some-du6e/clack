package popups

import (
	"image"
	"image/color"
	"strconv"
	"strings"
	"sync"
	"time"

	"gioui.org/f32"
	"gioui.org/layout"
	"gioui.org/op"
	"gioui.org/op/clip"
	"gioui.org/op/paint"

	"github.com/some-du6e/yoloslackgoclient/internal/core"
	"github.com/some-du6e/yoloslackgoclient/internal/ui/theme"
	"github.com/some-du6e/yoloslackgoclient/internal/ui/uipref"
)

// ---- images ----

// avatar draws a user/channel image of side size dp with Slack's avatar
// rounding (clamp(4px, 22.2%, 12px)). A neutral tile shows while loading.
func avatar(gtx layout.Context, h Host, url string, size float32) layout.Dimensions {
	px := dp(gtx, size)
	sz := image.Pt(px, px)
	rad := avatarRadius(gtx, size)
	if url != "" {
		if img, ok := h.Images().Get(url, px); ok {
			return theme.Image(gtx, img, sz, rad)
		}
	}
	rrect(gtx, image.Rectangle{Max: sz}, rad, colorsOf(h.Theme()).Selected)
	return layout.Dimensions{Size: sz}
}

func avatarRadius(gtx layout.Context, size float32) int {
	r := size * 0.2222
	if r < 4 {
		r = 4
	}
	if r > 12 {
		r = 12
	}
	return dp(gtx, r)
}

// emojiImage draws emoji name (":smile:" or "smile", skin tones allowed)
// at size dp. Unknown names draw nothing but keep the space.
func emojiImage(gtx layout.Context, h Host, name string, size float32) layout.Dimensions {
	px := dp(gtx, size)
	sz := image.Pt(px, px)
	url := emojiURL(h, name)
	if url != "" {
		if img, ok := h.Images().Get(url, px); ok {
			return containImage(gtx, img, sz)
		}
	}
	return layout.Dimensions{Size: sz}
}

// emojiURL resolves an emoji name to its image URL via core (custom map,
// aliases, standard CDN).
func emojiURL(h Host, name string) string {
	name = strings.Trim(name, ":")
	if name == "" || h.Core() == nil {
		return ""
	}
	u, _, ok := h.Core().Emoji(name)
	if !ok {
		return ""
	}
	return u
}

// ---- users and conversations ----

func userName(u core.User) string {
	if u.Name != "" {
		return u.Name
	}
	if u.RealName != "" {
		return u.RealName
	}
	return u.Handle
}

func lookupUser(h Host, id string) (core.User, bool) {
	if h.Core() == nil || id == "" {
		return core.User{}, false
	}
	u, ok := h.Core().User(id)
	if ok {
		u = memberView(u)
	}
	return u, ok
}

// memberView applies plugins' member patches (patchSlice('members'):
// Nicknames, StreamerMode…) as Slack's components read them from the
// store. Unpatched it returns u unchanged.
func memberView(u core.User) core.User {
	t := tautRT.Load()
	if t == nil || u.ID == "" || !t.SlicePatched("members") {
		return u
	}
	mf, ok := t.MemberView(u.ID)
	if !ok {
		return u
	}
	if mf.Name != "" {
		u.Name = mf.Name
	}
	if mf.RealName != "" {
		u.RealName = mf.RealName
	}
	if mf.Avatar != "" {
		u.Avatar, u.Avatar192, u.Avatar512 = mf.Avatar, mf.Avatar, mf.Avatar
	}
	return u
}

// convTitle is how the sidebar names a conversation ("general", a DM
// partner's name, "a, b, c" for group DMs).
func convTitle(h Host, c core.Conv) string {
	switch c.Kind {
	case "im":
		if u, ok := lookupUser(h, c.User); ok {
			return userName(u)
		}
	case "mpim":
		var names []string
		for _, id := range c.Members {
			if h.Core() != nil && id == h.Core().Self().ID {
				continue
			}
			if u, ok := lookupUser(h, id); ok {
				names = append(names, userName(u))
			}
		}
		if len(names) > 0 {
			return strings.Join(names, ", ")
		}
	}
	return c.Name
}

// convIcon returns the icon name for a conversation kind.
func convIcon(kind string) string {
	switch kind {
	case "private":
		return "lock"
	case "im", "mpim":
		return "dms"
	}
	return "hash"
}

// presenceDot draws Slack's presence indicator (filled green when active,
// hollow ring when away) of diameter d dp at the origin.
func presenceDot(gtx layout.Context, th *theme.Theme, active bool, d float32) layout.Dimensions {
	cl := colorsOf(th)
	px := dp(gtx, d)
	r := image.Rect(0, 0, px, px)
	if active {
		rrect(gtx, r, px/2, cl.Presence)
	} else {
		stroke(gtx, r, px/2, maxInt(1, px/6), cl.Away)
	}
	return layout.Dimensions{Size: r.Max}
}

func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}

func minInt(a, b int) int {
	if a < b {
		return a
	}
	return b
}

// ---- time ----

// tsTime parses a Slack ts ("1730000000.123456").
func tsTime(ts string) time.Time {
	if ts == "" {
		return time.Time{}
	}
	sec, frac, _ := strings.Cut(ts, ".")
	s, err := strconv.ParseInt(sec, 10, 64)
	if err != nil {
		return time.Time{}
	}
	var ns int64
	if frac != "" {
		for len(frac) < 9 {
			frac += "0"
		}
		ns, _ = strconv.ParseInt(frac[:9], 10, 64)
	}
	return time.Unix(s, ns)
}

// clock formats "4:31 PM".
func clock(t time.Time) string { return uipref.Clock(t) }

// dayTime formats Slack's search/lightbox timestamps: "Today at 12:15 PM",
// "Yesterday at 9:02 AM", "Nov 3rd at 4:31 PM", "Nov 3rd, 2024 at …".
func dayTime(t time.Time, now time.Time) string {
	if t.IsZero() {
		return ""
	}
	y1, m1, d1 := t.Date()
	y2, m2, d2 := now.Date()
	switch {
	case y1 == y2 && m1 == m2 && d1 == d2:
		return "Today at " + clock(t)
	case y1 == y2 && m1 == m2 && d1 == d2-1, now.AddDate(0, 0, -1).Format("20060102") == t.Format("20060102"):
		return "Yesterday at " + clock(t)
	case y1 == y2:
		return t.Format("Jan ") + ordinal(d1) + " at " + clock(t)
	}
	return t.Format("Jan ") + ordinal(d1) + t.Format(", 2006") + " at " + clock(t)
}

// ago formats "1 hour ago", "3 minutes ago", "just now".
func ago(t, now time.Time) string {
	d := now.Sub(t)
	switch {
	case d < time.Minute:
		return "just now"
	case d < time.Hour:
		return plural(int(d/time.Minute), "minute") + " ago"
	case d < 24*time.Hour:
		return plural(int(d/time.Hour), "hour") + " ago"
	case d < 30*24*time.Hour:
		return plural(int(d/(24*time.Hour)), "day") + " ago"
	}
	return t.Format("Jan 2, 2006")
}

func plural(n int, unit string) string {
	if n == 1 {
		return "1 " + unit
	}
	return strconv.Itoa(n) + " " + unit + "s"
}

func ordinal(d int) string {
	s := strconv.Itoa(d)
	switch {
	case d%100 >= 11 && d%100 <= 13:
		return s + "th"
	case d%10 == 1:
		return s + "st"
	case d%10 == 2:
		return s + "nd"
	case d%10 == 3:
		return s + "rd"
	}
	return s + "th"
}

// localTime is the user's wall clock ("4:31 PM") from their tz offset.
func localTime(u core.User, now time.Time) string {
	if u.TZ != "" {
		if loc, err := time.LoadLocation(u.TZ); err == nil {
			return clock(now.In(loc))
		}
	}
	return clock(now.UTC().Add(time.Duration(u.TZOffset) * time.Second))
}

// permalink builds a message link from the team domain.
func permalink(h Host, ch, ts, threadTS string) string {
	u := "https://" + teamDomain(h) + ".slack.com/archives/" + ch + "/p" + strings.ReplaceAll(ts, ".", "")
	if threadTS != "" && threadTS != ts {
		u += "?thread_ts=" + threadTS + "&cid=" + ch
	}
	return u
}

var domains sync.Map // Host -> team domain

// teamDomain is the workspace's Slack subdomain (cached; Snapshot copies).
func teamDomain(h Host) string {
	if v, ok := domains.Load(h); ok {
		return v.(string)
	}
	d := "hackclub"
	if c := h.Core(); c != nil {
		// the store, not Snapshot: Snapshot waits for Boot, and this runs
		// on the UI goroutine (Copy link before boot froze the window)
		c.S.RLock()
		s := c.S.TeamDomain
		c.S.RUnlock()
		if s == "" {
			if _, booted := actSnapshot(h); !booted {
				return d // not known yet: ask again later
			}
		} else {
			d = s
		}
	}
	domains.Store(h, d)
	return d
}

// containImage draws img scaled to fit inside sz (centered, no crop).
func containImage(gtx layout.Context, img paint.ImageOp, sz image.Point) layout.Dimensions {
	is := img.Size()
	if is.X <= 0 || is.Y <= 0 {
		return layout.Dimensions{Size: sz}
	}
	s := float32(sz.X) / float32(is.X)
	if sy := float32(sz.Y) / float32(is.Y); sy < s {
		s = sy
	}
	off := f32.Pt((float32(sz.X)-float32(is.X)*s)/2, (float32(sz.Y)-float32(is.Y)*s)/2)
	defer clip.Rect{Max: sz}.Push(gtx.Ops).Pop()
	defer op.Affine(f32.Affine2D{}.Scale(f32.Point{}, f32.Pt(s, s)).Offset(off)).Push(gtx.Ops).Pop()
	img.Filter = paint.FilterLinear
	img.Add(gtx.Ops)
	paint.PaintOp{}.Add(gtx.Ops)
	return layout.Dimensions{Size: sz}
}

// msgRoute is the Navigate route that jumps to a message: "c/<id>/m/<ts>"
// scrolls to and flashes it (ChannelView.Highlight); thread replies open
// their thread ("c/<id>/t/<root>"), per docs/NATIVE.md.
func msgRoute(m core.Message) string {
	if m.ThreadTS != "" && m.ThreadTS != m.TS {
		return "c/" + m.Channel + "/t/" + m.ThreadTS
	}
	if m.TS == "" {
		return "c/" + m.Channel
	}
	return "c/" + m.Channel + "/m/" + m.TS
}

// ---- group DM tile and facepiles ----
//
// Measured from Slack's CSS (c-small_mpim_entity__mpim_avatars, search
// autocomplete variant): a box of 24 (search rows) holding the first other
// member's avatar (20px, radius 4) at the top-left, and a 16px member-count
// chip (radius 4, 11px/700, #454447 on white) at (8,8) cut out of the avatar
// by a 2px ring in the row background. Selected rows draw the chip dark
// (#1d1c1d) with white text.

// mpimOthers returns the members of a group DM other than the viewer.
func mpimOthers(h Host, members []string) []string {
	self := ""
	if h.Core() != nil {
		self = h.Core().Self().ID
	}
	out := make([]string, 0, len(members))
	for _, id := range members {
		if id != self {
			out = append(out, id)
		}
	}
	return out
}

// mpimTile draws Slack's group-DM icon at the origin in a box of size dp
// (24 in search/switcher rows, 20 in compact pickers). rowBg is the colour
// behind the row (for the cut-out ring); selected darkens the chip.
func mpimTile(gtx layout.Context, h Host, members []string, size float32, rowBg color.NRGBA, selected bool) layout.Dimensions {
	th := h.Theme()
	cl := colorsOf(th)
	others := mpimOthers(h, members)
	box := dp(gtx, size)
	av := size * 20 / 24
	url := ""
	if len(others) > 0 {
		if u, ok := lookupUser(h, others[0]); ok {
			url = u.Avatar
		}
	}
	avPx := dp(gtx, av)
	if url != "" {
		if img, ok := h.Images().Get(url, avPx); ok {
			theme.Image(gtx, img, image.Pt(avPx, avPx), dp(gtx, 4))
		} else {
			rrect(gtx, image.Rect(0, 0, avPx, avPx), dp(gtx, 4), cl.Selected)
		}
	} else {
		rrect(gtx, image.Rect(0, 0, avPx, avPx), dp(gtx, 4), cl.Selected)
	}
	chip := dp(gtx, size*16/24)
	off := box - chip
	r := image.Rect(off, off, off+chip, off+chip)
	ring := dp(gtx, 2)
	rrect(gtx, r.Inset(-ring), dp(gtx, 4)+ring, rowBg)
	bg, fg := cl.Bg, cl.Fg2
	if selected {
		bg, fg = cl.Fg, cl.Bg
	}
	rrect(gtx, r, dp(gtx, 4), bg)
	if !selected {
		stroke(gtx, r, dp(gtx, 4), 1, cl.Ring)
	}
	n := strconv.Itoa(maxInt(1, len(others)))
	fs := float32(11) * size / 24
	if fs < 9 {
		fs = 9
	}
	tw := textW(gtx, th, fs, theme.Bold, n)
	at(gtx, image.Pt(off+(chip-tw)/2, off), func(gtx layout.Context) layout.Dimensions {
		return vcenter(gtx, chip, func(gtx layout.Context) layout.Dimensions {
			return text(gtx, th, fs, theme.Bold, fg, n)
		})
	})
	return layout.Dimensions{Size: image.Pt(box, box)}
}

// facepile draws up to max overlapping avatars (Slack's facepileAvatar:
// radius 6, 2px ring in rowBg, -4px overlap) of size dp; extra members
// show as a "+N" chip. Returns the width used.
func facepile(gtx layout.Context, h Host, ids []string, size float32, max int, rowBg color.NRGBA) layout.Dimensions {
	th := h.Theme()
	cl := colorsOf(th)
	px := dp(gtx, size)
	step := px - dp(gtx, 4)
	ring := dp(gtx, 2)
	n := len(ids)
	if n > max {
		n = max
	}
	x := 0
	for i := 0; i < n; i++ {
		u, _ := lookupUser(h, ids[i])
		r := image.Rect(x, 0, x+px, px)
		rrect(gtx, r.Inset(-ring), dp(gtx, 6)+ring, rowBg)
		at(gtx, r.Min, func(gtx layout.Context) layout.Dimensions {
			if u.Avatar != "" {
				if img, ok := h.Images().Get(u.Avatar, px); ok {
					return theme.Image(gtx, img, image.Pt(px, px), dp(gtx, 6))
				}
			}
			rrect(gtx, image.Rect(0, 0, px, px), dp(gtx, 6), cl.Selected)
			return layout.Dimensions{Size: image.Pt(px, px)}
		})
		x += step
	}
	if extra := len(ids) - n; extra > 0 {
		s := "+" + strconv.Itoa(extra)
		tw := textW(gtx, th, 12, theme.Bold, s)
		w := maxInt(px, tw+dp(gtx, 8))
		r := image.Rect(x, 0, x+w, px)
		rrect(gtx, r.Inset(-ring), dp(gtx, 6)+ring, rowBg)
		rrect(gtx, r, dp(gtx, 6), cl.Selected)
		at(gtx, image.Pt(x+(w-tw)/2, 0), func(gtx layout.Context) layout.Dimensions {
			return vcenter(gtx, px, func(gtx layout.Context) layout.Dimensions {
				return text(gtx, th, 12, theme.Bold, cl.Fg2, s)
			})
		})
		x += w
	} else if n > 0 {
		x += px - step
	}
	return layout.Dimensions{Size: image.Pt(x, px)}
}
