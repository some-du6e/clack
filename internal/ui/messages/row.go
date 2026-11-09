package messages

import (
	"context"
	"gioui.org/io/event"
	"gioui.org/io/pointer"
	"gioui.org/op/clip"
	"github.com/some-du6e/yoloslackgoclient/internal/ui/popups"
	"image"
	"image/color"
	"strconv"
	"strings"
	"time"

	"gioui.org/layout"
	"gioui.org/op"
	"gioui.org/op/paint"
	"gioui.org/unit"
	"gioui.org/widget"

	"github.com/some-du6e/yoloslackgoclient/internal/core"
	"github.com/some-du6e/yoloslackgoclient/internal/ui/composer"
	"github.com/some-du6e/yoloslackgoclient/internal/ui/ctxclick"
	"github.com/some-du6e/yoloslackgoclient/internal/ui/hovercard"
	"github.com/some-du6e/yoloslackgoclient/internal/ui/icons"
	"github.com/some-du6e/yoloslackgoclient/internal/ui/richview"
	"github.com/some-du6e/yoloslackgoclient/internal/ui/slackc"
	"github.com/some-du6e/yoloslackgoclient/internal/ui/theme"
	"github.com/some-du6e/yoloslackgoclient/internal/ui/uipref"
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
	bot       bool
	agent     bool // AGENT badge instead of APP (core.User.Agent)

	needChan bool // body mentions channels not resolved yet

	nameBtn, avatarBtn widget.Clickable
	nameHov, avatarHov hovercard.Trigger // Slack's ProfileHoverTrigger on both
	nameCtx, avatarCtx ctxTag            // right-click → the member context menu
	threadBtn          widget.Clickable
	broadcastBtn       widget.Clickable
	addReact           widget.Clickable
	pills              []reactPill

	// geometry of the last layout (row-local), for overlays
	reactRects []image.Rectangle
	addRect    image.Rectangle
	height     int

	// plugin state (patched path only)
	rc *rowCtx // this frame's inputs, for the natives

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

	// Slackbot custom response drawn as the team (custom_response.go)
	custom         bool
	crName, crIcon string
	crTag          crHoverTag
	crHovered      bool
	crTip          popups.TipState
	crTipText      string // the badge tooltip's text while hovered

	// what the header drew last frame (CSS may replace both)
	avDrawn   string // the avatar's picture: a CSS background-image wins
	nameShown string // the sender label: ::before + name (unless font-size 0) + ::after
}

// chanCard is one CodeChannelEntityCard under a message.
type chanCard struct {
	r  *row
	id string
}

// replyAv is one ReplyBarAvatar in the reply bar.
type replyAv struct {
	r  *row
	id string
	sz int
	w  int // width drawn last frame (0: a plugin hid it)
}

// reactPill is one reaction's state: Reaction / UnstyledButton natives.
type reactPill struct {
	r   *row
	i   int
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
	onReact   func(r *row, name string, add bool)
}

func newRow(e *entry) *row {
	r := &row{ts: e.m.TS}
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
	if r.rv == nil {
		r.rv = richview.New(r.m.Doc, opts)
	} else {
		r.rv.Set(r.m.Doc, opts)
	}
	r.uRev = 0
	r.blocksOv, r.tailOv = 0, [2]uintptr{}
	r.needChan = blocksNeedChan(r.m.Doc.Blocks)
	r.hasFiles, r.hasAtts = docTail(r.m.Doc.Blocks)
	r.chansDoc = false
}

// rvOpts are the body's richview options.
func (r *row) rvOpts() richview.Options {
	return richview.Options{Edited: r.m.Edited, Muted: r.m.System, Dim: r.m.Pending, Channel: r.m.Channel, TS: r.m.TS}
}

func (r *row) growPills() {
	if n := len(r.m.Reactions); len(r.pills) < n {
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
	}
	r.name, r.avatar, r.statusURL, r.bot = m.Name, m.Avatar, "", m.IsBot || (m.BotID != "" && m.User == "")
	coreName := ""
	r.agent = false
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
			if u.IsBot {
				r.bot = true
			}
			r.agent = u.Agent
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
	return r.pane(slackc.Props{"$key": r.ts, "className": cls, "timestamp": r.m.TS, "clickable": true, "visible": r.rc.hovered, "msg": r.message()})
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
	kids := make([]any, 0, len(m.Reactions)+1)
	for i := range m.Reactions {
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

func (pl *reactPill) props() slackc.Props {
	r := pl.r
	re := &r.m.Reactions[pl.i]
	p := slackReaction(re)
	p["$key"] = r.ts + ":" + re.Name
	p["currentUser"], p["reactionKey"], p["canReact"] = r.rc.selfID, reactionKey(&r.m), true
	p["reactions"] = slackReactions(r.m.Reactions)
	// the pill button renders inside Reaction's React tree, so context a
	// Reaction patch provides (WhoReacted's reactors) reaches the button
	p["children"] = []any{slackc.Child("UnstyledButton", pl.buttonProps(), pl.layoutButton)}
	return pl.r.pane(p)
}

func (pl *reactPill) buttonProps() slackc.Props {
	r := pl.r
	re := &r.m.Reactions[pl.i]
	cls := "c-button-unstyled c-reaction"
	if pl.reacted() {
		cls += " c-reaction--reacted"
	}
	return pl.r.pane(slackc.Props{"$key": r.ts + ":" + re.Name, "data-qa": "reactji", "className": cls, "aria-pressed": re.Self,
		"aria-label": reactionTip(r.rc.h, re, r.rc.selfID), "children": []any{
			slackc.Child("Emoji", slackc.Props{"emoji": re.Name, "size": mt.reactEmoji}, pl.layoutEmoji),
			slackc.Child("ReactionCount", slackc.Props{"count": re.Count}, pl.layoutCount),
		}})
}

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
	if r.m.System && !rc.parent {
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
	cx := dp(gtx, mt.contentX)
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
	if m.Pending {
		opac = paint.PushOpacity(gtx.Ops, 0.5)
	}
	y := dp(gtx, padTop) - dp(gtx, 4)

	if m.Pinned && !rc.thread {
		y += r.labelLine(gtx, rc, cx, y, "pin", pinnedLabel(rc, m.PinnedBy))
	}
	eph := ephemeral(rc, m)
	if eph {
		y += r.labelLine(gtx, rc, cx, y, "eye", "Only visible to you")
	} else if m.SlackbotCustomResponse() && !r.custom && !(m.Pinned && !rc.thread) {
		// MessageLabels' custom_response label (pencil), which the reskin drops
		y += r.labelLine(gtx, rc, cx, y, "edit", "Custom response")
	}
	r.col = image.Pt(cx, y)
	g := at(gtx, 0, y, func(gtx layout.Context) layout.Dimensions {
		return slackc.Component(gtx, "ProgressiveDisclosureSuccessCoachmarkWrapper", r, (*row).coachmarkProps, (*row).layoutCoachmark)
	})
	y += g.Size.Y
	if m.Pending {
		opac.Pop()
	}
	content := rec.Stop()

	h := y + dp(gtx, mt.padBottom) - dp(gtx, 8)
	if rc.header {
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
	case m.Pinned && rc.hovered:
		bg = c.PinnedHover
	case m.Pinned:
		bg = c.Pinned
	case rc.hovered, eph: // ephemeral: rgba(29,28,29,.04) (c-message_kit__background--labels--ephemeral)
		bg = c.Hover
	}
	bg = st.Background(bg)
	r.height = h
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
	content.Add(gtx.Ops)
	return layout.Dimensions{Size: size}
}

// layoutGutter is the gutter memo (filter-matched by its source marker
// c-message_kit__gutter__left): the avatar column, from its light/compact
// slots, and the body column.
func (r *row) layoutGutter(gtx layout.Context, p slackc.Props) layout.Dimensions {
	w := gtx.Constraints.Max.X
	padX := dp(gtx, mt.padX)
	cx := dp(gtx, mt.contentX)
	cw := w - cx - padX
	if cw < dp(gtx, 80) {
		cw = dp(gtx, 80)
	}
	if r.rc.thread {
		cw = w - cx - dp(gtx, 24)
	}
	gs := gutterClass.Enter(gtx, 0, 0)
	defer gs.Leave()
	if gs.Hidden() {
		return layout.Dimensions{}
	}
	left := gutterLeftClass.Enter(gtx, 0, 0)
	if !left.Hidden() {
		at(gtx, padX, 0, func(gtx layout.Context) layout.Dimensions {
			gtx.Constraints = layout.Constraints{Max: image.Pt(dp(gtx, mt.avatar), 1<<20)}
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
	if r.rc.header {
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
	visible := rc.hovered
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
	ts := shortClock(tsTime(r.m.TS))
	s := textStyle{size: float32(st.FontSize(12)), col: st.Color(rc.c.Text2), lineH: 22}
	sz := measure(gtx, rc.th, s, ts)
	at(gtx, box.X-sz.X, 0, func(gtx layout.Context) layout.Dimensions {
		if st.Active() {
			return st.Wrap(gtx, func(gtx layout.Context) layout.Dimensions { return decoLabel(gtx, rc.th, st, s, ts) })
		}
		return label(gtx, rc.th, s, ts)
	})
	return layout.Dimensions{Size: box}
}

// layoutColumn is the gutter's right side: name line, body, reactions,
// thread bar. Returns its height.
func (r *row) layoutColumn(gtx layout.Context) layout.Dimensions {
	rc := r.rc
	m := &r.m
	cw := gtx.Constraints.Max.X
	y := 0
	if rc.header {
		var d layout.Dimensions
		// The name/time line sits 2px lower than its 22px box would put it:
		// Slack's name ink starts 4px above the avatar's top, ours 6px.
		t := op.Offset(image.Pt(0, dp(gtx, 2))).Push(gtx.Ops)
		if rc.thread {
			d = slackc.Component(gtx, "ThreadSenderAndTimestampGeneric", r, (*row).threadSenderProps, (*row).layoutThreadSender)
		} else {
			d = slackc.Component(gtx, "BaseBroadcastPreamble", r, (*row).preambleProps, (*row).layoutPreamble)
		}
		t.Pop()
		y += max(d.Size.Y, dp(gtx, mt.nameLine))
	}
	if m.Broadcast && !rc.thread && m.ThreadTS != "" && m.ThreadTS != m.TS {
		y += r.broadcastLine(gtx, rc, 0, y)
	}

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
	y += dp(gtx, mt.bodyMarginBottom)

	// reactions
	if len(m.Reactions) > 0 {
		y += dp(gtx, mt.reactTop) - dp(gtx, 4)
		y += r.reactionBar(gtx, image.Pt(0, y), r.col, cw)
		y += dp(gtx, mt.reactBottom)
	} else {
		r.reactRects = r.reactRects[:0]
		r.addRect = image.Rectangle{}
	}

	// thread bar
	if m.ReplyCount > 0 && !rc.thread && (m.ThreadTS == "" || m.ThreadTS == m.TS) {
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
	if count <= 0 {
		r.replyN = 0
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
	url := ""
	if rc.h.Core() != nil {
		if u, ok := rc.h.Core().User(id); ok {
			url = u.Avatar
		} else {
			ensureUser(rc.h, id)
		}
	}
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
		doc.Blocks = fixChannels(rc.h, doc.Blocks)
		if id != 0 {
			doc.Blocks = docBlocks(rc.h.Core(), doc.Blocks, msg)
		}
		doc.Blocks = filterTail(doc.Blocks, r.filesOv, r.attsOv)
		r.rv.Set(doc, r.rvOpts())
		r.blocksOv, r.tailOv = id, tail
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
	return r.nameLine(gtx, true)
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
	if ctxclick.Clicked(gtx, &r.nameBtn) && r.m.User != "" && !r.custom {
		rc.h.OpenProfile(rc.origin(image.Point{}), r.m.User)
	}
	var bs slackc.Styled
	if r.custom { // not interactive: a span, no button (custom_response.go)
		bs = senderNameClass.Enter(gtx, 0, 0)
	} else {
		bs = senderBtnClass.Enter(gtx, btnState(gtx, &r.nameBtn), 0)
	}
	var nd layout.Dimensions
	if !bs.Hidden() {
		switch active := bs.Active(); {
		case active && r.custom:
			nd = bs.Wrap(gtx, func(gtx layout.Context) layout.Dimensions { return r.nameSpan(gtx, bs) })
		case active:
			nd = bs.Wrap(gtx, func(gtx layout.Context) layout.Dimensions { return r.nameButton(gtx, bs) })
		case r.custom:
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
	if r.bot && r.m.User != "USLACKBOT" { // Slack shows no APP tag on Slackbot
		xx += dp(gtx, mt.badgeGap)
		s := textStyle{size: 10, weight: theme.Bold, col: c.BadgeText}
		// Slack's BaseAppBadge: AGENT for agent apps, else APP.
		badge := "APP"
		if r.agent {
			badge = "AGENT"
		}
		sz := measure(gtx, rc.th, s, badge)
		bw := sz.X + dp(gtx, 6)
		bh := dp(gtx, 14)
		top := baseline - dp(gtx, 11)
		rrect(gtx.Ops, image.Rect(xx, top, xx+bw, top+bh), dp(gtx, 2), c.BadgeBg)
		at(gtx, xx+dp(gtx, 3), top+(bh-sz.Y)/2, func(gtx layout.Context) layout.Dimensions { return label(gtx, rc.th, s, badge) })
		xx += bw
	}
	if r.statusURL != "" {
		s := dp(gtx, mt.statusEmoji)
		xx += dp(gtx, 4)
		top := baseline - s + dp(gtx, 3)
		drawEmoji(gtx, rc.h, r.statusURL, image.Rect(xx, top, xx+s, top+s))
		xx += s
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
		d := label(gtx, r.rc.th, nameS, r.name)
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
	tsText := clock(tsTime(r.m.TS))
	if st.Active() {
		return st.Wrap(gtx, func(gtx layout.Context) layout.Dimensions { return decoLabel(gtx, r.rc.th, st, tsS, tsText) })
	}
	return label(gtx, r.rc.th, tsS, tsText)
}

// labelLine draws a small gray label with an icon above the name ("Pinned").
func (r *row) labelLine(gtx layout.Context, rc *rowCtx, x, y int, icon, txt string) int {
	c := rc.c
	lh := dp(gtx, 19)
	is := dp(gtx, 14)
	// icon right-aligned in the 36px gutter box that starts at x=20
	ls := labelsClass.Enter(gtx, 0, 0)
	defer ls.Leave()
	if ls.Hidden() {
		return 0
	}
	at(gtx, dp(gtx, 56)-is, y+(lh-is)/2, func(gtx layout.Context) layout.Dimensions {
		return icons.Get(icon)(gtx, unit.Dp(14), ls.Color(c.PinIcon))
	})
	ts := labelsTextClass.Enter(gtx, 0, 0)
	defer ts.Leave()
	at(gtx, dp(gtx, 64), y, func(gtx layout.Context) layout.Dimensions {
		return styledLabel(gtx, rc.th, ts, textStyle{size: 13, col: c.Muted70, lineH: 19}, txt)
	})
	return dp(gtx, 20)
}

// broadcastLine draws Slack's broadcast preamble under the header line of a
// thread reply also sent to the channel: "replied to a thread", 15px/22,
// with "a thread" a bold link that opens the thread.
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
	pd := at(gtx, x, y, func(gtx layout.Context) layout.Dimensions {
		return styledLabel(gtx, rc.th, ps, textStyle{size: 15, col: c.Text2, lineH: 22}, "replied to ")
	})
	ls := preambleLinkClass.Enter(gtx, btnState(gtx, &r.broadcastBtn), 0)
	defer ls.Leave()
	at(gtx, x+pd.Size.X, y, func(gtx layout.Context) layout.Dimensions {
		return r.broadcastBtn.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
			return styledLabel(gtx, rc.th, ls, textStyle{size: 15, weight: theme.Bold, col: c.Link, lineH: 22}, "a thread")
		})
	})
	return h
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
		if st.Active() {
			return st.Wrap(gtx, func(gtx layout.Context) layout.Dimensions { return slackc.LayoutElement(gtx, e) })
		}
		return slackc.LayoutElement(gtx, e)
	}
	if st.Active() {
		return st.Wrap(gtx, func(gtx layout.Context) layout.Dimensions { return r.reactionPills(gtx) })
	}
	return r.reactionPills(gtx)
}

// reactionPills flows the Reaction pills and the add button.
func (r *row) reactionPills(gtx layout.Context) layout.Dimensions {
	m := &r.m
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
	for i := range m.Reactions {
		pl := &r.pills[i]
		rec := op.Record(gtx.Ops)
		d := slackc.Component(g, "Reaction", pl, (*reactPill).props, (*reactPill).layout)
		r.reactRects = append(r.reactRects, place(rec.Stop(), d.Size).Add(r.reactAt))
	}
	// add-reaction button, visible on hover
	rec := op.Record(gtx.Ops)
	d := slackc.Component(g, "ReactionAddButton", r, (*row).addButtonProps, (*row).layoutAddButton)
	ar := place(rec.Stop(), d.Size)
	r.addRect = ar.Add(r.reactAt)
	return layout.Dimensions{Size: image.Pt(w, ar.Max.Y)}
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

// reacted reports whether the bar's current user reacted (the viewer,
// unless a plugin set ReactionBar's currentUser).
func (pl *reactPill) reacted() bool {
	r := pl.r
	re := &r.m.Reactions[pl.i]
	if as := r.reactAs; as != "" && as != r.rc.selfID {
		for _, u := range re.Users {
			if u == as {
				return true
			}
		}
		return false
	}
	return re.Self
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
	re := &r.m.Reactions[pl.i]
	if pl.btn.Clicked(gtx) && rc.onReact != nil {
		rc.onReact(r, re.Name, !re.Self)
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

// content is the pill's native children: emoji, then the count.
func (pl *reactPill) content(gtx layout.Context, st slackc.Styled) layout.Dimensions {
	ph := dp(gtx, mt.reactH)
	es := dp(gtx, mt.reactEmoji)
	at(gtx, 0, (ph-es)/2, func(gtx layout.Context) layout.Dimensions { return pl.layoutEmoji(gtx, nil) })
	d := at(gtx, es+dp(gtx, 4), (ph-dp(gtx, 16))/2, func(gtx layout.Context) layout.Dimensions { return pl.count(gtx, st) })
	return layout.Dimensions{Size: image.Pt(es+dp(gtx, 4)+d.Size.X, ph)}
}

// layoutEmoji is the pill's emoji (its alt text while the image loads).
func (pl *reactPill) layoutEmoji(gtx layout.Context, _ slackc.Props) layout.Dimensions {
	rc := pl.r.rc
	re := &pl.r.m.Reactions[pl.i]
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
	re := &pl.r.m.Reactions[pl.i]
	cs := textStyle{size: 12, col: rc.c.Text, lineH: 16}
	if pl.reacted() {
		cs.col, cs.weight = rc.c.ReactSelfText, theme.Bold
	}
	cs.col = st.Color(cs.col)
	return label(gtx, rc.th, cs, strconv.Itoa(re.Count))
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
	c := rc.c
	m := &r.m
	w := gtx.Constraints.Max.X
	padX := dp(gtx, mt.padX)
	cx := dp(gtx, mt.contentX)
	cw := w - cx - padX - dp(gtx, 20)
	rec := op.Record(gtx.Ops)
	icon := "info"
	switch m.Subtype {
	case "channel_join", "group_join":
		icon = "user-plus"
	case "channel_leave", "group_leave":
		icon = "user"
	case "channel_topic", "channel_purpose", "group_topic", "group_purpose", "channel_name",
		"channel_archive", "channel_unarchive", "channel_convert_to_private", "channel_convert_to_public":
		icon = "hash" // Slack's channel-event glyph
	case "pinned_item":
		icon = "pin"
	}
	is := dp(gtx, 16)
	at(gtx, padX+dp(gtx, mt.avatar)-is, dp(gtx, 10), func(gtx layout.Context) layout.Dimensions {
		return icons.Get(icon)(gtx, unit.Dp(16), c.Text3)
	})
	r.handler.h, r.handler.m, r.handler.anchor = rc.h, m, rc.origin
	y := dp(gtx, 8)
	body := at(gtx, cx, y, func(gtx layout.Context) layout.Dimensions {
		gtx.Constraints = layout.Constraints{Max: image.Pt(cw, 1<<20)}
		return r.rv.Layout(gtx, rc.st, &r.handler)
	})
	ts := clock(tsTime(m.TS))
	tst := tsClass.Enter(gtx, 0, 0)
	tsS := cssText(tst, textStyle{size: 12, col: c.TimeSystem, lineH: 19})
	tsz := measure(gtx, rc.th, tsS, ts)
	if body.Size.Y <= dp(gtx, 22) && body.Size.X+dp(gtx, 8)+tsz.X <= cw {
		at(gtx, cx+body.Size.X+dp(gtx, 8), y+dp(gtx, 1), func(gtx layout.Context) layout.Dimensions { return styledLabel(gtx, rc.th, tst, tsS, ts) })
		y += max(body.Size.Y, dp(gtx, 20))
	} else {
		y += body.Size.Y
		at(gtx, cx, y, func(gtx layout.Context) layout.Dimensions { return styledLabel(gtx, rc.th, tst, tsS, ts) })
		y += tsz.Y
	}
	tst.Leave()
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
			if len(users) > 5 {
				users = users[:5]
			}
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
				t.Pop()
				av.w = d.Size.X
				if d.Size.X > 0 {
					ax += d.Size.X + dp(gtx, 4)
				}
			}
			ax += dp(gtx, 3)
			n := "1 reply"
			if count != 1 {
				n = strconv.Itoa(count) + " replies"
			}
			ty := (bh - dp(gtx, 18)) / 2
			cs := replyCountClass.Enter(gtx, 0, 0)
			nd := at(gtx, ax, ty, func(gtx layout.Context) layout.Dimensions {
				return styledLabel(gtx, rc.th, cs, textStyle{size: 13, weight: theme.Bold, col: c.Link, lineH: 18}, n)
			})
			cs.Leave()
			ax += nd.Size.X + dp(gtx, 8)
			desc := ""
			if hov {
				desc = "View thread"
			} else if latest != "" {
				desc = "Last reply " + ago(tsTime(latest), rc.now)
			}
			ds := replyDescClass.Enter(gtx, 0, 0)
			at(gtx, ax, ty, func(gtx layout.Context) layout.Dimensions {
				gtx.Constraints.Max.X = bw - ax - dp(gtx, 28)
				return styledLabel(gtx, rc.th, ds, textStyle{size: 13, col: c.Text2, lineH: 18}, desc)
			})
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

// reactionTip is the tooltip text for a reaction pill.
func reactionTip(h Host, re *core.Reaction, selfID string) string {
	names := make([]string, 0, 4)
	for i, id := range re.Users {
		if i >= 3 {
			break
		}
		if id == selfID {
			names = append(names, "you")
			continue
		}
		n := id
		if h.Core() != nil {
			if u, ok := h.Core().User(id); ok && u.Name != "" {
				n = u.Name
			}
		}
		names = append(names, n)
	}
	others := re.Count - len(names)
	var who string
	switch {
	case len(names) == 0:
		who = strconv.Itoa(re.Count) + " people"
	case others > 0:
		who = strings.Join(names, ", ") + " and " + strconv.Itoa(others) + " others"
	case len(names) == 1:
		who = names[0]
	default:
		who = strings.Join(names[:len(names)-1], ", ") + " and " + names[len(names)-1]
	}
	if len(names) > 0 && names[0] == "you" {
		who = "You" + who[3:]
	}
	return who + " reacted with :" + re.Name + ":"
}

// skeleton draws a loading placeholder row.
func skeleton(gtx layout.Context, c *colors, i int) layout.Dimensions {
	w := gtx.Constraints.Max.X
	h := dp(gtx, 60)
	padX := dp(gtx, mt.padX)
	a := dp(gtx, mt.avatar)
	rrect(gtx.Ops, image.Rect(padX, dp(gtx, 8), padX+a, dp(gtx, 8)+a), avatarRadius(a), c.Skeleton)
	cx := dp(gtx, mt.contentX)
	widths := []int{120, 90, 160}
	nw := dp(gtx, float32(widths[i%3]))
	rrect(gtx.Ops, image.Rect(cx, dp(gtx, 10), cx+nw, dp(gtx, 22)), dp(gtx, 6), c.Skeleton)
	bw := (w - cx - padX) * (60 + (i*23)%35) / 100
	rrect(gtx.Ops, image.Rect(cx, dp(gtx, 30), cx+bw, dp(gtx, 42)), dp(gtx, 6), c.Skeleton)
	return layout.Dimensions{Size: image.Pt(w, h)}
}

// pinnedLabel is Slack's "Pinned by Name" ("Pinned by you" for the viewer).
func pinnedLabel(rc *rowCtx, by string) string {
	switch {
	case by == "":
		return "Pinned"
	case by == rc.selfID:
		return "Pinned by you"
	}
	if app := rc.h.Core(); app != nil {
		if u, ok := app.User(by); ok && u.Name != "" {
			return "Pinned by " + u.Name
		}
		ensureUser(rc.h, by)
	}
	return "Pinned"
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
