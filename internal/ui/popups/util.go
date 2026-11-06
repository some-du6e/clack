package popups

import (
	"image"
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
	return h.Core().User(id)
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
func clock(t time.Time) string { return t.Format("3:04 PM") }

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
	if h.Core() != nil {
		if s := h.Core().Snapshot().Team.Domain; s != "" {
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

// msgRoute is the Navigate route for a message (thread replies open the
// thread).
func msgRoute(m core.Message) string {
	if m.ThreadTS != "" && m.ThreadTS != m.TS {
		return "c/" + m.Channel + "/t/" + m.ThreadTS
	}
	return "c/" + m.Channel
}
