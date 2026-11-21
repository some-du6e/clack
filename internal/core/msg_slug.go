package core

import (
	"context"
	"encoding/json"
	"regexp"
	"slices"
	"strings"

	"github.com/some-du6e/clack/internal/richtext"
	"github.com/some-du6e/clack/internal/slack"
)

// Message slugs (a message_mention: a pasted message link, the "message"
// pill), labelled as Slack's getMessageSlugAttributes (client-boot N1tw)
// labels them (gantry-v2, 2026-11: EwNg MessageMention, DRDu
// messageMentionToText, Eley/DHDI ensureModelsArePresent):
//
//   - The label is the linked message's: getMessageByTimestamp (wClv Ld)
//     reads it from the messages store and getMsgSenderEntity (hVKV)
//     finds its sender. The element's author_id plays no part in it (a
//     notification fetches it, below): a slug whose element has none, or
//     names a member nobody fetched, still names the message's sender.
//   - A message the store doesn't hold is the messages
//     UnknownFetchManager's (Za0J: messages.list by id, every id pending
//     in one fetch, 200 ms apart): its unknown stub while the fetch is
//     out, its non-existent stub once messages.list didn't return it, or
//     once the fetch failed for good (0mwJ's finally marks what a fetch
//     didn't bring non-existent; Clack's after RefetchMessages' last
//     try). Both are the Unavailable variant: "Private message",
//     disabled, not a link, and a stub isn't asked for again. A ts that is a pending message's (isTempTs, dUDN: none or
//     an "x" in it) isn't fetched. Clack's fetch by id is
//     RefetchMessages (read_changes.go).
//   - The sender's name is getMessageSenderNameFromState (xbeB) over
//     maybeGetBotAndMemberByIds (Emf2 Od): a member's
//     getPrefCompliantMemberName, "" for the members fetch's unknown
//     stub (users/info is out) and for a non-existent member; a bot's
//     message its username, else the bot's name (getBotById: bots.info
//     for an id that looks like a bot's and isn't Slack's own), never
//     its bot_profile (N1tw passes none); else the username. "" reads
//     "Unknown author". (Not ported: getMsgSenderEntity's senders of
//     system messages, a huddle's, a canvas update's, a tab or list
//     item added, a custom response's.)
//   - In a DM under the mentions_full_names_only_pref experiment the
//     partner reads as mentions name them (N1tw's k: their real name
//     under shouldDisplayRealNamesInMentions, else their display name or
//     real name), as the author of their own messages and as the DM's
//     name in "{author} with {name}", unless the viewer hid them.
//   - What asks is the render: MessageMention selects
//     getMessageSlugAttributes, whose getMessageByTimestamp and
//     getMemberById ask for what the store lacks, and it renders again
//     when the store changes. Clack's draws read MessageSlugLabel with
//     ask (richview's SlugRead for every message drawn, the views'
//     previews), and the answer ("names" naming the message as
//     "channel/ts", or the member) draws them again. A conversion reads
//     the store only (refNames.SlugMessage) and records the message
//     (MsgRefs.Messages), which converts it again on that "names".
//   - A notification first waits for ensureModelsArePresent: the linked
//     messages (x0EU, by id), the elements' author_ids and channels
//     (EnsureMessages, the toast's noteResolve). Then
//     messageMentionToText writes the label, fetching nothing
//     (fetchIfMissing false), or the link itself when the slug is
//     disabled (convertUnknownSlugToUrl): MessageSlugText.

// slugReader reads what a slug's label names of its message's sender: a
// conversion's reads are recorded (refNames), a draw's ask for what the
// store doesn't hold, a notification's ask for nothing (it waited).
type slugReader interface {
	memberName(id string) (string, bool) // getPrefCompliantMemberName ("Name hidden" for someone hidden)
	user(id string) (*slack.User, bool)  // getMemberById
	botName(id string) string            // getBotById's name, "" while it isn't known
}

// MessageSlugLabel is getMessageSlugAttributes' label for a message slug
// linking to ts of ch, as the store has it now. ask: a draw's read (the
// slug renders), which asks for a message the store doesn't hold
// (messages.list, RefetchMessages) and for its sender (users/info,
// bots.info), each answer a "names" that draws it again; without ask
// nothing is asked for (fetchIfMissing false: a notification's text,
// written once what it names was waited for). Cheap enough for every
// frame; never blocks.
func (a *App) MessageSlugLabel(ch, ts string, ask bool) (label string, lock bool, pre int, off bool) {
	r := &slugNames{msgNames: msgNames{a.S}, app: a, ask: ask}
	return richtext.MessageSlugLabel(r, r, ch, ts)
}

// MessageSlugText is messageMentionToText for a notification: the plain
// label (a private channel's name after "#": plainTextChannelName's
// privateChannelUseHashSign, where the pill draws the lock icon), or url
// when the slug is disabled (convertUnknownSlugToUrl: the message isn't
// there, or its conversation isn't one the viewer can see). It asks for
// nothing.
func (a *App) MessageSlugText(ch, ts, url string) string {
	label, lock, pre, off := a.MessageSlugLabel(ch, ts, false)
	if off && url != "" {
		return url
	}
	return PlainSlugLabel(label, lock, pre)
}

// PlainSlugLabel is a slug label as text: the lock icon drawn before a
// private channel's name is "#" (getMessageSlugAttributes' plain label).
func PlainSlugLabel(label string, lock bool, pre int) string {
	if lock && pre > 0 && pre <= len(label) {
		return label[:pre] + "#" + label[pre:]
	}
	return label
}

// slugNames is the resolver MessageSlugLabel labels with: the store's
// names, the linked message as the store has it.
type slugNames struct {
	msgNames
	app *App
	ask bool
}

func (r *slugNames) SlugMessage(ch, ts string) (richtext.SlugMessage, bool) {
	return r.app.slugMessage(ch, ts, r.ask, slugStoreReader{r.app, r.ask})
}

// slugStoreReader reads the store's members and bots: with ask, as a
// render does (a miss is asked for), else only what it holds.
type slugStoreReader struct {
	a   *App
	ask bool
}

func (s slugStoreReader) memberName(id string) (string, bool) {
	if s.ask {
		return s.a.S.MemberName(id)
	}
	return s.a.S.MemberNameHeld(id)
}

func (s slugStoreReader) user(id string) (*slack.User, bool) {
	if s.ask {
		return s.a.S.User(id)
	}
	return s.a.S.RawUser(id)
}

func (s slugStoreReader) botName(id string) string {
	if b, ok := s.a.S.Bot(id); ok {
		return b.Name
	}
	if s.ask && slugBotFetched(id) {
		s.a.S.NoteMissingBot(id) // resolveLoop: bots.info, then "names"
	}
	return ""
}

// slugBotFetched reports whether getBotById asks for bot id when the
// store doesn't hold it: an id that looks like a bot's (looksLikeBotId)
// and isn't one of Slack's own.
func slugBotFetched(id string) bool {
	return botIDRe.MatchString(id) && !slackInternalBots[id]
}

// SlugMessage is richtext.MessageSlugger for a conversion: the linked
// message as the store holds it, nothing asked for (the slug asks as it
// draws: MessageSlugLabel), its sender named through the conversion's own
// reads, and recorded (Refs.Messages) so the message's answer converts
// this one again.
func (r *refNames) SlugMessage(ch, ts string) (richtext.SlugMessage, bool) {
	if ch != "" && ts != "" {
		addRef(&r.refs.Messages, MsgKey{ch, ts}.String())
	}
	if r.app == nil {
		return richtext.SlugMessage{}, false
	}
	return r.app.slugMessage(ch, ts, false, convSlugReader{r})
}

// convSlugReader is a conversion's slugReader: each member and bot it
// reads is recorded.
type convSlugReader struct{ r *refNames }

func (c convSlugReader) memberName(id string) (string, bool) { return c.r.MemberName(id) }

func (c convSlugReader) user(id string) (*slack.User, bool) {
	c.r.member(id)
	return c.r.app.S.RawUser(id)
}

func (c convSlugReader) botName(id string) string {
	addRef(&c.r.refs.Bots, id)
	if b, ok := c.r.app.S.Bot(id); ok {
		return b.Name
	}
	return ""
}

// slugMessage is the linked message ch/ts as getMessageSlugAttributes
// reads it for the label (getMessageByTimestamp, then the sender), ok
// false while the store doesn't hold it. ask: getMessageByTimestamp's
// fetchIfMissing, a message the store doesn't hold is asked for.
func (a *App) slugMessage(ch, ts string, ask bool, rd slugReader) (richtext.SlugMessage, bool) {
	m, ok := a.S.FindHeld(ch, ts)
	if !ok {
		if ask {
			a.askSlugMessage(ch, ts)
		}
		return richtext.SlugMessage{}, false
	}
	author, conv := a.slugSender(ch, &m, rd)
	// isFromCurrentUser (iub5 vZ): the message's user is the viewer
	return richtext.SlugMessage{Author: author, Conv: conv, Self: m.User != "" && m.User == a.S.SelfID()}, true
}

// slugTSRe is a ts getMessageByTimestamp fetches: a message's, not a
// pending message's temporary one (isTempTs).
var slugTSRe = regexp.MustCompile(`^[0-9]+\.[0-9]+$`)

// askSlugMessage asks Slack for ch/ts by id, as getMessageByTimestamp's
// fetchObjectWithId does, unless Slack said there is none (the
// non-existent stub stays: a "gone" or a deletion in the change table),
// core gave up on its fetch (refetchRound: the stub a failed fetch
// leaves), or ts can't be a message's. The answer names the key in a
// "names" (refetchRound), which draws the slugs again.
func (a *App) askSlugMessage(ch, ts string) {
	if ch == "" || !slugTSRe.MatchString(ts) {
		return
	}
	if c, ok := a.MessageChange(ch, ts); ok && c.Deleted {
		return
	}
	k := MsgKey{ch, ts}
	l := &a.msgLog
	l.mu.Lock()
	if l.slugGone[k] {
		l.mu.Unlock()
		return
	}
	if l.slugAsked == nil || len(l.slugAsked) >= maxRefetchKeys {
		l.slugAsked = map[MsgKey]bool{}
	}
	l.slugAsked[k] = true
	l.mu.Unlock()
	a.RefetchMessages(ch, []string{ts})
}

// slugSender is the label's messageAuthorName for m short of its
// fallback ("" is "Unknown author"), and its conversation's name when
// N1tw's k gives one: getMsgSenderEntity's user (a member), else its bot
// and username, named as getMessageSenderNameFromState names them.
func (a *App) slugSender(ch string, m *slack.Message, rd slugReader) (author, conv string) {
	partner := a.imPartner(ch)
	if partner != "" && a.Experiment("mentions_full_names_only_pref") == "on" && !a.UserHidden(partner) {
		if u, ok := rd.user(partner); ok && u != nil {
			conv = slugMentionName(u, a.realNamesInMentions())
		}
	}
	switch {
	case m.User != "":
		if m.User == partner && conv != "" {
			return conv, conv
		}
		n, _ := rd.memberName(m.User) // the unknown stub and a non-existent member have no name
		return n, conv
	case m.BotID != "" && m.Username != "":
		return m.Username, conv
	case m.BotID != "":
		return rd.botName(m.BotID), conv
	}
	return m.Username, conv
}

// imPartner is the other member of DM ch ("" when ch isn't a DM core
// knows): N1tw's c.e(getChannelById(ch)).
func (a *App) imPartner(ch string) string {
	c, ok := a.S.Conv(ch)
	if !ok {
		c, ok = a.S.ExtConv(ch)
	}
	if ok && c.IsIM {
		return c.User
	}
	return ""
}

// slugMentionName is N1tw's k for member u: their real name (hWCs wg)
// when real is set, else their display name or real name (hWCs Gt:
// getMemberMentionName).
func slugMentionName(u *slack.User, real bool) string {
	realName := u.Profile.RealName
	if realName == "" {
		realName = u.RealName
	}
	if real {
		return realName
	}
	if u.Profile.DisplayName != "" {
		return u.Profile.DisplayName
	}
	return realName
}

// realNamesInMentions is shouldDisplayRealNamesInMentions (client-boot
// rR47 kz): under the mentions_full_names_only_pref experiment, the
// viewer's mentions_real_names_override (1 on, -1 off), else the team's
// display_real_names_in_mentions. (An org's: Slack reads it across the
// org's teams; Clack has its own team's.)
func (a *App) realNamesInMentions() bool {
	if a.Experiment("mentions_full_names_only_pref") != "on" {
		return false
	}
	switch a.PrefInt("mentions_real_names_override", 0) {
	case 1:
		return true
	case -1:
		return false
	}
	if v, ok := a.Pref("display_real_names_in_mentions"); ok {
		return prefTruthy(v, false)
	}
	v, ok := a.TeamPref("display_real_names_in_mentions")
	return ok && prefTruthy(v, false)
}

// EnsureMessages is ensureModelsArePresent's messages (client-boot DHDI,
// x0EU): the ones keys names that the store doesn't hold, fetched by id
// (RefetchMessages, with whatever else is asked for meanwhile), waited
// for until each has an answer or ctx ends. A notification waits for its
// slugs' messages this way before it writes their labels.
func (a *App) EnsureMessages(ctx context.Context, keys []MsgKey) {
	var wait []MsgKey
	for _, k := range keys {
		if k.Channel == "" || !slugTSRe.MatchString(k.TS) {
			continue
		}
		if _, ok := a.S.FindHeld(k.Channel, k.TS); ok {
			continue
		}
		if c, ok := a.MessageChange(k.Channel, k.TS); ok && c.Deleted {
			continue
		}
		a.askSlugMessage(k.Channel, k.TS)
		wait = append(wait, k)
	}
	for _, ch := range a.refetchWaiters(wait) {
		select {
		case <-ch:
		case <-ctx.Done():
			return
		}
	}
}

// SlugAuthors are the author_ids of the message slugs in m's blocks, as
// getMessageMentions (client-boot s1s/, over JCh1's extractor) adds
// them to the members a notification's ensureModelsArePresent fetches
// (the element's author_id is what the composer knew of the linked
// message's sender).
func SlugAuthors(m *Message) []string {
	if m == nil || len(m.Blocks) == 0 {
		return nil
	}
	var v any
	if json.Unmarshal(m.Blocks, &v) != nil {
		return nil
	}
	var out []string
	var walk func(any)
	walk = func(x any) {
		switch t := x.(type) {
		case []any:
			for _, e := range t {
				walk(e)
			}
		case map[string]any:
			if t["type"] == "message_mention" {
				if id, _ := t["author_id"].(string); id != "" && !slices.Contains(out, id) {
					out = append(out, id)
				}
				return
			}
			for _, e := range t {
				walk(e)
			}
		}
	}
	walk(v)
	return out
}

// String is k as "channel/ts", the key a "names" event names a message
// by (MsgRefs.Messages).
func (k MsgKey) String() string { return k.Channel + "/" + k.TS }

// IsMessageKey reports whether id, from a "names" event, names a message
// ("channel/ts": a message slug's answer) rather than a member, a
// conversation, a bot or a file.
func IsMessageKey(id string) bool { return strings.IndexByte(id, '/') > 0 }
