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
	"image/color"
	"io"
	"net/url"
	"sort"
	"strings"
	"sync/atomic"
	"time"

	"gioui.org/io/clipboard"
	"gioui.org/io/event"
	"gioui.org/io/key"
	"gioui.org/io/pointer"
	"gioui.org/layout"
	"gioui.org/op"
	"gioui.org/op/clip"
	"gioui.org/unit"
	"gioui.org/widget"

	"github.com/some-du6e/clack/internal/core"
	"github.com/some-du6e/clack/internal/richtext"
	"github.com/some-du6e/clack/internal/ui/hovercard"
	"github.com/some-du6e/clack/internal/ui/popups"
	"github.com/some-du6e/clack/internal/ui/richview"
	"github.com/some-du6e/clack/internal/ui/theme"
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
	prevConv                                core.Conv // before an optimistic edit (revert)
	pendKind                                int       // the edit kind in flight

	// Header writes: star and notification prefs (optimistic).
	starOp       async[bool]
	starPending  bool
	starOv       bool
	notifMenu    dirDropdown
	notifOp      async[bool]
	notifPending bool
	notifOv      core.ChannelNotify

	managers async[[]string]
	counts   async[core.MemberCounts]
	apps     async[[]core.ChannelApp] // Agents & apps (core.ChannelApps)
	tabList  async[[]core.Tab]

	// Add people (Members tab): an inline people search with Add buttons.
	inviting   bool
	inviteQ    *search
	inviteDeb  debounce
	inviteRes  async[[]core.User]
	inviteOp   async[bool]
	inviteUser string           // user being added
	dmOp       async[core.Conv] // AddToDM (DMs: opens the bigger group DM)
	added      map[string]bool
	editName   widget.Clickable

	tab      *tabs
	tabKinds []int
	lists    map[int]*widget.List

	// tabsAgentsApps is the channel_details_agents_apps_tab group the tabs
	// were built for (a plugin's override rebuilds them)
	tabsAgentsApps bool

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
	newTab                          widget.Clickable // Tabs: "New Tab"
	orgLearn                        widget.Clickable // "Workspaces with access": Learn more

	editing      bool
	editErr      string // the topic modal's error (Slack's getChannelTopicError), shown instead of the hint
	editScrim    widget.Clickable
	editDlg      widget.Clickable
	editClose    widget.Clickable
	editRing     bool // a focus reached by Tab shows the ring (editModal)
	editPtr      int
	editKind     int // cdEditTopic | cdEditPurpose | cdEditName
	focusTopic   int
	confirmLeave bool
	topicEd      widget.Editor

	find      *search
	memFilter dirDropdown
	tabsPerm  dirDropdown
	memberClk map[string]*widget.Clickable
	memberHov map[string]*hovercard.Trigger // Slack's ProfileHoverTrigger on each member's name
	filtKey   string
	filt      []core.User
	rowClk    map[string]*widget.Clickable

	// Realtime: conv (joined elsewhere, renamed, topic/purpose changed via
	// a refresh) and conv_removed (left elsewhere, archived) for this
	// conversation. refreshDue defers a reload while a write is in flight.
	evq        dirEvents
	refreshDue bool

	// presIDs are the loaded members (the watch goroutine redraws on their
	// presence_change); presN is how many were loaded when it was built.
	presIDs atomic.Pointer[map[string]bool]
	presN   int

	x cdExtra // details_more.go: Settings, stacked dialogs, VIP / Hide

	cl cdCall  // details_call.go: Start a Call ▾
	em cdEmail // details_email.go: Send emails to this channel
}

// NewChannelDetails opens the details dialog for a conversation.
func NewChannelDetails(h Host, channelID string) *ChannelDetails {
	d := &ChannelDetails{
		base:      newBase(h),
		id:        channelID,
		kind:      "channel",
		lists:     map[int]*widget.List{},
		memberClk: map[string]*widget.Clickable{},
		memberHov: map[string]*hovercard.Trigger{},
		rowClk:    map[string]*widget.Clickable{},
		find:      newSearch("Find people or agents"),
		memFilter: dirDropdown{labels: []string{"All", "People", "Agents"}},
		tabsPerm:  dirDropdown{labels: []string{"Everyone"}},
		topicEd:   widget.Editor{SingleLine: true, Submit: true, MaxLen: 250},
		inviteQ:   newSearch("Search by name or email"),
		added:     map[string]bool{},
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

// ShowTab selects the tab labelled label ("Settings": the channel menu's
// "Edit settings"); unknown labels keep About.
func (d *ChannelDetails) ShowTab(label string) {
	for i, k := range d.tabKinds {
		if cdTabLabels[k] == label {
			d.tab.Sel = i
			return
		}
	}
}

// EditTopic is the About tab's Topic "Edit": the "Edit topic" modal.
func (d *ChannelDetails) EditTopic() {
	if inf, ok, _ := d.info.get(); ok {
		d.startEdit(cdEditTopic, inf.Conv.Topic)
	}
}

// SetEditText replaces the open edit modal's text (tests).
func (d *ChannelDetails) SetEditText(s string) { d.topicEd.SetText(s) }

// EditText is the open edit modal's text.
func (d *ChannelDetails) EditText() string { return d.topicEd.Text() }

// SaveEdit is the edit modal's Save.
func (d *ChannelDetails) SaveEdit() {
	if d.editing {
		d.saveTopicNow()
	}
}

// Editing reports whether an edit modal is open; EditError is its error.
func (d *ChannelDetails) Editing() bool     { return d.editing }
func (d *ChannelDetails) EditError() string { return d.editErr }

// Tab is the selected tab's label.
func (d *ChannelDetails) Tab() string {
	return cdTabLabels[d.tabKinds[min(d.tab.Sel, len(d.tabKinds)-1)]]
}

// Closed reports whether the dialog was dismissed (views.Closer). It also
// drops the realtime subscription.
func (d *ChannelDetails) Closed() bool {
	if d.closed {
		d.evq.stop()
	}
	return d.closed
}

func cdGroup(kind string) string {
	if kind == "im" || kind == "mpim" {
		return kind
	}
	return "channel"
}

// setTabs is BaseAboutChannelModal's tab list (vWNA aZ). Under
// channel_details_agents_apps_tab (M/qt _2) a channel has "Agents &
// apps" and, right after it, "Automations" (IntegrationsSection);
// otherwise the agents tab is "Agents", shown only for Agentforce,
// channel experts or Slackbot in channels (aO: LxPK TI / pj, RW6b _u,
// AkZ1 PN: data Clack doesn't load, so it stays hidden), and the same
// IntegrationsSection is "Integrations" after Tabs.
func (d *ChannelDetails) setTabs() {
	var kinds []int
	agentsApps := d.h.Core().Experiment("channel_details_agents_apps_tab") == "on"
	switch cdGroup(d.kind) {
	case "im":
		kinds = []int{cdAbout, cdTabs}
	case "mpim":
		kinds = []int{cdAbout, cdMembers, cdTabs}
	default:
		if agentsApps {
			kinds = []int{cdAbout, cdMembers, cdApps, cdAutomations, cdTabs}
		} else {
			kinds = []int{cdAbout, cdMembers, cdTabs, cdAutomations}
		}
	}
	// BaseAboutChannelModal shows Settings when the settings sections
	// (jf5b) aren't empty, never to guests
	if d.settingsVisible() {
		kinds = append(kinds, cdSettings)
	}
	labels := make([]string, len(kinds))
	for i, k := range kinds {
		labels[i] = cdTabLabels[k]
		if k == cdAutomations && !agentsApps {
			labels[i] = "Integrations"
		}
	}
	d.tab = newTabs(labels...)
	d.tabKinds = kinds
	d.tabsAgentsApps = agentsApps
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

// refreshInfo reloads the details in place (no skeleton), once no write
// is in flight.
func (d *ChannelDetails) refreshInfo() {
	if d.writing() {
		d.refreshDue = true
		return
	}
	d.refreshDue = false
	c, id := d.h.Core(), d.id
	d.info.load(d.h, true, func(ctx context.Context) (core.ChannelDetails, error) {
		return c.ChannelInfo(ctx, id)
	})
}

// watch subscribes to this conversation's conv / conv_removed events, and
// to presence_change / user_change for the loaded members (their rows read
// presence live, so an empty note just redraws); its permissions and
// channel_email events and the team and org prefs redraw too.
func (d *ChannelDetails) watch() {
	id := d.id
	shown := &d.presIDs
	d.evq.start(d.h, func(ev core.Event) (dirMemberNote, bool) {
		switch {
		case ev.Type == "resync":
			return dirMemberNote{id: id, verify: true}, true
		case ev.Type == "presence" || ev.Type == "user":
			ids := shown.Load()
			if ids == nil {
				return dirMemberNote{}, false
			}
			if ev.User != nil && (*ids)[ev.User.ID] || (*ids)[ev.UserID] {
				return dirMemberNote{}, true
			}
			for _, u := range ev.Users {
				if (*ids)[u] {
					return dirMemberNote{}, true
				}
			}
			return dirMemberNote{}, false
		case ev.Type == "team_prefs" || ev.Type == "enterprise_prefs":
			return dirMemberNote{}, true // calls_apps, disable_email_ingestion, the org's email pref: redraw
		case ev.Channel != id:
			return dirMemberNote{}, false
		case ev.Type == "permissions" || ev.Type == "channel_email":
			return dirMemberNote{}, true // the email grant, a kept address: redraw (details_email.go)
		case ev.Type == "conv":
			return dirMemberNote{id: id, verify: true}, true
		case ev.Type == "conv_removed":
			return dirMemberNote{id: id, ev: true}, true
		}
		return dirMemberNote{}, false
	})
	for _, n := range d.evq.drain() {
		switch {
		case n.verify:
			d.refreshInfo()
		case n.ev && cdGroup(d.kind) == "channel" && !d.writing():
			// Left from elsewhere (or archived): show Join channel.
			d.confirmLeave = false
			d.info.mutate(func(v *core.ChannelDetails) {
				if v.IsMember {
					v.IsMember = false
					if v.Conv.NumMembers > 0 {
						v.Conv.NumMembers--
					}
				}
			})
			d.refreshInfo()
		}
	}
	if d.refreshDue && !d.writing() {
		d.refreshInfo()
	}
}

func (d *ChannelDetails) loadMembers(marker string) {
	var prev []core.User
	if marker != "" {
		cur, _, _ := d.members.get()
		prev = append([]core.User(nil), cur.users...)
	}
	c, id, self := d.h.Core(), d.id, d.selfID()
	d.members.load(d.h, true, func(ctx context.Context) (cdMembersPage, error) {
		// Slack's Members tab is people only (MEMBER_LIST filter "everyone
		// AND NOT bots AND NOT apps"); bots and apps are Agents & apps.
		us, next, err := c.Members(ctx, id, marker, true)
		if err != nil {
			return cdMembersPage{}, err
		}
		return cdMembersPage{users: cdSortMembers(append(prev, us...), self), next: next}, nil
	})
}

func (d *ChannelDetails) loadBookmarks() {
	c, id := d.h.Core(), d.id
	d.bookmarks.load(d.h, false, func(ctx context.Context) ([]core.Bookmark, error) {
		return c.Bookmarks(ctx, id)
	})
}

// Inline edit kinds (one editor, one at a time).
const (
	cdEditTopic = iota + 1
	cdEditPurpose
	cdEditName
)

var cdEditWhat = map[int]string{cdEditTopic: "topic", cdEditPurpose: "description", cdEditName: "channel name"}

// Escape closes the innermost open layer (a menu, the stacked edit
// dialog, the leave confirmation) or else the dialog, as Slack's topmost
// modal takes Escape; it reports whether the details dialog stays open.
// The root's Esc calls it through the overlay's Dismiss.
func (d *ChannelDetails) Escape() bool {
	switch {
	case d.memFilter.open || d.tabsPerm.open || d.notifMenu.open:
		d.memFilter.open, d.tabsPerm.open, d.notifMenu.open = false, false, false
	case d.escapeDialog():
	case d.editing:
		d.editing, d.editErr = false, ""
	case d.confirmLeave:
		d.confirmLeave = false
	default:
		d.closed = true
	}
	d.h.Invalidate()
	return !d.closed
}

// editStops is the edit dialog's tab order (ChannelTopicDialog is a
// DeprecatedBaseDialog: the header's close button, the input, Cancel,
// Save), trapped and wrapping.
func (d *ChannelDetails) editStops() []any {
	return []any{&d.editClose, &d.topicEd, &d.cancelTop, &d.saveTopic}
}

// editKeys runs Tab / Shift+Tab in the edit dialog.
func (d *ChannelDetails) editKeys(gtx C) {
	if !d.editing || (d.editKind != cdEditTopic && d.editKind != cdEditPurpose) {
		return
	}
	for {
		ev, ok := gtx.Event(key.Filter{Name: key.NameTab, Optional: key.ModShift})
		if !ok {
			break
		}
		e, ok := ev.(key.Event)
		if !ok || e.State != key.Press {
			continue
		}
		st := d.editStops()
		cur := -1
		for i, t := range st {
			if gtx.Focused(t) {
				cur = i
			}
		}
		next := 0
		back := e.Modifiers.Contain(key.ModShift)
		switch {
		case cur < 0 && back:
			next = len(st) - 1
		case back:
			next = (cur - 1 + len(st)) % len(st)
		case cur >= 0:
			next = (cur + 1) % len(st)
		}
		gtx.Execute(key.FocusCmd{Tag: st[next]})
		d.editRing = true
		d.h.Invalidate()
	}
}

// ringed draws w with Slack's focus ring under it when c has a focus the
// keyboard gave it.
func (d *ChannelDetails) ringed(gtx C, c *widget.Clickable, w layout.Widget) D {
	m := op.Record(gtx.Ops)
	dims := w(gtx)
	call := m.Stop()
	if d.editRing && gtx.Focused(c) {
		popups.FocusRingColors(gtx, d.p.Link, flatten(theme.WithAlpha(d.p.Link, 0x4d), d.p.Surface), image.Rectangle{Max: dims.Size}, gtx.Dp(8))
	}
	call.Add(gtx.Ops)
	return dims
}

// EditFocus names what holds the key focus in the edit dialog ("close",
// "input", "cancel", "save" or "") and whether the ring shows (tests).
func (d *ChannelDetails) EditFocus(gtx C) (string, bool) {
	for i, t := range d.editStops() {
		if gtx.Focused(t) {
			return [...]string{"close", "input", "cancel", "save"}[i], d.editRing
		}
	}
	return "", false
}

// startEdit opens the inline editor for kind with its current value.
func (d *ChannelDetails) startEdit(kind int, cur string) {
	d.editing, d.editKind, d.confirmLeave = true, kind, false
	d.editRing = false
	// The topic and description inputs are Texty fields whose onEnter
	// (onTextyEnter) returns true: Texty then inserts a newline, and the
	// dialog's Enter→go skips contentEditable targets, so Enter never
	// saves there. The channel name stays a one-line input.
	d.topicEd.SingleLine = kind == cdEditName
	d.topicEd.Submit = kind == cdEditName
	d.topicEd.MaxLen = 250
	if kind == cdEditName {
		d.topicEd.MaxLen = 80
	}
	d.editErr = ""
	if kind != cdEditName {
		cur = cdEditableText(cur)
	}
	d.topicEd.SetText(cur)
	n := d.topicEd.Len()
	d.topicEd.SetCaret(n, n)
	d.focusTopic = 3
}

// cdEditableText is a topic or description as Slack's editor shows it:
// links as their text (<https://x> and <https://x|https://x> become
// https://x), the rest of the mrkdwn untouched.
func cdEditableText(s string) string {
	var b strings.Builder
	for {
		i := strings.Index(s, "<http")
		if i < 0 {
			b.WriteString(s)
			return b.String()
		}
		j := strings.IndexByte(s[i:], '>')
		if j < 0 {
			b.WriteString(s)
			return b.String()
		}
		inner := s[i+1 : i+j]
		u, label, hasLabel := strings.Cut(inner, "|")
		b.WriteString(s[:i])
		switch {
		case !hasLabel:
			b.WriteString(u)
		case label == u || "http://"+label == u || "https://"+label == u:
			b.WriteString(label)
		default:
			b.WriteString(s[i : i+j+1])
		}
		s = s[i+j+1:]
	}
}

// cdTopicError is Slack's getChannelTopicError for a setTopic error code.
func cdTopicError(err error) string {
	s := err.Error()
	has := func(code string) bool { return strings.Contains(s, code) }
	switch {
	case has("too_long"):
		return "Channel topics can only include up to 250 characters — please make this topic shorter."
	case has("restricted_action"), has("user_is_restricted"):
		return "It looks like you don’t have permission to change the channel topic. Please check with your workspace admin."
	case has("method_not_supported_for_channel_type"):
		return "It’s not possible to set a topic here, unfortunately."
	case has("method_not_supported_for_dm"):
		return "Regrettably, direct messages do not have topics."
	}
	return "It looks like setting the channel topic didn’t work, for some reason. Try again?"
}

// cdChannelName normalizes a typed channel name the way Slack does:
// lowercase, spaces to dashes, no leading #.
func cdChannelName(s string) string {
	s = strings.ToLower(strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(s), "#")))
	return strings.Join(strings.Fields(s), "-")
}

// saveTopicNow applies the open edit (topic, description or name)
// optimistically and sends it; update reverts on failure.
func (d *ChannelDetails) saveTopicNow() {
	t := strings.TrimSpace(d.topicEd.Text())
	kind := d.editKind
	if d.topicPending {
		return
	}
	// The topic and description modals stay open until Slack answers
	// (closing on success, showing the error otherwise); an unchanged
	// value just closes them.
	d.editing = kind != cdEditName
	defer func() {
		if !d.topicPending {
			d.editing = false
		}
	}()
	inf, _, _ := d.info.get()
	c, id := d.h.Core(), d.id
	var send func(ctx context.Context) error
	switch kind {
	case cdEditPurpose:
		if t == inf.Conv.Purpose {
			return
		}
		d.info.mutate(func(v *core.ChannelDetails) { v.Conv.Purpose, v.Conv.PurposeDoc = t, nil })
		send = func(ctx context.Context) error { return c.SetPurpose(ctx, id, t) }
	case cdEditName:
		t = cdChannelName(t)
		if t == "" || t == inf.Conv.Name {
			return
		}
		d.info.mutate(func(v *core.ChannelDetails) { v.Conv.Name = t })
		send = func(ctx context.Context) error { return c.Rename(ctx, id, t) }
	default:
		if t == inf.Conv.Topic {
			return
		}
		d.info.mutate(func(v *core.ChannelDetails) { v.Conv.Topic, v.Conv.TopicDoc = t, nil })
		send = func(ctx context.Context) error { return c.SetTopic(ctx, id, t) }
	}
	d.prevConv, d.pendKind = inf.Conv, kind
	d.topicPending = true
	d.topicOp.load(d.h, false, func(ctx context.Context) (bool, error) { return true, send(ctx) })
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

// writing reports whether a write from this dialog is in flight.
func (d *ChannelDetails) writing() bool {
	return d.topicPending || d.leavePending || d.joinPending || d.starPending || d.notifPending || d.inviteUser != ""
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
		if d.Escape() {
			gtx.Execute(key.FocusCmd{})
		}
	}
	d.editKeys(gtx)
	if d.scrim.Clicked(gtx) || d.closeBtn.Clicked(gtx) {
		d.closed = true
	}
	d.dlg.Clicked(gtx)
	if d.tab.update(gtx) {
		d.editing, d.confirmLeave = false, false
		d.memFilter.open, d.tabsPerm.open, d.notifMenu.open = false, false, false
	}
	d.memFilter.update(gtx)

	if !d.info.started() {
		d.loadInfo()
	}
	d.watch()
	inf, loaded, _ := d.info.get()
	if loaded && inf.Conv.Kind != "" && inf.Conv.Kind != d.kind {
		regroup := cdGroup(inf.Conv.Kind) != cdGroup(d.kind)
		d.kind = inf.Conv.Kind
		if regroup {
			d.setTabs()
		}
	}
	if (d.h.Core().Experiment("channel_details_agents_apps_tab") == "on") != d.tabsAgentsApps {
		d.setTabs() // the group changed under the open dialog
	}

	d.updateAddApps(gtx) // "Add an App" / "Add agents and apps": the apps browser (details_more.go)
	for _, c := range []*widget.Clickable{&d.addWorkflow} {
		if c.Clicked(gtx) {
			d.h.Toast(cdUnsupported)
		}
	}
	if d.call.Clicked(gtx) {
		d.callClicked() // details_call.go
	}
	if d.emails.Clicked(gtx) {
		d.openEmails() // details_email.go
	}
	d.updateSlackParts(gtx, inf, loaded)
	if d.orgLearn.Clicked(gtx) {
		// Slack's HelpLink articleId 0x1ac69e1923
		hostHandler{d.h}.OpenURL("https://slack.com/help/articles/115001399587")
	}
	if d.addPeople.Clicked(gtx) && loaded {
		// About's "Add people to this conversation" (1:1 DMs): Slack's
		// openMpdmAddPeopleModal
		d.openAddPeople(inf)
	}
	d.updateWrites(gtx, inf, loaded)

	// Topic / description / name editing.
	if d.editTopic.Clicked(gtx) && loaded {
		d.startEdit(cdEditTopic, inf.Conv.Topic)
	}
	if d.x.mpdmName.Clicked(gtx) && loaded {
		d.openRenameMpdm(d.conv(inf, true))
	}
	if d.editPurpose.Clicked(gtx) && loaded {
		d.startEdit(cdEditPurpose, inf.Conv.Purpose)
	}
	if d.editName.Clicked(gtx) && loaded {
		d.startEdit(cdEditName, inf.Conv.Name)
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
		d.editing, d.editErr = false, ""
	}
	if d.saveTopic.Clicked(gtx) && d.editing {
		d.saveTopicNow()
	}
	if d.topicPending {
		if _, done, err := d.topicOp.get(); done {
			d.topicPending = false
			switch {
			case err == nil:
				d.editing, d.editErr = false, ""
			default:
				prev := d.prevConv
				d.info.mutate(func(v *core.ChannelDetails) {
					v.Conv.Topic, v.Conv.TopicDoc = prev.Topic, prev.TopicDoc
					v.Conv.Purpose, v.Conv.PurposeDoc = prev.Purpose, prev.PurposeDoc
					v.Conv.Name = prev.Name
				})
				if d.pendKind == cdEditTopic && d.editing {
					d.editErr = cdTopicError(err) // the modal stays open, the error under the input
				} else {
					d.editing = false
					d.h.Toast("Couldn’t update the " + cdEditWhat[d.pendKind] + ": " + cdErr(err))
				}
			}
		}
	}

	// Leave / join / hide.
	name := inf.Conv.Name
	if d.leaveRow.Clicked(gtx) && loaded {
		// Slack's maybeLeaveChannel (details_more.go)
		d.editing = false
		d.maybeLeave(inf)
	}
	d.updateLeave()
	if d.leavePending {
		if _, done, err := d.leaveOp.get(); done {
			d.leavePending = false
			dm := cdGroup(d.kind) == "im"
			switch {
			case err != nil && dm:
				d.h.Toast("Couldn’t hide this conversation: " + friendlyErr(err))
			case err != nil:
				d.confirmLeave = false
				d.h.Toast("Couldn’t leave #" + name + ": " + dirErr(err))
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
		c, id, tt := d.h.Core(), d.id, d.tt
		d.joinPending = true
		d.joinOp.load(d.h, false, func(ctx context.Context) (bool, error) {
			err := joinChannelThunk(ctx, tt, c, id)
			return err == nil, err
		})
	}
	if d.joinPending {
		if _, done, err := d.joinOp.get(); done {
			d.joinPending = false
			if err != nil {
				d.h.Toast("Couldn’t join #" + name + ": " + dirErr(err))
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
			// "View full profile" (about-channel-modal vWNA): the dialog
			// closes, then showMemberProfile opens the profile pane
			d.closed = true
			d.h.OpenProfile(d.press.at, uid)
		}
	}
	if d.createdBy.Clicked(gtx) && inf.Conv.Creator != "" {
		// Clack keeps its card here. Slack's row isn't a profile link: it
		// closes the dialog and jumps to the channel's start (vWNA
		// BaseChannelInfo, jumpToMessage at the created ts).
		openProfileCard(d.h, d.press.at, inf.Conv.Creator)
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
	// Stays translucent; LinearAlpha makes Gio's linear blend match the
	// browser's sRGB result over the page background.
	scrim = theme.LinearAlpha(scrim, d.p.Bg)
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
	d.cl.dlg = image.Pt((full.X-dw)/2, (full.Y-dh)/2) // the overlay is at the window's origin
	func() {
		defer op.Offset(d.cl.dlg).Push(gtx.Ops).Pop()
		r := image.Rect(0, 0, dw, dh)
		rad := gtx.Dp(cdRadius)
		theme.Shadow(gtx, r, rad, gtx.Dp(24), argb(0x59000000))
		rrect(gtx, r.Inset(-max(1, gtx.Dp(1))), rad+max(1, gtx.Dp(1)), d.p.Ring)
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
	if d.editing && (d.editKind == cdEditTopic || d.editKind == cdEditPurpose) {
		d.editModal(gtx, full)
	}
	if d.x.dlg.kind != cdDlgNone {
		inf, _, _ := d.info.get()
		d.layoutDialog(gtx, full, inf)
	}
	return D{Size: full}
}

// editModal is Slack's stacked "Edit topic" / "Edit description" dialog
// over channel details (520 wide, at 1440×900: 460,336 520×228): the title
// 22/30 Black at (24, 19), the close button 36×36 16px from the right,
// the input with its focus ring, the hint (or, for the topic, the save
// error) 13/18 #5e5d60 under it, then Cancel / Save 80×36 at the bottom
// right (24px in, 20px up). Enter adds a line (onTextyEnter); Tab cycles
// close, input, Cancel, Save; Escape closes this dialog only.
func (d *ChannelDetails) editModal(gtx C, full image.Point) {
	scrim := theme.LinearAlpha(argb(0x991d1c1d), d.p.Bg)
	d.editScrim.Layout(gtx, func(gtx C) D {
		fill(gtx, image.Rectangle{Max: full}, scrim)
		return D{Size: full}
	})
	if d.editScrim.Clicked(gtx) {
		d.editing, d.editErr = false, ""
		gtx.Execute(key.FocusCmd{})
		return
	}
	defer func() { // last, over the dialog: a mouse press hides the ring
		for {
			ev, ok := gtx.Event(pointer.Filter{Target: &d.editPtr, Kinds: pointer.Press})
			if !ok {
				break
			}
			if pe, ok := ev.(pointer.Event); ok && pe.Kind == pointer.Press {
				d.editRing = false
			}
		}
		defer pointer.PassOp{}.Push(gtx.Ops).Pop()
		defer clip.Rect{Max: full}.Push(gtx.Ops).Pop()
		event.Op(gtx.Ops, &d.editPtr)
	}()
	w := min(gtx.Dp(520), full.X-gtx.Dp(32))
	title, hint := "Edit topic", "Let people know what this channel is for."
	if d.editKind == cdEditTopic {
		inf, _, _ := d.info.get()
		hint = "Let people know what #" + inf.Conv.Name + " is focused on right now (ex. a project milestone). Topics are always visible in the header."
	} else {
		title = "Edit description"
	}
	hintCol := d.p.Muted
	if d.editErr != "" {
		hint, hintCol = d.editErr, d.p.Red
	}
	inner := w - gtx.Dp(48)
	m := op.Record(gtx.Ops)
	dims := layout.Inset{Top: 19, Bottom: 20, Left: 24, Right: 24}.Layout(gtx, func(gtx C) D {
		gtx.Constraints = layout.Exact(image.Pt(inner, gtx.Constraints.Max.Y))
		gtx.Constraints.Min.Y = 0
		return col(gtx,
			layout.Rigid(func(gtx C) D {
				return row(gtx, layout.Middle,
					layout.Flexed(1, func(gtx C) D {
						return dirFill(gtx, func(gtx C) D { return d.para(gtx, 22, 30, wBlack, d.p.Text, 1, title) })
					}),
					layout.Rigid(func(gtx C) D {
						return layout.Inset{Right: -8}.Layout(gtx, func(gtx C) D {
							return d.ringed(gtx, &d.editClose, func(gtx C) D { return d.iconButton(gtx, &d.editClose, "close", 36, 20, d.p.Sec) })
						})
					}),
				)
			}),
			vgap(16),
			layout.Rigid(d.topicInput),
			vgap(6),
			layout.Rigid(func(gtx C) D {
				if d.editKind == cdEditTopic && d.editErr == "" {
					// "#name" is bold in Slack's hint
					inf, _, _ := d.info.get()
					pre := "Let people know what "
					return d.richHint(gtx, pre, "#"+inf.Conv.Name, hint[len(pre)+1+len(inf.Conv.Name):], hintCol)
				}
				return d.para(gtx, 13, 18, wReg, hintCol, 0, hint)
			}),
			vgap(20),
			layout.Rigid(func(gtx C) D {
				gtx.Constraints.Min.X = gtx.Constraints.Max.X
				return layout.E.Layout(gtx, func(gtx C) D {
					return row(gtx, layout.Middle,
						layout.Rigid(func(gtx C) D {
							return d.ringed(gtx, &d.cancelTop, func(gtx C) D { return d.button(gtx, &d.cancelTop, btnOutline, false, "Cancel") })
						}),
						hgap(12),
						layout.Rigid(func(gtx C) D {
							return d.ringed(gtx, &d.saveTopic, func(gtx C) D { return d.button(gtx, &d.saveTopic, btnPrimary, false, "Save") })
						}),
					)
				})
			}),
		)
	})
	call := m.Stop()
	h := dims.Size.Y
	org := image.Pt((full.X-w)/2, (full.Y-h)/2)
	defer op.Offset(org).Push(gtx.Ops).Pop()
	r := image.Rect(0, 0, w, h)
	rad := gtx.Dp(cdRadius)
	theme.Shadow(gtx, r, rad, gtx.Dp(24), argb(0x59000000))
	rrect(gtx, r, rad, d.p.Surface)
	g := gtx
	g.Constraints = layout.Exact(r.Max)
	d.editDlg.Layout(g, func(gtx C) D { return D{Size: r.Max} })
	call.Add(gtx.Ops)
	if d.editClose.Clicked(gtx) {
		d.editing, d.editErr = false, ""
		gtx.Execute(key.FocusCmd{})
	}
}

// richHint is a 13/18 line with one bold run (the hint's #channel).
func (d *ChannelDetails) richHint(gtx C, pre, bold, post string, col color.NRGBA) D {
	spans := []richtext.Span{{Kind: richtext.Text, Text: pre}, {Kind: richtext.Text, Text: bold, Style: richtext.Bold}, {Kind: richtext.Text, Text: post}}
	return d.richDoc(gtx, "edithint/"+d.id, pre+bold+post, richtext.Doc{Blocks: []richtext.Block{{Kind: richtext.Paragraph, Spans: spans}}},
		richview.Options{Compact: true, Secondary: true}, 0)
}

// topicInput is the modal's single input: 15/22, padding 8 12, radius 8,
// the 1px hairline, and Slack's focus ring when focused.
func (d *ChannelDetails) topicInput(gtx C) D {
	w := gtx.Constraints.Max.X
	m := op.Record(gtx.Ops)
	td := layout.Inset{Top: 8, Bottom: 8, Left: 12, Right: 12}.Layout(gtx, func(gtx C) D {
		gtx.Constraints.Min.X = gtx.Constraints.Max.X
		gtx.Constraints.Min.Y = gtx.Sp(22)
		return d.th.Editor(gtx, &d.topicEd, 15, d.p.Text, d.p.Mention)
	})
	call := m.Stop()
	r := image.Rect(0, 0, w, td.Size.Y)
	rad := gtx.Dp(8)
	if gtx.Focused(&d.topicEd) {
		ring := gtx.Dp(5)
		rrect(gtx, r.Inset(-ring), rad+ring, flatten(theme.WithAlpha(d.p.Link, 0x4d), d.p.Surface))
		one := max(1, gtx.Dp(1))
		rrect(gtx, r.Inset(-one), rad+one, d.p.Link)
	}
	rrect(gtx, r, rad, d.p.Surface)
	if !gtx.Focused(&d.topicEd) {
		border(gtx, r, rad, max(1, gtx.Dp(1)), d.p.Faint)
	}
	call.Add(gtx.Ops)
	return D{Size: r.Size()}
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
		named, small := false, false
		if cdGroup(d.kind) == "mpim" {
			_, named, small = d.mpdmTitle(cv)
		}
		dirAt(gtx, image.Pt(gtx.Dp(cdPadX), gtx.Dp(20)), func(gtx C) D {
			gtx.Constraints.Max.X = max(0, w-gtx.Dp(cdPadX+64))
			return row(gtx, layout.Middle,
				layout.Rigid(func(gtx C) D {
					if named { // BaseChannelPrefixMpim: margin 0 8px 0 -4px, then MPDMInfo's gap 8px
						return layout.Inset{Left: -4, Right: 16}.Layout(gtx, func(gtx C) D {
							gtx.Constraints = layout.Exact(image.Pt(gtx.Dp(20), gtx.Dp(20)))
							d.mpdmPrefix(gtx, cv)
							return D{Size: gtx.Constraints.Min}
						})
					}
					if ic == "" {
						return D{}
					}
					return layout.Inset{Right: 2}.Layout(gtx, func(gtx C) D { return d.icon(gtx, ic, 22, d.p.Text) })
				}),
				layout.Rigid(func(gtx C) D {
					if small { // .p-view_header__channel_title--small: 18px/1.33334
						return d.para(gtx, 18, 24, wBlack, d.p.Text, 1, title)
					}
					return d.para(gtx, 22, 30, wBlack, d.p.Text, 1, title)
				}),
			)
		})
	}
	dirAt(gtx, image.Pt(w-gtx.Dp(56), gtx.Dp(20)), func(gtx C) D {
		return d.iconButton(gtx, &d.closeBtn, "close", 36, 20, d.p.Sec)
	})
	btnRow := dirAt(gtx, image.Pt(gtx.Dp(cdPadX), btnY), func(gtx C) D {
		ch := []layout.FlexChild{
			layout.Rigid(func(gtx C) D {
				ic := "star"
				if d.isStarred() {
					ic = "star-filled"
				}
				return cdBtn(&d.base, gtx, &d.star, ic, "", true, 56)
			}),
			hgap(8),
		}
		if dm {
			ch = append(ch,
				layout.Rigid(func(gtx C) D {
					label := "Mute"
					if d.muted(cv) {
						label = "Unmute"
					}
					return cdBtn(&d.base, gtx, &d.notif, "bell", label, false, 0)
				}),
				hgap(8))
			if u, ok := d.dmPerson(inf); ok {
				// canToggleVip: "VIP" after user-add (user-hide once a VIP,
				// a glyph Clack's icon set lacks); canHide: Hide / Unhide
				vipIcon := "user-add"
				if d.isVIP(u.ID) {
					vipIcon = ""
				}
				label := "Hide"
				if d.h.Core().UserHidden(u.ID) {
					label = "Unhide"
				}
				ch = append(ch,
					layout.Rigid(func(gtx C) D { return d.cdHeaderBtn(gtx, &d.vip, vipIcon, "VIP") }),
					hgap(8),
					layout.Rigid(func(gtx C) D { return d.cdHeaderBtn(gtx, &d.hide, "", label) }),
					hgap(8))
			}
		} else {
			label := d.notifLabel(cv)
			ch = append(ch,
				layout.Rigid(func(gtx C) D {
					dims := cdBtn(&d.base, gtx, &d.notifMenu.btn, "bell", label, true, 0)
					if d.notifMenu.open {
						d.notifMenu.menu(&d.base, gtx, dims.Size, false)
					}
					return dims
				}),
				hgap(8))
		}
		if d.callVisible() { // CallButton (details_call.go)
			ch = append(ch, layout.Rigid(d.callButton))
		}
		return row(gtx, layout.Middle, ch...)
	})
	// the call button is the row's last: its right edge is the row's
	d.cl.row = image.Rectangle{Min: image.Pt(gtx.Dp(cdPadX), btnY), Max: image.Pt(gtx.Dp(cdPadX), btnY).Add(btnRow.Size)}
	dirAt(gtx, image.Pt(gtx.Dp(cdPadX-8), tabY), func(gtx C) D {
		counts := make([]string, len(d.tabKinds))
		for i, k := range d.tabKinds {
			if k == cdApps {
				// Slack: "Agents & apps 7", users/counts apps
				if mc, ok, _ := d.counts.get(); ok && mc.Apps > 0 {
					counts[i] = humanCount(mc.Apps)
				}
				continue
			}
			if k != cdMembers {
				continue
			}
			n := cv.NumMembers
			if mc, ok, _ := d.counts.get(); ok && mc.People > 0 {
				n = mc.People
			}
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
	if cdGroup(d.kind) == "mpim" { // MPDMTitle (details_mpdm.go)
		if t, _, _ := d.mpdmTitle(cv); t != "" {
			return t, ""
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
		items = d.appsItems()
	case cdAutomations:
		items = d.automationsItems() // details_automations.go
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
	line := dirRing(b, b.p.Surface)
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

// cdLine22 gives w at least one 22px line: Slack's 15/22 field text, whose
// single lines Gio measures at ~19px (rows came out 3–6px short of
// Slack's 77, 2026-11-08).
func cdLine22(gtx C, w W) D {
	dims := w(gtx)
	if h := gtx.Sp(22); dims.Size.Y < h && dims.Size.Y > 0 {
		dims.Size.Y = h
	}
	return dims
}

// cdField is a field row: padding 16px 20px, 15/22 bold label, an
// optional "Edit" (13/18 bold #1264a3) at the right, the value below.
func (d *ChannelDetails) cdField(gtx C, label string, edit *widget.Clickable, value W) D {
	return layout.Inset{Top: 16, Bottom: 16, Left: 20, Right: 20}.Layout(gtx, func(gtx C) D {
		gtx.Constraints.Min.X = gtx.Constraints.Max.X
		return col(gtx,
			layout.Rigid(func(gtx C) D {
				return cdLine22(gtx, func(gtx C) D {
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
				})
			}),
			layout.Rigid(func(gtx C) D {
				if value == nil {
					return D{}
				}
				return cdLine22(gtx, func(gtx C) D { return dirFill(gtx, value) })
			}),
		)
	})
}

// cdFieldW is cdField with a drawn label (no Edit).
func (d *ChannelDetails) cdFieldW(gtx C, label, value W) D {
	return layout.Inset{Top: 16, Bottom: 16, Left: 20, Right: 20}.Layout(gtx, func(gtx C) D {
		gtx.Constraints.Min.X = gtx.Constraints.Max.X
		return col(gtx,
			layout.Rigid(func(gtx C) D { return cdLine22(gtx, func(gtx C) D { return dirFill(gtx, label) }) }),
			layout.Rigid(func(gtx C) D { return cdLine22(gtx, func(gtx C) D { return dirFill(gtx, value) }) }),
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
		items = append(items, cdGap(16), d.cdCard(d.addPeopleField))
		if d.inviting {
			items = append(items, cdGap(8), func(gtx C) D { return d.invitePanel(gtx, d.dmMembers(cv)) })
		}
		items = append(items, d.emailsItems()...)
	case "mpim":
		items = []W{cdGap(16)}
		if d.h.Core().CanNameMpdm(d.id) { // MpdmConversationNameField
			items = append(items, d.cdCard(d.mpdmNameField), cdGap(16))
		}
		items = append(items, d.cdCard(topic))
		items = append(items, d.emailsItems()...)
	default:
		rows := []W{topic, func(gtx C) D { return d.purposeField(gtx, cv) }}
		if d.showOrgAccess(inf) {
			rows = append(rows, func(gtx C) D { return d.workspacesField(gtx, inf) })
		}
		if mgr := d.managerIDs(); len(mgr) > 0 {
			rows = append(rows, func(gtx C) D { return d.managedField(gtx, mgr) })
		}
		if cv.Creator != "" || cv.Created != 0 {
			rows = append(rows, func(gtx C) D { return d.createdField(gtx, cv) })
		}
		if !cv.IsArchived && !(cv.IsGeneral && inf.IsMember) {
			rows = append(rows, func(gtx C) D { return d.leaveField(gtx, inf) })
		}
		items = []W{cdGap(16), d.cdCard(rows...)}
		items = append(items, d.emailsItems()...)
	}
	return append(items, d.idLine)
}

// topicField is ChannelTopicField: an EditableField, disabled without
// canUserSetTopicForChannelWithReasonById.
func (d *ChannelDetails) topicField(gtx C, cv core.Conv) D {
	value := func(gtx C) D { return d.richText(gtx, "topic/"+d.id, cv.Topic, cv.TopicDoc, "Add a topic") }
	if !d.h.Core().CanSetTopic(d.id) {
		return d.cdFieldDisabled(gtx, "Topic", value)
	}
	return d.cdField(gtx, "Topic", &d.editTopic, value)
}

// purposeField is ChannelPurposeField: disabled without canUserSetPurpose.
func (d *ChannelDetails) purposeField(gtx C, cv core.Conv) D {
	value := func(gtx C) D {
		return d.richText(gtx, "purpose/"+d.id, cv.Purpose, cv.PurposeDoc, "Add a description")
	}
	if !d.h.Core().CanSetPurpose(d.id) {
		return d.cdFieldDisabled(gtx, "Description", value)
	}
	return d.cdField(gtx, "Description", &d.editPurpose, value)
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

// createdField: "Created by" / "<name> on May 17, 2026" in plain text, like
// Slack; the row opens the creator's profile.
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
					return d.para(gtx, 15, 22, wReg, d.p.Text, 1, name) // Slack: plain text (the whole row is the button)
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
	if !inf.IsMember {
		label := "Join channel"
		if d.joinPending {
			label = "Joining…"
		}
		return d.cdButtonField(gtx, &d.joinRow, func(gtx C) D { return d.cdField(gtx, label, nil, nil) })
	}
	label := "Leave channel"
	if d.leavePending || d.x.leaveCount.busy() {
		label = "Leaving…"
	}
	return d.cdButtonField(gtx, &d.leaveRow, func(gtx C) D {
		return layout.Inset{Top: 16, Bottom: 16, Left: 20, Right: 20}.Layout(gtx, func(gtx C) D {
			return dirFill(gtx, func(gtx C) D {
				return cdLine22(gtx, func(gtx C) D { return d.para(gtx, 15, 22, wBold, d.p.Red, 1, label) })
			})
		})
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
				rrect(gtx, r.Inset(-ring), rad+ring, flatten(theme.WithAlpha(d.p.Link, 0x4d), d.p.Surface))
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
			rrect(gtx, image.Rect(0, 0, s, s), gtx.Dp(4), flatten(cdTileBg, d.p.Surface))
			g := gtx
			g.Constraints = layout.Exact(image.Pt(s, s))
			layout.Center.Layout(g, func(gtx C) D { return d.icon(gtx, icon, 20, d.p.Link) })
			return D{Size: image.Pt(s, s)}
		}),
		hgap(12),
		layout.Flexed(1, func(gtx C) D { return dirFill(gtx, text) }),
	)
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
	if len(mp.users) != d.presN {
		ids := make(map[string]bool, len(mp.users))
		for _, u := range mp.users {
			ids[u.ID] = true
		}
		d.presIDs.Store(&ids)
		d.presN = len(mp.users)
	}
	busy := d.members.busy()
	users := d.filtered(mp.users, q, d.memFilter.sel)
	if d.moreMembers.Clicked(gtx) && mp.next != "" && !busy {
		d.loadMembers(mp.next)
		busy = true
	}
	filtering := q != "" || d.memFilter.sel != 0
	inv := 0
	if d.inviting {
		inv = 1
	}
	n := 1 + inv + len(users) + 1 // "Add people or agents", invite panel, members, footer
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
						return dirSearchField(&d.base, gtx, d.find, dirFieldStyle{H: 36, Icon: 16, PadL: 8, Gap: 8, Border: d.p.Faint, Over: d.p.Surface}, nil)
					}),
					hgap(8),
					layout.Rigid(func(gtx C) D { return d.memFilter.layoutSelect(&d.base, gtx, 200) }),
				)
			})
			if p := l.Position; p.First > 0 || p.Offset > 0 {
				fill(gtx, image.Rect(0, dims.Size.Y-max(1, gtx.Dp(1)), gtx.Constraints.Max.X, dims.Size.Y), dirRing(&d.base, d.p.Surface))
			}
			return dims
		}),
		layout.Flexed(1, func(gtx C) D {
			return d.scrollList(gtx, l, n, func(gtx C, i int) D {
				gtx.Constraints.Min.X = gtx.Constraints.Max.X
				switch {
				case i == 0:
					return d.addMembersRow(gtx)
				case inv == 1 && i == 1:
					return d.invitePanel(gtx, mp.users)
				case i-inv <= len(users):
					return d.memberRow(gtx, users[i-inv-1], self)
				}
				return d.membersFooter(gtx, q, filtering, loaded, err, busy, mp.next, len(users))
			})
		}),
	)
}

// cdSortMembers is the Members list order Slack showed for #clack
// (2026-11-08): the viewer first, then by display name, case-insensitive
// ("clod (you)", dodge1, karim, kyto), whatever order the edge list
// (present_first) returned.
func cdSortMembers(us []core.User, self string) []core.User {
	out := cdSelfFirst(append([]core.User(nil), us...), self)
	rest := out
	if len(out) > 0 && out[0].ID == self {
		rest = out[1:]
	}
	sort.SliceStable(rest, func(i, j int) bool {
		return strings.ToLower(displayName(rest[i])) < strings.ToLower(displayName(rest[j]))
	})
	return out
}

// cdSelfFirst moves the viewer to the top, as Slack's members list has it
// ("clod (you)" first, then the others in order).
func cdSelfFirst(us []core.User, self string) []core.User {
	for i, u := range us {
		if u.ID == self && i > 0 {
			out := make([]core.User, 0, len(us))
			out = append(out, u)
			out = append(out, us[:i]...)
			return append(out, us[i+1:]...)
		}
	}
	return us
}

// cdMemberNames is MediumMemberEntity's names: the primary is the display
// name (the real name when none is set) and " (you)" for the viewer; the
// secondary is the real name, shown only after a display name.
func cdMemberNames(u core.User, self string) (primary, secondary string) {
	primary = displayName(u)
	if u.DisplaySet {
		secondary = u.RealName
	}
	if u.ID == self {
		primary += " (you)"
	}
	return primary, secondary
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
			label := "Add people or agents"
			if cdGroup(d.kind) != "channel" {
				label = "Add people" // a group DM takes no agents
			}
			return d.cdTileRow(gtx, "user-plus", func(gtx C) D { return d.label(gtx, 15, wBold, d.p.Text, label) })
		})
	})
}

// memberTrigger is member id's name trigger.
func (d *ChannelDetails) memberTrigger(id string) *hovercard.Trigger {
	t, ok := d.memberHov[id]
	if !ok {
		t = new(hovercard.Trigger)
		d.memberHov[id] = t
	}
	return t
}

// memberRow: h 60, padding 12px 28px; 36px avatar, 12px gap; line 1 the
// bold full name (+ " (you)"), presence, display name and status emoji;
// line 2 the title in #454447. The text block overhangs the avatar by 4px.
func (d *ChannelDetails) memberRow(gtx C, u core.User, self string) D {
	u = d.viewUser(u)
	// MediumMemberEntity's presence follows presence_change: the loaded
	// page is a snapshot, the socket's latest is in core
	if p := d.h.Core().UserPresence(u.ID); p != "" {
		u.Presence = p
	}
	c := dirClick(d.memberClk, u.ID)
	if c.Clicked(gtx) {
		// the members list row (ia-details-popover Rrv2):
		// showMemberProfile, the profile pane
		d.h.OpenProfile(d.press.at, u.ID)
	}
	full, disp := cdMemberNames(u, self)
	manager := false
	for _, m := range d.managerIDs() {
		manager = manager || m == u.ID
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
										layout.Rigid(func(gtx C) D {
											// MediumMemberEntity showHoverCard: the trigger wraps the primary name
											nd := d.para(gtx, 15, 22, wBold, d.p.Text, 1, full)
											d.memberTrigger(u.ID).Layout(gtx, u.ID, nd.Size)
											return nd
										}),
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
				layout.Rigid(func(gtx C) D {
					if !manager {
						return D{}
					}
					return layout.Inset{Left: 8}.Layout(gtx, d.managerPill)
				}),
			)
		})
	})
}

// managerPill is Slack's p-ia_details_popover__channel_manager_indicator:
// "Channel Manager" 13/18 regular on #1d1c1d@0.06, padding 4px 8px,
// radius 16 (C0CFT52NEGM, 2026-11-08).
func (d *ChannelDetails) managerPill(gtx C) D {
	m := op.Record(gtx.Ops)
	td := layout.Inset{Top: 4, Bottom: 4, Left: 8, Right: 8}.Layout(gtx, func(gtx C) D {
		return d.para(gtx, 13, 18, wReg, d.p.Text, 1, "Channel Manager")
	})
	call := m.Stop()
	rrect(gtx, image.Rectangle{Max: td.Size}, gtx.Dp(16), cdPillBg(&d.base))
	call.Add(gtx.Ops)
	return td
}

// cdPillBg is #1d1c1d@0.06 (light) or #d1d2d3@0.06 over the surface.
func cdPillBg(b *base) color.NRGBA {
	c := b.p.Text
	c.A = 0x0f
	return c
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

// ---- Agents & apps ----

// appsItems is Slack's Agents & apps tab: a field group headed "Agents and
// apps" with "Add agents and apps" at the right, then one 60px row per
// agent or app in the channel (36px icon, bold name and its AGENT/APP
// badge, the app's tagline in 13/18 #5e5d60). With none, the
// "Bring the tools…" prompt.
func (d *ChannelDetails) appsItems() []W {
	if !d.apps.started() {
		c, id := d.h.Core(), d.id
		d.apps.load(d.h, false, func(ctx context.Context) ([]core.ChannelApp, error) { return c.ChannelApps(ctx, id) })
	}
	apps, loaded, err := d.apps.get()
	switch {
	case !loaded:
		return []W{cdGap(16), d.cdCard(func(gtx C) D {
			return layout.Inset{Top: 12, Bottom: 12}.Layout(gtx, func(gtx C) D { return d.skeleton(gtx, 3, 36) })
		})}
	case err == nil && len(apps) == 0:
		return d.sectionItems("Apps", "Bring the tools you need into this channel to pull reports, start calls, file tickets and more.", "Add an App", &d.addApp)
	}
	head := func(gtx C) D {
		return layout.Inset{Top: 16, Left: 20, Right: 20, Bottom: 4}.Layout(gtx, func(gtx C) D {
			gtx.Constraints.Min.X = gtx.Constraints.Max.X
			return row(gtx, layout.Middle,
				layout.Flexed(1, func(gtx C) D {
					return dirFill(gtx, func(gtx C) D { return d.para(gtx, 15, 22, wBold, d.p.Text, 1, "Agents and apps") })
				}),
				layout.Rigid(func(gtx C) D { return cdLink(&d.base, gtx, &d.addApp, 13, "Add agents and apps") }),
			)
		})
	}
	rows := []W{head}
	if err != nil {
		rows = append(rows, func(gtx C) D {
			return layout.Inset{Top: 8, Bottom: 16, Left: 20, Right: 20}.Layout(gtx, func(gtx C) D {
				return d.para(gtx, 13, 18, wReg, d.p.Muted, 0, "Couldn’t load agents and apps: "+friendlyErr(err))
			})
		})
	}
	for i := range apps {
		ap := apps[i]
		rows = append(rows, func(gtx C) D { return d.appRow(gtx, ap) })
	}
	rows = append(rows, cdGap(16))
	// one card without hairlines between the rows (Slack's p-field--list)
	card := func(gtx C) D {
		return layout.Inset{Left: cdPadX, Right: cdPadX}.Layout(gtx, func(gtx C) D {
			return cdBox(&d.base, gtx, func(gtx C) D {
				ch := make([]layout.FlexChild, len(rows))
				for i, r := range rows {
					r := r
					ch[i] = layout.Rigid(func(gtx C) D { gtx.Constraints.Min.X = gtx.Constraints.Max.X; return r(gtx) })
				}
				return layout.Flex{Axis: layout.Vertical}.Layout(gtx, ch...)
			})
		})
	}
	return []W{cdGap(16), card, cdGap(24)}
}

// appRow: padding 12, 36px icon (radius 8), 12px gap; bold 15/22 name with
// the badge 4px after it; the tagline below. Opens the app's card: Slack's
// AppsSectionItem wraps the row in ProfileCardTrigger (By70).
func (d *ChannelDetails) appRow(gtx C, ap core.ChannelApp) D {
	u := d.viewUser(ap.User)
	c := dirClick(d.memberClk, "app:"+u.ID)
	if c.Clicked(gtx) {
		openProfileCard(d.h, d.press.at, u.ID)
	}
	name := dirFirst(u.RealName, displayName(u))
	return d.hoverable(gtx, c, false, 0, func(gtx C) D {
		return layout.Inset{Top: 12, Bottom: 12, Left: 12, Right: 12}.Layout(gtx, func(gtx C) D {
			return row(gtx, layout.Middle,
				layout.Rigid(func(gtx C) D { return d.avatar(gtx, u.Avatar, 36) }),
				hgap(12),
				layout.Flexed(1, func(gtx C) D {
					return dirFill(gtx, func(gtx C) D {
						return layout.Inset{Top: -2, Bottom: -2}.Layout(gtx, func(gtx C) D {
							return col(gtx,
								layout.Rigid(func(gtx C) D {
									return row(gtx, layout.Middle,
										layout.Rigid(func(gtx C) D { return d.para(gtx, 15, 22, wBold, d.p.Text, 1, name) }),
										layout.Rigid(func(gtx C) D {
											return layout.Inset{Left: 4}.Layout(gtx, func(gtx C) D { return cdAppBadge(&d.base, gtx, ap.Badge) })
										}),
									)
								}),
								layout.Rigid(func(gtx C) D {
									if ap.Tagline == "" {
										return D{}
									}
									return d.para(gtx, 13, 18, wReg, d.p.Muted, 1, oneLine(ap.Tagline))
								}),
							)
						})
					})
				}),
			)
		})
	})
}

// cdAppBadge is c-app_badge: 10px/12.5 bold #454447 on #1d1c1d@0.06,
// padding 1px 3px, radius 2.
func cdAppBadge(b *base, gtx C, label string) D {
	m := op.Record(gtx.Ops)
	td := layout.Inset{Top: 1, Bottom: 1, Left: 3, Right: 3}.Layout(gtx, func(gtx C) D {
		return b.para(gtx, 10, 12.5, wBold, b.p.Sec, 1, label)
	})
	call := m.Stop()
	rrect(gtx, image.Rectangle{Max: td.Size}, gtx.Dp(2), cdPillBg(b))
	call.Add(gtx.Ops)
	return td
}

// ---- Tabs ----

type cdTabRow struct{ icon, label, note string }

var cdTabRows = []cdTabRow{
	{"dms", "Messages", "Always shown"},
	{"files", "Files & links", ""},
	{"pin", "Pins", ""},
	{"workflow", "Workflows", "Hidden"},
}

func (d *ChannelDetails) tabsItems() []W {
	tabRows := cdTabRows
	if cdGroup(d.kind) != "im" {
		if !d.tabList.started() {
			c, id := d.h.Core(), d.id
			d.tabList.load(d.h, false, func(ctx context.Context) ([]core.Tab, error) { return c.Tabs(ctx, id) })
		}
		ts, ok, err := d.tabList.get()
		switch {
		case !ok:
			return []W{cdGap(16), d.cdCard(func(gtx C) D {
				return layout.Inset{Top: 12, Bottom: 12}.Layout(gtx, func(gtx C) D { return d.skeleton(gtx, 4, 20) })
			})}
		case err == nil:
			tabRows = cdTabsRows(ts)
		}
	}
	// Slack's "Manage tabs" card (C0CFT52NEGM, 2026-11-08): title, the
	// description, a 46px row per tab (16px icon, 15px label, "Hidden" at
	// the right for a built-in tab that's off), then "+ New Tab".
	head := func(gtx C) D {
		return layout.Inset{Top: 16, Left: 20, Right: 20, Bottom: 12}.Layout(gtx, func(gtx C) D {
			gtx.Constraints.Min.X = gtx.Constraints.Max.X
			return col(gtx,
				layout.Rigid(func(gtx C) D {
					return cdLine22(gtx, func(gtx C) D { return d.para(gtx, 15, 22, wBold, d.p.Text, 1, "Manage tabs") })
				}),
				layout.Rigid(func(gtx C) D {
					return d.para(gtx, 15, 22, wReg, d.p.Text, 0, "Reorder, add, remove, and hide the tabs that everyone sees in this channel.")
				}),
			)
		})
	}
	rows := []W{head}
	for _, t := range tabRows {
		t := t
		rows = append(rows, func(gtx C) D {
			gtx.Constraints.Min.Y = gtx.Dp(46)
			return layout.Inset{Left: 28, Right: 32}.Layout(gtx, func(gtx C) D {
				return layout.Center.Layout(gtx, func(gtx C) D {
					gtx.Constraints.Min.X = gtx.Constraints.Max.X
					col := d.p.Text
					if t.note == "Hidden" {
						col = d.p.Muted
					}
					return row(gtx, layout.Middle,
						layout.Rigid(func(gtx C) D { return d.icon(gtx, t.icon, 16, col) }),
						hgap(4),
						layout.Flexed(1, func(gtx C) D {
							return dirFill(gtx, func(gtx C) D { return d.label(gtx, 15, wReg, col, t.label) })
						}),
						layout.Rigid(func(gtx C) D {
							if t.note == "" || t.note == "Always shown" {
								return D{}
							}
							return d.label(gtx, 15, wReg, d.p.Muted, t.note)
						}),
					)
				})
			})
		})
	}
	rows = append(rows, func(gtx C) D {
		return layout.Inset{Top: 8, Left: 20, Bottom: 16}.Layout(gtx, func(gtx C) D {
			return cdBtn(&d.base, gtx, &d.newTab, "plus", "New Tab", false, 0)
		})
	})
	card := func(gtx C) D {
		return layout.Inset{Left: cdPadX, Right: cdPadX}.Layout(gtx, func(gtx C) D {
			return cdBox(&d.base, gtx, func(gtx C) D {
				ch := make([]layout.FlexChild, len(rows))
				for i, r := range rows {
					r := r
					ch[i] = layout.Rigid(func(gtx C) D { gtx.Constraints.Min.X = gtx.Constraints.Max.X; return r(gtx) })
				}
				return layout.Flex{Axis: layout.Vertical}.Layout(gtx, ch...)
			})
		})
	}
	out := []W{cdGap(16), card, cdGap(24)}
	// ManageChannelTabs (Wg5J): without move_channel_tabs_settings the
	// "who can manage tabs" field (ManageChannelTabsPermissions, z92f)
	// heads the Tabs tab instead of Settings (not for app channels)
	if cdGroup(d.kind) == "channel" && d.kind != "app" && d.h.Core().Experiment("move_channel_tabs_settings") != "on" {
		if inf, ok, err := d.info.get(); ok && err == nil {
			perms := d.cdCard(func(gtx C) D { return d.tabPermsField(gtx, inf) })
			out = append([]W{cdGap(16), perms}, out...)
		}
	}
	return out
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
	inf, _, _ := d.info.get()
	name := func(gtx C) D {
		if d.editing && d.editKind == cdEditName {
			return d.cdField(gtx, "Channel name", nil, d.topicEditor)
		}
		return d.cdField(gtx, "Channel name", &d.editName, func(gtx C) D {
			return d.para(gtx, 15, 22, wReg, d.p.Text, 1, "#"+inf.Conv.Name)
		})
	}
	return d.slackSettingsItems(inf, name)
}

// selfRoles: is the viewer one of the channel's managers, a workspace
// admin or owner, the channel's creator.
func (d *ChannelDetails) selfRoles() (manager, admin, creator bool) {
	self := d.selfID()
	for _, m := range d.managerIDs() {
		manager = manager || m == self
	}
	if c := d.h.Core(); c != nil {
		if u, ok := c.User(self); ok {
			admin = u.IsAdmin || u.IsOwner
		}
	}
	inf, _, _ := d.info.get()
	creator = self != "" && inf.Conv.Creator == self
	return manager, admin, creator
}

// ---- core writes: star, notifications, invite; managers, counts ----

// isStarred is the star state shown (optimistic while a write runs).
func (d *ChannelDetails) isStarred() bool {
	if d.starPending {
		return d.starOv
	}
	return d.h.Core().Starred(d.id)
}

// notify is the conversation's notification override as shown.
func (d *ChannelDetails) notify() core.ChannelNotify {
	if d.notifPending {
		return d.notifOv
	}
	return d.h.Core().NotifyPrefs(d.id)
}

func (d *ChannelDetails) muted(cv core.Conv) bool {
	if d.notifPending {
		return d.notifOv.Muted
	}
	return d.notify().Muted || cv.Muted
}

// Notification levels of the bell menu (Slack's desktop values).
// Slack's ChannelNotificationPrefMenu (source channel_details): All new
// posts, Just mentions, then Mute and hide (improve_mute_pref) — no "Off".
var (
	cdNotifKeys   = []string{"everything", "mentions_dms"}
	cdNotifLabels = []string{"All new posts", "Just mentions"}
)

// notifLevel is the effective desktop level index (the override, else the
// global default; Just mentions when unknown).
func (d *ChannelDetails) notifLevel() int {
	v := d.notify().Desktop
	if v == "" || v == "default" {
		v = d.h.Core().Prefs().GlobalDesktop
	}
	for i, k := range cdNotifKeys {
		if k == v {
			return i
		}
	}
	return 1
}

// notifLabel is the bell button label: "Mute" when muted, else the level.
func (d *ChannelDetails) notifLabel(cv core.Conv) string {
	if d.muted(cv) {
		return "Mute"
	}
	return cdNotifLabels[d.notifLevel()]
}

// updateWrites handles the star, bell menu / mute, invite and the
// managers / counts loads.
func (d *ChannelDetails) updateWrites(gtx C, inf core.ChannelDetails, loaded bool) {
	c, id := d.h.Core(), d.id
	cv := d.conv(inf, loaded)
	group := cdGroup(d.kind)

	if d.star.Clicked(gtx) && !d.starPending {
		on := !d.isStarred()
		d.starOv, d.starPending = on, true
		d.starOp.load(d.h, false, func(ctx context.Context) (bool, error) { return true, c.Star(ctx, id, on) })
	}
	if d.starPending {
		if _, done, err := d.starOp.get(); done {
			d.starPending = false
			if err != nil {
				verb := "star"
				if !d.starOv {
					verb = "unstar"
				}
				d.h.Toast("Couldn’t " + verb + " this conversation: " + cdErr(err))
			}
		}
	}

	// Bell: channels get the level menu plus Mute; DMs a Mute toggle.
	setNotify := func(n core.ChannelNotify, mute *bool) {
		if d.notifPending {
			return
		}
		d.notifOv, d.notifPending = n, true
		d.notifOp.load(d.h, false, func(ctx context.Context) (bool, error) {
			if mute != nil {
				return true, c.Mute(ctx, id, *mute)
			}
			return true, c.SetNotifyPrefs(ctx, id, n)
		})
	}
	if d.notif.Clicked(gtx) {
		n := d.notify()
		m := !d.muted(cv)
		n.Muted = m
		setNotify(n, &m)
	}
	if group != "im" {
		dd := &d.notifMenu
		mute := "Mute channel"
		if c.Experiment("improve_mute_pref") == "on" {
			mute = "Mute and hide"
		}
		dd.labels = append(append(dd.labels[:0], cdNotifLabels...), mute)
		dd.sel = d.notifLevel()
		if d.muted(cv) {
			dd.sel = len(cdNotifKeys) // the mute radio is the checked one
		}
		if dd.update(gtx) {
			n := d.notify()
			switch pick := dd.sel; {
			case pick < len(cdNotifKeys):
				v := cdNotifKeys[pick]
				if n.Mobile == "" || n.Mobile == "default" || n.Mobile == n.Desktop {
					n.Mobile = v
				}
				n.Desktop, n.Muted = v, false
				setNotify(n, nil)
			default:
				m := !d.muted(cv)
				n.Muted = m
				setNotify(n, &m)
			}
			dd.sel = d.notifLevel()
			if d.muted(cv) {
				dd.sel = len(cdNotifKeys)
			}
		}
	}
	if d.notifPending {
		if _, done, err := d.notifOp.get(); done {
			d.notifPending = false
			if err != nil {
				d.h.Toast("Couldn’t change notifications: " + cdErr(err))
			}
		}
	}

	if group == "channel" && loaded && inf.Conv.ID != "" {
		if !d.managers.started() {
			d.managers.load(d.h, false, func(ctx context.Context) ([]string, error) { return c.Managers(ctx, id) })
		}
		if !d.counts.started() {
			d.counts.load(d.h, false, func(ctx context.Context) (core.MemberCounts, error) { return c.MemberCounts(ctx, id) })
		}
	}

	// Add people.
	if d.addPeopleRow.Clicked(gtx) {
		if group == "channel" {
			d.inviting = !d.inviting
			if d.inviting {
				gtx.Execute(key.FocusCmd{Tag: &d.inviteQ.ed})
			}
		} else if loaded {
			// openAddPeopleModal: IMs and MPIMs get openMpdmAddPeopleModal
			d.openAddPeople(inf)
		}
	}
	if d.inviting {
		q, changed, submitted := d.inviteQ.update(gtx)
		if changed {
			d.inviteDeb.set(q, gtx.Now)
		}
		dq, due := d.inviteDeb.due(gtx, dirDebounce)
		if submitted {
			dq, due = q, true
			d.inviteDeb.sent = q
		}
		if due && dq != "" {
			d.inviteRes.load(d.h, true, func(ctx context.Context) ([]core.User, error) { return c.SearchUsers(ctx, dq, 8) })
		}
	}
	if d.inviteUser != "" && cdGroup(d.kind) != "channel" {
		if conv, done, err := d.dmOp.get(); done {
			uid := d.inviteUser
			d.inviteUser = ""
			if err != nil {
				d.h.Toast("Couldn’t add " + d.userName(uid) + ": " + cdErr(err))
			} else if conv.ID != "" {
				d.inviting = false
				d.closed = true
				d.h.Navigate("c/" + conv.ID)
			}
		}
	}
	if d.inviteUser != "" && cdGroup(d.kind) == "channel" {
		if _, done, err := d.inviteOp.get(); done {
			uid := d.inviteUser
			d.inviteUser = ""
			if err != nil {
				d.h.Toast("Couldn’t add " + d.userName(uid) + ": " + cdErr(err))
			} else {
				d.added[uid] = true
				d.h.Toast("Added " + d.userName(uid) + " to #" + inf.Conv.Name)
				d.loadMembers("")
				d.counts.load(d.h, true, func(ctx context.Context) (core.MemberCounts, error) { return c.MemberCounts(ctx, id) })
			}
		}
	}
}

// managerIDs is the loaded "Managed by" list (nil until loaded or when
// the viewer can't read role assignments).
func (d *ChannelDetails) managerIDs() []string {
	ids, ok, err := d.managers.get()
	if !ok || err != nil {
		return nil
	}
	return ids
}

// managedField: "Managed by" then the managers' names as links.
// showOrgAccess is permissions.manageOrgAccess.shouldShow: on an
// Enterprise Grid org, for a channel (not a DM) that isn't shared outside
// the org.
func (d *ChannelDetails) showOrgAccess(inf core.ChannelDetails) bool {
	c := d.h.Core()
	return c != nil && c.C != nil && c.C.Creds.EnterpriseID != "" && cdGroup(d.kind) == "channel" &&
		!inf.IsExtShared && len(inf.Teams) > 0
}

// workspacesField is Slack's ManageOrgAccess field for a member who can't
// change it (isDisabled): title and text in #1d1c1d@0.7, each workspace's
// 16px icon and name, "Learn more", and the help icon at the right
// (padding 16px 20px; C0CFT52NEGM, 2026-11-08).
func (d *ChannelDetails) workspacesField(gtx C, inf core.ChannelDetails) D {
	dim := d.p.Text
	dim.A = 0xb3
	c := d.h.Core()
	return layout.Inset{Top: 16, Bottom: 16, Left: 20, Right: 20}.Layout(gtx, func(gtx C) D {
		gtx.Constraints.Min.X = gtx.Constraints.Max.X
		return col(gtx,
			layout.Rigid(func(gtx C) D {
				return row(gtx, layout.Start,
					layout.Flexed(1, func(gtx C) D {
						return dirFill(gtx, func(gtx C) D {
							return cdLine22(gtx, func(gtx C) D { return d.para(gtx, 15, 22, wBold, dim, 1, "Workspaces with access to this channel") })
						})
					}),
					layout.Rigid(func(gtx C) D {
						return layout.Inset{Top: 4}.Layout(gtx, func(gtx C) D { return d.icon(gtx, "help", 15, d.p.Text) })
					}),
				)
			}),
			layout.Rigid(func(gtx C) D {
				ch := make([]layout.FlexChild, 0, 3*len(inf.Teams))
				for _, t := range inf.Teams {
					name, icon, _ := c.Team(t, 16)
					if name == "" {
						name = t
					}
					ch = append(ch,
						layout.Rigid(func(gtx C) D {
							s := gtx.Dp(16)
							if icon == "" {
								return D{Size: image.Pt(s, s)}
							}
							return dirImage(&d.base, gtx, icon, image.Pt(s, s), gtx.Dp(4))
						}),
						hgap(4),
						layout.Rigid(func(gtx C) D {
							return layout.Inset{Right: 8}.Layout(gtx, func(gtx C) D { return d.para(gtx, 15, 22, wReg, dim, 1, name) })
						}))
				}
				return cdLine22(gtx, func(gtx C) D { return row(gtx, layout.Middle, ch...) })
			}),
			layout.Rigid(func(gtx C) D {
				return cdLine22(gtx, func(gtx C) D { return cdLinkReg(&d.base, gtx, &d.orgLearn, 15, "Learn more") })
			}),
		)
	})
}

// cdLinkReg is a regular-weight link button (underlined on hover).
func cdLinkReg(b *base, gtx C, c *widget.Clickable, size unit.Sp, label string) D {
	return c.Layout(gtx, func(gtx C) D {
		pointerCursor(gtx)
		d := b.label(gtx, size, wReg, b.p.Link, label)
		if c.Hovered() {
			fill(gtx, image.Rect(0, d.Size.Y-gtx.Dp(2), d.Size.X, d.Size.Y-gtx.Dp(1)), b.p.Link)
		}
		return d
	})
}

func (d *ChannelDetails) managedField(gtx C, ids []string) D {
	return d.cdFieldW(gtx, func(gtx C) D {
		// Slack: "Managed by" and a 15px info icon in the link blue
		return row(gtx, layout.Middle,
			layout.Rigid(func(gtx C) D { return d.para(gtx, 15, 22, wBold, d.p.Text, 1, "Managed by") }),
			hgap(4),
			layout.Rigid(func(gtx C) D { return d.icon(gtx, "info", 15, d.p.Link) }),
		)
	}, func(gtx C) D {
		ch := make([]layout.FlexChild, 0, 2*len(ids))
		for i, uid := range ids {
			c := dirClick(d.rowClk, "mgr:"+uid)
			if c.Clicked(gtx) { // ChannelManagerField: showMemberProfile (vWNA)
				d.h.OpenProfile(d.press.at, uid)
			}
			name := d.userName(uid)
			if i < len(ids)-1 {
				name += ","
			}
			if i > 0 {
				ch = append(ch, hgap(4))
			}
			ch = append(ch, layout.Rigid(func(gtx C) D {
				return c.Layout(gtx, func(gtx C) D {
					pointerCursor(gtx)
					return d.para(gtx, 15, 22, wReg, d.p.Link, 1, name)
				})
			}))
		}
		return row(gtx, layout.Baseline, ch...)
	})
}

// invitePanel is the inline Add people search: a field, then up to 8
// people with Add buttons (members show "In channel").
func (d *ChannelDetails) invitePanel(gtx C, members []core.User) D {
	in := make(map[string]bool, len(members))
	for _, u := range members {
		in[u.ID] = true
	}
	res, loaded, err := d.inviteRes.get()
	return layout.Inset{Top: 4, Bottom: 8, Left: cdPadX, Right: cdPadX}.Layout(gtx, func(gtx C) D {
		gtx.Constraints.Min.X = gtx.Constraints.Max.X
		ch := []layout.FlexChild{
			layout.Rigid(func(gtx C) D {
				return dirSearchField(&d.base, gtx, d.inviteQ, dirFieldStyle{H: 36, Icon: 16, PadL: 8, Gap: 8, Border: d.p.Faint, Over: d.p.Surface}, nil)
			}),
		}
		switch {
		case d.inviteQ.text() == "":
			ch = append(ch, layout.Rigid(func(gtx C) D {
				return layout.Inset{Top: 8}.Layout(gtx, func(gtx C) D {
					return d.para(gtx, 13, 18, wReg, d.p.Muted, 2, "Search for someone to add to this channel.")
				})
			}))
		case err != nil:
			ch = append(ch, layout.Rigid(func(gtx C) D {
				return layout.Inset{Top: 8}.Layout(gtx, func(gtx C) D {
					return d.para(gtx, 13, 18, wReg, d.p.Muted, 2, "Couldn’t search: "+friendlyErr(err))
				})
			}))
		case loaded && len(res) == 0 && !d.inviteRes.busy():
			ch = append(ch, layout.Rigid(func(gtx C) D {
				return layout.Inset{Top: 8}.Layout(gtx, func(gtx C) D { return d.para(gtx, 13, 18, wReg, d.p.Muted, 1, "No one found.") })
			}))
		}
		for _, u := range res {
			ch = append(ch, layout.Rigid(func(gtx C) D { return d.inviteRow(gtx, u, in[u.ID] || d.added[u.ID]) }))
		}
		return col(gtx, ch...)
	})
}

// inviteRow: 36px avatar, name and title, then Add / "In channel".
func (d *ChannelDetails) inviteRow(gtx C, u core.User, member bool) D {
	u = d.viewUser(u)
	c := dirClick(d.rowClk, "inv:"+u.ID)
	if c.Clicked(gtx) && !member && d.inviteUser == "" {
		cl, id, uid := d.h.Core(), d.id, u.ID
		d.inviteUser = uid
		if cdGroup(d.kind) == "channel" {
			d.inviteOp.load(d.h, false, func(ctx context.Context) (bool, error) { return true, cl.Invite(ctx, id, uid) })
		} else {
			d.dmOp.load(d.h, false, func(ctx context.Context) (core.Conv, error) { return cl.AddToDM(ctx, id, uid) })
		}
	}
	return layout.Inset{Top: 8}.Layout(gtx, func(gtx C) D {
		return row(gtx, layout.Middle,
			layout.Rigid(func(gtx C) D { return d.avatar(gtx, u.Avatar, 36) }),
			hgap(12),
			layout.Flexed(1, func(gtx C) D {
				return dirFill(gtx, func(gtx C) D {
					return col(gtx,
						layout.Rigid(func(gtx C) D { return d.para(gtx, 15, 22, wBold, d.p.Text, 1, dirFirst(u.RealName, displayName(u))) }),
						layout.Rigid(func(gtx C) D {
							if u.Title == "" {
								return D{}
							}
							return d.para(gtx, 13, 18, wReg, d.p.Sec, 1, u.Title)
						}),
					)
				})
			}),
			layout.Rigid(func(gtx C) D {
				switch {
				case member:
					return d.label(gtx, 13, wReg, d.p.Muted, "In channel")
				case d.inviteUser == u.ID:
					return d.label(gtx, 13, wReg, d.p.Muted, "Adding…")
				}
				return d.button(gtx, c, btnOutline, true, "Add")
			}),
		)
	})
}

// cdTabsRows turns the channel's tab bar into Tabs-tab rows: Messages
// first, then each tab with an icon for its type.
func cdTabsRows(ts []core.Tab) []cdTabRow {
	out := []cdTabRow{cdTabRows[0]}
	workflows := false
	for _, t := range ts {
		icon := "link"
		switch t.Type {
		case "canvas", "channel_canvas", "list":
			icon = "canvas"
		case "files":
			icon = "files"
		case "bookmarks":
			icon = "pin"
		case "folder":
			icon = "bookmark"
		case "workflows":
			icon, workflows = "workflow", true
		}
		note := ""
		if t.Deleted {
			note = "Deleted"
		}
		out = append(out, cdTabRow{icon, dirFirst(t.Title, t.Label, t.Type), note})
	}
	if !workflows {
		// Slack lists the built-in Workflows tab even when the channel
		// doesn't show it
		out = append(out, cdTabRow{"workflow", "Workflows", "Hidden"})
	}
	return out
}

// cdErr is a short reason for a failed channel write.
func cdErr(err error) string {
	s := err.Error()
	switch {
	case strings.Contains(s, "name_taken"):
		return "that name is already taken"
	case strings.Contains(s, "invalid_name"), strings.Contains(s, "invalid_name_specials"):
		return "names can only contain lowercase letters, numbers, hyphens and underscores"
	case strings.Contains(s, "not_authorized"), strings.Contains(s, "restricted_action"), strings.Contains(s, "cant_"):
		return "you don’t have permission to do that here"
	case strings.Contains(s, "already_in_channel"):
		return "they’re already in the channel"
	}
	return dirErr(err)
}

// dmMembers lists a DM's participants (the viewer, the partner, or the
// group DM's members) for the Add people panel's "already here" marks.
func (d *ChannelDetails) dmMembers(cv core.Conv) []core.User {
	ids := append([]string{d.selfID()}, cv.Members...)
	if cv.User != "" {
		ids = append(ids, cv.User)
	}
	out := make([]core.User, 0, len(ids))
	for _, id := range ids {
		if u, ok := d.user(id); ok {
			out = append(out, u)
		}
	}
	return out
}
