package richtext

import "strconv"

// richOf describes a Doc's rich_text headers and dividers through the
// API the coverage round added ("header <level>[ first]: <text>",
// "divider"; other blocks by kind). Tests reach them only through this
// file, so the red-evidence commit can stub it.
func richOf(d Doc) []string {
	var out []string
	for _, b := range d.Blocks {
		switch b.Kind {
		case RichHeader:
			s := "header " + strconv.Itoa(b.Level)
			if b.First {
				s += " first"
			}
			out = append(out, s+": "+SpansText(b.Spans))
		case RichDivider:
			out = append(out, "divider")
		default:
			out = append(out, b.kindName())
		}
	}
	return out
}
