package views

// Every Activity item type through the real view, from the synthetic feed
// in internal/core/testdata (made-up ids and text, a fake Web API, no
// network). ACTIVITY_SHOTS_DIR set (the activity-shots workflow) also
// saves screenshots of the feed.

import (
	"image"
	"image/png"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"gioui.org/layout"
	"gioui.org/op"
	"gioui.org/unit"

	"github.com/some-du6e/clack/internal/core"
	"github.com/some-du6e/clack/internal/slack"
	"github.com/some-du6e/clack/internal/store"
	"github.com/some-du6e/clack/internal/ui/theme"
	"github.com/some-du6e/clack/internal/ui/theme/pixeltest"
)

// actViewsDensity is the layout the fake activity.views answers.
var actViewsDensity = "compact"

// actFeedLen is the number of entries in testdata/activity_feed.json.
const actFeedLen = 38

func actFeedApp(t *testing.T) *core.App { return actFeedAppWith(t, nil) }

// actFeedAppWith is actFeedApp with rec seeing every API call (method and
// form) before it's answered.
func actFeedAppWith(t *testing.T, rec func(method string, form url.Values)) *core.App {
	t.Helper()
	feed, err := os.ReadFile("../../core/testdata/activity_feed.json")
	if err != nil {
		t.Fatal(err)
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		method := strings.TrimPrefix(r.URL.Path, "/api/")
		if rec != nil {
			if r.ParseMultipartForm(1<<20) != nil {
				_ = r.ParseForm()
			}
			rec(method, r.Form)
		}
		switch method {
		case "activity.feed":
			_, _ = w.Write(feed)
		case "messages.list":
			_, _ = w.Write([]byte(`{"ok":true,"messages":{}}`)) // k-channel-gone stays unhydrated
		case "activity.views": // the saved layout (fetchActivityViews: prefs.density)
			_, _ = w.Write([]byte(`{"ok":true,"views":[{"id":"V1","view_type":"all","density":"` + actViewsDensity +
				`"}],"prefs":{"sort":"newest","density":"` + actViewsDensity + `"}}`))
		default:
			_, _ = w.Write([]byte(`{"ok":true}`))
		}
	}))
	t.Cleanup(srv.Close)
	c := slack.New(slack.Creds{Token: "xoxc-test", Cookie: "xoxd-test", UserID: "USELF", Team: "T1", EnterpriseID: "E1",
		TeamURL: srv.URL + "/", EnterpriseURL: srv.URL + "/"})
	c.EdgeURL = srv.URL + "/edge/"
	s := store.New("")
	s.Lock()
	s.TeamID, s.TeamName = "T1", "Synthetic Club"
	s.Unlock()
	s.PutUsers([]slack.User{
		{ID: "USELF", Name: "self", RealName: "Test Self"},
		{ID: "U1", Name: "alice", RealName: "Alice Example"},
		{ID: "U2", Name: "bob", RealName: "Bob Example"},
		{ID: "U3", Name: "carol", RealName: "Carol Example"},
		{ID: "UB", Name: "synthbot", IsBot: true},
	})
	s.PutConv(slack.Conversation{ID: "C1", Name: "general", IsChannel: true, IsMember: true, NumMembers: 3})
	s.PutConv(slack.Conversation{ID: "C2", Name: "random", IsChannel: true, NumMembers: 3})
	s.PutConv(slack.Conversation{ID: "D1", IsIM: true, User: "U1", IsMember: true})
	s.PutConv(slack.Conversation{ID: "G1", IsMPIM: true, Name: "mpdm-self--alice--bob-1", IsMember: true, Members: []string{"USELF", "U1", "U2"}})
	m := func(ts, user, text string) slack.Message {
		return slack.Message{Type: "message", TS: ts, User: user, Text: text}
	}
	root := m("1700000110.000100", "USELF", "a synthetic thread")
	root.ThreadTS, root.ReplyCount, root.ReplyUsers = root.TS, 2, []string{"U2", "U1"}
	reply := m("1700000111.000100", "U2", "a synthetic reply")
	reply.ThreadTS = root.TS
	// several people reacted to this one (ReactionSender's "& others")
	liked := m("1700000132.000100", "USELF", "a synthetic message people liked")
	liked.Reactions = []slack.Reaction{{Name: "tada", Count: 3, Users: []string{"U2", "U1", "USELF"}}}
	// a thread with three other participants (ThreadSender's "& 2 others")
	root2 := m("1700000134.000100", "USELF", "a busy synthetic thread")
	root2.ThreadTS, root2.ReplyCount, root2.ReplyUsers = root2.TS, 3, []string{"U2", "U1", "U3"}
	reply2 := m("1700000135.000100", "U2", "a busy synthetic reply")
	reply2.ThreadTS = root2.TS
	joined := m("1700000120.000100", "U2", "<@U2> has joined the channel")
	joined.Subtype = "channel_join"
	s.SetLatest("C1", []slack.Message{
		m("1700000101.000100", "U2", "just a synthetic post in general"),
		m("1700000102.000100", "U1", "another synthetic post"),
		m("1700000105.000100", "U1", "<@USELF> a synthetic mention"),
		m("1700000106.000100", "U2", "<!channel> synthetic heads-up"),
		m("1700000107.000100", "U2", "<!everyone> synthetic news"),
		m("1700000108.000100", "U1", "<!subteam^S1> synthetic group ping"),
		m("1700000109.000100", "U2", "a synthetic keyword"),
		root, reply,
		m("1700000117.000100", "U1", "<@USELF> please review this synthetic change"),
		joined,
		m("1700000131.000100", "U2", "something new and synthetic"),
		liked, root2, reply2,
		m("1700000142.000100", "USELF", "<!subteam^S1> my own synthetic ping"),
	}, false)
	s.SetThread("C1", root.TS, []slack.Message{root, reply})
	s.SetThread("C1", root2.TS, []slack.Message{root2, reply2})
	s.SetLatest("G1", []slack.Message{m("1700000137.000100", "U1", "a synthetic group hello")}, false)
	s.SetLatest("C2", []slack.Message{
		m("1700000114.000100", "U1", "<@USELF> join us in random"),
		m("1700000116.000100", "U2", "<@USELF> over here in random"),
	}, false)
	bot := m("1700000113.000100", "UB", "a synthetic app message")
	bot.BotID = "B1"
	s.SetLatest("D1", []slack.Message{m("1700000104.000100", "U1", "synthetic hi"), bot}, false)
	app := core.New(c, s)
	if u, ok := s.User("USELF"); ok {
		app.S.Self = *u
	}
	app.ServeOffline()
	return app
}

func actFrame(ops *op.Ops, sz image.Point) layout.Context {
	ops.Reset()
	return layout.Context{Ops: ops, Metric: unit.Metric{PxPerDp: 1, PxPerSp: 1}, Constraints: layout.Exact(sz), Now: time.Now()}
}

// TestActivityEveryItemTypeRenders: no item type draws an empty row (a
// bare avatar square and a time). The "all new posts" channel item was
// one (Karim, 2026-11-08: a plain message in a channel set to every
// message).
func TestActivityEveryItemTypeRenders(t *testing.T) {
	app := actFeedApp(t)
	h := &slickHost{app: app, th: theme.New(false)}
	v := NewActivity(h)
	var ops op.Ops
	sz := image.Pt(1200, 800)
	deadline := time.Now().Add(10 * time.Second)
	for len(actPage(v).Items) < actFeedLen {
		if time.Now().After(deadline) {
			t.Fatalf("feed never loaded (%d items)", len(actPage(v).Items))
		}
		v.Layout(actFrame(&ops, sz))
		time.Sleep(10 * time.Millisecond)
	}
	page := actPage(v)
	// Slack's strings for each row (activity_slack.go cites the modules;
	// gantry boot-render's ActivityRowContentMultiLine / SingleLine with the
	// layout revisions on): the sender, the Detailed footer's text and its
	// 16px type icon, the Dense row's 20px type icon, the text drawn
	// instead of a message, and the subtext before the message.
	type want struct{ name, foot, icon, dense, body, sub string }
	cases := map[string]want{
		"k-channel":         {"Bob Example", "#general", "", "channel", "", ""},
		"k-channel-many":    {"Alice Example", "#general", "", "channel", "", ""},
		"k-channel-gone":    {"", "#general", "", "channel", "", ""},
		"k-dm":              {"Alice Example", "Direct message", "", "direct-messages", "", ""},
		"k-at-user":         {"Alice Example", "Mention in #general", "mentions", "mentions", "", ""},
		"k-at-channel":      {"Bob Example", "Mention in #general", "mentions", "mentions", "", ""},
		"k-at-everyone":     {"Bob Example", "Mention in #general", "mentions", "mentions", "", ""},
		"k-at-group":        {"Alice Example", "Mention in #general", "mentions", "mentions", "", ""},
		"k-keyword":         {"Bob Example", "Mention in #general", "mentions", "mentions", "", ""},
		"k-thread":          {"Bob Example +1", "Thread in #general", "threads", "threads", "", ""},
		"k-thread-many":     {"Bob Example +2", "Thread in #general", "threads", "threads", "", ""},
		"k-reaction":        {"Bob Example", "#general", "", "reaction", "", ""},
		"k-reaction-many":   {"Alice Example +1", "#general", "", "reaction", "", ""},
		"k-bot":             {"", "App", "", "apps", "", ""},
		"k-mpdm":            {"Alice Example", "Alice Example, Bob Example", "", "direct-messages", "", ""},
		"k-invite":          {"Alice Example", "Invitation", "paper-plane", "paper-plane", "invited you to #random", ""},
		"k-quiet":           {"Alice Example", "Invitation", "paper-plane", "paper-plane", "invited you to #random", ""},
		"k-unjoined":        {"Bob Example", "Mention in #random", "mentions", "mentions", "", ""},
		"k-ext-channel":     {"eve", "Invitation", "paper-plane", "paper-plane", "invited you to #partners", ""},
		"k-ext-dm":          {"Eve Example", "Invitation", "paper-plane", "paper-plane", "invited you to direct message", ""},
		"k-ext-revoked":     {"eve", "Invitation", "paper-plane", "paper-plane", "You received an invitation that has been ignored, expired, or is no longer available for another reason.", ""},
		"k-joiner":          {"Bob Example", "Workspace invitation", "paper-plane", "paper-plane", "Accepted your invitation to join Slack \u2014\u00a0take a second to say hello.", ""},
		"k-joiner-reminder": {"Bob Example", "Workspace invitation", "paper-plane", "paper-plane", "Joined your workspace. A welcome from you will help them get started.", ""},
		"k-prejoin":         {"Bob Example", "Workspace invitation", "", "notifications", "Was invited to join Synthetic Club \u2014 take a second to say hello.", ""},
		"k-reminder":        {"Reminder", "Reminder from #general", "bookmark", "bookmark", "", "Alice Example: "},
		"k-reminder-plain":  {"Reminder", "", "bookmark", "bookmark", "water the synthetic plants", ""},
		"k-reminder-file":   {"Reminder", "Reminder from ", "file", "file", "You set a reminder about this file", ""},
		"k-alert":           {"Slack", "", "", "notifications", "A synthetic channel was archived.", ""},
		"k-future":          {"Bob Example", "#general", "mentions", "mentions", "", ""},
		"k-mine":            {"", "Mention in #general", "mentions", "mentions", "", "You: "},
		// list items before their list loads (Hi+d ListItemName: "Untitled
		// item"; the footer's lists icon and an empty title)
		"k-list-assigned": {"Alice Example", "Mention in ", "mentions", "mentions", "Assigned to you: Untitled item", ""},
		"k-list-edited":   {"Bob Example", "Mention in ", "mentions", "mentions", "Edited Untitled item: ", ""},
		"k-list-mention":  {"Alice Example", "Mention in ", "mentions", "mentions", "@mention \u2022  \u2022 Untitled item", ""},
		"k-list-todo":     {"", "Mention in ", "mentions", "mentions", "List update: Untitled item", ""},
		"k-list-approval": {"", "", "mentions", "mentions", "", ""},
	}
	for i := range page.Items {
		it := &page.Items[i]
		key := actKey(it)
		d := v.describe(it, page.Convs)
		if w, ok := cases[key]; ok {
			if w.name != "" && d.name != w.name {
				t.Errorf("%s: name %q, want %q", key, d.name, w.name)
			}
			if d.foot != w.foot {
				t.Errorf("%s: footer %q, want %q", key, d.foot, w.foot)
			}
			if d.footIcon != w.icon {
				t.Errorf("%s: footer icon %q, want %q", key, d.footIcon, w.icon)
			}
			if d.typeIcon != w.dense {
				t.Errorf("%s: dense type icon %q, want %q", key, d.typeIcon, w.dense)
			}
			if d.body != w.body {
				t.Errorf("%s: body %q, want %q", key, d.body, w.body)
			}
			if d.subtext != w.sub {
				t.Errorf("%s: subtext %q, want %q", key, d.subtext, w.sub)
			}
		}
		if it.Msg == nil && !d.invite && it.Channel != "" && key != "k-channel-gone" && key != "k-future-bare" {
			t.Errorf("%s (%s): no message and no text in its place", key, it.Type)
		}
		if strings.HasPrefix(key, "k-list-") {
			if !d.pill {
				t.Errorf("%s: no context pill", key)
			}
			if key != "k-list-approval" && d.ft.nameIcon != "lists" {
				t.Errorf("%s: footer without the lists icon (%q)", key, d.ft.nameIcon)
			}
			// the to-do and approval entries name no one (hliv EM: the
			// message author): Slack's loading shimmer
			if (key == "k-list-todo" || key == "k-list-approval") && d.name != "" {
				t.Errorf("%s: sender %q, want none", key, d.name)
			}
		}
		if (key == "k-reaction" || key == "k-reaction-many") && !d.subReact {
			t.Errorf("%s: no Reacted subtext", key)
		}
		if (key == "k-ext-channel" || key == "k-reminder-plain") && d.locOn != (key == "k-ext-channel") {
			t.Errorf("%s: dense location shown %v", key, d.locOn)
		}
		if key == "k-dm" && (d.loc != "Direct message" || !d.locOn) {
			t.Errorf("%s: dense location %q", key, d.loc)
		}
		if (key == "k-channel" || key == "k-channel-many") && it.Msg == nil {
			t.Errorf("%s: the channel post wasn't hydrated", key)
		}
		gtx := actFrame(&ops, image.Pt(800, 400))
		gtx.Constraints = layout.Constraints{Min: image.Pt(800, 0), Max: image.Pt(800, 400)}
		if hgt := v.row(gtx, it, 1).Size.Y; hgt <= 0 {
			t.Errorf("%s (%s): row has no height", key, it.Type)
		}
	}
	if got := actUnreadCount(&core.ActivityItem{Type: "dm", Count: 2}); got != 2 {
		t.Errorf("dm unread count %d, want 2", got)
	}
	if got := actUnreadCount(&core.ActivityItem{Type: "at_user", Count: 4}); got != 1 {
		t.Errorf("mention unread count %d, want 1 (only bundles count messages)", got)
	}
	actShots(t, app)
}

// actShots saves the whole feed (Detailed and Dense, light and dark) when
// ACTIVITY_SHOTS_DIR is set.
func actShots(t *testing.T, app *core.App) {
	dir := os.Getenv("ACTIVITY_SHOTS_DIR")
	if dir == "" {
		return
	}
	for _, sh := range []struct {
		name        string
		dark, dense bool
		h           int
	}{
		{"activity-detailed-light", false, false, 4200},
		{"activity-detailed-dark", true, false, 4200},
		{"activity-dense-light", false, true, 1500},
		{"activity-dense-dark", true, true, 1500},
	} {
		sz := image.Pt(1100, sh.h)
		win, err := pixeltest.NewWindow(sz.X, sz.Y)
		if err != nil {
			t.Fatalf("headless window: %v", err)
		}
		v := NewActivity(&slickHost{app: app, th: theme.New(sh.dark)})
		v.dense = sh.dense
		var ops op.Ops
		deadline := time.Now().Add(10 * time.Second)
		for len(actPage(v).Items) < actFeedLen && time.Now().Before(deadline) {
			v.Layout(actFrame(&ops, sz))
			time.Sleep(10 * time.Millisecond)
		}
		for range 3 { // rows and images settle
			v.Layout(actFrame(&ops, sz))
		}
		if err := win.Frame(&ops); err != nil {
			t.Fatalf("frame: %v", err)
		}
		img := image.NewRGBA(image.Rectangle{Max: sz})
		if err := win.Screenshot(img); err != nil {
			t.Fatalf("screenshot: %v", err)
		}
		win.Release()
		f, err := os.Create(filepath.Join(dir, sh.name+".png"))
		if err != nil {
			t.Fatal(err)
		}
		if err := png.Encode(f, img); err != nil {
			t.Fatal(err)
		}
		_ = f.Close()
		t.Logf("saved %s", f.Name())
	}
}

// A reaction whose emoji has no image in the store (a custom one Clack
// doesn't know): Slack still shows the emoji. QTZf ReactionContent and
// ActivityDecoupledTypeIcon hand vbPY's Emoji ":name:" with
// renderEmojiFallback, which draws an <img> with no src and that alt
// text, clipped to the emoji box ("Reacted :ta").
func TestActivityReactionWithoutEmojiImage(t *testing.T) {
	app := actFeedApp(t)
	v := NewActivity(&slickHost{app: app, th: theme.New(false)})
	var ops op.Ops
	sz := image.Pt(1200, 800)
	deadline := time.Now().Add(10 * time.Second)
	for len(actPage(v).Items) < actFeedLen {
		if time.Now().After(deadline) {
			t.Fatalf("feed never loaded (%d items)", len(actPage(v).Items))
		}
		v.Layout(actFrame(&ops, sz))
		time.Sleep(10 * time.Millisecond)
	}
	page := actPage(v)
	known := actItem(t, v, "k-reaction")
	if known.Reaction == nil || known.Reaction.URL == "" {
		t.Fatalf("k-reaction's :tada: has no image: %+v", known.Reaction)
	}
	unknown := *known
	unknown.Reaction = &core.ActivityReaction{User: known.Reaction.User, Name: "tada_custom"}
	dk, du := v.describe(known, page.Convs), v.describe(&unknown, page.Convs)
	if !du.subReact || du.typeIcon != "reaction" || du.footEmoji != "" || du.footAlt != ":tada_custom:" {
		t.Fatalf("subReact %v type %q image %q alt %q, want the :tada_custom: alt text", du.subReact, du.typeIcon, du.footEmoji, du.footAlt)
	}
	icon := func(d *actDesc) image.Point {
		gtx := actFrame(&ops, sz)
		gtx.Constraints.Min = image.Point{}
		return v.actTypeIcon(gtx, d).Size
	}
	// the alt text takes the image's place and size, nothing wider
	if a, b := icon(&du), icon(&dk); a != b {
		t.Errorf("type icon with the alt text %v, with the image %v", a, b)
	}
	gtx := actFrame(&ops, sz)
	gtx.Constraints.Min = image.Point{}
	if d := v.emojiAlt(gtx, ":a_much_longer_custom_emoji_name:", 16); d.Size != image.Pt(16, 16) {
		t.Errorf("alt text box %v, want 16x16 (clipped)", d.Size)
	}
	if got := actEmojiText(":already_wrapped:"); got != ":already_wrapped:" {
		t.Errorf("actEmojiText(:already_wrapped:) = %q", got)
	}
}
