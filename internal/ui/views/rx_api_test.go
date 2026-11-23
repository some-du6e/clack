package views

import (
	"gioui.org/widget"

	"github.com/some-du6e/clack/internal/core"
	"github.com/some-du6e/clack/internal/ui/messages"
	"github.com/some-du6e/clack/internal/ui/slackc"
)

// The reaction tests' reach into the bar (reaction_click_test.go), so a
// red run can stub them on the code before it.

// rxPillBtn is the pill button of bar key's group (key + ":" + name).
func rxPillBtn(b *base, key string) *widget.Clickable { return b.pillBtns[key] }

// rxReacted reports whether the bar draws m's name reaction as the
// viewer's.
func rxReacted(b *base, m *core.Message, name string) bool {
	return reactedIn(b.withLocalReactions(m).Reactions, name)
}

// rxBar is m's bar's props with canReact can, its i-th pill's button
// props, and whether the i-th pill is the viewer's.
func rxBar(b *base, m *core.Message, can bool) (slackc.Props, func(int) slackc.Props, func(int) bool) {
	a := &actBar{b: b, m: m, key: "message-" + m.TS + "-" + m.Channel, groups: messages.ReactionGroups(m.Reactions), can: can}
	pill := func(i int) *actReact { return &actReact{b: b, m: m, g: a.groups[i], key: a.key, can: can} }
	return a.props(), func(i int) slackc.Props { return pill(i).buttonProps() }, func(i int) bool { return pill(i).reacted() }
}
