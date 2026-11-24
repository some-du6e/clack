package richview

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"gioui.org/f32"
	"gioui.org/io/pointer"

	rt "github.com/some-du6e/clack/internal/richtext"
	"github.com/some-du6e/clack/internal/slack"
)

// The rest of expand-more's review: the image group's file lookup, the
// video block's letterbox, caret and spacing, and MessageKitAttachments'
// elements.

// countExp is fakeExp counting ImageFile's asks by url.
type countExp struct {
	fakeExp
	n map[string]int
}

func (x *countExp) ImageFile(u string) (*rt.File, bool) {
	x.mu.Lock()
	if x.n == nil {
		x.n = map[string]int{}
	}
	x.n[u]++
	x.mu.Unlock()
	return x.fakeExp.ImageFile(u)
}

func (x *countExp) asked(u string) int {
	x.mu.Lock()
	defer x.mu.Unlock()
	return x.n[u]
}

func (x *countExp) hold(u string, f *rt.File) {
	x.mu.Lock()
	defer x.mu.Unlock()
	x.files[u] = f
}

// The group's file lookup (Expander.ImageFile, a scan of what the store
// holds) runs for the group the pointer is at, not for every group in the
// View, and again once the pointer comes back: a file the store got
// meanwhile gains Download (Slack's useSelector reads the store on each
// render).
func TestImageFileAskedNearItsGroup(t *testing.T) {
	x := &countExp{fakeExp: fakeExp{files: map[string]*rt.File{}}}
	useExpander(t, x)
	const a, b = "https://img.test/a.png", "https://img.test/b.png"
	two := `[{"type":"image","block_id":"a","image_url":"` + a + `","alt_text":"a","image_width":400,"image_height":200},` +
		`{"type":"section","block_id":"s","text":{"type":"mrkdwn","text":"between"}},` +
		`{"type":"image","block_id":"b","image_url":"` + b + `","alt_text":"b","image_width":400,"image_height":200}]`
	hs := &harness{st: newTestStyle(), v: New(kitDoc(t, two), Options{Channel: "C1", TS: "1.0"}), h: &dlHandler{}, w: 700}
	hs.frame()
	ia := imgsWithURL(&hs.v.d, a)[0]
	hs.send(pointer.Event{Kind: pointer.Move, Source: pointer.Mouse, Position: rectCenter(ia.r)})
	if x.asked(a) != 1 || x.asked(b) != 0 {
		t.Fatalf("asked a %d times, b %d: want the hovered group's once, the other's never", x.asked(a), x.asked(b))
	}
	if acts := hs.v.expandActs(hs.st, 0); len(acts) == 0 || acts[0] == imgActDownload {
		t.Fatalf("the group %v: Download for a file the store doesn't hold", acts)
	}
	// away from the View, the store gets the file, back to the image
	hs.send(pointer.Event{Kind: pointer.Move, Source: pointer.Mouse, Position: f32.Pt(5000, 5000)})
	x.hold(a, &rt.File{ID: "F0LATER001", Name: "a.png", Download: "https://files.slack.com/files-pri/T1-F0LATER001/download/a.png"})
	hs.send(pointer.Event{Kind: pointer.Move, Source: pointer.Mouse, Position: rectCenter(ia.r)})
	if acts := hs.v.expandActs(hs.st, 0); len(acts) == 0 || acts[0] != imgActDownload {
		t.Errorf("the group %v once the store holds the file: no Download (asked %d times)", acts, x.asked(a))
	}
	if x.asked(b) != 0 {
		t.Errorf("b's group asked %d times", x.asked(b))
	}
}

// VideoBlock's thumbnail sits on the player's black (.p-video_block__thumb
// {background-color:#000}): no placeholder fill under it, while it loads
// or round a letterboxed frame. Its title leaves the caret's 24dp free, so
// the trigger never lands past the View's edge; and in a message a video
// block keeps no attachment margins (its wrapper is a bare flex box, its
// spacing VideoBlock's own 8px padding).
func TestVideoBlockLetterboxCaretAndSpacing(t *testing.T) {
	st := newTestStyle()
	long := strings.Repeat("Demo reel ", 30)
	blk := strings.Replace(videoBlk, `"text":"Demo"`, `"text":"`+long+`"`, 1)
	o := Options{Channel: "C1", TS: "1700000000.000100"}
	v := layoutAt(st, kitDoc(t, blk), o, 500)
	thumbs := imgsWithURL(&v.d, "https://v.test/t.jpg")
	if len(thumbs) != 1 || !thumbs[0].bare || !thumbs[0].fit {
		t.Fatalf("the thumbnail %+v: want bare (the black behind it) and fit", thumbs)
	}
	for _, f := range v.d.frags {
		if f.kind == fText && strings.Contains(string(v.d.plain[f.p0:f.p1]), "reel") && int(f.x+f.w) > 500-24 {
			t.Errorf("a title frag ends at %.0f, past the caret's 24dp (the View is 500 wide)", f.x+f.w)
		}
	}
	if top, first, bottom := (&lay{st: st, scale: 1}).margins(&rt.Block{Kind: rt.AttachmentBlock, Att: &rt.Attachment{VideoBlock: true}}); top != 0 || first != 0 || bottom != 0 {
		t.Errorf("a video block's margins in a message %d %d %d, want none", top, first, bottom)
	}
}

// MessageKitAttachments' elements: the work objects are one element (and
// one drawn drops the limit to 10), an attachment's blocks drop it even
// when they're an empty list, and each agent channel's card is an element
// of its own.
func TestAttachmentsOverLimitElements(t *testing.T) {
	st := newTestStyle()
	lay := func(m slack.Message) string {
		v := layoutAt(st, rt.FromMessage(codeRes{}, &m), Options{Channel: "C1", TS: m.TS}, 700)
		return string(v.d.plain)
	}
	atts := func(n int) []slack.Attachment {
		var out []slack.Attachment
		for i := 1; i <= n; i++ {
			out = append(out, slack.Attachment{ID: i, Text: fmt.Sprintf("att-%02d", i)})
		}
		return out
	}
	wo := func(id int) slack.Attachment {
		raw, _ := json.Marshal(woEntity(fmt.Sprintf("T-%d", id), fmt.Sprintf("Task %d", id), false, false))
		return slack.Attachment{ID: id, FromURL: "https://tracker.example.com/x", WorkObjectEntity: raw}
	}
	// 22 work objects: one element, all drawn
	m := slack.Message{Type: "message", TS: "1762700000.000400", User: "U0", Text: "tasks"}
	for i := 1; i <= 22; i++ {
		m.Attachments = append(m.Attachments, wo(i))
	}
	if p := lay(m); strings.Contains(p, "more attachment") {
		t.Errorf("22 work objects (one element): %q", p)
	}
	// 12 attachments and a work object: the limit is 10, 3 elements past it
	m = slack.Message{Type: "message", TS: "1762700000.000500", User: "U0", Text: "mixed", Attachments: append(atts(12), wo(13))}
	if p := lay(m); !strings.Contains(p, "att-10") || strings.Contains(p, "att-11") || !strings.Contains(p, "+ 3 more attachments") {
		t.Errorf("12 attachments and a work object: %q", p)
	}
	// an attachment whose blocks are an empty list: the limit is 10
	m = slack.Message{Type: "message", TS: "1762700000.000600", User: "U0", Text: "blocks", Attachments: atts(12)}
	m.Attachments[0].Blocks = json.RawMessage(`[]`)
	if p := lay(m); !strings.Contains(p, "+ 2 more attachments") {
		t.Errorf("an attachment with empty blocks among 12: %q", p)
	}
	// 20 attachments and an agent channel's card: 21 elements
	m = slack.Message{Type: "message", TS: "1762700000.000700", User: "U0", Text: "agents", Attachments: atts(20)}
	m.Attachments = append(m.Attachments, slack.Attachment{ID: 21, Fallback: "agent channel",
		AgentChannelUnfurl: &slack.AgentChannelUnfurl{ChannelID: "C0AGENT001", ChannelName: "agent-room"}})
	if p := lay(m); !strings.Contains(p, "+ 1 more attachment") {
		t.Errorf("20 attachments and an agent channel's card: %q", p)
	}
}
