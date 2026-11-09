package fakeslack

import (
	"net/url"
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
	return ok("result", map[string]any{"data": map[string]any{"user": map[string]any{
		"id": id, "isSelf": self, "profileSections": sections, "__typename": "User",
	}}})
}
