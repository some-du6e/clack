package composer

import (
	"bytes"
	"context"
	"fmt"
	"image"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"gioui.org/layout"
	"gioui.org/op"
	"gioui.org/unit"

	"github.com/some-du6e/clack/internal/taut"
	"github.com/some-du6e/clack/internal/ui/slackc"
	"github.com/some-du6e/clack/internal/ui/theme"
)

// startTautUI is startSlick for a Taut plugin (TAUT_PLUGINS_DIR, the
// composer-taut workflow): the runtime wired to slackc like the ui root
// (styles, patches, trees), the plugin imported unmodified with opts set
// before it starts.
func startTautUI(t *testing.T, name string, opts map[string]any) *slickEnv {
	t.Helper()
	dir := os.Getenv("TAUT_PLUGINS_DIR")
	if dir == "" {
		t.Skip("TAUT_PLUGINS_DIR not set: the composer-taut workflow builds Taut's plugins and runs this")
	}
	code, err := os.ReadFile(filepath.Join(dir, name+".js"))
	if err != nil {
		t.Fatal(err)
	}
	app, fs := syntheticApp(t)
	e := &slickEnv{t: t, app: app, slack: fs, trees: map[uint64][]byte{}, h: &slickHost{app: app, th: theme.New(false)}}
	tdir := t.TempDir()
	tt, err := taut.New(taut.Options{
		App: app, Dir: filepath.Join(tdir, "taut"), CredsPath: filepath.Join(tdir, "creds.json"), CacheDir: t.TempDir(), NoAutoStart: true,
		UI: taut.UIHooks{
			Attached: func(b *taut.Bridge) { slackc.SetBridge(b) },
			Deliver: func(root uint64, tree []byte) {
				e.mu.Lock()
				e.trees[root] = append([]byte(nil), tree...)
				e.mu.Unlock()
				_ = slackc.Deliver(root, tree)
			},
			SetPatched:  slackc.SetPatched,
			CallGo:      slackc.CallGo,
			SetStyle:    slackc.SetStyle,
			RemoveStyle: slackc.RemoveStyle,
		},
		Fetch: func(ctx context.Context, req *http.Request) (*http.Response, error) {
			if strings.HasPrefix(req.URL.String(), fs.srv.URL) {
				return http.DefaultClient.Do(req.WithContext(ctx))
			}
			return &http.Response{StatusCode: 404, Header: http.Header{}, Body: io.NopCloser(bytes.NewReader(nil)), Request: req}, nil
		},
		Log: func(level, msg string) {
			if level == "error" || level == "warn" {
				t.Logf("js %s: %s", level, msg)
			}
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = tt.Close() })
	e.tt = tt
	SetTaut(tt)
	slackc.Init(slackc.Env{Theme: e.h.th, Core: app, Invalidate: func() {}})
	slackc.SetBridge(tt.Bridge())
	t.Cleanup(func() {
		slackc.Init(slackc.Env{})
		SetTaut(nil)
		syncHooks()
		slackc.SetBridge(nil)
		slackc.Reset()
		slackc.SetPatched(nil)
	})
	ctx := e.ctx()
	id, err := tt.Import(ctx, code, "")
	if err != nil {
		t.Fatalf("import %s: %v", name, err)
	}
	for k, v := range opts {
		if err := tt.SetOption(ctx, id, k, v); err != nil {
			t.Fatalf("set %s.%s: %v", id, k, err)
		}
	}
	if err := tt.SetEnabled(ctx, id, true); err != nil {
		t.Fatalf("enable %s: %v", id, err)
	}
	return e
}

// Taut's SlimMessageBox one-line layout (on by default): its CSS makes
// Slack's .c-basic_container__body a wrapping row (flex order 3 for the
// editor, 6 for the footer, as Clack draws them), so the footer sits
// beside the editor while the editor keeps its 450px; a narrow composer
// stacks; and attached files keep Slack's stacked layout (its
// :not(:has(… .c-pending_files …)), which the box's declared pending
// files row answers).
func TestTautSlimMessageBoxOneLine(t *testing.T) {
	e := startTautUI(t, "SlimMessageBox", nil)
	e.until(10*time.Second, "SlimMessageBox's TextyButtons patch", nil, func() bool { return slackc.Patched("TextyButtons") })
	c, frame := e.composer(tChGen)
	e.why = func() string {
		f := &c.ui.f
		return fmt.Sprintf("oneLine %v box %v send %v footNat %d row %d text %d att %d", f.oneLine, f.box, c.ui.rect[crSend], c.ui.footNat, f.rowH, f.textH, f.attH)
	}
	e.until(8*time.Second, "the footer beside the editor, in its row", frame, func() bool {
		f, send := &c.ui.f, c.ui.rect[crSend]
		return f.oneLine && !send.Empty() && f.rowH < f.textH+30 && send.Min.Y >= f.box.Max.Y-f.rowH-1
	})
	oneLineH := c.ui.f.box.Dy()

	ops := new(op.Ops)
	narrow := func() {
		ops.Reset()
		gtx := layout.Context{Ops: ops, Metric: unit.Metric{PxPerDp: 1, PxPerSp: 1},
			Constraints: layout.Constraints{Max: image.Pt(500, 600)}, Now: time.Now()}
		slackc.Frame(gtx)
		c.Layout(gtx)
	}
	e.until(5*time.Second, "the footer on its own row in a narrow composer", narrow, func() bool {
		return !c.ui.f.oneLine && c.ui.f.box.Dy() >= oneLineH+c.ui.f.textH
	})

	// a pending file: Slack's stacked layout again, at full width
	c.att.Add("synthetic.txt", []byte("synthetic file"))
	e.until(5*time.Second, "the footer on its own row with a file attached", frame, func() bool {
		return c.att.Len() == 1 && !c.ui.f.oneLine && c.ui.f.attH > 0
	})
	c.att.Clear()
	e.until(5*time.Second, "one line again once the file is gone", frame, func() bool { return c.ui.f.oneLine })
}
