package ui

// Three consecutive messages from one sender (test1, test2, test3), at the
// top level of a channel and as thread replies with the reply composer,
// in light and dark: the case Karim compared against Slack. The whole
// window over an offline core (synthetic workspace, nothing talks to
// Slack). With SPACING_SHOTS_DIR set (the spacing-shots workflow) each
// state is saved as a PNG; the geometry itself is asserted against
// Slack's CSS in internal/ui/messages/spacing_test.go.

import (
	"encoding/json"
	"fmt"
	"image"
	"image/png"
	"os"
	"path/filepath"
	"strconv"
	"testing"
	"time"

	"gioui.org/gpu/headless"

	"github.com/some-du6e/clack/internal/slack"
	"github.com/some-du6e/clack/internal/ui/theme/pixeltest"
)

const shotChan = "C0CLACK001"

func shotTS(t time.Time) string {
	return strconv.FormatInt(t.Unix(), 10) + fmt.Sprintf(".%06d", t.Nanosecond()/1000)
}

func shotBlocks(s string) json.RawMessage {
	return json.RawMessage(`[{"type":"rich_text","block_id":"b` + s + `","elements":[{"type":"rich_text_section","elements":[{"type":"text","text":"` + s + `"}]}]}]`)
}

func TestSpacingShots(t *testing.T) {
	dir := os.Getenv("SPACING_SHOTS_DIR")
	for _, dark := range []bool{false, true} {
		mode := map[bool]string{false: "light", true: "dark"}[dark]
		t.Run(mode, func(t *testing.T) {
			e := ctxShell(t)
			now := time.Now()
			// the sender is the viewer, in a huddle here (the headphones by
			// the name, as in Karim's screenshot)
			e.ca.S.PutUsers([]slack.User{{ID: ttSelf, Name: "karim", RealName: "Karim Example", Profile: slack.Profile{
				DisplayName: "karim", RealName: "Karim Example", HuddleState: "in_a_huddle", HuddleChannel: shotChan,
				HuddleExpires: now.Add(time.Hour).Unix()}}})
			e.ca.S.PutConv(slack.Conversation{ID: shotChan, Name: "clack", IsChannel: true, IsMember: true, NumMembers: 3})
			msg := func(at time.Time, text, thread string) slack.Message {
				return slack.Message{Type: "message", TS: shotTS(at), User: ttSelf, Text: text, Blocks: shotBlocks(text), ThreadTS: thread}
			}
			rootAt := now.Add(-50 * time.Second)
			root := msg(rootAt, "i think it was in a thread", shotTS(rootAt))
			replies := []slack.Message{
				msg(now.Add(-40*time.Second), "test1", root.TS),
				msg(now.Add(-35*time.Second), "test2", root.TS),
				msg(now.Add(-30*time.Second), "tesst3", root.TS),
			}
			root.ReplyCount, root.LatestReply, root.ReplyUsers = 3, replies[2].TS, []string{ttSelf}
			e.ca.S.SetLatest(shotChan, []slack.Message{root,
				msg(now.Add(-20*time.Second), "test1", ""), msg(now.Add(-15*time.Second), "test2", ""), msg(now.Add(-10*time.Second), "test3", "")}, false)
			e.ca.S.SetThread(shotChan, root.TS, append([]slack.Message{root}, replies...))
			b := e.a.data
			b.fetched[shotChan] = time.Now() // offline: no tabs/facepile fetches
			if len(b.m.Sections) > 0 {
				b.m.Sections[0].Rows = append(b.m.Sections[0].Rows, shotChan)
				b.m.version++
			}
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
			shot := func(name string) {
				for i := 0; i < 40; i++ { // history, names, layout settle
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
				save(name, img)
				if e.a.secondary.kind == secThread { // the pane alone, like Karim's crop
					w := int(e.a.secondary.width)
					save(name+"-pane", img.SubImage(image.Rect(e.sz.X-w, 0, e.sz.X, e.sz.Y)))
				}
			}
			e.a.Navigate("c/" + shotChan)
			shot("channel-consecutive-" + mode)
			e.a.OpenThread(shotChan, root.TS)
			shot("thread-" + mode)
			if e.a.secondary.kind != secThread {
				t.Fatalf("the thread pane didn't open (secondary %v)", e.a.secondary.kind)
			}
		})
	}
}
