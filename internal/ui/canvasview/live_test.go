package canvasview

// A read-only repro against a real account (CLACK_LIVE_CANVAS=1, creds in
// CLACK_LIVE_CREDS, the channel in CLACK_LIVE_CHANNEL): the channel's
// canvas through Clack's own fetch (files.info + the url_private export via
// slack.DownloadCanvasHTML), parsed, laid out, rendered through a headless
// GPU with CLACK_LIVE_GPU=1, and swept with the pointer (clicks included).
// The export stays in memory; only element, tag and span kinds are logged.
// Never sends files.open (live is pre-set); readOnly refuses every method
// but four reads, so an edit can't leave the machine. It found alpha.24's
// crash (crash_test.go has the synthetic regression test).

import (
	"context"
	"encoding/json"
	"fmt"
	"image"
	"net/http"
	"net/url"
	"os"
	"runtime/debug"
	"strings"
	"testing"
	"time"

	"gioui.org/f32"
	"gioui.org/gpu/headless"
	"gioui.org/io/input"
	"gioui.org/io/pointer"
	"gioui.org/op"
	"golang.org/x/net/html"

	"github.com/some-du6e/yoloslackgoclient/internal/canvas"
	"github.com/some-du6e/yoloslackgoclient/internal/core"
	"github.com/some-du6e/yoloslackgoclient/internal/imgcache"
	"github.com/some-du6e/yoloslackgoclient/internal/richtext"
	"github.com/some-du6e/yoloslackgoclient/internal/slack"
	"github.com/some-du6e/yoloslackgoclient/internal/store"
	"github.com/some-du6e/yoloslackgoclient/internal/ui/theme"
	"github.com/some-du6e/yoloslackgoclient/internal/ui/theme/pixeltest"
)

func TestLiveChannelCanvas(t *testing.T) {
	if os.Getenv("CLACK_LIVE_CANVAS") != "1" {
		t.Skip("CLACK_LIVE_CANVAS not set")
	}
	b, err := os.ReadFile(os.Getenv("CLACK_LIVE_CREDS"))
	if err != nil {
		t.Fatal(err)
	}
	var cr slack.Creds
	if err := json.Unmarshal(b, &cr); err != nil {
		t.Fatal(err)
	}
	cl := slack.New(cr)
	cl.HTTP.Transport = readOnly{cl.HTTP.Transport}
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	var ci struct {
		Channel struct {
			Properties struct {
				Canvas struct {
					FileID string `json:"file_id"`
				} `json:"canvas"`
				Tabs []struct {
					Type string `json:"type"`
					Data struct {
						FileID string `json:"file_id"`
					} `json:"data"`
				} `json:"tabs"`
			} `json:"properties"`
		} `json:"channel"`
	}
	if err := cl.Call(ctx, "conversations.info", url.Values{"channel": {os.Getenv("CLACK_LIVE_CHANNEL")}}, &ci); err != nil {
		t.Fatal(err)
	}
	var ids []string
	if id := ci.Channel.Properties.Canvas.FileID; id != "" {
		ids = append(ids, id)
	}
	for _, tb := range ci.Channel.Properties.Tabs {
		t.Logf("tab type=%s file=%s", tb.Type, tb.Data.FileID)
		if tb.Data.FileID != "" && tb.Type == "canvas" {
			ids = append(ids, tb.Data.FileID)
		}
	}
	if len(ids) == 0 {
		t.Fatal("no canvas")
	}
	a := core.New(cl, store.New(t.TempDir()))
	var el struct {
		Emoji map[string]string `json:"emoji"`
	}
	if err := cl.Call(ctx, "emoji.list", nil, &el); err != nil {
		t.Fatal(err)
	}
	a.S.SetEmojiList(el.Emoji)
	t.Logf("emoji: %d", len(el.Emoji))
	for _, id := range dedupe(ids) {
		d, err := a.Canvas(ctx, id)
		if err != nil {
			t.Fatalf("%s: %v", id, err)
		}
		if src, err := cl.DownloadCanvasHTML(ctx, d.Info.URL, canvas.MaxHTML); err == nil { // in memory; structure only
			if root, err := html.Parse(strings.NewReader(src)); err == nil {
				var walk func(n *html.Node, depth int)
				walk = func(n *html.Node, depth int) {
					if n.Type == html.ElementNode && depth <= 6 {
						var attrs []string
						for _, at := range n.Attr {
							if at.Key == "class" || at.Key == "data-section-style" {
								attrs = append(attrs, at.Key+"="+at.Val)
							} else {
								attrs = append(attrs, at.Key)
							}
						}
						t.Logf("  %*s<%s %s>", depth*2, "", n.Data, strings.Join(attrs, " "))
					}
					for c := n.FirstChild; c != nil; c = c.NextSibling {
						walk(c, depth+1)
					}
				}
				walk(root, 0)
			}
		}
		a.EnsureUsers(ctx, append([]string{d.Info.Creator}, d.Info.Editors...))
		t.Logf("%s: title spans=%s blocks=%d editable=%v", id, spanKinds(d.Info.TitleSpans), len(d.Doc.Blocks), d.Info.Editable)
		d.Doc.Walk(func(b *canvas.Block) bool {
			t.Logf("  block kind=%d level=%d spans=%s kids=%d cols=%d rows=%d", b.Kind, b.Level, spanKinds(b.Spans), len(b.Children), len(b.Columns), len(b.Rows))
			return true
		})
		for _, dark := range []bool{true, false} {
			for _, mode := range []Mode{Tab, Side, Full} {
				h := &testHost{a: a, th: theme.New(dark)}
				h.img = imgcache.New(func(ctx context.Context, u string) ([]byte, error) {
					b, _, err := a.Fetch(ctx, u)
					return b, err
				}, "", nil)
				v := New(h, id, mode, Actions{})
				v.live.Store(true) // no files.open
				v.doc, v.gen = d, 1

				var win *headless.Window
				if os.Getenv("CLACK_LIVE_GPU") == "1" { // run the ops through the GPU renderer (never read back)
					w, err := pixeltest.NewWindow(900, 700)
					if err != nil {
						t.Fatal(err)
					}
					win = w
					defer w.Release()
				}
				router := new(input.Router)
				step := 0
				frames := func(what string, n int) {
					for i := 0; i < n; i++ {
						func() {
							defer func() {
								if r := recover(); r != nil {
									t.Fatalf("panic (%s dark=%v mode=%d frame %d): %v\n%s", what, dark, mode, i, r, debug.Stack())
								}
							}()
							ops := new(op.Ops)
							gtx := gtxFor(ops, image.Pt(900, 700))
							gtx.Source = router.Source()
							v.Layout(gtx)
							router.Frame(ops)
							// sweep the pointer over the header and the first blocks; click now and then
							pt := f32.Pt(float32(30+(step*37)%700), float32(20+(step*53)%260))
							step++
							router.Queue(pointer.Event{Kind: pointer.Move, Source: pointer.Mouse, Position: pt})
							if step%9 == 0 {
								router.Queue(pointer.Event{Kind: pointer.Press, Source: pointer.Mouse, Buttons: pointer.ButtonPrimary, Position: pt},
									pointer.Event{Kind: pointer.Release, Source: pointer.Mouse, Position: pt})
							}
							if win != nil {
								if err := win.Frame(ops); err != nil {
									t.Fatalf("gpu frame: %v", err)
								}
							}
						}()
						time.Sleep(25 * time.Millisecond)
					}
				}
				frames("view", 160)
				for i, b := range v.blocks {
					t.Logf("  laid out block %d kind=%d rv=%v edOK=%v", i, b.b.Kind, b.rv != nil, b.edOK)
					if !b.edOK {
						continue
					}
					func() {
						defer func() {
							if r := recover(); r != nil {
								t.Fatalf("panic starting the editor on block %d: %v\n%s", i, r, debug.Stack())
							}
						}()
						v.edit.start(v, b)
					}()
					frames("editor", 20)
					v.edit.discard(v)
					frames("after editor", 5)
				}
				if d.Info.Editable {
					v.startRename()
					frames("rename", 20)
					v.edit.renaming, v.edit.renameOn = false, false
				}
				frames("again", 10)
				if v.crashed != nil {
					t.Fatalf("a panic was recovered (dark=%v mode=%d): %v", dark, mode, v.crashed)
				}
				u, uok := a.User(d.Info.Creator)
				t.Logf("  creator known=%v avatar=%v viewers=%d", uok, u.Avatar != "", len(a.CanvasViewers(id)))
			}
		}
	}
}

func dedupe(s []string) []string {
	seen := map[string]bool{}
	var out []string
	for _, x := range s {
		if !seen[x] {
			seen[x] = true
			out = append(out, x)
		}
	}
	return out
}

func spanKinds(sp []richtext.Span) string {
	var k []string
	for _, s := range sp {
		e := fmt.Sprintf("%d", s.Kind)
		if s.URL != "" {
			e += "+url"
			if strings.HasSuffix(strings.SplitN(s.URL, "?", 2)[0], ".gif") {
				e += "(gif)"
			}
		}
		if s.Style != 0 {
			e += fmt.Sprintf("/st%d", s.Style)
		}
		k = append(k, e)
	}
	return "[" + strings.Join(k, " ") + "]"
}

// readOnly lets only the reads this test makes leave the machine.
type readOnly struct{ rt http.RoundTripper }

func (r readOnly) RoundTrip(req *http.Request) (*http.Response, error) {
	ok := req.Method == http.MethodGet && !strings.Contains(req.URL.Path, "/api/")
	for _, m := range []string{"conversations.info", "files.info", "emoji.list", "users.info"} {
		if strings.HasSuffix(req.URL.Path, "/api/"+m) {
			ok = true
		}
	}
	if strings.HasPrefix(req.URL.Host, "edgeapi.") && strings.HasSuffix(req.URL.Path, "/users/info") {
		ok = true // the edge cache's users/info read (a POST)
	}
	if !ok {
		return nil, fmt.Errorf("live test: refused %s %s", req.Method, req.URL.Path)
	}
	return r.rt.RoundTrip(req)
}
