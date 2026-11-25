package composer

import (
	"context"
	"encoding/json"
	"regexp"
	"strings"
	"sync/atomic"
	"time"

	"github.com/some-du6e/clack/internal/core"
	"github.com/some-du6e/clack/internal/taut"
	"github.com/some-du6e/clack/internal/ui/popups"
)

// Slack's client commands that need the app around the composer
// (gantry-v2-async-slash-commands-local, boot-render Zrd3 getCommands,
// build 9f450b8ad3db). executeCommand finds the typed word's command in
// the slash command store by name or alias (core.App.LocalCommand) and
// runs its action with the parsed arguments:
//
//   - /shortcuts (/keys): toggleKeyboardShortcuts (DqD6 P), the Keyboard
//     Shortcuts pane.
//   - /search (/s): searchCommand (Oqka), openFromSlashCommand (elOg N1)
//     with deltaWithoutCmd as the query: with anything in it the search
//     runs at once (Open search UI with commitSearch and no popover: the
//     results page, the recent search, search.save); else the search
//     popover opens on the search box's own query.
//   - /open (/join): openCommand (8FiW, core.App.OpenCommand): the channel
//     browser without arguments, the channel joined and shown, a person's
//     DM; for a channel it can't find the create-channel modal with the
//     name typed, or Slackbot's "I couldn’t find a channel named …".
//   - /msg (/dm): msgCommand (6bdk, core.App.MsgCommand): the DMs without
//     arguments; the channel joined and shown or the person's DM opened,
//     then what follows the mention (trimDelta of the rest) goes there
//     through prepareAndSendMessage (with enableSlashCommands false, an
//     option this build's prepareAndSendMessage doesn't read: a message
//     that looks like a command runs as one there). When that send fails,
//     Slackbot says "Hmm, something went wrong. Try again?" there and the
//     command lands in that conversation's composer (addEphemeralFeedback's
//     inputDelta).
//   - /collapse, /expand: collapseCommand, expandCommand (229d, s/xk;
//     core.App.ExpandCommand): every preview of every message held for
//     the conversation (in a thread, the thread's) collapsed or expanded
//     in this composer's view (the expandables store), and outside a
//     thread Slackbot's "I’ve collapsed all files in this channel for
//     you." (or expanded), which stays.
//   - /feedback: feedbackCommand (tCkD) under slash_feedback_in_modal:
//     UserFeedbackModal (popups/user_feedback.go). With the experiment off
//     Slack opens its Help pane's feedback form, which Clack doesn't have:
//     the command then goes on to chat.command.
//   - /leave (/close, /part): leaveCommand (OOqF, core.App.LeaveCommand)
//     picks the conversation (this one, or the channel or person's DM the
//     first argument mentions) and submitLeave closes a DM or group DM
//     (closeChannelApi: conversations.close) or leaves a channel through
//     maybeLeaveChannel (popups.LeaveConversation, core.LeaveChannelAction:
//     canUserLeaveChannel's alerts, the leave, an archived channel's close,
//     a private or shared channel's confirmation, the last member's Leave
//     and archive).
//   - /rename: renameCommand (BasZ) opens RenameChannelModal
//     (popups/rename_channel.go) with the arguments as display text in its
//     input (convertContentsToStringForDisplay), or the channel's name.
//   - /archive: archiveCommand (Oc3D) opens ArchiveChannelModal
//     (popups/archive_channel.go).
//   - /gif: openGifPickerBySlashCommand (rxyx): this composer's emoji and
//     GIF picker on its GIFs tab, the command's words in its search
//     (allow_native_gif_picker and the huddle rule are
//     isSlashCommandEnabled's).
//   - /color (/colour): colorCommand (qu1f, core.App.ColorCommand): the
//     user_colors pref for the member mentioned, and Slackbot's word on
//     it.
//   - /invite_people: invitePeopleCommand (F6WW): the invite modal
//     (openInviteModal, source invite_people_slash_command) with the
//     command's words as its emails (canUserSeeInviteModal is
//     isSlashCommandEnabled's).
//   - /remove (/kick): removeCommand (p8LL, core.App.RemoveCommand) refuses
//     in its own words, or opens the roster's Remove from channel
//     confirmation for the person mentioned (openRemoveMemberDialog,
//     popups.OpenRemoveMemberDialog), whose Remove is conversations.kick;
//     the viewer naming themselves is maybeLeaveChannel.
//
// isSlashCommandEnabled has passed before any of these runs
// (core.CheckCommand in slashCommand): /rename and /archive only reach
// their dialogs with the permission. A failure is executeCommand's
// CUSTOM_ERROR (or UNKNOWN_COMMAND): its text as Slackbot's temporary
// ephemeral here, the command back in the composer (handleSlashCommand's
// catch, as for a server command).

// shortcutsToggler is optional on the Host: /shortcuts' Keyboard
// Shortcuts pane (toggleKeyboardShortcuts). UI goroutine.
type shortcutsToggler interface{ ToggleKeyboardShortcuts() }

// searchOpener is optional on the Host: /search without a query, the
// search popover on the search box's query (openFromSlashCommand's
// queryOps r). UI goroutine.
type searchOpener interface{ OpenSearchFromCommand() }

// clientCommandRan, set by tests, hears the canonical name of each client
// command localCommand runs, refused or not (what the parity oracle's
// mark on Zrd3's action entry hears in Slack).
var clientCommandRan atomic.Pointer[func(name string)]

// localCommand runs the typed command when it is one of the client
// commands above; false: it isn't (or no host can run it), and the
// command goes on.
func (j *sendJob) localCommand(ctx context.Context, a prepareSendArgs, q qDelta, args slashArgs) (bool, error) {
	ran, err := j.runLocal(ctx, a, q, args)
	if f := clientCommandRan.Load(); ran && f != nil {
		(*f)(j.api.a.LocalCommand(args.Cmd))
	}
	return ran, err
}

// runLocal is localCommand's switch.
func (j *sendJob) runLocal(ctx context.Context, a prepareSendArgs, q qDelta, args slashArgs) (bool, error) {
	if j.ui == nil || j.api.a == nil {
		return false, nil
	}
	ch, ts := a.ChannelID, a.ReplyToTs
	failed := func(err error) error {
		if txt := core.CommandFailureText(err, args.Cmd); txt != "" {
			j.api.commandFeedback(ch, ts, txt)
		}
		return &commandFailed{err}
	}
	switch j.api.a.LocalCommand(args.Cmd) {
	case "/shortcuts":
		if j.ui.shortcuts == nil {
			return false, nil
		}
		j.api.clearCommandFeedback(ts != "")
		return j.ui.shortcuts(), nil
	case "/search":
		if j.ui.search == nil {
			return false, nil
		}
		j.api.clearCommandFeedback(ts != "")
		return j.ui.search(searchQueryOf(args.Rest), qContentful(args.Rest)), nil
	case "/open":
		if j.ui.show == nil {
			return false, nil
		}
		j.api.clearCommandFeedback(ts != "")
		t, err := j.api.a.OpenCommand(ctx, args.Cmd, args.Params, openName(args.Rest))
		if err != nil {
			return true, failed(err)
		}
		j.ui.show("directories?tab=channels", t)
		return true, nil
	case "/msg":
		if j.ui.show == nil {
			return false, nil
		}
		j.api.clearCommandFeedback(ts != "")
		msg := msgRest(args.Rest)
		t, err := j.api.a.MsgCommand(ctx, args.Cmd, args.Params, len(msg.ops) > 0)
		if err != nil {
			return true, failed(err)
		}
		j.ui.show("dms", t)
		if t.Conv != "" && len(msg.ops) > 0 {
			j.msgSend(ctx, t.Conv, msg, q)
		}
		return true, nil
	case "/collapse", "/expand":
		j.api.clearCommandFeedback(ts != "")
		j.api.a.ExpandCommand(ch, ts, j.ui.viewContext, j.api.a.LocalCommand(args.Cmd) == "/expand")
		return true, nil
	case "/leave":
		if j.ui.leave == nil {
			return false, nil
		}
		j.api.clearCommandFeedback(ts != "")
		t, err := j.api.a.LeaveCommand(args.Cmd, ch, args.Params)
		if err != nil {
			return true, failed(err)
		}
		if t.Conv != "" {
			j.ui.leave(t)
		}
		return true, nil
	case "/rename":
		if j.ui.rename == nil {
			return false, nil
		}
		j.api.clearCommandFeedback(ts != "")
		j.ui.rename(ch, renameName(args.Rest))
		return true, nil
	case "/archive":
		if j.ui.archive == nil {
			return false, nil
		}
		j.api.clearCommandFeedback(ts != "")
		j.ui.archive(ch)
		return true, nil
	case "/remove":
		if j.ui.remove == nil || j.ui.leave == nil {
			return false, nil
		}
		j.api.clearCommandFeedback(ts != "")
		t, err := j.api.a.RemoveCommand(args.Cmd, ch, args.Params, renameName(args.Rest))
		if err != nil {
			return true, failed(err)
		}
		if t.Leave { // maybeLeaveChannel({channelId})
			j.ui.leave(core.LeaveTarget{Conv: ch})
		} else {
			j.ui.remove(ch, t.Member)
		}
		return true, nil
	case "/gif":
		if j.ui.gif == nil {
			return false, nil
		}
		j.api.clearCommandFeedback(ts != "")
		j.ui.gif(gifQuery(args.Params)) // with no picker to open, nothing: the command ran
		return true, nil
	case "/color":
		j.api.clearCommandFeedback(ts != "")
		if err := j.api.a.ColorCommand(ctx, args.Cmd, ch, ts, args.Params); err != nil {
			return true, failed(err)
		}
		return true, nil
	case "/invite_people":
		if j.ui.invitePeople == nil {
			return false, nil
		}
		j.api.clearCommandFeedback(ts != "")
		j.ui.invitePeople(invitePeopleEmails(args.Rest))
		return true, nil
	case "/feedback":
		if j.ui.feedback == nil || j.api.a.Experiment(popups.ExpSlashFeedbackInModal) != "on" {
			return false, nil
		}
		j.api.clearCommandFeedback(ts != "")
		j.ui.feedback()
		return true, nil
	}
	return false, nil
}

// msgSend is /msg's prepareAndSendMessage of the message into ch: what
// its send does happens in ch (sendMessage's jumpToTimestamp there, its
// unsend window, a command's keep_input in ch's message pane input, as
// executeCommand puts it into its own channel's); a failure is
// Slackbot's "Hmm, something went wrong. Try again?" in ch, the command
// (orig, the processed delta) put in ch's composer.
func (j *sendJob) msgSend(ctx context.Context, ch string, msg, orig qDelta) {
	d, _ := qToDoc(qNormalize(msg))
	no := false
	args := prepareSendArgs{ChannelID: ch, Delta: d.Delta(), Text: d.Mrkdwn(), Blocks: d.RichTextBlocks(), EnableSlashCommands: &no}
	ui := *j.ui
	// what goes back into ch's box (keep_input's text, the command after
	// a failure) waits for that box to mount, as the send's jump does
	put := func(pd *Doc) {
		switch {
		case j.ui.target != nil:
			j.ui.target(ch, func(t *Composer) { t.setContents(pd) })
		case j.ui.contents != nil:
			j.ui.contents(ch, pd)
		}
	}
	ui.keep = put
	mj := &sendJob{api: j.api, ch: ch, o: &outgoing{doc: d, text: args.Text, blocks: args.Blocks}, store: j.store, ui: &ui}
	if j.ui.target != nil {
		mj.sent = func(cid string, files bool) {
			j.ui.target(ch, func(t *Composer) {
				if t.OnSend != nil {
					t.OnSend(cid, files)
				}
			})
		}
	}
	// the command's delta was processed once already (submit's pass): the
	// message's job doesn't run the processor over it again
	mj.handed, _ = json.Marshal(args.Delta)
	var err error
	if t := currentTaut(); t.ThunkPatched(thPrepareAndSend) {
		_, err = taut.Dispatch(ctx, t, thPrepareAndSend, args, mj.native)
	} else {
		err = mj.prepareAndSend(ctx, args)
	}
	if err == nil {
		if mj.posted.Load() && j.ui.target != nil { // updateMessageEligibleForUnsending in ch: the window runs from the send
			by := time.Now().Add(unsendWindow)
			j.ui.target(ch, func(t *Composer) { t.unsendBy = by })
		}
		return
	}
	j.api.commandFeedback(ch, "", "Hmm, something went wrong. Try again?")
	od, _ := qToDoc(qNormalize(orig))
	put(od)
}

// gifQuery is openGifPickerBySlashCommand's searchQuery: the parsed
// arguments' words (the string ones), joined by single spaces.
func gifQuery(params []core.CommandParam) string {
	var words []string
	for _, p := range params {
		if p.Type == "string" {
			words = append(words, p.Value)
		}
	}
	return strings.Join(words, " ")
}

// inviteSplitRe is invitePeopleCommand's split: commas (with the spaces
// around them) or runs of space.
var inviteSplitRe = regexp.MustCompile(`\s*,\s*|\s+`)

// invitePeopleEmails is invitePeopleCommand's emails: deltaWithoutCmd as
// text (convertContentsToString), trimmed, split by commas and spaces,
// the blanks dropped.
func invitePeopleEmails(rest qDelta) []string {
	var out []string
	for _, e := range inviteSplitRe.Split(strings.TrimSpace(renameName(rest)), -1) {
		if strings.TrimSpace(e) != "" {
			out = append(out, e)
		}
	}
	return out
}

// renameName is renameCommand's d: deltaWithoutCmd as display text
// (lgkT convertContentsToStringForDisplay: atoms as their text, the last
// newline dropped); "" leaves the channel's name to the modal.
func renameName(rest qDelta) string {
	d, _ := qToDoc(qNormalize(rest))
	return strings.TrimSuffix(d.Text(), "\n")
}

// msgRest is /msg's message: deltaWithoutCmd without its first op (the
// mention; Quill holds one chip repeated as one op), trimmed (OQBl
// trimDelta).
func msgRest(rest qDelta) qDelta {
	ops := qMergeMentions(qNormalize(rest).ops)
	if len(ops) == 0 {
		return qDelta{}
	}
	return qDelta{ops: qTrim(ops[1:], true, true)}
}

// openName is openCommand's g: the first op's text when it is a channel
// mention (getMentionOpInsertText) or plain text, its "#" and spaces
// trimmed.
func openName(rest qDelta) string {
	ops := qNormalize(rest).ops
	if len(ops) == 0 {
		return ""
	}
	f := ops[0]
	s := ""
	if ref, ok := f.attrs["slackmention"].(qAtomRef); ok && f.k == qIns {
		if ref.a != nil && ref.a.Kind == AtomChannel {
			s = string(f.text)
		}
	} else if qTextItem(f) {
		s = string(f.text)
	}
	return strings.TrimFunc(strings.TrimPrefix(s, "#"), jsSpace)
}

// qContentful is FBMU isDeltaContentful with its defaults: anything but a
// lone unformatted newline (text, spaces too, an embed, a mention, a
// format).
func qContentful(q qDelta) bool {
	if len(q.ops) == 0 {
		return false
	}
	if p := q.ops[0]; len(q.ops) == 1 && p.k == qIns && string(p.text) == "\n" && len(p.attrs) == 0 {
		return false
	}
	for _, op := range q.ops {
		if !qTextItem(op) || len(op.attrs) > 0 || len(op.text) > 0 {
			return true
		}
	}
	return false
}

// searchQueryOf is SearchQuery.fromOps(queryOps).toString() for what
// /search was given: the text, a member mention as <@U…>, a user group's
// <@S…>, a channel's <#C…|name> (Member-, UserGroup- and
// ChannelQuerySuggestionToken), an emoji its text; a mention right after
// a filter ("from:") is its value; the tokens joined by single spaces
// (searchWords).
func searchQueryOf(rest qDelta) string {
	var b strings.Builder
	for _, op := range qMergeMentions(qNormalize(rest).ops) {
		switch op.k {
		case qEmb:
			if op.emb != nil && op.emb.atom != nil && op.emb.atom.Kind == AtomEmoji {
				b.WriteString(qEmojiText(op.emb.atom))
			}
		case qIns:
			ref, ok := op.attrs["slackmention"].(qAtomRef)
			if !ok || ref.a == nil {
				b.WriteString(string(op.text))
				continue
			}
			switch ref.a.Kind {
			case AtomUser, AtomUsergroup:
				b.WriteString("<@" + ref.a.ID + ">")
			case AtomChannel:
				name := strings.TrimPrefix(string(op.text), "#")
				if name == "" {
					b.WriteString("<#" + ref.a.ID + ">")
				} else {
					b.WriteString("<#" + ref.a.ID + "|" + name + ">")
				}
			default:
				b.WriteString(string(op.text))
			}
		}
	}
	return searchWords(b.String())
}

// searchWords joins s's tokens by single spaces, as SearchQuery's
// toString joins its tokens: runs of spaces between them become one, a
// "phrase" or a smart-quoted one keeps its own, the ends go.
func searchWords(s string) string {
	var b strings.Builder
	var closing rune // the phrase's closing quote while in one
	space := false
	for _, r := range strings.TrimFunc(s, jsSpace) {
		if closing != 0 {
			b.WriteRune(r)
			if r == closing {
				closing = 0
			}
			continue
		}
		if jsSpace(r) {
			space = true
			continue
		}
		if space {
			b.WriteByte(' ')
			space = false
		}
		b.WriteRune(r)
		switch r {
		case '"':
			closing = '"'
		case 0x201c: // a smart-quoted phrase
			closing = 0x201d
		}
	}
	return b.String()
}

// setContents is the input's setContents: d replaces what the composer
// holds (UI goroutine).
func (c *Composer) setContents(d *Doc) {
	c.ed.SetDoc(d)
	c.lastVer = c.ed.Version()
	c.saveDraft()
}
