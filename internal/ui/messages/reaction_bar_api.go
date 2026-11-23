package messages

// The reaction bar outside the message list: the Threads, Unreads,
// Activity, Pins and Later views render Slack's same MessageReactionBar
// (ReactionBar Ltiy, Reaction 9FoR), so a pill there is a group of
// getReactionGroups, its click is handleReactionClick, it is disabled
// where canAddReaction is false, and its tooltip is the group's
// ReactionTip, as a channel's (reaction_groups.go).

import (
	"context"
	"fmt"

	"github.com/some-du6e/clack/internal/core"
	"github.com/some-du6e/clack/internal/taut"
)

// ReactionGroup is one getReactionGroups group: the reactions sharing a
// baseName.
type ReactionGroup struct {
	Name  string // the baseName
	Count int    // the reactions' counts, summed
	Idx   []int  // the group's reactions, indices into the message's
}

// ReactionGroups is getReactionGroups over rs, in the order each
// baseName first appears.
func ReactionGroups(rs []core.Reaction) []ReactionGroup {
	gs := groupReactions(nil, rs)
	out := make([]ReactionGroup, len(gs))
	for i, g := range gs {
		out[i] = ReactionGroup{Name: g.name, Count: g.count, Idx: append([]int(nil), g.idx...)}
	}
	return out
}

func (g ReactionGroup) group() *reactGroup {
	return &reactGroup{name: g.Name, count: g.Count, idx: g.Idx}
}

// Users is the group's users: the union of its reactions' (lodash uniq).
func (g ReactionGroup) Users(rs []core.Reaction) []string { return groupUsers(rs, g.group()) }

// Reacted reports whether self reacted with any of the group's reactions.
func (g ReactionGroup) Reacted(rs []core.Reaction, self string) bool {
	for _, i := range g.Idx {
		if i < len(rs) && reactionHas(&rs[i], self, self) {
			return true
		}
	}
	return false
}

// ReactionEmojiMax is how many of a group's emoji its pill draws before
// "…" (Reaction's c-reaction__truncation).
const ReactionEmojiMax = reactEmojiMax

// NewReactionGroupTooltip is the ReactionTip of group g of rs for the
// viewer selfID, as a channel's pill has it. Build it once the tip shows.
func NewReactionGroupTooltip(h TipHost, rs []core.Reaction, g ReactionGroup, selfID string) ReactionTooltip {
	return ReactionTooltip{groupTipOf(h, rs, g.group(), selfID)}
}

// ReactionAria is the pill's aria-label for a group of count reactions.
func ReactionAria(name string, count int) string {
	return reactionAria(&core.Reaction{Name: name, Count: count})
}

// ReactHost is what a pill's click outside the list needs: the core, and
// the toast and redraw a change or its failure asks for.
type ReactHost interface {
	Core() *core.App
	Toast(text string)
	Invalidate()
}

// ReactionLocal is a bar's own copy of its message, for a message the
// store doesn't hold (submitAddOrRemoveReaction's update then has no
// store copy to change, as the list's reactNow mutates its own).
type ReactionLocal interface {
	// Reactions are the drawn copy's, with its local changes (any
	// goroutine).
	Reactions() []core.Reaction
	// Change adds (or takes off) the viewer's name locally; undo takes a
	// change back after reactions.add/remove failed (any goroutine).
	Change(name string, add, undo bool)
}

// ClickReactionGroup is a pill's click on the message ch/ts outside the
// list: handleReactionClick with the group's name base (actionSource
// reaction_toggle_button), whose changeReactionFromUserAction takes the
// viewer's reactions of the group off, or adds base in their
// preferred_skin_tone, each through submitAddOrRemoveReaction. Plugins
// may wrap either thunk, as in a channel. The message's reactions are the
// store's copy (Slack's views draw the messages store; Clack's keep the
// copies they fetched there, store/fetched.go): the optimistic change is
// core's (ReactBegin: the store changes and "update" reaches the views
// now; a failure is rolled back the same way and toasted). A message the
// store doesn't hold changes in local instead, and back on a failure.
func ClickReactionGroup(h ReactHost, ch, ts, base string, local ReactionLocal) {
	app := h.Core()
	if app == nil {
		return
	}
	self := app.Self().ID
	of := func(key string) []core.Reaction {
		if c, t, ok := parseRxnKey(key); ok {
			if m, ok := app.HeldMessage(c, t); ok {
				return m.Reactions
			}
		}
		return local.Reactions()
	}
	reactGroupWith(h, app, reactionKey(&core.Message{Channel: ch, TS: ts}), base, self, of,
		func(ctx context.Context, key, name string, add bool) (any, error) {
			return taut.Dispatch(ctx, tautOf(), "submitAddOrRemoveReaction", rxnSubmitArgs{ReactionKey: key, Name: name, Add: add},
				func(ctx context.Context, a rxnSubmitArgs) (any, error) {
					c, t, ok := parseRxnKey(a.ReactionKey)
					if !ok {
						return nil, fmt.Errorf("messages: bad reaction key %q", a.ReactionKey)
					}
					return nil, submitOutside(ctx, h, app, local, c, t, a.Name, a.Add)
				})
		})
}

// submitOutside is submitAddOrRemoveReaction's native for a bar outside
// the list: core's optimistic change (ReactBegin) when the store holds
// the message, else local's, then reactions.add/remove; a failure takes
// back only a change local made (core rolls its own back).
func submitOutside(ctx context.Context, h ReactHost, app *core.App, local ReactionLocal, ch, ts, name string, add bool) error {
	ev, call := app.ReactBegin(ch, ts, name, add)
	changed := false // a change of local's
	if ev != nil {
		ApplyEvent(app, *ev) // a channel list showing it too, this frame
	} else if _, ok := app.HeldMessage(ch, ts); !ok {
		local.Change(name, add, false)
		changed = true
	}
	h.Invalidate()
	err := call(ctx)
	if err != nil && changed {
		local.Change(name, add, true)
	}
	h.Invalidate()
	return err
}
