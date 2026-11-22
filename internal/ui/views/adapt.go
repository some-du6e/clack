package views

// Adapters to sibling packages (icons, richview) and core lookups, kept in
// one file so contract drift is fixed in one place.

import (
	"image"
	"image/color"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"gioui.org/op"
	"gioui.org/text"
	"gioui.org/unit"

	"github.com/some-du6e/clack/internal/core"
	"github.com/some-du6e/clack/internal/richtext"
	"github.com/some-du6e/clack/internal/ui/icons"
	"github.com/some-du6e/clack/internal/ui/openurl"
	"github.com/some-du6e/clack/internal/ui/richview"
)

// icon draws a named vector icon from internal/ui/icons.
func (b *base) icon(gtx C, name string, size unit.Dp, col color.NRGBA) D {
	if iconDrawn != nil {
		iconDrawn(name)
	}
	return icons.Get(name)(gtx, size, col)
}

// iconDrawn and emojiDrawn, when set (tests, UI goroutine), see every icon
// b.icon draws and every emoji image b.emojiImg draws (its URL and pixel
// size): what one row drew, not what it described.
var (
	iconDrawn  func(name string)
	emojiDrawn func(url string, px int)
)

// body draws a document as plain text (for docs without a message key,
// e.g. drafts) clipped to maxLines (0 = all). Prefer msgBody for messages.
func (b *base) body(gtx C, doc richtext.Doc, fallback string, maxLines int) D {
	t := b.plainText(doc)
	if strings.TrimSpace(t) == "" {
		t = fallback
	}
	return b.para(gtx, 15, 22, wReg, b.p.Text, maxLines, t)
}

// preview returns a one-line plain-text preview of a message (b.preview
// reads its mentions as it draws).
func preview(m *core.Message) string { return previewWith(m, richtext.PlainText) }

func previewWith(m *core.Message, plain func(richtext.Doc) string) string {
	if m == nil {
		return ""
	}
	t := oneLine(plain(m.Doc))
	if t == "" {
		t = oneLine(m.Text)
	}
	return t
}

// plainText is richtext.PlainText with each user mention read from the
// store as it's drawn, as Slack's MemberSlug reads its member on render
// (richview's MemberRead): "@name", nothing for a member the store can't
// name yet (BaseMissingMember's bar adds no text; the lookup asks, and
// "names" draws it again), never the "@U…" the doc was built with. An id
// that isn't a member's keeps the parsed text, as MemberSlug draws it. A
// message slug reads its label the same way (richview.SlugText: the
// linked message's sender, as SimpleMessagePreview's MessageMention
// reads it).
func (b *base) plainText(d richtext.Doc) string { return richtext.PlainTextWith(d, b.mentionText) }

func (b *base) mentionText(s richtext.Span) (string, bool) {
	if s.Kind == richtext.MessageMention {
		return richview.SlugText(s)
	}
	if s.Kind != richtext.User || !core.LooksLikeMemberID(s.ID) {
		return "", false
	}
	if n := b.userName(s.ID); n != "" {
		return "@" + strings.TrimPrefix(n, "@"), true
	}
	return "", true
}

// ---- core lookups ----

// user is getMemberById: the member if the store holds it (a miss is
// asked for; "names" follows the answer). A miss while a row is
// described is noted (missing_member.go) when users/info hasn't answered
// for the id yet.
func (b *base) user(id string) (core.User, bool) {
	if id == "" {
		return core.User{}, false
	}
	c := b.h.Core()
	u, ok := c.User(id)
	if ok {
		u = b.viewUser(u)
	} else if b.rec != nil && core.LooksLikeMemberID(id) && c.MissingMember(id) == core.MemberUnknown {
		b.noteWait(id)
	}
	b.noteRead(id) // missing_member.go
	return u, ok
}

// userName is the display name Slack shows (display name, else real
// name): getPrefCompliantMemberNameById, "" for a member the store can't
// name (the unknown stub while users/info runs, or a non-existent one),
// never the id. Where a name goes, the surfaces draw Slack's placeholder
// for "" (missing_member.go).
func (b *base) userName(id string) string {
	if u, ok := b.user(id); ok {
		return displayName(u)
	}
	return ""
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
			return dmFallbackName()
		}
		return r.Name
	case "mpim":
		if n, ok := b.h.Core().MpdmCustomName(r.ID); ok { // mpdm_naming
			return n
		}
		if c, ok := b.h.Core().Conv(r.ID); ok && len(c.Members) > 0 {
			return b.groupNames(c.Members)
		}
		return mpimName(r.Name)
	case "channel", "private":
		// getDisplayNameForChannel (channel_entity.go): "#name", a
		// private channel's bare name; a channel the store can't name yet
		// is looked up (core.InlineChannel) and reads formatChannelName's
		// "unknown-channel" meanwhile, never its id
		return b.refEnt(r).Disp
	}
	if r.Name != "" && r.Name != r.ID {
		return "#" + r.Name
	}
	return unknownChannelName()
}

// isDMKind reports whether a conversation kind is a direct message.
func isDMKind(kind string) bool { return kind == "im" || kind == "mpim" }

// groupNames is getMpimDisplayNameAlphabetical (W+Ke Bp): the names of
// the group DM's members other than the viewer, sorted as QJub sorts
// them; a member the store can't name yet is left out (the stub's name
// is "": its lookup asked for it, and "names" draws the title again).
func (b *base) groupNames(members []string) string {
	self := b.selfID()
	var names []string
	for _, id := range members {
		if id == self {
			continue
		}
		if n := b.userName(id); n != "" {
			names = append(names, n)
		}
	}
	sortNames(names)
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

// shownTracker tells a view's event goroutine whether the view is on
// screen: the view marks the host's frame count as it draws (a host
// without one counts as always on screen). A view's subscription outlives
// its page, and one left behind redrew the window for every event about
// it while another page showed (a busy workspace's socket: many a second).
type shownTracker struct{ seen atomic.Uint64 }

type frameCounter interface{ FrameSeq() uint64 }

// mark notes that the view draws in the host's current frame (UI
// goroutine, from Layout).
func (s *shownTracker) mark(h Host) {
	if fc, ok := h.(frameCounter); ok {
		s.seen.Store(fc.FrameSeq())
	}
}

// shown reports whether the view drew in the latest frame (or the one being
// laid out); any goroutine.
func (s *shownTracker) shown(h Host) bool {
	fc, ok := h.(frameCounter)
	if !ok {
		return true
	}
	return s.seen.Load()+1 >= fc.FrameSeq()
}

// clear drops the staleness a load covers: its answer has what happened
// before it went out.
func (w *watcher) clear() {
	w.mu.Lock()
	w.stale = false
	w.mu.Unlock()
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
	at  time.Time // the frame it was laid out in last (gtx.Now)
	// ctx sees the context presses on the body (content_menu.go)
	ctx bodyTag
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
	ver := b.msgVerOf(m)
	if m != orig {
		ver += ":p" // a plugin's view of it: never the source's layout
	}
	// muted (read Activity rows) only dims the body: mentions keep their
	// pills like Slack; Muted is for system messages.
	// Activity, Threads and Later are Slack's activity / all_threads /
	// starred_message block contexts: long sections truncate.
	// "(edited)": shouldDisplayAsEdited (none on an app's message)
	o := richview.Options{Edited: m.DisplaysEdited(), Compact: compact, Muted: m.System, Secondary: muted && !m.System,
		Channel: m.Channel, TS: m.TS, Truncate: true}
	if live {
		o.Elements = b.liveElements(m)
		o.Media = b.mediaFor(orig) // a lone video plays in place (media.go)
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
	e.at = gtx.Now
	b.notePress(gtx, e)
	if maxLines <= 0 {
		rec := op.Record(gtx.Ops)
		d := e.v.Layout(gtx, b.style(), hostHandler{b.h})
		bodyParent(gtx, &e.ctx, d.Size, rec.Stop())
		return d
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
		top := d.Size.Y - d.Baseline // the first line's baseline from the top stays put
		d.Size.Y = limit
		if d.Baseline != 0 {
			d.Baseline = max(0, d.Size.Y-top)
		}
	}
	bodyParent(gtx, &e.ctx, d.Size, call) // clipped to the lines shown
	return d
}

// hostHandler routes richview clicks to the Host.
type hostHandler struct{ h Host }

func (hh hostHandler) OpenURL(u string) { hh.follow(u, false) }

// OpenMrkdwnLink is a mrkdwn or rich_text link's click (richview's
// MrkdwnLinkHandler: BaseMrkdwnLink opens a thread's parent in its
// thread).
func (hh hostHandler) OpenMrkdwnLink(u, _ string) { hh.follow(u, true) }

// OpenMessageMention is a message_mention pill's click: MessageMention's
// jumpToMessage with the pill's own ids.
func (hh hostHandler) OpenMessageMention(ch, ts, thread, u string) {
	if mj, ok := hh.h.(interface{ JumpToMessage(ch, ts, thread string) }); ok {
		mj.JumpToMessage(ch, ts, thread)
		return
	}
	hh.follow(u, false)
}

// follow is Slack's Link click for a link in a view (the ui root's
// FollowLink: a slack: link to handleDeepLink, a permalink through
// openPermalink), else the browser.
func (hh hostHandler) follow(u string, openRoot bool) {
	if lf, ok := hh.h.(interface{ FollowLink(string, bool) bool }); ok {
		if !lf.FollowLink(u, openRoot) {
			go func() { _ = openurl.Open(u) }()
		}
		return
	}
	if o, ok := hh.h.(interface{ OpenURL(string) }); ok {
		o.OpenURL(u)
		return
	}
	hh.h.Toast(u)
}
func (hh hostHandler) OpenUser(id string, at image.Point) { hh.h.OpenProfile(at, id) }
func (hh hostHandler) OpenChannel(id string)              { hh.h.Navigate("c/" + id) }
func (hh hostHandler) OpenUsergroup(id string, at image.Point) {
	// Slack's user group profile (the ui root's App.OpenUserGroup)
	if o, ok := hh.h.(interface{ OpenUserGroup(id string) }); ok {
		o.OpenUserGroup(id)
	}
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

// bytesHash is textHash of b, without copying it into a string.
func bytesHash(b []byte) uint32 {
	h := uint32(2166136261)
	for _, c := range b {
		h ^= uint32(c)
		h *= 16777619
	}
	return h
}

// msgVer is what a message body's cached layout (richDoc, keyed by its
// channel/ts) is made again on: its edit, text, blocks and attachments
// themselves, and the blocks drawn. Lengths alone kept an app's
// chat.update on screen as the revision before it: a vote count's digit,
// new block_ids, an unfurl's text change none of them (Karim's vote
// counts in Activity).
func msgVer(m *core.Message) string {
	return m.EditedTS + ":" + strconv.FormatUint(uint64(textHash(m.Text)), 36) + ":" +
		strconv.FormatUint(uint64(bytesHash(m.Blocks)), 36) + ":" + strconv.FormatUint(uint64(bytesHash(m.Attachments)), 36) +
		":" + strconv.Itoa(len(m.Doc.Blocks))
}

// verMemo is msgVer of a message as one revision has it, with what tells
// the revision apart from another (its text, edit, the slices its blocks
// and attachments are, its document's length) but not the copy holding
// it: a patched row's view hands a new copy of the same revision every
// frame.
type verMemo struct {
	text, edited   string
	blocks, atts   *byte // the slices' first bytes
	nBlocks, nDocs int
	ver            string
}

func firstByte(b []byte) *byte {
	if len(b) == 0 {
		return nil
	}
	return &b[0]
}

// msgVerOf is msgVer(m), worked out once per revision: the rows ask every
// frame, two or three times each, and a plugin's view of a message is a
// new copy each time.
func (b *base) msgVerOf(m *core.Message) string {
	k := m.Channel + "/" + m.TS
	if e, ok := b.vers[k]; ok && e.text == m.Text && e.edited == m.EditedTS && e.blocks == firstByte(m.Blocks) &&
		e.atts == firstByte(m.Attachments) && e.nBlocks == len(m.Blocks) && e.nDocs == len(m.Doc.Blocks) {
		return e.ver
	}
	ver := msgVer(m)
	if b.vers == nil || len(b.vers) > 2000 {
		b.vers = map[string]verMemo{}
	}
	b.vers[k] = verMemo{text: m.Text, edited: m.EditedTS, blocks: firstByte(m.Blocks), atts: firstByte(m.Attachments),
		nBlocks: len(m.Blocks), nDocs: len(m.Doc.Blocks), ver: ver}
	return ver
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
