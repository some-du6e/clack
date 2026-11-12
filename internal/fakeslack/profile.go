package fakeslack

import (
	"bytes"
	"encoding/json"
	"io"
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
// photo. Kept beside the Server so its struct stays as it is.
type profileState struct {
	mu       sync.Mutex
	elements map[string]map[string]json.RawMessage // user → element id → element
	photo    string                                // the prepared photo's id
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
// back). Synthetic only.
func (s *Server) handleProfileWrites(method string, f url.Values) (any, bool) {
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
	case "users.preparePhoto":
		if f.Get(photoBytesKey) == "" {
			return map[string]any{"ok": false, "error": "no_image"}, true
		}
		ps := s.profile()
		ps.mu.Lock()
		ps.photo = "F0PHOTO0001"
		ps.mu.Unlock()
		return ok("id", "F0PHOTO0001", "url", s.URL+"img/460x460/photo.png"), true
	case "users.setPhoto":
		ps := s.profile()
		ps.mu.Lock()
		defer ps.mu.Unlock()
		if f.Get("id") == "" || f.Get("id") != ps.photo {
			return map[string]any{"ok": false, "error": "invalid_image"}, true
		}
		s.setSelfPhoto(true)
		return ok("profile", s.users[0].Profile), true
	case "users.deletePhoto":
		ps := s.profile()
		ps.mu.Lock()
		s.setSelfPhoto(false)
		ps.mu.Unlock()
		return ok(), true
	}
	return nil, false
}

// photoBytesKey carries a users.preparePhoto upload's image size (the
// multipart body isn't a form).
const photoBytesKey = "_fake_image_bytes"

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
