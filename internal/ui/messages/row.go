package messages

import (
	"context"
	"encoding/json"
	"gioui.org/io/event"
	"gioui.org/io/pointer"
	"gioui.org/op/clip"
	"github.com/some-du6e/clack/internal/ui/popups"
	"image"
	"image/color"
	"math"
	"sort"
	"strconv"
	"strings"
	"time"

	"gioui.org/layout"
	"gioui.org/op"
	"gioui.org/op/paint"
	"gioui.org/unit"
	"gioui.org/widget"

	"github.com/some-du6e/clack/internal/core"
	"github.com/some-du6e/clack/internal/i18n"
	"github.com/some-du6e/clack/internal/richtext"
	"github.com/some-du6e/clack/internal/ui/composer"
	"github.com/some-du6e/clack/internal/ui/ctxclick"
	"github.com/some-du6e/clack/internal/ui/hovercard"
	"github.com/some-du6e/clack/internal/ui/icons"
	"github.com/some-du6e/clack/internal/ui/richview"
	"github.com/some-du6e/clack/internal/ui/slackc"
	"github.com/some-du6e/clack/internal/ui/slackc/css"
	"github.com/some-du6e/clack/internal/ui/theme"
	"github.com/some-du6e/clack/internal/ui/uipref"
)

// row is the cached UI state of one message.
//
// A row is Slack's message component tree (docs/PLUGINS-components.md
// §1.1), each part a slackc boundary Taut plugins can patch:
//
//	MessageWrapper | ThreadRootGeneric      layoutWrapper (msg override)
//	└ Message | ThreadReplyGeneric (thread replies)  layoutMessage (msg override)
//	  └ MessageBackground                   layoutBackground
//	    └ MessageKitGutter {light|compact}  layoutGutter
//	      ├ MessageAvatar {BaseMessageAvatar {ConnectedBaseAvatar}}, or the compact Timestamp
//	      └ body column                     layoutColumn
//	        ├ BaseBroadcastPreamble {MessageSender, Timestamp} (threads:
//	        │ ThreadSenderAndTimestampGeneric) → MessageSender → BaseMessageSender
//	        ├ BaseVisuallyHiddenMessageText (screen-reader text: empty)
//	        ├ MessageText → MessageBlocks → Blocks  layoutBlocks (msg overrides)
//	        ├ MessageFiles, MessageKitAttachments   (drawn in the body; msg.files /
//	        │                                         msg.attachments overrides filter it)
//	        ├ ReactionBar {Reaction…, ReactionAddButton}
//	        │ └ Reaction → UnstyledButton data-qa=reactji
//	        ├ ThreadReplies → ReplyBarAvatar…  (the reply bar)
//	        └ CodeChannelEntityCard per channel mentioned (empty, while patched)
//
// Unpatched, every boundary is one atomic load and the natives get nil
// props.
type row struct {
	ts  string
	rev uint32
	bk  bkState      // live Block Kit elements (blockkit.go)
	src core.Message // the message as core has it
	m   core.Message // what the row draws: src, or src with a plugin's msg

	rv      *richview.View
	handler richHandler
	drawn   time.Time // last frame the row was laid out (sweepRows)

	// find in thread (thread_find.go): the query the body marks
	// (findInThreadHighlightWords) and whether the row is the current
	// result (c-message_kit__blocks--focused-result)
	find        string
	findFocused bool

	// a huddle message's room, status and activity (huddle_msg.go),
	// read once per message revision and users generation; hmName: the
	// sender line is the status (system_messages_redesign_m1 off)
	hm     huddleMsg
	hmOK   bool
	hmKey  [2]uint64
	hmName bool
	hmJoin widget.Clickable // the compact Join Huddle (system row)

	// inline edit composer (nil when not editing)
	edit                 *composer.Composer
	editFrames           int
	bodyRect             image.Rectangle // row-local rect of the message body
	retryBtn, discardBtn widget.Clickable

	// resolved author (refreshed when users change)
	uRev      uint64
	memAt     time.Time // last member-view check (patchSlice('members'))
	name      string
	avatar    string
	statusURL string
	huddle    string // the huddle channel while the sender is in one (headphones by the name)
	bot       bool
	badge     string // the sender's app badge (core.SenderBadge: APP, AGENT, WORKFLOW…; "" for people)

	needChan bool   // body mentions channels not resolved yet
	allRich  bool   // richRaw's blocks are all rich_text (Compact: the text runs on after the sender)
	richRaw  []byte // the msg.blocks allRich was read from

	nameBtn, avatarBtn widget.Clickable
	nameHov, avatarHov hovercard.Trigger // Slack's ProfileHoverTrigger on both
	nameCtx, avatarCtx ctxTag            // right-click → the member context menu
	threadBtn          widget.Clickable
	broadcastBtn       widget.Clickable
	labelLink          widget.Clickable // the label's link (broadcastLabel)
	laterLink          widget.Clickable // LaterLabel's "View Saved" / "View to-dos" (laterLabelLine)
	pinnedLink         widget.Clickable // "Pinned by {member}"'s MemberLink (pinnedRuns)
	pinnedHov          hovercard.Trigger
	addReact           widget.Clickable
	groups             []reactGroup // getReactionGroups over m.Reactions (reaction_groups.go), one pill each
	pills              []reactPill
	reactExpand        widget.Clickable // ReactionBar's "N more" / "Show less"
	reactExpanded      bool             // the bar's isExpanded (Slack keeps it per message)
	expandTip          popups.TipState

	// the broadcast's root preview (rootPreview), looked up once per list
	// snapshot
	rootPrev    string
	rootRev     uint64
	rootChecked bool

	// geometry of the last layout (row-local), for overlays
	reactRects []image.Rectangle
	addRect    image.Rectangle
	height     int

	// plugin state (patched path only)
	rc *rowCtx // this frame's inputs, for the natives
	// truncate: in a thread pane, long Block Kit sections cut at "Show
	// more" (richview Options.Truncate)
	truncate bool

	// plugin view of the message (taut patchMessageText…), see applyView
	viewAt           time.Time
	viewRev          uint32
	viewed           bool
	col              image.Point  // body column origin, row-local
	reactAt          image.Point  // reaction bar origin, row-local
	wrapOv, blocksOv uintptr      // msg overrides applied (propsID); 0 = none
	srcMsg, msg      slackc.Props // slackMessage of src and of m, built lazily
	reactAs          string       // ReactionBar's currentUser override ("" = the viewer)

	msgOv           uintptr      // Message / ThreadReplyGeneric msg override applied
	wrapMsg         slackc.Props // MessageWrapper's override (what msgOv falls back to)
	textOv          slackc.Props // MessageText's msg override (nil = none)
	filesOv, attsOv slackc.Props // MessageFiles / MessageKitAttachments msg overrides
	tailOv          [2]uintptr   // file / attachment filters applied (propsIDs)
	hasFiles        bool         // the body has file cards (MessageFiles)
	hasAtts         bool         // … attachment cards (MessageKitAttachments)
	chans           []chanCard   // CodeChannelEntityCard per channel mentioned, while patched
	chansDoc        bool         // chans is for the current doc
	replyAvs        []replyAv    // the reply bar's ReplyBarAvatars
	replyN          int          // … drawn in the last frame
	replyLink       string       // … its count link's text (getLinkTextWithScheduledDrafts), last frame
	// the thread's draft and scheduled replies (reply_bar.go threadDrafts)
	draftGen            uint64
	draftSeen, hasDraft bool
	scheduled           int
	draftAt             string // the draft's last_updated
	schedAt             int64  // the first scheduled reply's date_scheduled

	// Slackbot custom response drawn as the team (custom_response.go)
	custom         bool
	crName, crIcon string
	crTag          crHoverTag
	crHovered      bool
	crTip          popups.TipState
	crTipText      string // the badge tooltip's text while hovered

	// what the header drew last frame (CSS may replace both)
	compactBL  int         // the compact time's baseline, gutter-local (tests)
	nameBL     int         // the sender line's baseline, column-local (tests)
	nameX      int         // the sender's x, column-local (Compact: -8)
	unreadMark bool        // the reply bar drew the "Unread" mark (tests)
	avDrawn    string      // the avatar's picture: a CSS background-image wins
	bgDrawn    color.NRGBA // MessageBackground's fill (labels' tints, hover)
	nameShown  string      // the sender label: ::before + name (unless font-size 0) + ::after
}

// chanCard is one CodeChannelEntityCard under a message.
type chanCard struct {
	r  *row
	id string
}

// replyAv is one ReplyBarAvatar in the reply bar.
type replyAv struct {
	r     *row
	id    string
	sz    int
	w     int    // width drawn last frame (0: a plugin hid it)
	drawn string // the picture drawn last frame (tests)
	team  bool   // … the team icon (a Slackbot custom response in the thread)
}

// reactPill is one reaction group's state (getReactionGroups: a pill for
// every skin tone of one emoji): Reaction / UnstyledButton natives.
type reactPill struct {
	r   *row
	i   int // the group, r.groups[i]
	btn widget.Clickable
}

// rowCtx carries per-frame inputs to a row layout.
type rowCtx struct {
	h         Host
	th        *theme.Theme
	st        *richview.Style
	c         *colors
	header    bool // first of a group: avatar + name line
	hovered   bool
	thread    bool // inside a thread pane (no thread bar, no "replied to")
	dense     bool // messages_theme "dense" (Messages & media > Theme > Compact)
	persist   bool // Simplified layout mode's persistent message actions (persist.go)
	list      *listState
	parent    bool // the thread parent row
	now       time.Time
	selfID    string
	usersRev  uint64
	origin    func(image.Point) image.Point
	winOrigin image.Point // the row's top-left in window coordinates (last frame)
	onRetry   func(r *row)
	onDiscard func(r *row)
	highlight float32 // 0..1 flash for jump targets
	// findFlash is find in thread's flash on its result (0..1 of
	// --dt_color-base-hgl-3: p-threads_flexpane__find_flash)
	findFlash float32
	onReact   func(r *row, name string, add bool)
	// onReactGroup is a pill's click: handleReactionClick with the
	// group's name (reaction_groups.go toggleTargets)
	onReactGroup func(r *row, base string)
	// activeThread: the row is the root of the thread open in the side pane
	// (thread_root_selected_state, active_thread.go)
	activeThread bool
}

// padX is the gutter's side padding: 20 in a channel, 16 in a thread
// (mt). contentX is where the body column starts: padX + 36 + 8; under
// Compact 92 in a channel (the 56px time, its 8px margin and the indent)
// and padX in a thread, whose gutter has no left side (MessageKitGutter
// draws only gutter__right for "dense" without a compact slot, and
// ThreadReply hands one only to the Threads view).
func (rc *rowCtx) padX() float32 {
	if rc.thread {
		return mt.padX
	}
	return mt.chanPadX
}

func (rc *rowCtx) contentX() float32 {
	switch {
	case rc.dense && rc.thread:
		return mt.padX
	case rc.dense:
		return mt.denseX
	case rc.thread:
		return mt.contentX
	}
	return mt.chanContentX
}

// denseChan is a channel row under Compact.
func (rc *rowCtx) denseChan() bool { return rc.dense && !rc.thread }

// noAvatar: the row has no avatar column (Compact).
func (rc *rowCtx) noAvatar() bool { return rc.dense }

func newRow(e *entry) *row { return newListRow(e, false) }

// newListRow is a row of a list; thread rows truncate long Block Kit
// sections behind "Show more" like Slack's thread pane (a channel never
// does).
func newListRow(e *entry, thread bool) *row {
	r := &row{ts: e.m.TS, truncate: thread}
	r.set(e)
	return r
}

func (r *row) set(e *entry) {
	r.rev = e.rev
	r.src, r.m = e.m, e.m
	r.wrapOv, r.blocksOv, r.srcMsg, r.msg = 0, 0, nil, nil
	r.msgOv, r.wrapMsg, r.textOv, r.filesOv, r.attsOv = 0, nil, nil, nil, nil
	r.setDoc()
	r.growPills()
}

// setDoc shows r.m's body (after set or a msg override).
func (r *row) setDoc() {
	opts := r.rvOpts()
	doc := r.m.Doc
	if r.find != "" {
		doc = richtext.FindHighlighted(doc, []string{r.find})
	}
	if r.rv == nil {
		r.rv = richview.New(doc, opts)
	} else {
		r.rv.Set(doc, opts)
	}
	r.uRev = 0
	r.blocksOv, r.tailOv = 0, [2]uintptr{}
	r.needChan = blocksNeedChan(r.m.Doc.Blocks)
	r.hasFiles, r.hasAtts = docTail(r.m.Doc.Blocks)
	r.chansDoc = false
}

// rvOpts are the body's richview options.
func (r *row) rvOpts() richview.Options {
	own := r.rc != nil && r.rc.selfID != "" && r.m.User == r.rc.selfID && r.m.Subtype != "pinned_item" && !r.m.Pending
	o := richview.Options{Edited: r.m.Edited, Muted: r.m.System, Dim: r.dimPending(), Channel: r.m.Channel, TS: r.m.TS, Truncate: r.truncate, Own: own,
		FindFocused: r.findFocused}
	if r.m.Edited {
		o.EditedTS = r.m.EditedTS // the "(edited)" label's tooltip: the edit's time
		o.EditedBy = r.editedBy()
	}
	return o
}

// editedBy is the name EditedLabel's tooltip adds ("{dateTime} by
// {name}"): getMessageLabelValues (boot-shared XSZB) passes
// editedByUserId only for a workflow message (workflow_id), as
// edited.user; the name is getMemberNameById's. "" otherwise, and while
// the member isn't known yet.
func (r *row) editedBy() string {
	id := r.m.EditedBy
	if id == "" || r.m.WorkflowID == "" || r.rc == nil || r.rc.h == nil {
		return ""
	}
	app := r.rc.h.Core()
	if app == nil {
		return ""
	}
	if u, ok := app.User(id); ok && u.Name != "" {
		return u.Name
	}
	ensureUser(r.rc.h, id)
	return ""
}

// dimPending reports whether a pending row is drawn at .5. Slack's
// placeholder for a message with files (appendPlaceholderMessage, drawn by
// PendingFilesAtom) isn't faded: only the tiles of uploads still in
// flight are (richview), so the row doesn't flash when Slack confirms it.
func (r *row) dimPending() bool {
	if !r.m.Pending {
		return false
	}
	files, _ := docTail(r.m.Doc.Blocks) // not r.hasFiles: setDoc asks before it sets it
	return !files
}

func (r *row) growPills() {
	r.groups = groupReactions(r.groups, r.m.Reactions)
	if n := len(r.groups); len(r.pills) < n {
		r.pills = append(r.pills, make([]reactPill, n-len(r.pills))...)
	}
	for i := range r.pills {
		r.pills[i].r, r.pills[i].i = r, i
	}
}

func (r *row) resolve(rc *rowCtx) {
	t := tautOf()
	memPatched := t != nil && r.m.User != "" && t.SlicePatched("members")
	if r.uRev == rc.usersRev && r.uRev != 0 && !(memPatched && rc.now.Sub(r.memAt) >= viewRefresh) {
		return
	}
	r.uRev, r.memAt = rc.usersRev, rc.now
	m := &r.m
	if r.needChan {
		doc := m.Doc
		doc.Blocks = fixChannels(rc.h, doc.Blocks)
		r.needChan = blocksNeedChan(doc.Blocks)
		r.rv.Set(doc, r.rvOpts())
		r.blocksOv, r.tailOv = 0, [2]uintptr{}
	} else if m.Edited && m.WorkflowID != "" && m.EditedBy != "" {
		r.rv.SetOptions(r.rvOpts()) // EditedLabel's editor, named once core knows them
	}
	// Slack's sender: a message with a user is that member's (name,
	// picture, badge); m.Name/m.Avatar only name user-less senders
	// (core/sender.go).
	r.name, r.avatar, r.statusURL, r.bot = m.Name, m.Avatar, "", m.IsBot || (m.BotID != "" && m.User == "")
	r.huddle = ""
	if m.User != "" {
		r.name, r.avatar = "", ""
	}
	coreName := ""
	r.badge = ""
	if rc.h.Core() != nil {
		r.badge = rc.h.Core().SenderBadge(m.User, m.BotID)
	} else if r.bot {
		r.badge = core.BadgeApp
	}
	if m.User != "" && rc.h.Core() != nil {
		if u, ok := rc.h.Core().User(m.User); ok {
			coreName = u.Name
			if coreName == "" {
				coreName = u.RealName
			}
			if r.name == "" {
				r.name = u.Name
				if r.name == "" {
					r.name = u.RealName
				}
			}
			// Messages & media > Names: "Full & display names"
			// (display_real_names_override) shows people's full names.
			if uipref.Get().DisplayRealNames && !u.IsBot && u.RealName != "" && (m.Name == "" || m.Name == coreName) {
				r.name = u.RealName
			}
			if r.avatar == "" {
				r.avatar = u.Avatar
			}
			r.statusURL = u.StatusEmojiURL
			// qeNM: in_a_huddle with a channel and an unexpired state.
			if u.Huddle && u.HuddleChannel != "" && u.HuddleExpires > 0 && time.Unix(u.HuddleExpires, 0).After(rc.now) {
				r.huddle = u.HuddleChannel
			}
			if u.IsBot {
				r.bot = true
			}
		} else {
			ensureUser(rc.h, m.User)
		}
	}
	// plugins' member patches (patchSlice('members'): Nicknames…), as
	// Slack's sender reads the member from the store; a message's own
	// username (bots) still wins.
	if memPatched && (m.Name == "" || m.Name == coreName) {
		if mf, ok := t.MemberView(m.User); ok {
			if mf.Name != "" {
				r.name = mf.Name
			}
			if mf.Avatar != "" {
				r.avatar = mf.Avatar
			}
		}
	}
	if r.name == "" {
		if m.User != "" {
			r.name = m.User
		} else {
			r.name = "Unknown"
		}
	}
	// getMsgSenderEntity's huddle sender: the status line, no badge, no
	// profile, no custom status (huddle_msg.go)
	r.hmName = false
	if hm, ok := r.huddleInfo(rc); ok && !m.System {
		r.name, r.avatar, r.badge, r.statusURL, r.huddle, r.hmName = hm.status, "", "", "", "", true
	}
}

// huddleInfo is the row's huddle message state (huddle_msg.go), cached
// per message revision and users generation.
func (r *row) huddleInfo(rc *rowCtx) (huddleMsg, bool) {
	if !isHuddleMsg(&r.m) {
		return huddleMsg{}, false
	}
	k := [2]uint64{uint64(r.rev), rc.usersRev}
	if k != r.hmKey || !r.hmOK {
		r.hm, r.hmOK = huddleOf(rc.h.Core(), &r.m)
		r.hmKey = k
	}
	return r.hm, r.hmOK
}

var askedUsers = map[string]bool{}

// ensureUser asks core for a missing user once (UI goroutine only).
func ensureUser(h Host, id string) {
	if askedUsers[id] || h.Core() == nil {
		return
	}
	askedUsers[id] = true
	app := h.Core()
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
		defer cancel()
		app.EnsureUsers(ctx, []string{id})
		h.Invalidate()
	}()
}

// layout draws the row at full width and returns its size.
func (r *row) layout(gtx layout.Context, rc *rowCtx) layout.Dimensions {
	r.rc = rc
	if rc.parent {
		return slackc.Component(gtx, "ThreadRootGeneric", r, (*row).wrapperProps, (*row).layoutWrapper)
	}
	return slackc.Component(gtx, "MessageWrapper", r, (*row).wrapperProps, (*row).layoutWrapper)
}

// ---- props (patched path only) ----

func (r *row) viewContext() string {
	if r.rc.thread {
		return "threads_flexpane"
	}
	return "message_pane"
}

// message is the msg prop of the parts inside MessageWrapper: the message
// as the wrapper's (possibly overridden) msg left it.
func (r *row) message() slackc.Props {
	if r.wrapOv == 0 && r.msgOv == 0 {
		return r.sourceMessage()
	}
	if r.msg == nil {
		r.msg = slackMessage(&r.m)
	}
	return r.msg
}

func (r *row) sourceMessage() slackc.Props {
	if r.srcMsg == nil {
		r.srcMsg = slackMessage(&r.src)
	}
	return r.srcMsg
}

func (r *row) wrapperProps() slackc.Props {
	p := slackc.Props{"$key": r.ts, "msg": r.sourceMessage(), "channelId": r.src.Channel, "viewContext": r.viewContext()}
	if r.rc.parent {
		p["isSingleThread"], p["omitGutter"], p["omitSenderAndTimestamp"] = true, false, false
	}
	return r.pane(p)
}

// ownMsg reports whether msg is a msg prop the row built (handed back
// unchanged, before JS answered).
func (r *row) ownMsg(msg slackc.Props) bool {
	id := propsID(msg)
	return id == 0 || id == propsID(r.srcMsg) || id == propsID(r.msg)
}

func (r *row) messageProps() slackc.Props {
	return r.pane(slackc.Props{"$key": r.ts, "msg": r.message(), "channelId": r.src.Channel, "viewContext": r.viewContext()})
}

// replyProps are ThreadReplyGeneric's (a reply in the thread pane).
func (r *row) replyProps() slackc.Props {
	return r.pane(slackc.Props{"$key": r.ts, "msg": r.message(), "channelId": r.src.Channel, "parentId": r.src.ThreadTS,
		"adjacent": !r.rc.header, "className": "c-message_kit__background c-message_kit__message c-message_kit__thread_message",
		"blocksContainerContext": "message", "messageContainerType": "thread", "viewContext": r.viewContext(),
		"highlight": r.rc.highlight > 0, "focused": false, "isCompact": false, "isInView": true, "isSingleThread": false,
		"hasContextMenu": true})
}

func (r *row) backgroundProps() slackc.Props {
	cls := "c-message_kit__background c-message_kit__message"
	if r.rc.hovered {
		cls += " c-message_kit__background--hovered"
	}
	return r.pane(slackc.Props{"$key": r.ts, "msg": r.message(), "className": cls, "hover": r.rc.hovered,
		"highlight": r.rc.highlight > 0, "isEditing": r.edit != nil, "viewContext": r.viewContext(), "windowId": "main"})
}

func (r *row) gutterProps() slackc.Props {
	p := slackc.Props{"$key": r.ts, "msg": r.message()}
	switch {
	case r.rc.denseChan():
		// Message: under Compact every row's gutter is getCompactGutter
		// (the time; MessageKitGutter shows the compact slot for "dense")
		p["compact"] = slackc.Child("Timestamp", r.compactProps("c-timestamp p-message_pane_message__compact_timestamp"), r.layoutCompactTS)
	case r.rc.header:
		p["light"] = slackc.Child("MessageAvatar", r.avatarSlotProps(), r.layoutMessageAvatar)
	case r.rc.thread:
		p["compact"] = slackc.Child("ThreadCompactGutterGeneric", r.compactProps("p-thread_compact_gutter_generic"), r.layoutCompactTS)
	default:
		p["compact"] = slackc.Child("Timestamp", r.compactProps("c-timestamp p-message_pane_message__compact_timestamp"), r.layoutCompactTS)
	}
	return r.pane(p)
}

// avatarSlotProps are MessageAvatar's. Its child is BaseMessageAvatar,
// whose child is ConnectedBaseAvatar, so context either provides
// (ShowSendingBot's sender) reaches the avatar.
func (r *row) avatarSlotProps() slackc.Props {
	return r.pane(slackc.Props{"$key": r.ts, "msg": r.message(), "userId": r.m.User, "botId": r.m.BotID,
		"children": []any{slackc.Child("BaseMessageAvatar", r.baseAvatarProps(), r.layoutBaseMessageAvatar)}})
}

func (r *row) baseAvatarProps() slackc.Props {
	p := slackc.Props{"$key": r.ts, "msg": r.message(), "userId": r.m.User, "showCard": true, "isMpdm": false, "isAppOrBotJoiner": false,
		"children": []any{slackc.Child("ConnectedBaseAvatar", r.avatarProps(), r.layoutAvatar)}}
	if r.m.BotID != "" {
		p["botId"] = r.m.BotID
	}
	return r.pane(p)
}

func (r *row) compactProps(cls string) slackc.Props {
	return r.pane(slackc.Props{"$key": r.ts, "className": cls, "timestamp": r.m.TS, "clickable": true, "visible": r.compactVisible(), "msg": r.message()})
}

// compactVisible: the gutter's time shows on a grouped row while it's
// hovered (.p-message_pane_message__compact_timestamp--adjacent is
// opacity 0 until .c-message_kit__hover--hovered); under Compact a row
// that starts a group always shows it.
func (r *row) compactVisible() bool {
	return r.rc.hovered || (r.rc.denseChan() && r.rc.header)
}

func (r *row) avatarProps() slackc.Props {
	p := slackc.Props{"$key": r.ts, "userId": r.m.User, "size": mt.avatar, "className": "c-message_kit__avatar",
		"isInteractive": true, "showCard": true, "messageTs": r.m.TS, "channelId": r.m.Channel}
	if r.m.BotID != "" {
		p["botId"] = r.m.BotID
		if r.m.Avatar != "" {
			p["botProfile"] = slackc.Props{"id": r.m.BotID, "name": r.m.Name, "icons": slackc.Props{"image_36": r.m.Avatar, "image_48": r.m.Avatar, "image_72": r.m.Avatar}}
		}
	}
	return r.pane(p)
}

func (r *row) preambleProps() slackc.Props {
	if r.rc.denseChan() { // getCompactMessageBody: the sender only, the time is the gutter's
		return r.pane(slackc.Props{"$key": r.ts, "msg": r.message(), "shouldRender": true, "children": []any{
			slackc.Child("MessageSender", r.senderProps(), r.layoutSender),
		}})
	}
	return r.pane(slackc.Props{"$key": r.ts, "msg": r.message(), "shouldRender": true, "children": []any{
		slackc.Child("MessageSender", r.senderProps(), r.layoutSender),
		slackc.Child("Timestamp", r.timestampProps(), r.layoutPreambleTimestamp),
	}})
}

// layoutPreambleTimestamp is the Timestamp slot of a patched
// BaseBroadcastPreamble: the plugin's tree lays the slots out inline, so the
// slot keeps nameLine's gap after the sender itself.
func (r *row) layoutPreambleTimestamp(gtx layout.Context, p slackc.Props) layout.Dimensions {
	gap := dp(gtx, mt.tsGap)
	g := gtx
	g.Constraints.Min.X = 0
	g.Constraints.Max.X = max(0, gtx.Constraints.Max.X-gap)
	t := op.Offset(image.Pt(gap, 0)).Push(gtx.Ops)
	d := r.layoutTimestamp(g, p)
	t.Pop()
	if d.Size.X == 0 {
		return d
	}
	d.Size.X += gap
	return d
}

func (r *row) threadSenderProps() slackc.Props {
	return r.pane(slackc.Props{"$key": r.ts, "msg": r.message(), "adjacent": false, "visible": true, "omitTimestamp": false,
		"omitLinebreak": false, "keepSenderAccessible": false, "timestampLabel": clock(tsTime(r.m.TS))})
}

func (r *row) senderProps() slackc.Props {
	return r.pane(slackc.Props{"$key": r.ts, "msg": r.message(), "isInteractive": true, "visible": true, "viewContext": r.viewContext()})
}

func (r *row) baseSenderProps() slackc.Props {
	p := slackc.Props{"$key": r.ts, "className": "c-message__sender c-message_kit__sender", "isInteractive": true, "msg": r.message()}
	if r.m.User != "" {
		p["userId"] = r.m.User
	}
	if r.m.BotID != "" {
		p["botId"] = r.m.BotID
	}
	return r.pane(p)
}

func (r *row) timestampProps() slackc.Props {
	return r.pane(slackc.Props{"$key": r.ts, "timestamp": r.m.TS, "clickable": true, "visible": true, "className": "c-timestamp"})
}

// bodyMessage is the msg of the parts inside MessageText: its override,
// or the row's.
func (r *row) bodyMessage() slackc.Props {
	if r.textOv != nil {
		return r.textOv
	}
	return r.message()
}

func (r *row) blocksProps() slackc.Props {
	return r.pane(slackc.Props{"$key": r.ts, "msg": r.bodyMessage(), "blocksContainerContext": "message", "inline": false, "streaming": false})
}

func (r *row) textProps() slackc.Props {
	return r.pane(slackc.Props{"$key": r.ts, "msg": r.message(), "isThreadRoot": r.rc.parent, "isSystemMessageRow": r.m.System,
		"inline": false, "viewContext": r.viewContext()})
}

func (r *row) hiddenTextProps() slackc.Props {
	return r.pane(slackc.Props{"$key": r.ts, "msg": r.message()})
}

func (r *row) tailProps() slackc.Props {
	return r.pane(slackc.Props{"$key": r.ts, "msg": r.bodyMessage(), "blocksContainerContext": "message", "viewContext": r.viewContext()})
}

func (r *row) repliesProps() slackc.Props {
	return r.pane(slackc.Props{"$key": r.ts, "msg": r.message(), "channelId": r.src.Channel, "messageContainerType": "message", "unreadThreadsCount": 0})
}

func (av *replyAv) props() slackc.Props {
	r := av.r
	return r.pane(slackc.Props{"$key": r.ts + "/" + av.id, "userId": av.id, "channelId": r.src.Channel, "threadTs": r.src.TS})
}

func (cc *chanCard) props() slackc.Props {
	r := cc.r
	return r.pane(slackc.Props{"$key": r.ts + "/" + cc.id, "channelId": cc.id, "borderless": false, "joinableFromChannelId": r.src.Channel})
}

func (r *row) reactionBarProps() slackc.Props {
	m := &r.m
	kids := make([]any, 0, len(r.groups)+1)
	for i := range r.groups {
		pl := &r.pills[i]
		kids = append(kids, slackc.Child("Reaction", pl.props(), pl.layout))
	}
	kids = append(kids, slackc.Child("ReactionAddButton", r.addButtonProps(), r.layoutAddButton))
	return r.pane(slackc.Props{"$key": r.ts, "reactionKey": reactionKey(m), "reactions": slackReactions(m.Reactions),
		"currentUser": r.rc.selfID, "channelId": m.Channel, "className": "c-message_kit__reaction_bar c-reaction_bar",
		"canReact": true, "children": kids})
}

func (r *row) addButtonProps() slackc.Props {
	return r.pane(slackc.Props{"$key": r.ts, "channelId": r.m.Channel, "shouldShowReacjiTriggers": false})
}

// props are Reaction's: ReactionBar hands each one its group's name,
// count, users and reactions (getReactionGroups).
func (pl *reactPill) props() slackc.Props {
	r := pl.r
	g := &r.groups[pl.i]
	rs := make([]any, len(g.idx))
	for k, i := range g.idx {
		rs[k] = slackReaction(&r.m.Reactions[i])
	}
	p := slackc.Props{"name": g.name, "count": g.count, "users": strList(groupUsers(r.m.Reactions, g)), "reactions": rs}
	p["$key"] = r.ts + ":" + g.name
	p["currentUser"], p["reactionKey"], p["canReact"] = r.rc.selfID, reactionKey(&r.m), true
	// the pill button renders inside Reaction's React tree, so context a
	// Reaction patch provides (WhoReacted's reactors) reaches the button
	p["children"] = []any{slackc.Child("UnstyledButton", pl.buttonProps(), pl.layoutButton)}
	return pl.r.pane(p)
}

// buttonProps are the pill's UnstyledButton: Reaction's children are the
// group's emoji (the first ten), "…" past ten, and the count.
func (pl *reactPill) buttonProps() slackc.Props {
	r := pl.r
	g := &r.groups[pl.i]
	cls := "c-button-unstyled c-reaction"
	self := pl.reacted()
	if self {
		cls += " c-reaction--reacted"
	}
	kids := make([]any, 0, min(len(g.idx), reactEmojiMax)+2)
	for _, i := range g.idx[:min(len(g.idx), reactEmojiMax)] {
		kids = append(kids, slackc.Child("Emoji", slackc.Props{"emoji": r.m.Reactions[i].Name, "size": mt.reactEmoji}, pl.layoutEmoji))
	}
	if len(g.idx) > reactEmojiMax {
		kids = append(kids, slackc.Child("ReactionTruncation", slackc.Props{"data-qa": "reaction-truncation", "className": "c-reaction__truncation"}, pl.layoutTruncation))
	}
	kids = append(kids, slackc.Child("ReactionCount", slackc.Props{"count": g.count}, pl.layoutCount))
	return pl.r.pane(slackc.Props{"$key": r.ts + ":" + g.name, "data-qa": "reactji", "className": cls, "aria-pressed": self,
		"aria-label": reactionAria(&core.Reaction{Name: g.name, Count: g.count}), "children": kids})
}

// reactEmojiMax is how many of a group's emoji Reaction draws before its
// "…" (9FoR: d.length > 10).
const reactEmojiMax = 10

// ---- natives ----

// layoutWrapper is MessageWrapper / ThreadRootGeneric: a plugin's msg
// (ShowRealUser) replaces the sender, avatar, badge and blocks drawn.
func (r *row) layoutWrapper(gtx layout.Context, p slackc.Props) layout.Dimensions {
	rc := r.rc
	if msg := p.Map("msg"); propsID(msg) != r.wrapOv {
		r.wrapOv = propsID(msg)
		if r.wrapOv == propsID(r.srcMsg) { // our own props, before JS answered
			r.wrapOv = 0
		}
		r.wrapMsg = nil
		if r.wrapOv == 0 {
			r.m = r.src
		} else {
			r.wrapMsg = msg
			r.m = applySlackMessage(rc.h.Core(), &r.src, msg)
		}
		r.msgOv = 0 // the inner override re-applies over the new base
		r.msg = nil
		r.setDoc()
		r.growPills()
	}
	r.resolve(rc)
	r.customResponse(rc)
	switch {
	case rc.parent:
		return r.layoutMessage(gtx, nil)
	case rc.thread:
		return slackc.Component(gtx, "ThreadReplyGeneric", r, (*row).replyProps, (*row).layoutMessage)
	}
	return slackc.Component(gtx, "Message", r, (*row).messageProps, (*row).layoutMessage)
}

// layoutMessage is Message (a reply in a thread pane: ThreadReplyGeneric):
// a plugin's msg replaces what the row draws, like MessageWrapper's.
func (r *row) layoutMessage(gtx layout.Context, p slackc.Props) layout.Dimensions {
	rc := r.rc
	if p != nil || r.msgOv != 0 {
		msg := p.Map("msg")
		id := propsID(msg)
		if r.ownMsg(msg) {
			id = 0
		}
		if id != r.msgOv {
			r.msgOv = id
			switch {
			case id != 0:
				r.m = applySlackMessage(rc.h.Core(), &r.src, msg)
			case r.wrapMsg != nil:
				r.m = applySlackMessage(rc.h.Core(), &r.src, r.wrapMsg)
			default:
				r.m = r.src
			}
			r.msg = nil
			r.setDoc()
			r.growPills()
			r.resolve(rc)
		}
	}
	if r.m.Tombstone {
		return r.tombstoneLayout(gtx, rc)
	}
	if r.m.System && (!rc.parent || huddleSubtype(r.m.Subtype)) {
		// ThreadRootGeneric draws a huddle root as a redesigned system
		// message too (huddle_msg.go)
		return r.systemLayout(gtx, rc)
	}
	return slackc.Component(gtx, "MessageBackground", r, (*row).backgroundProps, (*row).layoutBackground)
}

// layoutBackground is MessageBackground: the hover/pinned/editing/highlight
// fill under the labels and the gutter.
func (r *row) layoutBackground(gtx layout.Context, p slackc.Props) layout.Dimensions {
	rc := r.rc
	c := rc.c
	m := &r.m
	w := gtx.Constraints.Max.X
	cx := dp(gtx, rc.contentX())
	padTop := mt.padTop
	if rc.parent {
		padTop = 20
	}
	var mods uint64
	if rc.hovered {
		mods |= 1
	}
	if rc.thread {
		mods |= 2
		if rc.parent {
			mods |= 4
		}
	}
	if m.SlackbotCustomResponse() {
		mods |= 8
	}
	st := msgClass.EnterClass(gtx, hoverState(rc.hovered), mods, slackc.ExtraClass(p))
	defer st.Leave()
	if st.Hidden() {
		r.height = 0
		return layout.Dimensions{}
	}

	// Record content first; the background depends on the height.
	rec := op.Record(gtx.Ops)
	var opac paint.OpacityStack
	dim := r.dimPending()
	if dim {
		opac = paint.PushOpacity(gtx.Ops, 0.5)
	}
	y := dp(gtx, padTop) - dp(gtx, 4)

	// MessageLabels (boot-shared meLQ) draws one primary label: ephemeral,
	// then a broadcast, then pinned, then Later (a saved message that
	// isn't completed or archived, actions_rules.go laterLabelFor), then a
	// custom response. Message (the channel row), ThreadRoot and
	// ThreadReplyGeneric all wrap the row in it, so the thread pane shows
	// pinned labels too; only ThreadReplyGeneric passes showBroadcast. A
	// custom response gets no label line ("custom_response" ===
	// primaryLabelType renders none), only the row's tint.
	eph := ephemeral(rc, m)
	broadcast := rc.thread && !rc.parent && m.Broadcast
	var later bool
	switch {
	case eph:
		y += r.labelLine(gtx, rc, cx, y, "eye", "Only visible to you")
	case broadcast:
		y += r.broadcastLabel(gtx, rc, y)
	case m.Pinned:
		y += r.labelRuns(gtx, rc, y, "pin", r.pinnedRuns(gtx, rc, nil)...)
	default:
		if ll, ok := laterLabelFor(rc.h.Core(), m, rc.now); ok {
			later = true
			y += r.laterLabelLine(gtx, rc, y, ll)
		}
	}
	r.col = image.Pt(cx, y)
	g := at(gtx, 0, y, func(gtx layout.Context) layout.Dimensions {
		return slackc.Component(gtx, "ProgressiveDisclosureSuccessCoachmarkWrapper", r, (*row).coachmarkProps, (*row).layoutCoachmark)
	})
	y += g.Size.Y
	if dim {
		opac.Pop()
	}
	content := rec.Stop()

	h := y + dp(gtx, mt.padBottom) - dp(gtx, 8)
	if rc.header && !rc.noAvatar() {
		if mh := dp(gtx, padTop) + dp(gtx, mt.avatar) + dp(gtx, mt.padBottom); h < mh {
			h = mh
		}
	}
	size := image.Pt(w, h)
	bg := c.Bg
	bg.A = 0
	switch {
	case r.edit != nil:
		bg = c.Editing
	case rc.activeThread: // .p-message_pane_message__active_thread, after the hover and label rules
		bg = c.ActiveThread
	// c-message_kit__background--labels: any primary label but ephemeral
	// and custom_response (which have their own) tints the row
	case (m.Pinned || broadcast) && rc.hovered:
		bg = c.PinnedHover
	case m.Pinned || broadcast:
		bg = c.Pinned
	// --labels--custom_response, --labels--later and --labels--later_todo
	// {background:#1d9bd11a} come after --hovered in Slack's stylesheet,
	// so the tint stays under the pointer
	case m.SlackbotCustomResponse() && !eph, later:
		bg = c.CustomTint
	case rc.hovered, eph: // ephemeral: rgba(29,28,29,.04) (c-message_kit__background--labels--ephemeral)
		bg = c.Hover
	}
	bg = st.Background(bg)
	r.height, r.bgDrawn = h, bg
	if st.Active() {
		return st.Wrap(gtx, func(gtx layout.Context) layout.Dimensions { return r.paintBackground(gtx, content, size, bg) })
	}
	return r.paintBackground(gtx, content, size, bg)
}

func (r *row) paintBackground(gtx layout.Context, content op.CallOp, size image.Point, bg color.NRGBA) layout.Dimensions {
	fill(gtx.Ops, image.Rectangle{Max: size}, bg)
	if hl := r.rc.highlight; hl > 0 {
		c := r.rc.c.Highlight
		c.A = uint8(float32(c.A) * hl)
		fill(gtx.Ops, image.Rectangle{Max: size}, c)
	}
	if fl := r.rc.findFlash; fl > 0 {
		c := r.rc.c.ActiveThread
		c.A = uint8(float32(c.A) * fl)
		fill(gtx.Ops, image.Rectangle{Max: size}, c)
	}
	content.Add(gtx.Ops)
	return layout.Dimensions{Size: size}
}

// layoutGutter is the gutter memo (filter-matched by its source marker
// c-message_kit__gutter__left): the avatar column, from its light/compact
// slots, and the body column.
func (r *row) layoutGutter(gtx layout.Context, p slackc.Props) layout.Dimensions {
	w := gtx.Constraints.Max.X
	padX := dp(gtx, r.rc.padX())
	cx := dp(gtx, r.rc.contentX())
	// The column's right edge: the gutter's right padding less
	// gutter__right's -8px margin plus its padding-right (8 in a channel:
	// w-20; 0 in a thread, padding-right 24: w-16).
	cw := w - cx - padX
	if r.rc.thread {
		cw = w - cx - dp(gtx, 16)
	}
	cw -= r.rc.actionsPad(gtx)
	if cw < dp(gtx, 80) {
		cw = dp(gtx, 80)
	}
	gs := gutterClass.Enter(gtx, 0, 0)
	defer gs.Leave()
	if gs.Hidden() {
		return layout.Dimensions{}
	}
	// Compact in a thread: MessageKitGutter has no compact slot there, so
	// only gutter__right
	noLeft := r.rc.dense && r.rc.thread
	lw := dp(gtx, mt.avatar)
	if r.rc.denseChan() {
		lw = dp(gtx, mt.denseTS)
	}
	left := gutterLeftClass.Enter(gtx, 0, 0)
	if !left.Hidden() && !noLeft {
		at(gtx, padX, 0, func(gtx layout.Context) layout.Dimensions {
			gtx.Constraints = layout.Constraints{Max: image.Pt(lw, 1<<20)}
			if left.Active() {
				return left.Wrap(gtx, func(gtx layout.Context) layout.Dimensions { return r.layoutGutterLeft(gtx, p) })
			}
			return r.layoutGutterLeft(gtx, p)
		})
	}
	left.Leave()
	right := gutterRightClass.Enter(gtx, 0, 0)
	defer right.Leave()
	if right.Hidden() {
		return layout.Dimensions{}
	}
	d := at(gtx, cx, 0, func(gtx layout.Context) layout.Dimensions {
		gtx.Constraints = layout.Constraints{Max: image.Pt(cw, 1<<20)}
		if right.Active() {
			return right.Wrap(gtx, func(gtx layout.Context) layout.Dimensions { return r.layoutColumn(gtx) })
		}
		return r.layoutColumn(gtx)
	})
	return layout.Dimensions{Size: image.Pt(w, d.Size.Y)}
}

func (r *row) layoutGutterLeft(gtx layout.Context, p slackc.Props) layout.Dimensions {
	if r.rc.header && !r.rc.denseChan() {
		return at(gtx, 0, dp(gtx, 4), func(gtx layout.Context) layout.Dimensions {
			if e, ok := p.Elem("light"); ok {
				return slackc.LayoutElement(gtx, e)
			}
			return slackc.Component(gtx, "MessageAvatar", r, (*row).avatarSlotProps, (*row).layoutMessageAvatar)
		})
	}
	if e, ok := p.Elem("compact"); ok {
		return slackc.LayoutElement(gtx, e)
	}
	return r.layoutCompactTS(gtx, nil)
}

// layoutMessageAvatar is MessageAvatar around BaseMessageAvatar (its
// child, rendered in its tree when patched).
func (r *row) layoutMessageAvatar(gtx layout.Context, p slackc.Props) layout.Dimensions {
	if e, ok := slackc.PropChildren(p); ok {
		return slackc.LayoutElement(gtx, e)
	}
	return slackc.Component(gtx, "BaseMessageAvatar", r, (*row).baseAvatarProps, (*row).layoutBaseMessageAvatar)
}

// layoutBaseMessageAvatar is BaseMessageAvatar around ConnectedBaseAvatar.
func (r *row) layoutBaseMessageAvatar(gtx layout.Context, p slackc.Props) layout.Dimensions {
	if e, ok := slackc.PropChildren(p); ok {
		return slackc.LayoutElement(gtx, e)
	}
	return slackc.Component(gtx, "ConnectedBaseAvatar", r, (*row).avatarProps, (*row).layoutAvatar)
}

// layoutAvatar is the row's ConnectedBaseAvatar (className
// c-message_kit__avatar); a plugin may change userId/botId/size.
func (r *row) layoutAvatar(gtx layout.Context, p slackc.Props) layout.Dimensions {
	rc := r.rc
	s := dp(gtx, mt.avatar)
	url := r.avatar
	if p != nil {
		s = dp(gtx, float32(p.IntOr("size", int(mt.avatar))))
		url = avatarURL(rc.h, p, r.m.User, url)
	}
	if r.custom { // BaseMessageAvatar: the TeamIcon, no profile card
		return r.layoutTeamAvatar(gtx, p, s)
	}
	if r.hmName { // BaseMessageAvatar: HuddleMsgAvatar, no profile card
		return r.layoutHuddleAvatar(gtx, rc, s)
	}
	st := avatarClass.EnterClass(gtx, btnState(gtx, &r.avatarBtn), 0, slackc.ExtraClass(p))
	defer st.Leave()
	if st.Hidden() {
		return layout.Dimensions{}
	}
	// Slack's avatar is the element's picture, so a background-image rule
	// (CustomSlackbot's custom-response avatar) replaces it.
	url = st.BackgroundImage(url)
	r.avDrawn = url
	// Clicked before Layout: Clickable.Layout consumes pending clicks.
	if ctxclick.Clicked(gtx, &r.avatarBtn) && r.m.User != "" {
		rc.h.OpenProfile(rc.origin(image.Point{}), r.m.User)
	}
	size := image.Pt(s, s)
	gtx.Constraints = layout.Exact(size)
	if st.Active() {
		return st.Wrap(gtx, func(gtx layout.Context) layout.Dimensions { return r.avatarButton(gtx, url, size) })
	}
	return r.avatarButton(gtx, url, size)
}

func (r *row) avatarButton(gtx layout.Context, url string, size image.Point) layout.Dimensions {
	r.memberMenuOn(gtx, &r.avatarCtx)
	d := r.avatarBtn.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
		drawAvatar(gtx, r.rc.h, url, image.Rectangle{Max: size}, r.rc.c)
		return layout.Dimensions{Size: size}
	})
	contextArea(gtx, &r.avatarCtx, d.Size)
	r.avatarHov.LayoutBot(gtx, r.hoverUser(), r.hoverBot(), d.Size)
	return d
}

// ctxTag is a right-click (context menu) event target.
type ctxTag struct{ _ byte }

// memberMenuOn opens Slack's member context menu (popups.NewMemberMenu,
// which plugins like Nicknames extend) when tag got a right-click (or a
// macOS Ctrl+click), and tells the list the press is taken so it doesn't
// open the message's menu as well.
func (r *row) memberMenuOn(gtx layout.Context, tag *ctxTag) {
	for {
		ev, ok := gtx.Event(pointer.Filter{Target: tag, Kinds: pointer.Press})
		if !ok {
			return
		}
		if e, ok := ev.(pointer.Event); ok && ctxclick.Press(e) && r.m.User != "" {
			h := r.rc.h
			if l := r.rc.list; l != nil {
				l.ctx.taken = true
			}
			h.ShowOverlay(popups.NewMemberMenu(h, r.rc.origin(image.Pt(int(e.Position.X), int(e.Position.Y))), r.m.User))
		}
	}
}

// contextArea registers tag for presses over size, passing them through
// to the button underneath.
func contextArea(gtx layout.Context, tag *ctxTag, size image.Point) {
	defer clip.Rect(image.Rectangle{Max: size}).Push(gtx.Ops).Pop()
	defer pointer.PassOp{}.Push(gtx.Ops).Pop()
	event.Op(gtx.Ops, tag)
}

// layoutCompactTS is the grouped row's hover time ("3:04"), right-aligned
// in the avatar column: the gutter's compact slot.
func (r *row) layoutCompactTS(gtx layout.Context, p slackc.Props) layout.Dimensions {
	rc := r.rc
	box := image.Pt(dp(gtx, mt.avatar), dp(gtx, 22))
	if rc.denseChan() {
		box.X = dp(gtx, mt.denseTS)
	}
	visible := r.compactVisible()
	if p.Has("visible") {
		visible = p.Bool("visible")
	}
	if !visible {
		return layout.Dimensions{Size: box}
	}
	def := compactTSClass
	if rc.thread {
		def = threadCompactTSClass
	}
	st := def.Enter(gtx, hoverState(rc.hovered), 0)
	defer st.Leave()
	if st.Hidden() {
		return layout.Dimensions{Size: box}
	}
	// getCompactGutter: renderTimestamp({meridiem: !(adjacent && light)}):
	// "3:04" on a grouped Clean row, "3:04 PM" under Compact
	ts := shortClock(tsTime(r.m.TS))
	if rc.denseChan() {
		ts = clock(tsTime(r.m.TS))
	}
	s := textStyle{size: float32(st.FontSize(12)), col: st.Color(rc.c.Text2), lineH: 22}
	sz := measure(gtx, rc.th, s, ts)
	// The 12px time is inline in a 15px/22px line (the timestamp div
	// inherits .c-message_kit__message's font, margin -4px 0 puts its line
	// where the body's is), so it sits on the body's baseline: the 15px
	// strut's, not a 12px line's.
	rec := op.Record(gtx.Ops)
	d := at(gtx, 0, 0, func(gtx layout.Context) layout.Dimensions {
		if st.Active() {
			return st.Wrap(gtx, func(gtx layout.Context) layout.Dimensions { return decoLabel(gtx, rc.th, st, s, ts) })
		}
		return label(gtx, rc.th, s, ts)
	})
	call := rec.Stop()
	dy := strutBaseline(gtx) - (d.Size.Y - d.Baseline)
	t := op.Offset(image.Pt(box.X-sz.X, dy)).Push(gtx.Ops)
	call.Add(gtx.Ops)
	t.Pop()
	r.compactBL = dy + d.Size.Y - d.Baseline
	return layout.Dimensions{Size: box}
}

// strutBaseline is the baseline of a 15px/22px line (the message font)
// from its top.
func strutBaseline(gtx layout.Context) int { return cssBaseline(gtx, 15, 22) }

// layoutColumn is the gutter's right side: name line, body, reactions,
// thread bar. Returns its height.
func (r *row) layoutColumn(gtx layout.Context) layout.Dimensions {
	rc := r.rc
	m := &r.m
	cw := gtx.Constraints.Max.X
	y := 0
	broadcast := m.Broadcast && !rc.thread && m.ThreadTS != "" && m.ThreadTS != m.TS
	// Compact: the sender recorded, put on the body's first line once the
	// body is laid out (inline), its x and baseline
	var name op.CallOp
	nameX, nameTop, inline := 0, 0, false
	r.nameX = 0
	lead := 0
	if rc.header {
		var d layout.Dimensions
		// The sender line is the column's first 15px/22px line (inline in
		// .c-message_kit__gutter__right, 4px below the row top), the body's
		// first line right under it: baselines at 4+16.8 and 26+16.8.
		switch {
		case rc.thread:
			d = slackc.Component(gtx, "ThreadSenderAndTimestampGeneric", r, (*row).threadSenderProps, (*row).layoutThreadSender)
			r.nameBL = d.Size.Y - d.Baseline
			y += max(d.Size.Y, dp(gtx, mt.nameLine))
		case rc.dense:
			// getCompactMessageBody: inside .c-message_kit__indent,
			// BaseBroadcastPreamble {the sender (margin-left -8px), a
			// no-break space}, then the text inline (CompactBlocks)
			rec := op.Record(gtx.Ops)
			d = slackc.Component(gtx, "BaseBroadcastPreamble", r, (*row).preambleProps, (*row).layoutPreamble)
			name = rec.Stop()
			nameX = -dp(gtx, mt.indent)
			r.nameX = nameX
			nameTop = d.Size.Y - d.Baseline
			r.nameBL = nameTop
			after := nameX + d.Size.X + nbspWidth(gtx, rc.th)
			switch {
			case broadcast:
				// .c-message_kit__broadcast_preamble--dense (display flex):
				// "replied to a thread" on the sender's line, the text under it
				placeCall(gtx, name, nameX, 0)
				r.broadcastLine(gtx, rc, after, 0)
				y += max(d.Size.Y, dp(gtx, mt.nameLine))
			case r.edit == nil && r.inlineBody():
				inline, lead = true, after
			default:
				placeCall(gtx, name, nameX, 0)
				y += max(d.Size.Y, dp(gtx, mt.nameLine))
			}
		default:
			d = slackc.Component(gtx, "BaseBroadcastPreamble", r, (*row).preambleProps, (*row).layoutPreamble)
			r.nameBL = d.Size.Y - d.Baseline
			y += max(d.Size.Y, dp(gtx, mt.nameLine))
		}
	}
	if broadcast && !rc.dense {
		y += r.broadcastLine(gtx, rc, 0, y)
	}
	r.rv.SetLead(lead)

	// body (or the inline edit composer)
	r.handler.h, r.handler.m = rc.h, m
	r.handler.anchor = rc.origin
	var body layout.Dimensions
	if r.edit != nil {
		ed := r.edit
		// The composer's first Focus can land before its editor registered
		// for input; ask again on the next frames so Escape/typing work.
		if r.editFrames < 3 {
			r.editFrames++
			ed.Focus()
		}
		ed.Origin = rc.winOrigin.Add(r.col).Add(image.Pt(-dp(gtx, 4), y))
		body = at(gtx, -dp(gtx, 4), y, func(gtx layout.Context) layout.Dimensions {
			gtx.Constraints = layout.Constraints{Max: image.Pt(cw+dp(gtx, 4), 1<<20)}
			return ed.Layout(gtx)
		})
		y += dp(gtx, 4)
	} else {
		hd := at(gtx, 0, y, func(gtx layout.Context) layout.Dimensions {
			gtx.Constraints = layout.Constraints{Max: image.Pt(cw, 1<<20)}
			return slackc.Component(gtx, "BaseVisuallyHiddenMessageText", r, (*row).hiddenTextProps, (*row).layoutNothing)
		})
		y += hd.Size.Y
		body = at(gtx, 0, y, func(gtx layout.Context) layout.Dimensions {
			gtx.Constraints = layout.Constraints{Max: image.Pt(cw, 1<<20)}
			return slackc.Component(gtx, "MessageText", r, (*row).textProps, (*row).layoutMessageText)
		})
	}
	r.bodyRect = image.Rectangle{Min: image.Pt(0, y), Max: image.Pt(body.Size.X, y+body.Size.Y)}.Add(r.col)
	if inline {
		// the sender sits on the body's first line, baselines together
		dy := y
		if bl := r.rv.FirstBaseline(); bl >= 0 && body.Size.Y > 0 {
			dy = y + bl - nameTop
		}
		placeCall(gtx, name, nameX, dy)
		r.nameBL = dy + nameTop
		if body.Size.Y == 0 {
			y += dp(gtx, mt.nameLine)
		}
	}
	y += body.Size.Y
	if r.edit == nil && (r.hasFiles || r.hasAtts) {
		// the files and attachments are drawn in the body; these carry
		// their props (overrides filter the body) and what plugins add
		y += r.tailParts(gtx, y, cw)
	}
	if isFailed(m) {
		y += r.failedLine(gtx, rc, 0, y)
	}
	if body.Size.Y == 0 && !rc.header {
		y += dp(gtx, mt.nameLine)
	}
	if r.edit != nil || r.textMargin() {
		y += dp(gtx, mt.bodyMarginBottom)
	}

	// reactions
	if len(m.Reactions) > 0 {
		// ReactionBar (Ltiy): div.c-reaction_bar > span > the pills, each
		// .c-reaction { display: inline-flex; vertical-align: top;
		// height 24px; margin-bottom: 4px }, so every line of pills is a
		// 28px line box; then .c-message_kit__reaction_bar
		// { margin-bottom: 4px }
		y += dp(gtx, mt.reactTop) - dp(gtx, 4)
		y += r.reactionBar(gtx, image.Pt(0, y), r.col, cw)
		y += dp(gtx, mt.reactGap) + dp(gtx, mt.reactBottom)
	} else {
		r.reactRects = r.reactRects[:0]
		r.addRect = image.Rectangle{}
	}

	// thread bar
	if !rc.thread && (m.ThreadTS == "" || m.ThreadTS == m.TS) && r.replyBarWanted(m.ReplyCount) {
		d := at(gtx, 0, y, func(gtx layout.Context) layout.Dimensions {
			gtx.Constraints = layout.Constraints{Max: image.Pt(cw, 1<<20)}
			return slackc.Component(gtx, "ThreadReplies", r, (*row).repliesProps, (*row).layoutThreadReplies)
		})
		y += d.Size.Y
	}
	if slackc.Patched("CodeChannelEntityCard") {
		y += r.channelCards(gtx, y, cw)
	}
	return layout.Dimensions{Size: image.Pt(cw, y)}
}

// textMargin: the body ends with Slack's 4px. MessageBlocks (iGDn) adds
// c-message_kit__blocks--rich_text (margin-bottom 4px) only when the
// message's last block is rich_text, and a message without blocks is
// .c-message_kit__text (:after, 4px). After a Block Kit block (a section,
// context, actions…) the body ends at that block's own bottom margin,
// which richview already adds; .c-message_kit__blocks has none.
func (r *row) textMargin() bool {
	bs := r.m.Doc.Blocks
	return len(bs) == 0 || !bs[len(bs)-1].Kit
}

// layoutNothing is an empty native: a surface Clack doesn't draw
// (BaseVisuallyHiddenMessageText is screen-reader text), so what a
// plugin adds there still shows.
func (r *row) layoutNothing(gtx layout.Context, _ slackc.Props) layout.Dimensions {
	return layout.Dimensions{}
}

// layoutMessageText is MessageText around MessageBlocks: a plugin's msg
// (BetterBlock's filtered text) is what the body draws.
func (r *row) layoutMessageText(gtx layout.Context, p slackc.Props) layout.Dimensions {
	r.textOv = nil
	if msg := p.Map("msg"); !r.ownMsg(msg) {
		r.textOv = msg
	}
	return slackc.Component(gtx, "MessageBlocks", r, (*row).blocksProps, (*row).layoutMessageBlocks)
}

// tailParts lays out MessageFiles and MessageKitAttachments under the
// body; returns their height (0 unless a plugin adds something).
func (r *row) tailParts(gtx layout.Context, y, cw int) int {
	h := 0
	if r.hasFiles {
		d := at(gtx, 0, y, func(gtx layout.Context) layout.Dimensions {
			gtx.Constraints = layout.Constraints{Max: image.Pt(cw, 1<<20)}
			return slackc.Component(gtx, "MessageFiles", r, (*row).tailProps, (*row).layoutMessageFiles)
		})
		h += d.Size.Y
	}
	if r.hasAtts {
		d := at(gtx, 0, y+h, func(gtx layout.Context) layout.Dimensions {
			gtx.Constraints = layout.Constraints{Max: image.Pt(cw, 1<<20)}
			return slackc.Component(gtx, "MessageKitAttachments", r, (*row).tailProps, (*row).layoutMessageAttachments)
		})
		h += d.Size.Y
	}
	return h
}

// layoutMessageFiles is MessageFiles: the files are the body's file
// cards; a plugin's msg.files (BetterBlock) filters them.
func (r *row) layoutMessageFiles(gtx layout.Context, p slackc.Props) layout.Dimensions {
	r.tailOverride(gtx, &r.filesOv, p)
	return layout.Dimensions{}
}

// layoutMessageAttachments is MessageKitAttachments: the attachments are
// the body's cards; a plugin's msg.attachments filters them.
func (r *row) layoutMessageAttachments(gtx layout.Context, p slackc.Props) layout.Dimensions {
	r.tailOverride(gtx, &r.attsOv, p)
	return layout.Dimensions{}
}

func (r *row) tailOverride(gtx layout.Context, ov *slackc.Props, p slackc.Props) {
	msg := p.Map("msg")
	if r.ownMsg(msg) || propsID(msg) == propsID(r.textOv) {
		msg = nil
	}
	if propsID(msg) != propsID(*ov) {
		*ov = msg
		gtx.Execute(op.InvalidateCmd{}) // the body applies it next frame
	}
}

// layoutThreadReplies is ThreadReplies, the reply bar. A plugin's msg
// (BetterBlock's) sets the count, the repliers and the last reply; a
// count of 0 hides the bar.
func (r *row) layoutThreadReplies(gtx layout.Context, p slackc.Props) layout.Dimensions {
	m := &r.m
	count, users, latest := m.ReplyCount, m.ReplyUsers, m.LatestReply
	if msg := p.Map("msg"); msg != nil {
		count, latest = msg.IntOr("reply_count", 0), msg.StrOr("latest_reply", "")
		users = users[:0:0]
		for _, u := range msg.List("reply_users") {
			if s, ok := u.(string); ok {
				users = append(users, s)
			}
		}
	}
	if !r.replyBarWanted(count) {
		r.replyN, r.replyLink = 0, ""
		return layout.Dimensions{}
	}
	h := r.threadBar(gtx, r.rc, 0, 0, gtx.Constraints.Max.X, count, users, latest)
	return layout.Dimensions{Size: image.Pt(gtx.Constraints.Max.X, h+dp(gtx, 4))}
}

// layoutReplyAvatar is one ReplyBarAvatar (a replier's 24px avatar); a
// plugin may hide it (BetterBlock) or draw its own (CustomSlackbot).
func (av *replyAv) layout(gtx layout.Context, p slackc.Props) layout.Dimensions {
	rc := av.r.rc
	id := av.id
	if p != nil {
		id = p.StrOr("userId", id)
	}
	// ReplyBarAvatar (v8ac): Slackbot answering with one of the
	// workspace's custom responses in this thread (its loaded replies,
	// hasSlackbotCustomResponseInThread) shows as the team, a 24px TeamIcon
	// (data-qa reply_bar_team_avatar), like its custom responses do
	av.team = false
	if app := rc.h.Core(); app != nil && id == "USLACKBOT" {
		m := &av.r.m
		root := m.ThreadTS
		if root == "" {
			root = m.TS
		}
		if app.SlackbotCustomResponseInThread(m.Channel, root, id) {
			name, icon := app.CustomResponseTeam(int(mt.threadAvatar))
			av.drawn, av.team = icon, true
			drawTeamIcon(gtx, rc.h, icon, name, image.Rect(0, 0, av.sz, av.sz), rc.c)
			return layout.Dimensions{Size: image.Pt(av.sz, av.sz)}
		}
	}
	url := ""
	if app := rc.h.Core(); app != nil {
		switch u, ok := app.User(id); {
		case core.LooksLikeBotID(id): // ReplyBarAvatar's botId: reply_user_icons, agent_identity_overrides, the bot's icon
			url = app.ReplyBarBotAvatar(&av.r.m, id, 2*int(mt.threadAvatar))
		case ok:
			url = u.Avatar
		default:
			ensureUser(rc.h, id)
		}
	}
	av.drawn = url
	drawAvatar(gtx, rc.h, url, image.Rect(0, 0, av.sz, av.sz), rc.c)
	return layout.Dimensions{Size: image.Pt(av.sz, av.sz)}
}

// channelCards lays out a CodeChannelEntityCard (empty natively: Clack
// has no channel entity card) under the message for each channel it
// mentions, so what a plugin adds (QuickJoin's Join button) shows there.
// Only called while the name is patched.
func (r *row) channelCards(gtx layout.Context, y, cw int) int {
	if !r.chansDoc {
		r.chansDoc = true
		r.chans = r.chans[:0]
		for _, id := range docChannels(r.m.Doc.Blocks) {
			r.chans = append(r.chans, chanCard{r: r, id: id})
		}
	}
	h := 0
	for i := range r.chans {
		cc := &r.chans[i]
		d := at(gtx, 0, y+h, func(gtx layout.Context) layout.Dimensions {
			gtx.Constraints = layout.Constraints{Max: image.Pt(cw, 1<<20)}
			return slackc.Component(gtx, "CodeChannelEntityCard", cc, (*chanCard).props, (*chanCard).layout)
		})
		h += d.Size.Y
	}
	return h
}

func (cc *chanCard) layout(gtx layout.Context, _ slackc.Props) layout.Dimensions {
	return layout.Dimensions{}
}

// layoutMessageBlocks is MessageBlocks: the boundary around the body that
// plugins append siblings to (OwlMode's save button).
func (r *row) layoutMessageBlocks(gtx layout.Context, _ slackc.Props) layout.Dimensions {
	st := msgBlocksClass.Enter(gtx, 0, 0)
	defer st.Leave()
	if st.Hidden() {
		return layout.Dimensions{}
	}
	if st.Active() {
		return st.Wrap(gtx, func(gtx layout.Context) layout.Dimensions {
			return slackc.Component(gtx, "Blocks", r, (*row).blocksProps, (*row).layoutBlocks)
		})
	}
	return slackc.Component(gtx, "Blocks", r, (*row).blocksProps, (*row).layoutBlocks)
}

// layoutBlocks is Blocks: the rich body. A plugin's msg.blocks (filtered,
// reordered, or new Block Kit) is what gets drawn.
func (r *row) layoutBlocks(gtx layout.Context, p slackc.Props) layout.Dimensions {
	rc := r.rc
	msg := p.Map("msg")
	if r.ownMsg(msg) || propsID(msg) == propsID(r.textOv) {
		msg = r.textOv // our own props (before JS answered): MessageText's, if any
	}
	id := propsID(msg)
	tail := [2]uintptr{propsID(r.filesOv), propsID(r.attsOv)}
	if id != r.blocksOv || tail != r.tailOv {
		doc := r.m.Doc
		if id != 0 {
			doc.Blocks = docBlocks(rc.h.Core(), doc.Blocks, r.m.Blocks, msg)
		}
		doc.Blocks = fixChannels(rc.h, doc.Blocks)
		doc.Blocks = filterTail(doc.Blocks, r.filesOv, r.attsOv)
		r.rv.Set(doc, r.rvOpts())
		r.blocksOv, r.tailOv = id, tail
	} else {
		// the row is made before its context knows the viewer (Own: the
		// link previews' remove buttons and "Remove preview")
		o := r.rvOpts()
		o.Elements = r.rv.Options().Elements // withElements' (blockkit.go)
		r.rv.SetOptions(o)
	}
	st := blocksClass.Enter(gtx, 0, 0)
	defer st.Leave()
	if st.Hidden() {
		return layout.Dimensions{}
	}
	r.withElements(rc.h)
	if st.Active() {
		return st.Wrap(gtx, func(gtx layout.Context) layout.Dimensions { return r.rv.Layout(gtx, rc.st, &r.handler) })
	}
	return r.rv.Layout(gtx, rc.st, &r.handler)
}

// layoutPreamble is BaseBroadcastPreamble, the channel message header.
// Its children are the MessageSender and Timestamp slots; a plugin may
// insert siblings between them (UserPronouns).
func (r *row) layoutPreamble(gtx layout.Context, p slackc.Props) layout.Dimensions {
	if e, ok := slackc.PropChildren(p); ok {
		return slackc.LayoutElement(gtx, e)
	}
	return r.nameLine(gtx, !r.rc.denseChan()) // Compact: the time is in the gutter
}

// layoutThreadSender is ThreadSenderAndTimestampGeneric (thread pane).
func (r *row) layoutThreadSender(gtx layout.Context, p slackc.Props) layout.Dimensions {
	if p.Has("visible") && !p.Bool("visible") {
		return layout.Dimensions{}
	}
	return r.nameLine(gtx, !p.Bool("omitTimestamp"))
}

// nameLine draws "Name [APP] [status] 3:04 PM", baselines aligned.
func (r *row) nameLine(gtx layout.Context, withTS bool) layout.Dimensions {
	w := gtx.Constraints.Max.X
	lh := dp(gtx, mt.nameLine)
	g := gtx
	g.Constraints = layout.Constraints{Max: image.Pt(w*2/3, lh)}
	nd := slackc.Component(g, "MessageSender", r, (*row).senderProps, (*row).layoutSender)
	baseline := nd.Size.Y - nd.Baseline
	if nd.Size.Y == 0 {
		baseline = lh - dp(gtx, 5)
	}
	if withTS {
		// Align baselines.
		m := op.Record(gtx.Ops)
		d := r.layoutTimestamp(gtx, nil)
		call := m.Stop()
		t := op.Offset(image.Pt(nd.Size.X+dp(gtx, mt.tsGap), baseline-(d.Size.Y-d.Baseline))).Push(gtx.Ops)
		call.Add(gtx.Ops)
		t.Pop()
	}
	if !withTS && r.rc.dense {
		w = nd.Size.X // inline before the text: its own width
	}
	return layout.Dimensions{Size: image.Pt(w, lh), Baseline: lh - baseline}
}

// layoutSender is MessageSender around BaseMessageSender.
func (r *row) layoutSender(gtx layout.Context, _ slackc.Props) layout.Dimensions {
	return slackc.Component(gtx, "BaseMessageSender", r, (*row).baseSenderProps, (*row).layoutBaseSender)
}

// layoutBaseSender is BaseMessageSender: the name button, APP badge and
// status emoji. Returns the width drawn and the name's baseline.
func (r *row) layoutBaseSender(gtx layout.Context, p slackc.Props) layout.Dimensions {
	rc := r.rc
	c := rc.c
	st := senderClass.EnterClass(gtx, 0, 0, slackc.ExtraClass(p))
	defer st.Leave()
	if st.Hidden() {
		return layout.Dimensions{}
	}
	span := r.custom || r.hmName // not interactive: a span, no button (custom_response.go, huddle_msg.go)
	if ctxclick.Clicked(gtx, &r.nameBtn) && r.m.User != "" && !span {
		rc.h.OpenProfile(rc.origin(image.Point{}), r.m.User)
	}
	var bs slackc.Styled
	if span {
		bs = senderNameClass.Enter(gtx, 0, 0)
	} else {
		bs = senderBtnClass.Enter(gtx, btnState(gtx, &r.nameBtn), 0)
	}
	var nd layout.Dimensions
	if !bs.Hidden() {
		switch active := bs.Active(); {
		case active && span:
			nd = bs.Wrap(gtx, func(gtx layout.Context) layout.Dimensions { return r.nameSpan(gtx, bs) })
		case active:
			nd = bs.Wrap(gtx, func(gtx layout.Context) layout.Dimensions { return r.nameButton(gtx, bs) })
		case span:
			nd = r.nameSpan(gtx, bs)
		default:
			nd = r.nameButton(gtx, bs)
		}
	}
	// a plugin's text-decoration on the sender (IdvStatus' wavy underline)
	if nd.Size.X > 0 {
		ds := bs
		if s := ds.Style(); s == nil || s.TextDecoration.Lines() == 0 {
			ds = st
		}
		slackc.Decorate(gtx, ds, 0, nd.Size.Y-nd.Baseline, nd.Size.X, bs.Color(c.Text))
	}
	bs.Leave()
	if nd.Size.Y == 0 {
		nd.Size.Y, nd.Baseline = dp(gtx, mt.nameLine), dp(gtx, 5)
	}
	baseline := nd.Size.Y - nd.Baseline
	xx := nd.Size.X
	if r.custom {
		xx += r.customBadge(gtx, xx, nd.Size.Y)
	}
	if r.hmName { // renderStatusBadge: LIVE / MISSED
		xx += r.huddleBadge(gtx, rc, xx, baseline)
	}
	if badge := r.badge; badge != "" && !r.custom { // renderBotLabel: none for people, Slackbot, custom responses
		xx += dp(gtx, mt.badgeGap)
		s := textStyle{size: 10, weight: theme.Bold, col: c.BadgeText}
		sz := measure(gtx, rc.th, s, badge)
		bw := sz.X + dp(gtx, 6)
		bh := dp(gtx, 14)
		top := baseline - dp(gtx, 11)
		rrect(gtx.Ops, image.Rect(xx, top, xx+bw, top+bh), dp(gtx, 2), c.BadgeBg)
		at(gtx, xx+dp(gtx, 3), top+(bh-sz.Y)/2, func(gtx layout.Context) layout.Dimensions { return label(gtx, rc.th, s, badge) })
		xx += bw
	}
	// Compact: .c-message_kit__sender--dense .c-custom_status { border:
	// 1px solid rgba(var(--sk_foreground_low), .13); border-radius: 5px;
	// padding: 1px 2px } around the huddle icon or status emoji
	dense := rc.dense && !rc.thread
	if r.huddle != "" {
		// BaseHuddleStatus in .c-custom_status (margin-left 4px): the 20px
		// p-huddle_status_icon (vertical-align top, margin-top 2px) with
		// the headphones glyph; a huddle in another channel gets the
		// --other --no_theme circle (#1d1c1d21).
		xx += dp(gtx, 4)
		bs := dp(gtx, 20)
		top := baseline - strutBaseline(gtx) + dp(gtx, 2)
		if dense {
			xx += statusBorder(gtx, c, image.Rect(xx, top, xx+bs, top+bs))
		}
		if r.huddle != r.m.Channel {
			bg := color.NRGBA{R: 0x1d, G: 0x1c, B: 0x1d, A: 0x21}
			if rc.th.Dark {
				bg = color.NRGBA{R: 0xe8, G: 0xe8, B: 0xe8, A: 0x21}
			}
			rrect(gtx.Ops, image.Rect(xx, top, xx+bs, top+bs), bs/2, bg)
		}
		is := dp(gtx, 16)
		at(gtx, xx+(bs-is)/2, top+(bs-is)/2, func(gtx layout.Context) layout.Dimensions {
			return icons.Get(icons.Slack("headphones"))(gtx, unit.Dp(16), c.Text)
		})
		xx += bs
		if dense {
			xx += dp(gtx, 3)
		}
	} else if r.statusURL != "" {
		s := dp(gtx, mt.statusEmoji)
		xx += dp(gtx, 4)
		top := baseline - s + dp(gtx, 3)
		if dense {
			xx += statusBorder(gtx, c, image.Rect(xx, top, xx+s, top+s))
		}
		drawEmoji(gtx, rc.h, r.statusURL, image.Rect(xx, top, xx+s, top+s))
		xx += s
		if dense {
			xx += dp(gtx, 3)
		}
	}
	return layout.Dimensions{Size: image.Pt(xx, nd.Size.Y), Baseline: nd.Baseline}
}

// nameButton is .c-message__sender_button: the name, underlined on hover.
func (r *row) nameButton(gtx layout.Context, bs slackc.Styled) layout.Dimensions {
	c := r.rc.c
	nameS := textStyle{size: float32(bs.FontSize(15)), weight: theme.Black, col: bs.Color(c.Text), lineH: 22}
	before, after := bs.Generated()
	r.nameShown = before + after
	if nameS.size > 0 {
		r.nameShown = before + r.name + after
	}
	r.memberMenuOn(gtx, &r.nameCtx)
	d := r.nameBtn.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
		if nameS.size <= 0 { // font-size: 0, the name drawn by ::after (CustomSlackbot)
			h := dp(gtx, nameS.lineH)
			return layout.Dimensions{Size: image.Pt(0, h), Baseline: dp(gtx, 5)}
		}
		d := lineBox(gtx, nameS, func(gtx layout.Context) layout.Dimensions { return label(gtx, r.rc.th, nameS, r.name) })
		if r.nameBtn.Hovered() {
			base := d.Size.Y - d.Baseline
			fill(gtx.Ops, image.Rect(0, base+dp(gtx, 2), d.Size.X, base+dp(gtx, 3)), nameS.col)
		}
		return d
	})
	contextArea(gtx, &r.nameCtx, d.Size)
	r.nameHov.LayoutBot(gtx, r.hoverUser(), r.hoverBot(), d.Size)
	return d
}

// hoverUser is the member whose hover card the sender's name and avatar
// open (the one their click opens; none for a custom-response sender).
func (r *row) hoverUser() string {
	if r.custom {
		return ""
	}
	return r.m.User
}

// hoverBot is the bot a sender with no user opens AppHoverCard for (Slack
// passes the message's bot_id as ProfileHoverTrigger's serviceId).
func (r *row) hoverBot() string {
	if r.custom || r.m.User != "" {
		return ""
	}
	if r.m.BotID != "" {
		return r.m.BotID
	}
	if bp := r.m.BotProfile; bp != nil {
		return bp.ID
	}
	return ""
}

// layoutTimestamp is the header's Timestamp ("3:04 PM").
func (r *row) layoutTimestamp(gtx layout.Context, p slackc.Props) layout.Dimensions {
	if p.Has("visible") && !p.Bool("visible") {
		return layout.Dimensions{}
	}
	st := tsClass.Enter(gtx, 0, 0)
	defer st.Leave()
	if st.Hidden() {
		return layout.Dimensions{}
	}
	tsS := textStyle{size: float32(st.FontSize(12)), col: st.Color(r.rc.c.Text2)}
	t := tsTime(r.m.TS)
	tsText := clock(t)
	if r.rc.thread {
		// ThreadSenderAndTimestampGeneric: customTimestampFormat
		// THREAD_DETAIL_VIEW, re-rendered every minute.
		now := r.rc.now
		tsText = threadTime(t, now)
		if age := now.Sub(t); age >= 0 && age < time.Hour {
			gtx.Execute(op.InvalidateCmd{At: now.Add(time.Minute - age%time.Minute)})
		}
	}
	if st.Active() {
		return st.Wrap(gtx, func(gtx layout.Context) layout.Dimensions { return decoLabel(gtx, r.rc.th, st, tsS, tsText) })
	}
	return label(gtx, r.rc.th, tsS, tsText)
}

// labelLine draws a small gray label with an icon above the name ("Pinned").
func (r *row) labelLine(gtx layout.Context, rc *rowCtx, x, y int, icon, txt string) int {
	return r.labelWith(gtx, rc, y, icon, "", txt)
}

// labelGlyphs are the labels whose icon is a glyph of Slack's icon font
// (MessageLabels' legacy Icon, size "inherit": the label's 13px), drawn on
// the text's baseline (pin_icon.go), with the icon's CSS colour: pinned
// #e8912d, broadcast sk_highlight, ephemeral the label's own colour.
var labelGlyphs = map[string]func(gtx layout.Context, origin image.Point, em int, col color.NRGBA){
	"pin":             drawPin,
	"jump":            drawJump,
	"eye":             drawEye,
	"bookmark-filled": drawBookmark,
}

// laterIcons are the Later label's icons (meLQ ec: "later" the font's
// bookmark-filled, "later_todo" the SVG check-circle-filled), whose label
// is .c-message_kit__labels--later / --later_todo: the text and icon in
// sk_highlight, bold.
var laterIcons = map[string]bool{"bookmark-filled": true, "check-circle-filled": true}

// labelWith is labelLine with a link leading the text: Slack's
// ButtonLink (.c-message_kit__labels__link: the label's colour,
// underlined under the pointer), clicked through r.labelLink.
func (r *row) labelWith(gtx layout.Context, rc *rowCtx, y int, icon, link, txt string) int {
	return r.labelRuns(gtx, rc, y, icon, labelRun{text: link, btn: &r.labelLink}, labelRun{text: txt})
}

// labelRun is one run of a label's text: plain, or a ButtonLink in the
// label's colour (.c-message_kit__labels__link, underlined under the
// pointer); bold is MemberName's strong; hov, with user, the link's
// ProfileHoverTrigger (MemberLink, 25Rg).
type labelRun struct {
	text string
	btn  *widget.Clickable
	bold bool
	hov  *hovercard.Trigger
	user string
}

// labelRuns draws a label line: the icon, then the runs one after the
// other on one line.
func (r *row) labelRuns(gtx layout.Context, rc *rowCtx, y int, icon string, runs ...labelRun) int {
	c := rc.c
	lh := dp(gtx, 19)
	is := dp(gtx, 14)
	// icon right-aligned in the 36px box after the label's padding-left
	// (20 in a channel, .p-message_pane_message__message_label; 16 in the
	// thread pane, .c-message_kit__thread_message), 8px before the text
	iconR, textX := dp(gtx, rc.padX()+36), dp(gtx, rc.padX()+44)
	switch {
	case rc.denseChan():
		// .c-message_kit__labels--dense.p-message_pane_message__message_label
		// .c-message_kit__labels__icon { width: 56px }: under the time
		iconR, textX = dp(gtx, mt.chanPadX+mt.denseTS), dp(gtx, mt.chanPadX+mt.denseTS+8)
	case rc.dense:
		// a thread's dense label has no icon width: the icon, its 8px
		// margin, the text (padding-left 16)
		iconR = dp(gtx, mt.padX) + is
		textX = iconR + dp(gtx, 8)
	}
	ls := labelsClass.Enter(gtx, 0, 0)
	defer ls.Leave()
	if ls.Hidden() {
		return 0
	}
	glyph, isGlyph := labelGlyphs[icon]
	inlineSVG := icon == "check-circle-filled" // the later_todo label's SvgIcon (inline, 1em)
	if !isGlyph && !inlineSVG {
		at(gtx, iconR-is, y+(lh-is)/2, func(gtx layout.Context) layout.Dimensions {
			return icons.Get(icon)(gtx, unit.Dp(14), ls.Color(c.PinIcon))
		})
	}
	ts := labelsTextClass.Enter(gtx, 0, 0)
	defer ts.Leave()
	st0 := textStyle{size: 13, col: c.Muted70, lineH: 19}
	if laterIcons[icon] {
		st0.col, st0.weight = c.Link, theme.Bold
	}
	var td layout.Dimensions
	tx := textX
	for _, run := range runs {
		if run.text == "" {
			continue
		}
		st := st0
		if run.bold {
			st.weight = theme.Bold
		}
		if run.btn == nil {
			td = at(gtx, tx, y, func(gtx layout.Context) layout.Dimensions {
				return styledLabel(gtx, rc.th, ts, st, run.text)
			})
			tx += td.Size.X
			continue
		}
		td = at(gtx, tx, y, func(gtx layout.Context) layout.Dimensions {
			d := run.btn.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
				d := styledLabel(gtx, rc.th, ts, st, run.text)
				if run.btn.Hovered() && d.Size.X > 0 {
					slackc.DrawDecoration(gtx.Ops, css.DecoUnderline, cssText(ts, st).col, 0, float32(d.Size.Y-d.Baseline), float32(d.Size.X), 13*gtx.Metric.PxPerSp)
				}
				return d
			})
			if run.hov != nil {
				run.hov.Layout(gtx, run.user, d.Size)
			}
			return d
		})
		tx += td.Size.X
	}
	if isGlyph || inlineSVG {
		// right-aligned 8px before the text, on its baseline (pin_icon.go)
		em := dp(gtx, 13)
		base := y + lh*3/4
		if td.Size.Y > 0 {
			base = y + td.Size.Y - td.Baseline
		}
		col := c.PinIcon
		switch {
		case icon == "jump", laterIcons[icon]:
			col = c.Link
		case icon == "eye":
			col = cssText(ts, st0).col
		}
		if inlineSVG {
			// [data-crz] 1em square, .is-inline vertical-align -.125em
			at(gtx, iconR-em, base+em/8-em, func(gtx layout.Context) layout.Dimensions {
				return icons.Get(icon)(gtx, unit.Dp(13), ls.Color(col))
			})
		} else {
			glyph(gtx, image.Pt(iconR-em, base-em*glyphAscent/glyphUPM), em, ls.Color(col))
		}
	}
	return dp(gtx, 20)
}

// laterLabelLine draws LaterLabel (wRj5): the label, and with a link
// "{label} · {viewLink}" (or viewBookmarksLink). "View to-dos" opens the
// To-dos page, "View Saved" the Saved page. TodoDueLabel re-renders every
// minute (NLD/ useMinuteRefresh): a deadline at the next minute.
func (r *row) laterLabelLine(gtx layout.Context, rc *rowCtx, y int, ll laterLabel) int {
	if r.laterLink.Clicked(gtx) && ll.route != "" {
		rc.h.Navigate(ll.route)
	}
	if ll.route == "later" {
		gtx.Execute(op.InvalidateCmd{At: rc.now.Truncate(time.Minute).Add(time.Minute)})
	}
	if ll.format == "" {
		return r.labelRuns(gtx, rc, y, ll.icon, labelRun{text: ll.text})
	}
	var runs []labelRun
	for _, p := range msgNS.RT(ll.format, "label", ll.text, ll.arg, "\x00") {
		if p.Arg == ll.arg {
			runs = append(runs, labelRun{text: ll.link, btn: &r.laterLink})
			continue
		}
		runs = append(runs, labelRun{text: p.Text})
	}
	return r.labelRuns(gtx, rc, y, ll.icon, runs...)
}

// broadcastLabel is MessageLabels' broadcast label, which only
// ThreadReplyGeneric asks for (showBroadcast): a thread reply also sent to
// the conversation. 2D+h's text is the link ("Also sent to the channel",
// "Also sent as direct message" in a DM, "Also sent to the group" in a
// group DM); onBroadcastClick opens the message in the conversation. A
// pinned broadcast reads "{broadcast} ∙ {pinned}".
func (r *row) broadcastLabel(gtx layout.Context, rc *rowCtx, y int) int {
	m := &r.m
	if r.labelLink.Clicked(gtx) {
		rc.h.Navigate("c/" + m.Channel + "/m/" + m.TS)
	}
	link := "Also sent to the channel"
	if app := rc.h.Core(); app != nil {
		if cv, ok := app.Conv(m.Channel); ok {
			switch cv.Kind {
			case "im":
				link = "Also sent as direct message"
			case "mpim":
				link = "Also sent to the group"
			}
		}
	}
	if !m.Pinned {
		return r.labelWith(gtx, rc, y, "jump", link, "")
	}
	// "{broadcast} \u2219 {pinned}" (the message I18n), the pinned part
	// with its MemberLink
	var runs []labelRun
	for _, p := range msgNS.RT("{broadcast} \u2219 {pinned}", "broadcast", link, "pinned", "\x00") {
		switch p.Arg {
		case "broadcast":
			runs = append(runs, labelRun{text: p.Text, btn: &r.labelLink})
		case "pinned":
			runs = r.pinnedRuns(gtx, rc, runs)
		default:
			runs = append(runs, labelRun{text: p.Text})
		}
	}
	return r.labelRuns(gtx, rc, y, "jump", runs...)
}

// broadcastLine draws Slack's broadcast preamble (ThreadBroadcastPreamble,
// boot-shared UsKL) for a thread reply also sent to the channel, 15px/22:
// when the root has previewable text (canRenderSimpleMessagePreview),
// "replied to a thread:" (.c-message__broadcast_preamble_meta,
// flex-shrink 0) and the root's SimpleMessagePreview in the bold
// .c-message__broadcast_preamble_link (margin 0 4px, one line, ellipsis);
// otherwise "replied to a thread" with "a thread" the link. The link opens
// the thread. .c-message__broadcast_preamble.c-message_kit__broadcast_preamble__inner
// has margin 0, so the line is 22px in both layouts.
func (r *row) broadcastLine(gtx layout.Context, rc *rowCtx, x, y int) int {
	c := rc.c
	h := dp(gtx, 22)
	if r.broadcastBtn.Clicked(gtx) {
		rc.h.OpenThread(r.m.Channel, r.m.ThreadTS)
	}
	ps := preambleClass.Enter(gtx, 0, 0)
	defer ps.Leave()
	if ps.Hidden() {
		return 0
	}
	link := func(x int, txt string, maxW int) int {
		ls := preambleLinkClass.Enter(gtx, btnState(gtx, &r.broadcastBtn), 0)
		defer ls.Leave()
		return at(gtx, x, y, func(gtx layout.Context) layout.Dimensions {
			gtx.Constraints.Max.X = max(maxW, 0)
			return r.broadcastBtn.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
				return styledLabel(gtx, rc.th, ls, textStyle{size: 15, weight: theme.Bold, col: c.Link, lineH: 22, maxLines: 1}, txt)
			})
		}).Size.X
	}
	right := gtx.Constraints.Max.X
	if prev := r.rootPreview(rc); prev != "" {
		// `${getThreadPreambleMetaText()}:` (G7JM, the threads I18n)
		pd := at(gtx, x, y, func(gtx layout.Context) layout.Dimensions {
			return styledLabel(gtx, rc.th, ps, textStyle{size: 15, col: c.Text2, lineH: 22}, threadsNS.T("replied to a thread")+":")
		})
		lx := x + pd.Size.X + dp(gtx, 4)
		link(lx, prev, right-lx-dp(gtx, 4))
		return h
	}
	// rt("replied to <a>a thread</a>"): the <a> is the link, wherever the
	// locale puts it (ja-JP leads with it)
	lx := x
	for _, p := range threadsNS.RT("replied to <a>a thread</a>") {
		if p.Tag == "a" {
			lx += link(lx, p.Text, right-lx)
			continue
		}
		txt := p.Text
		pd := at(gtx, lx, y, func(gtx layout.Context) layout.Dimensions {
			return styledLabel(gtx, rc.th, ps, textStyle{size: 15, col: c.Text2, lineH: 22}, txt)
		})
		lx += pd.Size.X
	}
	return h
}

// rootPreview is the broadcast's root as SimpleMessagePreview shows it
// (one line: the text with its formatting and line breaks flattened), or
// "" when Clack has no root with text: the channel's loaded messages
// first, then the store (threads Clack has open). Slack's preamble reads
// the root from its store and draws nothing until it's there; Clack keeps
// "replied to a thread" so the row doesn't change height when the root
// loads.
func (r *row) rootPreview(rc *rowCtx) string {
	var rev uint64
	if rc.list != nil {
		rev = rc.list.snap.rev
	}
	if r.rootChecked && r.rootRev == rev {
		return r.rootPrev
	}
	r.rootChecked, r.rootRev, r.rootPrev = true, rev, ""
	ts := r.m.ThreadTS
	var doc *richtext.Doc
	if rc.list != nil {
		ms := rc.list.snap.msgs
		i := sort.Search(len(ms), func(i int) bool { return !tsLess(ms[i].m.TS, ts) })
		if i < len(ms) && ms[i].m.TS == ts {
			doc = &ms[i].m.Doc
		}
	}
	if doc == nil && rc.h.Core() != nil && rc.h.Core().S != nil {
		if sm, ok := rc.h.Core().S.Find(r.m.Channel, ts); ok {
			cm := rc.h.Core().MessageFromSlack(r.m.Channel, &sm)
			doc = &cm.Doc
		}
	}
	if doc != nil {
		r.rootPrev = strings.Join(strings.Fields(richtext.PlainText(*doc)), " ")
	}
	return r.rootPrev
}

// reactionBar lays out the ReactionBar at pos (origin is pos's frame in
// row-local coordinates), width w; returns its height.
func (r *row) reactionBar(gtx layout.Context, pos, origin image.Point, w int) int {
	r.growPills()
	r.reactAt = origin.Add(pos)
	d := at(gtx, pos.X, pos.Y, func(gtx layout.Context) layout.Dimensions {
		gtx.Constraints = layout.Constraints{Max: image.Pt(w, 1<<20)}
		return slackc.Component(gtx, "ReactionBar", r, (*row).reactionBarProps, (*row).layoutReactionBar)
	})
	return d.Size.Y
}

// layoutReactionBar is ReactionBar: the pills and the add button, or the
// children a plugin passed (its own buttons beside Slack's).
func (r *row) layoutReactionBar(gtx layout.Context, p slackc.Props) layout.Dimensions {
	r.reactAs = p.StrOr("currentUser", "")
	r.reactRects = r.reactRects[:0]
	r.addRect = image.Rectangle{}
	st := reactionBarClass.Enter(gtx, 0, 0)
	defer st.Leave()
	if st.Hidden() {
		return layout.Dimensions{}
	}
	if e, ok := slackc.PropChildren(p); ok {
		// Slack's .c-reaction_bar is a flex-wrap row: the children (the
		// Reaction and add-button slots a plugin passed on, its own
		// buttons) are flex items, each placed like a pill
		items := slackc.Items(e)
		lay := func(gtx layout.Context) layout.Dimensions {
			return r.flowPills(gtx, len(items), func(gtx layout.Context, i int) layout.Dimensions {
				return slackc.LayoutElement(gtx, items[i])
			}, nil)
		}
		if st.Active() {
			return st.Wrap(gtx, lay)
		}
		return lay(gtx)
	}
	if st.Active() {
		return st.Wrap(gtx, func(gtx layout.Context) layout.Dimensions { return r.reactionPills(gtx) })
	}
	return r.reactionPills(gtx)
}

// reactionPills flows the Reaction pills, ReactionBar's expand or collapse
// button (reactionCollapse) and the add button.
func (r *row) reactionPills(gtx layout.Context) layout.Dimensions {
	n := len(r.groups)
	shown, more, sel := r.reactionCollapse(gtx, gtx.Constraints.Max.X)
	extra := 0
	if more != 0 {
		extra = 1
	}
	return r.flowPills(gtx, shown+extra+1, func(gtx layout.Context, i int) layout.Dimensions {
		switch {
		case i < shown:
			pl := &r.pills[i]
			return slackc.Component(gtx, "Reaction", pl, (*reactPill).props, (*reactPill).layout)
		case i < shown+extra:
			return r.expandButton(gtx, more, n-shown, sel)
		}
		// add-reaction button
		return slackc.Component(gtx, "ReactionAddButton", r, (*row).addButtonProps, (*row).layoutAddButton)
	}, func(i int, rect image.Rectangle) {
		switch {
		case i < shown:
			r.reactRects = append(r.reactRects, rect.Add(r.reactAt))
		case i >= shown+extra:
			r.addRect = rect.Add(r.reactAt)
		}
	})
}

// ReactionBar's buttons after the pills (reactionCollapse).
const (
	reactMore = 1 // ReactionExpandButton: "{n} more"
	reactLess = 2 // ReactionCollapseButton: "Show less"
)

// reactionCollapse is ReactionBar's collapse (boot-shared Ltiy,
// getDerivedStateFromProps and render), over the pills (getReactionGroups).
// With more than five pills Slack estimates each one (8px per digit of
// its count, 18 per emoji in it, 18) and, in the bar's width w, takes
// l = the pills that fit one line,
// collapseLimit = those that fit within the first line's pills plus w-38,
// and collapseTarget = the same less 64 more. Past collapseLimit the bar
// shows collapseTarget pills and "{n} more" until it's expanded, then all
// of them and "Show less". sel: one of the hidden reactions is the
// viewer's ("N more" takes the reacted look). Slack also opens the bar
// when the viewer's own new reaction lands among the hidden ones (a
// vendor helper this audit couldn't read); Clack doesn't.
func (r *row) reactionCollapse(gtx layout.Context, w int) (shown, more int, sel bool) {
	gs := r.groups
	n := len(gs)
	if n <= 5 || w <= 0 {
		return n, 0, false
	}
	bar := float64(w) / float64(max(gtx.Metric.PxPerDp, 0.01))
	est := func(i int) float64 {
		n := 0
		for _, k := range gs[i].idx {
			n += r.m.Reactions[k].Count
		}
		return 8*math.Floor(math.Log10(float64(max(n, 1)))+1) + 18*float64(len(gs[i].idx)) + 18
	}
	// fit is lodash sortedIndex over the running estimates: how many sums
	// stay under limit
	fit := func(limit float64) int {
		acc := 0.0
		for i := range gs {
			if acc += est(i); acc >= limit {
				return i
			}
		}
		return n
	}
	l := fit(bar)
	if l == 0 {
		return n, 0, false // r[l-1] is undefined: Slack's limit is NaN and nothing collapses
	}
	first := 0.0
	for i := 0; i < l; i++ {
		first += est(i)
	}
	limit, target := fit(first+bar-38), fit(first+bar-38-64)
	if n <= limit {
		return n, 0, false
	}
	if r.reactExpanded {
		return n, reactLess, false
	}
	for i := target; i < n; i++ {
		if (&r.pills[i]).reacted() {
			sel = true
			break
		}
	}
	return target, reactMore, sel
}

// expandButton is ReactionExpandButton ("{n} more", the tooltip "Show
// more reactions" after 300ms) or ReactionCollapseButton ("Show less"):
// .c-reaction_expand, the pill's box (24px, padding-inline 8, the 6%
// fill, the otl-pry ring under the pointer) with its 12px bold
// __overlay text; --reacted takes your reaction's colours.
func (r *row) expandButton(gtx layout.Context, kind, hidden int, sel bool) layout.Dimensions {
	rc := r.rc
	c := rc.c
	if r.reactExpand.Clicked(gtx) {
		r.reactExpanded = kind == reactMore
		rc.h.Invalidate()
	}
	txt := "Show less"
	if kind == reactMore {
		txt = strconv.Itoa(hidden) + " more"
	}
	ts := textStyle{size: 12, weight: theme.Bold, col: c.Text, lineH: 16}
	if sel {
		ts.col = c.ReactSelfText
	}
	ph := dp(gtx, mt.reactH)
	size := image.Pt(dp(gtx, 8)+measure(gtx, rc.th, ts, txt).X+dp(gtx, 8), ph)
	gtx.Constraints = layout.Exact(size)
	d := r.reactExpand.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
		rr := image.Rectangle{Max: size}
		rad := ph / 2
		switch {
		case sel:
			rrect(gtx.Ops, rr, rad, c.ReactSelfBg)
			border(gtx.Ops, rr, rad, dp(gtx, 1), c.ReactSelfBorder)
		case r.reactExpand.Hovered():
			border(gtx.Ops, rr, rad, dp(gtx, 1), c.ReactHoverBorder)
		default:
			rrect(gtx.Ops, rr, rad, c.ReactBg)
		}
		at(gtx, dp(gtx, 8), (ph-dp(gtx, 16))/2, func(gtx layout.Context) layout.Dimensions { return label(gtx, rc.th, ts, txt) })
		return layout.Dimensions{Size: size}
	})
	if kind == reactMore {
		r.expandTip.Delay = 300 * time.Millisecond
		popups.Tooltip(gtx, rc.th, &r.expandTip, r.reactExpand.Hovered(), size, popups.Above, "Show more reactions", "")
	}
	return d
}

// flowPills places n items left to right in rows of pill height, wrapping
// at the bar's width (the reaction bar's flex-wrap). placed, for the
// native bar, gets each item's rect (kept for overlays).
func (r *row) flowPills(gtx layout.Context, n int, item func(gtx layout.Context, i int) layout.Dimensions, placed func(i int, rect image.Rectangle)) layout.Dimensions {
	native := placed != nil
	w := gtx.Constraints.Max.X
	ph := dp(gtx, mt.reactH)
	gap := dp(gtx, mt.reactGap)
	cx, cy := 0, 0
	place := func(call op.CallOp, sz image.Point) image.Rectangle {
		if cx > 0 && cx+sz.X > w {
			cx = 0
			cy += ph + gap
		}
		t := op.Offset(image.Pt(cx, cy)).Push(gtx.Ops)
		call.Add(gtx.Ops)
		t.Pop()
		rect := image.Rect(cx, cy, cx+sz.X, cy+ph)
		cx += sz.X + gap
		return rect
	}
	g := gtx
	g.Constraints.Min = image.Point{}
	bottom := 0
	for i := 0; i < n; i++ {
		rec := op.Record(gtx.Ops)
		d := item(g, i)
		call := rec.Stop()
		if !native && d.Size.X == 0 && d.Size.Y == 0 {
			continue // a plugin's null child takes no place in the row
		}
		rect := place(call, d.Size)
		bottom = rect.Max.Y
		if native {
			placed(i, rect)
		}
	}
	return layout.Dimensions{Size: image.Pt(w, bottom)}
}

// layoutAddButton is ReactionAddButton (.c-reaction_add): always drawn
// after the pills, like Slack (34×24, rgba(29,28,29,.06), not only on hover).
func (r *row) layoutAddButton(gtx layout.Context, _ slackc.Props) layout.Dimensions {
	rc := r.rc
	ph := dp(gtx, mt.reactH)
	size := image.Pt(dp(gtx, 34), ph)
	if r.addReact.Clicked(gtx) {
		// popups' reaction picker reacts through submitAddOrRemoveReaction
		// with this message's channel/ts (OwlMode may redirect it)
		h := rc.h
		h.ShowOverlay(popups.NewReactionPicker(h, rc.origin(image.Point{}), r.src.Channel, r.src.TS, r.src.ThreadTS, nil))
	}
	st := reactionAddClass.Enter(gtx, btnState(gtx, &r.addReact), 0)
	defer st.Leave()
	if st.Hidden() {
		return layout.Dimensions{Size: size}
	}
	gtx.Constraints = layout.Exact(size)
	if st.Active() {
		return st.Wrap(gtx, func(gtx layout.Context) layout.Dimensions { return r.addButton(gtx, st, size) })
	}
	return r.addButton(gtx, st, size)
}

func (r *row) addButton(gtx layout.Context, st slackc.Styled, size image.Point) layout.Dimensions {
	c := r.rc.c
	ph := size.Y
	return r.addReact.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
		rr := image.Rectangle{Max: size}
		col := c.Text
		if r.addReact.Hovered() {
			border(gtx.Ops, rr, ph/2, dp(gtx, 1), c.ReactHoverBorder)
			col = rgb(0xecb22e)
		} else {
			rrect(gtx.Ops, rr, ph/2, st.Background(c.ReactBg))
		}
		col = st.Color(col)
		is := dp(gtx, 18)
		at(gtx, (rr.Dx()-is)/2, (rr.Dy()-is)/2, func(gtx layout.Context) layout.Dimensions {
			return icons.Get("add-reaction")(gtx, unit.Dp(18), col)
		})
		return layout.Dimensions{Size: size}
	})
}

// reacted reports whether the bar's current user reacted with any of the
// group's reactions (the group's users include them; the viewer, unless
// a plugin set ReactionBar's currentUser).
func (pl *reactPill) reacted() bool {
	r := pl.r
	self := ""
	if r.rc != nil {
		self = r.rc.selfID
	}
	as := r.reactAs
	if as == self {
		as = ""
	}
	for _, i := range r.groups[pl.i].idx {
		re := &r.m.Reactions[i]
		if as == "" && re.Self || as != "" && reactionHas(re, as, "") {
			return true
		}
	}
	return false
}

// layout is Reaction around the UnstyledButton pill.
func (pl *reactPill) layout(gtx layout.Context, p slackc.Props) layout.Dimensions {
	if e, ok := slackc.PropChildren(p); ok {
		return slackc.LayoutElement(gtx, e) // the button JS rendered in Reaction's tree
	}
	return slackc.Component(gtx, "UnstyledButton", pl, (*reactPill).buttonProps, (*reactPill).layoutButton)
}

// layoutButton is the pill: UnstyledButton data-qa="reactji", .c-reaction
// (--reacted for the viewer's own). Its children are the emoji and the
// count; a plugin may append to them (WhoReacted's avatars).
func (pl *reactPill) layoutButton(gtx layout.Context, p slackc.Props) layout.Dimensions {
	r := pl.r
	rc := r.rc
	if pl.btn.Clicked(gtx) && rc.onReactGroup != nil {
		rc.onReactGroup(r, r.groups[pl.i].name) // handleReactionClick (reaction_groups.go)
	}
	var mods uint64
	if pl.reacted() {
		mods = 1
	}
	st := reactionClass.Enter(gtx, btnState(gtx, &pl.btn), mods)
	defer st.Leave()
	if st.Hidden() {
		return layout.Dimensions{}
	}
	ph := dp(gtx, mt.reactH)
	rec := op.Record(gtx.Ops)
	var cd layout.Dimensions
	cy := 0
	if e, ok := slackc.PropChildren(p); ok {
		cd = slackc.LayoutElement(gtx, e)
		cy = (ph - cd.Size.Y) / 2
	} else {
		cd = pl.content(gtx, st)
	}
	content := rec.Stop()
	size := image.Pt(dp(gtx, 8)+cd.Size.X+dp(gtx, 8), ph)
	gtx.Constraints = layout.Exact(size)
	if st.Active() {
		return st.Wrap(gtx, func(gtx layout.Context) layout.Dimensions { return pl.pill(gtx, st, content, size, cy) })
	}
	return pl.pill(gtx, st, content, size, cy)
}

func (pl *reactPill) pill(gtx layout.Context, st slackc.Styled, content op.CallOp, size image.Point, cy int) layout.Dimensions {
	c := pl.r.rc.c
	self := pl.reacted()
	return pl.btn.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
		rr := image.Rectangle{Max: size}
		rad := size.Y / 2
		hov := pl.btn.Hovered()
		switch {
		case self && hov:
			rrect(gtx.Ops, rr, rad, st.Background(c.ReactSelfHoverBg))
			border(gtx.Ops, rr, rad, dp(gtx, 1), c.ReactSelfBorder)
		case self:
			rrect(gtx.Ops, rr, rad, st.Background(c.ReactSelfBg))
			border(gtx.Ops, rr, rad, dp(gtx, 1), c.ReactSelfBorder)
		case hov:
			rrect(gtx.Ops, rr, rad, st.Background(colorNone))
			border(gtx.Ops, rr, rad, dp(gtx, 1), c.ReactHoverBorder)
		default:
			rrect(gtx.Ops, rr, rad, st.Background(c.ReactBg))
		}
		t := op.Offset(image.Pt(dp(gtx, 8), cy)).Push(gtx.Ops)
		content.Add(gtx.Ops)
		t.Pop()
		return layout.Dimensions{Size: size}
	})
}

// content is the pill's native children, .c-reaction's inline-flex with
// gap 4: the group's emoji (the first ten), "…" past ten, then the count.
func (pl *reactPill) content(gtx layout.Context, st slackc.Styled) layout.Dimensions {
	ph := dp(gtx, mt.reactH)
	es := dp(gtx, mt.reactEmoji)
	gap := dp(gtx, 4)
	g := &pl.r.groups[pl.i]
	x := 0
	for k, i := range g.idx {
		if k == reactEmojiMax {
			break
		}
		re := &pl.r.m.Reactions[i]
		at(gtx, x, (ph-es)/2, func(gtx layout.Context) layout.Dimensions { return pl.emoji(gtx, re) })
		x += es + gap
	}
	if len(g.idx) > reactEmojiMax {
		d := at(gtx, x, (ph-dp(gtx, 16))/2, func(gtx layout.Context) layout.Dimensions { return pl.truncation(gtx, st) })
		x += d.Size.X + gap
	}
	d := at(gtx, x, (ph-dp(gtx, 16))/2, func(gtx layout.Context) layout.Dimensions { return pl.count(gtx, st) })
	return layout.Dimensions{Size: image.Pt(x+d.Size.X, ph)}
}

// layoutEmoji is one of the pill's Emoji children: the reaction its
// "emoji" prop names (the group's first without props).
func (pl *reactPill) layoutEmoji(gtx layout.Context, p slackc.Props) layout.Dimensions {
	r := pl.r
	g := &r.groups[pl.i]
	re := &r.m.Reactions[g.idx[0]]
	if name := p.StrOr("emoji", ""); name != "" {
		for _, i := range g.idx {
			if r.m.Reactions[i].Name == name {
				re = &r.m.Reactions[i]
				break
			}
		}
	}
	return pl.emoji(gtx, re)
}

// layoutTruncation is the pill's "…" after ten emoji
// (c-reaction__truncation).
func (pl *reactPill) layoutTruncation(gtx layout.Context, _ slackc.Props) layout.Dimensions {
	return pl.truncation(gtx, slackc.Styled{})
}

func (pl *reactPill) truncation(gtx layout.Context, st slackc.Styled) layout.Dimensions {
	rc := pl.r.rc
	cs := textStyle{size: 12, col: rc.c.Text, lineH: 16}
	if pl.reacted() {
		cs.col, cs.weight = rc.c.ReactSelfText, theme.Bold
	}
	cs.col = st.Color(cs.col)
	return label(gtx, rc.th, cs, "\u2026")
}

// emoji draws re's emoji (its alt text while the image loads).
func (pl *reactPill) emoji(gtx layout.Context, re *core.Reaction) layout.Dimensions {
	rc := pl.r.rc
	es := dp(gtx, mt.reactEmoji)
	url, alt := re.URL, ""
	if url == "" && rc.h.Core() != nil {
		url, alt, _ = rc.h.Core().Emoji(re.Name)
	}
	if !drawEmoji(gtx, rc.h, url, image.Rect(0, 0, es, es)) {
		if alt == "" {
			alt = ":" + re.Name + ":"
		}
		ph := dp(gtx, mt.reactH)
		at(gtx, 0, -(ph-es)/2, func(gtx layout.Context) layout.Dimensions {
			return label(gtx, rc.th, textStyle{size: 13, col: rc.c.Text, lineH: 24}, alt)
		})
	}
	return layout.Dimensions{Size: image.Pt(es, es)}
}

// layoutCount is the pill's count slot.
func (pl *reactPill) layoutCount(gtx layout.Context, _ slackc.Props) layout.Dimensions {
	return pl.count(gtx, slackc.Styled{})
}

func (pl *reactPill) count(gtx layout.Context, st slackc.Styled) layout.Dimensions {
	rc := pl.r.rc
	cs := textStyle{size: 12, col: rc.c.Text, lineH: 16}
	if pl.reacted() {
		cs.col, cs.weight = rc.c.ReactSelfText, theme.Bold
	}
	cs.col = st.Color(cs.col)
	return label(gtx, rc.th, cs, reactionCount(pl.r.groups[pl.i].count))
}

// failedLine is "Message failed to send. Retry · Delete" under a failed
// optimistic send.
func (r *row) failedLine(gtx layout.Context, rc *rowCtx, x, y int) int {
	c := rc.c
	lh := dp(gtx, 20)
	is := dp(gtx, 14)
	at(gtx, x, y+(lh-is)/2+dp(gtx, 2), func(gtx layout.Context) layout.Dimensions {
		return icons.Get("warning")(gtx, unit.Dp(14), c.FailedText)
	})
	s := textStyle{size: 13, col: c.FailedText, lineH: 20}
	xx := x + is + dp(gtx, 4)
	d := at(gtx, xx, y+dp(gtx, 2), func(gtx layout.Context) layout.Dimensions { return label(gtx, rc.th, s, "Message failed to send.") })
	xx += d.Size.X + dp(gtx, 4)
	link := func(btn *widget.Clickable, txt string) {
		d := at(gtx, xx, y+dp(gtx, 2), func(gtx layout.Context) layout.Dimensions {
			return btn.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
				ls := textStyle{size: 13, weight: theme.Bold, col: c.Link, lineH: 20}
				dd := label(gtx, rc.th, ls, txt)
				if btn.Hovered() {
					base := dd.Size.Y - dd.Baseline
					fill(gtx.Ops, image.Rect(0, base+dp(gtx, 1), dd.Size.X, base+dp(gtx, 2)), c.Link)
				}
				return dd
			})
		})
		xx += d.Size.X
	}
	if r.retryBtn.Clicked(gtx) && rc.onRetry != nil {
		rc.onRetry(r)
	}
	if r.discardBtn.Clicked(gtx) && rc.onDiscard != nil {
		rc.onDiscard(r)
	}
	link(&r.retryBtn, "Retry")
	d = at(gtx, xx, y+dp(gtx, 2), func(gtx layout.Context) layout.Dimensions { return label(gtx, rc.th, s, " · ") })
	xx += d.Size.X
	link(&r.discardBtn, "Delete")
	return lh + dp(gtx, 4)
}

// systemLayout draws join/leave/topic messages: a 16px icon in the
// avatar column, 13px text and an inline 12px time.
func (r *row) systemLayout(gtx layout.Context, rc *rowCtx) layout.Dimensions {
	if rc.dense {
		return r.denseSystemLayout(gtx, rc)
	}
	// .textRow__DEPF9 / .text__nwvni: 13px, line-height 1.5 in both
	// layouts (richview's Compact text, 13px/19.5)
	o := r.rvOpts()
	o.Compact = true
	r.rv.SetOptions(o)
	c := rc.c
	m := &r.m
	w := gtx.Constraints.Max.X
	padX := dp(gtx, rc.padX())
	cx := dp(gtx, rc.contentX())
	cw := w - cx - padX - dp(gtx, 20) - rc.actionsPad(gtx)
	rec := op.Record(gtx.Ops)
	icon := redesignedIcon(rc.h.Core(), m) // getRedesignedSystemMessageIcon (system.go)
	iconCol := c.Text3
	hm, huddle := r.huddleInfo(rc)
	if huddle {
		// HuddleSystemMessageIcon: headphones, content-hgl-2 while live
		// (.systemIconHuddleLive__sh4y9)
		icon = icons.Slack("headphones")
		if hm.room.Live {
			iconCol = c.HuddleLive
		}
	}
	// .systemIcon__mZbDL: a 36px box (the avatar's), the 16px glyph
	// centred, padding-top 2px
	is := dp(gtx, 16)
	at(gtx, padX+(dp(gtx, mt.avatar)-is)/2, dp(gtx, mt.padTop)+dp(gtx, 2), func(gtx layout.Context) layout.Dimensions {
		return icons.Get(icon)(gtx, unit.Dp(16), iconCol)
	})
	r.handler.h, r.handler.m, r.handler.anchor = rc.h, m, rc.origin
	// the column's first line (4px under the row top) and .container__OsSxI
	// { padding: 4px 20px 4px 0 }
	y := dp(gtx, mt.padTop) - dp(gtx, 4) + dp(gtx, 4)
	body := at(gtx, cx, y, func(gtx layout.Context) layout.Dimensions {
		gtx.Constraints = layout.Constraints{Max: image.Pt(cw, 1<<20)}
		return r.rv.Layout(gtx, rc.st, &r.handler)
	})
	r.bodyRect = image.Rectangle{Min: image.Pt(cx, y), Max: image.Pt(cx+body.Size.X, y+body.Size.Y)}
	// <time class="timestamp__bfLC8">: 12px inline after the text
	// (margin-inline-end 8px), on its baseline
	ts := clock(tsTime(m.TS))
	tst := tsClass.Enter(gtx, 0, 0)
	tsS := cssText(tst, textStyle{size: 12, col: c.TimeSystem, lineH: 19.5})
	tsz := measure(gtx, rc.th, tsS, ts)
	if body.Size.Y <= dp(gtx, 20) && body.Size.X+dp(gtx, 8)+tsz.X <= cw {
		trec := op.Record(gtx.Ops)
		td := styledLabel(gtx, rc.th, tst, tsS, ts)
		call := trec.Stop()
		ty := y
		if bl := r.rv.FirstBaseline(); bl >= 0 && td.Size.Y > 0 {
			ty = y + bl - (td.Size.Y - td.Baseline)
		}
		placeCall(gtx, call, cx+body.Size.X+dp(gtx, 8), ty)
		y += max(body.Size.Y, dp(gtx, 19.5))
	} else {
		y += body.Size.Y
		at(gtx, cx, y, func(gtx layout.Context) layout.Dimensions { return styledLabel(gtx, rc.th, tst, tsS, ts) })
		y += tsz.Y
	}
	tst.Leave()
	if huddle && hm.room.Joinable {
		// .contentWithHugAttachment__sm2TO { gap: 4px }, then the button
		// (.attachment__e73SZ: align-self flex-start)
		y += dp(gtx, 4)
		y += r.huddleJoinButton(gtx, rc, hm, image.Pt(cx, y))
	}
	if len(m.Reactions) > 0 {
		y += dp(gtx, 4)
		y += r.reactionBar(gtx, image.Pt(cx, y), image.Point{}, cw)
		y += dp(gtx, 4)
	} else {
		r.reactRects = r.reactRects[:0]
	}
	content := rec.Stop()
	h := y + dp(gtx, 8)
	if rc.hovered {
		fill(gtx.Ops, image.Rect(0, 0, w, h), c.Hover)
	}
	content.Add(gtx.Ops)
	r.height = h
	return layout.Dimensions{Size: image.Pt(w, h)}
}

// denseSystemLayout is a system message under Compact. In a channel,
// Message's gutter is getCompactGutter for every row, so the time sits
// in the 56px box where Clean has the icon, and SystemMessage drops its
// inline time ("dense" → no <time>) and puts its text in
// containerCompact (margin-top 2px, padding 0 0 4px) instead of
// container (padding 4px 20px 4px 0), the body's 4px :after under it.
// In a thread the gutter has no left side; ThreadReplyGeneric keeps the
// sender and time line over the text (it hides it only for "light").
func (r *row) denseSystemLayout(gtx layout.Context, rc *rowCtx) layout.Dimensions {
	c := rc.c
	m := &r.m
	w := gtx.Constraints.Max.X
	padX := dp(gtx, rc.padX())
	cx := dp(gtx, rc.contentX())
	cw := w - cx - padX
	if rc.thread {
		cw = w - cx - dp(gtx, 16)
	}
	cw = max(cw-rc.actionsPad(gtx), dp(gtx, 80))
	rec := op.Record(gtx.Ops)
	top := dp(gtx, mt.padTop) - dp(gtx, 4) // the gutter's first line
	y := top
	if rc.thread {
		d := at(gtx, cx, y, func(gtx layout.Context) layout.Dimensions {
			gtx.Constraints = layout.Constraints{Max: image.Pt(cw, 1<<20)}
			return slackc.Component(gtx, "ThreadSenderAndTimestampGeneric", r, (*row).threadSenderProps, (*row).layoutThreadSender)
		})
		y += max(d.Size.Y, dp(gtx, mt.nameLine))
	} else {
		at(gtx, padX, y, func(gtx layout.Context) layout.Dimensions {
			gtx.Constraints = layout.Constraints{Max: image.Pt(dp(gtx, mt.denseTS), 1<<20)}
			return r.layoutCompactTS(gtx, nil)
		})
	}
	y += dp(gtx, 2)
	r.handler.h, r.handler.m, r.handler.anchor = rc.h, m, rc.origin
	// .textRow__DEPF9 / .text__nwvni: 13px, line-height 1.5 (richview's
	// Compact text, 13px/19.5)
	o := r.rvOpts()
	o.Compact = true
	r.rv.SetOptions(o)
	body := at(gtx, cx, y, func(gtx layout.Context) layout.Dimensions {
		gtx.Constraints = layout.Constraints{Max: image.Pt(cw, 1<<20)}
		return r.rv.Layout(gtx, rc.st, &r.handler)
	})
	r.bodyRect = image.Rectangle{Min: image.Pt(cx, y), Max: image.Pt(cx+body.Size.X, y+body.Size.Y)}
	y += max(body.Size.Y, dp(gtx, 20)) + dp(gtx, 4) + dp(gtx, 4)
	if len(m.Reactions) > 0 {
		y += r.reactionBar(gtx, image.Pt(cx, y), image.Point{}, cw)
		y += dp(gtx, 4)
	} else {
		r.reactRects = r.reactRects[:0]
	}
	content := rec.Stop()
	h := y
	if rc.hovered {
		fill(gtx.Ops, image.Rect(0, 0, w, h), c.Hover)
	}
	content.Add(gtx.Ops)
	r.height = h
	return layout.Dimensions{Size: image.Pt(w, h)}
}

// tombstoneLayout is Slack's MessageTombstone (boot-shared 2X8J): a
// deleted message that still has replies (subtype "tombstone", user
// USLACKBOT, hidden) draws c-message_kit__tombstone: the 36px placeholder
// with the trash icon where the avatar goes, "This message was deleted."
// in c-message_kit__tombstone__text, then the reply bar. No author, no
// timestamp, no hover actions. A plugin view (Message Logger's kept copy)
// replaces r.m and so draws as the message instead.
func (r *row) tombstoneLayout(gtx layout.Context, rc *rowCtx) layout.Dimensions {
	c := rc.c
	m := &r.m
	w := gtx.Constraints.Max.X
	padX := dp(gtx, mt.padX)
	cx := dp(gtx, mt.contentX)
	cw := w - cx - padX
	if rc.thread {
		cw = w - cx - dp(gtx, 24)
	}
	cw = max(cw, dp(gtx, 80))
	if rc.dense {
		return r.denseTombstoneLayout(gtx, rc)
	}
	rec := op.Record(gtx.Ops)
	top := dp(gtx, mt.padTop)
	av := dp(gtx, mt.avatar)
	sq := image.Rect(padX, top, padX+av, top+av)
	rrect(gtx.Ops, sq, dp(gtx, 8), c.BadgeBg)
	is := dp(gtx, 20)
	at(gtx, padX+(av-is)/2, top+(av-is)/2, func(gtx layout.Context) layout.Dimensions {
		return icons.Get("trash")(gtx, unit.Dp(20), c.Text2)
	})
	r.handler.h, r.handler.m, r.handler.anchor = rc.h, m, rc.origin
	// .c-message_kit__tombstone__right { margin: -4px 0 }; its
	// .c-message_kit__tombstone__text { min-height: 44px; display: flex;
	// align-items: center } centres the line in 44px, the reply bar
	// right under it (margin -4px 0 4px, 34px)
	y := top - dp(gtx, 4)
	rec2 := op.Record(gtx.Ops)
	g := gtx
	g.Constraints = layout.Constraints{Max: image.Pt(cw, 1<<20)}
	body := r.rv.Layout(g, rc.st, &r.handler)
	call := rec2.Stop()
	box := max(body.Size.Y, dp(gtx, 44))
	ty := y + (box-body.Size.Y)/2
	placeCall(gtx, call, cx, ty)
	r.bodyRect = image.Rectangle{Min: image.Pt(cx, ty), Max: image.Pt(cx+body.Size.X, ty+body.Size.Y)}
	y += box
	r.reactRects = r.reactRects[:0]
	r.addRect = image.Rectangle{}
	if !rc.thread && (m.ThreadTS == "" || m.ThreadTS == m.TS) && r.replyBarWanted(m.ReplyCount) {
		d := at(gtx, cx, y, func(gtx layout.Context) layout.Dimensions {
			gtx.Constraints = layout.Constraints{Max: image.Pt(cw, 1<<20)}
			return slackc.Component(gtx, "ThreadReplies", r, (*row).repliesProps, (*row).layoutThreadReplies)
		})
		y += d.Size.Y
	}
	y -= dp(gtx, 4)
	h := max(y, top+av) + dp(gtx, mt.padBottom)
	content := rec.Stop()
	content.Add(gtx.Ops)
	r.height = h
	return layout.Dimensions{Size: image.Pt(w, h)}
}

// denseTombstoneLayout is MessageTombstone under Compact
// (c-message_kit__tombstone--compact): no placeholder square ("light"
// draws it), so the text starts at the row's padding;
// .c-message_kit__tombstone__right { margin: -4px 0 } puts it 4px below
// the row top. .c-message_kit__tombstone__text--compact { height: auto;
// margin-bottom: 4px } keeps the base rule's min-height 44px, a flex box
// with align-items center: the line is centred in 44px, then 4px.
func (r *row) denseTombstoneLayout(gtx layout.Context, rc *rowCtx) layout.Dimensions {
	m := &r.m
	w := gtx.Constraints.Max.X
	x := dp(gtx, rc.padX())
	cw := w - 2*x
	if rc.thread {
		cw = w - x - dp(gtx, 24)
	}
	cw = max(cw, dp(gtx, 80))
	r.handler.h, r.handler.m, r.handler.anchor = rc.h, m, rc.origin
	y := dp(gtx, mt.padTop) - dp(gtx, 4)
	rec := op.Record(gtx.Ops)
	g := gtx
	g.Constraints = layout.Constraints{Max: image.Pt(cw, 1<<20)}
	body := r.rv.Layout(g, rc.st, &r.handler)
	call := rec.Stop()
	box := max(body.Size.Y, dp(gtx, 44))
	ty := y + (box-body.Size.Y)/2
	placeCall(gtx, call, x, ty)
	r.bodyRect = image.Rectangle{Min: image.Pt(x, ty), Max: image.Pt(x+body.Size.X, ty+body.Size.Y)}
	y += box + dp(gtx, mt.bodyMarginBottom)
	r.reactRects = r.reactRects[:0]
	r.addRect = image.Rectangle{}
	if !rc.thread && (m.ThreadTS == "" || m.ThreadTS == m.TS) && r.replyBarWanted(m.ReplyCount) {
		d := at(gtx, x, y, func(gtx layout.Context) layout.Dimensions {
			gtx.Constraints = layout.Constraints{Max: image.Pt(cw, 1<<20)}
			return slackc.Component(gtx, "ThreadReplies", r, (*row).repliesProps, (*row).layoutThreadReplies)
		})
		y += d.Size.Y
	}
	h := y + dp(gtx, mt.padBottom) - dp(gtx, 4)
	r.height = h
	return layout.Dimensions{Size: image.Pt(w, h)}
}

// replyBarWanted is whether ThreadReplies draws a reply bar for a message
// with count replies: also for a thread with a draft or scheduled replies
// and none sent yet (reply_bar.go replyBarShown).
func (r *row) replyBarWanted(count int) bool {
	if count > 0 {
		return true
	}
	if r.rc == nil || r.m.Pending || r.m.Tombstone || r.m.System {
		return false
	}
	draft, sched := r.threadDrafts(r.rc.h.Core())
	return replyBarShown(count, draft, sched, r.threadOpen())
}

// threadBar draws the reply summary; returns its height.
func (r *row) threadBar(gtx layout.Context, rc *rowCtx, x, y, w, count int, users []string, latest string) int {
	c := rc.c
	m := &r.m
	bh := dp(gtx, mt.threadH)
	bw := w + dp(gtx, 5)
	if mx := dp(gtx, 600); bw > mx {
		bw = mx
	}
	bx := x - dp(gtx, 5)
	if r.threadBtn.Clicked(gtx) {
		rc.h.OpenThread(m.Channel, m.TS)
		if f, ok := rc.h.(ThreadComposerFocuser); ok { // openThread's setFocusInput (reply_bar.go)
			f.FocusThreadComposer(m.Channel, m.TS)
		}
	}
	bs := replyBarClass.Enter(gtx, btnState(gtx, &r.threadBtn), 0)
	defer bs.Leave()
	if bs.Hidden() {
		return 0
	}
	at(gtx, bx, y-dp(gtx, 4), func(gtx layout.Context) layout.Dimensions {
		gtx.Constraints = layout.Exact(image.Pt(bw, bh))
		return r.threadBtn.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
			rr := image.Rectangle{Max: image.Pt(bw, bh)}
			hov := r.threadBtn.Hovered()
			if hov {
				rrect(gtx.Ops, rr, dp(gtx, 6), c.ThreadBg)
				border(gtx.Ops, rr, dp(gtx, 6), dp(gtx, 1), c.ThreadBorder)
			}
			pad := dp(gtx, 5)
			as := dp(gtx, mt.threadAvatar)
			ax := pad
			users, over := replyBarFaces(users, m.ReplyUsersN) // renderFaces (reply_bar.go)
			if len(r.replyAvs) < len(users) {
				r.replyAvs = append(r.replyAvs, make([]replyAv, len(users)-len(r.replyAvs))...)
			}
			r.replyN = len(users)
			for i, id := range users {
				av := &r.replyAvs[i]
				av.r, av.id, av.sz = r, id, as
				g := gtx
				g.Constraints = layout.Constraints{Max: image.Pt(max(bw-ax, as), as)}
				t := op.Offset(image.Pt(ax, (bh-as)/2)).Push(gtx.Ops)
				d := slackc.Component(g, "ReplyBarAvatar", av, (*replyAv).props, (*replyAv).layout)
				if i == 4 && over > 0 && d.Size.X > 0 {
					replyOverlay(gtx, rc.th, over, as)
				}
				t.Pop()
				av.w = d.Size.X
				if d.Size.X > 0 {
					ax += d.Size.X + dp(gtx, 4)
				}
			}
			ax += dp(gtx, 3)
			draft, sched := r.threadDrafts(rc.h.Core())
			n, icon := replyBarLink(count, draft, sched, r.threadOpen()) // reply_bar.go
			r.replyLink = n
			ty := (bh - dp(gtx, 18)) / 2
			cs := replyCountClass.Enter(gtx, 0, 0)
			ls := textStyle{size: 13, weight: theme.Bold, col: c.Link, lineH: 18}
			if icon != "" {
				// the inline Icon (size inherit: the link's 13px), then a space
				em := dp(gtx, 13)
				at(gtx, ax, ty+(dp(gtx, 18)-em)/2, func(gtx layout.Context) layout.Dimensions {
					return icons.Get(icon+"-glyph")(gtx, unit.Dp(13), cs.Color(ls.col))
				})
				ax += em
				if n != "" {
					ax += measure(gtx, rc.th, ls, " ").X
				}
			}
			nd := at(gtx, ax, ty, func(gtx layout.Context) layout.Dimensions {
				return styledLabel(gtx, rc.th, cs, ls, n)
			})
			cs.Leave()
			ax += nd.Size.X + dp(gtx, 8)
			// ReplyBar's description: under
			// unread_thread_indication_in_replybar (on for the account) only
			// the last reply (getThreadLatestReplyText, util.go), in the
			// span that shows on hover (reply_bar_hover_last_reply), and at
			// rest the "Unread" mark while the thread has unread replies
			// (renderUnreadThreadsIndicator, unread_threads.go); without it
			// the last reply at rest and "View thread" on hover.
			desc := ""
			unread := 0
			switch app := rc.h.Core(); {
			case count == 0: // a draft or scheduled replies only (reply_bar.go)
				desc = r.replyBarNoReplies(rc.now, hov)
			case app != nil && app.Experiment("unread_thread_indication_in_replybar") == "on":
				if hov && latest != "" {
					desc = lastReply(tsTime(latest), rc.now, count)
				} else if !hov && rc.list != nil && !rc.thread {
					unread = rc.list.unreadThreadsFor(m.TS)
				}
			case hov:
				desc = threadsNS.T("View thread") // the threads I18n
			case latest != "":
				desc = lastReply(tsTime(latest), rc.now, count)
			}
			ds := replyDescClass.Enter(gtx, 0, 0)
			r.unreadMark = unread > 0
			if unread > 0 {
				// .c-message__reply_bar_unread_thread: the 8px dot
				// (icon-placeholder-filled, a circle of r 9 in 20), 4px,
				// "Unread", in content-imp
				col := ds.Color(c.Important)
				d := dp(gtx, 8)
				rad := float32(d) * 9 / 20
				cx, cy := float32(ax)+float32(d)/2, float32(ty)+float32(dp(gtx, 18))/2
				paint.FillShape(gtx.Ops, col, clip.Ellipse{Min: image.Pt(int(cx-rad+0.5), int(cy-rad+0.5)), Max: image.Pt(int(cx+rad+0.5), int(cy+rad+0.5))}.Op(gtx.Ops))
				at(gtx, ax+d+dp(gtx, 4), ty, func(gtx layout.Context) layout.Dimensions {
					return styledLabel(gtx, rc.th, ds, textStyle{size: 13, col: col, lineH: 18}, threadsNS.T("Unread")) // ReplyBar's (the threads I18n)
				})
			} else {
				at(gtx, ax, ty, func(gtx layout.Context) layout.Dimensions {
					gtx.Constraints.Max.X = bw - ax - dp(gtx, 28)
					return styledLabel(gtx, rc.th, ds, textStyle{size: 13, col: c.Text2, lineH: 18}, desc)
				})
			}
			ds.Leave()
			if hov {
				is := dp(gtx, 18)
				at(gtx, bw-pad-is, (bh-is)/2, func(gtx layout.Context) layout.Dimensions {
					return icons.Get("chevron-right")(gtx, unit.Dp(18), c.Edited)
				})
			}
			return layout.Dimensions{Size: rr.Size()}
		})
	})
	return bh - dp(gtx, 4)
}

// reactionTip is the reaction tooltip's text, Slack's getTip (boot-shared
// Qvn6, the reactions I18n): every reactor by name, the viewer as "You"
// first or "you" after (TooltipMemberName, channel_token), joined by
// listify ("A and B", "A, B, and C"; ja-JP "A、 B"), and "others" at the
// end once the count passes 50 (the API stops listing reactors); then
// "reacted with :name:" in the tooltip's regular-weight subtitle. Each is
// one of Slack's rich strings ("{users} <span className=
// "c-tooltip__subtitle">reacted with {emoji}</span>"), so the subtitle's
// words and their order are the locale's: ja-JP puts the name first and
// "さんが :tada: でリアクションしました" after it. The viewer alone reads
// "You (click to remove)" (showClickToRemove). The runs are marked with
// tipSubOn/tipSubOff (i18n.go).
func reactionTip(h Host, re *core.Reaction, selfID string) string {
	return reactionTipLine(h, re, selfID, false)
}

// reactionTipLine is getTip for one reaction of a pill's group; cut is
// ReactionTip's E (the group's count past 50 over several reactions):
// only the first ten users, and "others" once the reaction's count passes
// 10.
func reactionTipLine(h Host, re *core.Reaction, selfID string, cut bool) string {
	emoji := ":" + re.Name + ":"
	users, over := re.Users, re.Count > 50
	if cut {
		users, over = users[:min(len(users), 10)], re.Count > 10
	}
	if len(users) == 1 && users[0] == selfID {
		lastTip.txt, lastTip.count = reactionTipRuns(tipYouRemove, "emoji", emoji), re.Count
		return lastTip.txt
	}
	names := make([]string, 0, len(users)+1)
	self := false
	for i, id := range users {
		if id == selfID {
			self = true
			if i == 0 {
				names = append(names, tokenNS.T("You"))
			} else {
				names = append(names, tokenNS.T("you"))
			}
			continue
		}
		n := id
		if app := h.Core(); app != nil {
			if u, ok := app.User(id); ok && u.Name != "" {
				n = u.Name
			} else {
				ensureUser(h, id)
			}
		}
		names = append(names, n)
	}
	if len(names) > 1 && over {
		names = append(names, reactionsNS.T("others"))
	}
	var txt string
	switch {
	case len(names) == 1 && !self:
		txt = reactionTipRuns(tipUser, "user", names[0], "emoji", emoji)
	case self:
		txt = reactionTipRuns(tipUsersAndYou, "users_and_you", listify(names), "emoji", emoji)
	default:
		txt = reactionTipRuns(tipUsers, "users", listify(names), "emoji", emoji)
	}
	lastTip.txt, lastTip.count = txt, re.Count
	return txt
}

// getTip's strings (the reactions I18n), as the pinned bundles key them.
const (
	tipYouRemove   = `You (click to remove) <span className="c-tooltip__subtitle">reacted with {emoji}</span>`
	tipUser        = `{user} <span className="c-tooltip__subtitle">reacted with {emoji}</span>`
	tipUsersAndYou = `{users_and_you} <span className="c-tooltip__subtitle">reacted with {emoji}</span>`
	tipUsers       = `{users} <span className="c-tooltip__subtitle">reacted with {emoji}</span>`
)

// reactionTipSubtitle starts an unmarked tooltip's subtitle run (a plain
// text tooltip: tests, plugins).
const reactionTipSubtitle = "reacted with"

// listify joins names as Slack's listify does in the locale: "A", "A and
// B", "A, B, and C" in en-US (the serial comma from three on).
func listify(names []string) string { return i18n.Listify(names, false) }

// reactionAria is the pill's aria-label (Reaction 9FoR): "{count}
// reaction(s), react with {name} emoji", the name's _ and - read as spaces.
func reactionAria(re *core.Reaction) string {
	word := "reactions"
	if re.Count == 1 {
		word = "reaction"
	}
	return strconv.Itoa(re.Count) + " " + word + ", react with " + emojiPhrase.Replace(re.Name) + " emoji"
}

// emojiPhrase reads an emoji name as words (Lf9r: _ and - become spaces).
var emojiPhrase = strings.NewReplacer("_", " ", "-", " ")

// reactionCount is the pill's count as formatNumber writes it (en-US
// grouping: 1,234).
func reactionCount(n int) string {
	s := strconv.Itoa(n)
	if n < 1000 && n > -1000 {
		return s
	}
	neg := strings.HasPrefix(s, "-")
	s = strings.TrimPrefix(s, "-")
	var b strings.Builder
	if neg {
		b.WriteByte('-')
	}
	for i, ch := range s {
		if i > 0 && (len(s)-i)%3 == 0 {
			b.WriteByte(',')
		}
		b.WriteRune(ch)
	}
	return b.String()
}

// skeleton draws a loading placeholder row.
func skeleton(gtx layout.Context, c *colors, i int) layout.Dimensions {
	w := gtx.Constraints.Max.X
	h := dp(gtx, 60)
	padX := dp(gtx, mt.chanPadX)
	a := dp(gtx, mt.avatar)
	rrect(gtx.Ops, image.Rect(padX, dp(gtx, 8), padX+a, dp(gtx, 8)+a), avatarRadius(a), c.Skeleton)
	cx := dp(gtx, mt.chanContentX)
	widths := []int{120, 90, 160}
	nw := dp(gtx, float32(widths[i%3]))
	rrect(gtx.Ops, image.Rect(cx, dp(gtx, 10), cx+nw, dp(gtx, 22)), dp(gtx, 6), c.Skeleton)
	bw := (w - cx - padX) * (60 + (i*23)%35) / 100
	rrect(gtx.Ops, image.Rect(cx, dp(gtx, 30), cx+bw, dp(gtx, 42)), dp(gtx, 6), c.Skeleton)
	return layout.Dimensions{Size: image.Pt(w, h)}
}

// pinnedRuns appends PinnedLabel (boot-shared GSoE) to runs: "Pinned by
// you" for the viewer, "Pinned" without a pinner, else the message
// string "Pinned by {member}" whose member is renderPinnedBy's MemberLink
// (25Rg, class c-message_kit__labels__link): MemberName's bold name, a
// ButtonLink in the label's colour, underlined under the pointer; a
// click opens the member's profile (openProfile) and the pointer resting
// on it opens their hover card (ProfileHoverTrigger, QvaD). Until core
// knows the member the label reads "Pinned".
func (r *row) pinnedRuns(gtx layout.Context, rc *rowCtx, runs []labelRun) []labelRun {
	by := r.m.PinnedBy
	if r.pinnedLink.Clicked(gtx) && by != "" {
		rc.h.OpenProfile(rc.origin(image.Point{}), by)
	}
	name := ""
	if by != "" && by != rc.selfID {
		if app := rc.h.Core(); app != nil {
			if u, ok := app.User(by); ok && u.Name != "" {
				name = u.Name
			} else {
				ensureUser(rc.h, by)
			}
		}
	}
	if name == "" {
		return append(runs, labelRun{text: pinnedLabel(rc, by)})
	}
	for _, p := range msgNS.RT("Pinned by {member}", "member", "\x00") {
		if p.Arg == "member" {
			runs = append(runs, labelRun{text: name, btn: &r.pinnedLink, bold: true, hov: &r.pinnedHov, user: by})
			continue
		}
		runs = append(runs, labelRun{text: p.Text})
	}
	return runs
}

// pinnedLabel is PinnedLabel's text when it has no link: "Pinned by
// you", "Pinned" (and "Pinned by Name" in plain text, for callers that
// draw no link).
func pinnedLabel(rc *rowCtx, by string) string {
	switch {
	case by == "":
		return msgNS.T("Pinned")
	case by == rc.selfID:
		return msgNS.T("Pinned by you")
	}
	if app := rc.h.Core(); app != nil {
		if u, ok := app.User(by); ok && u.Name != "" {
			return msgNS.T("Pinned by {member}", "member", u.Name)
		}
		ensureUser(rc.h, by)
	}
	return msgNS.T("Pinned")
}

// ProgressiveDisclosureSuccessCoachmarkWrapper wraps the message's gutter
// and content in Slack (props.msg); Clack has no coachmarks, so its native
// is the gutter. Plugins provide context here (ShowSendingBot's sender for
// grouped rows' compact gutters).
func (r *row) coachmarkProps() slackc.Props {
	return slackc.Props{"$key": r.ts, "msg": r.message(), "coachmarkName": "", "isOpen": false}
}

func (r *row) layoutCoachmark(gtx layout.Context, _ slackc.Props) layout.Dimensions {
	return slackc.Component(gtx, "MessageKitGutter", r, (*row).gutterProps, (*row).layoutGutter)
}

// placeCall replays a recorded layout at (x, y).
func placeCall(gtx layout.Context, c op.CallOp, x, y int) {
	t := op.Offset(image.Pt(x, y)).Push(gtx.Ops)
	c.Add(gtx.Ops)
	t.Pop()
}

// nbsps caches the message font's no-break space advance per scale.
var nbsps = map[float32]int{}

// nbspWidth is the advance of the " " getCompactMessageBody puts
// between the sender and the text (the 15px message font).
func nbspWidth(gtx layout.Context, th *theme.Theme) int {
	k := gtx.Metric.PxPerSp
	if w, ok := nbsps[k]; ok {
		return w
	}
	s := textStyle{size: 15}
	w := measure(gtx, th, s, "x x").X - measure(gtx, th, s, "xx").X
	nbsps[k] = max(w, 0)
	return nbsps[k]
}

// inlineBody reports whether, under Compact, the body's first line flows
// on from the sender: CompactBlocks renders the blocks inline only when
// every block is rich_text (a message without blocks is its inline
// text, MessageText inline), and inside them only the first
// .p-rich_text_section is inline (.p-rich_text_block--inline); a list, a
// quote, a code block, a file or a card starts on the next line.
func (r *row) inlineBody() bool {
	bs := r.rv.Doc().Blocks
	if len(bs) == 0 || bs[0].Kind != richtext.Paragraph {
		return false
	}
	raw := r.m.Blocks
	if len(raw) == 0 {
		return true
	}
	if len(r.richRaw) != len(raw) || &r.richRaw[0] != &raw[0] {
		r.richRaw, r.allRich = raw, allRichText(raw)
	}
	return r.allRich
}

// allRichText: msg.blocks is empty or only rich_text blocks.
func allRichText(raw []byte) bool {
	if len(raw) == 0 {
		return true
	}
	var bs []struct {
		Type string `json:"type"`
	}
	if json.Unmarshal(raw, &bs) != nil {
		return false
	}
	for _, b := range bs {
		if b.Type != "rich_text" {
			return false
		}
	}
	return true
}

// statusBorder strokes Compact's .c-custom_status box around content r
// (1px border, 1px 2px padding, radius 5) and returns the left inset (3px)
// content r moves by.
func statusBorder(gtx layout.Context, c *colors, r image.Rectangle) int {
	in := dp(gtx, 3)
	b := image.Rect(r.Min.X, r.Min.Y-dp(gtx, 2), r.Max.X+2*in, r.Max.Y+dp(gtx, 2))
	border(gtx.Ops, b, dp(gtx, 5), max(dp(gtx, 1), 1), c.HeaderBorder)
	return in
}
