package composer

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/some-du6e/clack/internal/core"
	"github.com/some-du6e/clack/internal/ui/slackc"
)

// The send side of @channel / @here / @everyone, in Slack's
// prepareAndSendMessage order (gantry boot-shared, build a84662d2d135):
//
//  0. sendDraftMessage, before the thunk: channels.prefs.get
//     enable_at_channel (core.LoadBroadcastEnabled). Clack asks only when
//     the message uses a keyword, the only time the answer is read.
//  1. includeBroadcastKeywordWarning (the channel composer and scheduled
//     sends pass it; the thread composer doesn't): shouldWarnBeforeBroadcast
//     (core.BroadcastWarning) opens the "Notify N people?" dialog
//     (broadcast_warning.go) and the send stops with "Shouty rooster in
//     effect", the message still in the composer. Its Send button
//     dispatches the thunk again with includeBroadcastKeywordWarning
//     false; Clack's job waits for the answer and goes on.
//  2. handleSlashCommand: a delta that passes the slash test (s4/N, on
//     the processed delta: mentions auto-slugged, links linkified) runs as
//     its command (slash_send.go): a typed one, or a plugin's rewrite (Taut's
//     NativeAtChannel turns a message Slack would refuse into
//     at-channel's "/channel …" with slashCommandAppId and calls the
//     original thunk; bChannel's "/bchannel …"). The keywords the warning
//     and the check read are the processed delta's too.
//  3. hasProhibitedKeywords (core.ProhibitedBroadcast): a keyword the
//     viewer may not use stops the send, Slackbot says why ("Only visible
//     to you") and the message goes back in the composer.
//  4. the post itself (deliver).
//
// The relay plugins only rewrite sends Slack would refuse, so 1 never
// fires for them: the warning needs the keyword to be allowed.

// errProhibitedBroadcast is Slack's ProhibitedBroadcastKeywordInEffect
// rejection. It may come back through a plugin's thunk as JS error text,
// so it is recognised by message.
var errProhibitedBroadcast = errors.New("ProhibitedBroadcastKeywordInEffect")

func isProhibitedBroadcast(err error) bool {
	return err != nil && strings.Contains(err.Error(), errProhibitedBroadcast.Error())
}

// errShoutyRooster is Slack's ShoutyRooster rejection ("Shouty rooster in
// effect"): the broadcast warning was dismissed with Edit Message.
var errShoutyRooster = errors.New("Shouty rooster in effect")

func isShoutyRooster(err error) bool {
	return err != nil && strings.Contains(err.Error(), errShoutyRooster.Error())
}

// isQuietSendStop reports a stopped send that already told the user why
// (Slackbot's message, or the dialog they closed): no toast.
func isQuietSendStop(err error) bool {
	return isProhibitedBroadcast(err) || isShoutyRooster(err) || isCommandFailed(err)
}

// broadcastPermsWait bounds the permission lookup a send may wait for.
const broadcastPermsWait = 10 * time.Second

// broadcastAsk is what the warning dialog shows.
type broadcastAsk struct {
	Channel   string
	Used      []string // keywords in the message
	Scheduled bool     // a scheduled send (dateScheduled): the "who'll be online" strings
}

// prepareAndSend is prepareAndSendMessage's native (and the whole send
// when no plugin patched it). a is the thunk's options as the plugins
// left them.
func (j *sendJob) prepareAndSend(ctx context.Context, a prepareSendArgs) error {
	q := j.processed(a) // processedDelta (slash_send.go)
	used := qKeywords(q)
	enabled := true
	if len(used) > 0 {
		pctx, cancel := context.WithTimeout(ctx, broadcastPermsWait)
		enabled = j.api.broadcastEnabled(pctx, a.ChannelID)
		cancel()
	}
	if a.IncludeBroadcastKeywordWarning && len(used) > 0 &&
		!j.confirmed(ctx, broadcastAsk{Channel: a.ChannelID, Used: used}, enabled) {
		return errShoutyRooster
	}
	if !j.files && looksLikeSlashCommand(q) {
		return j.slashCommand(ctx, a, q)
	}
	if len(used) > 0 {
		if msg, no := j.prohibited(ctx, a.ChannelID, a.ReplyToTs, used, enabled); no {
			j.api.localEphemeral(a.ChannelID, a.ReplyToTs, msg)
			return errProhibitedBroadcast
		}
	}
	broadcast := (a.ShouldBroadcast || a.Broadcast) && a.ReplyToTs != ""
	return j.deliver(ctx, a.ChannelID, a.ReplyToTs, a.Text, a.Blocks, broadcast)
}

// confirmed is step 1: true when no warning is due, else the user's
// answer to the dialog (false without a dialog to ask through).
func (j *sendJob) confirmed(ctx context.Context, ask broadcastAsk, enabled bool) bool {
	pctx, cancel := context.WithTimeout(ctx, broadcastPermsWait)
	warn := j.api.broadcastWarning(pctx, ask.Channel, ask.Used, enabled)
	cancel()
	if !warn {
		return true
	}
	if j.confirm == nil {
		return false
	}
	return j.confirm(ctx, ask)
}

// prohibited is hasProhibitedKeywords for this send. Permissions that
// can't be looked up count as not allowed, as in Slack. enabled is the
// channel's enable_at_channel.
func (j *sendJob) prohibited(ctx context.Context, ch, ts string, used []string, enabled bool) (string, bool) {
	c, _ := j.api.conv(ch)
	pctx, cancel := context.WithTimeout(ctx, broadcastPermsWait)
	p, _ := j.api.broadcastPerms(pctx, ch)
	cancel()
	return core.ProhibitedBroadcast(core.BroadcastSend{Conv: c, Perms: p, ThreadTS: ts, Used: used,
		Disabled: !enabled, Restricted: j.api.self().Guest})
}

// ---- Slack's delta ----

// asMap reads a JSON object that crossed from JS (map or slackc.Props).
func asMap(v any) map[string]any {
	switch m := v.(type) {
	case map[string]any:
		return m
	case slackc.Props:
		return m
	}
	return nil
}

func deltaOps(delta map[string]any) []map[string]any {
	raw := slackc.Props(delta).List("ops")
	out := make([]map[string]any, 0, len(raw))
	for _, o := range raw {
		if op := asMap(o); op != nil {
			out = append(out, op)
		}
	}
	return out
}

// mentionID is a Slack mention op's attributes.slackmention.id.
func mentionID(op map[string]any) string {
	id, _ := asMap(asMap(op["attributes"])["slackmention"])["id"].(string)
	return id
}

// deltaKeywords lists the broadcast keywords the delta mentions
// ("channel", "here", "everyone"): Slack's ops whose slackmention id
// starts with "BK".
func deltaKeywords(delta map[string]any) []string {
	var out []string
	for _, op := range deltaOps(delta) {
		if _, ok := op["insert"].(string); !ok {
			continue
		}
		if id := mentionID(op); strings.HasPrefix(id, "BK") && len(id) > 2 {
			out = append(out, id[2:])
		}
	}
	return out
}

// qKeywords lists the broadcast keywords a processed delta mentions
// (its mention ops whose id starts with "BK").
func qKeywords(q qDelta) []string {
	var out []string
	for _, op := range q.ops {
		if id, ok := qMentionID(op); ok && strings.HasPrefix(id, "BK") && len(id) > 2 {
			out = append(out, id[2:])
		}
	}
	return out
}
