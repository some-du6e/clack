package popups

import (
	"image"
	"image/color"
	"image/png"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"gioui.org/gpu/headless"
	"gioui.org/layout"
	"gioui.org/op"
	"gioui.org/op/paint"
	"gioui.org/unit"

	"github.com/some-du6e/clack/internal/fakeslack"
	"github.com/some-du6e/clack/internal/ui/theme"
	"github.com/some-du6e/clack/internal/ui/theme/pixeltest"
)

// pvImages serves a flat tile for every image URL (no network in tests)
// and records the emoji asked for.
type pvImages struct {
	mu   sync.Mutex
	urls []string
}

func (r *pvImages) Get(url string, _ int) (paint.ImageOp, bool) {
	r.mu.Lock()
	r.urls = append(r.urls, url)
	r.mu.Unlock()
	// a yellow disc, so emoji read as separate glyphs in the screenshots
	const n = 32
	img := image.NewRGBA(image.Rect(0, 0, n, n))
	for y := 0; y < n; y++ {
		for x := 0; x < n; x++ {
			if dx, dy := x-n/2, y-n/2; dx*dx+dy*dy < n*n/4 {
				img.Set(x, y, color.NRGBA{R: 0xf5, G: 0xc0, B: 0x2a, A: 0xff})
			}
		}
	}
	return paint.NewImageOp(img), true
}

func (r *pvImages) asked(stem string) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, u := range r.urls {
		if strings.HasSuffix(u, "/"+stem+".png") {
			return true
		}
	}
	return false
}

const pvW, pvH = 402, 1500 // Slack's pane in slack-own-profile.png is 402 wide

// pvFrames lays v out until its sections arrived, then once more under a
// tofu scan, and returns what that frame drew.
func pvFrames(t *testing.T, v *ProfileView, gpu *headless.Window, shot string) theme.TofuStats {
	t.Helper()
	ops := new(op.Ops)
	frame := func() {
		ops.Reset()
		gtx := layout.Context{Ops: ops, Now: time.Date(2026, 11, 8, 15, 42, 0, 0, time.UTC),
			Metric: unit.Metric{PxPerDp: 1, PxPerSp: 1}, Constraints: layout.Exact(image.Pt(pvW, pvH))}
		bg := colorsOf(v.h.Theme()).Bg
		if v.h.Theme().Dark {
			bg = rgb(0x1a1d21)
		}
		paint.Fill(ops, bg)
		v.Layout(gtx)
	}
	for dl := time.Now().Add(10 * time.Second); !v.secs.done; {
		if time.Now().After(dl) {
			t.Fatal("users.profile.getSections never answered")
		}
		frame()
		time.Sleep(5 * time.Millisecond)
	}
	if v.secs.err != nil {
		t.Fatalf("sections: %v", v.secs.err)
	}
	frame()
	stop := theme.StartTofuScan()
	frame()
	st := stop()
	if gpu != nil && shot != "" {
		if err := gpu.Frame(ops); err != nil {
			t.Fatalf("gpu frame: %v", err)
		}
		img := image.NewRGBA(image.Rect(0, 0, pvW, pvH))
		if err := gpu.Screenshot(img); err != nil {
			t.Fatalf("screenshot: %v", err)
		}
		f, err := os.Create(filepath.Join(os.Getenv("PROFILE_SHOTS_DIR"), shot+".png"))
		if err != nil {
			t.Fatal(err)
		}
		defer f.Close()
		if err := png.Encode(f, img); err != nil {
			t.Fatal(err)
		}
	}
	return st
}

// pvGPU is a headless GPU window when PROFILE_SHOTS_DIR is set (the
// own-profile workflow), else nil.
func pvGPU(t *testing.T) *headless.Window {
	if os.Getenv("PROFILE_SHOTS_DIR") == "" {
		return nil
	}
	w, err := pixeltest.NewWindow(pvW, pvH)
	if err != nil {
		t.Fatalf("headless window: %v", err)
	}
	t.Cleanup(w.Release)
	return w
}

func has(texts []string, s string) bool { return slices.Contains(texts, s) }

func hasPrefix(texts []string, p string) bool {
	return slices.ContainsFunc(texts, func(t string) bool { return strings.HasPrefix(t, p) })
}

// The own profile pane against Slack's RimetoMemberProfileFlexpane on the
// fake's users.profile.getSections, light and dark: the real name (no
// presence dot), the emoji title drawn with Slack's images, Contact
// information with Email Address and "+ Add Phone", People with the
// manager and "+ Add Mentor", About me with the custom fields, Edit on
// each editable section, nothing Slack doesn't show (Username, Timezone,
// the title again), and no tofu anywhere.
func TestOwnProfilePane(t *testing.T) {
	gpu := pvGPU(t)
	for _, dark := range []bool{false, true} {
		h := hcEnv(t)
		imgs := &pvImages{}
		h.th.Images = imgs
		h.th.SetDark(dark)
		self, _ := lookupUser(h, fakeslack.SelfID)
		if self.Title != fakeslack.SelfTitle || self.Email != fakeslack.SelfEmail {
			t.Fatalf("fixture: title %q email %q", self.Title, self.Email)
		}
		mode := map[bool]string{false: "light", true: "dark"}[dark]
		v := NewProfileView(h, fakeslack.SelfID)
		st := pvFrames(t, v, gpu, "own-profile-"+mode)

		if st.Notdef > 0 || st.Fallback > 0 {
			t.Errorf("%s: %d .notdef and %d system-font glyphs in the pane", mode, st.Notdef, st.Fallback)
		}
		if !imgs.asked("1f631") || !imgs.asked("1f99c") {
			t.Errorf("%s: the title's 😱 or the field's 🦜 wasn't drawn as Slack's image", mode)
		}
		x := st.Texts
		for _, want := range []string{
			self.RealName, "Edit", fakeslack.SelfTitle, "Add name pronunciation",
			"Contact information", "Email Address", fakeslack.SelfEmail, "Add Phone",
			"People", "Manager", "robin", "Synthetic Fixture Wrangler", "Add Mentor",
			"About me", "Start Date", "Website", "Add GitHub", "Favorite emoji",
			"🦜 parrots, and 😱 when the build is red", "Set a status", "View as",
		} {
			if !has(x, want) {
				t.Errorf("%s: %q not drawn", mode, want)
			}
		}
		if !hasPrefix(x, "Mar 1, 2024 (") {
			t.Errorf("%s: no start date with its tenure", mode)
		}
		for _, bad := range []string{"Username", "Timezone", "@" + self.Handle, "Title", "Affiliations", "Other fields", "Header"} {
			if has(x, bad) {
				t.Errorf("%s: %q drawn (Slack shows no such thing)", mode, bad)
			}
		}
		n := 0
		for _, s := range x {
			if s == fakeslack.SelfTitle {
				n++
			}
		}
		if n != 1 {
			t.Errorf("%s: the title drawn %d times, want once", mode, n)
		}
		edits := 0
		for _, s := range x {
			if s == "Edit" {
				edits++
			}
		}
		if edits != 4 { // the name, Contact information, People, About me
			t.Errorf("%s: %d Edit links, want 4", mode, edits)
		}
	}
}

// Someone else's profile: no Edit or Add links, no contact section (the
// fake, like Hack Club, returns no email), their custom fields.
func TestOtherProfilePane(t *testing.T) {
	h := hcEnv(t)
	h.th.Images = &pvImages{}
	v := NewProfileView(h, fakeslack.ProfileID)
	st := pvFrames(t, v, pvGPU(t), "other-profile-light")
	x := st.Texts
	for _, want := range []string{"Robin Example", "Synthetic Fixture Wrangler", "About me", "Website", "robin.example", "Favorite Channel(s)", "#sailing ⛵", "Message", "Huddle"} {
		if !has(x, want) {
			t.Errorf("%q not drawn", want)
		}
	}
	for _, bad := range []string{"Edit", "Contact information", "People", "Add pronouns", "Add name pronunciation"} {
		if has(x, bad) {
			t.Errorf("%q drawn on someone else's profile", bad)
		}
	}
	if hasPrefix(x, "Add ") {
		t.Errorf("an Add link on someone else's profile: %q", x)
	}
	if st.Notdef > 0 || st.Fallback > 0 {
		t.Errorf("%d .notdef and %d system-font glyphs", st.Notdef, st.Fallback)
	}
}

// A real name that is a lone zero-width space (Karim's) draws an empty
// name line, like Slack's getMemberRealName: never the display name.
func TestProfileNameIsRealName(t *testing.T) {
	h := hcEnv(t)
	h.th.Images = &pvImages{}
	h.a.S.Lock()
	u := h.a.S.Users[fakeslack.SelfID]
	u.Profile.RealName = "​"
	h.a.S.Unlock()
	self, _ := lookupUser(h, fakeslack.SelfID)
	if self.RealName != "" {
		t.Fatalf("core kept the zero-width real name %q", self.RealName)
	}
	v := NewProfileView(h, fakeslack.SelfID)
	st := pvFrames(t, v, nil, "")
	if has(st.Texts, self.Name) {
		t.Errorf("the display name %q drawn as the profile name", self.Name)
	}
	if !has(st.Texts, "Edit") {
		t.Error("no Edit link on the empty name line")
	}
}

// The ⋮ menu: Slack's own-profile template, and the other-person one.
func TestProfileMoreMenu(t *testing.T) {
	h := hcEnv(t)
	labels := func(id string) []string {
		u, _ := lookupUser(h, id)
		var out []string
		for _, it := range pfMoreItems(h, u, false, nil) {
			if it.Separator {
				out = append(out, "-")
			} else {
				out = append(out, it.Label)
			}
		}
		return out
	}
	self, _ := lookupUser(h, fakeslack.SelfID)
	got := strings.Join(labels(fakeslack.SelfID), "|")
	away := "Set yourself away"
	if self.Presence == "away" {
		away = "Set yourself active"
	}
	want := "Copy display name: @" + self.Name + "|-|View preferences|Account settings|-|View your files|" + away + "|-|Copy member ID|Copy link to profile"
	if got != want {
		t.Errorf("own ⋮ menu\n got %s\nwant %s", got, want)
	}
	got = strings.Join(labels(fakeslack.ProfileID), "|")
	want = "Copy display name: @robin|-|View files|-|Copy member ID|Copy link to profile"
	if got != want {
		t.Errorf("other ⋮ menu\n got %s\nwant %s", got, want)
	}
}

// The active dot follows the account theme's highlight (Slack:
// .c-presence--active{color: var(--dt_color-theme-base-inv-hgl-2)}).
func TestProfilePresenceColor(t *testing.T) {
	th := theme.New(false)
	if got := colorsOf(th).Presence; got != lightColors.Presence {
		t.Fatalf("default theme dot %v, want %v", got, lightColors.Presence)
	}
	teal := color.NRGBA{R: 0x7e, G: 0xcf, B: 0xd9, A: 0xff}
	th.C.Presence = teal
	if got := colorsOf(th).Presence; got != teal {
		t.Errorf("themed dot %v, want the theme's %v", got, teal)
	}
	if lightColors.Presence == teal {
		t.Error("the shared palette was modified")
	}
}

func TestProfileDate(t *testing.T) {
	now := time.Date(2026, 11, 8, 12, 0, 0, 0, time.UTC)
	for in, want := range map[string]string{
		"2024-03-01": "Mar 1, 2024 (2 years 8 months ago)",
		"2025-11-08": "Nov 8, 2025 (1 year ago)",
		"2026-10-01": "Oct 1, 2026 (1 month ago)",
		"2026-11-01": "Nov 1, 2026 (7 days ago)",
		"2027-01-01": "Jan 1, 2027",
	} {
		if got := pfProfileDate(in, now); got != want {
			t.Errorf("%s: %q, want %q", in, got, want)
		}
	}
}
