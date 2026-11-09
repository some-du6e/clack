package kit

import (
	"image"
	"testing"

	"gioui.org/layout"

	"github.com/some-du6e/yoloslackgoclient/internal/ui/icons"
	"github.com/some-du6e/yoloslackgoclient/internal/ui/popups"
	"github.com/some-du6e/yoloslackgoclient/internal/ui/slackc"
)

func TestMenuItems(t *testing.T) {
	var fired []any
	fire := func(_ layout.Context, fn any) { fired = append(fired, fn) }
	tpl := []any{
		slackc.Props{"key": "sep0", "type": "separator"}, // leading: dropped
		slackc.Props{"key": "h", "type": "header", "label": "Options"},
		slackc.Props{"key": "a", "label": "Copy", "icon": "copy", "click": slackc.JSFunc{ID: 7}},
		map[string]any{"key": "b", "label": "Delete", "danger": true, "icon": "trash-filled"},
		slackc.Props{"key": "s1", "type": "separator"},
		slackc.Props{"key": "s2", "type": "separator"}, // doubled: dropped
		slackc.Props{"key": "r", "type": "radio", "label": "Compact", "checked": true},
		slackc.Props{"key": "c", "type": "checkbox", "label": "Wide", "checked": false, "disabled": true},
		slackc.Props{"key": "sub", "type": "submenu", "label": "More", "template": []any{
			slackc.Props{"key": "x", "label": "Inner", "click": slackc.JSFunc{ID: 9}},
		}},
		slackc.Props{"key": "d", "label": slackc.Element{Nodes: []*slackc.Node{{T: "#", V: "Rich "}, {T: "b", C: []*slackc.Node{{T: "#", V: "label"}}}}},
			"description": "Shown under the label"},
		slackc.Props{"key": "end", "type": "separator"}, // trailing: dropped
		"junk",
	}
	items := menuItems(tpl, fire)
	if len(items) != 8 {
		t.Fatalf("got %d items: %+v", len(items), items)
	}
	if items[0].Header != "Options" {
		t.Errorf("header = %q", items[0].Header)
	}
	if items[1].Label != "Copy" || items[1].Icon != "copy" || items[1].DoCtx == nil {
		t.Errorf("copy item = %+v", items[1])
	}
	if !items[2].Danger || items[2].Icon != "trash-filled" || items[2].DoCtx != nil {
		t.Errorf("delete item = %+v", items[2])
	}
	if !items[3].Separator {
		t.Errorf("want one separator, got %+v", items[3])
	}
	if !items[4].Checked || items[4].Label != "Compact" {
		t.Errorf("radio = %+v", items[4])
	}
	if items[5].Checked || !items[5].Disabled {
		t.Errorf("checkbox = %+v", items[5])
	}
	if len(items[6].Sub) != 1 || items[6].Sub[0].Label != "Inner" {
		t.Errorf("submenu = %+v", items[6])
	}
	if items[7].Label != "Rich label" || items[7].Widget == nil {
		t.Errorf("described = %+v", items[7])
	}
	items[1].DoCtx(layout.Context{})
	items[6].Sub[0].DoCtx(layout.Context{})
	if len(fired) != 2 || fired[0] != (slackc.JSFunc{ID: 7}) || fired[1] != (slackc.JSFunc{ID: 9}) {
		t.Errorf("fired = %v", fired)
	}

	// the description row keeps the element's text as its label.
	d := menuItems([]any{tpl[9]}, fire)
	if len(d) != 1 || d[0].Label != "Rich label" || d[0].Widget == nil {
		t.Errorf("described = %+v", d)
	}
}

func TestMenuItemsEmpty(t *testing.T) {
	if got := menuItems(nil, nil); len(got) != 0 {
		t.Errorf("nil template: %v", got)
	}
	only := []any{slackc.Props{"type": "separator"}, slackc.Props{"type": "header"}}
	if got := menuItems(only, nil); len(got) != 0 {
		t.Errorf("separators only: %v", got)
	}
}

func TestAsList(t *testing.T) {
	if n := len(asList([]slackc.Props{{}, {}})); n != 2 {
		t.Errorf("[]Props: %d", n)
	}
	if asList("x") != nil {
		t.Error("string should not be a list")
	}
}

func TestMenuWidth(t *testing.T) {
	for in, want := range map[any]float32{"240px": 240, float64(300): 300, "50%": 0, "auto": 0, nil: 0} {
		if got := menuWidth(in); got != want {
			t.Errorf("menuWidth(%v) = %v, want %v", in, got, want)
		}
	}
}

func TestClackIcon(t *testing.T) {
	cases := map[string]string{
		"close":                    "close",
		"info":                     "info",
		"warning":                  "warning",
		"emoji":                    "emoji",
		"ellipsis-vertical-filled": "kebab",
		"lock":                     "lock",
		"hash":                     "hash",
		"edit":                     "edit",
		"trash":                    "trash",
		"trash-filled":             "trash-filled",
		"copy":                     "copy",
		"link":                     "link",
		"check":                    "check",
		"check-filled":             "check-filled",
		"chevron-down":             "chevron-down",
		"search":                   "search",
		"plus":                     "plus",
		"gear":                     "settings",
		"settings":                 "settings",
		"bell":                     "bell",
		"bookmark":                 "bookmark",
		"thread":                   "threads",
		"send":                     "send",
		"undo":                     "undo",
		"arrow-left":               "back",
		"caret-right":              "caret-right",
		"caret-up-down":            "caret-up-down",
		"user-add":                 "user-add", // Slack's own glyph now (icons)
		"file-upload":              "file-upload",
		"glasses-filled":           "glasses-filled",
		"Close":                    "close",
		"":                         fallbackIcon,
		"no-such-icon-anywhere":    fallbackIcon,
	}
	for in, want := range cases {
		if got := clackIcon(in); got != want {
			t.Errorf("clackIcon(%q) = %q, want %q", in, got, want)
		}
	}
	// every alias must point at a real icon.
	for k, v := range slackIcons {
		if !icons.Has(v) {
			t.Errorf("alias %q → %q: no such Clack icon", k, v)
		}
	}
	if !icons.Has(fallbackIcon) {
		t.Errorf("fallback %q missing", fallbackIcon)
	}
}

func TestPosition(t *testing.T) {
	type want struct {
		side  popups.Side
		align int
	}
	cases := map[string]want{
		"top":          {popups.Above, 1},
		"bottom":       {popups.Below, 1},
		"bottom-left":  {popups.Below, 0},
		"bottom-right": {popups.Below, 2},
		"top-right":    {popups.Above, 2},
		"left":         {popups.Left, 1},
		"right":        {popups.Right, 1},
		"":             {popups.Below, 0},
	}
	for in, w := range cases {
		s, a := position(in, popups.Below)
		if s != w.side || a != w.align {
			t.Errorf("position(%q) = %v,%d want %v,%d", in, s, a, w.side, w.align)
		}
	}
}

func TestPlace(t *testing.T) {
	win := image.Pt(1000, 800)
	a := image.Rect(100, 100, 200, 130)
	if p := place(a, image.Pt(50, 40), win, 8, popups.Below, 0); p != image.Pt(100, 130) {
		t.Errorf("below = %v", p)
	}
	if p := place(a, image.Pt(50, 40), win, 8, popups.Below, 2); p != image.Pt(150, 130) {
		t.Errorf("below-right = %v", p)
	}
	if p := place(a, image.Pt(50, 40), win, 8, popups.Above, 1); p != image.Pt(125, 60) {
		t.Errorf("above-center = %v", p)
	}
	// flips above when there is no room below.
	low := image.Rect(100, 760, 200, 790)
	if p := place(low, image.Pt(50, 40), win, 8, popups.Below, 0); p.Y != 720 {
		t.Errorf("flip = %v", p)
	}
	// unknown window: no flipping, only the margin clamp.
	if p := place(image.Rect(0, 0, 10, 10), image.Pt(50, 40), image.Point{}, 8, popups.Below, 0); p != image.Pt(8, 10) {
		t.Errorf("no window = %v", p)
	}
}

func TestParseLen(t *testing.T) {
	type res struct {
		v      float32
		pct, o bool
	}
	cases := map[any]res{
		float64(12): {12, false, true},
		"20px":      {20, false, true},
		"50%":       {50, true, true},
		"1.5rem":    {24, false, true},
		"auto":      {0, false, false},
		nil:         {0, false, false},
	}
	for in, w := range cases {
		v, pct, ok := parseLen(in)
		if v != w.v || pct != w.pct || ok != w.o {
			t.Errorf("parseLen(%v) = %v,%v,%v", in, v, pct, ok)
		}
	}
}

func TestNodesText(t *testing.T) {
	ns := []*slackc.Node{
		{T: "#", V: "a "},
		{T: "@", N: "MrkdwnElement", P: slackc.Props{"text": "*b*"}},
		{T: "br"},
		{T: "span", C: []*slackc.Node{{T: "#", V: "c"}}},
	}
	if got := nodesText(ns); got != "a *b*\nc" {
		t.Errorf("nodesText = %q", got)
	}
	if got := valueText(float64(3)); got != "3" {
		t.Errorf("valueText(3) = %q", got)
	}
}

func TestDomRect(t *testing.T) {
	gtx := layout.Context{}
	gtx.Metric.PxPerDp = 2
	r, ok := domRect(gtx, slackc.Props{"x": 10.0, "y": 20.0, "width": 30.0, "height": 5.0})
	if !ok || r != image.Rect(20, 40, 80, 50) {
		t.Errorf("domRect = %v %v", r, ok)
	}
	r, ok = domRect(gtx, slackc.Props{"left": 1.0, "top": 2.0, "right": 4.0, "bottom": 6.0})
	if !ok || r != image.Rect(2, 4, 8, 12) {
		t.Errorf("domRect ltrb = %v %v", r, ok)
	}
	if _, ok := domRect(gtx, nil); ok {
		t.Error("nil rect")
	}
}

func TestRegisterNames(t *testing.T) {
	want := []string{
		"ConnectedBaseAvatar", "Avatar", "ProfileHoverTrigger", "MrkdwnElement", "Button", "Tooltip",
		"IconButtonBase", "ConfirmationModal", "InlineAlert", "Label", "FormTextInput", "Checkbox",
		"FilterPill", "Tag", "DateRangePicker", "DatePickerTrigger", "FieldSet", "Legend", "Hint",
		"BasicSelect", "Blocks", "MenuTrigger", "MenuFromTemplate",
		"withKeyboardFocusContext(ContextMenuTrigger)", "ContextMenuTrigger", "TimePicker",
		"Connect(EmojiMenuTrigger)", "EmojiMenuTrigger", "Popover", "SvgIcon", "RichTextInput",
	}
	have := map[string]bool{}
	for _, n := range Names() {
		if have[n] {
			t.Errorf("%q registered twice", n)
		}
		have[n] = true
	}
	for _, n := range want {
		if !have[n] {
			t.Errorf("%q not registered", n)
		}
	}
	Register()
	Register() // idempotent
	for _, n := range want {
		if _, ok := slackc.LookupHost(n); !ok {
			t.Errorf("LookupHost(%q) failed after Register", n)
		}
	}
}
