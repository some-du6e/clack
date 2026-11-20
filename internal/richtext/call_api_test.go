package richtext

// callOf is the call card a Doc draws (call.go). Tests reach the call
// only through this file, so the red-evidence commit can stub it.

// callView is what a call card shows. Faces are "user id|name|avatar".
type callView struct {
	Title, Status, Description, MeetingID string
	Button, JoinURL, Icon                 string
	Primary, Ended, NoFooter              bool
	Faces                                 []string
}

func callOf(d Doc) (callView, bool) {
	for _, b := range d.Blocks {
		if b.Kind == CallBlock && b.Call != nil {
			c := b.Call
			v := callView{Title: c.Title, Status: c.Status, Description: c.Description, MeetingID: c.MeetingID,
				Button: c.Button, JoinURL: c.JoinURL, Icon: c.Icon, Primary: c.Primary, Ended: c.Ended, NoFooter: c.NoFooter}
			for _, f := range c.Faces {
				v.Faces = append(v.Faces, f.UserID+"|"+f.Name+"|"+f.Avatar)
			}
			return v, true
		}
	}
	return callView{}, false
}

// callGlyphOf is the call card's renderCallIcon glyph ("" without a card).
func callGlyphOf(d Doc) string {
	for _, b := range d.Blocks {
		if b.Kind == CallBlock && b.Call != nil {
			return b.Call.Glyph
		}
	}
	return ""
}
