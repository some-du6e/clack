package richview

import (
	"encoding/json"
	"image"
	"image/png"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"gioui.org/f32"
	"gioui.org/io/input"
	"gioui.org/io/pointer"
	"gioui.org/layout"
	"gioui.org/op"
	"gioui.org/op/clip"
	"gioui.org/op/paint"
	"gioui.org/unit"

	rt "github.com/some-du6e/clack/internal/richtext"
	"github.com/some-du6e/clack/internal/slack"
	"github.com/some-du6e/clack/internal/ui/theme/pixeltest"
	"github.com/some-du6e/clack/internal/workobject"
)

// woEntity is a made-up tracker task (or doc) the way an attachment's
// work_object_entity carries it.
func woEntity(id, title string, fields, updated bool) map[string]any {
	lay := map[string]any{"title": map[string]any{"text": title}, "subtitle": map[string]any{"text": "TASK-1 · Clack"},
		"header_title": map[string]any{"text": "Tracker"},
		"product_icon": map[string]any{"type": "image", "image_url": "http://img.test/product.png"}}
	if updated {
		lay["updated_at"] = 1762699700
	}
	if fields {
		lay["fields"] = map[string]any{"elements": []any{
			map[string]any{"field_type": "tag", "label": "Status", "tag": []any{map[string]any{"text": "In progress", "color": "lagoon"}}},
			map[string]any{"field_type": "text", "label": "Project", "text": "Clack desktop"},
		}}
		lay["actions"] = map[string]any{"primary_actions": []any{map[string]any{"type": "button", "action_id": "go", "block_id": "b",
			"text": map[string]any{"text": "Start work"}}}}
	}
	return map[string]any{"work_object_type": "external", "entity_id": id, "app_id": "A0TEST", "app_name": "Example Tracker",
		"product_name": "Tracker", "display_type": "Task", "external_url": "https://tracker.example.com/" + id,
		"comments": map[string]any{"count": 1}, "layouts": map[string]any{"expanded": lay, "compact": lay}}
}

func woDocOf(t testing.TB, ents ...map[string]any) rt.Doc {
	t.Helper()
	m := slack.Message{Type: "message", TS: "1762700000.000300", User: "U0", Text: "look"}
	for i, e := range ents {
		raw, _ := json.Marshal(e)
		m.Attachments = append(m.Attachments, slack.Attachment{ID: i + 1, FromURL: "https://tracker.example.com/x", WorkObjectEntity: raw})
	}
	return rt.FromMessage(codeRes{}, &m)
}

// woCards are the laid-out cards' frames (radius 12, an otl-ter border,
// no hover layer).
func woCards(v *View) []image.Rectangle {
	var out []image.Rectangle
	for _, b := range v.d.boxes {
		if b.kind == bBorder && b.radius == 12 && b.hov == 0 && b.fill.A > 0 {
			out = append(out, b.r)
		}
	}
	return out
}

// woTexts are the laid-out frags' texts, the hover layers' (hov) or
// the static ones'.
func woTexts(v *View, hov bool) string {
	var b strings.Builder
	for _, f := range v.d.frags {
		if f.kind == fText && f.sh != nil && (f.hov != 0) == hov {
			b.WriteString(f.sh.str)
			b.WriteByte(' ')
		}
	}
	return b.String()
}

func woStyle(f WorkObjectFlags) *Style {
	st := newTestStyle()
	st.SetWorkObjects(f)
	return st
}

// A lone card: the meta line (the header title), then the expanded card
// at most 480 wide with its header, fields and the footer's "Updated …"
// line (work_objects_refresh_timestamp) and primary action.
func TestWorkObjectCard(t *testing.T) {
	st := woStyle(WorkObjectFlags{Rearrange: true, RefreshTimestamp: true, CommentSync: true, Locale: "en-US"})
	v := layoutAt(st, woDocOf(t, woEntity("W:A0TEST:T1", "Draw work objects", true, true)), Options{Channel: "C1", TS: "1762700000.000300"}, 900)
	cards := woCards(v)
	if len(cards) != 1 {
		t.Fatalf("%d cards", len(cards))
	}
	if w := cards[0].Dx(); w != 480 {
		t.Errorf("card width %d, want 480 (customMaxWidth)", w)
	}
	static := woTexts(v, false)
	flat := strings.ReplaceAll(static, " ", "") // words shape apart ("TASK-" "1")
	for _, want := range []string{"Tracker", "Draw work objects", "TASK-1 · Clack", "Status", "In progress", "Project", "Clack desktop", "Updated", "Start work"} {
		if !strings.Contains(flat, strings.ReplaceAll(want, " ", "")) {
			t.Errorf("card text %q lacks %q", static, want)
		}
	}
	hover := woTexts(v, true)
	for _, want := range []string{"Preview in Slack", "Open", "Refresh"} {
		if !strings.Contains(hover, want) {
			t.Errorf("hover layers %q lack %q", hover, want)
		}
	}
	if strings.Contains(static, "Preview in Slack") || strings.Contains(static, "Refresh") {
		t.Error("hover-only text drawn at rest")
	}
	// without the refresh timestamp the header carries the time and the
	// footer only the actions
	st2 := woStyle(WorkObjectFlags{Rearrange: true, Locale: "en-US"})
	v2 := layoutAt(st2, woDocOf(t, woEntity("W:A0TEST:T1", "Draw work objects", true, true)), Options{Channel: "C1", TS: "1762700000.000300"}, 900)
	if s := woTexts(v2, false); strings.Contains(s, "Updated") {
		t.Errorf("footer time without work_objects_refresh_timestamp: %q", s)
	}
}

// Rearranged (work_objects_rearrangement), two cards share a row as
// compact cards (FileGallery --v3: two columns, 8px apart, cells at most
// 354); otherwise they stack under "2 attachments", 8px apart.
func TestWorkObjectRearrangement(t *testing.T) {
	d := func() rt.Doc {
		return woDocOf(t, woEntity("W:A0TEST:D1", "Roadmap", true, false), woEntity("W:A0TEST:D2", "Notes", true, false))
	}
	on := layoutAt(woStyle(WorkObjectFlags{Rearrange: true, Locale: "en-US"}), d(), Options{}, 900)
	c := woCards(on)
	if len(c) != 2 || c[0].Min.Y != c[1].Min.Y || c[0].Dx() != 354 || c[1].Min.X != c[0].Max.X+8 {
		t.Fatalf("rearranged cards %v, want two 354px cells side by side", c)
	}
	if s := woTexts(on, false); strings.Contains(s, "Status") || !strings.Contains(s, "2 attachments") {
		t.Errorf("rearranged: compact cards under \"2 attachments\", got %q", s)
	}
	off := layoutAt(woStyle(WorkObjectFlags{Locale: "en-US"}), d(), Options{}, 900)
	c = woCards(off)
	if len(c) != 2 || c[1].Min.Y != c[0].Max.Y+8 || c[0].Min.X != c[1].Min.X {
		t.Fatalf("stacked cards %v", c)
	}
	if s := woTexts(off, false); !strings.Contains(s, "2 attachments") || !strings.Contains(s, "Status") {
		t.Errorf("not rearranged: expanded cards under \"2 attachments\", got %q", s)
	}
}

type woRec struct {
	recHandler
	evs []WorkObjectEvent
}

func (r *woRec) WorkObject(ev WorkObjectEvent) { r.evs = append(r.evs, ev) }

// The card's click and the actions bar's Open reach the handler as
// work object events; the bar draws only while the card is hovered.
func TestWorkObjectClicks(t *testing.T) {
	h := &woRec{}
	hs := &harness{st: woStyle(WorkObjectFlags{Rearrange: true, RefreshTimestamp: true, Locale: "en-US"}),
		v: New(woDocOf(t, woEntity("W:A0TEST:T1", "Draw work objects", true, true)), Options{Channel: "C1", TS: "1762700000.000300"}), h: h, w: 900}
	hs.frame()
	cards := woCards(hs.v)
	if len(cards) != 1 {
		t.Fatalf("%d cards", len(cards))
	}
	r := cards[0]
	// the card body
	p := f32.Pt(float32(r.Min.X+40), float32(r.Max.Y-60))
	hs.send(pointer.Event{Kind: pointer.Move, Source: pointer.Mouse, Position: p}, press(p, 0), release(p, 20))
	if len(h.evs) != 1 || h.evs[0].Action != WOClick || h.evs[0].Att == nil || h.evs[0].Channel != "C1" {
		t.Fatalf("card click: %+v", h.evs)
	}
	// the bar's first button (Open) sits 8px in from the top right
	var open image.Rectangle
	for _, hit := range hs.v.d.hits {
		if tg := hs.v.d.targets[hit.t]; tg.kind == tWorkObject && tg.id == "open" {
			open = hit.r
		}
	}
	if open.Empty() || open.Min.Y < r.Min.Y+8 {
		t.Fatalf("no Open button in the bar (%v, card %v)", open, r)
	}
	q := f32.Pt(float32(open.Min.X+open.Max.X)/2, float32(open.Min.Y+open.Max.Y)/2)
	// a second later: two clicks within 400ms are a double click (tapCount)
	hs.send(pointer.Event{Kind: pointer.Move, Source: pointer.Mouse, Position: q}, press(q, time.Second), release(q, time.Second+20*time.Millisecond))
	if len(h.evs) != 2 || h.evs[1].Action != WOOpen {
		t.Fatalf("Open: %+v", h.evs)
	}
	shown := 0
	for i := range hs.v.d.hovers {
		if hs.v.d.hovers[i].shown(hs.v.hover) && hs.v.d.hovers[i].bar {
			shown++
		}
	}
	if shown != 1 {
		t.Errorf("the bar isn't shown with its own button hovered (%d)", shown)
	}
}

// The flexpane's Details (WorkObjectDetails): the full layout's fields,
// label and value in a row from 496px, and "Updated …".
func TestWorkObjectDetails(t *testing.T) {
	e := woEntity("W:A0TEST:T1", "Draw work objects", true, true)
	full := e["layouts"].(map[string]any)["expanded"]
	e["layouts"].(map[string]any)["full"] = full
	e["ts"] = "1762699700"
	raw, _ := json.Marshal(e)
	ent := workobject.Parse(raw)
	wo := rt.WorkObjectFromEntity(codeRes{}, ent, "")
	d := rt.Doc{Blocks: []rt.Block{{Kind: rt.AttachmentBlock, Att: &rt.Attachment{WorkObject: wo}}}}
	v := layoutAt(woStyle(WorkObjectFlags{Locale: "en-US"}), d, Options{WorkObjectDetails: true}, 600)
	s := woTexts(v, false)
	for _, want := range []string{"Status", "In progress", "Project", "Clack desktop", "Updated"} {
		if !strings.Contains(s, want) {
			t.Errorf("details %q lack %q", s, want)
		}
	}
	if len(woCards(v)) != 0 {
		t.Error("the Details tab drew a card")
	}
}

// TestWorkObjectShots saves the cards, light and dark (the
// messages-audit-shots workflow sets MESSAGES_AUDIT_SHOTS_DIR): a lone
// task at rest and hovered (the actions bar, "Preview in Slack", the
// footer's Refresh), two docs rearranged side by side, the same two
// stacked under their meta line without work_objects_rearrangement, and
// the flexpane's Details body.
func TestWorkObjectShots(t *testing.T) {
	dir := os.Getenv("MESSAGES_AUDIT_SHOTS_DIR")
	if dir == "" {
		t.Skip("MESSAGES_AUDIT_SHOTS_DIR not set (the messages-audit-shots workflow)")
	}
	task := func() rt.Doc {
		return woDocOf(t, woEntity("W:A0TEST:T1", "Draw work object cards like Slack", true, true))
	}
	pair := func() rt.Doc {
		return woDocOf(t, woEntity("W:A0TEST:D1", "Clack roadmap", true, false), woEntity("W:A0TEST:D2", "Meeting notes", true, false))
	}
	for _, dark := range []bool{false, true} {
		mode := map[bool]string{false: "light", true: "dark"}[dark]
		pal := LightPalette()
		if dark {
			pal = DarkPalette()
		}
		for _, c := range []struct {
			name  string
			doc   func() rt.Doc
			flags WorkObjectFlags
			hover bool
			opts  Options
		}{
			{"card", task, WorkObjectFlags{Rearrange: true, RefreshTimestamp: true, CommentSync: true}, false, Options{}},
			{"card-hover", task, WorkObjectFlags{Rearrange: true, RefreshTimestamp: true, CommentSync: true}, true, Options{}},
			{"card-no-refresh-timestamp", task, WorkObjectFlags{Rearrange: true}, false, Options{}},
			{"rearranged", pair, WorkObjectFlags{Rearrange: true, RefreshTimestamp: true}, false, Options{}},
			{"stacked", pair, WorkObjectFlags{RefreshTimestamp: true}, false, Options{}},
		} {
			c.flags.Locale = "en-US"
			st := NewStyle(themeShaper(), &gradImages{cache: map[string]paint.ImageOp{}}, pal)
			st.SetWorkObjects(c.flags)
			c.opts.Channel, c.opts.TS = "C1", "1762700000.000300"
			v := New(c.doc(), c.opts)
			var r input.Router
			var ops op.Ops
			const w, h, pad = 820, 560, 20
			frame := func() {
				ops.Reset()
				paint.FillShape(&ops, pal.Panel, clip.Rect{Max: image.Pt(w, h)}.Op())
				tr := op.Offset(image.Pt(pad, pad)).Push(&ops)
				gtx := layout.Context{Ops: &ops, Source: r.Source(), Metric: unit.Metric{PxPerDp: 1, PxPerSp: 1},
					Constraints: layout.Exact(image.Pt(w-2*pad, h-2*pad))}
				v.Layout(gtx, st, &recHandler{})
				tr.Pop()
				r.Frame(&ops)
			}
			frame()
			if c.hover {
				cards := woCards(v)
				if len(cards) == 0 {
					t.Fatal("no card")
				}
				cr := cards[0]
				r.Queue(pointer.Event{Kind: pointer.Move, Source: pointer.Mouse, Position: f32.Pt(float32(pad+cr.Min.X+60), float32(pad+cr.Max.Y-50))})
				frame()
				frame()
			}
			frame()
			img := pixeltest.Render(t, w, h, &ops)
			fh, err := os.Create(filepath.Join(dir, "work-object-"+c.name+"-"+mode+".png"))
			if err != nil {
				t.Fatal(err)
			}
			if err := png.Encode(fh, img); err != nil {
				t.Fatal(err)
			}
			fh.Close()
		}
	}
}
