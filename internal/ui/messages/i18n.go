package messages

import (
	"strings"

	"github.com/some-du6e/clack/internal/i18n"
)

// Slack's I18n instances the message rows draw from besides msgNS and
// threadsNS (util.go), each the namespace of the Slack module that renders
// the string (docs/I18N.md).
var (
	reactionsNS = i18n.NS("reactions")       // ReactionTip's getTip (Qvn6)
	tokenNS     = i18n.NS("channel_token")   // TooltipMemberName (NZA6): "You", "you"
	messagesNS  = i18n.NS("messages")        // UnreadDivider (VxYf)
	savedNS     = i18n.NS("saved_for_later") // the to-do action (boot-shared Yoyn; LaZW in modern.vendor)
	listsNS     = i18n.NS("lists")           // shouldShowAddToList's "Add to list…"
)

// tbTip is toolbar button i's label (its aria-label and tooltip) in the
// locale: each is its action module's name (the hover toolbar and the
// More actions menu share them).
func tbTip(i int, label string) string {
	switch i {
	case tbTodo:
		return savedNS.T(label)
	case tbList:
		return listsNS.T(label)
	}
	return msgNS.T(label)
}

// The reaction tooltip's text, as getTip builds it: Slack's rich strings
// "{user} <span className="c-tooltip__subtitle">reacted with
// {emoji}</span>" (the a84662d2d135 sources the bundles key; the next build
// writes <t1> with these as fallbackHash), whose span is TooltipSubtitle.
// reactionTip encodes the runs in one string: the subtitle's runs between
// tipSubOn and tipSubOff, the rest the tip's bold text.
const (
	tipSubOn  = "\x01"
	tipSubOff = "\x02"
)

// reactionTipRuns formats src (a getTip string) with args and marks its
// TooltipSubtitle span.
func reactionTipRuns(src string, args ...any) string {
	var b strings.Builder
	for _, p := range reactionsNS.RT(src, args...) {
		if p.Tag == "span" || p.Tag == "t1" {
			b.WriteString(tipSubOn + p.Text + tipSubOff)
			continue
		}
		b.WriteString(p.Text)
	}
	return b.String()
}

// plainTip is a tip without its run marks.
func plainTip(txt string) string {
	return strings.NewReplacer(tipSubOn, "", tipSubOff, "").Replace(txt)
}
