package core

// Editing your own profile, as Slack's web client writes it (CDN build
// d3de3a487b1c, 2026-11-12):
//
//   - The Edit profile modal's form (boot-shared eNqz EditProfileFormPane)
//     starts from getDefaultFieldValues (display_name, phone, real_name,
//     pronouns, skype, title, tz; the optional ones only when set). Save
//     with no change just closes. Otherwise, in order: when the name
//     pronunciation or the title changed and the member has a HEADER
//     section, setProfileHeaderFields (9N9Y) rewrites that section with
//     users.profile.setSections (GACL usersProfileSetSectionsFetcher,
//     camelCaseInput false: {user, section, elements, reason
//     "edit-profile-form-pane"}); then, at once, the tz pref
//     (setUserPrefByApi, users.prefs.set) when the time zone changed and
//     updateCurrentMemberProfile (NJNP → submitProfileChange cgvR →
//     users.profile.set {profile: JSON, reason "EditProfileFormPane"})
//     with every default value but tz and title, the changes but tz, and
//     an empty fields object. The member cache takes the profile the call
//     answers with. When the first answer of the two is an
//     invalid_name_specials error naming a field, the form shows
//     error_no_special_chars on it and stays open; otherwise it closes
//     (a failed users.profile.set is swallowed: `.catch(e => e)`).
//   - A section's Edit (vIit EditProfileSectionForm) submits the section's
//     elements as convertSectionToProfileInput (AT18) makes them, edited,
//     with empty values sent as null and the Contact section's email left
//     out, through users.profile.setSections (useSetUserProfileSection).
//   - The photo (eNqz processUpload, EditProfileCropPane,
//     EditProfileRemovePane): users.preparePhoto {image} (oYHX) answers an
//     id and a url once the file passes the checks (GIF, PNG or JPEG, 36
//     to 4000 px a side); users.setPhoto {id, crop_x, crop_y, crop_w} (o+6/
//     setUserPhotoApi, reason "edit-profile-set-photo") sets it from the
//     square the crop pane picked; users.deletePhoto (L35P, reason
//     "edit_profile_delete_photo") removes it.
//
// Field rules are NYcX's validators with s8g3's limits (_w), their
// messages h+Rv's (ui_validation).

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"image"
	_ "image/gif"  // DecodeConfig for profile photos
	_ "image/jpeg" // DecodeConfig for profile photos
	_ "image/png"  // DecodeConfig for profile photos
	"math"
	"math/rand/v2"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"
	"unicode/utf16"

	"github.com/some-du6e/clack/internal/emoji"
	"github.com/some-du6e/clack/internal/slack"
)

// Profile field limits (s8g3 _w), in UTF-16 code units as JavaScript
// counts a string's length.
const (
	ProfileMaxDisplayName   = 80
	ProfileMaxFullName      = 80
	ProfileMaxPronouns      = 80
	ProfileMaxPhone         = 98
	ProfileMaxTitle         = 249
	ProfileMaxPronunciation = 256
	ProfileMaxCustom        = 256
)

// Profile photo limits (eNqz processUpload, EditProfileCropPane).
const (
	PhotoMinSide     = 36   // tp invalid_min_size below this
	PhotoMaxSide     = 4000 // th invalid_max_size above this
	PhotoCropMaxSize = 460  // the cropper's box
	PhotoCropMinSize = 72   // the smallest crop square
)

// ProfileFieldError is one of NYcX's validation results ("" Type: valid).
type ProfileFieldError struct {
	// Type is error_is_required, error_max_length, error_no_leading_at,
	// error_no_restricted_chars, error_no_restricted_values,
	// error_has_alphanumeric, error_no_emoji, error_is_url, or
	// error_no_special_chars (the server's invalid_name_specials).
	Type      string
	MaxLength int
	// Subtype picks error_no_emoji's message: "poop" when 💩 is in the
	// name, else "1".."4" (addSubtypeToEmojiError: random 1–4).
	Subtype string
}

// OK reports a valid value.
func (e ProfileFieldError) OK() bool { return e.Type == "" }

// profileEmojiSubtype is BEl1's random emoji message (r.A(1, 4)); tests
// pin it.
var profileEmojiSubtype = func() string { return strconv.Itoa(1 + rand.IntN(4)) }

// slackRestrictedNames is s8g3 xt, the names Slack keeps for itself.
var slackRestrictedNames = []string{"all", "archive", "archived", "archives", "channel", "channels", "create", "delete", "deleted-channel", "edit", "everyone", "general", "group", "groups", "here", "me", "ms", "slack", "slackbot", "siackbot", "teambot", "today", "you", "canal", "aquí", "todos", "mí", "grupo", "hier", "alle", "mir", "gruppe", "chaîne", "chaine", "ici", "tous", "moi", "groupe", "チャンネル", "ここ", "全員", "自分", "グループ", "canais", "geral", "eu", "나", "그룹", "채널", "모두", "여기", "전체", "모든", "전부", "tutti", "canale", "gruppo", "qui", "me", "tu", "归档", "已归档", "全部", "频道", "创建", "删除", "已删除的频道", "编辑", "每个人", "组", "此处", "我", "今天", "你", "全部", "頻道", "頻道", "任意", "我", "您", "一般", "Slack 機器人", "архив", "архивированный", "архивный", "все", "вы", "группа", "группы", "здесь", "канал", "каналы", "общий", "редактировать", "сегодня", "создать", "удаленный канал", "удалить", "я"}

// nameRestrictedChars is NYcX p: the punctuation a name can't have.
const nameRestrictedChars = "`~!@#$%^&*+=\\:\"<>?|"

// jsTrim is lodash trim: JavaScript's whitespace off both ends.
func jsTrim(s string) string { return strings.TrimSpace(s) }

func jsLen(s string) int { return len(utf16.Encode([]rune(s))) }

func vRequired(v string) ProfileFieldError {
	if jsTrim(v) == "" {
		return ProfileFieldError{Type: "error_is_required"}
	}
	return ProfileFieldError{}
}

func vMaxLength(v string, max int) ProfileFieldError {
	if v != "" && jsLen(jsTrim(v)) > max {
		return ProfileFieldError{Type: "error_max_length", MaxLength: max}
	}
	return ProfileFieldError{}
}

// vName is validateMemberName: no leading @, none of the restricted
// characters, not one of Slack's names, not only punctuation, no emoji.
func vName(v string) ProfileFieldError {
	if v == "" {
		return ProfileFieldError{}
	}
	if strings.HasPrefix(v, "@") {
		return ProfileFieldError{Type: "error_no_leading_at"}
	}
	if strings.ContainsAny(jsTrim(v), nameRestrictedChars) {
		return ProfileFieldError{Type: "error_no_restricted_chars"}
	}
	low := strings.ToLower(jsTrim(v))
	for _, r := range slackRestrictedNames {
		if strings.ToLower(r) == low {
			return ProfileFieldError{Type: "error_no_restricted_values"}
		}
	}
	if onlyNamePunct(v) {
		return ProfileFieldError{Type: "error_has_alphanumeric"}
	}
	if found, poop := nativeEmojiIn(v); found {
		e := ProfileFieldError{Type: "error_no_emoji"}
		if poop {
			e.Subtype = "poop"
		} else {
			e.Subtype = profileEmojiSubtype()
		}
		return e
	}
	return ProfileFieldError{}
}

// onlyNamePunct is NYcX f: /^[-_.'‘’",(){}[\]/;]+$/.
func onlyNamePunct(v string) bool {
	if v == "" {
		return false
	}
	for _, r := range v {
		if !strings.ContainsRune("-_.'‘’\",(){}[]/;", r) {
			return false
		}
	}
	return true
}

// nativeEmojiIn is validateNoEmoji's match against every native emoji
// (CY0F); poop reports 💩 among them.
func nativeEmojiIn(v string) (found, poop bool) {
	if !emoji.MayHaveEmoji(v) && !strings.ContainsAny(v, "#*0123456789") {
		return false, false
	}
	rs := []rune(v)
	for i := 0; i < len(rs); {
		name, end, ok := emoji.MatchUnicode(rs, i)
		if !ok {
			i++
			continue
		}
		found = true
		if name == "poop" || name == "hankey" || strings.HasPrefix(name, "poop::") {
			poop = true
		}
		i = end
	}
	return found, poop
}

func firstFieldError(es ...ProfileFieldError) ProfileFieldError {
	for _, e := range es {
		if !e.OK() {
			return e
		}
	}
	return ProfileFieldError{}
}

// ValidateProfileField is the form field's validation (BEl1's field list
// with NYcX's validators): real_name (validateMemberFullName),
// display_name, title, pronouns and name_pronunciation. Other names are
// always valid (the time zone).
func ValidateProfileField(name, v string) ProfileFieldError {
	switch name {
	case "real_name":
		return firstFieldError(vRequired(v), vMaxLength(v, ProfileMaxFullName), vName(v))
	case "display_name":
		return firstFieldError(vMaxLength(v, ProfileMaxDisplayName), vName(v))
	case "title":
		return vMaxLength(v, ProfileMaxTitle)
	case "pronouns":
		return vMaxLength(v, ProfileMaxPronouns)
	case "name_pronunciation":
		return vMaxLength(v, ProfileMaxPronunciation)
	}
	return ProfileFieldError{}
}

// ValidateSectionText is EditProfileSectionForm's text rule (o): at most
// the custom limit.
func ValidateSectionText(v string) ProfileFieldError { return vMaxLength(v, ProfileMaxCustom) }

// ValidateSectionLinkText is the link's display text rule (l.displayText).
func ValidateSectionLinkText(v string) ProfileFieldError {
	if jsLen(v) > ProfileMaxCustom {
		return ProfileFieldError{Type: "error_max_length", MaxLength: ProfileMaxCustom}
	}
	return ProfileFieldError{}
}

// ValidateSectionLinkURL is the link's uri rule (link-url-test, 9mZZ
// simpleIsUrl): empty, or an absolute URL with a host that has a TLD
// (or is localhost), no spaces, not javascript: or about:.
func ValidateSectionLinkURL(v string) ProfileFieldError {
	if v == "" || simpleIsURL(v) {
		return ProfileFieldError{}
	}
	return ProfileFieldError{Type: "error_is_url"}
}

func simpleIsURL(v string) bool {
	if strings.Contains(v, " ") {
		return false
	}
	u, err := url.Parse(v)
	if err != nil || u.Scheme == "" {
		return false
	}
	s := strings.ToLower(u.Scheme)
	if s == "javascript" || s == "about" {
		return false
	}
	if s != "http" && s != "https" { // iJxR _J: the schemes that need a host
		return true
	}
	h := u.Hostname()
	return h == "localhost" || hostTLD.MatchString(h)
}

// hostTLD is iJxR Fn, /[a-z0-9]+\.[a-z0-9]+/i.
var hostTLD = regexp.MustCompile(`(?i)[a-z0-9]+\.[a-z0-9]+`)

// ---- the form ----

// ProfileFormDefaults is getDefaultFieldValues for userID: display_name,
// real_name and tz always, phone, pronouns, skype and title when set.
func (a *App) ProfileFormDefaults(userID string) map[string]string {
	u, ok := a.S.User(userID)
	if !ok {
		return map[string]string{"display_name": "", "real_name": "", "tz": ""}
	}
	p := u.Profile
	out := map[string]string{
		"display_name": p.DisplayName,
		"real_name":    firstNonEmpty(p.RealName, u.RealName),
		"tz":           u.TZ,
	}
	for k, v := range map[string]string{"phone": p.Phone, "pronouns": p.Pronouns, "title": p.Title} { // no skype in Clack's profile

		if v != "" {
			out[k] = v
		}
	}
	return out
}

// HasCustomPhoto is xJ: the member's photo is one they uploaded
// (profile.is_custom_image), so Remove Photo shows.
func (a *App) HasCustomPhoto(userID string) bool {
	u, ok := a.S.User(userID)
	return ok && u.Profile.IsCustomImage
}

// ProfileHeader is getHeaderFieldValues on the HEADER section: its title,
// name pronunciation and name recording elements.
type ProfileHeader struct {
	Section                      ProfileSection
	Found                        bool
	Title, Pronunciation, Record *ProfileElement
}

// HeaderOf finds ps's HEADER section and its elements (9N9Y).
func HeaderOf(ps ProfileSections) ProfileHeader {
	s, ok := ps.Section("HEADER")
	if !ok {
		return ProfileHeader{}
	}
	h := ProfileHeader{Section: s, Found: true}
	for i := range s.Elements {
		e := &s.Elements[i]
		switch {
		case e.ElementKey == "Name Pronunciation" && e.Typename == "ProfileTextElement":
			h.Pronunciation = e
		case e.ElementKey == "Name Recording" && e.Typename == "ProfileFileElement":
			h.Record = e
		case e.ElementKey == "title" && e.Typename == "ProfileTextElement":
			h.Title = e
		}
	}
	return h
}

// ProfileFormSave is one Save Changes of the form.
type ProfileFormSave struct {
	UserID string
	// Defaults is ProfileFormDefaults as the form opened; Changes the
	// fields the user typed in (key → value, "" for cleared), tz among
	// them when it was picked.
	Defaults, Changes map[string]string
	// Header, when found, gets the name pronunciation and title written
	// (setProfileHeaderFields) when either changed.
	Header                     ProfileHeader
	Pronunciation, Title       string
	PronunciationWas, TitleWas string
}

// Dirty is the form's e4: a field differs from its default (both empty
// counts as the same), or the pronunciation or title changed.
func (s ProfileFormSave) Dirty() bool {
	for k, v := range s.Changes {
		if d := s.Defaults[k]; !(d == "" && v == "") && d != v {
			return true
		}
	}
	return s.Pronunciation != s.PronunciationWas || s.Title != s.TitleWas
}

// ProfileFormResult is what Save did: Closed when the form closes, else
// FieldErrors holds the server's field error (invalid_name_specials).
type ProfileFormResult struct {
	Closed      bool
	FieldErrors map[string]ProfileFieldError
}

// SaveProfileForm runs EditProfileFormPane's handleSubmit (see the top of
// the file).
func (a *App) SaveProfileForm(ctx context.Context, s ProfileFormSave) (ProfileFormResult, error) {
	if !s.Dirty() {
		return ProfileFormResult{Closed: true}, nil
	}
	if s.Header.Found && (s.Pronunciation != s.PronunciationWas || s.Title != s.TitleWas) {
		if err := a.SetProfileHeaderFields(ctx, s.UserID, s.Header, s.Pronunciation, s.Title); err != nil {
			return ProfileFormResult{}, err
		}
	}
	type res struct{ err error }
	var calls []chan res
	if tz := s.Changes["tz"]; tz != "" {
		ch := make(chan res, 1)
		go func() { ch <- res{a.SetOwnTimezone(ctx, tz)} }()
		calls = append(calls, ch)
	}
	prof := map[string]any{}
	for k, v := range s.Defaults {
		if k != "tz" && k != "title" {
			prof[k] = v
		}
	}
	for k, v := range s.Changes {
		if k != "tz" {
			prof[k] = v
		}
	}
	prof["fields"] = map[string]any{}
	pch := make(chan res, 1)
	go func() {
		_, err := a.SetOwnProfile(ctx, prof)
		pch <- res{err}
	}()
	calls = append(calls, pch)
	var first error
	for i, ch := range calls {
		r := <-ch
		if i == 0 {
			first = r.err
		}
	}
	var se *slack.Error
	if errors.As(first, &se) && se.Code == "invalid_name_specials" {
		var b struct {
			Field string `json:"field"`
		}
		_ = json.Unmarshal(se.Body, &b)
		if b.Field != "" {
			return ProfileFormResult{FieldErrors: map[string]ProfileFieldError{b.Field: {Type: "error_no_special_chars"}}}, nil
		}
	}
	return ProfileFormResult{Closed: true}, nil
}

// SetOwnProfile is updateCurrentMemberProfile: users.profile.set with
// profile as JSON, then the member cache takes the profile Slack answers
// with (and a "user" event fires).
func (a *App) SetOwnProfile(ctx context.Context, profile map[string]any) (slack.Profile, error) {
	b, err := json.Marshal(profile)
	if err != nil {
		return slack.Profile{}, err
	}
	p := url.Values{"profile": {string(b)}} // Clack's calls carry no _x_reason
	var r struct {
		Profile slack.Profile `json:"profile"`
	}
	if err := a.C.Call(ctx, "users.profile.set", p, &r); err != nil {
		return slack.Profile{}, err
	}
	a.mergeSelfProfile(r.Profile)
	return r.Profile, nil
}

// mergeSelfProfile is NJNP's {...currentMember, profile}: the viewer's
// cached member with the new profile.
func (a *App) mergeSelfProfile(p slack.Profile) {
	id := a.Self().ID
	u, ok := a.S.User(id)
	if !ok || id == "" {
		return
	}
	nu := *u
	nu.Profile = p
	if p.RealName != "" {
		nu.RealName = p.RealName
	}
	a.S.PutUsers([]slack.User{nu})
	if a.S.Self.ID == id {
		a.S.Lock()
		a.S.Self = nu
		a.S.Unlock()
	}
	if cu, ok := a.User(id); ok {
		a.emit(Event{Type: "user", UserID: id, User: &cu})
	}
}

// SetOwnTimezone writes the tz pref (setUserPrefByApi) and moves the
// viewer's cached member to it, as the user_change that follows would.
func (a *App) SetOwnTimezone(ctx context.Context, tz string) error {
	if err := a.SetPref(ctx, "tz", tz); err != nil {
		return err
	}
	a.setSelfTZ(tz)
	return nil
}

func (a *App) setSelfTZ(tz string) {
	id := a.Self().ID
	u, ok := a.S.User(id)
	if !ok || tz == "" {
		return
	}
	nu := *u
	nu.TZ = tz
	if loc, err := time.LoadLocation(tz); err == nil {
		_, off := time.Now().In(loc).Zone()
		nu.TZOffset = off
	}
	a.S.PutUsers([]slack.User{nu})
	if cu, ok := a.User(id); ok {
		a.emit(Event{Type: "user", UserID: id, User: &cu})
	}
}

// ---- sections ----

// SectionElementInput is one element of users.profile.setSections'
// elements (AT18 convertSectionToProfileInput); exactly one of the value
// fields is set, or Null for a cleared one (sent as `"text": null`).
type SectionElementInput struct {
	ElementID string
	Kind      string // text, date, link, person, long_text, tags, file, currentLocation
	Text      string
	Date      string
	URI       string
	Display   string
	Persons   []string
	Tags      []string
	FileID    string
	Location  map[string]string // city, country, description, state
	Null      bool
}

// MarshalJSON writes Slack's element shape: {element_id, <kind>: …}.
func (e SectionElementInput) MarshalJSON() ([]byte, error) {
	m := map[string]any{"element_id": e.ElementID}
	if e.Null {
		m[e.Kind] = nil
		return json.Marshal(m)
	}
	switch e.Kind {
	case "text":
		m["text"] = map[string]any{"text": e.Text}
	case "date":
		m["date"] = map[string]any{"date": e.Date}
	case "link":
		m["link"] = map[string]any{"displayText": e.Display, "uri": e.URI}
	case "person":
		m["person"] = map[string]any{"persons": nonNil(e.Persons)}
	case "long_text":
		m["long_text"] = map[string]any{"blocks": e.Text}
	case "tags":
		m["tags"] = map[string]any{"tags": nonNil(e.Tags)}
	case "file":
		if e.FileID == "" {
			m["file"] = nil
		} else {
			m["file"] = map[string]any{"fileId": e.FileID}
		}
	case "currentLocation":
		l := map[string]string{"city": "", "country": "", "description": "", "state": ""}
		for k, v := range e.Location {
			l[k] = v
		}
		m["currentLocation"] = l
	}
	return json.Marshal(m)
}

func nonNil(s []string) []string {
	if s == nil {
		return []string{}
	}
	return s
}

// SectionInput is convertSectionToProfileInput: one input per element
// with an element id, in order (SCIM-managed people left out).
func SectionInput(s ProfileSection) []SectionElementInput {
	var out []SectionElementInput
	for _, e := range s.Elements {
		id := e.ElementID
		if id == "" {
			continue
		}
		in := SectionElementInput{ElementID: id}
		switch e.Typename {
		case "ProfileCurrentLocationElement":
			in.Kind, in.Location = "currentLocation", map[string]string{"city": e.City, "country": e.Country, "description": e.Description, "state": e.State}
		case "ProfileTagsElement":
			in.Kind, in.Tags = "tags", append([]string(nil), e.Tags...)
		case "ProfileTextElement":
			in.Kind, in.Text = "text", e.Text
		case "ProfileDateElement":
			in.Kind, in.Date = "date", e.Date
		case "ProfileLinkElement":
			in.Kind, in.URI, in.Display = "link", e.URI, e.DisplayText
		case "ProfilePersonElement":
			if e.IsScimManaged {
				continue
			}
			in.Kind, in.Persons = "person", append([]string(nil), e.Persons...)
		case "ProfileLongTextElement":
			in.Kind, in.Text = "long_text", e.Text
		case "ProfileFileElement":
			in.Kind, in.FileID = "file", e.FileID
			in.Null = e.FileID == ""
		default:
			continue
		}
		out = append(out, in)
	}
	return out
}

// CleanSectionInput is EditProfileSectionForm's onSubmit pass: the
// Contact section's email is left out, and an empty value is sent as null
// (a location with every part empty, an empty text, date, link uri,
// person list or long text).
func CleanSectionInput(s ProfileSection, els []SectionElementInput) []SectionElementInput {
	byID := map[string]ProfileElement{}
	for _, e := range s.Elements {
		byID[e.ElementID] = e
	}
	var out []SectionElementInput
	for _, e := range els {
		if s.Type == "CONTACT" {
			m, ok := byID[e.ElementID]
			if !ok || m.ElementKey == "email" {
				continue
			}
		}
		if !e.Null {
			switch e.Kind {
			case "currentLocation":
				empty := true
				for _, v := range e.Location {
					if v != "" {
						empty = false
					}
				}
				e.Null = empty
			case "text", "long_text":
				e.Null = e.Text == ""
			case "date":
				e.Null = e.Date == ""
			case "link":
				e.Null = e.URI == ""
			case "person":
				empty := true
				for _, p := range e.Persons {
					if p != "" {
						empty = false
					}
				}
				e.Null = empty
			}
		}
		out = append(out, e)
	}
	return out
}

// SectionIDOf is the section id EditProfileSectionForm sends:
// id.split(":")[2] ("U…:section:PsCONTACT" → "PsCONTACT").
func SectionIDOf(s ProfileSection) string {
	if s.SectionID != "" {
		return s.SectionID
	}
	parts := strings.Split(s.ID, ":")
	if len(parts) > 2 {
		return parts[2]
	}
	return ""
}

// SetProfileSection is useSetUserProfileSection: users.profile.setSections
// {user, section, elements (JSON), reason}.
func (a *App) SetProfileSection(ctx context.Context, user, section string, els []SectionElementInput) error {
	if els == nil {
		els = []SectionElementInput{}
	}
	b, err := json.Marshal(els)
	if err != nil {
		return err
	}
	p := url.Values{"user": {user}, "section": {section}, "elements": {string(b)}}
	return a.C.Call(ctx, "users.profile.setSections", p, nil)
}

// SetProfileHeaderFields is 9N9Y's setProfileHeaderFields with the
// recording left as it is (Clack doesn't record): the header section's
// elements with the pronunciation's and the title's text replaced.
func (a *App) SetProfileHeaderFields(ctx context.Context, user string, h ProfileHeader, pronunciation, title string) error {
	els := SectionInput(h.Section)
	for i := range els {
		switch {
		case h.Pronunciation != nil && els[i].ElementID == h.Pronunciation.ElementID:
			els[i].Kind, els[i].Text, els[i].Null = "text", pronunciation, false
		case h.Title != nil && els[i].ElementID == h.Title.ElementID:
			els[i].Kind, els[i].Text, els[i].Null = "text", title, false
		}
	}
	return a.SetProfileSection(ctx, user, SectionIDOf(h.Section), els)
}

// ---- the photo ----

// Photo upload errors (useImageUpload's error states).
var (
	ErrPhotoType     = errors.New("invalid_type")
	ErrPhotoTooSmall = errors.New("invalid_min_size")
	ErrPhotoTooBig   = errors.New("invalid_max_size")
	ErrPhotoAPI      = errors.New("api_error")
)

// PhotoUpload is a photo users.preparePhoto took: its id for
// users.setPhoto and the image's size.
type PhotoUpload struct {
	ID, URL       string
	Width, Height int
}

// CheckPhoto is processUpload's checks before the upload: a GIF, PNG or
// JPEG, at least 36 and at most 4000 px a side.
func CheckPhoto(data []byte) (image.Config, error) {
	cfg, format, err := image.DecodeConfig(bytes.NewReader(data))
	if err != nil || (format != "gif" && format != "png" && format != "jpeg") {
		return image.Config{}, ErrPhotoType
	}
	if cfg.Height < PhotoMinSide || cfg.Width < PhotoMinSide {
		return cfg, ErrPhotoTooSmall
	}
	if cfg.Height > PhotoMaxSide || cfg.Width > PhotoMaxSide {
		return cfg, ErrPhotoTooBig
	}
	return cfg, nil
}

// PreparePhoto is processUpload: the checks, then users.preparePhoto
// {image} (reason "EditProfileModal"); an answer without ok, url and id
// is ErrPhotoAPI.
func (a *App) PreparePhoto(ctx context.Context, name string, data []byte) (PhotoUpload, error) {
	cfg, err := CheckPhoto(data)
	if err != nil {
		return PhotoUpload{}, err
	}
	var r struct {
		ID  string `json:"id"`
		URL string `json:"url"`
	}
	if err := a.C.CallMultipart(ctx, "users.preparePhoto", url.Values{}, "image", name, data, &r); err != nil {
		if ctx.Err() != nil {
			return PhotoUpload{}, ctx.Err()
		}
		return PhotoUpload{}, ErrPhotoAPI
	}
	if r.ID == "" || r.URL == "" {
		return PhotoUpload{}, ErrPhotoAPI
	}
	return PhotoUpload{ID: r.ID, URL: r.URL, Width: cfg.Width, Height: cfg.Height}, nil
}

// PhotoDisplaySize is getImageDisplaySize (1PWH): w×h scaled to fit max
// on its longer side (never up).
func PhotoDisplaySize(w, h, max float64) (dw, dh float64) {
	dw, dh = w, h
	if h > max || w > max {
		if h > w {
			dw, dh = max/h*w, max
		} else {
			dh, dw = max/w*h, max
		}
	}
	return dw, dh
}

// PhotoCrop is a square in image pixels.
type PhotoCrop struct{ X, Y, Size int }

// NormalizeSquareCrop is normalizeSquareCrop: x, y and size rounded and
// kept inside the image.
func NormalizeSquareCrop(x, y, size float64, imgW, imgH int) PhotoCrop {
	r, s := max(0, imgW), max(0, imgH)
	o := min(max(0, int(math.Round(size))), r, s)
	return PhotoCrop{
		X:    min(max(0, int(math.Round(x))), r-o),
		Y:    min(max(0, int(math.Round(y))), s-o),
		Size: o,
	}
}

// CropFromDisplay is handleUpload's mapping of the crop square the user
// drew on the displayed image (x, y, side in display px) to image pixels.
func CropFromDisplay(x, y, side float64, imgW, imgH int) PhotoCrop {
	dw, dh := PhotoDisplaySize(float64(imgW), float64(imgH), PhotoCropMaxSize)
	sy := math.Max(1, float64(imgH)/dh)
	sx := math.Max(1, float64(imgW)/dw)
	return NormalizeSquareCrop(x*sx, y*sy, side*sx, imgW, imgH)
}

// DefaultPhotoCrop is BaseImageCrop's first square on the displayed
// image (dw×dh): defaultCropScale 1, at least the minimum, centred.
func DefaultPhotoCrop(dw, dh float64) (x, y, side float64) {
	e, t := math.Min(dh, PhotoCropMaxSize), math.Min(dw, PhotoCropMaxSize)
	side = math.Max(PhotoCropMinSize, math.Min(e, t)*1)
	return math.Max(0, (t-side)/2), math.Max(0, (e-side)/2), side
}

// SetPhoto is setUserPhotoApi: users.setPhoto {id, crop_x, crop_y,
// crop_w}, then the viewer's member is fetched again for the new
// pictures.
func (a *App) SetPhoto(ctx context.Context, id string, c PhotoCrop) error {
	p := url.Values{"id": {id}, "crop_x": {strconv.Itoa(c.X)}, "crop_y": {strconv.Itoa(c.Y)},
		"crop_w": {strconv.Itoa(c.Size)}}
	if err := a.C.Call(ctx, "users.setPhoto", p, nil); err != nil {
		return err
	}
	a.refreshSelf(ctx)
	return nil
}

// DeletePhoto is usersDeletePhotoFetcher (reason
// "edit_profile_delete_photo"), then the member is fetched again.
func (a *App) DeletePhoto(ctx context.Context) error {
	if err := a.C.Call(ctx, "users.deletePhoto", url.Values{}, nil); err != nil {
		return err
	}
	a.refreshSelf(ctx)
	return nil
}

// refreshSelf fetches the viewer's member (users.info) into the cache, as
// the user_change after a photo change would bring it.
func (a *App) refreshSelf(ctx context.Context) {
	id := a.Self().ID
	if id == "" {
		return
	}
	us, err := a.C.UsersInfoParallel(ctx, []string{id})
	if err != nil || len(us) == 0 {
		return
	}
	a.S.PutUsers(us)
	if a.S.Self.ID == id {
		a.S.Lock()
		a.S.Self = us[0]
		a.S.Unlock()
	}
	if cu, ok := a.User(id); ok {
		a.emit(Event{Type: "user", UserID: id, User: &cu})
	}
}
