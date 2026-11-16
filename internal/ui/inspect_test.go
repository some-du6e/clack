package ui

// The inspect tool (inspect_wire.go, internal/ui/inspect) on the fake
// Slack (synthetic; nothing talks to Slack): the gated shortcut starts a
// session, the reaction pill under the pointer is outlined as Slack's
// UnstyledButton in Reaction, ↑ widens the pick to the Reaction, a click
// copies one image and saves the report, whose text names the component,
// element and rect, tags the ids and holds no content, and the click
// never reaches the pill: after the session it takes clicks again.

import (
	"encoding/json"
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
	"unicode/utf8"

	"gioui.org/f32"
	"gioui.org/io/key"
	"gioui.org/io/pointer"
	"gioui.org/io/system"
	"gioui.org/op"

	"github.com/some-du6e/clack/internal/fakeslack"
	"github.com/some-du6e/clack/internal/imgcache"
	"github.com/some-du6e/clack/internal/ui/inspect"
	"github.com/some-du6e/clack/internal/ui/messages"
	"github.com/some-du6e/clack/internal/ui/shortcut"
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

// typeAt types s into the focused field as Gio's Window.EditorInsert
// does: the edit at the selection, then the caret after it.
func (e *perfEnv) typeAt(s string) {
	sel := e.r.EditorState().Selection.Range
	pos := min(sel.Start, sel.End) + utf8.RuneCountInString(s)
	e.r.Queue(key.EditEvent{Range: sel, Text: s}, key.SelectionEvent{Start: pos, End: pos})
	e.frame()
}

// typeKonami types the Konami code as a keyboard does: each key's press
// and release, and B and A also the text they type, a frame after each.
func (e *perfEnv) typeKonami() {
	for _, n := range konamiCode {
		e.key(n, 0)
		if n == "B" || n == "A" {
			e.typeAt(strings.ToLower(string(n)))
		}
		e.frame()
	}
}

// savedInspectTool is ui.json's inspect_tool in dir.
func savedInspectTool(t *testing.T, dir string) bool {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(dir, "ui.json"))
	if err != nil {
		return false
	}
	var p prefs
	if err := json.Unmarshal(b, &p); err != nil {
		t.Fatalf("ui.json: %v", err)
	}
	return p.InspectTool
}

// hyperD presses the tool's shortcut.
func (e *perfEnv) hyperD() {
	f := inspectFilter()
	e.key(f.Name, f.Required)
}

// startSession presses the shortcut and points at the pill.
func (e *inspectEnv) startSession(t *testing.T) {
	t.Helper()
	e.hyperD()
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

// fakeShotRegions are the regions the fake capture was asked for.
var fakeShotRegions []image.Rectangle

// fakeShots replaces the headless capture and the clipboard (no GPU or
// clipboard in plain CI): the shot is plain white.
func fakeShots(t *testing.T) (shots *int, copied *image.Image) {
	shots, copied = new(int), new(image.Image)
	prevShoot, prevWrite := inspect.Shoot, inspect.WriteImage
	fakeShotRegions = nil
	inspect.Shoot = func(_ *op.Ops, region image.Rectangle, k float32) (*image.RGBA, error) {
		*shots++
		fakeShotRegions = append(fakeShotRegions, region)
		img := image.NewRGBA(image.Rectangle{Max: inspect.ShotSize(region, k)})
		draw.Draw(img, img.Bounds(), image.NewUniform(color.White), image.Point{}, draw.Src)
		return img, nil
	}
	inspect.WriteImage = func(img image.Image) error { *copied = img; return nil }
	t.Cleanup(func() { inspect.Shoot, inspect.WriteImage = prevShoot, prevWrite })
	return shots, copied
}

// pickAndCopy clicks p: the click's frame is shot, the review opens
// over it, and Enter copies the report.
func (e *inspectEnv) pickAndCopy(t *testing.T, p image.Point) {
	t.Helper()
	e.click(p)
	e.frame()
	e.a.inspectAfterFrame(&e.ops, e.size)
	e.reviewAndCopy(t)
}

// reviewAndCopy finishes the review the last frame's shot started.
func (e *inspectEnv) reviewAndCopy(t *testing.T) {
	t.Helper()
	if !e.a.insp.Reviewing() {
		t.Fatal("no review after the capture")
	}
	e.frame()
	e.key(key.NameReturn, 0)
	e.frame()
	e.a.insp.Wait()
	e.frame()
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

	// the click picks: one shot, reviewed, then on Enter one image on the
	// clipboard and the session over
	p := e.pillCenter()
	e.pickAndCopy(t, image.Pt(int(p.X), int(p.Y)))
	if e.a.insp.Active() {
		t.Error("the session is still running after the pick")
	}
	if *shots != 1 || *copied == nil {
		t.Fatalf("after the pick: %d shots, image copied %v", *shots, *copied != nil)
	}
	// the image is the pill and its margin (the fake shot is white, the
	// panel's colour beside it), drawn ShotDensity times as dense as this
	// 1x window, over the report: nothing else of the window
	crop := r.Inset(-inspect.CropMargin).Intersect(image.Rectangle{Max: e.size})
	want := inspect.ShotSize(crop, inspect.ShotDensity)
	img := (*copied).(*image.RGBA)
	b := img.Bounds()
	white := color.RGBA{0xff, 0xff, 0xff, 0xff}
	shotW, shotH, left := 0, 0, -1
	for x := 0; x < b.Dx(); x++ {
		if img.RGBAAt(x, 2) == white {
			shotW++
			if left < 0 {
				left = x
			}
		}
	}
	for y := 0; left >= 0 && y < b.Dy() && img.RGBAAt(left+2, y) == white; y++ {
		shotH++ // down the margin left of the pill, to the band over the panel
	}
	abs := func(v int) int { return max(v, -v) }
	if abs(shotW-want.X) > 2*inspect.ShotDensity || abs(shotH-want.Y) > 2*inspect.ShotDensity {
		t.Errorf("the copied image's shot is %dx%d px, want the pill's crop, %v", shotW, shotH, want)
	}
	if shotW/inspect.ShotDensity*5 > e.size.X {
		t.Errorf("the copied image shows %d dp of the %d dp window: more than the pill", shotW/inspect.ShotDensity, e.size.X)
	}
	if b.Dx() < shotW || b.Dx() >= e.size.X*inspect.ShotDensity/2 || b.Dy() <= shotH+100 {
		t.Errorf("the copied image is %v (shot %dx%d): want the pill's crop with the report under it, far narrower than the window at that density (%d px)", b, shotW, shotH, e.size.X*inspect.ShotDensity)
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
	e.hyperD()
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

// The Konami code typed into the focused composer turns the tool on at
// once, with no CLACK_INSPECT and no pref: the composer still gets every
// key (its caret moves, B and A type), the tool only watches them (Gio
// patch 28). It's kept in ui.json, which the next launch reads; Hyper+D
// then starts a session and again ends it (on macOS the old ⌘⌥⇧D
// doesn't; elsewhere it was Ctrl+Alt+Shift+D already); the code again
// turns it off, in ui.json too.
func TestInspectKonami(t *testing.T) {
	t.Setenv("CLACK_INSPECT", "")
	t.Setenv("YOLOSLACK_INSPECT", "")
	fs, err := fakeslack.Start(fakeslack.Small)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { fs.Close() })
	e := startEnv(t, fs, t.TempDir())
	e.waitReady()
	ch := fs.ChannelIDs()[0]
	e.openChannel(ch)
	e.until("the composer", 20*time.Second, func() bool {
		cv := e.a.convos[ch+"/"]
		return cv != nil && cv.comp != nil
	})
	if e.a.insp != nil {
		t.Fatal("the tool exists before the code")
	}
	e.focusComposer(ch, "")
	cb, ok := e.a.convos[ch+"/"].comp.(composerBox)
	if !ok {
		t.Fatalf("the composer is a %T", e.a.convos[ch+"/"].comp)
	}
	// clear any draft fakeslack seeded on this channel so the typed text
	// starts clean (the first Small channel has one; without this the
	// composer reads "synthetic drafthello").
	e.key("A", key.ModShortcut)
	e.key(key.NameDeleteBackward, 0)
	e.frame()
	e.typeAt("hello")
	e.key(key.NameLeftArrow, 0)
	e.frame()
	e.key(key.NameLeftArrow, 0)
	e.frame()
	if got := cb.DocText(); got != "hello" {
		t.Fatalf("the composer has %q, want hello", got)
	}
	hyper := func() {
		e.hyperD()
		e.frame()
	}
	hyper()
	if e.a.insp != nil {
		t.Fatal("Hyper+D made the tool before the code")
	}

	// hel|lo: ↑ ↑ go to the start, ↓ ↓ to the end, ← → ← → stay there and
	// B A type: helloba only if the composer got the arrows
	e.typeKonami()
	if e.a.insp == nil {
		t.Fatal("the Konami code didn't turn the tool on")
	}
	if got := cb.DocText(); got != "helloba" {
		t.Errorf("after the code the composer has %q, want helloba (its caret moves with the code's arrows)", got)
	}
	if !strings.HasPrefix(e.a.toast.text, "Inspect tool on") {
		t.Errorf("toast %q", e.a.toast.text)
	}
	e.a.flushPrefs()
	if !savedInspectTool(t, e.home) {
		t.Error("ui.json has no inspect_tool after the code")
	}
	// the next launch (last: a second App takes over the package's
	// hooks) reads this ui.json
	next := t.TempDir()
	if b, err := os.ReadFile(filepath.Join(e.home, "ui.json")); err != nil {
		t.Fatal(err)
	} else if err := os.WriteFile(filepath.Join(next, "ui.json"), b, 0o600); err != nil {
		t.Fatal(err)
	}

	hyper()
	if !e.a.insp.Active() {
		t.Fatal("Hyper+D started no session after the code")
	}
	hyper()
	if e.a.insp.Active() {
		t.Error("Hyper+D again didn't end the session")
	}
	if shortcut.Mac() {
		e.key("D", key.ModCommand|key.ModAlt|key.ModShift) // the old shortcut
		e.frame()
		if e.a.insp.Active() {
			t.Error("the old ⌘⌥⇧D started a session")
		}
	}

	e.typeKonami()
	if e.a.insp != nil {
		t.Error("the code again left the tool on")
	}
	if !strings.HasPrefix(e.a.toast.text, "Inspect tool off") {
		t.Errorf("toast %q", e.a.toast.text)
	}
	e.a.flushPrefs()
	if savedInspectTool(t, e.home) {
		t.Error("ui.json still has inspect_tool after the code again")
	}
	if a := New(Options{ConfigDir: next, NoRealtime: true}, imgcache.New(fs.Fetch, "", nil)); a.insp == nil {
		t.Error("a launch with the ui.json the code wrote has no tool")
	}
}

// The code's keys in order, whatever comes between runs of them: a third
// ↑ keeps ↑ ↑, a modifier starts it over, Shift doesn't.
func TestKonamiSequence(t *testing.T) {
	press := func(k *konami, names ...key.Name) (done int) {
		for _, n := range names {
			if k.press(key.Event{Name: n, State: key.Press}) {
				done++
			}
		}
		return done
	}
	code := konamiCode[:]
	up, down, left, right := key.NameUpArrow, key.NameDownArrow, key.NameLeftArrow, key.NameRightArrow
	for _, c := range []struct {
		name string
		keys []key.Name
		want int
	}{
		{"the code", code, 1},
		{"twice", append(append([]key.Name{}, code...), code...), 2},
		{"three ups first", append([]key.Name{up}, code...), 1},
		{"noise first", append([]key.Name{"X", down, "B", up}, code...), 1},
		{"one key short", code[:len(code)-1], 0},
		{"a wrong key inside", []key.Name{up, up, down, down, left, right, "X", left, right, "B", "A"}, 0},
		{"B A again", append(append([]key.Name{}, code...), "B", "A"), 1},
		{"the arrows again then B A", []key.Name{up, up, down, down, left, right, up, up, down, down, left, right, left, right, "B", "A"}, 1},
	} {
		var k konami
		if got := press(&k, c.keys...); got != c.want {
			t.Errorf("%s: completed %d times, want %d", c.name, got, c.want)
		}
	}
	var k konami
	for _, n := range code[:len(code)-1] {
		k.press(key.Event{Name: n, State: key.Press})
	}
	if k.press(key.Event{Name: "A", Modifiers: key.ModShortcut, State: key.Press}) {
		t.Error("Mod+A completed the code")
	}
	k = konami{}
	for _, n := range code[:len(code)-1] {
		k.press(key.Event{Name: n, State: key.Press})
	}
	if !k.press(key.Event{Name: "A", Modifiers: key.ModShift, State: key.Press}) {
		t.Error("a capital A didn't complete the code")
	}
}

// Hyper+D is Mod+Alt+Ctrl+Shift+D: ⌃⌥⇧⌘D on macOS, Ctrl+Alt+Shift+D
// elsewhere, where a Hyper key may hold Super as well.
func TestInspectComboIsHyperD(t *testing.T) {
	for _, c := range []struct {
		goos, label string
		mods        key.Modifiers
	}{
		{"darwin", "⌃⌥⇧⌘D", key.ModCommand | key.ModCtrl | key.ModAlt | key.ModShift},
		{"linux", "Ctrl+Alt+Shift+D", key.ModCtrl | key.ModAlt | key.ModShift},
		{"windows", "Ctrl+Alt+Shift+D", key.ModCtrl | key.ModAlt | key.ModShift},
	} {
		prev := shortcut.GOOS
		shortcut.GOOS = c.goos
		f, hint := inspectFilter(), inspectHint()
		shortcut.GOOS = prev
		if f.Name != "D" || f.Required != c.mods || f.Optional != key.ModSuper {
			t.Errorf("%s: filter %+v, want D with %v (Super optional)", c.goos, f, c.mods)
		}
		if hint != c.label {
			t.Errorf("%s: shown as %q, want %q", c.goos, hint, c.label)
		}
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
		hover, err := inspect.Shoot(&e.ops, image.Rectangle{Max: e.size}, 1)
		if err != nil {
			t.Fatalf("%s: the headless capture: %v", mode, err)
		}
		save("inspect-"+mode+"-hover", hover)
		p := e.pillCenter()
		e.pickAndCopy(t, image.Pt(int(p.X), int(p.Y)))
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

// drag presses at from and moves the pointer pressed through the
// points, a frame after each; release lets go.
func (e *perfEnv) drag(from image.Point, through ...image.Point) {
	fp := f32.Pt(float32(from.X), float32(from.Y))
	e.r.Queue(pointer.Event{Kind: pointer.Move, Source: pointer.Mouse, Position: fp},
		pointer.Event{Kind: pointer.Press, Source: pointer.Mouse, Buttons: pointer.ButtonPrimary, Position: fp})
	e.frame()
	last := fp
	for _, p := range through {
		last = f32.Pt(float32(p.X), float32(p.Y))
		e.r.Queue(pointer.Event{Kind: pointer.Move, Source: pointer.Mouse, Buttons: pointer.ButtonPrimary, Position: last})
		e.frame()
	}
}

func (e *perfEnv) release(p image.Point) {
	e.r.Queue(pointer.Event{Kind: pointer.Release, Source: pointer.Mouse, Position: f32.Pt(float32(p.X), float32(p.Y))})
}

// In a session a drag grabs the region dragged out, over the message
// list, whatever is under it: the shot is that region, the review opens,
// and Enter copies a report of the region (no component, the shell's
// areas, no content) while the pill under the drag never reacts. Esc
// mid-drag drops only the drag: the picker and its ↑ go on as before.
// While a session runs the window has no system drag area (the top nav's
// on macOS), which would take the press first.
func TestInspectDragRegion(t *testing.T) {
	shots, copied := fakeShots(t)
	e := newInspectEnv(t)
	e.startSession(t)
	pc := e.pillCenter()
	p := image.Pt(int(pc.X), int(pc.Y))

	// Esc mid-drag: the drag goes, the session stays, ↑ still widens
	e.drag(p, p.Add(image.Pt(30, 20)))
	if r, ok := e.a.insp.Dragging(); !ok || r != (image.Rectangle{Min: p, Max: p.Add(image.Pt(30, 20))}) {
		t.Fatalf("mid-drag: region %v (%v), want %v-%v", r, ok, p, p.Add(image.Pt(30, 20)))
	}
	e.key(key.NameEscape, 0)
	e.frame()
	if _, ok := e.a.insp.Dragging(); ok || !e.a.insp.Active() {
		t.Fatalf("Esc mid-drag: dragging %v, active %v", ok, e.a.insp.Active())
	}
	e.release(p.Add(image.Pt(30, 20)))
	e.frame()
	e.move(pc)
	e.frame()
	if label, _, _ := e.a.insp.Hovered(); !strings.HasPrefix(label, "UnstyledButton") {
		t.Errorf("after the dropped drag: outline %q, want the pill's UnstyledButton", label)
	}
	e.key(key.NameUpArrow, 0)
	e.frame()
	if label, _, _ := e.a.insp.Hovered(); !strings.HasPrefix(label, "Reaction") {
		t.Errorf("↑ after the dropped drag: outline %q, want the Reaction", label)
	}
	if *shots != 0 {
		t.Fatalf("%d shots before any capture", *shots)
	}

	// the region: from left of the pill and above it, across it
	from := p.Add(image.Pt(-60, -40))
	to := p.Add(image.Pt(140, 50))
	e.drag(from, from.Add(image.Pt(3, 2)), p, to)
	e.release(to)
	e.frame()
	e.a.inspectAfterFrame(&e.ops, e.size)
	want := image.Rectangle{Min: from, Max: to}
	if *shots != 1 || fakeShotRegions[0] != want {
		t.Fatalf("after the drag: %d shots %v, want the region %v", *shots, fakeShotRegions, want)
	}
	e.reviewAndCopy(t)
	if e.a.insp.Active() || *copied == nil {
		t.Fatalf("after Enter: active %v, copied %v", e.a.insp.Active(), *copied != nil)
	}
	txt, _ := e.readReport(t)
	rows := reportRows(txt)
	for _, w := range []struct{ key, want string }{
		{"capture", "dragged region"},
		{"rect", "x " + itoa(from.X) + " y " + itoa(from.Y) + " w 200 h 90 dp"},
		{"region", "*primary*"},
		{"page", "c/ch:* (public)"},
	} {
		if got := rows[w.key]; !globMatch(w.want, got) {
			t.Errorf("report row %s = %q, want %q\n%s", w.key, got, w.want, txt)
		}
	}
	if _, ok := rows["component"]; ok {
		t.Errorf("a region's report names a component:\n%s", txt)
	}
	for _, leak := range append([]string{e.ch, e.ts, "launch", "friday", "quiet", "tada"}, e.names...) {
		if strings.Contains(txt, leak) {
			t.Errorf("the report holds %q:\n%s", leak, txt)
		}
	}
	e.settle(300 * time.Millisecond)
	if got := e.reactions(); len(got) != 0 {
		t.Errorf("the drag across the pill toggled the reaction: %v", got)
	}

	// no system drag area while a session runs (macOS: the top nav's)
	e.a.macNav = true
	t.Cleanup(func() { e.a.macNav = false })
	e.settle(100 * time.Millisecond)
	nav := image.Pt(e.a.searchPill.Max.X+40, 20)
	if a, ok := e.actionAt(nav); !ok || a != system.ActionMove {
		t.Fatalf("no drag area at %v in the top nav without a session (%v %v)", nav, a, ok)
	}
	e.hyperD()
	e.frame()
	e.frame()
	if a, ok := e.actionAt(nav); ok {
		t.Errorf("a session left the system action %v at %v: the press would move the window", a, nav)
	}
	e.key(key.NameEscape, 0)
	e.frame()
	e.frame()
	if a, ok := e.actionAt(nav); !ok || a != system.ActionMove {
		t.Errorf("after the session the top nav has no drag area (%v %v)", a, ok)
	}
}
