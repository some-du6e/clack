// Package workobject is Slack's work object model: the third-party
// records (tasks, files, incidents, calendar events…) apps unfurl as
// entity cards, and the details, comments and refresh state the client
// keeps for them.
//
// Evidence: Slack's public web client, CDN build 4d0c78085232
// (2026-11-10), fetched without cookies. WorkObjectModel is client-boot
// JrfS; the entity-type constants LG+m; the app/type constants oyZ+; the
// attachment accessors BOLE (getWorkObject: work_object_entity, else
// work_object) and 5xtd (external work objects).
package workobject

import (
	"bytes"
	"encoding/json"
	"strconv"
	"strings"
)

// Work object types (oyZ+ R7).
const (
	TypeExternal = "external"
	TypeTableau  = "tableau"
)

// Entity types (LG+m C): work_object_entity_type.
const (
	EntityTask             = "slack#/entities/task"
	EntityFile             = "slack#/entities/file"
	EntityItem             = "slack#/entities/item"
	EntityIncident         = "slack#/entities/incident"
	EntityContentItem      = "slack#/entities/content_item"
	EntityTableauAnalytics = "slack#/entities/tableau_analytics"
	EntityCalendarEvent    = "slack#/entities/calendar_event"
)

// Layout keys: the sizes an entity can draw at (JrfS Compact/Expanded/
// Full/MinimalLayout).
const (
	Compact  = "compact"
	Expanded = "expanded"
	Full     = "full"
	Minimal  = "minimal"
)

// ReviewAIContent is oyZ+ tc: the display_type of an AI draft to review.
const ReviewAIContent = "Review AI content"

// Text is a text object ({type, text}).
type Text struct {
	Type string `json:"type,omitempty"`
	Text string `json:"text"`
}

// SlackFile is an image's Slack-hosted file ({id, url}).
type SlackFile struct {
	ID  string `json:"id,omitempty"`
	URL string `json:"url,omitempty"`
}

// Image is an image block as fields, icons and product icons carry it.
type Image struct {
	Type      string     `json:"type,omitempty"`
	ImageURL  string     `json:"image_url,omitempty"`
	AltText   string     `json:"alt_text,omitempty"`
	Width     int        `json:"image_width,omitempty"`
	Height    int        `json:"image_height,omitempty"`
	Title     *Text      `json:"title,omitempty"`
	SlackFile *SlackFile `json:"slack_file,omitempty"`
}

// User is a user field's value: a Slack member (user_type "slack",
// user_id) or someone outside Slack (user_type "external": text, url,
// image).
type User struct {
	UserType string `json:"user_type"`
	UserID   string `json:"user_id,omitempty"`
	Text     string `json:"text,omitempty"`
	URL      string `json:"url,omitempty"`
	Email    string `json:"email,omitempty"`
	Image    *Image `json:"image,omitempty"`
}

// Tag is a tag field's value: text in a colored pill (theme: 33XR ES),
// with an optional icon and link.
type Tag struct {
	Text    string `json:"text"`
	Color   string `json:"color,omitempty"`
	IconURL string `json:"icon_url,omitempty"`
	Link    string `json:"link,omitempty"`
}

// Tag colors (33XR ES).
const (
	TagFlamingo    = "flamingo"
	TagHoneycomb   = "honeycomb"
	TagGrass       = "grass"
	TagGray        = "gray"
	TagInformative = "informative"
	TagIndigo      = "indigo"
	TagLagoon      = "lagoon"
	TagJade        = "jade"
	TagHorchata    = "horchata"
	TagAubergine   = "aubergine"
)

// EntityRef is a reference to another work object.
type EntityRef struct {
	EntityID string `json:"entity_id"`
	Title    string `json:"title"`
	Icon     *Image `json:"icon,omitempty"`
}

// Checkbox is a checkbox field's value.
type Checkbox struct {
	Text        string `json:"text"`
	Description string `json:"description,omitempty"`
	Checked     bool   `json:"checked,omitempty"`
}

// ChannelRef is a channel field's value.
type ChannelRef struct {
	ChannelID string `json:"channel_id"`
}

// FileRef is a file field's value.
type FileRef struct {
	FileID string `json:"file_id"`
}

// HeaderBadge is a header_with_badge field's value (Tableau's metric
// header): text, with the badge as Slack sends it.
type HeaderBadge struct {
	Text  string          `json:"text"`
	Badge json.RawMessage `json:"badge,omitempty"`
}

// Field types (JrfS BaseField.makeField).
const (
	FieldDate            = "date"
	FieldUser            = "user"
	FieldSlackUser       = "slack_user"
	FieldText            = "text"
	FieldRichText        = "rich_text"
	FieldImage           = "image"
	FieldTimestamp       = "timestamp"
	FieldTag             = "tag"
	FieldChannel         = "channel"
	FieldEntityRef       = "entity_ref"
	FieldCheckbox        = "checkbox"
	FieldEmail           = "email"
	FieldLink            = "link"
	FieldFile            = "file"
	FieldHeaderWithBadge = "header_with_badge"
	FieldDateTimeRange   = "date_time_range"
	// FieldUnknown is any other field_type: an UnknownField, drawn from
	// its rich_text.
	FieldUnknown = "unknown"
)

// Field is one entry of a layout's fields.elements, after makeField:
// Type says which value is set.
type Field struct {
	Type  string // one of the Field* types; FieldUnknown for an unrecognized field_type
	Label string
	Name  string // field_name
	Long  bool   // text and rich_text: the long variant (2 lines, 4 alone)

	Text      string          // text
	RichText  json.RawMessage // rich_text, unknown: a rich_text block
	Timestamp json.RawMessage // timestamp[0] as sent (unix seconds, number or string)
	Date      json.RawMessage // date[0] as sent ("2026-11-12" or unix seconds)
	Images    []Image
	Users     []User // user and slack_user
	Channels  []ChannelRef
	Refs      []EntityRef
	Checks    []Checkbox
	Emails    []string
	Links     []string
	Files     []FileRef
	Tags      []Tag
	Header    *HeaderBadge
	Ranges    json.RawMessage // date_time_range as sent

	// Inputs are the field's input blocks (input, or inputs), the ones
	// whose element is an input Slack edits work objects with.
	Inputs []json.RawMessage
	// RecurrenceTimeZone is a calendar field's recurrence_time_zone.
	RecurrenceTimeZone string
}

// IsImage, IsUser … are BaseField's is* checks.
func (f *Field) IsImage() bool    { return f.Type == FieldImage }
func (f *Field) IsUser() bool     { return f.Type == FieldUser || f.Type == FieldSlackUser }
func (f *Field) IsRichText() bool { return f.Type == FieldRichText }
func (f *Field) IsUnknown() bool  { return f.Type == FieldUnknown }
func (f *Field) IsText() bool     { return f.Type == FieldText }

// HasInputs is zZ0S hasFieldInput.
func (f *Field) HasInputs() bool { return len(f.Inputs) > 0 }

// Button is a layout action: a Block Kit button.
type Button struct {
	Type               string `json:"type,omitempty"`
	ActionID           string `json:"action_id,omitempty"`
	BlockID            string `json:"block_id,omitempty"`
	Text               *Text  `json:"text,omitempty"`
	Style              string `json:"style,omitempty"`
	URL                string `json:"url,omitempty"`
	Value              string `json:"value,omitempty"`
	AccessibilityLabel string `json:"accessibility_label,omitempty"`
	AgentPrompt        string `json:"agent_prompt,omitempty"`
}

// Label is the button's text.
func (b *Button) Label() string {
	if b.Text != nil {
		return b.Text.Text
	}
	return ""
}

// BlockPayload is where an action's block_actions dispatch goes.
type BlockPayload struct {
	ServiceID     string `json:"service_id,omitempty"`
	ServiceTeamID string `json:"service_team_id,omitempty"`
	Container     struct {
		Type string `json:"type,omitempty"`
	} `json:"container"`
}

// Actions is a layout's actions: the buttons the card's footer shows
// (primary_actions) and the app's own menu items (overflow_actions).
type Actions struct {
	Primary  []Button `json:"primary_actions,omitempty"`
	Overflow struct {
		AppActions []Button `json:"app_actions,omitempty"`
	} `json:"overflow_actions"`
	BlockPayload BlockPayload `json:"block_payload"`
}

// Layout is one size of an entity (JrfS Layout and its subclasses).
type Layout struct {
	Key            string
	Title          string
	TitleInput     json.RawMessage // full: title.input (an input block)
	Subtitle       string
	SubtitleURL    string // full
	SubtitleImage  *Image // full
	HoverSubtitle  string // compact, expanded
	HeaderTitle    string
	HeaderSubtitle string // full
	ProductIcon    *Image
	// UpdatedAt is updated_at as sent (unix seconds, number or string).
	UpdatedAt json.RawMessage
	Editable  bool // full
	Actions   *Actions
	Fields    []Field
	EntityID  string
}

// HasImage, HasFields are Layout's.
func (l *Layout) HasImage() bool {
	for i := range l.Fields {
		if l.Fields[i].IsImage() {
			return true
		}
	}
	return false
}
func (l *Layout) HasFields() bool { return len(l.Fields) > 0 }

// UpdatedAtUnix is updated_at in unix seconds (0 when absent).
func (l *Layout) UpdatedAtUnix() float64 { return number(l.UpdatedAt) }

// Comments is the entity's comment summary ({count, latest_timestamp}).
type Comments struct {
	Count           int             `json:"count"`
	LatestTimestamp json.RawMessage `json:"latest_timestamp,omitempty"`
}

// FullSizePreview is the entity's full_size_preview: whether Slack can
// show the item inside Slack, and the preview's URL.
type FullSizePreview struct {
	IsSupported bool   `json:"is_supported"`
	PreviewURL  string `json:"preview_url,omitempty"`
	MimeType    string `json:"mime_type,omitempty"`
	Error       *struct {
		Code    string `json:"code,omitempty"`
		Message string `json:"message,omitempty"`
	} `json:"error,omitempty"`
	FullSizePreviewSupported *bool `json:"full_size_preview_supported,omitempty"`
}

// PerspectiveUser is whose view of the item the data is ("As of … from
// {sourceUserName}").
type PerspectiveUser struct {
	UserType string `json:"user_type"`
	UserID   string `json:"user_id,omitempty"`
}

// Entity is WorkObjectModel: an entity's data (the attachment's
// work_object_entity, or workObjects.getEntity's / entity_details_present's
// details) and its layouts.
type Entity struct {
	Type            string // work_object_type
	EntityType      string // work_object_entity_type
	EntityID        string
	RelatedConvoID  string // related_conversations_entity_id
	AppID           string
	AppName         string
	ProductName     string
	DisplayType     string
	ExternalURL     string
	AuthProviderKey string
	LookupFunction  json.RawMessage
	SlackFileID     string
	Status          string // "draft"
	TS              string
	SlugText        string
	SlugIconName    string
	IsPageBlock     bool
	HasAIContent    bool
	PublishActionID string
	SlackbotPrompt  string
	refreshable     *bool
	Comments        *Comments
	FullSizePreview *FullSizePreview
	AppIcons        map[string]*Image
	Perspective     *PerspectiveUser

	layouts map[string]*Layout
	raw     json.RawMessage
}

type entityJSON struct {
	Type            string                     `json:"work_object_type"`
	EntityType      string                     `json:"work_object_entity_type"`
	EntityID        string                     `json:"entity_id"`
	RelatedConvoID  string                     `json:"related_conversations_entity_id"`
	AppID           string                     `json:"app_id"`
	AppName         string                     `json:"app_name"`
	ProductName     string                     `json:"product_name"`
	DisplayType     string                     `json:"display_type"`
	ExternalURL     string                     `json:"external_url"`
	AuthProviderKey string                     `json:"auth_provider_key"`
	LookupFunction  json.RawMessage            `json:"lookup_function"`
	SlackFileID     string                     `json:"slack_file_id"`
	Status          string                     `json:"status"`
	TS              json.RawMessage            `json:"ts"`
	SlugText        string                     `json:"slug_text"`
	SlugIconName    string                     `json:"slug_icon_name"`
	IsPageBlock     bool                       `json:"is_page_block"`
	HasAIContent    bool                       `json:"has_ai_generated_content"`
	PublishActionID string                     `json:"publish_action_id"`
	SlackbotPrompt  string                     `json:"slackbot_prompt"`
	IsRefreshable   *bool                      `json:"is_refreshable"`
	Comments        *Comments                  `json:"comments"`
	FullSizePreview *FullSizePreview           `json:"full_size_preview"`
	AppIcons        map[string]*Image          `json:"app_icons"`
	Perspective     *PerspectiveUser           `json:"perspective_user"`
	Layouts         map[string]json.RawMessage `json:"layouts"`
}

// Parse is WorkObjectModel.create: nil when raw isn't a work object (it
// has no layouts object).
func Parse(raw json.RawMessage) *Entity {
	raw = bytes.TrimSpace(raw)
	if len(raw) == 0 || raw[0] != '{' {
		return nil
	}
	var j entityJSON
	if json.Unmarshal(raw, &j) != nil || j.Layouts == nil {
		return nil
	}
	e := &Entity{
		Type: j.Type, EntityType: j.EntityType, EntityID: j.EntityID, RelatedConvoID: j.RelatedConvoID,
		AppID: j.AppID, AppName: j.AppName, ProductName: j.ProductName, DisplayType: j.DisplayType,
		ExternalURL: j.ExternalURL, AuthProviderKey: j.AuthProviderKey, LookupFunction: j.LookupFunction,
		SlackFileID: j.SlackFileID, Status: j.Status, TS: str(j.TS), SlugText: j.SlugText, SlugIconName: j.SlugIconName,
		IsPageBlock: j.IsPageBlock, HasAIContent: j.HasAIContent, PublishActionID: j.PublishActionID,
		SlackbotPrompt: j.SlackbotPrompt, refreshable: j.IsRefreshable, Comments: j.Comments,
		FullSizePreview: j.FullSizePreview, AppIcons: j.AppIcons, Perspective: j.Perspective,
		layouts: map[string]*Layout{}, raw: append(json.RawMessage(nil), raw...),
	}
	deprecated := false
	for _, k := range []string{Compact, Expanded, Full, Minimal} {
		if l, ok := j.Layouts[k]; ok && fieldsDeprecated(l) {
			deprecated = true
		}
	}
	for _, k := range []string{Compact, Expanded, Full, Minimal} {
		l, ok := j.Layouts[k]
		if !ok || isNull(l) {
			continue
		}
		if lay := parseLayout(k, l, e.EntityID, deprecated); lay != nil {
			e.layouts[k] = lay
		}
	}
	return e
}

// Raw is the entity's JSON as Slack sent it (dangerouslyGetRawData).
func (e *Entity) Raw() json.RawMessage { return e.raw }

// IsExternal is 5xtd W: the entity draws as ExternalWorkObjectEntity.
func (e *Entity) IsExternal() bool { return e != nil && e.Type == TypeExternal }

// Layout returns the layout for key, nil when the entity has none.
func (e *Entity) Layout(key string) *Layout {
	if e == nil {
		return nil
	}
	return e.layouts[key]
}

// Compact, Expanded, Full, Minimal are WorkObjectModel's layout getters.
func (e *Entity) Compact() *Layout  { return e.Layout(Compact) }
func (e *Entity) Expanded() *Layout { return e.Layout(Expanded) }
func (e *Entity) Full() *Layout     { return e.Layout(Full) }
func (e *Entity) Minimal() *Layout  { return e.Layout(Minimal) }

// UnfurlLayout is the card's layout: expanded, else compact, else
// minimal (ExternalWorkObjectEntity's tm).
func (e *Entity) UnfurlLayout() *Layout {
	if l := e.Expanded(); l != nil {
		return l
	}
	if l := e.Compact(); l != nil {
		return l
	}
	return e.Minimal()
}

// HasLayout reports any layout.
func (e *Entity) HasLayout() bool { return e != nil && len(e.layouts) > 0 }

// IsRefreshable is is_refreshable !== false.
func (e *Entity) IsRefreshable() bool { return e.refreshable == nil || *e.refreshable }

// IsDraft is status "draft".
func (e *Entity) IsDraft() bool { return e.Status == "draft" }

// IsFullSizePreviewEnabled is isFullSizePreviewEnabled(true): supported
// and not file_not_supported.
func (e *Entity) IsFullSizePreviewEnabled() bool {
	p := e.FullSizePreview
	return p != nil && p.IsSupported && (p.Error == nil || p.Error.Code != "file_not_supported")
}

// FileID is the entity's Slack file: slack_file_id, else for a file
// entity the first image field's slack_file.
func (e *Entity) FileID() string {
	if e.SlackFileID != "" {
		return e.SlackFileID
	}
	if e.EntityType != EntityFile {
		return ""
	}
	l := e.Expanded()
	if l == nil {
		l = e.Full()
	}
	if l == nil {
		return ""
	}
	for i := range l.Fields {
		if f := &l.Fields[i]; f.IsImage() && len(f.Images) > 0 && f.Images[0].SlackFile != nil && f.Images[0].SlackFile.ID != "" {
			return f.Images[0].SlackFile.ID
		}
	}
	return ""
}

// ContextMenuAppActions is contextMenuAppActions: the app's overflow
// actions, after the primary ones when the entity has no expanded
// layout (the compact card has no footer to show them).
func (e *Entity) ContextMenuAppActions() []Button {
	ex := e.Expanded()
	l := ex
	if l == nil {
		l = e.Compact()
	}
	if l == nil {
		l = e.Minimal()
	}
	if l == nil || l.Key == Minimal || l.Actions == nil {
		return nil
	}
	app := l.Actions.Overflow.AppActions
	if ex != nil {
		return app
	}
	var out []Button
	for _, b := range l.Actions.Primary {
		if b.ActionID != "" && b.BlockID != "" {
			out = append(out, b)
		}
	}
	return append(out, app...)
}

// PerspectiveUserID is the perspective user when they are a Slack member.
func (e *Entity) PerspectiveUserID() string {
	if p := e.Perspective; p != nil && p.UserType == "slack" {
		return p.UserID
	}
	return ""
}

// AppAndObject splits an entity id W:<app>:<object> (ddlI G).
func AppAndObject(entityID string) (app, object string) {
	rest, ok := strings.CutPrefix(entityID, "W:")
	if !ok {
		return "", ""
	}
	app, object, ok = strings.Cut(rest, ":")
	if !ok || app == "" || object == "" {
		return "", ""
	}
	return app, object
}

// ---- layouts and fields ----

type layoutJSON struct {
	Title struct {
		Text  string          `json:"text"`
		Input json.RawMessage `json:"input"`
	} `json:"title"`
	Subtitle *struct {
		Text  string `json:"text"`
		URL   string `json:"url"`
		Image *Image `json:"image"`
	} `json:"subtitle"`
	HoverSubtitle  *Text            `json:"hover_subtitle"`
	HeaderTitle    *Text            `json:"header_title"`
	HeaderSubtitle *Text            `json:"header_subtitle"`
	ProductIcon    *Image           `json:"product_icon"`
	UpdatedAt      json.RawMessage  `json:"updated_at"`
	Editable       bool             `json:"editable"`
	Actions        *Actions         `json:"actions"`
	Fields         *fieldsContainer `json:"fields"`
}

type fieldsContainer struct {
	Elements []json.RawMessage `json:"elements"`
}

func parseLayout(key string, raw json.RawMessage, entityID string, deprecated bool) *Layout {
	var j layoutJSON
	if json.Unmarshal(raw, &j) != nil {
		return nil
	}
	l := &Layout{Key: key, Title: j.Title.Text, ProductIcon: j.ProductIcon, UpdatedAt: j.UpdatedAt,
		Actions: j.Actions, EntityID: entityID}
	if s := j.Subtitle; s != nil {
		l.Subtitle = s.Text
		if key == Full {
			l.SubtitleURL, l.SubtitleImage = s.URL, s.Image
		}
	}
	if j.HoverSubtitle != nil && (key == Compact || key == Expanded) {
		l.HoverSubtitle = j.HoverSubtitle.Text
	}
	if j.HeaderTitle != nil {
		l.HeaderTitle = j.HeaderTitle.Text
	}
	if key == Full {
		if j.HeaderSubtitle != nil {
			l.HeaderSubtitle = j.HeaderSubtitle.Text
		}
		l.Editable = j.Editable
		if isInputBlock(j.Title.Input) {
			l.TitleInput = j.Title.Input
		}
	}
	if key == Minimal {
		l.Actions = nil // MinimalLayout has no actions()
	}
	if j.Fields != nil {
		for _, el := range j.Fields.Elements {
			if deprecated {
				el = transformDeprecatedField(el)
			}
			if f, ok := makeField(el); ok {
				l.Fields = append(l.Fields, f)
			}
		}
	}
	return l
}

type fieldJSON struct {
	FieldType          string            `json:"field_type"`
	Label              string            `json:"label"`
	FieldName          string            `json:"field_name"`
	Long               bool              `json:"long"`
	Text               *string           `json:"text"`
	Badge              json.RawMessage   `json:"badge"`
	RichText           json.RawMessage   `json:"rich_text"`
	Timestamp          []json.RawMessage `json:"timestamp"`
	Date               []json.RawMessage `json:"date"`
	Image              []Image           `json:"image"`
	User               []User            `json:"user"`
	SlackUser          []User            `json:"slack_user"`
	Channel            []ChannelRef      `json:"channel"`
	EntityRef          []EntityRef       `json:"entity_ref"`
	Checkbox           []Checkbox        `json:"checkbox"`
	Email              []string          `json:"email"`
	Link               []string          `json:"link"`
	File               []FileRef         `json:"file"`
	Tag                []Tag             `json:"tag"`
	HeaderWithBadge    *HeaderBadge      `json:"header_with_badge"`
	DateTimeRange      json.RawMessage   `json:"date_time_range"`
	Input              json.RawMessage   `json:"input"`
	Inputs             []json.RawMessage `json:"inputs"`
	RecurrenceTimeZone string            `json:"recurrence_time_zone"`
}

// present reports a JSON property that was sent (not absent; null counts
// as sent, as `void 0 === e.x` does not see null).
func present(m map[string]json.RawMessage, k string) bool {
	_, ok := m[k]
	return ok
}

// makeField is BaseField.makeField: false when the field's value is
// missing (the constructor throws and the field is dropped).
func makeField(raw json.RawMessage) (Field, bool) {
	var j fieldJSON
	var m map[string]json.RawMessage
	if json.Unmarshal(raw, &j) != nil || json.Unmarshal(raw, &m) != nil {
		return Field{}, false
	}
	f := Field{Type: j.FieldType, Label: j.Label, Name: j.FieldName, RecurrenceTimeZone: j.RecurrenceTimeZone}
	inputs := j.Inputs
	if len(j.Input) > 0 && !isNull(j.Input) {
		inputs = []json.RawMessage{j.Input}
	}
	for _, in := range inputs {
		if isInputBlock(in) {
			f.Inputs = append(f.Inputs, in)
		}
	}
	switch j.FieldType {
	case FieldDate:
		if len(j.Date) == 0 {
			return Field{}, false
		}
		f.Date = j.Date[0]
	case FieldUser:
		if !present(m, "user") {
			return Field{}, false
		}
		f.Users = j.User
	case FieldSlackUser:
		if !present(m, "slack_user") {
			return Field{}, false
		}
		f.Users = j.SlackUser
	case FieldText:
		if j.Text == nil {
			return Field{}, false
		}
		f.Text, f.Long = *j.Text, j.Long
	case FieldRichText:
		if !present(m, "rich_text") {
			return Field{}, false
		}
		f.RichText, f.Long = j.RichText, j.Long
	case FieldImage:
		if len(j.Image) == 0 {
			return Field{}, false
		}
		f.Images = j.Image
	case FieldFile:
		if len(j.File) == 0 {
			return Field{}, false
		}
		f.Files = j.File
	case FieldTimestamp:
		if len(j.Timestamp) == 0 {
			return Field{}, false
		}
		f.Timestamp = j.Timestamp[0]
	case FieldTag:
		if len(j.Tag) == 0 {
			return Field{}, false
		}
		f.Tags = j.Tag
	case FieldChannel:
		if !present(m, "channel") {
			return Field{}, false
		}
		f.Channels = j.Channel
	case FieldEntityRef:
		if !present(m, "entity_ref") {
			return Field{}, false
		}
		f.Refs = j.EntityRef
	case FieldCheckbox:
		if !present(m, "checkbox") {
			return Field{}, false
		}
		f.Checks = j.Checkbox
	case FieldEmail:
		if !present(m, "email") {
			return Field{}, false
		}
		f.Emails = j.Email
	case FieldLink:
		if !present(m, "link") {
			return Field{}, false
		}
		f.Links = j.Link
	case FieldHeaderWithBadge:
		switch {
		case j.HeaderWithBadge != nil:
			f.Header = j.HeaderWithBadge
		case j.Text != nil && *j.Text != "":
			f.Header = &HeaderBadge{Text: *j.Text, Badge: j.Badge}
		default:
			return Field{}, false
		}
	case FieldDateTimeRange:
		if !present(m, "date_time_range") {
			return Field{}, false
		}
		f.Ranges = j.DateTimeRange
	default:
		// UnknownField: drawn from its rich_text, dropped without one
		if !present(m, "rich_text") {
			return Field{}, false
		}
		f.Type, f.RichText = FieldUnknown, j.RichText
	}
	return f, true
}

// inputElementTypes are the input elements a work object edits with
// (sRGJ JV, isInputBlock's check on element.type).
var inputElementTypes = map[string]bool{
	"users_select": true, "conversations_select": true, "channels_select": true, "multi_users_select": true,
	"multi_conversations_select": true, "multi_channels_select": true, "multi_static_select": true,
	"multi_external_select": true, "external_select": true, "static_select": true, "datepicker": true,
	"timepicker": true, "plain_text_input": true, "number_input": true, "radio_buttons": true, "checkboxes": true,
	"password": true, "rich_text_input": true, "datetimepicker": true, "url_text_input": true,
	"email_text_input": true, "file_input": true,
}

func isInputBlock(raw json.RawMessage) bool {
	if len(raw) == 0 {
		return false
	}
	var b struct {
		Type    string `json:"type"`
		Element struct {
			Type string `json:"type"`
		} `json:"element"`
	}
	return json.Unmarshal(raw, &b) == nil && b.Type == "input" && inputElementTypes[b.Element.Type]
}

// fieldsDeprecated is fieldsContainsDeprecatedValues: an element whose
// rich_text is an array, or whose field_type is long_text or short_text.
func fieldsDeprecated(layout json.RawMessage) bool {
	var j struct {
		Fields *fieldsContainer `json:"fields"`
	}
	if json.Unmarshal(layout, &j) != nil || j.Fields == nil {
		return false
	}
	for _, el := range j.Fields.Elements {
		var f struct {
			FieldType string          `json:"field_type"`
			RichText  json.RawMessage `json:"rich_text"`
		}
		if json.Unmarshal(el, &f) != nil {
			continue
		}
		if rt := bytes.TrimSpace(f.RichText); len(rt) > 0 && rt[0] == '[' {
			return true
		}
		if f.FieldType == "long_text" || f.FieldType == "short_text" {
			return true
		}
	}
	return false
}

// transformDeprecatedField is transformDeprecatedArrayFields for one
// element: rich_text arrays become their first block, long_text becomes
// rich_text, short_text rich_text (with rich text) or text.
func transformDeprecatedField(el json.RawMessage) json.RawMessage {
	var m map[string]json.RawMessage
	if json.Unmarshal(el, &m) != nil {
		return el
	}
	// 7RVK head: the first element of an array (a string's first
	// character, which no field sends), undefined for anything else
	var first json.RawMessage
	if rt := bytes.TrimSpace(m["rich_text"]); len(rt) > 0 && rt[0] == '[' {
		var arr []json.RawMessage
		if json.Unmarshal(rt, &arr) == nil && len(arr) > 0 {
			first = arr[0]
		}
	}
	delete(m, "rich_text")
	var ft string
	_ = json.Unmarshal(m["field_type"], &ft)
	switch ft {
	case "long_text":
		ft = FieldRichText
	case "short_text":
		if first != nil {
			ft = FieldRichText
		} else {
			ft = FieldText
		}
	}
	m["field_type"], _ = json.Marshal(ft)
	if first != nil {
		m["rich_text"] = first
	}
	out, err := json.Marshal(m)
	if err != nil {
		return el
	}
	return out
}

func isNull(raw json.RawMessage) bool { return string(bytes.TrimSpace(raw)) == "null" }

// str is a JSON string or number as a string ("" otherwise).
func str(raw json.RawMessage) string {
	raw = bytes.TrimSpace(raw)
	if len(raw) == 0 || isNull(raw) {
		return ""
	}
	var s string
	if json.Unmarshal(raw, &s) == nil {
		return s
	}
	var n json.Number
	if json.Unmarshal(raw, &n) == nil {
		return n.String()
	}
	return ""
}

// number is a JSON number or numeric string as a float (0 otherwise).
func number(raw json.RawMessage) float64 {
	s := str(raw)
	if s == "" {
		return 0
	}
	v, err := strconv.ParseFloat(s, 64)
	if err != nil {
		return 0
	}
	return v
}

// Number is number for callers outside the package (a field's
// timestamp, a comment's latest_timestamp).
func Number(raw json.RawMessage) float64 { return number(raw) }

// String is str for callers outside the package (a field's date).
func String(raw json.RawMessage) string { return str(raw) }
