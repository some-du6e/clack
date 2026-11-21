package blockkit

import "github.com/some-du6e/clack/internal/ui/richview"

// twoDrawings is two drawings of me's elements (InView), and openIn opens
// a block in one of them. The red-evidence commit replaces this file with
// a stub drawing both through the shared host.
func twoDrawings(me *Message) (richview.Elements, richview.Elements) {
	return InView(me, NewViewID()), InView(me, NewViewID())
}

func openIn(el richview.Elements, key string) {
	v := el.(elemView)
	prev := v.me.inst
	v.me.inst = v.id
	v.me.agentState().open[key] = true
	v.me.inst = prev
}
