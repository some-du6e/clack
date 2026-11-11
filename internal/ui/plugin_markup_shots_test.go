package ui

// Plugin markup with Slack's class names, drawn with Slack's rules (the
// CSS engine's host base sheet) through the whole window: CopyReacted's
// copy chip (button.c-button-unstyled.c-reaction_add) after the
// add-reaction button, and ThreadJump's Jump in thread button
// (c-icon_button c-icon_button--size_medium c-icon_button--default
// p-flexpane_header__control) in the thread header. Screenshots go to
// SLICK_SHOTS_DIR / TAUT_SHOTS_DIR. Synthetic data only.

import (
	"context"
	"fmt"
	"image"
	"image/color"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/some-du6e/clack/internal/core"
	"github.com/some-du6e/clack/internal/slack"
	"github.com/some-du6e/clack/internal/ui/slackc"
	"github.com/some-du6e/clack/internal/ui/theme"
)

const pmRootTS = "1700000000.000100"

// pmEnv is newSV with a reacted message in #general and a 60-reply thread
// root after it.
func pmEnv(t *testing.T) *svEnv {
	return newSV(t, func(home string, a *App, ca *core.App) {
		root := slack.Message{Type: "message", TS: "1700000100.000100", User: svAlice, Text: "a synthetic thread with many replies",
			ReplyCount: 60, LatestReply: "1700006000.000100", ReplyUsers: []string{svBob}}
		root.ThreadTS = root.TS
		ca.S.SetLatest(ttGen, []slack.Message{
			{Type: "message", TS: pmRootTS, User: svBob, Text: "a synthetic message people reacted to",
				Reactions: []slack.Reaction{{Name: "tada", Count: 2, Users: []string{svAlice, ttSelf}}, {Name: "eyes", Count: 1, Users: []string{svBob}}}},
			root,
		}, false)
		var replies []slack.Message
		for i := 1; i <= 3; i++ {
			r := slack.Message{Type: "message", TS: fmt.Sprintf("17000%02d000.000100", i+10), User: svBob, Text: fmt.Sprintf("synthetic reply %d", i), ThreadTS: root.TS}
			replies = append(replies, r)
		}
		ca.S.SetThread(ttGen, root.TS, append([]slack.Message{root}, replies...))
	})
}

// pmShot saves the window and its primary view, and returns the window.
func pmShot(t *testing.T, e *svEnv, dir, name string) *image.RGBA {
	sh := newSVShooter(t, e)
	sh.dir = dir
	for i := 0; i < 5; i++ {
		e.frame()
	}
	img := sh.shot()
	sh.save(name, img)
	if sh.dir != "" {
		x := int(theme.RailW) + e.a.sidebar.width
		sh.save(name+"-pane", img.SubImage(image.Rect(x, int(theme.TopNavH), e.sz.X, e.sz.Y)).(*image.RGBA))
	}
	return img
}

// tautInstall imports and enables a Taut plugin from TAUT_PLUGINS_DIR.
func (e *svEnv) tautInstall(name string) {
	e.t.Helper()
	dir := os.Getenv("TAUT_PLUGINS_DIR")
	if dir == "" {
		e.t.Skip("TAUT_PLUGINS_DIR not set: the ui-taut workflow builds Taut's plugins and runs this")
	}
	tt := e.a.Taut()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	code, err := os.ReadFile(filepath.Join(dir, name+".js"))
	if err != nil {
		e.t.Fatal(err)
	}
	id, err := tt.Import(ctx, code, "")
	if err != nil {
		e.t.Fatal(err)
	}
	if err := tt.SetEnabled(ctx, id, true); err != nil {
		e.t.Fatal(err)
	}
}

func TestSlickViewCopyReactedChip(t *testing.T) {
	e := pmEnv(t)
	e.install("CopyReacted", nil)
	e.until(15*time.Second, "the copy chip after the add-reaction button", func() bool {
		return e.tree("slick-cr__btn", "c-reaction_add") != ""
	})
	pmShot(t, e, os.Getenv("SLICK_SHOTS_DIR"), "copy-reacted-chip-slick")
}

func TestTautViewCopyReactedChip(t *testing.T) {
	if os.Getenv("TAUT_PLUGINS_DIR") == "" {
		t.Skip("TAUT_PLUGINS_DIR not set: the ui-taut workflow builds Taut's plugins and runs this")
	}
	e := pmEnv(t)
	e.tautInstall("CopyReacted")
	e.until(15*time.Second, "the copy chip after the add-reaction button", func() bool {
		return e.tree("taut-copy-reacted", "c-reaction_add") != ""
	})
	pmShot(t, e, os.Getenv("TAUT_SHOTS_DIR"), "copy-reacted-chip-taut")
}

func TestTautViewThreadJumpButton(t *testing.T) {
	if os.Getenv("TAUT_PLUGINS_DIR") == "" {
		t.Skip("TAUT_PLUGINS_DIR not set: the ui-taut workflow builds Taut's plugins and runs this")
	}
	e := pmEnv(t)
	e.tautInstall("ThreadJump")
	e.a.OpenThread(ttGen, "1700000100.000100")
	e.until(15*time.Second, "Jump in thread in the thread header", func() bool {
		return e.tree("Jump in thread", "c-icon_button--size_medium") != ""
	})
	img := pmShot(t, e, os.Getenv("TAUT_SHOTS_DIR"), "thread-jump-button-taut")
	// its caret-up-down glyph sits in the header row, left of More actions
	// (the header's flex row: .p-flexpane_header__primary)
	r := image.Rect(e.sz.X-260, int(theme.TopNavH)+80, e.sz.X-84, int(theme.TopNavH)+140)
	if n := pmInk(img, r); n < 25 {
		t.Errorf("%d glyph pixels left of More actions in the thread header: Jump in thread isn't drawn there; ThreadSearchButton laid out as %s",
			n, slackc.LayoutShape("ThreadSearchButton"))
	}
}

// pmInk counts the pixels in r darker than luma 160 (glyph strokes on the
// light pane).
func pmInk(img *image.RGBA, r image.Rectangle) int {
	r = r.Intersect(img.Bounds())
	n := 0
	for y := r.Min.Y; y < r.Max.Y; y++ {
		for x := r.Min.X; x < r.Max.X; x++ {
			p := img.RGBAAt(x, y)
			if svLuma(color.NRGBA{p.R, p.G, p.B, 0xff}) < 160 {
				n++
			}
		}
	}
	return n
}
