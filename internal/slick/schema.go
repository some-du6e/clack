package slick

import (
	"bytes"
	"encoding/json"
	"math"
	"regexp"
	"sort"
)

// Slick's typed settings schema (src/shared/settings.ts in Slick): the
// plugin's static `settings` drives its config, Preferences and coercion.
// Coercion is central, so a hand-edited settings.json can never reach a
// plugin as the wrong type. Implemented here so Preferences > Slick and
// settings.json work without starting the JS engine.

// Categories are Slick's PLUGIN_CATEGORIES, in display order.
var Categories = []Category{
	{"messages", "Messages"},
	{"composer", "Composer"},
	{"people", "People"},
	{"channels", "Channels"},
	{"sounds", "Huddles & sounds"},
	{"privacy", "Privacy"},
	{"appearance", "Look & feel"},
	{"app", "App"},
	{"hackclub", "Hack Club Specialities"},
}

// Category is one plugin category: its key and label.
type Category struct{ Key, Label string }

func categoryLabel(key string) (string, bool) {
	for _, c := range Categories {
		if c.Key == key {
			return c.Label, true
		}
	}
	return "", false
}

// SelectOption is a select choice (also a schedule status).
type SelectOption struct {
	Value string `json:"value"`
	Label string `json:"label"`
}

// ScheduleRule is one OfficeHours-style rule.
type ScheduleRule struct {
	Days   []int  `json:"days"`
	Start  string `json:"start"`
	End    string `json:"end"`
	Status string `json:"status"`
}

// Setting is one entry of a plugin's schema. Type is boolean | number |
// text | color | select | file | names | schedule.
type Setting struct {
	Type            string          `json:"type"`
	Label           string          `json:"label"`
	Description     string          `json:"description,omitempty"`
	Default         json.RawMessage `json:"default"`
	Min             *float64        `json:"min,omitempty"`
	Max             *float64        `json:"max,omitempty"`
	MaxLength       int             `json:"maxLength,omitempty"`
	Accept          string          `json:"accept,omitempty"`
	Options         []SelectOption  `json:"options,omitempty"`
	Statuses        []SelectOption  `json:"statuses,omitempty"`
	RestartRequired bool            `json:"restartRequired,omitempty"`
}

const (
	maxText  = 4000
	maxNames = 2000
	maxRules = 50
)

var (
	nameID   = regexp.MustCompile(`^[UWA][A-Z0-9]{6,}$`)
	timeOfDy = regexp.MustCompile(`^([01]\d|2[0-3]):[0-5]\d$`)
	hexColor = regexp.MustCompile(`^#[0-9a-fA-F]{3,8}$`)
)

func (s Setting) defaultValue() any {
	var v any
	_ = json.Unmarshal(s.Default, &v)
	if v == nil {
		switch s.Type {
		case "boolean":
			return false
		case "number":
			return 0.0
		case "names":
			return map[string]any{}
		case "schedule":
			return []any{}
		}
		return ""
	}
	return v
}

// coerce is coerceSetting: value (decoded JSON) as this setting's type, or
// the default.
func (s Setting) coerce(value any) any {
	if value == nil {
		// undefined and null both fall back (null never type-checks)
		return s.defaultValue()
	}
	switch s.Type {
	case "boolean":
		b, _ := value.(bool)
		return b
	case "number":
		n, ok := toNumber(value)
		if !ok || math.IsNaN(n) || math.IsInf(n, 0) {
			return s.defaultValue()
		}
		if s.Min != nil && n < *s.Min {
			n = *s.Min
		}
		if s.Max != nil && n > *s.Max {
			n = *s.Max
		}
		return n
	case "select":
		str, _ := value.(string)
		for _, o := range s.Options {
			if o.Value == str {
				return str
			}
		}
		return s.defaultValue()
	case "color":
		if str, ok := value.(string); ok && hexColor.MatchString(str) {
			return str
		}
		return s.defaultValue()
	case "names":
		m, ok := value.(map[string]any)
		if !ok {
			return s.defaultValue()
		}
		ids := make([]string, 0, len(m))
		for id := range m {
			ids = append(ids, id)
		}
		sort.Strings(ids) // deterministic cap (JS keeps insertion order)
		out := map[string]any{}
		for _, id := range ids {
			if len(out) >= maxNames {
				break
			}
			name, ok := m[id].(string)
			if !nameID.MatchString(id) || !ok || name == "" || len([]rune(name)) > 100 {
				continue
			}
			out[id] = name
		}
		return out
	case "schedule":
		return s.coerceSchedule(value)
	}
	// text, file and unknown kinds: a string, capped
	str, ok := value.(string)
	if !ok {
		return s.defaultValue()
	}
	limit := s.MaxLength
	if limit <= 0 {
		limit = maxText
	}
	if r := []rune(str); len(r) > limit {
		str = string(r[:limit])
	}
	return str
}

func (s Setting) coerceSchedule(value any) any {
	list, ok := value.([]any)
	if !ok {
		return s.defaultValue()
	}
	if len(list) > maxRules {
		list = list[:maxRules]
	}
	rules := []any{}
	for _, x := range list {
		r, ok := x.(map[string]any)
		if !ok {
			continue
		}
		days, ok1 := r["days"].([]any)
		start, ok2 := r["start"].(string)
		end, ok3 := r["end"].(string)
		if !ok1 || !ok2 || !ok3 || !timeOfDy.MatchString(start) || !timeOfDy.MatchString(end) {
			continue
		}
		seen := map[int]bool{}
		for _, d := range days {
			if n, ok := d.(float64); ok && n == math.Trunc(n) && n >= 0 && n <= 6 {
				seen[int(n)] = true
			}
		}
		valid := []any{}
		for d := 0; d <= 6; d++ {
			if seen[d] {
				valid = append(valid, float64(d))
			}
		}
		status := ""
		if len(s.Statuses) > 0 {
			status = s.Statuses[0].Value
		}
		if st, ok := r["status"].(string); ok {
			for _, o := range s.Statuses {
				if o.Value == st {
					status = st
				}
			}
		}
		rules = append(rules, map[string]any{"days": valid, "start": start, "end": end, "status": status})
	}
	return rules
}

func toNumber(v any) (float64, bool) {
	switch x := v.(type) {
	case float64:
		return x, true
	case bool:
		if x {
			return 1, true
		}
		return 0, true
	case string:
		// Number("") is 0 in JS; Number("12") is 12
		if x == "" {
			return 0, true
		}
		var f float64
		if err := json.Unmarshal([]byte(x), &f); err != nil {
			return 0, false
		}
		return f, true
	}
	return 0, false
}

// Schema is a plugin's settings, in declaration order.
type Schema struct {
	Keys     []string
	Settings map[string]Setting
}

// UnmarshalJSON keeps the declaration order of the keys.
func (s *Schema) UnmarshalJSON(b []byte) error {
	s.Settings = map[string]Setting{}
	s.Keys = nil
	keys, err := orderedKeys(b)
	if err != nil {
		return err
	}
	var m map[string]Setting
	if err := json.Unmarshal(b, &m); err != nil {
		return err
	}
	for _, k := range keys {
		if k == "enabled" {
			continue // reserved: activation, never a setting
		}
		s.Keys = append(s.Keys, k)
		s.Settings[k] = m[k]
	}
	return nil
}

// MarshalJSON writes the schema back in order.
func (s Schema) MarshalJSON() ([]byte, error) {
	var b []byte
	b = append(b, '{')
	for i, k := range s.Keys {
		if i > 0 {
			b = append(b, ',')
		}
		kb, _ := json.Marshal(k)
		vb, err := json.Marshal(s.Settings[k])
		if err != nil {
			return nil, err
		}
		b = append(append(append(b, kb...), ':'), vb...)
	}
	return append(b, '}'), nil
}

func orderedKeys(b []byte) ([]string, error) {
	dec := json.NewDecoder(bytes.NewReader(b))
	t, err := dec.Token()
	if err != nil {
		return nil, err
	}
	if d, ok := t.(json.Delim); !ok || d != '{' {
		return nil, nil
	}
	var keys []string
	for dec.More() {
		t, err := dec.Token()
		if err != nil {
			return nil, err
		}
		k, _ := t.(string)
		keys = append(keys, k)
		var skip json.RawMessage
		if err := dec.Decode(&skip); err != nil {
			return nil, err
		}
	}
	return keys, nil
}

// resolve is resolveSettings: the stored block coerced against the schema,
// unknown keys dropped, enabled from the block or defaultEnabled.
func (s Schema) resolve(defaultEnabled bool, stored map[string]any) map[string]any {
	out := make(map[string]any, len(s.Keys)+1)
	for _, k := range s.Keys {
		set := s.Settings[k]
		if v, ok := stored[k]; ok {
			out[k] = set.coerce(v)
		} else {
			out[k] = set.defaultValue()
		}
	}
	enabled := defaultEnabled
	if b, ok := stored["enabled"].(bool); ok {
		enabled = b
	}
	out["enabled"] = enabled
	return out
}

// changedKeys is Slick's changedKeys over two resolved configs.
func changedKeys(before, after map[string]any) []string {
	keys := map[string]bool{}
	for k := range before {
		keys[k] = true
	}
	for k := range after {
		keys[k] = true
	}
	var out []string
	for k := range keys {
		a, _ := json.Marshal(before[k])
		b, _ := json.Marshal(after[k])
		if string(a) != string(b) {
			out = append(out, k)
		}
	}
	sort.Strings(out)
	return out
}
