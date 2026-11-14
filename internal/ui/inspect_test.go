package ui

// The inspect tool (inspect_wire.go, internal/ui/inspect) on the fake
// Slack (synthetic; nothing talks to Slack): the gated shortcut starts a
// session, the reaction pill under the pointer is outlined as Slack's
// UnstyledButton in Reaction, ↑ widens the pick to the Reaction, a click
// copies one image and saves the report, whose text names the component,
// element and rect, tags the ids and holds no content, and the click
// never reaches the pill: after the session it takes clicks again.

import (
	"image"
	"image/color"
	"image/draw"
	"image/png"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"gioui.org/f32"
	"gioui.org/io/key"
	"gioui.org/op"

	"github.com/some-du6e/clack/internal/fakeslack"
	"github.com/some-du6e/clack/internal/ui/inspect"
	"github.com/some-du6e/clack/internal/ui/messages"
)

// inspectEnv is the client on the fake with the tool gated on and a
// message with a reaction in view; reacted lists the reactions.* calls.
type inspectEnv struct {
	*perfEnv
	ch, ts  string
	pill    messages.ReactionRect
	mu      sync.Mutex
	reacted []string
	names   []string // the message's sender's names (must never show)
}

func newInspectEnv(t *testing.T) *inspectEnv {
	t.Helper()
	t.Setenv("CLACK_INSPECT", "1")
	fs, err := fakeslack.Start(fakeslack.Small)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { fs.Close() })
	ids := fs.UserIDs()
	ie := &inspectEnv{ch: fs.ChannelIDs()[0]}
	ie.ts, _ = fs.PostQuiet(ie.ch, ids[1], "the launch moved to friday, keep it quiet")
	if !fs.ReactQuiet(ie.ch, ie.ts, "tada", ids[2]) {
		t.Fatal("no message to react to")
	}
	fs.SetHook(func(method string, _ url.Values) (any, bool) {
		if strings.HasPrefix(method, "reactions.") {
			ie.mu.Lock()
			ie.reacted = append(ie.reacted, method)
			ie.mu.Unlock()
		}
		return nil, false
	})
	e := startEnv(t, fs, t.TempDir())
	ie.perfEnv = e
	if e.a.insp == nil {
		t.Fatal("CLACK_INSPECT=1 but the window has no inspect tool")
	}
	e.waitReady()
	e.openChannel(ie.ch)
	e.until("history", 20*time.Second, func() bool { return e.loadedMessages(ie.ch) > 0 })
	e.toLatest()
	e.until("the reaction pill", 20*time.Second, func() bool {
		cv := e.a.convos[ie.ch+"/"]
		if cv == nil {
			return false
		}
		l, ok := cv.list.(channelList)
		if !ok {
			return false
		}
		for _, r := range l.ReactionRects() {
			if r.TS == ie.ts && r.Name == "tada" {
				ie.pill = r
				return true
			}
		}
		return false
	})
	if u, ok := e.a.Core().S.RawUser(ids[1]); ok {
		for _, n := range []string{u.Name, u.RealName, u.Profile.DisplayName, u.Profile.RealName} {
			if len(n) > 2 {
				ie.names = append(ie.names, n)
			}
		}
	}
	return ie
}

func (e *inspectEnv) reactions() []string {
	e.mu.Lock()
	defer e.mu.Unlock()
	return append([]string(nil), e.reacted...)
}

func (e *inspectEnv) pillCenter() f32.Point {
	r := e.pill.R
	return f32.Pt(float32(r.Min.X+r.Dx()/2), float32(r.Min.Y+r.Dy()/2))
}

// startSession presses the shortcut and points at the pill.
func (e *inspectEnv) startSession(t *testing.T) {
	t.Helper()
	n, m := parseCombo(inspectCombo)
	e.key(n, m)
	e.frame()
	if !e.a.insp.Active() {
		t.Fatal("the shortcut started no inspect session")
	}
	e.move(e.pillCenter())
	e.frame()
}

// reportRows splits a report's text into its rows' values by key.
func reportRows(txt string) map[string]string {
	out := map[string]string{}
	for _, l := range strings.Split(txt, "\n") {
		k, v, ok := strings.Cut(l, "  ")
		if ok && k != "" && !strings.Contains(k, " ") {
			out[k] = strings.TrimLeft(v, " ")
		}
	}
	return out
}

// globMatch matches s against pattern, where * is any run of characters.
func globMatch(pattern, s string) bool {
	parts := strings.Split(pattern, "*")
	if !strings.HasPrefix(s, parts[0]) {
		return false
	}
	s = s[len(parts[0]):]
	for i, p := range parts[1:] {
		if i == len(parts)-2 {
			return strings.HasSuffix(s, p)
		}
		j := strings.Index(s, p)
		if j < 0 {
			return false
		}
		s = s[j+len(p):]
	}
	return s == ""
}

func nearRect(a, b image.Rectangle, d int) bool {
	abs := func(v int) int {
		if v < 0 {
			return -v
		}
		return v
	}
	return abs(a.Min.X-b.Min.X) <= d && abs(a.Min.Y-b.Min.Y) <= d && abs(a.Max.X-b.Max.X) <= d && abs(a.Max.Y-b.Max.Y) <= d
}

// fakeShots replaces the headless capture and the clipboard (no GPU or
// clipboard in plain CI): the shot is plain white.
func fakeShots(t *testing.T) (shots *int, copied *image.Image) {
	shots, copied = new(int), new(image.Image)
	prevShoot, prevWrite := inspect.Shoot, inspect.WriteImage
	inspect.Shoot = func(_ *op.Ops, size image.Point) (*image.RGBA, error) {
		*shots++
		img := image.NewRGBA(image.Rectangle{Max: size})
		draw.Draw(img, img.Bounds(), image.NewUniform(color.White), image.Point{}, draw.Src)
		return img, nil
	}
	inspect.WriteImage = func(img image.Image) error { *copied = img; return nil }
	t.Cleanup(func() { inspect.Shoot, inspect.WriteImage = prevShoot, prevWrite })
	return shots, copied
}

// readReport is the newest saved report's text.
func (e *inspectEnv) readReport(t *testing.T) (txt string, pngs int) {
	t.Helper()
	dir := filepath.Join(e.home, "inspect-reports")
	ents, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("no reports folder: %v", err)
	}
	for _, en := range ents {
		switch filepath.Ext(en.Name()) {
		case ".txt":
			b, err := os.ReadFile(filepath.Join(dir, en.Name()))
			if err != nil {
				t.Fatal(err)
			}
			txt = string(b)
		case ".png":
			pngs++
		}
	}
	return txt, pngs
}

func TestInspectReactionPill(t *testing.T) {
	shots, copied := fakeShots(t)
	e := newInspectEnv(t)
	if _, _, ok := e.a.insp.Hovered(); ok {
		t.Fatal("an outline before any session")
	}
	e.startSession(t)
	label, r, ok := e.a.insp.Hovered()
	if !ok {
		t.Fatalf("nothing outlined over the pill %v", e.pill.R)
	}
	if !strings.HasPrefix(label, "UnstyledButton · button.c-button-unstyled.c-reaction") || !nearRect(r, e.pill.R, 1) {
		t.Errorf("over the pill: outline %v %q, want the pill %v as UnstyledButton · button.c-button-unstyled.c-reaction…", r, label, e.pill.R)
	}
	// ↑ widens to the enclosing component, ↓ comes back
	e.key(key.NameUpArrow, 0)
	e.frame()
	if label, _, _ := e.a.insp.Hovered(); !strings.HasPrefix(label, "Reaction") {
		t.Errorf("↑: outline %q, want the Reaction", label)
	}
	e.key(key.NameDownArrow, 0)
	e.frame()
	if label, _, _ := e.a.insp.Hovered(); !strings.HasPrefix(label, "UnstyledButton") {
		t.Errorf("↓: outline %q, want the pill's UnstyledButton again", label)
	}

	// the click picks: one shot, one image on the clipboard, the session over
	p := e.pillCenter()
	e.click(image.Pt(int(p.X), int(p.Y)))
	e.frame()
	e.a.inspectAfterFrame(&e.ops, e.size)
	e.a.insp.Wait()
	e.frame()
	if e.a.insp.Active() {
		t.Error("the session is still running after the pick")
	}
	if *shots != 1 || *copied == nil {
		t.Fatalf("after the pick: %d shots, image copied %v", *shots, *copied != nil)
	}
	if b := (*copied).Bounds(); b.Dx() != e.size.X || b.Dy() <= e.size.Y {
		t.Errorf("the copied image is %v: want the window with the report under it", b)
	}
	if !strings.Contains(e.a.toast.text, "Copied the inspect report") {
		t.Errorf("toast %q", e.a.toast.text)
	}
	txt, pngs := e.readReport(t)
	if pngs != 1 {
		t.Errorf("%d report images saved, want 1", pngs)
	}
	rows := reportRows(txt)
	pr := e.pill.R // dp = px: the test window's scale is 1
	for _, w := range []struct{ key, want string }{
		{"component", "UnstyledButton"},
		{"element", "button.c-button-unstyled.c-reaction[data-qa=reactji]"},
		{"within", "Reaction < ReactionBar < *Message < *ClientPrimaryView"},
		{"classes", "div.c-message_kit__reaction_bar.c-reaction_bar[data-qa=reaction_bar] < *div.c-message_kit__background.c-message_kit__message*"},
		{"rect", "x " + itoa(pr.Min.X) + " y " + itoa(pr.Min.Y) + " w " + itoa(pr.Dx()) + " h " + itoa(pr.Dy()) + " dp"},
		{"region", "primary"},
		{"page", "c/ch:* (public)"},
		{"ids", "*channelId=ch:*msg.ts=ts:*msg.type=message*"},
		{"kinds", "ch:* public"},
		{"colors", "bg #ffffff*"},
		{"window", "light · * · 1440x900 dp at 1x"},
	} {
		if got := rows[w.key]; !globMatch(w.want, got) {
			t.Errorf("report row %s = %q, want %q\n%s", w.key, got, w.want, txt)
		}
	}
	for _, leak := range append([]string{e.ch, e.ts, "launch", "friday", "quiet", "tada"}, e.names...) {
		if strings.Contains(txt, leak) {
			t.Errorf("the report holds %q:\n%s", leak, txt)
		}
	}
	// the pick's click never reached the pill
	e.settle(300 * time.Millisecond)
	if got := e.reactions(); len(got) != 0 {
		t.Errorf("the pick's click toggled the reaction: %v", got)
	}
	// and after the session the pill takes clicks again
	e.click(image.Pt(int(p.X), int(p.Y)))
	deadline := time.Now().Add(10 * time.Second)
	for len(e.reactions()) == 0 && time.Now().Before(deadline) {
		e.frame()
		time.Sleep(10 * time.Millisecond)
	}
	if got := e.reactions(); len(got) == 0 {
		t.Error("after the session a click on the pill did nothing (input not given back)")
	}
}

// Esc ends a session without a report, and so does the shortcut again;
// with the gate off there's no tool at all.
func TestInspectEscAndGate(t *testing.T) {
	shots, _ := fakeShots(t)
	e := newInspectEnv(t)
	e.startSession(t)
	e.key(key.NameEscape, 0)
	e.frame()
	if e.a.insp.Active() {
		t.Error("Esc didn't end the session")
	}
	e.startSession(t)
	n, m := parseCombo(inspectCombo)
	e.key(n, m)
	e.frame()
	if e.a.insp.Active() {
		t.Error("the shortcut again didn't end the session")
	}
	if *shots != 0 {
		t.Errorf("%d shots without a pick", *shots)
	}
	if _, err := os.Stat(filepath.Join(e.home, "inspect-reports")); err == nil {
		t.Error("a report was saved without a pick")
	}

	t.Setenv("CLACK_INSPECT", "")
	if off, _ := ttApp(t, t.TempDir()); off.insp != nil {
		t.Error("the tool exists with CLACK_INSPECT unset and no inspect_tool pref")
	}
}

// The session's outline and label over the pill, and the image a pick
// copies, light and dark, both through the tool's real headless capture
// (inspect.Shoot: a GPU context made and released per shot, so no other
// headless window may share its EGL display here). INSPECT_SHOTS_DIR:
// the feel-shots workflow saves them.
func TestInspectShots(t *testing.T) {
	dir := os.Getenv("INSPECT_SHOTS_DIR")
	if dir == "" {
		t.Skip("INSPECT_SHOTS_DIR not set: the feel-shots workflow renders these")
	}
	e := newInspectEnv(t)
	prevWrite := inspect.WriteImage
	inspect.WriteImage = func(image.Image) error { return nil }
	t.Cleanup(func() { inspect.WriteImage = prevWrite })
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
	for _, dark := range []bool{false, true} {
		mode := map[bool]string{false: "light", true: "dark"}[dark]
		e.setDark(dark)
		e.settle(300 * time.Millisecond)
		e.startSession(t)
		e.frame()
		if _, _, ok := e.a.insp.Hovered(); !ok {
			t.Fatalf("%s: nothing outlined over the pill", mode)
		}
		hover, err := inspect.Shoot(&e.ops, e.size)
		if err != nil {
			t.Fatalf("%s: the headless capture: %v", mode, err)
		}
		save("inspect-"+mode+"-hover", hover)
		p := e.pillCenter()
		e.click(image.Pt(int(p.X), int(p.Y)))
		e.frame()
		e.a.inspectAfterFrame(&e.ops, e.size)
		e.a.insp.Wait()
		e.frame()
		if !strings.Contains(e.a.toast.text, "paste it as an image") {
			t.Fatalf("%s: toast %q: no image was made", mode, e.a.toast.text)
		}
		ents, _ := os.ReadDir(filepath.Join(e.home, "inspect-reports"))
		var newest string
		for _, en := range ents {
			if filepath.Ext(en.Name()) == ".png" && en.Name() > newest {
				newest = en.Name()
			}
		}
		if newest == "" {
			t.Fatalf("%s: no report image saved", mode)
		}
		b, err := os.ReadFile(filepath.Join(e.home, "inspect-reports", newest))
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, "inspect-"+mode+"-report.png"), b, 0o644); err != nil {
			t.Fatal(err)
		}
		time.Sleep(1100 * time.Millisecond) // the next report's file name (seconds) differs
	}
}
