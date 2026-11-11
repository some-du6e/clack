package richview

import (
	"image"
	"testing"
	"time"

	rt "github.com/some-du6e/clack/internal/richtext"
)

// joinRec records Join Huddle clicks.
type joinRec struct {
	recHandler
	joined []string
}

func (h *joinRec) JoinHuddle(ch string) { h.joined = append(h.joined, ch) }

func huddleDoc(joinable bool) rt.Doc {
	room := &rt.HuddleRoom{ID: "R1", Channel: "C1", Live: joinable, Joinable: joinable, Active: []string{"U1", "U2"},
		Faces: []string{"https://a.test/u1.png", "https://a.test/u2.png"}}
	return rt.Doc{Blocks: []rt.Block{{Kind: rt.AttachmentBlock, Att: &rt.Attachment{ServiceName: "Huddle", Huddle: room,
		Text: []rt.Block{{Kind: rt.Paragraph, Spans: []rt.Span{{Kind: rt.Text, Text: "alice and bob are already there."}}}}}}}}
}

// A huddle message is HuddleMsgText: the sentence in the automated
// (Text2) color, and under it, while live and you're not in it, the Join
// Huddle button with the faces; a click is HuddleJoiner.JoinHuddle with
// the conversation.
func TestHuddleMessageJoin(t *testing.T) {
	h := &joinRec{}
	hs := optHarness(huddleDoc(true), Options{}, h)
	if !textHas(hs.v, "alice and bob are already there.") {
		t.Fatalf("no sentence: %q", string(hs.v.d.plain))
	}
	f := findFrag(t, &hs.v.d, "alice")
	if f.col != hs.st.Palette.Text2 {
		t.Errorf("sentence color %v, want the automated Text2 %v", f.col, hs.st.Palette.Text2)
	}
	join := findFrag(t, &hs.v.d, "Join")
	var btn image.Rectangle
	for _, b := range hs.v.d.boxes {
		if b.kind == bBorder && b.stroke == hs.st.Palette.HuddleJoinBorder {
			btn = b.r
		}
	}
	if btn.Empty() || btn.Dy() != 36 || btn.Min.Y <= f.y {
		t.Fatalf("Join Huddle button %v (sentence at y %d)", btn, f.y)
	}
	faces := 0
	for _, im := range hs.v.d.imgs {
		if im.r.In(btn) {
			faces++
		}
	}
	if faces != 2 {
		t.Errorf("%d faces in the button, want 2", faces)
	}
	p := center(join)
	hs.send(press(p, time.Second), release(p, time.Second+20*time.Millisecond))
	if len(h.joined) != 1 || h.joined[0] != "C1" {
		t.Fatalf("Join Huddle joined %v", h.joined)
	}
	// a huddle that ended (or one you're in): the sentence only
	ended := optHarness(huddleDoc(false), Options{}, &joinRec{})
	for _, b := range ended.v.d.boxes {
		if b.kind == bBorder && b.stroke == ended.st.Palette.HuddleJoinBorder {
			t.Fatal("a Join Huddle button on a huddle you can't join")
		}
	}
}
