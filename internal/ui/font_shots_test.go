package ui

// A channel in every Appearance > Font option, light mode, plus the open
// font menu with each option drawn in its own font. The whole window over
// an offline core (synthetic workspace, nothing talks to Slack); the web
// fonts come from Slack's public CDN without a cookie, like the browser's
// @font-face loads. Runs only with FONT_SHOTS_DIR set (the font-shots
// workflow), which also gets fonts.txt: the family each option resolved
// to on the runner.

import (
	"encoding/json"
	"fmt"
	"image"
	"image/png"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"gioui.org/gpu/headless"

	"github.com/some-du6e/clack/internal/slack"
	"github.com/some-du6e/clack/internal/ui/theme/pixeltest"
	"github.com/some-du6e/clack/internal/ui/uifont"
)

const fontShotChan = "C0FONTS001"

// fontShotBlocks is one rich_text message: plain, bold, italic, strike,
// inline code, a link, an emoji, a mention.
func fontShotBlocks() json.RawMessage {
	els := []map[string]any{
		{"type": "text", "text": "The quick brown fox jumps over the lazy dog. "},
		{"type": "text", "text": "Bold", "style": map[string]any{"bold": true}},
		{"type": "text", "text": ", "},
		{"type": "text", "text": "italic", "style": map[string]any{"italic": true}},
		{"type": "text", "text": ", "},
		{"type": "text", "text": "struck", "style": map[string]any{"strike": true}},
		{"type": "text", "text": " and "},
		{"type": "text", "text": "inline code", "style": map[string]any{"code": true}},
		{"type": "text", "text": " stay in their fonts. 0123456789 "},
		{"type": "emoji", "name": "wave", "unicode": "1f44b"},
		{"type": "text", "text": " "},
		{"type": "link", "url": "https://slack.com/help", "text": "a link"},
		{"type": "text", "text": " for "},
		{"type": "user", "user_id": svAlice},
	}
	b, _ := json.Marshal([]any{map[string]any{"type": "rich_text", "block_id": "f1", "elements": []any{
		map[string]any{"type": "rich_text_section", "elements": els},
		map[string]any{"type": "rich_text_preformatted", "elements": []any{map[string]any{"type": "text", "text": "func main() {\n\tfmt.Println(\"code blocks keep the monospace font\")\n}"}}},
	}}})
	return b
}

func TestFontShots(t *testing.T) {
	dir := os.Getenv("FONT_SHOTS_DIR")
	if dir == "" {
		t.Skip("FONT_SHOTS_DIR not set (the font-shots workflow)")
	}
	e := ctxShell(t)
	now := time.Now()
	e.ca.S.PutUsers([]slack.User{{ID: ttSelf, Name: "karim", RealName: "Karim Example", Profile: slack.Profile{DisplayName: "karim", RealName: "Karim Example"}}})
	e.ca.S.PutConv(slack.Conversation{ID: fontShotChan, Name: "fonts", IsChannel: true, IsMember: true, NumMembers: 3,
		Topic: &slack.Purpose{Value: "Every Appearance > Font option"}})
	msg := func(at time.Duration, user, text string, blocks json.RawMessage) slack.Message {
		if blocks == nil {
			blocks = shotBlocks(text)
		}
		return slack.Message{Type: "message", TS: shotTS(now.Add(at)), User: user, Text: text, Blocks: blocks}
	}
	e.ca.S.SetLatest(fontShotChan, []slack.Message{
		msg(-9*time.Minute, svAlice, "Has anyone tried the new font setting?", nil),
		msg(-8*time.Minute, ttSelf, "fmt", fontShotBlocks()),
		msg(-7*time.Minute, ttSelf, "Sphinx of black quartz, judge my vow. Pack my box with five dozen liquor jugs.", nil),
		msg(-2*time.Minute, svAlice, "Съешь же ещё этих мягких французских булок — and some ×÷±≠ «quotes» … ", nil),
	}, false)
	b := e.a.data
	b.fetched[fontShotChan] = time.Now() // offline: no tabs/facepile fetches
	if len(b.m.Sections) > 0 {
		b.m.Sections[0].Rows = append(b.m.Sections[0].Rows, fontShotChan)
		b.m.version++
	}
	win, err := pixeltest.NewWindow(e.sz.X, e.sz.Y)
	if err != nil {
		t.Fatalf("headless window: %v", err)
	}
	t.Cleanup(win.Release)
	shot := func(name string, w *headless.Window) {
		for i := 0; i < 40; i++ { // names, images, layout settle
			e.frame()
			time.Sleep(10 * time.Millisecond)
		}
		if err := w.Frame(e.ops); err != nil {
			t.Fatal(err)
		}
		img := image.NewRGBA(image.Rectangle{Max: e.sz})
		if err := w.Screenshot(img); err != nil {
			t.Fatal(err)
		}
		f, err := os.Create(filepath.Join(dir, name+".png"))
		if err != nil {
			t.Fatal(err)
		}
		defer f.Close()
		if err := png.Encode(f, img); err != nil {
			t.Fatal(err)
		}
	}

	e.a.Navigate("c/" + fontShotChan)
	var report strings.Builder
	fmt.Fprintf(&report, "Appearance > Font on %s (CI runner)\n\n", runtime.GOOS)
	for i, o := range uifont.Options(runtime.GOOS) {
		f, err := fontLoader(false).Load(t.Context(), o)
		if err != nil {
			t.Fatalf("%s: %v", o.Label, err)
		}
		fmt.Fprintf(&report, "%-28s %-26q → %s", o.Label, o.FontFamily, f.Family)
		if f.Fallback {
			report.WriteString(" (fallback)")
		}
		report.WriteString("\n")
		e.a.setClientTypeface(o)
		waitFor(t, e.a, o.Label, func() bool { return e.a.font.applied == o.Value && e.a.font.loading == "" })
		shot(fmt.Sprintf("font-%02d-%s-light", i+1, o.Value), win)
	}
	if err := os.WriteFile(filepath.Join(dir, "fonts.txt"), []byte(report.String()), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Log("\n" + report.String())

	// the select open on Appearance, every option in its own font (Lato
	// as the UI font again)
	e.a.setClientTypeface(uifont.Default())
	waitFor(t, e.a, "Lato", func() bool { return e.a.font.applied == uifont.Lato && e.a.font.loading == "" })
	e.a.showPrefs("appearance")
	e.a.loadFontPreviews()
	waitFor(t, e.a, "the previews", func() bool { return e.a.font.preview != nil })
	for i := 0; i < 10; i++ {
		e.frame()
	}
	e.a.menu(image.Pt(e.sz.X/2-200, 230), e.a.fontItems(e.a.currentFont()))
	shot("font-menu-light", win)
}
