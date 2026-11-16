package core

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"testing"
	"time"

	"github.com/some-du6e/clack/internal/fakeslack"
	"github.com/some-du6e/clack/internal/slack"
	"github.com/some-du6e/clack/internal/store"
)

// shChannel is the i-th made-up channel; the ids sort in i order, so
// messages.list's groups of 10 are channels 0-9, 10-19 and 20-22.
func shChannel(i int) string { return fmt.Sprintf("C0SH%05d", i) }

// A Saved page over 23 channels whose messages.list fails for one group
// (too_many_channels) or for all of them: an item is Gone (deleted) only
// when its channel's group answered without it. Before the split, one
// failed call left every item without its message, and then a missing
// message couldn't be told from a deleted one.
func TestSavedGoneOnlyWhereItsGroupAnswered(t *testing.T) {
	const here, gone = "1762800100.000100", "1762800900.000100"
	for _, c := range []struct {
		name string
		fail func(ch string) bool
	}{
		{"one group fails", func(ch string) bool { return ch == shChannel(12) }},
		{"every group fails", func(string) bool { return true }},
	} {
		t.Run(c.name, func(t *testing.T) {
			fs, err := fakeslack.Start(fakeslack.Small)
			if err != nil {
				t.Fatal(err)
			}
			defer fs.Close()
			var items []map[string]any
			for i := range 23 {
				items = append(items, map[string]any{"item_id": shChannel(i), "item_type": "message", "ts": here})
				items = append(items, map[string]any{"item_id": shChannel(i), "item_type": "message", "ts": gone})
			}
			fs.SetHook(func(method string, form url.Values) (any, bool) {
				switch method {
				case "saved.list":
					return map[string]any{"ok": true, "saved_items": items}, true
				case "messages.list":
					var want []struct {
						Channel    string   `json:"channel"`
						Timestamps []string `json:"timestamps"`
					}
					_ = json.Unmarshal([]byte(form.Get("message_ids")), &want)
					if len(want) > 10 {
						return map[string]any{"ok": false, "error": "too_many_channels"}, true
					}
					out := map[string][]map[string]any{}
					for _, w := range want {
						if c.fail(w.Channel) {
							return map[string]any{"ok": false, "error": "too_many_channels"}, true
						}
						out[w.Channel] = []map[string]any{{"type": "message", "ts": here, "user": "U0FAKE00001", "text": "synthetic"}}
					}
					return map[string]any{"ok": true, "messages": out}, true
				}
				return nil, false
			})
			a := New(slack.New(fs.Creds()), store.New(""))
			ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
			defer cancel()
			p, err := a.Saved(ctx, SavedFilterSaved, "")
			if err != nil {
				t.Fatal(err)
			}
			if len(p.Items) != 46 {
				t.Fatalf("%d items, want 46", len(p.Items))
			}
			for _, it := range p.Items {
				var i int
				_, _ = fmt.Sscanf(it.Channel, "C0SH%05d", &i)
				answered := !c.fail(it.Channel) && !(c.name == "one group fails" && i >= 10 && i < 20)
				switch {
				case it.TS == here && (it.Msg != nil) != answered:
					t.Errorf("%s/%s (answered %v): Msg %v", it.Channel, it.TS, answered, it.Msg != nil)
				case it.TS == gone && it.Gone != answered:
					t.Errorf("%s/%s (answered %v): Gone %v", it.Channel, it.TS, answered, it.Gone)
				case it.TS == here && it.Gone:
					t.Errorf("%s/%s has its message but is Gone", it.Channel, it.TS)
				}
			}
		})
	}
}
