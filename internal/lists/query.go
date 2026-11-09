package lists

import (
	"math"
	"sort"
	"strconv"
	"strings"
	"time"
	"unicode"

	"golang.org/x/text/unicode/norm"
)

// Filter operators (Slack's pNWq enum).
const (
	OpEqual              = "equal"
	OpNotEqual           = "not_equal"
	OpGreaterThan        = "greater_than"
	OpLessThan           = "less_than"
	OpGreaterThanOrEqual = "greater_than_or_equal"
	OpLessThanOrEqual    = "less_than_or_equal"
	OpContains           = "contains"
	OpDoesNotContain     = "does_not_contain"
	OpIsEmpty            = "is_empty"
	OpIsNotEmpty         = "is_not_empty"
	OpIsMe               = "is_me"
	OpIsNotMe            = "is_not_me"
)

// Dynamic date filter values (Slack's +haa enum).
const (
	DateToday            = "today"
	DateYesterday        = "yesterday"
	DateTomorrow         = "tomorrow"
	DateThisWeek         = "this_week"
	DateNextWeek         = "next_week"
	DateLastWeek         = "last_week"
	DateUpcoming         = "upcoming"
	DateOverdue          = "overdue"
	DateLater            = "later"
	DateSinceILastViewed = "since_i_last_viewed"
)

// Operators are the operators Slack's filter menu offers for a column
// type (BFYq.UE).
func Operators(c *Column) []string {
	switch c.Type {
	case Checkbox, Completed, Message, Link, TodoCompleted:
		return []string{OpIsEmpty, OpIsNotEmpty}
	case Assignee, CreatedBy, LastEditedBy, Rating, Select, User, Text, TodoAssignee:
		return []string{OpContains, OpDoesNotContain, OpIsEmpty, OpIsNotEmpty}
	case Currency, Number, Date, TodoDueDate:
		return []string{OpEqual, OpNotEqual, OpLessThan, OpGreaterThan, OpIsEmpty, OpIsNotEmpty}
	}
	return []string{OpEqual, OpNotEqual, OpIsEmpty, OpIsNotEmpty}
}

// OperatorLabel is the filter menu's wording for op on c (BFYq.R8).
func OperatorLabel(op string, c *Column) string {
	checkbox := c.Type == Checkbox
	completed := c.Type == Completed || c.Type == TodoCompleted
	date := isDateType(c.Type)
	switch op {
	case OpIsEmpty:
		if checkbox {
			return "is not checked"
		}
		if completed {
			return "Not completed"
		}
		return "is empty"
	case OpIsNotEmpty:
		if checkbox {
			return "is checked"
		}
		if completed {
			return "Completed"
		}
		return "is not empty"
	case OpEqual:
		if date {
			return "is"
		}
		return "is equal to"
	case OpNotEqual:
		if date {
			return "is not"
		}
		return "is not equal to"
	case OpGreaterThan:
		if date {
			return "is after"
		}
		return "is greater than"
	case OpLessThan:
		if date {
			return "is before"
		}
		return "is less than"
	case OpGreaterThanOrEqual:
		return "is greater than or equal to"
	case OpLessThanOrEqual:
		return "is less than or equal to"
	case OpContains:
		return "includes"
	case OpDoesNotContain:
		return "does not include"
	case OpIsMe:
		return "is me"
	case OpIsNotMe:
		return "is not me"
	}
	return ""
}

func isDateType(t string) bool {
	switch t {
	case Date, DueDate, CreatedTime, LastEditedTime, TodoDueDate:
		return true
	}
	return false
}

func isTodoColumn(t string) bool { return t == TodoCompleted || t == TodoAssignee || t == TodoDueDate }

// Env is what the rules need besides the list: who is looking, names for
// people and channels (sorting and search use them), and "now".
type Env struct {
	Self         string
	UserNames    map[string]string // user id -> real name
	ChannelNames map[string]string // channel id -> name
	Now          time.Time
	LastRead     int64 // unix seconds the viewer last read the list (since_i_last_viewed)
	TodoMode     bool
}

func (e Env) now() time.Time {
	if e.Now.IsZero() {
		return time.Now()
	}
	return e.Now
}

// Row is a record with its flat cells (by column key).
type Row struct {
	Rec  *Record
	Flat map[string]Flat
}

// Rows flattens records for the rules.
func Rows(m *Meta, recs []Record) []Row {
	out := make([]Row, 0, len(recs))
	for i := range recs {
		r := &recs[i]
		fl := make(map[string]Flat, len(m.Schema))
		for j := range m.Schema {
			fl[m.Schema[j].Key] = Flatten(r.Field(m.Schema[j].Key))
		}
		out = append(out, Row{Rec: r, Flat: fl})
	}
	return out
}

// Apply runs a view over rows the way Slack's useGetRecords does: filters,
// then sorts, then the search box; rows without a sort keep the list's
// default order (DefaultOrder first).
func Apply(m *Meta, v *View, rows []Row, search string, env Env) []Row {
	if v != nil {
		rows = ApplyFilters(m, v.Filters, rows, env)
		if len(v.Sorts) > 0 {
			rows = ApplySorts(m, v.Sorts, rows, env)
		}
	}
	if search != "" {
		rows = Search(m, rows, search, env)
	}
	return rows
}

// DefaultOrder is Slack's sortRecordsByDefault: the record's position in
// the view (view_positions), else its list position, then creation time.
func DefaultOrder(rows []Row, viewID string) {
	pos := func(r *Record) (string, bool) {
		for k, p := range r.ViewPositions {
			if strings.EqualFold(k, viewID) && p != "" {
				return p, true
			}
		}
		return r.Position, r.Position != ""
	}
	sort.SliceStable(rows, func(i, j int) bool {
		a, aok := pos(rows[i].Rec)
		b, bok := pos(rows[j].Rec)
		switch {
		case aok && bok && a != b:
			return a < b
		case aok != bok:
			return aok // records without a position sort last
		}
		return rows[i].Rec.DateCreated < rows[j].Rec.DateCreated
	})
}

// ApplyFilters is Slack's applyFiltersToRecords (all filters must match).
func ApplyFilters(m *Meta, fs []Filter, rows []Row, env Env) []Row {
	for _, f := range fs {
		c := m.Column(f.Key)
		if c == nil || (!env.TodoMode && isTodoColumn(c.Type)) {
			continue
		}
		rows = filterRows(rows, func(r Row) bool { return matches(c, f, r.Flat[c.Key], r.Rec, env) })
	}
	return rows
}

func filterRows(rows []Row, keep func(Row) bool) []Row {
	out := rows[:0:0]
	for _, r := range rows {
		if keep(r) {
			out = append(out, r)
		}
	}
	return out
}

func first(vs []string) string {
	if len(vs) == 0 {
		return ""
	}
	return vs[0]
}

func matches(c *Column, f Filter, v Flat, rec *Record, env Env) bool {
	switch f.Operator {
	case OpIsEmpty:
		return v.Empty
	case OpIsNotEmpty:
		return !v.Empty
	}
	op := f.Operator
	switch c.Type {
	case Text:
		q := strings.ToLower(first(f.Values))
		has := strings.Contains(strings.ToLower(textOf(v)), q)
		if op == OpDoesNotContain {
			return !has
		}
		return has
	case Email, Phone:
		return textMatch(v.Text(), op, first(f.Values))
	case Number, Currency:
		if len(f.Values) == 0 {
			return true
		}
		if v.Kind != FlatNumber && (v.Kind != FlatString || v.S == "") {
			return false // a null cell never matches a number filter
		}
		want, _ := strconv.ParseFloat(first(f.Values), 64)
		got := v.N
		if v.Kind == FlatString {
			got, _ = strconv.ParseFloat(v.S, 64)
		}
		switch op {
		case OpNotEqual:
			return got != want
		case OpGreaterThan:
			return got > want
		case OpGreaterThanOrEqual:
			return got >= want
		case OpLessThan:
			return got < want
		case OpLessThanOrEqual:
			return got <= want
		}
		return got == want
	case Rating:
		if len(f.Values) == 0 {
			return true
		}
		s := ""
		if v.Kind == FlatNumber {
			s = v.Text()
		}
		in := false
		for _, x := range f.Values {
			in = in || x == s
		}
		switch op {
		case OpContains:
			return in
		case OpDoesNotContain:
			return !in
		}
		return true
	case Date, DueDate, TodoDueDate, CreatedTime, LastEditedTime:
		return dateMatch(f, op, v, env)
	case Checkbox, Completed, TodoCompleted, Link:
		return true
	case Select:
		return optionMatch(f.Values, op, v, c.Options.Format == "multi_select", env.Self)
	case Channel, Canvas, User, Assignee, TodoAssignee, CreatedBy, LastEditedBy:
		switch op {
		case OpIsMe:
			return env.Self != "" && strings.Contains(v.Text(), env.Self)
		case OpIsNotMe:
			return env.Self == "" || !strings.Contains(v.Text(), env.Self)
		}
		return optionMatch(f.Values, op, v, c.Options.Format == "multi_entity", env.Self)
	}
	return textMatch(v.Text(), op, first(f.Values))
}

func textOf(v Flat) string {
	if v.Kind == FlatRich {
		return v.S
	}
	return v.Text()
}

func textMatch(got, op, want string) bool {
	has := strings.Contains(strings.ToLower(got), strings.ToLower(want))
	if op == OpDoesNotContain {
		return !has
	}
	return has
}

// optionMatch is Slack's multiOptionFilter. "viewer" in the filter stands
// for the person looking. Single-value columns compare only their first
// value.
func optionMatch(values []string, op string, v Flat, multi bool, self string) bool {
	want := map[string]bool{}
	for _, x := range values {
		if x == "viewer" && self != "" {
			x = self
		}
		want[x] = true
	}
	if len(want) == 0 {
		return true
	}
	found := false
	for _, p := range v.Parts() {
		if want[p] {
			found = true
			break
		}
		if !multi {
			break
		}
	}
	switch op {
	case OpEqual, OpContains:
		return found
	case OpNotEqual:
		return !found
	}
	return !found
}

const dayLayout = "2006-01-02"

func day(t time.Time) time.Time {
	y, m, d := t.Date()
	return time.Date(y, m, d, 0, 0, 0, 0, t.Location())
}

func weekOf(t time.Time) []time.Time {
	wd := int(t.Weekday())
	if wd == 0 {
		wd = 7 // ISO weeks start on Monday
	}
	mon := day(t).AddDate(0, 0, 1-wd)
	out := make([]time.Time, 7)
	for i := range out {
		out[i] = mon.AddDate(0, 0, i)
	}
	return out
}

// recordDay is a date cell's day: unix seconds (created/edited times,
// timestamps) or a YYYY-MM-DD date (a range's start).
func recordDay(v Flat, loc *time.Location) (time.Time, bool) {
	switch v.Kind {
	case FlatNumber:
		if v.N == 0 {
			return time.Time{}, false
		}
		return time.Unix(int64(v.N), 0).In(loc), true
	case FlatString:
		s, _, _ := strings.Cut(v.S, ":")
		if t, err := time.ParseInLocation(dayLayout, s, loc); err == nil {
			return t, true
		}
	}
	return time.Time{}, false
}

// dateMatch is Slack's multiDateFilter for one record.
func dateMatch(f Filter, op string, v Flat, env Env) bool {
	if first(f.Values) == "" {
		return true
	}
	now := env.now()
	loc := now.Location()
	if first(f.Values) == DateSinceILastViewed {
		op = OpGreaterThan
	}
	if v.Kind == FlatNone {
		return false
	}
	recVal := 0.0
	if v.Kind == FlatNumber {
		recVal = v.N
	}
	rd, ok := recordDay(v, loc)
	for _, fv := range f.Values {
		if fv == "" {
			continue
		}
		switch fv {
		case DateLater:
			if op == OpNotEqual || op == OpDoesNotContain {
				if recVal != 0 {
					return true
				}
			} else if (op == OpEqual || op == OpContains) && recVal == 0 && v.Kind == FlatNumber {
				return true
			}
			continue
		case DateUpcoming:
			if !ok {
				continue
			}
			lim := day(now).AddDate(0, 0, 2)
			if op == OpNotEqual || op == OpDoesNotContain {
				if day(rd).Before(lim) {
					return true
				}
			} else if (op == OpEqual || op == OpContains) && day(rd).After(lim) {
				return true
			}
			continue
		case DateOverdue:
			if !ok || recVal == 0 && v.Kind == FlatNumber {
				continue
			}
			if op == OpNotEqual || op == OpDoesNotContain {
				if rd.After(now) {
					return true
				}
			} else if (op == OpEqual || op == OpContains) && rd.Before(now) {
				return true
			}
			continue
		}
		if !ok {
			continue
		}
		var days []time.Time
		exact := false
		switch fv {
		case DateToday:
			days = []time.Time{day(now)}
		case DateYesterday:
			days = []time.Time{day(now).AddDate(0, 0, -1)}
		case DateTomorrow:
			days = []time.Time{day(now).AddDate(0, 0, 1)}
		case DateThisWeek:
			days = weekOf(now)
		case DateNextWeek:
			days = weekOf(now.AddDate(0, 0, 7))
		case DateLastWeek:
			days = weekOf(now.AddDate(0, 0, -7))
		case DateSinceILastViewed:
			days, exact = []time.Time{time.Unix(env.LastRead, 0).In(loc)}, true
		default:
			t, err := time.ParseInLocation(dayLayout, fv, loc)
			if err != nil {
				continue
			}
			days = []time.Time{t}
		}
		if len(days) > 1 {
			if op == OpNotEqual {
				in := false
				for _, d := range days {
					in = in || d.Equal(day(rd))
				}
				if !in {
					return true
				}
				continue
			}
			for _, d := range days {
				if dateCmp(op, d, rd, false) {
					return true
				}
			}
			continue
		}
		if dateCmp(op, days[0], rd, exact) {
			return true
		}
	}
	return false
}

// dateCmp is Slack's multiDateOperatorFilterValueCheck: the filter's date
// against the record's (by day unless exact).
func dateCmp(op string, filter, rec time.Time, exact bool) bool {
	if !exact {
		filter, rec = day(filter), day(rec)
	}
	switch op {
	case OpNotEqual:
		return !filter.Equal(rec)
	case OpGreaterThan:
		return filter.Before(rec)
	case OpGreaterThanOrEqual:
		return !filter.After(rec)
	case OpLessThan:
		return filter.After(rec)
	case OpLessThanOrEqual:
		return !filter.Before(rec)
	}
	return filter.Equal(rec)
}

// sortKey is one record's value for one sort column: a number or a string
// (Slack's applySortToRecords puts empty values last either way).
type sortKey struct {
	num   bool
	n     float64
	s     string
	empty bool
}

func (a sortKey) less(b sortKey) bool {
	if a.num && b.num {
		return a.n < b.n
	}
	if a.num != b.num {
		return a.num // lodash's orderBy: numbers before strings
	}
	return a.s < b.s
}

func emptyKey(asc bool) sortKey {
	if asc {
		return sortKey{num: true, n: math.MaxFloat64, empty: true}
	}
	return sortKey{num: true, n: math.Inf(-1), empty: true}
}

// sortValue is Slack's per-type sort value.
func sortValue(c *Column, v Flat, asc bool, env Env) sortKey {
	zzz := "zzzzzzzz"
	strEmpty := func() sortKey {
		if asc {
			return sortKey{s: zzz, empty: true}
		}
		return sortKey{s: "", empty: true}
	}
	switch c.Type {
	case Select:
		parts := v.Parts()
		n := len(c.Options.Choices)
		if n == 0 || len(parts) == 0 {
			return emptyKey(asc)
		}
		best := n
		for _, p := range parts {
			for i, ch := range c.Options.Choices {
				if ch.Value == p && i < best {
					best = i
				}
			}
		}
		if best < n {
			return sortKey{num: true, n: float64(best)}
		}
		return emptyKey(asc)
	case User, TodoAssignee:
		if env.UserNames != nil {
			if v.Text() == "" || v.Empty {
				return strEmpty()
			}
			if name := env.UserNames[v.Text()]; name != "" {
				return sortKey{s: strings.ToLower(name)}
			}
			return strEmpty()
		}
	case Text:
		if v.Empty || textOf(v) == "" {
			return strEmpty()
		}
		return sortKey{s: strings.ToLower(textOf(v))}
	case Vote:
		if n := len(v.Parts()); n > 0 {
			return sortKey{num: true, n: float64(n)}
		}
		return emptyKey(asc)
	case Number, Currency, Rating:
		if v.Kind != FlatNumber {
			return emptyKey(asc)
		}
		return sortKey{num: true, n: v.N}
	case Date, DueDate, TodoDueDate, CreatedTime, LastEditedTime:
		switch v.Kind {
		case FlatNone:
			if asc {
				return sortKey{num: true, n: math.MaxFloat64 / 2, empty: true}
			}
			return sortKey{num: true, n: 0, empty: true}
		case FlatNumber:
			if v.N == 0 {
				return emptyKey(asc)
			}
			return sortKey{num: true, n: v.N * 1000}
		case FlatString:
			if v.S == "" {
				if asc {
					return sortKey{num: true, n: math.MaxFloat64 / 2, empty: true}
				}
				return sortKey{num: true, n: 0, empty: true}
			}
			if t, ok := recordDay(v, time.UTC); ok {
				return sortKey{num: true, n: float64(t.UnixMilli())}
			}
		}
	case Channel:
		if env.ChannelNames != nil {
			if v.Empty {
				return strEmpty()
			}
			ps := v.Parts()
			if len(ps) > 0 {
				return sortKey{s: strings.ToLower(env.ChannelNames[ps[0]])}
			}
			return strEmpty()
		}
	}
	switch v.Kind {
	case FlatNumber:
		return sortKey{num: true, n: v.N}
	case FlatBool:
		if v.B {
			return sortKey{num: true, n: 1}
		}
		return sortKey{num: true, n: 0}
	case FlatNone:
		return emptyKey(asc)
	}
	return sortKey{s: textOf(v)}
}

// ApplySorts is Slack's applySortToRecords (lodash orderBy over the sort
// keys, stable).
func ApplySorts(m *Meta, ss []Sort, rows []Row, env Env) []Row {
	type key struct {
		c   *Column
		asc bool
	}
	var keys []key
	for _, s := range ss {
		c := m.Column(s.Key)
		if c == nil || (!env.TodoMode && isTodoColumn(c.Type)) {
			continue
		}
		keys = append(keys, key{c, s.Ascending})
	}
	if len(keys) == 0 {
		return rows
	}
	vals := make([][]sortKey, len(rows))
	for i, r := range rows {
		vals[i] = make([]sortKey, len(keys))
		for k, kk := range keys {
			vals[i][k] = sortValue(kk.c, r.Flat[kk.c.Key], kk.asc, env)
		}
	}
	idx := make([]int, len(rows))
	for i := range idx {
		idx[i] = i
	}
	sort.SliceStable(idx, func(i, j int) bool {
		a, b := vals[idx[i]], vals[idx[j]]
		for k, kk := range keys {
			if a[k].less(b[k]) {
				return kk.asc
			}
			if b[k].less(a[k]) {
				return !kk.asc
			}
		}
		return false
	})
	out := make([]Row, len(rows))
	for i, j := range idx {
		out[i] = rows[j]
	}
	return out
}

func normalize(s string) string {
	d := norm.NFD.String(s)
	var b strings.Builder
	for _, r := range d {
		if unicode.Is(unicode.Mn, r) {
			continue
		}
		b.WriteRune(unicode.ToLower(r))
	}
	return b.String()
}

// Search is the list's search box: case- and accent-insensitive over text,
// email, phone, number, rating, select labels, people and channel names,
// and vote counts.
func Search(m *Meta, rows []Row, q string, env Env) []Row {
	q = normalize(q)
	var cols []*Column
	for i := range m.Schema {
		switch m.Schema[i].Type {
		case Text, Email, Phone, Number, Rating, Select, User, TodoAssignee, Channel, Vote:
			cols = append(cols, &m.Schema[i])
		}
	}
	return filterRows(rows, func(r Row) bool {
		for _, c := range cols {
			v := r.Flat[c.Key]
			switch c.Type {
			case Select:
				parts := v.Parts()
				if c.Options.Format != "multi_select" && len(parts) > 1 {
					parts = parts[:1]
				}
				for _, p := range parts {
					lbl := p
					if ch, ok := c.Choice(p); ok {
						lbl = ch.Label
					}
					if strings.Contains(normalize(lbl), q) {
						return true
					}
				}
				continue
			case Channel:
				if env.ChannelNames != nil {
					if n, ok := env.ChannelNames[v.Text()]; ok && strings.Contains(normalize(n), q) {
						return true
					}
					continue
				}
			case User, TodoAssignee:
				if env.UserNames != nil {
					if n, ok := env.UserNames[v.Text()]; ok && strings.Contains(normalize(n), q) {
						return true
					}
					continue
				}
			case Vote:
				if strings.Contains(strconv.Itoa(len(v.Parts())), q) {
					return true
				}
				continue
			}
			if strings.Contains(normalize(textOf(v)), q) {
				return true
			}
		}
		return false
	})
}

// Group is one group of a grouped view (a board lane).
type Group struct {
	Value string // "" = the empty group
	Rows  []Row
}

// EmptyGroupLabel is Slack's label for the group of records without a
// value (getEmptyGroupLabel); checkbox columns show an unchecked box.
func EmptyGroupLabel(c *Column) string {
	switch c.Type {
	case Rating:
		return "Not Rated"
	case Date:
		return "No date set"
	case DueDate, TodoDueDate:
		return "No due date set"
	}
	return "Ungrouped"
}

// GroupRows splits rows by the group-by column's value. Select columns
// give one group per option, in option order; people, channels and other
// columns one per distinct value in first-seen order; checkboxes checked
// and not. The empty group comes first.
func GroupRows(c *Column, rows []Row) []Group {
	idx := map[string]int{}
	var gs []Group
	add := func(v string) int {
		if i, ok := idx[v]; ok {
			return i
		}
		idx[v] = len(gs)
		gs = append(gs, Group{Value: v})
		return len(gs) - 1
	}
	add("")
	switch c.Type {
	case Select:
		for _, ch := range c.Options.Choices {
			add(ch.Value)
		}
	case Checkbox, Completed, TodoCompleted:
		add("true")
	}
	for _, r := range rows {
		v := r.Flat[c.Key]
		key := ""
		switch {
		case v.Kind == FlatBool:
			if v.B {
				key = "true"
			}
		case v.Empty:
		case v.Kind == FlatString:
			if ps := v.Parts(); len(ps) > 0 {
				key = ps[0]
			}
		default:
			key = v.Text()
		}
		i := add(key)
		gs[i].Rows = append(gs[i].Rows, r)
	}
	return gs
}
