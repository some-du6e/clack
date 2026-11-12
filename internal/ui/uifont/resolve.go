package uifont

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"strings"
	"sync"

	"gioui.org/font"
	gtfont "github.com/go-text/typesetting/font"
	gtot "github.com/go-text/typesetting/font/opentype"
	"github.com/go-text/typesetting/font/opentype/tables"

	"github.com/some-du6e/clack/internal/ui/theme"
	"github.com/some-du6e/clack/internal/ui/uiguard"
)

// face is one font file that can serve a family: a static face at one
// weight or a variable one over a range, like an @font-face rule.
type face struct {
	data   []byte
	index  int
	italic bool
	wmin   float32
	wmax   float32
	axes   map[string][3]float32 // fvar: min, default, max
	group  int                   // 0: the family itself; >0: a unicode-range companion
	source string                // file name or path (tests, logs)
}

// Loader resolves options to faces.
type Loader struct {
	GOOS string
	// CacheDir keeps CDN font files (<user cache>/clack/fonts); "" keeps
	// nothing on disk.
	CacheDir string
	// Fetch downloads a CDN URL (nil: fetchCDN, a cookieless GET).
	Fetch func(ctx context.Context, url string) ([]byte, error)
	// Offline only uses CDN files already in CacheDir (startup, so the
	// first frame never waits on the network).
	Offline bool
	// System lists installed fonts (nil: the OS's font directories).
	System SystemFonts
	// ReadFile reads a system font file (nil: os.ReadFile).
	ReadFile func(path string) ([]byte, error)

	mu  sync.Mutex
	mem map[string][]byte
}

// ErrUnavailable means a CDN file wasn't cached and Offline forbade the
// fetch: load again online.
var ErrUnavailable = errors.New("uifont: not cached")

// Font is a resolved option.
type Font struct {
	Option Option
	// UI is what the theme draws with; nil is the embedded Lato.
	UI *theme.UIFont
	// Family is the family that serves it: the option's own, or a
	// fallback ("Liberation Sans" for Arial on Linux, "Times New Roman"
	// as Chromium's standard font, "Lato").
	Family string
	// Fallback reports that Family isn't the option's first choice.
	Fallback bool
	// Pending is set when Offline skipped a CDN file the option needs:
	// load again online to get it.
	Pending bool
}

// Load resolves o. It never fails for a missing font (Slack's CSS falls
// back, and so does Load); it returns an error only when ctx is done.
func (l *Loader) Load(ctx context.Context, o Option) (*Font, error) {
	uiguard.OffUI("font load")
	if !UsesThemeFont(o) {
		return &Font{Option: o, Family: "Lato"}, nil
	}
	bold, black := Weights(o)
	fams := families(o.FontFamily)
	pending := false
	for i, fam := range fams {
		fs, name, err := l.family(ctx, fam)
		if err != nil {
			if ctxErr := ctx.Err(); ctxErr != nil {
				return nil, ctxErr
			}
			if errors.Is(err, ErrUnavailable) {
				pending = true
			}
			continue
		}
		if fs == nil {
			continue
		}
		if fs[0] == nil { // the embedded Lato
			return &Font{Option: o, Family: "Lato", Fallback: i > 0, Pending: pending}, nil
		}
		return &Font{Option: o, UI: build(o, fs, bold, black), Family: name, Fallback: i > 0 || pending, Pending: pending}, nil
	}
	// The list ran out: Chromium's standard font, then its last resort
	// (FontFallbackList::GetFontData), then Clack's own Lato.
	for _, fam := range lastResort(l.goos()) {
		if fs, name := l.installed(fam); len(fs) > 0 {
			return &Font{Option: o, UI: build(o, fs, bold, black), Family: name, Fallback: true, Pending: pending}, nil
		}
	}
	return &Font{Option: o, Family: "Lato", Fallback: true, Pending: pending}, nil
}

func (l *Loader) goos() string {
	if l.GOOS == "" {
		return defaultGOOS
	}
	return l.GOOS
}

// family resolves one name of a font-family list: Slack's @font-face
// families, CSS generics, the macOS system font, then installed fonts
// (with Chromium's metric-compatible substitutes on Linux). nil faces
// (and no error) means the family isn't there; a nil first face is the
// embedded Lato.
func (l *Loader) family(ctx context.Context, name string) ([]*face, string, error) {
	key := strings.ToLower(name)
	switch key {
	case "var(--font-family-lato)", "slack-lato", "lato":
		return []*face{nil}, "Lato", nil
	case "apple color emoji", "segoe ui emoji":
		return nil, "", nil // emoji are images in Slack and Clack
	}
	if rules, ok := webFonts[key]; ok {
		fs, err := l.webFamily(ctx, rules)
		if err != nil {
			return nil, "", err
		}
		return fs, rules[0].family, nil
	}
	goos := l.goos()
	if g, ok := genericFamily(goos, key); ok {
		return l.family(ctx, g)
	}
	if key == "-apple-system" || key == "blinkmacsystemfont" {
		if goos != "darwin" {
			return nil, "", nil
		}
		fs := l.files(appleSystemFont, 0)
		if len(fs) == 0 {
			return nil, "", nil
		}
		return fs, "San Francisco Pro", nil
	}
	fs, n := l.installed(name)
	return fs, n, nil
}

// installed finds an installed family, or on Linux a metric-compatible
// one.
func (l *Loader) installed(name string) ([]*face, string) {
	names := []string{name}
	if l.goos() == "linux" {
		names = append(names, metricCompatible(name)...)
	}
	for _, n := range names {
		if fs := l.system(n); len(fs) > 0 {
			return fs, n
		}
	}
	return nil, ""
}

// system loads an installed family's faces.
func (l *Loader) system(family string) []*face {
	sys := l.System
	if sys == nil {
		sys = osFonts()
	}
	var out []*face
	for _, f := range sys.Faces(family) {
		out = append(out, l.files([]string{f.Path}, f.Index)...)
	}
	return out
}

// files loads the faces of font files (index: the collection member;
// -1 every member).
func (l *Loader) files(paths []string, index int) []*face {
	read := l.ReadFile
	if read == nil {
		read = os.ReadFile
	}
	var out []*face
	for _, p := range paths {
		b, err := l.cached(p, func() ([]byte, error) { return read(p) })
		if err != nil {
			continue
		}
		lds, err := gtot.NewLoaders(bytes.NewReader(b))
		if err != nil {
			continue
		}
		for i, ld := range lds {
			if index >= 0 && i != index {
				continue
			}
			d, _ := gtfont.Describe(ld, nil)
			f := &face{data: b, index: i, source: p, axes: axesOf(ld),
				italic: d.Aspect.Style == gtfont.StyleItalic, wmin: float32(d.Aspect.Weight), wmax: float32(d.Aspect.Weight)}
			if f.wmin == 0 {
				f.wmin, f.wmax = 400, 400
			}
			if a, ok := f.axes["wght"]; ok {
				f.wmin, f.wmax = a[0], a[2]
			}
			out = append(out, f)
		}
	}
	return out
}

// cached memoizes file bytes per loader (a font switch reads each once).
func (l *Loader) cached(key string, get func() ([]byte, error)) ([]byte, error) {
	l.mu.Lock()
	b, ok := l.mem[key]
	l.mu.Unlock()
	if ok {
		return b, nil
	}
	b, err := get()
	if err != nil {
		return nil, err
	}
	l.mu.Lock()
	if l.mem == nil {
		l.mem = map[string][]byte{}
	}
	l.mem[key] = b
	l.mu.Unlock()
	return b, nil
}

func axesOf(ld *gtot.Loader) map[string][3]float32 {
	raw, err := ld.RawTable(gtot.MustNewTag("fvar"))
	if err != nil || len(raw) == 0 {
		return nil
	}
	fv, _, err := tables.ParseFvar(raw)
	if err != nil {
		return nil
	}
	m := map[string][3]float32{}
	for _, a := range fv.Axis {
		m[a.Tag.String()] = [3]float32{a.Minimum, a.Default, a.Maximum}
	}
	return m
}

// build fills theme.Slots from a family's faces with CSS font matching:
// the slot's weight (bold and black as Slack's Theme maps them for this
// font) and style pick a face, and a variable face is instanced at the
// weight (clamped to its range, like a font-weight range descriptor).
func build(o Option, fs []*face, bold, black float32) *theme.UIFont {
	groups := map[int][]*face{}
	maxG := 0
	for _, f := range fs {
		groups[f.group] = append(groups[f.group], f)
		maxG = max(maxG, f.group)
	}
	u := &theme.UIFont{Name: o.Value, Tracking: Tracks(o)}
	for i, s := range theme.Slots {
		u.Faces[i] = slotFace(groups[0], s, cssWeight(s.Weight, bold, black), "")
	}
	for g := 1; g <= maxG && g <= len(theme.ExtFamilies); g++ {
		if len(groups[g]) == 0 {
			continue
		}
		for _, s := range theme.Slots {
			u.Ext = append(u.Ext, slotFace(groups[g], s, cssWeight(s.Weight, bold, black), theme.ExtFamilies[g-1]))
		}
	}
	return u
}

// cssWeight is the CSS weight Slack asks for where Clack asks for w.
func cssWeight(w font.Weight, bold, black float32) float32 {
	switch {
	case w >= font.Black:
		return black
	case w >= font.Bold:
		return bold
	}
	return float32(w) + 400
}

func slotFace(fs []*face, s theme.Slot, w float32, family string) theme.FaceSlot {
	italic := s.Style == font.Italic
	f, at := match(fs, w, italic)
	out := theme.FaceSlot{Family: family, Slot: s, Data: f.data, Index: f.index}
	if len(f.axes) == 0 {
		return out
	}
	if a, ok := f.axes["wght"]; ok {
		out.Vars = append(out.Vars, gtfont.Variation{Tag: gtot.MustNewTag("wght"), Value: clamp(at, a[0], a[2])})
	}
	if a, ok := f.axes["opsz"]; ok {
		// font-optical-sizing:auto sets opsz to the font size: Slack's
		// 15px body text
		out.Vars = append(out.Vars, gtfont.Variation{Tag: gtot.MustNewTag("opsz"), Value: clamp(15, a[0], a[2])})
	}
	if a, ok := f.axes["ital"]; ok && italic && !f.italic {
		// No italic face: Chrome slants the upright one (font-synthesis).
		// Gio can't, so a font with an ital axis uses its own italic.
		out.Vars = append(out.Vars, gtfont.Variation{Tag: gtot.MustNewTag("ital"), Value: a[2]})
	}
	return out
}

func clamp(v, lo, hi float32) float32 { return max(lo, min(hi, v)) }

// match is CSS Fonts 4 §5.2 font matching within a family: the style
// first (italic, else the upright faces, which Chrome would slant), then
// the weight. It returns the face and the weight to instance it at.
func match(fs []*face, w float32, italic bool) (*face, float32) {
	var cand []*face
	for _, f := range fs {
		if f.italic == italic {
			cand = append(cand, f)
		}
	}
	if len(cand) == 0 {
		cand = fs
	}
	for _, f := range cand {
		if w >= f.wmin && w <= f.wmax {
			return f, w
		}
	}
	// nearest below (largest max under w) / above (smallest min over w)
	below := func(lim float32) (*face, float32) {
		var best *face
		for _, f := range cand {
			if f.wmax < w && f.wmax >= lim && (best == nil || f.wmax > best.wmax) {
				best = f
			}
		}
		if best == nil {
			return nil, 0
		}
		return best, best.wmax
	}
	above := func(lim float32) (*face, float32) {
		var best *face
		for _, f := range cand {
			if f.wmin > w && f.wmin <= lim && (best == nil || f.wmin < best.wmin) {
				best = f
			}
		}
		if best == nil {
			return nil, 0
		}
		return best, best.wmin
	}
	var order []func() (*face, float32)
	switch {
	case w >= 400 && w <= 500:
		order = []func() (*face, float32){func() (*face, float32) { return above(500) }, func() (*face, float32) { return below(0) }, func() (*face, float32) { return above(1e9) }}
	case w < 400:
		order = []func() (*face, float32){func() (*face, float32) { return below(0) }, func() (*face, float32) { return above(1e9) }}
	default:
		order = []func() (*face, float32){func() (*face, float32) { return above(1e9) }, func() (*face, float32) { return below(0) }}
	}
	for _, step := range order {
		if f, at := step(); f != nil {
			return f, at
		}
	}
	return cand[0], cand[0].wmin
}

func (f *face) String() string {
	it := ""
	if f.italic {
		it = " italic"
	}
	return fmt.Sprintf("%s#%d %g-%g%s", f.source, f.index, f.wmin, f.wmax, it)
}
