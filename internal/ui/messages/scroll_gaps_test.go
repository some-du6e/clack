package messages

import (
	"fmt"
	"image"
	"image/png"
	"os"
	"path/filepath"
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

	"github.com/some-du6e/yoloslackgoclient/internal/core"
	"github.com/some-du6e/yoloslackgoclient/internal/fakeslack"
	"github.com/some-du6e/yoloslackgoclient/internal/imgcache"
	"github.com/some-du6e/yoloslackgoclient/internal/slack"
	"github.com/some-du6e/yoloslackgoclient/internal/store"
	"github.com/some-du6e/yoloslackgoclient/internal/ui/slackc"
	"github.com/some-du6e/yoloslackgoclient/internal/ui/theme"
	"github.com/some-du6e/yoloslackgoclient/internal/ui/theme/pixeltest"
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
