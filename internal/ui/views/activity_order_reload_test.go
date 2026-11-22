package views

// What a reload keeps besides its page (activity_order.go actReloadKeeps):
// ui's rows (actMerge, actRetained) in Slack's order. The rows the page's
// span no longer lists are gone, the newest one too (an invitation you
// ignored or accepted); the later pages stay under every view but Unreads,
// the VIP sort included; a thread twin the mention dedupe keeps out of the
// list doesn't hide that the page reaches them; a copy a realtime entry
// brought while the request was out wins over the page's older one.
// Synthetic entries only.

import (
	"slices"
	"strconv"
	"strings"
	"testing"

	"github.com/some-du6e/clack/internal/core"
)

// actOrderSet is items' keys, sorted: what's listed, whatever the order.
func actOrderSet(items []core.ActivityItem) string {
	var ks []string
	for i := range items {
		ks = append(ks, actKey(&items[i]))
	}
	slices.Sort(ks)
	return strings.Join(ks, ",")
}

func actOrderKeysOf(keys ...string) string {
	ks := slices.Clone(keys)
	slices.Sort(ks)
	return strings.Join(ks, ",")
}

// The newest row the server no longer lists leaves on a reload (Clack's
// invitation flows remove the answered invite only by reloading); one a
// realtime entry changed while the request was out, or one the list
// didn't hold then, stays.
func TestActivityReloadDropsTheNewestRowGone(t *testing.T) {
	cur := core.ActivityPage{Next: "old-cursor", Items: []core.ActivityItem{
		actOI("inv", "external_dm_invite", "110.0"), actOI("a", "at_user", "100.0"),
		actOI("b", "at_user", "90.0"), actOI("c", "at_user", "80.0"),
	}}
	page := func(next string) core.ActivityPage {
		return core.ActivityPage{Next: next, Items: []core.ActivityItem{
			actOI("a", "at_user", "100.0"), actOI("b", "at_user", "90.0"), actOI("c", "at_user", "80.0")}}
	}
	for _, c := range []struct {
		name string
		next string
		view actReloadCase
	}{
		{"the plain list, the feed's last page", "", actReloadCase{}},
		{"the plain list, more pages", "p2", actReloadCase{}},
		{"Mentions", "p2", actReloadCase{filters: true}},
		{"the VIP sort", "p2", actReloadCase{vip: true}},
		{"Unreads", "p2", actReloadCase{unreads: true}},
	} {
		got := actReloadTest(page(c.next), cur, c.view)
		if s := actOrderKeys(got.Items); s != "a@100.0,b@90.0,c@80.0" {
			t.Errorf("%s: the reload lists %s, want the invitation the page no longer lists gone", c.name, s)
		}
		if got.Next != c.next {
			t.Errorf("%s: cursor %q, want the page's %q", c.name, got.Next, c.next)
		}
	}
	// changed on the socket while the request was out, or not listed then:
	// newer than the page's read
	touched := actReloadTest(page("p2"), cur, actReloadCase{touched: []string{"inv"}})
	if s := actOrderKeys(touched.Items); s != "inv@110.0,a@100.0,b@90.0,c@80.0" {
		t.Errorf("a row a realtime entry changed meanwhile: %s, want it kept", s)
	}
	arrived := actReloadTest(page("p2"), cur, actReloadCase{before: []string{"a", "b", "c"}})
	if s := actOrderKeys(arrived.Items); s != "inv@110.0,a@100.0,b@90.0,c@80.0" {
		t.Errorf("a row in on the socket meanwhile: %s, want it kept", s)
	}
}

// Under the VIP sort the feed isn't in time order (VIP unreads first,
// then the rest), and a reload's first page keeps the later pages loaded
// after it, found by the feed's order. When the old list doesn't hold the
// page's last row, or a row before it moved down, the cursor is the
// page's, so nothing is skipped. In time order a first-page row the page
// doesn't hold is gone: it stays out and the later pages stay. Reloads
// come from the peek opening, Undo and ⌘Z after a Clear, the invitation
// actions and activity_invalidated (ui's TestActivityMergeKeepsLaterPages
// OutOfTimeOrder, on the list stored by feed_ts).
func TestActivityMergeKeepsLaterPagesOutOfTimeOrder(t *testing.T) {
	ts := func(n int) string { return strconv.Itoa(1762700000+n) + ".000100" }
	vipItem := func(key string, n int) core.ActivityItem {
		return core.ActivityItem{Key: key, Type: "at_user", FeedTS: ts(n), Priority: true, Unread: true}
	}
	item := func(key string, n int) core.ActivityItem {
		return core.ActivityItem{Key: key, Type: "at_user", FeedTS: ts(n)}
	}
	var page1, old []core.ActivityItem
	for i := 0; i < 20; i++ { // page 1: VIP unreads, ts 100 … 81
		page1 = append(page1, vipItem("vip-"+string(rune('a'+i)), 100-i))
	}
	old = append(old, page1...)
	old = append(old, vipItem("vip-last", 5)) // page 2: the last VIP unread, then the rest by time
	for i := 0; i < 19; i++ {
		old = append(old, item("rest-"+string(rune('a'+i)), 99-i))
	}
	stored := core.ActivityPage{Items: slices.Clone(old), Next: "c3"}
	actSortFeed(stored.Items) // the list is stored by feed_ts (um/L Y)
	got := actReloadTest(core.ActivityPage{Items: page1, Next: "c2"}, stored, actReloadCase{vip: true})
	if actOrderSet(got.Items) != actOrderSet(old) || got.Next != "c3" {
		t.Fatalf("merged %d rows (next %q), want the %d loaded with next c3", len(got.Items), got.Next, len(old))
	}
	// a fresh page that ends on a row the old list doesn't hold
	fresh := append([]core.ActivityItem{vipItem("new", 101)}, page1[:19]...)
	fresh[len(fresh)-1] = vipItem("unseen", 82)
	got = actReloadTest(core.ActivityPage{Items: fresh, Next: "c2"}, stored, actReloadCase{vip: true})
	if actOrderSet(got.Items) != actOrderSet(fresh) || got.Next != "c2" {
		t.Errorf("merged %d rows (next %q), want fresh's %d with its cursor c2", len(got.Items), got.Next, len(fresh))
	}
	// the viewer read vip-b: it left the VIP unreads for the rest, a page
	// further on, so the reload pages again from the page instead of
	// keeping the old cursor past where vip-b now sits
	read := append(append([]core.ActivityItem{page1[0]}, page1[2:]...), old[20]) // vip-a, vip-c … vip-t, vip-last
	got = actReloadTest(core.ActivityPage{Items: read, Next: "c2"}, stored, actReloadCase{vip: true})
	if actOrderSet(got.Items) != actOrderSet(read) || got.Next != "c2" {
		t.Errorf("after reading vip-b: merged %d rows (next %q), want the page's %d with its cursor c2", len(got.Items), got.Next, len(read))
	}
	// in time order a first-page row the page doesn't hold is gone
	// (deleted, cleared): it stays out and the later pages stay, under the
	// plain list and a type tab alike
	var chrono []core.ActivityItem
	for i := 0; i < 40; i++ {
		chrono = append(chrono, item("t-"+strconv.Itoa(i), 200-i))
	}
	gone := append(append([]core.ActivityItem{}, chrono[:3]...), chrono[4:21]...) // t-3 deleted: t-20 joins page 1
	want := actOrderSet(append(append([]core.ActivityItem{}, chrono[:3]...), chrono[4:]...))
	for _, view := range []actReloadCase{{}, {filters: true}} {
		got = actReloadTest(core.ActivityPage{Items: gone, Next: "c2"}, core.ActivityPage{Items: chrono, Next: "c3"}, view)
		if actOrderSet(got.Items) != want || got.Next != "c3" {
			t.Errorf("time order (filters %v): merged %d rows (next %q), want %d with next c3", view.filters, len(got.Items), got.Next, len(chrono)-1)
		}
	}
	// Unreads keeps only the page and the retained rows (actRetained)
	got = actReloadTest(core.ActivityPage{Items: gone, Next: "c2"}, core.ActivityPage{Items: chrono, Next: "c3"}, actReloadCase{unreads: true})
	if actOrderSet(got.Items) != actOrderSet(gone) || got.Next != "c2" {
		t.Errorf("Unreads: merged %d rows (next %q), want the page's %d with next c2", len(got.Items), got.Next, len(gone))
	}
}

// A reply that mentions you comes twice in the feed, an at_user entry and
// the thread's thread_v2 with the same feed_ts, and the list keeps only
// the mention (1Cyf v). A page that ends on such a pair, or on the twin
// whose mention a later page brought, still reaches the rows loaded
// before: the later pages stay.
func TestActivityReloadThreadTwinKeepsLaterPages(t *testing.T) {
	mention := core.ActivityItem{Key: "at_user-C1-150.000000", Type: "at_user", FeedTS: "150.000000"}
	twin := core.ActivityItem{Key: "thread_v2-C1-140.000000", Type: "thread_v2", FeedTS: "150.000000", LatestTS: "150.000000"}
	var top []core.ActivityItem
	for i := 0; i < 18; i++ {
		top = append(top, actOI("r"+strconv.Itoa(i), "keyword", strconv.Itoa(300-i)+".000000"))
	}
	later := []core.ActivityItem{actOI("p2a", "keyword", "120.000000"), actOI("p2b", "keyword", "110.000000")}
	for _, c := range []struct {
		name string
		page []core.ActivityItem
		cur  []core.ActivityItem // the list as stored: never the twin
	}{
		{"the pair ends the page", append(slices.Clone(top), mention, twin), append(append(slices.Clone(top), mention), later...)},
		{"the twin first", append(slices.Clone(top), twin, mention), append(append(slices.Clone(top), mention), later...)},
		{"the twin ends the page, its mention on the next", append(slices.Clone(top), twin), append(append(slices.Clone(top), mention), later...)},
	} {
		cur := core.ActivityPage{Items: c.cur, Next: "p3"}
		actSortFeed(cur.Items)
		got := actReloadTest(core.ActivityPage{Items: c.page, Next: "p2"}, cur, actReloadCase{})
		if actOrderSet(got.Items) != actOrderSet(c.cur) || got.Next != "p3" {
			t.Errorf("%s: %d rows (next %q), want the %d loaded with next p3", c.name, len(got.Items), got.Next, len(c.cur))
		}
	}
}

// A thread that gets a reply on the socket while a reload is out keeps
// the copy the socket brought (newer, unread) over the page's, read
// before the reply.
func TestActivityReloadKeepsTheSocketsCopy(t *testing.T) {
	th := core.ActivityItem{Key: "thread_v2-C1-100.0", Type: "thread_v2", FeedTS: "130.0", LatestTS: "130.0", Unread: true}
	cur := core.ActivityPage{Items: []core.ActivityItem{th, actOI("a", "at_user", "120.0"), actOI("b", "at_user", "110.0")}}
	old := th
	old.FeedTS, old.LatestTS, old.Unread = "105.0", "105.0", false
	page := core.ActivityPage{Items: []core.ActivityItem{actOI("a", "at_user", "120.0"), actOI("b", "at_user", "110.0"), old}}
	got := actReloadTest(page, cur, actReloadCase{touched: []string{th.Key}})
	if s := actOrderKeys(got.Items); s != "thread_v2-C1-100.0@130.0,a@120.0,b@110.0" || !got.Items[0].Unread {
		t.Errorf("the reload lists %s (unread %v), want the socket's copy first and unread", s, got.Items[0].Unread)
	}
	if want := actOrderKeysOf("thread_v2-C1-100.0", "a", "b"); actOrderSet(got.Items) != want {
		t.Errorf("rows %s, want %s", actOrderSet(got.Items), want)
	}
}
