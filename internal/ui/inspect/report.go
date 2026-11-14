package inspect

import (
	"fmt"
	"image"
	"image/color"
	"strings"
	"time"
)

// Report is what one pick says about the element, every field already
// redacted: names of Clack's and Slack's code, classes, sizes, colours and
// tags, nothing anyone wrote.
type Report struct {
	When    time.Time
	Version string // Clack's version ("dev" for a dev build)
	OS      string // goos/goarch

	// The window: its size in dp, px per dp (zoom included), View > Zoom's
	// level, the colour mode, the account theme and the UI font.
	Window image.Point
	Scale  float32
	Zoom   int
	Dark   bool
	Theme  string
	Font   string

	// Page is the redacted route; Region the shell area the pointer was in.
	Page   string
	Region string

	// Component is the innermost Slack component under the pointer;
	// Within, the components around it, innermost first. Element is its
	// root element as a selector, Classes the elements around the
	// component, innermost first.
	Component string
	Patched   bool
	Within    []string
	Element   string
	Classes   []string
	// Tree is a plugin host tree's boxes under the pointer, innermost
	// first, with the innermost one's computed style in Style.
	Tree  []string
	Style []string

	// Rect is the outlined element in dp, window coordinates; Pointer
	// where the click was.
	Rect    image.Rectangle
	Pointer image.Point

	// IDs are the tagged ids the element's components were drawn for
	// ("channelId=ch:3fa2b1"), Kinds what the tagged conversations are
	// ("ch:3fa2b1 private"). Colors are sampled from the screenshot.
	IDs    []string
	Kinds  []string
	Colors []string
	// Exps are the experiments the element's code read while drawing it.
	Exps []string
	// Patches are the components plugins patch; Overlays the popups and
	// dialogs open (their Go types).
	Patches  []string
	Overlays []string
}

// Line is one row of the report: a short key and its value.
type Line struct{ Key, Value string }

// Lines is the report as rows, in the order the panel and the text show
// them. Empty fields are left out.
func (r *Report) Lines() []Line {
	var out []Line
	add := func(k, v string) {
		if v != "" {
			out = append(out, Line{k, v})
		}
	}
	join := func(xs []string, sep string) string { return strings.Join(xs, sep) }
	v := r.Version
	if v == "" {
		v = "dev"
	}
	add("clack", fmt.Sprintf("%s · %s · %s", v, r.OS, r.When.Format("2006-01-02 15:04:05 MST")))
	comp := r.Component
	if comp == "" {
		comp = "(no Slack component here)"
	}
	if r.Patched {
		comp += " (patched by a plugin)"
	}
	add("component", comp)
	add("element", r.Element)
	add("within", join(r.Within, " < "))
	add("classes", join(r.Classes, " < "))
	add("tree", join(r.Tree, " < "))
	add("style", join(r.Style, " · "))
	if !r.Rect.Empty() {
		add("rect", fmt.Sprintf("x %d y %d w %d h %d dp", r.Rect.Min.X, r.Rect.Min.Y, r.Rect.Dx(), r.Rect.Dy()))
	}
	add("pointer", fmt.Sprintf("%d,%d dp", r.Pointer.X, r.Pointer.Y))
	add("region", r.Region)
	add("page", r.Page)
	add("ids", join(r.IDs, " · "))
	add("kinds", join(r.Kinds, " · "))
	add("colors", join(r.Colors, " · "))
	mode := "light"
	if r.Dark {
		mode = "dark"
	}
	win := fmt.Sprintf("%s · %s · %s · %dx%d dp at %gx", mode, r.Theme, r.Font, r.Window.X, r.Window.Y, r.Scale)
	if r.Zoom != 0 {
		win += fmt.Sprintf(" · zoom %+d", r.Zoom)
	}
	add("window", win)
	add("experiments", join(r.Exps, " · "))
	add("overlays", join(r.Overlays, " · "))
	add("patches", join(r.Patches, " · "))
	add("tags", "one-way: sha256 with a salt made for this report and thrown away")
	return out
}

// Text is the report as plain text (the .txt file, and the clipboard when
// an image can't go there).
func (r *Report) Text() string {
	lines := r.Lines()
	w := 0
	for _, l := range lines {
		w = max(w, len(l.Key))
	}
	var b strings.Builder
	b.WriteString("Clack inspect report\n")
	for _, l := range lines {
		fmt.Fprintf(&b, "%-*s  %s\n", w, l.Key, l.Value)
	}
	return b.String()
}

// hex6 is #rrggbb.
func hex6(c color.NRGBA) string { return fmt.Sprintf("#%02x%02x%02x", c.R, c.G, c.B) }

// hexA is #rrggbb, or #rrggbbaa when not opaque.
func hexA(c color.NRGBA) string {
	if c.A == 0xff {
		return hex6(c)
	}
	return fmt.Sprintf("#%02x%02x%02x%02x", c.R, c.G, c.B, c.A)
}
