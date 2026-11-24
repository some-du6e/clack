package composer

import (
	"context"
	"encoding/json"
	"strings"
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
//
// A failure is executeCommand's CUSTOM_ERROR (or UNKNOWN_COMMAND): its
// text as Slackbot's temporary ephemeral here, the command back in the
// composer (handleSlashCommand's catch, as for a server command).

// shortcutsToggler is optional on the Host: /shortcuts' Keyboard
// Shortcuts pane (toggleKeyboardShortcuts). UI goroutine.
type shortcutsToggler interface{ ToggleKeyboardShortcuts() }

// searchOpener is optional on the Host: /search without a query, the
// search popover on the search box's query (openFromSlashCommand's
// queryOps r). UI goroutine.
type searchOpener interface{ OpenSearchFromCommand() }

// localCommand runs the typed command when it is one of the client
// commands above; false: it isn't (or no host can run it), and the
// command goes on.
func (j *sendJob) localCommand(ctx context.Context, a prepareSendArgs, q qDelta, args slashArgs) (bool, error) {
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
	case "/feedback":
		if err := j.api.a.FeedbackCommandErr(args.Cmd); err != nil { // isSlashCommandEnabled: feedback_disabled_orgs
			j.api.clearCommandFeedback(ts != "")
			return true, failed(err)
		}
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
