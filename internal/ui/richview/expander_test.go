package richview

import (
	"image"
	"strings"
	"sync"
	"testing"

	"gioui.org/io/pointer"

	"github.com/some-du6e/clack/internal/expandable"
	rt "github.com/some-du6e/clack/internal/richtext"
	"github.com/some-du6e/clack/internal/slack"
)

// The collapsible media read and write Slack's expandables store
// (expander.go) under Slack's keys: an image block's caret, its too-large
// warning, a link preview's media, the hover group's "Hide image", the
// unverified sender's disclosure.

// fakeExp is an Expander with an unverified sender rule of its own.
type fakeExp struct {
	localExp
	mu         sync.Mutex
	unverified map[string]bool // sender/channel pairs the rule hides
	allowed    []string
}

func (x *fakeExp) hides(sender, ch string) bool {
	x.mu.Lock()
	defer x.mu.Unlock()
	return x.unverified[sender+"/"+ch]
}

func (x *fakeExp) AttachmentExpanded(key string, bytes int64, sender, ch string) bool {
	if on, ok := x.Expanded(key); ok {
		return on
	}
	return x.localExp.AttachmentExpanded(key, bytes, sender, ch) && !x.hides(sender, ch)
}

func (x *fakeExp) UnverifiedDisclosure(key string, bytes int64, sender, ch string) bool {
	if _, ok := x.Expanded(key); ok || x.ImageTooLarge(key, bytes) {
		return false
	}
	return x.hides(sender, ch)
}

func (x *fakeExp) CollapseFromUnverifiedUser(sender, ch string) bool { return x.hides(sender, ch) }
func (x *fakeExp) PrefMemberName(id string) string                   { return "Ext Person" }
func (x *fakeExp) AllowPreviewsFrom(m string) {
	x.mu.Lock()
	x.allowed = append(x.allowed, m)
	x.mu.Unlock()
}

func useExpander(t *testing.T, e Expander) {
	t.Helper()
	SetExpander(e)
	t.Cleanup(func() { SetExpander(nil) })
}

// fragWith is the first text frag starting with s (a link's last word
// may carry the punctuation after it).
func fragWith(t *testing.T, d *display, s string) *frag {
	t.Helper()
	for _, f := range textFrags(d) {
		if strings.HasPrefix(f.sh.str, s) {
			return f
		}
	}
	t.Fatalf("no text frag starting %q; have %q", s, fragStrings(d))
	return nil
}

// expTarget is the tExpandable target for key, or -1.
func expTarget(v *View, key string) int16 {
	for i, tg := range v.d.targets {
		if tg.kind == tExpandable && tg.id == key {
			return int16(i)
		}
	}
	return -1
}

const imgBlock = `[{"type":"image","block_id":"pic","image_url":"https://img.test/a.png","alt_text":"a",` +
	`"title":{"type":"plain_text","text":"Shot"},"image_width":400,"image_height":200,"image_bytes":12345}]`

// TestImageBlockCaretInStore: an image block in a message shows its size
// (convertFilesize: "12 kB") and the caret; the caret collapses it under
// getExpandableKey("blockImage", ts, {blockId}), which every View of the
// message reads (a re-created row stays collapsed), and expands it again.
func TestImageBlockCaretInStore(t *testing.T) {
	st := newTestStyle()
	d := kitDoc(t, imgBlock)
	o := Options{Channel: "C1", TS: "1.0", ViewContext: expandable.ViewChannel}
	key := expandable.Key(expandable.BlockImage, "1.0", expandable.Opts{BlockID: "pic"})
	t.Cleanup(func() { forgetExpandable(key) })
	v := layoutAt(st, d, o, 700)
	if len(imgsWithURL(&v.d, "https://img.test/a.png")) != 1 || !strings.Contains(string(v.d.plain), "(12 kB)") {
		t.Fatalf("the image block: %d images, plain %q", len(imgsWithURL(&v.d, "https://img.test/a.png")), v.d.plain)
	}
	tg := expTarget(v, key)
	if tg < 0 || v.d.targets[tg].on {
		t.Fatalf("no collapse caret for %s", key)
	}
	v.activate(tg, image.Point{}, nil)
	if on, ok := xp().Expanded(key); !ok || on {
		t.Fatalf("the caret left %v %v", on, ok)
	}
	for _, w := range []*View{v, layoutAt(st, d, o, 700)} {
		w.relayout(st, 700, 1, 1)
		if len(imgsWithURL(&w.d, "https://img.test/a.png")) != 0 || len(boxesOf(&w.d, bCaretRight)) != 1 {
			t.Errorf("collapsed: %d images, %d right carets", len(imgsWithURL(&w.d, "https://img.test/a.png")), len(boxesOf(&w.d, bCaretRight)))
		}
	}
	v.activate(expTarget(v, key), image.Point{}, nil)
	v.relayout(st, 700, 1, 1)
	if len(imgsWithURL(&v.d, "https://img.test/a.png")) != 1 {
		t.Error("the caret didn't expand it again")
	}
}

// TestImageBlockTooLarge: over 20 MB with obey_inline_img_limit (uipref's
// default) the image starts collapsed behind the trigger's warning; its
// "expand it anyway" expands it, "open it in a new window" is the image's
// link.
func TestImageBlockTooLarge(t *testing.T) {
	st := newTestStyle()
	d := kitDoc(t, strings.Replace(imgBlock, `"image_bytes":12345`, `"image_bytes":25000000`, 1))
	o := Options{Channel: "C1", TS: "1.0", ViewContext: expandable.ViewChannel}
	key := expandable.Key(expandable.BlockImage, "1.0", expandable.Opts{BlockID: "pic"})
	t.Cleanup(func() { forgetExpandable(key) })
	v := layoutAt(st, d, o, 700)
	want := "(Not automatically expanded because 25 MB is too large. You can expand it anyway or open it in a new window.)"
	if len(imgsWithURL(&v.d, "https://img.test/a.png")) != 0 || !strings.Contains(string(v.d.plain), want) {
		t.Fatalf("too large: %d images, plain %q", len(imgsWithURL(&v.d, "https://img.test/a.png")), v.d.plain)
	}
	open := fragWith(t, &v.d, "window")
	if tg := v.d.targets[open.target]; tg.kind != tLink || tg.url != "https://img.test/a.png" {
		t.Errorf("open it in a new window: %+v", tg)
	}
	anyway := fragWith(t, &v.d, "anyway")
	tg := v.d.targets[anyway.target]
	if tg.kind != tExpandable || tg.id != key || !tg.on {
		t.Fatalf("expand it anyway: %+v", tg)
	}
	v.activate(anyway.target, image.Point{}, nil)
	v.relayout(st, 700, 1, 1)
	if len(imgsWithURL(&v.d, "https://img.test/a.png")) != 1 || strings.Contains(string(v.d.plain), "Not automatically") {
		t.Errorf("after expand it anyway: plain %q", v.d.plain)
	}
}

// imgMenus records ImageMenu calls.
type imgMenus struct {
	recHandler
	menus [][2]string
}

func (h *imgMenus) ImageMenu(href, key string, _ image.Rectangle) {
	h.menus = append(h.menus, [2]string{href, key})
}

// TestImageActionsGroup: hovering an image block's image shows
// AttachmentImageActions at top 10, right 10: Open in new window (the
// image's link) and More actions (the menu, with the image's expandable
// for Hide image); its Expand only with animations off.
func TestImageActionsGroup(t *testing.T) {
	h := &imgMenus{}
	key := expandable.Key(expandable.BlockImage, "1.0", expandable.Opts{BlockID: "pic"})
	hs := &harness{st: newTestStyle(), v: New(kitDoc(t, imgBlock), Options{Channel: "C1", TS: "1.0"}), h: h, w: 700}
	hs.frame()
	if len(hs.v.d.expands) != 1 || !hs.v.d.expands[0].full || hs.v.d.expands[0].key != key {
		t.Fatalf("groups %+v", hs.v.d.expands)
	}
	img := imgsWithURL(&hs.v.d, "https://img.test/a.png")[0]
	hs.send(pointer.Event{Kind: pointer.Move, Source: pointer.Mouse, Position: rectCenter(img.r)})
	acts := hs.v.expandActs(hs.st, 0)
	bar, btns := hs.v.expandBar(0, len(acts))
	if len(acts) != 2 || acts[0] != imgActOpen || acts[1] != imgActMore || bar.Max.X != img.r.Max.X-10 || bar.Min.Y != img.r.Min.Y+10 || btns[0].Dx() != 32 {
		t.Fatalf("the group %v %v in %v: %v", bar, btns, img.r, acts)
	}
	more := rectCenter(btns[1])
	hs.send(pointer.Event{Kind: pointer.Move, Source: pointer.Mouse, Position: more})
	hs.send(press(more, 0), release(more, 20*1e6))
	if len(h.menus) != 1 || h.menus[0] != [2]string{"https://img.test/a.png", key} {
		t.Fatalf("More actions: %v", h.menus)
	}
	open := rectCenter(btns[0])
	hs.send(pointer.Event{Kind: pointer.Move, Source: pointer.Mouse, Position: open})
	hs.send(press(open, 1e9), release(open, 1e9+20*1e6))
	if len(h.urls) != 1 || h.urls[0] != "https://img.test/a.png" || len(h.images) != 0 {
		t.Errorf("Open in new window: urls %v images %v", h.urls, h.images)
	}
}

// TestUnverifiedDisclosure: an image the unverified sender rule hides
// (an outsider in a DM) shows the disclosure under its title: "just show
// this one" expands it, "Allow all previews" allows the sender.
func TestUnverifiedDisclosure(t *testing.T) {
	x := &fakeExp{unverified: map[string]bool{"UEXT/D9": true}}
	useExpander(t, x)
	st := newTestStyle()
	d := kitDoc(t, imgBlock)
	o := Options{Channel: "D9", TS: "1.0", Sender: "UEXT"}
	key := expandable.Key(expandable.BlockImage, "1.0", expandable.Opts{BlockID: "pic"})
	v := layoutAt(st, d, o, 700)
	plain := string(v.d.plain)
	if len(imgsWithURL(&v.d, "https://img.test/a.png")) != 0 ||
		!strings.Contains(plain, "This preview is hidden because Ext Person is from outside your organization.") ||
		!strings.Contains(plain, "Allow all previews from Ext Person, or\u00a0just show this one.") {
		t.Fatalf("disclosure: plain %q", plain)
	}
	all := fragWith(t, &v.d, "Allow")
	v.activate(all.target, image.Point{}, nil)
	if len(x.allowed) != 1 || x.allowed[0] != "UEXT" {
		t.Errorf("Allow all previews: %v", x.allowed)
	}
	one := fragWith(t, &v.d, "one")
	v.activate(one.target, image.Point{}, nil)
	v.relayout(st, 700, 1, 1)
	if on, ok := x.Expanded(key); !ok || !on || len(imgsWithURL(&v.d, "https://img.test/a.png")) != 1 {
		t.Errorf("just show this one: %v %v, %d images", on, ok, len(imgsWithURL(&v.d, "https://img.test/a.png")))
	}
	// the same sender in a channel: shown
	if w := layoutAt(st, d, Options{Channel: "C1", TS: "2.0", Sender: "UEXT"}, 700); len(imgsWithURL(&w.d, "https://img.test/a.png")) != 1 {
		t.Error("the rule hid a channel's image")
	}
}

// TestAttachmentMediaInStore: a link preview's image (MessageAttachment
// v1) is shown until its expandable, getExpandableKey("attachment", ts,
// {id, imageUrl, viewContext}), is collapsed (/collapse, Hide image);
// another view's key leaves it shown.
func TestAttachmentMediaInStore(t *testing.T) {
	st := newTestStyle()
	m := slack.Message{TS: "1.0", Attachments: []slack.Attachment{{ID: 1, Title: "A page", ImageURL: "https://img.test/p.png", ImageWidth: 400, ImageHeight: 200}}}
	d := rt.FromMessage(codeRes{}, &m)
	o := Options{Channel: "C1", TS: "1.0", ViewContext: expandable.ViewChannel}
	key := expandable.AttachmentKey(&m.Attachments[0], "1.0", expandable.ViewChannel)
	if key != "img_msg_1.01Channel" {
		t.Fatalf("key %q", key)
	}
	t.Cleanup(func() {
		forgetExpandable(key)
		forgetExpandable(key[:len(key)-len("Channel")] + "Thread")
	})
	if v := layoutAt(st, d, o, 700); len(imgsWithURL(&v.d, "https://img.test/p.png")) != 1 {
		t.Fatal("the preview's image isn't drawn")
	}
	SetExpandable(key[:len(key)-len("Channel")]+"Thread", false)
	if v := layoutAt(st, d, o, 700); len(imgsWithURL(&v.d, "https://img.test/p.png")) != 1 {
		t.Error("the thread's collapse hid the channel's image")
	}
	SetExpandable(key, false)
	v := layoutAt(st, d, o, 700)
	if len(imgsWithURL(&v.d, "https://img.test/p.png")) != 0 {
		t.Error("collapsed: the image is still drawn")
	}
	if !strings.Contains(string(v.d.plain), "A page") {
		t.Errorf("the card went with its image: %q", v.d.plain)
	}
}
