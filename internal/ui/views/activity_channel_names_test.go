package views

// Activity rows named a channel the store couldn't name by its raw id
// ("Mention in #C0…", Karim's "pruvate channels in notifs … like
// CKJOIRJWEIO", 2026-11-14), wrote "#" before private channels, and kept
// that once the name came in. Slack's rows draw the destination as
// InlineChannelEntity (VeYL, PcpC; internal/ui/chanent): lock-filled and
// the bare name for a private channel, the hash and the name for a public
// one, the loading bar while the store fetches an unknown channel (and
// the name once it's in), the Private channel chip for one the viewer
// can't see. A synthetic feed over a fake Web API, made-up ids and names.

import (
	"encoding/json"
	"image"
	"image/png"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"gioui.org/layout"
	"gioui.org/op"

	"github.com/some-du6e/clack/internal/core"
	"github.com/some-du6e/clack/internal/slack"
	"github.com/some-du6e/clack/internal/store"
	"github.com/some-du6e/clack/internal/ui/theme"
	"github.com/some-du6e/clack/internal/ui/theme/pixeltest"
)

const (
	anPrivKnown = "C0ACTPRIV01" // private, in the store: secret-plans
	anPrivLater = "C0ACTPRIV02" // private, not in the store: conversations.info says later-plans
	anStub      = "C0ACTSTUB01" // private, in the store named by its id: conversations.info says stub-plans
	anGone      = "C0ACTGONE01" // conversations.info: channel_not_found
	anPublic    = "C0ACTPUBL01" // public, in the store: launch-notes
)

// actNamesFeed is the feed: a mention in each channel, an invite to the
// private one, a DM.
func actNamesFeed() []byte {
	mention := func(key, ts, ch string) map[string]any {
		return map[string]any{"key": key, "feed_ts": ts, "is_unread": true,
			"item": map[string]any{"type": "at_user", "message": map[string]any{"ts": ts, "channel": ch, "author_user_id": "U1"}}}
	}
	invite := mention("k-priv-invite", "1700000295.000100", anPrivKnown)
	invite["item"].(map[string]any)["type"] = "internal_channel_invite"
	b, _ := json.Marshal(map[string]any{"ok": true, "items": []any{
		mention("k-priv-known", "1700000300.000100", anPrivKnown),
		mention("k-priv-later", "1700000299.000100", anPrivLater),
		mention("k-stub", "1700000298.000100", anStub),
		mention("k-gone", "1700000297.000100", anGone),
		mention("k-public", "1700000296.000100", anPublic),
		invite,
		map[string]any{"key": "k-dm", "feed_ts": "1700000294.000100", "is_unread": true, "item": map[string]any{"type": "dm",
			"bundle_info": map[string]any{"unread_count": 1, "payload": map[string]any{"dm_entry": map[string]any{
				"latest_message": map[string]any{"ts": "1700000294.000100", "channel": "D1"}}}}}},
	}})
	return b
}

// actNamesApp is a core over the fake API; conversations.info for
// anPrivLater waits until release (a lookup still running), and calls
// counts its calls per channel.
func actNamesApp(t *testing.T) (app *core.App, calls func(ch string) int, release func()) {
	t.Helper()
	feed := actNamesFeed()
	hold := make(chan struct{})
	var once sync.Once
	release = func() { once.Do(func() { close(hold) }) }
	var mu sync.Mutex
	n := map[string]int{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.ParseMultipartForm(1<<20) != nil {
			_ = r.ParseForm()
		}
		write := func(s string) { _, _ = w.Write([]byte(s)) }
		switch strings.TrimPrefix(r.URL.Path, "/api/") {
		case "activity.feed":
			_, _ = w.Write(feed)
		case "messages.list":
			write(`{"ok":true,"messages":{}}`)
		case "activity.views":
			write(`{"ok":true,"views":[{"id":"V1","view_type":"all","density":"compact"}],"prefs":{"sort":"newest","density":"compact"}}`)
		case "conversations.info":
			ch := r.Form.Get("channel")
			mu.Lock()
			n[ch]++
			mu.Unlock()
			switch ch {
			case anPrivLater:
				<-hold
				write(`{"ok":true,"channel":{"id":"` + anPrivLater + `","name":"later-plans","is_group":true,"is_private":true,"is_member":true}}`)
			case anStub:
				write(`{"ok":true,"channel":{"id":"` + anStub + `","name":"stub-plans","is_group":true,"is_private":true,"is_member":true}}`)
			default:
				write(`{"ok":false,"error":"channel_not_found"}`)
			}
		default:
			write(`{"ok":true}`)
		}
	}))
	t.Cleanup(srv.Close)
	t.Cleanup(release) // before Close: a held call has to answer
	c := slack.New(slack.Creds{Token: "xoxc-test", Cookie: "xoxd-test", UserID: "USELF", Team: "T1", EnterpriseID: "E1",
		TeamURL: srv.URL + "/", EnterpriseURL: srv.URL + "/"})
	c.EdgeURL = srv.URL + "/edge/"
	s := store.New("")
	s.Lock()
	s.TeamID, s.TeamName = "T1", "Synthetic Club"
	s.Unlock()
	s.PutUsers([]slack.User{{ID: "USELF", Name: "self", RealName: "Test Self"}, {ID: "U1", Name: "alice", RealName: "Alice Example"}})
	s.PutConv(slack.Conversation{ID: anPrivKnown, Name: "secret-plans", IsGroup: true, IsPrivate: true, IsMember: true})
	s.PutConv(slack.Conversation{ID: anStub, Name: anStub, IsGroup: true, IsPrivate: true, IsMember: true})
	s.PutConv(slack.Conversation{ID: anPublic, Name: "launch-notes", IsChannel: true, IsMember: true})
	s.PutConv(slack.Conversation{ID: "D1", IsIM: true, User: "U1", IsMember: true})
	app = core.New(c, s)
	if u, ok := s.User("USELF"); ok {
		app.S.Self = *u
	}
	app.ServeOffline()
	return app, func(ch string) int {
		mu.Lock()
		defer mu.Unlock()
		return n[ch]
	}, release
}

// drawActRows draws every row of the page in v's density.
func drawActRows(t *testing.T, v *Activity) {
	t.Helper()
	var ops op.Ops
	gtx := actFrame(&ops, image.Pt(1000, 400))
	page := actPage(v)
	for i := range page.Items {
		g := gtx
		g.Constraints = layout.Constraints{Min: image.Pt(1000, 0), Max: image.Pt(1000, 400)}
		if h := v.row(g, &page.Items[i], i).Size.Y; h <= 0 {
			t.Errorf("%s: an empty row", page.Items[i].Key)
		}
	}
}

func TestActivityChannelNamesNeverIDs(t *testing.T) {
	app, calls, release := actNamesApp(t)
	v := NewActivity(&slickHost{app: app, th: theme.New(false)})
	e := &vaEnv{size: image.Pt(1200, 900)}
	e.draw = func(gtx layout.Context) { v.Layout(gtx) }
	e.until(t, "the feed", func() bool { return len(actPage(v).Items) == 7 })
	e.frame()
	row := func(key string) actDesc { return v.rowFor(key).d }

	// a private channel the store has: lock-filled, then its bare name
	if d := row("k-priv-known"); d.title != "secret-plans" || d.foot != "Mention in secret-plans" || !d.ft.ent || d.ft.nameIcon != "lock-filled" ||
		d.loc != "secret-plans" || !d.locEnt || d.locIcon != "lock-filled" {
		t.Errorf("private: title %q, footer %q (%q, entity %v), location %q (%q), Slack: Mention in [lock-filled] secret-plans",
			d.title, d.foot, d.ft.nameIcon, d.ft.ent, d.loc, d.locIcon)
	}
	// its invite: "invited you to <#C|label>", getDisplayNameForChannel's bare name
	if d := row("k-priv-invite"); d.body != "invited you to secret-plans" {
		t.Errorf("invite body %q, Slack \"invited you to secret-plans\"", d.body)
	}
	// a public channel: the hash (as text) and the name, no icon
	if d := row("k-public"); d.title != "#launch-notes" || d.foot != "Mention in #launch-notes" || d.ft.nameIcon != "" || d.loc != "#launch-notes" {
		t.Errorf("public: %q / %q (%q) / %q", d.title, d.foot, d.ft.nameIcon, d.loc)
	}
	// a DM is no channel entity
	if d := row("k-dm"); d.entOn || d.foot != "Direct message" {
		t.Errorf("DM: footer %q, entity %v", d.foot, d.entOn)
	}

	// a channel the store doesn't have: the unknown stub (the hash and
	// the loading bar, no text) while conversations.info runs
	if d := row("k-priv-later"); !d.ent.Loading() || d.title != "" || d.foot != "Mention in " || !d.ft.ent || !d.locEnt {
		t.Errorf("while the lookup runs: title %q, footer %q, entity %+v; want the loading bar", d.title, d.foot, d.ent)
	}
	drawActRows(t, v)
	v.dense, v.dirty = true, true
	e.frame()
	drawActRows(t, v)
	v.dense, v.dirty = false, true
	release()
	// the row is described again once the name is in ("names")
	e.until(t, "the looked-up channel's name", func() bool { return row("k-priv-later").title == "later-plans" })
	if d := row("k-priv-later"); d.foot != "Mention in later-plans" || d.ft.nameIcon != "lock-filled" || d.locIcon != "lock-filled" || d.ent.Loading() {
		t.Errorf("after the lookup: footer %q (%q), location icon %q", d.foot, d.ft.nameIcon, d.locIcon)
	}
	// a stub named by its id is looked up the same way
	e.until(t, "the stub's name", func() bool { return row("k-stub").title == "stub-plans" })
	if d := row("k-stub"); d.ft.nameIcon != "lock-filled" {
		t.Errorf("stub: icon %q", d.ft.nameIcon)
	}
	// conversations.info doesn't know it: the Private channel chip
	e.until(t, "the Private channel chip", func() bool { return row("k-gone").ent.Chip })
	if d := row("k-gone"); d.title != "Private channel" || d.foot != "Mention in Private channel" || d.ft.nameIcon != "lock" || d.loc != "Private channel" {
		t.Errorf("gone: title %q, footer %q (%q), location %q", d.title, d.foot, d.ft.nameIcon, d.loc)
	}
	drawActRows(t, v)

	// no row shows an id, nor a "#" before a private channel
	page := actPage(v)
	for i := range page.Items {
		it := &page.Items[i]
		d := row(actKey(it))
		for _, s := range []string{d.title, d.foot, d.loc, d.body, d.prev} {
			if it.Channel != "" && strings.Contains(s, it.Channel) {
				t.Errorf("%s shows its channel's id: %q", it.Key, s)
			}
			if strings.Contains(s, "#secret-plans") || strings.Contains(s, "#later-plans") || strings.Contains(s, "#stub-plans") {
				t.Errorf("%s: %q puts a # before a private channel", it.Key, s)
			}
		}
	}
	// one lookup per channel the store couldn't name, none for the rest
	for ch, want := range map[string]int{anPrivLater: 1, anStub: 1, anGone: 1, anPrivKnown: 0, anPublic: 0} {
		if got := calls(ch); got != want {
			t.Errorf("%d conversations.info calls for %s, want %d", got, ch, want)
		}
	}
}

// convTitle, which other views name conversations with, is
// getDisplayNameForChannel's text: never the id, no "#" before a private
// channel, formatChannelName's "unknown-channel" while a channel loads.
func TestConvTitleNeverID(t *testing.T) {
	app, _, release := actNamesApp(t)
	b := NewActivity(&slickHost{app: app, th: theme.New(false)})
	if got := b.convTitle(anPrivKnown, nil); got != "secret-plans" {
		t.Errorf("private: %q, want secret-plans", got)
	}
	if got := b.convTitle(anPublic, nil); got != "#launch-notes" {
		t.Errorf("public: %q, want #launch-notes", got)
	}
	if got := b.convTitle(anPrivLater, nil); got != "unknown-channel" {
		t.Errorf("loading: %q, want unknown-channel", got)
	}
	release()
	deadline := time.Now().Add(10 * time.Second)
	for b.convTitle(anPrivLater, nil) != "later-plans" {
		if time.Now().After(deadline) {
			t.Fatalf("convTitle stayed %q after the lookup", b.convTitle(anPrivLater, nil))
		}
		time.Sleep(5 * time.Millisecond)
	}
	if got := b.convTitle("D1", nil); got != "Alice Example" && got != "alice" {
		t.Errorf("DM: %q", got)
	}
}

// TestActivityChannelNamesShots (ACTIVITY_SHOTS_DIR, the activity-shots
// workflow) saves the feed's rows while a channel loads and once every
// channel is in, Detailed and Dense, light and dark:
// activity-names-{loading,named}-{detailed,dense}-{light,dark}.png.
func TestActivityChannelNamesShots(t *testing.T) {
	dir := os.Getenv("ACTIVITY_SHOTS_DIR")
	if dir == "" {
		t.Skip("ACTIVITY_SHOTS_DIR not set")
	}
	for _, dark := range []bool{false, true} {
		mode := map[bool]string{false: "light", true: "dark"}[dark]
		app, _, release := actNamesApp(t)
		v := NewActivity(&slickHost{app: app, th: theme.New(dark)})
		e := &vaEnv{size: image.Pt(1200, 900)}
		e.draw = func(gtx layout.Context) { v.Layout(gtx) }
		e.until(t, "the feed", func() bool { return len(actPage(v).Items) == 7 })
		e.frame()
		shoot := func(state string) {
			for _, dense := range []bool{false, true} {
				v.dense, v.dirty = dense, true
				e.frame()
				name := "activity-names-" + state + "-" + map[bool]string{false: "detailed", true: "dense"}[dense] + "-" + mode + ".png"
				saveActRows(t, v, filepath.Join(dir, name))
			}
		}
		shoot("loading")
		release()
		e.until(t, "every channel", func() bool {
			return v.rowFor("k-priv-later").d.title == "later-plans" && v.rowFor("k-stub").d.title == "stub-plans" && v.rowFor("k-gone").d.ent.Chip
		})
		shoot("named")
	}
}

// saveActRows draws the page's rows one under another, 1100px wide, into
// a PNG at path.
func saveActRows(t *testing.T, v *Activity, path string) {
	t.Helper()
	const w = 1100
	var ops op.Ops
	gtx := actFrame(&ops, image.Pt(w, 4000))
	page := actPage(v)
	y := 0
	for i := range page.Items {
		func() {
			defer op.Offset(image.Pt(0, y)).Push(gtx.Ops).Pop()
			g := gtx
			g.Constraints = layout.Constraints{Min: image.Pt(w, 0), Max: image.Pt(w, 400)}
			y += v.row(g, &page.Items[i], i).Size.Y
		}()
	}
	win, err := pixeltest.NewWindow(w, y)
	if err != nil {
		t.Fatalf("headless window: %v", err)
	}
	defer win.Release()
	if err := win.Frame(&ops); err != nil {
		t.Fatalf("frame: %v", err)
	}
	img := image.NewRGBA(image.Rect(0, 0, w, y))
	if err := win.Screenshot(img); err != nil {
		t.Fatalf("screenshot: %v", err)
	}
	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	if err := png.Encode(f, img); err != nil {
		t.Fatal(err)
	}
}
