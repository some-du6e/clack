package core

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/some-du6e/clack/internal/canvas"
	"github.com/some-du6e/clack/internal/fakeslack"
	"github.com/some-du6e/clack/internal/richtext"
	"github.com/some-du6e/clack/internal/slack"
	"github.com/some-du6e/clack/internal/store"
)

func canvasApp(t *testing.T) (*App, *fakeslack.Server) {
	t.Helper()
	fs, err := fakeslack.Start(fakeslack.Small)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { fs.Close() })
	a := New(slack.New(fs.Creds()), store.New(t.TempDir()))
	t.Cleanup(a.Shutdown)
	if err := a.Boot(context.Background()); err != nil {
		t.Fatal(err)
	}
	return a, fs
}

// The channel canvas tab, the export and its embedded files.
func TestCanvasFetch(t *testing.T) {
	a, fs := canvasApp(t)
	ctx := context.Background()
	tabs, err := a.Tabs(ctx, "C0FAKE00000")
	if err != nil {
		t.Fatal(err)
	}
	if len(tabs) == 0 || tabs[0].Type != "canvas" || tabs[0].FileID != fakeslack.CanvasChannel || tabs[0].Title != "Launch plan" {
		t.Fatalf("tabs = %+v", tabs)
	}
	cd, err := a.Canvas(ctx, fakeslack.CanvasChannel)
	if err != nil {
		t.Fatal(err)
	}
	if cd.Info.Title != "Launch plan" || cd.Info.Creator != "U0FAKE00001" || !cd.Info.Editable || cd.Info.Updated == 0 {
		t.Errorf("info %+v", cd.Info)
	}
	seen := map[canvas.Kind]bool{}
	cd.Doc.Walk(func(b *canvas.Block) bool { seen[b.Kind] = true; return true })
	for k := canvas.Paragraph; k <= canvas.Callout; k++ {
		if !seen[k] {
			t.Errorf("block kind %d missing from the fixture", k)
		}
	}
	if f := cd.Files[fakeslack.CanvasImage]; !f.IsImage || f.Thumb == "" || f.ThumbW != 1024 {
		t.Errorf("image file %+v", f)
	}
	if f := cd.Files[fakeslack.CanvasFile]; f.Title != "Quarterly report" || f.IsImage {
		t.Errorf("pdf file %+v", f)
	}
	if fs.CallCount("files-pri") != 1 {
		t.Errorf("export fetched %d times", fs.CallCount("files-pri"))
	}
	// the export is re-read each time (no cache)
	if _, err := a.Canvas(ctx, fakeslack.CanvasChannel); err != nil || fs.CallCount("files-pri") != 2 {
		t.Errorf("refetch: %v, %d", err, fs.CallCount("files-pri"))
	}
	if _, err := a.Canvas(ctx, fakeslack.CanvasImage); err == nil {
		t.Error("a PNG opened as a canvas")
	}
}

// Editing round trip: canvases.edit with markdown from the model, the fake
// applies it to its document, the export Clack re-reads has the change.
func TestCanvasEditRoundTrip(t *testing.T) {
	a, fs := canvasApp(t)
	ctx := context.Background()
	evs, stop := a.Subscribe()
	defer stop()
	a.SetRealtime(true)
	waitEvent(t, evs, 5*time.Second, func(ev Event) bool { return ev.Type == "connection" && ev.Connected })

	cd, err := a.Canvas(ctx, fakeslack.CanvasChannel)
	if err != nil {
		t.Fatal(err)
	}
	if err := a.OpenCanvas(ctx, fakeslack.CanvasChannel); err != nil {
		t.Fatal(err)
	}
	if fc, _ := fs.Canvas(fakeslack.CanvasChannel); fc.Opened != 1 {
		t.Errorf("files.open calls %d", fc.Opened)
	}
	// tick a checklist item, rewrite a paragraph with formatting and a
	// mention, append a block after the heading, delete the divider
	var check, para, h2, div *canvas.Block
	cd.Doc.Walk(func(b *canvas.Block) bool {
		switch {
		case b.Kind == canvas.Check && check == nil && !b.Checked:
			check = b
		case b.Kind == canvas.Paragraph && para == nil && b.ID != "" && len(b.Spans) > 0:
			para = b
		case b.Kind == canvas.Heading2 && h2 == nil:
			h2 = b
		case b.Kind == canvas.Divider && div == nil:
			div = b
		}
		return true
	})
	if check == nil || para == nil || h2 == nil {
		t.Fatal("fixture blocks missing")
	}
	ticked := *check
	ticked.Checked = true
	np := *para
	np.Spans = []richtext.Span{{Kind: richtext.Text, Text: "Rewritten "}, {Kind: richtext.Text, Text: "bold", Style: richtext.Bold},
		{Kind: richtext.Text, Text: " for "}, {Kind: richtext.User, ID: "U0FAKE00003", Text: "@someone"}}
	changes := []CanvasChange{
		{Operation: "replace", SectionID: check.ID, Markdown: CanvasMarkdown(ticked)},
		{Operation: "replace", SectionID: para.ID, Markdown: CanvasMarkdown(np)},
		{Operation: "insert_after", SectionID: h2.ID, Markdown: "A new line under the heading"},
	}
	if div != nil && div.ID != "" {
		changes = append(changes, CanvasChange{Operation: "delete", SectionID: div.ID})
	}
	if err := a.EditCanvas(ctx, fakeslack.CanvasChannel, changes...); err != nil {
		t.Fatal(err)
	}
	// the call is Slack's documented shape
	edits := fs.CanvasEdits()
	if len(edits) != 1 || edits[0].Get("canvas_id") != fakeslack.CanvasChannel {
		t.Fatalf("edits %v", edits)
	}
	var sent []map[string]any
	if err := json.Unmarshal([]byte(edits[0].Get("changes")), &sent); err != nil || len(sent) != len(changes) {
		t.Fatalf("changes %s", edits[0].Get("changes"))
	}
	if dc, _ := sent[0]["document_content"].(map[string]any); dc["type"] != "markdown" || !strings.HasPrefix(dc["markdown"].(string), "- [x] ") {
		t.Errorf("change 0 %v", sent[0])
	}
	// the realtime echo (document_update) reaches the open view
	waitEvent(t, evs, 5*time.Second, func(ev Event) bool { return ev.Type == "canvas" && ev.ID == fakeslack.CanvasChannel })

	cd2, err := a.Canvas(ctx, fakeslack.CanvasChannel)
	if err != nil {
		t.Fatal(err)
	}
	b := cd2.Doc.Find(check.ID)
	if b == nil || !b.Checked || b.Text() != check.Text() {
		t.Errorf("checked item after edit: %+v", b)
	}
	p := cd2.Doc.Find(para.ID)
	if p == nil || p.Text() != "Rewritten bold for @"+fakeNameOf(t, a, fs, "U0FAKE00003") {
		t.Errorf("paragraph after edit: %+v", p)
	}
	found := false
	for i, x := range cd2.Doc.Blocks {
		if x.ID == h2.ID && i+1 < len(cd2.Doc.Blocks) && cd2.Doc.Blocks[i+1].Text() == "A new line under the heading" {
			found = true
		}
	}
	if !found {
		t.Error("inserted block not after the heading")
	}
	if div != nil && div.ID != "" && cd2.Doc.Find(div.ID) != nil {
		t.Error("divider not deleted")
	}

	// rename; another user's edit arrives live
	if err := a.RenameCanvas(ctx, fakeslack.CanvasChannel, "Launch plan v2"); err != nil {
		t.Fatal(err)
	}
	waitEvent(t, evs, 5*time.Second, func(ev Event) bool { return ev.Type == "canvas" })
	fs.EditCanvasAsOther(fakeslack.CanvasChannel, "Added by someone else")
	waitEvent(t, evs, 5*time.Second, func(ev Event) bool { return ev.Type == "canvas" && ev.ID == fakeslack.CanvasChannel })
	cd3, err := a.Canvas(ctx, fakeslack.CanvasChannel)
	if err != nil {
		t.Fatal(err)
	}
	if cd3.Info.Title != "Launch plan v2" || cd3.Doc.Blocks[len(cd3.Doc.Blocks)-1].Text() != "Added by someone else" {
		t.Errorf("after rename + other edit: %q / %q", cd3.Info.Title, cd3.Doc.Blocks[len(cd3.Doc.Blocks)-1].Text())
	}
	if err := a.CloseCanvas(ctx, fakeslack.CanvasChannel); err != nil {
		t.Fatal(err)
	}
	if fs.CallCount("files.close") != 1 {
		t.Error("files.close not called")
	}
}

func fakeNameOf(t *testing.T, a *App, fs *fakeslack.Server, id string) string {
	t.Helper()
	a.EnsureUsers(context.Background(), []string{id})
	u, ok := a.User(id)
	if !ok {
		t.Fatalf("no user %s", id)
	}
	return u.Handle
}

// Creating (standalone and channel canvas) and sharing.
func TestCanvasCreateShare(t *testing.T) {
	a, fs := canvasApp(t)
	ctx := context.Background()
	id, err := a.CreateCanvas(ctx, "", "Ideas", "# Ideas\n\n- one")
	if err != nil {
		t.Fatal(err)
	}
	cd, err := a.Canvas(ctx, id)
	if err != nil || cd.Info.Title != "Ideas" || len(cd.Doc.Blocks) != 2 || cd.Doc.Blocks[1].Kind != canvas.Bullet {
		t.Fatalf("created: %v %+v", err, cd)
	}
	cid, err := a.CreateCanvas(ctx, "C0FAKE00001", "", "")
	if err != nil {
		t.Fatal(err)
	}
	tabs, _ := a.Tabs(ctx, "C0FAKE00001")
	if len(tabs) == 0 || tabs[0].FileID != cid || tabs[0].Title != "Untitled" {
		t.Errorf("channel canvas tab %+v", tabs)
	}
	if fs.CallCount("conversations.canvases.create") != 1 || fs.CallCount("canvases.create") != 1 {
		t.Error("create methods")
	}
	if err := a.ShareCanvas(ctx, id, []string{"C0FAKE00002"}, "write", "Have a look"); err != nil {
		t.Fatal(err)
	}
	if fc, _ := fs.Canvas(id); fc.Access["C0FAKE00002"] != "write" {
		t.Errorf("access %v", fc.Access)
	}
	if fs.CallCount("chat.postMessage") != 1 {
		t.Errorf("share posts %d", fs.CallCount("chat.postMessage"))
	}
}

// Mentions in a canvas export carry names only; they resolve to ids by
// display name, then username, then real name, and only when unique.
func TestResolveCanvasMention(t *testing.T) {
	a, _ := canvasApp(t)
	u := func(id, handle, display, real string) slack.User {
		return slack.User{ID: id, Name: handle, RealName: real, Profile: slack.Profile{DisplayName: display, RealName: real}}
	}
	a.S.PutUsers([]slack.User{
		u("UR0001", "solo.handle", "Solo", "Solo Person"),
		u("UR0002", "twin.one", "Twin", "Twin One"),
		u("UR0003", "twin.two", "Twin", "Twin Two"),
		u("UR0004", "only.handle", "", "Only Real"),
		u("UR0005", "shared.real", "", "Same Name"),
		u("UR0006", "shared.real2", "", "Same Name"),
	})
	for in, want := range map[string]string{
		"@Solo":          "UR0001", // display name
		"@solo":          "UR0001", // case-insensitive
		"@twin.one":      "UR0002", // username (display name "Twin" is ambiguous, but this tier has no match)
		"@Twin":          "",       // two people: never guess
		"@only.handle":   "UR0004",
		"@Only Real":     "UR0004", // real name
		"@Same Name":     "",
		"@nobody.at.all": "",
	} {
		id, ok := a.ResolveCanvasMention(richtext.User, in)
		if id != want || ok != (want != "") {
			t.Errorf("%s -> %q %v, want %q", in, id, ok, want)
		}
	}
	if id, ok := a.ResolveCanvasMention(richtext.Channel, "#"+a.S.Convs["C0FAKE00002"].Name); !ok || id != "C0FAKE00002" {
		t.Errorf("channel -> %q %v", id, ok)
	}
	if _, ok := a.ResolveCanvasMention(richtext.Channel, "#no-such-channel"); ok {
		t.Error("unknown channel resolved")
	}
}
