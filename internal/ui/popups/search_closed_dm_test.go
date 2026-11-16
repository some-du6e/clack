package popups

import (
	"context"
	"errors"
	"image"
	"testing"
	"time"

	"github.com/some-du6e/clack/internal/core"
	"github.com/some-du6e/clack/internal/fakeslack"
	"github.com/some-du6e/clack/internal/imgcache"
	"github.com/some-du6e/clack/internal/slack"
	"github.com/some-du6e/clack/internal/store"
	"github.com/some-du6e/clack/internal/ui/theme"
)

// The quick switcher lists a closed DM whose partner the members cache let
// go, once users/info has the partner again, without being reopened. The
// snapshot names a closed DM only while the store holds its partner (core
// reads closed DMs' partners raw); the switcher's own lookup asks for a
// missing one, and its candidates load again when that member is in.
func TestSearchPopoverClosedDMNamedLater(t *testing.T) {
	fs, err := fakeslack.Start(fakeslack.Small)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { fs.Close() })
	const dm, partner = "D0SWCLOSED01", fakeslack.ProfileID // a closed DM; nothing else draws its partner
	fs.AddClosedDM(dm, partner)
	a := core.New(slack.New(fs.Creds()), store.New(t.TempDir()))
	t.Cleanup(a.Shutdown)
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	if err := a.Boot(ctx); err != nil {
		t.Fatal(err)
	}
	// the members cache lets everyone but the viewer go
	a.S.SetMembersSizeForDebug(1, 1)
	a.EvictMembers()
	a.S.SetMembersSizeForDebug(-1, -1)
	if _, ok := a.S.RawUser(partner); ok {
		t.Fatal("the closed DM's partner is still in the store")
	}
	noNet := func(context.Context, string) ([]byte, error) { return nil, errors.New("no network in tests") }
	th := theme.New(false)
	sh := theme.NewShaper()
	th.SetShapers(sh, sh)
	srResetRecents()
	t.Cleanup(srResetRecents)
	h := &tzHost{a: a, th: th, im: imgcache.New(noNet, "", nil)}

	p := NewSearchPopover(h, "", image.Rectangle{})
	d := &srDrv{size: image.Pt(1280, 800), w: p}
	d.frame()
	if !p.snapOK {
		t.Fatal("the switcher took no snapshot")
	}
	d.until(t, "the closed DM among the switcher's conversations once its partner is in", func() bool {
		_, ok := p.byConv[dm]
		return ok && p.dmOf[partner] == dm
	})
	if _, ok := a.S.RawUser(partner); !ok {
		t.Error("the switcher's lookup didn't fetch the partner")
	}
}
