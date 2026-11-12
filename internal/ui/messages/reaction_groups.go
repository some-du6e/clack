package messages

import (
	"context"
	"strconv"
	"strings"
	"time"

	"github.com/some-du6e/clack/internal/core"
	"github.com/some-du6e/clack/internal/emoji"
	"github.com/some-du6e/clack/internal/taut"
)

// Skin tones in the reaction bar, as Slack's web client does them (CDN
// build 4d0c78085232, gantry-v2-async-client-boot-shared):
//
//   - The reactions store (client-boot daKM toReaction) gives every
//     reaction a baseName, its name before "::" ("+1::skin-tone-4" is
//     "+1").
//   - ReactionBar (Ltiy) draws one Reaction per getReactionGroups (hzIJ)
//     group: the reactions sharing a baseName, in the order their
//     baseName first appears, with count the sum of their counts, users
//     the union of their users (lodash uniq of the flattened lists) and
//     reactions the group's own reactions in their order.
//   - Reaction (9FoR) draws each of the group's reactions' emoji (the
//     first ten, then "…", c-reaction__truncation) and the count; it is
//     reacted while users includes the bar's current user. The tooltip
//     is ReactionTip (Qvn6) over the group's reactions: their emoji, then
//     one getTip line per reaction, or a single line under baseName for
//     an emoji whose skin tones are per person (isMultiSkintoneCapable).
//   - A click is handleReactionClick (aHRx) with the group's name and
//     actionSource reaction_toggle_button: changeReactionFromUserAction
//     (QOQU) removes every reaction of the group the viewer made, and
//     with none adds the name in the viewer's preferred_skin_tone
//     (getSkinToneModifiedName, QMob) when the emoji takes skin tones
//     (isEmojiNameSkinToneModifiable: it has skinVariations).

// reactGroup is one getReactionGroups group of the row's reactions.
type reactGroup struct {
	name  string // the baseName
	count int    // the reactions' counts, summed
	idx   []int  // the reactions, indices into r.m.Reactions
}

// reactionBase is toReaction's baseName.
func reactionBase(name string) string {
	if i := strings.Index(name, "::"); i >= 0 {
		return name[:i]
	}
	return name
}

// groupReactions is getReactionGroups over rs, into gs's storage (no
// allocation once the buffers are as big as the reactions).
func groupReactions(gs []reactGroup, rs []core.Reaction) []reactGroup {
	gs = gs[:0]
	for i := range rs {
		b := reactionBase(rs[i].Name)
		k := -1
		for j := range gs {
			if gs[j].name == b {
				k = j
				break
			}
		}
		if k < 0 {
			if len(gs) < cap(gs) {
				gs = gs[:len(gs)+1]
				g := &gs[len(gs)-1]
				g.name, g.count, g.idx = b, 0, g.idx[:0]
			} else {
				gs = append(gs, reactGroup{name: b})
			}
			k = len(gs) - 1
		}
		gs[k].count += rs[i].Count
		gs[k].idx = append(gs[k].idx, i)
	}
	return gs
}

// groupUsers is the group's users: lodash uniq over its reactions' users.
func groupUsers(rs []core.Reaction, g *reactGroup) []string {
	var out []string
	for _, i := range g.idx {
		for _, u := range rs[i].Users {
			dup := false
			for _, o := range out {
				if o == u {
					dup = true
					break
				}
			}
			if !dup {
				out = append(out, u)
			}
		}
	}
	return out
}

// reactionHas reports whether user reacted re (the viewer: re.Self, which
// core keeps even when users is cut at 50).
func reactionHas(re *core.Reaction, user, self string) bool {
	if user == "" || user == self {
		if re.Self {
			return true
		}
		user = self
	}
	for _, u := range re.Users {
		if u == user {
			return true
		}
	}
	return false
}

// skinToneName is getSkinToneModifiedName: base in skin tone tone
// ("2"…"6") when the standard emoji has skin variations
// (isEmojiNameSkinToneModifiable), else base.
func skinToneName(base, tone string) string {
	n, err := strconv.Atoi(tone)
	if err != nil || n < 2 || n > 6 || !skinToneModifiable(base) {
		return base
	}
	return base + "::skin-tone-" + tone
}

// skinToneModifiable is isEmojiNameSkinToneModifiable: a standard emoji
// with skin variations (its toned image differs).
func skinToneModifiable(name string) bool {
	plain, _, ok := emoji.Standard(name)
	if !ok {
		return false
	}
	toned, _, _ := emoji.Standard(name + "::skin-tone-2")
	return toned != plain
}

// multiSkinTone are the standard emoji whose skin tones are per person
// (multiSkinTone in Slack's emoji data, client-boot IYuW): ReactionTip
// shows one line for all of their tones.
var multiSkinTone = map[string]bool{
	"man-heart-man": true, "man-kiss-man": true, "woman-heart-man": true, "woman-heart-woman": true,
	"woman-kiss-man": true, "woman-kiss-woman": true, "man_and_woman_holding_hands": true,
	"two_men_holding_hands": true, "two_women_holding_hands": true, "couplekiss": true,
	"couple_with_heart": true, "handshake": true, "people_holding_hands": true,
	"women-with-bunny-ears-partying": true, "men-with-bunny-ears-partying": true, "dancers": true,
	"woman-wrestling": true, "man-wrestling": true, "wrestlers": true,
}

// toggleTargets is changeReactionFromUserAction for the pill's toggle
// (actionSource reaction_toggle_button) on the reactions rs: every
// reaction of base's group that self made, to remove; with none, the name
// to add (base in the preferred skin tone tone).
func toggleTargets(rs []core.Reaction, base, self, tone string) (add string, remove []string) {
	f := skinToneName(base, tone)
	fb := reactionBase(f)
	for i := range rs {
		if reactionBase(rs[i].Name) == fb && reactionHas(&rs[i], self, self) {
			remove = append(remove, rs[i].Name)
		}
	}
	if len(remove) > 0 {
		return "", remove
	}
	return f, nil
}

// reactionToggleButton is QOQU's actionSource for a pill's click.
const reactionToggleButton = "reaction_toggle_button"

// reactGroup is a pill's click: handleReactionClick (aHRx) dispatches
// changeReactionFromUserAction with the group's name and actionSource
// reaction_toggle_button, whose native (QOQU) takes the viewer's
// reactions of the group off, or adds the name in their
// preferred_skin_tone, through submitAddOrRemoveReaction each.
func (s *listState) reactGroup(h Host, r *row, base, self string) {
	app := h.Core()
	if app == nil {
		return
	}
	key := reactionKey(&r.src)
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
		defer cancel()
		_, err := taut.Dispatch(ctx, tautOf(), "changeReactionFromUserAction",
			rxnUserArgs{Key: key, Name: base, ActionSource: reactionToggleButton, UseDefaultSkinToneForMultiSkinToneEmoji: true},
			func(ctx context.Context, a rxnUserArgs) (any, error) {
				if a.Add != nil || a.ActionSource != reactionToggleButton {
					// a plugin turned it into a plain toggle of one name
					on := !s.hasOwnReaction(a.Key, a.Name)
					if a.Add != nil {
						on = *a.Add
					}
					if on && !a.SkipFrecencyRecord {
						app.RecordEmojiFrecency(a.Name, "")
					}
					return s.submitReaction(ctx, h, app, a.Key, a.Name, on, self)
				}
				add, remove := toggleTargets(s.reactionsOf(a.Key), a.Name, self, app.Prefs().SkinTone)
				if add != "" {
					if !a.SkipFrecencyRecord {
						app.RecordEmojiFrecency(add, "") // recordEmojiFrecencyEvent {emojiName, text: ""}
					}
					return s.submitReaction(ctx, h, app, a.Key, add, true, self)
				}
				var first error
				for _, name := range remove {
					if _, err := s.submitReaction(ctx, h, app, a.Key, name, false, self); err != nil && first == nil {
						first = err
					}
				}
				return nil, first
			})
		if err != nil {
			h.Toast("Couldn't update reaction")
		}
	}()
}

// reactionsOf is a copy of the reactions on the message with Slack
// reaction key key (any goroutine).
func (s *listState) reactionsOf(key string) []core.Reaction {
	_, ts, ok := parseRxnKey(key)
	if !ok {
		return nil
	}
	s.m.mu.Lock()
	defer s.m.mu.Unlock()
	if i := s.m.find(ts); i >= 0 {
		rs := s.m.msgs[i].m.Reactions
		out := make([]core.Reaction, len(rs))
		for k := range rs {
			out[k] = rs[k]
			out[k].Users = append([]string(nil), rs[k].Users...)
		}
		return out
	}
	return nil
}
