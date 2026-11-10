package messages

import (
	"fmt"
	"image"
	"image/png"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
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

	"github.com/some-du6e/clack/internal/core"
	"github.com/some-du6e/clack/internal/fakeslack"
	"github.com/some-du6e/clack/internal/imgcache"
	"github.com/some-du6e/clack/internal/slack"
	"github.com/some-du6e/clack/internal/store"
	"github.com/some-du6e/clack/internal/ui/slackc"
	"github.com/some-du6e/clack/internal/ui/theme"
	"github.com/some-du6e/clack/internal/ui/theme/pixeltest"
)

// flingEnv is a channel view over fakeslack's generated history (rich
// text, images, reactions, thread roots; pages of 100 older messages)
// drawn through a real input.Router.
type flingEnv struct {
	t     *testing.T
	v     *ChannelView
	r     input.Router
	ops   *op.Ops
	now   time.Time
	size  image.Point
	th    *theme.Theme
	scale float32
	shots string // CLACK_FLING_SHOTS: a PNG per frame
	nshot int
}

func newFlingEnv(t *testing.T, ch string, scale float32) *flingEnv {
	return newFlingEnvHook(t, ch, scale, nil)
}

// newFlingEnvHook is newFlingEnv with hook answering API calls first.
func newFlingEnvHook(t *testing.T, ch string, scale float32, hook func(method string, form url.Values) (any, bool)) *flingEnv {
	t.Helper()
	SetTaut(nil)
	sz := fakeslack.Small
	sz.History = 450
	sz.Files = true
	fs, err := fakeslack.Start(sz)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { fs.Close() })
	if hook != nil {
		fs.SetHook(hook)
	}
	c := slack.New(fs.Creds())
	c.EdgeURL = fs.URL + "edge/"
	s := store.New("")
	s.Lock()
	s.TeamID, s.TeamName = fakeslack.TeamID, "Fake Club"
	s.Unlock()
	self := slack.User{ID: fakeslack.SelfID, TeamID: fakeslack.TeamID, Name: "me", Profile: slack.Profile{DisplayName: "me"}}
	s.PutUsers([]slack.User{self})
	s.PutConv(slack.Conversation{ID: ch, Name: "fling", IsChannel: true, IsMember: true, NumMembers: 40})
	app := core.New(c, s)
	app.S.Self = self
	app.ServeOffline()
	th := theme.New(false)
	h := identityHost{coreHost: coreHost{app: app, th: th}, imgs: imgcache.New(fs.Fetch, "", nil)}
	e := &flingEnv{t: t, ops: new(op.Ops), now: time.Now(), size: image.Pt(int(900*scale), int(700*scale)), th: th, scale: scale}
	e.v = NewChannelView(h, ch)
	e.v.Preview = true
	e.shots = os.Getenv("CLACK_FLING_SHOTS")
	return e
}

func (e *flingEnv) frame() {
	e.ops.Reset()
	e.now = e.now.Add(16 * time.Millisecond)
	paint.FillShape(e.ops, e.th.C.Bg, clip.Rect{Max: e.size}.Op())
	gtx := layout.Context{Ops: e.ops, Metric: unit.Metric{PxPerDp: e.scale, PxPerSp: e.scale}, Constraints: layout.Exact(e.size), Now: e.now, Source: e.r.Source()}
	e.v.Layout(gtx)
	e.r.Frame(e.ops)
	if e.shots != "" && e.nshot < 400 {
		img := pixeltest.Render(e.t, e.size.X, e.size.Y, e.ops)
		f, err := os.Create(filepath.Join(e.shots, fmt.Sprintf("f%04d.png", e.nshot)))
		if err == nil {
			_ = png.Encode(f, img)
			f.Close()
		}
		e.nshot++
	}
}

// wheel queues n wheel events of dy px each (negative: up) over the list.
func (e *flingEnv) wheel(n int, dy float32) {
	p := f32.Pt(float32(e.size.X)/2, float32(e.size.Y)/2)
	evs := []any{pointer.Event{Kind: pointer.Move, Source: pointer.Mouse, Position: p, Time: time.Since(time.Time{})}}
	for i := 0; i < n; i++ {
		evs = append(evs, pointer.Event{Kind: pointer.Scroll, Source: pointer.Mouse, Position: p, Scroll: f32.Pt(0, dy)})
	}
	for _, ev := range evs {
		e.r.Queue(ev.(pointer.Event))
	}
}

// fresh lays a new row for item i out with this frame's inputs and
// returns its height: what the list must have used.
func (e *flingEnv) fresh(i int) int {
	s := e.v.s
	it := s.items[i]
	en := &s.snap.msgs[it.idx]
	if len(it.group) > 0 {
		ge := s.groupEntry(s.rc.h, it.group)
		en = &ge
	}
	r := newListRow(en, s.thread)
	rc := s.rc
	rc.header, rc.hovered, rc.highlight, rc.parent = it.header, false, 0, false
	gtx := layout.Context{Ops: new(op.Ops), Metric: unit.Metric{PxPerDp: e.scale, PxPerSp: e.scale}, Constraints: layout.Constraints{Max: image.Pt(e.size.X, 1<<20)}, Now: e.now}
	gtx.Constraints.Min.X = e.size.X
	return r.layout(gtx, &rc).Size.Y
}

// check asserts the visible rows have the heights fresh rows have and
// tile the viewport: no gaps, no overlaps.
func (e *flingEnv) check(what string) int {
	e.t.Helper()
	s := e.v.s
	bad := 0
	for i := s.visible[0]; i < s.visible[1]; i++ {
		if i > s.visible[0] && s.pos[i] != s.pos[i-1]+s.heights[i-1] {
			e.t.Errorf("%s: item %d (%s) at %d, the one above ends at %d", what, i, s.items[i].key, s.pos[i], s.pos[i-1]+s.heights[i-1])
			bad++
		}
		if s.items[i].kind != itMsg {
			continue
		}
		if got, want := s.heights[i], e.fresh(i); got != want {
			e.t.Errorf("%s: item %d (%s, header %v) is %d tall, a fresh row %d", what, i, s.items[i].key, s.items[i].header, got, want)
			bad++
		}
	}
	return bad
}

// firstMsg is the first visible message's key and y.
func (e *flingEnv) firstMsg() (string, int) {
	s := e.v.s
	for i := s.visible[0]; i < s.visible[1]; i++ {
		if s.items[i].kind == itMsg && s.pos[i] >= 0 {
			return s.items[i].key, s.pos[i]
		}
	}
	return "", 0
}

func (e *flingEnv) yOf(key string) (int, bool) {
	s := e.v.s
	for i := s.visible[0]; i < s.visible[1]; i++ {
		if s.items[i].key == key {
			return s.pos[i], true
		}
	}
	return 0, false
}

// Flinging up through a long history (many big wheel events a frame,
// older pages arriving mid-fling, rows off screen releasing their layout
// as sweepRows does after 30 s) must leave rows at their real heights,
// stacked with nothing between them, and a page arriving must not move
// what is on screen. Then the way back down and jump to recent.
func TestFlingScrollNoGaps(t *testing.T) {
	for _, tc := range []struct {
		name  string
		ch    string
		scale float32
		css   string
	}{
		{"history", "C0FAKE00001", 1, ""},
		{"history@2x", "C0FAKE00002", 2, ""},
		{"blockkit", fakeslack.GalleryChannelID, 1, ""},
		{"files", fakeslack.FilesChannelID, 2, ""},
		// a theme's sheet: natives take their styles through the CSS cache
		{"theme-css", "C0FAKE00003", 2, flingCSS},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if tc.css != "" {
				slackc.SetStyle("test:fling", tc.css)
				t.Cleanup(func() { slackc.RemoveStyle("test:fling") })
			}
			flingThrough(t, newFlingEnv(t, tc.ch, tc.scale))
		})
	}
}

// flingCSS restyles message parts the way Slick themes do.
const flingCSS = `
.c-virtual_list__item { color: #222 }
.c-message_kit__background--hovered { background-color: #f0f0ff }
.c-message_kit__gutter__right { color: #333 }
.p-rich_text_section { color: #123456 }
.c-message__sender_button { color: #a00 }
.c-timestamp { color: #777 }
.c-reaction { background-color: #eef }
`

// release drops the layout of every row not drawn in the last frame, as
// sweepRows does to rows idle for 30 s.
func (e *flingEnv) release() {
	s := e.v.s
	on := map[string]bool{}
	for i := s.visible[0]; i < s.visible[1]; i++ {
		on[s.items[i].key] = true
	}
	for k, r := range s.rows {
		if !on[k] && r.rv != nil {
			r.rv.Release()
		}
	}
}

func flingThrough(t *testing.T, e *flingEnv) {
	until(t, 10*time.Second, "the channel", e.frame, func() bool { return e.v.s.snap.loaded && len(e.v.s.items) > 3 })
	for i := 0; i < 5; i++ {
		e.frame()
	}
	e.check("opened")
	bad := 0
	pages := 0
	step := float32(-250) * e.scale
	deadline := time.Now().Add(20 * time.Second)
	for f := 0; time.Now().Before(deadline) && bad < 20; f++ {
		n0 := len(e.v.s.snap.msgs)
		key, y := e.firstMsg()
		if f%3 == 2 {
			// an idle frame (a page may land): nothing on screen moves
			e.frame()
			if len(e.v.s.snap.msgs) != n0 {
				pages++
			}
			if ny, ok := e.yOf(key); key != "" && ok && ny != y {
				t.Errorf("frame %d: %s moved from %d to %d with no scroll (msgs %d → %d)", f, key, y, ny, n0, len(e.v.s.snap.msgs))
				bad++
			}
		} else {
			e.wheel(6, step)
			e.frame()
			if len(e.v.s.snap.msgs) != n0 {
				pages++
			}
		}
		bad += e.check("fling up, frame " + itoa(f))
		if f%25 == 24 {
			e.release()
		}
		p := e.v.s.list.Position
		if p.First == 0 && p.Offset == 0 {
			if !e.v.s.snap.hasMore {
				break
			}
			time.Sleep(50 * time.Millisecond) // at the top: wait for the page
		}
	}
	t.Logf("fling up: %d messages, %d pages arrived", len(e.v.s.snap.msgs), pages)
	if e.v.s.snap.hasMore {
		t.Errorf("never reached the start of the channel (%d messages)", len(e.v.s.snap.msgs))
	}
	// fast back down
	for f := 0; f < 600 && bad < 40; f++ {
		e.wheel(6, -step)
		e.frame()
		bad += e.check("fling down, frame " + itoa(f))
		if f%25 == 24 {
			e.release()
		}
		if !e.v.s.list.Position.BeforeEnd {
			break
		}
	}
	if e.v.s.list.Position.BeforeEnd {
		t.Errorf("fling down never reached the newest message")
	}
	// up a little, then jump to recent
	e.wheel(4, step)
	e.frame()
	e.frame()
	if !e.v.s.list.Position.BeforeEnd {
		t.Fatalf("not scrolled up")
	}
	e.v.s.list.Position = layout.Position{}
	e.frame()
	e.frame()
	e.check("jumped to recent")
	n := len(e.v.s.items) - 1
	if y, ok := e.yOf(e.v.s.items[n].key); !ok || y+e.v.s.heights[n] != e.size.Y {
		t.Errorf("jump to recent: the newest row at %d (shown %v), want its bottom at %d", y, ok, e.size.Y)
	}
}

// A page of older history arriving while the first row on screen is a
// join summary ("A and B have joined") keeps that row where it was, also
// when the page ends with more joins and the summary grows in front (its
// key, "g"+its first ts, changes).
func TestPrependKeepsJoinSummaryInPlace(t *testing.T) {
	const ch = "C0JOINS001"
	base := int64(1762600000)
	ts := func(i int) string { return fmt.Sprintf("%d.%06d", base+int64(i)*60, 100) }
	var all []map[string]any // oldest first
	for i := 0; i < 60; i++ {
		m := map[string]any{"type": "message", "ts": ts(i), "user": fmt.Sprintf("U0FAKE%05d", 1+i%7), "text": fmt.Sprintf("synthetic %d", i)}
		if i >= 36 && i < 44 { // joins across the page boundary (newest page: 40..59)
			m["subtype"], m["text"] = "channel_join", "has joined the channel"
		}
		all = append(all, m)
	}
	hook := func(method string, f url.Values) (any, bool) {
		if method != "conversations.history" || f.Get("channel") != ch {
			return nil, false
		}
		end := len(all)
		if l := f.Get("latest"); l != "" {
			for end > 0 && all[end-1]["ts"].(string) >= l {
				end--
			}
		}
		lim := 20
		start := max(0, end-lim)
		var page []map[string]any
		for i := end - 1; i >= start; i-- { // newest first, like Slack
			page = append(page, all[i])
		}
		return map[string]any{"ok": true, "messages": page, "has_more": start > 0}, true
	}
	e := newFlingEnvHook(t, ch, 1, hook)
	e.size = image.Pt(900, 400)
	until(t, 10*time.Second, "the channel", e.frame, func() bool { return e.v.s.snap.loaded && len(e.v.s.snap.msgs) >= 20 })
	key := "g" + ts(40)
	s := e.v.s
	i := s.anchorItem(key)
	if i < 0 {
		t.Fatalf("no join summary %s in %d items", key, len(s.items))
	}
	// bring the summary to the top of the view (and keep older pages from
	// loading until it's there)
	s.lastOlder = time.Now().Add(time.Hour)
	s.list.Position = layout.Position{BeforeEnd: true, First: i, Offset: -10}
	e.frame()
	e.frame()
	k0, y0 := e.firstMsg()
	if k0 != key {
		t.Fatalf("first row %s, want the join summary %s", k0, key)
	}
	s.lastOlder = time.Time{}
	until(t, 10*time.Second, "the older page", e.frame, func() bool { return len(s.snap.msgs) >= 40 })
	e.frame()
	j := -1
	for i := s.visible[0]; i < s.visible[1]; i++ {
		for _, k := range s.items[i].group {
			if s.snap.msgs[k].m.TS == ts(40) {
				j = i
			}
		}
	}
	if j < 0 {
		t.Fatalf("the join summary went off screen after the page arrived (first row now %v)", s.items[s.visible[0]].key)
	}
	if s.pos[j] != y0 {
		t.Errorf("join summary moved from %d to %d when the older page arrived", y0, s.pos[j])
	}
	if s.items[j].key == key {
		t.Errorf("the summary's key didn't change (%s): the test no longer covers a grown summary", key)
	}
}

// The page of older history arriving at every point of a fling, one run
// each (it used to land wherever the network put it, and a header row was
// drawn 22px too tall for a frame now and then, CI run 42663172766): the
// request is held until frame k of the fling, then answered.
func TestFlingPageArrivalEveryFrame(t *testing.T) {
	for k := 0; k < 12; k++ {
		t.Run("frame"+itoa(k), func(t *testing.T) {
			gate := make(chan struct{})
			var held atomic.Bool
			hook := func(method string, f url.Values) (any, bool) {
				if method == "conversations.history" && f.Get("latest") != "" {
					held.Store(true)
					<-gate
				}
				return nil, false
			}
			e := newFlingEnvHook(t, "C0FAKE00001", 1, hook)
			t.Cleanup(func() {
				select {
				case <-gate:
				default:
					close(gate)
				}
			})
			until(t, 10*time.Second, "the channel", e.frame, func() bool { return e.v.s.snap.loaded && len(e.v.s.items) > 3 })
			for i := 0; i < 30; i++ { // users, avatars and images settle
				e.frame()
				time.Sleep(5 * time.Millisecond)
			}
			e.check("opened")
			step := float32(-250)
			f, since := 0, -1
			n0 := len(e.v.s.snap.msgs)
			for ; f < 300 && len(e.v.s.snap.msgs) == n0; f++ {
				if held.Load() && since < 0 {
					since = f
				}
				if since >= 0 && f-since == k {
					close(gate)
				}
				e.wheel(3, step)
				e.frame()
				if since >= 0 && f-since > k {
					time.Sleep(2 * time.Millisecond) // the page is on its way
				}
				if e.check("fling up, frame "+itoa(f)) > 0 {
					e.diag()
					return
				}
			}
			if len(e.v.s.snap.msgs) == n0 {
				t.Fatalf("no page arrived (held %v since frame %d)", held.Load(), since)
			}
			// frames after the page: still flinging, then idle
			for i := 0; i < 20; i++ {
				key, y := e.firstMsg()
				if i%2 == 0 {
					e.wheel(3, step)
				}
				e.frame()
				if i%2 == 1 {
					if ny, ok := e.yOf(key); key != "" && ok && ny != y {
						t.Errorf("after the page, frame %d: %s moved from %d to %d with no scroll", i, key, y, ny)
					}
				}
				if e.check("after the page, frame "+itoa(i)) > 0 {
					e.diag()
					return
				}
			}
		})
	}
}

// diag logs a mismatching row's parts, cached and fresh.
func (e *flingEnv) diag() {
	s := e.v.s
	for i := s.visible[0]; i < s.visible[1]; i++ {
		it := s.items[i]
		if it.kind != itMsg {
			continue
		}
		want := e.fresh(i)
		if s.heights[i] == want {
			continue
		}
		r := s.rows[it.key]
		en := &s.snap.msgs[it.idx]
		f := newListRow(en, s.thread)
		rc := s.rc
		rc.header, rc.hovered, rc.highlight, rc.parent = it.header, false, 0, false
		gtx := layout.Context{Ops: new(op.Ops), Metric: unit.Metric{PxPerDp: e.scale, PxPerSp: e.scale}, Constraints: layout.Constraints{Min: image.Pt(e.size.X, 0), Max: image.Pt(e.size.X, 1<<20)}, Now: e.now}
		f.layout(gtx, &rc)
		e.t.Logf("item %d %s: cached %d col %v body %v name %q nameBL %d plain %d | fresh %d col %v body %v name %q nameBL %d plain %d | hovered %v reactions %d replies %d",
			i, it.key, s.heights[i], r.col, r.bodyRect, r.name, r.nameBL, r.rv.PlainLen(), want, f.col, f.bodyRect, f.name, f.nameBL, f.rv.PlainLen(),
			i == s.hoverIdx, len(r.m.Reactions), r.m.ReplyCount)
	}
}

// A channel name looked up in the background (a mention of a channel the
// store doesn't know) shows in every row from the same frame, and the row
// cut off at the top growing or shrinking with it doesn't move what's
// under it. The lookup is done by the test, around frames: storing the
// name and announcing it are two steps, and a frame drawn between them
// used to show it in the rows laid out after the store and not in those
// before (a row a line too tall for a frame, then the list jumped: CI run
// 42663172766, which got there by timing).
func TestChannelNameArrivesBetweenFrames(t *testing.T) {
	chanMu.Lock()
	saved := chanNames
	chanNames = map[string]chanEntry{} // other tests resolved fakeslack's channels already
	chanMu.Unlock()
	t.Cleanup(func() { chanMu.Lock(); chanNames = saved; chanMu.Unlock() })
	hold := make(chan struct{})
	t.Cleanup(func() { close(hold) })
	hook := func(method string, f url.Values) (any, bool) {
		if method == "conversations.info" {
			<-hold // names arrive only as the test announces them
		}
		return nil, false
	}
	e := newFlingEnvHook(t, "C0FAKE00001", 1, hook)
	until(t, 10*time.Second, "the channel", e.frame, func() bool { return e.v.s.snap.loaded && len(e.v.s.items) > 3 })
	s := e.v.s
	// scroll up until the row cut off at the top mentions a channel whose
	// name is still being looked up
	pendingAt := func() (string, string) {
		p := s.list.Position
		if !p.BeforeEnd || p.Offset <= 0 || p.First >= len(s.items) || s.items[p.First].kind != itMsg {
			return "", ""
		}
		r := s.rows[s.items[p.First].key]
		if r == nil {
			return "", ""
		}
		for _, b := range r.m.Doc.Blocks {
			for _, sp := range b.Spans {
				if rawChannel(&sp) {
					chanMu.Lock()
					en, asked := chanNames[sp.ID]
					chanMu.Unlock()
					if asked && en.name == "" {
						return s.items[p.First].key, sp.ID
					}
				}
			}
		}
		return "", ""
	}
	key, id := "", ""
	for f := 0; f < 400 && key == ""; f++ {
		e.wheel(1, -37)
		e.frame()
		e.frame()
		key, id = pendingAt()
	}
	if key == "" {
		t.Fatal("no row with a pending channel name came to the top")
	}
	for i := 0; i < 3; i++ {
		e.frame()
	}
	e.check("before the name")
	akey, ay := e.firstMsg()
	h0 := itemHeight(s, key)
	moved := func(when string) {
		if y, ok := e.yOf(akey); !ok || y != ay {
			t.Errorf("%s: %s, first on screen, moved from %d to %d (shown %v)", when, akey, ay, y, ok)
		}
	}
	// the lookup's two steps with a frame between them
	hb := s.hb
	hb.mu.Lock()
	at := hb.userRv + 1
	hb.mu.Unlock()
	name := strings.Repeat("a-channel-name-far-longer-than-its-id-", 6) + id // wraps onto more lines
	chanMu.Lock()
	chanNames[id] = chanEntry{name: name, at: at}
	chanMu.Unlock()
	e.frame()
	e.check("name stored, not announced")
	moved("name stored, not announced")
	hb.mu.Lock()
	hb.userRv++
	hb.mu.Unlock()
	for i := 0; i < 3; i++ {
		e.frame()
		e.check("name announced, frame " + itoa(i))
		moved("name announced, frame " + itoa(i))
	}
	if r := s.rows[key]; r == nil || !strings.Contains(r.rv.TextRange(0, r.rv.PlainLen()), name) {
		t.Errorf("the top row doesn't show the announced name")
	}
	if h1 := itemHeight(s, key); h1 == h0 {
		t.Errorf("the top row stayed %dpx with the long name: the test no longer moves anything above the anchor", h0)
	} else {
		t.Logf("top row %s: %d → %d px with #%s's name", key, h0, h1, id)
	}
}
