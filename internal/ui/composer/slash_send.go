package composer

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"log"
	"strconv"
	"strings"

	"github.com/some-du6e/clack/internal/core"
	"github.com/some-du6e/clack/internal/emoji"
	"github.com/some-du6e/clack/internal/i18n"
	"github.com/some-du6e/clack/internal/ui/popups"
	"github.com/some-du6e/clack/internal/ui/uipref"
)

// A command goes through prepareAndSendMessage like any message (the
// broadcast keyword warning first, plugins' patches around it), and the
// job's native half runs handleSlashCommand and executeCommand on the
// processed delta (slash.go): typed commands and the ones a plugin made
// of a message (NativeAtChannel's "/channel …", bChannel) alike.

// commandUI is what a command needs from the composer; each runs on the
// UI goroutine (posted there).
type commandUI struct {
	// huddle is /huddle outside a thread: start or join the huddle here
	// (false: no host for it, so the command goes to chat.command)
	huddle func(ch string) bool
	// remind is "/remind" without arguments: the reminder modal
	// (openLaterCustomModal, isMsgContext false)
	remind func(ch, ts string)
	// keep is keep_input: the command's delta back in the composer
	keep func(d *Doc)
	// invite is the local /invite's invite modal for a channel
	invite func(ch string, users []string)
	// shortcuts is /shortcuts: the Keyboard Shortcuts pane toggled (false:
	// no host for it)
	shortcuts func() bool
	// search is /search: with commit the search for q runs, else the
	// search popover opens (false: no host for it)
	search func(q string, commit bool) bool
	// show takes the viewer where /open or /msg says: browse (the route)
	// with no arguments, the create-channel modal, the conversation
	show func(browse string, t core.LocalTarget)
	// feedback is /feedback's dialog
	feedback func()
	// contents puts d in ch's composer (addEphemeralFeedback's
	// inputDelta), when it is open
	contents func(ch string, d *Doc)
}

// commandUI is the composer's.
func (c *Composer) commandUI() *commandUI {
	return &commandUI{
		huddle: func(ch string) bool {
			hs, ok := c.h.(slashHuddler)
			if ok {
				c.later(func() { hs.SlashHuddle(ch) })
			}
			return ok
		},
		remind: func(ch, ts string) { c.later(func() { c.openReminderModal(ch, ts) }) },
		keep:   func(d *Doc) { c.restore(d) },
		invite: func(ch string, users []string) {
			if o, ok := c.h.(inviteOpener); ok {
				o.OpenInviteToChannel(ch, users)
			}
		},
		shortcuts: func() bool {
			t, ok := c.h.(shortcutsToggler)
			if ok {
				c.later(t.ToggleKeyboardShortcuts)
			}
			return ok
		},
		search: func(q string, commit bool) bool {
			if commit {
				c.later(func() { popups.SubmitSearch(c.h, q) })
				return true
			}
			o, ok := c.h.(searchOpener)
			if ok {
				c.later(o.OpenSearchFromCommand)
			}
			return ok
		},
		show: func(browse string, t core.LocalTarget) {
			c.later(func() {
				switch {
				case t.Browse:
					c.h.Navigate(browse)
				case t.Create:
					c.h.ShowOverlay(popups.NewCreateChannelModal(c.h, popups.CreateChannelOpts{Name: t.Name}))
				case t.Conv != "":
					c.h.Navigate("c/" + t.Conv)
				}
			})
		},
		feedback: func() { c.later(func() { popups.NewUserFeedbackModal(c.h) }) },
		contents: func(ch string, d *Doc) {
			c.later(func() {
				if t := liveComposer(draftKey(ch, "")); t != nil && t.edit == nil {
					t.setContents(d)
				}
			})
		},
	}
}

// processed is prepareAndSendMessage's processedDelta for the thunk's
// delta: the message processor (the smart-symbol pass and the mention
// auto-slugging, slash_mentions.go) and linkify. The composer's own
// delta comes processed already (submit's prepareMessage): run once, as
// rNCs is, a second auto-slugging pass would resolve names the first left
// as text (a name now starting its own op after a resolved one). A delta
// a plugin changed or relayed is processed here.
func (j *sendJob) processed(a prepareSendArgs) qDelta {
	q := qFromSlackDelta(a.Delta)
	if j.handed == nil || !sameDelta(a.Delta, j.handed) {
		q, _ = processQ(q, j.store)
	}
	return linkifyQ(q)
}

// sameDelta reports whether delta encodes to handed.
func sameDelta(delta map[string]any, handed []byte) bool {
	b, err := json.Marshal(delta)
	return err == nil && bytes.Equal(b, handed)
}

// errMissingCommand is chatCommandApi's "Missing required params".
var errMissingCommand = errors.New("Missing required params")

// slashCommand is handleSlashCommand (after the slash test) and
// executeCommand for the processed delta q.
func (j *sendJob) slashCommand(ctx context.Context, a prepareSendArgs, q qDelta) error {
	args := parseSlashArgs(q)
	ch, ts := a.ChannelID, a.ReplyToTs
	if args.Cmd == "" {
		return errMissingCommand
	}
	if args.Cmd == "/huddle" && ts == "" && j.ui != nil && j.ui.huddle != nil {
		// Clack's /huddle starts or joins the huddle here, like the
		// header's button (Slack's startHuddle)
		j.api.clearCommandFeedback(false)
		if j.ui.huddle(ch) {
			return nil
		}
	}
	if args.Cmd == "/remind" && len(args.Params) == 0 && j.ui != nil && j.ui.remind != nil {
		// executeCommand: the reminder modal, no chat.command
		j.api.clearCommandFeedback(ts != "")
		j.ui.remind(ch, ts)
		return nil
	}
	if ok, err := j.localCommand(ctx, a, q, args); ok { // slash_local.go
		return err
	}
	o := core.CommandOpts{Channel: ch, ThreadTS: ts, Command: args.Cmd, Disp: args.Disp, Params: args.Params,
		Blocks: commandBlocks(args.Rest), AppID: a.SlashCommandAppID}
	// the local commands that read text (topicCommand: convertContentsToString
	// of deltaWithoutCmd); chat.command never carries it (Params is set)
	rest, _ := qToDoc(qNormalize(args.Rest))
	o.Text = TextyValue(rest)
	// and /topic's topic as submitChannelTopic sends it (TextyInput.Submit)
	p := uipref.Get()
	o.Submit = LinkifyPhoneNumbers(CleanMessageText(o.Text, p.ConvertEmoticons && !p.EmojiAsText))
	res, err := runCommand(ctx, j.api, o)
	if err == nil && res.KeepInput && j.ui != nil && j.ui.keep != nil {
		d, _ := qToDoc(qNormalize(q)) // executeCommand: setContents(the command's delta)
		j.ui.keep(d)
	}
	if inv := res.Invite; inv != nil && inv.AddPeople != "" && j.ui != nil && j.ui.invite != nil {
		j.ui.invite(inv.AddPeople, inv.AddUsers)
	}
	return err
}

// ---- the thunk's delta ----

// qFromSlackDelta reads prepareAndSendMessage's delta (Doc.Delta's, or
// what a plugin made of it) as the Quill document Slack would hold: the
// composer's mention embeds (slackuser, slackchannel, slackusergroup) as
// Slack's mention ops (the label with slackmention), Slack's own mention
// and slug ops as they are, emoji and dividers as embeds.
func qFromSlackDelta(delta map[string]any) qDelta {
	var q qDelta
	seq := 0
	ref := func(a *Atom) qAtomRef {
		seq++
		return qAtomRef{a: a, seq: seq}
	}
	for _, op := range deltaOps(delta) {
		raw := asMap(op["attributes"])
		at := qAttrsFromJSON(raw)
		switch ins := op["insert"].(type) {
		case string:
			if ins == "" {
				continue
			}
			if m := asMap(raw["slackmention"]); m != nil {
				id, _ := m["id"].(string)
				label, _ := m["label"].(string)
				if a := slackMentionAtom(id, firstNonEmpty(label, ins)); a != nil {
					at = withAttr(at, "slackmention", ref(a))
				}
			} else if s := asMap(raw["slackslug"]); s != nil {
				if a := slackSlugAtom(s, ins); a != nil {
					at = withAttr(at, "slackslug", ref(a))
				}
			}
			q = q.insert(ins, at)
		default:
			e := asMap(op["insert"])
			if e == nil {
				continue
			}
			field := func(k, f string) string {
				v, _ := asMap(e[k])[f].(string)
				return v
			}
			switch {
			case asMap(e["slackuser"]) != nil, asMap(e["slackchannel"]) != nil, asMap(e["slackusergroup"]) != nil:
				kind, key := AtomUser, "slackuser"
				if asMap(e["slackchannel"]) != nil {
					kind, key = AtomChannel, "slackchannel"
				} else if asMap(e["slackusergroup"]) != nil {
					kind, key = AtomUsergroup, "slackusergroup"
				}
				id, label := field(key, "id"), field(key, "label")
				if id == "" {
					continue
				}
				if label == "" {
					label = id
				}
				q = q.insert(label, withAttr(at, "slackmention", ref(&Atom{Kind: kind, ID: id, Text: label})))
			case asMap(e["slackemoji"]) != nil:
				name := field("slackemoji", "name")
				if name == "" {
					name = strings.Trim(field("slackemoji", "text"), ":")
				}
				if name == "" {
					continue
				}
				uni := field("slackemoji", "unicode")
				if uni == "" {
					// createEmojiChunkFromOp: a standard emoji (in the emoji
					// data, isStandardEmojiFromParsedEmoji) is its unicode, its
					// skin tone's; a custom one keeps none
					if _, text, ok := emoji.Standard(name); ok {
						uni = text
					}
				}
				q.push(qOp{k: qEmb, emb: &qEmbed{atom: &Atom{Kind: AtomEmoji, ID: name, Text: ":" + name + ":", Uni: uni}}, attrs: at})
			case truthy(e["divider"]) || truthy(e["slackhorizontalrule"]):
				q.push(qOp{k: qEmb, emb: &qEmbed{hr: true}, attrs: at})
			}
		}
	}
	return q
}

// withAttr is at with k set.
func withAttr(at qAttrs, k string, v any) qAttrs {
	out := at.clone()
	if out == nil {
		out = qAttrs{}
	}
	out[k] = v
	return out
}

// qAttrsFromJSON keeps a delta op's inline and line formats.
func qAttrsFromJSON(m map[string]any) qAttrs {
	var at qAttrs
	set := func(k string, v any) {
		if at == nil {
			at = qAttrs{}
		}
		at[k] = v
	}
	for _, k := range [...]string{"bold", "italic", "underline", "strike", "code", "unlink", "blockquote", "code-block"} {
		if truthy(m[k]) {
			set(k, true)
		}
	}
	if l, _ := m["link"].(string); l != "" {
		set("link", l)
	}
	if l, _ := m["list"].(string); l != "" {
		set("list", l)
	}
	for _, k := range [...]string{"header", "indent", "border"} {
		if n := jsonInt(m[k]); n != 0 {
			set(k, n)
		}
	}
	return at
}

// jsonInt is a number that crossed from JS (float64), Go (int) or a
// format's string value ("1").
func jsonInt(v any) int {
	switch v := v.(type) {
	case float64:
		return int(v)
	case int:
		return v
	case int64:
		return int(v)
	case string:
		n, _ := strconv.Atoi(v)
		return n
	case bool:
		if v {
			return 1
		}
	}
	return 0
}

// slackMentionAtom is a slackmention's atom: a broadcast keyword (BK…),
// a user group (S…), a channel (looksLikeChannelId) or a member.
func slackMentionAtom(id, label string) *Atom {
	switch {
	case id == "":
		return nil
	case strings.HasPrefix(id, "BK"):
		return &Atom{Kind: AtomBroadcast, ID: id[2:], Text: label}
	case strings.HasPrefix(id, "S"):
		return &Atom{Kind: AtomUsergroup, ID: id, Text: label}
	case looksLikeChannelID(id):
		return &Atom{Kind: AtomChannel, ID: id, Text: label}
	}
	return &Atom{Kind: AtomUser, ID: id, Text: label}
}

// slackSlugAtom is a slackslug's atom: a message or a link slug
// (messageSlugDelta, linkSlugDelta); nil for other kinds.
func slackSlugAtom(s map[string]any, label string) *Atom {
	str := func(k string) string {
		v, _ := s[k].(string)
		return v
	}
	if l := str("label"); l != "" {
		label = l
	}
	switch str("type") {
	case "message":
		return &Atom{Kind: AtomMessage, ID: str("id"), Channel: str("secondaryObjectId"), Author: str("authorId"),
			Thread: str("tertiaryObjectId"), URL: str("url"), Text: label}
	case "link":
		return &Atom{Kind: AtomLink, ID: str("id"), URL: firstNonEmpty(str("url"), str("id")), Text: label}
	}
	return nil
}

// savedNS is Slack's saved_for_later strings (LaterCustomModal's).
var savedNS = i18n.NS("saved_for_later")

// openReminderModal is "/remind" alone: openLaterCustomModal with
// isMsgContext false (E8e8 LaterCustomModal): the Reminder dialog with a
// Description ("Remind me to…"). Save files the reminder: saved.add
// {item_type: reminder} with the description's blocks, convertDeltaToBlocks
// of the input's contents with the defaults (no processing, links as
// typed).
func (c *Composer) openReminderModal(ch, ts string) {
	m, _ := c.reminderModal(ch, ts)
	c.h.ShowOverlay(m)
}

// reminderModal is openReminderModal's dialog and its Description input.
func (c *Composer) reminderModal(ch, ts string) (*popups.Modal, *TextyInput) {
	_, _ = ch, ts // the modal names no conversation without a message
	in := NewTextyInput(c.h, "", savedNS.T("Remind me to\u2026"), 0)
	api := c.api
	m := popups.NewReminderDescriptionModal(c.h, in, func(due int64) {
		q, _ := qFromDoc(in.ed.Doc())
		desc := convertBlocks(q, false)
		go func() {
			if err := api.addReminder(context.Background(), desc, due); err != nil {
				// addReminder reverts its optimistic add and logs "Failed to add
				// reminder"; only a to-do's failure toasts (LaterTodoErrorToast)
				log.Printf("composer: saved.add reminder: %v", err)
			}
		}()
	})
	return m, in
}
