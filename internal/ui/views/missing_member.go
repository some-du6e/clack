package views

import (
	"image"
	"slices"
	"strings"
	"sync"
	"time"

	"gioui.org/io/event"
	"gioui.org/io/pointer"
	"gioui.org/op"
	"gioui.org/op/clip"
	"golang.org/x/text/collate"
	"golang.org/x/text/language"

	"github.com/some-du6e/clack/internal/core"
	"github.com/some-du6e/clack/internal/i18n"
	"github.com/some-du6e/clack/internal/ui/popups"
	"github.com/some-du6e/clack/internal/ui/theme"
)

// A member the store can't name yet, as the views draw one. Core loads
// pages without most of their people and a view looks each member up as
// it draws them (core/lazy_users.go): the Threads view's participants
// (subscriptions.thread.getView fetches nobody, as Slack's modern getView
// fetcher doesn't), an Activity thread's other participants, a DM's or a
// group DM's members, a live Activity item's people. Such a member is
// getMemberById's unknown stub until users/info answers and core's
// "names" event lists them, or a non-existent member when users/info
// didn't return the id. userName fell back to the raw U… id meanwhile,
// and Activity's rows and the Threads view's cards kept it.
//
// Slack never draws the id; what it draws depends on the surface (CDN
// build of 2026-11-14, and fa938a086569 for the Threads view's chunk;
// chunks read without a session):
//
//   - Activity's sender (ActivityRowContentSenderName, boot-shared laKt)
//     is BaseMissingMember (FToV) with no type, so its default "unknown",
//     for the stub and a non-existent member alike: MissingText's filled
//     bar and "Loading…". ReactionSender draws it while the reactor's
//     name (getPrefCompliantMemberNameById, Emmo) is "", without its
//     "+N"; ThreadSender while any other participant's name (Emmo, else
//     getBotNameById) is "".
//   - A Threads view message's sender is BaseMessageSender (/OeL): the
//     filled bar for the stub, the outlined --non-existent one for a
//     member users/info didn't return (messages/missing_member.go).
//   - A name in text is getPrefCompliantMemberNameById's, "" for both:
//     getThreadRepliersNamesFromState (client-boot V4VG) leaves a
//     participant without a name out and ThreadParticipantList (oYYP)
//     counts the named; getDisplayNameForChannel (W+Ke SB) names an IM
//     after its member, "" until they load (DeprecatedChannelName's text
//     in the Threads view's header, DmsChannel's in the DMs list), and a
//     group DM with getMpimDisplayNameAlphabetical: the members it can
//     name, sorted (QJub), the rest left out (ActivityMPDMName, VeYL).
//
// All of them draw again when the fetch upserts the member. Clack's views
// that keep what they described (Activity's rows, the Threads view's
// cards) note the members they couldn't name (base.rec, "waits"), describe
// the row again on the "names" event that lists one, and look the members
// up again each frame they show them, as Slack's components call
// getMemberById on every render (a lookup whose fetch failed asks again).

// missingBarW is MissingText's medium width (sSUQ: small 80, medium 120,
// large 160): BaseMissingMember passes no size.
const missingBarW = 120

// missingTipDelay is MissingText's Tooltip delay: BaseTooltip's default.
const missingTipDelay = 150 * time.Millisecond

var nsMemberToken = i18n.NS("member_token") // BaseMissingMember (FToV)

// missingTip is BaseMissingMember's tooltip: "Loading…" for the unknown
// type, "Private user info" for a non-existent member (gone).
func missingTip(gone bool) string {
	if gone {
		return nsMemberToken.T("Private user info")
	}
	return nsMemberToken.T("Loading…")
}

// missTip is a bar's tooltip: its hover and timing.
type missTip struct {
	hovered bool
	tip     popups.TipState
}

// memberState is member id as getMemberById answers a view, read as it
// reads (a hit is an access, a miss is asked for): in the store, the
// unknown stub, or non-existent (users/info didn't return it; an id that
// isn't a member's, or none, getMemberById makes non-existent at once).
func (b *base) memberState(id string) core.MemberState {
	if _, ok := b.user(id); ok {
		return core.MemberKnown
	}
	if !core.LooksLikeMemberID(id) {
		return core.MemberNonExistent
	}
	return b.h.Core().MissingMember(id)
}

// missingBar draws BaseMissingMember (MissingText, medium) where a name
// that line draws would go: line's box for an empty name, so the row
// keeps its height, and the 120×8 bar (border-radius 1000px) with its
// bottom on that line's baseline, as wide as fits. The unknown type
// fills it with --dt_color-base-ter; gone (--non-existent) is a 1px
// rgb(--sk_foreground_low_solid) line inside it (border-box). tip, when
// not nil, is MissingText's tooltip over the bar.
func (b *base) missingBar(gtx C, gone bool, tip *missTip, line func(gtx C) D) D {
	m := op.Record(gtx.Ops)
	d := line(gtx)
	m.Stop()
	bl := d.Size.Y - d.Baseline
	w := max(0, min(gtx.Dp(missingBarW), gtx.Constraints.Max.X))
	r := image.Rect(0, max(0, bl-gtx.Dp(8)), w, bl)
	rad := r.Dy() / 2
	if gone {
		border(gtx, r, rad, max(1, gtx.Dp(1)), b.p.MissingLine)
	} else {
		rrect(gtx, r, rad, b.p.Skel)
	}
	if tip != nil && !r.Empty() {
		tip.layout(gtx, b.th, r, gone)
	}
	return D{Size: gtx.Constraints.Constrain(image.Pt(w, d.Size.Y)), Baseline: d.Baseline}
}

// layout is MissingText's Tooltip (F2XN, above) over the bar at r.
func (t *missTip) layout(gtx C, th *theme.Theme, r image.Rectangle, gone bool) {
	if t.tip.Delay == 0 {
		t.tip.Delay = missingTipDelay
	}
	for {
		ev, ok := gtx.Event(pointer.Filter{Target: t, Kinds: pointer.Enter | pointer.Leave | pointer.Cancel})
		if !ok {
			break
		}
		if e, ok := ev.(pointer.Event); ok {
			t.hovered = e.Kind == pointer.Enter
		}
	}
	st := op.Offset(r.Min).Push(gtx.Ops)
	area := clip.Rect{Max: r.Size()}.Push(gtx.Ops)
	event.Op(gtx.Ops, t)
	area.Pop()
	popups.Tooltip(gtx, th, &t.tip, t.hovered, r.Size(), popups.Above, missingTip(gone), "")
	st.Pop()
}

// noteWait records id in the description being built (rec), if any: a
// member it drew without a name that users/info hasn't answered for, or
// a bot whose name bots.info is fetching.
func (b *base) noteWait(id string) {
	if b.rec != nil && id != "" && !slices.Contains(*b.rec, id) {
		*b.rec = append(*b.rec, id)
	}
}

// recording runs describe with rec on and returns what it noted: the
// members (and bots) the description couldn't name yet.
func (b *base) recording(describe func()) []string {
	var waits []string
	prev := b.rec
	b.rec = &waits
	describe()
	b.rec = prev
	return waits
}

// noteRead records id among the members the description being built
// looked up (recordingReads), if one is.
func (b *base) noteRead(id string) {
	if b.reads != nil && id != "" && !slices.Contains(*b.reads, id) {
		*b.reads = append(*b.reads, id)
	}
}

// recordingReads is recording that also returns every member the
// description looked up, named or not: its sender, a DM's partner, the
// people its preview mentions. A profile change to one of them (a "user"
// event) leaves what was described from them out of date, as Slack's
// rows render again when the member store changes.
func (b *base) recordingReads(describe func()) (waits, reads []string) {
	prev := b.reads
	b.reads = &reads
	waits = b.recording(describe)
	b.reads = prev
	return waits, reads
}

// waitOver looks the members and bots in waits up again, as every render
// of Slack's components calls getMemberById (a miss is asked for, so a
// fetch that failed is asked again), and reports whether one of them can
// be named now or is known not to exist: what was described from them
// is out of date.
func (b *base) waitOver(waits []string) bool {
	over := false
	for _, id := range waits {
		if core.LooksLikeMemberID(id) {
			if b.memberState(id) != core.MemberUnknown {
				over = true
			}
		} else if b.h.Core().S.BotName(id) != "" {
			over = true
		}
	}
	return over
}

// namesAny reports whether a "names" event's ids hold one of waits.
func namesAny(waits, ids []string) bool {
	for _, id := range waits {
		if slices.Contains(ids, id) {
			return true
		}
	}
	return false
}

// personName is a participant's name in text: getPrefCompliantMemberNameById,
// else (an id that isn't a member's) getBotNameById; "" while either
// isn't loaded (both ask for it, and rec notes it).
func (b *base) personName(id string) string {
	if n := b.userName(id); n != "" || core.LooksLikeMemberID(id) {
		return n
	}
	n := b.h.Core().S.BotName(id)
	if n == "" {
		b.noteWait(id)
	}
	return n
}

// nameCollator is QJub's sorter, Intl.Collator("en-US", {numeric: true});
// a collator isn't safe for concurrent use.
var nameCollator struct {
	sync.Mutex
	c *collate.Collator
}

// SortNames sorts names as Slack's QJub sorter does (en-US, numeric): a
// group DM's members in its name (getMpimDisplayNameAlphabetical).
func SortNames(names []string) { sortNames(names) }

// sortNames sorts names as QJub's sorter does.
func sortNames(names []string) {
	if len(names) < 2 {
		return
	}
	nameCollator.Lock()
	defer nameCollator.Unlock()
	if nameCollator.c == nil {
		nameCollator.c = collate.New(language.AmericanEnglish, collate.Numeric)
	}
	nameCollator.c.SortStrings(names)
}

// joinNamed joins the non-empty names with ", ": a list of people in
// text, the ones without a name yet left out.
func joinNamed(names []string) string {
	return strings.Join(slices.DeleteFunc(names, func(s string) bool { return s == "" }), ", ")
}
