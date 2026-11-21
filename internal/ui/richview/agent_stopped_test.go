package richview

// StoppedLabel (J825) on an agent reply whose stream the person stopped:
// insertMessageLabelIntoBlocks puts "(stopped)" where "(edited)" goes
// (editedPlace), in c-message__stopped_label's 13px, the same colour; with a
// name it reads "(stopped by {name})"; a message showing "(edited)" shows
// that alone.

import "testing"

func TestAgentStoppedLabel(t *testing.T) {
	st := newTestStyle()
	v := layoutAt(st, doc(para(txt("Here is the start"))), stoppedOpts(""), 400)
	body, lbl := findFrag(t, &v.d, "start"), findFrag(t, &v.d, "(stopped)")
	if lbl.line != body.line || lbl.x <= body.x+body.w {
		t.Errorf("(stopped) not after the text on its line: %+v, text %+v", lbl, body)
	}
	if lbl.col != st.Palette.Edited || lbl.size != 13 {
		t.Errorf("(stopped) colour %v size %d, want the edited label's", lbl.col, lbl.size)
	}
	if string(v.d.plain) != "Here is the start" {
		t.Errorf("plain %q: the label takes no text", v.d.plain)
	}
	v = layoutAt(st, doc(para(txt("Here is the start"))), stoppedOpts("Ana"), 400)
	findFrag(t, &v.d, "(stopped by Ana)")
	v = layoutAt(st, doc(para(txt("Here is the start"))), editedStoppedOpts(), 400)
	findFrag(t, &v.d, "(edited)")
	if hasFrag(&v.d, "(stopped)") {
		t.Errorf("both labels drawn: Slack shows (edited) alone")
	}
}
