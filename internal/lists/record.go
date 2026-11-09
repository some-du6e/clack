package lists

import (
	"encoding/json"
	"strconv"
	"strings"
)

// LinkValue is one entry of a link cell. The API spells these keys in
// camelCase (originalUrl, displayAsUrl, displayName); attachment is the
// unfurl Slack stored for the URL.
type LinkValue struct {
	OriginalURL  string          `json:"originalUrl"`
	DisplayAsURL bool            `json:"displayAsUrl"`
	DisplayName  string          `json:"displayName,omitempty"`
	Attachment   json.RawMessage `json:"attachment,omitempty"`
}

// MessageValue is a message cell: a link to a Slack message.
type MessageValue struct {
	Value     string `json:"value"` // the message's permalink
	ChannelID string `json:"channel_id"`
	TS        string `json:"ts"`
	ThreadTS  string `json:"thread_ts,omitempty"`
}

// Field is one cell of a record as lists.records.list sends it. Only the
// arrays of the column's type are set.
type Field struct {
	Key        string            `json:"key"`
	ColumnID   string            `json:"column_id"`
	Text       *string           `json:"text,omitempty"`
	RichText   json.RawMessage   `json:"rich_text,omitempty"`
	Select     []string          `json:"select,omitempty"`
	User       []string          `json:"user,omitempty"`
	Channel    []string          `json:"channel,omitempty"`
	Date       []string          `json:"date,omitempty"`
	Timestamp  []float64         `json:"timestamp,omitempty"`
	Number     []float64         `json:"number,omitempty"`
	Rating     []float64         `json:"rating,omitempty"`
	Checkbox   *bool             `json:"checkbox,omitempty"`
	Email      []string          `json:"email,omitempty"`
	Phone      []string          `json:"phone,omitempty"`
	Attachment []string          `json:"attachment,omitempty"`
	Link       []LinkValue       `json:"link,omitempty"`
	Message    []MessageValue    `json:"message,omitempty"`
	Reference  []json.RawMessage `json:"reference,omitempty"`
	Raw        json.RawMessage   `json:"-"`
}

// PlainText is the cell's text (text columns: the API's text field).
func (f *Field) PlainText() string {
	if f == nil || f.Text == nil {
		return ""
	}
	return *f.Text
}

// Record is one item of a list.
type Record struct {
	ID               string            `json:"id"`
	ListID           string            `json:"list_id"`
	DateCreated      int64             `json:"date_created"`
	CreatedBy        string            `json:"created_by"`
	UpdatedBy        string            `json:"updated_by"`
	UpdatedTimestamp string            `json:"updated_timestamp"`
	Position         string            `json:"position"`
	ParentRecordID   string            `json:"parent_record_id"`
	ThreadTS         string            `json:"thread_ts"`
	ViewPositions    map[string]string `json:"view_positions"`
	Archived         bool              `json:"archived"`
	IsSubscribed     bool              `json:"is_subscribed"`
	Fields           []Field           `json:"fields"`
}

// Field is the record's cell for a column key (nil when it has none).
func (r *Record) Field(key string) *Field {
	for i := range r.Fields {
		if r.Fields[i].Key == key {
			return &r.Fields[i]
		}
	}
	return nil
}

// SetField replaces (or adds) the record's cell for f.Key.
func (r *Record) SetField(f Field) {
	for i := range r.Fields {
		if r.Fields[i].Key == f.Key {
			r.Fields[i] = f
			return
		}
	}
	r.Fields = append(r.Fields, f)
}

// UpdatedUnix is updated_timestamp as unix seconds (it is a decimal string,
// "1762516900.000100"), else the creation time.
func (r *Record) UpdatedUnix() int64 {
	if s, _, _ := strings.Cut(r.UpdatedTimestamp, "."); s != "" {
		if n, err := strconv.ParseInt(s, 10, 64); err == nil && n > 0 {
			return n
		}
	}
	return r.DateCreated
}

// ParseRecord decodes one record, keeping each field's raw JSON (the
// fidelity guard compares against it).
func ParseRecord(raw json.RawMessage) (Record, error) {
	var r Record
	if err := json.Unmarshal(raw, &r); err != nil {
		return r, err
	}
	var fs struct {
		Fields []json.RawMessage `json:"fields"`
	}
	_ = json.Unmarshal(raw, &fs)
	for i := range r.Fields {
		if i < len(fs.Fields) {
			r.Fields[i].Raw = fs.Fields[i]
		}
	}
	return r, nil
}

// Flat is a cell's value the way Slack's grid holds it (getTypedFieldValue):
// one string, number or boolean, with multi-values joined by "|" (people,
// selects, channels), "," (attachments) or ":" (date ranges). Filters,
// sorts and search read this.
type Flat struct {
	Kind  FlatKind
	S     string
	N     float64
	B     bool
	Rich  json.RawMessage // text columns: the rich_text blocks
	Empty bool            // Slack's isEmptyValue
}

// FlatKind is what a Flat holds.
type FlatKind uint8

const (
	FlatNone FlatKind = iota
	FlatString
	FlatNumber
	FlatBool
	FlatRich
)

// Flatten is Slack's getTypedFieldValue for f (shouldUseTimestamp false:
// dates come back as their YYYY-MM-DD strings).
func Flatten(f *Field) Flat {
	if f == nil {
		return Flat{Empty: true}
	}
	str := func(s string) Flat { return Flat{Kind: FlatString, S: s, Empty: s == "" || s == "false"} }
	num := func(n float64) Flat { return Flat{Kind: FlatNumber, N: n} }
	switch {
	case len(f.Attachment) > 0:
		return str(strings.Join(f.Attachment, ","))
	case len(f.Channel) > 0:
		return str(strings.Join(f.Channel, "|"))
	case f.Checkbox != nil:
		return Flat{Kind: FlatBool, B: *f.Checkbox, Empty: !*f.Checkbox}
	case len(f.Date) > 0:
		return str(strings.Join(f.Date, ":"))
	case len(f.Email) > 0:
		return str(f.Email[0])
	case len(f.Message) > 0 && f.Message[0].Value != "":
		return str(f.Message[0].Value)
	case len(f.Link) > 0:
		return Flat{Kind: FlatString, S: f.Link[0].OriginalURL, Empty: false}
	case len(f.Number) > 0:
		return num(f.Number[0])
	case len(f.Phone) > 0:
		return str(f.Phone[0])
	case len(f.Rating) > 0:
		return num(f.Rating[0])
	case f.RichText != nil && string(f.RichText) != "null":
		var bs []json.RawMessage
		_ = json.Unmarshal(f.RichText, &bs)
		return Flat{Kind: FlatRich, Rich: f.RichText, S: f.PlainText(), Empty: len(bs) == 0}
	case len(f.Select) > 0:
		return str(strings.Join(f.Select, "|"))
	case len(f.User) > 0:
		return str(strings.Join(f.User, "|"))
	case len(f.Timestamp) > 0:
		return num(f.Timestamp[0])
	case len(f.Reference) > 0:
		return str(string(f.Reference[0]))
	}
	return Flat{Empty: true}
}

// Text is the flat value as text (numbers in shortest form).
func (v Flat) Text() string {
	switch v.Kind {
	case FlatNumber:
		return strconv.FormatFloat(v.N, 'f', -1, 64)
	case FlatBool:
		return strconv.FormatBool(v.B)
	}
	return v.S
}

// Parts splits a multi-value flat ("U1|U2") into its values.
func (v Flat) Parts() []string {
	if v.Kind != FlatString || v.S == "" {
		return nil
	}
	var out []string
	for _, p := range strings.Split(v.S, "|") {
		if p != "" {
			out = append(out, p)
		}
	}
	return out
}
