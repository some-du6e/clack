package ui

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"gioui.org/f32"
	"gioui.org/io/pointer"

	"github.com/some-du6e/clack/internal/fakeslack"
	"github.com/some-du6e/clack/internal/imgcache/webptest"
	"github.com/some-du6e/clack/internal/slack"
)

// quietlyUpper patches every message's text, as a messages plugin's view
// of it (Message Logger, Show Real User).
const quietlyUpper = `(() => {
const TautPlugin = globalThis.TautPlugin
class QuietlyUpper extends TautPlugin {
  static id = 'QuietlyUpper'
  static pluginName = 'Quietly upper (test)'
  static description = 'test: patches the text of every message'
  static authors = ['test']
  static defaultConfig = { enabled: true }
  start() { this.api.messages.patchMessageText((t) => t.replace(/quietly/g, 'QUIETLY')) }
  stop() {}
}
return QuietlyUpper
})()`

// A message arrives while the window shows an animated emoji and nothing
// else lays it out (in the background: no caret). Its row draws the
// message as core has it and asks taut for the plugin's view; the view
// comes a moment later with one Invalidate, whose layout falls inside the
// row's 250 ms viewRefresh. The row re-reads at once because taut's views
// changed (ViewEpoch), and the patched text shows within a few animation
// ticks. It stopped at the throttle, and every frame after was a quiet
// one (an animation tick lays nothing out), until a scrollbar's wake-up
// 1.25 s later or a mouse move. Synthetic data only.
func TestPatchedViewShowsUnderQuietFrames(t *testing.T) {
	fs, err := fakeslack.Start(benchSize)
	if err != nil {
		t.Fatal(err)
	}
	defer fs.Close()
	name, u := fs.FixtureEmoji()
	path := strings.TrimPrefix(u, fs.URL+"img/")
	data := webptest.Explodes()
	fs.SetImages(func(p string) ([]byte, string, bool) {
		if p == path {
			return data, "image/png", true // a WebP behind a .png URL, as Slack serves :explodes:
		}
		return nil, "", false
	})
	e := benchStart(t, fs)
	e.waitReady()
	e.until("Taut", 60*time.Second, func() bool { return e.a.Taut() != nil })
	tt := e.a.Taut()
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	id, err := tt.Import(ctx, []byte(quietlyUpper), "")
	if err != nil {
		t.Fatal(err)
	}
	if err := tt.SetEnabled(ctx, id, true); err != nil {
		t.Fatal(err)
	}
	e.until("the messages slice patched", 20*time.Second, func() bool { return tt.SlicePatched("messages") })
	wake := make(chan struct{}, 1)
	onAnimRepaint(e.perfEnv, func() {
		select {
		case wake <- struct{}{}:
		default:
		}
	})
	ch := fakeslack.BenchChannelID
	e.openChannel(ch)
	e.settle(500 * time.Millisecond)
	self := e.a.Core().Self().ID
	post := func(text string) string {
		ts, ev := fs.PostQuiet(ch, self, text)
		b, _ := json.Marshal(ev)
		chJSON, _ := json.Marshal(ch)
		e.a.data.core.Inject(slack.Event{Type: "message", Channel: chJSON, Raw: b})
		return ts
	}
	post("boom :" + name + ": boom")
	e.toBottom()
	e.until("the emoji playing", 20*time.Second, func() bool { return e.a.images.Stats().Players > 0 })
	e.r.Queue(pointer.Event{Kind: pointer.Move, Source: pointer.Mouse, Position: f32.Pt(-100, -100)}) // off the window
	// in the background: no caret blinks, only the emoji's ticks
	e.blur()
	e.settle(800 * time.Millisecond)
	e.frame()
	askedAt, asked := e.r.WakeupTime()
	rowText := func(ts string) string {
		if c := e.a.convos[ch+"/"]; c != nil {
			if l, ok := c.list.(channelList); ok {
				return l.RowText(ts)
			}
		}
		return ""
	}

	ts := post("this one arrives quietly")
	t0 := time.Now()
	var shown time.Duration
	quiet := 0
	for dl := t0.Add(2 * time.Second); time.Now().Before(dl) && shown == 0; {
		var due <-chan time.Time
		if asked {
			due = time.After(max(time.Until(askedAt), 0))
		}
		select {
		case <-wake:
		case <-due:
		case <-time.After(time.Until(dl)):
			continue
		}
		if e.quietFrame(asked, askedAt) {
			quiet++
		}
		askedAt, asked = e.r.WakeupTime()
		if strings.Contains(rowText(ts), "QUIETLY") {
			shown = time.Since(t0)
		}
	}
	t.Logf("the patched text showed after %v (%d quiet frames before)", shown, quiet)
	if shown == 0 || shown > 600*time.Millisecond {
		t.Errorf("with only the emoji's ticks after it, the new message's row drew %q; want the plugin's text within ~300 ms (showed after %v)", rowText(ts), shown)
	}
}
