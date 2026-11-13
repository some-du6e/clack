package popups

import (
	"bytes"
	"context"
	"image"
	"image/color"
	"image/png"
	"os"
	"slices"
	"strings"
	"testing"
	"time"

	"gioui.org/layout"
	"gioui.org/op"
	"gioui.org/op/paint"
	"gioui.org/unit"

	"github.com/some-du6e/clack/internal/core"
	"github.com/some-du6e/clack/internal/fakeslack"
	"github.com/some-du6e/clack/internal/ui/theme"
	"github.com/some-du6e/clack/internal/ui/theme/pixeltest"
)

// The Edit profile modal and the section editor against Slack's
// EditProfileModal / EditProfileSectionForm (boot-shared eNqz, vIit; CDN
// build d3de3a487b1c) on internal/fakeslack, whose writes round-trip
// (fakeslack/profile.go). Synthetic only: nothing here talks to Slack.

// epEnv is pfEnv with the team's pronouns on and a display-name policy.
func epEnv(t *testing.T, dark bool) (*tzHost, *fakeslack.Server) {
	t.Helper()
	h, fs := pfEnv(t)
	fs.TeamPrefs = map[string]any{"display_pronouns": true, "username_policy": "Use the name people call you at the club."}
	if err := h.a.LoadTeamPrefs(context.Background()); err != nil {
		t.Fatal(err)
	}
	h.a.S.Lock()
	h.a.S.TeamName = "Fake Club"
	h.a.S.Unlock()
	h.th.SetDark(dark)
	return h, fs
}

// epFrame lays o out once at 1440×900 and runs posted results.
func epFrame(o Overlay) {
	ops := new(op.Ops)
	gtx := layout.Context{Ops: ops, Metric: unit.Metric{PxPerDp: 1, PxPerSp: 1}, Constraints: layout.Exact(image.Pt(1440, 900)), Now: time.Now()}
	o.Layout(gtx)
	drain()
}

func epUntil(t *testing.T, o Overlay, what string, cond func() bool) {
	t.Helper()
	for dl := time.Now().Add(10 * time.Second); !cond(); {
		if time.Now().After(dl) {
			t.Fatalf("timed out waiting for %s", what)
		}
		epFrame(o)
		time.Sleep(5 * time.Millisecond)
	}
}

func epPNG(t *testing.T, w, h int) []byte {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			img.Set(x, y, color.NRGBA{R: uint8(255 * x / w), G: uint8(180 * y / h), B: 0xc0, A: 0xff})
		}
	}
	var b bytes.Buffer
	if err := png.Encode(&b, img); err != nil {
		t.Fatal(err)
	}
	return b.Bytes()
}

// The form pane: BEl1's fields in order (Pronouns with display_pronouns,
// Name pronunciation from the header section, no Name recording), the
// title unlocked once the header says so, validation on typing with
// Slack's messages, Save disabled on an error, then users.profile.set and
// the cache updated; Cancel closes without the speed bump, Escape with
// unsaved changes shows it and Escape again cancels it.
func TestEditProfileFormLikeSlack(t *testing.T) {
	h, _ := epEnv(t, false)
	m := NewEditProfileModal(h, EditProfileOpts{Focus: "pronouns"})
	f := m.Form()
	epUntil(t, m, "the header section", func() bool { return !f.secsLoading })
	if got, want := f.Fields(), []string{"real_name", "display_name", "title", "pronouns", "name_pronunciation", "tz"}; !slices.Equal(got, want) {
		t.Fatalf("fields %v, want %v", got, want)
	}
	if f.byName["title"].disabled || f.byName["title"].ed.Text() != fakeslack.SelfTitle {
		t.Errorf("title disabled %v text %q", f.byName["title"].disabled, f.byName["title"].ed.Text())
	}
	if f.byName["display_name"].rules == "" {
		t.Error("the workspace's username_policy isn't under Display name")
	}
	if tz := f.byName["tz"]; tz.options[0].label != "Please select an option" || tz.value != "America/New_York" {
		t.Errorf("tz first %q value %q", tz.options[0].label, tz.value)
	}
	if f.dirty() {
		t.Fatal("dirty before any edit")
	}
	f.SetField("real_name", "")
	if got := f.FieldError("real_name"); got != "Unfortunately, you can’t leave this blank." || f.canSave() {
		t.Errorf("empty name: %q, can save %v", got, f.canSave())
	}
	f.SetField("real_name", "@ada")
	if got := f.FieldError("real_name"); !strings.HasPrefix(got, "Names can’t start with the @ sign") {
		t.Errorf("@ name: %q", got)
	}
	f.SetField("real_name", "Ada Fixture")
	f.SetField("display_name", "everyone")
	if got := f.FieldError("display_name"); got != "That name’s already being used by Slack." {
		t.Errorf("reserved display name: %q", got)
	}
	f.SetField("display_name", "ada")
	f.SetField("pronouns", "they/them")
	if !f.canSave() || !f.dirty() {
		t.Fatalf("can save %v dirty %v", f.canSave(), f.dirty())
	}
	// Escape: the speed bump, Escape again: back to the form
	m.Dismiss()
	if m.Dismissed() || !f.SpeedBump() {
		t.Fatalf("Escape with changes: dismissed %v bump %v", m.Dismissed(), f.SpeedBump())
	}
	epFrame(m)
	m.Dismiss()
	if m.Dismissed() || f.SpeedBump() {
		t.Fatalf("Escape on the bump: dismissed %v bump %v", m.Dismissed(), f.SpeedBump())
	}
	f.Save()
	epUntil(t, m, "the save", func() bool { return m.Dismissed() })
	u, _ := h.a.User(fakeslack.SelfID)
	if u.RealName != "Ada Fixture" || u.Pronouns != "they/them" || u.DisplayName != "ada" {
		t.Errorf("after save: %q %q %q", u.RealName, u.Pronouns, u.DisplayName)
	}

	// Cancel closes at once (handleCancel: closeModal({force: true}))
	m = NewEditProfileModal(h, EditProfileOpts{})
	epUntil(t, m, "the header section", func() bool { return !m.Form().secsLoading })
	m.Form().SetField("title", "Something else")
	m.Form().m.press(m.Form().m.o.Buttons[0])
	if !m.Dismissed() || m.Form().SpeedBump() {
		t.Errorf("Cancel: dismissed %v bump %v", m.Dismissed(), m.Form().SpeedBump())
	}
}

// A title and a pronunciation go to the header section first
// (setProfileHeaderFields), and the pane is refetched.
func TestEditProfileHeaderFields(t *testing.T) {
	h, _ := epEnv(t, false)
	refetched := 0
	m := NewEditProfileModal(h, EditProfileOpts{Refetch: func() { refetched++ }})
	f := m.Form()
	epUntil(t, m, "the header section", func() bool { return !f.secsLoading })
	f.SetField("title", "Chief Fixture Officer")
	f.SetField("name_pronunciation", "AY-duh")
	f.Save()
	epUntil(t, m, "the save", func() bool { return m.Dismissed() })
	if refetched != 1 {
		t.Errorf("refetched %d times", refetched)
	}
	ps, err := h.a.ProfileSections(context.Background(), fakeslack.SelfID)
	if err != nil {
		t.Fatal(err)
	}
	hd := core.HeaderOf(ps)
	if hd.Pronunciation.Text != "AY-duh" || hd.Title.Text != "Chief Fixture Officer" {
		t.Errorf("header after save: %q %q", hd.Pronunciation.Text, hd.Title.Text)
	}
}

// The section editor: "Edit Contact information" with the email locked
// and the phone editable; the URL rule on a link; Save writes
// users.profile.setSections and refetches; Escape with changes shows the
// speed bump.
func TestEditProfileSectionLikeSlack(t *testing.T) {
	h, _ := epEnv(t, false)
	ctx := context.Background()
	ps, err := h.a.ProfileSections(ctx, fakeslack.SelfID)
	if err != nil {
		t.Fatal(err)
	}
	contact, _ := ps.Section("CONTACT")
	refetched := 0
	s := NewEditSectionModal(h, contact, "", func() { refetched++ })
	epFrame(s)
	if s.Title() != "Edit Contact information" {
		t.Errorf("title %q", s.Title())
	}
	if got := s.Editable(); !slices.Equal(got, []string{"Email Address", "Phone"}) {
		t.Errorf("elements %v", got)
	}
	if !s.els[0].disabled || s.els[1].disabled {
		t.Errorf("email locked %v, phone locked %v", s.els[0].disabled, s.els[1].disabled)
	}
	s.Set("Phone", "+1 555 0100", "")
	m := s
	m.Dismiss()
	if s.Dismissed() || !s.SpeedBump() {
		t.Fatalf("Escape with changes: dismissed %v bump %v", s.Dismissed(), s.SpeedBump())
	}
	m.Dismiss() // Escape again cancels the bump
	s.Save()
	epUntil(t, s, "the save", func() bool { return s.Dismissed() })
	if refetched != 1 {
		t.Errorf("refetched %d", refetched)
	}
	ps, _ = h.a.ProfileSections(ctx, fakeslack.SelfID)
	contact, _ = ps.Section("CONTACT")
	if e := contact.Elements[1]; e.Text != "+1 555 0100" || e.IsMissing {
		t.Errorf("phone after save: %+v", e)
	}

	about, _ := ps.Section("ADDITIONAL_INFO")
	s = NewEditSectionModal(h, about, "", nil)
	epFrame(s)
	if got := s.Editable(); !slices.Equal(got, []string{"Start Date", "Website", "GitHub", "Favorite emoji"}) {
		t.Errorf("about me elements %v", got)
	}
	s.Set("GitHub", "me", "github.com/me")
	if got := s.Error("GitHub"); got != "This doesn’t seem like a proper link. Sorry!" || s.canSave() {
		t.Errorf("bad URL: %q, can save %v", got, s.canSave())
	}
	s.Set("GitHub", "me", "https://github.com/me")
	if !s.canSave() {
		t.Error("can't save a good URL")
	}
}

// The photo: Upload Photo's file goes through users.preparePhoto to the
// crop pane, whose first square is the largest centred one; Save sends it
// (users.setPhoto) and returns to the form, now with Remove Photo; the
// remove pane deletes it. A tiny image stops at "Try a larger image".
func TestEditProfilePhotoLikeSlack(t *testing.T) {
	h, _ := epEnv(t, false)
	h.a.SetExperimentGroup(ExpProfilePhotoComp, "") // unassigned: "Uploading your photo…" (core's defaults hold the account's on)
	m := NewEditProfileModal(h, EditProfileOpts{})
	f := m.Form()
	if f.hasPhoto() {
		t.Fatal("custom photo before the upload")
	}
	m.UploadPhoto(core.UploadFile{Name: "me.png", Data: epPNG(t, 600, 400)})
	if m.Pane() != "uploadPane" || m.upl.title() != "Uploading your photo…" {
		t.Fatalf("pane %q title %q", m.Pane(), m.upl.title())
	}
	epUntil(t, m, "the crop pane", func() bool { return m.Pane() == "cropPane" })
	if c := m.crop.Crop(); c != (core.PhotoCrop{X: 100, Y: 0, Size: 400}) {
		t.Errorf("first square %+v", c)
	}
	m.crop.Save()
	epUntil(t, m, "back on the form", func() bool { return m.Pane() == "formPane" })
	if !f.hasPhoto() {
		t.Fatal("no custom photo after Save")
	}
	m.switchPane("removePane")
	m.rm.Remove()
	epUntil(t, m, "back on the form", func() bool { return m.Pane() == "formPane" })
	if f.hasPhoto() {
		t.Error("photo still custom after Yes, Remove Photo")
	}
	if m.Dismissed() {
		t.Error("the modal closed")
	}

	m.UploadPhoto(core.UploadFile{Name: "tiny.png", Data: epPNG(t, 20, 20)})
	epUntil(t, m, "the size error", func() bool { return m.upl.Status() == "error" })
	if m.upl.title() != "Try a larger image" || !strings.Contains(m.upl.ErrorText(), "36px tall by 36px wide") {
		t.Errorf("tiny: %q %q", m.upl.title(), m.upl.ErrorText())
	}
	m.UploadPhoto(core.UploadFile{Name: "notes.txt", Data: []byte("hello")})
	epUntil(t, m, "the type error", func() bool { return m.upl.Status() == "error" })
	if m.upl.title() != "Try another photo" || m.upl.ErrorText() != "Profile photos must be GIFs, JPGs or PNGs." {
		t.Errorf("text file: %q %q", m.upl.title(), m.upl.ErrorText())
	}
}

// The admin locks (eNqz mapStateToProps): without UPDATE_NAME Full name is
// locked ("Locked by an Admin"); without UPDATE_DISPLAY_NAME Display name
// is locked, says "Display names are automatically set by your
// organization." and loses the workspace's rules; without
// EDIT_PROFILE_PHOTO the photo's label gets the lock and Upload Photo and
// Remove Photo do nothing and take no focus. The title's spinner stays up
// until permissions/info answers.
func TestEditProfileLocksLikeSlack(t *testing.T) {
	h, fs := epEnv(t, false)
	fs.Permissions = map[string][]string{"": {}}
	m := NewEditProfileModal(h, EditProfileOpts{})
	f := m.Form()
	if !f.grantsLoading {
		t.Error("no spinner while the grants load")
	}
	epUntil(t, m, "the grants", func() bool { return !f.grantsLoading && !f.secsLoading })
	if !f.Locked("real_name") || !f.Locked("display_name") || !f.PhotoLocked() {
		t.Fatalf("locked: name %v display %v photo %v", f.Locked("real_name"), f.Locked("display_name"), f.PhotoLocked())
	}
	if fd := f.byName["real_name"]; fd.disabledTip != "Locked by an Admin" {
		t.Errorf("Full name tip %q", fd.disabledTip)
	}
	if fd := f.byName["display_name"]; fd.disabledTip != "Display names are automatically set by your organization." || fd.rules != "" {
		t.Errorf("Display name tip %q rules %q", fd.disabledTip, fd.rules)
	}
	for _, tag := range f.stops() {
		if tag == &f.upload || tag == &f.remove || tag == &f.byName["real_name"].ed || tag == &f.byName["display_name"].ed {
			t.Errorf("a locked control takes the focus: %T", tag)
		}
	}
	f.upload.Click()
	epFrame(m)
	if m.Pane() != "formPane" {
		t.Errorf("Upload Photo while locked went to %q", m.Pane())
	}

	// a second open reads the cached answer: no spinner
	m2 := NewEditProfileModal(h, EditProfileOpts{})
	if m2.Form().grantsLoading || !m2.Form().PhotoLocked() {
		t.Errorf("second open: loading %v photo locked %v", m2.Form().grantsLoading, m2.Form().PhotoLocked())
	}

	// the fake's default grants: nothing locked, the rules back
	h2, _ := epEnv(t, false)
	m3 := NewEditProfileModal(h2, EditProfileOpts{})
	f3 := m3.Form()
	epUntil(t, m3, "the grants", func() bool { return !f3.grantsLoading })
	if f3.Locked("real_name") || f3.Locked("display_name") || f3.PhotoLocked() || f3.byName["display_name"].rules == "" {
		t.Errorf("default grants: name %v display %v photo %v rules %q", f3.Locked("real_name"), f3.Locked("display_name"), f3.PhotoLocked(), f3.byName["display_name"].rules)
	}
}

// profile_photo_comp: any group (eNqz tests getGroupForUser's string)
// titles the crop pane and the upload's spinner "Add a profile photo";
// unassigned, "Crop your photo" and "Uploading your photo…".
func TestEditProfilePhotoCompTitles(t *testing.T) {
	h, _ := epEnv(t, false)
	for _, c := range []struct{ group, crop, upload string }{
		{"", "Crop your photo", "Uploading your photo…"},
		{"on", "Add a profile photo", "Add a profile photo"},
		{"off", "Add a profile photo", "Add a profile photo"},
	} {
		h.a.SetExperimentGroup(ExpProfilePhotoComp, c.group)
		m := NewEditProfileModal(h, EditProfileOpts{})
		m.UploadPhoto(core.UploadFile{Name: "me.png", Data: epPNG(t, 600, 400)})
		if got := m.upl.title(); got != c.upload {
			t.Errorf("group %q: upload title %q, want %q", c.group, got, c.upload)
		}
		epUntil(t, m, "the crop pane", func() bool { return m.Pane() == "cropPane" })
		if got := m.crop.m.o.TitleFunc(); got != c.crop {
			t.Errorf("group %q: crop title %q, want %q", c.group, got, c.crop)
		}
	}
}

// Screenshots, light and dark (PROFILE_SHOTS_DIR, own-profile.yml): the
// form with an error, the speed bump, the section editor, the crop pane,
// the remove pane and an upload error; no tofu in any of them.
func TestEditProfileShots(t *testing.T) {
	dir := os.Getenv("PROFILE_SHOTS_DIR")
	for _, dark := range []bool{false, true} {
		mode := map[bool]string{false: "light", true: "dark"}[dark]
		h, _ := epEnv(t, dark)
		m := NewEditProfileModal(h, EditProfileOpts{})
		f := m.Form()
		epUntil(t, m, "the header section", func() bool { return !f.secsLoading })
		f.SetField("display_name", "@ada")
		epShot(t, h, dir, "edit-profile-form-"+mode, m, func() *Modal { return m.m })
		f.bump = true
		epShot(t, h, dir, "edit-profile-speedbump-"+mode, m, func() *Modal { return m.m })
		f.bump = false

		m.UploadPhoto(core.UploadFile{Name: "me.png", Data: epPNG(t, 600, 400)})
		epUntil(t, m, "the crop pane", func() bool { return m.Pane() == "cropPane" })
		epShot(t, h, dir, "edit-profile-crop-"+mode, m, func() *Modal { return m.m })
		m.switchPane("removePane")
		epShot(t, h, dir, "edit-profile-remove-"+mode, m, func() *Modal { return m.m })
		m.UploadPhoto(core.UploadFile{Name: "tiny.png", Data: epPNG(t, 20, 20)})
		epUntil(t, m, "the size error", func() bool { return m.upl.Status() == "error" })
		epShot(t, h, dir, "edit-profile-upload-error-"+mode, m, func() *Modal { return m.m })

		// profile_photo_comp on (the account's group): "Add a profile photo"
		h.a.SetExperimentGroup(ExpProfilePhotoComp, "on")
		m.UploadPhoto(core.UploadFile{Name: "me.png", Data: epPNG(t, 600, 400)})
		epUntil(t, m, "the crop pane", func() bool { return m.Pane() == "cropPane" })
		epShot(t, h, dir, "edit-profile-crop-photo-comp-"+mode, m, func() *Modal { return m.m })
		h.a.ClearExperimentGroup(ExpProfilePhotoComp)

		// the admin locks: Full name, Display name and the photo
		hl, fsl := epEnv(t, dark)
		fsl.Permissions = map[string][]string{"": {}}
		ml := NewEditProfileModal(hl, EditProfileOpts{})
		epUntil(t, ml, "the grants", func() bool { return !ml.Form().grantsLoading && !ml.Form().secsLoading })
		epShot(t, hl, dir, "edit-profile-locked-"+mode, ml, func() *Modal { return ml.m })

		ps, err := h.a.ProfileSections(context.Background(), fakeslack.SelfID)
		if err != nil {
			t.Fatal(err)
		}
		about, _ := ps.Section("ADDITIONAL_INFO")
		s := NewEditSectionModal(h, about, "", nil)
		s.Set("GitHub", "me", "github.com/me")
		epShot(t, h, dir, "edit-profile-section-"+mode, s, func() *Modal { return s.m })
	}
}

const epShotScale = 2

// epShot lays o out at 1100×1000 dp (scale 2) until it settles, checks
// the frame for tofu and saves the card (with 8dp around it) as name.png.
func epShot(t *testing.T, h *tzHost, dir, name string, o Overlay, m func() *Modal) {
	t.Helper()
	win := image.Pt(1100*epShotScale, 1000*epShotScale)
	var ops op.Ops
	frame := func() {
		ops.Reset()
		bg := colorsOf(h.th).Bg
		if h.th.Dark {
			bg = rgb(0x1a1d21)
		}
		paint.Fill(&ops, bg)
		gtx := layout.Context{Ops: &ops, Now: time.Now(), Metric: unit.Metric{PxPerDp: epShotScale, PxPerSp: epShotScale}, Constraints: layout.Exact(win)}
		o.Layout(gtx)
	}
	for i := 0; i < 6; i++ {
		frame()
		Pump()
		time.Sleep(20 * time.Millisecond)
	}
	stop := theme.StartTofuScan()
	frame()
	if st := stop(); st.Notdef > 0 {
		t.Errorf("%s: %d .notdef glyphs", name, st.Notdef)
	}
	img := pixeltest.Render(t, win.X, win.Y, &ops)
	epSaveShot(t, dir, name, stCrop(img, m().rect.Inset(-8*epShotScale)))
}
