package views

// Channel details: Slack's conversation details modal (spec:
// /tmp/ui-views-spec/details.md). Channels get About / Members / Agents &
// apps / Automations / Tabs / Settings; 1:1 DMs get a profile header and
// About / Tabs / Settings; group DMs get About / Members / Tabs /
// Settings. Writes (SetTopic, Join, Leave, Hide) only run from the user's
// clicks.

import (
	"context"
	"image"
	"io"
	"net/url"
	"strings"
	"time"

	"gioui.org/io/clipboard"
	"gioui.org/io/key"
	"gioui.org/layout"
	"gioui.org/op"
	"gioui.org/op/clip"
	"gioui.org/unit"
	"gioui.org/widget"

	"github.com/some-du6e/yoloslackgoclient/internal/core"
	"github.com/some-du6e/yoloslackgoclient/internal/richtext"
	"github.com/some-du6e/yoloslackgoclient/internal/ui/richview"
	"github.com/some-du6e/yoloslackgoclient/internal/ui/theme"
)

// Tab kinds; which ones show depends on the conversation type.
const (
	cdAbout = iota
	cdMembers
	cdApps
	cdAutomations
	cdTabs
	cdSettings
)

var cdTabLabels = map[int]string{
	cdAbout: "About", cdMembers: "Members", cdApps: "Agents & apps", cdAutomations: "Automations",
	cdTabs: "Tabs", cdSettings: "Settings",
}

const (
	cdWidth       = 580 // dialog width
	cdMaxH        = 765 // dialog height cap; window height minus 2x68 below that
	cdMargin      = 68
	cdRadius      = 8
	cdPadX        = 28 // header, card and list side inset
	cdHeadH       = 130
	cdHeadDMH     = 188
	cdLinkPrefix  = "https://hackclub.slack.com/archives/"
	cdUnsupported = "Not supported yet"
)

var cdTileBg = argb(0x1a1d9bd1) // #1d9bd1@0.1 icon tiles

type cdMembersPage struct {
	users []core.User
	next  string
}

// ChannelDetails is the conversation details dialog. It draws its own
// scrim over the whole constraints; the Host drops it once Closed is true.
type ChannelDetails struct {
	base
	id     string
	kind   string // channel | private | im | mpim
	closed bool
	press  dirPress

	info      async[core.ChannelDetails]
	members   async[cdMembersPage]
	bookmarks async[[]core.Bookmark]
	topicOp   async[bool]
	leaveOp   async[bool]
	joinOp    async[bool]

	topicPending, leavePending, joinPending bool
	prevTopic                               string
	prevTopicDoc                            []richtext.Block

	tab      *tabs
	tabKinds []int
	lists    map[int]*widget.List

	scrim, dlg, closeBtn            widget.Clickable
	star, notif, call, vip, hide    widget.Clickable
	retry                           widget.Clickable
	editTopic, saveTopic, cancelTop widget.Clickable
	editPurpose, createdBy          widget.Clickable
	copyID, leaveRow, leaveYes      widget.Clickable
	leaveNo, joinRow, emails        widget.Clickable
	addPeople, addPeopleRow         widget.Clickable
	viewProfile, moreMembers        widget.Clickable
	addApp, addWorkflow             widget.Clickable
	huddleStart, huddleLink         widget.Clickable

	editing      bool
	focusTopic   int
	confirmLeave bool
	topicEd      widget.Editor

	find      *search
	memFilter dirDropdown
	tabsPerm  dirDropdown
	memberClk map[string]*widget.Clickable
	filtKey   string
	filt      []core.User
	rowClk    map[string]*widget.Clickable
}

// NewChannelDetails opens the details dialog for a conversation.
func NewChannelDetails(h Host, channelID string) *ChannelDetails {
	d := &ChannelDetails{
		base:      newBase(h),
		id:        channelID,
		kind:      "channel",
		lists:     map[int]*widget.List{},
		memberClk: map[string]*widget.Clickable{},
		rowClk:    map[string]*widget.Clickable{},
		find:      newSearch("Find people or agents"),
		memFilter: dirDropdown{labels: []string{"All", "People", "Agents"}},
		tabsPerm:  dirDropdown{labels: []string{"Everyone"}},
		topicEd:   widget.Editor{SingleLine: true, Submit: true, MaxLen: 250},
	}
	if strings.HasPrefix(channelID, "D") {
		d.kind = "im"
	}
	if c, ok := h.Core().Conv(channelID); ok && c.Kind != "" {
		d.kind = c.Kind
	}
	d.setTabs()
	return d
}

// Closed reports whether the dialog was dismissed (views.Closer).
func (d *ChannelDetails) Closed() bool { return d.closed }

func cdGroup(kind string) string {
	if kind == "im" || kind == "mpim" {
		return kind
	}
	return "channel"
}

func (d *ChannelDetails) setTabs() {
	var kinds []int
	switch cdGroup(d.kind) {
	case "im":
		kinds = []int{cdAbout, cdTabs, cdSettings}
	case "mpim":
		kinds = []int{cdAbout, cdMembers, cdTabs, cdSettings}
	default:
		kinds = []int{cdAbout, cdMembers, cdApps, cdAutomations, cdTabs, cdSettings}
	}
	labels := make([]string, len(kinds))
	for i, k := range kinds {
		labels[i] = cdTabLabels[k]
	}
	d.tab = newTabs(labels...)
	d.tabKinds = kinds
}

func (d *ChannelDetails) list(k int) *widget.List {
	l, ok := d.lists[k]
	if !ok {
		l = new(widget.List)
		l.Axis = layout.Vertical
		d.lists[k] = l
	}
	return l
}

// ---- loading and actions ----

func (d *ChannelDetails) loadInfo() {
	c, id := d.h.Core(), d.id
	d.info.load(d.h, false, func(ctx context.Context) (core.ChannelDetails, error) {
		return c.ChannelInfo(ctx, id)
	})
}

func (d *ChannelDetails) loadMembers(marker string) {
	var prev []core.User
	if marker != "" {
		cur, _, _ := d.members.get()
		prev = append([]core.User(nil), cur.users...)
	}
	c, id := d.h.Core(), d.id
	d.members.load(d.h, true, func(ctx context.Context) (cdMembersPage, error) {
		us, next, err := c.Members(ctx, id, marker, false)
		if err != nil {
			return cdMembersPage{}, err
		}
		return cdMembersPage{users: append(prev, us...), next: next}, nil
	})
}

func (d *ChannelDetails) loadBookmarks() {
	c, id := d.h.Core(), d.id
	d.bookmarks.load(d.h, false, func(ctx context.Context) ([]core.Bookmark, error) {
		return c.Bookmarks(ctx, id)
	})
}

// saveTopicNow applies the edited topic optimistically and sends it.
func (d *ChannelDetails) saveTopicNow() {
	t := strings.TrimSpace(d.topicEd.Text())
	d.editing = false
	inf, _, _ := d.info.get()
	if t == inf.Conv.Topic {
		return
	}
	d.prevTopic, d.prevTopicDoc = inf.Conv.Topic, inf.Conv.TopicDoc
	d.info.mutate(func(v *core.ChannelDetails) { v.Conv.Topic, v.Conv.TopicDoc = t, nil })
	c, id := d.h.Core(), d.id
	d.topicPending = true
	d.topicOp.load(d.h, false, func(ctx context.Context) (bool, error) { return true, c.SetTopic(ctx, id, t) })
}

func (d *ChannelDetails) startLeave() {
	c, id := d.h.Core(), d.id
	d.leavePending = true
	d.leaveOp.load(d.h, false, func(ctx context.Context) (bool, error) { return true, c.Leave(ctx, id) })
}

func cdCopy(gtx C, s string) {
	gtx.Execute(clipboard.WriteCmd{Type: "application/text", Data: io.NopCloser(strings.NewReader(s))})
}

// conv is the best conversation record available this frame.
func (d *ChannelDetails) conv(inf core.ChannelDetails, loaded bool) core.Conv {
	if loaded && inf.Conv.ID != "" {
		return inf.Conv
	}
	if c, ok := d.h.Core().Conv(d.id); ok {
		return c
	}
	return inf.Conv
}

func (d *ChannelDetails) dmUser(inf core.ChannelDetails) string {
	if inf.Conv.User != "" {
		return inf.Conv.User
	}
	if c, ok := d.h.Core().Conv(d.id); ok {
		return c.User
	}
	return ""
}

// update handles keys, clicks and finished writes before drawing.
func (d *ChannelDetails) update(gtx C) {
	for {
		ev, ok := gtx.Event(key.Filter{Name: key.NameEscape})
		if !ok {
			break
		}
		e, ok := ev.(key.Event)
		if !ok || e.State != key.Press {
			continue
		}
		switch {
		case d.memFilter.open || d.tabsPerm.open:
			d.memFilter.open, d.tabsPerm.open = false, false
		case d.editing:
			d.editing = false
			gtx.Execute(key.FocusCmd{})
		case d.confirmLeave:
			d.confirmLeave = false
		default:
			d.closed = true
		}
	}
	if d.scrim.Clicked(gtx) || d.closeBtn.Clicked(gtx) {
		d.closed = true
	}
	d.dlg.Clicked(gtx)
	if d.tab.update(gtx) {
		d.editing, d.confirmLeave = false, false
		d.memFilter.open, d.tabsPerm.open = false, false
	}
	d.memFilter.update(gtx)
	d.tabsPerm.update(gtx)

	if !d.info.started() {
		d.loadInfo()
	}
	inf, loaded, _ := d.info.get()
	if loaded && inf.Conv.Kind != "" && inf.Conv.Kind != d.kind {
		regroup := cdGroup(inf.Conv.Kind) != cdGroup(d.kind)
		d.kind = inf.Conv.Kind
		if regroup {
			d.setTabs()
		}
	}

	for _, c := range []*widget.Clickable{&d.star, &d.notif, &d.call, &d.vip, &d.editPurpose, &d.emails,
		&d.addPeople, &d.addPeopleRow, &d.addApp, &d.addWorkflow, &d.huddleStart, &d.huddleLink} {
		if c.Clicked(gtx) {
			d.h.Toast(cdUnsupported)
		}
	}

	// Topic editing.
	if d.editTopic.Clicked(gtx) && loaded {
		d.editing, d.confirmLeave = true, false
		d.topicEd.SetText(inf.Conv.Topic)
		n := d.topicEd.Len()
		d.topicEd.SetCaret(n, n)
		d.focusTopic = 3
	}
	if d.editing && d.focusTopic > 0 {
		// The editor appears this frame; retry focusing for a few frames.
		if gtx.Focused(&d.topicEd) {
			d.focusTopic = 0
		} else {
			gtx.Execute(key.FocusCmd{Tag: &d.topicEd})
			gtx.Execute(op.InvalidateCmd{})
			d.focusTopic--
		}
	}
	if d.editing {
		for {
			ev, ok := d.topicEd.Update(gtx)
			if !ok {
				break
			}
			if _, ok := ev.(widget.SubmitEvent); ok && d.editing {
				d.saveTopicNow()
			}
		}
	}
	if d.cancelTop.Clicked(gtx) {
		d.editing = false
	}
	if d.saveTopic.Clicked(gtx) && d.editing {
		d.saveTopicNow()
	}
	if d.topicPending {
		if _, done, err := d.topicOp.get(); done {
			d.topicPending = false
			if err != nil {
				prev, doc := d.prevTopic, d.prevTopicDoc
				d.info.mutate(func(v *core.ChannelDetails) { v.Conv.Topic, v.Conv.TopicDoc = prev, doc })
				d.h.Toast("Couldn’t update the topic: " + friendlyErr(err))
			}
		}
	}

	// Leave / join / hide.
	name := inf.Conv.Name
	if d.leaveRow.Clicked(gtx) {
		d.confirmLeave, d.editing = true, false
	}
	if d.leaveNo.Clicked(gtx) {
		d.confirmLeave = false
	}
	if d.leaveYes.Clicked(gtx) && d.confirmLeave && !d.leavePending {
		d.startLeave()
	}
	if d.hide.Clicked(gtx) && !d.leavePending {
		d.startLeave()
	}
	if d.leavePending {
		if _, done, err := d.leaveOp.get(); done {
			d.leavePending = false
			dm := cdGroup(d.kind) == "im"
			switch {
			case err != nil && dm:
				d.h.Toast("Couldn’t hide this conversation: " + friendlyErr(err))
			case err != nil:
				d.confirmLeave = false
				d.h.Toast("Couldn’t leave #" + name + ": " + friendlyErr(err))
			case dm:
				d.h.Toast("Conversation hidden")
				d.closed = true
				d.h.Navigate("dms")
			default:
				d.h.Toast("You left #" + name)
				d.closed = true
				d.h.Navigate("activity")
			}
		}
	}
	if d.joinRow.Clicked(gtx) && loaded && !inf.IsMember && !d.joinPending {
		c, id := d.h.Core(), d.id
		d.joinPending = true
		d.joinOp.load(d.h, false, func(ctx context.Context) (bool, error) {
			_, err := c.Join(ctx, id)
			return err == nil, err
		})
	}
	if d.joinPending {
		if _, done, err := d.joinOp.get(); done {
			d.joinPending = false
			if err != nil {
				d.h.Toast("Couldn’t join #" + name + ": " + friendlyErr(err))
			} else {
				d.info.mutate(func(v *core.ChannelDetails) { v.IsMember = true; v.Conv.NumMembers++ })
				d.h.Toast("You joined #" + name)
			}
		}
	}

	// Copy, profile.
	if d.copyID.Clicked(gtx) {
		cdCopy(gtx, d.id)
		d.h.Toast("Channel ID copied to clipboard")
	}
	if d.viewProfile.Clicked(gtx) {
		if uid := d.dmUser(inf); uid != "" {
			d.h.OpenProfile(d.press.at, uid)
		}
	}
	if d.createdBy.Clicked(gtx) && inf.Conv.Creator != "" {
		d.h.OpenProfile(d.press.at, inf.Conv.Creator)
	}
}

// ---- drawing ----

// Layout draws the scrim over the constraints and the centered dialog.
func (d *ChannelDetails) Layout(gtx C) D {
	d.frame()
	full := gtx.Constraints.Max
	defer d.press.begin(gtx).Pop()
	d.update(gtx)
	if d.closed {
		return D{Size: full}
	}
	scrim := argb(0xb31d1c1d) // #1d1c1d@0.7
	if d.th.Dark {
		scrim = d.p.Overlay
	}
	d.scrim.Layout(gtx, func(gtx C) D {
		fill(gtx, image.Rectangle{Max: full}, scrim)
		return D{Size: full}
	})
	dw := min(gtx.Dp(cdWidth), full.X-gtx.Dp(56))
	dh := min(gtx.Dp(cdMaxH), full.Y-2*gtx.Dp(cdMargin))
	if dh < gtx.Dp(320) {
		dh = full.Y - gtx.Dp(56)
	}
	if dw <= 0 || dh <= 0 {
		return D{Size: full}
	}
	func() {
		defer op.Offset(image.Pt((full.X-dw)/2, (full.Y-dh)/2)).Push(gtx.Ops).Pop()
		r := image.Rect(0, 0, dw, dh)
		rad := gtx.Dp(cdRadius)
		theme.Shadow(gtx, r, rad, gtx.Dp(24), argb(0x59000000))
		rrect(gtx, r.Inset(-max(1, gtx.Dp(1))), rad+max(1, gtx.Dp(1)), argb(0x211d1c1d))
		rrect(gtx, r, rad, d.p.Surface)
		defer clip.UniformRRect(r, rad).Push(gtx.Ops).Pop()
		g := gtx
		g.Constraints = layout.Exact(r.Max)
		// Swallows clicks on the dialog's empty areas so they don't reach
		// the scrim; the content is drawn above it as a sibling.
		d.dlg.Layout(g, func(gtx C) D { return D{Size: r.Max} })
		inf, loaded, _ := d.info.get()
		col(g,
			layout.Rigid(func(gtx C) D { return d.head(gtx, inf, loaded) }),
			layout.Flexed(1, d.bodyArea),
		)
	}()
	return D{Size: full}
}

// head draws the header at Slack's absolute positions: channels have the
// title at y 20, buttons at y 62 and tabs at y 98 (h 130); 1:1 DMs have a
// 72px avatar at (28, 28), the name at x 116, buttons at y 120 and tabs at
// y 156 (h 188).
func (d *ChannelDetails) head(gtx C, inf core.ChannelDetails, loaded bool) D {
	w := gtx.Constraints.Max.X
	cv := d.conv(inf, loaded)
	dm := cdGroup(d.kind) == "im"
	h := gtx.Dp(cdHeadH)
	btnY, tabY := gtx.Dp(62), gtx.Dp(98)
	if dm {
		h, btnY, tabY = gtx.Dp(cdHeadDMH), gtx.Dp(120), gtx.Dp(156)
		uid := d.dmUser(inf)
		u, ok := d.user(uid)
		if !ok {
			u = core.User{ID: uid, Name: d.userName(uid)}
		}
		dirAt(gtx, image.Pt(gtx.Dp(28), gtx.Dp(28)), func(gtx C) D {
			s := gtx.Dp(72)
			return dirImage(&d.base, gtx, dirFirst(u.Avatar192, u.Avatar512, u.Avatar), image.Pt(s, s), gtx.Dp(12))
		})
		dirAt(gtx, image.Pt(gtx.Dp(116), gtx.Dp(28)), func(gtx C) D {
			gtx.Constraints.Max.X = max(0, w-gtx.Dp(116+64))
			return col(gtx,
				layout.Rigid(func(gtx C) D { return dirNameMarks(&d.base, gtx, u, displayName(u), 22, wBlack, 1) }),
				layout.Rigid(func(gtx C) D {
					sub := dirJoin(" · ", u.Pronouns, u.Title)
					if sub == "" {
						return D{}
					}
					return d.para(gtx, 13, 18, wReg, d.p.Muted, 1, sub)
				}),
			)
		})
	} else {
		title, ic := d.title(cv)
		dirAt(gtx, image.Pt(gtx.Dp(cdPadX), gtx.Dp(20)), func(gtx C) D {
			gtx.Constraints.Max.X = max(0, w-gtx.Dp(cdPadX+64))
			return row(gtx, layout.Middle,
				layout.Rigid(func(gtx C) D {
					if ic == "" {
						return D{}
					}
					return layout.Inset{Right: 2}.Layout(gtx, func(gtx C) D { return d.icon(gtx, ic, 22, d.p.Text) })
				}),
				layout.Rigid(func(gtx C) D { return d.para(gtx, 22, 30, wBlack, d.p.Text, 1, title) }),
			)
		})
	}
	dirAt(gtx, image.Pt(w-gtx.Dp(56), gtx.Dp(20)), func(gtx C) D {
		return d.iconButton(gtx, &d.closeBtn, "close", 36, 20, d.p.Sec)
	})
	dirAt(gtx, image.Pt(gtx.Dp(cdPadX), btnY), func(gtx C) D {
		ch := []layout.FlexChild{
			layout.Rigid(func(gtx C) D { return cdBtn(&d.base, gtx, &d.star, "star", "", true, 56) }),
			hgap(8),
		}
		if dm {
			ch = append(ch,
				layout.Rigid(func(gtx C) D { return cdBtn(&d.base, gtx, &d.notif, "bell", "Mute", false, 0) }),
				hgap(8),
				layout.Rigid(func(gtx C) D { return cdBtn(&d.base, gtx, &d.vip, "", "VIP", false, 0) }),
				hgap(8),
				layout.Rigid(func(gtx C) D { return cdBtn(&d.base, gtx, &d.hide, "", "Hide", false, 0) }),
				hgap(8))
		} else {
			label := "Just mentions"
			if cv.Muted {
				label = "Mute"
			}
			ch = append(ch,
				layout.Rigid(func(gtx C) D { return cdBtn(&d.base, gtx, &d.notif, "bell", label, true, 0) }),
				hgap(8))
		}
		ch = append(ch, layout.Rigid(func(gtx C) D { return cdBtn(&d.base, gtx, &d.call, "phone", "Start a Call", true, 0) }))
		return row(gtx, layout.Middle, ch...)
	})
	dirAt(gtx, image.Pt(gtx.Dp(cdPadX-8), tabY), func(gtx C) D {
		counts := make([]string, len(d.tabKinds))
		for i, k := range d.tabKinds {
			if k != cdMembers {
				continue
			}
			n := cv.NumMembers
			if n == 0 {
				n = len(cv.Members)
			}
			if n > 0 {
				counts[i] = humanCount(n)
			}
		}
		return dirTabStrip(&d.base, gtx, d.tab, nil, counts)
	})
	return D{Size: image.Pt(w, h)}
}

// title returns the channel/group title and its leading glyph.
func (d *ChannelDetails) title(cv core.Conv) (string, string) {
	if cdGroup(d.kind) == "mpim" {
		if len(cv.Members) > 0 {
			return d.groupNames(cv.Members), ""
		}
		if cv.Name != "" {
			return mpimName(cv.Name), ""
		}
		return d.convTitle(d.id, nil), ""
	}
	name := cv.Name
	if name == "" {
		name = d.h.Core().ConvRef(d.id).Name
	}
	if name == "" {
		name = d.id
	}
	if d.kind == "private" {
		return name, "lock"
	}
	return name, "hash"
}

// cdBtn is a header button: outline small, h 28, radius 4, 1px
// #5e5d60@.45, 13px bold, optional 16px icon and caret; width > 0 fixes
// the width (the 56px star split).
func cdBtn(b *base, gtx C, c *widget.Clickable, icon, label string, caret bool, width unit.Dp) D {
	return c.Layout(gtx, func(gtx C) D {
		pointerCursor(gtx)
		h, is := gtx.Dp(28), gtx.Dp(16)
		padL, padR := gtx.Dp(12), gtx.Dp(12)
		if icon != "" {
			padL = gtx.Dp(8)
		}
		if caret {
			padR = gtx.Dp(4)
		}
		tw := 0
		if label != "" {
			tw = b.th.Measure(gtx, 13, wBold, label)
		}
		w := padL + tw + padR
		if icon != "" {
			w += is
			if label != "" {
				w += gtx.Dp(4)
			}
		}
		if caret {
			w += gtx.Dp(2) + is
		}
		if width > 0 {
			w = gtx.Dp(width)
		}
		r := image.Rect(0, 0, w, h)
		rad := gtx.Dp(4)
		rrect(gtx, r, rad, b.p.Surface)
		if c.Hovered() {
			rrect(gtx, r, rad, b.p.Hover)
		}
		border(gtx, r, rad, max(1, gtx.Dp(1)), b.p.BorderStr)
		x := padL
		if icon != "" {
			dirAt(gtx, image.Pt(x, (h-is)/2), func(gtx C) D { return b.icon(gtx, icon, 16, b.p.Sec) })
			x += is
			if label != "" {
				x += gtx.Dp(4)
			}
		}
		if label != "" {
			dirAt(gtx, image.Pt(x, 0), func(gtx C) D {
				gtx.Constraints = layout.Exact(image.Pt(tw, h))
				return layout.W.Layout(gtx, func(gtx C) D { return b.label(gtx, 13, wBold, b.p.Text, label) })
			})
		}
		if caret {
			dirAt(gtx, image.Pt(w-padR-is, (h-is)/2), func(gtx C) D { return b.icon(gtx, "caret-down", 16, b.p.Sec) })
		}
		return D{Size: r.Max}
	})
}

func (d *ChannelDetails) bodyArea(gtx C) D {
	k := d.tabKinds[min(d.tab.Sel, len(d.tabKinds)-1)]
	bg := d.p.ReadFill
	if k == cdMembers {
		bg = d.p.Surface
	}
	fill(gtx, image.Rectangle{Max: gtx.Constraints.Max}, bg)
	inf, loaded, err := d.info.get()
	if !loaded {
		return layout.Inset{Top: 12, Left: cdPadX - 20, Right: cdPadX - 20}.Layout(gtx, func(gtx C) D { return d.skeleton(gtx, 4, 36) })
	}
	if err != nil {
		if d.retry.Clicked(gtx) {
			d.loadInfo()
		}
		return d.errorState(gtx, err, &d.retry)
	}
	var items []W
	switch k {
	case cdMembers:
		return d.membersTab(gtx)
	case cdAbout:
		items = d.aboutItems(inf)
	case cdApps:
		items = d.sectionItems("Apps", "Bring the tools you need into this channel to pull reports, start calls, file tickets and more.", "Add an App", &d.addApp)
	case cdAutomations:
		items = d.sectionItems("Workflows", "Automate routine tasks in this channel with workflows.", "Add a Workflow", &d.addWorkflow)
	case cdTabs:
		items = d.tabsItems()
	case cdSettings:
		items = d.settingsItems()
	}
	return d.scrollList(gtx, d.list(k), len(items), func(gtx C, i int) D {
		gtx.Constraints.Min.X = gtx.Constraints.Max.X
		return items[i](gtx)
	})
}

func cdGap(v unit.Dp) W {
	return func(gtx C) D { return D{Size: image.Pt(0, gtx.Dp(v))} }
}

// cdCard wraps rows in a field-group card inset 28px each side.
func (d *ChannelDetails) cdCard(rows ...W) W {
	return func(gtx C) D {
		return layout.Inset{Left: cdPadX, Right: cdPadX}.Layout(gtx, func(gtx C) D { return cdBox(&d.base, gtx, rows...) })
	}
}

// cdBox is Slack's field group: white, radius 12, 1px #1d1c1d@0.13
// border, rows separated by hairlines.
func cdBox(b *base, gtx C, rows ...W) D {
	gtx.Constraints.Min.X = gtx.Constraints.Max.X
	gtx.Constraints.Min.Y = 0
	rad := gtx.Dp(12)
	line := argb(0x211d1c1d)
	if b.th.Dark {
		line = b.p.Border
	}
	ch := make([]layout.FlexChild, 0, 2*len(rows))
	for i, r := range rows {
		if i > 0 {
			ch = append(ch, layout.Rigid(func(gtx C) D { return hline(gtx, line) }))
		}
		ch = append(ch, layout.Rigid(func(gtx C) D {
			gtx.Constraints.Min.X = gtx.Constraints.Max.X
			return r(gtx)
		}))
	}
	m := op.Record(gtx.Ops)
	dims := layout.Flex{Axis: layout.Vertical}.Layout(gtx, ch...)
	call := m.Stop()
	rect := image.Rectangle{Max: dims.Size}
	rrect(gtx, rect, rad, b.p.Surface)
	func() {
		defer clip.UniformRRect(rect, rad).Push(gtx.Ops).Pop()
		call.Add(gtx.Ops)
	}()
	border(gtx, rect, rad, max(1, gtx.Dp(1)), line)
	return dims
}

// cdField is a field row: padding 16px 20px, 15/22 bold label, an
// optional "Edit" (13/18 bold #1264a3) at the right, the value below.
func (d *ChannelDetails) cdField(gtx C, label string, edit *widget.Clickable, value W) D {
	return layout.Inset{Top: 16, Bottom: 16, Left: 20, Right: 20}.Layout(gtx, func(gtx C) D {
		gtx.Constraints.Min.X = gtx.Constraints.Max.X
		return col(gtx,
			layout.Rigid(func(gtx C) D {
				return row(gtx, layout.Start,
					layout.Flexed(1, func(gtx C) D {
						return dirFill(gtx, func(gtx C) D { return d.para(gtx, 15, 22, wBold, d.p.Text, 1, label) })
					}),
					layout.Rigid(func(gtx C) D {
						if edit == nil {
							return D{}
						}
						return cdLink(&d.base, gtx, edit, 13, "Edit")
					}),
				)
			}),
			layout.Rigid(func(gtx C) D {
				if value == nil {
					return D{}
				}
				return dirFill(gtx, value)
			}),
		)
	})
}

// cdLink is a bold link button (underlined on hover).
func cdLink(b *base, gtx C, c *widget.Clickable, size unit.Sp, label string) D {
	return c.Layout(gtx, func(gtx C) D {
		pointerCursor(gtx)
		d := b.label(gtx, size, wBold, b.p.Link, label)
		if c.Hovered() {
			fill(gtx, image.Rect(0, d.Size.Y-gtx.Dp(2), d.Size.X, d.Size.Y-gtx.Dp(1)), b.p.Link)
		}
		return d
	})
}

// cdButtonField is a clickable field row (hover #f8f8f8).
func (d *ChannelDetails) cdButtonField(gtx C, c *widget.Clickable, w W) D {
	return c.Layout(gtx, func(gtx C) D {
		pointerCursor(gtx)
		m := op.Record(gtx.Ops)
		dims := w(gtx)
		call := m.Stop()
		if c.Hovered() {
			fill(gtx, image.Rectangle{Max: dims.Size}, d.p.ReadFill)
		}
		call.Add(gtx.Ops)
		return dims
	})
}

// ---- About ----

func (d *ChannelDetails) aboutItems(inf core.ChannelDetails) []W {
	cv := inf.Conv
	topic := func(gtx C) D { return d.topicField(gtx, cv) }
	var items []W
	switch cdGroup(d.kind) {
	case "im":
		items = []W{cdGap(16), d.cdCard(topic), cdGap(16), d.cdCard(func(gtx C) D { return d.localTimeField(gtx, inf) })}
		items = append(items, cdGap(16), d.cdCard(d.addPeopleField), cdGap(16), d.cdCard(d.emailsField))
	case "mpim":
		items = []W{cdGap(16), d.cdCard(topic), cdGap(16), d.cdCard(d.addPeopleField), cdGap(16), d.cdCard(d.emailsField)}
	default:
		rows := []W{topic, func(gtx C) D { return d.purposeField(gtx, cv) }}
		if cv.Creator != "" || cv.Created != 0 {
			rows = append(rows, func(gtx C) D { return d.createdField(gtx, cv) })
		}
		if !cv.IsArchived {
			rows = append(rows, func(gtx C) D { return d.leaveField(gtx, inf) })
		}
		items = []W{cdGap(16), d.cdCard(rows...), cdGap(16), d.cdCard(d.emailsField)}
	}
	return append(items, d.idLine)
}

func (d *ChannelDetails) topicField(gtx C, cv core.Conv) D {
	if d.editing {
		return d.cdField(gtx, "Topic", nil, d.topicEditor)
	}
	return d.cdField(gtx, "Topic", &d.editTopic, func(gtx C) D {
		return d.richText(gtx, "topic/"+d.id, cv.Topic, cv.TopicDoc, "Add a topic")
	})
}

func (d *ChannelDetails) purposeField(gtx C, cv core.Conv) D {
	return d.cdField(gtx, "Description", &d.editPurpose, func(gtx C) D {
		return d.richText(gtx, "purpose/"+d.id, cv.Purpose, cv.PurposeDoc, "Add a description")
	})
}

// richText draws a topic/description with richview (mentions and links
// clickable), plain text when there is no document, or the placeholder.
func (d *ChannelDetails) richText(gtx C, rk, raw string, blocks []richtext.Block, empty string) D {
	switch {
	case strings.TrimSpace(raw) == "":
		return d.para(gtx, 15, 22, wReg, d.p.Muted, 0, empty)
	case len(blocks) > 0:
		return d.richDoc(gtx, rk, raw, richtext.Doc{Blocks: blocks}, richview.Options{}, 0)
	}
	return d.para(gtx, 15, 22, wReg, d.p.Text, 0, raw)
}

// createdField: "Created by" / "<name> on May 17, 2026", the name a link;
// the row opens the creator's profile.
func (d *ChannelDetails) createdField(gtx C, cv core.Conv) D {
	name := ""
	if cv.Creator != "" {
		name = d.userName(cv.Creator)
	}
	date := ""
	if t := unixTime(cv.Created); !t.IsZero() {
		date = t.Format("January 2, 2006")
	}
	return d.cdButtonField(gtx, &d.createdBy, func(gtx C) D {
		return d.cdField(gtx, "Created by", nil, func(gtx C) D {
			return row(gtx, layout.Baseline,
				layout.Rigid(func(gtx C) D {
					if name == "" {
						return D{}
					}
					return d.para(gtx, 15, 22, wReg, d.p.Link, 1, name)
				}),
				layout.Flexed(1, func(gtx C) D {
					s := date
					if name != "" && date != "" {
						s = " on " + date
					}
					return dirFill(gtx, func(gtx C) D { return d.para(gtx, 15, 22, wReg, d.p.Text, 1, s) })
				}),
			)
		})
	})
}

// leaveField is the red "Leave channel" row (h 55), its inline
// confirmation, or "Join channel" when the viewer isn't a member.
func (d *ChannelDetails) leaveField(gtx C, inf core.ChannelDetails) D {
	cv := inf.Conv
	if !inf.IsMember {
		label := "Join channel"
		if d.joinPending {
			label = "Joining…"
		}
		return d.cdButtonField(gtx, &d.joinRow, func(gtx C) D { return d.cdField(gtx, label, nil, nil) })
	}
	if !d.confirmLeave {
		return d.cdButtonField(gtx, &d.leaveRow, func(gtx C) D {
			return layout.Inset{Top: 16, Bottom: 16, Left: 20, Right: 20}.Layout(gtx, func(gtx C) D {
				return dirFill(gtx, func(gtx C) D { return d.para(gtx, 15, 22, wBold, d.p.Red, 1, "Leave channel") })
			})
		})
	}
	note := "You can rejoin at any time."
	if cv.Kind == "private" {
		note = "You won’t be able to rejoin unless someone invites you."
	}
	yes := "Leave"
	if d.leavePending {
		yes = "Leaving…"
	}
	return layout.Inset{Top: 16, Bottom: 16, Left: 20, Right: 20}.Layout(gtx, func(gtx C) D {
		return row(gtx, layout.Middle,
			layout.Flexed(1, func(gtx C) D {
				return dirFill(gtx, func(gtx C) D {
					return col(gtx,
						layout.Rigid(func(gtx C) D { return d.para(gtx, 15, 22, wBold, d.p.Text, 1, "Leave #"+cv.Name+"?") }),
						layout.Rigid(func(gtx C) D { return d.para(gtx, 13, 18, wReg, d.p.Muted, 2, note) }),
					)
				})
			}),
			hgap(12),
			layout.Rigid(func(gtx C) D { return d.button(gtx, &d.leaveNo, btnOutline, true, "Cancel") }),
			hgap(8),
			layout.Rigid(func(gtx C) D { return d.button(gtx, &d.leaveYes, btnDanger, true, yes) }),
		)
	})
}

// topicEditor is the inline topic input with Cancel / Save.
func (d *ChannelDetails) topicEditor(gtx C) D {
	return col(gtx,
		vgap(6),
		layout.Rigid(func(gtx C) D {
			w, h := gtx.Constraints.Max.X, gtx.Dp(40)
			r := image.Rect(0, 0, w, h)
			rad := gtx.Dp(8)
			if gtx.Focused(&d.topicEd) {
				ring := gtx.Dp(5)
				rrect(gtx, r.Inset(-ring), rad+ring, theme.WithAlpha(d.p.Link, 0x4d))
				one := max(1, gtx.Dp(1))
				rrect(gtx, r.Inset(-one), rad+one, d.p.Link)
			}
			rrect(gtx, r, rad, d.p.Surface)
			border(gtx, r, rad, max(1, gtx.Dp(1)), d.p.Faint)
			g := gtx
			g.Constraints = layout.Exact(image.Pt(w, h))
			layout.Inset{Left: 10, Right: 10}.Layout(g, func(gtx C) D {
				return layout.W.Layout(gtx, func(gtx C) D {
					gtx.Constraints.Min.X = gtx.Constraints.Max.X
					return d.th.Editor(gtx, &d.topicEd, 15, d.p.Text, d.p.Mention)
				})
			})
			return D{Size: image.Pt(w, h)}
		}),
		vgap(10),
		layout.Rigid(func(gtx C) D {
			gtx.Constraints.Min.X = gtx.Constraints.Max.X
			return layout.E.Layout(gtx, func(gtx C) D {
				return row(gtx, layout.Middle,
					layout.Rigid(func(gtx C) D { return d.button(gtx, &d.cancelTop, btnOutline, true, "Cancel") }),
					hgap(8),
					layout.Rigid(func(gtx C) D { return d.button(gtx, &d.saveTopic, btnPrimary, true, "Save") }),
				)
			})
		}),
	)
}

// localTimeField (DMs): 20px clock + "<time> local time", then "View full
// profile" 12px below.
func (d *ChannelDetails) localTimeField(gtx C, inf core.ChannelDetails) D {
	u, _ := d.user(d.dmUser(inf))
	return layout.Inset{Top: 16, Bottom: 16, Left: 20, Right: 20}.Layout(gtx, func(gtx C) D {
		return col(gtx,
			layout.Rigid(func(gtx C) D {
				if u.TZ == "" && u.TZOffset == 0 {
					return D{}
				}
				now := gtx.Now
				if now.IsZero() {
					now = time.Now()
				}
				local := now.In(time.FixedZone(u.TZLabel, u.TZOffset))
				gtx.Execute(op.InvalidateCmd{At: now.Truncate(time.Minute).Add(time.Minute)})
				return layout.Inset{Bottom: 12}.Layout(gtx, func(gtx C) D {
					return row(gtx, layout.Middle,
						layout.Rigid(func(gtx C) D { return d.icon(gtx, "clock", 20, d.p.Sec) }),
						hgap(8),
						layout.Rigid(func(gtx C) D { return d.label(gtx, 15, wReg, d.p.Text, clock(local)+" local time") }),
					)
				})
			}),
			layout.Rigid(func(gtx C) D { return cdLink(&d.base, gtx, &d.viewProfile, 15, "View full profile") }),
		)
	})
}

// cdTileRow draws a 36px icon tile (#1d9bd1@.1, radius 4, #1264a3 icon)
// and text starting at x+48.
func (d *ChannelDetails) cdTileRow(gtx C, icon string, text W) D {
	return row(gtx, layout.Middle,
		layout.Rigid(func(gtx C) D {
			s := gtx.Dp(36)
			rrect(gtx, image.Rect(0, 0, s, s), gtx.Dp(4), cdTileBg)
			g := gtx
			g.Constraints = layout.Exact(image.Pt(s, s))
			layout.Center.Layout(g, func(gtx C) D { return d.icon(gtx, icon, 20, d.p.Link) })
			return D{Size: image.Pt(s, s)}
		}),
		hgap(12),
		layout.Flexed(1, func(gtx C) D { return dirFill(gtx, text) }),
	)
}

func (d *ChannelDetails) emailsField(gtx C) D {
	what := "channel"
	if cdGroup(d.kind) != "channel" {
		what = "conversation"
	}
	return d.cdButtonField(gtx, &d.emails, func(gtx C) D {
		return layout.Inset{Top: 16, Bottom: 16, Left: 20, Right: 20}.Layout(gtx, func(gtx C) D {
			return d.cdTileRow(gtx, "mail", func(gtx C) D {
				return col(gtx,
					layout.Rigid(func(gtx C) D { return d.para(gtx, 15, 22, wBold, d.p.Text, 1, "Send emails to this "+what) }),
					layout.Rigid(func(gtx C) D {
						return d.para(gtx, 15, 22, wReg, d.p.Text, 0, "Get an email address that posts incoming emails in this "+what+".")
					}),
				)
			})
		})
	})
}

// addPeopleField (DMs): 20px person-plus icon + 15px bold label, h ~56.
func (d *ChannelDetails) addPeopleField(gtx C) D {
	return d.cdButtonField(gtx, &d.addPeople, func(gtx C) D {
		return layout.Inset{Top: 17, Bottom: 17, Left: 20, Right: 20}.Layout(gtx, func(gtx C) D {
			return dirFill(gtx, func(gtx C) D {
				return row(gtx, layout.Middle,
					layout.Rigid(func(gtx C) D { return d.icon(gtx, "user-plus", 20, d.p.Text) }),
					hgap(8),
					layout.Rigid(func(gtx C) D { return d.label(gtx, 15, wBold, d.p.Text, "Add people to this conversation") }),
				)
			})
		})
	})
}

// idLine: "Channel ID: <id>" 13/18 muted, margin 20px 28px 20px 48px,
// with a 13px copy icon 4px after it.
func (d *ChannelDetails) idLine(gtx C) D {
	return layout.Inset{Top: 20, Bottom: 20, Left: 48, Right: cdPadX}.Layout(gtx, func(gtx C) D {
		return row(gtx, layout.Middle,
			layout.Rigid(func(gtx C) D { return d.label(gtx, 13, wReg, d.p.Muted, "Channel ID: "+d.id) }),
			hgap(4),
			layout.Rigid(func(gtx C) D {
				return d.copyID.Layout(gtx, func(gtx C) D {
					pointerCursor(gtx)
					fg := d.p.Muted
					if d.copyID.Hovered() {
						fg = d.p.Text
					}
					return d.icon(gtx, "copy", 13, fg)
				})
			}),
		)
	})
}

// ---- Members ----

func (d *ChannelDetails) membersTab(gtx C) D {
	if !d.members.started() {
		d.loadMembers("")
	}
	q, _, _ := d.find.update(gtx)
	mp, loaded, err := d.members.get()
	busy := d.members.busy()
	users := d.filtered(mp.users, q, d.memFilter.sel)
	if d.moreMembers.Clicked(gtx) && mp.next != "" && !busy {
		d.loadMembers(mp.next)
		busy = true
	}
	filtering := q != "" || d.memFilter.sel != 0
	n := 1 + len(users) + 1 // "Add people or agents", members, footer
	l := d.list(cdMembers)
	if !filtering && loaded && err == nil && mp.next != "" && !busy {
		if p := l.Position; p.First+p.Count >= n-5 {
			d.loadMembers(mp.next)
			busy = true
		}
	}
	self := d.selfID()
	return col(gtx,
		// Sticky search row: padding 16px 28px 16px; a hairline once the
		// list scrolls.
		layout.Rigid(func(gtx C) D {
			dims := layout.Inset{Top: 16, Bottom: 16, Left: cdPadX, Right: cdPadX}.Layout(gtx, func(gtx C) D {
				return row(gtx, layout.Middle,
					layout.Rigid(func(gtx C) D {
						gtx.Constraints.Max.X = min(gtx.Dp(316), max(0, gtx.Constraints.Max.X-gtx.Dp(208)))
						gtx.Constraints.Min.X = gtx.Constraints.Max.X
						return dirSearchField(&d.base, gtx, d.find, dirFieldStyle{H: 36, Icon: 16, PadL: 8, Gap: 8, Border: d.p.Faint}, nil)
					}),
					hgap(8),
					layout.Rigid(func(gtx C) D { return d.memFilter.layoutSelect(&d.base, gtx, 200) }),
				)
			})
			if p := l.Position; p.First > 0 || p.Offset > 0 {
				fill(gtx, image.Rect(0, dims.Size.Y-max(1, gtx.Dp(1)), gtx.Constraints.Max.X, dims.Size.Y), argb(0x211d1c1d))
			}
			return dims
		}),
		layout.Flexed(1, func(gtx C) D {
			return d.scrollList(gtx, l, n, func(gtx C, i int) D {
				gtx.Constraints.Min.X = gtx.Constraints.Max.X
				switch {
				case i == 0:
					return d.addMembersRow(gtx)
				case i <= len(users):
					return d.memberRow(gtx, users[i-1], self)
				}
				return d.membersFooter(gtx, q, filtering, loaded, err, busy, mp.next, len(users))
			})
		}),
	)
}

// filtered matches loaded members by name, real name, handle or title and
// the All / People / Agents select, cached until any input changes.
func (d *ChannelDetails) filtered(us []core.User, q string, sel int) []core.User {
	if q == "" && sel == 0 {
		return us
	}
	k := q + "\x00" + string(rune('0'+sel)) + "\x00" + humanCount(len(us))
	if k == d.filtKey && d.filt != nil {
		return d.filt
	}
	lq := strings.ToLower(q)
	out := make([]core.User, 0, 16)
	for _, u := range us {
		if (sel == 1 && u.IsBot) || (sel == 2 && !u.IsBot) {
			continue
		}
		if lq != "" && !strings.Contains(strings.ToLower(u.Name), lq) && !strings.Contains(strings.ToLower(u.RealName), lq) &&
			!strings.Contains(strings.ToLower(u.Handle), lq) && !strings.Contains(strings.ToLower(u.Title), lq) {
			continue
		}
		out = append(out, u)
	}
	d.filtKey, d.filt = k, out
	return out
}

// addMembersRow: h 60, padding 12px 28px; 36px tile then a bold label.
func (d *ChannelDetails) addMembersRow(gtx C) D {
	return d.hoverable(gtx, &d.addPeopleRow, false, 0, func(gtx C) D {
		return layout.Inset{Top: 12, Bottom: 12, Left: cdPadX, Right: cdPadX}.Layout(gtx, func(gtx C) D {
			return d.cdTileRow(gtx, "user-plus", func(gtx C) D { return d.label(gtx, 15, wBold, d.p.Text, "Add people or agents") })
		})
	})
}

// memberRow: h 60, padding 12px 28px; 36px avatar, 12px gap; line 1 the
// bold full name (+ " (you)"), presence, display name and status emoji;
// line 2 the title in #454447. The text block overhangs the avatar by 4px.
func (d *ChannelDetails) memberRow(gtx C, u core.User, self string) D {
	c := dirClick(d.memberClk, u.ID)
	if c.Clicked(gtx) {
		d.h.OpenProfile(d.press.at, u.ID)
	}
	full := dirFirst(u.RealName, displayName(u))
	if u.ID == self {
		full += " (you)"
	}
	disp := ""
	if u.Name != "" && u.Name != u.RealName {
		disp = u.Name
	}
	return d.hoverable(gtx, c, false, 0, func(gtx C) D {
		return layout.Inset{Top: 12, Bottom: 12, Left: cdPadX, Right: cdPadX}.Layout(gtx, func(gtx C) D {
			return row(gtx, layout.Middle,
				layout.Rigid(func(gtx C) D { return d.avatar(gtx, u.Avatar, 36) }),
				hgap(12),
				layout.Flexed(1, func(gtx C) D {
					return dirFill(gtx, func(gtx C) D {
						return layout.Inset{Top: -4, Bottom: -4}.Layout(gtx, func(gtx C) D {
							return col(gtx,
								layout.Rigid(func(gtx C) D {
									return row(gtx, layout.Middle,
										layout.Rigid(func(gtx C) D { return d.para(gtx, 15, 22, wBold, d.p.Text, 1, full) }),
										layout.Rigid(func(gtx C) D {
											if u.Presence == "" {
												return D{}
											}
											return dirPresence(&d.base, gtx, u.Presence)
										}),
										layout.Rigid(func(gtx C) D {
											if disp == "" {
												return D{}
											}
											return layout.Inset{Left: 4}.Layout(gtx, func(gtx C) D { return d.para(gtx, 15, 22, wReg, d.p.Text, 1, disp) })
										}),
										layout.Rigid(func(gtx C) D {
											if u.StatusEmojiURL == "" {
												return D{}
											}
											return layout.Inset{Left: 4}.Layout(gtx, func(gtx C) D { return d.emojiImg(gtx, u.StatusEmojiURL, 16) })
										}),
									)
								}),
								layout.Rigid(func(gtx C) D {
									if u.Title == "" {
										return D{}
									}
									return d.para(gtx, 15, 22, wReg, d.p.Sec, 1, u.Title)
								}),
							)
						})
					})
				}),
			)
		})
	})
}

func (d *ChannelDetails) membersFooter(gtx C, q string, filtering, loaded bool, err error, busy bool, next string, shown int) D {
	msg := func(s string) D {
		return layout.Inset{Top: 12, Bottom: 16, Left: cdPadX, Right: cdPadX}.Layout(gtx, func(gtx C) D {
			return d.centered(gtx, 13, wReg, d.p.Muted, s)
		})
	}
	switch {
	case !loaded:
		return layout.Inset{Left: cdPadX - 20, Right: cdPadX - 20}.Layout(gtx, func(gtx C) D { return d.skeleton(gtx, 5, 36) })
	case busy:
		return msg("Loading members…")
	case err != nil && shown == 0:
		return msg("Couldn’t load members: " + friendlyErr(err))
	case next != "" && (filtering || err != nil):
		return layout.Inset{Top: 8, Bottom: 16, Left: cdPadX, Right: cdPadX}.Layout(gtx, func(gtx C) D {
			return d.button(gtx, &d.moreMembers, btnOutline, true, "Load more members")
		})
	case filtering && shown == 0 && q != "":
		return msg("No one matches “" + q + "”.")
	case filtering && shown == 0:
		return msg("No one matches this filter.")
	}
	return D{Size: image.Pt(0, gtx.Dp(16))}
}

// ---- Tabs ----

type cdTabRow struct{ icon, label, note string }

var cdTabRows = []cdTabRow{
	{"dms", "Messages", "Always shown"},
	{"files", "Files", ""},
	{"pin", "Pins", ""},
	{"bookmark", "Bookmarks", ""},
	{"canvas", "Canvas", ""},
	{"workflow", "Workflows", ""},
}

func (d *ChannelDetails) tabsItems() []W {
	if !d.bookmarks.started() {
		d.loadBookmarks()
	}
	rows := make([]W, len(cdTabRows))
	for i, t := range cdTabRows {
		rows[i] = func(gtx C) D {
			return layout.Inset{Top: 16, Bottom: 16, Left: 20, Right: 20}.Layout(gtx, func(gtx C) D {
				return row(gtx, layout.Middle,
					layout.Rigid(func(gtx C) D { return d.icon(gtx, t.icon, 20, d.p.Sec) }),
					hgap(8),
					layout.Flexed(1, func(gtx C) D {
						return dirFill(gtx, func(gtx C) D { return d.label(gtx, 15, wBold, d.p.Text, t.label) })
					}),
					layout.Rigid(func(gtx C) D {
						if t.note == "" {
							return D{}
						}
						return d.label(gtx, 13, wReg, d.p.Muted, t.note)
					}),
				)
			})
		}
	}
	items := []W{cdGap(16), d.cdCard(rows...), cdGap(16)}
	bms, loaded, err := d.bookmarks.get()
	note := func(s string) W {
		return func(gtx C) D {
			return layout.Inset{Left: cdPadX + 20, Right: cdPadX}.Layout(gtx, func(gtx C) D { return d.label(gtx, 13, wReg, d.p.Muted, s) })
		}
	}
	switch {
	case !loaded:
		items = append(items, func(gtx C) D { return d.skeleton(gtx, 2, 20) })
	case err != nil:
		items = append(items, note("Couldn’t load bookmarks: "+friendlyErr(err)))
	case len(bms) == 0:
		items = append(items, note("No bookmarks yet."))
	default:
		bm := make([]W, 0, len(bms)+1)
		bm = append(bm, func(gtx C) D { return d.cdField(gtx, "Bookmarks", nil, nil) })
		for i := range bms {
			b := bms[i]
			bm = append(bm, func(gtx C) D { return d.bookmarkRow(gtx, b) })
		}
		items = append(items, d.cdCard(bm...))
	}
	return append(items, cdGap(24))
}

func (d *ChannelDetails) bookmarkRow(gtx C, b core.Bookmark) D {
	c := dirClick(d.rowClk, "bm:"+b.ID)
	if c.Clicked(gtx) && b.Link != "" {
		cdCopy(gtx, b.Link)
		d.h.Toast("Link copied to clipboard")
	}
	title := dirFirst(b.Title, b.Link)
	host := cdHost(b.Link)
	return d.cdButtonField(gtx, c, func(gtx C) D {
		return layout.Inset{Top: 12, Bottom: 12, Left: 20, Right: 20}.Layout(gtx, func(gtx C) D {
			return dirFill(gtx, func(gtx C) D {
				return row(gtx, layout.Middle,
					layout.Rigid(func(gtx C) D {
						if b.EmojiURL != "" {
							return d.emojiImg(gtx, b.EmojiURL, 20)
						}
						if b.Icon != "" {
							s := gtx.Dp(20)
							return dirImage(&d.base, gtx, b.Icon, image.Pt(s, s), gtx.Dp(4))
						}
						return d.icon(gtx, "link", 20, d.p.Sec)
					}),
					hgap(8),
					layout.Rigid(func(gtx C) D { return d.label(gtx, 15, wReg, d.p.Text, title) }),
					layout.Flexed(1, func(gtx C) D {
						return dirFill(gtx, func(gtx C) D {
							if host == "" || host == title {
								return D{}
							}
							return layout.Inset{Left: 8}.Layout(gtx, func(gtx C) D { return d.label(gtx, 13, wReg, d.p.Muted, host) })
						})
					}),
				)
			})
		})
	})
}

func cdHost(link string) string {
	u, err := url.Parse(link)
	if err != nil {
		return ""
	}
	return strings.TrimPrefix(u.Host, "www.")
}

// ---- Agents & apps / Automations ----

func (d *ChannelDetails) sectionItems(title, body, btn string, c *widget.Clickable) []W {
	return []W{
		cdGap(16),
		d.cdCard(func(gtx C) D {
			return d.cdField(gtx, title, nil, func(gtx C) D {
				return col(gtx,
					layout.Rigid(func(gtx C) D { return d.para(gtx, 15, 22, wReg, d.p.Text, 0, body) }),
					vgap(12),
					layout.Rigid(func(gtx C) D { return d.button(gtx, c, btnOutline, true, btn) }),
				)
			})
		}),
		cdGap(24),
	}
}

// ---- Settings ----

func (d *ChannelDetails) settingsItems() []W {
	setting := func(title, desc string) W {
		return func(gtx C) D {
			c := dirClick(d.rowClk, "set:"+title)
			if c.Clicked(gtx) {
				d.h.Toast(cdUnsupported)
			}
			return d.cdButtonField(gtx, c, func(gtx C) D {
				return layout.Inset{Top: 16, Bottom: 16, Left: 20, Right: 20}.Layout(gtx, func(gtx C) D {
					return row(gtx, layout.Middle,
						layout.Flexed(1, func(gtx C) D {
							return dirFill(gtx, func(gtx C) D {
								return col(gtx,
									layout.Rigid(func(gtx C) D { return d.para(gtx, 15, 22, wBold, d.p.Text, 1, title) }),
									layout.Rigid(func(gtx C) D { return d.para(gtx, 15, 22, wReg, d.p.Text, 2, desc) }),
								)
							})
						}),
						layout.Rigid(func(gtx C) D { return d.icon(gtx, "chevron-right", 16, d.p.Sec) }),
					)
				})
			})
		}
	}
	if cdGroup(d.kind) != "channel" {
		rows := []W{setting("Notifications", "Choose how you’re notified about this conversation.")}
		if cdGroup(d.kind) == "mpim" {
			rows = append(rows, setting("Convert to a private channel", "Keep the history and add more people."))
		}
		return []W{cdGap(16), d.cdCard(rows...), cdGap(24)}
	}
	huddles := func(gtx C) D {
		return d.cdField(gtx, "Huddles", nil, func(gtx C) D {
			return col(gtx,
				layout.Rigid(func(gtx C) D {
					return d.para(gtx, 15, 22, wReg, d.p.Text, 0, "Members can start and join huddles in this channel.")
				}),
				vgap(12),
				layout.Rigid(func(gtx C) D {
					return row(gtx, layout.Middle,
						layout.Rigid(func(gtx C) D { return cdBtn(&d.base, gtx, &d.huddleStart, "headphones", "Start Huddle", false, 0) }),
						hgap(8),
						layout.Rigid(func(gtx C) D { return cdBtn(&d.base, gtx, &d.huddleLink, "link", "Copy Huddle Link", false, 0) }),
					)
				}),
			)
		})
	}
	tabsPerm := func(gtx C) D {
		return d.cdField(gtx, "Choose who can add, remove, and reorder tabs", nil, func(gtx C) D {
			return layout.Inset{Top: 8}.Layout(gtx, func(gtx C) D { return d.tabsPerm.layoutSelect(&d.base, gtx, 224) })
		})
	}
	return []W{
		cdGap(16),
		d.cdCard(huddles, tabsPerm, setting("Posting permissions", "Choose who can post in this channel.")),
		cdGap(24),
	}
}
