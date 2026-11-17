package fakeslack

import (
	"bytes"
	"encoding/json"
	"fmt"
	"image"
	_ "image/gif"  // photoUploadForm's DecodeConfig
	_ "image/jpeg" // photoUploadForm's DecodeConfig
	_ "image/png"  // photoUploadForm's DecodeConfig
	"io"
	"math"
	"mime"
	"mime/multipart"
	"net/url"
	"strconv"
	"strings"
	"sync"
)

// Own-profile fixture (popups.ProfileView): the viewer has an emoji title
// that wraps, an email, a missing phone, a manager, a missing mentor and
// About me custom fields (set and missing), served by
// users.profile.getSections in Slack's shape (result.data.user).
const (
	SelfTitle = "Fixture Wrangler 😱😱😱 — msgs expire in 72hrs unless they end with //"
	SelfEmail = "self@fakeclub.example"
)

// setupSelfProfile fills the viewer's profile fields (after the random
// directory is built, so no fixture shifts).
func (s *Server) setupSelfProfile() {
	if len(s.users) == 0 {
		return
	}
	p := &s.users[0].Profile
	p.Title = SelfTitle
	p.Email = SelfEmail
}

func fsElement(user, section, id, typename, typ, key, label string, missing bool, extra map[string]any) map[string]any {
	e := map[string]any{
		"id": user + ":section:" + section + ":" + id, "hint": "", "isFilterable": false, "isScimManaged": false,
		"isHidden": false, "isMissing": missing, "label": label, "order": 0, "type": typ, "elementKey": key,
		"elementId": id, "legacyFieldId": "Xf" + id[2:],
		"permissions":       map[string]any{"api": []any{}, "ui": true, "scim": false, "__typename": "ProfileElementWritePermissions"},
		"defaultVisibility": "INTERNAL", "__typename": typename,
	}
	for k, v := range extra {
		e[k] = v
	}
	return e
}

func fsSection(user, id, typ, label string, canEdit bool, els ...map[string]any) map[string]any {
	if els == nil {
		els = []map[string]any{}
	}
	return map[string]any{"id": user + ":section:" + id, "sectionId": id, "label": label, "order": 0, "type": typ,
		"canEdit": canEdit, "profileElements": els, "__typename": "ProfileSection"}
}

// profileSections answers users.profile.getSections for user.
func (s *Server) profileSections(f url.Values) any {
	id := f.Get("user")
	u, found := s.userByID(id)
	if !found {
		return map[string]any{"ok": false, "error": "user_not_found"}
	}
	self := id == SelfID
	text := func(v string) map[string]any { return map[string]any{"text": v, "options": nil} }
	var sections []map[string]any
	// Header: the title; for yourself the missing name pronunciation too
	hdr := []map[string]any{}
	if u.Profile.Title != "" || self {
		e := fsElement(id, "PsHEADER", "PeTITLE", "ProfileTextElement", "TEXT", "title", "Title", u.Profile.Title == "", text(u.Profile.Title))
		e["isFilterable"] = true
		hdr = append(hdr, e)
	}
	if self {
		hdr = append(hdr,
			fsElement(id, "PsHEADER", "PeRECORD", "ProfileFileElement", "FILE", "Name Recording", "Name Recording", true, map[string]any{"fileId": nil}),
			fsElement(id, "PsHEADER", "PePRONOUNCE", "ProfileTextElement", "TEXT", "Name Pronunciation", "Name Pronunciation", true, text("")))
	}
	sections = append(sections, fsSection(id, "PsHEADER", "HEADER", "Header", false, hdr...))
	// Contact information: Slack only returns the email to the person
	// themselves on this workspace; a missing phone only to its owner
	var contact []map[string]any
	if self {
		em := fsElement(id, "PsCONTACT", "PeEMAIL", "ProfileTextElement", "TEXT", "email", "Email Address", false, text(u.Profile.Email))
		em["permissions"].(map[string]any)["ui"] = false
		contact = append(contact, em,
			fsElement(id, "PsCONTACT", "PePHONE", "ProfileTextElement", "TEXT", "Alternate Phone", "Phone", true, text("")))
	}
	sections = append(sections, fsSection(id, "PsCONTACT", "CONTACT", "Contact information", true, contact...))
	// People: the viewer's manager is Robin (ProfileID); a mentor is unset
	var people []map[string]any
	if self {
		people = append(people,
			fsElement(id, "PsPEOPLE", "PeMANAGER", "ProfilePersonElement", "PERSON", "custom_1000000000001", "Manager", false, map[string]any{"persons": []string{ProfileID}}),
			fsElement(id, "PsPEOPLE", "PeMENTOR", "ProfilePersonElement", "PERSON", "custom_1000000000002", "Mentor", true, map[string]any{"persons": []string{}}))
	}
	sections = append(sections,
		fsSection(id, "PsPEOPLE", "PEOPLE", "People", true, people...),
		fsSection(id, "PsAFFIL", "AFFILIATIONS", "Affiliations", false))
	// About me: custom fields (team.profile.get's), set and missing
	var about []map[string]any
	switch {
	case self:
		about = append(about,
			fsElement(id, "PsABOUT", "PeSTART", "ProfileDateElement", "DATE", "Start Date", "Start Date", false, map[string]any{"date": "2024-03-01"}),
			fsElement(id, "PsABOUT", "PeWEBSITE", "ProfileLinkElement", "LINK", "key_1000000000003", "Website", false, map[string]any{"uri": "https://example.com/self", "displayText": ""}),
			fsElement(id, "PsABOUT", "PeGITHUB", "ProfileLinkElement", "LINK", "key_1000000000004", "GitHub", true, map[string]any{"uri": "", "displayText": ""}),
			fsElement(id, "PsABOUT", "PeFAVE", "ProfileTextElement", "TEXT", "key_1000000000005", "Favorite emoji", false, text("🦜 parrots, and 😱 when the build is red")))
	case id == ProfileID:
		about = append(about,
			fsElement(id, "PsABOUT", "PeWEBSITE", "ProfileLinkElement", "LINK", "key_1000000000003", "Website", false, map[string]any{"uri": "https://example.com/robin", "displayText": "robin.example"}),
			fsElement(id, "PsABOUT", "PeFAVE", "ProfileTextElement", "TEXT", "key_1000000000005", "Favorite Channel(s)", false, text("#sailing ⛵")))
	}
	sections = append(sections,
		fsSection(id, "PsABOUT", "ADDITIONAL_INFO", "About me", true, about...),
		fsSection(id, "PsSCIM", "OTHER_SCIM_FIELDS", "Other fields", false))
	for _, sec := range sections { // what users.profile.setSections wrote
		for _, e := range sec["profileElements"].([]map[string]any) {
			eid, _ := e["elementId"].(string)
			s.sectionOverride(id, eid, e)
		}
	}
	return ok("result", map[string]any{"data": map[string]any{"user": map[string]any{
		"id": id, "isSelf": self, "profileSections": sections, "__typename": "User",
	}}})
}

// ---- writes (the Edit profile modal, core/profile_write.go) ----

// profileState is what the viewer's profile writes changed: section
// element overrides (element id → the setSections element) and the
// prepared photos. Kept beside the Server so its struct stays as it is.
type profileState struct {
	mu       sync.Mutex
	elements map[string]map[string]json.RawMessage // user → element id → element
	photos   map[string]image.Point                // users.preparePhoto's ids → the prepared image's size
	nextID   int64
}

var profileStates sync.Map // *Server -> *profileState

func (s *Server) profile() *profileState {
	v, _ := profileStates.LoadOrStore(s, &profileState{elements: map[string]map[string]json.RawMessage{}})
	return v.(*profileState)
}

// handleProfileWrites answers users.profile.set (the viewer's profile,
// merged; "<" or ">" in a name answers invalid_name_specials with the
// field, like Slack), users.profile.setSections (the elements override
// what getSections serves), users.preparePhoto, users.setPhoto and
// users.deletePhoto (the viewer's pictures switch to a "custom" set and
// back; photoWrite checks them). Synthetic only.
func (s *Server) handleProfileWrites(method string, f url.Values) (any, bool) {
	if out, handled := s.photoWrite(method, f); handled {
		return out, true
	}
	switch method {
	case "users.profile.set":
		var p map[string]any
		if json.Unmarshal([]byte(f.Get("profile")), &p) != nil {
			return map[string]any{"ok": false, "error": "invalid_profile"}, true
		}
		for _, k := range []string{"real_name", "display_name"} {
			if v, _ := p[k].(string); strings.ContainsAny(v, "<>") {
				return map[string]any{"ok": false, "error": "invalid_name_specials", "field": k}, true
			}
		}
		ps := s.profile()
		ps.mu.Lock()
		u := &s.users[0]
		str := func(k string, dst *string) {
			if v, ok := p[k].(string); ok {
				*dst = v
			}
		}
		str("real_name", &u.Profile.RealName)
		str("display_name", &u.Profile.DisplayName)
		str("title", &u.Profile.Title)
		str("pronouns", &u.Profile.Pronouns)
		str("phone", &u.Profile.Phone)
		if v, ok := p["real_name"].(string); ok {
			u.RealName = v
		}
		prof := u.Profile
		ps.mu.Unlock()
		return ok("profile", prof), true
	case "users.profile.setSections":
		user := f.Get("user")
		var els []map[string]json.RawMessage
		if json.Unmarshal([]byte(f.Get("elements")), &els) != nil {
			return map[string]any{"ok": false, "error": "invalid_elements"}, true
		}
		ps := s.profile()
		ps.mu.Lock()
		m := ps.elements[user]
		if m == nil {
			m = map[string]json.RawMessage{}
			ps.elements[user] = m
		}
		for _, e := range els {
			var id string
			_ = json.Unmarshal(e["element_id"], &id)
			b, _ := json.Marshal(e)
			m[id] = b
		}
		ps.mu.Unlock()
		return ok(), true
	}
	return nil, false
}

// The photo methods as the fake's Slack answers them. What Slack's client
// sends (web client build 2c0d02ca130f: eNqz processUpload and
// EditProfileCropPane, generated fetchers through apiCall):
//
//   - users.preparePhoto: a FormData body with the selected workspace's
//     token (computeToken, no enterprise_token: TeamToken here) and the
//     picked File as the "image" part, typed as the file is (processUpload
//     lets only image/gif, image/png and image/jpeg through, 36 to 4000
//     px a side). The fake checks the token, that there's an image part
//     that decodes as one of those formats, and its size; it records the
//     part's filename and Content-Type (photoNameKey, photoTypeKey) for
//     tests to compare with what a browser sends, without refusing other
//     types, as what Slack's server does with them isn't known. It answers
//     {id, url}: id a number's digits (Slack's client parseInts the ids it
//     gets back elsewhere), url the prepared image, which is the upload
//     fitted into PreparedPhotoMax (users.setPhoto's documented limit) and
//     served by /img/ at that size.
//   - users.setPhoto {id, crop_x, crop_y, crop_w}: the same token; an id
//     preparePhoto answered (not_found otherwise); the crop, when sent,
//     whole numbers making a square inside the prepared image, which is
//     what the crop pane picks it in (getLoadedImage(e.url)'s width and
//     height). A crop outside it answers invalid_arguments: Slack's own
//     answer to one has never been seen (Clack never calls the photo
//     methods against Slack), so that's the fake's stand-in, one of the
//     method's documented errors. No crop is the whole image.
//   - users.deletePhoto: the same token.
//
// A wrong token answers like Slack does for one: the org's
// enterprise_is_restricted (the method can't be called on the org),
// anything else invalid_auth.
func (s *Server) photoWrite(method string, f url.Values) (any, bool) {
	fail := func(code string) (any, bool) { return map[string]any{"ok": false, "error": code}, true }
	switch method {
	case "users.preparePhoto", "users.setPhoto", "users.deletePhoto":
		switch f.Get("token") {
		case TeamToken:
		case OrgToken:
			return fail("enterprise_is_restricted")
		default:
			return fail("invalid_auth")
		}
	default:
		return nil, false
	}
	ps := s.profile()
	switch method {
	case "users.preparePhoto":
		if f.Get(photoBytesKey) == "" {
			return fail("no_image")
		}
		var w, h int
		if _, err := fmt.Sscanf(f.Get(photoDimsKey), "%dx%d", &w, &h); err != nil {
			return fail("bad_image")
		}
		if w > 4000 || h > 4000 {
			return fail("too_large")
		}
		pw, ph := PreparedPhotoSize(w, h)
		ps.mu.Lock()
		if ps.photos == nil {
			ps.photos = map[string]image.Point{}
		}
		ps.nextID++
		id := strconv.FormatInt(1000000000000+ps.nextID, 10)
		ps.photos[id] = image.Pt(pw, ph)
		ps.mu.Unlock()
		return ok("id", id, "url", s.URL+"img/"+strconv.Itoa(pw)+"x"+strconv.Itoa(ph)+"/prepared-"+id+".png"), true
	case "users.setPhoto":
		ps.mu.Lock()
		defer ps.mu.Unlock()
		size, found := ps.photos[f.Get("id")]
		if !found {
			return fail("not_found")
		}
		if f.Has("crop_x") || f.Has("crop_y") || f.Has("crop_w") {
			x, ex := strconv.Atoi(f.Get("crop_x"))
			y, ey := strconv.Atoi(f.Get("crop_y"))
			cw, ew := strconv.Atoi(f.Get("crop_w"))
			if ex != nil || ey != nil || ew != nil || x < 0 || y < 0 || cw < 1 || x+cw > size.X || y+cw > size.Y {
				return fail("invalid_arguments")
			}
		}
		s.setSelfPhoto(true)
		return ok("profile", s.users[0].Profile), true
	default: // users.deletePhoto
		ps.mu.Lock()
		s.setSelfPhoto(false)
		ps.mu.Unlock()
		return ok(), true
	}
}

// PreparedPhotoMax is the longest side of the fake's prepared photos:
// users.setPhoto's documented limit ("a maximum size of 1024 by 1024
// pixels").
const PreparedPhotoMax = 1024

// PreparedPhotoSize is the size the fake's users.preparePhoto answers an
// upload of w×h with: fitted into PreparedPhotoMax, never scaled up.
func PreparedPhotoSize(w, h int) (int, int) {
	if w <= PreparedPhotoMax && h <= PreparedPhotoMax {
		return w, h
	}
	if w >= h {
		return PreparedPhotoMax, max(1, int(math.Round(float64(h)*PreparedPhotoMax/float64(w))))
	}
	return max(1, int(math.Round(float64(w)*PreparedPhotoMax/float64(h)))), PreparedPhotoMax
}

// What the fake reads from a users.preparePhoto upload's image part (the
// multipart body isn't a form): its size in bytes, its decoded dimensions
// ("WxH"; absent when it doesn't decode as a GIF, PNG or JPEG), its
// filename and its Content-Type.
const (
	photoBytesKey = "_fake_image_bytes"
	photoDimsKey  = "_fake_image_dims"
	photoNameKey  = "_fake_image_filename"
	photoTypeKey  = "_fake_image_type"
)

// The fake's form keys for a users.preparePhoto image part, for tests.
const (
	PhotoFilenameKey = photoNameKey
	PhotoTypeKey     = photoTypeKey
	PhotoBytesKey    = photoBytesKey
)

// photoUploadForm reads a users.preparePhoto multipart body: its text
// fields (token first, as Slack's client sends it) and what photoWrite
// checks about the image part.
func photoUploadForm(contentType string, body []byte) url.Values {
	form := url.Values{}
	_, params, err := mime.ParseMediaType(contentType)
	if err != nil || params["boundary"] == "" {
		return form
	}
	mr := multipart.NewReader(bytes.NewReader(body), params["boundary"])
	for {
		p, err := mr.NextPart()
		if err != nil {
			return form
		}
		b, _ := io.ReadAll(p)
		if p.FormName() != "image" {
			form.Add(p.FormName(), string(b))
			continue
		}
		if len(b) == 0 {
			continue
		}
		form.Set(photoBytesKey, strconv.Itoa(len(b)))
		form.Set(photoNameKey, p.FileName())
		form.Set(photoTypeKey, p.Header.Get("Content-Type"))
		if cfg, format, err := image.DecodeConfig(bytes.NewReader(b)); err == nil && (format == "gif" || format == "png" || format == "jpeg") {
			form.Set(photoDimsKey, strconv.Itoa(cfg.Width)+"x"+strconv.Itoa(cfg.Height))
		}
	}
}

// setSelfPhoto switches the viewer's pictures to the uploaded ones (on)
// or back to the generated default.
func (s *Server) setSelfPhoto(on bool) {
	p := &s.users[0].Profile
	seed := "u0"
	if on {
		seed = "custom"
	}
	img := func(px int) string {
		return s.URL + "img/" + strconv.Itoa(px) + "x" + strconv.Itoa(px) + "/" + seed + ".png"
	}
	p.Image24, p.Image32, p.Image48, p.Image72, p.Image192, p.Image512 = img(24), img(32), img(48), img(72), img(192), img(512)
	p.IsCustomImage = on
	if on {
		p.AvatarHash, p.ImageOriginal = "c0ffee000001", img(1024)
	} else {
		p.AvatarHash, p.ImageOriginal = "default00001", ""
	}
}

// sectionOverride applies a setSections element to a getSections one.
func (s *Server) sectionOverride(user, id string, e map[string]any) {
	ps := s.profile()
	ps.mu.Lock()
	raw, ok := ps.elements[user][id]
	ps.mu.Unlock()
	if !ok {
		return
	}
	var in map[string]json.RawMessage
	if json.Unmarshal(raw, &in) != nil {
		return
	}
	set := false
	for k, v := range in {
		if k == "element_id" {
			continue
		}
		var val map[string]any
		_ = json.Unmarshal(v, &val) // null → nil: cleared
		switch k {
		case "text":
			t, _ := val["text"].(string)
			e["text"], set = t, t != ""
		case "date":
			d, _ := val["date"].(string)
			e["date"], set = d, d != ""
		case "link":
			u, _ := val["uri"].(string)
			d, _ := val["displayText"].(string)
			e["uri"], e["displayText"], set = u, d, u != ""
		case "file":
			id, _ := val["fileId"].(string)
			if id == "" {
				e["fileId"] = nil
			} else {
				e["fileId"] = id
			}
			set = id != ""
		}
	}
	e["isMissing"] = !set
}

// multipartFileSize is the byte size of a multipart body's file part
// field, as a string ("" when it has none).
func multipartFileSize(contentType string, body []byte, field string) string {
	_, params, err := mime.ParseMediaType(contentType)
	if err != nil || params["boundary"] == "" {
		return ""
	}
	mr := multipart.NewReader(bytes.NewReader(body), params["boundary"])
	for {
		p, err := mr.NextPart()
		if err != nil {
			return ""
		}
		if p.FormName() == field {
			b, _ := io.ReadAll(p)
			if len(b) == 0 {
				return ""
			}
			return strconv.Itoa(len(b))
		}
	}
}
