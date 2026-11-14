package messages

import (
	"image"
	"strings"
	"sync"
	"testing"
	"time"

	"gioui.org/f32"
	"gioui.org/io/input"
	"gioui.org/io/pointer"
	"gioui.org/layout"
	"gioui.org/op"
	"gioui.org/unit"

	"github.com/some-du6e/clack/internal/core"
	"github.com/some-du6e/clack/internal/fakeslack"
	"github.com/some-du6e/clack/internal/richtext"
	"github.com/some-du6e/clack/internal/slack"
	"github.com/some-du6e/clack/internal/store"
	"github.com/some-du6e/clack/internal/ui/popups"
	"github.com/some-du6e/clack/internal/ui/theme"
)

// Clicks on the files of fakeslack's #files-gallery (synthetic files)
// through Gio's router, in a channel and a thread, with and without
// plugins patching the rows: every gallery tile opens the image viewer on
// its own file (the "+ N" tile on the file under it), a video and the
// file cards (audio, PDF, snippet, zip, documents) open the file, and a
// right click on a tile opens the file menu. A snippet's pillow has no
// click (a collapsed one's click expands it).

type fileClickHost struct {
	coreHost
	mu       sync.Mutex
	viewer   []string // OpenLightbox image URLs
	overlays []popups.Overlay
}

func (h *fileClickHost) OpenLightbox(img richtext.Image, _ core.Message) {
	h.mu.Lock()
	h.viewer = append(h.viewer, img.URL)
	h.mu.Unlock()
}

func (h *fileClickHost) ShowOverlay(o popups.Overlay) {
	h.mu.Lock()
	h.overlays = append(h.overlays, o)
	h.mu.Unlock()
}

func (h *fileClickHost) counts() (int, int) {
	h.mu.Lock()
	defer h.mu.Unlock()
	return len(h.viewer), len(h.overlays)
}

// filesSeed puts #files-gallery (history and its thread) in s; fs is the
// fake whose fixtures they are (its /img URLs are never fetched here).
func filesSeed(fs *fakeslack.Server) func(s *store.Store) {
	return func(s *store.Store) {
		s.PutConv(slack.Conversation{ID: fakeslack.FilesChannelID, Name: "files-gallery", IsChannel: true, IsMember: true, NumMembers: 6})
		hist := fs.FilesHistory()
		s.SetLatest(fakeslack.FilesChannelID, hist, false)
		for _, m := range hist {
			if m.ReplyCount > 0 {
				s.SetThread(fakeslack.FilesChannelID, m.TS, fs.FilesReplies(m.TS))
			}
		}
	}
}

func filesFake(t *testing.T) *fakeslack.Server {
	t.Helper()
	sz := fakeslack.Small
	sz.Files = true
	fs, err := fakeslack.Start(sz)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { fs.Close() })
	return fs
}

type clickView interface {
	Layout(gtx layout.Context) layout.Dimensions
	FileRects() []FileRect
}

// fileClicks draws v over a router and clicks every file of msgs,
// checking what opened.
func fileClicks(t *testing.T, label string, h *fileClickHost, v clickView, size image.Point, msgs []slack.Message, opened *[]string) {
	t.Helper()
	var r input.Router
	ops := new(op.Ops)
	now := time.Now()
	frame := func() {
		ops.Reset()
		now = now.Add(20 * time.Millisecond)
		gtx := layout.Context{Ops: ops, Metric: unit.Metric{PxPerDp: 1, PxPerSp: 1}, Constraints: layout.Exact(size), Now: now, Source: r.Source()}
		v.Layout(gtx)
		r.Frame(ops)
	}
	until(t, 8*time.Second, label+": files on screen", frame, func() bool {
		has := map[string]bool{}
		for _, fr := range v.FileRects() {
			has[fr.TS] = true
		}
		for _, m := range msgs {
			if len(m.Files) > 0 && !has[m.TS] {
				return false
			}
		}
		return true
	})
	for k := 0; k < 10; k++ {
		frame()
	}
	var clock time.Duration
	click := func(p image.Point, btn pointer.Buttons) {
		clock += 600 * time.Millisecond
		pt := f32.Pt(float32(p.X), float32(p.Y))
		r.Queue(
			pointer.Event{Kind: pointer.Move, Source: pointer.Mouse, Position: pt, Time: clock},
			pointer.Event{Kind: pointer.Press, Source: pointer.Mouse, Buttons: btn, Position: pt, Time: clock + time.Millisecond},
			pointer.Event{Kind: pointer.Release, Source: pointer.Mouse, Position: pt, Time: clock + 2*time.Millisecond},
		)
		for k := 0; k < 4; k++ {
			frame()
		}
	}
	byTS := map[string][]FileRect{}
	for _, fr := range v.FileRects() {
		byTS[fr.TS] = append(byTS[fr.TS], fr)
	}
	tiles := 0
	for _, m := range msgs {
		if len(m.Files) == 0 {
			continue
		}
		rs := byTS[m.TS]
		name := label + " " + strings.TrimSpace(m.Text)
		if len(rs) == 0 {
			t.Errorf("%s: no file on screen", name)
			continue
		}
		if len(rs) > len(m.Files) {
			t.Errorf("%s: %d boxes for %d files", name, len(rs), len(m.Files))
			continue
		}
		for i, fr := range rs {
			want := &m.Files[i]
			if fr.File.ID != want.ID {
				t.Errorf("%s: box %d is %s, want %s", name, i, fr.File.ID, want.ID)
				continue
			}
			p := fr.R.Min.Add(fr.R.Max).Div(2)
			nv, _ := h.counts()
			nu := len(*opened)
			click(p, pointer.ButtonPrimary)
			gotV, _ := h.counts()
			switch img := strings.HasPrefix(want.Mimetype, "image/"); {
			case img:
				tiles++
				if fr.R.Dy() < 100 {
					t.Errorf("%s: %s is %v, not a gallery tile", name, want.ID, fr.R)
				}
				if gotV != nv+1 {
					t.Errorf("%s: clicking %s at %v didn't open the image viewer", name, want.ID, p)
				} else if u := h.viewer[len(h.viewer)-1]; u != want.URLPrivate {
					t.Errorf("%s: the viewer opened %s, want %s", name, u, want.URLPrivate)
				}
			case want.Mode == "snippet":
				// PillowFile has no onClick for a snippet (isExpandable);
				// a collapsed one expands instead (snippet.go)
				if gotV != nv || len(*opened) != nu {
					t.Errorf("%s: clicking the snippet %s opened %v (viewer %v)", name, want.ID, (*opened)[nu:], gotV != nv)
				}
			default:
				if gotV != nv {
					t.Errorf("%s: %s (%s) opened the image viewer", name, want.ID, want.Mimetype)
				}
				if len(*opened) != nu+1 || (*opened)[len(*opened)-1] != want.Permalink {
					t.Errorf("%s: clicking %s (%s) at %v opened %v, want %s", name, want.ID, want.Mimetype, p, (*opened)[nu:], want.Permalink)
				}
			}
		}
		// the first file's context menu (Slack's FileContextMenuTrigger)
		_, no := h.counts()
		click(rs[0].R.Min.Add(rs[0].R.Max).Div(2), pointer.ButtonSecondary)
		_, got := h.counts()
		if got != no+1 {
			t.Errorf("%s: no menu on a right click on %s", name, rs[0].File.ID)
			continue
		}
		h.mu.Lock()
		o := h.overlays[len(h.overlays)-1]
		h.mu.Unlock()
		if mn, ok := o.(*popups.Menu); !ok || !menuHas(mn, "Copy link") {
			t.Errorf("%s: right click on %s opened %T, not the file menu", name, rs[0].File.ID, o)
		}
	}
	if tiles == 0 {
		t.Errorf("%s: no image tiles clicked", label)
	}
}

func menuHas(m *popups.Menu, label string) bool {
	for _, it := range m.Items() {
		if it.Label == label {
			return true
		}
	}
	return false
}

func recordOpenURL(t *testing.T) *[]string {
	var got []string
	prev := openURL
	openURL = func(u string) { got = append(got, u) }
	t.Cleanup(func() { openURL = prev })
	return &got
}

// fileClickViews clicks through #files-gallery and its thread in h.
func fileClickViews(t *testing.T, fs *fakeslack.Server, h *fileClickHost) {
	opened := recordOpenURL(t)
	ch := fakeslack.FilesChannelID
	cv := NewChannelView(h, ch)
	cv.Preview = true
	hist := fs.FilesHistory()
	fileClicks(t, "channel", h, cv, image.Pt(1600, 9000), hist, opened)
	for _, m := range hist {
		if m.ReplyCount == 0 {
			continue
		}
		tv := NewThreadView(h, ch, m.TS)
		fileClicks(t, "thread", h, tv, image.Pt(480, 4000), fs.FilesReplies(m.TS)[1:], opened)
	}
}

func TestFileClicks(t *testing.T) {
	fs := filesFake(t)
	app, _ := newTestApp(t)
	filesSeed(fs)(app.S)
	fileClickViews(t, fs, &fileClickHost{coreHost: coreHost{app: app, th: theme.New(false)}})
}

// The same with a plugin patching the file cards and the message text
// (the row's natives under a plugin tree).
func TestFileClicksUnderPatches(t *testing.T) {
	fs := filesFake(t)
	app, _, tt, u, ch := pluginEnv(t)
	filesSeed(fs)(app.S)
	install(t, tt, []byte(`(() => {
const TautPlugin = globalThis.TautPlugin
class WrapFiles extends TautPlugin {
  static id = 'WrapFiles'
  static pluginName = 'Wrap Files'
  static description = 'wraps MessageFile and MessageText'
  static authors = ['test']
  static defaultConfig = { enabled: true }
  start() {
    const R = React
    this.api.patchComponent('MessageFile', (O) => (p) => R.createElement('div', { className: 'x-wrapped-file' }, R.createElement(O, p)))
    this.api.patchComponent('MessageText', (O) => (p) => R.createElement('div', { className: 'x-wrapped-text' }, R.createElement(O, p)))
  }
}
return WrapFiles
})()`))
	patched(t, "MessageFile", "MessageText")
	fileClickViews(t, fs, &fileClickHost{coreHost: ch})
	if !u.anyTree("x-wrapped-text") {
		t.Error("the plugin's MessageText tree never rendered")
	}
}

// With Slick's message-patching plugins (the ui-slick workflow).
func TestSlickViewFileClicks(t *testing.T) {
	fs := filesFake(t)
	e := newSlickEnv(t, filesSeed(fs))
	for _, p := range []string{"ShowSendingBot", "BetterBlock", "MessageLogger", "WhoReacted", "LocalTime"} {
		e.install(t, p, nil)
	}
	time.Sleep(300 * time.Millisecond)
	fileClickViews(t, fs, &fileClickHost{coreHost: e.h})
}
