package canvasview

// link-url-box-4: a canvas's link card to a Slack list, a list view or a
// list item draws Slack's EmbeddedSlackList (the list's header over its
// preview), and a /canvas/C… link draws the channel's canvas.

import (
	"encoding/json"
	"image"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/some-du6e/clack/internal/fakeslack"
)

func listLink(id, query string) string {
	u := "https://fake.slack.com/lists/" + fakeslack.TeamID + "/" + id
	if query != "" {
		u += "?" + query
	}
	return u
}

// TestRenderLinkCardList: a list link's card is the list's entity: its
// title and "List" over its first records (files.info and
// lists.records.list, never lists.open); a view's says "{view} - Table
// view"; an item's is the item's name, "Item in {list}" and its fields; a
// view the list doesn't have, and a list files.info doesn't know, draw
// Slack's list error state. The card asks for no frames once drawn.
func TestRenderLinkCardList(t *testing.T) {
	for _, c := range []struct {
		name, link, kind, primary, second, cell, code string
	}{
		{"list", listLink(fakeslack.ListKitchen, ""), "list", "Launch tracker", "List", "Write the launch announcement", ""},
		{"view", listLink(fakeslack.ListKitchen, "view_id=View0GRP001"), "list", "Launch tracker", "By status - Table view", "", ""},
		{"item", listLink(fakeslack.ListKitchen, "record_id="+fakeslack.ListKitchenRecord), "list", "Write the launch announcement", "Item in Launch tracker", "", ""},
		{"view deleted", listLink(fakeslack.ListKitchen, "view_id=View0GONE01"), "list-error", "", "", "", "view_deleted"},
		{"not found", listLink("F0NOSUCHLST1", ""), "list-error", "", "", "", "not_found"},
	} {
		t.Run(c.name, func(t *testing.T) {
			h, fs := newEnv(t, false)
			cardEnvOn(t, h, fs, c.link, bareContainer(), cardTS, nil)
			r, v := cardRig(t, h, Actions{}, 15*time.Second, func(v *View) bool {
				k, _, _ := cardState(v)
				return k == c.kind
			})
			k, size, _ := cardState(v)
			if k != c.kind {
				t.Fatalf("the card is %q, want %s", k, c.kind)
			}
			if c.code != "" && listCode(v) != c.code {
				t.Errorf("the list's state %q, want %q", listCode(v), c.code)
			}
			p, s, cells := listShown(v)
			if c.primary != "" && p != c.primary {
				t.Errorf("the entity's title %q, want %q", p, c.primary)
			}
			if c.second != "" && s != c.second {
				t.Errorf("the entity's second line %q, want %q", s, c.second)
			}
			if c.cell != "" && !slices.Contains(cells, c.cell) {
				t.Errorf("the preview's rows %q have no %q", cells, c.cell)
			}
			if c.name == "item" && len(cells) == 0 {
				t.Error("the item's preview shows no fields")
			}
			if want := max(1100*400/799, 235); size.Y < 235 || size.Y > want+2 {
				t.Errorf("the card is %d tall, want the 799×400 box (%d at this width, at least 235)", size.Y, want)
			}
			if n := fs.CallCount("lists.open"); n != 0 {
				t.Errorf("the card called lists.open %d times", n)
			}
			frameFor(r, 500*time.Millisecond)
			if wakes(r) {
				t.Error("the list card keeps asking for frames")
			}
		})
	}
}

// TestRenderLinkCardChannelCanvas: a /canvas/C… link's card is the
// channel's canvas (gZnA: the channel's properties.canvas.file_id),
// drawn as a canvas link card is; a channel without one is EmptyUnfurl.
func TestRenderLinkCardChannelCanvas(t *testing.T) {
	for _, c := range []struct {
		name, props, kind, file string
	}{
		{"with a canvas", `{"canvas":{"file_id":"` + fakeslack.CanvasChannel + `","is_empty":false}}`, "canvas", fakeslack.CanvasChannel},
		{"without one", `{}`, "empty", ""},
	} {
		t.Run(c.name, func(t *testing.T) {
			h, fs := newEnv(t, false)
			ch := fs.ChannelIDs()[3]
			cv, ok := h.a.S.Conv(ch)
			if !ok {
				t.Fatalf("the store holds no %s", ch)
			}
			x := *cv
			x.Properties = json.RawMessage(c.props)
			h.a.S.PutConv(x)
			link := "https://fake.slack.com/canvas/" + ch
			_, _, open := cardEnvOn(t, h, fs, link, bareContainer(), cardTS, nil)
			v, s := open(Full, image.Pt(1100, 900))
			waitKind(s, v, c.kind, "")
			if k, _, _ := cardState(v); k != c.kind {
				t.Fatalf("the card is %q (%q), want %s", k, embedText(v), c.kind)
			}
			if c.file != "" && canvasShownID(v) != c.file {
				t.Errorf("the card draws canvas %q, want the channel's %s", canvasShownID(v), c.file)
			}
			if c.kind == "canvas" && !strings.Contains(embedText(v), "Launch plan") {
				t.Errorf("the canvas card's text %q has no %q", embedText(v), "Launch plan")
			}
		})
	}
}
