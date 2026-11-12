package messages

// Slick's MentionProfiles in every message row path (Karim, α.37: a pill
// with no picture): its MemberSlug patch wraps the native slug, and its
// CSS draws the member's image_48 in the slug's ::before (content '', a
// 1em circle, background: var(--slick-mention-profile) center / cover).
// The picture is checked in the pixels, from a real-shaped
// avatars.slack-edge.com URL, in a history row, a row that arrives over
// realtime and a thread reply; and its one setting, showAtSymbol, keeps
// or drops the @. Synthetic data; ui-slick runs it with Slick's plugins
// and the GPU renderer.

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"image"
	"image/color"
	"image/png"
	"strings"
	"testing"
	"time"

	"gioui.org/layout"
	"gioui.org/op"
	"gioui.org/op/clip"
	"gioui.org/op/paint"
	"gioui.org/unit"

	"github.com/some-du6e/clack/internal/imgcache"
	"github.com/some-du6e/clack/internal/slack"
	"github.com/some-du6e/clack/internal/store"
	"github.com/some-du6e/clack/internal/ui/slackc"
	"github.com/some-du6e/clack/internal/ui/theme/pixeltest"
)

func TestSlickViewMentionProfilesRows(t *testing.T) {
	const (
		ch, verity = "C0MENTPIX", "U0VERITY01"
		tsHist     = "1700000901.000100"
		tsRoot     = "1700000902.000100"
		tsReply    = "1700000902.000200"
		tsLive     = "1700000903.000100"
		avatar     = "https://avatars.slack-edge.com/2025-06-01/9001234567890_0a1b2c3d4e5f60718293_"
	)
	mention := func(rest string) json.RawMessage {
		return richBlocks(map[string]any{"type": "user", "user_id": verity}, textEl(rest))
	}
	e := newSlickEnv(t, func(s *store.Store) {
		s.PutUsers([]slack.User{{ID: verity, Name: "verity", RealName: "Verity Example", Profile: slack.Profile{DisplayName: "verity",
			Image24: avatar + "24.png", Image32: avatar + "32.png", Image48: avatar + "48.png"}}})
		s.PutConv(slack.Conversation{ID: ch, Name: "mention-pictures", IsChannel: true, IsMember: true, NumMembers: 3})
		s.SetLatest(ch, []slack.Message{
			{Type: "message", TS: tsHist, User: "UBOB", Text: "<@" + verity + "> the synthetic notes", Blocks: mention(" the synthetic notes")},
			{Type: "message", TS: tsRoot, User: "UBOB", Text: "a synthetic thread", ThreadTS: tsRoot, ReplyCount: 1, LatestReply: tsReply, ReplyUsers: []string{"UALICE"}},
		}, false)
		s.SetThread(ch, tsRoot, []slack.Message{
			{Type: "message", TS: tsRoot, User: "UBOB", Text: "a synthetic thread", ThreadTS: tsRoot, ReplyCount: 1},
			{Type: "message", TS: tsReply, User: "UALICE", Text: "<@" + verity + "> a synthetic reply", ThreadTS: tsRoot, Blocks: mention(" a synthetic reply")},
		})
	})
	// the pills' pictures come from the image cache, as in the app
	green := color.NRGBA{G: 0xff, A: 0xff}
	im := image.NewNRGBA(image.Rect(0, 0, 48, 48))
	for i := 0; i < 48*48; i++ {
		im.SetNRGBA(i%48, i/48, green)
	}
	var pngB bytes.Buffer
	_ = png.Encode(&pngB, im)
	imgs := imgcache.New(func(ctx context.Context, url string) ([]byte, error) {
		if strings.HasPrefix(url, "https://avatars.slack-edge.com/") {
			return pngB.Bytes(), nil
		}
		return nil, errors.New("offline")
	}, "", nil)
	slackc.Init(slackc.Env{Theme: e.h.th, Core: e.app, Images: imgs, Invalidate: func() {}})
	t.Cleanup(func() { slackc.Init(slackc.Env{}) })

	e.install(t, "MentionProfiles", nil) // showAtSymbol: its default, on
	patched(t, "MemberSlug")

	size := image.Pt(720, 560)
	ops := new(op.Ops)
	draw := func(w interface {
		Layout(gtx layout.Context) layout.Dimensions
	}) {
		ops.Reset()
		paint.FillShape(ops, e.h.th.C.Bg, clip.Rect{Max: size}.Op())
		gtx := layout.Context{Ops: ops, Metric: unit.Metric{PxPerDp: 1, PxPerSp: 1}, Constraints: layout.Exact(size), Now: time.Now()}
		w.Layout(gtx)
	}
	pixels := func(w interface {
		Layout(gtx layout.Context) layout.Dimensions
	}) int {
		for i := 0; i < 3; i++ {
			draw(w)
		}
		img := pixeltest.Render(t, size.X, size.Y, ops)
		n := 0
		for y := 0; y < size.Y; y++ {
			for x := 0; x < size.X; x++ {
				if c := img.RGBAAt(x, y); c.G > 0xc0 && c.R < 0x60 && c.B < 0x60 {
					n++
				}
			}
		}
		return n
	}
	waitPixels := func(what string, w interface {
		Layout(gtx layout.Context) layout.Dimensions
	}, atLeast int) int {
		t.Helper()
		deadline := time.Now().Add(15 * time.Second)
		for {
			n := pixels(w)
			if n >= atLeast {
				t.Logf("%s: %d avatar pixels", what, n)
				return n
			}
			if time.Now().After(deadline) {
				t.Fatalf("%s: %d avatar pixels, want at least %d (the picture isn't drawn in the pill)", what, n, atLeast)
			}
			time.Sleep(50 * time.Millisecond)
		}
	}

	// a history row, with its @ (showAtSymbol is on)
	v := NewChannelView(e.h, ch)
	v.Preview = true
	one := waitPixels("the history row", v, 40)
	if txt := rowText(v.s.rows[tsHist]); !strings.Contains(txt, "@verity") {
		t.Errorf("history row %q: the @ went with showAtSymbol on", txt)
	}
	// a row that arrives over realtime
	raw, _ := json.Marshal(map[string]any{"type": "message", "channel": ch, "user": "UALICE", "ts": tsLive,
		"text": "<@" + verity + "> a synthetic live note", "blocks": mention(" a synthetic live note")})
	e.app.Inject(slack.Event{Type: "message", Channel: json.RawMessage(`"` + ch + `"`), Raw: raw})
	waitPixels("the history and the live rows", v, one+one*3/4)
	// a thread reply
	waitPixels("the thread reply", NewThreadView(e.h, ch, tsRoot), 40)

	// showAtSymbol off: the name without the @
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if err := e.s.SetOption(ctx, "MentionProfiles", "showAtSymbol", false); err != nil {
		t.Fatal(err)
	}
	e.waitFor(t, "the pill without the @", func() { draw(v) }, func() bool {
		txt := rowText(v.s.rows[tsHist])
		return strings.Contains(txt, "verity") && !strings.Contains(txt, "@verity")
	})
}
