package messages

// TestCompactShots (COMPACT_SHOTS_DIR, the compact-shots workflow) draws a
// realistic synthetic channel and one of its threads in Clean and Compact,
// light and dark, 1100px wide, and saves what each row says in
// compact-ref-rows.json so .github/scripts/compact_ref.py can build the same
// rows in Slack's markup under Slack's shipping CSS for the side-by-sides.
// Every name and message is made up; nothing talks to Slack.

import (
	"context"
	"encoding/json"
	"fmt"
	"image"
	"image/draw"
	"image/png"
	"os"
	"path/filepath"
	"testing"
	"time"

	"gioui.org/layout"
	"gioui.org/op"
	"gioui.org/op/clip"
	"gioui.org/op/paint"
	"gioui.org/unit"

	"github.com/some-du6e/clack/internal/core"
	"github.com/some-du6e/clack/internal/fakeslack"
	"github.com/some-du6e/clack/internal/imgcache"
	"github.com/some-du6e/clack/internal/slack"
	"github.com/some-du6e/clack/internal/store"
	"github.com/some-du6e/clack/internal/ui/theme"
	"github.com/some-du6e/clack/internal/ui/theme/pixeltest"
	"github.com/some-du6e/clack/internal/ui/uipref"
)

// compactRefRow is one row as the reference page draws it.
type compactRefRow struct {
	Key, Kind, Name, Time, Text, Avatar string
	Header, Edited, Bot, Pinned         bool
	List                                []string
	Reactions                           []compactRefReact
	Replies                             int
	ReplyAvatars                        []string
	Status                              string // the sender's status emoji picture
	LastReply                           string
	RootPreview                         string // a broadcast's root, as its preamble shows it
}

var (
	shotChannelKeys = []string{"a1", "a2", "b1", "b2", "bot", "root", "bc", "join", "pin", "c1", "gone"}
	shotThreadKeys  = []string{"root", "r1", "r2", "r3"}
)

type compactRefReact struct {
	Emoji string
	Count int
	Self  bool
}

const tShots = "C0RELEASE"

type shotUser struct{ id, name, real, status string }

var shotUsers = []shotUser{
	{"UANA00001", "ana", "Ana Ortiz", ""},
	{"UBEN00001", "ben", "Ben Okafor", ""},
	{"UCARA0001", "cara", "Cara Lindqvist", "palm_tree"},
}

func shotAvatar(id string) string { return "https://avatars.invalid/" + id + "-72.png" }

func shotRich(parts ...any) json.RawMessage {
	var els []any
	for _, p := range parts {
		els = append(els, p)
	}
	b, _ := json.Marshal([]any{map[string]any{"type": "rich_text", "block_id": "r", "elements": els}})
	return b
}

func shotSection(s string) map[string]any {
	return map[string]any{"type": "rich_text_section", "elements": []any{map[string]any{"type": "text", "text": s}}}
}

func shotList(items ...string) map[string]any {
	var els []any
	for _, it := range items {
		els = append(els, shotSection(it))
	}
	return map[string]any{"type": "rich_text_list", "style": "bullet", "indent": 0, "elements": els}
}

// shotsApp is the synthetic #release channel and its thread.
func shotsApp(t *testing.T) (*core.App, *fakeslack.Server, map[string]string) {
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
	s.PutBot("B0DEPLOY1", store.BotInfo{Name: "Deploy Bot", Icon: shotAvatar("B0DEPLOY1")})
	s.PutConv(slack.Conversation{ID: tShots, Name: "release", IsChannel: true, IsMember: true, NumMembers: 12})
	day := time.Now()
	at := func(h, m int) string {
		tt := time.Date(day.Year(), day.Month(), day.Day(), h, m, 0, 0, time.Local)
		return fmt.Sprintf("%d.000100", tt.Unix())
	}
	ts := map[string]string{
		"a1": at(9, 2), "a2": at(9, 3), "b1": at(9, 10), "b2": at(9, 11), "bot": at(9, 15), "root": at(9, 20),
		"bc": at(9, 31), "join": at(9, 40), "pin": at(9, 45), "c1": at(9, 50), "gone": at(9, 52),
		"r1": at(9, 22), "r2": at(9, 24), "r3": at(9, 25), "g1": at(9, 53),
	}
	msg := func(key, user, text string, blocks json.RawMessage) slack.Message {
		return slack.Message{Type: "message", TS: ts[key], User: user, Text: text, Blocks: blocks}
	}
	a1 := msg("a1", "UANA00001", "Morning! Did the 4.2 release go out?", shotRich(shotSection("Morning! Did the 4.2 release go out?")))
	a2 := msg("a2", "UANA00001", "I saw the deploy notice but not the changelog.", shotRich(shotSection("I saw the deploy notice but not the changelog.")))
	long := "It did, around 8:40. The rollout paused once at 20% while the error rate settled, then went to everyone. " +
		"Search indexing is a little behind, so give it an hour before you check the new filters."
	b1 := msg("b1", "UBEN00001", long, shotRich(shotSection(long)))
	b1.Edited = &slack.Edited{User: "UBEN00001", TS: at(9, 12)}
	b1.Reactions = []slack.Reaction{{Name: "tada", Count: 3, Users: []string{"UANA00001", fakeslack.SelfID, "UCARA0001"}}, {Name: "eyes", Count: 1, Users: []string{"UANA00001"}}}
	b2 := msg("b2", "UBEN00001", "Still to do:", shotRich(shotList("Post the changelog", "Close the milestone")))
	bot := slack.Message{Type: "message", TS: ts["bot"], BotID: "B0DEPLOY1", Username: "Deploy Bot", Text: "Deployed web 4.2.0 to production",
		Blocks: json.RawMessage(`[{"type":"section","block_id":"s","text":{"type":"mrkdwn","text":"Deployed *web 4.2.0* to production"}}]`)}
	root := msg("root", "UANA00001", "Changelog draft is in the release doc, comments welcome.", shotRich(shotSection("Changelog draft is in the release doc, comments welcome.")))
	root.ThreadTS, root.ReplyCount, root.ReplyUsers, root.LatestReply = ts["root"], 3, []string{"UBEN00001", "UCARA0001"}, ts["r3"]
	bc := msg("bc", "UCARA0001", "Added the migration notes to the doc.", shotRich(shotSection("Added the migration notes to the doc.")))
	bc.Subtype, bc.ThreadTS = "thread_broadcast", ts["root"]
	join := slack.Message{Type: "message", TS: ts["join"], User: "UCARA0001", Subtype: "channel_join", Text: "<@UCARA0001> has joined the channel"}
	pin := msg("pin", fakeslack.SelfID, "Release checklist: freeze Friday, ship Tuesday.", shotRich(shotSection("Release checklist: freeze Friday, ship Tuesday.")))
	pin.PinnedTo = []string{tShots}
	pin.PinnedInfo = &slack.PinnedInfo{Channel: tShots, PinnedBy: fakeslack.SelfID}
	c1 := msg("c1", "UCARA0001", "Back Monday, ping Ben for anything urgent.", shotRich(shotSection("Back Monday, ping Ben for anything urgent.")))
	gone := slack.Message{Type: "message", TS: ts["gone"], User: "USLACKBOT", Subtype: "tombstone", Hidden: true, Text: "This message was deleted.",
		ThreadTS: ts["gone"], ReplyCount: 1, ReplyUsers: []string{"UBEN00001"}, LatestReply: ts["g1"]}
	s.SetLatest(tShots, []slack.Message{a1, a2, b1, b2, bot, root, bc, join, pin, c1, gone}, false)
	reply := func(key, user, text string) slack.Message {
		m := msg(key, user, text, shotRich(shotSection(text)))
		m.ThreadTS = ts["root"]
		return m
	}
	s.SetThread(tShots, ts["root"], []slack.Message{root,
		reply("r1", "UBEN00001", "Looks good. Can we call out the new filters first?"),
		reply("r2", "UCARA0001", "Agreed, and link the migration guide."),
		reply("r3", "UCARA0001", "I'll add it now."),
	})
	app := core.New(c, s)
	app.S.Self = self
	app.ServeOffline()
	return app, fs, ts
}

// shotFrame lays v out at w×h and returns the ops.
func shotFrame(v interface {
	Layout(gtx layout.Context) layout.Dimensions
}, th *theme.Theme, w, h int, ops *op.Ops) {
	ops.Reset()
	paint.FillShape(ops, th.C.Bg, clip.Rect{Max: image.Pt(w, h)}.Op())
	gtx := layout.Context{Ops: ops, Metric: unit.Metric{PxPerDp: 1, PxPerSp: 1}, Constraints: layout.Exact(image.Pt(w, h)), Now: time.Now()}
	v.Layout(gtx)
}

// rowsSpan is the y range of the list's message rows.
func rowsSpan(s *listState) (int, int) {
	y0, y1 := -1, 0
	for i := s.visible[0]; i < s.visible[1] && i < len(s.items); i++ {
		if s.items[i].kind != itMsg {
			continue
		}
		if y0 < 0 {
			y0 = s.pos[i]
		}
		y1 = s.pos[i] + s.heights[i]
	}
	return max(y0, 0), y1
}

func savePNG(t *testing.T, path string, img image.Image) {
	t.Helper()
	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	if err := png.Encode(f, img); err != nil {
		t.Fatal(err)
	}
}

func crop(img *image.RGBA, y0, y1 int) *image.RGBA {
	out := image.NewRGBA(image.Rect(0, 0, img.Bounds().Dx(), y1-y0))
	draw.Draw(out, out.Bounds(), img, image.Pt(0, y0), draw.Src)
	return out
}

func TestCompactShots(t *testing.T) {
	dir := os.Getenv("COMPACT_SHOTS_DIR")
	if dir == "" {
		t.Skip("COMPACT_SHOTS_DIR not set (the compact-shots workflow)")
	}
	t.Cleanup(uipref.Reset)
	const w, h, tw, th0 = 1100, 1500, 520, 1100
	var rows, threadRows []compactRefRow
	heights := map[string]map[string]float32{}
	for _, dark := range []bool{false, true} {
		for _, dense := range []bool{false, true} {
			p := *uipref.Get()
			p.Dense, p.Jumbomoji, p.ShowTyping = dense, true, true
			uipref.Set(p)
			app, fs, ts := shotsApp(t)
			th := theme.New(dark)
			host := identityHost{coreHost: coreHost{app: app, th: th}, imgs: imgcache.New(fs.Fetch, "", nil)}
			v := NewChannelView(host, tShots)
			v.Preview = true
			ops := new(op.Ops)
			f := func() { shotFrame(v, th, w, h, ops) }
			loadList(v, f)
			until(t, 5*time.Second, "the rows", f, func() bool { return v.s.rows[ts["gone"]] != nil && itemHeight(v.s, ts["gone"]) > 0 })
			// let the pictures arrive (imgcache decodes off the UI goroutine)
			for i := 0; i < 60; i++ {
				f()
				time.Sleep(10 * time.Millisecond)
			}
			f()
			img := pixeltest.Render(t, w, h, ops)
			y0, y1 := rowsSpan(v.s)
			mode := map[bool]string{false: "light", true: "dark"}[dark]
			kind := map[bool]string{false: "clean", true: "compact"}[dense]
			savePNG(t, filepath.Join(dir, "clack-"+kind+"-"+mode+".png"), crop(img, y0, y1))

			tv := NewThreadView(host, tShots, ts["root"])
			tops := new(op.Ops)
			ft := func() { shotFrame(tv, th, tw, th0, tops) }
			until(t, 5*time.Second, "the replies", ft, func() bool { return tv.s.rows[ts["r3"]] != nil && itemHeight(tv.s, ts["r3"]) > 0 })
			for i := 0; i < 40; i++ {
				ft()
				time.Sleep(10 * time.Millisecond)
			}
			ft()
			timg := pixeltest.Render(t, tw, th0, tops)
			ty0, ty1 := rowsSpan(tv.s)
			ty0, ty1 = ty0+tv.s.origin.Y, ty1+tv.s.origin.Y // the list sits under the pane's header
			savePNG(t, filepath.Join(dir, "clack-thread-"+kind+"-"+mode+".png"), crop(timg, ty0, ty1))

			if dark {
				continue
			}
			for k, hs := range rowHeights(v.s, tv.s, ts, dense) {
				heights[k] = hs
			}
			if dense {
				continue
			}
			rows = shotRefRows(t, app, v.s, ts, shotChannelKeys)
			threadRows = shotRefRows(t, app, tv.s, ts, shotThreadKeys)
			for i := range threadRows { // ThreadSenderAndTimestampGeneric's THREAD_DETAIL_VIEW time
				threadRows[i].Time = threadTime(tsTime(ts[threadRows[i].Key]), time.Now())
			}
			// the pictures, for the reference page
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			for _, id := range []string{"UANA00001", "UBEN00001", "UCARA0001", fakeslack.SelfID, "B0DEPLOY1"} {
				if b, err := fs.Fetch(ctx, shotAvatar(id)); err == nil {
					_ = os.WriteFile(filepath.Join(dir, "avatar-"+id+".png"), b, 0o644)
				}
			}
			for _, e := range []string{"tada", "eyes", "palm_tree"} {
				if u, _, _ := app.S.Emoji(e); u != "" {
					if b, err := fs.Fetch(ctx, u); err == nil {
						_ = os.WriteFile(filepath.Join(dir, "emoji-"+e+".png"), b, 0o644)
					}
				}
			}
			cancel()
		}
	}
	b, _ := json.MarshalIndent(map[string]any{"channel": rows, "thread": threadRows}, "", " ")
	if err := os.WriteFile(filepath.Join(dir, "compact-ref-rows.json"), b, 0o644); err != nil {
		t.Fatal(err)
	}
	b, _ = json.MarshalIndent(heights, "", " ")
	if err := os.WriteFile(filepath.Join(dir, "clack-row-heights.json"), b, 0o644); err != nil {
		t.Fatal(err)
	}
}

// shotRefRows reads the rows back for the reference page.
func shotRefRows(t *testing.T, app *core.App, s *listState, ts map[string]string, keys []string) []compactRefRow {
	var out []compactRefRow
	for _, k := range keys {
		r := s.rows[ts[k]]
		if r == nil {
			t.Fatalf("no row %s", k)
		}
		m := &r.m
		row := compactRefRow{Key: k, Name: r.name, Time: clock(tsTime(m.TS)), Text: rowText(r), Edited: m.Edited, Bot: r.badge != "",
			Pinned: m.Pinned, Replies: m.ReplyCount, Avatar: "avatar-" + firstNonEmptyStr(m.User, m.BotID) + ".png"}
		for i := range s.items {
			if s.items[i].kind == itMsg && s.items[i].key == ts[k] {
				row.Header = s.items[i].header
			}
		}
		switch {
		case m.Tombstone:
			row.Kind = "tombstone"
		case m.System:
			row.Kind = "system"
		case m.Broadcast:
			row.Kind = "broadcast"
		case r.badge != "":
			row.Kind = "bot"
		default:
			row.Kind = "message"
		}
		if k == "b2" {
			row.Kind, row.List, row.Text = "list", []string{"Post the changelog", "Close the milestone"}, ""
		}
		for _, re := range m.Reactions {
			row.Reactions = append(row.Reactions, compactRefReact{Emoji: re.Name, Count: re.Count, Self: re.Self})
		}
		if m.LatestReply != "" {
			row.LastReply = clock(tsTime(m.LatestReply))
		}
		for _, u := range m.ReplyUsers {
			row.ReplyAvatars = append(row.ReplyAvatars, "avatar-"+u+".png")
		}
		if r.statusURL != "" {
			row.Status = "emoji-palm_tree.png"
		}
		if m.Broadcast {
			row.RootPreview = r.rootPrev
		}
		out = append(out, row)
	}
	return out
}

func firstNonEmptyStr(a, b string) string {
	if a != "" {
		return a
	}
	return b
}
