package lists

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"strings"
)

// Edit is a new value for one cell. Only the fields of the column's type
// are read: Text (text, email, phone, number as typed), Values (selects,
// people, channels, dates, votes), Number, Check, Rating, Link.
type Edit struct {
	RowID  string // the record ("" in lists.records.create)
	Column *Column
	Text   string
	Values []string
	Number *float64
	Check  bool
	Rating int
	Link   *LinkValue
	Clear  bool // empty the cell
	// Rich is a text cell's rich_text blocks as Clack's rich editor wrote
	// them (formatted cells, listview's richedit.go); they are sent as
	// they are, and Text is their plain text.
	Rich json.RawMessage
}

// TextBlocks is Slack's text-cell value for plain text (AhXZ.u1): a link
// element when the whole text is one URL, else a text element.
func TextBlocks(s string) []any {
	if s == "" {
		return []any{}
	}
	el := map[string]any{"type": "text", "text": s}
	if u, err := url.Parse(strings.TrimSpace(s)); err == nil && (u.Scheme == "http" || u.Scheme == "https") && u.Host != "" && !strings.ContainsAny(strings.TrimSpace(s), " \n") {
		el = map[string]any{"type": "link", "url": strings.TrimSpace(s)}
	}
	return []any{map[string]any{"type": "rich_text", "elements": []any{
		map[string]any{"type": "rich_text_section", "elements": []any{el}}}}}
}

// Typed is the cell's typed value in lists.cells.update's shape (Slack's
// xWHc "v2 cells update schema"), keys already snake_case as the client's
// fetcher sends them.
func (e Edit) Typed() (map[string]any, error) {
	c := e.Column
	if c == nil {
		return nil, errors.New("lists: no column")
	}
	out := map[string]any{}
	list := func(vs []string) []string {
		if e.Clear || vs == nil {
			return []string{}
		}
		return vs
	}
	switch c.Type {
	case Text, RichText:
		switch {
		case e.Clear:
			out["rich_text"] = []any{}
		case len(e.Rich) > 0:
			out["rich_text"] = e.Rich
		case e.Text == "":
			out["rich_text"] = []any{}
		default:
			out["rich_text"] = TextBlocks(e.Text)
		}
	case Number, Currency:
		if e.Clear || e.Number == nil {
			out["number"] = []float64{}
		} else {
			out["number"] = []float64{*e.Number}
		}
	case Select, MultiSelect:
		out["select"] = list(e.Values)
	case Date, DueDate, TodoDueDate:
		out["date"] = list(e.Values)
	case User, Assignee, TodoAssignee, Vote:
		out["user"] = list(e.Values)
	case Channel:
		out["channel"] = list(e.Values)
	case Checkbox, Completed, TodoCompleted:
		out["checkbox"] = e.Check && !e.Clear
	case Email:
		out["email"] = list(nonEmpty(e.Text))
	case Phone:
		out["phone"] = list(nonEmpty(e.Text))
	case Rating:
		if e.Clear || e.Rating <= 0 {
			out["rating"] = []int{}
		} else {
			out["rating"] = []int{e.Rating}
		}
	case Link:
		if e.Clear || e.Link == nil || e.Link.OriginalURL == "" {
			out["link"] = []any{}
		} else {
			l := map[string]any{"original_url": e.Link.OriginalURL, "display_as_url": e.Link.DisplayAsURL}
			if e.Link.DisplayName != "" {
				l["display_name"] = e.Link.DisplayName
			}
			out["link"] = []any{l}
		}
	default:
		return nil, fmt.Errorf("lists: %s cells can't be written", c.Type)
	}
	return out, nil
}

func nonEmpty(s string) []string {
	if s = strings.TrimSpace(s); s == "" {
		return []string{}
	}
	return []string{s}
}

// Cell is one entry of lists.cells.update's cells.
func (e Edit) Cell() (map[string]any, error) {
	m, err := e.Typed()
	if err != nil {
		return nil, err
	}
	m["row_id"] = e.RowID
	m["column_id"] = e.Column.ID
	return m, nil
}

// InitialField is one entry of lists.records.create's initial_fields.
func (e Edit) InitialField() (map[string]any, error) {
	m, err := e.Typed()
	if err != nil {
		return nil, err
	}
	m["column_id"] = e.Column.ID
	return m, nil
}

// Apply returns f with the edit applied, the way the server will store it
// (the optimistic value the grid shows until the echo arrives).
func (e Edit) Apply(f Field) Field {
	c := e.Column
	nf := Field{Key: c.Key, ColumnID: c.ID}
	switch c.Type {
	case Text, RichText:
		t := e.Text
		if e.Clear {
			t = ""
		}
		nf.Text = &t
		b, _ := json.Marshal(TextBlocks(t))
		if t == "" {
			b = []byte("[]")
		}
		if len(e.Rich) > 0 && !e.Clear {
			b = append(json.RawMessage(nil), e.Rich...)
		}
		nf.RichText = b
	case Number, Currency:
		if !e.Clear && e.Number != nil {
			nf.Number = []float64{*e.Number}
		}
	case Select, MultiSelect:
		nf.Select = clone(e.Values, e.Clear)
	case Date, DueDate, TodoDueDate:
		nf.Date = clone(e.Values, e.Clear)
	case User, Assignee, TodoAssignee, Vote:
		nf.User = clone(e.Values, e.Clear)
	case Channel:
		nf.Channel = clone(e.Values, e.Clear)
	case Checkbox, Completed, TodoCompleted:
		b := e.Check && !e.Clear
		nf.Checkbox = &b
	case Email:
		nf.Email = clone(nonEmpty(e.Text), e.Clear)
	case Phone:
		nf.Phone = clone(nonEmpty(e.Text), e.Clear)
	case Rating:
		if !e.Clear && e.Rating > 0 {
			nf.Rating = []float64{float64(e.Rating)}
		}
	case Link:
		if !e.Clear && e.Link != nil && e.Link.OriginalURL != "" {
			nf.Link = []LinkValue{*e.Link}
		}
	default:
		return f
	}
	return nf
}

func clone(vs []string, clear bool) []string {
	if clear {
		return nil
	}
	return append([]string(nil), vs...)
}

// ---- the fidelity guard ----

// ErrReadOnly is returned for columns Slack computes.
var ErrReadOnly = errors.New("Slack fills this field in")

// Unsupported says why a cell can't be edited in Clack without losing
// something (shown as "This field can't be edited in Clack yet (it has …)").
type Unsupported struct{ What string }

func (u Unsupported) Error() string { return "it has " + u.What }

// Editable reports whether Clack can edit the cell without changing
// anything it doesn't show: computed columns are Slack's; message,
// attachment, canvas and reference cells need pickers Clack doesn't have;
// text is editable only while it is plain text and links (no formatting,
// mentions, emoji, lists, quotes or code), so writing it back as plain
// text loses nothing; date ranges and links with several URLs stay
// read-only.
func Editable(c *Column, f *Field) error {
	if c == nil {
		return Unsupported{"no column"}
	}
	switch c.Type {
	case CreatedBy, LastEditedBy, CreatedTime, LastEditedTime:
		return ErrReadOnly
	case Message:
		return Unsupported{"a message link"}
	case Attachment:
		return Unsupported{"files"}
	case Canvas:
		return Unsupported{"a canvas"}
	case Reference:
		return Unsupported{"a reference"}
	case Text, RichText:
		if f == nil || len(f.RichText) == 0 {
			return nil
		}
		s, why := PlainRichText(f.RichText)
		if why != "" {
			return Unsupported{why}
		}
		// the round trip: what Clack would write back reads as the same text
		b, _ := json.Marshal(TextBlocks(s))
		if back, w := PlainRichText(b); w != "" || back != s {
			return Unsupported{"text Clack can't write back"}
		}
		return nil
	case Date, DueDate, TodoDueDate:
		if c.Options.Format == "range" || (f != nil && len(f.Date) > 1) {
			return Unsupported{"a date range"}
		}
	case Link:
		if f != nil && len(f.Link) > 1 {
			return Unsupported{"several links"}
		}
	case Number, Currency, Select, MultiSelect, User, Assignee, TodoAssignee, Vote, Channel,
		Checkbox, Completed, TodoCompleted, Email, Phone, Rating:
	default:
		return Unsupported{"a " + c.Type + " field"}
	}
	return nil
}

// PlainRichText reads text-cell rich_text as plain text. why is non-empty
// when the blocks hold anything plain text can't say (and names it).
func PlainRichText(raw json.RawMessage) (text, why string) {
	var blocks []struct {
		Type     string `json:"type"`
		Elements []struct {
			Type     string            `json:"type"`
			Elements []json.RawMessage `json:"elements"`
		} `json:"elements"`
	}
	if len(raw) == 0 || string(raw) == "null" {
		return "", ""
	}
	if err := json.Unmarshal(raw, &blocks); err != nil {
		return "", "text Clack can't read"
	}
	if len(blocks) > 1 {
		return "", "several blocks"
	}
	var b strings.Builder
	for _, bl := range blocks {
		if bl.Type != "rich_text" {
			return "", "a " + bl.Type + " block"
		}
		for i, sec := range bl.Elements {
			switch sec.Type {
			case "rich_text_section":
			case "rich_text_list":
				return "", "a list"
			case "rich_text_quote":
				return "", "a quote"
			case "rich_text_preformatted":
				return "", "a code block"
			default:
				return "", "a " + strings.TrimPrefix(sec.Type, "rich_text_")
			}
			if i > 0 {
				b.WriteString("\n")
			}
			for _, el := range sec.Elements {
				var e struct {
					Type  string          `json:"type"`
					Text  string          `json:"text"`
					URL   string          `json:"url"`
					Style json.RawMessage `json:"style"`
				}
				if json.Unmarshal(el, &e) != nil {
					return "", "text Clack can't read"
				}
				if len(e.Style) > 0 && string(e.Style) != "null" && string(e.Style) != "{}" {
					return "", "formatting"
				}
				switch e.Type {
				case "text":
					b.WriteString(e.Text)
				case "link":
					if e.Text != "" && e.Text != e.URL {
						return "", "a named link"
					}
					b.WriteString(e.URL)
				case "user", "usergroup":
					return "", "a mention"
				case "channel":
					return "", "a channel link"
				case "broadcast":
					return "", "an @-mention of everyone"
				case "emoji":
					return "", "emoji"
				case "date":
					return "", "a date"
				default:
					return "", "a " + e.Type
				}
			}
		}
	}
	return b.String(), ""
}
