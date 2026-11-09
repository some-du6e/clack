package core

import (
	"encoding/json"
	"os"
	"testing"
)

// The synthetic feed (testdata/activity_feed.json) has one entry per item
// type the web client handles. Every one must locate its subject: the
// "all new posts" channel item (channel_entry) used to come out with no
// channel, ts or author, which drew an empty row.
func TestActivityItemShapes(t *testing.T) {
	b, err := os.ReadFile("testdata/activity_feed.json")
	if err != nil {
		t.Fatal(err)
	}
	var feed struct {
		Items []activityRaw `json:"items"`
	}
	if err := json.Unmarshal(b, &feed); err != nil {
		t.Fatal(err)
	}
	type want struct{ ch, ts, thread, author, actor, title, team, text string }
	cases := map[string]want{
		"k-channel":         {ch: "C1", ts: "1700000101.000100"},
		"k-channel-many":    {ch: "C1", ts: "1700000102.000100"},
		"k-channel-gone":    {ch: "C1", ts: "1700000199.000100"},
		"k-dm":              {ch: "D1", ts: "1700000104.000100"},
		"k-at-user":         {ch: "C1", ts: "1700000105.000100", author: "U1"},
		"k-at-channel":      {ch: "C1", ts: "1700000106.000100", author: "U2"},
		"k-at-everyone":     {ch: "C1", ts: "1700000107.000100", author: "U2"},
		"k-at-group":        {ch: "C1", ts: "1700000108.000100", author: "U1"},
		"k-keyword":         {ch: "C1", ts: "1700000109.000100", author: "U2"},
		"k-thread":          {ch: "C1", ts: "1700000111.000100", thread: "1700000110.000100"},
		"k-reaction":        {ch: "C1", ts: "1700000110.000100"},
		"k-bot":             {ch: "D1", ts: "1700000113.000100"},
		"k-invite":          {ch: "C2", ts: "1700000114.000100", author: "U1"},
		"k-quiet":           {ch: "C2", actor: "U1"},
		"k-unjoined":        {ch: "C2", ts: "1700000116.000100"},
		"k-code-review":     {ch: "C1", ts: "1700000117.000100"},
		"k-ext-channel":     {actor: "U9EXT", title: "partners", team: "Example Co", text: "come chat"},
		"k-ext-dm":          {actor: "U9EXT", team: "Example Co"},
		"k-joiner":          {ch: "C1", ts: "1700000120.000100"},
		"k-prejoin":         {ch: "D1", actor: "U2"},
		"k-list-assigned":   {actor: "U1"},
		"k-list-edited":     {actor: "U2"},
		"k-list-mention":    {actor: "U1"},
		"k-list-todo":       {},
		"k-list-approval":   {actor: "U2"},
		"k-reminder":        {ch: "C1", ts: "1700000105.000100"},
		"k-reminder-plain":  {text: "water the synthetic plants"},
		"k-calendar":        {title: "Synthetic standup"},
		"k-alert":           {title: "CHANNEL", text: "A synthetic channel was archived."},
		"k-future":          {ch: "C1", ts: "1700000131.000100", author: "U2"},
		"k-future-bare":     {},
		"k-reaction-many":   {ch: "C1", ts: "1700000132.000100"},
		"k-thread-many":     {ch: "C1", ts: "1700000135.000100", thread: "1700000134.000100"},
		"k-mpdm":            {ch: "G1", ts: "1700000137.000100"},
		"k-joiner-reminder": {ch: "C1", ts: "1700000120.000100"},
		"k-ext-revoked":     {actor: "U9EXT", title: "old-partners", team: "Example Co"},
		"k-reminder-file":   {},
		"k-mine":            {ch: "C1", ts: "1700000142.000100", author: "USELF"},
	}
	if len(feed.Items) != len(cases) {
		t.Fatalf("fixture has %d items, cases %d", len(feed.Items), len(cases))
	}
	for i := range feed.Items {
		it, _ := activityItem(&feed.Items[i])
		w, ok := cases[it.Key]
		if !ok {
			t.Errorf("no case for %s", it.Key)
			continue
		}
		got := want{it.Channel, it.TS, it.ThreadTS, it.Author, it.Actor, it.Title, it.Team, it.Text}
		if got != w {
			t.Errorf("%s (%s):\n got %+v\nwant %+v", it.Key, it.Type, got, w)
		}
		if it.Type == "" || it.FeedTS == "" {
			t.Errorf("%s lost its type or feed ts", it.Key)
		}
		// the payload fields Slack's renderers read (w4rX, wM7g, aJri)
		switch it.Key {
		case "k-joiner", "k-joiner-reminder":
			if it.JoinerReminder != (it.Key == "k-joiner-reminder") {
				t.Errorf("%s: JoinerReminder %v", it.Key, it.JoinerReminder)
			}
		case "k-ext-revoked":
			if it.InviteStatus != "revoked" {
				t.Errorf("%s: InviteStatus %q", it.Key, it.InviteStatus)
			}
		case "k-reminder", "k-reminder-plain", "k-reminder-file":
			want := map[string]string{"k-reminder": "message", "k-reminder-plain": "reminder", "k-reminder-file": "file"}[it.Key]
			if it.ReminderItem != want {
				t.Errorf("%s: ReminderItem %q, want %q", it.Key, it.ReminderItem, want)
			}
		}
	}
}
