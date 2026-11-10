package demo

import (
	"encoding/json"
	"regexp"
	"strings"

	"github.com/some-du6e/clack/internal/emoji"
	"github.com/some-du6e/clack/internal/slack"
)

// Message state changes. Each one updates the world under mu, then
// pushes the realtime events Slack would send (the echo, message_replied,
// reaction_added…) and records Activity items for the viewer.

var liveMentionRe = regexp.MustCompile(`<@([UW][A-Z0-9]+)(?:\|[^>]*)?>`)

// mentions are the user ids text mentions.
func mentionIDs(text string) []string {
	var out []string
	for _, m := range liveMentionRe.FindAllStringSubmatch(text, -1) {
		if !hasStr(out, m[1]) {
			out = append(out, m[1])
		}
	}
	return out
}

func textMentionsSelf(text string) bool {
	return hasStr(mentionIDs(text), SelfID) || strings.Contains(text, "<!channel") || strings.Contains(text, "<!here") ||
		strings.Contains(text, "<!everyone")
}

// botFields fills in a bot user's message identity (bot_id, bot_profile)
// like Slack's app messages and fakeslack.AgentMessage.
func (w *World) botFields(m *slack.Message) {
	for _, b := range w.ws.Bots {
		if b.UserID != m.User {
			continue
		}
		m.BotID = b.BotID
		icons := slack.Icons{}
		if b.Icons != nil {
			icons.Image36, icons.Image48, icons.Image72 = b.Icons["image_36"], b.Icons["image_48"], b.Icons["image_72"]
		}
		m.BotProfile = &slack.BotProfile{ID: b.BotID, Name: b.Name, AppID: b.AppID, UserID: b.UserID, TeamID: TeamID, Icons: icons}
	}
}

// post stores m in ch (m.ThreadTS: a reply) and pushes it. fromAPI is a
// chat.postMessage (its client_msg_id rides on the echo). The viewer's
// posts wake the bots. It returns the ts and the stored message.
func (w *World) post(ch string, m slack.Message, fromAPI bool) (string, slack.Message) {
	if m.Type == "" {
		m.Type = "message"
	}
	w.botFields(&m)
	var evs []any
	var parent *slack.Message
	w.mu.Lock()
	m.TS = w.nextTSLocked()
	if m.ThreadTS == m.TS {
		m.ThreadTS = ""
	}
	if m.ThreadTS != "" {
		p := w.msgPtrLocked(ch, m.ThreadTS)
		if p != nil && p.ThreadTS != "" && p.ThreadTS != p.TS {
			// a reply to a reply: Slack threads it under the root
			m.ThreadTS = p.ThreadTS
			p = w.msgPtrLocked(ch, m.ThreadTS)
		}
		if p == nil {
			m.ThreadTS = "" // no such parent: post top level
		} else {
			parent = p
		}
	}
	if parent != nil {
		m.ParentUser = parent.User
		if w.replies[ch] == nil {
			w.replies[ch] = map[string][]slack.Message{}
		}
		w.replies[ch][parent.TS] = append(w.replies[ch][parent.TS], m)
		parent.ThreadTS = parent.TS
		parent.ReplyCount++
		parent.LatestReply = m.TS
		if !hasStr(parent.ReplyUsers, m.User) {
			parent.ReplyUsers = append(parent.ReplyUsers, m.User)
		}
		if m.Subtype == "thread_broadcast" {
			root := copyMsg(*parent)
			m.Root = &root
			w.hist[ch] = append([]slack.Message{m}, w.hist[ch]...)
		}
	} else {
		w.hist[ch] = append([]slack.Message{m}, w.hist[ch]...)
	}
	if m.User == SelfID && (parent == nil || m.Subtype == "thread_broadcast") {
		w.lastRead[ch] = m.TS // your own message reads the conversation
	}
	stored := copyMsg(m)
	echo := msgEvent(ch, m)
	evs = append(evs, echo)
	if parent != nil {
		pm := copyMsg(*parent)
		pm.Channel = ""
		evs = append(evs, map[string]any{"type": "message", "subtype": "message_replied", "channel": ch, "hidden": true,
			"message": pm, "ts": m.TS, "event_ts": m.TS})
	}
	evs = append(evs, w.activityForPostLocked(ch, m, parent)...)
	w.mu.Unlock()
	w.pushAll(evs)
	if m.User == SelfID {
		w.bots.onViewerPost(ch, stored, fromAPI)
	}
	return m.TS, stored
}

// msgEvent is a message's realtime event: the message with its channel.
func msgEvent(ch string, m slack.Message) map[string]any {
	b, _ := json.Marshal(m)
	var ev map[string]any
	_ = json.Unmarshal(b, &ev)
	ev["type"] = "message"
	ev["channel"] = ch
	ev["event_ts"] = m.TS
	ev["team"] = TeamID
	if m.BotProfile != nil {
		ev["app_id"] = m.BotProfile.AppID
	}
	return ev
}

func (w *World) pushAll(evs []any) {
	for _, e := range evs {
		w.fs.Push(e)
	}
}

// change replaces a stored message with f's edit and pushes
// message_changed. It reports false for a missing message.
func (w *World) change(ch, ts string, f func(m *slack.Message)) (slack.Message, bool) {
	w.mu.Lock()
	p := w.msgPtrLocked(ch, ts)
	if p == nil {
		w.mu.Unlock()
		return slack.Message{}, false
	}
	prev := copyMsg(*p)
	f(p)
	m := copyMsg(*p)
	// a broadcast reply has a copy in the channel and one in its thread
	if m.ThreadTS != "" && m.ThreadTS != m.TS {
		for i := range w.hist[ch] {
			if w.hist[ch][i].TS == ts && p != &w.hist[ch][i] {
				w.hist[ch][i] = copyMsg(m)
			}
		}
		rs := w.replies[ch][m.ThreadTS]
		for i := range rs {
			if rs[i].TS == ts && p != &rs[i] {
				rs[i] = copyMsg(m)
			}
		}
	}
	w.mu.Unlock()
	w.fs.Push(changedEvent(ch, m, &prev))
	return m, true
}

func changedEvent(ch string, m slack.Message, prev *slack.Message) map[string]any {
	ev := map[string]any{"type": "message", "subtype": "message_changed", "channel": ch, "hidden": true, "message": m,
		"ts": m.TS, "event_ts": m.TS}
	if prev != nil {
		ev["previous_message"] = prev
	}
	return ev
}

// remove deletes a message (a parent with replies becomes a tombstone,
// as Slack keeps the thread).
func (w *World) remove(ch, ts string) bool {
	var evs []any
	w.mu.Lock()
	p := w.msgPtrLocked(ch, ts)
	if p == nil {
		w.mu.Unlock()
		return false
	}
	prev := copyMsg(*p)
	isReply := prev.ThreadTS != "" && prev.ThreadTS != prev.TS
	if !isReply && prev.ReplyCount > 0 {
		p.Subtype, p.Text, p.Blocks, p.Files, p.Attachments, p.Reactions = "tombstone", "This message was deleted.", nil, nil, nil, nil
		p.Hidden = true
		evs = append(evs, changedEvent(ch, copyMsg(*p), &prev))
		w.mu.Unlock()
		w.pushAll(evs)
		return true
	}
	h := w.hist[ch]
	for i := range h {
		if h[i].TS == ts {
			w.hist[ch] = append(h[:i:i], h[i+1:]...)
			break
		}
	}
	if isReply {
		rs := w.replies[ch][prev.ThreadTS]
		for i := range rs {
			if rs[i].TS == ts {
				w.replies[ch][prev.ThreadTS] = append(rs[:i:i], rs[i+1:]...)
				break
			}
		}
	}
	evs = append(evs, map[string]any{"type": "message", "subtype": "message_deleted", "channel": ch, "hidden": true,
		"deleted_ts": ts, "previous_message": prev, "ts": w.nextTSLocked()})
	if isReply {
		if par := w.msgPtrLocked(ch, prev.ThreadTS); par != nil {
			rs := w.replies[ch][prev.ThreadTS]
			par.ReplyCount = len(rs)
			par.ReplyUsers, par.LatestReply = nil, ""
			for _, r := range rs {
				if !hasStr(par.ReplyUsers, r.User) {
					par.ReplyUsers = append(par.ReplyUsers, r.User)
				}
				par.LatestReply = r.TS
			}
			if len(rs) == 0 && par.Subtype == "tombstone" {
				// the last reply of a deleted parent: the thread goes
				for i := range w.hist[ch] {
					if w.hist[ch][i].TS == par.TS {
						w.hist[ch] = append(w.hist[ch][:i:i], w.hist[ch][i+1:]...)
						break
					}
				}
			} else {
				evs = append(evs, changedEvent(ch, copyMsg(*par), nil))
			}
		}
	}
	w.dropActivityLocked(ch, ts)
	w.mu.Unlock()
	w.pushAll(evs)
	return true
}

// react adds (or removes) user's reaction; false when nothing changed
// (already reacted, or no such reaction / message).
func (w *World) react(user, ch, ts, name string, add bool) bool {
	name = strings.Trim(name, ":")
	w.mu.Lock()
	p := w.msgPtrLocked(ch, ts)
	if p == nil {
		w.mu.Unlock()
		return false
	}
	changed := false
	idx := -1
	for i := range p.Reactions {
		if p.Reactions[i].Name == name {
			idx = i
		}
	}
	if add {
		if idx < 0 {
			p.Reactions = append(p.Reactions, slack.Reaction{Name: name})
			idx = len(p.Reactions) - 1
		}
		r := &p.Reactions[idx]
		if !hasStr(r.Users, user) {
			r.Users = append(append([]string(nil), r.Users...), user)
			r.Count++
			changed = true
		}
	} else if idx >= 0 {
		r := &p.Reactions[idx]
		if hasStr(r.Users, user) {
			var keep []string
			for _, u := range r.Users {
				if u != user {
					keep = append(keep, u)
				}
			}
			r.Users, r.Count = keep, len(keep)
			changed = true
			if r.Count == 0 {
				p.Reactions = append(p.Reactions[:idx:idx], p.Reactions[idx+1:]...)
				if len(p.Reactions) == 0 {
					p.Reactions = nil
				}
			}
		}
	}
	if !changed {
		w.mu.Unlock()
		return false
	}
	owner := p.User
	typ := "reaction_removed"
	if add {
		typ = "reaction_added"
	}
	evTS := w.nextTSLocked()
	evs := []any{map[string]any{"type": typ, "user": user, "reaction": name, "item_user": owner, "event_ts": evTS, "ts": evTS,
		"item": map[string]any{"type": "message", "channel": ch, "ts": ts}}}
	if add && owner == SelfID && user != SelfID {
		evs = append(evs, w.addActivityLocked("message_reaction", ch, ts, "", user, name)...)
	}
	w.mu.Unlock()
	w.pushAll(evs)
	return true
}

// validEmoji reports whether a reaction name exists here: one of Slack's
// standard emoji or the workspace's custom ones.
func (w *World) validEmoji(name string) bool {
	name = strings.Trim(name, ":")
	if base, _, ok := strings.Cut(name, "::"); ok { // skin tones
		name = base
	}
	if _, ok := w.ws.Emoji[name]; ok {
		return true
	}
	_, _, ok := emoji.Standard(name)
	return ok
}

// setPin pins or unpins a message and pushes pin_added / pin_removed.
func (w *World) setPin(user, ch, ts string, on bool) string {
	w.mu.Lock()
	p := w.msgPtrLocked(ch, ts)
	if p == nil {
		w.mu.Unlock()
		return "message_not_found"
	}
	idx := -1
	for i, x := range w.pins[ch] {
		if x.TS == ts {
			idx = i
		}
	}
	if on == (idx >= 0) {
		w.mu.Unlock()
		if on {
			return "already_pinned"
		}
		return "no_pin"
	}
	now := w.now().Unix()
	if on {
		w.pins[ch] = append(w.pins[ch], livePin{TS: ts, By: user, At: now})
		p.PinnedTo = []string{ch}
		p.PinnedInfo = &slack.PinnedInfo{Channel: ch, PinnedBy: user, PinnedTS: now}
	} else {
		w.pins[ch] = append(w.pins[ch][:idx:idx], w.pins[ch][idx+1:]...)
		p.PinnedTo, p.PinnedInfo = nil, nil
	}
	m := copyMsg(*p)
	typ := "pin_removed"
	if on {
		typ = "pin_added"
	}
	evTS := w.nextTSLocked()
	w.mu.Unlock()
	w.fs.Push(map[string]any{"type": typ, "user": user, "channel_id": ch, "event_ts": evTS, "has_pins": on,
		"item": map[string]any{"type": "message", "channel": ch, "created": now, "created_by": user, "message": m}})
	return ""
}

// typing pushes user_typing for user in ch (a thread with threadTS).
func (w *World) typing(user, ch, threadTS string) {
	ev := map[string]any{"type": "user_typing", "channel": ch, "user": user, "id": 1}
	if threadTS != "" {
		ev["thread_ts"] = threadTS
	}
	w.fs.Push(ev)
}
