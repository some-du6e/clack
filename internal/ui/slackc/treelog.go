package slackc

import (
	"fmt"
	"image/color"
	"log"
	"os"
	"slices"
	"sort"
	"strings"

	"github.com/some-du6e/clack/internal/ui/slackc/css"
)

// treeLog names the components whose delivered host trees are logged
// (CLACK_TAUT_TREELOG=Reaction,UnstyledButton or "*"): a debugging aid for
// plugin patches. Only the shape is logged (tags, classes, component names,
// prop keys), never text or prop values, so no message contents reach the log.
var treeLog = func() map[string]bool {
	v := os.Getenv("CLACK_TAUT_TREELOG")
	if v == "" {
		return nil
	}
	m := map[string]bool{}
	for _, n := range strings.Split(v, ",") {
		if n = strings.TrimSpace(n); n != "" {
			m[n] = true
		}
	}
	return m
}()

func logTree(name string, root uint64, nodes []*Node) {
	if treeLog == nil || !(treeLog[name] || treeLog["*"]) {
		return
	}
	log.Printf("slackc tree %s #%d: %s", name, root, TreeShape(nodes))
}

// TreeShape is a one-line outline of a host tree without any text or prop
// values: `span.a.b>(@Emoji{emoji,size} #)`.
func TreeShape(nodes []*Node) string {
	var b strings.Builder
	var walk func([]*Node)
	walk = func(ns []*Node) {
		for i, n := range ns {
			if i > 0 {
				b.WriteByte(' ')
			}
			switch n.T {
			case "#":
				b.WriteByte('#')
				continue
			case "@":
				b.WriteString("@" + n.N)
			default:
				b.WriteString(n.T)
			}
			for _, c := range n.Class {
				b.WriteString("." + c)
			}
			if len(n.P) > 0 {
				keys := make([]string, 0, len(n.P))
				for k := range n.P {
					keys = append(keys, k)
				}
				sort.Strings(keys)
				b.WriteString("{" + strings.Join(keys, ",") + "}")
			}
			if len(n.C) > 0 {
				b.WriteString(">(")
				walk(n.C)
				b.WriteByte(')')
			}
		}
	}
	walk(nodes)
	return b.String()
}

// LayoutShape outlines the laid-out host trees of component name, with
// each box's display and border box (layout-parent-relative device px),
// and the root's: `root(flex 412x26)>[div.a(block 26,0 34x26)>(@Tooltip…)]`.
// Like TreeShape it carries no text or prop values. A debugging aid for
// tests that can only read pixels (UI goroutine).
func LayoutShape(name string) string {
	var out []string
	for _, in := range insts {
		if in.name != name || in.view == nil {
			continue
		}
		var b strings.Builder
		r := &in.view.main.root
		fmt.Fprintf(&b, "root(%s %.0fx%.0f)>[", displayName(r.st.Display), r.w, r.h)
		var walk func(list []*box)
		walk = func(list []*box) {
			for i, k := range list {
				if i > 0 {
					b.WriteByte(' ')
				}
				switch k.kind {
				case kText:
					b.WriteByte('#')
					continue
				case kComp:
					b.WriteString("@")
					if k.n != nil {
						b.WriteString(k.n.N)
					}
				default:
					if k.n != nil {
						b.WriteString(k.n.T)
						for _, c := range k.n.Class {
							b.WriteString("." + c)
						}
					}
				}
				fmt.Fprintf(&b, "(%s %.0f,%.0f %.0fx%.0f)", displayName(k.st.Display), k.x, k.y, k.w, k.h)
				if len(k.kids) > 0 {
					b.WriteString(">[")
					walk(k.kids)
					b.WriteByte(']')
				}
			}
		}
		walk(in.view.top)
		b.WriteByte(']')
		out = append(out, b.String())
	}
	sort.Strings(out)
	return strings.Join(out, " | ")
}

func displayName(d css.Display) string {
	switch d {
	case css.DisplayNone:
		return "none"
	case css.DisplayBlock:
		return "block"
	case css.DisplayInline:
		return "inline"
	case css.DisplayInlineBlock:
		return "inline-block"
	case css.DisplayFlex:
		return "flex"
	case css.DisplayInlineFlex:
		return "inline-flex"
	case css.DisplayContents:
		return "contents"
	}
	return fmt.Sprint(int(d))
}

// ClassStyle is what the cascade gave a laid-out box with some class: its
// own colour, background and text-decoration, the decorations its inline
// ancestors propagate to its text, and the opacity its ancestors and it
// multiply to. Like LayoutShape it carries no text. A debugging aid for
// tests that check a plugin's CSS against Chromium (UI goroutine).
type ClassStyle struct {
	Component  string
	Color      color.NRGBA
	Background color.NRGBA
	Decoration css.TextDecoration // the box's own
	Propagated css.TextDecoration // its own and its ancestors'
	Opacity    float32            // the effective (group) opacity
	W, H       float32
}

// ClassStyles lists every laid-out box of any patched component whose
// class list has class.
func ClassStyles(class string) []ClassStyle {
	var out []ClassStyle
	var walk func(name string, list []*box)
	walk = func(name string, list []*box) {
		for _, k := range list {
			if k.kind == kText {
				continue
			}
			if k.n != nil && slices.Contains(k.n.Class, class) {
				cs := ClassStyle{Component: name, Color: k.st.Color, Background: k.st.Background.Color,
					Decoration: k.st.TextDecoration, Opacity: 1, W: k.w, H: k.h}
				for p := k; p != nil; p = p.parent {
					cs.Propagated |= p.st.TextDecoration
					cs.Opacity *= p.st.Opacity
				}
				out = append(out, cs)
			}
			walk(name, k.kids)
		}
	}
	for _, in := range insts {
		if in.view == nil {
			continue
		}
		walk(in.name, in.view.top)
		for _, x := range in.view.extra {
			walk(in.name, x.boxes)
		}
	}
	return out
}
