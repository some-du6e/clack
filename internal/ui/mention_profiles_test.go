package ui

// Slick's MentionProfiles the way Karim used it (α.37 on Windows, a fresh
// install, "pfp icon extension doesnt work"): installed from the plugin
// library it is on at once (its own default is off) and its MemberSlug
// patch applies without a restart; then a message you send with an
// @mention (pending, then confirmed by chat.postMessage) draws the
// member's picture in the pill: Slick's CSS puts it in the slug's
// ::before (content '', 1em, background: url(image_48) center / cover),
// from a real-shaped avatars.slack-edge.com URL. Synthetic data against
// internal/fakeslack; the ui-slick workflow builds Slick's plugins and has
// the GPU renderer for the pixels.

import (
	"bytes"
	"context"
	"encoding/json"
	"image"
	"image/color"
	"image/png"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/some-du6e/clack/internal/core"
	"github.com/some-du6e/clack/internal/fakeslack"
	"github.com/some-du6e/clack/internal/pluginlib"
	"github.com/some-du6e/clack/internal/slack"
	"github.com/some-du6e/clack/internal/ui/slackc"
	"github.com/some-du6e/clack/internal/ui/theme/pixeltest"
)

const (
	mpUser = "U0VERITY01"
	// Slack's avatar URLs: avatars.slack-edge.com/<date>/<id>_<hash>_<size>.png
	mpAvatar = "https://avatars.slack-edge.com/2025-06-01/9001234567890_0a1b2c3d4e5f60718293_"
)

// mpGreen is the avatar: solid green, a colour nothing else in the window
// draws.
var mpGreen = color.NRGBA{G: 0xff, A: 0xff}

// mpPNG is a 48px square of c.
func mpPNG(c color.NRGBA) []byte {
	im := image.NewNRGBA(image.Rect(0, 0, 48, 48))
	for y := 0; y < 48; y++ {
		for x := 0; x < 48; x++ {
			im.SetNRGBA(x, y, c)
		}
	}
	var b bytes.Buffer
	_ = png.Encode(&b, im)
	return b.Bytes()
}

// mpGreenPixels counts the pixels close to mpGreen in img.
func mpGreenPixels(img *image.RGBA) int {
	n := 0
	b := img.Bounds()
	for y := b.Min.Y; y < b.Max.Y; y++ {
		for x := b.Min.X; x < b.Max.X; x++ {
			c := img.RGBAAt(x, y)
			if c.G > 0xc0 && c.R < 0x60 && c.B < 0x60 {
				n++
			}
		}
	}
	return n
}

func TestSlickViewMentionProfilesFromLibrary(t *testing.T) {
	dir := os.Getenv("SLICK_PLUGINS_DIR")
	if dir == "" {
		t.Skip("SLICK_PLUGINS_DIR not set: the ui-slick workflow builds Slick's plugins and runs this")
	}
	code, err := os.ReadFile(filepath.Join(dir, "MentionProfiles.js"))
	if err != nil {
		t.Fatal(err)
	}
	// the library: MentionProfiles as the published index lists it
	ls := newLibServer(t)
	entry := pluginlib.Entry{System: pluginlib.Slick, ID: "MentionProfiles", Name: "Mention Profiles",
		Description: "Show profile pictures inside @mentions", Category: "messages", CategoryLabel: "Messages",
		File: pluginlib.File{File: "slick-MentionProfiles.js", SHA256: pluginlib.Hash(code)}}
	ix, _ := json.Marshal(pluginlib.Index{Schema: 1, Plugins: []pluginlib.Entry{entry},
		Sources: map[string]pluginlib.Source{pluginlib.Slick: {Name: "Slick", Repo: "https://github.com/3kh0/slick", Commit: "f52d55dbd458feb0", License: "GPL-3.0"}}})
	ls.mu.Lock()
	ls.files["index.json"], ls.files[entry.File.File] = ix, code
	ls.mu.Unlock()

	fs, err := fakeslack.Start(fakeslack.Small)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { fs.Close() })
	// Verity's avatar is green; every other picture flat gray (the fake's
	// own gradients have green corners)
	avatar, gray := mpPNG(mpGreen), mpPNG(color.NRGBA{R: 0x88, G: 0x88, B: 0x88, A: 0xff})
	fs.SetImages(func(path string) ([]byte, string, bool) {
		if strings.Contains(path, "avatars.slack-edge.com") {
			return avatar, "image/png", true
		}
		return gray, "image/png", true
	})
	home := t.TempDir()
	t.Setenv("CLACK_HOME", home)
	e := startEnv(t, fs, home)
	if e.gpu == nil {
		w, err := pixeltest.NewWindow(e.size.X, e.size.Y)
		if err != nil {
			t.Skipf("no GPU renderer: %v", err)
		}
		e.gpu = w
	}
	e.waitReady()
	e.until("Slick", 60*time.Second, func() bool { return e.a.Slick() != nil })
	app := e.a.Core()
	app.S.PutUsers([]slack.User{{ID: mpUser, Name: "verity", RealName: "Verity Example", Profile: slack.Profile{DisplayName: "verity",
		Image24: mpAvatar + "24.png", Image32: mpAvatar + "32.png", Image48: mpAvatar + "48.png"}}})

	// Preferences > Plugins > Plugin library > Install
	var pm *prefsModal
	prefsTap = func(p *prefsModal) { pm = p }
	t.Cleanup(func() { prefsTap = nil })
	e.a.showPrefs(pluginsTabID)
	e.until("Preferences", 5*time.Second, func() bool { return pm != nil && pm.plugins != nil })
	pp := pm.plugins
	pp.setMode("browse")
	e.until("the library", 30*time.Second, func() bool { return pp.lib.ix != nil && !pp.lib.loading })
	var g libGroup
	for _, x := range libraryGroups(libraryEntries(pp.lib.ix)) {
		if x.first().ID == "MentionProfiles" {
			g = x
		}
	}
	if len(g.Entries) == 0 {
		t.Fatal("MentionProfiles isn't in the library")
	}
	act := pp.lib.action(g)
	if act.label != "Install" || act.do == nil {
		t.Fatalf("its action is %q", act.label)
	}
	act.do()
	info := func() (on, running bool) {
		for _, pl := range e.a.Slick().Settings().Plugins {
			if pl.ID == "MentionProfiles" {
				return pl.Enabled, pl.Running
			}
		}
		return false, false
	}
	e.until("MentionProfiles installed, on and running", 60*time.Second, func() bool {
		on, running := info()
		return on && running && slackc.Patched("MemberSlug")
	})
	if st := pp.lib.status[libKey(entry)]; st != "Installed and turned on." {
		t.Errorf("the row says %q after Install", st)
	}
	for e.a.dismissTop() {
	}

	// a fresh message with a mention of Verity, sent from #general
	const ch = "C0FAKE00000"
	e.openChannel(ch)
	e.settle(300 * time.Millisecond)
	shot := func() *image.RGBA {
		img := image.NewRGBA(image.Rectangle{Max: e.size})
		if err := e.gpu.Screenshot(img); err != nil {
			t.Fatal(err)
		}
		return img
	}
	if n := mpGreenPixels(shot()); n != 0 {
		t.Fatalf("%d avatar-green pixels before the message: the check can't tell the pill's picture", n)
	}
	blocks, _ := json.Marshal([]any{map[string]any{"type": "rich_text", "block_id": "mp", "elements": []any{
		map[string]any{"type": "rich_text_section", "elements": []any{
			map[string]any{"type": "user", "user_id": mpUser}, map[string]any{"type": "text", "text": " the synthetic launch notes are up"}}}}}})
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	m, err := app.Send(ctx, core.SendOpts{Channel: ch, Text: "<@" + mpUser + "> the synthetic launch notes are up", Blocks: blocks})
	if err != nil {
		t.Fatal(err)
	}
	if m.Pending || m.TS == "" {
		t.Fatalf("the send didn't land: %+v", m)
	}
	// #general opens at its unread line (the fake's unreads), and the list
	// doesn't follow a send on its own: show the newest message
	if c := e.a.convoFor(ch, ""); c != nil {
		if l, ok := c.list.(channelList); ok {
			l.ScrollToBottom()
		}
	}
	var img *image.RGBA
	n := 0
	e.until("Verity's picture in the mention pill", 20*time.Second, func() bool {
		img = shot()
		n = mpGreenPixels(img)
		return n >= 40
	})
	t.Logf("%d avatar pixels in the pill", n)
	if d := os.Getenv("SLICK_SHOTS_DIR"); d != "" {
		f, err := os.Create(filepath.Join(d, "mention-profiles-library.png"))
		if err == nil {
			_ = png.Encode(f, img)
			f.Close()
		}
	}
}
