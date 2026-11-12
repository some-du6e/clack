package core

import (
	"bytes"
	"context"
	"encoding/json"
	"image"
	"image/color"
	"image/png"
	"net/url"
	"strings"
	"sync"
	"testing"

	"github.com/some-du6e/clack/internal/fakeslack"
	"github.com/some-du6e/clack/internal/slack"
	"github.com/some-du6e/clack/internal/store"
)

// The form fields' validation, against Slack's NYcX validators and s8g3
// limits (boot-shared BEl1's field list, CDN build d3de3a487b1c): the
// first failing rule wins, in validateMemberFullName's order (required,
// max length, then validateMemberName: no leading @, no restricted
// characters, not a name Slack keeps, not only punctuation, no emoji).
func TestValidateProfileFieldLikeSlack(t *testing.T) {
	old := profileEmojiSubtype
	profileEmojiSubtype = func() string { return "2" }
	defer func() { profileEmojiSubtype = old }()
	long := func(n int) string { return strings.Repeat("a", n) }
	for _, c := range []struct {
		field, v string
		want     string
		max      int
		sub      string
	}{
		{"real_name", "", "error_is_required", 0, ""},
		{"real_name", "   ", "error_is_required", 0, ""},
		{"real_name", "Ada Lovelace", "", 0, ""},
		{"real_name", long(80), "", 0, ""},
		{"real_name", long(81), "error_max_length", 80, ""},
		{"real_name", "  " + long(80) + "  ", "", 0, ""}, // trimmed first
		{"real_name", "@ada", "error_no_leading_at", 0, ""},
		{"real_name", "ada!", "error_no_restricted_chars", 0, ""},
		{"real_name", "a<b", "error_no_restricted_chars", 0, ""},
		{"real_name", "O'Brien-Smith Jr.", "", 0, ""},
		{"real_name", "Everyone", "error_no_restricted_values", 0, ""},
		{"real_name", "slackbot", "error_no_restricted_values", 0, ""},
		{"real_name", "...", "error_has_alphanumeric", 0, ""},
		{"real_name", "'-'", "error_has_alphanumeric", 0, ""},
		{"real_name", "Ada 🎉", "error_no_emoji", 0, "2"},
		{"real_name", "Ada 💩", "error_no_emoji", 0, "poop"},
		{"display_name", "", "", 0, ""}, // not required
		{"display_name", long(81), "error_max_length", 80, ""},
		{"display_name", "here", "error_no_restricted_values", 0, ""},
		{"display_name", "ada 😱", "error_no_emoji", 0, "2"},
		{"title", long(249), "", 0, ""},
		{"title", long(250), "error_max_length", 249, ""},
		{"title", "Wrangler 😱 — #1!", "", 0, ""}, // titles may have anything
		{"pronouns", long(81), "error_max_length", 80, ""},
		{"name_pronunciation", long(256), "", 0, ""},
		{"name_pronunciation", long(257), "error_max_length", 256, ""},
		{"real_name", strings.Repeat("😀", 41), "error_max_length", 80, ""}, // UTF-16 length: 82
		{"tz", "", "", 0, ""},
	} {
		got := ValidateProfileField(c.field, c.v)
		if got.Type != c.want || got.MaxLength != c.max || (c.sub != "" && got.Subtype != c.sub) {
			t.Errorf("%s %q: got %+v, want %s max %d sub %q", c.field, c.v, got, c.want, c.max, c.sub)
		}
	}
	// the section editor's rules (EditProfileSectionForm's yup schema)
	if e := ValidateSectionText(long(257)); e.Type != "error_max_length" || e.MaxLength != 256 {
		t.Errorf("section text: %+v", e)
	}
	for v, ok := range map[string]bool{"": true, "https://example.com/x": true, "http://localhost:3000": true,
		"example.com": false, "https://exa mple.com": false, "javascript:alert(1)": false, "https://nodot": false, "mailto:a@b.co": true} {
		if got := ValidateSectionLinkURL(v).OK(); got != ok {
			t.Errorf("link %q valid %v, want %v", v, got, ok)
		}
	}
}

// The crop maths: getImageDisplaySize, BaseImageCrop's first square
// (defaultCropScale 1, centred) and handleUpload's mapping back to image
// pixels with normalizeSquareCrop.
func TestPhotoCropLikeSlack(t *testing.T) {
	dw, dh := PhotoDisplaySize(600, 400, 460)
	if dw != 460 || int(dh*100) != 30666 {
		t.Fatalf("display %v×%v", dw, dh)
	}
	x, y, s := DefaultPhotoCrop(dw, dh)
	if int(x*100) != 7666 || y != 0 || int(s*100) != 30666 {
		t.Fatalf("default square %v,%v %v", x, y, s)
	}
	if c := CropFromDisplay(x, y, s, 600, 400); c != (PhotoCrop{X: 100, Y: 0, Size: 400}) {
		t.Errorf("crop %+v, want 100,0 400", c)
	}
	// a small image is shown as it is and cropped 1:1
	if dw, dh := PhotoDisplaySize(200, 300, 460); dw != 200 || dh != 300 {
		t.Errorf("small %v×%v", dw, dh)
	}
	if c := CropFromDisplay(10, 50, 200, 200, 300); c != (PhotoCrop{X: 0, Y: 50, Size: 200}) {
		t.Errorf("small crop %+v", c) // x clamped to imageWidth - size
	}
	if c := NormalizeSquareCrop(-5, 900, 5000, 300, 200); c != (PhotoCrop{X: 0, Y: 0, Size: 200}) {
		t.Errorf("normalize %+v", c)
	}
}

func pwPNG(t *testing.T, w, h int) []byte {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			img.Set(x, y, color.NRGBA{R: uint8(x), G: uint8(y), B: 0x80, A: 0xff})
		}
	}
	var b bytes.Buffer
	if err := png.Encode(&b, img); err != nil {
		t.Fatal(err)
	}
	return b.Bytes()
}

func TestCheckPhoto(t *testing.T) {
	if _, err := CheckPhoto(pwPNG(t, 600, 400)); err != nil {
		t.Errorf("600×400: %v", err)
	}
	if _, err := CheckPhoto(pwPNG(t, 20, 400)); err != ErrPhotoTooSmall {
		t.Errorf("20 wide: %v", err)
	}
	if _, err := CheckPhoto(pwPNG(t, 4001, 40)); err != ErrPhotoTooBig {
		t.Errorf("4001 wide: %v", err)
	}
	if _, err := CheckPhoto([]byte("not an image")); err != ErrPhotoType {
		t.Errorf("text: %v", err)
	}
}

// pwEnv is the fake with the viewer booted into the store and every call
// recorded.
type pwCalls struct {
	mu sync.Mutex
	m  map[string][]url.Values
}

func (c *pwCalls) get(m string) []url.Values {
	c.mu.Lock()
	defer c.mu.Unlock()
	return append([]url.Values(nil), c.m[m]...)
}

func pwEnv(t *testing.T) (*App, *fakeslack.Server, *pwCalls) {
	t.Helper()
	fs, err := fakeslack.Start(fakeslack.Small)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { fs.Close() })
	calls := &pwCalls{m: map[string][]url.Values{}}
	fs.SetHook(func(method string, f url.Values) (any, bool) {
		calls.mu.Lock()
		calls.m[method] = append(calls.m[method], f)
		calls.mu.Unlock()
		return nil, false
	})
	a := New(slack.New(fs.Creds()), store.New(""))
	a.EnsureUsers(context.Background(), []string{fakeslack.SelfID})
	if u, ok := a.S.User(fakeslack.SelfID); ok {
		a.S.Self = *u
	}
	return a, fs, calls
}

// EditProfileFormPane's handleSubmit: the header section first (title and
// name pronunciation through users.profile.setSections), then the tz pref
// and users.profile.set with every default but tz and title, the changes
// but tz, and an empty fields object; the member cache takes the answer.
func TestSaveProfileFormLikeSlack(t *testing.T) {
	a, _, calls := pwEnv(t)
	ctx := context.Background()
	ps, err := a.ProfileSections(ctx, fakeslack.SelfID)
	if err != nil {
		t.Fatal(err)
	}
	h := HeaderOf(ps)
	if !h.Found || h.Title == nil || h.Pronunciation == nil || h.Record == nil {
		t.Fatalf("header %+v", h)
	}
	if h.Title.ElementID != "PeTITLE" || SectionIDOf(h.Section) != "PsHEADER" {
		t.Fatalf("ids %q %q", h.Title.ElementID, SectionIDOf(h.Section))
	}
	def := a.ProfileFormDefaults(fakeslack.SelfID)
	if def["title"] != fakeslack.SelfTitle || def["real_name"] == "" || def["tz"] != "America/New_York" {
		t.Fatalf("defaults %v", def)
	}
	// nothing changed: Save just closes, no call
	s := ProfileFormSave{UserID: fakeslack.SelfID, Defaults: def, Changes: map[string]string{"display_name": def["display_name"]},
		Header: h, Title: def["title"], TitleWas: def["title"]}
	if s.Dirty() {
		t.Fatal("unchanged form is dirty")
	}
	if r, err := a.SaveProfileForm(ctx, s); err != nil || !r.Closed || len(calls.get("users.profile.set")) != 0 {
		t.Fatalf("no-op save: %+v %v, %d calls", r, err, len(calls.get("users.profile.set")))
	}
	// a new name, title, pronunciation and time zone
	s.Changes = map[string]string{"real_name": "Ada Fixture", "tz": "Europe/London", "title": "Chief Fixture Officer"}
	s.Title, s.Pronunciation = "Chief Fixture Officer", "AY-duh"
	r, err := a.SaveProfileForm(ctx, s)
	if err != nil || !r.Closed {
		t.Fatalf("save: %+v %v", r, err)
	}
	ss := calls.get("users.profile.setSections")
	if len(ss) != 1 || ss[0].Get("user") != fakeslack.SelfID || ss[0].Get("section") != "PsHEADER" {
		t.Fatalf("setSections %v", ss)
	}
	var els []map[string]any
	if err := json.Unmarshal([]byte(ss[0].Get("elements")), &els); err != nil {
		t.Fatal(err)
	}
	got := map[string]string{}
	for _, e := range els {
		b, _ := json.Marshal(e)
		got[e["element_id"].(string)] = string(b)
	}
	if got["PeTITLE"] != `{"element_id":"PeTITLE","text":{"text":"Chief Fixture Officer"}}` ||
		got["PePRONOUNCE"] != `{"element_id":"PePRONOUNCE","text":{"text":"AY-duh"}}` ||
		got["PeRECORD"] != `{"element_id":"PeRECORD","file":null}` {
		t.Errorf("header elements %v", got)
	}
	if tz := calls.get("users.prefs.set"); len(tz) != 1 || tz[0].Get("name") != "tz" || tz[0].Get("value") != "Europe/London" {
		t.Errorf("tz %v", tz)
	}
	ups := calls.get("users.profile.set")
	if len(ups) != 1 {
		t.Fatalf("users.profile.set ×%d", len(ups))
	}
	var prof map[string]any
	_ = json.Unmarshal([]byte(ups[0].Get("profile")), &prof)
	if prof["real_name"] != "Ada Fixture" || prof["title"] != "Chief Fixture Officer" || prof["display_name"] != def["display_name"] {
		t.Errorf("profile %v", prof)
	}
	if _, ok := prof["tz"]; ok {
		t.Errorf("tz in the profile: %v", prof)
	}
	if f, ok := prof["fields"].(map[string]any); !ok || len(f) != 0 {
		t.Errorf("fields %v", prof["fields"])
	}
	if u, _ := a.User(fakeslack.SelfID); u.RealName != "Ada Fixture" || u.Title != "Chief Fixture Officer" {
		t.Errorf("cache: %q %q", u.RealName, u.Title)
	}
	ps, _ = a.ProfileSections(ctx, fakeslack.SelfID)
	if h := HeaderOf(ps); h.Pronunciation.Text != "AY-duh" || h.Pronunciation.IsMissing {
		t.Errorf("pronunciation after save %+v", h.Pronunciation)
	}
}

// invalid_name_specials keeps the form open with the server's field
// error, but only when users.profile.set's answer is the first one (no tz
// change): with a tz change the pref's answer is e[0] and the form closes,
// as Slack's does.
func TestSaveProfileFormNameSpecials(t *testing.T) {
	a, _, _ := pwEnv(t)
	ctx := context.Background()
	def := a.ProfileFormDefaults(fakeslack.SelfID)
	s := ProfileFormSave{UserID: fakeslack.SelfID, Defaults: def, Changes: map[string]string{"real_name": "Ada <script>"},
		Title: def["title"], TitleWas: def["title"]}
	r, err := a.SaveProfileForm(ctx, s)
	if err != nil || r.Closed || r.FieldErrors["real_name"].Type != "error_no_special_chars" {
		t.Fatalf("got %+v %v", r, err)
	}
	s.Changes["tz"] = "Asia/Tokyo"
	if r, err := a.SaveProfileForm(ctx, s); err != nil || !r.Closed {
		t.Fatalf("with tz: %+v %v", r, err)
	}
}

// A section's Edit: convertSectionToProfileInput, the edit, then the
// cleanup (the Contact email left out, empty values as null) through
// users.profile.setSections; getSections then serves the new value.
func TestSetProfileSectionLikeSlack(t *testing.T) {
	a, _, calls := pwEnv(t)
	ctx := context.Background()
	ps, err := a.ProfileSections(ctx, fakeslack.SelfID)
	if err != nil {
		t.Fatal(err)
	}
	c, _ := ps.Section("CONTACT")
	in := SectionInput(c)
	if len(in) != 2 || in[0].ElementID != "PeEMAIL" || in[1].ElementID != "PePHONE" || in[1].Kind != "text" {
		t.Fatalf("contact input %+v", in)
	}
	in[1].Text = "+1 555 0100"
	out := CleanSectionInput(c, in)
	if len(out) != 1 || out[0].ElementID != "PePHONE" {
		t.Fatalf("cleaned %+v", out)
	}
	if err := a.SetProfileSection(ctx, fakeslack.SelfID, SectionIDOf(c), out); err != nil {
		t.Fatal(err)
	}
	if ss := calls.get("users.profile.setSections"); len(ss) != 1 || ss[0].Get("section") != "PsCONTACT" ||
		ss[0].Get("elements") != `[{"element_id":"PePHONE","text":{"text":"+1 555 0100"}}]` {
		t.Fatalf("call %v", ss)
	}
	ps, _ = a.ProfileSections(ctx, fakeslack.SelfID)
	c, _ = ps.Section("CONTACT")
	if e := c.Elements[1]; e.Text != "+1 555 0100" || e.IsMissing {
		t.Errorf("phone after save %+v", e)
	}
	// About me: the website cleared goes as null, the date kept
	ab, _ := ps.Section("ADDITIONAL_INFO")
	in = SectionInput(ab)
	for i := range in {
		if in[i].Kind == "link" && in[i].URI != "" {
			in[i].URI, in[i].Display = "", ""
		}
	}
	b, _ := json.Marshal(CleanSectionInput(ab, in))
	if !strings.Contains(string(b), `{"element_id":"PeWEBSITE","link":null}`) || !strings.Contains(string(b), `{"date":{"date":"2024-03-01"},"element_id":"PeSTART"}`) {
		t.Errorf("about me %s", b)
	}
}

// The photo: users.preparePhoto with the image, users.setPhoto with the
// crop, the viewer refetched with the custom picture; users.deletePhoto
// puts the default back.
func TestProfilePhotoLikeSlack(t *testing.T) {
	a, _, calls := pwEnv(t)
	ctx := context.Background()
	if a.HasCustomPhoto(fakeslack.SelfID) {
		t.Fatal("custom photo before the upload")
	}
	if _, err := a.PreparePhoto(ctx, "tiny.png", pwPNG(t, 20, 20)); err != ErrPhotoTooSmall {
		t.Fatalf("tiny: %v", err)
	}
	if len(calls.get("users.preparePhoto")) != 0 {
		t.Fatal("a failed check still uploaded")
	}
	up, err := a.PreparePhoto(ctx, "me.png", pwPNG(t, 600, 400))
	if err != nil || up.ID == "" || up.Width != 600 || up.Height != 400 {
		t.Fatalf("prepare %+v %v", up, err)
	}
	crop := CropFromDisplay(76.666, 0, 306.666, 600, 400)
	if err := a.SetPhoto(ctx, up.ID, crop); err != nil {
		t.Fatal(err)
	}
	sp := calls.get("users.setPhoto")
	if len(sp) != 1 || sp[0].Get("id") != up.ID || sp[0].Get("crop_x") != "100" || sp[0].Get("crop_y") != "0" || sp[0].Get("crop_w") != "400" {
		t.Fatalf("setPhoto %v", sp)
	}
	if !a.HasCustomPhoto(fakeslack.SelfID) {
		t.Fatal("no custom photo after setPhoto")
	}
	if u, _ := a.User(fakeslack.SelfID); !strings.Contains(u.Avatar192, "custom") {
		t.Errorf("avatar %q", u.Avatar192)
	}
	if err := a.DeletePhoto(ctx); err != nil {
		t.Fatal(err)
	}
	if a.HasCustomPhoto(fakeslack.SelfID) {
		t.Error("custom photo after deletePhoto")
	}
}
