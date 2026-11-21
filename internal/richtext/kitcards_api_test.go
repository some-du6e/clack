package richtext

import "strings"

// boxedOf describes a Doc's boxed blocks (kitcards.go), through the API
// the coverage round added: "alert <level>: <text>", "callout <colour>
// [<child kinds>]", "loading", "container <width> title=… [<child kinds>]". Tests reach those blocks only through
// this file, so the red-evidence commit can stub it.
func boxedOf(d Doc) []string {
	var out []string
	for _, b := range d.Blocks {
		switch b.Kind {
		case AlertBlock:
			out = append(out, "alert "+b.Tone+": "+PlainText(Doc{Blocks: b.Children}))
		case CalloutBlock:
			var ks []string
			for _, c := range b.Children {
				k := c.kindName()
				if c.Kit {
					k += "*"
				}
				ks = append(ks, k)
			}
			out = append(out, "callout "+b.Tone+" ["+strings.Join(ks, " ")+"]")
		case LoadingBlock:
			out = append(out, "loading")
		case ContainerBlock:
			ci := b.Container
			s := "container " + ci.Width + " title=" + SpansText(ci.Title) + " subtitle=" + SpansText(ci.Subtitle) + " icon=" + ci.Icon
			if ci.Collapsible {
				s += " collapsible"
			}
			if ci.DefaultCollapsed {
				s += " collapsed"
			}
			if ci.HeaderDivider {
				s += " divider"
			}
			var ks []string
			for _, c := range b.Children {
				ks = append(ks, c.kindName())
			}
			out = append(out, s+" ["+strings.Join(ks, " ")+"]")
		}
	}
	return out
}
