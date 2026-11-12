package popups

import (
	"context"
	"encoding/json"
	"image"
	"sort"
	"strings"
	"time"

	"gioui.org/io/event"
	"gioui.org/io/key"
	"gioui.org/io/pointer"
	"gioui.org/layout"
	"gioui.org/op/clip"
	"gioui.org/unit"
	"gioui.org/widget"

	"github.com/some-du6e/clack/internal/core"
	"github.com/some-du6e/clack/internal/ui/theme"
)

// The share ("forward") dialog, Slack's MessageShareDialog (gantry-v2-
// async-message-share-dialog jdUv, build 4d0c78085232): a conversation
// picker (ShareDialogChannelPicker eKtn, "Search for channel or person";
// fuzzy over the sidebar's conversations, DMs included; picks become
// chips), an optional message ("Add a message, if you’d like."), a
// quoted preview, and the footer: ShareDialogCopyLinkButton (Zp0h) on the
// left; on the right Save Draft (forwarding_save_as_draft) and Forward,
// which under schedule_send_forwards carries a "Schedule for later" caret
// (Slack Kit's ButtonGroup with slack_kit_v2_button, else
// .p-share_dialog__forward_button--with_options) opening ScheduleSendMenu
// (BYKP). Forwarding posts the optional note plus the permalink, which
// Slack unfurls as a shared message; scheduleForwardMessage and saveDraft
// put the same in a draft (the permalink inserted after the note's ops).
// Group DMs show Slack's member tile (mpimTile) and are titled and matched
// by their members' names.
//
// The title (e3): "Share this message", or with the team's
// private_message_forwarding (Db0x) "Forward this message" in a public
// channel and "Forward this private message" elsewhere. Copy Link's
// tooltip explains the link ("…To forward, paste it…" / "…To share…")
// and says "Copied!" once clicked, until it hides; Slack toasts only for
// canvases and lists. The picker's options list (SelectOptionsList Fd8o)
// has 32px rows (optionsRowHeight) and scrolls past 200px
// (maxOptionsListHeight).

// actFwdMax caps the local matches the picker lists (Clack matches the
// sidebar's conversations itself; Slack asks its searcher). The list
// shows 32px rows and scrolls past 200px.
const actFwdMax = 50

// The options list's geometry: optionsRowHeight 32, maxOptionsListHeight
// 200 (dp).
const (
	actFwdRowH    = 32
	actFwdListMax = 200
)

type actFwdTarget struct {
	id, title, lower, kind, avatar string
	members                        []string // group DMs: every member
	keys                           []string // extra lowercase match keys (members' real names, handles)
}

type actFwdChip struct {
	t  *actFwdTarget
	rm widget.Clickable
}

type actForward struct {
	h     Host
	m     core.Message
	modal *Modal
	fwd   *ModalButton
	caret *ModalButton // the schedule caret (schedule_send_forwards)
	copy  *ModalButton
	link  string

	to, note widget.Editor
	toBox    widget.Clickable
	noteBox  widget.Clickable
	all      []actFwdTarget
	match    []int
	sel      int
	sugg     [actFwdMax]widget.Clickable
	sugList  layout.List
	chips    []*actFwdChip
	query    string
	focusTo  bool
	copyLink bool
	copied   bool // Copy Link's "Copied!" until its tooltip hides
	hov      int  // the suggestion under the pointer last frame (-1 none)
}

// Experiments the share dialog reads (jdUv, O2By jv).
const (
	ExpScheduleSendForwards  = "schedule_send_forwards"
	ExpForwardingSaveAsDraft = "forwarding_save_as_draft"
	expUpdatedScheduledSends = "updated_scheduled_sends_desktop"
)

// ScheduleSendMenu, set by the ui root to the composer's, opens Slack's
// ScheduleSendMenu (boot-shared BYKP) at anchor for a message to
// channelID that has no draft yet (the share dialog's draftId ""): its
// rows, and under updated_scheduled_sends_desktop the "{time} for
// {member}" lines of getScheduledChannelTimeZoneDetails (Mluw). pick
// gets a preset's time; custom is "Custom time".
var ScheduleSendMenu func(h Host, anchor image.Rectangle, channelID string, pick func(at time.Time), custom func())

// OpenScheduleDialog, set by the ui root to the composer's, opens Slack's
// ScheduleMessageDialog (z1vG) for a draft to channelID; pick gets the
// time chosen with Schedule Message.
var OpenScheduleDialog func(h Host, channelID string, pick func(at time.Time))

// IconToaster is a Host that shows Slack's Toast (c-toast, 4HH1: bottom
// right, 20px in, the icon 20px before the text) with an icon.
type IconToaster interface{ ToastIcon(text, icon string) }

// toastIcon shows text in Slack's Toast with icon where the host can,
// else as a plain toast.
func toastIcon(h Host, text, icon string) {
	if t, ok := h.(IconToaster); ok {
		t.ToastIcon(text, icon)
		return
	}
	h.Toast(text)
}

// NewForwardModal builds the forward dialog for m. Show it with
// h.ShowOverlay.
func NewForwardModal(h Host, m core.Message) *Modal { return newForward(h, m).modal }

func newForward(h Host, m core.Message) *actForward {
	f := &actForward{h: h, m: m, focusTo: true, hov: -1}
	f.to.SingleLine = true
	f.to.Submit = true
	f.sugList.Axis = layout.Vertical
	f.link = m.Permalink
	if f.link == "" {
		f.link = permalink(h, m.Channel, m.TS, m.ThreadTS)
	}
	f.all = actFwdTargets(h)
	v2 := func() bool { return expOn(h, ExpSlackKitV2Button) }
	label := nsShare.T("Share") // e0: "Forward" with private_message_forwarding
	if privateForwarding(h.Core()) {
		label = nsShare.T("Forward")
	}
	f.fwd = &ModalButton{Label: label, Kind: BtnPrimary, Enabled: f.canGo, Do: func() bool {
		f.send()
		return true
	},
		// the share dialog's go button: Slack Kit's v2 Button under
		// slack_kit_v2_button (jdUv: ex = W ? ey : ev), the legacy one else
		V2: v2,
	}
	// ShareDialogCopyLinkButton (Zp0h): the legacy outline button
	// whatever the experiment, labelled "Copy Link", inside a Tooltip
	f.copy = &ModalButton{Label: nsShare.T("Copy Link"), Kind: BtnOutline, Left: true, Do: func() bool {
		f.copyLink = true // copied by the body, which has the frame context
		return false
	}, Tip: f.copyTip, TipHidden: func() { f.copied = false }}
	buttons := []*ModalButton{f.copy}
	if f.saveDraftShown() {
		// Save Draft (e5): v2 secondary, or the legacy outline
		buttons = append(buttons, &ModalButton{Label: nsShare.T("Save Draft"), Kind: BtnOutline, V2: v2, Do: func() bool {
			f.saveDraft()
			return true
		}})
	}
	buttons = append(buttons, f.fwd)
	if expOn(h, ExpScheduleSendForwards) {
		// renderForwardActions: the caret ("Schedule for later",
		// caret-down, data-qa share_dialog_schedule_send_button), off
		// with Forward
		f.caret = &ModalButton{Icon: "caret-down", Kind: BtnPrimary, Joined: true, Enabled: f.canGo, V2: v2, Draw: f.drawCaret,
			Do: func() bool {
				f.openSchedule()
				return false
			}}
		buttons = append(buttons, f.caret)
	}
	f.modal = NewModal(h, ModalOpts{
		TitleFunc: f.title,
		Body:      f.layout,
		Buttons:   buttons,
		FooterPad: 24, // c-sk-modal_footer: 84px, padding 24px 28px
	})
	return f
}

// canGo is Forward's enabled state: a destination chosen (Slack's ea:
// canPostInSelectedChannel and nothing else in the way).
func (f *actForward) canGo() bool { return len(f.chips) > 0 }

// title is e3's: "Share this message", or with private message
// forwarding "Forward this message" from a public channel, else
// "Forward this private message".
func (f *actForward) title() string {
	if !privateForwarding(f.h.Core()) {
		return nsShare.T("Share this message")
	}
	if a := f.h.Core(); a != nil {
		if c, ok := a.Conv(f.m.Channel); ok && c.Kind == "channel" {
			return nsShare.T("Forward this message")
		}
	} else if actKindOf(f.m.Channel) == "channel" {
		return nsShare.T("Forward this message")
	}
	return nsShare.T("Forward this private message")
}

// copyTip is Copy Link's tooltip: "Copied!" after a click, else what
// the link is for.
func (f *actForward) copyTip() string {
	if f.copied {
		return nsShare.T("Copied!")
	}
	if privateForwarding(f.h.Core()) {
		return nsShare.T("You can also copy a link to this message. To forward, paste it in any channel or conversation.")
	}
	return nsShare.T("You can also copy a link to this message. To share, paste it in any channel or conversation.")
}

// saveDraftShown is el: forwarding_save_as_draft (O2By jv), outside the
// story viewer, for a message that isn't an AI answer.
func (f *actForward) saveDraftShown() bool { return expOn(f.h, ExpForwardingSaveAsDraft) }

// drawCaret draws the schedule caret: Slack Kit's icon-only Button, last
// in the ButtonGroup, under slack_kit_v2_button; else the legacy
// c-icon_button p-share_dialog__schedule_send_button (36px,
// base-inv-hgl-2 with a white caret, the start corners square; disabled
// base-ter and content-sec).
func (f *actForward) drawCaret(gtx layout.Context, th *theme.Theme, c *widget.Clickable, kind BtnKind, ring bool, grp grpSide) layout.Dimensions {
	if expOn(f.h, ExpSlackKitV2Button) {
		return buttonV2Grp(gtx, th, c, kind, theme.ButtonV2Medium, "", "caret-down", ring, grp)
	}
	cl := colorsOf(th)
	sz := image.Pt(dp(gtx, 36), dp(gtx, 36))
	draw := func(gtx layout.Context) layout.Dimensions {
		bg, fg := cl.Primary, cl.PrimaryText
		switch {
		case kind == BtnDisabled:
			bg, fg = cl.Disabled, cl.DisText
		case c.Hovered():
			bg = theme.Mix(bg, rgb(0x000000), .1)
		}
		if ring {
			focusRing(gtx, cl, image.Rectangle{Max: sz}, dp(gtx, 8))
		}
		rrectGrp(gtx, image.Rectangle{Max: sz}, dp(gtx, 8), grpLast, bg)
		at(gtx, image.Pt((sz.X-dp(gtx, 20))/2, (sz.Y-dp(gtx, 20))/2), func(gtx layout.Context) layout.Dimensions {
			return icon(gtx, "caret-down", 20, fg) // font-size calc(.5 * 36px + 2px)
		})
		if kind != BtnDisabled {
			pointerCursor(gtx, sz)
		}
		return layout.Dimensions{Size: sz}
	}
	if kind == BtnDisabled {
		return draw(gtx)
	}
	return c.Layout(gtx, draw)
}

// targets are the chosen conversations' ids.
func (f *actForward) targets() []string {
	ids := make([]string, len(f.chips))
	for i, c := range f.chips {
		ids[i] = c.t.id
	}
	return ids
}

// message is what the forward posts: the note, then the permalink.
func (f *actForward) message() (text string, blocks json.RawMessage) {
	note := strings.TrimSpace(f.note.Text())
	return fwdText(note, f.link), fwdBlocks(note, f.link)
}

// fwdText is the forward's text: the note and the permalink on its own
// line.
func fwdText(note, link string) string {
	if note == "" {
		return link
	}
	return note + "\n" + link
}

// fwdBlocks is the draft scheduleForwardMessage and saveDraft build: the
// note's ops with the permalink inserted after them (Delta.insert(link,
// {link})), as rich text.
func fwdBlocks(note, link string) json.RawMessage {
	var els []any
	if note != "" {
		els = append(els, map[string]any{"type": "text", "text": note + "\n"})
	}
	els = append(els, map[string]any{"type": "link", "url": link})
	b, _ := json.Marshal([]any{map[string]any{"type": "rich_text", "elements": []any{
		map[string]any{"type": "rich_text_section", "elements": els},
	}}})
	return b
}

func (f *actForward) send() {
	ids := f.targets()
	msg, _ := f.message()
	actDo(f.h, "Message forwarded", func(ctx context.Context, a *core.App) error {
		for _, id := range ids {
			if _, err := a.Send(ctx, core.SendOpts{Channel: id, Text: msg}); err != nil {
				return err
			}
		}
		return nil
	})
}

// openSchedule is the caret: ScheduleSendMenu over it for the first
// destination (selectedChannelId).
func (f *actForward) openSchedule() {
	if !f.canGo() || ScheduleSendMenu == nil {
		return
	}
	ScheduleSendMenu(f.h, f.caret.Rect(), f.chips[0].t.id, f.schedule, f.scheduleCustom)
}

// schedule is onScheduleSend with a preset's time: scheduleForwardMessage
// creates the draft with date_scheduled for each destination, the dialog
// closes, and once scheduled ScheduleForwardToast says "Message
// scheduled" (the schedule-send icon).
func (f *actForward) schedule(at time.Time) {
	if !f.canGo() {
		return
	}
	ids := f.targets()
	text, blocks := f.message()
	f.modal.Close()
	f.scheduleDrafts(ids, text, blocks, at.Unix(), "")
}

func (f *actForward) scheduleDrafts(ids []string, text string, blocks json.RawMessage, postAt int64, draftID string) {
	h := f.h
	a := h.Core()
	if a == nil {
		h.Toast("Not connected yet")
		return
	}
	run(h, func(ctx context.Context) (struct{}, error) {
		if draftID != "" {
			_, err := a.RescheduleMessage(ctx, draftID, postAt)
			return struct{}{}, err
		}
		for _, id := range ids {
			if _, err := a.ScheduleMessage(ctx, core.SendOpts{Channel: id, Text: text, Blocks: blocks}, postAt); err != nil {
				return struct{}{}, err
			}
		}
		return struct{}{}, nil
	}, func(_ struct{}, err error) {
		if err != nil {
			return // Slack only logs it ("Failed to schedule forward message")
		}
		toastIcon(h, nsShare.T("Message scheduled"), "schedule-send") // ScheduleForwardToast
	})
}

// scheduleCustom is onScheduleSend without a time ("Custom time"):
// scheduleForwardMessage saves the draft first, then opens
// ScheduleMessageDialog for it, stacked over the share dialog under
// updated_scheduled_sends_desktop (which then stays until the time is
// picked, closing with it), else after the share dialog closes.
func (f *actForward) scheduleCustom() {
	if !f.canGo() || OpenScheduleDialog == nil {
		return
	}
	h := f.h
	a := h.Core()
	if a == nil {
		h.Toast("Not connected yet")
		return
	}
	ch := f.chips[0].t.id
	text, blocks := f.message()
	stacked := expOn(h, expUpdatedScheduledSends)
	if !stacked {
		f.modal.Close()
	}
	run(h, func(ctx context.Context) (core.Draft, error) {
		return a.SaveDraft(ctx, core.DraftOpts{Channel: ch, Text: text, Blocks: blocks})
	}, func(d core.Draft, err error) {
		if err != nil {
			return // logged only, as scheduleForwardMessage's catch does
		}
		OpenScheduleDialog(h, ch, func(at time.Time) {
			f.modal.Close() // closeAll
			f.scheduleDrafts(nil, text, blocks, at.Unix(), d.ID)
		})
	})
}

// saveDraft is Save Draft (ek saveDraft): a draft with the note and the
// permalink for each destination (none yet: a draft without one), then
// the dialog closes; Slack says nothing.
func (f *actForward) saveDraft() {
	ids := f.targets()
	text, blocks := f.message()
	actDo(f.h, "", func(ctx context.Context, a *core.App) error {
		if len(ids) == 0 {
			_, err := a.SaveDraft(ctx, core.DraftOpts{NoDestination: true, Text: text, Blocks: blocks})
			return err
		}
		for _, id := range ids {
			if _, err := a.SaveDraft(ctx, core.DraftOpts{Channel: id, Text: text, Blocks: blocks}); err != nil {
				return err
			}
		}
		return nil
	})
}

// actFwdTargets lists forwardable conversations from the snapshot.
func actFwdTargets(h Host) []actFwdTarget {
	snap, ok := actSnapshot(h)
	if !ok {
		return nil
	}
	out := make([]actFwdTarget, 0, len(snap.Convs))
	for _, c := range snap.Convs {
		if c.IsArchived {
			continue
		}
		t := actFwdTarget{id: c.ID, kind: c.Kind, title: convTitle(h, c)}
		if c.Kind == "mpim" {
			if t.title == c.Name {
				if hs := srMpdmNames(c.Name, snap.Self.Handle); len(hs) > 0 {
					t.title = strings.Join(hs, ", ")
				}
			}
			t.members = c.Members
			for _, id := range c.Members {
				if id == snap.Self.ID {
					continue
				}
				u, ok := snap.Users[id]
				if !ok {
					u, ok = lookupUser(h, id)
				}
				if !ok {
					continue
				}
				if u.RealName != "" && u.RealName != userName(u) {
					t.keys = append(t.keys, strings.ToLower(u.RealName))
				}
				if u.Handle != "" {
					t.keys = append(t.keys, strings.ToLower(u.Handle))
				}
			}
		}
		if c.Kind == "im" {
			u, ok := snap.Users[c.User]
			if !ok {
				u, ok = lookupUser(h, c.User)
			}
			if ok {
				if u.Deleted {
					continue
				}
				t.avatar = u.Avatar
			}
		}
		if t.title == "" {
			continue
		}
		t.lower = strings.ToLower(t.title)
		out = append(out, t)
	}
	return out
}

// actFuzzy scores how well s (lowercase) matches q (lowercase): exact >
// prefix > word prefix > substring > subsequence; -1 = no match.
func actFuzzy(s, q string) int {
	if q == "" {
		return -1
	}
	switch {
	case s == q:
		return 1000
	case strings.HasPrefix(s, q):
		return 800 - minInt(len(s)-len(q), 99)
	}
	if i := strings.Index(s, q); i >= 0 {
		for j := i; j >= 0; j = actIndexFrom(s, q, j+1) {
			if j > 0 && strings.ContainsRune(" -_.,", rune(s[j-1])) {
				return 600 - minInt(j, 99)
			}
		}
		return 400 - minInt(i, 99)
	}
	// subsequence: every rune of q in order.
	gaps, k, last := 0, 0, -1
	qr := []rune(q)
	for i, r := range s {
		if k < len(qr) && r == qr[k] {
			if last >= 0 {
				gaps += i - last - 1
			}
			last = i
			k++
		}
	}
	if k < len(qr) {
		return -1
	}
	return 200 - minInt(gaps, 199)
}

// actFwdScore matches a target's title, then its extra keys (a notch
// lower, so a name match wins); -1 = no match.
func actFwdScore(t *actFwdTarget, q string) int {
	sc := actFuzzy(t.lower, q)
	for _, k := range t.keys {
		if s2 := actFuzzy(k, q); s2 >= 0 {
			s2 = maxInt(0, s2-50)
			if s2 > sc {
				sc = s2
			}
		}
	}
	return sc
}

func actIndexFrom(s, q string, from int) int {
	if from >= len(s) {
		return -1
	}
	i := strings.Index(s[from:], q)
	if i < 0 {
		return -1
	}
	return from + i
}

func (f *actForward) chosen(id string) bool {
	for _, c := range f.chips {
		if c.t.id == id {
			return true
		}
	}
	return false
}

func (f *actForward) refilter() {
	q := strings.ToLower(strings.TrimSpace(f.to.Text()))
	if q == f.query {
		return
	}
	f.query = q
	f.match = f.match[:0]
	f.sugList.Position = layout.Position{}
	// SelectOptionsList on new options: the first match is the active row,
	// or, under a11y_no_auto_highlight_search_multi_select, no row is
	// active and the first is pseudo-active (Fd8o componentDidUpdate)
	f.sel = 0
	if noAutoHighlight(f.h) {
		f.sel = -1
	}
	if q == "" {
		return
	}
	type hit struct{ i, score int }
	var hits []hit
	for i := range f.all {
		t := &f.all[i]
		if sc := actFwdScore(t, q); sc >= 0 && !f.chosen(t.id) {
			hits = append(hits, hit{i, sc})
		}
	}
	sort.SliceStable(hits, func(a, b int) bool {
		if hits[a].score != hits[b].score {
			return hits[a].score > hits[b].score
		}
		return len(f.all[hits[a].i].title) < len(f.all[hits[b].i].title)
	})
	for k := 0; k < len(hits) && k < actFwdMax; k++ {
		f.match = append(f.match, hits[k].i)
	}
}

// enterRow is the row Enter and Tab pick: the active row, else the
// pseudo-active one (onEnter: activeRowIndex ?? pseudoActiveRowIndex);
// -1 for none.
func (f *actForward) enterRow() int {
	if f.sel >= 0 {
		return f.sel
	}
	return f.pseudoRow()
}

// pseudoRow is getPseudoActiveRowIndex: the first match while no row is
// active, under a11y_no_auto_highlight_search_multi_select; -1 otherwise.
func (f *actForward) pseudoRow() int {
	if f.sel < 0 && len(f.match) > 0 && noAutoHighlight(f.h) {
		return 0
	}
	return -1
}

func (f *actForward) pick(i int) {
	if i < 0 || i >= len(f.match) {
		return
	}
	t := &f.all[f.match[i]]
	if !f.chosen(t.id) {
		f.chips = append(f.chips, &actFwdChip{t: t})
	}
	f.to.SetText("")
	f.query = ""
	f.match = f.match[:0]
	f.focusTo = true
}

func (f *actForward) events(gtx layout.Context) {
	// keys the picker handles before the editor sees them.
	filters := []event.Filter{}
	if len(f.match) > 0 {
		filters = append(filters,
			key.Filter{Focus: &f.to, Name: key.NameUpArrow},
			key.Filter{Focus: &f.to, Name: key.NameDownArrow},
			key.Filter{Focus: &f.to, Name: key.NameReturn},
			key.Filter{Focus: &f.to, Name: key.NameEnter},
			key.Filter{Focus: &f.to, Name: key.NameTab},
		)
	}
	if f.to.Len() == 0 && len(f.chips) > 0 {
		filters = append(filters, key.Filter{Focus: &f.to, Name: key.NameDeleteBackward})
	}
	for len(filters) > 0 {
		e, ok := gtx.Event(filters...)
		if !ok {
			break
		}
		ke, isKey := e.(key.Event)
		if !isKey || ke.State != key.Press {
			continue
		}
		switch ke.Name {
		case key.NameUpArrow:
			// getPreviousActiveRowIndex: the last row from none, no
			// further than the first (the picker doesn't wrap)
			if n := len(f.match); n > 0 {
				switch {
				case f.sel < 0:
					f.sel = n - 1
				case f.sel > 0:
					f.sel--
				}
			}
		case key.NameDownArrow:
			// getNextActiveRowIndex: the first row from none, no
			// further than the last
			if n := len(f.match); n > 0 && f.sel < n-1 {
				f.sel++
			}
		case key.NameReturn, key.NameEnter, key.NameTab:
			f.pick(f.enterRow())
		case key.NameDeleteBackward:
			if f.to.Len() == 0 && len(f.chips) > 0 {
				f.chips = f.chips[:len(f.chips)-1]
			}
		}
	}
	for {
		e, ok := f.to.Update(gtx)
		if !ok {
			break
		}
		switch e.(type) {
		case widget.ChangeEvent:
			f.refilter()
		case widget.SubmitEvent:
			if len(f.match) > 0 {
				f.pick(f.enterRow())
			} else if len(f.chips) > 0 {
				f.modal.press(f.fwd)
				return
			}
		}
	}
	f.refilter()
	for i := range f.match {
		if f.sugg[i].Clicked(gtx) {
			f.pick(i)
			break
		}
	}
	for i := 0; i < len(f.chips); i++ {
		if f.chips[i].rm.Clicked(gtx) {
			f.chips = append(f.chips[:i], f.chips[i+1:]...)
			f.query = "\x00" // force a refilter (removed targets come back)
			f.refilter()
			f.focusTo = true
			break
		}
	}
	if f.toBox.Clicked(gtx) {
		f.focusTo = true
	}
	if f.noteBox.Clicked(gtx) {
		gtx.Execute(key.FocusCmd{Tag: &f.note})
	}
}

func (f *actForward) layout(gtx layout.Context) layout.Dimensions {
	if f.modal.done {
		return layout.Dimensions{}
	}
	if f.copyLink {
		// Zp0h: the clipboard, then "Copied!" in the tooltip (a toast only
		// for canvases and lists)
		f.copyLink = false
		copyText(gtx, f.link)
		f.copied = true
	}
	f.events(gtx)
	if f.modal.done {
		return layout.Dimensions{}
	}
	if f.focusTo {
		gtx.Execute(key.FocusCmd{Tag: &f.to})
		f.focusTo = false
	}
	th := f.h.Theme()
	cl := colorsOf(th)
	w := gtx.Constraints.Max.X
	y := 0
	put := func(wd layout.Widget) int {
		call, d := record(loose(gtx, image.Pt(w, 1<<20)), wd)
		macroAt(gtx, call, image.Pt(0, y))
		return d.Size.Y
	}
	y += put(func(gtx layout.Context) layout.Dimensions { return f.picker(gtx, th, cl) })
	if len(f.match) > 0 {
		y += dp(gtx, 4)
		y += put(func(gtx layout.Context) layout.Dimensions { return f.suggestions(gtx, th, cl) })
	}
	y += dp(gtx, 16)
	y += put(func(gtx layout.Context) layout.Dimensions { return f.noteField(gtx, th, cl) })
	y += dp(gtx, 16)
	y += put(func(gtx layout.Context) layout.Dimensions { return actMsgPreview(gtx, f.h, f.m, false) })
	return layout.Dimensions{Size: image.Pt(w, y+dp(gtx, 4))}
}

// actFocusBox paints an input box (focus ring when focused).
func actFocusBox(gtx layout.Context, cl *Colors, r image.Rectangle, focused bool) {
	rad := dp(gtx, 8)
	if focused {
		ring := dp(gtx, 4)
		rrect(gtx, r.Inset(-ring), rad+ring, cl.FocusRing)
		rrect(gtx, r, rad, cl.Focus)
	} else {
		rrect(gtx, r, rad, cl.InputBorder)
	}
	rrect(gtx, r.Inset(1), rad-1, cl.Bg)
}

// picker draws the "Add a channel or person" field with chips.
func (f *actForward) picker(gtx layout.Context, th *theme.Theme, cl *Colors) layout.Dimensions {
	w := gtx.Constraints.Max.X
	padX, padY := dp(gtx, 8), dp(gtx, 7)
	lineH, gap := dp(gtx, 28), dp(gtx, 4)
	innerW := maxInt(1, w-2*padX)
	content, cd := record(gtx, func(gtx layout.Context) layout.Dimensions {
		x, y := 0, 0
		for _, c := range f.chips {
			call, d := record(gtx, func(gtx layout.Context) layout.Dimensions { return f.chip(gtx, th, cl, c, innerW) })
			if x > 0 && x+d.Size.X > innerW {
				x, y = 0, y+lineH+gap
			}
			macroAt(gtx, call, image.Pt(x, y+(lineH-d.Size.Y)/2))
			x += d.Size.X + gap
		}
		ew := innerW - x
		if ew < dp(gtx, 120) {
			x, y, ew = 0, y+lineH+gap, innerW
		}
		at(gtx, image.Pt(x+dp(gtx, 4), y), func(gtx layout.Context) layout.Dimensions {
			ew := maxInt(1, ew-dp(gtx, 4))
			return vcenter(gtx, lineH, func(gtx layout.Context) layout.Dimensions {
				gtx.Constraints = layout.Constraints{Min: image.Pt(ew, 0), Max: image.Pt(ew, lineH)}
				if f.to.Len() == 0 && len(f.chips) == 0 {
					call, _ := record(gtx, func(gtx layout.Context) layout.Dimensions {
						return text(gtx, th, 15, theme.Regular, cl.Placeholder, nsShare.T("Search for channel or person"))
					})
					call.Add(gtx.Ops)
				}
				return th.Editor(gtx, &f.to, unit.Sp(15), cl.Fg, theme.WithAlpha(cl.Focus, 0x55))
			})
		})
		return layout.Dimensions{Size: image.Pt(innerW, y+lineH)}
	})
	r := image.Rect(0, 0, w, cd.Size.Y+2*padY)
	actFocusBox(gtx, cl, r, gtx.Focused(&f.to))
	// background click target (siblings drawn later sit on top of it).
	f.toBox.Layout(exact(gtx, r.Max), func(gtx layout.Context) layout.Dimensions {
		defer clip.Rect(r).Push(gtx.Ops).Pop()
		pointer.CursorText.Add(gtx.Ops)
		return layout.Dimensions{Size: r.Max}
	})
	macroAt(gtx, content, image.Pt(padX, padY))
	return layout.Dimensions{Size: r.Max}
}

func (f *actForward) chip(gtx layout.Context, th *theme.Theme, cl *Colors, c *actFwdChip, maxW int) layout.Dimensions {
	ch := dp(gtx, 26)
	tw := textW(gtx, th, 15, theme.Bold, c.t.title)
	iconW := dp(gtx, 20)
	rmW := dp(gtx, 20)
	w := dp(gtx, 4) + iconW + dp(gtx, 6) + tw + dp(gtx, 2) + rmW + dp(gtx, 2)
	if w > maxW {
		tw -= w - maxW
		w = maxW
	}
	// opaque tint (sRGB mix) so the group-DM tile's cut-out ring matches
	chipBg := theme.Mix(cl.Bg, cl.Link, float32(0x1f)/255)
	rrect(gtx, image.Rect(0, 0, w, ch), dp(gtx, 4), chipBg)
	x := dp(gtx, 4)
	at(gtx, image.Pt(x, (ch-iconW)/2), func(gtx layout.Context) layout.Dimensions {
		switch c.t.kind {
		case "im":
			return avatar(gtx, f.h, c.t.avatar, 20)
		case "mpim":
			return mpimTile(gtx, f.h, c.t.members, 20, chipBg, false)
		}
		at(gtx, image.Pt(dp(gtx, 2), dp(gtx, 2)), func(gtx layout.Context) layout.Dimensions {
			return icon(gtx, convIcon(c.t.kind), 16, cl.Fg2)
		})
		return layout.Dimensions{Size: image.Pt(iconW, iconW)}
	})
	x += iconW + dp(gtx, 6)
	at(gtx, image.Pt(x, 0), func(gtx layout.Context) layout.Dimensions {
		gtx.Constraints = layout.Constraints{Max: image.Pt(maxInt(1, tw), ch)}
		return vcenter(gtx, ch, func(gtx layout.Context) layout.Dimensions {
			return text(gtx, th, 15, theme.Bold, cl.Fg, c.t.title)
		})
	})
	x += maxInt(1, tw) + dp(gtx, 2)
	at(gtx, image.Pt(x, (ch-rmW)/2), func(gtx layout.Context) layout.Dimensions {
		return iconButton(gtx, &c.rm, "close-small", 20, 14, cl.Fg2, theme.WithAlpha(cl.Link, 0x26), 4)
	})
	return layout.Dimensions{Size: image.Pt(w, ch)}
}

// suggestions draws the matching conversations under the picker: 32px
// rows (optionsRowHeight) in a list that scrolls past 200px
// (maxOptionsListHeight); the keyboard's row is kept in view.
func (f *actForward) suggestions(gtx layout.Context, th *theme.Theme, cl *Colors) layout.Dimensions {
	w := gtx.Constraints.Max.X
	rh := dp(gtx, actFwdRowH)
	pad := dp(gtx, 6)
	n := len(f.match)
	lh := minInt(n*rh, dp(gtx, actFwdListMax))
	r := image.Rect(0, 0, w, lh+2*pad)
	rrect(gtx, r, dp(gtx, 8), cl.Ring)
	rrect(gtx, r.Inset(1), dp(gtx, 8)-1, cl.Bg)
	// onMouseEnteredOption makes the row under the pointer active;
	// onMouseLeftOption clears it (clearActive: the pseudo-active row
	// comes back under a11y_no_auto_highlight_search_multi_select)
	hov := -1
	for i := range f.match {
		if f.sugg[i].Hovered() {
			hov = i
		}
	}
	if hov >= 0 {
		f.sel = hov
	} else if f.hov >= 0 {
		f.sel = -1
	}
	f.hov = hov
	if f.sel >= 0 && hov < 0 { // scrollToRow: the active row in view
		vis := maxInt(1, lh/rh)
		switch {
		case f.sel < f.sugList.Position.First:
			f.sugList.Position.First, f.sugList.Position.Offset = f.sel, 0
		case f.sel >= f.sugList.Position.First+vis:
			f.sugList.Position.First, f.sugList.Position.Offset = f.sel-vis+1, 0
		}
	}
	pseudo := f.pseudoRow()
	at(gtx, image.Pt(0, pad), func(gtx layout.Context) layout.Dimensions {
		gtx.Constraints = layout.Exact(image.Pt(w, lh))
		defer clip.Rect{Max: gtx.Constraints.Max}.Push(gtx.Ops).Pop()
		return f.sugList.Layout(gtx, n, func(gtx layout.Context, i int) layout.Dimensions {
			t := &f.all[f.match[i]]
			gtx.Constraints = layout.Exact(image.Pt(w, rh))
			return clickBox(gtx, &f.sugg[i], nil, func(gtx layout.Context) layout.Dimensions {
				fg, fg2 := cl.Fg, cl.Fg2
				sel, rowBg := i == f.sel, cl.Bg
				right := w - dp(gtx, 16)
				if sel {
					rowBg = cl.Highlight
					fill(gtx, image.Rect(0, 0, w, rh), rowBg)
					fg, fg2 = cl.HighText, cl.HighText
				} else if i == pseudo {
					// .c-select_options_list__option--pseudo-active and its
					// KeyboardKey "Enter" (margin-inline-start 16)
					fill(gtx, image.Rect(0, 0, w, rh), selectPseudoActive(th))
					right = enterKeyChip(gtx, th, right, rh) - dp(gtx, 16)
				}
				x := dp(gtx, 16)
				at(gtx, image.Pt(x, (rh-dp(gtx, 20))/2), func(gtx layout.Context) layout.Dimensions {
					switch t.kind {
					case "im":
						return avatar(gtx, f.h, t.avatar, 20)
					case "mpim":
						return mpimTile(gtx, f.h, t.members, 20, rowBg, sel)
					}
					at(gtx, image.Pt(dp(gtx, 2), dp(gtx, 2)), func(gtx layout.Context) layout.Dimensions {
						return icon(gtx, convIcon(t.kind), 16, fg2)
					})
					return layout.Dimensions{Size: image.Pt(dp(gtx, 20), dp(gtx, 20))}
				})
				x += dp(gtx, 30)
				at(gtx, image.Pt(x, 0), func(gtx layout.Context) layout.Dimensions {
					gtx.Constraints = layout.Constraints{Max: image.Pt(maxInt(1, right-x), rh)}
					return vcenter(gtx, rh, func(gtx layout.Context) layout.Dimensions {
						return text(gtx, th, 15, theme.Regular, fg, t.title)
					})
				})
				return layout.Dimensions{Size: image.Pt(w, rh)}
			})
		})
	})
	return layout.Dimensions{Size: r.Max}
}

// noteField draws the optional multi-line "Add a message" input.
func (f *actForward) noteField(gtx layout.Context, th *theme.Theme, cl *Colors) layout.Dimensions {
	w := gtx.Constraints.Max.X
	padX, padY := dp(gtx, 12), dp(gtx, 10)
	tw := maxInt(1, w-2*padX)
	content, cd := record(gtx, func(gtx layout.Context) layout.Dimensions {
		gtx.Constraints = layout.Constraints{Min: image.Pt(tw, 0), Max: image.Pt(tw, dp(gtx, 160))}
		if f.note.Len() == 0 {
			call, _ := record(gtx, func(gtx layout.Context) layout.Dimensions {
				return text(gtx, th, 15, theme.Regular, cl.Placeholder, nsShare.T("Add a message, if you’d like."))
			})
			call.Add(gtx.Ops)
		}
		return th.Editor(gtx, &f.note, unit.Sp(15), cl.Fg, theme.WithAlpha(cl.Focus, 0x55))
	})
	hh := maxInt(cd.Size.Y+2*padY, dp(gtx, 64))
	r := image.Rect(0, 0, w, hh)
	actFocusBox(gtx, cl, r, gtx.Focused(&f.note))
	f.noteBox.Layout(exact(gtx, r.Max), func(gtx layout.Context) layout.Dimensions {
		defer clip.Rect(r).Push(gtx.Ops).Pop()
		pointer.CursorText.Add(gtx.Ops)
		return layout.Dimensions{Size: r.Max}
	})
	macroAt(gtx, content, image.Pt(padX, padY))
	return layout.Dimensions{Size: r.Max}
}
