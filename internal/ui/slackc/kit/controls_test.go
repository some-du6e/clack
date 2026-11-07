package kit

import (
	"testing"

	"github.com/some-du6e/yoloslackgoclient/internal/ui/slackc"
)

func TestCtlAria(t *testing.T) {
	for _, c := range []struct {
		v    any
		want bool
	}{{"true", true}, {true, true}, {"false", false}, {nil, false}, {false, false}, {"mixed", false}} {
		if got := ctlAria(c.v); got != c.want {
			t.Errorf("ctlAria(%v) = %v", c.v, got)
		}
	}
}

func TestCtlDisabled(t *testing.T) {
	if !ctlDisabled(slackc.Props{"disabled": true}) || !ctlDisabled(slackc.Props{"isDisabled": true}) ||
		!ctlDisabled(slackc.Props{"aria-disabled": "true"}) || ctlDisabled(slackc.Props{}) {
		t.Fatal("ctlDisabled")
	}
}

func TestBtnMetrics(t *testing.T) {
	if h, fs, rad, _, _ := btnMetrics(0); h != 28 || fs != 13 || rad != 4 {
		t.Errorf("small: %v %v %v", h, fs, rad)
	}
	if h, _, _, _, _ := btnMetrics(1); h != 36 {
		t.Errorf("medium: %v", h)
	}
	if h, _, _, _, _ := btnMetrics(2); h != 44 {
		t.Errorf("large: %v", h)
	}
}

func TestBtnColors(t *testing.T) {
	cl := &lightColors
	if bg, fg, b := btnColors(cl, "primary", false, false); bg != cl.Primary || fg != cl.PrimaryText || b.A != 0 {
		t.Error("primary")
	}
	if _, _, b := btnColors(cl, "outline", false, false); b != cl.BtnBorder {
		t.Error("outline border")
	}
	if bg, _, _ := btnColors(cl, "ghost", false, false); bg.A != 0 {
		t.Error("ghost is transparent")
	}
	if bg, _, _ := btnColors(cl, "ghost", false, true); bg != cl.GhostHover {
		t.Error("ghost hover")
	}
	if bg, fg, _ := btnColors(cl, "danger", true, true); bg != cl.Disabled || fg != cl.DisText {
		t.Error("disabled wins")
	}
}

func TestTagColors(t *testing.T) {
	cl := &lightColors
	if bg, fg := tagColors(cl, ""); bg != cl.TagBg || fg != cl.TagFg {
		t.Error("default tag")
	}
	if bg, fg := tagColors(cl, "informative"); bg != cl.InfoBg || fg != cl.InfoFg {
		t.Error("informative tag")
	}
	if _, fg := tagColors(cl, "danger"); fg != cl.Error {
		t.Error("danger tag")
	}
}

func TestToggleEvent(t *testing.T) {
	e := toggleEvent(slackc.Props{"checked": true, "id": "x"})
	tg := e["target"].(slackc.Props)
	if e["type"] != "change" || tg["checked"] != false || tg["value"] != "on" || tg["id"] != "x" {
		t.Fatalf("toggleEvent = %v", e)
	}
}

func TestToggleChild(t *testing.T) {
	var got any
	cb := &slackc.Node{T: "@", N: "Checkbox", P: slackc.Props{"checked": false, "onChange": func(e any) { got = e }}}
	lbl := &slackc.Node{T: "@", N: "Label", C: []*slackc.Node{{T: "div", C: []*slackc.Node{cb}}}}
	if !toggleChild(lbl) {
		t.Fatal("checkbox not found")
	}
	e, _ := got.(slackc.Props)
	if e == nil || e["target"].(slackc.Props)["checked"] != true {
		t.Fatalf("onChange got %v", got)
	}
	got = nil
	cb.P["disabled"] = true
	if !toggleChild(lbl) || got != nil {
		t.Fatal("disabled checkbox must not change")
	}
	if toggleChild(&slackc.Node{T: "@", N: "Label"}) || toggleChild(nil) {
		t.Fatal("no checkbox")
	}
}

func TestCounterText(t *testing.T) {
	if counterText(90, 100) != "10" || counterText(105, 100) != "-5" {
		t.Fatal("counterText")
	}
}
