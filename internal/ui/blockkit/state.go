package blockkit

import (
	"encoding/json"
	"strconv"
	"time"
)

// Value is one stateful element's current value, in Slack's
// view.state.values shape (which is also what selections add to an
// action payload).
type Value struct {
	Type string

	SelectedOption  *Option
	SelectedOptions []Option
	options         bool // SelectedOptions is set (even when empty)

	SelectedUser  string
	SelectedUsers []string
	SelectedConv  string
	SelectedConvs []string
	SelectedChan  string
	SelectedChans []string
	multiIDs      bool // the plural id list is set

	SelectedDate     string // YYYY-MM-DD
	SelectedTime     string // HH:mm
	Timezone         string
	SelectedDateTime int64

	Text    string // text inputs
	textSet bool
	Rich    json.RawMessage   // rich_text_input
	Files   []json.RawMessage // file_input: Slack file objects
}

// initialValue is an element's starting state (its initial_* fields).
func initialValue(e *Element) *Value {
	v := &Value{Type: e.Type}
	switch e.Type {
	case "static_select", "external_select", "radio_buttons":
		if e.InitialOption != nil {
			o := *e.InitialOption
			v.SelectedOption = &o
		}
	case "multi_static_select", "multi_external_select", "checkboxes":
		v.SelectedOptions = append([]Option(nil), e.InitialOptions...)
		v.options = true
	case "users_select":
		v.SelectedUser = e.InitialUser
	case "multi_users_select":
		v.SelectedUsers, v.multiIDs = append([]string(nil), e.InitialUsers...), true
	case "conversations_select":
		v.SelectedConv = e.InitialConversation
	case "multi_conversations_select":
		v.SelectedConvs, v.multiIDs = append([]string(nil), e.InitialConvs...), true
	case "channels_select":
		v.SelectedChan = e.InitialChannel
	case "multi_channels_select":
		v.SelectedChans, v.multiIDs = append([]string(nil), e.InitialChannels...), true
	case "datepicker":
		v.SelectedDate = e.InitialDate
	case "timepicker":
		v.SelectedTime, v.Timezone = e.InitialTime, e.Timezone
	case "datetimepicker":
		v.SelectedDateTime = e.InitialDateTime
	case "plain_text_input", "email_text_input", "url_text_input", "number_input":
		v.Text = e.initialText()
		v.textSet = v.Text != ""
	case "rich_text_input":
		if len(e.InitialValue) > 0 && e.InitialValue[0] == '{' {
			v.Rich = e.InitialValue
		}
	}
	return v
}

// empty reports whether nothing is selected / entered.
func (v *Value) empty() bool {
	if v == nil {
		return true
	}
	switch v.Type {
	case "static_select", "external_select", "radio_buttons", "overflow":
		return v.SelectedOption == nil
	case "multi_static_select", "multi_external_select", "checkboxes":
		return len(v.SelectedOptions) == 0
	case "users_select":
		return v.SelectedUser == ""
	case "multi_users_select":
		return len(v.SelectedUsers) == 0
	case "conversations_select":
		return v.SelectedConv == ""
	case "multi_conversations_select":
		return len(v.SelectedConvs) == 0
	case "channels_select":
		return v.SelectedChan == ""
	case "multi_channels_select":
		return len(v.SelectedChans) == 0
	case "datepicker":
		return v.SelectedDate == ""
	case "timepicker":
		return v.SelectedTime == ""
	case "datetimepicker":
		return v.SelectedDateTime == 0
	case "rich_text_input":
		return len(v.Rich) == 0 && v.Text == ""
	case "file_input":
		return len(v.Files) == 0
	}
	return v.Text == ""
}

// fields adds the value's keys to m, the way Slack serializes state
// (selected_option is null when nothing is chosen; text inputs send
// value:null when empty).
func (v *Value) fields(m map[string]any) {
	opt := func(o *Option) any {
		if o == nil {
			return nil
		}
		return o.payload()
	}
	opts := func(os []Option) any {
		out := make([]json.RawMessage, 0, len(os))
		for i := range os {
			out = append(out, os[i].payload())
		}
		return out
	}
	ids := func(s []string) []string {
		if s == nil {
			return []string{}
		}
		return s
	}
	nul := func(s string) any {
		if s == "" {
			return nil
		}
		return s
	}
	switch v.Type {
	case "static_select", "external_select", "radio_buttons", "overflow":
		m["selected_option"] = opt(v.SelectedOption)
	case "multi_static_select", "multi_external_select", "checkboxes":
		m["selected_options"] = opts(v.SelectedOptions)
	case "users_select":
		m["selected_user"] = nul(v.SelectedUser)
	case "multi_users_select":
		m["selected_users"] = ids(v.SelectedUsers)
	case "conversations_select":
		m["selected_conversation"] = nul(v.SelectedConv)
	case "multi_conversations_select":
		m["selected_conversations"] = ids(v.SelectedConvs)
	case "channels_select":
		m["selected_channel"] = nul(v.SelectedChan)
	case "multi_channels_select":
		m["selected_channels"] = ids(v.SelectedChans)
	case "datepicker":
		m["selected_date"] = nul(v.SelectedDate)
	case "timepicker":
		m["selected_time"] = nul(v.SelectedTime)
		if v.Timezone != "" {
			m["timezone"] = v.Timezone
		}
	case "datetimepicker":
		if v.SelectedDateTime != 0 {
			m["selected_date_time"] = v.SelectedDateTime
		} else {
			m["selected_date_time"] = nil
		}
	case "rich_text_input":
		if len(v.Rich) > 0 {
			m["rich_text_value"] = v.Rich
		} else {
			m["rich_text_value"] = richFromText(v.Text)
		}
	case "plain_text_input", "email_text_input", "url_text_input", "number_input":
		m["value"] = nul(v.Text)
	case "file_input":
		fs := v.Files
		if fs == nil {
			fs = []json.RawMessage{}
		}
		m["files"] = fs
	}
}

// richFromText is the rich_text value of plain text (one section per
// paragraph), for rich_text_input typed as plain text.
func richFromText(s string) json.RawMessage {
	type elem struct {
		Type string `json:"type"`
		Text string `json:"text"`
	}
	type section struct {
		Type     string `json:"type"`
		Elements []elem `json:"elements"`
	}
	var secs []section
	if s != "" {
		secs = append(secs, section{Type: "rich_text_section", Elements: []elem{{Type: "text", Text: s}}})
	} else {
		secs = []section{}
	}
	b, _ := json.Marshal(map[string]any{"type": "rich_text", "elements": secs})
	return b
}

// stateJSON is a value as a view.state.values entry: {"type": …, …}.
func (v *Value) stateJSON() map[string]any {
	m := map[string]any{"type": v.Type}
	v.fields(m)
	return m
}

// Values is a surface's state: block_id → action_id → value.
type Values map[string]map[string]*Value

func (vs Values) get(blockID, actionID string) *Value {
	if b := vs[blockID]; b != nil {
		return b[actionID]
	}
	return nil
}

func (vs Values) set(blockID, actionID string, v *Value) {
	b := vs[blockID]
	if b == nil {
		b = map[string]*Value{}
		vs[blockID] = b
	}
	b[actionID] = v
}

// initValues fills the state of every stateful element of bs (keeping
// values already there).
func initValues(vs Values, bs []Block) {
	for i := range bs {
		b := &bs[i]
		for _, e := range elementsOf(b) {
			if !stateful(e.Type) {
				continue
			}
			if vs.get(b.BlockID, e.ActionID) == nil {
				vs.set(b.BlockID, e.ActionID, initialValue(e))
			}
		}
	}
}

// JSON serializes the state values the way Slack sends them to
// views.submit and blocks.actions (state.values). With onlySet, only
// elements that have a value (and checkboxes, which Slack always
// reports) are included: that's message state.
func (vs Values) JSON(onlySet bool) json.RawMessage {
	out := map[string]map[string]any{}
	for bid, acts := range vs {
		for aid, v := range acts {
			if onlySet && v.empty() && v.Type != "checkboxes" {
				continue
			}
			if out[bid] == nil {
				out[bid] = map[string]any{}
			}
			out[bid][aid] = v.stateJSON()
		}
	}
	b, _ := json.Marshal(out)
	return b
}

// actionPayload is actions[0] of blocks.actions for element e of block
// b with value v (nil for buttons): the fields Slack's client sends for
// that element type (type, block_id, action_id, its label/value/style
// and, for inputs and selects, the selection plus initial_* and
// placeholder).
func actionPayload(blockID string, e *Element, v *Value) json.RawMessage {
	m := map[string]any{"type": e.Type, "block_id": blockID, "action_id": e.ActionID}
	switch e.Type {
	case "button", "workflow_button":
		if e.Text != nil {
			m["text"] = e.Text
		}
		if e.Value != "" {
			m["value"] = e.Value
		}
		if e.Style != "" {
			m["style"] = e.Style
		}
		if e.URL != "" {
			m["url"] = e.URL
		}
		if len(e.Workflow) > 0 {
			m["workflow"] = e.Workflow
		}
	default:
		if v != nil {
			v.fields(m)
		}
		if e.Placeholder != nil {
			m["placeholder"] = e.Placeholder
		}
		switch e.Type {
		case "static_select", "external_select", "radio_buttons":
			if e.InitialOption != nil {
				m["initial_option"] = e.InitialOption.payload()
			}
		case "multi_static_select", "multi_external_select", "checkboxes":
			if len(e.InitialOptions) > 0 {
				ops := make([]json.RawMessage, len(e.InitialOptions))
				for i := range e.InitialOptions {
					ops[i] = e.InitialOptions[i].payload()
				}
				m["initial_options"] = ops
			}
		case "users_select":
			if e.InitialUser != "" {
				m["initial_user"] = e.InitialUser
			}
		case "conversations_select":
			if e.InitialConversation != "" {
				m["initial_conversation"] = e.InitialConversation
			}
		case "channels_select":
			if e.InitialChannel != "" {
				m["initial_channel"] = e.InitialChannel
			}
		case "datepicker":
			if e.InitialDate != "" {
				m["initial_date"] = e.InitialDate
			}
		case "timepicker":
			if e.InitialTime != "" {
				m["initial_time"] = e.InitialTime
			}
		case "datetimepicker":
			if e.InitialDateTime != 0 {
				m["initial_date_time"] = e.InitialDateTime
			}
		}
	}
	b, _ := json.Marshal(m)
	return b
}

// overflowPayload is an overflow menu choice: Slack sends the option as
// selected_option.
func overflowPayload(blockID string, e *Element, o *Option) json.RawMessage {
	m := map[string]any{"type": "overflow", "block_id": blockID, "action_id": e.ActionID, "selected_option": o.payload()}
	b, _ := json.Marshal(m)
	return b
}

// ---- dates ----

func parseDate(s string) (time.Time, bool) {
	t, err := time.ParseInLocation("2006-01-02", s, time.Local)
	return t, err == nil
}

func formatDate(t time.Time) string { return t.Format("2006-01-02") }

// parseHM parses "HH:mm" into minutes after midnight.
func parseHM(s string) (int, bool) {
	if len(s) != 5 || s[2] != ':' {
		return 0, false
	}
	h, err1 := strconv.Atoi(s[:2])
	m, err2 := strconv.Atoi(s[3:])
	if err1 != nil || err2 != nil || h > 23 || m > 59 {
		return 0, false
	}
	return h*60 + m, true
}

func formatHM(mins int) string {
	return twoDigits(mins/60) + ":" + twoDigits(mins%60)
}

func twoDigits(n int) string {
	if n < 10 {
		return "0" + strconv.Itoa(n)
	}
	return strconv.Itoa(n)
}

// dateLabel is how Slack shows a picked date in the picker field:
// "Thursday, November 6th" style is for the calendar header; the field
// shows "Nov 6, 2026".
func dateLabel(s string) string {
	t, ok := parseDate(s)
	if !ok {
		return s
	}
	return t.Format("Jan 2, 2006")
}

// timeLabel shows "HH:mm" as Slack does ("3:30 PM").
func timeLabel(s string) string {
	m, ok := parseHM(s)
	if !ok {
		return s
	}
	h := m / 60
	ap := "AM"
	if h >= 12 {
		ap = "PM"
	}
	if h%12 == 0 {
		h = 12
	} else {
		h %= 12
	}
	return strconv.Itoa(h) + ":" + twoDigits(m%60) + " " + ap
}
