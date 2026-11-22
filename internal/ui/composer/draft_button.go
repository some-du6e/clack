package composer

import (
	"image"
	"sort"
	"strconv"
	"strings"
	"time"

	"gioui.org/io/pointer"
	"gioui.org/layout"
	"gioui.org/op"
	"gioui.org/op/clip"

	"github.com/some-du6e/clack/internal/core"
	"github.com/some-du6e/clack/internal/ui/icons"
	"github.com/some-du6e/clack/internal/ui/popups"
	"github.com/some-du6e/clack/internal/ui/slackc"
	"github.com/some-du6e/clack/internal/ui/slackc/css"
	"github.com/some-du6e/clack/internal/ui/theme"
)

// The composer's drafts button: Slack's DraftManagementButton (boot-shared
// /vRM, in WysiwygContainer's suffix before the send button), under
// draft_management_in_composer (treatment: the account's group).
//
//   - It shows while the conversation (or the thread) has a draft
//     (getDraftsByDestinationIdSortedByRecency, core.DraftsFor). The one
//     being typed counts, so the first character typed brings it. It is
//     a ghost button 28px high (c-wysiwyg_container__button--
//     draft_management: padding-inline 8, gap 4, radius 4, content-sec,
//     base-ter on hover): the edit glyph at 18 and, with more than one
//     draft, their count (draftManagementCount: 12px bold), 4px left of
//     the send button (c-wysiwyg_container__suffix: gap 4).
//   - Its tooltip says "N drafts in this channel" ("… in this
//     conversation" in a DM or a group DM). While a file uploads it says
//     "Hang tight — you can save your draft once all uploads are done."
//     and the button is aria-disabled (content-ter, no hover, no menu).
//   - It opens a menu above it, right-aligned (MenuTrigger "top-right"):
//     "Drafts in this channel" (or conversation), the 4 newest drafts
//     (the composer's own is the checked radio row; a row is the draft's
//     DraftRichTextPreview truncated at 50, or "No message (N
//     attachments)"), a separator, "View N more drafts" past 4 (the
//     Drafts page) and "Save as draft".
//   - Picking another draft swaps it into the composer
//     (selectStashedDraft, stash_draft.go), focused with the caret at its
//     end. "Save as draft" (stashDraft) keeps what the composer holds as
//     a draft of its own (is_from_composer, date_stashed now) and empties
//     the composer.
//
// Not ported: the coachmark the button shows once
// (DraftManagementButtonCoachmark, "View and save draft messages": an
// education notification with server-side state Clack has no store for),
// and its place in the toolbar under
// composer_control_simplifications_formatting_toolbar (the account isn't
// in it).

// draftMgmtOn is WysiwygContainer's
// experimentDraftManagementInComposerEnabled.
func draftMgmtOn(a *core.App) bool {
	return a != nil && a.Experiment("draft_management_in_composer") == "treatment"
}

// destDraft is one of the composer's drafts: a server draft, or the one
// the composer holds (own: its text and files as they are now).
type destDraft struct {
	core.Draft
	own bool
}

// ownDraft is the draft the composer holds, as Slack's drafts store has
// it while it's typed: its text and files now. ok is false when there is
// nothing in it (draftHasContent: no text past whitespace, no files).
func (c *Composer) ownDraft() (d core.Draft, ok bool) {
	if c.ed.Empty() && c.att.Len() == 0 {
		return core.Draft{}, false
	}
	return core.Draft{ID: c.serverDraftID(), Channel: c.ch, ThreadTS: c.ts, Text: c.ed.Doc().Text(), Files: c.att.Len()}, true
}

// destDrafts is the button's list, newest first: core.DraftsFor, with the
// composer's own draft as it is now in place of its server copy. The own
// draft is the newest while it has changes not saved yet.
func (c *Composer) destDrafts() []destDraft {
	a := c.api.a
	if a == nil || c.noDraft() {
		return nil
	}
	id := c.serverDraftID()
	own, has := c.ownDraft()
	var out []destDraft
	for _, d := range a.DraftsFor(c.ch, c.ts) {
		if id != "" && d.ID == id {
			own.LastUpdated, own.DateCreated = d.LastUpdated, d.DateCreated
			continue
		}
		out = append(out, destDraft{Draft: d})
	}
	if !has {
		return out
	}
	o := destDraft{Draft: own, own: true}
	i := 0
	if own.LastUpdated != "" && c.ed.Version() == c.dsync.synced && c.attVer == c.dsync.syncedAtt {
		lu := draftTS(own.LastUpdated)
		i = sort.Search(len(out), func(i int) bool { return draftTS(out[i].LastUpdated) < lu })
	}
	return append(out[:i], append([]destDraft{o}, out[i:]...)...)
}

func draftTS(ts string) float64 {
	f, _ := strconv.ParseFloat(ts, 64)
	return f
}

// dmCountKey is what the button's count depends on.
type dmCountKey struct {
	gen uint64 // core.DraftsGen
	id  string // the server draft the composer holds
	own bool   // the composer holds something
}

// draftsCount is how many drafts the button counts (0: no button). It is
// counted again only when the drafts core knows change, or the
// composer's own draft does.
func (c *Composer) draftsCount() int {
	a := c.api.a
	if !draftMgmtOn(a) || c.noDraft() {
		return 0
	}
	k := dmCountKey{gen: a.DraftsGen(), id: c.serverDraftID(), own: !c.ed.Empty() || c.att.Len() > 0}
	if !c.dmOK || k != c.dmKey {
		c.dmKey, c.dmOK = k, true
		c.dmN = len(c.destDrafts())
	}
	return c.dmN
}

// DraftPreview is a draft's DraftRichTextPreview text: its words on one
// line, truncated at 50.
func DraftPreview(d core.Draft) string {
	t := strings.Join(strings.Fields(d.Text), " ")
	if r := []rune(t); len(r) > 50 {
		return string(r[:50]) + "…"
	}
	return t
}

// draftLabel is a draft's menu row: its text, or "No message (N
// attachments)".
func draftLabel(d core.Draft) string {
	if p := DraftPreview(d); p != "" {
		return p
	}
	n := d.Files
	if n == 0 {
		n = len(d.FileIDs)
	}
	return draftsNS.T("No message ({count, plural, one {# attachment} other {# attachments}})", "count", n)
}

// draftsTip is the button's tooltip.
func draftsTip(n int, dm, busy bool) string {
	switch {
	case busy:
		return draftsHangTight()
	case dm:
		return draftsNS.T("{count, plural, one {# draft in this conversation} other {# drafts in this conversation}}", "count", n)
	}
	return draftsNS.T("{count, plural, one {# draft in this channel} other {# drafts in this channel}}", "count", n)
}

// draftsHangTight is the tooltip (and stashDraft's toast) while a file
// still uploads.
func draftsHangTight() string {
	return draftsNS.T("Hang tight — you can save your draft once all uploads are done.")
}

// draftsMenu opens the button's menu above anchor, right-aligned
// (MenuTrigger position "top-right").
func (c *Composer) draftsMenu(anchor image.Rectangle) {
	c.h.ShowOverlay(popups.NewMenuOpts(c.h, c.draftsMenuItems(), popups.MenuOpts{Anchor: anchor, Side: popups.Above, AlignRight: true}))
}

// draftsMenuItems is the button's menu.
func (c *Composer) draftsMenuItems() []popups.MenuItem {
	ds := c.destDrafts()
	dm := false
	if cv, ok := c.api.conv(c.ch); ok {
		dm = cv.Kind == "im" || cv.Kind == "mpim"
	}
	var items []popups.MenuItem
	if len(ds) > 0 {
		head := draftsNS.T("Drafts in this channel")
		if dm {
			head = draftsNS.T("Drafts in this conversation")
		}
		items = append(items, popups.MenuItem{Key: popups.LabelKey(head), Header: head})
		for _, d := range ds[:min(len(ds), 4)] {
			d := d
			items = append(items, popups.MenuItem{Key: "texty_draft_management_draft_preview", Label: draftLabel(d.Draft),
				Radio: d.own, Checked: d.own, Do: func() {
					if !d.own {
						c.pickDraft(d.Draft)
					}
				}})
		}
		items = append(items, popups.Sep())
	}
	if more := len(ds) - 4; more > 0 {
		items = append(items, popups.MenuItem{Key: "texty_draft_management_view_all_drafts",
			Label: draftsNS.T("{count, plural, one {View # more draft} other {View # more drafts}}", "count", more),
			Do:    func() { c.h.Navigate("drafts") }})
	}
	return append(items, popups.MenuItem{Key: "texty_draft_management_save_as_draft", Label: draftsNS.T("Save as draft"),
		Disabled: c.ed.Empty() && c.att.Len() == 0 || c.dsync.stashing, Do: c.saveAsDraft})
}

// pickDraft is the menu's onSelectDraft: d takes the composer's slot
// (selectStashedDraft), focused with the caret at its end: at once, or
// once Slack has what the slot held (stash_draft.go).
func (c *Composer) pickDraft(d core.Draft) {
	if SelectStashedDraft(c.h, c, c.ch, c.ts, d) {
		c.Focus()
	}
}

// saveAsDraft is "Save as draft": stashDraft, then the composer empties.
// What the composer holds becomes a composer draft of its own, stashed
// now. A conversation's composer keeps its server draft for it (Slack
// re-keys it, draft-management-save-as-draft) and holds none after; a
// thread's saves a new one and empties, which deletes the one it held, as
// emptying it does. Nothing happens while a file uploads (a toast says to
// wait) or when the composer holds nothing.
//
// The composer empties only once Slack has the draft (stashed): Slack's
// drafts store keeps the draft locally and syncs it later, so nothing
// typed is ever dropped. Refused (Slack unreachable), everything stays
// where it was and a toast says the draft couldn't be saved.
func (c *Composer) saveAsDraft() {
	if c.noDraft() || c.dsync.stashing {
		return
	}
	if c.att.uploading() {
		c.h.Toast(draftsHangTight())
		return
	}
	if c.ed.Empty() && c.att.Len() == 0 {
		return
	}
	if c.dsync.timer != nil {
		c.dsync.timer.Stop()
	}
	args := c.draftArgs()
	args.DateStashed = time.Now().UnixMilli()
	v, av, gen := c.ed.Version(), c.attVer, c.attachGen
	c.dsync.stashing = true
	queueDraftOp(c.key(), draftOp{api: c.api, stash: &args, keep: c.ts == "", done: func(err error) {
		c.later(func() { c.stashed(v, av, gen, err) })
	}})
	c.h.Invalidate()
}

// stashed is "Save as draft"'s answer (UI goroutine), for what the
// composer held at version v, files av, slot generation gen.
func (c *Composer) stashed(v, av, gen uint64, err error) {
	c.dsync.stashing = false
	if err != nil {
		c.h.Toast("Couldn't save your draft: " + err.Error())
		c.dsync.synced = unsyncedVer // the stash was its save: sync it again
		c.retrySync()
		return
	}
	key := c.key()
	if c.closed || c.attachGen != gen || c.ed.Version() != v || c.attVer != av {
		// changed since (or closed): what the slot holds now stays, saved
		// as a draft of its own (a conversation's slot let its draft go
		// with the stash)
		if c.closed && liveComposer(key) == nil && c.ed.Version() == v {
			clearDraft(c.ch, c.ts)
		}
		c.dsync.synced = unsyncedVer
		c.syncDraftSoon()
		return
	}
	c.ed.SetDoc(NewDoc())
	c.lastVer = c.ed.Version()
	c.att.Clear() // the files went with the draft
	clearDraft(c.ch, c.ts)
	c.clearLiveDraft()
	c.broadcast = false
	c.typingStopped()
	if c.ts != "" {
		c.deleteServerDraft("composer_cleared") // the thread draft it held: the stash made its own
	} else {
		c.dsync.synced = c.ed.Version()
	}
	c.h.Invalidate()
}

// uploading reports a file still uploading (the draft's pending uploads).
func (a *attachments) uploading() bool {
	a.mu.Lock()
	defer a.mu.Unlock()
	for _, c := range a.chips {
		if c.preparing {
			return true
		}
	}
	return false
}

// ---- the button (chrome) ----

func (c *chrome) draftButtonProps() slackc.Props {
	p := c.props()
	p["side"] = "right"
	return p
}

func (c *chrome) layoutDraftButton(gtx layout.Context, p slackc.Props) layout.Dimensions {
	return c.layoutDrafts(gtx, &c.cur)
}

// layoutDrafts draws the drafts button at the gtx origin.
func (c *chrome) layoutDrafts(gtx layout.Context, st *chromeState) layout.Dimensions {
	if st.Drafts <= 0 {
		return layout.Dimensions{}
	}
	th := c.f.h.Theme()
	vars := css.SlackVars(th.Dark)
	col, ok := cssVarColor(vars, "--dt_color-content-sec")
	if !ok {
		col = rgb(0x5e5d60)
	}
	if st.DraftsBusy {
		if v, ok := cssVarColor(vars, "--dt_color-content-ter"); ok {
			col = v
		}
	}
	h, padX, is, gap := gtx.Dp(28), gtx.Dp(8), gtx.Dp(18), gtx.Dp(4)
	var count op.CallOp
	var cd layout.Dimensions
	inner := is
	if st.Drafts > 1 {
		count, cd = crRec(gtx, th.Shaper, 1<<16, textStyle{Size: 12, Weight: theme.Bold, Color: col, MaxLines: 1}, strconv.Itoa(st.Drafts))
		inner += gap + cd.Size.X
	}
	w := max(padX+inner+padX, gtx.Dp(28))
	r := image.Rect(0, 0, w, h)
	hovered := c.btn[crDrafts].Hovered()
	if hovered && !st.DraftsBusy {
		if bg, ok := cssVarColor(vars, "--dt_color-base-ter"); ok {
			fillRRect(gtx, r, gtx.Dp(4), bg)
		}
	}
	x := (w - inner) / 2
	t := op.Offset(image.Pt(x, (h-is)/2)).Push(gtx.Ops)
	icons.Get("edit")(gtx, 18, col)
	t.Pop()
	if st.Drafts > 1 {
		t := op.Offset(image.Pt(x+is+gap, (h-cd.Size.Y)/2)).Push(gtx.Ops)
		count.Add(gtx.Ops)
		t.Pop()
	}
	c.hit(gtx, crDrafts, r, !st.DraftsBusy)
	if st.DraftsBusy { // aria-disabled: cursor not-allowed, still hovered for the tooltip
		a := clip.Rect(r).Push(gtx.Ops)
		pointer.CursorNotAllowed.Add(gtx.Ops)
		a.Pop()
	}
	popups.Tooltip(gtx, th, &c.tip[crDrafts], hovered, r.Size(), popups.Above, draftsTip(st.Drafts, st.IsDM, st.DraftsBusy), "")
	return layout.Dimensions{Size: r.Size()}
}
