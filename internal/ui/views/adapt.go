package views

// Adapters to sibling packages (icons, richview) and core lookups, kept in
// one file so contract drift is fixed in one place.

import (
	"image"
	"image/color"
	"strings"
	"sync"
	"time"

	"gioui.org/op"
	"gioui.org/unit"

	"github.com/some-du6e/yoloslackgoclient/internal/core"
	"github.com/some-du6e/yoloslackgoclient/internal/richtext"
)

// icon draws a named vector icon from internal/ui/icons.
func (b *base) icon(gtx C, name string, size unit.Dp, col color.NRGBA) D {
	s := gtx.Dp(size)
	return D{Size: image.Pt(s, s)}
}

// body draws a message document (richview when available) clipped to
// maxLines (0 = all).
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
	return b.h.Core().User(id)
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
		return r.Name
	case "mpim":
		if c, ok := b.h.Core().Conv(r.ID); ok && len(c.Members) > 0 {
			return b.groupNames(c.Members)
		}
		return mpimName(r.Name)
	case "channel", "private":
		if r.Name == "" {
			return r.ID
		}
		return "#" + r.Name
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
