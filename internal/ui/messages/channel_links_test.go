package messages

// Channel mentions against internal/fakeslack (Karim: "I couldn't click
// these thingies to go to the channel"): #channel-links mentions a
// channel the viewer is in, a private one they're in, a public one they
// aren't in, an archived one, a private one they can't see, one on
// another workspace of the org, and one inside inline code. Slack's
// BaseMrkdwnChannel makes every channel the viewer can see a link that
// navigates to it (the not-member preview and the archived bar are the
// channel view's business: ui/channel_preview_test.go); the hidden one is
// BaseMissingChannel's "Private channel" chip, not a link.

import (
	"context"
	"strings"
	"testing"
	"time"

	"gioui.org/f32"
	"gioui.org/io/pointer"

	"github.com/some-du6e/yoloslackgoclient/internal/core"
	"github.com/some-du6e/yoloslackgoclient/internal/fakeslack"
	"github.com/some-du6e/yoloslackgoclient/internal/richtext"
	"github.com/some-du6e/yoloslackgoclient/internal/slack"
	"github.com/some-du6e/yoloslackgoclient/internal/store"
	"github.com/some-du6e/yoloslackgoclient/internal/ui/richview"
	"github.com/some-du6e/yoloslackgoclient/internal/ui/theme"
)

func linksEnv(t *testing.T) (*ctxEnv, *fakeslack.Server) {
	t.Helper()
	sz := fakeslack.Small
	sz.ChannelLinks = true
	fs, err := fakeslack.Start(sz)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { fs.Close() })
	c := slack.New(fs.Creds())
	c.EdgeURL = fs.URL + "edge/"
	app := core.New(c, store.New(""))
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	done := make(chan error, 1)
	go func() { done <- app.Boot(ctx) }()
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("boot: %v", err)
		}
	case <-time.After(30 * time.Second):
		t.Fatal("boot timed out")
	}
	e := &ctxEnv{t: t, h: &ctxHost{coreHost: coreHost{app: app, th: theme.New(false)}}, now: time.Now()}
	e.v = NewChannelView(e.h, fakeslack.ChannelLinksID)
	e.v.Preview = true
	until(t, 10*time.Second, "the links message laid out", e.frame, func() bool { return e.row(fakeslack.ChannelLinksTS) != nil })
	prev := openURL
	t.Cleanup(func() { openURL = prev })
	return e, fs
}

// linkHit is the first point of ts's body over a channel link to id.
func (e *ctxEnv) linkHit(ts, id string) (f32.Point, bool) {
	s := e.v.s
	r := e.row(ts)
	top := s.pos[s.sel.keyIdx[ts]]
	for y := 1; y < r.bodyRect.Dy(); y += 2 {
		for x := 1; x < r.bodyRect.Dx(); x += 2 {
			p := f32.Pt(float32(x)+.5, float32(y)+.5)
			if h := r.rv.HitAt(p); h.Kind == richview.HitChannel && h.ID == id {
				return p.Add(f32.Pt(float32(r.bodyRect.Min.X)+2, float32(top+r.bodyRect.Min.Y))), true
			}
		}
	}
	return f32.Point{}, false
}

func TestChannelLinks(t *testing.T) {
	e, fs := linksEnv(t)
	cases := fs.ChannelLinkCases()
	app := e.h.app
	name := func(id string) string {
		if c, ok := app.S.Conv(id); ok {
			return c.Name
		}
		return app.S.ChannelName(id)
	}
	var want []string
	for _, c := range cases {
		switch c.Name {
		case "private member":
			want = append(want, name(c.ID))
		case "hidden private":
			want = append(want, richtext.PrivateChannelLabel)
		case "public non-member":
			want = append(want, "#lounge-preview")
		case "archived":
			want = append(want, "#old-hackathon")
		case "other workspace":
			want = append(want, "#partner-workspace-lobby")
		default:
			want = append(want, "#"+name(c.ID))
		}
	}
	wantPlain := strings.Join(want, " · ")
	var got string
	defer func() { t.Logf("plain %q\nwant  %q", got, wantPlain) }()
	until(t, 15*time.Second, "the mentioned channels names", e.frame, func() bool {
		r := e.row(fakeslack.ChannelLinksTS)
		got = r.rv.TextRange(0, r.rv.PlainLen())
		return got == wantPlain
	})
	for k := 0; k < 3; k++ {
		e.frame()
	}
	for _, c := range cases {
		p, ok := e.linkHit(fakeslack.ChannelLinksTS, c.ID)
		if c.Name == "hidden private" {
			if ok {
				t.Errorf("%s: the Private channel chip is a link", c.Name)
			}
			continue
		}
		if !ok {
			t.Errorf("%s (%s): not a link in %q", c.Name, c.ID, got)
			continue
		}
		n := len(e.h.navs)
		e.press(p, pointer.ButtonPrimary, 0)
		if navs := e.h.navs[n:]; len(navs) != 1 || navs[0] != "c/"+c.ID {
			t.Errorf("%s: clicking %s navigated %q", c.Name, c.ID, navs)
		}
	}
	// the hidden chip: a click on it opens nothing
	i := int32(strings.Index(got, richtext.PrivateChannelLabel))
	n := len(e.h.navs)
	e.press(e.at(fakeslack.ChannelLinksTS, i+2), pointer.ButtonPrimary, 0)
	if navs := e.h.navs[n:]; len(navs) != 0 {
		t.Errorf("clicking the Private channel chip navigated %q", navs)
	}
}
