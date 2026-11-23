package popups

// Karim, 2026-11-23 (α.146): "clicking a bot profile to open this, the
// pfp takes a bit to load. should no loading bc it alr there from the
// message". The App pane draws the app's 192px icon; the message drew the
// bot user's 72px picture, the same image at another size. With that one
// cached, the pane's first frame with the app draws the icon (scaled for
// the moment), never the loading tile, while the sharp size loads.
// Synthetic: the in-repo fake's app.

import (
	"context"
	"image"
	"strings"
	"sync"
	"testing"
	"time"

	"gioui.org/layout"
	"gioui.org/op"
	"gioui.org/unit"

	"github.com/some-du6e/clack/internal/fakeslack"
	"github.com/some-du6e/clack/internal/imgcache"
)

func TestAppPaneIconFromTheMessageLikeSlack(t *testing.T) {
	h, fs := acEnv(t, fakeslack.AppUserID, fakeslack.AdminID)
	release := make(chan struct{})
	var once sync.Mutex
	freed := false
	free := func() {
		once.Lock()
		if !freed {
			freed = true
			close(release)
		}
		once.Unlock()
	}
	t.Cleanup(free)
	u, ok := h.a.User(fakeslack.AppUserID)
	if !ok || u.Avatar == "" {
		t.Fatal("the bot user has no picture")
	}
	// the message's picture loads; any other size is held
	h.im = imgcache.New(func(ctx context.Context, url string) ([]byte, error) {
		if url != u.Avatar {
			select {
			case <-release:
			case <-ctx.Done():
				return nil, ctx.Err()
			}
		}
		return fs.Fetch(ctx, url)
	}, "", nil)
	for dl := time.Now().Add(10 * time.Second); ; time.Sleep(10 * time.Millisecond) {
		if _, ok := h.im.Get(u.Avatar, 72); ok {
			break
		}
		if time.Now().After(dl) {
			t.Fatal("the message's picture never loaded")
		}
	}
	v := NewAppDetailsView(h, fakeslack.AppUserID, "")
	ops := new(op.Ops)
	frame := func() {
		ops.Reset()
		gtx := layout.Context{Ops: ops, Now: time.Now(), Metric: unit.Metric{PxPerDp: 2, PxPerSp: 2},
			Constraints: layout.Exact(image.Pt(800, 1600))}
		v.Layout(gtx)
		drain()
	}
	var card bool
	for dl := time.Now().Add(10 * time.Second); !card; time.Sleep(10 * time.Millisecond) {
		frame()
		_, card = v.Card()
		if time.Now().After(dl) {
			t.Fatal("the app never loaded")
		}
	}
	c, _ := v.Card()
	if c.IconDefault || c.Icon == "" || c.Icon == u.Avatar || !strings.Contains(c.Icon, "/app.png") {
		t.Fatalf("the app's icon %q (default %v) isn't another size of the message's %q: the case tests nothing", c.Icon, c.IconDefault, u.Avatar)
	}
	// the frame that drew the app first: its icon, no loading tile
	gtx := layout.Context{Ops: new(op.Ops), Now: time.Now(), Metric: unit.Metric{PxPerDp: 2, PxPerSp: 2}}
	if !appIcon(gtx, h, c, 72, 16) {
		t.Errorf("the App pane draws its loading tile for %q while the message's %q is cached", c.Icon, u.Avatar)
	}
}
