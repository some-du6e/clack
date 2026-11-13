package popups

import (
	"context"
	"image"
	"math"
	"strconv"
	"strings"
	"time"

	"gioui.org/layout"

	"github.com/some-du6e/clack/internal/core"
	"github.com/some-du6e/clack/internal/richtext"
	"github.com/some-du6e/clack/internal/ui/openurl"
	"github.com/some-du6e/clack/internal/ui/richclip"
	"github.com/some-du6e/clack/internal/ui/shortcut"
	"github.com/some-du6e/clack/internal/ui/slackc"
	"github.com/some-du6e/clack/internal/ui/theme"
)

// Message "More actions" menu, Slack's MessageActionsMenu (gantry-v2-
// async-client-boot-shared 2rcC, build 4d0c78085232), its groups in
// Slack's order: Edit message E (own, 5Ech) | Mark unread U (jFAp in the
// conversation, JsXu in a thread), the save row ⇧⌘S (oMi4: Add to
// saved, Remove from Saved, Move to Saved), the reminder row M ▸ (ih0g:
// Add a reminder, Edit reminder on a to-do; actions_todo.go), the follow
// row (ojBT) | Copy link L, Copy message ⌘C | Organize ▸ (the
// pin row, loUg), Connect to apps ▸ | Send reply to this channel (T),
// Remove reply from channel… (removeBroadcast), View workflow details
// (t7), Copy workflow ID (q) | Flag this message… (ed, actions_flag.go)
// or Report to Slack (tH), Delete message… (own, danger, 3eXV). Icons
// only on the first groups and the flag row, like Slack.

// MessageMenuOpts lets the caller take over actions it handles inline.
type MessageMenuOpts struct {
	OnEdit  func() // start inline editing (own messages)
	OnReply func() // open the thread composer
	OnTodo  func() // the hover toolbar's Add to-do (context menus)
	// Delete, when set, deletes for "Delete message…" (ui-messages routes
	// it through Taut's chatDeleteFetcher); else DeleteMessage, else
	// popups dispatches chatDeleteFetcher over core.Delete.
	Delete func(ctx context.Context, ch, ts string) error
	// Where is the message's container (Slack's messageContainerType):
	// a thread's "Mark unread" marks the thread, the conversation's the
	// conversation, and "Send reply to this channel" is only in threads.
	// NewMessageContextMenu sets it from its MessageContext.
	Where MessageContext
}

// NewMessageMenu builds the More actions menu for m. The menu's right edge
// sits at anchor.X (the kebab button). Show it with h.ShowOverlay.
func NewMessageMenu(h Host, anchor image.Point, m core.Message) *Menu {
	return NewMessageMenuOpts(h, anchor, m, MessageMenuOpts{})
}

// NewMessageMenuOpts is NewMessageMenu with caller hooks.
func NewMessageMenuOpts(h Host, anchor image.Point, m core.Message, o MessageMenuOpts) *Menu {
	a := pointRect(anchor)
	p := actMenuProps(m, a)
	if o.Where != InChannel {
		p["messageContainerType"] = ctxContainer(o.Where)
	}
	return NewMenuOpts(h, actMessageItems(h, m, o), MenuOpts{Anchor: a, AlignRight: true,
		Within: "MessageActionsMenu", WithinProps: p})
}

// actMenuProps are Slack's MessageActionsMenu props for m's ⋮ menu
// (MessageLogger adds its rows by channelId/ts).
func actMenuProps(m core.Message, anchor image.Rectangle) slackc.Props {
	p := slackc.Props{
		"channelId":                m.Channel,
		"ts":                       m.TS,
		"userId":                   m.User,
		"menuUsageLocation":        "message_actions_menu",
		"messageContainerType":     "message",
		"showJumpActionInOverflow": false,
		"targetBounds":             rectProps(anchor, 1),
	}
	if m.ThreadTS != "" && m.ThreadTS != m.TS {
		p["threadTs"] = m.ThreadTS
	}
	if m.BotID != "" {
		p["botId"] = m.BotID
	}
	return p
}

func actMessageItems(h Host, m core.Message, o MessageMenuOpts) []MenuItem {
	ch, ts := m.Channel, m.TS
	root := m.ThreadTS
	if root == "" {
		root = ts
	}
	self := actSelfID(h)
	own := m.User != "" && m.User == self
	isReply := m.ThreadTS != "" && m.ThreadTS != m.TS
	inThread := o.Where == InThread || o.Where == InThreadsView
	var conv core.Conv
	if a := h.Core(); a != nil {
		conv, _ = a.Conv(ch)
	}
	if conv.Kind == "" {
		conv.Kind = actKindOf(ch)
	}
	channel := conv.Kind == "channel" || conv.Kind == "private" // gXDR V: a public or private channel
	dm := conv.Kind == "im" || conv.Kind == "mpim"              // aK5v JM
	link := func() string {
		if m.Permalink != "" {
			return m.Permalink
		}
		return permalink(h, ch, ts, m.ThreadTS)
	}

	var items []MenuItem
	if own {
		items = append(items, MenuItem{Label: "Edit message", Icon: "edit", Hint: nsMessage.T("E"), Keys: KeyEdit, Do: func() {
			if o.OnEdit != nil {
				o.OnEdit()
				return
			}
			h.Toast("Press ↑ in the composer to edit your last message")
		}}.In(nsMessage), Sep())
	}

	remind := func(kind int) func() {
		return func() {
			due := actReminderDue(time.Now(), kind).Unix()
			actDo(h, "Reminder set", func(ctx context.Context, a *core.App) error { return a.Save(ctx, ch, ts, due) })
		}
	}
	// Mark unread: in the conversation (jFAp, messageActionsMarkUnreadAction)
	// the conversation from this message; in a thread or the Threads view
	// (JsXu, messageActionsMarkThreadUnreadAction) the thread. Slack only
	// announces "Conversation marked as unread from this message" to
	// screen readers: no toast.
	markUnread := MenuItem{Key: "mark_unread", Label: nsMessage.T("Mark unread"), Icon: "mark-unread", Hint: nsMessage.T("U"), Keys: KeyMarkUnread, Do: func() {
		actDo(h, "", func(ctx context.Context, a *core.App) error {
			if inThread {
				return a.MarkThreadUnread(ctx, ch, root, ts)
			}
			_, err := a.MarkUnread(ctx, ch, ts)
			return err
		})
	}}
	if inThread {
		markUnread.Key = "mark_thread_unread" // Slack's action ids (lq1n)
	}
	sa := SaveActionFor(h.Core(), &m)
	items = append(items,
		markUnread,
		MenuItem{Label: sa.Label, Icon: sa.Icon, Hint: shortcut.Format("Mod+Shift+S"), Keys: KeySaveTodos, Do: func() {
			RunSaveAction(h, m) // oMi4 / aSBj (actions_todo.go)
		}}.In(nsSaved),
		MenuItem{Label: ReminderLabel(h.Core(), &m), Icon: "remind", Hint: nsMessage.T("M"), Keys: KeyRemind, Sub: actReminderItems(remind, func() { h.ShowOverlay(actNewReminderModal(h, ch, ts)) })}.In(nsMessage), // ih0g
	)
	if f, ok := actFollowItem(h, m, conv, root); ok {
		items = append(items, f)
	}
	items = append(items,
		Sep(),
		MenuItem{Label: "Copy link", Icon: "link", Hint: "L", Keys: KeyCopyLink, DoCtx: func(gtx layout.Context) {
			copyText(gtx, link()) // copyLink (5FS0): the clipboard only, no toast
		}}.In(nsMessage),
		MenuItem{Label: "Copy message", Icon: "text-format", Hint: shortcut.Format("Mod+C"), Keys: KeyCopyText, DoCtx: func(gtx layout.Context) {
			copyMessage(gtx, m)
		}}.In(nsMessage),
		Sep(),
	)

	// getPinActionLabel (loUg): "…channel" in public and private channels,
	// "…this conversation" in DMs and group DMs
	pinLabel, unpinLabel := "Pin to this conversation", "Un-pin from this conversation"
	if channel {
		pinLabel, unpinLabel = "Pin to channel", "Un-pin from channel"
	}
	pin := MenuItem{Label: pinLabel, Icon: "pin", Hint: nsMessage.T("P"), Keys: KeyPin, Do: func() {
		actDo(h, "Message pinned", func(ctx context.Context, a *core.App) error { return a.Pin(ctx, ch, ts) })
	}}.In(nsMessage)
	if m.Pinned {
		pin = MenuItem{Label: unpinLabel, Icon: "unpin", Hint: nsMessage.T("P"), Keys: KeyPin, Do: func() {
			actDo(h, "Message unpinned", func(ctx context.Context, a *core.App) error { return a.Unpin(ctx, ch, ts) })
		}}.In(nsMessage)
	}
	items = append(items,
		MenuItem{Label: "Organize", Sub: []MenuItem{pin}}.In(nsMessage),
		// no "Forward message…" here: Slack keeps it on the hover toolbar
		MenuItem{Label: "Connect to apps", Sub: connectToApps(h, m)}.In(nsMessage),
	)

	// the broadcast and workflow group: [send reply, remove reply, view
	// workflow details, copy workflow ID]
	var bw []MenuItem
	// T: a thread container's own reply, not yet sent to the
	// conversation (nor a pending send), where the viewer is a member and
	// may post (canUserPostInChannel)
	if inThread && isReply && own && !m.Broadcast && !m.Pending && !m.Failed && !conv.Preview && !conv.IsArchived && actCanPost(h, ch) {
		label := "Send reply to this channel"
		if dm {
			label = "Send reply to this conversation"
		}
		bw = append(bw, MenuItem{Key: "broadcast_message", Label: label, Do: func() {
			actDo(h, "", func(ctx context.Context, a *core.App) error { return a.BroadcastReply(ctx, ch, ts) })
		}}.In(nsMessage))
	}
	// removeBroadcast: a reply sent to the conversation that the viewer
	// may delete (canRemoveBroadcast: thread_broadcast and
	// canDeleteMessageHelper; Clack deletes its own messages only)
	if m.Broadcast && isReply && own {
		label := "Remove reply from conversation…"
		if channel {
			label = "Remove reply from channel…"
		}
		bw = append(bw, MenuItem{Key: "remove_broadcast", Label: label, Do: func() { confirmRemoveBroadcast(h, m, channel) }}.In(nsMessage))
	}
	// t7: a workflow bot's message with a trigger_id: "View workflow
	// details" opens the workflow details view for the trigger
	// (openWorkflowDetailsModalCircular: toWorkflowDetails {triggerId})
	wfBot := actWorkflowBot(h, m)
	if wfBot && m.TriggerID != "" {
		trig, ch := m.TriggerID, m.Channel
		bw = append(bw, MenuItem{Key: "view_workflow_details", Label: "View workflow details", Do: func() {
			if OpenWorkflowDetails != nil {
				OpenWorkflowDetails(trig, ch)
			}
		}}.In(nsMessage))
	}
	// q: a workflow bot's message with its workflow_id, or a trigger_id
	// whose preview (workflows.triggers.preview, fetched as the menu
	// opens: EXgk getTriggerPreviews) names the workflow; copyWorkflowId
	// (aO) puts it on the clipboard and says nothing
	if wfBot && (m.WorkflowID != "" || m.TriggerID != "") {
		wfID, trig := m.WorkflowID, m.TriggerID
		if wfID == "" {
			fetchTriggerPreview(h, trig)
		}
		bw = append(bw, MenuItem{Key: "copy_workflow_id", Label: "Copy workflow ID", DoCtx: func(gtx layout.Context) {
			if id := workflowIDOf(h, wfID, trig); id != "" {
				copyText(gtx, id)
			}
		}}.In(nsMessage))
	}
	if len(bw) > 0 {
		items = append(append(items, Sep()), bw...)
	}

	// the last group: [flag, report, remove response, delete]
	var last []MenuItem
	// ed: Flag this message… (actions_flag.go), for others' messages; it
	// takes Report to Slack's place
	flag, flagged := MenuItem{}, false
	if !own {
		flag, flagged = flagRow(h, m, conv)
	}
	if flagged {
		last = append(last, flag)
	}
	// tH: isReportToSlackEnabled (client.extras), someone else's message,
	// not a tombstone, no flag row; Slack opens the help center's report
	// form
	if !flagged && !own && !m.Tombstone && m.TS != "" && actReporting(h) {
		last = append(last, MenuItem{Key: "report_message", Label: "Report to Slack", Do: func() { reportMessage(h, link()) }}.In(nsMsgActionItems))
	}
	if own {
		last = append(last, MenuItem{Label: "Delete message…", Hint: nsMessage.T("delete"), Keys: KeyDelete(), Danger: true, Do: func() { confirmDelete(h, m, o.Delete) }}.In(nsMessage))
	}
	if len(last) > 0 {
		items = append(append(items, Sep()), last...)
	}
	return items
}

// actFollowItem is the follow row (ojBT): its label from the thread's
// subscription (the root's subscribed flag), "Turn off notifications for
// replies" with notifications-off when subscribed, else "Get notified
// about new replies"; none when the viewer is neither a member of the
// conversation nor subscribed.
func actFollowItem(h Host, m core.Message, conv core.Conv, root string) (MenuItem, bool) {
	ch := m.Channel
	subscribed := m.FollowingThread
	if m.ThreadTS != "" && m.ThreadTS != m.TS {
		subscribed = actThreadSubscribed(h, ch, root)
	}
	if conv.Preview && !subscribed {
		return MenuItem{}, false
	}
	if subscribed {
		return MenuItem{Key: "subscribe", Label: "Turn off notifications for replies", Icon: "bell-off", Do: func() {
			actDo(h, "You won't be notified about new replies", func(ctx context.Context, a *core.App) error {
				return a.SubscribeThread(ctx, ch, root, false)
			})
		}}.In(nsMessage), true
	}
	return MenuItem{Key: "subscribe", Label: "Get notified about new replies", Icon: "bell", Do: func() {
		actDo(h, "You'll be notified about new replies", func(ctx context.Context, a *core.App) error {
			return a.SubscribeThread(ctx, ch, root, true)
		})
	}}.In(nsMessage), true
}

// actThreadSubscribed is the thread root's subscribed flag (the thread
// subscription Slack's 8l6l nq reads), from the store.
func actThreadSubscribed(h Host, ch, root string) bool {
	a := h.Core()
	if a == nil || a.S == nil {
		return false
	}
	if r, ok := a.S.Find(ch, root); ok {
		return r.Subscribed
	}
	return false
}

// actKindOf guesses a conversation's kind from its id when core doesn't
// know it yet.
func actKindOf(ch string) string {
	switch {
	case strings.HasPrefix(ch, "D"):
		return "im"
	case strings.HasPrefix(ch, "G"):
		return "private"
	}
	return "channel"
}

// actCanPost is canUserPostInChannel for a top-level post.
func actCanPost(h Host, ch string) bool {
	a := h.Core()
	return a != nil && a.PostingIn(ch).CanPost()
}

// actWorkflowBot is q's isWorkflowBot(member) || isWorkflowBot(bot): the
// sender is Workflow Builder's bot user.
func actWorkflowBot(h Host, m core.Message) bool {
	if u, ok := lookupUser(h, m.User); ok && u.Workflow {
		return true
	}
	if m.BotProfile != nil && m.BotProfile.UserID != "" {
		if u, ok := lookupUser(h, m.BotProfile.UserID); ok && u.Workflow {
			return true
		}
	}
	return false
}

// OpenWorkflowDetails, set by the ui root, opens the workflow details
// view (Slack's WorkflowDetails, viewType WorkflowDetails) for a trigger
// seen in channel ch.
var OpenWorkflowDetails func(triggerID, ch string)

// fetchTriggerPreview asks for a trigger's preview unless it is known
// (getTriggerPreviews: fetchObjectWithId for the missing ones).
func fetchTriggerPreview(h Host, id string) {
	a := h.Core()
	if a == nil || id == "" {
		return
	}
	ix := a.Interactive()
	if _, ok := ix.TriggerPreview(id); ok {
		return
	}
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		_, _ = ix.TriggerPreviews(ctx, id)
	}()
}

// workflowIDOf is copyWorkflowId's id: the message's workflow_id, else
// its trigger preview's (createShortcutFromTriggerAndWorkflow:
// trigger.workflow.workflow_id); "" when the preview hasn't come (Slack
// logs an error and copies nothing).
func workflowIDOf(h Host, wfID, trig string) string {
	if wfID != "" {
		return wfID
	}
	a := h.Core()
	if a == nil || trig == "" {
		return ""
	}
	if t, ok := a.Interactive().TriggerPreview(trig); ok && t.Error == "" {
		return t.WorkflowID
	}
	return ""
}

// actReporting is isReportToSlackEnabled.
func actReporting(h Host) bool {
	a := h.Core()
	return a != nil && a.ContentReportingEnabled()
}

// openReport opens a URL in the browser (a var for tests).
var openReport = openurl.Open

// reportMessage is reportMessage (ceUl): openAuthenticatedUrlInBrowser
// (mAu1) on the team's /help/requests/report-content?u=<the permalink,
// encodeURI'd>&type=message.
func reportMessage(h Host, link string) {
	base := "https://" + teamDomain(h) + ".slack.com/"
	if a := h.Core(); a != nil && a.TeamURL() != "" {
		base = a.TeamURL()
	}
	if !strings.HasSuffix(base, "/") {
		base += "/"
	}
	if err := openReport(base + "help/requests/report-content?u=" + jsEncodeURI(link) + "&type=message"); err != nil {
		h.Toast("Couldn't open the link: " + errText(err))
	}
}

// jsEncodeURI is JavaScript's encodeURI: everything but the URI's
// reserved and unreserved characters is percent-encoded (so a
// permalink's ?thread_ts=…&cid=… stays as it is, as in Slack).
func jsEncodeURI(s string) string {
	const keep = "ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789;,/?:@&=+$-_.!~*'()#"
	var b strings.Builder
	for i := 0; i < len(s); i++ {
		c := s[i]
		if strings.IndexByte(keep, c) >= 0 {
			b.WriteByte(c)
			continue
		}
		b.WriteString("%" + strings.ToUpper(strconv.FormatInt(int64(c)>>4, 16)) + strings.ToUpper(strconv.FormatInt(int64(c)&15, 16)))
	}
	return b.String()
}

// confirmRemoveBroadcast is BaseRemoveBroadcastDialog (L16H): "Remove from
// channel" (or "…conversation"), the question, the message, and "Remove
// message", which calls core.RemoveBroadcast (chat.delete with
// broadcast_delete). The dialog focuses its primary button.
func confirmRemoveBroadcast(h Host, m core.Message, channel bool) {
	title, ask := "Remove from conversation", "Are you sure you want to remove this message from the conversation?"
	if channel {
		title, ask = "Remove from channel", "Are you sure you want to remove this message from the channel?"
	}
	ch, ts := m.Channel, m.TS
	// [t, " ", note]: the question and the note are two strings in Slack too
	Confirm(h, nsMsgEdit.T(title), nsMsgEdit.T(ask)+" "+nsMsgEdit.T("The original reply will still be visible in its thread."), nsMsgEdit.T("Remove message"), false,
		func(gtx layout.Context) layout.Dimensions { return actMsgPreview(gtx, h, m, true) },
		func() {
			actDo(h, "", func(ctx context.Context, a *core.App) error { return a.RemoveBroadcast(ctx, ch, ts) })
		}).OldDialog() // a DeprecatedBaseDialog with focusOnPrimaryButton
}

// ConfirmDelete asks before deleting m (delmodal.png) and deletes it on
// confirmation. It shows the modal itself.
func ConfirmDelete(h Host, m core.Message) { confirmDelete(h, m, nil) }

func confirmDelete(h Host, m core.Message, del func(ctx context.Context, ch, ts string) error) {
	Confirm(h, nsMsgEdit.T("Delete message"), nsMsgEdit.T("Are you sure you want to delete this message? This cannot be undone."), nsMsgEdit.T("Delete"), true,
		func(gtx layout.Context) layout.Dimensions { return actMsgPreview(gtx, h, m, true) },
		func() {
			ch, ts := m.Channel, m.TS
			actDo(h, "", func(ctx context.Context, _ *core.App) error { return deleteMessage(ctx, h, del, ch, ts) })
		}).OldDialog() // BaseDeleteMessageDialog: DeprecatedBaseDialog, focusOnPrimaryButton
}

// ConfirmRemovePreview is Slack's BaseDeleteMessageAttachmentDialog for
// an unfurl's remove button: "Remove preview?" with the danger "Remove"
// (the admin-only "Also block previews from this website" form is not
// offered), then removeAttachmentsIncludingChildren (core.DeleteAttachment:
// the preview leaves the message at once, then chat.deleteAttachment).
// On failure Slack says "Attachment removing failed!".
func ConfirmRemovePreview(h Host, ch, ts, id string) {
	Confirm(h, nsInline.T("Remove preview?"), "", nsInline.T("Remove"), true, nil, func() {
		a := h.Core()
		if a == nil {
			h.Toast("Not connected yet")
			return
		}
		run(h, func(ctx context.Context) (struct{}, error) {
			return struct{}{}, a.DeleteAttachment(ctx, ch, ts, id)
		}, func(_ struct{}, err error) {
			if err != nil {
				h.Toast(nsInline.T("Attachment removing failed!"))
			}
		})
	})
}

// ---- shared helpers (actions_*.go) ----

// actDo runs a core write through do(), toasting when core is missing.
func actDo(h Host, okMsg string, work func(ctx context.Context, a *core.App) error) {
	a := h.Core()
	if a == nil {
		h.Toast("Not connected yet")
		return
	}
	do(h, okMsg, func(ctx context.Context) error { return work(ctx, a) })
}

// actToast returns an action that toasts s (features core can't do yet).
func actToast(h Host, s string) func() { return func() { h.Toast(s) } }

func actSelfID(h Host) string {
	if a := h.Core(); a != nil {
		return a.Self().ID
	}
	return ""
}

// actSnapshot returns the core snapshot without blocking on boot.
func actSnapshot(h Host) (core.Snapshot, bool) {
	a := h.Core()
	if a == nil {
		return core.Snapshot{}, false
	}
	select {
	case <-a.Ready():
		return a.Snapshot(), true
	default:
		return core.Snapshot{}, false
	}
}

// actMsgText is the copyable text of m.
// copyMessage is Copy message (messageActionsCopyTextAction, boot-shared
// 4EhV): the text, and the message as the composer's delta
// ("slack/texty", richclip.TextyDelta), which a paste in Slack's
// composer or Clack's turns back into the message with its mentions.
// Where the platform has no rich clipboard, the text alone.
func copyMessage(gtx layout.Context, m core.Message) {
	text := actMsgText(m)
	if !richclip.Supported() || text == "" {
		copyText(gtx, text)
		return
	}
	c := richclip.Copy{Text: text}
	if t := richclip.TextyDelta(m.Doc); t != "" {
		c.Custom = map[string]string{"slack/texty": t}
	}
	go func() { _ = richclip.Write(context.Background(), c) }()
}

func actMsgText(m core.Message) string {
	if s := richtext.PlainText(m.Doc); strings.TrimSpace(s) != "" {
		return s
	}
	return m.Text
}

// actAuthor resolves the display name and avatar URL of m's author.
func actAuthor(h Host, m core.Message) (name, av string) {
	name, av = m.Name, m.Avatar
	if u, ok := lookupUser(h, m.User); ok {
		if name == "" {
			name = userName(u)
		}
		if av == "" {
			av = u.Avatar
		}
	}
	if name == "" {
		name = "Unknown"
	}
	return name, av
}

// actMsgPreview draws a compact message: avatar 20, name 15/900, time 12px
// muted, then up to 4 lines of text. boxed draws Slack's bordered card
// (1px rgba(29,28,29,.13), radius 8, padding 12 16); otherwise a quote bar.
func actMsgPreview(gtx layout.Context, h Host, m core.Message, boxed bool) layout.Dimensions {
	th := h.Theme()
	cl := colorsOf(th)
	w := gtx.Constraints.Max.X
	padL, padR, padY := dp(gtx, 16), dp(gtx, 16), dp(gtx, 12)
	if !boxed {
		padL, padR, padY = dp(gtx, 16), 0, dp(gtx, 2)
	}
	innerW := maxInt(1, w-padL-padR)
	name, av := actAuthor(h, m)
	when := dayTime(tsTime(m.TS), time.Now())
	body := actMsgText(m)

	call, d := record(loose(gtx, image.Pt(innerW, 1<<20)), func(gtx layout.Context) layout.Dimensions {
		avPx, gap := dp(gtx, 20), dp(gtx, 8)
		at(gtx, image.Pt(0, dp(gtx, 1)), func(gtx layout.Context) layout.Dimensions { return avatar(gtx, h, av, 20) })
		x := avPx + gap
		tw := maxInt(1, innerW-x)
		nc, nd := record(loose(gtx, image.Pt(tw, 1<<20)), func(gtx layout.Context) layout.Dimensions {
			return text(gtx, th, 15, theme.Black, cl.Fg, name)
		})
		macroAt(gtx, nc, image.Pt(x, 0))
		if when != "" && nd.Size.X+gap < tw {
			tc, td := record(loose(gtx, image.Pt(tw-nd.Size.X-gap, 1<<20)), func(gtx layout.Context) layout.Dimensions {
				return text(gtx, th, 12, theme.Regular, cl.Muted, when)
			})
			macroAt(gtx, tc, image.Pt(x+nd.Size.X+gap, (nd.Size.Y-td.Size.Y)/2+dp(gtx, 1)))
		}
		y := maxInt(nd.Size.Y, avPx) + dp(gtx, 2)
		if body == "" {
			return layout.Dimensions{Size: image.Pt(innerW, y)}
		}
		bc, bd := record(loose(gtx, image.Pt(tw, 1<<20)), func(gtx layout.Context) layout.Dimensions {
			return para(gtx, th, 15, 22, theme.Regular, cl.Fg, body, 4)
		})
		macroAt(gtx, bc, image.Pt(x, y))
		return layout.Dimensions{Size: image.Pt(innerW, y+bd.Size.Y)}
	})
	ht := d.Size.Y + 2*padY
	if boxed {
		stroke(gtx, image.Rect(0, 0, w, ht), dp(gtx, 8), 1, cl.Ring)
	} else {
		rrect(gtx, image.Rect(0, 0, dp(gtx, 4), ht), dp(gtx, 2), theme.WithAlpha(cl.Fg, 0x33))
	}
	macroAt(gtx, call, image.Pt(padL, padY))
	return layout.Dimensions{Size: image.Pt(w, ht)}
}

// ---- Reminder (Custom…) modal ----

// actNewReminderModal is Slack's "Reminder" dialog (remind.png): 500 wide,
// "When" (15/700) + a 256x36 date select, "Time" + a 200x36 time select,
// Cancel / Save. Save → core.Save(ch, ts, due).
func actNewReminderModal(h Host, ch, ts string) *Modal {
	return actReminderModal(h, func(dueAt int64) {
		actDo(h, "Reminder set", func(ctx context.Context, a *core.App) error { return a.Save(ctx, ch, ts, dueAt) })
	})
}

// actReminderModal is the Reminder dialog; save gets the chosen time
// (unix s) once it's in the future.
func actReminderModal(h Host, save func(dueAt int64)) *Modal {
	now := time.Now()
	start := dtCeil(now, 30)
	day := &dtDate{Day: dtDay(start), Min: dtDay(now)}
	tm := &dtTime{Mins: dtMins(start)}
	tm.From = func() int {
		if n := time.Now(); dtSameDay(day.Day, n) {
			return dtMins(n)
		}
		return 0
	}
	var m *Modal
	body := func(gtx layout.Context) layout.Dimensions {
		th := h.Theme()
		cl := colorsOf(th)
		org := dtModalOrigin(gtx, m)
		w := gtx.Constraints.Max.X
		y := 0
		lbl := func(s string) {
			at(gtx, image.Pt(0, y), func(gtx layout.Context) layout.Dimensions {
				return vcenter(gtx, dp(gtx, 22), func(gtx layout.Context) layout.Dimensions {
					return text(gtx, th, 15, theme.Bold, cl.Fg, s)
				})
			})
			y += dp(gtx, 30)
		}
		lbl(nsSaved.T("When")) // LaterTimePicker (FzLp)
		at(gtx, image.Pt(0, y), func(gtx layout.Context) layout.Dimensions {
			return day.Layout(gtx, h, org.Add(image.Pt(0, y)), minInt(dp(gtx, 256), w), dtLarge)
		})
		y += dp(gtx, 36)
		lbl(nsSaved.T("Time"))
		at(gtx, image.Pt(0, y), func(gtx layout.Context) layout.Dimensions {
			return tm.Layout(gtx, h, org.Add(image.Pt(0, y)), minInt(dp(gtx, 200), w), dtLarge)
		})
		y += dp(gtx, 36)
		return layout.Dimensions{Size: image.Pt(w, y+dp(gtx, 26))}
	}
	// the saved item's reminder modal (4hLh, saved_for_later)
	m = NewModal(h, ModalOpts{Title: nsSaved.T("Reminder"), Width: 500, Body: body, Buttons: []*ModalButton{
		{Label: nsSaved.T("Cancel"), Kind: BtnOutline},
		{Label: nsSaved.T("Save"), Kind: BtnPrimary, Do: func() bool {
			due := dtAt(day.Day, tm.Mins)
			if !due.After(time.Now()) {
				h.Toast("Choose a time in the future")
				return false
			}
			save(due.Unix())
			return true
		}},
	}})
	return m
}

// actReminderItems is useReminderTimeOptions (GufF, saved_for_later) for
// the reminder submenu: In 30 mins, 1 hour and 3 hours, "Tomorrow at
// {reminderPref}" and "Monday at {reminderPref}" (9:00 in the viewer's
// clock), then Custom…. The keys stay the English labels'.
func actReminderItems(remind func(int) func(), custom func()) []MenuItem {
	pref := clock(time.Date(2000, 1, 1, 9, 0, 0, 0, time.Local))
	return []MenuItem{
		{Key: LabelKey("In 30 mins"), Label: nsSaved.T("In 30 mins"), Do: remind(actRem30m)},
		{Key: LabelKey("In 1 hour"), Label: nsSaved.T("In 1 hour"), Do: remind(actRem1h)},
		{Key: LabelKey("In 3 hours"), Label: nsSaved.T("In 3 hours"), Do: remind(actRem3h)},
		{Key: LabelKey("Tomorrow at 9:00 AM"), Label: nsSaved.T("Tomorrow at {reminderPref}", "reminderPref", pref), Do: remind(actRemTomorrow)},
		{Key: LabelKey("Monday at 9:00 AM"), Label: nsSaved.T("Monday at {reminderPref}", "reminderPref", pref), Do: remind(actRemMonday)},
		{Key: LabelKey("Custom…"), Label: nsSaved.T("Custom…"), Do: custom},
	}
}

// ---- reminder / snooze time math (pure; tested) ----

const (
	actRem30m = iota
	actRem1h
	actRem3h
	actRemTomorrow
	actRemMonday
)

// actReminderDue is the due time of a reminder preset picked at now.
func actReminderDue(now time.Time, kind int) time.Time {
	switch kind {
	case actRem30m:
		return now.Add(30 * time.Minute)
	case actRem1h:
		return now.Add(time.Hour)
	case actRem3h:
		return now.Add(3 * time.Hour)
	case actRemTomorrow:
		return actTomorrow9(now)
	}
	return actNextMonday9(now)
}

// actTomorrow9 is 9:00 AM local on the day after now.
func actTomorrow9(now time.Time) time.Time {
	y, m, d := now.Date()
	return time.Date(y, m, d+1, 9, 0, 0, 0, now.Location())
}

// actNextMonday9 is 9:00 AM on the next Monday strictly after today.
func actNextMonday9(now time.Time) time.Time {
	days := (8 - int(now.Weekday())) % 7
	if days == 0 {
		days = 7
	}
	y, m, d := now.Date()
	return time.Date(y, m, d+days, 9, 0, 0, 0, now.Location())
}

// actMinutesUntil rounds t-now up to whole minutes (at least 1).
func actMinutesUntil(now, t time.Time) int {
	n := int(math.Ceil(t.Sub(now).Minutes()))
	if n < 1 {
		n = 1
	}
	return n
}

// MessageShortcuts, when set (internal/ui/blockkit), fills the message
// menu's "Connect to apps" submenu with the workspace's message
// shortcuts (recent ones, then "More message shortcuts…").
var MessageShortcuts func(h Host, m core.Message) []MenuItem

func connectToApps(h Host, m core.Message) []MenuItem {
	if MessageShortcuts != nil {
		if items := MessageShortcuts(h, m); len(items) > 0 {
			return items
		}
	}
	return []MenuItem{{Label: "No apps", Disabled: true}}
}
