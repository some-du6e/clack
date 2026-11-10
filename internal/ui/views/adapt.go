package views

// Adapters to sibling packages (icons, richview) and core lookups, kept in
// one file so contract drift is fixed in one place.

import (
	"image"
	"image/color"
	"strconv"
	"strings"
	"sync"
	"time"

	"gioui.org/op"
	"gioui.org/op/clip"
	"gioui.org/text"
	"gioui.org/unit"

	"github.com/some-du6e/clack/internal/core"
	"github.com/some-du6e/clack/internal/richtext"
	"github.com/some-du6e/clack/internal/ui/icons"
	"github.com/some-du6e/clack/internal/ui/richview"
)

// icon draws a named vector icon from internal/ui/icons.
func (b *base) icon(gtx C, name string, size unit.Dp, col color.NRGBA) D {
	return icons.Get(name)(gtx, size, col)
}

// body draws a document as plain text (for docs without a message key,
// e.g. drafts) clipped to maxLines (0 = all). Prefer msgBody for messages.
func (b *base) body(gtx C, doc richtext.Doc, fallback string, maxLines int) D {
	t := richtext.PlainText(doc)
	if strings.TrimSpace(t) == "" {
		t = fallback
	}
	return b.para(gtx, 15, 22, wReg, b.p.Text, maxLines, t)
}

// preview returns a one-line plain-text preview of a message.
func preview(m *core.Message) string {
	if m == nil {
		return ""
	}
	t := oneLine(richtext.PlainText(m.Doc))
	if t == "" {
		t = oneLine(m.Text)
	}
	return t
}

// ---- core lookups ----

func (b *base) user(id string) (core.User, bool) {
	if id == "" {
		return core.User{}, false
	}
	u, ok := b.h.Core().User(id)
	if ok {
		u = b.viewUser(u)
	}
	return u, ok
}

// userName is the display name Slack shows (display name, else real name).
func (b *base) userName(id string) string {
	if u, ok := b.user(id); ok {
		return displayName(u)
	}
	return id
}

func displayName(u core.User) string {
	if u.Name != "" {
		return u.Name
	}
	if u.RealName != "" {
		return u.RealName
	}
	return u.Handle
}

// authorOf resolves the name and avatar of a message author, honoring bot
// and custom-username overrides.
func (b *base) authorOf(m *core.Message) (name, avatar string) {
	if m == nil {
		return "", ""
	}
	m = b.viewMsg(m)
	name, avatar = m.Name, m.Avatar
	if u, ok := b.user(m.User); ok {
		if name == "" {
			name = displayName(u)
		}
		if avatar == "" {
			avatar = u.Avatar
		}
	}
	return name, avatar
}

func (b *base) selfID() string { return b.h.Core().Self().ID }

// convTitle names a conversation the way Slack does in list context:
// "#general", a person's name, or a comma list for group DMs.
func (b *base) convTitle(id string, refs map[string]core.ConvRef) string {
	if r, ok := refs[id]; ok {
		return b.refTitle(r)
	}
	if c, ok := b.h.Core().Conv(id); ok {
		return b.refTitle(core.ConvRef{ID: c.ID, Name: c.Name, Kind: c.Kind, User: c.User})
	}
	return b.refTitle(b.h.Core().ConvRef(id))
}

func (b *base) refTitle(r core.ConvRef) string {
	switch r.Kind {
	case "im":
		if r.User != "" {
			return b.userName(r.User)
		}
		if r.Name == "" || r.Name == r.ID {
			return "Direct message"
		}
		return r.Name
	case "mpim":
		if c, ok := b.h.Core().Conv(r.ID); ok && len(c.Members) > 0 {
			return b.groupNames(c.Members)
		}
		return mpimName(r.Name)
	case "channel", "private":
		name := b.viewChannelName(r.ID, r.Name)
		if name == "" {
			return r.ID
		}
		return "#" + name
	}
	if r.Name != "" {
		return "#" + r.Name
	}
	return r.ID
}

// isDMKind reports whether a conversation kind is a direct message.
func isDMKind(kind string) bool { return kind == "im" || kind == "mpim" }

// groupNames lists group-DM members other than the viewer.
func (b *base) groupNames(members []string) string {
	self := b.selfID()
	var names []string
	for _, id := range members {
		if id == self {
			continue
		}
		names = append(names, b.userName(id))
	}
	return strings.Join(names, ", ")
}

// mpimName turns "mpdm-a--b--c-1" into "a, b, c".
func mpimName(n string) string {
	n = strings.TrimPrefix(n, "mpdm-")
	if i := strings.LastIndex(n, "-"); i > 0 {
		n = n[:i]
	}
	return strings.Join(strings.Split(n, "--"), ", ")
}

// ---- realtime ----

// watcher marks a view stale when matching core events arrive, so the view
// reloads on its next frame. Events are drained on a background goroutine.
type watcher struct {
	mu    sync.Mutex
	stale bool
	last  time.Time
	on    bool
}

// watch subscribes once; match runs on the event goroutine and must only
// read the event.
func (w *watcher) watch(h Host, match func(ev core.Event) bool) {
	w.mu.Lock()
	if w.on {
		w.mu.Unlock()
		return
	}
	w.on = true
	w.mu.Unlock()
	ch, _ := h.Core().Subscribe()
	go func() {
		for ev := range ch {
			if match(ev) {
				w.mu.Lock()
				w.stale = true
				w.mu.Unlock()
				h.Invalidate()
			}
		}
	}()
}

// due reports (and clears) staleness at most once per minGap.
func (w *watcher) due(gtx C, minGap time.Duration) bool {
	w.mu.Lock()
	defer w.mu.Unlock()
	if !w.stale {
		return false
	}
	if left := minGap - gtx.Now.Sub(w.last); left > 0 {
		gtx.Execute(op.InvalidateCmd{At: gtx.Now.Add(left)})
		return false
	}
	w.stale, w.last = false, gtx.Now
	return true
}

// ---- rich message bodies (internal/ui/richview) ----

// One richview.Style per shaper, shared by every view (UI goroutine only).
var (
	richStyle  *richview.Style
	richShaper *text.Shaper
	richDark   bool
	richGen    uint64 // th.Gen the palette was set at (stylesheet tokens)
)

func (b *base) style() *richview.Style {
	dark := b.th.Dark
	if richStyle == nil || richShaper != b.th.Shaper {
		var imgs richview.Images
		if c := b.h.Images(); c != nil {
			imgs = c
		}
		p := richview.LightPalette()
		if dark {
			p = richview.DarkPalette()
		}
		richStyle, richShaper, richDark = richview.NewStyle(b.th.Shaper, imgs, p), b.th.Shaper, dark
		richGen = 0
	}
	if dark != richDark || richGen != b.th.Gen {
		richDark, richGen = dark, b.th.Gen
		if dark {
			richStyle.SetPalette(richview.DarkPalette())
		} else {
			richStyle.SetPalette(richview.LightPalette())
		}
		if p, ok := richview.Themed(b.th); ok {
			richStyle.SetPalette(p)
		}
	}
	return richStyle
}

type richEntry struct {
	v   *richview.View
	ver string
}

// richCache keeps one richview.View per message for a view.
type richCache map[string]*richEntry

// msgBody draws a message's document with richview, exactly like the
// channel view. compact uses 13px/18px text. maxLines > 0 clips the body to
// that many lines (Slack's 2-3 line previews).
func (b *base) msgBody(gtx C, m *core.Message, compact bool, maxLines int) D {
	return b.msgBodyLive(gtx, m, compact, false, maxLines, true)
}

// msgBodyMuted is msgBody with the body in the secondary text color when
// muted is set (read Activity rows). Activity's Block Kit stays inert
// (elements.go).
func (b *base) msgBodyMuted(gtx C, m *core.Message, compact, muted bool, maxLines int) D {
	return b.msgBodyLive(gtx, m, compact, muted, maxLines, false)
}

// msgBodyLive draws the body; live gives its Block Kit elements the
// channel row's element host (elements.go).
func (b *base) msgBodyLive(gtx C, m *core.Message, compact, muted bool, maxLines int, live bool) D {
	if m == nil {
		return D{}
	}
	orig := m
	m = b.viewMsg(m)
	if len(m.Doc.Blocks) == 0 {
		return b.body(gtx, m.Doc, m.Text, maxLines)
	}
	key := m.Channel + "/" + m.TS
	ver := m.EditedTS + ":" + strconv.Itoa(len(m.Text)) + ":" + strconv.Itoa(len(m.Doc.Blocks))
	if m != orig {
		// a plugin's text patch can keep the length: key on the text
		ver += ":p" + strconv.FormatUint(uint64(textHash(m.Text)), 36)
	}
	// muted (read Activity rows) only dims the body: mentions keep their
	// pills like Slack; Muted is for system messages.
	// Activity, Threads and Later are Slack's activity / all_threads /
	// starred_message block contexts: long sections truncate.
	o := richview.Options{Edited: m.Edited, Compact: compact, Muted: m.System, Secondary: muted && !m.System,
		Channel: m.Channel, TS: m.TS, Truncate: true}
	if live {
		o.Elements = b.liveElements(m)
	}
	return b.richDoc(gtx, key, ver, m.Doc, o, maxLines)
}

// richDoc draws any document under a stable key.
func (b *base) richDoc(gtx C, key, ver string, doc richtext.Doc, o richview.Options, maxLines int) D {
	if b.rich == nil {
		b.rich = richCache{}
	}
	if len(b.rich) > 3000 {
		clear(b.rich)
	}
	e := b.rich[key]
	if e == nil {
		e = &richEntry{v: richview.New(doc, o), ver: ver}
		b.rich[key] = e
	} else if e.ver != ver {
		e.v.Set(doc, o)
		e.ver = ver
	} else {
		e.v.SetOptions(o)
	}
	if maxLines <= 0 {
		return e.v.Layout(gtx, b.style(), hostHandler{b.h})
	}
	lh := 22
	if o.Compact {
		lh = 18
	}
	limit := gtx.Dp(unit.Dp(lh * maxLines))
	m := op.Record(gtx.Ops)
	d := e.v.Layout(gtx, b.style(), hostHandler{b.h})
	call := m.Stop()
	if d.Size.Y > limit {
		d.Size.Y = limit
	}
	st := clip.Rect{Max: d.Size}.Push(gtx.Ops)
	call.Add(gtx.Ops)
	st.Pop()
	return d
}

// hostHandler routes richview clicks to the Host.
type hostHandler struct{ h Host }

func (hh hostHandler) OpenURL(u string) {
	if o, ok := hh.h.(interface{ OpenURL(string) }); ok {
		o.OpenURL(u)
		return
	}
	hh.h.Toast(u)
}
func (hh hostHandler) OpenUser(id string, at image.Point) { hh.h.OpenProfile(at, id) }
func (hh hostHandler) OpenChannel(id string)              { hh.h.Navigate("c/" + id) }
func (hh hostHandler) OpenUsergroup(id string, at image.Point) {
	hh.h.Toast("User group profiles aren't supported yet")
}
func (hh hostHandler) OpenImage(img richtext.Image) { hh.h.OpenLightbox(img, core.Message{}) }
func (hh hostHandler) OpenFile(f *richtext.File) {
	if f != nil && f.Thumb != nil && f.IsImage {
		hh.h.OpenLightbox(*f.Thumb, core.Message{})
		return
	}
	if f != nil {
		openFile(hh.h, fileFromRich(f)) // files.go
	}
}

// Button is a click on a Block Kit element richview drew itself: only a
// link button does anything. Live views draw their elements through the
// element host (elements.go), which sends blocks.actions; in Activity
// Slack disables them (Nc1H.DD).
func (hh hostHandler) Button(btn richtext.Button) {
	if btn.URL != "" {
		hh.OpenURL(btn.URL)
	}
}

// textHash is FNV-1a of s (patched-message cache keys).
func textHash(s string) uint32 {
	h := uint32(2166136261)
	for i := 0; i < len(s); i++ {
		h ^= uint32(s[i])
		h *= 16777619
	}
	return h
}

func firstNonEmpty(s ...string) string {
	for _, x := range s {
		if x != "" {
			return x
		}
	}
	return ""
}

// teamName is the workspace's name ("" before boot).
func (b *base) teamName() string {
	s := b.h.Core().S
	s.RLock()
	defer s.RUnlock()
	return s.TeamName
}
