package popups

import (
	"bytes"
	"context"
	"errors"
	"image"
	"image/color"
	_ "image/gif" // decoders for image.Decode
	_ "image/jpeg"
	_ "image/png"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"time"
	"unicode"
	"unicode/utf8"

	"gioui.org/io/key"
	"gioui.org/io/pointer"
	"gioui.org/layout"
	"gioui.org/op/clip"
	"gioui.org/op/paint"
	"gioui.org/unit"
	"gioui.org/widget"
	xdraw "golang.org/x/image/draw"
	_ "golang.org/x/image/webp"

	"github.com/some-du6e/clack/internal/i18n"
	"github.com/some-du6e/clack/internal/ui/theme"
	"github.com/some-du6e/clack/internal/ui/uiguard"
)

// Measured from the 2026 Slack client's "Add emoji" dialog (addemoji.png,
// 1x): the standard 520px modal (title 22/900, 70px header, padding 28).
// Body coordinates below are relative to the body origin (28, 70):
//   - tabs "Custom emoji" | "Emoji packs": 13/700, text at x 4 and 28px
//     apart, centered on y 4; active #1d1c1d with a 2px #83388a underline
//     under the text (y 18..20), inactive #454447; a full-width 1px #eaeaea
//     rule at y 19 (it runs edge to edge across the card).
//   - intro at y 36, 15/22 #1d1c1d, team name bold.
//   - "1. Upload an image" 15/700 at y 127, help 15/22 #454447 indented 16.
//   - 16 below: two 64x64 tiles at x 16 and 88 (radius 2; light #f8f8f8
//     with a 1px #e4e4e4 ring, dark #1d1c1d with #252425), a 20px image
//     icon centered; at x 168 "Select an image" 15/22 #454447 on top and
//     the 36px "Upload Image" outline button 27 below it.
//   - 24 below the tiles a 1px #dddddd rule, then 16, "2. Give it a name",
//     help, 16, a 36px input (x 16 to the right edge, radius 8, 1px
//     #7c7a7f) showing ":avocado:" as placeholder, then 28 to the footer.
//   - footer Cancel / Save (Save disabled #eaeaea until it can save).

const (
	emojiAddMaxBytes  = 1 << 20  // reject files over 1 MB
	emojiAddMaxPixels = 40 << 20 // refuse decompression bombs
	emojiAddMaxName   = 100
	emojiAddThumbPx   = 256 // preview texture size cap
)

var emojiAddErrNoChooser = errors.New("no file chooser")

// emojiAddWord is one word of the intro paragraph (widths cached).
type emojiAddWord struct {
	s    string
	bold bool
	glue bool // no space after it (it runs on into the next word)
	w    int
}

// emojiAddFile is a loaded, validated image.
type emojiAddFile struct {
	name string // base name
	data []byte // original bytes (uploaded as-is; Slack resizes)
	op   paint.ImageOp
}

// emojiAddCmd is a native file chooser invocation.
type emojiAddCmd struct {
	name string
	args []string
}

type emojiAddModal struct {
	h     Host
	modal *Modal

	tab    int
	tabBtn [2]widget.Clickable

	words  []emojiAddWord
	wordM  unit.Metric
	spaceW int

	chooser    emojiAddCmd
	hasChooser bool
	upload     widget.Clickable
	pathEd     widget.Editor // fallback when no chooser exists
	useBtn     widget.Clickable

	busy   bool // picking or loading
	file   string
	data   []byte
	img    paint.ImageOp
	imgErr string

	ed       widget.Editor
	fieldBtn widget.Clickable
	name     string
	taken    map[string]struct{} // nil until built
	saving   bool
}

// NewAddEmojiModal builds Slack's "Add emoji" dialog (custom emoji upload).
// Show it with h.ShowOverlay.
func NewAddEmojiModal(h Host) *Modal {
	s := &emojiAddModal{h: h}
	s.ed.SingleLine = true
	s.ed.Submit = true
	s.ed.MaxLen = emojiAddMaxName
	s.pathEd.SingleLine = true
	s.pathEd.Submit = true
	team, bold := "this", false
	if a := h.Core(); a != nil {
		select {
		case <-a.Ready():
			if n := strings.TrimSpace(a.Snapshot().Team.Name); n != "" {
				team, bold = n, true
			}
		default:
		}
	}
	s.words = emojiAddWords(team, bold)
	// The PATH lookup is file I/O: it runs once in the background. Until it
	// answers, "Upload Image" shows (the usual case) and pick waits for it.
	s.chooser, s.hasChooser = emojiChooserCached()
	s.modal = NewModal(h, ModalOpts{
		Title: nsEmojiList.T("Add emoji"),
		Body:  s.layout,
		Buttons: []*ModalButton{
			{Label: nsEmojiList.T("Cancel"), Kind: BtnOutline},
			{Label: nsEmojiList.T("Save"), Kind: BtnPrimary, Enabled: s.canSave, Do: func() bool {
				s.save()
				return true
			}},
		},
	})
	s.loadTaken()
	return s.modal
}

func emojiAddWords(team string, bold bool) []emojiAddWord {
	var out []emojiAddWord
	spaceAfter := true // the text so far ends in whitespace
	add := func(txt string, b bool) {
		if txt == "" {
			return
		}
		// a word that runs on into the next part (no space between, as
		// in ja-JP) and a CJK word's characters (i18n.LineChunks) are glued:
		// no space after them, though a line may break there
		if n := len(out); n > 0 && !spaceAfter && !unicode.IsSpace([]rune(txt)[0]) {
			out[n-1].glue = true
		}
		r := []rune(txt)
		spaceAfter = unicode.IsSpace(r[len(r)-1])
		for _, f := range strings.Fields(txt) {
			cs := i18n.LineChunks(f)
			for i, c := range cs {
				out = append(out, emojiAddWord{s: c, bold: b, glue: i < len(cs)-1})
			}
		}
	}
	// "Your custom emoji will be available in every <b>{orgName}</b>
	// workspace. …" (custom_emoji_list): the org name bold, wherever the
	// locale puts it
	for _, p := range nsEmojiList.RT("Your custom emoji will be available in every <b>{orgName}</b> workspace. You’ll find it in the custom tab of the emoji picker. (Hint: it’s the one with the Slack icon!)", "orgName", team) {
		add(p.Text, bold && p.Tag == "b")
	}
	return out
}

// loadTaken builds the lowercase set of names already in use (custom map,
// which can hold ~65k entries, plus standard emoji) off the UI goroutine.
func (s *emojiAddModal) loadTaken() {
	a := s.h.Core()
	run(s.h, func(ctx context.Context) (map[string]struct{}, error) {
		std := epStandard()
		var custom map[string]string
		if a != nil {
			select {
			case <-a.Ready():
				custom = a.CustomEmoji()
			case <-ctx.Done():
			}
		}
		set := make(map[string]struct{}, len(custom)+len(std.byName))
		for n := range std.byName {
			set[strings.ToLower(n)] = struct{}{}
		}
		for n := range custom {
			set[strings.ToLower(n)] = struct{}{}
		}
		return set, nil
	}, func(m map[string]struct{}, _ error) {
		s.taken = m
	})
}

// ---- name ----

// emojiAddCleanName lowercases ASCII letters, turns spaces into
// underscores and drops colons (they're drawn around the field).
func emojiAddCleanName(t string) string {
	return strings.Map(func(r rune) rune {
		switch {
		case r >= 'A' && r <= 'Z':
			return r + ('a' - 'A')
		case r == ' ':
			return '_'
		case r == ':':
			return -1
		}
		return r
	}, t)
}

func emojiAddNameValid(n string) bool {
	if n == "" {
		return false
	}
	for i := 0; i < len(n); i++ {
		c := n[i]
		if !(c >= 'a' && c <= 'z' || c >= '0' && c <= '9' || c == '-' || c == '_') {
			return false
		}
	}
	return true
}

func (s *emojiAddModal) nameErr() string {
	if s.name == "" {
		return ""
	}
	if !emojiAddNameValid(s.name) {
		return "Names can only contain lowercase letters, numbers, hyphens and underscores."
	}
	if _, ok := s.taken[s.name]; ok {
		return "That name is already taken"
	}
	return ""
}

func (s *emojiAddModal) onNameChange() {
	t := s.ed.Text()
	n := emojiAddCleanName(t)
	if n != t {
		st, en := s.ed.Selection()
		rs := []rune(t)
		fix := func(i int) int {
			if i > len(rs) {
				i = len(rs)
			}
			if i < 0 {
				i = 0
			}
			return utf8.RuneCountInString(emojiAddCleanName(string(rs[:i])))
		}
		st, en = fix(st), fix(en)
		s.ed.SetText(n)
		s.ed.SetCaret(st, en)
	}
	s.name = n
}

func (s *emojiAddModal) canSave() bool {
	return s.data != nil && !s.busy && !s.saving && emojiAddNameValid(s.name) && s.nameErr() == ""
}

func (s *emojiAddModal) save() {
	if !s.canSave() {
		return
	}
	h, name, data := s.h, s.name, s.data
	s.saving = true
	do(h, "Emoji added", func(ctx context.Context) error { return cxAddEmoji(ctx, h, name, data) })
}

// ---- image ----

var emojiChooser struct {
	once sync.Once
	done chan struct{}
	cmd  emojiAddCmd
	ok   bool
}

// emojiChooserCached starts the chooser lookup (first call) and returns its
// answer if it's in; before that it says there is one.
func emojiChooserCached() (emojiAddCmd, bool) {
	emojiChooser.once.Do(func() {
		emojiChooser.done = make(chan struct{})
		go func() {
			emojiChooser.cmd, emojiChooser.ok = emojiAddChooser()
			close(emojiChooser.done)
		}()
	})
	select {
	case <-emojiChooser.done:
		return emojiChooser.cmd, emojiChooser.ok
	default:
		return emojiAddCmd{}, true
	}
}

// emojiAddChooser finds the platform's native file chooser (off the UI
// goroutine: PATH lookups).
func emojiAddChooser() (emojiAddCmd, bool) {
	uiguard.OffUI("exec.LookPath (emoji chooser)")
	var cands []emojiAddCmd
	switch runtime.GOOS {
	case "darwin":
		cands = []emojiAddCmd{{"osascript", []string{"-e",
			`POSIX path of (choose file of type {"public.image"} with prompt "Select an image")`}}}
	case "windows":
		ps := `Add-Type -AssemblyName System.Windows.Forms; ` +
			`$d = New-Object System.Windows.Forms.OpenFileDialog; ` +
			`$d.Title = 'Select an image'; ` +
			`$d.Filter = 'Images (*.png;*.gif;*.jpg;*.jpeg;*.webp)|*.png;*.gif;*.jpg;*.jpeg;*.webp'; ` +
			`if ($d.ShowDialog() -eq [System.Windows.Forms.DialogResult]::OK) { [Console]::Out.Write($d.FileName) }`
		cands = []emojiAddCmd{{"powershell", []string{"-NoProfile", "-STA", "-Command", ps}}}
	default:
		home, err := os.UserHomeDir()
		if err != nil || home == "" {
			home = "."
		}
		cands = []emojiAddCmd{
			{"zenity", []string{"--file-selection", "--title=Select an image",
				"--file-filter=Images | *.png *.gif *.jpg *.jpeg *.webp", "--file-filter=All files | *"}},
			{"kdialog", []string{"--title", "Select an image", "--getopenfilename", home,
				"Images (*.png *.gif *.jpg *.jpeg *.webp)"}},
		}
	}
	for _, c := range cands {
		if p, err := exec.LookPath(c.name); err == nil {
			c.name = p
			return c, true
		}
	}
	return emojiAddCmd{}, false
}

func emojiAddCleanPath(p string) string {
	p = strings.TrimSpace(p)
	if len(p) >= 2 && (p[0] == '"' && p[len(p)-1] == '"' || p[0] == '\'' && p[len(p)-1] == '\'') {
		p = p[1 : len(p)-1]
	}
	if strings.HasPrefix(p, "file://") {
		if u, err := url.Parse(p); err == nil {
			p = u.Path
		}
	}
	if p == "~" || strings.HasPrefix(p, "~/") {
		if home, err := os.UserHomeDir(); err == nil {
			p = filepath.Join(home, p[1:])
		}
	}
	return p
}

// emojiAddLoad reads and validates an image file (worker goroutine).
func emojiAddLoad(path string) (emojiAddFile, error) {
	path = emojiAddCleanPath(path)
	fi, err := os.Stat(path)
	if err != nil {
		return emojiAddFile{}, errors.New("Couldn’t open that file.")
	}
	if fi.IsDir() {
		return emojiAddFile{}, errors.New("That’s a folder, not an image.")
	}
	if fi.Size() > emojiAddMaxBytes {
		return emojiAddFile{}, errors.New("That image is over 1 MB. Try a smaller one.")
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return emojiAddFile{}, errors.New("Couldn’t read that file.")
	}
	if len(data) > emojiAddMaxBytes {
		return emojiAddFile{}, errors.New("That image is over 1 MB. Try a smaller one.")
	}
	cfg, _, err := image.DecodeConfig(bytes.NewReader(data))
	if err != nil {
		return emojiAddFile{}, errors.New("That file isn’t a PNG, GIF, JPEG or WebP image.")
	}
	if cfg.Width <= 0 || cfg.Height <= 0 || int64(cfg.Width)*int64(cfg.Height) > emojiAddMaxPixels {
		return emojiAddFile{}, errors.New("That image is too big to use as an emoji.")
	}
	img, _, err := image.Decode(bytes.NewReader(data))
	if err != nil {
		return emojiAddFile{}, errors.New("That file isn’t a PNG, GIF, JPEG or WebP image.")
	}
	return emojiAddFile{name: filepath.Base(path), data: data, op: paint.NewImageOp(emojiAddThumb(img, emojiAddThumbPx))}, nil
}

// emojiAddThumb downscales img so its longer side is at most limit px.
func emojiAddThumb(img image.Image, limit int) image.Image {
	b := img.Bounds()
	w, h := b.Dx(), b.Dy()
	if w <= limit && h <= limit {
		return img
	}
	nw, nh := limit, limit
	if w > h {
		nh = maxInt(1, h*limit/w)
	} else {
		nw = maxInt(1, w*limit/h)
	}
	dst := image.NewRGBA(image.Rect(0, 0, nw, nh))
	xdraw.CatmullRom.Scale(dst, dst.Bounds(), img, b, xdraw.Src, nil)
	return dst
}

func (s *emojiAddModal) applyFile(f emojiAddFile, err error) {
	s.busy = false
	switch {
	case errors.Is(err, emojiAddErrNoChooser):
		s.hasChooser = false // show the path field instead
	case err != nil:
		s.imgErr = err.Error()
	case f.data != nil:
		s.file, s.data, s.img, s.imgErr = f.name, f.data, f.op, ""
	}
	s.h.Invalidate()
}

// pick opens the native chooser and loads the chosen file. The dialog can
// stay open longer than run()'s 15 s timeout, so it doesn't use ctx.
func (s *emojiAddModal) pick() {
	if s.busy || !s.hasChooser {
		return
	}
	s.busy = true
	c := s.chooser
	run(s.h, func(context.Context) (emojiAddFile, error) {
		uiguard.OffUI("exec (emoji chooser)")
		if c.name == "" { // opened before the lookup answered
			<-emojiChooser.done
			if !emojiChooser.ok {
				return emojiAddFile{}, emojiAddErrNoChooser
			}
			c = emojiChooser.cmd
		}
		cmd := exec.Command(c.name, c.args...)
		cmd.WaitDelay = 2 * time.Second // never wait on a grandchild's inherited pipes
		out, err := cmd.Output()
		if err != nil {
			var ee *exec.ExitError
			if errors.As(err, &ee) {
				return emojiAddFile{}, nil // cancelled
			}
			return emojiAddFile{}, emojiAddErrNoChooser
		}
		p := strings.TrimRight(string(out), "\r\n")
		if p == "" {
			return emojiAddFile{}, nil
		}
		return emojiAddLoad(p)
	}, s.applyFile)
}

func (s *emojiAddModal) usePath() {
	p := strings.TrimSpace(s.pathEd.Text())
	if s.busy || p == "" {
		return
	}
	s.busy = true
	run(s.h, func(context.Context) (emojiAddFile, error) { return emojiAddLoad(p) }, s.applyFile)
}

// ---- layout ----

func (s *emojiAddModal) events(gtx layout.Context) {
	for i := range s.tabBtn {
		if s.tabBtn[i].Clicked(gtx) {
			s.tab = i
		}
	}
	if s.upload.Clicked(gtx) {
		s.pick()
	}
	if s.useBtn.Clicked(gtx) {
		s.usePath()
	}
	if s.fieldBtn.Clicked(gtx) {
		gtx.Execute(key.FocusCmd{Tag: &s.ed})
	}
	for {
		e, ok := s.pathEd.Update(gtx)
		if !ok {
			break
		}
		if _, ok := e.(widget.SubmitEvent); ok {
			s.usePath()
		}
	}
	for {
		e, ok := s.ed.Update(gtx)
		if !ok {
			break
		}
		switch e.(type) {
		case widget.ChangeEvent:
			s.onNameChange()
		case widget.SubmitEvent:
			if s.canSave() {
				s.save()
				s.modal.Dismiss()
				return
			}
		}
	}
}

func (s *emojiAddModal) errColor(th *theme.Theme, cl *Colors) color.NRGBA {
	if th != nil && th.Dark {
		return cl.Danger
	}
	return rgb(0xe01e5a)
}

func (s *emojiAddModal) layout(gtx layout.Context) layout.Dimensions {
	if s.modal == nil || s.modal.done {
		return layout.Dimensions{}
	}
	s.events(gtx)
	if s.modal.done {
		return layout.Dimensions{}
	}
	th := s.h.Theme()
	cl := colorsOf(th)
	w := gtx.Constraints.Max.X
	ind := dp(gtx, 16)
	lh := dp(gtx, 22)
	s.tabs(gtx, th, cl, w)
	y := dp(gtx, 36)
	if s.tab == 1 {
		d := at(gtx, image.Pt(0, y), func(gtx layout.Context) layout.Dimensions {
			gtx.Constraints = layout.Constraints{Max: image.Pt(w, 1<<20)}
			return para(gtx, th, 15, 22, theme.Regular, cl.Fg, "Emoji packs aren’t available in "+appName+" yet.", 0)
		})
		return layout.Dimensions{Size: image.Pt(w, y+d.Size.Y+dp(gtx, 28))}
	}
	y += s.intro(gtx, th, cl, w, y)
	y += dp(gtx, 25)

	heading := func(y int, txt string) {
		at(gtx, image.Pt(0, y), func(gtx layout.Context) layout.Dimensions {
			gtx.Constraints = layout.Constraints{Max: image.Pt(w, lh)}
			return vcenter(gtx, lh, func(gtx layout.Context) layout.Dimensions {
				return text(gtx, th, 15, theme.Bold, cl.Fg, txt)
			})
		})
	}
	help := func(y int, txt string) int {
		return at(gtx, image.Pt(ind, y), func(gtx layout.Context) layout.Dimensions {
			gtx.Constraints = layout.Constraints{Max: image.Pt(w-ind, 1<<20)}
			return para(gtx, th, 15, 22, theme.Regular, cl.Fg2, txt, 0)
		}).Size.Y
	}
	errLine := func(y int, txt string) int {
		return at(gtx, image.Pt(ind, y), func(gtx layout.Context) layout.Dimensions {
			gtx.Constraints = layout.Constraints{Max: image.Pt(w-ind, 1<<20)}
			return para(gtx, th, 13, 18, theme.Regular, s.errColor(th, cl), txt, 0)
		}).Size.Y
	}

	// 1. Upload an image
	heading(y, "1. "+nsEmojiList.T("Upload an image"))
	y += lh
	y += help(y, nsEmojiList.T("Square images under 128KB and with transparent backgrounds work best. If your image is too large, we’ll try to resize it for you."))
	y += dp(gtx, 16)
	s.uploadRow(gtx, th, cl, w, y)
	y += dp(gtx, 64)
	if s.imgErr != "" {
		y += dp(gtx, 8)
		y += errLine(y, s.imgErr)
	}
	y += dp(gtx, 24)
	rule := rgb(0xdddddd)
	if th != nil && th.Dark {
		rule = cl.Divider
	}
	fill(gtx, image.Rect(0, y, w, y+1), rule)
	y += 1 + dp(gtx, 16)

	// 2. Give it a name
	heading(y, "2. "+nsEmojiList.T("Give it a name"))
	y += lh
	y += help(y, nsEmojiList.T("This is also what you’ll type to add this emoji to your messages."))
	y += dp(gtx, 16)
	at(gtx, image.Pt(ind, y), func(gtx layout.Context) layout.Dimensions {
		return s.nameField(gtx, th, cl, w-ind)
	})
	y += dp(gtx, 36)
	if e := s.nameErr(); e != "" {
		y += dp(gtx, 8)
		y += errLine(y, e)
	}
	y += dp(gtx, 28)
	return layout.Dimensions{Size: image.Pt(w, y)}
}

// tabs draws the tab strip from y -12 to 20 (it rides up into the
// header's bottom padding, as in Slack) and the edge-to-edge rule.
func (s *emojiAddModal) tabs(gtx layout.Context, th *theme.Theme, cl *Colors, w int) {
	padX := dp(gtx, 28)
	fill(gtx, image.Rect(-padX, dp(gtx, 19), w+padX, dp(gtx, 19)+1), cl.Divider)
	bandH := dp(gtx, 32)
	x := dp(gtx, 4)
	for i, label := range [2]string{nsEmojiList.T("Custom emoji"), nsEmojiList.T("Emoji packs")} {
		tw := textW(gtx, th, 13, theme.Bold, label)
		active := s.tab == i
		col := cl.Fg2
		if active || s.tabBtn[i].Hovered() {
			col = cl.Fg
		}
		at(gtx, image.Pt(x, -dp(gtx, 12)), func(gtx layout.Context) layout.Dimensions {
			gtx.Constraints = layout.Exact(image.Pt(tw, bandH))
			return clickBox(gtx, &s.tabBtn[i], nil, func(gtx layout.Context) layout.Dimensions {
				return vcenter(gtx, bandH, func(gtx layout.Context) layout.Dimensions {
					return text(gtx, th, 13, theme.Bold, col, label)
				})
			})
		})
		if active {
			fill(gtx, image.Rect(x, dp(gtx, 18), x+tw, dp(gtx, 20)), cl.Aubergine)
		}
		x += tw + dp(gtx, 28)
	}
}

// intro lays out the intro paragraph (mixed weights) at y; returns its
// height.
func (s *emojiAddModal) intro(gtx layout.Context, th *theme.Theme, cl *Colors, w, y int) int {
	if s.wordM != gtx.Metric || s.spaceW == 0 {
		s.wordM = gtx.Metric
		s.spaceW = textW(gtx, th, 15, theme.Regular, "a a") - textW(gtx, th, 15, theme.Regular, "aa")
		if s.spaceW <= 0 {
			s.spaceW = dp(gtx, 4)
		}
		for i := range s.words {
			wt := theme.Regular
			if s.words[i].bold {
				wt = theme.Bold
			}
			s.words[i].w = textW(gtx, th, 15, wt, s.words[i].s)
		}
	}
	lh := dp(gtx, 22)
	x, line := 0, 0
	for _, wd := range s.words {
		if x > 0 && x+wd.w > w {
			x, line = 0, line+1
		}
		wt := theme.Regular
		if wd.bold {
			wt = theme.Bold
		}
		word := wd.s
		at(gtx, image.Pt(x, y+line*lh), func(gtx layout.Context) layout.Dimensions {
			gtx.Constraints = layout.Constraints{Max: image.Pt(w, lh)}
			return vcenter(gtx, lh, func(gtx layout.Context) layout.Dimensions {
				return text(gtx, th, 15, wt, cl.Fg, word)
			})
		})
		x += wd.w
		if !wd.glue {
			x += s.spaceW
		}
	}
	if len(s.words) == 0 {
		return 0
	}
	return (line + 1) * lh
}

// tile draws one 64px preview tile with the chosen image or the
// image placeholder icon.
func (s *emojiAddModal) tile(gtx layout.Context, r image.Rectangle, bg, ring, ic color.NRGBA) {
	rad := dp(gtx, 2)
	rrect(gtx, r, rad, ring)
	rrect(gtx, r.Inset(1), maxInt(0, rad-1), bg)
	if s.data != nil {
		in := r.Inset(dp(gtx, 4))
		at(gtx, in.Min, func(gtx layout.Context) layout.Dimensions {
			return containImage(gtx, s.img, in.Size())
		})
		return
	}
	isz := dp(gtx, 20)
	at(gtx, r.Min.Add(image.Pt((r.Dx()-isz)/2, (r.Dy()-isz)/2)), func(gtx layout.Context) layout.Dimensions {
		return icon(gtx, "image", 20, ic)
	})
}

func (s *emojiAddModal) uploadRow(gtx layout.Context, th *theme.Theme, cl *Colors, w, y int) {
	ind := dp(gtx, 16)
	t := dp(gtx, 64)
	r1 := image.Rect(ind, y, ind+t, y+t)
	s.tile(gtx, r1, rgb(0xf8f8f8), rgb(0xe4e4e4), rgb(0x616061))
	r2 := r1.Add(image.Pt(t+dp(gtx, 8), 0))
	s.tile(gtx, r2, rgb(0x1d1c1d), rgb(0x252425), rgb(0xbfbebf))

	cx := r2.Max.X + dp(gtx, 16)
	colW := w - cx
	if colW < dp(gtx, 120) {
		colW = dp(gtx, 120)
	}
	lh := dp(gtx, 22)
	label := nsEmojiList.T("Select an image")
	switch {
	case s.busy:
		label = nsEmojiList.T("Loading…")
	case s.file != "":
		label = s.file
	}
	at(gtx, image.Pt(cx, y), func(gtx layout.Context) layout.Dimensions {
		gtx.Constraints = layout.Constraints{Max: image.Pt(colW, lh)}
		return vcenter(gtx, lh, func(gtx layout.Context) layout.Dimensions {
			return text(gtx, th, 15, theme.Regular, cl.Fg2, label)
		})
	})
	by := y + dp(gtx, 27)
	if s.hasChooser {
		at(gtx, image.Pt(cx, by), func(gtx layout.Context) layout.Dimensions {
			gtx.Constraints = layout.Constraints{Max: image.Pt(colW, dp(gtx, 36))}
			return Button(gtx, th, &s.upload, BtnOutline, BtnMedium, nsEmojiList.T("Upload Image"), "", 0)
		})
		return
	}
	// no native chooser: a path field + "Use"
	useW := textW(gtx, th, 15, theme.Bold, "Use") + 2*dp(gtx, 12)
	fw := colW - useW - dp(gtx, 8)
	if fw < dp(gtx, 60) {
		fw = dp(gtx, 60)
	}
	at(gtx, image.Pt(cx, by), func(gtx layout.Context) layout.Dimensions {
		gtx.Constraints = layout.Constraints{Max: image.Pt(fw, dp(gtx, 36))}
		return field(gtx, th, &s.pathEd, "Path to an image file", "", 36, 15)
	})
	kind := BtnOutline
	if s.busy {
		kind = BtnDisabled
	}
	at(gtx, image.Pt(cx+fw+dp(gtx, 8), by), func(gtx layout.Context) layout.Dimensions {
		gtx.Constraints = layout.Constraints{Max: image.Pt(useW, dp(gtx, 36))}
		return Button(gtx, th, &s.useBtn, kind, BtnMedium, "Use", "", 0)
	})
}

// nameField is the 36px emoji name input: ":" name ":" with ":avocado:"
// as placeholder.
func (s *emojiAddModal) nameField(gtx layout.Context, th *theme.Theme, cl *Colors, w int) layout.Dimensions {
	hh := dp(gtx, 36)
	r := image.Rect(0, 0, w, hh)
	rad := dp(gtx, 8)
	// clicks on the padding focus the editor (registered under it).
	s.fieldBtn.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
		return layout.Dimensions{Size: r.Max}
	})
	if gtx.Focused(&s.ed) {
		ring := dp(gtx, 4)
		rrect(gtx, r.Inset(-ring), rad+ring, cl.FocusRing)
		rrect(gtx, r, rad, cl.Focus)
	} else {
		border := rgb(0x7c7a7f)
		if th != nil && th.Dark {
			border = cl.InputBorder
		}
		rrect(gtx, r, rad, border)
	}
	rrect(gtx, r.Inset(1), rad-1, cl.Bg)

	pad := dp(gtx, 12)
	colonW := textW(gtx, th, 15, theme.Regular, ":")
	x := pad
	edW := w - 2*pad - 2*colonW
	if edW < 1 {
		edW = 1
	}
	line := func(x int, c color.NRGBA, txt string) {
		at(gtx, image.Pt(x, 0), func(gtx layout.Context) layout.Dimensions {
			gtx.Constraints = layout.Constraints{Max: image.Pt(w-x, hh)}
			return vcenter(gtx, hh, func(gtx layout.Context) layout.Dimensions {
				return text(gtx, th, 15, theme.Regular, c, txt)
			})
		})
	}
	if s.name == "" && s.ed.Len() == 0 {
		line(x, cl.Placeholder, ":avocado:")
	} else {
		line(x, cl.Fg, ":")
		nw := textW(gtx, th, 15, theme.Regular, s.name)
		if nw > edW {
			nw = edW
		}
		line(x+colonW+nw, cl.Fg, ":")
	}
	at(gtx, image.Pt(x+colonW, 0), func(gtx layout.Context) layout.Dimensions {
		return vcenter(gtx, hh, func(gtx layout.Context) layout.Dimensions {
			gtx.Constraints = layout.Constraints{Min: image.Pt(edW, 0), Max: image.Pt(edW, hh)}
			return th.Editor(gtx, &s.ed, unit.Sp(15), cl.Fg, theme.WithAlpha(cl.Focus, 0x55))
		})
	})
	defer clip.Rect(r).Push(gtx.Ops).Pop()
	pointer.CursorText.Add(gtx.Ops)
	return layout.Dimensions{Size: r.Max}
}
