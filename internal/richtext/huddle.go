package richtext

import (
	"encoding/json"
	"strconv"
	"strings"
	"time"
)

// A huddle_thread message's body, as Slack's HuddleMsgText draws it
// (gantry-v2-async-client-boot-shared EKdW, getHuddleMsgText dbqw, the
// room → call mapping and selectors in client-boot zq6z / b0mi, build
// 4d0c78085232): one automated sentence (c-message__body--automated) and,
// while the huddle is live and you're not in it, a "Join Huddle" button
// with the faces of up to three people in it.
//
// The call comes from the message's room: activeParticipants are its
// participants, allParticipants its participant_history,
// missedParticipants its missed_participants (an object by user).

// HuddleRoom is the huddle a huddle_thread message is about.
type HuddleRoom struct {
	ID                 string
	Channel            string // the conversation (the message's, or the room's first)
	DateStart, DateEnd int64
	HasEnded           bool
	Active, All        []string
	Missed             map[string]bool
	// Live is isHuddleActive: not ended, and someone's in it (or nobody
	// has been yet).
	Live bool
	// Joinable is shouldAddJoinButton: live, and you're not in it.
	Joinable bool
}

// HuddleTextOpts are the conversation and the experiments the sentence
// depends on.
type HuddleTextOpts struct {
	IM, MPIM bool   // the conversation is a DM / group DM
	Partner  string // the DM's other member (memberFromIm)
	// Redesign is system_messages_redesign_m1 ("on"): "{a}, {b}, and N
	// others" in place of "{a} and N others", and "Enjoy the
	// tranquility, …" without "You’re the only one here."
	Redesign bool
	// Missed is huddles_missed_notification ("on"): a missed invite counts
	// (getDidUserMissHuddleInvite).
	Missed bool
}

// HuddleResolver is an optional Resolver method: the conversation and
// the experiment groups a huddle message's sentence depends on.
type HuddleResolver interface {
	HuddleTextOpts(channel string) HuddleTextOpts
}

// ParseHuddleRoom reads a message's room ("" or {} is none).
func ParseHuddleRoom(raw json.RawMessage) (*HuddleRoom, bool) {
	var room struct {
		ID                 string          `json:"id"`
		Channels           []string        `json:"channels"`
		ChannelID          string          `json:"channel_id"`
		DateStart          json.Number     `json:"date_start"`
		DateEnd            json.Number     `json:"date_end"`
		HasEnded           bool            `json:"has_ended"`
		Participants       []string        `json:"participants"`
		ParticipantHistory []string        `json:"participant_history"`
		MissedParticipants json.RawMessage `json:"missed_participants"`
	}
	if len(raw) <= 2 || json.Unmarshal(raw, &room) != nil {
		return nil, false
	}
	h := &HuddleRoom{ID: room.ID, HasEnded: room.HasEnded, Active: room.Participants, All: room.ParticipantHistory, Channel: room.ChannelID}
	if len(room.Channels) > 0 {
		h.Channel = room.Channels[0]
	}
	h.DateStart, _ = room.DateStart.Int64()
	h.DateEnd, _ = room.DateEnd.Int64()
	var missed map[string]json.RawMessage
	if json.Unmarshal(room.MissedParticipants, &missed) == nil {
		h.Missed = map[string]bool{}
		for u := range missed {
			h.Missed[u] = true
		}
	}
	// isHuddleActive: !hasEnded && (no history yet || someone active)
	h.Live = !h.HasEnded && h.DateEnd == 0 && (len(h.All) == 0 || len(h.Active) > 0)
	return h, true
}

// in reports whether u is in l.
func in(l []string, u string) bool {
	for _, x := range l {
		if x == u {
			return true
		}
	}
	return false
}

// without is l minus u (getParticipantsIds' userIdsToExclude).
func without(l []string, u string) []string {
	var out []string
	for _, x := range l {
		if x != u {
			out = append(out, x)
		}
	}
	return out
}

// HuddleMsgText is getHuddleMsgText for the viewer self: the sentence
// as spans, nil when Slack shows none. now is for a huddle without an
// end (its duration so far).
func HuddleMsgText(r Resolver, h *HuddleRoom, o HuddleTextOpts, now time.Time) []Span {
	self := r.SelfID()
	youIn := h.Live && in(h.Active, self)                                                                  // isUserInActiveHuddle
	youWere := !h.Live && in(h.All, self)                                                                  // youWereInEndedHuddle (qp)
	missed := o.Missed && h.HasEnded && h.Missed[self]                                                     // getDidUserMissHuddleInvite
	partnerLeft := o.IM && o.Partner != "" && !(h.Live && in(h.Active, o.Partner)) && in(h.All, o.Partner) // imUserLeftHuddle (xe)
	active := without(h.Active, self)
	all := without(h.All, self)
	dm := o.IM || o.MPIM
	if len(active) == 0 && !youIn && len(all) == 0 && !youWere {
		return nil
	}
	name := func(id string) string {
		if n := r.UserName(id); n != "" {
			return strings.TrimPrefix(n, "@")
		}
		return id
	}
	names := func(ids []string) []string {
		out := make([]string, len(ids))
		for i, id := range ids {
			out[i] = name(id)
		}
		return out
	}
	text := func(s string) []Span { return []Span{{Kind: Text, Text: s}} }
	dur := huddleDuration(h, now)
	switch {
	case h.Live && !youIn: // getAlreadyThereText
		if dm || len(active) == 0 {
			return nil
		}
		return text(listed(names(active), o.Redesign, "is", "are") + " already there.")
	case h.Live && youIn:
		if len(active) == 0 && (!o.IM || partnerLeft) {
			if o.Redesign {
				return text("Enjoy the tranquility, or invite someone.")
			}
			return text("You’re the only one here. Enjoy the tranquility, or invite someone.")
		}
		if len(active) == 0 && o.IM && !partnerLeft {
			return text(name(o.Partner) + " has received a notification to join")
		}
		return text(listed(names(active), o.Redesign, "is", "are") + " here too.")
	case !h.Live && youWere && len(all) == 0:
		out := text("You were in the huddle by yourself for " + dur + ". A nice, meditative session ")
		if e, ok := emojiSpan(r, "tea"); ok {
			return append(out, e)
		}
		return append(out, Span{Kind: Text, Text: ":tea:"})
	case !h.Live && youWere:
		return text(youAnd(names(all), o.Redesign) + " in the huddle for " + dur + ".")
	case (!h.Live && !youWere) || missed:
		return text(listed(names(all), o.Redesign, "was", "were") + " in the huddle for " + dur + ".")
	}
	return nil
}

// listed is the "{participants, plural, =0 {{a} is} =1 {{a} and {b} are}
// other {…}}" part: "{a} and N others" (the N others carry the rest's
// names in a tooltip in Slack), or with the redesign "{a}, {b}, and N
// others" / "1 other".
func listed(ns []string, redesign bool, one, many string) string {
	switch len(ns) {
	case 0:
		return ""
	case 1:
		return ns[0] + " " + one
	case 2:
		return ns[0] + " and " + ns[1] + " " + many
	}
	if redesign {
		rest := len(ns) - 2
		other := strconv.Itoa(rest) + " others"
		if rest == 1 {
			other = "1 other"
		}
		return ns[0] + ", " + ns[1] + ", and " + other + " " + many
	}
	return ns[0] + " and " + strconv.Itoa(len(ns)-1) + " others " + many
}

// youAnd is "You and {a} were", "You, {a}, and {b} were", and the longer
// forms.
func youAnd(ns []string, redesign bool) string {
	switch len(ns) {
	case 1:
		return "You and " + ns[0] + " were"
	case 2:
		return "You, " + ns[0] + ", and " + ns[1] + " were"
	}
	if redesign {
		rest := len(ns) - 2
		other := strconv.Itoa(rest) + " others"
		if rest == 1 {
			other = "1 other"
		}
		return "You, " + ns[0] + ", " + ns[1] + ", and " + other + " were"
	}
	return "You, " + ns[0] + ", and " + strconv.Itoa(len(ns)-1) + " others were"
}

// huddleDuration is HuddleDuration (A6NG over b0mi Tm): whole minutes
// from date_start to date_end (or now), "{hours}h {minutes}m" from an
// hour on, else "{minutes}m".
func huddleDuration(h *HuddleRoom, now time.Time) string {
	end := h.DateEnd
	if end == 0 {
		end = now.Unix()
	}
	m := max(end-h.DateStart, 0) / 60
	if m >= 60 {
		return strconv.FormatInt(m/60, 10) + "h " + strconv.FormatInt(m%60, 10) + "m"
	}
	return strconv.FormatInt(m, 10) + "m"
}
