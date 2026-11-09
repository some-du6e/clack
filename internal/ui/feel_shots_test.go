package ui

// The sidebar's top links (Threads … Directories, with Drafts & sent's
// badges) and the empty channel composer, unfocused and focused, light
// and dark, at Slack's 1440×900 with its 328px sidebar: the two crops
// Karim said "feel off". The whole window over an offline core (synthetic
// workspace, nothing talks to Slack). With FEEL_SHOTS_DIR set (the
// feel-shots workflow) each state is saved as a PNG next to a crop of the
// sidebar top and of the composer, plus palette.json: the colours the
// sidebar rows and the composer were drawn with, for rebuilding the same
// rows in Chrome (Slack's DOM and SVGs) and comparing ink.

import (
	"encoding/json"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"os"
	"path/filepath"
	"testing"
	"time"

	"gioui.org/f32"
	"gioui.org/gpu/headless"
	"gioui.org/io/pointer"

	"github.com/some-du6e/yoloslackgoclient/internal/slack"
	"github.com/some-du6e/yoloslackgoclient/internal/ui/theme/pixeltest"
)

func TestFeelShots(t *testing.T) {
	dir := os.Getenv("FEEL_SHOTS_DIR")
	pal := map[string]map[string]string{}
	hex := func(c color.NRGBA) string { return fmt.Sprintf("#%02x%02x%02x%02x", c.R, c.G, c.B, c.A) }
	for _, dark := range []bool{false, true} {
		mode := map[bool]string{false: "light", true: "dark"}[dark]
		t.Run(mode, func(t *testing.T) {
			e := ctxShell(t)
			e.sz = image.Pt(1440, 900)
			e.a.sidebar.width = 328
			e.ca.S.PutConv(slack.Conversation{ID: shotChan, Name: "clack", IsChannel: true, IsMember: true, NumMembers: 3})
			e.ca.S.SetLatest(shotChan, nil, false)
			b := e.a.data
			b.fetched[shotChan] = time.Now() // offline: no tabs/facepile fetches
			if len(b.m.Sections) > 0 {
				b.m.Sections[0].Rows = append(b.m.Sections[0].Rows, shotChan)
				b.m.version++
			}
			// Karim's screenshot showed 100 drafts: Slack caps the badge at
			// "99+"; two scheduled messages add Slack's second badge.
			b.draftN, b.schedN = 100, 2
			if dark {
				e.a.setDark(true)
			}
			var win *headless.Window
			if dir != "" {
				w, err := pixeltest.NewWindow(e.sz.X, e.sz.Y)
				if err != nil {
					t.Fatalf("headless window: %v", err)
				}
				t.Cleanup(w.Release)
				win = w
			}
			save := func(name string, img image.Image) {
				f, err := os.Create(filepath.Join(dir, name+".png"))
				if err != nil {
					t.Fatal(err)
				}
				defer f.Close()
				if err := png.Encode(f, img); err != nil {
					t.Fatal(err)
				}
			}
			shot := func(name string) {
				for i := 0; i < 30; i++ {
					e.frame()
					time.Sleep(10 * time.Millisecond)
				}
				if win == nil {
					return
				}
				if err := win.Frame(e.ops); err != nil {
					t.Fatal(err)
				}
				img := image.NewRGBA(image.Rectangle{Max: e.sz})
				if err := win.Screenshot(img); err != nil {
					t.Fatal(err)
				}
				save(name, img)
				save(name+"-sidebar-top", img.SubImage(image.Rect(70, 80, 70+328, 300)))
				if cv := e.a.convos[shotChan+"/"]; cv != nil && !cv.compRect.Empty() {
					save(name+"-composer", img.SubImage(cv.compRect.Inset(-10)))
				}
			}
			e.a.Navigate("c/" + shotChan)
			shot("feel-" + mode + "-unfocused")
			cv := e.a.convos[shotChan+"/"]
			if cv == nil || cv.compRect.Empty() {
				t.Fatal("no composer laid out")
			}
			// focus the composer with a click in its text area
			p := cv.compRect.Min.Add(image.Pt(200, cv.compRect.Dy()/2))
			e.clock += time.Second
			pp := f32.Pt(float32(p.X), float32(p.Y))
			e.rt.Queue(
				pointer.Event{Kind: pointer.Move, Source: pointer.Mouse, Position: pp, Time: e.clock},
				pointer.Event{Kind: pointer.Press, Source: pointer.Mouse, Buttons: pointer.ButtonPrimary, Position: pp, Time: e.clock + 10*time.Millisecond},
				pointer.Event{Kind: pointer.Release, Source: pointer.Mouse, Position: pp, Time: e.clock + 60*time.Millisecond},
			)
			shot("feel-" + mode + "-focused")
			pal[mode] = map[string]string{
				"sidebarBase":      hex(e.a.sbBase),
				"sidebarTextRaw":   hex(e.a.th.C.SidebarText),
				"sidebarText":      hex(e.a.sbPal.SidebarText),
				"sidebarHover":     hex(e.a.sbPal.SidebarHover),
				"bg":               hex(e.a.th.C.Bg),
				"bgInset":          hex(e.a.th.C.BgInset),
				"fg":               hex(e.a.th.C.Fg),
				"fgMuted":          hex(e.a.th.C.FgMuted),
				"fgSubtle":         hex(e.a.th.C.FgSubtle),
				"chromeA":          hex(e.a.th.C.ChromeA),
				"chromeB":          hex(e.a.th.C.ChromeB),
				"sidebarBgOverlay": hex(e.a.th.C.SidebarBg),
			}
		})
	}
	if dir != "" {
		js, _ := json.MarshalIndent(pal, "", "  ")
		if err := os.WriteFile(filepath.Join(dir, "palette.json"), js, 0o644); err != nil {
			t.Fatal(err)
		}
	}
}

// Drafts & sent's badge and tooltip text follow Slack's OutboxLinkContent:
// drafts capped at maxCount 99, scheduled uncapped, the tooltip's plural.
func TestOutboxBadgeText(t *testing.T) {
	for n, want := range map[int]string{1: "1", 99: "99", 100: "99+", 1234: "99+"} {
		if got := outboxCount(n); got != want {
			t.Errorf("outboxCount(%d) = %q, want %q", n, got, want)
		}
	}
	for _, c := range []struct {
		d, s int
		want string
	}{{1, 0, "1 Draft"}, {100, 0, "100 Drafts"}, {0, 3, "3 Scheduled"}, {2, 1, "2 Drafts\n1 Scheduled"}} {
		if got := outboxTip(c.d, c.s); got != c.want {
			t.Errorf("outboxTip(%d, %d) = %q, want %q", c.d, c.s, got, c.want)
		}
	}
}
