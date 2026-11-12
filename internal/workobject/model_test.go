package workobject

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/some-du6e/clack/internal/i18n"
)

const taskJSON = `{
 "work_object_type":"external","work_object_entity_type":"slack#/entities/task",
 "entity_id":"W:A0FAKEWOT1:TASK-142","app_id":"A0FAKEWOT1","app_name":"Example Tracker",
 "product_name":"Tracker","display_type":"Task","external_url":"https://tracker.example.com/TASK-142",
 "comments":{"count":2,"latest_timestamp":1762699400},"is_refreshable":true,"ts":"1762699700",
 "layouts":{
  "compact":{"title":{"type":"plain_text","text":"Fix it"},"subtitle":{"type":"plain_text","text":"TASK-142"},
   "actions":{"primary_actions":[{"type":"button","action_id":"a1","block_id":"b1","text":{"type":"plain_text","text":"Start"}},{"type":"button","text":{"type":"plain_text","text":"no ids"}}],
    "overflow_actions":{"app_actions":[{"type":"button","action_id":"a2","block_id":"b1","text":{"type":"plain_text","text":"Assign"}}]}}},
  "expanded":{"title":{"type":"plain_text","text":"Fix it"},"header_title":{"type":"plain_text","text":"Tracker"},"updated_at":1762699700,
   "fields":{"elements":[
    {"field_type":"tag","label":"Status","tag":[{"text":"Open","color":"lagoon"}]},
    {"field_type":"user","label":"Who","user":[{"user_type":"slack","user_id":"U1"}]},
    {"field_type":"date","label":"Due","date":["2026-11-20"]},
    {"field_type":"text","label":"Missing text"},
    {"field_type":"image","label":"Empty image","image":[]},
    {"field_type":"sparkles","label":"Unknown","rich_text":{"type":"rich_text","elements":[]}},
    {"field_type":"sparkles","label":"Unknown, no rich text"},
    {"field_type":"header_with_badge","label":"Header","text":"Revenue"}
   ]},
   "actions":{"overflow_actions":{"app_actions":[{"type":"button","action_id":"a2","block_id":"b1","text":{"type":"plain_text","text":"Assign"}}]}}},
  "full":{"title":{"type":"plain_text","text":"Fix it","input":{"type":"input","block_id":"t","element":{"type":"plain_text_input"}}},
   "subtitle":{"type":"plain_text","text":"Example Tracker","url":"https://tracker.example.com"},"editable":true,
   "header_title":{"type":"plain_text","text":"Task"},"header_subtitle":{"type":"plain_text","text":"Tracker"}}
 }}`

// WorkObjectModel (JrfS): the layouts, the fields makeField keeps and
// drops, and the accessors.
func TestParseWorkObject(t *testing.T) {
	e := Parse(json.RawMessage(taskJSON))
	if e == nil || !e.IsExternal() || e.EntityType != EntityTask || e.EntityID != "W:A0FAKEWOT1:TASK-142" {
		t.Fatalf("parse: %+v", e)
	}
	if l := e.UnfurlLayout(); l == nil || l.Key != Expanded || l.HeaderTitle != "Tracker" || l.UpdatedAtUnix() != 1762699700 {
		t.Fatalf("unfurl layout %+v", l)
	}
	var kinds []string
	for _, f := range e.Expanded().Fields {
		kinds = append(kinds, f.Type+":"+f.Label)
	}
	want := []string{"tag:Status", "user:Who", "date:Due", "unknown:Unknown", "header_with_badge:Header"}
	if len(kinds) != len(want) {
		t.Fatalf("fields %v, want %v (text without text, an empty image and an unknown type without rich_text drop)", kinds, want)
	}
	for i := range want {
		if kinds[i] != want[i] {
			t.Fatalf("fields %v, want %v", kinds, want)
		}
	}
	if d := String(e.Expanded().Fields[2].Date); d != "2026-11-20" {
		t.Errorf("date %q", d)
	}
	f := e.Full()
	if f == nil || !f.Editable || f.SubtitleURL != "https://tracker.example.com" || f.HeaderSubtitle != "Tracker" || len(f.TitleInput) == 0 {
		t.Fatalf("full %+v", f)
	}
	// the expanded layout's own overflow actions; without an expanded
	// layout the compact's primary actions (with ids) come first
	if a := e.ContextMenuAppActions(); len(a) != 1 || a[0].ActionID != "a2" {
		t.Errorf("menu actions %+v", a)
	}
	if !e.IsRefreshable() || e.Comments == nil || e.Comments.Count != 2 {
		t.Errorf("refreshable/comments %v %+v", e.IsRefreshable(), e.Comments)
	}
	if app, obj := AppAndObject(e.EntityID); app != "A0FAKEWOT1" || obj != "TASK-142" {
		t.Errorf("entity id parts %q %q", app, obj)
	}
	// no layouts: not a work object
	if Parse(json.RawMessage(`{"work_object_type":"external"}`)) != nil {
		t.Error("an entity without layouts parsed")
	}
}

// The deprecated array format (fieldsContainsDeprecatedValues): rich_text
// arrays become their first block, long_text rich_text, short_text text.
func TestDeprecatedFields(t *testing.T) {
	e := Parse(json.RawMessage(`{"work_object_type":"external","layouts":{"expanded":{"title":{"text":"x"},"fields":{"elements":[
		{"field_type":"long_text","label":"Body","rich_text":[{"type":"rich_text","elements":[]}]},
		{"field_type":"short_text","label":"Plain","text":"hi"},
		{"field_type":"short_text","label":"Rich","rich_text":[{"type":"rich_text","elements":[]}]}]}}}}`))
	if e == nil {
		t.Fatal("no entity")
	}
	fs := e.Expanded().Fields
	if len(fs) != 3 || fs[0].Type != FieldRichText || len(fs[0].RichText) == 0 || fs[1].Type != FieldText || fs[1].Text != "hi" || fs[2].Type != FieldRichText {
		t.Fatalf("fields %+v", fs)
	}
}

// generateUpdatedTimestamp and generateAsOfTimestamp in en-US.
func TestFooterLabels(t *testing.T) {
	i18n.Use(i18n.EnUS, nil)
	now := time.Date(2026, 11, 11, 15, 0, 0, 0, time.Local)
	for _, c := range []struct {
		at   time.Time
		want string
	}{
		{now.Add(-20 * time.Second), "Updated just now"},
		{now.Add(-5 * time.Minute), "Updated 5 min ago"},
		{now.Add(-1 * time.Hour), "Updated 1 hour ago"},
		{now.Add(-3 * time.Hour), "Updated 3 hours ago"},
		{now.AddDate(0, 0, -1).Add(-2 * time.Hour), "Updated yesterday"},
		{now.AddDate(0, 0, -3), "Updated on Sunday"},
		{time.Date(2026, 9, 3, 12, 0, 0, 0, time.Local), "Updated on Sep 3"},
		{time.Date(2025, 9, 3, 12, 0, 0, 0, time.Local), "Updated on Sep 3, 2025"},
	} {
		if got := UpdatedLabel(i18n.EnUS, c.at, now); got != c.want {
			t.Errorf("updated %v: %q, want %q", c.at, got, c.want)
		}
	}
	for _, c := range []struct {
		at         time.Time
		from, want string
	}{
		{time.Date(2026, 11, 11, 9, 41, 0, 0, time.Local), "", "As of today at 9:41 AM"},
		{time.Date(2026, 11, 10, 9, 41, 0, 0, time.Local), "Robin", "As of yesterday at 9:41 AM from Robin"},
		{time.Date(2026, 11, 7, 9, 41, 0, 0, time.Local), "", "As of Saturday at 9:41 AM"},
		{time.Date(2026, 10, 1, 9, 41, 0, 0, time.Local), "", "As of Oct 1"},
		{time.Date(2025, 10, 1, 9, 41, 0, 0, time.Local), "", "As of Oct 1, 2025"},
	} {
		if got := AsOfLabel(i18n.EnUS, c.at, now, false, c.from); got != c.want {
			t.Errorf("as of %v: %q, want %q", c.at, got, c.want)
		}
	}
	if got := AsOfLabel(i18n.EnUS, time.Date(2026, 11, 11, 15, 4, 0, 0, time.Local), now, true, ""); got != "As of today at 15:04" {
		t.Errorf("24h: %q", got)
	}
}
