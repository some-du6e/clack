package richtext

import "strings"

// cardOf describes a Doc's cards (card.go) as "hero=…|alt=…|icon=…|
// slack=…|title=…|subtitle=…|body=…|subtext=…|left=[…]|right=[…]" (the
// actions as action_id/style). Tests reach the card only through this
// file, so the red-evidence commit can stub it.
func cardOf(d Doc) []string {
	ids := func(bs []Button) string {
		var s []string
		for _, b := range bs {
			s = append(s, b.ActionID+"/"+b.Style)
		}
		return "[" + strings.Join(s, " ") + "]"
	}
	var out []string
	for _, b := range d.Blocks {
		if b.Kind != CardBlock || b.Card == nil {
			continue
		}
		c := b.Card
		out = append(out, "hero="+c.Hero+"|alt="+c.HeroAlt+"|icon="+c.Icon+"|slack="+c.SlackIcon+"|title="+SpansText(c.Title)+
			"|subtitle="+SpansText(c.Subtitle)+"|body="+PlainText(Doc{Blocks: c.Body})+"|subtext="+SpansText(c.Subtext)+
			"|left="+ids(c.Left)+"|right="+ids(c.Right))
	}
	return out
}
