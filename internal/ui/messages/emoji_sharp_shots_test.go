package messages

// TestEmojiSharpShots (EMOJI_SHARP_SHOTS_DIR and CLACK_EMOJI_CDN, the
// emoji-picker workflow's emoji-sharp jobs) draws a synthetic channel with
// Karim's message of three :grin: (jumbomoji, 32px), emoji inline in text
// (22px), reactions (16px) and a status emoji next to a name, light and
// dark, at 100%, 125% and 150%, with Slack's own standard emoji images from
// its public CDN (a.slack-edge.com, no cookie). Every name and message is
// made up; nothing talks to Slack's API.

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"image"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"gioui.org/layout"
	"gioui.org/op"
	"gioui.org/op/clip"
	"gioui.org/op/paint"
	"gioui.org/unit"

	"github.com/some-du6e/clack/internal/core"
	"github.com/some-du6e/clack/internal/emoji"
	"github.com/some-du6e/clack/internal/fakeslack"
	"github.com/some-du6e/clack/internal/imgcache"
	"github.com/some-du6e/clack/internal/slack"
	"github.com/some-du6e/clack/internal/store"
	"github.com/some-du6e/clack/internal/ui/theme"
	"github.com/some-du6e/clack/internal/ui/theme/pixeltest"
	"github.com/some-du6e/clack/internal/ui/uipref"
)

const esChan = "C0EMOJI01"

func esEmoji(name, uni string) map[string]any {
	return map[string]any{"type": "emoji", "name": name, "unicode": uni}
}

func esText(s string) map[string]any { return map[string]any{"type": "text", "text": s} }

func esRich(els ...any) json.RawMessage {
	b, _ := json.Marshal([]any{map[string]any{"type": "rich_text", "block_id": "r", "elements": []any{
		map[string]any{"type": "rich_text_section", "elements": els}}}})
	return b
}

// esApp is the synthetic #emoji channel.
func esApp(t *testing.T) (*core.App, *fakeslack.Server, map[string]string) {
	t.Helper()
	SetTaut(nil)
	fs, err := fakeslack.Start(fakeslack.Small)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { fs.Close() })
	c := slack.New(fs.Creds())
	c.EdgeURL = fs.URL + "edge/"
	s := store.New("")
	s.Lock()
	s.TeamID, s.TeamName = fakeslack.TeamID, "Fake Club"
	s.Unlock()
	self := slack.User{ID: fakeslack.SelfID, TeamID: fakeslack.TeamID, Name: "me", RealName: "Me Example",
		Profile: slack.Profile{DisplayName: "me", Image48: shotAvatar(fakeslack.SelfID), Image72: shotAvatar(fakeslack.SelfID)}}
	users := []slack.User{self}
	for _, u := range shotUsers {
		p := slack.Profile{DisplayName: u.name, RealName: u.real, Image48: shotAvatar(u.id), Image72: shotAvatar(u.id)}
		if u.status != "" {
			p.StatusEmoji, p.StatusText = ":"+u.status+":", "Out of office"
		}
		users = append(users, slack.User{ID: u.id, TeamID: fakeslack.TeamID, Name: u.name, RealName: u.real, Profile: p})
	}
	s.PutUsers(users)
	s.PutConv(slack.Conversation{ID: esChan, Name: "emoji", IsChannel: true, IsMember: true, NumMembers: 4})
	day := time.Now()
	at := func(h, m int) string {
		tt := time.Date(day.Year(), day.Month(), day.Day(), h, m, 0, 0, time.Local)
		return fmt.Sprintf("%d.000100", tt.Unix())
	}
	ts := map[string]string{"jumbo": at(10, 1), "inline": at(10, 5), "status": at(10, 9)}
	grin := esEmoji("grin", "1f601")
	jumbo := slack.Message{Type: "message", TS: ts["jumbo"], User: "UANA00001", Text: ":grin::grin::grin:", Blocks: esRich(grin, grin, grin)}
	inline := slack.Message{Type: "message", TS: ts["inline"], User: "UBEN00001", Text: "shipped it :grin: thanks all :tada:",
		Blocks: esRich(esText("shipped it "), grin, esText(" thanks all "), esEmoji("tada", "1f389"))}
	inline.Reactions = []slack.Reaction{{Name: "grin", Count: 2, Users: []string{"UANA00001", fakeslack.SelfID}}, {Name: "+1", Count: 1, Users: []string{"UCARA0001"}}}
	status := slack.Message{Type: "message", TS: ts["status"], User: "UCARA0001", Text: "Back Monday :palm_tree:",
		Blocks: esRich(esText("Back Monday "), esEmoji("palm_tree", "1f334"))}
	s.SetLatest(esChan, []slack.Message{jumbo, inline, status}, false)
	app := core.New(c, s)
	app.S.Self = self
	app.ServeOffline()
	return app, fs, ts
}

// esFetch serves Slack's standard emoji from its public CDN and the rest
// (avatars) from the fake.
func esFetch(fs *fakeslack.Server) imgcache.Fetcher {
	return func(ctx context.Context, u string) ([]byte, error) {
		if !strings.HasPrefix(u, "https://a.slack-edge.com/production-standard-emoji-assets/") {
			if strings.HasPrefix(u, fs.URL) || strings.HasPrefix(u, "https://avatars.invalid/") {
				return fs.Fetch(ctx, u)
			}
			return nil, errors.New("not fetched in this test")
		}
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
		if err != nil {
			return nil, err
		}
		res, err := http.DefaultClient.Do(req)
		if err != nil {
			return nil, err
		}
		defer res.Body.Close()
		if res.StatusCode != 200 {
			return nil, fmt.Errorf("%s: %s", u, res.Status)
		}
		return io.ReadAll(res.Body)
	}
}

func TestEmojiSharpShots(t *testing.T) {
	dir := os.Getenv("EMOJI_SHARP_SHOTS_DIR")
	if dir == "" || os.Getenv("CLACK_EMOJI_CDN") == "" {
		t.Skip("EMOJI_SHARP_SHOTS_DIR and CLACK_EMOJI_CDN not set (emoji-picker's emoji-sharp jobs)")
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(uipref.Reset)
	p := *uipref.Get()
	p.Dense, p.Jumbomoji = false, true
	uipref.Set(p)
	const cw, ch = 700, 420 // CSS px
	for _, s := range []float32{1, 1.25, 1.5} {
		for _, dark := range []bool{false, true} {
			app, fs, ts := esApp(t)
			if got, want := func() string { u, _, _ := app.S.Emoji("grin"); return u }(), emoji.URL("1f601", ""); got != want {
				t.Errorf(":grin: resolves to %q, want %q (Slack's set for this platform)", got, want)
			}
			th := theme.New(dark)
			imgs := imgcache.New(esFetch(fs), "", nil)
			host := identityHost{coreHost: coreHost{app: app, th: th}, imgs: imgs}
			v := NewChannelView(host, esChan)
			v.Preview = true
			w, h := int(float32(cw)*s+0.5), int(float32(ch)*s+0.5)
			ops := new(op.Ops)
			f := func() {
				ops.Reset()
				paint.FillShape(ops, th.C.Bg, clip.Rect{Max: image.Pt(w, h)}.Op())
				gtx := layout.Context{Ops: ops, Metric: unit.Metric{PxPerDp: s, PxPerSp: s}, Constraints: layout.Exact(image.Pt(w, h)), Now: time.Now()}
				v.Layout(gtx)
			}
			loadList(v, f)
			until(t, 5*time.Second, "the rows", f, func() bool { return v.s.rows[ts["status"]] != nil && itemHeight(v.s, ts["status"]) > 0 })
			for i := 0; i < 200 && imgs.Pending() > 0; i++ {
				f()
				time.Sleep(20 * time.Millisecond)
			}
			for i := 0; i < 20; i++ {
				f()
				time.Sleep(10 * time.Millisecond)
			}
			img := pixeltest.Render(t, w, h, ops)
			y0, y1 := rowsSpan(v.s)
			name := fmt.Sprintf("clack-%s-%gx.png", map[bool]string{false: "light", true: "dark"}[dark], s)
			savePNG(t, filepath.Join(dir, name), crop(img, y0, y1))
		}
	}
}
