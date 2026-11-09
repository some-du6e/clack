package lists

import (
	"encoding/json"
	"strings"
	"testing"
	"time"
)

func testMeta() *Meta {
	return &Meta{Schema: []Column{
		{ID: "C1", Key: "name", Type: Text, IsPrimary: true},
		{ID: "C2", Key: "status", Type: Select, Options: Options{Format: "single_select", Choices: []Choice{{Value: "a", Label: "Todo"}, {Value: "b", Label: "Doing"}, {Value: "c", Label: "Done"}}}},
		{ID: "C3", Key: "n", Type: Number},
		{ID: "C4", Key: "due", Type: Date},
		{ID: "C5", Key: "who", Type: User, Options: Options{Format: "multi_entity"}},
		{ID: "C6", Key: "ok", Type: Checkbox},
	}}
}

func rec(id string, fs ...Field) Record { return Record{ID: id, Fields: fs} }

func txt(s string) Field {
	b, _ := json.Marshal(TextBlocks(s))
	return Field{Key: "name", Text: &s, RichText: b}
}

func ids(rows []Row) string {
	var out []string
	for _, r := range rows {
		out = append(out, r.Rec.ID)
	}
	return strings.Join(out, ",")
}

func TestFiltersAndSorts(t *testing.T) {
	m := testMeta()
	now := time.Date(2026, 11, 9, 12, 0, 0, 0, time.UTC)
	tru := true
	recs := []Record{
		rec("r1", txt("Alpha"), Field{Key: "status", Select: []string{"b"}}, Field{Key: "n", Number: []float64{3}}, Field{Key: "due", Date: []string{"2026-11-10"}}, Field{Key: "who", User: []string{"U2", "UME"}}),
		rec("r2", txt("beta"), Field{Key: "status", Select: []string{"a"}}, Field{Key: "n", Number: []float64{0}}, Field{Key: "due", Date: []string{"2026-11-01"}}, Field{Key: "ok", Checkbox: &tru}),
		rec("r3", txt(""), Field{Key: "status", Select: []string{"c"}}, Field{Key: "who", User: []string{"U3"}}),
	}
	rows := Rows(m, recs)
	env := Env{Self: "UME", Now: now}
	f := func(key, op string, vals ...string) []Filter {
		return []Filter{{Key: key, Operator: op, Values: vals}}
	}
	cases := []struct {
		name string
		fs   []Filter
		want string
	}{
		{"text contains", f("name", OpContains, "ALP"), "r1"},
		{"text empty", f("name", OpIsEmpty), "r3"},
		{"select is", f("status", OpContains, "a", "c"), "r2,r3"},
		{"select not", f("status", OpDoesNotContain, "c"), "r1,r2"},
		{"number >", f("n", OpGreaterThan, "1"), "r1"},
		{"number = 0 (zero is a value)", f("n", OpEqual, "0"), "r2"},
		{"number empty", f("n", OpIsEmpty), "r3"},
		{"date after", f("due", OpGreaterThan, "2026-11-05"), "r1"},
		{"date this week", f("due", OpEqual, DateThisWeek), "r1"},
		{"date tomorrow", f("due", OpEqual, DateTomorrow), "r1"},
		{"date overdue", f("due", OpEqual, DateOverdue), "r2"},
		{"people viewer", f("who", OpContains, "viewer"), "r1"},
		{"people is me", f("who", OpIsMe), "r1"},
		{"checkbox checked", f("ok", OpIsNotEmpty), "r2"},
		{"checkbox unchecked", f("ok", OpIsEmpty), "r1,r3"},
	}
	for _, c := range cases {
		if got := ids(ApplyFilters(m, c.fs, rows, env)); got != c.want {
			t.Errorf("%s: %s, want %s", c.name, got, c.want)
		}
	}
	srt := func(key string, asc bool) string {
		return ids(ApplySorts(m, []Sort{{Key: key, Ascending: asc}}, rows, env))
	}
	if got := srt("name", true); got != "r1,r2,r3" { // case-insensitive, empty last
		t.Errorf("name asc %s", got)
	}
	if got := srt("name", false); got != "r2,r1,r3" {
		t.Errorf("name desc %s", got)
	}
	if got := srt("status", true); got != "r2,r1,r3" { // by option order
		t.Errorf("status asc %s", got)
	}
	if got := srt("n", false); got != "r1,r2,r3" {
		t.Errorf("n desc %s", got)
	}
	if got := srt("due", true); got != "r2,r1,r3" {
		t.Errorf("due asc %s", got)
	}
	if got := ids(Search(m, rows, "doing", env)); got != "r1" { // select labels
		t.Errorf("search %s", got)
	}
	gs := GroupRows(m.Column("status"), rows)
	if len(gs) != 4 || gs[0].Value != "" || gs[1].Value != "a" || len(gs[1].Rows) != 1 || len(gs[3].Rows) != 1 {
		t.Errorf("groups %+v", gs)
	}
}

func TestDefaultOrder(t *testing.T) {
	m := testMeta()
	recs := []Record{{ID: "a", Position: "3"}, {ID: "b", Position: "1", ViewPositions: map[string]string{"View1": "9"}}, {ID: "c", DateCreated: 5}, {ID: "d", Position: "2"}}
	rows := Rows(m, recs)
	DefaultOrder(rows, "")
	if got := ids(rows); got != "b,d,a,c" {
		t.Errorf("list order %s", got)
	}
	DefaultOrder(rows, "VIEW1")
	if got := ids(rows); got != "d,a,b,c" {
		t.Errorf("view order %s", got)
	}
}

func TestGuard(t *testing.T) {
	text := &Column{ID: "C1", Key: "name", Type: Text}
	plain := txt("hello https://example.com")
	if err := Editable(text, &plain); err != nil {
		t.Errorf("plain text refused: %v", err)
	}
	for _, raw := range []string{
		`[{"type":"rich_text","elements":[{"type":"rich_text_section","elements":[{"type":"text","text":"b","style":{"bold":true}}]}]}]`,
		`[{"type":"rich_text","elements":[{"type":"rich_text_section","elements":[{"type":"user","user_id":"U1"}]}]}]`,
		`[{"type":"rich_text","elements":[{"type":"rich_text_section","elements":[{"type":"emoji","name":"tada"}]}]}]`,
		`[{"type":"rich_text","elements":[{"type":"rich_text_list","elements":[]}]}]`,
		`[{"type":"rich_text","elements":[{"type":"rich_text_section","elements":[{"type":"link","url":"https://x.y","text":"named"}]}]}]`,
	} {
		f := Field{Key: "name", RichText: json.RawMessage(raw)}
		if err := Editable(text, &f); err == nil {
			t.Errorf("lossy text editable: %s", raw)
		}
	}
	for _, typ := range []string{CreatedBy, CreatedTime, Message, Attachment, Canvas, Reference} {
		if err := Editable(&Column{Type: typ}, nil); err == nil {
			t.Errorf("%s editable", typ)
		}
	}
	if err := Editable(&Column{Type: Date, Options: Options{Format: "range"}}, nil); err == nil {
		t.Error("date range editable")
	}
	// encoding: the camelCase link keys go out snake_case
	m, err := Edit{RowID: "R", Column: &Column{ID: "CL", Type: Link}, Link: &LinkValue{OriginalURL: "https://a.b", DisplayName: "A"}}.Cell()
	if err != nil {
		t.Fatal(err)
	}
	b, _ := json.Marshal(m)
	if !strings.Contains(string(b), `"original_url":"https://a.b"`) || !strings.Contains(string(b), `"display_name":"A"`) || !strings.Contains(string(b), `"row_id":"R"`) {
		t.Errorf("link cell %s", b)
	}
	if s, why := PlainRichText(mustJSON(TextBlocks("https://example.com"))); why != "" || s != "https://example.com" {
		t.Errorf("link round trip %q %q", s, why)
	}
}

func mustJSON(v any) json.RawMessage {
	b, _ := json.Marshal(v)
	return b
}
