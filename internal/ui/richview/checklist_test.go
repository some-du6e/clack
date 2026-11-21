package richview

import (
	"encoding/json"
	"strings"
	"testing"

	rt "github.com/some-du6e/clack/internal/richtext"
	"github.com/some-du6e/clack/internal/slack"
)

// TestCheckListLayout: in a message a check list draws Slack's
// checkboxes (RichTextList's p-rich_text_list__check_icon: the 16px
// checkbox-filled or square SvgIcon, 8px before the text, the item 4px in
// at indent 0, 28px per indent step deeper); the next lines wrap back
// under the icon. Where the rich_text renderable doesn't allow them it's a
// bullet list.
func TestCheckListLayout(t *testing.T) {
	st := newTestStyle()
	long := strings.Repeat("word ", 40)
	d := kitDoc(t, `[{"type":"rich_text","elements":[
		{"type":"rich_text_list","style":"check","elements":[
			{"type":"rich_text_section","checked":true,"elements":[{"type":"text","text":"Alpha"}]},
			{"type":"rich_text_section","elements":[{"type":"text","text":"Bravo `+long+`"}]}]},
		{"type":"rich_text_list","style":"check","indent":1,"elements":[
			{"type":"rich_text_section","elements":[{"type":"text","text":"Charlie"}]}]}]}]`)
	v := layoutAt(st, d, checkListOpts(), 400)
	filled, squares := imgsWithURL(&v.d, iconURL+"checkbox-filled"), imgsWithURL(&v.d, iconURL+"square")
	if len(filled) != 1 || len(squares) != 2 {
		t.Fatalf("%d filled and %d empty checkboxes, want 1 and 2", len(filled), len(squares))
	}
	a := findFrag(t, &v.d, "Alpha")
	if r := filled[0].r; r.Min.X != 4 || r.Dx() != 16 || r.Min.Y != a.top+(a.bot-a.top-16)/2 {
		t.Errorf("checked icon %v, want 16px at x 4 centred on Alpha's line %d-%d", r, a.top, a.bot)
	}
	if int(a.x) != 4+16+8 {
		t.Errorf("Alpha at x %.0f, want 28", a.x)
	}
	// Bravo's item wraps: its second line starts under the icon
	b := findFrag(t, &v.d, "Bravo")
	second := -1
	for _, f := range v.d.frags {
		if f.top >= b.bot && f.top < b.bot+1 && f.kind == fText && second < 0 {
			second = int(f.x)
		}
	}
	if second != 4 {
		t.Errorf("Bravo's second line starts at %d, want 4 (under the icon)", second)
	}
	if c := findFrag(t, &v.d, "Charlie"); int(c.x) != 56+16+8 || squares[1].r.Min.X != 56 {
		t.Errorf("indent 1: icon at %d, text at %.0f; want 56 and 80", squares[1].r.Min.X, c.x)
	}
	// elsewhere: bullets, no checkboxes
	v = layoutAt(st, d, Options{}, 400)
	if n := len(imgsWithURL(&v.d, iconURL+"square")) + len(imgsWithURL(&v.d, iconURL+"checkbox-filled")); n != 0 {
		t.Errorf("%d checkboxes outside a message, want bullets", n)
	}
	bullets := 0
	for _, bx := range v.d.boxes {
		if bx.kind == bCircle || bx.kind == bRing {
			bullets++
		}
	}
	if bullets != 3 {
		t.Errorf("%d bullets, want 3", bullets)
	}
}

// TestCheckListInAttachments: an attachment's rich text renders in the
// message_attachment context (AttachmentBlocks; a forwarded message's
// renderTruncatedAttachmentBlocks), where RichTextList draws a check
// list as bullets: s3Wf's sS allows checkboxes only in a message, a
// thread and a Slackbot message. The message's own list keeps them.
func TestCheckListInAttachments(t *testing.T) {
	st := newTestStyle()
	list := `{"type":"rich_text","elements":[{"type":"rich_text_list","style":"check","elements":[
		{"type":"rich_text_section","checked":true,"elements":[{"type":"text","text":"Alpha"}]},
		{"type":"rich_text_section","elements":[{"type":"text","text":"Bravo"}]}]}]}`
	count := func(v *View) (boxes, bullets int) {
		boxes = len(imgsWithURL(&v.d, iconURL+"square")) + len(imgsWithURL(&v.d, iconURL+"checkbox-filled"))
		for _, bx := range v.d.boxes {
			if bx.kind == bCircle || bx.kind == bRing {
				bullets++
			}
		}
		return
	}
	for _, c := range []struct{ name, msg string }{
		{"an attachment's blocks", `{"ts":"1.0","text":"x","attachments":[{"id":1,"color":"36a64f","blocks":[` + list + `]}]}`},
		{"a forwarded message", `{"ts":"1.0","text":"x","attachments":[{"id":1,"is_share":true,"author_name":"alice","channel_id":"C1",
			"ts":"1700000000.000100","message_blocks":[{"message":{"blocks":[` + list + `]}}]}]}`},
	} {
		var m slack.Message
		if err := json.Unmarshal([]byte(c.msg), &m); err != nil {
			t.Fatalf("%s: %v", c.name, err)
		}
		v := layoutAt(st, rt.FromMessage(codeRes{}, &m), checkListOpts(), 600)
		if !hasFrag(&v.d, "Alpha") {
			t.Fatalf("%s: the list isn't drawn: %q", c.name, fragStrings(&v.d))
		}
		if boxes, bullets := count(v); boxes != 0 || bullets != 2 {
			t.Errorf("%s: %d checkboxes and %d bullets, want 0 and 2", c.name, boxes, bullets)
		}
	}
	// the message's own list: its checkboxes
	v := layoutAt(st, kitDoc(t, `[`+list+`]`), checkListOpts(), 600)
	if boxes, _ := count(v); boxes != 2 {
		t.Errorf("the message's own check list: %d checkboxes, want 2", boxes)
	}
}
