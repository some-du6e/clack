package composer

import (
	"context"
	"encoding/json"

	"github.com/some-du6e/clack/internal/core"
	"github.com/some-du6e/clack/internal/richtext"
)

// coreapi.go is the only file that calls into *core.App. The rest of the
// package goes through these helpers, so a core signature change is a
// one-file fix.

type coreAPI struct{ a *core.App }

func (c coreAPI) self() core.User                         { return c.a.Self() }
func (c coreAPI) user(id string) (core.User, bool)        { return c.a.User(id) }
func (c coreAPI) users(ids []string) map[string]core.User { return c.a.Users(ids) }
func (c coreAPI) subscribePresence(ids []string)          { c.a.SubscribePresence(ids) }

// conv returns the conversation, falling back to the store's name for
// channels that aren't in the sidebar.
func (c coreAPI) conv(id string) (core.Conv, bool) {
	if cv, ok := c.a.Conv(id); ok {
		return cv, true
	}
	r := c.a.ConvRef(id)
	if r.Name == "" {
		return core.Conv{}, false
	}
	return core.Conv{ID: r.ID, Name: r.Name, Kind: r.Kind, User: r.User}, true
}
func (c coreAPI) snapshot() core.Snapshot        { return c.a.Snapshot() }
func (c coreAPI) customEmoji() map[string]string { return c.a.CustomEmoji() }
func (c coreAPI) emoji(name string) (url, text string, ok bool) {
	return c.a.Emoji(name)
}

// emojiVersion changes whenever the custom emoji set does (emoji.list
// landing, emoji_changed).
func (c coreAPI) emojiVersion() uint64 { return c.a.S.CustomEmoji.Version() }

// plainTextMode is Slack's plain_text_mode user pref.
func (c coreAPI) plainTextMode() bool { return c.a.Prefs().PlainTextMode }

// upToBrowse is users.prefs up_to_browse_kb_shortcut (the old "↑ browses
// messages" switch, read when the Accessibility choice is unset).
func (c coreAPI) upToBrowse() bool { return c.a.PrefBool("up_to_browse_kb_shortcut", false) }

// smartSymbols is getSmartSymbolsEnabled: composer_smart_symbols on and
// disable_smart_symbols unset (smart_symbols.go).
func (c coreAPI) smartSymbols() bool {
	return c.a.Experiment("composer_smart_symbols") == "on" && !c.a.PrefBool("disable_smart_symbols", false)
}

// resolver names users, channels and emoji for the mrkdwn parser.
func (c coreAPI) resolver() richtext.Resolver { return c.a.S }

// send is core.Send; cid is the client_msg_id ("" lets core make one).
func (c coreAPI) send(ctx context.Context, ch, threadTS, text string, blocks json.RawMessage, broadcast bool, cid string, attachments ...json.RawMessage) (core.Message, error) {
	o := core.SendOpts{Channel: ch, ThreadTS: threadTS, Text: text, Blocks: blocks, Broadcast: broadcast, ClientMsgID: cid}
	if len(attachments) > 0 {
		o.Attachments = attachments[0]
	}
	return c.a.Send(ctx, o)
}

func (c coreAPI) edit(ctx context.Context, ch, ts, text string, blocks json.RawMessage) error {
	return c.a.Edit(ctx, ch, ts, text, blocks)
}

func (c coreAPI) editWithFiles(ctx context.Context, ch, ts, text string, blocks json.RawMessage, fileIDs []string) error {
	return c.a.EditWithFiles(ctx, ch, ts, text, blocks, fileIDs)
}

func (c coreAPI) typing(ch, threadTS string) { c.a.Typing(ch, threadTS) }

func (c coreAPI) typingErr(ch, threadTS string) error { return c.a.Typing(ch, threadTS) }

func (c coreAPI) saveDraft(ctx context.Context, o core.DraftOpts) (core.Draft, error) {
	return c.a.SaveDraft(ctx, o)
}

func (c coreAPI) deleteDraft(ctx context.Context, id string) error { return c.a.DeleteDraft(ctx, id) }

func (c coreAPI) upload(ctx context.Context, o core.UploadOpts) ([]core.UploadedFile, error) {
	return c.a.Upload(ctx, o)
}

func (c coreAPI) prepareUpload(ctx context.Context, f core.UploadFile, progress func(sent, total int64)) (core.PreparedUpload, error) {
	return c.a.PrepareUpload(ctx, f, progress)
}

func (c coreAPI) scheduleMessage(ctx context.Context, ch, threadTS, text string, blocks json.RawMessage, broadcast bool, postAt int64) (core.ScheduledMessage, error) {
	return c.a.ScheduleMessage(ctx, core.SendOpts{Channel: ch, ThreadTS: threadTS, Text: text, Blocks: blocks, Broadcast: broadcast}, postAt)
}

// searchUsers and searchChannels are the composer lists' server
// searches, with Slack's texty-autocomplete args (count 25).
func (c coreAPI) searchUsers(ctx context.Context, q, channel string) ([]core.User, error) {
	return c.a.AutocompleteUsers(ctx, q, channel)
}

func (c coreAPI) searchChannels(ctx context.Context, q string) ([]core.ChannelHit, error) {
	return c.a.AutocompleteChannels(ctx, q)
}

// recordFrecency is recordFrecencyEvent (the shared "frecency" pref).
func (c coreAPI) recordFrecency(id, text string) { c.a.RecordFrecency(id, text) }

func (c coreAPI) usergroups(ctx context.Context) ([]core.Usergroup, error) {
	return c.a.Usergroups(ctx, "", 0)
}

func (c coreAPI) members(ctx context.Context, ch string) ([]string, error) {
	us, err := c.memberUsers(ctx, ch, 500)
	ids := make([]string, len(us))
	for i, u := range us {
		ids[i] = u.ID
	}
	return ids, err
}

// memberUsers pages a conversation's members (people and bots) until max
// users or the end of the list.
func (c coreAPI) memberUsers(ctx context.Context, ch string, max int) ([]core.User, error) {
	var out []core.User
	marker := ""
	for {
		us, next, err := c.a.Members(ctx, ch, marker, false)
		out = append(out, us...)
		if err != nil || next == "" || len(us) == 0 || len(out) >= max {
			return out, err
		}
		marker = next
	}
}

func (c coreAPI) commands(ctx context.Context) ([]core.Command, error) {
	return c.a.Commands(ctx, "")
}

func (c coreAPI) command(ctx context.Context, o core.CommandOpts) (core.CommandResult, error) {
	return c.a.Command(ctx, o)
}

// broadcastPerms is the viewer's @channel/@everyone permissions in ch
// (permissions/info, cached by core for an hour). Blocks on a miss.
func (c coreAPI) broadcastPerms(ctx context.Context, ch string) (core.BroadcastPerms, error) {
	return c.a.LoadBroadcastPerms(ctx, ch)
}

// localEphemeral shows a Slackbot "Only visible to you" message.
func (c coreAPI) localEphemeral(ch, threadTS, text string) { c.a.LocalEphemeral(ch, threadTS, text) }

// commandFeedback is a slash command's reply or failure as Slackbot's
// temporary ephemeral; clearCommandFeedback removes the earlier ones.
func (c coreAPI) commandFeedback(ch, threadTS, text string) { c.a.CommandFeedback(ch, threadTS, text) }
func (c coreAPI) clearCommandFeedback(thread bool)          { c.a.ClearCommandFeedback(thread) }

// broadcastEnabled is the channel's enable_at_channel (channels.prefs.get).
func (c coreAPI) broadcastEnabled(ctx context.Context, ch string) bool {
	return c.a.LoadBroadcastEnabled(ctx, ch)
}

// broadcastWarning is shouldWarnBeforeBroadcast for a send using used.
func (c coreAPI) broadcastWarning(ctx context.Context, ch string, used []string, enabled bool) bool {
	return c.a.BroadcastWarning(ctx, ch, used, enabled)
}

// markBroadcastWarningSeen writes last_seen_at_channel_warning.
func (c coreAPI) markBroadcastWarningSeen(ctx context.Context) error {
	return c.a.MarkBroadcastWarningSeen(ctx)
}

// broadcastAudience is conversations.info's display and timezone counts.
func (c coreAPI) broadcastAudience(ctx context.Context, ch string) (members, timezones int, err error) {
	return c.a.BroadcastAudience(ctx, ch)
}

// broadcastWarningAdmin: is the viewer an admin, and the settings URL.
func (c coreAPI) broadcastWarningAdmin() (bool, string) { return c.a.BroadcastWarningAdmin() }

// memberCount is the cached people count of ch (membershipCounts).
func (c coreAPI) memberCount(ch string) int {
	n, _ := c.a.MemberCountCached(ch)
	return n
}
