package popups

// "Share directly", from a custom section's Share ▸ (getSectionShareMenuItem,
// boot-render): Slack's ShareChannelSectionMultiPaneDialog
// (share-dialog-channel-section, module IEmj), with its channel list
// ShareChannelSectionChannelList (boot-shared e9lq).
//
//   - Title "Share <section>". The section's channels that can be shared
//     (not DMs, not externally shared) are listed private first, then by
//     name, all selected; if some were left out the list says "Only
//     channels in your organization can be shared…". With none, the body
//     is "This section can’t be shared".
//   - People: a multi-select of people and group DMs; the picks become one
//     conversation (conversations.open, at most 8 others: "Only 9 people
//     can be in a direct message."). Each new destination is checked with
//     users.channelSections.validateShare; the channels it refuses are
//     unticked and disabled, their reasons in a tooltip.
//   - Add a message (optional), then Share: with private channels picked,
//     the footer first asks "Are you sure you want to include N private
//     channels?" (Edit Channels / Confirm and Share Section).
//   - The share is one message in the destination: the note as rich_text
//     and the section as an attachment {type: channel_section,
//     channel_set_name, channel_ids, emoji} (shareChannelSection,
//     prepareAndSendMessage's channelSectionBlocks); the dialog closes and
//     Clack jumps to the message. A failed validation or send shows
//     "Something went wrong and that didn’t work right. Give it another
//     try." above the form.
//
// Clack's differences: the message field is plain text (Slack's is its
// WYSIWYG composer, with mentions and emoji), and the people search is
// the directory search Clack has (edge users/search), not Slack's
// frecency searcher.

import (
	"context"
	"image"
	"image/color"
	"sort"
	"strings"

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

// ShareSectionOpts are the section being shared.
type ShareSectionOpts struct {
	Name, Emoji string
	Channels    []string // the section's conversations (channelIdsIncludedInSection)
}

type shareTarget struct {
	id     string // a group DM's id ("" for a person)
	user   string // a person
	title  string
	lower  string
	kind   string // im | mpim
	avatar string
	// members: a group DM's members; keys: extra match keys
	members, keys []string
}

type shareChip struct {
	t  shareTarget
	rm widget.Clickable
}

// ShareSectionModal is the dialog.
type ShareSectionModal struct {
	h     Host
	o     ShareSectionOpts
	modal *Modal

	ordered []string // the shareable channels, private first, then by name
	removed bool     // some of the section's conversations can't be shared
	sel     []string
	errs    map[string][]string // validateShare's refusals by channel

	to      widget.Editor
	toBox   widget.Clickable
	query   string
	local   []shareTarget
	remote  []shareTarget
	remoteQ string
	match   []shareTarget
	hl      int
	sugg    [6]widget.Clickable
	chips   []*shareChip
	focusTo bool

	dest, destKind string // the conversation the picks became (im | mpim)
	destErr        string // too_many_users | unknown
	gen            int    // destination generation: stale answers are dropped

	note    widget.Editor
	noteBox widget.Clickable

	validating, validateFailed bool
	sending, sendFailed        bool
	confirm                    bool

	workspace string // the team's name (the refusal tooltips)

	allBtn widget.Clickable
	rowBtn map[string]*widget.Clickable
	tips   map[string]*TipState

	share, cancel, editCh, confirmShare *ModalButton
}

// NewShareSectionModal builds it; show it with h.ShowOverlay.
func NewShareSectionModal(h Host, o ShareSectionOpts) *ShareSectionModal {
	m := &ShareSectionModal{h: h, o: o, rowBtn: map[string]*widget.Clickable{}, tips: map[string]*TipState{}, focusTo: true}
	m.to.SingleLine, m.to.Submit = true, true
	m.ordered, m.removed = shareableChannels(h, o.Channels)
	m.sel = append([]string(nil), m.ordered...)
	m.local = shareLocalTargets(h)
	if snap, ok := actSnapshot(h); ok {
		m.workspace = strings.TrimSpace(snap.Team.Name)
	}
	// ShareChannelSectionMultiPaneDialog's strings (IEmj, share-dialog)
	m.share = &ModalButton{Label: nsShare.T("Share"), Kind: BtnPrimary, Enabled: m.CanShare, Do: func() bool { m.Share(); return false }}
	m.cancel = &ModalButton{Label: nsShare.T("Cancel"), Kind: BtnOutline, Do: func() bool { return true }}
	m.editCh = &ModalButton{Label: nsShare.T("Edit Channels"), Kind: BtnOutline, Do: func() bool { m.confirm = false; return false }}
	m.confirmShare = &ModalButton{Label: nsShare.T("Confirm and Share Section"), Kind: BtnPrimary,
		Enabled: func() bool { return !m.sending }, Do: func() bool { m.send(); return false }}
	m.modal = NewModal(h, ModalOpts{Title: nsShare.T("Share {channelSectionName}", "channelSectionName", o.Name), Body: m.layout, Width: 600,
		Buttons:    []*ModalButton{m.cancel, m.share},
		FocusStops: func() []event.Tag { return []event.Tag{&m.to, &m.note} }})
	return m
}

func (m *ShareSectionModal) Layout(gtx layout.Context) layout.Dimensions { return m.modal.Layout(gtx) }
func (m *ShareSectionModal) Dismissed() bool                             { return m.modal.Dismissed() }
func (m *ShareSectionModal) Dismiss()                                    { m.modal.Dismiss() }

// shareableChannels is the dialog's channel list: the section's known
// channels that aren't DMs or externally shared (j), private first, then
// by name (A, sortChannels). removed: some were left out.
func shareableChannels(h Host, ids []string) (out []string, removed bool) {
	a := h.Core()
	type row struct {
		id, name string
		private  bool
	}
	var rows []row
	for _, id := range ids {
		var c core.Conv
		ok := false
		if a != nil {
			c, ok = a.Conv(id)
		}
		if !ok || (c.Kind != "channel" && c.Kind != "private") || c.IsShared {
			continue
		}
		rows = append(rows, row{id, strings.ToLower(c.Name), c.Kind == "private"})
	}
	sort.SliceStable(rows, func(i, j int) bool {
		if rows[i].private != rows[j].private {
			return rows[i].private
		}
		return rows[i].name < rows[j].name
	})
	for _, r := range rows {
		out = append(out, r.id)
	}
	return out, len(out) != len(ids)
}

// shareLocalTargets are the people you have DMs with and your group DMs
// (the searcher's members without bots, Slackbot or you, and mpims).
func shareLocalTargets(h Host) []shareTarget {
	snap, ok := actSnapshot(h)
	if !ok {
		return nil
	}
	var out []shareTarget
	for _, c := range snap.Convs {
		if c.IsArchived {
			continue
		}
		switch c.Kind {
		case "im":
			if c.User == "" || c.User == snap.Self.ID || c.User == "USLACKBOT" {
				continue
			}
			u, ok := snap.Users[c.User]
			if !ok {
				u, ok = lookupUser(h, c.User)
			}
			if !ok || u.Deleted || u.IsBot {
				continue
			}
			t := shareTarget{user: u.ID, title: userName(u), kind: "im", avatar: u.Avatar}
			if u.RealName != "" && u.RealName != t.title {
				t.keys = append(t.keys, strings.ToLower(u.RealName))
			}
			out = append(out, t)
		case "mpim":
			t := shareTarget{id: c.ID, title: convTitle(h, c), kind: "mpim", members: c.Members}
			if t.title == c.Name {
				if hs := srMpdmNames(c.Name, snap.Self.Handle); len(hs) > 0 {
					t.title = strings.Join(hs, ", ")
				}
			}
			out = append(out, t)
		}
	}
	for i := range out {
		out[i].lower = strings.ToLower(out[i].title)
	}
	return out
}

func (t *shareTarget) key() string {
	if t.id != "" {
		return t.id
	}
	return t.user
}

// ---- state (exported for tests) ----

// Shareable is the channel list, in its order.
func (m *ShareSectionModal) Shareable() []string { return m.ordered }

// Selected are the channels that will be shared: the ticked ones the
// destination doesn't refuse.
func (m *ShareSectionModal) Selected() []string {
	var out []string
	for _, id := range m.sel {
		if len(m.errs[id]) == 0 {
			out = append(out, id)
		}
	}
	return out
}

// ChannelErrors are validateShare's refusals by channel.
func (m *ShareSectionModal) ChannelErrors() map[string][]string { return m.errs }

// Destination is the conversation the picks became ("" while none).
func (m *ShareSectionModal) Destination() string { return m.dest }

// DestinationError is the people field's error text.
func (m *ShareSectionModal) DestinationError() string {
	switch m.destErr {
	case "":
		return ""
	case "too_many_users":
		return nsShare.T("Only 9 people can be in a direct message.")
	}
	return nsShare.T("An unknown error occurred.")
}

// Failed reports the "Something went wrong" alert.
func (m *ShareSectionModal) Failed() bool { return m.validateFailed || m.sendFailed }

// Confirming reports the private-channel confirmation.
func (m *ShareSectionModal) Confirming() bool { return m.confirm }

// Busy reports a validation or send in flight.
func (m *ShareSectionModal) Busy() bool { return m.validating || m.sending }

// SetNote sets the message.
func (m *ShareSectionModal) SetNote(s string) { m.note.SetText(s) }

// AddPerson picks a person (as choosing them in the People field).
func (m *ShareSectionModal) AddPerson(userID string) {
	t := shareTarget{user: userID, kind: "im", title: userID}
	if u, ok := lookupUser(m.h, userID); ok {
		t.title, t.avatar = userName(u), u.Avatar
	}
	m.addChip(t)
}

// AddGroupDM picks a group DM.
func (m *ShareSectionModal) AddGroupDM(id string) {
	for _, t := range m.local {
		if t.id == id {
			m.addChip(t)
			return
		}
	}
	m.addChip(shareTarget{id: id, kind: "mpim", title: id})
}

// Toggle ticks or unticks a channel (refused ones can't be).
func (m *ShareSectionModal) Toggle(id string) {
	if len(m.errs[id]) > 0 {
		return
	}
	for i, s := range m.sel {
		if s == id {
			m.sel = append(m.sel[:i:i], m.sel[i+1:]...)
			return
		}
	}
	m.sel = append(m.sel, id)
}

// ToggleAll is the "Select all shareable channels" checkbox: none
// selected when there are any, else all.
func (m *ShareSectionModal) ToggleAll() {
	if len(m.sel) > 0 {
		m.sel = nil
		return
	}
	m.sel = append([]string(nil), m.ordered...)
}

func (m *ShareSectionModal) privateSelected() int {
	n := 0
	a := m.h.Core()
	for _, id := range m.Selected() {
		if a != nil {
			if c, ok := a.Conv(id); ok && c.Kind == "private" {
				n++
			}
		}
	}
	return n
}

// CanShare is the Share button's state.
func (m *ShareSectionModal) CanShare() bool {
	n := len(m.Selected())
	return m.destErr == "" && n > 0 && n <= 100 && m.dest != "" && !m.sending && !m.validating
}

// Share is the Share button: a failed validation is retried first; then
// private channels ask for confirmation, or the section is sent.
func (m *ShareSectionModal) Share() {
	if !m.CanShare() {
		return
	}
	if m.validateFailed {
		// Slack's ej: (!E || await ey(_)) && …, which only goes on when the
		// retry found refusals or failed again
		m.validate(m.dest, func(goOn bool) {
			if goOn {
				m.proceed()
			}
		})
		return
	}
	m.proceed()
}

// Confirm is "Confirm and Share Section".
func (m *ShareSectionModal) Confirm() {
	if m.confirm && m.confirmShare.enabled() {
		m.send()
	}
}

func (m *ShareSectionModal) proceed() {
	if m.privateSelected() > 0 {
		m.confirm = true
		return
	}
	m.send()
}

// ---- destination ----

func (m *ShareSectionModal) chosen(key string) bool {
	for _, c := range m.chips {
		if c.t.key() == key {
			return true
		}
	}
	return false
}

func (m *ShareSectionModal) addChip(t shareTarget) {
	if !m.chosen(t.key()) {
		m.chips = append(m.chips, &shareChip{t: t})
		m.resolve()
	}
	m.to.SetText("")
	m.query, m.match = "", nil
	m.focusTo = true
}

// resolve turns the picks into one conversation, then validates it (the
// destination input's force-create-DM multi-select, then onChange).
func (m *ShareSectionModal) resolve() {
	m.gen++
	g := m.gen
	m.dest, m.destKind, m.destErr = "", "", ""
	m.validating = false
	if len(m.chips) == 0 {
		// eC: no destination clears the refusals
		m.validateFailed, m.errs = false, nil
		return
	}
	if len(m.chips) == 1 && m.chips[0].t.kind == "mpim" {
		m.setDest(m.chips[0].t.id, "mpim")
		return
	}
	self := ""
	if a := m.h.Core(); a != nil {
		self = a.S.SelfID()
	}
	var users []string
	add := func(u string) {
		if u != "" && u != self && !strings.Contains(","+strings.Join(users, ",")+",", ","+u+",") {
			users = append(users, u)
		}
	}
	for _, c := range m.chips {
		if c.t.kind == "mpim" {
			for _, u := range c.t.members {
				add(u)
			}
		} else {
			add(c.t.user)
		}
	}
	if len(users) > 8 {
		m.destErr = "too_many_users"
		return
	}
	kind := "im"
	if len(users) > 1 {
		kind = "mpim"
	}
	run(m.h, func(ctx context.Context) (core.Conv, error) {
		a, err := cxApp(m.h)
		if err != nil {
			return core.Conv{}, err
		}
		return a.OpenDM(ctx, users...)
	}, func(c core.Conv, err error) {
		if g != m.gen {
			return
		}
		if err != nil || c.ID == "" {
			m.destErr = "unknown"
			return
		}
		m.setDest(c.ID, kind)
	})
}

func (m *ShareSectionModal) setDest(id, kind string) {
	m.dest, m.destKind = id, kind
	m.validate(id, nil)
}

// validate is ey: validateShare for every shareable channel into target;
// done (if any) gets whether to go on (refusals found, or it failed).
func (m *ShareSectionModal) validate(target string, done func(goOn bool)) {
	m.validating = true
	g := m.gen
	chans := append([]string(nil), m.ordered...)
	run(m.h, func(ctx context.Context) (map[string][]string, error) {
		a, err := cxApp(m.h)
		if err != nil {
			return nil, err
		}
		return a.ValidateShareSection(ctx, target, chans)
	}, func(errs map[string][]string, err error) {
		if g != m.gen { // aborted: the destination changed
			return
		}
		m.validating = false
		if err != nil {
			m.validateFailed = true
			if done != nil {
				done(true)
			}
			return
		}
		m.validateFailed = false
		if len(errs) > 0 {
			m.errs = errs
		}
		if done != nil {
			done(len(errs) > 0)
		}
	})
}

// send is ep: shareChannelSection, then close and jump to the message.
func (m *ShareSectionModal) send() {
	if m.sending || m.dest == "" {
		return
	}
	m.sending, m.sendFailed = true, false
	dest, ids, note := m.dest, m.Selected(), strings.TrimSpace(m.note.Text())
	name, emoji := m.o.Name, m.o.Emoji
	run(m.h, func(ctx context.Context) (core.Message, error) {
		a, err := cxApp(m.h)
		if err != nil {
			return core.Message{}, err
		}
		return a.ShareSection(ctx, dest, name, emoji, ids, note)
	}, func(msg core.Message, err error) {
		m.sending = false
		if err != nil {
			m.sendFailed = true
			m.confirm = false
			return
		}
		m.modal.Dismiss()
		if msg.Channel == "" {
			msg.Channel = dest
		}
		m.h.Navigate(msgRoute(msg))
	})
}

// ---- people field ----

func shareScore(t *shareTarget, q string) int {
	best := actFuzzy(t.lower, q)
	for _, k := range t.keys {
		if s := actFuzzy(k, q); s > best {
			best = s
		}
	}
	return best
}

func (m *ShareSectionModal) refilter(force bool) {
	q := strings.ToLower(strings.TrimSpace(m.to.Text()))
	if q == m.query && !force {
		return
	}
	if q != m.query {
		m.hl = 0
	}
	m.query = q
	type hit struct {
		t     shareTarget
		score int
	}
	var hits []hit
	seen := map[string]bool{}
	for _, list := range [][]shareTarget{m.local, m.remote} {
		for i := range list {
			t := list[i]
			if seen[t.key()] || m.chosen(t.key()) {
				continue
			}
			sc := 0
			if q != "" {
				if sc = shareScore(&t, q); sc < 0 {
					continue
				}
			}
			seen[t.key()] = true
			hits = append(hits, hit{t, sc})
		}
	}
	if q != "" {
		sort.SliceStable(hits, func(a, b int) bool { return hits[a].score > hits[b].score })
	}
	m.match = m.match[:0]
	for k := 0; k < len(hits) && k < len(m.sugg); k++ {
		m.match = append(m.match, hits[k].t)
	}
	if m.hl >= len(m.match) {
		m.hl = 0
	}
	if q != "" && q != m.remoteQ {
		m.remoteQ = q
		self := ""
		if a := m.h.Core(); a != nil {
			self = a.S.SelfID()
		}
		run(m.h, func(ctx context.Context) ([]core.User, error) {
			a, err := cxApp(m.h)
			if err != nil {
				return nil, err
			}
			return a.SearchUsers(ctx, q, 20)
		}, func(us []core.User, err error) {
			if err != nil || q != m.query {
				return
			}
			m.remote = m.remote[:0]
			for _, u := range us {
				if u.IsBot || u.Deleted || u.ID == self || u.ID == "USLACKBOT" || u.Workflow {
					continue
				}
				t := shareTarget{user: u.ID, title: userName(u), kind: "im", avatar: u.Avatar}
				t.lower = strings.ToLower(t.title)
				if u.RealName != "" {
					t.keys = append(t.keys, strings.ToLower(u.RealName))
				}
				if u.Handle != "" {
					t.keys = append(t.keys, strings.ToLower(u.Handle))
				}
				m.remote = append(m.remote, t)
			}
			m.refilter(true)
			m.h.Invalidate()
		})
	}
}

func (m *ShareSectionModal) events(gtx layout.Context) {
	open := gtx.Focused(&m.to) && len(m.match) > 0
	var filters []event.Filter
	if open {
		filters = append(filters,
			key.Filter{Focus: &m.to, Name: key.NameUpArrow},
			key.Filter{Focus: &m.to, Name: key.NameDownArrow},
			key.Filter{Focus: &m.to, Name: key.NameTab},
		)
	}
	if m.to.Len() == 0 && len(m.chips) > 0 {
		filters = append(filters, key.Filter{Focus: &m.to, Name: key.NameDeleteBackward})
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
			m.hl = (m.hl - 1 + len(m.match)) % len(m.match)
		case key.NameDownArrow:
			m.hl = (m.hl + 1) % len(m.match)
		case key.NameTab:
			m.addChip(m.match[m.hl])
		case key.NameDeleteBackward:
			m.chips = m.chips[:len(m.chips)-1]
			m.resolve()
			m.refilter(true)
		}
	}
	for {
		e, ok := m.to.Update(gtx)
		if !ok {
			break
		}
		if _, sub := e.(widget.SubmitEvent); sub && gtx.Focused(&m.to) && len(m.match) > 0 {
			m.addChip(m.match[m.hl])
		}
	}
	m.refilter(false)
	for i := range m.match {
		if m.sugg[i].Clicked(gtx) {
			m.addChip(m.match[i])
			break
		}
	}
	for i := 0; i < len(m.chips); i++ {
		if m.chips[i].rm.Clicked(gtx) {
			m.chips = append(m.chips[:i:i], m.chips[i+1:]...)
			m.resolve()
			m.refilter(true)
			m.focusTo = true
			break
		}
	}
	if m.toBox.Clicked(gtx) {
		m.focusTo = true
	}
	if m.noteBox.Clicked(gtx) {
		gtx.Execute(key.FocusCmd{Tag: &m.note})
	}
	if m.allBtn.Clicked(gtx) {
		m.ToggleAll()
	}
	for id, b := range m.rowBtn {
		if b.Clicked(gtx) {
			m.Toggle(id)
		}
	}
}

// ---- layout ----

func (m *ShareSectionModal) layout(gtx layout.Context) layout.Dimensions {
	if m.modal.done {
		return layout.Dimensions{}
	}
	th := m.h.Theme()
	cl := colorsOf(th)
	w := gtx.Constraints.Max.X
	if len(m.ordered) == 0 {
		m.modal.o.FooterNote = ""
		m.modal.o.Buttons = []*ModalButton{m.cancel, m.share}
		return m.layoutEmpty(gtx, th, cl, w)
	}
	m.events(gtx)
	if m.modal.done {
		return layout.Dimensions{}
	}
	if m.focusTo {
		gtx.Execute(key.FocusCmd{Tag: &m.to})
		m.focusTo = false
	}
	m.modal.o.FooterNote = nsShare.T("{count} selected", "count", len(m.Selected()))
	if m.confirm {
		m.modal.o.Buttons = []*ModalButton{m.editCh, m.confirmShare}
	} else {
		m.modal.o.Buttons = []*ModalButton{m.cancel, m.share}
	}
	y := 0
	put := func(wd layout.Widget) {
		call, d := record(loose(gtx, image.Pt(w, 1<<20)), wd)
		macroAt(gtx, call, image.Pt(0, y))
		y += d.Size.Y
	}
	label := func(s string) {
		put(func(gtx layout.Context) layout.Dimensions { return text(gtx, th, 15, theme.Bold, cl.Fg, s) })
		y += dp(gtx, 8)
	}
	if m.Failed() {
		put(func(gtx layout.Context) layout.Dimensions { return m.alert(gtx, th, cl, w) })
		y += dp(gtx, 16)
	}
	put(func(gtx layout.Context) layout.Dimensions {
		return para(gtx, th, 15, 22, theme.Regular, cl.Fg, nsShare.T("People will be invited to join channels you share. You’ll get a chance to review any private channels before sending."), 0)
	})
	y += dp(gtx, 20)
	label(nsShare.T("People"))
	put(func(gtx layout.Context) layout.Dimensions { return m.picker(gtx, th, cl) })
	pickerBottom := y
	if e := m.DestinationError(); e != "" {
		y += dp(gtx, 4)
		put(func(gtx layout.Context) layout.Dimensions { return text(gtx, th, 13, theme.Regular, cl.Danger, e) })
	}
	y += dp(gtx, 20)
	label(nsShare.T("Add a message"))
	put(func(gtx layout.Context) layout.Dimensions { return m.noteField(gtx, th, cl) })
	y += dp(gtx, 20)
	label(nsShare.T("Channels"))
	put(func(gtx layout.Context) layout.Dimensions { return m.channelList(gtx, th, cl, w) })
	if m.removed {
		y += dp(gtx, 8)
		put(func(gtx layout.Context) layout.Dimensions {
			return para(gtx, th, 13, 18, theme.Regular, cl.Fg2, nsShare.T("Only channels in your organization can be shared. Apps and other objects will be removed."), 0)
		})
	}
	if m.confirm {
		y += dp(gtx, 16)
		// one string: the <strong> question, then the rest (Clack draws
		// them as two paragraphs, as before)
		var ask, rest string
		for _, p := range nsShare.RT("<strong>Are you sure you want to include {numPrivateChannels, plural, =1 {# private channel} other {# private channels}}?</strong> Recipients will have {numDaysToJoin} days to join and see channel history, including files.",
			"numPrivateChannels", m.privateSelected(), "numDaysToJoin", 14) {
			if p.Tag == "strong" {
				ask += p.Text
			} else {
				rest += p.Text
			}
		}
		ask, rest = strings.TrimSpace(ask), strings.TrimSpace(rest)
		put(func(gtx layout.Context) layout.Dimensions {
			return para(gtx, th, 15, 22, theme.Bold, cl.Fg, ask, 0)
		})
		put(func(gtx layout.Context) layout.Dimensions {
			return para(gtx, th, 15, 22, theme.Regular, cl.Fg, rest, 0)
		})
	}
	if gtx.Focused(&m.to) && len(m.match) > 0 {
		// the options float over what follows the field
		call, _ := record(loose(gtx, image.Pt(w, 1<<20)), func(gtx layout.Context) layout.Dimensions { return m.suggestions(gtx, th, cl) })
		macroAt(gtx, call, image.Pt(0, pickerBottom+dp(gtx, 4)))
	}
	return layout.Dimensions{Size: image.Pt(w, y+dp(gtx, 4))}
}

func (m *ShareSectionModal) layoutEmpty(gtx layout.Context, th *theme.Theme, cl *Colors, w int) layout.Dimensions {
	y := dp(gtx, 24)
	call, d := record(loose(gtx, image.Pt(w, 1<<20)), func(gtx layout.Context) layout.Dimensions {
		return text(gtx, th, 18, theme.Bold, cl.Fg, nsShare.T("This section can’t be shared"))
	})
	macroAt(gtx, call, image.Pt((w-d.Size.X)/2, y))
	y += d.Size.Y + dp(gtx, 8)
	call, d = record(loose(gtx, image.Pt(w-dp(gtx, 48), 1<<20)), func(gtx layout.Context) layout.Dimensions {
		return para(gtx, th, 15, 22, theme.Regular, cl.Fg2, nsShare.T("It’s likely because only apps or external channels are included and right now you can only share channels in your organization."), 0)
	})
	macroAt(gtx, call, image.Pt(dp(gtx, 24), y))
	y += d.Size.Y + dp(gtx, 24)
	return layout.Dimensions{Size: image.Pt(w, y)}
}

// alert is SendChannelSectionAlert: an inline error banner.
func (m *ShareSectionModal) alert(gtx layout.Context, th *theme.Theme, cl *Colors, w int) layout.Dimensions {
	pad := dp(gtx, 12)
	call, d := record(loose(gtx, image.Pt(w-2*pad-dp(gtx, 28), 1<<20)), func(gtx layout.Context) layout.Dimensions {
		return para(gtx, th, 15, 22, theme.Regular, cl.Fg, nsShare.T("Something went wrong and that didn’t work right. Give it another try."), 0)
	})
	r := image.Rect(0, 0, w, d.Size.Y+2*pad)
	rrect(gtx, r, dp(gtx, 8), cl.Danger)
	rrect(gtx, r.Inset(border1(gtx)), dp(gtx, 8)-border1(gtx), theme.Mix(cl.Bg, cl.Danger, 0.08))
	at(gtx, image.Pt(pad, pad+(dp(gtx, 22)-dp(gtx, 20))/2), func(gtx layout.Context) layout.Dimensions {
		return icon(gtx, "warning", 20, cl.Danger)
	})
	macroAt(gtx, call, image.Pt(pad+dp(gtx, 28), pad))
	return layout.Dimensions{Size: r.Max}
}

// picker draws the People field with its chips.
func (m *ShareSectionModal) picker(gtx layout.Context, th *theme.Theme, cl *Colors) layout.Dimensions {
	w := gtx.Constraints.Max.X
	padX, padY := dp(gtx, 8), dp(gtx, 5)
	lineH, gap := dp(gtx, 28), dp(gtx, 4)
	innerW := maxInt(1, w-2*padX)
	content, cd := record(gtx, func(gtx layout.Context) layout.Dimensions {
		x, y := 0, 0
		for _, c := range m.chips {
			call, d := record(gtx, func(gtx layout.Context) layout.Dimensions { return m.chip(gtx, th, cl, c, innerW) })
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
				if m.to.Len() == 0 && len(m.chips) == 0 {
					call, _ := record(gtx, func(gtx layout.Context) layout.Dimensions {
						return text(gtx, th, 15, theme.Regular, cl.Placeholder, nsShare.T("Please select an option"))
					})
					call.Add(gtx.Ops)
				}
				return th.Editor(gtx, &m.to, unit.Sp(15), cl.Fg, theme.WithAlpha(cl.Focus, 0x55))
			})
		})
		return layout.Dimensions{Size: image.Pt(innerW, y+lineH)}
	})
	r := image.Rect(0, 0, w, cd.Size.Y+2*padY)
	actFocusBox(gtx, cl, r, gtx.Focused(&m.to))
	m.toBox.Layout(exact(gtx, r.Max), func(gtx layout.Context) layout.Dimensions {
		defer clip.Rect(r).Push(gtx.Ops).Pop()
		pointer.CursorText.Add(gtx.Ops)
		return layout.Dimensions{Size: r.Max}
	})
	macroAt(gtx, content, image.Pt(padX, padY))
	return layout.Dimensions{Size: r.Max}
}

func (m *ShareSectionModal) targetIcon(gtx layout.Context, t *shareTarget, bg color.NRGBA, sel bool) layout.Dimensions {
	if t.kind == "mpim" {
		return mpimTile(gtx, m.h, t.members, 20, bg, sel)
	}
	return avatar(gtx, m.h, t.avatar, 20)
}

func (m *ShareSectionModal) chip(gtx layout.Context, th *theme.Theme, cl *Colors, c *shareChip, maxW int) layout.Dimensions {
	ch := dp(gtx, 26)
	tw := textW(gtx, th, 15, theme.Bold, c.t.title)
	iconW, rmW := dp(gtx, 20), dp(gtx, 20)
	w := dp(gtx, 4) + iconW + dp(gtx, 6) + tw + dp(gtx, 2) + rmW + dp(gtx, 2)
	if w > maxW {
		tw -= w - maxW
		w = maxW
	}
	chipBg := theme.Mix(cl.Bg, cl.Link, float32(0x1f)/255)
	rrect(gtx, image.Rect(0, 0, w, ch), dp(gtx, 4), chipBg)
	x := dp(gtx, 4)
	at(gtx, image.Pt(x, (ch-iconW)/2), func(gtx layout.Context) layout.Dimensions { return m.targetIcon(gtx, &c.t, chipBg, false) })
	x += iconW + dp(gtx, 6)
	at(gtx, image.Pt(x, 0), func(gtx layout.Context) layout.Dimensions {
		gtx.Constraints = layout.Constraints{Max: image.Pt(maxInt(1, tw), ch)}
		return vcenter(gtx, ch, func(gtx layout.Context) layout.Dimensions { return text(gtx, th, 15, theme.Bold, cl.Fg, c.t.title) })
	})
	x += maxInt(1, tw) + dp(gtx, 2)
	at(gtx, image.Pt(x, (ch-rmW)/2), func(gtx layout.Context) layout.Dimensions {
		return iconButton(gtx, &c.rm, "close-small", 20, 14, cl.Fg2, theme.WithAlpha(cl.Link, 0x26), 4)
	})
	return layout.Dimensions{Size: image.Pt(w, ch)}
}

// suggestions are the field's options (people and group DMs).
func (m *ShareSectionModal) suggestions(gtx layout.Context, th *theme.Theme, cl *Colors) layout.Dimensions {
	w := gtx.Constraints.Max.X
	rh, pad := dp(gtx, 36), dp(gtx, 6)
	r := image.Rect(0, 0, w, len(m.match)*rh+2*pad)
	rrect(gtx, r, dp(gtx, 8), cl.Ring)
	rrect(gtx, r.Inset(border1(gtx)), dp(gtx, 8)-border1(gtx), cl.Bg)
	for i := range m.match {
		if m.sugg[i].Hovered() {
			m.hl = i
		}
	}
	for i := range m.match {
		t := &m.match[i]
		at(gtx, image.Pt(0, pad+i*rh), func(gtx layout.Context) layout.Dimensions {
			gtx.Constraints = layout.Exact(image.Pt(w, rh))
			return clickBox(gtx, &m.sugg[i], nil, func(gtx layout.Context) layout.Dimensions {
				fg, bg, sel := cl.Fg, cl.Bg, i == m.hl
				if sel {
					bg, fg = cl.Highlight, cl.HighText
					fill(gtx, image.Rect(0, 0, w, rh), bg)
				}
				at(gtx, image.Pt(dp(gtx, 16), (rh-dp(gtx, 20))/2), func(gtx layout.Context) layout.Dimensions { return m.targetIcon(gtx, t, bg, sel) })
				x := dp(gtx, 46)
				at(gtx, image.Pt(x, 0), func(gtx layout.Context) layout.Dimensions {
					gtx.Constraints = layout.Constraints{Max: image.Pt(maxInt(1, w-x-dp(gtx, 16)), rh)}
					return vcenter(gtx, rh, func(gtx layout.Context) layout.Dimensions { return text(gtx, th, 15, theme.Regular, fg, t.title) })
				})
				return layout.Dimensions{Size: image.Pt(w, rh)}
			})
		})
	}
	return layout.Dimensions{Size: r.Max}
}

// noteField is "Add a message": one to three lines.
func (m *ShareSectionModal) noteField(gtx layout.Context, th *theme.Theme, cl *Colors) layout.Dimensions {
	w := gtx.Constraints.Max.X
	padX, padY := dp(gtx, 12), dp(gtx, 9)
	tw := maxInt(1, w-2*padX)
	content, cd := record(gtx, func(gtx layout.Context) layout.Dimensions {
		gtx.Constraints = layout.Constraints{Min: image.Pt(tw, 0), Max: image.Pt(tw, dp(gtx, 66))}
		if m.note.Len() == 0 {
			call, _ := record(gtx, func(gtx layout.Context) layout.Dimensions {
				return text(gtx, th, 15, theme.Regular, cl.Placeholder, nsShare.T("Here’s a shared section to keep all our channels organized."))
			})
			call.Add(gtx.Ops)
		}
		return th.Editor(gtx, &m.note, unit.Sp(15), cl.Fg, theme.WithAlpha(cl.Focus, 0x55))
	})
	r := image.Rect(0, 0, w, maxInt(cd.Size.Y, dp(gtx, 22))+2*padY)
	actFocusBox(gtx, cl, r, gtx.Focused(&m.note))
	m.noteBox.Layout(exact(gtx, r.Max), func(gtx layout.Context) layout.Dimensions {
		defer clip.Rect(r).Push(gtx.Ops).Pop()
		pointer.CursorText.Add(gtx.Ops)
		return layout.Dimensions{Size: r.Max}
	})
	macroAt(gtx, content, image.Pt(padX, padY))
	return layout.Dimensions{Size: r.Max}
}

// shareErrorText is ShareChannelSectionChannelListItem's tooltip lines.
func shareErrorText(errs []string, destKind, workspace string) string {
	var out []string
	for _, e := range errs {
		switch e {
		case "target_user_not_in_workspace":
			switch {
			case destKind == "im" && workspace != "":
				out = append(out, nsShare.T("This person needs to be in {workspaceName} workspace for the channel to be shared.", "workspaceName", workspace))
			case destKind == "im":
				out = append(out, nsShare.T("This person needs to be in the workspace before the channel can be shared."))
			case workspace != "":
				out = append(out, nsShare.T("At least one person is not in the {workspaceName} workspace so this channel can’t be shared.", "workspaceName", workspace))
			default:
				out = append(out, nsShare.T("At least one person is not in the workspace so this channel can’t be shared."))
			}
		case "actor_cannot_invite_member":
			out = append(out, nsShare.T("Permission required to add people to this channel."))
		}
	}
	return strings.Join(out, "\n\n")
}

// channelList is ShareChannelSectionChannelList: "Select all shareable
// channels" (checked when every allowed channel is, indeterminate for
// some), then a 32px row per channel.
func (m *ShareSectionModal) channelList(gtx layout.Context, th *theme.Theme, cl *Colors, w int) layout.Dimensions {
	a := m.h.Core()
	allowed, picked := 0, map[string]bool{}
	for _, id := range m.sel {
		picked[id] = true
	}
	all := true
	for _, id := range m.ordered {
		if len(m.errs[id]) == 0 {
			allowed++
			all = all && picked[id]
		}
	}
	checked := len(m.sel) > 0 && allowed > 0 && all
	indet := !checked && len(m.sel) > 0
	hh := dp(gtx, 28)
	at(gtx, image.Point{}, func(gtx layout.Context) layout.Dimensions {
		return clickBox(gtx, &m.allBtn, nil, func(gtx layout.Context) layout.Dimensions {
			at(gtx, image.Pt(0, (hh-dp(gtx, 18))/2), func(gtx layout.Context) layout.Dimensions { return Checkbox(gtx, th, checked, indet, false) })
			s := nsShare.T("Select all shareable channels")
			at(gtx, image.Pt(dp(gtx, 26), 0), func(gtx layout.Context) layout.Dimensions {
				return vcenter(gtx, hh, func(gtx layout.Context) layout.Dimensions { return text(gtx, th, 15, theme.Regular, cl.Fg, s) })
			})
			return layout.Dimensions{Size: image.Pt(dp(gtx, 26)+textW(gtx, th, 15, theme.Regular, s), hh)}
		})
	})
	y := hh + dp(gtx, 8)
	rh := dp(gtx, 32)
	box := image.Rect(0, y, w, y+rh*len(m.ordered)+dp(gtx, 19)-dp(gtx, 8))
	rrect(gtx, box, dp(gtx, 8), cl.InputBorder)
	rrect(gtx, box.Inset(border1(gtx)), dp(gtx, 8)-border1(gtx), cl.Bg)
	y += dp(gtx, 6)
	workspace := m.workspace
	for _, id := range m.ordered {
		id := id
		b := m.rowBtn[id]
		if b == nil {
			b = new(widget.Clickable)
			m.rowBtn[id] = b
		}
		tip := m.tips[id]
		if tip == nil {
			tip = &TipState{MaxWidth: 280}
			m.tips[id] = tip
		}
		errs := m.errs[id]
		disabled := len(errs) > 0
		on := picked[id] && !disabled
		var c core.Conv
		if a != nil {
			c, _ = a.Conv(id)
		}
		at(gtx, image.Pt(0, y), func(gtx layout.Context) layout.Dimensions {
			gtx.Constraints = layout.Exact(image.Pt(w, rh))
			d := clickBox(gtx, b, func(gtx layout.Context, s image.Point, hovered bool) {
				if hovered && !disabled {
					fill(gtx, image.Rectangle{Max: s}, cl.BgHover)
				}
			}, func(gtx layout.Context) layout.Dimensions {
				fg := cl.Fg
				if disabled {
					fg = cl.Fg2
				}
				x := dp(gtx, 12)
				at(gtx, image.Pt(x, (rh-dp(gtx, 18))/2), func(gtx layout.Context) layout.Dimensions { return Checkbox(gtx, th, on, false, disabled) })
				x += dp(gtx, 28)
				at(gtx, image.Pt(x, (rh-dp(gtx, 16))/2), func(gtx layout.Context) layout.Dimensions { return icon(gtx, convIcon(c.Kind), 16, fg) })
				x += dp(gtx, 22)
				at(gtx, image.Pt(x, 0), func(gtx layout.Context) layout.Dimensions {
					gtx.Constraints = layout.Constraints{Max: image.Pt(maxInt(1, w-x-dp(gtx, 12)), rh)}
					return vcenter(gtx, rh, func(gtx layout.Context) layout.Dimensions { return text(gtx, th, 15, theme.Regular, fg, c.Name) })
				})
				return layout.Dimensions{Size: image.Pt(w, rh)}
			})
			if disabled {
				Tooltip(gtx, th, tip, b.Hovered(), d.Size, Above, shareErrorText(errs, m.destKind, workspace), "")
			}
			return d
		})
		y += rh
	}
	return layout.Dimensions{Size: image.Pt(w, box.Max.Y)}
}
