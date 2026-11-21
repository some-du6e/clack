package slackc

import (
	"image/color"
	"testing"

	"github.com/some-du6e/clack/internal/ui/slackc/css"
)

// A plugin's own reaction pill (a button.c-reaction it renders itself in
// a reaction bar, beside Slack's) is drawn with Slack's whole .c-reaction
// rule (client-boot-styles cf726c4f2eaecc74), as in Slack's DOM: a 24px
// round box with 8px padding, Slack's fill and text colour and 4px
// margins, its items centred 4px apart; your pill (--reacted) Slack's
// tint, bold text and its 1px ring. The base sheet's .c-reaction is the
// reduced part the native pill's children lay out under (the native draws
// its box); alone it gave a plugin's pill a flex row and 12px text and no
// box. No plugin CSS is installed: a Slack class on a plugin's element
// looks like Slack's by itself.
func TestPluginOwnReactionLikeSlack(t *testing.T) {
	v, _ := layoutJS(t, `{"t":"div","k":1,"p":{"className":"c-message_kit__reaction_bar c-reaction_bar"},"c":[`+
		`{"t":"button","k":2,"p":{"id":"own","className":"c-button-unstyled c-reaction"},"c":[{"t":"span","k":3,"p":{"id":"a"},"c":["7"]},{"t":"span","k":4,"p":{"id":"b"},"c":["x"]}]},`+
		`{"t":"button","k":5,"p":{"id":"mine","className":"c-button-unstyled c-reaction c-reaction--reacted"},"c":["8"]}]}`, 600, 200)
	hex := func(s string) color.NRGBA {
		c, ok := css.ParseColor(s)
		if !ok {
			t.Fatalf("colour %q", s)
		}
		return c
	}
	own := byID(v, "own")
	if own == nil {
		t.Fatal("no box for the plugin's pill")
	}
	st := &own.st
	if st.Display != css.DisplayInlineFlex {
		t.Errorf("display %v, want inline-flex", st.Display)
	}
	if !near(own.h, 24) {
		t.Errorf("height %.1f, want Slack's 24px", own.h)
	}
	if st.Padding[1] != css.Px(8) || st.Padding[3] != css.Px(8) {
		t.Errorf("padding-inline %v / %v, want 8px", st.Padding[3], st.Padding[1])
	}
	if st.Margin[1] != css.Px(4) || st.Margin[2] != css.Px(4) {
		t.Errorf("margin-right %v, margin-bottom %v, want 4px", st.Margin[1], st.Margin[2])
	}
	if st.Radius[0].Px < 12 {
		t.Errorf("border-radius %v, want Slack's 9999px (round ends)", st.Radius[0])
	}
	if want := hex("#1d1c1d0f"); st.Background.Color != want {
		t.Errorf("background %v, want var(--dt_color-surf-pry) %v", st.Background.Color, want)
	}
	if want := hex("#1d1c1d"); st.Color != want {
		t.Errorf("colour %v, want var(--dt_color-content-pry) %v", st.Color, want)
	}
	if st.FontSize != 12 {
		t.Errorf("font-size %v, want 12px", st.FontSize)
	}
	// its items: centred in the 24px box, 4px apart after the 8px padding
	a, b := byID(v, "a"), byID(v, "b")
	if a == nil || b == nil {
		t.Fatal("no boxes for the pill's items")
	}
	absX := func(b *box) float32 {
		x := b.x + b.relX
		for p := b.lp; p != nil; p = p.lp {
			x += p.x + p.relX
		}
		return x
	}
	ax, bx := absX(a)-absX(own), absX(b)-absX(own)
	if !near(ax, 8) || !near(bx, ax+a.w+4) {
		t.Errorf("items at x %.1f and %.1f in the pill (first %.1f wide), want 8 and 4px after it", ax, bx, a.w)
	}
	if ay := absY(a) - absY(own); !near(ay, (24-a.h)/2) {
		t.Errorf("item at y %.1f in the pill (%.1f tall), want centred in its 24px", ay, a.h)
	}

	mine := byID(v, "mine")
	if mine == nil {
		t.Fatal("no box for the plugin's reacted pill")
	}
	ms := &mine.st
	if want := hex("#e3f8ff"); ms.Background.Color != want {
		t.Errorf("reacted background %v, want var(--dt_color-base-hgl-1) %v", ms.Background.Color, want)
	}
	if want := hex("#1264a3"); ms.Color != want {
		t.Errorf("reacted colour %v, want var(--dt_color-content-hgl-1) %v", ms.Color, want)
	}
	if ms.FontWeight != 700 {
		t.Errorf("reacted font-weight %d, want 700", ms.FontWeight)
	}
	if want := hex("#1264a3"); len(ms.BoxShadow) != 1 || ms.BoxShadow[0].Spread != 1 || ms.BoxShadow[0].Color != want {
		t.Errorf("reacted box-shadow %+v, want 0 0 0 1px var(--dt_color-base-inv-hgl-1) %v", ms.BoxShadow, want)
	}
}
