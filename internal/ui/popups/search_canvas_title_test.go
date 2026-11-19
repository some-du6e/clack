package popups

// A canvas result's title as Slack's canvas titles draw it (srCanvasTitle,
// srTitleSpans: renderQuipTitle over title_blocks): an emoji the emoji
// map learned after the results came in is drawn as its image, never its
// ":name:". A hidden emoji (Unicode 17 with emoji_v17 off) stays its
// characters even when a custom emoji has its name. Fake Slack only.

import (
	"context"
	"encoding/json"
	"errors"
	"image"
	"slices"
	"strings"
	"sync"
	"testing"

	"gioui.org/font"
	"gioui.org/layout"

	"github.com/some-du6e/clack/internal/core"
	"github.com/some-du6e/clack/internal/fakeslack"
	"github.com/some-du6e/clack/internal/imgcache"
	"github.com/some-du6e/clack/internal/richtext"
)

// srTitleWidget draws a result title's spans as the Files tab does.
type srTitleWidget struct {
	h  *tzHost
	sp []richtext.Span
}

func (w srTitleWidget) Layout(gtx layout.Context) layout.Dimensions {
	return srTitleSpans(gtx, w.h, w.h.th, w.sp, 15, 22, font.Bold, rgb(0x1d1c1d), 700)
}

func TestSearchCanvasTitleEmojiLate(t *testing.T) {
	e := srNewEnv(t, false)
	var mu sync.Mutex
	var asked []string
	e.h.im = imgcache.New(func(_ context.Context, u string) ([]byte, error) {
		mu.Lock()
		asked = append(asked, u)
		mu.Unlock()
		return nil, errors.New("no network in tests")
	}, "", nil)
	wasAsked := func(u string) bool { mu.Lock(); defer mu.Unlock(); return slices.Contains(asked, u) }

	const name, hidden = "late-search-fixture", "late-hidden-fixture"
	blocks, _ := json.Marshal([]any{map[string]any{"type": "rich_text", "elements": []any{map[string]any{"type": "rich_text_section",
		"elements": []any{map[string]any{"type": "text", "text": "Search plan "}, map[string]any{"type": "emoji", "name": name}}}}}})
	f := core.File{ID: "F0SRCHEMJ1", IsCanvas: true, Title: "Search plan :" + name + ":", TitleBlocks: blocks}
	sp := srCanvasTitle(e.h, f)
	if !slices.ContainsFunc(sp, func(s richtext.Span) bool { return s.Kind == richtext.Emoji && s.ID == name && s.URL == "" }) {
		t.Fatalf("precondition: the title's spans %+v lack the unknown %s emoji", sp, name)
	}
	// a hidden emoji: its characters, no image by design (richEmoji)
	char := string(rune(0x1FAE8))
	sp = append(sp, richtext.Span{Kind: richtext.Text, Text: " "}, richtext.Span{Kind: richtext.Emoji, ID: hidden, Text: char})

	d := &srDrv{size: image.Pt(800, 40), w: srTitleWidget{h: e.h, sp: sp}}
	if x := strings.Join(d.scan(), "\x00"); !strings.Contains(x, ":"+name+":") {
		t.Fatalf("precondition: before the map has it the emoji reads as its name, drew %q", x)
	}
	url := e.fs.URL + "img/64x64/" + fakeslack.TeamID + "/" + name + "/00000000000000f2.png"
	hiddenURL := e.fs.URL + "img/64x64/" + fakeslack.TeamID + "/" + hidden + "/00000000000000f3.png"
	e.h.a.S.CustomEmoji.Put(name, url)
	e.h.a.S.CustomEmoji.Put(hidden, hiddenURL)
	d.until(t, "the title's emoji asked for as an image", func() bool { return wasAsked(url) })
	x := strings.Join(d.scan(), "\x00")
	if strings.Contains(x, ":"+name+":") {
		t.Errorf("the emoji still reads as its name: %q", x)
	}
	if !strings.Contains(x, "Search plan") || !strings.Contains(x, char) {
		t.Errorf("the title's text or the hidden emoji's characters aren't drawn: %q", x)
	}
	if wasAsked(hiddenURL) {
		t.Errorf("the hidden emoji was drawn as the custom emoji of its name")
	}
}
