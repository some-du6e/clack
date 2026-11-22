package messages

// ReactionTip for the reaction pills drawn outside the message list: the
// Threads view's roots and replies, the Unreads view's messages and the
// Activity pane's (internal/ui/views' actReaction), which render Slack's
// same ReactionBar and Reaction, so the same ReactionTip (boot-shared
// Qvn6): the reactors' names, "reacted with :name:" and the emoji, above
// the pill after ReactionTipDelay.

import (
	"image"

	"gioui.org/layout"

	"github.com/some-du6e/clack/internal/core"
	"github.com/some-du6e/clack/internal/imgcache"
	"github.com/some-du6e/clack/internal/ui/theme"
)

// TipHost is what a reaction tooltip reads: the reactors' names (the
// core), the emoji's picture (the images) and the theme. Every Host is
// one, and so is a view's host.
type TipHost interface {
	Core() *core.App
	Theme() *theme.Theme
	Images() *imgcache.Cache
}

// ReactionTipDelay is the pills' tooltip delay (Reaction's Tooltip).
const ReactionTipDelay = reactionTipDelay

// ReactionTooltip is one pill's ReactionTip.
type ReactionTooltip struct{ gt groupTip }

// NewReactionTooltip is the tip of reaction re for the viewer selfID: its
// reactors by name as a channel's pills name them (TooltipMemberName),
// asking for a missing one. Build it once the tip shows, as Slack mounts
// ReactionTip only then.
func NewReactionTooltip(h TipHost, re *core.Reaction, selfID string) ReactionTooltip {
	url := re.URL
	if url == "" && h.Core() != nil {
		url, _, _ = h.Core().Emoji(re.Name)
	}
	txt := reactionTip(h, re, selfID)
	return ReactionTooltip{groupTip{lines: []string{txt}, urls: []string{url}, wide: re.Count > 9}}
}

// Text is the tip's text, the subtitle marked (tests).
func (t *ReactionTooltip) Text() string { return t.gt.text() }

// Draw draws the tip above pill inside bounds (both in the caller's
// coordinates; an empty bounds doesn't limit it). Draw it deferred.
func (t *ReactionTooltip) Draw(gtx layout.Context, h TipHost, pill, bounds image.Rectangle) {
	th := h.Theme()
	reactionGroupTooltip(gtx, h, th, colorsFor(th), &t.gt, pill, bounds)
}
