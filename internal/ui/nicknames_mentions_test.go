package ui

// Nicknames (Slick's and Taut's) in @mentions, through the whole window:
// a mention names the member as plugins present it (the patched members
// slice, richview.MemberLabel installed by installTaut), as Slack's
// MemberSlug reads the member from the store. Screenshots of the message
// pane go to SLICK_SHOTS_DIR / TAUT_SHOTS_DIR. Synthetic data only.

import (
	"context"
	"image"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/some-du6e/clack/internal/core"
	"github.com/some-du6e/clack/internal/slack"
	"github.com/some-du6e/clack/internal/ui/theme"
)

const nkNick = "Ally Nickname"

func nkEnv(t *testing.T) *svEnv {
	return newSV(t, func(home string, a *App, ca *core.App) {
		ca.S.SetLatest(ttGen, []slack.Message{
			{Type: "message", TS: "1700000000.000100", User: svBob, Text: "<@" + svAlice + "> synthetic ping about the plan"},
		}, false)
	})
}

// nkCheck waits for alice's mention label to read the nickname and saves
// the window.
func nkCheck(t *testing.T, e *svEnv, dir, name string) {
	t.Helper()
	label := newMentionLabels(e.a.Taut())
	e.until(15*time.Second, "alice's mention label reading the nickname", func() bool {
		n, ok := label(svAlice)
		return ok && n == nkNick
	})
	if n, ok := label(svBob); ok && n != "bob" {
		t.Errorf("bob's mention label %q, want his own name", n)
	}
	sh := newSVShooter(t, e)
	sh.dir = dir
	for i := 0; i < 5; i++ {
		e.frame()
	}
	img := sh.shot()
	sh.save(name, img)
	if sh.dir != "" { // the primary view
		x := int(theme.RailW) + e.a.sidebar.width
		sh.save(name+"-pane", img.SubImage(image.Rect(x, int(theme.TopNavH), e.sz.X, e.sz.Y)).(*image.RGBA))
	}
}

func TestSlickViewNicknamesMentions(t *testing.T) {
	e := nkEnv(t)
	e.install("Nicknames", map[string]any{"names": map[string]any{svAlice: nkNick}})
	nkCheck(t, e, os.Getenv("SLICK_SHOTS_DIR"), "nicknames-mention-slick")
}

func TestTautViewNicknamesMentions(t *testing.T) {
	dir := os.Getenv("TAUT_PLUGINS_DIR")
	if dir == "" {
		t.Skip("TAUT_PLUGINS_DIR not set: the ui-taut workflow builds Taut's plugins and runs this")
	}
	e := nkEnv(t)
	tt := e.a.Taut()
	tt.DataWrite("plugin:Nicknames:storage", "nicknames", `{"`+svAlice+`":"`+nkNick+`"}`)
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	code, err := os.ReadFile(filepath.Join(dir, "Nicknames.js"))
	if err != nil {
		t.Fatal(err)
	}
	id, err := tt.Import(ctx, code, "")
	if err != nil {
		t.Fatal(err)
	}
	if err := tt.SetEnabled(ctx, id, true); err != nil {
		t.Fatal(err)
	}
	nkCheck(t, e, os.Getenv("TAUT_SHOTS_DIR"), "nicknames-mention-taut")
}
