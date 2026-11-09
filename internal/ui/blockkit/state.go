package blockkit

import (
	"encoding/json"
	"strconv"
	"strings"
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

	defaulted bool // default_to_current_conversation applied
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
	return textValue(v.Type, v.Text) == ""
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
		m["value"] = nul(textValue(v.Type, v.Text))
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
// b with value v (nil for buttons): exactly the object each of Slack's
// element components hands onAction (gantry-v2 2026-11, camelCase there,
// snake_cased by the sendActions fetcher). Per type:
//
//	button              type block_id action_id text value? style?
//	icon_button         type block_id action_id icon text value?
//	static/external     … selected_option|null initial_option? placeholder?
//	multi_static/ext    … selected_options initial_options? placeholder?
//	users_select        … selected_user|null initial_user?
//	multi_users_select  … selected_users initial_users?
//	conversations_select … selected_conversation|null initial_conversation? response_url_enabled?
//	multi_conversations … selected_conversations initial_conversations?
//	channels_select     … selected_channel|null initial_channel? response_url_enabled?
//	multi_channels      … selected_channels initial_channels?
//	datepicker          … selected_date|null initial_date?
//	timepicker          … selected_time|null initial_time? timezone?
//	datetimepicker      … selected_date_time|null initial_date_time?
//	checkboxes          … selected_options (no initial_options)
//	radio_buttons       … selected_option|null initial_option?
//	text inputs         … value (trimmed)|null initial_value?
//	rich_text_input     … rich_text_value
//	file_input          … files
//
// Selects carry no placeholder except static/external ones; workflow
// buttons never send an action (they trip their trigger).
func actionPayload(blockID string, e *Element, v *Value) json.RawMessage {
	m := map[string]any{"type": e.Type, "block_id": blockID, "action_id": e.ActionID}
	raw := func(k string) json.RawMessage { return rawField(e.Raw, k) }
	setRaw := func(k string) {
		if r := raw(k); len(r) > 0 && string(r) != "null" {
			m[k] = r
		}
	}
	if v == nil {
		v = &Value{Type: e.Type}
	}
	switch e.Type {
	case "button", "workflow_button":
		if r := raw("text"); len(r) > 0 {
			m["text"] = r
		} else if e.Text != nil {
			m["text"] = e.Text
		}
		if e.Value != "" {
			m["value"] = e.Value
		}
		if e.Style != "" {
			m["style"] = e.Style
		}
	case "icon_button":
		m["icon"] = e.Icon
		if r := raw("text"); len(r) > 0 {
			m["text"] = r
		} else if e.Text != nil {
			m["text"] = e.Text
		}
		if e.Value != "" {
			m["value"] = e.Value
		}
	case "static_select", "external_select":
		v.fields(m)
		setRaw("initial_option")
		setRaw("placeholder")
	case "multi_static_select", "multi_external_select":
		v.fields(m)
		setRaw("initial_options")
		setRaw("placeholder")
	case "radio_buttons":
		v.fields(m)
		setRaw("initial_option")
	case "checkboxes":
		v.fields(m)
	case "users_select":
		v.fields(m)
		setRaw("initial_user")
	case "multi_users_select":
		v.fields(m)
		setRaw("initial_users")
	case "conversations_select":
		v.fields(m)
		setRaw("initial_conversation")
		if e.ResponseURLEnabled {
			m["response_url_enabled"] = true
		}
	case "multi_conversations_select":
		v.fields(m)
		setRaw("initial_conversations")
	case "channels_select":
		v.fields(m)
		setRaw("initial_channel")
		if e.ResponseURLEnabled {
			m["response_url_enabled"] = true
		}
	case "multi_channels_select":
		v.fields(m)
		setRaw("initial_channels")
	case "datepicker":
		v.fields(m)
		setRaw("initial_date")
	case "timepicker":
		v.fields(m)
		setRaw("initial_time")
		if e.Timezone != "" {
			m["timezone"] = e.Timezone
		}
	case "datetimepicker":
		v.fields(m)
		if e.InitialDateTime != 0 {
			m["initial_date_time"] = e.InitialDateTime
		}
	case "plain_text_input", "email_text_input", "url_text_input", "number_input":
		v.fields(m)
		if s := e.initialText(); s != "" {
			m["initial_value"] = s
		}
	default:
		v.fields(m)
	}
	b, _ := json.Marshal(m)
	return b
}

// rawField is key of a JSON object (nil when absent).
func rawField(obj json.RawMessage, key string) json.RawMessage {
	if len(obj) == 0 {
		return nil
	}
	var m map[string]json.RawMessage
	if json.Unmarshal(obj, &m) != nil {
		return nil
	}
	return m[key]
}

// overflowPayload is an overflow menu choice: Slack's OverflowElement
// sends selected_option as just {text, value} (no description or url).
func overflowPayload(blockID string, e *Element, o *Option) json.RawMessage {
	text := rawField(o.Raw, "text")
	if len(text) == 0 {
		text, _ = json.Marshal(o.Text)
	}
	sel := map[string]any{"text": text, "value": o.Value}
	m := map[string]any{"type": "overflow", "block_id": blockID, "action_id": e.ActionID, "selected_option": sel}
	b, _ := json.Marshal(m)
	return b
}

// textValue is a typed value as Slack records it: trimmed, and a number
// input's normalized by Number(x).toString() ("007" → "7", "1.50" →
// "1.5"); "" is null.
func textValue(typ, s string) string {
	s = strings.TrimSpace(s)
	if typ == "number_input" && s != "" && s != "-" && s != "." {
		if f, err := strconv.ParseFloat(s, 64); err == nil {
			return strconv.FormatFloat(f, 'f', -1, 64)
		}
	}
	return s
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

// shortDateLabel is a message datepicker's value: moment's "L"
// (MM/DD/YYYY in en-US), or "Today".
func shortDateLabel(s string, now time.Time) string {
	t, ok := parseDate(s)
	if !ok {
		return s
	}
	if t.Year() == now.Year() && t.YearDay() == now.YearDay() {
		return "Today"
	}
	return t.Format("01/02/2006")
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
