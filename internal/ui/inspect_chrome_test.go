package ui

// The inspect tool reaches the chrome Clack draws without a Slack
// component around it (slackc ClassDef.Inspect), the way a browser's
// inspector reaches any element: the channel header and its title and
// name, the tab bar, a tab and its title, the tab rail's Activity button
// with its icon and label, and the Activity view's tabs. ↑ walks out
// through them to the component they're in, and a pick reports the
// element, the elements around it and that component, never the
// channel's name.

import (
	"image"
	"strings"
	"testing"
	"time"

	"gioui.org/f32"
	"gioui.org/io/key"
)

// hoverLabel points the session at p and returns what it outlines.
func (e *inspectEnv) hoverLabel(t *testing.T, what string, p image.Point) (string, image.Rectangle) {
	t.Helper()
	e.move(f32.Pt(float32(p.X), float32(p.Y)))
	e.frame()
	label, r, ok := e.a.insp.Hovered()
	if !ok {
		t.Errorf("%s at %v: nothing outlined", what, p)
	}
	return label, r
}

func TestInspectPicksChrome(t *testing.T) {
	shots, _ := fakeShots(t)
	e := newInspectEnv(t)
	e.a.railRects = map[string]image.Rectangle{}
	e.frame()
	o := e.a.primaryOrigin
	// where they are, found as a user finds them (hover states)
	title, ok := e.scanHover(e.a.clicks.get("hdr.title"), image.Pt(o.X+20, o.Y+24), image.Pt(o.X+400, o.Y+24))
	if !ok {
		t.Fatal("no channel title in the header")
	}
	msgs, ok := e.scanHover(e.a.clicks.get("tab."+e.ch+".messages"), image.Pt(o.X+40, o.Y), image.Pt(o.X+40, o.Y+200))
	if !ok {
		t.Fatal("no Messages tab under the header")
	}
	act, ok := e.a.railRects["activity"]
	if !ok {
		t.Fatalf("no Activity button in the rail: %v", e.a.railRects)
	}
	e.move(f32.Pt(float32(e.size.X/2), float32(e.size.Y/2)))

	e.hyperD()
	e.frame()
	if !e.a.insp.Active() {
		t.Fatal("the shortcut started no inspect session")
	}
	name := title.Add(image.Pt(30, 0)) // the button's 4px padding and the 18px icon (+2) come first
	for _, c := range []struct {
		what string
		at   image.Point
		want []string // in the label
	}{
		{"the channel's name", name, []string{"span.p-view_header__channel_title"}},
		{"the title button", title, []string{"button.c-button-unstyled.p-view_header__big_button"}},
		{"the header left of the star", image.Pt(o.X+12, o.Y+24), []string{"div.p-view_header.p-view_header--tiles"}},
		{"the Messages tab", image.Pt(o.X+16+2, msgs.Y), []string{"button.c-button-unstyled.c-tabs__tab", "[role=tab]"}},
		{"the Messages tab's title", image.Pt(o.X+16+28+6, msgs.Y), []string{"span.c-tabs__tab_content"}},
		{"the tab bar left of the tabs", image.Pt(o.X+10, msgs.Y), []string{"div.c-tabs__tab_menu[role=tablist]"}},
		{"the Activity button", act.Min.Add(image.Pt(2, 2)), []string{"button.c-button-unstyled.c-tabs__tab.p-tab_rail__button", "[data-qa=tab_rail_activity_button]"}},
		{"the Activity button's icon", act.Min.Add(image.Pt(26, 26)), []string{"div.p-tab_rail__button__icon"}},
		{"the Activity button's label", image.Pt(act.Min.X+26, act.Min.Y+53), []string{"div.p-tab_rail__button__label"}},
	} {
		label, r := e.hoverLabel(t, c.what, c.at)
		for _, w := range c.want {
			if !strings.Contains(label, w) {
				t.Errorf("%s at %v: outlined %q %v, want %q", c.what, c.at, label, r, w)
			}
		}
		if !c.at.In(r.Inset(-1)) {
			t.Errorf("%s at %v: the outline %v isn't around the pointer", c.what, c.at, r)
		}
	}

	// ↑ from the name: the title button, the header, then the component
	// they're in (ClientPrimaryView draws the view through it)
	e.hoverLabel(t, "the channel's name", name)
	for _, want := range []string{"button.c-button-unstyled.p-view_header__big_button", "div.p-view_header", "RenderedViewContents"} {
		e.key(key.NameUpArrow, 0)
		e.frame()
		if label, _, _ := e.a.insp.Hovered(); !strings.HasPrefix(label, want) {
			t.Errorf("↑ from the name: outline %q, want %s…", label, want)
		}
	}
	e.key(key.NameDownArrow, 0)
	e.key(key.NameDownArrow, 0)
	e.key(key.NameDownArrow, 0)
	e.frame()
	if label, _, _ := e.a.insp.Hovered(); !strings.HasPrefix(label, "span.p-view_header__channel_title") {
		t.Errorf("↓ back: outline %q, want the name", label)
	}

	// the pick: the element, the elements around it, the component
	e.pickAndCopy(t, name)
	if *shots != 1 {
		t.Fatalf("%d shots after the pick", *shots)
	}
	txt, _ := e.readReport(t)
	rows := reportRows(txt)
	for _, w := range []struct{ key, want string }{
		{"component", "RenderedViewContents"},
		{"within", "*ClientPrimaryView"},
		{"element", "span.p-view_header__channel_title"},
		{"classes", "button.c-button-unstyled.p-view_header__big_button.p-view_header__big_button--channel < div.p-view_header.p-view_header--tiles*"},
		{"region", "primary"},
	} {
		if got := rows[w.key]; !globMatch(w.want, got) {
			t.Errorf("report row %s = %q, want %q\n%s", w.key, got, w.want, txt)
		}
	}
	if c := e.a.model().conv(e.ch); c != nil && len(c.Name) > 2 && strings.Contains(txt, c.Name) {
		t.Errorf("the report holds the channel's name %q:\n%s", c.Name, txt)
	}

	// the Activity view's tabs
	e.a.Navigate("activity")
	e.settle(500 * time.Millisecond)
	e.hyperD()
	e.frame()
	if !e.a.insp.Active() {
		t.Fatal("no second session")
	}
	o = e.a.primaryOrigin
	var tab, tabTitle bool
	var seen []string
	for y := o.Y + 40; y < o.Y+120 && !(tab && tabTitle); y += 6 {
		for x := o.X + 8; x < o.X+200 && !(tab && tabTitle); x += 8 {
			e.move(f32.Pt(float32(x), float32(y)))
			label, _, _ := e.a.insp.Hovered()
			tab = tab || strings.HasPrefix(label, "button.c-button-unstyled.c-tabs__tab")
			tabTitle = tabTitle || strings.HasPrefix(label, "span.c-tabs__tab_content")
			if len(seen) == 0 || seen[len(seen)-1] != label {
				seen = append(seen, label)
			}
		}
	}
	if !tab || !tabTitle {
		t.Errorf("the Activity view's tabs: a tab %v, a tab's title %v; outlined %q", tab, tabTitle, seen)
	}

	// the filter bar under them (Karim, α.85: the Unreads filter button
	// wasn't pickable): Slack's component names, the bar around them
	var unreads, bulk bool
	seen = seen[:0]
	for y := o.Y + 60; y < o.Y+280 && !(unreads && bulk); y += 6 {
		for x := o.X + 18; x < o.X+260 && !(unreads && bulk); x += 10 {
			e.move(f32.Pt(float32(x), float32(y)))
			label, _, _ := e.a.insp.Hovered()
			unreads = unreads || strings.HasPrefix(label, "ActivityFilterUnreadsButton")
			bulk = bulk || strings.HasPrefix(label, "ActivityFilterBulkAction")
			if len(seen) == 0 || seen[len(seen)-1] != label {
				seen = append(seen, label)
			}
			if strings.HasPrefix(label, "ActivityFilterUnreadsButton") {
				e.key(key.NameUpArrow, 0)
				e.frame()
				if up, _, _ := e.a.insp.Hovered(); !strings.HasPrefix(up, "ActivityFilterBar") {
					t.Errorf("↑ from the Unreads button: outline %q, want ActivityFilterBar", up)
				}
				e.key(key.NameDownArrow, 0)
				e.frame()
			}
		}
	}
	if !unreads || !bulk {
		t.Errorf("the Activity filter bar: the Unreads button %v, the select group %v; outlined %q", unreads, bulk, seen)
	}
}
