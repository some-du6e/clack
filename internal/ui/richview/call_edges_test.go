package richview

import (
	"image"
	"strconv"
	"strings"
	"testing"
	"time"

	"gioui.org/op"
	"gioui.org/op/paint"
)

// TestCallCardEdges: an external participant without an avatar_url shows
// Slack's ava_0002 (Facepile's renderExternalAvatarFallback, 3px
// corners); a missed DM call with a join_url and its facepile hidden has
// no footer band (renderCallFooter returns null), so the card closes
// under its header.
func TestCallCardEdges(t *testing.T) {
	defer setDefaultAvatar("local://ava_0002")()
	st := newTestStyle()
	start := strconv.FormatInt(time.Now().Unix()-600, 10)
	d := kitDoc(t, `[{"type":"call","call_id":"R1","call":{"v1":{"id":"R1","name":"Design review","date_start":`+start+`,
		"join_url":"https://call.test/j","active_participants":[{"slack_id":"U1"},{"external_id":"g1","display_name":"Guest"}],"all_participants":[]}}}]`)
	v := layoutAt(st, d, Options{}, 800)
	if ims := imgsWithURL(&v.d, "local://ava_0002"); len(ims) != 1 || ims[0].r.Dx() != 36 || ims[0].radii[0] != 3 {
		t.Errorf("guest face %+v, want ava_0002 at 36px with 3px corners", ims)
	}
	// the viewer (U0) missed U9's DM call
	d = kitDoc(t, `[{"type":"call","call_id":"R2","call":{"v1":{"id":"R2","is_dm_call":true,"channels":["D1"],"created_by":"U9","was_missed":true,
		"date_start":1700000000,"date_end":1700000060,"join_url":"https://call.test/j2","all_participants":[{"slack_id":"U9"}]}}}]`)
	v = layoutAt(st, d, Options{}, 800)
	p := st.Palette
	body := Over(rgba(p.Text.R, p.Text.G, p.Text.B, 0.04), p.Panel)
	for _, b := range v.d.boxes {
		if b.kind == bFill && b.fill == body {
			t.Errorf("a footer band %v on a missed DM call with its facepile hidden", b.r)
		}
	}
	if !hasFrag(&v.d, "missed") {
		t.Errorf("no status: %q", fragStrings(&v.d))
	}
}

// TestCallFacepileOverflow: seven people in a call show five faces, the
// fifth under "+2" (Facepile: maxNumFaces 5, the rest counted on the
// last face): the overlay's dark box sits under its text
// (.c-avatar__additional_overlay), on the face.
func TestCallFacepileOverflow(t *testing.T) {
	st := newTestStyle()
	start := strconv.FormatInt(time.Now().Unix()-600, 10)
	ps := ""
	for i := 1; i <= 7; i++ {
		if i > 1 {
			ps += ","
		}
		ps += `{"external_id":"g` + strconv.Itoa(i) + `","display_name":"Guest","avatar_url":"https://call.test/g` + strconv.Itoa(i) + `.png"}`
	}
	d := kitDoc(t, `[{"type":"call","call_id":"R1","call":{"v1":{"id":"R1","name":"All hands","date_start":`+start+`,
		"join_url":"https://call.test/j","active_participants":[`+ps+`],"all_participants":[]}}}]`)
	v := layoutAt(st, d, Options{}, 800)
	var faces []imgPrim
	for _, im := range v.d.imgs {
		if strings.HasPrefix(im.url, "https://call.test/g") {
			faces = append(faces, im)
		}
	}
	if len(faces) != 5 {
		t.Fatalf("%d faces, want 5", len(faces))
	}
	fifth := faces[4].r
	more := findFrag(t, &v.d, "+2")
	if int(more.x) < fifth.Min.X || int(more.x+more.w) > fifth.Max.X || more.top < fifth.Min.Y || more.bot > fifth.Max.Y {
		t.Errorf("+2 at (%.0f, %d)-(%.0f, %d), want inside the fifth face %v", more.x, more.top, more.x+more.w, more.bot, fifth)
	}
	var overlay *box
	for i := range v.d.boxes {
		if b := &v.d.boxes[i]; b.r == fifth && b.kind == bFill && b.fill == rgba(0, 0, 0, 0.5) {
			overlay = b
		}
	}
	if overlay == nil || overlay.layer != 2 {
		t.Errorf("overlay %+v, want the 50%% black box on layer 2 (over the face, under the +2)", overlay)
	}
}

// failImages fails every URL in bad and has every other one ready,
// recording what was asked for.
type failImages struct {
	bad  map[string]bool
	asks map[string]int
}

func (f *failImages) Get(url string, _ int) (paint.ImageOp, bool) {
	f.asks[url]++
	if f.bad[url] {
		return paint.ImageOp{}, false
	}
	return paint.NewImageOp(image.NewRGBA(image.Rect(0, 0, 4, 4))), true
}

func (f *failImages) Failed(url string, _ int) bool { return f.bad[url] }

// TestCallFaceLoadError: an external participant whose avatar_url fails
// to load shows ava_0002 instead: Facepile's renderExternalAvatar is an Img
// whose renderError is renderExternalAvatarFallback (ConnectedBaseAvatar
// with no member), so the face doesn't wait on its broken image.
func TestCallFaceLoadError(t *testing.T) {
	defer setDefaultAvatar("local://ava_0002")()
	st := newTestStyle()
	imgs := &failImages{bad: map[string]bool{"https://call.test/broken.png": true}, asks: map[string]int{}}
	st.Images = imgs
	start := strconv.FormatInt(time.Now().Unix()-600, 10)
	d := kitDoc(t, `[{"type":"call","call_id":"R3","call":{"v1":{"id":"R3","name":"Sync","date_start":`+start+`,
		"join_url":"https://call.test/j","active_participants":[{"external_id":"g1","display_name":"Guest","avatar_url":"https://call.test/broken.png"}],
		"all_participants":[]}}}]`)
	v := New(d, Options{})
	var ops op.Ops
	v.Layout(testGtx(&ops, 800), st, &recHandler{})
	ims := imgsWithURL(&v.d, "https://call.test/broken.png")
	if len(ims) != 1 || imgOnFail(ims[0]) != "local://ava_0002" {
		t.Fatalf("guest face %+v, want its avatar_url with ava_0002 for a load error", ims)
	}
	if imgs.asks["local://ava_0002"] == 0 {
		t.Errorf("the failed avatar didn't draw ava_0002 (asked %v)", imgs.asks)
	}
	for _, p := range v.pending {
		if p >= 0 && v.d.imgs[p].url == "https://call.test/broken.png" {
			t.Error("the face whose avatar failed is still waiting, with ava_0002 ready")
		}
	}
}

// TestCallIconGlyph: a call card without an app icon draws renderCallIcon's
// glyph in its blue (or, ended, grey) square: the declined DM call's
// call-declined, from the icon font.
func TestCallIconGlyph(t *testing.T) {
	st := newTestStyle()
	d := kitDoc(t, `[{"type":"call","call_id":"R4","call":{"v1":{"id":"R4","is_dm_call":true,"was_rejected":true,"channels":["D1"],"created_by":"U9",
		"date_start":1700000000,"date_end":1700000060,"all_participants":[{"slack_id":"U9"}]}}}]`)
	v := layoutAt(st, d, Options{}, 800)
	if ims := imgsWithURL(&v.d, iconURL+"call-declined-font"); len(ims) != 1 || ims[0].r.Dx() != 20 {
		t.Errorf("declined DM call icon %+v, want the 20px call-declined glyph", ims)
	}
	if ims := imgsWithURL(&v.d, iconURL+"phone"); len(ims) != 0 {
		t.Errorf("the declined DM call drew the phone glyph %+v", ims)
	}
}
