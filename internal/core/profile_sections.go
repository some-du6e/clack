package core

import (
	"context"
	"encoding/json"
	"net/url"
	"strings"
)

// Full-profile sections, as Slack's RimetoMemberProfileFlexpane reads them:
// users.profile.getSections{user, reason: "profiles"} (fetcher
// usersProfileGetSectionsFetcher, gantry-v2-async-client-boot-vendors
// module P8XE) returns result.data.user.profileSections, one section per
// admin-configured profile section (HEADER, CONTACT, PEOPLE,
// AFFILIATIONS, ADDITIONAL_INFO "About me", OTHER_SCIM_FIELDS, CUSTOM),
// each with typed elements. For the viewer's own profile the missing
// elements are included (isMissing) so Slack can offer "Add <label>";
// other people's profiles carry only what's set. Read-only.

// ProfileSection is one section of a member's full profile.
type ProfileSection struct {
	ID, Label string
	Type      string // HEADER, CONTACT, PEOPLE, AFFILIATIONS, ADDITIONAL_INFO, OTHER_SCIM_FIELDS, CUSTOM
	CanEdit   bool
	Elements  []ProfileElement
}

// ProfileElement is one field of a section.
type ProfileElement struct {
	ID, Label, Hint  string
	Typename         string // ProfileTextElement, ProfileDateElement, ProfileLinkElement, ProfilePersonElement, ProfileLongTextElement, ProfileTagsElement, ProfileFileElement, ProfileCurrentLocationElement
	Type             string // TEXT, DATE, LINK, PERSON, LONG_TEXT, TAGS, FILE, ...
	ElementKey       string // email, phone, title, manager, "Start Date", custom keys
	LegacyFieldID    string // the team.profile.get field id (Xf…)
	IsMissing        bool
	IsHidden         bool
	IsFilterable     bool
	IsScimManaged    bool
	EditableInUI     bool // permissions.ui
	Text             string
	Date             string // YYYY-MM-DD
	URI, DisplayText string
	Persons          []string
	Tags             []string
	FileID           string
}

// ProfileSections is a member's full profile layout.
type ProfileSections struct {
	UserID   string
	IsSelf   bool
	Sections []ProfileSection
}

// Section returns the first section of type typ.
func (p ProfileSections) Section(typ string) (ProfileSection, bool) {
	for _, s := range p.Sections {
		if s.Type == typ {
			return s, true
		}
	}
	return ProfileSection{}, false
}

type wireProfileElement struct {
	ID            string `json:"id"`
	Label         string `json:"label"`
	Hint          string `json:"hint"`
	Typename      string `json:"__typename"`
	Type          string `json:"type"`
	ElementKey    string `json:"elementKey"`
	LegacyFieldID string `json:"legacyFieldId"`
	IsMissing     bool   `json:"isMissing"`
	IsHidden      bool   `json:"isHidden"`
	IsFilterable  bool   `json:"isFilterable"`
	IsScimManaged bool   `json:"isScimManaged"`
	Permissions   struct {
		UI bool `json:"ui"`
	} `json:"permissions"`
	Text        *string         `json:"text"`
	Date        *string         `json:"date"`
	URI         *string         `json:"uri"`
	DisplayText *string         `json:"displayText"`
	Persons     []string        `json:"persons"`
	Tags        json.RawMessage `json:"tags"`
	FileID      *string         `json:"fileId"`
}

type wireProfileSections struct {
	Result struct {
		Data struct {
			User struct {
				ID       string `json:"id"`
				IsSelf   bool   `json:"isSelf"`
				Sections []struct {
					ID       string               `json:"id"`
					Label    string               `json:"label"`
					Type     string               `json:"type"`
					CanEdit  bool                 `json:"canEdit"`
					Elements []wireProfileElement `json:"profileElements"`
				} `json:"profileSections"`
			} `json:"user"`
		} `json:"data"`
	} `json:"result"`
}

func strOf(p *string) string {
	if p == nil {
		return ""
	}
	return *p
}

// decodeProfileSections parses a users.profile.getSections response.
func decodeProfileSections(b []byte) (ProfileSections, error) {
	var w wireProfileSections
	if err := json.Unmarshal(b, &w); err != nil {
		return ProfileSections{}, err
	}
	u := w.Result.Data.User
	out := ProfileSections{UserID: u.ID, IsSelf: u.IsSelf}
	for _, s := range u.Sections {
		ps := ProfileSection{ID: s.ID, Label: s.Label, Type: s.Type, CanEdit: s.CanEdit}
		for _, e := range s.Elements {
			pe := ProfileElement{
				ID: e.ID, Label: e.Label, Hint: e.Hint, Typename: e.Typename, Type: e.Type,
				ElementKey: e.ElementKey, LegacyFieldID: e.LegacyFieldID,
				IsMissing: e.IsMissing, IsHidden: e.IsHidden, IsFilterable: e.IsFilterable,
				IsScimManaged: e.IsScimManaged, EditableInUI: e.Permissions.UI,
				Text: strOf(e.Text), Date: strOf(e.Date), URI: strOf(e.URI), DisplayText: strOf(e.DisplayText),
				FileID: strOf(e.FileID),
			}
			for _, p := range e.Persons {
				if p = strings.TrimSpace(p); p != "" {
					pe.Persons = append(pe.Persons, p)
				}
			}
			pe.Tags = decodeTags(e.Tags)
			ps.Elements = append(ps.Elements, pe)
		}
		out.Sections = append(out.Sections, ps)
	}
	return out, nil
}

// decodeTags reads a tags element's values: strings, or objects with a
// label/text/value.
func decodeTags(raw json.RawMessage) []string {
	if len(raw) == 0 || string(raw) == "null" {
		return nil
	}
	var ss []string
	if json.Unmarshal(raw, &ss) == nil {
		return ss
	}
	var os []map[string]any
	if json.Unmarshal(raw, &os) != nil {
		return nil
	}
	for _, o := range os {
		for _, k := range []string{"label", "text", "value"} {
			if v, ok := o[k].(string); ok && v != "" {
				ss = append(ss, v)
				break
			}
		}
	}
	return ss
}

// ProfileSections fetches userID's full-profile sections (Slack's
// useProfileSections: users.profile.getSections with reason "profiles").
// The persons it names are fetched too, so People rows can draw.
func (a *App) ProfileSections(ctx context.Context, userID string) (ProfileSections, error) {
	var raw json.RawMessage
	if err := a.C.Call(ctx, "users.profile.getSections", url.Values{"user": {userID}, "reason": {"profiles"}}, &raw); err != nil {
		return ProfileSections{}, err
	}
	ps, err := decodeProfileSections(raw)
	if err != nil {
		return ProfileSections{}, err
	}
	var people []string
	for _, s := range ps.Sections {
		for _, e := range s.Elements {
			people = append(people, e.Persons...)
		}
	}
	if len(people) > 0 {
		a.EnsureUsers(ctx, people)
	}
	return ps, nil
}
