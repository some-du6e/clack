package slackc

import (
	"image/color"
	"testing"

	"github.com/some-du6e/clack/internal/ui/slackc/css"
)

// A native's declared kids (ClassDef.Kids) answer :has(): Taut's and
// Slick's StreamerMode blur a private channel's name in an
// InlineChannelEntity through
// .c-inline_channel_entity:has([data-inline-channel-type-icon^="lock"])
// :not([data-qa="activity-item-container"] *) .c-channel_entity__name,
// which looks for the lock icon Slack's InlineChannelIcon puts before the
// name. Natives without declared kids keep answering :has() with false
// and :empty with no.
func TestNativeKidsHas(t *testing.T) {
	defer func() { stack = stack[:0] }()
	var (
		lock    = Class("c-inline_channel_icon c-inline_channel_icon--space", "data-inline-channel-type-icon", "lock").Tag("span")
		hash    = Class("c-inline_channel_icon c-inline_channel_icon--space", "data-inline-channel-type-icon", "channel").Tag("span")
		avLock  = Class("c-base_inline_entity__avatar").Tag("span").Kids(lock)
		avHash  = Class("c-base_inline_entity__avatar").Tag("span").Kids(hash)
		nameDef = Class("c-channel_entity__name").Tag("span")
		private = Class("c-base_inline_entity c-inline_channel_entity").Tag("span").Kids(avLock)
		public  = Class("c-base_inline_entity c-inline_channel_entity").Tag("span").Kids(avHash)
		actRow  = Class("p-activity_ia4_page__item", "data-qa", "activity-item-container")
		plain   = Class("c-message_kit__blocks")
	)
	SetStyle("test:kids", `html.taut-streamer-mode .c-inline_channel_entity:has([data-inline-channel-type-icon^="lock"]):not([data-qa="activity-item-container"] *) .c-channel_entity__name { filter: blur(4px) }
html.taut-streamer-mode .c-inline_channel_entity:hover .c-channel_entity__name { filter: none }
html.taut-streamer-mode .taut-streamer-mode__revealed .c-inline_channel_entity:has([data-inline-channel-type-icon^="lock"]):not([data-qa="activity-item-container"] *) .c-channel_entity__name { filter: none }
.c-message_kit__blocks:empty { color: red }
.c-message_kit__blocks:has(*) { color: blue }`)
	defer RemoveStyle("test:kids")
	g := ctx()
	htmlEl := Class("").Tag("html").Mods("taut-streamer-mode")
	blur := func(entity *ClassDef, st css.State, under *ClassDef) float32 {
		t.Helper()
		h := htmlEl.Enter(g, 0, 1)
		defer h.Leave()
		if under != nil {
			u := under.Enter(g, 0, 0)
			defer u.Leave()
		}
		e := entity.Enter(g, st, 0)
		defer e.Leave()
		s := e.Child(nameDef, 0).Style()
		if s == nil {
			t.Fatal("no style with CSS installed")
		}
		return s.Filter.Blur
	}
	if b := blur(private, 0, nil); b != 4 {
		t.Errorf("private channel name: blur %v, want 4 (the lock icon is a declared kid)", b)
	}
	if b := blur(public, 0, nil); b != 0 {
		t.Errorf("public channel name blurred (%v)", b)
	}
	// the plugin's :hover reveal (0,4,1) loses to its blur rule (0,5,1:
	// :has() and :not() each weigh their argument's class), so a hovered
	// private name stays blurred, as in a browser
	if b := blur(private, css.StateHover, nil); b != 4 {
		t.Errorf("hovered private name: blur %v, want 4 (the more specific blur rule wins)", b)
	}
	// its reveal rule for a hovered thread row (.taut-streamer-mode__revealed,
	// 0,6,1) does win
	revealed := Class("c-virtual_list__item taut-streamer-mode__revealed")
	if b := blur(private, 0, revealed); b != 0 {
		t.Errorf("private name in a revealed thread row: blur %v, want 0", b)
	}
	if b := blur(private, 0, actRow); b != 0 {
		t.Errorf("inside an activity row: blur %v, want the :not() exclusion", b)
	}
	// a native without declared kids: :has() false, :empty not matched
	p := plain.Enter(g, 0, 0)
	if c := p.Style().Color; c == (color.NRGBA{R: 0xff, A: 0xff}) || c == (color.NRGBA{B: 0xff, A: 0xff}) {
		t.Errorf("a native without declared kids matched :empty or :has(): %v", c)
	}
	p.Leave()
}
